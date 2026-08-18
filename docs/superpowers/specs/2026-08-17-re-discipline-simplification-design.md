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
`re-search.exe` are copied into the project so any agent that can read
files and run a command can use the system without the plugin installed.
(`bin/` is gitignored, so a fresh clone lacks the exe until init is re-run
or the exe is copied in — the docs themselves remain fully readable and
grep-able regardless.)

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
   findings into `docs/`, setting `status: promoted` as part of the move
   and regenerating `INDEX.md` (`re-search index`). Investigators do not
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

- `re-search index` — rebuild `index.db` from `docs/` (SQLite FTS5, BM25)
  and regenerate `INDEX.md` from doc frontmatter and paths. `INDEX.md` is
  a derived artifact (committed for browsability, but a clobber costs
  nothing — regenerating it is the recovery). `INDEX.md` itself is
  excluded from the FTS index so it cannot pollute rankings.
- `re-search query "<question>"` — ranked excerpts + file paths, text or
  JSON output. **Auto-reindexes** first when stale. Staleness is a
  manifest diff: the index stores `(path, mtime, size)` for every indexed
  file, and a fresh scan of `docs/` that differs — including deletions and
  moves — triggers rebuild. At hundreds of docs the scan is trivially
  cheap per query. Each hit carries its `status`, `kind`, and `grade`;
  superseded hits are labeled and downranked so dead knowledge is never
  presented as current truth.
- `re-search bench` — run every `golden.jsonl` question (record shape:
  `{"q": "...", "expect": "docs/..."}`), report whether the expected doc
  ranked in the top 5. A regression test, not a research instrument. The
  project's real `golden.jsonl` is run in-project (manually, or as part of
  the close-campaign sweep); plugin CI runs bench only against its own
  fixture corpus.
- `re-search serve --mcp` — thin stdio MCP server exposing one `query`
  tool. Spawned per-session by the host (normal MCP lifecycle), stateless,
  safe to run many instances, loses nothing when killed.
- `re-search serve --http` — small JSON endpoint over the same query path.
  Included as an explicit user decision (no phased builds), not on behalf
  of the out-of-scope oracle; it is the retrieval half any future consumer
  wraps.

Robustness rules (binding on the implementation):

- **Project-root discovery**: `re-search` walks up from cwd to the nearest
  `.re-discipline/`, overridable with `--root` (the plugin `.mcp.json`
  entry sets `--root` via the host's project-directory variable).
- **Indexing is lenient**: a doc with malformed frontmatter is indexed as
  plain text with a warning in command output; indexing never fails on doc
  content.
- **The index is disposable at runtime**: any error opening or reading
  `index.db` triggers silent delete-and-rebuild.

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
added to `golden.jsonl`. Real failures become permanent regression cases,
run by the in-project `bench` (plugin CI covers only its fixture corpus).

## 6. Concurrency model (multiple sessions)

No shared daemon. The shared surface is the filesystem.

- **Reads**: SQLite serves concurrent readers; N sessions = N short-lived
  processes reading one file.
- **Reindex contention (Windows semantics, not POSIX)**: one rebuilder at
  a time, enforced by an OS-held lock the kernel releases on process death
  (exclusive file handle / `LockFileEx`) — never a bare marker file that a
  killed process would orphan. The rebuilt index is swapped in without
  assuming atomic replace-over-open-file (which fails with sharing
  violations on Windows): retry with short backoff, and on persistent
  failure serve the existing index for this query and try again next time.
  **A query never fails or blocks because a reindex could not complete.**
- **Writes**: one campaign folder per session/task isolates concurrent
  curation. Concurrent sessions share one checkout, so same-doc collisions
  are silent last-writer-wins, not git conflicts — accepted as rare and
  repairable: docs are small and atomic, git history preserves the
  overwritten version, and the designated hot file (`INDEX.md`) is
  derived and regenerable (§5), so a clobber there costs nothing.
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
  `autoMemoryEnabled: false` in project `.claude/settings.json`; Codex:
  the equivalent key, pinned at implementation — and if Codex has no
  project-scoped memory switch, the marker-block instruction alone is the
  mechanism there). Agent edits these directly at init; the awk parser
  machinery is deleted. `Shared memory: disabled` means init **never
  touches host memory settings** — it only ever writes them when enabling,
  so a user's unrelated memory configuration is never reverted.
