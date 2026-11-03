package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testLab struct {
	t       *testing.T
	srv     *Server
	ts      *httptest.Server
	results string
}

// newTestLab starts a lab whose harness is the fake bench (this test
// binary) with the given extra environment.
func newTestLab(t *testing.T, env ...string) *testLab {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bench := newBenchManager(t.TempDir(), exe)
	bench.extraEnv = append([]string{"GOARENA_LAB_FAKE_BENCH=1"}, env...)
	bench.Prepare(context.Background())
	if info := bench.Info(); info.State != benchReady {
		t.Fatalf("fake bench not ready: %+v", info)
	}
	results := t.TempDir()
	srv := newServer(serverConfig{
		Repo: t.TempDir(), ResultsDir: results, Bench: bench,
		Machine: MachineInfo{TotalRAMBytes: 32 << 30, LogicalCPUs: 16, OS: "test"},
		Logger:  log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		srv.Shutdown(10 * time.Second)
		ts.Close()
	})
	return &testLab{t: t, srv: srv, ts: ts, results: results}
}

func (l *testLab) do(method, path string, body any) (*http.Response, []byte) {
	l.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			l.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, l.ts.URL+path, rd)
	if err != nil {
		l.t.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		l.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (l *testLab) waitIdle() {
	l.t.Helper()
	done := make(chan struct{})
	go func() { l.srv.runner.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		l.t.Fatal("session did not finish")
	}
}

func smallRun(impls ...string) RunConfig {
	return RunConfig{Impls: impls, N: 100000, ValueSize: 128, ReadRatio: 0.9, Goroutines: 4, Dist: "uniform",
		DurationS: 1, WarmupS: 0, Profile: "steady"}
}

func TestInfoAndPresets(t *testing.T) {
	l := newTestLab(t)
	resp, b := l.do("GET", "/api/info", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("info: %d %s", resp.StatusCode, b)
	}
	var info struct {
		Implementations []ImplInfo `json:"implementations"`
		Bench           BenchInfo  `json:"bench"`
		BudgetMiB       float64    `json:"budget_mib"`
		Machine         MachineInfo
	}
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Implementations) != 7 || info.Bench.State != benchReady || !info.Bench.Progress || !info.Bench.Evict {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.BudgetMiB != 24576 { // min(0.75 x 32 GiB, 32 - 6 GiB)
		t.Fatalf("budget %v", info.BudgetMiB)
	}

	resp, b = l.do("GET", "/api/presets", nil)
	var ps []Preset
	if resp.StatusCode != 200 || json.Unmarshal(b, &ps) != nil || len(ps) < 11 {
		t.Fatalf("presets: %d %s", resp.StatusCode, b)
	}
	for _, p := range ps {
		var plan []plannedRun
		var err error
		if p.Kind == "sweep" {
			sw := SweepConfig{Base: p.Config, Param: p.SweepParam, Values: p.SweepValues}
			plan, err = planFor(nil, &sw)
		} else {
			c := p.Config
			plan, err = planFor(&c, nil)
		}
		if err != nil {
			t.Fatalf("preset %s invalid: %v", p.ID, err)
		}
		if est := estimatePlan(plan, 32<<30); !est.OK {
			t.Fatalf("preset %s exceeds the budget of a 32 GiB machine: %s", p.ID, est.Message)
		}
	}

	resp, b = l.do("GET", "/", nil)
	if resp.StatusCode != 200 || !bytes.Contains(b, []byte("<html")) {
		t.Fatalf("index: %d", resp.StatusCode)
	}
	for _, f := range []string{"/app.js", "/app.css"} {
		if resp, _ := l.do("GET", f, nil); resp.StatusCode != 200 {
			t.Fatalf("%s: %d", f, resp.StatusCode)
		}
	}
	if resp, _ := l.do("GET", "/api/nope", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown api: %d", resp.StatusCode)
	}
}

func TestEstimateEndpoint(t *testing.T) {
	l := newTestLab(t)
	cfg := smallRun("gomap", "goarena")
	resp, b := l.do("POST", "/api/estimate", map[string]any{"config": cfg})
	var est EstimateReport
	if resp.StatusCode != 200 || json.Unmarshal(b, &est) != nil || !est.Valid || !est.OK || len(est.Runs) != 2 {
		t.Fatalf("estimate: %d %s", resp.StatusCode, b)
	}
	cfg.N = 150_000_000
	cfg.ValueSize = 4096
	_, b = l.do("POST", "/api/estimate", map[string]any{"config": cfg})
	if json.Unmarshal(b, &est) != nil || !est.Valid || est.OK || est.Runs[0].OK {
		t.Fatalf("estimate should exceed the budget: %s", b)
	}
	cfg.Impls = []string{"nope"}
	_, b = l.do("POST", "/api/estimate", map[string]any{"config": cfg})
	if json.Unmarshal(b, &est) != nil || est.Valid || !strings.Contains(est.Error, "unknown implementation") {
		t.Fatalf("invalid config: %s", b)
	}
	sw := SweepConfig{Base: smallRun("gomap"), Param: "n", Values: []float64{2e6, 1e6}}
	_, b = l.do("POST", "/api/estimate", map[string]any{"sweep": sw})
	if json.Unmarshal(b, &est) != nil || !est.Valid || len(est.Runs) != 2 || est.Runs[0].N != 1_000_000 {
		t.Fatalf("sweep estimate: %s", b)
	}
}

func TestRunValidationAndGuards(t *testing.T) {
	l := newTestLab(t)
	cases := []struct {
		name   string
		body   any
		status int
		want   string
	}{
		{"unknown impl", map[string]any{"impls": []string{"rm -rf"}, "n": 1000, "value_size": 128, "goroutines": 1, "duration_s": 1}, 400, "unknown implementation"},
		{"unknown field", map[string]any{"impls": []string{"gomap"}, "n": 1000, "value_size": 128, "goroutines": 1, "duration_s": 1, "cmd": "x"}, 400, "unknown field"},
		{"bad dist", map[string]any{"impls": []string{"gomap"}, "n": 1000, "value_size": 128, "goroutines": 1, "duration_s": 1, "dist": "-rf"}, 400, "distribution"},
		{"zero goroutines", map[string]any{"impls": []string{"gomap"}, "n": 1000, "value_size": 128, "goroutines": 0, "duration_s": 1}, 400, "goroutines"},
		{"evict without maxmem", map[string]any{"impls": []string{"goarena-evict"}, "n": 1000, "value_size": 128, "goroutines": 1, "duration_s": 1}, 400, "max memory"},
		{"memory guard", map[string]any{"impls": []string{"syncmap"}, "n": 150000000, "value_size": 4096, "goroutines": 1, "duration_s": 1}, 400, "memory guard"},
	}
	for _, c := range cases {
		resp, b := l.do("POST", "/api/run", c.body)
		if resp.StatusCode != c.status || !strings.Contains(string(b), c.want) {
			t.Errorf("%s: got %d %s", c.name, resp.StatusCode, b)
		}
	}
	// The memory guard response carries the estimate for the UI.
	_, b := l.do("POST", "/api/run", cases[len(cases)-1].body)
	var ae apiError
	if json.Unmarshal(b, &ae) != nil || ae.Estimate == nil || ae.Estimate.OK {
		t.Errorf("guard response without estimate: %s", b)
	}

	// Request guard: JSON content type, same origin, loopback Host.
	req, _ := http.NewRequest("POST", l.ts.URL+"/api/run", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", l.ts.URL+"/api/cancel", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", l.ts.URL+"/api/info", nil)
	req.Host = "evil.example:8090"
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host: %d", resp.StatusCode)
	}
	if resp, b := l.do("POST", "/api/cancel", map[string]any{}); resp.StatusCode != 200 || !strings.Contains(string(b), "false") {
		t.Errorf("cancel without session: %d %s", resp.StatusCode, b)
	}
}

