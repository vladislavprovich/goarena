#!/usr/bin/env python3
"""Generate the Markdown tables of results/SUMMARY.md directly from
results/summary.csv, results/runs.csv and results/microbench.csv, so that no
number is transcribed by hand.

usage: python scripts/make_tables.py [results-dir]  -> writes results/tables.md
"""
import os
import sys

import pandas as pd

R = sys.argv[1] if len(sys.argv) > 1 else "results"
S = pd.read_csv(os.path.join(R, "summary.csv"))
RUNS = pd.read_csv(os.path.join(R, "runs.csv"))
IMPLS = ["gomap", "gomap-gogc400", "syncmap", "gobytes", "goarena", "goarena-gogc400", "goarena-budget"]
out = []


def md(df, cols, headers):
    lines = ["| " + " | ".join(headers) + " |", "|" + "---|" * len(headers)]
    for _, r in df.iterrows():
        lines.append("| " + " | ".join(str(f(r)) if callable(f) else str(r[f]) for f in cols) + " |")
    return "\n".join(lines)


def f1(x):
    return "%.1f" % x


def f2(x):
    return "%.2f" % x


def mops(r, p="ops_per_sec"):
    return "%.2f ± %.2f" % (r[p + "_mean"] / 1e6, r[p + "_std"] / 1e6) if r["runs"] > 1 else "%.2f" % (r[p + "_mean"] / 1e6)


def us(col):
    return lambda r: "%.2f" % (r[col + "_mean"] / 1e3)


def ms(col):
    return lambda r: "%.2f" % (r[col + "_mean"] / 1e6)


def order(df):
    df = df.copy()
    df["_o"] = df.impl.map({k: i for i, k in enumerate(IMPLS)})
    return df.sort_values(["n", "_o"] if "n" in df else ["_o"])


# ------------------------------------------------------------------ E1
e1 = order(S[S.experiment == "E1"])
out.append("### E1 - throughput, latency, GC and memory vs N (mean ± std of 3 runs)\n")
out.append(md(e1, ["impl", lambda r: "%d M" % (r.n / 1e6), mops, us("p50_ns"), us("p99_ns"), us("p999_ns"), ms("max_ns"),
                   lambda r: f1(r.gc_cycles_mean), lambda r: "%.2f ± %.2f" % (r.gc_cpu_percent_mean, r.gc_cpu_percent_std),
                   lambda r: f2(r.pause_total_ms_mean), lambda r: f2(r.cycle_pause_max_ms_mean),
                   lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean,
                   lambda r: "%.0f" % r.offheap_mb_mean, lambda r: f2(r.populate_s_mean)],
              ["impl", "N", "Mops/s", "p50 µs", "p99 µs", "p99.9 µs", "max ms", "GC cycles", "GC CPU %",
               "STW total ms", "max cycle STW ms", "Go heap MiB", "RSS MiB", "off-heap MiB", "populate s"]))
out.append("")

# per-repeat throughput table for E1 (min/max)
out.append("E1 throughput, min - max over the 3 runs (Mops/s):\n")
piv = e1.pivot_table(index="impl", columns="n", values=["ops_per_sec_min", "ops_per_sec_max"])
lines = ["| impl | " + " | ".join("%d M" % (n / 1e6) for n in sorted(e1.n.unique())) + " |",
         "|---|" + "---|" * len(e1.n.unique())]
for impl in [i for i in IMPLS if i in set(e1.impl)]:
    cells = []
    for n in sorted(e1.n.unique()):
        cells.append("%.2f - %.2f" % (piv.loc[impl, ("ops_per_sec_min", n)] / 1e6, piv.loc[impl, ("ops_per_sec_max", n)] / 1e6))
    lines.append("| %s | %s |" % (impl, " | ".join(cells)))
out.append("\n".join(lines))
out.append("")

