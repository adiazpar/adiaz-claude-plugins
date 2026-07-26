---
name: review-memory
description: >-
  Review shared-memory proposals when asked to inspect memory candidates,
  accept a project memory proposal, reject a memory proposal, clean the
  proposal queue, or decide what operational recall all managers may share.
---

# Review A Shared-Memory Proposal

Keep shared memory proposal-only. Require a direct manager and an explicit
user decision before accepting or rejecting a proposal.

## Step 1: Read One Pending Proposal

Require an exact file under `.re-discipline/memory/proposals/`. Read it in
full, then read:

- `.re-discipline/memory/INDEX.md`;
- relevant accepted topics;
- linked truth, history, campaign, source, or guidance;
- `<plugin-root>/references/knowledge-governance.md`.

Confirm that normal orientation, search, and context packs exclude the pending
proposal. Stop and report a retrieval-policy defect if it is visible as
accepted recall.

## Step 2: Review The Content

Check:

- duplication or conflict with canonical guidance;
- project scope and likely reuse;
- secrets, credentials, private paths, or machine-local values;
- accidental authority leakage from history, campaign work, or inference;
- durable source links;
- expiration or re-verification conditions;
- whether a concise pointer is better than copied source text.

Accept only operational recall such as navigation, workflow preferences,
recurring failure patterns, useful commands, prior decisions and their durable
locations, or cross-session continuity.

Never treat memory as empirical evidence. Require a DIRECT claim to live in
`docs/truth/` with a durable verifier before memory points to it as current
fact. Reject unsupported empirical content, or leave the proposal pending
while missing evidence is gathered, instead of laundering it through recall.

## Step 3: Obtain A Decision

Present the proposal, duplicate/conflict status, sensitivity result, authority
classification, durable links, expiration, and an `accept` or `reject`
recommendation.

Obtain the user's explicit `accept` or `reject` decision. Do nothing when the
choice is absent or ambiguous. Never infer acceptance from a report,
checkpoint, campaign closure, or prior approval of another proposal.

## Step 4: Apply The Decision

### Accept

1. Distill only the approved recall into a focused topic under
   `.re-discipline/memory/topics/`.
2. Label it as accepted recall, not empirical authority.
3. Record scope, durable source links, acceptance date, and any expiry or
   re-verification trigger.
4. Update `.re-discipline/memory/INDEX.md`.
5. Validate that ordinary retrieval returns the accepted topic with the memory
   tier.
6. Remove the pending proposal only after the topic and index are valid.

Augment an existing topic instead of creating a duplicate.

### Reject

1. Record a concise reason in the originating `CAMPAIGN.md`.
2. When no campaign owns the proposal, append the reason under
   `## Proposal decisions` in `.re-discipline/memory/INDEX.md`.
3. Remove only the exact pending proposal after the disposition is durable.
4. Confirm that no accepted topic or index entry was changed accidentally.

## Step 5: Report

Report the decision, proposal path, accepted topic or rejection record, source
links, expiry, and remaining proposal count.

Never edit Claude or Codex native memory stores. Never accept a retrieval
profile, promote truth, or copy raw conversation transcripts from this skill.

Do not commit unless the user explicitly asks.

## Reference

- Memory and authority policy:
  `<plugin-root>/references/knowledge-governance.md`.
- Drafter candidate review: `review-subagent`.
- Truth admission: `promote-truth`.
