# goarena - results summary, v0.3.0 (for chapters 3, 4, 5 and the conclusions)

All numbers come from real runs on the machine below; nothing was estimated,
adjusted or copied from literature. Three versions of goarena were measured
with identical baselines (the baseline code never changed):

| version | session | what was measured | where |
|---|---|---|---|
| v0.1.0 | 13.10.2026 18:10-18:55 | full matrix E1-E7 | `results/v1/` |
| v0.2.0 | 13.10.2026 19:55-21:21 | full matrix E1-E10 + E3 | `results/v2/` |
| **v0.3.0** | 13.10.2026 21:33-22:10 | **E1r (E1 at 10M), E1t, E4, E9, E10** | `results/` (this directory) |

**v0.3.0 numbers are primary for the experiments it re-ran** (E1 at N = 10M as
"E1r", E4, E9, E10, the new tail check E1t). v0.3.0 changed only (a) the size
classes above 256 B and (b) added the opt-in GC pacer; the 156-byte entries
of E1/E1b/E2/E5-E8 keep the same 160-byte class and pacing is off by default,
so those experiments exercise unchanged code and are reported from the v0.2.0
session (section 6.2). E3 micro-benchmarks are from v0.2.0 as well.

Files: `results/raw/*.json` (113 runs of the v0.3.0 session),
`results/summary.csv`, `results/runs.csv`, generated tables
`results/tables_v3.md` (`scripts/make_tables_v3.py`); logs `results/logs/`.

## 1. Environment

| item | value |
|---|---|
| CPU | AMD Ryzen 7 7735HS with Radeon Graphics, 8 cores / 16 threads (laptop CPU, boost enabled); invariant TSC 3.19 GHz |
| RAM | 32 GB |
| OS | Windows 10 Pro 22H2, build 10.0.19045 |
| Go | go1.24.6 windows/amd64, GOMAXPROCS = 16, GOGC = 100 unless stated |
| C compiler | gcc 14.2.0 (x86_64-posix-seh-rev1, MinGW-Builds / MinGW-w64), CGO_ENABLED = 1 |
| Python | 3.8.10, matplotlib 3.7.5, numpy 1.24.4, pandas 2.0.3, Pillow 9.5.0 (figures only) |
| Module | `github.com/savokh/goarena` v0.3.0, standard library only, `go 1.22` in go.mod |
| Protocol | `work/BENCH_RUNNING.flag` present during every benchmark session, removed afterwards |

## 2. How to reproduce

```powershell
cd D:\master-diploma\goarena
$env:PATH = "C:\Users\Vlad\mingw64\bin;$env:PATH"; $env:CGO_ENABLED = "1"
.\build.ps1 -Race -Cover
.\scripts\run_tests.ps1 -FuzzTime 60s          # all test logs into results\logs
bash scripts/run_v3.sh                         # v0.3.0 session (~37 min)
bash scripts/run_all_v2.sh                     # full v0.2.0-style matrix incl. E3, E8 (~70 min)
python scripts\make_tables_v3.py
cd ..\work\figscripts; python fig4_experiments.py; python fig4_target.py; python fig4_v1v2v3.py; python fig4_tail.py; python fig4_terms.py
```

## 3. Headline numbers

* **Service with request garbage (E9; every operation allocates a 1 KiB
  request object):** goarena with `store.Config.GCPacing` (`goarena-paced`)
  **10.05 ± 0.44 Mops/s at N = 5M and 9.94 ± 0.55 at 10M**, versus gobytes
  9.55 / 9.92, goarena with default settings 5.03 / 5.19, gomap 4.98 / 4.22,
  gomap GOGC=400 7.04 / 1.80, sync.Map 2.07 / 1.57. GC cycles per second:
  paced 11.3 / 5.7, gobytes 11.2 / 5.9, default goarena 306 / 331; GC CPU
  paced 0.40 / 0.25 %; p99.9 66 / 55 µs (gobytes 98 / 77 µs).
* **Container memory budget (E10, GOMEMLIMIT = 1210 MiB for all, N = 5M):**
  goarena-paced **9.17 ± 0.40 Mops/s, RSS 1132 MiB, p99.9 82 µs** versus
  gobytes 9.52 Mops/s, 1159 MiB, 105 µs (goarena -3.7 % throughput, -2.3 %
  memory, -22 % p99.9); gomap 1.07 Mops/s at 55 % GC CPU, sync.Map 0.71 at
  74 %.
* **Memory, E4 (N = 2M):** goarena v0.3.0 RSS versus gobytes: 64 B -12.5 %,
  256 B -6.5 %, **1 KiB -1.4 %** (v0.2.0: +16.6 %), **4 KiB +4.2 %**
  (v0.2.0: +12.8 %); internal fragmentation 4.2 / 1.4 / 0.4 / 2.4 %.
