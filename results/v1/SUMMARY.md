# goarena - results summary (for chapters 3, 4, 5 and the conclusions)

All numbers in this file come from real runs on the machine below; nothing
was estimated, adjusted or copied from literature. Raw data: `results/raw/*.json`
(one file per benchmark run, 154 runs), aggregated: `results/runs.csv`,
`results/summary.csv`, `results/microbench.csv`; console logs:
`results/logs/*.txt`. The tables in section 6 were generated from the CSV
files by `scripts/make_tables.py` (also saved as `results/tables.md`).

## 1. Environment

| item | value |
|---|---|
| CPU | AMD Ryzen 7 7735HS with Radeon Graphics, 8 cores / 16 threads (laptop CPU, boost enabled); invariant TSC 3.19 GHz (0.3135 ns/tick, calibrated) |
| RAM | 32 GB |
| OS | Windows 10 Pro 22H2, build 10.0.19045 |
| Go | go1.24.6 windows/amd64, GOMAXPROCS = 16, GOGC = 100 unless stated |
| C compiler | gcc 14.2.0 (x86_64-posix-seh-rev1, MinGW-Builds / MinGW-w64), CGO_ENABLED = 1 |
| Python | 3.8.10, matplotlib 3.7.5, numpy 1.24.4, pandas 2.0.3, Pillow 9.5.0 (figures only) |
| Browser | Google Chrome (headless, `--headless=new`) for the dashboard screenshots |
| Module | `github.com/savokh/goarena` v0.1.0, standard library only, `go 1.22` in go.mod |
| Date | 13.10.2026 |

## 2. How to reproduce

```powershell
cd D:\master-diploma\goarena
$env:PATH = "C:\Users\Vlad\mingw64\bin;$env:PATH"; $env:CGO_ENABLED = "1"
.\build.ps1 -Race -Cover                      # vet, tests, race, coverage, build
.\scripts\run_tests.ps1 -FuzzTime 90s         # all test logs into results\logs
.\scripts\run_experiments.ps1                 # E1, E1b, E2, E4, E5, E6, E7 (~45 min) + aggregation
.\scripts\run_microbench.ps1                  # E3 (go test -bench, -count 5) -> results\microbench.csv
.\scripts\screenshot_dashboard.ps1 -Backend goarena -WaitSeconds 125 -Out ..\work\figures\fig4_dashboard.png
.\scripts\screenshot_dashboard.ps1 -Backend gomap   -WaitSeconds 125 -Out ..\work\figures\fig4_dashboard_gomap.png
python scripts\make_tables.py                 # results\tables.md
python scripts\count_loc.py . results\loc.csv
cd ..\work\figscripts; python fig4_e1.py; python fig4_experiments.py; python fig4_micro.py; python fig4_terms.py
```

Single run example: `bin\goarena-bench.exe run -impl goarena -n 5000000 -duration 15s -warmup 3s`.
The commands actually executed head every block of
`results/logs/bench_matrix.txt` (the E7 re-run with `-force` and the E4r
verification runs are logged there as well).

## 3. Headline numbers

* **Throughput (E1, 16 goroutines, 90 % reads, 128-byte values):** goarena
  31.80 Mops/s at N = 10M vs 24.39 (gomap, +30.4 %), 26.79 (gobytes,
  +18.7 %), 25.15 (gomap GOGC=400), 11.73 (sync.Map). At N = 1M: 37.47 vs
  30.91 (+21.2 %) and 34.60 (+8.3 %).
* **Latency (E1, N = 10M):** p99 1.27 µs (goarena) vs 1.77 µs (gomap,
  -28 %) and 1.59 µs (gobytes); maximum observed operation latency 2.28 ms
  vs 22.86 ms (gomap) and 76 ms (sync.Map). p99.9 is similar for all
  implementations (15-30 µs; dominated by OS scheduling with 16 busy threads
  on 16 logical CPUs).
* **GC:** goarena performed **0 GC cycles** in every measured phase of E1,
  E1b, E2, E4, E5, E6, E7 (the Go heap stays at 2.3 MiB); gomap spent 1.9 %
  (1M) to 5.7 % (10M) of the available CPU in the GC, sync.Map 10.8-13.1 %.
* **Cost of one full GC cycle (E7, forced `runtime.GC()` with the data
  live, TSC-timed):** goarena 0.51-0.61 ms independently of N; gomap 18.6 ms
  (1M) -> 309 ms (10M), i.e. linear in N; sync.Map 113 -> 1508 ms; gobytes
  0.9 -> 2.6 ms. Mean concurrent mark phase from gctrace: gomap 18.9 ms
  (1M), 42.8 (2M), 153 (5M), 345 ms (10M).
* **Memory (E1, N = 10M):** RSS goarena 2103 MiB (off-heap 2112 MiB, Go heap
  2.3 MiB) vs gomap 4103 MiB (-48.7 %), gomap GOGC=400 7853 MiB, sync.Map
  6623 MiB, **but gobytes 1922 MiB (goarena +9.4 %)**.
* **cgo:** an empty cgo call costs **63.6 ± 3.0 ns** (E3, -count 5; a
  separate -count 1 run measured 43.4 ns, so the range on this machine is
  43-68 ns) versus **1.90 ± 0.27 ns** for an empty non-inlined Go call (a
  factor of ~23-36). goarena makes 3 587 (1M) to 5 149 (10M) cgo calls to
  populate the store, i.e. 0.0005-0.004 calls per entry, and **zero cgo
  calls in the measured read/write phase** (`measured_cgo_calls` = 0 in all
  goarena runs).

## 4. Code, tests and quality

### 4.1 Size (lines, `scripts/count_loc.py`, `results/loc.csv`)

| package | files | source lines (code) | test lines (code) |
|---|---|---|---|
| `internal/cmem` | 3 | 206 (121) | 170 (150) |
| `internal/tsc` | 6 | 95 (51) | 44 (40) |
| `internal/latency` | 2 | 203 (151) | 94 (88) |
| `arena` | 9 | 932 (651) | 723 (659) |
| `store` | 13 | 927 (655) | 1122 (999) |
| `gcstats` | 6 | 422 (331) | 82 (74) |
| `cmd/goarena-bench` | 7 | 1493 (1301) | 280 (268) |
| `examples/httpcache` | 5 | 683 (566) | 169 (159) |
| root (`doc.go`) | 1 | 23 (2) | 0 |
| **total (Go + asm)** | **52** | **4984 (3829)** | **2684 (2437)** |

"code" = non-blank, non-comment lines. Not included: the dashboard
(`dashboard.html`, 221 lines), the Python/PowerShell tooling (`scripts/`,
`tools/term2png.py` 204 lines) and the documentation.

