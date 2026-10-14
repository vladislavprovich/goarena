package store

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/vladislavprovich/goarena/arena"
)

// checkSize returns ErrTooLarge, wrapped with the sizes involved, if key or
// the entry for key and val exceed the limits.
func checkSize(key, val []byte) error {
	if len(key) > MaxKeySize || entrySize(len(key), len(val), true) > MaxEntrySize {
		return &tooLargeError{keyLen: len(key), valLen: len(val)}
	}
	return nil
}

// tooLargeError is ErrTooLarge with the sizes of the rejected write. Its
// message is built only when it is printed, which keeps checkSize cheap
// enough to be inlined into every write.
type tooLargeError struct{ keyLen, valLen int }

func (e *tooLargeError) Unwrap() error { return ErrTooLarge }

func (e *tooLargeError) Error() string {
	if e.keyLen > MaxKeySize {
		return fmt.Sprintf("%v: key of %d bytes exceeds MaxKeySize (%d bytes)", ErrTooLarge, e.keyLen, MaxKeySize)
	}
	return fmt.Sprintf("%v: %d-byte key and %d-byte value exceed MaxEntrySize (%d bytes including the entry header)",
		ErrTooLarge, e.keyLen, e.valLen, MaxEntrySize)
}

func mapAllocErr(err error) error {
	if errors.Is(err, arena.ErrOutOfMemory) {
		return ErrNoMemory
	}
	if errors.Is(err, arena.ErrReleased) {
		return ErrClosed
	}
	return err
}

// allocErr maps an allocation error of a write of key and val to the
// store's errors and adds the sizes and the memory limit to ErrNoMemory.
// It must be called without holding a shard lock.
func (s *Store) allocErr(err error, key, val []byte) error {
	err = mapAllocErr(err)
	if err != ErrNoMemory {
		return err
	}
	if s.maxMemory > 0 {
		return fmt.Errorf("%w: cannot store a %d-byte key with a %d-byte value: %d of %d bytes (Config.MaxMemory) reserved in chunks of %d bytes; delete entries, call EvictExpired, raise MaxMemory or lower ChunkSize",
			ErrNoMemory, len(key), len(val), s.arena.ReservedBytes(), s.maxMemory, s.arena.ChunkSize())
	}
	return fmt.Errorf("%w: cannot store a %d-byte key with a %d-byte value: the C allocator failed with %d bytes reserved",
		ErrNoMemory, len(key), len(val), s.arena.ReservedBytes())
}

// stringBytes returns the bytes of str without copying them. The result
// must not be modified; the store only reads (and copies) it.
func stringBytes(str string) []byte { return unsafe.Slice(unsafe.StringData(str), len(str)) }

// Set stores val under key, replacing any previous value. The default TTL
// of the store applies. Both slices are copied; the caller may reuse them.
func (s *Store) Set(key, val []byte) error {
	var exp int64
	if s.defaultTTL > 0 {
		exp = nowNano() + s.defaultTTL
	}
	return s.set(key, val, exp)
}

// SetWithTTL stores val under key with the given time to live. A ttl <= 0
// means the entry never expires.
func (s *Store) SetWithTTL(key, val []byte, ttl time.Duration) error {
	var exp int64
	if ttl > 0 {
		exp = nowNano() + int64(ttl)
	}
	return s.set(key, val, exp)
}

// set is the write path of Set, SetWithTTL and SetString. It contains its
// own copy of the logic of storeLocked: calling storeLocked here made a
// single-threaded Set measurably (~1.5 %) slower. Keep the two in sync.
func (s *Store) set(key, val []byte, exp int64) error {
	if err := checkSize(key, val); err != nil {
		return err
	}
	h := s.hash(key)
	sh := s.shardFor(h)
	size := entrySize(len(key), len(val), exp != 0)
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return ErrClosed
	}
	t, pos, e, found := sh.find(h, key)
	if found {
		oldSize := e.size()
		oldKV := oldSize - e.headerLen()
		if sh.slab.BlockSize(oldSize) == sh.slab.BlockSize(size) {
			// Same block size: overwrite in place, no allocation.
			sh.beginWrite()
			e.write(key, val, exp)
			sh.endWrite()
		} else {
			ref, err := sh.slab.Alloc(size)
			if err != nil {
				sh.mu.Unlock()
				return s.allocErr(err, key, val)
			}
			entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
			sl := t.at(pos)
			old := arena.Ref(sl.ref)
			sh.beginWrite()
			sl.ref = uint64(ref)
			sh.slab.Free(old, oldSize) // overwrites the old block's first word
			sh.endWrite()
		}
		sh.kvBytes += int64(len(key) + len(val) - oldKV)
		sh.entryBytes += int64(size - oldSize)
		sh.sets++
		sh.mu.Unlock()
		return nil
	}
	ref, err := sh.slab.Alloc(size)
	if err != nil {
		sh.mu.Unlock()
		return s.allocErr(err, key, val)
	}
	entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
	sh.beginWrite()
	if sh.needsRehash(t) {
		if err := sh.rehash(); err != nil {
			sh.slab.Free(ref, size)
			sh.endWrite()
			sh.mu.Unlock()
			return s.allocErr(err, key, val)
		}
		t = sh.tab.Load()
	}
	sh.insertAt(t, firstFree(t, h), h, uint64(ref))
	sh.endWrite()
	sh.kvBytes += int64(len(key) + len(val))
	sh.entryBytes += int64(size)
	sh.sets++
	sh.mu.Unlock()
	return nil
}

