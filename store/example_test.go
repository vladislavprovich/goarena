package store_test

import (
	"fmt"
	"time"

	"github.com/vladislavprovich/goarena/store"
)

func Example() {
	s, err := store.New(store.Config{})
	if err != nil {
		panic(err)
	}
	defer s.Close() // releases all off-heap memory

	_ = s.Set([]byte("user:42"), []byte(`{"name":"Ada"}`))
	if v, ok := s.Get([]byte("user:42")); ok {
		fmt.Println(string(v))
	}
	fmt.Println(s.Len())
	// Output:
	// {"name":"Ada"}
	// 1
}

func ExampleStore_View() {
	s, _ := store.New(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetString("greeting", "hello, off-heap world")

	// View avoids the copy made by Get; the slice is only valid inside fn.
	var n int
	s.View([]byte("greeting"), func(v []byte) { n = len(v) })
	fmt.Println(n)
	// Output: 21
}

func ExampleStore_GetInto() {
	s, _ := store.New(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetString("k", "v1")

	buf := make([]byte, 0, 256) // reused across calls: no allocation per read
	buf, ok := s.GetInto(buf, []byte("k"))
	fmt.Println(string(buf), ok)
	// Output: v1 true
}

func ExampleStore_SetWithTTL() {
	s, _ := store.New(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetWithTTL([]byte("session"), []byte("token"), time.Hour)
	_, ok := s.Get([]byte("session"))
	fmt.Println(ok)
	// Output: true
}

func ExampleStore_Stats() {
	s, _ := store.New(store.Config{Shards: 1, ChunkSize: 1 << 20})
	defer s.Close()
	for i := 0; i < 1000; i++ {
		_ = s.Set([]byte(fmt.Sprintf("key-%04d", i)), make([]byte, 100))
	}
	st := s.Stats()
	fmt.Println(st.Entries, st.ArenaChunks, st.LoadFactor <= store.MaxLoadFactor)
	// Output: 1000 1 true
}
