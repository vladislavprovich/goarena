package store

import (
	"bytes"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/arena"
	"github.com/vladislavprovich/goarena/gcpace"
)

func TestGetOrSet(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4})
	v, loaded, err := s.GetOrSet([]byte("k"), []byte("first"))
	if err != nil || loaded || string(v) != "first" {
		t.Fatalf("miss: %q %v %v", v, loaded, err)
	}
	v, loaded, err = s.GetOrSet([]byte("k"), []byte("second"))
	if err != nil || !loaded || string(v) != "first" {
		t.Fatalf("hit: %q %v %v", v, loaded, err)
	}
	if got, _ := s.GetString("k"); got != "first" {
		t.Fatalf("GetOrSet overwrote a present key: %q", got)
	}
	// the returned value of a hit is a copy
	v[0] = 'X'
	if got, _ := s.GetString("k"); got != "first" {
		t.Fatal("hit result aliases the store")
	}
	// empty values
	v, loaded, err = s.GetOrSet([]byte("empty"), nil)
	if err != nil || loaded || len(v) != 0 {
		t.Fatalf("empty miss: %q %v %v", v, loaded, err)
	}
	if v, loaded, _ = s.GetOrSet([]byte("empty"), []byte("x")); !loaded || v == nil || len(v) != 0 {
		t.Fatalf("empty hit: %q %v", v, loaded)
	}
	if _, _, err := s.GetOrSet(make([]byte, MaxKeySize+1), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big key: %v", err)
	}
	if st := s.Stats(); st.Entries != 2 || st.Sets != 2 {
		t.Fatalf("stats %#v", st)
	}
}

func TestGetOrSetExpiredAndTTL(t *testing.T) {
	now := fakeClock(t, 1000)
	s := newTestStore(t, Config{Shards: 1, DefaultTTL: time.Second})
	_ = s.SetWithTTL([]byte("k"), []byte("old"), time.Millisecond)
	*now += int64(time.Millisecond)
	v, loaded, err := s.GetOrSet([]byte("k"), []byte("new"))
	if err != nil || loaded || string(v) != "new" {
		t.Fatalf("expired entry must be replaced: %q %v %v", v, loaded, err)
	}
	*now += int64(time.Second) - 1
	if !s.HasString("k") {
		t.Fatal("DefaultTTL applies to GetOrSet: expired too early")
	}
	*now++
	if s.HasString("k") {
		t.Fatal("DefaultTTL applies to GetOrSet: not expired")
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d", s.Len())
	}
	// an expired entry replaced by a value of another size class
	_ = s.SetWithTTL([]byte("small"), []byte("x"), time.Millisecond)
	*now += int64(time.Millisecond)
	big := bytes.Repeat([]byte("B"), 1000)
	if v, loaded, err := s.GetOrSet([]byte("small"), big); err != nil || loaded || !bytes.Equal(v, big) {
		t.Fatalf("relocating replace: %d bytes %v %v", len(v), loaded, err)
	}
	if v, _ := s.Get([]byte("small")); !bytes.Equal(v, big) {
		t.Fatal("relocated value lost")
	}
	if st := s.Stats(); st.KeyValueBytes != int64(len("k")+len("new")+len("small")+len(big)) {
		t.Fatalf("accounting after replace: %#v", st)
	}
}

func TestGetOrSetGrowsIndex(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1})
	for i := 0; i < 5000; i++ {
		if _, loaded, err := s.GetOrSet([]byte(fmt.Sprint("key-", i)), []byte(fmt.Sprint(i))); err != nil || loaded {
			t.Fatalf("insert %d: %v %v", i, loaded, err)
		}
	}
	for i := 0; i < 5000; i++ {
		if v, ok := s.GetString(fmt.Sprint("key-", i)); !ok || v != fmt.Sprint(i) {
			t.Fatalf("key %d: %q %v", i, v, ok)
		}
	}
	if st := s.Stats(); st.Entries != 5000 || st.LoadFactor > MaxLoadFactor {
		t.Fatalf("stats %#v", st)
	}
}

