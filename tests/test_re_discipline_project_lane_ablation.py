import json
import unittest
from pathlib import Path

from tests.re_discipline_project_lane_ablation import (
    MeasurementValidationError,
    _expected_bootstrap,
    _expected_slices,
    _expected_wilson,
    _ordered_identity,
    validate_project_lane_ablation_report,
)


ROOT = Path(__file__).resolve().parents[1]
SCHEMA = json.loads(
    (
        ROOT
        / "knowledge"
        / "schemas"
        / "project-lane-ablation-report.schema.json"
    ).read_text(encoding="utf-8")
)
IDENTITY = "sha256:" + "a" * 64
BARE_DIGEST = "b" * 64


def metrics() -> dict[str, int | float]:
    return {
        "recallAtK": 0.0,
        "meanReciprocalRank": 0.0,
        "nDCG": 0.0,
        "precisionAtK": 0.0,
        "exactIdentifierHitRate": 0.0,
        "completeEvidenceCoverage": 0.0,
        "abstentionAccuracy": 1.0,
        "citationPrecision": 0.0,
        "citationRecall": 0.0,
        "supportingEvidenceRecall": 0.0,
        "budgetComplianceRate": 1.0,
        "authorityViolationRate": 0.0,
        "staleResultRate": 0.0,
        "authorityViolations": 0,
        "citationViolations": 0,
        "citationMetadataViolations": 0,
        "hardNegativeHits": 0,
        "relevantTokenRatio": 0.0,
        "duplicateTokenRatio": 0.0,
        "deterministicReplayRate": 1.0,
        "p50LatencyMillis": 1,
        "p95LatencyMillis": 2,
    }


def outcome(case_id: str, split: str, topic: str) -> dict[str, object]:
    return {
        "caseId": case_id,
        "split": split,
        "topic": topic,
        "paths": [],
        "tiers": [],
        "chunkIds": [],
        "contentHashes": [],
        "relevantPaths": [],
        "relevantRanks": [],
        "hardNegativeHits": [],
        "expectedCitationsFound": [],
        "estimatedTokens": 0,
        "returnedUniquePaths": 0,
        "expectedFound": False,
        "completeEvidence": False,
        "authoritySafe": True,
        "citationMetadataSafe": True,
        "citationSafe": True,
        "corpusMatched": True,
        "abstentionCorrect": True,
        "budgetSafe": True,
        "replayIdentical": True,
        "minimumTokenBudget": 128,
        "qualityGateApplicable": False,
        "safetyPassed": True,
        "qualityPassed": True,
        "gatePassed": True,
        "returnedTokens": 0,
        "relevantTokens": 0,
        "duplicateTokens": 0,
        "staleResults": 0,
        "latencyMillis": 1,
    }


def comparison() -> dict[str, object]:
    return {
        "hitEffect": "unchanged",
        "rankDelta": 0,
        "pathsChanged": False,
        "uniqueRescue": False,
        "rankImproved": False,
        "rankDegraded": False,
    }


