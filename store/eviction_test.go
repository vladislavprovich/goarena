package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func evictKey(i uint64) []byte { return []byte(fmt.Sprintf("key:%012d", i)) }

func TestEvictionConfig(t *testing.T) {
	if _, err := New(Config{Eviction: Eviction(7)}); err == nil {
		t.Fatal("invalid Eviction accepted")
	}
	if EvictNone.String() != "none" || EvictClock.String() != "clock" || Eviction(9).String() != "Eviction(9)" {
		t.Fatal("Eviction.String")
	}
	s := newTestStore(t, Config{Eviction: EvictClock})
	if st := s.Stats(); st.Eviction != EvictClock || st.Evictions != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// TestEvictionKeepsMemoryBounded writes far more data than MaxMemory
// allows: without eviction writes fail, with EvictClock every write
// succeeds, the reserved memory stays within the limit and the most recent
// entries are present.
func TestEvictionKeepsMemoryBounded(t *testing.T) {
	const limit = 4 << 20
	val := make([]byte, 100)
	for _, ev := range []Eviction{EvictNone, EvictClock} {
		s := newTestStore(t, Config{Shards: 4, ChunkSize: 256 << 10, MaxMemory: limit, Eviction: ev})
		var failed int
		const n = 200_000
		for i := uint64(0); i < n; i++ {
			binary.LittleEndian.PutUint64(val, i)
			if err := s.Set(evictKey(i), val); err != nil {
				if !errors.Is(err, ErrNoMemory) {
					t.Fatal(err)
				}
				failed++
			}
		}
		st := s.Stats()
		if st.ArenaReservedBytes > limit {
			t.Fatalf("%v: reserved %d > limit %d", ev, st.ArenaReservedBytes, limit)
		}
		switch ev {
		case EvictNone:
			if failed == 0 || st.Evictions != 0 {
				t.Fatalf("none: failed=%d evictions=%d", failed, st.Evictions)
			}
		case EvictClock:
			if failed != 0 {
				t.Fatalf("clock: %d writes failed", failed)
			}
			if st.Evictions == 0 || st.Entries+int64(st.Evictions) != n {
				t.Fatalf("clock: entries %d + evictions %d != %d", st.Entries, st.Evictions, n)
			}
			for i := uint64(n - 100); i < n; i++ {
				v, ok := s.Get(evictKey(i))
				if !ok || binary.LittleEndian.Uint64(v) != i {
					t.Fatalf("recent key %d missing or wrong", i)
				}
			}
			t.Logf("clock: %v", st)
		}
	}
}

// TestEvictionSecondChance checks the CLOCK property: entries that are
// read between the sweeps of the hand survive a stream of new keys that
// are never read again.
func TestEvictionSecondChance(t *testing.T) {
	s := newTestStore(t, Config{Shards: 2, ChunkSize: 256 << 10, MaxMemory: 2 << 20, Eviction: EvictClock})
	val := make([]byte, 64)
	const hot = 200
	for i := uint64(0); i < hot; i++ {
		if err := s.Set(evictKey(i), val); err != nil {
			t.Fatal(err)
		}
	}
	for i := uint64(hot); i < 200_000; i++ {
		if err := s.Set(evictKey(i), val); err != nil {
			t.Fatal(err)
		}
		if i%50 == 0 { // read the hot set regularly
			for h := uint64(0); h < hot; h++ {
				s.Has(evictKey(h))
			}
		}
	}
	survived := 0
	for h := uint64(0); h < hot; h++ {
		if s.Has(evictKey(h)) {
			survived++
		}
	}
	cold := 0
	for i := uint64(hot); i < hot+2000; i++ {
		if s.Has(evictKey(i)) {
			cold++
		}
	}
	if survived < hot*95/100 {
		t.Fatalf("only %d of %d hot keys survived", survived, hot)
	}
	if cold > 200 {
		t.Fatalf("%d of the 2000 oldest cold keys survived", cold)
	}
	t.Logf("hot survived %d/%d, old cold survived %d/2000, evictions %d", survived, hot, cold, s.Stats().Evictions)
}

// TestEvictionMixedSizes writes values of random sizes (several size
// classes, and occasionally a large object) into a full store: victims of
// a larger class are reclaimed as carving space, values are never mixed up
// and almost every write succeeds.
func TestEvictionMixedSizes(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4, ChunkSize: 256 << 10, MaxMemory: 6 << 20, Eviction: EvictClock})
	r := rand.New(rand.NewSource(3))
	var buf []byte
	failed := 0
	const n = 60_000
	for i := uint64(0); i < n; i++ {
		size := 16 + r.Intn(1500)
		if r.Intn(2000) == 0 {
			size = 80 << 10 // large object
		}
		buf = checksumValue(buf, i, size, r)
		if err := s.Set(evictKey(i), buf); err != nil {
			if !errors.Is(err, ErrNoMemory) {
				t.Fatal(err)
			}
			failed++
		}
		if i%997 == 0 { // overwrite an older key with a different size
			k := uint64(r.Int63n(int64(i + 1)))
			buf = checksumValue(buf, k, 16+r.Intn(3000), r)
			if err := s.Set(evictKey(k), buf); err != nil && !errors.Is(err, ErrNoMemory) {
				t.Fatal(err)
			}
		}
	}
	if failed > n/100 {
		t.Fatalf("%d of %d writes failed", failed, n)
	}
	present := 0
	s.Range(func(key, val []byte) bool {
		var k uint64
		if _, err := fmt.Sscanf(string(key), "key:%d", &k); err != nil || !checksumOK(k, val) {
			t.Errorf("bad entry %q", key)
			return false
		}
		present++
		return true
	})
	st := s.Stats()
	if st.ArenaReservedBytes > 6<<20 || int64(present) != st.Entries {
		t.Fatalf("reserved %d, present %d, stats %v", st.ArenaReservedBytes, present, st)
	}
	t.Logf("failed %d/%d, %v", failed, n, st)
}

