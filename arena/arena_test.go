package arena

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/vladislavprovich/goarena/internal/cmem"
)

func mustPanic(t *testing.T, substr string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q", substr)
		}
		if s, ok := r.(string); ok && !strings.Contains(s, substr) {
			t.Fatalf("panic %q does not contain %q", s, substr)
		}
	}()
	f()
}

func TestRefEncoding(t *testing.T) {
	r := makeRef(3, 1234)
	if r.Chunk() != 3 || r.Offset() != 1234 || r.IsNil() {
		t.Fatalf("bad ref %v", r)
	}
	if Nil.Chunk() != -1 || !Nil.IsNil() || Nil.String() != "Ref(nil)" {
		t.Fatal("bad nil ref")
	}
	if r.Add(8).Offset() != 1242 || r.String() != "Ref(3:1234)" {
		t.Fatal("bad Add/String")
	}
}

func TestNewConfig(t *testing.T) {
	if _, err := New(Config{ChunkSize: 100}); err == nil {
		t.Fatal("expected error for tiny chunk")
	}
	if _, err := New(Config{ChunkSize: MaxChunkSize + 1}); err == nil {
		t.Fatal("expected error for huge chunk")
	}
	if _, err := New(Config{MaxBytes: -1}); err == nil {
		t.Fatal("expected error for negative MaxBytes")
	}
	a := MustNew(Config{ChunkSize: 5000})
	if a.ChunkSize() != 8192 {
		t.Fatalf("chunk size not rounded to 4 KiB: %d", a.ChunkSize())
	}
	a.Release()
	if MustNew(Config{}).ChunkSize() != DefaultChunkSize {
		t.Fatal("default chunk size")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustNew must panic")
		}
	}()
	MustNew(Config{ChunkSize: 1})
}

func TestAllocAlignmentAndIsolation(t *testing.T) {
	a := MustNew(Config{ChunkSize: 64 << 10})
	defer a.Release()
	type blk struct {
		ref Ref
		n   int
	}
	var blocks []blk
	for i := 1; i <= 2000; i++ {
		n := i%97 + 1
		ref, err := a.Alloc(n)
		if err != nil {
			t.Fatal(err)
		}
		if uintptr(a.Pointer(ref))%Alignment != 0 {
			t.Fatalf("misaligned allocation %v", ref)
		}
		b := a.Bytes(ref, n)
		for j := range b {
			b[j] = byte(i)
		}
		blocks = append(blocks, blk{ref, n})
	}
	for i, bl := range blocks {
		for _, c := range a.Bytes(bl.ref, bl.n) {
			if c != byte(i+1) {
				t.Fatalf("block %d overwritten", i)
			}
		}
	}
	st := a.Stats()
	if st.Chunks < 2 || st.Allocs != 2000 || st.ReservedBytes != int64(st.Chunks)*64<<10 {
		t.Fatalf("unexpected stats %+v", st)
	}
	if st.WastedBytes < 0 || st.UsedBytes <= 0 {
		t.Fatalf("bad accounting %+v", st)
	}
}

func TestAllocErrors(t *testing.T) {
	a := MustNew(Config{ChunkSize: 4096, MaxBytes: 8192})
	for _, n := range []int{0, -1, MaxAlloc + 1} {
		if _, err := a.Alloc(n); !errors.Is(err, ErrInvalidSize) {
			t.Fatalf("Alloc(%d) err = %v", n, err)
		}
		if _, err := a.AllocDedicated(n); !errors.Is(err, ErrInvalidSize) {
			t.Fatalf("AllocDedicated(%d) err = %v", n, err)
		}
	}
	if _, err := a.Alloc(4000); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Alloc(4000); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Alloc(4000); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory, got %v", err)
	}
	if _, err := a.Alloc(10000); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory for dedicated, got %v", err)
	}
	// after a failed chunk switch the arena still works within its chunk
	if _, err := a.Alloc(8); err != nil {
		t.Fatal(err)
	}
	a.Release()
	a.Release() // idempotent
	if !a.Released() {
		t.Fatal("Released() = false")
	}
	if _, err := a.Alloc(8); !errors.Is(err, ErrReleased) {
		t.Fatalf("Alloc after release: %v", err)
	}
	if _, err := a.AllocDedicated(8); !errors.Is(err, ErrReleased) {
		t.Fatalf("AllocDedicated after release: %v", err)
	}
	a.Reset()                      // no-op after release
	a.FreeDedicated(makeRef(0, 0)) // no-op after release
}

