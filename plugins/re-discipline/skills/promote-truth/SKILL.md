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

## Step 2: Check Existing Knowledge

Search truth and history for exact values, names, and key terms.

- A contradiction blocks ordinary promotion. Use `overturn` only when DIRECT
  evidence disconfirms an existing synthesis.
- An overlap is an augmentation, not a duplicate file.
- A dependency requires inverse links and a recheck of affected truth.

## Step 3: Choose The Truth Kind

- **atomic:** reproducible bedrock such as an exact behavior, layout, value, or
  protocol field;
- **synthesis:** an interpretation across atomic facts, with explicit scope and
  confidence.

## Step 4: Write Or Augment

Resolve the plugin root from this skill's path. For a new claim, render
`<plugin-root>/templates/truth-claim.md` to
`docs/truth/<subsystem>/<claim>.md`. Fill claim, kind, confidence, scope,
validity, re-verification trigger, dependencies, and evidence.

Evidence must survive campaign deletion:

- a runnable reproduction recipe;
- a permanent test;
- a preserved irreproducible artifact in `archive/`;
- the campaign chronicle for derivation context.

For an augmentation, edit the existing synthesis in place, update its scope and
verification date, add the new evidence, and preserve dependency integrity.

## Step 5: Update Index And Campaign

Add or revise the one-line entry in `docs/truth/INDEX.md`. Update
`CAMPAIGN.md` to point to the promoted truth and remove any question it
directly resolves.

Re-open the finished truth file and verify every exact value against its cited
artifact or recipe.

Do not commit unless the user explicitly asks.

## Reference

- Truth template: `<plugin-root>/templates/truth-claim.md`.
- Disconfirmation: `overturn`.
- Closure: `close-campaign`.