### 4.2 Tests

| package | Test* | Example* | Fuzz* | Benchmark* | coverage |
|---|---|---|---|---|---|
| `arena` | 16 | 3 | - | 7 | 98.3 % |
| `store` | 16 | 5 | 1 | 8 | 97.3 % |
| `gcstats` | 4 | - | - | - | 96.7 % |
| `internal/cmem` | 5 | - | - | 5 | 98.0 % |
| `internal/latency` | 4 | - | - | 1 | 91.4 % |
| `internal/tsc` | 2 | - | - | 1 | 87.5 % |
| `cmd/goarena-bench` | 8 | - | - | - | 86.9 % |
| `examples/httpcache` | 4 | - | - | - | 94.2 % |
| **total** | **59** | **8** | **1** | **22** | **92.9 %** of all statements (`go tool cover -func`) |

* `go test -count=1 -v ./...`: 68 top-level tests (59 Test, 8 Example, the
  FuzzStore seed corpus) and 12 subtests, **all PASS, 0 FAIL**
  (`logs/test_v.txt`, `logs/test.txt`; final re-check after all edits
  `logs/test_final.txt`).
* `go test -race -count=1 -v ./...`: **all 80 PASS, 0 data races reported**
  (`logs/race.txt`). The race detector works with cgo on windows/amd64 with
  GCC 14.2; `-race` also enables `checkptr`, which reported no unsafe-pointer
  violation. Stress tests run 0.5 s instead of 2 s under `-race`.
* Coverage: `results/coverage.out`, per-function report `logs/cover_func.txt`,
  per package `logs/cover.txt`; total **92.9 %** (target 80 %).
* Fuzzing: `go test -run XXX -fuzz FuzzStore -fuzztime 90s
  -fuzzminimizetime 5s ./store` - **1 850 785 executions in 90 s with 16
  workers, 53 new interesting inputs (173 in the corpus), no failure**
  (`logs/fuzz.txt`). A first run with the default minimisation time
  (`-fuzztime 60s`) made 341 587 executions, also without failure
  (`logs/fuzz_60s_default_minimize.txt`; the engine spent most of that time
  minimising new inputs, hence `-fuzzminimizetime 5s`). FuzzStore is a
  differential test against a Go map model covering Set, overwrite across
  size classes, Delete, Get/GetInto/View/Has, TTL with a fake clock,
  EvictExpired, Len, Range and large (>64 KiB) values, with a C-memory leak
  check after every input.
* Leak tests: `TestNoLeakAfterClose` (20 000 entries of all sizes incl.
  large objects and deletions; 84 C blocks while open, **0 live blocks
  after Close**), `TestConcurrentClose`, `TestFinalizerSafetyNet` (a store
  dropped without Close is released by the finalizer, `FinalizedStores`
  incremented), FuzzStore leak check. `TestOffHeapIsInvisibleToGC`:
  500 000 entries -> 144 MiB off-heap, Go heap growth 422 KiB.
* Concurrency stress: `TestConcurrentStress` (16 workers, 4 shards, 2 000
  keys, 60 % GetInto / 5 % View / 25 % Set with random sizes up to 3 KB /
  10 % Delete, plus concurrent Stats/Len/Range): 10.5 M operations in 2 s,
  every value self-validated, 0 inconsistent reads.
* `go vet ./...`: no issues; `gofmt -l .`: 0 files; `go build ./...` ok
  (`logs/vet_build.txt`).

## 5. Benchmark methodology (short)

* Implementations (each run in a **separate OS process**): `gomap` - 256
  shards x (`sync.RWMutex` + `map[string][]byte`), GOGC=100; `gomap-gogc400`
  - the same with `debug.SetGCPercent(400)`; `syncmap` - `sync.Map`;
  `gobytes` - 256 shards x (`map[uint64]uint64` pointer-free index + 256 KiB
  `[]byte` chunks), the bigcache/freecache technique inside the Go heap;
  `goarena` - `store.Store` (256 shards, one shared arena with 64 MiB chunks).
* Workload: populate N keys (20-byte keys `key:%016x` of a bijective
  scramble, 128-byte values by default) with 16 goroutines, warm-up 3 s
  (E1, E1b) / 2 s (E4-E7) / 1 s (E2), measured mixed phase 15 s (E1, E1b),
  10 s (E4-E7) or 5 s (E2); 90 % reads / 10 % same-size overwrites, uniform
  keys unless stated. Every 16th operation is timed with RDTSCP
  (`internal/tsc`; `time.Now` has 0.5 ms resolution on Windows) into a
  log-linear histogram (<= 3.1 % quantile error). GC metrics are deltas over
  the measured phase (`gcstats`: runtime/metrics + MemStats); memory gauges
  at the end of the measured phase; RSS = Windows working set
  (K32GetProcessMemoryInfo); "GC CPU %" = GC CPU time / (GOMAXPROCS x wall
  time), the quantity the GC pacer targets.
* Fairness: same keys, value bytes, seeds, shards, goroutines, mix, warm-up
  and duration; maphash everywhere; no pre-sizing. **Reads:** every
  implementation returns the value by copying it into a reused
  per-goroutine buffer - goarena via `GetInto` (0 allocations, never `Get`),
  gomap/sync.Map by appending the stored slice to the buffer *outside* the
  lock (their values are immutable, so this is safe), gobytes by copying
  under its read lock. **Asymmetry to note:** the natural gomap API would
  return the stored slice without any copy; the harness adds the copy to
  gomap/sync.Map to give all implementations the same "caller owns the
  bytes" semantics (in the single-threaded E3 micro-benchmark `map` Get and
  `store.GetInto` are equal, 218 vs 225 ns, so the copy is cheap compared
  with the cache misses). **Writes:** gomap/sync.Map allocate a new value
  slice (and key string) per Set, as a map-based cache must; gobytes and
  goarena overwrite same-size values in place.
* Repeats: E1, E1b, E2 - 3 runs; E4-E7 - 1 run; the 4 KiB point of E4 was
  run once more (E4r) to verify an anomaly. Benchmark wall time: 29 m 44 s
  (E1-E6) + 9 m 10 s (E1b, E7) + 3 m 30 s (E7 re-run with forced-GC timing)
  + ~1 min (E4r) ~= 43 min, plus ~5 min of micro-benchmarks.

## 6. Results tables

