package store

import (
	"fmt"
	"testing"
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
	if st.OffHeapBytes != st.ArenaReservedBytes || st.ArenaReservedBytes < st.SlabInUseBytes+st.IndexBytes {
		t.Fatalf("off-heap accounting %+v", st)
	}
	if st.CgoCalls <= 0 || st.ArenaChunkMallocs == 0 {
		t.Fatalf("cgo accounting %+v", st)
	}
	t.Logf("entries=%d slab=%d frag=%.3f arena=%d index=%d probe=%.3f/%d",
		st.Entries, st.SlabInUseBytes, st.InternalFragmentation, st.ArenaReservedBytes,
		st.IndexBytes, st.AvgProbeDistance, st.MaxProbeDistance)
}

func TestGoMemoryBudget(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4, ChunkSize: 1 << 20})
	off := s.Stats().OffHeapBytes
	if got := s.GoMemoryBudget(off + 1000); got != 1000 {
		t.Fatalf("budget = %d, want 1000", got)
	}
	if got := s.GoMemoryBudget(off - 1); got != 0 {
		t.Fatalf("budget = %d, want 0", got)
	}
}
