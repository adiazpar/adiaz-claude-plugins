---
name: init-project
description: >-
  Initialize, adopt, migrate, resync, or recover a re-discipline project.
  Creates the epistemic directory tree, one canonical project profile, manager
  adapters, the external-agent core, root role routing, and master indexes
  while preserving existing project guidance.
---

# Initialize A Re-Discipline Project

Build one host-neutral knowledge system. Shared laws and project facts live
once in `.re-discipline/project-profile.md`. Each supported manager host gets a
thin adapter with preserved project-owned host notes, never separate laws or a
second project profile.

## Locate The Plugin

Resolve the plugin root from this `SKILL.md` path (two parent directories up).
Hook environments may expose `CLAUDE_PLUGIN_ROOT` or `PLUGIN_ROOT`, but an
interactive skill run must not require either variable. Templates live in
`<plugin-root>/templates/project/`.

## Target Topology

Every initialized project has:

| Path | Purpose |
|---|---|
| `.re-discipline/project-profile.md` | Canonical shared laws, identity, and domain facts. |
| `.re-discipline/local-paths.md` | Untracked machine-local path values shared by every manager host. |
| `.re-discipline/agents/README.md` | External-provider schema and lifecycle. |
| `.re-discipline/agents/config.json` | Only live provider roster and backend switch. |
| `.re-discipline/agents/dispatch.ps1` | Host-neutral external-provider dispatcher. |
| `.re-discipline/agents/providers/` | Three-file durable records for configured providers. |
| `.re-discipline/agents/recruiting/` | Transient candidate workspaces. |
| `.claude/CLAUDE.md` | Claude Code import adapter and project-owned Claude notes. |
| `AGENTS.md` | Root manager/drafter role router. |
| `.codex/AGENTS.md` | Codex hook/fallback adapter and project-owned Codex notes. |
| `.codex/external-drafter-contract.md` | Drafter restrictions and report format. |
| `docs/INDEX.md` | Project front door. |

Always create the external-agent core, even when no external provider is
configured. Its initial config is exactly `backend: native` with an empty
`providers` object. Empty `providers/` and `recruiting/` directories are
intentional local structure; do not invent placeholder state to track them.

The `framing` field appears only in the canonical profile. Legacy
`.claude/project-profile.md`, `.codex/project-profile.md`,
`.claude/local-paths.md`, and `.codex/local-paths.md` files are migration or
recovery input only, never steady-state output.

## Step 1: Detect The Mode

Inspect `AGENTS.md`, `.codex/`, `.claude/`, `.re-discipline/`, `.gitignore`,
`docs/`, the repository tree, README files, dispatch scripts, and relevant git
history before asking anything.

Evaluate migration and recovery gates before Initialized.
Legacy archive semantics always override Initialized. Apply this rule even when
normalized topology already exists.

- **Initialized:** the canonical profile, `.re-discipline/local-paths.md`,
  both current manager adapters, and normalized agent core exist; the neutral
  local-path file is ignored and untracked; and no legacy host profile or
  host-local path file remains; and no unresolved legacy archive semantics
  remain. Stop unless the user requested resync or repair.
- **Migration:** legacy archive semantics, legacy profiles, duplicated host
  laws, old agent paths, old host-local path files, or a missing neutral
  local-path signature or ignore rule exists. Old re-discipline markers without
  normalized topology also qualify. Follow `references/dropin.md` and preserve
  meaning.
- **Recovery:** re-discipline law markers or routing exist, but every usable
  identity profile is missing. Follow `references/recovery.md`.
- **Drop-in:** existing project guidance or populated docs exist but no prior
  re-discipline markers. Follow `references/dropin.md`.
- **Greenfield:** no meaningful project guidance exists. Follow
  `references/greenfield.md`.

State the detected mode and evidence.

## Legacy Archive Semantic Migration

Treat ownership as semantic, not lexical. The existence of a directory named
`archive` alone does not prove re-discipline ownership; it may contain
project-owned release bundles or domain data. Classify it as a legacy
re-discipline archive only when managed shared laws, campaign templates, truth
files, or chronicles describe that path as preserved evidence.

When legacy archive semantics are present:

1. Inventory every reference and active consumer before writing.
2. Report that semantic migration is required.
3. Do not delete, move, or rewrite any file automatically.
4. Preserve the current shared-law block until the user approves migration.
5. After approval, classify every affected artifact as Maintain, Distill, or
   Delete and verify each destination or recorded rationale.

Create no durable root-level replacement evidence directory. Temporary
campaign evidence under `active/<slug>/evidence/` remains valid until
checkpoint or closure. A project-owned directory named `archive` remains
project-owned and unchanged.

## Step 2: Preserve One Source Of Shared Policy And Project Facts

The canonical profile owns the shared re-discipline laws plus `name`, `type`,
`framing`, mission, domain, source of record, portable tooling, artifact/path
schema, and environment.

Project-owned sections outside adapter markers own host-specific material:

- `.claude/CLAUDE.md`: Claude settings, MCP names, memory, native delegation
  notes, and Claude-only runtime behavior.
- `.codex/AGENTS.md`: Codex config or sandbox notes, MCP names, memory or
  memory bridge, shell-recovery notes, and Codex-only runtime behavior.

