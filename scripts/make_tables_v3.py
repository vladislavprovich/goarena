#!/usr/bin/env python3
"""Tables of the v0.3.0 session (experiments re-run in v0.3.0: E1r, E1t,
E4, E9, E10) and the v0.1.0 -> v0.2.0 -> v0.3.0 comparison, generated from
results/summary.csv, results/v2/summary.csv and results/v1/summary.csv.

usage: python scripts/make_tables_v3.py [results-dir]  -> results/tables_v3.md
"""
import os
import sys

import pandas as pd

R = sys.argv[1] if len(sys.argv) > 1 else "results"
S3 = pd.read_csv(os.path.join(R, "summary.csv"))
S2 = pd.read_csv(os.path.join(R, "v2", "summary.csv"))
S1 = pd.read_csv(os.path.join(R, "v1", "summary.csv"))
IMPLS = ["gomap", "gomap-gogc400", "syncmap", "gobytes", "goarena", "goarena-gogc400",
         "goarena-budget", "goarena-paced"]
BASE = ["gomap", "gomap-gogc400", "syncmap", "gobytes"]
out = []


def md(df, cols, headers):
    lines = ["| " + " | ".join(headers) + " |", "|" + "---|" * len(headers)]
    for _, r in df.iterrows():
        lines.append("| " + " | ".join(str(f(r)) if callable(f) else str(r[f]) for f in cols) + " |")
    return "\n".join(lines)


def order(df, *keys):
    df = df.copy()
    df["_o"] = df.impl.map({k: i for i, k in enumerate(IMPLS)})
    return df.sort_values(list(keys) + ["_o"])


def mops(r):
    if r["runs"] > 1:
        return "%.2f ± %.2f" % (r.ops_per_sec_mean / 1e6, r.ops_per_sec_std / 1e6)
    return "%.2f" % (r.ops_per_sec_mean / 1e6)


def us(col):
    return lambda r: "%.2f" % (r[col + "_mean"] / 1e3)


def ms(col):
    return lambda r: "%.2f" % (r[col + "_mean"] / 1e6)


def pick(S, exp, impl, **kw):
    d = S[(S.experiment == exp) & (S.impl == impl)]
    for k, v in kw.items():
        d = d[d[k] == v]
    return None if d.empty else d.iloc[0]


# ----------------------------------------------------------------- E1r
e = order(S3[S3.experiment == "E1r"])
if not e.empty:
    out.append("### E1r - E1 at N = 10M re-run with v0.3.0 (3 runs, all implementations)\n")
    out.append(md(e, ["impl", "runs", mops, us("p50_ns"), us("p99_ns"), us("p999_ns"), ms("max_ns"),
                      lambda r: "%.1f" % r.over_1ms_per_million_mean, lambda r: "%.2f" % r.gc_cpu_percent_mean,
                      lambda r: "%.0f" % r.heap_objects_mb_mean, lambda r: "%.0f" % r.rss_mb_mean,
                      lambda r: "%.0f" % r.offheap_mb_mean, lambda r: "%.2f" % r.populate_s_mean],
                  ["impl", "runs", "Mops/s", "p50 µs", "p99 µs", "p99.9 µs", "max ms", "ops > 1 ms per million",
                   "GC CPU %", "Go heap MiB", "RSS MiB", "off-heap MiB", "populate s"]))
    out.append("")

# ----------------------------------------------------------------- E1t
e = order(S3[S3.experiment.isin(["E1t", "E1r"])], "goroutines")
if not S3[S3.experiment == "E1t"].empty:
    out.append("### E1t - tail vs CPU oversubscription: N = 10M, GOMAXPROCS = 16, 14 / 15 / 16 workers "
               "(16 = E1r; 2 runs for 14/15, 3 for 16)\n")
    out.append(md(e, ["impl", "goroutines", "runs", mops, us("p99_ns"), us("p999_ns"), ms("max_ns"),
                      lambda r: "%.2f - %.2f" % (r.max_ns_min / 1e6, r.max_ns_max / 1e6),
                      lambda r: "%.2f" % r.over_1ms_per_million_mean, lambda r: "%.1f" % r.process_cpu_s_mean],
                  ["impl", "G", "runs", "Mops/s", "p99 µs", "p99.9 µs", "max ms (mean)", "max ms (min - max)",
                   "ops > 1 ms per million", "process CPU s (15 s)"]))
    out.append("")