* **E1 at N = 10M (E1r):** goarena 32.37 ± 1.75 Mops/s (gobytes 25.38,
  gomap 23.36, gomap GOGC=400 23.09, sync.Map 10.93), p99 0.90 µs, p99.9
  4.36 µs (baselines 17.8-28.7 µs), RSS 1797 MiB (gobytes 1912, gomap 4050).
* **Tail (E1t):** goarena's operations > 1 ms per million samples fall from
  10.6 with 16 workers to 0.78 with 15 and 0.73 with 14 (gobytes 0.32 / 0 / 0).
* **Size classes:** 201 (8 B steps to 256 B, 32 per power of two to 8 KiB,
  x1.25 to 64 KiB); mean waste for uniform sizes 8.44 % (17 B-64 KiB) and
  1.04 % (257 B-4 KiB); worst case < 3.1 % between 256 B and 8 KiB.
* From the v0.2.0 session (unchanged code paths): Zipf hot keys 48.66 Mops/s
  and p99.9 1.01 µs (sync.Map 22.12 / 5.44); one full GC 0.27-0.70 ms at
  every N from 1M to 50M (gomap 26 ms -> 316 ms -> 2-23 s); N = 50M: 25.57
  Mops/s, 8.5 GiB RSS (gobytes 20.12, 9.0 GiB); empty cgo call 38.4 ns vs Go
  call 1.37 ns.

## 4. Code, tests and quality (final v0.3.0 run)

### 4.1 Size (`scripts/count_loc.py`, `results/loc.csv`)

| package | files | source lines (code) | test lines (code) |
|---|---|---|---|
| `internal/cmem` | 3 | 213 (121) | 189 (165) |
| `internal/tsc` | 6 | 95 (51) | 44 (40) |
| `internal/latency` | 2 | 203 (151) | 94 (88) |
| `arena` | 9 | 1146 (799) | 866 (791) |
| `store` | 15 | 1182 (789) | 1421 (1270) |
| `gcpace` (new) | 3 | 339 (238) | 215 (194) |
| `gcstats` | 6 | 422 (331) | 85 (74) |
| `cmd/goarena-bench` | 7 | 1779 (1550) | 313 (298) |
| `examples/httpcache` | 5 | 683 (566) | 169 (159) |
| root (`doc.go`) | 1 | 25 (2) | 0 |
| **total (Go + asm)** | **57** | **6087 (4598)** | **3396 (3079)** |

### 4.2 Tests

| package | Test* | Example* | Fuzz* | coverage |
|---|---|---|---|---|
| `arena` | 19 | 3 | - | 97.4 % |
| `store` | 22 | 5 | 1 | 99.3 % |
| `gcpace` | 6 | 1 | - | 97.6 % |
| `gcstats` | 4 | - | - | 96.7 % |
| `internal/cmem` | 6 | - | - | 100.0 % |
| `internal/latency` | 4 | - | - | 91.4 % |
| `internal/tsc` | 2 | - | - | 87.5 % |
| `cmd/goarena-bench` | 8 | - | - | 88.3 % |
| `examples/httpcache` | 4 | - | - | 94.2 % |
| **total** | **75** | **9** | **1** | **93.9 %** of all statements |

* `go test -count=1 -v ./...`: 85 top-level tests and 15 subtests, all PASS
  (`test_v.txt`, `test.txt`; after the final formatting `test_final.txt`).
* `go test -race -count=1 -v ./...`: all PASS, 0 data races (`race.txt`).
* Coverage 93.9 % (`coverage.out`, `cover.txt`, `cover_func.txt`).
* Fuzzing: `go test -run XXX -fuzz FuzzStore -fuzztime 60s -fuzzminimizetime
  5s ./store` - **1 463 001 executions in 60 s, 16 workers, 10 new interesting
  inputs (210 in the corpus), PASS** (`fuzz.txt`).
* New v0.3.0 tests: `TestPercent` (formula incl. the E9 case 16 MiB live +
  960 MiB off-heap -> GOGC 6100), `TestGoLimit`, `TestPacerAdjustsAndRestores`,
  `TestPacerInheritsSettings` (GOGC=200 and a 2 GiB limit; GOGC=off left
  alone), `TestDefaultPacerRefcount`, `TestPacingReducesGCFrequency`
  (320 MiB of garbage: 105 GC cycles without, 1 with 256 MiB of registered
  off-heap memory), `TestGCPacing` (store integration, restore on Close),
  `ExampleStart`; `TestHybridClassTable` rewritten for the 201-class table;
  `TestOffHeapIsInvisibleToGC` now measures heap growth relative to an empty
  store (the class tables add a constant ~2 MiB per store).
* `go vet ./...` clean, `gofmt -l .` 0 files, `go build ./...` ok.

## 5. What changed in v0.3.0 (details: DESIGN_NOTES.md §5.1, §15, §16)

