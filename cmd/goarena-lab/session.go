package main

import (
	"encoding/json"
	"slices"
	"time"
)

// Run and session states.
const (
	statePending   = "pending"
	stateRunning   = "running"
	stateDone      = "done"
	stateFailed    = "failed"
	stateCancelled = "cancelled"
	stateSkipped   = "skipped"
)

// labFormatVersion is stored in every history file.
const labFormatVersion = 1

// SweepSpec is the swept parameter of a sweep session.
type SweepSpec struct {
	Param  string    `json:"param"`
	Values []float64 `json:"values"`
}

// SessionBench records which harness binary produced a session.
type SessionBench struct {
	Path      string `json:"path"`
	Cgo       string `json:"cgo"`
	GoVersion string `json:"go_version,omitempty"`
}

// Session is a run or sweep session: the configuration, the machine, and
// one record per harness process. It is also the history file format
// (results/lab/<id>.json).
type Session struct {
	ID         string       `json:"id"`
	LabFormat  int          `json:"lab_format"`
	Kind       string       `json:"kind"` // run | sweep
	Preset     string       `json:"preset,omitempty"`
	Title      string       `json:"title"`
	State      string       `json:"state"`
	Error      string       `json:"error,omitempty"`
	Saved      bool         `json:"saved"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at,omitzero"`
	Config     RunConfig    `json:"config"`
	Sweep      *SweepSpec   `json:"sweep,omitempty"`
	Machine    MachineInfo  `json:"machine"`
	Bench      SessionBench `json:"bench"`
	Runs       []*RunRecord `json:"runs"`
	Knees      []Knee       `json:"knees,omitempty"`
}

// RunRecord is one harness process of a session.
type RunRecord struct {
	Index      int             `json:"index"`
	Impl       string          `json:"impl"`
	ParamValue *float64        `json:"param_value,omitempty"`
	Config     RunConfig       `json:"config"`
	Args       []string        `json:"args,omitempty"`
	State      string          `json:"state"`
	Error      string          `json:"error,omitempty"`
	StartedAt  time.Time       `json:"started_at,omitzero"`
	Seconds    float64         `json:"seconds,omitempty"`
	Summary    *RunSummary     `json:"summary,omitempty"`
	Last       *Progress       `json:"last,omitempty"`
	LogTail    []string        `json:"log_tail,omitempty"`
	Timeline   []Progress      `json:"timeline,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// snapshot copies the session. Without full, the heavy members of the runs
// (timeline, raw result, log tail) are left out. The caller holds the lock
// that guards s; the copy may be read without it (slices are only ever
// appended to or replaced).
func (s *Session) snapshot(full bool) *Session {
	c := *s
	c.Config.Impls = slices.Clone(s.Config.Impls)
	c.Knees = slices.Clone(s.Knees)
	if s.Sweep != nil {
		sw := *s.Sweep
		c.Sweep = &sw
	}
	c.Runs = make([]*RunRecord, len(s.Runs))
	for i, r := range s.Runs {
		rc := *r
		rc.LogTail = slices.Clone(r.LogTail)
		if !full {
			rc.Timeline, rc.Result, rc.LogTail = nil, nil, nil
		}
		c.Runs[i] = &rc
	}
	return &c
}

// hasResults reports whether at least one run produced a result.
func (s *Session) hasResults() bool {
	for _, r := range s.Runs {
		if r.Summary != nil {
			return true
		}
	}
	return false
}
