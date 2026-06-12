---
name: decide-agent
description: This skill should be used when invoked as /decide-agent <name> promote|reject|fire, or when asked to "promote an agent", "hire this agent for real", "reject the candidate", "fire an agent", "remove an agent from the project", "add the agent to the team", "tear down an agent". Commits the hiring decision after hire-agent's interview - promote graduates a candidate into the team (touching config + instructions file + READMEs + CLAUDE.md + memory, recording a teardown manifest), reject cleanly discards a candidate, fire removes a previously-promoted member. The user makes the call; this skill executes it with no residuals.
argument-hint: <name> promote|reject|fire
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, AskUserQuestion
---

# Decide-agent — commit a hiring decision (promote | reject | fire)

The ratify half of the agent lifecycle. `hire-agent` interviewed a candidate and wrote a
scorecard; this skill commits the decision the **user** makes. Three verbs:

- **promote** — graduate a candidate (in `recruiting/<candidate>/`) into the team.
- **reject** — discard a candidate cleanly (pre-promotion).
- **fire** — remove a previously-promoted member (any time later).

`<name>` resolves by verb: for **promote/reject** it is a `recruiting/<candidate>/` dir; for
**fire** it is a `promoted: true` provider in `tools/agents/config.json`.

Promote is the ONLY action that touches the project outside `recruiting/`. Reject and fire are
manifest-driven so they leave **zero residuals**. Read the spec for rationale:
`superpowers/specs/2026-06-11-agent-hiring-lifecycle-design.md`.

## Hard rules

- **The user decides.** Always present the scorecard (promote/reject) or confirm the target
  (fire), and CONFIRM the decision with `AskUserQuestion` before executing — especially before
  any destructive teardown. Manager recommends; user ratifies (the Wall asymmetry).
- **No residuals.** Reject replays `rollback-manifest.md`; fire replays the `teardown-manifest`.
  After either, the repo + home-dir CLI config are back to the pre-integration state. The CLI
  stays installed (firing removes project integration, not the binary).
- **Promote records its own undo.** Promote writes a `teardown-manifest` listing every file it
  touched, so a future fire is a mechanical reverse. See `references/integration-points.md`.

## promote `<candidate>`

Precondition: `recruiting/<candidate>/scorecard.md` exists with a recommendation. Present it; get
the user's go-ahead. Then, for EACH integration point in `references/integration-points.md`:
1. Graduate `recruiting/<candidate>/config-draft.json`'s provider entry into
   `tools/agents/config.json` with `promoted: true`.
2. Materialize the provider's instructions file at repo root under its CLI's expected name
   (`AGENTS.md` for Codex, `GEMINI.md` for Gemini, …) = the shared contract content. If the file
   already exists (e.g. `AGENTS.md`), leave it; only create a missing provider-specific name.
3. Make the candidate's MCP registration permanent (promote it out of the rollback manifest).
4. Update `tools/agents/README.md`, the **`.claude/project-profile.md` Tooling roster line**, and
   project memory to mention the new member. (The roster lives in the profile, NOT in CLAUDE.md's
   generic laws block — never write domain/roster content into the `resync-laws`-replaceable laws.)
5. Write `tools/agents/roster/<provider>/teardown-manifest.md` recording every file + location
   touched above (the exact undo for fire), and a one-line hiring record.
6. Delete `recruiting/<candidate>/` (the interview scratch). The team membership now lives in
   config + the manifest.

## reject `<candidate>`

Precondition: a candidate in `recruiting/<candidate>/`. Confirm with the user. Then:
1. Replay `recruiting/<candidate>/rollback-manifest.md` — undo every out-of-dir change (remove
   the candidate's temp MCP registrations, any global config it wrote).
2. Delete `recruiting/<candidate>/`.
3. Leave the CLI installed. Verify nothing outside the dir changed (the manifest is the checklist).
   Project state == pre-onboard. No residuals.

## fire `<provider>`

Precondition: a `promoted: true` provider in `tools/agents/config.json`. Confirm with the user
(this removes a working team member). Then replay `tools/agents/roster/<provider>/teardown-manifest.md`
IN REVERSE:
1. Remove the provider entry from `tools/agents/config.json`.
2. Remove the provider's instructions file if it is provider-specific (e.g. `GEMINI.md`). Do NOT
   remove `AGENTS.md` if other providers still use it — check the roster first.
3. Remove the provider's MCP registration from its CLI config.
4. Revert the README / `.claude/project-profile.md` / memory mentions added at promote. The
   profile's roster line and the README roster section list ALL promoted providers — **edit them
   to remove only this provider's name**; delete the whole line/section only if this was the last
   member (same rule as `AGENTS.md` in step 2). Do NOT touch `CLAUDE.md`'s generic laws.
5. Archive a one-line "departed <date>" note; delete `tools/agents/roster/<provider>/`.
6. Leave the CLI installed on the machine. Verify no residual references remain
   (`grep` the provider name across `tools/agents/`, `CLAUDE.md`, `AGENTS.md`, README, memory).

## Additional resources

- **`references/integration-points.md`** — the canonical list of every file promote touches (and
  fire reverses), with what to write/remove at each. Keep promote, fire, and the teardown
  manifest in sync with this list.
- Provider config schema + dispatch mechanics: `tools/agents/README.md`.
- The interview that precedes this: the `hire-agent` skill.
