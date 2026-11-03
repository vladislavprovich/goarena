package gcstats

import (
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
