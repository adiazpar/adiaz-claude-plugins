---
name: migrate-project
description: >-
  Preview, approve, apply, resume, verify, and ratify an explicit
  re-discipline 0.7-to-0.8 project conversion. This is the only workflow
  permitted to read legacy campaign state.
---

# Migrate A Project To 0.8

This skill is the sole legacy reader. It never writes a 0.7 record and never
converts state implicitly during onboarding, initialization, or ordinary
engine operations.

## Preview Without Mutation

Run the shared migrator's preview operation. Inventory every legacy
`CAMPAIGN.md`, `REVIEWS.md`, `subagents/` workspace, report review stamp,
truth record, historical record, configured host, and affected repository
path. Hash all inputs and emit a versioned migration plan with blockers,
classification uncertainties, live-campaign designations, physical moves,
and a preview digest.

Do not edit source state. Present the digest, counts, ambiguities, dirty-tree
status, and proposed destinations to the user.

## Require Explicit Approval

Apply only an exact approved preview digest. A changed source digest invalidates
approval and requires a new preview. Acquire only the short project-scoped
transaction lock needed for each published operation.

## Apply Resumably

Use migration operations with expected revisions and idempotency keys. Create
only 0.8 records. Shadow-index every legacy report as digest-verified,
labeled provenance. Exhaustively normalize campaigns explicitly designated
live; queue closed material for demand-driven normalization.

Persist an operation receipt before advancing. On interruption, `resume`
rechecks source and result digests and continues from the last completed
operation. Ordinary state commands must refuse legacy inputs with a direct
migration instruction.

## Verify And Ratify

Run structural, coverage, retrieval, host-parity, archive, and restart gates.
Require all live campaigns to pass traversal coverage; closed legacy reports
must remain reachable and labeled unnormalized provenance. Prove that no
legacy writer, wrapper, or deprecated alias exists in the installed artifact.

Ratification requires the user-approved plan digest and a complete immutable
migration receipt. Do not claim project migration until every required gate
passes. Keep source backups or frozen provenance until the receipt proves the
new records and archive are complete.

## References

- Detailed mapping: `<plugin-root>/docs/migrations/0.7-to-0.8.md`.
- Versioned migration schemas: `<plugin-root>/knowledge/schemas/`.
