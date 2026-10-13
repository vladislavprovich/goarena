package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// metric extracts one number from a result.
type metric struct {
	Name string
	Get  func(r *Result) float64
}

const mib = 1 << 20

var metrics = []metric{
	{"ops_per_sec", func(r *Result) float64 { return r.OpsPerSec }},
	{"p50_ns", func(r *Result) float64 { return float64(r.Latency.P50) }},
	{"p90_ns", func(r *Result) float64 { return float64(r.Latency.P90) }},
	{"p99_ns", func(r *Result) float64 { return float64(r.Latency.P99) }},
	{"p999_ns", func(r *Result) float64 { return float64(r.Latency.P999) }},
	{"max_ns", func(r *Result) float64 { return float64(r.Latency.Max) }},
	{"mean_ns", func(r *Result) float64 { return r.Latency.Mean }},
	{"read_p99_ns", func(r *Result) float64 { return float64(r.ReadLatency.P99) }},
	{"write_p99_ns", func(r *Result) float64 { return float64(r.WriteLatency.P99) }},
	{"read_p50_ns", func(r *Result) float64 { return float64(r.ReadLatency.P50) }},
	{"write_p50_ns", func(r *Result) float64 { return float64(r.WriteLatency.P50) }},
	{"gc_cycles", func(r *Result) float64 { return float64(r.GC.GCCycles) }},
	{"gc_per_sec", func(r *Result) float64 { return float64(r.GC.GCCycles) / r.MeasuredSeconds }},
	{"gc_cpu_percent", func(r *Result) float64 { return r.GCCPUPercent }},
	{"gc_busy_share_percent", func(r *Result) float64 { return 100 * r.GC.GCShareOfBusy }},
	{"assist_cpu_s", func(r *Result) float64 { return r.GC.AssistCPUSeconds }},
	{"pause_total_ms", func(r *Result) float64 { return float64(r.GC.PauseTotal) / 1e6 }},
	{"cycle_pause_max_ms", func(r *Result) float64 { return float64(r.GC.CyclePauseMax) / 1e6 }},
	{"pause_max_event_ms", func(r *Result) float64 { return float64(r.GC.PauseMaxEvent) / 1e6 }},
	{"alloc_mb_per_sec", func(r *Result) float64 { return float64(r.GC.AllocBytes) / mib / r.MeasuredSeconds }},
	{"process_cpu_s", func(r *Result) float64 { return r.GC.ProcessCPUSeconds }},
	{"heap_live_mb", func(r *Result) float64 { return float64(r.After.HeapLiveBytes) / mib }},
	{"heap_goal_mb", func(r *Result) float64 { return float64(r.After.HeapGoalBytes) / mib }},
	{"heap_objects_mb", func(r *Result) float64 { return float64(r.After.HeapObjectsBytes) / mib }},
	{"heap_scan_mb", func(r *Result) float64 { return float64(r.After.HeapScanBytes) / mib }},
	{"heap_objects", func(r *Result) float64 { return float64(r.After.HeapObjects) }},
	{"runtime_total_mb", func(r *Result) float64 { return float64(r.After.RuntimeBytes) / mib }},
	{"rss_mb", func(r *Result) float64 { return float64(r.After.RSSBytes) / mib }},
	{"peak_rss_mb", func(r *Result) float64 { return float64(r.After.PeakRSSBytes) / mib }},
	{"private_mb", func(r *Result) float64 { return float64(r.After.PrivateBytes) / mib }},
	{"offheap_mb", func(r *Result) float64 { return float64(r.OffHeapBytes) / mib }},
	{"populate_s", func(r *Result) float64 { return r.PopulateSeconds }},
	{"populate_ops_per_sec", func(r *Result) float64 { return r.PopulateOpsPerSec }},
	{"populate_gc_cycles", func(r *Result) float64 { return float64(r.PopulateGC.GCCycles) }},
	{"populate_cgo_calls", func(r *Result) float64 { return float64(r.PopulateCgoCalls) }},
	{"measured_cgo_calls", func(r *Result) float64 { return float64(r.MeasuredCgoCalls) }},
	{"arena_chunks", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return float64(r.Store.ArenaChunks)
	}},
	{"arena_reserved_mb", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return float64(r.Store.ArenaReservedBytes) / mib
	}},
	{"index_mb", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return float64(r.Store.IndexBytes) / mib
	}},
	{"fragmentation", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return r.Store.InternalFragmentation
	}},
	{"slab_inuse_mb", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return float64(r.Store.SlabInUseBytes) / mib
	}},
	{"entry_mb", func(r *Result) float64 {
		if r.Store == nil {
			return 0
		}
		return float64(r.Store.EntryBytes) / mib
	}},
	{"misses", func(r *Result) float64 { return float64(r.Misses) }},
	{"memlimit_mib", func(r *Result) float64 { return float64(r.Options.MemLimitMiB) }},
	{"garbage_rate", func(r *Result) float64 { return r.Options.GarbageRate }},
	{"garbage_bytes", func(r *Result) float64 { return float64(r.Options.GarbageBytes) }},
	{"read_p999_ns", func(r *Result) float64 { return float64(r.ReadLatency.P999) }},
	{"write_p999_ns", func(r *Result) float64 { return float64(r.WriteLatency.P999) }},
	{"stw_per_sec_ms", func(r *Result) float64 { return float64(r.GC.PauseTotal) / 1e6 / r.MeasuredSeconds }},
	{"over_1ms_per_million", func(r *Result) float64 {
		var n uint64
		for _, b := range r.LatencyBuckets {
			if b.Lower > 1_000_000 {
				n += b.Count
			}
		}
		if r.Latency.Count == 0 {
			return 0
		}
		return float64(n) / float64(r.Latency.Count) * 1e6
	}},
	{"pacer_gc_percent", func(r *Result) float64 {
		if r.Pacer == nil {
			return math.NaN()
		}
		return float64(r.Pacer.GCPercent)
	}},
	{"pacer_go_limit_mib", func(r *Result) float64 {
		if r.Pacer == nil || r.Pacer.GoMemoryLimit == math.MaxInt64 {
			return math.NaN()
		}
		return float64(r.Pacer.GoMemoryLimit >> 20)
	}},
	// gctrace-derived metrics (NaN for runs without GODEBUG=gctrace=1)
	{"trace_cycles", traceMetric(func(t *gcTrace) float64 { return float64(t.Cycles) })},
	{"mark_ms_mean", traceMetric(func(t *gcTrace) float64 { return t.MarkMsMean })},
	{"mark_ms_max", traceMetric(func(t *gcTrace) float64 { return t.MarkMsMax })},
	{"stw_ms_max", traceMetric(func(t *gcTrace) float64 { return t.STWMsMax })},
	{"trace_gc_cpu_percent", traceMetric(func(t *gcTrace) float64 { return t.LastPercent })},
	{"populate_trace_cycles", traceMetric(func(t *gcTrace) float64 { return float64(t.PopCycles) })},
	{"populate_mark_ms_max", traceMetric(func(t *gcTrace) float64 { return t.PopMarkMsMax })},
	{"forced_gc_ms", func(r *Result) float64 { return r.ForcedGCms }},
	{"forced_mark_ms", traceMetric(func(t *gcTrace) float64 { return t.ForcedMarkMs })},
}

