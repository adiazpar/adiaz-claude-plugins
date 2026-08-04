"""Produce strict external evidence for a re-discipline 0.8 migration.

The formal runner launches real Claude Code and Codex processes.  It compares
the last released 0.7 runtime over an exact disposable legacy checkout with
the current runtime over an already-activated disposable migration.  Eval
files are inventoried and withheld before either agent process starts.

The module deliberately has a test-only executor seam.  Formal mode rejects
that seam (and every executor except :class:`SubprocessExecutor`) so unit tests
cannot accidentally become certification evidence.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Mapping, Protocol, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_migration_pilot import (  # noqa: E402
    DIGEST_RE,
    REVISION_RE,
    _git,
    _pair_digest,
    _repo_binding,
    _tracked_rows,
)


class EvidenceError(ValueError):
    """Raised when captured execution cannot support certification."""


BLINDED_SUITE = "migration-blinded-agent-evaluation-v1"
HOST_SUITE = "migration-host-conformance-v1"
EXECUTION_SUITE = "migration-external-execution-manifest-v1"
BLINDED_PROTOCOL = (
    "Compare the same hidden migration task under legacy and compiled context. "
    "Record the exact request, answer, citations, opened sources, expansions, "
    "durability labels, unsupported claims, decisions, and context-token cost. "
    "The compiled arm must be factually and decision non-inferior, use no more "
    "context, preserve evidence traceability, and avoid full-corpus reads."
)
HOST_PROTOCOL = (
    "Exercise the actual MCP, CLI, and every configured agent-host adapter with "
    "captured request, transport result, normalized semantic result, and expected "
    "failure. Require discovery, status, retrieval, expansion, role-boundary "
    "refusal, bounded recovery, and local fallback coverage with equivalent "
    "semantic digests."
)
MIGRATION_STATE = ".re-discipline/migration/0.8/state.json"
CURRENT_PLUGIN = "plugins/re-discipline"
CURRENT_ASSETS = f"{CURRENT_PLUGIN}/knowledge"
EVAL_ROOT = ".re-discipline/knowledge/evals"
PRIMARY_PROFILE = "hybrid-no-rerank-v1"
MAX_CAPTURE_BYTES = 16 * 1024 * 1024
MAX_QUOTE_BYTES = 4096
FORMAL_MIN_TIMEOUT = 60

MCP_SERVER_NAME = "re-discipline-knowledge"
MCP_PROTOCOL_VERSION = "2025-11-25"
HOST_TRANSPORTS = {
    "mcp": "mcp-stdio-jsonrpc",
    "cli": "cli-process-json",
    "claude": "claude-code-plugin",
    "codex": "codex-plugin",
}
AGENT_IDENTITIES = {"claude": "claude-code", "codex": "codex-cli"}
# The conformance surface is fixed so that every host issues byte-identical
# tool arguments and the verifier can compare normalized semantic digests.
CONFORMANCE_QUERY = (
    "What does the project profile require before promoting a finding to durable truth?"
)
CONFORMANCE_LEASE = "migration-host-conformance-lease"
CONFORMANCE_READ_PATH = ".re-discipline/project-profile.md"
CONFORMANCE_TOKEN_BUDGET = 1024
ROLE_BOUNDARY_ROLE = "curator"
ROLE_BOUNDARY_SURFACE = "context_pack_materialize"
ROLE_BOUNDARY_MESSAGE = "context pack role must be manager or drafter"
MCP_UNAVAILABLE_MESSAGE = (
    "re-discipline knowledge MCP server is unavailable in this session"
)
CLI_COMMANDS = {
    "state": "state",
    "query": "query",
    "read": "read",
    "context_pack_materialize": "context-pack-materialize",
}

HOST_MATRIX: dict[str, tuple[str, ...]] = {
    "mcp": (
        "discovery",
        "status",
        "retrieval",
        "expansion",
        "role-boundary",
        "bounded-recovery",
    ),
    "cli": (
        "status",
        "retrieval",
        "expansion",
        "role-boundary",
        "bounded-recovery",
    ),
    "claude": (
        "discovery",
        "status",
        "retrieval",
        "expansion",
        "role-boundary",
        "bounded-recovery",
        "local-fallback",
    ),
    "codex": (
        "discovery",
        "status",
        "retrieval",
        "expansion",
        "role-boundary",
        "bounded-recovery",
        "local-fallback",
    ),
}

CASE_OUTCOME_FIELDS = (
    "caseId",
    "split",
    "topic",
    "paths",
    "tiers",
    "chunkIds",
    "contentHashes",
    "relevantPaths",
    "relevantRanks",
    "hardNegativeHits",
    "expectedCitationsFound",
    "estimatedTokens",
    "returnedUniquePaths",
    "expectedFound",
    "completeEvidence",
    "authoritySafe",
    "citationMetadataSafe",
    "citationSafe",
    "corpusMatched",
    "abstentionCorrect",
    "budgetSafe",
    "replayIdentical",
    "minimumTokenBudget",
    "qualityGateApplicable",
    "safetyPassed",
    "qualityPassed",
    "gatePassed",
    "returnedTokens",
    "relevantTokens",
    "duplicateTokens",
    "staleResults",
    "latencyMillis",
)
EVAL_CASE_FIELDS = (
    "id",
    "role",
    "topic",
    "split",
    "query",
    "queryClass",
    "vocabularyPolicy",
    "allowedTiers",
    "corpusSnapshot",
    "expectedPaths",
    "gradedRelevantPaths",
    "minimumEvidencePaths",
    "hardNegativePaths",
    "expectedCitations",
    "forbiddenTiers",
    "tokenBudget",
    "answerable",
    "evidencePins",
)


def _fail(field: str, message: str) -> None:
    raise EvidenceError(f"{field}: {message}")


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace(
        "+00:00", "Z"
    )


def _sha256(body: bytes) -> str:
    return "sha256:" + hashlib.sha256(body).hexdigest()


def _normalized_digest(value: Any) -> str | None:
    """Normalize a tool-reported hash to the canonical sha256:hex form.

    The last released 0.7 runtime reports bare lowercase hex digests while
    0.8 reports the prefixed form; both arms' captured hashes are compared in
    one canonical vocabulary.
    """

    if not isinstance(value, str):
        return None
    candidate = value.strip().lower()
    if candidate.startswith("sha256:"):
        candidate = candidate[len("sha256:"):]
    if re.fullmatch(r"[0-9a-f]{64}", candidate):
        return "sha256:" + candidate
    return None


def _strict_json(body: bytes, field: str) -> Any:
    if len(body) > MAX_CAPTURE_BYTES:
        _fail(field, f"exceeds {MAX_CAPTURE_BYTES} bytes")
    try:
        text = body.decode("utf-8", errors="strict")
        decoder = json.JSONDecoder()
        value, end = decoder.raw_decode(text)
        if text[end:].strip():
            _fail(field, "contains a trailing JSON value")
        return value
    except (UnicodeError, json.JSONDecodeError) as error:
        raise EvidenceError(f"{field}: invalid strict UTF-8 JSON: {error}") from error


def _canonical_bytes(value: Any) -> bytes:
    return json.dumps(
        value, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")


def _canonical_digest(value: Any) -> str:
    return _sha256(_canonical_bytes(value))


def _stable_bytes(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


class RawJSON:
    """Bytes already encoded exactly as the Go verifier will observe them.

    A ``json.RawMessage`` struct field is re-emitted verbatim (after Go's
    compaction) rather than re-encoded from a decoded value, so its digest
    contribution is the artifact's own bytes, not a re-serialization.
    """

    __slots__ = ("body",)

    def __init__(self, body: bytes) -> None:
        self.body = body


_GO_SHORT_ESCAPES = {0x22: b'\\"', 0x5C: b"\\\\", 0x0A: b"\\n", 0x0D: b"\\r", 0x09: b"\\t"}
_COMPACT_ESCAPES = (
    (b"<", b"\\u003c"),
    (b">", b"\\u003e"),
    (b"&", b"\\u0026"),
    ("\u2028".encode("utf-8"), b"\\u2028"),
    ("\u2029".encode("utf-8"), b"\\u2029"),
)


def _go_string(value: str) -> bytes:
    # encoding/json string encoding with the default escapeHTML setting.
    out = bytearray(b'"')
    for character in value:
        point = ord(character)
        short = _GO_SHORT_ESCAPES.get(point)
        if short is not None:
            out += short
        elif character in "<>&" or point < 0x20 or point in (0x2028, 0x2029):
            out += f"\\u{point:04x}".encode("ascii")
        else:
            out += character.encode("utf-8")
    out += b'"'
    return bytes(out)


def _go_json_bytes(value: Any, *, sort_keys: bool = False) -> bytes:
    """Reproduce Go ``json.Marshal`` for the values this module emits.

    Go preserves struct-field order and sorts map keys, so callers pass an
    already-ordered mapping for a struct and request ``sort_keys`` for a value
    the verifier decodes into ``any`` before hashing.
    """

    if isinstance(value, RawJSON):
        return value.body
    if value is None:
        return b"null"
    if isinstance(value, bool):
        return b"true" if value else b"false"
    if isinstance(value, str):
        return _go_string(value)
    if isinstance(value, int):
        return str(value).encode("ascii")
    if isinstance(value, float):
        if not value.is_integer():
            _fail("digest", f"float {value!r} has no reproducible Go encoding here")
        return str(int(value)).encode("ascii")
    if isinstance(value, Mapping):
        rows = list(value.items())
        if sort_keys:
            rows.sort(key=lambda row: str(row[0]))
        return (
            b"{"
            + b",".join(
                _go_string(str(key)) + b":" + _go_json_bytes(child, sort_keys=sort_keys)
                for key, child in rows
            )
            + b"}"
        )
    if isinstance(value, (list, tuple)):
        return (
            b"["
            + b",".join(_go_json_bytes(row, sort_keys=sort_keys) for row in value)
            + b"]"
        )
    _fail("digest", f"unsupported value {value!r}")
    raise AssertionError("unreachable")


def _go_digest(value: Any, *, sort_keys: bool = False) -> str:
    return _sha256(_go_json_bytes(value, sort_keys=sort_keys))


def _raw_capture(value: Any) -> RawJSON:
    """Encode a captured payload exactly as Go compaction will see our file."""

    body = json.dumps(
        value, ensure_ascii=False, sort_keys=False, separators=(",", ":")
    ).encode("utf-8")
    for needle, replacement in _COMPACT_ESCAPES:
        body = body.replace(needle, replacement)
    return RawJSON(body)


def _go_number(value: float) -> Any:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        _fail("score", f"{value!r} is not a measured number")
    if isinstance(value, float) and value.is_integer():
        return int(value)
    return value


def _go_struct_value(
    value: Mapping[str, Any], fields: Sequence[str], *, omit_empty: Iterable[str] = ()
) -> dict[str, Any]:
    omitted = set(omit_empty)
    result: dict[str, Any] = {}
    for field in fields:
        if field not in value:
            continue
        child = value[field]
        if field in omitted and child in (None, "", [], {}):
            continue
        result[field] = child
    return result


def _go_struct_digest(
    value: Mapping[str, Any], fields: Sequence[str], *, omit_empty: Iterable[str] = ()
) -> str:
    # Go encoding/json preserves struct-field order and sorts map keys.  Python
    # preserves insertion order; nested dictionaries are normalized separately.
    ordered = _go_struct_value(value, fields, omit_empty=omit_empty)
    return _go_digest(ordered)


def _stable_id(prefix: str, *values: str) -> str:
    digest = hashlib.sha256("\x00".join(values).encode("utf-8")).hexdigest()
    return f"{prefix}-{digest[:20]}"


def _safe_relative(value: str, *, field: str) -> PurePosixPath:
    if not isinstance(value, str) or not value or "\\" in value:
        _fail(field, "must be a non-empty forward-slash relative path")
    relative = PurePosixPath(value)
    if (
        relative.is_absolute()
        or any(part in ("", ".", "..") for part in relative.parts)
        or re.match(r"^[A-Za-z]:", value)
    ):
        _fail(field, "must be canonical and project-relative")
    return relative


def _contained(root: Path, relative: str, *, field: str) -> Path:
    rel = _safe_relative(relative, field=field)
    resolved_root = root.resolve(strict=True)
    candidate = resolved_root.joinpath(*rel.parts)
    try:
        candidate.resolve(strict=False).relative_to(resolved_root)
    except ValueError:
        _fail(field, "escapes its declared root")
    return candidate


def _regular_bytes(path: Path, *, field: str) -> bytes:
    if path.is_symlink() or not path.is_file():
        _fail(field, "must be a regular non-link file")
    info = path.stat()
    if info.st_size < 1 or info.st_size > MAX_CAPTURE_BYTES:
        _fail(field, "has an invalid bounded size")
    return path.read_bytes()


def _artifact_bytes(path: Path, *, field: str) -> bytes:
    if path.is_symlink() or not path.is_file():
        _fail(field, "must be a regular non-link file")
    info = path.stat()
    if info.st_size > MAX_CAPTURE_BYTES:
        _fail(field, "exceeds the artifact size bound")
    return path.read_bytes()


def _inventory(root: Path, *, include_git: bool = False) -> list[dict[str, Any]]:
    if root.is_symlink() or not root.is_dir():
        _fail("inventory.root", "must be a real directory")
    rows: list[dict[str, Any]] = []
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        relative = path.relative_to(root).as_posix()
        if not include_git and (relative == ".git" or relative.startswith(".git/")):
            continue
        info = path.lstat()
        if path.is_symlink() or bool(
            getattr(info, "st_file_attributes", 0)
            & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0x400)
        ):
            _fail("inventory", f"link or reparse point is forbidden: {relative}")
        if path.is_dir():
            continue
        if not path.is_file():
            _fail("inventory", f"non-regular entry is forbidden: {relative}")
        body = path.read_bytes()
        rows.append(
            {"path": relative, "digest": _sha256(body), "sizeBytes": len(body)}
        )
    return rows


def _inventory_digest(rows: Sequence[Mapping[str, Any]]) -> str:
    pairs = [(str(row["path"]), str(row["digest"])) for row in rows]
    return _pair_digest(pairs)


@dataclass(frozen=True)
class ArtifactRef:
    path: str
    digest: str
    size_bytes: int

    def json(self) -> dict[str, Any]:
        return {
            "path": self.path,
            "digest": self.digest,
            "sizeBytes": self.size_bytes,
        }


class ArtifactStore:
    """Write a staging tree whose references already name final project paths."""

    def __init__(self, root: Path, destination_prefix: str) -> None:
        self.root = root.resolve(strict=False)
        self.destination_prefix = _safe_relative(
            destination_prefix, field="destinationPrefix"
        ).as_posix()
        if self.root.exists() or self.root.is_symlink():
            _fail("output", "refuses an existing output path")
        self.root.mkdir(parents=True)
        self._refs: dict[str, ArtifactRef] = {}

    def write_bytes(self, relative: str, body: bytes) -> ArtifactRef:
        rel = _safe_relative(relative, field="artifact.path").as_posix()
        if len(body) > MAX_CAPTURE_BYTES:
            _fail("artifact", "exceeds the artifact size bound")
        if rel in self._refs:
            _fail("artifact", f"duplicate artifact path {rel}")
        path = _contained(self.root, rel, field="artifact.path")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        final = f"{self.destination_prefix}/{rel}"
        ref = ArtifactRef(final, _sha256(body), len(body))
        self._refs[rel] = ref
        return ref

    def write_json(self, relative: str, value: Any) -> ArtifactRef:
        return self.write_bytes(relative, _stable_bytes(value))

    def physical_path(self, ref: ArtifactRef) -> Path:
        prefix = self.destination_prefix + "/"
        if not ref.path.startswith(prefix):
            _fail("artifact.path", "does not use this store's destination prefix")
        relative = ref.path[len(prefix) :]
        path = _contained(self.root, relative, field="artifact.path")
        if _sha256(_artifact_bytes(path, field=ref.path)) != ref.digest:
            _fail("artifact", f"staged bytes changed: {ref.path}")
        return path

    def copy_map(self) -> list[dict[str, Any]]:
        rows = []
        for relative, ref in sorted(self._refs.items()):
            rows.append(
                {
                    "sourcePath": relative,
                    "destinationPath": ref.path,
                    "sha256": ref.digest,
                    "sizeBytes": ref.size_bytes,
                }
            )
        return rows


@dataclass(frozen=True)
class ProcessCapture:
    argv: tuple[str, ...]
    cwd: str
    stdout: bytes
    stderr: bytes
    exit_code: int
    started_at: str
    finished_at: str


class Executor(Protocol):
    formal: bool

    def run(
        self,
        argv: Sequence[str],
        *,
        cwd: Path,
        env: Mapping[str, str] | None = None,
        stdin: bytes | None = None,
        timeout_seconds: int,
    ) -> ProcessCapture: ...


class SubprocessExecutor:
    """The only executor accepted by formal mode."""

    formal = True

    def run(
        self,
        argv: Sequence[str],
        *,
        cwd: Path,
        env: Mapping[str, str] | None = None,
        stdin: bytes | None = None,
        timeout_seconds: int,
    ) -> ProcessCapture:
        if not argv or timeout_seconds < 1:
            _fail("process", "argv and positive timeout are required")
        started = _utc_now()
        try:
            result = subprocess.run(
                list(argv),
                cwd=cwd,
                env=dict(env) if env is not None else None,
                input=stdin,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
                timeout=timeout_seconds,
                shell=False,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise EvidenceError(f"process {list(argv)!r} failed: {error}") from error
        finished = _utc_now()
        if len(result.stdout) > MAX_CAPTURE_BYTES or len(result.stderr) > MAX_CAPTURE_BYTES:
            _fail("process", "stdout or stderr exceeded the evidence bound")
        return ProcessCapture(
            tuple(str(value) for value in argv),
            str(cwd.resolve(strict=True)),
            result.stdout,
            result.stderr,
            result.returncode,
            started,
            finished,
        )


def _require_formal_executor(executor: Executor, *, formal: bool) -> None:
    if formal and (
        type(executor) is not SubprocessExecutor
        or getattr(executor, "formal", False) is not True
        or os.environ.get("RE_DISCIPLINE_EXTERNAL_EVIDENCE_FAKE")
    ):
        _fail("executor", "formal mode requires the built-in real-process executor")


def _json_lines(body: bytes, *, field: str) -> list[dict[str, Any]]:
    if len(body) > MAX_CAPTURE_BYTES:
        _fail(field, "exceeds the transcript bound")
    rows: list[dict[str, Any]] = []
    try:
        text = body.decode("utf-8", errors="strict")
    except UnicodeError as error:
        raise EvidenceError(f"{field}: is not strict UTF-8: {error}") from error
    for number, line in enumerate(text.splitlines(), start=1):
        if not line.strip():
            continue
        value = _strict_json(line.encode("utf-8"), f"{field}[{number}]")
        if not isinstance(value, dict):
            _fail(field, f"line {number} is not an object")
        rows.append(value)
    if not rows:
        _fail(field, "contains no JSON events")
    return rows


def _unwrap_json(value: Any) -> Any:
    """Decode the nested text envelopes used by MCP host event streams."""

    if isinstance(value, str):
        stripped = value.strip()
        if stripped.startswith(("{", "[")):
            try:
                return _unwrap_json(json.loads(stripped))
            except json.JSONDecodeError:
                return value
        return value
    if isinstance(value, list):
        if len(value) == 1 and isinstance(value[0], dict):
            row = value[0]
            if row.get("type") == "text" and isinstance(row.get("text"), str):
                return _unwrap_json(row["text"])
        return [_unwrap_json(row) for row in value]
    if isinstance(value, dict):
        if set(value) >= {"content"} and len(value) <= 3:
            return _unwrap_json(value["content"])
        return {key: _unwrap_json(child) for key, child in value.items()}
    return value


@dataclass(frozen=True)
class ToolObservation:
    call_id: str
    name: str
    arguments: dict[str, Any]
    result: Any
    error: str


@dataclass(frozen=True)
class AgentTranscript:
    host: str
    events: tuple[dict[str, Any], ...]
    tools: tuple[ToolObservation, ...]
    final: dict[str, Any]
    usage: dict[str, int]


def _parse_claude_transcript(stdout: bytes) -> AgentTranscript:
    events = _json_lines(stdout, field="claude.stdout")
    calls: dict[str, tuple[str, dict[str, Any]]] = {}
    results: dict[str, tuple[Any, str]] = {}
    final: dict[str, Any] | None = None
    usage: dict[str, int] = {}
    for event in events:
        if event.get("type") == "assistant":
            message = event.get("message", {})
            for block in message.get("content", []) if isinstance(message, dict) else []:
                if isinstance(block, dict) and block.get("type") == "tool_use":
                    call_id = str(block.get("id", ""))
                    name = str(block.get("name", ""))
                    arguments = block.get("input", {})
                    if not call_id or not name or not isinstance(arguments, dict):
                        _fail("claude.transcript", "contains malformed tool_use")
                    calls[call_id] = (name, arguments)
        if event.get("type") == "user":
            message = event.get("message", {})
            for block in message.get("content", []) if isinstance(message, dict) else []:
                if isinstance(block, dict) and block.get("type") == "tool_result":
                    call_id = str(block.get("tool_use_id", ""))
                    error = "tool-error" if block.get("is_error") else ""
                    results[call_id] = (_unwrap_json(block.get("content")), error)
        if event.get("type") == "result":
            candidate = event.get("structured_output")
            if candidate is None and isinstance(event.get("result"), str):
                candidate = _unwrap_json(event["result"])
            if isinstance(candidate, dict):
                final = candidate
            raw_usage = event.get("usage", {})
            if isinstance(raw_usage, dict):
                usage = {
                    str(key): int(value)
                    for key, value in raw_usage.items()
                    if isinstance(value, int)
                }
    observations = []
    for call_id, (name, arguments) in calls.items():
        if name == "StructuredOutput":
            # The Claude CLI delivers --json-schema output through a
            # harness-side StructuredOutput pseudo-tool. It is the final
            # answer channel, not an agent capability, and the result event
            # carries the same object as structured_output.
            if final is None and isinstance(arguments, dict):
                final = arguments
            continue
        if call_id not in results:
            _fail("claude.transcript", f"tool call {call_id} has no result")
        result, error = results[call_id]
        observations.append(ToolObservation(call_id, name, arguments, result, error))
    if final is None:
        _fail("claude.transcript", "has no structured final response")
    return AgentTranscript("claude", tuple(events), tuple(observations), final, usage)


def _parse_codex_transcript(stdout: bytes, final_body: bytes) -> AgentTranscript:
    events = _json_lines(stdout, field="codex.stdout")
    final = _strict_json(final_body, "codex.final")
    if not isinstance(final, dict):
        _fail("codex.final", "must be an object")
    observations: list[ToolObservation] = []
    usage: dict[str, int] = {}
    for event in events:
        if event.get("type") == "item.completed" and isinstance(event.get("item"), dict):
            item = event["item"]
            if item.get("type") in {"mcp_tool_call", "mcp_call"}:
                call_id = str(item.get("id", ""))
                name = str(item.get("tool", item.get("name", "")))
                arguments = item.get("arguments", item.get("input", {}))
                if isinstance(arguments, str):
                    arguments = _unwrap_json(arguments)
                if not call_id or not name or not isinstance(arguments, dict):
                    _fail("codex.transcript", "contains malformed MCP tool call")
                raw_result = item.get("result", item.get("output", item.get("content")))
                error = str(item.get("error", "") or "")
                observations.append(
                    ToolObservation(
                        call_id, name, arguments, _unwrap_json(raw_result), error
                    )
                )
            if item.get("type") in {
                "command_execution",
                "file_change",
                "web_search",
            }:
                _fail("codex.transcript", f"forbidden tool event {item.get('type')}")
        if event.get("type") == "turn.completed" and isinstance(
            event.get("usage"), dict
        ):
            usage = {
                str(key): int(value)
                for key, value in event["usage"].items()
                if isinstance(value, int)
            }
    return AgentTranscript("codex", tuple(events), tuple(observations), final, usage)


def _tool_basename(name: str) -> str:
    value = name.strip()
    for separator in ("__", "/", "."):
        if separator in value:
            value = value.rsplit(separator, 1)[-1]
    return value.replace("-", "_")


def _walk(value: Any) -> Iterable[Any]:
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from _walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from _walk(child)


def _strings_for_keys(value: Any, keys: set[str]) -> list[str]:
    result: list[str] = []
    for row in _walk(value):
        if not isinstance(row, dict):
            continue
        for key in keys:
            child = row.get(key)
            if isinstance(child, str) and child:
                result.append(child)
    return result


def _dicts_under_key(value: Any, key: str) -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    for row in _walk(value):
        if not isinstance(row, dict) or not isinstance(row.get(key), list):
            continue
        result.extend(child for child in row[key] if isinstance(child, dict))
    return result


def _normalized_project_path(value: str) -> str:
    if not value or "\\" in value or value.startswith(("sha256:", "finding:")):
        return ""
    try:
        return _safe_relative(value.split("#", 1)[0], field="sourcePath").as_posix()
    except EvidenceError:
        return ""


def _result_texts(value: Any) -> list[bytes]:
    result: list[bytes] = []
    for row in _walk(value):
        if isinstance(row, dict):
            for key in ("text", "content", "claim", "body", "excerpt"):
                child = row.get(key)
                if isinstance(child, str) and child:
                    result.append(child.encode("utf-8"))
        elif isinstance(row, str) and row:
            result.append(row.encode("utf-8"))
    return result


def _observed_context(
    transcript: AgentTranscript,
    retrieval_tool: str = "query",
) -> dict[str, Any]:
    cards: list[dict[str, Any]] = []
    paths: set[str] = set()
    handles: set[str] = set()
    expansions: set[str] = set()
    content_hashes: set[str] = set()
    durability: set[str] = set()
    context_tokens = 0
    texts: list[bytes] = []
    query_count = 0
    read_count = 0
    for observation in transcript.tools:
        name = _tool_basename(observation.name)
        if name not in {retrieval_tool, "read"}:
            _fail("agent.tools", f"forbidden tool {observation.name!r}")
        if observation.error:
            # A failed call contributes nothing to observed context. Agents
            # legitimately recover from argument-schema rejections by reading
            # the advertised schema and retrying; only successful calls carry
            # evidence.
            continue
        if name == retrieval_tool:
            query_count += 1
            cards.extend(_dicts_under_key(observation.result, "cards"))
        else:
            read_count += 1
            for key in ("value", "path", "chunkId", "uri"):
                value = observation.arguments.get(key)
                if isinstance(value, str) and value:
                    expansions.add(value)
        paths.update(
            filter(
                None,
                (
                    _normalized_project_path(value)
                    for value in _strings_for_keys(
                        observation.result,
                        {"path", "sourcePath", "sourceId", "sourceID"},
                    )
                ),
            )
        )
        handles.update(
            _strings_for_keys(
                observation.result,
                {"handle", "sourceHandle", "findingHandle", "evidenceHandle", "uri"},
            )
        )
        content_hashes.update(
            normalized
            for normalized in (
                _normalized_digest(value)
                for value in _strings_for_keys(
                    observation.result,
                    {"contentHash", "contentSha256", "digest", "sourceHash"},
                )
            )
            if normalized is not None
        )
        for row in _walk(observation.result):
            if isinstance(row, dict):
                for key in ("sourceClass", "durability", "tier", "validity"):
                    value = row.get(key)
                    if isinstance(value, str) and value:
                        durability.add(value)
                for key in ("tokenCost", "estimatedTokens", "returnedTokens"):
                    value = row.get(key)
                    if isinstance(value, int) and value >= 0:
                        context_tokens += value
                        break
        texts.extend(_result_texts(observation.result))
    if query_count < 1:
        _fail("agent.tools", "a blinded trial must execute a successful retrieval call")
    card_handles = {
        value
        for card in cards
        for value in (
            card.get("handle"),
            card.get("findingHandle"),
            card.get("recordHandle"),
        )
        if isinstance(value, str) and value
    }
    return {
        "cards": cards,
        "paths": sorted(paths),
        "handles": sorted(handles),
        "cardHandles": sorted(card_handles),
        "expansions": sorted(expansions),
        "contentHashes": sorted(content_hashes),
        "durability": sorted(durability),
        "contextTokens": context_tokens,
        "texts": texts,
        "queryCount": query_count,
        "readCount": read_count,
    }


def _validate_agent_final(value: Mapping[str, Any]) -> tuple[str, list[dict[str, Any]]]:
    if set(value) != {"decision", "claims"}:
        _fail("agent.final", "fields must equal decision and claims")
    decision = value.get("decision")
    claims = value.get("claims")
    if decision not in {"answer", "abstain"} or not isinstance(claims, list):
        _fail("agent.final", "has an invalid decision or claims array")
    required = {
        "claim",
        "sourcePath",
        "startByte",
        "endByte",
        "contentHash",
        "sourceHandle",
        "evidenceHandles",
    }
    result: list[dict[str, Any]] = []
    for index, claim in enumerate(claims):
        if not isinstance(claim, dict) or set(claim) != required:
            _fail(f"agent.final.claims[{index}]", f"fields must equal {sorted(required)}")
        quote = claim.get("claim")
        path = claim.get("sourcePath")
        start = claim.get("startByte")
        end = claim.get("endByte")
        content_hash = claim.get("contentHash")
        source_handle = claim.get("sourceHandle")
        evidence_handles = claim.get("evidenceHandles")
        if (
            not isinstance(quote, str)
            or not quote.strip()
            or len(quote.encode("utf-8")) > MAX_QUOTE_BYTES
            or not _normalized_project_path(str(path))
            or not isinstance(start, int)
            or not isinstance(end, int)
            or start < 0
            or end <= start
            or _normalized_digest(content_hash) is None
            or not isinstance(source_handle, str)
            or not source_handle
            or not isinstance(evidence_handles, list)
            or not evidence_handles
            or any(not isinstance(item, str) or not item for item in evidence_handles)
        ):
            _fail(f"agent.final.claims[{index}]", "is malformed or unbounded")
        normalized = dict(claim)
        normalized["contentHash"] = _normalized_digest(content_hash)
        result.append(normalized)
    if decision == "abstain" and result:
        _fail("agent.final", "an abstention may not contain claims")
    if decision == "answer" and not result:
        _fail("agent.final", "an answer must contain at least one exact claim")
    return decision, result


def _source_bytes(workspace: Path, source_path: str) -> bytes | None:
    path = _contained(workspace, source_path, field="claim.sourcePath")
    if path.is_symlink() or not path.is_file():
        return None
    body = path.read_bytes()
    if len(body) > MAX_CAPTURE_BYTES:
        _fail("claim.sourcePath", "source exceeds the bounded evidence size")
    return body


def _claim_supported(
    claim: Mapping[str, Any],
    *,
    workspace: Path,
    observed: Mapping[str, Any],
) -> bool:
    quote = str(claim["claim"]).encode("utf-8")
    start = int(claim["startByte"])
    end = int(claim["endByte"])
    path = _normalized_project_path(str(claim["sourcePath"]))
    content_hash = str(claim["contentHash"])
    source_handle = str(claim["sourceHandle"])
    evidence_handles = set(str(value) for value in claim["evidenceHandles"])
    actual_handles = set(observed["handles"]) | set(observed["expansions"]) | set(
        observed["cardHandles"]
    )
    if source_handle not in actual_handles or not evidence_handles.issubset(actual_handles):
        return False
    if path not in observed["paths"]:
        return False
    body = _source_bytes(workspace, path)
    if body is not None and _sha256(body) == content_hash and end <= len(body) and body[start:end] == quote:
        return True
    # Tool-attested span: the hash must have appeared in a real captured tool
    # result and the exact byte span must appear in retained result content.
    # This grounds a claim in what the runtime actually served when the agent
    # cannot address the physical file - the 0.7 runtime reports line-granular
    # chunks without full-source byte offsets, and migrated finding cards may
    # retain an exact source span even when the physical destination changed.
    # Both arms are scored by the same rule.
    if content_hash not in observed["contentHashes"]:
        return False
    return any(end <= len(body) and body[start:end] == quote for body in observed["texts"])


def _eval_digest(case: Mapping[str, Any]) -> str:
    return _go_struct_digest(
        case,
        EVAL_CASE_FIELDS,
        omit_empty=("vocabularyPolicy", "gradedRelevantPaths", "evidencePins"),
    )


def _benchmark_outcome_digest(outcome: Mapping[str, Any]) -> str:
    return _go_struct_digest(outcome, CASE_OUTCOME_FIELDS)


def _derive_judgment(
    *,
    trial_id: str,
    eval_case: Mapping[str, Any],
    benchmark_outcome: Mapping[str, Any],
    transcript: AgentTranscript,
    workspace: Path,
    retrieval_tool: str = "query",
) -> tuple[dict[str, Any], dict[str, Any]]:
    decision, claims = _validate_agent_final(transcript.final)
    observed = _observed_context(transcript, retrieval_tool)
    answerable = eval_case.get("answerable")
    if not isinstance(answerable, bool):
        _fail("evalCase.answerable", "must be boolean")
    expected_paths = list(eval_case.get("expectedPaths", []))
    minimum_paths = list(eval_case.get("minimumEvidencePaths", []))
    hard_negatives = list(eval_case.get("hardNegativePaths", []))
    expected_citations = list(eval_case.get("expectedCitations", []))
    for field, values in (
        ("expectedPaths", expected_paths),
        ("minimumEvidencePaths", minimum_paths),
        ("hardNegativePaths", hard_negatives),
        ("expectedCitations", expected_citations),
    ):
        if not isinstance(values, list) or any(not isinstance(row, str) for row in values):
            _fail(f"evalCase.{field}", "must be a string array")
    claim_rows: list[dict[str, Any]] = []
    unsupported = 0
    for claim in claims:
        supported = _claim_supported(
            claim, workspace=workspace, observed=observed
        )
        if claim["sourcePath"] not in expected_paths or not supported:
            unsupported += 1
        claim_rows.append(copy.deepcopy(claim))
    cited_paths = sorted({str(row["sourcePath"]) for row in claims})
    opened_paths = set(observed["paths"])
    evidence_coverage = set(minimum_paths).issubset(opened_paths | set(cited_paths))
    citation_coverage = set(expected_citations).issubset(set(cited_paths))
    negative_safe = not set(hard_negatives).intersection(opened_paths | set(cited_paths))
    expected_decision = "answer" if answerable else "abstain"
    decision_correct = decision == expected_decision
    if answerable:
        factual_accuracy = (
            1.0
            if claims
            and unsupported == 0
            and evidence_coverage
            and citation_coverage
            and negative_safe
            else 0.0
        )
    else:
        factual_accuracy = 1.0 if decision_correct and not claims else 0.0
    decision_accuracy = 1.0 if decision_correct else 0.0
    evidence_trace = (
        unsupported == 0
        and negative_safe
        and (
            (answerable and evidence_coverage and citation_coverage and bool(claims))
            or (not answerable and not claims and not cited_paths)
        )
    )
    opened_evidence = []
    for path in sorted(opened_paths):
        matching = [
            row for row in claims if str(row.get("sourcePath", "")) == path
        ]
        opened_evidence.append(
            {
                "sourcePath": path,
                "sourceHandle": (
                    str(matching[0]["sourceHandle"]) if matching else "unclaimed"
                ),
                "contentHash": (
                    str(matching[0]["contentHash"])
                    if matching
                    else "sha256:" + "0" * 64
                ),
            }
        )
    judgment: dict[str, Any] = {
        "schemaVersion": 1,
        "trialId": trial_id,
        "evalCaseDigest": _eval_digest(eval_case),
        "benchmarkOutcomeDigest": _benchmark_outcome_digest(benchmark_outcome),
        "answerable": answerable,
        "expectedPaths": expected_paths,
        "minimumEvidencePaths": minimum_paths,
        "hardNegativePaths": hard_negatives,
        "expectedCitations": expected_citations,
        "claims": claim_rows,
        "openedEvidence": opened_evidence,
        "decision": {
            "observed": decision,
            "expected": expected_decision,
            "correct": decision_correct,
        },
        "resultDigest": "",
    }
    judgment["resultDigest"] = _canonical_digest(judgment)
    response = {
        "answerClaims": [str(row["claim"]) for row in claims],
        "decision": decision,
        "openedSourceIds": sorted(opened_paths),
        "contextCardHandles": list(observed["cardHandles"]),
        "expansionHandles": list(observed["expansions"]),
        "citations": cited_paths,
        "durabilityLabels": list(observed["durability"]),
        "fullCorpusRead": False,
        "contextTokens": int(observed["contextTokens"]),
        "error": "",
    }
    scores = {
        "response": response,
        "factualAccuracy": factual_accuracy,
        "decisionAccuracy": decision_accuracy,
        "unsupportedClaims": unsupported,
        "evidenceTracePassed": evidence_trace,
        "passed": (
            factual_accuracy >= 0.5
            and decision_accuracy >= 0.5
            and unsupported == 0
            and evidence_trace
        ),
    }
    return judgment, scores


def _copy_regular_tree(source: Path, destination: Path, *, excluded: set[str]) -> None:
    if destination.exists() or destination.is_symlink():
        _fail("workspace", f"destination already exists: {destination}")
    destination.mkdir(parents=True)
    for path in sorted(source.rglob("*"), key=lambda item: item.as_posix()):
        relative = path.relative_to(source).as_posix()
        if any(relative == row or relative.startswith(row + "/") for row in excluded):
            continue
        info = path.lstat()
        if path.is_symlink() or bool(
            getattr(info, "st_file_attributes", 0)
            & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0x400)
        ):
            _fail("workspace", f"link or reparse point is forbidden: {relative}")
        target = destination.joinpath(*PurePosixPath(relative).parts)
        if path.is_dir():
            target.mkdir(parents=True, exist_ok=True)
        elif path.is_file():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
            shutil.copymode(path, target)
        else:
            _fail("workspace", f"non-regular entry is forbidden: {relative}")


def _copy_managed_workspace(
    source: Path,
    destination: Path,
    *,
    required_paths: Sequence[str],
) -> None:
    destination.mkdir(parents=True, exist_ok=False)
    managed = (
        "AGENTS.md",
        ".codex",
        ".claude",
        ".re-discipline",
        "docs",
        "active",
    )
    exclusions = {
        ".git",
        ".re-discipline/cache",
        # Migration transaction state is recovery bookkeeping, not knowledge
        # corpus. Its backups mirror the complete pre-migration tree —
        # including the withheld evaluation corpus — so copying it into an
        # agent workspace would both leak evaluation bytes and exceed
        # platform path limits.
        ".re-discipline/migration",
        EVAL_ROOT,
        ".re-discipline/local-paths.md",
    }
    copied: set[str] = set()
    for relative in managed:
        path = source.joinpath(*PurePosixPath(relative).parts)
        if not path.exists():
            continue
        target = destination.joinpath(*PurePosixPath(relative).parts)
        if path.is_file():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
            copied.add(relative)
            continue
        _copy_regular_tree(
            path,
            target,
            excluded={
                row[len(relative) + 1 :]
                for row in exclusions
                if row.startswith(relative + "/")
            },
        )
    for relative in required_paths:
        rel = _safe_relative(relative, field="eval.requiredPath").as_posix()
        source_path = source.joinpath(*PurePosixPath(rel).parts)
        target = destination.joinpath(*PurePosixPath(rel).parts)
        if target.exists():
            continue
        if source_path.is_symlink() or not source_path.is_file():
            _fail("eval.requiredPath", f"source path is absent or unsafe: {rel}")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source_path, target)
        copied.add(rel)
    for path in destination.rglob("*"):
        relative = path.relative_to(destination).as_posix()
        if relative == EVAL_ROOT or relative.startswith(EVAL_ROOT + "/"):
            _fail("evalIsolation", "workspace retained an evaluation file")


def _load_eval_cases(project: Path) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    root = _contained(project, EVAL_ROOT, field="evalRoot")
    if root.is_symlink() or not root.is_dir():
        _fail("evalRoot", "must be a real directory")
    cases: list[dict[str, Any]] = []
    inventory: list[dict[str, Any]] = []
    for path in sorted(root.glob("*.json")):
        body = _regular_bytes(path, field="evalCaseFile")
        inventory.append(
            {
                "path": path.relative_to(project).as_posix(),
                "digest": _sha256(body),
                "sizeBytes": len(body),
            }
        )
        value = _strict_json(body, str(path))
        if not isinstance(value, list):
            _fail("evalCaseFile", f"{path.name} is not an array")
        for row in value:
            if not isinstance(row, dict):
                _fail("evalCaseFile", f"{path.name} contains a non-object case")
            cases.append(row)
    if not inventory or not cases:
        _fail("evalRoot", "contains no evaluation cases")
    seen: set[str] = set()
    for case in cases:
        case_id = case.get("id")
        if not isinstance(case_id, str) or not case_id or case_id in seen:
            _fail("evalCase.id", "must be non-empty and unique")
        seen.add(case_id)
    holdout = sorted(
        (case for case in cases if case.get("split") == "holdout"),
        key=lambda row: str(row["id"]),
    )
    if not holdout:
        _fail("evalRoot", "contains no holdout cases")
    return holdout, inventory


def _load_benchmark(path: Path) -> tuple[dict[str, Any], dict[str, dict[str, Any]], bytes]:
    body = _regular_bytes(path, field="benchmark")
    report = _strict_json(body, "benchmark")
    if not isinstance(report, dict) or report.get("suite") != "project-benchmark-v1":
        _fail("benchmark", "is not a project-benchmark-v1 report")
    profiles = report.get("profiles")
    if not isinstance(profiles, list):
        _fail("benchmark.profiles", "must be an array")
    primary = next(
        (
            row
            for row in profiles
            if isinstance(row, dict) and row.get("profileName") == PRIMARY_PROFILE
        ),
        None,
    )
    if primary is None or not isinstance(primary.get("cases"), list):
        _fail("benchmark", f"omits primary profile {PRIMARY_PROFILE}")
    outcomes: dict[str, dict[str, Any]] = {}
    for row in primary["cases"]:
        if not isinstance(row, dict) or not isinstance(row.get("caseId"), str):
            _fail("benchmark.cases", "contains a malformed case")
        case_id = row["caseId"]
        if case_id in outcomes:
            _fail("benchmark.cases", f"duplicates {case_id}")
        if row.get("split") == "holdout":
            outcomes[case_id] = row
    if not outcomes:
        _fail("benchmark", "primary profile has no holdout outcomes")
    return report, outcomes, body


def _migration_identity(project: Path) -> tuple[str, str, dict[str, Any]]:
    path = _contained(project, MIGRATION_STATE, field="migrationState")
    value = _strict_json(_regular_bytes(path, field="migrationState"), "migrationState")
    if not isinstance(value, dict):
        _fail("migrationState", "must be an object")
    transaction = value.get("transactionId")
    plan = value.get("planDigest")
    if (
        not isinstance(transaction, str)
        or not re.fullmatch(r"M-[A-F0-9]{20}", transaction)
        or not isinstance(plan, str)
        or not DIGEST_RE.fullmatch(plan)
    ):
        _fail("migrationState", "has invalid transaction or plan identity")
    state = value.get("state")
    if state not in {
        "physically-reorganized",
        "coverage-complete",
        "traversal-verified",
        "migrated",
    }:
        _fail("migrationState", f"clone is not activated: {state!r}")
    return transaction, plan, value


def _workspace_required_paths(cases: Sequence[Mapping[str, Any]]) -> list[str]:
    result: set[str] = set()
    for case in cases:
        for field in (
            "expectedPaths",
            "minimumEvidencePaths",
            "hardNegativePaths",
            "expectedCitations",
        ):
            values = case.get(field, [])
            if isinstance(values, list):
                result.update(str(value) for value in values if isinstance(value, str))
        pins = case.get("evidencePins", [])
        if isinstance(pins, list):
            result.update(
                str(row["path"])
                for row in pins
                if isinstance(row, dict) and isinstance(row.get("path"), str)
            )
    return sorted(result)


def _clone_legacy_revision(
    source: Path,
    revision: str,
    destination: Path,
    *,
    timeout_seconds: int,
) -> dict[str, Any]:
    """Clone ``source`` detached at ``revision`` and bind its committed tree.

    ``_clone_exact`` proves that a clone reproduces the source working tree,
    which is only meaningful when the requested revision is the source HEAD.
    The legacy runtime is an older commit whose tracked set legitimately
    differs from the current one, so this clone is verified against that
    revision's own committed tree, and the legacy runtime binary is
    separately digest-verified against its packaged manifest before use.
    """

    if destination.exists() or destination.is_symlink():
        _fail("legacyClone", f"refuses existing path {destination}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
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
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=destination.parent,
        timeout=timeout_seconds,
        check=False,
    )
    if result.returncode != 0:
        _fail(
            "legacyClone",
            result.stderr.decode("utf-8", errors="replace").strip()
            or f"git clone exited {result.returncode}",
        )
    _git(destination, "checkout", "--detach", revision)
    head = _git(destination, "rev-parse", "HEAD").decode("ascii").strip()
    if head != revision:
        _fail("legacyClone.revision", f"detached HEAD is {head}, expected {revision}")
    status = _git(destination, "status", "--porcelain=v1", "-z", "--untracked-files=all")
    if status:
        _fail("legacyClone", "checkout does not reproduce the committed legacy tree")
    rows = _tracked_rows(destination)
    tree = _git(destination, "rev-parse", "HEAD^{tree}").decode("ascii").strip()
    return {
        "revision": revision,
        "tree": tree,
        "trackedFileCount": len(rows),
        "trackedManifestSha256": _pair_digest(rows),
        "clean": True,
    }


def _find_legacy_plugin_revision(plugin: Path, current_revision: str) -> str:
    commits = _git(plugin, "rev-list", "--first-parent", current_revision).decode(
        "ascii", errors="strict"
    ).splitlines()
    manifest = f"{CURRENT_PLUGIN}/.claude-plugin/plugin.json"
    for commit in commits:
        result = subprocess.run(
            ["git", "--no-optional-locks", "-C", str(plugin), "show", f"{commit}:{manifest}"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if result.returncode != 0:
            continue
        try:
            value = _strict_json(result.stdout, "legacyPluginManifest")
        except EvidenceError:
            continue
        if isinstance(value, dict) and str(value.get("version", "")).startswith("0.7."):
            return commit
    _fail("legacyPluginRevision", "no 0.7 release is reachable from the plugin revision")


def _runtime_path(plugin: Path) -> Path:
    manifest_path = _contained(
        plugin, f"{CURRENT_ASSETS}/bin/manifest.json", field="runtimeManifest"
    )
    manifest = _strict_json(
        _regular_bytes(manifest_path, field="runtimeManifest"), "runtimeManifest"
    )
    if not isinstance(manifest, dict):
        _fail("runtimeManifest", "must be an object")
    suffix = ".exe" if os.name == "nt" else ""
    launcher_name = "re-discipline-knowledge" + suffix
    candidates = []
    for section in ("launchers", "targets"):
        rows = manifest.get(section, [])
        if not isinstance(rows, list):
            continue
        for row in rows:
            if not isinstance(row, dict) or not isinstance(row.get("path"), str):
                continue
            relative = row["path"]
            if PurePosixPath(relative).name != launcher_name:
                continue
            if section == "targets":
                machine = os.environ.get("PROCESSOR_ARCHITECTURE", "").lower()
                wanted = "arm64" if "arm64" in machine else "amd64"
                if wanted not in relative:
                    continue
            candidates.append((section, row))
    if not candidates:
        _fail("runtimeManifest", "omits the current platform runtime")
    section, row = sorted(candidates, key=lambda item: item[0] != "launchers")[0]
    path = _contained(
        plugin,
        f"{CURRENT_ASSETS}/bin/{row['path']}",
        field="runtime",
    )
    body = path.read_bytes() if path.is_file() and not path.is_symlink() else b""
    if not body or row.get("sha256") != _sha256(body) or row.get("size") != len(body):
        _fail("runtime", "does not match the package manifest")
    return path


def _agent_output_schema() -> dict[str, Any]:
    claim = {
        "type": "object",
        "additionalProperties": False,
        "required": [
            "claim",
            "sourcePath",
            "startByte",
            "endByte",
            "contentHash",
            "sourceHandle",
            "evidenceHandles",
        ],
        "properties": {
            "claim": {"type": "string", "minLength": 1, "maxLength": 2048},
            "sourcePath": {"type": "string", "minLength": 1},
            "startByte": {"type": "integer", "minimum": 0},
            "endByte": {"type": "integer", "minimum": 1},
            "contentHash": {
                "type": "string",
                "pattern": "^sha256:[0-9a-f]{64}$",
            },
            "sourceHandle": {"type": "string", "minLength": 1},
            "evidenceHandles": {
                "type": "array",
                "minItems": 1,
                "uniqueItems": True,
                "items": {"type": "string", "minLength": 1},
            },
        },
    }
    return {
        "type": "object",
        "additionalProperties": False,
        "required": ["decision", "claims"],
        "properties": {
            "decision": {"enum": ["answer", "abstain"]},
            "claims": {"type": "array", "items": claim},
        },
    }


def _blinded_request(case: Mapping[str, Any]) -> dict[str, Any]:
    required = ("id", "query", "queryClass", "role", "allowedTiers", "tokenBudget")
    if any(field not in case for field in required):
        _fail("evalCase", "omits a blinded request field")
    return {
        "caseId": case["id"],
        "query": case["query"],
        "queryClass": case["queryClass"],
        "role": case["role"],
        "allowedTiers": case["allowedTiers"],
        "tokenBudget": case["tokenBudget"],
    }


def _agent_prompt(request: Mapping[str, Any], condition: str) -> str:
    request_json = json.dumps(request, ensure_ascii=False, separators=(",", ":"))
    return (
        "This is a blinded, read-only knowledge-navigation trial. "
        f"The context condition is {condition}. The frozen request is {request_json}. "
        "Use only the re-discipline knowledge retrieval and read tools. Do not use shell, "
        "filesystem, web, memory, subagents, or write tools. Do not look for evaluation "
        "files or infer hidden expected paths. Retrieve with the exact question, "
        "honoring the role and tier constraints through whatever parameters the "
        "retrieval tool's schema actually declares - never pass a parameter the "
        "schema omits - then expand only exact returned handles or paths needed "
        "to answer. The request's tokenBudget bounds the TOTAL result tokens "
        "across all of your tool calls together, and exceeding it invalidates "
        "the trial: always pass an explicit small tokenBudget on every "
        "retrieval and read call (at most one third of the total), use a small "
        "result limit where the schema offers one, make at most three "
        "retrieval calls, and stop retrieving the moment you can answer. "
        "For every non-abstention claim, copy one exact contiguous UTF-8 span verbatim "
        "from an actual tool result and return: the canonical project path of its "
        "source; the content hash exactly as the tool reported it for that source or "
        "span; half-open byte offsets locating the quote - copy the tool-reported "
        "startByte/endByte when you quote that entire returned span, otherwise give "
        "offsets of the quote within the exact returned content (0 and the quote's "
        "byte length when you quote a full returned text); the returned source or "
        "finding handle; and every evidence handle supporting it. Prefer quoting an "
        "entire small returned span over computing offsets yourself, and keep each "
        "quote under 4000 bytes. Do not paraphrase inside claims. If the allowed "
        "corpus does not support an answer, return decision=abstain and claims=[]. "
        "Declining is the correct outcome for a question the corpus cannot support, "
        "and inventing a claim to avoid abstaining is wrong - but so is giving up "
        "early: before abstaining, spend your whole retrieval allowance on genuinely "
        "different reformulations of the question (synonyms, the underlying "
        "artifact names, the outcome it describes), because abstaining on a "
        "question the corpus does support is scored as a miss."
    )


def _hash_file(path: Path) -> tuple[str, int]:
    if path.is_symlink() or not path.is_file():
        _fail("executable", "must be a regular non-link file")
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as handle:
        while True:
            block = handle.read(1024 * 1024)
            if not block:
                break
            digest.update(block)
            size += len(block)
    if size < 1:
        _fail("executable", "may not be empty")
    return "sha256:" + digest.hexdigest(), size


def _resolve_executable(value: str | Path, *, expected: str) -> Path:
    raw = str(value)
    discovered = shutil.which(raw)
    path = Path(discovered if discovered else raw).resolve(strict=True)
    name = path.name.lower()
    if expected == "claude" and not name.startswith("claude"):
        _fail("claudeExecutable", f"unexpected launcher name {path.name!r}")
    if expected == "codex" and not name.startswith("codex"):
        _fail("codexExecutable", f"unexpected launcher name {path.name!r}")
    if expected == "codex" and path.suffix.lower() in {".ps1", ".cmd", ".bat", ".js", ""}:
        search_roots = [path.parent / "node_modules" / "@openai" / "codex"]
        native_name = "codex.exe" if os.name == "nt" else "codex"
        native = []
        for root in search_roots:
            if root.is_dir():
                native.extend(
                    candidate
                    for candidate in root.rglob(native_name)
                    if candidate.is_file()
                    and not candidate.is_symlink()
                    and "vendor" in candidate.parts
                )
        if native:
            path = sorted(native, key=lambda candidate: len(candidate.parts))[0].resolve(
                strict=True
            )
    if path.suffix.lower() in {".ps1", ".cmd", ".bat", ".js"}:
        _fail(
            f"{expected}Executable",
            "formal mode requires the resolved native executable, not a shell script",
        )
    _hash_file(path)
    return path


def _capture_ref_set(
    store: ArtifactStore,
    *,
    prefix: str,
    capture: ProcessCapture,
) -> tuple[ArtifactRef, ArtifactRef, ArtifactRef]:
    argv_ref = store.write_json(f"{prefix}/argv.json", list(capture.argv))
    stdout_ref = store.write_bytes(f"{prefix}/stdout.jsonl", capture.stdout)
    stderr_ref = store.write_bytes(f"{prefix}/stderr.txt", capture.stderr)
    return argv_ref, stdout_ref, stderr_ref


def _executable_identity(
    *,
    key: str,
    path: Path,
    version_argv: Sequence[str],
    executor: Executor,
    cwd: Path,
    store: ArtifactStore,
    timeout_seconds: int,
) -> dict[str, Any]:
    digest, byte_count = _hash_file(path)
    capture = executor.run(
        version_argv, cwd=cwd, timeout_seconds=min(timeout_seconds, 60)
    )
    if capture.exit_code != 0:
        _fail(f"executable.{key}", "version command failed")
    text = (capture.stdout + b"\n" + capture.stderr).decode(
        "utf-8", errors="strict"
    ).strip()
    patterns = {
        "claude": r"\d+\.\d+\.\d+.*Claude Code",
        "codex": r"codex-cli\s+\d+\.\d+\.\d+",
        "current-runtime": r"re-discipline-knowledge.*0\.8",
        "legacy-runtime": r"re-discipline-knowledge.*0\.7",
    }
    pattern = patterns.get(key)
    if pattern and not re.search(pattern, text, flags=re.IGNORECASE | re.DOTALL):
        _fail(f"executable.{key}", f"unexpected version output {text!r}")
    stdout_ref = store.write_bytes(f"executables/{key}/version-stdout.txt", capture.stdout)
    stderr_ref = store.write_bytes(f"executables/{key}/version-stderr.txt", capture.stderr)
    return {
        "resolvedPath": str(path),
        "sha256": digest,
        "byteCount": byte_count,
        "version": text,
        "versionStdout": stdout_ref.json(),
        "versionStderr": stderr_ref.json(),
    }


def _check_authentication(
    *,
    claude: Path,
    codex: Path | None,
    executor: Executor,
    cwd: Path,
    store: ArtifactStore,
    timeout_seconds: int,
) -> dict[str, Any]:
    captures = {
        "claude": executor.run(
            [str(claude), "auth", "status", "--json"],
            cwd=cwd,
            timeout_seconds=min(timeout_seconds, 60),
        ),
    }
    if codex is not None:
        captures["codex"] = executor.run(
            [str(codex), "login", "status"],
            cwd=cwd,
            timeout_seconds=min(timeout_seconds, 60),
        )
    refs: dict[str, Any] = {}
    for host, capture in captures.items():
        if capture.exit_code != 0:
            _fail(f"authentication.{host}", "required host authentication is unavailable")
        combined = (capture.stdout + capture.stderr).decode(
            "utf-8", errors="strict"
        ).lower()
        if host == "claude":
            value = _strict_json(capture.stdout, "claude.auth")
            if not isinstance(value, dict) or not any(
                value.get(key) is True
                for key in ("loggedIn", "authenticated", "isAuthenticated")
            ):
                _fail("authentication.claude", "auth status is not logged in")
            retained_stdout = _stable_bytes(
                {
                    "loggedIn": True,
                    "authMethod": value.get("authMethod", "authenticated"),
                    "apiProvider": value.get("apiProvider", "authenticated"),
                }
            )
        elif "logged in" not in combined:
            _fail("authentication.codex", "login status is not logged in")
        else:
            retained_stdout = b"Logged in\n"
        redacted = ProcessCapture(
            capture.argv,
            capture.cwd,
            retained_stdout,
            capture.stderr,
            capture.exit_code,
            capture.started_at,
            capture.finished_at,
        )
        argv_ref, stdout_ref, stderr_ref = _capture_ref_set(
            store, prefix=f"authentication/{host}", capture=redacted
        )
        refs[host] = {
            "argv": argv_ref.json(),
            "stdout": stdout_ref.json(),
            "stderr": stderr_ref.json(),
            "exitCode": capture.exit_code,
        }
    return refs


def _mcp_config(runtime: Path, assets: Path, cache: Path, workspace: Path) -> dict[str, Any]:
    # `serve` accepts --asset-root and --project-root only.  The runtime picks
    # its cache root from the bound project, falling back to the machine cache
    # base named by LOCALAPPDATA/XDG_CACHE_HOME; pinning both keeps a disposable
    # run from touching the operator's real machine cache.
    return {
        "mcpServers": {
            MCP_SERVER_NAME: {
                "command": str(runtime),
                "args": ["serve", "--asset-root", str(assets)],
                "cwd": str(workspace),
                "env": {"LOCALAPPDATA": str(cache), "XDG_CACHE_HOME": str(cache)},
            }
        }
    }


def _write_private_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(_stable_bytes(value))


# Every knowledge tool either runtime serves other than the trial's
# retrieval and read surface. Explicitly disallowed so a respondent cannot
# execute them even where the host's permission mode would allow it.
_NON_TRIAL_KNOWLEDGE_TOOLS = (
    "status",
    "orient",
    "search",
    "query",
    "context_pack",
    "context_pack_materialize",
    "recall_propose",
    "state",
    "trace",
    "manager_apply",
    "curation_submit",
    "closure_apply",
    "normalization_queue",
    "migrate_project",
)


def _run_claude_trial(
    *,
    executable: Path,
    model: str,
    request: Mapping[str, Any],
    condition: str,
    workspace: Path,
    runtime: Path,
    assets: Path,
    cache: Path,
    scratch: Path,
    executor: Executor,
    timeout_seconds: int,
    retrieval_tool: str = "query",
) -> tuple[ProcessCapture, AgentTranscript, dict[str, Any]]:
    config = scratch / "mcp.json"
    _write_private_json(config, _mcp_config(runtime, assets, cache, workspace))
    schema = _agent_output_schema()
    prompt = _agent_prompt(request, condition)
    argv = [
        str(executable),
        "--print",
        prompt,
        "--output-format",
        "stream-json",
        "--verbose",
        "--json-schema",
        json.dumps(schema, ensure_ascii=False, separators=(",", ":")),
        "--model",
        model,
        "--no-session-persistence",
        "--strict-mcp-config",
        "--mcp-config",
        str(config),
        "--permission-mode",
        "dontAsk",
        "--tools",
        "",
        "--allowedTools",
        f"mcp__re-discipline-knowledge__{retrieval_tool},mcp__re-discipline-knowledge__read",
        "--disallowedTools",
        ",".join(
            [
                "Bash",
                "Write",
                "Edit",
                "Read",
                "Glob",
                "Grep",
                "WebFetch",
                "WebSearch",
                "Task",
            ]
            + [
                f"mcp__re-discipline-knowledge__{name}"
                for name in _NON_TRIAL_KNOWLEDGE_TOOLS
                if name != retrieval_tool
            ]
        ),
    ]
    environment = dict(os.environ)
    environment["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
    capture = executor.run(
        argv,
        cwd=workspace,
        env=environment,
        timeout_seconds=timeout_seconds,
    )
    if capture.exit_code != 0:
        _fail("claude", f"agent process exited {capture.exit_code}")
    transcript = _parse_claude_transcript(capture.stdout)
    invocation = {
        "condition": condition,
        "trialRequest": dict(request),
        "prompt": prompt,
        "outputSchema": schema,
        "mcpConfigDigest": _canonical_digest(_mcp_config(runtime, assets, cache, workspace)),
    }
    return capture, transcript, invocation


def _toml_literal(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return "[" + ",".join(_toml_literal(row) for row in value) + "]"
    _fail("codex.config", f"unsupported TOML value {value!r}")


def _codex_mcp_overrides(
    runtime: Path, assets: Path, cache: Path, workspace: Path
) -> list[str]:
    prefix = f'mcp_servers."{MCP_SERVER_NAME}"'
    values = {
        f"{prefix}.command": str(runtime),
        f"{prefix}.args": ["serve", "--asset-root", str(assets)],
        f"{prefix}.cwd": str(workspace),
        f"{prefix}.env.LOCALAPPDATA": str(cache),
        f"{prefix}.env.XDG_CACHE_HOME": str(cache),
        f"{prefix}.required": True,
        f"{prefix}.enabled_tools": ["query", "read"],
        "features.shell_tool": False,
        "tools.web_search": False,
        "agents.enabled": False,
    }
    result: list[str] = []
    for key, value in values.items():
        result.extend(["-c", f"{key}={_toml_literal(value)}"])
    return result


def _run_codex_trial(
    *,
    executable: Path,
    model: str,
    request: Mapping[str, Any],
    condition: str,
    workspace: Path,
    runtime: Path,
    assets: Path,
    cache: Path,
    scratch: Path,
    executor: Executor,
    timeout_seconds: int,
    retrieval_tool: str = "query",
) -> tuple[ProcessCapture, AgentTranscript, dict[str, Any]]:
    schema_path = scratch / "agent-output.schema.json"
    final_path = scratch / "final.json"
    schema = _agent_output_schema()
    _write_private_json(schema_path, schema)
    prompt = _agent_prompt(request, condition)
    argv = [
        str(executable),
        "exec",
        prompt,
        "--json",
        "--output-schema",
        str(schema_path),
        "--output-last-message",
        str(final_path),
        "--ephemeral",
        "--ignore-user-config",
        "--ignore-rules",
        "--sandbox",
        "read-only",
        # codex-cli 0.145 `exec` rejects `--ask-for-approval` (exec never
        # prompts), and refuses an untrusted non-repository workspace unless
        # the check is explicitly skipped. The disposable arm workspaces are
        # plain directory copies, never Git repositories.
        "--skip-git-repo-check",
        "--cd",
        str(workspace),
        "--model",
        model,
        *_codex_mcp_overrides(runtime, assets, cache, workspace),
    ]
    capture = executor.run(
        argv, cwd=workspace, timeout_seconds=timeout_seconds
    )
    if capture.exit_code != 0:
        _fail("codex", f"agent process exited {capture.exit_code}")
    final_body = _regular_bytes(final_path, field="codex.final")
    transcript = _parse_codex_transcript(capture.stdout, final_body)
    invocation = {
        "condition": condition,
        "trialRequest": dict(request),
        "prompt": prompt,
        "outputSchema": schema,
        "mcpConfig": {
            "runtime": str(runtime),
            "assets": str(assets),
            "cache": str(cache),
            "enabledTools": [retrieval_tool, "read"],
        },
    }
    return capture, transcript, invocation


# ---------------------------------------------------------------------------
# Sealed artifact assembly
# ---------------------------------------------------------------------------


def _case_answerable(case: Mapping[str, Any]) -> bool:
    """Read answerability from the canonical case exactly as the verifier does.

    The Go verifier treats an absent ``answerable`` as answerable and rejects a
    trial whose recorded value disagrees with the case, so this value is never
    taken from what the agent chose to do.
    """

    value = case.get("answerable")
    if value is None:
        return True
    if not isinstance(value, bool):
        _fail("evalCase.answerable", "must be boolean when present")
    return value


def _answer_shape_correct(response: Mapping[str, Any], answerable: bool) -> bool:
    """Mirror migrationBlindedAnswerShapeCorrect.

    A correct abstention produces neither claims nor citations; scoring it by
    the answerable rule would make a correct refusal indistinguishable from a
    failure.
    """

    claims = len(response["answerClaims"])
    citations = len(response["citations"])
    if answerable:
        return claims > 0 and citations > 0
    return claims == 0 and citations == 0


def _blinded_case_outcome(
    *,
    case_id: str,
    condition: str,
    answerable: bool,
    agent: str,
    model: str,
    request: Mapping[str, Any],
    response: Mapping[str, Any],
    scores: Mapping[str, Any],
    benchmark_outcome_digest: str,
) -> dict[str, Any]:
    """Rederive every identity the Go verifier recomputes for one trial."""

    if not agent.strip() or not model.strip():
        _fail("blindedCase.agent", "agent and model identity are required")
    if int(response["contextTokens"]) > int(request["tokenBudget"]):
        _fail(
            "blindedCase.contextTokens",
            f"{case_id}/{condition} consumed {response['contextTokens']} result "
            f"tokens over its {request['tokenBudget']} budget; the verifier "
            "rejects over-budget trials, so fail here with the exact overrun",
        )
    if not isinstance(answerable, bool):
        _fail("blindedCase.answerable", "must be the canonical case's boolean")
    agent_digest = _go_digest({"agent": agent, "model": model})
    query_digest = _sha256(str(request["query"]).encode("utf-8"))
    response_digest = _go_digest(response)
    factual = _go_number(scores["factualAccuracy"])
    decision = _go_number(scores["decisionAccuracy"])
    unsupported = int(scores["unsupportedClaims"])
    trace_passed = bool(scores["evidenceTracePassed"])
    passed = (
        unsupported == 0
        and trace_passed
        and factual >= 0.5
        and decision >= 0.5
        and _answer_shape_correct(response, answerable)
    )
    outcome: dict[str, Any] = {
        "id": _stable_id("MBE", case_id, condition, agent_digest, query_digest),
        "caseId": case_id,
        "condition": condition,
        "agent": agent,
        "model": model,
        "agentDigest": agent_digest,
        "queryDigest": query_digest,
        "benchmarkOutcomeDigest": benchmark_outcome_digest,
        "answerable": answerable,
        "request": dict(request),
        "response": dict(response),
        "responseDigest": response_digest,
        "factualAccuracy": factual,
        "decisionAccuracy": decision,
        "unsupportedClaims": unsupported,
        "evidenceTracePassed": trace_passed,
        "passed": passed,
        "digest": "",
    }
    outcome["digest"] = _go_digest(outcome)
    return outcome


def _blinded_case_non_inferior(
    legacy: Mapping[str, Any], compiled: Mapping[str, Any]
) -> bool:
    """Mirror the Go verifier's documented per-case bar for one measured pair.

    The compiled arm must be factually and decision non-inferior, contain no
    unsupported claims, preserve evidence traceability, use no more context
    tokens, avoid a full-corpus read, and label the durability of any answer
    it gives. This is a paired comparison, not an absolute per-trial pass
    requirement: both arms are stochastic external respondents measured by one
    deterministic judge, so demanding a perfect compiled trial on every case
    would make the gate unsatisfiable for any corpus - the same defect, one
    level up, as requiring a perfect 0.7 baseline. A compiled arm that
    regresses any judged dimension on any case still fails.
    """

    legacy_response = legacy["response"]
    compiled_response = compiled["response"]
    return (
        float(compiled["factualAccuracy"]) >= float(legacy["factualAccuracy"])
        and float(compiled["decisionAccuracy"]) >= float(legacy["decisionAccuracy"])
        and int(compiled_response["contextTokens"]) <= int(legacy_response["contextTokens"])
        and int(compiled["unsupportedClaims"]) == 0
        and not (
            bool(legacy["evidenceTracePassed"])
            and not bool(compiled["evidenceTracePassed"])
        )
        and not bool(compiled_response["fullCorpusRead"])
        and (
            compiled_response["decision"] != "answer"
            or len(compiled_response["durabilityLabels"]) > 0
        )
    )


def _seal_blinded_evaluation(
    *,
    transaction_id: str,
    plan_digest: str,
    benchmark_digest: str,
    calibration_digest: str,
    evaluator: str,
    cases: Sequence[Mapping[str, Any]],
) -> dict[str, Any]:
    if not cases:
        _fail("blindedEvaluation.cases", "requires at least one measured trial")
    if not evaluator.strip() or len(evaluator) > 128:
        _fail("blindedEvaluation.evaluator", "must be a bounded non-empty identity")
    for case in cases:
        if case["agent"] == evaluator:
            _fail(
                "blindedEvaluation.evaluator",
                "the evaluator identity may not equal a tested agent identity",
            )
    protocol_digest = _sha256(BLINDED_PROTOCOL.encode("utf-8"))
    # Mirror the verifier: the legacy arm is the captured baseline and the
    # compiled arm must be non-inferior against it case by case, per the
    # documented gate contract. A legacy miss is a true measurement of the
    # predecessor runtime and is preserved in its trial; a compiled miss on a
    # case the legacy arm also missed is a shared measurement of the
    # respondent. Only a case where the compiled arm scores worse than the
    # captured baseline fails the evaluation.
    compiled_cases = [case for case in cases if case["condition"] == "compiled"]
    if not compiled_cases:
        _fail("blindedEvaluation.cases", "requires at least one compiled trial")
    by_case: dict[str, dict[str, Mapping[str, Any]]] = {}
    for case in cases:
        by_case.setdefault(str(case["caseId"]), {})[str(case["condition"])] = case
    passed = bool(cases) and bool(compiled_cases)
    for arms in by_case.values():
        legacy = arms.get("legacy")
        compiled = arms.get("compiled")
        if (
            legacy is None
            or compiled is None
            or not _blinded_case_non_inferior(legacy, compiled)
        ):
            passed = False
    evaluation: dict[str, Any] = {
        "schemaVersion": 1,
        "suite": BLINDED_SUITE,
        "transactionId": transaction_id,
        "planDigest": plan_digest,
        "benchmarkDigest": benchmark_digest,
        "calibrationDigest": calibration_digest,
        "protocolDigest": protocol_digest,
        "evaluator": evaluator,
        "evaluatorDigest": _go_digest(
            {"evaluator": evaluator, "protocolDigest": protocol_digest}
        ),
        "cases": [dict(case) for case in cases],
        "passed": passed,
        "resultDigest": "",
    }
    evaluation["resultDigest"] = _go_digest(evaluation)
    return evaluation


def _host_trial(
    *,
    host: str,
    scenario: str,
    request: Mapping[str, Any],
    result: Any,
    failure: Mapping[str, Any] | None,
    exit_code: int,
    semantic: Any,
) -> dict[str, Any]:
    """Build one host trial with verifier-derived identity and result."""

    if host not in HOST_TRANSPORTS or scenario not in HOST_MATRIX.get(host, ()):
        _fail("hostTrial", f"unsupported host/scenario pair {host}/{scenario}")
    transport = (
        f"{host}-cli-fallback"
        if scenario == "local-fallback"
        else HOST_TRANSPORTS[host]
    )
    if request.get("scenario") != scenario:
        _fail("hostTrial.request", "captured request must name its exact scenario")
    if semantic is None:
        _fail("hostTrial.semanticResult", "a normalized semantic result is required")
    request_digest = _go_digest(request, sort_keys=True)
    semantic_digest = _go_digest(semantic, sort_keys=True)
    if scenario == "role-boundary":
        passed = bool(
            failure
            and failure.get("code") == "role-boundary-refused"
            and str(failure.get("message", "")).strip()
        )
    elif scenario == "local-fallback":
        passed = bool(
            failure
            and failure.get("code") == "mcp-unavailable"
            and str(failure.get("message", "")).strip()
            and exit_code == 0
        )
    else:
        passed = result is not None and failure is None and exit_code == 0
    trial: dict[str, Any] = {
        "id": _stable_id("MHT", host, scenario, request_digest),
        "host": host,
        "scenario": scenario,
        "transport": transport,
        "request": dict(request),
        "result": result,
        "failure": dict(failure) if failure else None,
        "exitCode": int(exit_code),
        "semanticResult": semantic,
        "semanticDigest": semantic_digest,
        "passed": passed,
        "digest": "",
    }
    digest_view = dict(trial)
    digest_view["request"] = _raw_capture(trial["request"])
    digest_view["result"] = _raw_capture(trial["result"])
    digest_view["semanticResult"] = _raw_capture(trial["semanticResult"])
    trial["digest"] = _go_digest(digest_view)
    return trial


def _seal_host_conformance(
    *,
    transaction_id: str,
    plan_digest: str,
    trials: Sequence[Mapping[str, Any]],
) -> dict[str, Any]:
    seen: set[str] = set()
    hosts_present: set[str] = set()
    for trial in trials:
        key = f"{trial['host']}:{trial['scenario']}"
        if key in seen:
            _fail("hostConformance", f"duplicates the {key} trial")
        seen.add(key)
        hosts_present.add(str(trial["host"]))
    # Mirror the verifier: mcp, cli, and claude are mandatory; codex is
    # covered only when the run captured it, and then completely.
    required_hosts = {"mcp", "cli", "claude"} | (
        {"codex"} if "codex" in hosts_present else set()
    )
    for host in sorted(required_hosts):
        for scenario in HOST_MATRIX[host]:
            if f"{host}:{scenario}" not in seen:
                _fail("hostConformance", f"omits {host}/{scenario}")
    evidence: dict[str, Any] = {
        "schemaVersion": 1,
        "suite": HOST_SUITE,
        "transactionId": transaction_id,
        "planDigest": plan_digest,
        "protocolDigest": _sha256(HOST_PROTOCOL.encode("utf-8")),
        "trials": [dict(trial) for trial in trials],
        "passed": all(bool(trial["passed"]) for trial in trials),
        "resultDigest": "",
    }
    digest_view = dict(evidence)
    digest_rows = []
    for trial in evidence["trials"]:
        row = dict(trial)
        row["request"] = _raw_capture(trial["request"])
        row["result"] = _raw_capture(trial["result"])
        row["semanticResult"] = _raw_capture(trial["semanticResult"])
        digest_rows.append(row)
    digest_view["trials"] = digest_rows
    evidence["resultDigest"] = _go_digest(digest_view)
    return evidence


def _assert_semantic_parity(
    trials: Sequence[Mapping[str, Any]],
    agent_hosts: Sequence[str],
) -> dict[str, str]:
    """Reproduce the verifier's cross-host parity requirements locally."""

    by_key = {f"{row['host']}:{row['scenario']}": row for row in trials}
    parity: dict[str, str] = {}
    for scenario in ("status", "retrieval", "expansion", "bounded-recovery"):
        want = by_key[f"mcp:{scenario}"]["semanticDigest"]
        for host in ("cli", *agent_hosts):
            observed = by_key[f"{host}:{scenario}"]["semanticDigest"]
            if observed != want:
                _fail(
                    "hostConformance.semanticParity",
                    f"{host}/{scenario} returned {observed}, mcp returned {want}",
                )
        parity[scenario] = want
    discovery = by_key["mcp:discovery"]["semanticDigest"]
    for host in agent_hosts:
        if by_key[f"{host}:discovery"]["semanticDigest"] != discovery:
            _fail(
                "hostConformance.discovery",
                f"{host} did not observe the current MCP tool schema",
            )
    parity["discovery"] = discovery
    status = by_key["cli:status"]["semanticDigest"]
    for host in agent_hosts:
        if by_key[f"{host}:local-fallback"]["semanticDigest"] != status:
            _fail(
                "hostConformance.localFallback",
                f"{host} local fallback did not preserve CLI status semantics",
            )
    parity["local-fallback"] = status
    return parity


