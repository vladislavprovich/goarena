// Package gcpace adapts the Go garbage collector's pacing to programs that
// keep a large part of their data off-heap.
//
// # The problem
//
// With GOGC = 100 the runtime starts a GC cycle when the heap reaches about
// twice the live heap. A program whose large data set lives in goarena's
// C memory has a tiny live Go heap (a few MiB), so the heap goal is tiny
// too: a service that allocates a short-lived request object per operation
// then runs hundreds of GC cycles per second. Each cycle is cheap (nothing
// big to mark), but the stop-the-world phases, mark assists and write
// barriers add up. A heap-resident cache of the same size would have given
// the GC gigabytes of headroom and would collect a few times per second.
//
// # What the pacer does
//
// A Pacer periodically sets the GC percent so that the heap goal is the one
// the runtime would compute if the off-heap bytes were part of the heap:
//
//	goal = live + (live + offHeap) * GOGC/100
//	     = live * (1 + p/100)   with   p = GOGC * (live + offHeap) / live
//
// i.e. GC frequency equals that of a heap-resident store of the same size,
// while every cycle still marks only the small Go heap. If a total memory
// budget is configured (or a GOMEMLIMIT was in effect when the pacer
// started), the pacer also lowers the Go memory limit to
// budget - offHeap, because GOMEMLIMIT counts only memory managed by the Go
// runtime. Both settings are restored when the pacer stops.
//
// The pacer changes process-wide runtime settings. Use one per process:
// [Register] adds an off-heap source to the shared default pacer, which is
// what store.Config.GCPacing does.
package gcpace
