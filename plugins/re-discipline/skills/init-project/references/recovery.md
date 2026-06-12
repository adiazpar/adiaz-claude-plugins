# Recovery mode — a deleted/lost `project-profile.md`

The project was split-initialized (its `.claude/CLAUDE.md` carries the `<!-- re-discipline:laws`
markers + an `@project-profile.md` import) but `.claude/project-profile.md` is GONE. The laws still
load; the project's **identity** (mission, framing, source-of-record, tooling) has vanished from
context, and `dispatch.ps1` has fallen back to a generic framing. Restore the profile.

**Identity is not derivable from code.** Structure/tooling/source-of-record can be reconstructed
mechanically, but the *mission* and the neutral *framing* one-liner are declarations — only git or
the user can supply them faithfully. So: **git first, reconstruct (with user confirmation) only as a
fallback.** Never silently invent the identity — a wrong framing propagates into every agent prompt.

## 1. Confirm the state
`.claude/project-profile.md` absent AND `.claude/CLAUDE.md` has the laws marker / `@project-profile.md`
import. (If the laws marker is absent too, this is not recovery — it's drop-in or greenfield.)

## 2. git-restore (preferred — exact, lossless)
Check whether the file is in history:
```
git log --oneline -- .claude/project-profile.md
```
- If it was in the last commit: `git restore --source=HEAD --staged --worktree .claude/project-profile.md`
  (or `git checkout HEAD -- .claude/project-profile.md`).
- If it was deleted in an earlier commit: restore from the last commit that HAD it —
  `git checkout <that-commit> -- .claude/project-profile.md`.
Then show the restored frontmatter (`name`/`type`/`framing`) + Mission, and confirm with the user that
it's the right identity. **Done — this is the lossless path.** Verify Step 4.

## 3. Reconstruct (fallback — only if git has nothing: never committed, no git, or shallow/orphaned)
Rebuild the profile from the template (`${CLAUDE_PLUGIN_ROOT}/templates/project/project-profile.md`),
filling the **derivable** sections from project state:
- **Domain / Source-of-record / Tooling / Binaries & paths / Environment** ← read `docs/INDEX.md`,
  the public `README`, `docs/truth/INDEX.md` (subsystems → domain), `tools/` (tooling), the project's
  reference set, `git log` (history), and auto-memory (`MEMORY.md` + topic files often quote the
  framing/mission).
- **Identity (`name` / `type` / `framing` / Mission)** ← the part that can't be derived. Mine
  `docs/INDEX.md`'s mission line, the `README`, and memory for any verbatim framing; then **ASK the
  user (`AskUserQuestion`) to confirm or correct the mission + the neutral framing one-liner** before
  writing. Present your best-reconstructed draft so they're confirming, not authoring from scratch.
Write `.claude/project-profile.md`.

## 4. Verify
- `.claude/CLAUDE.md`'s `@project-profile.md` import now resolves (the file exists).
- `dispatch.ps1` reads a real `framing` again (no longer the generic fallback) — e.g. dry-run a dispatch
  and confirm the project framing appears.
- Report whether recovery was git-exact (Step 2) or reconstructed-with-confirmation (Step 3); if
  reconstructed, flag that the identity was user-confirmed, not byte-restored.
