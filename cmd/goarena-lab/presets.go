package main

import "slices"

// Preset is a one-click scenario. Run presets fill the run form; sweep
// presets switch the UI to the Sweep page.
type Preset struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"` // run | sweep
	Experiment  string    `json:"experiment,omitempty"`
	Config      RunConfig `json:"config"`
	SweepParam  string    `json:"sweep_param,omitempty"`
	SweepValues []float64 `json:"sweep_values,omitempty"`
}

var (
	allBaselines = []string{"gomap", "gomap-gogc400", "syncmap", "gobytes", "goarena", "goarena-paced"}
	garbageSet   = []string{"gomap", "syncmap", "gobytes", "goarena", "goarena-paced"}
)

// baseConfig is the common E1-style configuration of the presets.
func baseConfig(id string, impls []string, n int) RunConfig {
	return RunConfig{
		Impls: slices.Clone(impls), N: n, ValueSize: 128, ReadRatio: 0.9, Goroutines: 16,
		Dist: "uniform", ZipfS: 1.1, GarbageBytes: 1024, DurationS: 10, WarmupS: 2,
		Profile: "steady", Preset: id,
	}
}

// presets returns the scenario presets.
func presets() []Preset {
	p := []Preset{}
	add := func(pr Preset) { p = append(p, pr) }

	add(Preset{ID: "baseline-10m", Name: "Baseline 10M", Kind: "run", Experiment: "E1",
		Description: "All implementations at N = 10M, 128-byte values, 90 % reads, 16 goroutines, uniform keys.",
		Config:      baseConfig("baseline-10m", allBaselines, 10_000_000)})

	add(Preset{ID: "large-20m", Name: "Large scale 20M", Kind: "run", Experiment: "E8",
		Description: "gomap, gobytes and goarena at N = 20M: GC and memory cost of a large heap.",
		Config:      baseConfig("large-20m", []string{"gomap", "gobytes", "goarena"}, 20_000_000)})

	c := baseConfig("zipf-hot", allBaselines, 5_000_000)
	c.Dist, c.ZipfS = "zipf", 1.1
	add(Preset{ID: "zipf-hot", Name: "Hot keys (Zipf)", Kind: "run", Experiment: "E6",
		Description: "Skewed access: Zipf s = 1.1 over N = 5M keys, 90 % reads - contention on hot shards.",
		Config:      c})

	c = baseConfig("write-heavy", allBaselines, 5_000_000)
	c.ReadRatio = 0.5
	add(Preset{ID: "write-heavy", Name: "Write-heavy 50 %", Kind: "run", Experiment: "E6",
		Description: "Half of the operations overwrite values (N = 5M): allocation and free-list pressure.",
		Config:      c})

	c = baseConfig("request-garbage", garbageSet, 5_000_000)
	c.GarbageRate, c.GarbageBytes = 1, 1024
	add(Preset{ID: "request-garbage", Name: "Service with request garbage", Kind: "run", Experiment: "E9",
		Description: "Every operation also allocates a 1 KiB short-lived request object, so the GC runs continuously (N = 5M).",
		Config:      c})

	c = baseConfig("memory-budget", garbageSet, 5_000_000)
	c.GarbageRate, c.GarbageBytes, c.MemLimitMiB = 1, 1024, 1210
	add(Preset{ID: "memory-budget", Name: "Container memory budget", Kind: "run", Experiment: "E10",
		Description: "The request-garbage workload under GOMEMLIMIT = 1210 MiB (1.25 x gobytes RSS), as in a container.",
		Config:      c})

	c = baseConfig("bounded-evict", []string{"goarena", "goarena-evict"}, 10_000_000)
	c.Dist, c.ZipfS, c.FillOnMiss, c.MaxMemMiB = "zipf", 1.1, true, 512
	add(Preset{ID: "bounded-evict", Name: "Bounded cache with eviction", Kind: "run",
		Description: "N = 10M Zipf keys with fill-on-miss: unbounded goarena versus goarena-evict limited to 512 MiB (CLOCK eviction).",
		Config:      c})

	c = baseConfig("load-ramp", allBaselines, 5_000_000)
	c.Profile, c.DurationS = "ramp", 16
	add(Preset{ID: "load-ramp", Name: "Load ramp", Kind: "run",
		Description: "The harness raises the number of active goroutines step by step during a 16 s measured phase (N = 5M).",
		Config:      c})

	c = baseConfig("spikes", allBaselines, 5_000_000)
	c.Profile, c.DurationS = "spike", 16
	add(Preset{ID: "spikes", Name: "Write / garbage spikes", Kind: "run",
		Description: "Steady reads interrupted by a write spike and a request-garbage burst (16 s, N = 5M).",
		Config:      c})

	c = baseConfig("goroutine-sweep", allBaselines, 2_000_000)
	c.DurationS = 5
	add(Preset{ID: "goroutine-sweep", Name: "Goroutine sweep", Kind: "sweep", Experiment: "E5",
		Description: "Throughput and tail latency with 1, 2, 4, 8, 12 and 16 goroutines at N = 2M (5 s per run).",
		Config:      c, SweepParam: "goroutines", SweepValues: []float64{1, 2, 4, 8, 12, 16}})

	c = baseConfig("value-size-sweep", []string{"gomap", "gobytes", "goarena"}, 1_000_000)
	add(Preset{ID: "value-size-sweep", Name: "Value-size sweep", Kind: "sweep", Experiment: "E4",
		Description: "Values of 64, 256, 1024 and 4096 bytes at N = 1M: copy cost and size-class waste.",
		Config:      c, SweepParam: "value_size", SweepValues: []float64{64, 256, 1024, 4096}})
	return p
}

// presetByID returns the preset with the given id.
func presetByID(id string) (Preset, bool) {
	for _, p := range presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
