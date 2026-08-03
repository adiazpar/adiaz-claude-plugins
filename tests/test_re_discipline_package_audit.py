import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_package_audit import (
    is_allowlisted,
    parse_checksum_file,
    shared_asset_kind,
    validate_manifest_file,
)


class ReDisciplinePackageAuditTests(unittest.TestCase):

    def test_legacy_hook_allowance_is_enforcement_only(self) -> None:
        for hook in ("hooks/re-discipline-hook.ps1", "hooks/re-discipline-hook.sh"):
            self.assertTrue(is_allowlisted(hook, "campaign-narrative-file"))
            self.assertTrue(is_allowlisted(hook, "review-ledger-file"))
            self.assertFalse(is_allowlisted(hook, "checkpoint-skill"))
            self.assertFalse(is_allowlisted(hook, "standalone-truth-skill"))
            self.assertFalse(is_allowlisted(hook, "categorical-run-root"))
        self.assertFalse(is_allowlisted("hooks/unrelated.sh", "campaign-narrative-file"))
    def parse(self, body: str) -> dict[str, str]:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "SHA256SUMS"
            path.write_text(body, encoding="ascii")
            return parse_checksum_file(path)

    def test_checksum_parser_accepts_only_canonical_package_roots(self) -> None:
        digest = "a" * 64
        parsed = self.parse(
            f"{digest}  ../evals/conformance/cases.json\n"
            f"{digest}  ../models/manifest.json\n"
            f"{digest}  ../profiles/balanced-v1.json\n"
            f"{digest}  ../schemas/context-pack.schema.json\n"
            f"{digest}  manifest.json\n"
            f"{digest}  windows-amd64/re-discipline-knowledge.exe\n"
        )
        self.assertEqual(len(parsed), 6)

    def test_checksum_parser_rejects_escaping_or_noncanonical_paths(self) -> None:
        digest = "a" * 64
        for relative in (
            "../outside/file.json",
            "../../schemas/file.json",
            "./manifest.json",
            "windows-amd64//re-discipline-knowledge.exe",
            r"windows-amd64\re-discipline-knowledge.exe",
            "C:/re-discipline-knowledge.exe",
            "/re-discipline-knowledge",
            "*manifest.json",
        ):
            with self.subTest(relative=relative):
                with self.assertRaises(ValueError):
                    self.parse(f"{digest}  {relative}\n")

    def test_finding_eval_corpus_is_a_benchmark_asset(self) -> None:
        self.assertEqual(
            shared_asset_kind("evals/conformance/finding-cases.json"),
            "benchmark-cases",
        )

    def test_project_lane_evidence_assets_are_classified(self) -> None:
        self.assertEqual(
            shared_asset_kind("evals/conformance/project-lane-ablation.json"),
            "project-lane-ablation-measurement",
        )
        self.assertEqual(
            shared_asset_kind(
                "evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip"
            ),
            "lane-ablation-evidence-archive",
        )

    def test_manifest_file_validator_rejects_noncanonical_paths(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "payload").mkdir()
            body = b"runtime artifact"
            (root / "payload" / "file.bin").write_bytes(body)
            for relative in (
                "payload//file.bin",
                "payload/./file.bin",
                r"payload\file.bin",
                "../file.bin",
            ):
                with self.subTest(relative=relative):
                    violations: list[dict[str, str]] = []
                    result = validate_manifest_file(
                        root,
                        {
                            "path": relative,
                            "sha256": "sha256:" + "a" * 64,
                            "size": len(body),
                        },
                        "manifest.json",
                        violations,
                    )
                    self.assertIsNone(result)
                    self.assertEqual(violations[0]["kind"], "runtime-manifest-path")


if __name__ == "__main__":
    unittest.main()
