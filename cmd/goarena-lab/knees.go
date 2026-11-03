package main

import (
	"fmt"
	"math"
	"slices"
)

// Knee detection thresholds (between consecutive sweep steps of one
// implementation).
const (
	kneeDropFraction = 0.15 // throughput falls by more than 15 %
	kneeScalingRatio = 0.5  // scaling efficiency below half of the previous step's
	kneeTailFactor   = 2.0  // p99.9 more than doubles
	kneeGCRisePoints = 10.0 // GC CPU rises by more than 10 percentage points
	kneeKindDrop     = "throughput-drop"
	kneeKindScaling  = "scaling-loss"
	kneeKindTail     = "tail-rise"
	kneeKindGC       = "gc-rise"
	kneeMetricOps    = "ops_per_sec"
	kneeMetricP999   = "p999_ns"
	kneeMetricGCCPU  = "gc_cpu_percent"
	idealEfficiency  = 1.0
)

// SweepPoint is the result of one (implementation, parameter value) run.
type SweepPoint struct {
	Impl         string  `json:"impl"`
	X            float64 `json:"x"`
	OpsPerSec    float64 `json:"ops_per_sec"`
	P99Ns        float64 `json:"p99_ns"`
	P999Ns       float64 `json:"p999_ns"`
	GCCPUPercent float64 `json:"gc_cpu_percent"`
	RSSBytes     float64 `json:"rss_bytes"`
}

// Knee is a detected drop between two consecutive steps of a sweep.
type Knee struct {
	Impl      string  `json:"impl"`
	Param     string  `json:"param"`
	Kind      string  `json:"kind"`
	Metric    string  `json:"metric"`
	FromX     float64 `json:"from_x"`
	ToX       float64 `json:"to_x"`
	FromValue float64 `json:"from_value"`
	ToValue   float64 `json:"to_value"`
	// Change is the relative change (-0.23 = -23 %) for throughput and
	// p99.9, the difference in percentage points for GC CPU and the
	// scaling efficiency of the step for scaling losses.
	Change  float64 `json:"change"`
	Message string  `json:"message"`
}