func traceMetric(f func(*gcTrace) float64) func(*Result) float64 {
	return func(r *Result) float64 {
		if r.trace == nil {
			return math.NaN()
		}
		return f(r.trace)
	}
}

// gcTrace summarises the GODEBUG=gctrace=1 lines of one run.
type gcTrace struct {
	Cycles       int     // GC cycles during the measured phase
	MarkMsMean   float64 // mean wall time of the concurrent mark phase
	MarkMsMax    float64 // longest concurrent mark phase
	STWMsMax     float64 // longest single STW phase (sweep or mark termination)
	LastPercent  float64 // the runtime's cumulative GC CPU % at the last cycle
	PopCycles    int     // GC cycles during populate
	PopMarkMsMax float64 // longest mark phase during populate
	ForcedMarkMs float64 // mark phase of the forced runtime.GC() after the run
}

// gcLine matches e.g. "gc 7 @2.345s 4%: 0.031+52+0.044 ms clock, ...".
var gcLine = regexp.MustCompile(`^gc \d+ @[\d.]+s (\d+)%: ([\d.]+)\+([\d.]+)\+([\d.]+) ms clock`)

// parseTrace reads a stderr capture with "#phase" markers and gctrace lines.
func parseTrace(path string) (*gcTrace, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := &gcTrace{}
	phase := ""
	var markSum float64
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "#phase ") {
			phase = strings.TrimSpace(strings.TrimPrefix(line, "#phase "))
			continue
		}
		m := gcLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pct, _ := strconv.ParseFloat(m[1], 64)
		sweepTerm, _ := strconv.ParseFloat(m[2], 64)
		mark, _ := strconv.ParseFloat(m[3], 64)
		markTerm, _ := strconv.ParseFloat(m[4], 64)
		switch phase {
		case "populate-start":
			t.PopCycles++
			t.PopMarkMsMax = math.Max(t.PopMarkMsMax, mark)
		case "forced-gc":
			t.ForcedMarkMs = mark
		case "measure-start":
			t.Cycles++
			markSum += mark
			t.MarkMsMax = math.Max(t.MarkMsMax, mark)
			t.STWMsMax = math.Max(t.STWMsMax, math.Max(sweepTerm, markTerm))
			t.LastPercent = pct
		}
	}
	if t.Cycles > 0 {
		t.MarkMsMean = markSum / float64(t.Cycles)
	}
	return t, nil
}

