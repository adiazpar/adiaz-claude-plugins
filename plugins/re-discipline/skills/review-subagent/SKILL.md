---
name: review-subagent
description: >-
  Review a report returned by a re-discipline drafter or subagent. Reclassifies
  every claim against the DIRECT-evidence Wall, checks contradictions, and
  assigns promote, hold, drop, or block dispositions before any truth change.
---

# Review A Drafter Report

A report is a draft about evidence, not the evidence itself. The manager must
rederive every claim that could affect durable truth.

## Step 1: Read Report And Primary Artifacts

Read `active/<slug>/subagents/<name>/report.md` in full. Its confidence color
is a prior, not a verdict. Follow every value-precise claim to the cited output,
source file, capture, or primary artifact and read the value yourself.

Different DIRECT sources may attest different facts. Source code can show what
a system implements; an exported artifact can show what a particular tool
produced. When they differ, describe what each source proves before deciding
that either is wrong.

## Step 2: Reclassify Evidence

For every claim, ask: could this evidence exist if the claim were false?

- **DIRECT:** an observation impossible under the negation of the claim.
- **INFERRED:** a best explanation while another interpretation survives.

Check the source of record in `.re-discipline/project-profile.md` first for
subject-defined facts. Downgrade call-chain association, elimination, corpus
patterns, or multi-variable experiments that are presented as proof.

## Step 3: Scan For Conflict

Search `docs/truth/`, relevant chronicles, the campaign, and other reports for
the claim's exact values and key terms. Classify hits as:

- contradiction: current truth asserts the opposite;
- overlap: an existing truth already covers the claim;
- dependency: accepting the claim changes another truth;
- known dead end: history already records why this route failed.

Resolve contradictions at the primary-artifact layer. Do not accept or reject
a report because two summaries use different words.

## Step 4: Assign Dispositions

| Verdict | Condition | Action |
|---|---|---|
| PROMOTE | DIRECT, value-precise, no unresolved conflict | Offer it to `promote-truth`. |
| HOLD | INFERRED or one observation short | Keep it provisional and name the decisive observation. |
| DROP | Disproved or repeats a dead end | Add it to campaign dead ends with evidence. |
| BLOCK | Conflicts with current truth or primary evidence | Stop promotion and reconcile both sides. |

INFERRED claims never cross the Wall.

## Step 5: Ratify With The User

Present a compact table of claim, evidence class, primary artifact, conflict
status, and proposed disposition. Obtain user approval before invoking
`promote-truth` or `overturn`. Never edit truth directly from this skill.

Treat MEMORY CANDIDATES as suggestions. Deduplicate them against checked-in
guidance and the active host's memory policy. Persist one only when the user
requests it and its supporting claim is DIRECT.

## Step 6: Update Campaign State

Record the report, review date, PROMOTE/HOLD/DROP/BLOCK outcomes, corrections,
and any blocking uncertainty in `CAMPAIGN.md`. Keep report artifacts in place
until closure disposition.

Verify that every proposed promotion has a surviving recipe or preserved
artifact and every rejected claim has a clear reason.

Do not commit unless the user explicitly asks.

## Reference

- Manager law: `.claude/CLAUDE.md` or `.codex/AGENTS.md`.
- Promotion: `promote-truth`.
- Follow-up delegation: `delegate`.
