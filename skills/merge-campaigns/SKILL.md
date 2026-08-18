---
name: merge-campaigns
description: >-
  Merge two or more re-discipline campaigns when asked to consolidate,
  combine, or unify campaign histories into one canonical campaign. Plans the
  exact atomic merge, obtains explicit approval of its digest, applies it
  through the shared engine, and verifies the single resulting graph.
---

# Merge Campaigns

Treat campaign consolidation as one engine-owned topology transaction. Never
move records manually, use `reconcile.import` as a mover, or leave a successor
campaign beside source archives.

## Orient And Define The Order

Call `state(mode="orient")`, then bounded `resume` views for the named source
campaigns. Require exact campaign IDs and slugs, one absent target ID and slug,
the manager identity, a target objective, and an explicit chronological
sequence. Separate historical dates from canonical migration and merge
timestamps. Encode every required predecessor edge in chronology
`dependsOn`, and order `sources` so the engine can link each source root to the
previous source root.

Accept only open or paused sources managed by the invoking actor. Stop if a
source has a live closure job, an invalid graph, a dirty inventory, or an
unresolved identity choice.

## Plan Without Writing

Call `campaign_merge_plan` with the exact current `expectedHeadRevision` and
`expectedHeadDigest` plus the full target, ordered sources, UTC `mergedAt`, and
chronology specification. For CLI use, call `campaign-merge-plan --input` with
the same strict JSON request.

Retain the returned plan digest outside all source campaign trees. Review:

- every source campaign record and exact tree digest;
- record, returned-run, aborted-run, event, artifact, and byte counts;
- every collision-safe record, evidence, review-load, session, event, and path
  mapping;
- the chronology digest and historical record order;
- artifact mappings and preserved file modes.

Re-run planning with the identical request to prove deterministic equality.
Treat any changed head, source digest, mapping, or plan digest as a new plan
requiring a fresh decision.

## Apply Atomically

Submit `manager_apply` with action `campaign.merge`, the same actor, target ID
and slug, expected head, full unchanged specification, retained
`approvedPlanDigest`, non-empty rationale, correlation ID, and stable
idempotency key. The action-locked CLI equivalent is `campaign-merge`.

On an interrupted response, retry the exact request and idempotency key. Expect
the immutable receipt to replay. Never alter one field while retaining that
key. On a concurrency conflict, orient again, produce and inspect a new plan,
and apply it with a new idempotency key.

## Verify The Result

Call `state(mode="orient")` and `state(mode="resume", campaignId=<target>)`.
Require exactly one target graph and no remaining source inventory entries or
trees. Verify all planned counts, relationships, returned and aborted run
states, source-qualified provenance, artifact digests and modes, and the
explicit dependency chain. Read and verify `merge/plan.json`, `merge/id-map.json`,
`merge/chronology.json`, `merge/CHRONOLOGY.md`, and the historical event
stream. Reconcile the derived index and confirm project health.

Repeat the exact merge request once to prove idempotent receipt replay. Commit
project state only when explicitly authorized.
