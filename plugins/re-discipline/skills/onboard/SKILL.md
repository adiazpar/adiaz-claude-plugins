---
name: onboard
description: >-
  Orient a re-discipline project at session start or when asked to get caught
  up, show current project state, or resume an active campaign. Reads the
  canonical profile, active manager adapter, truth and history
  indexes, and active campaign masterfiles, then returns a compact orientation.
---

# Onboard A Re-Discipline Project

Onboarding is a read-only orientation. Directory location encodes trust:

| Location | Status |
|---|---|
| `docs/truth/` | Verified and durable. |
| `docs/history/` | Retrospective context and leads, never current authority. |
| `active/<slug>/` | Provisional, in-flight work. |
| `archive/` | Preserved evidence, not prose authority. |

## Step 1: Detect The Active Host

Determine the active host from the tools exposed by the session and the
instructions that loaded. Do not infer it merely because `.claude/` or
`.codex/` exists; a dual-compatible project contains both.

- **Claude Code:** read `.claude/CLAUDE.md`.
- **Codex:** read `.codex/AGENTS.md`.
- **Unknown host:** read both manager adapters, identify the applicable tool
  adapter, and state the uncertainty instead of silently choosing one.

Always read `.re-discipline/project-profile.md` first. It is the single source
for shared re-discipline laws, name, framing, mission, source of record,
portable tooling, and domain roles.

If the canonical profile is missing, stop substantive work and invoke
`init-project` in migration or recovery mode. A legacy
`.claude/project-profile.md` or `.codex/project-profile.md` is recovery input,
not permission to invent a replacement.

## Step 2: Read The Masterfiles

Read:

1. `docs/INDEX.md` for mission, current focus, and navigation.
2. `docs/truth/INDEX.md` for what is known now.
3. `docs/history/INDEX.md` for what has already been explored.

Report missing or dangling index links. Do not manufacture content during
onboarding.

## Step 3: Read Active State

List directories directly under `active/`. For each campaign, read
`active/<slug>/CAMPAIGN.md`, focusing on Objective, Current state, Open
questions, Dead ends, and the disposition manifest.

When no campaign is active, call the state a cold start and await direction.
Do not open a campaign merely because none exists.

## Step 4: Read Optional Agent State

If `agents/config.json` exists, list promoted external providers and the
configured backend. Treat absent or `native` as the active host. If
`recruiting/` exists, list candidate directories. Do not describe Claude Code
or Codex as the permanent default; the active manager host is the native
adapter for this session.

## Step 5: Return One Screen

Use this shape, replacing placeholders with facts from the files:

```text
## Onboarded - <project name>

Host: <Claude Code|Codex|other>
Mission: <one sentence>
Current focus: <one or two lines>

Active campaigns:
- <slug>: <status>; open questions: <count>
- none (cold start)

Agent roster: <native host plus promoted external providers>
Recent history: <latest chronicle and outcome>
Ready to: <resume named campaign, open a requested campaign, or await direction>
```

Stop after orientation. Do not begin substantive investigation until the user
directs it.

## Reference

- Host mapping: `<plugin-root>/references/runtime-adapters.md`.
- Canonical laws and project facts: `.re-discipline/project-profile.md`.
