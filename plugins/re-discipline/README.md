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

The default dense lane is a learned embedding built from a checksum-pinned,
bundled subset of Stanford GloVe 6B 50d vectors. It runs entirely local with a
deterministic fixed-point sentence encoder. The optional rerank lane is a
deterministic integer linear scorer over phrase, identifier, path, heading,
and token-overlap features; neither lane downloads models or sends corpus
content over the network.

## Write Boundary

PowerShell and POSIX hooks provide symmetric orientation, compaction handles,
run context, return checks, and Stop warnings. PreToolUse denies accidental
direct Write/Edit access to engine-owned campaign and truth paths while
allowing a run report and lazy payload. This is an accident boundary, not
adversarial authentication. Safety remains in engine validation,
reconciliation, atomic publication, and replay even when hooks are omitted.

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
inputs, requires approval of the exact plan digest, applies only 0.8 records
resumably, verifies live campaign coverage and archive reachability, and emits
an immutable certification receipt. Initialization and ordinary state commands
never auto-convert a project.

See `references/` for governance, internals, reporting, and host adapters.
