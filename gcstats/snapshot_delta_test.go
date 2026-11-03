package gcstats

import (
	"testing"
	"time"
)

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
