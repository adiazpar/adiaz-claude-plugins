# adiaz-claude-plugins

A Claude Code plugin marketplace. Two plugins for solo-founder product discovery & validation, plus a reusable knowledge-management discipline for reverse-engineering and research projects.

## Plugins

| Plugin | What it does | Slash commands |
|---|---|---|
| [**idea-hunt**](./plugins/idea-hunt/) | Scans free public web sources (HN, GitHub, Reddit, IH, YC RFS, Product Hunt) and surfaces one ranked software-product idea per run with cited evidence. No paid APIs. | `/idea-hunt` |
| [**market-research**](./plugins/market-research/) | Six-phase methodology for evaluating commercial viability of a digital product. Each phase has its own slash command and subagent. | `/research-demand-discovery`, `/research-icp-audit`, `/research-profitability`, `/research-adjacent-scan`, `/research-angle`, `/research-pressure-test`, `/research-extraction` |
| [**re-discipline**](./plugins/re-discipline/) | Evidence-based knowledge management for reverse-engineering & research projects: where a file lives encodes its trust level (provisional / verified / historical), nothing becomes "truth" without DIRECT evidence (the "Wall"), work happens in "campaigns", and external AI agents can be hired as research drafters. `/init-project` drops the structure into any repo. | `/init-project`, `/onboard`, `/open-campaign`, `/delegate`, `/review-subagent`, `/promote-truth`, `/overturn`, `/close-campaign`, `/checkpoint-campaign`, `/hire-agent`, `/decide-agent` |

`idea-hunt` and `market-research` are complementary: one finds a candidate, the other decides whether it's a real business. `re-discipline` is a standalone methodology for running disciplined RE/research projects — independent of the other two.

## Install

```
/plugin marketplace add adiazpar/adiaz-claude-plugins
/plugin install idea-hunt@adiaz-claude-plugins
/plugin install market-research@adiaz-claude-plugins
/plugin install re-discipline@adiaz-claude-plugins
```

**From a local clone**, swap the first line for `/plugin marketplace add /path/to/adiaz-claude-plugins`. You can install any plugin without the others.

For `re-discipline`, after installing run `/init-project` in a repo to scaffold the structure (or to adopt an existing project).

## Updating

Plugin updates are **version-gated**: Claude Code only re-pulls a plugin when its `plugin.json` `version` changes. So to ship a change to users:

1. Edit the plugin, **bump `version`** in its `.claude-plugin/plugin.json`, commit + push.
2. Users run `/plugin marketplace update adiaz-claude-plugins` then `/reload-plugins` to apply (with `autoUpdate` enabled in their marketplace settings, the pull happens automatically; the reload applies it).

Changing files *without* bumping the version will leave installed copies stale — Claude Code reports "already at the latest version."

## Layout

```
adiaz-claude-plugins/
├── .claude-plugin/
│   └── marketplace.json          ← marketplace manifest (lists all plugins)
└── plugins/
    ├── idea-hunt/                ← installable plugin
    ├── market-research/          ← installable plugin
    └── re-discipline/            ← installable plugin (skills + hooks + project templates)
```

Each plugin is fully self-documenting — design notes, principles, and prompt sources live inside the plugin itself (its `README.md`, `SKILL.md`, commands, and agents), not in a separate notebook.

## Recommended user setting: disable Claude Code auto-memory

Claude Code's built-in auto-memory system writes summary files to `~/.claude/projects/<slug>/memory/` and injects `MEMORY.md` into the system prompt every session. The product-discovery plugins don't use it — `idea-hunt` has its own project-local history file, and `market-research` writes its outputs into your active project. (`re-discipline` *does* make use of project memory as a manager-ratified store.) If you don't want auto-memory running for a given project, add this to its `.claude/settings.json`:

```json
{
  "autoMemoryEnabled": false
}
```

See the [Claude Code memory docs](https://code.claude.com/docs/en/memory) for details.

## License

MIT (see `plugins/market-research/LICENSE`). The other plugins inherit the same license.
