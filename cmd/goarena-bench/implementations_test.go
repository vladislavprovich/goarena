package main

import (
	"bytes"
	"math"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/vladislavprovich/goarena/arena"
)

// restoreGC undoes the GC settings that some implementations apply to the
// (test) process.
func restoreGC() {
	debug.SetGCPercent(100)
	debug.SetMemoryLimit(math.MaxInt64)
}

func TestImplementations(t *testing.T) {
	defer restoreGC()
	if _, err := impls["goarena-budget"].New(options{Shards: 4, ChunkMiB: 1}); err == nil {
		t.Fatal("goarena-budget without a memory limit must fail")
	}
	for _, name := range implNames() {
		t.Run(name, func(t *testing.T) {
			o := options{Shards: 16, ChunkMiB: 1, MemLimitMiB: 4096}
			c, err := impls[name].New(o)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var k []byte
			val := make([]byte, 64)
			for i := uint64(0); i < 5000; i++ {
				k = appendKey(k, i)
				fillValue(val, i, 0)
				if err := c.Set(k, val); err != nil {
					t.Fatal(err)
				}
			}
			buf := make([]byte, 0, 64)
			for i := uint64(0); i < 5000; i++ {
				k = appendKey(k, i)
				fillValue(val, i, 0)
				var ok bool
				buf, ok = c.Get(buf, k)
				if !ok || !bytes.Equal(buf, val) {
					t.Fatalf("Get(%d) mismatch", i)
				}
			}
			// overwrite with same and different sizes
			k = appendKey(k, 7)
			_ = c.Set(k, []byte("12345678"))
			_ = c.Set(k, bytes.Repeat([]byte("x"), 300))
			if buf, ok := c.Get(buf, k); !ok || len(buf) != 300 {
				t.Fatalf("overwrite: %d %v", len(buf), ok)
			}
			if _, ok := c.Get(buf, []byte("absent")); ok {
				t.Fatal("found absent key")
			}
			if strings.HasPrefix(name, "goarena") && arena.OffHeap && c.OffHeapBytes() <= 0 {
				t.Fatal("goarena must report off-heap bytes")
			}
			if strings.HasPrefix(name, "goarena") && !arena.OffHeap && c.OffHeapBytes() != 0 {
				t.Fatal("goarena built without cgo holds no off-heap memory")
			}
			if !strings.HasPrefix(name, "goarena") && c.OffHeapBytes() != 0 {
				t.Fatal("Go implementations hold no off-heap memory")
			}
		})
	}
	g := newGoBytes(1)
	if err := g.Set([]byte("k"), make([]byte, goBytesChunk)); err == nil {
		t.Fatal("gobytes must reject entries larger than a chunk")
	}
}
