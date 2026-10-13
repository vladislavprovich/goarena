package main

import (
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"runtime/debug"
	"sort"
	"sync"

	"github.com/savokh/goarena/store"
)

// Cache is the minimal interface every compared implementation provides.
// All implementations copy the value on Set and copy it into a caller-owned
// buffer on Get, so that the benchmark compares equivalent semantics: the
// caller owns the bytes it receives and may modify them.
type Cache interface {
	// Set stores a copy of val under key.
	Set(key, val []byte) error
	// Get appends the value stored under key to dst[:0].
	Get(dst, key []byte) ([]byte, bool)
	// OffHeapBytes reports memory held outside of the Go heap (0 for
	// pure-Go implementations).
	OffHeapBytes() int64
	// Close releases resources.
	Close()
}

// implInfo describes one implementation.
type implInfo struct {
	Name        string
	Description string
	New         func(o options) (Cache, error)
}

var impls = map[string]implInfo{
	"gomap": {
		Name:        "gomap",
		Description: "sharded map[string][]byte + RWMutex, GOGC=100",
		New:         func(o options) (Cache, error) { return newGoMap(o.Shards), nil },
	},
	"gomap-gogc400": {
		Name:        "gomap-gogc400",
		Description: "same as gomap with debug.SetGCPercent(400)",
		New: func(o options) (Cache, error) {
			debug.SetGCPercent(400)
			return newGoMap(o.Shards), nil
		},
	},
	"syncmap": {
		Name:        "syncmap",
		Description: "sync.Map with string keys and []byte values",
		New:         func(o options) (Cache, error) { return &syncMap{}, nil },
	},
	"gobytes": {
		Name:        "gobytes",
		Description: "pointer-free Go-heap cache a la bigcache: map[uint64]uint64 index + []byte chunks",
		New:         func(o options) (Cache, error) { return newGoBytes(o.Shards), nil },
	},
	"goarena-gogc400": {
		Name:        "goarena-gogc400",
		Description: "goarena with debug.SetGCPercent(400) - the same GC tuning as gomap-gogc400",
		New: func(o options) (Cache, error) {
			debug.SetGCPercent(400)
			return newArenaCache(o)
		},
	},
	"goarena-budget": {
		Name: "goarena-budget",
		Description: "goarena with GOGC=off and GOMEMLIMIT = memory budget - off-heap bytes " +
			"(store.GoMemoryBudget); requires -memlimit",
		New: func(o options) (Cache, error) {
			if o.MemLimitMiB <= 0 {
				return nil, fmt.Errorf("goarena-budget needs -memlimit")
			}
			c, err := newArenaCache(o)
			if err != nil {
				return nil, err
			}
			c.budget = int64(o.MemLimitMiB) << 20
			debug.SetGCPercent(-1)
			c.applyBudget()
			return c, nil
		},
	},
	"goarena-paced": {
		Name: "goarena-paced",
		Description: "goarena with store.Config.GCPacing: GOGC applied as if off-heap bytes were heap, " +
			"GOMEMLIMIT (if set) treated as the total budget",
		New: func(o options) (Cache, error) {
			s, err := store.New(store.Config{Shards: o.Shards, ChunkSize: o.ChunkMiB << 20, GCPacing: true})
			if err != nil {
				return nil, err
			}
			return &arenaCache{s: s}, nil
		},
	},
	"goarena": {
		Name:        "goarena",
		Description: "goarena/store: off-heap slab entries + off-heap open-addressing index",
		New:         func(o options) (Cache, error) { return newArenaCache(o) },
	},
}

func newArenaCache(o options) (*arenaCache, error) {
	s, err := store.New(store.Config{Shards: o.Shards, ChunkSize: o.ChunkMiB << 20})
	if err != nil {
		return nil, err
	}
	return &arenaCache{s: s}, nil
}

