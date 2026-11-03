package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// statusError is an error with an HTTP status.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

var errBusy = &statusError{http.StatusConflict, "a session is already running (cancel it first)"}

const (
	maxTimelinePoints = 7200 // two hours of 1-s progress lines per run
	logTailLines      = 40
)

// Runner executes sessions: every planned run is a separate goarena-bench
// process, started one after the other. Only one session runs at a time.
// All mutable session state is guarded by mu; events that describe it are
// published while mu is held, so their order matches the state changes.
type Runner struct {
	mu      sync.Mutex
	hub     *Hub
	bench   *BenchManager
	history *History
	machine MachineInfo
	logger  *log.Logger

	cur       *Session // running or last finished session
	running   bool
	cancel    context.CancelFunc
	done      chan struct{}
	cancelled bool // cancel requested by the user
	now       func() time.Time
}

func newRunner(hub *Hub, bench *BenchManager, history *History, machine MachineInfo, logger *log.Logger) *Runner {
	return &Runner{hub: hub, bench: bench, history: history, machine: machine, logger: logger, now: time.Now}
}

// startRequest is a validated session request.
type startRequest struct {
	Kind   string
	Title  string
	Slug   string
	Config RunConfig
	Sweep  *SweepSpec
	Plan   []plannedRun
}

// Start begins a session in the background.
func (r *Runner) Start(req startRequest) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil, errBusy
	}
	benchPath, caps, info, err := r.bench.Ready()
	if err != nil {
		return nil, &statusError{http.StatusServiceUnavailable, err.Error()}
	}
	// Check every command line before starting anything.
	for _, p := range req.Plan {
		if _, err := p.Config.benchArgs(p.Impl, "out.json", caps); err != nil {
			return nil, &statusError{http.StatusBadRequest, err.Error()}
		}
	}
	dir, err := os.MkdirTemp("", "goarena-lab-")
	if err != nil {
		return nil, err
	}
	now := r.now()
	s := &Session{
		ID:        r.newID(now, req.Slug),
		LabFormat: labFormatVersion,
		Kind:      req.Kind,
		Preset:    req.Config.Preset,
		Title:     req.Title,
		State:     stateRunning,
		StartedAt: now,
		Config:    req.Config,
		Sweep:     req.Sweep,
		Machine:   r.machine,
		Bench:     SessionBench{Path: benchPath, Cgo: info.Cgo, GoVersion: info.GoVersion},
	}
	for i, p := range req.Plan {
		s.Runs = append(s.Runs, &RunRecord{Index: i, Impl: p.Impl, ParamValue: p.Param, Config: p.Config, State: statePending})
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cur, r.running, r.cancel, r.cancelled = s, true, cancel, false
	r.done = make(chan struct{})
	r.hub.Reset()
	r.hub.Publish(evSessionStarted, s.snapshot(false))
	go r.loop(ctx, s, req.Plan, benchPath, caps, dir, r.done)
	return s.snapshot(false), nil
}

// newID returns a unique session id <yyyymmdd-hhmmss>_<slug>.
func (r *Runner) newID(t time.Time, slug string) string {
	if !slugRe.MatchString(slug) {
		slug = "custom"
	}
	base := t.Format("20060102-150405") + "_" + slug
	id := base
	for n := 2; r.history.Exists(id) || (r.cur != nil && r.cur.ID == id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// Cancel stops the running session; it reports whether one was running.
func (r *Runner) Cancel() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return false
	}
	r.cancelled = true
	r.cancel()
	return true
}

// Shutdown cancels a running session and waits for it to finish.
func (r *Runner) Shutdown(timeout time.Duration) {
	r.mu.Lock()
	running, done := r.running, r.done
	if running {
		r.cancelled = true
		r.cancel()
	}
	r.mu.Unlock()
	if running {
		select {
		case <-done:
		case <-time.After(timeout):
		}
	}
}

// Busy reports whether a session is running.
func (r *Runner) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Current returns a snapshot of the running or last session, or nil.
func (r *Runner) Current(full bool) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return nil, r.running
	}
	return r.cur.snapshot(full), r.running
}

