package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// histSource is a directory of historical result files of one version.
type histSource struct {
	Dir     string // relative to the results directory
	Version string
}

// historicalSources lists the result directories that are imported.
var historicalSources = []histSource{
	{Dir: "v1/raw", Version: "v0.1"},
	{Dir: "v2/raw", Version: "v0.2"},
	{Dir: "raw", Version: "v0.3"},
	{Dir: "demo/baseline-v0.4/raw", Version: "v0.4"},
	{Dir: "demo/raw", Version: "demo"},
}

// versionOrder is the column order of the version comparison.
var versionOrder = []string{"v0.1", "v0.2", "v0.3", "v0.4", "demo"}

// histRecord is one historical result file.
type histRecord struct {
	Version        string
	File           string
	Experiment     string
	Impl           string
	N              int
	ValueSize      int
	Goroutines     int
	Dist           string
	ZipfS          float64
	ReadRatio      float64
	GarbageRate    float64
	MemLimitMiB    int
	GCTrace        bool
	GoVersion      string
	GoarenaVersion string
	m              histMetrics
}

// histMetrics are the averaged metrics.
type histMetrics struct {
	OpsPerSec       float64 `json:"ops_per_sec"`
	P50Ns           float64 `json:"p50_ns"`
	P99Ns           float64 `json:"p99_ns"`
	P999Ns          float64 `json:"p999_ns"`
	MaxNs           float64 `json:"max_ns"`
	GCCPUPercent    float64 `json:"gc_cpu_percent"`
	GCCycles        float64 `json:"gc_cycles"`
	HeapBytes       float64 `json:"heap_bytes"`
	RSSBytes        float64 `json:"rss_bytes"`
	OffHeapBytes    float64 `json:"offheap_bytes"`
	PopulateSeconds float64 `json:"populate_seconds"`
	ForcedGCms      float64 `json:"forced_gc_ms"`
}

// HistGroup is the average of the repeats of one configuration.
type HistGroup struct {
	Version        string   `json:"version"`
	Experiment     string   `json:"experiment"`
	Impl           string   `json:"impl"`
	N              int      `json:"n"`
	ValueSize      int      `json:"value_size"`
	Goroutines     int      `json:"goroutines"`
	Dist           string   `json:"dist"`
	ReadRatio      float64  `json:"read_ratio"`
	GarbageRate    float64  `json:"garbage_rate"`
	MemLimitMiB    int      `json:"memlimit_mib"`
	Runs           int      `json:"runs"`
	GoVersions     []string `json:"go_versions"`
	GoarenaVersion string   `json:"goarena_version,omitempty"`
	OpsPerSecStd   float64  `json:"ops_per_sec_std"`
	histMetrics
}

// SourceInfo describes one imported directory.
type SourceInfo struct {
	Version    string   `json:"version"`
	Dir        string   `json:"dir"`
	Exists     bool     `json:"exists"`
	Files      int      `json:"files"`
	Errors     int      `json:"errors"`
	GoVersions []string `json:"go_versions"`
}

// HistIndex is the imported historical data.
type HistIndex struct {
	LoadedAt  time.Time    `json:"loaded_at"`
	Versions  []string     `json:"versions"`
	Sources   []SourceInfo `json:"sources"`
	Groups    []HistGroup  `json:"groups"`
	Scenarios []Scenario   `json:"scenarios"`
	records   []histRecord
}

// Historical caches the imported historical results.
type Historical struct {
	resultsDir string
	mu         sync.Mutex
	index      *HistIndex
}

func newHistorical(resultsDir string) *Historical { return &Historical{resultsDir: resultsDir} }

// Index returns the imported data, loading it on first use or on reload.
func (h *Historical) Index(reload bool) *HistIndex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.index == nil || reload {
		h.index = loadHistorical(h.resultsDir)
	}
	return h.index
}

