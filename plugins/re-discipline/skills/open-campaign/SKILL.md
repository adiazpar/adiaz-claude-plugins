---
name: open-campaign
description: This skill should be used when invoked as /open-campaign <slug> or when asked to "open a campaign", "start a new RE investigation", "scaffold a campaign workspace", "begin investigation on <topic>". Scaffolds active/<slug>/ with a CAMPAIGN.md masterfile (from the campaign-masterfile template) and the standard subdirs, then commits the scaffold. Slug-only naming.
argument-hint: <slug>
allowed-tools: Read, Write, Bash, AskUserQuestion
---

# Open-campaign — scaffold a new campaign workspace

A **campaign** is the unit of RE work. It is self-contained: everything it produces lives under `active/<slug>/`, so a subagent can treat it as its workspace and `close-campaign` can process it as one unit. You scaffold it here; you fill `CAMPAIGN.md`'s Objective so the closure bar is explicit from day one.

Open a campaign for: any investigation >30 min, anything with live tests / Ghidra / oracle runs, anything that will delegate to subagents. When in doubt, scaffold.

## Procedure

### Step 1: Validate the slug

The slug is the campaign's only name (no date prefix). It must be:
- lowercase kebab-case (hyphens; no spaces/underscores), 3-50 chars
- not already a directory under `active/`

If invalid or taken, ask the user for a different slug.

### Step 2: Create the workspace + subdirs

```powershell
$slug = "<slug>"
"scripts","ghidra","decomps","artifacts","evidence","subagents" |
  ForEach-Object { New-Item -ItemType Directory -Force -Path "active/$slug/$_" | Out-Null }
```

This is the canonical layout (each subdir's close-disposition is documented in the template's dir-map):
- `scripts/` — one-off python (reproducible → deleted at close)
- `ghidra/` — one-off Ghidra scripts (reproducible → deleted; reusable → promote to `tools/re/`)
- `decomps/` — decompile / trace logs (reproducible → deleted; the RECIPE goes in the truth)
- `artifacts/` — data (test rawmaps, generated maps, captured crash/freeze/watch JSON, ground-truth)
- `evidence/` — `.md` reasoning notes (folded into the chronicle at close)
- `subagents/<name>/` — per-subagent scratch (created by `delegate`)

### Step 3: Write CAMPAIGN.md from the template

Read `${CLAUDE_PLUGIN_ROOT}/templates/campaign-masterfile.md` and write it to `active/<slug>/CAMPAIGN.md`, filling the header + Objective.

Use AskUserQuestion (one field at a time) to fill:
1. **Status line** — one line of what's being investigated right now.
2. **Objective** — the question this campaign answers, AND what "solved" looks like (the **closure bar** — `close-campaign` checks against it).
3. **Open questions** — the unknowns gating closure (seed with at least one).
4. **Leads** — threads worth pulling; pointers to the truth/chronicle/backlog item that seeded this (if any).

Set `Opened:` to today. Leave Dead-ends and the Disposition manifest as empty scaffolds — they fill as the campaign runs.

If this campaign came from a `docs/backlog/<slug>.md` brief, pull the Objective from there and note it under Leads.

### Step 4: Update the front door

Add the campaign to the "Active campaigns" list in `docs/INDEX.md`:

```
- [<slug>](../active/<slug>/CAMPAIGN.md) — <one-line objective>
```

(Use the relative path that resolves from `docs/INDEX.md`.)

### Step 5: Commit the scaffold

```powershell
git add active/<slug> docs/INDEX.md
git commit -m "campaign: open <slug> -- <one-line objective>"
```

### Step 6: Confirm

```
Campaign opened: active/<slug>/.
CAMPAIGN.md holds the objective + closure bar; it's the first read for you and every subagent.
Subdirs ready: scripts/ ghidra/ decomps/ artifacts/ evidence/ subagents/.
Ready to work — what's the first move?
```

## Reference

- Masterfile template: `${CLAUDE_PLUGIN_ROOT}/templates/campaign-masterfile.md`.
- The lifecycle + the Wall: `.claude/CLAUDE.md` §4-5.
- Dispatch RE into this workspace via the `delegate` skill.
