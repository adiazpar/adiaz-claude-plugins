---
name: init-project
description: This skill should be used when invoked as /init-project, or when asked to "set up a new project with re-discipline", "initialize the re-discipline structure", "scaffold the truth/active/history tree", "drop re-discipline into this repo", "restructure this project to follow the re-discipline laws", "resync the laws", "recover/restore a deleted project-profile.md", or "my project profile is missing". Detects greenfield vs drop-in vs recovery, scaffolds the dir tree + masterfiles from the plugin templates, splits generic laws (CLAUDE.md) from project domain (.claude/project-profile.md), normalizes restatements so the project identity is declared once, and self-heals a missing profile (git-restore first). Idempotent.
argument-hint: [resync-laws]
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, AskUserQuestion
---

# Init-project — scaffold (or adopt) the re-discipline structure

Make any repo follow the re-discipline laws: the directory-as-epistemic-status tree, the masterfiles,
and the **generic-laws / project-domain split**. The generic methodology lives in the plugin templates
and is seeded into `.claude/CLAUDE.md`; everything project-specific lives in `.claude/project-profile.md`
(imported by CLAUDE.md, so always loaded). The project's identity is declared **once** (the profile) and
everything else points or pulls — no file restates the mission.

Templates: `${CLAUDE_PLUGIN_ROOT}/templates/project/` (CLAUDE.md laws, project-profile.md, the 3 INDEX
skeletons, tree.txt). The snaphak-re repo is itself the reference example of the result (its
`.claude/CLAUDE.md` = generic laws + `@project-profile.md`).

## Step 0: Detect the mode (idempotent)

- If `.claude/project-profile.md` exists → already initialized. Stop, report, and offer `resync-laws`.
- Else if `.claude/CLAUDE.md` carries the laws markers (`<!-- re-discipline:laws`) or an
  `@project-profile.md` import — i.e. the project WAS split-initialized but the profile is now GONE →
  **recovery mode** (someone deleted/lost `project-profile.md`). Follow `references/recovery.md`
  (git-restore first; reconstruct only as a fallback). Do NOT treat this as drop-in.
- Else if `docs/INDEX.md` OR a populated `.claude/CLAUDE.md` (or `./CLAUDE.md`) exists → **drop-in mode**
  (an existing hand-made project that was never adopted). Follow `references/dropin.md`.
- Else → **greenfield mode** (fresh repo). Follow `references/greenfield.md`.

The discriminator between recovery and drop-in is the **laws marker**: its presence proves the project
was already split-initialized, so a missing profile is a deletion to *recover*, not a hand-made repo to
*adopt*. State which mode you detected before proceeding.

## The two modes (summary; full procedure in references/)

- **Greenfield** = *ask, then seed.* There is nothing to infer from, so ASK a short batch of clarifying
  questions (name, type, one-line framing, mission, source-of-record, tooling, agent-framework?) via
  `AskUserQuestion`, then seed the tree + all files from templates, filling the profile from the answers.
  Unanswered fields become clearly-marked `TODO` placeholders. See `references/greenfield.md`.
- **Drop-in** = *explore, then confirm.* Run an exploration phase (read the existing CLAUDE.md, docs, tree)
  and CLASSIFY each chunk as generic-law vs project-domain. Propose a reconciliation plan. Then, with the
  user's confirmation, non-destructively add missing scaffolding AND refactor the existing CLAUDE.md into
  laws + profile, **verifying the split loses nothing** (`(laws ∪ profile) ≡ original`). See `references/dropin.md`.

Ask clarifying questions only where a wrong assumption would waste effort or write the wrong content —
greenfield asks more (to gather), drop-in asks less (to confirm). Do not over-ask the obvious.

## Normalization (both modes) — the single-source rule

The project identity (mission, the neutral `framing` one-liner) is declared once in
`.claude/project-profile.md` frontmatter + body. Everything else must POINT or PULL, never restate:
- **Claude-loaded docs** (CLAUDE.md, the INDEX files) → static reference (CLAUDE.md `@import`s the profile;
  INDEX mission line points at it).
- **Generated prompts** (`agents/dispatch.ps1` bootstrap, delegate/interview briefs) → runtime
  injection: the orchestrator/tool reads `framing` from the profile frontmatter and injects the literal
  text. `dispatch.ps1` already does this — do NOT re-hardcode a project string in a generated prompt.

In drop-in, hunt existing restatements (`grep` the repo for the old framing) and convert each to a
reference or injection — but NEVER touch `active/<slug>/` campaign scratch (provisional; own lifecycle).

## resync-laws

When invoked as `/init-project resync-laws`: replace the content between the
`<!-- re-discipline:laws ... -->` / `<!-- re-discipline:laws:end -->` markers in `.claude/CLAUDE.md` with
the current `${CLAUDE_PLUGIN_ROOT}/templates/project/CLAUDE.md` laws (re-filling `{{PROJECT_NAME}}` from
the profile), preserving the `@import` line and leaving `.claude/project-profile.md` untouched. Show a diff
and confirm before writing. This is how a project picks up plugin law updates without losing its domain.

## Verification (always)

After any mode: confirm `.claude/CLAUDE.md` imports `@project-profile.md`; the tree exists; the three
INDEX files are present; `grep` confirms the framing/mission appears in the profile and is referenced —
not duplicated — elsewhere. In drop-in, show the `(laws ∪ profile) ≡ original` check before committing.

## Additional resources

- **`references/greenfield.md`** — the new-project procedure (questions → seed).
- **`references/dropin.md`** — the existing-project procedure (explore → confirm → refactor → normalize → verify).
- **`references/recovery.md`** — the self-heal procedure when `project-profile.md` was deleted (git-restore → reconstruct).
- Templates: `${CLAUDE_PLUGIN_ROOT}/templates/project/`.
