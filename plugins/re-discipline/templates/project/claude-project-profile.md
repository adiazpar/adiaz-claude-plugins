# {{PROJECT_NAME}} - Claude Code Overlay

This file contains Claude Code-specific operating guidance only. Canonical
project identity and domain facts live in `.re-discipline/project-profile.md`;
do not restate them here.

## Claude Code Configuration

{{CLAUDE_CONFIG}}

<!-- Project-local settings, permissions, and Claude-only tool behavior. Use
     "no project-specific settings" when none are needed. -->

## MCP And Tool Surfaces

{{CLAUDE_TOOLS}}

<!-- Claude MCP server names and host-specific invocation notes. Point back to
     canonical tooling facts instead of duplicating them. -->

## Memory

{{CLAUDE_MEMORY}}

<!-- Claude auto-memory location or policy when relevant. Memory is recall,
     never stronger than source, truth, or direct evidence. -->

## Local Paths

Machine-local values belong in `.claude/local-paths.md` and remain untracked.
Portable path schema belongs in the canonical profile or `docs/truth/`.

{{CLAUDE_LOCAL_PATHS}}
