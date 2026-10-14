package store

// GetOrSet returns a copy of the value stored under key if the key is
// present and not expired; loaded is then true. Otherwise it stores val
// under key (with the store's default TTL), returns val itself and
// loaded = false.
//
// The check and the write are atomic: of several goroutines calling
// GetOrSet for the same absent key, exactly one stores its value and all
// the others return that value with loaded = true. A hit costs the same as
// [Store.Get] (no lock); only a miss takes the shard's writer lock.
//
// It returns ErrTooLarge or ErrNoMemory (wrapped) if val cannot be stored,
// and ErrClosed after Close.
func (s *Store) GetOrSet(key, val []byte) (actual []byte, loaded bool, err error) {
	if v, ok := s.Get(key); ok {
		return v, true, nil
	}
	if err := checkSize(key, val); err != nil {
		return nil, false, err
	}
	var exp int64
	if s.defaultTTL > 0 {
		exp = nowNano() + s.defaultTTL
	}
	h := s.hash(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return nil, false, ErrClosed
	}
	t, pos, e, found := sh.find(h, key)
	if found && !e.expired(nowNano()) {
		// Another writer stored the key after the lock-free lookup.
		v := append([]byte{}, e.value()...)
		sh.mu.Unlock()
		return v, true, nil
	}
	err = sh.storeLocked(h, key, val, exp, t, pos, e, found)
	sh.mu.Unlock()
	if err != nil {
		return nil, false, s.allocErr(err, key, val)
	}
	return val, false, nil
}

// GetOrLoad implements the cache-aside pattern: it returns a copy of the
// value stored under key if the key is present and not expired, and
// otherwise calls load, stores its result under key (with the store's
// default TTL) and returns it.
//
// load runs without any lock held, so it may be slow (a database query, an
// RPC) and may call methods of the store. If load returns an error, nothing
// is stored and GetOrLoad returns that error unchanged.
//
// Concurrent misses are not deduplicated: goroutines that miss the same key
// at the same time may each call load. The first value stored wins (see
// [Store.GetOrSet]); the other callers return that value instead of their
// own, so all of them observe the same result. Wrap the call in a
// single-flight group if load is expensive and duplicate calls must be
// avoided.
//
// If the loaded value cannot be stored (ErrTooLarge, ErrNoMemory),
// GetOrLoad returns the loaded value together with the wrapped error, so
// the caller can still use the value. After Close it returns ErrClosed
// without calling load.
func (s *Store) GetOrLoad(key []byte, load func() ([]byte, error)) ([]byte, error) {
	if v, ok := s.Get(key); ok {
		return v, nil
	}
	if s.closed.Load() {
		return nil, ErrClosed
	}
	v, err := load()
	if err != nil {
		return nil, err
	}
	actual, _, err := s.GetOrSet(key, v)
	if err != nil {
		return v, err
	}
	return actual, nil
}
