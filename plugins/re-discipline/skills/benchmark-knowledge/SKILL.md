---
name: benchmark-knowledge
description: >-
  Run a read-only re-discipline knowledge benchmark when asked to benchmark
  knowledge, test retrieval, measure retrieval accuracy, compare fallback
  profiles, check context-token efficiency, or validate the knowledge server.
---

# Benchmark Project Knowledge

Measure retrieval without changing canonical knowledge, settings, weights, or
the active profile. Permit any user to invoke this skill.

## Step 1: Validate The Project

Read:

- `.re-discipline/project-profile.md`;
- `.re-discipline/config.json`;
- `.re-discipline/settings/README.md`;
- `.re-discipline/settings/knowledge.jsonc`;
- the requested accepted profile;
- `<plugin-root>/references/knowledge-governance.md`.

Run the bundled knowledge runtime's cheap health check. Stop and report an
invalid config, unresolved source boundary, stale citation, corrupt index, or
unsupported effective profile. Allow automatic index reconciliation, but do
not repair canonical files or download a model implicitly.

Pin the corpus fingerprint, Git and dirty-state identity, evaluation
fingerprint, requested profile, model checksums, and every effective profile
under test.

## Step 2: Select One Mode

- **quick:** Run integrity, exact-identifier, tier-policy, citation,
  deterministic replay, and token-budget conformance. Use this default when
  the user requests an unspecified check.
- **full:** Run lexical, dense, hybrid, reranking, graph, context-pack, and
  project evaluation against every supported effective profile, including
  model-free fallbacks. Exercise both manager and drafter context ceilings at
  512, 1024, 2048, and 4096 tokens.
- **end-to-end:** Run selected manager and drafter trials only after the user
  explicitly authorizes the additional model tokens, time, and dispatch
  budget. Do not make this the default.

State the selected mode and estimated local work before starting a full or
end-to-end run.

## Step 3: Run The Deterministic Harness

Invoke the packaged benchmark operation. Do not score results manually in the
manager prompt. Do not launch subagents for each query or parameter
combination.

Apply these gates before comparing quality or cost:

1. source allowlist and secret exclusion;
2. tier and access enforcement;
3. valid paths, lines, hashes, and generations;
4. no stale-result leakage;
5. exact search and context-pack replay for each effective profile;
6. hard token, byte, citation, and passage limits at every tested budget.

Apply retrieval-quality gates to a case at its ratified token budget and
larger budgets. Record lower-budget degradation without turning it into an
authority, privacy, freshness, citation, replay, or hard-budget exception.

Then report evidence-set recall, exact-target MRR, nDCG, precision,
multi-source coverage, abstention, citation quality, relevant-token ratio,
duplicate-token ratio, latency, and local compute. Keep metrics separate
instead of hiding them in one score.

## Step 4: Preserve Read-Only Semantics

Write run output only under `.re-discipline/cache/`. Do not edit:

- `.re-discipline/settings/`;
- `.re-discipline/memory/`;
- `.re-discipline/knowledge/evals/`;
- `docs/`, `active/`, or source files;
- plugin baseline profiles.

Do not calibrate, activate a candidate, accept memory, or promote truth as a
benchmark side effect.

## Step 5: Report

Report:

- mode and pinned corpus/evaluation identities;
- requested and effective profiles, active lanes, models, and fallbacks;
- hard-gate results;
- metric table by effective profile and token budget;
- regressions and unsupported capability combinations;
- cache report path;
- whether an explicit calibration or profile decision is warranted.

Recommend `calibrate-knowledge` only when measured evidence shows a retrieval
or token-budget problem. Never change behavior from this skill.

Do not commit unless the user explicitly asks.

## Reference

- Governance and ownership:
  `<plugin-root>/references/knowledge-governance.md`.
- Candidate generation: `calibrate-knowledge`.
- Profile decision: `decide-retrieval-profile`.
