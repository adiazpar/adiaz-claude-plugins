# Changelog

## 1.1.0 - 2026-08-18

Host-wiring correction, verified against Codex source and Claude Code
docs: root `AGENTS.md` is now the single canonical agent file (Codex
reads it natively but never reads `.codex/AGENTS.md`; Claude Code does
not auto-read `AGENTS.md` and imports it instead). `init-project` now
writes the marker block only into `AGENTS.md`, ensures root `CLAUDE.md`
contains `@AGENTS.md`, and removes marker blocks from pre-1.1 dual-block
layouts. New "Working artifacts" convention: campaign-scoped scratch in
`active/<slug>/work/`, durable tooling graduates to the project source
tree. README gains an Updating section — re-running init-project is the
whole upgrade path for initialized projects.

## 1.0.0 - 2026-08-18

Ground-up simplification. Markdown in `.re-discipline/docs/` is now the
canonical knowledge store; the retrieval index is derived and
disposable. Replaces the 0.x transactional engine (digests, head
revisions, ratification, write-guard hooks, retrieval profiles,
benchmarking apparatus) with:

- `re-search`: small ephemeral Go CLI — `index`, `query`
  (auto-reindexing, identifier-aware FTS5), `bench` (golden regression
  questions), `serve --mcp` (per-session stdio, any MCP host),
  `serve --http`.
- Producer-distilled curation: investigators write reports + atomic
  candidate findings; the manager's promotion skim is the single gate.
- 4 skills (init-project, open-campaign, close-campaign, curate), one
  read-only SessionStart hook, shared memory as `docs/ops/`.
- Windows-only; single 1–2 minute CI job.

Migration from 0.x is a one-time agent session (old JSON → markdown);
init detects old state and stops rather than converting.