- **UX: ask once at first init** — "Use shared memory? Replaces host-native
  memory with `.re-discipline/docs/ops/` so all agents share recall.
  [Recommended: yes]". The decision is recorded as a visible line in the
  AGENTS.md marker block (`Shared memory: enabled`), which is
  simultaneously the stored policy, the agent instruction, and
  user-editable. **Root `AGENTS.md` is canonical for policy**; init syncs
  the `.claude/CLAUDE.md` block from it, so disagreement between the two
  resolves in AGENTS.md's favor. Re-running init reads the line and
  converges host settings without re-asking. Changing your mind = edit the
  line, re-run init.
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
Marker edge cases: if markers are absent, append a fresh block; if exactly
one well-formed pair exists, replace its contents; in any other case
(unpaired, duplicated, nested) init leaves that file untouched and reports
it for manual repair. Re-running init after a plugin update refreshes the
block, the conventions copy, and the exe.

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

## 11. Plugin repo structure, versioning, tests, CI

**Structure.** Repo becomes: 4 skills, `retrieval/` (the Go module),
`hooks/` (one PowerShell file + hooks.json), `templates/` (CONVENTIONS.md,
marker block, skeleton), `.mcp.json` (registers `re-search serve --mcp` for
Claude/Codex), one committed `windows-amd64` exe (marketplaces install by
git clone), plugin manifests, README, CHANGELOG.

Deleted: the 0.x Go engine (~62k lines + ~35k test lines), all 52 schemas,
GloVe/model artifacts, evals/conformance, benchmarking/calibration/
migration tooling, references/ (replaced by CONVENTIONS.md), sh/awk hooks,
the version sync/guard Python scripts, SHA256SUMS and binary manifests.

**Versioning.** Manual semver. Canonical version lives in
`.claude-plugin/plugin.json`; the Codex manifest and marketplace entries
must match; the exe is stamped at build via ldflags (`re-search
--version`). A release = bump manifests, CHANGELOG entry, rebuild exe,
commit `Cut re-discipline X.Y.Z`, tag. No sync scripts — CI checks the
manifests agree.

**Tests.** All 0.x engine tests are deleted with the engine. New tests,
written fresh for `re-search`: tokenizer (identifier splitting),
frontmatter parsing, index build, query ranking, auto-reindex staleness +
lock/swap behavior, MCP stdio smoke test (spawn, one query,
exit), HTTP handler test, and `bench` against a small fixture corpus as
the end-to-end accuracy check. Total runtime: seconds.

**Build pinning (makes the stale-binary check passable).** SQLite driver
is pure-Go with FTS5 support (`modernc.org/sqlite`) — no cgo, or
reproducibility is unattainable. Go toolchain pinned via the `toolchain`
directive in `go.mod` and used identically by CI. One canonical build
command shared verbatim by the release step and CI:
`go build -trimpath -buildvcs=false -ldflags "-X ...version=X.Y.Z"`.

**CI.** One workflow, one Windows job, on push/PR: `go vet` + `go build` +
`go test` + `bench` on the fixture corpus + version-consistency check + a
stale-binary check (rebuild with the canonical pinned command,
hash-compare against the committed exe — catches "changed source, forgot
to rebuild"). ~1–2 minutes. CI's entire purpose: a bad commit cannot ship
a broken or stale exe. No mac/linux jobs, no packaging matrices. The
committed exe does grow repo history by a few MB per release — accepted
as far better than 0.x's six-binary matrix; shallow clones mitigate.

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
