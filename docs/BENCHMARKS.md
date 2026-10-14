# Benchmarks

All numbers were measured with the harness in [`cmd/goarena-bench`](../cmd/goarena-bench)
on one machine; nothing is estimated or copied from elsewhere.

| item | value |
|---|---|
| CPU | AMD Ryzen 7 7735HS, 8 cores / 16 threads (laptop) |
| RAM | 32 GB |
| OS | Windows 10 Pro 22H2 (10.0.19045) |
| Go | 1.24.6 windows/amd64, `GOMAXPROCS=16`, `GOGC=100` unless stated |
| C compiler | GCC 14.2.0 (MinGW-w64), `CGO_ENABLED=1` |

## Method

* Every run is a separate OS process, so no GC state leaks between runs.
* A run populates N entries (20-byte keys, 128-byte values unless stated),
  warms up for 3 s and then measures a mixed workload for 15 s
  (10 s for secondary experiments) with 16 goroutines, 90 % reads, uniform
  keys unless stated.
* Every 16th operation is timed with the CPU time-stamp counter
  ([`internal/tsc`](../internal/tsc)); `time.Now` has a resolution of about
  0.5 ms on Windows. Latencies go into a log-linear histogram.
* GC metrics come from [`gcstats`](../gcstats) (`runtime/metrics`); memory is
  the process RSS reported by the OS.
* Main series are repeated 3 times (mean ± std); secondary ones 1-2 times.

Compared implementations (the baselines are deliberately simple and were
never changed between goarena versions):

| name | description |
|---|---|
| `gomap` | 256 shards of `map[string][]byte` + `sync.RWMutex`, `GOGC=100` |
| `gomap-gogc400` | the same with `GOGC=400` |
| `syncmap` | `sync.Map` |
| `gobytes` | bigcache-style pointer-free Go-heap cache: `map[uint64]uint32` index + large `[]byte` chunks |
| `goarena` | `store.Store` from this module (default configuration) |
| `goarena-paced` | `store.Store` with `Config.GCPacing = true` |

## Results at N = 10 million entries

| metric | gomap | gomap GOGC=400 | sync.Map | gobytes | **goarena** |
|---|---|---|---|---|---|
| throughput, Mops/s | 23.36 | 23.09 | 10.93 | 25.38 | **32.37** |
| latency p99, µs | 1.94 | 2.46 | 2.55 | 1.67 | **0.90** |
| latency p99.9, µs | 17.83 | 23.42 | 21.59 | 28.67 | **4.36** |
| GC CPU, % of available CPU | 5.69 | 3.88 | 15.72 | 0.00 | **0.00** |
| Go heap, MiB | 3466 | 7216 | 6274 | 1914 | **4** |
| process RSS, MiB | 4050 | 7402 | 6538 | 1912 | **1797** |
| populate 10 M entries, s | 1.30 | 1.11 | 1.76 | 0.60 | **0.58** |

Means of 3 runs with goarena v0.3.0. goarena keeps about 1.86 GiB off-heap
and only 4 MiB in the Go heap, so the collector has almost nothing to mark.

### Cost of one full GC cycle versus N

| N | gomap | gobytes | goarena |
|---|---|---|---|
| 1 M | 26 ms | 0.6 ms | 0.27 ms |
| 10 M | 316 ms | 2.4 ms | 0.45 ms |
| 20 M | 941 ms | 4.9 ms | 0.6 ms |
| 50 M | 12.5 s | 11.1 ms | 0.7 ms |

### Large data sets

| N | gomap Mops/s / RSS | gobytes Mops/s / RSS | goarena Mops/s / RSS |
|---|---|---|---|
| 20 M | 21.47 / 8.0 GiB | 24.22 / 3.7 GiB | **29.96 / 3.5 GiB** |
| 30 M | 18.25 / 11.2 GiB | 20.04 / 5.8 GiB | **26.90 / 5.5 GiB** |
| 50 M | 18.40 / 15.0 GiB | 20.12 / 9.0 GiB | **25.57 / 8.5 GiB** |

`gomap-gogc400` and `sync.Map` were not run at 30 M and 50 M: their projected
memory did not fit the 32 GB machine.

### Value size (N = 2 M)

| value | gobytes Mops/s / RSS | goarena Mops/s / RSS | goarena class waste |
|---|---|---|---|
| 64 B | 28.96 / 306 MiB | **37.85 / 268 MiB** | 4.2 % |
| 256 B | 28.66 / 670 MiB | **30.17 / 627 MiB** | 1.4 % |
| 1 KiB | 17.32 / 2123 MiB | **18.55 / 2094 MiB** | 0.4 % |
| 4 KiB | 6.32 / **8070 MiB** | **6.75** / 8413 MiB | 2.4 % |

