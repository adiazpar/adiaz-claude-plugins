---
description: Evaluate standalone product opportunities pullable from the codebase (Phase 6 / alt-path of the market-research methodology). Use when no whole-product ICP works.
argument-hint: [codebase-path] [prior-kills] [founder-constraints]
---

You are starting Phase 6 of the market-research methodology — evaluate what standalone product opportunities can be pulled from a codebase after the whole-product ICP path has been exhausted.

## Step 1: Collect the three required inputs

The user supplied: `$ARGUMENTS`

The `extraction-agent` requires three inputs:

- **Codebase path** (`$1`) — absolute path to the root of the codebase to evaluate (e.g., `/Users/founder/projects/my-app`)
- **Prior kills history** (`$2`) — which whole-product candidates were evaluated and why each was killed — context for what hasn't worked (e.g., "ICP audit killed 'productivity app for freelancers' — no willingness to pay. Adjacent scan killed 'B2B team tool' — dominated by Notion/Linear. Pressure test killed 'niche booking plugin' — TAM too small for founder's constraints.")
- **Founder constraints** (`$3`) — time available, monetization goals, willingness to maintain vs. sell, open-source vs. proprietary preference (e.g., "8 weeks to MVP, needs $500/mo minimum within 6 months, prefers self-serve API or marketplace, open to open-source as portfolio/consulting lead-gen if nothing else passes")

If any input was not supplied (positionally as `$1` `$2` `$3` or in the user's prompt), use AskUserQuestion to collect what is missing. Confirm all three before dispatching the agent.

## Step 2: Dispatch the extraction-agent

Before dispatching, inform the user that the analysis will take approximately 30-45 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `extraction-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the three input values inline. The subagent's body (loaded automatically as its system prompt) describes the extraction-evaluation methodology, Critical constraints, and required output structure; your Task `prompt` only needs to supply the three input values, e.g.:

> Run a standalone product extraction evaluation on this codebase. Codebase path: /Users/founder/projects/my-app. Prior kills history: ICP audit killed "productivity app for freelancers" — no willingness to pay; pressure test killed "niche booking plugin" — TAM too small. Founder constraints: 8 weeks to MVP, needs $500/mo by month 6, open to open-source as portfolio/consulting lead-gen if nothing else passes. Read the actual code at the codebase path to verify what capabilities exist, then evaluate each extraction candidate on the dimensions your system prompt specifies. Follow your system prompt's Constraints and produce the output structure.

The agent will run for 30-45 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve the summary table, the top-2 candidate expansions, the kill list, and the honest answer to the framing question exactly as the agent produced them.

Highlight the honest answer to item 4 ("is there anything in this codebase that's a standalone product, or is the codebase only valuable as the whole app?") — this is the highest-trust output from the pass.

## Methodology context

This command implements Phase 6 (alt-path) of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "The legitimacy of NO").

**The reframe move.** Per SKILL.md's "Failure modes summary" section: when the whole-product ICP path is exhausted after 2-3 negative passes, the right move is not another ICP pivot — it is asking "what's the smallest, most defensible product I could pull out of this codebase and sell to someone in 8 weeks?" This command implements that reframe.

**Phase-specific NO criterion (the kill point for non-viable extractions):** A valid output of this command is "No standalone product can be honestly pulled out of this codebase that the founder can ship and maintain at their constraints." Per SKILL.md's "Legitimacy of NO": this is a valid endpoint, not a failure. Do not soften "nothing is viable" into "maybe with more time."

**Routing:**
- **Viable extraction candidate found (B+ or higher on agent scoring)** → end of methodology for this path (verdict: build extraction, redirect from whole-product ambition). The next step is execution — pursue as side project with a clear walk-criterion. This is not a new desk-research loop.
- **Marginal candidate found (B or B−)** → founder decides. Pursue only if they want closure with a small upside path. Do NOT pursue as a primary business move.
- **No extraction viable (nothing scores above C, or honest answer to item 4 is negative)** → end of methodology (verdict: accept negative result). The honest options are open-source release (for portfolio/consulting lead-gen) or sunset. Per SKILL.md's meta-pattern recognition: the methodology has done its job. Reference the "Legitimacy of NO" section.
