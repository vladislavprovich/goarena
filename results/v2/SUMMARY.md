# goarena - results summary, v0.2.0 (for chapters 3, 4, 5 and the conclusions)

All numbers in this file come from real runs on the machine below; nothing
was estimated, adjusted or copied from literature. **v0.2.0 numbers are
primary.** The complete v0.1.0 results (summary, raw JSON, logs, figures) are
preserved unchanged in `results/v1/`; section 7 compares the two versions.

* raw data: `results/raw/*.json` - one file per benchmark run (230 runs in
  the v0.2.0 session), `*.stderr.txt` gctrace side files (E1b, E7, E8),
  `*.skipped.txt` reasons for E8 runs not executed;
* aggregated: `results/runs.csv`, `results/summary.csv` (mean, std, min,
  max per configuration), `results/microbench.csv` (E3);
* console logs: `results/logs/*.txt`; tables of section 6 are generated
  from the CSV files by `scripts/make_tables.py` (also `results/tables.md`);
* exploratory runs of the optimisation round: `results/optimization_runs/`.

## 1. Environment

| item | value |
|---|---|
| CPU | AMD Ryzen 7 7735HS with Radeon Graphics, 8 cores / 16 threads (laptop CPU, boost enabled); invariant TSC 3.19 GHz (0.3135 ns/tick) |
| RAM | 32 GB |
| OS | Windows 10 Pro 22H2, build 10.0.19045 |
| Go | go1.24.6 windows/amd64, GOMAXPROCS = 16, GOGC = 100 unless stated |
| C compiler | gcc 14.2.0 (x86_64-posix-seh-rev1, MinGW-Builds / MinGW-w64), CGO_ENABLED = 1 |
| Python | 3.8.10, matplotlib 3.7.5, numpy 1.24.4, pandas 2.0.3, Pillow 9.5.0 (figures only) |
| Browser | Google Chrome, headless (`--headless=new`), for the dashboard screenshots |
| Module | `github.com/savokh/goarena` v0.2.0, standard library only, `go 1.22` in go.mod |
| Date | 13.10.2026; v0.2.0 benchmark session 19:55-21:01 plus supplement 21:05-21:12 (BENCH_RUNNING.flag protocol) |

## 2. How to reproduce

```powershell
cd D:\master-diploma\goarena
$env:PATH = "C:\Users\Vlad\mingw64\bin;$env:PATH"; $env:CGO_ENABLED = "1"
.\build.ps1 -Race -Cover                      # vet, tests, race, coverage, build
.\scripts\run_tests.ps1 -FuzzTime 90s         # all test logs into results\logs
bash scripts/run_all_v2.sh                    # E1, E1b, E2, E4, E5, E6, E7, E3, E8, E9, E10 (~70 min)
bash scripts/run_v2_supplement.sh             # E3 re-run + tuned goarena variants in E9/E10
.\scripts\screenshot_dashboard.ps1 -Backend goarena -WaitSeconds 125 -Out ..\work\figures\fig4_dashboard.png
.\scripts\screenshot_dashboard.ps1 -Backend gomap   -WaitSeconds 125 -Out ..\work\figures\fig4_dashboard_gomap.png
python scripts\make_tables.py; python scripts\count_loc.py . results\loc.csv
cd ..\work\figscripts; python fig4_e1.py; python fig4_experiments.py; python fig4_micro.py
python fig4_v1v2.py; python fig4_target.py; python fig4_terms.py
```

Single run: `bin\goarena-bench.exe run -impl goarena -n 5000000 -duration 15s -warmup 3s`.
Every command actually executed heads its block in `results/logs/bench_matrix.txt`.

## 3. Headline numbers (v0.2.0)

* **Throughput (E1, 16 goroutines, 90 % reads, 20 B keys, 128 B values):**
  goarena 34.96 ± 2.50 Mops/s at N = 10M vs gobytes 28.68 (+21.9 %),
  gomap GOGC=400 24.00, gomap 21.54 ± 5.50 (+62 %; one gomap run was slow,
  15.3 Mops/s; the gomap range is 15.3-25.7), sync.Map 12.34. At N = 1M:
  40.54 vs 34.07 (gobytes, +19.0 %) and 30.86 (gomap, +31.4 %).
* **Latency (E1, N = 10M):** p99 0.91 µs (gobytes 1.34, gomap 2.55),
  **p99.9 2.39 µs** (gobytes 16.81, sync.Map 12.93, gomap 38.10).
* **Hot keys (E6, Zipf s = 1.1, N = 5M):** goarena **48.66 Mops/s, p99.9
  1.01 µs**; sync.Map 22.12 Mops/s / 5.44 µs; gobytes 19.71 / 82.9 µs.
* **Memory (E1, N = 10M):** RSS goarena **1796 MiB**, gobytes 1902 (-5.6 %),
  gomap 4133 (-56.6 %), gomap GOGC=400 7190, sync.Map 6375. At 5M: 904 vs
  968 (gobytes). Internal fragmentation 2.5 % (v0.1.0: 14.6 %).
* **GC:** in E1-E8 goarena ran 0-1 GC cycles per measured phase (Go heap
  <= 3.3 MiB); one full forced GC takes 0.27-0.70 ms at every N from 1M to 50M,
  versus 26 ms (1M) -> 316 ms (10M) -> 0.75-1.13 s (20M) -> 2.0-23 s (50M)
  for gomap and 104 ms -> 1.5 s (10M) -> 3.2-3.4 s (20M) for sync.Map.
* **Large scale (E8, N = 50M):** goarena 25.57 Mops/s, p99.9 5.05 µs, RSS
  8.5 GiB; gobytes 20.12 / 29.7 µs / 9.0 GiB; gomap 18.40 / 23.0 µs /
  15.0 GiB; gomap GOGC=400 and sync.Map not run at 30M/50M (projected
  memory > 24 GiB budget).
* **Where goarena loses:** (a) request-garbage service (E9): with default
  GOGC the tiny Go heap triggers 358-378 GC cycles/s, throughput 4.73 vs
  9.45 Mops/s for gobytes; with GOGC=400 7.70, and in the container-budget
  experiment (E10) the budget-aware configuration (GOGC=off, GOMEMLIMIT =
  budget - off-heap) reaches 8.59 vs 9.59 Mops/s for gobytes; (b) extreme
  tail: 0.7-12 sampled operations per million above 1 ms (gobytes 0-1),
  max 7-24 ms; (c) values of 256 B and more: 2.7-16.6 % more RSS than gobytes (E4).
* **cgo:** empty cgo call **38.38 ± 1.11 ns** vs empty Go call **1.37 ±
  0.01 ns** (E3, -count 5, v0.2.0 session); across all sessions on this
  laptop the cgo call measured 38-68 ns and the Go call 1.4-1.9 ns
  (v0.1.0 session: 63.6 / 1.90 ns; v0.2.0 -count 1: 47.0 / 1.83 ns), i.e. a
  factor of 23-36. goarena v0.2.0 makes **28 cgo calls to load 10M entries**
  (one per 64 MiB chunk; the index is now part of the arena) and **0 in the
  measured phase**.

## 4. Code, tests and quality

### 4.1 Size (`scripts/count_loc.py`, `results/loc.csv`)

| package | files | source lines (code) | test lines (code) |
|---|---|---|---|
| `internal/cmem` | 3 | 213 (121) | 189 (165) |
| `internal/tsc` | 6 | 95 (51) | 44 (40) |
| `internal/latency` | 2 | 203 (151) | 94 (88) |
| `arena` | 9 | 1120 (785) | 856 (779) |
| `store` | 15 | 1162 (780) | 1383 (1236) |
| `gcstats` | 6 | 422 (331) | 85 (74) |
| `cmd/goarena-bench` | 7 | 1698 (1472) | 313 (298) |
| `examples/httpcache` | 5 | 683 (566) | 169 (159) |
| root (`doc.go`) | 1 | 23 (2) | 0 |
| **total (Go + asm)** | **54** | **5619 (4259)** | **3133 (2839)** |

"code" = non-blank, non-comment lines. Not included: `dashboard.html` (221
lines), Python/PowerShell/Bash tooling, documentation.

### 4.2 Tests (final run after all changes, `results/logs/`)

| package | Test* | Example* | Fuzz* | Benchmark* | coverage |
|---|---|---|---|---|---|
| `arena` | 19 | 3 | - | 7 | 98.1 % |
| `store` | 21 | 5 | 1 | 8 | 99.3 % |
| `gcstats` | 4 | - | - | - | 96.7 % |
| `internal/cmem` | 6 | - | - | 5 | 100.0 % |
| `internal/latency` | 4 | - | - | 1 | 91.4 % |
| `internal/tsc` | 2 | - | - | 1 | 87.5 % |
| `cmd/goarena-bench` | 8 | - | - | - | 88.1 % |
| `examples/httpcache` | 4 | - | - | - | 94.2 % |
| **total** | **68** | **8** | **1** | **22** | **93.8 %** of all statements |