def report_fixture() -> dict[str, object]:
    cases = []
    for index in range(64):
        case_id = f"case-{index:02d}"
        split = "development" if index < 32 else "holdout"
        topic = "topic-development" if index < 32 else "topic-holdout"
        cases.append(
            {
                "caseId": case_id,
                "split": split,
                "role": "manager" if index < 52 else "drafter",
                "topic": topic,
                "queryClass": "conceptual",
                "vocabularyPolicy": (
                    "target-disjoint-v1" if index == 32 else "none"
                ),
                "answerable": True,
                "baseline": outcome(case_id, split, topic),
                "dense": outcome(case_id, split, topic),
                "denseComparison": comparison(),
            }
        )
    profile_rows = [
        ("baseline", "lexical-graph-v1", ["exact", "fts", "graph"], []),
        (
            "dense",
            "hybrid-no-rerank-v1",
            ["exact", "fts", "graph", "dense"],
            ["builtin:embedding"],
        ),
        (
            "rerank",
            "hybrid-local-v1",
            ["exact", "fts", "graph", "dense", "rerank"],
            ["builtin:embedding", "builtin:reranker"],
        ),
    ]
    historical_profiles = [
        {
            "role": role,
            "name": name,
            "activeLanes": lanes,
            "effectiveIdentity": name + "@" + IDENTITY,
            "observationDigest": IDENTITY,
            "modelIds": model_ids,
            "hardGatesPassed": True,
            "nonInferiorToLexical": True,
            "metrics": metrics(),
            "metricsBySplit": {
                "development": metrics(),
                "holdout": metrics(),
            },
        }
        for role, name, lanes, model_ids in profile_rows
    ]
    profiles = historical_profiles[:2]
    historical_cases = [
        {
            "caseId": case["caseId"],
            "split": case["split"],
            "topic": case["topic"],
            "answerable": case["answerable"],
            "dense": outcome(case["caseId"], case["split"], case["topic"]),
            "rerank": outcome(case["caseId"], case["split"], case["topic"]),
            "rerankComparison": comparison(),
        }
        for case in cases
    ]
    eval_files = [
        {
            "path": f".re-discipline/knowledge/evals/cases-{index}.json",
            "finalSha256": IDENTITY,
            "projectedSha256": IDENTITY,
            "finalByteCount": 1000,
            "projectedByteCount": 900 if index == 0 else 1000,
            "caseCount": 8 if index < 8 else 0,
            "removedOccurrences": 26 if index == 0 else 0,
        }
        for index in range(9)
    ]
    dense_zero_counts = {
        "denseRescues": 0,
        "denseLosses": 0,
        "denseRankImprovements": 0,
        "denseRankDegradations": 0,
    }
    rerank_zero_counts = {
        "rerankRescues": 0,
        "rerankLosses": 0,
        "rerankRankImprovements": 0,
        "rerankRankDegradations": 0,
    }
    metric_evidence = {
        role: {
            key: value
            for key, value in profile["metrics"].items()
            if key not in {"p50LatencyMillis", "p95LatencyMillis"}
        }
        for role, profile in ((row["role"], row) for row in profiles)
    }
    metric_digest = _ordered_identity(metric_evidence)
    historical_metric_evidence = {
        role: {
            key: value
            for key, value in historical_profiles[index]["metrics"].items()
            if key not in {"p50LatencyMillis", "p95LatencyMillis"}
        }
        for index, role in enumerate(("baseline", "dense"))
    }
    historical_metric_digest = _ordered_identity(historical_metric_evidence)
    return {
        "$schema": "plugin://re-discipline/schemas/project-lane-ablation-report.schema.json",
        "schemaVersion": 2,
        "kind": "project-retrieval-lane-ablation",
        "measurementOnly": True,
        "project": "fixture-project",
        "evaluatedAt": "2026-08-03T00:00:00Z",
        "provenance": {
            "frozenSourceRevision": "c" * 40,
            "projectGitRevision": "d" * 40,
            "projectDirtyFingerprint": IDENTITY,
            "frozenRuntimeFingerprint": IDENTITY,
            "rawBenchmarkSha256": IDENTITY,
            "rawBenchmarkRunId": "benchmark-fixture",
            "rawBenchmarkComplete": True,
            "rawBenchmarkArmCount": 2,
            "rawBenchmarkCasesPerArm": 64,
            "rawBenchmarkDurationMillis": 1,
            "parserVersion": "parser-v1",
            "chunkerVersion": "chunker-v1",
            "profileCatalogSha256": IDENTITY,
            "modelManifestSha256": IDENTITY,
        },
        "corpus": {
            "generationId": "generation-" + "d" * 20,
            "corpusFingerprint": IDENTITY,
            "documentCount": 1,
            "chunkCount": 1,
            "indexedSourceProof": {
                "algorithm": "sorted-path-null-sha256-v1",
                "sourceCount": 1,
                "byteExactCount": 1,
                "mismatchCount": 0,
                "pathDigestPairsSha256": IDENTITY,
            },
            "finalEvalFileCount": 9,
            "finalEvalCaseCount": 64,
            "finalEvalFiles": eval_files,
            "projectedEvalFingerprint": IDENTITY,
            "projectionManifestSha256": IDENTITY,
            "projectionTransformDigest": IDENTITY,
            "projectionOperation": "delete-whole-json-member-line-v1",
            "projectionRemovedOccurrences": 26,
            "projectionPreservesAllOtherBytes": True,
        },
        "harness": {
            "sourceRepositoryMutated": False,
            "indexedSourceBytesUnchanged": True,
            "receiptSha256": IDENTITY,
            "pluginRevision": "c" * 40,
            "projectRevision": "d" * 40,
            "benchmarkCommandSha256": IDENTITY,
            "projectionManifestArtifactSha256": IDENTITY,
            "indexedSourcesManifestSha256": IDENTITY,
            "controlPlaneSubstitutions": [
                {
                    "kind": kind,
                    "projectPath": path,
                    "originalSha256": IDENTITY,
                    "replacementSha256": IDENTITY,
                    "replacementSource": "frozen-template",
                }
                for kind, path in (
                    ("bootstrap-config", ".re-discipline/config.json"),
                    ("knowledge-policy", ".re-discipline/knowledge/policy.jsonc"),
                    (
                        "retrieval-profile",
                        ".re-discipline/knowledge/retrieval-profile.json",
                    ),
                )
            ],
            "renamedPaths": [
                {"from": name, "to": name + ".checkout", "restorable": True}
                for name in ("active", "docs", ".re-discipline")
            ],
            "excludedSourcePaths": [
                ".re-discipline/cache",
                ".re-discipline/local-paths.md",
            ],
            "negativeControls": [
                {
                    "name": "external-cache-without-grant",
                    "expectedFailure": True,
                    "observed": True,
                    "message": "cache boundary rejected the ungranted path",
                }
            ],
        },
        "models": [
            {
                "id": "builtin:embedding",
                "role": "embedding",
                "revision": "1",
                "implementation": "fixture",
                "specSha256": BARE_DIGEST,
                "artifactSha256": BARE_DIGEST,
            },
        ],
        "profiles": profiles,
        "cases": cases,
        "slices": _expected_slices(cases),
        "uncertainty": {
            "wilsonIntervals": _expected_wilson(cases),
            "topicClusterBootstrap": _expected_bootstrap(
                cases, 10_000, 0x5A17_2026
            ),
        },
        "sensitivity": {
            "budgetSlices": [
                {"tokenBudget": budget, **dense_zero_counts}
                for budget in (512, 1024, 2048, 4096)
            ],
            "budgetCaseComparisons": [
                {
                    "tokenBudget": budget,
                    "cases": [
                        {
                            "caseId": case["caseId"],
                            "denseComparison": comparison(),
                        }
                        for case in cases
                    ],
                }
                for budget in (512, 1024, 2048, 4096)
            ],
            "leaveOneTopicOut": [
                {
                    "omittedTopic": topic,
                    "answerableCases": 32,
                    "denseHitRateDelta": 0.0,
                }
                for topic in ("topic-development", "topic-holdout")
            ],
            "densePathChangedCases": 0,
            "preliminaryComparison": {
                "available": True,
                "preliminaryCorpusFingerprint": IDENTITY,
                "finalCorpusFingerprint": IDENTITY,
                "preliminaryDenseRescues": 0,
                "preliminaryMetricsSha256": historical_metric_digest,
                "finalMetricsSha256": metric_digest,
                "metricsChanged": False,
                "denseRescueDelta": 0,
            },
        },
        "historicalRerank": {
            "provenance": {
                "runtimeSourceRevision": "e" * 40,
                "projectGitRevision": "f" * 40,
                "projectDirtyFingerprint": IDENTITY,
                "runtimeFingerprint": IDENTITY,
                "rawBenchmarkSha256": IDENTITY,
                "evidenceArchivePath": "evals/conformance/evidence/historical.zip",
                "evidenceArchiveSha256": IDENTITY,
                "rawBenchmarkByteCount": 7500000,
                "evidenceArchiveByteCount": 450000,
                "evidenceArchiveFormat": "zip-deflate-fixed-v1",
                "rawBenchmarkRunId": "benchmark-historical",
                "rawBenchmarkComplete": True,
                "rawBenchmarkArmCount": 3,
                "rawBenchmarkCasesPerArm": 64,
                "rawBenchmarkDurationMillis": 2,
                "generationId": "generation-" + "e" * 20,
                "corpusFingerprint": IDENTITY,
                "evalFingerprint": IDENTITY,
                "parserVersion": "parser-v1",
                "chunkerVersion": "chunker-v1",
                "profileCatalogSha256": IDENTITY,
                "modelManifestSha256": IDENTITY,
                "projectionManifestSha256": IDENTITY,
                "projectionTransformDigest": IDENTITY,
            },
            "models": [
                {
                    "id": "builtin:embedding",
                    "role": "embedding",
                    "revision": "1",
                    "implementation": "fixture",
                    "specSha256": BARE_DIGEST,
                    "artifactSha256": BARE_DIGEST,
                },
                {
                    "id": "builtin:reranker",
                    "role": "reranker",
                    "revision": "1",
                    "implementation": "fixture",
                    "specSha256": BARE_DIGEST,
                },
            ],
            "profiles": historical_profiles,
            "cases": historical_cases,
            "budgetSlices": [
                {"tokenBudget": budget, **rerank_zero_counts}
                for budget in (512, 1024, 2048, 4096)
            ],
            "budgetCaseComparisons": [
                {
                    "tokenBudget": budget,
                    "cases": [
                        {
                            "caseId": case["caseId"],
                            "rerankComparison": comparison(),
                        }
                        for case in historical_cases
                    ],
                }
                for budget in (512, 1024, 2048, 4096)
            ],
            "sharedSafetyFailures": 0,
            "rerankAddedSafetyRegressions": 0,
            "decision": {
                "action": "remove",
                "positiveEvidence": False,
                "eventCount": 0,
                "caseIds": [],
                "rationale": "no frozen rerank improvements",
            },
        },
        "validation": {
            "schemaSha256": IDENTITY,
            "validatorSha256": IDENTITY,
            "generatorSha256": IDENTITY,
            "command": "python validator --schema schema --report report",
            "validatedAt": "2026-08-03T00:00:00Z",
            "passed": True,
        },
        "decision": {
            "dense": {
                "action": "remove",
                "positiveEvidence": False,
                "eventCount": 0,
                "caseIds": [],
                "rationale": "no unique rescues",
            },
            "rerank": {
                "action": "remove",
                "positiveEvidence": False,
                "eventCount": 0,
                "caseIds": [],
                "rationale": "no frozen rerank improvements",
            },
            "productionLanes": ["exact", "fts", "graph"],
            "sharedSafetyFailures": 0,
            "denseAddedSafetyRegressions": 0,
            "productionProfileConsistent": True,
            "releaseGatePassed": True,
            "rationale": "fixture decisions match production lanes",
        },
    }


