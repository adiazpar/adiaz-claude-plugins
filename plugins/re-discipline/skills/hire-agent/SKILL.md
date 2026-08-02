---
name: hire-agent
description: >-
  Evaluate an external agent or CLI for a re-discipline project when asked to
  hire, interview, onboard, or benchmark a candidate. Runs an evidence-based,
  isolated battery without changing live routing.
---

# Evaluate An External Agent

This workflow evaluates an external CLI as a drafter. `decide-agent` applies
the user's later decision.

## Prepare Candidate State

Require valid provider configuration and choose a 2-50 character lowercase
kebab-case candidate slug. Create only:

```text
.re-discipline/agents/recruiting/<candidate>/
  candidate.md
  config.json
  profile.md
  scorecard.md
  teardown.md
  runs/
```

Record exact versions in candidate metadata. Research current official
documentation and verify operational flags with the installed CLI's `--help`.
Obtain explicit approval before installing software or changing machine-level
configuration. Never automate login or handle credentials.

Candidate config contains one provider and verified command, model, sandbox,
and output settings. `teardown.md` gives the exact inverse of each approved
external change.

## Run A Representative Battery

Use versioned, project-relevant tasks with manager-only answer keys or
observable oracles. Start with cheap static tasks, then test required tool
reach and a production-shaped loop. Compare candidates only on the same
fixture version, manager baseline, model policy, tool set, and context budget.

For each attempt, reserve one opaque run ID and create:

```text
runs/<run-id>/
  run.json
  brief.md
  context-pack.json
```

The run may later contain `report.md` and a lazily created `payload/`. Do not
create generic category folders. The run record inventories important files
by media kind, semantic role, retention, digest, and support relations.

Compile the narrowest `recruiting-run` context pack with a retained digest,
materialize it at the server-derived run destination, and verify it with the
active packaged runtime before launch. Exclude answer keys, pending memory
proposals, secrets, and unrelated project material. A mismatch blocks the
attempt before any card or expansion handle is used.

Dispatch through the candidate adapter with the exact run path, brief, pack
ID, retained digest, drafter contract, and write grants. The adapter may not
create workflow state or broaden grants. Require a report that cites evidence,
names uncertainty, inventories payload, and proposes spawned work without
claiming manager authority.

## Score And Recommend

Measure evidence honesty, scope compliance, answer accuracy, tool use, context
efficiency, latency, cost, and recovery behavior. Weight evidence honesty and
scope compliance above speed. Record limitations, unsafe modes, teardown
status, and a hire or no-hire recommendation in `scorecard.md`, then stop for
the user's decision.

Do not commit unless the user explicitly asks.

## References

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- Research checklist: `references/research-checklist.md`.
- Scoring: `references/scoring-rubric.md`.
- Decision: `decide-agent`.
