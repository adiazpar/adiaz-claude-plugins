---
name: idea-hunt-scoring
description: Score and rank candidate software-product ideas across six validation dimensions (demand, pain, monetization, competition, distribution, why-now). Use when running /idea-hunt and a stage needs to score, filter, or rank candidates.
---

# Idea-Hunt Scoring Rubric

This skill provides the rubric, formula, and hard gates used by `/idea-hunt` at filtering and ranking stages. The command invokes this skill from Stages 2-5.

## Dimensions

Each candidate is scored on six dimensions. Five are 1-5 ordinal; "why-now" is a 0.5-2.0 multiplier.

### Demand (1-5)

How many distinct, recent (≤12mo) public signals point at this problem?

- **5** — 50+ distinct posts/comments across 3+ sources in the last 12 months
- **4** — 20-49 distinct recent signals across 2+ sources
- **3** — 10-19 distinct recent signals, may be single-source
- **2** — 3-9 signals, scattered or older
- **1** — 0-2 signals, or all stale (>2yr)

### Pain intensity (1-5)

How angry are the signals? Quotes from the corpus are required to justify ≥4.

- **5** — Profanity, "wasted N hours/days", documented homemade workarounds, repeated rants
- **4** — Clear frustration, "this is broken", references to abandoning the existing tool
- **3** — Wishlist-style requests with detail
- **2** — Mild "would be nice"
- **1** — No emotional signal — purely hypothetical

### Monetization (1-5)

Is there evidence anyone pays for solutions in this space?

- **5** — At least one direct competitor has public pricing ≥$50/mo OR multiple paid alternatives exist
- **4** — At least one paid product exists in the space, pricing visible
- **3** — Patreon/Gumroad/Discord-tier paid offerings exist
- **2** — Paid offerings exist only in adjacent spaces
- **1** — No paid offerings anywhere; freebie/OSS culture

### Competition strength (1-5)

How fortified are existing solutions?

- **5** — Google/Microsoft/Adobe-tier incumbent, well-funded, actively shipping, deep moat
- **4** — Well-funded VC-backed startup or established mid-size player, actively shipping
- **3** — Multiple small-but-active competitors, no clear dominant player
- **2** — One or two competitors, weak execution, dated UX, or stalled development
- **1** — No real competitor, or only abandoned/dormant tools

### Distribution reachability (1-5)

How clustered is the customer base?

- **5** — One subreddit + one Discord + one conference = ~90% of the customers
- **4** — 2-3 well-known communities cover most of the audience
- **3** — Scattered across several communities but identifiable
- **2** — Spread across many small channels
- **1** — No identifiable cluster; broad/diffuse audience

### Why-now (0.5-2.0 multiplier)

Has something changed *recently* that makes this newly viable?

- **2.0** — Brand-new enabler in the last 12 months (new model capability, new public API, new regulation, new platform launch) that is the unlock for this idea
- **1.5** — Moderate recency advantage (capability matured in last 2 years)
- **1.0** — No specific recency advantage; idea has been viable for a while
- **0.7** — Idea is somewhat stale; the window may be closing
- **0.5** — Recency works against it (e.g., new entrant just won)

## Formula

```
score = demand × pain × monetization × ((6 − competition_strength)² / 5) × distribution × why_now
```

- Multiplicative — any near-zero factor kills the score.
- Squared competition term: competition=1 contributes ×5, competition=4 contributes ×0.8, competition=5 contributes ×0.2 (and is hard-gated below).
- Range: ≈ 0.5 to 6250.

## Hard gates (eliminated regardless of total score)

A candidate is eliminated outright if any of these are true:

1. `monetization < 2` — no money path = no business.
2. `competition_strength == 5` AND no explicit, documented wedge in the candidate's notes — can't win.
3. `why_now < 1.0` AND `depth=deep` AND no explicit justification — fail loud, not quietly.

## Output format

When scoring a single candidate, return the six dimension scores, the total, whether any hard gate fires, and (if gate fires) which one.

Example:

```
candidate: AI-powered timesheet validator
demand: 4
pain: 5
monetization: 4
competition: 2
distribution: 4
why_now: 1.5
total: 4 × 5 × 4 × ((6-2)²/5) × 4 × 1.5 = 4 × 5 × 4 × 3.2 × 4 × 1.5 = 1536
hard_gate: none
```