* `go test -count=1 -v ./...`: 77 top-level tests (68 Test, 8 Example, the
  FuzzStore seed corpus) and 14 subtests, **all PASS** (`test_v.txt`,
  `test.txt`, re-check after the last formatting `test_final.txt`).
* `go test -race -count=1 -v ./...`: **all PASS, 0 data races**
  (`race.txt`), including the new seqlock tests (the race detector does not
  instrument C memory; all Go-memory state shared with lock-free readers is
  atomic; `checkptr` reported nothing).
* Coverage `coverage.out`, `cover.txt`, `cover_func.txt`: 93.8 % total.
* Fuzzing: `go test -run XXX -fuzz FuzzStore -fuzztime 90s
  -fuzzminimizetime 5s ./store` - **2 329 310 executions in 90 s, 16
  workers, 8 new interesting inputs (200 in the corpus), PASS** (`fuzz.txt`).
* New v0.2.0 tests: `TestOptimisticReadsNeverTorn` (3 writers relocate
  CRC32-checked values across size classes and into the large-object path,
  insert/delete churn forces rehashes and table donation; 12 readers;
  ~74 M reads per 1.5 s run, 0 torn values, 0 missing pinned keys),
  `TestHotKeyReaders`, `TestReadOptimisticRejectsGarbage`, `TestShardLayout`,
  `TestHybridClassTable`, `TestSlabRetainLargeAndDonate`, `TestTryPointer`,
  `TestGoMemoryBudget`, `TestMallocFailureReturnsNil`.
* Bugs found and fixed during v0.2.0 (before the reported runs unless
  stated): (1) the first optimistic reader re-read the entry header for the
  copy, so a concurrently rewritten header could yield a garbage length -
  `TestOptimisticReadsNeverTorn` crashed with an access violation within
  seconds; fixed by reading header fields once (DESIGN_NOTES 8.3);
  (2) `cmem` used cgo's `C.malloc`, which never returns NULL but aborts the
  process - `ErrOutOfMemory` on C allocation failure was unreachable; fixed
  with own `malloc`/`calloc` wrappers (found after the main matrix when
  `BenchmarkArenaAlloc128` exhausted memory; the change only affects the
  failure path of chunk allocation, so E1-E10 results are unaffected; E3 was
  re-run); (3) the arena micro-benchmarks grew without bound (now Reset
  every 1M allocations); (4) `TestSnapshotAndDelta` assumed non-zero GC
  pauses, which the 0.5 ms Windows clock sometimes reports as 0.
* `go vet ./...` clean, `gofmt -l .` 0 files, `go build ./...` ok
  (`vet_build.txt`).

## 5. Benchmark methodology (short)

* Implementations, each run in a **separate OS process**: `gomap` (256 x
  `sync.RWMutex` + `map[string][]byte`, GOGC=100), `gomap-gogc400` (same,
  `debug.SetGCPercent(400)`), `syncmap` (`sync.Map`), `gobytes` (256 x
  pointer-free `map[uint64]uint64` index + 256 KiB `[]byte` chunks, the
  bigcache/freecache technique in the Go heap), `goarena` (`store.Store`, 256
  shards, 64 MiB chunks). **The baseline code is identical in v0.1.0 and
  v0.2.0** (only goarena changed). Two *additional* goarena configurations
  are used only in E9/E10: `goarena-gogc400` (the same GOGC=400 knob as
  `gomap-gogc400`) and `goarena-budget` (GOGC=off, GOMEMLIMIT = container
  budget - off-heap bytes via `Store.GoMemoryBudget`).
* Workload: populate N keys (20-byte keys, 128-byte values unless stated)
  with 16 goroutines, warm-up 3 s (E1, E1b, E8, E9, E10) / 2 s (E4-E7) /
  1 s (E2), measured phase 15 s (E1, E1b, E8, E9, E10), 10 s (E4-E7), 5 s
  (E2); 90 % reads / 10 % same-size overwrites, uniform keys unless stated.
  Every 16th operation timed with RDTSCP into a log-linear histogram
  (<= 3.1 % quantile error). GC metrics are deltas over the measured phase;
  "GC CPU %" = GC CPU time / (GOMAXPROCS x wall time); RSS = Windows working
  set; "full GC" = TSC-timed `runtime.GC()` after the measured phase.
* Fairness: same keys, value bytes, seeds, shards, goroutines, mix, warm-up,
  duration, maphash everywhere, no pre-sizing. Every implementation returns
  the value by copying it into a reused per-goroutine buffer (goarena via
  `GetInto`). *Asymmetry:* gomap/sync.Map could return the stored slice
  without a copy; the harness adds the copy (outside the lock) to give all
  implementations "caller owns the bytes" semantics. Writes of gomap/sync.Map
  allocate a new value (and key string), gobytes and goarena overwrite
  same-size values in place.
* E9/E10 request garbage: before every operation the worker allocates a
  request object (struct with 3 pointer fields + key copy + 1 KiB payload)
  that stays reachable for the next 32 operations; the recorded latency
  covers allocation + cache operation. E10 applies `debug.SetMemoryLimit`
  = 1.25 x gobytes' E1 RSS at N = 5M (968 MiB) = **1210 MiB** to every
  implementation; goarena's C memory is not counted by GOMEMLIMIT, which is
  why RSS is reported for all and `goarena-budget` subtracts it explicitly.
* E8 memory guard: projected private memory = private bytes at 10M (v0.1.0
  E1) x N/10M x 1.15; runs above 24 GiB were skipped and recorded.
* Repeats: E1, E1b, E2, E9, E10 - 3; E8 - 2; E4-E7 - 1 (as in v0.1.0).
  v0.2.0 session wall time: 39 m 14 s (E1-E7) + ~2 min (E3) + 21 m (E8, E9)
  + 4 m 49 s (E10) + 6 min supplement (E3 re-run, tuned variants).

## 6. Results tables (v0.2.0 session)

### E1 - throughput, latency, GC and memory vs N (mean ± std of 3 runs)

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles | GC CPU % | STW total ms | max cycle STW ms | Go heap MiB | RSS MiB | off-heap MiB | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 1 M | 30.86 ± 0.61 | 0.38 | 1.51 | 14.21 | 7.15 | 28.3 | 1.92 ± 0.01 | 13.83 | 1.59 | 301 | 510 | 0 | 0.13 |
| gomap-gogc400 | 1 M | 30.87 ± 0.95 | 0.40 | 1.53 | 13.74 | 5.81 | 7.3 | 0.63 ± 0.01 | 5.13 | 1.65 | 817 | 1272 | 0 | 0.11 |
| syncmap | 1 M | 16.75 ± 0.65 | 0.71 | 1.67 | 9.58 | 130.92 | 14.3 | 10.30 ± 0.27 | 11.82 | 1.72 | 678 | 856 | 0 | 0.14 |
| gobytes | 1 M | 34.07 ± 1.18 | 0.35 | 1.15 | 14.25 | 1.64 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 236 | 218 | 0 | 0.06 |
| goarena | 1 M | 40.54 ± 2.62 | 0.32 | 0.82 | 1.95 | 13.74 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 3 | 196 | 192 | 0.05 |
| gomap | 2 M | 28.99 ± 1.94 | 0.41 | 1.56 | 16.25 | 10.60 | 13.3 | 2.71 ± 0.11 | 7.52 | 1.33 | 882 | 1006 | 0 | 0.25 |
| gomap-gogc400 | 2 M | 28.55 ± 0.92 | 0.42 | 1.71 | 15.66 | 10.07 | 3.7 | 0.85 ± 0.08 | 3.57 | 1.37 | 1257 | 2454 | 0 | 0.24 |
| syncmap | 2 M | 14.54 ± 0.26 | 0.79 | 1.84 | 12.07 | 199.96 | 6.7 | 11.67 ± 0.38 | 4.91 | 1.43 | 1185 | 1770 | 0 | 0.26 |
| gobytes | 2 M | 33.19 ± 2.25 | 0.37 | 1.12 | 14.14 | 10.05 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 422 | 432 | 0 | 0.12 |
| goarena | 2 M | 39.13 ± 1.75 | 0.33 | 0.81 | 1.91 | 10.25 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 3 | 381 | 384 | 0.10 |
| gomap | 5 M | 24.30 ± 1.98 | 0.46 | 1.94 | 23.76 | 22.24 | 5.3 | 4.06 ± 0.14 | 3.99 | 1.24 | 1721 | 2285 | 0 | 0.58 |
| gomap-gogc400 | 5 M | 25.39 ± 3.54 | 0.46 | 2.15 | 23.25 | 16.65 | 1.3 | 1.51 ± 0.34 | 1.02 | 1.02 | 3796 | 4995 | 0 | 0.57 |
| syncmap | 5 M | 12.98 ± 0.61 | 0.89 | 2.06 | 17.62 | 301.42 | 2.0 | 11.93 ± 0.48 | 2.16 | 1.50 | 3722 | 4319 | 0 | 0.69 |
| gobytes | 5 M | 28.77 ± 0.18 | 0.39 | 1.47 | 23.64 | 3.24 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 958 | 968 | 0 | 0.30 |
| goarena | 5 M | 35.15 ± 3.00 | 0.35 | 0.86 | 2.75 | 12.74 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 3 | 904 | 960 | 0.26 |
| gomap | 10 M | 21.54 ± 5.50 | 0.51 | 2.55 | 38.10 | 27.61 | 2.7 | 5.12 ± 0.52 | 1.70 | 1.03 | 3240 | 4133 | 0 | 1.31 |
| gomap-gogc400 | 10 M | 24.00 ± 1.22 | 0.50 | 2.20 | 21.80 | 18.04 | 1.0 | 4.88 ± 0.94 | 0.52 | 0.52 | 7009 | 7190 | 0 | 1.09 |
| syncmap | 10 M | 12.34 ± 0.41 | 0.99 | 2.13 | 12.93 | 75.89 | 1.0 | 12.23 ± 0.26 | 1.03 | 1.03 | 6109 | 6375 | 0 | 1.69 |
| gobytes | 10 M | 28.68 ± 1.83 | 0.42 | 1.34 | 16.81 | 1.59 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 1891 | 1902 | 0 | 0.62 |
| goarena | 10 M | 34.96 ± 2.50 | 0.36 | 0.91 | 2.39 | 16.18 | 0.0 | 0.00 ± 0.00 | 0.00 | 0.00 | 3 | 1796 | 1856 | 0.53 |

