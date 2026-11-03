//go:build !cgo && race

package store

// lockFreeReads is false in race-detector builds without cgo: the arena is
// then Go-heap memory, which the race detector instruments, and the
// speculative (validated afterwards) reads of the seqlock path would be
// reported as data races. Reads take the shard lock instead.
const lockFreeReads = false
