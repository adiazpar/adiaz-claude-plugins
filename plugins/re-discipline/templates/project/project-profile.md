---
# Machine-readable project identity. Dispatchers read `framing` from here.
# This is the single source for project facts; harness overlays must not repeat it.
name: "{{PROJECT_NAME}}"
type: "{{PROJECT_TYPE}}"
framing: "{{ONE_LINE_FRAMING}}"
---

# {{PROJECT_NAME}} - Canonical Project Profile

Both `.claude/CLAUDE.md` and `.codex/AGENTS.md` load this profile. It contains
project facts only. Claude- or Codex-specific settings belong in their
respective `project-profile.md` overlays.

## Mission

{{MISSION}}

## Domain

{{DOMAIN_DESCRIPTION}}

## Source Of Record

{{SOURCE_OF_RECORD}}

<!-- Name authoritative schemas, declarations, specifications, or primary
     artifacts and state exactly what each can prove. Use "none identified"
     when the subject defines no authoritative data set. -->

## Tooling

{{TOOLING}}

<!-- Record portable tool locations and sanctioned invocations. Host-specific
     MCP names or permission settings belong in the harness overlays. -->

## Roles

{{DOMAIN_ROLES}}

<!-- Generic roles are Orchestrator, Analyst, Mechanical fan-out, and
     Synthesizer. Add only roles created by this project's actual apparatus. -->

## Paths And Artifacts

{{BINARIES_AND_PATHS}}

<!-- Record path schema, tracked artifact locations, and re-verification
     triggers. Machine-local values stay in harness-local untracked files. -->

## Environment

{{ENVIRONMENT}}

<!-- Shell, test, build, and commit mechanics that are true for the project,
     independent of which agent harness is active. -->

## Wall Example

{{WALL_EXAMPLE}}

<!-- Optional concrete example showing what two evidence sources each attest. -->
