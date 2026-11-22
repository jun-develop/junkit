"""Check every *_test.go Test Case Overview against the tests that actually ran.

usage: python3 check_test_overview.py <go-test-json-file>

Run from the repository root after `go test -json ./... > <file>`. Exits 1 when an overview
disagrees with the code or a test failed. See TESTING.md for the overview format.
"""
import collections
import json
import os
import re
import sys

MODULE = "github.com/jun-develop/junkit"

runs = collections.defaultdict(set)
skipped = collections.defaultdict(set)
output = collections.defaultdict(list)
failed = []
for line in open(sys.argv[1]):
    try:
        e = json.loads(line)
    except ValueError:
        print(line, end="")
        continue
    pkg = e.get("Package", "").removeprefix(MODULE).lstrip("/")
    test = e.get("Test")
    if e.get("Action") == "output":
        output[(pkg, test)].append(e["Output"])
    if e.get("Action") == "fail" and not test:
        failed.append((pkg, None))
    if not test or e.get("Action") not in ("pass", "fail", "skip"):
        continue
    runs[pkg].add(test)
    if e["Action"] == "skip":
        skipped[pkg].add(test)
    if e["Action"] == "fail":
        failed.append((pkg, test))

# Leaf runs per top-level test: subtests that only group other subtests are not scenarios.
leaves = collections.defaultdict(collections.Counter)
for pkg, names in runs.items():
    for n in names:
        if not any(o.startswith(n + "/") for o in names):
            leaves[pkg][n.split("/")[0]] += 1

problems = []
for root, dirs, files in os.walk("."):
    dirs[:] = [d for d in dirs if not d.startswith(".")]
    pkg = os.path.relpath(root, ".").replace(os.sep, "/").removeprefix(".")
    for name in sorted(f for f in files if f.endswith("_test.go")):
        path = os.path.join(root, name)[2:]
        src = open(os.path.join(root, name)).read()
        head = src.split("\nimport", 1)[0]
        funcs = re.findall(r"^func (Test\w+)\(t \*testing\.T\)", src, re.M)

        def bad(msg):
            problems.append(f"{path}: {msg}")

        if not funcs:
            if "TEST HELPERS" not in head:
                bad("helper-only file without a TEST HELPERS block")
            continue
        if "TEST CASE OVERVIEW" not in head:
            bad("missing TEST CASE OVERVIEW")
            continue

        m = re.search(r"^// File: (\S+)", head, re.M)
        if not m or m.group(1) != name:
            bad(f"'File:' line says {m and m.group(1)!r}")
        for key in ("Purpose", "Type"):
            if not re.search(rf"^// {key}: \S", head, re.M):
                bad(f"missing '{key}:' line")

        rows = re.findall(r"^//\s+(\d+)\s+(Test\w+)\s{2,}\S.*?\s{2,}\S", head, re.M)
        tf = re.search(r"^// Test functions: (\d+)", head, re.M)
        ts = re.search(r"^// Total scenarios: (\d+)", head, re.M)
        if not tf or int(tf.group(1)) != len(funcs):
            bad(f"'Test functions: {tf and tf.group(1)}' but the file has {len(funcs)}")
        if not ts or int(ts.group(1)) != len(rows):
            bad(f"'Total scenarios: {ts and ts.group(1)}' but the table has {len(rows)} rows")
        if [int(n) for n, _ in rows] != list(range(1, len(rows) + 1)):
            bad("rows are not numbered 1..n")

        per_test = collections.Counter(t for _, t in rows)
        for fn in set(per_test) - set(funcs):
            bad(f"row for unknown test {fn}")
        order = [t for _, t in rows if t in funcs]
        if order != sorted(order, key=funcs.index):
            bad("rows are not in source order")
        for fn in funcs:
            ran = leaves[pkg].get(fn)
            if ran is None:
                bad(f"{fn} did not run")
            elif per_test[fn] != ran and fn not in skipped[pkg]:
                bad(f"{fn}: {per_test[fn]} rows but {ran} scenarios ran")

        for kind, prefix in (("Fuzz tests", "Fuzz"), ("Benchmarks", "Benchmark")):
            n = len(re.findall(rf"^func ({prefix}\w+)\(", src, re.M))
            m = re.search(rf"^// {kind}: (\d+)", head, re.M)
            if n and (not m or int(m.group(1)) != n):
                bad(f"'{kind}:' line missing or not {n}")

for pkg, test in failed:
    print(f"--- FAILED {pkg or '.'} {test or '(package)'}")
    print("".join(output[(pkg, test)]), end="")
for p in problems:
    print(p)
print(f"{len(problems)} overview problems, {len(failed)} failing tests")
sys.exit(1 if problems or failed or not runs else 0)
