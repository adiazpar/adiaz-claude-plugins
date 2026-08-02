# External Agent Framework

`config.json` is the only live provider roster and backend switch. `native`
uses the active host's worker mechanism. Any other backend must name a
configured provider selected by the user.

Each provider defines a command, argument template, optional model selection,
verified sandbox arguments, and optional explicitly approved bypass arguments.
Durable provider records are `profile.md`, `scorecard.md`, and `teardown.md`.

## Recruiting

Candidate evaluations live under:

```text
.re-discipline/agents/recruiting/<candidate>/
  candidate.md
  config.json
  profile.md
  scorecard.md
  teardown.md
  runs/<run-id>/
```

Each run has `run.json`, `brief.md`, verified `context-pack.json`, terminal
`report.md`, and optional lazy `payload/`. No categorical workspace tree is
created. Important files are registered in the run record.

## Campaign Dispatch

The `delegate` skill prepares a run transactionally under
`active/<campaign>/runs/<run-id>/`. The dispatcher receives the existing run
ID and may only translate provider command syntax. It verifies the pack with
the active packaged runtime before launch and cannot create records, change
grants, or infer state.

```powershell
.re-discipline/agents/dispatch.ps1 `
  -Provider <provider> `
  -Slug <slug> `
  -RunId <run-id> `
  -ExpectedContextPackDigest <sha256:64-lowercase-hex> `
  -KnowledgeRuntime <absolute-active-packaged-launcher> `
  -DryRun
```

The run must already contain valid `run.json`, `brief.md`,
`AGENTS.override.md`, and `context-pack.json`. A missing or mismatched pack
blocks launch. `-DryRun` still validates every prerequisite.

Candidate promotion, rejection, and removal are applied only by
`decide-agent` after explicit user choice. Provider cleanup follows the exact
teardown record and never changes manager adapters or another provider.
