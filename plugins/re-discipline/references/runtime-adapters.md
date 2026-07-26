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
- Read `.re-discipline/config.json` only as the small bootstrap manifest. Read
  documented project-facing knowledge policy under
  `.re-discipline/settings/`.
- Use an external provider only when the user explicitly selected it or live
  config records that selection.
- Put concrete required tools, paths, deliverables, evidence standards,
  budgets, immutable context-pack path and digest, and exclusive live surfaces
  directly in the brief.
- Ask the user directly when a material choice is unresolved.
- Do not commit unless the user explicitly asks.

Use the same local knowledge server for every adapter. Preserve the distinction
between requested and effective retrieval profiles, and report every fallback.
Select only a separately benchmarked effective fallback profile; never
improvise a lane combination.

## Locate The Plugin

During a skill run, resolve the plugin root from the active `SKILL.md` path.
During a plugin hook, prefer `PLUGIN_ROOT`; `CLAUDE_PLUGIN_ROOT` is a compatible
fallback. Do not assume either variable exists during an interactive run.

## Knowledge Server Adapters And Launcher

The package has two deliberately thin MCP declarations because the hosts
resolve plugin commands differently:

- Claude Code reads `.mcp.json`. That adapter may use Claude's documented
  `${CLAUDE_PLUGIN_ROOT}` substitution.
- Codex reads the inline `mcpServers` object in
  `.codex-plugin/plugin.json`. That adapter uses its declared
  plugin-root-relative command and working directory; do not assume it expands
  `${CLAUDE_PLUGIN_ROOT}`, `${PLUGIN_ROOT}`, or another placeholder.

Both declarations launch the same packaged knowledge server with the same
asset root and protocol. They are host adapters, not separate indexes,
memories, or server implementations. Never copy the Claude declaration into
Codex or reinterpret a relative Codex command as project-relative.

When a workflow needs `<knowledge-runtime>` for `verify-pack` or an external
dispatch, resolve the command selected by the current host declaration against
the active plugin root, apply only that host's supported resolution, and
canonicalize the packaged launcher to an absolute path. Require an existing
file inside the same plugin installation that supplied the active skill. Never
select a bare command from `PATH`, a source-tree build, or a different cached
plugin version.

Every managed v0.6 external campaign or recruiting call must pass all three:

```text
-ContextPackPath <absolute immutable pack path inside the workspace>
-ExpectedContextPackDigest <independently retained sha256:64-lowercase-hex>
-KnowledgeRuntime <absolute active packaged launcher>
```

The dispatcher verifies the pack against that expected digest before provider
argument expansion, including on `-DryRun`, then includes the digest and
mismatch block rule in the external prompt. The host's already-running MCP
server does not make any dispatcher argument optional.

## Claude Code

Use Claude Code's native subagent tool when available and delegation is
allowed. Pass the exact `brief.md` and immutable `context-pack.json` paths, the
manager-retained expected digest, and the mismatch block rule; require
`report.md`. Select a worker capable of the brief's concrete tool and access
requirements. Use the host's normal background and result controls.

Claude Code automatically loads `.claude/CLAUDE.md`. Its
`@../.re-discipline/project-profile.md` import is canonical. Claude-only
project notes live outside the managed adapter block.

## Codex

Use `spawn_agent` for a native Codex drafter when collaboration is available
and allowed. Give it the exact workspace, brief, report path, required tools,
context-pack path and digest, granted paths, and drafter contract. Use
`send_message`, `followup_task`, `wait_agent`, or `interrupt_agent` only for
task lifecycle control.

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
`-DispatchId`. Both forms also pass the absolute immutable
`-ContextPackPath`, manager-retained `-ExpectedContextPackDigest`, and active
packaged `-KnowledgeRuntime`. The dispatcher never prepends a provider.

All campaign adapters produce
`active/<slug>/subagents/<workspace-id>/report.md`. Recruiting adapters produce
`.re-discipline/agents/recruiting/<candidate>/runs/<workspace-id>/report.md`.
Treat directory IDs as opaque. Legacy task-only and provider-prefixed campaign
paths remain valid inputs to review and lifecycle workflows.

Materialize the same bounded `context-pack.json` for native and external
drafters. External-provider limitations never justify a broader source grant,
pending-memory access, missing citations, or removal of the token budget.
