# Changelog

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

## 0.9.6 - 2026-08-17

- Treat the five engine-managed documentation indexes as navigation artifacts
  before applying the stricter canonical truth-finding path rule, allowing a
  truth-projecting closure to finalize without admitting loose truth files.

## 0.9.5 - 2026-08-17

- Normalize namespaced Codex tool names at the hook boundary so registered
  workers launched through `collaboration.spawn_agent` receive their run
  binding while namespaced write tools retain the same grant enforcement.
- Bind the dispatched Windows knowledge runtime to a kill-on-close Job Object
  so terminating its architecture launcher cannot leave the child process
  orphaned.
- Publish native truth as canonical `FindingDocument` records under the single
  campaign-scoped `docs/truth/findings/<campaign>/<F-id>.md` provenance
  namespace, refuse free-form topic paths, and provide an exact-digest atomic
  relocation for legacy root and flat-finding projections.
- Key the derived finding index by campaign plus local finding ID, collapse an
  exact archive/truth projection pair to the truth source, and retain strict
  conflict refusal for non-identical copies.
- Make typed `docs/truth/findings/**` records the sole truth retrieval source;
  classify `docs/truth/INDEX.md` as navigation and keep legacy split manifests
  and loose Markdown outside the truth-authority index.

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
