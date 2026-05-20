---
description: Evaluate whether the implicit ICP from the audit can be monetized at SaaS economics for the founder's profile (Phase 2b of the market-research methodology)
argument-hint: [implicit-icp] [founder-profile] [target-economics]
---

You are starting Phase 2b of the market-research methodology — evaluate whether the implicit ICP from the audit can be monetized at SaaS economics for the founder's profile.

## Step 1: Collect the three required inputs

The user supplied: `$ARGUMENTS`

The `profitability-agent` requires three inputs:

- **Implicit ICP description** (typically pasted from the prior `/research-icp-audit` output — should be a concrete profile of the implicit target user, e.g., "informal market stall vendor in Peru/Colombia with 1–3 employees, cash-dominant, uses WhatsApp Business, not formally registered")
- **Founder profile** (e.g., "US solo founder, no existing distribution, $0 customer-acquisition budget, technical background")
- **Target economics** (e.g., "$50-200/mo SaaS pricing, 12-month payback, target ARR $200k by Y2")

If any input was not supplied (positionally as `$1` `$2` `$3` or in the user's prompt), use AskUserQuestion to collect what is missing. Confirm all three before dispatching the agent.

## Step 2: Dispatch the profitability-agent

Before dispatching, inform the user that the evaluation will take approximately 15-25 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `profitability-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the three input values inline. The subagent's body (loaded automatically as its system prompt) describes the evaluation methodology, CRITICAL CONSTRAINTS, and required output structure; your Task `prompt` only needs to supply the three input values, e.g.:

> Evaluate whether the "informal market stall vendor in Peru/Colombia with 1–3 employees, cash-dominant, uses WhatsApp Business, not formally registered" market is profitable to serve as paid SaaS. Founder profile: US solo founder, no existing distribution, $0 customer-acquisition budget, technical background. Target economics: $50-200/mo SaaS pricing, 12-month payback, target ARR $200k by Y2. Follow your system prompt's critical constraints and produce the written report (~1500–2000 words).

The agent will run for 15-25 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve specificity and source citations exactly as the agent produced them.

Highlight the "honest profitability verdict" section and the "typical paid ARPU" figure — these are the highest-trust outputs from the evaluation.

## Methodology context

This command implements Phase 2b of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "The legitimacy of NO").

A valid output of this evaluation is "This segment does not monetize at SaaS economics for this founder profile." — do not soften an honest NO. If profitability looks viable, proceed to `/research-angle`. If the audit returns a NO, the next step depends on what the founder has already tried:
- **If this is the first NO** (or the founder hasn't yet evaluated adjacent ICPs the codebase could serve), proceed to `/research-adjacent-scan` to evaluate alternative ICPs.
- **If the founder has already evaluated adjacent ICPs and none work as a whole-product angle**, proceed to `/research-extraction` to evaluate what standalone product opportunities could be pulled from the codebase instead.
