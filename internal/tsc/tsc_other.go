//go:build !amd64

package tsc

import "time"

const hasTSC = false

// Ticks returns nanoseconds elapsed since package initialisation.
func Ticks() uint64 { return uint64(time.Since(origin)) }
