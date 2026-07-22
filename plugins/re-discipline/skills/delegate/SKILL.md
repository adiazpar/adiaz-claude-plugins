---
name: delegate
description: >-
  Delegate a focused investigation into an active re-discipline campaign.
  Creates an isolated drafter workspace and brief, uses the active host's
  native Claude Code or Codex adapter unless an external provider was
  explicitly selected, and requires a report for manager review.
---

# Delegate Campaign Work

Subagents draft; the manager ratifies. Every adapter must produce the same
reviewable report and must keep the drafter away from durable truth.

## Step 1: Validate And Select The Route

Require:

- an existing `active/<slug>/CAMPAIGN.md`;
- a unique lowercase kebab-case `<name>`;
- a concrete objective with an observable deliverable or answer.

Select the route before naming the workspace:

1. An external provider explicitly named by the user wins.
2. An explicit non-`native` backend in `agents/config.json` is a project-level
   user selection.
3. Otherwise use the current host's native adapter.

For a legacy `backend: claude`, treat it as native only in Claude Code. From
Codex, ask before interpreting it as an external Claude CLI route. Never move
work between providers solely to save tokens or because one appears stronger.

Use `<name>` for native work and `<provider>-<name>` for external work. The
resulting workspace is `active/<slug>/subagents/<dispatch-name>/`.

## Step 2: Create The Drafter Workspace

Create `scripts/`, `analysis/`, `artifacts/`, and `evidence/` under the
workspace. Render
`<plugin-root>/templates/project/drafter-AGENTS-override.md` to
`AGENTS.override.md`. This gives external Codex-compatible CLIs a closer
drafter contract when they run with this directory as their working directory.

Do not overwrite an existing workspace.

Do not commit unless the user explicitly asks.

## Step 3: Gather Only Relevant Context

Read the campaign and choose:

- truth files the drafter needs;
- chronicles containing dead ends it must not retry;
- primary artifacts and sanctioned tools;
- a time or effort budget;
- any relevant memory facts that are not already in checked-in guidance.

Memory is recall, not authority. Quote only the few useful facts in the brief;
do not assume a subagent can access the manager's memory store.

## Step 4: Write `brief.md`

Lead with the canonical `framing` value from
`.re-discipline/project-profile.md`, then include:

```text
# Dispatch Brief - <dispatch-name>

Project: <name and neutral framing>
Workspace: active/<slug>/subagents/<dispatch-name>/
Report: active/<slug>/subagents/<dispatch-name>/report.md

## Required Reads
- .re-discipline/project-profile.md
- .codex/external-drafter-contract.md
- active/<slug>/CAMPAIGN.md
- docs/INDEX.md and docs/truth/INDEX.md
- <selected truth, history, and artifact paths>

## Objective
<objective verbatim>

## Evidence Standard
Tag every claim DIRECT or INFERRED. Verify value-precise claims from the
primary artifact. For subject-defined facts, check the canonical source of
record before empirical inference.

## Scope
Write only in the assigned workspace unless this brief grants another exact
campaign path. Do not edit truth, history, governance, or another drafter's
work. Do not commit, push, close the campaign, promote truth, or spawn agents.

## Deliverable
Write report.md and lead with VERDICT. Include CLAIMS, CORRECTIONS / OVERTURNS,
TRUTH-PROMOTION CANDIDATES, DELIVERABLES when applicable, RESIDUAL
UNCERTAINTIES, MANAGER RUNBOOK when applicable, EVIDENCE INDEX, MEMORY
CANDIDATES, and OVERALL CONFIDENCE. Every promoted candidate needs a surviving
recipe or preserved artifact. Do not add a next-steps section.

## Exit
Budget: <budget>. If blocked, return a partial report with the evidence boundary
and the missing observation stated plainly.
```

## Step 5: Dispatch Through The Active Adapter

### Claude Code

When running in Claude Code, use its native subagent tool if available. Supply
the exact `brief.md` path and require `report.md`. Choose capability by role and
current availability; do not hardcode model aliases in this skill. If the
subagent can only return a final message, land that message in `report.md`
without changing its claims.

### Codex

When running in Codex and collaboration is allowed, call `spawn_agent` with a
bounded task. The message must instruct the worker to read `brief.md` and
`.codex/external-drafter-contract.md`, stay inside the assigned workspace, and
write `report.md`. Retain the returned agent id. Use `send_message`,
`followup_task`, `wait_agent`, or `interrupt_agent` only to manage that task.
If the worker returns the report only in its final response, write it to the
required path verbatim before review.

### Explicit External Provider

Require a configured provider and dispatcher. Invoke the project's documented
external command, normally:

```powershell
agents/dispatch.ps1 -Provider <provider> -Slug <slug> -Name <name>
```

Run it with the drafter workspace as the effective working directory when the
provider supports that option, so `AGENTS.override.md` is discovered. Do not
install, authenticate, or bypass a provider's sandbox without the user's
approval.

If the selected adapter is unavailable, leave the brief and workspace intact,
report the exact blocker, and do not pretend the dispatch occurred.

## Step 6: Record And Review

Add one current-state line to `CAMPAIGN.md` with provider, date, objective, and
report path. When the task completes, invoke `review-subagent`. Nothing in the
report crosses into `docs/truth/` before manager ratification.

## Reference

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- Drafter law: `.codex/external-drafter-contract.md`.
- Return triage: `review-subagent`.
