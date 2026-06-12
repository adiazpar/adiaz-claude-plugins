---
name: hire-agent
description: This skill should be used when invoked as /hire-agent <candidate> "<capability target>", or when asked to "hire an agent", "onboard a new AI agent", "interview an agent", "add Gemini/Qwen/<provider> as an agent", "evaluate an external agent for the team". Scaffolds an ISOLATED recruiting/<candidate>/ workspace, researches + installs the candidate CLI, drafts its provider config, registers its MCP servers, runs the golden interview battery, and writes a scorecard benchmarked against the whole agent team. Drafts a recommendation only — committing the hire is the decide-agent skill.
argument-hint: <candidate> "<capability target>"
allowed-tools: Read, Write, Glob, Grep, Bash, WebSearch, WebFetch, AskUserQuestion
---

# Hire-agent — onboard + interview a candidate AI agent

Evaluate a new AI coding agent (Codex, Gemini, a local Qwen runner, …) as a reverse-engineering
**drafter** for snaphak-re, applying the project's draft/ratify discipline to agents themselves.
This skill does ALL the work in an isolated `recruiting/<candidate>/` dir and produces a
scorecard + recommendation. It NEVER promotes — that is `decide-agent`. The asymmetry is the
same as the Wall: this skill drafts; the user (via decide-agent) ratifies the hire.

Read the design spec for full rationale: `superpowers/specs/2026-06-11-agent-hiring-lifecycle-design.md`.

## Hard rules

- **ISOLATION.** During this skill, touch ONLY `recruiting/<candidate>/` in the repo. Every
  change made OUTSIDE it (a home-dir CLI config write, an MCP registration) MUST be appended to
  `recruiting/<candidate>/rollback-manifest.md` so a later reject is a clean replay. Do NOT
  touch `tools/agents/config.json`, `AGENTS.md`/`GEMINI.md`, READMEs, `CLAUDE.md`, or memory —
  those are promote-only (handled by `decide-agent`).
  - **Two intended exceptions** (durable team infra, NOT candidate scratch — do NOT log these to
    the rollback-manifest; they survive reject): writing fresh runs into the baseline store
    `tools/agents/benchmarks/<task>/` (per freshness policy (c)), and the transient interview
    scratch campaign `active/agent-interviews/` that the dispatcher requires (Step 6). Both are
    interview machinery shared across hires, not part of this candidate's footprint.
- **DRAFTS ONLY.** Produce a scorecard + recommendation. Never mark a provider `promoted`.
- **User-handled steps.** Install a missing CLI yourself (automatable). If authentication is
  required, PAUSE and tell the user to authenticate on their device — never attempt auth.
- **Ask when genuinely unsure** (`AskUserQuestion`): an ambiguous capability target; which CLI
  or model tier to evaluate when several exist; whether to install a missing CLI; how broad an
  MCP grant to give for the interview; an unverifiable flag where docs disagree. Do not over-ask
  on the obvious (clear target, one obvious CLI).

## Procedure

### 1. Scaffold the isolated workspace
Create `recruiting/<candidate>/` with `interview/`, and seed `CANDIDATE.md` (dossier: provider
name, CLI, capability target, install/auth status) and an empty `rollback-manifest.md`.

### 2. Research the CLI
Discover how to drive the candidate non-interactively. Follow `references/research-checklist.md`
— use the web tool (WebSearch/WebFetch) for the CLI's docs AND verify from `--help` after
install; never trust training data for flag syntax. If no web tool is available, fall back to
`<cli> --help` as the authoritative source and flag any flag you could NOT verify in
`CANDIDATE.md`. Record findings in `CANDIDATE.md`.

### 3. Install (if missing); pause for auth
Install the CLI if absent. If it needs login/credentials, STOP and tell the user the exact auth
command to run on their device. Resume after they confirm.