# ---------------------------------------------------------------------------
# Real transport capture
# ---------------------------------------------------------------------------


PROXY_SOURCE = '''"""Transparent stdio proxy that records real host MCP traffic."""

import argparse
import json
import subprocess
import sys
import threading


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--log", required=True)
    parser.add_argument("--mode", choices=("proxy", "unavailable"), default="proxy")
    parser.add_argument("--message", default="")
    parser.add_argument("--runtime", default="")
    parser.add_argument("--asset-root", default="")
    parser.add_argument("--project-root", default="")
    arguments = parser.parse_args()
    log = open(arguments.log, "ab")
    lock = threading.Lock()

    def record(row):
        with lock:
            log.write(json.dumps(row, ensure_ascii=False).encode("utf-8") + b"\\n")
            log.flush()

    if arguments.mode == "unavailable":
        record({"direction": "proxy", "event": "unavailable",
                "message": arguments.message})
        log.close()
        sys.stderr.write(arguments.message + "\\n")
        sys.stderr.flush()
        return 3
    child = subprocess.Popen(
        [arguments.runtime, "serve", "--asset-root", arguments.asset_root,
         "--project-root", arguments.project_root],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
    )

    def pump(source, sink, direction):
        while True:
            line = source.readline()
            if not line:
                break
            record({"direction": direction,
                    "line": line.decode("utf-8", errors="replace").rstrip("\\r\\n")})
            sink.write(line)
            sink.flush()
        try:
            sink.close()
        except OSError:
            pass

    upstream = threading.Thread(
        target=pump, args=(sys.stdin.buffer, child.stdin, "client"))
    downstream = threading.Thread(
        target=pump, args=(child.stdout, sys.stdout.buffer, "server"))
    upstream.start()
    downstream.start()
    upstream.join()
    downstream.join()
    code = child.wait()
    log.close()
    return code


if __name__ == "__main__":
    raise SystemExit(main())
'''


