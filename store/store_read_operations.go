package store

import "unsafe"

// Get returns a copy of the value stored under key. It reports false if the
// key is absent, expired, or the store is closed.
func (s *Store) Get(key []byte) ([]byte, bool) {
	v, ok := s.lookup(key, nil, true)
	if ok && v == nil {
		v = []byte{}
	}
	return v, ok
}

// GetInto appends the value stored under key to dst[:0] and returns the
// result, reusing dst's capacity. It allocates only if dst is too small,
// which makes it the preferred read method on hot paths. Reads normally
// complete without taking any lock (see the package documentation).
func (s *Store) GetInto(dst, key []byte) ([]byte, bool) {
	return s.lookup(key, dst, true)
}

// View calls fn with a zero-copy view of the value stored under key, while
// holding the shard lock, and reports whether the key was found. Writers of
// the same shard wait while fn runs, so fn should be short.
//
// The slice passed to fn aliases off-heap memory: fn must not retain it or
// any sub-slice after it returns, must not modify it, and must not call
// methods of the same Store (that may deadlock). Copy the data if it is
// needed later.
func (s *Store) View(key []byte, fn func(val []byte)) bool {
	h := s.hash(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock() // fn may panic
	if s.closed.Load() {
		return false
	}
	_, _, e, found := sh.find(h, key)
	if !found || e.expired(nowNano()) {
		return false
	}
	fn(e.value())
	return true
}

// Has reports whether key is present and not expired.
func (s *Store) Has(key []byte) bool {
	_, ok := s.lookup(key, nil, false)
	return ok
}

// Len returns the number of entries, including expired entries that have
// not been evicted yet.
func (s *Store) Len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n += int(sh.live)
		sh.mu.Unlock()
	}
	return n
}

// Range calls fn for every live entry, shard by shard, until fn returns
// false. Each shard is locked while its entries are visited, so fn sees a
// consistent view of one shard at a time (and writers of that shard wait). The key and value slices
// alias off-heap memory and are valid only during the call; fn must not
// call methods of the same Store.
func (s *Store) Range(fn func(key, val []byte) bool) {
	now := nowNano()
	for i := range s.shards {
		sh := &s.shards[i]
		if !s.rangeShard(sh, now, fn) {
			return
		}
	}
}

func (s *Store) rangeShard(sh *shard, now int64, fn func(key, val []byte) bool) bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s.closed.Load() {
		return false
	}
	t := sh.tab.Load()
	for j := uint64(0); j <= t.mask; j++ {
		sl := t.at(j)
		if sl.hash < 2 {
			continue
		}
		e := sh.entryAt(sl.ref)
		if e.expired(now) {
			continue
		}
		if !fn(e.key(), e.value()) {
			return false
		}
	}
	return true
}

// GetString is Get for string keys and values.
func (s *Store) GetString(key string) (string, bool) {
	v, ok := s.lookup(unsafe.Slice(unsafe.StringData(key), len(key)), nil, true)
	if !ok || len(v) == 0 {
		return "", ok
	}
	return unsafe.String(unsafe.SliceData(v), len(v)), true // v is a fresh copy
}