func TestBoundsChecks(t *testing.T) {
	a := MustNew(Config{ChunkSize: 4096})
	defer a.Release()
	ref, _ := a.Alloc(16)
	mustPanic(t, "invalid reference", func() { a.Bytes(Nil, 1) })
	mustPanic(t, "invalid reference", func() { a.Bytes(makeRef(5, 0), 1) })
	mustPanic(t, "out of bounds", func() { a.Bytes(ref, 5000) })
	mustPanic(t, "out of bounds", func() { a.Bytes(ref, -1) })
	if len(a.Bytes(ref, 4096)) != 4096 {
		t.Fatal("full-chunk view must be allowed")
	}
}

func TestDedicatedChunks(t *testing.T) {
	live := cmem.ReadStats().LiveBlocks
	a := MustNew(Config{ChunkSize: 4096})
	big, err := a.Alloc(10000) // larger than a chunk -> dedicated
	if err != nil || big.Offset() != 0 {
		t.Fatalf("big alloc: %v %v", big, err)
	}
	d, err := a.AllocDedicated(100)
	if err != nil {
		t.Fatal(err)
	}
	b := a.Bytes(big, 10000)
	b[9999] = 7
	st := a.Stats()
	if st.DedicatedChunks != 2 || st.DedicatedBytes != 10000+104 {
		t.Fatalf("dedicated stats %+v", st)
	}
	a.FreeDedicated(d)
	mustPanic(t, "not a live dedicated", func() { a.FreeDedicated(d) })
	d2, _ := a.AllocDedicated(64) // reuses the freed table slot
	if d2.Chunk() != d.Chunk() {
		t.Fatalf("slot not reused: %v vs %v", d2, d)
	}
	a.FreeDedicated(d2)
	r, _ := a.Alloc(8) // a regular chunk may also take the free slot
	mustPanic(t, "not a live dedicated", func() { a.FreeDedicated(r) })
	r2, _ := a.Alloc(4096) // does not fit behind r: second regular chunk
	if r2.Chunk() == r.Chunk() {
		t.Fatal("expected a new chunk")
	}
	a.Release()
	if got := cmem.ReadStats().LiveBlocks; got != live {
		t.Fatalf("leak: live blocks %d -> %d", live, got)
	}
}

func TestResetReusesChunks(t *testing.T) {
	a := MustNew(Config{ChunkSize: 4096})
	defer a.Release()
	for i := 0; i < 10; i++ {
		if _, err := a.Alloc(1000); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Alloc(9000); err != nil {
		t.Fatal(err)
	}
	before := a.Stats()
	a.Reset()
	after := a.Stats()
	if after.Chunks != before.Chunks || after.DedicatedChunks != 0 || after.UsedBytes != 0 {
		t.Fatalf("reset stats %+v -> %+v", before, after)
	}
	for i := 0; i < 10; i++ {
		if _, err := a.Alloc(1000); err != nil {
			t.Fatal(err)
		}
	}
	if a.Stats().ChunkMallocs != before.ChunkMallocs {
		t.Fatal("chunks were not reused after Reset")
	}
}

func TestConcurrentAlloc(t *testing.T) {
	a := MustNew(Config{ChunkSize: 16 << 10})
	defer a.Release()
	var wg sync.WaitGroup
	refs := make([][]Ref, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				ref, err := a.Alloc(24)
				if err != nil {
					t.Error(err)
					return
				}
				b := a.Bytes(ref, 24)
				for j := range b {
					b[j] = byte(g)
				}
				refs[g] = append(refs[g], ref)
			}
		}(g)
	}
	wg.Wait()
	for g, rs := range refs {
		for _, r := range rs {
			for _, c := range a.Bytes(r, 24) {
				if c != byte(g) {
					t.Fatalf("goroutine %d data corrupted", g)
				}
			}
		}
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
