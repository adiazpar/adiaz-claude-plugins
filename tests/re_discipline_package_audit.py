"""Deterministic re-discipline 0.8 release-tree inventory and legacy audit."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import tempfile
from pathlib import Path


FORBIDDEN_PATHS = {
    "skills/checkpoint-campaign",
    "skills/promote-truth",
    "templates/campaign-masterfile.md",
    "templates/campaign-reviews.md",
    "templates/chronicle.md",
}

EXPECTED_SKILLS = {
    "benchmark-knowledge",
    "calibrate-knowledge",
    "close-campaign",
    "decide-agent",
    "decide-retrieval-profile",
    "delegate",
    "hire-agent",
    "init-project",
    "migrate-project",
    "onboard",
    "open-campaign",
    "overturn",
    "review-memory",
    "review-subagent",
}

EXPECTED_MCP_TOOLS = [
    "state",
    "query",
    "read",
    "trace",
    "context_pack_materialize",
    "manager_apply",
    "curation_submit",
    "closure_apply",
    "normalization_queue",
    "migrate_project",
]

EXPECTED_PLUGIN_VERSION = "0.8.0"
RUNTIME_BUILD_ID_PATH = (
    "github.com/adiaz/re-discipline-knowledge/internal/knowledge.CompiledBuildID"
)
SHARED_ASSET_ROOTS = ("evals/conformance", "models", "profiles", "schemas")

EXPECTED_RUNTIME_TARGETS = [
    (
        "runtime",
        "windows",
        "amd64",
        "windows-amd64/re-discipline-knowledge.exe",
        "0644",
    ),
    (
        "runtime",
        "windows",
        "arm64",
        "windows-arm64/re-discipline-knowledge.exe",
        "0644",
    ),
    ("runtime", "linux", "amd64", "linux-amd64/re-discipline-knowledge", "0755"),
    ("runtime", "linux", "arm64", "linux-arm64/re-discipline-knowledge", "0755"),
    ("runtime", "darwin", "amd64", "darwin-amd64/re-discipline-knowledge", "0755"),
    ("runtime", "darwin", "arm64", "darwin-arm64/re-discipline-knowledge", "0755"),
]

EXPECTED_RUNTIME_LAUNCHERS = [
    ("posix-dispatch", "", "", "re-discipline-knowledge", "0755"),
    (
        "windows-architecture-dispatch",
        "windows",
        "amd64",
        "re-discipline-knowledge.exe",
        "0644",
    ),
]

EXPECTED_RUNTIME_MANIFEST_KEYS = {
    "$schema",
    "schemaVersion",
    "runtime",
    "build",
    "targets",
    "launchers",
    "sharedAssets",
    "notices",
}

REQUIRED_PATHS = {
    ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json",
    "agents/knowledge-curator.md",
    "skills/migrate-project/SKILL.md",
    "templates/campaign/campaign.json",
    "templates/campaign/work-item.json",
    "templates/campaign/deferred-work-item.json",
    "templates/campaign/run.json",
    "templates/campaign/brief.md",
    "templates/campaign/report.md",
    "templates/campaign/finding.md",
    "templates/campaign/intake.json",
    "templates/campaign/review.json",
    "templates/campaign/STATE.md",
    "templates/campaign/archive-README.md",
    "knowledge/schemas/campaign.schema.json",
    "knowledge/schemas/work-item.schema.json",
    "knowledge/schemas/run.schema.json",
    "knowledge/schemas/finding-frontmatter.schema.json",
    "knowledge/schemas/migration-plan.schema.json",
    "knowledge/schemas/migration-operation.schema.json",
    "knowledge/schemas/migration-receipt.schema.json",
    "knowledge/schemas/migration-gate-artifact.schema.json",
    "knowledge/schemas/migration-retrieval-gate-evidence.schema.json",
    "knowledge/schemas/migration-blinded-agent-evaluation.schema.json",
    "knowledge/schemas/migration-host-conformance.schema.json",
    "knowledge/schemas/state-inventory.schema.json",
    "knowledge/schemas/runtime-package-manifest.schema.json",
    "knowledge/bin/manifest.json",
    "knowledge/bin/SHA256SUMS",
}

LEGACY_PATTERNS = {
    "checkpoint-skill": re.compile(r"checkpoint-campaign", re.I),
    "standalone-truth-skill": re.compile(r"promote-truth", re.I),
    "campaign-narrative-file": re.compile(r"CAMPAIGN\.md", re.I),
    "review-ledger-file": re.compile(r"REVIEWS\.md", re.I),
    "categorical-run-root": re.compile(r"(?:^|[/\\])subagents(?:[/\\]|$)", re.I),
    "report-stamp": re.compile(r"(?:report|review)[ -]stamp", re.I),
    "master-file": re.compile(r"masterfile|master-file", re.I),
}

# The write-boundary hooks must name the two legacy canonical records they
# refuse to mutate. This is an enforcement-only exception: it does not permit
# retired commands, writers, or workspace layouts anywhere in either hook.
ENFORCEMENT_LEGACY_MATCHES = {
    ("hooks/re-discipline-hook.ps1", "campaign-narrative-file"),
    ("hooks/re-discipline-hook.ps1", "review-ledger-file"),
    ("hooks/re-discipline-hook.sh", "campaign-narrative-file"),
    ("hooks/re-discipline-hook.sh", "review-ledger-file"),
}

TEXT_SUFFIXES = {".md", ".json", ".jsonc", ".toml", ".ps1", ".sh", ".go", ".yaml", ".yml", ".awk"}


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def add_violation(
    violations: list[dict[str, str]], kind: str, path: str, detail: str
) -> None:
    violations.append({"kind": kind, "path": path, "detail": detail})


def read_version_constant(path: Path, name: str) -> str:
    text = path.read_text(encoding="utf-8")
    match = re.search(rf"\b{re.escape(name)}\s*=\s*\"([^\"]+)\"", text)
    return match.group(1) if match else ""


def read_go_toolchain(path: Path) -> str:
    for line in path.read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == "go":
            return "go" + fields[1]
    raise ValueError("go.mod does not declare a Go version")


def compute_runtime_build_id(knowledge_root: Path) -> str:
    """Mirror the packager's source-and-asset build identity exactly."""
    inputs = [
        knowledge_root / "go.mod",
        knowledge_root / "go.sum",
        knowledge_root / "THIRD_PARTY_NOTICES.md",
        knowledge_root.parent / "LICENSE",
    ]
    for relative_root in (
        "cmd/re-discipline-knowledge",
        "internal/knowledge",
    ):
        root = knowledge_root / relative_root
        for path in root.rglob("*"):
            if path.is_symlink():
                raise ValueError(f"build input cannot be a link: {path}")
            if (
                path.is_file()
                and path.suffix.lower() == ".go"
                and not path.name.lower().endswith("_test.go")
            ):
                inputs.append(path)
    for relative_root in SHARED_ASSET_ROOTS:
        root = knowledge_root / relative_root
        for path in root.rglob("*"):
            if path.is_symlink():
                raise ValueError(f"build input cannot be a link: {path}")
            if path.is_file():
                inputs.append(path)

    def relative(path: Path) -> str:
        return os.path.relpath(path, knowledge_root).replace(os.sep, "/")

    digest = hashlib.sha256()
    for path in sorted(inputs, key=relative):
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"build input is not a regular file: {path}")
        body = path.read_bytes()
        digest.update(relative(path).encode("utf-8"))
        digest.update(b"\0")
        digest.update(str(len(body)).encode("ascii"))
        digest.update(b"\0")
        digest.update(body)
    return "sha256:" + digest.hexdigest()


