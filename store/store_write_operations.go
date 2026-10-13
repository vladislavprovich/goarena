package store

import (
	"errors"
	"time"
	"unsafe"

	"github.com/vladislavprovich/goarena/arena"
)

func checkSize(key, val []byte) error {
	if len(key) > MaxKeySize || entrySize(len(key), len(val), true) > MaxEntrySize {
		return ErrTooLarge
	}
	return nil
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
				return mapAllocErr(err)
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
		return mapAllocErr(err)
	}
	entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
	sh.beginWrite()
	if sh.needsRehash(t) {
		if err := sh.rehash(); err != nil {
			sh.slab.Free(ref, size)
			sh.endWrite()
			sh.mu.Unlock()
			return mapAllocErr(err)
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

// DeleteString is Delete for string keys.
func (s *Store) DeleteString(key string) bool {
	return s.Delete(unsafe.Slice(unsafe.StringData(key), len(key)))
}
