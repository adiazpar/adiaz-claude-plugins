# RFC 0002: Delivery amendments to the 0.8 campaign state engine

**Status:** Draft amendment set. Reviewed against RFC 0001, the 0.7-to-0.8
migration plan, the measured 0.7 failure record, and a full survey of the
0.7.1 implementation (2026-07-31).

**Relationship to RFC 0001:** RFC 0001's architecture is ratified as written:
the three-plane model, the work-item/run/finding vocabulary, independent
epistemic axes, closure-as-coverage, filesystem-canonical state, and the hard
0.8 cutover (no legacy writers, no deprecated wrappers) all stand. This
document amends **delivery sequencing, migration scope, enforcement scope,
and the retrieval roadmap**. Where an amendment conflicts with RFC 0001 or
the migration plan, the amendment governs.

**Review evidence base.** These amendments were written against fresh
measurements of the origin project (`snaphak-re`, 2026-07-31):

- Two active masterfiles at 186 KB and 172 KB (~45k tokens each) — the
  context-as-storage failure is live, not historical.
- Review-stamp compliance since the 0.6.x review workflow shipped
  (2026-07-27): 37 of 169 reports stamped; `REVIEWS.md` ledgers in 3 of 9
  campaigns. The voluntary-compliance leak (RFC 0001 §2.6) is confirmed on
  current data, with the caveat that most unstamped reports predate the
  workflow.
- Current implementation: ~13,600 non-test Go LOC in the knowledge engine;
  campaign state awareness in Go is 121 lines of mtime staleness checks;
  all campaign semantics live in Markdown parsed only by the model.

---

## Amendment 1: Re-sequence delivery — knowledge plane first, additively

**Changes:** RFC 0001 §18 (implementation increments); migration plan §19
(implementation order).

RFC 0001 delivers nothing user-visible until the transactional control plane
exists (Increments A–B precede C–D). That is an all-or-nothing bet: the two
problems the design exists to solve — dropped work branches and re-solving
already-solved questions — get no relief until near the end, and the
meta-tooling risks becoming the project.

The two problems have different substrates:

- Dropped branches are a **control-plane** problem (work graph, deferral
  contracts, frontier views).
- Re-solved questions are a **knowledge-plane** problem (atomic findings,
  cross-campaign visibility, retrieval unit).

The knowledge plane does not require the control plane. A finding can cite a
legacy subagent workspace path as its source run. Therefore:

**New order:**

1. **0.7.x additive releases (knowledge plane):** finding records
   (§8.4/§8.5 schema as written), the knowledge-curator agent role, intake
   batches and review receipts, finding-card indexing, and cross-campaign
   visibility of manager-ratified findings. Findings cite legacy
   `subagents/<workspace>/` paths via a `sourceRuns` compatibility form.
   Masterfiles remain canonical campaign state during this phase. No legacy
   surface is removed.
2. **0.8.0 hard cutover (control plane):** work items, runs, transactions,
   events, generated `STATE.md`, closure jobs, and the removal of
   checkpoint-campaign, promote-truth, masterfile writers, and report
   stamps — exactly as RFC 0001 specifies, but now migrating a corpus whose
   knowledge is already partially normalized and whose extraction quality
   is already measured (Amendment 3).

This preserves RFC 0001 decision 18 (hard release, no legacy writer *in
0.8*). It front-loads the additive half so the cutover is smaller, later,
and de-risked. Increment mapping: old C and the finding-card part of D move
into the 0.7.x series; A, B, E, F, G retain their content and exit gates and
become the 0.8 series.

**Exit gate for the 0.7.x knowledge-plane series:** a returned run in a live
0.7 campaign becomes searchable normalized findings without editing a
masterfile; a manager in another campaign retrieves those findings with the
campaign-provisional label; the curator cannot ratify.

## Amendment 2: Demand-driven legacy normalization

**Changes:** RFC 0001 §15.3, §15.6; migration plan §11, §16.2, §18.

The migration plan requires exhaustive span-level coverage of every legacy
report before a campaign is `migrated`. For the scale pilot alone that is 79
reports, plausibly 400–800 candidate findings and 80–150 review packets of
manager attention — paid up front, mostly for material that may never be
queried again. Exhaustive certification is the right bar for the *system*;
it is the wrong default for *dead* material.

**Amended model:**

- Every legacy report is **shadow-indexed as provenance** (the archive
  retrieval class; today's `draft`/`campaign` tiers already approximate
  this). This is cheap, complete, and preserves reachability: nothing
  becomes invisible.
