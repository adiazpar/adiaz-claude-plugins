import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def compact(text: str) -> str:
    return " ".join(text.lower().split())


class KnowledgeSkillContracts(unittest.TestCase):
    def setUp(self) -> None:
        self.skills = PLUGIN / "skills"

    def skill(self, name: str) -> str:
        return read(self.skills / name / "SKILL.md")

    def test_governance_skills_have_portable_trigger_metadata(self) -> None:
        expected = {
            "benchmark-knowledge": ("benchmark", "retrieval"),
            "calibrate-knowledge": ("calibrate", "weights"),
            "decide-retrieval-profile": ("promote", "rollback"),
            "review-memory": ("memory proposal", "accept"),
        }

        for name, triggers in expected.items():
            with self.subTest(skill=name):
                body = self.skill(name)
                self.assertTrue(body.startswith("---\n"))
                frontmatter = body.split("---", 2)[1].lower()
                self.assertIn(f"name: {name}", frontmatter)
                self.assertIn("description:", frontmatter)
                for trigger in triggers:
                    self.assertIn(trigger, frontmatter)
                self.assertNotIn("allowed-tools:", frontmatter)
                self.assertNotIn("argument-hint:", frontmatter)

    def test_benchmark_is_read_only_and_covers_effective_profiles(self) -> None:
        body = compact(self.skill("benchmark-knowledge"))

        for phrase in (
            "permit any user",
            "quick",
            "full",
            "end-to-end",
            "every supported effective profile",
            "model-free fallbacks",
            "deterministic replay",
            "token-budget",
            ".re-discipline/cache/",
            "do not calibrate",
            "never change behavior",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)

        self.assertIn("explicitly authorizes", body)
        self.assertIn("do not edit", body)

    def test_calibration_creates_only_a_measured_candidate(self) -> None:
        body = compact(self.skill("calibrate-knowledge"))

        for phrase in (
            "direct manager or project-maintainer",
            "explicit calibration request",
            "never tune",
            "tier or access filters",
            "frozen holdout",
            "every declared effective capability profile",
            ".re-discipline/cache/calibration/<run-id>/",
            "do not edit `.re-discipline/knowledge/retrieval-profile.json`",
            "do not activate the candidate",
            "do not launch a subagent for every combination",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)

    def test_profile_decision_is_explicit_and_generated(self) -> None:
        body = compact(self.skill("decide-retrieval-profile"))

        for action in ("`promote`", "`reject`", "`retain`", "`rollback`"):
            self.assertIn(action, body)
        for phrase in (
            "explicit user decision",
            "never let a drafter",
            "independent evidence for every declared effective fallback profile",
            ".re-discipline/knowledge/retrieval-profile.json",
            "exact generated candidate",
            "content hash",
            "requested and effective profiles",
            "plugin maintainer",
            "never copy project data upstream",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)

    def test_memory_review_is_the_only_accept_reject_door(self) -> None:
        body = compact(self.skill("review-memory"))

        for phrase in (
            ".re-discipline/memory/proposals/",
            "exclude the pending proposal",
            "explicit `accept` or `reject` decision",
            ".re-discipline/memory/topics/",
            ".re-discipline/memory/index.md",
            "## proposal decisions",
            "never treat memory as empirical evidence",
            "never edit claude or codex native memory stores",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)


class KnowledgeLifecycleContracts(unittest.TestCase):
    def skill(self, name: str) -> str:
        return compact(read(PLUGIN / "skills" / name / "SKILL.md"))

    def test_onboard_reports_health_without_expensive_work(self) -> None:
        body = self.skill("onboard")

        for phrase in (
            ".re-discipline/config.json",
            ".re-discipline/knowledge/",
            "policy.jsonc",
            "requested and effective profiles",
            "fallback reason",
            "sanity-check the reported count",
            "do not build vectors",
            "do not",
            "calibrate weights",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)

    def test_delegate_materializes_a_bounded_shared_context_pack(self) -> None:
        delegate = self.skill("delegate")
        adapters = compact(
            read(PLUGIN / "references" / "runtime-adapters.md")
        )
        combined = delegate + "\n" + adapters

        for phrase in (
            "context_pack",
            "context-pack.json",
            "pack id and digest",
            "requested and effective retrieval profiles",
            "allowed epistemic tiers",
            "explicit token budget",
            ".re-discipline/memory/proposals/",
            "same bounded `context-pack.json` for native and external",
            "separately benchmarked effective fallback profile",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, combined)

    def test_report_review_proposes_but_does_not_ratify_memory_or_evals(self) -> None:
        body = self.skill("review-subagent")

        for phrase in (
            "context-pack.json",
            "verify the pack digest",
            "candidate evaluation case",
            "manager or user ratifies",
            ".re-discipline/memory/proposals/",
            "never write it to accepted topics",
            "review-memory",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, body)

    def test_checkpoint_and_close_preserve_knowledge_boundaries(self) -> None:
        checkpoint = self.skill("checkpoint-campaign")
        close = self.skill("close-campaign")

        for phrase in (
            "context-pack ids and digests",
            "do not run a full benchmark",
            "calibrate weights",
            "leave pending proposals for `review-memory`",
        ):
            with self.subTest(skill="checkpoint", phrase=phrase):
                self.assertIn(phrase, checkpoint)

        for phrase in (
            ".re-discipline/memory/proposals/",
            "review-memory",
            "never edit accepted memory topics or host-native memory stores",
            ".re-discipline/knowledge/evals/",
            "token budget",
            "scratch-only citation",
        ):
            with self.subTest(skill="close", phrase=phrase):
                self.assertIn(phrase, close)


class DurableKnowledgeDocumentationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.governance = compact(
            read(PLUGIN / "references" / "knowledge-governance.md")
        )
        self.readme = compact(read(PLUGIN / "README.md"))

    def test_settings_control_plane_is_separate_from_bootstrap_and_state(self) -> None:
        combined = self.governance + "\n" + self.readme

        for phrase in (
            ".re-discipline/config.json",
            "strict-json bootstrap",
            ".re-discipline/knowledge/",
            "policy.jsonc",
            "retrieval-profile.json",
            "generated, content-hashed",
            "keep code, model artifacts, indexes, benchmark output, and memory "
            "outside the control files",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, combined)

    def test_source_authority_and_pending_memory_are_explicit(self) -> None:
        for phrase in (
            "docs/truth/**",
            "docs/history/**",
            "active/*/campaign.md",
            "docs/backlog/**",
            ".re-discipline/memory/topics/**",
            ".re-discipline/memory/proposals/**",
            "never authoritative",
            "apply tier and access filters before relevance ranking",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, self.governance)

    def test_permissions_and_no_invisible_expensive_work_are_documented(self) -> None:
        for phrase in (
            "read-only benchmark",
            "any user",
            "project calibration",
            "direct manager or project maintainer",
            "project profile decision",
            "explicit user decision",
            "global calibration or promotion",
            "plugin maintainer",
            "never run a full benchmark",
            "model download",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, self.governance)

    def test_fallback_profiles_are_independently_measured(self) -> None:
        for phrase in (
            "requested profile from the effective profile",
            "finite capability matrix",
            "model-free lexical and graph retrieval",
            "independent benchmark evidence",
            "never improvise an unmeasured lane combination",
            "fallback reason",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, self.governance)


if __name__ == "__main__":
    unittest.main()
