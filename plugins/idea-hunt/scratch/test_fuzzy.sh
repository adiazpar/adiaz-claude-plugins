#!/bin/bash
# Test the past-winner fuzzy match using python3 + difflib.
# Usage: test_fuzzy.sh "<new candidate>" "<historic winner>"
NEW="${1:-AI Timesheet Validator}"
OLD="${2:-AI-powered timesheet validator}"

python3 - "$NEW" "$OLD" <<'PY'
import sys, difflib, re
def norm(s):
    s = s.lower()
    s = re.sub(r'[^\w\s]', '', s)
    s = re.sub(r'\s+', ' ', s).strip()
    return s
new, old = sys.argv[1], sys.argv[2]
ratio = difflib.SequenceMatcher(None, norm(new), norm(old)).ratio()
print(f"normalized new: '{norm(new)}'")
print(f"normalized old: '{norm(old)}'")
print(f"ratio: {ratio:.3f}")
print(f"match (>=0.85): {ratio >= 0.85}")
PY
