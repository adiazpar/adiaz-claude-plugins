# re-discipline 1.0

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
- `/re-discipline:open-campaign` — start an investigation workspace.
- Investigate; producers distill findings as they go (see the curate
  skill / `.re-discipline/CONVENTIONS.md`).
- `/re-discipline:close-campaign` — promote, summarize, archive.
- Wrong doc later? Edit it, mark `superseded`, link the replacement.

Windows-only. Requires nothing running in the background: the tool
starts, answers, exits.
