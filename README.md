# adiaz-claude-plugins

A plugin marketplace containing:

| Plugin | Hosts | Purpose |
|---|---|---|
| [cofounder](./plugins/cofounder/) | Claude Code | A structured founding session that produces a validated direction and execution handoff. |
| [re-discipline](./plugins/re-discipline/) | Claude Code and Codex | Evidence-based campaign and durable-knowledge management for reverse-engineering and research. |

The plugins are independent.

## Claude Code Marketplace

```text
/plugin marketplace add adiazpar/adiaz-claude-plugins
/plugin install cofounder@adiaz-claude-plugins
/plugin install re-discipline@adiaz-claude-plugins
```

For a local clone, use the clone's absolute path in `/plugin marketplace add`.

## Codex Marketplace

Only `re-discipline` currently exposes a native Codex manifest and marketplace
entry.

```powershell
codex plugin marketplace add adiazpar/adiaz-claude-plugins
codex plugin add re-discipline@adiaz-claude-plugins
```

For local development, replace the repository shorthand with the absolute path
to this clone. The Codex marketplace is
`.agents/plugins/marketplace.json`; the Claude Code marketplace is
`.claude-plugin/marketplace.json`.

After installation, initialize a project with
`/re-discipline:init-project` in Claude Code or
`$re-discipline:init-project` in Codex.

## Updating

Release changes to `re-discipline` by keeping its Claude Code and Codex
manifests on the same version. Claude Code uses its marketplace update and
plugin reload flow. Codex uses `codex plugin marketplace upgrade` followed by
`codex plugin add re-discipline@adiaz-claude-plugins`. Both hosts should start a
new session after loading a changed plugin.

## Layout

```text
adiaz-claude-plugins/
|-- .agents/plugins/marketplace.json
|-- .claude-plugin/marketplace.json
`-- plugins/
    |-- cofounder/
    `-- re-discipline/
        |-- .codex-plugin/plugin.json
        |-- .claude-plugin/plugin.json
        |-- hooks/
        |-- skills/
        `-- templates/
```

## License

MIT. See `plugins/cofounder/LICENSE`; the remaining plugins inherit the same
license.
