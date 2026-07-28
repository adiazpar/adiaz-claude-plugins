---
# Machine-readable project identity. Dispatchers read `framing` from here.
# This is the single source for project facts and laws; adapters must not repeat it.
name: "{{PROJECT_NAME}}"
type: "{{PROJECT_TYPE}}"
framing: "{{ONE_LINE_FRAMING}}"
---

# {{PROJECT_NAME}} - Canonical Project Profile

This is the single source of project identity, domain facts, and shared
re-discipline laws. Runtime-specific bootstrap and configuration belong in
thin manager adapters, not in a second project profile.

<!-- re-discipline:shared-laws v0.7.0 -->
## Directory Means Trust

| Location | Status | Treatment |
|---|---|---|
| `docs/truth/` | verified current claim | Trust it only within its scope and re-verification conditions. |
| `docs/history/` | retrospective provenance | Use for context and leads, never current authority or sole empirical support. |
| `active/<slug>/` | provisional work and temporary evidence | Keep unresolved material here; do not present it as fact. |
| `active/<slug>/subagents/*/report.md` | a drafter claim | Stamped by a manager, it is a rederived finding and still not empirical support. Unstamped, nobody has checked it, and it is retrievable only when asked for by name. |
| `docs/backlog/` | deferred briefs | Treat as intent, not completed work, and as the destination for a HOLD that outlives its campaign. |
| maintained source, tools, tests, fixtures, corpora, and references | durable project assets | Keep only with an active consumer or owner; change only for scoped work and verify. |

## Session Start

Use the `onboard` skill. Read `docs/INDEX.md`, the truth and history indexes,
and each relevant active `CAMPAIGN.md`. If this profile is missing or invalid,
use `init-project` recovery rather than guessing project identity.

Project knowledge and recall are governed by `.re-discipline/config.json`.
Accepted shared memory is recall, not empirical evidence. Pending memory
proposals never enter normal retrieval before manager review and user
ratification.

## The Wall

Only DIRECT evidence crosses into `docs/truth/`. DIRECT evidence is observed
in a way that would be impossible if the claim were false. INFERRED evidence
is a best explanation while alternatives survive.

Check the source of record below before empirical work on subject-defined
facts. Truth admission has two gates: DIRECT evidence must establish the claim,
and a maintained source, permanent test or fixture, or runnable recipe must let
a future manager recheck it. A chronicle records provenance and cannot be the
claim's sole empirical support.

## Campaign Lifecycle

Use the lifecycle skills: `onboard`, `open-campaign`, `delegate`,
`review-subagent`, `promote-truth`, `overturn`, `checkpoint-campaign`, and
`close-campaign`. Keep campaigns open until their explicit closure bars are
met.

## Manager And Drafter Roles

The manager scopes, delegates, reviews, integrates, and ratifies. Subagents and
external agents are drafters: they investigate only their brief and never
promote truth or close campaigns. Delegate through the `delegate` skill, check
every claimed DIRECT fact against its evidence, route drafters to the external
drafter contract, and serialize exclusive live surfaces.

## Commits And Local State

Commit or push only when the user asks. Follow the project rules below. Keep
machine-local values in `.re-discipline/local-paths.md`, keep that file
untracked, and never hardcode its values in committed scripts. Treat the
values as private machine state; do not copy them into manager context,
dispatch briefs, reports, or memory.

## Anti-Patterns

- Do not move INFERRED findings into truth.
- Do not blindly accept a drafter report.
- Do not treat a chronicle as current truth or sole empirical support.
- Do not merge manager and drafter roles.
- Do not leave closed campaign scratch in `active/`.
- Do not preserve raw output without an active consumer or owner.
- At closure, Maintain durable project assets, Distill necessary meaning, and
  Delete the remainder.
- Do not use an empirical shortcut before checking an authoritative source.
- Do not duplicate this profile's facts or laws in a manager adapter.
<!-- re-discipline:shared-laws:end -->

## Mission

{{MISSION}}

## Domain

{{DOMAIN_DESCRIPTION}}

## Source Of Record

{{SOURCE_OF_RECORD}}

<!-- Name authoritative schemas, declarations, specifications, or primary
     artifacts and state exactly what each can prove. Use "none identified"
     when the subject defines no authoritative data set. -->

## Tooling

{{TOOLING}}

<!-- Record portable tool locations and sanctioned invocations. Runtime-specific
     MCP names or permission settings belong in the manager adapters. -->

## Paths And Artifacts

{{BINARIES_AND_PATHS}}

<!-- Record path schema, tracked artifact locations, and re-verification
     triggers. Machine-local values stay in the untracked
     `.re-discipline/local-paths.md`. -->

## Environment

{{ENVIRONMENT}}

<!-- Shell, test, build, and commit mechanics that are true for the project,
     independent of which manager runtime is active. -->

## Wall Example

{{WALL_EXAMPLE}}

<!-- Optional concrete example showing what two evidence sources each attest. -->
