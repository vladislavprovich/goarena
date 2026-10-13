// Package gcpace adapts the Go garbage collector's pacing to programs that
// keep a large part of their data off-heap.
//
// # The problem
//
// With GOGC = 100 the runtime starts a GC cycle when the heap reaches about
// twice the live heap. A program whose large data set lives in goarena's
// C memory has a tiny live Go heap (a few MiB), so the heap goal is tiny
// too: a service that allocates a short-lived request object per operation
// then runs hundreds of GC cycles per second. Each cycle is cheap (nothing
// big to mark), but the stop-the-world phases, mark assists and write
// barriers add up. A heap-resident cache of the same size would have given
// the GC gigabytes of headroom and would collect a few times per second.
//
// # What the pacer does
//
// A Pacer periodically sets the GC percent so that the heap goal is the one
// the runtime would compute if the off-heap bytes were part of the heap:
//
//	goal = live + (live + offHeap) * GOGC/100
//	     = live * (1 + p/100)   with   p = GOGC * (live + offHeap) / live
//
// i.e. GC frequency equals that of a heap-resident store of the same size,
// while every cycle still marks only the small Go heap. If a total memory
// budget is configured (or a GOMEMLIMIT was in effect when the pacer
// started), the pacer also lowers the Go memory limit to
// budget - offHeap, because GOMEMLIMIT counts only memory managed by the Go
// runtime. Both settings are restored when the pacer stops.
//
// The pacer changes process-wide runtime settings. Use one per process:
// [Register] adds an off-heap source to the shared default pacer, which is
// what store.Config.GCPacing does.
package gcpace

import (
	"math"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"
)

// Defaults of [Config].
const (
	DefaultInterval   = 50 * time.Millisecond
	DefaultMinGoLimit = 64 << 20  // 64 MiB
	DefaultMaxPercent = 1_000_000 // upper bound of the GC percent set by the pacer
	minLiveHeap       = 4 << 20   // the runtime's own minimum heap goal
)

// Config configures a [Pacer]. The zero value is valid: it keeps the GOGC
// and GOMEMLIMIT that are in effect when the pacer starts as the targets
// for "Go heap + off-heap".
type Config struct {
	// GOGC is the effective GC percent applied to the Go heap plus the
	// off-heap bytes. Zero means the value in effect when the pacer
	// starts; if the GC was disabled then (GOGC=off), the pacer leaves the
	// GC percent alone and only manages the memory limit.
	GOGC int

	// MemoryLimit is the total memory budget of the process (Go memory +
	// off-heap). Zero means the GOMEMLIMIT in effect when the pacer
	// starts; a negative value or math.MaxInt64 means no budget.
	MemoryLimit int64

	// MinGoLimit is the smallest Go memory limit the pacer sets when the
	// off-heap bytes approach the budget. Zero means DefaultMinGoLimit.
	MinGoLimit int64

	// Interval is the adjustment period. Zero means DefaultInterval.
	Interval time.Duration

	// MaxPercent bounds the GC percent the pacer sets. Zero means
	// DefaultMaxPercent.
	MaxPercent int
}

// Stats describes the last adjustment of a pacer.
type Stats struct {
	OffHeapBytes  int64 // sum of all registered sources
	LiveHeapBytes int64 // Go live heap used for the computation
	GCPercent     int   // GC percent in effect (-1 = untouched/off)
	GoMemoryLimit int64 // Go memory limit in effect (math.MaxInt64 = none)
	Updates       int64 // adjustments performed
}

// Pacer adjusts GOGC and GOMEMLIMIT for off-heap memory. Create it with
// [Start]; it is safe for concurrent use.
type Pacer struct {
	mu        sync.Mutex
	gogc      int   // target percent (<0: do not touch the GC percent)
	budget    int64 // total budget (math.MaxInt64: none)
	minLimit  int64
	maxPct    int
	origGOGC  int
	origLimit int64
	sources   map[int]func() int64
	nextID    int
	stats     Stats
	stopped   bool
	stop      chan struct{}
	done      chan struct{}
}

// currentGCPercent reads the GC percent (setting it requires a write,
// which is undone immediately).
func currentGCPercent() int {
	v := debug.SetGCPercent(100)
	debug.SetGCPercent(v)
	return v
}