E1 throughput, min - max over the 3 runs (Mops/s):

| impl | 1 M | 2 M | 5 M | 10 M |
|---|---|---|---|---|
| gomap | 30.49 - 31.57 | 27.62 - 31.20 | 22.15 - 26.05 | 15.31 - 25.72 |
| gomap-gogc400 | 29.78 - 31.53 | 27.88 - 29.60 | 21.95 - 29.02 | 23.25 - 25.40 |
| syncmap | 16.02 - 17.23 | 14.28 - 14.80 | 12.60 - 13.69 | 11.99 - 12.79 |
| gobytes | 32.73 - 34.91 | 30.88 - 35.37 | 28.56 - 28.90 | 26.94 - 30.59 |
| goarena | 38.64 - 43.53 | 38.08 - 41.15 | 32.86 - 38.54 | 33.46 - 37.85 |

goarena relative to the other implementations (E1 means):

| N | vs gomap throughput | vs gobytes throughput | vs gomap p99 | vs gomap RSS | vs gobytes RSS |
|---|---|---|---|---|---|
| 1 M | +31.4 % | +19.0 % | -45.8 % | -61.5 % | -9.8 % |
| 2 M | +35.0 % | +17.9 % | -48.3 % | -62.1 % | -11.7 % |
| 5 M | +44.6 % | +22.2 % | -55.7 % | -60.5 % | -6.7 % |
| 10 M | +62.3 % | +21.9 % | -64.4 % | -56.6 % | -5.6 % |

goarena memory breakdown (E1, MiB): reserved arena vs used slab blocks vs exact entry bytes:

| N | arena reserved | slab in use | entries (exact) | index | internal fragmentation | 64 MiB chunks | cgo calls (populate) | Go heap MiB |
|---|---|---|---|---|---|---|---|---|
| 1 M | 192 | 153 | 149 | 32 | 2.5 % | 3 | 2 | 3.04 |
| 2 M | 384 | 305 | 298 | 64 | 2.5 % | 6 | 5 | 3.05 |
| 5 M | 960 | 763 | 744 | 128 | 2.5 % | 15 | 14 | 3.05 |
| 10 M | 1856 | 1526 | 1488 | 256 | 2.5 % | 29 | 28 | 3.06 |

### E1b - GOMEMLIMIT = 4 GiB (debug.SetMemoryLimit), 3 runs

| impl | N | Mops/s | p99 µs | p99.9 µs | max ms | GC cycles | GC CPU % | mean mark ms (gctrace) | Go heap MiB | RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 26.65 ± 0.68 | 1.66 | 16.13 | 31.44 | 5.7 | 4.03 ± 0.08 | 155.7 | 1581 | 2300 |
| gobytes | 5 M | 30.72 ± 0.64 | 1.23 | 15.06 | 0.51 | 0.0 | 0.00 ± 0.00 | 0.0 | 934 | 948 |
| goarena | 5 M | 35.27 ± 2.03 | 0.89 | 2.87 | 23.75 | 0.0 | 0.00 ± 0.00 | 0.0 | 3 | 904 |
| gomap | 10 M | 23.69 ± 1.29 | 1.77 | 22.10 | 33.11 | 4.7 | 7.46 ± 0.55 | 358.0 | 2797 | 3559 |
| gobytes | 10 M | 28.86 ± 2.76 | 1.49 | 20.05 | 1.42 | 0.0 | 0.00 ± 0.00 | 0.0 | 1886 | 1914 |
| goarena | 10 M | 34.52 ± 1.70 | 0.88 | 2.98 | 23.24 | 0.0 | 0.00 ± 0.00 | 0.0 | 3 | 1796 |

E1b individual runs (Mops/s, GC CPU %):

| repeat | impl | N | Mops/s | GC CPU % | GC cycles |
|---|---|---|---|---|---|
| 1 | gomap | 5 M | 25.89 | 3.94 | 5 |
| 1 | gobytes | 5 M | 30.26 | 0.00 | 0 |
| 1 | goarena | 5 M | 33.18 | 0.00 | 0 |
| 1 | gomap | 10 M | 22.65 | 8.09 | 5 |
| 1 | gobytes | 10 M | 25.81 | 0.00 | 0 |
| 1 | goarena | 10 M | 32.94 | 0.00 | 0 |
| 2 | gomap | 5 M | 26.88 | 4.09 | 6 |
| 2 | gobytes | 5 M | 31.45 | 0.00 | 0 |
| 2 | goarena | 5 M | 35.37 | 0.00 | 0 |
| 2 | gomap | 10 M | 23.29 | 7.10 | 5 |
| 2 | gobytes | 10 M | 31.18 | 0.00 | 0 |
| 2 | goarena | 10 M | 34.31 | 0.00 | 0 |
| 3 | gomap | 5 M | 27.20 | 4.05 | 6 |
| 3 | gobytes | 5 M | 30.44 | 0.00 | 0 |
| 3 | goarena | 5 M | 37.25 | 0.00 | 0 |
| 3 | gomap | 10 M | 25.13 | 7.18 | 4 |
| 3 | gobytes | 10 M | 29.60 | 0.00 | 0 |
| 3 | goarena | 10 M | 36.31 | 0.00 | 0 |

### E7 - cost of one GC cycle vs N (1 run each, gctrace + forced runtime.GC())

| impl | N | GC cycles (10 s) | mean concurrent mark ms | forced runtime.GC() ms | scannable heap MiB | Go heap MiB | GC CPU % | Mops/s |
|---|---|---|---|---|---|---|---|---|
| gomap | 1 M | 19 | 16.8 | 26.14 | 96.1 | 283 | 1.97 | 29.93 |
| syncmap | 1 M | 10 | 375.1 | 104.22 | 185.7 | 675 | 10.60 | 16.53 |
| gobytes | 1 M | 0 | 0.0 | 0.60 | 0.4 | 235 | 0.00 | 35.12 |
| goarena | 1 M | 0 | 0.0 | 0.27 | 0.4 | 3 | 0.00 | 39.02 |
| gomap | 2 M | 10 | 42.3 | 41.74 | 192.0 | 939 | 2.67 | 31.90 |
| syncmap | 2 M | 5 | 805.6 | 234.66 | 242.4 | 1224 | 10.67 | 16.97 |
| gobytes | 2 M | 0 | 0.0 | 0.91 | 0.5 | 433 | 0.00 | 36.49 |
| goarena | 2 M | 0 | 0.0 | 0.53 | 0.4 | 3 | 0.00 | 39.23 |
| gomap | 5 M | 5 | 164.0 | 123.37 | 384.5 | 1383 | 4.19 | 28.85 |
| syncmap | 5 M | 2 | 2405.0 | 738.82 | 825.5 | 3231 | 13.48 | 14.85 |
| gobytes | 5 M | 0 | 0.0 | 1.25 | 0.7 | 934 | 0.00 | 31.71 |
| goarena | 5 M | 0 | 0.0 | 0.27 | 0.4 | 3 | 0.00 | 29.24 |
| gomap | 10 M | 2 | 326.0 | 315.80 | 768.7 | 2905 | 5.15 | 23.30 |
| syncmap | 10 M | 1 | 5585.0 | 1475.28 | 1310.9 | 5294 | 14.09 | 11.80 |
| gobytes | 10 M | 0 | 0.0 | 2.43 | 1.1 | 1912 | 0.00 | 30.88 |
| goarena | 10 M | 0 | 0.0 | 0.45 | 0.4 | 3 | 0.00 | 34.23 |

