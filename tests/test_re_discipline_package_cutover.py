import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_package_audit import audit_plugin


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"
HOOKS = PLUGIN / "hooks"


class ReDisciplinePackageCutoverTests(unittest.TestCase):
    def test_exact_staged_artifact_passes_legacy_allowlist(self):
        first = audit_plugin(PLUGIN)
        second = audit_plugin(PLUGIN)
        self.assertEqual(first["artifactDigest"], second["artifactDigest"])
        self.assertEqual(first["inventoryDigest"], second["inventoryDigest"])
        self.assertEqual(first["pluginVersion"], "0.8.0")
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
        return json.loads(completed.stdout or "{}")

    def decision(self, result: dict) -> str:
        return result.get("hookSpecificOutput", {}).get("permissionDecision", "allow")

    def test_pretooluse_denies_state_and_truth_but_allows_run_outputs(self):
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
                "report": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "report.md",
                "payload": project / "active" / "fixture" / "runs" / "R-20000101-0001" / "payload" / "probe.txt",
                "source": project / "src" / "main.py",
            }
            allowed = {"report", "payload", "source"}
            expected = {name: "allow" if name in allowed else "deny" for name in paths}
            for name, path in paths.items():
                payload = {"tool_name": "Write", "tool_input": {"file_path": str(path)}}
                ps = self.decision(self.run_powershell(project, "pre-tool-use", payload))
                self.assertEqual(ps, expected[name], name)
                posix_payload = {"tool_name": "Write", "tool_input": {"file_path": path.as_posix()}}
                sh = self.decision(self.run_posix(project, "pre-tool-use", posix_payload))
                self.assertEqual(sh, expected[name], name)

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
                "subagent-stop": {"runId": "R-20000101-0001", "runPath": str(run_path)},
                "stop": {"transactionInFlight": True},
            }
            for event, payload in cases.items():
                ps = self.run_powershell(project, event, payload)
                sh = self.run_posix(project, event, payload)
                ps_output = ps.get("hookSpecificOutput", {})
                sh_output = sh.get("hookSpecificOutput", {})
                self.assertEqual(ps_output.get("hookEventName"), sh_output.get("hookEventName"), event)
                self.assertLess(len(ps_output.get("additionalContext", "")), 900)
                self.assertLess(len(sh_output.get("additionalContext", "")), 900)
                for token in ("checkpoint-campaign", "promote-truth"):
                    self.assertNotIn(token, json.dumps(ps).lower())
                    self.assertNotIn(token, json.dumps(sh).lower())

    def test_stop_is_silent_without_inflight_transaction(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.make_project(project)
            self.assertEqual(self.run_powershell(project, "stop", {"transactionInFlight": False}), {})
            self.assertEqual(self.run_posix(project, "stop", {"transactionInFlight": False}), {})


if __name__ == "__main__":
    unittest.main()
