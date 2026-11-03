package main

import (
	"encoding/json"
	"fmt"

	"github.com/vladislavprovich/goarena/internal/latency"
)

// benchOptions is the "options" object of a goarena-bench result. Fields
// added after v0.3 (profile, maxmem_mib, fill_on_miss) are optional.
type benchOptions struct {
	Impl         string  `json:"impl"`
	N            int     `json:"n"`
	ValueSize    int     `json:"value_size"`
	Goroutines   int     `json:"goroutines"`
	ReadRatio    float64 `json:"read_ratio"`
	Dist         string  `json:"dist"`
	ZipfS        float64 `json:"zipf_s"`
	DurationNs   int64   `json:"duration_ns"`
	WarmupNs     int64   `json:"warmup_ns"`
	Experiment   string  `json:"experiment"`
	Repeat       int     `json:"repeat"`
	MemLimitMiB  int     `json:"memlimit_mib"`
	GCTrace      bool    `json:"gctrace"`
	GarbageRate  float64 `json:"garbage_rate"`
	GarbageBytes int     `json:"garbage_bytes"`
	Profile      string  `json:"profile"`
	MaxMemMiB    int     `json:"maxmem_mib"`
	FillOnMiss   bool    `json:"fill_on_miss"`
}

// benchGauges is a subset of gcstats.Gauges.
type benchGauges struct {
	HeapObjectsBytes float64 `json:"heap_objects_bytes"`
	HeapGoalBytes    float64 `json:"heap_goal_bytes"`
	RSSBytes         float64 `json:"rss_bytes"`
	PeakRSSBytes     float64 `json:"peak_rss_bytes"`
	PrivateBytes     float64 `json:"private_bytes"`
}

// benchResult is the subset of the harness Result JSON (result_format.go)
// the lab reads. Stages and the timeline are decoded separately and
// leniently, so a type change there never loses the main metrics.
type benchResult struct {
	Options         benchOptions     `json:"options"`
	GoVersion       string           `json:"go_version"`
	GOMAXPROCS      int              `json:"gomaxprocs"`
	NumCPU          int              `json:"num_cpu"`
	Hostname        string           `json:"hostname"`
	StartedAt       string           `json:"started_at"`
	PopulateSeconds float64          `json:"populate_seconds"`
	MeasuredSeconds float64          `json:"measured_seconds"`
	Ops             float64          `json:"ops"`
	Reads           float64          `json:"reads"`
	Writes          float64          `json:"writes"`
	Misses          float64          `json:"misses"`
	OpsPerSec       float64          `json:"ops_per_sec"`
	Latency         latency.Summary  `json:"latency"`
	LatencyBuckets  []latency.Bucket `json:"latency_buckets"`
	GC              struct {
		GCCycles     float64 `json:"gc_cycles"`
		PauseTotalNs float64 `json:"pause_total_ns"`
	} `json:"gc"`
	GCCPUPercent float64         `json:"gc_cpu_percent"`
	After        benchGauges     `json:"after"`
	ForcedGCms   float64         `json:"forced_gc_ms"`
	Version      string          `json:"goarena_version"`
	OffHeapBytes float64         `json:"offheap_bytes"`
	WriteErrors  *float64        `json:"write_errors"`
	Evictions    *float64        `json:"evictions"`
	Stages       json.RawMessage `json:"stages"`
	Timeline     json.RawMessage `json:"timeline"`

	stages   []Stage    // decoded Stages
	timeline []Progress // decoded Timeline
}

// Stage is one stage of a load profile in the result ("stages").
type Stage struct {
	Name         string          `json:"name"`
	Index        float64         `json:"index"`
	Goroutines   float64         `json:"goroutines"`
	ReadRatio    float64         `json:"read_ratio"`
	GarbageRate  float64         `json:"garbage_rate"`
	StartS       float64         `json:"start_s"`
	Seconds      float64         `json:"seconds"`
	Ops          float64         `json:"ops"`
	OpsPerSec    float64         `json:"ops_per_sec"`
	Misses       float64         `json:"misses"`
	Latency      latency.Summary `json:"latency"`
	GCCycles     float64         `json:"gc_cycles"`
	GCCPUPercent float64         `json:"gc_cpu_percent"`
}

