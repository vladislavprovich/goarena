package cmem

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// This file holds what both allocator back ends share: the block header,
// the size limit and the process-wide counters. The back end itself is
// c_allocator.go (cgo builds: C malloc/calloc/free) or
// go_heap_allocator.go (builds without cgo: pointer-free Go-heap blocks).

// HeaderSize is the size of the hidden header that precedes every block.
const HeaderSize = 16

// magicLive marks the header of a live block ("goarena!" in ASCII).
const magicLive uint64 = 0x676f6172656e6121

// magicFreed is written into the header right before the block is released,
// which turns the most common double-free pattern into a deterministic panic
// instead of heap corruption (best effort only: once the block is released,
// the header memory may be reused by the allocator).
const magicFreed uint64 = 0x6465657266726565

// MaxSize is the largest block that can be requested. It keeps size
// arithmetic far away from overflow on every supported platform.
const MaxSize = 1 << 40

// counters are padded to separate cache lines because they are updated by
// many goroutines at once.
type paddedInt64 struct {
	v atomic.Int64
	_ [56]byte
}

var (
	cgoCalls    paddedInt64 // number of Go->C transitions made by this package
	liveBlocks  paddedInt64 // blocks allocated and not yet freed
	liveBytes   paddedInt64 // user bytes in live blocks (headers excluded)
	totalAllocs paddedInt64 // blocks ever allocated
	totalFrees  paddedInt64 // blocks ever freed
	peakBytes   paddedInt64 // high-water mark of liveBytes
)

type header struct {
	size  uint64
	magic uint64
}

// initBlock writes the header of a fresh block of n user bytes starting at
// raw, updates the counters and returns the user pointer.
func initBlock(raw unsafe.Pointer, n uintptr) unsafe.Pointer {
	h := (*header)(raw)
	h.size = uint64(n)
	h.magic = magicLive
	liveBlocks.v.Add(1)
	totalAllocs.v.Add(1)
	cur := liveBytes.v.Add(int64(n))
	for {
		peak := peakBytes.v.Load()
		if cur <= peak || peakBytes.v.CompareAndSwap(peak, cur) {
			break
		}
	}
	return unsafe.Add(raw, HeaderSize)
}

// retireBlock validates the header of the live block p, marks it freed,
// updates the counters and returns the start of the underlying allocation.
// It panics on a foreign pointer or a (detectable) double free.
func retireBlock(p unsafe.Pointer) unsafe.Pointer {
	raw := unsafe.Add(p, -HeaderSize)
	h := (*header)(raw)
	if h.magic != magicLive {
		if h.magic == magicFreed {
			panic("cmem: double free")
		}
		panic(fmt.Sprintf("cmem: Free of pointer %p not allocated by cmem", p))
	}
	n := h.size
	h.magic = magicFreed
	liveBlocks.v.Add(-1)
	liveBytes.v.Add(-int64(n))
	totalFrees.v.Add(1)
	return raw
}

// SizeOf returns the user size of a live block allocated by this package.
func SizeOf(p unsafe.Pointer) uintptr {
	if p == nil {
		return 0
	}
	h := (*header)(unsafe.Add(p, -HeaderSize))
	if h.magic != magicLive {
		panic("cmem: SizeOf on a block that is not live")
	}
	return uintptr(h.size)
}

// Stats is a snapshot of the process-wide counters of this package.
type Stats struct {
	CgoCalls    int64 // Go->C transitions performed by cmem (always 0 without cgo)
	LiveBlocks  int64 // blocks currently allocated
	LiveBytes   int64 // user bytes currently allocated (headers excluded)
	PeakBytes   int64 // maximum value LiveBytes has reached
	TotalAllocs int64 // blocks allocated since process start
	TotalFrees  int64 // blocks freed since process start
}

// ReadStats returns the current values of the counters. The individual
// fields are read atomically but not as one consistent snapshot.
func ReadStats() Stats {
	return Stats{
		CgoCalls:    cgoCalls.v.Load(),
		LiveBlocks:  liveBlocks.v.Load(),
		LiveBytes:   liveBytes.v.Load(),
		PeakBytes:   peakBytes.v.Load(),
		TotalAllocs: totalAllocs.v.Load(),
		TotalFrees:  totalFrees.v.Load(),
	}
}
