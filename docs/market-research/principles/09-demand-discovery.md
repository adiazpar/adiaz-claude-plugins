# Principle: Demand Discovery — finding real problems before naming a solution

The methodology was originally written assuming the founder shows up with a candidate — a product they've built, a category they're considering, an idea they want validated. The six phases evaluate that candidate. But the highest-leverage research move often isn't "validate this idea I have." It's "find me a problem that exists with enough signal density and willingness-to-pay to be worth building anything for, *before* I commit to a candidate."

This is Demand Discovery — the from-nothing entry point to the methodology. Phase 0, if you want a number. It precedes everything else in the greenfield case (no codebase, no candidate) and serves as an optional cross-check in the brownfield case ("did I build the wrong thing for a market that doesn't exist?").

## The core idea

**Founder introspection is internal. Demand discovery is external.** A founder asking "what should I build?" by sitting alone and listing interests produces a candidate list with one structural flaw: there's no evidence anyone wants any of it. A founder asking "what problems do I see *real people paying real money to try to solve*?" produces a candidate list grounded in observable demand.

This principle is the methodology's commitment to demand-first ideation. The agent's job is to gather external evidence — from sources where buyers behave, complainers complain, and incumbents charge — then surface a ranked list of problems with citations, and recommend exactly one as the starting point for downstream phases.

It's not the methodology's *only* entry point. Brownfield work (existing codebase) still starts with Phase 2 (Audit). But for the from-nothing case, Demand Discovery replaces the older "Phase 1 Map" framing, which was vague about *what kind of map* and easy to slip into founder-introspection theater.

## What Demand Discovery does

The output is a ranked list of 6-12 problems that:

1. Exist in observable form (cited evidence, not speculation)
2. Have reachable populations (segment density + acquisition path identified)
3. Have willingness-to-pay signal (sold-business comps, incumbent pricing, or paying-customer review density)
4. Are filter-able by founder profile (founder constraints + domain familiarity supplied as input)

