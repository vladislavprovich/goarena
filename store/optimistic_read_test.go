package store

import (
	"encoding/binary"
	"hash/crc32"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// checksumValue builds [key u64][len u32][crc32 u32][payload] so that any
// torn, stale or misdirected read is detectable by the reader.
func checksumValue(dst []byte, key uint64, n int, r *rand.Rand) []byte {
	if n < 16 {
		n = 16
	}
	dst = dst[:0]
	dst = binary.LittleEndian.AppendUint64(dst, key)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(n))
	dst = append(dst, 0, 0, 0, 0)
	for len(dst) < n {
		dst = append(dst, byte(r.Intn(256)))
	}
	binary.LittleEndian.PutUint32(dst[12:], crc32.ChecksumIEEE(dst[16:]))
	return dst
}

func checksumOK(key uint64, v []byte) bool {
	return len(v) >= 16 &&
		binary.LittleEndian.Uint64(v) == key &&
		int(binary.LittleEndian.Uint32(v[8:])) == len(v) &&
		binary.LittleEndian.Uint32(v[12:]) == crc32.ChecksumIEEE(v[16:])
}

// TestOptimisticReadsNeverTorn hammers a few shards with writers that
// overwrite values with random sizes (in-place and relocating overwrites,
// across size classes and into the large-object path), insert and delete
// churn keys (forcing rehashes, tombstones and table donation) while
// lock-free readers verify every value they get. Pinned keys are never
// deleted, so a reader must always find them, even during a rehash.
func TestOptimisticReadsNeverTorn(t *testing.T) {
	s := newTestStore(t, Config{Shards: 2, ChunkSize: 256 << 10})
	const pinned = 512
	r0 := rand.New(rand.NewSource(1))
	var buf []byte
	key := make([]byte, 8)
	for k := uint64(0); k < pinned; k++ {
		binary.LittleEndian.PutUint64(key, k)
		buf = checksumValue(buf, k, 16+r0.Intn(300), r0)
		if err := s.Set(key, buf); err != nil {
			t.Fatal(err)
		}
	}
	dur := 1500 * time.Millisecond
	if testing.Short() || raceEnabled {
		dur = 400 * time.Millisecond
	}
	var stop atomic.Bool
	var torn, missing, reads atomic.Int64
	var wg sync.WaitGroup
	writers, readers := 3, 2*runtime.GOMAXPROCS(0)
	if readers > 12 {
		readers = 12
	}
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(100 + w)))
			key := make([]byte, 8)
			var val []byte
			for i := 0; !stop.Load(); i++ {
				var k uint64
				if r.Intn(3) == 0 {
					k = pinned + uint64(w)<<32 + uint64(r.Intn(4000)) // churn keys
				} else {
					k = uint64(r.Intn(pinned))
				}
				binary.LittleEndian.PutUint64(key, k)
				switch x := r.Intn(100); {
				case k >= pinned && x < 40:
					s.Delete(key)
				default:
					n := 16 + r.Intn(600)
					if r.Intn(500) == 0 {
						n = 70 << 10 // large object path
					}
					val = checksumValue(val, k, n, r)
					if err := s.Set(key, val); err != nil {
						t.Error(err)
						return
					}
				}
			}
		}(w)
	}
	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(g)))
			key := make([]byte, 8)
			dst := make([]byte, 0, 1024)
			var n int64
			for !stop.Load() {
				var k uint64
				pin := r.Intn(4) != 0
				if pin {
					k = uint64(r.Intn(pinned))
				} else {
					k = pinned + uint64(r.Intn(3))<<32 + uint64(r.Intn(4000))
				}
				binary.LittleEndian.PutUint64(key, k)
				var ok bool
				dst, ok = s.GetInto(dst, key)
				if ok && !checksumOK(k, dst) {
					torn.Add(1)
				}
				if pin && !ok {
					missing.Add(1)
				}
				if pin && !s.Has(key) {
					missing.Add(1)
				}
				n++
			}
			reads.Add(n)
		}(g)
	}
	time.Sleep(dur)
	stop.Store(true)
	wg.Wait()
	if torn.Load() != 0 || missing.Load() != 0 {
		t.Fatalf("%d torn values, %d missing pinned keys out of %d reads", torn.Load(), missing.Load(), reads.Load())
	}
	st := s.Stats()
	t.Logf("reads=%d entries=%d slots=%d donated=%d KiB", reads.Load(), st.Entries,
		st.IndexSlots, st.DonatedBytes>>10)
}

