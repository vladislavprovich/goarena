# goarena

Hybrid memory management for Go: keep large, long-lived data sets in
off-heap memory obtained through cgo, so that the garbage collector's work no
longer grows with the size of your cache.

```go
s, _ := store.New(store.Config{})   // 256 shards, 64 MiB chunks
defer s.Close()                      // off-heap memory is freed explicitly

s.Set([]byte("user:42"), []byte(`{"name":"Ada"}`))
buf, ok := s.GetInto(buf[:0], []byte("user:42"))
```

## Overview

goarena is a small, dependency-free (standard library only) Go module that
consists of:

| package | purpose |
|---|---|
| `arena` | `Arena` - chunked bump-pointer allocator over C memory; `Ref` - 64-bit GC-invisible reference; `Slab` - 201 log-linear size classes (8-byte steps to 256 B, 32 classes per power of two to 8 KiB, then x1.25 up to 64 KiB) with intrusive free lists; `NewValue[T]` / `NewSlice[T]` typed helpers that refuse types containing Go pointers |
| `store` | concurrent, sharded key-value store whose entries **and** hash index live off-heap (open addressing, linear probing), with lock-free (seqlock) reads, TTLs, zero-copy `View`, statistics and an idempotent `Close` |
| `gcpace` | opt-in GC pacer for programs with large off-heap data: GOGC applied as if off-heap bytes were heap, GOMEMLIMIT treated as the total budget (`store.Config.GCPacing`) |
| `gcstats` | GC and process metrics: GC CPU fraction, cycles, STW pauses, heap live/goal/scannable bytes, RSS (Windows and Linux) |
| `internal/cmem` | the cgo bridge - the only package that imports `"C"`; counts every cgo call, live block and byte |
| `cmd/goarena-bench` | benchmark harness comparing goarena with `map`, `sync.Map` and a bigcache-style Go-heap cache, one OS process per run |
| `examples/httpcache` | HTTP cache service with a live, self-contained dashboard |

## Why

The Go GC is concurrent and pauses are short, but every cycle must *mark*
all live pointer-bearing objects, and cycles happen more often when the
program allocates. A cache with ten million `map[string][]byte` entries
contains twenty million heap objects the collector traces again and again;
GC CPU time, mark assists on the request path, tail latency and RSS
(about 2x the live heap with the default `GOGC=100`) all scale with the
cache size, not with the request rate.

goarena moves that data out of the Go heap. The collector sees only a few
hundred small bookkeeping objects, independently of how many entries the
store holds. The price is explicit lifetime management, a cgo call per
64 MiB chunk (amortised to almost nothing) and copying values in and out.

## Design

```
            Go heap                         |              C heap (malloc)
  Store ─┬─ shard[0] {seq, Mutex, Slab} ────┼──► arena chunk 0 (64 MiB): index slots [hash|ref]...,
         ├─ shard[1] ...                    │      spans (64 KiB) of entries, recycled old index tables
         └─ arena (shared chunk pool) ──────┼──► chunk 1 (64 MiB) ...
                                            │    dedicated chunks (values > 64 KiB, retained for reuse)
```

* A key is hashed once (`hash/maphash`, per-store seed). The top bits
  select one of 256 shards, the low bits the home slot of the shard index.
* Index slots `{hash uint64, ref uint64}` (16 B) live in the arena; linear
  probing; tombstones; the table doubles at load factor 0.75 and the old
  table is reused for entries.
* Entries `[keyLen u16][flags u16][valLen u32]([expireAt i64])[key][value]`
  (8-byte header, 16 bytes only with a TTL) live in slab blocks of 201 size
  classes; same-class overwrites happen in place.
* **Reads take no lock:** a per-shard sequence number (seqlock) validates a
  speculative read; writers make it odd while they mutate the shard. Every
  reference a reader follows is bounds-checked, and memory is never unmapped
  while the store is open, so a torn read is simply retried.
* One arena per store is the shared chunk pool; shard slabs take 64 KiB
  spans from it, so 256 shards do not reserve 256 chunks.

