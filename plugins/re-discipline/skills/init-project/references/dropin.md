# Drop-In And Migration

Adopt an existing project non-destructively. This mode also migrates projects
initialized before the neutral 0.2 profile topology.

## Explore Before Asking

Read root and nested `AGENTS.md` files, `.codex/AGENTS.md`,
`.codex/project-profile.md`, `.claude/CLAUDE.md`,
`.claude/project-profile.md`, docs indexes, README files, dispatch scripts, and
git history. Classify every relevant instruction as:

- **Project fact:** identity, mission, domain, source of record, portable
  tooling, roles, paths, or environment.
- **Generic re-discipline law:** Wall, directory trust, campaign lifecycle, or
  manager/drafter asymmetry.
- **Claude overlay:** Claude settings, tools, memory, or local paths.
- **Codex overlay:** Codex settings, tools, memory, sandbox, or local paths.
- **Unrelated project guidance:** preserve in place.

## Reconcile Duplicated Profiles

Treat neither legacy profile as automatically authoritative. Compare repeated
facts. Matching facts move once into `.re-discipline/project-profile.md`.
Unique host behavior moves into the matching overlay.

When copies conflict:

1. Check current source, config, git history, and durable truth for direct
   evidence.
2. Present both values and the evidence for each.
3. Ask the user to choose or correct the canonical value when evidence cannot.

This step is essential for hand-built pairs such as a Claude profile and a
Codex profile that were maintained separately.

## Present The Planned Split

Before destructive refactoring, show:

- Missing files and directories to add.
- The proposed canonical profile.
- Content moving to each harness overlay.
- The root `AGENTS.md` routing block.
- Any old external-drafter content moving to the dedicated contract.
- Unrelated instructions that will remain unchanged.

Ask only about genuine ambiguity. An explicit user request to apply an already
reviewed reconciliation is sufficient approval to continue.

## Apply Non-Destructively

1. Add missing epistemic directories and indexes; never overwrite a populated
   index without approval.
2. Write `.re-discipline/project-profile.md` with reconciled project facts.
3. Write or update the Claude manager laws and Claude overlay.
4. Write or update the Codex manager laws and Codex overlay.
5. Move the old root drafter role to
   `.codex/external-drafter-contract.md` and install the root router, preserving
   unrelated root guidance outside managed markers.
6. Update dispatchers to read canonical `framing`, retaining the legacy Claude
   path as a temporary fallback.
7. Replace duplicate identity prose in internal agent-loaded docs with links to
   the canonical profile. Human-facing README files may remain self-contained
   when they do not contradict it.

Never normalize provisional `active/` scratch as part of migration.

## Meaning-Preserving Gate

Verify that the union of the canonical profile, harness overlays, manager
contracts, drafter contract, and preserved unrelated guidance contains every
meaningful instruction from the originals. Report moved, deduplicated, and
unchanged sections. Do not commit unless the user asks.