func implNames() []string {
	names := make([]string, 0, len(impls))
	for n := range impls {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------- gomap

type goMapShard struct {
	mu sync.RWMutex
	m  map[string][]byte
	_  [40]byte // padding against false sharing
}

type goMap struct {
	seed   maphash.Seed
	shards []goMapShard
	mask   uint64
}

func newGoMap(n int) *goMap {
	g := &goMap{seed: maphash.MakeSeed(), shards: make([]goMapShard, n), mask: uint64(n - 1)}
	for i := range g.shards {
		g.shards[i].m = make(map[string][]byte)
	}
	return g
}

func (g *goMap) shard(key []byte) *goMapShard {
	return &g.shards[maphash.Bytes(g.seed, key)&g.mask]
}

func (g *goMap) Set(key, val []byte) error {
	v := append([]byte(nil), val...) // the value must be owned by the map
	sh := g.shard(key)
	sh.mu.Lock()
	sh.m[string(key)] = v
	sh.mu.Unlock()
	return nil
}

func (g *goMap) Get(dst, key []byte) ([]byte, bool) {
	sh := g.shard(key)
	sh.mu.RLock()
	v, ok := sh.m[string(key)] // no allocation: compiler optimisation
	sh.mu.RUnlock()
	if !ok {
		return dst[:0], false
	}
	// Values are never mutated in place (Set stores a fresh slice), so the
	// copy may happen outside of the lock.
	return append(dst[:0], v...), true
}

func (g *goMap) OffHeapBytes() int64 { return 0 }
func (g *goMap) Close()              {}

// -------------------------------------------------------------- syncmap

type syncMap struct{ m sync.Map }

func (s *syncMap) Set(key, val []byte) error {
	s.m.Store(string(key), append([]byte(nil), val...))
	return nil
}

func (s *syncMap) Get(dst, key []byte) ([]byte, bool) {
	v, ok := s.m.Load(string(key))
	if !ok {
		return dst[:0], false
	}
	return append(dst[:0], v.([]byte)...), true
}

func (s *syncMap) OffHeapBytes() int64 { return 0 }
func (s *syncMap) Close()              {}

// -------------------------------------------------------------- gobytes

// goBytesChunk is the size of the []byte blocks entries are appended to.
const goBytesChunk = 256 << 10

// goBytesShard stores entries as [keyLen u32][valLen u32][key][value] in
// large []byte chunks and indexes them with a map[uint64]uint64 from the
// key hash to (chunk<<32 | offset). Neither the map nor the chunks contain
// pointers, so the garbage collector does not scan them - the technique
// used by bigcache and freecache to keep the GC cost independent of the
// number of entries while staying entirely inside the Go heap.
type goBytesShard struct {
	mu     sync.RWMutex
	index  map[uint64]uint64
	chunks [][]byte
	off    int
	_      [24]byte
}

type goBytes struct {
	seed   maphash.Seed
	shards []goBytesShard
	shift  uint
}

func newGoBytes(n int) *goBytes {
	g := &goBytes{seed: maphash.MakeSeed(), shards: make([]goBytesShard, n)}
	for n > 1 {
		n >>= 1
		g.shift++
	}
	g.shift = 64 - g.shift
	for i := range g.shards {
		g.shards[i].index = make(map[uint64]uint64)
	}
	return g
}

func (g *goBytes) shard(h uint64) *goBytesShard {
	if len(g.shards) == 1 {
		return &g.shards[0]
	}
	return &g.shards[h>>g.shift]
}

func (sh *goBytesShard) entry(loc uint64) []byte {
	return sh.chunks[loc>>32][uint32(loc):]
}

func (g *goBytes) Set(key, val []byte) error {
	h := maphash.Bytes(g.seed, key)
	sh := g.shard(h)
	need := 8 + len(key) + len(val)
	if need > goBytesChunk {
		return fmt.Errorf("gobytes: entry of %d bytes too large", need)
	}
	sh.mu.Lock()
	if loc, ok := sh.index[h]; ok {
		e := sh.entry(loc)
		kl := int(binary.LittleEndian.Uint32(e))
		vl := int(binary.LittleEndian.Uint32(e[4:]))
		if kl == len(key) && vl == len(val) && string(e[8:8+kl]) == string(key) {
			copy(e[8+kl:], val) // same size: overwrite in place
			sh.mu.Unlock()
			return nil
		}
		// different size or hash collision: append a new copy (the old
		// bytes become garbage inside the chunk, as in bigcache)
	}
	if len(sh.chunks) == 0 || sh.off+need > goBytesChunk {
		sh.chunks = append(sh.chunks, make([]byte, goBytesChunk))
		sh.off = 0
	}
	ci := len(sh.chunks) - 1
	e := sh.chunks[ci][sh.off:]
	binary.LittleEndian.PutUint32(e, uint32(len(key)))
	binary.LittleEndian.PutUint32(e[4:], uint32(len(val)))
	copy(e[8:], key)
	copy(e[8+len(key):], val)
	sh.index[h] = uint64(ci)<<32 | uint64(sh.off)
	sh.off += (need + 7) &^ 7
	sh.mu.Unlock()
	return nil
}

func (g *goBytes) Get(dst, key []byte) ([]byte, bool) {
	h := maphash.Bytes(g.seed, key)
	sh := g.shard(h)
	sh.mu.RLock()
	loc, ok := sh.index[h]
	if !ok {
		sh.mu.RUnlock()
		return dst[:0], false
	}
	e := sh.entry(loc)
	kl := int(binary.LittleEndian.Uint32(e))
	vl := int(binary.LittleEndian.Uint32(e[4:]))
	if string(e[8:8+kl]) != string(key) { // hash collision
		sh.mu.RUnlock()
		return dst[:0], false
	}
	// in-place overwrites happen under the write lock, so copy under RLock
	dst = append(dst[:0], e[8+kl:8+kl+vl]...)
	sh.mu.RUnlock()
	return dst, true
}

func (g *goBytes) OffHeapBytes() int64 { return 0 }
func (g *goBytes) Close()              {}

// -------------------------------------------------------------- goarena

type arenaCache struct {
	s      *store.Store
	budget int64 // total memory budget (goarena-budget only)
	goLim  int64 // Go memory limit applied last
}

// applyBudget sets GOMEMLIMIT to the budget minus the store's off-heap
// memory (at least 64 MiB, so that a too small budget degrades into
// frequent GC rather than a stall).
func (a *arenaCache) applyBudget() {
	if a.budget <= 0 {
		return
	}
	lim := a.s.GoMemoryBudget(a.budget)
	if lim < 64<<20 {
		lim = 64 << 20
	}
	debug.SetMemoryLimit(lim)
	a.goLim = lim
}

// afterPopulate re-applies the budget once the data set is loaded.
func (a *arenaCache) afterPopulate() int64 {
	a.applyBudget()
	return a.goLim
}

func (a *arenaCache) Set(key, val []byte) error          { return a.s.Set(key, val) }
func (a *arenaCache) Get(dst, key []byte) ([]byte, bool) { return a.s.GetInto(dst, key) }
func (a *arenaCache) OffHeapBytes() int64                { return a.s.Stats().OffHeapBytes }
func (a *arenaCache) Close()                             { _ = a.s.Close() }
func (a *arenaCache) storeStats() store.Stats            { return a.s.Stats() }
