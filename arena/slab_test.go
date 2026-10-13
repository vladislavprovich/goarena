package arena

import (
	"errors"
	"math/bits"
	"math/rand"
	"testing"
)

func TestSizeClassTable(t *testing.T) {
	cl := SizeClasses()
	if cl[0] != MinClassSize || cl[len(cl)-1] != MaxClassSize {
		t.Fatalf("class range %d..%d", cl[0], cl[len(cl)-1])
	}
	for i := 1; i < len(cl); i++ {
		if cl[i]%8 != 0 || cl[i] <= cl[i-1] {
			t.Fatalf("class %d = %d not increasing/aligned", i, cl[i])
		}
		if g := float64(cl[i]) / float64(cl[i-1]); g > 1.5 {
			t.Fatalf("growth %d->%d = %.2f too large", cl[i-1], cl[i], g)
		}
	}
	for n := 1; n <= MaxClassSize; n++ {
		c := ClassSize(n)
		if c < n {
			t.Fatalf("ClassSize(%d) = %d", n, c)
		}
		ci := classIndex(n)
		if ci > 0 && int(classSizes[ci-1]) >= n {
			t.Fatalf("n=%d not mapped to the smallest class", n)
		}
	}
	if ClassSize(0) != 0 || ClassSize(MaxClassSize+1) != MaxClassSize+8 {
		t.Fatal("ClassSize edge cases")
	}
	t.Logf("%d size classes: %v", len(cl), cl)
}

func TestSlabReuseAndStats(t *testing.T) {
	a := MustNew(Config{ChunkSize: 1 << 20})
	defer a.Release()
	s := NewSlab(a, SlabConfig{})
	if s.Arena() != a {
		t.Fatal("Arena()")
	}
	r1, _ := s.Alloc(100)
	r2, _ := s.Alloc(100)
	if r1 == r2 {
		t.Fatal("duplicate refs")
	}
	copy(s.Bytes(r1, 100), "hello")
	s.Free(r1, 100)
	r3, _ := s.Alloc(97) // same class -> LIFO reuse
	if r3 != r1 {
		t.Fatalf("free block not reused: %v vs %v", r3, r1)
	}
	st := s.Stats()
	if len(st.Classes) != 1 || st.Classes[0].InUse != 2 || st.Classes[0].Free != 0 {
		t.Fatalf("stats %+v", st)
	}
	if st.RequestedBytes != 197 || st.InUseBytes != int64(2*ClassSize(100)) {
		t.Fatalf("bytes %+v", st)
	}
	if st.InternalFragmentation <= 0 || st.InternalFragmentation >= 0.25 {
		t.Fatalf("fragmentation %v", st.InternalFragmentation)
	}
	s.Free(Nil, 10) // no-op
	if s.Pointer(r2) == nil {
		t.Fatal("Pointer")
	}
}

func TestSlabLargeAndDirect(t *testing.T) {
	a := MustNew(Config{ChunkSize: 1 << 20})
	defer a.Release()
	s := NewSlab(a, SlabConfig{SpanSize: 64 << 10})
	big, err := s.Alloc(100000)
	if err != nil {
		t.Fatal(err)
	}
	s.Bytes(big, 100000)[99999] = 1
	mid, err := s.Alloc(40000) // > SpanSize/4: direct from arena
	if err != nil {
		t.Fatal(err)
	}
	st := s.Stats()
	if st.LargeObjects != 1 || st.LargeBytes != 100000 || st.DirectBytes != int64(ClassSize(40000)) {
		t.Fatalf("stats %+v", st)
	}
	s.Free(big, 100000)
	s.Free(mid, 40000)
	st = s.Stats()
	if st.LargeObjects != 0 || a.Stats().DedicatedChunks != 0 {
		t.Fatalf("large object not released: %+v", st)
	}
	if _, err := s.Alloc(0); !errors.Is(err, ErrInvalidSize) {
		t.Fatal("Alloc(0)")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Free with bad size must panic")
		}
	}()
	s.Free(mid, 0)
}

