# Changelog

## Unreleased

- Normalize namespaced Codex tool names at the hook boundary so registered
  workers launched through `collaboration.spawn_agent` receive their run
  binding while namespaced write tools retain the same grant enforcement.

## 0.9.4 - 2026-08-16

- Make run report paths engine-owned: manager transitions submit only the
  report SHA-256, while one typed run-workspace constructor derives and
  verifies the canonical handle.
- Reject caller-supplied report paths before publication and enforce canonical
  brief, context-pack, and report locations at the transaction boundary.
- Remove the unreleased report-relocation action and its special transaction
  path.

## 0.9.3 - 2026-08-16

- Keep PowerShell on native Unix from reinterpreting Windows-rooted patch
  targets as project-relative paths.
- Reject ambiguous drive-relative and backslash-rooted targets consistently
  across PowerShell and POSIX hook hosts.

## 0.9.2 - 2026-08-16

- Reject Windows drive-qualified patch targets on native POSIX hosts while
  preserving canonical in-project resolution under MSYS and Cygwin shells.
- Exercise the foreign-drive containment boundary independently on Linux and
  macOS so host-parity tests cannot be skipped with PowerShell unavailable.

## 0.9.1 - 2026-08-16

- Let ordinary record-scoped mutations rebase once under the writer lock while
  retaining exact global-head gates for irreversible project-wide operations.
- Isolate concurrent manager sessions and registered subagents by host session,
  agent identity, immutable context pack, and non-overlapping write grants.
- Run onboarding once per host session and keep launch contention inside a
  bounded pre-start handoff instead of exposing retry loops to agents.
- Treat response budgets as hints, preserve context packs as snapshots, and
  align Windows launch, hook parity, and cache-version checks with those rules.

## 0.9.0 - 2026-08-12

- Add deterministic, digest-bound `campaign.merge` planning and atomic
  multi-campaign application through MCP and CLI.
- Preserve and remap complete campaign graphs, artifacts, event history,
  provenance, run outcomes, and explicit historical chronology in one
  canonical target campaign.
- Add explicitly destructive `campaign.discard` with exact identity,
  confirmation, digest, concurrency, recovery, and idempotency guards.
- Add merge/discard schemas, manager skills, transaction recovery coverage,
  action-surface tests, and end-to-end topology fixtures.
