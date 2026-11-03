// Package cmem is the CGO bridge of goarena: the only package in the module
// that imports "C".
//
// It exposes a minimal, allocation-free wrapper around the C runtime allocator
// (malloc, calloc and free) and keeps process-wide atomic counters of every
// crossing of the Go/C boundary, the number of live blocks and the number of
// bytes currently reserved outside of the Go heap. These counters are what
// the leak tests and the benchmark harness use to prove that every byte that
// was obtained from C has been returned.
//
// # Builds without cgo
//
// With CGO_ENABLED=0 the package falls back to a pure-Go allocator
// (go_heap_allocator.go, build tag !cgo; [OffHeap] is false): blocks are
// pointer-free []byte allocations in the Go heap, kept reachable in an
// internal registry until [Free] (the Go collector never moves heap
// objects, so their addresses are stable). The API, the block header and
// the live-block and byte counters are the same; CgoCalls stays 0. The
// memory is not scanned by the collector, but it is part of the Go heap:
// it counts towards GOGC's heap goal and GOMEMLIMIT. The Go runtime also
// terminates the process instead of returning nil when it cannot satisfy
// an allocation.
//
// # Block header
//
// Every block returned by [Malloc] and [Calloc] is preceded by a hidden
// 16-byte header that stores the requested size and a magic word:
//
//	offset 0  uint64  size of the user area in bytes
//	offset 8  uint64  magic (0x676f6172656e6121, "goarena!") while live
//	offset 16 ...     user area (returned pointer, 16-byte aligned)
//
// The header lets [Free] take only a pointer (like C free) while still keeping
// exact byte accounting, and it allows a cheap best-effort detection of
// pointers that were not produced by this package. Blocks used by goarena are
// large (arena chunks, hash index tables, large objects), so the header costs
// far less than 0.01 % of the reserved memory in practice.
//
// # Safety
//
// Memory obtained from this package is invisible to the Go garbage
// collector. It must never contain Go pointers: the collector would not see
// them, could free the referenced objects and leave dangling references.
// All higher-level packages (arena, store) only ever store plain integers
// (lengths, hashes, offsets) and raw bytes in C memory.
package cmem
