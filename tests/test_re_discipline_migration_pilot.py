import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests import re_discipline_migration_pilot as pilot
from tests.re_discipline_project_lane_ablation import validate_json_schema


class MigrationPilotTests(unittest.TestCase):
    def _git(self, repo: Path, *args: str) -> str:
        result = subprocess.run(
            ["git", "-C", str(repo), *args],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(
            result.returncode,
            0,
            result.stderr.decode("utf-8", errors="replace"),
        )
        return result.stdout.decode("utf-8", errors="strict").strip()

    def _repository(self, root: Path) -> tuple[Path, str]:
        repo = root / "repo"
        repo.mkdir()
        self._git(repo, "init")
        self._git(repo, "config", "user.name", "Fixture")
        self._git(repo, "config", "user.email", "fixture@example.invalid")
        (repo / "tracked.txt").write_text("tracked\n", encoding="utf-8")
        self._git(repo, "add", "tracked.txt")
        self._git(repo, "commit", "-m", "fixture")
        return repo, self._git(repo, "rev-parse", "HEAD")

    def _legacy_project(self, root: Path) -> tuple[Path, str]:
        repo = root / "project"
        repo.mkdir()
        self._git(repo, "init")
        self._git(repo, "config", "user.name", "Fixture")
        self._git(repo, "config", "user.email", "fixture@example.invalid")
        files = {
            ".re-discipline/project-profile.md": (
                "<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy\n"
            ),
            ".codex/AGENTS.md": (
                "<!-- re-discipline:codex-adapter v0.7.0 -->\nlegacy\n"
            ),
            "active/example/CAMPAIGN.md": "# Example\n",
        }
        for relative, body in files.items():
            path = repo / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(body, encoding="utf-8")
        self._git(repo, "add", ".")
        self._git(repo, "commit", "-m", "legacy fixture")
        return repo, self._git(repo, "rev-parse", "HEAD")

    def test_repository_binding_requires_exact_clean_full_revision(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repo, revision = self._repository(Path(directory))
            binding = pilot._repo_binding(repo, revision, label="fixture")
            self.assertTrue(binding["clean"])
            self.assertRegex(binding["trackedManifestSha256"], pilot.DIGEST_RE)
            with self.assertRaisesRegex(pilot.PilotError, "40-character"):
                pilot._repo_binding(repo, revision[:12], label="fixture")
            (repo / "untracked.txt").write_text("dirty\n", encoding="utf-8")
            with self.assertRaisesRegex(pilot.PilotError, "dirty"):
                pilot._repo_binding(repo, revision, label="fixture")

    def test_clone_reproduces_exact_source_working_bytes_and_refuses_existing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo, revision = self._repository(root)
            destination = root / "clone"
            result = pilot._clone_exact(
                repo, revision, destination, timeout_seconds=30
            )
            self.assertTrue(result["sourceWorkingBytesExact"])
            self.assertEqual(
                result["trackedManifestSha256"],
                pilot._repo_binding(repo, revision, label="fixture")[
                    "trackedManifestSha256"
                ],
            )
            with self.assertRaisesRegex(pilot.PilotError, "existing path"):
                pilot._clone_exact(
                    repo, revision, destination, timeout_seconds=30
                )

    def test_production_guard_rejects_marker_tamper_and_migration_state(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repo, revision = self._legacy_project(Path(directory))
            binding = pilot._repo_binding(repo, revision, label="project")
            guard = pilot._production_guard(repo, binding)
            self.assertTrue(guard["legacyStateConfirmed"])
            migration = repo / pilot.MIGRATION_ROOT
            migration.mkdir(parents=True)
            with self.assertRaisesRegex(pilot.PilotError, "migration state"):
                pilot._production_guard(repo, binding)
            migration.rmdir()
            (repo / ".codex/AGENTS.md").write_text("tampered\n", encoding="utf-8")
            with self.assertRaisesRegex(pilot.PilotError, "lacks"):
                pilot._production_guard(repo, binding)

    def test_capture_binds_failure_stdout_stderr_and_detects_tamper(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plugin = root / "plugin"
            project = root / "project"
            plugin.mkdir()
            project.mkdir()
            log = pilot.CaptureLog(
                root, plugin=plugin, project=project, timeout_seconds=30
            )
            capture, _, _ = log.run(
                name="missing-command",
                command=[str(root / "absent-executable")],
                cwd=project,
                expect_success=False,
                failure_class="mcp-unavailable",
            )
            self.assertEqual(capture["exitCode"], 127)
            self.assertEqual(capture["failure"]["class"], "mcp-unavailable")
            pilot._command_refs(root)
            stderr_path = root / capture["stderr"]["path"]
            stderr_path.write_bytes(stderr_path.read_bytes() + b"tamper")
            with self.assertRaisesRegex(pilot.PilotError, "digest or byte count"):
                pilot._command_refs(root)

    def _package_fixture(self, root: Path) -> tuple[Path, Path]:
        plugin = root / "plugin"
        bin_root = plugin / "plugins/re-discipline/knowledge/bin"
        bin_root.mkdir(parents=True)
        goos, goarch, executable = pilot._platform_target()
        target_path = bin_root / f"{goos}-{goarch}" / executable
        target_path.parent.mkdir()
        target_path.write_bytes(b"runtime")
        launcher_path = bin_root / "launcher"
        launcher_path.write_bytes(b"launcher")
        asset_path = bin_root.parent / "asset.dat"
        asset_path.write_bytes(b"asset")
        notice_path = bin_root / "NOTICE"
        notice_path.write_bytes(b"notice")

        def row(file_path: Path, **extra: object) -> dict[str, object]:
            body = file_path.read_bytes()
            return {
                "sha256": pilot._identity(body),
                "size": len(body),
                "mode": "0644",
                **extra,
            }

        manifest = {
            "schemaVersion": 1,
            "runtime": {
                "name": "re-discipline-knowledge",
                "version": "0.8.0",
                "buildId": pilot._identity(b"build"),
            },
            "targets": [
                row(
                    target_path,
                    kind="runtime",
                    goos=goos,
                    goarch=goarch,
                    path=f"{goos}-{goarch}/{executable}",
                )
            ],
            "launchers": [row(launcher_path, kind="dispatch", path="launcher")],
            "sharedAssets": [row(asset_path, kind="asset", path="asset.dat")],
            "notices": row(notice_path, kind="notice", path="NOTICE"),
        }
        (bin_root / "manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )
        sums = {
            f"{goos}-{goarch}/{executable}": pilot._identity(target_path.read_bytes()),
            "launcher": pilot._identity(launcher_path.read_bytes()),
            "../asset.dat": pilot._identity(asset_path.read_bytes()),
            "NOTICE": pilot._identity(notice_path.read_bytes()),
            "manifest.json": pilot._identity(
                (bin_root / "manifest.json").read_bytes()
            ),
        }
        (bin_root / "SHA256SUMS").write_text(
            "".join(
                f"{identity.removeprefix('sha256:')}  {path}\n"
                for path, identity in sums.items()
            ),
            encoding="utf-8",
        )
        return plugin, target_path

    def test_package_manifest_and_checksums_reject_tampered_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            plugin, runtime = self._package_fixture(Path(directory))
            result = pilot._verify_package(plugin)
            self.assertEqual(result["runtimeSha256"], pilot._identity(b"runtime"))
            runtime.write_bytes(b"tampered")
            with self.assertRaisesRegex(pilot.PilotError, "digest mismatches"):
                pilot._verify_package(plugin)

    def test_receipt_and_manager_approval_are_exact_digest_bound(self) -> None:
        value = {"phase": "fixture", "receiptDigest": ""}
        value["receiptDigest"] = pilot._canonical_digest(value)
        pilot._verify_receipt_digest(value)
        changed = dict(value)
        changed["phase"] = "tampered"
        with self.assertRaisesRegex(pilot.PilotError, "authenticate"):
            pilot._verify_receipt_digest(changed)
        digest = pilot._identity(b"plan")
        approval = {
            "schemaVersion": 1,
            "kind": "migration-plan-approval-v1",
            "pilot": "small",
            "liveCampaign": "prelude-pack-recalibration",
            "planDigest": digest,
            "authority": "manager",
            "reviewer": "maintainer",
            "rationale": "Reviewed exact regenerated plan.",
            "decidedAt": "2026-08-03T00:00:00Z",
            "explicitApproval": True,
        }
        pilot._validate_manager_approval(
            approval,
            kind="migration-plan-approval-v1",
            pilot="small",
            campaign="prelude-pack-recalibration",
            digest_field="planDigest",
            digest_value=digest,
        )
        approval["planDigest"] = pilot._identity(b"different")
        with self.assertRaisesRegex(pilot.PilotError, "incomplete"):
            pilot._validate_manager_approval(
                approval,
                kind="migration-plan-approval-v1",
                pilot="small",
                campaign="prelude-pack-recalibration",
                digest_field="planDigest",
                digest_value=digest,
            )

    def test_evidence_submission_rejects_escape_and_tamper(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            project = root / "disposable/project"
            project.mkdir(parents=True)
            gate_root = root / "manager-input/gates"
            gate_root.mkdir(parents=True)
            source = gate_root / "retrieval.json"
            source.write_text("{}\n", encoding="utf-8")
            state = {
                "transactionId": "M-EXAMPLE",
                "planDigest": pilot._identity(b"plan"),
            }
            receipt = {
                "pilot": "small",
                "liveCampaign": "prelude-pack-recalibration",
                "migrationState": state,
            }
            submission = {
                "schemaVersion": 1,
                "kind": "migration-pilot-evidence-submission-v1",
                "pilot": "small",
                "liveCampaign": "prelude-pack-recalibration",
                "transactionId": state["transactionId"],
                "planDigest": state["planDigest"],
                "authority": "manager",
                "reviewer": "maintainer",
                "rationale": "Reviewed exact evidence bundle.",
                "decidedAt": "2026-08-03T00:00:00Z",
                "copies": [
                    {
                        "sourcePath": "manager-input/gates/retrieval.json",
                        "destinationPath": "../escape.json",
                        "sha256": pilot._identity(source.read_bytes()),
                    },
                    {
                        "sourcePath": "manager-input/gates/retrieval.json",
                        "destinationPath": f"{pilot.MIGRATION_ROOT}/evidence/host.json",
                        "sha256": pilot._identity(source.read_bytes()),
                    },
                ],
                "gateArtifacts": {
                    "retrieval-context": "../escape.json",
                    "host-parity": f"{pilot.MIGRATION_ROOT}/evidence/host.json",
                },
            }
            pilot._write_json(gate_root / "evidence-submission.json", submission)
            with self.assertRaisesRegex(pilot.PilotError, "unsafe"):
                pilot._install_evidence_submission(
                    run_root=root, project_clone=project, receipt=receipt
                )

    def test_pilot_receipt_schema_accepts_sealed_common_contract(self) -> None:
        repository = {
            "revision": "1" * 40,
            "tree": "2" * 40,
            "trackedFileCount": 1,
            "trackedManifestSha256": pilot._identity(b"tracked"),
            "clean": True,
        }
        copy = {
            key: value for key, value in repository.items() if key != "clean"
        }
        copy["sourceWorkingBytesExact"] = True
        artifact = {
            "path": "artifacts/example.json",
            "sha256": pilot._identity(b"example"),
            "byteCount": 7,
        }
        receipt = {
            "$schema": "plugin://re-discipline/schemas/disposable-migration-pilot.schema.json",
            "schemaVersion": 1,
            "kind": pilot.PILOT_KIND,
            "receiptSequence": 1,
            "priorReceiptDigest": "",
            "updatedAt": "2026-08-03T00:00:00Z",
            "pilot": "small",
            "liveCampaign": "prelude-pack-recalibration",
            "phase": "manager-decisions-required",
            "createdAt": "2026-08-03T00:00:00Z",
            "repositories": {"plugin": repository, "project": repository},
            "disposableCopies": {"plugin": copy, "project": copy},
            "completeProjectTreeRetained": True,
            "liveCampaignIsCertificationScopeOnly": True,
            "productionBoundary": {},
            "package": {},
            "tools": {},
            "preview": {},
            "decisionRequest": artifact,
            "failureInjections": [{}, {}],
            "commands": [artifact],
            "receiptDigest": pilot._identity(b"receipt"),
        }
        schema_path = (
            Path(__file__).resolve().parents[1]
            / pilot.PILOT_SCHEMA
        )
        validate_json_schema(json.loads(schema_path.read_text(encoding="utf-8")), receipt)

    @unittest.skipUnless(
        os.environ.get("RE_DISCIPLINE_RUN_PILOT_INTEGRATION") == "1",
        "set RE_DISCIPLINE_RUN_PILOT_INTEGRATION=1 for the packaged disposable rehearsal",
    )
    def test_packaged_prepare_decisions_coverage_and_recovery_rehearsal(self) -> None:
        source_root = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plugin_repo = root / "plugin-source"
            shutil.copytree(
                source_root,
                plugin_repo,
                ignore=shutil.ignore_patterns(
                    ".git", "__pycache__", "*.pyc", ".pytest_cache"
                ),
            )
            knowledge_root = plugin_repo / pilot.ASSET_ROOT
            goos, goarch, executable = pilot._platform_target()
            runtime_path = knowledge_root / "bin" / f"{goos}-{goarch}" / executable
            runtime_path.parent.mkdir(parents=True, exist_ok=True)
            package = subprocess.run(
                [
                    "go",
                    "build",
                    "-trimpath",
                    "-o",
                    str(runtime_path),
                    "./cmd/re-discipline-knowledge",
                ],
                cwd=knowledge_root,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
                timeout=900,
            )
            self.assertEqual(
                package.returncode,
                0,
                package.stderr.decode("utf-8", errors="replace"),
            )
            launcher_path = knowledge_root / "bin" / "pilot-launcher"
            launcher_path.write_text("synthetic pilot launcher\n", encoding="utf-8")
            profile_path = knowledge_root / "profiles/balanced-v1.json"
            notice_path = knowledge_root / "THIRD_PARTY_NOTICES.md"

            def package_row(file_path: Path, **fields: object) -> dict[str, object]:
                body = file_path.read_bytes()
                return {
                    **fields,
                    "sha256": pilot._identity(body),
                    "size": len(body),
                    "mode": "0644",
                }

            manifest = {
                "schemaVersion": 1,
                "runtime": {
                    "name": "re-discipline-knowledge",
                    "version": "0.8.0",
                    "buildId": pilot._identity(b"synthetic-current-source"),
                },
                "targets": [
                    package_row(
                        runtime_path,
                        kind="runtime",
                        goos=goos,
                        goarch=goarch,
                        path=f"{goos}-{goarch}/{executable}",
                    )
                ],
                "launchers": [
                    package_row(
                        launcher_path,
                        kind="pilot-launcher",
                        path="pilot-launcher",
                    )
                ],
                "sharedAssets": [
                    package_row(
                        profile_path,
                        kind="retrieval-profile",
                        path="profiles/balanced-v1.json",
                    )
                ],
                "notices": package_row(
                    notice_path,
                    kind="third-party-notices",
                    path="THIRD_PARTY_NOTICES.md",
                ),
            }
            pilot._write_json(knowledge_root / "bin/manifest.json", manifest)
            sums = {
                f"{goos}-{goarch}/{executable}": pilot._identity(runtime_path.read_bytes()),
                "pilot-launcher": pilot._identity(launcher_path.read_bytes()),
                "../profiles/balanced-v1.json": pilot._identity(profile_path.read_bytes()),
                "../THIRD_PARTY_NOTICES.md": pilot._identity(notice_path.read_bytes()),
            }
            (knowledge_root / "bin/SHA256SUMS").write_text(
                "".join(
                    f"{identity.removeprefix('sha256:')}  {path}\n"
                    for path, identity in sums.items()
                ),
                encoding="utf-8",
            )
            self._git(plugin_repo, "init")
            self._git(plugin_repo, "config", "user.name", "Fixture")
            self._git(plugin_repo, "config", "user.email", "fixture@example.invalid")
            self._git(plugin_repo, "add", ".")
            self._git(plugin_repo, "commit", "-m", "plugin fixture")
            plugin_revision = self._git(plugin_repo, "rev-parse", "HEAD")

            project_repo = root / "project-source"
            project_repo.mkdir()
            self._git(project_repo, "init")
            self._git(project_repo, "config", "user.name", "Fixture")
            self._git(project_repo, "config", "user.email", "fixture@example.invalid")
            fixture_files = {
                ".gitignore": ".re-discipline/cache/\n.re-discipline/local-paths.md\n",
                ".re-discipline/project-profile.md": (
                    "# Fixture project\n\n"
                    "<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy laws\n"
                    "<!-- re-discipline:shared-laws:end -->\n\n"
                    "## Mission\n\nPreserve fixture truth.\n"
                ),
                ".re-discipline/config.json": json.dumps(
                    {
                        "schemaVersion": 2,
                        "knowledgeDirectory": "knowledge",
                        "memory": {
                            "mode": "shared-only",
                            "writePolicy": "proposal-only",
                        },
                        "knowledge": {
                            "enabled": True,
                            "profile": "plugin:balanced-v1",
                            "settingsFile": "knowledge/policy.jsonc",
                            "projectProfile": "knowledge/retrieval-profile.json",
                        },
                    }
                )
                + "\n",
                ".re-discipline/knowledge/policy.jsonc": (
                    '{"$schema":"plugin://re-discipline/schemas/knowledge-settings.schema.json",'
                    '"schemaVersion":1,"sources":{"truth":true,"history":false,'
                    '"backlog":true,"activeCampaigns":true,"sharedMemory":false,'
                    '"drafterReports":true,"additional":[]},"models":{"execution":"local"},'
                    '"telemetry":{"mode":"off"},"budgets":{"searchTokens":2048,'
                    '"managerContextTokens":4096,"drafterContextTokens":2048,'
                    '"maxPassages":9,"maxBytes":16384}}\n'
                ),
                ".re-discipline/agents/dispatch.ps1": "# legacy dispatcher\n",
                ".re-discipline/memory/topics/working-style.md": "# Retained shared memory\n",
                "AGENTS.md": (
                    "<!-- re-discipline:router v0.7.0 -->\nrouter\n"
                    "<!-- re-discipline:router:end -->\n"
                ),
                ".codex/AGENTS.md": (
                    "<!-- re-discipline:codex-adapter v0.7.0 -->\nlegacy\n"
                    "<!-- re-discipline:codex-adapter:end -->\n"
                ),
                ".codex/external-drafter-contract.md": (
                    "# External Drafter Contract\n\nUse `active/<slug>/subagents/` and `CAMPAIGN.md`.\n"
                ),
                ".claude/CLAUDE.md": (
                    "<!-- re-discipline:claude-adapter v0.7.0 -->\nlegacy\n"
                    "<!-- re-discipline:claude-adapter:end -->\n"
                ),
                "active/prelude-pack-recalibration/CAMPAIGN.md": (
                    "# Campaign: prelude-pack-recalibration\n\n"
                    "**Status:** OPEN\n\n## Objective\n\nRecalibrate the prelude pack.\n"
                ),
                "active/prelude-pack-recalibration/REVIEWS.md": (
                    "# Review ledger\n\n"
                    "| Date | Report | PROMOTE | HOLD | DROP | BLOCK | Promoted to |\n"
                    "|---|---|---:|---:|---:|---:|---|\n"
                    "| 2026-08-01 | `subagents/worker/report.md` | 0 | 1 | 0 | 0 | - |\n"
                ),
                "active/prelude-pack-recalibration/subagents/worker/brief.md": "# Brief\n",
                "active/prelude-pack-recalibration/subagents/worker/report.md": (
                    "**Review:** 2026-08-01 fixture-manager\n"
                    "**Disposition:** PROMOTE=0 HOLD=1 DROP=0 BLOCK=0\n\n"
                    "# VERDICT\n\nDIRECT: observed bounded migration behavior.\n"
                ),
                "active/resource-registration/CAMPAIGN.md": (
                    "# Campaign: resource-registration\n\n**Status:** OPEN\n"
                ),
                "docs/truth/claim.md": (
                    "# Fixture truth\n\n**Claim:** The fixture preserves accepted truth.\n\n"
                    "**Confidence:** Strong\n"
                ),
                "docs/history/chronicle.md": "# Chronicle\n",
                "docs/backlog/next.md": "# Next\n",
                "docs/INDEX.md": (
                    "# Project map\n\n"
                    "- [Live](../active/prelude-pack-recalibration/CAMPAIGN.md)\n"
                ),
                "src/ordinary.txt": "ordinary project file\n",
            }
            for relative, body in fixture_files.items():
                path = project_repo / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(body, encoding="utf-8")
            self._git(project_repo, "add", ".")
            self._git(project_repo, "commit", "-m", "legacy project fixture")
            project_revision = self._git(project_repo, "rev-parse", "HEAD")
            output = root / "pilot-output"

            prepared = pilot._prepare(
                plugin_source=plugin_repo,
                plugin_revision=plugin_revision,
                project_source=project_repo,
                project_revision=project_revision,
                run_root=output,
                pilot="small",
                timeout_seconds=900,
            )
            self.assertEqual(prepared["phase"], "manager-decisions-required")
            plan_digest = prepared["preview"]["planDigest"]
            manager = output / pilot.REVIEW_ROOT
            manager.mkdir()
            pilot._write_json(
                manager / "plan-approval.json",
                {
                    "schemaVersion": 1,
                    "kind": "migration-plan-approval-v1",
                    "pilot": "small",
                    "liveCampaign": "prelude-pack-recalibration",
                    "planDigest": plan_digest,
                    "authority": "manager",
                    "reviewer": "fixture-manager",
                    "rationale": "Reviewed the exact regenerated fixture plan.",
                    "decidedAt": "2026-08-03T00:00:00Z",
                    "explicitApproval": True,
                },
            )
            shadow = pilot._advance_decisions(
                plugin_source=plugin_repo,
                plugin_revision=plugin_revision,
                project_source=project_repo,
                project_revision=project_revision,
                run_root=output,
                pilot="small",
                timeout_seconds=900,
            )
            self.assertEqual(shadow["phase"], "coverage-review-required")
            coverage_request = pilot._read_json(
                pilot._verify_artifact_ref(
                    output, shadow["coverageRequest"], field="coverageRequest"
                )
            )
            coverage_root = manager / "coverage"
            coverage_root.mkdir()
            for report in coverage_request["reports"]:
                pilot._write_json(
                    coverage_root / f"{report['sourceDigest']}.json",
                    {
                        "sourcePath": report["sourcePath"],
                        "sourceDigest": report["sourceDigest"],
                        "complete": True,
                        "coverage": [
                            {
                                "sourceHandle": report["fullSpanHandle"],
                                "sourcePath": report["destinationPath"],
                                "sourceSha256": report["destinationDigest"],
                                "startLine": 1,
                                "endLine": report["sourceLineCount"],
                                "sourceLineCount": report["sourceLineCount"],
                                "disposition": "unresolved",
                                "rationale": (
                                    "Preserve the complete fixture span for explicit manager attention."
                                ),
                            }
                        ],
                        "findingIds": [],
                        "findings": [],
                        "reviewer": "fixture-manager",
                        "rationale": "Exhaustive unresolved fixture coverage.",
                        "digest": "",
                    },
                )
            reorganized = pilot._advance_coverage(
                plugin_source=plugin_repo,
                plugin_revision=plugin_revision,
                project_source=project_repo,
                project_revision=project_revision,
                run_root=output,
                pilot="small",
                timeout_seconds=900,
            )
            self.assertEqual(
                reorganized["phase"], "certification-evidence-required"
            )
            self.assertEqual(
                reorganized["migrationState"]["state"], "physically-reorganized"
            )
            pilot._verify_run(
                plugin_source=plugin_repo,
                plugin_revision=plugin_revision,
                project_source=project_repo,
                project_revision=project_revision,
                run_root=output,
                pilot="small",
                timeout_seconds=900,
            )


if __name__ == "__main__":
    unittest.main()
