---
name: close-campaign
description: This skill should be used when a campaign is irrefutably solved and ready to close, at end of a session that solved one, or when asked to "close the campaign", "distill the findings", "wrap up this investigation", "write the chronicle and clean up the scratch". The keystone of the lifecycle: promotes DIRECT findings to truth, preserves irreproducible artifacts to archive/, writes the chronicle, promotes keeper tools/tests to the live system, then deletes active/<slug>/ — but ONLY if the chronicle is rich enough to re-open from. If not irrefutably solved, the campaign stays open.
argument-hint: <slug>
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, Agent, AskUserQuestion
---

# Close-campaign — the keystone

Closing converts a campaign from provisional scratch into durable record: the DIRECT findings become **truth**, the irreproducible material becomes **archive**, the journey becomes a **chronicle**, the keeper code joins the **live system**, and the scratch is **deleted**. Deletion is safe *because* of the recipe model — reproducible evidence becomes a recipe, irreproducible evidence moves to `archive/`, the narrative lives in the chronicle.

## Closure bar (check FIRST — do not close prematurely)

Close only when the campaign's Objective is **irrefutably solved** and the chronicle you're about to write is **rich enough to re-open the topic from** (a future agent could resume with no other context). If either fails, **do not close** — the campaign stays open across sessions (the next agent picks it up via `onboard`). If the work yielded a future *direction* rather than closure, write a `docs/backlog/<slug>.md` brief instead and leave the campaign open or convert it.

