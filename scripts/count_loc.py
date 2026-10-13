#!/usr/bin/env python3
"""Count lines of Go code per package (non-test / test, total and non-blank
non-comment) and write results/loc.csv.

usage: python scripts/count_loc.py [root] [out.csv]
"""
import csv
import os
import sys


def count(path):
    total = code = 0
    in_block = False
    with open(path, encoding="utf-8") as f:
        for line in f:
            total += 1
            s = line.strip()
            if in_block:
                if "*/" in s:
                    in_block = False
                continue
            if not s or s.startswith("//"):
                continue
            if s.startswith("/*"):
                in_block = "*/" not in s
                continue
            code += 1
    return total, code


def main(root=".", out="results/loc.csv"):
    pkgs = {}
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d not in (".git", "bin", "results", "testdata")]
        for f in fn:
            if not f.endswith((".go", ".s")):
                continue
            rel = os.path.relpath(dp, root).replace("\\", "/")
            p = pkgs.setdefault(rel, {"files": 0, "src_lines": 0, "src_code": 0, "test_lines": 0, "test_code": 0})
            t, c = count(os.path.join(dp, f))
            p["files"] += 1
            if f.endswith("_test.go"):
                p["test_lines"] += t
                p["test_code"] += c
            else:
                p["src_lines"] += t
                p["src_code"] += c
    with open(out, "w", newline="", encoding="utf-8") as fh:
        w = csv.writer(fh)
        w.writerow(["package", "files", "src_lines", "src_code_lines", "test_lines", "test_code_lines"])
        tot = [0, 0, 0, 0, 0]
        for k in sorted(pkgs):
            p = pkgs[k]
            row = [p["files"], p["src_lines"], p["src_code"], p["test_lines"], p["test_code"]]
            tot = [a + b for a, b in zip(tot, row)]
            w.writerow([k] + row)
            print("%-22s %3d files  src %5d (%5d code)  tests %5d (%5d code)" % (k, *row))
        w.writerow(["TOTAL"] + tot)
        print("%-22s %3d files  src %5d (%5d code)  tests %5d (%5d code)" % ("TOTAL", *tot))


if __name__ == "__main__":
    main(*sys.argv[1:])
