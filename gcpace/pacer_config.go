package gcpace

import "time"

// Defaults of [Config].
const (
	DefaultInterval   = 50 * time.Millisecond
	DefaultMinGoLimit = 64 << 20  // 64 MiB
	DefaultMaxPercent = 1_000_000 // upper bound of the GC percent set by the pacer
	minLiveHeap       = 4 << 20   // the runtime's own minimum heap goal
)

// Config configures a [Pacer]. The zero value is valid: it keeps the GOGC
// and GOMEMLIMIT that are in effect when the pacer starts as the targets
// for "Go heap + off-heap".
type Config struct {
	// GOGC is the effective GC percent applied to the Go heap plus the
	// off-heap bytes. Zero means the value in effect when the pacer
	// starts; if the GC was disabled then (GOGC=off), the pacer leaves the
	// GC percent alone and only manages the memory limit.
	GOGC int

	// MemoryLimit is the total memory budget of the process (Go memory +
	// off-heap). Zero means the GOMEMLIMIT in effect when the pacer
	// starts; a negative value or math.MaxInt64 means no budget.
	MemoryLimit int64

	// MinGoLimit is the smallest Go memory limit the pacer sets when the
	// off-heap bytes approach the budget. Zero means DefaultMinGoLimit.
	MinGoLimit int64

	// Interval is the adjustment period. Zero means DefaultInterval.
	Interval time.Duration

	// MaxPercent bounds the GC percent the pacer sets. Zero means
	// DefaultMaxPercent.
	MaxPercent int
}

// Stats describes the last adjustment of a pacer.
type Stats struct {
	OffHeapBytes  int64 // sum of all registered sources
	LiveHeapBytes int64 // Go live heap used for the computation
	GCPercent     int   // GC percent in effect (-1 = untouched/off)
	GoMemoryLimit int64 // Go memory limit in effect (math.MaxInt64 = none)
	Updates       int64 // adjustments performed
}
