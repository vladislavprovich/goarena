package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/vladislavprovich/goarena/gcpace"
	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/internal/latency"
	"github.com/vladislavprovich/goarena/internal/tsc"
	"github.com/vladislavprovich/goarena/internal/version"
	"github.com/vladislavprovich/goarena/store"
)

// Result is the JSON document written for every benchmark run.
type Result struct {
	Options options `json:"options"`

	// environment
	GoVersion  string  `json:"go_version"`
	GOOS       string  `json:"goos"`
	GOARCH     string  `json:"goarch"`
	GOMAXPROCS int     `json:"gomaxprocs"`
	NumCPU     int     `json:"num_cpu"`
	GOGC       int     `json:"gogc"`
	Hostname   string  `json:"hostname"`
	StartedAt  string  `json:"started_at"`
	TSC        bool    `json:"tsc_clock"`
	NsPerTick  float64 `json:"ns_per_tick"`

	// populate phase
	PopulateSeconds   float64        `json:"populate_seconds"`
	PopulateOpsPerSec float64        `json:"populate_ops_per_sec"`
	PopulateGC        gcstats.Delta  `json:"populate_gc"`
	PopulateCgoCalls  int64          `json:"populate_cgo_calls"`
	AfterPopulate     gcstats.Gauges `json:"after_populate"`

	// measured phase
	MeasuredSeconds  float64          `json:"measured_seconds"`
	Ops              uint64           `json:"ops"`
	Reads            uint64           `json:"reads"`
	Writes           uint64           `json:"writes"`
	Misses           uint64           `json:"misses"`
	OpsPerSec        float64          `json:"ops_per_sec"`
	Latency          latency.Summary  `json:"latency"`
	ReadLatency      latency.Summary  `json:"read_latency"`
	WriteLatency     latency.Summary  `json:"write_latency"`
	LatencyBuckets   []latency.Bucket `json:"latency_buckets"`
	GC               gcstats.Delta    `json:"gc"`
	GCCPUPercent     float64          `json:"gc_cpu_percent"`
	After            gcstats.Gauges   `json:"after"`
	MeasuredCgoCalls int64            `json:"measured_cgo_calls"`
	ForcedGCms       float64          `json:"forced_gc_ms"`              // wall time of runtime.GC() after the run
	GoMemLimitMiB    int64            `json:"go_memlimit_mib,omitempty"` // goarena-budget: Go limit after populate
	Pacer            *gcpace.Stats    `json:"pacer,omitempty"`           // goarena-paced: last pacer adjustment
	Version          string           `json:"goarena_version"`
	OffHeapBytes     int64            `json:"offheap_bytes"`
	CmemLiveBytes    int64            `json:"cmem_live_bytes"`
	Store            *store.Stats     `json:"store,omitempty"`

	trace *gcTrace // parsed gctrace side file (aggregation only)
}

func newResult(o options) *Result {
	h, _ := os.Hostname()
	return &Result{
		Options:    o,
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		NumCPU:     runtime.NumCPU(),
		Hostname:   h,
		StartedAt:  time.Now().Format(time.RFC3339),
		TSC:        tsc.Available(),
		NsPerTick:  tsc.NsPerTick(),
		Version:    version.Module,
	}
}

// currentGOGC returns the GC percent in effect (reading it requires
// setting it, so the value is restored immediately).
func currentGOGC() int {
	v := debug.SetGCPercent(100)
	debug.SetGCPercent(v)
	return v
}

func itoa(n int) string { return strconv.Itoa(n) }
