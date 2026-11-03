package main

import (
	"fmt"
	"math"
)

const mib = 1 << 20

// memCoef is the memory model of one implementation used by the memory
// guard: projected RSS in MiB =
//
//	N x (Fixed + PerValueByte x valueSize) / MiB x (Garbage if garbage rate > 0) + 50 MiB
//
// The coefficients were fitted from the measured RSS in results/SUMMARY.md
// (E1: N = 1M..10M at 128 B, E4: value sizes 64 B..4 KiB at N = 2M, E9:
// request garbage at N = 5M and 10M) and rounded up, so the estimate errs
// on the safe side.
type memCoef struct {
	Fixed        float64 // bytes per entry independent of the value size
	PerValueByte float64 // bytes per value byte
	Garbage      float64 // multiplier when the workload allocates request garbage
	LimitCapped  bool    // a GOMEMLIMIT bounds the Go heap of this implementation
}

// memModel: fitted from results/SUMMARY.md E1/E4/E9 (see memCoef).
// goarena-paced is capped by a memory limit as well: its pacer treats
// GOMEMLIMIT as the budget for Go + off-heap memory (E10).
var memModel = map[string]memCoef{
	"gomap":         {Fixed: 266, PerValueByte: 1.98, Garbage: 1.5, LimitCapped: true},
	"gomap-gogc400": {Fixed: 140, PerValueByte: 5.0, Garbage: 3.4, LimitCapped: true},
	"syncmap":       {Fixed: 430, PerValueByte: 2.0, Garbage: 2.4, LimitCapped: true},
	"gobytes":       {Fixed: 96, PerValueByte: 1.01, Garbage: 2.05, LimitCapped: true},
	"goarena":       {Fixed: 73, PerValueByte: 1.06, Garbage: 1.05},
	"goarena-paced": {Fixed: 73, PerValueByte: 1.06, Garbage: 2.25, LimitCapped: true},
	"goarena-evict": {Fixed: 73, PerValueByte: 1.06, Garbage: 1.05}, // data capped at -maxmem
}

const (
	estimateBaseMiB   = 50   // runtime, binaries, workload generator
	fallbackBudgetMiB = 8192 // when the total RAM cannot be determined
	reservedMiB       = 6144 // left to the OS, the browser and the lab
)

// estimateMiB projects the peak RSS of one run of impl in MiB.
func estimateMiB(c RunConfig, impl string) float64 {
	m, ok := memModel[impl]
	if !ok {
		m = memModel["syncmap"] // most conservative
	}
	data := float64(c.N) * (m.Fixed + m.PerValueByte*float64(c.ValueSize)) / mib
	if impl == "goarena-evict" && c.MaxMemMiB > 0 {
		data = math.Min(data, float64(c.MaxMemMiB))
	}
	noGarbage := data + estimateBaseMiB
	est := noGarbage
	if c.GarbageRate > 0 {
		est = data*m.Garbage + estimateBaseMiB
	}
	if c.MemLimitMiB > 0 && m.LimitCapped {
		limitCap := math.Max(noGarbage*1.1, float64(c.MemLimitMiB)*1.15)
		est = math.Min(est, limitCap)
	}
	return est
}

// safeBudgetMiB is the largest projected RSS the lab accepts:
// min(75 % of the physical RAM, RAM - 6 GiB).
func safeBudgetMiB(totalRAM uint64) float64 {
	if totalRAM == 0 {
		return fallbackBudgetMiB
	}
	t := float64(totalRAM) / mib
	return math.Max(0, math.Min(0.75*t, t-reservedMiB))
}

// EstimateRun is the projection of one planned run.
type EstimateRun struct {
	Impl        string   `json:"impl"`
	ParamValue  *float64 `json:"param_value,omitempty"`
	N           int      `json:"n"`
	ValueSize   int      `json:"value_size"`
	EstimateMiB float64  `json:"estimate_mib"`
	OK          bool     `json:"ok"`
}

// EstimateReport is the memory-guard verdict for a session.
type EstimateReport struct {
	Valid       bool          `json:"valid"`
	Error       string        `json:"error,omitempty"`
	OK          bool          `json:"ok"`
	BudgetMiB   float64       `json:"budget_mib"`
	TotalRAMMiB float64       `json:"total_ram_mib"`
	MaxMiB      float64       `json:"max_mib"`
	Runs        []EstimateRun `json:"runs"`
	Message     string        `json:"message"`
}

// estimatePlan evaluates every planned run against the budget.
func estimatePlan(plan []plannedRun, totalRAM uint64) *EstimateReport {
	r := &EstimateReport{Valid: true, OK: true, BudgetMiB: safeBudgetMiB(totalRAM), TotalRAMMiB: float64(totalRAM) / mib}
	worst := ""
	for _, p := range plan {
		e := estimateMiB(p.Config, p.Impl)
		run := EstimateRun{Impl: p.Impl, ParamValue: p.Param, N: p.Config.N, ValueSize: p.Config.ValueSize,
			EstimateMiB: math.Round(e), OK: e <= r.BudgetMiB}
		r.Runs = append(r.Runs, run)
		if e > r.MaxMiB {
			r.MaxMiB = math.Round(e)
			worst = p.Impl
		}
		if !run.OK {
			r.OK = false
		}
	}
	if r.OK {
		r.Message = fmt.Sprintf("largest projected RSS %s (%s) within the safe budget of %s",
			fmtMiB(r.MaxMiB), worst, fmtMiB(r.BudgetMiB))
	} else {
		r.Message = fmt.Sprintf("projected RSS of %s is %s, above the safe budget of %s "+
			"(min(75 %% of RAM, RAM - 6 GiB)); reduce N or the value size, or deselect it",
			worst, fmtMiB(r.MaxMiB), fmtMiB(r.BudgetMiB))
	}
	return r
}

func fmtMiB(v float64) string {
	if v >= 1024 {
		return trimFloat(v/1024, 1) + " GiB"
	}
	return trimFloat(v, 0) + " MiB"
}
