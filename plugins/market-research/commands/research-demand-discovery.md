---
description: Surface real problems from external data and recommend a starting point (Phase 0 — Demand Discovery, the from-nothing entry point to the market-research methodology)
argument-hint: [founder-profile] [founder-constraints] [familiar-domains] [starting-hypothesis?] [budget?]
---

You are starting Phase 0 of the market-research methodology — Demand Discovery. The founder has no committed candidate, no built codebase, and is looking for a problem worth attacking. Your job is to mine external evidence and surface a ranked list of problems with citations, then recommend exactly one as the starting point for downstream phases.

## Step 1: Collect the inputs

The user supplied: `$ARGUMENTS`

The `demand-discovery-agent` requires three inputs, plus two optional ones:

- **Founder profile** (`$1`, required) — who the founder is in operational terms: location, skills, prior distribution, employees, acquisition budget, sales comfort (e.g., "US solo founder, 8 years backend engineering, no existing distribution, no employees, $0 acquisition budget, comfortable with B2B technical sales")
- **Founder constraints** (`$2`, required) — what is and isn't on the table: MVP horizon, pricing target, revenue model, acquisition channels in/out (e.g., "12-week MVP horizon, $50-200/mo SaaS pricing target, prefer recurring revenue, willing to do cold outbound, NOT willing to do paid ads")
- **Familiar domains** (`$3`, required) — verticals the founder has real exposure to vs. cold zones (e.g., "deep retail/POS familiarity, some healthcare exposure via prior consulting, no fintech, no manufacturing")
- **Starting hypothesis** (`$4`, optional) — a domain to bias the scan toward, if any (e.g., "I'm curious about tools for small accounting firms"). If omitted, the agent runs a broad scan across the founder's familiar domains — do NOT prompt for this.
- **Budget** (`$5`, optional) — "cheap-and-broad" (1-2 hours, Tier 1+2 sources, 3-4 source categories) or "deep-and-narrow" (3-4 hours, all tiers, 6+ source categories). Defaults to "cheap-and-broad" if absent — do NOT prompt.

If any of the three required inputs was not supplied (positionally as `$1` `$2` `$3` or in the user's prompt), use AskUserQuestion to collect what is missing. AskUserQuestion is the fallback for the three required inputs ONLY — the two optional ones default silently. Confirm the three required inputs before dispatching the agent.

## Step 2: Dispatch the demand-discovery-agent

Before dispatching, inform the user that the analysis will take approximately 30-45 minutes — longer than other phases because this is the most tool-heavy pass in the methodology. The full report will be presented on completion.

Use the Task tool to dispatch the `demand-discovery-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the input values inline. The subagent's body (loaded automatically as its system prompt) describes the demand-discovery methodology, CRITICAL CONSTRAINTS, source hierarchy, anti-patterns, and required output structure; your Task `prompt` only needs to supply the input values, e.g.:

> Run a Demand Discovery (Phase 0) pass for this founder. Founder profile: US solo founder, 8 years backend engineering, no existing distribution, no employees, $0 acquisition budget, comfortable with B2B technical sales. Founder constraints: 12-week MVP horizon, $50-200/mo SaaS pricing target, prefer recurring revenue, willing to do cold outbound, NOT willing to do paid ads. Familiar domains: deep retail/POS familiarity, some healthcare exposure via prior consulting, no fintech, no manufacturing. Starting hypothesis: none supplied — scan broadly across the founder's familiar domains. Budget: cheap-and-broad (default). Follow your system prompt's CRITICAL CONSTRAINTS, source hierarchy, and required output structure. You ARE in exploratory + analytical mode — surface problems with evidence, do NOT propose solutions.

The agent will run for 30-45 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve the ranked list, the structured fields per candidate, the citation URLs, and the recommended next step exactly as the agent produced them.

Highlight the "Top 3 expanded" section and the recommended next step — those are the operational output of the pass. The longer 6-12 list is the evidence trail showing what was ranked below.

## Methodology context

This command implements Phase 0 of the market-research methodology — the from-nothing entry point that precedes the six structured phases. See the `market-research` skill, sections "The six phases", "Phase-to-command map", "The legitimacy of NO", and "Research tools".

**Phase-specific NO criterion:** "No problem surfaced with enough signal density + reachability + founder-fit. This is a valid output of the pass per SKILL.md's "The legitimacy of NO" section. Recommended path: scan a different domain or stop desk research and go talk to humans." Returning a NO is the pass succeeding at its job, not failing — telling the founder that desk research has not found a credible target in this scan saves months of misdirected work.

**Routing:**

- **If a viable problem is surfaced (#1 candidate scores well):** proceed to `/research-angle` with the surfaced problem + ICP + named alternatives as input. The natural next step is angle-formulation; pressure-testing comes only after an angle is named per SKILL.md's "Stance discipline rule".
- **If the surfaced #1 is borderline (segment unclear, WTP signal weak):** consider running `/research-profitability` first to validate unit economics for the founder profile against that segment before committing to angle-formulation.
- **If no problem qualifies (the honest NO):** the agent's report will recommend either a different scan domain (re-run `/research-demand-discovery` with adjusted inputs) or stopping desk research entirely (the founder should do 5-10 cold conversations with humans in industries they're curious about).

**Critical anti-pattern reminder:** this command surfaces PROBLEMS, not SOLUTIONS. The agent must name the problem and the buyer; it must NOT name the product. If the user asks "tell me what to build" after this command runs, redirect them to `/research-angle` — angle-formulation is the next phase, not part of Demand Discovery. The same problem has many possible solutions and the founder picks one in Phase 3, not before.
