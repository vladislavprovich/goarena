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
	"unsafe"

	"github.com/savokh/goarena/arena"
	"github.com/savokh/goarena/gcpace"
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

// Errors returned by Store methods.
var (
	// ErrClosed is returned by methods called after Close.
	ErrClosed = errors.New("store: closed")

	// ErrTooLarge is returned when a key or an entry exceeds MaxKeySize or
	// MaxEntrySize.
	ErrTooLarge = errors.New("store: key or value too large")

	// ErrNoMemory is returned when Config.MaxMemory is reached or the C
	// allocator fails.
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
	// README, "Recommended configuration"). The pacer changes GOGC and
	// GOMEMLIMIT; the previous values are restored when the last paced
	// store is closed.
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

// shard is one independently locked partition of the store. Its index and
// entry data live in C memory; the struct itself holds only integers, the
// lock, an atomic pointer to the immutable table descriptor and two
// pointers to Go objects. The layout keeps the fields read by lock-free
// readers (first cache line) apart from the writer-only state, and the
// struct size is a multiple of 64 bytes so that shards never share a line.
type shard struct {
	// --- cache line 0: read by optimistic readers
	seq atomic.Uint64              // even: stable; odd: a writer is mutating
	tab atomic.Pointer[indexTable] // current index table
	a   *arena.Arena               // shared arena (chunk table for refs)
	_   [40]byte

	// --- cache lines 1-2: writer state (guarded by mu)
	mu         shardLock // writers, and readers on the slow path
	slab       *arena.Slab
	live       uint64   // slots holding entries
	tombs      uint64   // tombstone slots
	kvBytes    int64    // sum of len(key)+len(value) of live entries
	entryBytes int64    // sum of entry sizes (incl. headers) of live entries
	sets       uint64   // successful Set calls
	deletes    uint64   // successful Delete calls
	expired    uint64   // entries removed because their TTL elapsed
	_          [56]byte // pad the struct to 192 bytes (3 cache lines)
}

// beginWrite and endWrite bracket every mutation of index slots or entry
// memory (sh.mu must be held): the odd sequence number tells optimistic
// readers that what they read may be inconsistent.
func (sh *shard) beginWrite() { sh.seq.Add(1) }
func (sh *shard) endWrite()   { sh.seq.Add(1) }

// readerStripe counts optimistic readers in flight; padded to a line.
type readerStripe struct {
	n atomic.Int64
	_ [56]byte
}

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
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.a = a
		sh.slab = arena.NewSlab(a, arena.SlabConfig{RetainLarge: true})
		t, err := newTable(a, capacity)
		if err != nil {
			a.Release()
			return nil, ErrNoMemory
		}
		sh.tab.Store(t)
	}
	if cfg.GCPacing {
		// The source captures only the arena, not the store, so that a
		// forgotten store can still be finalized.
		s.unpace = gcpace.Register(a.ReservedBytes)
	}
	runtime.SetFinalizer(s, finalizeStore)
	return s, nil
}

func finalizeStore(s *Store) {
	if !s.closed.Load() {
		finalized.Add(1)
		_ = s.Close()
	}
}

// hash computes the remapped 64-bit hash of key.
func (s *Store) hash(key []byte) uint64 { return fixHash(maphash.Bytes(s.seed, key)) }

// shardFor selects the shard for hash h using its top bits (the index uses
// the low bits, so the two choices are independent).
func (s *Store) shardFor(h uint64) *shard {
	if len(s.shards) == 1 {
		return &s.shards[0]
	}
	return &s.shards[h>>s.shardShift]
}

// enterRead registers an optimistic reader and returns its stripe, or nil
// if the store is closed. Close waits until every stripe is zero before it
// releases memory, so an optimistic reader never touches freed chunks.
//
// The stripe is chosen from the address of a stack variable, which differs
// between goroutines; readers of the same hot key therefore do not share a
// counter (unlike the reader count of a sync.RWMutex, which every reader
// of a shard increments). The address is only used as a hash.
func (s *Store) enterRead() *atomic.Int64 {
	var marker byte
	x := uint64(uintptr(unsafe.Pointer(&marker))) >> 11
	c := &s.readers[(x*0x9E3779B97F4A7C15)>>(64-6)].n
	c.Add(1)
	if s.closed.Load() {
		c.Add(-1)
		return nil
	}
	return c
}

// entryAt returns a view of the entry referenced by ref (locked paths only).
func (sh *shard) entryAt(ref uint64) entry { return entry{p: sh.a.Pointer(arena.Ref(ref))} }