// Wait blocks until the running session (if any) has finished.
func (r *Runner) Wait() {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (r *Runner) loop(ctx context.Context, s *Session, plan []plannedRun, benchPath string, caps benchCaps, dir string, done chan struct{}) {
	defer close(done)
	defer os.RemoveAll(dir)
	env := childEnv(r.bench.ExtraEnv())
	for i, p := range plan {
		if ctx.Err() != nil {
			break
		}
		r.execRun(ctx, s, i, p, benchPath, caps, env, dir)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	ok, failed := 0, 0
	for _, run := range s.Runs {
		switch run.State {
		case statePending, stateRunning:
			run.State = stateSkipped
		case stateDone:
			ok++
		case stateFailed:
			failed++
		}
	}
	switch {
	case r.cancelled:
		s.State = stateCancelled
	case ok == 0:
		s.State = stateFailed
		if s.Error == "" {
			s.Error = "no run produced a result"
		}
	default:
		s.State = stateDone
		if failed > 0 {
			s.Error = fmt.Sprintf("%d of %d runs failed", failed, len(s.Runs))
		}
	}
	s.FinishedAt = r.now()
	if s.Kind == "sweep" && s.Sweep != nil {
		s.Knees = detectKnees(s.Sweep.Param, sweepPoints(s.Runs))
	}
	if s.hasResults() {
		s.Saved = true
		b, err := json.Marshal(s)
		if err == nil {
			err = r.history.Save(s.ID, b)
		}
		if err != nil {
			s.Saved = false
			s.Error = strings.TrimPrefix(s.Error+"; ", "; ") + "history not saved: " + err.Error()
			r.logger.Printf("session %s: %v", s.ID, err)
		}
	}
	r.hub.Publish(evSessionFinished, map[string]any{
		"id": s.ID, "state": s.State, "error": s.Error, "saved": s.Saved,
		"finished_at": s.FinishedAt, "knees": s.Knees,
	})
	r.running, r.cancel = false, nil
}

// runTimeout bounds one harness process.
func runTimeout(c RunConfig) time.Duration {
	return time.Duration((c.DurationS+c.WarmupS)*2*float64(time.Second)) + 15*time.Minute
}

func (r *Runner) execRun(ctx context.Context, s *Session, i int, p plannedRun, benchPath string, caps benchCaps, env []string, dir string) {
	rec := s.Runs[i]
	out := filepath.Join(dir, fmt.Sprintf("run%03d_%s.json", i, p.Impl))
	args, err := p.Config.benchArgs(p.Impl, out, caps)

	r.mu.Lock()
	rec.State, rec.StartedAt, rec.Args = stateRunning, r.now(), args
	r.hub.Publish(evRunStarted, map[string]any{
		"index": i, "total": len(s.Runs), "impl": p.Impl, "param_value": p.Param,
		"args": args, "started_at": rec.StartedAt,
	})
	r.mu.Unlock()

	start := time.Now()
	var res *benchResult
	var raw []byte
	state := stateDone
	errMsg := ""
	if err != nil {
		state, errMsg = stateFailed, err.Error()
	} else {
		rctx, cancel := context.WithTimeout(ctx, runTimeout(p.Config))
		cmd := exec.CommandContext(rctx, benchPath, args...)
		cmd.Dir, cmd.Env = dir, env
		cmd.Cancel = func() error { return killProcessTree(cmd.Process) }
		cmd.WaitDelay = 5 * time.Second
		stdout := newLineWriter(func(line []byte) { r.onLine(s, i, "stdout", line) })
		stderr := newLineWriter(func(line []byte) { r.onLine(s, i, "stderr", line) })
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err = cmd.Run()
		stdout.Flush()
		stderr.Flush()
		timedOut := errors.Is(rctx.Err(), context.DeadlineExceeded)
		cancel()
		switch {
		case ctx.Err() != nil:
			state, errMsg = stateCancelled, "cancelled"
		case timedOut:
			state, errMsg = stateFailed, fmt.Sprintf("timed out after %s", runTimeout(p.Config))
		case err != nil:
			state, errMsg = stateFailed, "goarena-bench: "+err.Error()
		default:
			if raw, err = os.ReadFile(out); err != nil {
				state, errMsg = stateFailed, "no result file: "+err.Error()
			} else if res, err = parseBenchResult(raw); err != nil {
				state, errMsg = stateFailed, err.Error()
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	rec.State, rec.Error, rec.Seconds = state, errMsg, time.Since(start).Seconds()
	if state == stateFailed && len(rec.LogTail) > 0 {
		rec.Error += " | " + strings.Join(lastN(rec.LogTail, 3), " | ")
	}
	if res != nil {
		rec.Summary = summarize(res)
		if rec.Summary.Impl == "" {
			rec.Summary.Impl = p.Impl
		}
		rec.Result = stripTimeline(raw)
		if len(res.timeline) > 0 {
			rec.Timeline = res.timeline
		}
	}
	r.hub.Publish(evRunFinished, map[string]any{
		"index": i, "impl": p.Impl, "param_value": p.Param, "state": rec.State, "error": rec.Error,
		"seconds": rec.Seconds, "summary": rec.Summary,
	})
	if s.Kind == "sweep" && s.Sweep != nil && rec.Summary != nil {
		s.Knees = detectKnees(s.Sweep.Param, sweepPoints(s.Runs))
		r.hub.Publish(evKnees, s.Knees)
	}
}

// onLine handles one output line of the running harness process.
func (r *Runner) onLine(s *Session, i int, stream string, line []byte) {
	rec := s.Runs[i]
	if stream == "stderr" {
		if p, ok := parseProgressLine(line); ok {
			r.mu.Lock()
			if len(rec.Timeline) < maxTimelinePoints {
				rec.Timeline = append(rec.Timeline, p)
			}
			last := p
			rec.Last = &last
			r.hub.Publish(evProgress, map[string]any{"index": i, "impl": rec.Impl, "p": p})
			r.mu.Unlock()
			return
		}
	}
	text := strings.ToValidUTF8(string(line), "?")
	r.mu.Lock()
	rec.LogTail = append(rec.LogTail, text)
	if len(rec.LogTail) > logTailLines {
		rec.LogTail = slicesCloneTail(rec.LogTail, logTailLines)
	}
	r.hub.Publish(evLog, map[string]any{"index": i, "impl": rec.Impl, "stream": stream, "line": text})
	r.mu.Unlock()
}

func lastN(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// slicesCloneTail returns a copy of the last n elements (so that the
// backing array does not grow without bound).
func slicesCloneTail(s []string, n int) []string {
	return append([]string(nil), lastN(s, n)...)
}

// childEnv is the environment of harness processes: the lab's environment
// without the variables that would silently change the measured GC
// behaviour (the harness sets GOGC and the memory limit itself), plus
// extra.
func childEnv(extra []string) []string {
	drop := []string{"GOGC=", "GOMEMLIMIT=", "GODEBUG=", "GOMAXPROCS="}
	var env []string
next:
	for _, kv := range os.Environ() {
		for _, d := range drop {
			if len(kv) >= len(d) && strings.EqualFold(kv[:len(d)], d) {
				continue next
			}
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}