def _write_proxy(scratch: Path) -> Path:
    path = scratch / "mcp-capture-proxy.py"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(PROXY_SOURCE.encode("utf-8"))
    return path


def _proxy_argv(
    proxy: Path,
    log: Path,
    *,
    runtime: Path | None = None,
    assets: Path | None = None,
    workspace: Path | None = None,
    unavailable: bool = False,
) -> list[str]:
    if unavailable:
        return [
            sys.executable,
            str(proxy),
            "--log",
            str(log),
            "--mode",
            "unavailable",
            "--message",
            MCP_UNAVAILABLE_MESSAGE,
        ]
    if runtime is None or assets is None or workspace is None:
        _fail("proxy", "a proxied MCP server requires runtime, assets, and workspace")
    return [
        sys.executable,
        str(proxy),
        "--log",
        str(log),
        "--runtime",
        str(runtime),
        "--asset-root",
        str(assets),
        "--project-root",
        str(workspace),
    ]


def _conformance_calls() -> tuple[tuple[str, str, dict[str, Any]], ...]:
    """The exact tool arguments every host must issue for semantic parity."""

    retrieval = {
        "query": CONFORMANCE_QUERY,
        "queryClass": "orientation",
        "limit": 5,
        "tokenBudget": CONFORMANCE_TOKEN_BUDGET,
    }
    recovery = dict(retrieval)
    recovery["contextLeaseId"] = CONFORMANCE_LEASE
    recovery["resetContextLease"] = True
    return (
        (
            "status",
            "state",
            {"mode": "orient", "tokenBudget": CONFORMANCE_TOKEN_BUDGET, "maxCards": 5},
        ),
        ("retrieval", "query", retrieval),
        (
            "expansion",
            "read",
            {
                "selector": "path",
                "value": CONFORMANCE_READ_PATH,
                "tokenBudget": CONFORMANCE_TOKEN_BUDGET,
            },
        ),
        ("bounded-recovery", "query", recovery),
        (
            "role-boundary",
            ROLE_BOUNDARY_SURFACE,
            {
                "action": "preview",
                "target": {
                    "kind": "recruiting-run",
                    "candidateSlug": "migration-host-conformance",
                    "recruitingRunId": "migration-host-conformance",
                },
                "task": (
                    "Capture the role boundary enforced for a consumer role that is "
                    "neither manager nor drafter."
                ),
                "role": ROLE_BOUNDARY_ROLE,
            },
        ),
    )


