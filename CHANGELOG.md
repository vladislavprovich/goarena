# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/).

## [0.4.0] - 2026-10-14

New, backward-compatible API (hence a minor release; 0.3.2 was never
tagged and its fix is part of this release), a pure-Go build mode and a
rewritten documentation set.

### Added

- Builds without cgo: with `CGO_ENABLED=0` (no C compiler, cross-compilation)
  `internal/cmem` falls back to pointer-free `[]byte` blocks in the Go heap,
  kept reachable in a registry until `Free` (`go_heap_allocator.go`, build tag
  `!cgo`). The API and behaviour are unchanged and the GC still does not scan
  the data, but it counts towards the heap goal and `GOMEMLIMIT`. Previously
  such builds failed with `undefined: cmem.Malloc`.
- `arena.OffHeap`: reports the mode (true with cgo).
- `store.MustNew(Config) *Store`.
- `Store.GetOrSet(key, val) (actual []byte, loaded bool, err error)`: atomic
  set-if-absent; hits are lock-free.
- `Store.GetOrLoad(key, load func() ([]byte, error)) ([]byte, error)`:
  cache-aside helper; `load` runs without any lock, concurrent misses may
  call it more than once and all return the first stored value; a value that
  cannot be stored is returned together with the error.
- `Store.SetStringWithTTL`, `Store.HasString`.
- `Stats.String()`: one-line human-readable summary.
- Runnable examples on pkg.go.dev for `New`, `MustNew`, `Get`, `GetInto`,
  `View`, `SetWithTTL`, `EvictExpired`, `GetOrSet`, `GetOrLoad`, `Range`,
  `Config.GCPacing`, `Stats.String`, error handling, `arena.NewValue`,
  `Arena.Reset` and `arena.OffHeap`; expanded package documentation.
- Documentation: `docs/GUIDE.md` (user guide), `docs/USE_CASES.md` (when to
  use goarena and when not), `docs/FAQ.md`, `docs/TROUBLESHOOTING.md`,
  `docs/README.md` (index); README rewritten as a landing page; DESIGN.md
  section 17.
- CI job `test (ubuntu-latest, CGO_ENABLED=0)`: vet, build, test, examples
  and cross-compilation without a C toolchain; Makefile target `nocgo`.
- `examples/quickstart` shows `GetOrLoad`, `GetOrSet`, the string helpers,
  error details and `Stats.String`.

### Changed

- Write errors carry details and wrap the sentinel errors: `ErrTooLarge`
  names the key or entry size and the limit, `ErrNoMemory` (also from `New`)
  the sizes, the reserved bytes, `MaxMemory` and the chunk size. `errors.Is`
  works as before; code that compared errors with `==` must switch to
  `errors.Is`.
- `Stats` implements `fmt.Stringer`, so `%v` and `%+v` print the summary;
  use `%#v` to print every field.
- Builds without cgo only (no change with cgo): `Stats.OffHeapBytes` is 0,
  `GoMemoryBudget` returns the whole budget, `Config.GCPacing` has no effect,
  and race-detector builds read under the shard lock instead of the seqlock
  path.

### Fixed

- Lock-free reads on weakly ordered processors (arm64, e.g. Apple Silicon;
  also ppc64, riscv64, loong64): the seqlock reader could validate an
  unchanged sequence number while having copied bytes of a concurrent
  overwrite, because an atomic load does not order earlier plain loads
  there. Every validation is now preceded by a read fence
  (`store/read_fence_weak_order.go`); on amd64 and 386 the fence is empty,
  so performance there is unchanged. Found by `TestHotKeyReaders` in the
  macOS CI job.

## [0.3.1] - 2026-10-14

Repository and packaging release: no change to the public API or to the
behaviour of any package.

### Added

- Runnable examples, each with a short README: `examples/quickstart`
  (store tour), `examples/typed-arena` (typed values and slices in an
  `Arena`, `Slab` alloc/free) and `examples/gc-pacing` (GC cycles with and
  without `store.Config.GCPacing`, measured with `gcstats`).