// find looks key up under the shard lock. It returns the table, the slot
// position and the entry when found.
func (sh *shard) find(h uint64, key []byte) (*indexTable, uint64, entry, bool) {
	t := sh.tab.Load()
	i := h & t.mask
	for {
		sl := t.at(i)
		switch sl.hash {
		case hashEmpty:
			return t, i, entry{}, false
		case h:
			if e := sh.entryAt(sl.ref); e.keyEqual(key) {
				return t, i, e, true
			}
		}
		i = (i + 1) & t.mask
	}
}

// Results of an optimistic lookup.
const (
	optRetry = iota // inconsistent snapshot: try again or fall back
	optMiss         // key absent (or expired) in a consistent snapshot
	optHit          // value copied in a consistent snapshot
)

// readOptimistic looks key up without taking the shard lock (seqlock
// reader). It reads the sequence number, probes the index with atomic slot
// loads, bounds-checks every reference it follows (a torn read may produce
// garbage, which must be rejected rather than dereferenced blindly),
// validates header and key against the sequence number before trusting the
// value length, copies the value into dst when wantValue is set and
// re-validates at the end. Memory it reads is never unmapped while the
// store is open (see enterRead and DESIGN_NOTES.md).
func (sh *shard) readOptimistic(h uint64, key, dst []byte, wantValue bool) ([]byte, int) {
	seq := sh.seq.Load()
	for spins := 0; seq&1 != 0; spins++ {
		// A writer is inside its (short) critical section: wait for it
		// with plain loads instead of failing the attempt immediately.
		if spins == writerWaitSpins {
			return dst, optRetry
		}
		seq = sh.seq.Load()
	}
	t := sh.tab.Load()
	mask := t.mask
	i := h & mask
	for n := uint64(0); n <= mask; n++ {
		sp := unsafe.Add(t.slots, i*slotSize)
		hv := atomic.LoadUint64((*uint64)(sp))
		if hv == h {
			ref := arena.Ref(atomic.LoadUint64((*uint64)(unsafe.Add(sp, 8))))
			p, ok := sh.a.TryPointer(ref, entryHeaderSize)
			if !ok {
				return dst, optRetry
			}
			// Read every header field exactly once: the memory may change
			// under us, so all later accesses must be derived from these
			// locals and stay inside the bounds-checked range.
			kl := int(*(*uint16)(p))
			fl := *(*uint16)(unsafe.Add(p, 2))
			vl := int(*(*uint32)(unsafe.Add(p, 4)))
			hl := entryHeaderSize + int(fl&flagTTL)*ttlFieldSize
			if _, ok := sh.a.TryPointer(ref, hl+kl+vl); !ok {
				return dst, optRetry
			}
			if kl == len(key) && string(unsafe.Slice((*byte)(unsafe.Add(p, hl)), kl)) == string(key) {
				var exp int64
				if fl&flagTTL != 0 {
					exp = *(*int64)(unsafe.Add(p, entryHeaderSize))
				}
				if sh.seq.Load() != seq { // header and key were consistent?
					return dst, optRetry
				}
				if exp != 0 && nowNano() >= exp {
					return dst, optMiss
				}
				if wantValue {
					if cap(dst) < vl {
						dst = make([]byte, 0, vl)
					}
					dst = append(dst[:0], unsafe.Slice((*byte)(unsafe.Add(p, hl+kl)), vl)...)
					if sh.seq.Load() != seq {
						return dst, optRetry
					}
				}
				return dst, optHit
			}
		} else if hv == hashEmpty {
			if sh.seq.Load() != seq {
				return dst, optRetry
			}
			return dst, optMiss
		}
		i = (i + 1) & mask
	}
	return dst, optRetry
}

// lookup is the common read path of GetInto, Get, GetString and Has: up to
// optimisticTries lock-free attempts, then the locked slow path.
func (s *Store) lookup(key, dst []byte, wantValue bool) ([]byte, bool) {
	h := s.hash(key)
	sh := s.shardFor(h)
	if c := s.enterRead(); c != nil {
		for try := 0; try < optimisticTries; try++ {
			out, st := sh.readOptimistic(h, key, dst, wantValue)
			if st != optRetry {
				c.Add(-1)
				if st == optHit {
					return out, true
				}
				return out[:0], false
			}
			dst = out
		}
		c.Add(-1) // leave before blocking on the lock (Close waits for us)
	}
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return dst[:0], false
	}
	_, _, e, found := sh.find(h, key)
	if !found || e.expired(nowNano()) {
		sh.mu.Unlock()
		return dst[:0], false
	}
	if wantValue {
		dst = append(dst[:0], e.value()...)
	}
	sh.mu.Unlock()
	return dst, true
}

