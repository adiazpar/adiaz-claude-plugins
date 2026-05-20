# Industry Guide: Consumer Apps

Direct-to-consumer mobile and web apps. Habit-forming, content-driven, social, productivity, finance, health, entertainment, lifestyle.

## Population characteristics

- **App Store / Play Store dominated.** Discovery is increasingly platform-controlled; organic install rates have declined for years.
- **High install volume, low retention.** Average consumer app loses 75%+ of users within 30 days. LTV math is brutal.
- **Content marketing and virality are the only viable acquisition for non-funded founders.** Paid app install is $3–15+ per install on iOS in competitive categories; recovers only with high LTV.
- **Network effects matter more than features.** The best product without users loses to a mediocre product with a community.

## Proxy sources calibrated for consumer apps

**Tier 1:**
- **App Store / Play Store reviews:** Capterra-equivalent for apps. Sensor Tower / data.ai / AppFollow / app-store-scraper (npm) for scraping. AppFigures for indie-friendly analytics.
- **Reddit:** r/iphone, r/Android, r/apps, r/AppHookup, category-specific subs (r/productivity, r/fitness, r/personalfinance, r/mealprep).
- **TikTok / Instagram / YouTube Shorts:** consumer-app discovery happens here. Search "best [category] app 2026" — what surfaces is what's winning.
- **Product Hunt:** consumer app launches and discussion. Reasonable signal for early adoption.

**Tier 2:**
- **Pew Research Center** and **Statista** for category-level consumer behavior.
- **Apple's annual "Best of" awards** and **Google Play awards** — signal of platform-favored aesthetic and category direction.
- **App Annie / Sensor Tower category reports** (paid; some excerpts free).

**Tier 3:**
- **TechCrunch / The Verge / Engadget** consumer app coverage — biased toward funded launches but signals "what is press-worthy in this category."
- **YouTube reviewers** — MKBHD, iJustine, etc. for hardware-adjacent apps; category-specific reviewers for niches.

## Competitor categories

1. **The default app on the OS.** Apple Health, Apple Notes, Google Photos, Google Calendar. These are free, pre-installed, and good-enough for the majority. The question is "why would someone download an alternative?"
2. **The category leader.** Spotify, Strava, Notion, Headspace, Cash App, MyFitnessPal. They have brand and network effects. Don't compete head-on.
3. **The viral niche player.** BeReal, Locket, Marco Polo, Wonderful — products that capture a specific moment in consumer culture. Often short-lived but generate copycats.
4. **The free vs paid sub-segment.** Most consumer apps have free competitors. The paid version must do something the free version structurally cannot — usually privacy, advanced features, or no ads.
5. **The web vs app vs desktop alternative.** Same need, different surface. Many "apps" lose to the web alternative when the web alternative is good.

## Acquisition channel realities

| Channel | Outcome | Cost |
|---|---|---|
| App Store / Play Store organic | Highly variable. ASO matters but the algorithm punishes lower-quality apps. | Free |
| ASO (App Store Optimization) | Keywords + screenshots + ratings drive installs. Solo founders can do this. | Time |
| Content marketing (YouTube / TikTok / blog) | Effective for category-specific apps. Slow start. | Time, low cash |
| Paid acquisition (FB / Google / TikTok ads) | $3–15 CPI in competitive categories. $15+ in finance, fitness. | Brutal for low-LTV apps |
| Influencer / creator marketing | Effective if budget allows. $500–5,000 per mid-tier creator partnership. | Mid-cost |
| Viral mechanics (referral, sharing, social proof) | The only "free" acquisition that compounds. Hard to engineer. | Free if it works |
| Press coverage | Once-and-done spike unless launch is exceptional. | Time |
| Subreddit communities | Effective for hobby / niche apps. Hostile to promotion. | Time, community |

## Pricing norms

- **Freemium with IAP:** the dominant pattern. Free download, premium features $4.99–14.99/mo or $30–80/year.
- **One-time purchase:** $0.99–$9.99. Decreasing share of market; works for utilities and games.
- **Subscription-only:** $4.99–$19.99/mo for serious productivity / health / finance apps. Higher in B2B-adjacent (Notion, Roam Research).
- **Lifetime deal:** $40–$199 one-time. Works for indie apps with low ongoing-cost.
- **Ad-supported:** mostly games and content. Display ads, $0.50–$2 RPM in 2026; rewarded video $5–15 eCPM.
- **App Store / Play Store take:** 15–30% depending on revenue tier and program. Bake into unit economics.

## Anti-patterns specific to consumer apps

1. **Building for yourself only.** Consumer apps need a target audience that is NOT you. "I built it because I wanted it" is sometimes a winning thesis but more often a niche-of-one.
2. **Ignoring retention math.** D7 / D30 retention determines whether your CAC is recoverable. If you don't have a hypothesis about retention, you don't have a business.
3. **Feature-matching the category leader.** You will lose. Win on a specific differentiator (privacy, speed, design, niche fit), not breadth.
4. **Building for both iOS and Android from day one.** Pick one. Both = half-built on both. Ship iOS-first if you're in a premium / paid category; Android-first if you're in a global / freemium / emerging-market category.
5. **Underestimating App Store policy risk.** Apple's review can reject for vague reasons. Android's policy enforcement is uneven. Build a contingency plan.

## When to walk from consumer apps as a category

Walk if you have none of: (1) viral / network mechanics inherent in the product, (2) an audience to seed the launch, (3) capital for paid acquisition, (4) a niche so specific that organic discovery in that niche is achievable. Consumer apps are the highest-CAC category in tech; non-funded founders without an audience usually lose.

## Stub status

Light coverage. Expand as you evaluate consumer-app candidates.
