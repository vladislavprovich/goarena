package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectKneesThroughputDrop(t *testing.T) {
	pts := []SweepPoint{
		{Impl: "gomap", X: 10e6, OpsPerSec: 19.6e6, P999Ns: 18000, GCCPUPercent: 5},
		{Impl: "gomap", X: 1e6, OpsPerSec: 29e6, P999Ns: 9000, GCCPUPercent: 2},
		{Impl: "gomap", X: 5e6, OpsPerSec: 25.4e6, P999Ns: 10000, GCCPUPercent: 3},
		{Impl: "goarena", X: 1e6, OpsPerSec: 40e6, P999Ns: 3000},
		{Impl: "goarena", X: 5e6, OpsPerSec: 36e6, P999Ns: 3500},
		{Impl: "goarena", X: 10e6, OpsPerSec: 32e6, P999Ns: 4300},
	}
	k := detectKnees("n", pts)
	if len(k) != 1 {
		t.Fatalf("knees %+v", k)
	}
	if k[0].Impl != "gomap" || k[0].Kind != kneeKindDrop || k[0].FromX != 5e6 || k[0].ToX != 10e6 {
		t.Fatalf("knee %+v", k[0])
	}
	if want := "gomap: throughput -23 % from N=5M to N=10M (25.4 -> 19.6 Mops/s)"; k[0].Message != want {
		t.Fatalf("message %q, want %q", k[0].Message, want)
	}
	if math.Abs(k[0].Change+0.2283) > 0.001 {
		t.Fatalf("change %v", k[0].Change)
	}
}

func TestDetectKneesBoundaries(t *testing.T) {
	// Exactly -15 % is not a drop; p99.9 exactly x2 is not a tail rise;
	// GC exactly +10 pp is not a rise.
	pts := []SweepPoint{
		{Impl: "a", X: 1, OpsPerSec: 100, P999Ns: 1000, GCCPUPercent: 1},
		{Impl: "a", X: 2, OpsPerSec: 85, P999Ns: 2000, GCCPUPercent: 11},
	}
	if k := detectKnees("value_size", pts); len(k) != 0 {
		t.Fatalf("boundary values flagged: %+v", k)
	}
	pts[1] = SweepPoint{Impl: "a", X: 2, OpsPerSec: 84, P999Ns: 2001, GCCPUPercent: 11.5}
	k := detectKnees("value_size", pts)
	kinds := map[string]bool{}
	for _, x := range k {
		kinds[x.Kind] = true
	}
	if len(k) != 3 || !kinds[kneeKindDrop] || !kinds[kneeKindTail] || !kinds[kneeKindGC] {
		t.Fatalf("knees %+v", k)
	}
	for _, x := range k {
		if x.Kind == kneeKindGC && !strings.Contains(x.Message, "GC CPU +10.5 pp from value=1 B to value=2 B") {
			t.Fatalf("gc message %q", x.Message)
		}
		if x.Kind == kneeKindTail && !strings.HasPrefix(x.Message, "a: p99.9 x2 from") {
			t.Fatalf("tail message %q", x.Message)
		}
	}
}

func TestDetectKneesGoroutineScaling(t *testing.T) {
	// Linear to 4, then +5 % for 2x goroutines: efficiency 0.525 < 0.5 x 1.0? no;
	// then 8 -> 16 gives +2 % for 2x: efficiency 0.51 < 0.5 x 0.525? no.
	// A flat step after linear steps: 4 -> 8 with +2 %: efficiency 0.51 >= 0.5,
	// so use 3x goroutines to make the loss visible.
	pts := []SweepPoint{
		{Impl: "g", X: 1, OpsPerSec: 10},
		{Impl: "g", X: 2, OpsPerSec: 20},
		{Impl: "g", X: 4, OpsPerSec: 40},
		{Impl: "g", X: 12, OpsPerSec: 42}, // e = 1.05 / 3 = 0.35 < 0.5 x 1.0
		{Impl: "g", X: 16, OpsPerSec: 30}, // -29 %: a drop, not reported twice
	}
	k := detectKnees("goroutines", pts)
	if len(k) != 2 {
		t.Fatalf("knees %+v", k)
	}
	if k[0].Kind != kneeKindScaling || k[0].ToX != 12 || math.Abs(k[0].Change-0.35) > 1e-9 {
		t.Fatalf("scaling knee %+v", k[0])
	}
	if !strings.Contains(k[0].Message, "scaling stalls from G=4 to G=12") {
		t.Fatalf("message %q", k[0].Message)
	}
	if k[1].Kind != kneeKindDrop || k[1].ToX != 16 {
		t.Fatalf("drop knee %+v", k[1])
	}
	// The same numbers for a parameter that is not expected to scale only
	// report the drop.
	if k := detectKnees("n", pts); len(k) != 1 || k[0].Kind != kneeKindDrop {
		t.Fatalf("n knees %+v", k)
	}
	// Scaling compares with the previous step's efficiency.
	pts = []SweepPoint{
		{Impl: "g", X: 1, OpsPerSec: 10},
		{Impl: "g", X: 2, OpsPerSec: 14}, // e = 0.7
		{Impl: "g", X: 4, OpsPerSec: 18}, // e = 0.64: fine
		{Impl: "g", X: 8, OpsPerSec: 19}, // e = 0.53 < ... no (0.5 x 0.64 = 0.32)
	}
	if k := detectKnees("goroutines", pts); len(k) != 0 {
		t.Fatalf("gradual flattening flagged: %+v", k)
	}
	if k := detectKnees("goroutines", nil); k == nil || len(k) != 0 {
		t.Fatal("empty input must give an empty (non-nil) list")
	}
}

