package store_test

import (
	"errors"
	"fmt"
	"log"
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

func ExampleNew() {
	s, err := store.New(store.Config{
		Shards:          64,               // fewer shards for a small store
		MaxMemory:       256 << 20,        // fail writes with ErrNoMemory above 256 MiB
		DefaultTTL:      10 * time.Minute, // applies to Set and SetString
		InitialCapacity: 100_000,          // pre-size the index, no rehashing up to 100k entries
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	_ = s.SetString("greeting", "hello")
	v, ok := s.GetString("greeting")
	fmt.Println(v, ok)
	// Output: hello true
}

func ExampleMustNew() {
	// MustNew panics on an invalid configuration; handy in tests and for
	// package-level variables with a constant configuration.
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()

	_ = s.SetString("k", "v")
	fmt.Println(s.HasString("k"))
	// Output: true
}

func ExampleStore_Get() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()

	_ = s.Set([]byte("k"), []byte("value"))
	v, ok := s.Get([]byte("k")) // v is a fresh copy, owned by the caller
	fmt.Println(string(v), ok)

	_, ok = s.Get([]byte("missing"))
	fmt.Println(ok)
	// Output:
	// value true
	// false
}

func ExampleStore_GetInto() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetString("k", "v1")

	buf := make([]byte, 0, 256) // reused across calls: no allocation per read
	buf, ok := s.GetInto(buf, []byte("k"))
	fmt.Println(string(buf), ok)
	// Output: v1 true
}

func ExampleStore_View() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetString("greeting", "hello, off-heap world")

	// View avoids the copy made by Get; the slice is only valid inside fn.
	var n int
	s.View([]byte("greeting"), func(v []byte) { n = len(v) })
	fmt.Println(n)
	// Output: 21
}

func ExampleStore_SetWithTTL() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetWithTTL([]byte("session"), []byte("token"), time.Hour)
	_, ok := s.Get([]byte("session"))
	fmt.Println(ok)
	// Output: true
}

func ExampleStore_EvictExpired() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()
	_ = s.SetStringWithTTL("session:1", "token", 10*time.Millisecond)
	_ = s.SetString("config", "no TTL")
	time.Sleep(20 * time.Millisecond)

	// An expired entry is invisible at once, but it keeps its memory (and
	// counts in Len) until EvictExpired removes it. Services call it
	// periodically, for example from a time.Ticker loop.
	fmt.Println(s.HasString("session:1"), s.Len())
	fmt.Println(s.EvictExpired(), s.Len())
	// Output:
	// false 2
	// 1 1
}

func ExampleStore_GetOrSet() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()

	v, loaded, _ := s.GetOrSet([]byte("k"), []byte("first"))
	fmt.Println(string(v), loaded)
	v, loaded, _ = s.GetOrSet([]byte("k"), []byte("second")) // already present
	fmt.Println(string(v), loaded)
	// Output:
	// first false
	// first true
}

func ExampleStore_GetOrLoad() {
	s := store.MustNew(store.Config{Shards: 16, DefaultTTL: time.Minute})
	defer s.Close()

	// A slow source of truth, for example a database.
	db := map[string]string{"user:42": `{"name":"Ada"}`}
	queries := 0
	errNotFound := errors.New("not found")
	loadUser := func(id string) func() ([]byte, error) {
		return func() ([]byte, error) {
			queries++
			v, ok := db[id]
			if !ok {
				return nil, errNotFound // nothing is cached
			}
			return []byte(v), nil
		}
	}

	for i := 0; i < 3; i++ {
		v, err := s.GetOrLoad([]byte("user:42"), loadUser("user:42"))
		fmt.Println(string(v), err)
	}
	_, err := s.GetOrLoad([]byte("user:7"), loadUser("user:7"))
	fmt.Println(err, queries)
	// Output:
	// {"name":"Ada"} <nil>
	// {"name":"Ada"} <nil>
	// {"name":"Ada"} <nil>
	// not found 2
}

func ExampleStore_Range() {
	s := store.MustNew(store.Config{Shards: 16})
	defer s.Close()
	for i := 0; i < 5; i++ {
		_ = s.SetString(fmt.Sprint("item:", i), "x")
	}

	// Range visits entries in no particular order. The slices alias
	// off-heap memory: copy what you keep, and do not call the store
	// from inside the callback.
	n := 0
	s.Range(func(key, val []byte) bool {
		n++
		return true // false stops the iteration
	})
	fmt.Println(n)
	// Output: 5
}

func ExampleConfig_gcPacing() {
	// For a service that allocates per request: let the GC pace as if the
	// off-heap data were part of the heap, and treat an existing
	// GOMEMLIMIT as the budget for Go plus off-heap memory. GOGC and
	// GOMEMLIMIT are restored when the last paced store is closed.
	s, err := store.New(store.Config{GCPacing: true})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	_ = s.SetString("k", "v")
	fmt.Println(s.Len())
	// Output: 1
}

func ExampleStore_Stats() {
	s := store.MustNew(store.Config{Shards: 1, ChunkSize: 1 << 20})
	defer s.Close()
	for i := 0; i < 1000; i++ {
		_ = s.Set([]byte(fmt.Sprintf("key-%04d", i)), make([]byte, 100))
	}
	st := s.Stats()
	fmt.Println(st.Entries, st.ArenaChunks, st.LoadFactor <= store.MaxLoadFactor)
	// Output: 1000 1 true
}

func ExampleStats_String() {
	s := store.MustNew(store.Config{Shards: 1, ChunkSize: 1 << 20})
	defer s.Close()
	for i := 0; i < 1000; i++ {
		_ = s.Set([]byte(fmt.Sprintf("key-%04d", i)), make([]byte, 100))
	}
	// A one-line summary for logs, for example:
	// 1000 entries, 105.5 KiB of keys and values, 1.0 MiB reserved off-heap
	// (index 32.0 KiB, load factor 0.49), 3.3% size-class waste; 1000 sets,
	// 0 deletes, 0 expired
	log.Println("cache:", s.Stats())
}

func ExampleStore_Set_errors() {
	s := store.MustNew(store.Config{Shards: 1, ChunkSize: 64 << 10, MaxMemory: 128 << 10})
	defer s.Close()

	err := s.Set([]byte("big"), make([]byte, 1<<20))
	// The errors carry details; compare them with errors.Is.
	fmt.Println(errors.Is(err, store.ErrNoMemory), errors.Is(err, store.ErrTooLarge))

	err = s.Set(make([]byte, store.MaxKeySize+1), nil)
	fmt.Println(err)
	// Output:
	// true false
	// store: key or value too large: key of 65536 bytes exceeds MaxKeySize (65535 bytes)
}