// detectKnees finds the steps where an implementation "drops" when the
// parameter grows. For each implementation the points are sorted by the
// parameter value and every consecutive pair (prev, cur) is checked:
//
//   - throughput-drop: throughput falls by more than 15 % versus prev;
//   - scaling-loss (only for parameters where throughput is expected to
//     rise, i.e. goroutines): the per-step scaling efficiency
//     e = (T_cur/T_prev) / (x_cur/x_prev) - 1.0 for perfectly linear
//     scaling - falls below half of the previous step's efficiency (the
//     first step is compared with the ideal 1.0). A drop that is already
//     reported as throughput-drop is not reported twice;
//   - tail-rise: p99.9 latency more than doubles;
//   - gc-rise: GC CPU rises by more than 10 percentage points.
func detectKnees(param string, pts []SweepPoint) []Knee {
	sp, _ := sweepParamByName(param)
	var order []string
	byImpl := map[string][]SweepPoint{}
	for _, p := range pts {
		if _, ok := byImpl[p.Impl]; !ok {
			order = append(order, p.Impl)
		}
		byImpl[p.Impl] = append(byImpl[p.Impl], p)
	}
	knees := []Knee{}
	for _, impl := range order {
		ps := slices.Clone(byImpl[impl])
		slices.SortStableFunc(ps, func(a, b SweepPoint) int {
			switch {
			case a.X < b.X:
				return -1
			case a.X > b.X:
				return 1
			}
			return 0
		})
		prevEff := idealEfficiency
		for i := 1; i < len(ps); i++ {
			prev, cur := ps[i-1], ps[i]
			if cur.X == prev.X {
				continue
			}
			mk := func(kind, metric string, from, to, change float64, msg string) Knee {
				return Knee{Impl: impl, Param: param, Kind: kind, Metric: metric, FromX: prev.X, ToX: cur.X,
					FromValue: from, ToValue: to, Change: change, Message: msg}
			}
			span := fmt.Sprintf("from %s to %s", paramLabel(param, prev.X), paramLabel(param, cur.X))
			dropped := false
			if prev.OpsPerSec > 0 && cur.OpsPerSec >= 0 {
				rel := (cur.OpsPerSec - prev.OpsPerSec) / prev.OpsPerSec
				if rel < -kneeDropFraction {
					dropped = true
					knees = append(knees, mk(kneeKindDrop, kneeMetricOps, prev.OpsPerSec, cur.OpsPerSec, rel,
						fmt.Sprintf("%s: throughput %s %s (%s -> %s Mops/s)", impl, signedPercent(rel), span,
							trimFloat(prev.OpsPerSec/1e6, 2), trimFloat(cur.OpsPerSec/1e6, 2))))
				}
				if sp.ExpectRise && prev.X > 0 && cur.X > prev.X {
					eff := (cur.OpsPerSec / prev.OpsPerSec) / (cur.X / prev.X)
					if !dropped && eff < kneeScalingRatio*prevEff {
						knees = append(knees, mk(kneeKindScaling, kneeMetricOps, prev.OpsPerSec, cur.OpsPerSec, eff,
							fmt.Sprintf("%s: scaling stalls %s - throughput %s for %sx %s (step efficiency %s, previous %s)",
								impl, span, signedPercent(rel), trimFloat(cur.X/prev.X, 2), sp.Label,
								trimFloat(eff, 2), trimFloat(prevEff, 2))))
					}
					prevEff = eff
				}
			}
			if prev.P999Ns > 0 && cur.P999Ns > kneeTailFactor*prev.P999Ns {
				f := cur.P999Ns / prev.P999Ns
				knees = append(knees, mk(kneeKindTail, kneeMetricP999, prev.P999Ns, cur.P999Ns, f-1,
					fmt.Sprintf("%s: p99.9 x%s %s (%s -> %s)", impl, trimFloat(f, 1), span,
						fmtLatency(prev.P999Ns), fmtLatency(cur.P999Ns))))
			}
			if d := cur.GCCPUPercent - prev.GCCPUPercent; d > kneeGCRisePoints {
				knees = append(knees, mk(kneeKindGC, kneeMetricGCCPU, prev.GCCPUPercent, cur.GCCPUPercent, d,
					fmt.Sprintf("%s: GC CPU +%s pp %s (%s %% -> %s %%)", impl, trimFloat(d, 1), span,
						trimFloat(prev.GCCPUPercent, 1), trimFloat(cur.GCCPUPercent, 1))))
			}
		}
	}
	return knees
}

// paramLabel formats a sweep value for messages, e.g. "N=5M".
func paramLabel(param string, x float64) string {
	switch param {
	case "n":
		return "N=" + humanCount(x)
	case "value_size":
		return "value=" + humanBytes(x)
	case "goroutines":
		return "G=" + trimFloat(x, 0)
	case "read_ratio":
		return "reads=" + trimFloat(x*100, 1) + " %"
	case "garbage_rate":
		return "garbage=" + trimFloat(x, 3)
	}
	return param + "=" + trimFloat(x, 3)
}

func signedPercent(rel float64) string {
	p := math.Round(rel * 100)
	if p > 0 {
		return "+" + trimFloat(p, 0) + " %"
	}
	return trimFloat(p, 0) + " %"
}

func fmtLatency(ns float64) string {
	switch {
	case ns >= 1e6:
		return trimFloat(ns/1e6, 2) + " ms"
	case ns >= 1e3:
		return trimFloat(ns/1e3, 2) + " µs"
	}
	return trimFloat(ns, 0) + " ns"
}

// sweepPoints extracts the knee-detection input of a sweep session.
func sweepPoints(runs []*RunRecord) []SweepPoint {
	var pts []SweepPoint
	for _, r := range runs {
		if r.ParamValue == nil || r.Summary == nil || r.State != stateDone {
			continue
		}
		s := r.Summary
		pts = append(pts, SweepPoint{Impl: r.Impl, X: *r.ParamValue, OpsPerSec: s.OpsPerSec, P99Ns: s.P99Ns,
			P999Ns: s.P999Ns, GCCPUPercent: s.GCCPUPercent, RSSBytes: s.RSSBytes})
	}
	return pts
}
