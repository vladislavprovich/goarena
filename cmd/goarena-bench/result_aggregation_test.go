package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
