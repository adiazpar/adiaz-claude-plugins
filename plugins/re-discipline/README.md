# re-discipline

An evidence-based campaign and knowledge-management plugin for long-running
reverse-engineering and research projects. The same plugin runs in Claude Code
and Codex.

## Core Model

Directory location encodes epistemic status:

- `active/<slug>/` is provisional work.
- `docs/truth/` contains claims admitted through the DIRECT-evidence Wall.
- `docs/history/chronicles/` is retrospective context, never current authority.
- `archive/` preserves irreproducible evidence.
- `docs/backlog/` contains deferred briefs.

The lifecycle skills open, delegate, review, promote, overturn, checkpoint, and
close campaigns. Drafters investigate; the direct manager ratifies.

## Generated Project Contracts

Run the initializer once in each project. It creates or reconciles:

| Path | Ownership |
|---|---|
| `.re-discipline/project-profile.md` | Single source for shared re-discipline laws plus project identity, framing, source of record, tooling, paths, and environment. |
| `.re-discipline/local-paths.md` | Single untracked machine-local path map shared by all manager hosts and maintained project tools. |
| `.re-discipline/agents/` | Always-present host-neutral provider configuration, recruiting, durable records, and dispatch. |
| `.claude/CLAUDE.md` | Thin Claude Code adapter with one native import of the canonical profile and preserved Claude-specific notes. |
| `AGENTS.md` | Root role router for direct Codex managers versus briefed drafters. |
| `.codex/AGENTS.md` | Thin Codex adapter with the canonical-profile fallback and preserved Codex-specific notes. |
| `.codex/external-drafter-contract.md` | Restricted drafter role and report format. |

Every initialized project has one canonical project profile:
`.re-discipline/project-profile.md`. Claude loads it through its native `@`
import. A trusted Codex `SessionStart` hook injects the complete profile, while
root and nested `AGENTS.md` instructions provide an explicit-read fallback.
Host-specific configuration stays in project-owned sections of the applicable
manager adapter. Shared laws never fork by host.

The initializer adds `.re-discipline/local-paths.md` to the project
`.gitignore` before creating it. During migration it merges legacy
`.claude/local-paths.md` and `.codex/local-paths.md` assignments, reports
conflicting variable names without exposing private values, updates maintained
readers, and removes the legacy files only after coverage is verified.
Session hooks continue to inject only the canonical project profile; they
never inject the private local-path file into manager context.

The state model is manager-neutral. Claude Code and Codex are the packaged
native adapters today; another manager host needs only a thin loader/tool
adapter for its instruction and delegation surfaces. It does not get a
different project profile or provider state model.

The initializer is migration-aware. It preserves unrelated existing
instructions, moves old root drafter rules into the dedicated contract, and
asks before resolving contradictory legacy profile facts. Legacy
`.claude/project-profile.md` and `.codex/project-profile.md` files are recovery
inputs only and are removed after the meaning-preservation gate succeeds.

## Install In Claude Code

From the published repository:

```text
/plugin marketplace add adiazpar/adiaz-claude-plugins
/plugin install re-discipline@adiaz-claude-plugins
```

For a local clone, use its absolute path in the marketplace command. Start the
initializer with:

```text
/re-discipline:init-project
```

## Install In Codex

Codex has plugin marketplaces and a native plugin manifest. From a local clone:

```powershell
codex plugin marketplace add C:\path\to\adiaz-claude-plugins
codex plugin add re-discipline@adiaz-claude-plugins
```

For the Git repository, the marketplace source can be the repository shorthand:

```powershell
codex plugin marketplace add adiazpar/adiaz-claude-plugins
codex plugin add re-discipline@adiaz-claude-plugins
```

In the ChatGPT desktop app, the repo marketplace is
`.agents/plugins/marketplace.json`. That `.agents/` path is Codex plugin
marketplace metadata, not re-discipline scratch. Re-discipline campaign
workspaces remain under `active/`, and hiring state remains under
`.re-discipline/agents/`. Restart the app, select the marketplace in the
Plugins Directory, and install `re-discipline`.

Codex requires review of non-managed hooks. Open `/hooks`, review the plugin's
`SessionStart` and `PreCompact` commands, and trust the current definitions.
Then start a new thread and invoke:

```text
$re-discipline:init-project
```

New or changed plugin skills and hooks are picked up at a new-thread boundary.

## Hooks

- `SessionStart` reminds Claude to run `onboard` and injects the complete
  canonical profile into trusted Codex sessions.
- `PreCompact` conditionally reminds an active campaign to checkpoint or close.

The hooks stay silent outside a project containing the neutral profile, a
legacy host profile, or `docs/INDEX.md`. Legacy-only projects receive a
migration/recovery reminder. The package ships a POSIX helper and a PowerShell
5.1 Windows override, so Codex does not send shell-test syntax to PowerShell.

## Delegation

Native delegation is the default:

- Claude Code uses its native subagent tool.
- Codex uses `spawn_agent` when collaboration is available and allowed.

Every new campaign workspace is named
`YYYY-MM-DDTHH-mm-ssZ-<executor>-<task>`, where executor identifies the worker
rather than the manager or exact model. Candidate evaluations use the same
signature under `.re-discipline/agents/recruiting/<candidate>/runs/`. Existing
task-only and provider-prefixed campaign directories remain valid and are not
renamed.

The external-provider core is always initialized at `.re-discipline/agents/`
with `backend: native` and no configured providers. `hire-agent` evaluates a
candidate in isolated recruiting state and leaves the final promotion to the
user through `decide-agent`. Live config is the only provider list. Every
configured provider retains exactly `profile.md`, `scorecard.md`, and
`teardown.md`; raw candidate runs are discarded after a decision. External
dispatch is sandboxed by default, and bypass requires an explicit
per-dispatch user decision.

## Updating

Keep `.claude-plugin/plugin.json` and `.codex-plugin/plugin.json` on the same
release version. Claude Code updates through its marketplace update and reload
flow. Codex refreshes Git marketplaces with
`codex plugin marketplace upgrade`, then reinstalls with
`codex plugin add re-discipline@adiaz-claude-plugins`. Start a new thread after
either host reloads the plugin.
