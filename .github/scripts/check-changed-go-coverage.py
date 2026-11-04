#!/usr/bin/env python3
"""Require at least 80% statement coverage for changed Go code."""

import os
import re
import subprocess
import sys


HUNK = re.compile(r"@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
COVERAGE_BLOCK = re.compile(r"(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)")
THRESHOLD = 80.0


def git_diff(base, head):
    return subprocess.run(
        ["git", "diff", "--no-ext-diff", "--unified=0", f"{base}...{head}", "--", "*.go"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout


def added_lines(diff):
    changed = {}
    current_file = None
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            current_file = line[6:]
            changed.setdefault(current_file, set())
        elif line.startswith("@@") and current_file:
            match = HUNK.search(line)
            if not match:
                continue
            start = int(match.group(1))
            count = int(match.group(2) or "1")
            changed[current_file].update(range(start, start + count))
    return changed


def main():
    base = os.environ.get("DIFF_BASE", "")
    head = os.environ.get("DIFF_HEAD", "HEAD")
    if not base or set(base) == {"0"}:
        print("No usable base commit; changed-code coverage check is skipped.")
        return 0

    changed = added_lines(git_diff(base, head))
    changed = {path: lines for path, lines in changed.items() if lines}
    if not changed:
        print("No added Go lines; changed-code coverage check passed.")
        return 0

    covered = 0
    total = 0
    with open(sys.argv[1], encoding="utf-8") as profile:
        for row in profile:
            match = COVERAGE_BLOCK.fullmatch(row.strip())
            if not match:
                continue
            path, first, last, statements, hits = match.groups()
            changed_file = next((name for name in changed if path.endswith(name)), None)
            if not changed_file:
                continue
            first, last = int(first), int(last)
            if any(first <= line <= last for line in changed[changed_file]):
                statements = int(statements)
                total += statements
                if int(hits) > 0:
                    covered += statements

    if total == 0:
        print("Changed Go lines contain no executable statements; coverage check passed.")
        return 0

    percentage = covered * 100.0 / total
    print(f"Changed Go statement coverage: {percentage:.1f}% ({covered}/{total}); required: {THRESHOLD:.0f}%.")
    return 0 if percentage >= THRESHOLD else 1


if __name__ == "__main__":
    raise SystemExit(main())
