# re-discipline 0.8

Re-discipline is an evidence-disciplined work and knowledge system for Claude
Code and Codex. One filesystem-canonical engine manages campaigns, work items,
runs, atomic findings, immutable reviews, bounded context, closure, and
archives. MCP and CLI are peer adapters over the same operations.

## Three Planes

- **Control:** campaign records, work graph, run lifecycle, events, revision
  checks, idempotency, closure coverage, and recovery.
- **Knowledge:** atomic findings, evidence relations, review receipts,
  conflicts, supersession, retrieval cards, and provenance expansion.
- **Execution:** manager, investigator, reviewer, and knowledge-curator runs
  with exact briefs, immutable packs, write grants, reports, and lazy payload.

Canonical files are source of record; indexes, caches, and `STATE.md` are
derived and rebuildable. Every state mutation supplies expected revisions and
an idempotency key and publishes atomically with its event.

## Campaign Shape

```text
active/<slug>/
  campaign.json
  STATE.md
  work-items/
  runs/<run-id>/
    run.json
    brief.md
    context-pack.json
    report.md
    payload/
  findings/
  intake/
  reviews/
  events/events.jsonl
  closure/
```

`payload/` is created only when needed. Ordinary project source remains in its
normal location and is registered in the run record.

## Lifecycle

1. `onboard` calls bounded `state(mode="orient")` and a scoped `resume`.
2. `open-campaign` opens a campaign and root work atomically.
3. `delegate` prepares one run for one primary work item and verifies its
   immutable context pack before dispatch.
4. A return freezes the report digest and queues curation.
5. The `knowledge-curator` drafts atomic findings and complete coverage; it
   cannot ratify.
6. `review-subagent` records manager decisions as immutable receipts.
7. `overturn` challenges and traces dependent findings without rewriting
   provenance.
8. `close-campaign` proves coverage and atomically projects current truth,
   history, backlog, maintained assets, and a complete campaign archive.

Evidence grade (`direct`, `inferred`, `reported`, `unknown`), review state,
and validity are independent. Manager-ratified active findings remain labeled
campaign knowledge. Only closure may project a direct, reproducible,
conflict-free finding into `docs/truth/`.

## Context And Retrieval

The server returns bounded cards and stable handles. Expand exact provenance
only when the next decision needs it. Normalized findings rank before raw
provenance. Returned report chunks remain a lower-ranked default fallback
until the ratified known-question benchmark shows normalized retrieval is
non-inferior on recall and better on token cost.

The 0.8 release-candidate profile uses identifier-aware exact matching,
SQLite FTS5, relationship-graph expansion, and one checksum-pinned local
GloVe dense lane. An independently benchmarked lexical-graph profile remains
available when that embedding cannot load. The packaged holdout fixture was
too small to justify removing dense retrieval; a frozen pre-removal 64-case
project run measured two dense-only rescues with no added hard-gate regression.
Dense therefore remains pending a fresh current-runtime two-arm final-corpus
run. Reranking produced no measured benefit in the packaged holdout or the
separately frozen historical project layer and has been removed. The packager
recomputes both decisions from their own per-case runtime layers and requires
the final project receipt, historical archive, lane decisions, profile
inventories, and model inventories to agree before release.

A caller may name a process-local `contextLeaseId` on `query`. The default
memory-only lease suppresses repeated cards and returns cumulative token and
source-digest accounting. `resetContextLease` resets that derived lease after
real compaction without touching campaign state. MCP naturally keeps that
ledger for the server process; the CLI provides `query --session --input
<path-or->` for a stream of queries that must share the same lease ledger.
Separate one-shot CLI invocations intentionally start fresh ledgers.

## Write Boundary

PowerShell and POSIX hooks provide symmetric orientation, compaction handles,
run context, return checks, and Stop warnings. `PreToolUse` treats Claude Code
`Write`/`Edit` and Codex `apply_patch` as the same write-class guard: it denies
accidental direct access to engine-owned campaign and truth paths while
allowing a run report and lazy payload. When the host supplies a current run
ID, the hooks resolve its canonical `run.json` and permit ordinary project
writes only through its sealed exact-path or bounded-directory grants;
unknown, ambiguous, and terminal runs fail closed. `SubagentStart` and
`SubagentStop` remain silent for ordinary host subagents. They inject run
context or a return check only when explicit dispatch metadata resolves one
unique registered run and matches that run's immutable context-pack identity.
This accident boundary is not adversarial authentication.
Safety remains in engine validation, reconciliation, atomic publication, and
replay even when hooks are omitted.

## Worked Example

A manager opens campaign `resource-registration` with work item `W-0001`, then
prepares `R-20260801-0001`. The investigator verifies the pack digest, edits an
explicitly granted parser, records that path in the run, and returns a cited
report. The curator creates `I-0001`, splits two independent claims into
`F-0001` and `F-0002`, maps every report span, and flags one conflict for
attention. The manager ratifies the direct claim to campaign knowledge and
defers the inferred claim with a revisit contract. At closure, coverage proves
every run, finding, decision, and file has a destination. The direct claim is
projected to truth while the other is exported to backlog, and the archive
retains the complete records and receipt digests.

## Failed Transition Example

If a manager reviews intake revision 3 while another manager has already
published revision 4, the engine rejects the stale request. No review, finding,
or event is partially written. The caller resumes from the new event head,
rechecks the changed row, and retries with a new expected revision while
retaining the same idempotency key only when the semantic request is
identical.

## Migration

`migrate-project` is the sole prior-version reader. It previews and hashes all
inputs, exports a digest-bound legacy retrieval-profile packet when an exact
non-activating packaged-baseline decision is required, exports review packets
for truth claims that require a manager-approved rewrite or split, requires
approval of the regenerated exact plan digest, applies only 0.8 records
resumably, verifies live campaign coverage and archive reachability, and emits
an immutable certification receipt. Initialization and ordinary state commands
never auto-convert a project or promote a retrieval profile.

Retrieval and host certification use strict evidence rather than named checks
plus arbitrary fingerprints. The engine re-hashes the exact final full
benchmark, calibration, unapproved candidate profile, and paired blinded
holdout trials. It also verifies a complete captured MCP/CLI/Claude Code/Codex
request-result-failure matrix and derives all semantic and host fingerprints.

See `references/` for governance, internals, reporting, and host adapters.
