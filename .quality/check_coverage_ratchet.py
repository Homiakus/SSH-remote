#!/usr/bin/env python3
"""Fail when statement coverage drops below the committed quality baseline."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

OK_RE = re.compile(
    r"^ok\s+(?P<pkg>\S+)\s+(?P<duration>[0-9.]+)s\s+coverage:\s+(?P<coverage>[0-9.]+)% of statements$"
)
COVERAGE_RE = re.compile(
    r"^(?P<pkg>\S+)\s+coverage:\s+(?P<coverage>[0-9.]+)% of statements$"
)
TOTAL_RE = re.compile(r"^total:\s+\(statements\)\s+(?P<coverage>[0-9.]+)%$")


def read_current_packages(path: Path) -> dict[str, float]:
    current: dict[str, float] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        match = OK_RE.match(line) or COVERAGE_RE.match(line)
        if match:
            current[match.group("pkg")] = float(match.group("coverage"))
    return current


def read_total(path: Path) -> float:
    for raw in reversed(path.read_text(encoding="utf-8").splitlines()):
        match = TOTAL_RE.match(raw.strip())
        if match:
            return float(match.group("coverage"))
    raise ValueError(f"coverage total not found in {path}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--packages", type=Path, required=True)
    parser.add_argument("--functions", type=Path, required=True)
    args = parser.parse_args()

    baseline = json.loads(args.baseline.read_text(encoding="utf-8"))
    tolerance = float(baseline["policy"].get("coverage_rounding_tolerance_pct", 0.05))
    current = read_current_packages(args.packages)
    failures: list[str] = []

    for package, metrics in baseline["packages"].items():
        floor = metrics.get("statement_coverage_pct")
        if floor is None:
            continue
        actual = current.get(package)
        if actual is None:
            failures.append(f"{package}: package/coverage result disappeared (floor {floor:.1f}%)")
            continue
        if actual + tolerance < float(floor):
            failures.append(f"{package}: coverage regressed {actual:.1f}% < {float(floor):.1f}%")

    total_floor = float(baseline["module"]["statement_coverage_pct"])
    total_actual = read_total(args.functions)
    if total_actual + tolerance < total_floor:
        failures.append(f"module: coverage regressed {total_actual:.1f}% < {total_floor:.1f}%")

    if failures:
        print("Coverage ratchet failed:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(f"Coverage ratchet passed: module {total_actual:.1f}% >= {total_floor:.1f}%")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