// sseClient collects events of /api/events.
type sseClient struct {
	events chan Event
	cancel context.CancelFunc
}

func connectSSE(t *testing.T, url string) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	c := &sseClient{events: make(chan Event, 10000), cancel: cancel}
	go func() {
		defer resp.Body.Close()
		defer close(c.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		for sc.Scan() {
			line := sc.Text()
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var ev Event
				var raw struct {
					Seq  int64           `json:"seq"`
					Type string          `json:"type"`
					Data json.RawMessage `json:"data"`
				}
				if json.Unmarshal([]byte(data), &raw) == nil {
					ev.Seq, ev.Type, ev.Data = raw.Seq, raw.Type, raw.Data
					c.events <- ev
				}
			}
		}
	}()
	t.Cleanup(cancel)
	return c
}

// until reads events until one of type typ arrives.
func (c *sseClient) until(t *testing.T, typ string) []Event {
	t.Helper()
	var got []Event
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				t.Fatalf("event stream closed before %s (got %d events)", typ, len(got))
			}
			got = append(got, ev)
			if ev.Type == typ {
				return got
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s (got %d events)", typ, len(got))
		}
	}
}

func countTypes(evs []Event) map[string]int {
	m := map[string]int{}
	for _, e := range evs {
		m[e.Type]++
	}
	return m
}

