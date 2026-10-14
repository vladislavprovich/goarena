package store

import (
	"errors"
	"fmt"
	"hash/maphash"
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vladislavprovich/goarena/arena"
	"github.com/vladislavprovich/goarena/gcpace"
)

// Limits and defaults.
const (
	// DefaultShards is the number of shards used when Config.Shards is 0.
	DefaultShards = 256

	// MaxShards is the largest accepted number of shards.
	MaxShards = 1 << 16

	// MaxKeySize is the largest accepted key, in bytes (the key length is
	// stored in 16 bits).
	MaxKeySize = 1<<16 - 1

	// MaxEntrySize is the largest accepted entry (header + key + value), in
	// bytes.
	MaxEntrySize = 1 << 30

	// MaxLoadFactor is the share of used index slots (live + tombstones)
	// that triggers a rehash.
	MaxLoadFactor = 0.75

	// optimisticTries is how many times a lock-free read is attempted
	// before a reader falls back to the shard lock.
	optimisticTries = 4

	// writerWaitSpins bounds how many times an optimistic reader re-reads
	// an odd sequence number before it counts the attempt as failed.
	writerWaitSpins = 256

	// readerStripes is the number of in-flight reader counters (a power
	// of two); see Store.enterRead.
	readerStripes = 64
)

// Errors returned by Store methods. Methods may wrap them with details
// (for example the sizes involved), so test for them with errors.Is:
//
//	if errors.Is(err, store.ErrNoMemory) { ... }
var (
	// ErrClosed is returned by methods called after Close.
	ErrClosed = errors.New("store: closed")

	// ErrTooLarge is returned when a key or an entry exceeds MaxKeySize or
	// MaxEntrySize.
	ErrTooLarge = errors.New("store: key or value too large")

	// ErrNoMemory is returned when Config.MaxMemory is reached or the C
	// allocator fails. The entry that could not be written is unchanged
	// (an overwrite keeps the old value).
	ErrNoMemory = errors.New("store: out of off-heap memory")
)

// Config configures a [Store]. The zero value is valid.
type Config struct {
	// Shards is the number of independently locked shards. It is rounded
	// up to a power of two. Zero means DefaultShards (256).
	Shards int

	// ChunkSize is the size of the chunks the shared arena requests from
	// the C allocator. Zero means arena.DefaultChunkSize (64 MiB).
	ChunkSize int

	// MaxMemory limits the bytes the store may reserve from C (arena
	// chunks, which hold the entries and the index tables, and large
	// objects). Zero means unlimited.
	MaxMemory int64

	// DefaultTTL is applied by Set and SetString. Zero means entries do
	// not expire.
	DefaultTTL time.Duration

	// InitialCapacity is a hint of the expected number of entries; the
	// per-shard indexes are pre-sized so that this many entries fit
	// without rehashing. Zero means the minimum size.
	InitialCapacity int

	// GCPacing registers the store's off-heap memory with the process-wide
	// GC pacer (package gcpace) for the lifetime of the store: the GC then
	// paces as if the off-heap bytes were part of the Go heap, and an
	// existing GOMEMLIMIT is treated as the budget for Go + off-heap
	// memory. Recommended for services that allocate per request (see
	// docs/GUIDE.md, "GC pacing and GOMEMLIMIT"). The pacer changes GOGC
	// and GOMEMLIMIT; the previous values are restored when the last paced
	// store is closed.
	//
	// In builds without cgo (arena.OffHeap is false) the store's memory is
	// already part of the Go heap and GCPacing has no effect.
	GCPacing bool
}

// nowNano returns the current wall-clock time; tests replace it.
var nowNano = func() int64 { return time.Now().UnixNano() }

// finalized counts stores that were reclaimed by the finalizer without an
// explicit Close (a resource leak in the calling program).
var finalized atomic.Int64

// FinalizedStores returns how many stores were released by the garbage
// collector's finalizer instead of an explicit Close. A growing value
// indicates a missing Close in the calling program.
func FinalizedStores() int64 { return finalized.Load() }