func TestSlabSpanClampAndReset(t *testing.T) {
	a := MustNew(Config{ChunkSize: 8192})
	defer a.Release()
	s := NewSlab(a, SlabConfig{SpanSize: 1 << 20}) // clamped to chunk size
	if s.spanSize != 8192 {
		t.Fatalf("span size %d", s.spanSize)
	}
	for i := 0; i < 1000; i++ {
		if _, err := s.Alloc(40); err != nil {
			t.Fatal(err)
		}
	}
	if s.Stats().Spans < 2 || s.Stats().SpanWaste < 0 {
		t.Fatal("expected several spans")
	}
	s.Reset()
	a.Reset()
	if st := s.Stats(); st.InUseBytes != 0 || len(st.Classes) != 0 {
		t.Fatalf("reset: %+v", st)
	}
	if _, err := s.Alloc(40); err != nil {
		t.Fatal(err)
	}
}

func TestSlabOutOfMemory(t *testing.T) {
	a := MustNew(Config{ChunkSize: 4096, MaxBytes: 4096})
	defer a.Release()
	s := NewSlab(a, SlabConfig{SpanSize: 4096})
	var err error
	for i := 0; i < 1000 && err == nil; i++ {
		_, err = s.Alloc(64)
	}
	if !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory, got %v", err)
	}
	if _, err := s.Alloc(2000); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("direct path: %v", err)
	}
	if _, err := s.Alloc(MaxClassSize + 1); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("large path: %v", err)
	}
}

// TestSlabRandomized checks that live blocks never overlap under a random
// sequence of allocations and frees of mixed sizes.
func TestSlabRandomized(t *testing.T) {
	a := MustNew(Config{ChunkSize: 256 << 10})
	defer a.Release()
	s := NewSlab(a, SlabConfig{})
	type blk struct {
		ref Ref
		n   int
		tag byte
	}
	r := rand.New(rand.NewSource(42))
	var live []blk
	for step := 0; step < 50000; step++ {
		if len(live) > 0 && r.Intn(3) == 0 {
			i := r.Intn(len(live))
			b := live[i]
			for _, c := range s.Bytes(b.ref, b.n) {
				if c != b.tag {
					t.Fatalf("step %d: block %v corrupted", step, b.ref)
				}
			}
			s.Free(b.ref, b.n)
			live[i] = live[len(live)-1]
			live = live[:len(live)-1]
			continue
		}
		n := 1 + r.Intn(2000)
		if r.Intn(200) == 0 {
			n = MaxClassSize + r.Intn(5000)
		}
		ref, err := s.Alloc(n)
		if err != nil {
			t.Fatal(err)
		}
		tag := byte(step)
		b := s.Bytes(ref, n)
		for j := range b {
			b[j] = tag
		}
		live = append(live, blk{ref, n, tag})
	}
}

func TestHybridClassTable(t *testing.T) {
	cl := SizeClasses()
	if len(cl) != 201 {
		t.Fatalf("expected 201 classes, got %d", len(cl))
	}
	for i := 1; i < len(cl); i++ {
		prev, cur := cl[i-1], cl[i]
		switch {
		case cur <= LinearClassLimit:
			if cur-prev != 8 {
				t.Fatalf("linear part broken at %d: %d -> %d", i, prev, cur)
			}
		case prev < FineClassLimit:
			p := 1 << (bits.Len(uint(prev)) - 1)
			if cur-prev != p/FineSubclasses {
				t.Fatalf("log-linear part: %d -> %d, want step %d", prev, cur, p/FineSubclasses)
			}
		case cur < MaxClassSize:
			want := (int(float64(prev)*ClassGrowth) + 7) &^ 7
			if cur != want {
				t.Fatalf("geometric part: %d -> %d, want %d", prev, cur, want)
			}
		}
	}
	if ClassSize(156) != 160 || ClassSize(284) != 288 || ClassSize(1052) != 1056 || ClassSize(4124) != 4224 {
		t.Fatalf("ClassSize: %d %d %d %d", ClassSize(156), ClassSize(284), ClassSize(1052), ClassSize(4124))
	}
	// worst-case waste: < 8 B up to 256 B, < 1/32 up to 8 KiB, < ~20 % above
	for n := 17; n <= MaxClassSize; n++ {
		c := ClassSize(n)
		rel := float64(c-n) / float64(c)
		switch {
		case n <= LinearClassLimit && c-n >= 8:
			t.Fatalf("n=%d class %d: absolute waste %d", n, c, c-n)
		case n > LinearClassLimit && n <= FineClassLimit && rel >= 1.0/32:
			t.Fatalf("n=%d class %d: relative waste %.3f", n, c, rel)
		case n > FineClassLimit && rel >= 0.21:
			t.Fatalf("n=%d class %d: relative waste too high", n, c)
		}
	}
	if len(largeSizes) == 0 || largeSizes[len(largeSizes)-1] != MaxAlloc {
		t.Fatal("large classes must end at MaxAlloc")
	}
	for i, s := range largeSizes {
		if s%largeAlign != 0 && s != MaxAlloc {
			t.Fatalf("large class %d = %d not aligned", i, s)
		}
		if largeIndex(int(s)) != i || (i > 0 && largeIndex(int(largeSizes[i-1])+1) != i) {
			t.Fatalf("largeIndex inconsistent at %d", i)
		}
	}
}