- Continuous integration (`.github/workflows/ci.yml`): gofmt, `go vet`,
  `go build`, `go test` on Linux, Windows and macOS, `go test -race` on
  Linux and macOS, and a run of the examples.
- `docs/BENCHMARKS.md` (methodology and results), `docs/DESIGN.md`
  (formerly `DESIGN_NOTES.md`, now with a per-file source map) and
  `docs/CONTRIBUTING.md`.
- `internal/version`: the module version (`version.Module`) that
  `goarena-bench` writes into its result documents.

### Changed

- Module path is `github.com/vladislavprovich/goarena`; `go.mod` declares
  `go 1.27` (the minimum supported version).
- Repository layout: no Go code in the root; programs import
  `github.com/vladislavprovich/goarena/store` (or `.../arena`) directly.
  Source files are split by responsibility and carry descriptive names
  (for example `store/optimistic_read.go`, `store/store_write_operations.go`,
  `arena/slab_size_classes.go`, `gcpace/default_pacer.go`,
  `gcstats/process_metrics_windows.go`), with tests next to the code they
  test. The `httpcache` dashboard moved to `examples/httpcache/web/`.
- Makefile: `make build` builds the benchmark harness and every example
  into `bin/`; new target `make examples` runs the examples.

### Removed

- Benchmark result files, measurement scripts and screenshot tooling from
  the repository; `cmd/goarena-bench` reproduces every experiment and writes
  to the git-ignored `results/` directory.

## [0.3.0] - 2026-10-13

Focused iteration on the two v0.2.0 losses: request-garbage services
(E9/E10) and memory for 256 B - 4 KiB values (E4). See
[docs/BENCHMARKS.md](docs/BENCHMARKS.md).

### Added

- Package `gcpace`: opt-in GC pacer. Periodically (50 ms) sets the GC
  percent to `GOGC * (live + offHeap) / live` so that the heap goal equals
  that of a heap-resident data set of the same size, and - if a total
  budget is configured or a GOMEMLIMIT was in effect when it started -
  lowers the Go memory limit to `budget - offHeap`. Restores the previous
  settings on `Stop`. Process-wide default pacer with reference-counted
  `Register`.
- `store.Config.GCPacing`: registers the store's arena with the default
  pacer for the lifetime of the store (recommended for services that
  allocate per request).
- `arena.Arena.ReservedBytes`.
- `goarena-bench`: implementation `goarena-paced`; experiments `E1r`
  (E1 at N = 10M re-run) and `E1t` (14/15 worker goroutines); metrics
  `over_1ms_per_million`, pacer GC percent and Go limit; the result JSON
  records the goarena version.
- Tests for the pacer (formula, limit, restore, inheritance, reference
  counting, end-to-end GC reduction) and for store pacing.

### Changed

- `arena.Slab`: log-linear size classes - 8-byte steps to 256 B, 32
  classes per power of two from 256 B to 8 KiB (waste < 3.1 %), x1.25 to
  64 KiB: 201 classes instead of 56.
- `goarena-bench`: the `-memlimit` setting is applied before the cache is
  created (needed for `goarena-paced` to inherit it as its budget; for the
  other implementations it only moves before the allocation of empty
  shards).

## [0.2.0] - 2026-10-13

Optimisation round driven by the v0.1.0 measurements (memory versus the
pointer-free Go-heap baseline, tail latency under hot keys).

### Changed

- `arena.Slab`: hybrid size classes - 8-byte steps from 16 B to 256 B, then
  growth factor 1.25 (rounded to 8 B) up to 64 KiB: 56 classes instead of
  36. A 156-byte entry now uses a 160-byte block instead of 192 B.
- `store`: entry header shrunk to 8 bytes (`keyLen u16, flags u16,
  valLen u32`); the 8-byte expiration time is stored only for entries with a
  TTL. `MaxKeySize` is now 65 535 bytes (was 65 536).
- `store`: reads (`Get`, `GetInto`, `GetString`, `Has`) are lock-free
  seqlock reads validated by a per-shard sequence number, with a bounded
  number of retries and a locked fallback. Readers of a hot key no longer
  write to a shared cache line.