func checkSize(key, val []byte) error {
	if len(key) > MaxKeySize || entrySize(len(key), len(val), true) > MaxEntrySize {
		return ErrTooLarge
	}
	return nil
}

func mapAllocErr(err error) error {
	if errors.Is(err, arena.ErrOutOfMemory) {
		return ErrNoMemory
	}
	if errors.Is(err, arena.ErrReleased) {
		return ErrClosed
	}
	return err
}

// Set stores val under key, replacing any previous value. The default TTL
// of the store applies. Both slices are copied; the caller may reuse them.
func (s *Store) Set(key, val []byte) error {
	var exp int64
	if s.defaultTTL > 0 {
		exp = nowNano() + s.defaultTTL
	}
	return s.set(key, val, exp)
}

// SetWithTTL stores val under key with the given time to live. A ttl <= 0
// means the entry never expires.
func (s *Store) SetWithTTL(key, val []byte, ttl time.Duration) error {
	var exp int64
	if ttl > 0 {
		exp = nowNano() + int64(ttl)
	}
	return s.set(key, val, exp)
}

func (s *Store) set(key, val []byte, exp int64) error {
	if err := checkSize(key, val); err != nil {
		return err
	}
	h := s.hash(key)
	sh := s.shardFor(h)
	size := entrySize(len(key), len(val), exp != 0)
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return ErrClosed
	}
	t, pos, e, found := sh.find(h, key)
	if found {
		oldSize := e.size()
		oldKV := oldSize - e.headerLen()
		if sh.slab.BlockSize(oldSize) == sh.slab.BlockSize(size) {
			// Same block size: overwrite in place, no allocation.
			sh.beginWrite()
			e.write(key, val, exp)
			sh.endWrite()
		} else {
			ref, err := sh.slab.Alloc(size)
			if err != nil {
				sh.mu.Unlock()
				return mapAllocErr(err)
			}
			entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
			sl := t.at(pos)
			old := arena.Ref(sl.ref)
			sh.beginWrite()
			sl.ref = uint64(ref)
			sh.slab.Free(old, oldSize) // overwrites the old block's first word
			sh.endWrite()
		}
		sh.kvBytes += int64(len(key) + len(val) - oldKV)
		sh.entryBytes += int64(size - oldSize)
		sh.sets++
		sh.mu.Unlock()
		return nil
	}
	ref, err := sh.slab.Alloc(size)
	if err != nil {
		sh.mu.Unlock()
		return mapAllocErr(err)
	}
	entry{p: sh.a.Pointer(ref)}.write(key, val, exp) // not yet visible
	sh.beginWrite()
	if sh.needsRehash(t) {
		if err := sh.rehash(); err != nil {
			sh.slab.Free(ref, size)
			sh.endWrite()
			sh.mu.Unlock()
			return mapAllocErr(err)
		}
		t = sh.tab.Load()
	}
	sh.insertAt(t, firstFree(t, h), h, uint64(ref))
	sh.endWrite()
	sh.kvBytes += int64(len(key) + len(val))
	sh.entryBytes += int64(size)
	sh.sets++
	sh.mu.Unlock()
	return nil
}

// Get returns a copy of the value stored under key. It reports false if the
// key is absent, expired, or the store is closed.
func (s *Store) Get(key []byte) ([]byte, bool) {
	v, ok := s.lookup(key, nil, true)
	if ok && v == nil {
		v = []byte{}
	}
	return v, ok
}

// GetInto appends the value stored under key to dst[:0] and returns the
// result, reusing dst's capacity. It allocates only if dst is too small,
// which makes it the preferred read method on hot paths. Reads normally
// complete without taking any lock (see the package documentation).
func (s *Store) GetInto(dst, key []byte) ([]byte, bool) {
	return s.lookup(key, dst, true)
}

