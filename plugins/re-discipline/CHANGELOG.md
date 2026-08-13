# Changelog

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
