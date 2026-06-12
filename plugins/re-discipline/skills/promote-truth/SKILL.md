---
name: promote-truth
description: This skill should be used when graduating a DIRECT-evidenced finding into docs/truth/, when asked to "promote this to truth", "graduate this finding", "write the truth file for X", "update the truth about Y" (augmenting a synthesis with new DIRECT evidence). It is the ONLY door from a campaign's scratch into docs/truth/. Gate: DIRECT evidence only — INFERRED findings stay in the campaign. Writes/updates the truth file from the truth-claim template with a self-verifying recipe citation, tags atomic vs synthesis, and updates docs/truth/INDEX.md.
argument-hint: <claim> (and target truth path, if augmenting)
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, Agent, AskUserQuestion
---

# Promote-truth — the only door from scratch into docs/truth/

Truth edits are **rare and momentous**. A finding crosses this door only on **DIRECT evidence**; everything else stays provisional in `active/`. Frequent truth edits are a signal the Wall was breached — slow down and re-check the evidence class. This skill folds in the old DIRECT/INFERRED cross-check and the new-claim and augment paths.

## The Wall (the gate — apply before writing anything)

Classify every supporting piece of evidence. Ask: **"Could this evidence exist even if the claim were false?"** If yes → INFERRED.

| Class | What it is |
|---|---|
| **DIRECT** | Observed, impossible if the claim were false: a decompiled instruction you can read; an oracle byte-diff (`/ser_roundtrip` returns the exact bytes); a live A/B test where the variable under test is the ONLY change; a value read from the game's own decl/schema in `reference/`. |
| **INFERRED** | Best-explanation; other readings survive: circumstantial call-chain attribution; "behavior changed when I changed Y" among other changes; elimination ("not Z, so W"); a corpus-mined pattern presented as a complete grammar. |

**Authoritative-source-first:** for any fact the *game itself defines* (wiring grammar, schemas, ref/var counts, layouts, enums), a value read from `reference/` is DIRECT/authoritative; a corpus-mined inference is INFERRED (an attested subset, never proof of the complete grammar). Check `reference/` before promoting any game-defined claim — and prefer it.

**Verdict:** all supporting evidence DIRECT → proceed. Any load-bearing evidence INFERRED → **STOP**: the claim does not cross. Leave it in the campaign, and note in `CAMPAIGN.md` exactly what DIRECT evidence (which decomp, which oracle call, which A/B) would let it cross later.

**Transcribe from the artifact, not the summary.** For any value-precise claim — an RVA, offset, byte, key string, enum value — read the exact value off the cited recipe's **actual output / primary artifact** (the `decomps/` decompile log, the `/ser_roundtrip` bytes, the raw `reference/` decl) right before you write it into the truth file. Never copy it from a subagent's prose summary or from memory. A mis-transcribed RVA/key still *passes the Wall* (the underlying evidence IS DIRECT) yet writes a wrong value into truth — the silent failure mode. (Mirror of `review-subagent`: a subagent's prose is a claim *about* its evidence, not the evidence.)

## Procedure

### Step 1: State the claim + classify its evidence

One or two sentences: the claim, and each supporting datum labeled DIRECT/INFERRED. Be honest — the default failure is calling an inference direct. If anything load-bearing is INFERRED, stop here (see the Wall verdict).

### Step 2: Contradiction + overlap scan

Grep `docs/truth/` and the relevant `docs/history/chronicles/` for the subsystem, the RVA/function names, the entity/field names, and the claim's key nouns. Classify each hit:
- **Contradiction** — an existing truth asserts the opposite, or a chronicle's dead-ends already buried this idea → **BLOCK**: resolve which is right first (if the new DIRECT evidence overturns a prior *synthesis*, that's the `overturn` skill, not this one).
- **Overlap** — an existing truth already covers this → this is an **augment** (Step 4b), not a new file.
- **Dependency** — note any truth whose `Depends-on` would be affected.