### E1 - throughput, latency, GC and memory vs N (mean ± std of 3 runs)

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles | GC CPU % | STW total ms | max cycle STW ms | Go heap MiB | RSS MiB | off-heap MiB | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 1 M | 30.91 ± 0.36 | 0.39 | 1.46 | 15.44 | 6.73 | 28.3 | 1.94 ± 0.06 | 16.27 | 1.32 | 360 | 508 | 0 | 0.13 |
| gomap-gogc400 | 1 M | 30.00 ± 0.74 | 0.40 | 1.50 | 17.02 | 6.84 | 7.3 | 0.63 ± 0.02 | 3.65 | 1.10 | 662 | 1270 | 0 | 0.11 |
| syncmap | 1 M | 16.52 ± 0.34 | 0.71 | 1.61 | 9.34 | 96.16 | 14.0 | 10.77 ± 0.05 | 13.28 | 1.74 | 565 | 867 | 0 | 0.13 |
| gobytes | 1 M | 34.60 ± 1.97 | 0.35 | 1.09 | 16.04 | 0.52 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 242 | 216 | 0 | 0.06 |
| goarena | 1 M | 37.47 ± 1.67 | 0.32 | 1.01 | 14.42 | 0.64 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 2 | 231 | 224 | 0.05 |
| gomap | 2 M | 29.33 ± 1.21 | 0.41 | 1.54 | 16.04 | 17.08 | 13.7 | 2.63 ± 0.04 | 6.15 | 1.58 | 718 | 1001 | 0 | 0.26 |
| gomap-gogc400 | 2 M | 29.10 ± 0.33 | 0.42 | 1.61 | 16.34 | 10.72 | 3.7 | 0.82 ± 0.01 | 1.56 | 0.81 | 1258 | 2423 | 0 | 0.22 |
| syncmap | 2 M | 15.00 ± 0.19 | 0.77 | 1.72 | 13.87 | 91.48 | 6.3 | 11.58 ± 0.01 | 5.29 | 1.62 | 1315 | 1730 | 0 | 0.29 |
| gobytes | 2 M | 33.61 ± 0.99 | 0.37 | 1.12 | 14.68 | 0.45 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 410 | 431 | 0 | 0.12 |
| goarena | 2 M | 36.60 ± 1.02 | 0.33 | 1.05 | 14.16 | 1.27 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 2 | 450 | 448 | 0.10 |
| gomap | 5 M | 27.04 ± 0.26 | 0.44 | 1.61 | 17.02 | 13.52 | 5.7 | 4.05 ± 0.08 | 2.67 | 1.25 | 1711 | 2286 | 0 | 0.60 |
| gomap-gogc400 | 5 M | 26.90 ± 0.53 | 0.45 | 1.87 | 18.00 | 10.65 | 2.0 | 1.27 ± 0.08 | 0.96 | 0.76 | 1800 | 5050 | 0 | 0.56 |
| syncmap | 5 M | 13.21 ± 1.05 | 0.88 | 2.09 | 18.86 | 77.10 | 2.7 | 12.48 ± 0.22 | 2.88 | 1.93 | 3644 | 4307 | 0 | 0.65 |
| gobytes | 5 M | 30.88 ± 0.59 | 0.38 | 1.26 | 17.96 | 4.28 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 940 | 956 | 0 | 0.33 |
| goarena | 5 M | 34.67 ± 0.91 | 0.34 | 1.12 | 15.62 | 1.33 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 2 | 1070 | 1088 | 0.27 |
| gomap | 10 M | 24.39 ± 0.75 | 0.49 | 1.77 | 19.71 | 22.86 | 3.0 | 5.67 ± 0.42 | 1.48 | 0.96 | 3366 | 4103 | 0 | 1.38 |
| gomap-gogc400 | 10 M | 25.15 ± 0.49 | 0.49 | 2.12 | 18.56 | 15.27 | 0.7 | 3.62 ± 3.15 | 0.59 | 0.59 | 7656 | 7853 | 0 | 1.18 |
| syncmap | 10 M | 11.73 ± 1.22 | 0.99 | 2.33 | 19.63 | 76.04 | 1.0 | 13.14 ± 2.53 | 1.11 | 1.11 | 6339 | 6623 | 0 | 1.60 |
| gobytes | 10 M | 26.79 ± 5.44 | 0.43 | 1.59 | 29.14 | 2.12 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 1926 | 1922 | 0 | 0.65 |
| goarena | 10 M | 31.80 ± 1.49 | 0.37 | 1.27 | 19.71 | 2.28 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 2 | 2103 | 2112 | 0.57 |

E1 throughput, min - max over the 3 runs (Mops/s):

| impl | 1 M | 2 M | 5 M | 10 M |
|---|---|---|---|---|
| gomap | 30.62 - 31.32 | 27.94 - 30.06 | 26.85 - 27.34 | 23.58 - 25.07 |
| gomap-gogc400 | 29.29 - 30.76 | 28.72 - 29.34 | 26.28 - 27.21 | 24.69 - 25.67 |
| syncmap | 16.32 - 16.91 | 14.83 - 15.21 | 12.02 - 13.98 | 10.33 - 12.50 |
| gobytes | 32.32 - 35.80 | 32.47 - 34.25 | 30.20 - 31.24 | 20.51 - 30.01 |
| goarena | 35.72 - 39.06 | 35.84 - 37.76 | 33.67 - 35.43 | 30.13 - 32.99 |

goarena relative to the other implementations (E1 means):

| N | vs gomap throughput | vs gobytes throughput | vs gomap p99 | vs gomap RSS | vs gobytes RSS |
|---|---|---|---|---|---|
| 1 M | +21.2 % | +8.3 % | -30.8 % | -54.4 % | +7.4 % |
| 2 M | +24.8 % | +8.9 % | -31.5 % | -55.0 % | +4.4 % |
| 5 M | +28.2 % | +12.3 % | -30.4 % | -53.2 % | +11.9 % |
| 10 M | +30.4 % | +18.7 % | -28.2 % | -48.7 % | +9.4 % |

goarena memory breakdown (E1, MiB): reserved arena vs used slab blocks vs exact entry bytes:

| N | arena reserved | slab in use | entries (exact) | index | internal fragmentation | 64 MiB chunks | cgo calls (populate) | Go heap MiB |
|---|---|---|---|---|---|---|---|---|
| 1 M | 192 | 183 | 156 | 32 | 14.6 % | 3 | 3587 | 2.27 |
| 2 M | 384 | 366 | 313 | 64 | 14.6 % | 6 | 4102 | 2.27 |
| 5 M | 960 | 916 | 782 | 128 | 14.6 % | 15 | 4623 | 2.26 |
| 10 M | 1856 | 1831 | 1564 | 256 | 14.6 % | 29 | 5149 | 2.26 |

### E1b - GOMEMLIMIT = 4 GiB (debug.SetMemoryLimit), 3 runs

