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

Role claims are validated with available host and engine signals. In 0.8 the
hook boundary protects against accidental direct edits; it is not proof
against a malicious caller. Structural invariants still refuse ratification
without review and truth projection outside closure.

## Review And Challenge

Intake coverage accounts for every claim span. Routine uncontested candidates
may be reviewed in one submission; conflicts and truth-touching candidates
require individual engagement. Every finding receives its own immutable
decision receipt.

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
