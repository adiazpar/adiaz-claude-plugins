---
name: close-campaign
description: >-
  Close an irrefutably solved re-discipline campaign when asked to distill its
  findings, write its chronicle, promote DIRECT truth with durable
  verification, classify campaign artifacts, and remove scratch. Keep the
  campaign open when the closure bar is not met.
---

# Close A Campaign

Closure converts provisional work into durable truth, maintained deliverables,
and a retrospective chronicle. Scratch is removed only after every artifact is
classified as Maintain, Distill, or Delete.

## Step 1: Check The Closure Bar

Read the campaign objective and definition of solved. Close only when the
question is irrefutably resolved and a future manager could reopen the topic
from the resulting truth, maintained project assets, and chronicle. Otherwise run
`checkpoint-campaign` and leave the campaign open.

For a non-trivial campaign, use `delegate` to request a bounded fresh-eyes
closure proposal through the active host adapter. Do not hardcode a provider or
model. The manager still ratifies the proposal.

## Step 2: Ratify Findings

Classify every candidate through the Wall. Require both DIRECT establishment
and a durable verifier before promotion. Present the proposed promotion,
augmentation, overturn, hold, and dead-end list to the user. Invoke
`promote-truth` or `overturn` only for approved findings that pass both gates.

## Step 3: Disposition Artifacts

Complete the campaign manifest, then handle each artifact:

- **Maintain:** move it into supported source, tooling, tests, fixtures,
  corpora, or reference material only when an active consumer or owner
  justifies ongoing maintenance.
- **Distill:** record its necessary meaning in truth, history, or backlog, then
  remove the raw material.
- **Delete:** remove raw output, screenshots, superseded reports, and spent
  scripts after their relevant meaning is recorded.

Do not delete an unclassified artifact. Preserve licenses and source
restrictions when moving material into the repository.

Treat drafter context packs as campaign scratch after their reports and cited
sources are reviewed. Keep only durable source citations and ratified
evaluation cases; never promote a generated context pack into truth or memory.

Treat each `subagents/` child name as an opaque workspace key while recording
provenance. New chronological IDs and legacy task-only or provider-prefixed
names are equally valid inputs. Do not normalize or rename either form before
the campaign's final disposition removes the whole campaign.

## Step 4: Write The Chronicle

Resolve the plugin root from this skill's path and render
`<plugin-root>/templates/chronicle.md` to
`docs/history/chronicles/<date>-<topic>.md`. Write in dated, past-tense,
retrospective voice. Include:

- the question and outcome;
- the sequence of attempts and pivots;
- dead ends with DIRECT or INFERRED labels;
- old claims overturned by this campaign;
- truth files produced;
- exact reproduction recipes and maintained deliverables;
- classes of raw material discarded after distillation;
- deferred leads and backlog links;
- provenance and scratch sources folded into the chronicle.

## Step 5: Update Masterfiles

Add the chronicle to `docs/history/INDEX.md`, remove the campaign from
`docs/INDEX.md`, and verify `docs/truth/INDEX.md`.

Distill any durable operational recall only into a pending file under
`.re-discipline/memory/proposals/`. Invoke `review-memory` only after
presenting the exact proposal and obtaining the user's explicit accept or
reject decision. Never edit accepted memory topics or host-native memory stores
as an implicit closure side effect.

Add a retrieval failure to `.re-discipline/knowledge/evals/` only after the
manager or user ratifies its query, expected evidence, hard negatives, tier
boundary, and token budget.

## Step 6: Remove Scratch Safely

Before recursive removal, resolve the repository root and target path. Confirm
the target is exactly `<repo>/active/<slug>`, remains inside `<repo>/active/`,
and is the campaign the user asked to close. Use one shell and literal paths
throughout the operation.

Remove the campaign only after the chronicle passes a cold-reopen read, every
truth has a durable verifier, and every manifest entry has a completed
Maintain, Distill, or Delete disposition. If there is any hesitation, stop and
improve the chronicle or disposition first.

Before removal, resolve every campaign-owned pending memory proposal or replace
its scratch-only citation with a durable source. Do not leave accepted recall
or an evaluation case pointing only to the campaign being deleted.

## Step 7: Verify And Report

Confirm truth links, index links, verification bases, recipes, and maintained
source, tools, tests, fixtures, corpora, or references. Confirm
`active/<slug>/` is gone and no durable file points only to the deleted
scratch. Report truth additions, overturns, chronicle, maintained deliverables,
distilled material, and deletions.

Do not commit unless the user explicitly asks.

## Reference

- Chronicle template: `<plugin-root>/templates/chronicle.md`.
- Gates: `review-subagent`, `promote-truth`, and `overturn`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Shared-memory decisions: `review-memory`.
