# Changelog

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