1. **GC pacing (new package `gcpace`, opt-in `store.Config.GCPacing`).** Every
   50 ms the pacer sets `GOGC_eff = GOGC * (live + offHeap) / live` (bounded),
   so the heap goal `live + (live + offHeap) * GOGC/100` equals that of a
   heap-resident store of the same size; GC frequency is equalised with what
   such a store would get while each cycle still marks only the small Go
   heap. If a GOMEMLIMIT is in effect (or a budget is configured) it is
   treated as the budget for Go + off-heap memory and the Go limit is set to
   `budget - offHeap`. Process-wide, reference-counted, restores GOGC and
   GOMEMLIMIT when the last paced store closes. Documented in README
   ("Recommended configuration for services") as the recommended setting for
   services that allocate per request.
2. **Log-linear size classes (201 instead of 56).** Candidates were compared
   by mean internal waste and by the waste of the E4 entries (DESIGN_NOTES
   §5.1): fixed 32 B / 128 B steps (92 classes) still wasted 19.5 % on 4 KiB
   values (4124-byte entries fall just above 4096); 16 classes per power of
   two (121) left 5.2 %; 32 per power of two up to 8 KiB was chosen. Lookup
   remains one table load.
3. Harness: implementation `goarena-paced`; experiments E1r and E1t; the
   `-memlimit` setting is now applied before the cache is created (needed so
   that the pacer inherits it; for the other implementations this only moves
   it before the allocation of empty shards); result JSON records the goarena
   version; new metrics (ops > 1 ms per million, pacer GOGC and Go limit).

## 6. Results tables

### 6.1 v0.3.0 session (E1r, E1t, E4, E9, E10)

### E1r - E1 at N = 10M re-run with v0.3.0 (3 runs, all implementations)

| impl | runs | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | ops > 1 ms per million | GC CPU % | Go heap MiB | RSS MiB | off-heap MiB | populate s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 3 | 23.36 ± 0.38 | 0.51 | 1.94 | 17.83 | 35.31 | 4.2 | 5.69 | 3466 | 4050 | 0 | 1.30 |
| gomap-gogc400 | 3 | 23.09 ± 2.20 | 0.52 | 2.46 | 23.42 | 15.59 | 1.1 | 3.88 | 7216 | 7402 | 0 | 1.11 |
| syncmap | 3 | 10.93 ± 0.37 | 1.03 | 2.55 | 21.59 | 119.50 | 27.1 | 15.72 | 6274 | 6538 | 0 | 1.76 |
| gobytes | 3 | 25.38 ± 1.30 | 0.45 | 1.67 | 28.67 | 3.87 | 0.3 | 0.00 | 1914 | 1912 | 0 | 0.60 |
| goarena | 3 | 32.37 ± 1.75 | 0.37 | 0.90 | 4.36 | 20.02 | 10.6 | 0.00 | 4 | 1797 | 1856 | 0.58 |

### E1t - tail vs CPU oversubscription: N = 10M, GOMAXPROCS = 16, 14 / 15 / 16 workers (16 = E1r; 2 runs for 14/15, 3 for 16)

| impl | G | runs | Mops/s | p99 µs | p99.9 µs | max ms (mean) | max ms (min - max) | ops > 1 ms per million | process CPU s (15 s) |
|---|---|---|---|---|---|---|---|---|---|
| gomap | 14 | 2 | 23.01 ± 0.24 | 1.55 | 7.97 | 30.28 | 28.32 - 32.23 | 4.87 | 205.7 |
| gomap-gogc400 | 14 | 2 | 23.30 ± 0.00 | 1.90 | 8.57 | 23.77 | 13.24 - 34.31 | 1.63 | 206.1 |
| syncmap | 14 | 2 | 12.18 ± 0.20 | 2.06 | 10.75 | 45.97 | 34.23 - 57.72 | 5.57 | 212.3 |
| gobytes | 14 | 2 | 27.98 ± 0.33 | 1.12 | 8.16 | 0.38 | 0.32 - 0.44 | 0.00 | 203.2 |
| goarena | 14 | 2 | 33.18 ± 1.35 | 0.87 | 2.79 | 11.25 | 3.37 - 19.13 | 0.73 | 202.9 |
| gomap | 15 | 2 | 23.79 ± 0.08 | 1.68 | 11.52 | 33.40 | 28.93 - 37.87 | 4.64 | 214.2 |
| gomap-gogc400 | 15 | 2 | 24.38 ± 0.07 | 2.04 | 10.62 | 16.38 | 1.28 - 31.48 | 0.94 | 214.9 |
| syncmap | 15 | 2 | 12.39 ± 0.36 | 2.09 | 13.05 | 44.18 | 33.71 - 54.64 | 10.82 | 218.7 |
| gobytes | 15 | 2 | 29.05 ± 0.50 | 1.18 | 10.37 | 0.35 | 0.35 - 0.36 | 0.00 | 210.8 |
| goarena | 15 | 2 | 34.89 ± 0.49 | 0.88 | 2.21 | 4.08 | 3.15 - 5.00 | 0.78 | 215.7 |
| gomap | 16 | 3 | 23.36 ± 0.38 | 1.94 | 17.83 | 35.31 | 28.80 - 45.92 | 4.16 | 221.2 |
| gomap-gogc400 | 16 | 3 | 23.09 ± 2.20 | 2.46 | 23.42 | 15.59 | 0.53 - 32.08 | 1.07 | 214.5 |
| syncmap | 16 | 3 | 10.93 ± 0.37 | 2.55 | 21.59 | 119.50 | 95.78 - 131.58 | 27.10 | 211.0 |
| gobytes | 16 | 3 | 25.38 ± 1.30 | 1.67 | 28.67 | 3.87 | 1.39 - 8.30 | 0.32 | 202.2 |
| goarena | 16 | 3 | 32.37 ± 1.75 | 0.90 | 4.36 | 20.02 | 16.03 - 23.61 | 10.57 | 214.0 |

