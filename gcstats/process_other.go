//go:build !windows && !linux

package gcstats

// readProcess is not implemented on this platform; all fields are zero.
func readProcess() ProcessInfo { return ProcessInfo{} }
