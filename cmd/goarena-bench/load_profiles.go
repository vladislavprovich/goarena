package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/internal/latency"
)

// Load profiles (-profile) change the load during the measured phase so
// that one run shows where an implementation degrades:
//
//	steady            constant load (the default; identical to earlier versions)
//	ramp              active goroutines 1, then +ceil(G/8) per stage up to G
//	sweep-goroutines  active goroutines 1, 2, 4, 8, ... up to G
//	spike             alternating stages: base, write spike (40 points fewer
//	                  reads), base, request-garbage burst (garbage rate 1)
//
// The measured duration is split into equal stages; per-stage statistics
// are written to Result.Stages.
var profileNames = []string{"steady", "ramp", "sweep-goroutines", "spike"}

// stage is one constant-load part of the measured phase.
type stage struct {
	Name        string
	Goroutines  int
	ReadRatio   float64
	GarbageRate float64
}

// buildStages returns the stages of the configured profile.
func buildStages(o *options) ([]stage, error) {
	g := o.Goroutines
	switch o.Profile {
	case "", "steady":
		return []stage{{Name: "", Goroutines: g, ReadRatio: o.ReadRatio, GarbageRate: o.GarbageRate}}, nil
	case "ramp":
		step := (g + 7) / 8
		out := []stage{}
		for n := 1; ; {
			out = append(out, stage{Name: fmt.Sprintf("g=%d", n), Goroutines: n, ReadRatio: o.ReadRatio, GarbageRate: o.GarbageRate})
			if n >= g {
				break
			}
			if n == 1 && step > 1 {
				n = step
			} else {
				n += step
			}
			if n > g {
				n = g
			}
		}
		return out, nil
	case "sweep-goroutines":
		out := []stage{}
		for n := 1; ; n *= 2 {
			if n > g {
				n = g
			}
			out = append(out, stage{Name: fmt.Sprintf("g=%d", n), Goroutines: n, ReadRatio: o.ReadRatio, GarbageRate: o.GarbageRate})
			if n == g {
				break
			}
		}
		return out, nil
	case "spike":
		spikeReads := math.Max(0.1, o.ReadRatio-0.4)
		base := stage{Name: "base", Goroutines: g, ReadRatio: o.ReadRatio, GarbageRate: o.GarbageRate}
		ws := stage{Name: "write-spike", Goroutines: g, ReadRatio: spikeReads, GarbageRate: o.GarbageRate}
		gb := stage{Name: "garbage-burst", Goroutines: g, ReadRatio: o.ReadRatio, GarbageRate: 1}
		return []stage{base, ws, base, gb, base, ws, base, gb}, nil
	}
	return nil, fmt.Errorf("unknown profile %q (have %v)", o.Profile, profileNames)
}

// loadControl holds the parameters workers read at the start of every
// block of 64 operations, so that a profile can change them while the
// workers run. Without a profile the workers never look at it.
type loadControl struct {
	active  atomic.Int32  // workers with index < active run, the others sleep
	readThr atomic.Uint32 // reads: opRng>>32 < readThr
	garbThr atomic.Uint32 // request garbage: uint32(opRng) < garbThr
	garbOn  atomic.Bool
}

func (lc *loadControl) apply(st stage) {
	lc.active.Store(int32(st.Goroutines))
	lc.readThr.Store(uint32(st.ReadRatio * float64(math.MaxUint32)))
	lc.garbThr.Store(uint32(st.GarbageRate * float64(math.MaxUint32)))
	lc.garbOn.Store(st.GarbageRate > 0)
}

// workerLive is the part of a worker's state the collector reads while
// the worker runs: cumulative counters (published every 64 operations) and
// an outbox for the latency samples of the current interval.
type workerLive struct {
	ops, misses, writeErrs atomic.Uint64
	epoch                  atomic.Uint64 // last collector epoch the worker flushed for
	mu                     sync.Mutex
	outbox                 *latency.Histogram
	_                      [32]byte
}

// collector produces the per-second progress lines and the per-stage
// statistics. It exists only when -progress is set or a profile other
// than steady is used, so that default runs measure exactly what earlier
// versions measured.
type collector struct {
	c        Cache
	o        *options
	out      io.Writer // progress lines, or nil
	epoch    atomic.Uint64
	start    time.Time // start of the run (populate)
	timeline []progressPoint

	// state of the current phase
	phase      string
	phaseStart time.Time
	workers    []workerLive
	populated  *atomic.Int64
	lastOps    uint64
	lastMisses uint64
	lastPop    int64
	lastTime   time.Time
	lastGC     gcstats.Snapshot
	interval   *latency.Histogram

	// stages
	stageIdx   int
	stageName  string
	stageCfg   stage
	stageStart time.Time
	stageOps   uint64
	stageMiss  uint64
	stageGC    gcstats.Snapshot
	stageHist  *latency.Histogram
	stages     []stageResult
}

