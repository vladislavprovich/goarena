package store

import (
	"sync/atomic"
	"unsafe"

	"github.com/vladislavprovich/goarena/arena"
)

// readerStripe counts optimistic readers in flight; padded to a line.
type readerStripe struct {
	n atomic.Int64
	_ [56]byte
}

// enterRead registers an optimistic reader and returns its stripe, or nil
// if the store is closed. Close waits until every stripe is zero before it
// releases memory, so an optimistic reader never touches freed chunks.
//
// The stripe is chosen from the address of a stack variable, which differs
// between goroutines; readers of the same hot key therefore do not share a
// counter (unlike the reader count of a sync.RWMutex, which every reader
// of a shard increments). The address is only used as a hash.
func (s *Store) enterRead() *atomic.Int64 {
	var marker byte
	x := uint64(uintptr(unsafe.Pointer(&marker))) >> 11
	c := &s.readers[(x*0x9E3779B97F4A7C15)>>(64-6)].n
	c.Add(1)
	if s.closed.Load() {
		c.Add(-1)
		return nil
	}
	return c
}

// Results of an optimistic lookup.
const (
	optRetry = iota // inconsistent snapshot: try again or fall back
	optMiss         // key absent (or expired) in a consistent snapshot
	optHit          // value copied in a consistent snapshot
)

// readOptimistic looks key up without taking the shard lock (seqlock
// reader). It reads the sequence number, probes the index with atomic slot
// loads, bounds-checks every reference it follows (a torn read may produce
// garbage, which must be rejected rather than dereferenced blindly),
// validates header and key against the sequence number before trusting the
// value length, copies the value into dst when wantValue is set and
// re-validates at the end. Every validation is preceded by readFence, which
// keeps the plain loads of entry memory before the validating load on
// weakly ordered processors such as arm64. Memory it reads is never unmapped while the
// store is open (see enterRead and docs/DESIGN.md, section 8).
func (sh *shard) readOptimistic(h uint64, key, dst []byte, wantValue bool) ([]byte, int) {
	seq := sh.seq.Load()
	for spins := 0; seq&1 != 0; spins++ {
		// A writer is inside its (short) critical section: wait for it
		// with plain loads instead of failing the attempt immediately.
		if spins == writerWaitSpins {
			return dst, optRetry
		}
		seq = sh.seq.Load()
	}
	t := sh.tab.Load()
	mask := t.mask
	i := h & mask
	for n := uint64(0); n <= mask; n++ {
		sp := unsafe.Add(t.slots, i*slotSize)
		hv := atomic.LoadUint64((*uint64)(sp))
		if hv == h {
			ref := arena.Ref(atomic.LoadUint64((*uint64)(unsafe.Add(sp, 8))))
			p, ok := sh.a.TryPointer(ref, entryHeaderSize)
			if !ok {
				return dst, optRetry
			}
			// Read every header field exactly once: the memory may change
			// under us, so all later accesses must be derived from these
			// locals and stay inside the bounds-checked range.
			kl := int(*(*uint16)(p))
			fl := *(*uint16)(unsafe.Add(p, 2))
			vl := int(*(*uint32)(unsafe.Add(p, 4)))
			hl := entryHeaderSize + int(fl&flagTTL)*ttlFieldSize
			if _, ok := sh.a.TryPointer(ref, hl+kl+vl); !ok {
				return dst, optRetry
			}
			if kl == len(key) && string(unsafe.Slice((*byte)(unsafe.Add(p, hl)), kl)) == string(key) {
				var exp int64
				if fl&flagTTL != 0 {
					exp = *(*int64)(unsafe.Add(p, entryHeaderSize))
				}
				readFence()
				if sh.seq.Load() != seq { // header and key were consistent?
					return dst, optRetry
				}
				if exp != 0 && nowNano() >= exp {
					return dst, optMiss
				}
				if wantValue {
					if cap(dst) < vl {
						dst = make([]byte, 0, vl)
					}
					dst = append(dst[:0], unsafe.Slice((*byte)(unsafe.Add(p, hl+kl)), vl)...)
					readFence()
					if sh.seq.Load() != seq {
						return dst, optRetry
					}
				}
				return dst, optHit
			}
		} else if hv == hashEmpty {
			readFence()
			if sh.seq.Load() != seq {
				return dst, optRetry
			}
			return dst, optMiss
		}
		i = (i + 1) & mask
	}
	return dst, optRetry
}

// lookup is the common read path of GetInto, Get, GetString and Has: up to
// optimisticTries lock-free attempts, then the locked slow path.
func (s *Store) lookup(key, dst []byte, wantValue bool) ([]byte, bool) {
	h := s.hash(key)
	sh := s.shardFor(h)
	if lockFreeReads {
		if c := s.enterRead(); c != nil {
			for try := 0; try < optimisticTries; try++ {
				out, st := sh.readOptimistic(h, key, dst, wantValue)
				if st != optRetry {
					c.Add(-1)
					if st == optHit {
						return out, true
					}
					return out[:0], false
				}
				dst = out
			}
			c.Add(-1) // leave before blocking on the lock (Close waits for us)
		}
	}
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return dst[:0], false
	}
	_, _, e, found := sh.find(h, key)
	if !found || e.expired(nowNano()) {
		sh.mu.Unlock()
		return dst[:0], false
	}
	if wantValue {
		dst = append(dst[:0], e.value()...)
	}
	sh.mu.Unlock()
	return dst, true
}
