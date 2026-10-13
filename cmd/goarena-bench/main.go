// Command goarena-bench compares goarena's off-heap store with Go-heap
// caches under identical workloads.
//
// Usage:
//
//	goarena-bench run       -impl goarena -n 5000000 [flags]   one run, JSON result
//	goarena-bench matrix    -exp E1,E2 -out results/raw         run experiments, one process per run
//	goarena-bench aggregate -in results/raw -out results        write runs.csv and summary.csv
//	goarena-bench list                                          list implementations and experiments
//
// Every run populates N entries, warms up, and then measures a mixed
// read/write workload for a fixed duration with G goroutines. Every 16th
// operation is timed with the CPU time-stamp counter (see internal/tsc) and
// recorded in a log-linear histogram; GC metrics come from package gcstats.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:], os.Stdout)
	case "matrix":
		err = cmdMatrix(os.Args[2:], os.Stdout)
	case "aggregate":
		err = cmdAggregate(os.Args[2:], os.Stdout)
	case "list":
		cmdList(os.Stdout)
	case "-h", "-help", "--help", "help":
		usage(os.Stdout)
	default:
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goarena-bench:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: goarena-bench <run|matrix|aggregate|list> [flags]
  run        execute one benchmark run and print/write a JSON result
  matrix     execute experiments (E1..E10), each run in a separate process
  aggregate  combine JSON results into runs.csv and summary.csv
  list       show implementations and experiments
Use "goarena-bench <command> -h" for the flags of a command.`)
}

func runFlags(fs *flag.FlagSet, o *options, out *string) {
	fs.StringVar(&o.Impl, "impl", "goarena", "implementation: gomap, gomap-gogc400, syncmap, gobytes, goarena")
	fs.IntVar(&o.N, "n", 1_000_000, "number of entries to populate")
	fs.IntVar(&o.ValueSize, "value", 128, "value size in bytes")
	fs.IntVar(&o.Goroutines, "goroutines", 16, "number of worker goroutines")
	fs.Float64Var(&o.ReadRatio, "reads", 0.9, "share of reads in the mixed phase")
	fs.StringVar(&o.Dist, "dist", "uniform", "key distribution: uniform or zipf")
	fs.Float64Var(&o.ZipfS, "zipf-s", 1.1, "Zipf exponent s (> 1)")
	fs.DurationVar(&o.Duration, "duration", 15*time.Second, "measured phase duration")
	fs.DurationVar(&o.Warmup, "warmup", 3*time.Second, "warm-up duration (not measured)")
	fs.IntVar(&o.SampleEvery, "sample-every", 16, "time every k-th operation (power of two)")
	fs.IntVar(&o.Shards, "shards", 256, "number of shards (power of two)")
	fs.IntVar(&o.ChunkMiB, "chunk", 64, "goarena arena chunk size in MiB")
	fs.Int64Var(&o.Seed, "seed", 1, "workload seed")
	fs.StringVar(&o.Experiment, "exp", "adhoc", "experiment label stored in the result")
	fs.IntVar(&o.Repeat, "repeat", 1, "repeat number stored in the result")
	fs.IntVar(&o.MemLimitMiB, "memlimit", 0, "soft memory limit in MiB (debug.SetMemoryLimit), 0 = none")
	fs.Float64Var(&o.GarbageRate, "garbage-rate", 0, "share of operations that allocate a short-lived request object (E9/E10)")
	fs.IntVar(&o.GarbageBytes, "garbage-bytes", 1536, "payload size of a simulated request object")
	fs.BoolVar(&o.GCTrace, "gctrace", false, "mark the result as traced (the driver sets GODEBUG=gctrace=1)")
	fs.StringVar(out, "out", "", "write the JSON result to this file instead of stdout")
	fs.StringVar(&cpuProfile, "cpuprofile", "", "write a CPU profile of the measured phase to this file")
}

// cpuProfile is the -cpuprofile flag of "run".
var cpuProfile string

func cmdRun(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var o options
	var out string
	runFlags(fs, &o, &out)
	if err := fs.Parse(args); err != nil {
		return err
	}
	res, err := run(o)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%-14s n=%-9d v=%-5d g=%-3d ops/s=%-12.0f p50=%dns p99=%dns p999=%dns max=%.2fms gc=%d gcCPU=%.2f%% heap=%dMiB rss=%dMiB offheap=%dMiB populate=%.2fs\n",
		o.Impl, o.N, o.ValueSize, o.Goroutines, res.OpsPerSec, res.Latency.P50, res.Latency.P99, res.Latency.P999,
		float64(res.Latency.Max)/1e6, res.GC.GCCycles, res.GCCPUPercent, res.After.HeapObjectsBytes>>20,
		res.After.RSSBytes>>20, res.OffHeapBytes>>20, res.PopulateSeconds)
	if out == "" {
		_, err = stdout.Write(append(b, '\n'))
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out) // atomic: a crashed run leaves no partial file
}

func cmdMatrix(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	var m matrixOptions
	exps := fs.String("exp", "E1", "comma-separated experiments (E1,E1b,E2,E4,E5,E6,E7,E8,E9,E10 or all)")
	out := fs.String("out", "results/raw", "directory for JSON results")
	dry := fs.Bool("dry", false, "print the commands without running them")
	force := fs.Bool("force", false, "re-run even if the result file exists")
	timeout := fs.Duration("timeout", 10*time.Minute, "timeout per run")
	fs.IntVar(&m.Repeats, "repeats", 3, "repeats for E1/E2")
	fs.DurationVar(&m.Duration, "duration", 15*time.Second, "measured duration for E1")
	fs.DurationVar(&m.Warmup, "warmup", 3*time.Second, "warm-up for E1")
	fs.DurationVar(&m.Short, "short", 10*time.Second, "measured duration for E2/E4/E5/E6")
	fs.Float64Var(&m.Scale, "scale", 1, "scale factor for N (smoke tests)")
	fs.IntVar(&m.E10MemLimitMiB, "e10-memlimit", 0, "GOMEMLIMIT in MiB for E10 (1.25 x gobytes RSS at N=5M)")
	fs.Float64Var(&m.MemBudgetGiB, "mem-budget", 24, "skip E8 runs whose projected private memory exceeds this (GiB)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sel, err := selectExperiments(*exps)
	if err != nil {
		return err
	}
	return runMatrix(sel, m, *out, *dry, *force, *timeout, stdout)
}

func cmdAggregate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("aggregate", flag.ContinueOnError)
	in := fs.String("in", "results/raw", "directory with JSON results")
	out := fs.String("out", "results", "output directory for CSV files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	runs, groups, err := aggregate(*in, *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "aggregated %d runs into %d configurations\n", runs, groups)
	return nil
}

func cmdList(w io.Writer) {
	fmt.Fprintln(w, "implementations:")
	for _, n := range implNames() {
		fmt.Fprintf(w, "  %-14s %s\n", n, impls[n].Description)
	}
	fmt.Fprintln(w, "experiments:")
	for _, e := range experiments {
		fmt.Fprintf(w, "  %-4s %s\n", e.Name, e.Description)
	}
}
