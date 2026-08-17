# Reporting

Lead with the user-visible outcome: current focus, blockers, decisions needed,
completed validation, and next valid transition. Keep cache generations,
model fingerprints, lane weights, internal health codes, and transaction
details out of ordinary status unless they block the requested work.

## Run Reports

A run report is terminal provenance, not an epistemic decision. It contains:

- concise result and terminal recommendation;
- atomic candidate claims with evidence grade, scope, and limits;
- exact evidence handles, digests, ranges, and what each establishes;
- reproduction and validation results;
- changed project paths and registered payload;
- uncertainties, dead ends, and spawned work proposals.

Never label a run reviewed, ratified, or current by editing report text. The
engine freezes its digest at return; curation and immutable manager reviews
create later records.

On `run.return`, submit only the report SHA-256. The report path is not caller
input: the engine derives `active/<slug>/runs/<run-id>/report.md`, verifies the
bytes there against the digest, and freezes that canonical handle. Supplying a
report path is refused before any state is written.

## Status Views

Orientation should fit one screen and name campaign handles, focus, blocked or
due-or-near deferred work, pending returns and review, knowledge availability, and the
next action. Resume may include changes since a generation or event handle.
Expand full evidence only for the next decision.

## Language Boundary

Translate machinery into plain impact. Say "the campaign has two blocked work
items" instead of printing an internal state envelope. When a transition
fails, name the refused action, exact missing condition, and safe recovery; do
not imply partial success.
