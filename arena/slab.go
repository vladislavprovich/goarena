package arena

import (
	"math/bits"
	"unsafe"
)

// Size-class parameters of [Slab].
const (
	// MinClassSize is the smallest size class (and the minimum block size:
	// a free block must hold the 8-byte free-list link).
	MinClassSize = 16

	// LinearClassLimit is the end of the linear part of the class table:
	// up to this size classes are spaced 8 bytes apart.
	LinearClassLimit = 256

	// FineClassLimit is the end of the log-linear part of the class table:
	// between LinearClassLimit and FineClassLimit every power of two is
	// split into FineSubclasses equal steps (bounding the waste by 1/32).
	FineClassLimit = 8 << 10

	// FineSubclasses is the number of classes per power of two in the
	// log-linear part.
	FineSubclasses = 32

	// MaxClassSize is the largest small size class. Larger requests take
	// the large-object path (one dedicated arena chunk per object).
	MaxClassSize = 64 << 10

	// ClassGrowth is the geometric growth factor between classes above
	// FineClassLimit (and between large-object classes).
	ClassGrowth = 1.25

	// DefaultSpanSize is the size of the spans a Slab carves small objects
	// from (see SlabConfig.SpanSize).
	DefaultSpanSize = 64 << 10

	// largeAlign is the rounding of large-object classes.
	largeAlign = 4 << 10
)

var (
	classSizes []uint32 // ascending small class sizes, multiples of 8
	// classOf8 maps ceil(n/8) to the smallest class index whose size >= n,
	// for every n in [1, MaxClassSize]. 8193 entries, one byte each.
	//
	// The table is heap-allocated on purpose: with external linking on
	// windows/amd64 (cgo + MinGW ld), a large zero-initialised global
	// array ended up overlapping the coverage-counter section of
	// "go test -cover" binaries and corrupted the coverage report (see
	// DESIGN_NOTES.md, "Toolchain notes").
	classOf8 = new([MaxClassSize/8 + 1]uint8)

	// largeSizes are the block sizes used for large objects when a Slab
	// retains large blocks (SlabConfig.RetainLarge): geometric growth by
	// 1.25 from 64 KiB, rounded up to 4 KiB, up to MaxAlloc.
	largeSizes []uint32
)

// nextClass returns the class that follows size s in the geometric part.
func nextClass(s, align uint32) uint32 {
	next := uint32(float64(s) * ClassGrowth)
	next = (next + align - 1) &^ (align - 1)
	if next < s+align {
		next = s + align
	}
	return next
}

func init() {
	// Three-part table (v0.3.0):
	//   16 B ... 256 B   8-byte steps (absolute waste < 8 B);
	//   256 B ... 8 KiB  32 classes per power of two, i.e. steps of
	//                    8, 16, 32, 64, 128 B (relative waste < 1/32);
	//   8 KiB ... 64 KiB growth factor 1.25 rounded up to 8 B.
	for s := uint32(MinClassSize); ; {
		classSizes = append(classSizes, s)
		if s >= MaxClassSize {
			break
		}
		switch {
		case s < LinearClassLimit:
			s += 8
		case s < FineClassLimit:
			p := uint32(1) << (bits.Len32(s) - 1) // power of two <= s
			s += p / FineSubclasses
		default:
			s = nextClass(s, 8)
		}
		if s > MaxClassSize {
			s = MaxClassSize
		}
	}
	if len(classSizes) > 255 {
		panic("arena: too many size classes")
	}
	c := 0
	for i := range classOf8 {
		n := uint32(i) * 8
		for classSizes[c] < n {
			c++
		}
		classOf8[i] = uint8(c)
	}
	for s := uint32(MaxClassSize); s < MaxAlloc; {
		s = nextClass(s, largeAlign)
		if s > MaxAlloc {
			s = MaxAlloc
		}
		largeSizes = append(largeSizes, s)
	}
}

// SizeClasses returns a copy of the small size-class table (bytes,
// ascending).
func SizeClasses() []int {
	out := make([]int, len(classSizes))
	for i, s := range classSizes {
		out[i] = int(s)
	}
	return out
}

// classIndex returns the class for 1 <= n <= MaxClassSize.
func classIndex(n int) int { return int(classOf8[(n+7)>>3]) }

