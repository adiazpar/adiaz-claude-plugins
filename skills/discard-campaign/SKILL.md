---
name: discard-campaign
description: >-
  Destructively discard a re-discipline campaign only when the user explicitly
  asks to permanently remove that exact active or paused campaign without
  closure or archival projection. Requires strong literal confirmation,
  current digests, a reason, and post-removal verification.
---

# Discard A Campaign

Treat `campaign.discard` as an unsafe destructive exception, not as campaign
cleanup. Never infer permission from a request to close, merge, archive,
cancel, hide, or repair a campaign. Do not discard a real campaign unless the
user separately and explicitly requests that exact destruction.

## Establish The Exact Target

Call `state(mode="orient")`, then `state(mode="resume", campaignId=...)` and
`read` the exact campaign record. Require one matching campaign ID and slug,
status `open` or `paused`, a permitted manager actor, the current project head
revision and digest, the campaign record digest, and a non-empty reason.

Refuse ambiguity. Closed campaigns are immutable. Missing, malformed, and
already-discarded targets require no replacement action. Use closure when
durable truth or an archive must survive; use merge when history belongs in
another campaign; use reconciliation only for exact-record recovery.

## Confirm And Apply

Construct the confirmation exactly as:

```text
DISCARD <campaign-id> FROM <campaign-slug>
```

Submit `manager_apply` with action `campaign.discard`, the exact target and
head, identical `rationale` and `campaignDiscard.reason`, the literal
confirmation, `expectedCampaignDigest`, a correlation ID, and a stable
idempotency key. Supply `expectedTreeDigest` only when an independent trusted
process has computed it; the engine always computes and rechecks the tree
under its writer lock. The action-locked CLI equivalent is
`campaign-discard`. Label the pending action as destructive before invocation.

On an interrupted response, retry only the byte-equivalent semantic request
with the same key. Expect receipt replay. Never weaken the confirmation or
reuse the key for another target, reason, head, or digest.

## Verify Destruction

Call `state(mode="orient")` and verify that the target is absent from canonical
inventory and that its exact `active/<slug>` tree is gone. Require a healthy
head, one `campaign.discard` project event, and one immutable transaction
receipt. Confirm that no campaign archive, closure projection, truth promotion,
or copied source tree was created. Reconcile derived indexes after the
canonical removal.

Report the destroyed ID and slug, reason, event, receipt digest, and whether an
exact retry replayed. State plainly that discard is not recoverable from an
engine-owned campaign archive.