See [DESIGN_NOTES.md](DESIGN_NOTES.md) for byte-level layouts, algorithms
and the rationale behind every decision.

## Install

goarena needs cgo, i.e. `CGO_ENABLED=1` and a C compiler in `PATH`.

```sh
go get github.com/savokh/goarena@latest
```

On Windows install MinGW-w64 (GCC) and add its `bin` directory to `PATH`;
`build.ps1` does this for the default location and runs vet, tests and the
build:

```powershell
.\build.ps1 -Race -Cover
```

On Linux and macOS the system compiler is used; `make` runs the same steps.
Go 1.22 or newer is required.

## Quick start

```go
package main

import (
	"fmt"
	"time"

	"github.com/savokh/goarena/store"
)

func main() {
	s, err := store.New(store.Config{Shards: 256, ChunkSize: 64 << 20})
	if err != nil {
		panic(err)
	}
	defer s.Close()

	_ = s.SetString("greeting", "hello, off-heap world")
	_ = s.SetWithTTL([]byte("session:1"), []byte("token"), time.Hour)

	v, _ := s.GetString("greeting")         // copy
	buf := make([]byte, 0, 256)
	buf, _ = s.GetInto(buf, []byte("session:1")) // copy into a reused buffer
	s.View([]byte("greeting"), func(b []byte) { // zero-copy, under a read lock
		fmt.Println(len(b))
	})
	fmt.Println(v, string(buf), s.Stats().OffHeapBytes)
}
```

Lower-level allocators:

```go
a := arena.MustNew(arena.Config{ChunkSize: 1 << 20})
defer a.Release()

type sample struct{ At int64; Value float64 }
xs, _ := arena.NewSlice[sample](a, 1_000_000) // 16 MB, invisible to the GC
_, err := arena.NewSlice[string](a, 1)        // ErrPointerType: strings hold pointers

sl := arena.NewSlab(a, arena.SlabConfig{})
ref, _ := sl.Alloc(100)                        // 120-byte class
copy(sl.Bytes(ref, 100), "payload")
sl.Free(ref, 100)
```

Example service with the dashboard at <http://127.0.0.1:8080/dashboard>:

```sh
go run ./examples/httpcache -selfload -keys 2000000
go run ./examples/httpcache -selfload -keys 2000000 -backend gomap   # compare
curl -X PUT --data-binary @file.json http://127.0.0.1:8080/kv/doc1
curl http://127.0.0.1:8080/kv/doc1
curl http://127.0.0.1:8080/stats
```

## API

### package `store`

| | |
|---|---|
| `New(Config) (*Store, error)` | `Config{Shards, ChunkSize, MaxMemory, DefaultTTL, InitialCapacity}` |
| `Set(key, val) error`, `SetWithTTL(key, val, ttl) error`, `SetString` | copy the data into the store |
| `Get(key) ([]byte, bool)`, `GetString` | return a copy |
| `GetInto(dst, key) ([]byte, bool)` | append into `dst[:0]`; no allocation when `dst` is large enough |
| `View(key, fn func([]byte)) bool` | zero-copy access under the shard read lock |
| `Delete(key) bool`, `DeleteString`, `Has(key) bool`, `Len() int` | |
| `Range(fn func(key, val []byte) bool)` | iterate shard by shard |
| `EvictExpired() int` | free entries whose TTL elapsed |
| `Stats() Stats` | entries, bytes, fragmentation, arena, index, probe distance, cgo calls |
| `Close() error` | free all memory; idempotent |
| `ErrClosed`, `ErrTooLarge`, `ErrNoMemory`, `FinalizedStores()` | |

### package `arena`