class ProjectLaneAblationSchemaTests(unittest.TestCase):
    def test_valid_closed_measurement_passes(self) -> None:
        validate_project_lane_ablation_report(SCHEMA, report_fixture())

    def test_schema_rejects_nonmeasurement_and_unknown_fields(self) -> None:
        for mutate in (
            lambda value: value.__setitem__("measurementOnly", False),
            lambda value: value.__setitem__("unexpected", True),
        ):
            with self.subTest(mutate=mutate):
                report = report_fixture()
                mutate(report)
        with self.assertRaises(MeasurementValidationError):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_harness_revision_and_projection_bindings_are_semantic(self) -> None:
        report = report_fixture()
        report["harness"]["pluginRevision"] = "e" * 40
        with self.assertRaisesRegex(MeasurementValidationError, "runtime provenance"):
            validate_project_lane_ablation_report(SCHEMA, report)

        report = report_fixture()
        report["harness"]["projectionManifestArtifactSha256"] = (
            "sha256:" + "e" * 64
        )
        with self.assertRaisesRegex(MeasurementValidationError, "projection artifact"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_schema_requires_exact_two_by_sixty_four_current_matrix(self) -> None:
        report = report_fixture()
        report["cases"].pop()
        with self.assertRaisesRegex(MeasurementValidationError, "minItems"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_schema_rejects_escaping_eval_path(self) -> None:
        report = report_fixture()
        report["corpus"]["finalEvalFiles"][0]["path"] = "../outside.json"
        with self.assertRaisesRegex(MeasurementValidationError, "pattern"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_semantics_recompute_case_comparisons(self) -> None:
        report = report_fixture()
        report["cases"][0]["denseComparison"]["pathsChanged"] = True
        with self.assertRaisesRegex(MeasurementValidationError, "does not match arm outcomes"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_semantics_recompute_historical_comparisons_and_budget_slices(self) -> None:
        report = report_fixture()
        report["historicalRerank"]["cases"][0]["rerankComparison"][
            "pathsChanged"
        ] = True
        with self.assertRaisesRegex(MeasurementValidationError, "historical.*arm outcomes"):
            validate_project_lane_ablation_report(SCHEMA, report)

        report = report_fixture()
        report["historicalRerank"]["budgetSlices"][0]["rerankRescues"] = 1
        with self.assertRaisesRegex(MeasurementValidationError, "budgetSlices"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_top_level_rerank_decision_must_copy_historical_decision(self) -> None:
        report = report_fixture()
        report["decision"]["rerank"]["rationale"] = "stale aggregate decision"
        with self.assertRaisesRegex(MeasurementValidationError, "historical layer"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_positive_dense_rescue_cannot_be_labeled_remove(self) -> None:
        report = report_fixture()
        case = report["cases"][0]
        case["dense"]["paths"] = ["docs/evidence.md"]
        case["dense"]["tiers"] = ["truth"]
        case["dense"]["chunkIds"] = ["chunk-1"]
        case["dense"]["contentHashes"] = ["hash-1"]
        case["dense"]["relevantPaths"] = ["docs/evidence.md"]
        case["dense"]["expectedFound"] = True
        case["dense"]["relevantRanks"] = [1]
        case["dense"]["returnedUniquePaths"] = 1
        rescue = {
            "hitEffect": "rescue",
            "rankDelta": 0,
            "pathsChanged": True,
            "uniqueRescue": True,
            "rankImproved": False,
            "rankDegraded": False,
        }
        case["denseComparison"] = rescue
        report["slices"] = _expected_slices(report["cases"])
        report["uncertainty"]["wilsonIntervals"] = _expected_wilson(
            report["cases"]
        )
        report["uncertainty"]["topicClusterBootstrap"] = _expected_bootstrap(
            report["cases"], 10_000, 0x5A17_2026
        )
        report["sensitivity"]["densePathChangedCases"] = 1
        report["sensitivity"]["preliminaryComparison"]["denseRescueDelta"] = 1
        for row in report["sensitivity"]["leaveOneTopicOut"]:
            remaining = [
                candidate
                for candidate in report["cases"]
                if candidate["answerable"] and candidate["topic"] != row["omittedTopic"]
            ]
            row["denseHitRateDelta"] = (
                sum(candidate["dense"]["expectedFound"] for candidate in remaining)
                - sum(candidate["baseline"]["expectedFound"] for candidate in remaining)
            ) / len(remaining)
        report["decision"]["dense"].update(
            positiveEvidence=True,
            eventCount=1,
            caseIds=[case["caseId"]],
        )
        with self.assertRaisesRegex(MeasurementValidationError, "derived action"):
            validate_project_lane_ablation_report(SCHEMA, report)

    def test_profile_roles_are_exactly_once(self) -> None:
        report = report_fixture()
        report["profiles"][1]["role"] = "baseline"
        with self.assertRaisesRegex(MeasurementValidationError, "ordered|repeated"):
            validate_project_lane_ablation_report(SCHEMA, report)


if __name__ == "__main__":
    unittest.main()