// progressPoint is one progress line (JSON, one per second).
type progressPoint struct {
	Type          string  `json:"type"` // always "progress"
	Impl          string  `json:"impl"`
	Phase         string  `json:"phase"` // populate, warmup, measure
	Stage         string  `json:"stage"`
	StageIndex    int     `json:"stage_index"`
	T             float64 `json:"t_s"`       // seconds since the start of the run
	PhaseT        float64 `json:"phase_t_s"` // seconds since the start of the phase
	Interval      float64 `json:"interval_s"`
	Goroutines    int     `json:"goroutines"`
	ReadRatio     float64 `json:"read_ratio"`
	GarbageRate   float64 `json:"garbage_rate"`
	Ops           uint64  `json:"ops"` // operations in the interval
	OpsPerSec     float64 `json:"ops_per_sec"`
	Misses        uint64  `json:"misses"`
	HitRatio      float64 `json:"hit_ratio"`
	Samples       uint64  `json:"samples"`
	P50           uint64  `json:"p50_ns"`
	P99           uint64  `json:"p99_ns"`
	P999          uint64  `json:"p999_ns"`
	Max           uint64  `json:"max_ns"`
	GCCycles      uint64  `json:"gc_cycles"` // cycles in the interval
	GCCyclesTotal uint64  `json:"gc_cycles_total"`
	GCCPUPercent  float64 `json:"gc_cpu_percent"`
	STWms         float64 `json:"stw_ms"` // stop-the-world time in the interval
	HeapBytes     uint64  `json:"heap_bytes"`
	HeapGoalBytes uint64  `json:"heap_goal_bytes"`
	RSSBytes      uint64  `json:"rss_bytes"`
	OffHeapBytes  int64   `json:"offheap_bytes"`
	Populated     int64   `json:"populated"`
	N             int     `json:"n"`
}

// stageResult summarises one stage of a load profile.
type stageResult struct {
	Name         string          `json:"name"`
	Index        int             `json:"index"`
	Goroutines   int             `json:"goroutines"`
	ReadRatio    float64         `json:"read_ratio"`
	GarbageRate  float64         `json:"garbage_rate"`
	Start        float64         `json:"start_s"` // seconds since the start of the measured phase
	Seconds      float64         `json:"seconds"`
	Ops          uint64          `json:"ops"`
	OpsPerSec    float64         `json:"ops_per_sec"`
	Misses       uint64          `json:"misses"`
	Latency      latency.Summary `json:"latency"`
	GCCycles     uint64          `json:"gc_cycles"`
	GCCPUPercent float64         `json:"gc_cpu_percent"`
}

func newCollector(c Cache, o *options, out io.Writer) *collector {
	return &collector{c: c, o: o, out: out, start: time.Now(), interval: latency.New()}
}

// beginPhase starts reporting a phase; workers or populated may be nil.
func (cl *collector) beginPhase(phase string, workers []workerLive, populated *atomic.Int64) {
	now := time.Now()
	cl.phase, cl.phaseStart, cl.workers, cl.populated = phase, now, workers, populated
	cl.lastOps, cl.lastMisses, cl.lastPop, cl.lastTime = 0, 0, 0, now
	cl.lastGC = gcstats.Read()
	cl.interval.Reset()
}

// flush asks the workers to hand over their latency samples and merges
// them into the interval histogram.
func (cl *collector) flush() {
	if cl.workers == nil {
		return
	}
	e := cl.epoch.Add(1)
	deadline := time.Now().Add(5 * time.Millisecond)
	for i := range cl.workers {
		w := &cl.workers[i]
		for w.epoch.Load() < e && time.Now().Before(deadline) {
			time.Sleep(50 * time.Microsecond)
		}
		w.mu.Lock()
		if w.outbox != nil {
			cl.interval.Merge(w.outbox)
			w.outbox.Reset()
		}
		w.mu.Unlock()
	}
}

