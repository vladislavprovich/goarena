package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// decodeJSON decodes a size-limited JSON body, rejecting unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %v", err)
	}
	if dec.More() {
		return errors.New("invalid JSON body: trailing data")
	}
	return nil
}

// docLink is a document served by /api/doc.
type docLink struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (s *Server) docs() []docLink {
	var out []docLink
	add := func(name, title string) {
		if _, err := os.Stat(s.docPath(name)); err == nil {
			out = append(out, docLink{Name: name, Title: title, URL: "/api/doc/" + name})
		}
	}
	add("BENCHMARK_LAB.md", "Benchmark Lab guide")
	add("BENCHMARKS.md", "Benchmarks and method")
	add("README.md", "Project README")
	add("GUIDE.md", "User guide (GC pacing, GOMEMLIMIT)")
	add("DESIGN.md", "Design")
	add("SUMMARY.md", "Results summary (v0.3)")
	return out
}

var docNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.md$`)

// docPath maps a document name to a file: README.md and CHANGELOG.md in
// the repository root, SUMMARY.md in the results directory, everything
// else in docs/.
func (s *Server) docPath(name string) string {
	switch name {
	case "README.md", "CHANGELOG.md":
		return filepath.Join(s.cfg.Repo, name)
	case "SUMMARY.md":
		return filepath.Join(s.cfg.ResultsDir, name)
	}
	return filepath.Join(s.cfg.Repo, "docs", name)
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !docNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid document name")
		return
	}
	b, err := os.ReadFile(s.docPath(name))
	if err != nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tc := s.toolchain
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"lab": map[string]any{
			"go_version": runtime.Version(), "started_at": s.startedAt, "format": labFormatVersion,
		},
		"machine":         s.cfg.Machine,
		"toolchain":       tc,
		"bench":           s.cfg.Bench.Info(),
		"repo":            s.cfg.Repo,
		"results_dir":     s.cfg.ResultsDir,
		"history_dir":     s.history.dir,
		"implementations": implCatalog,
		"sweep_params":    sweepParams,
		"profiles":        profiles,
		"budget_mib":      safeBudgetMiB(s.cfg.Machine.TotalRAMBytes),
		"docs":            s.docs(),
		"limits": map[string]any{
			"n": []int{minN, maxN}, "value_size": []int{minValue, maxValue}, "goroutines": []int{1, maxGoroutines},
			"duration_s": []float64{minDurationS, maxDurationS}, "warmup_s": []float64{0, maxWarmupS},
			"maxmem_mib_min": minMaxMemMiB, "sweep_values_max": maxSweepValues,
		},
	})
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, presets())
}

// estimateRequest is the body of /api/estimate: a run or a sweep config.
type estimateRequest struct {
	Config *RunConfig   `json:"config"`
	Sweep  *SweepConfig `json:"sweep"`
}

// plan validates a run or sweep configuration and returns its runs.
func planFor(cfg *RunConfig, sw *SweepConfig) ([]plannedRun, error) {
	switch {
	case sw != nil:
		if err := sw.Validate(); err != nil {
			return nil, err
		}
		return planSweep(*sw), nil
	case cfg != nil:
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		return planRun(*cfg), nil
	}
	return nil, errors.New("config or sweep required")
}

func (s *Server) handleEstimate(w http.ResponseWriter, r *http.Request) {
	var req estimateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := planFor(req.Config, req.Sweep)
	if err != nil {
		writeJSON(w, http.StatusOK, &EstimateReport{Valid: false, Error: err.Error(),
			BudgetMiB: safeBudgetMiB(s.cfg.Machine.TotalRAMBytes), Runs: []EstimateRun{}})
		return
	}
	writeJSON(w, http.StatusOK, estimatePlan(plan, s.cfg.Machine.TotalRAMBytes))
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var cfg RunConfig
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := planFor(&cfg, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title, slug := "Custom run", "custom"
	if p, ok := presetByID(cfg.Preset); ok && p.Kind == "run" {
		title, slug = p.Name, p.ID
	}
	s.start(w, startRequest{Kind: "run", Title: title, Slug: slug, Config: cfg, Plan: plan})
}

func (s *Server) handleSweep(w http.ResponseWriter, r *http.Request) {
	var sw SweepConfig
	if err := decodeJSON(w, r, &sw); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := planFor(nil, &sw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sp, _ := sweepParamByName(sw.Param)
	title, slug := "Sweep: "+sp.Label, "sweep-"+strings.ReplaceAll(sw.Param, "_", "-")
	if p, ok := presetByID(sw.Base.Preset); ok && p.Kind == "sweep" && p.SweepParam == sw.Param {
		title, slug = p.Name, p.ID
	}
	s.start(w, startRequest{Kind: "sweep", Title: title, Slug: slug, Config: sw.Base,
		Sweep: &SweepSpec{Param: sw.Param, Values: sw.Values}, Plan: plan})
}

// start applies the memory guard and starts the session.
func (s *Server) start(w http.ResponseWriter, req startRequest) {
	est := estimatePlan(req.Plan, s.cfg.Machine.TotalRAMBytes)
	if !est.OK {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "memory guard: " + est.Message, Estimate: est})
		return
	}
	sess, err := s.runner.Start(req)
	if err != nil {
		status := http.StatusInternalServerError
		var se *statusError
		if errors.As(err, &se) {
			status = se.status
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": sess.ID, "runs": len(sess.Runs), "estimate": est, "session": sess})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": s.runner.Cancel()})
}

func (s *Server) handleRebuild(w http.ResponseWriter, r *http.Request) {
	if s.runner.Busy() {
		writeError(w, http.StatusConflict, "cannot rebuild the harness while a session is running")
		return
	}
	done := make(chan struct{})
	if err := s.cfg.Bench.Rebuild(s.ctx, done); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	go func() {
		<-done
		s.hub.Publish(evBench, s.cfg.Bench.Info())
	}()
	writeJSON(w, http.StatusAccepted, s.cfg.Bench.Info())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	sess, busy := s.runner.Current(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"busy": busy, "session": sess, "bench": s.cfg.Bench.Info(),
		"subscribers": s.hub.Subscribers(), "time": time.Now(),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	var after int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("last"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	replay, sub := s.hub.Subscribe(after)
	defer s.hub.Unsubscribe(sub)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	var buf bytes.Buffer
	buf.WriteString("retry: 2000\n\n")
	for _, ev := range replay {
		writeSSE(&buf, ev)
		if buf.Len() > 64<<10 {
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			buf.Reset()
		}
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return
	}
	_ = rc.Flush()
	if r.URL.Query().Get("once") == "1" {
		return // replay only (static snapshots, e.g. headless screenshots)
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev, ok := <-sub.ch:
			if !ok {
				return
			}
			buf.Reset()
			writeSSE(&buf, ev)
			// Coalesce what is already queued into one write.
			for more := true; more && buf.Len() < 64<<10; {
				select {
				case ev2, ok2 := <-sub.ch:
					if !ok2 {
						more = false
						break
					}
					writeSSE(&buf, ev2)
				default:
					more = false
				}
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func writeSSE(buf *bytes.Buffer, ev encodedEvent) {
	fmt.Fprintf(buf, "id: %d\ndata: ", ev.seq)
	buf.Write(ev.data)
	buf.WriteString("\n\n")
}

func (s *Server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	list, err := s.history.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cur, busy := s.runner.Current(false)
	var current any
	if cur != nil && (busy || !cur.Saved) {
		current = cur
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list, "current": current, "dir": s.history.dir})
}

// loadSession returns a session by id: the running (or unsaved) session
// from memory, otherwise the history file.
func (s *Server) loadSession(id string) (*Session, []byte, error) {
	if cur, busy := s.runner.Current(true); cur != nil && cur.ID == id && (busy || !cur.Saved) {
		return cur, nil, nil
	}
	b, err := s.history.Read(id)
	if err != nil {
		return nil, nil, err
	}
	var sess Session
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, nil, fmt.Errorf("corrupt history file: %v", err)
	}
	return &sess, b, nil
}

func sessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadID):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "no such session")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleHistoryGet(w http.ResponseWriter, r *http.Request) {
	sess, raw, err := s.loadSession(r.PathValue("id"))
	if err != nil {
		sessionError(w, err)
		return
	}
	if raw != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(raw)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleHistoryDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if cur, busy := s.runner.Current(false); busy && cur != nil && cur.ID == id {
		writeError(w, http.StatusConflict, "the session is running")
		return
	}
	if err := s.history.Delete(id); err != nil {
		sessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHistorical(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.historical.Index(r.URL.Query().Get("reload") == "1"))
}

// comparisonFor resolves the scenario of a /api/compare or export request:
// a predefined scenario id, or scenario=custom with explicit parameters.
func (s *Server) comparisonFor(r *http.Request) (*Comparison, error) {
	q := r.URL.Query()
	id := q.Get("scenario")
	if id == "" {
		id = "e1-10m"
	}
	sc, ok := scenarioByID(id)
	if !ok && id != "custom" {
		return nil, fmt.Errorf("unknown scenario %q", id)
	}
	if id == "custom" {
		sc = Scenario{ID: "custom", Name: "Custom scenario", Dist: "uniform", ValueSize: 128, Goroutines: 16, ReadRatio: 0.9}
		ints := map[string]*int{"n": &sc.N, "value": &sc.ValueSize, "goroutines": &sc.Goroutines, "memlimit": &sc.MemLimitMiB}
		for k, p := range ints {
			if v := q.Get(k); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < -1 || n > maxN {
					return nil, fmt.Errorf("invalid %s", k)
				}
				*p = n
			}
		}
		floats := map[string]*float64{"reads": &sc.ReadRatio, "garbage": &sc.GarbageRate}
		for k, p := range floats {
			if v := q.Get(k); v != "" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil || f < 0 || f > 1 {
					return nil, fmt.Errorf("invalid %s", k)
				}
				*p = f
			}
		}
		if d := q.Get("dist"); d != "" {
			if d != "uniform" && d != "zipf" {
				return nil, errors.New("invalid dist")
			}
			sc.Dist = d
		}
		if sc.N <= 0 {
			return nil, errors.New("custom scenario needs n")
		}
	}
	return s.historical.Index(false).compare(sc), nil
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	c, err := s.comparisonFor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	id, ext := file, ""
	for _, e := range []string{".json", ".csv"} {
		if base, ok := strings.CutSuffix(file, e); ok {
			id, ext = base, e
		}
	}
	if ext == "" {
		writeError(w, http.StatusBadRequest, "export as <id>.json or <id>.csv")
		return
	}
	attach := func(name, ctype string) {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
	}
	if id == "compare" {
		c, err := s.comparisonFor(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		name := "goarena_compare_" + c.Scenario.ID + ext
		if ext == ".json" {
			b, err := marshalComparison(c)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			attach(name, "application/json; charset=utf-8")
			_, _ = w.Write(b)
			return
		}
		attach(name, "text/csv; charset=utf-8")
		_ = writeComparisonCSV(w, c)
		return
	}
	sess, raw, err := s.loadSession(id)
	if err != nil {
		sessionError(w, err)
		return
	}
	if ext == ".json" {
		if raw == nil {
			if raw, err = json.Marshal(sess); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		attach("goarena_lab_"+id+".json", "application/json; charset=utf-8")
		_, _ = w.Write(raw)
		return
	}
	attach("goarena_lab_"+id+".csv", "text/csv; charset=utf-8")
	_ = writeSessionCSV(w, sess)
}
