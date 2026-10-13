// Package arena provides off-heap memory allocators for Go programs:
// a chunked bump-pointer [Arena] and a size-class [Slab] allocator built on
// top of it.
//
// Memory managed by this package is obtained from the C allocator through
// cgo, in large chunks. It is not part of the Go heap: the garbage
// collector neither scans it nor counts it towards the heap goal, so
// gigabytes of long-lived data stored here add (almost) nothing to the cost
// of a GC cycle. In exchange the program becomes responsible for the
// lifetime of that memory.
//
// # References
//
// Allocations are identified by a [Ref], a 64-bit integer that encodes a
// chunk index and an offset. Refs are not pointers, so they can be kept in
// pointer-free data structures that the garbage collector never inspects
// (maps of integers, C memory, arrays of integers). [Arena.Bytes] turns a
// Ref into a temporary []byte view.
//
// # Safety rules
//
//  1. Never store Go pointers (including strings, slices, maps, interfaces)
//     in arena memory. The typed helpers [NewValue] and [NewSlice] enforce
//     this with a reflection check.
//  2. Views returned by Bytes and pointers returned by NewValue are valid
//     only until the memory is freed, Reset or Released. Do not keep them
//     in long-lived Go structures.
//  3. An Arena is safe for concurrent use; a Slab is not.
//  4. Always Release an arena you no longer need: the memory is not
//     reclaimed by the garbage collector.
package arena