// loadHistorical reads every source directory.
func loadHistorical(resultsDir string) *HistIndex {
	idx := &HistIndex{LoadedAt: time.Now(), Versions: versionOrder, Scenarios: scenarios}
	for _, src := range historicalSources {
		dir := filepath.Join(resultsDir, filepath.FromSlash(src.Dir))
		si := SourceInfo{Version: src.Version, Dir: filepath.ToSlash(filepath.Join(filepath.Base(resultsDir), src.Dir)), GoVersions: []string{}}
		des, err := os.ReadDir(dir)
		if err == nil {
			si.Exists = true
		}
		for _, de := range des {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, de.Name()))
			if err != nil {
				si.Errors++
				continue
			}
			rec, err := parseHistRecord(b)
			if err != nil {
				si.Errors++
				continue
			}
			rec.Version, rec.File = src.Version, de.Name()
			si.Files++
			if rec.GoVersion != "" && !slices.Contains(si.GoVersions, rec.GoVersion) {
				si.GoVersions = append(si.GoVersions, rec.GoVersion)
			}
			idx.records = append(idx.records, rec)
		}
		idx.Sources = append(idx.Sources, si)
	}
	idx.Groups = groupRecords(idx.records)
	return idx
}

func parseHistRecord(b []byte) (histRecord, error) {
	r, err := parseBenchResult(b)
	if err != nil {
		return histRecord{}, err
	}
	o := r.Options
	if o.Impl == "" || o.N <= 0 {
		return histRecord{}, fmt.Errorf("not a goarena-bench result")
	}
	return histRecord{
		Experiment: o.Experiment, Impl: o.Impl, N: o.N, ValueSize: o.ValueSize, Goroutines: o.Goroutines,
		Dist: o.Dist, ZipfS: o.ZipfS, ReadRatio: o.ReadRatio, GarbageRate: o.GarbageRate, MemLimitMiB: o.MemLimitMiB,
		GCTrace: o.GCTrace, GoVersion: r.GoVersion, GoarenaVersion: r.Version,
		m: histMetrics{
			OpsPerSec: r.OpsPerSec, P50Ns: float64(r.Latency.P50), P99Ns: float64(r.Latency.P99),
			P999Ns: float64(r.Latency.P999), MaxNs: float64(r.Latency.Max), GCCPUPercent: r.GCCPUPercent,
			GCCycles: r.GC.GCCycles, HeapBytes: r.After.HeapObjectsBytes, RSSBytes: r.After.RSSBytes,
			OffHeapBytes: r.OffHeapBytes, PopulateSeconds: r.PopulateSeconds, ForcedGCms: r.ForcedGCms,
		},
	}, nil
}

// groupKey identifies the repeats of one configuration.
type groupKey struct {
	Version, Experiment, Impl string
	N, ValueSize, Goroutines  int
	Dist                      string
	ReadRatio, GarbageRate    float64
	MemLimitMiB               int
}

func keyOf(r histRecord) groupKey {
	return groupKey{r.Version, r.Experiment, r.Impl, r.N, r.ValueSize, r.Goroutines, r.Dist, r.ReadRatio, r.GarbageRate, r.MemLimitMiB}
}

// groupRecords averages the repeats of every configuration.
func groupRecords(recs []histRecord) []HistGroup {
	byKey := map[groupKey][]histRecord{}
	var keys []groupKey
	for _, r := range recs {
		k := keyOf(r)
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], r)
	}
	out := make([]HistGroup, 0, len(keys))
	for _, k := range keys {
		g := aggregate(byKey[k])
		g.Version, g.Experiment, g.Impl = k.Version, k.Experiment, k.Impl
		g.N, g.ValueSize, g.Goroutines, g.Dist = k.N, k.ValueSize, k.Goroutines, k.Dist
		g.ReadRatio, g.GarbageRate, g.MemLimitMiB = k.ReadRatio, k.GarbageRate, k.MemLimitMiB
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Version != b.Version {
			return slices.Index(versionOrder, a.Version) < slices.Index(versionOrder, b.Version)
		}
		if a.Experiment != b.Experiment {
			return a.Experiment < b.Experiment
		}
		if a.N != b.N {
			return a.N < b.N
		}
		return implRank(a.Impl) < implRank(b.Impl)
	})
	return out
}

// implRank orders implementations as in the catalog; unknown ones last.
func implRank(name string) int {
	for i, im := range implCatalog {
		if im.Name == name {
			return i
		}
	}
	return len(implCatalog)
}

