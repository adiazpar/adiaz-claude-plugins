"""Promote a closed project measurement into packaged lane-release evidence.

The project measurement is the only source for project-corpus counts, rescue
inventory, uncertainty, and lane decisions. The packaged conformance report is
retained as its own immutable runtime layer. This tool replaces only its
projectEvidence block, recomputes the packaged holdout layer from per-case
rows, and writes a digest-bound decision receipt.
"""

from __future__ import annotations

import argparse
import copy
import json
import os
import sys
import tempfile
from pathlib import Path
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from tests.re_discipline_project_lane_ablation import (
    MeasurementValidationError,
    validate_json_schema,
    validate_project_lane_ablation_report,
)
from tests.re_discipline_project_lane_ablation_build import (
    _identity,
    _stable_output_bytes,
    _strict_json_loads,
)


PROJECT_MEASUREMENT_PATH = "evals/conformance/project-lane-ablation.json"


class PromotionError(ValueError):
    """Raised when evidence cannot be promoted into a closed release receipt."""


def _fail(path: str, message: str) -> None:
    raise PromotionError(f"{path}: {message}")


def _read_json(path: Path) -> tuple[dict[str, Any], bytes]:
    if path.is_symlink():
        _fail(str(path), "may not be a symbolic link")
    try:
        body = path.read_bytes()
        value = _strict_json_loads(body, str(path))
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError) as error:
        raise PromotionError(f"cannot read JSON {path}: {error}") from error
    if not isinstance(value, dict):
        _fail(str(path), "top-level JSON value must be an object")
    return value, body


def _rank(outcome: dict[str, Any]) -> int:
    ranks = outcome["relevantRanks"]
    return min(ranks) if ranks else 0


def _event_counts(
    cases: list[dict[str, Any]], comparison_name: str
) -> dict[str, int]:
    comparisons = [case[comparison_name] for case in cases]
    return {
        "rescues": sum(int(row["uniqueRescue"]) for row in comparisons),
        "losses": sum(int(row["hitEffect"] == "loss") for row in comparisons),
        "rankImprovements": sum(int(row["rankImproved"]) for row in comparisons),
        "rankDegradations": sum(int(row["rankDegraded"]) for row in comparisons),
    }


def _source_class(path: str) -> str:
    for prefix, source_class in (
        ("docs/history/", "history"),
        ("docs/truth/", "truth"),
        ("docs/backlog/", "backlog"),
        ("active/", "active"),
    ):
        if path.startswith(prefix):
            return source_class
    return "project"


def _profiles_by_role(
    profiles: list[dict[str, Any]], path: str
) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for index, profile in enumerate(profiles):
        role = profile.get("role")
        if not isinstance(role, str) or role in result:
            _fail(f"{path}[{index}].role", "is missing or repeated")
        result[role] = profile
    return result


