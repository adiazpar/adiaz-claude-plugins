---
name: init-project
description: >-
  Initialize, adopt, migrate, resync, or recover a re-discipline project.
  Creates the epistemic directory tree, one canonical project profile, Claude
  Code and Codex manager adapters, root role routing, and master indexes while
  preserving existing project guidance.
---

# Initialize A Re-Discipline Project

Build one knowledge system with two native manager entrypoints. Shared laws and
project facts live once in `.re-discipline/project-profile.md`. Claude Code and
Codex each get a thin adapter with preserved project-owned host notes, not
separate laws or a second project profile.

## Locate The Plugin

Resolve the plugin root from this `SKILL.md` path (two parent directories up).
Hook environments may also expose `CLAUDE_PLUGIN_ROOT` or `PLUGIN_ROOT`, but do
not require either variable during an interactive skill run. Templates live in
`<plugin-root>/templates/project/`.

## Target Topology

Every initialized project has:

| Path | Purpose |
|---|---|
| `.re-discipline/project-profile.md` | Canonical shared laws, identity, and domain facts. |
| `.claude/CLAUDE.md` | Claude Code import adapter and project-owned Claude notes. |
| `AGENTS.md` | Root manager/drafter role router. |
| `.codex/AGENTS.md` | Codex hook/fallback adapter and project-owned Codex notes. |
| `.codex/external-drafter-contract.md` | Drafter restrictions and report format. |
| `docs/INDEX.md` | Project front door. |

The `framing` field appears only in the canonical profile. Dispatch tooling
must read it there. Legacy `.claude/project-profile.md` and
`.codex/project-profile.md` files are migration or recovery input only; they
are never steady-state outputs.

## Step 1: Detect The Mode

Inspect `AGENTS.md`, `.codex/`, `.claude/`, `.re-discipline/`, `docs/`, the
repository tree, README files, and relevant git history before asking anything.

- **Initialized:** the canonical profile contains a `shared-laws` block and both
  current manager adapters exist. Stop unless the user requested `resync-laws`
  or repair.
- **Migration:** a legacy `.claude/project-profile.md`, a hand-built
  `.codex/project-profile.md`, duplicated host laws, or old re-discipline
  markers exist without the neutral topology. Follow `references/dropin.md`
  and preserve all meaning.
- **Recovery:** re-discipline law markers or routing exist, but every usable
  identity profile is missing. Follow `references/recovery.md`.
- **Drop-in:** existing project guidance or populated docs exist but no prior
  re-discipline markers. Follow `references/dropin.md`.
- **Greenfield:** no meaningful project guidance exists. Follow
  `references/greenfield.md`.

State the detected mode and the evidence for it.

## Step 2: Preserve One Source Of Shared Policy And Project Facts

The canonical profile owns the shared re-discipline laws plus `name`, `type`,
`framing`, mission, domain, source of record, portable tooling, roles,
artifact/path schema, and environment.

Project-owned sections outside adapter markers own host-specific material:

- `.claude/CLAUDE.md`: Claude settings, MCP names, memory, native delegation
  notes, and `.claude/local-paths.md`.
- `.codex/AGENTS.md`: Codex config or sandbox notes, MCP names, memory or
  memory bridge, shell-recovery notes, and `.codex/local-paths.md`.

Do not copy the Wall, trust map, campaign lifecycle, manager/drafter split,
commit policy, or shared anti-patterns into either host adapter.

When old Claude and Codex profiles repeat a fact, move one reconciled value to
the canonical profile. Move unique host behavior into the matching manager
adapter outside its managed block. Never silently choose between
contradictory copies; show the conflict and ask the user.

Keep the canonical profile concise and optimized for agent context. After
writing it, count lines and UTF-8 bytes. Warn when it exceeds 240 lines or
16 KiB. Never truncate or silently discard content to satisfy either limit.

## Step 3: Protect Existing Instructions

For a missing root `AGENTS.md`, render the router template directly. For an
existing user-owned file, add or update only the block between
`re-discipline:router` markers. Do not replace unrelated instructions.

If root `AGENTS.md` is the old re-discipline external-drafter contract, move its
project-specific tooling and live-surface rules into the new external contract,
then replace the old role with the router.

Apply the same managed-block discipline to `.claude/CLAUDE.md` and
`.codex/AGENTS.md`. A resync updates the canonical profile's marked shared-law
block plus generic adapters and routing; it never overwrites project-owned
profile facts or host notes.

During migration, delete legacy host profiles only after the
meaning-preservation gate confirms that every meaningful instruction survives
in the canonical profile, a project-owned manager section, or its unrelated
original location.

## Step 4: Verify

After any write:

1. Confirm all target topology files exist and neither legacy host profile
   exists as a steady-state output.
2. Confirm `.claude/CLAUDE.md` contains exactly one project-profile import:
   `@../.re-discipline/project-profile.md`.
3. Confirm root `AGENTS.md` routes direct managers to `.codex/AGENTS.md` and
   briefed drafters to `.codex/external-drafter-contract.md`.
4. Confirm `.codex/AGENTS.md` requires the canonical profile and explains the
   trusted SessionStart hook plus explicit-read fallback.
5. Search outside `active/` for duplicate `framing` declarations. Only the
   canonical profile may declare it.
6. Confirm existing unrelated instructions remain byte-for-byte or explain each
   approved edit.
7. Confirm the shared laws appear once in the canonical profile and are absent
   from both host adapters.
8. Report the canonical profile's line count and UTF-8 byte size, warning above
   240 lines or 16 KiB without truncating it.
9. Report remaining placeholders and intentional legacy recovery references.

Do not commit unless the user explicitly asks.

## Resync

When asked to resync, update the marked shared-law block in the canonical
profile plus the managed router, Claude-adapter, and Codex-adapter blocks from
current templates. Show the diff before writing when project-owned content
surrounds a managed block. Leave canonical project facts and all project-owned
host notes untouched. Never recreate a legacy host profile.

## References

- `references/greenfield.md` - gather identity and seed a new project.
- `references/dropin.md` - adopt or migrate existing instructions.
- `references/recovery.md` - restore a missing identity without guessing.
