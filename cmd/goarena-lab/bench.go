package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Bench binary states.
const (
	benchUnknown  = "unknown"
	benchBuilding = "building"
	benchReady    = "ready"
	benchMissing  = "missing"
	benchError    = "error"
)

// benchCaps are the capabilities of a goarena-bench binary, probed from
// "run -h" (flags) and "list" (implementations).
type benchCaps struct {
	Flags []string
	Impls []string
}

func (c benchCaps) has(flag string) bool { return slices.Contains(c.Flags, flag) }

// hasImpl reports whether the binary lists impl. An empty list (old or
// unparsable output) is treated as "unknown" and accepted.
func (c benchCaps) hasImpl(impl string) bool {
	return len(c.Impls) == 0 || slices.Contains(c.Impls, impl)
}

// BenchInfo is the state of the harness binary shown in the UI.
type BenchInfo struct {
	Path       string    `json:"path"`
	Managed    bool      `json:"managed"` // built by the lab
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	BuildLog   string    `json:"build_log,omitempty"`
	ModTime    time.Time `json:"mod_time,omitzero"`
	BuiltAt    time.Time `json:"built_at,omitzero"`
	Cgo        string    `json:"cgo"` // "1", "0" or "" (unknown)
	GoVersion  string    `json:"go_version,omitempty"`
	ModVersion string    `json:"module_version,omitempty"`
	Flags      []string  `json:"flags,omitempty"`
	Impls      []string  `json:"impls,omitempty"`
	Progress   bool      `json:"progress"` // supports -progress (live charts)
	Profiles   bool      `json:"profiles"` // supports -profile
	Evict      bool      `json:"evict"`    // knows goarena-evict and -maxmem
	FillOnMiss bool      `json:"fill_on_miss"`
}

// BenchManager locates, builds and probes the goarena-bench binary.
type BenchManager struct {
	repo    string
	path    string
	managed bool
	goTool  string

	mu       sync.Mutex
	info     BenchInfo
	caps     benchCaps
	extraEnv []string // extra environment of harness processes (tests)
}

// newBenchManager uses the binary at path, or - if path is empty - manages
// <repo>/bin/goarena-bench(.exe), building it when missing or stale.
func newBenchManager(repo, path string) *BenchManager {
	b := &BenchManager{repo: repo, goTool: "go"}
	if path == "" {
		name := "goarena-bench"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		b.path, b.managed = filepath.Join(repo, "bin", name), true
	} else {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		b.path = path
	}
	b.info = BenchInfo{Path: b.path, Managed: b.managed, State: benchUnknown}
	return b
}

// ExtraEnv returns the extra environment of harness processes.
func (b *BenchManager) ExtraEnv() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.extraEnv)
}

// Info returns the current state.
func (b *BenchManager) Info() BenchInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := b.info
	i.Flags, i.Impls = slices.Clone(i.Flags), slices.Clone(i.Impls)
	return i
}

// Ready returns the binary and its capabilities if it can be used.
func (b *BenchManager) Ready() (string, benchCaps, BenchInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.info.State {
	case benchReady:
		return b.path, b.caps, b.info, nil
	case benchBuilding, benchUnknown:
		return "", benchCaps{}, b.info, errors.New("the goarena-bench binary is being prepared, try again in a few seconds")
	}
	msg := "the goarena-bench binary is not available"
	if b.info.Error != "" {
		msg += ": " + b.info.Error
	}
	return "", benchCaps{}, b.info, errors.New(msg)
}

// Prepare builds the binary if it is managed and missing or stale, then
// probes it. It is called once at startup.
func (b *BenchManager) Prepare(ctx context.Context) {
	if b.managed {
		st, err := os.Stat(b.path)
		if err != nil || newestSource(b.repo).After(st.ModTime()) {
			b.build(ctx)
			return
		}
	}
	b.probe(ctx)
}

// Rebuild starts rebuilding a managed binary (or re-probing an external
// one) in the background; done, if not nil, is closed when it finishes.
func (b *BenchManager) Rebuild(ctx context.Context, done chan<- struct{}) error {
	b.mu.Lock()
	if b.info.State == benchBuilding {
		b.mu.Unlock()
		return errors.New("a build is already in progress")
	}
	b.info.State = benchBuilding
	b.mu.Unlock()
	go func() {
		if done != nil {
			defer close(done)
		}
		if b.managed {
			b.build(ctx)
		} else {
			b.probe(ctx)
		}
	}()
	return nil
}

