# Greenfield mode — ask, then seed

A fresh repo with no re-discipline structure. There is nothing to infer from, so gather the identity
up front, then seed everything from templates.

## 1. Clarifying questions (a short batch, up front)

Ask via `AskUserQuestion` (batch the related ones; don't drip). Needed to fill the profile:
- **Project name** (the `name`).
- **Project type** (`type`: reverse-engineering | research | library | app | …).
- **One-line framing** (`framing`) — the accurate, neutral description injected into every agent prompt.
  Get this right; it's the §10 framing and the single source for it.
- **Mission** — one or two sentences; the long-term deliverable.
- **Source-of-record** — does the subject define its own authoritative data (schemas/decls/specs)? Where?
- **Tooling** — any project tools/harnesses to register, and the sanctioned way to run each.
- **Agent framework?** — include `agents/` + `AGENTS.md` for external drafter agents, or skip.

Anything the user can't answer yet → leave a clearly-marked `TODO` in the profile (do not block).

## 2. Seed the structure

1. Create the dir tree from `${CLAUDE_PLUGIN_ROOT}/templates/project/tree.txt` (one dir per line; add a
   `.gitkeep` to empty dirs so they're tracked).
2. Write `.claude/CLAUDE.md` from the template, filling `{{PROJECT_NAME}}`. Keep the `@project-profile.md`
   import and the `<!-- re-discipline:laws -->` markers intact.
3. Write `.claude/project-profile.md` from the template — fill the frontmatter (`name`/`type`/`framing`)
   and the body sections from the answers; `TODO`-mark the rest. Seed `{{DOMAIN_ROLES}}` from the project
   type + tooling (RE project → scope Analyst as *RE-analyst*; a live oracle/daemon in tooling → a
   *live-tester*; visual/rendered output → a *vision-reader*; else "none beyond the generic four"). The
   generic roles stay in CLAUDE.md §10; only domain roles go here.
4. Write `docs/INDEX.md`, `docs/truth/INDEX.md`, `docs/history/INDEX.md` from the templates (fill
   `{{PROJECT_NAME}}` / `{{ONE_LINE_FRAMING}}`).
5. If the agent framework was requested, set up the top-level `agents/` dir and write `AGENTS.md` from
   `${CLAUDE_PLUGIN_ROOT}/templates/project/AGENTS.md` (the canonical shared drafter contract — fill
   `{{PROJECT_NAME}}`, `{{PROJECT_TOOLING_RULES}}`, `{{PROJECT_LIVE_SURFACES}}` from the profile; the
   report-format section ships filled — do NOT rewrite it). Create `agents/profiles/` (empty until the
   first hire) — per-agent prompt-style + role-fit overlays live there, materialized by `decide-agent`
   from the `agent-profile.md` template. (`agents/` is a first-class top-level dir, NOT under `tools/` —
   the agent team is not "tooling code"; `tools/` exists only if the project has its own code tooling.)
   Otherwise skip the whole framework.
6. Note that auto-memory is harness-managed (lives outside the repo at `~/.claude/projects/<proj>/memory/`)
   — do NOT seed memory content; it is earned per-project.

## 3. Verify + report

Confirm CLAUDE.md imports the profile, the tree + INDEX files exist, and the framing lives only in the
profile (referenced elsewhere, never restated). Report what was created and the `TODO`s the user should
fill. Re-running is safe (Step 0 detects the profile and stops).
