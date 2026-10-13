#!/usr/bin/env python3
"""Parse `go test -bench` output (several -count runs) into a CSV with the
mean, standard deviation, minimum and maximum of ns/op, B/op and allocs/op
per benchmark.

usage: python scripts/parse_microbench.py results/logs/microbench.txt results/microbench.csv
"""
import csv
import re
import statistics
import sys

LINE = re.compile(r"^(Benchmark\S+?)(?:-(\d+))?\s+(\d+)\s+([\d.]+) ns/op(?:\s+([\d.]+) B/op)?(?:\s+([\d.]+) allocs/op)?")


def main(src, dst):
    pkg = ""
    rows = {}
    order = []
    with open(src, encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if line.startswith("pkg:"):
                pkg = line.split()[1]
                continue
            m = LINE.match(line)
            if not m:
                continue
            name = m.group(1)
            key = (pkg, name)
            if key not in rows:
                rows[key] = {"procs": m.group(2) or "1", "ns": [], "b": [], "allocs": []}
                order.append(key)
            r = rows[key]
            r["ns"].append(float(m.group(4)))
            if m.group(5) is not None:
                r["b"].append(float(m.group(5)))
            if m.group(6) is not None:
                r["allocs"].append(float(m.group(6)))
    with open(dst, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["package", "benchmark", "procs", "runs", "ns_mean", "ns_std", "ns_min", "ns_max", "bytes_per_op", "allocs_per_op"])
        for key in order:
            r = rows[key]
            ns = r["ns"]
            std = statistics.stdev(ns) if len(ns) > 1 else 0.0
            w.writerow([key[0], key[1], r["procs"], len(ns), "%.3f" % statistics.mean(ns), "%.3f" % std,
                        "%.3f" % min(ns), "%.3f" % max(ns),
                        "%.1f" % statistics.mean(r["b"]) if r["b"] else "",
                        "%.2f" % statistics.mean(r["allocs"]) if r["allocs"] else ""])
    print("wrote %s (%d benchmarks)" % (dst, len(order)))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