func TestGetOrSetConcurrentSingleWinner(t *testing.T) {
	s := newTestStore(t, Config{Shards: 2})
	const workers = 16
	for round := 0; round < 50; round++ {
		key := []byte(fmt.Sprint("race-", round))
		var wg sync.WaitGroup
		var stored atomic.Int32
		results := make([]string, workers)
		for g := 0; g < workers; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				v, loaded, err := s.GetOrSet(key, []byte(fmt.Sprint("value-", g)))
				if err != nil {
					t.Error(err)
				}
				if !loaded {
					stored.Add(1)
				}
				results[g] = string(v)
			}(g)
		}
		wg.Wait()
		if stored.Load() != 1 {
			t.Fatalf("round %d: %d goroutines stored a value", round, stored.Load())
		}
		for _, r := range results[1:] {
			if r != results[0] {
				t.Fatalf("round %d: different results %q", round, results)
			}
		}
	}
}

func TestGetOrLoad(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4})
	calls := 0
	load := func() ([]byte, error) { calls++; return []byte("loaded"), nil }
	for i := 0; i < 3; i++ {
		v, err := s.GetOrLoad([]byte("k"), load)
		if err != nil || string(v) != "loaded" {
			t.Fatalf("GetOrLoad: %q %v", v, err)
		}
	}
	if calls != 1 {
		t.Fatalf("load called %d times", calls)
	}

	// a failing load stores nothing and returns its error unchanged
	boom := errors.New("boom")
	if v, err := s.GetOrLoad([]byte("fail"), func() ([]byte, error) { return nil, boom }); err != boom || v != nil {
		t.Fatalf("failing load: %q %v", v, err)
	}
	if s.HasString("fail") {
		t.Fatal("failed load stored a value")
	}

	// load may use the store (no lock is held)
	v, err := s.GetOrLoad([]byte("nested"), func() ([]byte, error) {
		got, _ := s.GetString("k")
		return []byte("from " + got), nil
	})
	if err != nil || string(v) != "from loaded" {
		t.Fatalf("nested: %q %v", v, err)
	}

	// a value that cannot be stored is still returned, with the error
	big := make([]byte, MaxKeySize+1)
	v, err = s.GetOrLoad(big, func() ([]byte, error) { return []byte("v"), nil })
	if !errors.Is(err, ErrTooLarge) || string(v) != "v" {
		t.Fatalf("unstorable: %q %v", v, err)
	}
}

func TestGetOrLoadNoMemoryAndClosed(t *testing.T) {
	s, err := New(Config{Shards: 1, ChunkSize: 64 << 10, MaxMemory: 128 << 10})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetOrLoad([]byte("huge"), func() ([]byte, error) { return make([]byte, 1<<20), nil })
	if !errors.Is(err, ErrNoMemory) || len(v) != 1<<20 {
		t.Fatalf("no memory: %d bytes, %v", len(v), err)
	}
	_ = s.Close()
	called := false
	if _, err := s.GetOrLoad([]byte("k"), func() ([]byte, error) { called = true; return nil, nil }); !errors.Is(err, ErrClosed) || called {
		t.Fatalf("after Close: %v, load called: %v", err, called)
	}
	if _, _, err := s.GetOrSet([]byte("k"), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetOrSet after Close: %v", err)
	}
}