// TestHotKeyReaders runs many readers on one key while a writer keeps
// overwriting it, the pattern that serialises RWMutex readers on one cache
// line. All reads must return a valid version.
func TestHotKeyReaders(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4})
	key := []byte("hot")
	r0 := rand.New(rand.NewSource(9))
	_ = s.Set(key, checksumValue(nil, 7, 64, r0))
	var stop atomic.Bool
	var bad atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := rand.New(rand.NewSource(3))
		var v []byte
		for !stop.Load() {
			v = checksumValue(v, 7, 16+r.Intn(200), r)
			_ = s.Set(key, v)
		}
	}()
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var dst []byte
			for !stop.Load() {
				var ok bool
				if dst, ok = s.GetInto(dst, key); !ok || !checksumOK(7, dst) {
					bad.Add(1)
				}
				if str, ok := s.GetString("hot"); !ok || !checksumOK(7, []byte(str)) {
					bad.Add(1)
				}
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d bad reads", bad.Load())
	}
}

// TestReadOptimisticRejectsGarbage feeds the optimistic reader slots that
// point outside the arena and checks that it reports a retry instead of
// crashing; it also covers the retry-on-odd-sequence branch.
func TestReadOptimisticRejectsGarbage(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1, ChunkSize: 64 << 10})
	_ = s.SetString("k", "v")
	sh := &s.shards[0]
	h := s.hash([]byte("k"))
	tb := sh.tab.Load()
	_, pos, _, found := sh.find(h, []byte("k"))
	if !found {
		t.Fatal("key not found")
	}
	if _, st := sh.readOptimistic(h, []byte("k"), nil, true); st != optHit {
		t.Fatalf("expected hit, got %d", st)
	}
	saved := tb.at(pos).ref
	for _, bogus := range []uint64{0, 1<<32 | 0xffffffff, 77 << 32} {
		tb.at(pos).ref = bogus
		if _, st := sh.readOptimistic(h, []byte("k"), nil, true); st != optRetry {
			t.Fatalf("ref %x: expected retry, got %d", bogus, st)
		}
	}
	tb.at(pos).ref = saved
	sh.seq.Add(1) // pretend a writer is active
	if _, st := sh.readOptimistic(h, []byte("k"), nil, true); st != optRetry {
		t.Fatal("odd sequence must force a retry")
	}
	// with the sequence odd, lookup falls back to the read lock
	if v, ok := s.GetString("k"); !ok || v != "v" {
		t.Fatal("fallback path failed")
	}
	sh.seq.Add(1)
	// a full table of non-matching hashes terminates with a retry
	full := &indexTable{slots: tb.slots, ref: tb.ref, mask: 0}
	saveHash := tb.at(0).hash
	tb.at(0).hash = 12345
	sh.tab.Store(full)
	if _, st := sh.readOptimistic(h, []byte("k"), nil, true); st != optRetry {
		t.Fatal("exhausted probe must retry")
	}
	tb.at(0).hash = saveHash
	sh.tab.Store(tb)
}

func TestShardLayout(t *testing.T) {
	if sz := unsafe.Sizeof(shard{}); sz%64 != 0 {
		t.Fatalf("shard size %d is not a multiple of 64", sz)
	}
	if off := unsafe.Offsetof(shard{}.mu); off != 64 {
		t.Fatalf("writer state starts at %d, want 64", off)
	}
	if unsafe.Sizeof(readerStripe{}) != 64 {
		t.Fatal("reader stripe must fill one cache line")
	}
}