### E2 - goarena chunk size, N = 5M (3 runs)

| chunk | populate s | cgo calls (populate) | chunks | arena reserved MiB | slab in use MiB | index MiB | RSS MiB | Mops/s (5 s) |
|---|---|---|---|---|---|---|---|---|
| 1 MiB | 0.252 ± 0.011 | 947 | 948 | 948 | 763 | 128 | 908 | 38.57 ± 1.30 |
| 4 MiB | 0.270 ± 0.047 | 227 | 228 | 912 | 763 | 128 | 905 | 34.23 ± 5.83 |
| 16 MiB | 0.258 ± 0.031 | 56 | 57 | 912 | 763 | 128 | 904 | 36.29 ± 0.95 |
| 64 MiB | 0.241 ± 0.007 | 14 | 15 | 960 | 763 | 128 | 904 | 38.99 ± 0.83 |
| 256 MiB | 0.240 ± 0.002 | 3 | 4 | 1024 | 763 | 128 | 903 | 38.38 ± 1.51 |

### E4 - value size, N = 2M

| impl | value B | runs | Mops/s | p99 µs | p99.9 µs | GC CPU % | Go heap MiB | RSS MiB | peak RSS MiB | private MiB | off-heap MiB | fragmentation | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 64 | 1 | 28.70 | 1.74 | 22.78 | 1.87 | 571 | 751 | 751 | 760 | 0 | 0.0 % | 0.29 |
| gobytes | 64 | 1 | 33.46 | 1.10 | 16.64 | 0.00 | 302 | 306 | 306 | 321 | 0 | 0.0 % | 0.11 |
| goarena | 64 | 1 | 42.49 | 0.71 | 2.53 | 0.00 | 3 | 267 | 267 | 276 | 256 | 4.2 % | 0.09 |
| gomap | 256 | 1 | 27.44 | 1.65 | 16.25 | 3.57 | 899 | 1513 | 1513 | 1523 | 0 | 0.0 % | 0.29 |
| gobytes | 256 | 1 | 31.03 | 1.29 | 12.16 | 0.00 | 686 | 671 | 671 | 706 | 0 | 0.0 % | 0.16 |
| goarena | 256 | 1 | 33.73 | 1.00 | 4.54 | 0.00 | 3 | 689 | 689 | 726 | 704 | 11.2 % | 0.14 |
| gomap | 1024 | 1 | 17.08 | 2.21 | 26.37 | 4.18 | 2732 | 4555 | 4555 | 4573 | 0 | 0.0 % | 0.64 |
| gobytes | 1024 | 1 | 18.37 | 1.94 | 16.25 | 0.00 | 2145 | 2126 | 2126 | 2170 | 0 | 0.0 % | 0.48 |
| goarena | 1024 | 1 | 18.55 | 1.55 | 5.95 | 0.00 | 3 | 2479 | 2479 | 2521 | 2496 | 15.2 % | 0.39 |
| gomap | 4096 | 1 | 0.80 | 78.85 | 3047.42 | 0.00 | 15188 | 2506 | 14287 | 15628 | 0 | 0.0 % | 2.21 |
| gobytes | 4096 | 1 | 6.39 | 11.65 | 133.12 | 0.00 | 8045 | 8070 | 8070 | 8124 | 0 | 0.0 % | 1.49 |
| goarena | 4096 | 1 | 6.99 | 3.42 | 33.28 | 0.18 | 2 | 9100 | 9100 | 9769 | 9728 | 13.2 % | 1.87 |

### E5 - goroutine scaling, N = 2M (Mops/s; p99 µs in parentheses)

| impl | G = 1 | G = 4 | G = 8 | G = 16 | speed-up 1->16 |
|---|---|---|---|---|---|
| gomap | 2.70 (0.79) | 9.73 (0.82) | 16.14 (1.10) | 29.50 (1.52) | 10.9x |
| gomap-gogc400 | 2.75 (0.78) | 9.63 (0.84) | 17.72 (0.98) | 29.81 (1.68) | 10.8x |
| syncmap | 1.38 (1.46) | 5.34 (1.36) | 10.57 (1.42) | 14.79 (1.68) | 10.7x |
| gobytes | 3.07 (0.65) | 11.22 (0.69) | 21.31 (0.76) | 32.14 (1.14) | 10.5x |
| goarena | 3.42 (0.60) | 12.56 (0.65) | 23.79 (0.69) | 34.88 (0.84) | 10.2x |

### E6 - skewed and write-heavy workloads, N = 5M

| impl | keys | reads | Mops/s | p99 µs | p99.9 µs | max ms | GC CPU % | GC cycles | Go alloc MiB/s | RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|
| gomap | uniform | 50 % | 19.33 | 3.42 | 34.30 | 65.49 | 13.70 | 14 | 1401 | 2405 |
| gomap-gogc400 | uniform | 50 % | 19.94 | 4.42 | 22.78 | 5.41 | 4.76 | 3 | 1445 | 5731 |
| syncmap | uniform | 50 % | 7.97 | 2.98 | 29.44 | 52.87 | 30.23 | 4 | 912 | 5249 |
| gobytes | uniform | 50 % | 28.02 | 2.33 | 14.46 | 0.28 | 0.00 | 0 | 0 | 955 |
| goarena | uniform | 50 % | 33.07 | 1.04 | 2.72 | 1.75 | 0.00 | 0 | 0 | 903 |
| gomap | zipf | 90 % | 18.61 | 8.83 | 80.89 | 26.21 | 2.30 | 3 | 270 | 2005 |
| gomap-gogc400 | zipf | 90 % | 17.96 | 9.09 | 87.04 | 63.01 | 0.90 | 1 | 261 | 4121 |
| syncmap | zipf | 90 % | 22.12 | 1.52 | 5.44 | 67.10 | 21.42 | 3 | 506 | 3081 |
| gobytes | zipf | 90 % | 19.71 | 8.32 | 82.94 | 0.52 | 0.00 | 0 | 0 | 927 |
| goarena | zipf | 90 % | 48.66 | 0.66 | 1.01 | 20.57 | 0.00 | 1 | 0 | 905 |

### E3 - micro-benchmarks (go test -bench, -count 5, mean ± std)

| package | benchmark | ns/op | min - max | B/op | allocs/op |
|---|---|---|---|---|---|
| internal/cmem | BenchmarkGoCall | 1.37 ± 0.01 | 1.36 - 1.38 | 0 | 0 |
| internal/cmem | BenchmarkCgoCall | 38.38 ± 1.11 | 37.13 - 39.81 | 0 | 0 |
| internal/cmem | BenchmarkCgoCallParallel | 14.29 ± 0.25 | 14.01 - 14.51 | 0 | 0 |
| internal/cmem | BenchmarkMallocFree128 | 125.18 ± 1.85 | 122.60 - 127.70 | 0 | 0 |
| internal/cmem | BenchmarkGoMake128 | 28.63 ± 0.66 | 27.61 - 29.38 | 128 | 1 |
| internal/tsc | BenchmarkTicks | 17.89 ± 0.97 | 16.74 - 19.20 | 0 | 0 |
| arena | BenchmarkArenaAlloc128 | 4.86 ± 0.11 | 4.76 - 5.02 | 0 | 0 |
| arena | BenchmarkArenaAllocBytes128 | 6.91 ± 0.09 | 6.84 - 7.04 | 0 | 0 |
| arena | BenchmarkMakeBytes128 | 26.77 ± 0.38 | 26.23 - 27.14 | 128 | 1 |
| arena | BenchmarkSlabAllocFree128 | 11.00 ± 0.29 | 10.62 - 11.29 | 0 | 0 |
| arena | BenchmarkSlabAllocFreeMixed | 12.52 ± 0.17 | 12.33 - 12.68 | 0 | 0 |
| arena | BenchmarkNewValue | 26.73 ± 0.84 | 26.08 - 27.96 | 0 | 0 |
| arena | BenchmarkGoNew | 15.87 ± 0.31 | 15.59 - 16.36 | 24 | 1 |
| store | BenchmarkStoreGet | 188.06 ± 5.70 | 183.10 - 197.60 | 0 | 0 |
| store | BenchmarkMapGet | 197.38 ± 4.76 | 189.40 - 202.10 | 0 | 0 |
| store | BenchmarkStoreSet | 218.76 ± 17.06 | 191.90 - 233.20 | 0 | 0 |
| store | BenchmarkMapSet | 283.36 ± 2.34 | 281.10 - 286.70 | 144 | 2 |
| store | BenchmarkStoreGetParallel | 19.88 ± 2.63 | 18.27 - 24.52 | 0 | 0 |
| store | BenchmarkMapGetParallel | 28.14 ± 0.05 | 28.06 - 28.19 | 0 | 0 |
| store | BenchmarkStoreSetParallel | 24.46 ± 0.23 | 24.08 - 24.68 | 0 | 0 |
| store | BenchmarkMapSetParallel | 37.21 ± 1.54 | 35.10 - 39.09 | 144 | 2 |

