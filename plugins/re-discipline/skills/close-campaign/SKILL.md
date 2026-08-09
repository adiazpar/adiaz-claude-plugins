---
name: close-campaign
description: >-
  Close a solved re-discipline campaign through the resumable shared-engine
  closure job. Proves coverage, projects approved durable outputs, verifies
  archive digests, and finalizes only when every closure gate passes.
---

# Close A Campaign

Closure is a stateful, resumable coverage job. Leave the campaign open when
the result is not irrefutable or when any required disposition is missing.

## Start Or Resume

Call `closure_apply` with action `start`, the campaign revision, and an
idempotency key, or call `state(mode="closure")` for an existing job. Advance,
verify, reopen, and finalize through typed `closure_apply` actions. Entering
`closing` freezes ordinary new runs; remediation requires an explicit manager
transaction.

## Advance The Stages

Use engine operations to complete, in order:

1. inventory every work item, run, finding, intake, review, event, registered
   file, and changed project path;
2. prove coverage for every non-aborted run and every report claim span;
3. normalize uncovered material through curator runs, then proof-resolve each
   closure-triggered normalization item against the exact curator run, intake
   coverage, and manager review receipt;
4. reconcile duplicates, conflicts, dependencies, challenges, and
   supersessions;
5. record a manager destination and retention decision for every item;
6. stage current truth, history, backlog, playbook, maintained-source, and
   asset projections;
7. verify projection, archive, source, and retrieval digests;
8. publish the complete archive transaction;
9. finalize and emit the immutable closure receipt.

Deferred work is not covered by a prose note. A closure-blocking deferment
must be resolved. A non-blocking deferment must already declare
`export-backlog` and an exact `docs/backlog/*.md` destination; include its work
item ID in `exportedWorkItemIds` no later than the `project` stage so the engine
can stage, reproduce, verify, and atomically publish that backlog record.

Truth projection is permitted only inside this job. Each current claim must be
manager-ratified, direct under project policy, reproducible, atomic, scoped,
conflict-free, and linked to exact finding and review revisions. Partial truth
publication is a failed transaction.

## Refusal Is Correct

Do not bypass a refusal. Surface the exact missing coverage, open work,
unreviewed intake, unresolved normalization item, unresolved conflict, absent
report, retention gap, or digest mismatch and return to the relevant stage.
Deletion requires explicit manager approval and a durable replacement for the
artifact's value.

`missing-reviewed-intake` on a run that is already `completed` or `invalidated`
means its only intake left a span `unresolved`. Clear it through
`review-subagent`: submit a further curator intake over the same frozen report
with every span disposed and review it. Coverage is a union across reviewed
intakes, so this supplies what closure requires without disturbing any review
already ratified over the earlier intake. Do not attempt to reopen or re-return
the run. An invalidated run is not exempt: its report is still retained and may
still be cited by a ratified finding, so closure accounts for it like any other
non-aborted run with a report.

## Verify And Return

Require the archive manifest, generated archive README, closure receipt, truth
and backlog projections, navigation update, and retrieval reachability to
regenerate from canonical records. Report durable destinations, unresolved
exports, retained external references, and the closure receipt digest.

Do not commit unless the user explicitly asks.

## References

- Archive template: `<plugin-root>/templates/campaign/archive-README.md`.
- Governance: `<plugin-root>/references/knowledge-governance.md`.
