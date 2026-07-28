import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


class PackagingTests(unittest.TestCase):
    def test_codex_manifest_describes_re_discipline(self) -> None:
        manifest_path = PLUGIN / ".codex-plugin" / "plugin.json"
        manifest = json.loads(read(manifest_path))

        self.assertEqual(manifest["name"], "re-discipline")
        self.assertEqual(manifest["version"], "0.6.8")
        self.assertEqual(manifest["skills"], "./skills/")
        self.assertTrue((PLUGIN / "hooks" / "hooks.json").is_file())

    def test_codex_marketplace_exposes_re_discipline(self) -> None:
        marketplace = json.loads(
            read(ROOT / ".agents" / "plugins" / "marketplace.json")
        )
        entry = next(
            plugin
            for plugin in marketplace["plugins"]
            if plugin["name"] == "re-discipline"
        )

        self.assertEqual(entry["source"]["source"], "local")
        self.assertEqual(entry["source"]["path"], "./plugins/re-discipline")
        self.assertEqual(entry["policy"]["installation"], "AVAILABLE")
        self.assertEqual(entry["policy"]["authentication"], "ON_INSTALL")

    def test_claude_and_codex_manifests_share_version(self) -> None:
        claude = json.loads(read(PLUGIN / ".claude-plugin" / "plugin.json"))
        codex = json.loads(read(PLUGIN / ".codex-plugin" / "plugin.json"))

        self.assertEqual(claude["version"], "0.6.8")
        self.assertEqual(codex["version"], "0.6.8")
        self.assertEqual(claude["version"], codex["version"])