- Curator normalization of legacy reports is **demand-driven**: a report
  earns a curator run when (a) the manager requests it, (b) retrieval lands
  on its chunks repeatedly (a served-from-archive counter crossing a
  configured threshold queues it), or (c) its campaign enters closure.
- **Exhaustive traversal certification applies only to campaigns designated
  live** by the manager at migration time. Closed campaigns and legacy
  material certify at the shadow-index level: every report reachable,
  digest-verified, and labeled unnormalized provenance.
- The `migrated` state is correspondingly split: `migrated (live-certified)`
  for the project's live frontier, with a visible backlog of unnormalized
  provenance that never silently disappears.

This converts an O(entire-history) migration into O(what the project still
needs), while the coverage machinery (which still exists and still gates
closure of live campaigns) guarantees nothing is lost — only deferred,
visibly.

The pilot sequence stands (small pilot first, then the scale pilot), with
one operational prerequisite: **checkpoint the scale-pilot campaign before
conversion begins** if its masterfile is stale, so the frozen legacy
narrative is current at freeze time.

## Amendment 3: Finding-first retrieval is gated on measurement

**Changes:** RFC 0001 §12.8, §17.2; migration plan §14.2. Resolves the
tension between §12.8 (archive tier opt-in) and migration §14.2 (reports
remain a searchable fallback).

Once cards are the default and raw reports are opt-in, extraction quality
becomes the retrieval ceiling: a curator that drops a qualifier makes
knowledge invisible even though the report said it. Measured 0.7 evidence
cuts both ways — reports are already well-structured along epistemic
headings, so chunk retrieval over reports is a stronger baseline than the
RFC assumes.

**Amended behavior:**

- Report chunks remain in the **default fallback lane** (ranked below valid
  normalized findings, as migration §14.2 says) until the ratified suite
  shows normalized finding retrieval is non-inferior on recall and superior
  on token cost for the known-question corpus. §17.2's
  "normalized-beats-raw" test is promoted from a test to a **release gate**
  for making archive opt-in.
- After the gate passes, §12.8's opt-in archive class takes effect as
  written.

## Amendment 4: Enforcement scope — hook-gated writes now, capabilities later

**Changes:** RFC 0001 §9.5, §12.6.3 (capability authentication), open
question 1; migration plan §7.5.

On current hosts every MCP call arrives from the same process; a curator
subagent and the manager are indistinguishable without host-attested
capabilities that do not exist today. The realistic threat model is
**accident, not adversary**: an agent forgetting the workflow, not forging
authority.

**Amended scope for 0.8:**

- Enforce the write boundary with what hosts actually provide: PreToolUse
  hooks deny direct `Write`/`Edit` on canonical state paths (campaign
  records, work items, runs, findings, reviews, events, truth), forcing
  mutations through the engine adapters; the engine's reconciliation flags
  out-of-band edits as dirty, exactly as RFC 0001 §12.2 specifies.
- Role separation (curator cannot ratify) is enforced by the engine on the
  **declared** role of the mutation plus the structural invariants (a
  ratification without a review receipt is refused regardless of caller).
- Signed or host-attested capabilities (open question 1) are **deferred**
  until a host provides a primitive worth building on. They are removed
  from the 0.8 release gates; hook coverage plus reconciliation is the
  shipped boundary, and the documentation states plainly that it protects
  against accidents, not adversarial callers.

## Amendment 5: Context leases ship minimal or not at all

**Changes:** RFC 0001 §12.7; open question 2.

Budgets, handles, and delta mode deliver most of the flood control. Leases
add novel server state with unproven benefit. 0.8 ships the simplest
possible form — caller-supplied lease ID with in-memory dedup, no
persistence across compaction — or defers the feature entirely if the
retrieval evaluations meet their token targets without it. Lease
sophistication is not a release gate.

## Amendment 6: Manager review-load controls

**Changes:** RFC 0001 §9.3 (curator review packets), §17.5; migration plan
§11.2.

Manager attention is the scarcest resource in the system. Continuous
curation multiplies decisions: every finding gets an individual receipt.
Controls, in addition to the packet grouping already specified:

- **Triage tiers in the packet:** the curator marks each candidate
  `routine` (uncontested observation, method, or dead-end with no truth
  contact) or `attention` (conflicts, truth candidates, scope changes,
  challenged material). A single submission may bulk-accept the `routine`
  rows; `attention` rows require individual engagement. Conflicted findings
  can never be bulk-accepted (unchanged from RFC 0001).
