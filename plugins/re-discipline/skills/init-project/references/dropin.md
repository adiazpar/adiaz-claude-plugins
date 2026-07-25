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
  manager/drafter asymmetry; preserve once in the canonical shared-law block.
- **Claude manager note:** Claude settings, tools, memory, delegation behavior,
  or local paths.
- **Codex manager note:** Codex settings, tools, memory, sandbox, shell
  behavior, or local paths.
- **Unrelated project guidance:** preserve in place.

## Reconcile Duplicated Profiles

Treat neither legacy profile as automatically authoritative. Compare repeated
facts. Matching facts move once into `.re-discipline/project-profile.md`.
Generic laws also move there once. Unique host behavior moves outside the
managed block in the matching manager adapter.

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
- Content moving to the canonical shared-law block or each project-owned host
  section.
- The root `AGENTS.md` routing block.
- Any old external-drafter content moving to the dedicated contract.
- Unrelated instructions that will remain unchanged.

Ask only about genuine ambiguity. An explicit user request to apply an already
reviewed reconciliation is sufficient approval to continue.

## Apply Non-Destructively

1. Add missing epistemic directories and indexes; never overwrite a populated
   index without approval.
2. Write `.re-discipline/project-profile.md` with one shared-law block and
   reconciled project facts.
3. Write or update the Claude managed-adapter block and preserve or append
   project-owned Claude notes outside it.
4. Write or update the Codex managed-adapter block and preserve or append
   project-owned Codex notes outside it.
5. Move the old root drafter role to
   `.codex/external-drafter-contract.md` and install the root router, preserving
   unrelated root guidance outside managed markers.
6. Update dispatchers to read canonical `framing`. Retain legacy host paths
   only in explicitly labeled migration or recovery code.
7. Replace duplicate identity prose in internal agent-loaded docs with links to
   the canonical profile. Human-facing README files may remain self-contained
   when they do not contradict it.
8. Compare the post-migration union against both legacy profiles. Delete
   `.claude/project-profile.md` and `.codex/project-profile.md` only after every
   meaningful instruction has a surviving destination.
9. Count canonical-profile lines and UTF-8 bytes. Warn above 240 lines or
   16 KiB; never truncate it.

Never normalize provisional `active/` scratch as part of migration.

## Meaning-Preserving Gate

Verify that the union of the canonical profile, project-owned manager notes,
managed adapters, drafter contract, and preserved unrelated guidance contains
every meaningful instruction from the originals. Report moved, deduplicated,
and unchanged sections. Do not commit unless the user asks.
