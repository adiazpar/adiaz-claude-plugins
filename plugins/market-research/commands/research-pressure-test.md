---
description: Adversarial pass against a named three-sentence angle (Phase 5 of the market-research methodology). Requires a prior named angle from `/research-angle` — blocks dispatch if none supplied.
argument-hint: [named-angle] [named-alternatives] [proxy-data?]
---

You are starting Phase 5 of the market-research methodology — adversarially pressure-test a named three-sentence angle to find the specific killer (or honestly conclude the angle survives scrutiny).

## Step 1: Verify a named angle exists

A qualifying angle is **exactly three sentences** produced verbatim by `/research-angle` (sentence 1: what it is; sentence 2: who it's for; sentence 3: why it wins against the named alternatives). A one-liner summary, a paraphrase, or a list of bullet points does not qualify — those produce false-negative kills. If the user supplies something that isn't three distinct sentences, do not run the pressure-test pass on it.

The pressure-test pass requires a three-sentence named angle from `/research-angle`. If `[NAMED_ANGLE]` is not supplied (positionally as `$1` or in the user's prompt), STOP — do not dispatch the subagent. Instead, tell the user:

> Adversarial passes against unnamed angles produce false-negative kills (per SKILL.md's 'Stance discipline rule'). Please run `/research-angle` first to name the three-sentence angle, then return here with the angle as input. When you return, paste the angle output from `/research-angle` exactly as that command produced it — three distinct sentences. Do not paraphrase or summarize. Verbatim is the test.

Use AskUserQuestion only if the user clearly has an angle in mind but didn't paste it — never ask the user to "make one up on the spot." If the user supplied a paraphrase or vague description rather than a verbatim three-sentence angle, ask them to paste the angle exactly as `/research-angle` produced it; the agent attacks specific sentences, not summaries.

## Step 2: Collect the remaining inputs

The user supplied: `$ARGUMENTS`

The `pressure-test-agent` requires these inputs:

- **Named angle** (`$1`) — the verbatim three-sentence angle from `/research-angle` output. All three sentences are passed VERBATIM into the dispatch prompt; the agent attacks those specific sentences, not a paraphrase (e.g., "(1) A Square plugin that adds session-package functionality to Square Appointments — buy 10 yoga classes, auto-deduct on each visit. (2) Built for service businesses on Square Appointments (fitness studios, salons, tutors, massage therapists) currently using paper class cards and manual tracking. (3) Wins against MindBody at $129+/mo and Acuity at $20+/mo because it stays on Square (no payment-processor re-integration cost) and is priced for businesses under $300K GMV that find MindBody's pricing punitive.")
- **Named alternatives** (`$2`) — the same specific competitors/workarounds from the angle's sentence 3, with prices where known. Required so the agent can attack the comparison claim (e.g., "(1) Square Appointments without packages, with manual tracking; (2) MindBody at $129+/mo; (3) Acuity at $20+/mo; (4) Vagaro at $30+/mo; (5) Paper class cards and Google Sheets")
- **Proxy data** (`$3`, optional) — if `/research-angle` produced a comparable proxy (a prior product in an adjacent vertical with available outcome data) the founder wants pressure-tested against, paste it. If none, the agent will mine for proxies during execution.

If `[NAMED_ALTERNATIVES]` was not supplied, use AskUserQuestion to collect it — the agent cannot attack the comparison claim without knowing what the angle claims to beat. Confirm `[NAMED_ANGLE]` + `[NAMED_ALTERNATIVES]` before dispatching the agent. Proxy data is optional; do not block dispatch waiting for it.

## Step 3: Dispatch the pressure-test-agent

Before dispatching, inform the user that the analysis will take approximately 25-40 minutes and the full report will be presented on completion.

Use the Task tool to dispatch the `pressure-test-agent` subagent. The Task tool does NOT auto-substitute template variables — construct the Task `prompt` to include the input values inline, and include the named angle's exact three sentences verbatim (the agent attacks those specific sentences, not your paraphrase). The subagent's body (loaded automatically as its system prompt) describes the adversarial methodology, Critical constraints, and required output structure; your Task `prompt` only needs to supply the input values, e.g.:

> Run an adversarial pressure-test pass on this named angle. Named angle (verbatim, three sentences): "(1) A Square plugin that adds session-package functionality to Square Appointments — buy 10 yoga classes, auto-deduct on each visit. (2) Built for service businesses on Square Appointments (fitness studios, salons, tutors, massage therapists) currently using paper class cards and manual tracking. (3) Wins against MindBody at $129+/mo and Acuity at $20+/mo because it stays on Square (no payment-processor re-integration cost) and is priced for businesses under $300K GMV that find MindBody's pricing punitive." Named alternatives: (1) Square Appointments without packages with manual tracking; (2) MindBody at $129+/mo; (3) Acuity at $20+/mo; (4) Vagaro at $30+/mo; (5) Paper class cards and Google Sheets. Proxy data: none supplied — mine for comparable proxies during execution. Follow your system prompt's Critical constraints and produce the output structure. You ARE in adversarial mode — find the specific killer or honestly verify the angle survives.

The agent will run for 25-40 minutes.

## Step 4: Surface the agent's report

Present the agent's full report to the user. Do not summarize, condense, or truncate — preserve the verdict, the killed claims, the surviving claims, the missed competitors, and source citations exactly as the agent produced them.

Highlight the "Verdict" (Survives / Dies entirely / Conditional) in the first paragraph and the "Scope of this verdict" bracket (a/b/c/d) — these are the highest-trust outputs from the pass. If the verdict is **Conditional**, surface the specific conditions named so the founder can decide whether they are feasible.

## Methodology context

This command implements Phase 5 of the six-phase market-research methodology (see the `market-research` skill, sections "The six phases" and "Stance discipline rule").

**The single highest-value pass.** Per the methodology's pressure-testing principle (encoded in SKILL.md section 8 "Stance discipline rule"), this is the single highest-value pass in the methodology. The cost is hours; the cost of skipping is months. There is no project in which skipping the pressure test is the right call after a strong angle has been named.

**Stance discipline rule (structural):** Per SKILL.md's 'Stance discipline rule' and `/research-angle`'s output: adversarial passes against unnamed angles produce false-negative kills — the agent will confidently kill a poorly-formed angle for the wrong reason and the methodology will read that as a real verdict. Step 1's structural guard above is non-optional.

**Phase-specific NO criterion (success-state framing):** A valid output of this command is **Dies entirely** — the named angle does not survive scrutiny because of a specific identifiable killer (hidden incumbent, false load-bearing claim, structural mismatch, regulatory/platform guillotine, comparable proxy that failed). Per SKILL.md's "The legitimacy of NO" section: do not soften a kill into "conditional with hand-waving." Finding the killer is the correct and valuable output of this phase — it saves months of misdirected engineering and marketing work. The pressure-test cost is hours; the cost of accepting a bad angle is months.

**Meta-pattern recognition:** If the verdict comes back as **Dies entirely** AND this is the second or third candidate in the same sub-category to die under pressure-testing, the framing of the question is wrong. Pause before evaluating another candidate — consult SKILL.md's section 7 "Failure modes summary" (kill-machine drift specifically) and consider whether the founder's framing of the opportunity needs to change before more candidates are evaluated.

**Routing:**
- **Survives verdict** (most claims hold, one or two refined) → the angle is worth committing to. Proceed to founder commitment and customer-discovery interviews. The pressure test does not validate the angle; it removes obviously wrong versions of it. This is the end of the desk-research methodology for this candidate.
- **Conditional verdict** (several claims fall, recommendation hinges on named conditions) → the founder decides. If the conditions are feasible, proceed with the conditional version; if infeasible, treat as a soft kill. "Conditional, leaning no" is usually a "no" in practice — founders tend to interpret "conditional" optimistically.
- **Dies entirely verdict** (specific killer found) → candidate killed. Do NOT immediately run pass N+1. Either (a) evaluate another candidate in the same sub-category if the agent's scope bracket is (a) candidate-specific, (b) consult SKILL.md's section 7 "Failure modes summary" — kill-machine drift specifically — if the scope bracket is (b) sub-category or (c) category-wide, or (c) run `/research-extraction` to evaluate whether a standalone component from the killed candidate has independent product value.