def _project_evidence(
    measurement: dict[str, Any], measurement_body: bytes
) -> dict[str, Any]:
    current_cases = measurement["cases"]
    historical_cases = measurement["historicalRerank"]["cases"]
    dense_counts = _event_counts(current_cases, "denseComparison")
    rerank_counts = _event_counts(historical_cases, "rerankComparison")
    current_profiles = _profiles_by_role(measurement["profiles"], "$.profiles")

    def metric(role: str, split: str | None, field: str) -> float:
        profile = current_profiles[role]
        values = profile["metrics"] if split is None else profile["metricsBySplit"][split]
        value = values[field]
        if not isinstance(value, (int, float)) or isinstance(value, bool):
            _fail(f"$.profiles[{role}].{field}", "must be numeric")
        return float(value)

    dense = {
        **dense_counts,
        "overallRecallDelta": metric("dense", None, "recallAtK")
        - metric("baseline", None, "recallAtK"),
        "holdoutRecallDelta": metric("dense", "holdout", "recallAtK")
        - metric("baseline", "holdout", "recallAtK"),
        "overallCompleteEvidenceDelta": metric(
            "dense", None, "completeEvidenceCoverage"
        )
        - metric("baseline", None, "completeEvidenceCoverage"),
        "holdoutCompleteEvidenceDelta": metric(
            "dense", "holdout", "completeEvidenceCoverage"
        )
        - metric("baseline", "holdout", "completeEvidenceCoverage"),
    }
    rerank = {
        **rerank_counts,
        "pathsChanged": sum(
            int(case["rerankComparison"]["pathsChanged"])
            for case in historical_cases
        ),
    }

    rescues: list[dict[str, Any]] = []
    for case in current_cases:
        if not case["denseComparison"]["uniqueRescue"]:
            continue
        dense_outcome = case["dense"]
        pairs = list(
            zip(dense_outcome["relevantPaths"], dense_outcome["relevantRanks"])
        )
        if not pairs:
            _fail(f"$.cases[{case['caseId']}].dense", "rescue has no relevant path")
        target_path, dense_rank = min(pairs, key=lambda row: (row[1], row[0]))
        rescues.append(
            {
                "caseId": case["caseId"],
                "split": case["split"],
                "topic": case["topic"],
                "targetPath": target_path,
                "baselineRelevantRank": _rank(case["baseline"]),
                "denseRelevantRank": dense_rank,
                "targetDisjoint": case["vocabularyPolicy"]
                == "target-disjoint-v1",
                "sourceClass": _source_class(target_path),
                "addedHardGateRegressions": 0,
            }
        )

    uncertainty = measurement["uncertainty"]
    bootstrap = uncertainty["topicClusterBootstrap"]
    zero_topics = sorted(
        row["omittedTopic"]
        for row in measurement["sensitivity"]["leaveOneTopicOut"]
        if row["denseHitRateDelta"] <= 0
    )
    aggregate_uncertainty = {
        "wilsonIntervals": [
            {
                field: row[field]
                for field in (
                    "slice",
                    "successes",
                    "trials",
                    "estimate",
                    "lower",
                    "upper",
                )
            }
            for row in uncertainty["wilsonIntervals"]
        ],
        "topicBootstrap": {
            field: bootstrap[field]
            for field in (
                "replicates",
                "pointEstimate",
                "lower",
                "upper",
                "zeroOrLowerFraction",
            )
        },
        "leaveOneTopicOutZeroWhenOmitting": zero_topics,
        "clusterFragile": bool(zero_topics),
        "finalCorpusRerunRequired": False,
    }

    provenance = measurement["provenance"]
    corpus = measurement["corpus"]
    historical = measurement["historicalRerank"]
    decision = measurement["decision"]
    return {
        "suite": "project-benchmark-v1",
        "project": measurement["project"],
        "status": "final-corpus-verified",
        "evaluatedAt": measurement["evaluatedAt"],
        "frozenSourceRevision": provenance["frozenSourceRevision"],
        "rawBenchmarkSha256": provenance["rawBenchmarkSha256"],
        "rawBenchmarkRunId": provenance["rawBenchmarkRunId"],
        "complete": provenance["rawBenchmarkComplete"],
        "armCount": provenance["rawBenchmarkArmCount"],
        "casesPerArm": provenance["rawBenchmarkCasesPerArm"],
        "corpusFingerprint": corpus["corpusFingerprint"],
        "evalFingerprint": corpus["projectedEvalFingerprint"],
        "projectionTransformDigest": corpus["projectionTransformDigest"],
        "projectMeasurementPath": PROJECT_MEASUREMENT_PATH,
        "projectMeasurementSha256": _identity(measurement_body),
        "indexedSourceBytesUnchanged": measurement["harness"][
            "indexedSourceBytesUnchanged"
        ],
        "sharedHardGateFailures": decision["sharedSafetyFailures"],
        "denseAddedHardGateRegressions": decision[
            "denseAddedSafetyRegressions"
        ],
        "rerankAddedHardGateRegressions": historical[
            "rerankAddedSafetyRegressions"
        ],
        "dense": dense,
        "rerank": rerank,
        "rescueCases": rescues,
        "uncertainty": aggregate_uncertainty,
    }


