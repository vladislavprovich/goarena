package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/vladislavprovich/goarena/gcpace"
	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/internal/cmem"
	"github.com/vladislavprovich/goarena/internal/tsc"
)

// options are the parameters of one benchmark run.
type options struct {
	Impl        string        `json:"impl"`
	N           int           `json:"n"`
	ValueSize   int           `json:"value_size"`
	Goroutines  int           `json:"goroutines"`
	ReadRatio   float64       `json:"read_ratio"`
	Dist        string        `json:"dist"`
	ZipfS       float64       `json:"zipf_s"`
	Duration    time.Duration `json:"duration_ns"`
	Warmup      time.Duration `json:"warmup_ns"`
	SampleEvery int           `json:"sample_every"`
	Shards      int           `json:"shards"`
	ChunkMiB    int           `json:"chunk_mib"`
	Seed        int64         `json:"seed"`
	Experiment  string        `json:"experiment"`
	Repeat      int           `json:"repeat"`
	MemLimitMiB int           `json:"memlimit_mib"` // debug.SetMemoryLimit; 0 = none
	GCTrace     bool          `json:"gctrace"`      // child runs with GODEBUG=gctrace=1

	// Request garbage (experiments E9, E10): with probability GarbageRate
	// an operation first allocates a short-lived "request" object (a
	// struct with pointers, a copy of the key and a GarbageBytes payload)
	// that stays reachable for the next 32 operations of the goroutine.
	// The same garbage is generated for every implementation; when it is
	// enabled the recorded latency covers allocation + cache operation.
	GarbageRate  float64 `json:"garbage_rate"`
	GarbageBytes int     `json:"garbage_bytes"`
}

// phaseMarker writes a phase boundary to stderr when the process runs with
// GODEBUG=gctrace=1, so that the aggregator can attribute every gctrace
// line (which the runtime also writes to stderr) to a benchmark phase.
func phaseMarker(name string) {
	if strings.Contains(os.Getenv("GODEBUG"), "gctrace=1") {
		fmt.Fprintf(os.Stderr, "#phase %s\n", name)
	}
}

func (o *options) validate() error {
	if _, ok := impls[o.Impl]; !ok {
		return fmt.Errorf("unknown implementation %q (have %v)", o.Impl, implNames())
	}
	switch {
	case o.N <= 0:
		return fmt.Errorf("n must be positive")
	case o.ValueSize < 8:
		return fmt.Errorf("value size must be >= 8")
	case o.Goroutines <= 0:
		return fmt.Errorf("goroutines must be positive")
	case o.ReadRatio < 0 || o.ReadRatio > 1:
		return fmt.Errorf("read ratio must be in [0,1]")
	case o.Dist != "uniform" && o.Dist != "zipf":
		return fmt.Errorf("dist must be uniform or zipf")
	case o.SampleEvery <= 0 || o.SampleEvery&(o.SampleEvery-1) != 0:
		return fmt.Errorf("sample-every must be a power of two")
	case o.Shards <= 0 || o.Shards&(o.Shards-1) != 0:
		return fmt.Errorf("shards must be a power of two")
	case o.ChunkMiB <= 0:
		return fmt.Errorf("chunk size must be positive")
	case o.GarbageRate < 0 || o.GarbageRate > 1 || o.GarbageBytes < 0:
		return fmt.Errorf("garbage rate must be in [0,1] and garbage bytes >= 0")
	}
	return nil
}

// startProfile starts a CPU profile if path is not empty and returns the
// function that stops it.
func startProfile(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { pprof.StopCPUProfile(); f.Close() }, nil
}

// run executes one complete benchmark: populate, warm up, measure.
func run(o options) (*Result, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	tsc.Calibrate()
	info := impls[o.Impl]
	res := newResult(o)

	// The memory limit is applied before the cache is created (v0.3.0;
	// earlier versions applied it right after New, which only allocates a
	// few empty shards) so that goarena-paced inherits it as its budget.
	if o.MemLimitMiB > 0 {
		debug.SetMemoryLimit(int64(o.MemLimitMiB) << 20)
	}
	c, err := info.New(o)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	res.GOGC = currentGOGC()

	cm0 := cmem.ReadStats()
	g0 := gcstats.Read()
	phaseMarker("populate-start")
	popTime, err := populate(c, &o)
	phaseMarker("populate-end")
	if err != nil {
		return nil, fmt.Errorf("populate: %w", err)
	}
	g1 := gcstats.Read()
	cm1 := cmem.ReadStats()
	res.PopulateSeconds = popTime.Seconds()
	res.PopulateOpsPerSec = float64(o.N) / popTime.Seconds()
	res.PopulateGC = gcstats.Between(g0, g1)
	res.PopulateCgoCalls = cm1.CgoCalls - cm0.CgoCalls
	res.AfterPopulate = g1.Gauges()
	if bc, ok := c.(interface{ afterPopulate() int64 }); ok {
		res.GoMemLimitMiB = bc.afterPopulate() >> 20
	}

	if o.Warmup > 0 {
		mixed(c, &o, o.Warmup, false)
	}

	g2 := gcstats.Read()
	cm2 := cmem.ReadStats()
	stopProfile, err := startProfile(cpuProfile)
	if err != nil {
		return nil, err
	}
	phaseMarker("measure-start")
	ph := mixed(c, &o, o.Duration, true)
	phaseMarker("measure-end")
	stopProfile()
	g3 := gcstats.Read()
	cm3 := cmem.ReadStats()

	res.Ops, res.Reads, res.Writes, res.Misses = ph.Ops, ph.Reads, ph.Writes, ph.Misses
	res.MeasuredSeconds = ph.Elapsed.Seconds()
	res.OpsPerSec = float64(ph.Ops) / ph.Elapsed.Seconds()
	res.Latency = ph.All.Summarize()
	res.ReadLatency = ph.ReadLat.Summarize()
	res.WriteLatency = ph.WriteLat.Summarize()
	res.LatencyBuckets = ph.All.Buckets()
	res.GC = gcstats.Between(g2, g3)
	res.GCCPUPercent = 100 * res.GC.GCCPUFraction
	res.After = g3.Gauges()

	// A forced, blocking full collection with the data set still live
	// measures the cost of one complete GC cycle (mark + sweep) directly.
	phaseMarker("forced-gc")
	t0 := tsc.Ticks()
	runtime.GC()
	res.ForcedGCms = float64(tsc.ToNanos(tsc.Ticks()-t0)) / 1e6
	phaseMarker("forced-gc-end")
	res.MeasuredCgoCalls = cm3.CgoCalls - cm2.CgoCalls
	res.OffHeapBytes = c.OffHeapBytes()
	res.CmemLiveBytes = cm3.LiveBytes
	if p := gcpace.Default(); p != nil {
		ps := p.Stats()
		res.Pacer = &ps
	}
	if ac, ok := c.(*arenaCache); ok {
		st := ac.storeStats()
		res.Store = &st
	}
	runtime.KeepAlive(c)
	return res, nil
}