// Store is a concurrent, sharded key-value store whose entries and hash
// index live outside of the Go heap. See the package documentation for the
// design and the safety rules.
//
// A Store must be closed with Close when it is no longer needed.
type Store struct {
	shards     []shard
	readers    []readerStripe
	shardShift uint // 64 - log2(len(shards)); the top bits select a shard
	seed       maphash.Seed
	arena      *arena.Arena
	defaultTTL int64
	maxMemory  int64 // Config.MaxMemory, for error messages
	closed     atomic.Bool
	closeMu    sync.Mutex
	unpace     func() // unregisters from the GC pacer (GCPacing only)
}

// New creates a store.
func New(cfg Config) (*Store, error) {
	n := cfg.Shards
	if n == 0 {
		n = DefaultShards
	}
	if n < 0 || n > MaxShards {
		return nil, fmt.Errorf("store: invalid shard count %d", cfg.Shards)
	}
	n = 1 << bits.Len(uint(n-1)) // round up to a power of two
	if cfg.MaxMemory < 0 || cfg.DefaultTTL < 0 || cfg.InitialCapacity < 0 {
		return nil, errors.New("store: negative MaxMemory, DefaultTTL or InitialCapacity")
	}
	a, err := arena.New(arena.Config{ChunkSize: cfg.ChunkSize, MaxBytes: cfg.MaxMemory})
	if err != nil {
		return nil, err
	}
	per := uint64(cfg.InitialCapacity / n)
	capacity := uint64(minIndexCap)
	for capacity*3 < (per+1)*4 { // keep the initial load factor <= 0.75
		capacity *= 2
	}
	s := &Store{
		shards:     make([]shard, n),
		readers:    make([]readerStripe, readerStripes),
		shardShift: uint(64 - bits.TrailingZeros(uint(n))),
		seed:       maphash.MakeSeed(),
		arena:      a,
		defaultTTL: int64(cfg.DefaultTTL),
		maxMemory:  cfg.MaxMemory,
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.a = a
		sh.slab = arena.NewSlab(a, arena.SlabConfig{RetainLarge: true})
		t, err := newTable(a, capacity)
		if err != nil {
			a.Release()
			return nil, fmt.Errorf("%w: cannot allocate the initial index of %d shards x %d slots (%d bytes); lower Shards or InitialCapacity, or raise MaxMemory",
				ErrNoMemory, n, capacity, int64(n)*int64(capacity)*slotSize)
		}
		sh.tab.Store(t)
	}
	if cfg.GCPacing && arena.OffHeap {
		// The source captures only the arena, not the store, so that a
		// forgotten store can still be finalized.
		s.unpace = gcpace.Register(a.ReservedBytes)
	}
	runtime.SetFinalizer(s, finalizeStore)
	return s, nil
}

// MustNew is like [New] but panics if the configuration is invalid. It is
// meant for tests, examples and package-level initialisation with a
// constant configuration.
func MustNew(cfg Config) *Store {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

func finalizeStore(s *Store) {
	if !s.closed.Load() {
		finalized.Add(1)
		_ = s.Close()
	}
}

// Closed reports whether Close has been called.
func (s *Store) Closed() bool { return s.closed.Load() }

// Close releases all off-heap memory held by the store. It waits for
// in-flight operations, including lock-free readers, to finish.
// Subsequent writes return ErrClosed and reads report "not found". Close
// is idempotent and always returns nil.
func (s *Store) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed.Load() {
		return nil
	}
	if s.unpace != nil {
		s.unpace()
	}
	for i := range s.shards {
		s.shards[i].mu.Lock()
	}
	s.closed.Store(true)
	// Optimistic readers that entered before the flag was set may still be
	// reading arena memory; they never block, so this wait is short.
	for i := range s.readers {
		for s.readers[i].n.Load() != 0 {
			runtime.Gosched()
		}
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.slab = nil
		sh.kvBytes, sh.entryBytes = 0, 0
	}
	s.arena.Release()
	for i := range s.shards {
		s.shards[i].mu.Unlock()
	}
	runtime.SetFinalizer(s, nil)
	return nil
}
