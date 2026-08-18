# re-discipline 0.9

Re-discipline is an evidence-disciplined work and knowledge system for Claude
Code and Codex. One filesystem-canonical engine manages campaigns, work items,
runs, atomic findings, immutable reviews, bounded context, closure, and
archives. MCP and CLI are peer adapters over the same operations.

## Install

```text
/plugin marketplace add adiazpar/re-discipline
/plugin install re-discipline@re-discipline
```

For Codex:

```powershell
codex plugin marketplace add adiazpar/re-discipline
codex plugin add re-discipline@re-discipline
```

For a local clone, replace the repository shorthand with the clone's absolute
path. After installation, initialize a project with
`/re-discipline:init-project` in Claude Code or
`$re-discipline:init-project` in Codex.

## Three Planes

- **Control:** campaign records, work graph, run lifecycle, events, revision
  checks, idempotency, closure coverage, and recovery.
- **Knowledge:** atomic findings, evidence relations, review receipts,
  conflicts, supersession, retrieval cards, and provenance expansion.
- **Execution:** manager, investigator, reviewer, and curator runs
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
  merge/
  closure/
```

`payload/`, `merge/`, and `closure/` are created only when needed. A merged
campaign retains its approved plan, collision-safe ID map, explicit chronology,
source event journals, and otherwise-unclassified source artifacts under
`merge/`. Ordinary project source remains in its normal location and is
registered in the run record.

## Lifecycle

1. At host session start, `onboard` calls bounded `state(mode="orient")` and a
   scoped `resume` once. It is not repeated for ordinary prompts, tool rounds,
   or compaction in the same session.
2. `open-campaign` opens a campaign and root work atomically.
3. `run.prepare` readies one run for one primary work item with an
   immutable, digest-verified context pack.
4. A return freezes the report digest and queues curation.
5. Curation drafts atomic findings and complete coverage; drafts cannot
   self-ratify.
6. Reviews record manager decisions as immutable receipts.
7. `overturn` challenges and traces dependent findings without rewriting
   provenance.
8. `merge-campaigns`, when explicitly requested, plans against exact source
   trees and atomically consolidates them into one paused canonical graph.
9. `close-campaign` proves coverage and atomically projects current truth,
   history, backlog, maintained assets, and a complete campaign archive.

Evidence grade (`direct`, `inferred`, `reported`, `unknown`), review state,
and validity are independent. Manager-ratified active findings remain labeled
campaign knowledge. Only closure may project a direct, reproducible,
conflict-free finding into `docs/truth/`.

## Campaign Topology

Use `campaign_merge_plan` before `manager_apply(action="campaign.merge")`.
The dry run is deterministic and binds the target, ordered sources, explicit
historical chronology, current head, exact source-tree digests, remapped IDs,
and preserved artifacts. Application accepts only that retained digest and
publishes one target while retiring every source in the same recoverable
transaction. It does not create a successor plus source archives, and it does
not reinterpret returned runs as approved work.

`manager_apply(action="campaign.discard")` is deliberately outside the normal
lifecycle. Use it only after an explicit request to destroy one exact open or
paused campaign. It requires the current head and campaign digest, a reason,
and the literal confirmation `DISCARD <campaign-id> FROM <campaign-slug>`.
Discard removes the active tree and inventory membership without closure,
projection, or a disguised archive, retaining only project-level proof of the
intentional destructive action. Closed campaigns cannot be discarded.

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
allowing a run report and lazy payload. Relative and absolute editor paths are
normalized against the verified project root before the same containment
decision; path spelling is not an authority boundary. When the host supplies a
current run ID, the hooks resolve its canonical `run.json` and permit ordinary
project writes only through its sealed exact-path or bounded-directory grants;
unknown, ambiguous, and terminal runs fail closed. A registered native launch
starts its message with
`re-discipline-run: <run-id> <context-pack-digest>`. The launch hook reserves a
session-local ticket, and `SubagentStart` binds it to the host agent ID.
Ordinary launches receive an ordinary binding and remain free to edit
non-canonical project files; they are not forced into campaign runs. Concurrent
launch calls in one manager session queue only inside this launch-to-start
handoff instead of returning a model-visible retry. Already-bound agents
execute concurrently, with each write resolved through its own
session/agent/run binding. Separate manager sessions have separate dispatch
state, and an agent that bypassed launch registration fails closed instead of
inheriting a process-global run identity. `SubagentStart` and `SubagentStop`
remain silent for ordinary host subagents. This accident boundary is not
adversarial authentication.
Safety remains in engine validation, reconciliation, atomic publication, and
replay even when hooks are omitted.

Multiple manager processes may work in one project. The canonical engine uses
a project-wide OS file lock, a crash-recoverable journal, and a global audit
head for every commit. The lock is an internal publication seam, not a
caller-managed retry protocol: ordinary manager and curator transactions that
touch disjoint records queue briefly and rebase to the current head inside the
same engine call. They do not require `expectedHeadRevision` or
`expectedHeadDigest` from the caller. Their record revisions, record digests,
artifact digests, and write grants remain exact, so a true collision on shared
state is still rejected. Context packs are immutable scoped snapshots; an
unrelated campaign commit or later retrieval-index generation does not expire
one. Destructive topology changes, migration, reconciliation, and closure
finalization retain exact project-head gates.

The engine also refuses `run.prepare` when its project write grants overlap
those of any `prepared` or `running` run in any campaign. Returned and
terminal runs release those project grants. Mutation `tokenBudget` values are
response hints: optional sections may be omitted whole, but a small hint never
refuses a valid commit and the complete identity floor is always returned.

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
published revision 4 of that same intake, the engine rejects the exact-record
collision. No review, finding, or event is partially written. A commit to an
unrelated campaign does not cause this refusal; only the changed record does.
The caller rereads and rechecks the changed intake before deciding whether a
new semantic transition is still valid.

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
