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
- Domain-specific roles created by those tools or live surfaces.
- Path/artifact schema and environment/build/test rules.

Do not ask the user to translate those answers into Claude or Codex settings.
Inspect existing `.claude/`, `.codex/`, MCP, and shell configuration and derive
project-owned manager notes. Ask only when a host-specific choice cannot be
discovered safely.

## Seed The Structure

1. Create the directories listed in `templates/project/tree.txt`; add
   `.gitkeep` only where an empty tracked directory is required.
2. Render `templates/project/project-profile.md` to
   `.re-discipline/project-profile.md` using the gathered facts.
3. Render `CLAUDE.md` to `.claude/CLAUDE.md`. Add discovered Claude-only notes
   after the managed block.
4. Render `codex-AGENTS.md` to `.codex/AGENTS.md`. Add discovered Codex-only
   notes after the managed block.
5. Render `external-drafter-contract.md` to
   `.codex/external-drafter-contract.md`.
6. Render root `AGENTS.md` as the role router. Always create the router even
   when the optional external-agent CLI framework is not requested; Codex
   needs the direct-manager route.
7. Render the three index templates under `docs/`.
8. If the user wants external-provider dispatch, render `agents-config.json`,
   `agents-README.md`, and `dispatch.ps1` into `agents/`, then create
   `agents/profiles/`, `agents/roster/`, and `agents/benchmarks/`. Keep
   `backend` set to `native` until a provider passes `hire-agent` and the user
   promotes it.

Omit legitimately empty project-owned host sections. Do not copy canonical
laws, mission, or domain prose into a manager adapter just to make it longer.

## Verify

- The canonical profile is the only file declaring `framing`.
- The canonical profile is the only file containing the shared-law block.
- Claude contains exactly one canonical profile import.
- The root router and Codex manager chain resolve.
- Neither `.claude/project-profile.md` nor `.codex/project-profile.md` exists.
- Project-owned manager notes contain only host-specific guidance.
- The canonical profile is at most 240 lines and 16 KiB, or the report warns
  that it exceeds a limit without truncating it.
- All indexes and epistemic-status directories exist.

Report generated files and any user-confirmed unknowns. Do not commit unless
the user asks.
