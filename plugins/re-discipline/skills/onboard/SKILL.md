---
name: onboard
description: >-
  Orient a re-discipline project at session start or when asked to get caught
  up, show current project state, or resume an active campaign. Reads the
  canonical profile, active manager adapter, truth and history indexes, the
  names of open campaigns, knowledge health, shared-memory status, and
  normalized external-provider state.
---

# Onboard A Re-Discipline Project

Onboarding is read-only. Directory location encodes trust:

| Location | Status |
|---|---|
| `docs/truth/` | Current claims with durable verification. |
| `docs/history/` | Retrospective provenance and leads, never current authority or sole empirical support. |
| `active/<slug>/` | Provisional work and temporary evidence. |
| `docs/backlog/` | Deferred intent, not completed work. |
| `.re-discipline/memory/topics/` | Accepted operational recall, never empirical authority. |
| `.re-discipline/memory/proposals/` | Pending proposals, excluded from ordinary retrieval. |
| maintained source, tools, tests, fixtures, corpora, and references | Durable project assets with an active consumer or owner. |

## Step 1: Detect The Active Host

Determine the active host from the tools exposed by the session and the
instructions that loaded. Do not infer it merely because `.claude/` or
`.codex/` exists; a compatible project can contain both.

- **Claude Code:** read `.claude/CLAUDE.md`.
- **Codex:** read `.codex/AGENTS.md`.
- **Unknown host:** read available manager adapters, identify the applicable
  tool surface, and state the uncertainty instead of silently choosing one.

Always read `.re-discipline/project-profile.md` first. It is the single source
for shared laws, name, framing, mission, source of record, portable tooling,
and domain facts.

If the canonical profile is missing, stop substantive work and invoke
`init-project` in migration or recovery mode. A legacy host profile is
recovery input, not permission to invent a replacement.

Read the small `.re-discipline/config.json` bootstrap. The knowledge
system's machine-managed state lives under `.re-discipline/knowledge/`:
`policy.jsonc` is AI-curated policy the manager edits on user request, and
`retrieval-profile.json` is a generated accepted profile. Neither is a
user-facing settings surface. Report malformed managed files through the
status user block instead of guessing replacements.

## Step 2: Read The Masterfiles

Read:

1. `docs/INDEX.md` for mission, current focus, and navigation.
2. `docs/truth/INDEX.md` for what is known now.
3. `docs/history/INDEX.md` for what has already been explored.

Report missing or dangling index links. Do not manufacture content during
onboarding.

## Step 3: Read Active State

List directories directly under `active/`. That listing is the authoritative
set of open campaigns, and at session start it is the whole of what onboarding
needs from `active/`.

**Do not read campaign masterfiles during onboarding.** Take each campaign's
one-line description from `docs/INDEX.md`, already read in Step 2. When the
index does not describe a listed campaign, print the slug alone and report the
index as out of date. Onboarding orients; it does not preload the working
context of campaigns the session has not asked for. A project can carry many
open campaigns, and their masterfiles are long, provisional, and mostly
irrelevant to any one session.

Read a campaign's `CAMPAIGN.md` — Objective, Current state, Open questions,
Dead ends, the disposition manifest — and its `REVIEWS.md` when the user
directs the session into that campaign. That is the cold-resume read, and a
cold resume is always into a named campaign.

Status reports a `campaigns` block. When a campaign is marked `stale`, its
masterfile is older than the newest file under its own directory by more than
the reported margin: work continued and the cold-resume surface did not.
Report that alongside the campaign and recommend `checkpoint-campaign`. Do not
treat a stale masterfile as current state and do not silently rewrite it during
onboarding.

Treat names below each campaign's `subagents/` directory as opaque workspace
keys. New chronological IDs and legacy task-only or provider-prefixed names
remain valid history. Onboarding never parses, normalizes, or renames them.

When no campaign is active, call the state a cold start and await direction.
Do not open a campaign merely because none exists.

## Step 4: Read Knowledge And Memory State Silently

Call the knowledge server's read-only `status` operation when available.
The response has two blocks. The `system` block is yours: read index
health, corpus generation, requested and effective profiles, active lanes,
fallback reason, benchmark and evidence-pin state, and campaign staleness,
and use them to plan the session. When the last session's corpus
generation is known - from a checkpoint, a campaign masterfile, or a
session note - pass it to `orient` as `sinceGeneration` and use the
document-level delta to orient yourself; an unavailable delta means the
recorded history no longer reaches that generation, not that nothing
changed.

None of that machinery state is narrated to the user
(`<plugin-root>/references/reporting.md`). The `user` block is the only
knowledge/memory text you print: its `knowledge` and `memory` lines go on
the dashboard verbatim, and each `attention` item becomes a line under
"Needs your attention". Benchmark staleness and evidence-pin drift are
never onboarding news; the measurement skills handle them at their own
gate time.

Campaign masterfile staleness from the `system` block is user-relevant:
report a stale campaign in its own campaign line and recommend
`checkpoint-campaign`.

List proposal filenames only to sanity-check the reported count; never
read proposal content during ordinary onboarding. Pending proposals are
not accepted project knowledge.

Run only the cheap health check. Do not build vectors, download models,
run a full benchmark, calibrate weights, accept memory, or activate a
profile during onboarding. If status is unavailable, print "Knowledge
search: unavailable" and continue from canonical source files rather than
inventing knowledge health.

## Step 5: Read External-Provider State

Read `.re-discipline/agents/config.json`. Its provider keys are the complete
set of live external providers, and `backend` is the live route selected by the
user. `native` means the active host's native adapter.

List candidate directories directly under
`.re-discipline/agents/recruiting/`, if any. Do not treat candidates as live
providers. If the normalized agent core is missing or the JSON is invalid,
report that `init-project` resync or repair is required.

## Step 6: Return One Screen

Print exactly this shape and nothing more
(`<plugin-root>/references/reporting.md`):

```user-facing
## Onboarded - <project name>

Mission: <one sentence>
Current focus: <one or two lines>

Active campaigns:
- <slug>: <one-line description from docs/INDEX.md, or "no index entry"><; masterfile stale: recommend checkpoint>
- none (cold start)

Knowledge search: <user.knowledge>
Memory: <user.memory>
Delegation: <native|provider>
External providers: <names|none> | Recruiting: <candidates|none>

Needs your attention:
- <each user.attention item, verbatim>

Recent history: <latest chronicle and outcome>
Ready to: <resume named campaign, open a requested campaign, or await direction>
```

Omit the "Needs your attention" section when there are no attention items.
Every other line is always present. The knowledge and memory lines are the
status `user` block verbatim; print nothing else about knowledge or memory
internals.

Campaign lines carry no open-question count and no per-campaign status text
beyond the index description, because onboarding does not open masterfiles.
Do not read one to enrich the screen.

Stop after orientation. Do not begin substantive investigation until the user
directs it.

## Reference

- Host mapping: `<plugin-root>/references/runtime-adapters.md`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Canonical laws and project facts: `.re-discipline/project-profile.md`.
