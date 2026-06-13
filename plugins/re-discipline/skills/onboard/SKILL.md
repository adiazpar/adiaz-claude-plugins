---
name: onboard
description: This skill should be used at the start of any new session in the snaphak-re project, when invoked as /onboard, or when asked to "orient yourself in the project", "get caught up on the project state", "show me where we are". Reads the project front door + the truth and history masterfiles + lists active campaigns, then produces a one-screen orientation. Handles both cold-starts (no active campaign) and joining an in-flight campaign.
argument-hint: (no arguments)
allowed-tools: Read, Glob, Bash
---

# Onboard — orient in the snaphak-re project

You are the steward of this living knowledge system. This is your first action every session: load the masterfiles and produce a one-screen orientation so you can begin work with full project state. Orientation is **two reads** by design (the masterfile structure exists for exactly this).

## The epistemic-status map (internalize this before reading anything)

The directory an artifact lives in *is* its trust level:

| Location | Status | Trust |
|---|---|---|
| `active/<slug>/` | provisional, in-flight | do NOT treat as fact — someone is mid-thought |
| `docs/truth/` | verified, durable | trust it (it earned its place across the Wall) |
| `docs/history/` | retrospective narrative + leads | never normative — "what a past campaign did/learned" |
| `archive/` | preserved irreproducible material | evidence, not prose |
| `src/` + `tools/` | the live system | maintained code |

## Procedure

### Step 1: Read project conventions

Read `.claude/CLAUDE.md` in full — the always-loaded behavioral contract (manager voice, the Wall, the lifecycle verbs).

**Health check — the project profile.** `.claude/CLAUDE.md` imports `@project-profile.md` (the domain single-source: mission, framing, source-of-record, tooling). If that import **dangles** — `.claude/project-profile.md` is missing — the project's identity is gone from context (and `dispatch.ps1` has fallen back to a generic framing). Do NOT proceed to orient on a project whose identity you can't see. Flag it immediately and recover: **`git restore .claude/project-profile.md`** (lossless if it was committed), or invoke `init-project` (it detects this as recovery mode — git-restore first, reconstruct-with-confirmation otherwise). Then re-read it and continue onboarding.

### Step 2: Read the front door

Read `docs/INDEX.md`. Note: mission, current focus, and its pointers to the two indexes + the active-campaign list.

### Step 3: Read the two masterfiles

- `docs/truth/INDEX.md` — what is known **now** (atomic facts + syntheses, by subsystem).
- `docs/history/INDEX.md` — what has been **explored**; one line per chronicle + cross-cutting leads. (It defers to `truth/INDEX.md` for current state.)

### Step 4: List active campaigns

```powershell
Get-ChildItem active -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name
```

- **No active campaign (cold-start):** orient from the two indexes alone; await direction. Do not invent work.
- **Joining an in-flight campaign:** read that campaign's `active/<slug>/CAMPAIGN.md` in full — Objective, Current state, Open questions, Dead-ends-so-far. That file is the campaign's local index and tells you exactly where the prior session left off.

### Step 4b: Note the agent roster

The team that does the RE legwork:

```powershell
# Promoted external agents (the team):
(Get-Content agents/config.json -Raw | ConvertFrom-Json).providers.PSObject.Properties |
  Where-Object { $_.Value.promoted } | Select-Object -ExpandProperty Name
# Any candidate mid-interview:
Get-ChildItem recruiting -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name
```

The default backend is native Claude subagents (`agents/config.json → backend`); promoted external agents (e.g. codex) are dispatched via `delegate` when the user routes to them. A candidate in `recruiting/` means a hire is mid-flight (see `hire-agent`/`decide-agent`).

### Step 5: Produce the orientation summary

```
## Onboarded — snaphak-re

**Mission:** <1 sentence from docs/INDEX.md>

**Current focus:** <1-2 lines>

**Active campaigns:**
- <slug>: <status line from CAMPAIGN.md> — open questions: <count>
- (or: none — cold-start)

**Agent roster:** <promoted providers, e.g. codex> (backend: <claude|provider>) — candidates in interview: <recruiting/ dirs, or none>

**Recently explored (history):**
- <most recent chronicle>: <one-line outcome>

**Ready to:** Awaiting direction. Options:
- Open a new campaign — invoke `open-campaign <slug>`.
- Pick up an active campaign — name the slug (I'll re-read its CAMPAIGN.md and continue).
- Promote a DIRECT finding to truth, review a returned subagent, or just answer questions.
```

### Step 6: Stop

Onboarding is orientation only. Do not begin substantive RE work until the user gives direction.

## Reference

- Conventions + the lifecycle: `.claude/CLAUDE.md`.
- Design source of truth: `superpowers/specs/2026-05-25-re-discipline-v2-campaign-lifecycle-design.md`.