func TestRunSessionSSEHistoryExport(t *testing.T) {
	l := newTestLab(t)
	sse := connectSSE(t, l.ts.URL)

	resp, b := l.do("POST", "/api/run", smallRun("gomap", "goarena"))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %d %s", resp.StatusCode, b)
	}
	var started struct {
		ID   string `json:"id"`
		Runs int    `json:"runs"`
	}
	if err := json.Unmarshal(b, &started); err != nil || started.Runs != 2 || !historyIDRe.MatchString(started.ID) {
		t.Fatalf("run response %s", b)
	}
	evs := sse.until(t, evSessionFinished)
	n := countTypes(evs)
	if n[evSessionStarted] != 1 || n[evRunStarted] != 2 || n[evRunFinished] != 2 || n[evProgress] != 10 {
		t.Fatalf("event counts %v", n)
	}
	var sawLog, sawSummaryLine bool
	for _, e := range evs {
		if e.Type == evLog {
			sawLog = sawLog || strings.Contains(string(e.Data.(json.RawMessage)), "fake: starting")
			sawSummaryLine = sawSummaryLine || strings.Contains(string(e.Data.(json.RawMessage)), "ops/s=")
		}
		if e.Type == evRunFinished {
			var rf struct {
				State   string      `json:"state"`
				Summary *RunSummary `json:"summary"`
			}
			_ = json.Unmarshal(e.Data.(json.RawMessage), &rf)
			if rf.State != stateDone || rf.Summary == nil || rf.Summary.OpsPerSec <= 0 || rf.Summary.HitRatio == nil {
				t.Fatalf("run_finished %s", e.Data)
			}
		}
	}
	if !sawLog || !sawSummaryLine {
		t.Fatalf("non-progress stderr lines must reach the log (log=%v summary=%v)", sawLog, sawSummaryLine)
	}
	last := evs[len(evs)-1]
	var fin struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Saved bool   `json:"saved"`
	}
	_ = json.Unmarshal(last.Data.(json.RawMessage), &fin)
	if fin.State != stateDone || !fin.Saved || fin.ID != started.ID {
		t.Fatalf("session_finished %s", last.Data)
	}
	l.waitIdle()

	// A tab that connects after the run is replayed the whole session.
	late := connectSSE(t, l.ts.URL)
	replay := late.until(t, evSessionFinished)
	if replay[0].Type != evSessionStarted || countTypes(replay)[evProgress] != 10 {
		t.Fatalf("replay %v", countTypes(replay))
	}

	// One-shot replay (no streaming).
	resp, b = l.do("GET", "/api/events?once=1", nil)
	if resp.StatusCode != 200 || !bytes.Contains(b, []byte(`"session_finished"`)) || !bytes.Contains(b, []byte(`"session_started"`)) {
		t.Fatalf("once replay: %d", resp.StatusCode)
	}

	// Status.
	resp, b = l.do("GET", "/api/status", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"busy": false`) {
		t.Fatalf("status %s", b)
	}

	// History file and list.
	if _, err := os.Stat(filepath.Join(l.results, "lab", started.ID+".json")); err != nil {
		t.Fatalf("history file: %v", err)
	}
	resp, b = l.do("GET", "/api/history", nil)
	var hl struct {
		Sessions []HistoryEntry `json:"sessions"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(b, &hl) != nil || len(hl.Sessions) != 1 || hl.Sessions[0].RunsDone != 2 {
		t.Fatalf("history list %s", b)
	}
	resp, b = l.do("GET", "/api/history/"+started.ID, nil)
	var sess Session
	if resp.StatusCode != 200 || json.Unmarshal(b, &sess) != nil {
		t.Fatalf("history get %d", resp.StatusCode)
	}
	if len(sess.Runs) != 2 || len(sess.Runs[0].Timeline) != 5 || sess.Runs[0].Summary == nil || len(sess.Runs[0].Result) == 0 {
		t.Fatalf("stored session incomplete: runs=%d", len(sess.Runs))
	}
	if bytes.Contains(sess.Runs[0].Result, []byte(`"timeline"`)) {
		t.Fatal("the timeline must be stored once (run record), not in the raw result")
	}
	if !bytes.Contains(sess.Runs[0].Result, []byte(`"latency_buckets"`)) {
		t.Fatal("raw result lost latency_buckets")
	}
	if a := strings.Join(sess.Runs[1].Args, " "); !strings.Contains(a, "-impl goarena") || !strings.Contains(a, "-progress") ||
		strings.Contains(a, "-maxmem") || strings.Contains(a, "-profile") {
		t.Fatalf("args %q", a)
	}

	// Export.
	resp, b = l.do("GET", "/api/export/"+started.ID+".csv", nil)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if resp.StatusCode != 200 || len(lines) != 3 || !strings.HasPrefix(lines[0], "session_id,") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("csv export %d %q", resp.StatusCode, b)
	}
	resp, b = l.do("GET", "/api/export/"+started.ID+".json", nil)
	if resp.StatusCode != 200 || !json.Valid(b) {
		t.Fatalf("json export %d", resp.StatusCode)
	}
	if resp, _ := l.do("GET", "/api/export/"+started.ID+".xml", nil); resp.StatusCode != 400 {
		t.Fatalf("xml export: %d", resp.StatusCode)
	}

	// Delete.
	if resp, _ := l.do("DELETE", "/api/history/"+started.ID, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete %d", resp.StatusCode)
	}
	if resp, _ := l.do("GET", "/api/history/"+started.ID, nil); resp.StatusCode != 404 {
		t.Fatalf("get after delete %d", resp.StatusCode)
	}
	if resp, _ := l.do("DELETE", "/api/history/"+started.ID, nil); resp.StatusCode != 404 {
		t.Fatalf("second delete %d", resp.StatusCode)
	}
	for _, bad := range []string{"..%2f..%2fgo.mod", "20260101-000000_x.json", "abc"} {
		if resp, _ := l.do("GET", "/api/history/"+bad, nil); resp.StatusCode != 400 && resp.StatusCode != 404 {
			t.Fatalf("bad id %q: %d", bad, resp.StatusCode)
		}
	}
}

