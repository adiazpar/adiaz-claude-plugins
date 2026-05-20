# Agent prompt: Codebase ICP audit

Use this when there is an existing codebase and the founder does not have a clear, confident answer to "who pays for this." The prompt directs a research agent to read the code (schema, route handlers, modal flows) and reverse-engineer the implicit target user from the features that have been built.

Dispatch with a general-purpose agent that has Read, Grep, and Bash tools available. Allow 15–25 minutes of agent time. Output is a written report, ~1500 words.

## Template — fill in `{PROJECT_NAME}`, `{ROOT_PATH}`, `{LANGUAGE_NOTES}`, and any project-specific constraints

```
You are auditing the {PROJECT_NAME} codebase at {ROOT_PATH} to reverse-engineer the implicit target user (ICP) from what's been built. The founder is having an identity crisis about who this app is for and wants an honest, evidence-based read on what kind of person the existing features assume.

CRITICAL CONSTRAINTS:
- DO NOT read anything under .claude/, docs/, README.md, or any prior strategic-framing documents. Those carry conclusions that would bias your audit. We want a fresh read from source code only.
- DO read actual code: schemas first, then route handlers, then component / modal flows, then shared types and helpers.
- Cite specific files and tables. Do not vibe-check at the feature-name level — read constraints.

{LANGUAGE_NOTES — e.g. "The schema lives in packages/shared/src/db/schema.ts. Backend routes are in apps/api/src/app/api/. Frontend flows in apps/web/src/components/."}

What you're producing — a written report (under 1500 words) that answers:

1. Feature inventory by domain — list the major feature areas (sales, products, providers, team, AI, etc.). For each, one sentence in user terms (not implementation).

2. What each feature assumes about the user — for each major area, list 2–4 implicit assumptions. Examples of the kind of assumption I mean: "user has physical inventory they restock from suppliers" / "user transacts in cash at a stall, not online" / "user has employees who need separate logins" / "user runs multiple distinct businesses". Be concrete, cite specific files / tables that gave you the signal.

3. Synthesized implicit ICP(s) — given all assumptions in (2), describe the kind of person who would actually need ALL of this. Don't sanitize — if the assumptions point to a narrow weirdly-specific persona, say so. If they point to 2–3 plausible personas, list them with the tradeoff. Include: business type, scale (revenue / employees / locations), formality (registered? has accountant?), tech-savviness, geography signals.

4. Niche-vs-broad audit — split the feature list into:
   - Broad — features almost any small business operator would use
   - Niche — features that only fit a specific subset
   - Specifically evaluate features the founder suspects are over-built or over-niche.

5. The 3 sharpest signals — what 3 pieces of evidence most strongly constrain who this app is for? (e.g. "the existence of X table means the user must be doing Y").

6. What's notably missing — what does the code NOT have that you'd expect for adjacent ICPs? (e.g. no e-commerce integration → not for online sellers; no employee scheduling → not for restaurants with shifts; etc.) This list is often more useful than the implicit ICP itself because it constrains the pivot space.

Tone: honest, specific, no hedging. Cite file paths and tables. The founder needs to know what they actually built, not a flattering interpretation.
```

## How to read the output

- The "sharpest signals" section is what to trust most. It is the part most firmly grounded in schema constraints.
- The "what's missing" list constrains the next research pass. If the schema cannot represent customer records, do not later research segments that need customer records without first accepting that this is a multi-month schema change.
- If the synthesized ICP returns multiple equally plausible personas, the code is unfocused. That's a finding, not a failure of the audit.
- If the founder doesn't recognize the implicit ICP, the drift was unconscious. Spend extra time here before moving to profitability research.

## Common adjustments

- For a frontend-only product, replace "schema" with "data types and component state". The audit logic is the same.
- For an API product, prioritize the route handlers and the request/response schemas over UI flows.
- For a library or framework, the audit produces a different output (who would import this?). The agent will need different scoping.
