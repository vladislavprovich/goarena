package store

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/vladislavprovich/goarena/arena"
)

// Eviction selects what the store does when a write needs memory and
// Config.MaxMemory (or the C allocator) refuses to provide more.
type Eviction int

const (
	// EvictNone (the default) never removes entries by itself: a write
	// that needs more memory than MaxMemory allows fails with ErrNoMemory
	// and the store is unchanged.
	EvictNone Eviction = iota

	// EvictClock makes room by evicting entries chosen by the CLOCK
	// (second-chance) policy, an approximation of LRU. Every index slot
	// has a reference bit that reads set (only if it is clear, so reads of
	// hot keys do not keep writing to memory); when a write cannot obtain
	// memory, the clock hand of the entry's shard sweeps the shard's
	// index, clears set bits ("second chance") and evicts the first entry
	// whose bit is clear and whose memory can hold the new entry. Expired
	// entries are evicted first, whatever their bit. See the package
	// documentation, "Eviction", for the exact semantics.
	EvictClock
)

// String returns "none" or "clock".
func (e Eviction) String() string {
	switch e {
	case EvictNone:
		return "none"
	case EvictClock:
		return "clock"
	}
	return fmt.Sprintf("Eviction(%d)", int(e))
}

// newRefBits returns a zeroed reference bitmap for a table of c slots.
func newRefBits(c uint64) []uint64 { return make([]uint64, (c+63)/64) }

// markRef sets the reference bit of slot i if it is clear. It may be called
// by lock-free readers, also on a table that has been replaced: the bitmap
// is Go memory owned by the descriptor, so a stale write is harmless (at
// worst it gives an unrelated entry a second chance).
func (t *indexTable) markRef(i uint64) {
	w := &t.refs[(i&t.mask)>>6]
	b := uint64(1) << (i & 63)
	if atomic.LoadUint64(w)&b == 0 {
		atomic.OrUint64(w, b)
	}
}

// clearRef clears the reference bit of slot i.
func (t *indexTable) clearRef(i uint64) {
	w := &t.refs[i>>6]
	b := uint64(1) << (i & 63)
	if atomic.LoadUint64(w)&b != 0 {
		atomic.AndUint64(w, ^b)
	}
}

// referenced reports whether the reference bit of slot i is set.
func (t *indexTable) referenced(i uint64) bool {
	return atomic.LoadUint64(&t.refs[i>>6])&(1<<(i&63)) != 0
}

// noSlot is the protect argument of evictAlloc when no slot is protected.
const noSlot = ^uint64(0)

// isOutOfMemory reports whether err is the arena's out-of-memory error.
func isOutOfMemory(err error) bool { return errors.Is(err, arena.ErrOutOfMemory) }

// allocSlow is called when sh.slab.Alloc(size) failed with err. With
// eviction enabled and an out-of-memory error it evicts entries of the
// shard (see evictAlloc) and returns the block; otherwise it returns err.
// The entry in slot protect (the entry being overwritten) is never
// evicted. sh.mu must be held and the sequence number must be even.
func (sh *shard) allocSlow(err error, size int, protect uint64) (arena.Ref, error) {
	if sh.tab.Load().refs == nil || !isOutOfMemory(err) {
		return arena.Nil, err
	}
	sh.beginWrite()
	ref, err := sh.evictAlloc(size, protect, nowNano())
	sh.endWrite()
	return ref, err
}

// evictAlloc evicts entries chosen by the CLOCK policy until a block for
// a size-byte entry can be allocated, and returns that block.
//
// The hand visits the slots of the shard's index in order. A live entry
// with its reference bit set loses the bit and is skipped (second
// chance). An entry whose bit is clear is a candidate:
//
//   - a candidate with the same block size as the new entry is evicted at
//     once; its block goes back to its free list and serves the new entry;
//   - a candidate with a larger block is remembered; if no same-size
//     candidate turns up among the next maxCandidates candidates, the
//     smallest larger one seen is evicted and its block is turned into carving
//     space for the new entry (Slab.Reclaim). Preferring same-size victims
//     keeps the mix of block sizes stable, so memory does not drift
//     towards small blocks when values have different sizes;
//   - a candidate with a smaller block is skipped: its memory cannot hold
//     the new entry.
//
// Expired entries are always removed when the hand reaches them. The scan
// is bounded by two full turns of the hand; if it finds no usable victim
// (for example a value larger than every entry of the shard), the write
// fails with ErrNoMemory as without eviction.
//
// sh.mu must be held and the sequence number must be odd.
func (sh *shard) evictAlloc(size int, protect uint64, now int64) (arena.Ref, error) {
	t := sh.tab.Load()
	want := sh.slab.BlockSize(size)
	carved := want <= arena.MaxClassSize && want <= sh.slab.SpanSize()/4
	limit := 2*t.capacity() + 1
	larger, largerSize, examined := noSlot, 0, 0
	for n := uint64(0); n < limit; n++ {
		i := sh.hand & t.mask
		sh.hand++
		sl := t.at(i)
		if sl.hash < 2 || i == protect {
			continue
		}
		e := sh.entryAt(sl.ref)
		expired := e.expired(now)
		if !expired && t.referenced(i) {
			t.clearRef(i)
			continue
		}
		esz := e.size()
		bs := sh.slab.BlockSize(esz)
		if bs == want || expired {
			sh.evictAt(t, i, e, expired, false)
			if bs != want {
				continue
			}
			r, err := sh.slab.Alloc(size)
			if err == nil {
				return r, nil
			}
			if !isOutOfMemory(err) {
				return arena.Nil, err
			}
			continue
		}
		if carved && bs > want && esz <= arena.MaxClassSize && (larger == noSlot || bs < largerSize) {
			larger, largerSize = i, bs // best fit: the smallest larger block
		}
		if examined++; examined >= maxCandidates && larger != noSlot {
			break
		}
	}
	if larger == noSlot {
		return arena.Nil, arena.ErrOutOfMemory
	}
	sl := t.at(larger)
	sh.evictAt(t, larger, sh.entryAt(sl.ref), false, true)
	return sh.slab.Alloc(size)
}

