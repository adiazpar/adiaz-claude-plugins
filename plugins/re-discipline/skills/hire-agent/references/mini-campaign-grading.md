# Mini-Campaign Grading

A representative interview has two stages.

## Stage 1: Candidate Draft

Give the candidate a versioned brief, a scoped artifact set, and only the tools
needed for the task. Keep the manager-only answer key and current truth outside
the candidate's readable scope. Require the standard `report.md`.

## Stage 2: Fixed Manager Ratification

Run the real `review-subagent` process over the draft with a fixed manager
configuration. Record host, model, effort, tools, and date. Use the same
configuration for all candidates compared in one benchmark set; the fixed
manager can be Claude Code or Codex.

Ask whether the manager, working from the report and cited artifacts, reaches
the proven result or correctly holds what remains uncertain without promoting
a false claim.

## Scoring

- conclusion correctness;
- quality and durability of evidence;
- value-precise transcription;
- ratification-enabling report structure;
- honesty when the evidence is insufficient;
- write-scope and tool-scope compliance;
- cost and latency as secondary measures.

A confidently wrong draft that would induce false promotion is a hard failure.
An honest partial result that causes the manager to HOLD an unproven value may
be a strong result.

Use the bundled project-specific fixtures only when their subject and live
tools exist. Otherwise create an equivalent mini-campaign around a known,
withheld result from the current project's domain.
