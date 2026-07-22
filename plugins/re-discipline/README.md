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
| `.re-discipline/project-profile.md` | Single source for project identity, framing, mission, source of record, portable tooling, roles, paths, and environment. |
| `.claude/CLAUDE.md` | Claude Code manager laws, loaded automatically by Claude Code. |
| `.claude/project-profile.md` | Claude Code-only configuration, MCP, memory, and local-path overlay. |
| `AGENTS.md` | Root role router for direct Codex managers versus briefed drafters. |
| `.codex/AGENTS.md` | Codex manager laws. |
| `.codex/project-profile.md` | Codex-only configuration, MCP, memory, sandbox, and local-path overlay. |
| `.codex/external-drafter-contract.md` | Restricted drafter role and report format. |

Yes, a new project gets `.codex/project-profile.md`. It is not a second copy of
the Claude profile and it is not where shared project facts live. It is an
overlay optimized for Codex-specific operation. Both hosts read the canonical
`.re-discipline/project-profile.md`, which prevents the two profiles from
drifting into conflicting descriptions of the project.

The initializer is migration-aware. It preserves unrelated existing
instructions, moves old root drafter rules into the dedicated contract, and
asks before resolving contradictory Claude and Codex profile facts.

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
`.agents/plugins/marketplace.json`. Restart the app, select the marketplace in
the Plugins Directory, and install `re-discipline`.

Codex requires review of non-managed hooks. Open `/hooks`, review the plugin's
`SessionStart` and `PreCompact` commands, and trust the current definitions.
Then start a new thread and invoke:

```text
$re-discipline:init-project
```

New or changed plugin skills and hooks are picked up at a new-thread boundary.

## Hooks

- `SessionStart` conditionally reminds an initialized project to run `onboard`.
- `PreCompact` conditionally reminds an active campaign to checkpoint or close.

The hooks stay silent outside a project containing the neutral profile, a
legacy Claude profile, or `docs/INDEX.md`. The package ships a POSIX helper and
a PowerShell 5.1 Windows override, so Codex does not send shell-test syntax to
PowerShell.

## Delegation

Native delegation is the default:

- Claude Code uses its native subagent tool.
- Codex uses `spawn_agent` when collaboration is available and allowed.

An external CLI provider is optional. `hire-agent` can scaffold the generic
`agents/` adapter layer, evaluate a candidate, and leave the final promotion to
the user through `decide-agent`. External dispatch is sandboxed by default;
bypass requires an explicit per-dispatch user decision.

## Updating

Keep `.claude-plugin/plugin.json` and `.codex-plugin/plugin.json` on the same
release version. Claude Code updates through its marketplace update and reload
flow. Codex refreshes Git marketplaces with
`codex plugin marketplace upgrade`, then reinstalls with
`codex plugin add re-discipline@adiaz-claude-plugins`. Start a new thread after
either host reloads the plugin.
