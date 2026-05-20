# adiaz-claude-plugins

A Claude Code plugin marketplace with two plugins for solo founders doing product discovery and validation.

## Plugins

| Plugin | What it does | Slash commands |
|---|---|---|
| [**idea-hunt**](./plugins/idea-hunt/) | Scans free public web sources (HN, GitHub, Reddit, IH, YC RFS, Product Hunt) and surfaces one ranked software-product idea per run with cited evidence. No paid APIs. | `/idea-hunt` |
| [**market-research**](./plugins/market-research/) | Six-phase methodology for evaluating commercial viability of a digital product. Each phase has its own slash command and subagent. | `/research-demand-discovery`, `/research-icp-audit`, `/research-profitability`, `/research-adjacent-scan`, `/research-angle`, `/research-pressure-test`, `/research-extraction` |

The two plugins are complementary: `idea-hunt` finds a candidate, `market-research` decides whether it's a real business.

## Install

**From GitHub (once published):**

```
/plugin marketplace add adiaz/adiaz-claude-plugins
/plugin install idea-hunt@adiaz-claude-plugins
/plugin install market-research@adiaz-claude-plugins
```

**From a local clone:**

```
/plugin marketplace add /path/to/adiaz-claude-plugins
/plugin install idea-hunt@adiaz-claude-plugins
/plugin install market-research@adiaz-claude-plugins
```

You can install either plugin without the other.

## Layout

```
adiaz-claude-plugins/
├── .claude-plugin/
│   └── marketplace.json          ← marketplace manifest (lists both plugins)
├── plugins/
│   ├── idea-hunt/                ← installable plugin
│   └── market-research/          ← installable plugin
└── docs/                         ← dev plans, methodology essays, agent-prompt sources (not shipped)
    ├── idea-hunt/
    └── market-research/
```

The `docs/` directory is development artifacts and is NOT part of either installed plugin. It lives here so the source-of-truth for prompts, principles, and design specs travels with the repo.

## Recommended user setting: disable Claude Code auto-memory

Claude Code's built-in auto-memory system writes summary files to `~/.claude/projects/<slug>/memory/` and injects `MEMORY.md` into the system prompt every session. Neither plugin uses it — `idea-hunt` has its own project-local history file, and `market-research` writes its outputs into your active project. If you don't want both running, add this to your project's `.claude/settings.json`:

```json
{
  "autoMemoryEnabled": false
}
```

See the [Claude Code memory docs](https://code.claude.com/docs/en/memory) for details.

## License

MIT (see `plugins/market-research/LICENSE`). `idea-hunt` inherits the same license.
