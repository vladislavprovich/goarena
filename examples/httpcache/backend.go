package main

import (
	"hash/maphash"
	"sync"

	"github.com/savokh/goarena/store"
)

// Backend is the storage used by the HTTP service. The goarena backend is
// the subject of the example; the gomap backend exists so that the two can
// be compared side by side on the dashboard.
type Backend interface {
	Name() string
	Set(key, val []byte) error
	GetInto(dst, key []byte) ([]byte, bool)
	Delete(key []byte) bool
	Len() int
	// OffHeapBytes returns memory held outside of the Go heap.
	OffHeapBytes() int64
	// Details returns backend-specific statistics for /stats (may be nil).
	Details() any
	Close() error
}

// ------------------------------------------------------------- goarena

type arenaBackend struct{ s *store.Store }

func newArenaBackend(shards, chunkMiB int) (*arenaBackend, error) {
	s, err := store.New(store.Config{Shards: shards, ChunkSize: chunkMiB << 20})
	if err != nil {
		return nil, err
	}
	return &arenaBackend{s: s}, nil
}

func (b *arenaBackend) Name() string                         { return "goarena" }
func (b *arenaBackend) Set(k, v []byte) error                { return b.s.Set(k, v) }
func (b *arenaBackend) GetInto(dst, k []byte) ([]byte, bool) { return b.s.GetInto(dst, k) }
func (b *arenaBackend) Delete(k []byte) bool                 { return b.s.Delete(k) }
func (b *arenaBackend) Len() int                             { return b.s.Len() }
func (b *arenaBackend) Close() error                         { return b.s.Close() }
func (b *arenaBackend) OffHeapBytes() int64                  { return b.s.Stats().OffHeapBytes }
func (b *arenaBackend) Details() any                         { st := b.s.Stats(); return &st }

// --------------------------------------------------------------- gomap

type mapShard struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// mapBackend is a conventional sharded map[string][]byte cache in the Go
// heap: every entry is two heap objects that the GC must trace.
type mapBackend struct {
	seed   maphash.Seed
	shards []mapShard
}

func newMapBackend(shards int) *mapBackend {
	b := &mapBackend{seed: maphash.MakeSeed(), shards: make([]mapShard, shards)}
	for i := range b.shards {
		b.shards[i].m = make(map[string][]byte)
	}
	return b
}

func (b *mapBackend) shard(k []byte) *mapShard {
	return &b.shards[maphash.Bytes(b.seed, k)%uint64(len(b.shards))]
}

func (b *mapBackend) Name() string { return "gomap" }

func (b *mapBackend) Set(k, v []byte) error {
	c := append([]byte(nil), v...)
	sh := b.shard(k)
	sh.mu.Lock()
	sh.m[string(k)] = c
	sh.mu.Unlock()
	return nil
}

func (b *mapBackend) GetInto(dst, k []byte) ([]byte, bool) {
	sh := b.shard(k)
	sh.mu.RLock()
	v, ok := sh.m[string(k)]
	sh.mu.RUnlock()
	if !ok {
		return dst[:0], false
	}
	return append(dst[:0], v...), true
}

func (b *mapBackend) Delete(k []byte) bool {
	sh := b.shard(k)
	sh.mu.Lock()
	_, ok := sh.m[string(k)]
	delete(sh.m, string(k))
	sh.mu.Unlock()
	return ok
}

func (b *mapBackend) Len() int {
	n := 0
	for i := range b.shards {
		sh := &b.shards[i]
		sh.mu.RLock()
		n += len(sh.m)
		sh.mu.RUnlock()
	}
	return n
}

func (b *mapBackend) OffHeapBytes() int64 { return 0 }
func (b *mapBackend) Details() any        { return nil }
func (b *mapBackend) Close() error        { return nil }
