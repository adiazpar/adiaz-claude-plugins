import copy
import io
import json
import sqlite3
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path

from tests.re_discipline_project_lane_ablation import (
    MeasurementValidationError,
    validate_project_lane_ablation_report,
)
from tests.re_discipline_project_lane_ablation_archive import build_archive_bytes
from tests.re_discipline_project_lane_ablation_build import (
    MeasurementBuildError,
    _calculate_metrics,
    _go_json_bytes,
    _identity,
    _normalize_eval_for_go,
    _project_bytes,
    _stable_output_bytes,
    build_report,
)
from tests.re_discipline_project_lane_ablation_stage import (
    HARNESS_SCRIPT,
    _artifact_ref,
    _directory_ref,
    _pair_digest,
    _semantic_benchmark_command,
)


ROOT = Path(__file__).resolve().parents[1]
SCHEMA_PATH = (
    ROOT
    / "knowledge"
    / "schemas"
    / "project-lane-ablation-report.schema.json"
)
VALIDATOR_PATH = ROOT / "tests" / "re_discipline_project_lane_ablation.py"
BUILDER_PATH = ROOT / "tests" / "re_discipline_project_lane_ablation_build.py"
ARCHIVE_BUILDER_PATH = (
    ROOT / "tests" / "re_discipline_project_lane_ablation_archive.py"
)
COMMITTED_HISTORICAL_ARCHIVE = (
    ROOT
    / "knowledge"
    / "evals"
    / "conformance"
    / "evidence"
    / "2026-08-03-snaphak-pre-removal-rerank.zip"
)
COMMITTED_HISTORICAL_ARCHIVE_SIZE = 456_281
COMMITTED_HISTORICAL_ARCHIVE_IDENTITY = (
    "sha256:c20ed97f01a324397eedad89f2c628a28cad94d562595f6f342a8efdae5c9ac6"
)
IDENTITY = "sha256:" + "a" * 64
BARE_DIGEST = "b" * 64
CURRENT_REVISION = "c" * 40
HISTORICAL_REVISION = "d" * 40


def _write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def _eval_case(index: int) -> dict[str, object]:
    case_id = f"case-{index:02d}"
    split = "development" if index < 32 else "holdout"
    topic = "topic-a" if index % 2 == 0 else "topic-b"
    target = f"docs/truth/target-{index:02d}.md"
    return {
        "id": case_id,
        "role": "manager" if index < 32 else "drafter",
        "topic": topic,
        "split": split,
        "query": f"fixture query {index}",
        "queryClass": "conceptual",
        "vocabularyPolicy": "target-disjoint-v1",
        "allowedTiers": ["truth"],
        "corpusSnapshot": IDENTITY,
        "expectedPaths": [target],
        "minimumEvidencePaths": [target],
        "hardNegativePaths": [],
        "expectedCitations": [target],
        "forbiddenTiers": [],
        "tokenBudget": 2048,
        "answerable": True,
    }


def _outcome(case: dict[str, object], *, hit: bool, rank: int = 1) -> dict[str, object]:
    target = str(case["expectedPaths"][0])
    paths = [target] if hit else []
    relevant_ranks = [rank] if hit else []
    safety = True
    quality = hit
    return {
        "caseId": case["id"],
        "split": case["split"],
        "topic": case["topic"],
        "paths": paths,
        "tiers": ["truth"] if hit else [],
        "chunkIds": [f"chunk-{case['id']}"] if hit else [],
        "contentHashes": [IDENTITY] if hit else [],
        "relevantPaths": paths,
        "relevantRanks": relevant_ranks,
        "hardNegativeHits": [],
        "expectedCitationsFound": paths,
        "estimatedTokens": 32 if hit else 0,
        "returnedUniquePaths": len(paths),
        "expectedFound": hit,
        "completeEvidence": hit,
        "authoritySafe": True,
        "citationMetadataSafe": True,
        "citationSafe": True,
        "corpusMatched": True,
        "abstentionCorrect": True,
        "budgetSafe": True,
        "replayIdentical": True,
        "minimumTokenBudget": 128,
        "qualityGateApplicable": True,
        "safetyPassed": safety,
        "qualityPassed": quality,
        "gatePassed": safety and quality,
        "returnedTokens": 32 if hit else 0,
        "relevantTokens": 32 if hit else 0,
        "duplicateTokens": 0,
        "staleResults": 0,
        "latencyMillis": 2,
    }


