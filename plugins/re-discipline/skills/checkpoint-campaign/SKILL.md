---
name: checkpoint-campaign
description: >-
  Checkpoint an active re-discipline campaign at session end, before context
  compaction, or when asked to save campaign state. Rewrites CAMPAIGN.md for a
  cold resume and sweeps spent scratch without closing the campaign.
---

# Checkpoint An Active Campaign

Checkpointing preserves a cold-resume state. It is routine hygiene, not
closure.

## Step 1: Rewrite Current State

Update `active/<slug>/CAMPAIGN.md` so Current state is at most about 30 lines,
newest first, and answers:

- what just happened;
- what is proven and where the evidence lives;
- what remains provisional;
- the exact next move;
- what is blocked on what.

Move superseded session narrative to Historical log or a campaign evidence
note. Remove solved open questions and point to their disposition. Add new dead
ends with DIRECT or INFERRED labels. Update the artifact disposition manifest.
Preserve compact source handles, reviewed report paths, and immutable
context-pack IDs and digests instead of copying passages or spent tool output.

## Step 2: Sweep Scratch Carefully

Survey tracked and untracked files under the campaign. For each one-off script
or log:

- delete it only when its necessary meaning and durable verification are
  already captured;
- keep it and name it in Current state when the next session still needs it;
- move genuinely reusable tooling into maintained project tooling only when it
  has an active consumer or owner;
- keep ambiguous or campaign-needed evidence inside the active campaign until
  closure disposition.
- retain a drafter's `brief.md`, `context-pack.json`, and `report.md` together
  until manager review and closure disposition;
- route durable recall candidates to `.re-discipline/memory/proposals/`
  without accepting them during checkpointing.

Do not delete ambiguous or user-owned artifacts. Do not touch another active
campaign.

Treat every `subagents/` child name as an opaque workspace key. Preserve both
new chronological IDs and legacy task-only or provider-prefixed names exactly
as they are. Checkpointing never parses, normalizes, or renames them.

## Step 3: Preserve Knowledge Boundaries

Run only the cheap knowledge status and freshness check. Record a changed
corpus generation, requested/effective profile, or fallback reason only when
it affects the next campaign action or invalidates a cited pack.

Do not run a full benchmark, calibrate weights, activate a retrieval profile,
accept memory, or copy raw conversation transcripts into durable state. Leave
pending proposals for `review-memory`.

## Step 4: Verify Cold Resume

Read the rewritten masterfile as if the session had no prior context. Confirm
that its links resolve, its next action is executable, and every new artifact
has a disposition.

Report the Current state line count, removed scratch, retained artifacts, and
resume action. If the closure bar is already met, recommend `close-campaign`
instead of describing the campaign as both open and solved.

Do not commit unless the user explicitly asks.

## Reference

- Active manager adapter: `.claude/CLAUDE.md` or `.codex/AGENTS.md`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Memory decisions: `review-memory`.
- Closure: `close-campaign`.