| impl | N | Mops/s | p99 µs | p99.9 µs | max ms | GC cycles | GC CPU % | mean mark ms (gctrace) | Go heap MiB | RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 21.09 ± 5.56 | 3.32 | 50.60 | 37.34 | 4.3 | 3.46 ± 0.38 | 156.7 | 1882 | 2233 |
| gobytes | 5 M | 27.13 ± 7.00 | 2.24 | 37.59 | 5.14 | 0.0 | 0.00 ± 0.00 | 0.0 | 931 | 937 |
| goarena | 5 M | 30.42 ± 5.16 | 1.89 | 29.01 | 1.64 | 0.0 | 0.00 ± 0.00 | 0.0 | 2 | 1071 |
| gomap | 10 M | 20.46 ± 5.85 | 3.34 | 46.76 | 39.52 | 4.0 | 6.61 ± 1.58 | 378.6 | 2946 | 3585 |
| gobytes | 10 M | 25.60 ± 5.39 | 2.52 | 36.52 | 1.91 | 0.0 | 0.00 ± 0.00 | 0.0 | 1842 | 1876 |
| goarena | 10 M | 30.88 ± 5.92 | 1.38 | 27.01 | 2.92 | 0.0 | 0.00 ± 0.00 | 0.0 | 2 | 2103 |

E1b individual runs (Mops/s, GC CPU %):

| repeat | impl | N | Mops/s | GC CPU % | GC cycles |
|---|---|---|---|---|---|
| 1 | gomap | 5 M | 15.58 | 3.18 | 4 |
| 1 | gobytes | 5 M | 19.06 | 0.00 | 0 |
| 1 | goarena | 5 M | 24.50 | 0.00 | 0 |
| 1 | gomap | 10 M | 13.72 | 4.80 | 2 |
| 1 | gobytes | 10 M | 19.73 | 0.00 | 0 |
| 1 | goarena | 10 M | 24.51 | 0.00 | 0 |
| 2 | gomap | 5 M | 20.99 | 3.32 | 4 |
| 2 | gobytes | 5 M | 31.60 | 0.00 | 0 |
| 2 | goarena | 5 M | 32.75 | 0.00 | 0 |
| 2 | gomap | 10 M | 24.25 | 7.33 | 5 |
| 2 | gobytes | 10 M | 26.74 | 0.00 | 0 |
| 2 | goarena | 10 M | 31.94 | 0.00 | 0 |
| 3 | gomap | 5 M | 26.69 | 3.89 | 5 |
| 3 | gobytes | 5 M | 30.74 | 0.00 | 0 |
| 3 | goarena | 5 M | 34.00 | 0.00 | 0 |
| 3 | gomap | 10 M | 23.41 | 7.70 | 5 |
| 3 | gobytes | 10 M | 30.33 | 0.00 | 0 |
| 3 | goarena | 10 M | 36.20 | 0.00 | 0 |

### E7 - cost of one GC cycle vs N (1 run each, gctrace + forced runtime.GC())

| impl | N | GC cycles (10 s) | mean concurrent mark ms | forced runtime.GC() ms | scannable heap MiB | Go heap MiB | GC CPU % | Mops/s |
|---|---|---|---|---|---|---|---|---|
| gomap | 1 M | 17 | 18.9 | 18.64 | 96.3 | 303 | 1.81 | 26.92 |
| syncmap | 1 M | 11 | 353.6 | 113.00 | 117.4 | 418 | 9.69 | 18.40 |
| gobytes | 1 M | 0 | 0.0 | 0.86 | 0.4 | 248 | 0.00 | 33.62 |
| goarena | 1 M | 0 | 0.0 | 0.59 | 0.4 | 2 | 0.00 | 29.12 |
| gomap | 2 M | 9 | 42.8 | 48.39 | 192.0 | 590 | 2.47 | 27.65 |
| syncmap | 2 M | 4 | 963.5 | 294.20 | 301.4 | 1065 | 12.17 | 14.66 |
| gobytes | 2 M | 0 | 0.0 | 1.39 | 0.5 | 430 | 0.00 | 23.63 |
| goarena | 2 M | 0 | 0.0 | 0.61 | 0.4 | 2 | 0.00 | 35.80 |
| gomap | 5 M | 4 | 153.0 | 177.29 | 384.5 | 1736 | 3.96 | 25.12 |
| syncmap | 5 M | 2 | 2765.5 | 723.15 | 724.6 | 3606 | 13.14 | 13.76 |
| gobytes | 5 M | 0 | 0.0 | 1.70 | 0.7 | 942 | 0.00 | 33.55 |
| goarena | 5 M | 0 | 0.0 | 0.51 | 0.4 | 2 | 0.00 | 34.83 |
| gomap | 10 M | 2 | 344.5 | 309.17 | 768.7 | 3762 | 5.44 | 25.55 |
| syncmap | 10 M | 1 | 5324.0 | 1508.43 | 1160.6 | 5829 | 11.71 | 12.01 |
| gobytes | 10 M | 0 | 0.0 | 2.59 | 1.1 | 1876 | 0.00 | 29.36 |
| goarena | 10 M | 0 | 0.0 | 0.61 | 0.4 | 2 | 0.00 | 29.10 |

### E2 - goarena chunk size, N = 5M (3 runs)

| chunk | populate s | cgo calls (populate) | chunks | arena reserved MiB | slab in use MiB | index MiB | RSS MiB | Mops/s (5 s) |
|---|---|---|---|---|---|---|---|---|
| 1 MiB | 0.292 ± 0.060 | 5533 | 925 | 925 | 916 | 128 | 1071 | 35.06 ± 3.32 |
| 4 MiB | 0.300 ± 0.060 | 4840 | 232 | 928 | 916 | 128 | 1070 | 35.33 ± 3.16 |
| 16 MiB | 0.259 ± 0.013 | 4666 | 58 | 928 | 916 | 128 | 1069 | 30.50 ± 7.89 |
| 64 MiB | 0.284 ± 0.038 | 4623 | 15 | 960 | 916 | 128 | 1071 | 28.62 ± 8.09 |
| 256 MiB | 0.283 ± 0.048 | 4612 | 4 | 1024 | 916 | 128 | 1071 | 32.46 ± 5.66 |

### E4 - value size, N = 2M