class SyntheticInputs:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.final_eval_root = root / "final-evals"
        self.projected_eval_root = root / "projected-evals"
        self.projection_path = root / "projection.json"
        self.raw_path = root / "current-raw.json"
        self.profile_catalog_path = root / "current-profiles.json"
        self.model_manifest_path = root / "current-models.json"
        self.harness_path = root / "harness.json"
        self.indexed_sources_path = root / "indexed-sources.json"
        self.current_generation_path = root / "current-generation.json"
        self.generation_database_path = root / "generation.sqlite"
        self.production_profile_path = root / "production-profile.json"
        self.historical_raw_path = root / "historical-raw.json"
        self.historical_profile_catalog_path = root / "historical-profiles.json"
        self.historical_model_manifest_path = root / "historical-models.json"
        self.historical_archive_path = root / "historical.zip"
        self.output_path = root / "measurement.json"
        self.evals = [_eval_case(index) for index in range(64)]
        self.embedding_model = {
            "id": "builtin:embedding",
            "role": "embedding",
            "revision": "1",
            "implementation": "fixture",
            "specSha256": BARE_DIGEST,
            "artifactSha256": BARE_DIGEST,
        }
        self.reranker_model = {
            "id": "builtin:reranker",
            "role": "reranker",
            "revision": "1",
            "implementation": "fixture",
            "specSha256": "e" * 64,
        }
        self._write_eval_projection()
        self._write_inputs()

    def _write_eval_projection(self) -> None:
        rows: list[dict[str, object]] = []
        projected_cases: list[dict[str, object]] = []
        for file_index in range(8):
            relative = f"cases-{file_index}.json"
            selected = self.evals[file_index * 8 : (file_index + 1) * 8]
            body = (json.dumps(selected, indent=2) + "\n").encode("utf-8")
            projected, removals = _project_bytes(body, relative)
            (self.final_eval_root / relative).parent.mkdir(parents=True, exist_ok=True)
            (self.final_eval_root / relative).write_bytes(body)
            (self.projected_eval_root / relative).parent.mkdir(
                parents=True, exist_ok=True
            )
            (self.projected_eval_root / relative).write_bytes(projected)
            projected_cases.extend(json.loads(projected))
            rows.append(
                {
                    "path": f".re-discipline/knowledge/evals/{relative}",
                    "finalSha256": _identity(body),
                    "projectedSha256": _identity(projected),
                    "finalByteCount": len(body),
                    "projectedByteCount": len(projected),
                    "caseCount": len(selected),
                    "removals": removals,
                }
            )
        readme = b"# Frozen evaluation corpus\n"
        for root in (self.final_eval_root, self.projected_eval_root):
            (root / "README.md").write_bytes(readme)
        rows.append(
            {
                "path": ".re-discipline/knowledge/evals/README.md",
                "finalSha256": _identity(readme),
                "projectedSha256": _identity(readme),
                "finalByteCount": len(readme),
                "projectedByteCount": len(readme),
                "caseCount": 0,
                "removals": [],
            }
        )
        manifest = {
            "schemaVersion": 1,
            "transform": "delete-whole-json-member-line-v1",
            "field": "vocabularyPolicy",
            "allowedValue": "target-disjoint-v1",
            "preservesAllOtherBytes": True,
            "finalFileCount": 9,
            "caseCount": 64,
            "removedOccurrences": 64,
            "files": rows,
        }
        manifest["digest"] = _identity(_go_json_bytes(manifest))
        _write_json(self.projection_path, manifest)
        self.eval_fingerprint = _identity(
            _go_json_bytes(
                [_normalize_eval_for_go(case) for case in projected_cases]
            )
        )

    def _profile(
        self,
        *,
        name: str,
        lanes: list[str],
        models: list[dict[str, object]],
        outcomes: list[dict[str, object]],
    ) -> dict[str, object]:
        eval_by_id = {case["id"]: case for case in self.evals}
        computed = _calculate_metrics(outcomes, eval_by_id)
        split_metrics = {
            split: _calculate_metrics(
                [row for row in outcomes if row["split"] == split], eval_by_id
            )
            for split in ("development", "holdout")
        }
        return {
            "profileName": name,
            "activeLanes": lanes,
            "effectiveProfile": name + "@" + IDENTITY,
            "observationDigest": IDENTITY,
            "models": copy.deepcopy(models),
            "hardGatesPassed": all(bool(row["gatePassed"]) for row in outcomes),
            "nonInferiorToLexical": True,
            "metrics": computed,
            "metricsBySplit": split_metrics,
            "cases": copy.deepcopy(outcomes),
            "casesByBudget": {
                str(budget): copy.deepcopy(outcomes)
                for budget in (512, 1024, 2048, 4096)
            },
            "contextPackCases": _context_rows(outcomes),
            "contextPacksByBudget": {
                str(budget): _context_rows(outcomes)
                for budget in (512, 1024, 2048, 4096)
            },
        }

    def _raw(self, profiles: list[dict[str, object]], run_id: str) -> dict[str, object]:
        return {
            "schemaVersion": 1,
            "runId": run_id,
            "mode": "full",
            "suite": "project-benchmark-v1",
            "requestedProfile": "plugin:balanced-v1",
            "generation": {
                "id": "generation-" + "1" * 20,
                "corpusFingerprint": IDENTITY,
                "modelFingerprint": IDENTITY,
                "project": "fixture-project",
                "worktree": str(self.root),
                "gitRevision": "f" * 40,
                "dirtyFingerprint": IDENTITY,
                "parserVersion": "parser-v1",
                "chunkerVersion": "chunker-v1",
                "createdAt": "2026-08-03T07:00:00Z",
                "runtime": {"implementation": "fixture", "version": "0.8.0"},
                "documentCount": 10,
                "chunkCount": 20,
            },
            "evalFingerprint": self.eval_fingerprint,
            "findingSuiteDigests": {},
            "hardNegativeCoverage": {
                "casesWithNegatives": 0,
                "totalCases": 64,
                "declaredPaths": 0,
            },
            "profiles": profiles,
            "unsupportedProfiles": [],
            "passed": False,
            "complete": True,
            "durationMillis": 100,
            "reportPath": str(self.raw_path),
        }

    def _write_inputs(self) -> None:
        baseline = [
            _outcome(case, hit=index != 0)
            for index, case in enumerate(self.evals)
        ]
        dense = [_outcome(case, hit=True) for case in self.evals]
        rerank = copy.deepcopy(dense)
        current_profiles = [
            self._profile(
                name="lexical-graph-v1",
                lanes=["exact", "fts", "graph"],
                models=[],
                outcomes=baseline,
            ),
            self._profile(
                name="hybrid-no-rerank-v1",
                lanes=["exact", "fts", "graph", "dense"],
                models=[self.embedding_model],
                outcomes=dense,
            ),
        ]
        historical_profiles = [
            copy.deepcopy(current_profiles[0]),
            copy.deepcopy(current_profiles[1]),
            self._profile(
                name="hybrid-local-v1",
                lanes=["exact", "fts", "graph", "dense", "rerank"],
                models=[self.embedding_model, self.reranker_model],
                outcomes=rerank,
            ),
        ]
        _write_json(
            self.raw_path,
            self._raw(current_profiles, "benchmark-20260803T070000.000000000Z"),
        )
        _write_json(
            self.historical_raw_path,
            self._raw(
                historical_profiles, "benchmark-20260803T060000.000000000Z"
            ),
        )

        def catalog(rows: list[tuple[str, list[str]]]) -> dict[str, object]:
            return {
                "$schema": "../schemas/retrieval-profile.schema.json",
                "schemaVersion": 1,
                "profileId": "plugin:balanced-v1",
                "effectiveProfiles": [
                    {"name": name, "lanes": lanes} for name, lanes in rows
                ],
            }

        current_catalog_rows = [
            ("lexical-graph-v1", ["exact", "fts", "graph"]),
            (
                "hybrid-no-rerank-v1",
                ["exact", "fts", "graph", "dense"],
            ),
        ]
        _write_json(self.profile_catalog_path, catalog(current_catalog_rows))
        _write_json(
            self.historical_profile_catalog_path,
            catalog(
                current_catalog_rows
                + [
                    (
                        "hybrid-local-v1",
                        ["exact", "fts", "graph", "dense", "rerank"],
                    )
                ]
            ),
        )
        _write_json(
            self.model_manifest_path,
            {"schemaVersion": 1, "models": [self.embedding_model]},
        )
        _write_json(
            self.historical_model_manifest_path,
            {
                "schemaVersion": 1,
                "models": [self.embedding_model, self.reranker_model],
            },
        )
        self._write_harness()
        _write_json(
            self.production_profile_path,
            {
                "effectiveProfiles": [
                    {
                        "name": "hybrid-no-rerank-v1",
                        "lanes": ["exact", "fts", "graph", "dense"],
                    },
                    {
                        "name": "lexical-graph-v1",
                        "lanes": ["exact", "fts", "graph"],
                    },
                ]
            },
        )
        self.historical_archive_path.write_bytes(
            build_archive_bytes(
                raw_benchmark_path=self.historical_raw_path,
                projection_manifest_path=self.projection_path,
                projected_eval_root=self.projected_eval_root,
                profile_catalog_path=self.historical_profile_catalog_path,
                model_manifest_path=self.historical_model_manifest_path,
            )
        )

    def _write_harness(self) -> None:
        sources = [
            {
                "path": f"docs/truth/source-{index:02d}.md",
                "tier": "truth",
                "sourceKind": "",
                "size": 1,
                "sha256": IDENTITY,
                "checkoutSha256": IDENTITY,
                "byteExact": True,
            }
            for index in range(10)
        ]
        connection = sqlite3.connect(self.generation_database_path)
        try:
            connection.execute(
                "CREATE TABLE documents ("
                "path TEXT PRIMARY KEY, tier TEXT NOT NULL, "
                "content_hash TEXT NOT NULL, size INTEGER NOT NULL, "
                "source_kind TEXT NOT NULL)"
            )
            for source in sources:
                connection.execute(
                    "INSERT INTO documents VALUES (?,?,?,?,?)",
                    (
                        source["path"],
                        source["tier"],
                        source["sha256"].removeprefix("sha256:"),
                        source["size"],
                        source["sourceKind"],
                    ),
                )
            connection.commit()
        finally:
            connection.close()
        raw = json.loads(self.raw_path.read_text(encoding="utf-8"))
        pointer = copy.deepcopy(raw["generation"])
        pointer["database"] = str(self.generation_database_path)
        _write_json(self.current_generation_path, pointer)
        proof = {
            "algorithm": "sorted-path-null-sha256-v1",
            "sourceCount": len(sources),
            "byteExactCount": len(sources),
            "mismatchCount": 0,
            "pathDigestPairsSha256": _pair_digest(
                (source["path"], source["sha256"]) for source in sources
            ),
        }
        indexed = {
            "$schema": "plugin://re-discipline/schemas/indexed-source-manifest.internal.json",
            "schemaVersion": 1,
            "kind": "project-indexed-source-byte-proof",
            "project": "fixture-project",
            "projectRevision": "f" * 40,
            "generationId": raw["generation"]["id"],
            "corpusFingerprint": raw["generation"]["corpusFingerprint"],
            **proof,
            "sources": sources,
        }
        _write_json(self.indexed_sources_path, indexed)
        command = _semantic_benchmark_command("go")
        command.update(
            {
                "exitCode": 1,
                "stdoutSha256": IDENTITY,
                "stderrSha256": IDENTITY,
            }
        )
        substitutions = [
            {
                "kind": kind,
                "projectPath": project_path,
                "originalSha256": IDENTITY,
                "replacementSha256": IDENTITY,
                "replacementSource": replacement,
            }
            for kind, replacement, project_path in (
                (
                    "bootstrap-config",
                    "templates/project/config.json",
                    ".re-discipline/config.json",
                ),
                (
                    "knowledge-policy",
                    "templates/project/policy.jsonc",
                    ".re-discipline/knowledge/policy.jsonc",
                ),
                (
                    "retrieval-profile",
                    "templates/project/retrieval-profile.json",
                    ".re-discipline/knowledge/retrieval-profile.json",
                ),
            )
        ]
        receipt = {
            "$schema": "plugin://re-discipline/schemas/project-lane-ablation-harness.schema.json",
            "schemaVersion": 1,
            "kind": "project-retrieval-staging-harness",
            "project": "fixture-project",
            "sourceRepositoryMutated": False,
            "repositories": {
                "plugin": {
                    "revision": CURRENT_REVISION,
                    "tree": "1" * 40,
                    "trackedFileCount": 1,
                    "trackedManifestSha256": IDENTITY,
                    "cleanBefore": True,
                    "cleanAfter": True,
                },
                "project": {
                    "revision": "f" * 40,
                    "tree": "2" * 40,
                    "trackedFileCount": 1,
                    "trackedManifestSha256": IDENTITY,
                    "cleanBefore": True,
                    "cleanAfter": True,
                },
            },
            "checkout": {
                "disposableProjectPath": "disposable/project",
                "preservedCheckoutPath": "checkout-roots",
                "sourceRootsAlgorithm": "sorted-path-null-sha256-v1",
                "sourceRootsFileCount": 1,
                "sourceRootsManifestSha256": IDENTITY,
            },
            "tools": {
                "harnessScriptSha256": _identity((ROOT / HARNESS_SCRIPT).read_bytes()),
                "harnessSchemaSha256": _identity(
                    (
                        ROOT
                        / "knowledge/schemas/project-lane-ablation-harness.schema.json"
                    ).read_bytes()
                ),
                "pythonVersion": "fixture-python",
                "gitVersion": "fixture-git",
                "goVersion": "fixture-go",
            },
            "benchmarkCommand": command,
            "artifacts": {
                "rawBenchmark": _artifact_ref(self.raw_path, self.root),
                "projectionManifest": _artifact_ref(self.projection_path, self.root),
                "profileCatalog": _artifact_ref(self.profile_catalog_path, self.root),
                "modelManifest": _artifact_ref(self.model_manifest_path, self.root),
                "indexedSources": _artifact_ref(self.indexed_sources_path, self.root),
                "currentGeneration": _artifact_ref(
                    self.current_generation_path, self.root
                ),
                "generationDatabase": _artifact_ref(
                    self.generation_database_path, self.root
                ),
                "finalEvalRoot": _directory_ref(self.final_eval_root, self.root),
                "projectedEvalRoot": _directory_ref(
                    self.projected_eval_root, self.root
                ),
            },
            "runtime": {
                "runId": raw["runId"],
                "generationId": raw["generation"]["id"],
                "corpusFingerprint": raw["generation"]["corpusFingerprint"],
                "evalFingerprint": raw["evalFingerprint"],
                "projectGitRevision": raw["generation"]["gitRevision"],
                "runtimeIdentity": raw["generation"]["runtime"],
                "runtimeIdentitySha256": _identity(
                    _go_json_bytes(raw["generation"]["runtime"])
                ),
                "profileCatalogSha256": _identity(
                    self.profile_catalog_path.read_bytes()
                ),
                "modelManifestSha256": _identity(
                    self.model_manifest_path.read_bytes()
                ),
                "armCount": 2,
                "casesPerArm": 64,
            },
            "indexedSourceProof": proof,
            "controlPlaneSubstitutions": substitutions,
            "renamedPaths": [
                {
                    "from": name,
                    "to": f"checkout-roots/{name}",
                    "restorable": True,
                }
                for name in ("active", "docs", ".re-discipline")
            ],
            "excludedSourcePaths": [
                ".re-discipline/cache",
                ".re-discipline/local-paths.md",
            ],
            "negativeControls": [
                {
                    "name": name,
                    "expectedFailure": True,
                    "observed": True,
                    "message": "rejected",
                }
                for name in (
                    "dirty-source-repository",
                    "projection-byte-tamper",
                    "indexed-source-byte-tamper",
                )
            ],
            "migrationGuard": {
                "paths": [
                    ".re-discipline/state",
                    ".re-discipline/transactions",
                    "docs/history/campaigns",
                ],
                "absentBefore": True,
                "absentAfter": True,
            },
        }
        _write_json(self.harness_path, receipt)

    def build(self) -> dict[str, object]:
        return build_report(
            raw_path=self.raw_path,
            projection_manifest_path=self.projection_path,
            final_eval_root=self.final_eval_root,
            projected_eval_root=self.projected_eval_root,
            profile_catalog_path=self.profile_catalog_path,
            model_manifest_path=self.model_manifest_path,
            harness_receipt_path=self.harness_path,
            historical_evidence_archive_path=self.historical_archive_path,
            historical_evidence_receipt_path=(
                "evals/conformance/evidence/fixture-historical.zip"
            ),
            production_profile_path=self.production_profile_path,
            schema_path=SCHEMA_PATH,
            validator_path=VALIDATOR_PATH,
            runtime_source_revision=CURRENT_REVISION,
            historical_runtime_source_revision=HISTORICAL_REVISION,
        )

    def cli(self, *extra: str) -> list[str]:
        return [
            sys.executable,
            str(BUILDER_PATH),
            "--raw-benchmark",
            str(self.raw_path),
            "--projection-manifest",
            str(self.projection_path),
            "--final-eval-root",
            str(self.final_eval_root),
            "--projected-eval-root",
            str(self.projected_eval_root),
            "--profile-catalog",
            str(self.profile_catalog_path),
            "--model-manifest",
            str(self.model_manifest_path),
            "--harness-receipt",
            str(self.harness_path),
            "--historical-evidence-archive",
            str(self.historical_archive_path),
            "--historical-evidence-path",
            "evals/conformance/evidence/fixture-historical.zip",
            "--production-profile",
            str(self.production_profile_path),
            "--schema",
            str(SCHEMA_PATH),
            "--validator",
            str(VALIDATOR_PATH),
            "--runtime-source-revision",
            CURRENT_REVISION,
            "--historical-runtime-source-revision",
            HISTORICAL_REVISION,
            "--output",
            str(self.output_path),
            *extra,
        ]


