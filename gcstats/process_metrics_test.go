package gcstats

import (
	"runtime"
	"testing"
	"time"
)

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
