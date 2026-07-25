# Runtime Adapters

Re-discipline defines one workflow and maps it onto the active host. Skills
must not assume a host from a directory name, provider name, or stale project
configuration. Detect it from the tools available in the current session and
the manager adapter that loaded.

## Shared Rules

- Read `.re-discipline/project-profile.md` for shared laws and project facts.
- Read the active manager adapter for host-specific tools and configuration.
- Treat an absent or `native` delegation backend as the current host.
- Use an external provider only when the user explicitly selected it or a
  project setting records that selection.
- Ask the user directly when a real choice is unresolved. Do not require a
  host-specific question tool.
- Do not commit unless the user explicitly asks.

## Locate The Plugin

During a skill run, resolve the plugin root from the active `SKILL.md` path.
During a plugin hook, prefer `PLUGIN_ROOT`; `CLAUDE_PLUGIN_ROOT` is a compatible
fallback supported by both packaged hosts. Do not assume either variable exists
during an interactive skill run.

## Claude Code

Use Claude Code's native subagent tool when it is available and the user has
asked for delegation. Pass the complete brief or an exact `brief.md` path.
Choose capability by role and current availability; do not pin model aliases in
the reusable skill. Use the host's normal background and result controls.

Claude Code automatically loads `.claude/CLAUDE.md`. The project profile it
imports with `@../.re-discipline/project-profile.md` is canonical. Claude-only
project notes live outside the managed adapter block in the same file.

## Codex

Use `spawn_agent` for a native Codex drafter when collaboration tools are
available and delegation is allowed by the current user and project
instructions. Give the agent the exact workspace, brief, report path, and
drafter contract. Use `send_message`, `followup_task`, `wait_agent`, or
`interrupt_agent` only for lifecycle control, not to bypass the report.

Codex loads root and nested `AGENTS.md` instructions. Put an
`AGENTS.override.md` in an external drafter's workspace and run that external
CLI with the workspace as its working directory. Native `spawn_agent` workers
may share the manager's working directory, so their task message must also tell
them to read `.codex/external-drafter-contract.md` and their `brief.md`.

Codex has no native `AGENTS.md` include syntax. When the plugin hook is enabled
and trusted, its `SessionStart` handler injects the complete canonical profile
for `startup`, `resume`, `clear`, and `compact`. Root `AGENTS.md` and
`.codex/AGENTS.md` still require an explicit canonical-profile read as the
fallback when hooks are disabled, untrusted, or unavailable. Codex-only
project notes live outside the managed adapter block in `.codex/AGENTS.md`.

## External Providers

An external CLI is an optional third adapter, not the default for either host.
Use `agents/dispatch.ps1` or the project's documented dispatcher only after the
provider is configured and explicitly selected. Prefix its workspace name with
the provider so report provenance is visible.

All adapters produce the same artifact:
`active/<slug>/subagents/<name>/report.md`. Review it with `review-subagent`
before promoting any claim.
