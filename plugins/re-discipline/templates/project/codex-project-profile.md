# {{PROJECT_NAME}} - Codex Overlay

This file contains Codex-specific operating guidance only. Canonical project
identity and domain facts live in `.re-discipline/project-profile.md`; do not
restate them here.

## Codex Configuration

{{CODEX_CONFIG}}

<!-- Project-local .codex/config.toml behavior, sandbox/approval notes, and
     trusted-workspace requirements. Use "no project-specific settings" when
     none are needed. -->

## MCP And Tool Surfaces

{{CODEX_TOOLS}}

<!-- Codex MCP server names, sanctioned commands, and any exclusive-resource
     rules. Point back to canonical tooling facts instead of duplicating them. -->

## Memory

{{CODEX_MEMORY}}

<!-- Codex native-memory policy and any bridge to an existing memory corpus.
     Memory is recall, never stronger than source, truth, or direct evidence. -->

## Local Paths

Machine-local values belong in `.codex/local-paths.md` and remain untracked.
Portable path schema belongs in the canonical profile or `docs/truth/`.

{{CODEX_LOCAL_PATHS}}
