package main

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// ImplInfo describes an implementation the lab can run.
type ImplInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Color       string `json:"color"`
	Dashed      bool   `json:"dashed,omitempty"`
	GoHeap      bool   `json:"go_heap"`
	Description string `json:"description"`
}

// implCatalog is the whitelist of implementations the lab passes to the
// harness, in display order. Colors are those of the thesis charts.
var implCatalog = []ImplInfo{
	{
		Name: "gomap", Label: "gomap", Color: "#4C72B0", GoHeap: true,
		Description: "The textbook Go cache: 256 shards of map[string][]byte, each guarded by a sync.RWMutex, " +
			"GOGC=100. Every key and every value is a heap object the collector marks on each cycle " +
			"(about 20 million objects at N = 10M), so GC CPU, mark assists on the request path, tail latency " +
			"and RSS (about twice the live heap) grow with the number of entries.",
	},
	{
		Name: "gomap-gogc400", Label: "gomap GOGC=400", Color: "#7F9CCF", GoHeap: true,
		Description: "The same sharded map with debug.SetGCPercent(400): the usual first tuning step. " +
			"The collector runs less often, but the heap may grow to five times the live data " +
			"(7.2 GiB of Go heap at N = 10M) and each cycle still marks every entry.",
	},
	{
		Name: "syncmap", Label: "sync.Map", Color: "#8172B2", GoHeap: true,
		Description: "sync.Map with string keys and []byte values, the standard library's concurrent map. " +
			"It is optimised for keys written once and read many times; its internal boxing makes it the most " +
			"memory-hungry and GC-heavy baseline (6.5 GiB RSS and 15.7 % GC CPU at N = 10M).",
	},
	{
		Name: "gobytes", Label: "gobytes", Color: "#CCB974", GoHeap: true,
		Description: "A bigcache-style pointer-free Go-heap cache: a map[uint64]uint32 index per shard plus " +
			"large []byte chunks that hold the entries. The collector does not scan pointer-free memory, so GC " +
			"work stays small, but the data still counts towards the heap goal. The strongest pure-Go baseline.",
	},
	{
		Name: "goarena", Label: "goarena", Color: "#55A868",
		Description: "store.Store from this module with the default configuration: entries live in size-class " +
			"slabs inside 64 MiB arena chunks and the open-addressing hash index lives off-heap too, all in memory " +
			"obtained from C through cgo. The Go heap holds only a few MiB of bookkeeping, reads are lock-free and " +
			"allocation-free. Built without cgo the store falls back to a pure-Go mode (arena in the Go heap).",
	},
	{
		Name: "goarena-paced", Label: "goarena paced", Color: "#3B7D4C",
		Description: "goarena with store.Config.GCPacing = true. Every 50 ms the gcpace pacer sets " +
			"GOGC_eff = GOGC x (live + off-heap) / live, so the collector runs as often as it would for a " +
			"heap-resident store of the same size while each cycle still marks only the small Go heap; a " +
			"GOMEMLIMIT is treated as the budget for Go + off-heap memory. Recommended for services that " +
			"allocate per request (E9/E10).",
	},
	{
		Name: "goarena-evict", Label: "goarena evict", Color: "#8FD19E", Dashed: true,
		Description: "goarena as a bounded cache: store.Config.MaxMemory = max memory (MiB) with CLOCK " +
			"eviction, so instead of failing with ErrNoMemory the store evicts cold entries when the off-heap " +
			"budget is full. Use it with fill-on-miss (a miss writes the entry, like cache-aside) and a Zipf key " +
			"distribution to see the hit ratio a fixed memory budget buys.",
	},
}

// implByName returns the catalog entry for name.
func implByName(name string) (ImplInfo, bool) {
	for _, im := range implCatalog {
		if im.Name == name {
			return im, true
		}
	}
	return ImplInfo{}, false
}

