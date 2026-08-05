import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"
GUARD = ROOT / ".github" / "scripts" / "re-discipline-version-guard.py"

VERSION_MANIFESTS = (
    ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json",
)


def git(args, cwd):
    return subprocess.run(
        [
            "git",
            "-c",
            "user.email=test@example.com",
            "-c",
            "user.name=test",
            "-c",
            "commit.gpgsign=false",
        ]
        + args,
        cwd=cwd,
        capture_output=True,
        text=True,
        check=True,
    )


def run_guard(cwd, base=""):
    return subprocess.run(
        [sys.executable, str(GUARD), "--base", base],
        cwd=cwd,
        capture_output=True,
        text=True,
        check=False,
    )


def write_manifest(plugin_dir: Path, version: str):
    for rel in VERSION_MANIFESTS:
        path = plugin_dir / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps({"name": "re-discipline", "version": version}, indent=2)
            + "\n",
            encoding="utf-8",
        )


class VersionGuardRepoInvariants(unittest.TestCase):
    def test_guard_script_exists(self):
        self.assertTrue(GUARD.is_file(), f"missing guard script: {GUARD}")

    def test_manifests_declare_the_same_version(self):
        seen = {}
        for rel in VERSION_MANIFESTS:
            path = PLUGIN / rel
            if not path.is_file():
                continue
            seen[rel] = json.loads(path.read_text(encoding="utf-8"))["version"]
        self.assertTrue(seen, "no plugin manifest declares a version")
        self.assertEqual(
            len(set(seen.values())),
            1,
            f"plugin manifests disagree on version: {seen}",
        )

    def test_workflow_invokes_the_guard(self):
        workflow = (
            ROOT / ".github" / "workflows" / "re-discipline.yml"
        ).read_text(encoding="utf-8")
        self.assertIn("re-discipline-version-guard.py", workflow)
        self.assertIn("fetch-depth: 0", workflow)


class VersionGuardBehavior(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.repo = Path(self._tmp.name)
        self.plugin = self.repo / "plugins" / "re-discipline"
        git(["init", "-b", "main"], self.repo)
        write_manifest(self.plugin, "0.8.0")
        (self.plugin / "payload.txt").write_text("one\n", encoding="utf-8")
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "base"], self.repo)
        self.base = git(
            ["rev-parse", "HEAD"], self.repo
        ).stdout.strip()

    def tearDown(self):
        self._tmp.cleanup()

    def test_unchanged_content_passes(self):
        result = run_guard(self.repo, self.base)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no packaged change", result.stdout)

    def test_changed_content_without_bump_fails(self):
        (self.plugin / "payload.txt").write_text("two\n", encoding="utf-8")
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "change without bump"], self.repo)

        result = run_guard(self.repo, self.base)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("still 0.8.0", result.stdout)

    def test_changed_content_with_bump_passes(self):
        (self.plugin / "payload.txt").write_text("two\n", encoding="utf-8")
        write_manifest(self.plugin, "0.8.1")
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "change with bump"], self.repo)

        result = run_guard(self.repo, self.base)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("0.8.0 -> 0.8.1", result.stdout)

    def test_version_may_not_move_backwards(self):
        (self.plugin / "payload.txt").write_text("two\n", encoding="utf-8")
        write_manifest(self.plugin, "0.7.9")
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "regress"], self.repo)

        result = run_guard(self.repo, self.base)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("backwards", result.stdout)

    def test_manifests_disagreeing_fails(self):
        (self.plugin / ".codex-plugin" / "plugin.json").write_text(
            json.dumps({"name": "re-discipline", "version": "0.9.0"}, indent=2)
            + "\n",
            encoding="utf-8",
        )
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "drift"], self.repo)

        result = run_guard(self.repo, self.base)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("different versions", result.stdout)

    def test_missing_base_skips_the_comparison(self):
        (self.plugin / "payload.txt").write_text("two\n", encoding="utf-8")
        git(["add", "-A"], self.repo)
        git(["commit", "-m", "change without bump"], self.repo)

        # A new branch push reports an all-zero "before" sha.
        result = run_guard(self.repo, "0" * 40)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("skipping the bump check", result.stdout)


if __name__ == "__main__":
    unittest.main()
