// Command typed-arena shows the lower-level allocators of package arena:
// typed values and slices of pointer-free structs in a bump-pointer Arena,
// and individually freed blocks in a size-class Slab.
//
//	go run ./examples/typed-arena
package main

import (
	"fmt"
	"log"

	"github.com/vladislavprovich/goarena/arena"
)

// vec2 and particle contain only numbers, so they may live in C memory: the
// garbage collector never needs to look inside them.
type vec2 struct{ X, Y float64 }

type particle struct {
	Pos, Vel vec2
	Mass     float32
	ID       uint32
}

// named is NOT pointer-free: a string holds a pointer to its bytes.
type named struct {
	Name string
}

func main() {
	a, err := arena.New(arena.Config{ChunkSize: 1 << 20}) // 1 MiB chunks
	if err != nil {
		log.Fatal(err)
	}
	// Release returns all chunks to C; every value below dies with it.
	defer a.Release()

	// --- typed helpers -------------------------------------------------
	fmt.Println("pointer-free:", "particle =", arena.IsPointerFree[particle](),
		" named =", arena.IsPointerFree[named]())

	origin, err := arena.NewValue[vec2](a) // zeroed, lives off-heap
	if err != nil {
		log.Fatal(err)
	}
	origin.X, origin.Y = 1, 2

	ps, err := arena.NewSlice[particle](a, 10_000)
	if err != nil {
		log.Fatal(err)
	}
	for i := range ps {
		ps[i] = particle{Pos: *origin, Vel: vec2{X: float64(i), Y: 1}, Mass: 1, ID: uint32(i)}
	}
	for step := 0; step < 10; step++ { // a few integration steps
		for i := range ps {
			ps[i].Pos.X += ps[i].Vel.X * 0.1
			ps[i].Pos.Y += ps[i].Vel.Y * 0.1
		}
	}
	fmt.Printf("particles:    %d in arena memory, last at (%.1f, %.1f)\n",
		len(ps), ps[len(ps)-1].Pos.X, ps[len(ps)-1].Pos.Y)

	// Types with Go pointers are refused instead of silently corrupting
	// memory the garbage collector cannot see.
	if _, err := arena.NewValue[named](a); err != nil {
		fmt.Println("refused:     ", err)
	}

	st := a.Stats()
	fmt.Printf("arena:        chunks=%d reserved=%d KiB used=%d KiB allocs=%d\n",
		st.Chunks, st.ReservedBytes>>10, st.UsedBytes>>10, st.Allocs)

	// --- raw references --------------------------------------------------
	// A Ref is a plain integer (chunk, offset) and can be stored anywhere.
	ref, _ := a.Alloc(16)
	copy(a.Bytes(ref, 16), "hello, off-heap!")
	fmt.Printf("ref:          %v -> %q\n", ref, a.Bytes(ref, 16))

	// --- slab: free individual objects -----------------------------------
	// A Slab rounds requests up to a size class and keeps freed blocks on
	// per-class free lists for reuse. It is not safe for concurrent use.
	s := arena.NewSlab(a, arena.SlabConfig{})
	refs := make([]arena.Ref, 0, 1000)
	for i := 0; i < 1000; i++ {
		r, err := s.Alloc(100) // 100 B -> class of 104 B
		if err != nil {
			log.Fatal(err)
		}
		refs = append(refs, r)
	}
	for _, r := range refs[:500] {
		s.Free(r, 100) // the size selects the class
	}
	reused, _ := s.Alloc(100) // pops the most recently freed block
	ss := s.Stats()
	fmt.Printf("slab:         class(100)=%d B, reused freed block: %v\n",
		arena.ClassSize(100), reused == refs[499])
	fmt.Printf("slab stats:   in use=%d KiB free lists=%d KiB fragmentation=%.1f%%\n",
		ss.InUseBytes>>10, ss.FreeListBytes>>10, 100*ss.InternalFragmentation)
}
