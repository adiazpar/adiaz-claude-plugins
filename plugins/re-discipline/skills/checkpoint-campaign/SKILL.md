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

## Step 2: Sweep Scratch Carefully

Survey tracked and untracked files under the campaign. For each one-off script
or log:

- delete it only when its result and reproduction recipe are already captured;
- keep it and name it in Current state when the next session still needs it;
- promote genuinely reusable tooling to the project's durable tooling area;
- preserve irreproducible evidence for closure disposition.

Do not delete ambiguous or user-owned artifacts. Do not touch another active
campaign.

## Step 3: Verify Cold Resume

Read the rewritten masterfile as if the session had no prior context. Confirm
that its links resolve, its next action is executable, and every new artifact
has a disposition.

Report the Current state line count, removed scratch, retained artifacts, and
resume action. If the closure bar is already met, recommend `close-campaign`
instead of describing the campaign as both open and solved.

Do not commit unless the user explicitly asks.

## Reference

- Active manager contract: `.claude/CLAUDE.md` or `.codex/AGENTS.md`.
- Closure: `close-campaign`.
