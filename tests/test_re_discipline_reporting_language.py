import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT


class ReportingLanguageTests(unittest.TestCase):
    def test_reporting_reference_separates_outcome_from_machinery(self):
        text = (PLUGIN / "references" / "reporting.md").read_text()
        self.assertIn("Lead with the user-visible outcome", text)
        for internal in ("cache generations", "model fingerprints", "lane weights", "transaction"):
            self.assertIn(internal, text)
        self.assertIn("Keep", text)

    def test_run_report_is_provenance_not_a_decision(self):
        text = (PLUGIN / "references" / "reporting.md").read_text()
        self.assertIn("terminal provenance", text)
        self.assertIn("not an epistemic decision", text)
        self.assertIn("Never label a run reviewed", text)

    def test_lifecycle_skills_link_reporting_contract(self):
        for name in ("delegate", "review-subagent"):
            text = (PLUGIN / "skills" / name / "SKILL.md").read_text()
            self.assertIn("references/reporting.md", text)

    def test_status_views_are_bounded_and_actionable(self):
        text = (PLUGIN / "references" / "reporting.md").read_text()
        for term in (
            "focus",
            "blocked",
            "due-or-near deferred",
            "pending returns",
            "next action",
        ):
            self.assertIn(term, text)
        self.assertIn("Expand full evidence only for the next decision", text)


if __name__ == "__main__":
    unittest.main()
