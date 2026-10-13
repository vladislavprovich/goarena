package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/internal/cmem"
)

// TestConcurrentStress runs readers, writers and deleters concurrently on a
// small number of shards (to maximise contention and rehashing) and checks
// that every value read is internally consistent: values encode their key,
// so a torn or misdirected read is detected. Run it with -race to also
// check the synchronisation.
func TestConcurrentStress(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4, ChunkSize: 256 << 10})
	const keys = 2000
	workers := 2 * runtime.GOMAXPROCS(0)
	if workers > 16 {
		workers = 16
	}
	dur := 2 * time.Second
	if testing.Short() || raceEnabled {
		dur = 500 * time.Millisecond
	}
	var stop atomic.Bool
	var ops, bad atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(w)))
			key := make([]byte, 8)
			val := make([]byte, 0, 4096)
			buf := make([]byte, 0, 4096)
			var n int64
			for !stop.Load() {
				k := uint64(r.Intn(keys))
				binary.LittleEndian.PutUint64(key, k)
				switch x := r.Intn(100); {
				case x < 60:
					var ok bool
					buf, ok = s.GetInto(buf, key)
					if ok && !validValue(k, buf) {
						bad.Add(1)
					}
				case x < 65:
					s.View(key, func(v []byte) {
						if !validValue(k, v) {
							bad.Add(1)
						}
					})
				case x < 90:
					val = makeValue(val, k, uint64(n), 16+r.Intn(3000))
					if err := s.Set(key, val); err != nil {
						t.Error(err)
						return
					}
				default:
					s.Delete(key)
				}
				n++
			}
			ops.Add(n)
		}(w)
	}
	// a concurrent observer exercising Stats, Len and Range
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_ = s.Stats()
			_ = s.Len()
			s.Range(func(k, v []byte) bool {
				if !validValue(binary.LittleEndian.Uint64(k), v) {
					bad.Add(1)
				}
				return true
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()
	time.Sleep(dur)
	stop.Store(true)
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d inconsistent reads", bad.Load())
	}
	st := s.Stats()
	t.Logf("workers=%d ops=%d entries=%d slots=%d load=%.2f tombs=%d frag=%.3f",
		workers, ops.Load(), st.Entries, st.IndexSlots, st.LoadFactor, st.Tombstones, st.InternalFragmentation)
}

// makeValue builds a self-describing value:
// [key uint64][version uint64][n-16 bytes derived from key].
func makeValue(dst []byte, key, version uint64, n int) []byte {
	dst = dst[:0]
	dst = binary.LittleEndian.AppendUint64(dst, key)
	dst = binary.LittleEndian.AppendUint64(dst, version)
	for i := 16; i < n; i++ {
		dst = append(dst, byte(key)+byte(i))
	}
	return dst
}

func validValue(key uint64, v []byte) bool {
	if len(v) < 16 || binary.LittleEndian.Uint64(v) != key {
		return false
	}
	for i := 16; i < len(v); i++ {
		if v[i] != byte(key)+byte(i) {
			return false
		}
	}
	return true
}

// TestConcurrentClose closes a store while goroutines are using it: every
// operation must either succeed or observe the closed state, and no memory
// may leak.
func TestConcurrentClose(t *testing.T) {
	before := cmem.ReadStats().LiveBlocks
	for round := 0; round < 5; round++ {
		s, err := New(Config{Shards: 8, ChunkSize: 64 << 10})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; ; i++ {
					k := []byte(fmt.Sprintf("%d-%d", w, i%500))
					if err := s.Set(k, k); err != nil {
						if !errors.Is(err, ErrClosed) {
							t.Error(err)
						}
						return
					}
					s.Get(k)
				}
			}(w)
		}
		time.Sleep(20 * time.Millisecond)
		_ = s.Close()
		wg.Wait()
	}
	if after := cmem.ReadStats().LiveBlocks; after > before {
		t.Fatalf("leaked %d blocks", after-before)
	}
}
