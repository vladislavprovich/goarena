package gcpace

import (
	"math"
	"testing"
	"time"
)

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