### E8 - large scale (2 runs each; same metrics as E1 + one forced full GC)

| impl | N | runs | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles | GC CPU % | full GC ms | mean mark ms | Go heap MiB | RSS MiB | private MiB | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 20 M | 2 | 21.47 ± 0.82 | 0.56 | 2.17 | 20.73 | 37.47 | 1.0 | 6.62 | 940.6 | 966 | 7069 | 8028 | 8057 | 3.00 |
| gomap-gogc400 | 20 M | 2 | 21.74 ± 1.46 | 0.57 | 2.62 | 19.77 | 5.19 | 0.0 | 0.00 | 4679.4 | 0 | 10676 | 10909 | 10965 | 2.83 |
| syncmap | 20 M | 2 | 11.52 ± 0.50 | 1.15 | 2.46 | 12.80 | 46.80 | 1.0 | 30.89 | 3296.6 | 6877 | 10276 | 10690 | 10724 | 3.65 |
| gobytes | 20 M | 2 | 24.22 ± 1.51 | 0.51 | 1.76 | 24.83 | 1.34 | 0.0 | 0.00 | 4.9 | 0 | 3713 | 3738 | 3774 | 1.33 |
| goarena | 20 M | 2 | 29.96 ± 1.74 | 0.45 | 0.94 | 4.53 | 21.89 | 0.5 | 0.00 | 0.6 | 0 | 3 | 3580 | 3676 | 1.02 |
| gomap | 30 M | 2 | 18.25 ± 0.36 | 0.65 | 2.69 | 27.90 | 26.43 | 1.0 | 9.94 | 1298.5 | 1652 | 9176 | 11492 | 11533 | 5.33 |
| gobytes | 30 M | 2 | 20.04 ± 4.28 | 0.59 | 2.69 | 38.53 | 5.94 | 0.0 | 0.00 | 8.4 | 0 | 5932 | 5926 | 5974 | 1.93 |
| goarena | 30 M | 2 | 26.90 ± 0.50 | 0.50 | 1.01 | 4.67 | 14.44 | 0.0 | 0.00 | 0.6 | 0 | 3 | 5620 | 5728 | 1.68 |
| gomap | 50 M | 2 | 18.40 ± 0.69 | 0.68 | 3.04 | 23.04 | 8.02 | 0.0 | 0.00 | 12505.5 | 0 | 15023 | 15381 | 15428 | 7.86 |
| gobytes | 50 M | 2 | 20.12 ± 0.41 | 0.61 | 2.17 | 29.70 | 2.86 | 0.0 | 0.00 | 11.1 | 0 | 9253 | 9246 | 9308 | 3.63 |
| goarena | 50 M | 2 | 25.57 ± 0.47 | 0.51 | 1.07 | 5.05 | 18.75 | 0.0 | 0.00 | 0.7 | 0 | 3 | 8680 | 8806 | 2.70 |

E8 runs not executed (memory guard):

| impl | N | reason |
|---|---|---|
| gomap-gogc400 | 30 M | projected private memory 27.8 GiB (incl. 15 % margin) exceeds the budget of 24.0 GiB (32 GB RAM machine): does not fit / risk of swapping |
| gomap-gogc400 | 50 M | projected private memory 46.4 GiB (incl. 15 % margin) exceeds the budget of 24.0 GiB (32 GB RAM machine): does not fit / risk of swapping |
| syncmap | 30 M | projected private memory 25.2 GiB (incl. 15 % margin) exceeds the budget of 24.0 GiB (32 GB RAM machine): does not fit / risk of swapping |
| syncmap | 50 M | projected private memory 42.0 GiB (incl. 15 % margin) exceeds the budget of 24.0 GiB (32 GB RAM machine): does not fit / risk of swapping |

### E9 - service with request garbage (1 KiB request object per operation), 3 runs

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles/s | GC CPU % | STW ms/s | Go alloc MiB/s | Go heap MiB | RSS MiB | peak RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 5.33 ± 0.11 | 1.18 | 14.29 | 378.20 | 38.36 | 3.7 | 34.75 ± 0.34 | 0.3 | 5888 | 2922 | 3358 | 3358 |
| gomap-gogc400 | 5 M | 6.81 ± 0.67 | 1.20 | 13.52 | 169.98 | 60.09 | 0.8 | 12.81 ± 0.81 | 0.8 | 7529 | 8930 | 18454 | 18687 |
| syncmap | 5 M | 2.13 ± 0.19 | 1.69 | 23.12 | 887.47 | 322.76 | 1.0 | 60.84 ± 1.14 | 0.3 | 2378 | 4556 | 9046 | 9312 |
| gobytes | 5 M | 9.45 ± 0.09 | 1.16 | 4.54 | 100.01 | 4.62 | 11.1 | 0.61 ± 0.04 | 4.4 | 10309 | 1760 | 1972 | 1972 |
| goarena | 5 M | 4.73 ± 0.27 | 0.88 | 25.34 | 441.00 | 26.85 | 358.1 | 9.22 ± 0.03 | 109.1 | 5157 | 18 | 979 | 1090 |
| goarena-gogc400 | 5 M | 7.70 ± 0.23 | 0.99 | 9.68 | 223.23 | 22.06 | 107.7 | 3.25 ± 0.17 | 59.7 | 8396 | 108 | 1379 | 1474 |
| gomap | 10 M | 4.46 ± 0.21 | 1.31 | 12.84 | 525.65 | 47.18 | 1.6 | 43.05 ± 1.34 | 0.2 | 4926 | 5393 | 6542 | 6542 |
| gomap-gogc400 | 10 M | 1.69 ± 0.68 | 1.22 | 54.27 | 1073.15 | 2259.19 | 0.2 | 7.13 ± 1.13 | 5.9 | 1872 | 9185 | 10745 | 19808 |
| syncmap | 10 M | 1.52 ± 0.65 | 1.85 | 54.87 | 1267.03 | 1202.99 | 0.3 | 41.42 ± 9.16 | 0.1 | 1699 | 13254 | 14988 | 19182 |
| gobytes | 10 M | 9.36 ± 0.14 | 1.23 | 4.24 | 87.04 | 3.81 | 5.6 | 0.42 ± 0.01 | 2.1 | 10207 | 3178 | 3845 | 3845 |
| goarena | 10 M | 4.73 ± 0.04 | 0.91 | 25.34 | 427.35 | 57.49 | 378.1 | 9.25 ± 0.12 | 119.0 | 5161 | 16 | 1826 | 1926 |
| goarena-gogc400 | 10 M | 7.47 ± 0.06 | 1.07 | 9.60 | 224.60 | 16.50 | 111.0 | 3.43 ± 0.22 | 57.9 | 8151 | 59 | 2201 | 2522 |

### E10 - container memory budget: E9 workload, N = 5M, GOMEMLIMIT = 1.25 x gobytes RSS, 3 runs

Memory limit applied to every implementation: 1210 MiB.

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles/s | GC CPU % | STW ms/s | Go alloc MiB/s | Go heap MiB | RSS MiB | peak RSS MiB |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 1.03 ± 0.05 | 1.39 | 91.82 | 890.20 | 91.19 | 7.3 | 58.20 ± 0.65 | 1.1 | 1143 | 1371 | 1429 | 1563 |
| gomap-gogc400 | 5 M | 1.11 ± 0.12 | 1.37 | 97.45 | 808.28 | 79.95 | 7.7 | 57.20 ± 0.78 | 0.8 | 1225 | 1346 | 1401 | 1564 |
| syncmap | 5 M | 0.69 ± 0.12 | 1.82 | 19.37 | 658.09 | 666.36 | 1.5 | 75.57 ± 1.89 | 0.0 | 767 | 2053 | 2225 | 3537 |
| gobytes | 5 M | 9.59 ± 0.39 | 1.10 | 5.05 | 98.64 | 7.84 | 55.3 | 2.37 ± 0.26 | 19.2 | 10466 | 1074 | 1167 | 1208 |
| goarena | 5 M | 5.28 ± 0.47 | 0.82 | 22.61 | 389.12 | 14.80 | 412.2 | 8.99 ± 0.24 | 125.2 | 5764 | 17 | 943 | 1039 |
| goarena-gogc400 | 5 M | 7.79 ± 0.20 | 1.02 | 9.17 | 215.04 | 16.36 | 117.5 | 3.45 ± 0.16 | 62.0 | 8495 | 60 | 1281 | 1484 |
| goarena-budget | 5 M | 8.59 ± 0.06 | 1.09 | 5.35 | 87.72 | 49.63 | 47.4 | 1.53 ± 0.11 | 27.5 | 9371 | 99 | 1029 | 1196 |