### E4 - value size, N = 2M, v0.3.0 size classes (1 run)

| impl | value B | Mops/s | p99 µs | p99.9 µs | RSS MiB | private MiB | off-heap MiB | internal fragmentation | populate s |
|---|---|---|---|---|---|---|---|---|---|
| gomap | 64 | 30.95 | 1.52 | 11.90 | 750 | 759 | 0 | - | 0.21 |
| gobytes | 64 | 28.96 | 1.39 | 33.28 | 306 | 321 | 0 | - | 0.10 |
| goarena | 64 | 37.85 | 0.73 | 2.40 | 268 | 279 | 256 | 4.2 % | 0.07 |
| gomap | 256 | 25.88 | 1.84 | 19.20 | 1503 | 1514 | 0 | - | 0.33 |
| gobytes | 256 | 28.66 | 1.46 | 20.22 | 670 | 706 | 0 | - | 0.15 |
| goarena | 256 | 30.17 | 1.07 | 5.31 | 627 | 664 | 640 | 1.4 % | 0.13 |
| gomap | 1024 | 16.51 | 2.40 | 29.95 | 4605 | 4623 | 0 | - | 0.65 |
| gobytes | 1024 | 17.32 | 2.14 | 24.32 | 2123 | 2165 | 0 | - | 0.38 |
| goarena | 1024 | 18.55 | 1.52 | 6.59 | 2094 | 2138 | 2112 | 0.4 % | 0.36 |
| gomap | 4096 | 6.01 | 10.88 | 182.27 | 16011 | 16060 | 0 | - | 1.87 |
| gobytes | 4096 | 6.32 | 12.41 | 133.12 | 8070 | 8124 | 0 | - | 1.46 |
| goarena | 4096 | 6.75 | 3.23 | 26.37 | 8413 | 8487 | 8448 | 2.4 % | 1.64 |

E4 memory across versions (RSS, internal fragmentation in parentheses):

| value B | goarena v0.1.0 RSS | v0.2.0 | v0.3.0 | gobytes (v0.3.0 session) | v0.3.0 vs gobytes |
|---|---|---|---|---|---|
| 64 | 313 MiB (16.7 %) | 267 MiB (4.2 %) | 268 MiB (4.2 %) | 306 MiB | -12.5 % |
| 256 | 665 MiB (3.9 %) | 689 MiB (11.2 %) | 627 MiB (1.4 %) | 670 MiB | -6.5 % |
| 1024 | 2355 MiB (10.5 %) | 2479 MiB (15.2 %) | 2094 MiB (0.4 %) | 2123 MiB | -1.4 % |
| 4096 | 9011 MiB (9.1 %) | 9100 MiB (13.2 %) | 8413 MiB (2.4 %) | 8070 MiB | +4.2 % |

