import json
import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"

SKILLS = {
    "benchmark-knowledge",
    "calibrate-knowledge",
    "close-campaign",
    "decide-agent",
    "decide-retrieval-profile",
    "delegate",
    "hire-agent",
    "init-project",
    "migrate-project",
    "onboard",
    "open-campaign",
    "overturn",
    "review-memory",
    "review-subagent",
}

CAMPAIGN_TEMPLATES = {
    "campaign.json",
    "work-item.json",
    "deferred-work-item.json",
    "run.json",
    "brief.md",
    "report.md",
    "finding.md",
    "intake.json",
    "review.json",
    "STATE.md",
    "archive-README.md",
}

CORE_SCHEMAS = {
    "campaign.schema.json",
    "work-item.schema.json",
    "run.schema.json",
    "finding-frontmatter.schema.json",
    "finding-evidence.schema.json",
    "intake.schema.json",
    "review.schema.json",
    "review-decision.schema.json",
    "event.schema.json",
    "closure-job.schema.json",
    "closure-coverage.schema.json",
    "archive-manifest.schema.json",
    "context-card.schema.json",
    "transaction.schema.json",
}

MIGRATION_SCHEMAS = {
    "review-packet.schema.json",
    "closure-plan.schema.json",
    "closure-receipt.schema.json",
    "migration-plan.schema.json",
    "migration-operation.schema.json",
    "migration-receipt.schema.json",
    "migration-gate-artifact.schema.json",
    "migration-retrieval-gate-evidence.schema.json",
    "migration-blinded-agent-evaluation.schema.json",
    "migration-host-conformance.schema.json",
    "benchmark-result.schema.json",
}


def frontmatter(text: str) -> str:
    assert text.startswith("---\n")
    return text.split("---\n", 2)[1]


