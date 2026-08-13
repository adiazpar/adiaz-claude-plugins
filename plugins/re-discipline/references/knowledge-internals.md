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
- `campaign_merge_plan`: read-only validation and deterministic planning over
  exact source trees, collision-safe mappings, artifact inventory, and an
  explicit historical chronology;
- `manager_apply`: typed campaign, work, run, review, finding, decision,
  campaign-merge, and destructive campaign-discard transitions;
- `curation_submit`: intake, candidate findings, and coverage for granted runs;
- `closure_apply`: start, advance, verify, reopen, restart, and finalize
  closure; `restart` is the sole re-entry after a reopen and the sole rule that
  may move a closure job's frozen campaign revision, always forward;
- migration CLI operations: explicit preview-approved conversion only.

CLI and MCP invoke the same engine requests and return the same semantic
result digest for identical state and input.

## Campaign Topology

Campaign membership is ordinarily immutable. `campaign.merge` is the one
engine-owned topology transaction that may replace membership: an approved
dry-run plan creates one absent target graph while retiring every exact source
tree. Planning binds the current head and source-tree byte digests. Application
recomputes that plan, collision-safely remaps every campaign-local identifier,
rewrites internal relations and handles, preserves source-qualified provenance
and artifacts, and publishes the target records, merge metadata, historical
events, generated view, inventory, event, receipt, and state head through one
recoverable journal. Any validation, write, or retirement failure rolls all
trees back.

`campaign.discard` is intentionally not a closure transition. It accepts one
exact open or paused campaign, a literal destructive confirmation, a reason,
the current head, and the campaign digest. The engine computes and rechecks the
source-tree digest while holding the state writer and removes that tree from
the canonical inventory without archive projection or truth promotion. The
only durable remnants are the project-level discard event and transaction
receipt. Missing, malformed, closed, already-discarded, and concurrently
modified targets are distinct refusals; only an exact idempotent retry replays
success.

Search generations are immutable derived state. The atomic tree switch changes
their recorded source states, so freshness checks invalidate the prior
generation immediately and reconciliation constructs a new generation. No
topology operation treats a cache or generated view as authority.

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
