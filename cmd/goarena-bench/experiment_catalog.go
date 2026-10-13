package main

import (
	"fmt"
	"strings"
	"time"
)

// experiment is a named set of runs.
type experiment struct {
	Name        string
	Description string
	Runs        func(m matrixOptions) []options
}

// privateGiBPer10M is the measured private (committed) memory of every
// implementation at N = 10M in E1 (v1 run, 13.10.2026; baselines are
// unchanged in v2, goarena v2 measured 1.88 GiB). E8 scales it linearly
// with N plus a 15 % margin to decide whether a run fits into the memory
// budget of the machine.
var privateGiBPer10M = map[string]float64{
	"gomap": 4.06, "gomap-gogc400": 8.07, "syncmap": 7.31, "gobytes": 1.92, "goarena": 2.09,
}

// projectedGiB estimates the private memory of a run.
func projectedGiB(impl string, n int) float64 {
	return privateGiBPer10M[impl] * float64(n) / 10e6 * 1.15
}

// skipReason returns why a run must not be executed, or "".
func skipReason(o options, m matrixOptions) string {
	if o.Experiment != "E8" || m.MemBudgetGiB <= 0 {
		return ""
	}
	if p := projectedGiB(o.Impl, o.N); p > m.MemBudgetGiB {
		return fmt.Sprintf("projected private memory %.1f GiB (incl. 15 %% margin) exceeds the budget of %.1f GiB "+
			"(32 GB RAM machine): does not fit / risk of swapping", p, m.MemBudgetGiB)
	}
	return ""
}

var allImpls = []string{"gomap", "gomap-gogc400", "syncmap", "gobytes", "goarena"}

func base(m matrixOptions, exp, impl string, n int) options {
	return options{
		Impl: impl, N: scaleN(n, m.Scale), ValueSize: 128, Goroutines: 16, ReadRatio: 0.9,
		Dist: "uniform", ZipfS: 1.1, Duration: m.Duration, Warmup: m.Warmup,
		SampleEvery: 16, Shards: 256, ChunkMiB: 64, Seed: 1, Experiment: exp,
	}
}

func scaleN(n int, s float64) int {
	if s <= 0 || s == 1 {
		return n
	}
	v := int(float64(n) * s)
	if v < 1000 {
		v = 1000
	}
	return v
}

