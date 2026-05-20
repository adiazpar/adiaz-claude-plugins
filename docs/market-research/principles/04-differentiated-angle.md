# Principle: Name the differentiated angle before pressure-testing

This is the step the methodology was missing. Adding it changes which products survive evaluation.

A "product idea" by itself is not a thing the methodology can evaluate honestly. "Cash drawer reconciliation for Square" is a feature description, not a differentiated angle. Adversarial research aimed at that level of specificity will always find killers, because every feature has competitors and every category has free incumbents. The output looks rigorous but it's information-poor.

A **differentiated angle** is a three-sentence claim that survives independent scrutiny:

1. **What it specifically is** (capability + form factor)
2. **Who it's specifically for** (named segment with a verifiable pain)
3. **Why it specifically wins against the named alternatives** (the defensible delta)

Without all three sentences written out, there is nothing to pressure-test. The adversarial pass will produce a confident kill that's actually a kill of a poorly-formed proposal.

## What a real differentiated angle looks like

Bad angle (feature description):
> "A Square plugin for cash drawer reconciliation with per-employee variance attribution."

Better angle (named segment, no specific delta):
> "A Square plugin for cash drawer reconciliation with per-employee variance attribution, targeted at multi-shift convenience stores."

Real angle (passes all three sentences):
> "A Square App Marketplace plugin (form factor) that surfaces per-employee cash variance trends over 30/60/90 days (capability), targeted at independent convenience store owners with 5–15 part-time employees who currently lose $30–$100/week to unattributed drawer shortages and reconcile manually through spreadsheets (segment). It wins against Square Free's anonymous-drawer-only report and against switching to Toast (which costs $69/mo more) by isolating shortage attribution as a $14/mo standalone — a price the math pays back inside two months of caught shortages."

The third sentence — the "why it wins" — is the load-bearing part. It must name (a) the specific alternatives the buyer is currently using and (b) the specific reason this beats those alternatives. "Better UX" is not a reason. "Faster" is not a reason unless you can quantify against a named alternative. "AI-powered" is not a reason. A reason is something you can verify in customer reviews or product comparison: the specific gap one alternative leaves open and you fill.

## Where the angle comes from

The angle does NOT come from staring at your codebase and asking "what could this be." That produces feature descriptions, not angles.

The angle comes from **mining competitor reviews and customer-pain channels for the specific unmet need that the named alternatives have left open.** This is the part of proxy-data research that was under-applied earlier in this methodology's life. It is the central analytical move of finding a viable product.

Practical sequence to find an angle:
1. List the named alternatives the target customer currently uses (free incumbent, paid leader, DIY workaround, "switch to a different platform").
2. For each, mine reviews / forum threads / one-star Capterra ratings for what they specifically fail at — quoted, not paraphrased.
3. Look for a complaint cluster that recurs across alternatives AND is specific enough to address with a focused product AND is not a complaint about pricing alone.
4. Write the three sentences. If you can't write the third sentence with a specific named alternative and a specific named gap, you haven't found an angle yet.

## When you can't find an angle

Two outcomes when this principle is applied honestly:

1. **You find an angle.** Now you have something to pressure-test. The adversarial pass becomes much more useful because the killer it produces is specifically about your angle, not about the category. If the angle survives the pressure-test, you have a candidate worth customer-discovery-validating.

2. **You can't find an angle.** This is itself a verdict — and a more honest one than the adversarial pass would have given you. "No differentiated angle exists for this product in this market for this founder" is a real conclusion. It saves the cost of a pressure-test that would have killed it for the wrong reason.

The second outcome happened repeatedly in this methodology's earlier life. Drawer ("Square cash drawer reconciliation") had no real differentiated angle — the third sentence couldn't be written because the alternatives (Square Free + spreadsheet) were adequate for the pain Drawer addressed. Tally ("Square denomination counting") had no real angle because the named alternatives (free PDF count sheets, the phone calculator, Toast for switchers) were adequate or strictly superior. In both cases, the adversarial pass found a killer — but the real failure was upstream: the angle was never named.

## Defensibility test

If you have an angle, run a defensibility check before pressure-testing. The angle is defensible if at least one of the following is true:

- **Distribution advantage:** you have a channel competitors don't (a community, a list, a partner network, a regulatory license)
- **Data advantage:** the product generates data that compounds over time and is hard to replicate
- **Cost advantage:** you can serve the segment at a structurally lower cost (e.g., self-serve where competitors require sales)
- **Specialization advantage:** the segment is too small for generalists but lucrative enough for you (the "niche down to win" play)
- **Founder advantage:** you have lived experience in the segment that competitors don't and that translates into product judgment
- **Timing advantage:** a recent change (regulation, technology, platform shift) created an opening that hasn't yet been filled

If none of these is true, the angle is real but not defensible. A non-defensible angle can still be a viable side project, but it should not be a primary commitment.

## Failure modes

1. **"We'd be better."** Not an angle. Refine until the third sentence names a specific gap, not a vague superiority.

2. **"AI-powered" / "modern UX" / "mobile-first."** Not angles. These are implementation choices. The angle is what the AI / UX / mobile-first specifically enables that the named alternatives don't.

3. **The angle is a pricing claim only.** "Same product, cheaper" is a strategy, not an angle. It's also usually a doomed strategy because incumbents can drop price faster than you can acquire customers.

4. **The angle requires changing the customer's behavior.** "If only they tracked variance per employee, they would have caught this earlier" is not an angle if they currently aren't motivated to track that. The behavior-change tax usually kills products targeting low-motivation populations.

## See also

- `01-research-stance.md` — this principle is the bridge between exploratory and adversarial stances
- `agent-prompts/04-differentiated-angle.md` — the agent prompt template that produces the angle analysis
- `05-pressure-testing.md` — what to do once the angle is named
