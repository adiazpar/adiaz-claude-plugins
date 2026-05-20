# Industry Guide: Retail and SMB

The vertical the methodology was originally developed in. This guide compresses what was learned across the Kasero evaluation (LatAm bodegas, US food trucks, makers, market organizers, convention vendors), the Drawer evaluation (Square cash-drawer plugin), and the Tally evaluation (Square denomination-counting plugin).

## Population characteristics

- **Digital presence is mixed.** US Square sellers post in forums and review sites; informal LatAm / SEA / South Asian merchants are mostly digitally quiet. Researching the former needs Capterra / Reddit / community forums. Researching the latter needs proxy data (payment rails, distributors, government stats).
- **Tech-savviness skews low.** Smartphones yes, complex software no. Onboarding friction kills more products than feature gaps.
- **Cash matters more than the keyword search suggests.** Even in 2026, 19% of US Square transactions are cash. In LatAm informal sectors, cash is dominant. Don't assume a "card-only" workflow.
- **Acquisition is fragmented.** Each vertical (convenience, coffee, food trucks, salons) has its own associations, trade publications, subreddits, and YouTube channels. No single channel reaches all retailers.

## Proxy sources calibrated for retail

**Tier 1 (start here):**
- **Square community forum** (community.squareup.com) — `/t5/Feature-Requests/` is the highest-density signal for what Square sellers want. Sort by kudos. A 10-kudo request is rare but should not be treated as "high demand" without triangulation.
- **Capterra / G2 / Trustpilot reviews** of Square POS, Shopify POS, Toast, Lightspeed, Clover. One-star reviews are gold for unmet need.
- **Reddit:** r/smallbusiness (~2M), r/SquarePOS (~3k), r/Etsy (~700k), r/conveniencestore (~30k), r/foodtrucks (~85k), r/coffee, r/Coffee, r/cafe, r/restaurantowners, r/Bakery, r/cosmetology, r/MUAontheCheap, r/barbershop, r/EnamelPins, r/ArtistAlley (~140k).

**Tier 2 (for sizing):**
- **Block 10-K** and **Square quarterly investor decks** — seller counts, GPV, vertical mix, ARPU benchmarks
- **IBISWorld** US industry reports (food trucks, coffee shops, convenience stores, smoke shops, vape shops, pet stores, barber shops, cannabis dispensaries) — number of businesses, growth rates, average revenue
- **NACS** (convenience store association), **NRA** (National Restaurant Association), **SCAA** (specialty coffee), **NACA** (cosmetology)
- **US Census Bureau** NAICS codes for businesses by sector
- **Eventbrite + Eventeny** for vendor-event counts (craft fairs, conventions, farmers markets)

**Tier 3 (for informal markets, especially LatAm / SEA / South Asia):**
- **Payment rail merchant adoption:** Yape, Plin, Nequi, Pix, Mercado Pago, M-Pesa, GCash, PayMaya, GoPay, Paytm, PhonePe
- **Distributor case studies:** AB InBev BEES, Coca-Cola FEMSA Mi Tienda, Bimbo Vendo, Diageo reports, JustoMx, Frubana post-mortems
- **National statistical offices:** INEGI (Mexico), DANE (Colombia), INE (Peru), IBGE (Brazil), DGEEC (Paraguay)
- **Development banks:** IDB, CAF, World Bank, CGAP, GSMA financial inclusion reports

## Competitor categories to map

When researching a retail/SMB product, the standard competitor map covers:

1. **The free incumbent.** Square Free, Shopify Lite, Excel + paper. Almost every retail SMB product competes against one of these. The free incumbent is the price ceiling and the baseline workflow.
2. **The paid-bundled competitor.** Toast, Lightspeed, Clover. These charge $69+/mo and bundle features that would otherwise require multiple plugins. They're the "switch the whole POS to escape this problem" alternative.
3. **The vertical specialist.** Per vertical, one or two specialists with deeper functionality and higher pricing — MindBody for fitness/salon, Booksy for barbers, Toast for restaurants, KORONA for convenience, Cova for cannabis. These are competition only inside their vertical.
4. **The DIY workaround.** Spreadsheets, paper logs, free PDF templates, phone calculator, physical hardware ($30 bill counter, $200 cash drawer). These are the actual most-common alternative.
5. **The platform's own roadmap.** Square's, Shopify's, Toast's roadmap. Anything in "Reviewed" status on Square's forum, anything mentioned in earnings calls — these are guillotines over plugins in those categories.

