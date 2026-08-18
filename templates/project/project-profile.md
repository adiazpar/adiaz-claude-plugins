---
name: "<project-name>"
type: "<project-type>"
framing: "<neutral project framing>"
---

# <project-name> - Canonical Project Profile

This is the single source of project identity, domain facts, and shared laws.
Host adapters contain only host-specific configuration.

<!-- re-discipline:shared-laws v0.8.0 -->
## Directory Means Trust

| Location | Status |
|---|---|
| `docs/truth/` | Closure-approved current claims with durable verification. |
| `docs/history/campaigns/` | Complete historical records and provenance, not current authority. |
| `active/<slug>/` | Provisional structured campaign state. |
| `active/<slug>/runs/*/report.md` | Run provenance awaiting or supporting curation and review. |
| `active/<slug>/findings/` | Atomic campaign knowledge with explicit evidence, review, and validity axes. |
| `docs/backlog/` | Deferred intent, not completed work. |
| maintained source, tests, tools, fixtures, corpora, and references | Durable project assets with an active owner or consumer. |

## Session Start

Use `onboard`. Call bounded `state(mode="orient")`, select a campaign, then
call `state(mode="resume")`. Expand only cited handles needed for the next
decision. Generated views and caches are derived; source records are canonical.

## Evidence And Review

Evidence grade, review state, and validity are independent. A report is
provenance, not knowledge by itself. Curators draft atomic findings and
coverage; managers ratify or reject them through immutable review receipts.
Only closure may project an approved direct finding into `docs/truth/`.

## Campaign Lifecycle

Use `open-campaign`, `overturn`, and `close-campaign`. Every substantive
activity has a work item and run. Record
tangents and deferrals as addressable work with explicit revisit and closure
contracts. The shared engine validates revisions, idempotency, roles, paths,
and transitions, appends events, and regenerates bounded state views.

## Manager And Drafter Roles

Managers own intent, review, ratification, retention, and closure. Drafters
work only the exact run brief, report evidence and uncertainty, and write only
their report, lazy payload, and explicit project grants. Curators cannot
ratify, edit truth, close campaigns, or approve their own packets.

## Migration And Recovery

Ordinary operations never read or convert older state. Use `migrate-project`
for explicit preview, approval, resumable application, verification, and
ratification. Recovery rebuilds derived state from canonical 0.8 records.

## Shared Recall

Accepted memory is operational recall, never empirical evidence. Pending
proposals are excluded from ordinary retrieval until explicit user review.

## Safety

- Do not write canonical campaign or truth records directly; use the engine.
- Do not treat inference, a report, or memory as current truth.
- Do not silently overwrite stale revisions or retry without an idempotency key.
- Do not delete retained material without coverage and manager approval.
- Do not leave closure-blocking work without an explicit disposition.
- Commit or push only when the user asks.
<!-- re-discipline:shared-laws:end -->

## Mission

<One accurate paragraph.>

## Source Of Record

<Authoritative project inputs and verification paths.>

## Tooling And Exclusive Surfaces

<Portable tools, concurrency constraints, and maintained recipes.>

## Environment

<Host-neutral environment facts. Keep machine-local values outside this file.>
