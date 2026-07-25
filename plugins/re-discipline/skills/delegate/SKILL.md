---
name: delegate
description: >-
  Delegate a focused investigation into an active re-discipline campaign.
  Creates an isolated drafter workspace and brief, uses the active host's
  native Claude Code or Codex adapter unless an external provider was
  explicitly selected, and requires a report for manager review.
---

# Delegate Campaign Work

Drafters investigate; the manager ratifies. Every adapter must produce the
same reviewable report and keep the drafter away from durable truth.

## Step 1: Validate And Select The Route

Require:

- an existing `active/<slug>/CAMPAIGN.md`;
- a lowercase kebab-case `<task>` of 2-50 characters matching
  `^[a-z0-9]+(?:-[a-z0-9]+)*$`;
- a concrete objective with an observable deliverable or answer.

Select the route before naming the workspace:

1. A one-off external provider explicitly authorized by the user wins.
2. A non-`native` backend in `.re-discipline/agents/config.json` is the user's
   project-level selection.
3. Otherwise use the current host's native adapter.

Never change the backend or move work between providers without the user.

Determine `<executor>` from the worker that will actually perform the task,
not the manager:

- native work uses the active host's worker family, such as `codex` or
  `claude`;
- external work uses the selected provider's stable lowercase kebab slug.

The executor is not an exact model, role, or persona. Record those details in
the brief instead. Validate the executor with the same 2-50-character
lowercase kebab rule as the task.

## Step 2: Reserve The Drafter Workspace

Immediately after route selection, capture the current UTC time once as
`YYYY-MM-DDTHH-mm-ssZ`. Build:

```text
<dispatch-id> = YYYY-MM-DDTHH-mm-ssZ-<executor>-<task>
```

Reserve `active/<slug>/subagents/<dispatch-id>/` by attempting directory
creation with fail-if-exists semantics. Do not check existence separately
before creation. If the base ID collides, atomically try `-02`, then `-03`,
through `-99`. The first workspace has no `-01`. Stop with an explicit error
if `-99` is exhausted.

The selected ID is immutable. Reuse it in the brief, dispatch command,
campaign record, and report path; never recompute the timestamp.

Create `scripts/`, `analysis/`, `artifacts/`, and `evidence/` under the
reserved workspace. Render
`<plugin-root>/templates/project/drafter-AGENTS-override.md` to
`AGENTS.override.md`.

Do not commit unless the user explicitly asks.

## Step 3: Gather Only Relevant Context

Read the campaign and select only:

- truth files the drafter needs;
- chronicles containing dead ends it must not retry;
- primary artifacts and sanctioned tools;
- exact granted paths and exclusive live-surface constraints;
- a time or effort budget;
- relevant memory facts absent from checked-in guidance.

Memory is recall, not authority. Quote only the few useful facts in the brief;
do not assume a drafter can access the manager's memory store.

## Step 4: Write `brief.md`

Lead with the canonical `framing` value from
`.re-discipline/project-profile.md`, then include:

```text
# Dispatch Brief - <dispatch-id>

Project: <name and neutral framing>
Workspace: active/<slug>/subagents/<dispatch-id>/
Created UTC: <YYYY-MM-DDTHH:MM:SSZ>
Manager host: <claude|codex|other>
Executor: <executor>
Execution route: <native|external>
Provider/model: <provider and exact model when known, otherwise unknown>
Task: <task>
Report: active/<slug>/subagents/<dispatch-id>/report.md

## Required Reads
- .re-discipline/project-profile.md
- .codex/external-drafter-contract.md
- active/<slug>/CAMPAIGN.md
- docs/INDEX.md and docs/truth/INDEX.md
- <selected truth, history, and artifact paths>

## Required Tools And Access
- <exact tools and granted paths>
- <exclusive live surfaces and serialization requirements>

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
CANDIDATES, and OVERALL CONFIDENCE. Every promotion candidate needs a
surviving recipe or preserved artifact. Do not add a next-steps section.

## Exit
Budget: <budget>. If blocked, return a partial report with the evidence boundary
and missing observation stated plainly.
```

## Step 5: Dispatch Through The Active Adapter

### Claude Code

Use its native subagent tool when available and delegation is allowed. Supply
the exact `brief.md` path and require `report.md`. Select a worker that can
satisfy the brief's concrete tool and access requirements. If the worker can
only return a final message, land it in `report.md` without changing claims.

### Codex

When collaboration is allowed, call `spawn_agent` with a bounded task. Tell
the worker to read `brief.md` and `.codex/external-drafter-contract.md`, stay
inside the assigned workspace, and write `report.md`. Retain the agent id. Use
`send_message`, `followup_task`, `wait_agent`, or `interrupt_agent` only for
that task. If the worker returns the report only in its final response, write
it to the required path verbatim before review.

### Explicit External Provider

Require a live configured provider, or a candidate config explicitly
authorized by the user. Invoke:

```powershell
.re-discipline/agents/dispatch.ps1 -Provider <provider> -Slug <slug> -DispatchId <dispatch-id>
```

For a candidate or one-off provider, also pass its exact `-ConfigPath`. The
dispatcher selects the adjacent candidate `profile.md`; live providers use
`.re-discipline/agents/providers/<provider>/profile.md`.

Sandbox arguments are the default. Pass `-Bypass` only for that exact
user-approved dispatch. Do not install or authenticate a provider without the
user's approval.

If the selected adapter is unavailable, leave the brief and workspace intact,
report the exact blocker, and do not pretend the dispatch occurred.

Retry an interrupted launch in the same workspace only when it continues that
exact dispatch. A reroute to another executor is new work: leave the prior
workspace intact and reserve a new timestamped dispatch ID. Never rename or
recycle a workspace to hide a blocked or failed route.

## Step 6: Record And Review

Add one current-state line to `CAMPAIGN.md` with provider, date, objective, and
report path. When complete, invoke `review-subagent`. Nothing in the report
crosses into `docs/truth/` before manager ratification.

## Reference

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- Drafter law: `.codex/external-drafter-contract.md`.
- Return triage: `review-subagent`.
