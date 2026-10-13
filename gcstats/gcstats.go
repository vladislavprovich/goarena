package gcstats

import (
	"math"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"
)

// Metric names read by this package (runtime/metrics, Go 1.21+).
const (
	mGCCycles   = "/gc/cycles/total:gc-cycles"
	mGCCPU      = "/cpu/classes/gc/total:cpu-seconds"
	mGCAssist   = "/cpu/classes/gc/mark/assist:cpu-seconds"
	mGCPauseCPU = "/cpu/classes/gc/pause:cpu-seconds"
	mTotalCPU   = "/cpu/classes/total:cpu-seconds"
	mIdleCPU    = "/cpu/classes/idle:cpu-seconds"
	mUserCPU    = "/cpu/classes/user:cpu-seconds"
	mHeapLive   = "/gc/heap/live:bytes"
	mHeapGoal   = "/gc/heap/goal:bytes"
	mHeapObjB   = "/memory/classes/heap/objects:bytes"
	mHeapObjN   = "/gc/heap/objects:objects"
	mScanHeap   = "/gc/scan/heap:bytes"
	mScanTotal  = "/gc/scan/total:bytes"
	mAllocBytes = "/gc/heap/allocs:bytes"
	mRuntimeTot = "/memory/classes/total:bytes"
	mGoroutines = "/sched/goroutines:goroutines"
	mGOGC       = "/gc/gogc:percent"
	mPausesNew  = "/sched/pauses/total/gc:seconds" // Go 1.22+
	mPausesOld  = "/gc/pauses:seconds"             // deprecated fallback
)

var (
	initOnce  sync.Once
	pauseName string
	supported map[string]bool
)

func setup() {
	initOnce.Do(func() {
		supported = map[string]bool{}
		for _, d := range metrics.All() {
			supported[d.Name] = true
		}
		pauseName = mPausesNew
		if !supported[pauseName] {
			pauseName = mPausesOld
		}
	})
}

// Snapshot is the state of the Go runtime and the process at one instant.
// Cumulative fields only make sense as differences between two snapshots;
// use [Between].
type Snapshot struct {
	Time time.Time

	// Cumulative GC counters.
	GCCycles           uint64  // completed GC cycles
	GCCPUSeconds       float64 // estimated CPU time spent in GC (all workers)
	GCAssistCPUSeconds float64 // part of GCCPUSeconds spent in mutator assists
	GCPauseCPUSeconds  float64 // part of GCCPUSeconds spent stopped-the-world
	TotalCPUSeconds    float64 // CPU time available: GOMAXPROCS x wall time
	IdleCPUSeconds     float64 // available CPU time that was idle
	UserCPUSeconds     float64 // CPU time spent running Go code
	AllocBytes         uint64  // bytes allocated on the heap since start
	PauseTotalNs       uint64  // total stop-the-world time (MemStats)

	// Gauges.
	HeapLiveBytes    uint64 // live heap after the last GC (marked bytes)
	HeapGoalBytes    uint64 // heap size at which the next GC starts
	HeapObjectsBytes uint64 // bytes in allocated heap objects (incl. dead)
	HeapObjects      uint64 // allocated heap objects (incl. dead)
	HeapScanBytes    uint64 // scannable heap bytes (pointer-bearing)
	ScanTotalBytes   uint64 // heap + stacks + globals scannable bytes
	RuntimeBytes     uint64 // all memory mapped by the Go runtime
	Goroutines       uint64
	GOGC             int64

	Process ProcessInfo // OS view (resident set, private bytes, CPU time)

	pauses     *metrics.Float64Histogram // individual STW pauses (cumulative)
	cyclePause [256]uint64               // MemStats.PauseNs ring buffer
	memNumGC   uint32                    // MemStats.NumGC matching cyclePause
}

