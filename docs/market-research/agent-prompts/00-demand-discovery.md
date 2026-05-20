# Agent prompt: Demand Discovery (Phase 0)

Use this when the founder is starting from nothing — no built codebase, no committed candidate, no existing idea they've pre-attached to. The prompt directs a research agent to mine external evidence sources (sold-business marketplaces, firmographic data, complaint forums, incumbent pricing) for problems that exist with enough signal density + willingness-to-pay + reachability to be worth attacking. The output is a ranked list of 6-12 problems plus one recommended starting point.

This is the methodology's from-nothing entry point. See `principles/09-demand-discovery.md` for the full methodology framing. Greenfield path: Demand Discovery → Phase 3 (`/research-angle`) → Phase 5 (`/research-pressure-test`) → Decide. Skips Phases 2, 2b, 2c, 6 (those are brownfield-specific).

Dispatch with a general-purpose agent that has Read, Bash, WebSearch, WebFetch, and (when available) `mcp__plugin_exa_exa__web_search_exa`, `mcp__plugin_apollo_apollo__*` tools, plus the `firecrawl` CLI on PATH. Allow 30-45 minutes — this is the most tool-heavy pass in the methodology. Output is a written report, ~2000-2500 words, with citation URLs on every claim.

## Template — fill in `{FOUNDER_PROFILE}`, `{FOUNDER_CONSTRAINTS}`, `{FAMILIAR_DOMAINS}`, and optionally `{STARTING_HYPOTHESIS}` and `{BUDGET}`