def _packaged_counts(
    report: dict[str, Any], holdout_ids: list[str]
) -> dict[str, dict[str, int]]:
    cases = report.get("cases")
    if not isinstance(cases, list):
        _fail("$.cases", "must be an array")
    case_ids = [row.get("caseId") for row in cases if isinstance(row, dict)]
    if case_ids != sorted(holdout_ids) or len(case_ids) != len(cases):
        _fail("$.cases", "must equal the sorted frozen holdout case IDs")
    counts = {
        "dense": {"uniqueFirst": 0, "improved": 0, "degraded": 0},
        "rerank": {"uniqueFirst": 0, "improved": 0, "degraded": 0},
    }

    def validate_outcome(case_id: str, label: str, outcome: Any) -> None:
        if not isinstance(outcome, dict):
            _fail(f"$.cases[{case_id}].{label}", "must be an object")
        rank = outcome.get("relevantRank")
        hit = outcome.get("relevantHit")
        unique = outcome.get("uniqueFirst")
        findings = outcome.get("findingIds")
        if (
            not isinstance(rank, int)
            or isinstance(rank, bool)
            or rank < 0
            or hit is not (rank > 0)
            or not isinstance(unique, bool)
            or (unique and (not hit or rank != 1))
            or not isinstance(findings, list)
            or rank > len(findings)
            or findings != sorted(set(findings))
            or any(
                not isinstance(value, str) or not value.startswith("F-")
                for value in findings
            )
        ):
            _fail(f"$.cases[{case_id}].{label}", "has invalid retrieval evidence")

    def movement(
        counter: dict[str, int],
        baseline: dict[str, Any],
        candidate: dict[str, Any],
    ) -> None:
        if candidate["relevantHit"] and (
            not baseline["relevantHit"]
            or candidate["relevantRank"] < baseline["relevantRank"]
        ):
            counter["improved"] += 1
        if baseline["relevantHit"] and (
            not candidate["relevantHit"]
            or candidate["relevantRank"] > baseline["relevantRank"]
        ):
            counter["degraded"] += 1

    for case in cases:
        case_id = case["caseId"]
        for label in ("baseline", "dense", "rerank"):
            validate_outcome(case_id, label, case.get(label))
        if case["baseline"]["uniqueFirst"] or case["rerank"]["uniqueFirst"]:
            _fail(f"$.cases[{case_id}]", "assigns unique-first to a non-dense arm")
        if case["dense"]["uniqueFirst"]:
            counts["dense"]["uniqueFirst"] += 1
        movement(counts["dense"], case["baseline"], case["dense"])
        movement(counts["rerank"], case["dense"], case["rerank"])
    return counts


