import json
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / "plugins" / "re-discipline"


class ReDisciplineKnowledgeContractTests(unittest.TestCase):
    def skill(self, name: str) -> str:
        return (PLUGIN / "skills" / name / "SKILL.md").read_text(encoding="utf-8")

    def test_onboard_is_bounded_and_read_only(self):
        text = self.skill("onboard")
        self.assertIn("mode: orient", text)
        self.assertIn("mode: resume", text)
        self.assertIn("Do not preload reports", text)
        self.assertIn("Onboarding is read-only", text)

    def test_delegation_binds_work_run_pack_and_grants(self):
        text = self.skill("delegate")
        for phrase in ("exactly one primary work item", "run.prepare", "retained digest", "explicitly granted project paths"):
            self.assertIn(phrase, text)
        self.assertIn("state(mode=\"resume\"", text)

    def test_return_curation_and_review_are_distinct(self):
        text = self.skill("review-subagent")
        compact = " ".join(text.split())
        self.assertIn("status `returned`", text)
        self.assertIn("complete intake coverage", text)
        self.assertIn("immutable review receipts", compact)
        self.assertIn("remain campaign-provisional until closure", text)

    def test_curator_authority_is_least_privilege(self):
        text = (PLUGIN / "agents" / "knowledge-curator.md").read_text()
        self.assertIn("Write only inside the granted curator run", text)
        self.assertIn("Never set `manager-ratified`", text)
        self.assertIn("Abstain and request manager judgment", text)
        self.assertIn("map every claim span", text)

    def test_truth_is_closure_only(self):
        close = self.skill("close-campaign")
        review = self.skill("review-subagent")
        overturn = self.skill("overturn")
        self.assertIn("Truth projection is permitted only inside this job", close)
        self.assertIn("may not edit a projected", overturn)
        self.assertIn("never edits truth during an active campaign", review)

    def test_closure_is_resumable_coverage_not_summary(self):
        text = self.skill("close-campaign")
        for stage in ("inventory every work item", "prove coverage", "reconcile duplicates", "verify projection", "immutable closure receipt"):
            self.assertIn(stage, text)
        self.assertIn("Do not bypass a refusal", text)

    def test_migration_is_explicit_and_isolated(self):
        text = self.skill("migrate-project")
        self.assertIn("sole legacy reader", text)
        self.assertIn("exact approved preview digest", text)
        self.assertIn("never writes a 0.7 record", text)
        self.assertIn("Ordinary state commands must refuse", text)
        init = self.skill("init-project")
        self.assertIn("does not perform project state migration", " ".join(init.split()))
        self.assertIn("Never inspect or convert older", init)

    def test_findings_keep_independent_epistemic_axes(self):
        schema = json.loads((PLUGIN / "knowledge" / "schemas" / "finding-frontmatter.schema.json").read_text())
        properties = schema["properties"]
        self.assertEqual(properties["evidenceGrade"]["enum"], ["direct", "inferred", "reported", "unknown"])
        self.assertIn("manager-ratified", properties["reviewState"]["enum"])
        self.assertIn("challenged", properties["validity"]["enum"])
        self.assertIn("syntheticQuestions", properties)
        self.assertEqual(properties["syntheticQuestions"]["minItems"], 3)
        self.assertEqual(properties["syntheticQuestions"]["maxItems"], 5)
        self.assertIs(properties["questionsReviewed"]["const"], True)

    def test_finding_schema_and_template_match_canonical_contract(self):
        schema = json.loads((PLUGIN / "knowledge" / "schemas" / "finding-frontmatter.schema.json").read_text())
        canonical_fields = [
            "schemaVersion", "id", "campaignId", "revision", "createdAt", "updatedAt",
            "createdBy", "updatedBy", "digest", "correlationId", "kind", "subject",
            "claim", "scope", "appliesWhen", "knownLimits", "tags", "subsystems",
            "aliases", "sourceRuns", "evidence", "supports", "contradicts", "dependsOn",
            "supersedes", "duplicates", "answers", "spawned", "evidenceGrade",
            "reviewState", "validity", "projection", "syntheticQuestions",
            "questionsReviewed",
        ]
        relation_fields = [
            "supports", "contradicts", "dependsOn", "supersedes", "duplicates", "answers", "spawned",
        ]
        projection_values = [
            "none", "campaign", "truth", "history", "backlog", "playbook", "maintained", "archive", "rejected",
        ]

        self.assertEqual(schema["required"], canonical_fields)
        self.assertEqual(set(schema["properties"]), set(canonical_fields) | {"policyId", "verifiedAt"})
        self.assertNotIn("relations", schema["properties"])
        self.assertTrue(all(field in schema["properties"] for field in relation_fields))
        self.assertEqual(schema["properties"]["projection"]["enum"], projection_values)
        self.assertIs(schema["additionalProperties"], False)

        template = (PLUGIN / "templates" / "campaign" / "finding.md").read_text(encoding="utf-8")
        _, frontmatter, body = template.split("---", 2)
        template_fields = [
            line.split(":", 1)[0]
            for line in frontmatter.strip().splitlines()
            if line and not line.startswith(" ") and ":" in line
        ]
        self.assertEqual(template_fields, canonical_fields)
        self.assertNotIn("relations:", frontmatter)
        self.assertNotIn("syntheticQueries:", frontmatter)
        self.assertIn("questionsReviewed: true", frontmatter)
        questions_line = next(line for line in frontmatter.splitlines() if line.startswith("syntheticQuestions:"))
        questions = json.loads(questions_line.split(":", 1)[1].strip())
        self.assertEqual(questions, sorted(set(questions)))
        self.assertTrue(3 <= len(questions) <= 5)
        self.assertTrue(all(question.rstrip().endswith("?") for question in questions))
        self.assertEqual(
            [line for line in body.splitlines() if line.startswith("#")],
            ["# Claim", "## Applies when", "## Does not establish", "## Evidence", "## Reproduction", "## Relations"],
        )

    def test_report_fallback_has_measurement_gate(self):
        policy = (PLUGIN / "templates" / "project" / "policy.jsonc").read_text()
        readme = (PLUGIN / "README.md").read_text()
        self.assertIn('"reportFallback": true', policy)
        self.assertIn('"reportFallbackUntilMeasured": true', policy)
        self.assertIn("non-inferior on recall", readme)
        self.assertIn("better on token cost", readme)

    def test_hooks_are_guardrails_not_state_authority(self):
        readme = (PLUGIN / "README.md").read_text()
        runtime = (PLUGIN / "references" / "runtime-adapters.md").read_text()
        self.assertIn("accident boundary", readme)
        self.assertIn("Safety remains in engine validation", readme)
        self.assertIn("Hosts without reliable hook delivery", runtime)

    def test_memory_and_profile_governance_remain_explicit(self):
        memory = self.skill("review-memory")
        profile = self.skill("decide-retrieval-profile")
        self.assertIn("require the user's exact", memory)
        self.assertIn("never empirical evidence", memory)
        self.assertIn("explicit", profile.lower())
        self.assertIn("candidate", profile.lower())


if __name__ == "__main__":
    unittest.main()
