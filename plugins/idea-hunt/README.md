# idea-hunt

A Claude Code plugin that adds `/idea-hunt`, a slash command that scans free public web sources and surfaces a single defensible software-product idea per run with cited evidence.

## What it does

- Crawls free public sources (HN Algolia API, GitHub Search API, Reddit public JSON, Indie Hackers, YC RFS, Product Hunt)
- Generates candidate ideas via three discovery strategies: trend-chasing, unmet-demand mining, arbitrage
- Filters through six validation dimensions: demand, pain intensity, monetization evidence, competition strength, distribution reachability, why-now
- Outputs a funnel report ending in one ranked winner with full citations

No paid APIs. No credentials. All sources are free public web endpoints.

## Install

This plugin ships through the `adiaz-claude-plugins` marketplace.

**From GitHub (once published):**

```
/plugin marketplace add adiaz/adiaz-claude-plugins
/plugin install idea-hunt@adiaz-claude-plugins
```

**From a local clone:**

```
/plugin marketplace add /path/to/adiaz-claude-plugins
/plugin install idea-hunt@adiaz-claude-plugins
```

**Quick local iteration without the marketplace step:**

```bash
claude --plugin-dir /path/to/adiaz-claude-plugins/plugins/idea-hunt
```

While iterating, use `/reload-plugins` in-session to pick up changes without restarting.

## Usage

```
/idea-hunt                                 # cold mode, broad scan, medium depth
/idea-hunt "developer tools"               # narrow to a scope
/idea-hunt --depth=light                   # fast pass, ~2-3 min
/idea-hunt "B2B fitness" --depth=deep      # full validation + disprove pass, ~10-15 min
```

## History (project-scoped)

Past winners are appended to `<your-project>/.claude/idea-hunt-history.jsonl` — resolved from `git rev-parse --show-toplevel` with a `pwd` fallback. The file is created on first run.

History travels with the repo, not with your home directory, so different projects keep different idea ledgers. To re-surface a past idea, delete its line from that file.

If you'd rather not commit the history file, add this to `.gitignore`:

```
.claude/idea-hunt-history.jsonl
```

## Recommended: disable Claude Code auto-memory

Claude Code's built-in auto-memory system writes summary files to `~/.claude/projects/<slug>/memory/` and injects `MEMORY.md` into the system prompt every session. This plugin doesn't use that system — it has its own project-local history file (see above). If you don't want both running, turn auto-memory off in the project where you use `/idea-hunt`.

Add to `<your-project>/.claude/settings.json`:

```json
{
  "autoMemoryEnabled": false
}
```

(Plugins can't write this file for you — it's a per-project user setting. See [Claude Code memory docs](https://code.claude.com/docs/en/memory) for the authoritative reference.)

## Configuration

Scoring rubric, formula, and hard gates live in `skills/idea-hunt-scoring/SKILL.md`. Edit that file to tune scoring without touching the command logic.
