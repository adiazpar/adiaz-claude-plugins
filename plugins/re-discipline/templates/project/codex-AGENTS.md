<!-- re-discipline:codex-adapter v0.6.0 -->
# AGENTS.md - Codex Manager Adapter for {{PROJECT_NAME}}

Codex loads this host adapter. The canonical
`.re-discipline/project-profile.md` is the single source of shared project
facts and re-discipline laws.

A trusted re-discipline `SessionStart` hook injects that complete profile.
Because `AGENTS.md` has no native include directive, explicitly read the
profile when hooks are disabled, untrusted, or unavailable. Then follow its
Session Start contract and use `$re-discipline:onboard` when available.

`.re-discipline/config.json` selects the project memory mode. New projects use
`shared-only`, represented by disabled memories in the preserved project
`.codex/config.toml`; this does not modify Codex's machine-local memory
directory.

Project-owned Codex-specific notes may follow this managed block.
<!-- re-discipline:codex-adapter:end -->
