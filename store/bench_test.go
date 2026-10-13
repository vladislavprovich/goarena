package store

import (
	"encoding/binary"
	"hash/maphash"
	"sync"
	"testing"
)

const benchKeys = 1 << 20 // 1M entries, 128-byte values

func benchKey(dst []byte, i uint64) []byte {
	dst = append(dst[:0], "bench:"...)
	return binary.BigEndian.AppendUint64(dst, i*0x9E3779B97F4A7C15)
}

// shardedMap is the Go-heap reference: 256 x (RWMutex + map[string][]byte).
type shardedMap struct {
	seed   maphash.Seed
	shards [256]struct {
		mu sync.RWMutex
		m  map[string][]byte
	}
}

func newShardedMap() *shardedMap {
	m := &shardedMap{seed: maphash.MakeSeed()}
	for i := range m.shards {
		m.shards[i].m = map[string][]byte{}
	}
	return m
}

func (m *shardedMap) set(k, v []byte) {
	c := append([]byte(nil), v...)
	sh := &m.shards[maphash.Bytes(m.seed, k)&255]
	sh.mu.Lock()
	sh.m[string(k)] = c
	sh.mu.Unlock()
}

func (m *shardedMap) getInto(dst, k []byte) ([]byte, bool) {
	sh := &m.shards[maphash.Bytes(m.seed, k)&255]
	sh.mu.RLock()
	v, ok := sh.m[string(k)]
	sh.mu.RUnlock()
	return append(dst[:0], v...), ok
}

var (
	benchStoreOnce sync.Once
	benchStore     *Store
	benchMapOnce   sync.Once
	benchMap       *shardedMap
)

func filledStore(b *testing.B) *Store {
	benchStoreOnce.Do(func() {
		s, err := New(Config{})
		if err != nil {
			b.Fatal(err)
		}
		val := make([]byte, 128)
		var k []byte
		for i := uint64(0); i < benchKeys; i++ {
			k = benchKey(k, i)
			_ = s.Set(k, val)
		}
		benchStore = s
	})
	return benchStore
}

func filledMap() *shardedMap {
	benchMapOnce.Do(func() {
		m := newShardedMap()
		val := make([]byte, 128)
		var k []byte
		for i := uint64(0); i < benchKeys; i++ {
			k = benchKey(k, i)
			m.set(k, val)
		}
		benchMap = m
	})
	return benchMap
}

func BenchmarkStoreGet(b *testing.B) {
	s := filledStore(b)
	k := make([]byte, 0, 16)
	buf := make([]byte, 0, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k = benchKey(k, uint64(i)&(benchKeys-1))
		buf, _ = s.GetInto(buf, k)
	}
}

func BenchmarkMapGet(b *testing.B) {
	m := filledMap()
	k := make([]byte, 0, 16)
	buf := make([]byte, 0, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k = benchKey(k, uint64(i)&(benchKeys-1))
		buf, _ = m.getInto(buf, k)
	}
}

func BenchmarkStoreSet(b *testing.B) {
	s := filledStore(b)
	k := make([]byte, 0, 16)
	val := make([]byte, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k = benchKey(k, uint64(i)&(benchKeys-1))
		_ = s.Set(k, val)
	}
}

func BenchmarkMapSet(b *testing.B) {
	m := filledMap()
	k := make([]byte, 0, 16)
	val := make([]byte, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k = benchKey(k, uint64(i)&(benchKeys-1))
		m.set(k, val)
	}
}

func BenchmarkStoreGetParallel(b *testing.B) {
	s := filledStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		k := make([]byte, 0, 16)
		buf := make([]byte, 0, 128)
		i := uint64(0)
		for pb.Next() {
			i += 0x9E37
			k = benchKey(k, i&(benchKeys-1))
			buf, _ = s.GetInto(buf, k)
		}
	})
}

func BenchmarkMapGetParallel(b *testing.B) {
	m := filledMap()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		k := make([]byte, 0, 16)
		buf := make([]byte, 0, 128)
		i := uint64(0)
		for pb.Next() {
			i += 0x9E37
			k = benchKey(k, i&(benchKeys-1))
			buf, _ = m.getInto(buf, k)
		}
	})
}

func BenchmarkStoreSetParallel(b *testing.B) {
	s := filledStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		k := make([]byte, 0, 16)
		val := make([]byte, 128)
		i := uint64(0)
		for pb.Next() {
			i += 0x9E37
			k = benchKey(k, i&(benchKeys-1))
			_ = s.Set(k, val)
		}
	})
}

func BenchmarkMapSetParallel(b *testing.B) {
	m := filledMap()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		k := make([]byte, 0, 16)
		val := make([]byte, 128)
		i := uint64(0)
		for pb.Next() {
			i += 0x9E37
			k = benchKey(k, i&(benchKeys-1))
			m.set(k, val)
		}
	})
}
