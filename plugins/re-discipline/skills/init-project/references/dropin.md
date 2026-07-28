# Drop-In And Migration

Adopt an existing project non-destructively. This mode also migrates projects
initialized before the neutral 0.2 profile topology.

## Explore Before Asking

Read root and nested `AGENTS.md` files, `.codex/AGENTS.md`,
`.codex/project-profile.md`, `.claude/CLAUDE.md`,
`.claude/project-profile.md`, `.codex/local-paths.md`,
`.claude/local-paths.md`, project `.codex/config.toml`, project
`.claude/settings.json`, `.re-discipline/config.json`,
`.re-discipline/knowledge/`, `.gitignore`, docs indexes, README files,
dispatch scripts, campaign templates, truth files, chronicles, and git
history. Search those sources for legacy archive semantics before treating any
filesystem path as re-discipline state. Classify every relevant instruction as:

- **Project fact:** identity, mission, domain, source of record, portable
  tooling, paths, or environment.
- **Generic re-discipline law:** Wall, directory trust, campaign lifecycle,
  legacy archive evidence semantics, or manager/drafter asymmetry; preserve or
  migrate once in the canonical shared-law block.
- **Claude manager note:** Claude settings, tools, memory, or delegation
  behavior.
- **Codex manager note:** Codex settings, tools, memory, sandbox, shell
  behavior, or recovery policy.
- **Machine-local value:** a host-neutral assignment for the untracked
  `.re-discipline/local-paths.md`.
- **Unrelated project guidance:** preserve in place.

Do not inspect or migrate machine-local Claude or Codex memory directories.
Existing native-memory content is outside initialization scope even when the
target project selects `shared-only`.

## Gate Legacy Archive Semantics

The existence of a directory named `archive` alone does not prove
re-discipline ownership. A project may use that name for releases or domain
data. Treat a path as a legacy re-discipline archive only when managed shared
laws, campaign templates, truth files, or chronicles describe it as preserved
evidence.

When that semantic evidence exists:

1. Inventory every reference and active consumer.
2. Report that semantic migration is required.
3. Do not delete, move, or rewrite any file automatically.
4. Preserve the current shared-law block until the user approves migration.
5. After approval, classify each affected artifact as Maintain, Distill, or
   Delete. Verify maintained consumers, distilled destinations, and deletion
   rationale before changing the legacy directory.

Create no durable root-level replacement evidence directory. Temporary
campaign evidence under `active/<slug>/evidence/` remains valid until
checkpoint or closure. Leave a project-owned directory named `archive`
unchanged.

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

## Reconcile Legacy Local Paths

Treat `.claude/local-paths.md` and `.codex/local-paths.md` as private migration
inputs, not manager profiles.

1. Parse their assignment names and compare values without printing values in
   the migration report.
2. Merge matching and unique assignments into one union.
3. Resolve conflicts from current source, configuration, and durable path
   contracts. Ask the user when direct evidence cannot select a value.
4. Move host-specific operational prose into the matching manager adapter
   rather than the neutral value file.
5. Add `.re-discipline/local-paths.md` to `.gitignore` before rendering the
   merged union from `templates/project/local-paths.md`. Retain
   `.claude/local-paths.md` and `.codex/local-paths.md` as defense-only
   secret-path ignore patterns even after deleting those legacy files.
6. Update maintained readers, currently executable campaign helpers, and
   current documentation to the neutral path. Leave reports and historical
   evidence unchanged when they accurately describe the old layout.
7. Verify assignment-key coverage and required values, then delete both legacy
   files. If either file was tracked, remove it from current tracking without
   rewriting history.

## Present The Planned Split

Before destructive refactoring, show:

- Missing files and directories to add.
- The proposed canonical profile.
- Content moving to the canonical shared-law block or each project-owned host
  section.
- The root `AGENTS.md` routing block.
- Any old external-drafter content moving to the dedicated contract.
- Any legacy external-provider state moving under `.re-discipline/agents/`.
- Legacy local-path assignments merging into the untracked neutral signature,
  including conflict names but not private values.
- Legacy archive references and active consumers, with proposed Maintain,
  Distill, or Delete dispositions.
- Bootstrap/settings files to add or migrate, the proposed memory mode, and
  only the native-memory fields to merge into each project host settings file.