var experiments = []experiment{
	{
		Name:        "E1",
		Description: "throughput, latency, GC and memory vs number of entries N (all implementations)",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ { // repeats outermost: spreads drift
				for _, n := range []int{1_000_000, 2_000_000, 5_000_000, 10_000_000} {
					for _, impl := range allImpls {
						o := base(m, "E1", impl, n)
						o.Repeat, o.Seed = r, int64(r)
						out = append(out, o)
					}
				}
			}
			return out
		},
	},
	{
		Name:        "E1b",
		Description: "GOMEMLIMIT=4 GiB (debug.SetMemoryLimit) at N=5M and 10M, with gctrace",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ {
				for _, n := range []int{5_000_000, 10_000_000} {
					for _, impl := range []string{"gomap", "gobytes", "goarena"} {
						o := base(m, "E1b", impl, n)
						o.Repeat, o.Seed = r, int64(r)
						o.MemLimitMiB, o.GCTrace = 4096, true
						out = append(out, o)
					}
				}
			}
			return out
		},
	},
	{
		Name:        "E7",
		Description: "GC mark-phase duration vs N from GODEBUG=gctrace=1",
		Runs: func(m matrixOptions) []options {
			var out []options
			for _, n := range []int{1_000_000, 2_000_000, 5_000_000, 10_000_000} {
				for _, impl := range []string{"gomap", "syncmap", "gobytes", "goarena"} {
					o := base(m, "E7", impl, n)
					o.Repeat, o.GCTrace = 1, true
					o.Duration, o.Warmup = m.Short, 2*time.Second
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E8",
		Description: "large scale: N = 20M, 30M, 50M (runs that do not fit the memory budget are skipped)",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= 2; r++ {
				for _, n := range []int{20_000_000, 30_000_000, 50_000_000} {
					for _, impl := range allImpls {
						o := base(m, "E8", impl, n)
						o.Repeat, o.Seed, o.GCTrace = r, int64(r), true
						out = append(out, o)
					}
				}
			}
			return out
		},
	},
	{
		Name:        "E9",
		Description: "service with request garbage: every operation allocates a 1 KiB request object, N = 5M, 10M (+ goarena-gogc400)",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ {
				for _, n := range []int{5_000_000, 10_000_000} {
					for _, impl := range append(allImpls, "goarena-gogc400", "goarena-paced") {
						o := base(m, "E9", impl, n)
						o.Repeat, o.Seed = r, int64(r)
						o.GarbageRate, o.GarbageBytes = 1, 1024
						out = append(out, o)
					}
				}
			}
			return out
		},
	},
	{
		Name:        "E10",
		Description: "container memory budget: E9 workload at N = 5M with GOMEMLIMIT = 1.25 x gobytes RSS (+ goarena-gogc400, goarena-budget)",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ {
				for _, impl := range append(allImpls, "goarena-gogc400", "goarena-budget", "goarena-paced") {
					o := base(m, "E10", impl, 5_000_000)
					o.Repeat, o.Seed = r, int64(r)
					o.GarbageRate, o.GarbageBytes = 1, 1024
					o.MemLimitMiB = m.E10MemLimitMiB
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E1r",
		Description: "E1 at N = 10M re-run with goarena v0.3.0 (regression check), all implementations",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ {
				for _, impl := range allImpls {
					o := base(m, "E1r", impl, 10_000_000)
					o.Repeat, o.Seed = r, int64(r)
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E1t",
		Description: "E1 at N = 10M with 14 and 15 worker goroutines (GOMAXPROCS 16): tail vs CPU oversubscription",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= 2; r++ {
				for _, g := range []int{14, 15} {
					for _, impl := range allImpls {
						o := base(m, "E1t", impl, 10_000_000)
						o.Goroutines, o.Repeat, o.Seed = g, r, int64(r)
						out = append(out, o)
					}
				}
			}
			return out
		},
	},
	{
		Name:        "E2",
		Description: "goarena chunk size (1..256 MiB): populate time, cgo calls, reserved memory",
		Runs: func(m matrixOptions) []options {
			var out []options
			for r := 1; r <= m.Repeats; r++ {
				for _, c := range []int{1, 4, 16, 64, 256} {
					o := base(m, "E2", "goarena", 5_000_000)
					o.ChunkMiB, o.Repeat, o.Seed = c, r, int64(r)
					o.Duration, o.Warmup = m.Short/2, time.Second
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E4",
		Description: "value size 64..4096 B at N=2M",
		Runs: func(m matrixOptions) []options {
			var out []options
			for _, v := range []int{64, 256, 1024, 4096} {
				for _, impl := range []string{"gomap", "gobytes", "goarena"} {
					o := base(m, "E4", impl, 2_000_000)
					o.ValueSize, o.Repeat = v, 1
					o.Duration, o.Warmup = m.Short, 2*time.Second
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E5",
		Description: "goroutine scaling 1..16 at N=2M",
		Runs: func(m matrixOptions) []options {
			var out []options
			for _, g := range []int{1, 4, 8, 16} {
				for _, impl := range allImpls {
					o := base(m, "E5", impl, 2_000_000)
					o.Goroutines, o.Repeat = g, 1
					o.Duration, o.Warmup = m.Short, 2*time.Second
					out = append(out, o)
				}
			}
			return out
		},
	},
	{
		Name:        "E6",
		Description: "skewed (Zipf s=1.1) and write-heavy (50 %) workloads at N=5M",
		Runs: func(m matrixOptions) []options {
			var out []options
			for _, impl := range allImpls {
				o := base(m, "E6", impl, 5_000_000)
				o.Dist, o.Repeat = "zipf", 1
				o.Duration, o.Warmup = m.Short, 2*time.Second
				out = append(out, o)
				o2 := base(m, "E6", impl, 5_000_000)
				o2.ReadRatio, o2.Repeat = 0.5, 1
				o2.Duration, o2.Warmup = m.Short, 2*time.Second
				out = append(out, o2)
			}
			return out
		},
	},
}

// selectExperiments resolves a comma-separated list of experiment names.
func selectExperiments(list string) ([]experiment, error) {
	if list == "" || list == "all" {
		return experiments, nil
	}
	var out []experiment
	for _, name := range strings.Split(list, ",") {
		found := false
		for _, e := range experiments {
			if strings.EqualFold(e.Name, strings.TrimSpace(name)) {
				out = append(out, e)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown experiment %q", name)
		}
	}
	return out, nil
}
