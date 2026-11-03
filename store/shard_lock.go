package store

import "sync"

// shardLock is the writer lock of a shard. Since v0.2.0 readers do not
// lock on the fast path (see readOptimistic), so a plain sync.Mutex
// replaces the sync.RWMutex of v0.1.0: there is no reader count that every
// reader of a hot shard would increment, and writers (critical sections of
// a few hundred nanoseconds) spin briefly inside sync.Mutex before they
// park.
//
// A pure spinning lock (test-and-test-and-set with runtime.Gosched) was
// evaluated as well: it lowered the write p99.9 under the Zipf workload
// slightly further, but by never releasing the CPU it kept all 16 logical
// CPUs saturated, and preemption of the lock holder by the operating
// system tripled the number of operations slower than 1 ms (docs/DESIGN.md,
// section 7.6), so it was not adopted.
type shardLock struct {
	mu sync.Mutex
}

// Lock acquires the lock.
func (l *shardLock) Lock() { l.mu.Lock() }

// Unlock releases the lock.
func (l *shardLock) Unlock() { l.mu.Unlock() }
