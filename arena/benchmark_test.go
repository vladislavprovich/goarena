package arena

import "testing"

var sinkBytes []byte

// resetEvery bounds the memory of the bump-allocation benchmarks: the arena
// is Reset (chunks kept for reuse) after this many allocations, i.e. every
// 128 MiB, so b.N in the hundreds of millions does not need tens of GiB.
const resetEvery = 1 << 20

// BenchmarkArenaAlloc128 measures a 128-byte bump allocation (the arena
// grows by 64 MiB chunks; the cost of chunk allocation and of the periodic
// Reset is amortised).
func BenchmarkArenaAlloc128(b *testing.B) {
	a := MustNew(Config{})
	defer a.Release()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%resetEvery == resetEvery-1 {
			a.Reset()
		}
		if _, err := a.Alloc(128); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkArenaAllocBytes128 adds the Bytes view to the allocation, i.e.
// the full cost of obtaining a usable []byte.
func BenchmarkArenaAllocBytes128(b *testing.B) {
	a := MustNew(Config{})
	defer a.Release()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%resetEvery == resetEvery-1 {
			a.Reset()
		}
		r, _ := a.Alloc(128)
		sinkBytes = a.Bytes(r, 128)
	}
}

// BenchmarkMakeBytes128 is the Go-heap counterpart: make([]byte, 128)
// escaping to the heap (includes the amortised GC cost).
func BenchmarkMakeBytes128(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBytes = make([]byte, 128)
	}
}

// BenchmarkSlabAllocFree128 measures an Alloc+Free pair of a 128-byte
// block (free-list pop and push).
func BenchmarkSlabAllocFree128(b *testing.B) {
	a := MustNew(Config{})
	defer a.Release()
	s := NewSlab(a, SlabConfig{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := s.Alloc(128)
		if err != nil {
			b.Fatal(err)
		}
		s.Free(r, 128)
	}
}

// BenchmarkSlabAllocFreeMixed cycles through sizes 16..4096 B with a
// window of 1024 live blocks.
func BenchmarkSlabAllocFreeMixed(b *testing.B) {
	a := MustNew(Config{})
	defer a.Release()
	s := NewSlab(a, SlabConfig{})
	const window = 1024
	refs := make([]Ref, window)
	sizes := make([]int, window)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % window
		if refs[j] != Nil {
			s.Free(refs[j], sizes[j])
		}
		n := 16 + (i*131)%4080
		r, err := s.Alloc(n)
		if err != nil {
			b.Fatal(err)
		}
		refs[j], sizes[j] = r, n
	}
}

type pt struct{ X, Y, Z float64 }

var sinkPt *pt

// BenchmarkNewValue measures the typed helper (reflection check is cached).
func BenchmarkNewValue(b *testing.B) {
	a := MustNew(Config{})
	defer a.Release()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%resetEvery == resetEvery-1 {
			a.Reset()
		}
		p, err := NewValue[pt](a)
		if err != nil {
			b.Fatal(err)
		}
		sinkPt = p
	}
}

// BenchmarkGoNew is the Go-heap counterpart of BenchmarkNewValue.
func BenchmarkGoNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkPt = new(pt)
	}
}
