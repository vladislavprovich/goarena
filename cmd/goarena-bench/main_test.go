package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestAppendKey(t *testing.T) {
	seen := map[string]bool{}
	var k []byte
	for i := uint64(0); i < 10000; i++ {
		k = appendKey(k, i)
		if len(k) != keyLen || !bytes.HasPrefix(k, []byte("key:")) {
			t.Fatalf("bad key %q", k)
		}
		if seen[string(k)] {
			t.Fatalf("duplicate key for %d", i)
		}
		seen[string(k)] = true
	}
}

func TestKeyChooser(t *testing.T) {
	for _, dist := range []string{"uniform", "zipf"} {
		o := options{N: 1000, Dist: dist, ZipfS: 1.1, Seed: 3}
		kc := newKeyChooser(&o, 1)
		hits := map[uint64]int{}
		for i := 0; i < 100000; i++ {
			k := kc.next()
			if k >= 1000 {
				t.Fatalf("%s: key %d out of range", dist, k)
			}
			hits[k]++
		}
		if dist == "uniform" && len(hits) < 990 {
			t.Fatalf("uniform covers only %d keys", len(hits))
		}
		if dist == "zipf" {
			max := 0
			for _, c := range hits {
				if c > max {
					max = c
				}
			}
			if max < 5000 {
				t.Fatalf("zipf not skewed: max hits %d", max)
			}
		}
	}
}

// restoreGC undoes the GC settings that some implementations apply to the
// (test) process.
func restoreGC() {
	debug.SetGCPercent(100)
	debug.SetMemoryLimit(math.MaxInt64)
}

func TestImplementations(t *testing.T) {
	defer restoreGC()
	if _, err := impls["goarena-budget"].New(options{Shards: 4, ChunkMiB: 1}); err == nil {
		t.Fatal("goarena-budget without a memory limit must fail")
	}
	for _, name := range implNames() {
		t.Run(name, func(t *testing.T) {
			o := options{Shards: 16, ChunkMiB: 1, MemLimitMiB: 4096}
			c, err := impls[name].New(o)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var k []byte
			val := make([]byte, 64)
			for i := uint64(0); i < 5000; i++ {
				k = appendKey(k, i)
				fillValue(val, i, 0)
				if err := c.Set(k, val); err != nil {
					t.Fatal(err)
				}
			}
			buf := make([]byte, 0, 64)
			for i := uint64(0); i < 5000; i++ {
				k = appendKey(k, i)
				fillValue(val, i, 0)
				var ok bool
				buf, ok = c.Get(buf, k)
				if !ok || !bytes.Equal(buf, val) {
					t.Fatalf("Get(%d) mismatch", i)
				}
			}
			// overwrite with same and different sizes
			k = appendKey(k, 7)
			_ = c.Set(k, []byte("12345678"))
			_ = c.Set(k, bytes.Repeat([]byte("x"), 300))
			if buf, ok := c.Get(buf, k); !ok || len(buf) != 300 {
				t.Fatalf("overwrite: %d %v", len(buf), ok)
			}
			if _, ok := c.Get(buf, []byte("absent")); ok {
				t.Fatal("found absent key")
			}
			if strings.HasPrefix(name, "goarena") && c.OffHeapBytes() <= 0 {
				t.Fatal("goarena must report off-heap bytes")
			}
			if !strings.HasPrefix(name, "goarena") && c.OffHeapBytes() != 0 {
				t.Fatal("Go implementations hold no off-heap memory")
			}
		})
	}
	g := newGoBytes(1)
	if err := g.Set([]byte("k"), make([]byte, goBytesChunk)); err == nil {
		t.Fatal("gobytes must reject entries larger than a chunk")
	}
}

