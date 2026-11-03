package arena_test

import (
	"fmt"
	"log"

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

func ExampleNewValue() {
	a := arena.MustNew(arena.Config{ChunkSize: 1 << 20})
	defer a.Release()

	// A pointer-free struct: fixed-size arrays and numbers only.
	type counters struct {
		Hits, Misses uint64
		Name         [16]byte
	}
	c, err := arena.NewValue[counters](a) // zeroed, invisible to the GC
	if err != nil {
		panic(err)
	}
	c.Hits++
	copy(c.Name[:], "cache")
	fmt.Println(c.Hits, c.Misses, string(c.Name[:5]))

	fmt.Println(arena.IsPointerFree[counters](), arena.IsPointerFree[[]int]())
	// Output:
	// 1 0 cache
	// true false
}

func ExampleArena_Reset() {
	a := arena.MustNew(arena.Config{ChunkSize: 1 << 20})
	defer a.Release()

	// Per-batch scratch memory: allocate freely, then drop everything at
	// once. Reset keeps the chunks for the next batch.
	for batch := 0; batch < 3; batch++ {
		xs, _ := arena.NewSlice[int64](a, 10_000)
		xs[0] = int64(batch)
		a.Reset() // xs must not be used after this point
	}
	fmt.Println(a.Stats().Chunks, a.Stats().ChunkMallocs)
	// Output: 1 1
}

func ExampleOffHeap() {
	// OffHeap is false when the module is built with CGO_ENABLED=0: the
	// arena then lives in the Go heap (still not scanned by the GC, but
	// counted by GOGC and GOMEMLIMIT).
	if !arena.OffHeap {
		log.Print("goarena built without cgo: data is kept in the Go heap")
	}
}