// TestEvictionIndexCompaction fills a store whose index cannot grow any
// more: inserts then evict entries and compact the index in place instead
// of rehashing into a new table.
func TestEvictionIndexCompaction(t *testing.T) {
	// One shard, tiny values: the index (16 B per slot) is a large part of
	// the memory, so a rehash runs out of memory before the entries do.
	s := newTestStore(t, Config{Shards: 1, ChunkSize: 1 << 20, MaxMemory: 1 << 20, Eviction: EvictClock})
	val := []byte{1}
	for i := uint64(0); i < 300_000; i++ {
		if err := s.Set(evictKey(i), val); err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
		if i%1000 == 0 {
			s.Delete(evictKey(i / 2)) // tombstones
		}
	}
	st := s.Stats()
	if st.LoadFactor > MaxLoadFactor || st.Evictions == 0 {
		t.Fatalf("stats %v", st)
	}
	for i := uint64(300_000 - 50); i < 300_000; i++ {
		if !s.Has(evictKey(i)) {
			t.Fatalf("recent key %d missing", i)
		}
	}
}

// TestEvictionConcurrent runs lock-free readers that verify every value
// against writers that keep the store at its memory limit, so reads race
// with evictions, reclaims and in-place index compactions.
func TestEvictionConcurrent(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4, ChunkSize: 256 << 10, MaxMemory: 3 << 20, Eviction: EvictClock})
	dur := 1200 * time.Millisecond
	if testing.Short() || raceEnabled {
		dur = 400 * time.Millisecond
	}
	var stop atomic.Bool
	var torn, reads, hits atomic.Int64
	var wg sync.WaitGroup
	const keys = 40_000
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(w)))
			var val []byte
			for !stop.Load() {
				k := uint64(r.Intn(keys))
				val = checksumValue(val, k, 16+r.Intn(700), r)
				if err := s.Set(evictKey(k), val); err != nil && !errors.Is(err, ErrNoMemory) {
					t.Error(err)
					return
				}
				if r.Intn(20) == 0 {
					s.Delete(evictKey(uint64(r.Intn(keys))))
				}
			}
		}(w)
	}
	readers := runtime.GOMAXPROCS(0)
	if readers > 8 {
		readers = 8
	}
	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(100 + g)))
			dst := make([]byte, 0, 1024)
			for !stop.Load() {
				k := uint64(r.Intn(keys))
				var ok bool
				dst, ok = s.GetInto(dst, evictKey(k))
				if ok {
					hits.Add(1)
					if !checksumOK(k, dst) {
						torn.Add(1)
					}
				}
				reads.Add(1)
			}
		}(g)
	}
	time.Sleep(dur)
	stop.Store(true)
	wg.Wait()
	st := s.Stats()
	if torn.Load() != 0 {
		t.Fatalf("%d torn values out of %d reads", torn.Load(), reads.Load())
	}
	if st.ArenaReservedBytes > 3<<20 || st.Evictions == 0 {
		t.Fatalf("stats %v", st)
	}
	t.Logf("reads %d, hits %d, %v", reads.Load(), hits.Load(), st)
}

