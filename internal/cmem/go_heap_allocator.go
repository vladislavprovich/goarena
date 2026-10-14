//go:build !cgo

package cmem

import (
	"fmt"
	"sync"
	"unsafe"
)

// OffHeap reports whether blocks come from the C allocator, outside of the
// Go heap. It is false in builds without cgo (this file): blocks are then
// pointer-free []byte allocations in the Go heap.
//
// The garbage collector does not scan pointer-free memory, so the store's
// data still adds (almost) nothing to the mark work of a GC cycle. But the
// bytes are part of the Go heap: they count towards the heap goal (GOGC)
// and GOMEMLIMIT, and they show up in heap profiles. The benefits of
// goarena on heap size and GC pacing are therefore lost in this mode; it
// exists so that programs build and behave correctly where cgo is not
// available (CGO_ENABLED=0, cross-compilation without a C toolchain).
const OffHeap = false

// blocks keeps every live block reachable until Free. Without it the
// garbage collector could reclaim a block while the arena still refers to
// it through an integer Ref. The Go collector never moves heap objects, so
// the addresses handed out stay valid for the lifetime of the block.
var blocks = struct {
	sync.Mutex
	m map[uintptr][]byte // user pointer -> backing allocation
}{m: make(map[uintptr][]byte)}

// Malloc allocates n bytes and returns a pointer to them. Without cgo the
// block is a pointer-free []byte in the Go heap, kept alive until [Free];
// the memory is zeroed, as every Go allocation. The pointer is aligned to
// 16 bytes. Malloc returns nil when n is zero or larger than [MaxSize].
//
// Unlike the C allocator, the Go runtime does not report a failed
// allocation: it terminates the process ("out of memory"). Limits such as
// arena.Config.MaxBytes are checked before Malloc is called and keep
// working.
func Malloc(n uintptr) unsafe.Pointer {
	if n == 0 || n > MaxSize {
		return nil
	}
	// Go guarantees 8-byte alignment; 8 spare bytes let the header start
	// on a 16-byte boundary, which keeps the user pointer 16-byte aligned
	// as with malloc.
	buf := make([]byte, n+HeaderSize+8)
	raw := unsafe.Pointer(unsafe.SliceData(buf))
	if uintptr(raw)%16 != 0 {
		raw = unsafe.Add(raw, 8)
	}
	p := initBlock(raw, n)
	blocks.Lock()
	blocks.m[uintptr(p)] = buf
	blocks.Unlock()
	return p
}

// Calloc is like [Malloc]; Go memory is always zeroed.
func Calloc(n uintptr) unsafe.Pointer {
	return Malloc(n)
}

// Free releases a block obtained from [Malloc] or [Calloc]: the block is
// dropped from the registry and reclaimed by the garbage collector once
// nothing refers to it any more. Free(nil) is a no-op. Passing any other
// pointer, or freeing the same block twice, is a programming error; Free
// panics when it detects it.
func Free(p unsafe.Pointer) {
	if p == nil {
		return
	}
	blocks.Lock()
	_, ok := blocks.m[uintptr(p)]
	if ok {
		delete(blocks.m, uintptr(p))
	}
	blocks.Unlock()
	if !ok {
		// The memory of a block freed earlier may already have been
		// reclaimed by the garbage collector, so its header is not read.
		panic(fmt.Sprintf("cmem: Free of pointer %p that is not a live block (double free or foreign pointer)", p))
	}
	retireBlock(p)
}

// Memclr zeroes n bytes starting at p.
func Memclr(p unsafe.Pointer, n uintptr) {
	if p == nil || n == 0 {
		return
	}
	clear(unsafe.Slice((*byte)(p), n))
}

// Noop does nothing: without cgo there is no Go/C transition to measure.
// It is not counted in Stats.CgoCalls.
func Noop() {}
