---
name: onboard
description: >-
  Orient a re-discipline project at session start or when asked to get caught
  up, show current project state, or resume an active campaign. Reads the
  canonical profile, active manager adapter, truth and history indexes, active
  campaign masterfiles, knowledge health, shared-memory status, and normalized
  external-provider state.
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

Read the small `.re-discipline/config.json` bootstrap and the documented
project-facing settings under `.re-discipline/settings/`. Treat
`knowledge.jsonc` as human-editable policy and `retrieval-profile.json` as a
generated accepted profile. Report malformed or missing managed settings
instead of guessing replacements.

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
questions, Dead ends, and the disposition manifest. Read
`active/<slug>/REVIEWS.md` when it exists, for what has already been checked
and which holds are still unresolved.

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

## Step 4: Read Knowledge Status

Call the knowledge server's read-only `status` operation when available.
Report:

- index health and corpus generation;
- requested and effective retrieval profiles;
- active retrieval lanes, local model identities, and fallback reason;
- settings and source-policy validity;
- latest benchmark age, and its `actionableStaleReasons` when
  `benchmark.staleActionable` is true;
- evidence-pin health, but only when `pins.drifted` or `pins.broken` is above
  zero;
- masterfile staleness for any campaign the `campaigns` block marks `stale`;
- pending memory-proposal count.

Evidence pins tie an evaluation case to what its cited documents claim.
`broken` means a pinned document is gone or now asserts something else, so the
case's ground truth may no longer hold and re-answering it is the repair;
`drifted` means only the file's bytes moved and is advisory. Report a single
line when either is non-zero and stay silent when the census is clean - a
report that always mentions pins teaches sessions to skip the line that
matters.

Benchmark staleness carries two severities and they are not the same news.
`staleActionable` means the models, the runtime contract, or the chunker moved
under the measurement, so what retrieval actually does may have changed and a
re-run is warranted. `staleInformational` means only the corpus or the
evaluation set drifted, which is ordinary in a living project and warrants no
action. Give actionable staleness its own line. Mention informational drift as
a parenthetical at most, and never recommend a benchmark because of it.

List proposal filenames to count them, but do not read their content during
ordinary onboarding. Pending proposals are not accepted project knowledge.

Run only the cheap health/freshness check. Do not build vectors, download
models, run a full benchmark, calibrate weights, accept memory, or activate a
profile during onboarding. If status is unavailable, state that fact and
continue from canonical source files rather than inventing knowledge health.

## Step 5: Read External-Provider State

Read `.re-discipline/agents/config.json`. Its provider keys are the complete
set of live external providers, and `backend` is the live route selected by the
user. `native` means the active host's native adapter.

List candidate directories directly under
`.re-discipline/agents/recruiting/`, if any. Do not treat candidates as live
providers. If the normalized agent core is missing or the JSON is invalid,
report that `init-project` resync or repair is required.

## Step 6: Return One Screen

Use this shape:

```text
## Onboarded - <project name>

Host: <Claude Code|Codex|other>
Mission: <one sentence>
Current focus: <one or two lines>

Active campaigns:
- <slug>: <status>; open questions: <count><; masterfile stale: <n>d behind>
- none (cold start)

Knowledge: <healthy|degraded|unavailable>; generation: <id|none>
Benchmark: <age> days old<; re-run warranted: reason,reason | (corpus drift only)>
Evidence pins: <n broken, n drifted of n>
Retrieval: requested <profile>; effective <profile>; fallback: <reason|none>
Pending memory proposals: <count>
Delegation backend: <native|provider>
External providers: <configured names|none>
Recruiting candidates: <candidate names|none>
Recent history: <latest chronicle and outcome>
Ready to: <resume named campaign, open a requested campaign, or await direction>
```

Omit the `Evidence pins` line when the census is clean. Every other line is
always present.

Stop after orientation. Do not begin substantive investigation until the user
directs it.

## Reference

- Host mapping: `<plugin-root>/references/runtime-adapters.md`.
- Knowledge governance: `<plugin-root>/references/knowledge-governance.md`.
- Canonical laws and project facts: `.re-discipline/project-profile.md`.