// largeIndex returns the large class for MaxClassSize < n <= MaxAlloc.
func largeIndex(n int) int {
	lo, hi := 0, len(largeSizes)-1
	for lo < hi { // binary search over ~47 entries
		mid := (lo + hi) / 2
		if int(largeSizes[mid]) >= n {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// ClassSize returns the number of bytes a Slab with the default
// configuration reserves for an n-byte request: the size of its class, or
// n rounded up to 8 for large objects. It returns 0 for n <= 0. See
// [Slab.BlockSize] for slabs that retain large blocks.
func ClassSize(n int) int {
	switch {
	case n <= 0:
		return 0
	case n > MaxClassSize:
		return int(alignUp(uintptr(n)))
	default:
		return int(classSizes[classIndex(n)])
	}
}

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

// region is a block of arena memory donated to a Slab for reuse.
type region struct {
	ref  Ref
	size uint32
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

// popFree pops the head of a free list; its first word links to the next.
func (s *Slab) popFree(cs *classState, n int) Ref {
	ref := cs.free
	cs.free = Ref(*(*uint64)(s.a.resolve(ref, 8)))
	cs.freeCount--
	cs.inUse++
	cs.requested += int64(n)
	return ref
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

// refill makes a new current span: a donated region if one is large
// enough, otherwise a fresh span from the arena.
func (s *Slab) refill(size uint32) error {
	for len(s.donated) > 0 {
		r := s.donated[len(s.donated)-1]
		s.donated = s.donated[:len(s.donated)-1]
		if r.size >= size {
			s.spanWaste += int64(s.spanLeft)
			s.span, s.spanLeft = r.ref, r.size
			return nil
		}
		s.spanWaste += int64(r.size) // too small for this class: unusable
	}
	r, err := s.a.Alloc(int(s.spanSize))
	if err != nil {
		return err
	}
	s.spanWaste += int64(s.spanLeft)
	s.span, s.spanLeft = r, s.spanSize
	s.spans++
	s.spanBytes += int64(s.spanSize)
	return nil
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

// Free returns a block to the slab. n must be the size passed to the Alloc
// that returned ref (it selects the size class). Freeing Nil is a no-op.
// Freeing a block twice corrupts the free list, exactly like double free in
// C; the store package never does it.
func (s *Slab) Free(ref Ref, n int) {
	if ref == Nil {
		return
	}
	if n <= 0 || n > MaxAlloc {
		panic("arena: Slab.Free with invalid size")
	}
	if n > MaxClassSize {
		size := s.BlockSize(n)
		s.largeCount--
		s.largeBytes -= int64(size)
		s.largeReq -= int64(n)
		if !s.retainLarge {
			s.a.FreeDedicated(ref)
			return
		}
		s.pushFree(&s.large[largeIndex(n)], ref, size, n)
		return
	}
	ci := classIndex(n)
	s.pushFree(&s.classes[ci], ref, int(classSizes[ci]), n)
}

func (s *Slab) pushFree(cs *classState, ref Ref, size, n int) {
	*(*uint64)(s.a.resolve(ref, size)) = uint64(cs.free)
	cs.free = ref
	cs.freeCount++
	cs.inUse--
	cs.requested -= int64(n)
}

// Donate hands an n-byte block of arena memory that the caller no longer
// needs (for example an outgrown hash table) to the slab, which will carve
// future small blocks from it. The memory must belong to the slab's arena
// and must not be used by the caller afterwards. Blocks smaller than the
// smallest class are ignored.
func (s *Slab) Donate(ref Ref, n int) {
	if ref == Nil || n < MinClassSize {
		return
	}
	s.a.resolve(ref, n) // bounds check
	for n > 0 {         // regions are limited to 4 GiB - 8 by the uint32 size
		part := n
		if part > 1<<31 {
			part = 1 << 31
		}
		s.donated = append(s.donated, region{ref: ref, size: uint32(part)})
		s.donatedBytes += int64(part)
		ref += Ref(part)
		n -= part
	}
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

// ClassStats describes one size class of a Slab.
type ClassStats struct {
	Size           int    // block size of the class
	InUse          uint64 // blocks currently allocated
	Free           uint64 // blocks on the free list
	Carved         uint64 // blocks ever carved for this class
	RequestedBytes int64  // sum of requested sizes of in-use blocks
}

// SlabStats summarises the memory usage of a Slab.
type SlabStats struct {
	Classes        []ClassStats // only small classes that were ever used
	InUseBytes     int64        // bytes of in-use blocks (class-rounded)
	RequestedBytes int64        // bytes requested by in-use blocks
	FreeListBytes  int64        // bytes parked on free lists (incl. large)
	SpanBytes      int64        // bytes obtained from the arena as spans
	DonatedBytes   int64        // bytes received through Donate
	DirectBytes    int64        // bytes of mid-size classes obtained directly
	SpanWaste      int64        // abandoned span tails and unusable donations
	LargeObjects   int64        // live large objects
	LargeBytes     int64        // bytes of live large objects
	Spans          uint64       // spans requested from the arena

	// InternalFragmentation is 1 - RequestedBytes/InUseBytes: the share of
	// in-use memory lost to size-class rounding (0 when nothing is in use).
	InternalFragmentation float64
}

// Stats returns a snapshot of the slab statistics.
func (s *Slab) Stats() SlabStats {
	st := SlabStats{
		SpanBytes:    s.spanBytes,
		DonatedBytes: s.donatedBytes,
		DirectBytes:  s.directSz,
		SpanWaste:    s.spanWaste,
		LargeObjects: s.largeCount,
		LargeBytes:   s.largeBytes,
		Spans:        s.spans,
	}
	for i := range s.classes {
		cs := &s.classes[i]
		if cs.carved == 0 {
			continue
		}
		sz := int64(classSizes[i])
		st.Classes = append(st.Classes, ClassStats{
			Size: int(sz), InUse: cs.inUse, Free: cs.freeCount,
			Carved: cs.carved, RequestedBytes: cs.requested,
		})
		st.InUseBytes += int64(cs.inUse) * sz
		st.RequestedBytes += cs.requested
		st.FreeListBytes += int64(cs.freeCount) * sz
	}
	for i := range s.large {
		st.FreeListBytes += int64(s.large[i].freeCount) * int64(largeSizes[i])
	}
	st.InUseBytes += s.largeBytes
	st.RequestedBytes += s.largeReq
	if st.InUseBytes > 0 {
		st.InternalFragmentation = 1 - float64(st.RequestedBytes)/float64(st.InUseBytes)
	}
	return st
}