// View calls fn with a zero-copy view of the value stored under key, while
// holding the shard lock, and reports whether the key was found. Writers of
// the same shard wait while fn runs, so fn should be short.
//
// The slice passed to fn aliases off-heap memory: fn must not retain it or
// any sub-slice after it returns, must not modify it, and must not call
// methods of the same Store (that may deadlock). Copy the data if it is
// needed later.
func (s *Store) View(key []byte, fn func(val []byte)) bool {
	h := s.hash(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	defer sh.mu.Unlock() // fn may panic
	if s.closed.Load() {
		return false
	}
	_, _, e, found := sh.find(h, key)
	if !found || e.expired(nowNano()) {
		return false
	}
	fn(e.value())
	return true
}

// Has reports whether key is present and not expired.
func (s *Store) Has(key []byte) bool {
	_, ok := s.lookup(key, nil, false)
	return ok
}

// Delete removes key and reports whether a live (non-expired) entry was
// removed.
func (s *Store) Delete(key []byte) bool {
	h := s.hash(key)
	sh := s.shardFor(h)
	sh.mu.Lock()
	if s.closed.Load() {
		sh.mu.Unlock()
		return false
	}
	t, pos, e, found := sh.find(h, key)
	if !found {
		sh.mu.Unlock()
		return false
	}
	live := !e.expired(nowNano())
	sh.beginWrite()
	sh.removeAt(t, pos, e)
	sh.endWrite()
	if live {
		sh.deletes++
	} else {
		sh.expired++
	}
	sh.mu.Unlock()
	return live
}

// removeAt frees the entry e stored in slot pos. sh.mu must be held and the
// sequence number must be odd.
func (sh *shard) removeAt(t *indexTable, pos uint64, e entry) {
	size := e.size()
	ref := arena.Ref(t.at(pos).ref)
	sh.kvBytes -= int64(size - e.headerLen())
	sh.entryBytes -= int64(size)
	sh.remove(t, pos)
	sh.slab.Free(ref, size)
}

// EvictExpired removes all entries whose TTL has elapsed and returns their
// number. Expired entries are invisible to readers even before eviction;
// calling EvictExpired periodically returns their memory to the slabs.
func (s *Store) EvictExpired() int {
	now := nowNano()
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		if s.closed.Load() {
			sh.mu.Unlock()
			return n
		}
		t := sh.tab.Load()
		sh.beginWrite()
		for j := uint64(0); j <= t.mask; j++ {
			sl := t.at(j)
			if sl.hash < 2 {
				continue
			}
			if e := sh.entryAt(sl.ref); e.expired(now) {
				// remove never moves entries, so the scan can continue
				sh.removeAt(t, j, e)
				sh.expired++
				n++
			}
		}
		sh.endWrite()
		sh.mu.Unlock()
	}
	return n
}

// Len returns the number of entries, including expired entries that have
// not been evicted yet.
func (s *Store) Len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n += int(sh.live)
		sh.mu.Unlock()
	}
	return n
}

// Range calls fn for every live entry, shard by shard, until fn returns
// false. Each shard is locked while its entries are visited, so fn sees a
// consistent view of one shard at a time (and writers of that shard wait). The key and value slices
// alias off-heap memory and are valid only during the call; fn must not
// call methods of the same Store.
func (s *Store) Range(fn func(key, val []byte) bool) {
	now := nowNano()
	for i := range s.shards {
		sh := &s.shards[i]
		if !s.rangeShard(sh, now, fn) {
			return
		}
	}
}

func (s *Store) rangeShard(sh *shard, now int64, fn func(key, val []byte) bool) bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s.closed.Load() {
		return false
	}
	t := sh.tab.Load()
	for j := uint64(0); j <= t.mask; j++ {
		sl := t.at(j)
		if sl.hash < 2 {
			continue
		}
		e := sh.entryAt(sl.ref)
		if e.expired(now) {
			continue
		}
		if !fn(e.key(), e.value()) {
			return false
		}
	}
	return true
}

// SetString is Set for string keys and values. The strings are read in
// place (no conversion copy) and copied into the store.
func (s *Store) SetString(key, val string) error {
	return s.Set(unsafe.Slice(unsafe.StringData(key), len(key)), unsafe.Slice(unsafe.StringData(val), len(val)))
}

// GetString is Get for string keys and values.
func (s *Store) GetString(key string) (string, bool) {
	v, ok := s.lookup(unsafe.Slice(unsafe.StringData(key), len(key)), nil, true)
	if !ok || len(v) == 0 {
		return "", ok
	}
	return unsafe.String(unsafe.SliceData(v), len(v)), true // v is a fresh copy
}

// DeleteString is Delete for string keys.
func (s *Store) DeleteString(key string) bool {
	return s.Delete(unsafe.Slice(unsafe.StringData(key), len(key)))
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
