package gcpace

import (
	"math"
	"testing"
)

func TestPercent(t *testing.T) {
	const MiB = 1 << 20
	cases := []struct {
		gogc      int
		live, off int64
		maxPct    int
		want      int
	}{
		{100, 16 * MiB, 0, DefaultMaxPercent, 100},          // no off-heap memory: unchanged
		{100, 16 * MiB, 960 * MiB, DefaultMaxPercent, 6100}, // the E9 situation
		{100, 1 * MiB, 60 * MiB, DefaultMaxPercent, 1600},   // live raised to the 4 MiB minimum
		{50, 100 * MiB, 100 * MiB, DefaultMaxPercent, 100},  // scales the user's GOGC
		{100, 4 * MiB, 1 << 50, 1000, 1000},                 // capped by maxPct
		{-1, 16 * MiB, 960 * MiB, DefaultMaxPercent, -1},    // GC off stays off
		{100, 16 * MiB, -5, DefaultMaxPercent, 100},         // negative sources are ignored
	}
	for _, c := range cases {
		if got := Percent(c.gogc, c.live, c.off, c.maxPct); got != c.want {
			t.Errorf("Percent(%d, %d, %d, %d) = %d, want %d", c.gogc, c.live, c.off, c.maxPct, got, c.want)
		}
	}
	// heap goal with the computed percent equals the goal of live+off
	live, off := int64(16<<20), int64(960<<20)
	p := Percent(100, live, off, DefaultMaxPercent)
	goal := float64(live) * (1 + float64(p)/100)
	want := float64(live) + float64(live+off)
	if math.Abs(goal-want)/want > 0.01 {
		t.Fatalf("goal %.0f, want %.0f", goal, want)
	}
}

func TestGoLimit(t *testing.T) {
	if GoLimit(math.MaxInt64, 100, 10) != math.MaxInt64 {
		t.Fatal("no budget must mean no limit")
	}
	if GoLimit(1000, 300, 10) != 700 || GoLimit(1000, 995, 10) != 10 {
		t.Fatal("GoLimit")
	}
}
