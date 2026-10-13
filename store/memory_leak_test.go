package store

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/internal/cmem"
)

// TestNoLeakAfterClose fills a store with entries of all size classes
// (including large objects), deletes part of them and checks that Close
// returns every C block to the allocator.
func TestNoLeakAfterClose(t *testing.T) {
	before := cmem.ReadStats()
	s, err := New(Config{Shards: 32, ChunkSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20000; i++ {
		v := make([]byte, (i*37)%5000)
		if i%1000 == 0 {
			v = make([]byte, 100<<10) // large object
		}
		if err := s.Set([]byte(fmt.Sprintf("k%06d", i)), v); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			s.Delete([]byte(fmt.Sprintf("k%06d", i/2)))
		}
	}
	mid := cmem.ReadStats()
	if mid.LiveBlocks <= before.LiveBlocks {
		t.Fatal("store did not allocate C memory")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	after := cmem.ReadStats()
	if after.LiveBlocks != before.LiveBlocks || after.LiveBytes != before.LiveBytes {
		t.Fatalf("leak: live blocks %d -> %d, bytes %d -> %d",
			before.LiveBlocks, after.LiveBlocks, before.LiveBytes, after.LiveBytes)
	}
	t.Logf("C blocks while open: %d, live after Close: %d, cgo calls: %d",
		mid.LiveBlocks-before.LiveBlocks, after.LiveBlocks-before.LiveBlocks, after.CgoCalls-before.CgoCalls)
}

// TestFinalizerSafetyNet checks that a store dropped without Close is
// eventually released by its finalizer and counted in FinalizedStores.
func TestFinalizerSafetyNet(t *testing.T) {
	before := cmem.ReadStats().LiveBlocks
	fin := FinalizedStores()
	func() {
		s, err := New(Config{Shards: 4})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.SetString("leaked", "store")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for FinalizedStores() == fin && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if FinalizedStores() != fin+1 {
		t.Fatal("finalizer did not run")
	}
	if after := cmem.ReadStats().LiveBlocks; after > before {
		t.Fatalf("finalizer did not free memory: %d -> %d", before, after)
	}
}

// TestOffHeapIsInvisibleToGC is a coarse check of the central claim: a
// store holding ~100 MB of data adds almost nothing to the Go heap.
func TestOffHeapIsInvisibleToGC(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates ~100 MB")
	}
	// The store's fixed bookkeeping (shards, slabs with their class
	// tables: ~2 MiB for 256 shards) is allocated by New; what must not
	// grow with the number of entries is measured from there on.
	s := newTestStore(t, Config{})
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	val := make([]byte, 128)
	key := make([]byte, 0, 32)
	for i := 0; i < 500000; i++ {
		key = fmt.Appendf(key[:0], "user:%016x", i)
		if err := s.Set(key, val); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	st := s.Stats()
	heapGrowth := int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	if heapGrowth > st.OffHeapBytes/100 {
		t.Fatalf("Go heap grew by %d bytes for %d off-heap bytes", heapGrowth, st.OffHeapBytes)
	}
	t.Logf("entries=%d off-heap=%d MiB, Go heap growth=%d KiB", st.Entries, st.OffHeapBytes>>20, heapGrowth>>10)
}
