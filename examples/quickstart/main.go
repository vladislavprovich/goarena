// Command quickstart is a tour of the goarena key-value store: create a
// store, write and read entries in every supported way, use a TTL, load
// values cache-aside, look at the statistics and close the store.
//
//	go run ./examples/quickstart
package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/vladislavprovich/goarena/arena"
	"github.com/vladislavprovich/goarena/store"
)

func main() {
	// The zero Config is valid: 256 shards, 64 MiB arena chunks, no memory
	// limit, no default TTL.
	s, err := store.New(store.Config{})
	if err != nil {
		log.Fatal(err)
	}
	// Close returns every off-heap byte to the C allocator. The garbage
	// collector does not do it for you.
	defer s.Close()

	if !arena.OffHeap {
		fmt.Println("note: built without cgo, the store's memory is in the Go heap")
	}

	// Set copies key and value into off-heap memory; the caller may reuse
	// both slices immediately.
	if err := s.Set([]byte("user:1"), []byte(`{"name":"Ada"}`)); err != nil {
		log.Fatal(err)
	}
	_ = s.SetString("user:2", `{"name":"Grace"}`)

	// Get returns a fresh copy of the value.
	if v, ok := s.Get([]byte("user:1")); ok {
		fmt.Printf("Get        user:1 -> %s\n", v)
	}

	// GetInto reuses the capacity of a caller-owned buffer: on a hot path
	// it does not allocate at all.
	buf := make([]byte, 0, 256)
	buf, ok := s.GetInto(buf, []byte("user:2"))
	fmt.Printf("GetInto    user:2 -> %s (found=%v)\n", buf, ok)

	// View passes a zero-copy view of the value to a callback. The slice
	// aliases off-heap memory and is valid only inside the callback.
	s.View([]byte("user:2"), func(v []byte) {
		fmt.Printf("View       user:2 -> %d bytes, no copy\n", len(v))
	})

	// GetOrLoad is the cache-aside pattern: on a miss it calls the loader
	// (without holding any lock), caches the result and returns it.
	queries := 0
	loadUser := func() ([]byte, error) {
		queries++ // stands for a database query
		return []byte(`{"name":"Linus"}`), nil
	}
	for i := 0; i < 3; i++ {
		_, _ = s.GetOrLoad([]byte("user:3"), loadUser)
	}
	v, _ := s.GetOrLoad([]byte("user:3"), loadUser)
	fmt.Printf("GetOrLoad  user:3 -> %s (loader called %d time)\n", v, queries)

	// GetOrSet stores a value only if the key is absent (atomically).
	v, loaded, _ := s.GetOrSet([]byte("user:1"), []byte(`{"name":"someone else"}`))
	fmt.Printf("GetOrSet   user:1 -> %s (already present=%v)\n", v, loaded)

	// Entries with a time to live are invisible once the TTL has elapsed;
	// EvictExpired returns their memory to the allocator.
	_ = s.SetStringWithTTL("session:42", "token", 50*time.Millisecond)
	fmt.Printf("TTL        session:42 present: %v\n", s.HasString("session:42"))
	time.Sleep(60 * time.Millisecond)
	fmt.Printf("TTL        session:42 present after 60 ms: %v, evicted: %d\n",
		s.HasString("session:42"), s.EvictExpired())

	// Delete reports whether a live entry was removed.
	fmt.Printf("Delete     user:1 -> %v, again -> %v\n",
		s.Delete([]byte("user:1")), s.Delete([]byte("user:1")))

	// Write errors wrap ErrTooLarge, ErrNoMemory and ErrClosed with
	// details; compare them with errors.Is.
	err = s.Set(make([]byte, store.MaxKeySize+1), nil)
	fmt.Printf("Errors     too large: %v\n           errors.Is(err, store.ErrTooLarge) = %v\n",
		err, errors.Is(err, store.ErrTooLarge))

	// Fill the store a little to make the statistics interesting.
	val := make([]byte, 100)
	for i := 0; i < 10_000; i++ {
		_ = s.Set(fmt.Appendf(nil, "item:%05d", i), val)
	}
	// Stats has a one-line String form for logs; the fields are there for
	// monitoring.
	st := s.Stats()
	fmt.Printf("Stats      %v\n", st)
	fmt.Printf("           entries=%d  load factor=%.2f  avg probe=%.2f\n",
		st.Entries, st.LoadFactor, st.AvgProbeDistance)

	// Range visits every live entry, one shard at a time.
	n := 0
	s.Range(func(key, val []byte) bool {
		n++
		return true
	})
	fmt.Printf("Range      visited %d entries\n", n)

	if err := s.Close(); err != nil {
		log.Fatal(err)
	}
	// After Close writes fail with ErrClosed and reads report "not found".
	fmt.Printf("Close      closed=%v, Set after Close -> %v\n",
		s.Closed(), s.Set([]byte("k"), []byte("v")))
}
