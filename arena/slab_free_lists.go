package arena

// popFree pops the head of a free list; its first word links to the next.
func (s *Slab) popFree(cs *classState, n int) Ref {
	ref := cs.free
	cs.free = Ref(*(*uint64)(s.a.resolve(ref, 8)))
	cs.freeCount--
	cs.inUse++
	cs.requested += int64(n)
	return ref
}

// Free returns a block to the slab. n must be the size passed to the Alloc
// that returned ref (it selects the size class). Freeing Nil is a no-op.
// Freeing a block twice corrupts the free list, exactly like double free in
// C; the store package never does it.
func (s *Slab) Free(ref Ref, n int) {
	if ref == Nil {
		return
	}
	if n <= 0 || n > MaxAlloc {
		panic("arena: Slab.Free with invalid size")
	}
	if n > MaxClassSize {
		size := s.BlockSize(n)
		s.largeCount--
		s.largeBytes -= int64(size)
		s.largeReq -= int64(n)
		if !s.retainLarge {
			s.a.FreeDedicated(ref)
			return
		}
		s.pushFree(&s.large[largeIndex(n)], ref, size, n)
		return
	}
	ci := classIndex(n)
	s.pushFree(&s.classes[ci], ref, int(classSizes[ci]), n)
}

func (s *Slab) pushFree(cs *classState, ref Ref, size, n int) {
	*(*uint64)(s.a.resolve(ref, size)) = uint64(cs.free)
	cs.free = ref
	cs.freeCount++
	cs.inUse--
	cs.requested -= int64(n)
}
