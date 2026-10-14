//go:build cgo || !race

package store

// lockFreeReads enables the seqlock read path (optimistic_read.go). It is
// disabled only in race-detector builds without cgo, where the arena lives
// in the Go heap: the race detector instruments that memory and would
// report the intentional, validated races of the speculative readers.
const lockFreeReads = true
