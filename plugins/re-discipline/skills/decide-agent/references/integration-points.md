# Integration points — every file promote touches (and fire reverses)

This is the canonical checklist of what becomes part of the project when an agent is promoted.
`promote` writes each; `fire` removes each; the per-provider `teardown-manifest` records exactly
what was done so fire is mechanical. Keep all three in sync with this list. (Derived from the
manual Codex integration on 2026-06-11 — that is the worked example.)

## The points

1. **`tools/agents/config.json`** — add the provider entry (from the candidate's
   `config-draft.json`) with `promoted: true`. *Fire:* remove the entry.

2. **Provider instructions file at repo root** — the shared external-drafter contract
   materialized under the CLI's expected filename: `AGENTS.md` (Codex and most CLIs), `GEMINI.md`
   (Gemini), etc. If a shared `AGENTS.md` already exists and the new CLI reads it, no new file is
   needed. *Fire:* remove ONLY a provider-specific file (e.g. `GEMINI.md`); never remove
   `AGENTS.md` while another promoted provider still uses it (check the roster).

3. **Agent profile** — copy `recruiting/<candidate>/profile-draft.md` to
   `tools/agents/profiles/<provider>.md` (the per-model prompt-style + `role-fit` overlay), and add a
   `"profile": "profiles/<provider>.md"` pointer to the provider's entry in `config.json` (point 1).
   The dispatcher (`dispatch.ps1`) reads that pointer and PREPENDS the profile's "How to prompt this
   model" section to the brief — so the shared `AGENTS.md` contract stays model-agnostic while each
   agent still gets its model-true prompting. *Fire:* delete `profiles/<provider>.md` + the pointer.

4. **MCP registration** — make the candidate's daemon + ghidra MCP registration permanent in the
   CLI's own config (Codex: `~/.codex/config.toml` via `tools/agents/setup_codex_mcp.ps1`;
   others: their format). *Fire:* remove those registrations.

5. **`tools/agents/README.md`** — note the provider in the providers/roster section. *Fire:* drop
   the note.

6. **`.claude/project-profile.md` (Tooling roster line)** — the roster of promoted external agents
   lives in the PROFILE (domain), never in `CLAUDE.md`'s generic laws (the laws are
   `resync-laws`-replaceable; writing domain there would be lost + breaks the boundary). *Fire:*
   the roster line lists ALL promoted providers — EDIT it to remove only this provider's name;
   delete the whole line only if this was the last member (same caution as `AGENTS.md` in point 2).

7. **Project memory** — a one-line `MEMORY.md` pointer + any provider fact file (mirrors the
   Codex bypass-policy memory). *Fire:* remove the pointer + fact file.

8. **`tools/agents/roster/<provider>/`** — created by promote: the `teardown-manifest.md` (the
   exact undo for 1–7) + a one-line hiring record. *Fire:* delete this dir last, after replaying it.

## Manifest format (teardown-manifest.md)

For each point touched, record: the file/location, what was added (so fire knows what to remove),
and any value needed to reverse it (e.g. the exact config-entry key, the instructions filename,
the MCP server names registered). The manifest is the single source of truth for a clean fire —
if a future promote touches a new place, add it here AND to the manifest it writes.

## Verification (both promote and fire)

After the action, `grep` the provider name across `tools/agents/`, `CLAUDE.md`, `AGENTS.md`/the
provider instructions file, `tools/agents/README.md`, and the memory dir. Promote: expect it
present in 1–7. Fire: expect ZERO matches (no residuals).