| | |
|---|---|
| `New(Config) (*Arena, error)`, `MustNew` | `Config{ChunkSize, MaxBytes}` |
| `(*Arena).Alloc(n) (Ref, error)` | 8-byte aligned bump allocation |
| `(*Arena).Bytes(ref, n) []byte`, `Pointer(ref)` | bounds-checked views |
| `AllocDedicated`, `FreeDedicated` | individually freed large blocks |
| `Reset()`, `Release()`, `Stats()` | |
| `NewSlab(a, SlabConfig) *Slab`; `Alloc(n)`, `Free(ref, n)`, `Bytes`, `Stats` | size-class allocator |
| `NewValue[T]`, `NewSlice[T]`, `IsPointerFree[T]` | typed helpers |
| `Ref`, `SizeClasses()`, `ClassSize(n)` | |

### package `gcstats`

`Read() Snapshot`, `Between(a, b) Delta`, `Snapshot.Gauges()`, `Process()`.

## Recommended configuration for services

A store moves the data set out of the Go heap, so the heap the garbage
collector sees becomes tiny. With the default `GOGC=100` a service that
allocates per request (decoding, buffers, response objects) then reaches
its - tiny - heap goal every few megabytes and may run hundreds of GC
cycles per second. Each cycle is cheap, but together they cost throughput.
Enable **GC pacing** for such services:

```go
s, _ := store.New(store.Config{GCPacing: true})
defer s.Close() // restores GOGC / GOMEMLIMIT when the last paced store closes
```

The pacer (package `gcpace`) periodically sets `GOGC` so that the heap goal
is the one the runtime would use if the off-heap bytes were part of the
heap (`goal = live + (live + offHeap) * GOGC/100`): the GC runs as often as
it would for a heap-resident cache of the same size, while every cycle still
marks only the small Go heap. If a `GOMEMLIMIT` is set (for example the
container budget), the pacer treats it as the budget for Go *plus* off-heap
memory and lowers the Go limit to `budget - offHeap`, because the runtime
does not count C memory. Batch programs and programs that allocate little
do not need it. Configuration knobs: `gcpace.SetDefaultConfig`, or start a
pacer of your own with `gcpace.Start`.

## Safety rules

1. **Close what you open.** Off-heap memory is not reclaimed by the GC.
   `Store.Close` and `Arena.Release` are idempotent; a finalizer closes a
   forgotten store as a safety net and counts it in `FinalizedStores()`.
2. **Never put Go pointers into arena memory.** Strings, slices, maps,
   interfaces and pointers stored there are invisible to the GC. `NewValue`
   and `NewSlice` reject such types.
3. **Do not retain views.** Slices from `Arena.Bytes`, `Slab.Bytes`, `View`
   and `Range` alias C memory; they dangle after the memory is freed. In
   callbacks, copy what you need and do not call back into the store.
4. **Concurrency.** `Store` and `Arena` are safe for concurrent use; `Slab`
   is not (protect it with a lock).
5. **Free with the right size.** `Slab.Free(ref, n)` needs the size passed
   to `Alloc`.

## Benchmarks

All numbers below were measured on an AMD Ryzen 7 7735HS (8 cores /
16 threads), 32 GB RAM, Windows 10 Pro 22H2, Go 1.24.6, GCC 14.2 (MinGW-w64);
16 goroutines, 90 % reads, uniform keys, 20-byte keys, 128-byte values,
15 s measured after 3 s warm-up, 3 repeats per point, every run in a separate
process. The complete tables, methodology and interpretation are in
[results/SUMMARY.md](results/SUMMARY.md); raw JSON in `results/raw`.

N = 10 000 000 entries, goarena v0.2.0 session (means of 3 runs; the v0.3.0
re-run of this point measured 32.37 Mops/s, p99.9 4.36 µs, RSS 1797 MiB for
goarena and 25.38 Mops/s / 28.67 µs / 1912 MiB for gobytes; "full GC" is the
wall time of one forced `runtime.GC()` with the data set live, experiment E7):