// build runs "go build -o <path> ./cmd/goarena-bench" in the repository
// with the lab's environment (CGO_ENABLED and CC as set by the user).
func (b *BenchManager) build(ctx context.Context) {
	b.mu.Lock()
	b.info.State, b.info.Error, b.info.BuildLog = benchBuilding, "", ""
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var out bytes.Buffer
	err := os.MkdirAll(filepath.Dir(b.path), 0o755)
	if err == nil {
		cmd := exec.CommandContext(ctx, b.goTool, "build", "-o", b.path, "./cmd/goarena-bench")
		cmd.Dir = b.repo
		cmd.Stdout, cmd.Stderr = &out, &out
		err = cmd.Run()
	}
	b.mu.Lock()
	b.info.BuildLog = strings.TrimSpace(out.String())
	if err != nil {
		b.info.State, b.info.Error = benchError, "go build failed: "+err.Error()
		b.mu.Unlock()
		return
	}
	b.info.BuiltAt = time.Now()
	b.mu.Unlock()
	b.probe(ctx)
}

var (
	flagLineRe = regexp.MustCompile(`(?m)^\s+-([a-z][a-z0-9-]*)`)
	implLineRe = regexp.MustCompile(`^\s{2}([a-z][a-z0-9-]*)\s`)
)

// probe inspects the binary: build settings, supported flags and listed
// implementations.
func (b *BenchManager) probe(ctx context.Context) {
	st, err := os.Stat(b.path)
	if err != nil {
		b.mu.Lock()
		b.info.State, b.info.Error = benchMissing, "not found: "+b.path
		b.mu.Unlock()
		return
	}
	info := BenchInfo{Path: b.path, Managed: b.managed, ModTime: st.ModTime()}
	if bi, err := buildinfo.ReadFile(b.path); err == nil {
		info.GoVersion = bi.GoVersion
		info.ModVersion = bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "CGO_ENABLED" {
				info.Cgo = s.Value
			}
		}
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	env := childEnv(b.ExtraEnv())
	help := runCapture(pctx, b.path, env, "run", "-h")
	var caps benchCaps
	for _, m := range flagLineRe.FindAllStringSubmatch(help, -1) {
		if !slices.Contains(caps.Flags, m[1]) {
			caps.Flags = append(caps.Flags, m[1])
		}
	}
	list := runCapture(pctx, b.path, env, "list")
	inImpls := false
	for _, line := range strings.Split(list, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "implementations:"):
			inImpls = true
			continue
		case strings.HasSuffix(t, ":") && !strings.HasPrefix(line, " "):
			inImpls = false
		}
		if m := implLineRe.FindStringSubmatch(line); inImpls && m != nil {
			caps.Impls = append(caps.Impls, m[1])
		}
	}
	info.Flags, info.Impls = caps.Flags, caps.Impls
	info.Progress = caps.has("progress")
	info.Profiles = caps.has("profile")
	info.FillOnMiss = caps.has("fill-on-miss")
	info.Evict = caps.has("maxmem") && slices.Contains(caps.Impls, "goarena-evict")
	b.mu.Lock()
	defer b.mu.Unlock()
	info.BuiltAt, info.BuildLog = b.info.BuiltAt, b.info.BuildLog
	if len(caps.Flags) == 0 {
		info.State, info.Error = benchError, "the binary did not print the flags of \"run -h\" - is it goarena-bench?"
	} else {
		info.State = benchReady
	}
	b.info, b.caps = info, caps
}

// runCapture runs a short command and returns its combined output; the exit
// status is ignored ("run -h" exits with status 1 or 2).
func runCapture(ctx context.Context, path string, env []string, args ...string) string {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	return out.String()
}

// newestSource returns the newest modification time of the Go sources the
// harness is built from (every non-test .go file of the module outside
// results, bin, examples and the lab itself, plus go.mod).
func newestSource(repo string) time.Time {
	var newest time.Time
	skip := map[string]bool{".git": true, "bin": true, "results": true, "examples": true, "testdata": true, "docs": true}
	lab := filepath.Join(repo, "cmd", "goarena-lab")
	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != repo && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".") || path == lab) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")) || name == "go.mod" ||
			strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".s") {
			if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		return nil
	})
	return newest
}

// ToolchainInfo describes the Go toolchain found in PATH.
type ToolchainInfo struct {
	GoVersion  string `json:"go_version"`  // output of "go version"
	CgoEnabled string `json:"cgo_enabled"` // go env CGO_ENABLED
	CC         string `json:"cc,omitempty"`
	Error      string `json:"error,omitempty"`
}

// toolchainInfo runs "go version" and "go env".
func toolchainInfo(ctx context.Context, goTool string) ToolchainInfo {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var t ToolchainInfo
	out, err := exec.CommandContext(ctx, goTool, "version").Output()
	if err != nil {
		t.Error = "go toolchain not available: " + err.Error()
		return t
	}
	t.GoVersion = strings.TrimSpace(string(out))
	if out, err := exec.CommandContext(ctx, goTool, "env", "CGO_ENABLED", "CC").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			t.CgoEnabled = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 {
			t.CC = strings.TrimSpace(lines[1])
		}
	}
	return t
}