func TestParamLabel(t *testing.T) {
	cases := map[string]string{
		paramLabel("n", 2_500_000):      "N=2.5M",
		paramLabel("value_size", 4096):  "value=4 KiB",
		paramLabel("value_size", 64):    "value=64 B",
		paramLabel("goroutines", 12):    "G=12",
		paramLabel("read_ratio", 0.9):   "reads=90 %",
		paramLabel("garbage_rate", 0.5): "garbage=0.5",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestEstimator(t *testing.T) {
	c := RunConfig{N: 10_000_000, ValueSize: 128}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1 }
	// goarena: 10M x (73 + 1.06 x 128) B = 1990 MiB + 50
	if e := estimateMiB(c, "goarena"); !near(e, 10e6*(73+1.06*128)/mib+50) {
		t.Fatalf("goarena %v", e)
	}
	// every Go-heap implementation needs more than goarena at 128 B
	for _, impl := range []string{"gomap", "gomap-gogc400", "syncmap", "gobytes"} {
		if estimateMiB(c, impl) <= estimateMiB(c, "goarena") {
			t.Fatalf("%s below goarena", impl)
		}
	}
	// goarena-evict is bounded by maxmem
	c.MaxMemMiB = 512
	if e := estimateMiB(c, "goarena-evict"); !near(e, 512+50) {
		t.Fatalf("evict %v", e)
	}
	// garbage multiplies the data part
	g := RunConfig{N: 5_000_000, ValueSize: 128, GarbageRate: 1}
	data := 5e6 * (266 + 1.98*128) / mib
	if e := estimateMiB(g, "gomap"); !near(e, data*1.5+50) {
		t.Fatalf("gomap garbage %v", e)
	}
	// a memory limit caps Go-heap implementations at max(1.1 x no-garbage, 1.15 x limit)
	g.MemLimitMiB = 1210
	sync := 5e6 * (430 + 2.0*128) / mib
	if e := estimateMiB(g, "syncmap"); !near(e, math.Max((sync+50)*1.1, 1210*1.15)) {
		t.Fatalf("syncmap with limit %v", e)
	}
	// ... but not goarena (off-heap memory is outside GOMEMLIMIT)
	if e := estimateMiB(g, "goarena"); !near(e, 5e6*(73+1.06*128)/mib*1.05+50) {
		t.Fatalf("goarena with limit %v", e)
	}
}

func TestSafeBudgetAndGuard(t *testing.T) {
	if b := safeBudgetMiB(32 << 30); b != 24576 {
		t.Fatalf("32 GiB: %v", b)
	}
	if b := safeBudgetMiB(16 << 30); b != 10240 { // 16 - 6 GiB < 12 GiB
		t.Fatalf("16 GiB: %v", b)
	}
	if b := safeBudgetMiB(4 << 30); b != 0 {
		t.Fatalf("4 GiB: %v", b)
	}
	if b := safeBudgetMiB(0); b != fallbackBudgetMiB {
		t.Fatalf("unknown RAM: %v", b)
	}
	c := RunConfig{Impls: []string{"goarena", "syncmap"}, N: 50_000_000, ValueSize: 128}
	r := estimatePlan(planRun(c), 32<<30)
	if r.OK || r.Runs[0].OK == false || r.Runs[1].OK || !strings.Contains(r.Message, "syncmap") {
		t.Fatalf("guard %+v", r)
	}
	c.N = 1_000_000
	if r := estimatePlan(planRun(c), 32<<30); !r.OK || !strings.Contains(r.Message, "within") {
		t.Fatalf("small run refused: %+v", r)
	}
}

func TestParseProgressLine(t *testing.T) {
	line := `{"type":"progress","impl":"goarena","phase":"measure","stage":"g=8","stage_index":2,"t_s":7.0,"phase_t_s":3.0,"interval_s":1.0,"goroutines":8,"read_ratio":0.9,"garbage_rate":0,"ops":31000000,"ops_per_sec":3.1e7,"misses":0,"hit_ratio":1,"samples":1900000,"p50_ns":350,"p99_ns":900,"p999_ns":4000,"max_ns":1500000,"gc_cycles":2,"gc_cycles_total":10,"gc_cpu_percent":1.2,"stw_ms":0.35,"heap_bytes":4000000,"heap_goal_bytes":8000000,"rss_bytes":1900000000,"offheap_bytes":1850000000,"populated":10000000,"n":10000000,"brand_new":{"x":1}}`
	p, ok := parseProgressLine([]byte(line + "\r"))
	if !ok || p.Phase != "measure" || p.Stage != "g=8" || p.StageIndex != 2 || p.OpsPerSec != 3.1e7 ||
		p.P999Ns != 4000 || p.StwMs != 0.35 || p.RSSBytes != 1.9e9 || p.Goroutines != 8 || p.HitRatio != 1 {
		t.Fatalf("parsed %+v ok=%v", p, ok)
	}
	// missing fields are tolerated
	if p, ok := parseProgressLine([]byte(`{"type":"progress","phase":"populate","populated":5}`)); !ok || p.Populated != 5 || p.OpsPerSec != 0 {
		t.Fatalf("partial line %+v %v", p, ok)
	}
	for _, s := range []string{
		"goarena        n=1000000   v=128   g=16  ops/s=31000000",
		`{"type":"other","x":"progress"}`,
		`{"type":"progress",`,
		`{"type":"progress","ops":"many"}`,
		"",
		"goarena-bench: unknown implementation",
	} {
		if _, ok := parseProgressLine([]byte(s)); ok {
			t.Errorf("parsed non-progress line %q", s)
		}
	}
}

func TestLineWriter(t *testing.T) {
	var lines []string
	w := newLineWriter(func(b []byte) { lines = append(lines, string(b)) })
	for _, chunk := range []string{"first li", "ne\r\nsecond\n\n  \nthi", "rd"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatal(n, err)
		}
	}
	w.Flush()
	if strings.Join(lines, "|") != "first line|second|third" {
		t.Fatalf("lines %q", lines)
	}
	lines = nil
	big := strings.Repeat("x", maxLineBytes+10)
	_, _ = w.Write([]byte(big))
	if len(lines) != 1 || len(lines[0]) != len(big) {
		t.Fatalf("long line not emitted: %d", len(lines))
	}
}