# relative gains of goarena
out.append("goarena relative to the other implementations (E1 means):\n")
lines = ["| N | vs gomap throughput | vs gobytes throughput | vs gomap p99 | vs gomap RSS | vs gobytes RSS |", "|---|---|---|---|---|---|"]
for n in sorted(e1.n.unique()):
    g = e1[(e1.n == n)].set_index("impl")
    a = g.loc["goarena"]
    lines.append("| %d M | %+.1f %% | %+.1f %% | %+.1f %% | %+.1f %% | %+.1f %% |" % (
        n / 1e6,
        100 * (a.ops_per_sec_mean / g.loc["gomap"].ops_per_sec_mean - 1),
        100 * (a.ops_per_sec_mean / g.loc["gobytes"].ops_per_sec_mean - 1),
        100 * (a.p99_ns_mean / g.loc["gomap"].p99_ns_mean - 1),
        100 * (a.rss_mb_mean / g.loc["gomap"].rss_mb_mean - 1),
        100 * (a.rss_mb_mean / g.loc["gobytes"].rss_mb_mean - 1)))
out.append("\n".join(lines))
out.append("")

# goarena memory breakdown
ga = e1[e1.impl == "goarena"]
out.append("goarena memory breakdown (E1, MiB): reserved arena vs used slab blocks vs exact entry bytes:\n")
out.append(md(ga, [lambda r: "%d M" % (r.n / 1e6), lambda r: "%.0f" % r.arena_reserved_mb_mean,
                   lambda r: "%.0f" % r.slab_inuse_mb_mean, lambda r: "%.0f" % r.entry_mb_mean,
                   lambda r: "%.0f" % r.index_mb_mean, lambda r: "%.1f %%" % (100 * r.fragmentation_mean),
                   lambda r: "%.0f" % r.arena_chunks_mean, lambda r: "%.0f" % r.populate_cgo_calls_mean,
                   lambda r: "%.2f" % r.heap_objects_mb_mean],
              ["N", "arena reserved", "slab in use", "entries (exact)", "index", "internal fragmentation",
               "64 MiB chunks", "cgo calls (populate)", "Go heap MiB"]))
out.append("")

# ------------------------------------------------------------------ E1b
e = order(S[S.experiment == "E1b"])
if not e.empty:
    out.append("### E1b - GOMEMLIMIT = 4 GiB (debug.SetMemoryLimit), 3 runs\n")
    out.append(md(e, ["impl", lambda r: "%d M" % (r.n / 1e6), mops, us("p99_ns"), us("p999_ns"), ms("max_ns"),
                      lambda r: f1(r.gc_cycles_mean), lambda r: "%.2f ± %.2f" % (r.gc_cpu_percent_mean, r.gc_cpu_percent_std),
                      lambda r: f1(r.mark_ms_mean_mean), lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean],
                  ["impl", "N", "Mops/s", "p99 µs", "p99.9 µs", "max ms", "GC cycles", "GC CPU %", "mean mark ms (gctrace)",
                   "Go heap MiB", "RSS MiB"]))
    out.append("")
    rr = RUNS[RUNS.experiment == "E1b"].copy()
    rr["_o"] = rr.impl.map({k: i for i, k in enumerate(IMPLS)})
    rr = rr.sort_values(["repeat", "n", "_o"])
    out.append("E1b individual runs (Mops/s, GC CPU %):\n")
    out.append(md(rr, ["repeat", "impl", lambda r: "%d M" % (r.n / 1e6), lambda r: f2(r.ops_per_sec / 1e6),
                       lambda r: f2(r.gc_cpu_percent), lambda r: "%d" % r.gc_cycles],
                  ["repeat", "impl", "N", "Mops/s", "GC CPU %", "GC cycles"]))
    out.append("")

