# Agent prompt: Adjacent market scan

Use this when the implicit-ICP profitability evaluation has returned a negative verdict but the codebase still has potential value. The prompt directs a research agent to survey adjacent segments where the existing capabilities could serve a different ICP with moderate (not full) rework, with the founder's specific constraints baked in.

This prompt is intentionally constraint-heavy. The most common failure mode in adjacent-market scans is recommending segments that are theoretically viable but practically inaccessible to the founder (wrong language, wrong country, wrong sales motion, requires enterprise BD a solo founder can't run).

Dispatch with a general-purpose agent. Allow 30–45 minutes. Output is ~2000–2500 words.

## Template — fill in `{CAPABILITIES}`, `{ABSENCES}`, `{FOUNDER_CONSTRAINTS}`, `{CANDIDATE_LIST}`

```
You are doing an adjacent-market scan for the founder of {PROJECT_NAME}, a {ONE-LINE DESCRIPTION OF CURRENT PRODUCT}. The founder wants to know what other markets the existing capabilities could serve with moderate (not full) rework, ranked by attractiveness.

Required reading before starting:
- `/Users/adiaz/market-research-methodology/principles/08-tool-selection.md` — which tool to use for each research task. Apply tool preferences before defaulting to WebSearch/WebFetch.

The existing capabilities to leverage (don't recommend a market that requires throwing away most of this):
{CAPABILITIES — bullet list of what the codebase actually does, derived from the codebase ICP audit}

The existing capabilities explicitly missing (so the adjacent market shouldn't NEED these as table stakes):
{ABSENCES — bullet list of what the codebase / schema cannot represent without significant rework}

Hard founder constraints:
{FOUNDER_CONSTRAINTS — e.g. "Founder is US-based, English-primary, Spanish-secondary. No ability to do field research. All research and acquisition must be remote-feasible. Solo or small team — no VC for free-tier subsidies. Wants paid SaaS monetization with reasonable ARPU. Available customer-discovery channels: Reddit, Discord, vertical forums, app store reviews, Capterra, niche Facebook groups, Zoom calls with strangers found via outreach."}

Candidate adjacent ICPs to evaluate (and others you surface):
{CANDIDATE_LIST — 6–12 specific candidate segments to consider, e.g. "Food trucks, US makers, coffee shops, smoke/liquor shops, service businesses, ..."}

Methodology:
- US/EU markets: use Census, BLS, IBISWorld, trade-association data, app-store reviews, vertical subreddits
- For each segment, look at what tools the segment currently uses, the price points, and the reviews — that's the WTP signal
- For each segment, look at where the population gathers online — that's the acquisition story
- Reports from payment processors, vertical SaaS quarterly disclosures, and trade publications

What you're producing — a written report under 2500 words:

For EACH candidate ICP (you can drop or add):

1. Fit score with existing capabilities (1–10): does the current shape serve this segment well? What needs to change minimally vs significantly?
2. Market size: merchant count, segment revenue
3. Current tools they use + price points
4. Willingness to pay signal: average tool spend per merchant
5. Acquisition channel: how reachable are they to a {founder profile} founder?
6. Biggest gap the product would need to fill
7. Competitor density: red ocean or under-served?

Then a final ranking table sorting candidates by: (market size × WTP × reachability × code-fit) − pivot cost. Be willing to recommend pivoting hard if data justifies it. Be willing to say "stay with current ICP" if no adjacent market is more attractive.

Surface 2–3 non-obvious adjacent markets you discover that aren't on the candidate list above. The founder is open to insights.

Critical constraints:
- App's existing i18n / language stack / regional features are incidental, not strategic. Don't downgrade markets because the app doesn't support a locale yet — i18n is cheap to add.
- Be honest about pivot cost. If a market requires adding a domain from scratch (appointment booking, METRC integration, multi-state tax), that's a multi-month project — say so.
- The founder-constraint list is hard. If a market requires capabilities the founder doesn't have (Spanish, in-market presence, BD muscle), say so and downgrade accordingly.
- Cite sources with URLs.
```

## How to read the output

- The non-obvious adjacencies the agent surfaces are often the best candidates. They are non-obvious because the obvious adjacencies have already been picked over by competitors.
- The "remote-research data availability" axis (if you included it in scoring) is often the binding constraint. A market that's larger but unresearchable from a desk is worth less than a smaller, transparent one.
- Be skeptical of "market-organizer B2B2C wedge" or similar B2B2C theses unless the agent cites a specific successful precedent. These look attractive in scans and often turn out to be slots already filled.
- Be skeptical of "fastest-growing category" claims — growth at high pace means high competitor density.

## Common adjustments

- For consumer-facing products, replace the merchant-count proxies with end-user behavior data and channel-specific reach data.
- For B2B enterprise products, swap "vertical subreddits" with industry conferences and trade publications.
- If the founder has specific BD capacity (e.g., warm intros to an enterprise customer base), surface markets where that capacity is an advantage instead of dismissing enterprise-sales-motion candidates.