- **A measured ceiling:** §17.5's "manager review load per curator batch"
  gains a configured target (minutes per packet, packets per session). If
  the measured load exceeds the target during the pilots, curation
  granularity is coarsened (fewer, larger findings) before the workflow is
  declared done. Load is a first-class pilot output, not an afterthought.
- **Demand-driven backfill** (Amendment 2) is the primary load valve for
  legacy material.

## Amendment 7: Immediate 0.7.x fixes independent of 0.8

These attack the live failure modes now and are prerequisites for a fair
0.8 baseline evaluation.

1. **`allowedTiers` schema bug.** The MCP tool schema's `allowedTiers` enum
   omits `campaign` and `draft`, though the engine accepts both and the
   manager's default context-pack tier set includes `campaign`. A
   schema-validating client cannot explicitly request the tier where
   stamped reports live — silently suppressing the one cross-campaign
   knowledge surface 0.7 has. Fix the enum.
2. **Frontier re-render in the lifecycle skills.** In-session drift needs
   no compaction: a manager tangents mid-session and the remaining work
   items never resurface. Amend `review-subagent` and `delegate` to end
   every cycle by re-rendering the campaign frontier (open items,
   unresolved holds, next actions) to the user. This is the behavioral
   bridge until the work graph exists; the 0.8 `state(mode="resume")` view
   is its durable replacement.

## Amendment 8: Retrieval and model-engineering roadmap

**Changes:** RFC 0001 §12.6.2; migration plan §15. Ordered by expected
value; every measured 0.7 retrieval defect so far was plumbing rather than
ranking, and the largest remaining win is the retrieval unit (findings) —
these items compound on top of that.

1. **Synthetic queries at curation time (doc2query).** The curator already
   reads every report; it additionally emits 3–5 anticipated
   natural-language questions per finding, stored in frontmatter (an
   extension of `aliases`) and indexed as retrieval keys. This bridges the
   vocabulary gap between question phrasing and claim phrasing at near-zero
   marginal cost, and keeps ranking fully deterministic — the model
   contributes at index time, never at query time.
2. **Identifier-aware lexical analysis.** The origin corpus is dense with
   forms like `idSnapEditorLocal+0x23618` and `FUN_141a08e80`. At index
   time: normalize hex addresses to a canonical form, split camelCase and
   underscore identifiers into subtokens, and index both the raw and split
   forms. For identifier-heavy corpora, better lexical analysis outranks
   better dense embeddings.
3. **Dense-lane ablation, then replace or delete.** The bundled model is a
   50-dimension static word embedding with a 50k vocabulary — nearly blind
   to identifier-heavy text, which is why calibration drove its weight to
   the floor. Run the ablation (count queries where dense contributes a
   unique first hit no lexical lane found). If near zero, delete the lane.
   If dense earns a place, replace the embedding with a small quantized
   ONNX model (MiniLM/bge-small class; int8 CPU inference is
   deterministic) — but only after items 1–2, which likely move the needle
   more.
4. **Reranker ablation; the agent is the reranker.** The bundled linear
   reranker approximates the exact lane with inverted field priorities.
   Instead of a smarter server-side reranker, return more, smaller cards
   and let the calling model choose what to expand — which is exactly the
   card design. Delete the rerank lane if the ablation agrees.

Calibration scope (RFC 0001 §12.6.2) is unchanged: these are indexing and
lane-inventory changes, versioned in the chunker/parser/profile contracts,
not new tunable weights.

## Amendment summary

| # | Amendment | Supersedes / modifies |
|---|---|---|
| 1 | Knowledge plane ships additively in 0.7.x; control plane is the 0.8 cutover | RFC §18; migration §19 |
| 2 | Legacy normalization is demand-driven; exhaustive certification only for live campaigns | RFC §15.3, §15.6; migration §11, §16.2, §18 |
| 3 | Archive opt-in is gated on normalized-beats-raw measurement | RFC §12.8, §17.2; migration §14.2 |
| 4 | Hook-gated writes ship; attested capabilities deferred, removed from release gates | RFC §9.5, §12.6.3, open Q1; migration §7.5 |
| 5 | Context leases minimal or deferred; never a release gate | RFC §12.7, open Q2 |
| 6 | Review-load triage tiers and a measured ceiling | RFC §9.3, §17.5; migration §11.2 |
| 7 | Immediate 0.7.x fixes: `allowedTiers` enum; frontier re-render in skills | 0.7.x, pre-0.8 |
| 8 | Retrieval roadmap: doc2query, identifier analysis, dense/rerank ablations | RFC §12.6.2; migration §15 |
