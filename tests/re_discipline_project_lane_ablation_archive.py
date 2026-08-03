"""Build or verify the frozen pre-removal lane-ablation evidence archive.

The archive preserves source bytes exactly while fixing every ZIP metadata
field that would otherwise depend on the host clock or filesystem. Entries are
sorted, regular read-only-independent files with a 1980 epoch timestamp and
level-9 raw-deflate ZIP members. Only the standard library is required.
"""

from __future__ import annotations

import argparse
import io
import sys
import zipfile
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_project_lane_ablation_build import (
    MeasurementBuildError,
    _decode_json_bytes,
    _historical_projection_from_entries,
    _manifest_transform_digest,
)


ARCHIVE_TIMESTAMP = (1980, 1, 1, 0, 0, 0)
ARCHIVE_MODE = 0o100644
ARCHIVE_COMPRESSION_LEVEL = 9


def _source_bytes(path: Path, label: str) -> bytes:
    if not path.is_file() or path.is_symlink():
        raise MeasurementBuildError(f"{label}: must be a regular non-symlink file")
    try:
        return path.read_bytes()
    except OSError as error:
        raise MeasurementBuildError(f"{label}: cannot read {path}: {error}") from error


def _eval_entries(root: Path) -> dict[str, bytes]:
    if not root.is_dir() or root.is_symlink():
        raise MeasurementBuildError(
            "projectedEvalRoot: must be a regular non-symlink directory"
        )
    paths = sorted(path for path in root.rglob("*") if path.is_file())
    if len(paths) != 9:
        raise MeasurementBuildError(
            f"projectedEvalRoot: must contain exactly nine files, found {len(paths)}"
        )
    if any(path.is_symlink() for path in root.rglob("*")):
        raise MeasurementBuildError("projectedEvalRoot: may not contain symbolic links")
    entries: dict[str, bytes] = {}
    for path in paths:
        relative = path.relative_to(root).as_posix()
        if any(part in {"", ".", ".."} for part in relative.split("/")):
            raise MeasurementBuildError(
                f"projectedEvalRoot: unsafe relative path {relative!r}"
            )
        entries[f"evals/{relative}"] = _source_bytes(path, f"evals/{relative}")
    return entries


def build_archive_bytes(
    *,
    raw_benchmark_path: Path,
    projection_manifest_path: Path,
    projected_eval_root: Path,
    profile_catalog_path: Path,
    model_manifest_path: Path,
) -> bytes:
    """Return canonical archive bytes after validating the evidence inventory."""

    entries = {
        "raw-benchmark.json": _source_bytes(raw_benchmark_path, "rawBenchmark"),
        "projection-manifest.json": _source_bytes(
            projection_manifest_path, "projectionManifest"
        ),
        "profile-catalog.json": _source_bytes(profile_catalog_path, "profileCatalog"),
        "model-manifest.json": _source_bytes(model_manifest_path, "modelManifest"),
    }
    entries.update(_eval_entries(projected_eval_root))

    manifest = _decode_json_bytes(
        entries["projection-manifest.json"], "projection-manifest.json"
    )
    _manifest_transform_digest(manifest)
    _historical_projection_from_entries(manifest, entries)
    _decode_json_bytes(entries["raw-benchmark.json"], "raw-benchmark.json")
    _decode_json_bytes(entries["profile-catalog.json"], "profile-catalog.json")
    _decode_json_bytes(entries["model-manifest.json"], "model-manifest.json")

    buffer = io.BytesIO()
    with zipfile.ZipFile(
        buffer,
        mode="w",
        compression=zipfile.ZIP_DEFLATED,
        compresslevel=ARCHIVE_COMPRESSION_LEVEL,
        strict_timestamps=True,
    ) as archive:
        archive.comment = b""
        for name in sorted(entries):
            info = zipfile.ZipInfo(name, ARCHIVE_TIMESTAMP)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.create_version = 20
            info.extract_version = 20
            info.flag_bits = 0
            info.volume = 0
            info.internal_attr = 0
            info.external_attr = ARCHIVE_MODE << 16
            info.extra = b""
            info.comment = b""
            archive.writestr(
                info,
                entries[name],
                compress_type=zipfile.ZIP_DEFLATED,
                compresslevel=ARCHIVE_COMPRESSION_LEVEL,
            )
    return buffer.getvalue()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--raw-benchmark", type=Path, required=True)
    parser.add_argument("--projection-manifest", type=Path, required=True)
    parser.add_argument("--projected-eval-root", type=Path, required=True)
    parser.add_argument("--profile-catalog", type=Path, required=True)
    parser.add_argument("--model-manifest", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--verify",
        action="store_true",
        help="byte-compare the deterministic rebuild with an existing archive",
    )
    args = parser.parse_args()
    body = build_archive_bytes(
        raw_benchmark_path=args.raw_benchmark,
        projection_manifest_path=args.projection_manifest,
        projected_eval_root=args.projected_eval_root,
        profile_catalog_path=args.profile_catalog,
        model_manifest_path=args.model_manifest,
    )
    if args.verify:
        if not args.output.is_file() or args.output.is_symlink():
            raise MeasurementBuildError(
                f"{args.output}: existing archive is not a regular file"
            )
        if args.output.read_bytes() != body:
            raise MeasurementBuildError(
                f"{args.output}: existing archive is not the byte-identical rebuild"
            )
        print(f"verified byte-stable historical evidence archive: {args.output}")
        return 0
    args.output.parent.mkdir(parents=True, exist_ok=True)
    temporary = args.output.with_name(args.output.name + ".tmp")
    temporary.write_bytes(body)
    temporary.replace(args.output)
    print(f"wrote deterministic historical evidence archive: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
