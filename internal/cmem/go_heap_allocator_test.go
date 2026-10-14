//go:build !cgo

package cmem

import (
	"runtime"
	"testing"
	"unsafe"
)

// TestGoHeapBlocksSurviveGC checks the property the pure-Go fallback
// relies on: a block referenced only through its registry entry (the test
// keeps nothing but the address as an integer, like an arena Ref) survives
// garbage collections with its address and contents, and Free drops it
// from the registry.
func TestGoHeapBlocksSurviveGC(t *testing.T) {
	const n = 1 << 20
	addrs := make([]uintptr, 8)
	for i := range addrs {
		p := Malloc(n)
		if uintptr(p)%16 != 0 {
			t.Fatalf("pointer %p is not 16-byte aligned", p)
		}
		b := unsafe.Slice((*byte)(p), n)
		for j := range b {
			b[j] = byte(i + j)
		}
		addrs[i] = uintptr(p)
	}
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	blocks.Lock()
	registered := len(blocks.m)
	blocks.Unlock()
	if registered < len(addrs) {
		t.Fatalf("registry holds %d blocks, want at least %d", registered, len(addrs))
	}
	for i, a := range addrs {
		blocks.Lock()
		buf, ok := blocks.m[a]
		blocks.Unlock()
		if !ok {
			t.Fatalf("block %d missing from the registry", i)
		}
		// Recover the pointer from the backing slice: the registry is the
		// only reference the test kept.
		p := unsafe.Pointer(&buf[a-uintptr(unsafe.Pointer(&buf[0]))])
		b := unsafe.Slice((*byte)(p), n)
		for j := 0; j < n; j += 4093 {
			if b[j] != byte(i+j) {
				t.Fatalf("block %d byte %d changed after GC", i, j)
			}
		}
		Free(p)
	}
	blocks.Lock()
	defer blocks.Unlock()
	for _, a := range addrs {
		if _, ok := blocks.m[a]; ok {
			t.Fatal("Free did not remove the block from the registry")
		}
	}
}
