package gcpace

import "math"

// Percent returns the GC percent that yields the heap goal of a heap of
// live+off bytes under gogc, expressed for a heap of live bytes, clamped to
// [gogc, maxPct]. live is raised to the runtime's 4 MiB minimum.
func Percent(gogc int, live, off int64, maxPct int) int {
	if gogc < 0 {
		return gogc
	}
	if live < minLiveHeap {
		live = minLiveHeap
	}
	if off < 0 {
		off = 0
	}
	p := float64(gogc) * float64(live+off) / float64(live)
	if p > float64(maxPct) {
		return maxPct
	}
	if int(p) < gogc {
		return gogc
	}
	return int(p)
}

// GoLimit returns the Go memory limit for a total budget and off bytes of
// off-heap memory, at least minLimit; math.MaxInt64 means no limit.
func GoLimit(budget, off, minLimit int64) int64 {
	if budget == math.MaxInt64 {
		return math.MaxInt64
	}
	l := budget - off
	if l < minLimit {
		l = minLimit
	}
	return l
}