| metric | gomap | gomap GOGC=400 | sync.Map | gobytes | **goarena** |
|---|---|---|---|---|---|
| throughput, Mops/s | 21.54 | 24.00 | 12.34 | 28.68 | **34.96** |
| latency p99, µs | 2.55 | 2.20 | 2.13 | 1.34 | **0.91** |
| latency p99.9, µs | 38.10 | 21.80 | 12.93 | 16.81 | **2.39** |
| latency max, ms | 27.61 | 18.04 | 75.89 | **1.59** | 16.18 |
| GC CPU (share of available CPU), % | 5.12 | 4.88 | 12.23 | 0.00 | **0.00** |
| one full GC cycle, ms | 316 | - | 1475 | 2.4 | **0.45** |
| Go heap (objects), MiB | 3240 | 7009 | 6109 | 1891 | **3** |
| process RSS, MiB | 4133 | 7190 | 6375 | 1902 | **1796** |
| populate 10M entries, s | 1.31 | 1.09 | 1.69 | 0.62 | **0.53** |

Hot keys (Zipf s = 1.1, N = 5M): goarena 48.7 Mops/s with p99.9 1.0 µs,
sync.Map 22.1 Mops/s / 5.4 µs. At N = 50M goarena keeps 25.6 Mops/s and a
0.7 ms full GC; gomap needs 12.5 s for one full GC.

Where goarena does **not** win (v0.3.0): its worst-case latency (about 10
operations per million above 1 ms with 16 busy goroutines on 16 hardware
threads, 0.7 per million with 14-15) is worse than gobytes'; with 4 KiB values
it needs 4 % more memory than gobytes (1 KiB and smaller: 1-13 % less); and in
a service that allocates request garbage the tiny Go heap makes the default
GOGC=100 run hundreds of GC cycles per second - enable `GCPacing` (see
"Recommended configuration"): in experiment E9 goarena then reaches 10.05
Mops/s versus 9.55 for gobytes and 5.03 without pacing, and in a 1210 MiB
container budget (E10) 9.17 versus 9.52. An empty cgo call costs ~40-65 ns
versus ~1.4-1.9 ns for a Go call, so goarena only pays off because it
crosses into C once per 64 MiB chunk, not per operation.

Reproduce:

```powershell
.\scripts\run_experiments.ps1          # E1, E1b, E2, E4, E5, E6, E7 (~45 min)
.\scripts\run_microbench.ps1           # E3: go test -bench micro-benchmarks
bash scripts/run_all_v2.sh             # the complete v0.2.0 session incl. E3, E8, E9, E10 (~70 min)
bash scripts/run_v2_supplement.sh      # E3 re-run and the tuned goarena variants of E9/E10
bash scripts/run_v3.sh                 # v0.3.0 session: E1r, E4, E9, E10 (+ goarena-paced), E1t
```

## Limitations

* **cgo is required** (no cross-compilation without a C toolchain, slower
  builds). Each cgo call costs tens of nanoseconds; goarena amortises it by
  allocating 64 MiB chunks, but code that calls C per operation would not
  benefit.
* **Copy semantics.** Values are copied in and out (or accessed under a lock
  with `View`). Workloads dominated by large values pay for the copies.
* **No eviction policy** besides TTL; memory can be limited with
  `MaxMemory` (writes then fail with `ErrNoMemory`).
* **Memory is not returned to the OS** until `Close`: freed blocks are reused
  through free lists of the same size class; a workload that shifts its value
  sizes over time keeps the old classes' memory.
* **View and Range hold the shard lock** while the callback runs; writers of
  that shard wait.
* **Internal fragmentation:** below 8 B per entry up to 256 B, below 3.1 %
  up to 8 KiB, up to ~20 % between 8 and 64 KiB; large values (> 64 KiB)
  are rounded up to large classes (up to 25 %).
* **Large-object memory is retained** on free lists until reused or `Close`
  (required by the lock-free read path).
* **CPU saturation:** reads never block, so with as many busy goroutines as
  logical CPUs the process uses nearly all CPU time; rare multi-millisecond
  outliers then come from the operating system preempting threads.
* **Not a database:** no persistence, no transactions, no iteration order.
* Process-level tools (RSS) see the off-heap memory, but Go tooling
  (`GOMEMLIMIT`, heap profiles) does not.

## License

MIT - see [LICENSE](LICENSE). Copyright (c) 2026 Vladyslav Savokh.
