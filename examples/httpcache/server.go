package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/savokh/goarena/gcstats"
	"github.com/savokh/goarena/internal/latency"
	"github.com/savokh/goarena/internal/tsc"
)

//go:embed dashboard.html
var dashboardHTML []byte

// maxValueSize limits PUT bodies.
const maxValueSize = 1 << 20

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

// server is the HTTP front end of the cache.
type server struct {
	b     Backend
	start time.Time
	c     counters
	lat   latWindow
	pool  sync.Pool // *[]byte read buffers

	mu       sync.RWMutex
	snapshot statsDoc   // refreshed every second by the sampler
	history  []statsDoc // last historyLen snapshots, oldest first
	prevGC   gcstats.Snapshot
	firstGC  gcstats.Snapshot
	prevOps  uint64
	prevTime time.Time
}

func newServer(b Backend) *server {
	s := &server{b: b, start: time.Now()}
	s.pool.New = func() any { b := make([]byte, 0, 4096); return &b }
	s.firstGC = gcstats.Read()
	s.prevGC, s.prevTime = s.firstGC, s.firstGC.Time
	s.sample()
	return s
}

// handler returns the HTTP routes (Go 1.22 method-and-wildcard patterns).
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /kv/{key}", s.handleGet)
	mux.HandleFunc("PUT /kv/{key}", s.handlePut)
	mux.HandleFunc("DELETE /kv/{key}", s.handleDelete)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /stats/history", s.handleHistory)
	mux.HandleFunc("GET /dashboard", s.handleDashboard)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok\n") })
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	return mux
}

// get, set and del are the instrumented store operations shared by the
// HTTP handlers and the self-load generator. Every 8th operation of a
// worker is timed; HTTP requests are always timed.
func (s *server) get(worker int, timed bool, dst, key []byte) ([]byte, bool) {
	var t0 uint64
	if timed {
		t0 = tsc.Ticks()
	}
	v, ok := s.b.GetInto(dst, key)
	if timed {
		s.lat.record(worker, tsc.ToNanos(tsc.Ticks()-t0))
	}
	s.c.gets.Add(1)
	if ok {
		s.c.hits.Add(1)
	} else {
		s.c.misses.Add(1)
	}
	return v, ok
}

func (s *server) set(worker int, timed bool, key, val []byte) error {
	var t0 uint64
	if timed {
		t0 = tsc.Ticks()
	}
	err := s.b.Set(key, val)
	if timed {
		s.lat.record(worker, tsc.ToNanos(tsc.Ticks()-t0))
	}
	s.c.sets.Add(1)
	return err
}

func (s *server) del(key []byte) bool {
	s.c.deletes.Add(1)
	return s.b.Delete(key)
}

func (s *server) handleGet(w http.ResponseWriter, r *http.Request) {
	s.c.httpRequests.Add(1)
	bp := s.pool.Get().(*[]byte)
	defer s.pool.Put(bp)
	v, ok := s.get(int(s.c.httpRequests.Load()), true, *bp, []byte(r.PathValue("key")))
	*bp = v[:0]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(v)
}

func (s *server) handlePut(w http.ResponseWriter, r *http.Request) {
	s.c.httpRequests.Add(1)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxValueSize+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > maxValueSize {
		http.Error(w, "value too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.set(int(s.c.httpRequests.Load()), true, []byte(r.PathValue("key")), body); err != nil {
		http.Error(w, err.Error(), http.StatusInsufficientStorage)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.c.httpRequests.Add(1)
	if !s.del([]byte(r.PathValue("key"))) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	doc := s.snapshot
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := append([]statsDoc(nil), s.history...)
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(h)
}

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
