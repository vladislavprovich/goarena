package main

import (
	"bytes"
	"encoding/json"
	"sync"
)

// Progress is one per-second progress line of goarena-bench -progress.
// Numbers are float64 so that the parser tolerates integer and float
// encodings; unknown fields are ignored and missing ones stay zero.
type Progress struct {
	Type          string  `json:"type"`
	Impl          string  `json:"impl,omitempty"`
	Phase         string  `json:"phase"` // populate | warmup | measure
	Stage         string  `json:"stage"`
	StageIndex    float64 `json:"stage_index"`
	TS            float64 `json:"t_s"`
	PhaseTS       float64 `json:"phase_t_s"`
	IntervalS     float64 `json:"interval_s"`
	Goroutines    float64 `json:"goroutines"`
	ReadRatio     float64 `json:"read_ratio"`
	GarbageRate   float64 `json:"garbage_rate"`
	Ops           float64 `json:"ops"`
	OpsPerSec     float64 `json:"ops_per_sec"`
	Misses        float64 `json:"misses"`
	HitRatio      float64 `json:"hit_ratio"`
	Samples       float64 `json:"samples"`
	P50Ns         float64 `json:"p50_ns"`
	P99Ns         float64 `json:"p99_ns"`
	P999Ns        float64 `json:"p999_ns"`
	MaxNs         float64 `json:"max_ns"`
	GCCycles      float64 `json:"gc_cycles"`
	GCCyclesTotal float64 `json:"gc_cycles_total"`
	GCCPUPercent  float64 `json:"gc_cpu_percent"`
	StwMs         float64 `json:"stw_ms"` // stop-the-world time in the interval
	HeapBytes     float64 `json:"heap_bytes"`
	HeapGoalBytes float64 `json:"heap_goal_bytes"`
	RSSBytes      float64 `json:"rss_bytes"`
	OffHeapBytes  float64 `json:"offheap_bytes"`
	Populated     float64 `json:"populated"`
	N             float64 `json:"n"`
	Evictions     float64 `json:"evictions,omitempty"`
}

var progressTag = []byte(`"progress"`)

// parseProgressLine parses a stderr line of the harness. It reports false
// for every line that is not a JSON progress object (summary lines, errors,
// malformed JSON); such lines are passed through to the UI log.
func parseProgressLine(line []byte) (Progress, bool) {
	line = bytes.TrimSpace(line)
	if len(line) < 2 || line[0] != '{' || line[len(line)-1] != '}' || !bytes.Contains(line, progressTag) {
		return Progress{}, false
	}
	var p Progress
	if err := json.Unmarshal(line, &p); err != nil || p.Type != "progress" {
		return Progress{}, false
	}
	return p, true
}

// maxLineBytes bounds one buffered output line of a child process.
const maxLineBytes = 1 << 20

// lineWriter is an io.Writer that splits a child's output stream into
// lines (without the trailing \r\n) and hands each line to fn. exec copies
// each stream from its own goroutine, so one lineWriter is used by one
// goroutine at a time; the mutex makes Flush after Wait safe as well.
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(line []byte)
}

func newLineWriter(fn func(line []byte)) *lineWriter { return &lineWriter{fn: fn} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLineBytes {
		w.emit(w.buf)
		w.buf = nil
	}
	if len(w.buf) == 0 {
		w.buf = nil // release the backing array between bursts
	}
	return len(p), nil
}

// Flush emits a final line without a newline.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(w.buf)
	}
	w.buf = nil
}

func (w *lineWriter) emit(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	w.fn(bytes.Clone(line))
}
