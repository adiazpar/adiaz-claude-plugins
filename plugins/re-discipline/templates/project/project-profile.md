---
# Machine-readable identity (tools parse this frontmatter; e.g. dispatch.ps1 reads `framing`).
# This is the SINGLE SOURCE for the project's identity — everything else points/pulls from here.
name: "{{PROJECT_NAME}}"
type: "{{PROJECT_TYPE}}"            # e.g. reverse-engineering | research | library | app
framing: "{{ONE_LINE_FRAMING}}"    # the accurate, neutral one-liner injected into EVERY agent prompt
---

# {{PROJECT_NAME}} — Project Profile (the domain single-source)

> Imported by `.claude/CLAUDE.md` (always loaded). The generic *laws* live in CLAUDE.md; everything
> **specific to this project** lives here. Change the project → change this file; nothing else restates it.

## Mission

{{MISSION}}

<!-- One or two sentences: what this project is and its long-term deliverable. -->

## Domain / what's under study

{{DOMAIN_DESCRIPTION}}

<!-- What system/codebase/subject is being investigated; the key subsystems; anything an agent needs to
     orient. Keep it factual and neutral. -->

## Source-of-record (check FIRST for source-defined facts — CLAUDE.md §9)

{{SOURCE_OF_RECORD}}

<!-- The authoritative declared-data set for this project (schemas, decls, specs the subject itself
     defines). Path(s) + what each contains. This is the §9 "first stop". -->

## Tooling

{{TOOLING}}

<!-- The project's own tools: where they live, how to invoke them, the one sanctioned way to run each.
     e.g. analysis harnesses, daemons, oracles. -->

## Roles (domain-specific)

{{DOMAIN_ROLES}}

<!-- The GENERIC roles (Orchestrator / Analyst / Mechanical-fan-out / Synthesizer) live in CLAUDE.md §10.
     Declare HERE the roles born from THIS project's apparatus — each: the role, what it does, and which
     agent/tier fits it. The orchestrator staffs from the generic roles PLUS these. Examples by apparatus:
       - a live oracle / daemon  -> a *live-tester* role (drive it, classify verdicts; single-live-consumer)
       - visual / rendered output -> a *vision-reader* role (read a captured screenshot -> structured verdict)
       - reverse-engineering      -> scope *Analyst* as an *RE-analyst* (decompile / schema / trace)
     Add a role when you build the capability that needs it; "none beyond the generic four" is a valid value. -->

## Binaries & paths

{{BINARIES_AND_PATHS}}

<!-- Path SCHEMA location; path VALUES live in .claude/local-paths.md (gitignored). Build-state notes,
     re-verification triggers, the "never hardcode paths" reminder's project specifics. -->

## Environment (shell, commit mechanics)

{{ENVIRONMENT}}

<!-- Project-machine specifics: shell (PowerShell/bash quirks), the multi-line commit workflow, any
     environment gotchas. These are environment-specific, not generic laws. -->

## Worked example — the Wall, by provenance

{{WALL_EXAMPLE}}

<!-- A concrete past instance where DIRECT-vs-DIRECT provenance mattered (illustrates CLAUDE.md §5).
     Optional but valuable; keep one good example. -->
