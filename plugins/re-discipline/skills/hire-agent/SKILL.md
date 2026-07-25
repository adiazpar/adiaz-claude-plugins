---
name: hire-agent
description: >-
  Evaluate an external agent or CLI for a re-discipline project when asked to
  hire, interview, onboard, or benchmark a candidate. Builds an isolated
  recruiting workspace, runs an evidence-based interview battery, and drafts a
  recommendation without changing live provider configuration.
---

# Evaluate An External Agent

This workflow evaluates an external CLI as a drafter. It never changes live
provider state; `decide-agent` applies the user's later decision.

## Step 1: Verify The Agent Core

Require the normalized files created by `init-project`:

- `.re-discipline/agents/README.md`
- `.re-discipline/agents/config.json`
- `.re-discipline/agents/dispatch.ps1`
- `.re-discipline/agents/providers/`
- `.re-discipline/agents/recruiting/`

If any are missing or malformed, use `init-project` repair before recruiting.
Use a 2-50-character lowercase kebab `<candidate>` slug matching
`^[a-z0-9]+(?:-[a-z0-9]+)*$`. It names the stable executor family, not an
exact model or release; record exact versions in candidate and run metadata.

Create `.re-discipline/agents/recruiting/<candidate>/` with:

```text
candidate.md
config.json
profile.md
scorecard.md
teardown.md
runs/
```

Candidate state stays isolated there. `teardown.md` records an exact inverse
for every approved write outside the repository.

## Step 2: Research Current CLI Behavior

Use current official provider documentation and verify every operational flag
with the installed CLI's `--help`. Record non-interactive invocation, working
directory, prompt delivery, model selection, output capture, sandbox and
approval controls, MCP configuration, instruction discovery, authentication,
and current prompting guidance. Follow `references/research-checklist.md`.

Do not rely on remembered flag names. When live documentation and installed
help disagree, use the installed version and record the discrepancy.

## Step 3: Install And Authenticate Deliberately

Obtain explicit user approval before installing a missing CLI or changing
machine-level configuration. Never automate login, copy credentials, or handle
authentication secrets. Pause with the exact official login command when user
action is required.

## Step 4: Draft Candidate State

- `candidate.md`: provider, installed version, research date, sources, and
  evaluation target.
- `config.json`: the documented provider schema, with the candidate as its
  backend and only provider. Include no lifecycle flags.
- `profile.md`: render `agent-profile.md` with only provider-specific
  prompting and operational guidance.
- `teardown.md`: exact paths, keys, and inverse operations for approved
  external configuration.
- `scorecard.md`: start with fixture and manager-baseline metadata; complete
  it after evaluation.

Use verified `sandbox_args` for normal dispatch. Record `bypass_args` only
when the CLI supports them; bypass is never the default.

## Step 5: Configure Only Required Tool Surfaces

Derive concrete tool requirements from the candidate tasks. Register only
those tools in the candidate CLI's own configuration format, with user
approval for each machine-level change. Do not hardcode another project's
daemon, disassembler, or MCP names. Record every temporary registration in
`teardown.md`.

## Step 6: Run A Representative Battery

Use cheap static tasks first, then tool-reach and production-loop tasks only if
the candidate passes the gate. Each task needs a versioned brief, manager-only
answer key or observable oracle, write-scope check, and cost/latency capture.

For each task, capture UTC once after selecting the candidate and form:

```text
<dispatch-id> = YYYY-MM-DDTHH-mm-ssZ-<candidate>-<task>
```

Validate `<task>` with the same 2-50-character lowercase kebab rule. Reserve
`runs/<dispatch-id>/` with atomic fail-if-exists directory creation. On a
same-second collision, try `-02`, then `-03`, through `-99`; never use a
check-then-create sequence.

The run directory is the actual isolated drafter workspace. Create
`scripts/`, `analysis/`, `artifacts/`, and `evidence/`; write `brief.md`; and
render `<plugin-root>/templates/project/recruiting-AGENTS-override.md` as
`AGENTS.override.md`. The brief records Workspace, Created UTC, Manager host,
Executor, Execution route, Provider/model, Task, and report path.

Dispatch with:

```powershell
.re-discipline/agents/dispatch.ps1 -Provider <candidate> -RecruitingCandidate <candidate> -DispatchId <dispatch-id>
```

The dispatcher reads the candidate's own `config.json` and `profile.md`; do not
pass `-ConfigPath`, require a campaign, or add the candidate to live config.
The drafter writes `report.md` inside the run workspace. Leave blocked or
failed runs intact for scoring and teardown. A reroute or distinct attempt
gets a new dispatch ID.

Bundled fixtures are examples only; use a project-specific equivalent when
their subject or tools do not exist.

## Step 7: Score And Recommend

Follow `references/scoring-rubric.md` and
`references/mini-campaign-grading.md`. Compare only runs on the same fixture
version. Record the fixed manager host/model used for ratification so future
runs are comparable. Weight evidence honesty and scope compliance above speed.

Complete `scorecard.md` with limitations, costs, unsafe modes, a hire or
no-hire recommendation, and teardown status. Stop for the user's decision.

Do not commit unless the user explicitly asks.

## Reference

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- CLI research: `references/research-checklist.md`.
- Decision: `decide-agent`.
