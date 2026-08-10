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
verify, reopen, restart, and finalize through typed `closure_apply` actions.
Entering `closing` freezes ordinary new runs; remediation requires an explicit
manager transaction. That transaction is `manager_apply` with action
`closure.remediation.run.create`, which takes the same payload as `run.prepare`
and is the only way to open a run while a campaign is closing. Reach for it
whenever a closure gate asks for work that needs a run, a normalization item
most often. Do not reopen the campaign to get one: reopening abandons the
closure attempt, it does not perform the work the attempt asked for.

## Re-Enter After A Reopen

`start` refuses while any closure job exists, so a reopened campaign re-enters
through action `restart`, not through `start`. A restart is one explicit edge
from the reopened job back to stage `inventory`: it re-plans against the
campaign as it stands after remediation, freezes a later campaign revision, and
records one further attempt. It keeps the closure job's identity and its archive
destination, because one campaign has one archive tree.

Send `closureJobId` for the exact job being re-entered,
`expectedClosurePlanRevision` equal to the `campaignRevision` of
`active/<slug>/closure/plan.json`, and the exact prior digests for the campaign,
`closure-plan`, the closure job, and `closure-coverage`. Read the plan before
you replace it; the restart is refused unless all of them match, and a refused
restart changes nothing.

Explicit file-retention and exported-backlog decisions carry into the restarted
attempt, and anything supplied in the restart request overrides what was
carried. Every other coverage class is recomputed from live records, so nothing
inherited can carry a stale approval past a gate. Inherited active-file
dispositions naming files that no longer exist are dropped; a disposition
declared in the request must name a file that exists, and is refused otherwise.

A restarted attempt may require less than the one it replaces. The plan is one
attempt's obligation, not a campaign-wide ratchet, and nothing durable is
published before `finalize`.

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
means its only intake left a span `unresolved`. Which repair applies depends on
whether that intake has been reviewed.

If the covering intake is already `reviewed` and its only defect is one or more
`unresolved` spans, retire them in place with
`manager_apply(action="intake.coverage.retire")`. Submit the next intake
revision with those spans re-dispositioned as `non-claim` or `out-of-scope`, plus
one appended `amendments` entry naming each retired span, the exact prior
disposition and rationale it displaces, its new rationale, and the review ID that
ratified the intake. No finding, review, run, or work record may be part of that
transaction, and nothing else in the intake may change. The existing review keeps
binding the record, so no candidate is re-ratified. Then re-run
`closure_apply advance`: the retirement bumps the campaign revision while the job
holds a frozen one, and coverage is only re-digested at the next stage
transition.

A retirement is one-way. There is no transition back to `unresolved`, and once
the covering intake is clean `curation_submit` refuses a further supplementary
intake over that report. Retire only spans you have actually judged.

If the covering intake is not yet reviewed, or its defect is anything other than
an unresolved span, clear it through `review-subagent` instead: submit a further
curator intake over the same frozen report with every span disposed and review
it. Coverage is a union across reviewed intakes, so this supplies what closure
requires without disturbing any review already ratified over the earlier intake.

Do not attempt to reopen or re-return the run. An invalidated run that returned
is not exempt: its report is still retained and may still be cited by a ratified
finding, so closure accounts for it like any other run with a report.

Retiring a span to `out-of-scope` clears the closure coverage gate but does not
satisfy a cross-campaign normalization resolution, which accepts only
`candidate-finding`, `duplicate`, and `non-claim`. If the same report is also a
queued normalization source, prefer `non-claim` where it is the honest judgment.

`missing-report` means a run froze no report. Closure exempts a run that was
`aborted`, and one that was `invalidated` before it ever returned, because
neither can be cited: no coverage span can exist over a report that was never
written. So this blocker only ever names a run that is still `prepared` or
`running`. Return it, or end it through `review-subagent` as `aborted` with a
reason in `resultSummary`. Do not try to curate it - there is nothing to cover.

## Verify And Return

Require the archive manifest, generated archive README, closure receipt, truth
and backlog projections, navigation update, and retrieval reachability to
regenerate from canonical records. Report durable destinations, unresolved
exports, retained external references, and the closure receipt digest.

Do not commit unless the user explicitly asks.

## References

- Archive template: `<plugin-root>/templates/campaign/archive-README.md`.
- Governance: `<plugin-root>/references/knowledge-governance.md`.