For each problem, the agent produces:
- The pain phrase in the customer's actual words (not paraphrased)
- ≥3 independent source URLs (no single-source claims)
- A severity assessment (1 = annoyance, 2 = workflow-breaking, 3 = budget-justifying)
- A density estimate (how many of these people / businesses exist in the addressable market)
- A WTP signal (closest sold-business comp, or incumbent pricing band)
- A reachability assessment (founder-credible acquisition path)
- A founder-fit score (how well the founder's stated constraints and familiarity overlap with the segment)

The ranked top 3 get expanded analysis. The #1 candidate gets a recommended next step — almost always `/research-angle` to name the specific three-sentence angle the founder would attack the problem with.

## What Demand Discovery does NOT do

These are the failure modes the principle exists to prevent. Each one is plausible-sounding and degrades the methodology.

### Anti-pattern 1: Proposing solutions

Demand Discovery surfaces *problems*. It does not name solutions. That's Phase 3's job (`/research-angle`).

If the agent's output includes phrases like "you could build X" or "this needs an app that does Y," it has drifted from problem-discovery into solution-formulation. The discipline is: name the problem and the buyer; do NOT name the product. The same problem has many possible solutions and the founder picks one in Phase 3 — not before.

### Anti-pattern 2: Endless option-generation

The agent must output a hard-capped 6-12 problems, ranked, with a single recommended #1. Not "here are 30 interesting opportunities." Not "this market has many gaps." Not "more research needed."

If every Demand Discovery pass ends with "interesting candidates worth further investigation" and no committed #1, the agent has turned into a candidate factory. That's the exploratory-stance failure mode (see [[01-research-stance]] → "The exploratory failure mode") in its purest form. The forcing function is non-negotiable: 6-12 problems, top 3 expanded, exactly one recommended next.

### Anti-pattern 3: Search-trend extrapolation

Glimpse, Google Trends, and trending-topic feeds are *demand-curiosity signals*, not demand signals. People searching for something does not mean they will pay for a solution. Rising search interest in "ChatGPT for X" predicts an ocean of failed wrappers, not a category. See [[02-proxy-data-research]] on triangulation.

Use trend tools as one input among many. Never use them as the *primary* basis for ranking a problem. People paying beats people searching by an order of magnitude in signal quality.

### Anti-pattern 4: The wishlist trap

"I wish there was a tool for X" on Reddit / Twitter / IndieHackers is the weakest demand signal that still feels like demand. People say "I wish" about things they would never buy. They say "I wish" about things they would buy for $5/mo but not $50/mo. They say "I wish" because complaining is cheap.

A surfaced problem must be triangulated against a *paying* signal — incumbent pricing in the category, sold-business comps for similar products, B2B contract sizes via Apollo firmographics, etc. — before it earns a slot in the ranked list. Wishlists without paying-signal triangulation are noise.

### Anti-pattern 5: The loud-market trap

Developers, AI enthusiasts, and indie hackers complain very visibly. The rest of the economy — informal merchants, services businesses, regulated industries, small-business owners outside the venture-capital orbit — complains differently or not at all in places that can be scraped.

Demand Discovery passes that mine only the loud markets will surface only loud-market problems. The principle requires the agent to allocate at least one source-category to *non-loud* segments (industry trade publications, sold-business marketplaces in the candidate's vertical, Apollo firmographic browsing in unfamiliar industries). The bias is real and pre-existing in the methodology — see [[02-proxy-data-research]] → "The loudness bias."

### Anti-pattern 6: Computing founder-market fit *for* the founder

The agent can score founder-fit based on the inputs the founder supplied (constraints, domain familiarity, time available, acquisition-channel preferences). The agent *cannot* know how much pain the founder will tolerate for 18 months on a sub-$5k MRR product, or whether the founder genuinely wants to wake up and sell to dental offices every day for two years.

Founder-market fit is a judgment about the founder's emotional and motivational endurance. The agent's job is "here's the demand data filtered by your stated constraints"; the founder's job is "am I really going to do this for two years?" The principle is that the agent surfaces; the founder commits. The agent must not pretend to compute the commitment.

### Anti-pattern 7: Failure to surface AI-native 2024-2026 entrants and consolidator-owned products

The single most expensive blind spot in the methodology, surfaced after two convergent Phase 3 NO verdicts (dental insurance verification, restaurant invoice reconciliation) where the DD pass missed 8 and 15 hidden competitors respectively. The pattern: DD finds the obvious incumbents (well-funded category leaders, original brands) but systematically misses two categories:

- **AI-native startups shipped in the last 24 months** that haven't yet reached G2/Capterra prominence but are actively selling to the same ICP at price points the candidate would need to undercut. In the restaurant pass, this cohort included Lido, Cactus AI, Dishboard, InvSpot, Scanny AI, Tako Solutions, TableWise, ReceiptsAI — all 2024-2026 launches, all using the same foundation models the candidate would use.
- **Consolidator-owned and rebranded products** where the original brand still surfaces in search but the product now lives inside a larger platform with different economics. In the restaurant pass: xtraCHEF was acquired by Toast and now ships xtraCASH FREE to ~120K locations; Plate IQ rebranded to Ottimate; MarketMan operates inside Lightspeed at $199. None surfaced by DD's default Tier 1/2 sweep.

**Required Tier 1 sweep additions (applied by default before any candidate enters the ranked list):**

- **Product directory date filter.** Run a G2 / Capterra / SoftwareAdvice search in the candidate sub-category sorted by "Recently added" or "Last 12 months." Surfaces the AI-native cohort that hasn't reached directory prominence by default.
- **Exa semantic search for AI-native entrants.** Query patterns: `"AI [category] startup 2024"`, `"AI [category] 2025"`, `"[problem] automation startup [year]"`. Surfaces entrants below directory prominence.
- **M&A / rebrand check.** For every product surfaced at composite ≥3.0, verify acquisition status via Acquire.com sold listings + Crunchbase acquisitions + general web search. If acquired, document the parent company's bundling, pricing, and distribution changes — these often reframe the candidate's competitive position.

**Saturation signal demotion.** When a candidate sub-category has ≥3 AI-native startups visible within a 24-month window, flag the sub-category as **AI-saturated** and demote candidate composite by 1-2 points. Three well-funded AI-native entrants is itself evidence of saturation — even before evaluating their specific capabilities. The presence pattern matters as much as the per-competitor analysis.

Over-thorough competitor sweeps cost nothing when wrong; missed competitors cost months when wrong. Apply by default.

### Anti-pattern 8: Channel economics via competitor-owned marketplaces

When candidate distribution depends on a third-party platform marketplace (Toast Marketplace, Square App Marketplace, Clover, App Store, Shopify App Store), platform economics determine feasibility regardless of product quality. The pattern:

- **30%+ perpetual rev share is common.** Toast charges 30% of partner revenue in perpetuity per joint customer ([reformingretail.com partner contract reporting](https://reformingretail.com/index.php/2019/07/16/as-we-predicted-toast-has-greatly-increased-their-pos-integration-fees)).
- **Platform-owner first-party competition is common.** Toast ships xtraCHEF/xtraCASH; Apple Intelligence may commoditize consumer agent apps; Shopify ships first-party support tooling. The platform owner's offering is usually free or bundled, while the partner pays 30% rev share plus their own COGS.

**Required check before ranking any channel-dependent candidate:**

1. **Marketplace fee structure** — rev share %, listing fees, certification requirements, onboarding lock-ins
2. **Platform owner's first-party offering** in the candidate's category — does the platform already ship the same or adjacent capability for free / bundled?
3. **Owned-channel alternatives** — can the candidate be sold via cold outbound, content, or direct sales without depending on the marketplace?

If marketplace fees are ≥30% AND the platform owner ships a competing first-party product, demote founder-fit by 2 points or kill outright. The product's quality is downstream of channel economics; if the channel doesn't work, the product can't either.

### Anti-pattern 9: Operator credibility as recurring incumbent moat

In verticals where ≥2 incumbents anchor marketing on operator background (restaurant: MarginEdge "by restaurant people, for restaurant people"; dental: DCS founded by office managers; legal-tech founded by ex-attorneys; HVAC SaaS founded by ex-contractors), solo founders without equivalent lived experience start behind on credibility regardless of product quality.

The credibility deficit shows up as: customer reviews anchoring trust to industry background, buyer purchase decisions gated by credibility signaling before feature comparison, and multiple operator-founded incumbents reinforcing the trust pattern across the category.

**Required check:** when ≥2 incumbents in the candidate sub-category cite operator background in their marketing or founder bios, demote founder-fit by 1 point for any founder without equivalent lived industry experience.

The mitigations are the founder's call, not the agent's:

- (a) Find an operator co-founder for credibility
- (b) Build operator-credibility via long-form content — founder learns the vertical from operators in public over 6-12 months before launch
- (c) Target adjacent ICPs in the same vertical where operator-credibility isn't the dominant trust signal (e.g., the vendor-side of a B2B marketplace where buyers don't gate on operator background)

The agent surfaces the deficit; the founder commits to a mitigation or chooses a different vertical. The agent does not recommend which mitigation to take.

## Source hierarchy

Not all evidence sources are equal. The agent prefers sources where humans *behave economically* over sources where humans *talk*.

**Tier 1 — Buying behavior + competitive baseline (highest signal)**
- Sold-business marketplaces (Acquire.com, Empire Flippers, Flippa, Tiny Acquisitions) — real prices for real businesses solving real problems
- Apollo firmographic browsing — real companies in real segments, with real headcount, revenue, and tech-stack data
- Incumbent pricing pages of category leaders — what people *currently pay* to solve adjacent problems
- B2B contract values where surfaceable (LinkedIn Sales Navigator signal, occasional disclosure in case studies)
- **Product directories with date filters** — G2 / Capterra / SoftwareAdvice listings in candidate sub-categories sorted by "Recently added" or "Last 12 months"; Product Hunt category archives for the last 12-24 months. Surfaces the AI-native cohort that hasn't reached default directory prominence. (Required per anti-pattern 7.)
- **M&A and rebrand status check** — Acquire.com sold listings, Crunchbase acquisitions, PitchBook M&A data for any product surfaced in the candidate sub-category. Verifies whether the surfaced brand still operates independently or has been consolidated and re-priced inside a larger platform. (Required per anti-pattern 7.)

**Tier 2 — Visible complaint (medium signal)**
- Reddit subreddit complaint threads (`/r/<segment>` where customers vent)
- G2 / Capterra one-star reviews of category incumbents — paying customers complaining about specific tools
- Hacker News "Who Is Hiring" / "Who Wants to Be Hired" archives — emergent demand from a high-signal community
- IndieHackers discussions / launches with traction or its absence

**Tier 3 — Demand-curiosity indicators (lowest signal, use only for triangulation)**
- Google Trends / Glimpse search volume
- "X tool requirements" recurring in job postings
- Industry trade publications and roundups
- Twitter/X mentions (heavily biased; use sparingly)

A surfaced problem with evidence drawn entirely from Tier 3 should be marked speculative and *excluded from the top 3*. A surfaced problem with Tier 1 + Tier 2 evidence is a real candidate.

## The output discipline

The Demand Discovery output structure is non-negotiable:

1. **The 6-12 problems surfaced** — each with the structure above (pain phrase, severity, density, WTP, reachability, founder-fit, ≥3 source URLs)
2. **Top 3 expanded** — each with a one-paragraph "why this matters to your stated founder profile" rationale and a candidate buyer-persona sketch (NOT a solution sketch)
3. **The recommended next step for the #1 candidate** — usually `/research-angle` with the problem and ICP pre-filled; sometimes `/research-profitability` if the founder needs unit-economics validation before angle-formulation; rarely "go talk to 5 people in segment X before any more desk research" when desk evidence has hit its ceiling
4. **Honest NO output** if applicable — see below

## The legitimacy of NO for Demand Discovery

A valid Demand Discovery output is **"No problem surfaced with enough signal density + reachability + founder-fit."** This is not a failure of the pass. It is the pass succeeding at its job: telling the founder that desk research has not found a credible target in this scan.

What the agent should NOT do when no problem qualifies:
- Soften the verdict ("interesting opportunities to explore further")
- Pad the list with Tier-3-only candidates to hit the 6-12 cap
- Recommend the founder "try a different category" as the next step without a specific direction

What the agent SHOULD do when no problem qualifies:
- Say so clearly. Explain which evidence was thin: severity? density? WTP? reachability? founder-fit?
- Recommend either (a) scanning a specific different domain the founder hasn't mentioned, with a one-paragraph rationale for the direction, or (b) accepting that desk research has hit its ceiling and recommending the founder do 5-10 cold conversations with humans in industries they're curious about.

The methodology's "Legitimacy of NO" principle (see [[01-research-stance]] → "The legitimacy of NO") applies here in the same way it applies to angle-formulation and pressure-testing. NO is correct when the data supports it. Pretending otherwise produces false-positive starting points and wasted founder months.

## How to apply

Practical heuristics for running the pass:

1. **Time-box the first pass.** Two to three hours of agent time, not eight. If the first pass is thin, the answer is often that the founder's stated constraints are too narrow or the scan domain was too generic — not that the agent needs more time.
2. **Weight Tier 1 sources 2x Tier 2.** People paying outweighs people complaining when the two disagree.
3. **Apollo browsing in one unfamiliar industry per pass.** Forces the agent out of the loud-market trap. The founder may not have asked for it; the agent does it anyway and reports what it found.
4. **Recurring pain phrases beat one-off complaints.** A problem cited five different ways across three Reddit threads from different years is a real recurring problem. A single viral complaint from last month is a moment, not a market.
5. **Sold-business comps trump everything else for WTP.** If similar businesses to the surfaced problem sold for $80k-$500k on Acquire.com, that's the WTP-comp ceiling for a solo founder building in that space. If nothing similar has ever sold, that's a real signal too — usually that the segment has no liquidity, which is itself a kill criterion.
6. **The forcing function is the output discipline, not the agent's enthusiasm.** Even if the agent surfaces 50 interesting things, the output is 6-12 with top 3 expanded and one recommended next. The agent does not get to escape the forcing function by calling its list "preliminary."

## Failure modes summary

Beyond the six anti-patterns above, three additional risks apply to Demand Discovery specifically:

1. **Streaming-algorithm bias.** Reddit, HN, and IndieHackers are sampled by recency and engagement. They over-represent topics that are *currently controversial* (often AI, crypto, dev tooling) and under-represent topics that are *durably profitable but quiet* (legacy industry SaaS, regional services, regulated B2B). Counter by intentionally allocating at least one source-category per pass to non-streamable sources (Apollo, sold-business marketplaces, trade pubs).

2. **Survivorship bias in sold-business data.** Acquire.com and Empire Flippers show businesses that *did* sell. They don't show the long tail of solo-founder products that died unsold. Use sold-business data as a "this category has been profitable enough to exit" signal, not as a "this is what's possible to build" signal.

3. **Apollo skew toward B2B SaaS.** Apollo's data is denser for B2B SaaS than for consumer products, regional services, or informal economies. A pass that uses only Apollo will be biased toward B2B SaaS-shaped problems. Counter by deliberately using non-Apollo Tier 1 + Tier 2 sources for any pass that wants to find non-B2B-SaaS problems.

## How Demand Discovery integrates with the other phases

The from-nothing path:

```
Demand Discovery  →  Phase 3 (Angle)  →  Phase 5 (Pressure-test)  →  Decide
   (find problems)     (name the solution)    (find the killer)        (commit / kill)
```

Phases 2, 2b, 2c, and 6 are *skipped* in the greenfield from-nothing case because they're brownfield-specific (existing-code constraints).

The brownfield path (unchanged):

```
Phase 2 (Audit) → Phase 2b (Profitability) → ... → Phase 3 → Phase 5 → Decide
```

Demand Discovery is *optional* in the brownfield case as a cross-check: "Is the audited ICP actually a real demand source, or did I build something for a market that doesn't exist?" When the audit reveals an ICP that doesn't surface in Demand Discovery as a real-demand category, that's a meta-pattern signal worth taking seriously. See [[07-anti-patterns]] → "Building for a market that doesn't exist."

## See also

- [[01-research-stance]] — Demand Discovery is exploratory + analytical, not adversarial. The "Legitimacy of NO" applies here.
- [[02-proxy-data-research]] — Demand Discovery relies heavily on proxy data; the triangulation discipline is essential.
- [[04-differentiated-angle]] — Demand Discovery's output feeds Phase 3 (Angle), not Phase 5 (Pressure-test). Angle-naming is the gate before adversarial testing.
- [[07-anti-patterns]] — Several anti-patterns apply: kill-machine drift (running adversarial passes on Demand Discovery output before an angle is named), fluffy-angle drift (skipping angle-naming and going straight to commit), single-proxy extrapolation (treating Reddit-only signal as conclusive).
- [[08-tool-selection]] — Demand Discovery is the most tool-heavy pass in the methodology. The tool-selection matrix governs which tool runs against which source category.
