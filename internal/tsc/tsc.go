package tsc

import (
	"sync"
	"time"
)

var (
	calibrateOnce sync.Once
	nsPerTick     float64 = 1
	origin                = time.Now()
)

// Calibrate measures the tick rate against the wall clock. It is invoked
// lazily by [ToNanos]; call it explicitly before a measurement starts to keep
// the ~200 ms calibration out of the timed region.
func Calibrate() {
	calibrateOnce.Do(func() {
		if !hasTSC {
			nsPerTick = 1
			return
		}
		// Busy-wait instead of sleeping: on Windows a sleep may overshoot
		// by a full timer period, which is harmless here because both
		// clocks are read at the same instants, but busy-waiting keeps the
		// thread on one core.
		t0 := time.Now()
		c0 := Ticks()
		for time.Since(t0) < 200*time.Millisecond {
		}
		c1 := Ticks()
		el := time.Since(t0)
		if c1 > c0 {
			nsPerTick = float64(el.Nanoseconds()) / float64(c1-c0)
		}
	})
}

// NsPerTick returns the calibrated conversion factor.
func NsPerTick() float64 {
	Calibrate()
	return nsPerTick
}

// ToNanos converts a tick delta into nanoseconds.
func ToNanos(ticks uint64) uint64 {
	Calibrate()
	return uint64(float64(ticks) * nsPerTick)
}

// Available reports whether the hardware counter is used (true) or the
// time.Since fallback (false).
func Available() bool { return hasTSC }
