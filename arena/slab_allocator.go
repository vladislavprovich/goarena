package arena

import "unsafe"

// SlabConfig configures a [Slab]. The zero value is valid.
type SlabConfig struct {
	// SpanSize is the number of bytes a Slab requests from its arena at
	// once and then carves into blocks of small classes. Batching keeps
	// the arena mutex off the hot path when many Slabs share one Arena.
	// Classes larger than SpanSize/4 are allocated from the arena
	// directly. Zero means DefaultSpanSize; the value is clamped to the
	// arena chunk size.
	SpanSize int

	// RetainLarge keeps freed large objects (> MaxClassSize) on free
	// lists of geometric large classes instead of returning them to C.
	// No arena memory is then unmapped before the arena is released,
	// which the store's lock-free read path relies on. Large requests are
	// rounded up to the large class (at most 25 % + 4 KiB).
	RetainLarge bool
}

type classState struct {
	free      Ref    // head of the intrusive free list
	inUse     uint64 // blocks handed out and not freed
	freeCount uint64 // blocks on the free list
	carved    uint64 // blocks ever carved from spans or the arena
	requested int64  // sum of requested sizes of in-use blocks
}

// Slab is a size-class allocator on top of an [Arena] that supports freeing
// individual objects.
//
// Requests are rounded up to one of 201 size classes: 8-byte steps from
// 16 B to 256 B, 32 classes per power of two from 256 B to 8 KiB, then a
// growth factor of 1.25 (rounded to 8 B) up to 64 KiB. Every class has an intrusive LIFO free list: the first 8 bytes of
// a freed block hold the Ref of the next free block, so the free lists cost
// no extra memory and contain no Go pointers. New blocks are carved from
// spans obtained from the arena (or donated to the slab with
// [Slab.Donate]). Requests above MaxClassSize are served by a dedicated
// arena chunk each; they are returned to C on Free unless
// SlabConfig.RetainLarge is set.
//
// A Slab is not safe for concurrent use; protect it with a mutex (the store
// package uses one Slab per shard). Several Slabs may share one Arena.
type Slab struct {
	a           *Arena
	spanSize    uint32
	retainLarge bool
	span        Ref    // next free byte of the current span
	spanLeft    uint32 // bytes left in the current span

	classes []classState
	large   []classState // large classes (RetainLarge only)
	donated []region     // donated regions waiting to become spans

	spans        uint64 // spans requested from the arena
	spanBytes    int64  // bytes requested as spans
	donatedBytes int64  // bytes received through Donate
	directSz     int64  // bytes of mid-size blocks allocated directly
	spanWaste    int64  // span tails abandoned when a span ran out
	largeCount   int64  // live large objects
	largeBytes   int64  // bytes of live large objects (block size)
	largeReq     int64  // requested bytes of live large objects
}

// NewSlab creates a Slab that obtains memory from a.
func NewSlab(a *Arena, cfg SlabConfig) *Slab {
	ss := cfg.SpanSize
	if ss <= 0 {
		ss = DefaultSpanSize
	}
	if ss > a.ChunkSize() {
		ss = a.ChunkSize()
	}
	if ss < MinClassSize {
		ss = MinClassSize
	}
	ss = int(alignUp(uintptr(ss)))
	s := &Slab{a: a, spanSize: uint32(ss), retainLarge: cfg.RetainLarge,
		classes: make([]classState, len(classSizes))}
	if cfg.RetainLarge {
		s.large = make([]classState, len(largeSizes))
	}
	return s
}

// Arena returns the arena the slab allocates from.
func (s *Slab) Arena() *Arena { return s.a }

// BlockSize returns the number of bytes this slab reserves for an n-byte
// request (0 for n <= 0). Two requests share a block size exactly when a
// block allocated for one can hold the other.
func (s *Slab) BlockSize(n int) int {
	if n > MaxClassSize && n <= MaxAlloc && s.retainLarge {
		return int(largeSizes[largeIndex(n)])
	}
	return ClassSize(n)
}

// Alloc returns a block of at least n bytes. The memory is not zeroed.
func (s *Slab) Alloc(n int) (Ref, error) {
	if n <= 0 || n > MaxAlloc {
		return Nil, ErrInvalidSize
	}
	if n > MaxClassSize {
		return s.allocLarge(n)
	}
	ci := classIndex(n)
	cs := &s.classes[ci]
	if cs.free != Nil {
		return s.popFree(cs, n), nil
	}
	size := classSizes[ci]
	var ref Ref
	if size > s.spanSize/4 {
		r, err := s.a.Alloc(int(size))
		if err != nil {
			return Nil, err
		}
		s.directSz += int64(size)
		ref = r
	} else {
		if s.spanLeft < size {
			if err := s.refill(size); err != nil {
				return Nil, err
			}
		}
		ref = s.span
		s.span += Ref(size)
		s.spanLeft -= size
	}
	cs.carved++
	cs.inUse++
	cs.requested += int64(n)
	return ref, nil
}

func (s *Slab) allocLarge(n int) (Ref, error) {
	size := int(alignUp(uintptr(n)))
	var cs *classState
	if s.retainLarge {
		cs = &s.large[largeIndex(n)]
		size = int(largeSizes[largeIndex(n)])
		if cs.free != Nil {
			ref := s.popFree(cs, n)
			s.largeCount++
			s.largeBytes += int64(size)
			s.largeReq += int64(n)
			return ref, nil
		}
	}
	ref, err := s.a.AllocDedicated(size)
	if err != nil {
		return Nil, err
	}
	if cs != nil {
		cs.carved++
		cs.inUse++
		cs.requested += int64(n)
	}
	s.largeCount++
	s.largeBytes += int64(size)
	s.largeReq += int64(n)
	return ref, nil
}

// Bytes returns an n-byte view of the block at ref (see [Arena.Bytes]).
func (s *Slab) Bytes(ref Ref, n int) []byte { return s.a.Bytes(ref, n) }

// Pointer returns the address of the block at ref (see [Arena.Pointer]).
func (s *Slab) Pointer(ref Ref) unsafe.Pointer { return s.a.Pointer(ref) }

// Reset forgets every block and free list. It must be called together with
// [Arena.Reset] of the underlying arena (which invalidates all references);
// large objects are released by the arena reset as well.
func (s *Slab) Reset() {
	for i := range s.classes {
		s.classes[i] = classState{}
	}
	for i := range s.large {
		s.large[i] = classState{}
	}
	s.donated = s.donated[:0]
	s.span, s.spanLeft = Nil, 0
	s.spans, s.spanBytes, s.directSz, s.spanWaste, s.donatedBytes = 0, 0, 0, 0, 0
	s.largeCount, s.largeBytes, s.largeReq = 0, 0, 0
}