def _role_boundary_reason(text: str, *, field: str) -> str:
    if ROLE_BOUNDARY_MESSAGE not in text:
        _fail(field, f"no role-boundary refusal was observed in {text[:200]!r}")
    return ROLE_BOUNDARY_MESSAGE


def _normalized_semantic(scenario: str, payload: Any, *, field: str) -> Any:
    """Reduce a transport-specific result to its host-independent meaning."""

    if scenario == "discovery":
        if not isinstance(payload, list) or not payload:
            _fail(field, "discovery must return the real tool definition array")
        return payload
    if scenario == "role-boundary":
        return {
            "code": "role-boundary-refused",
            "reason": _role_boundary_reason(str(payload), field=field),
            "refused": True,
            "role": ROLE_BOUNDARY_ROLE,
            "surface": ROLE_BOUNDARY_SURFACE,
        }
    if not isinstance(payload, dict) or not payload:
        _fail(field, "a normalized semantic result object is required")
    return payload


def _host_request(
    *,
    host: str,
    scenario: str,
    surface: Mapping[str, Any],
    wire: bytes,
) -> dict[str, Any]:
    transport = (
        f"{host}-cli-fallback"
        if scenario == "local-fallback"
        else HOST_TRANSPORTS[host]
    )
    return {
        "scenario": scenario,
        "host": host,
        "transport": transport,
        "surface": dict(surface),
        "wire": wire.decode("utf-8", errors="strict"),
        "wireSha256": _sha256(wire),
        "wireBytes": len(wire),
    }


