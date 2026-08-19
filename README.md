# re-discipline 1.2

An evidence-disciplined reverse-engineering knowledge system for Claude
Code, Codex, and any MCP-capable agent. Two halves:

- **Curation** — campaign workspaces in `.re-discipline/active/` where
  investigators write full reports and distill atomic candidate
  findings; a manager promotion skim is the single quality gate into
  `.re-discipline/docs/`, the markdown-canonical knowledge base.
- **Retrieval** — `re-search`, a small ephemeral CLI: SQLite FTS5 with
  identifier-aware matching, auto-reindexing, golden-question
  regression bench, per-session MCP stdio server, and an HTTP endpoint.

Design rule: at every layer the recovery path is "edit a text file" or
"delete and rebuild" — never satisfying a machine's proof requirements.
Docs are truth; the index is disposable; git is history.

## Install

```text
/plugin marketplace add adiazpar/re-discipline
/plugin install re-discipline@re-discipline
```

Then run `/re-discipline:init-project` inside your target project. It is
idempotent: it creates `.re-discipline/` (docs, campaigns, conventions,
a project-local copy of `re-search.exe`), writes the marker-bounded
orientation block into root `AGENTS.md` (the one canonical agent file —
Codex reads it natively; root `CLAUDE.md` imports it via `@AGENTS.md`
for Claude Code), and asks once whether to use shared memory
(`docs/ops/` replacing host-native memory).

## Updating

When the plugin updates (marketplace update + plugin reload), re-run
`/re-discipline:init-project` in each project that uses it. Init is the
upgrade path: it refreshes the three plugin-owned copies
(`CONVENTIONS.md`, `.re-discipline/.gitignore`, `re-search.exe`) and
converges the host wiring to the current layout, while never touching
your docs, campaigns, archives, golden questions, or any content outside
its marker block. Tell collaborators the same: after updating the
plugin, run init-project once per project — that is the whole migration
for 1.x upgrades.

## Daily use

- `.re-discipline/bin/re-search.exe query "how do I …"` — search before
  investigating; agents get the same via the `query` MCP tool.
- `query --kind fact` / `--kind ops` / `--kind reference`, and
  `--grade direct` — narrow to one class of doc. Flags go BEFORE the
  question text.
- `/re-discipline:open-campaign` — start an investigation workspace.
- Investigate; producers distill findings as they go (see the curate
  skill / `.re-discipline/CONVENTIONS.md`).
- `/re-discipline:close-campaign` — promote, summarize, archive.
- Wrong doc later? Edit it, mark `superseded`, link the replacement.

## Symbol lookup

Some knowledge is a table, not a document: struct layouts, enum values,
constants. Turning tens of thousands of those into markdown would bury
the docs and dilute the index for records nobody browses — you look a
struct up by name or not at all.

So `re-search` keeps them in a separate `symbols` table, deliberately
outside the FTS doc index:

```text
re-search symbol idLangDict_langEntry_t

[struct] reference/idlib_schema.json
idLangDict_langEntry_t  size 32
  id      unsigned int    +0x00  4
  key     idAtomicString  +0x08  8
  value   idAtomicString  +0x10  8
  maxLen  unsigned int    +0x18  4
  minLen  unsigned int    +0x1C  4
```

Exact match first, substring only as a fallback. Also available as the
`symbol` MCP tool and `GET /symbol?name=…`.

To populate it, write `.re-discipline/symbols.jsonl` — one object per
line, `{name, kind, render, source}` — from whatever your project's
type data happens to be. `render` is opaque pre-formatted text, so the
tool never needs to learn your schema's shape. The file is optional;
its absence is a no-op.

Symbols stay behind the dedicated call rather than being appended to
`query` results. That was measured, not assumed: across a 148-question
golden set, 16 questions contained a token matching a symbol name and
10 of those were ordinary English colliding with constants (`player`,
`resource`, `encounter`). Blending would have attached noise to more
queries than it helped, and the largest single render is 642 lines.

## Mixing curated facts with generated reference material

A knowledge base usually grows two kinds of doc: hard-won `kind: fact`
claims, and bulk `kind: reference` entries generated one-per-item from a
dump — every console command, every engine event, every type. Reference
entries are numerous, short and keyword-dense, so left alone they bury
the curated facts on ordinary questions.

`re-search` handles this without a filter the caller has to remember:

- **Ranking** weights `title` and the identifier column above `body`.
- **Reference docs take a rank penalty on concept questions**, and are
  exempt from it when the query names one of their declared `idents`.
  So "why did the boss stand still" reaches a curated fact, while
  "what does event `spawnSingleAI` do" reaches the reference entry.
- **`idents:` frontmatter** declares the identifiers a doc owns. This is
  what earns the exemption; without it a reference doc is only findable
  by prose.
- **Cross-reference bullets are stripped from the index** (not from your
  files) — a "Depends on" list quoting other docs' titles makes every
  doc compete for every other doc's topic.

One rule when generating docs in bulk: **a phrase repeated across every
generated doc destroys its own search weight.** Measured on a real
11,000-doc corpus, writing one stock token into 1,611 docs took its
document frequency from 22 docs to 1,633 and broke unrelated queries;
a tag applied to 6,862 docs put that word in 64% of the corpus. Put
provenance and confidence in frontmatter, keep body prose specific to
the entry, and keep tags rare.

Grow `golden.jsonl` before a bulk load, not after, and include questions
whose expected answer is a reference doc — otherwise the bench can only
measure harm to facts and never benefit to lookups.

Windows-only. Requires nothing running in the background: the tool
starts, answers, exits.