| impl | value B | runs | Mops/s | p99 µs | p99.9 µs | GC CPU % | Go heap MiB | RSS MiB | peak RSS MiB | private MiB | off-heap MiB | fragmentation | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 64 | 1 | 24.98 | 2.33 | 38.40 | 1.73 | 495 | 748 | 748 | 757 | 0 | 0.0 % | 0.28 |
| gobytes | 64 | 1 | 32.13 | 1.29 | 21.25 | 0.00 | 302 | 304 | 304 | 320 | 0 | 0.0 % | 0.14 |
| goarena | 64 | 1 | 34.67 | 1.10 | 21.76 | 0.00 | 2 | 313 | 313 | 350 | 320 | 16.7 % | 0.09 |
| gomap | 256 | 1 | 24.11 | 2.08 | 26.37 | 3.83 | 790 | 1506 | 1506 | 1518 | 0 | 0.0 % | 0.33 |
| gobytes | 256 | 1 | 27.48 | 1.39 | 24.83 | 0.00 | 686 | 670 | 670 | 706 | 0 | 0.0 % | 0.15 |
| goarena | 256 | 1 | 30.50 | 1.29 | 15.74 | 0.00 | 2 | 665 | 665 | 733 | 704 | 3.9 % | 0.16 |
| gomap | 1024 | 1 | 15.88 | 2.46 | 35.33 | 4.24 | 3177 | 4534 | 4534 | 4553 | 0 | 0.0 % | 0.59 |
| gobytes | 1024 | 1 | 17.16 | 2.21 | 24.32 | 0.00 | 2145 | 2128 | 2128 | 2170 | 0 | 0.0 % | 0.42 |
| goarena | 1024 | 1 | 16.83 | 2.65 | 38.40 | 0.00 | 2 | 2355 | 2355 | 2400 | 2368 | 10.5 % | 0.38 |
| gomap | 4096 | 1 | 0.77 | 52.73 | 3637.25 | 0.00 | 14777 | 1564 | 13412 | 15206 | 0 | 0.0 % | 2.20 |
| gobytes | 4096 | 1 | 6.64 | 9.60 | 111.61 | 0.00 | 8078 | 8072 | 8072 | 8126 | 0 | 0.0 % | 1.86 |
| goarena | 4096 | 1 | 6.65 | 10.11 | 121.86 | 0.00 | 2 | 9011 | 9011 | 9069 | 9024 | 9.1 % | 1.68 |

### E5 - goroutine scaling, N = 2M (Mops/s; p99 µs in parentheses)

| impl | G = 1 | G = 4 | G = 8 | G = 16 | speed-up 1->16 |
|---|---|---|---|---|---|
| gomap | 2.34 (1.00) | 9.60 (0.85) | 16.71 (1.04) | 28.75 (1.65) | 12.3x |
| gomap-gogc400 | 2.18 (1.01) | 9.50 (0.87) | 16.20 (1.10) | 24.77 (2.14) | 11.3x |
| syncmap | 1.22 (1.61) | 5.27 (1.46) | 9.44 (1.58) | 13.24 (1.84) | 10.8x |
| gobytes | 2.84 (0.74) | 11.10 (0.69) | 19.96 (0.81) | 23.79 (2.03) | 8.4x |
| goarena | 3.16 (0.68) | 11.91 (0.69) | 21.09 (0.81) | 29.57 (1.33) | 9.4x |

### E6 - skewed and write-heavy workloads, N = 5M

| impl | keys | reads | Mops/s | p99 µs | p99.9 µs | max ms | GC CPU % | GC cycles | Go alloc MiB/s | RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|
| gomap | uniform | 50 % | 17.48 | 4.54 | 35.33 | 50.87 | 13.77 | 13 | 1267 | 2337 |
| gomap-gogc400 | uniform | 50 % | 15.64 | 12.93 | 47.62 | 32.21 | 3.56 | 3 | 1134 | 5507 |
| syncmap | uniform | 50 % | 5.88 | 4.67 | 33.28 | 1705.22 | 29.09 | 3 | 673 | 5347 |
| gobytes | uniform | 50 % | 20.39 | 10.62 | 41.47 | 1.38 | 0.00 | 0 | 0 | 958 |
| goarena | uniform | 50 % | 22.69 | 9.09 | 35.33 | 1.91 | 0.00 | 0 | 0 | 1070 |
| gomap | zipf | 90 % | 15.93 | 9.86 | 101.38 | 61.45 | 2.17 | 3 | 231 | 2057 |
| gomap-gogc400 | zipf | 90 % | 15.45 | 10.11 | 115.71 | 3.19 | 0.00 | 0 | 224 | 4044 |
| syncmap | zipf | 90 % | 15.93 | 1.52 | 6.08 | 231.09 | 18.97 | 2 | 365 | 3079 |
| gobytes | zipf | 90 % | 14.95 | 10.88 | 133.12 | 2.90 | 0.00 | 0 | 0 | 954 |
| goarena | zipf | 90 % | 17.52 | 8.57 | 115.71 | 3.25 | 0.00 | 0 | 0 | 1072 |

### E3 - micro-benchmarks (go test -bench, -count 5, mean ± std)

| package | benchmark | ns/op | min - max | B/op | allocs/op |
|---|---|---|---|---|---|
| internal/cmem | BenchmarkGoCall | 1.90 ± 0.27 | 1.58 - 2.14 | 0 | 0 |
| internal/cmem | BenchmarkCgoCall | 63.62 ± 2.99 | 60.29 - 67.77 | 0 | 0 |
| internal/cmem | BenchmarkCgoCallParallel | 16.22 ± 0.31 | 15.86 - 16.62 | 0 | 0 |
| internal/cmem | BenchmarkMallocFree128 | 227.10 ± 21.05 | 200.80 - 247.20 | 0 | 0 |
| internal/cmem | BenchmarkGoMake128 | 44.72 ± 0.78 | 44.00 - 46.02 | 128 | 1 |
| internal/tsc | BenchmarkTicks | 19.16 ± 0.38 | 18.78 - 19.64 | 0 | 0 |
| arena | BenchmarkArenaAlloc128 | 7.69 ± 1.17 | 6.19 - 8.89 | 0 | 0 |
| arena | BenchmarkArenaAllocBytes128 | 11.91 ± 0.46 | 11.27 - 12.45 | 0 | 0 |
| arena | BenchmarkMakeBytes128 | 45.89 ± 3.04 | 43.12 - 49.46 | 128 | 1 |
| arena | BenchmarkSlabAllocFree128 | 15.83 ± 0.83 | 14.59 - 16.81 | 0 | 0 |
| arena | BenchmarkSlabAllocFreeMixed | 18.46 ± 0.45 | 17.87 - 18.94 | 0 | 0 |
| arena | BenchmarkNewValue | 50.81 ± 0.92 | 49.81 - 51.90 | 0 | 0 |
| arena | BenchmarkGoNew | 27.89 ± 0.86 | 26.73 - 28.93 | 24 | 1 |
| store | BenchmarkStoreGet | 225.36 ± 9.86 | 217.00 - 241.70 | 0 | 0 |
| store | BenchmarkMapGet | 218.16 ± 5.47 | 214.10 - 226.10 | 0 | 0 |
| store | BenchmarkStoreSet | 229.28 ± 4.78 | 224.50 - 236.90 | 0 | 0 |
| store | BenchmarkMapSet | 319.94 ± 17.14 | 297.80 - 337.20 | 144 | 2 |
| store | BenchmarkStoreGetParallel | 32.43 ± 4.17 | 25.40 - 35.66 | 0 | 0 |
| store | BenchmarkMapGetParallel | 31.01 ± 0.79 | 29.72 - 31.66 | 0 | 0 |
| store | BenchmarkStoreSetParallel | 23.94 ± 1.01 | 23.04 - 25.58 | 0 | 0 |
| store | BenchmarkMapSetParallel | 42.84 ± 2.74 | 39.97 - 46.10 | 144 | 2 |