- `store`: the per-shard `sync.RWMutex` is replaced by a `sync.Mutex`
  writer lock (`shardLock`), because readers no longer lock on the fast
  path; `View`, `Range`, `Len` and `Stats` take it.
- `store`: index slot arrays are allocated from the store's arena (they
  count towards `MaxMemory`) and outgrown tables are donated to the shard's
  slab and reused for entries instead of being freed.
- `store`: shard structs are laid out on 64-byte boundaries (192 bytes;
  read-mostly fields of lock-free readers on their own cache line).
- `store.Stats.OffHeapBytes` now equals `ArenaReservedBytes` (the index is
  part of the arena); new field `DonatedBytes`.

### Added

- `arena.Arena.TryPointer`: non-panicking bounds-checked reference lookup
  for optimistic readers.
- `arena.SlabConfig.RetainLarge`, `arena.Slab.BlockSize`, `arena.Slab.Donate`,
  large-object classes (x1.25 from 64 KiB, rounded to 4 KiB).
- Tests: `TestOptimisticReadsNeverTorn` (CRC-checked values under
  relocating writers, rehash and deletion churn), `TestHotKeyReaders`,
  `TestReadOptimisticRejectsGarbage`, `TestShardLayout`,
  `TestHybridClassTable`, `TestSlabRetainLargeAndDonate`, `TestTryPointer`.
- `goarena-bench`: request-garbage simulation (`-garbage-rate`,
  `-garbage-bytes`), target-domain experiments E8 (N up to 50M with a
  memory-budget guard that records skipped runs), E9 (service with request
  garbage) and E10 (container memory budget via `debug.SetMemoryLimit`),
  new aggregated metrics (read/write p99.9, STW ms/s, garbage settings).

## [0.1.0] - 2026-10-13

### Added

- `internal/cmem`: cgo bridge over `malloc`/`calloc`/`free` with a 16-byte
  block header, exact byte accounting, live-block and cgo-call counters and a
  best-effort detector of foreign pointers and double frees.
- `arena.Arena`: chunked bump-pointer allocator (64 MiB chunks by default,
  8-byte alignment, `MaxBytes` limit, dedicated chunks for oversized
  requests, lock-free `Bytes`/`Pointer`, `Reset` and idempotent `Release`).
- `arena.Ref`: 64-bit GC-invisible reference `(chunk+1)<<32 | offset`.
- `arena.Slab`: 36 size classes from 16 B to 64 KiB (growth factor 1.25,
  multiples of 8 B), intrusive LIFO free lists, span batching (64 KiB) and a
  large-object path; per-class statistics and internal fragmentation.
- `arena.NewValue`, `arena.NewSlice`, `arena.IsPointerFree`: generic typed
  helpers with a reflection check that rejects types containing Go pointers.
- `store.Store`: sharded (256 by default) off-heap key-value store with an
  off-heap open-addressing index (linear probing, 16-byte slots, max load
  factor 0.75, tombstones with backward cleanup), `maphash` hashing with a
  per-store seed, TTL support, `Get`/`GetInto`/`View`/`Range`, string
  helpers, statistics, idempotent `Close` and a finalizer safety net.
- `gcstats`: snapshots and deltas of GC CPU time, cycles, pause histogram,
  exact per-cycle pauses, heap goal/live/scannable bytes and process RSS
  (Windows and Linux) using only the standard library.
- `internal/latency`: allocation-free log-linear latency histogram.
- `internal/tsc`: time-stamp-counter clock for sub-microsecond latency
  sampling on Windows, where `time.Now` has ~0.5 ms resolution.
- `cmd/goarena-bench`: benchmark harness comparing `gomap`,
  `gomap-gogc400`, `syncmap`, `gobytes` and `goarena`, one OS process per
  run, experiment driver (E1, E1b, E2, E4, E5, E6, E7) and CSV aggregation.
- `examples/httpcache`: HTTP KV service with `/stats` and a self-contained
  live dashboard, in-process load generator and a `gomap` backend for
  comparison.
- Unit tests, differential fuzz test, concurrency stress tests, leak and
  finalizer tests, examples and micro-benchmarks.
