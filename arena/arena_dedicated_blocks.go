package arena

import (
	"fmt"

	"github.com/vladislavprovich/goarena/internal/cmem"
)

// AllocDedicated allocates n bytes in a chunk of its own, independently of
// the chunk size, and returns its reference (offset 0). Unlike regular
// allocations, a dedicated block can be returned individually with
// [Arena.FreeDedicated]. Slab uses it for objects larger than its biggest
// size class.
func (a *Arena) AllocDedicated(n int) (Ref, error) {
	if n <= 0 || n > MaxAlloc {
		return Nil, ErrInvalidSize
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return Nil, ErrReleased
	}
	ref, err := a.allocDedicatedLocked(alignUp(uintptr(n)))
	if err == nil {
		a.allocs++
	}
	return ref, err
}

// FreeDedicated returns a block obtained from [Arena.AllocDedicated] (or a
// large Alloc) to C. It panics if ref does not refer to the start of a live
// dedicated chunk.
func (a *Arena) FreeDedicated(ref Ref) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return // everything is already gone
	}
	idx := ref.Chunk()
	t := a.tableLocked()
	if idx < 0 || idx >= len(t) || ref.Offset() != 0 || t[idx].base == nil || !t[idx].dedicated {
		panic(fmt.Sprintf("arena: FreeDedicated(%v): not a live dedicated chunk", ref))
	}
	c := t[idx]
	cmem.Free(c.base)
	t[idx] = chunk{}
	a.freeSlots = append(a.freeSlots, idx)
	a.reserved -= int64(c.size)
	a.dedicated--
	a.dedicatedSz -= int64(c.size)
}

func (a *Arena) allocDedicatedLocked(sz uintptr) (Ref, error) {
	p, err := a.reserveLocked(sz)
	if err != nil {
		return Nil, err
	}
	idx := a.addChunkLocked(chunk{base: p, size: sz, dedicated: true})
	a.dedicated++
	a.dedicatedSz += int64(sz)
	return makeRef(idx, 0), nil
}
