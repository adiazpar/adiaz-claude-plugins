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

## Resolve An Unsupported Retrieval Profile

If preview reports `unsupported-retrieval-profile`, export the stable packet
with `migrate-project --profile-conflict` (MCP action `profile-conflict`). The
packet captures the exact 0.7 profile bytes and source fingerprint, plus the
exact packaged 0.8 baseline, primary effective-profile digest, and benchmark
evidence digest. Do not copy old weights, remove the blocker by hand, or infer
that using the plugin baseline is approval.

Obtain an explicit manager decision and submit its strict JSON with
`--profile-decision DECISION.json` (MCP action `profile-decision`). The
submission must copy every identity from the current packet, set `decision`
to `retain-packaged-baseline`, set `explicitManagerApproval` to `true`, and
declare `authority` as `manager`, while keeping `projectProfileActivation`
`false`. Supply an explicit UTC RFC3339 `decidedAt`; the engine never invents
one from its wall clock. The first decision has an empty
`replacesDecisionDigest`. Replacing a different sealed decision requires both
that field and `--replace-profile-decision PRIOR_DIGEST` (or the equivalent MCP
expected-digest field) to equal the exact current decision digest.

Preview again. Confirm the sealed decision digest appears in the plan, only
the exact profile blocker disappeared, and the legacy profile still has a
provenance-only destination. Approve only the regenerated plan digest.
Application freezes the legacy bytes and the sealed decision under migration
audit state; it never writes or promotes a project retrieval profile. Project
calibration and any later profile promotion remain separately measured and
explicit workflows.

## Resolve Truth Atomicization Conflicts

If preview reports an implicit, empty, compound, or over-limit accepted-truth
claim, export the stable conflict packet with `migrate-project
--truth-conflicts` (MCP action `truth-conflicts`). Do not infer a boundary,
truncate the claim, or edit the legacy truth source.

For each conflict, obtain an explicit manager review whose ordered
`sourceText` rows cover every normalized explicit claim exactly once. If the
source has no explicit claim marker, cover the complete normalized document;
do not select a claim span by inference. Each row must provide one bounded
atomic claim and three to five reviewed retrieval questions. Submit it with
`--truth-review REVIEW.json` (MCP action
`truth-review`). The submission must bind the current packet and source
digests. Replacing an existing different review requires its exact prior
review digest.

Preview again after submission. Confirm the new plan contains every reviewed
split row, the review digest, a split navigation destination when applicable,
and no truth atomicization blocker. Only the regenerated plan digest can be
approved. During verification, require exact source-span coverage, frozen
provenance, resolvable rewritten links and dependencies, one compatibility
receipt per split, and retrieval of every resulting finding.

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
rechecks source, profile-decision, and result digests and continues from the
last completed operation. Ordinary state commands must refuse legacy inputs
with a direct migration instruction.

## Verify And Ratify

Run structural, coverage, retrieval, host-parity, archive, and restart gates.
Require all live campaigns to pass traversal coverage; closed legacy reports
must remain reachable and labeled unnormalized provenance. Prove that no
legacy writer, wrapper, or deprecated alias exists in the installed artifact.

The retrieval and host gates never accept generic check names plus caller-made
hashes. Use `migration-gate-artifact.schema.json`. Retrieval evidence must bind
the exact final `project-benchmark-v1` full report, final non-activating
calibration report and candidate bytes, and a paired case-level blinded
evaluation for every benchmark holdout case. The engine re-reads all four
regular files, recomputes their digests, rejects benchmark/calibration drift,
and derives every retrieval fingerprint and check handle itself.

Host evidence must use the fixed request/result/failure matrix in
`migration-host-conformance.schema.json`: real MCP, CLI, and Claude Code
captures for discovery, status, retrieval, expansion, role-boundary refusal,
bounded recovery, and the Claude CLI fallback. Codex is an optional host:
supply its captures only when the project uses the Codex host, and then its
complete scenario matrix must pass and match cross-host parity like every
other captured host. The engine recomputes trial IDs, semantic digests,
current MCP schema identity, cross-host parity, and per-host fingerprints.
Missing rows, arbitrary transport labels, self-authored semantic hashes, or
generic evidence strings fail closed.

Record only the resulting project-contained regular artifact with
`--gate retrieval-context` or `--gate host-parity`; `--verify` replays those
exact bytes and every nested evidence binding. If the benchmark, calibration,
candidate, blinded result, host capture, plan, or transaction changes, generate
fresh evidence instead of editing a receipt.

Ratification requires the user-approved plan digest and a complete immutable
migration receipt. Do not claim project migration until every required gate
passes. Keep source backups or frozen provenance until the receipt proves the
new records and archive are complete.

## References

- Versioned migration schemas: `<plugin-root>/knowledge/schemas/`.