### E9 - service with request garbage (1 KiB request object per operation), 3 runs

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles/s | GC CPU % | STW ms/s | Go heap MiB | RSS MiB | peak RSS MiB | pacer GOGC (last) | pacer Go limit MiB |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 4.98 ± 0.19 | 1.19 | 14.97 | 378.20 | 41.68 | 3.5 | 36.05 ± 0.88 | 0.5 | 3085 | 3295 | 3302 | - | - |
| gomap-gogc400 | 5 M | 7.04 ± 0.48 | 1.20 | 13.52 | 165.89 | 39.61 | 0.9 | 14.13 ± 1.41 | 0.8 | 13690 | 16917 | 16928 | - | - |
| syncmap | 5 M | 2.07 ± 0.20 | 1.72 | 24.49 | 920.23 | 511.29 | 1.0 | 59.03 ± 1.57 | 0.2 | 4460 | 9848 | 10072 | - | - |
| gobytes | 5 M | 9.55 ± 0.20 | 1.11 | 4.50 | 97.96 | 5.37 | 11.2 | 0.63 ± 0.04 | 5.0 | 1741 | 1973 | 1973 | - | - |
| goarena | 5 M | 5.03 ± 0.10 | 0.89 | 22.78 | 383.66 | 11.32 | 305.8 | 8.23 ± 0.02 | 106.4 | 25 | 946 | 1034 | - | - |
| goarena-gogc400 | 5 M | 7.51 ± 0.28 | 1.04 | 9.77 | 191.83 | 23.38 | 82.3 | 3.07 ± 0.39 | 47.9 | 102 | 1337 | 1537 | - | - |
| goarena-paced | 5 M | 10.05 ± 0.44 | 1.06 | 3.76 | 66.05 | 25.53 | 11.3 | 0.40 ± 0.11 | 6.5 | 788 | 2017 | 2074 | 24100 | - |
| gomap | 10 M | 4.22 ± 0.48 | 1.27 | 13.70 | 495.62 | 43.99 | 1.5 | 41.71 ± 0.92 | 0.3 | 5792 | 6474 | 6500 | - | - |
| gomap-gogc400 | 10 M | 1.80 ± 1.15 | 1.23 | 45.61 | 1260.20 | 1076.44 | 0.1 | 10.23 ± 3.90 | 0.3 | 21038 | 10118 | 18366 | - | - |
| syncmap | 10 M | 1.57 ± 0.85 | 1.79 | 82.52 | 1627.48 | 1002.56 | 0.3 | 47.76 ± 13.12 | 0.0 | 12843 | 12108 | 18272 | - | - |
| gobytes | 10 M | 9.92 ± 0.21 | 1.18 | 3.89 | 76.80 | 1.96 | 5.9 | 0.43 ± 0.03 | 2.2 | 3562 | 3840 | 3840 | - | - |
| goarena | 10 M | 5.19 ± 0.16 | 0.90 | 22.10 | 353.62 | 13.11 | 331.2 | 8.26 ± 0.15 | 123.1 | 23 | 1834 | 1954 | - | - |
| goarena-gogc400 | 10 M | 7.63 ± 0.25 | 1.05 | 10.28 | 189.10 | 20.13 | 93.5 | 3.11 ± 0.30 | 52.4 | 70 | 2206 | 2463 | - | - |
| goarena-paced | 10 M | 9.94 ± 0.55 | 1.10 | 3.19 | 54.78 | 46.25 | 5.7 | 0.25 ± 0.17 | 3.4 | 1597 | 3861 | 3861 | 46500 | - |

### E10 - container memory budget: E9 workload, N = 5M, GOMEMLIMIT = 1210 MiB, 3 runs

| impl | N | Mops/s | p50 µs | p99 µs | p99.9 µs | max ms | GC cycles/s | GC CPU % | STW ms/s | Go heap MiB | RSS MiB | peak RSS MiB | pacer GOGC (last) | pacer Go limit MiB |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gomap | 5 M | 1.07 ± 0.04 | 1.39 | 152.57 | 854.70 | 76.26 | 7.3 | 54.62 ± 1.13 | 1.6 | 1299 | 1362 | 1577 | - | - |
| gomap-gogc400 | 5 M | 1.04 ± 0.06 | 1.41 | 181.25 | 947.54 | 76.35 | 7.4 | 55.36 ± 0.52 | 1.3 | 1256 | 1326 | 1565 | - | - |
| syncmap | 5 M | 0.71 ± 0.04 | 1.82 | 19.71 | 712.70 | 598.45 | 1.4 | 73.76 ± 0.28 | 0.0 | 1906 | 2079 | 3379 | - | - |
| gobytes | 5 M | 9.52 ± 0.10 | 1.10 | 4.93 | 104.79 | 16.27 | 56.2 | 2.52 ± 0.28 | 20.5 | 1092 | 1159 | 1200 | - | - |
| goarena | 5 M | 5.50 ± 0.35 | 0.85 | 21.42 | 334.51 | 25.68 | 346.1 | 7.71 ± 0.05 | 121.5 | 19 | 990 | 1057 | - | - |
| goarena-gogc400 | 5 M | 7.93 ± 0.30 | 1.01 | 9.86 | 185.00 | 16.67 | 97.6 | 2.91 ± 0.33 | 51.1 | 98 | 1387 | 1555 | - | - |
| goarena-budget | 5 M | 9.09 ± 0.52 | 1.04 | 4.60 | 82.26 | 36.27 | 50.8 | 1.47 ± 0.22 | 26.7 | 168 | 1140 | 1196 | - | - |
| goarena-paced | 5 M | 9.17 ± 0.40 | 1.04 | 4.97 | 81.58 | 29.37 | 51.5 | 1.59 ± 0.22 | 29.1 | 193 | 1132 | 1195 | 17950 | 250 |

### goarena v0.1.0 -> v0.2.0 -> v0.3.0 (default configuration unless stated; baselines unchanged)