// groupKey identifies runs that are repeats of the same configuration.
type groupKey struct {
	Exp, Impl, Dist    string
	N, Value, G, Chunk int
	ReadPct, MemLimit  int
}

func keyOf(r *Result) groupKey {
	o := r.Options
	return groupKey{o.Experiment, o.Impl, o.Dist, o.N, o.ValueSize, o.Goroutines, o.ChunkMiB, int(o.ReadRatio*100 + 0.5), o.MemLimitMiB}
}

func loadResults(dir string) ([]*Result, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []*Result
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var r Result
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if t, err := parseTrace(traceFileName(f)); err == nil {
			r.trace = t
		}
		out = append(out, &r)
	}
	return out, nil
}

func fmtF(v float64) string { return strconv.FormatFloat(v, 'g', 8, 64) }

// aggregate writes runs.csv (one row per run) and summary.csv (mean, std,
// min, max per configuration) into outDir.
func aggregate(inDir, outDir string) (int, int, error) {
	rs, err := loadResults(inDir)
	if err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, 0, err
	}
	head := []string{"experiment", "impl", "n", "value_size", "goroutines", "chunk_mib", "dist", "read_pct", "memlimit"}

	// runs.csv
	f, err := os.Create(filepath.Join(outDir, "runs.csv"))
	if err != nil {
		return 0, 0, err
	}
	w := csv.NewWriter(f)
	h := append(append([]string{}, head...), "repeat")
	for _, m := range metrics {
		h = append(h, m.Name)
	}
	_ = w.Write(h)
	groups := map[groupKey][]*Result{}
	var order []groupKey
	for _, r := range rs {
		k := keyOf(r)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
		row := []string{k.Exp, k.Impl, itoa(k.N), itoa(k.Value), itoa(k.G), itoa(k.Chunk), k.Dist, itoa(k.ReadPct), itoa(k.MemLimit), itoa(r.Options.Repeat)}
		for _, m := range metrics {
			row = append(row, fmtF(m.Get(r)))
		}
		_ = w.Write(row)
	}
	w.Flush()
	if err := f.Close(); err != nil {
		return 0, 0, err
	}

	// summary.csv
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.Exp != b.Exp {
			return a.Exp < b.Exp
		}
		if a.Impl != b.Impl {
			return a.Impl < b.Impl
		}
		if a.N != b.N {
			return a.N < b.N
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		if a.G != b.G {
			return a.G < b.G
		}
		if a.Chunk != b.Chunk {
			return a.Chunk < b.Chunk
		}
		if a.Dist != b.Dist {
			return a.Dist < b.Dist
		}
		if a.ReadPct != b.ReadPct {
			return a.ReadPct < b.ReadPct
		}
		return a.MemLimit < b.MemLimit
	})
	f2, err := os.Create(filepath.Join(outDir, "summary.csv"))
	if err != nil {
		return 0, 0, err
	}
	w2 := csv.NewWriter(f2)
	h2 := append(append([]string{}, head...), "runs")
	for _, m := range metrics {
		h2 = append(h2, m.Name+"_mean", m.Name+"_std", m.Name+"_min", m.Name+"_max")
	}
	_ = w2.Write(h2)
	for _, k := range order {
		g := groups[k]
		row := []string{k.Exp, k.Impl, itoa(k.N), itoa(k.Value), itoa(k.G), itoa(k.Chunk), k.Dist, itoa(k.ReadPct), itoa(k.MemLimit), itoa(len(g))}
		for _, m := range metrics {
			mean, std, lo, hi := stats(g, m.Get)
			row = append(row, fmtF(mean), fmtF(std), fmtF(lo), fmtF(hi))
		}
		_ = w2.Write(row)
	}
	w2.Flush()
	return len(rs), len(order), f2.Close()
}

// stats returns the mean, sample standard deviation, minimum and maximum.
func stats(rs []*Result, get func(*Result) float64) (mean, std, lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, r := range rs {
		v := get(r)
		mean += v
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	mean /= float64(len(rs))
	if len(rs) > 1 {
		for _, r := range rs {
			d := get(r) - mean
			std += d * d
		}
		std = math.Sqrt(std / float64(len(rs)-1))
	}
	return mean, std, lo, hi
}