All managers and maintained project tools share one machine-local value file:
`.re-discipline/local-paths.md`. Keep variable names and portable contracts in
the canonical profile or durable project documentation; keep only local values
and local-only explanatory notes in this untracked file. Never create a
host-specific local-path file in steady state.

Before writing machine-local values, add `.re-discipline/local-paths.md` to
the root `.gitignore` without replacing unrelated ignore rules. Also keep
`.claude/local-paths.md` and `.codex/local-paths.md` as defense-only ignore
patterns so older tooling cannot stage private values there; the legacy files
must still be absent in steady state. Render `templates/project/local-paths.md`,
substituting discovered assignments. When no values are known yet, replace the
assignment placeholder with a commented instruction rather than leaving
template syntax in the project.

Do not copy the Wall, trust map, campaign lifecycle, manager/drafter split,
commit policy, or shared anti-patterns into a host adapter. Do not add
descriptive staffing personas; put required tools, paths, constraints,
deliverables, evidence standards, budgets, and exclusive live surfaces
directly into each dispatch brief.

When old profiles repeat a fact, move one reconciled value to the canonical
profile. Move unique host behavior into the matching manager adapter outside
its managed block. Never silently choose between contradictory copies; show
the conflict and ask the user.

Treat legacy local-path files the same way, but compare assignment names
without echoing their values into reports or manager context. Deduplicate
matching assignments, preserve unique assignments, and resolve conflicting
assignments from current source/config evidence or ask the user. Move
non-path, host-specific operational notes into the applicable manager adapter.
Delete the legacy files only after the neutral file preserves every required
assignment and note.

Keep the canonical profile concise and optimized for agent context. After
writing it, count lines and UTF-8 bytes. Warn above 240 lines or 16 KiB. Never
truncate or silently discard content to satisfy either limit.

## Step 3: Protect Existing Instructions

For a missing root `AGENTS.md`, render the router template directly. For an
existing user-owned file, add or update only the block between
`re-discipline:router` markers. Do not replace unrelated instructions.

If root `AGENTS.md` is the old external-drafter contract, move its
project-specific tooling and live-surface rules into the dedicated contract,
then replace the old role with the router.

Apply the same managed-block discipline to `.claude/CLAUDE.md` and
`.codex/AGENTS.md`. A resync updates the canonical profile's marked shared-law
block, generic adapters, routing, and normalized agent templates. It never
overwrites project-owned facts or host notes.

Delete legacy host profiles or old agent paths only after a
meaning-preservation gate confirms that every meaningful instruction or
configured provider has a surviving normalized destination.

Delete legacy host-local path files only after their reconciled values exist
in `.re-discipline/local-paths.md`, maintained readers point to the neutral
path, and the neutral file is ignored. If a legacy file is tracked, remove it
from current tracking; do not rewrite Git history unless the user separately
requests that destructive operation.

## Step 4: Verify

After any write:

1. Confirm every target topology file and directory exists.
2. Confirm `.re-discipline/local-paths.md` exists, is ignored, is not tracked,
   no legacy host-local path file remains, and both legacy locations remain
   ignored as defense-only secret paths.
3. Parse `.re-discipline/agents/config.json`; require exactly the documented
   schema and a backend that is `native` or a configured provider.
4. Confirm each configured provider directory contains exactly `profile.md`,
   `scorecard.md`, and `teardown.md`.
5. Confirm `.claude/CLAUDE.md` contains exactly one project-profile import:
   `@../.re-discipline/project-profile.md`.
6. Confirm root `AGENTS.md` routes direct managers to `.codex/AGENTS.md` and
   briefed drafters to `.codex/external-drafter-contract.md`.
7. Confirm `.codex/AGENTS.md` requires the canonical profile and explains the
   trusted SessionStart hook plus explicit-read fallback.
8. Search outside `active/` for duplicate `framing` declarations. Only the
   canonical profile may declare it.
9. Confirm unrelated instructions remain byte-for-byte or explain each
   approved edit.
10. Confirm shared laws appear once in the canonical profile and are absent
   from host adapters.
11. Confirm maintained code and current documentation do not name
    `.claude/local-paths.md` or `.codex/local-paths.md`.
12. Confirm greenfield and fully migrated topology does not create or require a
    durable root-level replacement evidence directory. Temporary campaign
    evidence under `active/<slug>/evidence/` remains valid. Report every
    unresolved legacy archive dependency instead of declaring migration
    complete.
13. Report canonical profile line and byte counts, remaining placeholders,
    and intentional migration or recovery references.

Do not commit unless the user explicitly asks.

## Resync

When asked to resync, update marked shared-law, router, and manager-adapter
blocks from current templates. Replace the three normalized agent core files
only after preserving configured provider entries and project-owned additions.
Leave canonical project facts, provider records, candidates, and project-owned
host notes untouched. Never recreate legacy paths. When unresolved legacy
archive dependencies remain, preserve the current shared-law block, report the
semantic migration as incomplete, and do not claim a successful resync.

## References

- `references/greenfield.md` - gather identity and seed a new project.
- `references/dropin.md` - adopt or migrate existing instructions.
- `references/recovery.md` - restore a missing identity without guessing.