func TestBusyAndCancel(t *testing.T) {
	l := newTestLab(t, "GOARENA_LAB_FAKE_STEPS=500", "GOARENA_LAB_FAKE_SLEEP_MS=50")
	sse := connectSSE(t, l.ts.URL)
	if resp, b := l.do("POST", "/api/run", smallRun("gomap", "goarena")); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %d %s", resp.StatusCode, b)
	}
	first := sse.until(t, evProgress) // the first process is running
	if resp, _ := l.do("POST", "/api/run", smallRun("gomap")); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second run: %d, want 409", resp.StatusCode)
	}
	sw := SweepConfig{Base: smallRun("gomap"), Param: "goroutines", Values: []float64{1, 2}}
	if resp, _ := l.do("POST", "/api/sweep", sw); resp.StatusCode != http.StatusConflict {
		t.Fatalf("sweep while busy: %d, want 409", resp.StatusCode)
	}
	if resp, _ := l.do("POST", "/api/rebuild", map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("rebuild while busy: %d, want 409", resp.StatusCode)
	}
	start := time.Now()
	resp, b := l.do("POST", "/api/cancel", map[string]any{})
	if resp.StatusCode != 200 || !strings.Contains(string(b), "true") {
		t.Fatalf("cancel: %d %s", resp.StatusCode, b)
	}
	evs := append(first, sse.until(t, evSessionFinished)...)
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("cancel took %v", d)
	}
	l.waitIdle()
	cur, busy := l.srv.runner.Current(false)
	if busy || cur.State != stateCancelled || cur.Runs[0].State != stateCancelled || cur.Runs[1].State != stateSkipped {
		t.Fatalf("after cancel: busy=%v state=%s runs=%s/%s", busy, cur.State, cur.Runs[0].State, cur.Runs[1].State)
	}
	if countTypes(evs)[evRunStarted] != 1 {
		t.Fatalf("the second run must not start after cancel: %v", countTypes(evs))
	}
}