| case | metric | v0.1.0 | v0.2.0 | v0.3.0 | best baseline (v0.3.0 session) |
|---|---|---|---|---|---|
| E1 N=10M | throughput Mops/s | 31.80 | 34.96 | 32.37 | 25.38 (gobytes) |
| E1 N=10M | p99.9 µs | 19.71 | 2.39 | 4.36 | 17.83 (gomap) |
| E1 N=10M | RSS MiB | 2103.22 | 1795.68 | 1796.52 | 1911.79 (gobytes) |
| E4 1 KiB | RSS MiB | 2354.55 | 2479.42 | 2093.67 | 2122.77 (gobytes) |
| E4 4 KiB | RSS MiB | 9010.89 | 9100.25 | 8413.08 | 8070.39 (gobytes) |
| E9 N=5M | throughput Mops/s (v0.3.0: paced) | - | 4.73 | 10.05 | 9.55 (gobytes) |
| E9 N=10M | throughput Mops/s (v0.3.0: paced) | - | 4.73 | 9.94 | 9.92 (gobytes) |
| E9 N=5M | GC cycles/s (v0.3.0: paced) | - | 358.07 | 11.25 | 0.87 (gomap-gogc400) |
| E10 5M, 1210 MiB | throughput Mops/s (v0.3.0: paced) | - | 5.28 | 9.17 | 9.52 (gobytes) |
| E10 5M, 1210 MiB | RSS MiB (v0.3.0: paced) | - | 943.27 | 1131.94 | 1159.34 (gobytes) |

### 6.2 Experiments reported from the v0.2.0 session (code paths unchanged in v0.3.0)

### E1 (v0.2.0 session) - throughput, latency, GC and memory vs N (mean ± std of 3 runs)

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

## 7. v0.1.0 -> v0.2.0 -> v0.3.0

| case | v0.1.0 | v0.2.0 | v0.3.0 | best baseline | where |
|---|---|---|---|---|---|
| E1 10M throughput, Mops/s | 31.80 | 34.96 | 32.37 | 25.38 gobytes (v0.3.0 session) | E1 / E1 / E1r |
| E1 10M p99.9, µs | 19.71 | 2.39 | 4.36 | 17.83 gomap | |
| E1 10M RSS, MiB | 2103 | 1796 | 1797 | 1912 gobytes | |
| E6 Zipf 5M throughput, Mops/s | 17.52 | 48.66 | (= v0.2.0) | 22.12 sync.Map | E6 |
| E6 Zipf 5M p99.9, µs | 115.7 | 1.01 | (= v0.2.0) | 5.44 sync.Map | |
| E4 1 KiB RSS, MiB | 2355 | 2479 | **2094** | 2123 gobytes | E4 |
| E4 4 KiB RSS, MiB | 9011 | 9100 | **8413** | 8070 gobytes | E4 |
| E9 5M throughput, Mops/s | - | 4.73 | **10.05 (paced)**; 5.03 default | 9.55 gobytes | E9 |
| E9 10M throughput, Mops/s | - | 4.73 | **9.94 (paced)**; 5.19 default | 9.92 gobytes | E9 |
| E9 5M GC cycles/s | - | 358 | **11.3 (paced)** | 11.2 gobytes | E9 |
| E10 throughput, Mops/s | - | 5.28 (8.59 hand-tuned budget) | **9.17 (paced)** | 9.52 gobytes | E10 |
| E10 RSS, MiB | - | 943 | 1132 (paced) | 1159 gobytes | E10 |

v0.1.0 -> v0.2.0 (hybrid classes, 8-byte header, seqlock reads, recycled
index tables) is described in `results/v2/SUMMARY.md` §7 and DESIGN_NOTES §14;
the v0.2.0 -> v0.3.0 changes in §5 above.

Figures: `fig4_v1v2v3_memory.png` (RSS of E1 10M and E4 1/4 KiB across the
three versions and gobytes) and `fig4_v1v2v3_service.png` (E9/E10 throughput
and GC cycles/s: v0.2.0 default vs v0.3.0 paced vs gobytes);
`fig4_v1v2_latency.png` / `fig4_v1v2_throughput.png` / `fig4_v1v2_memory.png`
(v1 vs v2) remain valid.

## 8. Interpretation

### 8.1 What GC pacing changes (E9, E10)

* Without pacing, the Go heap of a goarena process contains only short-lived
  request objects (16-25 MiB); its heap goal is ~16-21 MiB, so the 5.5-5.7
  GiB/s of request garbage triggers a GC every ~18 ms (306-331 cycles/s).
  Each cycle is cheap (GC CPU 8.2 %), but the STW phases (106-123 ms per
  second in total), mark assists and write barriers halve the throughput.
