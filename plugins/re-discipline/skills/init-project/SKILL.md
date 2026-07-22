---
name: init-project
description: >-
  Initialize, adopt, migrate, resync, or recover a re-discipline project.
  Creates the epistemic directory tree, one canonical project profile, Claude
  Code and Codex manager adapters, root role routing, and master indexes while
  preserving existing project guidance.
---

# Initialize A Re-Discipline Project

Build one knowledge system with two native manager entrypoints. Project facts
live once in `.re-discipline/project-profile.md`. Claude Code and Codex each get
an operating overlay, not a second copy of the project's identity.

## Locate The Plugin

Resolve the plugin root from this `SKILL.md` path (two parent directories up).
Hook environments may also expose `CLAUDE_PLUGIN_ROOT` or `PLUGIN_ROOT`, but do
not require either variable during an interactive skill run. Templates live in
`<plugin-root>/templates/project/`.

## Target Topology

Every initialized project has:

| Path | Purpose |
|---|---|
| `.re-discipline/project-profile.md` | Canonical identity and domain facts. |
| `.claude/CLAUDE.md` | Claude Code manager laws. |
| `.claude/project-profile.md` | Claude-only operating overlay. |
| `AGENTS.md` | Root manager/drafter role router. |
| `.codex/AGENTS.md` | Codex manager laws. |
| `.codex/project-profile.md` | Codex-only operating overlay. |
| `.codex/external-drafter-contract.md` | Drafter restrictions and report format. |
| `docs/INDEX.md` | Project front door. |

The `framing` field appears only in the canonical profile. Dispatch tooling
must read it there, with `.claude/project-profile.md` as a legacy fallback.

## Step 1: Detect The Mode

Inspect `AGENTS.md`, `.codex/`, `.claude/`, `.re-discipline/`, `docs/`, the
repository tree, README files, and relevant git history before asking anything.

- **Initialized:** the canonical profile and both manager contracts exist.
  Stop unless the user requested `resync-laws` or repair.
- **Migration:** a legacy `.claude/project-profile.md`, a hand-built
  `.codex/project-profile.md`, or old re-discipline laws exist without the
  neutral topology. Follow `references/dropin.md` and preserve all meaning.
- **Recovery:** re-discipline law markers or routing exist, but every usable
  identity profile is missing. Follow `references/recovery.md`.
- **Drop-in:** existing project guidance or populated docs exist but no prior
  re-discipline markers. Follow `references/dropin.md`.
- **Greenfield:** no meaningful project guidance exists. Follow
  `references/greenfield.md`.

State the detected mode and the evidence for it.

## Step 2: Preserve One Source Of Project Facts

The canonical profile owns `name`, `type`, `framing`, mission, domain, source
of record, portable tooling, roles, artifact/path schema, and environment.

Harness overlays own only host-specific material:

- Claude settings, Claude MCP names, Claude memory, and `.claude/local-paths.md`.
- Codex config/sandbox notes, Codex MCP names, Codex memory or memory bridge,
  and `.codex/local-paths.md`.

When old Claude and Codex profiles repeat a fact, move one reconciled value to
the canonical profile and replace both copies with a pointer. Never silently
choose between contradictory copies; show the conflict and ask the user.

## Step 3: Protect Existing Instructions

For a missing root `AGENTS.md`, render the router template directly. For an
existing user-owned file, add or update only the block between
`re-discipline:router` markers. Do not replace unrelated instructions.

If root `AGENTS.md` is the old re-discipline external-drafter contract, move its
project-specific tooling and live-surface rules into the new external contract,
then replace the old role with the router.

Apply the same managed-block discipline to `.claude/CLAUDE.md` and
`.codex/AGENTS.md`. A resync changes generic laws and routing only; it never
overwrites the canonical profile or either overlay.

## Step 4: Verify

After any write:

1. Confirm all target topology files exist.
2. Confirm `.claude/CLAUDE.md` imports
   `@../.re-discipline/project-profile.md` and `@project-profile.md`.
3. Confirm root `AGENTS.md` routes direct managers to `.codex/AGENTS.md` and
   briefed drafters to `.codex/external-drafter-contract.md`.
4. Confirm `.codex/AGENTS.md` requires both the canonical profile and Codex
   overlay.
5. Search outside `active/` for duplicate `framing` declarations. Only the
   canonical profile may declare it.
6. Confirm existing unrelated instructions remain byte-for-byte or explain each
   approved edit.
7. Report remaining placeholders and legacy fallbacks.

Do not commit unless the user explicitly asks.

## Resync Laws

When asked to resync, update the managed router, Claude-law, and Codex-law
blocks from current templates. Show the diff before writing when existing
project-owned content surrounds a managed block. Leave all three profiles
untouched.

## References

- `references/greenfield.md` - gather identity and seed a new project.
- `references/dropin.md` - adopt or migrate existing instructions.
- `references/recovery.md` - restore a missing identity without guessing.