### E4r - verification re-run of the 4 KiB point (N = 2M, 10 s)

| impl | Mops/s | p99 µs | p99.9 µs | max ms | GC CPU % | Go heap MiB | RSS MiB | peak RSS MiB |
|---|---|---|---|---|---|---|---|---|
| gomap | 2.18 | 13.18 | 462.85 | 978.14 | 9.47 | 14142 | 14135 | 14267 |
| gobytes | 6.48 | 18.69 | 165.89 | 1.73 | 0.00 | 8068 | 8062 | 8062 |
| goarena | 5.95 | 18.69 | 194.56 | 7.85 | 0.00 | 2 | 9009 | 9009 |

The collapse of `gomap` at 4 KiB values is reproducible (0.77 and 2.18
Mops/s): with 8.3 GiB live data and GOGC=100 the heap grows to 14-15 GiB;
in the first run Windows additionally trimmed the working set (RSS 1.5 GiB at
the end, peak 13.4 GiB, private 15.2 GiB, i.e. paging). gobytes and goarena
are equal within run-to-run noise at 4 KiB (6.64 vs 6.65 and 6.48 vs 5.95
Mops/s).

### Example service (dashboard screenshots; 4 load workers + 200 HTTP req/s, 2M keys x 256 B)

| backend | ops/s | p99 (1-s window) | Go heap | off-heap | RSS | GC CPU since start |
|---|---|---|---|---|---|---|
| goarena | 9.15 M | 871 ns | 1.8 MiB | 704 MiB | 671 MiB | 0.01 % |
| gomap | 7.71 M | 935 ns | 1.14 GiB (goal 1.43 GiB) | 0 | 1.47 GiB | 1.09 % |

(values as displayed in `fig4_dashboard*.png`; `/stats` snapshots taken
just before the screenshots: `results/stats_goarena.json`,
`results/stats_gomap.json`.) With the goarena backend the runtime still runs
~1.5 GC cycles/s, caused by the HTTP server's own allocations against the
4 MiB minimum heap goal, but each cycle is almost free (0.01 % CPU) because
nothing large is live in the Go heap.

## 7. Interpretation

**Where goarena wins and why**

1. *GC work no longer depends on N.* The store keeps ~260 small Go objects
   (Store, 256 shards, their slabs); the scannable Go heap stays at 0.36 MiB
   for every N (E7), so a full GC costs ~0.6 ms at 1M and at 10M entries,
   versus 309 ms for gomap at 10M (768 MiB scannable heap) and 1.5 s for
   sync.Map. During the measured phases goarena triggered no GC cycle at all
   (allocation rate 0.05-0.09 MiB/s versus 350-450 MiB/s for gomap).
2. *Throughput +21-30 % vs gomap, +8-19 % vs gobytes (E1).* A goarena Get
   touches one 16-byte index slot and one entry (2 cache misses); gomap's
   `map[string][]byte` touches the bucket, the key string and the value
   slice, and its writes allocate two heap objects (E3: 144 B and 2 allocs
   per Set; 320 vs 229 ns single-threaded, 42.8 vs 23.9 ns/op parallel).
   gobytes has a similar miss profile, but its Go-map index costs more at
   large N (its 10M point also has the largest spread, 20.5-30.0 Mops/s).
3. *Tail latency.* p99 is 28-31 % lower than gomap at every N; the maximum
   observed operation latency (mean of the per-run maxima) is 0.6-2.3 ms for
   goarena (single runs 0.3-4.4 ms) versus 6.7-22.9 ms for gomap (single
   runs 3.5-26.9 ms) and 76-96 ms for sync.Map (60-125 ms): GC assists and
   mark work on the request path; sync.Map additionally promotes/copies its
   dirty map.
4. *Memory.* RSS is about half of gomap's (no 2x heap-goal headroom, no
   per-entry object headers) and 3-4x smaller than gomap with GOGC=400 or
   sync.Map.
5. *Populate* is the fastest (0.57 s for 10M entries vs 1.38 s for gomap):
   bump allocation from spans, no per-entry Go allocation.
6. *Write-heavy load (E6, 50 % writes):* 22.7 Mops/s vs 17.5 (gomap, which
   now spends 13.8 % CPU in GC and allocates 1.27 GiB/s) and 5.9 (sync.Map,
   29 % GC CPU, max latency 1.7 s).

**Where goarena loses or ties, and why**

1. *Memory vs gobytes:* +4-12 % RSS (E1), +11.6 % at 4 KiB values (E4).
   Size-class rounding wastes 14.6 % for the 164-byte entries of E1 (class
   192 B; 3.9-16.7 % in E4 depending on the value size; 9.9 % is the model
   average for uniformly distributed sizes), plus the 16-byte index slot at
   load factor <= 0.75; gobytes packs entries exactly (and never frees in
   place). The arena over-reservation itself is small: reserved - used =
   9-44 MiB (less than one 64 MiB chunk plus span tails).
2. *Large values (E4, 1 KiB and 4 KiB):* the advantage disappears because
   copying the value dominates; goarena and gobytes tie (16.83 vs 17.16
   Mops/s at 1 KiB; 6.65 vs 6.64 and 5.95 vs 6.48 at 4 KiB), and at 1 KiB
   goarena's p99.9 is worse (38.4 vs 24.3 µs).
3. *Hot keys (E6, Zipf s = 1.1):* all RWMutex-sharded designs (gomap,
   gobytes, goarena) suffer from cache-line contention on the reader counters
   of hot shards: p99.9 101-133 µs, whereas sync.Map's lock-free read path
   gives 6.1 µs. goarena still has the best throughput (17.5 Mops/s), but
   not the best tail.
