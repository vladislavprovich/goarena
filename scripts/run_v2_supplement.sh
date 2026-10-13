#!/usr/bin/env bash
# Supplement of the v2 session (13.10.2026, after scripts/run_all_v2.sh):
#  - E3 micro-benchmarks re-run after the fix of BenchmarkArenaAlloc128
#    (unbounded memory) and of cmem's malloc wrapper;
#  - E9/E10 runs of the tuned goarena variants goarena-gogc400 and
#    goarena-budget (the runs of the five base implementations already
#    exist and are skipped by the driver).
set -u
cd "$(dirname "$0")/.."
export PATH="/c/Users/Vlad/mingw64/bin:$PATH" CGO_ENABLED=1
unset GOGC GOMEMLIMIT
FLAG=../work/BENCH_RUNNING.flag
echo "goarena v2 supplement benchmarks running since $(date) (code agent)" > "$FLAG"
LOG=results/logs/bench_matrix.txt
go build -o bin/goarena-bench.exe ./cmd/goarena-bench || exit 1
echo "# go test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./internal/tsc ./arena ./store (re-run)" | tee -a "$LOG"
go test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./internal/tsc ./arena ./store 2>&1 | tee results/logs/microbench.txt
go test -run XXX -bench . -benchmem -count 1 ./internal/cmem ./arena ./store 2>&1 | tee results/logs/microbench_count1.txt
python scripts/parse_microbench.py results/logs/microbench.txt results/microbench.csv
echo "# goarena-bench matrix -exp E9 (tuned goarena variants)" | tee -a "$LOG"
./bin/goarena-bench.exe matrix -exp E9 -out results/raw -repeats 3 -duration 15s -warmup 3s 2>&1 | tee -a "$LOG"
echo "# goarena-bench matrix -exp E10 -e10-memlimit 1210 (tuned goarena variants)" | tee -a "$LOG"
./bin/goarena-bench.exe matrix -exp E10 -out results/raw -repeats 3 -duration 15s -warmup 3s -e10-memlimit 1210 2>&1 | tee -a "$LOG"
./bin/goarena-bench.exe aggregate -in results/raw -out results 2>&1 | tee -a "$LOG"
rm -f "$FLAG"
echo "V2 SUPPLEMENT DONE $(date)" | tee -a "$LOG"