def shared_asset_kind(relative: str) -> str:
    if relative in {
        "evals/conformance/cases.json",
        "evals/conformance/finding-cases.json",
    }:
        return "benchmark-cases"
    if relative == "evals/conformance/lane-ablation-decision.json":
        return "lane-ablation-decision"
    if relative == "evals/conformance/lane-ablation-report.json":
        return "lane-ablation-report"
    if relative == "evals/conformance/project-lane-ablation.json":
        return "project-lane-ablation-measurement"
    if relative.startswith("evals/conformance/evidence/") and relative.lower().endswith(
        ".zip"
    ):
        return "lane-ablation-evidence-archive"
    if relative.startswith("evals/conformance/fixture/") and relative.lower().endswith(
        (".md", ".json", ".jsonc")
    ):
        return "benchmark-fixture"
    if relative == "models/artifacts/README.md":
        return "model-artifact-documentation"
    if relative.startswith("models/artifacts/") and relative.lower().endswith(
        ".bin"
    ):
        return "shared-model-artifact"
    if relative == "models/manifest.json":
        return "model-manifest"
    if relative.startswith("models/specs/") and relative.lower().endswith(".json"):
        return "model-specification"
    if relative.startswith("profiles/") and relative.lower().endswith(".json"):
        return "retrieval-profile"
    if relative.startswith("schemas/") and relative.lower().endswith(".json"):
        return "json-schema"
    raise ValueError(f"unclassified runtime asset {relative}")


