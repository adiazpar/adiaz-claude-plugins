"""Build a reproducible project retrieval lane-ablation receipt.

The builder consumes a fresh two-arm benchmark from the current runtime and a
separately frozen three-arm pre-removal evidence archive. It independently
replays both evaluation projections, verifies every arm and case join,
recomputes the Go runtime's quality metrics, and derives every comparison,
slice, uncertainty, sensitivity, and lane-decision field. Cross-runtime arms
are never spliced into one comparison.

Only the Python standard library is used so the command is suitable for CI and
for a clean, disposable project checkout.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import math
import re
import sqlite3
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Iterable

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_project_lane_ablation import (
    validate_json_schema,
    validate_project_lane_ablation_report,
)


class MeasurementBuildError(ValueError):
    """Raised when a source artifact cannot prove a closed measurement."""


IDENTITY_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
REVISION_RE = re.compile(r"^[0-9a-f]{40}$")
CANONICAL_BUDGETS = (512, 1024, 2048, 4096)
BOOTSTRAP_REPLICATES = 10_000
BOOTSTRAP_SEED = 0x5A17_2026
OUTCOME_ARRAY_FIELDS = (
    "paths",
    "tiers",
    "chunkIds",
    "contentHashes",
    "relevantPaths",
    "relevantRanks",
    "hardNegativeHits",
    "expectedCitationsFound",
)
OUTCOME_FIELDS = (
    "caseId",
    "split",
    "topic",
    *OUTCOME_ARRAY_FIELDS,
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
METRIC_FIELDS = (
    "recallAtK",
    "meanReciprocalRank",
    "nDCG",
    "precisionAtK",
    "exactIdentifierHitRate",
    "completeEvidenceCoverage",
    "abstentionAccuracy",
    "citationPrecision",
    "citationRecall",
    "supportingEvidenceRecall",
    "budgetComplianceRate",
    "authorityViolationRate",
    "staleResultRate",
    "authorityViolations",
    "citationViolations",
    "citationMetadataViolations",
    "hardNegativeHits",
    "relevantTokenRatio",
    "duplicateTokenRatio",
    "deterministicReplayRate",
    "p50LatencyMillis",
    "p95LatencyMillis",
)
HISTORICAL_PROFILES = {
    "baseline": ("lexical-graph-v1", ("exact", "fts", "graph")),
    "dense": (
        "hybrid-no-rerank-v1",
        ("exact", "fts", "graph", "dense"),
    ),
    "rerank": (
        "hybrid-local-v1",
        ("exact", "fts", "graph", "dense", "rerank"),
    ),
}
CURRENT_PROFILES = {
    role: HISTORICAL_PROFILES[role] for role in ("baseline", "dense")
}


def _fail(path: str, message: str) -> None:
    raise MeasurementBuildError(f"{path}: {message}")


def _strict_json_loads(data: str | bytes, path: str) -> Any:
    def reject_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        value: dict[str, Any] = {}
        for key, item in pairs:
            if key in value:
                raise MeasurementBuildError(f"{path}: duplicate JSON key {key!r}")
            value[key] = item
        return value

    def reject_constant(value: str) -> None:
        raise MeasurementBuildError(f"{path}: non-finite JSON number {value!r}")

    return json.loads(
        data,
        object_pairs_hook=reject_duplicates,
        parse_constant=reject_constant,
    )


def _read_json(path: Path) -> dict[str, Any]:
    try:
        if path.is_symlink():
            raise MeasurementBuildError(f"{path}: source artifacts may not be symbolic links")
        value = _strict_json_loads(path.read_text(encoding="utf-8"), str(path))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise MeasurementBuildError(f"cannot read JSON {path}: {error}") from error
    if not isinstance(value, dict):
        raise MeasurementBuildError(f"{path}: top-level JSON value must be an object")
    return value


def _identity(data: bytes) -> str:
    return "sha256:" + hashlib.sha256(data).hexdigest()


def _file_identity(path: Path) -> str:
    return _identity(path.read_bytes())


def _go_json_bytes(value: Any) -> bytes:
    """Approximate encoding/json's compact output for the scalar corpus data."""

    body = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    body = (
        body.replace("&", "\\u0026")
        .replace("<", "\\u003c")
        .replace(">", "\\u003e")
        .replace("\u2028", "\\u2028")
        .replace("\u2029", "\\u2029")
    )
    return body.encode("utf-8")


def _stable_output_bytes(report: dict[str, Any]) -> bytes:
    return (
        json.dumps(report, ensure_ascii=False, indent=2, sort_keys=False) + "\n"
    ).encode("utf-8")


def _normalize_eval_for_go(case: dict[str, Any]) -> dict[str, Any]:
    """Return EvalCase in Go struct field order with omitempty behavior."""

    required = (
        "id",
        "role",
        "topic",
        "split",
        "query",
        "queryClass",
        "allowedTiers",
        "corpusSnapshot",
        "expectedPaths",
        "minimumEvidencePaths",
        "hardNegativePaths",
        "expectedCitations",
        "forbiddenTiers",
        "tokenBudget",
        "answerable",
    )
    missing = [field for field in required if field not in case]
    if missing:
        _fail(f"eval[{case.get('id', '?')}]", f"missing fields {missing!r}")
    ordered: dict[str, Any] = {}
    field_order = (
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
    for field in field_order:
        if field not in case:
            continue
        value = case[field]
        if field in {"vocabularyPolicy", "gradedRelevantPaths", "evidencePins"} and not value:
            continue
        if field == "gradedRelevantPaths":
            if not isinstance(value, dict):
                _fail(f"eval[{case['id']}].gradedRelevantPaths", "must be an object")
            value = {key: value[key] for key in sorted(value)}
        elif field == "evidencePins":
            normalized_pins = []
            for pin in value:
                normalized = {
                    "path": pin["path"],
                    "claimSha256": pin["claimSha256"],
                }
                if pin.get("contentSha256"):
                    normalized["contentSha256"] = pin["contentSha256"]
                normalized_pins.append(normalized)
            value = normalized_pins
        ordered[field] = value
    return ordered


_PROJECTION_LINE_RE = re.compile(
    r'^[ \t]*"vocabularyPolicy"[ \t]*:[ \t]*'
    r'"target-disjoint-v1"[ \t]*,[ \t]*(?:\r\n|\n)?$'
)


def _project_bytes(body: bytes, path: str) -> tuple[bytes, list[dict[str, Any]]]:
    try:
        text = body.decode("utf-8", errors="strict")
    except UnicodeDecodeError as error:
        _fail(path, f"is not strict UTF-8: {error}")
    projected: list[str] = []
    removals: list[dict[str, Any]] = []
    for line_number, line in enumerate(text.splitlines(keepends=True), start=1):
        if _PROJECTION_LINE_RE.fullmatch(line):
            encoded = line.encode("utf-8")
            removals.append(
                {
                    "line": line_number,
                    "value": "target-disjoint-v1",
                    "removedLineSha256": _identity(encoded),
                    "removedByteCount": len(encoded),
                }
            )
        else:
            projected.append(line)
    return "".join(projected).encode("utf-8"), removals


def _manifest_transform_digest(manifest: dict[str, Any]) -> str:
    unsigned = copy.deepcopy(manifest)
    claimed = unsigned.pop("digest", None)
    if not IDENTITY_RE.fullmatch(str(claimed)):
        _fail("projection.digest", "must be a sha256 identity")
    computed = _identity(_go_json_bytes(unsigned))
    if claimed != computed:
        _fail("projection.digest", f"is {claimed}, recomputed {computed}")
    return computed


def _load_eval_projection(
    final_root: Path,
    projected_root: Path,
    manifest: dict[str, Any],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]], list[dict[str, Any]], str]:
    if manifest.get("schemaVersion") != 1:
        _fail("projection.schemaVersion", "must equal 1")
    expected_header = {
        "transform": "delete-whole-json-member-line-v1",
        "field": "vocabularyPolicy",
        "allowedValue": "target-disjoint-v1",
        "preservesAllOtherBytes": True,
        "finalFileCount": 9,
        "caseCount": 64,
    }
    for field, expected in expected_header.items():
        if manifest.get(field) != expected:
            _fail(f"projection.{field}", f"must equal {expected!r}")
    rows = manifest.get("files")
    if not isinstance(rows, list) or len(rows) != 9:
        _fail("projection.files", "must contain exactly nine rows")
    paths = [row.get("path") for row in rows if isinstance(row, dict)]
    if len(paths) != 9 or len(set(paths)) != len(paths):
        _fail("projection.files", "paths must be unique")

    for root_name, root in (("final", final_root), ("projected", projected_root)):
        if root.is_symlink() or any(path.is_symlink() for path in root.rglob("*")):
            _fail(f"{root_name}EvalRoot", "may not contain symbolic links")
    final_inventory = sorted(
        path.relative_to(final_root).as_posix()
        for path in final_root.rglob("*")
        if path.is_file()
    )
    projected_inventory = sorted(
        path.relative_to(projected_root).as_posix()
        for path in projected_root.rglob("*")
        if path.is_file()
    )
    manifest_inventory = []
    for path in paths:
        marker = ".re-discipline/knowledge/evals/"
        if (
            not isinstance(path, str)
            or not path.startswith(marker)
            or "\\" in path
            or any(part in {"", ".", ".."} for part in path.split("/"))
            or any(ord(character) < 32 for character in path)
        ):
            _fail("projection.files.path", "must be rooted under project evals")
        manifest_inventory.append(path.split(marker, 1)[1])
    if final_inventory != sorted(manifest_inventory):
        _fail("projection.files", "does not exactly match the final eval inventory")
    if projected_inventory != sorted(manifest_inventory):
        _fail("projection.files", "does not exactly match the projected eval inventory")

    final_cases: list[dict[str, Any]] = []
    projected_cases: list[dict[str, Any]] = []
    report_rows: list[dict[str, Any]] = []
    total_removed = 0
    total_cases = 0
    for index, (relative, row) in enumerate(zip(manifest_inventory, rows)):
        base = f"projection.files[{index}]"
        final_body = (final_root / relative).read_bytes()
        projected_body = (projected_root / relative).read_bytes()
        expected_projected, removals = _project_bytes(final_body, row["path"])
        if projected_body != expected_projected:
            _fail(base, "projected file differs outside the declared line deletion")
        expected = {
            "path": row["path"],
            "finalSha256": _identity(final_body),
            "projectedSha256": _identity(projected_body),
            "finalByteCount": len(final_body),
            "projectedByteCount": len(projected_body),
            "removals": removals,
        }
        for field, value in expected.items():
            if row.get(field) != value:
                _fail(f"{base}.{field}", "does not match replayed projection")
        if relative.lower().endswith(".json"):
            try:
                loaded_final = _strict_json_loads(final_body, str(final_root / relative))
                loaded_projected = _strict_json_loads(
                    projected_body, str(projected_root / relative)
                )
            except json.JSONDecodeError as error:
                _fail(base, f"invalid eval JSON: {error}")
            if not isinstance(loaded_final, list) or not isinstance(loaded_projected, list):
                _fail(base, "eval JSON must contain a case array")
            case_count = len(loaded_final)
            if len(loaded_projected) != case_count:
                _fail(base, "projection changed the case count")
            final_cases.extend(loaded_final)
            projected_cases.extend(loaded_projected)
        else:
            case_count = 0
            if removals:
                _fail(base, "non-JSON file unexpectedly contains projection removals")
        if row.get("caseCount") != case_count:
            _fail(f"{base}.caseCount", f"is {row.get('caseCount')}, want {case_count}")
        total_cases += case_count
        total_removed += len(removals)
        report_rows.append(
            {
                "path": row["path"],
                "finalSha256": expected["finalSha256"],
                "projectedSha256": expected["projectedSha256"],
                "finalByteCount": len(final_body),
                "projectedByteCount": len(projected_body),
                "caseCount": case_count,
                "removedOccurrences": len(removals),
            }
        )
    if total_cases != 64 or manifest.get("caseCount") != total_cases:
        _fail("projection.caseCount", f"must equal replayed total 64, got {total_cases}")
    if total_removed < 1 or manifest.get("removedOccurrences") != total_removed:
        _fail("projection.removedOccurrences", "does not match replayed removals")
    final_ids = [case.get("id") for case in final_cases]
    projected_ids = [case.get("id") for case in projected_cases]
    if len(set(final_ids)) != 64 or final_ids != projected_ids:
        _fail("evals", "final/projected case IDs must be the same unique 64-case order")
    eval_fingerprint = _identity(
        _go_json_bytes([_normalize_eval_for_go(case) for case in projected_cases])
    )
    return final_cases, projected_cases, report_rows, eval_fingerprint


