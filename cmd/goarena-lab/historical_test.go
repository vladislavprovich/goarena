package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeHistResult writes a minimal historical result file.
func writeHistResult(t *testing.T, dir, name string, opts map[string]any, ops float64, goVersion string, withVersion bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := map[string]any{
		"options":          opts,
		"go_version":       goVersion,
		"ops_per_sec":      ops,
		"populate_seconds": 1.5,
		"latency":          map[string]any{"count": 10, "p50_ns": 400, "p99_ns": 1000, "p999_ns": 5000, "max_ns": 100000},
		"gc":               map[string]any{"gc_cycles": 4},
		"gc_cpu_percent":   2.5,
		"after":            map[string]any{"heap_objects_bytes": 1 << 30, "rss_bytes": 2 << 30},
		"offheap_bytes":    0,
	}
	if withVersion {
		r["goarena_version"] = "v0.3.0"
		r["forced_gc_ms"] = 0.5
	}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func e1opts(exp, impl string, n int) map[string]any {
	return map[string]any{"impl": impl, "n": n, "value_size": 128, "goroutines": 16, "read_ratio": 0.9,
		"dist": "uniform", "zipf_s": 1.1, "experiment": exp, "repeat": 1}
}

func TestHistoricalImportAndCompare(t *testing.T) {
	l := newTestLab(t)
	res := l.results
	// v0.1 (old schema: no goarena_version, memlimit, garbage fields)
	for rep, ops := range []float64{20e6, 22e6} {
		writeHistResult(t, filepath.Join(res, "v1", "raw"), fmt.Sprintf("E1_goarena_rep%d.json", rep+1),
			e1opts("E1", "goarena", 10_000_000), ops, "go1.24.6", false)
	}
	writeHistResult(t, filepath.Join(res, "v1", "raw"), "E1_gomap_rep1.json", e1opts("E1", "gomap", 10_000_000), 15e6, "go1.24.6", false)
	// E7 uses the same parameters but gctrace: must not count as E1.
	e7 := e1opts("E7", "goarena", 10_000_000)
	e7["gctrace"] = true
	writeHistResult(t, filepath.Join(res, "v1", "raw"), "E7_goarena_rep1.json", e7, 1e6, "go1.24.6", false)
	// v0.2
	writeHistResult(t, filepath.Join(res, "v2", "raw"), "E1_goarena_rep1.json", e1opts("E1", "goarena", 10_000_000), 30e6, "go1.24.6", false)
	// v0.3: E1 at 10M is called E1r
	writeHistResult(t, filepath.Join(res, "raw"), "E1r_goarena_rep1.json", e1opts("E1r", "goarena", 10_000_000), 32e6, "go1.24.6", true)
	e9 := e1opts("E9", "goarena-paced", 5_000_000)
	e9["garbage_rate"], e9["garbage_bytes"] = 1.0, 1024
	writeHistResult(t, filepath.Join(res, "raw"), "E9_paced_rep1.json", e9, 10e6, "go1.24.6", true)
	// demo: custom label, matching parameters
	writeHistResult(t, filepath.Join(res, "demo", "raw"), "demo_goarena.json", e1opts("demo-baseline", "goarena", 10_000_000), 34e6, "go1.27.0", true)
	// garbage that must be skipped
	if err := os.WriteFile(filepath.Join(res, "raw", "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res, "raw", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, b := l.do("GET", "/api/historical?reload=1", nil)
	var idx HistIndex
	if resp.StatusCode != 200 || json.Unmarshal(b, &idx) != nil {
		t.Fatalf("historical: %d %s", resp.StatusCode, b)
	}
	src := map[string]SourceInfo{}
	for _, s := range idx.Sources {
		src[s.Version] = s
	}
	if src["v0.1"].Files != 4 || src["v0.3"].Files != 2 || src["v0.3"].Errors != 1 || src["v0.4"].Exists || !src["demo"].Exists {
		t.Fatalf("sources %+v", idx.Sources)
	}
	var g1 *HistGroup
	for i, g := range idx.Groups {
		if g.Version == "v0.1" && g.Impl == "goarena" && g.Experiment == "E1" {
			g1 = &idx.Groups[i]
		}
	}
	if g1 == nil || g1.Runs != 2 || g1.OpsPerSec != 21e6 || math.Abs(g1.OpsPerSecStd-math.Sqrt(2e12)) > 1 {
		t.Fatalf("v0.1 goarena group %+v", g1)
	}

	resp, b = l.do("GET", "/api/compare?scenario=e1-10m", nil)
	var c Comparison
	if resp.StatusCode != 200 || json.Unmarshal(b, &c) != nil {
		t.Fatalf("compare: %d %s", resp.StatusCode, b)
	}
	ga := c.Cells["goarena"]
	if ga["v0.1"].OpsPerSec != 21e6 || ga["v0.2"].OpsPerSec != 30e6 || ga["v0.3"].OpsPerSec != 32e6 ||
		ga["demo"].OpsPerSec != 34e6 || ga["v0.4"] != nil {
		t.Fatalf("goarena cells %+v", ga)
	}
	if c.Impls[0] != "gomap" || c.Impls[1] != "goarena" || c.GoVersions["demo"] != "go1.27.0" {
		t.Fatalf("impls %v go %v", c.Impls, c.GoVersions)
	}
	resp, b = l.do("GET", "/api/compare?scenario=e9-5m", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"goarena-paced"`) {
		t.Fatalf("e9 compare %s", b)
	}
	resp, b = l.do("GET", "/api/compare?scenario=custom&n=10000000&reads=0.9", nil)
	if resp.StatusCode != 200 || json.Unmarshal(b, &c) != nil || c.Cells["goarena"]["v0.2"] == nil {
		t.Fatalf("custom compare %d %s", resp.StatusCode, b)
	}
	for _, bad := range []string{"scenario=nope", "scenario=custom", "scenario=custom&n=x", "scenario=custom&n=5&reads=2"} {
		if resp, _ := l.do("GET", "/api/compare?"+bad, nil); resp.StatusCode != 400 {
			t.Fatalf("%s: %d", bad, resp.StatusCode)
		}
	}
	resp, b = l.do("GET", "/api/export/compare.csv?scenario=e1-10m", nil)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if resp.StatusCode != 200 || len(lines) != 6 || !strings.HasPrefix(lines[0], "scenario,version,impl") {
		t.Fatalf("compare csv %d:\n%s", resp.StatusCode, b)
	}
	resp, b = l.do("GET", "/api/export/compare.json?scenario=e1-10m", nil)
	if resp.StatusCode != 200 || !json.Valid(b) {
		t.Fatalf("compare json %d", resp.StatusCode)
	}
}
