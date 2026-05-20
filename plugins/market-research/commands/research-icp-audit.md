---
description: Reverse-engineer the implicit ICP from an existing codebase (Phase 2 of the market-research methodology)
argument-hint: [project-name] [root-path] [language-notes]
---

You are starting Phase 2 of the market-research methodology — reverse-engineer the implicit ICP from an existing codebase by reading schema, route handlers, and component flows.

## Step 1: Collect the three required inputs

The user supplied: `$ARGUMENTS`

The `icp-audit-agent` requires three inputs:

- **Project name** (e.g., "AcmePOS")
- **Root path** to the codebase (absolute path)
- **Language/framework notes** (e.g., "Schema at packages/shared/src/db/schema.ts; backend routes at apps/api/src/app/api/; React frontend at apps/web/src/components/")

If any input was not supplied (positionally as `$1` `$2` `$3` or in the user's prompt), use AskUserQuestion to collect what is missing. Confirm all three before dispatching the agent.

## Step 2: Dispatch the icp-audit-agent

Before dispatching, inform the user that the audit will take approximately 15-25 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `icp-audit-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the three input values inline. The subagent's body (loaded automatically as its system prompt) describes the audit methodology, CRITICAL CONSTRAINTS, and required output structure; your Task `prompt` only needs to supply the three input values, e.g.:

> Audit the `AcmePOS` codebase at `/Users/founder/acmepos`. Framework/file layout: Schema at packages/shared/src/db/schema.ts; backend routes at apps/api/src/app/api/; React frontend at apps/web/src/components/. Follow your system prompt's CRITICAL CONSTRAINTS and produce the 6-item written report (~1500 words).

The agent will run for 15-25 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve specificity and file citations exactly as the agent produced them.

Highlight the "Sharpest signals" and "What's notably missing" sections — these are the highest-trust outputs from the audit.

## Methodology context

This command implements Phase 2 of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "Phase-to-command map"). The agent will NOT read prior strategic-framing docs (.claude/, docs/, README.md) — its job is a fresh read from source code only.

A valid output of this audit is "no ICP exists that the founder can reach" — see the SKILL.md's "The legitimacy of NO" section. Do not soften an honest NO into "maybe with more research." If the implicit ICP is reachable and looks monetizable, proceed to `/research-profitability`. If the audit returns a NO (the ICP cannot be reached or doesn't support SaaS economics for the founder profile), proceed to `/research-adjacent-scan` to evaluate alternative ICPs the codebase could serve.