# ------------------------------------------------------------------ E7
e = order(S[S.experiment == "E7"])
if not e.empty:
    out.append("### E7 - cost of one GC cycle vs N (1 run each, gctrace + forced runtime.GC())\n")
    out.append(md(e, ["impl", lambda r: "%d M" % (r.n / 1e6), lambda r: "%.0f" % r.trace_cycles_mean,
                      lambda r: f1(r.mark_ms_mean_mean), lambda r: f2(r.forced_gc_ms_mean), lambda r: f1(r.heap_scan_mb_mean),
                      lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: f2(r.gc_cpu_percent_mean), mops],
                  ["impl", "N", "GC cycles (10 s)", "mean concurrent mark ms", "forced runtime.GC() ms",
                   "scannable heap MiB", "Go heap MiB", "GC CPU %", "Mops/s"]))
    out.append("")

# ------------------------------------------------------------------ E2
e = S[S.experiment == "E2"].sort_values("chunk_mib")
if not e.empty:
    out.append("### E2 - goarena chunk size, N = 5M (3 runs)\n")
    out.append(md(e, [lambda r: "%d MiB" % r.chunk_mib, lambda r: "%.3f ± %.3f" % (r.populate_s_mean, r.populate_s_std),
                      lambda r: "%.0f" % r.populate_cgo_calls_mean, lambda r: "%.0f" % r.arena_chunks_mean,
                      lambda r: "%.0f" % r.arena_reserved_mb_mean, lambda r: "%.0f" % r.slab_inuse_mb_mean,
                      lambda r: "%.0f" % r.index_mb_mean, lambda r: "%.0f" % r.rss_mb_mean, mops],
                  ["chunk", "populate s", "cgo calls (populate)", "chunks", "arena reserved MiB", "slab in use MiB",
                   "index MiB", "RSS MiB", "Mops/s (5 s)"]))
    out.append("")

# ------------------------------------------------------------------ E4
e = S[S.experiment == "E4"].copy()
if not e.empty:
    e["_o"] = e.impl.map({k: i for i, k in enumerate(IMPLS)})
    e = e.sort_values(["value_size", "_o"])
    out.append("### E4 - value size, N = 2M\n")
    out.append(md(e, ["impl", "value_size", "runs", mops, us("p99_ns"), us("p999_ns"), lambda r: f2(r.gc_cpu_percent_mean),
                      lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean,
                      lambda r: "%.0f" % r.peak_rss_mb_mean, lambda r: "%.0f" % r.private_mb_mean,
                      lambda r: "%.0f" % r.offheap_mb_mean, lambda r: "%.1f %%" % (100 * r.fragmentation_mean), lambda r: f2(r.populate_s_mean)],
                  ["impl", "value B", "runs", "Mops/s", "p99 µs", "p99.9 µs", "GC CPU %", "Go heap MiB", "RSS MiB",
                   "peak RSS MiB", "private MiB", "off-heap MiB", "fragmentation", "populate s"]))
    out.append("")

# ------------------------------------------------------------------ E5
e = S[S.experiment == "E5"].copy()
if not e.empty:
    out.append("### E5 - goroutine scaling, N = 2M (Mops/s; p99 µs in parentheses)\n")
    gs = sorted(e.goroutines.unique())
    lines = ["| impl | " + " | ".join("G = %d" % g for g in gs) + " | speed-up 1->16 |", "|---|" + "---|" * (len(gs) + 1)]
    for impl in [i for i in IMPLS if i in set(e.impl)]:
        d = e[e.impl == impl].set_index("goroutines")
        cells = ["%.2f (%.2f)" % (d.loc[g].ops_per_sec_mean / 1e6, d.loc[g].p99_ns_mean / 1e3) for g in gs]
        lines.append("| %s | %s | %.1fx |" % (impl, " | ".join(cells), d.loc[gs[-1]].ops_per_sec_mean / d.loc[gs[0]].ops_per_sec_mean))
    out.append("\n".join(lines))
    out.append("")