// Start creates a pacer and its background goroutine. The settings in
// effect at this moment are restored by [Pacer.Stop].
func Start(cfg Config) *Pacer {
	p := &Pacer{
		origGOGC:  currentGCPercent(),
		origLimit: debug.SetMemoryLimit(-1),
		sources:   map[int]func() int64{},
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	p.gogc = cfg.GOGC
	if p.gogc == 0 {
		p.gogc = p.origGOGC // may be -1: leave the GC percent alone
	}
	p.budget = cfg.MemoryLimit
	if p.budget == 0 {
		p.budget = p.origLimit
	}
	if p.budget < 0 {
		p.budget = math.MaxInt64
	}
	p.minLimit = cfg.MinGoLimit
	if p.minLimit <= 0 {
		p.minLimit = DefaultMinGoLimit
	}
	p.maxPct = cfg.MaxPercent
	if p.maxPct <= 0 {
		p.maxPct = DefaultMaxPercent
	}
	p.stats.GCPercent = p.origGOGC
	p.stats.GoMemoryLimit = p.origLimit
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	go p.loop(interval)
	return p
}

func (p *Pacer) loop(interval time.Duration) {
	defer close(p.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.Update()
		}
	}
}

// Add registers a source of off-heap bytes and returns the function that
// removes it again. Sources must be cheap and safe for concurrent use.
func (p *Pacer) Add(src func() int64) (remove func()) {
	p.mu.Lock()
	id := p.nextID
	p.nextID++
	p.sources[id] = src
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			delete(p.sources, id)
			p.mu.Unlock()
		})
	}
}

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

var liveSample = []metrics.Sample{{Name: "/gc/heap/live:bytes"}}

// Update recomputes and applies the settings immediately (the background
// goroutine calls it every interval).
func (p *Pacer) Update() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	var off int64
	for _, src := range p.sources {
		off += src()
	}
	metrics.Read(liveSample)
	var live int64
	if liveSample[0].Value.Kind() == metrics.KindUint64 {
		live = int64(liveSample[0].Value.Uint64())
	}
	p.stats.OffHeapBytes, p.stats.LiveHeapBytes = off, live
	if p.gogc >= 0 {
		pct := Percent(p.gogc, live, off, p.maxPct)
		// avoid touching the runtime for changes below 2 %
		if cur := p.stats.GCPercent; cur < 0 || pct*50 > cur*51 || pct*51 < cur*50 {
			debug.SetGCPercent(pct)
			p.stats.GCPercent = pct
			p.stats.Updates++
		}
	}
	if lim := GoLimit(p.budget, off, p.minLimit); lim != p.stats.GoMemoryLimit {
		diff := lim - p.stats.GoMemoryLimit
		if diff < 0 {
			diff = -diff
		}
		if lim == math.MaxInt64 || p.stats.GoMemoryLimit == math.MaxInt64 || diff >= 1<<20 {
			debug.SetMemoryLimit(lim)
			p.stats.GoMemoryLimit = lim
			p.stats.Updates++
		}
	}
}

// Stats returns the state after the last adjustment.
func (p *Pacer) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// Stop ends the background goroutine and restores the GC percent and the
// memory limit that were in effect when the pacer started. Stop is
// idempotent.
func (p *Pacer) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	close(p.stop)
	debug.SetGCPercent(p.origGOGC)
	debug.SetMemoryLimit(p.origLimit)
	p.mu.Unlock()
	<-p.done
}

// ------------------------------------------------------- default pacer

var (
	defMu     sync.Mutex
	defCfg    Config
	defPacer  *Pacer
	defActive int
)

// SetDefaultConfig sets the configuration used when the default pacer is
// started by the next [Register] (it does not affect a running pacer).
func SetDefaultConfig(cfg Config) {
	defMu.Lock()
	defCfg = cfg
	defMu.Unlock()
}

// Register adds an off-heap source to the process-wide default pacer,
// starting the pacer on the first registration. The returned function
// removes the source; when the last source is removed the pacer stops and
// restores the original GC settings.
func Register(src func() int64) (unregister func()) {
	defMu.Lock()
	if defPacer == nil {
		defPacer = Start(defCfg)
	}
	p := defPacer
	remove := p.Add(src)
	defActive++
	defMu.Unlock()
	p.Update()
	var once sync.Once
	return func() {
		once.Do(func() {
			remove()
			defMu.Lock()
			defActive--
			if defActive == 0 && defPacer == p {
				defPacer = nil
				defMu.Unlock()
				p.Stop()
				return
			}
			defMu.Unlock()
		})
	}
}

// Default returns the running default pacer, or nil.
func Default() *Pacer {
	defMu.Lock()
	defer defMu.Unlock()
	return defPacer
}
