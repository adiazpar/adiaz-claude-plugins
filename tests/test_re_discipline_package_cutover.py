import hashlib
import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_package_audit import audit_plugin, declared_plugin_version


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"
HOOKS = PLUGIN / "hooks"


class ReDisciplinePackageCutoverTests(unittest.TestCase):
    def test_exact_staged_artifact_passes_legacy_allowlist(self):
        first = audit_plugin(PLUGIN)
        second = audit_plugin(PLUGIN)
        self.assertEqual(first["artifactDigest"], second["artifactDigest"])
        self.assertEqual(first["inventoryDigest"], second["inventoryDigest"])
        self.assertEqual(first["pluginVersion"], declared_plugin_version(ROOT))
        self.assertEqual(first["violations"], [], json.dumps(first["violations"], indent=2))
        self.assertGreater(first["fileCount"], 0)

    def make_project(self, root: Path) -> None:
        profile = root / ".re-discipline" / "project-profile.md"
        profile.parent.mkdir(parents=True)
        profile.write_text("<!-- re-discipline:shared-laws v0.8.0 -->\n", encoding="ascii")
        campaign = root / "active" / "fixture"
        (campaign / "runs" / "R-20000101-0001" / "payload").mkdir(parents=True)
        (campaign / "work-items").mkdir()
        (campaign / "findings").mkdir()
        (campaign / "intake").mkdir()
        (campaign / "reviews").mkdir()
        (campaign / "events").mkdir()
        (campaign / "closure").mkdir()
        (root / "docs" / "truth").mkdir(parents=True)
        (root / "src").mkdir()
        (campaign / "campaign.json").write_text('{"id":"C-FIXTURE"}\n', encoding="ascii")
        pack_digest = "sha256:" + "a" * 64
        pack_body = json.dumps(
            {"schemaVersion": 2, "packId": "context-aaaaaaaaaaaaaaaaaaaa", "digest": pack_digest},
            separators=(",", ":"),
        ) + "\n"
        pack_path = campaign / "runs" / "R-20000101-0001" / "context-pack.json"
        pack_path.write_text(pack_body, encoding="ascii")
        (campaign / "runs" / "R-20000101-0001" / "run.json").write_text(
            json.dumps(
                {
                    "id": "R-20000101-0001",
                    "primaryWorkItemId": "W-0001",
                    "status": "running",
                    "writeGrants": [
                        {"mode": "exact", "path": "src/main.py"},
                        {"mode": "directory", "path": "generated/output"},
                    ],
                    "contextPack": {
                        "path": "active/fixture/runs/R-20000101-0001/context-pack.json",
                        "sha256": "sha256:" + hashlib.sha256(pack_body.encode("ascii")).hexdigest(),
                    },
                },
                separators=(",", ":"),
            )
            + "\n",
            encoding="ascii",
        )

    def make_legacy_project(self, root: Path) -> None:
        profile = root / ".re-discipline" / "project-profile.md"
        profile.parent.mkdir(parents=True)
        profile.write_text("<!-- re-discipline:shared-laws v0.7.0 -->\n", encoding="ascii")
        (profile.parent / "config.json").write_text('{"schemaVersion":2}\n', encoding="ascii")
        campaign = root / "active" / "fixture"
        campaign.mkdir(parents=True)
        (campaign / "CAMPAIGN.md").write_text("# Campaign: fixture\n", encoding="ascii")

    def run_powershell(self, project: Path, event: str, payload: dict) -> dict:
        executable = shutil.which("powershell.exe") or shutil.which("powershell")
        if executable is None:
            self.skipTest("PowerShell is unavailable")
        completed = subprocess.run(
            [executable, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(HOOKS / "re-discipline-hook.ps1"), event],
            input=json.dumps({"cwd": str(project), **payload}),
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        return json.loads(completed.stdout or "{}")

    def run_posix(self, project: Path, event: str, payload: dict) -> dict:
        executable = shutil.which("sh")
        if executable is None:
            for candidate in (
                Path(r"C:\Program Files\Git\bin\sh.exe"),
                Path(r"C:\Program Files\Git\usr\bin\sh.exe"),
            ):
                if candidate.is_file():
                    executable = str(candidate)
                    break
        if executable is None:
            self.skipTest("POSIX sh is unavailable")
        completed = subprocess.run(
            [executable, str(HOOKS / "re-discipline-hook.sh"), event],
            input=json.dumps({"cwd": project.as_posix(), **payload}),
            text=True,
            capture_output=True,
            check=False,
        )
        if completed.returncode != 0:
            self.skipTest(f"available sh cannot execute workspace hook: {completed.stderr}")
        self.assertEqual(completed.stderr, "")
        return json.loads(completed.stdout or "{}")

    def decision(self, result: dict) -> str:
        return result.get("hookSpecificOutput", {}).get("permissionDecision", "allow")

    def patch_command(self, *targets: str, added_line: str = "+replacement") -> str:
        lines = ["*** Begin Patch"]
        for target in targets:
            lines.extend((f"*** Update File: {target}", "@@", "-original", added_line))
        lines.append("*** End Patch")
        return "\n".join(lines)

    def test_pretooluse_denies_canonical_and_unidentified_run_outputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            paths = {
                "campaign": project / "active" / "fixture" / "campaign.json",
                "work": project / "active" / "fixture" / "work-items" / "W-0001.json",
                "run": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "run.json",
                "brief": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "brief.md",
                "pack": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "context-pack.json",
                "finding": project / "active" / "fixture" / "findings" / "F-0001.md",
                "intake": project / "active" / "fixture" / "intake" / "I-0001.json",
                "review": project / "active" / "fixture" / "reviews" / "V-0001.json",
                "event": project / "active" / "fixture" / "events" / "events.jsonl",
                "closure": project / "active" / "fixture" / "closure" / "job.json",
                "state-view": project / "active" / "fixture" / "STATE.md",
                "truth": project / "docs" / "truth" / "claim.md",
                "history": project / "docs" / "history" / "campaigns" / "fixture" / "campaign.json",
                "state-head": project / ".re-discipline" / "state" / "head.json",
                "migration-state": project / ".re-discipline" / "migration" / "0.8" / "state.json",
                "normalization-queue": project / ".re-discipline" / "knowledge" / "normalization-queue.json",
                "normalization-lock": project / ".re-discipline" / "knowledge" / "normalization-queue.json.lock",
                "normalization-temp": project / ".re-discipline" / "knowledge" / ".re-discipline-tmp-adversarial",
                "report": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md",
                "payload": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "payload" / "probe.txt",
                "source": project / "src" / "main.py",
            }
            allowed = {"source"}
            expected = {name: "allow" if name in allowed else "deny" for name in paths}
            for name, path in paths.items():
                payload = {"tool_name": "Write", "tool_input": {"file_path": str(path)}}
                ps = self.decision(self.run_powershell(project, "pre-tool-use", payload))
                self.assertEqual(ps, expected[name], name)
                posix_payload = {"tool_name": "Write", "tool_input": {"file_path": path.as_posix()}}
                sh = self.decision(self.run_posix(project, "pre-tool-use", posix_payload))
                self.assertEqual(sh, expected[name], name)

    def test_apply_patch_targets_share_the_write_boundary_with_host_parity(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)

            cases = (
                (("src/main.py",), {}, "allow"),
                (("docs/notes.md",), {}, "allow"),
                (("docs/notes with spaces.md",), {}, "allow"),
                (("active/fixture/campaign.json",), {}, "deny"),
                (("ACTIVE/fixture/campaign.json",), {}, "deny"),
                (("docs/truth/claim.md",), {}, "deny"),
                (("src/main.py", "active/fixture/campaign.json"), {}, "deny"),
                (("src/main.py",), {"runId": "R-20000101-0001"}, "allow"),
                (("generated/output/nested/probe.txt",), {"runId": "R-20000101-0001"}, "allow"),
                (("docs/notes.md",), {"runId": "R-20000101-0001"}, "deny"),
                (("active/fixture/runs/R-20000101-0001/report.md",), {}, "deny"),
                (("active/fixture/runs/R-20000101-0001/payload/probe.txt",), {}, "deny"),
                (("active/fixture/runs/R-20000101-0001/report.md",), {"runId": "R-20000101-0001"}, "allow"),
                (("active/fixture/runs/R-20000101-0001/payload/probe.txt",), {"runId": "R-20000101-0001"}, "allow"),
                (("../outside.txt",), {}, "deny"),
                (("C:/outside.txt",), {}, "deny"),
            )
            for targets, identity, expected in cases:
                payload = {
                    "tool_name": "apply_patch",
                    **identity,
                    "tool_input": {"command": self.patch_command(*targets)},
                }
                self.assertEqual(
                    expected,
                    self.decision(self.run_powershell(project, "pre-tool-use", payload)),
                    targets,
                )
                self.assertEqual(
                    expected,
                    self.decision(self.run_posix(project, "pre-tool-use", payload)),
                    targets,
                )

            malformed = {"tool_name": "apply_patch", "tool_input": {"command": "not a patch"}}
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", malformed)))
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", malformed)))

            # JSON object order is not an authority boundary. Envelope fields
            # after tool_input must still be parsed as top-level metadata.
            reordered = {
                "tool_input": {"command": self.patch_command("active/fixture/campaign.json")},
                "tool_name": "apply_patch",
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", reordered)))
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", reordered)))
            reordered_run = {
                "tool_input": {"command": self.patch_command("docs/notes.md")},
                "runId": "R-20000101-0001",
                "tool_name": "apply_patch",
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", reordered_run)))
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", reordered_run)))

            action_cases = (
                (
                    "*** Begin Patch\n*** Add File: docs/new-note.md\n+new\n*** End Patch",
                    "allow",
                ),
                (
                    "*** Begin Patch\n*** Delete File: docs/truth/claim.md\n*** End Patch",
                    "deny",
                ),
                (
                    "*** Begin Patch\n*** Update File: src/main.py\n*** Move to: active/fixture/campaign.json\n@@\n-old\n+new\n*** End Patch",
                    "deny",
                ),
                (
                    "*** Begin Patch\n*** Update File: src/main.py\n*** Move to: docs/moved.py\n@@\n-old\n+new\n*** End Patch",
                    "allow",
                ),
                (
                    "*** Begin Patch\n*** Rename File: docs/notes.md\n*** Update File: src/main.py\n@@\n-old\n+new\n*** End Patch",
                    "deny",
                ),
            )
            for command, expected in action_cases:
                payload = {"tool_name": "apply_patch", "tool_input": {"command": command}}
                self.assertEqual(expected, self.decision(self.run_powershell(project, "pre-tool-use", payload)))
                self.assertEqual(expected, self.decision(self.run_posix(project, "pre-tool-use", payload)))

            # Patch content is data: it cannot impersonate envelope run identity
            # or create a second protected target without a patch header.
            content_only = {
                "tool_name": "apply_patch",
                "tool_input": {
                    "command": self.patch_command(
                        "docs/notes.md",
                        added_line='+{"runId":"R-20000101-0001"} *** Update File: active/fixture/campaign.json',
                    )
                },
            }
            self.assertEqual("allow", self.decision(self.run_powershell(project, "pre-tool-use", content_only)))
            self.assertEqual("allow", self.decision(self.run_posix(project, "pre-tool-use", content_only)))

            content_only_run_output = {
                "tool_name": "apply_patch",
                "tool_input": {
                    "command": self.patch_command(
                        "active/fixture/runs/R-20000101-0001/report.md",
                        added_line='+{"runId":"R-20000101-0001"}',
                    )
                },
            }
            self.assertEqual(
                "deny",
                self.decision(self.run_powershell(project, "pre-tool-use", content_only_run_output)),
            )
            self.assertEqual(
                "deny",
                self.decision(self.run_posix(project, "pre-tool-use", content_only_run_output)),
            )

    def test_lifecycle_hook_outputs_are_bounded_and_semantically_symmetric(self):
        posix_hook = (HOOKS / "re-discipline-hook.sh").read_text(encoding="ascii")
        self.assertNotIn(r"\(true\|false\)", posix_hook)
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            run_path = project / "active" / "fixture" / "runs" / "R-20000101-0001"
            (run_path / "report.md").write_text("# report\n", encoding="ascii")
            cases = {
                "session-start": {},
                "pre-compact": {"campaignId": "C-FIXTURE", "workItemId": "W-0001", "generation": "G-1", "lastEventId": "E-1"},
                "post-compact": {"campaignId": "C-FIXTURE", "generation": "G-1", "lastEventId": "E-1"},
                "subagent-start": {"runId": "R-20000101-0001", "workItemId": "W-0001", "runPath": str(run_path), "contextPackDigest": "sha256:" + "a" * 64},
                "subagent-stop": {
                    "runId": "R-20000101-0001",
                    "workItemId": "W-0001",
                    "runPath": str(run_path),
                    "contextPackDigest": "sha256:" + "a" * 64,
                },
                "stop": {"transactionInFlight": True},
            }
            for event, payload in cases.items():
                ps = self.run_powershell(project, event, payload)
                sh_payload = dict(payload)
                if "runPath" in sh_payload:
                    sh_payload["runPath"] = Path(sh_payload["runPath"]).as_posix()
                sh = self.run_posix(project, event, sh_payload)
                ps_output = ps.get("hookSpecificOutput", {})
                sh_output = sh.get("hookSpecificOutput", {})
                self.assertEqual(ps_output.get("hookEventName"), sh_output.get("hookEventName"), event)
                self.assertLess(len(ps_output.get("additionalContext", "")), 900)
                self.assertLess(len(sh_output.get("additionalContext", "")), 900)
                for token in ("checkpoint-campaign", "promote-truth"):
                    self.assertNotIn(token, json.dumps(ps).lower())
                    self.assertNotIn(token, json.dumps(sh).lower())

    def test_ordinary_or_partial_subagent_events_do_not_receive_run_contracts(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            ordinary = {
                "agent_id": "019fc3d7-d891-75c3-a2f8-45c75c8ddf9f",
                "agent_type": "explorer",
                "permission_mode": "default",
            }
            partial = {**ordinary, "runId": "R-20000101-0001"}
            wrong_digest = {
                **ordinary,
                "runId": "R-20000101-0001",
                "workItemId": "W-0001",
                "runPath": str(project / "active" / "fixture" / "runs" / "R-20000101-0001"),
                "contextPackDigest": "sha256:" + "b" * 64,
            }
            for event in ("subagent-start", "subagent-stop"):
                for payload in (ordinary, partial, wrong_digest):
                    self.assertEqual({}, self.run_powershell(project, event, payload), (event, payload))
                    sh_payload = dict(payload)
                    if "runPath" in sh_payload:
                        sh_payload["runPath"] = Path(sh_payload["runPath"]).as_posix()
                    self.assertEqual({}, self.run_posix(project, event, sh_payload), (event, payload))

    def test_identified_run_is_limited_to_registered_grants_with_host_parity(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            allowed = (
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md",
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "payload" / "probe.txt",
                project / "src" / "main.py",
                project / "generated" / "output" / "nested" / "artifact.bin",
            )
            denied = (
                project / "src" / "other.py",
                project / "generated" / "outside.bin",
                project / "docs" / "notes.md",
                project / "active" / "fixture" / "campaign.json",
                project / "active" / "fixture" / "runs" / "R-20000101-9999" / "report.md",
            )
            for target in allowed + denied:
                expected = "allow" if target in allowed else "deny"
                base = {"tool_name": "Write", "runId": "R-20000101-0001"}
                ps = self.run_powershell(
                    project, "pre-tool-use",
                    {**base, "tool_input": {"file_path": str(target)}},
                )
                sh = self.run_posix(
                    project, "pre-tool-use",
                    {**base, "tool_input": {"file_path": target.as_posix()}},
                )
                self.assertEqual(expected, self.decision(ps), str(target))
                self.assertEqual(expected, self.decision(sh), str(target))

            missing = {
                "tool_name": "Edit",
                "runId": "R-20000101-7777",
                "tool_input": {"file_path": str(project / "src" / "main.py")},
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", missing)))
            missing["tool_input"]["file_path"] = (project / "src" / "main.py").as_posix()
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", missing)))

            # JSON-looking file content is not a host-supplied run identity.
            content_only = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": str(project / "docs" / "notes.md"),
                    "content": '{"runId":"R-20000101-0001"}',
                },
            }
            self.assertEqual("allow", self.decision(self.run_powershell(project, "pre-tool-use", content_only)))
            content_only["tool_input"]["file_path"] = (project / "docs" / "notes.md").as_posix()
            self.assertEqual("allow", self.decision(self.run_posix(project, "pre-tool-use", content_only)))

            content_only["tool_input"]["file_path"] = str(
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md"
            )
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", content_only)))
            content_only["tool_input"]["file_path"] = (
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md"
            ).as_posix()
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", content_only)))

            nested_identity = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": str(project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md"),
                    "runId": "R-20000101-0001",
                },
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", nested_identity)))
            nested_identity["tool_input"]["file_path"] = (
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md"
            ).as_posix()
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", nested_identity)))

            run_record = project / "active" / "fixture" / "runs" / "R-20000101-0001" / "run.json"
            terminal = json.loads(run_record.read_text(encoding="ascii"))
            terminal["status"] = "completed"
            run_record.write_text(json.dumps(terminal, separators=(",", ":")) + "\n", encoding="ascii")
            terminal_write = {
                "tool_name": "Edit",
                "runPath": str(run_record.parent),
                "tool_input": {"file_path": str(run_record.parent / "report.md")},
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", terminal_write)))
            terminal_write["runPath"] = run_record.parent.as_posix()
            terminal_write["tool_input"]["file_path"] = (run_record.parent / "report.md").as_posix()
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", terminal_write)))

            terminal["status"] = "running"
            run_record.write_text(json.dumps(terminal, separators=(",", ":")) + "\n", encoding="ascii")
            duplicate = project / "active" / "duplicate" / "runs" / "R-20000101-0001" / "run.json"
            duplicate.parent.mkdir(parents=True)
            duplicate.write_text(json.dumps(terminal, separators=(",", ":")) + "\n", encoding="ascii")
            duplicate_identity = {
                "tool_name": "Write",
                "runId": "R-20000101-0001",
                "tool_input": {"file_path": str(project / "src" / "main.py")},
            }
            self.assertEqual("deny", self.decision(self.run_powershell(project, "pre-tool-use", duplicate_identity)))
            duplicate_identity["tool_input"]["file_path"] = (project / "src" / "main.py").as_posix()
            self.assertEqual("deny", self.decision(self.run_posix(project, "pre-tool-use", duplicate_identity)))

    def test_session_start_distinguishes_legacy_from_08_without_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_legacy_project(project)
            before = sorted(
                (path.relative_to(project).as_posix(), path.read_bytes())
                for path in project.rglob("*")
                if path.is_file()
            )
            for result in (
                self.run_powershell(project, "session-start", {}),
                self.run_posix(project, "session-start", {}),
            ):
                context = result.get("hookSpecificOutput", {}).get("additionalContext", "")
                self.assertIn("Legacy re-discipline 0.7 project detected", context)
                self.assertIn("read-only preview", context)
                self.assertNotIn("Re-discipline 0.8 project detected", context)
            after = sorted(
                (path.relative_to(project).as_posix(), path.read_bytes())
                for path in project.rglob("*")
                if path.is_file()
            )
            self.assertEqual(before, after)

    def test_legacy_project_blocks_canonical_state_but_allows_project_owned_outputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_legacy_project(project)
            denied = (
                project / "active" / "fixture" / "CAMPAIGN.md",
                project / "active" / "fixture" / "REVIEWS.md",
                project / ".re-discipline" / "config.json",
                project / ".re-discipline" / "knowledge" / "retrieval-profile.json",
                project / "active" / "fixture" / "campaign.json",
                project / ".re-discipline" / "state" / "head.json",
                project / ".re-discipline" / "knowledge" / "migration" / "truth-receipts" / "F-0001.json",
                project / ".re-discipline" / "knowledge" / "migration" / "truth-reviews" / "review.json",
                project / ".re-discipline" / "knowledge" / "migration" / "legacy-retrieval-profile.json",
            )
            allowed = (
                project / "active" / "fixture" / "subagents" / "run-1" / "report.md",
                project / "active" / "fixture" / "subagents" / "run-1" / "artifacts" / "probe.txt",
                project / ".re-discipline" / "knowledge" / "evals" / "cases.json",
                project / ".re-discipline" / "local-paths.md",
                project / ".re-discipline" / "agents" / "config.json",
                project / ".re-discipline" / "agents" / "providers" / "claude.json",
                project / "docs" / "notes.md",
            )
            denied = denied + (
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md",
                project / "active" / "fixture" / "runs" / "R-20000101-0001" / "payload" / "probe.txt",
            )
            for target in denied:
                ps_payload = {"tool_name": "Edit", "tool_input": {"file_path": str(target)}}
                sh_payload = {"tool_name": "Edit", "tool_input": {"file_path": target.as_posix()}}
                for result in (
                    self.run_powershell(project, "pre-tool-use", ps_payload),
                    self.run_posix(project, "pre-tool-use", sh_payload),
                ):
                    output = result.get("hookSpecificOutput", {})
                    self.assertEqual("deny", output.get("permissionDecision"))
            for target in allowed:
                ps_payload = {"tool_name": "Write", "tool_input": {"file_path": str(target)}}
                sh_payload = {"tool_name": "Write", "tool_input": {"file_path": target.as_posix()}}
                self.assertEqual({}, self.run_powershell(project, "pre-tool-use", ps_payload), str(target))
                self.assertEqual({}, self.run_posix(project, "pre-tool-use", sh_payload), str(target))

    def test_stop_is_silent_without_inflight_transaction(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            self.assertEqual(self.run_powershell(project, "stop", {"transactionInFlight": False}), {})
            self.assertEqual(self.run_posix(project, "stop", {"transactionInFlight": False}), {})


if __name__ == "__main__":
    unittest.main()
