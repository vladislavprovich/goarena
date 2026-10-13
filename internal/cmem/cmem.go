package cmem

/*
#include <stdlib.h>
#include <string.h>

// goarena_noop is an empty C function used to measure the fixed cost of a
// single Go -> C -> Go transition.
static void goarena_noop(void) {}

// goarena_malloc and goarena_calloc call the C allocator directly. cgo's
// own C.malloc wrapper never returns NULL - it crashes the whole process
// when malloc fails - so going through these wrappers is what lets Malloc
// report an allocation failure (and the arena return ErrOutOfMemory).
static void *goarena_malloc(size_t n) { return malloc(n); }
static void *goarena_calloc(size_t n) { return calloc(1, n); }
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// HeaderSize is the size of the hidden header that precedes every block.
const HeaderSize = 16

// magicLive marks the header of a live block ("goarena!" in ASCII).
const magicLive uint64 = 0x676f6172656e6121

// magicFreed is written into the header right before the block is released,
// which turns the most common double-free pattern into a deterministic panic
// instead of heap corruption (best effort only: once C free returns, the
// header memory may be reused by the C allocator).
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

// Malloc allocates n bytes of uninitialised memory outside of the Go heap and
// returns a pointer to it. The pointer is aligned to 16 bytes. Malloc returns
// nil when n is zero, larger than [MaxSize], or when the C allocator fails.
//
// The returned memory must be released with [Free].
func Malloc(n uintptr) unsafe.Pointer {
	return alloc(n, false)
}

// Calloc is like [Malloc] but the returned memory is zeroed.
func Calloc(n uintptr) unsafe.Pointer {
	return alloc(n, true)
}

func alloc(n uintptr, zero bool) unsafe.Pointer {
	if n == 0 || n > MaxSize {
		return nil
	}
	var raw unsafe.Pointer
	if zero {
		raw = C.goarena_calloc(C.size_t(n + HeaderSize))
	} else {
		raw = C.goarena_malloc(C.size_t(n + HeaderSize))
	}
	cgoCalls.v.Add(1)
	if raw == nil {
		return nil
	}
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

// Free releases a block obtained from [Malloc] or [Calloc]. Free(nil) is a
// no-op. Passing any other pointer, or freeing the same block twice, is a
// programming error; Free panics when it detects it.
func Free(p unsafe.Pointer) {
	if p == nil {
		return
	}
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
	C.free(raw)
	cgoCalls.v.Add(1)
	liveBlocks.v.Add(-1)
	liveBytes.v.Add(-int64(n))
	totalFrees.v.Add(1)
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

// Memclr zeroes n bytes starting at p using C memset. It is intended for
// large blocks, where one cgo transition is cheaper than a Go loop.
func Memclr(p unsafe.Pointer, n uintptr) {
	if p == nil || n == 0 {
		return
	}
	C.memset(p, 0, C.size_t(n))
	cgoCalls.v.Add(1)
}

// Noop performs one call to an empty C function. It exists only so that
// benchmarks can measure the bare cost of a cgo transition.
func Noop() {
	C.goarena_noop()
	cgoCalls.v.Add(1)
}

// Stats is a snapshot of the process-wide counters of this package.
type Stats struct {
	CgoCalls    int64 // Go->C transitions performed by cmem
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
