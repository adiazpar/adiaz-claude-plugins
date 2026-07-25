# Greenfield Initialization

Use this mode only when there is no meaningful existing project guidance to
reconcile.

## Gather The Project Facts

Ask one compact batch for:

- Project name and type.
- Accurate neutral one-line framing.
- Mission and long-term deliverable.
- Domain or subject under study.
- Authoritative source-of-record data, if any.
- Portable project tools and sanctioned invocations.
- Path/artifact schema and environment/build/test rules.

Do not ask the user to translate those answers into host settings. Inspect
existing manager, MCP, and shell configuration and derive project-owned host
notes. Ask only when a host-specific choice cannot be discovered safely.

## Seed The Structure

1. Create the directories listed in `templates/project/tree.txt`; add
   `.gitkeep` only where the project requires an empty tracked directory.
2. Render `project-profile.md` to
   `.re-discipline/project-profile.md` using the gathered facts.
3. Add `.re-discipline/local-paths.md` to the root `.gitignore`, preserving
   unrelated ignore rules. Keep `.claude/local-paths.md` and
   `.codex/local-paths.md` there as defense-only secret-path ignores, although
   neither legacy file may exist. Render `local-paths.md` beside the canonical
   profile, substituting discovered machine-local assignments. When no values
   are known, replace the placeholder with a commented instruction so the
   untracked signature still exists without inventing paths.
4. Render `agents-README.md`, `agents-config.json`, and `dispatch.ps1` to:
   - `.re-discipline/agents/README.md`
   - `.re-discipline/agents/config.json`
   - `.re-discipline/agents/dispatch.ps1`
5. Create `.re-discipline/agents/providers/` and
   `.re-discipline/agents/recruiting/`. Keep both empty and keep the backend
   `native`.
6. Render `CLAUDE.md` to `.claude/CLAUDE.md`. Add discovered Claude-only notes
   after the managed block.
7. Render `codex-AGENTS.md` to `.codex/AGENTS.md`. Add discovered Codex-only
   notes after the managed block.
8. Render `external-drafter-contract.md` to
   `.codex/external-drafter-contract.md`.
9. Render root `AGENTS.md` as the role router.
10. Render the three index templates under `docs/`.

Omit legitimately empty project-owned host sections. Do not copy canonical
laws, mission, or domain prose into a manager adapter.

## Verify

- The canonical profile is the only file declaring `framing` and shared laws.
- `.re-discipline/local-paths.md` exists, is ignored, is untracked, and has no
  unresolved template placeholder.
- Neither `.claude/local-paths.md` nor `.codex/local-paths.md` exists.
- The normalized agent core always exists and its JSON parses.
- The live config contains `backend: native` and an empty provider object.
- Claude contains exactly one canonical profile import.
- The root router and Codex manager chain resolve.
- Neither legacy host profile exists.
- The canonical profile is at most 240 lines and 16 KiB, or the report warns
  that it exceeds a limit without truncating it.
- All indexes and epistemic-status directories exist.

Report generated files and user-confirmed unknowns. Do not commit unless the
user asks.
