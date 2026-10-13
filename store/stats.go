package store

import "github.com/savokh/goarena/internal/cmem"

// Stats is a snapshot of the store's size and memory usage. Shards are
// visited one at a time, so the snapshot is not atomic across shards.
type Stats struct {
	Shards  int   // number of shards
	Entries int64 // entries in the index (incl. not yet evicted expired ones)

	KeyValueBytes int64 // sum of len(key)+len(value) of all entries
	EntryBytes    int64 // KeyValueBytes plus the entry headers (8 B, 16 B with TTL)

	SlabInUseBytes        int64   // class-rounded bytes of live entries
	FreeListBytes         int64   // freed blocks waiting for reuse
	SpanWasteBytes        int64   // abandoned span tails
	LargeObjects          int64   // entries above the largest size class
	InternalFragmentation float64 // 1 - EntryBytes/SlabInUseBytes

	ArenaReservedBytes int64  // bytes the arena reserved from C (entries + index tables)
	ArenaChunks        int    // regular arena chunks
	ArenaChunkMallocs  uint64 // chunks requested from C over the lifetime
	DonatedBytes       int64  // outgrown index tables recycled as entry memory

	IndexSlots       int64   // total slots of all shard indexes
	IndexBytes       int64   // IndexSlots * 16 (part of ArenaReservedBytes)
	Tombstones       int64   // tombstone slots
	LoadFactor       float64 // (Entries + Tombstones) / IndexSlots
	AvgProbeDistance float64 // mean displacement of entries from home slot
	MaxProbeDistance uint64  // largest displacement

	OffHeapBytes int64 // all C memory of the store (= ArenaReservedBytes)

	Sets    uint64 // successful writes
	Deletes uint64 // successful deletes
	Expired uint64 // entries removed after their TTL elapsed

	// CgoCalls is the process-wide number of cgo transitions made by
	// goarena (all stores and arenas), useful to verify amortisation.
	CgoCalls int64
}

// Stats returns a snapshot of the store statistics. It costs O(index size)
// because probe distances are computed by scanning the index; do not call
// it on a hot path.
func (s *Store) Stats() Stats {
	st := Stats{Shards: len(s.shards), CgoCalls: cmem.ReadStats().CgoCalls}
	var probeSum uint64
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		if s.closed.Load() {
			sh.mu.Unlock()
			break
		}
		t := sh.tab.Load()
		st.Entries += int64(sh.live)
		st.Tombstones += int64(sh.tombs)
		st.IndexSlots += int64(t.capacity())
		st.KeyValueBytes += sh.kvBytes
		st.EntryBytes += sh.entryBytes
		st.Sets += sh.sets
		st.Deletes += sh.deletes
		st.Expired += sh.expired
		ss := sh.slab.Stats()
		st.SlabInUseBytes += ss.InUseBytes
		st.FreeListBytes += ss.FreeListBytes
		st.SpanWasteBytes += ss.SpanWaste
		st.LargeObjects += ss.LargeObjects
		st.DonatedBytes += ss.DonatedBytes
		sum, max := probeStats(t)
		probeSum += sum
		if max > st.MaxProbeDistance {
			st.MaxProbeDistance = max
		}
		sh.mu.Unlock()
	}
	as := s.arena.Stats()
	st.ArenaReservedBytes = as.ReservedBytes
	st.ArenaChunks = as.Chunks
	st.ArenaChunkMallocs = as.ChunkMallocs
	st.IndexBytes = st.IndexSlots * slotSize
	st.OffHeapBytes = st.ArenaReservedBytes
	if st.SlabInUseBytes > 0 {
		st.InternalFragmentation = 1 - float64(st.EntryBytes)/float64(st.SlabInUseBytes)
	}
	if st.IndexSlots > 0 {
		st.LoadFactor = float64(st.Entries+st.Tombstones) / float64(st.IndexSlots)
	}
	if st.Entries > 0 {
		st.AvgProbeDistance = float64(probeSum) / float64(st.Entries)
	}
	return st
}

// GoMemoryBudget returns the part of a total process memory budget (bytes)
// that remains for the Go heap once the store's off-heap memory is
// subtracted, or 0 if the store already exceeds the budget.
//
// GOMEMLIMIT (debug.SetMemoryLimit) counts only memory managed by the Go
// runtime, so a process that keeps its data in a Store must lower the Go
// limit by the off-heap size to stay inside a container budget. Because the
// Go heap of such a process is small, the GC can then run with GOGC=off
// and be triggered by the limit alone:
//
//	debug.SetGCPercent(-1)
//	debug.SetMemoryLimit(s.GoMemoryBudget(budget))
//
// Call it again (or periodically) when the store grows significantly.
func (s *Store) GoMemoryBudget(total int64) int64 {
	if r := total - s.Stats().OffHeapBytes; r > 0 {
		return r
	}
	return 0
}
