---
name: promote-truth
description: >-
  Promote a DIRECT-evidenced campaign finding into docs/truth/, or augment an
  existing synthesis with new DIRECT evidence. This is the only truth-promotion
  door; INFERRED findings remain provisional. Writes a reproducible truth file
  and updates docs/truth/INDEX.md.
---

# Promote A Finding To Truth

Truth changes are deliberately rare. This skill is the only door from
provisional campaign work into `docs/truth/`.

## Step 1: Apply The Wall

State the claim in one or two value-precise sentences. Label every load-bearing
datum DIRECT or INFERRED. Read exact values from the primary artifact, not a
report summary or memory.

For subject-defined facts, inspect the source of record named in
`.re-discipline/project-profile.md` before empirical inference. A declaration
may be DIRECT for what is defined; a sample corpus is normally only evidence of
observed instances.

If any load-bearing step remains INFERRED, stop. Record the observation needed
to settle it in `CAMPAIGN.md`.

## Step 2: Require A Durable Verifier

Pass the second promotion gate only when at least one durable verifier lets a
future manager recheck the claim:

- a maintained source containing the exact value;
- a permanent test with its maintained fixture;
- a runnable recipe against the named subject revision.

A chronicle supplies provenance and is never sole empirical support. When
DIRECT evidence established the claim during the campaign but no durable
verifier survives, record a scoped historical observation for the chronicle
and do not promote it to current truth.

## Step 3: Check Existing Knowledge

Search truth and history for exact values, names, and key terms.

- A contradiction blocks ordinary promotion. Use `overturn` only when DIRECT
  evidence disconfirms an existing synthesis.
- An overlap is an augmentation, not a duplicate file.
- A dependency requires inverse links and a recheck of affected truth.

## Step 4: Choose The Truth Kind

- **atomic:** reproducible bedrock such as an exact behavior, layout, value, or
  protocol field;
- **synthesis:** an interpretation across atomic facts, with explicit scope and
  confidence.

## Step 5: Write Or Augment

Resolve the plugin root from this skill's path. For a new claim, render
`<plugin-root>/templates/truth-claim.md` to
`docs/truth/<subsystem>/<claim>.md`. Fill claim, kind, confidence, scope,
validity, re-verification trigger, dependencies, and verification.

The durable verifier must survive campaign deletion. Record:

- the maintained source, permanent test and fixture, or runnable recipe;
- the campaign chronicle as provenance and derivation context only.

For an augmentation, edit the existing synthesis in place, update its scope and
verification date, add the new evidence, and preserve dependency integrity.

## Step 6: Update Index And Campaign

Add or revise the one-line entry in `docs/truth/INDEX.md`. Update
`CAMPAIGN.md` to point to the promoted truth and remove any question it
directly resolves.

Re-open the finished truth file and verify every exact value against its cited
maintained source, permanent test and fixture, or runnable recipe.

Do not commit unless the user explicitly asks.

## Reference

- Truth template: `<plugin-root>/templates/truth-claim.md`.
- Disconfirmation: `overturn`.
- Closure: `close-campaign`.
