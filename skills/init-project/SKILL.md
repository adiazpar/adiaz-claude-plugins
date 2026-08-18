---
name: init-project
description: >-
  Initialize or resync a re-discipline project. Idempotent and
  non-destructive: creates what is missing, never overwrites existing
  content, updates only marker-bounded blocks, and reports what it did.
---

# Initialize A re-discipline Project

Run from anywhere inside the target project. New and existing projects
are the same procedure — they just start with different things missing.

## 0. Old-format check (hard stop)

If `.re-discipline/config.json` or campaign JSON records from
re-discipline 0.x exist: create and convert NOTHING. Tell the user:
"old-format re-discipline state detected — run the one-time migration
session first (an agent reads the old JSON and writes 1.0 markdown docs;
you review the diff before the old tree is deleted)." Stop.

## 1. Create the tree (only what's missing)

```
.re-discipline/
  CONVENTIONS.md      copy of <plugin-root>/templates/CONVENTIONS.md
  docs/  docs/ops/  active/  archive/
  golden.jsonl        empty file
  bin/re-search.exe   copy of <plugin-root>/bin/re-search.exe
  .gitignore          copy of <plugin-root>/templates/project-gitignore
```

Re-running init refreshes `CONVENTIONS.md`, `.gitignore`, and the exe
from the plugin (these three are plugin-owned copies); everything else
is user data — never touch it.

## 2. Shared memory decision

Read the policy line from the marker block in root `AGENTS.md` (the
canonical policy file). If no recorded decision exists, ask the user
once: "Use shared memory? Replaces Claude/Codex native memory with
`.re-discipline/docs/ops/` so all agents share the same recall.
[Recommended: yes]". Then converge:

- **enabled**: set `autoMemoryEnabled: false` in the project's
  `.claude/settings.json` (create the file if absent, preserving any
  existing keys). For Codex, apply the equivalent project-scoped memory
  switch if one exists; if none exists, the marker-block instruction is
  the mechanism.
- **disabled**: do NOT touch host memory settings at all — init only
  ever writes them when enabling.

## 3. Marker blocks

Render `<plugin-root>/templates/marker-block.md` with
`{{SHARED_MEMORY}}` → `enabled`/`disabled`, then apply to root
`AGENTS.md` and `.claude/CLAUDE.md`:

- markers absent → append the block (create the file if missing);
- exactly one well-formed `<!-- re-discipline:start -->` …
  `<!-- re-discipline:end -->` pair → replace its contents;
- anything else (unpaired, duplicated, nested) → leave that file
  untouched and report it for manual repair.

`AGENTS.md` is canonical: sync the `CLAUDE.md` block from it whenever
they disagree. Never modify anything outside the markers.

## 4. Report

List what was created, refreshed, skipped, and any files needing manual
repair. Do not commit unless the user asks.