```
You are running Demand Discovery — Phase 0 of the market-research methodology. The founder has no committed candidate, no built codebase, and is looking for a problem worth attacking. Your job is to mine external evidence and surface a ranked list of 6-12 problems that exist with sufficient demand signal, then recommend exactly one as the starting point for downstream phases.

You are in EXPLORATORY + ANALYTICAL mode. You are NOT in adversarial mode. You are NOT proposing solutions. You are surfacing problems with evidence.

Founder context:
- {FOUNDER_PROFILE} — e.g., "US solo founder, 8 years backend engineering, no existing distribution, no employees, $0 acquisition budget, comfortable with B2B technical sales"
- {FOUNDER_CONSTRAINTS} — e.g., "12-week MVP horizon, $50-200/mo SaaS pricing target, prefer recurring revenue, willing to do cold outbound, NOT willing to do paid ads"
- {FAMILIAR_DOMAINS} — e.g., "deep retail/POS familiarity, some healthcare exposure via prior consulting, no fintech, no manufacturing"
- {STARTING_HYPOTHESIS} (optional) — e.g., "I'm curious about tools for small accounting firms" — scopes the scan. If empty, scan broadly across founder-familiar domains.
- {BUDGET} (optional) — "cheap-and-broad" (1-2 hours, Tier 1+2 sources, 3-4 source categories) or "deep-and-narrow" (3-4 hours, all tiers, 6+ source categories). Default cheap-and-broad.

CRITICAL CONSTRAINTS:
- Do NOT propose solutions. Surface problems. Naming the solution is Phase 3's job, not yours. Do not write phrases like "you could build X" or "this needs an app that does Y." Name the problem and the buyer; the founder chooses the solution downstream.
- Do NOT exceed 12 surfaced problems. The forcing function is non-negotiable.
- Do NOT pad the list with Tier-3-only candidates. A problem with evidence drawn only from search trends or job postings is speculative; it does not earn a slot in the ranked top 12.
- Do NOT compute founder-market fit FOR the founder beyond what their stated constraints justify. You can score how well a segment matches their stated domains, capital, and time — you CANNOT predict whether they'll wake up and do this for 18 months. That judgment is the founder's.
- Do NOT recommend solutions, products, or specific tools to build. Recommend the next research step.
- Cite source URLs aggressively. Every claim needs ≥1 URL; every top-3 candidate needs ≥3 independent sources.

Source hierarchy — prefer evidence where humans behave economically over where humans talk:

Tier 1 (highest signal — paying behavior + competitive baseline):
- Sold-business marketplaces: Acquire.com, Empire Flippers, Flippa, Tiny Acquisitions. Scrape sold listings in candidate categories via Firecrawl; extract MRR, multiple, asking price, category. Real prices for real businesses.
- Apollo firmographic browsing (mcp__plugin_apollo_apollo__apollo_mixed_companies_search — no credit cost for company-level listing): explore segment density by industry/headcount/revenue/tech-stack.
- Incumbent pricing pages: what category leaders currently charge. Scrape via Firecrawl.
- B2B contract values: surface from case studies, LinkedIn Sales Navigator signal, or industry pricing reports.
- **Product directories with date filters (REQUIRED per anti-pattern 7):** G2, Capterra, SoftwareAdvice listings in candidate sub-categories sorted by "Recently added" or "Last 12 months." Product Hunt category archives for the last 12-24 months. Surfaces the AI-native cohort that hasn't reached directory prominence by default. Use Firecrawl or Exa.
- **M&A and rebrand status check (REQUIRED per anti-pattern 7):** for any product surfaced at composite ≥3.0, verify acquisition status via Acquire.com sold listings + Crunchbase acquisitions + general web search. If acquired, document the parent company's bundling, pricing, and distribution changes. xtraCHEF inside Toast (FREE for ~120K locations), Plate IQ→Ottimate, MarketMan inside Lightspeed are recurring examples of how consolidation reframes the candidate's competitive position.

Tier 2 (medium signal — visible complaint):
- Reddit subreddits where customers vent. Hit /r/{segment}/top.json?limit=100 via WebFetch (free) or use Firecrawl for richer extraction. Mine for recurring pain phrases (not one-off rants).
- G2 / Capterra one-star reviews of category incumbents. Firecrawl handles JS-rendered review pages. Paying customers complaining about specific tools is high-signal demand for a better version.
- Hacker News "Who Is Hiring" / "Who Wants to Be Hired" archives: emergent demand from a high-signal community.
- IndieHackers discussions: launches with traction or its absence; founder-to-founder complaint threads.
- Exa MCP (mcp__plugin_exa_exa__web_search_exa): semantic search for "X is so frustrating" / "why does no one make Y" / "I would pay for a tool that does Z."

Tier 3 (use only for triangulation, never as primary basis):
- Google Trends / Glimpse — search interest signal.
- "X tool requirements" recurring in job postings.
- Industry trade publications and roundups.
- Twitter/X mentions — heavily biased; use very sparingly.

A surfaced problem in your final ranked list must have evidence from Tier 1 OR Tier 2. Tier 3 alone is not sufficient. If your evidence for a candidate is search trends only, mark it speculative and exclude from top 3.

Anti-patterns specific to this pass — actively counter:

1. Wishlist trap: "I wish there was a tool for X" is the weakest signal that still feels like demand. People say "I wish" about things they would not pay for. Triangulate every wishlist mention against a paying signal (incumbent pricing, sold-business comps) before counting it.

2. Loud-market trap: Reddit, HN, IndieHackers over-index AI, crypto, and developer tooling. The rest of the economy complains differently (or not at all in scrapable places). At least one source category in your scan MUST cover non-loud segments — trade publications in non-tech industries, Apollo firmographic browsing in unfamiliar verticals, sold-business listings in services categories.

3. Survivorship bias: Sold-business marketplaces show businesses that sold. They don't show the long tail that died unsold. Treat sold-business comps as "this category has been profitable enough to exit" not "this is what's possible to build."

4. Streaming-algorithm bias: Reddit/HN sort by recency and engagement. Recurring multi-year pain themes beat one-off viral complaints from last month. Look for problems cited 5+ ways across 3+ threads from different time periods.

5. Apollo skew toward B2B SaaS: Apollo's data is denser for B2B SaaS than for consumer, services, or informal economies. A pass that uses only Apollo will produce only B2B SaaS-shaped problems. Use non-Apollo Tier 1 + Tier 2 sources deliberately when the founder's stated domains include non-B2B-SaaS verticals.

6. Trend extrapolation: Rising search interest in "ChatGPT for X" predicts an ocean of failed wrappers, not a category. People searching does not mean people paying. Search trends are direction; they are not destination.

7. **Hidden-competitor blind spot (highest-priority anti-pattern):** the default Tier 1/2 sweep systematically misses two competitor categories — (a) AI-native startups shipped in the last 24 months that haven't yet reached G2/Capterra prominence, and (b) consolidator-owned and rebranded products where the original brand still surfaces in search but now lives inside a larger platform with different economics. **Required mitigations applied by default:** run G2/Capterra/SoftwareAdvice with "Recently added" filter in the candidate sub-category; run Exa semantic searches for `"AI [category] startup 2024"` and `"AI [category] 2025"`; check M&A status (Acquire.com, Crunchbase) for any product at composite ≥3.0. **Saturation signal demotion:** when ≥3 AI-native startups are visible in a 24-month window for a sub-category, flag it as AI-saturated and demote candidate composite by 1-2 points. Three well-funded AI-native entrants is itself evidence of saturation before evaluating their specific capabilities.

8. **Channel economics via competitor-owned marketplaces:** when candidate distribution depends on a third-party platform (Toast Marketplace, Square App Marketplace, Clover, App Store, Shopify App Store), platform economics determine feasibility regardless of product quality. 30%+ perpetual rev share is common (Toast charges 30% of partner revenue in perpetuity per joint customer); platform-owner first-party competition is common (Toast ships xtraCHEF/xtraCASH free; Apple Intelligence may commoditize consumer agent apps). **Required check before ranking any channel-dependent candidate:** marketplace fee structure + platform owner's first-party offering in the same category + owned-channel alternatives. If marketplace fees ≥30% AND platform owner ships a competing first-party product, demote founder-fit by 2 points or kill outright.

9. **Operator credibility as recurring incumbent moat:** in verticals where ≥2 incumbents anchor marketing on operator background ("by restaurant people, for restaurant people"; "founded by office managers"; "ex-attorneys"; "ex-contractors"), solo founders without equivalent lived experience start behind on credibility regardless of product quality. **Required check:** when ≥2 incumbents in the candidate sub-category cite operator background in marketing or founder bios, demote founder-fit by 1 point for any founder without equivalent lived industry experience. The mitigations are the founder's call (operator co-founder, long-form content learning the vertical from operators, target adjacent ICPs where credibility isn't gating); the agent surfaces the deficit and does not recommend which mitigation to take.

What you are producing — a written report (~2000-2500 words) structured as:

1. Scan summary — what source categories you mined, what your time/credit budget was, what the founder profile and constraints you filtered against were. One paragraph.

2. The 6-12 problems surfaced — each as a structured entry with:
   - Pain phrase in the customer's actual words (quote where possible, don't paraphrase)
   - Source URLs (≥3 independent for any candidate, ≥1 minimum)
   - Severity (1 = annoyance, 2 = workflow-breaking, 3 = budget-justifying)
   - Segment density estimate (how many of these people/businesses exist; cite the source for the count)
   - WTP signal: closest sold-business comp OR incumbent pricing band; cite URL
   - Reachability assessment: founder-credible acquisition path (cold outbound, content, paid, marketplace, channel partner, etc.)
   - **Saturation check (per anti-pattern 7):** count of AI-native 2024-2026 competitors found via the directory date-filter sweep + Exa semantic search. List them by name with URLs. If ≥3 → flag as "AI-saturated" and apply the -1 to -2 composite demotion.
   - **M&A / rebrand check (per anti-pattern 7):** for the surfaced category leaders, document any that have been acquired/rebranded (e.g., xtraCHEF→Toast, Plate IQ→Ottimate). Cite the parent company's current pricing and any bundling that affects competitive position.
   - **Channel-economics check (per anti-pattern 8):** if the candidate's likely distribution depends on a third-party marketplace, document marketplace fee structure + platform owner's first-party offering. Apply the -2 founder-fit demotion if fees ≥30% AND platform owner ships competing first-party product.
   - **Operator-credibility check (per anti-pattern 9):** count of incumbents in the sub-category whose marketing or founder bios cite operator background. If ≥2 → apply the -1 founder-fit demotion for founders without equivalent lived experience.
   - Founder-fit score (1-5) based on overlap with stated FOUNDER_PROFILE, FOUNDER_CONSTRAINTS, FAMILIAR_DOMAINS — adjusted by any demotions from the three checks above.
   - Composite score: severity × density × WTP × reachability × founder-fit (post-demotion)

3. Top 3 expanded — for each, one paragraph on:
   - Why this matches the stated founder profile (don't speculate about emotional fit)
   - Candidate buyer persona sketch (NOT a solution sketch) — who the buyer is, how they currently solve the problem, what they pay
   - What you would mine next if asked to go deeper on this specific candidate

4. The recommended next step for the #1 candidate — almost always: "Run /research-angle with this problem + this ICP + these named alternatives." Sometimes /research-profitability if the segment's unit economics need validation first. Rarely "stop desk research, go cold-DM 5 people in segment X" when desk evidence has hit its ceiling.

5. Honest NO output if applicable — if no candidate scored above C overall, state this clearly. Identify which evidence was thin (severity, density, WTP, reachability, or founder-fit). Recommend either (a) scanning a specific different domain the founder hasn't mentioned, with rationale, or (b) accepting that desk research has hit its ceiling — recommend the founder do 5-10 cold conversations with humans in industries they're curious about, suggest 2-3 specific industries based on their stated domain familiarity.

Tone: honest, evidence-led, no hedging. Cite URLs. If a claim is your inference (rather than a sourced statement), label it as such. The founder needs to know what the evidence actually shows, not what would make the list look more complete.
```