func TestGetOrLoadConcurrentSameResult(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1})
	var calls atomic.Int32
	var wg sync.WaitGroup
	results := make([]string, 16)
	start := make(chan struct{})
	for g := range results {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			v, err := s.GetOrLoad([]byte("k"), func() ([]byte, error) {
				calls.Add(1)
				time.Sleep(time.Millisecond) // widen the window for duplicate loads
				return []byte(fmt.Sprint("v", g)), nil
			})
			if err != nil {
				t.Error(err)
			}
			results[g] = string(v)
		}(g)
	}
	close(start)
	wg.Wait()
	for _, r := range results {
		if r != results[0] {
			t.Fatalf("callers observed different values: %q", results)
		}
	}
	if got, _ := s.GetString("k"); got != results[0] {
		t.Fatalf("stored %q, returned %q", got, results[0])
	}
	t.Logf("load called %d times by %d concurrent callers", calls.Load(), len(results))
}

func TestStringHelpers(t *testing.T) {
	now := fakeClock(t, 1000)
	s := newTestStore(t, Config{Shards: 4})
	if err := s.SetStringWithTTL("session", "token", time.Minute); err != nil {
		t.Fatal(err)
	}
	if !s.HasString("session") || s.HasString("other") {
		t.Fatal("HasString")
	}
	if v, ok := s.GetString("session"); !ok || v != "token" {
		t.Fatalf("GetString = %q %v", v, ok)
	}
	*now += int64(time.Minute)
	if s.HasString("session") {
		t.Fatal("SetStringWithTTL: entry did not expire")
	}
	if err := s.SetStringWithTTL("forever", "v", 0); err != nil || !s.HasString("forever") {
		t.Fatal("ttl 0 means no expiry")
	}
}

func TestMustNew(t *testing.T) {
	s := MustNew(Config{Shards: 2})
	_ = s.SetString("k", "v")
	if v, _ := s.GetString("k"); v != "v" {
		t.Fatal("MustNew store does not work")
	}
	_ = s.Close()
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "shard count") {
			t.Fatalf("MustNew with an invalid config: %v", r)
		}
	}()
	MustNew(Config{Shards: -1})
}

func TestErrorMessages(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1, ChunkSize: 64 << 10, MaxMemory: 128 << 10})
	err := s.Set(make([]byte, MaxKeySize+1), nil)
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "key of 65536 bytes exceeds MaxKeySize (65535 bytes)") {
		t.Fatalf("key too large: %v", err)
	}
	err = checkSize([]byte("k"), make([]byte, MaxEntrySize))
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "1-byte key and 1073741824-byte value exceed MaxEntrySize") {
		t.Fatalf("entry too large: %v", err)
	}
	err = s.Set([]byte("big"), make([]byte, 200<<10))
	if !errors.Is(err, ErrNoMemory) ||
		!strings.Contains(err.Error(), "cannot store a 3-byte key with a 204800-byte value") ||
		!strings.Contains(err.Error(), "of 131072 bytes (Config.MaxMemory) reserved") {
		t.Fatalf("no memory: %v", err)
	}
	if !strings.HasPrefix(err.Error(), ErrNoMemory.Error()+": ") {
		t.Fatalf("message must start with the sentinel's text: %v", err)
	}
	if _, err := New(Config{Shards: 64, ChunkSize: 4 << 10, MaxMemory: 8 << 10}); !errors.Is(err, ErrNoMemory) ||
		!strings.Contains(err.Error(), "initial index of 64 shards x 64 slots") {
		t.Fatalf("New with too little memory: %v", err)
	}
}

// TestGCPacingWithoutCgo checks that GCPacing is ignored when the arena is
// part of the Go heap (the GC already sees those bytes).
func TestGCPacingWithoutCgo(t *testing.T) {
	if arena.OffHeap {
		t.Skip("built with cgo (see TestGCPacing)")
	}
	oldP := debug.SetGCPercent(100)
	defer debug.SetGCPercent(oldP)
	s := newTestStore(t, Config{Shards: 4, GCPacing: true})
	_ = s.SetString("k", "v")
	if gcpace.Default() != nil {
		t.Fatal("GCPacing must not start the pacer without cgo")
	}
	if v := debug.SetGCPercent(100); v != 100 {
		t.Fatalf("GOGC changed: %d", v)
	}
}