class SkillMetadataTests(unittest.TestCase):
    def test_every_skill_has_portable_frontmatter(self) -> None:
        skill_paths = sorted((PLUGIN / "skills").glob("*/SKILL.md"))
        self.assertEqual(len(skill_paths), 15)

        for skill_path in skill_paths:
            with self.subTest(skill=skill_path.parent.name):
                body = read(skill_path)
                self.assertTrue(body.startswith("---\n"))
                frontmatter = body.split("---", 2)[1]
                self.assertIn("\nname:", "\n" + frontmatter)
                self.assertIn("\ndescription:", "\n" + frontmatter)
                self.assertNotIn("argument-hint:", frontmatter)
                self.assertNotIn("allowed-tools:", frontmatter)

    def test_init_project_describes_dual_harness_topology(self) -> None:
        body = read(PLUGIN / "skills" / "init-project" / "SKILL.md")
        topology = body.split("## Target Topology", 1)[1].split(
            "The `framing` field",
            1,
        )[0]

        self.assertIn(".re-discipline/project-profile.md", body)
        self.assertIn(".codex/AGENTS.md", body)
        self.assertIn(".claude/CLAUDE.md", body)
        self.assertNotIn(".codex/project-profile.md", topology)
        self.assertNotIn(".claude/project-profile.md", topology)
        self.assertIn("project-owned", body.lower())
        self.assertIn("legacy", body.lower())

    def test_init_project_owns_one_neutral_local_path_signature(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md")
        topology = skill.split("## Target Topology", 1)[1].split(
            "The `framing` field",
            1,
        )[0]
        greenfield = read(
            PLUGIN / "skills" / "init-project" / "references" / "greenfield.md"
        )
        dropin = read(
            PLUGIN / "skills" / "init-project" / "references" / "dropin.md"
        )

        self.assertIn(".re-discipline/local-paths.md", topology)
        self.assertNotIn(".claude/local-paths.md", topology)
        self.assertNotIn(".codex/local-paths.md", topology)
        self.assertIn(".re-discipline/local-paths.md", greenfield)
        self.assertIn(".gitignore", greenfield)
        self.assertIn(".claude/local-paths.md", dropin)
        self.assertIn(".codex/local-paths.md", dropin)
        self.assertIn("merge", dropin.lower())
        self.assertIn("conflict", dropin.lower())
        self.assertIn(".re-discipline/local-paths.md", dropin)

    def test_initialized_mode_requires_the_neutral_local_path_contract(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md")
        initialized = skill.split("- **Initialized:**", 1)[1].split(
            "- **Migration:**",
            1,
        )[0]

        self.assertIn(".re-discipline/local-paths.md", initialized)
        self.assertIn("ignored", initialized)
        self.assertIn("legacy", initialized)

    def test_init_project_keeps_defensive_legacy_secret_ignores(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md")
        greenfield = read(
            PLUGIN / "skills" / "init-project" / "references" / "greenfield.md"
        )
        dropin = read(
            PLUGIN / "skills" / "init-project" / "references" / "dropin.md"
        )

        for guide in (skill, greenfield, dropin):
            self.assertIn(".re-discipline/local-paths.md", guide)
            self.assertIn(".claude/local-paths.md", guide)
            self.assertIn(".codex/local-paths.md", guide)
            self.assertIn("defense-only", guide.lower())

    def test_delegate_has_native_claude_and_codex_adapters(self) -> None:
        body = read(PLUGIN / "skills" / "delegate" / "SKILL.md")

        self.assertIn("spawn_agent", body)
        self.assertIn("Claude Code", body)
        self.assertIn("Codex", body)
        self.assertIn(".codex/external-drafter-contract.md", body)

    def test_delegate_owns_chronological_workspace_identity(self) -> None:
        body = read(PLUGIN / "skills" / "delegate" / "SKILL.md")

        self.assertIn("YYYY-MM-DDTHH-mm-ssZ-<executor>-<task>", body)
        self.assertIn("fail-if-exists", body)
        self.assertIn("-02", body)
        self.assertIn("-99", body)
        self.assertIn("-DispatchId <dispatch-id>", body)
        self.assertIn("worker", body.lower())
        self.assertIn("not the manager", body.lower())
        self.assertIn("reroute", body.lower())
        self.assertIn("leave", body.lower())
        self.assertNotIn("<provider>-<name>", body)
        for field in (
            "Workspace:",
            "Created UTC:",
            "Manager host:",
            "Executor:",
            "Execution route:",
            "Provider/model:",
            "Task:",
        ):
            with self.subTest(field=field):
                self.assertIn(field, body)

    def test_hire_agent_uses_real_chronological_run_workspaces(self) -> None:
        body = read(PLUGIN / "skills" / "hire-agent" / "SKILL.md")

        self.assertIn("runs/<dispatch-id>/", body)
        self.assertIn("-RecruitingCandidate <candidate>", body)
        self.assertIn("-DispatchId <dispatch-id>", body)
        self.assertIn("recruiting-AGENTS-override.md", body)
        self.assertIn("stable executor", body.lower())

    def test_lifecycle_accepts_legacy_opaque_workspace_keys(self) -> None:
        for name in (
            "review-subagent",
            "checkpoint-campaign",
            "close-campaign",
            "onboard",
        ):
            with self.subTest(skill=name):
                body = read(PLUGIN / "skills" / name / "SKILL.md").lower()
                self.assertIn("legacy", body)
                self.assertIn("opaque", body)

    def test_lifecycle_skills_do_not_require_claude_only_tools(self) -> None:
        for skill_path in sorted((PLUGIN / "skills").glob("*/SKILL.md")):
            if skill_path.parent.name == "init-project":
                continue
            with self.subTest(skill=skill_path.parent.name):
                body = read(skill_path)
                self.assertNotIn("AskUserQuestion", body)
                self.assertNotIn("${CLAUDE_PLUGIN_ROOT}", body)

    def test_mutating_skills_leave_commits_to_the_user(self) -> None:
        mutating = {
            "checkpoint-campaign",
            "close-campaign",
            "decide-agent",
            "delegate",
            "hire-agent",
            "open-campaign",
            "overturn",
            "promote-truth",
            "review-subagent",
        }

        for name in sorted(mutating):
            with self.subTest(skill=name):
                body = read(PLUGIN / "skills" / name / "SKILL.md").lower()
                self.assertIn("do not commit unless the user explicitly asks", body)

    def test_onboard_detects_the_active_harness(self) -> None:
        body = read(PLUGIN / "skills" / "onboard" / "SKILL.md")

        self.assertIn(".re-discipline/project-profile.md", body)
        self.assertIn(".codex/AGENTS.md", body)
        self.assertIn(".claude/CLAUDE.md", body)
        self.assertIn("active host", body.lower())

    def test_shared_runtime_adapter_reference_exists(self) -> None:
        body = read(PLUGIN / "references" / "runtime-adapters.md")

        self.assertIn("Claude Code", body)
        self.assertIn("Codex", body)
        self.assertIn("spawn_agent", body)
        self.assertIn("PLUGIN_ROOT", body)

    def test_init_project_always_creates_agent_core(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md")
        greenfield = read(
            PLUGIN / "skills" / "init-project" / "references" / "greenfield.md"
        )
        combined = skill + "\n" + greenfield

        for path in (
            ".re-discipline/agents/README.md",
            ".re-discipline/agents/config.json",
            ".re-discipline/agents/dispatch.ps1",
            ".re-discipline/agents/providers/",
            ".re-discipline/agents/recruiting/",
        ):
            self.assertIn(path, combined)

        self.assertNotIn("If the user wants external-provider dispatch", combined)

    def test_dropin_preserves_inflight_legacy_candidates(self) -> None:
        dropin = read(
            PLUGIN / "skills" / "init-project" / "references" / "dropin.md"
        )

        for mapping in (
            "`CANDIDATE.md` -> `candidate.md`",
            "`config-draft.json` -> `config.json`",
            "`profile-draft.md` -> `profile.md`",
            "`rollback-manifest.md` -> `teardown.md`",
            "`interview/` -> `runs/`",
        ):
            self.assertIn(mapping, dropin)

        self.assertIn(".re-discipline/agents/recruiting/<candidate>/", dropin)

    def test_agent_framework_has_no_legacy_state_or_descriptive_roles(self) -> None:
        current_paths = [
            PLUGIN / "README.md",
            PLUGIN / "references" / "runtime-adapters.md",
            PLUGIN / "skills" / "delegate" / "SKILL.md",
            PLUGIN / "skills" / "hire-agent" / "SKILL.md",
            PLUGIN
            / "skills"
            / "hire-agent"
            / "references"
            / "research-checklist.md",
            PLUGIN / "skills" / "hire-agent" / "references" / "scoring-rubric.md",
            PLUGIN / "skills" / "decide-agent" / "SKILL.md",
            PLUGIN
            / "skills"
            / "decide-agent"
            / "references"
            / "integration-points.md",
            PLUGIN / "skills" / "onboard" / "SKILL.md",
            PLUGIN / "templates" / "project" / "agents-README.md",
            PLUGIN / "templates" / "project" / "agent-profile.md",
            PLUGIN / "templates" / "project" / "project-profile.md",
            PLUGIN / "templates" / "project" / "tree.txt",
        ]
        forbidden = (
            "agents/roster",
            "agents/profiles",
            "agents/benchmarks",
            "DEPARTED.md",
            "departure record",
            "role-fit",
            "{{DOMAIN_ROLES}}",
            "Mechanical fan-out",
            "Vision reader",
            "Live tester",
            "choose capability by role",
            '"enabled"',
            '"promoted"',
        )

        for path in current_paths:
            body = read(path)
            for needle in forbidden:
                with self.subTest(path=path, needle=needle):
                    self.assertNotIn(needle, body)

    def test_lifecycle_uses_durable_verification_not_archive_storage(self) -> None:
        lifecycle_paths = [
            PLUGIN / "skills" / "onboard" / "SKILL.md",
            PLUGIN / "skills" / "checkpoint-campaign" / "SKILL.md",
            PLUGIN / "skills" / "close-campaign" / "SKILL.md",
            PLUGIN / "skills" / "promote-truth" / "SKILL.md",
            PLUGIN / "skills" / "review-subagent" / "SKILL.md",
            PLUGIN / "skills" / "overturn" / "SKILL.md",
            PLUGIN / "skills" / "delegate" / "SKILL.md",
        ]
        forbidden = (
            "archive/",
            "irreproducible artifacts",
            "expensive-reproducible",
            "preserved artifact",
        )

        for path in lifecycle_paths:
            body = read(path).lower()
            for phrase in forbidden:
                with self.subTest(skill=path.parent.name, phrase=phrase):
                    self.assertNotIn(phrase, body)

        promote = read(PLUGIN / "skills" / "promote-truth" / "SKILL.md").lower()
        self.assertIn("direct", promote)
        self.assertIn("durable verifier", promote)
        self.assertIn("maintained source", promote)
        self.assertIn("permanent test", promote)
        self.assertIn("runnable recipe", promote)
        self.assertIn("chronicle", promote)
        self.assertIn("sole empirical support", promote)

    def test_init_project_gates_legacy_archive_semantic_migration(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md").lower()
        dropin = read(
            PLUGIN / "skills" / "init-project" / "references" / "dropin.md"
        ).lower()
        combined = skill + "\n" + dropin

        for phrase in (
            "legacy archive",
            "semantic migration",
            "do not delete",
            "directory named `archive` alone",
            "does not prove re-discipline ownership",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, combined)

        self.assertIn("maintain", combined)
        self.assertIn("distill", combined)
        self.assertIn("delete", combined)

    def test_legacy_archive_semantics_override_initialized_mode(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md").lower()
        initialized = skill.split("- **initialized:**", 1)[1].split(
            "- **migration:**",
            1,
        )[0]

        self.assertIn("no unresolved legacy archive semantics", initialized)
        self.assertIn("legacy archive semantics always override initialized", skill)

    def test_legacy_migration_allows_temporary_active_evidence(self) -> None:
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md").lower()
        dropin = read(
            PLUGIN / "skills" / "init-project" / "references" / "dropin.md"
        ).lower()
        combined = skill + "\n" + dropin

        self.assertIn("active/<slug>/evidence/", combined)
        self.assertIn("temporary campaign evidence", combined)
        self.assertIn("no durable root-level replacement evidence directory", combined)


class ProjectTemplateTests(unittest.TestCase):
    def setUp(self) -> None:
        self.templates = PLUGIN / "templates" / "project"

    def test_dual_harness_templates_exist(self) -> None:
        expected = {
            "AGENTS.md",
            "CLAUDE.md",
            "agents-config.json",
            "agents-README.md",
            "dispatch.ps1",
            "codex-AGENTS.md",
            "drafter-AGENTS-override.md",
            "external-drafter-contract.md",
            "local-paths.md",
            "project-profile.md",
            "recruiting-AGENTS-override.md",
            "config.json",
            "knowledge.jsonc",
            "settings-README.md",
            "retrieval-profile.json",
            "memory-INDEX.md",
            "knowledge-evals-README.md",
            "claude-settings.json",
            "codex-config.toml",
        }

        self.assertTrue(expected.issubset({path.name for path in self.templates.iterdir()}))
        self.assertFalse((self.templates / "claude-project-profile.md").exists())
        self.assertFalse((self.templates / "codex-project-profile.md").exists())
        self.assertEqual(
            [path.name for path in self.templates.glob("*project-profile.md")],
            ["project-profile.md"],
        )

    def test_new_projects_have_shared_only_knowledge_defaults(self) -> None:
        config = json.loads(read(self.templates / "config.json"))
        claude = json.loads(read(self.templates / "claude-settings.json"))
        codex = read(self.templates / "codex-config.toml")
        tree = read(self.templates / "tree.txt")

        self.assertEqual(config["schemaVersion"], 1)
        self.assertEqual(config["memory"], {
            "mode": "shared-only",
            "writePolicy": "proposal-only",
        })
        self.assertEqual(config["knowledge"]["profile"], "plugin:balanced-v1")
        self.assertFalse(claude["autoMemoryEnabled"])
        self.assertIn("[features]\nmemories = false", codex)
        self.assertIn("generate_memories = false", codex)
        self.assertIn("use_memories = false", codex)
        for path in (
            ".re-discipline/settings",
            ".re-discipline/memory/proposals",
            ".re-discipline/memory/topics",
            ".re-discipline/knowledge/evals",
            ".re-discipline/cache/knowledge/generations",
            ".re-discipline/cache/knowledge/vectors",
            ".re-discipline/cache/calibration",
        ):
            with self.subTest(path=path):
                self.assertIn(path, tree)

    def test_new_project_knowledge_settings_match_the_server_defaults(self) -> None:
        """The template and DefaultKnowledgeSettings are one decision.

        A new project takes its controls from the template, and a project with
        no settings file at all takes them from the Go default. When the two
        disagree, the shipped default silently stops being the shipped default.
        """
        body = read(self.templates / "knowledge.jsonc")
        stripped = "\n".join(
            "" if line.lstrip().startswith("//") else line
            for line in body.splitlines()
        )
        settings = json.loads(stripped)

        self.assertTrue(settings["sources"]["drafterReports"])
        self.assertEqual(
            settings["budgets"],
            {
                "searchTokens": 3072,
                "managerContextTokens": 6144,
                "drafterContextTokens": 3072,
                "maxPassages": 12,
                "maxBytes": 32768,
            },
        )

        defaults = read(
            PLUGIN / "knowledge" / "internal" / "knowledge" / "config.go"
        ).split("func DefaultKnowledgeSettings()", 1)[1].split("\n}\n", 1)[0]
        self.assertIn("DrafterReports: true", defaults)
        self.assertIn("SearchTokens: 3072", defaults)
        self.assertIn("ManagerContextTokens: 6144", defaults)
        self.assertIn("DrafterContextTokens: 3072", defaults)

    def test_project_retrieval_profile_tracks_packaged_baseline(self) -> None:
        project_profile = json.loads(read(self.templates / "retrieval-profile.json"))
        packaged_profile = json.loads(
            read(PLUGIN / "knowledge" / "profiles" / "balanced-v1.json")
        )

        project_profile.pop("$schema", None)
        packaged_profile.pop("$schema", None)
        self.assertEqual(project_profile, packaged_profile)

    def test_settings_readme_documents_every_control_plane_field(self) -> None:
        body = read(self.templates / "settings-README.md")
        required_fields = (
            "memory.mode",
            "memory.writePolicy",
            "knowledge.enabled",
            "knowledge.profile",
            "sources.truth",
            "sources.history",
            "sources.backlog",
            "sources.activeCampaigns",
            "sources.sharedMemory",
            "sources.drafterReports",
            "models.execution",
            "telemetry.mode",
            "budgets.searchTokens",
            "budgets.managerContextTokens",
            "budgets.drafterContextTokens",
            "budgets.maxPassages",
            "budgets.maxBytes",
            "effectiveProfiles[].requires.embedding",
            "effectiveProfiles[].requires.reranker",
            "effectiveProfiles[].weights",
            "effectiveProfiles[].benchmark.digest",
        )
        for field in required_fields:
            with self.subTest(field=field):
                self.assertIn(field, body)

        for default in ("3072", "6144", "12", "32768", "shared-only"):
            with self.subTest(default=default):
                self.assertIn(default, body)
        self.assertIn("never silently replaced", body)
        self.assertIn("de-initialize", body.lower())

    def test_managed_profile_declares_v060_recovery_contract(self) -> None:
        canonical = read(self.templates / "project-profile.md")
        skill = read(PLUGIN / "skills" / "init-project" / "SKILL.md")

        self.assertIn("re-discipline:shared-laws v0.6.0", canonical)
        self.assertIn("Managed Configuration Recovery", skill)
        self.assertIn("staged", skill.lower())
        self.assertIn("de-initialization", skill.lower())
        self.assertIn("machine-local native memory", skill.lower())

    def test_external_dispatch_defaults_to_native_and_sandboxed(self) -> None:
        config = json.loads(read(self.templates / "agents-config.json"))
        dispatcher = read(self.templates / "dispatch.ps1")

        self.assertEqual(config["backend"], "native")
        self.assertEqual(config["providers"], {})
        self.assertIn("[switch]$Bypass", dispatcher)
        self.assertNotIn("bypass_default", dispatcher)
        self.assertIn(".re-discipline/project-profile.md", dispatcher)
        self.assertIn("AGENTS.override.md", dispatcher)

    def test_agent_framework_templates_use_normalized_topology(self) -> None:
        config = json.loads(read(self.templates / "agents-config.json"))
        readme = read(self.templates / "agents-README.md")
        tree = read(self.templates / "tree.txt")
        profile = read(self.templates / "agent-profile.md")

        self.assertEqual(config, {"backend": "native", "providers": {}})
        self.assertIn(".re-discipline/agents/", tree)
        self.assertNotIn("Empty dirs get a .gitkeep", tree)
        self.assertIn(".re-discipline/agents/providers/<provider>/", readme)
        self.assertIn("profile.md", readme)
        self.assertIn("scorecard.md", readme)
        self.assertIn("teardown.md", readme)
        self.assertIn(".re-discipline/agents/recruiting/<candidate>/", readme)
        self.assertNotIn("role-fit", profile)
        self.assertNotIn("promoted:", profile)
        self.assertNotIn("benchmarks/", profile)

    def test_root_agents_routes_manager_and_drafter_roles(self) -> None:
        body = read(self.templates / "AGENTS.md")

        self.assertIn(".codex/AGENTS.md", body)
        self.assertIn(".codex/external-drafter-contract.md", body)
        self.assertIn("Do not merge these roles", body)
        self.assertIn("manager", body.lower())
        self.assertIn("drafter", body.lower())
        self.assertNotIn("role-fit", body)

    def test_harness_contracts_point_to_canonical_profile(self) -> None:
        canonical = read(self.templates / "project-profile.md")
        claude = read(self.templates / "CLAUDE.md")
        codex = read(self.templates / "codex-AGENTS.md")
        root = read(self.templates / "AGENTS.md")

        self.assertIn("single source", canonical.lower())
        self.assertEqual(
            claude.count("@../.re-discipline/project-profile.md"),
            1,
        )
        self.assertNotIn("@project-profile.md", claude)
        self.assertIn(".re-discipline/project-profile.md", codex)
        self.assertIn(".re-discipline/project-profile.md", root)
        self.assertNotIn(".codex/project-profile.md", codex)
        self.assertNotIn(".codex/project-profile.md", root)
        self.assertNotIn(".claude/project-profile.md", root)

    def test_local_path_template_is_neutral_and_untracked_by_contract(self) -> None:
        canonical = read(self.templates / "project-profile.md")
        local_paths = read(self.templates / "local-paths.md")
        claude = read(self.templates / "CLAUDE.md")
        codex = read(self.templates / "codex-AGENTS.md")

        self.assertIn(".re-discipline/local-paths.md", canonical)
        self.assertIn("untracked", local_paths.lower())
        self.assertIn("{{LOCAL_PATH_ASSIGNMENTS}}", local_paths)
        self.assertNotIn("Claude", local_paths)
        self.assertNotIn("Codex", local_paths)
        self.assertNotIn(".claude/local-paths.md", claude)
        self.assertNotIn(".codex/local-paths.md", codex)

    def test_shared_laws_exist_only_in_the_canonical_profile(self) -> None:
        canonical = read(self.templates / "project-profile.md")
        adapters = {
            "Claude": read(self.templates / "CLAUDE.md"),
            "Codex": read(self.templates / "codex-AGENTS.md"),
        }
        shared_headings = {
            "## Directory Means Trust",
            "## Session Start",
            "## The Wall",
            "## Campaign Lifecycle",
            "## Manager And Drafter Roles",
            "## Commits And Local State",
            "## Anti-Patterns",
        }

        for heading in shared_headings:
            self.assertIn(heading, canonical)
            for name, adapter in adapters.items():
                with self.subTest(adapter=name, heading=heading):
                    self.assertNotIn(heading, adapter)

        self.assertNotIn("claude-laws", adapters["Claude"])
        self.assertNotIn("codex-laws", adapters["Codex"])
        self.assertNotIn("## Roles", canonical)
        self.assertIn("## Manager And Drafter Roles", canonical)
        self.assertEqual(canonical.count("re-discipline:shared-laws v"), 1)
        self.assertEqual(canonical.count("re-discipline:shared-laws:end"), 1)
        self.assertLessEqual(len(adapters["Claude"].splitlines()), 24)
        self.assertLessEqual(len(adapters["Codex"].splitlines()), 28)

    def test_canonical_profile_template_is_host_neutral_and_bounded(self) -> None:
        canonical = read(self.templates / "project-profile.md")

        self.assertLessEqual(len(canonical.splitlines()), 240)
        self.assertLessEqual(len(canonical.encode("utf-8")), 16 * 1024)
        self.assertEqual(canonical.count("framing:"), 1)
        self.assertNotIn("Claude", canonical)
        self.assertNotIn("Codex", canonical)
        self.assertNotIn("overlay", canonical.lower())

    def test_external_contract_is_not_the_root_router(self) -> None:
        root = read(self.templates / "AGENTS.md")
        drafter = read(self.templates / "external-drafter-contract.md")

        self.assertNotIn("## Report Format", root)
        self.assertIn("drafter, not ratifier", drafter.lower())
        self.assertIn("report.md", drafter)

    def test_external_contract_supports_campaign_and_recruiting_workspaces(
        self,
    ) -> None:
        contract = read(self.templates / "external-drafter-contract.md")
        recruiting_override = read(
            self.templates / "recruiting-AGENTS-override.md"
        )

        self.assertIn("active/<slug>/subagents/<workspace-id>/", contract)
        self.assertIn(
            ".re-discipline/agents/recruiting/<candidate>/runs/<workspace-id>/",
            contract,
        )
        for relative in (
            "../../../../../project-profile.md",
            "../../../../../../.codex/external-drafter-contract.md",
            "../../candidate.md",
            "../../profile.md",
            "brief.md",
        ):
            with self.subTest(relative=relative):
                self.assertIn(relative, recruiting_override)
        self.assertNotIn("CAMPAIGN.md", recruiting_override)

    def test_campaign_template_is_host_neutral(self) -> None:
        body = read(PLUGIN / "templates" / "campaign-masterfile.md")

        self.assertIn("re-discipline manager", body)
        self.assertNotIn("Owner:** Claude Code", body)

    def test_core_templates_do_not_encode_the_source_project(self) -> None:
        paths = [
            PLUGIN / "templates" / "campaign-masterfile.md",
            PLUGIN / "templates" / "chronicle.md",
            PLUGIN / "templates" / "truth-claim.md",
            *sorted((PLUGIN / "templates" / "project").glob("*.md")),
        ]

        for path in paths:
            with self.subTest(template=path.name):
                body = read(path).lower()
                self.assertNotIn("snaphak", body)
                self.assertNotIn("doom", body)

    def test_greenfield_topology_has_no_archive_directory(self) -> None:
        tree = read(self.templates / "tree.txt")
        topology = {
            line.strip().lower()
            for line in tree.splitlines()
            if line.strip() and not line.lstrip().startswith("#")
        }

        self.assertNotIn("archive", topology)

    def test_truth_requires_recheckable_verification(self) -> None:
        truth = read(PLUGIN / "templates" / "truth-claim.md")

        self.assertIn("## Verification", truth)
        self.assertIn("**Source:**", truth)
        self.assertIn("**Test or fixture:**", truth)
        self.assertIn("**Recipe:**", truth)
        self.assertIn("**Provenance:**", truth)
        self.assertIn("future manager", truth.lower())
        self.assertIn("not empirical support", truth.lower())
        self.assertNotIn("**Archive:**", truth)

    def test_chronicle_preserves_provenance_not_raw_evidence(self) -> None:
        chronicle = read(PLUGIN / "templates" / "chronicle.md")

        self.assertIn("## Reproduction Recipes", chronicle)
        self.assertIn("Maintained deliverables", chronicle)
        self.assertIn("Discarded material", chronicle)
        self.assertIn("Folded from deleted scratch", chronicle)
        self.assertIn("provenance", chronicle.lower())
        self.assertNotIn("Irreproducible artifacts", chronicle)
        self.assertNotIn("archive/", chronicle.lower())

    def test_campaign_disposes_artifacts_by_maintenance_value(self) -> None:
        campaign = read(PLUGIN / "templates" / "campaign-masterfile.md").lower()

        for disposition in ("maintain", "distill", "delete"):
            with self.subTest(disposition=disposition):
                self.assertIn(f"| {disposition} |", campaign)

        for legacy_tag in (
            "| ground-truth |",
            "| capture |",
            "| expensive-reproducible |",
        ):
            with self.subTest(legacy_tag=legacy_tag):
                self.assertNotIn(legacy_tag, campaign)


class DocumentationTests(unittest.TestCase):
    def test_plugin_readme_explains_both_install_and_init_flows(self) -> None:
        body = read(PLUGIN / "README.md")

        self.assertIn("codex plugin marketplace add", body)
        self.assertIn("codex plugin add re-discipline@adiaz-claude-plugins", body)
        self.assertIn("$re-discipline:init-project", body)
        self.assertIn("/re-discipline:init-project", body)
        self.assertIn(".re-discipline/project-profile.md", body)
        self.assertIn(".re-discipline/local-paths.md", body)
        self.assertIn("one canonical project profile", body.lower())
        self.assertNotIn("Yes, a new project gets `.codex/project-profile.md`", body)
        self.assertIn("/hooks", body)
        self.assertIn(".agents/plugins/marketplace.json", body)
        self.assertIn("not re-discipline scratch", body.lower())

    def test_plugin_readme_defines_no_archive_knowledge_model(self) -> None:
        body = read(PLUGIN / "README.md").lower()

        self.assertNotIn("archive/", body)
        self.assertIn("temporary evidence", body)
        self.assertIn("durable verification", body)
        self.assertIn("maintain", body)
        self.assertIn("distill", body)
        self.assertIn("delete", body)

    def test_repository_readme_identifies_the_codex_marketplace(self) -> None:
        body = read(ROOT / "README.md")

        self.assertIn(".agents/plugins/marketplace.json", body)
        self.assertIn("Codex", body)


class ExternalDispatcherTests(unittest.TestCase):
    DISPATCH_ID = "2026-07-25T21-45-03Z-demo-resource-scan"
    RECRUITING_ID = "2026-07-25T21-45-03Z-demo-static-fixture"
    CONTEXT_PACK_DIGEST = "sha256:" + ("a" * 64)

    def setUp(self) -> None:
        self.powershell = shutil.which("powershell.exe") or shutil.which("powershell")
        if not self.powershell:
            self.skipTest("PowerShell is required for dispatcher tests")

    @staticmethod
    def expected_dispatch_path(path: Path) -> str:
        return str(path.resolve()) if os.name == "nt" else str(path)

    def make_dispatch_project(
        self,
        root: Path,
        config: dict,
        *,
        candidate: str | None = None,
        recruiting: bool = False,
        dispatch_id: str | None = None,
        managed_v06: bool = False,
    ) -> tuple[Path, Path]:
        for provider_config in config.get("providers", {}).values():
            if provider_config.get("command") == "powershell.exe":
                provider_config["command"] = self.powershell

        templates = PLUGIN / "templates" / "project"
        agents = root / ".re-discipline" / "agents"
        agents.mkdir(parents=True)
        shutil.copy2(templates / "dispatch.ps1", agents / "dispatch.ps1")

        profile = root / ".re-discipline" / "project-profile.md"
        profile.parent.mkdir(parents=True, exist_ok=True)
        marker = (
            "<!-- re-discipline:shared-laws v0.6.0 -->\n"
            "<!-- re-discipline:shared-laws:end -->\n"
            if managed_v06
            else ""
        )
        profile.write_text(
            f"---\nname: sample\n---\n{marker}",
            encoding="utf-8",
        )

        candidate_name = candidate or "demo"
        if candidate is None and not recruiting:
            config_path = agents / "config.json"
            for provider_name in config.get("providers", {}):
                provider_profile = (
                    agents / "providers" / provider_name / "profile.md"
                )
                provider_profile.parent.mkdir(parents=True, exist_ok=True)
                provider_profile.write_text(
                    f"# {provider_name} provider\n",
                    encoding="utf-8",
                )
        else:
            candidate_dir = agents / "recruiting" / candidate_name
            candidate_dir.mkdir(parents=True)
            config_path = candidate_dir / "config.json"
            provider_profile = candidate_dir / "profile.md"
            provider_profile.write_text("# Demo provider\n", encoding="utf-8")

        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(json.dumps(config), encoding="utf-8")

        contract = root / ".codex" / "external-drafter-contract.md"
        contract.parent.mkdir(parents=True, exist_ok=True)
        contract.write_text("# External drafter\n", encoding="utf-8")

        if recruiting:
            (candidate_dir / "candidate.md").write_text(
                "# Candidate\n",
                encoding="utf-8",
            )
            workspace = (
                candidate_dir
                / "runs"
                / (dispatch_id or self.RECRUITING_ID)
            )
            workspace.mkdir(parents=True)
            shutil.copy2(
                templates / "recruiting-AGENTS-override.md",
                workspace / "AGENTS.override.md",
            )
        else:
            campaign = root / "active" / "sample"
            workspace = (
                campaign / "subagents" / (dispatch_id or self.DISPATCH_ID)
            )
            workspace.mkdir(parents=True)
            (campaign / "CAMPAIGN.md").write_text(
                "# Campaign\n",
                encoding="utf-8",
            )
            (workspace / "AGENTS.override.md").write_text(
                "# Drafter\n",
                encoding="utf-8",
            )

        (workspace / "brief.md").write_text("# Brief\n", encoding="utf-8")
        return agents / "dispatch.ps1", config_path

    def dispatch(
        self,
        dispatcher: Path,
        *extra: str,
        provider: str = "demo",
        mode_args: list[str] | None = None,
        dispatch_id: str | None = None,
        id_parameter: str = "-DispatchId",
    ) -> subprocess.CompletedProcess[str]:
        selected_mode = (
            ["-Slug", "sample"] if mode_args is None else mode_args
        )
        return subprocess.run(
            [
                self.powershell,
                "-NoProfile",
                "-NonInteractive",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(dispatcher),
                "-Provider",
                provider,
                *selected_mode,
                id_parameter,
                dispatch_id or self.DISPATCH_ID,
                *extra,
                "-DryRun",
            ],
            text=True,
            capture_output=True,
            check=False,
        )

    def test_dispatcher_dry_run_resolves_a_provider_workspace(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = {
                "backend": "native",
                "providers": {
                    "demo": {
                        "command": "powershell.exe",
                        "args": ["{model_args}", "{policy_args}", "{prompt}"],
                        "model_flag": "-Model",
                        "sandbox_args": ["-NoProfile"],
                        "bypass_args": ["-NoProfile"],
                    }
                },
            }
            dispatcher, _ = self.make_dispatch_project(root, config)
            result = self.dispatch(dispatcher)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("provider=demo", result.stdout)
        self.assertIn(f"workspace={self.DISPATCH_ID}", result.stdout)
        self.assertNotIn(
            f"workspace=demo-{self.DISPATCH_ID}",
            result.stdout,
        )
        self.assertIn("SANDBOXED", result.stdout)
        self.assertIn("<CLI default>", result.stdout)
        self.assertIn(
            ".re-discipline\\agents\\providers\\demo\\profile.md",
            result.stdout.replace("/", "\\"),
        )

    def test_dispatcher_prompt_uses_absolute_external_contract(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, _ = self.make_dispatch_project(root, config)
            result = self.dispatch(dispatcher)
            expected = self.expected_dispatch_path(
                root / ".codex" / "external-drafter-contract.md"
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            expected.replace("/", "\\"),
            result.stdout.replace("/", "\\"),
        )

    def test_managed_dispatch_requires_an_immutable_context_pack(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(
                Path(directory),
                config,
                managed_v06=True,
            )
            result = self.dispatch(dispatcher)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires immutable context-pack.json", result.stderr)

    def test_dispatcher_invokes_context_pack_verifier_and_rejects_tampering(
        self,
    ) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{context_pack}", "{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, _ = self.make_dispatch_project(
                root,
                config,
                managed_v06=True,
            )
            workspace = (
                root
                / "active"
                / "sample"
                / "subagents"
                / self.DISPATCH_ID
            )
            context_pack = workspace / "context-pack.json"
            context_pack.write_text(
                json.dumps(
                    {
                        "packId": "context-verified-fixture",
                        "digest": self.CONTEXT_PACK_DIGEST,
                    }
                ),
                encoding="utf-8",
            )
            expected_context_pack = self.expected_dispatch_path(context_pack)
            verifier = root / "verify-pack-fixture.ps1"
            verifier.write_text(
                "if ($args.Count -ne 5 -or $args[0] -ne 'verify-pack' "
                "-or $args[1] -ne '--input' "
                "-or $args[3] -ne '--expected-digest') "
                "{ throw 'unexpected verifier args' }\n"
                "$pack = Get-Content -LiteralPath $args[2] -Raw | ConvertFrom-Json\n"
                "if ($pack.digest -cne $args[4]) { throw 'digest mismatch' }\n"
                "Write-Output '{\"valid\":true}'\n",
                encoding="utf-8",
            )

            missing_expected = self.dispatch(
                dispatcher,
                "-ContextPackPath",
                str(context_pack),
                "-KnowledgeRuntime",
                str(verifier),
            )
            malformed_expected = self.dispatch(
                dispatcher,
                "-ContextPackPath",
                str(context_pack),
                "-ExpectedContextPackDigest",
                "sha256:not-a-digest",
                "-KnowledgeRuntime",
                str(verifier),
            )
            mismatched_expected = self.dispatch(
                dispatcher,
                "-ContextPackPath",
                str(context_pack),
                "-ExpectedContextPackDigest",
                "sha256:" + ("b" * 64),
                "-KnowledgeRuntime",
                str(verifier),
            )
            verified = self.dispatch(
                dispatcher,
                "-ContextPackPath",
                str(context_pack),
                "-ExpectedContextPackDigest",
                self.CONTEXT_PACK_DIGEST,
                "-KnowledgeRuntime",
                str(verifier),
            )
            context_pack.write_text(
                json.dumps(
                    {
                        "packId": "context-verified-fixture",
                        "digest": "sha256:" + ("c" * 64),
                    }
                ),
                encoding="utf-8",
            )
            tampered = self.dispatch(
                dispatcher,
                "-ContextPackPath",
                str(context_pack),
                "-ExpectedContextPackDigest",
                self.CONTEXT_PACK_DIGEST,
                "-KnowledgeRuntime",
                str(verifier),
            )

        self.assertNotEqual(missing_expected.returncode, 0)
        self.assertIn("-ExpectedContextPackDigest", missing_expected.stderr)
        self.assertNotEqual(malformed_expected.returncode, 0)
        self.assertIn("64 lowercase hexadecimal", malformed_expected.stderr)
        self.assertNotEqual(mismatched_expected.returncode, 0)
        self.assertIn(
            "Context pack verification failed",
            mismatched_expected.stderr,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        self.assertIn("context-pack=context-verified-fixture", verified.stdout)
        self.assertIn(
            f"expected-digest={self.CONTEXT_PACK_DIGEST}",
            verified.stdout,
        )
        self.assertIn("blocked report", verified.stdout)
        self.assertIn(expected_context_pack, verified.stdout)
        self.assertNotEqual(tampered.returncode, 0)
        self.assertIn("Context pack verification failed", tampered.stderr)

    def test_dispatcher_name_alias_uses_the_exact_completed_id(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            result = self.dispatch(
                dispatcher,
                id_parameter="-Name",
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"workspace={self.DISPATCH_ID}", result.stdout)

    def test_dispatcher_rejects_provider_executor_mismatch(self) -> None:
        provider = {
            "command": "powershell.exe",
            "args": ["{prompt}"],
            "sandbox_args": ["-NoProfile"],
            "bypass_args": ["-NoProfile"],
        }
        config = {
            "backend": "native",
            "providers": {"demo": provider, "other": provider},
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            result = self.dispatch(dispatcher, provider="other")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("executor", result.stderr.lower())

    def test_dispatcher_rejects_malformed_completed_ids(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        invalid_ids = (
            "2026-13-40T25-99-99Z-demo-task",
            "2026-07-25T21-45-03Z-demo-bad--task",
        )
        for dispatch_id in invalid_ids:
            with self.subTest(dispatch_id=dispatch_id):
                with tempfile.TemporaryDirectory() as directory:
                    dispatcher, _ = self.make_dispatch_project(
                        Path(directory),
                        config,
                        dispatch_id=dispatch_id,
                    )
                    result = self.dispatch(
                        dispatcher,
                        dispatch_id=dispatch_id,
                    )

                self.assertNotEqual(result.returncode, 0)
                self.assertIn("dispatch id", result.stderr.lower())

    def test_dispatcher_rejects_malformed_provider_slugs(self) -> None:
        for provider_name in ("bad--provider", "bad-"):
            with self.subTest(provider=provider_name):
                config = {
                    "backend": "native",
                    "providers": {
                        provider_name: {
                            "command": "powershell.exe",
                            "args": ["{prompt}"],
                            "sandbox_args": ["-NoProfile"],
                            "bypass_args": ["-NoProfile"],
                        }
                    },
                }
                dispatch_id = (
                    f"2026-07-25T21-45-03Z-{provider_name}-task"
                )
                with tempfile.TemporaryDirectory() as directory:
                    dispatcher, _ = self.make_dispatch_project(
                        Path(directory),
                        config,
                        dispatch_id=dispatch_id,
                    )
                    result = self.dispatch(
                        dispatcher,
                        provider=provider_name,
                        dispatch_id=dispatch_id,
                    )

                self.assertNotEqual(result.returncode, 0)
                self.assertIn("provider", result.stderr.lower())

    def test_dispatcher_rejects_brief_outside_workspace(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, _ = self.make_dispatch_project(root, config)
            outside = root / "outside-brief.md"
            outside.write_text("# Outside\n", encoding="utf-8")
            result = self.dispatch(
                dispatcher,
                "-BriefPath",
                str(outside),
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("brief", result.stderr.lower())
        self.assertIn("workspace", result.stderr.lower())

    def test_dispatcher_requires_workspace_override(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, _ = self.make_dispatch_project(root, config)
            override = (
                root
                / "active"
                / "sample"
                / "subagents"
                / self.DISPATCH_ID
                / "AGENTS.override.md"
            )
            override.unlink()
            result = self.dispatch(dispatcher)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("AGENTS.override.md", result.stderr)

    def test_dispatcher_requires_exactly_one_workspace_mode(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            neither = self.dispatch(dispatcher, mode_args=[])
            both = self.dispatch(
                dispatcher,
                mode_args=[
                    "-Slug",
                    "sample",
                    "-RecruitingCandidate",
                    "demo",
                ],
            )

        self.assertNotEqual(neither.returncode, 0)
        self.assertNotEqual(both.returncode, 0)

    def test_dispatcher_model_precedence(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{model_args}", "{policy_args}", "{prompt}"],
                    "model_flag": "-Model",
                    "model": "configured-model",
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            configured = self.dispatch(dispatcher)
            explicit = self.dispatch(dispatcher, "-Model", "explicit-model")

        self.assertEqual(configured.returncode, 0, configured.stderr)
        self.assertIn("model=configured-model", configured.stdout)
        self.assertEqual(explicit.returncode, 0, explicit.stderr)
        self.assertIn("model=explicit-model", explicit.stdout)

    def test_dispatcher_bypass_is_explicit(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{model_args}", "{policy_args}", "{prompt}"],
                    "model_flag": "-Model",
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            result = self.dispatch(dispatcher, "-Bypass")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("BYPASS (explicit)", result.stdout)

    def test_dispatcher_uses_candidate_config_and_profile(self) -> None:
        config = {
            "backend": "demo",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{model_args}", "{policy_args}", "{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, config_path = self.make_dispatch_project(
                root,
                config,
                candidate="demo",
            )
            result = self.dispatch(
                dispatcher,
                "-ConfigPath",
                str(config_path),
            )
            live_provider = (
                root
                / ".re-discipline"
                / "agents"
                / "providers"
                / "demo"
            )
            self.assertFalse(live_provider.exists())

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            ".re-discipline\\agents\\recruiting\\demo\\profile.md",
            result.stdout.replace("/", "\\"),
        )

    def test_dispatcher_uses_recruiting_run_workspace(self) -> None:
        config = {
            "backend": "demo",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            dispatcher, _ = self.make_dispatch_project(
                root,
                config,
                candidate="demo",
                recruiting=True,
            )
            result = self.dispatch(
                dispatcher,
                mode_args=["-RecruitingCandidate", "demo"],
                dispatch_id=self.RECRUITING_ID,
            )
            self.assertFalse((root / "active").exists())

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"workspace={self.RECRUITING_ID}", result.stdout)
        self.assertIn(
            ".re-discipline\\agents\\recruiting\\demo\\profile.md",
            result.stdout.replace("/", "\\"),
        )

    def test_dispatcher_rejects_recruiting_provider_mismatch(self) -> None:
        provider = {
            "command": "powershell.exe",
            "args": ["{prompt}"],
            "sandbox_args": ["-NoProfile"],
            "bypass_args": ["-NoProfile"],
        }
        config = {
            "backend": "demo",
            "providers": {"demo": provider, "other": provider},
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(
                Path(directory),
                config,
                candidate="demo",
                recruiting=True,
            )
            result = self.dispatch(
                dispatcher,
                provider="other",
                mode_args=["-RecruitingCandidate", "demo"],
                dispatch_id=self.RECRUITING_ID,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("candidate", result.stderr.lower())
        self.assertIn("provider", result.stderr.lower())

    def test_dispatcher_rejects_malformed_candidate_slugs(self) -> None:
        for candidate in ("bad--candidate", "bad-"):
            with self.subTest(candidate=candidate):
                provider = {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
                config = {
                    "backend": "demo",
                    "providers": {"demo": provider},
                }
                with tempfile.TemporaryDirectory() as directory:
                    dispatcher, _ = self.make_dispatch_project(
                        Path(directory),
                        config,
                        candidate=candidate,
                        recruiting=True,
                        dispatch_id=self.RECRUITING_ID,
                    )
                    result = self.dispatch(
                        dispatcher,
                        mode_args=["-RecruitingCandidate", candidate],
                        dispatch_id=self.RECRUITING_ID,
                    )

                self.assertNotEqual(result.returncode, 0)
                self.assertIn("candidate", result.stderr.lower())

    def test_dispatcher_rejects_config_path_in_recruiting_mode(self) -> None:
        config = {
            "backend": "demo",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, config_path = self.make_dispatch_project(
                Path(directory),
                config,
                candidate="demo",
                recruiting=True,
            )
            result = self.dispatch(
                dispatcher,
                "-ConfigPath",
                str(config_path),
                mode_args=["-RecruitingCandidate", "demo"],
                dispatch_id=self.RECRUITING_ID,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Parameter set", result.stderr)

    def test_dispatcher_requires_complete_recruiting_workspace(self) -> None:
        config = {
            "backend": "demo",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        relative_targets = (
            Path("candidate.md"),
            Path("config.json"),
            Path("profile.md"),
            Path("runs") / self.RECRUITING_ID / "brief.md",
            Path("runs") / self.RECRUITING_ID / "AGENTS.override.md",
        )
        for relative_target in relative_targets:
            with self.subTest(missing=str(relative_target)):
                with tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    dispatcher, _ = self.make_dispatch_project(
                        root,
                        config,
                        candidate="demo",
                        recruiting=True,
                    )
                    candidate_dir = (
                        root
                        / ".re-discipline"
                        / "agents"
                        / "recruiting"
                        / "demo"
                    )
                    (candidate_dir / relative_target).unlink()
                    result = self.dispatch(
                        dispatcher,
                        mode_args=["-RecruitingCandidate", "demo"],
                        dispatch_id=self.RECRUITING_ID,
                    )

                self.assertNotEqual(result.returncode, 0)

    def test_recruiting_override_paths_resolve(self) -> None:
        config = {
            "backend": "demo",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_dispatch_project(
                root,
                config,
                candidate="demo",
                recruiting=True,
            )
            workspace = (
                root
                / ".re-discipline"
                / "agents"
                / "recruiting"
                / "demo"
                / "runs"
                / self.RECRUITING_ID
            )
            for relative in (
                "../../../../../project-profile.md",
                "../../../../../../.codex/external-drafter-contract.md",
                "../../candidate.md",
                "../../profile.md",
                "brief.md",
            ):
                with self.subTest(relative=relative):
                    self.assertTrue((workspace / relative).resolve().is_file())

    def test_dispatcher_rejects_legacy_provider_keys(self) -> None:
        config = {
            "backend": "native",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                    "enabled": True,
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            result = self.dispatch(dispatcher)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unsupported provider key 'enabled'", result.stderr)

    def test_dispatcher_rejects_unknown_backend(self) -> None:
        config = {
            "backend": "missing",
            "providers": {
                "demo": {
                    "command": "powershell.exe",
                    "args": ["{prompt}"],
                    "sandbox_args": ["-NoProfile"],
                    "bypass_args": ["-NoProfile"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            dispatcher, _ = self.make_dispatch_project(Path(directory), config)
            result = self.dispatch(dispatcher)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "Backend 'missing' is not native or a configured provider",
            result.stderr,
        )


class HookTests(unittest.TestCase):
    def setUp(self) -> None:
        self.hook = PLUGIN / "hooks" / "re-discipline-hook.ps1"
        self.shell_hook = PLUGIN / "hooks" / "re-discipline-hook.sh"
        self.hook_config = json.loads(read(PLUGIN / "hooks" / "hooks.json"))
        self.powershell = shutil.which("powershell.exe") or shutil.which("powershell")
        self.sh = shutil.which("sh")
        if not self.sh:
            git = shutil.which("git.exe") or shutil.which("git")
            if git:
                candidate = Path(git).resolve().parent.parent / "bin" / "sh.exe"
                if candidate.is_file():
                    self.sh = str(candidate)

    def run_hook(
        self,
        event: str,
        cwd: Path,
        *,
        host: str = "claude",
        source: str | None = None,
        extra_env: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        if not self.powershell:
            self.skipTest("PowerShell is required for Windows hook tests")
        env = os.environ.copy()
        env.pop("PLUGIN_ROOT", None)
        env.pop("CLAUDE_PLUGIN_ROOT", None)
        if host == "codex":
            env["PLUGIN_ROOT"] = str(PLUGIN)
        else:
            env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN)
        if extra_env:
            env.update(extra_env)

        payload = {"cwd": str(cwd), "hook_event_name": event}
        if source is not None:
            payload["source"] = source

        return subprocess.run(
            [
                self.powershell,
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(self.hook),
                event,
            ],
            input=json.dumps(payload),
            text=True,
            capture_output=True,
            check=False,
            env=env,
        )

    def run_posix_hook(
        self,
        event: str,
        cwd: Path,
        *,
        host: str = "claude",
        extra_env: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        if not self.sh:
            self.skipTest("A POSIX shell is required for hook parity tests")

        def shell_path(path: Path) -> str:
            if os.name != "nt":
                return str(path)
            result = subprocess.run(
                [self.sh, "-lc", 'cygpath -u "$1"', "sh", str(path)],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            return result.stdout.strip()

        env = os.environ.copy()
        env.pop("PLUGIN_ROOT", None)
        env.pop("CLAUDE_PLUGIN_ROOT", None)
        env["CLAUDE_PROJECT_DIR"] = shell_path(cwd)
        plugin_path = shell_path(PLUGIN)
        if host == "codex":
            env["PLUGIN_ROOT"] = plugin_path
        else:
            env["CLAUDE_PLUGIN_ROOT"] = plugin_path
        if extra_env:
            env.update(extra_env)

        return subprocess.run(
            [self.sh, shell_path(self.shell_hook), event],
            input=json.dumps({"hook_event_name": event}),
            text=True,
            capture_output=True,
            check=False,
            env=env,
        )

    def make_managed_project(self, root: Path) -> None:
        managed = root / ".re-discipline"
        managed.mkdir(parents=True)
        (managed / "project-profile.md").write_text(
            "---\nname: managed-test\n---\n"
            "<!-- re-discipline:shared-laws v0.6.0 -->\n"
            "# Managed test\n"
            "<!-- re-discipline:shared-laws:end -->\n",
            encoding="utf-8",
        )

    def assert_ambiguous_host_settings_preserved(self, runner) -> None:
        cases = (
            (
                "claude-root-array",
                ".claude/settings.json",
                b'[{"autoMemoryEnabled": true}]\n',
            ),
            (
                "claude-escaped-managed-key",
                ".claude/settings.json",
                b'{"auto\\u004demoryEnabled": true}\n',
            ),
            (
                "claude-duplicate-managed-key",
                ".claude/settings.json",
                b'{"autoMemoryEnabled": true, "autoMemoryEnabled": false}\n',
            ),
            (
                "codex-root-inline-table",
                ".codex/config.toml",
                b"features = { memories = true }\n",
            ),
            (
                "codex-root-dotted-key",
                ".codex/config.toml",
                b"features.memories = true\n",
            ),
            (
                "codex-quoted-table",
                ".codex/config.toml",
                b'["features"]\nmemories = true\n',
            ),
            (
                "codex-array-table",
                ".codex/config.toml",
                b"[[features]]\nmemories = true\n",
            ),
            (
                "codex-unclosed-multiline-string",
                ".codex/config.toml",
                b'description = """\nmanaged-looking text\n',
            ),
            (
                "codex-unbalanced-array",
                ".codex/config.toml",
                b'args = [\n  "a",\n',
            ),
            (
                "codex-plain-array-table",
                ".codex/config.toml",
                b'[[servers]]\nname = "a"\n',
            ),
            (
                "codex-unterminated-string",
                ".codex/config.toml",
                b'model = "unterminated\n',
            ),
        )
        for label, relative, original in cases:
            with self.subTest(case=label), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                self.make_managed_project(root)
                shutil.copy2(
                    PLUGIN / "templates" / "project" / "config.json",
                    root / ".re-discipline" / "config.json",
                )
                target = root / relative
                target.parent.mkdir()
                target.write_bytes(original)

                result = runner("session-start", root, host="codex")
                after = target.read_bytes()

                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(after, original)
                context = json.loads(result.stdout)["hookSpecificOutput"][
                    "additionalContext"
                ]
                self.assertRegex(context, r"ambiguous|malformed|JSON object")
                self.assertIn("preserved", context)

    def make_control_plane(self, root: Path, profile_id: str) -> Path:
        """Materialize a managed project whose retrieval profile is promoted."""
        self.make_managed_project(root)
        shutil.copy2(
            PLUGIN / "templates" / "project" / "config.json",
            root / ".re-discipline" / "config.json",
        )
        settings = root / ".re-discipline" / "settings"
        settings.mkdir(parents=True)
        template = json.loads(
            read(PLUGIN / "templates" / "project" / "retrieval-profile.json")
        )
        baseline = template["profileId"]
        template["profileId"] = profile_id
        if profile_id != baseline:
            template["baseProfile"] = baseline
        path = settings / "retrieval-profile.json"
        path.write_text(json.dumps(template, indent=2) + "\n", encoding="utf-8")
        return path

    def hook_context(self, result: subprocess.CompletedProcess[str]) -> str:
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]

    def assert_promoted_project_profile_accepted(self, runner) -> None:
        """A calibrated project keeps its own profile identity.

        decide-retrieval-profile writes "project:candidate-<hex>", which the
        packaged server accepts under profileIdentityRE. A hook that demanded
        the packaged baseline identity reported every calibrated project as
        degraded at every session start.
        """
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_control_plane(root, "project:candidate-12750fb43b2f9efd")

            result = runner("session-start", root, host="codex")
            context = self.hook_context(result)

        self.assertNotIn("retrieval-profile", context)
        self.assertNotIn("read-only degraded", context)

    def assert_malformed_project_profile_warns(self, runner) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = self.make_control_plane(root, "plugin:balanced-v1")
            body = json.loads(read(path))
            body["profileId"] = "Project Candidate!"
            path.write_text(json.dumps(body, indent=2) + "\n", encoding="utf-8")

            result = runner("session-start", root, host="codex")
            context = self.hook_context(result)

        self.assertIn("retrieval-profile.json", context)
        self.assertIn("read-only degraded", context)

    MULTILINE_CODEX_CONFIG = (
        "# the managed-looking keys below are string content, not structure\n"
        'model = "gpt-test"\n'
        'notes = """\n'
        "first line\n"
        "[features]\n"
        "memories = true\n"
        '"""\n'
        'windows_path = "C:\\\\Users\\\\x"\n'
        'hashy = "value#notacomment"\n'
        "\n"
        "[mcp_servers.alpha.tools.beta]\n"
        'command = "npx"\n'
        "args = [\n"
        '  "-y",\n'
        '  "some-server"\n'
        "]\n"
    )

    def assert_multiline_toml_config_repaired(self, runner) -> None:
        """Multi-line TOML is structure the hook must scan, not give up on.

        The packaged Go recovery scanner tracks open multi-line strings and
        bracket depth, so interior lines are value content. Hooks that bailed
        on any triple quote reported an ordinary Codex configuration as
        malformed and never applied the memory policy.
        """
        original = self.MULTILINE_CODEX_CONFIG.encode("utf-8")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_control_plane(root, "plugin:balanced-v1")
            codex_path = root / ".codex" / "config.toml"
            codex_path.parent.mkdir()
            codex_path.write_bytes(original)

            result = runner("session-start", root, host="codex")
            repaired = codex_path.read_bytes()
            again = runner("session-start", root, host="codex")
            settled = codex_path.read_bytes()

        context = self.hook_context(result)
        self.hook_context(again)
        self.assertNotIn("preserved", context)
        self.assertNotIn("malformed", context)
        self.assertTrue(repaired.startswith(original), repaired)
        self.assertEqual(repaired, settled)

        text = repaired.decode("utf-8")
        self.assertIn("[features]\nmemories = false", text)
        self.assertIn("generate_memories = false", text)
        self.assertIn("use_memories = false", text)
        self.assertIn("[mcp_servers.alpha.tools.beta]", text)
        self.assertIn('windows_path = "C:\\\\Users\\\\x"', text)
        self.assertIn('hashy = "value#notacomment"', text)
        self.assertEqual(text.count("memories = true"), 1)

    def test_hook_is_silent_outside_initialized_project(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_hook("session-start", Path(directory))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "")

    def test_session_start_runs_bounded_read_only_knowledge_status(self) -> None:
        powershell_body = read(self.hook)
        self.assertIn("Get-KnowledgeHealthSummary", powershell_body)
        self.assertIn("$process.WaitForExit(3000)", powershell_body)
        self.assertIn('"status"', powershell_body)
        session_handler = self.hook_config["hooks"]["SessionStart"][0]["hooks"][0]
        self.assertLessEqual(session_handler["timeout"], 10)

        if not self.sh:
            self.skipTest("A POSIX shell is required for health-status execution")

        def shell_path(path: Path) -> str:
            if os.name != "nt":
                return str(path)
            result = subprocess.run(
                [self.sh, "-lc", 'cygpath -u "$1"', "sh", str(path)],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            return result.stdout.strip()

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "project"
            fake_plugin = base / "plugin"
            marker = base / "status-arguments.txt"
            self.make_managed_project(root)
            shutil.copytree(PLUGIN / "templates", fake_plugin / "templates")
            shutil.copytree(PLUGIN / "hooks", fake_plugin / "hooks")
            launcher = (
                fake_plugin
                / "knowledge"
                / "bin"
                / "re-discipline-knowledge"
            )
            launcher.parent.mkdir(parents=True)
            launcher.write_text(
                "#!/bin/sh\n"
                'printf "%s\\n" "$*" > "$HEALTH_MARKER"\n',
                encoding="utf-8",
            )
            launcher.chmod(0o755)

            result = self.run_posix_hook(
                "session-start",
                root,
                host="codex",
                extra_env={
                    "PLUGIN_ROOT": shell_path(fake_plugin),
                    "HEALTH_MARKER": shell_path(marker),
                },
            )
            arguments = marker.read_text(encoding="utf-8").strip()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            arguments,
            "status "
            f"--asset-root {shell_path(fake_plugin)}/knowledge "
            f"--project-root {shell_path(root)}",
        )
        context = json.loads(result.stdout)["hookSpecificOutput"][
            "additionalContext"
        ]
        self.assertNotIn("packaged runtime is missing", context)

    def test_codex_hook_injects_complete_neutral_profile(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".re-discipline").mkdir()
            profile = "---\nname: test\n---\n# Canonical \"Profile\"\npath: C:\\test\n"
            (root / ".re-discipline" / "project-profile.md").write_text(
                profile, encoding="utf-8"
            )
            result = self.run_hook("session-start", root, host="codex")

        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(
            output["hookSpecificOutput"]["hookEventName"],
            "SessionStart",
        )
        self.assertEqual(
            output["hookSpecificOutput"]["additionalContext"],
            profile,
        )

    def test_codex_hook_does_not_inject_machine_local_values(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".re-discipline").mkdir()
            profile = "---\nname: test\n---\n# Canonical profile\n"
            (root / ".re-discipline" / "project-profile.md").write_text(
                profile, encoding="utf-8"
            )
            private_sentinel = "PRIVATE_LOCAL_VALUE_MUST_NOT_ENTER_CONTEXT"
            (root / ".re-discipline" / "local-paths.md").write_text(
                f'$PRIVATE = "{private_sentinel}"\n',
                encoding="utf-8",
            )
            result = self.run_hook("session-start", root, host="codex")

        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        context = output["hookSpecificOutput"]["additionalContext"]
        self.assertEqual(context, profile)
        self.assertNotIn(private_sentinel, context)

    def test_codex_hook_discovers_project_from_nested_working_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            nested = root / "src" / "deep"
            nested.mkdir(parents=True)
            (root / ".re-discipline").mkdir()
            profile = "---\nname: nested\n---\n# Nested profile\n"
            (root / ".re-discipline" / "project-profile.md").write_text(
                profile, encoding="utf-8"
            )
            result = self.run_hook("session-start", nested, host="codex")

        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(
            output["hookSpecificOutput"]["additionalContext"],
            profile,
        )

    def test_claude_hook_emits_onboarding_without_duplicate_profile(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".re-discipline").mkdir()
            profile = "---\nname: claude\n---\n# Must not be duplicated\n"
            (root / ".re-discipline" / "project-profile.md").write_text(
                profile, encoding="utf-8"
            )
            result = self.run_hook("session-start", root, host="claude")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("onboard", result.stdout.lower())
        self.assertNotIn("# Must not be duplicated", result.stdout)

    def test_hook_reports_legacy_profile_as_migration_input(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".claude").mkdir()
            (root / ".claude" / "project-profile.md").write_text(
                "---\nname: legacy\n---\n", encoding="utf-8"
            )
            result = self.run_hook("session-start", root, host="codex")

        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        context = output["hookSpecificOutput"]["additionalContext"].lower()
        self.assertIn("legacy", context)
        self.assertIn("migration", context)
        self.assertIn("init-project", context)

    def test_session_start_materializes_shared_only_control_plane(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            result = self.run_hook("session-start", root, host="codex")

            config = json.loads(read(root / ".re-discipline" / "config.json"))
            claude = json.loads(read(root / ".claude" / "settings.json"))
            codex = read(root / ".codex" / "config.toml")
            expected_paths = (
                ".re-discipline/settings/README.md",
                ".re-discipline/settings/knowledge.jsonc",
                ".re-discipline/settings/retrieval-profile.json",
                ".re-discipline/memory/INDEX.md",
                ".re-discipline/knowledge/evals/README.md",
            )
            existing = tuple((root / path).is_file() for path in expected_paths)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(existing))
        self.assertEqual(config["memory"]["mode"], "shared-only")
        self.assertEqual(config["memory"]["writePolicy"], "proposal-only")
        self.assertFalse(claude["autoMemoryEnabled"])
        self.assertIn("[features]\nmemories = false", codex)
        self.assertIn("generate_memories = false", codex)
        self.assertIn("use_memories = false", codex)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("Recovered managed project files", context)

    def test_hook_repairs_only_managed_host_memory_fields(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            shutil.copy2(
                PLUGIN / "templates" / "project" / "config.json",
                root / ".re-discipline" / "config.json",
            )
            claude_path = root / ".claude" / "settings.json"
            claude_path.parent.mkdir()
            claude_path.write_text(
                '{\n  "permissions": {"allow": ["Read"]},\n'
                '  "autoMemoryEnabled": true\n}\n',
                encoding="utf-8",
            )
            codex_path = root / ".codex" / "config.toml"
            codex_path.parent.mkdir()
            codex_path.write_text(
                '# keep this comment\nmodel = "gpt-test"\n\n'
                "[features]\nweb_search = true # unrelated\n\n"
                "[memories]\nmax_unused_days = 30\n",
                encoding="utf-8",
            )

            result = self.run_hook("session-start", root, host="codex")
            first_claude = claude_path.read_bytes()
            first_codex = codex_path.read_bytes()
            result_again = self.run_hook("session-start", root, host="codex")
            second_claude = claude_path.read_bytes()
            second_codex = codex_path.read_bytes()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result_again.returncode, 0, result_again.stderr)
        settings = json.loads(first_claude.decode("utf-8"))
        self.assertEqual(settings["permissions"], {"allow": ["Read"]})
        self.assertFalse(settings["autoMemoryEnabled"])
        codex = first_codex.decode("utf-8")
        self.assertIn("# keep this comment", codex)
        self.assertIn('model = "gpt-test"', codex)
        self.assertIn("web_search = true # unrelated", codex)
        self.assertIn("max_unused_days = 30", codex)
        self.assertIn("memories = false", codex)
        self.assertIn("generate_memories = false", codex)
        self.assertIn("use_memories = false", codex)
        self.assertEqual(first_claude, second_claude)
        self.assertEqual(first_codex, second_codex)

    def test_hook_preserves_malformed_existing_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            config_path = root / ".re-discipline" / "config.json"
            malformed_config = b'{"schemaVersion": 1, "memory": '
            config_path.write_bytes(malformed_config)

            result = self.run_hook("session-start", root, host="codex")
            preserved = config_path.read_bytes()
            hosts_absent = (
                not (root / ".claude" / "settings.json").exists()
                and not (root / ".codex" / "config.toml").exists()
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(preserved, malformed_config)
        self.assertTrue(hosts_absent)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("read-only degraded", context)
        self.assertIn("malformed", context)

    def test_hook_preserves_malformed_host_settings_byte_for_byte(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            shutil.copy2(
                PLUGIN / "templates" / "project" / "config.json",
                root / ".re-discipline" / "config.json",
            )
            claude_path = root / ".claude" / "settings.json"
            claude_path.parent.mkdir()
            claude_original = b'{"autoMemoryEnabled": false'
            claude_path.write_bytes(claude_original)
            codex_path = root / ".codex" / "config.toml"
            codex_path.parent.mkdir()
            codex_original = b"{ definitely-not-toml\n"
            codex_path.write_bytes(codex_original)

            result = self.run_hook("session-start", root, host="codex")
            claude_after = claude_path.read_bytes()
            codex_after = codex_path.read_bytes()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(claude_after, claude_original)
        self.assertEqual(codex_after, codex_original)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("malformed", context)
        self.assertIn("preserved", context)

    def test_hook_preserves_ambiguous_host_settings_byte_for_byte(self) -> None:
        self.assert_ambiguous_host_settings_preserved(self.run_hook)

    def test_hook_accepts_a_promoted_project_retrieval_profile(self) -> None:
        self.assert_promoted_project_profile_accepted(self.run_hook)

    def test_hook_still_warns_on_a_malformed_profile_identity(self) -> None:
        self.assert_malformed_project_profile_warns(self.run_hook)

    def test_hook_repairs_configuration_with_multiline_toml(self) -> None:
        self.assert_multiline_toml_config_repaired(self.run_hook)

    def test_hook_recovers_staged_deletions_without_changing_index(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            first = self.run_hook("session-start", root, host="codex")
            self.assertEqual(first.returncode, 0, first.stderr)
            subprocess.run(["git", "init", str(root)], check=True, capture_output=True)
            subprocess.run(
                ["git", "-C", str(root), "config", "user.name", "Test"],
                check=True,
            )
            subprocess.run(
                ["git", "-C", str(root), "config", "user.email", "test@example.invalid"],
                check=True,
            )
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
            subprocess.run(
                ["git", "-C", str(root), "commit", "-m", "fixture"],
                check=True,
                capture_output=True,
            )
            deleted = (
                ".re-discipline/config.json",
                ".re-discipline/settings/knowledge.jsonc",
            )
            expected = {
                path: (root / path).read_bytes()
                for path in deleted
            }
            for path in deleted:
                (root / path).unlink()
            subprocess.run(
                ["git", "-C", str(root), "add", "-u", "--", *deleted],
                check=True,
            )
            cached_before = subprocess.run(
                ["git", "-C", str(root), "diff", "--cached", "--name-status", "--", *deleted],
                text=True,
                capture_output=True,
                check=True,
            ).stdout

            result = self.run_hook("session-start", root, host="codex")
            cached_after = subprocess.run(
                ["git", "-C", str(root), "diff", "--cached", "--name-status", "--", *deleted],
                text=True,
                capture_output=True,
                check=True,
            ).stdout
            restored = {
                path: (root / path).read_bytes()
                for path in deleted
            }

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(cached_before, cached_after)
        self.assertEqual(restored, expected)
        for path in deleted:
            self.assertIn(f"D\t{path}", cached_after)

    def test_hook_never_accesses_configured_native_memory_roots(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "project"
            root.mkdir()
            self.make_managed_project(root)
            native = Path(directory) / "native"
            claude_native = native / "claude" / "projects" / "memory"
            codex_native = native / "codex" / "memories"
            claude_native.mkdir(parents=True)
            codex_native.mkdir(parents=True)
            claude_sentinel = claude_native / "MEMORY.md"
            codex_sentinel = codex_native / "MEMORY.md"
            claude_sentinel.write_text("CLAUDE_NATIVE_SENTINEL", encoding="utf-8")
            codex_sentinel.write_text("CODEX_NATIVE_SENTINEL", encoding="utf-8")

            result = self.run_hook(
                "session-start",
                root,
                host="codex",
                extra_env={
                    "CLAUDE_CONFIG_DIR": str(native / "claude"),
                    "CODEX_HOME": str(native / "codex"),
                },
            )
            claude_after = claude_sentinel.read_text(encoding="utf-8")
            codex_after = codex_sentinel.read_text(encoding="utf-8")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(claude_after, "CLAUDE_NATIVE_SENTINEL")
        self.assertEqual(codex_after, "CODEX_NATIVE_SENTINEL")

    def test_compaction_and_subagent_hooks_inject_bounded_context(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            post = self.run_hook("post-compact", root, host="codex")
            compatibility = self.run_hook(
                "session-start",
                root,
                host="codex",
                source="compact",
            )
            subagent = self.run_hook("subagent-start", root, host="codex")

        self.assertEqual(post.returncode, 0, post.stderr)
        self.assertEqual(compatibility.returncode, 0, compatibility.stderr)
        self.assertEqual(subagent.returncode, 0, subagent.stderr)
        post_output = json.loads(post.stdout)["hookSpecificOutput"]
        compatibility_output = json.loads(compatibility.stdout)["hookSpecificOutput"]
        subagent_output = json.loads(subagent.stdout)["hookSpecificOutput"]
        self.assertEqual(post_output["hookEventName"], "PostCompact")
        self.assertIn("bounded orientation", post_output["additionalContext"])
        self.assertEqual(compatibility_output["hookEventName"], "SessionStart")
        self.assertIn("after compaction", compatibility_output["additionalContext"])
        self.assertEqual(subagent_output["hookEventName"], "SubagentStart")
        self.assertIn("drafter, not a ratifier", subagent_output["additionalContext"])
        self.assertIn("immutable context pack", subagent_output["additionalContext"])

    def test_posix_session_start_materializes_shared_only_control_plane(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            result = self.run_posix_hook("session-start", root, host="codex")

            config = json.loads(read(root / ".re-discipline" / "config.json"))
            claude = json.loads(read(root / ".claude" / "settings.json"))
            codex = read(root / ".codex" / "config.toml")
            expected_paths = (
                ".re-discipline/settings/README.md",
                ".re-discipline/settings/knowledge.jsonc",
                ".re-discipline/settings/retrieval-profile.json",
                ".re-discipline/memory/INDEX.md",
                ".re-discipline/knowledge/evals/README.md",
            )
            existing = tuple((root / path).is_file() for path in expected_paths)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(existing))
        self.assertEqual(config["memory"]["mode"], "shared-only")
        self.assertEqual(config["memory"]["writePolicy"], "proposal-only")
        self.assertFalse(claude["autoMemoryEnabled"])
        self.assertIn("[features]\nmemories = false", codex)
        self.assertIn("generate_memories = false", codex)
        self.assertIn("use_memories = false", codex)

    def test_posix_hook_repairs_only_managed_host_memory_fields(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            shutil.copy2(
                PLUGIN / "templates" / "project" / "config.json",
                root / ".re-discipline" / "config.json",
            )
            claude_path = root / ".claude" / "settings.json"
            claude_path.parent.mkdir()
            claude_path.write_text(
                '{\n  "permissions": {"allow": ["Read"]},\n'
                '  "autoMemoryEnabled": true\n}\n',
                encoding="utf-8",
            )
            codex_path = root / ".codex" / "config.toml"
            codex_path.parent.mkdir()
            codex_path.write_text(
                '# keep this comment\nmodel = "gpt-test"\n\n'
                "[features]\nweb_search = true # unrelated\n\n"
                "[memories]\nmax_unused_days = 30\n",
                encoding="utf-8",
            )

            result = self.run_posix_hook("session-start", root, host="codex")
            first_claude = claude_path.read_bytes()
            first_codex = codex_path.read_bytes()
            result_again = self.run_posix_hook("session-start", root, host="codex")
            second_claude = claude_path.read_bytes()
            second_codex = codex_path.read_bytes()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result_again.returncode, 0, result_again.stderr)
        settings = json.loads(first_claude.decode("utf-8"))
        self.assertEqual(settings["permissions"], {"allow": ["Read"]})
        self.assertFalse(settings["autoMemoryEnabled"])
        codex = first_codex.decode("utf-8")
        self.assertIn("# keep this comment", codex)
        self.assertIn('model = "gpt-test"', codex)
        self.assertIn("web_search = true # unrelated", codex)
        self.assertIn("max_unused_days = 30", codex)
        self.assertIn("memories = false", codex)
        self.assertIn("generate_memories = false", codex)
        self.assertIn("use_memories = false", codex)
        self.assertEqual(first_claude, second_claude)
        self.assertEqual(first_codex, second_codex)

    def test_posix_hook_fails_closed_on_malformed_config(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            config_path = root / ".re-discipline" / "config.json"
            malformed = (
                read(PLUGIN / "templates" / "project" / "config.json")
                + "\nmalformed trailing text\n"
            ).encode("utf-8")
            config_path.write_bytes(malformed)

            result = self.run_posix_hook("session-start", root, host="codex")
            preserved = config_path.read_bytes()
            host_absent = not (root / ".claude" / "settings.json").exists()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(preserved, malformed)
        self.assertTrue(host_absent)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("read-only degraded", context)

    def test_posix_hook_preserves_malformed_host_settings_byte_for_byte(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            shutil.copy2(
                PLUGIN / "templates" / "project" / "config.json",
                root / ".re-discipline" / "config.json",
            )
            claude_path = root / ".claude" / "settings.json"
            claude_path.parent.mkdir()
            claude_original = b'{"autoMemoryEnabled": false'
            claude_path.write_bytes(claude_original)
            codex_path = root / ".codex" / "config.toml"
            codex_path.parent.mkdir()
            codex_original = b"{ definitely-not-toml\n"
            codex_path.write_bytes(codex_original)

            result = self.run_posix_hook("session-start", root, host="codex")
            claude_after = claude_path.read_bytes()
            codex_after = codex_path.read_bytes()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(claude_after, claude_original)
        self.assertEqual(codex_after, codex_original)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("malformed", context)
        self.assertIn("preserved", context)

    def test_posix_hook_preserves_ambiguous_host_settings_byte_for_byte(
        self,
    ) -> None:
        self.assert_ambiguous_host_settings_preserved(self.run_posix_hook)

    def test_posix_hook_accepts_a_promoted_project_retrieval_profile(self) -> None:
        self.assert_promoted_project_profile_accepted(self.run_posix_hook)

    def test_posix_hook_still_warns_on_a_malformed_profile_identity(self) -> None:
        self.assert_malformed_project_profile_warns(self.run_posix_hook)

    def test_posix_hook_repairs_configuration_with_multiline_toml(self) -> None:
        self.assert_multiline_toml_config_repaired(self.run_posix_hook)

    def test_posix_hook_does_not_follow_managed_directory_symlinks(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "project"
            root.mkdir()
            self.make_managed_project(root)
            shutil.copy2(
                PLUGIN / "templates" / "project" / "config.json",
                root / ".re-discipline" / "config.json",
            )
            outside = Path(directory) / "outside"
            outside.mkdir()
            sentinel = outside / "sentinel.txt"
            sentinel.write_text("OUTSIDE_SENTINEL", encoding="utf-8")
            settings_link = root / ".re-discipline" / "settings"
            try:
                settings_link.symlink_to(outside, target_is_directory=True)
            except OSError as error:
                self.skipTest(f"directory symlinks are unavailable: {error}")

            result = self.run_posix_hook("session-start", root, host="codex")
            sentinel_after = sentinel.read_text(encoding="utf-8")
            escaped_files = tuple(
                (outside / name).exists()
                for name in ("README.md", "knowledge.jsonc", "retrieval-profile.json")
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sentinel_after, "OUTSIDE_SENTINEL")
        self.assertFalse(any(escaped_files))
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("escapes through a link", context)

    @unittest.skipIf(
        os.name == "nt" or (hasattr(os, "geteuid") and os.geteuid() == 0),
        "POSIX permissions require a non-root POSIX test process",
    )
    def test_posix_hook_degrades_without_terminating_on_read_only_project(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            managed = root / ".re-discipline"
            managed.chmod(0o555)
            try:
                result = self.run_posix_hook("session-start", root, host="codex")
                config_absent = not (managed / "config.json").exists()
            finally:
                managed.chmod(0o755)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(config_absent)
        context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
        self.assertIn("read-only degraded", context)
        self.assertIn("could not be created", context)

    def test_posix_hook_never_accesses_configured_native_memory_roots(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "project"
            root.mkdir()
            self.make_managed_project(root)
            native = Path(directory) / "native"
            claude_native = native / "claude" / "projects" / "memory"
            codex_native = native / "codex" / "memories"
            claude_native.mkdir(parents=True)
            codex_native.mkdir(parents=True)
            claude_sentinel = claude_native / "MEMORY.md"
            codex_sentinel = codex_native / "MEMORY.md"
            claude_sentinel.write_text("CLAUDE_NATIVE_SENTINEL", encoding="utf-8")
            codex_sentinel.write_text("CODEX_NATIVE_SENTINEL", encoding="utf-8")

            result = self.run_posix_hook(
                "session-start",
                root,
                host="codex",
                extra_env={
                    "CLAUDE_CONFIG_DIR": str(native / "claude"),
                    "CODEX_HOME": str(native / "codex"),
                },
            )
            claude_after = claude_sentinel.read_text(encoding="utf-8")
            codex_after = codex_sentinel.read_text(encoding="utf-8")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(claude_after, "CLAUDE_NATIVE_SENTINEL")
        self.assertEqual(codex_after, "CODEX_NATIVE_SENTINEL")

    def test_posix_hook_recovers_staged_deletions_without_changing_index(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_managed_project(root)
            first = self.run_posix_hook("session-start", root, host="codex")
            self.assertEqual(first.returncode, 0, first.stderr)
            subprocess.run(["git", "init", str(root)], check=True, capture_output=True)
            subprocess.run(
                ["git", "-C", str(root), "config", "user.name", "Test"],
                check=True,
            )
            subprocess.run(
                ["git", "-C", str(root), "config", "user.email", "test@example.invalid"],
                check=True,
            )
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
            subprocess.run(
                ["git", "-C", str(root), "commit", "-m", "fixture"],
                check=True,
                capture_output=True,
            )
            deleted = (
                ".re-discipline/config.json",
                ".re-discipline/settings/knowledge.jsonc",
            )
            expected = {path: (root / path).read_bytes() for path in deleted}
            for path in deleted:
                (root / path).unlink()
            subprocess.run(
                ["git", "-C", str(root), "add", "-u", "--", *deleted],
                check=True,
            )
            cached_before = subprocess.run(
                ["git", "-C", str(root), "diff", "--cached", "--name-status", "--", *deleted],
                text=True,
                capture_output=True,
                check=True,
            ).stdout

            result = self.run_posix_hook("session-start", root, host="codex")
            cached_after = subprocess.run(
                ["git", "-C", str(root), "diff", "--cached", "--name-status", "--", *deleted],
                text=True,
                capture_output=True,
                check=True,
            ).stdout
            restored = {path: (root / path).read_bytes() for path in deleted}

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(cached_before, cached_after)
        self.assertEqual(restored, expected)
        for path in deleted:
            self.assertIn(f"D\t{path}", cached_after)

    def test_windows_overrides_execute_the_packaged_commands(self) -> None:
        if os.name != "nt":
            self.skipTest("Windows is required for Windows hook tests")
        if not self.powershell:
            self.skipTest("PowerShell is required for Windows hook tests")
        event_expectations = {
            "SessionStart": "onboard",
            "PreCompact": "checkpoint",
            "PostCompact": "rehydrate",
            "SubagentStart": "drafter",
        }

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".re-discipline").mkdir()
            (root / ".re-discipline" / "project-profile.md").write_text(
                "---\nname: test\n---\n", encoding="utf-8"
            )
            env = os.environ.copy()
            env["PLUGIN_ROOT"] = str(PLUGIN)
            env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN)

            for event_name, expected in event_expectations.items():
                with self.subTest(event=event_name):
                    handler = self.hook_config["hooks"][event_name][0]["hooks"][0]
                    self.assertIn("commandWindows", handler)
                    result = subprocess.run(
                        [self.powershell, "-NoProfile", "-Command", handler["commandWindows"]],
                        input=json.dumps(
                            {"cwd": str(root), "hook_event_name": event_name}
                        ),
                        text=True,
                        capture_output=True,
                        check=False,
                        env=env,
                    )

                    self.assertEqual(result.returncode, 0, result.stderr)
                    if event_name in ("SessionStart", "PostCompact", "SubagentStart"):
                        output = json.loads(result.stdout)
                        context = output["hookSpecificOutput"]["additionalContext"]
                        if event_name == "SessionStart":
                            self.assertIn("name: test", context)
                        else:
                            self.assertIn(expected, context.lower())
                    else:
                        self.assertIn(expected, result.stdout.lower())


if __name__ == "__main__":
    unittest.main()
