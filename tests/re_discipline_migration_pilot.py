"""Run digest-bound re-discipline 0.8 migrations in disposable Git clones.

The harness never invokes the migrator against either source checkout.  It
accepts only clean, full Git revisions, clones both repositories, synchronizes
the exact clean working-tree bytes into the clones, and records every command
and result.  Human judgment remains outside the harness: preview conflicts,
live-report coverage, gate evidence, and final ratification are explicit pause
points with digest-bound review packets.

Only the Python standard library is required.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import platform
import re
import shutil
import stat
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_project_lane_ablation import validate_json_schema


class PilotError(ValueError):
    """Raised when a pilot cannot prove an input, transition, or artifact."""


REVISION_RE = re.compile(r"^[0-9a-f]{40}$")
DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
PAIR_DIGEST_ALGORITHM = "sorted-path-null-sha256-v1"
PILOT_KIND = "re-discipline-disposable-migration-pilot-v1"
PILOT_SCHEMA = (
    "plugins/re-discipline/knowledge/schemas/"
    "disposable-migration-pilot.schema.json"
)
PLUGIN_ROOT = "plugins/re-discipline"
ASSET_ROOT = f"{PLUGIN_ROOT}/knowledge"
PACKAGE_MANIFEST = f"{ASSET_ROOT}/bin/manifest.json"
PACKAGE_SUMS = f"{ASSET_ROOT}/bin/SHA256SUMS"
MIGRATION_ROOT = ".re-discipline/migration/0.8"
CANONICAL_STATE_HEAD = ".re-discipline/state/head.json"
LEGACY_MARKERS = {
    ".re-discipline/project-profile.md": "re-discipline:shared-laws v0.7.0",
    ".codex/AGENTS.md": "re-discipline:codex-adapter v0.7.0",
}
PILOT_CAMPAIGNS = {
    "small": "prelude-pack-recalibration",
    "scale": "resource-registration",
}
REVIEW_ROOT = "manager-input"
ARTIFACT_ROOT = "artifacts"
CAPTURE_ROOT = f"{ARTIFACT_ROOT}/commands"


def _fail(path: str, message: str) -> None:
    raise PilotError(f"{path}: {message}")


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace(
        "+00:00", "Z"
    )


def _identity(body: bytes) -> str:
    return "sha256:" + hashlib.sha256(body).hexdigest()


def _canonical_bytes(value: Any) -> bytes:
    return json.dumps(
        value, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")


def _canonical_digest(value: Any) -> str:
    return _identity(_canonical_bytes(value))


def _stable_json_bytes(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def _strict_json_bytes(body: bytes, label: str) -> Any:
    try:
        text = body.decode("utf-8", errors="strict")
        decoder = json.JSONDecoder()
        value, end = decoder.raw_decode(text)
        if text[end:].strip():
            _fail(label, "contains a trailing JSON value")
        return value
    except (UnicodeError, json.JSONDecodeError) as error:
        raise PilotError(f"{label}: invalid strict UTF-8 JSON: {error}") from error


def _read_json(path: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        _fail(str(path), "must be a regular non-link JSON file")
    value = _strict_json_bytes(path.read_bytes(), str(path))
    if not isinstance(value, dict):
        _fail(str(path), "top-level JSON value must be an object")
    return value


def _write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_bytes(_stable_json_bytes(value))
    temporary.replace(path)


def _safe_relative(value: str, *, field: str) -> PurePosixPath:
    if not isinstance(value, str) or not value or "\\" in value:
        _fail(field, "must be a non-empty forward-slash relative path")
    relative = PurePosixPath(value)
    if (
        relative.is_absolute()
        or any(part in ("", ".", "..") for part in relative.parts)
        or re.match(r"^[A-Za-z]:", value)
    ):
        _fail(field, "must be a canonical contained relative path")
    return relative


def _contained(root: Path, relative: str, *, field: str) -> Path:
    rel = _safe_relative(relative, field=field)
    resolved_root = root.resolve(strict=True)
    candidate = resolved_root / Path(*rel.parts)
    resolved_candidate = candidate.resolve(strict=False)
    try:
        resolved_candidate.relative_to(resolved_root)
    except ValueError:
        _fail(field, "escapes its declared root")
    return candidate


def _is_link_or_reparse(path: Path) -> bool:
    if path.is_symlink():
        return True
    try:
        attributes = getattr(path.lstat(), "st_file_attributes", 0)
    except OSError:
        return False
    return bool(attributes & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0x400))


def _require_real_parent_chain(root: Path, parent: Path, *, field: str) -> None:
    resolved_root = root.resolve(strict=True)
    try:
        relative = parent.relative_to(resolved_root)
        parent.resolve(strict=False).relative_to(resolved_root)
    except ValueError:
        _fail(field, "parent escapes its declared root")
    current = resolved_root
    for part in relative.parts:
        current = current / part
        if not current.exists():
            continue
        if _is_link_or_reparse(current) or not current.is_dir():
            _fail(field, f"parent component {current} is not a real directory")


def _same_path(left: Path, right: Path) -> bool:
    return os.path.normcase(str(left.resolve(strict=False))) == os.path.normcase(
        str(right.resolve(strict=False))
    )


def _overlaps(left: Path, right: Path) -> bool:
    left = left.resolve(strict=False)
    right = right.resolve(strict=False)
    try:
        left.relative_to(right)
        return True
    except ValueError:
        pass
    try:
        right.relative_to(left)
        return True
    except ValueError:
        return False


def _run_bytes(
    command: Sequence[str],
    *,
    cwd: Path,
    timeout_seconds: int = 300,
) -> subprocess.CompletedProcess[bytes]:
    try:
        return subprocess.run(
            list(command),
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired as error:
        raise PilotError(f"command {list(command)!r} timed out: {error}") from error
    except OSError as error:
        raise PilotError(f"command {list(command)!r} could not start: {error}") from error


def _git(repo: Path, *arguments: str, timeout_seconds: int = 300) -> bytes:
    result = _run_bytes(
        ["git", "--no-optional-locks", "-C", str(repo), *arguments],
        cwd=repo,
        timeout_seconds=timeout_seconds,
    )
    if result.returncode != 0:
        message = result.stderr.decode("utf-8", errors="replace").strip()
        raise PilotError(
            f"git -C {repo} {' '.join(arguments)} failed "
            f"({result.returncode}): {message}"
        )
    return result.stdout


def _pair_digest(rows: Iterable[tuple[str, str]]) -> str:
    digest = hashlib.sha256()
    prior = ""
    seen: set[str] = set()
    for path, identity in sorted(rows):
        _safe_relative(path, field="manifest.path")
        if path in seen or (prior and path <= prior):
            _fail("manifest.path", "paths must be unique after canonical sorting")
        if not DIGEST_RE.fullmatch(identity):
            _fail("manifest.sha256", "must be a sha256 identity")
        seen.add(path)
        prior = path
        digest.update(path.encode("utf-8"))
        digest.update(b"\x00")
        digest.update(identity.encode("ascii"))
        digest.update(b"\x00")
    return "sha256:" + digest.hexdigest()


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
            raise PilotError(f"cannot parse Git index row {entry!r}") from error
        if stage != "0" or mode not in {"100644", "100755"}:
            _fail(
                "repository.index",
                f"{relative!r} has unsupported stage/mode {stage}/{mode}",
            )
        _safe_relative(relative, field="repository.index.path")
        if relative in seen:
            _fail("repository.index", f"duplicate tracked path {relative!r}")
        seen.add(relative)
        absolute = repo / Path(*PurePosixPath(relative).parts)
        info = absolute.lstat()
        if not stat.S_ISREG(info.st_mode) or absolute.is_symlink():
            _fail("repository.index", f"{relative!r} is not a regular file")
        rows.append((relative, _identity(absolute.read_bytes())))
    rows.sort()
    return rows


def _repo_binding(repo: Path, revision: str, *, label: str) -> dict[str, Any]:
    if not REVISION_RE.fullmatch(revision):
        _fail(f"{label}.revision", "must be a full lowercase 40-character Git commit")
    if repo.is_symlink() or not repo.is_dir():
        _fail(label, "repository root must be a real directory")
    repo = repo.resolve(strict=True)
    discovered = Path(
        _git(repo, "rev-parse", "--show-toplevel")
        .decode("utf-8", errors="strict")
        .strip()
    )
    if not _same_path(repo, discovered):
        _fail(label, f"{repo} is not the exact worktree root {discovered}")
    head = _git(repo, "rev-parse", "HEAD").decode("ascii").strip()
    if head != revision:
        _fail(f"{label}.revision", f"HEAD is {head}, expected {revision}")
    status = _git(
        repo, "status", "--porcelain=v1", "-z", "--untracked-files=all"
    )
    if status:
        _fail(label, "repository is dirty (tracked or untracked files are present)")
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
    if current != expected:
        _fail(label, "source repository changed during the pilot")


def _clone_exact(
    source: Path,
    revision: str,
    destination: Path,
    *,
    timeout_seconds: int,
) -> dict[str, Any]:
    if destination.exists() or destination.is_symlink():
        _fail("clone.destination", f"refuses existing path {destination}")
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
    _git(destination, "checkout", "--detach", revision)
    if _git(destination, "rev-parse", "HEAD").decode("ascii").strip() != revision:
        _fail("clone.revision", "detached clone did not select the requested commit")
    source_rows = dict(_tracked_rows(source))
    clone_rows = dict(_tracked_rows(destination))
    if set(source_rows) != set(clone_rows):
        _fail("clone.inventory", "clone tracked paths differ from source")
    # Clone-local line-ending filters can differ.  The clean source working
    # bytes are the actual migration input, so reproduce them byte-for-byte.
    for relative, identity in source_rows.items():
        source_path = source / Path(*PurePosixPath(relative).parts)
        clone_path = destination / Path(*PurePosixPath(relative).parts)
        clone_path.write_bytes(source_path.read_bytes())
        if _identity(clone_path.read_bytes()) != identity:
            _fail("clone.bytes", f"could not reproduce {relative}")
    copied_rows = _tracked_rows(destination)
    if copied_rows != sorted(source_rows.items()):
        _fail("clone.bytes", "disposable working bytes do not equal source bytes")
    return {
        "revision": revision,
        "tree": _git(destination, "rev-parse", "HEAD^{tree}")
        .decode("ascii")
        .strip(),
        "trackedFileCount": len(copied_rows),
        "trackedManifestSha256": _pair_digest(copied_rows),
        "sourceWorkingBytesExact": True,
    }


def _production_guard(project: Path, binding: dict[str, Any]) -> dict[str, Any]:
    markers: list[dict[str, Any]] = []
    for relative, marker in LEGACY_MARKERS.items():
        path = _contained(project, relative, field="productionGuard.marker")
        if path.is_symlink() or not path.is_file():
            _fail("productionGuard", f"legacy marker file {relative} is absent or unsafe")
        body = path.read_bytes()
        text = body.decode("utf-8", errors="strict")
        if marker not in text:
            _fail("productionGuard", f"{relative} lacks {marker!r}")
        markers.append(
            {"path": relative, "marker": marker, "sha256": _identity(body)}
        )
    absent = [MIGRATION_ROOT, CANONICAL_STATE_HEAD]
    for relative in absent:
        path = project / Path(*PurePosixPath(relative).parts)
        if path.exists() or path.is_symlink():
            _fail(
                "productionGuard",
                f"source checkout already contains migration state {relative}",
            )
    return {
        "projectRevision": binding["revision"],
        "projectTree": binding["tree"],
        "trackedManifestSha256": binding["trackedManifestSha256"],
        "legacyMarkers": markers,
        "absentPaths": absent,
        "legacyStateConfirmed": True,
    }


def _platform_target() -> tuple[str, str, str]:
    systems = {"windows": "windows", "linux": "linux", "darwin": "darwin"}
    goos = systems.get(platform.system().lower())
    machine = platform.machine().lower()
    if machine in {"amd64", "x86_64"}:
        goarch = "amd64"
    elif machine in {"arm64", "aarch64"}:
        goarch = "arm64"
    else:
        goarch = ""
    if not goos or not goarch:
        _fail("runtime.platform", f"unsupported host {platform.system()}/{machine}")
    executable = "re-discipline-knowledge.exe" if goos == "windows" else "re-discipline-knowledge"
    return goos, goarch, executable


def _package_entry_path(
    plugin: Path, entry: dict[str, Any], *, section: str
) -> Path:
    relative = entry.get("path")
    if not isinstance(relative, str):
        _fail(f"package.{section}.path", "must be a string")
    if section in {"targets", "launchers", "notices"}:
        base = f"{ASSET_ROOT}/bin"
    else:
        base = ASSET_ROOT
    combined = PurePosixPath(base) / _safe_relative(
        relative, field=f"package.{section}.path"
    )
    return _contained(plugin, combined.as_posix(), field=f"package.{section}.path")


def _verify_package(plugin: Path) -> dict[str, Any]:
    # Contained paths are built on the resolved root, so every relative
    # projection below must use the same resolved form; an unresolved
    # temporary directory differs through macOS /var symlinks and Windows
    # 8.3 short names.
    plugin = plugin.resolve(strict=True)
    manifest_path = _contained(plugin, PACKAGE_MANIFEST, field="package.manifest")
    sums_path = _contained(plugin, PACKAGE_SUMS, field="package.sums")
    manifest = _read_json(manifest_path)
    runtime = manifest.get("runtime")
    if manifest.get("schemaVersion") != 1 or not isinstance(runtime, dict):
        _fail("package.manifest", "has an unsupported schema or runtime identity")
    # The 0.8 project-state migrator remains supported by the 0.8 and 0.9
    # plugin lines. Pinning a patch level here rejects every subsequent release.
    runtime_version = runtime.get("version")
    if runtime.get("name") != "re-discipline-knowledge" or not (
        isinstance(runtime_version, str)
        and re.fullmatch(r"0\.(?:8|9)\.\d+", runtime_version)
    ):
        _fail("package.runtime", "does not support the re-discipline 0.8 state runtime")
    build_id = runtime.get("buildId")
    if not isinstance(build_id, str) or not DIGEST_RE.fullmatch(build_id):
        _fail("package.runtime.buildId", "must be a sha256 identity")

    verified: dict[str, dict[str, Any]] = {}
    for section in ("targets", "launchers", "sharedAssets"):
        entries = manifest.get(section)
        if not isinstance(entries, list) or not entries:
            _fail(f"package.{section}", "must be a non-empty array")
        for index, entry in enumerate(entries):
            if not isinstance(entry, dict):
                _fail(f"package.{section}[{index}]", "must be an object")
            path = _package_entry_path(plugin, entry, section=section)
            if path.is_symlink() or not path.is_file():
                _fail(f"package.{section}[{index}]", "artifact is absent or unsafe")
            body = path.read_bytes()
            expected = entry.get("sha256")
            if not isinstance(expected, str) or _identity(body) != expected:
                _fail(f"package.{section}[{index}]", "artifact digest mismatches manifest")
            if entry.get("size") != len(body):
                _fail(f"package.{section}[{index}]", "artifact size mismatches manifest")
            relative_to_bin = Path(
                os.path.relpath(
                    path,
                    _contained(plugin, f"{ASSET_ROOT}/bin", field="package.bin"),
                )
            ).as_posix()
            verified[relative_to_bin] = {
                "path": path.relative_to(plugin).as_posix(),
                "sumPath": relative_to_bin,
                "sha256": expected,
                "byteCount": len(body),
                "section": section,
            }
    notices = manifest.get("notices")
    if not isinstance(notices, dict):
        _fail("package.notices", "must be an object")
    notice_path = _package_entry_path(plugin, notices, section="notices")
    notice_body = notice_path.read_bytes()
    if _identity(notice_body) != notices.get("sha256") or len(notice_body) != notices.get("size"):
        _fail("package.notices", "notice digest or size mismatches manifest")
    notice_relative = Path(
        os.path.relpath(
            notice_path,
            _contained(plugin, f"{ASSET_ROOT}/bin", field="package.bin"),
        )
    ).as_posix()
    verified[notice_relative] = {
        "path": notice_path.relative_to(plugin).as_posix(),
        "sumPath": notice_relative,
        "sha256": notices["sha256"],
        "byteCount": len(notice_body),
        "section": "notices",
    }

    if sums_path.is_symlink() or not sums_path.is_file():
        _fail("package.sums", "must be a regular non-link file")
    sums: dict[str, str] = {}
    for number, line in enumerate(
        sums_path.read_text(encoding="utf-8", errors="strict").splitlines(), start=1
    ):
        if not line:
            continue
        match = re.fullmatch(r"([0-9a-f]{64})  (.+)", line)
        if not match:
            _fail("package.sums", f"line {number} is malformed")
        relative = PurePosixPath(match.group(2)).as_posix()
        if relative in sums:
            _fail("package.sums", f"line {number} repeats {relative}")
        sums[relative] = "sha256:" + match.group(1)
    expected_sums = {path: row["sha256"] for path, row in verified.items()}
    # The checksum inventory also seals the manifest bytes themselves.
    expected_sums["manifest.json"] = "sha256:" + _identity(
        manifest_path.read_bytes()
    ).removeprefix("sha256:")
    if sums != expected_sums:
        missing = sorted(set(expected_sums) - set(sums))
        extra = sorted(set(sums) - set(expected_sums))
        changed = sorted(
            key for key in set(sums) & set(expected_sums) if sums[key] != expected_sums[key]
        )
        _fail(
            "package.sums",
            f"does not equal manifest inventory (missing={missing}, extra={extra}, changed={changed})",
        )

    goos, goarch, executable = _platform_target()
    selected: dict[str, Any] | None = None
    for entry in manifest["targets"]:
        if entry.get("goos") == goos and entry.get("goarch") == goarch:
            selected = entry
            break
    if selected is None:
        _fail("package.runtime", f"manifest lacks {goos}-{goarch}")
    runtime_path = _package_entry_path(plugin, selected, section="targets")
    if runtime_path.name != executable:
        _fail("package.runtime", "selected target has the wrong executable name")
    if os.name != "nt":
        runtime_path.chmod(runtime_path.stat().st_mode | stat.S_IXUSR)
    return {
        "manifestPath": PACKAGE_MANIFEST,
        "manifestSha256": _identity(manifest_path.read_bytes()),
        "checksumsPath": PACKAGE_SUMS,
        "checksumsSha256": _identity(sums_path.read_bytes()),
        "runtimeName": runtime["name"],
        "runtimeVersion": runtime["version"],
        "buildId": build_id,
        "target": f"{goos}-{goarch}",
        "runtimePath": runtime_path.relative_to(plugin).as_posix(),
        "runtimeSha256": _identity(runtime_path.read_bytes()),
        "packageArtifactCount": len(verified),
        "packageInventorySha256": _pair_digest(
            (row["path"], row["sha256"]) for row in verified.values()
        ),
    }


def _portable_token(value: str, roots: dict[str, Path]) -> str:
    candidate = Path(value)
    for marker, root in roots.items():
        try:
            relative = candidate.resolve(strict=False).relative_to(root.resolve(strict=False))
        except (OSError, ValueError):
            continue
        suffix = relative.as_posix()
        return marker if suffix == "." else f"{marker}/{suffix}"
    normalized = value.replace("\\", "/")
    for marker, root in roots.items():
        root_text = str(root).replace("\\", "/")
        if normalized == root_text:
            return marker
        if normalized.startswith(root_text.rstrip("/") + "/"):
            return marker + normalized[len(root_text) :]
    return value


class CaptureLog:
    def __init__(
        self,
        run_root: Path,
        *,
        plugin: Path,
        project: Path,
        timeout_seconds: int,
    ) -> None:
        self.run_root = run_root
        self.capture_root = run_root / CAPTURE_ROOT
        self.capture_root.mkdir(parents=True, exist_ok=True)
        self.timeout_seconds = timeout_seconds
        self.sequence = len(list(self.capture_root.glob("*.json")))
        self.roots = {
            "$RUN": run_root,
            "$PLUGIN": plugin,
            "$PROJECT": project,
        }

    def run(
        self,
        *,
        name: str,
        command: Sequence[str],
        cwd: Path,
        request: Path | None = None,
        expect_success: bool = True,
        failure_class: str = "",
        timeout_seconds: int | None = None,
    ) -> tuple[dict[str, Any], bytes, bytes]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name):
            _fail("capture.name", f"invalid capture name {name!r}")
        self.sequence += 1
        prefix = f"{self.sequence:03d}-{name}"
        stdout_path = self.capture_root / f"{prefix}.stdout"
        stderr_path = self.capture_root / f"{prefix}.stderr"
        started = _utc_now()
        start_clock = time.monotonic()
        launch_failure = ""
        timed_out = False
        try:
            result = subprocess.run(
                list(command),
                cwd=cwd,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
                timeout=timeout_seconds or self.timeout_seconds,
            )
            exit_code = result.returncode
            stdout = result.stdout
            stderr = result.stderr
        except subprocess.TimeoutExpired as error:
            exit_code = 124
            stdout = error.stdout or b""
            stderr = error.stderr or b""
            launch_failure = str(error)
            timed_out = True
        except OSError as error:
            exit_code = 127
            stdout = b""
            stderr = str(error).encode("utf-8", errors="replace")
            launch_failure = str(error)
        duration_ms = int((time.monotonic() - start_clock) * 1000)
        stdout_path.write_bytes(stdout)
        stderr_path.write_bytes(stderr)
        request_ref: dict[str, Any] | None = None
        if request is not None:
            if request.is_symlink() or not request.is_file():
                _fail("capture.request", "request must be a regular non-link file")
            request_ref = _artifact_ref(request, self.run_root)
        portable_command = [
            _portable_token(str(token), self.roots) for token in command
        ]
        failure = None
        if exit_code != 0:
            failure = {
                "class": failure_class or ("timeout" if timed_out else "command-failed"),
                "messageSha256": _identity(stderr),
                "launchFailure": bool(launch_failure),
            }
        capture: dict[str, Any] = {
            "schemaVersion": 1,
            "sequence": self.sequence,
            "name": name,
            "startedAt": started,
            "finishedAt": _utc_now(),
            "durationMs": duration_ms,
            "cwd": _portable_token(str(cwd), self.roots),
            "command": portable_command,
            "commandSha256": _canonical_digest(portable_command),
            "request": request_ref,
            "exitCode": exit_code,
            "stdout": _artifact_ref(stdout_path, self.run_root),
            "stderr": _artifact_ref(stderr_path, self.run_root),
            "failure": failure,
            "digest": "",
        }
        capture["digest"] = _canonical_digest(capture)
        _write_json(self.capture_root / f"{prefix}.json", capture)
        if expect_success and exit_code != 0:
            message = stderr.decode("utf-8", errors="replace").strip()
            _fail(f"command.{name}", f"failed ({exit_code}): {message}")
        if not expect_success and exit_code == 0:
            _fail(f"command.{name}", "negative control unexpectedly succeeded")
        return capture, stdout, stderr


def _artifact_ref(path: Path, run_root: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        _fail(str(path), "artifact must be a regular non-link file")
    try:
        relative = path.resolve(strict=True).relative_to(run_root.resolve(strict=True))
    except ValueError:
        _fail(str(path), "artifact is outside the pilot root")
    body = path.read_bytes()
    return {
        "path": relative.as_posix(),
        "sha256": _identity(body),
        "byteCount": len(body),
    }


def _copy_artifact(source: Path, destination: Path) -> dict[str, Any]:
    if source.is_symlink() or not source.is_file():
        _fail(str(source), "source artifact must be a regular non-link file")
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists() or destination.is_symlink():
        _fail(str(destination), "refuses to overwrite an existing artifact")
    shutil.copyfile(source, destination)
    if destination.read_bytes() != source.read_bytes():
        _fail(str(destination), "copied artifact differs from source")
    return {"sha256": _identity(destination.read_bytes()), "byteCount": destination.stat().st_size}


def _json_stdout(stdout: bytes, label: str) -> dict[str, Any]:
    value = _strict_json_bytes(stdout, label)
    if not isinstance(value, dict):
        _fail(label, "result must be a JSON object")
    return value


def _migration_command(runtime: Path, plugin_clone: Path, *arguments: str) -> list[str]:
    return [
        str(runtime),
        "migrate-project",
        "--asset-root",
        str(_contained(plugin_clone, ASSET_ROOT, field="migration.assetRoot")),
        *arguments,
    ]


def _receipt_path(run_root: Path) -> Path:
    return run_root / ARTIFACT_ROOT / "pilot-receipt.json"


def _seal_receipt(
    *,
    run_root: Path,
    plugin_source: Path,
    receipt: dict[str, Any],
) -> dict[str, Any]:
    history = run_root / ARTIFACT_ROOT / "receipts"
    history.mkdir(parents=True, exist_ok=True)
    current = _receipt_path(run_root)
    prior_digest = ""
    prior_sequence = 0
    if current.exists():
        prior = _read_json(current)
        prior_digest = str(prior.get("receiptDigest", ""))
        if not DIGEST_RE.fullmatch(prior_digest):
            _fail("receipt.prior", "current receipt is unsealed")
        prior_sequence = int(prior.get("receiptSequence", 0))
        archive = history / f"{prior_sequence:03d}-{prior.get('phase', 'unknown')}.json"
        if archive.exists() or archive.is_symlink():
            _fail("receipt.history", f"refuses to overwrite {archive.name}")
        shutil.copyfile(current, archive)
        if _identity(archive.read_bytes()) != _identity(current.read_bytes()):
            _fail("receipt.history", "archived receipt copy mismatched")
    receipt = copy.deepcopy(receipt)
    receipt["$schema"] = "plugin://re-discipline/schemas/disposable-migration-pilot.schema.json"
    receipt["schemaVersion"] = 1
    receipt["kind"] = PILOT_KIND
    receipt["receiptSequence"] = prior_sequence + 1
    receipt["priorReceiptDigest"] = prior_digest
    receipt["updatedAt"] = _utc_now()
    receipt["receiptDigest"] = ""
    receipt["receiptDigest"] = _canonical_digest(receipt)
    schema = _read_json(_contained(plugin_source, PILOT_SCHEMA, field="receipt.schema"))
    validate_json_schema(schema, receipt)
    _write_json(current, receipt)
    if _read_json(current).get("receiptDigest") != receipt["receiptDigest"]:
        _fail("receipt", "written receipt does not match sealed value")
    return receipt


def _verify_receipt_digest(receipt: dict[str, Any]) -> None:
    expected = receipt.get("receiptDigest")
    if not isinstance(expected, str) or not DIGEST_RE.fullmatch(expected):
        _fail("receipt.receiptDigest", "must be a sha256 identity")
    candidate = copy.deepcopy(receipt)
    candidate["receiptDigest"] = ""
    if _canonical_digest(candidate) != expected:
        _fail("receipt.receiptDigest", "does not authenticate the receipt")


def _command_refs(run_root: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for path in sorted((run_root / CAPTURE_ROOT).glob("*.json")):
        capture = _read_json(path)
        expected = capture.get("digest")
        candidate = copy.deepcopy(capture)
        candidate["digest"] = ""
        if not isinstance(expected, str) or _canonical_digest(candidate) != expected:
            _fail("commands", f"capture {path.name} digest mismatches")
        for key in ("stdout", "stderr"):
            _verify_artifact_ref(run_root, capture[key], field=f"commands.{path.name}.{key}")
        if capture.get("request") is not None:
            _verify_artifact_ref(
                run_root, capture["request"], field=f"commands.{path.name}.request"
            )
        rows.append(_artifact_ref(path, run_root))
    return rows


def _verify_artifact_ref(run_root: Path, reference: Any, *, field: str) -> Path:
    if not isinstance(reference, dict):
        _fail(field, "must be an artifact reference")
    path_value = reference.get("path")
    if not isinstance(path_value, str):
        _fail(field, "path must be a string")
    path = _contained(run_root, path_value, field=f"{field}.path")
    if path.is_symlink() or not path.is_file():
        _fail(field, "artifact is absent or unsafe")
    body = path.read_bytes()
    if reference.get("sha256") != _identity(body) or reference.get("byteCount") != len(body):
        _fail(field, "artifact digest or byte count mismatches")
    return path


def _write_packet(
    *,
    run_root: Path,
    relative: str,
    value: dict[str, Any],
) -> dict[str, Any]:
    path = _contained(run_root, relative, field="packet.path")
    if path.exists() or path.is_symlink():
        _fail("packet.path", f"refuses to overwrite {relative}")
    sealed = copy.deepcopy(value)
    sealed["digest"] = ""
    sealed["digest"] = _canonical_digest(sealed)
    _write_json(path, sealed)
    return _artifact_ref(path, run_root)


def _prepare(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    if pilot not in PILOT_CAMPAIGNS:
        _fail("pilot", f"unsupported pilot {pilot!r}")
    plugin_source = plugin_source.resolve(strict=True)
    project_source = project_source.resolve(strict=True)
    run_root = run_root.resolve(strict=False)
    if run_root.exists() or run_root.is_symlink():
        _fail("output", "prepare requires an absent output directory")
    if _overlaps(run_root, plugin_source) or _overlaps(run_root, project_source):
        _fail("output", "pilot output may not overlap either source repository")
    plugin_before = _repo_binding(plugin_source, plugin_revision, label="pluginRepository")
    project_before = _repo_binding(project_source, project_revision, label="projectRepository")
    production_before = _production_guard(project_source, project_before)

    run_root.mkdir(parents=True)
    disposable = run_root / "disposable"
    plugin_clone = disposable / "plugin"
    project_clone = disposable / "project"
    plugin_copy = _clone_exact(
        plugin_source,
        plugin_revision,
        plugin_clone,
        timeout_seconds=timeout_seconds,
    )
    project_copy = _clone_exact(
        project_source,
        project_revision,
        project_clone,
        timeout_seconds=timeout_seconds,
    )
    if plugin_copy["trackedManifestSha256"] != plugin_before["trackedManifestSha256"]:
        _fail("clone.plugin", "tracked working bytes are incomplete")
    if project_copy["trackedManifestSha256"] != project_before["trackedManifestSha256"]:
        _fail("clone.project", "tracked working bytes are incomplete")
    _production_guard(project_clone, project_before)
    package = _verify_package(plugin_clone)
    runtime = _contained(plugin_clone, package["runtimePath"], field="runtime.path")
    campaign = PILOT_CAMPAIGNS[pilot]
    logger = CaptureLog(
        run_root,
        plugin=plugin_clone,
        project=project_clone,
        timeout_seconds=timeout_seconds,
    )

    helper = run_root / ARTIFACT_ROOT / "tools" / (
        "re-discipline-migration-pilot-helper.exe"
        if os.name == "nt"
        else "re-discipline-migration-pilot-helper"
    )
    helper.parent.mkdir(parents=True, exist_ok=True)
    build_capture, _, _ = logger.run(
        name="build-pilot-helper",
        command=[
            "go",
            "build",
            "-trimpath",
            "-o",
            str(helper),
            "./cmd/re-discipline-migration-pilot-helper",
        ],
        cwd=_contained(plugin_clone, f"{ASSET_ROOT}", field="helper.source"),
        timeout_seconds=max(timeout_seconds, 600),
    )
    if helper.is_symlink() or not helper.is_file():
        _fail("helper", "build did not produce a regular executable")
    if os.name != "nt":
        helper.chmod(helper.stat().st_mode | stat.S_IXUSR)
    go_version = _run_bytes(["go", "version"], cwd=plugin_clone, timeout_seconds=30)
    if go_version.returncode != 0:
        _fail("tools.go", "go version failed")

    capture, stdout, _ = logger.run(
        name="project-version-before",
        command=[str(runtime), "project-version", "--project-root", str(project_clone)],
        cwd=plugin_clone,
    )
    version = _json_stdout(stdout, "project-version-before")
    if version.get("projectStateVersion") != "0.7" or version.get("migrationRequired") is not True:
        _fail("projectVersion", "disposable checkout is not a legacy 0.7 project")

    malformed = run_root / ARTIFACT_ROOT / "injections" / "malformed-review.json"
    malformed.parent.mkdir(parents=True, exist_ok=True)
    malformed.write_bytes(b'{"schemaVersion":1,"unexpected":')
    malformed_capture, _, _ = logger.run(
        name="malformed-input-refusal",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--truth-review",
            str(malformed),
        ),
        cwd=plugin_clone,
        request=malformed,
        expect_success=False,
        failure_class="malformed-input",
    )
    _production_guard(project_clone, project_before)

    missing_mcp = run_root / ARTIFACT_ROOT / "injections" / "missing-mcp-runtime"
    if os.name == "nt":
        missing_mcp = missing_mcp.with_suffix(".exe")
    outage_capture, _, _ = logger.run(
        name="mcp-outage",
        command=[str(missing_mcp), "serve"],
        cwd=project_clone,
        expect_success=False,
        failure_class="mcp-unavailable",
    )
    fallback_capture, fallback_stdout, _ = logger.run(
        name="real-cli-fallback",
        command=[str(runtime), "project-version", "--project-root", str(project_clone)],
        cwd=plugin_clone,
    )
    fallback = _json_stdout(fallback_stdout, "real-cli-fallback")
    if fallback != version:
        _fail("fallback", "real CLI fallback disagrees with the initial project version")

    preview_output = run_root / ARTIFACT_ROOT / "preview" / "initial"
    preview_capture, preview_stdout, _ = logger.run(
        name="preview-initial",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--preview",
            "--live-campaigns",
            campaign,
            "--output",
            str(preview_output),
        ),
        cwd=plugin_clone,
    )
    preview = _json_stdout(preview_stdout, "preview-initial")
    preview_path = run_root / ARTIFACT_ROOT / "preview" / "initial-result.json"
    _write_json(preview_path, preview)
    plan = preview.get("plan")
    if not isinstance(plan, dict):
        _fail("preview.plan", "must be an object")
    if plan.get("detectedVersion") != "0.7" or plan.get("liveCampaigns") != [campaign]:
        _fail("preview.plan", "did not bind the requested 0.7 live campaign")
    plan_digest = plan.get("planDigest")
    source_fingerprint = plan.get("sourceFingerprint")
    if not isinstance(plan_digest, str) or not DIGEST_RE.fullmatch(plan_digest):
        _fail("preview.planDigest", "is missing or malformed")
    if not isinstance(source_fingerprint, str) or not DIGEST_RE.fullmatch(source_fingerprint):
        _fail("preview.sourceFingerprint", "is missing or malformed")
    sources = plan.get("sources")
    if not isinstance(sources, list) or not sources:
        _fail("preview.sources", "must inventory the complete managed tree")
    tracked_paths = {path for path, _ in _tracked_rows(project_clone)}
    for index, source in enumerate(sources):
        if not isinstance(source, dict) or source.get("path") not in tracked_paths:
            _fail("preview.sources", f"row {index} is not bound to a tracked source")
    if _tracked_rows(project_clone) != _tracked_rows(project_source):
        _fail("preview.readOnly", "preview changed disposable tracked bytes")
    _production_guard(project_clone, project_before)

    conflicts = plan.get("conflicts")
    if not isinstance(conflicts, list):
        _fail("preview.conflicts", "must be an array")
    blocking_codes = sorted(
        {
            str(conflict.get("code"))
            for conflict in conflicts
            if isinstance(conflict, dict) and conflict.get("blocks") is True
        }
    )
    allowed_review_codes = {"unsupported-retrieval-profile"}
    truth_review_needed = any(code.startswith("truth-") for code in blocking_codes)
    if truth_review_needed:
        allowed_review_codes.update(code for code in blocking_codes if code.startswith("truth-"))
    unexpected = sorted(set(blocking_codes) - allowed_review_codes)
    if unexpected:
        _fail("preview.conflicts", f"has non-reviewable blockers {unexpected}")

    packet_refs: dict[str, Any] = {}
    if "unsupported-retrieval-profile" in blocking_codes:
        packet_capture, packet_stdout, _ = logger.run(
            name="profile-conflict-export",
            command=_migration_command(
                runtime,
                plugin_clone,
                "--project",
                str(project_clone),
                "--profile-conflict",
            ),
            cwd=plugin_clone,
        )
        profile_packet = _json_stdout(packet_stdout, "profile-conflict-export")
        packet_path = run_root / ARTIFACT_ROOT / "review" / "profile-conflict.json"
        _write_json(packet_path, profile_packet)
        packet_refs["profileConflict"] = _artifact_ref(packet_path, run_root)
        packet_refs["profileConflictCapture"] = _artifact_ref(
            run_root / CAPTURE_ROOT / f"{packet_capture['sequence']:03d}-profile-conflict-export.json",
            run_root,
        )
    if truth_review_needed:
        packet_capture, packet_stdout, _ = logger.run(
            name="truth-conflicts-export",
            command=_migration_command(
                runtime,
                plugin_clone,
                "--project",
                str(project_clone),
                "--truth-conflicts",
            ),
            cwd=plugin_clone,
        )
        truth_packet = _json_stdout(packet_stdout, "truth-conflicts-export")
        packet_path = run_root / ARTIFACT_ROOT / "review" / "truth-conflicts.json"
        _write_json(packet_path, truth_packet)
        packet_refs["truthConflicts"] = _artifact_ref(packet_path, run_root)
        packet_refs["truthConflictsCapture"] = _artifact_ref(
            run_root / CAPTURE_ROOT / f"{packet_capture['sequence']:03d}-truth-conflicts-export.json",
            run_root,
        )

    request = {
        "schemaVersion": 1,
        "kind": "migration-manager-decision-request-v1",
        "pilot": pilot,
        "liveCampaign": campaign,
        "initialPlanDigest": plan_digest,
        "initialSourceFingerprint": source_fingerprint,
        "blockingConflictCodes": blocking_codes,
        "packets": packet_refs,
        "requiredSubmissions": {
            "profileDecision": "manager-input/profile-decision.json"
            if "unsupported-retrieval-profile" in blocking_codes
            else "",
            "truthReviewsDirectory": "manager-input/truth-reviews"
            if truth_review_needed
            else "",
            "planApproval": "manager-input/plan-approval.json",
        },
        "instruction": (
            "Review exact packets; submit only manager-authored decisions bound to "
            "their current digests. Then approve only the regenerated preview digest."
        ),
    }
    decision_request = _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/review/manager-decision-request.json",
        value=request,
    )

    plugin_after = _repo_binding(plugin_source, plugin_revision, label="pluginRepository")
    project_after = _repo_binding(project_source, project_revision, label="projectRepository")
    _assert_binding_unchanged(plugin_before, plugin_after, label="pluginRepository")
    _assert_binding_unchanged(project_before, project_after, label="projectRepository")
    production_after = _production_guard(project_source, project_after)
    if production_after != production_before:
        _fail("productionGuard", "production legacy proof changed during prepare")

    receipt: dict[str, Any] = {
        "pilot": pilot,
        "liveCampaign": campaign,
        "phase": "manager-decisions-required",
        "createdAt": _utc_now(),
        "repositories": {"plugin": plugin_before, "project": project_before},
        "disposableCopies": {"plugin": plugin_copy, "project": project_copy},
        "completeProjectTreeRetained": True,
        "liveCampaignIsCertificationScopeOnly": True,
        "productionBoundary": {
            "before": production_before,
            "after": production_after,
            "unchanged": True,
        },
        "package": package,
        "tools": {
            "harnessScriptPath": "tests/re_discipline_migration_pilot.py",
            "harnessScriptSha256": _identity(Path(__file__).resolve().read_bytes()),
            "pythonVersion": sys.version.splitlines()[0],
            "goVersion": go_version.stdout.decode("utf-8", errors="strict").strip(),
            "helper": _artifact_ref(helper, run_root),
            "helperBuildCaptureDigest": build_capture["digest"],
        },
        "preview": {
            "planDigest": plan_digest,
            "sourceFingerprint": source_fingerprint,
            "sourceCount": len(sources),
            "blockingConflictCodes": blocking_codes,
            "result": _artifact_ref(preview_path, run_root),
            "renderedDirectory": _directory_ref(preview_output, run_root),
        },
        "decisionRequest": decision_request,
        "failureInjections": [
            {
                "scenario": "malformed-input",
                "captureDigest": malformed_capture["digest"],
                "safe": True,
            },
            {
                "scenario": "mcp-outage-cli-fallback",
                "failureCaptureDigest": outage_capture["digest"],
                "fallbackCaptureDigest": fallback_capture["digest"],
                "safe": True,
            },
        ],
        "commands": _command_refs(run_root),
    }
    return _seal_receipt(
        run_root=run_root, plugin_source=plugin_source, receipt=receipt
    )


def _directory_ref(root: Path, run_root: Path) -> dict[str, Any]:
    if root.is_symlink() or not root.is_dir():
        _fail(str(root), "directory artifact must be a real directory")
    rows: list[tuple[str, str]] = []
    byte_count = 0
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        if path.is_symlink():
            _fail(str(path), "directory artifact contains a symbolic link")
        if path.is_dir():
            continue
        if not path.is_file():
            _fail(str(path), "directory artifact contains a non-regular file")
        relative = path.relative_to(root).as_posix()
        body = path.read_bytes()
        rows.append((relative, _identity(body)))
        byte_count += len(body)
    return {
        "path": root.resolve(strict=True).relative_to(run_root.resolve(strict=True)).as_posix(),
        "algorithm": PAIR_DIGEST_ALGORITHM,
        "fileCount": len(rows),
        "byteCount": byte_count,
        "manifestSha256": _pair_digest(rows),
    }


def _validate_manager_approval(
    value: dict[str, Any],
    *,
    kind: str,
    pilot: str,
    campaign: str,
    digest_field: str,
    digest_value: str,
) -> None:
    required = {
        "schemaVersion",
        "kind",
        "pilot",
        "liveCampaign",
        digest_field,
        "authority",
        "reviewer",
        "rationale",
        "decidedAt",
        "explicitApproval",
    }
    if set(value) != required:
        _fail("managerApproval", f"fields must equal {sorted(required)}")
    if (
        value.get("schemaVersion") != 1
        or value.get("kind") != kind
        or value.get("pilot") != pilot
        or value.get("liveCampaign") != campaign
        or value.get(digest_field) != digest_value
        or value.get("authority") != "manager"
        or value.get("explicitApproval") is not True
        or not isinstance(value.get("reviewer"), str)
        or not value["reviewer"].strip()
        or not isinstance(value.get("rationale"), str)
        or not value["rationale"].strip()
        or not isinstance(value.get("decidedAt"), str)
        or not value["decidedAt"].endswith("Z")
    ):
        _fail("managerApproval", "is incomplete or does not bind the exact pilot state")


def _phase_context(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> tuple[
    dict[str, Any],
    Path,
    Path,
    Path,
    CaptureLog,
    dict[str, Any],
    dict[str, Any],
]:
    plugin_source = plugin_source.resolve(strict=True)
    project_source = project_source.resolve(strict=True)
    run_root = run_root.resolve(strict=True)
    if _overlaps(run_root, plugin_source) or _overlaps(run_root, project_source):
        _fail("output", "pilot output overlaps a source repository")
    receipt = _read_json(_receipt_path(run_root))
    _verify_receipt_digest(receipt)
    schema = _read_json(_contained(plugin_source, PILOT_SCHEMA, field="receipt.schema"))
    validate_json_schema(schema, receipt)
    if receipt.get("pilot") != pilot or receipt.get("liveCampaign") != PILOT_CAMPAIGNS.get(pilot):
        _fail("receipt.pilot", "does not match the requested pilot")
    plugin_binding = _repo_binding(plugin_source, plugin_revision, label="pluginRepository")
    project_binding = _repo_binding(project_source, project_revision, label="projectRepository")
    if receipt.get("repositories") != {
        "plugin": plugin_binding,
        "project": project_binding,
    }:
        _fail("receipt.repositories", "does not bind the current clean source revisions")
    production = _production_guard(project_source, project_binding)
    before = receipt.get("productionBoundary", {}).get("before")
    if production != before:
        _fail("productionBoundary", "production source proof changed")
    plugin_clone = run_root / "disposable" / "plugin"
    project_clone = run_root / "disposable" / "project"
    if plugin_clone.is_symlink() or project_clone.is_symlink():
        _fail("disposable", "clone roots may not be symbolic links")
    if not plugin_clone.is_dir() or not project_clone.is_dir():
        _fail("disposable", "clone roots are absent")
    if _git(plugin_clone, "rev-parse", "HEAD").decode("ascii").strip() != plugin_revision:
        _fail("disposable.plugin", "revision changed")
    if _git(project_clone, "rev-parse", "HEAD").decode("ascii").strip() != project_revision:
        _fail("disposable.project", "revision changed")
    package = _verify_package(plugin_clone)
    if package != receipt.get("package"):
        _fail("package", "disposable plugin package identity changed")
    runtime = _contained(plugin_clone, package["runtimePath"], field="runtime.path")
    tools = receipt.get("tools")
    if not isinstance(tools, dict) or tools.get("harnessScriptSha256") != _identity(
        Path(__file__).resolve().read_bytes()
    ):
        _fail("tools.harnessScriptSha256", "executing harness differs from pinned receipt")
    _verify_artifact_ref(run_root, tools.get("helper"), field="tools.helper")
    logger = CaptureLog(
        run_root,
        plugin=plugin_clone,
        project=project_clone,
        timeout_seconds=timeout_seconds,
    )
    return (
        receipt,
        plugin_clone,
        project_clone,
        runtime,
        logger,
        plugin_binding,
        project_binding,
    )


def _submit_manager_decisions(
    *,
    receipt: dict[str, Any],
    plugin_clone: Path,
    project_clone: Path,
    runtime: Path,
    logger: CaptureLog,
    run_root: Path,
) -> tuple[list[dict[str, Any]], dict[str, Any], Path]:
    submitted: list[dict[str, Any]] = []
    conflicts = receipt["preview"]["blockingConflictCodes"]
    manager_root = run_root / REVIEW_ROOT
    if "unsupported-retrieval-profile" in conflicts:
        decision_path = manager_root / "profile-decision.json"
        decision = _read_json(decision_path)
        schema = _read_json(
            _contained(
                plugin_clone,
                f"{ASSET_ROOT}/schemas/migration-profile-decision-submission.schema.json",
                field="profileDecision.schema",
            )
        )
        validate_json_schema(schema, decision)
        capture, stdout, _ = logger.run(
            name="profile-decision-submit",
            command=_migration_command(
                runtime,
                plugin_clone,
                "--project",
                str(project_clone),
                "--profile-decision",
                str(decision_path),
            ),
            cwd=plugin_clone,
            request=decision_path,
        )
        result = _json_stdout(stdout, "profile-decision-submit")
        submitted.append(
            {
                "kind": "profile-decision",
                "input": _artifact_ref(decision_path, run_root),
                "resultDigest": result.get("digest"),
                "captureDigest": capture["digest"],
            }
        )
    truth_needed = any(str(code).startswith("truth-") for code in conflicts)
    if truth_needed:
        packet_ref = receipt["decisionRequest"]
        request_path = _verify_artifact_ref(run_root, packet_ref, field="decisionRequest")
        decision_request = _read_json(request_path)
        truth_ref = decision_request.get("packets", {}).get("truthConflicts")
        truth_packet_path = _verify_artifact_ref(
            run_root, truth_ref, field="decisionRequest.truthConflicts"
        )
        truth_packet = _read_json(truth_packet_path)
        packet_sources = {
            row.get("sourcePath")
            for row in truth_packet.get("conflicts", [])
            if isinstance(row, dict)
        }
        review_dir = manager_root / "truth-reviews"
        if review_dir.is_symlink() or not review_dir.is_dir():
            _fail("truthReviews", "manager-input/truth-reviews is required")
        review_paths = sorted(review_dir.glob("*.json"))
        reviews: list[dict[str, Any]] = []
        schema = _read_json(
            _contained(
                plugin_clone,
                f"{ASSET_ROOT}/schemas/migration-truth-review-submission.schema.json",
                field="truthReview.schema",
            )
        )
        for review_path in review_paths:
            review = _read_json(review_path)
            validate_json_schema(schema, review)
            reviews.append(review)
        review_sources = {review.get("sourcePath") for review in reviews}
        if review_sources != packet_sources or len(reviews) != len(packet_sources):
            _fail("truthReviews", "must contain exactly one review per conflict source")
        for index, (review_path, review) in enumerate(zip(review_paths, reviews), start=1):
            capture, stdout, _ = logger.run(
                name=f"truth-review-submit-{index:03d}",
                command=_migration_command(
                    runtime,
                    plugin_clone,
                    "--project",
                    str(project_clone),
                    "--truth-review",
                    str(review_path),
                ),
                cwd=plugin_clone,
                request=review_path,
            )
            result = _json_stdout(stdout, f"truth-review-submit-{index:03d}")
            submitted.append(
                {
                    "kind": "truth-review",
                    "sourcePath": review["sourcePath"],
                    "input": _artifact_ref(review_path, run_root),
                    "resultDigest": result.get("digest"),
                    "captureDigest": capture["digest"],
                }
            )

    preview_output = run_root / ARTIFACT_ROOT / "preview" / "reviewed"
    capture, stdout, _ = logger.run(
        name="preview-reviewed",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--preview",
            "--live-campaigns",
            receipt["liveCampaign"],
            "--output",
            str(preview_output),
        ),
        cwd=plugin_clone,
    )
    preview = _json_stdout(stdout, "preview-reviewed")
    preview_path = run_root / ARTIFACT_ROOT / "preview" / "reviewed-result.json"
    _write_json(preview_path, preview)
    plan = preview.get("plan")
    if not isinstance(plan, dict):
        _fail("preview.reviewed", "result has no plan")
    blockers = [
        item
        for item in plan.get("conflicts", [])
        if isinstance(item, dict) and item.get("blocks") is True
    ]
    if blockers:
        _fail(
            "preview.reviewed",
            f"manager decisions left blockers {[item.get('code') for item in blockers]}",
        )
    if plan.get("sourceFingerprint") != receipt["preview"]["sourceFingerprint"]:
        _fail("preview.reviewed", "source fingerprint changed during decision review")
    plan_digest = plan.get("planDigest")
    if not isinstance(plan_digest, str) or not DIGEST_RE.fullmatch(plan_digest):
        _fail("preview.reviewed.planDigest", "is malformed")
    approval_path = manager_root / "plan-approval.json"
    approval = _read_json(approval_path)
    _validate_manager_approval(
        approval,
        kind="migration-plan-approval-v1",
        pilot=receipt["pilot"],
        campaign=receipt["liveCampaign"],
        digest_field="planDigest",
        digest_value=plan_digest,
    )
    submitted.append(
        {
            "kind": "plan-approval",
            "input": _artifact_ref(approval_path, run_root),
            "planDigest": plan_digest,
        }
    )
    return submitted, preview, preview_path


def _coverage_review_request(
    *, run_root: Path, project_clone: Path, state: dict[str, Any], plan: dict[str, Any], receipt: dict[str, Any]
) -> dict[str, Any]:
    reports: list[dict[str, Any]] = []
    report_root = run_root / ARTIFACT_ROOT / "review" / "coverage-reports"
    for source in plan.get("sources", []):
        if not isinstance(source, dict):
            continue
        if source.get("role") != "legacy-run-report" or source.get("campaign") != receipt["liveCampaign"]:
            continue
        source_path = _contained(project_clone, source["path"], field="coverage.source")
        body = source_path.read_bytes()
        if _identity(body) != "sha256:" + source["sha256"]:
            _fail("coverage.source", f"{source['path']} digest changed")
        normalized = body.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
        lines = normalized.split(b"\n")
        if lines and lines[-1] == b"":
            lines.pop()
        if not lines:
            _fail("coverage.source", f"{source['path']} has no coverable lines")
        source_key = hashlib.sha256(source["path"].encode("utf-8")).hexdigest()
        artifact_name = source_key + "-" + source["sha256"] + ".md"
        artifact_path = report_root / artifact_name
        if not artifact_path.exists():
            _copy_artifact(source_path, artifact_path)
        elif artifact_path.read_bytes() != body:
            _fail("coverage.source", f"artifact collision for {source['path']}")
        destination = source.get("destination")
        if not isinstance(destination, str):
            _fail("coverage.source", "report destination is absent")
        reports.append(
            {
                "sourcePath": source["path"],
                "sourceDigest": source["sha256"],
                "destinationPath": destination,
                "destinationDigest": "sha256:" + source["sha256"],
                "sourceLineCount": len(lines),
                "fullSpanHandle": f"path:{destination}#L1-L{len(lines)}",
                "report": _artifact_ref(artifact_path, run_root),
                "requiredReceiptPath": (
                    f"manager-input/coverage/{source_key}.json"
                ),
            }
        )
    reports.sort(key=lambda row: row["sourcePath"])
    if not reports:
        _fail("coverage", "live campaign contains no legacy reports")
    packet = {
        "schemaVersion": 1,
        "kind": "migration-live-report-coverage-request-v1",
        "pilot": receipt["pilot"],
        "liveCampaign": receipt["liveCampaign"],
        "transactionId": state.get("transactionId"),
        "planDigest": state.get("planDigest"),
        "reportCount": len(reports),
        "reports": reports,
        "instruction": (
            "A curator must exhaustively partition every report and author any candidate "
            "findings; a manager must review the finalized receipt. The harness does not "
            "infer dispositions, claims, evidence grades, or approval."
        ),
    }
    return _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/review/coverage-request.json",
        value=packet,
    )


def _advance_decisions(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    (
        receipt,
        plugin_clone,
        project_clone,
        runtime,
        logger,
        plugin_binding,
        project_binding,
    ) = _phase_context(
        plugin_source=plugin_source,
        plugin_revision=plugin_revision,
        project_source=project_source,
        project_revision=project_revision,
        run_root=run_root,
        pilot=pilot,
        timeout_seconds=timeout_seconds,
    )
    if receipt["phase"] != "manager-decisions-required":
        _fail("phase", f"expected manager-decisions-required, got {receipt['phase']}")
    submitted, preview, preview_path = _submit_manager_decisions(
        receipt=receipt,
        plugin_clone=plugin_clone,
        project_clone=project_clone,
        runtime=runtime,
        logger=logger,
        run_root=run_root,
    )
    plan = preview["plan"]
    apply_capture, apply_stdout, _ = logger.run(
        name="apply-approved-plan",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--apply",
            plan["planDigest"],
            "--live-campaigns",
            receipt["liveCampaign"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
    )
    state = _json_stdout(apply_stdout, "apply-approved-plan")
    if state.get("state") != "inventoried" or state.get("planDigest") != plan["planDigest"]:
        _fail("apply", "did not enter inventoried at the approved plan digest")
    resume_capture, resume_stdout, _ = logger.run(
        name="resume-shadow-index",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--resume",
            state["transactionId"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
    )
    state = _json_stdout(resume_stdout, "resume-shadow-index")
    if state.get("state") != "shadow-indexed":
        _fail("resume", "did not enter shadow-indexed")
    coverage_request = _coverage_review_request(
        run_root=run_root,
        project_clone=project_clone,
        state=state,
        plan=plan,
        receipt=receipt,
    )
    current = copy.deepcopy(receipt)
    current["phase"] = "coverage-review-required"
    current["managerSubmissions"] = submitted
    current["reviewedPreview"] = {
        "planDigest": plan["planDigest"],
        "sourceFingerprint": plan["sourceFingerprint"],
        "result": _artifact_ref(preview_path, run_root),
    }
    current["migrationState"] = state
    current["coverageRequest"] = coverage_request
    current["commands"] = _command_refs(run_root)
    current["productionBoundary"]["after"] = _production_guard(
        project_source, project_binding
    )
    current["productionBoundary"]["unchanged"] = (
        current["productionBoundary"]["after"]
        == current["productionBoundary"]["before"]
    )
    if not current["productionBoundary"]["unchanged"]:
        _fail("productionBoundary", "production checkout changed")
    return _seal_receipt(
        run_root=run_root, plugin_source=plugin_source, receipt=current
    )


def _coverage_inputs(
    *, run_root: Path, receipt: dict[str, Any]
) -> list[tuple[Path, dict[str, Any]]]:
    request_path = _verify_artifact_ref(
        run_root, receipt.get("coverageRequest"), field="coverageRequest"
    )
    request = _read_json(request_path)
    reports = request.get("reports")
    if not isinstance(reports, list) or request.get("reportCount") != len(reports):
        _fail("coverageRequest", "report inventory is malformed")
    expected = {
        row.get("sourcePath"): row
        for row in reports
        if isinstance(row, dict) and isinstance(row.get("sourcePath"), str)
    }
    if len(expected) != len(reports):
        _fail("coverageRequest", "report source paths must be unique")
    coverage_root = run_root / REVIEW_ROOT / "coverage"
    if coverage_root.is_symlink() or not coverage_root.is_dir():
        _fail("coverage", "manager-input/coverage is required")
    paths = sorted(coverage_root.glob("*.json"))
    values: list[tuple[Path, dict[str, Any]]] = []
    observed: set[str] = set()
    for path in paths:
        value = _read_json(path)
        source_path = value.get("sourcePath")
        if not isinstance(source_path, str) or source_path not in expected:
            _fail("coverage", f"{path.name} names an unexpected report")
        if source_path in observed:
            _fail("coverage", f"more than one receipt names {source_path}")
        observed.add(source_path)
        report = expected[source_path]
        if (
            value.get("sourceDigest") != report["sourceDigest"]
            or value.get("complete") is not True
            or not isinstance(value.get("reviewer"), str)
            or not value["reviewer"].strip()
            or not isinstance(value.get("rationale"), str)
            or not value["rationale"].strip()
        ):
            _fail("coverage", f"{path.name} is incomplete or stale")
        values.append((path, value))
    if observed != set(expected):
        _fail("coverage", f"missing receipts for {sorted(set(expected) - observed)}")
    return values


def _inject_stale_writer(
    *,
    logger: CaptureLog,
    runtime: Path,
    plugin_clone: Path,
    project_clone: Path,
    run_root: Path,
    transaction_id: str,
    source_relative: str,
) -> dict[str, Any]:
    path = _contained(project_clone, source_relative, field="staleWriter.source")
    if path.is_symlink() or not path.is_file():
        _fail("staleWriter.source", "must be a regular report")
    before = path.read_bytes()
    before_digest = _identity(before)
    info = path.stat()
    atime_ns, mtime_ns = info.st_atime_ns, info.st_mtime_ns
    path.write_bytes(before + b"\n<!-- disposable stale-writer injection -->\n")
    changed_digest = _identity(path.read_bytes())
    if changed_digest == before_digest:
        _fail("staleWriter", "injection did not change the source")
    capture, _, stderr = logger.run(
        name="stale-writer-refusal",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--resume",
            transaction_id,
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
        expect_success=False,
        failure_class="stale-writer-conflict",
    )
    path.write_bytes(before)
    os.utime(path, ns=(atime_ns, mtime_ns))
    restored = path.stat()
    if _identity(path.read_bytes()) != before_digest or restored.st_mtime_ns != mtime_ns:
        _fail("staleWriter", "could not restore exact approved bytes and mtime")
    if b"changed" not in stderr.lower() and b"stale" not in stderr.lower():
        _fail("staleWriter", "engine failure did not identify stale/changed input")
    packet = {
        "schemaVersion": 1,
        "kind": "migration-stale-writer-injection-v1",
        "sourcePath": source_relative,
        "approvedSha256": before_digest,
        "injectedSha256": changed_digest,
        "mtimeNs": mtime_ns,
        "failureCaptureDigest": capture["digest"],
        "restoredExact": True,
        "safeRecovery": "restore approved bytes and nanosecond mtime, then resume",
    }
    return _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/injections/stale-writer-result.json",
        value=packet,
    )


def _record_gate(
    *,
    logger: CaptureLog,
    runtime: Path,
    plugin_clone: Path,
    project_clone: Path,
    gate: str,
    artifact_relative: str,
) -> dict[str, Any]:
    artifact = _contained(project_clone, artifact_relative, field=f"gate.{gate}.artifact")
    if artifact.is_symlink() or not artifact.is_file():
        _fail(f"gate.{gate}", "artifact is absent or unsafe")
    artifact_digest = _identity(artifact.read_bytes())
    capture, stdout, _ = logger.run(
        name=f"record-{gate}-gate",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--gate",
            gate,
            "--gate-passed",
            "--artifact",
            artifact_relative,
            "--artifact-digest",
            artifact_digest,
            "--reviewer",
            "manager",
        ),
        cwd=plugin_clone,
    )
    result = _json_stdout(stdout, f"record-{gate}-gate")
    return {
        "gate": gate,
        "artifactPath": artifact_relative,
        "artifactSha256": artifact_digest,
        "gateReceiptDigest": result.get("digest"),
        "captureDigest": capture["digest"],
    }


def _advance_coverage(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    (
        receipt,
        plugin_clone,
        project_clone,
        runtime,
        logger,
        _plugin_binding,
        project_binding,
    ) = _phase_context(
        plugin_source=plugin_source,
        plugin_revision=plugin_revision,
        project_source=project_source,
        project_revision=project_revision,
        run_root=run_root,
        pilot=pilot,
        timeout_seconds=timeout_seconds,
    )
    if receipt["phase"] != "coverage-review-required":
        _fail("phase", f"expected coverage-review-required, got {receipt['phase']}")
    state = receipt["migrationState"]
    if state.get("state") != "shadow-indexed":
        _fail("migrationState", "coverage phase requires shadow-indexed")
    coverage_inputs = _coverage_inputs(run_root=run_root, receipt=receipt)
    submissions: list[dict[str, Any]] = []
    for index, (path, value) in enumerate(coverage_inputs, start=1):
        capture, stdout, _ = logger.run(
            name=f"coverage-submit-{index:03d}",
            command=_migration_command(
                runtime,
                plugin_clone,
                "--project",
                str(project_clone),
                "--coverage",
                str(path),
            ),
            cwd=plugin_clone,
            request=path,
        )
        result = _json_stdout(stdout, f"coverage-submit-{index:03d}")
        if result.get("sourcePath") != value["sourcePath"] or not DIGEST_RE.fullmatch(
            str(result.get("digest", ""))
        ):
            _fail("coverage", f"engine returned an invalid receipt for {value['sourcePath']}")
        submissions.append(
            {
                "sourcePath": value["sourcePath"],
                "input": _artifact_ref(path, run_root),
                "receiptDigest": result["digest"],
                "captureDigest": capture["digest"],
            }
        )

    request_path = _verify_artifact_ref(
        run_root, receipt["coverageRequest"], field="coverageRequest"
    )
    coverage_request = _read_json(request_path)
    first_source = coverage_request["reports"][0]["sourcePath"]
    stale_result = _inject_stale_writer(
        logger=logger,
        runtime=runtime,
        plugin_clone=plugin_clone,
        project_clone=project_clone,
        run_root=run_root,
        transaction_id=state["transactionId"],
        source_relative=first_source,
    )

    normalize_capture, stdout, _ = logger.run(
        name="resume-normalized",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--resume",
            state["transactionId"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 900),
    )
    state = _json_stdout(stdout, "resume-normalized")
    if state.get("state") != "normalized":
        _fail("resume.normalized", f"unexpected state {state.get('state')}")

    helper = _verify_artifact_ref(
        run_root, receipt["tools"]["helper"], field="tools.helper"
    )
    interruption_capture, _, interruption_stderr = logger.run(
        name="activation-interruption",
        command=[
            str(helper),
            "interrupt",
            "--project",
            str(project_clone),
            "--transaction",
            state["transactionId"],
            "--phase",
            "backed-up",
            "--target-index",
            "0",
        ],
        cwd=plugin_clone,
        expect_success=False,
        failure_class="activation-interrupted-after-backup",
        timeout_seconds=max(timeout_seconds, 900),
    )
    if b"injected disposable activation interruption" not in interruption_stderr:
        _fail("activationInterruption", "helper did not reach the durable interruption seam")
    activation_path = project_clone / Path(*PurePosixPath(MIGRATION_ROOT).parts) / "activation.json"
    activation = _read_json(activation_path)
    targets = activation.get("targets")
    if (
        activation.get("phase") != "activating"
        or not isinstance(targets, list)
        or not targets
        or targets[0].get("phase") != "backed-up"
    ):
        _fail("activationInterruption", "journal did not persist the backed-up target")
    interruption_result = _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/injections/activation-interruption-result.json",
        value={
            "schemaVersion": 1,
            "kind": "migration-activation-interruption-v1",
            "transactionId": state["transactionId"],
            "planDigest": state["planDigest"],
            "phase": activation["phase"],
            "interruptedTargetPath": targets[0]["path"],
            "interruptedTargetPhase": targets[0]["phase"],
            "activationJournalDigest": activation.get("digest"),
            "failureCaptureDigest": interruption_capture["digest"],
            "safeNextAction": "resume the same transaction through the real CLI",
        },
    )

    recovery_capture, stdout, _ = logger.run(
        name="resume-after-interruption",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--resume",
            state["transactionId"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 1200),
    )
    state = _json_stdout(stdout, "resume-after-interruption")
    if state.get("state") != "physically-reorganized":
        _fail("activationRecovery", f"unexpected state {state.get('state')}")

    # Ordinary project files outside the approved managed inventory remain
    # byte-identical; the engine separately authenticates every managed move.
    reviewed_preview_path = _verify_artifact_ref(
        run_root, receipt["reviewedPreview"]["result"], field="reviewedPreview"
    )
    plan = _read_json(reviewed_preview_path)["plan"]
    managed_sources = {row["path"] for row in plan["sources"]}
    original_rows = dict(_tracked_rows(project_source))
    untouched_rows = {
        path: identity for path, identity in original_rows.items() if path not in managed_sources
    }
    for relative, identity in untouched_rows.items():
        path = project_clone / Path(*PurePosixPath(relative).parts)
        if path.is_symlink() or not path.is_file() or _identity(path.read_bytes()) != identity:
            _fail("ordinaryProjectFiles", f"migration changed {relative}")

    gate_dir_relative = f"{MIGRATION_ROOT}/evidence"
    gate_dir = project_clone / Path(*PurePosixPath(gate_dir_relative).parts)
    gate_dir.mkdir(parents=True, exist_ok=True)
    intrinsic: list[dict[str, Any]] = []
    for gate in ("structural", "semantic-traversal"):
        capture, gate_stdout, _ = logger.run(
            name=f"derive-{gate}-gate",
            command=[
                str(helper),
                "intrinsic-gate",
                "--project",
                str(project_clone),
                "--asset-root",
                str(_contained(plugin_clone, ASSET_ROOT, field="helper.assetRoot")),
                "--gate",
                gate,
            ],
            cwd=plugin_clone,
            timeout_seconds=max(timeout_seconds, 900),
        )
        artifact = _json_stdout(gate_stdout, f"derive-{gate}-gate")
        artifact_path = gate_dir / f"{gate}.json"
        _write_json(artifact_path, artifact)
        recorded = _record_gate(
            logger=logger,
            runtime=runtime,
            plugin_clone=plugin_clone,
            project_clone=project_clone,
            gate=gate,
            artifact_relative=f"{gate_dir_relative}/{gate}.json",
        )
        recorded["derivationCaptureDigest"] = capture["digest"]
        intrinsic.append(recorded)

    evidence_request = _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/review/certification-evidence-request.json",
        value={
            "schemaVersion": 1,
            "kind": "migration-certification-evidence-request-v1",
            "pilot": receipt["pilot"],
            "liveCampaign": receipt["liveCampaign"],
            "transactionId": state["transactionId"],
            "planDigest": state["planDigest"],
            "requiredSubmission": f"{REVIEW_ROOT}/gates/evidence-submission.json",
            "requiredGates": ["retrieval-context", "host-parity"],
            "submissionContract": {
                "kind": "migration-pilot-evidence-submission-v1",
                "copyRowsBindSourceAndProjectDestination": True,
                "gateArtifactsMustBeNamed": ["retrieval-context", "host-parity"],
                "engineRevalidatesEveryNestedEvidenceBinding": True,
            },
            "instruction": (
                "Supply measured strict artifacts only after current benchmark, blinded-agent, "
                "and fixed host-conformance evidence exists. The harness copies exact declared "
                "bytes into the disposable project and the engine re-derives all identities."
            ),
        },
    )

    current = copy.deepcopy(receipt)
    current["phase"] = "certification-evidence-required"
    current["coverageSubmissions"] = submissions
    current["migrationState"] = state
    current["intrinsicGates"] = intrinsic
    current["certificationEvidenceRequest"] = evidence_request
    current["ordinaryProjectFileProof"] = {
        "fileCount": len(untouched_rows),
        "manifestSha256": _pair_digest(untouched_rows.items()),
        "unchanged": True,
    }
    current["failureInjections"].extend(
        [
            {"scenario": "stale-writer", "result": stale_result, "safe": True},
            {
                "scenario": "activation-interruption",
                "result": interruption_result,
                "recoveryCaptureDigest": recovery_capture["digest"],
                "safe": True,
            },
        ]
    )
    current["commands"] = _command_refs(run_root)
    current["productionBoundary"]["after"] = _production_guard(project_source, project_binding)
    current["productionBoundary"]["unchanged"] = (
        current["productionBoundary"]["after"]
        == current["productionBoundary"]["before"]
    )
    if not current["productionBoundary"]["unchanged"]:
        _fail("productionBoundary", "production checkout changed")
    return _seal_receipt(
        run_root=run_root, plugin_source=plugin_source, receipt=current
    )


def _install_evidence_submission(
    *, run_root: Path, project_clone: Path, receipt: dict[str, Any]
) -> tuple[dict[str, Any], dict[str, str]]:
    path = run_root / REVIEW_ROOT / "gates" / "evidence-submission.json"
    submission = _read_json(path)
    required = {
        "schemaVersion",
        "kind",
        "pilot",
        "liveCampaign",
        "transactionId",
        "planDigest",
        "authority",
        "reviewer",
        "rationale",
        "decidedAt",
        "copies",
        "gateArtifacts",
    }
    if set(submission) != required:
        _fail("evidenceSubmission", f"fields must equal {sorted(required)}")
    state = receipt["migrationState"]
    if (
        submission.get("schemaVersion") != 1
        or submission.get("kind") != "migration-pilot-evidence-submission-v1"
        or submission.get("pilot") != receipt["pilot"]
        or submission.get("liveCampaign") != receipt["liveCampaign"]
        or submission.get("transactionId") != state["transactionId"]
        or submission.get("planDigest") != state["planDigest"]
        or submission.get("authority") != "manager"
        or not isinstance(submission.get("reviewer"), str)
        or not submission["reviewer"].strip()
        or not isinstance(submission.get("rationale"), str)
        or not submission["rationale"].strip()
        or not isinstance(submission.get("decidedAt"), str)
        or not submission["decidedAt"].endswith("Z")
    ):
        _fail("evidenceSubmission", "identity or manager review is incomplete")
    copies = submission.get("copies")
    if not isinstance(copies, list) or len(copies) < 2:
        _fail("evidenceSubmission.copies", "must contain at least two files")
    source_prefix = f"{REVIEW_ROOT}/gates/"
    destination_prefix = f"{MIGRATION_ROOT}/evidence/"
    seen_sources: set[str] = set()
    seen_destinations: set[str] = set()
    installed: list[dict[str, Any]] = []
    for index, row in enumerate(copies):
        if not isinstance(row, dict) or set(row) != {
            "sourcePath",
            "destinationPath",
            "sha256",
        }:
            _fail(f"evidenceSubmission.copies[{index}]", "has invalid fields")
        source_relative = row["sourcePath"]
        destination_relative = row["destinationPath"]
        if (
            not isinstance(source_relative, str)
            or not source_relative.startswith(source_prefix)
            or not isinstance(destination_relative, str)
            or not destination_relative.startswith(destination_prefix)
            or source_relative in seen_sources
            or destination_relative in seen_destinations
            or not isinstance(row["sha256"], str)
            or not DIGEST_RE.fullmatch(row["sha256"])
        ):
            _fail(f"evidenceSubmission.copies[{index}]", "is unsafe, duplicate, or malformed")
        seen_sources.add(source_relative)
        seen_destinations.add(destination_relative)
        source = _contained(run_root, source_relative, field="evidenceSubmission.source")
        destination = _contained(
            project_clone, destination_relative, field="evidenceSubmission.destination"
        )
        if source.is_symlink() or not source.is_file() or _identity(source.read_bytes()) != row["sha256"]:
            _fail(f"evidenceSubmission.copies[{index}]", "source digest mismatches")
        if destination.exists() or destination.is_symlink():
            _fail(f"evidenceSubmission.copies[{index}]", "destination already exists")
        _require_real_parent_chain(
            project_clone,
            destination.parent,
            field=f"evidenceSubmission.copies[{index}].destinationPath",
        )
        _copy_artifact(source, destination)
        if _identity(destination.read_bytes()) != row["sha256"]:
            _fail(f"evidenceSubmission.copies[{index}]", "destination digest mismatches")
        installed.append(
            {
                "source": _artifact_ref(source, run_root),
                "destinationPath": destination_relative,
                "sha256": row["sha256"],
            }
        )
    gate_artifacts = submission.get("gateArtifacts")
    if not isinstance(gate_artifacts, dict) or set(gate_artifacts) != {
        "retrieval-context",
        "host-parity",
    }:
        _fail("evidenceSubmission.gateArtifacts", "must name exactly both external gates")
    for gate, destination in gate_artifacts.items():
        if destination not in seen_destinations:
            _fail("evidenceSubmission.gateArtifacts", f"{gate} is not a copied destination")
        artifact = _read_json(_contained(project_clone, destination, field=f"gate.{gate}"))
        if (
            artifact.get("gate") != gate
            or artifact.get("transactionId") != state["transactionId"]
            or artifact.get("planDigest") != state["planDigest"]
            or artifact.get("passed") is not True
        ):
            _fail(f"gate.{gate}", "artifact does not bind the active passing migration")
    result = {
        "schemaVersion": 1,
        "kind": "migration-pilot-evidence-installation-v1",
        "submission": _artifact_ref(path, run_root),
        "transactionId": state["transactionId"],
        "planDigest": state["planDigest"],
        "installed": installed,
        "gateArtifacts": gate_artifacts,
    }
    result_ref = _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/evidence/evidence-installation.json",
        value=result,
    )
    return {"submission": submission, "result": result_ref}, gate_artifacts


def _advance_certification(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    (
        receipt,
        plugin_clone,
        project_clone,
        runtime,
        logger,
        _plugin_binding,
        project_binding,
    ) = _phase_context(
        plugin_source=plugin_source,
        plugin_revision=plugin_revision,
        project_source=project_source,
        project_revision=project_revision,
        run_root=run_root,
        pilot=pilot,
        timeout_seconds=timeout_seconds,
    )
    if receipt["phase"] != "certification-evidence-required":
        _fail("phase", f"expected certification-evidence-required, got {receipt['phase']}")
    installed, gate_artifacts = _install_evidence_submission(
        run_root=run_root, project_clone=project_clone, receipt=receipt
    )
    external_gates = []
    for gate in ("retrieval-context", "host-parity"):
        external_gates.append(
            _record_gate(
                logger=logger,
                runtime=runtime,
                plugin_clone=plugin_clone,
                project_clone=project_clone,
                gate=gate,
                artifact_relative=gate_artifacts[gate],
            )
        )
    verify_capture, verify_stdout, _ = logger.run(
        name="verify-candidate-certification",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--verify",
        ),
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 1200),
    )
    certification = _json_stdout(verify_stdout, "verify-candidate-certification")
    certification_path = run_root / ARTIFACT_ROOT / "evidence" / "candidate-certification.json"
    _write_json(certification_path, certification)
    if (
        certification.get("candidate") is not True
        or certification.get("finalManagerDecision") != "pending-ratification"
        or certification.get("blockers") != []
        or not DIGEST_RE.fullmatch(str(certification.get("digest", "")))
    ):
        _fail("certification", f"engine did not emit a passing candidate: {certification.get('blockers')}")
    resume_capture, resume_stdout, _ = logger.run(
        name="resume-traversal-verified",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--resume",
            receipt["migrationState"]["transactionId"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 1200),
    )
    state = _json_stdout(resume_stdout, "resume-traversal-verified")
    if (
        state.get("state") != "traversal-verified"
        or state.get("certificationDigest") != certification["digest"]
    ):
        _fail("certification", "resume did not bind the reviewed certification digest")
    approval_path = run_root / REVIEW_ROOT / "certification-approval.json"
    approval = _read_json(approval_path)
    _validate_manager_approval(
        approval,
        kind="migration-certification-approval-v1",
        pilot=receipt["pilot"],
        campaign=receipt["liveCampaign"],
        digest_field="certificationDigest",
        digest_value=certification["digest"],
    )
    ratify_capture, ratify_stdout, _ = logger.run(
        name="ratify-certification",
        command=_migration_command(
            runtime,
            plugin_clone,
            "--project",
            str(project_clone),
            "--ratify",
            certification["digest"],
            "--actor",
            "manager",
        ),
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 1200),
    )
    state = _json_stdout(ratify_stdout, "ratify-certification")
    if state.get("state") != "migrated":
        _fail("ratification", f"unexpected state {state.get('state')}")
    version_capture, version_stdout, _ = logger.run(
        name="project-version-after",
        command=[str(runtime), "project-version", "--project-root", str(project_clone)],
        cwd=plugin_clone,
    )
    version = _json_stdout(version_stdout, "project-version-after")
    if version.get("projectStateVersion") != "0.8" or version.get("migrationRequired") is not False:
        _fail("ratification", "migrated clone is not recognized as 0.8")
    index_capture, index_stdout, _ = logger.run(
        name="rebuild-derived-index",
        command=[
            str(runtime),
            "index",
            "-project-root",
            str(project_clone),
            "-asset-root",
            str(_contained(plugin_clone, ASSET_ROOT, field="assetRoot")),
            "-cache-root",
            str(run_root / "cache" / "knowledge"),
        ],
        cwd=plugin_clone,
        timeout_seconds=max(timeout_seconds, 1200),
    )
    index_result = _json_stdout(index_stdout, "rebuild-derived-index")
    ratification_path = (
        project_clone
        / Path(*PurePosixPath(MIGRATION_ROOT).parts)
        / "certification.json"
    )
    if not ratification_path.exists():
        _fail("ratification", "immutable certification manifest is absent")
    ratification = _read_json(ratification_path)
    if ratification.get("certificationDigest") != certification["digest"]:
        _fail("ratification", "manifest does not bind the approved certification")

    final_ref = _write_packet(
        run_root=run_root,
        relative=f"{ARTIFACT_ROOT}/final/pilot-result.json",
        value={
            "schemaVersion": 1,
            "kind": "migration-disposable-pilot-result-v1",
            "pilot": receipt["pilot"],
            "liveCampaign": receipt["liveCampaign"],
            "transactionId": state["transactionId"],
            "planDigest": state["planDigest"],
            "certificationDigest": certification["digest"],
            "migrationStateDigest": state["digest"],
            "ratificationManifest": _artifact_ref(ratification_path, run_root),
            "candidateCertification": _artifact_ref(certification_path, run_root),
            "derivedIndexResultDigest": index_result.get("digest"),
            "sourceRepositoryMutated": False,
        },
    )
    current = copy.deepcopy(receipt)
    current["phase"] = "completed"
    current["migrationState"] = state
    current["externalEvidenceInstallation"] = installed
    current["externalGates"] = external_gates
    current["candidateCertification"] = _artifact_ref(certification_path, run_root)
    current["certificationApproval"] = _artifact_ref(approval_path, run_root)
    current["finalResult"] = final_ref
    current["commands"] = _command_refs(run_root)
    current["productionBoundary"]["after"] = _production_guard(project_source, project_binding)
    current["productionBoundary"]["unchanged"] = (
        current["productionBoundary"]["after"]
        == current["productionBoundary"]["before"]
    )
    if not current["productionBoundary"]["unchanged"]:
        _fail("productionBoundary", "production checkout changed")
    current = _seal_receipt(
        run_root=run_root, plugin_source=plugin_source, receipt=current
    )
    _verify_run(
        plugin_source=plugin_source,
        plugin_revision=plugin_revision,
        project_source=project_source,
        project_revision=project_revision,
        run_root=run_root,
        pilot=pilot,
        timeout_seconds=timeout_seconds,
    )
    return current


def _verify_run_artifacts(run_root: Path, value: Any, *, field: str = "receipt") -> None:
    if isinstance(value, dict):
        if set(value) == {"path", "sha256", "byteCount"}:
            _verify_artifact_ref(run_root, value, field=field)
            return
        for key, child in value.items():
            _verify_run_artifacts(run_root, child, field=f"{field}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _verify_run_artifacts(run_root, child, field=f"{field}[{index}]")


def _verify_run(
    *,
    plugin_source: Path,
    plugin_revision: str,
    project_source: Path,
    project_revision: str,
    run_root: Path,
    pilot: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    (
        receipt,
        _plugin_clone,
        project_clone,
        runtime,
        _logger,
        _plugin_binding,
        project_binding,
    ) = _phase_context(
        plugin_source=plugin_source,
        plugin_revision=plugin_revision,
        project_source=project_source,
        project_revision=project_revision,
        run_root=run_root,
        pilot=pilot,
        timeout_seconds=timeout_seconds,
    )
    _verify_run_artifacts(run_root, receipt)
    _command_refs(run_root)
    if receipt["productionBoundary"]["before"] != _production_guard(
        project_source, project_binding
    ):
        _fail("productionBoundary", "source project proof changed")
    if receipt["phase"] == "completed":
        result = _run_bytes(
            [str(runtime), "project-version", "--project-root", str(project_clone)],
            cwd=project_clone,
            timeout_seconds=timeout_seconds,
        )
        if result.returncode != 0:
            _fail("verify.projectVersion", result.stderr.decode("utf-8", errors="replace"))
        version = _json_stdout(result.stdout, "verify.projectVersion")
        if version.get("projectStateVersion") != "0.8":
            _fail("verify.projectVersion", "completed clone is not 0.8")
    return receipt


def _common_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--plugin-repository", type=Path, required=True)
    parser.add_argument("--plugin-revision", required=True)
    parser.add_argument("--project-repository", type=Path, required=True)
    parser.add_argument("--project-revision", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pilot", choices=sorted(PILOT_CAMPAIGNS), required=True)
    parser.add_argument("--timeout-seconds", type=int, default=600)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Run a pinned 0.7-to-0.8 migration only in disposable clones."
    )
    subparsers = parser.add_subparsers(dest="action", required=True)
    for action in ("prepare", "advance", "verify"):
        child = subparsers.add_parser(action)
        _common_arguments(child)
    return parser


def _dispatch(arguments: argparse.Namespace) -> dict[str, Any]:
    common = {
        "plugin_source": arguments.plugin_repository,
        "plugin_revision": arguments.plugin_revision,
        "project_source": arguments.project_repository,
        "project_revision": arguments.project_revision,
        "run_root": arguments.output,
        "pilot": arguments.pilot,
        "timeout_seconds": arguments.timeout_seconds,
    }
    if arguments.timeout_seconds < 30:
        _fail("timeoutSeconds", "must be at least 30")
    if arguments.action == "prepare":
        return _prepare(**common)
    if arguments.action == "verify":
        return _verify_run(**common)
    receipt = _read_json(_receipt_path(arguments.output.resolve(strict=True)))
    _verify_receipt_digest(receipt)
    phase = receipt.get("phase")
    if phase == "manager-decisions-required":
        return _advance_decisions(**common)
    if phase == "coverage-review-required":
        return _advance_coverage(**common)
    if phase == "certification-evidence-required":
        return _advance_certification(**common)
    if phase == "completed":
        return _verify_run(**common)
    _fail("phase", f"cannot advance phase {phase!r}")


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _parser().parse_args(argv)
        result = _dispatch(arguments)
        sys.stdout.buffer.write(_stable_json_bytes(result))
        return 0
    except (PilotError, OSError, UnicodeError, ValueError) as error:
        print(f"migration pilot: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