def parse_checksum_file(path: Path) -> dict[str, str]:
    checksums: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^\s]+)", raw_line)
        if match is None:
            raise ValueError(f"malformed checksum line {raw_line!r}")
        digest, raw_relative = match.groups()
        relative = raw_relative.replace("\\", "/")
        parts = relative.split("/")
        asset_relative = "/".join(parts[1:])
        parent_asset = len(parts) >= 3 and parts[0] == ".." and any(
            asset_relative.startswith(root + "/") for root in SHARED_ASSET_ROOTS
        )
        if (
            not relative
            or raw_relative != relative
            or re.fullmatch(r"[A-Za-z0-9._/-]+", relative) is None
            or relative.startswith("/")
            or ":" in relative
            or any(part in {"", "."} for part in parts)
            or (".." in parts and not parent_asset)
            or (parent_asset and ".." in parts[1:])
            or relative in checksums
        ):
            raise ValueError(f"invalid or duplicate checksum line {raw_line!r}")
        checksums[relative] = digest
    return checksums


def validate_manifest_file(
    base: Path,
    record: dict[str, object],
    manifest_path: str,
    violations: list[dict[str, str]],
) -> tuple[str, str] | None:
    raw_value = record.get("path", "")
    if not isinstance(raw_value, str):
        add_violation(
            violations, "runtime-manifest-path", manifest_path, repr(raw_value)
        )
        return None
    raw_relative = raw_value
    relative = raw_relative.replace("\\", "/")
    if (
        not relative
        or raw_relative != relative
        or re.fullmatch(r"[A-Za-z0-9._/-]+", relative) is None
        or Path(relative).as_posix() != relative
        or relative.startswith("/")
        or ":" in relative
        or re.match(r"^[A-Za-z]:", relative)
        or ".." in Path(relative).parts
    ):
        add_violation(violations, "runtime-manifest-path", manifest_path, relative)
        return None
    target = base / Path(relative)
    if target.is_symlink():
        add_violation(violations, "runtime-manifest-link", manifest_path, relative)
        return None
    if not target.is_file():
        add_violation(
            violations, "runtime-manifest-missing-file", manifest_path, relative
        )
        return None
    try:
        target.resolve(strict=True).relative_to(base.resolve(strict=True))
    except (OSError, ValueError):
        add_violation(violations, "runtime-manifest-path", manifest_path, relative)
        return None
    data = target.read_bytes()
    actual_digest = "sha256:" + sha256_bytes(data)
    declared_digest = str(record.get("sha256", ""))
    declared_size = record.get("size")
    if (
        actual_digest != declared_digest
        or not isinstance(declared_size, int)
        or isinstance(declared_size, bool)
        or declared_size != len(data)
    ):
        add_violation(
            violations,
            "runtime-manifest-file-mismatch",
            manifest_path,
            f"{relative}: declared {declared_digest}/{declared_size}, "
            f"actual {actual_digest}/{len(data)}",
        )
    return relative, actual_digest.removeprefix("sha256:")


