# re-discipline

An evidence-based campaign and knowledge-management plugin for long-running
reverse-engineering and research projects. The same plugin runs in Claude Code
and Codex.

## Core Model

Directory location encodes epistemic status:

- `active/<slug>/` is provisional work and temporary evidence.
- `docs/truth/` contains current claims admitted through the DIRECT-evidence
  Wall with durable verification.
- `docs/history/chronicles/` is retrospective context, never current authority.
- `docs/backlog/` contains deferred briefs.
- Maintained source, tools, tests, fixtures, corpora, and reference material are
  durable project assets only while they have an active consumer or owner.

The lifecycle skills open, delegate, review, promote, overturn, checkpoint, and
close campaigns. Drafters investigate; the direct manager ratifies. At
closure, classify every campaign artifact as Maintain, Distill, or Delete:
maintain useful project assets, distill necessary meaning into truth, history,
or backlog, and delete the raw remainder.

## Generated Project Contracts

Run the initializer once in each project. It creates or reconciles:

| Path | Ownership |
|---|---|
| `.re-discipline/project-profile.md` | Single source for shared re-discipline laws plus project identity, framing, source of record, tooling, paths, and environment. |
| `.re-discipline/config.json` | Small strict-JSON bootstrap and recovery manifest. New projects select `shared-only` memory and proposal-only writes. |
| `.re-discipline/knowledge/README.md` | Two-line pointer: the knowledge system is machine-managed; ask the agent to change behavior. |
| `.re-discipline/knowledge/policy.jsonc` | Commented AI-curated source, context-budget, local-model, and local-telemetry policy, edited by the agent on user request. |
| `.re-discipline/knowledge/retrieval-profile.json` | Generated, content-hashed accepted project retrieval profile. Never hand-edit it. |
| `.re-discipline/memory/` | Tracked shared operational recall, with pending proposals isolated from accepted topics. |
| `.re-discipline/knowledge/evals/` | Ratified project retrieval judgments used for benchmark and calibration. |
| `.re-discipline/cache/` | Disposable local indexes, benchmark reports, and calibration candidates. |
| `.re-discipline/local-paths.md` | Single untracked machine-local path map shared by all manager hosts and maintained project tools. |
| `.re-discipline/agents/` | Always-present host-neutral provider configuration, recruiting, durable records, and dispatch. |
| `.claude/CLAUDE.md` | Thin Claude Code adapter with one native import of the canonical profile and preserved Claude-specific notes. |
| `.claude/settings.json` | Project-local Claude policy. New `shared-only` projects set `autoMemoryEnabled` to `false` while preserving unrelated settings. |
| `AGENTS.md` | Root role router for direct Codex managers versus briefed drafters. |
| `.codex/AGENTS.md` | Thin Codex adapter with the canonical-profile fallback and preserved Codex-specific notes. |
| `.codex/config.toml` | Project-local Codex policy. New `shared-only` projects disable memory generation and use while preserving unrelated settings. |
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

## Unified Project Knowledge

The local knowledge server gives managers and drafters one interface over
separately trusted project sources:

- `docs/truth/` remains verified current knowledge;
- `docs/history/` remains retrospective provenance;
- `active/*/CAMPAIGN.md` remains provisional;
- `docs/backlog/` remains deferred intent;
- `.re-discipline/memory/topics/` contains accepted operational recall;
- `.re-discipline/memory/proposals/` remains provisional and is excluded from
  normal search and context packs.

The server indexes source files with exact, lexical, graph, local embedding,
and local reranking lanes. Its dense lane uses a checksum-pinned, learned
Stanford GloVe model that is deterministically quantized and executed locally;
the reranker uses disclosed deterministic integer features. Indexes and
embeddings are disposable navigation data, never evidence or a source of
truth. Tier and access filters run before relevance ranking.

Claude Code and Codex use separate thin MCP declarations because their
plugin-root command resolution and declaration schemas differ. Claude uses
`.mcp.json` with its supported `${CLAUDE_PLUGIN_ROOT}` substitution. Codex
uses the inline `mcpServers` declaration in `.codex-plugin/plugin.json` with
a plugin-root-relative command and working directory; it is not assumed to
expand Claude or generic plugin-root placeholders. Both adapters launch the
same packaged local server and share the same project index and policies.

Embeddings and reranking run locally. The shipped release has no remote-model
or external-source grant surface, and tracked project settings cannot grant
network transmission. If a packaged model is unavailable, retrieval uses only
a separately benchmarked effective fallback profile and reports the requested
profile, effective profile, active lanes, models, and fallback reason. Any
future optional provider, external-root, or hardware grant must be introduced
as explicit machine-local policy rather than a tracked-project capability.

