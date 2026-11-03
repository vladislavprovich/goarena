package store

import (
	"bytes"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/arena"
	"github.com/vladislavprovich/goarena/gcpace"
	"github.com/vladislavprovich/goarena/internal/cmem"
)

func newTestStore(t testing.TB, cfg Config) *Store {
	t.Helper()
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// fakeClock replaces nowNano for the duration of a test.
func fakeClock(t *testing.T, start int64) *int64 {
	now := start
	old := nowNano
	nowNano = func() int64 { return now }
	t.Cleanup(func() { nowNano = old })
	return &now
}

func TestNewConfig(t *testing.T) {
	for _, cfg := range []Config{
		{Shards: -1}, {Shards: MaxShards + 1}, {MaxMemory: -1},
		{DefaultTTL: -1}, {InitialCapacity: -5}, {ChunkSize: 10},
	} {
		if s, err := New(cfg); err == nil {
			s.Close()
			t.Errorf("New(%+v) succeeded", cfg)
		}
	}
	s := newTestStore(t, Config{Shards: 3})
	if len(s.shards) != 4 {
		t.Fatalf("shards not rounded up: %d", len(s.shards))
	}
	s2 := newTestStore(t, Config{InitialCapacity: 100000, Shards: 16})
	if c := s2.shards[0].tab.Load().capacity(); c < 100000/16*4/3 {
		t.Fatalf("index not pre-sized: %d", c)
	}
	if st := newTestStore(t, Config{}).Stats(); st.Shards != DefaultShards {
		t.Fatalf("default shards = %d", st.Shards)
	}
}

func TestBasicOperations(t *testing.T) {
	s := newTestStore(t, Config{Shards: 4})
	if _, ok := s.Get([]byte("missing")); ok {
		t.Fatal("found missing key")
	}
	if err := s.Set([]byte("alpha"), []byte("one")); err != nil {
		t.Fatal(err)
	}
	v, ok := s.Get([]byte("alpha"))
	if !ok || string(v) != "one" {
		t.Fatalf("Get = %q %v", v, ok)
	}
	// the returned slice is a copy
	v[0] = 'X'
	if v2, _ := s.Get([]byte("alpha")); string(v2) != "one" {
		t.Fatal("Get must return a copy")
	}
	// overwrite: same class (in place) and different class (reallocated)
	if err := s.Set([]byte("alpha"), []byte("two")); err != nil {
		t.Fatal(err)
	}
	long := bytes.Repeat([]byte("z"), 5000)
	if err := s.Set([]byte("alpha"), long); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get([]byte("alpha")); !bytes.Equal(v, long) {
		t.Fatal("long overwrite lost")
	}
	if err := s.Set([]byte("alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get([]byte("alpha")); !ok || len(v) != 0 {
		t.Fatal("empty value")
	}
	if !s.Has([]byte("alpha")) || s.Has([]byte("beta")) {
		t.Fatal("Has")
	}
	// empty key is a valid key
	if err := s.Set(nil, []byte("empty-key")); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get([]byte{}); !ok || string(v) != "empty-key" {
		t.Fatal("empty key")
	}
	if s.Len() != 2 {
		t.Fatalf("Len = %d", s.Len())
	}
	if !s.Delete([]byte("alpha")) || s.Delete([]byte("alpha")) {
		t.Fatal("Delete")
	}
	if s.Len() != 1 {
		t.Fatalf("Len after delete = %d", s.Len())
	}
	st := s.Stats()
	if st.Sets != 5 || st.Deletes != 1 || st.Entries != 1 || st.EntryBytes != int64(entrySize(0, 9, false)) {
		t.Fatalf("stats %+v", st)
	}
}

func TestGetIntoViewStrings(t *testing.T) {
	s := newTestStore(t, Config{Shards: 2})
	if err := s.SetString("k", "value-1"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0, 64)
	out, ok := s.GetInto(buf, []byte("k"))
	if !ok || string(out) != "value-1" || &out[0] != &buf[:1][0] {
		t.Fatal("GetInto must reuse dst")
	}
	if out, ok := s.GetInto(buf, []byte("nope")); ok || len(out) != 0 {
		t.Fatal("GetInto on missing key")
	}
	var seen string
	if !s.View([]byte("k"), func(v []byte) { seen = string(v) }) || seen != "value-1" {
		t.Fatal("View")
	}
	if s.View([]byte("nope"), func([]byte) { t.Fatal("called") }) {
		t.Fatal("View on missing key")
	}
	if v, ok := s.GetString("k"); !ok || v != "value-1" {
		t.Fatal("GetString")
	}
	if _, ok := s.GetString("nope"); ok {
		t.Fatal("GetString missing")
	}
	if !s.DeleteString("k") || s.DeleteString("k") {
		t.Fatal("DeleteString")
	}
	// View must release the lock even if fn panics
	_ = s.SetString("p", "x")
	func() {
		defer func() { _ = recover() }()
		s.View([]byte("p"), func([]byte) { panic("boom") })
	}()
	if err := s.SetString("p", "y"); err != nil {
		t.Fatal(err)
	}
}

func TestTTL(t *testing.T) {
	now := fakeClock(t, 1_000_000_000)
	s := newTestStore(t, Config{Shards: 2, DefaultTTL: time.Second})
	_ = s.Set([]byte("a"), []byte("1"))                       // default TTL 1s
	_ = s.SetWithTTL([]byte("b"), []byte("2"), 3*time.Second) // explicit
	_ = s.SetWithTTL([]byte("c"), []byte("3"), 0)             // never expires
	*now += int64(1500 * time.Millisecond)
	if _, ok := s.Get([]byte("a")); ok {
		t.Fatal("a must be expired")
	}
	if s.Has([]byte("a")) || s.View([]byte("a"), func([]byte) {}) {
		t.Fatal("expired key visible")
	}
	if _, ok := s.GetInto(nil, []byte("a")); ok {
		t.Fatal("GetInto expired")
	}
	if _, ok := s.GetString("a"); ok {
		t.Fatal("GetString expired")
	}
	if _, ok := s.Get([]byte("b")); !ok {
		t.Fatal("b expired too early")
	}
	n := 0
	s.Range(func(k, v []byte) bool { n++; return true })
	if n != 2 {
		t.Fatalf("Range visited %d entries", n)
	}
	if s.Delete([]byte("a")) {
		t.Fatal("Delete of an expired entry must report false")
	}
	*now += int64(2 * time.Second)
	if got := s.EvictExpired(); got != 1 {
		t.Fatalf("EvictExpired = %d", got)
	}
	if s.Len() != 1 || s.Stats().Expired != 2 {
		t.Fatalf("Len = %d, stats %+v", s.Len(), s.Stats())
	}
	// re-setting an expired key revives it
	_ = s.SetWithTTL([]byte("b"), []byte("new"), time.Minute)
	if v, ok := s.Get([]byte("b")); !ok || string(v) != "new" {
		t.Fatal("revive")
	}
}

func TestLimits(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1, ChunkSize: 64 << 10, MaxMemory: 128 << 10})
	if err := s.Set(make([]byte, MaxKeySize+1), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big key: %v", err)
	}
	if err := checkSize(nil, make([]byte, 0)); err != nil {
		t.Fatal(err)
	}
	var err error
	val := make([]byte, 1000)
	i := 0
	for ; i < 1000 && err == nil; i++ {
		err = s.Set([]byte(fmt.Sprint(i)), val)
	}
	if !errors.Is(err, ErrNoMemory) {
		t.Fatalf("expected ErrNoMemory, got %v after %d sets", err, i)
	}
	// large object path is limited too
	if err := s.Set([]byte("big"), make([]byte, 200<<10)); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("large: %v", err)
	}
	// overwrite with a different class when memory is exhausted
	if err := s.Set([]byte("0"), make([]byte, 40000)); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("overwrite: %v", err)
	}
	if v, ok := s.Get([]byte("0")); !ok || len(v) != 1000 {
		t.Fatal("failed overwrite must keep the old value")
	}
	if !errors.Is(mapAllocErr(arena.ErrReleased), ErrClosed) || mapAllocErr(arena.ErrInvalidSize) != arena.ErrInvalidSize {
		t.Fatal("mapAllocErr")
	}
}

