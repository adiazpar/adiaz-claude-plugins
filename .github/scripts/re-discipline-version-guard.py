#!/usr/bin/env python3
"""Fail when packaged plugin content changes without a version bump.

Claude Code caches an installed plugin under `<marketplace>/<plugin>/<version>`
and keys updates on that version. Publishing changed content under a version
that already shipped therefore leaves every existing install pinned to the old
bytes: `autoUpdate` sees a version it already has and does nothing, and the
divergence is silent -- the runtime keeps serving stale packaged behavior with
no error anywhere.

This guard makes that failure impossible to merge. It enforces two rules:

1. Every manifest that declares the plugin version agrees with the others.
   The Claude and Codex manifests are separate files and drift independently.
2. If any packaged file changed between the base commit and HEAD, the declared
   version increased.

Usage:
    re-discipline-version-guard.py --base <ref> [--plugin <name>]

Exit status is 0 when the tree is publishable and 1 with an explanation
otherwise. A missing or unresolvable base ref (a new branch, an initial
commit, a force push) skips the comparison rather than failing, because there
is no prior published state to compare against.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

# Manifests that redundantly declare the plugin version, relative to the
# plugin directory. All of them must agree.
VERSION_MANIFESTS = (
    ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json",
)


def run_git(args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git"] + args, capture_output=True, text=True, check=False
    )


def ref_exists(ref: str) -> bool:
    return run_git(["rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"]).returncode == 0


def parse_semver(raw: str) -> tuple[int, ...]:
    """Parse a dotted numeric version, ignoring any pre-release suffix."""
    core = raw.split("-", 1)[0].split("+", 1)[0]
    parts = core.split(".")
    if not parts or not all(p.isdigit() for p in parts):
        raise ValueError(f"not a numeric version: {raw!r}")
    return tuple(int(p) for p in parts)


def version_from_json(text: str, origin: str) -> str:
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"{origin}: invalid JSON: {exc}") from exc
    version = data.get("version")
    if not isinstance(version, str) or not version.strip():
        raise ValueError(f"{origin}: missing a string 'version'")
    return version.strip()


def declared_versions(plugin_dir: Path) -> dict[str, str]:
    """Read the version each manifest declares in the working tree."""
    found: dict[str, str] = {}
    for rel in VERSION_MANIFESTS:
        path = plugin_dir / rel
        if not path.is_file():
            continue
        found[rel] = version_from_json(
            path.read_text(encoding="utf-8"), str(path)
        )
    return found


def version_at_ref(ref: str, path: str) -> str | None:
    """Read a manifest version as of `ref`, or None if absent there."""
    shown = run_git(["show", f"{ref}:{path}"])
    if shown.returncode != 0:
        return None
    return version_from_json(shown.stdout, f"{ref}:{path}")


def packaged_content_changed(base: str, plugin_path: str) -> bool:
    diff = run_git(["diff", "--quiet", base, "HEAD", "--", plugin_path])
    # 0 -> identical, 1 -> differs. Anything else is a git failure.
    if diff.returncode not in (0, 1):
        raise RuntimeError(
            f"git diff failed for {plugin_path}: {diff.stderr.strip()}"
        )
    return diff.returncode == 1


def changed_files(base: str, plugin_path: str) -> list[str]:
    listing = run_git(["diff", "--name-only", base, "HEAD", "--", plugin_path])
    return [line for line in listing.stdout.splitlines() if line.strip()]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base",
        default="",
        help="commit to compare against (the previously published state)",
    )
    parser.add_argument(
        "--plugin",
        default="re-discipline",
        help="plugin directory name under plugins/",
    )
    args = parser.parse_args(argv)

    plugin_path = f"plugins/{args.plugin}"
    plugin_dir = Path(plugin_path)
    if not plugin_dir.is_dir():
        print(f"version-guard: no such plugin directory: {plugin_path}")
        return 1

    # Rule 1: every manifest agrees, whether or not anything changed.
    try:
        versions = declared_versions(plugin_dir)
    except ValueError as exc:
        print(f"version-guard: {exc}")
        return 1

    if not versions:
        print(
            "version-guard: no manifest declares a version; expected one of: "
            + ", ".join(VERSION_MANIFESTS)
        )
        return 1

    distinct = sorted(set(versions.values()))
    if len(distinct) > 1:
        print("version-guard: plugin manifests declare different versions:")
        for rel, value in sorted(versions.items()):
            print(f"  {rel}: {value}")
        print("  fix: set the same version in every manifest above.")
        return 1

    current = distinct[0]
    try:
        current_parts = parse_semver(current)
    except ValueError as exc:
        print(f"version-guard: {exc}")
        return 1

    # Rule 2: content changes require the version to move forward.
    base = args.base.strip()
    if not base or not ref_exists(base):
        print(
            "version-guard: no comparable base commit "
            f"({base or 'unset'}); skipping the bump check."
        )
        print(f"version-guard: declared version {current} is self-consistent.")
        return 0

    try:
        changed = packaged_content_changed(base, plugin_path)
    except RuntimeError as exc:
        print(f"version-guard: {exc}")
        return 1

    if not changed:
        print(
            f"version-guard: no packaged change under {plugin_path}; "
            f"version {current} may stand."
        )
        return 0

    previous = None
    for rel in VERSION_MANIFESTS:
        previous = version_at_ref(base, f"{plugin_path}/{rel}")
        if previous is not None:
            break

    if previous is None:
        print(
            f"version-guard: {plugin_path} is new as of {base}; "
            f"version {current} accepted."
        )
        return 0

    files = changed_files(base, plugin_path)
    if previous == current:
        print(
            f"version-guard: {len(files)} packaged file(s) changed under "
            f"{plugin_path} but the version is still {current}."
        )
        for name in files[:20]:
            print(f"  {name}")
        if len(files) > 20:
            print(f"  ... and {len(files) - 20} more")
        print(
            "\n  Installed clients key their cache on this version, so "
            "republishing\n  the same version leaves every existing install "
            "on the old bytes.\n  fix: raise 'version' in "
            + " and ".join(VERSION_MANIFESTS)
            + "."
        )
        return 1

    try:
        previous_parts = parse_semver(previous)
    except ValueError as exc:
        print(f"version-guard: base version unreadable: {exc}")
        return 1

    if current_parts <= previous_parts:
        print(
            f"version-guard: version moved backwards: {previous} -> {current}."
        )
        print("  fix: the new version must be greater than the published one.")
        return 1

    print(
        f"version-guard: {len(files)} packaged file(s) changed and the "
        f"version advanced {previous} -> {current}."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
