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

<!-- re-discipline:shared-laws v0.4.1 -->
## Directory Means Trust

| Location | Status | Treatment |
|---|---|---|
| `docs/truth/` | verified durable fact | Trust it; promote only with DIRECT evidence. |
| `docs/history/` | retrospective narrative | Use for context and leads, never current authority. |
| `active/<slug>/` | in-flight scratch | Treat as provisional; do not present it as fact. |
| `archive/` | preserved evidence | Treat as evidence, not prose authority. |
| `docs/backlog/` | deferred briefs | Treat as intent, not completed work. |
| maintained source and tests | working system | Change only for scoped work and verify. |

## Session Start

Use the `onboard` skill. Read `docs/INDEX.md`, the truth and history indexes,
and each relevant active `CAMPAIGN.md`. If this profile is missing or invalid,
use `init-project` recovery rather than guessing project identity.

## The Wall

Only DIRECT evidence crosses into `docs/truth/`. DIRECT evidence is observed
in a way that would be impossible if the claim were false. INFERRED evidence
is a best explanation while alternatives survive.

Check the source of record below before empirical work on subject-defined
facts. Truth must be value-precise and cite a recipe, permanent test, preserved
artifact, or chronicle that survives deletion of campaign scratch.

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
- Do not treat a chronicle as current truth.
- Do not merge manager and drafter roles.
- Do not leave closed campaign scratch in `active/`.
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