4. *Single-threaded reads (E3)* equal a Go map (225 vs 218 ns), and the
   typed helper `NewValue[T]` (50.8 ns) is slower than `new(T)` (27.9 ns)
   because it takes the arena mutex and zeroes memory. `Arena.Alloc` alone
   (7.7 ns) is 6x faster than `make([]byte,128)` (45.9 ns), and
   `Slab.Alloc+Free` (15.8 ns) is 14x faster than C `malloc+free` (227 ns).
5. *A cgo call is expensive:* 43-68 ns per empty call, and calls from 16
   goroutines scale poorly (16.2 ns/op aggregate = ~260 ns per call per
   goroutine). A design that crossed into C per operation would lose;
   goarena works because it amortises: one cgo call per 64 MiB chunk or per
   index resize (E2: 4 612-5 533 cgo calls to insert 5M entries, of which
   only 4-925 are chunk allocations; most are index-table growth).
6. *Chunk size (E2)* has no measurable effect on populate time (0.26-0.30 s,
   differences within noise) between 1 and 256 MiB; it changes the number of
   cgo calls (5 533 -> 4 612) and the over-reservation (925 vs 1024 MiB
   reserved for 916 MiB in use). 64 MiB is a compromise between cgo
   amortisation and over-reservation for small stores. The E2 throughput
   column (5-s runs) varies 28.6-35.3 Mops/s with no trend - noise.
7. *GOMEMLIMIT (E1b):* the chapter-2 prediction that a 4 GiB limit drives
   the Go-heap store's GC CPU share towards the 50 % limiter cap did **not**
   materialise at these data sizes: gomap's live heap (1.9 GiB at 5M,
   2.9 GiB at 10M) stays below the limit, so GC CPU was 3.46 ± 0.38 % at 5M
   (4.05 % without limit, i.e. unchanged within noise) and 6.61 ± 1.58 % at
   10M (5.67 % without limit) with 4.0-4.3 cycles per 15 s. goarena and
   gobytes are unaffected (0 cycles). In E1b repeat 1 all three
   implementations were 20-35 % slower than in repeats 2-3 (external
   disturbance, probably OS background activity); the per-run table shows
   that the ordering goarena > gobytes > gomap holds in every repeat.
8. *Scaling (E5):* goarena is the fastest at every G (3.16 Mops/s at G = 1,
   29.6 at G = 16), but its 1->16 speed-up (9.4x) is lower than gomap's
   (12.3x) because gomap's single-thread baseline is slower; with 16
   goroutines on 8 physical cores, SMT and memory bandwidth limit everyone.
9. *Measurement limits:* on Windows the runtime's own pause timings
   (MemStats, pause histogram, gctrace) are quantised to ~0.5 ms ticks, so
   the "max STW pause" values (0.6-1.9 ms for the Go-heap implementations)
   are upper bounds; the latency impact is therefore reported through the
   TSC-timed operation latencies. Laptop CPU boost causes 3-8 % run-to-run
   variation (std columns).

## 8. Alignment with chapter 2 (coordinator requests)

1. *Shard reservation (formula 2.17):* implemented as the **shared chunk
   pool** variant - one `arena.Arena` per store shared by all 256 shards;
   shards take 64 KiB spans ("pages" B = 64 KiB) from it. Reserved vs used
   (E1, arena reserved / slab blocks in use): 192/183, 384/366, 960/916,
   1856/1831 MiB for 1M/2M/5M/10M entries; the index adds 32-256 MiB.
   See DESIGN_NOTES.md §4.4.
2. *Fair reads:* goarena is read with `GetInto` and a reused buffer; the
   other implementations copy into a reused buffer too (asymmetry note in
   §5).
3. *Size classes:* exactly `c(i+1) = 8*ceil(1.25*c(i)/8)` from 16 B to 64 KiB
   -> **36 classes** (list in DESIGN_NOTES.md §5.1). Measured internal
   fragmentation: 14.6 % (E1, 164-byte entries); 16.7 % / 3.9 % / 10.5 % /
   9.1 % for 64 / 256 / 1024 / 4096-byte values (E4); 9.9 % is the
   theoretical mean for uniformly distributed sizes.
4. *E1b (GOMEMLIMIT = 4 GiB) and mark time:* done (3 repeats, gctrace
   parsed). The mark-phase duration grows linearly with N for gomap: 18.9 /
   42.8 / 153 / 345 ms mean concurrent mark at 1/2/5/10M (E7; E1b: 157 and
   379 ms at 5M/10M) - close to the model's ~0.28 s at 10M; a full forced GC
   with goarena takes 0.5-0.6 ms at every N (model: ~1 ms). The predicted
   climb of GC CPU under GOMEMLIMIT was not observed (§7.7).
5. *cgo vs Go call:* empty cgo call **63.6 ± 3.0 ns** (runs range 43-68 ns),
   empty Go call **1.90 ± 0.27 ns** (chapter 1 literature: ~40 ns vs
   ~1.5 ns).

## 9. Figures for chapter 4 (`work/figures`, scripts in `work/figscripts`)

