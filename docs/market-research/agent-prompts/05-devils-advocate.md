# Agent prompt: Devil's advocate / pressure test

Use this before committing engineering or marketing work to a strategic recommendation produced by any prior research pass. The prompt instructs a second agent to adversarially attack the recommendation, specifically by checking the load-bearing claims and surfacing missed investigations.

This is the single highest-value pass in the methodology. The cost is hours; the cost of skipping is months. There is no project in which skipping the pressure test is the right call.

Dispatch with a general-purpose agent. Allow 25–40 minutes. Output is ~2000 words.

## Template — fill in `{RECOMMENDATION}`, `{LOAD_BEARING_CLAIMS}`, `{FOUNDER_CONSTRAINTS}`, `{MISSED_CATEGORIES}`

```
You are a hostile reviewer pressure-testing a strategic recommendation. Your job is to find evidence the recommendation is wrong. Do not soften findings. If the recommendation holds, say what specifically held up; if it falls apart, identify the killer.

Required reading before starting:
- `/Users/adiaz/market-research-methodology/principles/08-tool-selection.md` — which tool to use for each research task. Pressure-testing benefits acutely from systematic review mining (Firecrawl) and semantic forum search (Exa) when those tools are available; default to them over WebSearch/WebFetch.

The recommendation to attack:
{RECOMMENDATION — exact text of what was proposed, with specific load-bearing claims marked}

Specific claims you must scrutinize and find evidence for or against:
{LOAD_BEARING_CLAIMS — explicit list of claims from the recommendation, each as a separate question}

Founder constraints the recommendation is supposed to respect:
{FOUNDER_CONSTRAINTS — same constraint list used in the prior pass}

Missing investigations the prior scan didn't cover — investigate these now:
{MISSED_CATEGORIES — 3–6 specific candidate categories the previous research likely glossed or missed entirely}

Bigger contrarian questions to address:

A. Why hasn't anyone built this already? If the pain is so obvious and acquisition is so easy, where are the indie SaaS attempts? Either (1) someone tried and failed and we should learn why, (2) existing tools are actually good enough and the pain framing is folksy rather than urgent, or (3) the segment doesn't pay enough to attract attention. Identify which.

B. Is the target structurally healthy? Look for declining trends, regulatory shifts, market consolidation, or category-eroding adjacent products. The recommendation may be entering a shrinking market.

C. Hidden requirements the timeline estimate ignores. Sales tax, compliance, payment processing integration, data migration, multi-locale support, accounting integration — common omissions.

D. The reverse-causality risk: the recommendation assumes the code shape fits the segment. Validate this against the actual schema constraints. Read the code; don't take the prior agent's word for it.

E. The volatility / churn risk: what's the realistic annual churn for this segment? If it's 50–60%, LTV at any plausible ARPU may not cover even cheap CAC.

Output — under 2200 words, structured as:

1. Verdict — does the recommendation survive scrutiny? Yes / No / Conditional. State it in the first paragraph.
2. Killed claims — each claim from above that you found evidence against, with citations.
3. Surviving claims — each claim that held up under scrutiny.
4. Missed competitors found — anyone the prior scan missed, with URLs.
5. Real top pain points — what the target market actually complains about most, by tally from forums and reviews. (This is often different from what the recommendation assumed.)
6. The hidden requirements list — what the timeline estimate didn't account for.
7. Alternative ICPs surfaced during research — if you found 1–2 segments that look better than the original recommendation, list them.
8. Revised recommendation — what the founder should actually do given the new findings.
9. **Scope of this verdict — REQUIRED for any kill or conditional-leaning-no.** Pick exactly one:
   - **(a) Candidate-specific kill.** This recommendation dies for these specific reasons. A modified candidate in the same sub-category might survive.
   - **(b) Sub-category kill.** The killer is structural to this sub-category and would likely apply to similar candidates. Worth checking meta-pattern recognition before another candidate in the same sub-category.
   - **(c) Category-wide kill.** The broader category is foreclosed for this founder profile. Provide separate evidence that the failure mode generalizes.
   - **(d) Insufficient evidence to bracket scope.** Treat as (a) until further research.

   Default to (a) unless evidence supports broader bracketing. Misreporting scope causes downstream errors in meta-pattern recognition.

Cite URLs aggressively. Do not soften. Be willing to recommend against the pivot entirely.
```

## How to read the output

- Three outcomes: most claims survive (refined recommendation), several claims fall (conditional recommendation, re-evaluate), recommendation dies (move to next option or accept negative).
- The "missed competitors" section is the highest-value finding when the recommendation dies. The most common reason a recommendation falls is a hidden incumbent the first scan didn't surface.
- The "real top pain points" section is the highest-value finding when the recommendation survives but the priorities shift. The product roadmap should focus on the actual top complaints, not on the ones the recommendation assumed.
- When the verdict is "conditional, leaning no," that is usually a "no" in practice. Founders tend to interpret "conditional" optimistically. The honest interpretation is that the unconditional negatives outweighed the conditions.

## What to do with the result

- If the recommendation survives, proceed to customer-discovery interviews. The pressure test does not validate the recommendation; it removes obviously wrong versions of it.
- If the recommendation falls, do not run another adjacent-market scan immediately. Read principle 04 (meta-pattern recognition) first.
- If the recommendation falls and this is the second or third one to fall in a row, the framing of the question is wrong. Move to extraction evaluation or accept the negative outcome.

## When this prompt fails

- The agent finds nothing wrong with the recommendation. Possible reasons: (a) the recommendation is actually solid, (b) the agent is being agreeable. Mitigation: re-run with a different agent and check whether the same surviving claims appear. If both agents agree, you have signal.
- The agent kills the recommendation but for reasons that don't seem load-bearing. The agent may be over-attacking — surface-level critiques that don't actually change the strategic outcome. Read the killed claims carefully; not every weakness is fatal.