// maxCandidates bounds how many unreferenced entries of a different block
// size evictAlloc examines before it settles for a larger victim.
const maxCandidates = 256

// evictAt removes the entry e in slot i of t on behalf of the eviction
// policy. With reclaim its block becomes carving space (Slab.Reclaim),
// otherwise it goes back to its free list.
func (sh *shard) evictAt(t *indexTable, i uint64, e entry, expired, reclaim bool) {
	esz := e.size()
	ref := arena.Ref(t.at(i).ref)
	sh.kvBytes -= int64(esz - e.headerLen())
	sh.entryBytes -= int64(esz)
	sh.remove(t, i)
	if expired {
		sh.expired++
	} else {
		sh.evictions++
	}
	if reclaim {
		sh.slab.Reclaim(ref, esz)
	} else {
		sh.slab.Free(ref, esz)
	}
}

// rehashSlow is called when sh.rehash failed with err. With eviction
// enabled and an out-of-memory error the index cannot grow, so the shard
// keeps its capacity instead: it evicts unreferenced entries (CLOCK) until
// the live entries leave a margin of 1/16 of the slots below the maximum
// load factor, and removes all tombstones in place. Otherwise it returns
// err. sh.mu must be held and the sequence number must be odd.
func (sh *shard) rehashSlow(err error) error {
	t := sh.tab.Load()
	if t.refs == nil || !isOutOfMemory(err) {
		return err
	}
	c := t.capacity()
	target := c*3/4 - c/16 - 1
	now := nowNano()
	for n := uint64(0); sh.live > target && n < 2*c+1; n++ {
		i := sh.hand & t.mask
		sh.hand++
		sl := t.at(i)
		if sl.hash < 2 {
			continue
		}
		e := sh.entryAt(sl.ref)
		expired := e.expired(now)
		if !expired && t.referenced(i) {
			t.clearRef(i)
			continue
		}
		sh.evictAt(t, i, e, expired, false)
	}
	if (sh.live+1)*4 > c*3 {
		return err // nothing could be evicted
	}
	sh.compactInPlace(t)
	return nil
}

// compactInPlace removes every tombstone of t in place, without
// allocating: it turns the tombstones into empty slots and then, starting
// right after a slot that was empty before (no probe sequence crosses it),
// moves every live slot back to the first empty slot of its probe sequence,
// in scan order. A slot only moves backwards, onto a slot that was free when
// it was visited, so every probe sequence stays unbroken. Reference bits
// move with their slots. Lock-free readers may observe the table half
// rebuilt; they bounds-check every reference and fail validation because
// the sequence number is odd. It is the fallback of a rehash that cannot
// allocate a new table. sh.mu must be held and the sequence number must be
// odd.
func (sh *shard) compactInPlace(t *indexTable) {
	start := uint64(0)
	for t.at(start).hash != hashEmpty { // the load factor leaves empty slots
		start++
	}
	for i := uint64(0); i <= t.mask; i++ {
		if s := t.at(i); s.hash == hashTombstone {
			s.hash, s.ref = hashEmpty, 0
		}
	}
	for n := uint64(1); n <= t.mask+1; n++ {
		p := (start + n) & t.mask
		s := t.at(p)
		if s.hash < 2 {
			continue
		}
		q := s.hash & t.mask
		for q != p && t.at(q).hash != hashEmpty {
			q = (q + 1) & t.mask
		}
		if q == p {
			continue
		}
		*t.at(q) = *s
		s.hash, s.ref = hashEmpty, 0
		if t.refs != nil && t.referenced(p) {
			t.clearRef(p)
			t.markRef(q)
		}
	}
	sh.tombs = 0
}