| file | proposed caption (UA) | description |
|---|---|---|
| `fig4_e1_throughput.png` | Рисунок 4.x – Пропускна здатність реалізацій залежно від кількості записів N | E1: mean of 3 runs, error bars = min..max; goarena is highest at every N. |
| `fig4_e1_latency.png` | Рисунок 4.x – Затримка операцій (99-й і 99,9-й процентилі) залежно від N | E1: goarena's p99 is 28-31 % below gomap; p99.9 is similar for all. |
| `fig4_e1_gccpu.png` | Рисунок 4.x – Частка процесорного часу на збирання сміття залежно від N | E1: GC CPU grows with N for gomap and sync.Map and is zero for goarena and gobytes. |
| `fig4_e1_gcpause.png` | Рисунок 4.x – Кількість циклів збирання сміття та найдовша пауза STW | E1: GC cycles in 15 s and the longest per-cycle STW pause (Windows 0.5 ms timer granularity). |
| `fig4_e1_memory.png` | Рисунок 4.x – Обсяг керованої купи Go та резидентної пам'яті процесу | E1: Go heap objects and RSS vs N; goarena's data is off-heap (dashed line). |
| `fig4_e1_ccdf.png` | Рисунок 4.x – Розподіл «хвоста» затримок операцій при N = 5 млн | E1: complementary CDF of all sampled operation latencies (3 runs merged), log-log. |
| `fig4_e7_mark.png` | Рисунок 4.x – Тривалість циклу збирання сміття залежно від N | E7: forced runtime.GC() wall time (TSC) and mean concurrent mark time (gctrace); linear growth for Go-heap maps, constant ~0.6 ms for goarena. |
| `fig4_e1b_memlimit.png` | Рисунок 4.x – Вплив обмеження GOMEMLIMIT = 4 ГіБ на частку ЦП для GC і пропускну здатність | E1b vs E1: GC CPU without/with the limit, throughput with the limit. |
| `fig4_e2_chunk.png` | Рисунок 4.x – Вплив розміру чанка на час заповнення, кількість викликів CGO та резервування пам'яті | E2: populate time, cgo calls (chunk allocations highlighted), reserved vs used arena memory. |
| `fig4_e3_micro.png` | Рисунок 4.x – Результати мікробенчмарків: стандартні засоби Go та goarena/CGO | E3: ns/op of paired micro-benchmarks (Go call vs cgo call, make vs Arena.Alloc, map vs store ...), log scale. |
| `fig4_e4_value.png` | Рисунок 4.x – Вплив розміру значення на пропускну здатність і резидентну пам'ять | E4: N = 2M, values 64-4096 B; the advantage shrinks for large values; gomap collapses at 4 KiB. |
| `fig4_e5_goroutines.png` | Рисунок 4.x – Масштабування пропускної здатності та затримки p99 за кількістю горутин | E5: G = 1, 4, 8, 16 at N = 2M. |
| `fig4_e6_workloads.png` | Рисунок 4.x – Результати для нерівномірного (Zipf) та інтенсивного за записом навантажень | E6: throughput, p99.9 and GC CPU for Zipf/90 % reads and uniform/50 % writes. |
| `fig4_dashboard.png` | Рисунок 4.x – Панель моніторингу прикладу httpcache з бекендом goarena | Real headless-Chrome screenshot (1400x900) after 2 min of self-load: Go heap 1.8 MiB vs 704 MiB off-heap. |
| `fig4_dashboard_gomap.png` | Рисунок 4.x – Панель моніторингу прикладу httpcache з бекендом gomap (для порівняння) | Same load with map[string][]byte: Go heap saw-tooth 0.9-1.4 GiB and regular GC CPU spikes. |
| `fig4_term_test.png` | Рисунок 4.x – Результати виконання модульних тестів (go test -v) | Real log: last 33 PASS/ok lines of `go test -count=1 -v ./...`. |
| `fig4_term_race.png` | Рисунок 4.x – Перевірка детектором гонок даних (go test -race) | Real log: all packages ok, concurrency tests passed, no DATA RACE. |
| `fig4_term_cover.png` | Рисунок 4.x – Покриття коду тестами за пакетами | Real log of `go test -coverprofile`: 86.9-98.3 % per package. |
| `fig4_term_fuzz.png` | Рисунок 4.x – Фазинг-тестування сховища (FuzzStore) | Real log: 1.85 M executions in 90 s, 16 workers, PASS. |
| `fig4_term_bench.png` | Рисунок 4.x – Вивід мікробенчмарків go test -bench | Real `-count 1` log (tabwriter padding squeezed): Go call 1.6 ns vs cgo call 43 ns, etc. |
| `fig4_term_vet_build.png` | Рисунок 4.x – Статичний аналіз (go vet) і збирання проєкту | Real log: toolchain versions, vet/build/gofmt clean, binary sizes. |

Figure scripts: `work/figscripts/fig4_common.py` (style, colours, loading),
`fig4_e1.py`, `fig4_experiments.py` (E1b, E2, E4, E5, E6, E7),
`fig4_micro.py`, `fig4_terms.py` (calls `goarena/tools/term2png.py`).

## 10. Files suitable for appendix listings (Додаток А)

| file | lines | content |
|---|---|---|
| `internal/cmem/cmem.go` | 173 | cgo bridge: malloc/calloc/free with header, counters |
| `arena/arena.go` | 464 | Arena, Ref, chunk table, bump allocation, Reset/Release |
| `arena/slab.go` | 317 | size classes, free lists, spans, large objects, statistics |
| `arena/typed.go` | 120 | NewValue/NewSlice with the pointer check |
| `store/store.go` | 550 | Store API, shards, Set/Get/GetInto/View/Delete/Close |
| `store/index.go` | 167 | off-heap open-addressing index (linear probing, tombstones) |
| `store/entry.go` | 78 | entry layout helpers |
| `store/stats.go` | 91 | statistics |
| `gcstats/gcstats.go` | 295 | runtime/metrics snapshots and deltas |
| `gcstats/process_windows.go` | 55 | RSS / CPU time via kernel32 (syscall only) |
| `internal/latency/latency.go` | 203 | log-linear histogram |
| `internal/tsc/tsc.go`, `tsc_amd64.s` | 53 + 11 | RDTSCP clock |
| `cmd/goarena-bench/impl.go` | 272 | compared implementations (gomap, syncmap, gobytes, goarena) |
| `cmd/goarena-bench/workload.go` | 362 | workload, sampling, run phases |
| `cmd/goarena-bench/driver.go` | 281 | experiments E1-E7, process-per-run driver |
| `cmd/goarena-bench/aggregate.go` | 327 | CSV aggregation, gctrace parsing |
| `examples/httpcache/server.go` | 294 | HTTP handlers, /stats sampler |
| `examples/httpcache/dashboard.html` | 221 | dashboard (canvas charts) |
| `store/fuzz_test.go` | 175 | differential fuzz test |
| `store/stress_test.go` | 160 | concurrency stress tests |
| `store/leak_test.go` | 100 | leak / finalizer tests |
| `tools/term2png.py` | 204 | log-to-PNG renderer |

For Додаток Б (test and load-test results): `logs/test.txt`, `logs/race.txt`
(or the screenshots), `logs/cover.txt`, the head of `logs/cover_func.txt`,
`logs/fuzz.txt`, `logs/microbench.txt` and the tables of section 6. Додаток В
(installation and usage) can be based on README.md (Install, Quick start,
API, Safety rules).

## 11. Deviations and caveats (details in DESIGN_NOTES.md §12-13)

* One shared arena per store (not per shard) with 64 KiB slab spans;
  same-size index rehash when tombstones dominate; large objects as arena
  dedicated chunks; `MaxMemory` covers entry memory, not index tables.
* E4 compares gomap, gobytes and goarena only (memory budget of the machine).
* Extra experiments beyond the spec: E1b (GOMEMLIMIT), E6 (Zipf / 50 %
  writes), E7 (GC cycle cost via gctrace and forced GC), E4r (verification).
* Windows specifics: 0.5 ms runtime timer resolution (TSC used for
  latency); `go test -cover` failed with a large zero-initialised global
  array under external linking (worked around, DESIGN_NOTES §13.1).
* All benchmark processes, the example server and headless Chrome were
  stopped after the measurements (verified: no `goarena-bench`, `httpcache`
  or headless-Chrome process left).
