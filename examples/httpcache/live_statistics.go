package main

import (
	_ "embed"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/internal/latency"
)

// historyLen is the number of one-second snapshots kept for /stats/history,
// which lets a freshly opened dashboard start with full charts.
const historyLen = 120

// latShards is the number of independently locked latency histograms.
const latShards = 16

// latWindow collects operation latencies for the current one-second window.
type latWindow struct {
	shards [latShards]struct {
		mu sync.Mutex
		h  latency.Histogram
		_  [40]byte
	}
}

func (w *latWindow) record(shard int, ns uint64) {
	s := &w.shards[shard%latShards]
	s.mu.Lock()
	s.h.Record(ns)
	s.mu.Unlock()
}

// drain merges and resets all shards.
func (w *latWindow) drain() latency.Summary {
	total := latency.New()
	for i := range w.shards {
		s := &w.shards[i]
		s.mu.Lock()
		total.Merge(&s.h)
		s.h.Reset()
		s.mu.Unlock()
	}
	return total.Summarize()
}

// counters are cumulative operation counts.
type counters struct {
	gets, hits, misses, sets, deletes, httpRequests atomic.Uint64
}

func (c *counters) ops() uint64 { return c.gets.Load() + c.sets.Load() + c.deletes.Load() }

// statsDoc is the JSON document served at /stats.
type statsDoc struct {
	Backend      string          `json:"backend"`
	Time         time.Time       `json:"time"`
	UptimeSec    float64         `json:"uptime_s"`
	OpsTotal     uint64          `json:"ops_total"`
	OpsPerSec    float64         `json:"ops_per_sec"`
	Gets         uint64          `json:"gets"`
	Hits         uint64          `json:"hits"`
	Misses       uint64          `json:"misses"`
	Sets         uint64          `json:"sets"`
	Deletes      uint64          `json:"deletes"`
	HTTPRequests uint64          `json:"http_requests"`
	Entries      int             `json:"entries"`
	OffHeapBytes int64           `json:"offheap_bytes"`
	Latency      latency.Summary `json:"latency_1s"`

	GCCPUPercent      float64 `json:"gc_cpu_percent_1s"`
	GCCPUPercentTotal float64 `json:"gc_cpu_percent_total"`
	GCCyclesPerSec    float64 `json:"gc_cycles_per_s"`
	GCPauseMs         float64 `json:"gc_pause_ms_1s"`

	Runtime gcstats.Gauges `json:"runtime"`
	Store   any            `json:"store,omitempty"`
}

// sample refreshes the /stats snapshot; it runs once per second.
func (s *server) sample() {
	now := gcstats.Read()
	ops := s.c.ops()
	s.mu.Lock()
	defer s.mu.Unlock()
	d := gcstats.Between(s.prevGC, now)
	total := gcstats.Between(s.firstGC, now)
	dt := now.Time.Sub(s.prevTime).Seconds()
	doc := statsDoc{
		Backend:           s.b.Name(),
		Time:              now.Time,
		UptimeSec:         now.Time.Sub(s.start).Seconds(),
		OpsTotal:          ops,
		Gets:              s.c.gets.Load(),
		Hits:              s.c.hits.Load(),
		Misses:            s.c.misses.Load(),
		Sets:              s.c.sets.Load(),
		Deletes:           s.c.deletes.Load(),
		HTTPRequests:      s.c.httpRequests.Load(),
		Entries:           s.b.Len(),
		OffHeapBytes:      s.b.OffHeapBytes(),
		Latency:           s.lat.drain(),
		GCCPUPercent:      100 * d.GCCPUFraction,
		GCCPUPercentTotal: 100 * total.GCCPUFraction,
		GCPauseMs:         float64(d.PauseTotal) / 1e6,
		Runtime:           now.Gauges(),
		Store:             s.b.Details(),
	}
	if dt > 0 {
		doc.OpsPerSec = float64(ops-s.prevOps) / dt
		doc.GCCyclesPerSec = float64(d.GCCycles) / dt
	}
	s.snapshot = doc
	// the history omits the bulky store details
	doc.Store = nil
	s.history = append(s.history, doc)
	if len(s.history) > historyLen {
		s.history = s.history[len(s.history)-historyLen:]
	}
	s.prevGC, s.prevOps, s.prevTime = now, ops, now.Time
}

// runSampler calls sample every interval until stop is closed.
func (s *server) runSampler(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.sample()
		case <-stop:
			return
		}
	}
}
