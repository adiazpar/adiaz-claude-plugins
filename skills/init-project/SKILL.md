---
name: init-project
description: >-
  Initialize, resync, or recover a re-discipline 0.8 project. Installs the
  canonical profile, structured state configuration, host adapters, schemas,
  policy, and safe recovery markers without converting prior project state.
---

# Initialize A Re-Discipline Project

Initialization installs the 0.8 control plane. It does not perform project
state migration.

## Detect The Mode

Inspect the canonical profile, `.re-discipline/config.json`, host adapters,
managed markers, source indexes, and repository instructions.

- **Initialized:** required 0.8 files validate; stop unless resync was asked.
- **Recovery:** 0.8 markers exist but tracked managed files are missing.
- **Drop-in:** project guidance exists but no re-discipline state exists.
- **Greenfield:** no meaningful project guidance exists.
- **Migration required:** an older re-discipline state version is declared.

For migration-required projects, make no ordinary state mutation. Report the
detected version and invoke `migrate-project`. Never inspect or convert older
state as an initialization side effect.

## Install Canonical 0.8 Files

Render current templates and preserve unrelated host instructions:

- `.re-discipline/project-profile.md` with one shared-law block;
- `.re-discipline/config.json` and knowledge policy/profile files;
- `.claude/CLAUDE.md`, `.codex/AGENTS.md`, and the root router;
- the external drafter contract and provider configuration;
- state, archive, lock, cache, memory, and evaluation roots from `tree.txt`;
- host memory settings selected by project policy.

Use marker-bounded edits for existing adapters. Keep machine-local values in
the ignored `.re-discipline/local-paths.md`. Preserve malformed existing files
byte-for-byte and request repair approval.

Do not precreate campaign record trees or payload categories. Campaign state
is created by `campaign.open`.

## Verify

Validate all JSON against packaged schemas, path boundaries, ignored local and
cache state, host memory policy, adapter imports, provider records, and absence
of duplicate project facts. Resolve the active packaged runtime and run its
read-only preflight, one deterministic index build, exact profile replay, and
packaged quick benchmark.

Recovery may restore a missing tracked managed file from `HEAD` or create a
missing untracked managed file from the current safe template. It never
overwrites malformed content, performs migration, accesses native memory
stores, or runs calibration.

Report installed version, validation results, and explicit follow-ups. Do not
commit unless the user explicitly asks.

## References

- Project templates: `<plugin-root>/templates/project/`.
- Migration: `migrate-project`.
- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
