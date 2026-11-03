package store

import (
	"fmt"
	"testing"

	"github.com/vladislavprovich/goarena/arena"
)

func TestRehashAndTombstones(t *testing.T) {
	s := newTestStore(t, Config{Shards: 1})
	const n = 10000
	for i := 0; i < n; i++ {
		if err := s.Set([]byte(fmt.Sprintf("key-%d", i)), []byte(fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	st := s.Stats()
	if st.Entries != n || st.LoadFactor > MaxLoadFactor || st.IndexSlots < n {
		t.Fatalf("after inserts: %+v", st)
	}
	for i := 0; i < n; i += 2 {
		if !s.Delete([]byte(fmt.Sprintf("key-%d", i))) {
			t.Fatalf("delete %d", i)
		}
	}
	// churn: insert and delete fresh keys so tombstones accumulate and
	// force same-size rehashes
	for r := 0; r < 20; r++ {
		for i := 0; i < 2000; i++ {
			k := []byte(fmt.Sprintf("tmp-%d-%d", r, i))
			_ = s.Set(k, k)
			s.Delete(k)
		}
	}
	st = s.Stats()
	if st.Entries != n/2 || st.LoadFactor > MaxLoadFactor {
		t.Fatalf("after churn: %+v", st)
	}
	for i := 0; i < n; i++ {
		v, ok := s.Get([]byte(fmt.Sprintf("key-%d", i)))
		if (i%2 == 0) == ok {
			t.Fatalf("key %d presence %v", i, ok)
		}
		if ok && string(v) != fmt.Sprint(i) {
			t.Fatalf("key %d = %q", i, v)
		}
	}
	if st.AvgProbeDistance < 0 || st.AvgProbeDistance > 5 {
		t.Fatalf("suspicious probe distance %v", st.AvgProbeDistance)
	}
}

func TestIndexRemoveBackwardSweep(t *testing.T) {
	a := arena.MustNew(arena.Config{ChunkSize: 4096})
	defer a.Release()
	tb, err := newTable(a, 8)
	if err != nil {
		t.Fatal(err)
	}
	var ix shard
	// three colliding hashes with home slot 2 -> slots 2,3,4
	for _, h := range []uint64{2, 10, 18} {
		ix.insertAt(tb, firstFree(tb, h), h, 99)
	}
	ix.remove(tb, 2) // next slot used -> tombstone
	ix.remove(tb, 3) // next slot used -> tombstone
	if ix.tombs != 2 {
		t.Fatalf("tombs = %d", ix.tombs)
	}
	ix.remove(tb, 4) // next slot empty -> 4,3,2 all become empty
	if ix.tombs != 0 || ix.live != 0 || tb.at(2).hash != hashEmpty {
		t.Fatalf("sweep failed: tombs=%d live=%d", ix.tombs, ix.live)
	}
	if _, err := newTable(arena.MustNew(arena.Config{ChunkSize: 4096, MaxBytes: 4096}), 1024); err == nil {
		t.Fatal("expected allocation failure for a table above MaxBytes")
	}
	if fixHash(0) != 2 || fixHash(1) != 3 || fixHash(77) != 77 {
		t.Fatal("fixHash")
	}
}
