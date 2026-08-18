#!/usr/bin/env python3
"""Fail when packaged plugin content changes without a version bump.

Claude Code caches an installed plugin under `<marketplace>/<plugin>/<version>`
and keys updates on that version. Publishing changed content under a version
that already shipped therefore leaves every existing install pinned to the old
bytes: `autoUpdate` sees a version it already has and does nothing, and the
divergence is silent -- the runtime keeps serving stale packaged behavior with
no error anywhere. That is not hypothetical; it stranded 75 commits of changes
under a single published version.

Nothing else here can catch it. The package audit and
re-discipline-sync-version.py prove the tree is internally consistent - every
manifest agrees with the declared constant - but both only ever look at one
commit. This guard is the only check that compares against what was published
before, so it owns exactly that one rule and leaves consistency to them.

The version is read from its single definition, RuntimeVersion in
knowledge/internal/knowledge/types.go.

Usage:
    re-discipline-version-guard.py --base <ref>

Exit status is 0 when the tree is publishable and 1 with an explanation
otherwise. A missing or unresolvable base ref (a new branch, an initial
commit, a force push) skips the comparison rather than failing, because there
is no prior published state to compare against.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

VERSION_SOURCE = "knowledge/internal/knowledge/types.go"
VERSION_CONSTANT = "RuntimeVersion"

# The plugin is the repository root. These paths never ship to an install,
# so changing them alone requires no version bump.
UNPACKAGED = (
    ".github",
    "tests",
    ".claude",
    ".agents",
    ".claude-plugin/marketplace.json",
)


def packaged_pathspec() -> list[str]:
    return ["."] + [f":(exclude){path}" for path in UNPACKAGED]


def run_git(args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(["git"] + args, capture_output=True, text=True, check=False)


def ref_exists(ref: str) -> bool:
    return (
        run_git(["rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"]).returncode
        == 0
    )


def parse_semver(raw: str) -> tuple[int, ...]:
    """Parse a dotted numeric version, ignoring any pre-release suffix."""
    core = raw.split("-", 1)[0].split("+", 1)[0]
    parts = core.split(".")
    if not parts or not all(part.isdigit() for part in parts):
        raise ValueError(f"not a numeric version: {raw!r}")
    return tuple(int(part) for part in parts)


def extract_version(text: str, origin: str) -> str:
    match = re.search(rf'\b{VERSION_CONSTANT}\s*=\s*"([^"]+)"', text)
    if not match:
        raise ValueError(f"{origin}: {VERSION_CONSTANT} is not declared")
    return match.group(1)


def version_in_tree() -> str:
    path = Path(VERSION_SOURCE)
    return extract_version(path.read_text(encoding="utf-8"), str(path))


def version_at_ref(ref: str) -> str | None:
    """Read the declared version as of `ref`, or None if absent there."""
    shown = run_git(["show", f"{ref}:{VERSION_SOURCE}"])
    if shown.returncode != 0:
        return None
    return extract_version(shown.stdout, f"{ref}:{VERSION_SOURCE}")


def packaged_content_changed(base: str) -> bool:
    diff = run_git(["diff", "--quiet", base, "HEAD", "--"] + packaged_pathspec())
    # 0 -> identical, 1 -> differs. Anything else is a git failure.
    if diff.returncode not in (0, 1):
        raise RuntimeError(f"git diff failed: {diff.stderr.strip()}")
    return diff.returncode == 1


def changed_files(base: str) -> list[str]:
    listing = run_git(
        ["diff", "--name-only", base, "HEAD", "--"] + packaged_pathspec()
    )
    return [line for line in listing.stdout.splitlines() if line.strip()]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base",
        default="",
        help="commit to compare against (the previously published state)",
    )
    args = parser.parse_args(argv)

    try:
        current = version_in_tree()
        current_parts = parse_semver(current)
    except (ValueError, OSError) as exc:
        print(f"version-guard: {exc}")
        return 1

    base = args.base.strip()
    if not base or not ref_exists(base):
        print(
            "version-guard: no comparable base commit "
            f"({base or 'unset'}); skipping the bump check."
        )
        return 0

    try:
        changed = packaged_content_changed(base)
    except RuntimeError as exc:
        print(f"version-guard: {exc}")
        return 1

    if not changed:
        print(f"version-guard: no packaged change; version {current} may stand.")
        return 0

    try:
        previous = version_at_ref(base)
    except ValueError as exc:
        print(f"version-guard: base version unreadable: {exc}")
        return 1

    if previous is None:
        print(
            f"version-guard: the plugin is new as of {base}; "
            f"version {current} accepted."
        )
        return 0

    files = changed_files(base)
    if previous == current:
        print(
            f"version-guard: {len(files)} packaged file(s) changed "
            f"but the version is still {current}."
        )
        for name in files[:20]:
            print(f"  {name}")
        if len(files) > 20:
            print(f"  ... and {len(files) - 20} more")
        print(
            "\n  Installed clients key their cache on this version, so "
            "republishing\n  the same version leaves every existing install "
            "on the old bytes.\n  fix: raise "
            f"{VERSION_CONSTANT} in {VERSION_SOURCE}, then run"
            "\n  .github/scripts/re-discipline-sync-version.py"
        )
        return 1

    try:
        previous_parts = parse_semver(previous)
    except ValueError as exc:
        print(f"version-guard: base version unreadable: {exc}")
        return 1

    if current_parts <= previous_parts:
        print(f"version-guard: version moved backwards: {previous} -> {current}.")
        print("  fix: the new version must be greater than the published one.")
        return 1

    print(
        f"version-guard: {len(files)} packaged file(s) changed and the "
        f"version advanced {previous} -> {current}."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