### 4. Draft the provider config
Write `recruiting/<candidate>/config-draft.json` — a full `config.json`-shaped file containing
the candidate's provider entry so `dispatch.ps1 -ConfigPath` can use it. Fill from research:
`command`, `args` template (tokens `{model_args} {root} {policy_args} {lastmsg} {prompt}`),
`model_flag`, `bypass_args` (the candidate's skip-permissions flag), `bypass_default: true`,
`instructions_file`, `sandbox_args`. See `references/research-checklist.md` for the field map.

### 5. Register the candidate's MCP servers
Register `snaphak-daemon` + `ghidra` in the candidate CLI's own MCP config format/location
(NOT Codex's TOML unless it is Codex). **Use `tools/agents/setup_codex_mcp.ps1` as the worked
example** of what a registration must write (server names, command, args, and the long
tool-timeout the daemon's `test_rawmap` needs); adapt it to the candidate CLI's format/location
discovered in research item 8. Append every file/location written to `rollback-manifest.md`.

### 6. Run the interview battery
`dispatch.ps1` is campaign-scoped: it requires an existing `active/<slug>/` and writes the report
to `active/<slug>/subagents/<provider>-<name>/report.md`. Use a standing interview scratch
campaign — ensure `active/agent-interviews/` exists (scaffold a minimal `CAMPAIGN.md` if absent;
this is interview infra, an isolation exception, not candidate scratch). For each task in
`fixtures/`, copy its `brief.md` into `recruiting/<candidate>/interview/<task>/`, then dispatch:
`tools/agents/dispatch.ps1 -Provider <candidate> -ConfigPath recruiting/<candidate>/config-draft.json -Slug agent-interviews -Name interview-<task>`
The live roster (`tools/agents/config.json`) is never touched. After each run, **copy the report
back** from `active/agent-interviews/subagents/<candidate>-interview-<task>/report.md` into
`recruiting/<candidate>/interview/<task>/report.md` (so the candidate's footprint is self-contained),
then clear that campaign subdir. Grant the live surface for T2/T3 (bypass is the default).

### 7. Score against the team + recommend
Grade each report per `references/scoring-rubric.md` (T1/T2) and `references/mini-campaign-grading.md`
(T3). Benchmark **against the whole team** using the baseline store `tools/agents/benchmarks/<task>/`:
compare to every promoted member's scores AND the native Claude anchor, framed relative to the team
distribution. Apply **freshness policy (c)** — re-run external incumbents fresh on any stale/changed
task; cache the native Claude anchor (re-run only on tier/fixture change, since that bills the user's
Claude plan). If `tools/agents/benchmarks/` is unseeded (first run), seed it first per its README
(run the current team — codex + the Claude anchor — through the battery once) so there is a team
to compare against. Write `recruiting/<candidate>/scorecard.md` with a clear hire / no-hire
recommendation + reasoning, and update the baseline store with the runs. STOP — hand to `decide-agent`.

## The interview battery — tiered (`fixtures/`)

**Cheap gate (objective, fast — screens out the obviously-unfit):**
- **T1 decompile-analysis** (`fixtures/T1-decompile/`) — a stored Ghidra decompile + answer key.
  Tests RE accuracy AND evidence honesty (the input cannot reveal certain facts; a good agent flags
  that rather than fabricating).
- **T2 MCP reach-for-it** (`fixtures/T2-mcp-reach/`) — "report live game state"; a passing agent
  calls the daemon MCP rather than guessing. Requires the daemon up (`game_state` to check).

**Deciding test (representative, heavier — run ONLY on candidates that clear the gate):**
- **T3 mini-campaign** (`fixtures/T3-mini-campaign/`) — the production loop in miniature on a
  PROVEN, closed result (the answer withheld; candidate scoped away from `docs/truth/`/chronicles).
  The candidate drafts; a **constant Claude tier** then runs the real `review-subagent` triage over
  the draft; graded on whether the manager reaches the proven truth FROM THAT DRAFT (with honesty
  that prevents a wrong promotion). Needs live tools (single-consumer + bypass). It carries the
  weight in the recommendation. Method: `references/mini-campaign-grading.md`.

T3 is the most token-expensive task — gate on T1/T2 first. Add a T3 honesty-trap variant + a
reference-first T4 as the battery matures.

## Additional resources

- **`references/research-checklist.md`** — what to discover per CLI (flags, MCP format, auth) → config fields.
- **`references/scoring-rubric.md`** — the T1/T2 scoring dimensions + the against-the-team benchmarking method.
- **`references/mini-campaign-grading.md`** — the T3 in-the-loop (drafter→fixed-manager-ratify) grading method.
- **`fixtures/T1-decompile/`**, **`fixtures/T2-mcp-reach/`**, **`fixtures/T3-mini-campaign/`** — the golden tasks + answer keys.
- **`tools/agents/benchmarks/`** — the durable per-member baseline store (freshness policy (c)).
- Dispatch mechanics + provider config schema: `tools/agents/README.md`.
- The external-drafter contract candidates operate under: `AGENTS.md`.
- Commit the decision after this skill: the **`decide-agent`** skill (promote | reject | fire).