// tick emits one progress point for the time since the previous tick.
func (cl *collector) tick() {
	cl.flush()
	now := time.Now()
	g := gcstats.Read()
	d := gcstats.Between(cl.lastGC, g)
	var ops, misses uint64
	for i := range cl.workers {
		ops += cl.workers[i].ops.Load()
		misses += cl.workers[i].misses.Load()
	}
	el := now.Sub(cl.lastTime).Seconds()
	p := progressPoint{
		Type: "progress", Impl: cl.o.Impl, Phase: cl.phase, Stage: cl.stageName, StageIndex: cl.stageIdx,
		T: now.Sub(cl.start).Seconds(), PhaseT: now.Sub(cl.phaseStart).Seconds(), Interval: el,
		Goroutines: cl.o.Goroutines, ReadRatio: cl.o.ReadRatio, GarbageRate: cl.o.GarbageRate,
		GCCycles: d.GCCycles, GCCyclesTotal: g.GCCycles, GCCPUPercent: 100 * d.GCCPUFraction,
		STWms:     float64(d.PauseTotal) / 1e6,
		HeapBytes: g.HeapObjectsBytes, HeapGoalBytes: g.HeapGoalBytes, RSSBytes: g.Process.RSSBytes,
		OffHeapBytes: cheapOffHeap(cl.c), N: cl.o.N,
	}
	if cl.phase == "measure" {
		p.Goroutines, p.ReadRatio, p.GarbageRate = cl.stageCfg.Goroutines, cl.stageCfg.ReadRatio, cl.stageCfg.GarbageRate
	}
	if cl.populated != nil {
		pop := cl.populated.Load()
		p.Populated = pop
		p.Ops = uint64(pop - cl.lastPop)
		cl.lastPop = pop
	} else {
		p.Ops = ops - cl.lastOps
		p.Misses = misses - cl.lastMisses
		p.Populated = int64(cl.o.N)
	}
	cl.lastOps, cl.lastMisses = ops, misses
	if el > 0 {
		p.OpsPerSec = float64(p.Ops) / el
	}
	p.HitRatio = 1
	if p.Ops > 0 && cl.populated == nil {
		reads := float64(p.Ops) * p.ReadRatio
		if reads > 0 {
			p.HitRatio = math.Max(0, 1-float64(p.Misses)/reads)
		}
	}
	if s := cl.interval.Summarize(); s.Count > 0 {
		p.Samples, p.P50, p.P99, p.P999, p.Max = s.Count, s.P50, s.P99, s.P999, s.Max
	}
	if cl.stageHist != nil {
		cl.stageHist.Merge(cl.interval)
	}
	cl.interval.Reset()
	cl.lastTime, cl.lastGC = now, g
	cl.timeline = append(cl.timeline, p)
	if cl.out != nil {
		b, _ := json.Marshal(p)
		cl.out.Write(append(b, '\n'))
	}
}

// cheapOffHeap returns the off-heap bytes without an index scan.
func cheapOffHeap(c Cache) int64 {
	if ac, ok := c.(*arenaCache); ok {
		return ac.s.OffHeapBytes()
	}
	return c.OffHeapBytes()
}

// beginStage starts stage i (measure phase only).
func (cl *collector) beginStage(i int, st stage) {
	cl.stageIdx, cl.stageName, cl.stageCfg = i, st.Name, st
	cl.stageStart = time.Now()
	cl.stageGC = gcstats.Read()
	cl.stageHist = latency.New()
	cl.stageOps, cl.stageMiss = 0, 0
	for j := range cl.workers {
		cl.stageOps += cl.workers[j].ops.Load()
		cl.stageMiss += cl.workers[j].misses.Load()
	}
}

// endStage records the statistics of the current stage.
func (cl *collector) endStage() {
	cl.tick() // the samples up to now belong to this stage
	now := time.Now()
	var ops, misses uint64
	for j := range cl.workers {
		ops += cl.workers[j].ops.Load()
		misses += cl.workers[j].misses.Load()
	}
	d := gcstats.Between(cl.stageGC, gcstats.Read())
	sec := now.Sub(cl.stageStart).Seconds()
	sr := stageResult{
		Name: cl.stageName, Index: cl.stageIdx, Goroutines: cl.stageCfg.Goroutines,
		ReadRatio: cl.stageCfg.ReadRatio, GarbageRate: cl.stageCfg.GarbageRate,
		Start: cl.stageStart.Sub(cl.phaseStart).Seconds(), Seconds: sec,
		Ops: ops - cl.stageOps, Misses: misses - cl.stageMiss,
		Latency: cl.stageHist.Summarize(), GCCycles: d.GCCycles, GCCPUPercent: 100 * d.GCCPUFraction,
	}
	if sec > 0 {
		sr.OpsPerSec = float64(sr.Ops) / sec
	}
	if cl.phase == "measure" {
		cl.stages = append(cl.stages, sr)
	}
	cl.stageHist = nil
}

// runPhase drives the clock of a phase: it ticks every second until done
// is closed (populate) or until the stages have run (measure/warmup with
// stages given), and returns when the phase is over.
func (cl *collector) runPhase(stages []stage, stageDur time.Duration, lc *loadControl, done <-chan struct{}) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	if stages == nil {
		for {
			select {
			case <-tk.C:
				cl.tick()
			case <-done:
				cl.tick()
				return
			}
		}
	}
	for i, st := range stages {
		if lc != nil {
			lc.apply(st)
		}
		cl.beginStage(i, st)
		end := time.NewTimer(stageDur)
	loop:
		for {
			select {
			case <-tk.C:
				cl.tick()
			case <-end.C:
				break loop
			}
		}
		cl.endStage()
		tk.Reset(time.Second)
	}
}

// profileOrSteady returns p, or "steady" for the empty profile.
func profileOrSteady(p string) string {
	if p == "" {
		return "steady"
	}
	return p
}
