package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// matrixOptions are the knobs of the experiment driver.
type matrixOptions struct {
	Repeats  int
	Duration time.Duration
	Warmup   time.Duration
	Short    time.Duration // duration of the secondary experiments
	Scale    float64       // multiplies every N (for quick smoke runs)

	// E10MemLimitMiB is the soft memory limit applied to every
	// implementation in E10 (1.25 x the RSS of gobytes at N = 5M in E1).
	E10MemLimitMiB int
	// MemBudgetGiB is the largest projected private memory a run may need
	// (E8); runs above it are skipped and the reason is recorded.
	MemBudgetGiB float64
}

// runFileName returns a stable file name that identifies the run.
func runFileName(o options) string {
	ml := ""
	if o.MemLimitMiB > 0 {
		ml = fmt.Sprintf("_ml%d", o.MemLimitMiB)
	}
	return fmt.Sprintf("%s_%s_n%d_v%d_g%d_c%d_%s_r%02d%s_rep%d.json",
		o.Experiment, o.Impl, o.N, o.ValueSize, o.Goroutines, o.ChunkMiB, o.Dist,
		int(o.ReadRatio*100+0.5), ml, o.Repeat)
}

// traceFileName is where the stderr (gctrace) of a traced run is stored.
func traceFileName(jsonPath string) string {
	return strings.TrimSuffix(jsonPath, ".json") + ".stderr.txt"
}

// runArgs converts options into command-line arguments of "run".
func runArgs(o options, out string) []string {
	return []string{"run",
		"-impl", o.Impl, "-n", itoa(o.N), "-value", itoa(o.ValueSize),
		"-goroutines", itoa(o.Goroutines), "-reads", fmt.Sprint(o.ReadRatio),
		"-dist", o.Dist, "-zipf-s", fmt.Sprint(o.ZipfS),
		"-duration", o.Duration.String(), "-warmup", o.Warmup.String(),
		"-sample-every", itoa(o.SampleEvery), "-shards", itoa(o.Shards),
		"-chunk", itoa(o.ChunkMiB), "-seed", fmt.Sprint(o.Seed),
		"-exp", o.Experiment, "-repeat", itoa(o.Repeat), "-out", out,
		"-memlimit", itoa(o.MemLimitMiB), "-gctrace=" + fmt.Sprint(o.GCTrace),
		"-garbage-rate", fmt.Sprint(o.GarbageRate), "-garbage-bytes", itoa(o.GarbageBytes),
	}
}

// runMatrix executes every run of the selected experiments in a separate
// child process, skipping runs whose result file already exists.
func runMatrix(exps []experiment, m matrixOptions, outDir string, dry, force bool, timeout time.Duration, log io.Writer) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var all []options
	for _, e := range exps {
		all = append(all, e.Runs(m)...)
	}
	fmt.Fprintf(log, "%s matrix: %d runs in %s\n", time.Now().Format("15:04:05"), len(all), outDir)
	start := time.Now()
	done := 0
	for i, o := range all {
		out := filepath.Join(outDir, runFileName(o))
		if _, err := os.Stat(out); err == nil && !force {
			fmt.Fprintf(log, "[%d/%d] skip (exists) %s\n", i+1, len(all), filepath.Base(out))
			continue
		}
		if reason := skipReason(o, m); reason != "" {
			fmt.Fprintf(log, "%s [%d/%d] SKIPPED %s: %s\n", time.Now().Format("15:04:05"), i+1, len(all), filepath.Base(out), reason)
			if !dry {
				_ = os.WriteFile(strings.TrimSuffix(out, ".json")+".skipped.txt", []byte(reason+"\n"), 0o644)
			}
			continue
		}
		if o.Experiment == "E10" && o.MemLimitMiB <= 0 {
			return fmt.Errorf("E10 needs -e10-memlimit (MiB)")
		}
		args := runArgs(o, out)
		if dry {
			fmt.Fprintf(log, "[%d/%d] %s %s\n", i+1, len(all), filepath.Base(exe), strings.Join(args, " "))
			continue
		}
		t0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Stdout, cmd.Stderr = log, log
		var traceFile *os.File
		if o.GCTrace {
			// gctrace lines go to a side file next to the JSON result
			cmd.Env = append(os.Environ(), "GODEBUG=gctrace=1")
			if traceFile, err = os.Create(traceFileName(out)); err != nil {
				cancel()
				return err
			}
			cmd.Stderr = traceFile
		}
		runErr := cmd.Run()
		if traceFile != nil {
			traceFile.Close()
		}
		cancel()
		done++
		el := time.Since(start)
		eta := time.Duration(float64(el) / float64(done) * float64(len(all)-i-1))
		status := "ok"
		if runErr != nil {
			status = "FAILED: " + runErr.Error()
		}
		fmt.Fprintf(log, "%s [%d/%d] %s %s in %.1fs (eta %s)\n", time.Now().Format("15:04:05"),
			i+1, len(all), filepath.Base(out), status, time.Since(t0).Seconds(), eta.Round(time.Second))
	}
	fmt.Fprintf(log, "%s matrix finished in %s\n", time.Now().Format("15:04:05"), time.Since(start).Round(time.Second))
	return nil
}
