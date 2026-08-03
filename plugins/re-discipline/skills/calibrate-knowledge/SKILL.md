---
name: calibrate-knowledge
description: >-
  Calibrate re-discipline retrieval when a project maintainer asks to tune
  retrieval fusion weights, investigate measured retrieval misses, compare
  the supported hybrid weight candidates, or create a candidate knowledge
  profile.
---

# Calibrate Project Knowledge

Search for a better candidate profile without changing production behavior.
Require a direct manager or project-maintainer session and an explicit
calibration request.

## Step 1: Establish Scope And Inputs

Read `.re-discipline/project-profile.md`, the bootstrap config, documented
settings, accepted profile, ratified evaluation cases, and
`<plugin-root>/references/knowledge-governance.md`.

Choose one scope:

- **project:** use the tracked project evaluation corpus and write disposable
  candidate state locally;
- **global:** proceed only as a plugin maintainer in the authoritative plugin
  repository, using the cross-project suite.

Run health and a baseline full benchmark. Stop when the corpus lacks a
ratified development/holdout split, hard policy gates fail, model checksums do
not match, or the accepted baseline cannot be replayed.

## Step 2: Freeze Non-Tunable Policy

Never tune:

- tier or access filters;
- secret and path exclusions;
- source authority;
- citation or freshness requirements;
- deterministic-replay requirements;
- memory proposal exclusion;
- the DIRECT-evidence Wall.

Treat those rules as hard gates.

## Step 3: Sweep Candidate Parameters

Invoke the bundled deterministic calibration operation. In this release it
explores only the finite 3-by-3-by-3 grid of exact, FTS, and graph
reciprocal-rank-fusion weights for the active effective capability row. It
does not tune candidate counts, field boosts, graph expansion,
packing rules, or manager/drafter budgets. Those remain versioned profile or
project-policy inputs and require their own measured implementation before a
future calibration release may change them.

Measurement freshness (benchmark staleness, evidence-pin health) is
checked and repaired here, at gate time, as part of this skill's own run -
it is never surfaced during onboarding or session start.

Evaluate development cases first. Evaluate only finalists on the frozen
holdout. Benchmark the declared lexical-graph profile independently.

Do not launch a subagent for every combination. Use subagents only for an
explicitly authorized finalist trial, failure investigation, paraphrase
proposal, or blinded end-to-end comparison. Require manager or user
ratification before adding any resulting evaluation judgment.

## Step 4: Select A Candidate

Reject candidates that fail authority, privacy, citation, freshness,
abstention, exact-identifier, deterministic-replay, or budget gates.

Compare passing candidates on the Pareto frontier for evidence coverage,
relevant tokens, duplicate tokens, latency, and local compute. Do not hide a
quality/cost tradeoff in one aggregate score.

Write the generated candidate profile, content hash, and report only under
`.re-discipline/cache/calibration/<run-id>/`. Record its base profile, corpus
and evaluation fingerprints, parser/chunker versions, model checksums,
supported effective profiles, parameters, benchmark digest, and residual
regressions. The candidate content hash identifies the exact unsigned
candidate payload; it is not an accepted-profile hash.

Remove any inherited `approval` object before writing the candidate. A
calibration candidate must not contain or self-assert a promotion decision,
explicit user approval, promotion receipt, or accepted-profile content hash.
Treat any candidate carrying those fields as malformed candidate output.

## Step 5: Stop Before Promotion

Do not edit `.re-discipline/knowledge/retrieval-profile.json`, plugin baseline
profiles, memory, truth, or ratified evaluation cases. Do not activate the
candidate, even when every metric improves. Calibration never stamps approval
or writes a promotion receipt.

Report to the user in plain language per
`<plugin-root>/references/reporting.md`; machinery identities go into the
campaign or run record, not the screen. Record the candidate path,
hard-gate outcome, holdout comparison, supported fallback matrix, the
benchmark's token-budget measurements, and any end-to-end work not run in
the campaign or run record. Do not describe budget measurements as budget
tuning.

Print to the user only:

```user-facing
Calibration finished. A tuned search profile is ready for your decision:
it <improved|did not improve> retrieval on this project's own questions.
Accept or reject it with decide-retrieval-profile. Details are recorded at
<candidate path>.
```

Do not commit unless the user explicitly asks.

## Reference

- Governance and permissions:
  `<plugin-root>/references/knowledge-governance.md`.
- Read-only measurement: `benchmark-knowledge`.
- Explicit decision: `decide-retrieval-profile`.
