# External Agent Adapters

This directory is optional. Native Claude Code and Codex delegation does not
use it. It exists only for external CLI providers that have passed the
`hire-agent` evaluation and were promoted by an explicit user decision.

## Routing

`config.json` is the roster and backend switch:

- `backend: native` uses the current manager host's native subagent adapter.
- `backend: <provider>` selects a promoted provider under `providers`.

Only the user changes this selection. A one-off provider named in the user's
request is also valid without changing the default.

## Provider Schema

Each provider entry contains:

```json
{
  "enabled": true,
  "promoted": true,
  "command": "provider-cli",
  "args": ["exec", "{model_args}", "{policy_args}", "{prompt}"],
  "model_flag": "--model",
  "model_preference": [],
  "default_model": null,
  "sandbox_args": ["--sandbox", "workspace-write"],
  "bypass_args": ["--dangerously-bypass-sandbox"],
  "instructions_file": ".codex/external-drafter-contract.md",
  "profile": "profiles/provider.md"
}
```

Supported argument tokens are `{model_args}`, `{policy_args}`, `{root}`,
`{workspace}`, `{brief}`, `{report}`, `{lastmsg}`, and `{prompt}`.

The dispatcher uses `sandbox_args` by default. It expands `bypass_args` only
when called with `-Bypass`, which must correspond to an explicit user decision
for that dispatch.

## Contract

The manager creates the workspace and brief through `delegate`. The external
CLI runs in `active/<slug>/subagents/<provider>-<name>/`, where
`AGENTS.override.md` selects the drafter role. The expected result is
`report.md`; `last_message.md` is copied there only as a fallback.

Run a configuration check before a real dispatch:

```powershell
agents/dispatch.ps1 -Provider <provider> -Slug <slug> -Name <name> -DryRun
```