// parseBenchResult decodes a result file.
func parseBenchResult(b []byte) (*benchResult, error) {
	var r benchResult
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("invalid result JSON: %v", err)
	}
	if len(r.Stages) > 0 {
		if json.Unmarshal(r.Stages, &r.stages) != nil {
			r.stages = nil
		}
	}
	if len(r.Timeline) > 0 {
		if json.Unmarshal(r.Timeline, &r.timeline) != nil {
			r.timeline = nil
		}
	}
	return &r, nil
}

// RunSummary holds the metrics of one run shown in tables, CSV and knees.
type RunSummary struct {
	Impl            string   `json:"impl"`
	OpsPerSec       float64  `json:"ops_per_sec"`
	MeanNs          float64  `json:"mean_ns"`
	P50Ns           float64  `json:"p50_ns"`
	P90Ns           float64  `json:"p90_ns"`
	P99Ns           float64  `json:"p99_ns"`
	P999Ns          float64  `json:"p999_ns"`
	MaxNs           float64  `json:"max_ns"`
	GCCycles        float64  `json:"gc_cycles"`
	GCCyclesPerSec  float64  `json:"gc_cycles_per_sec"`
	GCCPUPercent    float64  `json:"gc_cpu_percent"`
	HeapBytes       float64  `json:"heap_bytes"`
	HeapGoalBytes   float64  `json:"heap_goal_bytes"`
	RSSBytes        float64  `json:"rss_bytes"`
	PeakRSSBytes    float64  `json:"peak_rss_bytes"`
	OffHeapBytes    float64  `json:"offheap_bytes"`
	PopulateSeconds float64  `json:"populate_seconds"`
	ForcedGCms      float64  `json:"forced_gc_ms"`
	MeasuredSeconds float64  `json:"measured_seconds"`
	Ops             float64  `json:"ops"`
	Reads           float64  `json:"reads"`
	Writes          float64  `json:"writes"`
	Misses          float64  `json:"misses"`
	HitRatio        *float64 `json:"hit_ratio,omitempty"`
	Evictions       *float64 `json:"evictions,omitempty"`
	WriteErrors     *float64 `json:"write_errors,omitempty"`
	GoVersion       string   `json:"go_version,omitempty"`
	GoarenaVersion  string   `json:"goarena_version,omitempty"`
	Stages          []Stage  `json:"stages,omitempty"`
}

// summarize extracts the run summary of a result.
func summarize(r *benchResult) *RunSummary {
	s := &RunSummary{
		Impl:            r.Options.Impl,
		OpsPerSec:       r.OpsPerSec,
		MeanNs:          r.Latency.Mean,
		P50Ns:           float64(r.Latency.P50),
		P90Ns:           float64(r.Latency.P90),
		P99Ns:           float64(r.Latency.P99),
		P999Ns:          float64(r.Latency.P999),
		MaxNs:           float64(r.Latency.Max),
		GCCycles:        r.GC.GCCycles,
		GCCPUPercent:    r.GCCPUPercent,
		HeapBytes:       r.After.HeapObjectsBytes,
		HeapGoalBytes:   r.After.HeapGoalBytes,
		RSSBytes:        r.After.RSSBytes,
		PeakRSSBytes:    r.After.PeakRSSBytes,
		OffHeapBytes:    r.OffHeapBytes,
		PopulateSeconds: r.PopulateSeconds,
		ForcedGCms:      r.ForcedGCms,
		MeasuredSeconds: r.MeasuredSeconds,
		Ops:             r.Ops,
		Reads:           r.Reads,
		Writes:          r.Writes,
		Misses:          r.Misses,
		Evictions:       r.Evictions,
		WriteErrors:     r.WriteErrors,
		GoVersion:       r.GoVersion,
		GoarenaVersion:  r.Version,
		Stages:          r.stages,
	}
	if r.MeasuredSeconds > 0 {
		s.GCCyclesPerSec = r.GC.GCCycles / r.MeasuredSeconds
	}
	if r.Reads > 0 {
		h := 1 - r.Misses/r.Reads
		if h < 0 {
			h = 0
		}
		s.HitRatio = &h
	}
	return s
}

// stripTimeline removes the "timeline" member of a result document (the
// lab stores the timeline once, in the run record).
func stripTimeline(raw []byte) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return json.RawMessage(raw)
	}
	if _, ok := m["timeline"]; !ok {
		return json.RawMessage(raw)
	}
	delete(m, "timeline")
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(raw)
	}
	return b
}
