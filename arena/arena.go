package arena

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/savokh/goarena/internal/cmem"
)

// Default and limit values of [Config].
const (
	// DefaultChunkSize is the chunk size used when Config.ChunkSize is 0.
	DefaultChunkSize = 64 << 20 // 64 MiB

	// MinChunkSize is the smallest accepted chunk size.
	MinChunkSize = 4 << 10 // 4 KiB

	// MaxChunkSize is the largest accepted chunk size. Offsets inside a
	// chunk are encoded in the low 32 bits of a [Ref].
	MaxChunkSize = 1 << 31 // 2 GiB

	// Alignment is the alignment, in bytes, of every allocation.
	Alignment = 8

	// MaxAlloc is the largest single allocation an Arena accepts.
	MaxAlloc = 1 << 31
)

// Errors returned by Arena and Slab.
var (
	// ErrOutOfMemory is returned when an allocation would exceed
	// Config.MaxBytes or when the C allocator fails.
	ErrOutOfMemory = errors.New("arena: out of memory")

	// ErrReleased is returned by allocation methods after Release.
	ErrReleased = errors.New("arena: use of released arena")

	// ErrInvalidSize is returned for negative, zero or too large requests.
	ErrInvalidSize = errors.New("arena: invalid allocation size")

	// ErrPointerType is returned by the typed helpers when the type contains
	// Go pointers and therefore must not live in C memory.
	ErrPointerType = errors.New("arena: type contains Go pointers")
)

// Config configures an [Arena]. The zero value is valid.
type Config struct {
	// ChunkSize is the size of every regular chunk requested from C.
	// It is rounded up to a multiple of 4 KiB. Zero means DefaultChunkSize.
	ChunkSize int

	// MaxBytes limits the total number of bytes the arena may reserve from
	// C (regular plus dedicated chunks). Zero means unlimited.
	MaxBytes int64
}

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

// chunk describes one block of C memory owned by the arena.
type chunk struct {
	base      unsafe.Pointer // start of the block (nil = free table slot)
	size      uintptr        // usable size in bytes
	dedicated bool           // true for large-object chunks
}

// chunkTable is an immutable-length table of chunks. A new table is
// published atomically whenever the arena needs more slots, so readers
// (Bytes, Pointer) never take a lock.
type chunkTable struct {
	chunks []chunk
}

// Arena is a chunked bump-pointer allocator over memory obtained from C.
//
// Memory is requested from the C allocator in large chunks (64 MiB by
// default); allocations are carved from the current chunk by advancing an
// offset, which makes Alloc a handful of instructions plus a mutex. Requests
// larger than the chunk size get a dedicated chunk of their own.
//
// Individual allocations are never freed: memory is reclaimed all at once by
// [Arena.Reset] (keep chunks for reuse) or [Arena.Release] (return them to C).
// Use a [Slab] on top of an Arena when objects must be freed individually.
//
// An Arena is safe for concurrent use: Alloc and the other mutating methods
// serialise on an internal mutex, while Bytes and Pointer are lock-free.
// Reset and Release invalidate every Ref and must not run concurrently with
// code that still dereferences old references.
type Arena struct {
	tab atomic.Pointer[chunkTable] // lock-free view of the chunk table

	mu        sync.Mutex
	chunkSize uintptr
	maxBytes  int64
	nChunks   int     // number of used table slots (incl. freed dedicated slots)
	freeSlots []int   // table slots of freed dedicated chunks, for reuse
	regs      []int   // table indices of regular chunks, in use order
	curPos    int     // position of the current chunk in regs (-1 = none)
	cur       int     // table index of the current regular chunk (-1 = none)
	off       uintptr // bump offset inside the current chunk
	released  bool

	// statistics (guarded by mu)
	reserved     int64  // bytes reserved from C (all chunks)
	used         int64  // bytes handed out by Alloc (after alignment)
	wasted       int64  // tail bytes abandoned when switching chunks
	allocs       uint64 // successful Alloc calls
	regular      int    // live regular chunks
	dedicated    int    // live dedicated chunks
	dedicatedSz  int64  // bytes in live dedicated chunks
	chunkMallocs uint64 // chunks requested from C over the lifetime
}

