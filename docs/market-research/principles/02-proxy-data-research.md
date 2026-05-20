# Principle: Researching populations that don't post

Some target populations are invisible to keyword search. Informal-economy micro-merchants, blue-collar trades, off-grid agricultural operators, low-tech-literacy seniors, immigrant business communities — they don't post on Reddit, don't write Medium articles, don't appear in Capterra reviews. Some populations are *over*-represented in keyword search and you over-weight what you find — developers, designers, indie hackers, early adopters. Both ends fail the same way: keyword search alone produces a distorted picture of the market.

The right framing: **they don't post (or they over-post), but they're measured by adjacent systems.** Research them through proxies.

## Proxy sources by population type

The proxies that work depend on what kind of population you're researching. For a complete list calibrated by industry, see `industry-guides/`.

### When the population is digitally quiet (informal merchants, blue-collar trades, seniors, regulated industries)
- **Transaction-rail data.** Payment processors, mobile money, fintech merchant-adoption reports
- **Adjacent-system data.** Suppliers, landlords, distributors, wholesalers who interact with the population and DO post
- **Government and development-bank statistics.** Census, BLS, IBGE, DANE, IDB, World Bank, CGAP, GSMA
- **Trade associations and industry conferences.** NACS, NRA, ABA, vertical pubs
- **Failed-startup post-mortems.** Failed marketplaces and SaaS that tried to serve the population publish what they learned

### When the population is digitally loud (developers, designers, AI enthusiasts, creators, gamers, indie founders)
- **Direct community channels.** Twitter/X, GitHub stars/issues, Hacker News threads, dev.to, Reddit (technical subs), Discord servers
- **Public revenue disclosures.** Indie Hackers income reports, OpenStartup.io, founder Twitter "MRR update" posts
- **Product Hunt launches.** Comments, upvotes, follow-ons, who pivoted where
- **Survey data.** Stack Overflow Developer Survey, State of JS, State of CSS, JetBrains surveys
- **Marketplace data.** App Store / Play Store reviews, Shopify App Store, Chrome Web Store, npm download stats
- **Job postings.** Hiring patterns from competing companies signal which features they're building

### When the population is mixed (most SMBs, consumers, prosumers)
- **Mixed-channel triangulation.** Use both above where the proxies converge or contradict — both are informative
- **Capterra / G2 / Trustpilot reviews.** Especially one-star reviews — the unmet need is in the complaints
- **YouTube tutorials.** The most-watched DIY tutorials in a category reveal pain points the existing tools don't solve
- **Subreddit volume.** Active subscriber count + post frequency + comment depth on niche subreddits

## How to combine proxies

Pick three proxies. Look for convergence. If three independent sources describe the same population at roughly the same scale with roughly the same pain points, you have a triangulated picture worth acting on. If three sources disagree, the disagreement is the finding — usually it means the population you imagined is several distinct sub-populations that should be evaluated separately, or the pain is real but localized.

A single proxy is usually not enough. The Drawer / Tally evaluations earlier in this methodology's life leaned heavily on a single Shopify CashUp proxy — informative, but a stronger evaluation would have triangulated against Square community forums, Capterra complaints, and at least one industry-vertical signal source.

## What proxies prove and don't prove

**Proxies prove** the market exists at scale, what participants already pay for, what they reject (via failed-product post-mortems), whether the segment is growing or shrinking, what acquisition channels could reach them.

**Proxies do not prove** that any specific person will pay you. Customer interviews remain the only way to validate willingness-to-pay for a specific product. Proxies tell you which customers are worth interviewing — that's their job.

## Failure modes

1. **The market is too small to show up in any proxy.** Itself a signal. A market truly invisible to payment rails, government data, distributor reports, App Stores, Reddit, and trade publications is usually too small to support paid SaaS economics.

2. **The proxies disagree systematically.** Usually means the segment is in transition (formalizing, fragmenting, being eaten by an adjacent category). Worth investigating; don't commit until the disagreement resolves.

3. **You over-weight the loudest proxy.** The single most quoted complaint isn't the most common pain — it's the loudest expression of a pain that may or may not be most common. Tally pain counts, not pain volume.

4. **You under-weight the founder's own channel access.** A market researchable from a desk isn't the same as a market reachable by a US-based solo founder. Pair proxy research with founder-channel-access reality (see `07-anti-patterns.md` → "Founder-market fit blindness").

## See also

- `industry-guides/` — calibrated proxy sources for specific verticals
- `tooling/recommended-tools.md` — the actual tools (Firecrawl, Exa, Reddit access, app-store scrapers, etc.) to execute proxy research effectively in 2026
