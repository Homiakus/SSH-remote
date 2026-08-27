#!/usr/bin/env python3
"""Compare a full Gremlins report with the committed module/package mutation baseline."""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

VALID = {"KILLED", "LIVED", "NOT COVERED", "TIMED OUT", "NOT VIABLE", "SKIPPED", "RUNNABLE"}


def package_name(module: str, file_name: str) -> str:
    path = Path(file_name)
    parent = path.parent.as_posix()
    if parent in ("", "."):
        return module
    return f"{module}/{parent}"


def scores(counts: Counter[str]) -> dict[str, float | int | None]:
    killed = counts["KILLED"]
    lived = counts["LIVED"]
    uncovered = counts["NOT COVERED"]
    efficacy_den = killed + lived
    coverage_den = killed + lived + uncovered
    return {
        "mutants_total_observed": sum(counts.values()),
        "mutants_killed": killed,
        "mutants_lived": lived,
        "mutants_not_covered": uncovered,
        "mutants_timed_out": counts["TIMED OUT"],
        "mutants_not_viable": counts["NOT VIABLE"],
        "test_efficacy_pct": round(100.0 * killed / efficacy_den, 2) if efficacy_den else None,
        "mutant_coverage_pct": round(100.0 * (killed + lived) / coverage_den, 2) if coverage_den else None,
    }


def aggregate(report: dict) -> dict[str, dict]:
    module = report["go_module"]
    grouped: dict[str, Counter[str]] = defaultdict(Counter)
    for file_info in report.get("files", []):
        pkg = package_name(module, file_info["file_name"])
        for mutation in file_info.get("mutations", []):
            status = mutation["status"].upper().replace("_", " ")
            if status not in VALID:
                raise ValueError(f"unknown Gremlins status {status!r}")
            grouped[pkg][status] += 1
    return {pkg: scores(counts) for pkg, counts in grouped.items()}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()

    baseline = json.loads(args.baseline.read_text(encoding="utf-8"))
    report = json.loads(args.report.read_text(encoding="utf-8"))
    failures: list[str] = []
    eps = 0.01

    module_floor = baseline["module"].get("mutation")
    if module_floor:
        for key, report_key in (
            ("test_efficacy_pct", "test_efficacy"),
            ("mutant_coverage_pct", "mutations_coverage"),
        ):
            floor = module_floor.get(key)
            actual = report.get(report_key)
            if floor is not None and actual is not None and float(actual) + eps < float(floor):
                failures.append(f"module {key}: {actual:.2f} < {floor:.2f}")

    actual_packages = aggregate(report)
    for package, metrics in baseline["packages"].items():
        floor = metrics.get("mutation")
        if not floor:
            continue
        actual = actual_packages.get(package)
        if actual is None:
            failures.append(f"{package}: mutation data disappeared")
            continue
        for key in ("test_efficacy_pct", "mutant_coverage_pct"):
            expected = floor.get(key)
            observed = actual.get(key)
            if expected is None:
                continue
            if observed is None or float(observed) + eps < float(expected):
                failures.append(f"{package} {key}: {observed} < {expected}")

    if failures:
        print("Mutation ratchet failed:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(
        "Mutation ratchet passed: "
        f"module efficacy={report.get('test_efficacy')}%, "
        f"mutant coverage={report.get('mutations_coverage')}%"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