// Read takes a snapshot. It calls runtime.ReadMemStats, which stops the
// world for a few microseconds, so call it at most a few times per second.
func Read() Snapshot {
	setup()
	names := []string{
		mGCCycles, mGCCPU, mGCAssist, mGCPauseCPU, mTotalCPU, mIdleCPU, mUserCPU,
		mHeapLive, mHeapGoal, mHeapObjB, mHeapObjN, mScanHeap, mScanTotal,
		mAllocBytes, mRuntimeTot, mGoroutines, mGOGC, pauseName,
	}
	samples := make([]metrics.Sample, 0, len(names))
	for _, n := range names {
		if supported[n] {
			samples = append(samples, metrics.Sample{Name: n})
		}
	}
	metrics.Read(samples)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	s := Snapshot{Time: time.Now(), PauseTotalNs: ms.PauseTotalNs, cyclePause: ms.PauseNs, memNumGC: ms.NumGC}
	for _, smp := range samples {
		v := smp.Value
		switch v.Kind() {
		case metrics.KindUint64:
			u := v.Uint64()
			switch smp.Name {
			case mGCCycles:
				s.GCCycles = u
			case mHeapLive:
				s.HeapLiveBytes = u
			case mHeapGoal:
				s.HeapGoalBytes = u
			case mHeapObjB:
				s.HeapObjectsBytes = u
			case mHeapObjN:
				s.HeapObjects = u
			case mScanHeap:
				s.HeapScanBytes = u
			case mScanTotal:
				s.ScanTotalBytes = u
			case mAllocBytes:
				s.AllocBytes = u
			case mRuntimeTot:
				s.RuntimeBytes = u
			case mGoroutines:
				s.Goroutines = u
			case mGOGC:
				s.GOGC = int64(u)
			}
		case metrics.KindFloat64:
			f := v.Float64()
			switch smp.Name {
			case mGCCPU:
				s.GCCPUSeconds = f
			case mGCAssist:
				s.GCAssistCPUSeconds = f
			case mGCPauseCPU:
				s.GCPauseCPUSeconds = f
			case mTotalCPU:
				s.TotalCPUSeconds = f
			case mIdleCPU:
				s.IdleCPUSeconds = f
			case mUserCPU:
				s.UserCPUSeconds = f
			}
		case metrics.KindFloat64Histogram:
			h := v.Float64Histogram()
			s.pauses = &metrics.Float64Histogram{
				Counts:  append([]uint64(nil), h.Counts...),
				Buckets: append([]float64(nil), h.Buckets...),
			}
		}
	}
	s.Process = readProcess()
	return s
}

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

// Gauges is a JSON-friendly copy of the instantaneous values of a snapshot.
type Gauges struct {
	HeapLiveBytes    uint64 `json:"heap_live_bytes"`
	HeapGoalBytes    uint64 `json:"heap_goal_bytes"`
	HeapObjectsBytes uint64 `json:"heap_objects_bytes"`
	HeapObjects      uint64 `json:"heap_objects"`
	HeapScanBytes    uint64 `json:"heap_scan_bytes"`
	RuntimeBytes     uint64 `json:"runtime_total_bytes"`
	Goroutines       uint64 `json:"goroutines"`
	GOGC             int64  `json:"gogc"`
	GCCycles         uint64 `json:"gc_cycles_total"`
	RSSBytes         uint64 `json:"rss_bytes"`
	PeakRSSBytes     uint64 `json:"peak_rss_bytes"`
	PrivateBytes     uint64 `json:"private_bytes"`
}

// Gauges extracts the instantaneous values of s.
func (s Snapshot) Gauges() Gauges {
	return Gauges{
		HeapLiveBytes:    s.HeapLiveBytes,
		HeapGoalBytes:    s.HeapGoalBytes,
		HeapObjectsBytes: s.HeapObjectsBytes,
		HeapObjects:      s.HeapObjects,
		HeapScanBytes:    s.HeapScanBytes,
		RuntimeBytes:     s.RuntimeBytes,
		Goroutines:       s.Goroutines,
		GOGC:             s.GOGC,
		GCCycles:         s.GCCycles,
		RSSBytes:         s.Process.RSSBytes,
		PeakRSSBytes:     s.Process.PeakRSSBytes,
		PrivateBytes:     s.Process.PrivateBytes,
	}
}

// ProcessInfo is the operating system's view of the current process.
// Fields that the platform cannot provide are zero.
type ProcessInfo struct {
	RSSBytes      uint64  // resident set size (Windows: working set)
	PeakRSSBytes  uint64  // peak resident set size
	PrivateBytes  uint64  // committed private memory (Windows), 0 elsewhere
	UserSeconds   float64 // user-mode CPU time
	KernelSeconds float64 // kernel-mode CPU time
}

// CPUSeconds returns user + kernel CPU time.
func (p ProcessInfo) CPUSeconds() float64 { return p.UserSeconds + p.KernelSeconds }

// Process returns the current OS-level memory and CPU figures.
func Process() ProcessInfo { return readProcess() }
