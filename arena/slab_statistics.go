package arena

// ClassStats describes one size class of a Slab.
type ClassStats struct {
	Size           int    // block size of the class
	InUse          uint64 // blocks currently allocated
	Free           uint64 // blocks on the free list
	Carved         uint64 // blocks ever carved for this class
	RequestedBytes int64  // sum of requested sizes of in-use blocks
}

// SlabStats summarises the memory usage of a Slab.
type SlabStats struct {
	Classes        []ClassStats // only small classes that were ever used
	InUseBytes     int64        // bytes of in-use blocks (class-rounded)
	RequestedBytes int64        // bytes requested by in-use blocks
	FreeListBytes  int64        // bytes parked on free lists (incl. large)
	SpanBytes      int64        // bytes obtained from the arena as spans
	DonatedBytes   int64        // bytes received through Donate
	DirectBytes    int64        // bytes of mid-size classes obtained directly
	SpanWaste      int64        // abandoned span tails and unusable donations
	LargeObjects   int64        // live large objects
	LargeBytes     int64        // bytes of live large objects
	Spans          uint64       // spans requested from the arena

	// InternalFragmentation is 1 - RequestedBytes/InUseBytes: the share of
	// in-use memory lost to size-class rounding (0 when nothing is in use).
	InternalFragmentation float64
}

// Stats returns a snapshot of the slab statistics.
func (s *Slab) Stats() SlabStats {
	st := SlabStats{
		SpanBytes:    s.spanBytes,
		DonatedBytes: s.donatedBytes,
		DirectBytes:  s.directSz,
		SpanWaste:    s.spanWaste,
		LargeObjects: s.largeCount,
		LargeBytes:   s.largeBytes,
		Spans:        s.spans,
	}
	for i := range s.classes {
		cs := &s.classes[i]
		if cs.carved == 0 {
			continue
		}
		sz := int64(classSizes[i])
		st.Classes = append(st.Classes, ClassStats{
			Size: int(sz), InUse: cs.inUse, Free: cs.freeCount,
			Carved: cs.carved, RequestedBytes: cs.requested,
		})
		st.InUseBytes += int64(cs.inUse) * sz
		st.RequestedBytes += cs.requested
		st.FreeListBytes += int64(cs.freeCount) * sz
	}
	for i := range s.large {
		st.FreeListBytes += int64(s.large[i].freeCount) * int64(largeSizes[i])
	}
	st.InUseBytes += s.largeBytes
	st.RequestedBytes += s.largeReq
	if st.InUseBytes > 0 {
		st.InternalFragmentation = 1 - float64(st.RequestedBytes)/float64(st.InUseBytes)
	}
	return st
}