### State And Ownership

The knowledge system owns its machine-managed state under
`.re-discipline/knowledge/`; users change behavior by asking the agent in
plain language (`references/reporting.md`):

- the agent edits `policy.jsonc` on user request
  (`references/knowledge-internals.md` documents every field);
- calibration and profile-decision workflows generate
  `retrieval-profile.json`;
- keep the root `config.json` tiny enough for hooks to recover and validate
  without loading the full knowledge runtime.

Accepted project profiles and ratified project eval cases are tracked and
shared with the repository. Candidate profiles, benchmark output, aggregate
metrics, and indexes remain disposable local state. The checksum-pinned model
artifact ships once with the plugin and is never copied into an initialized
project. Global retrieval defaults change only in this plugin repository and
ship in a new plugin release.

### Measurement And Decisions

The operating rule is:

> Indexing happens automatically. Benchmarking measures. Calibration
> proposes. Promotion changes behavior.

| Workflow | Who may run it | Production change |
|---|---|---|
| Managed bootstrap recovery | Automatic project hook or explicit `preflight` | Restores only missing managed files and reconciles project-local host memory-policy fields |
| Read-only knowledge health/freshness | SessionStart after recovery, `onboard`, or `status` | None |
| `benchmark-knowledge` | Any user | None; writes a local cache report |
| `calibrate-knowledge` | Direct manager or project maintainer after an explicit request | None; writes a candidate |
| `decide-retrieval-profile` | Direct manager with an explicit user decision | May promote, reject, retain, or roll back |
| Global calibration/promotion | Plugin maintainer in this source repository | Ships only after cross-project CI and release gates |
| `review-memory` | Direct manager with an explicit user decision | May accept or reject one pending memory proposal |

Ordinary sessions never hide a full benchmark, calibration sweep, model
download, end-to-end agent trial, memory acceptance, or profile promotion.
Subagents may test finalists or investigate failures, but they never establish
gold labels or run once per weight combination.

A calibration candidate is unsigned proposal state: it contains no approval
receipt and cannot activate itself. On explicit promotion,
`decide-retrieval-profile` stamps the decision receipt, records the candidate
digest and benchmark evidence, recomputes the accepted profile content hash,
validates the completed profile, and replaces it atomically.

### Shared Memory

Managers, drafters, campaign checkpoints, and closure reviews may propose
navigation knowledge, workflow preferences, recurring failure patterns,
useful commands, durable decision pointers, and continuity hints. Proposals
remain under `.re-discipline/memory/proposals/` until `review-memory` presents
one exact candidate and the user accepts or rejects it.

Acceptance distills operational recall into `memory/topics/` and updates
`memory/INDEX.md`. Rejection records a campaign-scoped decision in
`CAMPAIGN.md`, or a non-campaign decision under `## Proposal decisions` in the
memory index. Shared memory never substitutes for DIRECT evidence or durable
truth verification. Re-discipline does not synchronize or write Claude and
Codex native memory stores.

### Context Packs

`delegate` compiles one immutable, token-budgeted `context-pack.json` in every
drafter workspace. It records source paths, lines, hashes, trust tiers, corpus
generation, requested and effective profiles, active lanes, model identities,
fallback reason, and a pack digest. The exact same pack is available to native
Claude/Codex subagents and configured external drafters. Pending memory
proposals are never included. Every managed v0.6 external campaign and
recruiting dispatch passes the pack's absolute workspace path and the
canonical absolute active packaged launcher to `dispatch.ps1`; the dispatcher
requires the digest retained independently by the manager and verifies it
before provider launch, including on dry runs. A digest read from the same
workspace file is never sufficient verification. Retrieved passages are
evidence and data, not executable instructions; the canonical profile, brief,
and drafter contract remain the instruction boundary.

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

- `SessionStart` reminds Claude to run `onboard`, injects the complete
  canonical profile into trusted Codex sessions, performs cheap managed
  recovery, and runs a bounded read-only knowledge `status`. It never builds
  or reconciles the index.
- `PreCompact` conditionally reminds an active campaign to checkpoint or close.
- `PostCompact` rehydrates compact campaign handles and a bounded orientation
  reminder. Hosts that report compaction as a resumed `SessionStart` receive
  the same compatibility path.
- `SubagentStart` injects only the generic drafter boundary and project
  knowledge reminder. The task-specific immutable context pack is still
  selected and materialized by `delegate`.

Hooks do not build vectors, download models, run full benchmarks, calibrate
weights, or change accepted memory or retrieval profiles.

The hooks stay silent outside a project containing the neutral profile or
bootstrap, or a legacy host profile. Legacy-only projects receive a
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
