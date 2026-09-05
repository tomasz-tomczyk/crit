#!/usr/bin/env python3
"""Gate CI on Go benchmark regressions.

Reads benchstat output (from `benchstat old.txt new.txt` where both files are
`go test -bench=. -benchmem` results) and fails only on:
  - time/op: statistically significant slowdown >= TIME_THRESHOLD_PCT (default 20).
    benchstat marks insignificant rows with `~`; those are ignored.
    Rows where either side reports variance >= VARIANCE_SKIP_PCT (default 25)
    are ignored — shared CI runners produce ±70–90% noise on I/O-bound
    benches (e.g. ReviewSaveLoad), and benchstat can still mark those
    "significant".
  - allocs/op: ANY significant increase on a real benchmark row. Allocs are
    deterministic (unlike ns/op on shared runners), so 0 -> N allocs/op
    always means a heap escape worth a look. Note benchstat prints 0 -> N
    as `+Inf%`, which counts.

Geomean summary rows are never gated — they amplify one noisy bench into a
package-wide fail (and float rounding can invent +0.02% allocs deltas).

B/op regressions are printed as warnings but never fail (they usually track
allocs, which already gate).

Usage:
    benchstat old.txt new.txt | tee benchstat.txt
    python3 scripts/bench-compare.py benchstat.txt [--time-pct 20]

Exit 0 = no actionable regression, 1 = regression found, 2 = usage/input error.
"""

import re
import sys

TIME_THRESHOLD_PCT = 20.0
# Skip time/op gating when either side's reported variance is this high.
# Matches the ±N% annotations benchstat prints next to each mean.
VARIANCE_SKIP_PCT = 25.0

_VARIANCE_RE = re.compile(r"±\s*(\d+(?:\.\d+)?)%")


def parse_threshold(args: list) -> float:
    if "--time-pct" in args:
        i = args.index("--time-pct")
        if i + 1 >= len(args):
            print("error: --time-pct requires a value", file=sys.stderr)
            sys.exit(2)
        try:
            return float(args[i + 1])
        except ValueError:
            print(f"error: invalid --time-pct value: {args[i + 1]!r}", file=sys.stderr)
            sys.exit(2)
    return TIME_THRESHOLD_PCT


def delta_pct(line: str) -> float | None:
    """Return the delta percent of a benchstat row, or None if no delta.

    Handles benchstat's `+Inf%` for 0 -> N transitions (e.g. 0 -> 2 allocs).
    """
    m = re.search(r"\+(\d+(?:\.\d+)?)%", line)
    if m:
        return float(m.group(1))
    if re.search(r"\+Inf%", line, re.IGNORECASE):
        return float("inf")
    return None


def max_variance_pct(line: str) -> float | None:
    """Return the largest ±N% variance annotation on the row, or None."""
    vals = [float(m.group(1)) for m in _VARIANCE_RE.finditer(line)]
    return max(vals) if vals else None


def main() -> int:
    if len(sys.argv) < 2:
        print(f"usage: {sys.argv[0]} benchstat.txt [--time-pct 20]", file=sys.stderr)
        return 2
    path = sys.argv[1]
    threshold = parse_threshold(sys.argv[1:])
    time_regs: list = []
    alloc_regs: list = []
    mem_warn: list = []
    skipped_noisy: list = []
    table = None  # 'time' | 'alloc' | 'mem' | None
    saw_table = False

    try:
        with open(path) as f:
            lines = f.readlines()
    except OSError as e:
        print(f"error: cannot read {path}: {e}", file=sys.stderr)
        return 2
    if not any(line.strip() for line in lines):
        print(f"error: {path} is empty — no benchstat output to gate on", file=sys.stderr)
        return 2

    for line in lines:
        low = line.lower()
        # Benchstat output format varies by version: older versions emit
        # "old time/op" / "old allocs/op" / "old b/op" headers, newer
        # versions emit just the unit ("sec/op", "allocs/op", "b/op").
        if "time/op" in low or "sec/op" in low or "ns/op" in low:
            table = "time"
            saw_table = True
            continue
        if "allocs/op" in low or "alloc/op" in low:
            table = "alloc"
            saw_table = True
            continue
        if "b/op" in low:
            table = "mem"
            saw_table = True
            continue
        if low.startswith("name ") or not line.strip():
            if low.startswith("name "):
                table = None
            continue
        # Package geomean is a summary, not a benchmark — never gate on it.
        if low.lstrip().startswith("geomean"):
            continue
        if table is None or "~" in line:
            continue
        # benchstat emits a geomean summary row per table. It aggregates noise
        # across unrelated benches (and can flag +0.02% when every individual
        # row is "~"), so it must not gate CI — only named benchmarks do.
        if line.lstrip().lower().startswith("geomean"):
            continue
        pct = delta_pct(line)
        if pct is None:
            continue
        if table == "time" and pct >= threshold:
            var = max_variance_pct(line)
            if var is not None and var >= VARIANCE_SKIP_PCT:
                skipped_noisy.append(line.rstrip())
                continue
            time_regs.append(line.rstrip())
        elif table == "alloc" and pct > 0:
            alloc_regs.append(line.rstrip())
        elif table == "mem" and pct > 0:
            mem_warn.append(line.rstrip())

    if not saw_table:
        print(f"error: {path} contains no benchstat tables — refusing to pass blind", file=sys.stderr)
        return 2

    failed = False
    if time_regs:
        print(f"::error::Significant time/op regression (>= {threshold:g}%):")
        for r in time_regs:
            print(f"::error::{r}")
        failed = True
    if alloc_regs:
        print("::error::allocs/op regression (deterministic — likely heap escape):")
        for r in alloc_regs:
            print(f"::error::{r}")
        failed = True
    for r in skipped_noisy:
        print(
            f"::warning::Ignoring noisy time/op delta "
            f"(variance >= {VARIANCE_SKIP_PCT:g}%): {r}"
        )
    for r in mem_warn:
        print(f"::warning::B/op increase (informational): {r}")
    if not failed:
        print("bench check passed: no significant time/op or allocs/op regressions")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
