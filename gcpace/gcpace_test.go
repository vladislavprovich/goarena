package gcpace

import (
	"math"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"
)

// withGC runs f with GOGC/GOMEMLIMIT set as given and restores them.
func withGC(t *testing.T, gogc int, limit int64, f func()) {
	t.Helper()
	oldP := debug.SetGCPercent(gogc)
	oldL := debug.SetMemoryLimit(limit)
	defer func() {
		debug.SetGCPercent(oldP)
		debug.SetMemoryLimit(oldL)
	}()
	f()
}

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

func TestPacerAdjustsAndRestores(t *testing.T) {
	withGC(t, 100, math.MaxInt64, func() {
		var off atomic.Int64
		off.Store(2 << 30)
		p := Start(Config{Interval: time.Hour, MemoryLimit: 3 << 30})
		remove := p.Add(func() int64 { return off.Load() })
		p.Update()
		st := p.Stats()
		if st.GCPercent <= 100 || st.OffHeapBytes != 2<<30 || st.Updates == 0 {
			t.Fatalf("pacer did not raise GOGC: %+v", st)
		}
		if got := currentGCPercent(); got != st.GCPercent {
			t.Fatalf("runtime GOGC %d, pacer says %d", got, st.GCPercent)
		}
		if lim := debug.SetMemoryLimit(-1); lim != 1<<30 {
			t.Fatalf("Go memory limit %d, want 1 GiB (3 GiB budget - 2 GiB off-heap)", lim)
		}
		// small changes are not applied, large ones are
		before := p.Stats().Updates
		off.Store(off.Load() + 1)
		p.Update()
		if p.Stats().Updates != before {
			t.Fatal("negligible change must not touch the runtime")
		}
		off.Store(4 << 30)
		p.Update()
		if lim := debug.SetMemoryLimit(-1); lim != DefaultMinGoLimit {
			t.Fatalf("limit must not drop below the minimum: %d", lim)
		}
		remove()
		remove() // idempotent
		p.Update()
		if got := p.Stats().GCPercent; got != 100 {
			t.Fatalf("without sources GOGC must return to 100, got %d", got)
		}
		p.Stop()
		p.Stop() // idempotent
		p.Update()
		if currentGCPercent() != 100 || debug.SetMemoryLimit(-1) != math.MaxInt64 {
			t.Fatal("Stop must restore the original settings")
		}
	})
}

func TestPacerInheritsSettings(t *testing.T) {
	withGC(t, 200, 2<<30, func() {
		p := Start(Config{Interval: 5 * time.Millisecond})
		defer p.Stop()
		p.Add(func() int64 { return 1 << 30 })
		deadline := time.Now().Add(2 * time.Second)
		for p.Stats().Updates == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond) // the background goroutine adjusts
		}
		st := p.Stats()
		if st.GCPercent < 200 || st.GoMemoryLimit != 1<<30 {
			t.Fatalf("inherited GOGC=200 / 2 GiB budget not applied: %+v", st)
		}
	})
	withGC(t, -1, math.MaxInt64, func() {
		p := Start(Config{Interval: time.Hour})
		p.Add(func() int64 { return 1 << 30 })
		p.Update()
		if currentGCPercent() != -1 {
			t.Fatal("GOGC=off must be left alone")
		}
		p.Stop()
	})
}

func TestDefaultPacerRefcount(t *testing.T) {
	withGC(t, 100, math.MaxInt64, func() {
		SetDefaultConfig(Config{Interval: time.Hour})
		defer SetDefaultConfig(Config{})
		if Default() != nil {
			t.Fatal("no default pacer expected")
		}
		u1 := Register(func() int64 { return 1 << 30 })
		u2 := Register(func() int64 { return 1 << 30 })
		p := Default()
		if p == nil {
			t.Fatal("default pacer not started")
		}
		p.Update()
		if st := p.Stats(); st.OffHeapBytes != 2<<30 || currentGCPercent() <= 100 {
			t.Fatalf("sources not summed: %+v", st)
		}
		u1()
		u1()
		if Default() != p {
			t.Fatal("pacer stopped too early")
		}
		u2()
		if Default() != nil || currentGCPercent() != 100 {
			t.Fatal("last unregister must stop the pacer and restore GOGC")
		}
	})
}

// TestPacingReducesGCFrequency is an end-to-end check: a program with
// "off-heap" bytes and a stream of garbage runs far fewer GC cycles with
// the pacer than without it.
func TestPacingReducesGCFrequency(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates")
	}
	run := func(paced bool) uint32 {
		var ms runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&ms)
		before := ms.NumGC
		if paced {
			p := Start(Config{Interval: time.Hour})
			defer p.Stop()
			p.Add(func() int64 { return 256 << 20 })
			p.Update()
		}
		var sink []byte
		for i := 0; i < 20000; i++ {
			sink = make([]byte, 16<<10) // 320 MiB of garbage in total
		}
		_ = sink
		runtime.ReadMemStats(&ms)
		return ms.NumGC - before
	}
	withGC(t, 100, math.MaxInt64, func() {
		plain, paced := run(false), run(true)
		t.Logf("GC cycles: plain %d, paced %d", plain, paced)
		if paced*4 > plain {
			t.Fatalf("pacing should cut GC cycles at least 4x: plain %d, paced %d", plain, paced)
		}
	})
}