### goarena v0.1.0 -> v0.2.0 (same experiments, same parameters; baselines unchanged)

| case | metric | goarena v0.1.0 | goarena v0.2.0 | change | best baseline (v0.2.0 session) |
|---|---|---|---|---|---|
| E1 N=1M | throughput, Mops/s | 37.47 | 40.54 | +8.2 % | 34.07 (gobytes) |
| E1 N=1M | p99, µs | 1.01 | 0.82 | -18.8 % | 1.15 (gobytes) |
| E1 N=1M | p99.9, µs | 14.42 | 1.95 | -86.5 % | 9.58 (syncmap) |
| E1 N=1M | RSS, MiB | 231.37 | 196.28 | -15.2 % | 217.57 (gobytes) |
| E1 N=5M | throughput, Mops/s | 34.67 | 35.15 | +1.4 % | 28.77 (gobytes) |
| E1 N=5M | p99, µs | 1.12 | 0.86 | -23.5 % | 1.47 (gobytes) |
| E1 N=5M | p99.9, µs | 15.62 | 2.75 | -82.4 % | 17.62 (syncmap) |
| E1 N=5M | RSS, MiB | 1069.89 | 903.51 | -15.6 % | 968.07 (gobytes) |
| E1 N=10M | throughput, Mops/s | 31.80 | 34.96 | +9.9 % | 28.68 (gobytes) |
| E1 N=10M | p99, µs | 1.27 | 0.91 | -28.7 % | 1.34 (gobytes) |
| E1 N=10M | p99.9, µs | 19.71 | 2.39 | -87.9 % | 12.93 (syncmap) |
| E1 N=10M | RSS, MiB | 2103.22 | 1795.68 | -14.6 % | 1901.91 (gobytes) |
| E6 Zipf 5M | throughput, Mops/s | 17.52 | 48.66 | +177.7 % | 22.12 (syncmap) |
| E6 Zipf 5M | p99, µs | 8.58 | 0.66 | -92.3 % | 1.52 (syncmap) |
| E6 Zipf 5M | p99.9, µs | 115.71 | 1.02 | -99.1 % | 5.44 (syncmap) |
| E6 Zipf 5M | RSS, MiB | 1071.58 | 904.70 | -15.6 % | 926.82 (gobytes) |
| E6 50% writes 5M | throughput, Mops/s | 22.69 | 33.07 | +45.7 % | 28.02 (gobytes) |
| E6 50% writes 5M | p99, µs | 9.09 | 1.04 | -88.6 % | 2.33 (gobytes) |
| E6 50% writes 5M | p99.9, µs | 35.33 | 2.72 | -92.3 % | 14.46 (gobytes) |
| E6 50% writes 5M | RSS, MiB | 1070.04 | 903.45 | -15.6 % | 955.45 (gobytes) |
| E5 G=1 2M | throughput, Mops/s | 3.16 | 3.42 | +8.3 % | 3.07 (gobytes) |
| E5 G=1 2M | p99, µs | 0.68 | 0.60 | -11.8 % | 0.65 (gobytes) |
| E5 G=1 2M | p99.9, µs | 1.23 | 1.07 | -13.0 % | 1.23 (gobytes) |
| E5 G=1 2M | RSS, MiB | 444.91 | 378.26 | -15.0 % | 429.79 (gobytes) |
| E4 1 KiB 2M | throughput, Mops/s | 16.83 | 18.55 | +10.2 % | 18.37 (gobytes) |
| E4 1 KiB 2M | p99, µs | 2.66 | 1.55 | -41.6 % | 1.94 (gobytes) |
| E4 1 KiB 2M | p99.9, µs | 38.40 | 5.95 | -84.5 % | 16.25 (gobytes) |
| E4 1 KiB 2M | RSS, MiB | 2354.55 | 2479.42 | +5.3 % | 2125.93 (gobytes) |
| E4 4 KiB 2M | throughput, Mops/s | 6.65 | 6.99 | +5.2 % | 6.39 (gobytes) |
| E4 4 KiB 2M | p99, µs | 10.11 | 3.42 | -66.1 % | 11.65 (gobytes) |
| E4 4 KiB 2M | p99.9, µs | 121.86 | 33.28 | -72.7 % | 133.12 (gobytes) |
| E4 4 KiB 2M | RSS, MiB | 9010.89 | 9100.25 | +1.0 % | 2505.89 (gomap) |

### Example service (dashboard screenshots; 4 load workers + 200 HTTP req/s, 2M keys x 256 B)

| backend | ops/s | p99 (1-s window) | Go heap | off-heap | RSS | GC CPU since start |
|---|---|---|---|---|---|---|
| goarena | 9.01 M | 759 ns | 3.0 MiB | 704 MiB | 696 MiB | 0.01 % |
| gomap | 6.70 M | 1.17 µs | 1.18 GiB (goal 1.43 GiB) | 0 | 1.47 GiB | 0.96 % |

(values as displayed in `fig4_dashboard*.png`, retaken with v0.2.0; `/stats`
snapshots in `results/stats_goarena.json`, `results/stats_gomap.json`.)

## 7. v0.1.0 -> v0.2.0 optimisation (goarena only; baselines unchanged)

What changed (details and byte layouts: DESIGN_NOTES.md §5.1, §7.1-7.6,
§8.3-8.5, §14; CHANGELOG 0.2.0):

1. hybrid size classes: 8-byte steps 16-256 B, then x1.25 to 64 KiB -
   **56 classes** (v0.1.0: 36); mean internal waste for uniform sizes
   9.6 % (17 B-64 KiB) / 10.1 % (17 B-4 KiB); measured fragmentation of the
   E1 entries 2.5 % (v0.1.0 14.6 %);
2. 8-byte entry header without TTL (16 B with TTL);
3. lock-free seqlock reads with bounded retries and a locked fallback;
   `sync.Mutex` writer lock instead of `sync.RWMutex`; reader stripes for a
   safe `Close`; cache-line shard layout;
4. index tables allocated in the arena and donated to the slab on growth;
   large objects retained on large-class free lists (no unmapping while
   the store is open - the precondition of speculative reads).

The comparison table (generated, "best baseline" = the best of the four
baselines in the v0.2.0 session) is the last table of section 6. Summary:

| case | v0.1.0 | v0.2.0 | change |
|---|---|---|---|
| E1 10M throughput, Mops/s | 31.80 | 34.96 | +9.9 % |
| E1 10M p99.9, µs | 19.71 | 2.39 | -87.9 % |
| E1 10M RSS, MiB | 2103 | 1796 | -14.6 % (gobytes: 1902) |
| E1 5M RSS, MiB | 1070 | 904 | -15.6 % (gobytes: 968) |
| E6 Zipf throughput, Mops/s | 17.52 | 48.66 | +178 % (sync.Map: 22.12) |
| E6 Zipf p99.9, µs | 115.7 | 1.01 | -99.1 % (sync.Map: 5.44) |
| E6 50 % writes, Mops/s | 22.69 | 33.07 | +45.7 % (gobytes: 28.02) |
| E4 1 KiB RSS, MiB | 2355 | 2479 | **+5.3 %** (class 1240 for 1052-B entries) |
| populate cgo calls, 10M | 5 149 | 28 | index inside the arena |
| max latency E1 (mean of runs), ms | 0.64-2.28 | 10.3-16.2 | **worse** (see 8.2) |

Rejected alternative (measured, `results/optimization_runs/`): a spinning
writer lock gave 50.7 Mops/s and 1.1 µs write p99.9 under Zipf but tripled
the number of operations > 1 ms (113 per run at 1M) because it never
releases the CPU; an aborted first v0.2.0 session with that lock is kept in
`results/optimization_runs/aborted_spinlock_session/`.

## 8. Interpretation

### 8.1 Where goarena wins and why

1. **GC work independent of N.** The Go heap stays at 3 MiB (0.36 MiB
   scannable) for every N; a full GC costs 0.27-0.70 ms from 1M to 50M
   entries. For gomap the full GC grows linearly with the scannable heap
   (96 MiB -> 769 MiB, 26 -> 316 ms from 1M to 10M; mean concurrent mark from
   gctrace 17 -> 326 ms) and reaches 0.75-1.1 s at 20M; sync.Map 1.5 s at
   10M. In all measured phases of E1-E8 goarena allocated 0.01-0.22 MiB/s of Go memory
   versus 350-1400 MiB/s for gomap.
2. **Throughput** +19-22 % vs gobytes and +31-62 % vs gomap in E1, +120 %
   vs the best baseline (sync.Map) under hot keys, +18 % vs gobytes with
   50 % writes. A read touches one 16-byte slot and one entry and, since
   v0.2.0, writes nothing to shared cache lines.
3. **Tail latency**: in E1 p99 is 1.4-2.8x and p99.9 4-16x lower than for
   every baseline (p99.9 1.9-2.8 µs vs 9.6-38 µs); stays at 4.5-5.1 µs up to 50M (E8).
