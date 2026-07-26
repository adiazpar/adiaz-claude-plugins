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

## Step 1: Read Brief, Pack, Report, And Primary Artifacts

Read `brief.md`, its named immutable `context-pack.json`, and `report.md` in
full. Verify the pack digest, budget, allowed tiers, corpus generation,
requested and effective retrieval profiles, active lanes, and fallback reason.
Treat the directory segment as an opaque workspace key. New keys carry
timestamp, executor, and task text; legacy task-only or provider-prefixed keys
remain valid. Never parse, normalize, or rename either form during review.

Check every context-pack citation against its source path, line span, and hash.
Treat a stale, missing, over-budget, or forbidden-tier passage as a retrieval
defect, not support for the report.

The report's confidence color is a prior, not a verdict. Follow every
value-precise claim to the cited output, source file, capture, or primary
artifact and read the value yourself.

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

When knowledge retrieval omitted a source the brief required, record a
candidate evaluation case with the query, expected source, allowed tiers,
effective profile, budget, and observed miss. Add it to tracked project evals
only after a manager or user ratifies the expected evidence and hard
negatives. A drafter never supplies its own gold label.

## Step 4: Assign Dispositions

| Verdict | Condition | Action |
|---|---|---|
| PROMOTE | DIRECT, value-precise, no unresolved conflict, with a durable verifier | Offer it to `promote-truth`. |
| HOLD | INFERRED, one observation short, or missing a durable verifier | Keep it provisional and name the decisive observation or verifier. |
| DROP | Disproved or repeats a dead end | Add it to campaign dead ends with evidence. |
| BLOCK | Conflicts with current truth or primary evidence | Stop promotion and reconcile both sides. |

INFERRED claims never cross the Wall.

## Step 5: Ratify With The User

Present a compact table of claim, evidence class, primary artifact, conflict
status, and proposed disposition. Obtain user approval before invoking
`promote-truth` or `overturn`. Never edit truth directly from this skill.

Treat MEMORY CANDIDATES as suggestions. Deduplicate them against canonical
guidance and accepted shared memory, reject secrets and private paths, and
separate operational recall from empirical claims. Record a viable candidate
only as a bounded proposal under `.re-discipline/memory/proposals/`, directly
or through `recall_propose`. Never write it to accepted topics. Route later
acceptance or rejection through `review-memory` with an explicit user
decision.

## Step 6: Update Campaign State

Record the report, context-pack ID and digest, review date,
PROMOTE/HOLD/DROP/BLOCK outcomes, corrections, retrieval-evaluation
candidates, memory-proposal paths, and any blocking uncertainty in
`CAMPAIGN.md`. Keep report and context-pack artifacts in place until closure
disposition.

Verify that every proposed promotion has a maintained source, permanent test
and fixture, or runnable recipe. Reject a chronicle as sole empirical support,
and give every rejected claim a clear reason.

Do not commit unless the user explicitly asks.

## Reference

- Manager law: `.claude/CLAUDE.md` or `.codex/AGENTS.md`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Promotion: `promote-truth`.
- Memory decision: `review-memory`.
- Follow-up delegation: `delegate`.
