# Drop-in mode — explore, then confirm, then refactor + normalize

An existing hand-made project (it already has a populated `CLAUDE.md` and/or `docs/` tree). The job:
adopt the structure non-destructively AND refactor the existing instructions into the laws/profile split,
losing nothing.

## 1. Exploration phase (Claude works; no questions yet)

Read the existing `.claude/CLAUDE.md` (or `./CLAUDE.md`), `docs/`, the dir tree, `AGENTS.md` if present,
and any READMEs. Build a classification: for each chunk of the existing CLAUDE.md, tag it
**generic-law** (methodology that matches the template) or **project-domain** (mission, the subject, the
source-of-record set, tooling, binaries/paths, environment/shell, domain examples, the framing text).
List what scaffolding already exists vs is missing.

## 2. Propose a reconciliation plan (then get confirmation)

Present: (a) the missing scaffolding to add (dirs, INDEX files) — non-destructive; (b) the CLAUDE.md
refactor — "extract these domain chunks into `.claude/project-profile.md`; replace the methodology with
the generic laws template (filled with the project name); CLAUDE.md `@import`s the profile"; (c) the
normalization targets (files that restate the framing). Show the planned `.claude/project-profile.md`
content and the diff. **Confirm with `AskUserQuestion` before the refactor/normalization writes** — and
ask only the genuine ambiguities exploration couldn't resolve (a chunk that's half-law/half-domain; a
non-standard dir to keep-or-fold).

## 3. Apply

1. **Add missing scaffolding** non-destructively (create absent dirs from `tree.txt`; write any missing
   INDEX file from template — never overwrite an existing populated one without confirmation).
2. **Write `.claude/project-profile.md`**: frontmatter (`name`/`type`/`framing` — the framing is the
   neutral one-liner extracted from the existing CLAUDE.md) + body (the extracted domain chunks).
3. **Rewrite `.claude/CLAUDE.md`**: the generic laws template (filled `{{PROJECT_NAME}}`) + the
   `@project-profile.md` import. Preserve any genuinely project-specific *law-adjacent* note by moving it
   to the profile, not by bloating the laws.

## 4. Normalize restatements (the single-source pass)

`grep` the repo for the old framing / "this is a … of <subject>" restatements. For each (EXCLUDING
`active/<slug>/` campaign scratch — never touch it):
- **Claude-loaded doc** (INDEX, internal READMEs) → replace with a pointer to the profile. (Exception:
  the public/root `README.md` is the human-facing project front page — it may keep a self-contained
  mission/framing for GitHub visitors; just ensure it doesn't contradict the profile.)
- **Generated prompt** (`tools/agents/dispatch.ps1`, brief templates) → make it read `framing` from the
  profile frontmatter and inject it; remove the hardcoded string.
- **External-agent contract** (`AGENTS.md`) → genericize its opening to rely on the dispatch-injected
  framing; keep only genuinely project-specific content (live surfaces), pointing at the profile.

## 5. Verify (the meaning-preserving gate) + report

- **`(laws ∪ profile) ≡ original`**: confirm every chunk of the original CLAUDE.md now lives in either the
  laws or the profile — nothing dropped, nothing silently changed in meaning. Show this check.
- `grep` confirms the framing/mission now appears in the profile and is referenced (not duplicated)
  everywhere else outside `active/`.
- CLAUDE.md imports `@project-profile.md`; the tree + INDEX files exist.
- Report the restatements normalized and any left (with reason). Commit as its own commit (the user
  decides); never fold this into unrelated campaign commits.
