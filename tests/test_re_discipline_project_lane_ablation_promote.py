import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_project_lane_ablation_build import (
    _identity,
    _stable_output_bytes,
)
from tests.re_discipline_project_lane_ablation_promote import (
    PROJECT_MEASUREMENT_PATH,
    PromotionError,
    promote,
)
from tests.test_re_discipline_project_lane_ablation_build import (
    HISTORICAL_REVISION,
    SyntheticInputs,
)


ROOT = Path(__file__).resolve().parents[1]
KNOWLEDGE = ROOT / "knowledge"
PROJECT_SCHEMA = KNOWLEDGE / "schemas" / "project-lane-ablation-report.schema.json"
AGGREGATE_SCHEMA = KNOWLEDGE / "schemas" / "lane-ablation-report.schema.json"
SOURCE_REPORT = KNOWLEDGE / "evals" / "conformance" / "lane-ablation-report.json"
FINDING_CASES = KNOWLEDGE / "evals" / "conformance" / "finding-cases.json"
PROMOTER = ROOT / "tests" / "re_discipline_project_lane_ablation_promote.py"


class ProjectLaneAblationPromotionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.inputs = SyntheticInputs(self.root / "inputs")
        self.measurement = self.inputs.build()
        self.measurement["project"] = "snaphak-re"
        self.measurement_path = self.root / "project-lane-ablation.json"
        self.measurement_path.write_bytes(_stable_output_bytes(self.measurement))
        source_report = json.loads(SOURCE_REPORT.read_text(encoding="utf-8"))
        source_report["sourceRevision"] = HISTORICAL_REVISION
        self.report_path = self.root / "lane-ablation-report.json"
        self.report_path.write_bytes(_stable_output_bytes(source_report))
        self.decision_path = self.root / "lane-ablation-decision.json"

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _promote(self) -> tuple[dict[str, object], dict[str, object]]:
        return promote(
            measurement_path=self.measurement_path,
            project_schema_path=PROJECT_SCHEMA,
            aggregate_schema_path=AGGREGATE_SCHEMA,
            report_path=self.report_path,
            finding_cases_path=FINDING_CASES,
        )

    def _cli(self, *extra: str) -> list[str]:
        return [
            sys.executable,
            str(PROMOTER),
            "--measurement",
            str(self.measurement_path),
            "--project-schema",
            str(PROJECT_SCHEMA),
            "--aggregate-schema",
            str(AGGREGATE_SCHEMA),
            "--report",
            str(self.report_path),
            "--finding-cases",
            str(FINDING_CASES),
            "--decision-output",
            str(self.decision_path),
            *extra,
        ]

    def test_promotion_derives_generic_project_counts_and_receipt_digest(self) -> None:
        report, decision = self._promote()
        project = report["projectEvidence"]
        self.assertEqual(project["armCount"], 2)
        self.assertEqual(project["projectMeasurementPath"], PROJECT_MEASUREMENT_PATH)
        self.assertEqual(project["projectMeasurementSha256"], _identity(self.measurement_path.read_bytes()))
        self.assertEqual(project["dense"]["rescues"], 1)
        self.assertEqual([row["caseId"] for row in project["rescueCases"]], ["case-00"])
        self.assertEqual(len(project["uncertainty"]["wilsonIntervals"]), 4)
        self.assertEqual(
            decision["evidenceLayers"]["projectCorpus"]["lanes"]["dense"]["rescues"],
            1,
        )
        self.assertEqual(
            decision["reportDigest"],
            _identity(_stable_output_bytes(report)),
        )
        self.assertEqual(decision["lanes"]["dense"]["decision"], "retain")
        self.assertEqual(decision["lanes"]["rerank"]["decision"], "remove")

    def test_cli_generate_verify_and_tamper_rejection(self) -> None:
        generated = subprocess.run(
            self._cli(), cwd=ROOT, text=True, capture_output=True, check=False
        )
        self.assertEqual(generated.returncode, 0, generated.stderr)
        verified = subprocess.run(
            self._cli("--verify"),
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        body = bytearray(self.decision_path.read_bytes())
        body[-2] = ord(" ")
        self.decision_path.write_bytes(body)
        rejected = subprocess.run(
            self._cli("--verify"),
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("byte-identical", rejected.stderr)

    def test_packaged_and_historical_runtime_identity_must_match(self) -> None:
        report = json.loads(self.report_path.read_text(encoding="utf-8"))
        report["sourceRevision"] = "f" * 40
        self.report_path.write_bytes(_stable_output_bytes(report))
        with self.assertRaisesRegex(PromotionError, "pre-removal runtime"):
            self._promote()

    def test_duplicate_measurement_keys_are_rejected(self) -> None:
        original = self.measurement_path.read_text(encoding="utf-8")
        self.measurement_path.write_text(
            original.replace(
                '"schemaVersion": 2,',
                '"schemaVersion": 2,\n  "schemaVersion": 2,',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaises(PromotionError):
            self._promote()


if __name__ == "__main__":
    unittest.main()
