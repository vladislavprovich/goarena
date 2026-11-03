package gcstats

// ProcessInfo is the operating system's view of the current process.
// Fields that the platform cannot provide are zero.
type ProcessInfo struct {
	RSSBytes      uint64  // resident set size (Windows: working set)
	PeakRSSBytes  uint64  // peak resident set size
	PrivateBytes  uint64  // committed private memory (Windows), 0 elsewhere
	UserSeconds   float64 // user-mode CPU time
	KernelSeconds float64 // kernel-mode CPU time
}

// CPUSeconds returns user + kernel CPU time.
func (p ProcessInfo) CPUSeconds() float64 { return p.UserSeconds + p.KernelSeconds }

// Process returns the current OS-level memory and CPU figures.
func Process() ProcessInfo { return readProcess() }