func TestLargeValues(t *testing.T) {
	s := newTestStore(t, Config{Shards: 2, ChunkSize: 1 << 20})
	big := bytes.Repeat([]byte("L"), 3<<20) // bigger than a chunk and a class
	if err := s.Set([]byte("big"), big); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get([]byte("big")); !bytes.Equal(v, big) {
		t.Fatal("large value corrupted")
	}
	if st := s.Stats(); st.LargeObjects != 1 {
		t.Fatalf("large objects = %d", st.LargeObjects)
	}
	if !s.Delete([]byte("big")) {
		t.Fatal("delete large")
	}
	if st := s.Stats(); st.LargeObjects != 0 {
		t.Fatalf("large object not freed: %+v", st)
	}
}

func TestRangeStops(t *testing.T) {
	s := newTestStore(t, Config{Shards: 8})
	for i := 0; i < 100; i++ {
		_ = s.SetString(fmt.Sprint(i), strings.Repeat("v", i))
	}
	n := 0
	s.Range(func(k, v []byte) bool {
		if len(v) != int(mustAtoi(string(k))) {
			t.Fatalf("Range: key %s value len %d", k, len(v))
		}
		n++
		return n < 10
	})
	if n != 10 {
		t.Fatalf("Range did not stop: %d", n)
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestCloseSemantics(t *testing.T) {
	before := cmem.ReadStats()
	s, err := New(Config{Shards: 4})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetString("a", "b")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("second Close must return nil")
	}
	if !s.Closed() {
		t.Fatal("Closed() = false")
	}
	if err := s.SetString("a", "b"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after Close: %v", err)
	}
	if _, ok := s.Get([]byte("a")); ok {
		t.Fatal("Get after Close")
	}
	if _, ok := s.GetInto(nil, []byte("a")); ok {
		t.Fatal("GetInto after Close")
	}
	if _, ok := s.GetString("a"); ok || s.Has([]byte("a")) || s.Delete([]byte("a")) {
		t.Fatal("reads after Close")
	}
	if s.View([]byte("a"), func([]byte) {}) || s.EvictExpired() != 0 {
		t.Fatal("View/Evict after Close")
	}
	s.Range(func(k, v []byte) bool { t.Fatal("Range after Close"); return true })
	if st := s.Stats(); st.Entries != 0 || st.OffHeapBytes != 0 {
		t.Fatalf("stats after Close: %+v", st)
	}
	after := cmem.ReadStats()
	if after.LiveBlocks != before.LiveBlocks || after.LiveBytes != before.LiveBytes {
		t.Fatalf("leak after Close: %+v -> %+v", before, after)
	}
}

func TestGCPacing(t *testing.T) {
	if !arena.OffHeap {
		t.Skip("built without cgo: GCPacing has no effect (see TestGCPacingWithoutCgo)")
	}
	oldP := debug.SetGCPercent(100)
	defer debug.SetGCPercent(oldP)
	gcpace.SetDefaultConfig(gcpace.Config{Interval: time.Hour})
	defer gcpace.SetDefaultConfig(gcpace.Config{})
	s, err := New(Config{Shards: 4, ChunkSize: 16 << 20, GCPacing: true})
	if err != nil {
		t.Fatal(err)
	}
	val := make([]byte, 1000)
	for i := 0; i < 20000; i++ { // ~20 MiB off-heap
		_ = s.Set([]byte(fmt.Sprint(i)), val)
	}
	p := gcpace.Default()
	if p == nil {
		t.Fatal("GCPacing did not start the pacer")
	}
	p.Update()
	st := p.Stats()
	if st.OffHeapBytes != s.arena.ReservedBytes() || st.GCPercent <= 100 {
		t.Fatalf("pacer state %+v", st)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if gcpace.Default() != nil {
		t.Fatal("closing the last paced store must stop the pacer")
	}
	if v := debug.SetGCPercent(100); v != 100 {
		t.Fatalf("GOGC not restored: %d", v)
	}
}