def probe_runtime_tools(
    stage: Path,
    manifest: dict[str, object],
    expected_version: str,
    violations: list[dict[str, str]],
) -> None:
    goos = {"windows": "windows", "linux": "linux", "darwin": "darwin"}.get(
        platform.system().lower()
    )
    machine = platform.machine().lower()
    goarch = "amd64" if machine in {"amd64", "x86_64"} else "arm64" if machine in {
        "arm64",
        "aarch64",
    } else None
    if not goos or not goarch:
        add_violation(
            violations,
            "runtime-probe-unsupported-host",
            "knowledge/bin/manifest.json",
            f"{platform.system()}/{platform.machine()}",
        )
        return
    targets = manifest.get("targets")
    if not isinstance(targets, list):
        add_violation(
            violations,
            "runtime-probe-target",
            "knowledge/bin/manifest.json",
            "targets is not a list",
        )
        return
    target = next(
        (
            row
            for row in targets
            if isinstance(row, dict)
            if row.get("goos") == goos and row.get("goarch") == goarch
        ),
        None,
    )
    if not isinstance(target, dict):
        add_violation(
            violations,
            "runtime-probe-target",
            "knowledge/bin/manifest.json",
            f"missing {goos}-{goarch} target",
        )
        return
    binary = stage / "knowledge" / "bin" / str(target.get("path", ""))
    if not binary.is_file():
        return
    try:
        binary.resolve(strict=True).relative_to(
            (stage / "knowledge" / "bin").resolve(strict=True)
        )
    except (OSError, ValueError):
        add_violation(
            violations,
            "runtime-probe-target",
            str(target.get("path", "")),
            "target resolves outside knowledge/bin",
        )
        return
    if binary.is_symlink():
        add_violation(
            violations,
            "runtime-probe-target",
            str(target.get("path", "")),
            "target is a link",
        )
        return
    requests = [
        {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "package-audit", "version": "1"},
            },
        },
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}},
    ]
    payload = "".join(json.dumps(row, separators=(",", ":")) + "\n" for row in requests)
    try:
        completed = subprocess.run(
            [str(binary), "serve", "--asset-root", str(stage / "knowledge")],
            input=payload,
            text=True,
            capture_output=True,
            timeout=15,
            check=False,
            env={**os.environ, "CLAUDE_PROJECT_DIR": ""},
        )
        responses = {
            row["id"]: row
            for line in completed.stdout.splitlines()
            if line.strip()
            for row in [json.loads(line)]
            if isinstance(row, dict)
            if isinstance(row.get("id"), int)
        }
    except (OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        add_violation(
            violations, "runtime-tool-probe", str(target.get("path", "")), str(error)
        )
        return
    if completed.returncode != 0 or 1 not in responses or 2 not in responses:
        add_violation(
            violations,
            "runtime-tool-probe",
            str(target.get("path", "")),
            completed.stderr.strip() or "incomplete MCP response",
        )
        return
    initialize_result = responses[1].get("result")
    server_info = (
        initialize_result.get("serverInfo", {})
        if isinstance(initialize_result, dict)
        else {}
    )
    if not isinstance(server_info, dict):
        add_violation(
            violations,
            "runtime-probe-identity",
            str(target.get("path", "")),
            "serverInfo is not an object",
        )
        server_info = {}
    if server_info.get("name") != "re-discipline-knowledge":
        add_violation(
            violations,
            "runtime-probe-identity",
            str(target.get("path", "")),
            f"server name {server_info.get('name')!r}",
        )
    server_version = server_info.get("version")
    if server_version != expected_version:
        add_violation(
            violations,
            "runtime-probe-version",
            str(target.get("path", "")),
            f"{server_version!r} != {expected_version!r}",
        )
    tools_result = responses[2].get("result")
    tool_rows = tools_result.get("tools", []) if isinstance(tools_result, dict) else []
    tools = [
        row.get("name")
        for row in tool_rows
        if isinstance(row, dict)
    ]
    if tools != EXPECTED_MCP_TOOLS:
        add_violation(
            violations,
            "runtime-tool-surface",
            str(target.get("path", "")),
            json.dumps(tools),
        )


def is_allowlisted(relative: str, label: str = "") -> bool:
    path = relative.replace("\\", "/")
    return (
        (path, label) in ENFORCEMENT_LEGACY_MATCHES
        or
        path.startswith("docs/migrations/")
        or path.startswith("docs/rfcs/")
        or path.startswith("skills/migrate-project/")
        or path.startswith("knowledge/schemas/migration-")
        or re.fullmatch(r"knowledge/internal/knowledge/migration[^/]*\.go", path) is not None
        or path == "knowledge/internal/knowledge/migrate.go"
    )


def is_scannable(relative: str, path: Path) -> bool:
    normalized = relative.replace("\\", "/")
    if path.suffix.lower() not in TEXT_SUFFIXES:
        return False
    if normalized.startswith("knowledge/bin/") or normalized.startswith("knowledge/evals/"):
        return False
    if normalized.endswith("_test.go"):
        return False
    return True


def inventory_tree(root: Path) -> tuple[list[dict[str, object]], list[str]]:
    inventory: list[dict[str, object]] = []
    links: list[str] = []
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        relative = path.relative_to(root).as_posix()
        if path.is_symlink():
            links.append(relative)
            continue
        if not path.is_file():
            continue
        data = path.read_bytes()
        inventory.append({"path": relative, "size": len(data), "sha256": sha256_bytes(data)})
    return inventory, links


def digest_inventory(inventory: list[dict[str, object]], include_size: bool) -> str:
    digest = hashlib.sha256()
    for row in inventory:
        fields = [str(row["path"]), str(row["sha256"])]
        if include_size:
            fields.insert(1, str(row["size"]))
        digest.update("\0".join(fields).encode("utf-8") + b"\n")
    return "sha256:" + digest.hexdigest()


def validate_runtime_release(
    stage: Path,
    plugin_version: str,
    violations: list[dict[str, str]],
) -> None:
    if plugin_version != EXPECTED_PLUGIN_VERSION:
        add_violation(
            violations,
            "plugin-version-mismatch",
            ".claude-plugin/plugin.json",
            f"{plugin_version!r} != {EXPECTED_PLUGIN_VERSION!r}",
        )
    knowledge_root = stage / "knowledge"
    version_sources: list[tuple[str, str]] = []
    try:
        codex_manifest = json.loads(
            (stage / ".codex-plugin" / "plugin.json").read_text(encoding="utf-8")
        )
        version_sources.append(
            (".codex-plugin/plugin.json", str(codex_manifest.get("version", "")))
        )
        version_sources.append(
            (
                "knowledge/internal/knowledge/types.go",
                read_version_constant(
                    stage / "knowledge" / "internal" / "knowledge" / "types.go",
                    "RuntimeVersion",
                ),
            )
        )
        version_sources.append(
            (
                "knowledge/cmd/re-discipline-knowledge-packager/main.go",
                read_version_constant(
                    stage
                    / "knowledge"
                    / "cmd"
                    / "re-discipline-knowledge-packager"
                    / "main.go",
                    "runtimeVersion",
                ),
            )
        )
        model_manifest = json.loads(
            (stage / "knowledge" / "models" / "manifest.json").read_text(
                encoding="utf-8"
            )
        )
        version_sources.append(
            (
                "knowledge/models/manifest.json",
                str(model_manifest.get("runtime", {}).get("version", "")),
            )
        )
        runtime_manifest_path = stage / "knowledge" / "bin" / "manifest.json"
        runtime_manifest = json.loads(runtime_manifest_path.read_text(encoding="utf-8"))
        version_sources.append(
            (
                "knowledge/bin/manifest.json",
                str(runtime_manifest.get("runtime", {}).get("version", "")),
            )
        )
    except (OSError, json.JSONDecodeError, AttributeError) as error:
        add_violation(
            violations,
            "runtime-manifest-invalid",
            "knowledge/bin/manifest.json",
            str(error),
        )
        return

    expected_build_id = ""
    try:
        expected_build_id = compute_runtime_build_id(knowledge_root)
    except (OSError, ValueError) as error:
        add_violation(
            violations,
            "runtime-build-identity",
            "knowledge/bin/manifest.json",
            str(error),
        )
    pinned_go = ""
    try:
        pinned_go = read_go_toolchain(knowledge_root / "go.mod")
    except (OSError, ValueError) as error:
        add_violation(
            violations,
            "runtime-build-toolchain",
            "knowledge/go.mod",
            str(error),
        )

    for path, version in version_sources:
        if version != plugin_version:
            add_violation(
                violations,
                "runtime-version-mismatch",
                path,
                f"{version!r} != plugin {plugin_version!r}",
            )

    manifest_relative = "knowledge/bin/manifest.json"
    bin_root = stage / "knowledge" / "bin"
    if set(runtime_manifest) != EXPECTED_RUNTIME_MANIFEST_KEYS:
        add_violation(
            violations,
            "runtime-manifest-schema",
            manifest_relative,
            f"keys={sorted(runtime_manifest)}",
        )
    if (
        runtime_manifest.get("$schema")
        != "../schemas/runtime-package-manifest.schema.json"
    ):
        add_violation(
            violations,
            "runtime-manifest-schema",
            manifest_relative,
            repr(runtime_manifest.get("$schema")),
        )
    if runtime_manifest.get("schemaVersion") != 1:
        add_violation(
            violations,
            "runtime-manifest-schema",
            manifest_relative,
            f"schemaVersion={runtime_manifest.get('schemaVersion')!r}",
        )
    runtime_identity = runtime_manifest.get("runtime")
    declared_build_id = ""
    if not isinstance(runtime_identity, dict):
        add_violation(
            violations, "runtime-manifest-shape", manifest_relative, "runtime"
        )
    else:
        declared_build_id = str(runtime_identity.get("buildId", ""))
        if set(runtime_identity) != {"name", "version", "buildId"}:
            add_violation(
                violations,
                "runtime-manifest-identity",
                manifest_relative,
                f"keys={sorted(runtime_identity)}",
            )
        if runtime_identity.get("name") != "re-discipline-knowledge":
            add_violation(
                violations,
                "runtime-manifest-identity",
                manifest_relative,
                f"name={runtime_identity.get('name')!r}",
            )
        if not re.fullmatch(
            r"sha256:[0-9a-f]{64}", declared_build_id
        ):
            add_violation(
                violations,
                "runtime-manifest-identity",
                manifest_relative,
                f"buildId={runtime_identity.get('buildId')!r}",
            )
        if expected_build_id and declared_build_id != expected_build_id:
            add_violation(
                violations,
                "runtime-build-identity",
                manifest_relative,
                f"{declared_build_id!r} != source {expected_build_id!r}",
            )

    build = runtime_manifest.get("build")
    expected_target_order = (
        "windows-amd64,windows-arm64,linux-amd64,linux-arm64,"
        "darwin-amd64,darwin-arm64"
    )
    if not isinstance(build, dict):
        add_violation(
            violations, "runtime-manifest-shape", manifest_relative, "build"
        )
    else:
        flags = build.get("flags")
        required_build_id = expected_build_id or declared_build_id
        expected_flags = [
            "-trimpath",
            "-buildvcs=false",
            "-ldflags=-s -w -buildid= -X "
            + RUNTIME_BUILD_ID_PATH
            + "="
            + required_build_id,
        ]
        expected_environment = [
            "CGO_ENABLED=0",
            "GOAMD64=v1",
            "GOARM64=v8.0",
            "GOENV=off",
            "GOEXPERIMENT=",
            "GOFIPS140=off",
            "GOFLAGS=-mod=readonly",
            "GOWORK=off",
        ]
        if (
            set(build)
            != {"goToolchain", "cgoEnabled", "flags", "environment", "targetOrder"}
            or build.get("cgoEnabled") is not False
            or build.get("targetOrder") != expected_target_order
            or build.get("goToolchain") != pinned_go
            or flags != expected_flags
            or build.get("environment") != expected_environment
        ):
            add_violation(
                violations,
                "runtime-manifest-build",
                manifest_relative,
                json.dumps(build, sort_keys=True),
            )

    target_rows = runtime_manifest.get("targets")
    actual_targets = (
        [
            (
                str(row.get("kind", "")),
                str(row.get("goos", "")),
                str(row.get("goarch", "")),
                str(row.get("path", "")).replace("\\", "/"),
                str(row.get("mode", "")),
            )
            for row in target_rows
            if isinstance(row, dict)
        ]
        if isinstance(target_rows, list)
        else []
    )
    if actual_targets != EXPECTED_RUNTIME_TARGETS:
        add_violation(
            violations,
            "runtime-target-surface",
            manifest_relative,
            json.dumps(actual_targets),
        )

    launcher_rows = runtime_manifest.get("launchers")
    actual_launchers = (
        [
            (
                str(row.get("kind", "")),
                str(row.get("goos", "")),
                str(row.get("goarch", "")),
                str(row.get("path", "")).replace("\\", "/"),
                str(row.get("mode", "")),
            )
            for row in launcher_rows
            if isinstance(row, dict)
        ]
        if isinstance(launcher_rows, list)
        else []
    )
    if actual_launchers != EXPECTED_RUNTIME_LAUNCHERS:
        add_violation(
            violations,
            "runtime-launcher-surface",
            manifest_relative,
            json.dumps(actual_launchers),
        )

    expected_sums: dict[str, str] = {}
    for group in ("targets", "launchers"):
        rows = runtime_manifest.get(group, [])
        if not isinstance(rows, list):
            add_violation(
                violations, "runtime-manifest-shape", manifest_relative, group
            )
            continue
        for row in rows:
            if not isinstance(row, dict):
                add_violation(
                    violations, "runtime-manifest-shape", manifest_relative, group
                )
                continue
            expected_keys = {"kind", "path", "sha256", "size", "mode"}
            if group == "targets" or row.get("kind") == "windows-architecture-dispatch":
                expected_keys.update({"goos", "goarch"})
            if set(row) != expected_keys:
                add_violation(
                    violations,
                    "runtime-manifest-record-shape",
                    manifest_relative,
                    f"{group}:{row.get('path')}: keys={sorted(row)}",
                )
            verified = validate_manifest_file(
                bin_root, row, manifest_relative, violations
            )
            if verified:
                expected_sums[verified[0]] = verified[1]
                if (
                    expected_build_id
                    and (
                        group == "targets"
                        or row.get("kind") == "windows-architecture-dispatch"
                    )
                    and expected_build_id.encode("ascii")
                    not in (bin_root / verified[0]).read_bytes()
                ):
                    add_violation(
                        violations,
                        "runtime-binary-build-identity",
                        verified[0],
                        f"missing {expected_build_id}",
                    )

    declared_assets = runtime_manifest.get("sharedAssets", [])
    declared_asset_rows: dict[str, dict[str, object]] = {}
    if not isinstance(declared_assets, list):
        add_violation(
            violations, "runtime-manifest-shape", manifest_relative, "sharedAssets"
        )
        declared_assets = []
    for row in declared_assets:
        if not isinstance(row, dict):
            add_violation(
                violations, "runtime-manifest-shape", manifest_relative, "sharedAssets"
            )
            continue
        if set(row) != {"kind", "path", "sha256", "size", "mode"}:
            add_violation(
                violations,
                "runtime-manifest-record-shape",
                manifest_relative,
                f"sharedAssets:{row.get('path')}: keys={sorted(row)}",
            )
        if row.get("mode") != "0644":
            add_violation(
                violations,
                "runtime-manifest-asset-mode",
                manifest_relative,
                f"{row.get('path')}: {row.get('mode')!r}",
            )
        relative = str(row.get("path", "")).replace("\\", "/")
        if relative in declared_asset_rows:
            add_violation(
                violations,
                "runtime-manifest-duplicate",
                manifest_relative,
                relative,
            )
        declared_asset_rows[relative] = row
        verified = validate_manifest_file(
            stage / "knowledge", row, manifest_relative, violations
        )
        if verified:
            expected_sums["../" + verified[0]] = verified[1]

    expected_asset_rows: dict[str, str] = {}
    for relative_root in SHARED_ASSET_ROOTS:
        root = stage / "knowledge" / relative_root
        for asset in sorted(path for path in root.rglob("*") if path.is_file()):
            relative = asset.relative_to(stage / "knowledge").as_posix()
            try:
                expected_asset_rows[relative] = shared_asset_kind(relative)
            except ValueError as error:
                add_violation(
                    violations, "runtime-asset-kind", relative, str(error)
                )
    declared_pairs = {
        path: str(row.get("kind", "")) for path, row in declared_asset_rows.items()
    }
    expected_asset_order = sorted(
        ((kind, path) for path, kind in expected_asset_rows.items()),
        key=lambda row: row[1],
    )
    declared_asset_order = [
        (str(row.get("kind", "")), str(row.get("path", "")).replace("\\", "/"))
        for row in declared_assets
        if isinstance(row, dict)
    ]
    if (
        declared_pairs.keys() == expected_asset_rows.keys()
        and declared_asset_order != expected_asset_order
    ):
        add_violation(
            violations,
            "runtime-manifest-asset-order",
            manifest_relative,
            "sharedAssets must be complete and sorted by path",
        )
    for relative in sorted(expected_asset_rows.keys() - declared_pairs.keys()):
        add_violation(
            violations,
            "runtime-manifest-missing-asset",
            manifest_relative,
            relative,
        )
    for relative in sorted(declared_pairs.keys() - expected_asset_rows.keys()):
        add_violation(
            violations,
            "runtime-manifest-stale-asset",
            manifest_relative,
            relative,
        )
    for relative in sorted(expected_asset_rows.keys() & declared_pairs.keys()):
        if expected_asset_rows[relative] != declared_pairs[relative]:
            add_violation(
                violations,
                "runtime-manifest-asset-kind",
                manifest_relative,
                f"{relative}: {declared_pairs[relative]!r} != "
                f"{expected_asset_rows[relative]!r}",
            )

    notices = runtime_manifest.get("notices")
    if isinstance(notices, dict):
        if set(notices) != {"kind", "path", "sha256", "size", "mode"}:
            add_violation(
                violations,
                "runtime-manifest-record-shape",
                manifest_relative,
                f"notices: keys={sorted(notices)}",
            )
        notice_surface = (
            str(notices.get("kind", "")),
            str(notices.get("path", "")).replace("\\", "/"),
            str(notices.get("mode", "")),
        )
        if notice_surface != (
            "third-party-notices",
            "THIRD_PARTY_NOTICES.md",
            "0644",
        ):
            add_violation(
                violations,
                "runtime-notices-surface",
                manifest_relative,
                json.dumps(notice_surface),
            )
        verified = validate_manifest_file(
            bin_root, notices, manifest_relative, violations
        )
        if verified:
            expected_sums[verified[0]] = verified[1]
    else:
        add_violation(
            violations, "runtime-manifest-shape", manifest_relative, "notices"
        )
    expected_sums["manifest.json"] = sha256_bytes(runtime_manifest_path.read_bytes())

    expected_bin_files = {
        "manifest.json",
        "SHA256SUMS",
        *(
            relative
            for relative in expected_sums
            if not relative.startswith("../")
        ),
    }
    actual_bin_files = {
        path.relative_to(bin_root).as_posix()
        for path in bin_root.rglob("*")
        if path.is_file()
    }
    for relative in sorted(expected_bin_files - actual_bin_files):
        add_violation(
            violations,
            "runtime-bin-missing-file",
            "knowledge/bin",
            relative,
        )
    for relative in sorted(actual_bin_files - expected_bin_files):
        add_violation(
            violations,
            "runtime-bin-unexpected-file",
            "knowledge/bin",
            relative,
        )

    sums_path = bin_root / "SHA256SUMS"
    try:
        actual_sums = parse_checksum_file(sums_path)
    except (OSError, ValueError) as error:
        add_violation(
            violations, "runtime-checksums-invalid", "knowledge/bin/SHA256SUMS", str(error)
        )
    else:
        if list(actual_sums) != sorted(actual_sums):
            add_violation(
                violations,
                "runtime-checksum-order",
                "knowledge/bin/SHA256SUMS",
                "entries must be sorted by canonical path",
            )
        for relative in sorted(expected_sums.keys() - actual_sums.keys()):
            add_violation(
                violations,
                "runtime-checksum-missing",
                "knowledge/bin/SHA256SUMS",
                relative,
            )
        for relative in sorted(actual_sums.keys() - expected_sums.keys()):
            add_violation(
                violations,
                "runtime-checksum-stale",
                "knowledge/bin/SHA256SUMS",
                relative,
            )
        for relative in sorted(expected_sums.keys() & actual_sums.keys()):
            if expected_sums[relative] != actual_sums[relative]:
                add_violation(
                    violations,
                    "runtime-checksum-mismatch",
                    "knowledge/bin/SHA256SUMS",
                    relative,
                )

    probe_runtime_tools(stage, runtime_manifest, plugin_version, violations)


def audit_staged_tree(stage: Path) -> dict[str, object]:
    inventory, links = inventory_tree(stage)
    paths = {str(row["path"]) for row in inventory}
    violations: list[dict[str, str]] = []
    allowlisted: list[dict[str, str]] = []

    for link in links:
        violations.append({"kind": "link", "path": link, "detail": "release tree contains a link"})
    for forbidden in sorted(FORBIDDEN_PATHS):
        if forbidden in paths or any(path.startswith(forbidden + "/") for path in paths):
            add_violation(
                violations, "forbidden-path", forbidden, "retired path is present"
            )
    for required in sorted(REQUIRED_PATHS - paths):
        add_violation(
            violations, "missing-path", required, "required 0.8 path is absent"
        )

    skill_paths = {path for path in paths if re.fullmatch(r"skills/[^/]+/SKILL\.md", path)}
    expected_skill_paths = {f"skills/{name}/SKILL.md" for name in EXPECTED_SKILLS}
    for unexpected in sorted(skill_paths - expected_skill_paths):
        add_violation(
            violations,
            "unexpected-skill",
            unexpected,
            "skill is outside the 0.8 surface",
        )
    for missing in sorted(expected_skill_paths - skill_paths):
        add_violation(
            violations, "missing-skill", missing, "required 0.8 skill is absent"
        )

    agent_paths = {path for path in paths if re.fullmatch(r"agents/[^/]+\.md", path)}
    if agent_paths != {"agents/knowledge-curator.md"}:
        add_violation(
            violations, "agent-surface", "agents/", ",".join(sorted(agent_paths))
        )

    for row in inventory:
        relative = str(row["path"])
        path = stage / relative
        if not is_scannable(relative, path):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        for label, pattern in LEGACY_PATTERNS.items():
            if not pattern.search(text):
                continue
            match = {"kind": label, "path": relative, "detail": pattern.pattern}
            if is_allowlisted(relative, label):
                allowlisted.append(match)
            else:
                violations.append(match)

    plugin_version = ""
    try:
        plugin_manifest = json.loads(
            (stage / ".claude-plugin" / "plugin.json").read_text(encoding="utf-8")
        )
        if not isinstance(plugin_manifest, dict):
            raise ValueError("plugin manifest root is not an object")
        plugin_version = str(plugin_manifest.get("version", ""))
    except (OSError, ValueError, json.JSONDecodeError) as error:
        add_violation(
            violations,
            "plugin-manifest-invalid",
            ".claude-plugin/plugin.json",
            str(error),
        )
    validate_runtime_release(stage, plugin_version, violations)
    return {
        "auditVersion": 2,
        "pluginVersion": plugin_version,
        "fileCount": len(inventory),
        "artifactDigest": digest_inventory(inventory, include_size=False),
        "inventoryDigest": digest_inventory(inventory, include_size=True),
        "allowlistedLegacyMatches": sorted(allowlisted, key=lambda row: (row["path"], row["kind"])),
        "violations": sorted(violations, key=lambda row: (row["path"], row["kind"])),
    }


def audit_plugin(plugin: Path) -> dict[str, object]:
    plugin = plugin.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="re-discipline-package-audit-") as temporary:
        stage = Path(temporary) / "re-discipline"
        shutil.copytree(plugin, stage, symlinks=True)
        return audit_staged_tree(stage)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("plugin", type=Path)
    args = parser.parse_args()
    result = audit_plugin(args.plugin)
    print(json.dumps(result, indent=2, sort_keys=True))
    return 1 if result["violations"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
