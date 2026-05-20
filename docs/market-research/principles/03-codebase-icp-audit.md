# Principle: Reverse-engineer the ICP from the code

When you have already built a product and don't know who pays for it, you have a meaningful constraint hiding in plain sight: the code itself encodes assumptions about who's using it. Every feature votes about the user. The synthesis of those votes reveals the implicit ICP, which is usually different from the ICP the founder thinks they built for.

This is true because feature accretion happens faster than ICP reflection. A founder who starts with "I want to build for X" but adds features that fit Y will, within weeks, have a codebase that only fits Y. They will not notice. The audit is how you notice.

## What to read

For each meaningful feature or module, ask:

- What does the user need to be (role, business type, scale) to use this?
- What does the user need to have (employees, suppliers, inventory, equipment, customers)?
- What does the user need to do (workflow, frequency, time of day, environment)?
- What does the schema actively exclude that an adjacent ICP would need (tax fields, customer records, appointment slots, multi-location stock, online channel sync)?

Read the schema first, then route handlers, then modal flows. The schema is the most honest signal — it's the part most expensive to change later.

## What the synthesis produces

A persona description with five elements:

1. **Business type and scale.** Specific verticals, employee count, revenue range, location count.
2. **Formality.** Tax-registered? Has an accountant? Issues receipts? Operates in cash?
3. **Tech-savviness.** Mobile-first or desktop-first? Comfortable with X workflow?
4. **Geography signals.** What locales are coded? What currencies? Any regulatory hints (e-invoicing routes, specific compliance fields, country-specific business types)?
5. **What's structurally absent.** The list of adjacent ICPs the code cannot represent without schema changes. This list is often more useful than the implicit ICP itself because it constrains the pivot space.

## What this principle is not

It is not "what does the user want." That's a different question that requires talking to actual users. The audit only tells you who you have implicitly already built for.

It is not "is this a good ICP." The audit tells you the *shape* of the implicit user. Whether that user is profitable to serve is a separate question (see proxy-data research and ICP profitability evaluation).

It is not "what could the codebase serve with a pivot." That's the adjacent-market scan, which uses the audit as input but reaches further than the audit can on its own.

## Why this matters

Without this step, you will research markets that don't match the code you've built. Either you will pick an ICP that requires features you don't have (a wasted research pass) or you will pick an ICP that doesn't need features you do have (a wasted code base). The audit grounds the research in what's actually been built.

## Failure modes

1. **The audit returns "this fits anyone."** Usually wrong — it means the auditor stayed at the feature-name level instead of reading actual schema constraints. Re-run, instruct the agent to read code and cite specific tables and fields.

2. **The audit returns multiple equally plausible personas.** This is itself a signal — the code is unfocused. The right move is usually to ask whether the founder is willing to cut features to commit to one persona, not to research all of them.

3. **The audit returns a persona the founder doesn't recognize.** This is the most valuable case. The drift was unconscious. Spend extra time here before moving to profitability research.