// Load profiles supported by the harness (-profile).
var profiles = []string{"steady", "ramp", "spike", "sweep-goroutines"}

// Validation limits. They keep requests sane; the memory guard is the real
// protection against runs that do not fit the machine.
const (
	minN, maxN                 = 1000, 200_000_000
	minValue, maxValue         = 8, 1 << 20
	maxGoroutines              = 1024
	maxGarbageBytes            = 1 << 20
	maxMemLimitMiB             = 1 << 20
	minMaxMemMiB               = 16
	minDurationS, maxDurationS = 0.2, 3600
	maxWarmupS                 = 600
	maxSweepValues             = 16
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// RunConfig is the configuration of a run session (one harness process per
// selected implementation) and the base configuration of a sweep.
type RunConfig struct {
	Impls        []string `json:"impls"`
	N            int      `json:"n"`
	ValueSize    int      `json:"value_size"`
	ReadRatio    float64  `json:"read_ratio"`
	Goroutines   int      `json:"goroutines"`
	Dist         string   `json:"dist"`
	ZipfS        float64  `json:"zipf_s"`
	GarbageRate  float64  `json:"garbage_rate"`
	GarbageBytes int      `json:"garbage_bytes"`
	MemLimitMiB  int      `json:"memlimit_mib"`
	MaxMemMiB    int      `json:"maxmem_mib"`
	FillOnMiss   bool     `json:"fill_on_miss"`
	DurationS    float64  `json:"duration_s"`
	WarmupS      float64  `json:"warmup_s"`
	Profile      string   `json:"profile"`
	Preset       string   `json:"preset,omitempty"`
}

// normalize fills defaults for optional fields.
func (c *RunConfig) normalize() {
	if c.Dist == "" {
		c.Dist = "uniform"
	}
	if c.ZipfS == 0 {
		c.ZipfS = 1.1
	}
	if c.Profile == "" {
		c.Profile = "steady"
	}
	if c.GarbageBytes == 0 && c.GarbageRate > 0 {
		c.GarbageBytes = 1024
	}
}

// Validate checks every field against the whitelist and numeric ranges.
// Only validated values are ever passed to the harness.
func (c *RunConfig) Validate() error {
	c.normalize()
	if len(c.Impls) == 0 {
		return fmt.Errorf("select at least one implementation")
	}
	seen := map[string]bool{}
	for _, name := range c.Impls {
		if _, ok := implByName(name); !ok {
			return fmt.Errorf("unknown implementation %q", name)
		}
		if seen[name] {
			return fmt.Errorf("implementation %q selected twice", name)
		}
		seen[name] = true
	}
	for _, f := range []float64{c.ReadRatio, c.ZipfS, c.GarbageRate, c.DurationS, c.WarmupS} {
		if !finite(f) {
			return fmt.Errorf("numbers must be finite")
		}
	}
	switch {
	case c.N < minN || c.N > maxN:
		return fmt.Errorf("N must be in [%d, %d]", minN, maxN)
	case c.ValueSize < minValue || c.ValueSize > maxValue:
		return fmt.Errorf("value size must be in [%d, %d] bytes", minValue, maxValue)
	case c.ReadRatio < 0 || c.ReadRatio > 1:
		return fmt.Errorf("read ratio must be in [0, 1]")
	case c.Goroutines < 1 || c.Goroutines > maxGoroutines:
		return fmt.Errorf("goroutines must be in [1, %d]", maxGoroutines)
	case c.Dist != "uniform" && c.Dist != "zipf":
		return fmt.Errorf("distribution must be uniform or zipf")
	case c.Dist == "zipf" && (c.ZipfS <= 1 || c.ZipfS > 5):
		return fmt.Errorf("zipf s must be in (1, 5]")
	case c.GarbageRate < 0 || c.GarbageRate > 1:
		return fmt.Errorf("garbage rate must be in [0, 1]")
	case c.GarbageBytes < 0 || c.GarbageBytes > maxGarbageBytes:
		return fmt.Errorf("garbage bytes must be in [0, %d]", maxGarbageBytes)
	case c.MemLimitMiB < 0 || c.MemLimitMiB > maxMemLimitMiB:
		return fmt.Errorf("memory limit must be in [0, %d] MiB", maxMemLimitMiB)
	case c.MaxMemMiB < 0 || c.MaxMemMiB > maxMemLimitMiB:
		return fmt.Errorf("max memory must be in [0, %d] MiB", maxMemLimitMiB)
	case seen["goarena-evict"] && c.MaxMemMiB < minMaxMemMiB:
		return fmt.Errorf("goarena-evict needs max memory >= %d MiB", minMaxMemMiB)
	case c.DurationS < minDurationS || c.DurationS > maxDurationS:
		return fmt.Errorf("duration must be in [%g, %g] s", float64(minDurationS), float64(maxDurationS))
	case c.WarmupS < 0 || c.WarmupS > maxWarmupS:
		return fmt.Errorf("warm-up must be in [0, %d] s", maxWarmupS)
	case !slices.Contains(profiles, c.Profile):
		return fmt.Errorf("load profile must be one of %v", profiles)
	case c.Preset != "" && !slugRe.MatchString(c.Preset):
		return fmt.Errorf("invalid preset id")
	}
	return nil
}

// usesImpl reports whether the configuration selects impl.
func (c RunConfig) usesImpl(impl string) bool { return slices.Contains(c.Impls, impl) }

// seconds converts a float number of seconds into a duration flag value.
func seconds(s float64) string {
	return (time.Duration(math.Round(s*1000)) * time.Millisecond).String()
}

// benchArgs returns the goarena-bench command line of one run of impl.
// Flags beyond the original harness are passed only when they matter, and
// only when the binary supports them (caps).
func (c RunConfig) benchArgs(impl, outFile string, caps benchCaps) ([]string, error) {
	if !caps.hasImpl(impl) {
		return nil, fmt.Errorf("the goarena-bench binary does not know implementation %q (rebuild it)", impl)
	}
	exp := "lab"
	if c.Preset != "" {
		exp = "lab-" + c.Preset
	}
	args := []string{"run",
		"-impl", impl,
		"-n", strconv.Itoa(c.N),
		"-value", strconv.Itoa(c.ValueSize),
		"-goroutines", strconv.Itoa(c.Goroutines),
		"-reads", formatFloatArg(c.ReadRatio),
		"-dist", c.Dist,
	}
	if c.Dist == "zipf" {
		args = append(args, "-zipf-s", formatFloatArg(c.ZipfS))
	}
	args = append(args,
		"-duration", seconds(c.DurationS),
		"-warmup", seconds(c.WarmupS),
		"-garbage-rate", formatFloatArg(c.GarbageRate),
		"-garbage-bytes", strconv.Itoa(c.GarbageBytes),
	)
	if c.MemLimitMiB > 0 {
		args = append(args, "-memlimit", strconv.Itoa(c.MemLimitMiB))
	}
	need := func(flag, what string) error {
		if !caps.has(flag) {
			return fmt.Errorf("%s needs flag -%s, which this goarena-bench binary does not support (rebuild it)", what, flag)
		}
		return nil
	}
	if impl == "goarena-evict" {
		if err := need("maxmem", "goarena-evict"); err != nil {
			return nil, err
		}
		args = append(args, "-maxmem", strconv.Itoa(c.MaxMemMiB))
	}
	if c.FillOnMiss {
		if err := need("fill-on-miss", "fill-on-miss"); err != nil {
			return nil, err
		}
		args = append(args, "-fill-on-miss")
	}
	if c.Profile != "steady" {
		if err := need("profile", "load profile "+c.Profile); err != nil {
			return nil, err
		}
		args = append(args, "-profile", c.Profile)
	}
	if caps.has("progress") {
		args = append(args, "-progress")
	}
	args = append(args, "-exp", exp, "-out", outFile)
	return args, nil
}

// SweepConfig is a parameter sweep: every (value x implementation) pair is
// one harness process, values in the outer loop.
type SweepConfig struct {
	Base   RunConfig `json:"base"`
	Param  string    `json:"param"`
	Values []float64 `json:"values"`
}

// sweepParam describes a parameter that can be swept.
type sweepParam struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Integer    bool   `json:"integer"`
	ExpectRise bool   `json:"expect_rise"` // throughput should grow with the value (goroutines)
}