func TestSlabRetainLargeAndDonate(t *testing.T) {
	a := MustNew(Config{ChunkSize: 1 << 20})
	defer a.Release()
	s := NewSlab(a, SlabConfig{RetainLarge: true})
	if s.BlockSize(100000) <= 100000 || s.BlockSize(100000)%largeAlign != 0 || s.BlockSize(100) != 104 {
		t.Fatalf("BlockSize: %d %d", s.BlockSize(100000), s.BlockSize(100))
	}
	r1, err := s.Alloc(100000)
	if err != nil {
		t.Fatal(err)
	}
	s.Bytes(r1, s.BlockSize(100000))[s.BlockSize(100000)-1] = 1
	chunksBefore := a.Stats().DedicatedChunks
	s.Free(r1, 100000)
	if a.Stats().DedicatedChunks != chunksBefore {
		t.Fatal("RetainLarge must not return the block to C")
	}
	if st := s.Stats(); st.LargeObjects != 0 || st.FreeListBytes < 100000 {
		t.Fatalf("stats after free: %+v", st)
	}
	r2, _ := s.Alloc(99000) // same large class -> reused
	if r2 != r1 {
		t.Fatalf("large block not reused: %v vs %v", r2, r1)
	}
	if st := s.Stats(); st.LargeObjects != 1 || st.LargeBytes != int64(s.BlockSize(99000)) {
		t.Fatalf("large stats %+v", st)
	}
	// Donate a region and check that small allocations are carved from it
	d, _ := a.Alloc(4096)
	s.Donate(d, 4096)
	s.Donate(Nil, 100) // ignored
	s.Donate(d, 8)     // too small: ignored
	got, _ := s.Alloc(100)
	if got != d {
		t.Fatalf("expected the donated region to be used first: %v vs %v", got, d)
	}
	if st := s.Stats(); st.DonatedBytes != 4096 || st.Spans != 0 {
		t.Fatalf("donation stats %+v", st)
	}
	// a donated region too small for the requested class is skipped
	d2, _ := a.Alloc(64)
	for i := 0; i < 40; i++ { // exhaust the 4 KiB region
		_, _ = s.Alloc(100)
	}
	s.Donate(d2, 64)
	if _, err := s.Alloc(200); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Spans != 1 {
		t.Fatal("expected a fresh span after skipping the small donation")
	}
	s.Reset()
	if st := s.Stats(); st.DonatedBytes != 0 || st.FreeListBytes != 0 {
		t.Fatalf("reset: %+v", st)
	}
}

func TestTryPointer(t *testing.T) {
	a := MustNew(Config{ChunkSize: 4096})
	defer a.Release()
	r, _ := a.Alloc(64)
	if p, ok := a.TryPointer(r, 64); !ok || p != a.Pointer(r) {
		t.Fatal("valid ref rejected")
	}
	for _, c := range []struct {
		ref Ref
		n   int
	}{{Nil, 1}, {makeRef(9, 0), 1}, {r, 5000}, {r, -1}} {
		if _, ok := a.TryPointer(c.ref, c.n); ok {
			t.Fatalf("TryPointer(%v, %d) accepted", c.ref, c.n)
		}
	}
}
