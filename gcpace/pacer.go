package gcpace

import (
	"math"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"
)

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
