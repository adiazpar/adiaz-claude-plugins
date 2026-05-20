# Market Research Methodology (Claude Code Plugin)

> A six-phase methodology for evaluating commercial viability of digital products. Built for solo founders; adaptable to other founder profiles.

This plugin packages the market-research methodology as installable Claude Code components: one umbrella skill, six slash commands corresponding to the methodology's six structured agent passes, and six matching subagents that execute each phase with adapted prompt templates. Designed to work alongside (not bundle) marketplace plugins for Firecrawl, Exa, Airtable, and Apollo — falls back to general-purpose tools when those are absent.

## Install

**Local development:**
```bash
claude --plugin-dir /path/to/market-research-methodology/plugin
```

**Marketplace (future):**
```
/plugin install market-research
```

After install, reload plugins with `/reload-plugins` and verify the six commands appear in `/help`.

## External dependencies

The plugin works without these — agents fall back to WebSearch + WebFetch — but the methodology's research quality degrades meaningfully without Tier 1 tooling. Install in priority order:

| Tool | Install via | Used in | Purpose |
|---|---|---|---|
| **Firecrawl** (CLI + skill bundle, NOT MCP) | `/plugin install firecrawl@claude-plugins-official` then `npx -y firecrawl-cli@1.16.2 init -y --browser` | Phases 0, 2-5 | Multi-page web scraping with clean markdown output. Invoked via Bash (`firecrawl scrape <url> -o <file>`) not as an MCP tool. |
| **Exa MCP** | `/plugin install exa@claude-plugins-official` then authenticate | Phases 0, 2b, 2c, 4, 5 | Semantic search for varied-phrasing pain signals. |
| **Airtable MCP** | `/plugin install airtable@claude-plugins-official` then authenticate | All phases (state tracking) | Multi-candidate state, phase outputs, pain signals. See `resources/airtable-schema.json` for the suggested base schema. |
| **Apollo MCP** | `/plugin install apollo@claude-plugins-official` then authenticate | Phases 0, 2, 2c, 4 | ICP density + reachability checks (firmographics, named accounts, decision-makers). |
| **Perplexity** (raw MCP, optional) | Configure in `claude_desktop_config.json` | Phases 2b, 5 | Conversational synthesis with citations. |

**Credit awareness:** Firecrawl (~1500 cycle credits), Apollo (lead credits limited on free tier — ~95), Exa (per-search). Reserve Apollo lead credits for Phase 4 reachability checks specifically. See SKILL.md's "Tool-selection fallback matrix" section for the per-task tool routing.

## Setting up the Airtable base

The methodology uses Airtable as its persistence layer (state across multi-pass research engagements). The plugin includes a starter schema at `resources/airtable-schema.json` with three tables: Candidates, Phase Outputs, Pain Signals.

**Two setup paths:**

1. **Programmatic** (recommended if you have the Airtable MCP plugin installed): the subagent will use `mcp__plugin_airtable_airtable__create_base` and `create_field` to set up the base on first invocation, or you can ask Claude to "create the methodology base in Airtable from `resources/airtable-schema.json`".

2. **Manual**: open Airtable, create a base named "Market Research" with the three tables, fields, and choices listed in `resources/airtable-schema.json`. Save the base ID (format: `appXXXXXXXXXXXXXX`) somewhere subagents can read it — typically a `.claude/market-research.local.md` file in your project.

After setup, the base ID becomes per-user runtime config. Future versions of this plugin will support a `.claude/market-research.local.md` settings file that stores the base ID; for v0.1.1, you can either pass the base ID to commands as needed or hardcode it in a small wrapper.

## Usage

Seven slash commands cover the methodology — Phase 0 (Demand Discovery, the from-nothing entry point) plus the six structured agent passes (see the skill's "Phase-to-command map" section for the full phase-to-command mapping):

| Command | Phase | Inputs |
|---|---|---|
| `/research-demand-discovery` | 0 (Demand Discovery) | Founder profile, founder constraints, familiar domains, optional starting hypothesis, optional budget |
| `/research-icp-audit` | 2 (Audit) | Project name, root path, language/framework notes |
| `/research-profitability` | 2b (ICP profitability) | Implicit ICP, founder profile, target economics |
| `/research-adjacent-scan` | 2c (Adjacent market scan) | Codebase capabilities, codebase absences, killed ICP, founder domain familiarity, optional candidate list |
| `/research-angle` | 3 (Differentiated angle) | Candidate product, target segment, named alternatives |
| `/research-pressure-test` | 5 (Adversarial pressure test) | Named three-sentence angle from `/research-angle`, named alternatives, optional proxy data |
| `/research-extraction` | 6 alt-path (Extraction evaluation) | Codebase path, prior kills history, founder constraints |

### A greenfield engagement (no codebase, exploring what to build)

```
/research-demand-discovery  →  surfaces ranked problems with evidence
   ↓ recommends #1 candidate
/research-angle  →  names YOUR specific three-sentence angle for the surfaced problem
   ↓
/research-pressure-test  →  finds the killer
   ↓
[decide / commit / kill]
```

Three commands, ~1-2 days. Skips Phases 2/2b/2c/6 (those are brownfield-specific).

### A brownfield engagement (existing codebase, founder unsure who pays)

```
/research-icp-audit  →  identifies implicit ICP from code
/research-profitability  →  evaluates if implicit ICP monetizes
   ├─ pass → /research-angle → /research-pressure-test → verdict
   └─ NO → /research-adjacent-scan → /research-angle → /research-pressure-test → verdict
           └─ no viable adjacency → /research-extraction → verdict
```

Each pass produces a written report (~1500-2500 words). Log the verdict and evidence summary to your Airtable base as you go.

## What this plugin will not do

- It will not tell you that your idea is good.
- It will not produce confident TAM numbers. The data doesn't exist for most informal markets, and the data that does exist for loud markets is biased.
- It will not replace customer interviews. It tells you which customers are worth interviewing, not what they will say.
- It will not find a market that doesn't exist. If multiple honest passes return "no," the answer is no.
- It will not protect you from over-running. If you run pass after pass without ever committing or stopping, no methodology can save that.

NO is the methodology's most important output. See the skill's "The legitimacy of NO" section for the full elaboration.

## License

MIT.

## Author

Alex Diaz (<alexdiaz0923@gmail.com>)

Built on top of a personal market-research methodology repository. Plugin v0.1.1.