// storeLocked writes the entry (key, val, exp) into the shard, given the
// result of sh.find for key: it overwrites the entry found at slot pos of
// table t, or inserts a new one. sh.mu must be held. It returns the
// allocator's error unchanged; on error the shard is unchanged. GetOrSet
// uses it; set has an inlined copy of the same logic.
func (sh *shard) storeLocked(h uint64, key, val []byte, exp int64, t *indexTable, pos uint64, e entry, found bool) error {
	size := entrySize(len(key), len(val), exp != 0)
	if found {
		oldSize := e.size()
		oldKV := oldSize - e.headerLen()
		if sh.slab.BlockSize(oldSize) == sh.slab.BlockSize(size) {
			// Same block size: overwrite in place, no allocation.
			sh.beginWrite()
			e.write(key, val, exp)
			sh.endWrite()
		} else {
			ref, err := sh.slab.Alloc(size)
			if err != nil {
				return err
			}
			entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
			sl := t.at(pos)
			old := arena.Ref(sl.ref)
			sh.beginWrite()
			sl.ref = uint64(ref)
			sh.slab.Free(old, oldSize) // overwrites the old block's first word
			sh.endWrite()
		}
		sh.kvBytes += int64(len(key) + len(val) - oldKV)
		sh.entryBytes += int64(size - oldSize)
		sh.sets++
		return nil
	}
	ref, err := sh.slab.Alloc(size)
	if err != nil {
		return err
	}
	entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
	sh.beginWrite()
	if sh.needsRehash(t) {
		if err := sh.rehash(); err != nil {
			sh.slab.Free(ref, size)
			sh.endWrite()
			return err
		}
		t = sh.tab.Load()
	}
	sh.insertAt(t, firstFree(t, h), h, uint64(ref))
	sh.endWrite()
	sh.kvBytes += int64(len(key) + len(val))
	sh.entryBytes += int64(size)
	sh.sets++
	return nil
}

// Delete removes key and reports whether a live (non-expired) entry was
// removed.
func (s *Store) Delete(key []byte) bool {
	h := s.hash(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return false
	}
	t, pos, e, found := sh.find(h, key)
	if !found {
		sh.mu.Unlock()
		return false
	}
	live := !e.expired(nowNano())
	sh.beginWrite()
	sh.removeAt(t, pos, e)
	sh.endWrite()
	if live {
		sh.deletes++
	} else {
		sh.expired++
	}
	sh.mu.Unlock()
	return live
}

// removeAt frees the entry e stored in slot pos. sh.mu must be held and the
// sequence number must be odd.
func (sh *shard) removeAt(t *indexTable, pos uint64, e entry) {
	size := e.size()
	ref := arena.Ref(t.at(pos).ref)
	sh.kvBytes -= int64(size - e.headerLen())
	sh.entryBytes -= int64(size)
	sh.remove(t, pos)
	sh.slab.Free(ref, size)
}

// EvictExpired removes all entries whose TTL has elapsed and returns their
// number. Expired entries are invisible to readers even before eviction;
// calling EvictExpired periodically returns their memory to the slabs.
func (s *Store) EvictExpired() int {
	now := nowNano()
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		if s.closed.Load() {
			sh.mu.Unlock()
			return n
		}
		t := sh.tab.Load()
		sh.beginWrite()
		for j := uint64(0); j <= t.mask; j++ {
			sl := t.at(j)
			if sl.hash < 2 {
				continue
			}
			if e := sh.entryAt(sl.ref); e.expired(now) {
				// remove never moves entries, so the scan can continue
				sh.removeAt(t, j, e)
				sh.expired++
				n++
			}
		}
		sh.endWrite()
		sh.mu.Unlock()
	}
	return n
}

// SetString is Set for string keys and values. The strings are read in
// place (no conversion copy) and copied into the store.
func (s *Store) SetString(key, val string) error {
	return s.Set(unsafe.Slice(unsafe.StringData(key), len(key)), unsafe.Slice(unsafe.StringData(val), len(val)))
}

// SetStringWithTTL is SetWithTTL for string keys and values. The strings
// are read in place (no conversion copy) and copied into the store.
func (s *Store) SetStringWithTTL(key, val string, ttl time.Duration) error {
	return s.SetWithTTL(stringBytes(key), stringBytes(val), ttl)
}

// DeleteString is Delete for string keys.
func (s *Store) DeleteString(key string) bool {
	return s.Delete(unsafe.Slice(unsafe.StringData(key), len(key)))
}
