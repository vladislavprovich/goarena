//go:build amd64

package tsc

const hasTSC = true

// Ticks returns the current value of the time-stamp counter (RDTSCP).
//
//go:noescape
func Ticks() uint64