// FuzzEviction is a differential test of a store that is permanently at
// its memory limit: every value returned must be the last one written for
// that key, a key that was found missing (evicted) must stay missing until
// it is written again, a value written successfully must be readable
// immediately, deleted keys must be gone and the reserved memory must stay
// within MaxMemory.
//
// Operation encoding: op%4 == 0,1 Set(key, size), 2 Get(key), 3 Delete(key);
// key = next byte, size = next byte * 9 (0..2295 bytes, several classes).
func FuzzEviction(f *testing.F) {
	f.Add([]byte{0, 1, 10, 2, 1, 3, 1, 2, 1})
	f.Add(bytes.Repeat([]byte{0, 7, 200, 1, 9, 30, 2, 7}, 60))
	f.Add(bytes.Repeat([]byte{0, 3, 255, 0, 4, 1, 2, 3, 3, 4}, 80))
	f.Fuzz(func(t *testing.T, ops []byte) {
		const limit = 192 << 10
		s, err := New(Config{Shards: 2, ChunkSize: 64 << 10, MaxMemory: limit, Eviction: EvictClock})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		model := map[string][]byte{}
		var buf []byte
		for i := 0; i < len(ops); {
			op := ops[i] % 4
			i++
			if i >= len(ops) {
				break
			}
			// Many distinct keys so that the store is always full.
			key := []byte(fmt.Sprintf("k%03d-%d", ops[i], i%7))
			i++
			switch op {
			case 0, 1:
				n := 0
				if i < len(ops) {
					n = int(ops[i]) * 9
					i++
				}
				v := bytes.Repeat([]byte{byte(i)}, n)
				err := s.Set(key, v)
				if err != nil {
					if !errors.Is(err, ErrNoMemory) {
						t.Fatal(err)
					}
					// a failed overwrite keeps the old value
					continue
				}
				model[string(key)] = v
				got, ok := s.Get(key)
				if !ok || !bytes.Equal(got, v) {
					t.Fatalf("value written for %q not readable", key)
				}
			case 2:
				var ok bool
				buf, ok = s.GetInto(buf, key)
				want, inModel := model[string(key)]
				if ok && (!inModel || !bytes.Equal(buf, want)) {
					t.Fatalf("%q: got a value that was never written last (in model: %v)", key, inModel)
				}
				if !ok && inModel {
					delete(model, string(key)) // evicted: must stay absent
				}
			case 3:
				s.Delete(key)
				delete(model, string(key))
			}
			if r := s.Stats().ArenaReservedBytes; r > limit {
				t.Fatalf("reserved %d > %d", r, limit)
			}
		}
		n := 0
		s.Range(func(k, v []byte) bool {
			if want, ok := model[string(k)]; ok && !bytes.Equal(v, want) {
				t.Fatalf("Range: %q has a stale value", k)
			}
			n++
			return true
		})
		if n != s.Len() {
			t.Fatalf("Range visited %d, Len %d", n, s.Len())
		}
	})
}

// TestCompactInPlace removes the tombstones of a heavily used table in
// place and checks that every remaining key is still found and that the
// reference bits moved with their entries.
func TestCompactInPlace(t *testing.T) {
	for _, ev := range []Eviction{EvictNone, EvictClock} {
		s := newTestStore(t, Config{Shards: 1, Eviction: ev})
		r := rand.New(rand.NewSource(9))
		alive := map[uint64]bool{}
		for i := uint64(0); i < 3000; i++ {
			_ = s.Set(evictKey(i), []byte("v"))
			alive[i] = true
			if r.Intn(2) == 0 {
				k := uint64(r.Int63n(int64(i + 1)))
				s.Delete(evictKey(k))
				delete(alive, k)
			}
		}
		for k := range alive {
			if k%3 == 0 {
				s.Has(evictKey(k)) // set some reference bits
			}
		}
		sh := &s.shards[0]
		sh.mu.Lock()
		sh.beginWrite()
		tab := sh.tab.Load()
		refs := 0
		if tab.refs != nil {
			for i := uint64(0); i <= tab.mask; i++ {
				if tab.referenced(i) {
					refs++
				}
			}
		}
		sh.compactInPlace(tab)
		sh.endWrite()
		after := 0
		if tab.refs != nil {
			for i := uint64(0); i <= tab.mask; i++ {
				if tab.referenced(i) {
					after++
				}
			}
		}
		sh.mu.Unlock()
		if st := s.Stats(); st.Tombstones != 0 || st.Entries != int64(len(alive)) {
			t.Fatalf("%v: stats after compaction %v", ev, st)
		}
		if refs != after {
			t.Fatalf("%v: %d reference bits before, %d after", ev, refs, after)
		}
		for k := range alive {
			if !s.Has(evictKey(k)) {
				t.Fatalf("%v: key %d lost by compaction", ev, k)
			}
		}
	}
}
