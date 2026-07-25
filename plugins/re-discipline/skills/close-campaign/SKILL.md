---
name: close-campaign
description: >-
  Close an irrefutably solved re-discipline campaign when asked to distill its
  findings, write its chronicle, promote DIRECT truth, archive irreproducible
  evidence, and remove scratch. Keep the campaign open when the closure bar is
  not met.
---

# Close A Campaign

Closure converts provisional work into durable truth, preserved evidence,
maintained deliverables, and a retrospective chronicle. Scratch is removed only
after each useful artifact has a durable disposition.

## Step 1: Check The Closure Bar

Read the campaign objective and definition of solved. Close only when the
question is irrefutably resolved and a future manager could reopen the topic
from the resulting truth, archive, and chronicle. Otherwise run
`checkpoint-campaign` and leave the campaign open.

For a non-trivial campaign, use `delegate` to request a bounded fresh-eyes
closure proposal through the active host adapter. Do not hardcode a provider or
model. The manager still ratifies the proposal.

## Step 2: Ratify Findings

Classify every candidate through the Wall. Present the proposed promotion,
augmentation, overturn, hold, and dead-end list to the user. Invoke
`promote-truth` or `overturn` only for approved DIRECT findings.

## Step 3: Disposition Artifacts

Complete the campaign manifest, then handle each artifact:

- **ground-truth:** preserve under an appropriate `archive/` location;
- **capture:** archive only when a durable truth cites it, otherwise remove it;
- **reproducible:** preserve the recipe or permanent test, then remove output;
- **expensive-reproducible:** preserve both recipe and useful output;
- **keeper-tool/test/code:** generalize and move into the maintained project
  location named by the canonical profile.

Do not delete an unclassified artifact. Preserve licenses and source
restrictions when moving material into the repository.

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
- exact reproduction recipes and archive pointers;
- deferred leads and backlog links;
- provenance and scratch sources folded into the chronicle.

## Step 5: Update Masterfiles

Add the chronicle to `docs/history/INDEX.md`, remove the campaign from
`docs/INDEX.md`, and verify `docs/truth/INDEX.md`. Update checked-in project
memory pointers only when the active project contract requires them and the
user approved the memory change. Never edit host-native memory stores as an
implicit closure side effect.

## Step 6: Remove Scratch Safely

Before recursive removal, resolve the repository root and target path. Confirm
the target is exactly `<repo>/active/<slug>`, remains inside `<repo>/active/`,
and is the campaign the user asked to close. Use one shell and literal paths
throughout the operation.

Remove the campaign only after the chronicle passes a cold-reopen read and all
manifest entries have durable destinations. If there is any hesitation, stop
and improve the chronicle or disposition first.

## Step 7: Verify And Report

Confirm truth links, index links, archive paths, recipes, and promoted code or
tests. Confirm `active/<slug>/` is gone and no durable file points only to the
deleted scratch. Report truth additions, overturns, chronicle, archived items,
and maintained deliverables.

Do not commit unless the user explicitly asks.

## Reference

- Chronicle template: `<plugin-root>/templates/chronicle.md`.
- Gates: `review-subagent`, `promote-truth`, and `overturn`.