4. **Memory**: smallest RSS of all implementations for the 128-byte values
   of E1/E8 and the 64-byte values of E4 (with 256-byte values gobytes is
   2.7 % smaller): -5.6 % vs gobytes and -57 % vs gomap at 10M, -6 % vs
   gobytes at 50M. Reserved vs used arena memory at 10M: 1856 MiB reserved,
   1526 MiB slab blocks + 256 MiB index, < one 64 MiB chunk of slack; the
   outgrown index tables (255 MiB) were recycled as entry memory.
5. **Scaling (E5)**: fastest at every G (3.42 Mops/s at G = 1, 34.88 at
   G = 16); the 1->16 speed-up (10.2x) is limited by 8 physical cores.
6. **GOMEMLIMIT = 4 GiB (E1b)** does not affect goarena (C memory is not
   counted); gomap's GC CPU rises from 5.12 to 7.46 % at 10M.
7. **Chunk size (E2)**: populate time 0.24-0.27 s for 1-256 MiB chunks (no
   measurable effect); cgo calls 947 (1 MiB) -> 3 (256 MiB); over-reservation
   912-1024 MiB reserved for 891 MiB used; 64 MiB stays the default.

### 8.2 Where goarena loses or ties, and why

1. **Request garbage with default GC settings (E9).** Every operation
   allocates a 1 KiB request object (5-10 GiB/s across 16 goroutines).
   goarena's Go heap holds only these short-lived objects (16-18 MiB), so
   with GOGC=100 the heap goal is tiny and the GC runs **358-378 times per
   second** (gobytes: 5.6-11, its 1.8-3.2 GiB pointer-free heap acting as
   free headroom; gomap: 1.6-3.7). Each cycle is cheap but the stop-the-world
   phases add up to 109-119 ms per second and allocation slows down:
   **4.73 Mops/s vs 9.45 (gobytes)** at both N; p99.9 427-441 µs vs 87-100 µs
   (gobytes); gomap 4.46-5.33, sync.Map 1.5-2.1. GC CPU is still low (9.2 %
   vs 34.8-43.1 % for gomap). With the same GOGC=400 tuning as
   `gomap-gogc400`, `goarena-gogc400` reaches 7.47-7.70 Mops/s (108-111
   cycles/s, p99.9 223 µs) - still below gobytes. The lesson for
   deployments: a process whose data lives off-heap must give the Go GC
   headroom explicitly (GOGC/GOMEMLIMIT), because there is no large heap to
   amortise the garbage.
2. **Container memory budget (E10, 1210 MiB).** The Go-heap maps thrash
   against the limit (gomap 1.03 Mops/s, 58 % GC CPU; sync.Map 0.69, 76 %;
   their RSS 1.4-2.2 GiB exceeds the soft limit anyway); gobytes fits (RSS
   1167 MiB, 9.59 Mops/s). goarena with defaults: 5.28 Mops/s, RSS 943 MiB
   (inside the budget, limit irrelevant to it). `goarena-gogc400`: 7.79
   Mops/s but RSS 1281 MiB (over budget). **`goarena-budget` (GOGC=off,
   GOMEMLIMIT = 1210 MiB - 960 MiB off-heap = 250 MiB): 8.59 ± 0.06 Mops/s,
   p99 5.35 µs, p99.9 87.7 µs, GC CPU 1.53 %, 47 cycles/s, RSS 1029 MiB
   (peak 1196)** versus gobytes 9.59 ± 0.39 Mops/s, p99 5.05 µs, p99.9
   98.6 µs, RSS 1167 MiB (peak 1208). goarena-budget is 10 % slower in
   throughput, slightly better in p99.9 and uses 12 % less memory.
3. **Extreme tail / maximum.** goarena never blocks on the read path; with
   16 goroutines on 16 logical CPUs the process keeps ~15 CPUs busy (149.6 vs
   138.6 CPU-seconds for gobytes in a 10 s run), so the OS occasionally
   preempts a benchmark thread for a scheduler quantum: 0.7-12 sampled
   operations per million exceed 1 ms (gobytes 0-1, gomap 0.7-20), and the
   mean per-run maximum is 10-16 ms (gobytes 1.6-10 ms, gomap 7-28 ms,
   sync.Map 76-301 ms). With 14 goroutines this drops to 2 per run (max
   1.2 ms). This is a cost of the lock-free design under full CPU
   saturation, not of GC.
4. **Large values (E4).** 1 KiB: throughput ties gobytes (18.55 vs 18.37
   Mops/s), p99.9 better (5.95 vs 16.25 µs) but RSS +16.6 % (2479 vs 2126
   MiB) because 1052-byte entries fall into the 1240-byte class (15.2 %
   waste); 4 KiB: 6.99 vs 6.39 Mops/s but RSS +12.8 % (9100 vs 8070 MiB,
   13.2 % waste). gobytes packs entries exactly.
5. **Single-threaded micro-benchmarks (E3)**: `store.GetInto` equals a Go
   map read (188 vs 197 ns); `NewValue[T]` (26.7 ns) is slower than
   `new(T)` (15.9 ns) because of the arena mutex and zeroing;
   `Arena.Alloc` (4.9 ns) is 5.5x faster than `make([]byte,128)` (26.8 ns),
   `Slab.Alloc+Free` (11.0 ns) 11x faster than C `malloc+free` (125 ns).
6. **cgo per call is expensive** (38-68 ns) and does not scale well across
   goroutines (14.3 ns/op aggregate for 16 goroutines); the model only works
   because goarena amortises it (28 calls for 10M entries).
7. **Measurement limits**: Windows' 0.5 ms runtime clock quantises GC pause
   statistics (STW figures are upper bounds; latency impact is reported via
   TSC-timed operations); the laptop CPU shows run-to-run variation of
   3-25 % (e.g. gomap at 10M: 15.3-25.7 Mops/s; first runs of a large-N
   series had 4-11x slower forced GCs: gomap 50M 23.0 s vs 2.0 s, gomap
   GOGC=400 20M 8.4 s vs 1.0 s - both values are reported).

### 8.3 Target-domain experiments

* **E8 (large scale)**: goarena's advantage grows with N: at 20M/30M/50M
  29.96/26.90/25.57 Mops/s vs gobytes 24.22/20.04/20.12 and gomap
  21.47/18.25/18.40; p99.9 4.5-5.1 µs vs 20.7-38.5 µs; full GC 0.6-0.7 ms
  vs 4.9-11.1 ms (gobytes) and 0.75 s -> 2.0-23 s (gomap); RSS 3.5/5.5/8.5
  GiB vs 3.6/5.8/9.0 (gobytes) and 7.8/11.2/15.0 (gomap). gomap GOGC=400 and
  sync.Map at 30M and 50M were **not run** (projected 25-46 GiB > 24 GiB
  budget; recorded in `raw/*.skipped.txt`); at 20M they needed 10.7-10.9
  GiB RSS.
* **E9 / E10**: see 8.2 items 1-2 - the scenario where off-heap data needs
  explicit GC configuration; with it goarena reaches 90 % of the best
  baseline's throughput within the same memory budget.

## 9. Alignment with chapter 2 (coordinator requests)

1. *Shard reservation (2.17):* shared chunk pool (one arena per store,
   64 KiB spans, index tables inside the arena). Reserved vs used at 1M /
   2M / 5M / 10M: 192/184, 384/369, 960/891, 1856/1782 MiB (arena reserved /
   slab blocks + index).
2. *Fair reads:* `GetInto` with a reused buffer; asymmetry note in §5.
3. *Size classes:* v0.1.0 had exactly the chapter-2 formula (36 classes,
   measured 14.6 % on E1 entries); v0.2.0 uses the hybrid table (56
   classes, 9.6 % mean for uniform sizes, 2.5 % measured on E1 entries, 4.2 /
   11.2 / 15.2 / 13.2 % for 64 / 256 / 1024 / 4096-byte values in E4).
4. *E1b and mark time:* GC CPU under GOMEMLIMIT = 4 GiB: gomap 4.03 % (5M),
   7.46 % (10M), goarena 0; the limiter climb towards 50 % was not observed
   because gomap's live heap stays below 4 GiB; in E10 (1.21 GiB limit)
   gomap reaches 58 % and sync.Map 76 % GC CPU - the predicted thrashing.
   Mark time grows linearly with N for gomap (16.8 / 42.3 / 164 / 326 ms at
   1 / 2 / 5 / 10M, E7), close to the model's ~0.28 s at 10M; goarena's full
   GC 0.27-0.70 ms (model: ~1 ms).
5. *cgo vs Go call:* 38.38 ± 1.11 ns vs 1.37 ± 0.01 ns (v0.2.0 E3), range
   38-68 ns vs 1.4-1.9 ns across sessions.

## 10. Figures for chapter 4 (`work/figures`, scripts in `work/figscripts`)

