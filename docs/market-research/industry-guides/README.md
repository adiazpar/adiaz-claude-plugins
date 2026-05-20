# Industry Guides

Industry-specific calibration for the methodology. The principles in `../principles/` are general — they apply to evaluating any digital product. The industry guides in this directory specify *which proxy sources, which competitor categories, which acquisition channels, which pricing norms, and which anti-patterns matter most* for a given vertical.

Use these as a lens, not a substitute. The principles are the methodology; the guides tell you how the methodology shows up in a specific market.

## How to use a guide

1. Read the relevant guide before running any research pass for a product in that industry.
2. Use the guide's proxy-source list as the input to `02-proxy-data-research.md`.
3. Use the guide's competitor categories as the input to `04-differentiated-angle.md`.
4. Use the guide's acquisition channel realities as the input to `04-icp-profitability` or extraction evaluation.

## When to write a new guide

Write a new guide when you've evaluated 2+ products in a vertical and noticed that the proxy sources, pricing norms, or anti-patterns diverged from the defaults. The guide compresses that learning so the next evaluation doesn't re-derive it.

## Guides

- `retail-and-smb.md` — Small-merchant SaaS, POS, inventory, payments, micro-merchants. The vertical the methodology was originally developed in. Includes Square / Toast / Shopify ecosystem specifics.
- `developer-tools.md` — Dev infra, CLIs, libraries, IDE plugins, productivity tools for engineers. Different proxies (GitHub stars, dev.to, Hacker News, npm downloads). Different sales motion (self-serve, often free with paid premium).
- `consumer-apps.md` — Direct-to-consumer mobile and web apps. App Store / Play Store dynamics, content marketing as primary acquisition, freemium and IAP economics.
- `infrastructure-saas.md` — DevOps, observability, security, data infra. Enterprise sales motion, longer cycles, design-partner programs, OSS-leading-to-commercial patterns.
- `ai-tools.md` — AI-native products, agent platforms, model-wrapper plays, AI-augmented vertical SaaS. Moving target — most categories under 2 years old. Strong commodity-wrapper risk.
- `services-and-local-businesses.md` — Service businesses (salons, fitness, tattoo, pet, fitness, mobile services). Appointment-driven. High Reddit / Instagram presence; under-served by Square / Shopify ecosystem.

## Stubs in progress

Several guides are stubs as of writing — structure and pointers exist but full content is yet to accumulate. Fill them in as you evaluate products in those verticals.
