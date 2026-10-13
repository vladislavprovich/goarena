#!/usr/bin/env bash
# goarena v0.3.0 benchmark session (13.10.2026): only the experiments
# affected by the v0.3.0 changes are re-run with the v0.2.0 parameters:
#   E1r - E1 at N = 10M, all implementations, 3 repeats (regression check)
#   E4  - value sizes (new size classes)
#   E9  - request garbage, + goarena-paced (store.Config.GCPacing)
#   E10 - container memory budget (1210 MiB as in v0.2.0), + goarena-paced
#   E1t - E1 at N = 10M with 14 and 15 worker goroutines (tail check)
# Results of all other experiments are those of the v0.2.0 session
# (results/v2), whose code paths are unchanged in v0.3.0.
set -u
cd "$(dirname "$0")/.."
export PATH="/c/Users/Vlad/mingw64/bin:$PATH" CGO_ENABLED=1
unset GOGC GOMEMLIMIT
FLAG=../work/BENCH_RUNNING.flag
echo "goarena v3 benchmark session running since $(date) (code agent); do not run heavy jobs" > "$FLAG"
mkdir -p results/raw results/logs
LOG=results/logs/bench_matrix.txt
go build -o bin/goarena-bench.exe ./cmd/goarena-bench || exit 1
B=./bin/goarena-bench.exe
run() { echo "# goarena-bench $*" | tee -a "$LOG"; "$B" "$@" 2>&1 | tee -a "$LOG"; }
run matrix -exp E1r,E4,E9 -out results/raw -repeats 3 -duration 15s -warmup 3s -short 10s
run matrix -exp E10 -out results/raw -repeats 3 -duration 15s -warmup 3s -e10-memlimit 1210
run matrix -exp E1t -out results/raw -repeats 2 -duration 15s -warmup 3s
run aggregate -in results/raw -out results
rm -f "$FLAG"
echo "ALL V3 BENCHMARKS DONE $(date)" | tee -a "$LOG"
