# Agent prompt: ICP profitability evaluation

Use this after a codebase ICP audit has produced a specific persona, or whenever you need to evaluate whether a target market can be monetized at SaaS economics. The prompt forces the agent to use proxy data (payment rails, distributor case studies, government stats, competitor reviews) instead of keyword search, which is the only honest way to research demographics with low digital presence.

Dispatch with a general-purpose agent that has WebSearch and WebFetch. Allow 25–40 minutes of agent time. Output is ~1500–2000 words.

## Template — fill in `{ICP_DESCRIPTION}`, `{GEOGRAPHIES}`, `{FOUNDER_PROFILE}`

```
You are evaluating whether the "{ICP_DESCRIPTION}" market is profitable to serve as a paid SaaS customer. The founder wants an evidence-based, willing-to-say-no read on whether to commit to this market.

Required reading before starting:
- `/Users/adiaz/market-research-methodology/principles/08-tool-selection.md` — which tool to use for each research task. Check your tool list; if Firecrawl, Exa, or Apify Reddit are available, use them in preference to WebSearch/WebFetch for review mining and forum analysis.

The ICP under evaluation (do not narrow further — keep all of this in scope):
{Specific persona description with business type, scale, formality, tech-savviness, geography candidates}

Founder constraints (the realistic operating envelope):
{FOUNDER_PROFILE — location, language fluency, funding state, team size, time budget, distribution capacity, BD muscle}

Methodology — use these proxies, not just keyword search:
1. Payment-rail merchant adoption data: {region-specific — Yape, Plin, Nequi, Pix, Mercado Pago, M-Pesa, Square merchant reports}
2. Distributor / wholesaler case studies — they count and profile these merchants ({region-specific examples})
3. Government statistics: ({region-specific national statistical offices, business census data})
4. Development-bank reports: IDB, CAF, World Bank, CGAP, GSMA on financial inclusion + informal economy
5. Competitor app store reviews + Capterra / Trustpilot — find what the existing tools fail to do
6. B2B-marketplace failure post-mortems — failed startups often publish merchant counts and ARPU
7. Trade-association data and industry reports

What you're producing — a written report under 2000 words covering:

1. Market size by geography — how many of these merchants exist in each candidate country / region? Cite the source. Order of magnitude matters more than precision.

2. Digitalization rate — what % of these merchants already use SOME digital tool (mobile money, POS app, bookkeeping app, WhatsApp Business)? This is the addressable share, and it's often surprisingly high in markets that look "un-digitized."

3. Current willingness to pay — what are these merchants paying today, and for what?
   - What does the dominant free/freemium app monetize on? (Subscription, transaction fee, hardware, financial services attach?)
   - What's the typical paid ARPU when these merchants DO pay?
   - When they pay, is it for compliance, hardware, transaction processing, or premium features?

4. Competitive density — who is winning each market and why? Be honest about whether there's room for another tool. Specifically address the dominant local player(s) and the global generalist (Square, Loyverse, etc.) where relevant.

5. Acquisition cost reality — how have winners reached this segment? CAC numbers if disclosed. Channels (Facebook ads in local language, WhatsApp groups, in-market boots-on-ground reps, distributor partnerships, app-store ASO). Critically: are these channels accessible to a {founder profile} founder?

6. The honest profitability verdict — given (1)–(5):
   - If a founder enters this market with a free/cheap tool, can they monetize?
   - What's the typical ARPU for those who DO pay?
   - Is the segment growing or shrinking?
   - Which sub-segment within this ICP has the best paid-conversion economics?

7. Three things the founder doesn't already know — non-obvious findings: failed pivots, surprising willingness-to-pay categories, regulatory shifts, under-served niches within the ICP.

Critical constraints:
- Be willing to say "this ICP is not profitable to serve at SaaS economics" if the data supports that. Don't sell the ICP.
- Cite sources with URLs. Where data is missing, say so explicitly.
- If a {founder profile} founder cannot credibly access this market, say so clearly.
```

## How to read the output

- The "typical paid ARPU" number is the single most useful figure. Multiply it by realistic acquisition rates, not idealized ones.
- "Dominant free incumbent has X million users at $Y/year ARPU" is usually the verdict in disguise. If the dominant player can't make subscription work, a smaller competitor with worse distribution won't either.
- "Compliance hook" is the most common path to paid SaaS in formal-merchant markets. If the agent surfaces one, check whether it's a hook the founder can actually build (regulatory expertise, certification, local partnerships).
- The "acquisition cost reality" section is the part most likely to be glossed in summaries. Read it carefully — most failed pivots fail here, not on TAM.

## Common adjustments

- For B2C markets (consumers, not merchants), swap the payment-rail proxy for consumer behavior data (e.g., Pew, Statista consumer reports, ad-spend benchmarks).
- For B2B enterprise markets, use industry analyst reports (Gartner, Forrester) and SEC filings of public competitors. Proxy data matters less because enterprise customers post.
- For regulated markets (healthcare, finance, cannabis), add a compliance section. The compliance moat is often the whole answer.