// aggregate returns the mean of the metrics (and the std of throughput).
func aggregate(recs []histRecord) HistGroup {
	g := HistGroup{Runs: len(recs), GoVersions: []string{}}
	if len(recs) == 0 {
		return g
	}
	n := float64(len(recs))
	for _, r := range recs {
		m := r.m
		g.OpsPerSec += m.OpsPerSec / n
		g.P50Ns += m.P50Ns / n
		g.P99Ns += m.P99Ns / n
		g.P999Ns += m.P999Ns / n
		g.MaxNs += m.MaxNs / n
		g.GCCPUPercent += m.GCCPUPercent / n
		g.GCCycles += m.GCCycles / n
		g.HeapBytes += m.HeapBytes / n
		g.RSSBytes += m.RSSBytes / n
		g.OffHeapBytes += m.OffHeapBytes / n
		g.PopulateSeconds += m.PopulateSeconds / n
		g.ForcedGCms += m.ForcedGCms / n
		if r.GoVersion != "" && !slices.Contains(g.GoVersions, r.GoVersion) {
			g.GoVersions = append(g.GoVersions, r.GoVersion)
		}
		if r.GoarenaVersion != "" {
			g.GoarenaVersion = r.GoarenaVersion
		}
	}
	if len(recs) > 1 {
		var ss float64
		for _, r := range recs {
			d := r.m.OpsPerSec - g.OpsPerSec
			ss += d * d
		}
		g.OpsPerSecStd = math.Sqrt(ss / (n - 1))
	}
	return g
}

// Scenario selects comparable historical runs across versions.
type Scenario struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Exps        []string `json:"experiments"` // accepted catalogue labels
	N           int      `json:"n"`
	ValueSize   int      `json:"value_size"`
	Goroutines  int      `json:"goroutines"`
	Dist        string   `json:"dist"`
	ReadRatio   float64  `json:"read_ratio"`
	GarbageRate float64  `json:"garbage_rate"`
	// MemLimitMiB: 0 = no limit, -1 = any limit > 0.
	MemLimitMiB int `json:"memlimit_mib"`
}

var scenarios = []Scenario{
	{ID: "e1-10m", Name: "E1 · N = 10M", Description: "baseline mix at 10M entries (E1; E1r in v0.3)",
		Exps: []string{"E1", "E1r"}, N: 10_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9},
	{ID: "e1-5m", Name: "E1 · N = 5M", Description: "baseline mix at 5M entries",
		Exps: []string{"E1", "E1r"}, N: 5_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9},
	{ID: "e1-1m", Name: "E1 · N = 1M", Description: "baseline mix at 1M entries",
		Exps: []string{"E1", "E1r"}, N: 1_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9},
	{ID: "e6-zipf", Name: "E6 · Zipf hot keys", Description: "Zipf s = 1.1, 90 % reads, N = 5M",
		Exps: []string{"E6"}, N: 5_000_000, ValueSize: 128, Goroutines: 16, Dist: "zipf", ReadRatio: 0.9},
	{ID: "e6-write", Name: "E6 · write-heavy", Description: "uniform keys, 50 % writes, N = 5M",
		Exps: []string{"E6"}, N: 5_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.5},
	{ID: "e9-5m", Name: "E9 · request garbage, 5M", Description: "1 KiB request object per operation, N = 5M",
		Exps: []string{"E9"}, N: 5_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9, GarbageRate: 1},
	{ID: "e9-10m", Name: "E9 · request garbage, 10M", Description: "1 KiB request object per operation, N = 10M",
		Exps: []string{"E9"}, N: 10_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9, GarbageRate: 1},
	{ID: "e10", Name: "E10 · memory budget", Description: "E9 workload at N = 5M with GOMEMLIMIT",
		Exps: []string{"E10"}, N: 5_000_000, ValueSize: 128, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9, GarbageRate: 1, MemLimitMiB: -1},
	{ID: "e4-1k", Name: "E4 · 1 KiB values", Description: "value size 1024 B at N = 2M",
		Exps: []string{"E4", "E4r"}, N: 2_000_000, ValueSize: 1024, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9},
	{ID: "e4-4k", Name: "E4 · 4 KiB values", Description: "value size 4096 B at N = 2M",
		Exps: []string{"E4", "E4r"}, N: 2_000_000, ValueSize: 4096, Goroutines: 16, Dist: "uniform", ReadRatio: 0.9},
	{ID: "e5-g1", Name: "E5 · 1 goroutine", Description: "single goroutine at N = 2M",
		Exps: []string{"E5"}, N: 2_000_000, ValueSize: 128, Goroutines: 1, Dist: "uniform", ReadRatio: 0.9},
}