def _jsonrpc_lines(rows: Sequence[Mapping[str, Any]]) -> bytes:
    return b"".join(
        json.dumps(row, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        + b"\n"
        for row in rows
    )


def _mcp_handshake() -> list[dict[str, Any]]:
    return [
        {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": MCP_PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {
                    "name": "re-discipline-migration-external-evidence",
                    "version": "1",
                },
            },
        },
        {"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}},
    ]


def _mcp_exchange(
    *,
    runtime: Path,
    assets: Path,
    workspace: Path,
    request: Mapping[str, Any],
    executor: Executor,
    timeout_seconds: int,
) -> tuple[ProcessCapture, dict[str, Any], bytes]:
    """Drive one real MCP stdio session and return the matching response."""

    body = _jsonrpc_lines([*_mcp_handshake(), request])
    capture = executor.run(
        [str(runtime), "serve", "--asset-root", str(assets), "--project-root", str(workspace)],
        cwd=workspace,
        stdin=body,
        timeout_seconds=timeout_seconds,
    )
    if capture.exit_code != 0:
        _fail(
            "mcp",
            f"server exited {capture.exit_code}: "
            f"{capture.stderr.decode('utf-8', errors='replace')[:400]}",
        )
    responses = _json_lines(capture.stdout, field="mcp.stdout")
    wanted = request["id"]
    for row in responses:
        if row.get("id") == wanted:
            if "result" not in row or not isinstance(row["result"], dict):
                _fail("mcp", f"request {wanted} returned no JSON-RPC result")
            return capture, row, _jsonrpc_lines([request])
    _fail("mcp", f"server returned no response for request {wanted}")
    raise AssertionError("unreachable")


