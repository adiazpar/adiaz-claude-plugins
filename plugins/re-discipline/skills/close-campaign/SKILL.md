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

## Step 2: Account For Every Finding

Read `active/<slug>/REVIEWS.md` in full, then enumerate the campaign's own
reports and confirm each recorded finding has a resolution. Do not reconstruct
this from memory or from the masterfile alone: query the campaign's reports for
their TRUTH-PROMOTION CANDIDATES and RESIDUAL UNCERTAINTIES, and read the
`**Disposition:**` stamp on each report.

The ledger is the audit, the stamps are the evidence. Reconcile them against
each other: a stamped report with no ledger row, or a ledger row whose report
carries no stamp, means the review record is incomplete and closure cannot yet
account for it.

Every PROMOTE must be promoted or explicitly declined with a reason. Every
HOLD must reach `docs/backlog/`, be promoted now, or be explicitly dropped
with a reason. Every DROP must appear in the chronicle's dead ends. Every
BLOCK must be resolved or carried forward as a named open question.

The ledger's Unresolved Holds table is where HOLD dispositions are recorded,
and it is the one part of the campaign directory with no durable projection of
its own. Every row must be resolved now or carried to the destination its own
row names - normally a `docs/backlog/` entry - before Step 7 removes the
directory. Resolve rows; never empty the table by deleting them.

An unstamped report means nobody reviewed it. Review it now or record
explicitly that its content is being discarded unreviewed - do not let it pass
silently. A missing `REVIEWS.md` in a campaign that dispatched drafters means
the same thing about the campaign as a whole.

This step exists because closure performs extreme compression. A campaign with
a megabyte of reports and a nine-kilobyte chronicle is discarding most of what
it learned, and without an enumeration nobody can see what was lost. The
chronicle is not the archive; it is the explanation. The archive is
`docs/truth/` for what was proven and `docs/backlog/` for what was held.

## Step 3: Ratify Findings

Classify every candidate through the Wall. Require both DIRECT establishment
and a durable verifier before promotion. Present the proposed promotion,
augmentation, overturn, hold, and dead-end list to the user. Invoke
`promote-truth` or `overturn` only for approved findings that pass both gates.

## Step 4: Disposition Artifacts

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

## Step 5: Write The Chronicle

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
- provenance and scratch sources folded into the chronicle;
- the review ledger's outcome summary: reports reviewed, total
  PROMOTE/HOLD/DROP/BLOCK counts, and each carried hold with the destination it
  reached.

That summary is the ledger's only durable projection. `REVIEWS.md` dies with
the directory in Step 7, so a hold that appears in neither the chronicle nor
`docs/backlog/` is a judgement the campaign made and then erased.

## Step 6: Update Masterfiles

Add the chronicle to `docs/history/INDEX.md`, remove the campaign from
`docs/INDEX.md`, and verify `docs/truth/INDEX.md`.

If this campaign serves a `docs/goals/<slug>.md`, replace its row in that
goal's campaign table with the chronicle path, and move any question or held
finding that outlived it into the goal's carried-across section. That
substitution is what turns a campaign about to be deleted into a permanent
link; without it the arc loses its own history one closure at a time.

Distill any durable operational recall only into a pending file under
`.re-discipline/memory/proposals/`. Invoke `review-memory` only after
presenting the exact proposal and obtaining the user's explicit accept or
reject decision. Never edit accepted memory topics or host-native memory stores
as an implicit closure side effect.

Add a retrieval failure to `.re-discipline/knowledge/evals/` only after the
manager or user ratifies its query, expected evidence, hard negatives, tier
boundary, and token budget.

## Step 7: Remove Scratch Safely

Before recursive removal, resolve the repository root and target path. Confirm
the target is exactly `<repo>/active/<slug>`, remains inside `<repo>/active/`,
and is the campaign the user asked to close. Use one shell and literal paths
throughout the operation.

Remove the campaign only after the chronicle passes a cold-reopen read, every
truth has a durable verifier, every manifest entry has a completed Maintain,
Distill, or Delete disposition, and `REVIEWS.md` carries no unresolved hold
that has not reached its named destination. If there is any hesitation, stop
and improve the chronicle or disposition first.

The ledger is deleted by the same recursive removal that deletes the reports,
so re-read its Unresolved Holds table immediately before removal rather than
trusting the Step 2 reading: reviews performed during closure append to it.

Before removal, resolve every campaign-owned pending memory proposal or replace
its scratch-only citation with a durable source. Do not leave accepted recall
or an evaluation case pointing only to the campaign being deleted.

Confirm no other campaign's context pack or brief cites a path under this
campaign. A pack materialized inside campaign A that quotes campaign B's
reports survives B's closure as a dangling citation, and nothing else detects
it: a pack dies with its own campaign, not with the one it cites.

## Step 8: Verify And Report

Confirm truth links, index links, verification bases, recipes, and maintained
source, tools, tests, fixtures, corpora, or references. Confirm
`active/<slug>/` is gone and no durable file points only to the deleted
scratch. Record truth additions, overturns, chronicle, maintained
deliverables, distilled material, the review ledger's outcome summary and
where each carried hold landed, and deletions in the chronicle and indexes.
Report to the user in plain language per
`<plugin-root>/references/reporting.md`; machinery identities go into the
campaign or run record, not the screen. Print to the user only:

```user-facing
Campaign <slug> closed. Chronicle: docs/history/chronicles/<file>.
Truth promoted: <n claims|none>. Backlogged: <n briefs|none>.
<n> scratch files removed.
```

Do not commit unless the user explicitly asks.

## Reference

- Chronicle template: `<plugin-root>/templates/chronicle.md`.
- Review ledger template: `<plugin-root>/templates/campaign-reviews.md`.
- Gates: `review-subagent`, `promote-truth`, and `overturn`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Shared-memory decisions: `review-memory`.
