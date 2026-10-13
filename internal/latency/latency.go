// Package latency implements a compact log-linear latency histogram in the
// spirit of HdrHistogram, used by the benchmark harness and by the example
// dashboard.
//
// Values (nanoseconds) are bucketed by their power of two (the "magnitude")
// and, inside a magnitude, linearly into 2^SubBits sub-buckets, so the
// relative error of any reported quantile is below 1/2^SubBits (3.1 % for
// SubBits = 5). Values below 2^SubBits are stored exactly. The histogram has
// a fixed size (64 magnitudes x 32 sub-buckets = 2048 uint64 counters = 16
// KiB), needs no allocation when recording and is not safe for concurrent
// use; give every goroutine its own histogram and [Histogram.Merge] them.
package latency

import (
	"math"
	"math/bits"
)

// SubBits is the number of linear sub-bucket bits per power of two.
const SubBits = 5

const (
	subCount = 1 << SubBits
	nBuckets = (64 - SubBits + 1) * subCount
)

// Histogram is a log-linear histogram of non-negative integer values.
type Histogram struct {
	counts [nBuckets]uint64
	total  uint64
	sum    float64
	min    uint64
	max    uint64
}

// New returns an empty histogram. The zero value is also ready to use.
func New() *Histogram { return &Histogram{} }

// bucketOf maps a value to its bucket index.
func bucketOf(v uint64) int {
	if v < subCount {
		return int(v)
	}
	mag := bits.Len64(v) - SubBits // >= 1
	sub := (v >> uint(mag-1)) & (subCount - 1)
	return mag*subCount + int(sub)
}

// lowerBound returns the smallest value that maps to bucket i.
func lowerBound(i int) uint64 {
	if i < subCount {
		return uint64(i)
	}
	mag := i / subCount
	sub := uint64(i % subCount)
	return (subCount | sub) << uint(mag-1)
}

// upperBound returns the largest value that maps to bucket i.
func upperBound(i int) uint64 {
	if i+1 >= nBuckets {
		return math.MaxUint64
	}
	return lowerBound(i+1) - 1
}

// Record adds one observation.
func (h *Histogram) Record(v uint64) {
	if h.total == 0 || v < h.min {
		h.min = v
	}
	h.counts[bucketOf(v)]++
	h.total++
	h.sum += float64(v)
	if v > h.max {
		h.max = v
	}
}

// Count returns the number of observations.
func (h *Histogram) Count() uint64 { return h.total }

// Max returns the exact maximum observation (0 if empty).
func (h *Histogram) Max() uint64 { return h.max }

// Min returns the exact minimum observation (0 if empty).
func (h *Histogram) Min() uint64 {
	if h.total == 0 {
		return 0
	}
	return h.min
}

// Mean returns the arithmetic mean of the observations.
func (h *Histogram) Mean() float64 {
	if h.total == 0 {
		return 0
	}
	return h.sum / float64(h.total)
}

// Quantile returns an estimate of the q-quantile (0 <= q <= 1). The estimate
// is the midpoint of the bucket that contains the requested rank, clamped to
// the exact observed minimum and maximum.
func (h *Histogram) Quantile(q float64) uint64 {
	if h.total == 0 {
		return 0
	}
	if q <= 0 {
		return h.Min()
	}
	if q >= 1 {
		return h.max
	}
	rank := uint64(math.Ceil(q * float64(h.total)))
	if rank == 0 {
		rank = 1
	}
	var seen uint64
	for i := range h.counts {
		c := h.counts[i]
		if c == 0 {
			continue
		}
		seen += c
		if seen >= rank {
			lo, hi := lowerBound(i), upperBound(i)
			v := lo + (hi-lo)/2
			if v < h.min {
				v = h.min
			}
			if v > h.max {
				v = h.max
			}
			return v
		}
	}
	return h.max
}

// Merge adds all observations of o into h.
func (h *Histogram) Merge(o *Histogram) {
	if o.total == 0 {
		return
	}
	if h.total == 0 || o.min < h.min {
		h.min = o.min
	}
	if o.max > h.max {
		h.max = o.max
	}
	for i, c := range o.counts {
		h.counts[i] += c
	}
	h.total += o.total
	h.sum += o.sum
}

// Reset removes all observations.
func (h *Histogram) Reset() { *h = Histogram{} }

// Bucket is one non-empty bucket of a histogram.
type Bucket struct {
	Lower uint64 `json:"lo"`
	Upper uint64 `json:"hi"`
	Count uint64 `json:"n"`
}

// Buckets returns the non-empty buckets in ascending order; it is used to
// export full latency distributions (for CDF plots) into JSON.
func (h *Histogram) Buckets() []Bucket {
	var out []Bucket
	for i, c := range h.counts {
		if c != 0 {
			out = append(out, Bucket{Lower: lowerBound(i), Upper: upperBound(i), Count: c})
		}
	}
	return out
}

// Summary holds the most commonly reported statistics of a histogram.
type Summary struct {
	Count uint64  `json:"count"`
	Mean  float64 `json:"mean_ns"`
	P50   uint64  `json:"p50_ns"`
	P90   uint64  `json:"p90_ns"`
	P99   uint64  `json:"p99_ns"`
	P999  uint64  `json:"p999_ns"`
	Max   uint64  `json:"max_ns"`
}

// Summarize computes a [Summary].
func (h *Histogram) Summarize() Summary {
	return Summary{
		Count: h.total,
		Mean:  h.Mean(),
		P50:   h.Quantile(0.50),
		P90:   h.Quantile(0.90),
		P99:   h.Quantile(0.99),
		P999:  h.Quantile(0.999),
		Max:   h.max,
	}
}