def _tool_payload(response: Mapping[str, Any], *, field: str) -> tuple[Any, bool]:
    """Return the structured tool payload and whether the tool reported error."""

    result = response.get("result")
    if not isinstance(result, dict):
        _fail(field, "tool response has no result object")
    is_error = bool(result.get("isError"))
    if is_error:
        return _unwrap_json(result.get("content")), True
    structured = result.get("structuredContent")
    if structured is None:
        structured = _unwrap_json(result.get("content"))
    return structured, False


def _cli_invoke(
    *,
    runtime: Path,
    assets: Path,
    workspace: Path,
    command: str,
    request: Mapping[str, Any],
    scratch: Path,
    executor: Executor,
    timeout_seconds: int,
    label: str,
) -> tuple[ProcessCapture, list[str], bytes]:
    input_path = scratch / f"{label}-request.json"
    _write_private_json(input_path, request)
    argv = [
        str(runtime),
        command,
        "--asset-root",
        str(assets),
        "--project-root",
        str(workspace),
        "--input",
        str(input_path),
    ]
    capture = executor.run(argv, cwd=workspace, timeout_seconds=timeout_seconds)
    wire = _canonical_bytes({"argv": argv, "input": request})
    return capture, argv, wire


# ---------------------------------------------------------------------------
# Agent host capture
# ---------------------------------------------------------------------------


def _proxy_records(log: Path, *, field: str) -> list[dict[str, Any]]:
    if log.is_symlink() or not log.is_file():
        _fail(field, "the host never launched the instrumented MCP server")
    rows = _json_lines(_regular_bytes(log, field=field), field=field)
    return rows


def _proxy_messages(rows: Sequence[Mapping[str, Any]], direction: str) -> list[Any]:
    result: list[Any] = []
    for row in rows:
        if row.get("direction") != direction or not isinstance(row.get("line"), str):
            continue
        result.append(_strict_json(row["line"].encode("utf-8"), "proxy.line"))
    return result


def _proxy_tools_list(rows: Sequence[Mapping[str, Any]], *, field: str) -> Any:
    for message in _proxy_messages(rows, "server"):
        if not isinstance(message, dict):
            continue
        result = message.get("result")
        if isinstance(result, dict) and isinstance(result.get("tools"), list):
            return result["tools"]
    _fail(field, "the host never performed a real tools/list discovery")
    raise AssertionError("unreachable")


def _proxy_tool_call(
    rows: Sequence[Mapping[str, Any]],
    *,
    tool: str,
    arguments: Mapping[str, Any],
    field: str,
) -> tuple[dict[str, Any], dict[str, Any]]:
    """Locate the host's real call for exactly these arguments and its reply."""

    wanted = _canonical_digest(arguments)
    call: dict[str, Any] | None = None
    observed: list[str] = []
    for message in _proxy_messages(rows, "client"):
        if not isinstance(message, dict) or message.get("method") != "tools/call":
            continue
        params = message.get("params")
        if not isinstance(params, dict) or params.get("name") != tool:
            continue
        sent = params.get("arguments")
        if not isinstance(sent, dict):
            continue
        observed.append(_canonical_digest(sent))
        if _canonical_digest(sent) == wanted:
            call = message
            break
    if call is None:
        _fail(
            field,
            f"the host issued no {tool} call with the exact conformance arguments "
            f"(observed argument digests {observed})",
        )
    for message in _proxy_messages(rows, "server"):
        if isinstance(message, dict) and message.get("id") == call.get("id"):
            if not isinstance(message.get("result"), dict):
                _fail(field, f"the {tool} call returned no JSON-RPC result")
            return call, message
    _fail(field, f"the host received no reply for its {tool} call")
    raise AssertionError("unreachable")


def _embedded_objects(text: str) -> Iterable[dict[str, Any]]:
    decoder = json.JSONDecoder()
    index = 0
    while True:
        index = text.find("{", index)
        if index < 0:
            return
        try:
            value, end = decoder.raw_decode(text, index)
        except json.JSONDecodeError:
            index += 1
            continue
        if isinstance(value, dict):
            yield value
        index = max(end, index + 1)


def _event_strings(events: Sequence[Mapping[str, Any]]) -> list[str]:
    result: list[str] = []
    for event in events:
        for row in _walk(event):
            if isinstance(row, str) and row:
                result.append(row)
    return result


def _agent_events(capture: ProcessCapture, *, field: str) -> list[dict[str, Any]]:
    return _json_lines(capture.stdout, field=field)


def _claude_conformance_argv(
    *,
    executable: Path,
    model: str,
    prompt: str,
    config: Path,
    allowed_tools: str,
    disallowed_tools: str,
) -> list[str]:
    return [
        str(executable),
        "--print",
        prompt,
        "--output-format",
        "stream-json",
        "--verbose",
        "--model",
        model,
        "--no-session-persistence",
        "--strict-mcp-config",
        "--mcp-config",
        str(config),
        "--permission-mode",
        "dontAsk",
        "--allowedTools",
        allowed_tools,
        "--disallowedTools",
        disallowed_tools,
    ]


def _codex_host_overrides(
    *,
    command: Sequence[str],
    workspace: Path,
    enabled_tools: Sequence[str],
    required: bool,
    shell_tool: bool,
) -> list[str]:
    prefix = f'mcp_servers."{MCP_SERVER_NAME}"'
    values: dict[str, Any] = {
        f"{prefix}.command": str(command[0]),
        f"{prefix}.args": [str(value) for value in command[1:]],
        f"{prefix}.cwd": str(workspace),
        f"{prefix}.required": required,
        "tools.web_search": False,
        "agents.enabled": False,
        "features.shell_tool": shell_tool,
    }
    if enabled_tools:
        values[f"{prefix}.enabled_tools"] = list(enabled_tools)
    result: list[str] = []
    for key, value in values.items():
        result.extend(["-c", f"{key}={_toml_literal(value)}"])
    return result


def _codex_conformance_argv(
    *,
    executable: Path,
    model: str,
    prompt: str,
    workspace: Path,
    overrides: Sequence[str],
    sandbox: str,
) -> list[str]:
    return [
        str(executable),
        "exec",
        prompt,
        "--json",
        "--ephemeral",
        "--ignore-user-config",
        "--ignore-rules",
        "--sandbox",
        sandbox,
        # See _run_codex_trial: codex-cli 0.145 `exec` rejects
        # `--ask-for-approval` and requires the explicit repository-check
        # skip for a non-repository workspace copy.
        "--skip-git-repo-check",
        "--cd",
        str(workspace),
        "--model",
        model,
        *overrides,
    ]


def _conformance_prompt(calls: Sequence[tuple[str, str, Mapping[str, Any]]]) -> str:
    lines = [
        "This is a read-only host-conformance capture for a re-discipline knowledge "
        "migration. Call the listed re-discipline-knowledge MCP tools once each, in "
        "order, passing exactly the JSON arguments shown. Do not add, remove, rename, "
        "or reorder any argument. Do not call any other tool and do not run shell "
        "commands. One listed call is expected to be refused by the runtime; record "
        "that refusal and continue. When every call has been attempted, reply with "
        "the single word done."
    ]
    for index, (scenario, tool, arguments) in enumerate(calls, start=1):
        payload = json.dumps(arguments, ensure_ascii=False, separators=(",", ":"))
        lines.append(f"{index}. scenario {scenario}: call tool {tool} with {payload}")
    return "\n".join(lines)


def _fallback_prompt(command: Sequence[str]) -> str:
    rendered = " ".join(f'"{value}"' for value in command)
    return (
        "The re-discipline-knowledge MCP server is unavailable in this session. Do not "
        "retry it. Recover locally by running exactly this read-only command once and "
        "reporting its complete stdout verbatim, with no commentary before or after "
        f"the JSON: {rendered}"
    )


def _unavailable_failure(
    events: Sequence[Mapping[str, Any]], capture: ProcessCapture, *, field: str
) -> dict[str, str]:
    """Bind the mcp-unavailable failure to something the host actually said."""

    for event in events:
        for row in _walk(event):
            if not isinstance(row, dict):
                continue
            name = row.get("name") or row.get("server") or row.get("id")
            if not isinstance(name, str) or MCP_SERVER_NAME not in name:
                continue
            for key in ("status", "state", "error", "message", "reason"):
                value = row.get(key)
                if isinstance(value, str) and any(
                    token in value.lower()
                    for token in ("fail", "error", "unavailable", "disconnect")
                ):
                    return {
                        "code": "mcp-unavailable",
                        "message": json.dumps(row, ensure_ascii=False)[:2000],
                    }
    combined = (capture.stdout + b"\n" + capture.stderr).decode(
        "utf-8", errors="replace"
    )
    if MCP_UNAVAILABLE_MESSAGE in combined:
        return {"code": "mcp-unavailable", "message": MCP_UNAVAILABLE_MESSAGE}
    _fail(field, "the host never reported the MCP server as unavailable")
    raise AssertionError("unreachable")


def _fallback_payload(
    events: Sequence[Mapping[str, Any]], *, expected_digest: str, field: str
) -> dict[str, Any]:
    for text in _event_strings(events):
        for candidate in _embedded_objects(text):
            if _go_digest(candidate, sort_keys=True) == expected_digest:
                return candidate
    _fail(
        field,
        "the host's local CLI fallback did not return the exact CLI status payload",
    )
    raise AssertionError("unreachable")


# ---------------------------------------------------------------------------
# Host conformance drivers
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Arm:
    """One measured combination of runtime, packaged assets, and workspace."""

    name: str
    runtime: Path
    assets: Path
    workspace: Path
    cache: Path


@dataclass(frozen=True)
class Harness:
    store: ArtifactStore
    executor: Executor
    scratch: Path
    timeout_seconds: int


def _stage_capture(
    harness: Harness, prefix: str, capture: ProcessCapture
) -> dict[str, Any]:
    argv_ref, stdout_ref, stderr_ref = _capture_ref_set(
        harness.store, prefix=prefix, capture=capture
    )
    return {
        "argv": argv_ref.json(),
        "stdout": stdout_ref.json(),
        "stderr": stderr_ref.json(),
        "exitCode": capture.exit_code,
        "startedAt": capture.started_at,
        "finishedAt": capture.finished_at,
    }


def _run_mcp_host(arm: Arm, harness: Harness) -> tuple[list[dict[str, Any]], list[Any]]:
    trials: list[dict[str, Any]] = []
    captures: list[Any] = []
    discovery_request = {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}
    capture, response, wire = _mcp_exchange(
        runtime=arm.runtime,
        assets=arm.assets,
        workspace=arm.workspace,
        request=discovery_request,
        executor=harness.executor,
        timeout_seconds=harness.timeout_seconds,
    )
    captures.append(_stage_capture(harness, "host/mcp/discovery", capture))
    tools = response["result"].get("tools")
    trials.append(
        _host_trial(
            host="mcp",
            scenario="discovery",
            request=_host_request(
                host="mcp",
                scenario="discovery",
                surface={"method": "tools/list"},
                wire=wire,
            ),
            result=response,
            failure=None,
            exit_code=capture.exit_code,
            semantic=_normalized_semantic(
                "discovery", tools, field="mcp.discovery"
            ),
        )
    )
    for scenario, tool, arguments in _conformance_calls():
        request = {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/call",
            "params": {"name": tool, "arguments": arguments},
        }
        capture, response, wire = _mcp_exchange(
            runtime=arm.runtime,
            assets=arm.assets,
            workspace=arm.workspace,
            request=request,
            executor=harness.executor,
            timeout_seconds=harness.timeout_seconds,
        )
        captures.append(_stage_capture(harness, f"host/mcp/{scenario}", capture))
        payload, is_error = _tool_payload(response, field=f"mcp.{scenario}")
        failure = None
        if scenario == "role-boundary":
            if not is_error:
                _fail("mcp.role-boundary", "the runtime did not refuse the role")
            failure = {
                "code": "role-boundary-refused",
                "message": _role_boundary_reason(
                    str(payload), field="mcp.role-boundary"
                ),
            }
        elif is_error:
            _fail(f"mcp.{scenario}", f"tool call reported an error: {str(payload)[:300]}")
        trials.append(
            _host_trial(
                host="mcp",
                scenario=scenario,
                request=_host_request(
                    host="mcp",
                    scenario=scenario,
                    surface={"tool": tool, "arguments": arguments},
                    wire=wire,
                ),
                result=response,
                failure=failure,
                exit_code=capture.exit_code,
                semantic=_normalized_semantic(
                    scenario, payload, field=f"mcp.{scenario}"
                ),
            )
        )
    return trials, captures


def _cli_status_request() -> tuple[str, str, dict[str, Any]]:
    for row in _conformance_calls():
        if row[0] == "status":
            return row
    _fail("cli.status", "the conformance matrix lost its status scenario")
    raise AssertionError("unreachable")


