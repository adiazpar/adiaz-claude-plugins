# Principle: Tool selection for research passes

Detection is not selection. When an agent is dispatched with MCP-based research tools available (Firecrawl, Exa, Perplexity, etc.), it inherits them automatically — the tools appear in the agent's tool list with `mcp__server_name__tool_name` naming. But the agent will not *preferentially use* the specialized tool unless the prompt directs it to. Without explicit guidance, agents default to the general-purpose primitives (WebSearch, WebFetch) even when better tools are installed and visible.

This is the methodology's most subtle and recently-noticed gap. This principle closes it. Every research-running agent prompt — ICP profitability, adjacent market scan, differentiated angle, devil's advocate, extraction evaluation — should require this file as reading and apply its tool-selection preferences before defaulting to general-purpose search.

## Tool preference by research task

| Research task | Preferred tool | Fallback | Why the preference matters |
|---|---|---|---|
| Systematic review mining across multiple pages (Capterra, G2, App Store, Play Store, Trustpilot) | **Firecrawl** (multi-page crawl with structured output) | WebFetch (one URL at a time) | WebFetch produces summarization drift on long review pages and can't crawl pagination. Firecrawl returns clean markdown across pages. |
| Semantic search across forums for varied phrasing of a pain point | **Exa** (neural embeddings, intent-aware) | WebSearch (keyword match) | The same complaint gets phrased 10 different ways. Exa aggregates them; WebSearch keyword-matches will miss most. |
| Single known URL lookup | **WebFetch** | — | Specialized tools add no value when the URL is already known. WebFetch is faster. |
| Time-sensitive web queries (current events, recent funding, recent product launches) | **WebSearch** with explicit current year in the query | — | WebSearch is well-suited to fresh queries; specialized scrapers add latency. |
| Library / framework / API documentation | **Context7** (`mcp__plugin_context7_context7__query-docs`) | WebFetch on official docs | Training-data drift on library APIs is common; Context7 is purpose-built for fresh docs. |
| Reddit thread analysis | **Apify Reddit Scraper** (if installed) | WebFetch on `reddit.com/r/X/top.json?limit=N` endpoints | Direct JSON endpoints work for small queries; Apify becomes necessary for systematic multi-sub mining. |
| App Store / Play Store data (listings, reviews, ratings) | **app-store-scraper** / **google-play-scraper** (npm libraries via Bash) | WebFetch on individual listing pages | npm scrapers handle pagination, structured fields, and review extraction; WebFetch returns rendered HTML that's noisy. |
| Conversational synthesis with citations | **Perplexity Sonar** (if installed) | WebSearch + manual synthesis | Perplexity returns synthesized answers with sources in one call; useful for "what does the public web say about X" questions. |
| Public news / press releases / blog posts (general web reading) | **WebSearch + WebFetch** combination | — | Standard reading pattern. No specialized tool needed. |
| Competitor codebase analysis (if relevant) | **GitHub MCP** (if installed) | WebFetch on `github.com/owner/repo` URLs | GitHub MCP can query issues, PRs, contributor stats systematically. |
| ICP density + reachability check (named accounts, decision-makers, firmographic filtering) | **Apollo.io** (installed via `/plugin`) | WebSearch on company names + LinkedIn lookups | Apollo aggregates 250M+ contacts with firmographic filters. WebSearch on category terms returns marketing content, not segment-density numbers. Phase 2 (ICP audit) and Phase 4 (verify angle reachability) both depend on this signal. |

## Workflow-state tools (a different category)

The tools below are *not* data-gathering — they hold the structured state of the research itself across multi-pass engagements. Use them when the research becomes complex enough that markdown notes no longer support the queries you need to ask ("which candidates passed Phase 4?", "which kills had reachability as the killer?").

| Workflow task | Preferred tool | Fallback | Why the preference matters |
|---|---|---|---|
| Multi-candidate state tracking across passes 1–6 | **Airtable** (installed via `/plugin`) | Markdown files in `active-research/` | Once you're running 4+ candidates in parallel, candidate state in markdown becomes hard to query. Airtable's structured schema + filtered views (one view per methodology phase) makes status, verdicts, and kill-reasons trivially queryable. |
| Structured pain-signal capture from many sources | **Airtable** linked table | Markdown bullets per candidate | When pain signals come from 3+ sources (reviews, Reddit, cold-DM) across multiple candidates, an Airtable `pain_signals` table linked to candidates beats nested markdown. |

## How to apply

Before starting any research pass:

1. **Check the agent tool list.** Identify which preferred tools are available. Look for `mcp__` prefixed tools (those are MCP-server-backed) AND CLI-based tools that the agent invokes via Bash. **Not every preferred tool is an MCP tool.** For example, the official Firecrawl Claude Code plugin (2026) ships as a CLI + skill bundle, not an MCP server — Firecrawl is invoked via Bash (`firecrawl scrape <url> -o <file>`) rather than as `mcp__plugin_firecrawl_*`. Confirm CLI tools are installed by running `which <toolname>` or by checking that the relevant skill (e.g., `firecrawl-scrape`) is loaded.
2. **Default to the preferred tool for each task.** Only fall back to general-purpose alternatives if the preferred is unavailable. Do not default to WebSearch/WebFetch when Firecrawl/Exa are installed.
3. **Be explicit in the output.** When a research pass cites a source, indicate which tool retrieved it. This makes downstream meta-analysis (was the research thorough?) much easier.
4. **Report what was unavailable.** If the preferred tool for a task wasn't installed and the fallback was used, mention it in the output. This signals to the human reviewer where output quality may have been bounded by tooling.

## Why this matters

Without this principle, the methodology's `tooling/recommended-tools.md` recommendations are aspirational — the human installs Firecrawl, but the agent prompt doesn't direct the agent to use it, so the agent reaches for WebSearch + WebFetch as before. The tool install delivers near-zero improvement to research output. The principle is what activates the investment.

It also helps in the opposite direction: when no specialized tools are installed, the principle clarifies that the fallback path is legitimate. Agents should not refuse to do research because Firecrawl isn't available; they should fall back cleanly, note the limitation, and proceed.

## Failure modes

1. **Tool installed but not selected.** Output quality degrades silently. The agent uses WebFetch where Firecrawl would be 10× more efficient. Mitigation: this principle plus the agent-prompt references to it.

2. **Tool selected but unavailable (not installed).** Agent should detect the absence and fall back gracefully, not error out. The agent's tool list at dispatch time is the source of truth for what exists.

3. **Tool selected at the wrong scope.** e.g., using Firecrawl to fetch a single known URL when WebFetch is faster. Match the tool to the task scale; not every research action needs the heavy-weight tool.

4. **Refusing to research because the preferred tool is missing.** The fallback is always legitimate. The principle prefers tools when present; it does not require them.

5. **Selecting tools the agent invented.** Only select from the agent's actual tool list. Don't write code that calls `mcp__firecrawl__scrape` if the agent doesn't have Firecrawl available — that's a hallucination error.

## See also

- `tooling/recommended-tools.md` — how to install the recommended tools, with current 2026 install commands
- `../active-research/TOOLS-STATUS.md` (in any active research workspace) — the current installation status for the project being researched