func scenarioByID(id string) (Scenario, bool) {
	for _, s := range scenarios {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// catalogueExpRe matches the experiment labels of the harness catalogue.
var catalogueExpRe = regexp.MustCompile(`^E[0-9]+[a-z]*$`)

// matches reports whether a record belongs to the scenario. Records with a
// catalogue label outside Exps (e.g. E7 runs with gctrace at the same
// parameters as E1) are excluded; records with a custom label (demo runs,
// lab sessions) are included when the parameters match.
func (s Scenario) matches(r histRecord) bool {
	if catalogueExpRe.MatchString(r.Experiment) && len(s.Exps) > 0 && !slices.Contains(s.Exps, r.Experiment) {
		return false
	}
	if r.GCTrace {
		return false
	}
	switch {
	case r.N != s.N, r.ValueSize != s.ValueSize, r.Goroutines != s.Goroutines, r.Dist != s.Dist:
		return false
	case math.Abs(r.ReadRatio-s.ReadRatio) > 1e-9, math.Abs(r.GarbageRate-s.GarbageRate) > 1e-9:
		return false
	case s.MemLimitMiB == -1 && r.MemLimitMiB <= 0, s.MemLimitMiB >= 0 && r.MemLimitMiB != s.MemLimitMiB:
		return false
	}
	return true
}

// Comparison is the version comparison of one scenario.
type Comparison struct {
	Scenario   Scenario                         `json:"scenario"`
	Versions   []string                         `json:"versions"`
	Impls      []string                         `json:"impls"`
	Cells      map[string]map[string]*HistGroup `json:"cells"` // impl -> version -> group (absent = not measured)
	GoVersions map[string]string                `json:"go_versions"`
	MemLimits  map[string]int                   `json:"memlimits,omitempty"`
}

// compare aggregates the records matching the scenario per (version, impl).
func (idx *HistIndex) compare(s Scenario) *Comparison {
	c := &Comparison{Scenario: s, Versions: versionOrder, Impls: []string{},
		Cells: map[string]map[string]*HistGroup{}, GoVersions: map[string]string{}, MemLimits: map[string]int{}}
	type vk struct{ version, impl string }
	groups := map[vk][]histRecord{}
	for _, r := range idx.records {
		if !s.matches(r) {
			continue
		}
		groups[vk{r.Version, r.Impl}] = append(groups[vk{r.Version, r.Impl}], r)
		if !slices.Contains(c.Impls, r.Impl) {
			c.Impls = append(c.Impls, r.Impl)
		}
		if r.MemLimitMiB > 0 {
			c.MemLimits[r.Version] = r.MemLimitMiB
		}
	}
	sort.SliceStable(c.Impls, func(i, j int) bool { return implRank(c.Impls[i]) < implRank(c.Impls[j]) })
	for k, recs := range groups {
		g := aggregate(recs)
		g.Version, g.Impl = k.version, k.impl
		f := recs[0]
		g.Experiment, g.N, g.ValueSize, g.Goroutines, g.Dist = f.Experiment, f.N, f.ValueSize, f.Goroutines, f.Dist
		g.ReadRatio, g.GarbageRate, g.MemLimitMiB = f.ReadRatio, f.GarbageRate, f.MemLimitMiB
		if c.Cells[k.impl] == nil {
			c.Cells[k.impl] = map[string]*HistGroup{}
		}
		c.Cells[k.impl][k.version] = &g
		if gv := strings.Join(g.GoVersions, ", "); gv != "" {
			if prev := c.GoVersions[k.version]; prev == "" {
				c.GoVersions[k.version] = gv
			} else if !strings.Contains(prev, gv) {
				c.GoVersions[k.version] = prev + ", " + gv
			}
		}
	}
	return c
}

// marshalComparison is used by the JSON export.
func marshalComparison(c *Comparison) ([]byte, error) { return json.MarshalIndent(c, "", "  ") }