def _run_cli_host(arm: Arm, harness: Harness) -> tuple[list[dict[str, Any]], list[Any]]:
    trials: list[dict[str, Any]] = []
    captures: list[Any] = []
    for scenario, tool, arguments in _conformance_calls():
        capture, argv, wire = _cli_invoke(
            runtime=arm.runtime,
            assets=arm.assets,
            workspace=arm.workspace,
            command=CLI_COMMANDS[tool],
            request=arguments,
            scratch=harness.scratch,
            executor=harness.executor,
            timeout_seconds=harness.timeout_seconds,
            label=f"cli-{scenario}",
        )
        captures.append(_stage_capture(harness, f"host/cli/{scenario}", capture))
        stdout_text = capture.stdout.decode("utf-8", errors="replace")
        stderr_text = capture.stderr.decode("utf-8", errors="replace")
        failure = None
        if scenario == "role-boundary":
            if capture.exit_code == 0:
                _fail("cli.role-boundary", "the CLI did not refuse the role")
            failure = {
                "code": "role-boundary-refused",
                "message": _role_boundary_reason(
                    stderr_text, field="cli.role-boundary"
                ),
            }
            payload: Any = stderr_text
            result: Any = {
                "exitCode": capture.exit_code,
                "stdout": stdout_text,
                "stderr": stderr_text,
            }
        else:
            if capture.exit_code != 0:
                _fail(
                    f"cli.{scenario}",
                    f"exited {capture.exit_code}: {stderr_text[:300]}",
                )
            payload = _strict_json(capture.stdout, f"cli.{scenario}")
            result = payload
        trials.append(
            _host_trial(
                host="cli",
                scenario=scenario,
                request=_host_request(
                    host="cli",
                    scenario=scenario,
                    surface={"command": CLI_COMMANDS[tool], "argv": argv, "input": arguments},
                    wire=wire,
                ),
                result=result,
                failure=failure,
                exit_code=capture.exit_code,
                semantic=_normalized_semantic(
                    scenario, payload, field=f"cli.{scenario}"
                ),
            )
        )
    return trials, captures


def _run_agent_host(
    host: str,
    *,
    executable: Path,
    model: str,
    arm: Arm,
    harness: Harness,
    proxy: Path,
) -> tuple[list[dict[str, Any]], list[Any]]:
    calls = _conformance_calls()
    prompt = _conformance_prompt(calls)
    log = harness.scratch / f"{host}-conformance-proxy.jsonl"
    if log.exists():
        _fail("hostProxy", f"refuses an existing capture log {log}")
    command = _proxy_argv(
        proxy, log, runtime=arm.runtime, assets=arm.assets, workspace=arm.workspace
    )
    tools = sorted({tool for _, tool, _ in calls})
    if host == "claude":
        config = harness.scratch / f"{host}-conformance-mcp.json"
        _write_private_json(
            config,
            {
                "mcpServers": {
                    MCP_SERVER_NAME: {
                        "command": command[0],
                        "args": command[1:],
                        "cwd": str(arm.workspace),
                        "env": {
                            "LOCALAPPDATA": str(arm.cache),
                            "XDG_CACHE_HOME": str(arm.cache),
                        },
                    }
                }
            },
        )
        argv = _claude_conformance_argv(
            executable=executable,
            model=model,
            prompt=prompt,
            config=config,
            allowed_tools=",".join(f"mcp__{MCP_SERVER_NAME}__{tool}" for tool in tools),
            disallowed_tools="Bash,Write,Edit,Read,Glob,Grep,WebFetch,WebSearch,Task",
        )
        environment: Mapping[str, str] | None = {
            **os.environ,
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
        }
    else:
        argv = _codex_conformance_argv(
            executable=executable,
            model=model,
            prompt=prompt,
            workspace=arm.workspace,
            overrides=_codex_host_overrides(
                command=command,
                workspace=arm.workspace,
                enabled_tools=tools,
                required=True,
                shell_tool=False,
            ),
            sandbox="read-only",
        )
        environment = None
    capture = harness.executor.run(
        argv,
        cwd=arm.workspace,
        env=environment,
        timeout_seconds=harness.timeout_seconds,
    )
    captures = [_stage_capture(harness, f"host/{host}/session", capture)]
    if capture.exit_code != 0:
        _fail(host, f"conformance session exited {capture.exit_code}")
    rows = _proxy_records(log, field=f"{host}.proxy")
    captures.append(
        harness.store.write_bytes(
            f"host/{host}/mcp-traffic.jsonl", _regular_bytes(log, field="proxy.log")
        ).json()
    )
    trials: list[dict[str, Any]] = []
    discovery = _proxy_tools_list(rows, field=f"{host}.discovery")
    trials.append(
        _host_trial(
            host=host,
            scenario="discovery",
            request=_host_request(
                host=host,
                scenario="discovery",
                surface={"method": "tools/list", "server": MCP_SERVER_NAME},
                wire=_canonical_bytes(
                    {"method": "tools/list", "server": MCP_SERVER_NAME, "host": host}
                ),
            ),
            result={"tools": discovery},
            failure=None,
            exit_code=capture.exit_code,
            semantic=_normalized_semantic(
                "discovery", discovery, field=f"{host}.discovery"
            ),
        )
    )
    for scenario, tool, arguments in calls:
        call, response = _proxy_tool_call(
            rows, tool=tool, arguments=arguments, field=f"{host}.{scenario}"
        )
        payload, is_error = _tool_payload(response, field=f"{host}.{scenario}")
        failure = None
        if scenario == "role-boundary":
            if not is_error:
                _fail(f"{host}.role-boundary", "the host did not observe a refusal")
            failure = {
                "code": "role-boundary-refused",
                "message": _role_boundary_reason(
                    str(payload), field=f"{host}.role-boundary"
                ),
            }
        elif is_error:
            _fail(
                f"{host}.{scenario}",
                f"tool call reported an error: {str(payload)[:300]}",
            )
        trials.append(
            _host_trial(
                host=host,
                scenario=scenario,
                request=_host_request(
                    host=host,
                    scenario=scenario,
                    surface={"tool": tool, "arguments": arguments},
                    wire=_jsonrpc_lines([call]),
                ),
                result=response,
                failure=failure,
                exit_code=capture.exit_code,
                semantic=_normalized_semantic(
                    scenario, payload, field=f"{host}.{scenario}"
                ),
            )
        )
    return trials, captures


def _run_agent_fallback(
    host: str,
    *,
    executable: Path,
    model: str,
    arm: Arm,
    harness: Harness,
    proxy: Path,
    status_digest: str,
) -> tuple[dict[str, Any], list[Any]]:
    _scenario, tool, arguments = _cli_status_request()
    input_path = harness.scratch / f"{host}-fallback-request.json"
    _write_private_json(input_path, arguments)
    command = [
        str(arm.runtime),
        CLI_COMMANDS[tool],
        "--asset-root",
        str(arm.assets),
        "--project-root",
        str(arm.workspace),
        "--input",
        str(input_path),
    ]
    prompt = _fallback_prompt(command)
    log = harness.scratch / f"{host}-fallback-proxy.jsonl"
    if log.exists():
        _fail("hostProxy", f"refuses an existing capture log {log}")
    broken = _proxy_argv(proxy, log, unavailable=True)
    if host == "claude":
        config = harness.scratch / f"{host}-fallback-mcp.json"
        _write_private_json(
            config,
            {
                "mcpServers": {
                    MCP_SERVER_NAME: {
                        "command": broken[0],
                        "args": broken[1:],
                        "cwd": str(arm.workspace),
                    }
                }
            },
        )
        argv = _claude_conformance_argv(
            executable=executable,
            model=model,
            prompt=prompt,
            config=config,
            allowed_tools="Bash",
            disallowed_tools="Write,Edit,WebFetch,WebSearch,Task",
        )
        environment: Mapping[str, str] | None = {
            **os.environ,
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
        }
    else:
        argv = _codex_conformance_argv(
            executable=executable,
            model=model,
            prompt=prompt,
            workspace=arm.workspace,
            overrides=_codex_host_overrides(
                command=broken,
                workspace=arm.workspace,
                enabled_tools=(),
                required=False,
                shell_tool=True,
            ),
            sandbox="workspace-write",
        )
        environment = None
    capture = harness.executor.run(
        argv,
        cwd=arm.workspace,
        env=environment,
        timeout_seconds=harness.timeout_seconds,
    )
    captures = [_stage_capture(harness, f"host/{host}/local-fallback", capture)]
    if capture.exit_code != 0:
        _fail(f"{host}.local-fallback", f"session exited {capture.exit_code}")
    events = _agent_events(capture, field=f"{host}.fallback.stdout")
    failure = _unavailable_failure(events, capture, field=f"{host}.local-fallback")
    payload = _fallback_payload(
        events, expected_digest=status_digest, field=f"{host}.local-fallback"
    )
    trial = _host_trial(
        host=host,
        scenario="local-fallback",
        request=_host_request(
            host=host,
            scenario="local-fallback",
            surface={"command": command, "input": arguments},
            wire=_canonical_bytes({"argv": command, "input": arguments}),
        ),
        result={"exitCode": 0, "stdout": payload},
        failure=failure,
        exit_code=capture.exit_code,
        semantic=_normalized_semantic(
            "local-fallback", payload, field=f"{host}.local-fallback"
        ),
    )
    return trial, captures


def _run_host_conformance(
    *,
    arm: Arm,
    harness: Harness,
    claude: Path,
    codex: Path | None,
    claude_model: str,
    codex_model: str,
) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    proxy = _write_proxy(harness.scratch)
    proxy_digest, proxy_bytes = _hash_file(proxy)
    harness.store.write_bytes("host/mcp-capture-proxy.py", proxy.read_bytes())
    trials: list[dict[str, Any]] = []
    captures: dict[str, Any] = {
        "proxy": {"sha256": proxy_digest, "byteCount": proxy_bytes}
    }
    mcp_trials, mcp_captures = _run_mcp_host(arm, harness)
    trials.extend(mcp_trials)
    captures["mcp"] = mcp_captures
    cli_trials, cli_captures = _run_cli_host(arm, harness)
    trials.extend(cli_trials)
    captures["cli"] = cli_captures
    status_digest = next(
        row["semanticDigest"]
        for row in cli_trials
        if row["scenario"] == "status"
    )
    agent_rows = [("claude", claude, claude_model)]
    if codex is not None:
        agent_rows.append(("codex", codex, codex_model))
    for host, executable, model in agent_rows:
        host_trials, host_captures = _run_agent_host(
            host, executable=executable, model=model, arm=arm, harness=harness, proxy=proxy
        )
        trials.extend(host_trials)
        fallback, fallback_captures = _run_agent_fallback(
            host,
            executable=executable,
            model=model,
            arm=arm,
            harness=harness,
            proxy=proxy,
            status_digest=status_digest,
        )
        trials.append(fallback)
        captures[host] = list(host_captures) + list(fallback_captures)
    return trials, captures


# ---------------------------------------------------------------------------
# Blinded paired evaluation
# ---------------------------------------------------------------------------


def _safe_component(value: str, *, field: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", value):
        _fail(field, f"{value!r} is not a safe artifact path component")
    return value


def _prepare_arm(
    *,
    name: str,
    source: Path,
    runtime: Path,
    assets: Path,
    workspace_root: Path,
    required_paths: Sequence[str],
) -> Arm:
    workspace = workspace_root / name / "project"
    cache = workspace_root / name / "cache"
    cache.mkdir(parents=True, exist_ok=False)
    _copy_managed_workspace(source, workspace, required_paths=required_paths)
    return Arm(name, runtime, assets, workspace, cache)


def _assert_evals_withheld(arm: Arm, inventory: Sequence[Mapping[str, Any]]) -> dict[str, Any]:
    """Prove no evaluation byte reached a workspace an agent can read."""

    withheld = {str(row["digest"]) for row in inventory}
    rows = _inventory(arm.workspace)
    for row in rows:
        path = str(row["path"])
        if path == EVAL_ROOT or path.startswith(EVAL_ROOT + "/"):
            _fail("evalIsolation", f"{arm.name} retained {path}")
        if str(row["digest"]) in withheld:
            _fail("evalIsolation", f"{arm.name} retained evaluation bytes at {path}")
    return {
        "arm": arm.name,
        "fileCount": len(rows),
        "inventoryDigest": _inventory_digest(rows),
    }


def _blinded_trial(
    *,
    host: str,
    executable: Path,
    model: str,
    case: Mapping[str, Any],
    judging_case: Mapping[str, Any],
    condition: str,
    arm: Arm,
    benchmark_outcome: Mapping[str, Any],
    harness: Harness,
) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any]]:
    case_id = _safe_component(str(case["id"]), field="evalCase.id")
    request = _blinded_request(case)
    agent = AGENT_IDENTITIES[host]
    agent_digest = _go_digest({"agent": agent, "model": model})
    query_digest = _sha256(str(request["query"]).encode("utf-8"))
    trial_id = _stable_id("MBE", case_id, condition, agent_digest, query_digest)
    prefix = f"blinded/{host}/{condition}/{case_id}"
    scratch = harness.scratch / "blinded" / host / condition / case_id
    scratch.mkdir(parents=True, exist_ok=False)
    runner = _run_claude_trial if host == "claude" else _run_codex_trial
    # The last released 0.7 runtime names its retrieval tool `search`; the
    # 0.8 runtime names it `query`. Each arm is driven and judged through
    # its own runtime's actual tool surface.
    retrieval_tool = "query" if condition == "compiled" else "search"
    # Each arm is judged against the evaluation corpus that describes that
    # arm's own tree. The compiled arm answers from converted destinations;
    # the legacy arm's correct evidence lives at the paths the pre-migration
    # corpus names for the same case. Judging the legacy baseline against
    # converted destinations that do not exist in a 0.7 checkout would score
    # every legacy trial zero and make the paired comparison meaningless.
    # The frozen request, blinding, and identities always come from the
    # canonical case; only the expectation fields may differ per arm.
    #
    # A protocol violation by the respondent - an over-budget trial, a
    # malformed structured answer, a forbidden call - is a failure of the
    # interface, not a scored retrieval result, so the trial is re-run with a
    # fresh context a bounded number of times. A scored miss never raises and
    # is never retried: it is the measurement.
    attempts = 3
    for attempt in range(1, attempts + 1):
        attempt_scratch = scratch / f"attempt-{attempt}"
        attempt_scratch.mkdir(parents=True, exist_ok=False)
        try:
            capture, transcript, invocation = runner(
                executable=executable,
                model=model,
                request=request,
                condition=condition,
                workspace=arm.workspace,
                runtime=arm.runtime,
                assets=arm.assets,
                cache=arm.cache,
                scratch=attempt_scratch,
                executor=harness.executor,
                timeout_seconds=harness.timeout_seconds,
                retrieval_tool=retrieval_tool,
            )
            judgment, scores = _derive_judgment(
                trial_id=trial_id,
                eval_case=judging_case,
                benchmark_outcome=benchmark_outcome,
                transcript=transcript,
                workspace=arm.workspace,
                retrieval_tool=retrieval_tool,
            )
            outcome = _blinded_case_outcome(
                case_id=case_id,
                condition=condition,
                answerable=_case_answerable(case),
                agent=agent,
                model=model,
                request=request,
                response=scores["response"],
                scores=scores,
                benchmark_outcome_digest=_benchmark_outcome_digest(benchmark_outcome),
            )
            break
        except EvidenceError:
            if attempt == attempts:
                raise
    invocation = dict(invocation)
    invocation["attempts"] = attempt
    if outcome["id"] != trial_id:
        _fail("blindedCase.id", "trial identity is not reproducible")
    refs = _stage_capture(harness, prefix, capture)
    refs["invocation"] = harness.store.write_json(
        f"{prefix}/invocation.json", invocation
    ).json()
    refs["judgment"] = harness.store.write_json(f"{prefix}/judgment.json", judgment).json()
    refs["trialId"] = trial_id
    refs["arm"] = arm.name
    return outcome, judgment, refs