# ------------------------------------------------------------------ E6
e = S[S.experiment == "E6"].copy()
if not e.empty:
    e["_o"] = e.impl.map({k: i for i, k in enumerate(IMPLS)})
    e = e.sort_values(["dist", "read_pct", "_o"])
    out.append("### E6 - skewed and write-heavy workloads, N = 5M\n")
    out.append(md(e, ["impl", "dist", lambda r: "%d %%" % r.read_pct, mops, us("p99_ns"), us("p999_ns"), ms("max_ns"),
                      lambda r: f2(r.gc_cpu_percent_mean), lambda r: "%.0f" % r.gc_cycles_mean,
                      lambda r: "%.0f" % r.alloc_mb_per_sec_mean, lambda r: "%.0f" % r.rss_mb_mean],
                  ["impl", "keys", "reads", "Mops/s", "p99 µs", "p99.9 µs", "max ms", "GC CPU %", "GC cycles",
                   "Go alloc MiB/s", "RSS MiB"]))
    out.append("")

# ------------------------------------------------------------------ E3
mb = os.path.join(R, "microbench.csv")
if os.path.exists(mb):
    m = pd.read_csv(mb)
    out.append("### E3 - micro-benchmarks (go test -bench, -count 5, mean ± std)\n")
    out.append(md(m, [lambda r: r.package.replace("github.com/savokh/goarena/", ""), "benchmark",
                      lambda r: "%.2f ± %.2f" % (r.ns_mean, r.ns_std), lambda r: "%.2f - %.2f" % (r.ns_min, r.ns_max),
                      lambda r: "" if pd.isna(r.bytes_per_op) else "%.0f" % r.bytes_per_op,
                      lambda r: "" if pd.isna(r.allocs_per_op) else "%.0f" % r.allocs_per_op],
                  ["package", "benchmark", "ns/op", "min - max", "B/op", "allocs/op"]))
    out.append("")

# ------------------------------------------------------------------ E8
e = order(S[S.experiment == "E8"])
if not e.empty:
    out.append("### E8 - large scale (2 runs each; same metrics as E1 + one forced full GC)\n")
    out.append(md(e, ["impl", lambda r: "%d M" % (r.n / 1e6), "runs", mops, us("p50_ns"), us("p99_ns"), us("p999_ns"),
                      ms("max_ns"), lambda r: f1(r.gc_cycles_mean), lambda r: f2(r.gc_cpu_percent_mean),
                      lambda r: "%.1f" % r.forced_gc_ms_mean, lambda r: "%.0f" % r.mark_ms_mean_mean if r.mark_ms_mean_mean == r.mark_ms_mean_mean else "-",
                      lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean,
                      lambda r: "%.0f" % r.private_mb_mean, lambda r: f2(r.populate_s_mean)],
                  ["impl", "N", "runs", "Mops/s", "p50 µs", "p99 µs", "p99.9 µs", "max ms", "GC cycles", "GC CPU %",
                   "full GC ms", "mean mark ms", "Go heap MiB", "RSS MiB", "private MiB", "populate s"]))
    out.append("")
    import glob
    sk = sorted(glob.glob(os.path.join(R, "raw", "E8_*.skipped.txt")))
    if sk:
        out.append("E8 runs not executed (memory guard):\n")
        seen = set()
        lines = ["| impl | N | reason |", "|---|---|---|"]
        for f in sk:
            parts = os.path.basename(f).split("_")
            k = (parts[1], parts[2])
            if k in seen:
                continue
            seen.add(k)
            lines.append("| %s | %d M | %s |" % (parts[1], int(parts[2][1:]) / 1e6, open(f, encoding="utf-8").read().strip()))
        out.append("\n".join(lines))
        out.append("")

