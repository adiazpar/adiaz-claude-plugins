# Knowledge Internals

## Source And Derived State

Campaign, work-item, run, finding, intake, review, event, closure, and archive
records are canonical files validated by packaged versioned schemas. The
search index, vectors, generated state views, and orientation cards are
derived. Recovery replays records and event heads and refuses digest or
revision divergence.

Every mutation includes actor role, expected revisions, idempotency key, and
correlation ID. Under a short project-scoped lock the engine validates the
whole transition, writes temporary files, publishes all related records,
appends one event chain, and regenerates affected views. A failed validation or
publication leaves canonical state unchanged.

## Primary Operations

- `state`: bounded orient, resume, work, closure, and health views;
- `query`: ranked context cards from normalized knowledge and permitted
  provenance;
- `read`: exact handle expansion within budget and path policy;
- `trace`: provenance, conflict, dependency, challenge, and supersession;
- `context_pack_materialize`: scoped active-run/recruiting-run preview and
  digest-verified recruiting-run publication; active-run publication belongs
  exclusively to `manager_apply` `run.prepare`;
- `manager_apply`: typed campaign, work, run, review, finding, and decision
  transitions;
- `curation_submit`: intake, candidate findings, and coverage for granted runs;
- `closure_apply`: start, advance, verify, reopen, and finalize closure;
- migration CLI operations: explicit preview-approved conversion only.

CLI and MCP invoke the same engine requests and return the same semantic
result digest for identical state and input.

## Retrieval

Cards expose a concise claim or state edge, epistemic labels, relation alerts,
stable handle, evidence handle, match reason, and expansion cost. Identifier
analysis indexes raw and split forms. Synthetic questions generated during
curation are deterministic retrieval keys, never query-time judgments.

Normalized findings rank first. Returned reports remain a labeled, lower-
ranked fallback until the ratified benchmark proves non-inferior finding recall
and lower token cost. Intake proposals, pending memory, raw payload, secrets,
and caches are excluded from default answer context.

## Failure Behavior

Stale revisions, repeated non-idempotent requests, invalid roles, path escapes,
missing reports, incomplete coverage, unresolved truth conflicts, and dirty
out-of-band edits are explicit refusals. Read operations may degrade to the
CLI adapter or canonical file reads, but mutation never falls back to direct
file editing.
