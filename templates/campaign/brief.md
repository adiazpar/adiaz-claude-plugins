# Run Brief

Run: `R-...`
Campaign: `C-...`
Primary work item: `W-...`
Actor and role: `<actor>` / `<investigator|reviewer|curator>`
Context pack: `<pack-id>`
Retained digest: `sha256:<digest>`

## Objective

<One precise objective and observable result.>

## Scope And Exclusions

- In scope: <paths, systems, questions>
- Excluded: <explicit boundaries>

## Required Sources And Tools

- <source or tool handle>

## Write Grant

- `active/<campaign>/runs/<run-id>/report.md`
- `active/<campaign>/runs/<run-id>/payload/**` when needed
- exact file: `<project-relative-path>`
- directory prefix: `<project-relative-directory>/**`

The manager submits those project grants as structured `exact` or `directory`
entries. The engine appends the authoritative grant list below this brief and
seals it into the brief, context-pack, and run digests. All other campaign
records and project paths are read-only.

## Required Output

Write `report.md` using the packaged report template. Register important
payload and changed project paths through the shared engine.

## Completion Test

<Observable acceptance test.>
