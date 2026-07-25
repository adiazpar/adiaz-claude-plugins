# Runtime Adapters

Re-discipline defines one workflow and maps it onto the active host. Skills
must not infer a host from a directory name, provider name, or stale
configuration. Detect it from the session's available tools and loaded manager
adapter.

## Shared Rules

- Read `.re-discipline/project-profile.md` for shared laws and project facts.
- Read machine-local values only from the untracked
  `.re-discipline/local-paths.md` when the task requires them.
- Read the active manager adapter for host-specific tools and configuration.
- Read `.re-discipline/agents/config.json`; `native` means the active host.
- Use an external provider only when the user explicitly selected it or live
  config records that selection.
- Put concrete required tools, paths, deliverables, evidence standards,
  budgets, and exclusive live surfaces directly in the brief.
- Ask the user directly when a material choice is unresolved.
- Do not commit unless the user explicitly asks.

## Locate The Plugin

During a skill run, resolve the plugin root from the active `SKILL.md` path.
During a plugin hook, prefer `PLUGIN_ROOT`; `CLAUDE_PLUGIN_ROOT` is a compatible
fallback. Do not assume either variable exists during an interactive run.

## Claude Code

Use Claude Code's native subagent tool when available and delegation is
allowed. Pass the complete brief or exact `brief.md` path and require
`report.md`. Select a worker capable of the brief's concrete tool and access
requirements. Use the host's normal background and result controls.

Claude Code automatically loads `.claude/CLAUDE.md`. Its
`@../.re-discipline/project-profile.md` import is canonical. Claude-only
project notes live outside the managed adapter block.

## Codex

Use `spawn_agent` for a native Codex drafter when collaboration is available
and allowed. Give it the exact workspace, brief, report path, required tools,
granted paths, and drafter contract. Use `send_message`, `followup_task`,
`wait_agent`, or `interrupt_agent` only for task lifecycle control.

Codex loads root and nested `AGENTS.md` instructions. Put an
`AGENTS.override.md` in an external drafter's workspace and run that CLI with
the workspace as its working directory. Native workers may share the manager's
working directory, so their task message must explicitly name
`.codex/external-drafter-contract.md` and `brief.md`.

Codex has no native `AGENTS.md` include syntax. When the plugin hook is enabled
and trusted, its `SessionStart` handler injects the complete canonical profile.
Root and nested instructions require an explicit profile read as fallback.
Codex-only notes live outside the managed block in `.codex/AGENTS.md`.

## External Providers

The normalized core always exists under `.re-discipline/agents/`. Invoke
`.re-discipline/agents/dispatch.ps1` only for a configured provider or a
user-authorized candidate config. Live provider prompting guidance is at
`.re-discipline/agents/providers/<provider>/profile.md`; candidate guidance is
adjacent to its candidate config.

The manager creates one completed chronological workspace ID before invoking an
external adapter. Campaign dispatch passes that exact ID with `-Slug` and
`-DispatchId`; recruiting dispatch passes it with `-RecruitingCandidate` and
`-DispatchId`. The dispatcher never prepends a provider.

All campaign adapters produce
`active/<slug>/subagents/<workspace-id>/report.md`. Recruiting adapters produce
`.re-discipline/agents/recruiting/<candidate>/runs/<workspace-id>/report.md`.
Treat directory IDs as opaque. Legacy task-only and provider-prefixed campaign
paths remain valid inputs to review and lifecycle workflows.
