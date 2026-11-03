package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"
)

// The tests never run the real harness: the test binary re-executes itself
// with GOARENA_LAB_FAKE_BENCH=1 and then behaves like goarena-bench (the
// "run -h" flag list, "list", and "run" emitting progress lines and a
// result file). Knobs:
//
//	GOARENA_LAB_FAKE_STEPS     measure progress lines per run (default 3)
//	GOARENA_LAB_FAKE_SLEEP_MS  pause between progress lines (default 2)
//	GOARENA_LAB_FAKE_FAIL      implementation that exits with status 1
func TestMain(m *testing.M) {
	if os.Getenv("GOARENA_LAB_FAKE_BENCH") == "1" {
		os.Exit(fakeBench(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

var fakeImpls = []string{"gomap", "gomap-gogc400", "syncmap", "gobytes", "goarena", "goarena-gogc400",
	"goarena-budget", "goarena-paced", "goarena-evict"}

func fakeBench(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: goarena-bench <run|matrix|aggregate|list> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		fmt.Fprintln(stdout, "implementations:")
		for _, n := range fakeImpls {
			fmt.Fprintf(stdout, "  %-16s fake %s\n", n, n)
		}
		fmt.Fprintln(stdout, "experiments:")
		fmt.Fprintln(stdout, "  E1   throughput vs N")
		return 0
	case "run":
		return fakeRun(args[1:], stderr)
	}
	return 2
}

// fakeThroughput models an implementation whose throughput scales up to 4
// goroutines and halves at 8 or more (so sweeps have a knee at G=8).
func fakeThroughput(impl string, g int) float64 {
	base := map[string]float64{"gomap": 5e6, "goarena": 8e6, "gobytes": 6e6}[impl]
	if base == 0 {
		base = 4e6
	}
	switch {
	case g >= 8:
		return base * 2
	case g >= 4:
		return base * 4
	}
	return base * float64(g)
}

func fakeRun(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	impl := fs.String("impl", "goarena", "implementation")
	n := fs.Int("n", 1000000, "number of entries")
	value := fs.Int("value", 128, "value size")
	g := fs.Int("goroutines", 16, "goroutines")
	reads := fs.Float64("reads", 0.9, "read share")
	dist := fs.String("dist", "uniform", "distribution")
	zipfS := fs.Float64("zipf-s", 1.1, "zipf s")
	dur := fs.Duration("duration", time.Second, "duration")
	warm := fs.Duration("warmup", 0, "warm-up")
	gr := fs.Float64("garbage-rate", 0, "garbage rate")
	gb := fs.Int("garbage-bytes", 1536, "garbage bytes")
	ml := fs.Int("memlimit", 0, "memory limit")
	maxmem := fs.Int("maxmem", 0, "max memory")
	fill := fs.Bool("fill-on-miss", false, "fill on miss")
	profile := fs.String("profile", "steady", "profile")
	progress := fs.Bool("progress", false, "progress lines")
	exp := fs.String("exp", "adhoc", "experiment")
	out := fs.String("out", "", "output file")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, "goarena-bench:", err)
		return 1
	}
	if *impl == os.Getenv("GOARENA_LAB_FAKE_FAIL") {
		fmt.Fprintln(stderr, "goarena-bench: fake failure of", *impl)
		return 1
	}
	steps, _ := strconv.Atoi(os.Getenv("GOARENA_LAB_FAKE_STEPS"))
	if steps <= 0 {
		steps = 3
	}
	sleep := 2 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("GOARENA_LAB_FAKE_SLEEP_MS")); err == nil {
		sleep = time.Duration(ms) * time.Millisecond
	}
	fmt.Fprintln(stderr, "fake: starting", *impl)
	ops := fakeThroughput(*impl, *g)
	var timeline []map[string]any
	emit := func(p map[string]any) {
		p["type"], p["impl"] = "progress", *impl
		timeline = append(timeline, p)
		if *progress {
			b, _ := json.Marshal(p)
			fmt.Fprintln(stderr, string(b))
		}
		time.Sleep(sleep)
	}
	emit(map[string]any{"phase": "populate", "t_s": 1.0, "phase_t_s": 1.0, "interval_s": 1.0, "populated": *n / 2, "n": *n,
		"rss_bytes": 100 << 20, "heap_bytes": 10 << 20, "offheap_bytes": 50 << 20})
	emit(map[string]any{"phase": "warmup", "t_s": 2.0, "phase_t_s": 1.0, "interval_s": 1.0, "populated": *n, "n": *n})
	for i := 1; i <= steps; i++ {
		stage, idx := "", 0
		if *profile != "steady" {
			stage, idx = fmt.Sprintf("g=%d", i), i-1
		}
		emit(map[string]any{"phase": "measure", "stage": stage, "stage_index": idx, "t_s": 2.0 + float64(i),
			"phase_t_s": float64(i), "interval_s": 1.0, "goroutines": *g, "read_ratio": *reads, "ops_per_sec": ops,
			"ops": ops * float64(i), "hit_ratio": 1, "p50_ns": 300, "p99_ns": 900, "p999_ns": 4000, "max_ns": 90000,
			"gc_cycles": 1, "gc_cycles_total": i, "gc_cpu_percent": 0.5, "stw_ms": 0.2, "heap_bytes": 10 << 20,
			"heap_goal_bytes": 20 << 20, "rss_bytes": 120 << 20, "offheap_bytes": 50 << 20, "populated": *n, "n": *n,
			"unknown_future_field": "ignored"})
	}
	fmt.Fprintf(stderr, "%-14s n=%-9d ops/s=%.0f (fake)\n", *impl, *n, ops)
	measured := dur.Seconds()
	res := map[string]any{
		"options": map[string]any{"impl": *impl, "n": *n, "value_size": *value, "goroutines": *g, "read_ratio": *reads,
			"dist": *dist, "zipf_s": *zipfS, "duration_ns": dur.Nanoseconds(), "warmup_ns": warm.Nanoseconds(),
			"experiment": *exp, "repeat": 1, "memlimit_mib": *ml, "garbage_rate": *gr, "garbage_bytes": *gb,
			"profile": *profile, "maxmem_mib": *maxmem, "fill_on_miss": *fill},
		"go_version": "go1.27.0", "goos": "windows", "goarch": "amd64", "gomaxprocs": 16, "num_cpu": 16,
		"populate_seconds": 0.25, "measured_seconds": measured, "ops": ops * measured, "reads": ops * measured * *reads,
		"writes": ops * measured * (1 - *reads), "misses": 0, "ops_per_sec": ops,
		"latency":         map[string]any{"count": 1000, "mean_ns": 400.5, "p50_ns": 300, "p90_ns": 500, "p99_ns": 900, "p999_ns": 4000, "max_ns": 90000},
		"latency_buckets": []map[string]any{{"lo": 256, "hi": 263, "n": 500}, {"lo": 880, "hi": 895, "n": 490}, {"lo": 3968, "hi": 4095, "n": 9}, {"lo": 86016, "hi": 90111, "n": 1}},
		"gc":              map[string]any{"gc_cycles": 3, "pause_total_ns": 1000},
		"gc_cpu_percent":  0.5,
		"after":           map[string]any{"heap_objects_bytes": 10 << 20, "heap_goal_bytes": 20 << 20, "rss_bytes": 120 << 20, "peak_rss_bytes": 130 << 20},
		"forced_gc_ms":    0.4, "goarena_version": "v0.4.0-fake", "offheap_bytes": 50 << 20,
		"write_errors": 0, "evictions": 0,
		"timeline": timeline,
	}
	if *profile != "steady" {
		res["stages"] = []map[string]any{
			{"name": "g=1", "index": 0, "goroutines": 1, "read_ratio": *reads, "start_s": 0, "seconds": 1, "ops": ops, "ops_per_sec": ops,
				"latency": map[string]any{"count": 10, "p50_ns": 300, "p99_ns": 900, "p999_ns": 4000, "max_ns": 9000}, "gc_cycles": 1, "gc_cpu_percent": 0.4},
		}
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if *out == "" {
		os.Stdout.Write(b)
		return 0
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(stderr, "goarena-bench:", err)
		return 1
	}
	return 0
}