def _profile_role(
    profile: dict[str, Any], path: str, expected: dict[str, tuple[str, tuple[str, ...]]]
) -> str:
    for role, (name, lanes) in expected.items():
        if profile.get("profileName") == name and tuple(profile.get("activeLanes") or ()) == lanes:
            return role
    _fail(path, "does not match a controlled profile name and ordered lane set")
    raise AssertionError("unreachable")


def _profiles_by_role(
    raw: dict[str, Any], expected: dict[str, tuple[str, tuple[str, ...]]]
) -> dict[str, dict[str, Any]]:
    profiles = raw.get("profiles")
    if not isinstance(profiles, list) or len(profiles) != len(expected):
        _fail("raw.profiles", f"must contain exactly {len(expected)} controlled arms")
    result: dict[str, dict[str, Any]] = {}
    for index, profile in enumerate(profiles):
        if not isinstance(profile, dict):
            _fail(f"raw.profiles[{index}]", "must be an object")
        role = _profile_role(profile, f"raw.profiles[{index}]", expected)
        if role in result:
            _fail("raw.profiles", f"duplicates controlled role {role!r}")
        result[role] = profile
    if set(result) != set(expected):
        _fail("raw.profiles", "does not contain all controlled roles")
    return result


def _validate_profile_catalog(
    catalog: dict[str, Any],
    profiles: dict[str, dict[str, Any]],
    expected: dict[str, tuple[str, tuple[str, ...]]],
) -> None:
    rows = catalog.get("effectiveProfiles")
    if not isinstance(rows, list) or len(rows) != len(expected):
        _fail("profileCatalog.effectiveProfiles", "has the wrong controlled arm count")
    by_name: dict[str, dict[str, Any]] = {}
    for index, row in enumerate(rows):
        if not isinstance(row, dict) or not isinstance(row.get("name"), str):
            _fail(f"profileCatalog.effectiveProfiles[{index}]", "has no name")
        if row["name"] in by_name:
            _fail("profileCatalog.effectiveProfiles", "duplicates a profile name")
        by_name[row["name"]] = row
    for role, (name, lanes) in expected.items():
        row = by_name.get(name)
        if row is None or tuple(row.get("lanes") or ()) != lanes:
            _fail(
                f"profileCatalog.effectiveProfiles[{role}]",
                "does not match the controlled name and ordered lanes",
            )
        if profiles[role]["profileName"] != name:
            _fail("profileCatalog.effectiveProfiles", "differs from measured arms")


