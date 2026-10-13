package arena_test

import (
	"fmt"

	"github.com/vladislavprovich/goarena/arena"
)

func ExampleArena() {
	a, err := arena.New(arena.Config{ChunkSize: 1 << 20})
	if err != nil {
		panic(err)
	}
	defer a.Release() // off-heap memory is not reclaimed by the GC

	ref, _ := a.Alloc(5)
	copy(a.Bytes(ref, 5), "hello")
	fmt.Println(string(a.Bytes(ref, 5)), a.Stats().Chunks)
	// Output: hello 1
}

func ExampleSlab() {
	a := arena.MustNew(arena.Config{ChunkSize: 1 << 20})
	defer a.Release()
	s := arena.NewSlab(a, arena.SlabConfig{})

	r1, _ := s.Alloc(100) // rounded up to a size class
	s.Free(r1, 100)
	r2, _ := s.Alloc(100) // reuses the freed block
	fmt.Println(r1 == r2, arena.ClassSize(100))
	// Output: true 104
}

func ExampleNewSlice() {
	a := arena.MustNew(arena.Config{ChunkSize: 1 << 20})
	defer a.Release()

	type sample struct {
		At    int64
		Value float64
	}
	xs, err := arena.NewSlice[sample](a, 3)
	if err != nil {
		panic(err)
	}
	xs[1].Value = 2.5
	fmt.Println(len(xs), xs[1].Value)

	_, err = arena.NewSlice[string](a, 1) // strings hold Go pointers
	fmt.Println(err)
	// Output:
	// 3 2.5
	// arena: type contains Go pointers: string
}
