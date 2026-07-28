"""User-facing output blocks must never contain knowledge-machinery vocabulary."""

import pathlib
import re
import unittest

PLUGIN = pathlib.Path(__file__).resolve().parent.parent / "plugins" / "re-discipline"

BANNED = [
    "corpus generation",
    "generation-",
    "retrieval lane",
    "rrf",
    "reciprocal-rank",
    "effective profile",
    "requested profile",
    "fingerprint",
    "evidence pin",
    "fresh=",
    "staleactionable",
    "fallback reason",
    "context pack digest",
    "chunker",
    "rerank",
]

FENCE = re.compile(r"```user-facing\n(.*?)```", re.DOTALL)

# Skills whose report step must expose at least one user-facing fence.
MUST_HAVE_FENCE = [
    "onboard",
    "benchmark-knowledge",
    "calibrate-knowledge",
    "decide-retrieval-profile",
    "checkpoint-campaign",
    "close-campaign",
    "review-memory",
    "init-project",
]


class ReportingLanguageTests(unittest.TestCase):
    def all_fences(self):
        for skill_md in sorted((PLUGIN / "skills").glob("*/SKILL.md")):
            for block in FENCE.findall(skill_md.read_text(encoding="utf-8")):
                yield skill_md, block

    def test_user_facing_blocks_ban_machinery_vocabulary(self):
        found_any = False
        for path, block in self.all_fences():
            found_any = True
            lower = block.lower()
            for term in BANNED:
                self.assertNotIn(
                    term,
                    lower,
                    f"{path.parent.name}: banned term {term!r} in user-facing block",
                )
        self.assertTrue(found_any, "no user-facing fences found at all")

    def test_reporting_skills_declare_a_user_facing_template(self):
        for name in MUST_HAVE_FENCE:
            text = (PLUGIN / "skills" / name / "SKILL.md").read_text(encoding="utf-8")
            self.assertRegex(
                text, FENCE, f"{name} must mark its printed report as user-facing"
            )

    def test_go_user_block_bans_the_same_vocabulary(self):
        src = (
            PLUGIN / "knowledge" / "internal" / "knowledge" / "user_status.go"
        ).read_text(encoding="utf-8")
        # Exact system-block key names the code must read are exempt; anything
        # else (i.e. sentence fragments shown to users) must stay clean.
        system_keys = {
            "fallbackReason",
            "requestedProfile",
            "effectiveProfile",
            "staleActionable",
            "memoryProposalsPending",
            "generation",
            "benchmark",
            "pins",
            "index",
            "configuration",
            "knowledge",
        }
        strings = re.findall(r'"((?:[^"\\]|\\.)*)"', src)
        for value in strings:
            if value in system_keys:
                continue
            lower = value.lower()
            for term in ("generation", "lane", "fingerprint", "rerank", "fallback"):
                self.assertNotIn(
                    term, lower, f"user_status.go literal leaks machinery: {value!r}"
                )

    def test_reporting_law_exists_and_names_both_audiences(self):
        law = (PLUGIN / "references" / "reporting.md").read_text(encoding="utf-8")
        lower = law.lower()
        for needle in ("dashboard", "exception", "agent-internal", "silent self-healing"):
            self.assertIn(needle, lower)


if __name__ == "__main__":
    unittest.main()
