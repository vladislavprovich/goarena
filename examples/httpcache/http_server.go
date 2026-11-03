package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/internal/tsc"
)

//go:embed web/dashboard.html
var dashboardHTML []byte

// maxValueSize limits PUT bodies.
const maxValueSize = 1 << 20

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
