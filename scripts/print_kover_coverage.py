#!/usr/bin/env python3
"""Print total line coverage from a Kover/JaCoCo-style XML report.

Informational only (see DESIGN.md section 13: Android coverage is reported
as a CI artifact but deliberately not gated, unlike go/'s enforced floor).
Usage: print_kover_coverage.py <path/to/reportDebug.xml>
"""
import os
import sys
import xml.etree.ElementTree as ET


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <report.xml>", file=sys.stderr)
        return 1

    xml_path = sys.argv[1]
    if not os.path.isfile(xml_path):
        print(f"No Kover XML report found at {xml_path} "
              "(tests likely failed before reporting).")
        return 0

    root = ET.parse(xml_path).getroot()
    # Report-level totals are the <counter> elements that are direct
    # children of <report>, as opposed to the many nested per-package/
    # per-class/per-method ones.
    totals = {c.get("type"): c for c in root.findall("counter")}

    line = totals.get("LINE")
    if line is None:
        print("No report-level LINE counter found in Kover XML report.")
        return 0

    covered = int(line.get("covered"))
    missed = int(line.get("missed"))
    total = covered + missed
    pct = (covered / total * 100) if total else 100.0
    message = f"Android line coverage: {pct:.1f}% ({covered}/{total} lines)"
    print(message)

    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary_path:
        with open(summary_path, "a") as f:
            f.write(f"### {message}\n")

    return 0


if __name__ == "__main__":
    sys.exit(main())
