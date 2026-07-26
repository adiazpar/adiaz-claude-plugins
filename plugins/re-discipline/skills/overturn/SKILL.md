---
name: overturn
description: >-
  Overturn a prior synthesis only when new DIRECT evidence disconfirms it.
  Corrects or removes the truth in place, rechecks dependents, and requires the
  old claim and disconfirming evidence to be preserved in the campaign
  chronicle.
---

# Overturn A Synthesis

An overturn is rare and explicit. It requires DIRECT evidence that an existing
synthesis is false. A more plausible alternative or narrower interpretation is
not enough.

## Step 1: Prove Disconfirmation

Quote the current claim and list the disconfirming observations. Read exact
values from the primary artifacts. If the old claim could still be true under
the new evidence, stop and use `promote-truth` to narrow scope or confidence
instead.

Atomic facts should first be checked for source revision, build drift,
transcription error, or changed validity conditions. Most legitimate
overturns target syntheses.

## Step 2: Establish The Replacement

State the corrected claim only when DIRECT evidence supports it and a
maintained source, permanent test and fixture, or runnable recipe can recheck
it later. A chronicle supplies provenance, not sole empirical support. It is
valid to remove a false synthesis without replacing it; record the resulting
gap as an open campaign question.

## Step 3: Correct Current Truth

- Rewrite the existing file in place when the replacement has the same home.
- Remove the file when no current claim survives; do not leave a normative
  zombie or create a separate refuted-truth tree.
- Update verification date, scope, confidence, and verification basis.
- Search `Depends-on` links and recheck every dependent claim.
- Remove or retarget the entry in `docs/truth/INDEX.md`.

## Step 4: Preserve The Old Claim Retrospectively

Quote the old claim verbatim in the active campaign's Dead ends section with
the DIRECT disconfirming evidence and any residual inference. The closing
chronicle must preserve that explanation so the dead claim is not rediscovered.
Keep the old claim and disconfirmation in history as provenance; do not cite
history as the replacement truth's empirical support.

Verify that current truth contains only the replacement, while history retains
the old claim as retrospective context.

Do not commit unless the user explicitly asks.

## Reference

- Downgrade or augmentation: `promote-truth`.
- Chronicle template: `<plugin-root>/templates/chronicle.md`.
- Active manager adapter: `.claude/CLAUDE.md` or `.codex/AGENTS.md`.
