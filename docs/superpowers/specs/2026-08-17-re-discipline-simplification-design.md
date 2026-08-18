# re-discipline 1.0 — Simplification Redesign

Date: 2026-08-17
Status: Approved design, pending implementation plan

## 1. Mission and governing principle

re-discipline is a reverse-engineering discipline plugin with two halves:

- **Curation** — accumulate hard, correctable facts about the software under
  analysis (game engine behavior, function signatures, memory addresses,
  workflows, dead ends) through campaign-based investigation.
- **Retrieval** — let any agent ask a question and get the relevant curated
  evidence back, so work is never redone and knowledge compounds.

Governing principle of this redesign: **at every layer, the recovery path is
"edit a text file" or "delete and rebuild" — never "satisfy the machine's
proof requirements."** The prior implementation (0.x) was a transactional
engine with digests, head revisions, ratification protocols, write-grant
hooks, and retrieval benchmarking apparatus. Its failure mode was constant:
byte-exact proofs are what LLM agents are worst at, so sessions became plugin
surgery instead of reverse engineering. The nouns survive (campaigns,
findings, overturns); the machinery does not.

Platform scope: **Windows only.** No mac/linux binaries, hooks, or CI.

## 2. Project layout (what init creates in a target project)

```
.re-discipline/
  CONVENTIONS.md     # copied from plugin: doc format, curation flow, grades
  docs/              # curated truth — one topic/claim per file
    INDEX.md         # browsable table of contents, updated at promotion
    ops/             # operational recall (shared memory) — see §8
    <domain>/...     # engine/, entities/, snapmap/ — grows organically
  active/
    <campaign-slug>/
      CAMPAIGN.md    # goal, status, working notes (goals are session-scoped;
                     # there is no goal document type)
      reports/       # full investigator reports, append-only, never squashed
      findings/      # candidate findings awaiting promotion
  archive/
    <campaign-slug>/ # closed campaigns: reports + summary
  golden.jsonl       # retrieval regression questions
  bin/re-search.exe  # copied from plugin (gitignored)
  index.db           # SQLite FTS index (gitignored, disposable)
  .gitignore         # ignores index.db and bin/
AGENTS.md            # project root — orientation marker block (§9)
.claude/CLAUDE.md    # project root — same orientation via marker block
```

Everything plugin-related lives under `.re-discipline/` except the host
orientation files, which must stay at root. `CONVENTIONS.md` and
`re-search.exe` are copied into the project so it is self-contained: any
agent that can read files and run a command can use the system without the
plugin installed.

## 3. Document format

Small frontmatter, then one atomic claim with supporting detail:

```markdown
---
status: candidate | promoted | superseded
kind: fact | ops
grade: direct | inferred | reported     # facts only
evidence: [active/<slug>/reports/R-007.md#joints]
tags: [entities, animation]
---
# Entity binding to demon joints goes through idAnimatedEntity::AttachJoint
...details, addresses, snippets...
```

- One claim per file. Titles are searchable assertions, not labels.
- `grade` records how the fact was established: `direct` (observed in
  decompilation/memory), `inferred` (deduced), `reported` (external source).
- Superseding a claim = edit the doc, set `status: superseded`, link the
  replacement. Git history is the provenance trail. That is the entire
  overturn mechanism.
