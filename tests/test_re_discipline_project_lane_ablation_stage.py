import copy
import hashlib
import json
import shutil
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_project_lane_ablation_stage import (
    HARNESS_SCHEMA,
    HARNESS_SCRIPT,
    StagingError,
    _directory_manifest,
    _stable_json_bytes,
    stage,
    verify_stage,
)
from tests.test_re_discipline_project_lane_ablation_build import SyntheticInputs


ROOT = Path(__file__).resolve().parents[1]


def _write(path: Path, body: bytes | str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(body.encode("utf-8") if isinstance(body, str) else body)


def _git(repo: Path, *arguments: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *arguments],
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        raise AssertionError(result.stderr)
    return result.stdout.strip()


def _commit(repo: Path) -> str:
    _git(repo, "init")
    _git(repo, "config", "user.email", "fixture@example.invalid")
    _git(repo, "config", "user.name", "Fixture")
    _git(repo, "config", "core.autocrlf", "false")
    _git(repo, "add", "--all")
    _git(repo, "commit", "-m", "fixture")
    return _git(repo, "rev-parse", "HEAD")


class StagingFixture:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.synthetic = SyntheticInputs(root / "synthetic")
        self.plugin = root / "plugin"
        self.project = root / "project"
        self.output = root / "stage"
        self._write_plugin()
        self.plugin_revision = _commit(self.plugin)
        self._write_project()
        self.project_revision = _commit(self.project)

    def _write_plugin(self) -> None:
        _write(
            self.plugin / HARNESS_SCRIPT,
            (ROOT / HARNESS_SCRIPT).read_bytes(),
        )
        _write(
            self.plugin / HARNESS_SCHEMA,
            (ROOT / HARNESS_SCHEMA).read_bytes(),
        )
        profile = self.synthetic.profile_catalog_path.read_bytes()
        _write(
            self.plugin
            / "plugins/re-discipline/templates/project/retrieval-profile.json",
            profile,
        )
        _write(
            self.plugin / "plugins/re-discipline/knowledge/profiles/balanced-v1.json",
            profile,
        )
        _write(
            self.plugin
            / "plugins/re-discipline/knowledge/internal/knowledge/migration_templates/balanced-v1.json",
            profile,
        )
        _write(
            self.plugin / "plugins/re-discipline/knowledge/models/manifest.json",
            self.synthetic.model_manifest_path.read_bytes(),
        )
        _write(
            self.plugin / "plugins/re-discipline/templates/project/config.json",
            '{"schemaVersion":3,"knowledge":{"enabled":true}}\n',
        )
        _write(
            self.plugin / "plugins/re-discipline/templates/project/policy.jsonc",
            '{"schemaVersion":1,"sources":{}}\n',
        )
        _write(
            self.plugin / "plugins/re-discipline/knowledge/go.mod",
            "module fixture.invalid/re-discipline\n\ngo 1.22\n",
        )

    def _write_project(self) -> None:
        _write(self.project / "active/fixture/CAMPAIGN.md", "# Active fixture\n")
        _write(self.project / "docs/INDEX.md", "# Fixture index\n")
        for index in range(64):
            _write(
                self.project / f"docs/truth/target-{index:02d}.md",
                f"# Target {index:02d}\n\nEvidence {index:02d}.\n",
            )
        _write(
            self.project / ".re-discipline/project-profile.md",
            "# Fixture project profile\n",
        )
        _write(self.project / ".re-discipline/config.json", "{\"schemaVersion\":2}\n")
        _write(
            self.project / ".re-discipline/knowledge/policy.jsonc",
            "{\"schemaVersion\":0}\n",
        )
        _write(
            self.project / ".re-discipline/knowledge/retrieval-profile.json",
            "{\"schemaVersion\":0}\n",
        )
        shutil.copytree(
            self.synthetic.final_eval_root,
            self.project / ".re-discipline/knowledge/evals",
        )

    def runner(
        self, command: list[str], cwd: Path, timeout_seconds: int
    ) -> subprocess.CompletedProcess[bytes]:
        del cwd, timeout_seconds
        project = Path(command[command.index("--project-root") + 1])
        cache = Path(command[command.index("--cache-root") + 1])
        raw = json.loads(self.synthetic.raw_path.read_text(encoding="utf-8"))
        generation = raw["generation"]
        generation.update(
            {
                "project": "fixture-project",
                "worktree": str(project),
                "gitRevision": self.project_revision,
            }
        )
        indexed_paths = [
            ".re-discipline/project-profile.md",
            "docs/INDEX.md",
            *[f"docs/truth/target-{index:02d}.md" for index in range(64)],
        ]
        generation["documentCount"] = len(indexed_paths)
        generation["chunkCount"] = len(indexed_paths)
        report = cache / "benchmarks" / raw["runId"] / "report.json"
        raw["reportPath"] = str(report)
        database = cache / "generations" / f"{generation['id']}.sqlite"
        database.parent.mkdir(parents=True, exist_ok=True)
        connection = sqlite3.connect(database)
        try:
            connection.execute(
                "CREATE TABLE documents ("
                "path TEXT PRIMARY KEY, tier TEXT NOT NULL, "
                "content_hash TEXT NOT NULL, size INTEGER NOT NULL, "
                "source_kind TEXT NOT NULL)"
            )
            for relative in indexed_paths:
                body = (project / Path(*relative.split("/"))).read_bytes()
                tier = (
                    "profile"
                    if relative.startswith(".re-discipline/")
                    else "navigation"
                    if relative == "docs/INDEX.md"
                    else "truth"
                )
                connection.execute(
                    "INSERT INTO documents VALUES (?,?,?,?,?)",
                    (
                        relative,
                        tier,
                        hashlib.sha256(body).hexdigest(),
                        len(body),
                        "",
                    ),
                )
            connection.commit()
        finally:
            connection.close()
        pointer = copy.deepcopy(generation)
        pointer["database"] = str(database)
        _write(cache / "current.json", _stable_json_bytes(pointer))
        body = _stable_json_bytes(raw)
        _write(report, body)
        return subprocess.CompletedProcess(
            command,
            1,
            stdout=body,
            stderr=b"project benchmark gates failed\nexit status 1\n",
        )

    def stage(self) -> dict[str, object]:
        return stage(
            plugin_repository=self.plugin,
            plugin_revision=self.plugin_revision,
            project_repository=self.project,
            project_revision=self.project_revision,
            output_root=self.output,
            runner=self.runner,
            timeout_seconds=30,
        )

    def verify(self) -> dict[str, object]:
        return verify_stage(
            plugin_repository=self.plugin,
            plugin_revision=self.plugin_revision,
            project_repository=self.project,
            project_revision=self.project_revision,
            output_root=self.output,
        )


class ProjectLaneAblationStageTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.fixture = StagingFixture(Path(self.temporary.name))

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_success_is_pre_migration_and_verification_is_read_only_replay(self) -> None:
        receipt = self.fixture.stage()
        self.assertFalse(receipt["sourceRepositoryMutated"])
        self.assertEqual(receipt["runtime"]["armCount"], 2)
        self.assertEqual(receipt["runtime"]["casesPerArm"], 64)
        self.assertEqual(
            [row["name"] for row in receipt["negativeControls"]],
            [
                "dirty-source-repository",
                "projection-byte-tamper",
                "indexed-source-byte-tamper",
            ],
        )
        self.assertTrue(all(row["observed"] for row in receipt["negativeControls"]))
        for relative in receipt["migrationGuard"]["paths"]:
            self.assertFalse((self.fixture.output / "disposable/project" / relative).exists())
        before = _directory_manifest(self.fixture.output)
        first = self.fixture.verify()
        second = self.fixture.verify()
        after = _directory_manifest(self.fixture.output)
        self.assertEqual(first, second)
        self.assertEqual(before, after)

    def test_dirty_and_revision_mismatched_inputs_are_rejected_before_output(self) -> None:
        _write(self.fixture.project / "untracked.md", "dirty\n")
        with self.assertRaisesRegex(StagingError, "dirty"):
            self.fixture.stage()
        self.assertFalse(self.fixture.output.exists())
        (self.fixture.project / "untracked.md").unlink()
        with self.assertRaisesRegex(StagingError, "HEAD is"):
            stage(
                plugin_repository=self.fixture.plugin,
                plugin_revision="0" * 40,
                project_repository=self.fixture.project,
                project_revision=self.fixture.project_revision,
                output_root=self.fixture.output,
                runner=self.fixture.runner,
            )
        self.assertFalse(self.fixture.output.exists())

    def test_preexisting_migration_layout_is_rejected(self) -> None:
        _write(
            self.fixture.project / ".re-discipline/state/marker.json",
            "{}\n",
        )
        _git(self.fixture.project, "add", "--all")
        _git(self.fixture.project, "commit", "-m", "migration marker")
        self.fixture.project_revision = _git(self.fixture.project, "rev-parse", "HEAD")
        with self.assertRaisesRegex(StagingError, "pre-migration only"):
            self.fixture.stage()
        self.assertFalse(self.fixture.output.exists())

    def test_artifact_database_source_and_receipt_tampering_are_rejected(self) -> None:
        self.fixture.stage()

        def rejected(path: Path, mutate) -> None:
            original = path.read_bytes()
            mutate(path, original)
            try:
                with self.assertRaises(StagingError):
                    self.fixture.verify()
            finally:
                path.write_bytes(original)
            self.fixture.verify()

        projection = self.fixture.output / "artifacts/current-projection.json"
        rejected(projection, lambda path, body: path.write_bytes(body + b" "))

        database = self.fixture.output / "artifacts/generation.sqlite"
        rejected(
            database,
            lambda path, body: path.write_bytes(body[:-1] + bytes([body[-1] ^ 1])),
        )

        indexed = json.loads(
            (self.fixture.output / "artifacts/indexed-sources.json").read_text(
                encoding="utf-8"
            )
        )
        source = (
            self.fixture.output
            / "disposable/project"
            / Path(*indexed["sources"][0]["path"].split("/"))
        )
        rejected(source, lambda path, body: path.write_bytes(body + b"tamper"))

        receipt_path = self.fixture.output / "artifacts/harness-receipt.json"

        def mutate_receipt(path: Path, body: bytes) -> None:
            value = json.loads(body)
            value["runtime"]["armCount"] = 3
            path.write_bytes(_stable_json_bytes(value))

        rejected(receipt_path, mutate_receipt)


if __name__ == "__main__":
    unittest.main()
