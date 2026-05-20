---
description: Scan adjacent markets the codebase's capabilities could serve (Phase 2c of the market-research methodology — invoked when the audit/profitability returned a non-monetizable implicit ICP)
argument-hint: [capabilities] [absences] [killed-icp] [domain-familiarity] [candidate-list?]
---

You are starting Phase 2c of the market-research methodology — scan adjacent markets the existing codebase capabilities could serve with moderate (not full) rework, ranked by attractiveness for the founder's specific profile.

## Step 1: Collect inputs

The user supplied: `$ARGUMENTS`

The `adjacent-scan-agent` requires four inputs and accepts one optional input:

- **Codebase capabilities** (`$1`) — positive: what the code does (e.g., "inventory tracking, multi-location support, SKU-level reporting, mobile-first UI, WhatsApp Business integration")
- **Codebase absences** (`$2`) — negative: what the code does NOT do and would require significant rework (e.g., "no appointment-booking, no payroll, no METRC integration")
- **Killed ICP** (`$3`) — what `/research-icp-audit` or `/research-profitability` found that doesn't monetize (e.g., "informal market stall vendors in Peru/Colombia: too fragmented, no willingness to pay SaaS price, founder can't reach them remotely")
- **Founder's industry-domain familiarity** (`$4`) — e.g., "deep retail/POS familiarity; no healthcare exposure; comfortable with B2B sales, no consumer marketing experience"
- **Candidate list (optional)** (`$5`) — 6-12 specific adjacent segments the founder wants the agent to evaluate. If provided, the agent will score these and may add 2-3 non-obvious additions. If not provided, the agent generates the candidate list from scratch. Example: "auto shops, beauty salons, gyms, dentists, vet clinics, food trucks"

If any of the four required inputs was not supplied (positionally as `$1` `$2` `$3` `$4` or in the user's prompt), use AskUserQuestion to collect what is missing. The candidate list (`$5`) is optional — if the user did not provide it, do NOT prompt for it; accept its absence and proceed. Confirm all four required inputs before dispatching the agent.

## Step 2: Dispatch the adjacent-scan-agent

Before dispatching, inform the user that the scan will take approximately 15-25 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `adjacent-scan-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include all input values inline. The subagent's body (loaded automatically as its system prompt) describes the scan methodology, Critical constraints, and required output structure; your Task `prompt` only needs to supply the input values, e.g.:

> Scan adjacent markets for project AcmePOS. Capabilities: inventory tracking, multi-location support, SKU-level reporting. Absences: no appointment-booking, no payroll, no online ordering. Killed ICP: informal market stall vendor in Peru/Colombia, low willingness-to-pay. Founder domain familiarity: deep retail/POS, no healthcare. Follow your system prompt's Critical constraints and produce the output structure.

If a candidate list was provided, append it: e.g., "Candidate list to evaluate: auto shops, beauty salons, gyms, dentists, vet clinics, food trucks." If no candidate list was provided, omit that line entirely — the agent will generate its own.

The agent will run for 15-25 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve specificity and source citations exactly as the agent produced them.

Highlight the final ranking table and the "non-obvious adjacencies" section — these are the highest-trust outputs from the scan.

## Methodology context

This command implements Phase 2c of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "Phase-to-command map"). The agent evaluates candidate segments the codebase could serve with moderate rework, ranked by the compound score: (market size × WTP × reachability × code-fit) − pivot cost.

A valid output of this scan is "No adjacent market improves on the implicit ICP enough to justify the rework." — per SKILL.md section 3, "The legitimacy of NO." Do not soften an honest NO.

- **If the scan returns a viable adjacent ICP candidate**, proceed to `/research-angle` (Phase 3) to name the differentiated angle for that candidate.
- **If the scan returns a NO** (no adjacent market is meaningfully better than the killed one), proceed to `/research-extraction` to evaluate standalone product opportunities pullable from the codebase — OR accept the negative result per SKILL.md's meta-pattern recognition guidance (write the postmortem, redirect). The choice between extraction and accepting the result depends on whether any individual codebase components have standalone market value independent of the full product.
