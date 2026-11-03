package gcstats

import (
	"math"
	"time"
)

// Delta summarises what happened between two snapshots.
type Delta struct {
	Duration time.Duration `json:"duration_ns"`

	GCCycles uint64 `json:"gc_cycles"`

	// GCCPUSeconds is the estimated CPU time spent in the GC.
	GCCPUSeconds float64 `json:"gc_cpu_seconds"`
	// GCCPUFraction is GC CPU time divided by the available CPU time
	// (GOMAXPROCS x wall time); it is the quantity the GC pacer targets
	// (25 % during a cycle) and the one reported as "GC CPU %".
	GCCPUFraction float64 `json:"gc_cpu_fraction"`
	// GCShareOfBusy is GC CPU time divided by non-idle CPU time.
	GCShareOfBusy float64 `json:"gc_share_of_busy"`
	// AssistCPUSeconds is the part of GC CPU time stolen from goroutines
	// that allocate (mark assists) - the GC cost on the request path.
	AssistCPUSeconds float64 `json:"assist_cpu_seconds"`

	// Pauses: every GC cycle has two short stop-the-world phases.
	PauseEvents     uint64        `json:"pause_events"`       // STW events (histogram)
	PauseMaxEvent   time.Duration `json:"pause_max_event_ns"` // upper bound of the largest STW event bucket
	PauseTotal      time.Duration `json:"pause_total_ns"`     // exact total STW time (MemStats)
	CyclePauseMax   time.Duration `json:"cycle_pause_max_ns"` // exact largest per-cycle STW time
	CyclePauseExact bool          `json:"cycle_pause_exact"`  // false if >256 cycles (ring overflow)

	AllocBytes uint64 `json:"alloc_bytes"` // bytes allocated on the Go heap

	ProcessCPUSeconds float64 `json:"process_cpu_seconds"` // user+kernel CPU of the process (OS)
}

// Between computes the difference between snapshots a (earlier) and b.
func Between(a, b Snapshot) Delta {
	d := Delta{
		Duration:          b.Time.Sub(a.Time),
		GCCycles:          b.GCCycles - a.GCCycles,
		GCCPUSeconds:      b.GCCPUSeconds - a.GCCPUSeconds,
		AssistCPUSeconds:  b.GCAssistCPUSeconds - a.GCAssistCPUSeconds,
		PauseTotal:        time.Duration(b.PauseTotalNs - a.PauseTotalNs),
		AllocBytes:        b.AllocBytes - a.AllocBytes,
		ProcessCPUSeconds: b.Process.CPUSeconds() - a.Process.CPUSeconds(),
	}
	if tot := b.TotalCPUSeconds - a.TotalCPUSeconds; tot > 0 {
		d.GCCPUFraction = d.GCCPUSeconds / tot
		if busy := tot - (b.IdleCPUSeconds - a.IdleCPUSeconds); busy > 0 {
			d.GCShareOfBusy = d.GCCPUSeconds / busy
		}
	}
	if a.pauses != nil && b.pauses != nil && len(a.pauses.Counts) == len(b.pauses.Counts) {
		for i := len(b.pauses.Counts) - 1; i >= 0; i-- {
			c := b.pauses.Counts[i] - a.pauses.Counts[i]
			d.PauseEvents += c
			if c > 0 && d.PauseMaxEvent == 0 {
				d.PauseMaxEvent = bucketUpper(b.pauses.Buckets, i)
			}
		}
	}
	// Exact per-cycle pauses from the MemStats ring buffer.
	n := uint64(b.memNumGC - a.memNumGC)
	d.CyclePauseExact = n <= 256
	if n > 256 {
		n = 256
	}
	for k := uint64(0); k < n; k++ {
		cycle := uint64(b.memNumGC) - k // 1-based cycle number
		p := time.Duration(b.cyclePause[(cycle+255)%256])
		if p > d.CyclePauseMax {
			d.CyclePauseMax = p
		}
	}
	return d
}

// bucketUpper returns the upper boundary of histogram bucket i as a
// duration, using the lower boundary when the upper one is +Inf.
func bucketUpper(b []float64, i int) time.Duration {
	up := b[i+1]
	if math.IsInf(up, 1) {
		up = b[i]
	}
	return time.Duration(up * 1e9)
}