# ------------------------------------------------------------------ E4
e = order(S3[S3.experiment == "E4"], "value_size")
if not e.empty:
    out.append("### E4 - value size, N = 2M, v0.3.0 size classes (1 run)\n")
    out.append(md(e, ["impl", "value_size", mops, us("p99_ns"), us("p999_ns"),
                      lambda r: "%.0f" % r.rss_mb_mean, lambda r: "%.0f" % r.private_mb_mean,
                      lambda r: "%.0f" % r.offheap_mb_mean,
                      lambda r: "%.1f %%" % (100 * r.fragmentation_mean) if r.impl.startswith("goarena") else "-",
                      lambda r: "%.2f" % r.populate_s_mean],
                  ["impl", "value B", "Mops/s", "p99 µs", "p99.9 µs", "RSS MiB", "private MiB", "off-heap MiB",
                   "internal fragmentation", "populate s"]))
    out.append("")
    lines = ["| value B | goarena v0.1.0 RSS | v0.2.0 | v0.3.0 | gobytes (v0.3.0 session) | v0.3.0 vs gobytes |",
             "|---|---|---|---|---|---|"]
    for v in sorted(e.value_size.unique()):
        a, b, c, g = (pick(S1, "E4", "goarena", value_size=v), pick(S2, "E4", "goarena", value_size=v),
                      pick(S3, "E4", "goarena", value_size=v), pick(S3, "E4", "gobytes", value_size=v))
        lines.append("| %d | %.0f MiB (%.1f %%) | %.0f MiB (%.1f %%) | %.0f MiB (%.1f %%) | %.0f MiB | %+.1f %% |" % (
            v, a.rss_mb_mean, 100 * a.fragmentation_mean, b.rss_mb_mean, 100 * b.fragmentation_mean,
            c.rss_mb_mean, 100 * c.fragmentation_mean, g.rss_mb_mean, 100 * (c.rss_mb_mean / g.rss_mb_mean - 1)))
    out.append("E4 memory across versions (RSS, internal fragmentation in parentheses):\n")
    out.append("\n".join(lines))
    out.append("")

# ------------------------------------------------------------ E9, E10
for exp, title in (("E9", "### E9 - service with request garbage (1 KiB request object per operation), 3 runs\n"),
                   ("E10", "### E10 - container memory budget: E9 workload, N = 5M, GOMEMLIMIT = 1210 MiB, 3 runs\n")):
    e = order(S3[S3.experiment == exp], "n")
    if e.empty:
        continue
    out.append(title)
    out.append(md(e, ["impl", lambda r: "%d M" % (r.n / 1e6), mops, us("p50_ns"), us("p99_ns"), us("p999_ns"),
                      ms("max_ns"), lambda r: "%.1f" % r.gc_per_sec_mean,
                      lambda r: "%.2f ± %.2f" % (r.gc_cpu_percent_mean, r.gc_cpu_percent_std),
                      lambda r: "%.1f" % r.stw_per_sec_ms_mean, lambda r: "%.0f" % r.heap_objects_mb_mean,
                      lambda r: "%.0f" % r.rss_mb_mean, lambda r: "%.0f" % r.peak_rss_mb_mean,
                      lambda r: "-" if r.pacer_gc_percent_mean != r.pacer_gc_percent_mean else "%.0f" % r.pacer_gc_percent_mean,
                      lambda r: "-" if r.pacer_go_limit_mib_mean != r.pacer_go_limit_mib_mean else "%.0f" % r.pacer_go_limit_mib_mean],
                  ["impl", "N", "Mops/s", "p50 µs", "p99 µs", "p99.9 µs", "max ms", "GC cycles/s", "GC CPU %",
                   "STW ms/s", "Go heap MiB", "RSS MiB", "peak RSS MiB", "pacer GOGC (last)", "pacer Go limit MiB"]))
    out.append("")

