#!/usr/bin/env bash
# Complete v2 benchmark session (13.10.2026): E1-E7 with the v1 parameters,
# the E3 micro-benchmarks, then the target-domain experiments E8, E9 and
# E10. E10's memory limit is derived from this session's E1 result
# (1.25 x the mean RSS of gobytes at N = 5M).
#
# usage (Git Bash): ./scripts/run_all_v2.sh
set -u
cd "$(dirname "$0")/.."
export PATH="/c/Users/Vlad/mingw64/bin:$PATH" CGO_ENABLED=1
unset GOGC GOMEMLIMIT
FLAG=../work/BENCH_RUNNING.flag
echo "goarena v2 benchmark session running since $(date) (code agent); do not run heavy jobs" > "$FLAG"
mkdir -p results/raw results/logs
LOG=results/logs/bench_matrix.txt
go build -o bin/goarena-bench.exe ./cmd/goarena-bench || exit 1
B=./bin/goarena-bench.exe
run() { echo "# goarena-bench $*" | tee -a "$LOG"; "$B" "$@" 2>&1 | tee -a "$LOG"; }

run matrix -exp E1,E1b,E2,E4,E5,E6,E7 -out results/raw -repeats 3 -duration 15s -warmup 3s -short 10s
run aggregate -in results/raw -out results

echo "# go test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./internal/tsc ./arena ./store" | tee -a "$LOG"
go test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./internal/tsc ./arena ./store 2>&1 | tee results/logs/microbench.txt
go test -run XXX -bench . -benchmem -count 1 ./internal/cmem ./arena ./store 2>&1 | tee results/logs/microbench_count1.txt
python scripts/parse_microbench.py results/logs/microbench.txt results/microbench.csv

run matrix -exp E8,E9 -out results/raw -repeats 3 -duration 15s -warmup 3s -short 10s -mem-budget 24
ML=$(python -c "import pandas as pd; d=pd.read_csv('results/summary.csv'); r=d[(d.experiment=='E1')&(d.impl=='gobytes')&(d.n==5000000)].iloc[0]; print(int(round(1.25*r.rss_mb_mean)))")
echo "# E10 memory limit = 1.25 x gobytes E1 RSS(N=5M) = $ML MiB" | tee -a "$LOG"
run matrix -exp E10 -out results/raw -repeats 3 -duration 15s -warmup 3s -e10-memlimit "$ML"
run aggregate -in results/raw -out results

rm -f "$FLAG"
echo "ALL V2 BENCHMARKS DONE $(date)" | tee -a "$LOG"