def _context_rows(outcomes: list[dict[str, object]]) -> list[dict[str, object]]:
    """Model the runtime's bounded context-pack outcome rows for a fixture."""

    rows = []
    for outcome in outcomes:
        safety = bool(outcome["safetyPassed"])
        quality = bool(outcome["qualityPassed"])
        rows.append(
            {
                "caseId": outcome["caseId"],
                "split": outcome["split"],
                "topic": outcome["topic"],
                "safetyPassed": safety,
                "qualityPassed": quality,
                "passed": safety and quality,
            }
        )
    return rows


class ProjectLaneAblationBuildTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.inputs = SyntheticInputs(Path(self.temporary.name))

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_direct_file_help_from_repository_root(self) -> None:
        result = subprocess.run(
            [sys.executable, str(BUILDER_PATH), "--help"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("historical-evidence-archive", result.stdout)

    def test_valid_build_is_byte_stable_and_separates_runtime_layers(self) -> None:
        first = self.inputs.build()
        second = self.inputs.build()
        self.assertEqual(_stable_output_bytes(first), _stable_output_bytes(second))
        self.assertEqual(first["provenance"]["rawBenchmarkArmCount"], 2)
        self.assertEqual(
            first["historicalRerank"]["provenance"]["rawBenchmarkArmCount"], 3
        )
        self.assertEqual([row["role"] for row in first["models"]], ["embedding"])
        self.assertEqual(first["decision"]["dense"]["action"], "retain")
        self.assertEqual(first["decision"]["rerank"]["action"], "remove")

    def test_cli_generate_verify_and_one_byte_tamper_rejection(self) -> None:
        generated = subprocess.run(
            self.inputs.cli(), cwd=ROOT, text=True, capture_output=True, check=False
        )
        self.assertEqual(generated.returncode, 0, generated.stderr)
        verified = subprocess.run(
            self.inputs.cli("--verify"),
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        body = bytearray(self.inputs.output_path.read_bytes())
        body[-2] = ord(" ")
        self.inputs.output_path.write_bytes(body)
        rejected = subprocess.run(
            self.inputs.cli("--verify"),
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("byte-identical", rejected.stderr)

    def test_current_profile_and_metric_tampering_are_rejected(self) -> None:
        for mutate, expected in (
            (
                lambda raw: raw["profiles"][1]["activeLanes"].append("rerank"),
                "controlled profile",
            ),
            (
                lambda raw: raw["profiles"][0]["metrics"].__setitem__(
                    "recallAtK", 0.25
                ),
                "recomputed",
            ),
            (
                lambda raw: raw["profiles"][0]["cases"].__setitem__(
                    1, copy.deepcopy(raw["profiles"][0]["cases"][0])
                ),
                "duplicates caseId",
            ),
            (
                lambda raw: raw["profiles"][1]["contextPackCases"][0].__setitem__(
                    "safetyPassed", False
                ),
                "safetyPassed",
            ),
        ):
            with self.subTest(expected=expected):
                original = self.inputs.raw_path.read_text(encoding="utf-8")
                raw = json.loads(original)
                mutate(raw)
                _write_json(self.inputs.raw_path, raw)
                with self.assertRaisesRegex(MeasurementBuildError, expected):
                    self.inputs.build()
                self.inputs.raw_path.write_text(original, encoding="utf-8")

    def test_historical_archive_and_provenance_tampering_are_rejected(self) -> None:
        original = self.inputs.historical_archive_path.read_bytes()
        damaged = bytearray(original)
        damaged[len(damaged) // 2] ^= 1
        self.inputs.historical_archive_path.write_bytes(damaged)
        with self.assertRaises(MeasurementBuildError):
            self.inputs.build()
        self.inputs.historical_archive_path.write_bytes(original)

        report = self.inputs.build()
        report["historicalRerank"]["provenance"]["evidenceArchiveSha256"] = IDENTITY
        schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
        # Internal report validation cannot open the archive, but changing its
        # per-case decision is always independently rejected.
        report["historicalRerank"]["decision"]["action"] = "retain"
        with self.assertRaises(MeasurementValidationError):
            validate_project_lane_ablation_report(schema, report)

    def test_staging_receipt_revision_artifact_and_database_tampering_are_rejected(self) -> None:
        receipt_original = self.inputs.harness_path.read_bytes()
        receipt = json.loads(receipt_original)
        receipt["repositories"]["plugin"]["revision"] = "e" * 40
        _write_json(self.inputs.harness_path, receipt)
        with self.assertRaisesRegex(MeasurementBuildError, "current runtime revision"):
            self.inputs.build()
        self.inputs.harness_path.write_bytes(receipt_original)

        receipt = json.loads(receipt_original)
        receipt["artifacts"]["rawBenchmark"]["sha256"] = IDENTITY
        _write_json(self.inputs.harness_path, receipt)
        with self.assertRaisesRegex(MeasurementBuildError, "digest differs"):
            self.inputs.build()
        self.inputs.harness_path.write_bytes(receipt_original)

        database_original = self.inputs.generation_database_path.read_bytes()
        self.inputs.generation_database_path.write_bytes(database_original + b"tamper")
        with self.assertRaisesRegex(MeasurementBuildError, "size or digest"):
            self.inputs.build()
        self.inputs.generation_database_path.write_bytes(database_original)
        self.inputs.build()

    def test_archive_builder_is_byte_stable(self) -> None:
        first = build_archive_bytes(
            raw_benchmark_path=self.inputs.historical_raw_path,
            projection_manifest_path=self.inputs.projection_path,
            projected_eval_root=self.inputs.projected_eval_root,
            profile_catalog_path=self.inputs.historical_profile_catalog_path,
            model_manifest_path=self.inputs.historical_model_manifest_path,
        )
        second = build_archive_bytes(
            raw_benchmark_path=self.inputs.historical_raw_path,
            projection_manifest_path=self.inputs.projection_path,
            projected_eval_root=self.inputs.projected_eval_root,
            profile_catalog_path=self.inputs.historical_profile_catalog_path,
            model_manifest_path=self.inputs.historical_model_manifest_path,
        )
        self.assertEqual(first, second)

    def test_committed_historical_archive_identity_is_immutable(self) -> None:
        body = COMMITTED_HISTORICAL_ARCHIVE.read_bytes()
        self.assertEqual(len(body), COMMITTED_HISTORICAL_ARCHIVE_SIZE)
        self.assertEqual(_identity(body), COMMITTED_HISTORICAL_ARCHIVE_IDENTITY)
        with zipfile.ZipFile(io.BytesIO(body), "r") as archive:
            self.assertEqual(
                [row.filename for row in archive.infolist()],
                sorted(
                    [
                        "evals/README.md",
                        "evals/binaries-tooling-holdout.json",
                        "evals/campaign-history-abstention-expansion.json",
                        "evals/daemon-development.json",
                        "evals/development-batch2.json",
                        "evals/drafter-role.json",
                        "evals/engine-codec-holdout.json",
                        "evals/holdout-batch2.json",
                        "evals/snaphak-development.json",
                        "model-manifest.json",
                        "profile-catalog.json",
                        "projection-manifest.json",
                        "raw-benchmark.json",
                    ]
                ),
            )

    def test_utf16le_bom_raw_member_is_preserved_and_strictly_decoded(self) -> None:
        raw_value = json.loads(
            self.inputs.historical_raw_path.read_text(encoding="utf-8")
        )
        raw_bytes = (json.dumps(raw_value, indent=2) + "\n").encode("utf-16")
        self.assertTrue(raw_bytes.startswith(b"\xff\xfe"))
        self.inputs.historical_raw_path.write_bytes(raw_bytes)
        archive_bytes = build_archive_bytes(
            raw_benchmark_path=self.inputs.historical_raw_path,
            projection_manifest_path=self.inputs.projection_path,
            projected_eval_root=self.inputs.projected_eval_root,
            profile_catalog_path=self.inputs.historical_profile_catalog_path,
            model_manifest_path=self.inputs.historical_model_manifest_path,
        )
        with zipfile.ZipFile(io.BytesIO(archive_bytes), "r") as archive:
            archived_raw = archive.read("raw-benchmark.json")
        self.assertEqual(archived_raw, raw_bytes)
        self.assertEqual(_identity(archived_raw), _identity(raw_bytes))
        self.inputs.historical_archive_path.write_bytes(archive_bytes)
        report = self.inputs.build()
        self.assertEqual(
            report["historicalRerank"]["provenance"]["rawBenchmarkSha256"],
            _identity(raw_bytes),
        )

    def test_strict_duplicate_keys_and_nan_are_rejected(self) -> None:
        for body in (
            '{"schemaVersion":1,"schemaVersion":1}\n',
            '{"schemaVersion":NaN}\n',
        ):
            with self.subTest(body=body):
                original = self.inputs.raw_path.read_text(encoding="utf-8")
                self.inputs.raw_path.write_text(body, encoding="utf-8")
                with self.assertRaises(MeasurementBuildError):
                    self.inputs.build()
                self.inputs.raw_path.write_text(original, encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