func TestValidate(t *testing.T) {
	good := options{Impl: "goarena", N: 10, ValueSize: 8, Goroutines: 1, ReadRatio: 0.5,
		Dist: "uniform", SampleEvery: 16, Shards: 4, ChunkMiB: 1}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(o *options){
		func(o *options) { o.Impl = "nope" },
		func(o *options) { o.N = 0 },
		func(o *options) { o.ValueSize = 4 },
		func(o *options) { o.Goroutines = 0 },
		func(o *options) { o.ReadRatio = 2 },
		func(o *options) { o.Dist = "normal" },
		func(o *options) { o.SampleEvery = 3 },
		func(o *options) { o.Shards = 6 },
		func(o *options) { o.ChunkMiB = 0 },
	}
	for i, m := range mutations {
		o := good
		m(&o)
		if o.validate() == nil {
			t.Errorf("mutation %d accepted", i)
		}
	}
}

func TestRunAndAggregate(t *testing.T) {
	defer restoreGC()
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, impl := range implNames() {
		for rep := 1; rep <= 2; rep++ {
			o := options{Impl: impl, N: 20000, ValueSize: 64, Goroutines: 4, ReadRatio: 0.9,
				Dist: "uniform", ZipfS: 1.1, Duration: 60 * time.Millisecond, Warmup: 20 * time.Millisecond,
				SampleEvery: 4, Shards: 16, ChunkMiB: 1, Seed: int64(rep), Experiment: "T", Repeat: rep,
				MemLimitMiB: 4096, GarbageRate: 0.5, GarbageBytes: 256}
			out := filepath.Join(raw, runFileName(o))
			if err := cmdRun(runArgs(o, out)[1:], os.Stdout); err != nil {
				t.Fatalf("%s: %v", impl, err)
			}
		}
	}
	// a zipf run printed to a writer instead of a file
	var buf bytes.Buffer
	if err := cmdRun([]string{"-impl", "goarena", "-n", "5000", "-dist", "zipf", "-duration", "30ms",
		"-warmup", "0s", "-shards", "4", "-chunk", "1"}, &buf); err != nil {
		t.Fatal(err)
	}
	var r Result
	if err := json.Unmarshal(buf.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Ops == 0 || r.Misses != 0 || r.Latency.Count == 0 || r.Store == nil || len(r.LatencyBuckets) == 0 {
		t.Fatalf("implausible result: ops=%d misses=%d samples=%d", r.Ops, r.Misses, r.Latency.Count)
	}
	if err := cmdAggregate([]string{"-in", raw, "-out", dir}, os.Stdout); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "summary.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1+len(impls) {
		t.Fatalf("summary has %d rows", len(rows))
	}
	if rows[1][9] != "2" {
		t.Fatalf("expected 2 runs per configuration, got %s", rows[1][9])
	}
	if _, _, err := aggregate(filepath.Join(dir, "missing"), dir); err != nil {
		t.Fatal(err) // empty input is not an error
	}
	_ = os.WriteFile(filepath.Join(raw, "broken.json"), []byte("{"), 0o644)
	if _, _, err := aggregate(raw, dir); err == nil {
		t.Fatal("broken JSON must fail")
	}
	if err := cmdRun([]string{"-impl", "bogus"}, &buf); err == nil {
		t.Fatal("bogus implementation accepted")
	}
}