var sweepParams = []sweepParam{
	{Name: "goroutines", Label: "goroutines", Integer: true, ExpectRise: true},
	{Name: "n", Label: "entries N", Integer: true},
	{Name: "value_size", Label: "value size", Integer: true},
	{Name: "read_ratio", Label: "read ratio"},
	{Name: "garbage_rate", Label: "garbage rate"},
}

func sweepParamByName(name string) (sweepParam, bool) {
	for _, p := range sweepParams {
		if p.Name == name {
			return p, true
		}
	}
	return sweepParam{}, false
}

// apply returns the base configuration with the swept parameter set to v.
func (s SweepConfig) apply(v float64) RunConfig {
	c := s.Base
	c.Impls = slices.Clone(s.Base.Impls)
	switch s.Param {
	case "goroutines":
		c.Goroutines = int(v)
	case "n":
		c.N = int(v)
	case "value_size":
		c.ValueSize = int(v)
	case "read_ratio":
		c.ReadRatio = v
	case "garbage_rate":
		c.GarbageRate = v
		if v > 0 && c.GarbageBytes == 0 {
			c.GarbageBytes = 1024
		}
	}
	return c
}

// Validate checks the sweep; values are sorted ascending.
func (s *SweepConfig) Validate() error {
	p, ok := sweepParamByName(s.Param)
	if !ok {
		return fmt.Errorf("unknown sweep parameter %q", s.Param)
	}
	if len(s.Values) < 2 || len(s.Values) > maxSweepValues {
		return fmt.Errorf("a sweep needs 2..%d values", maxSweepValues)
	}
	s.Base.normalize()
	if s.Base.Profile != "steady" {
		return fmt.Errorf("sweeps use the steady load profile")
	}
	if err := s.Base.Validate(); err != nil {
		return err
	}
	vals := slices.Clone(s.Values)
	slices.Sort(vals)
	for i, v := range vals {
		if !finite(v) {
			return fmt.Errorf("sweep values must be finite")
		}
		if p.Integer && v != math.Trunc(v) {
			return fmt.Errorf("%s values must be integers", p.Label)
		}
		if i > 0 && vals[i-1] == v {
			return fmt.Errorf("duplicate sweep value %g", v)
		}
		c := s.apply(v)
		if err := c.Validate(); err != nil {
			return fmt.Errorf("value %g: %v", v, err)
		}
	}
	s.Values = vals
	return nil
}

// plannedRun is one harness process of a session.
type plannedRun struct {
	Impl   string
	Param  *float64
	Config RunConfig
}

// planRun returns the runs of a run session, in the order selected.
func planRun(c RunConfig) []plannedRun {
	out := make([]plannedRun, 0, len(c.Impls))
	for _, impl := range c.Impls {
		rc := c
		rc.Impls = []string{impl}
		out = append(out, plannedRun{Impl: impl, Config: rc})
	}
	return out
}

// planSweep returns the runs of a sweep: values outer, implementations inner.
func planSweep(s SweepConfig) []plannedRun {
	var out []plannedRun
	for _, v := range s.Values {
		c := s.apply(v)
		for _, impl := range c.Impls {
			rc := c
			rc.Impls = []string{impl}
			val := v
			out = append(out, plannedRun{Impl: impl, Param: &val, Config: rc})
		}
	}
	return out
}
