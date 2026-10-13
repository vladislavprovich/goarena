package arena

func alignUp(n uintptr) uintptr { return (n + Alignment - 1) &^ (Alignment - 1) }

// Alloc reserves n bytes (rounded up to 8) and returns a reference to them.
// The memory is not zeroed. It returns ErrInvalidSize for n <= 0 or
// n > MaxAlloc, ErrOutOfMemory when MaxBytes would be exceeded and
// ErrReleased after Release.
func (a *Arena) Alloc(n int) (Ref, error) {
	if n <= 0 || n > MaxAlloc {
		return Nil, ErrInvalidSize
	}
	sz := alignUp(uintptr(n))
	a.mu.Lock()
	if a.released {
		a.mu.Unlock()
		return Nil, ErrReleased
	}
	if sz > a.chunkSize {
		ref, err := a.allocDedicatedLocked(sz)
		if err == nil {
			a.used += int64(sz)
			a.allocs++
		}
		a.mu.Unlock()
		return ref, err
	}
	if a.cur < 0 || a.off+sz > a.tableLocked()[a.cur].size {
		if err := a.nextChunkLocked(); err != nil {
			a.mu.Unlock()
			return Nil, err
		}
	}
	ref := makeRef(a.cur, uint32(a.off))
	a.off += sz
	a.used += int64(sz)
	a.allocs++
	a.mu.Unlock()
	return ref, nil
}

// nextChunkLocked makes the next regular chunk current, reusing a chunk kept
// by Reset when possible.
func (a *Arena) nextChunkLocked() error {
	t := a.tableLocked()
	if a.cur >= 0 {
		a.wasted += int64(t[a.cur].size - a.off)
	}
	if a.curPos+1 < len(a.regs) {
		a.curPos++
		a.cur, a.off = a.regs[a.curPos], 0
		return nil
	}
	p, err := a.reserveLocked(a.chunkSize)
	if err != nil {
		if a.cur >= 0 {
			a.wasted -= int64(t[a.cur].size - a.off) // nothing was abandoned
		}
		return err
	}
	a.cur = a.addChunkLocked(chunk{base: p, size: a.chunkSize})
	a.regs = append(a.regs, a.cur)
	a.curPos = len(a.regs) - 1
	a.off = 0
	a.regular++
	return nil
}