## Acquisition channel realities

For a US solo founder:

| Channel | Realistic outcome | Cost / time |
|---|---|---|
| Square / Shopify App Marketplace organic | 5–15 installs/mo after 6 months for a niche utility | Free; time to build + list |
| Reddit (r/smallbusiness, r/SquarePOS, vertical subs) | Heavy moderation; promotional posts banned; works only as long-tail helpful contributions | Free; months of community participation |
| Content / SEO | Ranking for "Square cash drawer" takes 9–18 months; traffic is real once ranked | $200/mo content tools + months |
| Cold outbound to merchants | Bodega / convenience store emails not on LinkedIn; reachable only via in-person foot traffic | High cost, low conversion |
| Distributor / association partnerships | Real channel for scale but 12–18 month BD cycles | Inaccessible for solo founders |
| Vertical Reddit communities | r/Etsy, r/foodtrucks, r/ArtistAlley actually engage with specific products | Works for high-fit niches |
| Service-business Instagram / Facebook | Salons, tattoo, fitness — actually reachable here | Works for service segments |

## Pricing norms

- **App Marketplace plugins:** $5–25/mo per location is the standard band. Above $25 needs strong specialization. Below $5 is impulse-purchase but margin-thin.
- **Standalone retail POS:** $0–200/mo. Free Square baseline; Toast at $69+/mo; vertical specialists $100–300/mo.
- **Compliance / regulated SaaS:** $50–500/mo. Justified by regulatory necessity (cannabis, alcohol, tax filing).
- **Marketplace rev share:** Square ~15%; Shopify recently raised to ~20%. Bake this into all unit-economics math.
- **Churn benchmarks:** SMB SaaS churn runs 5–10%/mo for single-feature plugins, 2–5%/mo for revenue-generating tools, 1–3%/mo for compliance-critical tools.

## Anti-patterns specific to retail/SMB

In addition to the general anti-patterns in `../principles/07-anti-patterns.md`:

1. **Assuming digital natives.** Retail SMBs are not your Reddit-savvy peers. UI complexity that you find acceptable is friction-blocking for them.
2. **Designing for the founder, not the floor worker.** The owner buys the software; an employee uses it. If the employee can't operate it at 10pm during a rush, the owner will switch.
3. **Underestimating cash workflows.** Even card-heavy businesses have cash handling. Don't ignore it because your customer profile says "card-first."
4. **Conflating verticals.** Coffee shops are not restaurants. Convenience stores are not c-stores-with-gas. Food trucks are not pop-up retail. Each has distinct workflows, pain points, and competitive landscapes. Verticalize.
5. **Pricing as if WTP is uniform.** A solo operator and a 15-employee location pay vastly different amounts for the same software. Most successful retail SaaS prices per location, not per user, because operators self-segment that way.

## Notable failed precedents (what to learn from)

- **Treinta / Khatabook / OkCredit / Dukaan / BukuWarung** — bookkeeping/inventory SaaS for informal merchants. All failed to monetize standalone; all pivoted to lending or shut down. Lesson: don't try to sell productivity SaaS to populations whose primary need is credit.
- **Frubana / Justo** — B2B marketplace for tienditas. Both contracted significantly. Lesson: distributor disintermediation is harder than it looks; the existing logistics moat is real.
- **Elo7** (Etsy LatAm) — handmade marketplace acquired by Etsy in 2021, sold at a loss in 2023, shut down in 2024. Lesson: marketplace monetization at LatAm price points is structurally hard.
- **Shopify's CashUp Cash Counter** — denomination-counting plugin, 0 reviews in 17 months despite Shopify's larger marketplace. Lesson: niche utility plugins die quietly on App Marketplaces unless they have unusual distribution.

## When to walk from retail/SMB as a category

If you are a US-based solo founder with no funding and no field-research access, the LatAm informal merchant market is structurally inaccessible to you (founder-market fit anti-pattern). The US small-merchant market has Square Free everywhere and vertical specialists in every niche — viable but brutally competitive on margin. Consider walking from retail/SMB if you don't have at least one of: (1) lived experience in a specific vertical, (2) a distribution channel into a specific vertical, (3) capital to fund a 12–24 month payback period, or (4) a defensible angle that survives the differentiated-angle pass.

This is not "retail/SMB is unviable" — many viable products exist here. It's "retail/SMB rewards founders with vertical fluency or distribution; punishes generalists."
