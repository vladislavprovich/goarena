// Package gcstats collects garbage-collector and process memory metrics for
// goarena's benchmark harness and dashboard.
//
// It is a thin layer over runtime/metrics (GC CPU time, cycles, heap goal,
// live and scannable heap, the stop-the-world pause histogram) plus
// runtime.ReadMemStats (exact total and per-cycle pause times) and the
// operating system's view of the process (resident set size, committed
// private bytes and CPU time; via K32GetProcessMemoryInfo/GetProcessTimes on
// Windows and /proc and getrusage on Linux, using only the standard
// library).
//
// Typical use:
//
//	before := gcstats.Read()
//	runWorkload()
//	d := gcstats.Between(before, gcstats.Read())
//	fmt.Printf("GC: %d cycles, %.1f%% CPU, max pause %v\n",
//		d.GCCycles, 100*d.GCCPUFraction, d.CyclePauseMax)
//
// The CPU figures of runtime/metrics are estimates computed by the runtime
// itself; they are only comparable with each other (GC versus total
// available CPU), not with OS CPU time.
package gcstats