func TestFailedRunAndSweepKnees(t *testing.T) {
	l := newTestLab(t, "GOARENA_LAB_FAKE_FAIL=gomap")
	sse := connectSSE(t, l.ts.URL)
	sw := SweepConfig{Base: smallRun("gomap", "goarena"), Param: "goroutines", Values: []float64{8, 1, 2, 4}}
	resp, b := l.do("POST", "/api/sweep", sw)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sweep: %d %s", resp.StatusCode, b)
	}
	evs := sse.until(t, evSessionFinished)
	if n := countTypes(evs); n[evRunStarted] != 8 || n[evKnees] != 4 {
		t.Fatalf("event counts %v", n)
	}
	l.waitIdle()
	cur, _ := l.srv.runner.Current(true)
	if cur.State != stateDone || !strings.Contains(cur.Error, "4 of 8 runs failed") {
		t.Fatalf("state %s error %q", cur.State, cur.Error)
	}
	// value outer, implementation inner, values sorted
	wantOrder := []string{"1 gomap", "1 goarena", "2 gomap", "2 goarena", "4 gomap", "4 goarena", "8 gomap", "8 goarena"}
	for i, r := range cur.Runs {
		if got := trimFloat(*r.ParamValue, 0) + " " + r.Impl; got != wantOrder[i] {
			t.Fatalf("run %d is %q, want %q", i, got, wantOrder[i])
		}
		if r.Impl == "gomap" && (r.State != stateFailed || !strings.Contains(r.Error, "fake failure")) {
			t.Fatalf("gomap run: %s %q", r.State, r.Error)
		}
	}
	if len(cur.Knees) != 1 || cur.Knees[0].Impl != "goarena" || cur.Knees[0].Kind != kneeKindDrop || cur.Knees[0].ToX != 8 {
		t.Fatalf("knees %+v", cur.Knees)
	}
	if !strings.Contains(cur.Knees[0].Message, "goarena: throughput -50 % from G=4 to G=8") {
		t.Fatalf("message %q", cur.Knees[0].Message)
	}
	// The stored sweep has the knees too.
	b, err := l.srv.history.Read(cur.ID)
	if err != nil || !bytes.Contains(b, []byte(`"knees"`)) {
		t.Fatalf("stored sweep: %v", err)
	}
}

func TestHistoryDirect(t *testing.T) {
	h := newHistory(filepath.Join(t.TempDir(), "lab"))
	if l, err := h.List(); err != nil || len(l) != 0 {
		t.Fatalf("empty list: %v %v", l, err)
	}
	for _, bad := range []string{"../x", "20260101-000000_A", "x", "20260101-000000_a/b"} {
		if err := h.Save(bad, []byte("{}")); err != errBadID {
			t.Fatalf("save %q: %v", bad, err)
		}
	}
	id := "20261103-120000_custom"
	s := &Session{ID: id, Kind: "run", Title: "t", State: stateDone, Config: smallRun("gomap"),
		Runs: []*RunRecord{{Impl: "gomap", State: stateDone}}}
	b, _ := json.Marshal(s)
	if err := h.Save(id, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "20261103-120001_broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := h.List()
	if err != nil || len(l) != 1 || l[0].ID != id || l[0].RunsDone != 1 || l[0].Impls[0] != "gomap" {
		t.Fatalf("list %+v %v", l, err)
	}
	if !h.Exists(id) || h.Delete(id) != nil || h.Exists(id) {
		t.Fatal("exists/delete")
	}
}