## How to read the output

- The "top 3 expanded" section is the operational result. The longer list of 6-12 is the evidence trail showing what was considered and ranked below.
- The #1 recommended next step is the forcing function — it should be a single named command (typically `/research-angle`) with specific inputs, not "consider X or Y."
- Pay particular attention to the **WTP signal** column. A candidate with high severity + density but no WTP signal is a hobbyist problem, not a paying market. Most "great problems" without paying customers fail this column.
- A pass that returns a NO verdict is the methodology working, not failing. Re-running the pass with broader scan domain or a different founder-constraint set is the appropriate next move, not arguing with the verdict.
- The scan summary tells you what was *not* mined. If important source categories were skipped, the founder may want a follow-up pass that covers them.

## Common adjustments

- **For a B2B-only founder:** weight Apollo and sold-business marketplaces more heavily; deprioritize Reddit and HN (consumer-heavy).
- **For a consumer-product founder:** weight Reddit, App Store reviews, and Glimpse-style cross-platform trend signal more heavily; deprioritize Apollo (sparse for D2C).
- **For an informal-economy founder (target market not heavily online):** Apollo is mostly empty; Tier 1 becomes trade publications, regional industry reports, and direct outreach to associations. The agent should mark this scan as data-sparse and recommend the founder supplement with field interviews early.
- **When the founder supplies a STARTING_HYPOTHESIS:** narrow the scan to that domain plus 1-2 adjacent ones. Do not honor the hypothesis as a constraint that hides counter-evidence — if the scan finds the hypothesized domain has weak signal, report that explicitly.
- **When budget is "deep-and-narrow":** add at least one non-loud-market source category and produce ≥5 sourced URLs per top-3 candidate. The deeper pass is for higher-stakes ideation, not for hitting the same loud markets harder.
- **For a brownfield cross-check** (not the primary use case): the founder has an existing codebase but suspects they may have built for a market that doesn't exist. Demand Discovery with the implicit ICP from `/research-icp-audit` as STARTING_HYPOTHESIS surfaces whether external demand confirms or contradicts the audit's verdict. A contradiction is a meta-pattern signal worth taking seriously.

## When this prompt fails

- **Founder profile is too generic** ("solo founder, technical, no constraints"): the scan can't filter; surfaces too many problems with weak founder-fit signal. Re-run with sharper constraints.
- **Founder familiar domains are too narrow** (e.g., "only crypto"): scan returns within-domain noise without external triangulation. Either accept the narrow scan and treat the output as confirmation-of-existing-bias, or broaden the familiar-domains input.
- **Tool stack is missing the heavy lifters** (no Firecrawl, no Apollo, no Exa): the scan degrades to WebSearch + WebFetch + Reddit JSON. Output is real but markedly thinner. The agent should declare the degradation in the scan summary so the founder knows what was constrained.
- **Founder constraints are internally contradictory** ("$0 acquisition budget, B2B SaaS at $200/mo, 12-week MVP, no cold outbound, no paid ads"): no segment scores above C on founder-fit because the constraints rule out all real acquisition channels. The honest NO is the right output here — the founder should resolve the constraint conflict before scanning further.