class ReDisciplineCompatibilityTests(unittest.TestCase):
    def test_manifests_publish_one_080_contract(self):
        claude = json.loads((PLUGIN / ".claude-plugin" / "plugin.json").read_text())
        codex = json.loads((PLUGIN / ".codex-plugin" / "plugin.json").read_text())
        # Both manifests must publish the same version, and it must stay on
        # the 0.8 contract line. The patch version advances with every release
        # -- pinning it here would make each release a test failure, which is
        # how the published version fell 75 commits behind the source.
        self.assertEqual(claude["version"], codex["version"])
        self.assertRegex(claude["version"], r"^0\.8\.\d+$")
        for manifest in (claude, codex):
            description = manifest["description"].lower()
            self.assertIn("transactional", description)
            self.assertIn("findings", description)
            self.assertIn("closure", description)

    def test_exact_skill_and_agent_surface(self):
        actual = {
            path.parent.name
            for path in (PLUGIN / "skills").glob("*/SKILL.md")
        }
        self.assertEqual(actual, SKILLS)
        agents = list((PLUGIN / "agents").glob("*.md"))
        self.assertEqual([path.name for path in agents], ["knowledge-curator.md"])
        curator = agents[0].read_text(encoding="utf-8")
        meta = frontmatter(curator)
        self.assertIn("name: knowledge-curator", meta)
        self.assertIn("model: inherit", meta)
        self.assertIn("tools: Read, Grep, Glob, Write, Edit", meta)
        for boundary in ("Never set `manager-ratified`", "mutate truth", "close a campaign"):
            self.assertIn(boundary, curator)

    def test_every_skill_has_portable_frontmatter(self):
        for name in sorted(SKILLS):
            text = (PLUGIN / "skills" / name / "SKILL.md").read_text(encoding="utf-8")
            meta = frontmatter(text)
            self.assertRegex(meta, rf"(?m)^name: {re.escape(name)}$")
            self.assertRegex(meta, r"(?m)^description:")
            self.assertNotRegex(meta, r"(?m)^(tools|model|allowed-tools):")

    def test_retired_workflow_paths_are_absent(self):
        forbidden = [
            PLUGIN / "skills" / "checkpoint-campaign",
            PLUGIN / "skills" / "promote-truth",
            PLUGIN / "templates" / "campaign-masterfile.md",
            PLUGIN / "templates" / "campaign-reviews.md",
            PLUGIN / "templates" / "chronicle.md",
        ]
        present = [str(path) for path in forbidden if path.is_file() or (path.is_dir() and any(path.rglob("*")))]
        self.assertEqual(present, [])

    def test_campaign_template_family_is_complete_and_json_parses(self):
        root = PLUGIN / "templates" / "campaign"
        self.assertEqual({path.name for path in root.iterdir() if path.is_file()}, CAMPAIGN_TEMPLATES)
        for name in ("campaign.json", "work-item.json", "deferred-work-item.json", "run.json", "intake.json", "review.json"):
            data = json.loads((root / name).read_text(encoding="utf-8"))
            self.assertEqual(data["schemaVersion"], 2)
        self.assertIn("Derived view. Do not edit", (root / "STATE.md").read_text())

    def test_schema_superset_is_versioned_and_unique(self):
        schema_root = PLUGIN / "knowledge" / "schemas"
        actual = {path.name for path in schema_root.glob("*.schema.json")}
        self.assertTrue(CORE_SCHEMAS | MIGRATION_SCHEMAS <= actual)
        ids = set()
        for path in schema_root.glob("*.schema.json"):
            schema = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(schema["$schema"], "https://json-schema.org/draft/2020-12/schema")
            self.assertNotIn(schema["$id"], ids)
            ids.add(schema["$id"])
        for name in CORE_SCHEMAS:
            schema = json.loads((schema_root / name).read_text())
            if "schemaVersion" in schema.get("properties", {}):
                self.assertEqual(schema["properties"]["schemaVersion"].get("const"), 2)

    def test_project_configuration_declares_08_control_plane(self):
        config = json.loads((PLUGIN / "templates" / "project" / "config.json").read_text())
        self.assertEqual(config["schemaVersion"], 3)
        self.assertEqual(config["campaignSchemaVersion"], 2)
        self.assertFalse(config["authority"]["directStateWrites"])
        self.assertEqual(config["authority"]["truthProjection"], "closure-only")
        self.assertEqual(config["migration"], {"mode": "explicit-only", "legacyReaders": "migrator-only"})
        self.assertTrue(config["payload"]["createLazily"])
        self.assertTrue(config["closure"]["requireArchiveVerification"])

    def test_profile_and_host_adapters_use_bounded_engine_orientation(self):
        templates = PLUGIN / "templates" / "project"
        profile = (templates / "project-profile.md").read_text()
        self.assertIn("re-discipline:shared-laws v0.8.0", profile)
        self.assertIn('state(mode="orient")', profile)
        self.assertIn("Only closure may project", profile)
        self.assertNotIn("{{PROJECT_NAME}}", profile)
        for name in ("CLAUDE.md", "codex-AGENTS.md"):
            text = (templates / name).read_text()
            self.assertIn("v0.8.0", text)
            self.assertIn(".re-discipline/project-profile.md", text)

    def test_external_contract_uses_single_registered_run(self):
        text = (PLUGIN / "templates" / "project" / "external-drafter-contract.md").read_text()
        self.assertIn("active/<campaign>/runs/<run-id>/", text)
        self.assertIn("lazily created `payload/`", text)
        self.assertIn("retained digest", text)
        self.assertIn("Register important payload", text)
        self.assertNotIn("scripts/", text)
        self.assertNotIn("analysis/", text)
        self.assertNotIn("artifacts/", text)

    def test_dispatcher_is_launch_only_and_has_no_deprecated_alias(self):
        text = (PLUGIN / "templates" / "project" / "dispatch.ps1").read_text()
        self.assertIn("[string]$RunId", text)
        self.assertIn("campaign.json", text)
        self.assertIn("'runs'", text)
        self.assertIn("verify-pack", text)
        self.assertIn("Run status", text)
        self.assertNotIn("Alias('Name')", text)
        self.assertNotIn("New-Item", text)

    def test_hooks_declare_full_symmetric_lifecycle(self):
        hooks = json.loads((PLUGIN / "hooks" / "hooks.json").read_text())
        self.assertEqual(
            set(hooks["hooks"]),
            {"SessionStart", "PreToolUse", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop", "Stop"},
        )
        for event, registrations in hooks["hooks"].items():
            command = registrations[0]["hooks"][0]
            self.assertIn("re-discipline-hook.sh", command["command"])
            self.assertIn("re-discipline-hook.ps1", command["commandWindows"])
        for name in ("re-discipline-hook.ps1", "re-discipline-hook.sh"):
            text = (PLUGIN / "hooks" / name).read_text()
            self.assertIn("preflight", text)
            self.assertIn("run.return", text)
            self.assertIn("state mode orient", text)
            self.assertNotIn("semantic checkpoint", text.lower())

    def test_readme_documents_success_and_failed_transition(self):
        text = (PLUGIN / "README.md").read_text()
        self.assertIn("## Worked Example", text)
        self.assertIn("## Failed Transition Example", text)
        self.assertIn("lower-ranked default fallback", text)
        self.assertIn("accident boundary", text)
        self.assertIn("Only closure", text)

    def test_powershell_consumed_files_are_ascii(self):
        paths = [
            PLUGIN / "hooks" / "re-discipline-hook.ps1",
            PLUGIN / "templates" / "project" / "dispatch.ps1",
        ]
        for path in paths:
            path.read_bytes().decode("ascii")

    def test_source_project_identity_is_not_embedded_in_templates(self):
        for path in (PLUGIN / "templates").rglob("*"):
            if path.is_file():
                self.assertNotIn("snaphak-re", path.read_text(encoding="utf-8"), path.as_posix())


if __name__ == "__main__":
    unittest.main()