# ------------------------------------------------------------- E9, E10
for exp, title in (("E9", "### E9 - service with request garbage (1 KiB request object per operation), 3 runs\n"),
                   ("E10", "### E10 - container memory budget: E9 workload, N = 5M, GOMEMLIMIT = 1.25 x gobytes RSS, 3 runs\n")):
    e = order(S[S.experiment == exp])
    if e.empty:
        continue
    out.append(title)
    if exp == "E10":
        out.append("Memory limit applied to every implementation: %d MiB.\n" % e.memlimit_mib_mean.iloc[0])
    out.append(md(e, ["impl", lambda r: "%d M" % (r.n / 1e6), mops, us("p50_ns"), us("p99_ns"), us("p999_ns"),
                      ms("max_ns"), lambda r: "%.1f" % r.gc_per_sec_mean,
                      lambda r: "%.2f ± %.2f" % (r.gc_cpu_percent_mean, r.gc_cpu_percent_std),
                      lambda r: "%.1f" % r.stw_per_sec_ms_mean, lambda r: "%.0f" % r.alloc_mb_per_sec_mean,
                      lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean,
                      lambda r: "%.0f" % r.peak_rss_mb_mean],
                  ["impl", "N", "Mops/s", "p50 µs", "p99 µs", "p99.9 µs", "max ms", "GC cycles/s", "GC CPU %",
                   "STW ms/s", "Go alloc MiB/s", "Go heap MiB", "RSS MiB", "peak RSS MiB"]))
    out.append("")

# --------------------------------------------------------------- v1 -> v2
v1p = os.path.join(R, "v1", "summary.csv")
if os.path.exists(v1p):
    V1 = pd.read_csv(v1p)
    out.append("### goarena v0.1.0 -> v0.2.0 (same experiments, same parameters; baselines unchanged)\n")
    rows = []

    def pick(F, exp, impl, **kw):
        d = F[(F.experiment == exp) & (F.impl == impl)]
        for k, v in kw.items():
            d = d[d[k] == v]
        return None if d.empty else d.iloc[0]

    cases = [("E1 N=1M", "E1", dict(n=1000000)), ("E1 N=5M", "E1", dict(n=5000000)), ("E1 N=10M", "E1", dict(n=10000000)),
             ("E6 Zipf 5M", "E6", dict(dist="zipf")), ("E6 50% writes 5M", "E6", dict(dist="uniform", read_pct=50)),
             ("E5 G=1 2M", "E5", dict(goroutines=1)), ("E4 1 KiB 2M", "E4", dict(value_size=1024)),
             ("E4 4 KiB 2M", "E4", dict(value_size=4096))]
    lines = ["| case | metric | goarena v0.1.0 | goarena v0.2.0 | change | best baseline (v0.2.0 session) |", "|---|---|---|---|---|---|"]
    for name, exp, kw in cases:
        a, b = pick(V1, exp, "goarena", **kw), pick(S, exp, "goarena", **kw)
        if a is None or b is None:
            continue
        for metric, col, sc, unit, better in (("throughput", "ops_per_sec", 1e-6, "Mops/s", max),
                                              ("p99", "p99_ns", 1e-3, "µs", min), ("p99.9", "p999_ns", 1e-3, "µs", min),
                                              ("RSS", "rss_mb", 1, "MiB", min)):
            va, vb = a[col + "_mean"] * sc, b[col + "_mean"] * sc
            base = []
            for impl in ["gomap", "gomap-gogc400", "syncmap", "gobytes"]:
                r = pick(S, exp, impl, **kw)
                if r is not None:
                    base.append((r[col + "_mean"] * sc, impl))
            bb = better(base) if base else (float("nan"), "-")
            lines.append("| %s | %s, %s | %.2f | %.2f | %+.1f %% | %.2f (%s) |" % (name, metric, unit, va, vb, 100 * (vb / va - 1), bb[0], bb[1]))
    out.append("\n".join(lines))
    out.append("")

path = os.path.join(R, "tables.md")
with open(path, "w", encoding="utf-8") as f:
    f.write("\n".join(out) + "\n")
print("wrote", path)
