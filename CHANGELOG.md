# Changelog

## 1.2.0 - 2026-08-19

Retrieval work driven by a corpus that grew from 254 to 11,054 docs,
97% of it generated reference material. Ranking now weights title and
identifiers above body, and strips cross-reference bullets from the
index (not from your files), so a "Depends on" list quoting other docs'
titles no longer makes every doc compete for every other doc's topic.
That alone moved a 148-question benchmark from 89 to 98.

Generated per-item docs get a new `kind: reference` and a rank penalty
on natural-language questions, waived when the query names one of the
doc's declared `idents`. A concept question therefore reaches curated
facts while a name lookup reaches the reference entry. Tried and
rejected: a hard tier and a default kind filter, both of which fixed
the fact half by destroying the reference half, taking name lookups
from 41/42 to 7/42.

New `symbol` lookup for knowledge that is a table rather than a
document — struct layouts, constants, enum groups — read from an
optional `.re-discipline/symbols.jsonl` into a separate table outside
the doc index, and reachable from the CLI, the MCP server and HTTP.
Measurement kept it behind a dedicated call instead of blending into
`query` results: most name collisions across a real golden set were
ordinary English words matching constants.

Also: `query` gains `--kind` and `--grade` filters on all three
surfaces; `idents:` and `evidence:` frontmatter are parsed, the latter
having been documented but silently dropped; snippets widen with match
markers; and the index carries a format version so a content-shape
change forces a rebuild instead of serving stale terms indefinitely.

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