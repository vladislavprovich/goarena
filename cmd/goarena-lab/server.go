package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFiles embed.FS

// serverConfig configures a Server.
type serverConfig struct {
	Repo        string
	ResultsDir  string
	AllowRemote bool
	Bench       *BenchManager
	Machine     MachineInfo
	Logger      *log.Logger
}

// Server is the lab's HTTP server: static UI plus the JSON/SSE API.
type Server struct {
	cfg        serverConfig
	hub        *Hub
	runner     *Runner
	history    *History
	historical *Historical
	static     fs.FS
	mux        *http.ServeMux
	startedAt  time.Time

	mu        sync.Mutex
	toolchain ToolchainInfo
	ctx       context.Context // canceled on shutdown
	stop      context.CancelFunc
}

func newServer(cfg serverConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(log.Writer(), "goarena-lab: ", log.Ltime)
	}
	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err) // the embedded directory always exists
	}
	hub := newHub()
	history := newHistory(filepath.Join(cfg.ResultsDir, "lab"))
	s := &Server{
		cfg:        cfg,
		hub:        hub,
		history:    history,
		historical: newHistorical(cfg.ResultsDir),
		runner:     newRunner(hub, cfg.Bench, history, cfg.Machine, cfg.Logger),
		static:     static,
		mux:        http.NewServeMux(),
		startedAt:  time.Now(),
	}
	s.ctx, s.stop = context.WithCancel(context.Background())
	s.routes()
	return s
}

// prepare collects toolchain information and builds/probes the harness.
func (s *Server) prepare(ctx context.Context) {
	t := toolchainInfo(ctx, s.cfg.Bench.goTool)
	s.mu.Lock()
	s.toolchain = t
	s.mu.Unlock()
	s.cfg.Bench.Prepare(ctx)
	s.hub.Publish(evBench, s.cfg.Bench.Info())
	if info := s.cfg.Bench.Info(); info.State != benchReady {
		s.cfg.Logger.Printf("goarena-bench: %s %s", info.State, info.Error)
	}
}

// Shutdown cancels a running session (killing its process), waits for it
// and disconnects the event streams.
func (s *Server) Shutdown(timeout time.Duration) {
	s.stop()
	s.runner.Shutdown(timeout)
	s.hub.Close()
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/info", s.handleInfo)
	m.HandleFunc("GET /api/presets", s.handlePresets)
	m.HandleFunc("POST /api/estimate", s.handleEstimate)
	m.HandleFunc("POST /api/run", s.handleRun)
	m.HandleFunc("POST /api/sweep", s.handleSweep)
	m.HandleFunc("POST /api/cancel", s.handleCancel)
	m.HandleFunc("POST /api/rebuild", s.handleRebuild)
	m.HandleFunc("GET /api/status", s.handleStatus)
	m.HandleFunc("GET /api/events", s.handleEvents)
	m.HandleFunc("GET /api/history", s.handleHistoryList)
	m.HandleFunc("GET /api/history/{id}", s.handleHistoryGet)
	m.HandleFunc("DELETE /api/history/{id}", s.handleHistoryDelete)
	m.HandleFunc("GET /api/historical", s.handleHistorical)
	m.HandleFunc("GET /api/compare", s.handleCompare)
	m.HandleFunc("GET /api/export/{file}", s.handleExport)
	m.HandleFunc("GET /api/doc/{name}", s.handleDoc)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "unknown API endpoint "+r.Method+" "+r.URL.Path)
	})
	files := http.FileServerFS(s.static)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

// Handler returns the HTTP handler with the request guard.
func (s *Server) Handler() http.Handler { return s.guard(s.mux) }

// guard protects the API of a process-starting local server: unless
// -allow-remote is set, the Host header must name a loopback host (against
// DNS rebinding); state-changing requests must not come from a foreign
// Origin (CSRF), and POST bodies must be JSON (which makes cross-site
// browser requests subject to a CORS preflight that the lab never grants).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.AllowRemote && !loopbackHostHeader(r.Host) {
			writeError(w, http.StatusForbidden, "the lab only answers requests to a loopback host")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "cross-origin request refused")
					return
				}
			}
			if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "POST requests must have Content-Type: application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHostHeader reports whether a Host header names a loopback host.
func loopbackHostHeader(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	return isLoopbackHost(strings.ToLower(h))
}
