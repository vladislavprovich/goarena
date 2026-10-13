// Package tsc provides a cheap, high-resolution monotonic clock for latency
// sampling in benchmarks.
//
// On Windows the resolution of time.Now is bounded by the system timer
// (typically 0.5 ms), which is three to four orders of magnitude coarser than
// the operations measured by goarena-bench. On amd64 this package reads the
// processor time-stamp counter (RDTSCP), which on every CPU of the last decade
// is invariant (constant rate, synchronised across cores), and converts ticks
// to nanoseconds with a factor calibrated once against the wall clock.
// On other architectures it falls back to time.Since with a fixed origin.
package tsc
