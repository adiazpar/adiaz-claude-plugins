#!/usr/bin/env python3
"""Propagate the single published-version definition into the JSON manifests.

The version is declared exactly once, as RuntimeVersion in
plugins/re-discipline/knowledge/internal/knowledge/types.go. Go code consumes
that constant directly and the package audit derives its expectation from it,
so those can never drift. The Claude and Codex plugin manifests are static
JSON read by hosts before any code runs, so they cannot reference a constant
and must carry a literal.

This script copies the constant into those literals, which keeps a release to
a one-line edit. The package audit already fails when they disagree, so
--check here is a faster, clearer signal rather than a second source of truth.

    re-discipline-sync-version.py            # rewrite manifests from the constant
    re-discipline-sync-version.py --check    # fail if they disagree
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
PLUGIN = ROOT / "plugins" / "re-discipline"
SOURCE = PLUGIN / "knowledge" / "internal" / "knowledge" / "types.go"
# Manifests whose top-level "version" is the published plugin version.
MANIFESTS = (
    PLUGIN / ".claude-plugin" / "plugin.json",
    PLUGIN / ".codex-plugin" / "plugin.json",
)

# Model manifests carry the same version inside a "runtime" block, and the
# loader rejects a manifest whose runtime version disagrees with the binary.
MODEL_MANIFESTS = (
    PLUGIN / "knowledge" / "models" / "manifest.json",
    PLUGIN
    / "knowledge"
    / "internal"
    / "knowledge"
    / "migration_templates"
    / "model-manifest.json",
)


def declared_version() -> str:
    text = SOURCE.read_text(encoding="utf-8")
    match = re.search(r'\bRuntimeVersion\s*=\s*"([^"]+)"', text)
    if not match:
        raise SystemExit(f"sync-version: RuntimeVersion is not declared in {SOURCE}")
    version = match.group(1)
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise SystemExit(f"sync-version: RuntimeVersion is not a semver triple: {version!r}")
    return version


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check",
        action="store_true",
        help="report disagreement instead of rewriting",
    )
    args = parser.parse_args(argv)

    version = declared_version()
    drift = []

    targets: list[tuple[pathlib.Path, str | None, bool]] = []
    for manifest in MANIFESTS:
        if not manifest.is_file():
            raise SystemExit(f"sync-version: missing manifest {manifest}")
        targets.append((manifest, json.loads(manifest.read_text("utf-8")).get("version"), False))
    for manifest in MODEL_MANIFESTS:
        if not manifest.is_file():
            raise SystemExit(f"sync-version: missing model manifest {manifest}")
        runtime = json.loads(manifest.read_text("utf-8")).get("runtime") or {}
        targets.append((manifest, runtime.get("version"), True))

    for manifest, current, nested in targets:
        if current == version:
            continue
        drift.append((manifest, current))
        if args.check:
            continue
        raw = manifest.read_text(encoding="utf-8")
        # Rewrite only the version literal so formatting and key order survive.
        # In a model manifest the runtime block is the first "version", and the
        # per-model entries below it must not be touched.
        updated, count = re.subn(
            r'("version"\s*:\s*)"[^"]*"', rf'\1"{version}"', raw, count=1
        )
        if count != 1:
            raise SystemExit(f"sync-version: no version field to rewrite in {manifest}")
        manifest.write_text(updated, encoding="utf-8", newline="")

    rel = lambda p: p.relative_to(ROOT).as_posix()  # noqa: E731

    if not drift:
        print(f"sync-version: all manifests already declare {version}")
        return 0

    if args.check:
        print(f"sync-version: manifests disagree with RuntimeVersion {version}:")
        for manifest, current in drift:
            print(f"  {rel(manifest)}: {current!r}")
        print(f"  fix: run {rel(pathlib.Path(__file__))}")
        return 1

    for manifest, current in drift:
        print(f"sync-version: {rel(manifest)} {current!r} -> {version!r}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
