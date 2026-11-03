package arena

import (
	"fmt"
	"unsafe"
)

// Ref is a compact, GC-invisible reference to memory inside an [Arena]:
//
//	bits 63..32  chunk index + 1   (0 means "nil")
//	bits 31..0   byte offset inside the chunk
//
// A Ref is a plain integer, so it can be stored in C memory, in pointer-free
// Go structures (which the garbage collector never scans) or on disk.
type Ref uint64

// Nil is the zero Ref; it never refers to valid memory.
const Nil Ref = 0

// makeRef builds a Ref from a chunk index and an offset.
func makeRef(chunk int, off uint32) Ref { return Ref(uint64(chunk+1)<<32 | uint64(off)) }

// Chunk returns the chunk index encoded in r (-1 for Nil).
func (r Ref) Chunk() int { return int(uint32(r>>32)) - 1 }

// Offset returns the byte offset encoded in r.
func (r Ref) Offset() uint32 { return uint32(r) }

// IsNil reports whether r is the nil reference.
func (r Ref) IsNil() bool { return r == Nil }

// Add returns r advanced by n bytes inside the same chunk. The caller is
// responsible for staying within the chunk.
func (r Ref) Add(n int) Ref { return r + Ref(n) }

// String implements fmt.Stringer.
func (r Ref) String() string {
	if r == Nil {
		return "Ref(nil)"
	}
	return fmt.Sprintf("Ref(%d:%d)", r.Chunk(), r.Offset())
}

// resolve validates (ref, n) and returns the address of the first byte.
func (a *Arena) resolve(ref Ref, n int) unsafe.Pointer {
	t := a.tab.Load().chunks
	idx := uint32(ref>>32) - 1 // Nil wraps around to MaxUint32
	if uint64(idx) >= uint64(len(t)) {
		panic(fmt.Sprintf("arena: invalid reference %v", ref))
	}
	c := &t[idx]
	off := uintptr(uint32(ref))
	if c.base == nil || n < 0 || off+uintptr(n) > c.size {
		panic(fmt.Sprintf("arena: reference %v with length %d out of bounds", ref, n))
	}
	return unsafe.Add(c.base, off)
}

// TryPointer is a non-panicking variant of [Arena.Pointer] that also checks
// that n bytes starting at ref lie inside a live chunk. It returns false
// for Nil, unknown chunks and out-of-range lengths.
//
// It exists for optimistic (lock-free, validated-afterwards) readers such
// as the store's seqlock read path, which may observe a torn or stale Ref
// and must reject it instead of panicking. TryPointer never writes and is
// safe to call concurrently with Alloc.
func (a *Arena) TryPointer(ref Ref, n int) (unsafe.Pointer, bool) {
	t := a.tab.Load().chunks
	idx := uint32(ref>>32) - 1
	if uint64(idx) >= uint64(len(t)) || n < 0 {
		return nil, false
	}
	c := &t[idx]
	off := uintptr(uint32(ref))
	if c.base == nil || off+uintptr(n) > c.size {
		return nil, false
	}
	return unsafe.Add(c.base, off), true
}

// Bytes returns an n-byte slice that aliases the arena memory at ref.
//
// The slice is a view, not a copy: it becomes invalid (dangling) after the
// memory is freed, after Reset and after Release. Never store it, and never
// write Go pointers into it. Bytes panics if ref is Nil or the range is
// outside of the chunk.
func (a *Arena) Bytes(ref Ref, n int) []byte {
	p := a.resolve(ref, n)
	return unsafe.Slice((*byte)(p), n)
}

// Pointer returns the raw address of ref. The same validity rules as for
// [Arena.Bytes] apply.
func (a *Arena) Pointer(ref Ref) unsafe.Pointer { return a.resolve(ref, 0) }
