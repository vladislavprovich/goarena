package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vladislavprovich/goarena/arena"
)

func TestStatsMemoryAccounting(t *testing.T) {
	s := newTestStore(t, Config{Shards: 16, ChunkSize: 1 << 20})
	val := make([]byte, 128)
	for i := 0; i < 20000; i++ {
		_ = s.Set([]byte(fmt.Sprintf("user:%016x", i)), val)
	}
	st := s.Stats()
	wantKV := int64(20000 * (21 + 128))
	if st.KeyValueBytes != wantKV || st.EntryBytes != wantKV+20000*entryHeaderSize {
		t.Fatalf("kv bytes %d entry bytes %d", st.KeyValueBytes, st.EntryBytes)
	}
	if st.SlabInUseBytes < st.EntryBytes || st.InternalFragmentation < 0 || st.InternalFragmentation > 0.25 {
		t.Fatalf("slab accounting %+v", st)
	}
	if st.ArenaReservedBytes < st.SlabInUseBytes+st.IndexBytes {
		t.Fatalf("arena accounting %#v", st)
	}
	if arena.OffHeap && (st.OffHeapBytes != st.ArenaReservedBytes || st.CgoCalls <= 0) {
		t.Fatalf("off-heap accounting %#v", st)
	}
	if !arena.OffHeap && (st.OffHeapBytes != 0 || st.CgoCalls != 0) {
		t.Fatalf("without cgo nothing is off-heap: %#v", st)
	}
	if st.ArenaChunkMallocs == 0 {
		t.Fatalf("chunk accounting %#v", st)
	}
	t.Logf("entries=%d slab=%d frag=%.3f arena=%d index=%d probe=%.3f/%d",
		st.Entries, st.SlabInUseBytes, st.InternalFragmentation, st.ArenaReservedBytes,
		st.IndexBytes, st.AvgProbeDistance, st.MaxProbeDistance)
}

func TestStatsString(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1, ChunkSize: 1 << 20})
	for i := 0; i < 1000; i++ {
		_ = s.Set([]byte(fmt.Sprintf("key-%04d", i)), make([]byte, 100))
	}
	s.Delete([]byte("key-0000"))
	got := s.Stats().String()
	where := "reserved off-heap (index"
	if !arena.OffHeap {
		where = "reserved in the Go heap (built without cgo; index"
	}
	for _, want := range []string{
		"999 entries", "105.4 KiB of keys and values", "1.0 MiB " + where,
		" 32.0 KiB,", "load factor 0.", "1000 sets, 1 deletes, 0 expired",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Stats.String() = %q, missing %q", got, want)
		}
	}
	if fmt.Sprint(s.Stats()) != s.Stats().String() {
		t.Error("fmt does not use Stats.String")
	}
	if got := (Stats{}).String(); !strings.HasPrefix(got, "0 entries, 0 B of keys and values") {
		t.Errorf("zero Stats: %q", got)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB",
		64 << 20: "64.0 MiB", 3 << 30: "3.0 GiB", 5 << 40: "5.0 TiB", 2048 << 40: "2048.0 TiB",
	} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestGoMemoryBudget(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4, ChunkSize: 1 << 20})
	if !arena.OffHeap {
		if got := s.GoMemoryBudget(1000); got != 1000 {
			t.Fatalf("without cgo the whole budget belongs to Go: %d", got)
		}
		return
	}
	off := s.Stats().OffHeapBytes
	if got := s.GoMemoryBudget(off + 1000); got != 1000 {
		t.Fatalf("budget = %d, want 1000", got)
	}
	if got := s.GoMemoryBudget(off - 1); got != 0 {
		t.Fatalf("budget = %d, want 0", got)
	}
}
