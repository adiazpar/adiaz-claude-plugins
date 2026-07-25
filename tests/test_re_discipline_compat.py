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
        self.assertEqual(manifest["version"], "0.4.0")
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

        self.assertEqual(claude["version"], codex["version"])


class SkillMetadataTests(unittest.TestCase):
    def test_every_skill_has_portable_frontmatter(self) -> None:
        skill_paths = sorted((PLUGIN / "skills").glob("*/SKILL.md"))
        self.assertEqual(len(skill_paths), 11)

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

    def test_delegate_has_native_claude_and_codex_adapters(self) -> None:
        body = read(PLUGIN / "skills" / "delegate" / "SKILL.md")

        self.assertIn("spawn_agent", body)
        self.assertIn("Claude Code", body)
        self.assertIn("Codex", body)
        self.assertIn(".codex/external-drafter-contract.md", body)

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
            "project-profile.md",
        }

        self.assertTrue(expected.issubset({path.name for path in self.templates.iterdir()}))
        self.assertFalse((self.templates / "claude-project-profile.md").exists())
        self.assertFalse((self.templates / "codex-project-profile.md").exists())
        self.assertEqual(
            [path.name for path in self.templates.glob("*project-profile.md")],
            ["project-profile.md"],
        )

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


class DocumentationTests(unittest.TestCase):
    def test_plugin_readme_explains_both_install_and_init_flows(self) -> None:
        body = read(PLUGIN / "README.md")

        self.assertIn("codex plugin marketplace add", body)
        self.assertIn("codex plugin add re-discipline@adiaz-claude-plugins", body)
        self.assertIn("$re-discipline:init-project", body)
        self.assertIn("/re-discipline:init-project", body)
        self.assertIn(".re-discipline/project-profile.md", body)
        self.assertIn("one canonical project profile", body.lower())
        self.assertNotIn("Yes, a new project gets `.codex/project-profile.md`", body)
        self.assertIn("/hooks", body)

    def test_repository_readme_identifies_the_codex_marketplace(self) -> None:
        body = read(ROOT / "README.md")

        self.assertIn(".agents/plugins/marketplace.json", body)
        self.assertIn("Codex", body)


class ExternalDispatcherTests(unittest.TestCase):
    def setUp(self) -> None:
        self.powershell = shutil.which("powershell.exe") or shutil.which("powershell")
        if not self.powershell:
            self.skipTest("PowerShell is required for dispatcher tests")

    def make_dispatch_project(
        self,
        root: Path,
        config: dict,
        *,
        candidate: str | None = None,
    ) -> tuple[Path, Path]:
        templates = PLUGIN / "templates" / "project"
        agents = root / ".re-discipline" / "agents"
        agents.mkdir(parents=True)
        shutil.copy2(templates / "dispatch.ps1", agents / "dispatch.ps1")

        profile = root / ".re-discipline" / "project-profile.md"
        profile.parent.mkdir(parents=True, exist_ok=True)
        profile.write_text("---\nname: sample\n---\n", encoding="utf-8")

        if candidate is None:
            config_path = agents / "config.json"
            provider_profile = agents / "providers" / "demo" / "profile.md"
        else:
            candidate_dir = agents / "recruiting" / candidate
            candidate_dir.mkdir(parents=True)
            config_path = candidate_dir / "config.json"
            provider_profile = candidate_dir / "profile.md"

        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(json.dumps(config), encoding="utf-8")
        provider_profile.parent.mkdir(parents=True, exist_ok=True)
        provider_profile.write_text("# Demo provider\n", encoding="utf-8")

        campaign = root / "active" / "sample"
        workspace = campaign / "subagents" / "demo-task"
        workspace.mkdir(parents=True)
        (campaign / "CAMPAIGN.md").write_text("# Campaign\n", encoding="utf-8")
        (workspace / "brief.md").write_text("# Brief\n", encoding="utf-8")
        (workspace / "AGENTS.override.md").write_text(
            "# Drafter\n", encoding="utf-8"
        )
        return agents / "dispatch.ps1", config_path

    def dispatch(
        self,
        dispatcher: Path,
        *extra: str,
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                self.powershell,
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(dispatcher),
                "-Provider",
                "demo",
                "-Slug",
                "sample",
                "-Name",
                "task",
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
        self.assertIn("SANDBOXED", result.stdout)
        self.assertIn("<CLI default>", result.stdout)
        self.assertIn(
            ".re-discipline\\agents\\providers\\demo\\profile.md",
            result.stdout.replace("/", "\\"),
        )

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
        self.hook_config = json.loads(read(PLUGIN / "hooks" / "hooks.json"))
        self.powershell = shutil.which("powershell.exe") or shutil.which("powershell")
        if not self.powershell:
            self.skipTest("PowerShell is required for Windows hook tests")

    def run_hook(
        self,
        event: str,
        cwd: Path,
        *,
        host: str = "claude",
    ) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.pop("PLUGIN_ROOT", None)
        env.pop("CLAUDE_PLUGIN_ROOT", None)
        if host == "codex":
            env["PLUGIN_ROOT"] = str(PLUGIN)
        else:
            env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN)

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
            input=json.dumps({"cwd": str(cwd), "hook_event_name": event}),
            text=True,
            capture_output=True,
            check=False,
            env=env,
        )

    def test_hook_is_silent_outside_initialized_project(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_hook("session-start", Path(directory))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "")

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

    def test_windows_overrides_execute_the_packaged_commands(self) -> None:
        event_expectations = {
            "SessionStart": "onboard",
            "PreCompact": "checkpoint",
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
                    if event_name == "SessionStart":
                        output = json.loads(result.stdout)
                        self.assertIn(
                            "name: test",
                            output["hookSpecificOutput"]["additionalContext"],
                        )
                    else:
                        self.assertIn(expected, result.stdout.lower())


if __name__ == "__main__":
    unittest.main()
