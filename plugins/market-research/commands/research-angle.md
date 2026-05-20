---
description: Name the three-sentence defensible angle for a candidate (Phase 3 of the market-research methodology). Required before any pressure-test pass.
argument-hint: [candidate-product] [target-segment] [named-alternatives]
---

You are starting Phase 3 of the market-research methodology — name the differentiated angle for a candidate product (or honestly conclude that no angle exists).

## Step 1: Collect the three required inputs

The user supplied: `$ARGUMENTS`

The `angle-agent` requires three inputs:

- **Candidate product** (`$1`) — what is being angled: the specific product / feature / form factor under evaluation (e.g., "A Square plugin that adds session-package functionality to Square Appointments — buy 10 yoga classes, auto-deduct on each visit")
- **Target segment** (`$2`) — the hypothesized ICP, typically pasted from a prior `/research-icp-audit`, `/research-profitability`, or `/research-adjacent-scan` output (e.g., "Service businesses on Square Appointments — fitness studios, salons, tutors, massage therapists — currently using workarounds")
- **Named alternatives** (`$3`) — the specific competitors (3-5) the angle must beat. These are the incumbents/workarounds the target segment currently uses (e.g., "(1) Square Appointments without packages, with manual tracking; (2) MindBody at $129+/mo; (3) Acuity at $20+/mo; (4) Vagaro at $30+/mo; (5) Paper class cards and Google Sheets")

If any input was not supplied (positionally as `$1` `$2` `$3` or in the user's prompt), use AskUserQuestion to collect what is missing. Confirm all three before dispatching the agent.

## Step 2: Dispatch the angle-agent

Before dispatching, inform the user that the analysis will take approximately 15-25 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `angle-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the three input values inline. The subagent's body (loaded automatically as its system prompt) describes the angle-identification methodology, Critical constraints, and required output structure; your Task `prompt` only needs to supply the three input values, e.g.:

> Run a differentiated-angle identification pass on this candidate. Candidate product: A Square App Marketplace plugin that surfaces per-employee cash variance trends over 30/60/90 days. Target segment: independent convenience store owners with 5-15 part-time employees, currently losing $30-100/week to unattributed drawer shortages. Named alternatives: (1) Square Free's anonymous-drawer-only report, (2) switching to Toast at $69/mo more, (3) manual spreadsheet reconciliation, (4) custom POS reports. Follow your system prompt's Critical constraints and produce the output structure.

The agent will run for 15-25 minutes.

## Step 3: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve the named three-sentence angle and source citations exactly as the agent produced them.

Highlight the "Verdict on the angle" section (Strong angle / Weak angle / No angle) and the "Scope of this verdict" bracket (a/b/c/d) — these are the highest-trust outputs from the pass.

## Methodology context

This command implements Phase 3 of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "The legitimacy of NO").

**The three-sentence rule** is the methodology's most load-bearing commitment. Per SKILL.md's "Phase-to-command map" and the three-sentence rule documented inline, a differentiated angle is exactly three sentences:
1. **What it specifically is** (capability + form factor)
2. **Who it's specifically for** (named segment with a verifiable pain)
3. **Why it specifically wins against the named alternatives** (the defensible delta — naming specific alternatives and specific axis of advantage)

The third sentence is load-bearing. "Better UX," "faster," "AI-powered" are not reasons. A real reason names the specific gap one alternative leaves open that this product fills.

**Phase-specific NO criterion (the kill point for fluffy angles):** A valid output of this command is "No three-sentence angle can be honestly written with specific named alternatives and a specific named gap for this candidate. This is the kill point that prevents fluffy angles from advancing to pressure-testing. Per SKILL.md's "The legitimacy of NO" section: do not soften "no angle exists" into "maybe with more research"." This is the methodology's most subtle failure mode — running adversarial passes without a named angle produces confident kills of poorly-formed proposals, not real verdicts.

**Stance discipline rule (structural):** After this command produces a named angle, it is safe to run `/research-pressure-test`. Per SKILL.md's 'Stance discipline rule': adversarial passes against unnamed angles produce false-negative kills, so this phase is structurally non-optional before `/research-pressure-test`.

**Routing:**
- **Strong angle verdict** (three sentences cleanly written, at least one defensibility advantage holds) → proceed to `/research-pressure-test` (Phase 5). The pressure test now has a specific target.
- **Weak angle verdict** (third sentence couldn't be written specifically, OR no defensibility advantage holds) → reformulate the candidate or abandon. Do NOT proceed to `/research-pressure-test` — adversarial passes against weak angles produce low-information verdicts.
- **No angle verdict** → end of methodology for this candidate (verdict: kill), unless meta-pattern recognition from the agent's "closest viable candidate" or "Scope of this verdict" output suggests revisiting an earlier phase (`/research-adjacent-scan` for a different ICP, or `/research-extraction` for standalone components). After two or more No angle verdicts across different candidates in the same sub-category, treat this as a meta-pattern signal — pause and run meta-pattern recognition (write the postmortem and consider whether the framing itself is wrong) rather than evaluating another candidate in the same framing.