def promote(
    *,
    measurement_path: Path,
    project_schema_path: Path,
    aggregate_schema_path: Path,
    report_path: Path,
    finding_cases_path: Path,
) -> tuple[dict[str, Any], dict[str, Any]]:
    measurement, measurement_body = _read_json(measurement_path)
    project_schema, _ = _read_json(project_schema_path)
    aggregate_schema, _ = _read_json(aggregate_schema_path)
    report, _ = _read_json(report_path)
    finding_suite, _ = _read_json(finding_cases_path)
    try:
        validate_project_lane_ablation_report(project_schema, measurement)
    except MeasurementValidationError as error:
        raise PromotionError(f"project measurement is invalid: {error}") from error
    if (
        measurement["decision"]["releaseGatePassed"] is not True
        or measurement["decision"]["productionProfileConsistent"] is not True
    ):
        _fail("$.decision", "does not close the project release gate")
    if (
        report.get("sourceRevision")
        != measurement["historicalRerank"]["provenance"]["runtimeSourceRevision"]
    ):
        _fail(
            "$.sourceRevision",
            "does not identify the packaged pre-removal runtime layer",
        )

    holdout_ids = sorted(
        row["id"]
        for row in finding_suite.get("cases", [])
        if isinstance(row, dict) and row.get("split") == "holdout"
    )
    if len(holdout_ids) < 10 or len(set(holdout_ids)) != len(holdout_ids):
        _fail("findingCases", "must contain at least ten unique holdout IDs")
    packaged_counts = _packaged_counts(report, holdout_ids)
    if any(value for lane in packaged_counts.values() for value in lane.values()):
        _fail(
            "$.cases",
            "packaged layer is not the frozen no-positive-contribution result",
        )

    promoted_report = copy.deepcopy(report)
    promoted_report["projectEvidence"] = _project_evidence(
        measurement, measurement_body
    )
    try:
        validate_json_schema(aggregate_schema, promoted_report)
    except MeasurementValidationError as error:
        raise PromotionError(f"promoted aggregate report is invalid: {error}") from error
    report_body = _stable_output_bytes(promoted_report)

    dense_decision = measurement["decision"]["dense"]["action"]
    rerank_decision = measurement["decision"]["rerank"]["action"]
    if dense_decision not in {"retain", "remove"} or rerank_decision not in {
        "retain",
        "remove",
    }:
        _fail("$.decision", "contains an inconclusive lane")
    project = promoted_report["projectEvidence"]
    decision = {
        "schemaVersion": 3,
        "suite": promoted_report["suite"],
        "evaluatedAt": promoted_report["evaluatedAt"],
        "evalFingerprint": promoted_report["evalFingerprint"],
        "corpusFingerprint": promoted_report["corpusFingerprint"],
        "runtimeFingerprint": promoted_report["runtimeFingerprint"],
        "reportDigest": _identity(report_body),
        "evidenceLayers": {
            "packagedConformance": {
                "status": "underpowered-no-positive-contribution",
                "holdoutCases": len(holdout_ids),
                "lanes": packaged_counts,
            },
            "projectCorpus": {
                "status": project["status"],
                "casesPerArm": project["casesPerArm"],
                "rawBenchmarkSha256": project["rawBenchmarkSha256"],
                "projectMeasurementPath": project["projectMeasurementPath"],
                "projectMeasurementSha256": project[
                    "projectMeasurementSha256"
                ],
                "lanes": {
                    "dense": {
                        **{
                            field: project["dense"][field]
                            for field in (
                                "rescues",
                                "losses",
                                "rankImprovements",
                                "rankDegradations",
                            )
                        },
                        "addedHardGateRegressions": project[
                            "denseAddedHardGateRegressions"
                        ],
                    },
                    "rerank": {
                        **{
                            field: project["rerank"][field]
                            for field in (
                                "rescues",
                                "losses",
                                "rankImprovements",
                                "rankDegradations",
                            )
                        },
                        "addedHardGateRegressions": project[
                            "rerankAddedHardGateRegressions"
                        ],
                    },
                },
            },
        },
        "lanes": {
            "dense": {
                "decision": dense_decision,
                "basis": (
                    "project-corpus-rescues"
                    if dense_decision == "retain"
                    else "no-measured-benefit-current-layer"
                ),
            },
            "rerank": {
                "decision": rerank_decision,
                "basis": (
                    "frozen-historical-contribution"
                    if rerank_decision == "retain"
                    else "no-measured-benefit-across-both-layers"
                ),
            },
        },
        "conclusion": (
            "The packaged holdout remained underpowered with no positive lane "
            f"contribution. The fresh current-runtime project layer recorded "
            f"{project['dense']['rescues']} dense rescue(s) and selected "
            f"{dense_decision}; the separately frozen historical layer recorded "
            f"{project['rerank']['rescues']} rerank rescue(s) and "
            f"{project['rerank']['rankImprovements']} rank improvement(s) and "
            f"selected {rerank_decision}. No cross-runtime arms were combined."
        ),
    }
    return promoted_report, decision


def _replace_file(path: Path, body: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        _fail(str(path), "may not be a symbolic link")
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
    )
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--measurement", type=Path, required=True)
    parser.add_argument("--project-schema", type=Path, required=True)
    parser.add_argument("--aggregate-schema", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--finding-cases", type=Path, required=True)
    parser.add_argument("--decision-output", type=Path, required=True)
    parser.add_argument(
        "--verify",
        action="store_true",
        help="byte-compare both deterministic outputs without changing them",
    )
    arguments = parser.parse_args()
    try:
        report, decision = promote(
            measurement_path=arguments.measurement,
            project_schema_path=arguments.project_schema,
            aggregate_schema_path=arguments.aggregate_schema,
            report_path=arguments.report,
            finding_cases_path=arguments.finding_cases,
        )
        report_body = _stable_output_bytes(report)
        decision_body = _stable_output_bytes(decision)
        if arguments.verify:
            for path, expected in (
                (arguments.report, report_body),
                (arguments.decision_output, decision_body),
            ):
                if not path.is_file() or path.read_bytes() != expected:
                    _fail(str(path), "is not byte-identical to deterministic promotion")
        else:
            _replace_file(arguments.report, report_body)
            _replace_file(arguments.decision_output, decision_body)
    except (OSError, PromotionError) as error:
        print(f"project lane evidence promotion failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
