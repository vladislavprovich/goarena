package arena

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/vladislavprovich/goarena/internal/cmem"
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

// Released reports whether Release has been called.
func (a *Arena) Released() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.released
}
