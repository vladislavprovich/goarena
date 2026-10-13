package gcstats

import (
	"runtime"
	"testing"
	"time"
)

var sink [][]byte

func churn(n int) {
	for i := 0; i < n; i++ {
		sink = append(sink, make([]byte, 1024))
		if len(sink) > 4096 {
			sink = sink[:0]
		}
	}
}

func TestSnapshotAndDelta(t *testing.T) {
	a := Read()
	churn(200000)
	runtime.GC()
	b := Read()
	d := Between(a, b)
	if d.GCCycles == 0 {
		t.Fatal("expected at least one GC cycle")
	}
	if d.Duration <= 0 || d.AllocBytes < 200000*1024 {
		t.Fatalf("bad delta %+v", d)
	}
	if d.GCCPUFraction < 0 || d.GCCPUFraction > 1 || d.GCShareOfBusy < 0 || d.GCShareOfBusy > 1 {
		t.Fatalf("fractions out of range %+v", d)
	}
	if d.PauseEvents == 0 || d.PauseMaxEvent <= 0 {
		t.Fatalf("no pauses recorded: %+v", d)
	}
	// On Windows the runtime measures pauses with a ~0.5 ms clock, so every
	// individual pause of a short test can be reported as 0; only the
	// consistency of the figures can be asserted.
	if !d.CyclePauseExact || d.CyclePauseMax < 0 || d.PauseTotal < d.CyclePauseMax {
		t.Fatalf("cycle pauses %+v", d)
	}
	if b.HeapGoalBytes == 0 || b.RuntimeBytes == 0 || b.Goroutines == 0 || b.HeapObjects == 0 {
		t.Fatalf("gauges missing: %+v", b)
	}
	g := b.Gauges()
	if g.HeapGoalBytes != b.HeapGoalBytes || g.GCCycles != b.GCCycles {
		t.Fatal("Gauges copy")
	}
	t.Logf("cycles=%d gcCPU=%.4f busyShare=%.3f maxEvent=%v cycleMax=%v total=%v rss=%d MiB",
		d.GCCycles, d.GCCPUFraction, d.GCShareOfBusy, d.PauseMaxEvent, d.CyclePauseMax, d.PauseTotal, b.Process.RSSBytes>>20)
}

func TestProcessInfo(t *testing.T) {
	p := Process()
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		if p.RSSBytes == 0 || p.PeakRSSBytes < p.RSSBytes {
			t.Fatalf("RSS not available: %+v", p)
		}
		start := time.Now()
		for time.Since(start) < 50*time.Millisecond {
		}
		if Process().CPUSeconds() <= 0 {
			t.Fatal("CPU time not available")
		}
	}
}

func TestBucketUpper(t *testing.T) {
	inf := []float64{0, 1e-6, 1e-3, posInf()}
	if bucketUpper(inf, 0) != time.Microsecond || bucketUpper(inf, 2) != time.Millisecond {
		t.Fatal("bucketUpper")
	}
}

func posInf() float64 { var z float64; return 1 / z }

func TestDeltaWithoutHistograms(t *testing.T) {
	a, b := Snapshot{}, Snapshot{TotalCPUSeconds: 2, IdleCPUSeconds: 1, GCCPUSeconds: 0.5, GCCycles: 1}
	d := Between(a, b)
	if d.GCCPUFraction != 0.25 || d.GCShareOfBusy != 0.5 || d.PauseEvents != 0 {
		t.Fatalf("%+v", d)
	}
}
