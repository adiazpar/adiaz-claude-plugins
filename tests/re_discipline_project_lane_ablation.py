"""Dependency-free validation for project lane-ablation measurements.

The release gate deliberately uses only the Python standard library so the
same command runs in a clean checkout and in every CI operating-system job.
It validates the packaged JSON Schema keywords used by the report and then
checks cross-field measurement invariants that JSON Schema cannot express.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import re
from datetime import datetime
from pathlib import Path
from typing import Any


class MeasurementValidationError(ValueError):
    """Raised when a lane-ablation report violates its schema or semantics."""


def _fail(path: str, message: str) -> None:
    raise MeasurementValidationError(f"{path}: {message}")


def _resolve_local_ref(root: dict[str, Any], reference: str) -> dict[str, Any]:
    if not reference.startswith("#/"):
        raise MeasurementValidationError(f"unsupported non-local schema reference {reference!r}")
    value: Any = root
    for raw in reference[2:].split("/"):
        token = raw.replace("~1", "/").replace("~0", "~")
        if not isinstance(value, dict) or token not in value:
            raise MeasurementValidationError(f"unresolved schema reference {reference!r}")
        value = value[token]
    if not isinstance(value, dict):
        raise MeasurementValidationError(f"schema reference {reference!r} is not an object")
    return value


def _matches_type(value: Any, expected: str) -> bool:
    if expected == "object":
        return isinstance(value, dict)
    if expected == "array":
        return isinstance(value, list)
    if expected == "string":
        return isinstance(value, str)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "number":
        return (
            isinstance(value, (int, float))
            and not isinstance(value, bool)
            and math.isfinite(value)
        )
    if expected == "null":
        return value is None
    raise MeasurementValidationError(f"validator does not implement JSON type {expected!r}")


def _validate_format(value: str, format_name: str, path: str) -> None:
    if format_name != "date-time":
        raise MeasurementValidationError(
            f"validator does not implement JSON format {format_name!r}"
        )
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        _fail(path, f"is not an RFC 3339 date-time: {error}")
    if parsed.tzinfo is None:
        _fail(path, "date-time lacks an explicit UTC offset")


def _canonical(value: Any) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def _ordered_identity(value: Any) -> str:
    body = json.dumps(value, sort_keys=False, separators=(",", ":"), ensure_ascii=False)
    body = (
        body.replace("&", "\\u0026")
        .replace("<", "\\u003c")
        .replace(">", "\\u003e")
        .replace("\u2028", "\\u2028")
        .replace("\u2029", "\\u2029")
    )
    return "sha256:" + hashlib.sha256(body.encode("utf-8")).hexdigest()


def validate_json_schema(
    schema: dict[str, Any], instance: Any, *, root: dict[str, Any] | None = None, path: str = "$"
) -> None:
    """Validate the JSON-Schema subset used by the packaged measurement schema."""

    root = schema if root is None else root
    if "$ref" in schema:
        validate_json_schema(_resolve_local_ref(root, schema["$ref"]), instance, root=root, path=path)

    expected_type = schema.get("type")
    if expected_type is not None and not _matches_type(instance, expected_type):
        _fail(path, f"expected {expected_type}, got {type(instance).__name__}")
    if "const" in schema and instance != schema["const"]:
        _fail(path, f"must equal {schema['const']!r}")
    if "enum" in schema and instance not in schema["enum"]:
        _fail(path, f"must be one of {schema['enum']!r}")

    if isinstance(instance, dict):
        required = schema.get("required", [])
        missing = [name for name in required if name not in instance]
        if missing:
            _fail(path, f"missing required properties {missing!r}")
        properties = schema.get("properties", {})
        if schema.get("additionalProperties") is False:
            extras = sorted(set(instance) - set(properties))
            if extras:
                _fail(path, f"unexpected properties {extras!r}")
        for name, child in properties.items():
            if name in instance:
                validate_json_schema(
                    child, instance[name], root=root, path=f"{path}.{name}"
                )

    if isinstance(instance, list):
        if len(instance) < schema.get("minItems", 0):
            _fail(path, f"has {len(instance)} items, below minItems")
        if "maxItems" in schema and len(instance) > schema["maxItems"]:
            _fail(path, f"has {len(instance)} items, above maxItems")
        if schema.get("uniqueItems"):
            encoded = [_canonical(value) for value in instance]
            if len(set(encoded)) != len(encoded):
                _fail(path, "contains duplicate items")
        child = schema.get("items")
        if child is not None:
            for index, value in enumerate(instance):
                validate_json_schema(child, value, root=root, path=f"{path}[{index}]")

    if isinstance(instance, str):
        if len(instance) < schema.get("minLength", 0):
            _fail(path, "is shorter than minLength")
        if "maxLength" in schema and len(instance) > schema["maxLength"]:
            _fail(path, "is longer than maxLength")
        if "pattern" in schema and re.search(schema["pattern"], instance) is None:
            _fail(path, f"does not match pattern {schema['pattern']!r}")
        if "format" in schema:
            _validate_format(instance, schema["format"], path)

    if isinstance(instance, (int, float)) and not isinstance(instance, bool):
        if not math.isfinite(instance):
            _fail(path, "must be finite")
        if "minimum" in schema and instance < schema["minimum"]:
            _fail(path, f"is below minimum {schema['minimum']}")
        if "maximum" in schema and instance > schema["maximum"]:
            _fail(path, f"is above maximum {schema['maximum']}")


def _roles(values: list[dict[str, Any]], path: str, expected: set[str]) -> dict[str, Any]:
    by_role: dict[str, Any] = {}
    for value in values:
        role = value["role"]
        if role in by_role:
            _fail(path, f"role {role!r} is repeated")
        by_role[role] = value
    if set(by_role) != expected:
        _fail(path, f"roles are {sorted(by_role)!r}, want {sorted(expected)!r}")
    return by_role


def _rank(outcome: dict[str, Any]) -> int:
    ranks = outcome["relevantRanks"]
    return min(ranks) if ranks else 0


def _expected_comparison(
    before: dict[str, Any], after: dict[str, Any]
) -> dict[str, Any]:
    before_hit = before["expectedFound"]
    after_hit = after["expectedFound"]
    if not before_hit and after_hit:
        hit_effect = "rescue"
    elif before_hit and not after_hit:
        hit_effect = "loss"
    else:
        hit_effect = "unchanged"
    before_rank = _rank(before)
    after_rank = _rank(after)
    both_hit = before_hit and after_hit
    return {
        "hitEffect": hit_effect,
        # Positive means the later arm promoted relevant evidence. A rescue or
        # loss has no comparable pairwise rank and therefore records zero.
        "rankDelta": before_rank - after_rank if both_hit else 0,
        "pathsChanged": before["paths"] != after["paths"],
        "uniqueRescue": hit_effect == "rescue",
        "rankImproved": both_hit and after_rank < before_rank,
        "rankDegraded": both_hit and after_rank > before_rank,
    }


def _event_counts(cases: list[dict[str, Any]], key: str) -> dict[str, int]:
    comparisons = [case[key] for case in cases]
    return {
        "rescues": sum(value["uniqueRescue"] for value in comparisons),
        "losses": sum(value["hitEffect"] == "loss" for value in comparisons),
        "rankImprovements": sum(value["rankImproved"] for value in comparisons),
        "rankDegradations": sum(value["rankDegraded"] for value in comparisons),
    }


def _validate_outcome(
    outcome: dict[str, Any], case: dict[str, Any], path: str
) -> None:
    parallel = [
        len(outcome[field])
        for field in ("paths", "tiers", "chunkIds", "contentHashes")
    ]
    if len(set(parallel)) != 1:
        _fail(path, "paths, tiers, chunkIds, and contentHashes lengths differ")
    if len(outcome["relevantPaths"]) != len(outcome["relevantRanks"]):
        _fail(path, "relevantPaths and relevantRanks lengths differ")
    if outcome["returnedUniquePaths"] != len(set(outcome["paths"])):
        _fail(path + ".returnedUniquePaths", "does not equal the unique path count")
    if not set(outcome["relevantPaths"]).issubset(set(outcome["paths"])):
        _fail(path + ".relevantPaths", "contains a path absent from returned paths")
    if any(rank > len(outcome["paths"]) for rank in outcome["relevantRanks"]):
        _fail(path + ".relevantRanks", "contains a rank beyond the result set")
    if case["answerable"]:
        expected_found = bool(outcome["relevantRanks"])
    else:
        expected_found = outcome["abstentionCorrect"]
    if outcome["expectedFound"] != expected_found:
        _fail(path + ".expectedFound", "does not match answerability and returned evidence")
    safety = (
        outcome["authoritySafe"]
        and outcome["citationMetadataSafe"]
        and outcome["corpusMatched"]
        and outcome["budgetSafe"]
        and outcome["replayIdentical"]
        and outcome["staleResults"] == 0
    )
    quality = (not outcome["qualityGateApplicable"]) or (
        outcome["expectedFound"]
        and outcome["completeEvidence"]
        and outcome["citationSafe"]
        and outcome["abstentionCorrect"]
        and not outcome["hardNegativeHits"]
    )
    if outcome["safetyPassed"] != safety:
        _fail(path + ".safetyPassed", "does not match the safety predicates")
    if outcome["qualityPassed"] != quality:
        _fail(path + ".qualityPassed", "does not match the quality predicates")
    if outcome["gatePassed"] != (safety and quality):
        _fail(path + ".gatePassed", "does not equal safetyPassed && qualityPassed")


def _slice_row(name: str, cases: list[dict[str, Any]]) -> dict[str, Any]:
    dense = _event_counts(cases, "denseComparison")
    return {
        "name": name,
        "caseCount": len(cases),
        "denseRescues": dense["rescues"],
        "denseLosses": dense["losses"],
        "denseRankImprovements": dense["rankImprovements"],
        "denseRankDegradations": dense["rankDegradations"],
    }


def _expected_slices(cases: list[dict[str, Any]]) -> list[dict[str, Any]]:
    selectors = [
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
    return [
        _slice_row(name, [case for case in cases if selector(case)])
        for name, selector in selectors
    ]


def _wilson_expected(
    slice_name: str, event: str, successes: int, trials: int
) -> dict[str, Any]:
    if trials < 1:
        _fail("$.uncertainty.wilsonIntervals", f"slice {slice_name!r} is empty")
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


def _expected_wilson(cases: list[dict[str, Any]]) -> list[dict[str, Any]]:
    dense_slices = [
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
    ]
    return [
        _wilson_expected(
            name,
            "dense-unique-rescue",
            sum(int(case["denseComparison"]["uniqueRescue"]) for case in selected),
            len(selected),
        )
        for name, selected in dense_slices
    ]


def _dense_hit_delta(cases: list[dict[str, Any]]) -> float:
    if not cases:
        return 0.0
    return (
        sum(int(case["dense"]["expectedFound"]) for case in cases)
        - sum(int(case["baseline"]["expectedFound"]) for case in cases)
    ) / len(cases)


def _expected_bootstrap(
    cases: list[dict[str, Any]], replicates: int, seed: int
) -> dict[str, Any]:
    answerable = [case for case in cases if case["answerable"]]
    topics = sorted({case["topic"] for case in answerable})
    if len(topics) < 2:
        _fail("$.uncertainty.topicClusterBootstrap", "needs at least two topics")
    grouped = {
        topic: [case for case in answerable if case["topic"] == topic]
        for topic in topics
    }
    values: list[float] = []
    for replicate in range(replicates):
        sampled: list[dict[str, Any]] = []
        for draw in range(len(topics)):
            digest = hashlib.sha256(
                f"{seed}\0{replicate}\0{draw}".encode("ascii")
            ).digest()
            topic = topics[int.from_bytes(digest[:8], "big") % len(topics)]
            sampled.extend(grouped[topic])
        values.append(_dense_hit_delta(sampled))
    ordered = sorted(values)

    def quantile(value: float) -> float:
        return ordered[max(0, math.ceil(len(ordered) * value) - 1)]

    return {
        "method": "topic-cluster-percentile-v1",
        "clusterKey": "topic",
        "clusterCount": len(topics),
        "replicates": replicates,
        "seed": seed,
        "estimand": "answerable dense-minus-baseline expected-hit-rate delta",
        "pointEstimate": _dense_hit_delta(answerable),
        "lower": quantile(0.025),
        "median": quantile(0.5),
        "upper": quantile(0.975),
        "zeroOrLowerFraction": sum(value <= 0 for value in values) / len(values),
    }


def _numbers_close(actual: Any, expected: Any) -> bool:
    if isinstance(expected, float):
        return isinstance(actual, (int, float)) and math.isclose(
            actual, expected, rel_tol=0, abs_tol=1e-15
        )
    return actual == expected


def _assert_derived(actual: Any, expected: Any, path: str) -> None:
    if isinstance(expected, dict):
        if not isinstance(actual, dict) or set(actual) != set(expected):
            _fail(path, "has a different derived field set")
        for field, value in expected.items():
            _assert_derived(actual[field], value, f"{path}.{field}")
        return
    if isinstance(expected, list):
        if not isinstance(actual, list) or len(actual) != len(expected):
            _fail(path, "has a different derived row count")
        for index, value in enumerate(expected):
            _assert_derived(actual[index], value, f"{path}[{index}]")
        return
    if not _numbers_close(actual, expected):
        _fail(path, f"is {actual!r}, recomputed {expected!r}")


def validate_project_lane_ablation_semantics(report: dict[str, Any]) -> None:
    """Validate schema-v2 current and historical evidence as separate layers."""

    harness = report["harness"]
    provenance = report["provenance"]
    corpus = report["corpus"]
    if harness["pluginRevision"] != provenance["frozenSourceRevision"]:
        _fail("$.harness.pluginRevision", "does not match current runtime provenance")
    if harness["projectRevision"] != provenance["projectGitRevision"]:
        _fail("$.harness.projectRevision", "does not match project provenance")
    if (
        harness["projectionManifestArtifactSha256"]
        != corpus["projectionManifestSha256"]
    ):
        _fail(
            "$.harness.projectionManifestArtifactSha256",
            "does not match the measured projection artifact",
        )

    current_profile_order = [profile["role"] for profile in report["profiles"]]
    if current_profile_order != ["baseline", "dense"]:
        _fail("$.profiles", "must be ordered baseline, dense")
    profiles = _roles(report["profiles"], "$.profiles", {"baseline", "dense"})
    expected_current_profiles = {
        "baseline": ("lexical-graph-v1", ["exact", "fts", "graph"]),
        "dense": (
            "hybrid-no-rerank-v1",
            ["exact", "fts", "graph", "dense"],
        ),
    }
    for role, (name, lanes) in expected_current_profiles.items():
        if profiles[role]["name"] != name or profiles[role]["activeLanes"] != lanes:
            _fail(f"$.profiles[{role}]", "does not identify the current controlled arm")

    if [model["role"] for model in report["models"]] != ["embedding"]:
        _fail("$.models", "current runtime must declare only the embedding model")
    current_models = _roles(report["models"], "$.models", {"embedding"})
    embedding_id = current_models["embedding"]["id"]
    if profiles["baseline"]["modelIds"] != []:
        _fail("$.profiles[baseline].modelIds", "baseline must be model-free")
    if profiles["dense"]["modelIds"] != [embedding_id]:
        _fail("$.profiles[dense].modelIds", "must contain only the embedding model")

    cases = report["cases"]
    case_ids = [case["caseId"] for case in cases]
    if case_ids != sorted(case_ids) or len(set(case_ids)) != len(case_ids):
        _fail("$.cases", "must contain unique case IDs in sorted order")
    for index, case in enumerate(cases):
        base_path = f"$.cases[{index}]"
        for arm in ("baseline", "dense"):
            outcome = case[arm]
            for field in ("caseId", "split", "topic"):
                if outcome[field] != case[field]:
                    _fail(f"{base_path}.{arm}.{field}", "does not match case metadata")
            _validate_outcome(outcome, case, f"{base_path}.{arm}")
        if case["denseComparison"] != _expected_comparison(
            case["baseline"], case["dense"]
        ):
            _fail(f"{base_path}.denseComparison", "does not match arm outcomes")

    _assert_derived(report["slices"], _expected_slices(cases), "$.slices")
    dense_counts = _event_counts(cases, "denseComparison")

    eval_files = report["corpus"]["finalEvalFiles"]
    eval_paths = [value["path"] for value in eval_files]
    if len(set(eval_paths)) != len(eval_paths):
        _fail("$.corpus.finalEvalFiles", "paths must be unique")
    if sum(value["caseCount"] for value in eval_files) != 64:
        _fail("$.corpus.finalEvalFiles", "caseCount values do not total 64")
    if sum(value["removedOccurrences"] for value in eval_files) != report["corpus"][
        "projectionRemovedOccurrences"
    ]:
        _fail("$.corpus.finalEvalFiles", "removal total does not match projection")
    source_proof = report["corpus"]["indexedSourceProof"]
    if source_proof["byteExactCount"] != source_proof["sourceCount"]:
        _fail("$.corpus.indexedSourceProof", "not every indexed source is byte-exact")

    substitutions = harness["controlPlaneSubstitutions"]
    kinds = [value["kind"] for value in substitutions]
    if sorted(kinds) != ["bootstrap-config", "knowledge-policy", "retrieval-profile"]:
        _fail(
            "$.harness.controlPlaneSubstitutions",
            "must contain each kind exactly once",
        )
    rename_sources = [value["from"] for value in harness["renamedPaths"]]
    if len(set(rename_sources)) != len(rename_sources):
        _fail("$.harness.renamedPaths", "repeats a source path")

    budget_rows = report["sensitivity"]["budgetSlices"]
    if [value["tokenBudget"] for value in budget_rows] != [512, 1024, 2048, 4096]:
        _fail("$.sensitivity.budgetSlices", "has a non-canonical budget order")
    budget_evidence = report["sensitivity"]["budgetCaseComparisons"]
    if [value["tokenBudget"] for value in budget_evidence] != [
        512,
        1024,
        2048,
        4096,
    ]:
        _fail(
            "$.sensitivity.budgetCaseComparisons",
            "has a non-canonical budget order",
        )
    expected_budget_rows: list[dict[str, Any]] = []
    for index, evidence in enumerate(budget_evidence):
        evidence_ids = [row["caseId"] for row in evidence["cases"]]
        if evidence_ids != case_ids:
            _fail(
                f"$.sensitivity.budgetCaseComparisons[{index}].cases",
                "must contain the sorted current case IDs exactly once",
            )
        counts = _event_counts(evidence["cases"], "denseComparison")
        expected_budget_rows.append(
            {
                "tokenBudget": evidence["tokenBudget"],
                "denseRescues": counts["rescues"],
                "denseLosses": counts["losses"],
                "denseRankImprovements": counts["rankImprovements"],
                "denseRankDegradations": counts["rankDegradations"],
            }
        )
    _assert_derived(
        budget_rows, expected_budget_rows, "$.sensitivity.budgetSlices"
    )

    answerable = [case for case in cases if case["answerable"]]
    expected_leave_one_out: list[dict[str, Any]] = []
    for topic in sorted({case["topic"] for case in cases}):
        remaining = [case for case in answerable if case["topic"] != topic]
        if not remaining:
            _fail("$.sensitivity.leaveOneTopicOut", "a topic leaves no answerable cases")
        expected_leave_one_out.append(
            {
                "omittedTopic": topic,
                "answerableCases": len(remaining),
                "denseHitRateDelta": _dense_hit_delta(remaining),
            }
        )
    _assert_derived(
        report["sensitivity"]["leaveOneTopicOut"],
        expected_leave_one_out,
        "$.sensitivity.leaveOneTopicOut",
    )
    dense_changed = sum(case["denseComparison"]["pathsChanged"] for case in cases)
    if report["sensitivity"]["densePathChangedCases"] != dense_changed:
        _fail("$.sensitivity.densePathChangedCases", "does not match current cases")

    historical = report["historicalRerank"]
    historical_profile_order = [profile["role"] for profile in historical["profiles"]]
    if historical_profile_order != ["baseline", "dense", "rerank"]:
        _fail(
            "$.historicalRerank.profiles",
            "must be ordered baseline, dense, rerank",
        )
    historical_profiles = _roles(
        historical["profiles"],
        "$.historicalRerank.profiles",
        {"baseline", "dense", "rerank"},
    )
    expected_historical_profiles = {
        **expected_current_profiles,
        "rerank": (
            "hybrid-local-v1",
            ["exact", "fts", "graph", "dense", "rerank"],
        ),
    }
    for role, (name, lanes) in expected_historical_profiles.items():
        profile = historical_profiles[role]
        if profile["name"] != name or profile["activeLanes"] != lanes:
            _fail(
                f"$.historicalRerank.profiles[{role}]",
                "does not identify the frozen controlled arm",
            )

    if [model["role"] for model in historical["models"]] != [
        "embedding",
        "reranker",
    ]:
        _fail(
            "$.historicalRerank.models",
            "must be ordered embedding, reranker",
        )
    historical_models = _roles(
        historical["models"],
        "$.historicalRerank.models",
        {"embedding", "reranker"},
    )
    historical_embedding_id = historical_models["embedding"]["id"]
    historical_reranker_id = historical_models["reranker"]["id"]
    historical_expected_model_ids = {
        "baseline": [],
        "dense": [historical_embedding_id],
        "rerank": [historical_embedding_id, historical_reranker_id],
    }
    for role, ids in historical_expected_model_ids.items():
        if historical_profiles[role]["modelIds"] != ids:
            _fail(
                f"$.historicalRerank.profiles[{role}].modelIds",
                f"must equal {ids!r}",
            )

    historical_cases = historical["cases"]
    historical_case_ids = [case["caseId"] for case in historical_cases]
    if historical_case_ids != sorted(historical_case_ids) or len(
        set(historical_case_ids)
    ) != len(historical_case_ids):
        _fail(
            "$.historicalRerank.cases",
            "must contain unique case IDs in sorted order",
        )
    for index, case in enumerate(historical_cases):
        base_path = f"$.historicalRerank.cases[{index}]"
        for arm in ("dense", "rerank"):
            outcome = case[arm]
            for field in ("caseId", "split", "topic"):
                if outcome[field] != case[field]:
                    _fail(f"{base_path}.{arm}.{field}", "does not match case metadata")
            _validate_outcome(outcome, case, f"{base_path}.{arm}")
        if case["rerankComparison"] != _expected_comparison(
            case["dense"], case["rerank"]
        ):
            _fail(f"{base_path}.rerankComparison", "does not match arm outcomes")
    historical_rerank_counts = _event_counts(historical_cases, "rerankComparison")

    historical_budget_rows = historical["budgetSlices"]
    historical_budget_evidence = historical["budgetCaseComparisons"]
    if [row["tokenBudget"] for row in historical_budget_rows] != [
        512,
        1024,
        2048,
        4096,
    ] or [row["tokenBudget"] for row in historical_budget_evidence] != [
        512,
        1024,
        2048,
        4096,
    ]:
        _fail("$.historicalRerank.budgetSlices", "has a non-canonical budget order")
    expected_historical_budget_rows: list[dict[str, Any]] = []
    for index, evidence in enumerate(historical_budget_evidence):
        evidence_ids = [row["caseId"] for row in evidence["cases"]]
        if evidence_ids != historical_case_ids:
            _fail(
                f"$.historicalRerank.budgetCaseComparisons[{index}].cases",
                "must contain the sorted historical case IDs exactly once",
            )
        counts = _event_counts(evidence["cases"], "rerankComparison")
        expected_historical_budget_rows.append(
            {
                "tokenBudget": evidence["tokenBudget"],
                "rerankRescues": counts["rescues"],
                "rerankLosses": counts["losses"],
                "rerankRankImprovements": counts["rankImprovements"],
                "rerankRankDegradations": counts["rankDegradations"],
            }
        )
    _assert_derived(
        historical_budget_rows,
        expected_historical_budget_rows,
        "$.historicalRerank.budgetSlices",
    )

    preliminary = report["sensitivity"]["preliminaryComparison"]
    if preliminary["finalCorpusFingerprint"] != report["corpus"]["corpusFingerprint"]:
        _fail(
            "$.sensitivity.preliminaryComparison.finalCorpusFingerprint",
            "does not match the fresh measured corpus",
        )
    if preliminary["preliminaryCorpusFingerprint"] != historical["provenance"][
        "corpusFingerprint"
    ]:
        _fail(
            "$.sensitivity.preliminaryComparison.preliminaryCorpusFingerprint",
            "does not match frozen historical provenance",
        )
    if preliminary["denseRescueDelta"] != (
        dense_counts["rescues"] - preliminary["preliminaryDenseRescues"]
    ):
        _fail("$.sensitivity.preliminaryComparison.denseRescueDelta", "is not derived")
    current_metric_evidence = {
        role: {
            key: value
            for key, value in profiles[role]["metrics"].items()
            if key not in {"p50LatencyMillis", "p95LatencyMillis"}
        }
        for role in ("baseline", "dense")
    }
    historical_metric_evidence = {
        role: {
            key: value
            for key, value in historical_profiles[role]["metrics"].items()
            if key not in {"p50LatencyMillis", "p95LatencyMillis"}
        }
        for role in ("baseline", "dense")
    }
    current_metrics_digest = _ordered_identity(current_metric_evidence)
    historical_metrics_digest = _ordered_identity(historical_metric_evidence)
    if preliminary["finalMetricsSha256"] != current_metrics_digest:
        _fail(
            "$.sensitivity.preliminaryComparison.finalMetricsSha256",
            "does not bind current metrics",
        )
    if preliminary["preliminaryMetricsSha256"] != historical_metrics_digest:
        _fail(
            "$.sensitivity.preliminaryComparison.preliminaryMetricsSha256",
            "does not bind historical metrics",
        )
    if preliminary["metricsChanged"] != (
        historical_metrics_digest != current_metrics_digest
    ):
        _fail(
            "$.sensitivity.preliminaryComparison.metricsChanged",
            "does not match bound metric digests",
        )

    dense_ids = sorted(
        case["caseId"] for case in cases if case["denseComparison"]["uniqueRescue"]
    )
    rerank_ids = sorted(
        case["caseId"]
        for case in historical_cases
        if case["rerankComparison"]["uniqueRescue"]
        or case["rerankComparison"]["rankImproved"]
    )

    def validate_decision(
        decision: dict[str, Any],
        *,
        ids: list[str],
        losses: int,
        regressions: int,
        path: str,
    ) -> None:
        if decision["caseIds"] != ids or decision["eventCount"] != len(ids):
            _fail(path, "event evidence does not match per-case outcomes")
        if decision["positiveEvidence"] != bool(ids):
            _fail(path + ".positiveEvidence", "does not match event evidence")
        expected_action = (
            "inconclusive"
            if ids and (regressions or losses)
            else "retain"
            if ids
            else "remove"
        )
        if decision["action"] != expected_action:
            _fail(path + ".action", f"must equal derived action {expected_action!r}")

    validate_decision(
        report["decision"]["dense"],
        ids=dense_ids,
        losses=dense_counts["losses"],
        regressions=report["decision"]["denseAddedSafetyRegressions"],
        path="$.decision.dense",
    )
    validate_decision(
        historical["decision"],
        ids=rerank_ids,
        losses=historical_rerank_counts["losses"],
        regressions=historical["rerankAddedSafetyRegressions"],
        path="$.historicalRerank.decision",
    )
    if report["decision"]["rerank"] != historical["decision"]:
        _fail(
            "$.decision.rerank",
            "must be the exact decision derived in the frozen historical layer",
        )

    production_lane_list = report["decision"]["productionLanes"]
    if production_lane_list[:3] != ["exact", "fts", "graph"]:
        _fail("$.decision.productionLanes", "must begin with exact, fts, graph")
    expected_tail = [lane for lane in ("dense", "rerank") if lane in production_lane_list]
    if production_lane_list != ["exact", "fts", "graph", *expected_tail]:
        _fail("$.decision.productionLanes", "has a non-canonical lane order")
    production_lanes = set(production_lane_list)
    consistent = (
        report["decision"]["dense"]["action"] != "inconclusive"
        and report["decision"]["rerank"]["action"] != "inconclusive"
        and ((report["decision"]["dense"]["action"] == "retain") == ("dense" in production_lanes))
        and ((report["decision"]["rerank"]["action"] == "retain") == ("rerank" in production_lanes))
    )
    if report["decision"]["productionProfileConsistent"] != consistent:
        _fail(
            "$.decision.productionProfileConsistent",
            "does not match both evidence-layer decisions and production lanes",
        )
    release_gate = (
        consistent
        and report["decision"]["denseAddedSafetyRegressions"] == 0
        and historical["rerankAddedSafetyRegressions"] == 0
    )
    if report["decision"]["releaseGatePassed"] != release_gate:
        _fail(
            "$.decision.releaseGatePassed",
            "does not match consistency and both layers' safety regressions",
        )

    _assert_derived(
        report["uncertainty"]["wilsonIntervals"],
        _expected_wilson(cases),
        "$.uncertainty.wilsonIntervals",
    )
    bootstrap = report["uncertainty"]["topicClusterBootstrap"]
    if bootstrap["replicates"] != 10_000 or bootstrap["seed"] != 0x5A17_2026:
        _fail(
            "$.uncertainty.topicClusterBootstrap",
            "must use the frozen bootstrap parameters",
        )
    _assert_derived(
        bootstrap,
        _expected_bootstrap(cases, bootstrap["replicates"], bootstrap["seed"]),
        "$.uncertainty.topicClusterBootstrap",
    )


def validate_project_lane_ablation_report(
    schema: dict[str, Any], report: dict[str, Any]
) -> None:
    validate_json_schema(schema, report)
    validate_project_lane_ablation_semantics(report)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--schema", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    def strict_load(path: Path) -> dict[str, Any]:
        def reject_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
            value: dict[str, Any] = {}
            for key, item in pairs:
                if key in value:
                    raise MeasurementValidationError(
                        f"{path}: duplicate JSON key {key!r}"
                    )
                value[key] = item
            return value

        def reject_constant(value: str) -> None:
            raise MeasurementValidationError(
                f"{path}: non-finite JSON number {value!r}"
            )

        loaded = json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=reject_duplicates,
            parse_constant=reject_constant,
        )
        if not isinstance(loaded, dict):
            raise MeasurementValidationError(f"{path}: top-level value must be an object")
        return loaded

    schema = strict_load(args.schema)
    report = strict_load(args.report)
    validate_project_lane_ablation_report(schema, report)
    print(f"validated project lane-ablation measurement: {args.report}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
