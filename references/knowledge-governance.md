# Knowledge Governance

## Canonical Classes

| Class | Meaning | Default use |
|---|---|---|
| current truth | Closure-approved current claims with durable verification | Answer context |
| campaign finding | Manager-ratified but provisional knowledge | Manager context, labeled |
| extracted finding | Curator proposal awaiting manager review | Intake review only |
| history finding | Ratified historical observation, method, decision, or dead end | Historical queries |
| report provenance | Frozen run output supporting curation and trace | Lower-ranked fallback |
| backlog | Deferred intent with source provenance | Planning |
| memory | Accepted operational recall | Navigation only |

Source location does not collapse epistemic state. Every finding carries three
independent axes:

- evidence grade: direct, inferred, reported, or unknown;
- review state: extracted, curator-checked, manager-ratified, or
  manager-rejected;
- validity: provisional, current, challenged, historical, superseded, or
  invalid.

A reviewed inference is still inference. Direct evidence is not current truth
until manager review and closure coverage approve its projection.

## Authority

Managers own campaign intent, review decisions, ratification, retention, and
closure. Investigators produce run provenance. Curators split and normalize
claims, connect exact evidence, propose relations, and account for coverage.
Curators may not ratify, decide final evidence grade, edit truth, approve their
own packet, change retrieval profiles, delete artifacts, or close campaigns.
Curators also may not create work-item records or update the returned curator
run during curation submission. `spawnedWorkItems` contains proposal IDs only;
a manager may create those records as part of the bound review transaction,
and each created record must link back to that immutable review.

Measurement is evidence, not authority. In particular, a passing
normalized-versus-raw candidate cannot disable default report fallback. Only
the manager's `knowledge.archive-fallback.opt-in` transition may do so, after
the engine recomputes the exact ratified case-level evidence and atomically
binds the durable report, authorization receipt, and resulting policy. Startup
revalidates that binding; a missing, stale, or altered report or receipt makes
the opt-in policy invalid rather than silently trusted.

Role claims are validated with available host and engine signals. In 0.8 the
hook boundary protects against accidental direct edits; it is not proof
against a malicious caller. Structural invariants still refuse ratification
without review and truth projection outside closure.

## Review And Challenge

An intake binds every source to the exact canonical returned-run report path
and digest. Its inclusive line spans form an exhaustive partition from line 1
through the declared normalized line count: no gaps, overlaps, or unaccounted
tail are permitted. Each span uses the canonical
`path:<report-path>#L<start>-L<end>` handle. Candidate evidence must match a
targeted span exactly on source run, path, digest, line bounds, and object key.
Duplicate spans target an existing canonical finding. Coverage-only intakes
with zero candidates are valid and still close the source's review obligation.

Manager review is reconstructed from persisted canonical state at commit. The
submitted intake, candidate revisions and digests, packet rows, evidence
handles, and packet digest must all agree with that state. After the packet is
sealed, a review outcome may advance only review-owned metadata and disposition
fields. It may not substitute the claim, evidence, sources, relations, body,
or synthetic questions; a content correction requires a new candidate or
explicit follow-up work. Routine uncontested candidates may be reviewed in one
submission; conflicts and truth-touching candidates require individual
engagement. Every finding receives its own immutable decision receipt.

Closure counts a returned run as curated only when a reviewed intake
exhaustively covers that exact run report path and digest. Reusing a path with
different bytes, or reviewing only part of a multi-source intake, cannot satisfy
coverage. When closure itself queued the source for normalization, the campaign
cannot advance beyond the normalize stage until the manager also resolves that
exact queue item with the canonical curator-run, coverage, and review receipt.
This operational receipt does not add epistemic authority; it prevents closure
from retiring the source tree while normalization work still points at it.

Contrary evidence first marks a finding challenged. Retrieval surfaces the
conflict immediately and traces dependents. A later manager review may dismiss,
narrow, invalidate, historicize, or supersede it. Projected truth changes only
in an atomic closure transaction.

## Retention

Textual records and exact provenance are retained. Maintained assets stay in
their project-owned locations and are linked by digest. Large or external
inputs may remain by recoverable reference. Destruction requires explicit
manager approval and coverage showing what durable record replaces the
artifact's value.
