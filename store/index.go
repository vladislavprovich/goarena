package store

import (
	"unsafe"

	"github.com/savokh/goarena/arena"
)

// Reserved hash values of index slots. Real hashes are remapped away from
// them by fixHash.
const (
	hashEmpty     uint64 = 0 // slot never used since the last rehash
	hashTombstone uint64 = 1 // slot of a deleted entry
)

// slotSize is the size of one index slot in bytes: {hash uint64, ref uint64}.
const slotSize = 16

// minIndexCap is the initial (and minimum) number of slots per shard.
const minIndexCap = 64

// slot is one cell of the open-addressing table. It lives in arena (C)
// memory and therefore contains only integers.
type slot struct {
	hash uint64 // full 64-bit hash of the key, or hashEmpty / hashTombstone
	ref  uint64 // arena.Ref of the entry (0 for empty / tombstone slots)
}

// indexTable describes one slot array. A table is immutable once it has
// been published through shard.tab: a rehash builds a new table and
// publishes a new descriptor, so a lock-free reader always sees a
// consistent (base, mask) pair. The slot array itself is arena memory and
// is never returned to C while the store is open: an outgrown table is
// donated to the shard's slab and reused for entries (see DESIGN_NOTES.md,
// "Seqlock read path").
type indexTable struct {
	slots unsafe.Pointer // capacity * slotSize bytes of arena memory
	ref   arena.Ref      // arena reference of the slot array
	mask  uint64         // capacity - 1 (capacity is a power of two)
}

func (t *indexTable) capacity() uint64 { return t.mask + 1 }

// at returns the i-th slot.
func (t *indexTable) at(i uint64) *slot {
	return (*slot)(unsafe.Add(t.slots, i*slotSize))
}

// fixHash remaps the two reserved values so that every real key hash is >= 2.
func fixHash(h uint64) uint64 {
	if h < 2 {
		h += 2
	}
	return h
}

// newTable allocates a zeroed table with capacity c (a power of two) from
// the arena.
func newTable(a *arena.Arena, c uint64) (*indexTable, error) {
	n := int(c * slotSize)
	ref, err := a.Alloc(n)
	if err != nil {
		return nil, err
	}
	b := a.Bytes(ref, n)
	clear(b)
	return &indexTable{slots: unsafe.Pointer(unsafe.SliceData(b)), ref: ref, mask: c - 1}, nil
}

// needsRehash reports whether inserting one more entry would push the share
// of used slots (live + tombstones) above the maximum load factor of 0.75.
func (sh *shard) needsRehash(t *indexTable) bool {
	return (sh.live+sh.tombs+1)*4 > t.capacity()*3
}

// rehash rebuilds the table, doubling its capacity when live entries alone
// occupy more than 3/8 of it, and otherwise keeping the capacity and only
// purging tombstones. Slots store full hashes, so entries are not touched.
// The caller holds the write lock and has made the sequence number odd.
func (sh *shard) rehash() error {
	old := sh.tab.Load()
	newCap := old.capacity()
	if (sh.live+1)*8 > newCap*3 {
		newCap *= 2
	}
	nt, err := newTable(sh.a, newCap)
	if err != nil {
		return err
	}
	var live uint64
	for i := uint64(0); i <= old.mask; i++ {
		s := old.at(i)
		if s.hash >= 2 {
			j := s.hash & nt.mask
			for nt.at(j).hash != hashEmpty {
				j = (j + 1) & nt.mask
			}
			*nt.at(j) = *s
			live++
		}
	}
	sh.tab.Store(nt)
	sh.live, sh.tombs = live, 0
	// The old slot array may still be read by optimistic readers that
	// started before this rehash; they will fail validation. Recycling it
	// as entry memory keeps it mapped and avoids wasting it.
	sh.slab.Donate(old.ref, int(old.capacity()*slotSize))
	return nil
}

// insertAt stores (h, ref) in slot i, which must be empty or a tombstone.
func (sh *shard) insertAt(t *indexTable, i, h, ref uint64) {
	s := t.at(i)
	if s.hash == hashTombstone {
		sh.tombs--
	}
	s.hash, s.ref = h, ref
	sh.live++
}

// firstFree returns the first empty or tombstone slot on the probe sequence
// of h. The table always has at least one empty slot (load factor <= 0.75).
func firstFree(t *indexTable, h uint64) uint64 {
	i := h & t.mask
	for t.at(i).hash >= 2 {
		i = (i + 1) & t.mask
	}
	return i
}

// remove deletes the entry in slot i. If the next slot is empty, the probe
// sequences that pass through i end at i+1 anyway, so slot i (and the run
// of tombstones directly before it) can be turned back into empty slots,
// which keeps the tombstone count low under delete-heavy workloads.
func (sh *shard) remove(t *indexTable, i uint64) {
	s := t.at(i)
	sh.live--
	if t.at((i+1)&t.mask).hash != hashEmpty {
		s.hash, s.ref = hashTombstone, 0
		sh.tombs++
		return
	}
	s.hash, s.ref = hashEmpty, 0
	for j := (i - 1) & t.mask; j != i; j = (j - 1) & t.mask {
		p := t.at(j)
		if p.hash != hashTombstone {
			break
		}
		p.hash = hashEmpty
		sh.tombs--
	}
}

// probeStats returns the sum and maximum displacement of live slots from
// their home position (0 means found at the first probe).
func probeStats(t *indexTable) (sum, max uint64) {
	for i := uint64(0); i <= t.mask; i++ {
		s := t.at(i)
		if s.hash >= 2 {
			d := (i - s.hash) & t.mask
			sum += d
			if d > max {
				max = d
			}
		}
	}
	return sum, max
}
