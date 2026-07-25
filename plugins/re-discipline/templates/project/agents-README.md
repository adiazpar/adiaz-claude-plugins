# External Agent Framework

## State

`config.json` is the only live roster and backend switch. `backend: native`
uses the active manager host's native delegation. Any other backend must name
a configured provider. A provider's presence under `providers` means it was
promoted; there is no separate status flag.

Only the user changes the live backend or authorizes a one-off external
provider.

## Provider Schema

Each provider entry requires `command` and `args`. It may also contain
`model`, `model_flag`, `sandbox_args`, and `bypass_args`:

```json
{
  "command": "provider-cli",
  "args": ["exec", "{model_args}", "{policy_args}", "{prompt}"],
  "model": "optional-model-id",
  "model_flag": "--model",
  "sandbox_args": ["--sandbox", "workspace-write"],
  "bypass_args": ["--dangerously-bypass-sandbox"]
}
```

Supported argument tokens are `{model_args}`, `{policy_args}`, `{root}`,
`{workspace}`, `{brief}`, `{report}`, `{lastmsg}`, and `{prompt}`. An explicit
dispatcher `-Model` overrides the configured `model`; otherwise the provider
CLI chooses its own default.

## Durable Provider Record

Every configured provider has exactly three durable Markdown records:

```text
.re-discipline/agents/providers/<provider>/
  profile.md
  scorecard.md
  teardown.md
```

`profile.md` contains provider-specific prompting guidance. `scorecard.md`
contains the evaluation and promotion judgment. `teardown.md` contains exact
cleanup instructions for any provider-specific machine configuration.

## Recruiting

Candidate state is transient:

```text
.re-discipline/agents/recruiting/<candidate>/
  candidate.md
  config.json
  profile.md
  scorecard.md
  teardown.md
  runs/
```

Candidate configuration is passed with `-ConfigPath`. Candidate `runs/` and
the entire candidate directory are deleted after promotion or rejection.

## Lifecycle

- Recruit: evaluate isolated candidate state without changing live config.
- Promote: add the provider to live config and retain only its three durable
  Markdown records.
- Reject: delete the candidate directory.
- Fire: remove the provider from live config and delete its provider directory
  after applying `teardown.md`.

Reject and fire create no retained historical artifact or event chronicle.

## Dispatch

The manager creates the workspace and brief through `delegate`. The dispatcher
uses sandbox arguments by default. `-Bypass` is valid only for a user-approved
dispatch and requires configured `bypass_args`.

The external drafter runs in
`active/<slug>/subagents/<provider>-<name>/`, follows the fixed
`.codex/external-drafter-contract.md`, and writes `report.md`.
`last_message.md` is copied there only as a fallback.

Check a live provider configuration before executing it:

```powershell
.re-discipline/agents/dispatch.ps1 -Provider <provider> -Slug <slug> -Name <name> -DryRun
```