def _legacy_judging_cases(
    legacy_project: Path,
    cases: Sequence[Mapping[str, Any]],
) -> tuple[dict[str, dict[str, Any]], list[dict[str, Any]]]:
    """Map each canonical holdout case to its legacy-corpus counterpart.

    The legacy arm's ground truth is what the pre-migration corpus expected
    for the same case ID. Answerability must agree with the canonical case
    because the verifier binds every trial, in both conditions, to the
    canonical case's answerability.
    """

    legacy_holdout, legacy_inventory = _load_eval_cases(legacy_project)
    by_id = {str(row["id"]): row for row in legacy_holdout}
    result: dict[str, dict[str, Any]] = {}
    for case in cases:
        case_id = str(case["id"])
        legacy_case = by_id.get(case_id)
        if legacy_case is None:
            _fail(
                "legacyEvalCorpus",
                f"legacy corpus omits holdout case {case_id}; the legacy arm has no ground truth for it",
            )
        if _case_answerable(legacy_case) != _case_answerable(case):
            _fail(
                "legacyEvalCorpus",
                f"holdout case {case_id} changed answerability across the migration",
            )
        result[case_id] = dict(legacy_case)
    return result, legacy_inventory


def _run_blinded_suite(
    *,
    cases: Sequence[Mapping[str, Any]],
    legacy_cases: Mapping[str, Mapping[str, Any]],
    outcomes: Mapping[str, Mapping[str, Any]],
    arms: Mapping[str, Arm],
    harness: Harness,
    hosts: Mapping[str, tuple[Path, str]],
    tested_host: str,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]], list[dict[str, Any]]]:
    tested: list[dict[str, Any]] = []
    cross: list[dict[str, Any]] = []
    refs: list[dict[str, Any]] = []
    if tested_host not in hosts:
        _fail("blindedEvaluation", f"tested host {tested_host} has no executable")
    for case in cases:
        case_id = str(case["id"])
        if case_id not in outcomes:
            _fail(
                "blindedEvaluation",
                f"holdout case {case_id} has no primary-profile benchmark outcome",
            )
        for condition in ("legacy", "compiled"):
            judging_case = case if condition == "compiled" else legacy_cases[case_id]
            for host, (executable, model) in hosts.items():
                outcome, _judgment, row = _blinded_trial(
                    host=host,
                    executable=executable,
                    model=model,
                    case=case,
                    judging_case=judging_case,
                    condition=condition,
                    arm=arms[condition],
                    benchmark_outcome=outcomes[case_id],
                    harness=harness,
                )
                row["host"] = host
                row["condition"] = condition
                row["caseId"] = case_id
                refs.append(row)
                if host == tested_host:
                    tested.append(outcome)
                else:
                    cross.append(outcome)
    return tested, cross, refs


# ---------------------------------------------------------------------------
# Command line
# ---------------------------------------------------------------------------


def _existing_directory(value: str) -> Path:
    path = Path(value).expanduser()
    if path.is_symlink() or not path.is_dir():
        raise argparse.ArgumentTypeError(f"{value!r} is not a real directory")
    return path.resolve(strict=True)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="re_discipline_migration_external_evidence",
        description=(
            "Capture strict external evidence for a re-discipline 0.7-to-0.8 "
            "migration: a blinded paired agent evaluation and the host "
            "conformance matrix."
        ),
    )
    parser.add_argument(
        "--plugin-repository",
        required=True,
        type=_existing_directory,
        help="clean checkout of the plugin repository holding both runtimes",
    )
    parser.add_argument(
        "--plugin-revision",
        required=True,
        help="exact 40-character commit the plugin repository is checked out at",
    )
    parser.add_argument(
        "--project",
        required=True,
        type=_existing_directory,
        help="disposable, already-activated migrated project",
    )
    parser.add_argument(
        "--legacy-project",
        required=True,
        type=_existing_directory,
        help="disposable pre-migration 0.7 checkout of the same project",
    )
    parser.add_argument(
        "--benchmark",
        required=True,
        help="project-relative path to the final full project-benchmark-v1 report",
    )
    parser.add_argument(
        "--calibration",
        required=True,
        help="project-relative path to the final non-activating calibration report",
    )
    parser.add_argument(
        "--output",
        required=True,
        help="evidence output root; the path must not already exist",
    )
    parser.add_argument(
        "--destination-prefix",
        default=".re-discipline/migration/0.8/evidence",
        help="project-relative prefix the staged artifacts will occupy",
    )
    parser.add_argument("--claude-executable", required=True)
    parser.add_argument(
        "--codex-executable",
        default="",
        help=(
            "optional: supply only when the project uses the Codex host; the "
            "mandatory conformance surfaces are MCP, CLI, and Claude Code"
        ),
    )
    parser.add_argument("--claude-model", required=True)
    parser.add_argument(
        "--codex-model",
        default="",
        help="required with --codex-executable",
    )
    parser.add_argument(
        "--tested-host",
        choices=("claude", "codex"),
        default="claude",
        help="host whose paired trials become the blinded evaluation cases",
    )
    parser.add_argument(
        "--workspace-root",
        default="",
        help="disposable workspace root; a temporary directory is used when empty",
    )
    parser.add_argument("--timeout-seconds", type=int, default=900)
    return parser


def _execution_manifest(
    *,
    arguments: argparse.Namespace,
    plugin_binding: Mapping[str, Any],
    legacy_binding: Mapping[str, Any],
    executables: Mapping[str, Any],
    authentication: Mapping[str, Any],
    eval_inventory: Sequence[Mapping[str, Any]],
    legacy_eval_inventory: Sequence[Mapping[str, Any]],
    isolation: Sequence[Mapping[str, Any]],
    blinded_refs: Sequence[Mapping[str, Any]],
    cross_cases: Sequence[Mapping[str, Any]],
    host_captures: Mapping[str, Any],
    parity: Mapping[str, str],
    blinded_ref: ArtifactRef,
    host_ref: ArtifactRef | None,
    started_at: str,
    elapsed_seconds: int,
) -> dict[str, Any]:
    artifacts: dict[str, Any] = {"blindedEvaluation": blinded_ref.json()}
    if host_ref is not None:
        artifacts["hostConformance"] = host_ref.json()
    return {
        "schemaVersion": 1,
        "suite": EXECUTION_SUITE,
        "agentHosts": ["claude"] + (["codex"] if arguments.codex_executable else []),
        "startedAt": started_at,
        "finishedAt": _utc_now(),
        "elapsedSeconds": elapsed_seconds,
        "formal": True,
        "testedHost": arguments.tested_host,
        "pluginRepository": dict(plugin_binding),
        "legacyPluginRevision": dict(legacy_binding),
        "executables": dict(executables),
        "authentication": dict(authentication),
        "withheldEvaluationFiles": [dict(row) for row in eval_inventory],
        "withheldEvaluationDigest": _inventory_digest(eval_inventory),
        "withheldLegacyEvaluationFiles": [dict(row) for row in legacy_eval_inventory],
        "withheldLegacyEvaluationDigest": _inventory_digest(legacy_eval_inventory),
        "workspaceIsolation": [dict(row) for row in isolation],
        "blindedTrials": [dict(row) for row in blinded_refs],
        "crossHostCases": [dict(row) for row in cross_cases],
        "hostCaptures": dict(host_captures),
        "semanticParity": dict(parity),
        "artifacts": artifacts,
    }


def _execute(arguments: argparse.Namespace) -> int:
    started_at = _utc_now()
    started = time.monotonic()
    if arguments.timeout_seconds < FORMAL_MIN_TIMEOUT:
        _fail("timeoutSeconds", f"formal mode requires at least {FORMAL_MIN_TIMEOUT}")
    if not REVISION_RE.fullmatch(arguments.plugin_revision):
        _fail("pluginRevision", "must be a full lowercase 40-character Git commit")
    executor: Executor = SubprocessExecutor()
    _require_formal_executor(executor, formal=True)
    store = ArtifactStore(Path(arguments.output).expanduser(), arguments.destination_prefix)

    claude = _resolve_executable(arguments.claude_executable, expected="claude")
    codex: Path | None = None
    if arguments.codex_executable:
        if not arguments.codex_model:
            _fail("codexModel", "--codex-executable requires --codex-model")
        codex = _resolve_executable(arguments.codex_executable, expected="codex")
    elif arguments.codex_model:
        _fail("codexExecutable", "--codex-model requires --codex-executable")
    elif arguments.tested_host != "claude":
        _fail("testedHost", "testing the codex host requires --codex-executable")
    authentication = _check_authentication(
        claude=claude,
        codex=codex,
        executor=executor,
        cwd=arguments.project,
        store=store,
        timeout_seconds=arguments.timeout_seconds,
    )

    plugin = arguments.plugin_repository
    plugin_binding = _repo_binding(plugin, arguments.plugin_revision, label="pluginRepository")
    transaction_id, plan_digest, _state = _migration_identity(arguments.project)

    # The evaluation corpus is read, fingerprinted, and then withheld: no
    # workspace an agent can reach receives any of these bytes. The legacy
    # corpus supplies the legacy arm's ground truth and is withheld the same
    # way.
    holdout, eval_inventory = _load_eval_cases(arguments.project)
    legacy_cases, legacy_eval_inventory = _legacy_judging_cases(
        arguments.legacy_project, holdout
    )
    benchmark_path = _contained(arguments.project, arguments.benchmark, field="benchmark")
    _report, outcomes, benchmark_body = _load_benchmark(benchmark_path)
    benchmark_digest = _sha256(benchmark_body)
    calibration_path = _contained(
        arguments.project, arguments.calibration, field="calibration"
    )
    calibration_digest = _sha256(_regular_bytes(calibration_path, field="calibration"))
    for case in holdout:
        if str(case["id"]) not in outcomes:
            _fail("benchmark", f"omits current holdout case {case['id']}")
    for case_id in outcomes:
        if case_id not in {str(case["id"]) for case in holdout}:
            _fail("evalRoot", f"benchmark holdout {case_id} is not in the corpus")

    workspace_root = (
        Path(arguments.workspace_root).expanduser()
        if arguments.workspace_root
        else Path(tempfile.mkdtemp(prefix="re-discipline-external-evidence-"))
    )
    workspace_root.mkdir(parents=True, exist_ok=True)
    scratch = workspace_root / "scratch"
    scratch.mkdir(parents=True, exist_ok=False)

    legacy_revision = _find_legacy_plugin_revision(plugin, arguments.plugin_revision)
    legacy_plugin = workspace_root / "legacy-plugin"
    legacy_binding = _clone_legacy_revision(
        plugin, legacy_revision, legacy_plugin, timeout_seconds=arguments.timeout_seconds
    )
    current_runtime = _runtime_path(plugin)
    legacy_runtime = _runtime_path(legacy_plugin)
    current_assets = plugin / Path(*PurePosixPath(CURRENT_ASSETS).parts)
    legacy_assets = legacy_plugin / Path(*PurePosixPath(CURRENT_ASSETS).parts)

    compiled = _prepare_arm(
        name="compiled",
        source=arguments.project,
        runtime=current_runtime,
        assets=current_assets,
        workspace_root=workspace_root,
        required_paths=_workspace_required_paths(holdout),
    )
    legacy = _prepare_arm(
        name="legacy",
        source=arguments.legacy_project,
        runtime=legacy_runtime,
        assets=legacy_assets,
        workspace_root=workspace_root,
        required_paths=(),
    )
    withheld_inventory = list(eval_inventory) + list(legacy_eval_inventory)
    isolation = [
        _assert_evals_withheld(compiled, withheld_inventory),
        _assert_evals_withheld(legacy, withheld_inventory),
    ]

    harness = Harness(store, executor, scratch, arguments.timeout_seconds)
    executables = {
        "claude": _executable_identity(
            key="claude",
            path=claude,
            version_argv=[str(claude), "--version"],
            executor=executor,
            cwd=compiled.workspace,
            store=store,
            timeout_seconds=arguments.timeout_seconds,
        ),
        "current-runtime": _executable_identity(
            key="current-runtime",
            path=current_runtime,
            version_argv=[
                str(current_runtime),
                "status",
                "--asset-root",
                str(current_assets),
                "--project-root",
                str(compiled.workspace),
            ],
            executor=executor,
            cwd=compiled.workspace,
            store=store,
            timeout_seconds=arguments.timeout_seconds,
        ),
        "legacy-runtime": _executable_identity(
            key="legacy-runtime",
            path=legacy_runtime,
            version_argv=[
                str(legacy_runtime),
                "status",
                "--asset-root",
                str(legacy_assets),
                "--project-root",
                str(legacy.workspace),
            ],
            executor=executor,
            cwd=legacy.workspace,
            store=store,
            timeout_seconds=arguments.timeout_seconds,
        ),
    }
    hosts: dict[str, tuple[Path, str]] = {"claude": (claude, arguments.claude_model)}
    if codex is not None:
        executables["codex"] = _executable_identity(
            key="codex",
            path=codex,
            version_argv=[str(codex), "--version"],
            executor=executor,
            cwd=compiled.workspace,
            store=store,
            timeout_seconds=arguments.timeout_seconds,
        )
        hosts["codex"] = (codex, arguments.codex_model)

    tested_cases, cross_cases, blinded_refs = _run_blinded_suite(
        cases=holdout,
        legacy_cases=legacy_cases,
        outcomes=outcomes,
        arms={"compiled": compiled, "legacy": legacy},
        harness=harness,
        hosts=hosts,
        tested_host=arguments.tested_host,
    )
    if codex is not None:
        cross_host = "codex" if arguments.tested_host == "claude" else "claude"
        cross_model = (
            arguments.codex_model if cross_host == "codex" else arguments.claude_model
        )
        evaluator = f"{AGENT_IDENTITIES[cross_host]}:{cross_model}"
    else:
        # Scoring is derived deterministically by this harness from captured
        # transcripts and the arm's own evaluation corpus; no second agent
        # participates. Name that procedure, bound to these exact harness
        # bytes, instead of a provider that did not run.
        harness_digest, _harness_bytes = _hash_file(Path(__file__).resolve())
        evaluator = f"deterministic-judgment:{harness_digest}"
    evaluation = _seal_blinded_evaluation(
        transaction_id=transaction_id,
        plan_digest=plan_digest,
        benchmark_digest=benchmark_digest,
        calibration_digest=calibration_digest,
        evaluator=evaluator,
        cases=tested_cases,
    )
    store.write_json("blinded/cross-host-trials.json", cross_cases)

    agent_hosts = ["claude"] + (["codex"] if codex is not None else [])
    trials, host_captures = _run_host_conformance(
        arm=compiled,
        harness=harness,
        claude=claude,
        codex=codex,
        claude_model=arguments.claude_model,
        codex_model=arguments.codex_model,
    )
    parity = _assert_semantic_parity(trials, agent_hosts)
    conformance = _seal_host_conformance(
        transaction_id=transaction_id, plan_digest=plan_digest, trials=trials
    )

    blinded_ref = store.write_json(
        "migration-blinded-agent-evaluation.json", evaluation
    )
    host_ref = store.write_json("migration-host-conformance.json", conformance)
    manifest = _execution_manifest(
        arguments=arguments,
        plugin_binding=plugin_binding,
        legacy_binding={"revision": legacy_revision, **legacy_binding},
        executables=executables,
        authentication=authentication,
        eval_inventory=eval_inventory,
        legacy_eval_inventory=legacy_eval_inventory,
        isolation=isolation,
        blinded_refs=blinded_refs,
        cross_cases=cross_cases,
        host_captures=host_captures,
        parity=parity,
        blinded_ref=blinded_ref,
        host_ref=host_ref,
        started_at=started_at,
        elapsed_seconds=int(time.monotonic() - started),
    )
    manifest_ref = store.write_json("execution-manifest.json", manifest)
    store.write_json("copy-map.json", store.copy_map())

    references = [
        ("blindedEvaluation", blinded_ref),
        ("executionManifest", manifest_ref),
    ]
    if host_ref is not None:
        references.insert(1, ("hostConformance", host_ref))
    for label, reference in references:
        print(f"{label} destination {reference.path}")
        print(f"{label} staged {store.physical_path(reference)}")
        print(f"{label} sha256 {reference.digest}")
    if not evaluation["passed"] or (conformance is not None and not conformance["passed"]):
        _fail(
            "certification",
            "measured evidence does not support a passing gate; the artifacts "
            "record exactly what was observed",
        )
    return 0


def main(argv: Sequence[str] | None = None) -> int:
    arguments = _parser().parse_args(list(argv) if argv is not None else None)
    try:
        return _execute(arguments)
    except EvidenceError as error:
        print(f"external evidence failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
