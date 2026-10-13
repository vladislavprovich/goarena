// Package goarena is the root of a hybrid memory-management toolkit for Go:
// long-lived data is kept in memory obtained from the C allocator through
// cgo, outside of the garbage-collected heap, while the rest of the program
// keeps using ordinary Go memory.
//
// The module consists of the following packages:
//
//   - [github.com/savokh/goarena/arena]: chunked bump-pointer Arena and the
//     size-class Slab allocator, plus typed helpers NewValue and NewSlice
//     that refuse types containing Go pointers;
//   - [github.com/savokh/goarena/store]: a concurrent, sharded key-value
//     store whose entries and hash index live entirely off-heap;
//   - [github.com/savokh/goarena/gcpace]: opt-in GC pacer that compensates
//     for the small Go heap of programs whose data lives off-heap;
//   - [github.com/savokh/goarena/gcstats]: garbage-collector and process
//     memory metrics over runtime/metrics;
//   - cmd/goarena-bench: the benchmark harness used to evaluate the model;
//   - examples/httpcache: an HTTP cache service with a live dashboard.
//
// The single package that imports "C" is internal/cmem. See README.md for
// the safety rules and DESIGN_NOTES.md for the design rationale.
package goarena

// Version is the version of the goarena module.
const Version = "0.3.0"
