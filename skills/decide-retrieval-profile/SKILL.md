---
name: decide-retrieval-profile
description: >-
  Apply an explicit retrieval-profile decision when asked to promote a
  calibrated knowledge profile, reject a retrieval candidate, retain the
  current profile, or rollback to a previously accepted profile.
---

# Apply A Retrieval-Profile Decision

Change retrieval behavior only after an explicit user decision. Accept exactly
one action: `promote`, `reject`, `retain`, or `rollback`. Present the candidate
and obtain the missing choice before mutating anything.

## Step 1: Establish Authority And Target

Measurement freshness (benchmark staleness, evidence-pin health) is
checked and repaired here, at gate time, as part of this skill's own run -
it is never surfaced during onboarding or session start.


Read `<plugin-root>/references/knowledge-governance.md`, the active bootstrap
config, `.re-discipline/knowledge/README.md`, the current accepted profile, and
the candidate report.

- For a project profile, require a direct manager and explicit user approval.
- For a global baseline, require a plugin maintainer operating in the
  authoritative plugin repository.

Never let a drafter, hook, MCP read tool, benchmark, calibration run, telemetry
signal, or candidate file promote itself.

## Step 2: Validate The Candidate Or Rollback

For `promote`, require:

- the exact generated candidate with a valid detached candidate content hash
  and no `approval` object or promotion receipt;
- matching corpus, evaluation, parser, chunker, and model identities;
- a frozen-holdout full benchmark;
- passing authority, privacy, citation, freshness, abstention, exact-target,
  deterministic-replay, and token-budget gates;
- benchmark evidence for the sole declared effective profile, plus positive
  holdout evidence before any removed lane can return;
- no unreported regression against the accepted lexical baseline;
- a recorded benchmark digest and approval target.

Reject stale or manually edited candidates. Re-run the explicit benchmark
instead of waiving a failed gate.

For `rollback`, require a previously accepted, content-hashed profile whose
runtime identity remains compatible and whose benchmark evidence remains valid
for the current runtime. Treat an invalid rollback as a new candidate, not a
shortcut.

## Step 3: Apply The Explicit Action

### Promote

For a project profile:

1. Recompute the candidate content hash and reject any mismatch, inherited
   `approval`, or modification since the measured report.
2. Construct the accepted profile in memory from the exact generated
   candidate. Preserve its top-level base profile and stamp an `approval`
   promotion receipt only now, recording the explicit `promoted` decision,
   explicit-user-approval flag, candidate digest, benchmark-matrix digest,
   corpus and evaluation fingerprints, model/runtime identities, and
   calibration-report digest.
3. Leave the receipt's accepted `profileDigest` field empty, compute the
   canonical content hash over that completed profile, then set
   `profileDigest` to the result. Recompute it; the profile is valid only when
   the same empty-field hashing rule reproduces the receipt value.
4. Validate strict JSON, the finite capability matrix, every benchmark digest,
   the promotion receipt, and the bootstrap pointer before changing the active
   file.
5. Atomically replace
   `.re-discipline/knowledge/retrieval-profile.json` with the validated,
   receipt-stamped profile in one operation. Never copy an unsigned candidate
   directly into production and never write a receipt or hash in a separate
   partially applied step.
6. Reload the file and verify its receipt and content hash, then run health
   plus deterministic replay for the selected requested and effective
   profiles.
7. Report any active fallback without changing its identity.

For a global baseline, update only the source-owned plugin profile and its
sanitized cross-project evidence. Never copy project data upstream
automatically. Leave release and marketplace changes to the plugin release
workflow.

### Reject

Verify the exact candidate directory, report the rejection reason, then remove
only that disposable candidate state. Leave the accepted profile unchanged and
create no tracked rejection artifact.

### Retain

Leave the accepted profile and candidate state unchanged. Record why no
production change was made.

### Rollback

Atomically install the exact previously accepted generated profile, validate
its hash and capability matrix, then run health and deterministic replay.
Never reconstruct a prior profile by hand.

## Step 4: Verify And Report

Report to the user in plain language per
`<plugin-root>/references/reporting.md`; machinery identities go into the
campaign or run record, not the screen. Record in the decision or
campaign record:

- action and target scope;
- old and new requested-profile identities;
- effective profile, active lanes, model identities, and fallback reason;
- benchmark digest and hard-gate result;
- candidate digest and accepted-profile content hash;
- promotion-receipt decision and validation result when applicable;
- exact files changed or removed;
- whether tracked changes remain for user review.

Print to the user only:

```user-facing
Search profile decision applied: <accepted the tuned profile|kept the
current profile|rolled back>. The change is recorded in
<campaign or decision record path>.
```

Do not edit `.re-discipline/knowledge/policy.jsonc` as a ranking-profile
side effect. Do not accept memory or alter source authority.

Do not commit unless the user explicitly asks.

## Reference

- Governance and promotion gates:
  `<plugin-root>/references/knowledge-governance.md`.
- Candidate creation: `calibrate-knowledge`.
- Measurement: `benchmark-knowledge`.
