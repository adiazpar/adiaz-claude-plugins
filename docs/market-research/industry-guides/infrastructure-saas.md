# Industry Guide: Infrastructure SaaS

DevOps, observability, security, data infrastructure, internal-tools platforms. B2B, often enterprise-adjacent, longer sales cycles than dev tools.

## Population characteristics

- **Buyers are technical or technical-adjacent.** CTO / VP Engineering / Director of Platform / SRE leads. They do research, read docs, and often try-before-buy.
- **Sales motion mixes self-serve and assisted.** PLG (product-led growth) often opens the conversation; AE (account executive) closes the contract. Pure self-serve caps around $10–50k ACV; assisted goes higher.
- **Compliance and security review are gating.** SOC 2, HIPAA, FedRAMP — required for enterprise contracts. Non-trivial cost; multi-month process.
- **Adoption is bottom-up but procurement is top-down.** Engineers find and pilot the tool; finance and security approve the purchase.

## Proxy sources

**Tier 1:**
- **G2 / Capterra / TrustRadius** B2B SaaS reviews — buyer-side commentary, not just user
- **Reddit:** r/devops, r/sre, r/sysadmin, r/kubernetes, r/aws, r/dataengineering
- **Hacker News:** new infra companies announce here; comment depth reveals depth of unmet need
- **Cloud-vendor partner directories:** AWS / GCP / Azure marketplace listings reveal what's being deployed
- **Datadog / Sentry / PagerDuty / Snowflake quarterly earnings** — what they emphasize on roadmap calls is what enterprises ask for

**Tier 2:**
- **Gartner / Forrester Magic Quadrant** reports — paywalled but excerpted in vendor marketing
- **CNCF (Cloud Native Computing Foundation) annual survey** — adoption rates of OSS infra projects
- **State of DevOps Report** (Puppet / DORA) — operational metrics across orgs
- **Stack Overflow Developer Survey** — adoption shares for major infra categories

**Tier 3:**
- **VC investment memos:** Bessemer's State of the Cloud, Sequoia memos, a16z investment theses
- **Conference talks:** KubeCon, re:Invent, Strange Loop, SREcon — what speakers care about predicts category direction
- **Twitter/X DevOps community** — narrow but high-signal

## Competitor categories

1. **The OSS standard.** Prometheus, Grafana, Postgres, Kafka, Kubernetes. The free baseline.
2. **The commercial winner.** Datadog, Snowflake, Confluent, MongoDB — usually OSS-leading-to-commercial.
3. **The hyperscaler offering.** AWS / GCP / Azure managed services. The "good enough and already in our cloud" alternative.
4. **The legacy enterprise vendor.** Splunk, ServiceNow, Oracle — slow but entrenched. Replacement plays target these.
5. **The internal build.** Many large companies build their own. "Replace your internal tool" is a real category.

## Acquisition channel realities

- **PLG (product-led growth)**: free tier or free trial → self-serve adoption → upsell to paid. Standard for cloud infra in 2026.
- **Developer evangelism**: open-source release, conference talks, blog content, docs as marketing.
- **Design partners**: 5–10 named customers who co-design the product for early credibility. Standard playbook for $100k+ ACV.
- **Outbound sales**: SDR/BDR teams cold-emailing target accounts. Effective at scale; not a solo founder play.
- **Hacker News / Reddit launches**: opening salvo, not sustained channel.

## Pricing norms

- **Free tier + paid cloud:** $0 to $1,000+/mo per workspace, scaling with usage
- **Per-seat:** $20–100/user/month for B2B with seat-based usage
- **Usage-based:** API calls, ingested data volume, compute hours. Datadog, Snowflake, Vercel pattern.
- **Enterprise contracts:** $50k–$500k ACV with annual prepay, SLA, support tier
- **Marketplace listings:** AWS / GCP / Azure marketplace adds discoverability + procurement-ease at ~3% take

## Anti-patterns specific to infrastructure SaaS

1. **Building for a category you don't operate in.** Selling observability to SREs when you've never been on-call. Sales conversations fail in 60 seconds.
2. **Premature enterprise focus.** Trying to land $100k ACV deals before you have a self-serve flow that works at $100/mo. Skip a step, lose two years.
3. **Ignoring SOC 2 cost.** Real cost is $30k–$100k + months. Plan for it before enterprise pipeline opens.
4. **Underestimating switching cost on the buy-side.** Replacing infrastructure is multi-quarter migration. Customers don't switch on a whim.
5. **Building a generalist product.** Datadog and Splunk own "generalist observability." Win in a specific vertical (security observability, embedded telemetry, ML infra) where you can be 10x better.

## When to walk

Walk if you don't have: (1) operating experience in the category, (2) capital for an 18–36 month payback cycle, (3) network of enterprise design-partner candidates, (4) ability to maintain a SOC 2 program. Infra SaaS is a capital-intensive category dominated by patient teams.

## Stub status

Brief skeleton. Expand if evaluating infra SaaS candidates.