* With pacing the heap goal becomes ~1.0 GiB (5M) / 1.9 GiB (10M) - what a
  heap-resident store of the same size would have (gobytes: 1.9 / 3.7 GiB
  heap goal). GC frequency then matches gobytes (11.3 vs 11.2 and 5.7 vs 5.9
  cycles/s), GC CPU drops to 0.25-0.40 % (gobytes 0.43-0.63 %, gomap
  36-42 %), and goarena is +5 % (5M) / +0.2 % (10M) faster than gobytes with
  a 33-29 % lower p99.9. The price is the same as for the heap-resident
  stores: the garbage now occupies memory (RSS 2017 / 3861 MiB vs gobytes
  1973 / 3840 MiB and 946 / 1834 MiB without pacing).
* With a container budget (E10) the pacer turns the inherited GOMEMLIMIT
  into "budget - off-heap" (250 MiB) automatically: 9.17 Mops/s inside the
  budget (RSS 1132 MiB, peak 1195), equal to the hand-tuned
  `goarena-budget` (9.09) and 3.7 % below gobytes (9.52, RSS 1159 MiB). The
  Go-heap maps cannot fit their 1.3-2 GiB of live data into the limit and
  thrash (gomap 55 % GC CPU, 1.07 Mops/s; sync.Map 74 %, 0.71 Mops/s); their
  RSS exceeds the soft limit.
* Remaining gap: within the budget goarena-paced still runs 51.5 GC
  cycles/s (gobytes 56.2) and spends 29 ms/s in STW phases (gobytes 20.5),
  because the Go limit left for garbage is 250 MiB (gobytes' heap goal is
  1138 MiB, of which most is its pointer-free data).

### 8.2 Size classes (E4)

The log-linear table removes the v0.2.0 regression: 1 KiB values now need
1.4 % less memory than gobytes (2094 vs 2123 MiB; v0.2.0 +16.6 %), 256 B
values 6.5 % less, 64 B 12.5 % less. With 4 KiB values goarena still needs
4.2 % more (8413 vs 8070 MiB): 2.4 % class rounding (4124-byte entries in
4224-byte blocks) plus slab spans and the index; gobytes appends entries
back to back without rounding. Throughput is the best of the three at
every size (6.75 vs 6.32 Mops/s at 4 KiB), p99.9 2.4-6.6 µs vs 20-133 µs.

### 8.3 Tail and CPU oversubscription (E1t)

| G (workers, GOMAXPROCS 16) | goarena ops > 1 ms per million | goarena max ms (mean of runs) | gobytes ops > 1 ms per million | gomap |
|---|---|---|---|---|
| 16 | 10.57 | 20.0 | 0.32 | 4.16 |
| 15 | 0.78 | 4.1 | 0 | 4.64 |
| 14 | 0.73 | 11.3 (3.4 and 19.1) | 0 | 4.87 |

