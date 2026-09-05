#!/usr/bin/env python3
"""Regression tests for bench-compare.py (the CI bench gate).

The gate cannot detect its own parser regressions, so checked-in benchstat
samples lock the parsing contract: significant time slowdowns fail, noise
(~) and sub-threshold deltas pass, ANY allocs increase (including 0 -> N
rendered as +Inf%) fails, B/op only warns, and malformed input exits 2.

Run: python3 scripts/bench-compare_test.py
"""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("bench-compare.py")


def run_gate(content: str, *args: str) -> subprocess.CompletedProcess:
    with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as f:
        f.write(content)
        path = f.name
    try:
        return subprocess.run(
            [sys.executable, str(SCRIPT), path, *args],
            capture_output=True,
            text=True,
        )
    finally:
        Path(path).unlink()


TIME_TABLE = """\
name                  old time/op    new time/op    delta
ComputeLineDiff-8     1.00µs ± 2%    {new}           {delta}
"""

ALLOC_TABLE = """\
name                  old allocs/op  new allocs/op  delta
VisibleInFocus-8      {old}          {new}           {delta}
"""


class GateTest(unittest.TestCase):
    def test_significant_time_regression_fails(self):
        r = run_gate(TIME_TABLE.format(new="1.26µs ± 2%", delta="+26.00%  (p=0.008 n=6+6)"))
        self.assertEqual(r.returncode, 1)
        self.assertIn("time/op regression", r.stdout)

    def test_subthreshold_time_passes(self):
        r = run_gate(TIME_TABLE.format(new="1.05µs ± 2%", delta="+5.00%  (p=0.010 n=6+6)"))
        self.assertEqual(r.returncode, 0)

    def test_insignificant_row_passes(self):
        r = run_gate(TIME_TABLE.format(new="1.30µs ±20%", delta="~     (p=0.300 n=6+6)"))
        self.assertEqual(r.returncode, 0)

    def test_alloc_increase_fails(self):
        r = run_gate(ALLOC_TABLE.format(old="100 ± 0%", new="130 ± 0%", delta="+30.00%  (p=0.008 n=6+6)"))
        self.assertEqual(r.returncode, 1)
        self.assertIn("allocs/op regression", r.stdout)

    def test_alloc_zero_to_n_fails(self):
        # benchstat renders 0 -> N as +Inf% — the heap-escape case that must fail.
        r = run_gate(ALLOC_TABLE.format(old="0.00 ± 0%", new="2.00 ± 0%", delta="+Inf%  (p=0.008 n=6+6)"))
        self.assertEqual(r.returncode, 1)
        self.assertIn("allocs/op regression", r.stdout)

    def test_alloc_stable_passes(self):
        r = run_gate(ALLOC_TABLE.format(old="100 ± 0%", new="100 ± 0%", delta="~     (all equal)"))
        self.assertEqual(r.returncode, 0)

    def test_bop_only_warns(self):
        content = (
            TIME_TABLE.format(new="1.01µs ± 2%", delta="~     (p=0.300 n=6+6)")
            + "\nname                  old B/op       new B/op      delta\n"
            + "VisibleInFocus-8        0.00 ± 0%     16.00 ± 0%   +Inf%  (p=0.008 n=6+6)\n"
        )
        r = run_gate(content)
        self.assertEqual(r.returncode, 0)
        self.assertIn("B/op increase", r.stdout)

    def test_new_benchstat_format_time_regression_fails(self):
        # Newer benchstat versions omit the "old/new" prefix and "name" row.
        content = (
            "                               │ bench-old.txt │           bench-new.txt           │\n"
            "                               │    sec/op     │   sec/op     vs base              │\n"
            "ComputeLineDiff/100_lines-4      102.25µ ± 1%   130.00µ ± 1%  +27.13% (p=0.002 n=6)\n"
        )
        r = run_gate(content)
        self.assertEqual(r.returncode, 1)
        self.assertIn("time/op regression", r.stdout)

    def test_new_benchstat_format_alloc_regression_fails(self):
        content = (
            "                               │   allocs/op   │  allocs/op   vs base                │\n"
            "ComputeLineDiff/100_lines-4        597.0 ± 0%    600.0 ± 0%   +0.50%  (p=0.002 n=6)\n"
        )
        r = run_gate(content)
        self.assertEqual(r.returncode, 1)
        self.assertIn("allocs/op regression", r.stdout)

    def test_geomean_alloc_noise_is_ignored(self):
        # Real CI failure mode: every named bench is "~" but geomean still
        # prints a tiny +% from rounding across packages. Must not fail the gate.
        content = (
            "                         │   allocs/op   │  allocs/op   vs base                │\n"
            "VisibleInFocus/n=10-4        2.000 ± 0%    2.000 ± 0%       ~ (p=1.000 n=6) ¹\n"
            "ReviewSaveLoad/10x10-4       676.0 ± 0%    677.0 ± 0%       ~ (p=0.058 n=6)\n"
            "geomean                      70.45         70.46       +0.02%\n"
        )
        r = run_gate(content)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("bench check passed", r.stdout)

    def test_geomean_time_noise_is_ignored(self):
        content = (
            "                         │    sec/op     │    sec/op      vs base                │\n"
            "VisibleInFocus/n=10-4      200.0n ± 1%    200.0n ± 1%         ~ (p=1.000 n=6)\n"
            "geomean                    157.3µ          192.2µ         +22.16%\n"
        )
        r = run_gate(content)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("bench check passed", r.stdout)

    def test_missing_file_exits_2(self):
        r = subprocess.run(
            [sys.executable, str(SCRIPT), "/nonexistent/benchstat.txt"],
            capture_output=True,
            text=True,
        )
        self.assertEqual(r.returncode, 2)

    def test_empty_input_exits_2(self):
        r = run_gate("")
        self.assertEqual(r.returncode, 2)

    def test_no_tables_exits_2(self):
        r = run_gate("some log output without any benchstat tables\n")
        self.assertEqual(r.returncode, 2)


if __name__ == "__main__":
    unittest.main()
