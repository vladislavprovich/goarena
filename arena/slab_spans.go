package arena

// region is a block of arena memory donated to a Slab for reuse.
type region struct {
	ref  Ref
	size uint32
}

// refill makes a new current span: a donated region if one is large
// enough, otherwise a fresh span from the arena.
func (s *Slab) refill(size uint32) error {
	for len(s.donated) > 0 {
		r := s.donated[len(s.donated)-1]
		s.donated = s.donated[:len(s.donated)-1]
		if r.size >= size {
			s.spanWaste += int64(s.spanLeft)
			s.span, s.spanLeft = r.ref, r.size
			return nil
		}
		s.spanWaste += int64(r.size) // too small for this class: unusable
	}
	r, err := s.a.Alloc(int(s.spanSize))
	if err != nil {
		return err
	}
	s.spanWaste += int64(s.spanLeft)
	s.span, s.spanLeft = r, s.spanSize
	s.spans++
	s.spanBytes += int64(s.spanSize)
	return nil
}

// Donate hands an n-byte block of arena memory that the caller no longer
// needs (for example an outgrown hash table) to the slab, which will carve
// future small blocks from it. The memory must belong to the slab's arena
// and must not be used by the caller afterwards. Blocks smaller than the
// smallest class are ignored.
func (s *Slab) Donate(ref Ref, n int) {
	if ref == Nil || n < MinClassSize {
		return
	}
	s.a.resolve(ref, n) // bounds check
	for n > 0 {         // regions are limited to 4 GiB - 8 by the uint32 size
		part := n
		if part > 1<<31 {
			part = 1 << 31
		}
		s.donated = append(s.donated, region{ref: ref, size: uint32(part)})
		s.donatedBytes += int64(part)
		ref += Ref(part)
		n -= part
	}
}