func TestMatrixDryRun(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	if err := cmdMatrix([]string{"-exp", "all", "-out", dir, "-dry", "-repeats", "3", "-e10-memlimit", "1200"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"E1_goarena_n10000000", "E2_goarena_n5000000_v128_g16_c256", "E4_gobytes_n2000000_v4096",
		"E5_syncmap_n2000000_v128_g1_", "E6_gomap_n5000000_v128_g16_c64_zipf"} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %s", want)
		}
	}
	if !strings.Contains(s, "matrix: 282 runs") {
		t.Errorf("unexpected run count: %s", strings.SplitN(s, "\n", 2)[0])
	}
	// existing results are skipped
	o := experiments[0].Runs(matrixOptions{Repeats: 1})[0]
	_ = os.WriteFile(filepath.Join(dir, runFileName(o)), []byte("{}"), 0o644)
	out.Reset()
	if err := cmdMatrix([]string{"-exp", "e1", "-out", dir, "-dry", "-repeats", "1", "-scale", "0.001"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SKIPPED E8_gomap-gogc400_n30000000", "SKIPPED E8_syncmap_n50000000", "E9_goarena_n10000000",
		"E10_gobytes_n5000000_v128_g16_c64_uniform_r90_ml1200_rep3"} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %s", want)
		}
	}
	if strings.Contains(s, "SKIPPED E8_goarena") || strings.Contains(s, "SKIPPED E8_gomap_n20000000") {
		t.Error("runs that fit the budget must not be skipped")
	}
	if err := cmdMatrix([]string{"-exp", "E10", "-out", dir, "-dry"}, &out); err == nil {
		t.Fatal("E10 without -e10-memlimit must fail")
	}
	if err := cmdMatrix([]string{"-exp", "E8", "-out", dir, "-repeats", "1", "-mem-budget", "0.0001", "-scale", "0.001"}, &out); err != nil {
		t.Fatal(err) // every run is skipped, nothing is executed
	}
	if _, err := os.Stat(filepath.Join(dir, "E8_goarena_n20000_v128_g16_c64_uniform_r90_rep1.skipped.txt")); err != nil {
		t.Fatalf("skip reason file missing: %v", err)
	}
	if _, err := selectExperiments("E99"); err == nil {
		t.Fatal("unknown experiment accepted")
	}
	if scaleN(10, 0.5) != 1000 || scaleN(5000, 1) != 5000 || scaleN(1_000_000, 0.01) != 10000 {
		t.Fatal("scaleN")
	}
	var l bytes.Buffer
	cmdList(&l)
	usage(&l)
	if !strings.Contains(l.String(), "gobytes") || !strings.Contains(l.String(), "E5") {
		t.Fatal("list output")
	}
}

func TestParseTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.stderr.txt")
	log := strings.Join([]string{
		"gc 1 @0.010s 2%: 0.010+1.5+0.020 ms clock, 0.1+0.2/0.3/0.4+0.5 ms cpu, 4->4->1 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 16 P",
		"#phase populate-start",
		"gc 2 @0.110s 3%: 0.010+7.0+0.020 ms clock, x",
		"#phase populate-end",
		"#phase measure-start",
		"gc 3 @1.110s 4%: 0.050+10+0.020 ms clock, x",
		"gc 4 @2.110s 5%: 0.010+20+0.700 ms clock, x",
		"#phase measure-end",
		"gc 5 @3.110s 5%: 0.010+99+0.020 ms clock, x",
		"#phase forced-gc",
		"gc 6 @4.110s 5%: 0.010+3.5+0.020 ms clock, x",
	}, "\r\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := parseTrace(path)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Cycles != 2 || tr.MarkMsMax != 20 || tr.MarkMsMean != 15 || tr.STWMsMax != 0.7 || tr.LastPercent != 5 {
		t.Fatalf("measure phase: %+v", tr)
	}
	if tr.PopCycles != 1 || tr.PopMarkMsMax != 7 || tr.ForcedMarkMs != 3.5 {
		t.Fatalf("populate phase: %+v", tr)
	}
	if traceFileName("a/b.json") != "a/b.stderr.txt" {
		t.Fatal("traceFileName")
	}
	r := &Result{}
	if v := traceMetric(func(t *gcTrace) float64 { return 1 })(r); v == v {
		t.Fatal("expected NaN without trace")
	}
	phaseMarker("noop") // no output without GODEBUG
}

func TestProfileFlag(t *testing.T) {
	stop, err := startProfile(filepath.Join(t.TempDir(), "cpu.prof"))
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if _, err := startProfile(filepath.Join(t.TempDir(), "no", "such", "dir", "x")); err == nil {
		t.Fatal("expected error for bad path")
	}
}
