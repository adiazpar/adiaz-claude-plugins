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
a project-local copy of `re-search.exe`), adds marker-bounded
orientation blocks to `AGENTS.md`/`.claude/CLAUDE.md`, and asks once
whether to use shared memory (`docs/ops/` replacing host-native memory).

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
