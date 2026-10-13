package tsc

import (
	"testing"
	"time"
)

func TestTicksMonotonic(t *testing.T) {
	prev := Ticks()
	for i := 0; i < 100000; i++ {
		cur := Ticks()
		if cur < prev {
			t.Fatalf("ticks went backwards: %d < %d", cur, prev)
		}
		prev = cur
	}
}

func TestCalibration(t *testing.T) {
	Calibrate()
	f := NsPerTick()
	if f <= 0 || f > 10 {
		t.Fatalf("implausible ns/tick: %v", f)
	}
	c0 := Ticks()
	t0 := time.Now()
	time.Sleep(50 * time.Millisecond)
	el := time.Since(t0)
	ns := ToNanos(Ticks() - c0)
	diff := float64(ns) - float64(el.Nanoseconds())
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.1*float64(el.Nanoseconds()) {
		t.Fatalf("tsc %dns vs wall %v differ by more than 10%%", ns, el)
	}
	t.Logf("ns/tick=%.4f available=%v", f, Available())
}

func BenchmarkTicks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Ticks()
	}
}