- Unrelated instructions that will remain unchanged.

Ask only about genuine ambiguity. An explicit user request to apply an already
reviewed reconciliation is sufficient approval to continue.

## Apply Non-Destructively

Run the legacy archive semantic migration gate before replacing the canonical
shared-law block. When the gate finds unresolved dependencies, stop that part
of migration, preserve the existing block and files, and report partial
progress instead of claiming completion.

1. Add missing epistemic directories and indexes; never overwrite a populated
   index without approval.
2. Add the shared knowledge topology and source-owned bootstrap/settings
   templates. Existing projects migrate to `shared-only` only with the user's
   explicit approval; otherwise preserve their current host behavior and
   record `hybrid` or `native` accurately. Never infer a mode from native
   memory directory contents.
3. Add `.re-discipline/cache/` to `.gitignore` and keep every cache,
   calibration run, and model artifact untracked.
4. Write `.re-discipline/project-profile.md` with one shared-law block and
   reconciled project facts.
5. Reconcile legacy local paths through the procedure above. Create the
   neutral signature even when no legacy file exists.
6. Write or update the Claude managed-adapter block and preserve or append
   project-owned Claude notes outside it. Parse project
   `.claude/settings.json`, preserve unrelated fields, and change only
   `autoMemoryEnabled` for the approved mode.
7. Write or update the Codex managed-adapter block and preserve or append
   project-owned Codex notes outside it. Parse project `.codex/config.toml`,
   preserve unrelated keys/comments, and merge memory fields into existing
   tables without duplicating them.
8. Move the old root drafter role to
   `.codex/external-drafter-contract.md` and install the root router, preserving
   unrelated root guidance outside managed markers.
9. Always render the normalized agent core under `.re-discipline/agents/`.
   Migrate only real configured providers; do not promote disabled,
   placeholder, or unevaluated entries. Preserve each real provider as one
   config entry and exactly `profile.md`, `scorecard.md`, and `teardown.md`.
10. Preserve each in-flight candidate under
   `.re-discipline/agents/recruiting/<candidate>/`, using this mechanical
   mapping when the legacy names exist:
   - `CANDIDATE.md` -> `candidate.md`
   - `config-draft.json` -> `config.json`
   - `profile-draft.md` -> `profile.md`
   - `rollback-manifest.md` -> `teardown.md`
   - `interview/` -> `runs/`
   Keep an existing `scorecard.md`. Validate candidate config without adding
   it to live config.
11. Update dispatchers to read canonical `framing`. Retain legacy host paths
   only in explicitly labeled migration or recovery code.
12. Replace duplicate identity prose in internal agent-loaded docs with links to
   the canonical profile. Human-facing README files may remain self-contained
   when they do not contradict it.
13. Compare the post-migration union against both legacy profiles. Delete
   `.claude/project-profile.md` and `.codex/project-profile.md` only after every
   meaningful instruction has a surviving destination.
14. Delete old root agent and recruiting directories only after their
   normalized state and meaning are verified.
15. Count canonical-profile lines and UTF-8 bytes. Warn above 240 lines or
   16 KiB; never truncate it.
16. Validate the bootstrap, settings, host policy, and cache exclusion, then
    run the exact preflight, initial index, canonical-profile replay, and
    packaged quick-smoke commands in the parent skill's Step 4. Do not run a
    project/full benchmark or calibration during ordinary migration.

Do not restructure, promote, or otherwise normalize provisional `active/`
scratch as part of migration. Limit active-tree edits to executable path
readers that would otherwise break after the legacy files are removed.

## Meaning-Preserving Gate

Verify that the union of the canonical profile, project-owned manager notes,
managed adapters, drafter contract, and preserved unrelated guidance contains
every meaningful instruction from the originals. Report moved, deduplicated,
and unchanged sections. Separately verify that the neutral local-path file
covers every legacy assignment key, remains untracked, and is the only current
local-path signature. Confirm that a greenfield or fully migrated project does
not create or require a durable root-level replacement evidence directory.
Temporary campaign evidence under `active/<slug>/evidence/` remains valid.
Report unresolved legacy archive dependencies explicitly and do not declare
migration complete. Confirm no machine-local native memory file was read,
written, deleted, or copied. Do not commit unless the user asks.
