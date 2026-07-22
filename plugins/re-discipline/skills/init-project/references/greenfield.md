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
the overlays. Ask only when a host-specific choice cannot be discovered safely.

## Seed The Structure

1. Create the directories listed in `templates/project/tree.txt`; add
   `.gitkeep` only where an empty tracked directory is required.
2. Render `templates/project/project-profile.md` to
   `.re-discipline/project-profile.md` using the gathered facts.
3. Render `CLAUDE.md` and `claude-project-profile.md` into `.claude/`.
4. Render `codex-AGENTS.md` and `codex-project-profile.md` into `.codex/`.
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

Use `none` or `not configured` for legitimately empty overlay sections. Do not
copy canonical mission or domain prose into an overlay just to make it longer.

## Verify

- The canonical profile is the only file declaring `framing`.
- Claude imports the canonical profile and Claude overlay.
- The root router and Codex manager chain resolve.
- Both overlays contain only harness-specific guidance.
- All indexes and epistemic-status directories exist.

Report generated files and any user-confirmed unknowns. Do not commit unless
the user asks.