All figures were regenerated from the v0.2.0 data; the v0.1.0 versions are
kept in `results/v1/figures/`.

| file | proposed caption (UA) | description |
|---|---|---|
| `fig4_e1_throughput.png` | Рисунок 4.x – Пропускна здатність реалізацій залежно від кількості записів N | E1: mean of 3 runs, error bars min..max. |
| `fig4_e1_latency.png` | Рисунок 4.x – Затримка операцій (99-й і 99,9-й процентилі) залежно від N | E1: goarena p99.9 1.9-2.8 µs vs 9.6-38 µs. |
| `fig4_e1_gccpu.png` | Рисунок 4.x – Частка процесорного часу на збирання сміття залежно від N | E1: grows with N for Go-heap maps, zero for goarena and gobytes. |
| `fig4_e1_gcpause.png` | Рисунок 4.x – Кількість циклів збирання сміття та найдовша пауза STW | E1 (Windows 0.5 ms timer granularity). |
| `fig4_e1_memory.png` | Рисунок 4.x – Обсяг керованої купи Go та резидентної пам'яті процесу | E1: Go heap and RSS; goarena off-heap dashed. |
| `fig4_e1_ccdf.png` | Рисунок 4.x – Розподіл «хвоста» затримок операцій при N = 5 млн | E1: CCDF of all sampled latencies (3 runs merged). |
| `fig4_e7_mark.png` | Рисунок 4.x – Тривалість циклу збирання сміття залежно від N | E7: forced full GC and mean concurrent mark time. |
| `fig4_e1b_memlimit.png` | Рисунок 4.x – Вплив обмеження GOMEMLIMIT = 4 ГіБ на частку ЦП для GC і пропускну здатність | E1b vs E1. |
| `fig4_e2_chunk.png` | Рисунок 4.x – Вплив розміру чанка на час заповнення, кількість викликів CGO та резервування пам'яті | E2. |
| `fig4_e3_micro.png` | Рисунок 4.x – Результати мікробенчмарків: стандартні засоби Go та goarena/CGO | E3, log scale. |
| `fig4_e4_value.png` | Рисунок 4.x – Вплив розміру значення на пропускну здатність і резидентну пам'ять | E4: N = 2M, 64-4096 B. |
| `fig4_e5_goroutines.png` | Рисунок 4.x – Масштабування пропускної здатності та затримки p99 за кількістю горутин | E5. |
| `fig4_e6_workloads.png` | Рисунок 4.x – Результати для нерівномірного (Zipf) та інтенсивного за записом навантажень | E6. |
| `fig4_e8_scale.png` | Рисунок 4.x – Пропускна здатність і резидентна пам'ять за великої кількості записів (до 50 млн) | E1 + E8; × = run skipped by the memory guard. |
| `fig4_e8_gc.png` | Рисунок 4.x – Вартість повного циклу збирання сміття та затримка p99,9 за великої кількості записів | E1 + E8, log y for GC. |
| `fig4_e9_service.png` | Рисунок 4.x – Сервіс із «сміттям» запитів: пропускна здатність, p99,9, частка ЦП і частота циклів GC | E9 incl. goarena + GOGC=400; shows where default goarena loses. |
| `fig4_e10_budget.png` | Рисунок 4.x – Робота в межах бюджету пам'яті контейнера (GOMEMLIMIT = 1210 МіБ) | E10 incl. both tuned goarena configurations; dashed line = limit. |
| `fig4_v1v2_memory.png` | Рисунок 4.x – Оптимізація пам'яті: goarena v0.1.0, v0.2.0 і найкраща базова реалізація | RSS at 5M/10M. |
| `fig4_v1v2_latency.png` | Рисунок 4.x – Оптимізація «хвоста» затримок: goarena v0.1.0 і v0.2.0 | p99.9 uniform 10M and Zipf 5M vs best baseline. |
| `fig4_v1v2_throughput.png` | Рисунок 4.x – Пропускна здатність goarena v0.1.0 і v0.2.0 порівняно з найкращою базовою реалізацією | E1 10M, E6 Zipf, E6 50 % writes. |
| `fig4_dashboard.png` | Рисунок 4.x – Панель моніторингу прикладу httpcache з бекендом goarena | Real headless-Chrome screenshot (1400x900), v0.2.0, after 2 min of self-load. |
| `fig4_dashboard_gomap.png` | Рисунок 4.x – Панель моніторингу прикладу httpcache з бекендом gomap (для порівняння) | Same load with map[string][]byte. |
| `fig4_term_test.png` | Рисунок 4.x – Результати виконання модульних тестів (go test -v) | Real log, last 33 PASS/ok lines. |
| `fig4_term_race.png` | Рисунок 4.x – Перевірка детектором гонок даних (go test -race) | Real log: all ok, no DATA RACE. |
| `fig4_term_cover.png` | Рисунок 4.x – Покриття коду тестами за пакетами | Real log: 87.5-100 % per package. |
| `fig4_term_fuzz.png` | Рисунок 4.x – Фазинг-тестування сховища (FuzzStore) | Real log: 2.33 M executions in 90 s. |
| `fig4_term_bench.png` | Рисунок 4.x – Вивід мікробенчмарків go test -bench | Real `-count 1` log (padding squeezed). |
| `fig4_term_vet_build.png` | Рисунок 4.x – Статичний аналіз (go vet) і збирання проєкту | Real log: toolchain, vet/build/gofmt clean. |

## 11. Files suitable for appendix listings (Додаток А)

| file | lines | content |
|---|---|---|
| `internal/cmem/cmem.go` | 180 | cgo bridge: malloc/calloc/free wrappers, header, counters |
| `arena/arena.go` | 486 | Arena, Ref, chunk table, bump allocation, TryPointer, Reset/Release |
| `arena/slab.go` | 483 | hybrid size classes, free lists, spans, donations, large classes |
| `arena/typed.go` | 120 | NewValue/NewSlice with the pointer check |
| `store/store.go` | 706 | Store API, shard layout, seqlock reads, writers, Close handshake |
| `store/index.go` | 168 | open-addressing index in arena memory, rehash + donation |
| `store/entry.go` | 94 | entry layout v2 |
| `store/stats.go` | 115 | statistics, GoMemoryBudget |
| `store/lock.go` | 26 | writer lock |
| `gcstats/gcstats.go` | 295 | runtime/metrics snapshots and deltas |
| `gcstats/process_windows.go` | 55 | RSS / CPU time via kernel32 (syscall only) |
| `internal/latency/latency.go` | 203 | log-linear histogram |
| `internal/tsc/tsc.go`, `tsc_amd64.s` | 53 + 11 | RDTSCP clock |
| `cmd/goarena-bench/impl.go` | 325 | compared implementations and goarena configurations |
| `cmd/goarena-bench/workload.go` | 408 | workload, request garbage, sampling, phases |
| `cmd/goarena-bench/driver.go` | 377 | experiments E1-E10, memory guard, process-per-run driver |
| `cmd/goarena-bench/aggregate.go` | 332 | CSV aggregation, gctrace parsing |
| `examples/httpcache/server.go` | 294 | HTTP handlers, /stats sampler |
| `examples/httpcache/dashboard.html` | 221 | dashboard (canvas charts) |
| `store/optimistic_test.go` | 245 | seqlock correctness tests |
| `store/fuzz_test.go` | 175 | differential fuzz test |
| `store/stress_test.go` | 160 | concurrency stress tests |
| `store/leak_test.go` | 100 | leak / finalizer tests |
| `tools/term2png.py` | 204 | log-to-PNG renderer |

Додаток Б: `logs/test.txt`, `logs/race.txt`, `logs/cover.txt`, the head of
`logs/cover_func.txt`, `logs/fuzz.txt`, `logs/microbench.txt` and the tables
of section 6. Додаток В: README.md (Install, Quick start, API, Safety rules).

## 12. Deviations and caveats (details in DESIGN_NOTES.md §12-14)

* One shared arena per store with 64 KiB slab spans; since v0.2.0 the index
  tables also live in the arena; same-size rehash when tombstones dominate;
  large objects as retained dedicated chunks; `MaxMemory` covers all arena
  memory.
* v0.2.0 departs from the original parameter list where measurements
  justified it: hybrid size classes (56 instead of 36), 8-byte entry header,
  `MaxKeySize` 65 535, seqlock + `sync.Mutex` instead of `sync.RWMutex`.
* E4 compares gomap, gobytes, goarena only; E8 skips 4 configurations by
  the memory guard; E9/E10 add two goarena configurations (documented).
* The cmem malloc-wrapper fix landed after the main matrix (affects only the
  failure path); E3 was re-run after it.
* Windows specifics: 0.5 ms runtime timer resolution; coverage/global-array
  workaround (DESIGN_NOTES §13.1).
* All benchmark processes, the example server and headless Chrome were
  stopped after the measurements; `work/BENCH_RUNNING.flag` was created at
  the start of each benchmark session and removed at its end.
