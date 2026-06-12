---
name: overturn
description: This skill should be used in the RARE case where a campaign produces DIRECT evidence that disconfirms an existing truth (a prior synthesis), or when asked to "overturn the claim that X", "this truth is wrong", "the new evidence disproves docs/truth/...". Corrects the truth in place; the overturning campaign's chronicle quotes the old claim and explains why it was a dead-end. Requires DIRECT disconfirmation — if the disconfirmation is INFERRED, it is a downgrade via promote-truth, NOT an overturn. There is no separate refuted/ tree (retired).
argument-hint: <target truth path>
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, Agent, AskUserQuestion
---

# Overturn — disconfirm a prior truth (rare, loud, narrated)

An overturn is a momentous, infrequent event: a campaign has produced DIRECT evidence that a previously-promoted **synthesis** is wrong. Because the Wall keeps INFERRED material out of truth, this should be uncommon — frequent overturns mean the Wall is leaking. Truth is corrected *in place* (no zombie file, no separate `refuted/` tree — that tree is retired). The narrative of *why the old claim was wrong* lives in the overturning campaign's **chronicle**, which quotes the dead claim so a future agent never re-derives it.

## The gate: DIRECT disconfirmation only

This is the symmetric high-stakes case to promotion. A wrongly-overturned truth is buried with a "don't retry" sign on something that may actually be correct. Classify the **disconfirming** evidence:

- **DIRECT disconfirmation** — observed; impossible if the old claim were true (a live A/B that isolates the variable; an oracle byte-diff contradicting the claimed bytes; a decompiled instruction that reads the opposite). → Overturn is appropriate; proceed.
- **INFERRED disconfirmation** — "a better alternative explanation exists" / an elimination argument / a circumstantial call-chain attribution. → This is **NOT an overturn.** It's a **downgrade**: use `promote-truth`'s augment path (Step 4b) to lower the claim's Confidence and narrow its Scope, recording what would resurrect it. Do not bury a possibly-correct claim on an inference.

> Worked lesson: the `cachedActivator` hypothesis was overturned partly on a DIRECT A/B (wire-dependence observed) but partly on attributing a crash to a `"Too many connections!"` string that was never actually printed (INFERRED). The A/B carried it — but the *stated reasoning* leaned on the inference. A cross-check would have said: hold the residual inference explicitly until the CSR-build decomp confirms the actual throw. Overturn on the DIRECT part; record the residual inference in the chronicle.

If the disconfirmation is INFERRED → STOP and take the downgrade path instead.

## Procedure

### Step 1: State the target + the disconfirming evidence

Name the `docs/truth/<...>.md` being overturned, its current claim text, and the new disconfirming evidence — each datum labeled DIRECT/INFERRED. Confirm at least one load-bearing datum is DIRECT (else → downgrade via promote-truth).

### Step 2: Confirm it's a synthesis, not an atomic fact

Atomic facts (an RVA's instruction, a layout) are reproducible from the binary and almost never wrong — if one seems wrong, suspect a build change (re-verify against `docs/truth/binaries/build-state.md`) or a misread before overturning. Overturns almost always target **syntheses**.

### Step 3: Establish the replacement (if any)

What's true instead? If the replacement is itself DIRECT-evidenced, you'll `promote-truth` it (new file or augment). An overturn may leave a gap (claim removed, nothing replaces it yet) — that's allowed; record the open question in `CAMPAIGN.md`.

### Step 4: Correct the truth in place

- If a replacement claim exists for the same file: edit the file — rewrite the Claim/Scope/Confidence, bump `Verified:`, swap in the new recipe citation. (No snapshot — git history holds the prior version.)
- If the claim is simply wrong and nothing replaces it: `git rm` the truth file (no zombies). Capture the prior claim text now (you'll quote it in the chronicle — Step 6).
- Walk `Depends-on` inverses: Grep `docs/truth/` for files that cite the overturned one; re-check each against the new reality and augment/correct as needed (this replaces the old stale-recheck cascade — the high Wall makes it small).

### Step 5: Update docs/truth/INDEX.md

Remove or retarget the overturned claim's line in `docs/truth/INDEX.md`.

### Step 6: Narrate it in the chronicle (this is mandatory)

The overturning campaign's chronicle (`docs/history/chronicles/<date>-<topic>.md`, written at `close-campaign`) MUST contain, under "Dead ends — do NOT retry":
- the **old claim quoted verbatim**, and
- **why** it was wrong + the DIRECT evidence that disconfirmed it (and any residual inference, so it can be reopened).

If the campaign isn't closing yet, record the quote + reasoning in `active/<slug>/CAMPAIGN.md` "Dead ends so far" so it survives into the chronicle. The chronicle is the only durable record of the overturn — there is no refuted/ file.

### Step 7: Commit

```powershell
git add docs/truth docs/truth/INDEX.md active/<slug>/CAMPAIGN.md
git commit -m "truth: <subsystem> -- overturn <claim-slug> (DIRECT disconfirmation; replaced by <new|none>)"
```

### Step 8: Confirm

```
Overturned docs/truth/<...> (DIRECT disconfirmation).
Replacement: <new truth path | none — open question recorded>.
Old claim quoted in the campaign's dead-ends → will land in the chronicle at close.
Depends-on inverses re-checked: <list>.
```

## Reference

- The downgrade alternative (for INFERRED disconfirmation): `promote-truth` skill, augment path.
- Where the overturn is narrated: `close-campaign` skill + `${CLAUDE_PLUGIN_ROOT}/templates/chronicle.md`.
- The Wall: `.claude/CLAUDE.md` §5.
