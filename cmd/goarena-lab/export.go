package main

import (
	"encoding/csv"
	"io"
	"strconv"
)

func csvFloat(v float64, prec int) string {
	if !finite(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}

func csvOptFloat(v *float64, prec int) string {
	if v == nil {
		return ""
	}
	return csvFloat(*v, prec)
}

// writeSessionCSV writes one row per run of a session.
func writeSessionCSV(w io.Writer, s *Session) error {
	cw := csv.NewWriter(w)
	header := []string{
		"session_id", "kind", "preset", "sweep_param", "param_value", "index", "impl", "state", "error",
		"n", "value_size", "goroutines", "read_ratio", "dist", "zipf_s", "garbage_rate", "garbage_bytes",
		"memlimit_mib", "maxmem_mib", "fill_on_miss", "profile", "duration_s", "warmup_s",
		"ops_per_sec", "mops_per_sec", "mean_us", "p50_us", "p90_us", "p99_us", "p999_us", "max_us",
		"gc_cycles", "gc_cycles_per_s", "gc_cpu_percent", "heap_mib", "heap_goal_mib", "rss_mib", "peak_rss_mib",
		"offheap_mib", "populate_s", "forced_gc_ms", "measured_s", "ops", "reads", "writes", "misses",
		"hit_ratio", "evictions", "write_errors", "go_version", "goarena_version",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	param := ""
	if s.Sweep != nil {
		param = s.Sweep.Param
	}
	for _, r := range s.Runs {
		c := r.Config
		pv := ""
		if r.ParamValue != nil {
			pv = formatFloatArg(*r.ParamValue)
		}
		row := []string{
			s.ID, s.Kind, s.Preset, param, pv, strconv.Itoa(r.Index), r.Impl, r.State, r.Error,
			strconv.Itoa(c.N), strconv.Itoa(c.ValueSize), strconv.Itoa(c.Goroutines), formatFloatArg(c.ReadRatio),
			c.Dist, formatFloatArg(c.ZipfS), formatFloatArg(c.GarbageRate), strconv.Itoa(c.GarbageBytes),
			strconv.Itoa(c.MemLimitMiB), strconv.Itoa(c.MaxMemMiB), strconv.FormatBool(c.FillOnMiss), c.Profile,
			formatFloatArg(c.DurationS), formatFloatArg(c.WarmupS),
		}
		if m := r.Summary; m != nil {
			row = append(row,
				csvFloat(m.OpsPerSec, 0), csvFloat(m.OpsPerSec/1e6, 3), csvFloat(m.MeanNs/1e3, 3),
				csvFloat(m.P50Ns/1e3, 3), csvFloat(m.P90Ns/1e3, 3), csvFloat(m.P99Ns/1e3, 3),
				csvFloat(m.P999Ns/1e3, 3), csvFloat(m.MaxNs/1e3, 3),
				csvFloat(m.GCCycles, 0), csvFloat(m.GCCyclesPerSec, 2), csvFloat(m.GCCPUPercent, 3),
				csvFloat(m.HeapBytes/mib, 1), csvFloat(m.HeapGoalBytes/mib, 1), csvFloat(m.RSSBytes/mib, 1),
				csvFloat(m.PeakRSSBytes/mib, 1), csvFloat(m.OffHeapBytes/mib, 1), csvFloat(m.PopulateSeconds, 3),
				csvFloat(m.ForcedGCms, 3), csvFloat(m.MeasuredSeconds, 3), csvFloat(m.Ops, 0), csvFloat(m.Reads, 0),
				csvFloat(m.Writes, 0), csvFloat(m.Misses, 0), csvOptFloat(m.HitRatio, 5), csvOptFloat(m.Evictions, 0),
				csvOptFloat(m.WriteErrors, 0), m.GoVersion, m.GoarenaVersion,
			)
		} else {
			for len(row) < len(header) {
				row = append(row, "")
			}
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeComparisonCSV writes one row per (version, implementation).
func writeComparisonCSV(w io.Writer, c *Comparison) error {
	cw := csv.NewWriter(w)
	header := []string{"scenario", "version", "impl", "runs", "experiment", "go_version", "goarena_version",
		"n", "value_size", "goroutines", "dist", "read_ratio", "garbage_rate", "memlimit_mib",
		"mops_per_sec", "mops_std", "p50_us", "p99_us", "p999_us", "max_us", "gc_cpu_percent", "gc_cycles",
		"heap_mib", "rss_mib", "offheap_mib", "populate_s", "forced_gc_ms"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, impl := range c.Impls {
		for _, v := range c.Versions {
			g := c.Cells[impl][v]
			if g == nil {
				continue
			}
			row := []string{c.Scenario.ID, v, impl, strconv.Itoa(g.Runs), g.Experiment, joinComma(g.GoVersions),
				g.GoarenaVersion, strconv.Itoa(g.N), strconv.Itoa(g.ValueSize), strconv.Itoa(g.Goroutines), g.Dist,
				formatFloatArg(g.ReadRatio), formatFloatArg(g.GarbageRate), strconv.Itoa(g.MemLimitMiB),
				csvFloat(g.OpsPerSec/1e6, 3), csvFloat(g.OpsPerSecStd/1e6, 3), csvFloat(g.P50Ns/1e3, 3),
				csvFloat(g.P99Ns/1e3, 3), csvFloat(g.P999Ns/1e3, 3), csvFloat(g.MaxNs/1e3, 3),
				csvFloat(g.GCCPUPercent, 3), csvFloat(g.GCCycles, 1), csvFloat(g.HeapBytes/mib, 1),
				csvFloat(g.RSSBytes/mib, 1), csvFloat(g.OffHeapBytes/mib, 1), csvFloat(g.PopulateSeconds, 3),
				csvFloat(g.ForcedGCms, 3)}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
