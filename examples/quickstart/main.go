// Command quickstart is a tour of the goarena key-value store: create a
// store, write and read entries in every supported way, use a TTL, look at
// the statistics and close the store.
//
//	go run ./examples/quickstart
package main

import (
	"fmt"
	"log"
	"time"

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

	// Set copies key and value into off-heap memory; the caller may reuse
	// both slices immediately.
	if err := s.Set([]byte("user:1"), []byte(`{"name":"Ada"}`)); err != nil {
		log.Fatal(err)
	}
	_ = s.SetString("user:2", `{"name":"Grace"}`)

	// Get returns a fresh copy of the value.
	if v, ok := s.Get([]byte("user:1")); ok {
		fmt.Printf("Get       user:1 -> %s\n", v)
	}

	// GetInto reuses the capacity of a caller-owned buffer: on a hot path
	// it does not allocate at all.
	buf := make([]byte, 0, 256)
	buf, ok := s.GetInto(buf, []byte("user:2"))
	fmt.Printf("GetInto   user:2 -> %s (found=%v)\n", buf, ok)

	// View passes a zero-copy view of the value to a callback. The slice
	// aliases off-heap memory and is valid only inside the callback.
	s.View([]byte("user:2"), func(v []byte) {
		fmt.Printf("View      user:2 -> %d bytes, no copy\n", len(v))
	})

	// Entries with a time to live are invisible once the TTL has elapsed;
	// EvictExpired returns their memory to the allocator.
	_ = s.SetWithTTL([]byte("session:42"), []byte("token"), 50*time.Millisecond)
	fmt.Printf("TTL       session:42 present: %v\n", s.Has([]byte("session:42")))
	time.Sleep(60 * time.Millisecond)
	fmt.Printf("TTL       session:42 present after 60 ms: %v, evicted: %d\n",
		s.Has([]byte("session:42")), s.EvictExpired())

	// Delete reports whether a live entry was removed.
	fmt.Printf("Delete    user:1 -> %v, again -> %v\n",
		s.Delete([]byte("user:1")), s.Delete([]byte("user:1")))

	// Fill the store a little to make the statistics interesting.
	val := make([]byte, 100)
	for i := 0; i < 10_000; i++ {
		_ = s.Set(fmt.Appendf(nil, "item:%05d", i), val)
	}
	st := s.Stats()
	fmt.Printf("Stats     entries=%d  key+value=%d KiB  off-heap=%d MiB  load factor=%.2f\n",
		st.Entries, st.KeyValueBytes>>10, st.OffHeapBytes>>20, st.LoadFactor)

	// Range visits every live entry, one shard at a time.
	n := 0
	s.Range(func(key, val []byte) bool {
		n++
		return true
	})
	fmt.Printf("Range     visited %d entries\n", n)

	if err := s.Close(); err != nil {
		log.Fatal(err)
	}
	// After Close writes fail with ErrClosed and reads report "not found".
	fmt.Printf("Close     closed=%v, Set after Close -> %v\n",
		s.Closed(), s.Set([]byte("k"), []byte("v")))
}
