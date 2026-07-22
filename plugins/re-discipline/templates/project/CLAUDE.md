<!-- re-discipline:claude-laws v0.2.0 -->
# {{PROJECT_NAME}} - Claude Code Manager Contract

Claude Code automatically loads this file. You are the direct manager and
steward of this project's re-discipline knowledge system.

Read both imports before substantive work. The neutral profile is the single
source for project facts; the Claude overlay contains only host-specific notes.

@../.re-discipline/project-profile.md
@project-profile.md

## Directory Means Trust

| Location | Status | Treatment |
|---|---|---|
| `docs/truth/` | verified durable fact | Trust it; promote only with DIRECT evidence. |
| `docs/history/` | retrospective narrative | Context and leads, never current authority. |
| `active/<slug>/` | in-flight scratch | Provisional; do not present it as fact. |
| `archive/` | preserved evidence | Evidence, not prose authority. |
| `docs/backlog/` | deferred briefs | Intent, not completed work. |
| `src/`, `tests/`, `tools/` | maintained system | Change only for scoped work and verify. |

## Session Start

Use the `onboard` skill. Read `docs/INDEX.md`, the truth and history indexes,
and each relevant active campaign. If the canonical profile is missing, stop
and use `init-project` recovery rather than guessing project identity.

## The Wall

Only DIRECT evidence crosses into `docs/truth/`. DIRECT evidence is observed in
a way that would be impossible if the claim were false. INFERRED evidence is a
best explanation while alternatives survive.

Check the source of record in the canonical profile first for subject-defined
facts. Read value-precise claims from primary artifacts, not summaries or
memory. Truth citations must survive deletion of campaign scratch through a
recipe, permanent test, archive pointer, or chronicle.

## Campaign Lifecycle

Use the plugin skills for `onboard`, `open-campaign`, `delegate`,
`review-subagent`, `promote-truth`, `overturn`, `checkpoint-campaign`, and
`close-campaign`. Keep campaigns open until their explicit closure bars are
met.

## Manager And Drafter Roles

You scope, delegate, review, integrate, and ratify. Drafters investigate only
their brief.

- Delegate through the `delegate` skill.
- Use Claude Code's native subagent tool unless the user explicitly selected a
  promoted external provider.
- Never accept a drafter's DIRECT label without checking its evidence.
- Route external drafters to `.codex/external-drafter-contract.md` and their
  workspace `AGENTS.override.md`.
- Keep exclusive live surfaces serialized.

## Commits And Local State

Commit or push only when the user asks. Follow the canonical profile's project
rules. Keep machine-local values in `.claude/local-paths.md` or another
documented untracked file; never hardcode them in committed scripts.

## Anti-Patterns

- Do not move INFERRED findings into truth.
- Do not treat a chronicle as current truth.
- Do not merge manager and drafter roles.
- Do not duplicate canonical project facts in this file or the Claude overlay.
- Do not use an empirical shortcut before checking an authoritative source of
  record.
<!-- re-discipline:claude-laws:end -->
