# Agent prompt: Standalone product extraction evaluation

Use this when all attempts to find a viable ICP for the whole product have failed (typically after 2–3 negative passes through the codebase ICP audit → profitability → adjacent market scan → pressure test sequence). The prompt flips the framing: instead of asking "what segment does this app serve," it asks "what's the smallest, most defensible product I could pull out of this codebase and sell to someone in 8 weeks?"

This is the "reframe" move described in principle 04. It is appropriate when the meta-pattern is clear: the whole product doesn't have a viable market for this founder's profile, but the codebase contains specific capabilities that might.

Dispatch with a general-purpose agent. Allow 30–45 minutes. Output is ~2000–2200 words.

## Template — fill in `{PROJECT_NAME}`, `{ROOT_PATH}`, `{CAPABILITIES}`, `{FOUNDER_CONSTRAINTS}`

```
You are doing a strategic reframing exercise for the founder of {PROJECT_NAME}, a {ONE-LINE DESCRIPTION OF THE FULL APP}. After {N} failed ICP pivot attempts, the conclusion is that the general-purpose {CATEGORY} category is structurally bad for this founder's profile. Stop asking "what segment does this app serve" and ask:

"What's the smallest, most defensible product I could pull out of this codebase and sell to someone in 8 weeks?"

The framing flip: don't sell the app shell. Sell a specific capability the codebase contains that has standalone value to OTHER builders, platforms, or businesses.

Required reading before starting:
- `/Users/adiaz/market-research-methodology/principles/08-tool-selection.md` — which tool to use for each research task. The competitive-landscape and buy-vs-build work in this pass benefits from systematic review mining (Firecrawl) and semantic search across developer / indie forums (Exa) when those tools are available; default to them over WebSearch/WebFetch.

Codebase root: {ROOT_PATH}

Candidate extractions to evaluate (and add to this list):
{CAPABILITIES — bullet list of specific capabilities with file path pointers. For each candidate, name the buyer hypothesis ("could this be sold to X?"). Be specific about which files implement the capability.}

Founder constraints:
{FOUNDER_CONSTRAINTS}

Evaluate each candidate on:
- Time-to-MVP for a paying customer: Must be ≤8 weeks. If it's longer, kill it.
- Defensibility / moat: What stops a competitor or the buyer from doing it themselves?
- TAM for this specific capability sold standalone. Be honest — most extractions have small TAMs.
- Sales motion match for a {founder profile} founder: Self-serve API? Plugin marketplace? Enterprise BD? Open-source community?
- Pricing model viability: Monthly subscription, usage-based, one-time license, transaction fee, services.
- Killer risk — what's the one thing that would kill this product?

Output — under 2200 words. Structure:

1. Summary table: each candidate scored on the dimensions above.
2. The top 2 candidates expanded: who buys it, how they find it, what they pay, what competes, what kills it.
3. Honest list of candidates to kill immediately and why.
4. The honest answer to the framing question — is there anything in this codebase that's a standalone product, or is the codebase only valuable as the whole app?
5. If the answer to (4) is "nothing standalone is viable," say so. The founder has shown they can take a hard answer.

Constraints:
- Read actual code at the cited paths to verify capability descriptions; don't take prior agents' word for it.
- Be willing to recommend "kill all extractions, the app is the product or nothing is" if that's where the data lands.
- Cite sources for any competitive claims (URLs).
- Apply the buy-vs-build honest test to any API or service idea: at realistic scale, would the buyer pay you or build it themselves with the same primitive?
```

## How to read the output

- The honest verdict is usually "most extractions die; one or two survive at a side-income ceiling." That is a real finding. Side-income products are real businesses; they're just not startups.
- Be especially skeptical of "wrap a commodity API as a SaaS" extractions (image processing, AI feature wrappers, geocoding). These almost always fail the buy-vs-build test.
- "Open-source it and use as portfolio + consulting lead-gen" is a legitimate extraction option when nothing else passes. The agent may not surface it explicitly — name it in the output review.
- The agent's "killer risk" for each candidate is often more useful than the score. A 5/5 candidate with a high-probability killer is worse than a 3/5 candidate without one.

## What to do with the result

If the top extraction is graded B+ or higher: pursue it as a side project with a clear walk-criterion (e.g., "50 paying installs by week 16 or I walk").

If the top extraction is B or B−: pursue only if the founder wants closure with a small upside path. Don't pursue as a primary business move.

If nothing scores above C: the codebase is the whole app or nothing standalone. The honest options are open-source release (for portfolio / consulting lead-gen) or sunset.

## What this prompt is not

It is not a last-ditch attempt to keep working on the codebase. If the extraction evaluation returns no viable candidates, the right next step is closure, not yet-another-pivot. The methodology has done its job.