// New creates an empty arena. No memory is reserved until the first
// allocation.
func New(cfg Config) (*Arena, error) {
	cs := cfg.ChunkSize
	if cs == 0 {
		cs = DefaultChunkSize
	}
	if cs < MinChunkSize || cs > MaxChunkSize {
		return nil, fmt.Errorf("arena: chunk size %d out of range [%d, %d]", cs, MinChunkSize, MaxChunkSize)
	}
	if cfg.MaxBytes < 0 {
		return nil, fmt.Errorf("arena: negative MaxBytes %d", cfg.MaxBytes)
	}
	cs = (cs + 4095) &^ 4095
	a := &Arena{chunkSize: uintptr(cs), maxBytes: cfg.MaxBytes, cur: -1, curPos: -1}
	a.tab.Store(&chunkTable{chunks: make([]chunk, 0, 8)})
	return a, nil
}

// MustNew is like [New] but panics on an invalid configuration.
func MustNew(cfg Config) *Arena {
	a, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return a
}

// ChunkSize returns the configured chunk size.
func (a *Arena) ChunkSize() int { return int(a.chunkSize) }

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

// tableLocked returns the current chunk slice; a.mu must be held.
func (a *Arena) tableLocked() []chunk { return a.tab.Load().chunks }

// addChunkLocked stores c in a free slot (or a new one) and returns its index.
func (a *Arena) addChunkLocked(c chunk) int {
	if n := len(a.freeSlots); n > 0 {
		idx := a.freeSlots[n-1]
		a.freeSlots = a.freeSlots[:n-1]
		a.tab.Load().chunks[idx] = c
		return idx
	}
	t := a.tab.Load().chunks
	if len(t) >= 1<<32-2 {
		panic("arena: chunk table exhausted")
	}
	if len(t) == cap(t) {
		// Grow into a bigger copy; readers holding the old table still see
		// valid entries for every chunk that existed before the copy.
		nt := make([]chunk, len(t), 2*cap(t)+8)
		copy(nt, t)
		t = nt
	}
	// The slot at len(t) is beyond the published length, so no reader can
	// observe it before the new table header is stored below.
	t = t[:len(t)+1]
	t[len(t)-1] = c
	a.tab.Store(&chunkTable{chunks: t})
	a.nChunks = len(t)
	return len(t) - 1
}

func (a *Arena) reserveLocked(sz uintptr) (unsafe.Pointer, error) {
	if a.maxBytes > 0 && a.reserved+int64(sz) > a.maxBytes {
		return nil, ErrOutOfMemory
	}
	p := cmem.Malloc(sz)
	if p == nil {
		return nil, ErrOutOfMemory
	}
	a.reserved += int64(sz)
	a.chunkMallocs++
	return p, nil
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

// Reset invalidates every allocation but keeps the regular chunks for reuse;
// dedicated chunks are returned to C. Reset after Release is a no-op.
func (a *Arena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return
	}
	old := a.tableLocked()
	kept := make([]chunk, 0, cap(old))
	for _, c := range old {
		switch {
		case c.base == nil:
		case c.dedicated:
			cmem.Free(c.base)
			a.reserved -= int64(c.size)
		default:
			kept = append(kept, c)
		}
	}
	a.tab.Store(&chunkTable{chunks: kept})
	a.nChunks = len(kept)
	a.freeSlots = a.freeSlots[:0]
	a.regs = a.regs[:0]
	for i := range kept {
		a.regs = append(a.regs, i)
	}
	a.cur, a.curPos, a.off = -1, -1, 0
	a.used, a.wasted = 0, 0
	a.dedicated, a.dedicatedSz = 0, 0
}

// Release returns all memory to C. The arena cannot be used afterwards;
// allocation methods return ErrReleased. Release is idempotent.
func (a *Arena) Release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return
	}
	for _, c := range a.tableLocked() {
		if c.base != nil {
			cmem.Free(c.base)
		}
	}
	a.tab.Store(&chunkTable{})
	a.released = true
	a.nChunks, a.freeSlots, a.regs = 0, nil, nil
	a.cur, a.curPos, a.off = -1, -1, 0
	a.reserved, a.used, a.wasted = 0, 0, 0
	a.regular, a.dedicated, a.dedicatedSz = 0, 0, 0
}

// ReservedBytes returns the number of bytes currently reserved from C. It
// is cheaper than [Arena.Stats] and safe for concurrent use.
func (a *Arena) ReservedBytes() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reserved
}

// Released reports whether Release has been called.
func (a *Arena) Released() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.released
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