For a high-stakes new synthesis other work will build on, optionally `delegate` a cheap (<15 min, read-only) fresh-eyes subagent given ONLY the claim + the related truth files: "does this contradict anything here, and is the evidence DIRECT or INFERRED?" A reviewer with no stake in the hypothesis catches confirmation bias the author can't.

### Step 3: Tag the kind

- **atomic** — a bedrock fact reproducible from the binary/data (an RVA's behavior, a struct layout, a field offset). Write-once; almost never revised. Prefer many of these.
- **synthesis** — an interpretation of many atomic facts (an encoding rule, a philosophy). Carries an explicit **scope + confidence**; this is the augmentable / overturnable layer.

### Step 4a: Write a NEW truth file

Choose `docs/truth/<subsystem>/<claim>.md` (everything needs a subsystem home: binaries, codec, daemon, engine, snaphak, tooling). Render `${CLAUDE_PLUGIN_ROOT}/templates/truth-claim.md`, filling:
- **Claim** (one sentence), **Kind** (atomic|synthesis), **Confidence** (Strong only with fully DIRECT evidence; synthesis states its **Scope**).
- **Validity** (Verified: today; DOOM/SnapHak build pointers), **Re-verify trigger** (e.g. "after any DOOM patch" / "never — reproducible from the binary").
- **Depends-on** — cross-link related truth files (and add the inverse link in those files).
- **Evidence** — a **recipe citation that survives scratch deletion**: prefer a reproduction recipe (`tools/re/run.ps1 decompile_fn.py 0xRVA`, a `tests/` test, an oracle call) — self-verifying, regenerates fresh, valid as long as the binary is. For irreproducible support, point at `archive/`. Always also point at the **chronicle** that will hold the derivation.

### Step 4b: AUGMENT an existing synthesis (new DIRECT evidence refines, not refutes)

Edit the target truth file in place: revise the claim/scope, bump `Verified:` to today, add the new recipe/archive citation. (No snapshot step — git history + the chronicle hold prior versions; the snapshot tree is retired.) If a new DIRECT finding *contradicts* the existing claim rather than refining it, this is the `overturn` skill instead.

### Step 5: Update docs/truth/INDEX.md

Add (new) or revise (augment) the one-line entry under the right subsystem in `docs/truth/INDEX.md`. This masterfile is what `onboard` reads — keep it current.

### Step 6: Reflect into the campaign

In `active/<slug>/CAMPAIGN.md`, mark the promoted finding (move it out of Open questions; note "→ docs/truth/<...>"). The full derivation still goes into the chronicle at close.

### Step 7: Commit

```powershell
git add docs/truth/<...>.md docs/truth/INDEX.md active/<slug>/CAMPAIGN.md
git commit -m "truth: <subsystem> -- promote <claim-slug> (DIRECT)"
```

### Step 8: Confirm

```
Promoted → docs/truth/<subsystem>/<claim>.md  (Kind: <atomic|synthesis>, Confidence: <...>)
Evidence is a self-verifying recipe; INDEX updated.
```

## Anti-patterns this skill stops

- Writing a current best-guess as `Strong` truth because it's the only idea you have — that's INFERRED; it stays in the campaign.
- Treating an elimination ("not X") as proof of the alternative ("therefore Y").
- Corpus-mining or RE-deriving a game-defined fact `reference/` already declares authoritatively.
- Silently creating a truth that contradicts an existing one (the BLOCK forces resolution).
- Transcribing a value (RVA/offset/key/enum) from a subagent's prose summary or from memory instead of reading it off the primary artifact — it passes the Wall but writes a wrong value into truth.

## Reference

- Truth template: `${CLAUDE_PLUGIN_ROOT}/templates/truth-claim.md`.
- Disconfirming a prior synthesis: `overturn` skill. Closing the campaign: `close-campaign` skill.
- The model + the Wall: `.claude/CLAUDE.md` §5.
