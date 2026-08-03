"""Stage a pinned project retrieval benchmark without mutating its source checkout.

The staging harness accepts only clean, exact Git revisions.  It creates a
detached disposable project, preserves the original source roots outside that
project, applies the three current-runtime control-plane substitutions, and
runs the two-arm benchmark against a private cache.  Its receipt binds every
input and output needed by the project lane-ablation builder.

Only the Python standard library is used.  Verification is read-only and
replays the projection and indexed-source proofs from the staged artifacts.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import re
import shutil
import sqlite3
import stat
import subprocess
import sys
from pathlib import Path, PurePosixPath
from typing import Any, Callable, Iterable

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_project_lane_ablation import validate_json_schema
from tests.re_discipline_project_lane_ablation_build import (
    _go_json_bytes,
    _identity,
    _normalize_eval_for_go,
    _project_bytes,
    _strict_json_loads,
)


class StagingError(ValueError):
    """Raised when a staged benchmark cannot prove its inputs or outputs."""


REVISION_RE = re.compile(r"^[0-9a-f]{40}$")
IDENTITY_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
GENERATION_RE = re.compile(r"^generation-[0-9a-f]{20}$")
CANONICAL_BUDGETS = (512, 1024, 2048, 4096)
SOURCE_ROOTS = ("active", "docs", ".re-discipline")
EXCLUDED_SOURCE_PATHS = (
    ".re-discipline/cache",
    ".re-discipline/local-paths.md",
)
MIGRATION_PATHS = (
    ".re-discipline/state",
    ".re-discipline/transactions",
    "docs/history/campaigns",
)
CONTROL_SPECS = (
    (
        "bootstrap-config",
        "plugins/re-discipline/templates/project/config.json",
        ".re-discipline/config.json",
    ),
    (
        "knowledge-policy",
        "plugins/re-discipline/templates/project/policy.jsonc",
        ".re-discipline/knowledge/policy.jsonc",
    ),
    (
        "retrieval-profile",
        "plugins/re-discipline/templates/project/retrieval-profile.json",
        ".re-discipline/knowledge/retrieval-profile.json",
    ),
)
PROFILE_CATALOG = "plugins/re-discipline/knowledge/profiles/balanced-v1.json"
MIGRATION_PROFILE_CATALOG = (
    "plugins/re-discipline/knowledge/internal/knowledge/"
    "migration_templates/balanced-v1.json"
)
MODEL_MANIFEST = "plugins/re-discipline/knowledge/models/manifest.json"
ASSET_ROOT = "plugins/re-discipline/knowledge"
HARNESS_SCRIPT = "tests/re_discipline_project_lane_ablation_stage.py"
HARNESS_SCHEMA = (
    "plugins/re-discipline/knowledge/schemas/"
    "project-lane-ablation-harness.schema.json"
)
EVAL_ROOT = ".re-discipline/knowledge/evals"
PAIR_DIGEST_ALGORITHM = "sorted-path-null-sha256-v1"


def _fail(path: str, message: str) -> None:
    raise StagingError(f"{path}: {message}")


def _read_json(path: Path) -> dict[str, Any]:
    try:
        if path.is_symlink():
            _fail(str(path), "may not be a symbolic link")
        value = _strict_json_loads(path.read_bytes(), str(path))
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError) as error:
        if isinstance(error, StagingError):
            raise
        raise StagingError(f"cannot read JSON {path}: {error}") from error
    if not isinstance(value, dict):
        _fail(str(path), "top-level JSON value must be an object")
    return value


def _stable_json_bytes(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def _write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_bytes(_stable_json_bytes(value))
    temporary.replace(path)


def _file_identity(path: Path) -> str:
    if path.is_symlink() or not path.is_file():
        _fail(str(path), "must be a regular file")
    return _identity(path.read_bytes())


def _safe_relative(value: str, *, field: str) -> PurePosixPath:
    if not isinstance(value, str) or not value or "\\" in value:
        _fail(field, "must be a non-empty forward-slash relative path")
    path = PurePosixPath(value)
    if path.is_absolute() or any(part in ("", ".", "..") for part in path.parts):
        _fail(field, "must be a canonical contained relative path")
    if re.match(r"^[A-Za-z]:", value):
        _fail(field, "must not contain a drive prefix")
    return path


def _contained(root: Path, relative: str, *, field: str) -> Path:
    rel = _safe_relative(relative, field=field)
    candidate = (root / Path(*rel.parts)).resolve(strict=False)
    resolved_root = root.resolve(strict=True)
    try:
        candidate.relative_to(resolved_root)
    except ValueError:
        _fail(field, "escapes its declared root")
    return candidate


def _same_path(left: Path, right: Path) -> bool:
    return os.path.normcase(str(left.resolve(strict=False))) == os.path.normcase(
        str(right.resolve(strict=False))
    )


def _overlaps(left: Path, right: Path) -> bool:
    left_resolved = left.resolve(strict=False)
    right_resolved = right.resolve(strict=False)
    try:
        left_resolved.relative_to(right_resolved)
        return True
    except ValueError:
        pass
    try:
        right_resolved.relative_to(left_resolved)
        return True
    except ValueError:
        return False


def _run_bytes(
    command: list[str],
    *,
    cwd: Path,
    timeout_seconds: int = 120,
) -> subprocess.CompletedProcess[bytes]:
    try:
        return subprocess.run(
            command,
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=timeout_seconds,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise StagingError(f"command {command!r} failed to execute: {error}") from error


def _git(repo: Path, *arguments: str, timeout_seconds: int = 120) -> bytes:
    result = _run_bytes(
        ["git", "--no-optional-locks", "-C", str(repo), *arguments],
        cwd=repo,
        timeout_seconds=timeout_seconds,
    )
    if result.returncode != 0:
        message = result.stderr.decode("utf-8", errors="replace").strip()
        raise StagingError(
            f"git -C {repo} {' '.join(arguments)} failed ({result.returncode}): {message}"
        )
    return result.stdout


def _tool_version(command: list[str], *, cwd: Path) -> str:
    result = _run_bytes(command, cwd=cwd, timeout_seconds=30)
    if result.returncode != 0:
        _fail("tools", f"cannot run {' '.join(command)!r}")
    value = (result.stdout or result.stderr).decode("utf-8", errors="strict").strip()
    if not value:
        _fail("tools", f"{' '.join(command)!r} returned an empty version")
    return value


def _pair_digest(rows: Iterable[tuple[str, str]]) -> str:
    digest = hashlib.sha256()
    prior = ""
    seen: set[str] = set()
    for path, identity in sorted(rows):
        _safe_relative(path, field="pair.path")
        if path in seen or path <= prior:
            _fail("pairDigest", "paths must be unique after canonical sorting")
        if not IDENTITY_RE.fullmatch(identity):
            _fail("pair.identity", "must be a sha256 identity")
        seen.add(path)
        prior = path
        digest.update(path.encode("utf-8"))
        digest.update(b"\x00")
        digest.update(identity.encode("ascii"))
        digest.update(b"\x00")
    return "sha256:" + digest.hexdigest()


def _directory_manifest(root: Path) -> dict[str, Any]:
    if root.is_symlink() or not root.is_dir():
        _fail(str(root), "must be a real directory")
    rows: list[dict[str, Any]] = []
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        if path.is_symlink():
            _fail(str(path), "directory artifacts may not contain symbolic links")
        if path.is_dir():
            continue
        if not path.is_file():
            _fail(str(path), "directory artifacts may contain only regular files")
        relative = path.relative_to(root).as_posix()
        body = path.read_bytes()
        rows.append(
            {
                "path": relative,
                "sha256": _identity(body),
                "byteCount": len(body),
            }
        )
    return {
        "algorithm": PAIR_DIGEST_ALGORITHM,
        "fileCount": len(rows),
        "byteCount": sum(row["byteCount"] for row in rows),
        "manifestSha256": _pair_digest(
            (row["path"], row["sha256"]) for row in rows
        ),
        "files": rows,
    }


def _tracked_rows(repo: Path) -> list[tuple[str, str]]:
    output = _git(repo, "ls-files", "-z", "--stage")
    rows: list[tuple[str, str]] = []
    seen: set[str] = set()
    for entry in output.split(b"\x00"):
        if not entry:
            continue
        try:
            metadata, raw_path = entry.split(b"\t", 1)
            mode, _blob, stage = metadata.decode("ascii").split(" ")
            relative = raw_path.decode("utf-8", errors="strict")
        except (ValueError, UnicodeError) as error:
            raise StagingError(f"cannot parse git ls-files entry {entry!r}") from error
        if stage != "0":
            _fail("repository.index", f"{relative!r} has non-zero stage {stage}")
        if mode not in {"100644", "100755"}:
            _fail(
                "repository.index",
                f"{relative!r} has unsupported tracked mode {mode}; symlinks and submodules are forbidden",
            )
        _safe_relative(relative, field="repository.index.path")
        if relative in seen:
            _fail("repository.index", f"duplicate tracked path {relative!r}")
        seen.add(relative)
        absolute = repo / Path(*PurePosixPath(relative).parts)
        info = absolute.lstat()
        if not stat.S_ISREG(info.st_mode) or absolute.is_symlink():
            _fail("repository.index", f"{relative!r} is not a regular working-tree file")
        rows.append((relative, _identity(absolute.read_bytes())))
    rows.sort()
    return rows


def _repo_binding(repo: Path, revision: str, *, label: str) -> dict[str, Any]:
    if not REVISION_RE.fullmatch(revision):
        _fail(f"{label}.revision", "must be a full lowercase Git revision")
    if repo.is_symlink() or not repo.is_dir():
        _fail(label, "repository root must be a real directory")
    repo = repo.resolve(strict=True)
    discovered = Path(
        _git(repo, "rev-parse", "--show-toplevel").decode("utf-8", errors="strict").strip()
    )
    if not _same_path(repo, discovered):
        _fail(label, f"{repo} is not the exact Git worktree root {discovered}")
    head = _git(repo, "rev-parse", "HEAD").decode("ascii").strip()
    if head != revision:
        _fail(f"{label}.revision", f"HEAD is {head}, expected {revision}")
    status_output = _git(
        repo,
        "status",
        "--porcelain=v1",
        "-z",
        "--untracked-files=all",
    )
    if status_output:
        _fail(label, "repository is dirty (tracked or untracked changes are present)")
    rows = _tracked_rows(repo)
    tree = _git(repo, "rev-parse", "HEAD^{tree}").decode("ascii").strip()
    return {
        "revision": revision,
        "tree": tree,
        "trackedFileCount": len(rows),
        "trackedManifestSha256": _pair_digest(rows),
        "clean": True,
    }


def _assert_binding_unchanged(
    expected: dict[str, Any], current: dict[str, Any], *, label: str
) -> None:
    for field in (
        "revision",
        "tree",
        "trackedFileCount",
        "trackedManifestSha256",
        "clean",
    ):
        if current.get(field) != expected.get(field):
            _fail(f"{label}.{field}", "source repository changed during staging")


def _require_tracked(repo: Path, relatives: Iterable[str], *, label: str) -> None:
    tracked = {path for path, _identity_value in _tracked_rows(repo)}
    missing = sorted(set(relatives) - tracked)
    if missing:
        _fail(label, f"required revision-owned files are untracked: {missing!r}")


def _assert_migration_absent(root: Path, *, phase: str) -> None:
    for relative in MIGRATION_PATHS:
        path = root / Path(*PurePosixPath(relative).parts)
        if path.exists() or path.is_symlink():
            _fail(
                f"migrationGuard.{phase}",
                f"{relative} exists; this harness is pre-migration only",
            )


def _copy_project_roots(
    project: Path,
    backup: Path,
    *,
    exact_source: Path,
) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for relative in SOURCE_ROOTS:
        source = project / relative
        if source.is_symlink() or not source.is_dir():
            _fail("project", f"required source root {relative!r} is absent or unsafe")
        destination = backup / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(source), str(destination))
        rows.append(
            {
                "from": relative,
                "to": f"checkout-roots/{relative}",
                "restorable": True,
            }
        )
    _synchronize_checkout_bytes(exact_source, backup)

    def re_discipline_ignore(directory: str, names: list[str]) -> set[str]:
        if _same_path(Path(directory), backup / ".re-discipline"):
            return {name for name in names if name in {"cache", "local-paths.md"}}
        return set()

    for relative in SOURCE_ROOTS:
        source = backup / relative
        destination = project / relative
        ignore = re_discipline_ignore if relative == ".re-discipline" else None
        shutil.copytree(source, destination, copy_function=shutil.copy2, ignore=ignore)
    for relative in EXCLUDED_SOURCE_PATHS:
        path = project / Path(*PurePosixPath(relative).parts)
        if path.exists() or path.is_symlink():
            _fail("excludedSourcePaths", f"{relative} was not excluded")
    return rows


def _clone_project(
    source: Path,
    revision: str,
    destination: Path,
    *,
    timeout_seconds: int,
) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    result = _run_bytes(
        [
            "git",
            "--no-optional-locks",
            "clone",
            "--no-hardlinks",
            "--no-checkout",
            "--",
            str(source),
            str(destination),
        ],
        cwd=destination.parent,
        timeout_seconds=timeout_seconds,
    )
    if result.returncode != 0:
        _fail(
            "clone",
            result.stderr.decode("utf-8", errors="replace").strip()
            or f"git clone exited {result.returncode}",
        )
    _git(destination, "checkout", "--detach", revision, timeout_seconds=timeout_seconds)
    head = _git(destination, "rev-parse", "HEAD").decode("ascii").strip()
    if head != revision:
        _fail("clone.revision", f"checked out {head}, expected {revision}")


def _verify_checkout_bytes(source: Path, backup: Path) -> dict[str, Any]:
    tracked = {
        relative: identity
        for relative, identity in _tracked_rows(source)
        if relative.split("/", 1)[0] in SOURCE_ROOTS
    }
    rows: list[tuple[str, str]] = []
    observed: set[str] = set()
    for source_root in SOURCE_ROOTS:
        backup_root = backup / source_root
        manifest = _directory_manifest(backup_root)
        for row in manifest["files"]:
            relative = f"{source_root}/{row['path']}"
            observed.add(relative)
            if tracked.get(relative) != row["sha256"]:
                _fail(
                    "checkoutRoots",
                    f"detached checkout bytes for {relative} differ from the clean source worktree",
                )
            rows.append((relative, row["sha256"]))
    if observed != set(tracked):
        missing = sorted(set(tracked) - observed)
        extra = sorted(observed - set(tracked))
        _fail(
            "checkoutRoots",
            f"tracked inventory differs (missing={missing!r}, extra={extra!r})",
        )
    return {
        "algorithm": PAIR_DIGEST_ALGORITHM,
        "fileCount": len(rows),
        "manifestSha256": _pair_digest(rows),
    }


def _synchronize_checkout_bytes(source: Path, backup: Path) -> None:
    """Materialize the clean source worktree's exact bytes in the detached copy.

    Git checkout filters such as autocrlf are repository-local and are not
    cloned.  The clean source revision is already bound, so copying its tracked
    working bytes avoids silently measuring a second line-ending projection.
    """

    for relative, _identity_value in _tracked_rows(source):
        if relative.split("/", 1)[0] not in SOURCE_ROOTS:
            continue
        source_path = source / Path(*PurePosixPath(relative).parts)
        backup_path = backup / Path(*PurePosixPath(relative).parts)
        if backup_path.is_symlink() or not backup_path.is_file():
            try:
                probe = repr(backup_path.lstat())
            except OSError as error:
                probe = f"lstat failed: errno={error.errno} winerror={getattr(error, 'winerror', None)} {error}"
            siblings: list[str] = []
            try:
                siblings = sorted(item.name for item in backup_path.parent.iterdir())[:8]
            except OSError as error:
                siblings = [f"parent iterdir failed: {error}"]
            _fail(
                "checkoutRoots",
                f"detached checkout is missing {relative} "
                f"(target={backup_path} probe={probe} siblings={siblings!r})",
            )
        backup_path.write_bytes(source_path.read_bytes())


def _copy_file_exact(source: Path, destination: Path) -> None:
    if source.is_symlink() or not source.is_file():
        _fail(str(source), "must be a regular source file")
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(source.read_bytes())


def _control_rows(
    plugin_repo: Path,
    project: Path,
    backup: Path,
    *,
    apply: bool,
) -> list[dict[str, Any]]:
    packaged_profile = plugin_repo / Path(*PurePosixPath(PROFILE_CATALOG).parts)
    template_profile = plugin_repo / Path(*PurePosixPath(CONTROL_SPECS[2][1]).parts)
    migration_profile = plugin_repo / Path(
        *PurePosixPath(MIGRATION_PROFILE_CATALOG).parts
    )
    if not (
        packaged_profile.read_bytes()
        == template_profile.read_bytes()
        == migration_profile.read_bytes()
    ):
        _fail(
            "controlPlane.retrievalProfile",
            "packaged, project-template, and migration-template profiles must be byte-identical",
        )
    rows: list[dict[str, Any]] = []
    for kind, source_relative, project_relative in CONTROL_SPECS:
        source = plugin_repo / Path(*PurePosixPath(source_relative).parts)
        destination = project / Path(*PurePosixPath(project_relative).parts)
        original = backup / Path(*PurePosixPath(project_relative).parts)
        if original.is_symlink() or not original.is_file():
            _fail(
                f"controlPlaneSubstitutions.{kind}",
                f"original project file {project_relative} is absent",
            )
        if apply:
            _copy_file_exact(source, destination)
        elif destination.is_symlink() or not destination.is_file():
            _fail(
                f"controlPlaneSubstitutions.{kind}",
                f"staged project file {project_relative} is absent",
            )
        if destination.read_bytes() != source.read_bytes():
            _fail(
                f"controlPlaneSubstitutions.{kind}",
                f"staged project file {project_relative} differs from its replacement",
            )
        rows.append(
            {
                "kind": kind,
                "projectPath": project_relative,
                "originalSha256": _file_identity(original),
                "replacementSha256": _file_identity(source),
                "replacementSource": source_relative,
            }
        )
    return rows


def _apply_controls(
    plugin_repo: Path,
    project: Path,
    backup: Path,
) -> list[dict[str, Any]]:
    return _control_rows(plugin_repo, project, backup, apply=True)


def _eval_cases(root: Path) -> list[dict[str, Any]]:
    cases: list[dict[str, Any]] = []
    for path in sorted(root.rglob("*.json"), key=lambda item: item.as_posix()):
        value = _strict_json_loads(path.read_bytes(), str(path))
        if not isinstance(value, list):
            _fail(str(path), "evaluation JSON must be an array")
        for index, case in enumerate(value):
            if not isinstance(case, dict):
                _fail(f"{path}[{index}]", "evaluation case must be an object")
            cases.append(case)
    ids = [case.get("id") for case in cases]
    if len(cases) != 64:
        _fail("evals", f"must contain exactly 64 cases, found {len(cases)}")
    if any(not isinstance(case_id, str) or not case_id for case_id in ids):
        _fail("evals", "every case must have a non-empty string id")
    if len(set(ids)) != 64:
        _fail("evals", "case ids must be unique")
    return cases


def _build_projection(
    final_root: Path,
    projected_root: Path,
) -> tuple[dict[str, Any], list[dict[str, Any]], str]:
    final_manifest = _directory_manifest(final_root)
    if final_manifest["fileCount"] != 9:
        _fail(
            "finalEvalRoot",
            f"must contain exactly 9 files, found {final_manifest['fileCount']}",
        )
    if projected_root.exists() or projected_root.is_symlink():
        _fail("projectedEvalRoot", "destination already exists")
    projected_root.mkdir(parents=True)
    rows: list[dict[str, Any]] = []
    removed = 0
    for source_row in final_manifest["files"]:
        relative = source_row["path"]
        source = final_root / Path(*PurePosixPath(relative).parts)
        destination = projected_root / Path(*PurePosixPath(relative).parts)
        body = source.read_bytes()
        projected, removals = _project_bytes(body, relative)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(projected)
        case_count = 0
        if source.suffix.lower() == ".json":
            value = _strict_json_loads(body, str(source))
            if not isinstance(value, list):
                _fail(str(source), "evaluation JSON must be an array")
            case_count = len(value)
        rows.append(
            {
                "path": f"{EVAL_ROOT}/{relative}",
                "finalSha256": _identity(body),
                "projectedSha256": _identity(projected),
                "finalByteCount": len(body),
                "projectedByteCount": len(projected),
                "caseCount": case_count,
                "removals": removals,
            }
        )
        removed += len(removals)
    final_cases = _eval_cases(final_root)
    projected_cases = _eval_cases(projected_root)
    if [case["id"] for case in projected_cases] != [case["id"] for case in final_cases]:
        _fail("projection", "case ordering or identities changed")
    # The runtime forbids the target-disjoint attestation on abstention and
    # exact-lookup cases, so the corpus legitimately attests a subset. The
    # projection must strip exactly the attested members and nothing else.
    attested = sum(
        1
        for case in final_cases
        if case.get("vocabularyPolicy") == "target-disjoint-v1"
    )
    if removed != attested or removed < 1:
        _fail(
            "projection.removedOccurrences",
            f"must remove exactly the {attested} attested vocabulary members, removed {removed}",
        )
    manifest: dict[str, Any] = {
        "schemaVersion": 1,
        "transform": "delete-whole-json-member-line-v1",
        "field": "vocabularyPolicy",
        "allowedValue": "target-disjoint-v1",
        "preservesAllOtherBytes": True,
        "finalFileCount": 9,
        "caseCount": 64,
        "removedOccurrences": removed,
        "files": rows,
    }
    manifest["digest"] = _identity(_go_json_bytes(manifest))
    fingerprint = _identity(
        _go_json_bytes([_normalize_eval_for_go(case) for case in projected_cases])
    )
    return manifest, final_cases, fingerprint


def _replace_project_evals(project: Path, projected_root: Path) -> None:
    destination = project / Path(*PurePosixPath(EVAL_ROOT).parts)
    if destination.is_symlink() or not destination.is_dir():
        _fail("projectEvals", "disposable evaluation root is absent or unsafe")
    shutil.rmtree(destination)
    shutil.copytree(projected_root, destination, copy_function=shutil.copy2)


def _artifact_ref(path: Path, artifact_root: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        _fail(str(path), "artifact must be a regular file")
    try:
        relative = path.resolve(strict=True).relative_to(artifact_root.resolve(strict=True))
    except ValueError:
        _fail(str(path), "artifact must be contained by the artifact root")
    body = path.read_bytes()
    return {
        "path": relative.as_posix(),
        "sha256": _identity(body),
        "byteCount": len(body),
    }


def _directory_ref(path: Path, artifact_root: Path) -> dict[str, Any]:
    try:
        relative = path.resolve(strict=True).relative_to(artifact_root.resolve(strict=True))
    except ValueError:
        _fail(str(path), "directory artifact must be contained by the artifact root")
    manifest = _directory_manifest(path)
    return {
        "path": relative.as_posix(),
        "algorithm": manifest["algorithm"],
        "fileCount": manifest["fileCount"],
        "byteCount": manifest["byteCount"],
        "manifestSha256": manifest["manifestSha256"],
    }


def _resolve_artifact(
    artifact_root: Path,
    reference: dict[str, Any],
    *,
    field: str,
    directory: bool = False,
) -> Path:
    if not isinstance(reference, dict):
        _fail(field, "must be an artifact reference")
    path = _contained(artifact_root, reference.get("path"), field=f"{field}.path")
    if path.is_symlink():
        _fail(field, "artifact may not be a symbolic link")
    if directory:
        current = _directory_ref(path, artifact_root)
    else:
        current = _artifact_ref(path, artifact_root)
    if current != reference:
        _fail(field, "artifact path, size, or digest does not match the receipt")
    return path


def _validate_current_matrix(
    raw: dict[str, Any],
    *,
    project_revision: str,
    project_root: Path,
    eval_ids: set[str],
    eval_fingerprint: str,
) -> dict[str, Any]:
    expected = {
        "schemaVersion": 1,
        "mode": "full",
        "suite": "project-benchmark-v1",
        "complete": True,
        "unsupportedProfiles": [],
        "evalFingerprint": eval_fingerprint,
    }
    for field, value in expected.items():
        if raw.get(field) != value:
            _fail(f"rawBenchmark.{field}", f"must equal {value!r}")
    # The runtime reports the requested identity as the catalog id plus its
    # semantic digest; the catalog bytes themselves are captured beside the
    # receipt, so the digest suffix is validated by shape here.
    requested_profile = str(raw.get("requestedProfile"))
    if not re.fullmatch(
        r"plugin:balanced-v1@sha256:[0-9a-f]{64}", requested_profile
    ):
        _fail(
            "rawBenchmark.requestedProfile",
            "must be plugin:balanced-v1 with its semantic catalog digest",
        )
    generation = raw.get("generation")
    if not isinstance(generation, dict):
        _fail("rawBenchmark.generation", "must be an object")
    if generation.get("gitRevision") != project_revision:
        _fail("rawBenchmark.generation.gitRevision", "does not match pinned project revision")
    if not GENERATION_RE.fullmatch(str(generation.get("id"))):
        _fail("rawBenchmark.generation.id", "must be a canonical generation id")
    for field in ("corpusFingerprint", "dirtyFingerprint", "modelFingerprint"):
        if not IDENTITY_RE.fullmatch(str(generation.get(field))):
            _fail(f"rawBenchmark.generation.{field}", "must be a sha256 identity")
    for field in ("documentCount", "chunkCount"):
        if not isinstance(generation.get(field), int) or generation[field] < 1:
            _fail(f"rawBenchmark.generation.{field}", "must be positive")
    if not isinstance(generation.get("runtime"), dict) or not generation["runtime"]:
        _fail("rawBenchmark.generation.runtime", "must be a non-empty object")
    # The runtime deliberately reports a hashed worktree identity instead of
    # a machine-local path; the disposable project is already bound through
    # the pinned git revision and the projected corpus fingerprint.
    worktree = generation.get("worktree")
    if not isinstance(worktree, str) or not re.fullmatch(
        r"worktree:[0-9a-f]{16}", worktree
    ):
        _fail("rawBenchmark.generation.worktree", "must be a hashed worktree identity")
    if not isinstance(generation.get("project"), str) or not generation["project"]:
        _fail("rawBenchmark.generation.project", "must be non-empty")
    profiles = raw.get("profiles")
    if not isinstance(profiles, list) or len(profiles) != 2:
        _fail("rawBenchmark.profiles", "must contain exactly two current-runtime arms")
    by_name = {
        profile.get("profileName"): profile
        for profile in profiles
        if isinstance(profile, dict)
    }
    expected_profiles = {
        "lexical-graph-v1": ["exact", "fts", "graph"],
        "hybrid-no-rerank-v1": ["exact", "fts", "graph", "dense"],
    }
    if set(by_name) != set(expected_profiles):
        _fail("rawBenchmark.profiles", "must contain baseline and dense arms exactly once")
    for name, lanes in expected_profiles.items():
        profile = by_name[name]
        if profile.get("activeLanes") != lanes:
            _fail(f"rawBenchmark.profiles[{name}].activeLanes", "unexpected lane matrix")
        for field in ("cases", "contextPackCases"):
            rows = profile.get(field)
            if not isinstance(rows, list) or len(rows) != 64:
                _fail(
                    f"rawBenchmark.profiles[{name}].{field}",
                    "must contain exactly 64 rows",
                )
            ids = [row.get("caseId") for row in rows if isinstance(row, dict)]
            if len(ids) != 64 or set(ids) != eval_ids:
                _fail(
                    f"rawBenchmark.profiles[{name}].{field}",
                    "case ids do not join one-to-one to the eval corpus",
                )
        for field in ("casesByBudget", "contextPacksByBudget"):
            matrix = profile.get(field)
            if not isinstance(matrix, dict) or set(matrix) != {
                str(value) for value in CANONICAL_BUDGETS
            }:
                _fail(
                    f"rawBenchmark.profiles[{name}].{field}",
                    "must contain the complete 512/1024/2048/4096 matrix",
                )
            for budget in CANONICAL_BUDGETS:
                rows = matrix[str(budget)]
                if not isinstance(rows, list) or len(rows) != 64:
                    _fail(
                        f"rawBenchmark.profiles[{name}].{field}[{budget}]",
                        "must contain exactly 64 rows",
                    )
                ids = [row.get("caseId") for row in rows if isinstance(row, dict)]
                if len(ids) != 64 or set(ids) != eval_ids:
                    _fail(
                        f"rawBenchmark.profiles[{name}].{field}[{budget}]",
                        "case ids do not join one-to-one to the eval corpus",
                    )
    return generation


def _read_generation_pointer(cache_root: Path) -> tuple[dict[str, Any], Path]:
    pointer_path = cache_root / "current.json"
    pointer = _read_json(pointer_path)
    database_value = pointer.get("database")
    if not isinstance(database_value, str) or not database_value:
        _fail("currentGeneration.database", "must be a non-empty path")
    try:
        database = Path(database_value).resolve(strict=True)
    except OSError as error:
        raise StagingError(
            f"currentGeneration.database cannot be resolved: {error}"
        ) from error
    if database.is_symlink() or not database.is_file():
        _fail("currentGeneration.database", "must be a regular SQLite database")
    try:
        database.relative_to(cache_root.resolve(strict=True))
    except ValueError:
        _fail("currentGeneration.database", "escapes the disposable cache")
    return pointer, database


def _sqlite_document_rows(database: Path) -> list[tuple[str, str, str, int, str]]:
    try:
        uri = database.resolve(strict=True).as_uri() + "?mode=ro&immutable=1"
        connection = sqlite3.connect(uri, uri=True)
        try:
            rows = connection.execute(
                "SELECT path,tier,content_hash,size,source_kind "
                "FROM documents ORDER BY path"
            ).fetchall()
        finally:
            connection.close()
    except sqlite3.Error as error:
        raise StagingError(f"cannot read indexed documents from {database}: {error}") from error
    output: list[tuple[str, str, str, int, str]] = []
    prior = ""
    for index, row in enumerate(rows):
        if len(row) != 5:
            _fail(f"database.documents[{index}]", "unexpected row shape")
        path, tier, content_hash, size, source_kind = row
        _safe_relative(path, field=f"database.documents[{index}].path")
        if path <= prior:
            _fail("database.documents", "paths must be strictly sorted and unique")
        prior = path
        if not isinstance(tier, str) or not tier:
            _fail(f"database.documents[{index}].tier", "must be non-empty")
        if not re.fullmatch(r"[0-9a-f]{64}", str(content_hash)):
            _fail(f"database.documents[{index}].contentHash", "must be a bare SHA-256")
        if not isinstance(size, int) or size < 1:
            _fail(f"database.documents[{index}].size", "must be positive")
        if not isinstance(source_kind, str):
            _fail(f"database.documents[{index}].sourceKind", "must be a string")
        output.append((path, tier, content_hash, size, source_kind))
    if not output:
        _fail("database.documents", "must contain at least one indexed source")
    return output


def _build_indexed_source_manifest(
    *,
    database: Path,
    project: Path,
    backup: Path,
    project_name: str,
    project_revision: str,
    generation_id: str,
    corpus_fingerprint: str,
) -> dict[str, Any]:
    sources: list[dict[str, Any]] = []
    mismatches: list[str] = []
    for index, (relative, tier, db_hash, size, source_kind) in enumerate(
        _sqlite_document_rows(database)
    ):
        if relative.split("/", 1)[0] not in SOURCE_ROOTS:
            _fail(
                f"database.documents[{index}].path",
                "does not map to a preserved project source root",
            )
        disposable = project / Path(*PurePosixPath(relative).parts)
        original = backup / Path(*PurePosixPath(relative).parts)
        for label, path in (("disposable", disposable), ("checkout", original)):
            if path.is_symlink() or not path.is_file():
                _fail(
                    f"database.documents[{index}].{label}",
                    f"indexed path {relative!r} is absent or unsafe",
                )
        disposable_body = disposable.read_bytes()
        original_body = original.read_bytes()
        disposable_hash = hashlib.sha256(disposable_body).hexdigest()
        original_identity = _identity(original_body)
        byte_exact = (
            disposable_body == original_body
            and disposable_hash == db_hash
            and len(disposable_body) == size
        )
        if not byte_exact:
            mismatches.append(relative)
        sources.append(
            {
                "path": relative,
                "tier": tier,
                "sourceKind": source_kind,
                "size": size,
                "sha256": "sha256:" + db_hash,
                "checkoutSha256": original_identity,
                "byteExact": byte_exact,
            }
        )
    if mismatches:
        _fail(
            "indexedSourceProof",
            "indexed bytes differ from preserved checkout bytes: " + ", ".join(mismatches),
        )
    proof = {
        "algorithm": PAIR_DIGEST_ALGORITHM,
        "sourceCount": len(sources),
        "byteExactCount": len(sources),
        "mismatchCount": 0,
        "pathDigestPairsSha256": _pair_digest(
            (row["path"], row["sha256"]) for row in sources
        ),
    }
    return {
        "$schema": "plugin://re-discipline/schemas/indexed-source-manifest.internal.json",
        "schemaVersion": 1,
        "kind": "project-indexed-source-byte-proof",
        "project": project_name,
        "projectRevision": project_revision,
        "generationId": generation_id,
        "corpusFingerprint": corpus_fingerprint,
        **proof,
        "sources": sources,
    }


def _verify_indexed_source_manifest(
    manifest: dict[str, Any],
    *,
    database: Path,
    project: Path,
    backup: Path,
    project_name: str,
    project_revision: str,
    generation_id: str,
    corpus_fingerprint: str,
) -> dict[str, Any]:
    expected = _build_indexed_source_manifest(
        database=database,
        project=project,
        backup=backup,
        project_name=project_name,
        project_revision=project_revision,
        generation_id=generation_id,
        corpus_fingerprint=corpus_fingerprint,
    )
    if manifest != expected:
        _fail("indexedSources", "manifest is not the deterministic database/source replay")
    return {
        field: expected[field]
        for field in (
            "algorithm",
            "sourceCount",
            "byteExactCount",
            "mismatchCount",
            "pathDigestPairsSha256",
        )
    }


def _semantic_benchmark_command(go_executable: str) -> dict[str, Any]:
    command = {
        "cwd": "plugins/re-discipline/knowledge",
        "argv": [
            go_executable,
            "run",
            "./cmd/re-discipline-knowledge",
            "benchmark",
            "--asset-root",
            "$PLUGIN_ASSET_ROOT",
            "--project-root",
            "$DISPOSABLE_PROJECT_ROOT",
            "--cache-root",
            "$DISPOSABLE_CACHE_ROOT",
            "--mode",
            "full",
        ],
    }
    command["sha256"] = _identity(_go_json_bytes(command))
    return command


def _validate_receipt_schema(schema_path: Path, receipt: dict[str, Any]) -> None:
    schema = _read_json(schema_path)
    try:
        validate_json_schema(schema, receipt)
    except ValueError as error:
        raise StagingError(f"harness receipt schema validation failed: {error}") from error


def _negative_controls(
    projection: dict[str, Any],
    indexed: dict[str, Any],
    *,
    dirty_repository: Path,
    dirty_revision: str,
) -> list[dict[str, Any]]:
    projection_digest = projection["digest"]
    tampered_projection = copy.deepcopy(projection)
    tampered_projection["caseCount"] += 1
    unsigned = copy.deepcopy(tampered_projection)
    unsigned.pop("digest", None)
    projection_rejected = _identity(_go_json_bytes(unsigned)) != projection_digest

    indexed_digest = indexed["pathDigestPairsSha256"]
    first = indexed["sources"][0]
    tampered_source = _identity((first["sha256"] + "tamper").encode("utf-8"))
    indexed_rejected = (
        _pair_digest(
            [(first["path"], tampered_source)]
            + [(row["path"], row["sha256"]) for row in indexed["sources"][1:]]
        )
        != indexed_digest
    )
    dirty_rejected = False
    dirty_message = ""
    try:
        _repo_binding(
            dirty_repository,
            dirty_revision,
            label="negativeControl.dirtyDisposableRepository",
        )
    except StagingError as error:
        if "repository is dirty" not in str(error):
            raise
        dirty_rejected = True
        dirty_message = str(error)
    if not (projection_rejected and indexed_rejected and dirty_rejected):
        _fail("negativeControls", "an in-memory tamper control unexpectedly passed")
    return [
        {
            "name": "dirty-source-repository",
            "expectedFailure": True,
            "observed": dirty_rejected,
            "message": dirty_message,
        },
        {
            "name": "projection-byte-tamper",
            "expectedFailure": True,
            "observed": projection_rejected,
            "message": "projection transform digest changes after one semantic mutation",
        },
        {
            "name": "indexed-source-byte-tamper",
            "expectedFailure": True,
            "observed": indexed_rejected,
            "message": "sorted path/digest proof changes after one source mutation",
        },
    ]


BenchmarkRunner = Callable[
    [list[str], Path, int], subprocess.CompletedProcess[bytes]
]


def _default_benchmark_runner(
    command: list[str], cwd: Path, timeout_seconds: int
) -> subprocess.CompletedProcess[bytes]:
    return _run_bytes(command, cwd=cwd, timeout_seconds=timeout_seconds)


def _run_benchmark(
    *,
    plugin_repo: Path,
    project: Path,
    cache_root: Path,
    artifact_root: Path,
    project_revision: str,
    eval_ids: set[str],
    eval_fingerprint: str,
    go_executable: str,
    timeout_seconds: int,
    runner: BenchmarkRunner,
) -> tuple[dict[str, Any], dict[str, Any], Path, Path, dict[str, Any]]:
    asset_root = plugin_repo / Path(*PurePosixPath(ASSET_ROOT).parts)
    command = [
        go_executable,
        "run",
        "./cmd/re-discipline-knowledge",
        "benchmark",
        "--asset-root",
        str(asset_root),
        "--project-root",
        str(project),
        "--cache-root",
        str(cache_root),
        "--mode",
        "full",
    ]
    result = runner(command, asset_root, timeout_seconds)
    try:
        raw_value = _strict_json_loads(result.stdout, "benchmark.stdout")
    except (ValueError, json.JSONDecodeError, UnicodeError) as error:
        raise StagingError(
            "benchmark did not emit exactly one strict JSON report; stderr: "
            + result.stderr.decode("utf-8", errors="replace").strip()
        ) from error
    if not isinstance(raw_value, dict):
        _fail("benchmark.stdout", "top-level JSON value must be an object")
    raw = raw_value
    expected_exit = 0 if raw.get("passed") is True else 1
    if result.returncode != expected_exit:
        _fail(
            "benchmark.exitCode",
            f"was {result.returncode}, expected {expected_exit} for passed={raw.get('passed')!r}",
        )
    generation = _validate_current_matrix(
        raw,
        project_revision=project_revision,
        project_root=project,
        eval_ids=eval_ids,
        eval_fingerprint=eval_fingerprint,
    )
    report_value = raw.get("reportPath")
    if not isinstance(report_value, str) or not report_value:
        _fail("rawBenchmark.reportPath", "must be a non-empty path")
    try:
        report_path = Path(report_value).resolve(strict=True)
    except OSError as error:
        raise StagingError(f"rawBenchmark.reportPath cannot be resolved: {error}") from error
    if report_path.is_symlink() or not report_path.is_file():
        _fail("rawBenchmark.reportPath", "must name a regular cache report")
    try:
        report_path.relative_to(cache_root.resolve(strict=True))
    except ValueError:
        _fail("rawBenchmark.reportPath", "escapes the disposable cache")
    cached_raw = _read_json(report_path)
    if cached_raw != raw:
        _fail("rawBenchmark", "stdout and cache report differ")
    raw_artifact = artifact_root / "current-two-arm-raw.json"
    _copy_file_exact(report_path, raw_artifact)
    pointer, database = _read_generation_pointer(cache_root)
    summary_fields = (
        "id",
        "corpusFingerprint",
        "modelFingerprint",
        "project",
        "worktree",
        "gitRevision",
        "dirtyFingerprint",
        "parserVersion",
        "chunkerVersion",
        "createdAt",
        "runtime",
        "documentCount",
        "chunkCount",
    )
    for field in summary_fields:
        if pointer.get(field) != generation.get(field):
            _fail(f"currentGeneration.{field}", "differs from benchmark generation")
    pointer_artifact = artifact_root / "current-generation.json"
    _copy_file_exact(cache_root / "current.json", pointer_artifact)
    database_artifact = artifact_root / "generation.sqlite"
    _copy_file_exact(database, database_artifact)
    execution = {
        "exitCode": result.returncode,
        "stdoutSha256": _identity(result.stdout),
        "stderrSha256": _identity(result.stderr),
    }
    return raw, pointer, database, raw_artifact, execution


def _replay_projection(
    final_root: Path, projected_root: Path
) -> tuple[dict[str, Any], list[dict[str, Any]], str]:
    final_manifest = _directory_manifest(final_root)
    projected_manifest = _directory_manifest(projected_root)
    if final_manifest["fileCount"] != 9 or projected_manifest["fileCount"] != 9:
        _fail("projection", "final and projected roots must each contain exactly 9 files")
    if [row["path"] for row in final_manifest["files"]] != [
        row["path"] for row in projected_manifest["files"]
    ]:
        _fail("projection", "final and projected file inventories differ")
    rows: list[dict[str, Any]] = []
    removed = 0
    for final_row in final_manifest["files"]:
        relative = final_row["path"]
        final_path = final_root / Path(*PurePosixPath(relative).parts)
        projected_path = projected_root / Path(*PurePosixPath(relative).parts)
        body = final_path.read_bytes()
        expected, removals = _project_bytes(body, relative)
        observed = projected_path.read_bytes()
        if observed != expected:
            _fail(f"projection.files[{relative}]", "is not the byte-exact replay")
        case_count = 0
        if final_path.suffix.lower() == ".json":
            value = _strict_json_loads(body, str(final_path))
            if not isinstance(value, list):
                _fail(str(final_path), "evaluation JSON must be an array")
            case_count = len(value)
        rows.append(
            {
                "path": f"{EVAL_ROOT}/{relative}",
                "finalSha256": _identity(body),
                "projectedSha256": _identity(observed),
                "finalByteCount": len(body),
                "projectedByteCount": len(observed),
                "caseCount": case_count,
                "removals": removals,
            }
        )
        removed += len(removals)
    final_cases = _eval_cases(final_root)
    projected_cases = _eval_cases(projected_root)
    if [case["id"] for case in final_cases] != [case["id"] for case in projected_cases]:
        _fail("projection", "case ordering or identities changed")
    manifest: dict[str, Any] = {
        "schemaVersion": 1,
        "transform": "delete-whole-json-member-line-v1",
        "field": "vocabularyPolicy",
        "allowedValue": "target-disjoint-v1",
        "preservesAllOtherBytes": True,
        "finalFileCount": 9,
        "caseCount": 64,
        "removedOccurrences": removed,
        "files": rows,
    }
    manifest["digest"] = _identity(_go_json_bytes(manifest))
    fingerprint = _identity(
        _go_json_bytes([_normalize_eval_for_go(case) for case in projected_cases])
    )
    return manifest, final_cases, fingerprint


def _repo_receipt(binding: dict[str, Any]) -> dict[str, Any]:
    return {
        "revision": binding["revision"],
        "tree": binding["tree"],
        "trackedFileCount": binding["trackedFileCount"],
        "trackedManifestSha256": binding["trackedManifestSha256"],
        "cleanBefore": True,
        "cleanAfter": True,
    }


def stage(
    *,
    plugin_repository: Path,
    plugin_revision: str,
    project_repository: Path,
    project_revision: str,
    output_root: Path,
    go_executable: str = "go",
    timeout_seconds: int = 1800,
    runner: BenchmarkRunner | None = None,
) -> dict[str, Any]:
    """Create and immediately verify one self-contained staging receipt."""

    if timeout_seconds < 1:
        _fail("timeoutSeconds", "must be positive")
    if not isinstance(go_executable, str) or not go_executable.strip():
        _fail("goExecutable", "must be non-empty")
    plugin_repository = plugin_repository.resolve(strict=True)
    project_repository = project_repository.resolve(strict=True)
    output_root = output_root.resolve(strict=False)
    if output_root.exists() or output_root.is_symlink():
        _fail("outputRoot", "must not already exist")
    for label, repository in (
        ("plugin", plugin_repository),
        ("project", project_repository),
    ):
        if _overlaps(output_root, repository):
            _fail("outputRoot", f"must be outside the {label} repository")
    if _overlaps(plugin_repository, project_repository):
        _fail("repositories", "plugin and project repositories must not overlap")

    plugin_before = _repo_binding(
        plugin_repository, plugin_revision, label="pluginRepository"
    )
    project_before = _repo_binding(
        project_repository, project_revision, label="projectRepository"
    )
    _assert_migration_absent(project_repository, phase="sourceBefore")
    script_path = plugin_repository / Path(*PurePosixPath(HARNESS_SCRIPT).parts)
    schema_path = plugin_repository / Path(*PurePosixPath(HARNESS_SCHEMA).parts)
    if script_path.is_symlink() or not script_path.is_file():
        _fail("tools.harnessScriptSha256", "pinned harness script is absent or unsafe")
    if script_path.read_bytes() != Path(__file__).resolve().read_bytes():
        _fail(
            "tools.harnessScriptSha256",
            "executing harness differs from the pinned plugin repository copy",
        )
    _read_json(schema_path)
    for relative in (
        ASSET_ROOT,
        PROFILE_CATALOG,
        MODEL_MANIFEST,
        MIGRATION_PROFILE_CATALOG,
        *(row[1] for row in CONTROL_SPECS),
    ):
        path = plugin_repository / Path(*PurePosixPath(relative).parts)
        if not path.exists() or path.is_symlink():
            _fail("pluginRepository", f"required asset {relative} is absent or unsafe")
    _require_tracked(
        plugin_repository,
        (
            HARNESS_SCRIPT,
            HARNESS_SCHEMA,
            PROFILE_CATALOG,
            MIGRATION_PROFILE_CATALOG,
            MODEL_MANIFEST,
            *(row[1] for row in CONTROL_SPECS),
        ),
        label="pluginRepository",
    )

    artifact_root = output_root / "artifacts"
    disposable_project = output_root / "disposable" / "project"
    backup_root = output_root / "checkout-roots"
    # The runtime grants explicit cache roots only below the project or the
    # deterministic machine-local base, so the disposable measurement cache
    # lives inside the disposable project and is retained with it.
    cache_root = disposable_project / ".re-discipline" / "cache" / "knowledge"
    output_root.mkdir(parents=True)
    artifact_root.mkdir()
    _clone_project(
        project_repository,
        project_revision,
        disposable_project,
        timeout_seconds=timeout_seconds,
    )
    renames = _copy_project_roots(
        disposable_project,
        backup_root,
        exact_source=project_repository,
    )
    checkout_manifest = _verify_checkout_bytes(project_repository, backup_root)
    _assert_migration_absent(backup_root, phase="preservedCheckout")
    _assert_migration_absent(disposable_project, phase="disposableBefore")

    final_eval_root = artifact_root / "final-evals"
    final_source = backup_root / Path(*PurePosixPath(EVAL_ROOT).parts)
    shutil.copytree(final_source, final_eval_root, copy_function=shutil.copy2)
    projected_eval_root = artifact_root / "projected-evals"
    projection, final_cases, eval_fingerprint = _build_projection(
        final_eval_root, projected_eval_root
    )
    projection_path = artifact_root / "current-projection.json"
    _write_json(projection_path, projection)
    _replace_project_evals(disposable_project, projected_eval_root)
    substitutions = _apply_controls(
        plugin_repository, disposable_project, backup_root
    )

    profile_path = artifact_root / "current-profile-catalog.json"
    model_path = artifact_root / "current-model-manifest.json"
    _copy_file_exact(
        plugin_repository / Path(*PurePosixPath(PROFILE_CATALOG).parts), profile_path
    )
    _copy_file_exact(
        plugin_repository / Path(*PurePosixPath(MODEL_MANIFEST).parts), model_path
    )

    benchmark_runner = runner or _default_benchmark_runner
    raw, pointer, database, raw_path, execution = _run_benchmark(
        plugin_repo=plugin_repository,
        project=disposable_project,
        cache_root=cache_root,
        artifact_root=artifact_root,
        project_revision=project_revision,
        eval_ids={str(case["id"]) for case in final_cases},
        eval_fingerprint=eval_fingerprint,
        go_executable=go_executable,
        timeout_seconds=timeout_seconds,
        runner=benchmark_runner,
    )
    generation = raw["generation"]
    indexed = _build_indexed_source_manifest(
        database=database,
        project=disposable_project,
        backup=backup_root,
        project_name=generation["project"],
        project_revision=project_revision,
        generation_id=generation["id"],
        corpus_fingerprint=generation["corpusFingerprint"],
    )
    if indexed["sourceCount"] != generation["documentCount"]:
        _fail(
            "indexedSources.sourceCount",
            "does not equal the benchmark generation documentCount",
        )
    indexed_path = artifact_root / "indexed-sources.json"
    _write_json(indexed_path, indexed)

    _assert_migration_absent(disposable_project, phase="disposableAfter")
    _assert_migration_absent(project_repository, phase="sourceAfter")
    plugin_after = _repo_binding(
        plugin_repository, plugin_revision, label="pluginRepository"
    )
    project_after = _repo_binding(
        project_repository, project_revision, label="projectRepository"
    )
    _assert_binding_unchanged(plugin_before, plugin_after, label="pluginRepository")
    _assert_binding_unchanged(project_before, project_after, label="projectRepository")

    semantic_command = _semantic_benchmark_command(go_executable)
    semantic_command.update(execution)
    runtime_identity = copy.deepcopy(generation["runtime"])
    receipt: dict[str, Any] = {
        "$schema": "plugin://re-discipline/schemas/project-lane-ablation-harness.schema.json",
        "schemaVersion": 1,
        "kind": "project-retrieval-staging-harness",
        "project": generation["project"],
        "sourceRepositoryMutated": False,
        "repositories": {
            "plugin": _repo_receipt(plugin_before),
            "project": _repo_receipt(project_before),
        },
        "checkout": {
            "disposableProjectPath": "disposable/project",
            "preservedCheckoutPath": "checkout-roots",
            "sourceRootsAlgorithm": checkout_manifest["algorithm"],
            "sourceRootsFileCount": checkout_manifest["fileCount"],
            "sourceRootsManifestSha256": checkout_manifest["manifestSha256"],
        },
        "tools": {
            "harnessScriptSha256": _file_identity(script_path),
            "harnessSchemaSha256": _file_identity(schema_path),
            "pythonVersion": sys.version.splitlines()[0],
            "gitVersion": _tool_version(["git", "--version"], cwd=plugin_repository),
            "goVersion": _tool_version(
                [go_executable, "version"], cwd=plugin_repository
            ),
        },
        "benchmarkCommand": semantic_command,
        "artifacts": {
            "rawBenchmark": _artifact_ref(raw_path, artifact_root),
            "projectionManifest": _artifact_ref(projection_path, artifact_root),
            "profileCatalog": _artifact_ref(profile_path, artifact_root),
            "modelManifest": _artifact_ref(model_path, artifact_root),
            "indexedSources": _artifact_ref(indexed_path, artifact_root),
            "currentGeneration": _artifact_ref(
                artifact_root / "current-generation.json", artifact_root
            ),
            "generationDatabase": _artifact_ref(
                artifact_root / "generation.sqlite", artifact_root
            ),
            "finalEvalRoot": _directory_ref(final_eval_root, artifact_root),
            "projectedEvalRoot": _directory_ref(projected_eval_root, artifact_root),
        },
        "runtime": {
            "runId": raw["runId"],
            "generationId": generation["id"],
            "corpusFingerprint": generation["corpusFingerprint"],
            "evalFingerprint": raw["evalFingerprint"],
            "projectGitRevision": generation["gitRevision"],
            "runtimeIdentity": runtime_identity,
            "runtimeIdentitySha256": _identity(_go_json_bytes(runtime_identity)),
            "profileCatalogSha256": _file_identity(profile_path),
            "modelManifestSha256": _file_identity(model_path),
            "armCount": 2,
            "casesPerArm": 64,
        },
        "indexedSourceProof": {
            field: indexed[field]
            for field in (
                "algorithm",
                "sourceCount",
                "byteExactCount",
                "mismatchCount",
                "pathDigestPairsSha256",
            )
        },
        "controlPlaneSubstitutions": substitutions,
        "renamedPaths": renames,
        "excludedSourcePaths": list(EXCLUDED_SOURCE_PATHS),
        "negativeControls": _negative_controls(
            projection,
            indexed,
            dirty_repository=disposable_project,
            dirty_revision=project_revision,
        ),
        "migrationGuard": {
            "paths": list(MIGRATION_PATHS),
            "absentBefore": True,
            "absentAfter": True,
        },
    }
    _validate_receipt_schema(schema_path, receipt)
    receipt_path = artifact_root / "harness-receipt.json"
    _write_json(receipt_path, receipt)
    verify_stage(
        plugin_repository=plugin_repository,
        plugin_revision=plugin_revision,
        project_repository=project_repository,
        project_revision=project_revision,
        output_root=output_root,
        go_executable=go_executable,
    )
    return receipt


def verify_stage(
    *,
    plugin_repository: Path,
    plugin_revision: str,
    project_repository: Path,
    project_revision: str,
    output_root: Path,
    go_executable: str = "go",
) -> dict[str, Any]:
    """Verify an existing staging output without executing or writing anything."""

    plugin_repository = plugin_repository.resolve(strict=True)
    project_repository = project_repository.resolve(strict=True)
    output_root = output_root.resolve(strict=True)
    artifact_root = output_root / "artifacts"
    receipt_path = artifact_root / "harness-receipt.json"
    schema_path = plugin_repository / Path(*PurePosixPath(HARNESS_SCHEMA).parts)
    script_path = plugin_repository / Path(*PurePosixPath(HARNESS_SCRIPT).parts)
    receipt = _read_json(receipt_path)
    _validate_receipt_schema(schema_path, receipt)
    if script_path.is_symlink() or not script_path.is_file():
        _fail("tools.harnessScriptSha256", "pinned harness script is absent or unsafe")
    if script_path.read_bytes() != Path(__file__).resolve().read_bytes():
        _fail("tools.harnessScriptSha256", "executing harness differs from pinned copy")
    if receipt.get("sourceRepositoryMutated") is not False:
        _fail("sourceRepositoryMutated", "must be false")
    if receipt.get("checkout", {}).get("disposableProjectPath") != "disposable/project":
        _fail("checkout.disposableProjectPath", "unexpected staging layout")
    if receipt.get("checkout", {}).get("preservedCheckoutPath") != "checkout-roots":
        _fail("checkout.preservedCheckoutPath", "unexpected staging layout")
    disposable_project = output_root / "disposable" / "project"
    backup_root = output_root / "checkout-roots"
    cache_root = disposable_project / ".re-discipline" / "cache" / "knowledge"
    for label, path in (
        ("disposableProject", disposable_project),
        ("preservedCheckout", backup_root),
        ("cache", cache_root),
    ):
        if path.is_symlink() or not path.is_dir():
            _fail(label, "staging directory is absent or unsafe")

    plugin_binding = _repo_binding(
        plugin_repository, plugin_revision, label="pluginRepository"
    )
    project_binding = _repo_binding(
        project_repository, project_revision, label="projectRepository"
    )
    repositories = receipt.get("repositories")
    if not isinstance(repositories, dict):
        _fail("repositories", "must be an object")
    if repositories.get("plugin") != _repo_receipt(plugin_binding):
        _fail("repositories.plugin", "does not match the clean pinned repository")
    if repositories.get("project") != _repo_receipt(project_binding):
        _fail("repositories.project", "does not match the clean pinned repository")
    _require_tracked(
        plugin_repository,
        (
            HARNESS_SCRIPT,
            HARNESS_SCHEMA,
            PROFILE_CATALOG,
            MIGRATION_PROFILE_CATALOG,
            MODEL_MANIFEST,
            *(row[1] for row in CONTROL_SPECS),
        ),
        label="pluginRepository",
    )
    checkout_manifest = _verify_checkout_bytes(project_repository, backup_root)
    checkout = receipt["checkout"]
    expected_checkout = {
        "sourceRootsAlgorithm": checkout_manifest["algorithm"],
        "sourceRootsFileCount": checkout_manifest["fileCount"],
        "sourceRootsManifestSha256": checkout_manifest["manifestSha256"],
    }
    for field, expected in expected_checkout.items():
        if checkout.get(field) != expected:
            _fail(f"checkout.{field}", "does not match the preserved checkout")

    _assert_migration_absent(project_repository, phase="sourceVerify")
    _assert_migration_absent(backup_root, phase="preservedCheckoutVerify")
    _assert_migration_absent(disposable_project, phase="disposableVerify")
    migration_guard = receipt.get("migrationGuard")
    if migration_guard != {
        "paths": list(MIGRATION_PATHS),
        "absentBefore": True,
        "absentAfter": True,
    }:
        _fail("migrationGuard", "does not bind the complete pre-migration guard")
    if receipt.get("excludedSourcePaths") != list(EXCLUDED_SOURCE_PATHS):
        _fail("excludedSourcePaths", "does not bind the canonical exclusions")
    for relative in EXCLUDED_SOURCE_PATHS:
        path = disposable_project / Path(*PurePosixPath(relative).parts)
        if path.exists() or path.is_symlink():
            _fail("excludedSourcePaths", f"{relative} exists in disposable project")

    artifacts = receipt.get("artifacts")
    if not isinstance(artifacts, dict):
        _fail("artifacts", "must be an object")
    raw_path = _resolve_artifact(
        artifact_root, artifacts.get("rawBenchmark"), field="artifacts.rawBenchmark"
    )
    projection_path = _resolve_artifact(
        artifact_root,
        artifacts.get("projectionManifest"),
        field="artifacts.projectionManifest",
    )
    profile_path = _resolve_artifact(
        artifact_root,
        artifacts.get("profileCatalog"),
        field="artifacts.profileCatalog",
    )
    model_path = _resolve_artifact(
        artifact_root,
        artifacts.get("modelManifest"),
        field="artifacts.modelManifest",
    )
    indexed_path = _resolve_artifact(
        artifact_root,
        artifacts.get("indexedSources"),
        field="artifacts.indexedSources",
    )
    pointer_path = _resolve_artifact(
        artifact_root,
        artifacts.get("currentGeneration"),
        field="artifacts.currentGeneration",
    )
    database_path = _resolve_artifact(
        artifact_root,
        artifacts.get("generationDatabase"),
        field="artifacts.generationDatabase",
    )
    final_eval_root = _resolve_artifact(
        artifact_root,
        artifacts.get("finalEvalRoot"),
        field="artifacts.finalEvalRoot",
        directory=True,
    )
    projected_eval_root = _resolve_artifact(
        artifact_root,
        artifacts.get("projectedEvalRoot"),
        field="artifacts.projectedEvalRoot",
        directory=True,
    )

    expected_projection, final_cases, eval_fingerprint = _replay_projection(
        final_eval_root, projected_eval_root
    )
    projection = _read_json(projection_path)
    if projection != expected_projection:
        _fail("artifacts.projectionManifest", "is not the deterministic replay")
    project_evals = disposable_project / Path(*PurePosixPath(EVAL_ROOT).parts)
    if _directory_manifest(project_evals) != _directory_manifest(projected_eval_root):
        _fail("projectEvals", "does not equal the staged projected corpus")

    source_profile = plugin_repository / Path(*PurePosixPath(PROFILE_CATALOG).parts)
    source_models = plugin_repository / Path(*PurePosixPath(MODEL_MANIFEST).parts)
    if profile_path.read_bytes() != source_profile.read_bytes():
        _fail("artifacts.profileCatalog", "differs from pinned plugin source")
    if model_path.read_bytes() != source_models.read_bytes():
        _fail("artifacts.modelManifest", "differs from pinned plugin source")
    expected_substitutions = _control_rows(
        plugin_repository, disposable_project, backup_root, apply=False
    )
    if receipt.get("controlPlaneSubstitutions") != expected_substitutions:
        _fail("controlPlaneSubstitutions", "does not match staged control bytes")
    expected_renames = [
        {
            "from": relative,
            "to": f"checkout-roots/{relative}",
            "restorable": True,
        }
        for relative in SOURCE_ROOTS
    ]
    if receipt.get("renamedPaths") != expected_renames:
        _fail("renamedPaths", "does not bind each preserved source root")

    raw = _read_json(raw_path)
    generation = _validate_current_matrix(
        raw,
        project_revision=project_revision,
        project_root=disposable_project,
        eval_ids={str(case["id"]) for case in final_cases},
        eval_fingerprint=eval_fingerprint,
    )
    report_value = raw.get("reportPath")
    if not isinstance(report_value, str) or not report_value:
        _fail("rawBenchmark.reportPath", "must be a non-empty path")
    try:
        cache_report = Path(report_value).resolve(strict=True)
    except OSError as error:
        raise StagingError(f"rawBenchmark.reportPath cannot be resolved: {error}") from error
    try:
        cache_report.relative_to(cache_root.resolve(strict=True))
    except ValueError:
        _fail("rawBenchmark.reportPath", "escapes disposable cache")
    if cache_report.read_bytes() != raw_path.read_bytes():
        _fail("rawBenchmark.reportPath", "cache report differs from staged artifact")
    cache_pointer, cache_database = _read_generation_pointer(cache_root)
    if pointer_path.read_bytes() != (cache_root / "current.json").read_bytes():
        _fail("artifacts.currentGeneration", "differs from cache pointer")
    pointer = _read_json(pointer_path)
    if pointer != cache_pointer:
        _fail("artifacts.currentGeneration", "logical value differs from cache pointer")
    if database_path.read_bytes() != cache_database.read_bytes():
        _fail("artifacts.generationDatabase", "differs from leased generation database")
    for field in (
        "id",
        "corpusFingerprint",
        "modelFingerprint",
        "project",
        "worktree",
        "gitRevision",
        "dirtyFingerprint",
        "parserVersion",
        "chunkerVersion",
        "createdAt",
        "runtime",
        "documentCount",
        "chunkCount",
    ):
        if pointer.get(field) != generation.get(field):
            _fail(f"currentGeneration.{field}", "differs from benchmark generation")

    indexed = _read_json(indexed_path)
    indexed_proof = _verify_indexed_source_manifest(
        indexed,
        database=database_path,
        project=disposable_project,
        backup=backup_root,
        project_name=generation["project"],
        project_revision=project_revision,
        generation_id=generation["id"],
        corpus_fingerprint=generation["corpusFingerprint"],
    )
    if indexed_proof["sourceCount"] != generation["documentCount"]:
        _fail("indexedSourceProof.sourceCount", "differs from generation documentCount")
    if receipt.get("indexedSourceProof") != indexed_proof:
        _fail("indexedSourceProof", "differs from indexed-source manifest")

    tools = receipt.get("tools")
    expected_tools = {
        "harnessScriptSha256": _file_identity(script_path),
        "harnessSchemaSha256": _file_identity(schema_path),
        "pythonVersion": sys.version.splitlines()[0],
        "gitVersion": _tool_version(["git", "--version"], cwd=plugin_repository),
        "goVersion": _tool_version([go_executable, "version"], cwd=plugin_repository),
    }
    if tools != expected_tools:
        _fail("tools", "tool identities differ from the staging environment")
    benchmark_command = receipt.get("benchmarkCommand")
    if not isinstance(benchmark_command, dict):
        _fail("benchmarkCommand", "must be an object")
    expected_command = _semantic_benchmark_command(go_executable)
    for field, expected in expected_command.items():
        if benchmark_command.get(field) != expected:
            _fail(f"benchmarkCommand.{field}", "does not match canonical invocation")
    for field in ("exitCode", "stdoutSha256", "stderrSha256"):
        if field not in benchmark_command:
            _fail(f"benchmarkCommand.{field}", "is missing")
    expected_exit = 0 if raw.get("passed") is True else 1
    if benchmark_command["exitCode"] != expected_exit:
        _fail("benchmarkCommand.exitCode", "does not match raw pass state")
    for field in ("stdoutSha256", "stderrSha256"):
        if not IDENTITY_RE.fullmatch(str(benchmark_command[field])):
            _fail(f"benchmarkCommand.{field}", "must be a digest")

    runtime_identity = generation["runtime"]
    expected_runtime = {
        "runId": raw["runId"],
        "generationId": generation["id"],
        "corpusFingerprint": generation["corpusFingerprint"],
        "evalFingerprint": raw["evalFingerprint"],
        "projectGitRevision": generation["gitRevision"],
        "runtimeIdentity": runtime_identity,
        "runtimeIdentitySha256": _identity(_go_json_bytes(runtime_identity)),
        "profileCatalogSha256": _file_identity(profile_path),
        "modelManifestSha256": _file_identity(model_path),
        "armCount": 2,
        "casesPerArm": 64,
    }
    if receipt.get("runtime") != expected_runtime:
        _fail("runtime", "does not bind the staged runtime and corpus")
    if receipt.get("project") != generation["project"]:
        _fail("project", "differs from benchmark generation")
    if receipt.get("negativeControls") != _negative_controls(
        projection,
        indexed,
        dirty_repository=disposable_project,
        dirty_revision=project_revision,
    ):
        _fail("negativeControls", "are not the deterministic tamper controls")
    return receipt


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plugin-repository", type=Path, required=True)
    parser.add_argument("--plugin-revision", required=True)
    parser.add_argument("--project-repository", type=Path, required=True)
    parser.add_argument("--project-revision", required=True)
    parser.add_argument("--output-root", type=Path, required=True)
    parser.add_argument("--go-executable", default="go")
    parser.add_argument("--timeout-seconds", type=int, default=1800)
    parser.add_argument(
        "--verify",
        action="store_true",
        help="verify an existing staging output without rerunning the benchmark",
    )
    args = parser.parse_args()
    if args.verify:
        verify_stage(
            plugin_repository=args.plugin_repository,
            plugin_revision=args.plugin_revision,
            project_repository=args.project_repository,
            project_revision=args.project_revision,
            output_root=args.output_root,
            go_executable=args.go_executable,
        )
        print(
            "verified project lane-ablation staging receipt: "
            + str(args.output_root / "artifacts" / "harness-receipt.json")
        )
        return 0
    stage(
        plugin_repository=args.plugin_repository,
        plugin_revision=args.plugin_revision,
        project_repository=args.project_repository,
        project_revision=args.project_revision,
        output_root=args.output_root,
        go_executable=args.go_executable,
        timeout_seconds=args.timeout_seconds,
    )
    print(
        "wrote verified project lane-ablation staging receipt: "
        + str(args.output_root / "artifacts" / "harness-receipt.json")
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
