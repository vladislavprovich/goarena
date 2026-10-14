//go:build cgo

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

import "unsafe"

// OffHeap reports whether blocks come from the C allocator, outside of the
// Go heap. It is true in builds with cgo (this file) and false in builds
// without cgo, where go_heap_allocator.go provides a pure-Go fallback.
const OffHeap = true

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
	return initBlock(raw, n)
}

// Free releases a block obtained from [Malloc] or [Calloc]. Free(nil) is a
// no-op. Passing any other pointer, or freeing the same block twice, is a
// programming error; Free panics when it detects it.
func Free(p unsafe.Pointer) {
	if p == nil {
		return
	}
	raw := retireBlock(p)
	C.free(raw)
	cgoCalls.v.Add(1)
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
