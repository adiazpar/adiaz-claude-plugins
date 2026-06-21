# adiaz-claude-plugins

A Claude Code plugin marketplace. `cofounder` is a single-session founding tool that turns "I want to build something profitable" into a chosen direction and an actionable plan; `re-discipline` is a reusable knowledge-management discipline for reverse-engineering and research projects.

## Plugins

| Plugin | What it does | Slash commands |
|---|---|---|
| [**cofounder**](./plugins/cofounder/) | A single-session founding meeting between you and Claude — establish who you honestly are, get educated on a landscape with live evidence, converge on a direction, and leave with an actionable plan and a falsifiable first move. | `/cofounder` |
| [**re-discipline**](./plugins/re-discipline/) | Evidence-based knowledge management for reverse-engineering & research projects: where a file lives encodes its trust level (provisional / verified / historical), nothing becomes "truth" without DIRECT evidence (the "Wall"), work happens in "campaigns", and external AI agents can be hired as research drafters. `/init-project` drops the structure into any repo. | `/init-project`, `/onboard`, `/open-campaign`, `/delegate`, `/review-subagent`, `/promote-truth`, `/overturn`, `/close-campaign`, `/checkpoint-campaign`, `/hire-agent`, `/decide-agent` |

`cofounder` and `re-discipline` are independent: `cofounder` runs a founding session to choose and plan a profitable direction; `re-discipline` is a standalone methodology for running disciplined RE/research projects.

## Install

```
/plugin marketplace add adiazpar/adiaz-claude-plugins
/plugin install cofounder@adiaz-claude-plugins
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
    ├── cofounder/                ← installable plugin
    └── re-discipline/            ← installable plugin (skills + hooks + project templates)
```

Each plugin is fully self-documenting — design notes, principles, and prompt sources live inside the plugin itself (its `README.md`, `SKILL.md`, commands, and agents), not in a separate notebook.

## Recommended user setting: disable Claude Code auto-memory

Claude Code's built-in auto-memory system writes summary files to `~/.claude/projects/<slug>/memory/` and injects `MEMORY.md` into the system prompt every session. The product-discovery tooling doesn't use it — `cofounder` writes its briefs into your active project. (`re-discipline` *does* make use of project memory as a manager-ratified store.) If you don't want auto-memory running for a given project, add this to its `.claude/settings.json`:

```json
{
  "autoMemoryEnabled": false
}
```

See the [Claude Code memory docs](https://code.claude.com/docs/en/memory) for details.

## License

MIT (see `plugins/cofounder/LICENSE`). The other plugins inherit the same license.
