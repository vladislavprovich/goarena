package latency

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

func TestBucketBoundsRoundTrip(t *testing.T) {
	for i := 0; i < nBuckets-1; i++ {
		lo, hi := lowerBound(i), upperBound(i)
		if lo > hi {
			t.Fatalf("bucket %d: lo %d > hi %d", i, lo, hi)
		}
		if b := bucketOf(lo); b != i {
			t.Fatalf("bucketOf(lower(%d)=%d) = %d", i, lo, b)
		}
		if b := bucketOf(hi); b != i {
			t.Fatalf("bucketOf(upper(%d)=%d) = %d", i, hi, b)
		}
	}
	if bucketOf(math.MaxUint64) != nBuckets-1 {
		t.Fatalf("max value must land in the last bucket")
	}
}

func TestQuantileAccuracy(t *testing.T) {
	h := New()
	r := rand.New(rand.NewSource(1))
	vals := make([]uint64, 200000)
	for i := range vals {
		v := uint64(r.ExpFloat64()*500) + 20
		vals[i] = v
		h.Record(v)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	for _, q := range []float64{0.5, 0.9, 0.99, 0.999} {
		exact := vals[int(math.Ceil(q*float64(len(vals))))-1]
		got := h.Quantile(q)
		if rel := math.Abs(float64(got)-float64(exact)) / float64(exact); rel > 0.035 {
			t.Errorf("q=%v exact=%d got=%d rel err %.3f", q, exact, got, rel)
		}
	}
	if h.Max() != vals[len(vals)-1] || h.Min() != vals[0] {
		t.Errorf("min/max mismatch")
	}
	s := h.Summarize()
	if s.Count != uint64(len(vals)) || s.P50 > s.P99 || s.P99 > s.Max {
		t.Errorf("bad summary %+v", s)
	}
}

func TestMergeAndReset(t *testing.T) {
	a, b := New(), New()
	for i := uint64(1); i <= 100; i++ {
		a.Record(i)
		b.Record(i * 1000)
	}
	a.Merge(b)
	if a.Count() != 200 || a.Max() != 100000 || a.Min() != 1 {
		t.Fatalf("merge: count=%d max=%d min=%d", a.Count(), a.Max(), a.Min())
	}
	if len(a.Buckets()) == 0 {
		t.Fatal("expected buckets")
	}
	a.Reset()
	if a.Count() != 0 || a.Quantile(0.5) != 0 || a.Mean() != 0 || a.Min() != 0 {
		t.Fatal("reset failed")
	}
	a.Merge(New()) // merging an empty histogram keeps it empty
	if a.Count() != 0 {
		t.Fatal("merge of empty changed count")
	}
}

func TestQuantileEdges(t *testing.T) {
	h := New()
	h.Record(7)
	h.Record(9000)
	if h.Quantile(0) != 7 || h.Quantile(1) != 9000 {
		t.Fatal("edge quantiles")
	}
	if h.Quantile(0.5) != 7 {
		t.Fatalf("median of {7,9000} = %d", h.Quantile(0.5))
	}
}

func BenchmarkRecord(b *testing.B) {
	h := New()
	for i := 0; i < b.N; i++ {
		h.Record(uint64(i & 0xffff))
	}
}
