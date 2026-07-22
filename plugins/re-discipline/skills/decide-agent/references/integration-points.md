# External Provider Integration Points

Promotion records an exact inverse for every point it touches. Fire replays
that manifest in reverse.

1. `agents/config.json`: add or remove one promoted provider. The user controls
   `backend`; reset it to `native` before removing the selected provider.
2. `agents/profiles/<provider>.md`: provider-specific prompt style and role fit.
3. Provider CLI configuration: only approved MCP/tool registrations and their
   exact config locations. Never store credentials in the repository.
4. `agents/README.md`: human-readable roster entry.
5. `agents/roster/<provider>/teardown-manifest.md`: exact additions, config
   keys, external paths, and reversal commands, plus a dated hiring record.

Do not edit root `AGENTS.md`, `.codex/AGENTS.md`, `.claude/CLAUDE.md`, or either
harness profile for roster membership. The root file remains the manager versus
drafter router, and `agents/config.json` remains the roster source of truth.

After promotion, parse config and run a sandboxed dispatcher dry run. After
reject or fire, search the repository and recorded external config locations
for the provider name. Explain any intentional historical record that remains.