### Skewed and write-heavy workloads (N = 5 M)

| workload | gomap | sync.Map | gobytes | **goarena** |
|---|---|---|---|---|
| Zipf s = 1.1, 90 % reads: Mops/s / p99.9 | 18.61 / 80.9 µs | 22.12 / 5.4 µs | 19.71 / 82.9 µs | **48.66 / 1.0 µs** |
| uniform, 50 % writes: Mops/s / p99.9 | 19.33 / 34.3 µs | 7.97 / 29.4 µs | 28.02 / 14.5 µs | **33.07 / 2.7 µs** |

### Service that allocates per request

Every operation additionally allocates a 1 KiB request object, so the GC
runs continuously (see [GC pacing and GOMEMLIMIT](GUIDE.md#gc-pacing-and-gomemlimit)
in the user guide).

| configuration | N = 5 M Mops/s | GC cycles/s | p99.9 | RSS |
|---|---|---|---|---|
| gomap | 4.98 | 3.5 | 378 µs | 3295 MiB |
| sync.Map | 2.07 | 1.0 | 920 µs | 9848 MiB |
| gobytes | 9.55 | 11.2 | 98 µs | 1973 MiB |
| goarena (default GOGC) | 5.03 | 306 | 384 µs | 946 MiB |
| **goarena, `GCPacing: true`** | **10.05** | 11.3 | **66 µs** | 2017 MiB |

With a container budget (`GOMEMLIMIT = 1210 MiB` for every implementation,
N = 5 M) goarena with `GCPacing` reaches 9.17 Mops/s at 1132 MiB RSS and
p99.9 82 µs, gobytes 9.52 Mops/s at 1159 MiB and 105 µs, while `gomap` and
`sync.Map` spend 55-74 % of CPU time in the GC (1.07 and 0.71 Mops/s).

### Micro-benchmarks (`go test -bench`, 5 runs)

| benchmark | ns/op | allocs/op |
|---|---|---|
| empty Go call | 1.37 | 0 |
| empty cgo call | 38.4 | 0 |
| `C.malloc` + `C.free`, 128 B | 125 | 0 |
| `Arena.Alloc(128)` | 4.86 | 0 |
| `make([]byte, 128)` | 26.8 | 1 |
| `Slab.Alloc` + `Free`, 128 B | 11.0 | 0 |
| `Store.Get` / sharded map get (parallel) | 19.9 / 28.1 | 0 / 0 |
| `Store.Set` / sharded map set (parallel) | 24.5 / 37.2 | 0 / 2 |

Populating 10 M entries performs 28 cgo calls in total (one per 64 MiB
chunk); the measured phase performs none.

## Where goarena does not win

* **Default configuration under heavy request garbage:** without `GCPacing`
  the tiny Go heap makes `GOGC=100` run hundreds of cycles per second.
* **Very tight memory budget:** 3.7 % less throughput than `gobytes`.
* **4 KiB values:** 4.2 % more memory than `gobytes`.
* **Extreme tail with all hardware threads busy:** reads never block, so with
  16 busy goroutines on 16 logical CPUs about 10 operations per million take
  more than 1 ms (0.7 per million with 14-15 goroutines).

## Reproduce

```sh
go build -o bin/goarena-bench ./cmd/goarena-bench
./bin/goarena-bench list                                   # implementations and experiments
./bin/goarena-bench run -impl goarena -n 10000000          # one run, JSON to stdout
./bin/goarena-bench matrix -exp E1 -out results/raw        # full experiment, one process per run
./bin/goarena-bench aggregate -in results/raw -out results # runs.csv and summary.csv
go test -run XXX -bench . -benchmem ./internal/cmem ./arena ./store
```

`results/` is ignored by git.

Where the harness logic lives (`cmd/goarena-bench`):

| file | contents |
|---|---|
| `main.go` | command line: `run`, `matrix`, `aggregate`, `list` |
| `benchmark_run.go` | run options and validation, one complete run (populate, warm-up, measure) |
| `workload_generator.go` | keys, values, request garbage, key distributions, populate and mixed phases |
| `implementations.go` | the compared caches (`gomap`, `syncmap`, `gobytes`, `goarena`, ...) |
| `experiment_catalog.go` | definitions of the experiments (E1-E10, E1b, E1r, E1t) |
| `experiment_driver.go` | the matrix driver (one child process per run, gctrace side files) |
| `result_format.go` | the JSON result document |
| `result_aggregation.go` | gctrace parsing, `runs.csv` and `summary.csv` |