# ------------------------------------------------- v1 -> v2 -> v3 summary
out.append("### goarena v0.1.0 -> v0.2.0 -> v0.3.0 (default configuration unless stated; baselines unchanged)\n")
lines = ["| case | metric | v0.1.0 | v0.2.0 | v0.3.0 | best baseline (v0.3.0 session) |", "|---|---|---|---|---|---|"]


def best(S, exp, col, low, **kw):
    vals = []
    for impl in BASE:
        r = pick(S, exp, impl, **kw)
        if r is not None:
            vals.append((r[col + "_mean"], impl))
    return (min if low else max)(vals) if vals else (float("nan"), "-")


rows = [
    ("E1 N=10M", "throughput Mops/s", ("E1", dict(n=10000000)), ("E1", dict(n=10000000)), ("E1r", dict(n=10000000)), "ops_per_sec", 1e-6, False, "goarena"),
    ("E1 N=10M", "p99.9 µs", ("E1", dict(n=10000000)), ("E1", dict(n=10000000)), ("E1r", dict(n=10000000)), "p999_ns", 1e-3, True, "goarena"),
    ("E1 N=10M", "RSS MiB", ("E1", dict(n=10000000)), ("E1", dict(n=10000000)), ("E1r", dict(n=10000000)), "rss_mb", 1, True, "goarena"),
    ("E4 1 KiB", "RSS MiB", ("E4", dict(value_size=1024)), ("E4", dict(value_size=1024)), ("E4", dict(value_size=1024)), "rss_mb", 1, True, "goarena"),
    ("E4 4 KiB", "RSS MiB", ("E4", dict(value_size=4096)), ("E4", dict(value_size=4096)), ("E4", dict(value_size=4096)), "rss_mb", 1, True, "goarena"),
    ("E9 N=5M", "throughput Mops/s (v0.3.0: paced)", None, ("E9", dict(n=5000000)), ("E9", dict(n=5000000)), "ops_per_sec", 1e-6, False, "goarena-paced"),
    ("E9 N=10M", "throughput Mops/s (v0.3.0: paced)", None, ("E9", dict(n=10000000)), ("E9", dict(n=10000000)), "ops_per_sec", 1e-6, False, "goarena-paced"),
    ("E9 N=5M", "GC cycles/s (v0.3.0: paced)", None, ("E9", dict(n=5000000)), ("E9", dict(n=5000000)), "gc_per_sec", 1, True, "goarena-paced"),
    ("E10 5M, 1210 MiB", "throughput Mops/s (v0.3.0: paced)", None, ("E10", dict(n=5000000)), ("E10", dict(n=5000000)), "ops_per_sec", 1e-6, False, "goarena-paced"),
    ("E10 5M, 1210 MiB", "RSS MiB (v0.3.0: paced)", None, ("E10", dict(n=5000000)), ("E10", dict(n=5000000)), "rss_mb", 1, True, "goarena-paced"),
]
for case, metric, k1, k2, k3, col, sc, low, impl3 in rows:
    a = pick(S1, k1[0], "goarena", **k1[1]) if k1 else None
    b = pick(S2, k2[0], "goarena", **k2[1])
    c = pick(S3, k3[0], impl3, **k3[1])
    bb = best(S3, k3[0], col, low, **k3[1])
    f = lambda r: "-" if r is None else "%.2f" % (r[col + "_mean"] * sc)
    lines.append("| %s | %s | %s | %s | %s | %.2f (%s) |" % (case, metric, f(a), f(b), f(c), bb[0] * sc, bb[1]))
out.append("\n".join(lines))
out.append("")

path = os.path.join(R, "tables_v3.md")
with open(path, "w", encoding="utf-8", newline="\n") as fh:
    fh.write("\n".join(out) + "\n")
print("wrote", path)