- **Negative results are first-class docs** ("approach X does not work,
  because Y") — they are what prevents doing the same thing twice.
- Conflicting observations may both be recorded with the conflict noted;
  contradictions are information in RE work.

## 4. Curation flow

**Distillation happens at the point of production, by the producer.** The
agent that just finished investigating has full context in its window; that
moment never comes back.

1. **Investigate** — inline (manager works directly in-session) or via
   subagents. Same conventions either way; orchestration style is a
   per-session judgment (inline for quick targeted questions, subagents for
   deep dives that would flood manager context). The plugin does not encode
   or enforce modes.
2. **Producer writes two artifacts**: the full report (raw, archived,
   never squashed) and candidate findings — atomic docs written
   **incrementally as discovered**, not as an end-of-run chore, each linking
   back to its report.
3. **Manager promotes** — the single quality gate. The manager never re-reads
   reports to curate; it skims the already-atomic findings with a concrete
   checklist: is the claim atomic, is evidence cited, does the grade match
   the evidence. It dedupes (searching existing docs first), resolves
   conflicts (higher grade wins, or both recorded), and moves accepted
   findings into `docs/`, updating `INDEX.md`. Investigators do not
   self-promote.
4. **Close campaign** = final promotion sweep + short summary + move the
   folder to `archive/`. A file move, not a proof obligation.

The user's backstop is reviewing promotions as ordinary git diffs. The
quality bar is "traceable and fixable forever," not "provably true at
entry" — wrong docs are cheap to correct (§3) and evidence links let any
future agent re-verify.

## 5. Retrieval tool: `re-search`

A fresh, small Go program (windows-amd64). Ephemeral by design: starts,
answers, exits. All state on disk. The 0.x engine is deleted, not trimmed.

Commands:

- `re-search index` — rebuild `index.db` from `docs/` (SQLite FTS5, BM25).
- `re-search query "<question>"` — ranked excerpts + file paths, text or
  JSON output. **Auto-reindexes** first if any doc is newer than the index,
  so staleness is not a concept anyone manages.
- `re-search bench` — run every `golden.jsonl` question, report whether the
  expected doc ranked in the top 5. A regression test, not a research
  instrument.
- `re-search serve --mcp` — thin stdio MCP server exposing one `query`
  tool. Spawned per-session by the host (normal MCP lifecycle), stateless,
  safe to run many instances, loses nothing when killed.
- `re-search serve --http` — small JSON endpoint over the same query path;
  the retrieval half of any future hosted oracle.

Retrieval design decisions:

- **One query path, no modes/profiles.** The 0.x lanes-plus-profiles design
  was a symptom of not choosing.
- **Identifier-aware tokenization**, not a separate lane: index
  `idAnimatedEntity::AttachJoint` whole and split, so symbol queries and
  natural-language queries both hit.
- **Graph expansion is the markdown links**: a hit shows evidence links and
  tags; the querying agent follows them.
- **No vector/dense lane.** Design decision, not deferral: an
  identifier-heavy corpus is FTS home turf; embeddings drag model artifacts
  and their supply chain back in. Reopening condition: `golden.jsonl`
  accumulates real misses that are clearly semantic. Retrieval quality at
  this corpus scale (hundreds of docs) is dominated by doc quality, which
  the curation conventions enforce; and the consumer is an agent that can
  reword, retry, and fall back to grep.

Accuracy workflow: whenever retrieval fails in real use, that question is
added to `golden.jsonl`. Real failures become permanent regression cases;
CI runs `bench` against them.

## 6. Concurrency model (multiple sessions)

No shared daemon. The shared surface is the filesystem.

- **Reads**: SQLite serves concurrent readers; N sessions = N short-lived
  processes reading one file.
- **Reindex contention**: rebuild into a temp file, atomic rename over
  `index.db`, lock file so one rebuilder runs while others use the existing
  index.
- **Writes**: one campaign folder per session/task isolates concurrent
  curation. Same-doc promotion collisions surface as git conflicts for the
  user — git is the reconciliation, replacing the 0.x global lock/journal
  engine.
- **MCP**: stdio servers are per-session by nature (host spawns/kills).
  Statelessness makes many instances safe and orphans harmless.

## 7. Skills (12 → 4)

- `init-project` — §9.
- `open-campaign` — create `active/<slug>/` with CAMPAIGN.md; goal is
  stated there, in-session.
- `close-campaign` — promotion sweep, summary, move to archive.
- `curate` — the report/finding conventions, referenced by whoever
  investigates (manager inline or subagent brief).

Deleted: onboard (host-loaded orientation replaces it, §10), overturn (a
doc edit, §3), merge-campaigns, discard-campaign (file operations),
review-memory (§8), benchmark-knowledge, calibrate-knowledge,
decide-retrieval-profile (replaced by `bench` + fixed query path),
migrate-project (§12).

## 8. Shared memory

Purpose (retained from 0.x): replace Claude/Codex native memory so all
agents on the project share one recall instead of accumulating private,
divergent memories.

- **`docs/ops/` is the shared memory.** Operational recall ("Ghidra project
  at X", "dump tool needs flag Y", "tried Z, failed") flows through the
  same producer → promotion gate and the same index. Separation from
  empirical facts is carried by path and `kind: ops` — no second pipeline,
  no proposal tier, no memory index.
- **Host-native memory is switched off** when shared memory is enabled:
  project-scoped where the host allows (Claude Code:
  `autoMemoryEnabled: false` in project `.claude/settings.json`; Codex
  equivalent — implementation pins exact keys). Agent edits these directly
  at init; the awk parser machinery is deleted.
- **UX: ask once at first init** — "Use shared memory? Replaces host-native
  memory with `.re-discipline/docs/ops/` so all agents share recall.
  [Recommended: yes]". The decision is recorded as a visible line in the
  AGENTS.md marker block (`Shared memory: enabled`), which is
  simultaneously the stored policy, the agent instruction, and
  user-editable. Re-running init reads the line and converges host settings
  without re-asking. Changing your mind = edit the line, re-run init.
- There is no separate "project profile" artifact; the marker block is the
  profile, holding exactly the policies that exist (currently one).

## 9. init-project

**Idempotent and non-destructive**: run any time on anything; creates what
is missing, never overwrites what exists, updates only marker-bounded
blocks, reports what it did. New and existing projects are the same code
path. Replaces the 0.x five-mode detection, schema validation, and
init-time benchmark preflight entirely.

Creates: the `.re-discipline/` tree (§2), copies `CONVENTIONS.md` and
`re-search.exe` from the plugin, writes/updates the orientation marker
block (`<!-- re-discipline:start -->` … `<!-- re-discipline:end -->`) in
root `AGENTS.md` and `.claude/CLAUDE.md` (~5 lines: where docs live, how to
query, where active campaigns are, conventions location, shared-memory
policy line). Existing user content outside markers is never touched.
Re-running init after a plugin update refreshes the block, the conventions
copy, and the exe.

Old-format state (0.x `config.json`, campaign JSON) detected → init stops
and directs the user to the one-time migration session (§12). Never
converts as a side effect.

## 10. Session start

- **Orientation is free**: hosts load AGENTS.md/CLAUDE.md at session start,
  which contain the marker block.
- **One SessionStart hook** (~20–30 lines PowerShell, read-only, fails
  silent — the only hook in the plugin): prints a single status line, e.g.
  `re-discipline: 1 active campaign — demon-transforms (updated 2d ago).
  142 docs curated.` If it errors, the session starts without the line.

Deleted: all 0.x write-guard/launch-handshake/subagent-binding hooks
(~2,700 lines of PowerShell/bash/awk). Protection of `docs/` is the
promotion convention plus git review. (If accidental doc writes ever prove
a real problem, a ~20-line PreToolUse hook can return; not shipped.)

## 11. Plugin repo structure, CI, tests

Repo becomes: 4 skills, `retrieval/` (the Go module), `hooks/` (one
PowerShell file + hooks.json), `templates/` (CONVENTIONS.md, marker block,
skeleton), `.mcp.json` (registers `re-search serve --mcp` for Claude/Codex),
plugin manifests, README, CHANGELOG.

Deleted: the 0.x Go engine (~62k lines + ~35k test lines), all 52 schemas,
GloVe/model artifacts, evals/conformance, benchmarking/calibration/
migration tooling, references/ (replaced by CONVENTIONS.md), sh/awk hooks.

Tests: Go unit tests on tokenizer, indexer, ranking, auto-reindex locking —
seconds to run. CI: one Windows job, `go build && go test && re-search
bench` on a fixture corpus (~1–2 min). No mac/linux jobs, no binary
matrices, no version-guard scripts beyond what one platform needs.

## 12. Migration of existing project state (snaphak-re)

A one-time session task, not a plugin feature: an agent reads the old
campaign/finding JSON, writes equivalent markdown docs (findings →
`docs/`, reports → `archive/`), the user reviews the diff, and the old
tree is deleted only after the user is satisfied. The 0.x `migrate-project`
machinery is deleted.

## 13. Out of scope (by nature, not by phase)

- **The hosted Discord oracle** — a separate downstream product (hosting,
  bot, LLM answer synthesis) consuming this plugin's corpus and
  `serve --http` endpoint. Nothing in this design needs rework for it.
- **Vector retrieval** — excluded per §5, with an explicit evidence-based
  reopening condition.
- **mac/linux support.**
