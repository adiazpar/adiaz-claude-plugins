---
name: overturn
description: >-
  Challenge, narrow, invalidate, or supersede a re-discipline finding when new
  evidence conflicts with it. Preserves the prior claim, analyzes dependents,
  and queues any truth correction for closure.
---

# Challenge Or Overturn A Finding

Never silently rewrite a claim. Contrary evidence first creates a challenge.

## Establish The Challenge

Identify the exact finding revision and contrary evidence handles. Submit
`manager_apply` with action `finding.challenge`, the exact state-head revision
and digest, the challenged record digest, actor, rationale, idempotency key,
and source run or review IDs. The engine must expose affected dependents and
overlay the challenge in retrieval immediately.

## Resolve By Manager Review

Inspect the original evidence, new evidence, scope, dependents, projections,
and surviving alternatives. Through an immutable review, choose one:

- dismiss the challenge;
- narrow scope or applicability;
- mark the finding historical or invalid;
- supersede it with a new atomic finding;
- schedule correction or removal of a current-truth projection at closure.

Create verification work for every dependent whose conclusion may change.
Preserve the original finding, challenge, decision, and recheck results.

An active campaign may update campaign knowledge but may not edit a projected
truth file. The closure transaction applies approved truth corrections
atomically with archive and provenance updates.

## Verify

Call `trace` on the old and new finding handles. Require visible challenge or
supersession relations, correct dependent status, immutable review receipt,
and no unapproved truth filesystem change.

## References

- Governance: `<plugin-root>/references/knowledge-governance.md`.
- Closure projection: `close-campaign`.
