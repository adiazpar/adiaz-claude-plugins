# {{PROJECT_NAME}} - Project Map

## Mission

See `.re-discipline/project-profile.md`, the single source for project identity
and mission. Framing: {{ONE_LINE_FRAMING}}

## Knowledge Map

| Read | Purpose |
|---|---|
| [`truth/INDEX.md`](truth/INDEX.md) | What is known now. |
| [`history/INDEX.md`](history/INDEX.md) | What has been explored. |
| [`backlog/`](backlog/) | Deferred campaign briefs. |
| `../active/<slug>/CAMPAIGN.md` | Provisional work in flight. |
| `../.re-discipline/memory/INDEX.md` | Accepted operational recall and the proposal queue. |
| `../.re-discipline/settings/README.md` | Shared knowledge policy, defaults, and recovery. |

## Current Focus

_(none yet - newly initialized)_

## Active Campaigns

_(none)_

## Contracts

`.claude/CLAUDE.md` and `.codex/AGENTS.md` are thin host-specific manager
adapters. Shared laws live only in `.re-discipline/project-profile.md`. Claude
imports it natively; Codex uses the trusted re-discipline SessionStart hook
plus an explicit-read fallback.