def _normalize_outcome(value: dict[str, Any], path: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        _fail(path, "must be an object")
    missing = [field for field in OUTCOME_FIELDS if field not in value]
    if missing:
        _fail(path, f"missing outcome fields {missing!r}")
    result: dict[str, Any] = {}
    for field in OUTCOME_FIELDS:
        field_value = value[field]
        if field in OUTCOME_ARRAY_FIELDS and field_value is None:
            field_value = []
        result[field] = copy.deepcopy(field_value)
    lengths = [len(result[field]) for field in ("paths", "tiers", "chunkIds", "contentHashes")]
    if len(set(lengths)) != 1:
        _fail(path, "paths, tiers, chunkIds, and contentHashes lengths differ")
    if len(result["relevantPaths"]) != len(result["relevantRanks"]):
        _fail(path, "relevantPaths and relevantRanks lengths differ")
    if result["returnedUniquePaths"] != len(set(result["paths"])):
        _fail(path, "returnedUniquePaths does not equal the unique path count")
    safety = (
        result["authoritySafe"]
        and result["citationMetadataSafe"]
        and result["corpusMatched"]
        and result["budgetSafe"]
        and result["replayIdentical"]
        and result["staleResults"] == 0
    )
    quality = (not result["qualityGateApplicable"]) or (
        result["expectedFound"]
        and result["completeEvidence"]
        and result["citationSafe"]
        and result["abstentionCorrect"]
        and not result["hardNegativeHits"]
    )
    if result["safetyPassed"] != safety:
        _fail(path + ".safetyPassed", "does not match safety predicates")
    if result["qualityPassed"] != quality:
        _fail(path + ".qualityPassed", "does not match quality predicates")
    if result["gatePassed"] != (safety and quality):
        _fail(path + ".gatePassed", "does not equal safetyPassed && qualityPassed")
    return result


def _case_map(
    rows: Any,
    path: str,
    eval_by_id: dict[str, dict[str, Any]],
) -> dict[str, dict[str, Any]]:
    if not isinstance(rows, list) or len(rows) != 64:
        _fail(path, "must contain exactly 64 case outcomes")
    result: dict[str, dict[str, Any]] = {}
    for index, value in enumerate(rows):
        outcome = _normalize_outcome(value, f"{path}[{index}]")
        case_id = outcome["caseId"]
        if case_id in result:
            _fail(path, f"duplicates caseId {case_id!r}")
        eval_case = eval_by_id.get(case_id)
        if eval_case is None:
            _fail(path, f"contains unknown caseId {case_id!r}")
        for field in ("split", "topic"):
            if outcome[field] != eval_case[field]:
                _fail(f"{path}[{index}].{field}", "does not match eval metadata")
        if bool(eval_case["answerable"]):
            if outcome["expectedFound"] != bool(outcome["relevantRanks"]):
                _fail(f"{path}[{index}].expectedFound", "does not match answerable evidence")
        elif outcome["expectedFound"] != outcome["abstentionCorrect"]:
            _fail(f"{path}[{index}].expectedFound", "does not match abstention outcome")
        result[case_id] = outcome
    if set(result) != set(eval_by_id):
        _fail(path, "case IDs do not exactly equal the frozen eval corpus")
    return result


def _percentile(values: list[int], quantile: float) -> int:
    if not values:
        return 0
    ordered = sorted(values)
    index = max(0, math.ceil(len(ordered) * quantile) - 1)
    return ordered[index]


def _calculate_metrics(
    outcomes: Iterable[dict[str, Any]],
    eval_by_id: dict[str, dict[str, Any]],
) -> dict[str, int | float]:
    rows = list(outcomes)
    expected = found = returned = relevant = 0
    total_tokens = relevant_tokens = duplicate_tokens = 0
    expected_citations = found_citations = returned_citations = stale_results = 0
    reciprocal = ndcg = 0.0
    violations = citation_violations = citation_metadata_violations = 0
    hard_negative_hits = replay = complete_evidence = abstention_correct = budget_safe = 0
    exact_cases = exact_hits = 0
    latencies: list[int] = []
    for outcome in rows:
        eval_case = eval_by_id[outcome["caseId"]]
        expected += len(eval_case["expectedPaths"])
        found += len(outcome["relevantPaths"])
        returned += outcome["returnedUniquePaths"]
        relevant += len(outcome["relevantPaths"])
        total_tokens += outcome["returnedTokens"]
        relevant_tokens += outcome["relevantTokens"]
        duplicate_tokens += outcome["duplicateTokens"]
        expected_citations += len(eval_case["expectedCitations"])
        found_citations += len(outcome["expectedCitationsFound"])
        returned_citations += outcome["returnedUniquePaths"]
        stale_results += outcome["staleResults"]
        if eval_case["queryClass"] == "exact" and eval_case["answerable"]:
            exact_cases += 1
            if outcome["relevantRanks"]:
                exact_hits += 1
                reciprocal += 1.0 / outcome["relevantRanks"][0]
        complete_evidence += int(outcome["completeEvidence"])
        abstention_correct += int(outcome["abstentionCorrect"])
        budget_safe += int(outcome["budgetSafe"])
        violations += int(not outcome["authoritySafe"])
        citation_violations += int(not outcome["citationSafe"])
        citation_metadata_violations += int(not outcome["citationMetadataSafe"])
        hard_negative_hits += len(outcome["hardNegativeHits"])
        replay += int(outcome["replayIdentical"])
        grades = [
            int((eval_case.get("gradedRelevantPaths") or {}).get(path, 1))
            for path in eval_case["expectedPaths"]
        ]
        dcg = 0.0
        for path, rank in zip(outcome["relevantPaths"], outcome["relevantRanks"]):
            grade = int((eval_case.get("gradedRelevantPaths") or {}).get(path, 1))
            dcg += ((1 << grade) - 1) / math.log2(rank + 1)
        ideal = sum(
            ((1 << grade) - 1) / math.log2(rank + 2)
            for rank, grade in enumerate(sorted(grades, reverse=True))
        )
        if ideal > 0:
            ndcg += dcg / ideal
        elif not outcome["paths"]:
            ndcg += 1.0
        latencies.append(outcome["latencyMillis"])

    count = len(rows)
    metrics: dict[str, int | float] = {
        "recallAtK": found / expected if expected else 0.0,
        "meanReciprocalRank": reciprocal / exact_cases if exact_cases else 0.0,
        "nDCG": ndcg / count if count else 0.0,
        "precisionAtK": relevant / returned if returned else 0.0,
        "exactIdentifierHitRate": exact_hits / exact_cases if exact_cases else 0.0,
        "completeEvidenceCoverage": complete_evidence / count if count else 0.0,
        "abstentionAccuracy": abstention_correct / count if count else 0.0,
        "citationPrecision": found_citations / returned_citations if returned_citations else 0.0,
        "citationRecall": found_citations / expected_citations if expected_citations else 0.0,
        "supportingEvidenceRecall": found / expected if expected else 0.0,
        "budgetComplianceRate": budget_safe / count if count else 0.0,
        "authorityViolationRate": violations / count if count else 0.0,
        "staleResultRate": stale_results / returned_citations if returned_citations else 0.0,
        "authorityViolations": violations,
        "citationViolations": citation_violations,
        "citationMetadataViolations": citation_metadata_violations,
        "hardNegativeHits": hard_negative_hits,
        "relevantTokenRatio": relevant_tokens / total_tokens if total_tokens else 0.0,
        "duplicateTokenRatio": duplicate_tokens / total_tokens if total_tokens else 0.0,
        "deterministicReplayRate": replay / count if count else 0.0,
        "p50LatencyMillis": _percentile(latencies, 0.50),
        "p95LatencyMillis": _percentile(latencies, 0.95),
    }
    return metrics


def _assert_metrics(actual: Any, expected: dict[str, int | float], path: str) -> None:
    if not isinstance(actual, dict) or set(actual) != set(METRIC_FIELDS):
        _fail(path, "does not contain the exact quality metric field set")
    for field in METRIC_FIELDS:
        observed = actual[field]
        wanted = expected[field]
        if isinstance(wanted, int):
            if observed != wanted:
                _fail(f"{path}.{field}", f"is {observed!r}, recomputed {wanted!r}")
        elif not isinstance(observed, (int, float)) or not math.isclose(
            observed, wanted, rel_tol=1e-12, abs_tol=1e-15
        ):
            _fail(f"{path}.{field}", f"is {observed!r}, recomputed {wanted!r}")


def _comparison(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
    before_hit = before["expectedFound"]
    after_hit = after["expectedFound"]
    if not before_hit and after_hit:
        effect = "rescue"
    elif before_hit and not after_hit:
        effect = "loss"
    else:
        effect = "unchanged"
    before_rank = min(before["relevantRanks"]) if before["relevantRanks"] else 0
    after_rank = min(after["relevantRanks"]) if after["relevantRanks"] else 0
    both_hit = before_hit and after_hit
    return {
        "hitEffect": effect,
        "rankDelta": before_rank - after_rank if both_hit else 0,
        "pathsChanged": before["paths"] != after["paths"],
        "uniqueRescue": effect == "rescue",
        "rankImproved": both_hit and after_rank < before_rank,
        "rankDegraded": both_hit and after_rank > before_rank,
    }


def _event_counts(cases: Iterable[dict[str, Any]], comparison: str) -> dict[str, int]:
    rows = [case[comparison] for case in cases]
    return {
        "rescues": sum(int(row["uniqueRescue"]) for row in rows),
        "losses": sum(int(row["hitEffect"] == "loss") for row in rows),
        "rankImprovements": sum(int(row["rankImproved"]) for row in rows),
        "rankDegradations": sum(int(row["rankDegraded"]) for row in rows),
    }


def _slice(name: str, cases: list[dict[str, Any]]) -> dict[str, Any]:
    dense = _event_counts(cases, "denseComparison")
    return {
        "name": name,
        "caseCount": len(cases),
        "denseRescues": dense["rescues"],
        "denseLosses": dense["losses"],
        "denseRankImprovements": dense["rankImprovements"],
        "denseRankDegradations": dense["rankDegradations"],
    }


def _all_slices(cases: list[dict[str, Any]]) -> list[dict[str, Any]]:
    selectors: list[tuple[str, Callable[[dict[str, Any]], bool]]] = [
        ("all", lambda _case: True),
        ("split:development", lambda case: case["split"] == "development"),
        ("split:holdout", lambda case: case["split"] == "holdout"),
        ("role:manager", lambda case: case["role"] == "manager"),
        ("role:drafter", lambda case: case["role"] == "drafter"),
    ]
    selectors.extend(
        (f"topic:{topic}", lambda case, topic=topic: case["topic"] == topic)
        for topic in sorted({case["topic"] for case in cases})
    )
    return [_slice(name, [case for case in cases if selector(case)]) for name, selector in selectors]


def _wilson(slice_name: str, event: str, successes: int, trials: int) -> dict[str, Any]:
    if trials < 1:
        _fail("uncertainty", f"Wilson slice {slice_name!r} is empty")
    z = 1.959963984540054
    estimate = successes / trials
    denominator = 1 + (z * z) / trials
    center = (estimate + (z * z) / (2 * trials)) / denominator
    radius = z * math.sqrt(
        estimate * (1 - estimate) / trials + (z * z) / (4 * trials * trials)
    ) / denominator
    return {
        "slice": slice_name,
        "event": event,
        "successes": successes,
        "trials": trials,
        "confidence": 0.95,
        "estimate": estimate,
        "lower": max(0.0, center - radius),
        "upper": min(1.0, center + radius),
    }


def _uncertainty(cases: list[dict[str, Any]]) -> dict[str, Any]:
    dense_slices = (
        ("all-cases", cases),
        ("answerable-cases", [case for case in cases if case["answerable"]]),
        ("holdout-cases", [case for case in cases if case["split"] == "holdout"]),
        (
            "target-disjoint-holdout",
            [
                case
                for case in cases
                if case["split"] == "holdout"
                and case["vocabularyPolicy"] == "target-disjoint-v1"
            ],
        ),
    )
    intervals = [
        _wilson(
            name,
            "dense-unique-rescue",
            sum(int(case["denseComparison"]["uniqueRescue"]) for case in selected),
            len(selected),
        )
        for name, selected in dense_slices
    ]
    answerable = [case for case in cases if case["answerable"]]
    topics = sorted({case["topic"] for case in answerable})
    grouped = {topic: [case for case in answerable if case["topic"] == topic] for topic in topics}

    def hit_delta(rows: list[dict[str, Any]]) -> float:
        if not rows:
            return 0.0
        return (
            sum(int(case["dense"]["expectedFound"]) for case in rows)
            - sum(int(case["baseline"]["expectedFound"]) for case in rows)
        ) / len(rows)

    replicates: list[float] = []
    for replicate in range(BOOTSTRAP_REPLICATES):
        sampled: list[dict[str, Any]] = []
        for draw in range(len(topics)):
            digest = hashlib.sha256(
                f"{BOOTSTRAP_SEED}\0{replicate}\0{draw}".encode("ascii")
            ).digest()
            topic = topics[int.from_bytes(digest[:8], "big") % len(topics)]
            sampled.extend(grouped[topic])
        replicates.append(hit_delta(sampled))
    ordered = sorted(replicates)

    def quantile(value: float) -> float:
        return ordered[max(0, math.ceil(len(ordered) * value) - 1)]

    bootstrap = {
        "method": "topic-cluster-percentile-v1",
        "clusterKey": "topic",
        "clusterCount": len(topics),
        "replicates": BOOTSTRAP_REPLICATES,
        "seed": BOOTSTRAP_SEED,
        "estimand": "answerable dense-minus-baseline expected-hit-rate delta",
        "pointEstimate": hit_delta(answerable),
        "lower": quantile(0.025),
        "median": quantile(0.5),
        "upper": quantile(0.975),
        "zeroOrLowerFraction": sum(value <= 0 for value in replicates) / len(replicates),
    }
    return {"wilsonIntervals": intervals, "topicClusterBootstrap": bootstrap}


def _budget_case_maps(
    profiles: dict[str, dict[str, Any]],
    eval_by_id: dict[str, dict[str, Any]],
) -> dict[int, dict[str, dict[str, dict[str, Any]]]]:
    result: dict[int, dict[str, dict[str, dict[str, Any]]]] = {}
    for budget in CANONICAL_BUDGETS:
        arms: dict[str, dict[str, dict[str, Any]]] = {}
        for role, profile in profiles.items():
            by_budget = profile.get("casesByBudget")
            if not isinstance(by_budget, dict) or set(by_budget) != {
                str(value) for value in CANONICAL_BUDGETS
            }:
                _fail(f"raw.profiles[{role}].casesByBudget", "must cover the four budgets")
            arms[role] = _case_map(
                by_budget[str(budget)],
                f"raw.profiles[{role}].casesByBudget[{budget}]",
                eval_by_id,
            )
        result[budget] = arms
    return result


def _budget_slices(
    budgets: dict[int, dict[str, dict[str, dict[str, Any]]]],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    rows = []
    comparison_sets = []
    for budget in CANONICAL_BUDGETS:
        arms = budgets[budget]
        cases = []
        for case_id in sorted(arms["baseline"]):
            cases.append(
                {
                    "caseId": case_id,
                    "denseComparison": _comparison(
                        arms["baseline"][case_id], arms["dense"][case_id]
                    ),
                }
            )
        comparison_sets.append({"tokenBudget": budget, "cases": cases})
        dense = _event_counts(cases, "denseComparison")
        rows.append(
            {
                "tokenBudget": budget,
                "denseRescues": dense["rescues"],
                "denseLosses": dense["losses"],
                "denseRankImprovements": dense["rankImprovements"],
                "denseRankDegradations": dense["rankDegradations"],
            }
        )
    return rows, comparison_sets


def _historical_rerank_budget_slices(
    budgets: dict[int, dict[str, dict[str, dict[str, Any]]]],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    rows: list[dict[str, Any]] = []
    comparison_sets: list[dict[str, Any]] = []
    for budget in CANONICAL_BUDGETS:
        arms = budgets[budget]
        cases = [
            {
                "caseId": case_id,
                "rerankComparison": _comparison(
                    arms["dense"][case_id], arms["rerank"][case_id]
                ),
            }
            for case_id in sorted(arms["dense"])
        ]
        counts = _event_counts(cases, "rerankComparison")
        comparison_sets.append({"tokenBudget": budget, "cases": cases})
        rows.append(
            {
                "tokenBudget": budget,
                "rerankRescues": counts["rescues"],
                "rerankLosses": counts["losses"],
                "rerankRankImprovements": counts["rankImprovements"],
                "rerankRankDegradations": counts["rankDegradations"],
            }
        )
    return rows, comparison_sets


def _safety_regressions(
    profiles: dict[str, dict[str, Any]],
    main_maps: dict[str, dict[str, dict[str, Any]]],
    budget_maps: dict[int, dict[str, dict[str, dict[str, Any]]]],
    eval_by_id: dict[str, dict[str, Any]],
) -> tuple[int, int, int]:
    collections: list[dict[str, dict[str, dict[str, Any]]]] = [main_maps]
    collections.extend(budget_maps[budget] for budget in CANONICAL_BUDGETS)
    # Context-pack outcomes are not copied into the receipt, but they are part
    # of the raw hard gate and therefore participate in regression detection.
    context_main: dict[str, dict[str, dict[str, Any]]] = {}
    for role, profile in profiles.items():
        rows = profile.get("contextPackCases")
        context_main[role] = _case_map(
            rows,
            f"raw.profiles[{role}].contextPackCases",
            eval_by_id,
        )
    collections.append(context_main)
    for budget in CANONICAL_BUDGETS:
        context_budget: dict[str, dict[str, dict[str, Any]]] = {}
        for role, profile in profiles.items():
            by_budget = profile.get("contextPacksByBudget")
            if not isinstance(by_budget, dict) or set(by_budget) != {
                str(value) for value in CANONICAL_BUDGETS
            }:
                _fail(f"raw.profiles[{role}].contextPacksByBudget", "must cover budgets")
            rows = by_budget[str(budget)]
            context_budget[role] = _case_map(
                rows,
                f"raw.profiles[{role}].contextPacksByBudget[{budget}]",
                eval_by_id,
            )
        collections.append(context_budget)

    shared_failures = dense_regressions = rerank_regressions = 0
    has_rerank = "rerank" in profiles
    for collection in collections:
        ids = set(collection["baseline"])
        if ids != set(collection["dense"]) or (
            has_rerank and ids != set(collection["rerank"])
        ):
            _fail("raw.safety", "matched safety collection case sets differ")
        for case_id in ids:
            baseline = bool(collection["baseline"][case_id]["safetyPassed"])
            dense = bool(collection["dense"][case_id]["safetyPassed"])
            rerank = (
                bool(collection["rerank"][case_id]["safetyPassed"])
                if has_rerank
                else dense
            )
            shared_failures += int(
                not baseline and not dense and (not has_rerank or not rerank)
            )
            dense_regressions += int(baseline and not dense)
            if has_rerank:
                rerank_regressions += int(dense and not rerank)
    return shared_failures, dense_regressions, rerank_regressions


def _model_rows(
    profiles: dict[str, dict[str, Any]],
    manifest: dict[str, Any],
    *,
    include_reranker: bool,
) -> list[dict[str, Any]]:
    manifest_models = manifest.get("models")
    if not isinstance(manifest_models, list):
        _fail("modelManifest.models", "must be an array")
    expected_model_count = 2 if include_reranker else 1
    if len(manifest_models) != expected_model_count or any(
        not isinstance(row, dict) for row in manifest_models
    ):
        _fail(
            "modelManifest.models",
            f"must contain exactly {expected_model_count} controlled model(s)",
        )
    by_id = {row.get("id"): row for row in manifest_models}
    if len(by_id) != expected_model_count or None in by_id:
        _fail("modelManifest.models", "must contain unique non-null model IDs")
    baseline_models = profiles["baseline"].get("models") or []
    dense_models = profiles["dense"].get("models") or []
    if baseline_models:
        _fail("raw.profiles[baseline].models", "baseline must have no model")
    dense_ids = {row["id"] for row in dense_models}
    if len(dense_ids) != 1:
        _fail("raw.profiles.models", "dense arm must add exactly one embedding")
    role_ids = {"embedding": next(iter(dense_ids))}
    raw_by_id = {row["id"]: row for row in dense_models}
    if include_reranker:
        rerank_models = profiles["rerank"].get("models") or []
        full_rerank_ids = {row["id"] for row in rerank_models}
        reranker_ids = full_rerank_ids - dense_ids
        if len(reranker_ids) != 1 or not dense_ids <= full_rerank_ids:
            _fail("raw.profiles.models", "does not form embedding -> reranker")
        role_ids["reranker"] = next(iter(reranker_ids))
        raw_by_id = {row["id"]: row for row in rerank_models}
        dense_by_id = {row["id"]: row for row in dense_models}
        embedding_id = role_ids["embedding"]
        if dense_by_id.get(embedding_id) != raw_by_id.get(embedding_id):
            _fail(
                "raw.profiles.models",
                "embedding identity differs between dense and rerank arms",
            )
    output = []
    roles = ("embedding", "reranker") if include_reranker else ("embedding",)
    for role in roles:
        model_id = role_ids[role]
        raw_model = raw_by_id[model_id]
        declared = by_id.get(model_id)
        if declared is None or declared.get("role") != role:
            _fail("modelManifest.models", f"does not declare {role} model {model_id!r}")
        for field in ("revision", "implementation", "specSha256"):
            if raw_model.get(field) != declared.get(field):
                _fail(f"modelManifest.models[{model_id}].{field}", "differs from raw identity")
        if raw_model.get("artifactSha256", "") != declared.get("artifactSha256", ""):
            _fail(f"modelManifest.models[{model_id}].artifactSha256", "differs from raw identity")
        row = {
            "id": model_id,
            "role": role,
            "revision": raw_model["revision"],
            "implementation": raw_model["implementation"],
            "specSha256": raw_model["specSha256"],
        }
        if raw_model.get("artifactSha256"):
            row["artifactSha256"] = raw_model["artifactSha256"]
        output.append(row)
    return output


def _receipt_pair_digest(rows: Iterable[tuple[str, str]]) -> str:
    digest = hashlib.sha256()
    seen: set[str] = set()
    for relative, identity in sorted(rows):
        if (
            not isinstance(relative, str)
            or not relative
            or "\\" in relative
            or relative.startswith("/")
            or re.match(r"^[A-Za-z]:", relative)
            or any(part in ("", ".", "..") for part in relative.split("/"))
        ):
            _fail("harness.pathDigestPairs", f"unsafe path {relative!r}")
        if relative in seen:
            _fail("harness.pathDigestPairs", f"duplicate path {relative!r}")
        if not IDENTITY_RE.fullmatch(str(identity)):
            _fail("harness.pathDigestPairs", f"invalid identity for {relative!r}")
        seen.add(relative)
        digest.update(relative.encode("utf-8"))
        digest.update(b"\x00")
        digest.update(identity.encode("ascii"))
        digest.update(b"\x00")
    return "sha256:" + digest.hexdigest()


def _receipt_path(root: Path, relative: Any, field: str) -> Path:
    if (
        not isinstance(relative, str)
        or not relative
        or "\\" in relative
        or relative.startswith("/")
        or re.match(r"^[A-Za-z]:", relative)
        or any(part in ("", ".", "..") for part in relative.split("/"))
    ):
        _fail(field, "must be a canonical contained relative path")
    try:
        root_resolved = root.resolve(strict=True)
        path = (root / Path(*relative.split("/"))).resolve(strict=True)
    except OSError as error:
        raise MeasurementBuildError(f"{field}: cannot resolve artifact: {error}") from error
    try:
        path.relative_to(root_resolved)
    except ValueError:
        _fail(field, "escapes the harness artifact root")
    return path


def _receipt_file(
    artifact_root: Path, reference: Any, field: str
) -> Path:
    if not isinstance(reference, dict):
        _fail(field, "must be an artifact reference")
    path = _receipt_path(artifact_root, reference.get("path"), f"{field}.path")
    if path.is_symlink() or not path.is_file():
        _fail(field, "must name a regular artifact")
    body = path.read_bytes()
    expected = {
        "path": reference["path"],
        "sha256": _identity(body),
        "byteCount": len(body),
    }
    if reference != expected:
        _fail(field, "size or digest differs from the staged artifact")
    return path


def _receipt_directory(
    artifact_root: Path, reference: Any, field: str
) -> Path:
    if not isinstance(reference, dict):
        _fail(field, "must be a directory artifact reference")
    path = _receipt_path(artifact_root, reference.get("path"), f"{field}.path")
    if path.is_symlink() or not path.is_dir():
        _fail(field, "must name a real directory artifact")
    rows: list[tuple[str, str]] = []
    byte_count = 0
    for entry in sorted(path.rglob("*"), key=lambda item: item.as_posix()):
        if entry.is_symlink():
            _fail(field, "may not contain symbolic links")
        if entry.is_dir():
            continue
        if not entry.is_file():
            _fail(field, "may contain only regular files")
        body = entry.read_bytes()
        rows.append((entry.relative_to(path).as_posix(), _identity(body)))
        byte_count += len(body)
    expected = {
        "path": reference["path"],
        "algorithm": "sorted-path-null-sha256-v1",
        "fileCount": len(rows),
        "byteCount": byte_count,
        "manifestSha256": _receipt_pair_digest(rows),
    }
    if reference != expected:
        _fail(field, "inventory or digest differs from the directory artifact")
    return path


def _harness(
    receipt: dict[str, Any],
    *,
    receipt_path: Path,
    schema_path: Path,
    raw: dict[str, Any],
    generation: dict[str, Any],
    raw_path: Path,
    projection_manifest_path: Path,
    final_eval_root: Path,
    projected_eval_root: Path,
    profile_catalog_path: Path,
    model_manifest_path: Path,
    runtime_source_revision: str,
) -> tuple[str, dict[str, Any], dict[str, Any]]:
    harness_schema_path = schema_path.with_name(
        "project-lane-ablation-harness.schema.json"
    )
    harness_schema = _read_json(harness_schema_path)
    try:
        validate_json_schema(harness_schema, receipt)
    except ValueError as error:
        raise MeasurementBuildError(
            f"harness receipt schema validation failed: {error}"
        ) from error
    project = receipt["project"]
    if receipt["sourceRepositoryMutated"] is not False:
        _fail("harness.sourceRepositoryMutated", "must be false")
    repositories = receipt["repositories"]
    if repositories["plugin"]["revision"] != runtime_source_revision:
        _fail("harness.repositories.plugin.revision", "differs from current runtime revision")
    if repositories["project"]["revision"] != generation["gitRevision"]:
        _fail("harness.repositories.project.revision", "differs from benchmark generation")
    if any(
        repository[field] is not True
        for repository in repositories.values()
        for field in ("cleanBefore", "cleanAfter")
    ):
        _fail("harness.repositories", "both repositories must be clean before and after")

    artifact_root = receipt_path.parent
    artifacts = receipt["artifacts"]
    bound_files = {
        "rawBenchmark": raw_path,
        "projectionManifest": projection_manifest_path,
        "profileCatalog": profile_catalog_path,
        "modelManifest": model_manifest_path,
    }
    resolved_files: dict[str, Path] = {}
    for name, supplied in bound_files.items():
        resolved = _receipt_file(
            artifact_root, artifacts[name], f"harness.artifacts.{name}"
        )
        if resolved != supplied.resolve(strict=True):
            _fail(f"harness.artifacts.{name}.path", "differs from builder input")
        resolved_files[name] = resolved
    for name, supplied in (
        ("finalEvalRoot", final_eval_root),
        ("projectedEvalRoot", projected_eval_root),
    ):
        resolved = _receipt_directory(
            artifact_root, artifacts[name], f"harness.artifacts.{name}"
        )
        if resolved != supplied.resolve(strict=True):
            _fail(f"harness.artifacts.{name}.path", "differs from builder input")

    indexed_path = _receipt_file(
        artifact_root, artifacts["indexedSources"], "harness.artifacts.indexedSources"
    )
    pointer_path = _receipt_file(
        artifact_root, artifacts["currentGeneration"], "harness.artifacts.currentGeneration"
    )
    database_path = _receipt_file(
        artifact_root, artifacts["generationDatabase"], "harness.artifacts.generationDatabase"
    )
    indexed = _read_json(indexed_path)
    pointer = _read_json(pointer_path)
    expected_index_header = {
        "$schema": "plugin://re-discipline/schemas/indexed-source-manifest.internal.json",
        "schemaVersion": 1,
        "kind": "project-indexed-source-byte-proof",
        "project": project,
        "projectRevision": generation["gitRevision"],
        "generationId": generation["id"],
        "corpusFingerprint": generation["corpusFingerprint"],
    }
    for field, expected in expected_index_header.items():
        if indexed.get(field) != expected:
            _fail(f"harness.indexedSources.{field}", f"must equal {expected!r}")
    expected_index_keys = set(expected_index_header) | {
        "algorithm",
        "sourceCount",
        "byteExactCount",
        "mismatchCount",
        "pathDigestPairsSha256",
        "sources",
    }
    if set(indexed) != expected_index_keys:
        _fail(
            "harness.indexedSources",
            f"unexpected or missing fields {sorted(set(indexed) ^ expected_index_keys)!r}",
        )
    sources = indexed.get("sources")
    if not isinstance(sources, list) or not sources:
        _fail("harness.indexedSources.sources", "must be a non-empty array")
    source_by_path: dict[str, dict[str, Any]] = {}
    expected_source_keys = {
        "path",
        "tier",
        "sourceKind",
        "size",
        "sha256",
        "checkoutSha256",
        "byteExact",
    }
    for index, source in enumerate(sources):
        if not isinstance(source, dict):
            _fail(f"harness.indexedSources.sources[{index}]", "must be an object")
        path = source.get("path")
        if not isinstance(path, str) or not path:
            _fail(f"harness.indexedSources.sources[{index}].path", "must be non-empty")
        if set(source) != expected_source_keys:
            _fail(
                f"harness.indexedSources.sources[{index}]",
                "has unexpected or missing fields",
            )
        if path in source_by_path:
            _fail("harness.indexedSources.sources", f"duplicates {path!r}")
        if source.get("byteExact") is not True or source.get("sha256") != source.get(
            "checkoutSha256"
        ):
            _fail(f"harness.indexedSources.sources[{index}]", "is not byte exact")
        source_by_path[path] = source
    try:
        connection = sqlite3.connect(
            database_path.resolve(strict=True).as_uri() + "?mode=ro&immutable=1",
            uri=True,
        )
        try:
            database_rows = connection.execute(
                "SELECT path,tier,content_hash,size,source_kind FROM documents ORDER BY path"
            ).fetchall()
        finally:
            connection.close()
    except sqlite3.Error as error:
        raise MeasurementBuildError(
            f"harness generation database cannot be read: {error}"
        ) from error
    if len(database_rows) != len(sources):
        _fail("harness.indexedSources.sourceCount", "differs from generation database")
    for path, tier, content_hash, size, source_kind in database_rows:
        if not re.fullmatch(r"[0-9a-f]{64}", str(content_hash)):
            _fail(
                f"harness.generationDatabase.documents[{path}].content_hash",
                "must be a bare SHA-256",
            )
        source = source_by_path.get(path)
        expected = {
            "path": path,
            "tier": tier,
            "sourceKind": source_kind,
            "size": size,
            "sha256": "sha256:" + content_hash,
            "checkoutSha256": "sha256:" + content_hash,
            "byteExact": True,
        }
        if source != expected:
            _fail(f"harness.indexedSources.sources[{path}]", "differs from generation database")
    proof = {
        "algorithm": "sorted-path-null-sha256-v1",
        "sourceCount": len(sources),
        "byteExactCount": len(sources),
        "mismatchCount": 0,
        "pathDigestPairsSha256": _receipt_pair_digest(
            (source["path"], source["sha256"]) for source in sources
        ),
    }
    for field, expected in proof.items():
        if indexed.get(field) != expected or receipt["indexedSourceProof"].get(field) != expected:
            _fail(f"harness.indexedSourceProof.{field}", "differs from staged source evidence")
    if proof["sourceCount"] != generation["documentCount"]:
        _fail("harness.indexedSourceProof.sourceCount", "differs from generation documentCount")

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
            _fail(f"harness.currentGeneration.{field}", "differs from raw benchmark")
    runtime = receipt["runtime"]
    expected_runtime = {
        "runId": raw["runId"],
        "generationId": generation["id"],
        "corpusFingerprint": generation["corpusFingerprint"],
        "evalFingerprint": raw["evalFingerprint"],
        "projectGitRevision": generation["gitRevision"],
        "runtimeIdentity": generation["runtime"],
        "runtimeIdentitySha256": _identity(_go_json_bytes(generation["runtime"])),
        "profileCatalogSha256": _file_identity(profile_catalog_path),
        "modelManifestSha256": _file_identity(model_manifest_path),
        "armCount": 2,
        "casesPerArm": 64,
    }
    if runtime != expected_runtime:
        _fail("harness.runtime", "does not bind the raw runtime and controlled assets")
    command = receipt["benchmarkCommand"]
    semantic = {"cwd": command["cwd"], "argv": command["argv"]}
    if command["sha256"] != _identity(_go_json_bytes(semantic)):
        _fail("harness.benchmarkCommand.sha256", "does not bind cwd and argv")
    expected_tail = [
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
    ]
    if command["cwd"] != "plugins/re-discipline/knowledge" or command["argv"][1:] != expected_tail:
        _fail("harness.benchmarkCommand", "is not the canonical full benchmark invocation")
    expected_exit = 0 if raw.get("passed") is True else 1
    if command["exitCode"] != expected_exit:
        _fail("harness.benchmarkCommand.exitCode", "differs from raw pass state")
    stage_script = Path(__file__).with_name(
        "re_discipline_project_lane_ablation_stage.py"
    )
    if receipt["tools"]["harnessScriptSha256"] != _file_identity(stage_script):
        _fail("harness.tools.harnessScriptSha256", "differs from the staging implementation")
    if receipt["tools"]["harnessSchemaSha256"] != _file_identity(
        harness_schema_path
    ):
        _fail("harness.tools.harnessSchemaSha256", "differs from the harness schema")
    if receipt["migrationGuard"] != {
        "paths": [
            ".re-discipline/state",
            ".re-discipline/transactions",
            "docs/history/campaigns",
        ],
        "absentBefore": True,
        "absentAfter": True,
    }:
        _fail("harness.migrationGuard", "does not prove a pre-migration staging run")

    substitutions = receipt["controlPlaneSubstitutions"]
    kinds = [row["kind"] for row in substitutions]
    if sorted(kinds) != ["bootstrap-config", "knowledge-policy", "retrieval-profile"]:
        _fail("harness.controlPlaneSubstitutions", "must contain each substitution kind once")
    controls = receipt["negativeControls"]
    for index, control in enumerate(controls):
        if control["expectedFailure"] is not True or control["observed"] is not True:
            _fail(f"harness.negativeControls[{index}]", "must record an observed failure")
    proof_output = copy.deepcopy(proof)
    output = {
        "sourceRepositoryMutated": False,
        "indexedSourceBytesUnchanged": True,
        "receiptSha256": _file_identity(receipt_path),
        "pluginRevision": repositories["plugin"]["revision"],
        "projectRevision": repositories["project"]["revision"],
        "benchmarkCommandSha256": command["sha256"],
        "projectionManifestArtifactSha256": artifacts["projectionManifest"]["sha256"],
        "indexedSourcesManifestSha256": artifacts["indexedSources"]["sha256"],
        "controlPlaneSubstitutions": copy.deepcopy(substitutions),
        "renamedPaths": copy.deepcopy(receipt["renamedPaths"]),
        "excludedSourcePaths": copy.deepcopy(receipt["excludedSourcePaths"]),
        "negativeControls": copy.deepcopy(controls),
    }
    return project, proof_output, output


def _run_time(run_id: str) -> str:
    match = re.fullmatch(r"benchmark-(\d{8}T\d{6})(?:\.\d+)?Z", run_id)
    if match is None:
        _fail("raw.runId", "does not encode a UTC benchmark timestamp")
    parsed = datetime.strptime(match.group(1), "%Y%m%dT%H%M%S").replace(tzinfo=timezone.utc)
    return parsed.isoformat(timespec="seconds").replace("+00:00", "Z")


def _decode_json_bytes(body: bytes, path: str) -> dict[str, Any]:
    encoding = "utf-16" if body.startswith((b"\xff\xfe", b"\xfe\xff")) else "utf-8"
    try:
        value = _strict_json_loads(body.decode(encoding), path)
    except UnicodeError as error:
        _fail(path, f"cannot decode {encoding}: {error}")
    if not isinstance(value, dict):
        _fail(path, "top-level JSON value must be an object")
    return value


def _historical_projection_from_entries(
    manifest: dict[str, Any], entries: dict[str, bytes]
) -> tuple[list[dict[str, Any]], str]:
    expected_eval_names = sorted(
        name for name in entries if name.startswith("evals/") and not name.endswith("/")
    )
    rows = manifest.get("files")
    if not isinstance(rows, list) or len(rows) != 9:
        _fail("historical.projection.files", "must contain nine files")
    row_names = []
    projected_cases: list[dict[str, Any]] = []
    total_cases = total_removals = 0
    removal_line = b'    "vocabularyPolicy": "target-disjoint-v1",\n'
    for index, row in enumerate(rows):
        path = row.get("path")
        marker = ".re-discipline/knowledge/evals/"
        if not isinstance(path, str) or not path.startswith(marker):
            _fail(f"historical.projection.files[{index}].path", "is not canonical")
        name = "evals/" + path[len(marker) :]
        row_names.append(name)
        projected = entries.get(name)
        if projected is None:
            _fail("historical.archive", f"is missing {name}")
        if row.get("projectedSha256") != _identity(projected) or row.get(
            "projectedByteCount"
        ) != len(projected):
            _fail(f"historical.projection.files[{index}]", "projected bytes do not match")
        removals = row.get("removals")
        if not isinstance(removals, list):
            _fail(f"historical.projection.files[{index}].removals", "must be an array")
        removal_by_line: dict[int, dict[str, Any]] = {}
        for removal in removals:
            line = removal.get("line")
            if not isinstance(line, int) or line < 1 or line in removal_by_line:
                _fail(f"historical.projection.files[{index}].removals", "invalid line")
            if (
                removal.get("value") != "target-disjoint-v1"
                or removal.get("removedLineSha256") != _identity(removal_line)
                or removal.get("removedByteCount") != len(removal_line)
            ):
                _fail(f"historical.projection.files[{index}].removals", "invalid deletion")
            removal_by_line[line] = removal
        projected_lines = projected.splitlines(keepends=True)
        final_parts: list[bytes] = []
        projected_index = 0
        final_line_count = len(projected_lines) + len(removal_by_line)
        for line in range(1, final_line_count + 1):
            if line in removal_by_line:
                final_parts.append(removal_line)
            else:
                if projected_index >= len(projected_lines):
                    _fail(f"historical.projection.files[{index}]", "line map overflows")
                final_parts.append(projected_lines[projected_index])
                projected_index += 1
        if projected_index != len(projected_lines):
            _fail(f"historical.projection.files[{index}]", "line map is incomplete")
        final = b"".join(final_parts)
        if row.get("finalSha256") != _identity(final) or row.get("finalByteCount") != len(final):
            _fail(f"historical.projection.files[{index}]", "reconstructed final bytes differ")
        if name.lower().endswith(".json"):
            loaded = _strict_json_loads(projected, name)
            if not isinstance(loaded, list):
                _fail(name, "must be an eval case array")
            case_count = len(loaded)
            projected_cases.extend(loaded)
        else:
            case_count = 0
        if row.get("caseCount") != case_count:
            _fail(f"historical.projection.files[{index}].caseCount", "does not match")
        total_cases += case_count
        total_removals += len(removals)
    if len(set(row_names)) != len(row_names) or sorted(row_names) != expected_eval_names:
        _fail("historical.projection.files", "does not exactly match archived eval inventory")
    if (
        total_cases != 64
        or manifest.get("caseCount") != 64
        or total_removals != manifest.get("removedOccurrences")
        or manifest.get("transform") != "delete-whole-json-member-line-v1"
        or manifest.get("preservesAllOtherBytes") is not True
    ):
        _fail("historical.projection", "aggregate projection fields do not replay")
    ids = [case.get("id") for case in projected_cases]
    if len(ids) != 64 or len(set(ids)) != 64:
        _fail("historical.evals", "must contain 64 unique cases")
    fingerprint = _identity(
        _go_json_bytes([_normalize_eval_for_go(case) for case in projected_cases])
    )
    return projected_cases, fingerprint


def _load_historical_archive(
    archive_path: Path,
) -> tuple[
    dict[str, Any],
    dict[str, Any],
    dict[str, Any],
    dict[str, Any],
    list[dict[str, Any]],
    str,
    dict[str, bytes],
    bytes,
]:
    if archive_path.is_symlink():
        _fail("historicalArchive", "may not be a symbolic link")
    archive_body = archive_path.read_bytes()
    try:
        with zipfile.ZipFile(archive_path, "r") as archive:
            if archive.comment != b"":
                _fail("historicalArchive", "ZIP comment must be empty")
            infos = archive.infolist()
            names = [info.filename for info in infos]
            if names != sorted(names) or len(names) != len(set(names)):
                _fail("historicalArchive", "entries must be unique and sorted")
            entries: dict[str, bytes] = {}
            for info in infos:
                if (
                    info.is_dir()
                    or info.filename.startswith(("/", "\\"))
                    or "\\" in info.filename
                    or any(part in {"", ".", ".."} for part in info.filename.split("/"))
                    or info.date_time != (1980, 1, 1, 0, 0, 0)
                    or info.compress_type != zipfile.ZIP_DEFLATED
                    or info.create_system != 3
                    or info.create_version != 20
                    or info.extract_version != 20
                    or info.flag_bits != 0
                    or info.internal_attr != 0
                    or info.external_attr != (0o100644 << 16)
                    or info.extra != b""
                    or info.comment != b""
                ):
                    _fail("historicalArchive", f"entry {info.filename!r} is non-canonical")
                entries[info.filename] = archive.read(info)
    except (OSError, zipfile.BadZipFile) as error:
        raise MeasurementBuildError(f"cannot read historical archive: {error}") from error
    required = {
        "raw-benchmark.json",
        "projection-manifest.json",
        "profile-catalog.json",
        "model-manifest.json",
    }
    if not required <= set(entries) or len([name for name in entries if name.startswith("evals/")]) != 9:
        _fail("historicalArchive", "does not contain the frozen evidence inventory")
    if set(entries) != required | {name for name in entries if name.startswith("evals/")}:
        _fail("historicalArchive", "contains an unexpected entry")
    raw_body = entries["raw-benchmark.json"]
    raw = _decode_json_bytes(raw_body, "historicalArchive/raw-benchmark.json")
    manifest = _decode_json_bytes(
        entries["projection-manifest.json"], "historicalArchive/projection-manifest.json"
    )
    catalog = _decode_json_bytes(
        entries["profile-catalog.json"], "historicalArchive/profile-catalog.json"
    )
    models = _decode_json_bytes(
        entries["model-manifest.json"], "historicalArchive/model-manifest.json"
    )
    _manifest_transform_digest(manifest)
    evals, fingerprint = _historical_projection_from_entries(manifest, entries)
    return raw, manifest, catalog, models, evals, fingerprint, entries, archive_body


def _profile_receipts(
    profiles: dict[str, dict[str, Any]],
    maps: dict[str, dict[str, dict[str, Any]]],
    eval_by_id: dict[str, dict[str, Any]],
    roles: tuple[str, ...],
) -> tuple[list[dict[str, Any]], dict[str, dict[str, int | float]]]:
    receipts: list[dict[str, Any]] = []
    computed_by_role: dict[str, dict[str, int | float]] = {}
    for role in roles:
        profile = profiles[role]
        ordered = [maps[role][row["caseId"]] for row in profile["cases"]]
        metrics = _calculate_metrics(ordered, eval_by_id)
        _assert_metrics(profile.get("metrics"), metrics, f"raw.profiles[{role}].metrics")
        split_metrics: dict[str, dict[str, int | float]] = {}
        for split in ("development", "holdout"):
            selected = [row for row in ordered if eval_by_id[row["caseId"]]["split"] == split]
            computed = _calculate_metrics(selected, eval_by_id)
            _assert_metrics(
                (profile.get("metricsBySplit") or {}).get(split),
                computed,
                f"raw.profiles[{role}].metricsBySplit.{split}",
            )
            split_metrics[split] = computed
        receipts.append(
            {
                "role": role,
                "name": profile["profileName"],
                "activeLanes": profile["activeLanes"],
                "effectiveIdentity": profile["effectiveProfile"],
                "observationDigest": profile["observationDigest"],
                "modelIds": [row["id"] for row in (profile.get("models") or [])],
                "hardGatesPassed": profile["hardGatesPassed"],
                "nonInferiorToLexical": profile["nonInferiorToLexical"],
                "metrics": metrics,
                "metricsBySplit": split_metrics,
            }
        )
        computed_by_role[role] = metrics
    return receipts, computed_by_role


def _metric_evidence_digest(
    metrics: dict[str, dict[str, int | float]], roles: tuple[str, ...]
) -> str:
    evidence = {
        role: {
            key: value
            for key, value in metrics[role].items()
            if key not in {"p50LatencyMillis", "p95LatencyMillis"}
        }
        for role in roles
    }
    return _identity(_go_json_bytes(evidence))


def build_report(
    *,
    raw_path: Path,
    projection_manifest_path: Path,
    final_eval_root: Path,
    projected_eval_root: Path,
    profile_catalog_path: Path,
    model_manifest_path: Path,
    harness_receipt_path: Path,
    historical_evidence_archive_path: Path,
    historical_evidence_receipt_path: str,
    production_profile_path: Path,
    schema_path: Path,
    validator_path: Path,
    runtime_source_revision: str,
    historical_runtime_source_revision: str,
) -> dict[str, Any]:
    """Build schema-v2 evidence without combining cross-runtime arms."""

    for field, revision in (
        ("runtimeSourceRevision", runtime_source_revision),
        ("historicalRuntimeSourceRevision", historical_runtime_source_revision),
    ):
        if not REVISION_RE.fullmatch(revision):
            _fail(field, "must be a full lowercase Git revision")

    def validate_raw(
        value: dict[str, Any],
        *,
        label: str,
        arm_count: int,
    ) -> dict[str, Any]:
        if value.get("schemaVersion") != 1 or value.get("suite") != "project-benchmark-v1":
            _fail(label, "must be a project-benchmark-v1 schemaVersion 1 report")
        if value.get("mode") != "full" or value.get("complete") is not True:
            _fail(label, "must be a complete full run")
        if value.get("unsupportedProfiles") not in (None, []):
            _fail(f"{label}.unsupportedProfiles", "must be empty")
        if not isinstance(value.get("durationMillis"), int) or value["durationMillis"] < 0:
            _fail(f"{label}.durationMillis", "must be a non-negative integer")
        generation_value = value.get("generation")
        if not isinstance(generation_value, dict):
            _fail(f"{label}.generation", "must be an object")
        required = (
            "id",
            "corpusFingerprint",
            "gitRevision",
            "dirtyFingerprint",
            "parserVersion",
            "chunkerVersion",
            "documentCount",
            "chunkCount",
            "runtime",
        )
        missing = [field for field in required if field not in generation_value]
        if missing:
            _fail(f"{label}.generation", f"is missing fields {missing!r}")
        if not re.fullmatch(r"generation-[0-9a-f]{20}", str(generation_value["id"])):
            _fail(f"{label}.generation.id", "must be a canonical generation ID")
        if not REVISION_RE.fullmatch(str(generation_value["gitRevision"])):
            _fail(f"{label}.generation.gitRevision", "must be a full revision")
        for field in ("corpusFingerprint", "dirtyFingerprint"):
            if not IDENTITY_RE.fullmatch(str(generation_value[field])):
                _fail(f"{label}.generation.{field}", "must be a digest")
        for field in ("parserVersion", "chunkerVersion"):
            if not isinstance(generation_value[field], str) or not generation_value[field]:
                _fail(f"{label}.generation.{field}", "must be non-empty")
        for field in ("documentCount", "chunkCount"):
            if not isinstance(generation_value[field], int) or generation_value[field] < 1:
                _fail(f"{label}.generation.{field}", "must be positive")
        if not isinstance(generation_value["runtime"], dict):
            _fail(f"{label}.generation.runtime", "must be an object")
        profiles_value = value.get("profiles")
        if not isinstance(profiles_value, list) or len(profiles_value) != arm_count:
            _fail(f"{label}.profiles", f"must contain exactly {arm_count} arms")
        return generation_value

    raw = _read_json(raw_path)
    projection = _read_json(projection_manifest_path)
    profile_catalog = _read_json(profile_catalog_path)
    model_manifest = _read_json(model_manifest_path)
    harness_receipt = _read_json(harness_receipt_path)
    production_profile = _read_json(production_profile_path)
    schema = _read_json(schema_path)
    generation = validate_raw(raw, label="raw", arm_count=2)

    transform_digest = _manifest_transform_digest(projection)
    final_evals, _projected_evals, eval_rows, eval_fingerprint = _load_eval_projection(
        final_eval_root, projected_eval_root, projection
    )
    if raw.get("evalFingerprint") != eval_fingerprint:
        _fail(
            "raw.evalFingerprint",
            f"is {raw.get('evalFingerprint')}, projected evals recompute to {eval_fingerprint}",
        )
    eval_by_id = {case["id"]: case for case in final_evals}

    (
        historical_raw,
        historical_projection,
        historical_profile_catalog,
        historical_model_manifest,
        historical_evals,
        historical_eval_fingerprint,
        historical_entries,
        historical_archive_body,
    ) = _load_historical_archive(historical_evidence_archive_path)
    historical_generation = validate_raw(
        historical_raw, label="historical.raw", arm_count=3
    )
    if historical_raw.get("evalFingerprint") != historical_eval_fingerprint:
        _fail(
            "historical.raw.evalFingerprint",
            "does not equal the fingerprint replayed from the archived evals",
        )
    historical_eval_by_id = {case["id"]: case for case in historical_evals}
    historical_projection_digest = _manifest_transform_digest(historical_projection)

    profiles = _profiles_by_role(raw, CURRENT_PROFILES)
    _validate_profile_catalog(profile_catalog, profiles, CURRENT_PROFILES)
    main_maps = {
        role: _case_map(
            profile.get("cases"), f"raw.profiles[{role}].cases", eval_by_id
        )
        for role, profile in profiles.items()
    }
    budget_maps = _budget_case_maps(profiles, eval_by_id)
    profile_rows, current_metrics = _profile_receipts(
        profiles, main_maps, eval_by_id, ("baseline", "dense")
    )

    historical_profiles = _profiles_by_role(historical_raw, HISTORICAL_PROFILES)
    _validate_profile_catalog(
        historical_profile_catalog, historical_profiles, HISTORICAL_PROFILES
    )
    historical_main_maps = {
        role: _case_map(
            profile.get("cases"),
            f"historical.raw.profiles[{role}].cases",
            historical_eval_by_id,
        )
        for role, profile in historical_profiles.items()
    }
    historical_budget_maps = _budget_case_maps(
        historical_profiles, historical_eval_by_id
    )
    historical_profile_rows, historical_metrics = _profile_receipts(
        historical_profiles,
        historical_main_maps,
        historical_eval_by_id,
        ("baseline", "dense", "rerank"),
    )

    cases: list[dict[str, Any]] = []
    for case_id in sorted(eval_by_id):
        eval_case = eval_by_id[case_id]
        baseline = main_maps["baseline"][case_id]
        dense = main_maps["dense"][case_id]
        cases.append(
            {
                "caseId": case_id,
                "split": eval_case["split"],
                "role": eval_case["role"],
                "topic": eval_case["topic"],
                "queryClass": eval_case["queryClass"],
                "vocabularyPolicy": eval_case.get("vocabularyPolicy") or "none",
                "answerable": bool(eval_case["answerable"]),
                "baseline": baseline,
                "dense": dense,
                "denseComparison": _comparison(baseline, dense),
            }
        )

    historical_cases: list[dict[str, Any]] = []
    for case_id in sorted(historical_eval_by_id):
        eval_case = historical_eval_by_id[case_id]
        dense = historical_main_maps["dense"][case_id]
        rerank = historical_main_maps["rerank"][case_id]
        historical_cases.append(
            {
                "caseId": case_id,
                "split": eval_case["split"],
                "topic": eval_case["topic"],
                "answerable": bool(eval_case["answerable"]),
                "dense": dense,
                "rerank": rerank,
                "rerankComparison": _comparison(dense, rerank),
            }
        )

    budget_slices, budget_case_comparisons = _budget_slices(budget_maps)
    (
        historical_budget_slices,
        historical_budget_case_comparisons,
    ) = _historical_rerank_budget_slices(historical_budget_maps)

    answerable = [case for case in cases if case["answerable"]]
    leave_one_out: list[dict[str, Any]] = []
    for topic in sorted({case["topic"] for case in cases}):
        remaining = [case for case in answerable if case["topic"] != topic]
        if not remaining:
            _fail("sensitivity.leaveOneTopicOut", f"omitting {topic!r} leaves no cases")
        delta = (
            sum(int(case["dense"]["expectedFound"]) for case in remaining)
            - sum(int(case["baseline"]["expectedFound"]) for case in remaining)
        ) / len(remaining)
        leave_one_out.append(
            {
                "omittedTopic": topic,
                "answerableCases": len(remaining),
                "denseHitRateDelta": delta,
            }
        )

    dense_counts = _event_counts(cases, "denseComparison")
    historical_dense_cases = [
        {
            "denseComparison": _comparison(
                historical_main_maps["baseline"][case_id],
                historical_main_maps["dense"][case_id],
            )
        }
        for case_id in sorted(historical_eval_by_id)
    ]
    preliminary_dense_counts = _event_counts(
        historical_dense_cases, "denseComparison"
    )
    historical_rerank_counts = _event_counts(
        historical_cases, "rerankComparison"
    )
    final_metrics_digest = _metric_evidence_digest(
        current_metrics, ("baseline", "dense")
    )
    preliminary_metrics_digest = _metric_evidence_digest(
        historical_metrics, ("baseline", "dense")
    )

    shared_failures, dense_regressions, unexpected_current_rerank = _safety_regressions(
        profiles, main_maps, budget_maps, eval_by_id
    )
    if unexpected_current_rerank != 0:
        _fail("raw.safety", "current two-arm run unexpectedly reported rerank regressions")
    (
        historical_shared_failures,
        _historical_dense_regressions,
        historical_rerank_regressions,
    ) = _safety_regressions(
        historical_profiles,
        historical_main_maps,
        historical_budget_maps,
        historical_eval_by_id,
    )

    dense_ids = sorted(
        case["caseId"] for case in cases if case["denseComparison"]["uniqueRescue"]
    )
    historical_rerank_ids = sorted(
        case["caseId"]
        for case in historical_cases
        if case["rerankComparison"]["uniqueRescue"]
        or case["rerankComparison"]["rankImproved"]
    )

    def lane_decision(
        lane: str,
        ids: list[str],
        regressions: int,
        losses: int,
        evidence_label: str,
    ) -> dict[str, Any]:
        if ids and (regressions or losses):
            action = "inconclusive"
        elif ids:
            action = "retain"
        else:
            action = "remove"
        return {
            "action": action,
            "positiveEvidence": bool(ids),
            "eventCount": len(ids),
            "caseIds": ids,
            "rationale": (
                f"{lane} recorded {len(ids)} positive case(s), {losses} loss(es), "
                f"and {regressions} added safety regression(s) in {evidence_label}."
            ),
        }

    dense_decision = lane_decision(
        "dense",
        dense_ids,
        dense_regressions,
        dense_counts["losses"],
        "the fresh current-runtime two-arm matrix",
    )
    rerank_decision = lane_decision(
        "rerank",
        historical_rerank_ids,
        historical_rerank_regressions,
        historical_rerank_counts["losses"],
        "the frozen pre-removal three-arm matrix",
    )

    effective = production_profile.get("effectiveProfiles")
    if not isinstance(effective, list) or not effective:
        _fail("productionProfile.effectiveProfiles", "must contain a selected first row")
    production_lanes = effective[0].get("lanes")
    if not isinstance(production_lanes, list) or len(set(production_lanes)) != len(
        production_lanes
    ):
        _fail(
            "productionProfile.effectiveProfiles[0].lanes",
            "must be a unique lane array",
        )
    consistent = (
        dense_decision["action"] != "inconclusive"
        and rerank_decision["action"] != "inconclusive"
        and ((dense_decision["action"] == "retain") == ("dense" in production_lanes))
        and ((rerank_decision["action"] == "retain") == ("rerank" in production_lanes))
    )
    release_gate = (
        consistent
        and dense_regressions == 0
        and historical_rerank_regressions == 0
    )

    project, indexed_proof, harness = _harness(
        harness_receipt,
        receipt_path=harness_receipt_path,
        schema_path=schema_path,
        raw=raw,
        generation=generation,
        raw_path=raw_path,
        projection_manifest_path=projection_manifest_path,
        final_eval_root=final_eval_root,
        projected_eval_root=projected_eval_root,
        profile_catalog_path=profile_catalog_path,
        model_manifest_path=model_manifest_path,
        runtime_source_revision=runtime_source_revision,
    )
    models = _model_rows(profiles, model_manifest, include_reranker=False)
    historical_models = _model_rows(
        historical_profiles, historical_model_manifest, include_reranker=True
    )
    raw_runtime_fingerprint = _identity(_go_json_bytes(generation["runtime"]))
    historical_runtime_fingerprint = _identity(
        _go_json_bytes(historical_generation["runtime"])
    )
    evaluated_at = _run_time(raw["runId"])

    report = {
        "$schema": "plugin://re-discipline/schemas/project-lane-ablation-report.schema.json",
        "schemaVersion": 2,
        "kind": "project-retrieval-lane-ablation",
        "measurementOnly": True,
        "project": project,
        "evaluatedAt": evaluated_at,
        "provenance": {
            "frozenSourceRevision": runtime_source_revision,
            "projectGitRevision": generation["gitRevision"],
            "projectDirtyFingerprint": generation["dirtyFingerprint"],
            "frozenRuntimeFingerprint": raw_runtime_fingerprint,
            "rawBenchmarkSha256": _file_identity(raw_path),
            "rawBenchmarkRunId": raw["runId"],
            "rawBenchmarkComplete": True,
            "rawBenchmarkArmCount": 2,
            "rawBenchmarkCasesPerArm": 64,
            "rawBenchmarkDurationMillis": raw["durationMillis"],
            "parserVersion": generation["parserVersion"],
            "chunkerVersion": generation["chunkerVersion"],
            "profileCatalogSha256": _file_identity(profile_catalog_path),
            "modelManifestSha256": _file_identity(model_manifest_path),
        },
        "corpus": {
            "generationId": generation["id"],
            "corpusFingerprint": generation["corpusFingerprint"],
            "documentCount": generation["documentCount"],
            "chunkCount": generation["chunkCount"],
            "indexedSourceProof": indexed_proof,
            "finalEvalFileCount": len(eval_rows),
            "finalEvalCaseCount": len(final_evals),
            "finalEvalFiles": eval_rows,
            "projectedEvalFingerprint": eval_fingerprint,
            "projectionManifestSha256": _file_identity(projection_manifest_path),
            "projectionTransformDigest": transform_digest,
            "projectionOperation": projection["transform"],
            "projectionRemovedOccurrences": projection["removedOccurrences"],
            "projectionPreservesAllOtherBytes": True,
        },
        "harness": harness,
        "models": models,
        "profiles": profile_rows,
        "cases": cases,
        "slices": _all_slices(cases),
        "uncertainty": _uncertainty(cases),
        "sensitivity": {
            "budgetSlices": budget_slices,
            "budgetCaseComparisons": budget_case_comparisons,
            "leaveOneTopicOut": leave_one_out,
            "densePathChangedCases": sum(
                int(case["denseComparison"]["pathsChanged"]) for case in cases
            ),
            "preliminaryComparison": {
                "available": True,
                "preliminaryCorpusFingerprint": historical_generation[
                    "corpusFingerprint"
                ],
                "finalCorpusFingerprint": generation["corpusFingerprint"],
                "preliminaryDenseRescues": preliminary_dense_counts["rescues"],
                "preliminaryMetricsSha256": preliminary_metrics_digest,
                "finalMetricsSha256": final_metrics_digest,
                "metricsChanged": final_metrics_digest != preliminary_metrics_digest,
                "denseRescueDelta": (
                    dense_counts["rescues"] - preliminary_dense_counts["rescues"]
                ),
            },
        },
        "historicalRerank": {
            "provenance": {
                "runtimeSourceRevision": historical_runtime_source_revision,
                "projectGitRevision": historical_generation["gitRevision"],
                "projectDirtyFingerprint": historical_generation["dirtyFingerprint"],
                "runtimeFingerprint": historical_runtime_fingerprint,
                "rawBenchmarkSha256": _identity(
                    historical_entries["raw-benchmark.json"]
                ),
                "evidenceArchivePath": historical_evidence_receipt_path,
                "evidenceArchiveSha256": _identity(historical_archive_body),
                "rawBenchmarkByteCount": len(
                    historical_entries["raw-benchmark.json"]
                ),
                "evidenceArchiveByteCount": len(historical_archive_body),
                "evidenceArchiveFormat": "zip-deflate-fixed-v1",
                "rawBenchmarkRunId": historical_raw["runId"],
                "rawBenchmarkComplete": True,
                "rawBenchmarkArmCount": 3,
                "rawBenchmarkCasesPerArm": 64,
                "rawBenchmarkDurationMillis": historical_raw["durationMillis"],
                "generationId": historical_generation["id"],
                "corpusFingerprint": historical_generation["corpusFingerprint"],
                "evalFingerprint": historical_eval_fingerprint,
                "parserVersion": historical_generation["parserVersion"],
                "chunkerVersion": historical_generation["chunkerVersion"],
                "profileCatalogSha256": _identity(
                    historical_entries["profile-catalog.json"]
                ),
                "modelManifestSha256": _identity(
                    historical_entries["model-manifest.json"]
                ),
                "projectionManifestSha256": _identity(
                    historical_entries["projection-manifest.json"]
                ),
                "projectionTransformDigest": historical_projection_digest,
            },
            "models": historical_models,
            "profiles": historical_profile_rows,
            "cases": historical_cases,
            "budgetSlices": historical_budget_slices,
            "budgetCaseComparisons": historical_budget_case_comparisons,
            "sharedSafetyFailures": historical_shared_failures,
            "rerankAddedSafetyRegressions": historical_rerank_regressions,
            "decision": rerank_decision,
        },
        "validation": {
            "schemaSha256": _file_identity(schema_path),
            "validatorSha256": _file_identity(validator_path),
            "generatorSha256": _file_identity(Path(__file__).resolve()),
            "command": (
                "python tests/re_discipline_project_lane_ablation_build.py "
                "<frozen-current-inputs> <frozen-historical-archive> "
                "--output <measurement> --verify"
            ),
            "validatedAt": evaluated_at,
            "passed": True,
        },
        "decision": {
            "dense": dense_decision,
            "rerank": rerank_decision,
            "productionLanes": production_lanes,
            "sharedSafetyFailures": shared_failures,
            "denseAddedSafetyRegressions": dense_regressions,
            "productionProfileConsistent": consistent,
            "releaseGatePassed": release_gate,
            "rationale": (
                "Dense is decided only from the fresh current-runtime two-arm matrix; "
                "rerank is decided only from the separately frozen pre-removal "
                "three-arm matrix. No cross-runtime arms are spliced."
            ),
        },
    }
    validate_project_lane_ablation_report(schema, report)
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--raw-benchmark", type=Path, required=True)
    parser.add_argument("--projection-manifest", type=Path, required=True)
    parser.add_argument("--final-eval-root", type=Path, required=True)
    parser.add_argument("--projected-eval-root", type=Path, required=True)
    parser.add_argument("--profile-catalog", type=Path, required=True)
    parser.add_argument("--model-manifest", type=Path, required=True)
    parser.add_argument("--harness-receipt", type=Path, required=True)
    parser.add_argument("--historical-evidence-archive", type=Path, required=True)
    parser.add_argument("--historical-evidence-path", required=True)
    parser.add_argument("--production-profile", type=Path, required=True)
    parser.add_argument("--schema", type=Path, required=True)
    parser.add_argument("--validator", type=Path, required=True)
    parser.add_argument("--runtime-source-revision", required=True)
    parser.add_argument("--historical-runtime-source-revision", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--verify",
        action="store_true",
        help="byte-compare the deterministic rebuild with an existing output",
    )
    args = parser.parse_args()
    report = build_report(
        raw_path=args.raw_benchmark,
        projection_manifest_path=args.projection_manifest,
        final_eval_root=args.final_eval_root,
        projected_eval_root=args.projected_eval_root,
        profile_catalog_path=args.profile_catalog,
        model_manifest_path=args.model_manifest,
        harness_receipt_path=args.harness_receipt,
        historical_evidence_archive_path=args.historical_evidence_archive,
        historical_evidence_receipt_path=args.historical_evidence_path,
        production_profile_path=args.production_profile,
        schema_path=args.schema,
        validator_path=args.validator,
        runtime_source_revision=args.runtime_source_revision,
        historical_runtime_source_revision=args.historical_runtime_source_revision,
    )
    body = _stable_output_bytes(report)
    if args.verify:
        if not args.output.is_file() or args.output.read_bytes() != body:
            raise MeasurementBuildError(
                f"{args.output}: existing receipt is not the byte-identical deterministic rebuild"
            )
        print(f"verified byte-stable project lane-ablation receipt: {args.output}")
        return 0
    args.output.parent.mkdir(parents=True, exist_ok=True)
    temporary = args.output.with_name(args.output.name + ".tmp")
    temporary.write_bytes(body)
    temporary.replace(args.output)
    print(f"wrote deterministic project lane-ablation receipt: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
