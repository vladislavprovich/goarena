package arena

// ReservedBytes returns the number of bytes currently reserved from C. It
// is cheaper than [Arena.Stats] and safe for concurrent use.
func (a *Arena) ReservedBytes() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reserved
}

// Stats describes the memory usage of an arena.
type Stats struct {
	ChunkSize       int    // configured regular chunk size
	Chunks          int    // live regular chunks
	DedicatedChunks int    // live dedicated (large-object) chunks
	DedicatedBytes  int64  // bytes in dedicated chunks
	ReservedBytes   int64  // bytes reserved from C (all chunks)
	UsedBytes       int64  // bytes handed out since the last Reset
	WastedBytes     int64  // chunk tails abandoned when switching chunks
	Allocs          uint64 // successful allocations since creation
	ChunkMallocs    uint64 // chunks requested from C since creation
}

// Stats returns a snapshot of the arena statistics.
func (a *Arena) Stats() Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Stats{
		ChunkSize:       int(a.chunkSize),
		Chunks:          a.regular,
		DedicatedChunks: a.dedicated,
		DedicatedBytes:  a.dedicatedSz,
		ReservedBytes:   a.reserved,
		UsedBytes:       a.used,
		WastedBytes:     a.wasted,
		Allocs:          a.allocs,
		ChunkMallocs:    a.chunkMallocs,
	}
}
