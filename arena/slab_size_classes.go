package arena

import "math/bits"

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
	// docs/DESIGN.md, section 13.1).
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