(For end-of-session checkpoints that are NOT closure: invoke the `checkpoint-campaign` skill — current-state rewrite + scratch sweep + commit. That's not this skill.)

## Procedure

### Step 1: Fresh-eyes triage (recommended for non-trivial campaigns)

`delegate`-style, dispatch a fresh OPUS subagent (read-only, `run_in_background: false`) over `active/<slug>/` to propose: which findings are DIRECT-evidenced and ready to promote, which existing truths an overturn touches, which artifacts are reproducible/ground-truth/capture, and the dead-ends to absorb. It writes a proposal to `active/<slug>/evidence/close-proposal-<date>.md`; you ratify. A fresh reviewer catches the author's confirmation bias. (Skip for tiny campaigns where you already hold all the evidence.)

### Step 2 (a): Promote the DIRECT findings → truth

For each finding that crosses the Wall, invoke `promote-truth` (new file or augment). INFERRED findings do NOT promote — they become dead-ends or leads in the chronicle. If a DIRECT finding disconfirms a prior synthesis, invoke `overturn`. Present the promotion list to the user (AskUserQuestion: approve all / selective / inspect) before executing.

### Step 2 (b): Disposition the artifacts (the manifest makes this mechanical)

Read the **Disposition manifest** in `CAMPAIGN.md`; for any untagged artifact, tag it now. Then:
- **ground-truth** (engine-saved rawmaps authored in the SnapMap UI; other irreplaceable data) → move to `archive/ground-truth-maps/`.
- **capture** (one-time live crash/freeze/watch snapshots) → move to `archive/evidence/` **only if a truth cites it**; otherwise delete (regenerable diagnostic spam is gitignored and never kept).
- **reproducible** (decomp logs, round-trip outputs, oracle results) → **delete**; the evidence becomes a **recipe** in the truth file (`tools/re/run.ps1 decompile_fn.py 0xRVA`, a `tests/` test, an oracle call). A recipe beats a frozen log — it regenerates fresh and stays valid as long as the binary does.
- **expensive-reproducible** (multi-hour recovery) → recipe **and** archive the output.

```powershell
$slug="<slug>"
New-Item -ItemType Directory -Force -Path "archive/ground-truth-maps","archive/evidence" | Out-Null
# git mv each ground-truth / cited-capture artifact into archive/...
```

### Step 2 (c): Promote keeper tools/tests to the live system

- Reusable Ghidra helpers in `active/<slug>/ghidra/` → `tools/re/` (generalize, drop the campaign-specific bits).
- A permanent regression test → `tests/` (e.g. the round-trip / compat test the campaign produced).
- Reimplementation code the campaign matured → `src/`.
- A genuinely spent one-off worth keeping only for provenance → `archive/scripts/` (rare).

### Step 3: Write the chronicle

Render `${CLAUDE_PLUGIN_ROOT}/templates/chronicle.md` to `docs/history/chronicles/<date>-<topic>.md` (`<date>` = today, `<topic>` = the campaign's subject). Write in **retrospective, time-stamped voice** — never imperative — so it can never be mistaken for current truth. Mirror the exemplar `docs/history/chronicles/2026-05-25-connection-encoding.md`. Sections:
- **The question** — what the campaign set out to solve.
- **The journey** — numbered, past-tense: wrong frames, pivots, the breakthrough, the live validation.
- **Dead ends — do NOT retry** — absorb every refuted hypothesis (the old standalone `refuted/` files live here now), each with a one-line *why* and DIRECT|INFERRED. Any `overturn` from this campaign quotes the old truth here.
- **Truths produced** — links to the `docs/truth/` files promoted.
- **Reproduction recipes** — the exact commands / test files / oracle calls / `archive/` pointers that regenerate the evidence.
- **Leads for future campaigns** — what's left; pointers to `docs/backlog/<item>.md`.
- **Provenance** — key commit SHAs; `archive/` paths for irreproducible artifacts; "Folded from (deleted on close):" the scratch sources whose content now lives in this chronicle.

### Step 4: Delete the scratch (the irreversible step — gate on Step 3)

ONLY after the chronicle is written and rich enough to re-open from, and all artifacts are dispositioned (archived or recipe-backed or deleted):

```powershell
git rm -r active/<slug>
```

Nothing of value is lost: truth holds the facts, archive holds the irreproducible material, the chronicle holds the journey + recipes, the live system holds the keeper code. If you hesitate here, the chronicle isn't rich enough yet — go back to Step 3.

### Step 5: Update the masterfiles + prune auto-memory

- `docs/history/INDEX.md` — add a one-line entry for the new chronicle + any cross-cutting leads it surfaced.
- `docs/INDEX.md` — remove the campaign from "Active campaigns".
- `docs/truth/INDEX.md` — already updated by the promote/overturn steps; verify.
- **Auto-memory prune:** review `MEMORY.md` entries touching this campaign's subsystems. Memory entries are pointers into truth — when a truth was augmented/overturned or a campaign path (`active/<slug>/...`) they cite is about to be deleted, update or delete the memory file now. A stale memory pointer is the cross-session drift vector this step exists to cut.

### Step 6: Commit

Use the PS 5.1 multi-line workflow (write body to `$env:TEMP` via `[System.IO.File]::WriteAllText` with a no-BOM UTF8 encoding, then `git commit -F <file>`). Group sensibly:

```
campaign: close <slug> -- chronicle + N truths + archive; scratch deleted
```

List the truth files added/overturned and the chronicle path in the body.

### Step 7: Confirm

```
Campaign <slug> CLOSED.
- Truths: <N added, M overturned> (docs/truth/INDEX.md updated)
- Chronicle: docs/history/chronicles/<date>-<topic>.md
- Archived: <ground-truth + cited captures>
- Promoted to live system: <tools/re|verify|src items>
- Scratch active/<slug>/ deleted (re-openable from the chronicle).
```

## Reference

- Chronicle template + exemplar: `${CLAUDE_PLUGIN_ROOT}/templates/chronicle.md`; `docs/history/chronicles/2026-05-25-connection-encoding.md`.
- Promotion / overturn gates: `promote-truth`, `overturn` skills.
- The recipe model + lifecycle: `.claude/CLAUDE.md` §5 + the design spec `superpowers/specs/2026-05-25-re-discipline-v2-campaign-lifecycle-design.md`.
