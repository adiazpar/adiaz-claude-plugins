---
name: checkpoint-campaign
description: This skill should be used at the END of any session that worked inside an active campaign without closing it, when invoked as /checkpoint-campaign <slug>, when asked to "checkpoint the campaign", "save campaign state", "wrap up for today", or when the PreCompact reminder fires mid-campaign. Rewrites CAMPAIGN.md's Current state so the next session resumes cold, sweeps spent one-off scratch scripts, and commits the keepers. This is NOT closure — close-campaign is the keystone; this is the recurring hygiene verb between sessions.
argument-hint: <slug>
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, AskUserQuestion
---

# Checkpoint-campaign — end-of-session hygiene (not closure)

A campaign can stay open for weeks. Without a recurring checkpoint, two things rot: **CAMPAIGN.md drifts** from what's actually true in the scratch (the next agent orients on stale state), and **one-off scripts pile up** (50 `_diag_*.py` files that nobody can tell apart from keepers). This verb runs at every session end and keeps both honest. Closure (`close-campaign`) stays rare and momentous; checkpointing is routine.

## Procedure

### Step 1: Rewrite "Current state" (the cold-resume contract)

Edit `active/<slug>/CAMPAIGN.md`:

- **"Current state" stays ≤ ~30 lines, newest first.** It must answer, for an agent with zero context: what just happened, what is proven (with pointers), what is the next move, what is blocked on what.
- Move anything older than the last session out of "Current state" into a `## Historical log` section at the bottom of the file (or fold it into an `evidence/*.md` note). History is for the chronicle; "Current state" is for resuming.
- Update **Open questions** (remove solved ones — note where each answer went) and **Dead ends so far** (add any new refuted hypotheses with DIRECT|INFERRED tags).
- Update the **Disposition manifest** for any new artifact produced this session (tag: ground-truth / capture / reproducible / keeper-tool).

### Step 2: Sweep the scratch

List the campaign's scripts and artifacts, then for each **one-off** (`_`-prefixed or obviously single-purpose) script/log:

- Its finding is recorded (in CAMPAIGN.md, an evidence note, or a truth file) → **delete it**. The recipe model means the record, not the script, is the durable thing.
- It is still load-bearing for the next session → keep, and make sure "Current state" names it.
- It turned out to be reusable beyond this campaign → don't let it rot in scratch: promote to `tools/` (generalize) now or note it as keeper-tool in the manifest.

```powershell
# survey: tracked vs untracked scratch
git -C . ls-files active/<slug> ; Get-ChildItem active/<slug>/scripts -File
```

Untracked spent scripts: `Remove-Item`. Tracked spent scripts: `git rm`. When unsure whether a script's finding is recorded, grep CAMPAIGN.md + evidence/ for its key result first — delete only what is genuinely captured.

### Step 3: Commit the checkpoint

One commit; format per `.claude/CLAUDE.md` §8:

```
campaign: <slug> -- checkpoint (current-state rewrite + scratch sweep)
```

Include CAMPAIGN.md, any kept new artifacts the campaign should track, and the deletions. Compiled-from-copyrighted-source artifacts stay gitignored (never force-add).

### Step 4: Confirm

```
Campaign <slug> checkpointed.
- Current state: rewritten (<N> lines), history pushed down
- Scratch: <N> spent scripts deleted, <M> keepers noted
- Next session resumes from CAMPAIGN.md "Current state".
```

## Anti-patterns this skill stops

- A 200-line stratified CAMPAIGN.md where "Current state" is six sessions of sediment.
- 50 undifferentiated `_diag_*.py` scripts at close-campaign time, forcing archaeology.
- "Status: OPEN — closure bar MET" — if the bar is met, this is the wrong verb: invoke `close-campaign`.

## Reference

- Closure (the keystone): `close-campaign` skill. The lifecycle: `.claude/CLAUDE.md` §4.