func TestBenchArgs(t *testing.T) {
	caps := benchCaps{Flags: []string{"impl", "n", "progress", "maxmem", "fill-on-miss", "profile"}, Impls: fakeImpls}
	c := RunConfig{Impls: []string{"goarena", "goarena-evict"}, N: 1000, ValueSize: 64, ReadRatio: 0.5, Goroutines: 2,
		Dist: "zipf", ZipfS: 1.2, DurationS: 1.5, WarmupS: 0, MaxMemMiB: 64, FillOnMiss: true, Profile: "spike", Preset: "spikes"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a, err := c.benchArgs("goarena", "o.json", caps)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(a, " ")
	want := "run -impl goarena -n 1000 -value 64 -goroutines 2 -reads 0.5 -dist zipf -zipf-s 1.2 -duration 1.5s -warmup 0s " +
		"-garbage-rate 0 -garbage-bytes 0 -fill-on-miss -profile spike -progress -exp lab-spikes -out o.json"
	if got != want {
		t.Fatalf("args\n got %s\nwant %s", got, want)
	}
	a, _ = c.benchArgs("goarena-evict", "o.json", caps)
	if !strings.Contains(strings.Join(a, " "), "-maxmem 64") {
		t.Fatalf("evict args %v", a)
	}
	// an old harness: no new flags, no goarena-evict
	old := benchCaps{Flags: []string{"impl", "n"}, Impls: []string{"gomap", "goarena"}}
	if _, err := c.benchArgs("goarena", "o.json", old); err == nil || !strings.Contains(err.Error(), "-fill-on-miss") {
		t.Fatalf("missing flag not detected: %v", err)
	}
	if _, err := c.benchArgs("goarena-evict", "o.json", old); err == nil {
		t.Fatal("unknown impl not detected")
	}
	steady := RunConfig{Impls: []string{"gomap"}, N: 1000, ValueSize: 64, Goroutines: 1, DurationS: 1, MemLimitMiB: 100}
	if err := steady.Validate(); err != nil {
		t.Fatal(err)
	}
	a, err = steady.benchArgs("gomap", "o.json", old)
	if err != nil || strings.Contains(strings.Join(a, " "), "-progress") || !strings.Contains(strings.Join(a, " "), "-memlimit 100") {
		t.Fatalf("old harness args %v %v", a, err)
	}
}

func TestSweepValidate(t *testing.T) {
	sw := SweepConfig{Base: smallRun("gomap"), Param: "value_size", Values: []float64{1024, 64, 256}}
	if err := sw.Validate(); err != nil || sw.Values[0] != 64 || sw.Values[2] != 1024 {
		t.Fatalf("%v %v", err, sw.Values)
	}
	bad := []SweepConfig{
		{Base: smallRun("gomap"), Param: "shards", Values: []float64{1, 2}},
		{Base: smallRun("gomap"), Param: "goroutines", Values: []float64{1}},
		{Base: smallRun("gomap"), Param: "goroutines", Values: []float64{1, 1.5}},
		{Base: smallRun("gomap"), Param: "goroutines", Values: []float64{2, 2}},
		{Base: smallRun("gomap"), Param: "read_ratio", Values: []float64{0.5, 1.5}},
		{Base: smallRun("gomap"), Param: "n", Values: []float64{1000, 1e12}},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	ramp := smallRun("gomap")
	ramp.Profile = "ramp"
	if err := (&SweepConfig{Base: ramp, Param: "n", Values: []float64{1000, 2000}}).Validate(); err == nil {
		t.Error("sweep with a load profile accepted")
	}
	plan := planSweep(SweepConfig{Base: smallRun("gomap", "goarena"), Param: "garbage_rate", Values: []float64{0, 0.5}})
	if len(plan) != 4 || plan[2].Config.GarbageRate != 0.5 || plan[2].Config.GarbageBytes != 1024 || plan[3].Impl != "goarena" {
		t.Fatalf("plan %+v", plan)
	}
}

func TestListenAddrAndRepoRoot(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8090": true, "localhost:0": true, "[::1]:8090": true,
		":8090": false, "0.0.0.0:8090": false, "192.168.1.5:8090": false, "example.com:80": false, "nonsense": false,
	} {
		if err := checkListenAddr(addr, false); (err == nil) != ok {
			t.Errorf("%s: %v", addr, err)
		}
	}
	if err := checkListenAddr("0.0.0.0:8090", true); err != nil {
		t.Errorf("allow-remote: %v", err)
	}
	for host, ok := range map[string]bool{"127.0.0.1:8090": true, "localhost": true, "[::1]:1": true, "LOCALHOST:5": true,
		"evil.example": false, "127.0.0.1.evil.example:80": false} {
		if loopbackHostHeader(host) != ok {
			t.Errorf("host header %q", host)
		}
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("// x\nmodule \"github.com/vladislavprovich/goarena\"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "cmd", "goarena-lab", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findRepoRoot(sub)
	if err != nil || got != root {
		t.Fatalf("findRepoRoot = %q, %v; want %q", got, err, root)
	}
	if _, err := findRepoRoot(t.TempDir()); err == nil {
		// t.TempDir is outside any goarena checkout
		t.Fatal("found a repository root in a temp dir")
	}
}

func TestParseFlags(t *testing.T) {
	f, err := parseFlags([]string{"-addr", "127.0.0.1:18090", "-open"}, os.Stderr)
	if err != nil || f.addr != "127.0.0.1:18090" || !f.open || f.allowRemote {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := parseFlags([]string{"extra"}, os.Stderr); err == nil {
		t.Fatal("extra argument accepted")
	}
	if err := run([]string{"-addr", ":0"}, os.Stderr); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("run with a wildcard address: %v", err)
	}
}

func TestMachineInfo(t *testing.T) {
	m := collectMachineInfo()
	if m.LogicalCPUs < 1 || m.GOOS == "" || m.OS == "" || m.LabGoVersion == "" {
		t.Fatalf("%+v", m)
	}
}

func TestStripTimelineAndSummary(t *testing.T) {
	raw := []byte(`{"options":{"impl":"gomap"},"reads":100,"misses":25,"measured_seconds":2,"gc":{"gc_cycles":10},"timeline":[{"type":"progress"}],"stages":"oops"}`)
	r, err := parseBenchResult(raw)
	if err != nil || len(r.timeline) != 1 || r.stages != nil {
		t.Fatalf("%+v %v", r, err)
	}
	s := summarize(r)
	if s.HitRatio == nil || *s.HitRatio != 0.75 || s.GCCyclesPerSec != 5 {
		t.Fatalf("summary %+v", s)
	}
	if b := stripTimeline(raw); strings.Contains(string(b), "timeline") || !strings.Contains(string(b), `"misses":25`) {
		t.Fatalf("strip %s", b)
	}
	if _, err := parseBenchResult([]byte("{")); err == nil {
		t.Fatal("broken JSON accepted")
	}
}
