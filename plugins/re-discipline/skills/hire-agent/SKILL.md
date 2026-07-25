---
name: hire-agent
description: >-
  Evaluate an external agent or CLI for a re-discipline project when asked to
  hire, interview, onboard, or benchmark a candidate. Builds an isolated
  recruiting workspace, runs an evidence-based interview battery, and drafts a
  recommendation without changing the active roster.
---

# Evaluate An External Agent

This workflow evaluates an optional external CLI as a drafter. It never changes
the active roster; `decide-agent` applies the user's later decision.

## Step 1: Ensure The Optional Adapter Layer

If `agents/` is absent, render `agents-config.json`, `agents-README.md`, and
`dispatch.ps1` from `<plugin-root>/templates/project/`, then create
`agents/profiles/`, `agents/roster/`, and `agents/benchmarks/`. Keep
`backend: native`.

Create `recruiting/<candidate>/` with `interview/`, `CANDIDATE.md`, and
`rollback-manifest.md`. Candidate work stays there. Any approved write outside
the repository, such as temporary provider configuration, must be recorded with
an exact undo operation in the rollback manifest.

## Step 2: Research Current CLI Behavior

Use current official provider documentation and verify every operational flag
with the installed CLI's `--help`. Record non-interactive invocation, working
directory, prompt delivery, model selection, output capture, sandbox and
approval controls, MCP configuration, instruction discovery, authentication,
and current prompting guidance. Follow `references/research-checklist.md`.

Do not rely on remembered flag names. If live documentation and the installed
CLI disagree, use the installed version and record the discrepancy.

## Step 3: Install And Authenticate Deliberately

Obtain explicit user approval before installing a missing CLI or changing
machine-level configuration. Never automate login, copy credentials, or handle
authentication secrets. Pause with the exact official login command when user
action is required.

## Step 4: Draft Config And Profile

Write:

- `config-draft.json`, using the schema in `agents/README.md`, with
  `enabled: true` and `promoted: false`;
- `profile-draft.md`, rendered from
  `<plugin-root>/templates/project/agent-profile.md`, containing only
  provider-specific prompting guidance and provisional role fit.

Use verified `sandbox_args` as the normal path. Record `bypass_args` only when
the CLI supports them; never make bypass the default.

## Step 5: Configure Only Required Tool Surfaces

Use the canonical profile and active manager adapter to identify tools
required by the capability target. Register only those tools in the candidate
CLI's own configuration format, with user approval for each machine-level
change. Do not hardcode another project's daemon, Ghidra instance, or MCP
names. Record every temporary registration in the rollback manifest.

## Step 6: Run A Representative Battery

Use cheap static tasks first, then tool-reach and production-loop tasks only if
the candidate passes the gate. Each task needs a versioned brief, manager-only
answer key or observable oracle, write-scope check, and cost/latency capture.
Dispatch with the candidate config through `agents/dispatch.ps1 -ConfigPath`;
do not add the candidate to the live roster.

The bundled T1/T2/T3 fixtures are legacy examples from one RE project. T1 may
be used as a general evidence-honesty sample. Run T2 or T3 only when the new
project actually exposes the named subject and tools; otherwise create
project-specific equivalents under the recruiting workspace.

## Step 7: Score And Recommend

Follow `references/scoring-rubric.md` and
`references/mini-campaign-grading.md`. Compare only runs on the same fixture
version. Record the fixed manager host/model used for ratification so future
runs are comparable. Weight evidence honesty and scope compliance above speed.

Write `scorecard.md` with role fit, limitations, costs, a hire or no-hire
recommendation, and rollback status. Stop for the user's decision. Do not mark
the candidate promoted.

Do not commit unless the user explicitly asks.

## Reference

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- CLI research: `references/research-checklist.md`.
- Decision: `decide-agent`.
