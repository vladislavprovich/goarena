package cmem

import (
	"sync"
	"testing"
	"unsafe"
)

func TestMallocFreeAccounting(t *testing.T) {
	before := ReadStats()
	p := Malloc(1000)
	if p == nil {
		t.Fatal("Malloc returned nil")
	}
	if uintptr(p)%16 != 0 {
		t.Fatalf("pointer %p is not 16-byte aligned", p)
	}
	if SizeOf(p) != 1000 {
		t.Fatalf("SizeOf = %d", SizeOf(p))
	}
	b := unsafe.Slice((*byte)(p), 1000)
	for i := range b {
		b[i] = byte(i)
	}
	mid := ReadStats()
	if mid.LiveBlocks != before.LiveBlocks+1 || mid.LiveBytes != before.LiveBytes+1000 {
		t.Fatalf("accounting after Malloc: %+v -> %+v", before, mid)
	}
	if mid.PeakBytes < mid.LiveBytes {
		t.Fatal("peak below live")
	}
	Free(p)
	after := ReadStats()
	if after.LiveBlocks != before.LiveBlocks || after.LiveBytes != before.LiveBytes {
		t.Fatalf("leak: %+v -> %+v", before, after)
	}
	if after.CgoCalls < before.CgoCalls+2 || after.TotalFrees != before.TotalFrees+1 {
		t.Fatalf("cgo calls not counted: %+v", after)
	}
}

func TestCallocZeroes(t *testing.T) {
	p := Calloc(4096)
	defer Free(p)
	for i, c := range unsafe.Slice((*byte)(p), 4096) {
		if c != 0 {
			t.Fatalf("byte %d = %d", i, c)
		}
	}
	q := Malloc(64)
	defer Free(q)
	b := unsafe.Slice((*byte)(q), 64)
	for i := range b {
		b[i] = 0xff
	}
	Memclr(q, 64)
	for _, c := range b {
		if c != 0 {
			t.Fatal("Memclr did not clear")
		}
	}
	Memclr(nil, 10) // no-op
}

// TestMallocFailureReturnsNil checks that an allocation the C allocator
// cannot satisfy is reported as nil instead of crashing the process (cgo's
// C.malloc would abort).
func TestMallocFailureReturnsNil(t *testing.T) {
	before := ReadStats()
	if p := Malloc(MaxSize); p != nil { // 1 TiB
		Free(p)
		t.Skip("the C allocator granted 1 TiB; cannot test the failure path here")
	}
	if p := Calloc(MaxSize); p != nil {
		Free(p)
		t.Skip("calloc granted 1 TiB")
	}
	after := ReadStats()
	if after.LiveBlocks != before.LiveBlocks || after.CgoCalls != before.CgoCalls+2 {
		t.Fatalf("failed allocations must not be counted as live: %+v -> %+v", before, after)
	}
}

func TestEdgeCases(t *testing.T) {
	if Malloc(0) != nil || Calloc(0) != nil || Malloc(MaxSize+1) != nil {
		t.Fatal("expected nil for invalid sizes")
	}
	Free(nil)
	if SizeOf(nil) != 0 {
		t.Fatal("SizeOf(nil)")
	}
}

func TestInvalidFreePanics(t *testing.T) {
	buf := make([]uint64, 8) // a Go block with a zero "header"
	p := unsafe.Pointer(&buf[4])
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for foreign pointer")
			}
		}()
		Free(p)
	}()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for SizeOf on foreign pointer")
			}
		}()
		SizeOf(p)
	}()
	buf[3] = magicFreed // simulate the header of a freed block
	func() {
		defer func() {
			if r := recover(); r != "cmem: double free" {
				t.Fatalf("expected double free panic, got %v", r)
			}
		}()
		Free(p)
	}()
}

func TestConcurrentCounters(t *testing.T) {
	before := ReadStats()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				Free(Malloc(uintptr(16 + i)))
				Noop()
			}
		}()
	}
	wg.Wait()
	after := ReadStats()
	if after.LiveBlocks != before.LiveBlocks || after.LiveBytes != before.LiveBytes {
		t.Fatalf("leak under concurrency: %+v -> %+v", before, after)
	}
	if after.CgoCalls-before.CgoCalls != 8*1000*3 {
		t.Fatalf("cgo calls = %d", after.CgoCalls-before.CgoCalls)
	}
}

//go:noinline
func goNoop() {}

// BenchmarkGoCall measures a call to an empty, non-inlined Go function.
func BenchmarkGoCall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		goNoop()
	}
}

// BenchmarkCgoCall measures a call to an empty C function through cgo.
func BenchmarkCgoCall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Noop()
	}
}

// BenchmarkCgoCallParallel measures concurrent cgo calls from all Ps.
func BenchmarkCgoCallParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Noop()
		}
	})
}

// BenchmarkMallocFree128 measures one C malloc+free pair of 128 bytes
// (two cgo transitions).
func BenchmarkMallocFree128(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Free(Malloc(128))
	}
}

var sink []byte

// BenchmarkGoMake128 is the Go-heap counterpart of BenchmarkMallocFree128.
func BenchmarkGoMake128(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = make([]byte, 128)
	}
}