With one hardware thread left free, goarena's > 1 ms outliers drop by a
factor of ~14 and its p99.9 improves from 4.36 to 2.2-2.8 µs (the
baselines' p99.9 also improve, 17-29 -> 8-13 µs). So most of the extreme
tail of v0.2.0 is CPU oversubscription by never-blocking readers, not the
store. A residue remains that gobytes does not show (0.7-0.8 per million,
one 19 ms maximum): plausible but unverified cause - a writer preempted
inside its critical section, which makes readers of that shard spin and then
fall back to the lock. gomap's outliers (4-5 per million) do not depend on G:
they come from GC work. Honest conclusion: the store itself contributes
little to the > 1 ms tail, but it does not eliminate it either.

### 8.4 E1r (regression check)

goarena 32.37 Mops/s at N = 10M versus 34.96 in the v0.2.0 session. The whole
v0.3.0 session ran slower on this laptop (gobytes 25.38 vs 28.68, -11.5 %;
goarena -7.4 %; gomap +8.4 % - gomap's v0.2.0 mean was pulled down by one
slow run). The goarena/gobytes ratio is 1.28 (v0.2.0: 1.22), RSS is
unchanged (1797 MiB), p99.9 4.36 µs (v0.2.0 2.39 µs; with 15 workers 2.21 µs).
No regression attributable to the v0.3.0 changes; session-to-session
variation of 7-12 % is a property of the machine.

### 8.5 Where goarena still loses (all versions, v0.3.0 numbers where re-run)

1. Request garbage with **default** GC settings (E9: 5.03 vs 9.55 Mops/s
   for gobytes) - fixed by the opt-in pacer, not by default.
2. Within a tight memory budget it is 3.7 % slower than gobytes (E10).
3. 4 KiB values: +4.2 % memory versus gobytes (E4).
4. Rare > 1 ms outliers (8.3).
5. From v0.2.0 (unchanged): single-threaded `NewValue[T]` slower than
   `new(T)`; a cgo call costs 38-68 ns; GC pause statistics on Windows are
   quantised to 0.5 ms.

## 9. Alignment with chapter 2

* Shard reservation: shared chunk pool (one arena per store, 64 KiB spans,
  index tables in the arena; v0.2.0 numbers in `results/v2/SUMMARY.md` §9).
* Size classes: v0.1.0 had the chapter-2 formula (36 classes); v0.2.0 56
  hybrid; **v0.3.0 201 log-linear classes, mean waste 8.44 % (17 B-64 KiB),
  1.04 % (257 B-4 KiB), measured 2.5 % (E1 entries) and 4.2 / 1.4 / 0.4 /
  2.4 % (E4 entries)**.
* GC model: the pacing formula is the chapter-2 pacer model applied to
  `live + offHeap`; measured GC frequency with pacing equals that of the
  heap-resident pointer-free store (11.3 vs 11.2 cycles/s) while GC CPU
  stays 0.25-0.40 % (mark work proportional to the small scannable heap).
* GOMEMLIMIT: E10 shows the predicted thrashing of heap-resident maps
  (55-74 % GC CPU) under a 1.21 GiB limit.

## 10. Figures for chapter 4

Regenerated in v0.3.0 (data of the v0.3.0 session):

| file | proposed caption (UA) | description |
|---|---|---|
| `fig4_e4_value.png` | Рисунок 4.x – Вплив розміру значення на пропускну здатність і резидентну пам'ять | E4 with the 201-class table. |
| `fig4_e9_service.png` | Рисунок 4.x – Сервіс із «сміттям» запитів: пропускна здатність, p99,9, частка ЦП і частота циклів GC | E9 incl. goarena + GOGC=400 and goarena + GCPacing. |
| `fig4_e10_budget.png` | Рисунок 4.x – Робота в межах бюджету пам'яті контейнера (GOMEMLIMIT = 1210 МіБ) | E10, 8 configurations, dashed line = limit. |
| `fig4_v1v2v3_memory.png` | Рисунок 4.x – Оптимізація використання пам'яті: goarena v0.1.0, v0.2.0, v0.3.0 і gobytes | E1 10M and E4 1/4 KiB. |
| `fig4_v1v2v3_service.png` | Рисунок 4.x – Вплив компенсації пейсингу GC (GCPacing) на сервіс із «сміттям» запитів | E9/E10: v0.2.0 default vs v0.3.0 paced vs gobytes. |
| `fig4_e1t_tail.png` | Рисунок 4.x – Крайній «хвіст» затримок залежно від кількості робочих горутин | E1t: ops > 1 ms per million and p99.9 for 14/15/16 workers. |
| `fig4_term_test.png`, `fig4_term_race.png`, `fig4_term_cover.png`, `fig4_term_fuzz.png`, `fig4_term_vet_build.png` | (captions as in v0.2.0) | Re-rendered from the v0.3.0 logs. |

Unchanged from v0.2.0 (still valid, data of the v0.2.0 session; v0.2.0
copies also in `results/v2/figures/`): `fig4_e1_throughput.png`,
`fig4_e1_latency.png`, `fig4_e1_gccpu.png`, `fig4_e1_gcpause.png`,
`fig4_e1_memory.png`, `fig4_e1_ccdf.png`, `fig4_e1b_memlimit.png`,
`fig4_e2_chunk.png`, `fig4_e3_micro.png`, `fig4_e5_goroutines.png`,
`fig4_e6_workloads.png`, `fig4_e7_mark.png`, `fig4_e8_scale.png`,
`fig4_e8_gc.png`, `fig4_v1v2_*.png`, `fig4_term_bench.png` (E3 not re-run),
`fig4_dashboard.png`, `fig4_dashboard_gomap.png` (the example is unchanged and
pacing is off by default). Captions: `results/v2/SUMMARY.md` §10.

## 11. Appendix listings (Додаток А) - changed or new in v0.3.0

| file | lines | content |
|---|---|---|
| `gcpace/gcpace.go` | 339 | GC pacer (new) |
| `arena/slab.go` | 501 | 201-class log-linear table, free lists, spans, donations |
| `arena/arena.go` | 494 | + `ReservedBytes` |
| `store/store.go` | 726 | + `Config.GCPacing` |
| `cmd/goarena-bench/impl.go` | 337 | + `goarena-paced` |
| `cmd/goarena-bench/driver.go` | 409 | + E1r, E1t |
| `gcpace/gcpace_test.go` | 195 | pacer tests |

Other listings and line counts: `results/v2/SUMMARY.md` §11.

## 12. Deviations and caveats

* The v0.3.0 session re-ran only the affected experiments; mixing sessions
  is explicit everywhere (tables, figure list).
* E1r shows a 7-12 % slower machine state than the v0.2.0 session for all
  implementations; comparisons are made within a session.
* `goarena-paced` raises RSS by the garbage headroom (like heap-resident
  stores); with a memory limit this is bounded by the limit.
* The pacer changes process-wide GC settings; it is opt-in and restores them.
* All benchmark processes were stopped; `BENCH_RUNNING.flag` removed.
