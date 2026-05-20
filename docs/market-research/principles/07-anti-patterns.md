# Anti-patterns

Things this methodology has watched fail. Each is described as the failure mode, the warning sign, and the corrective.

## Romance bias

**The failure:** Building (or researching) for a market because of a personal observation, without checking whether you have operating access to it. A founder visits Lima, observes an artisan fair, falls in love with the idea of serving those vendors, and builds a product for them despite living in the US, not speaking Spanish fluently, having no local relationships, and no way to do field research.

**The warning sign:** When you describe the target market, you reach for a specific scene or memory rather than a specific buying decision. "I want to help the kind of person I saw at X" is romance. "I want to help the kind of person who pays Y for Z" is research.

**The corrective:** Personal observation can generate hypotheses, never conclusions. Before committing to a market spotted in the field, check the proxy data for it AND check whether you can credibly acquire customers in it. If the answer to either is "I don't know," the answer to "should I build for this market" is "not yet."

## Single-pass commitment

**The failure:** Reading one research output, finding it compelling, and starting to build. The first agent's recommendation feels true in the moment because nothing has contradicted it yet. Three months later the founder discovers that the recommendation rested on three claims that would not have survived a pressure test.

**The warning sign:** "This recommendation makes sense; let me start designing the feature." The sentence "let me start designing" before any adversarial review has been performed.

**The corrective:** Always run a pressure test before commitment. Always. The pressure test takes hours; the misdirected build takes months. There is no exception in which skipping the pressure test is the right call.

## Segment-narrowing before audit

**The failure:** A founder with a built codebase decides "I want to target X segment" without first auditing what the codebase actually fits. They then research X, find a path, and start working — only to discover that X needed features they don't have and didn't need features they do.

**The warning sign:** The founder names a target segment before describing what's been built. The segment was picked from inspiration, not from the code.

**The corrective:** Always run a codebase ICP audit first when there is existing code. The audit either confirms the founder's intuition (now grounded in evidence) or reveals drift (which the founder should know about before researching markets).

## Free-incumbent oversight

**The failure:** Recommending a paid SaaS product that competes against a free incumbent without acknowledging the free incumbent. In small-merchant SaaS specifically, Square Free is the baseline you compete against. In document collaboration, Google Docs is the baseline. In project management, free Trello / Asana / Notion tiers are the baseline. If your product is "better X" at $19/mo, and the free tier is good enough for 80% of your target market, your real addressable market is the 20% the free incumbent can't serve. That's a different market than the keyword search suggests.

**The warning sign:** Pricing claims like "the alternative is Square + Craftybase + spreadsheet at $50+/mo." The alternative is what most users actually do, not what you wish they did.

**The corrective:** When evaluating any tooling segment, find what most users currently do, not what they should do. The current behavior is the price ceiling on any replacement. Free incumbents are everywhere — beat them, or compete in a different segment.

**Important counterweight:** Free generics lose to specialized paid tools constantly. ConvertKit beat free Mailchimp tier for creators. Linear beat free Jira for engineering teams. Notion beat free Google Docs for collaborative work. "Free exists" is not "free wins" — what matters is whether your specialized angle is worth paying for, and whether the segment you're targeting feels the specialized pain enough to switch.

## User count → TAM extrapolation

**The failure:** A competitor has 7 million users. Therefore the addressable market is 7 million. Therefore at 1% conversion at $20/mo this is $1.7M MRR. None of this math is true. The 7 million users are mostly free / inactive / never-pay; the 1% conversion is fictional; the $20/mo is unobtainable. The arithmetic is precise and the inputs are wrong.

**The warning sign:** Any TAM calculation that multiplies a raw user count by a conversion rate by an ARPU. If the founder cannot defend each of those three numbers with citations, the calculation is decorative.

**The corrective:** Look at the competitor's actual revenue (from public filings, getlatka, ProductHunt acquisition prices) and divide by their user count to get real ARPU. For Treinta, that math gave $5.86/year, not $240/year. The ARPU you derive from public data is the ceiling you should plan against, not the ARPU you wish were true.

## Capability fit confused with market fit

**The failure:** "Our code can serve this market" gets treated as "this market will pay us." It will not. Capability fit is necessary; market fit is separate. A codebase that fits a market the market doesn't want to pay for is the worst position to be in — close enough to feel real, far enough to fail.

**The warning sign:** "We have all the features X market needs." That's a capability claim. The market-fit claim is "X market currently pays for tools that have features we have, in volumes we can serve, at prices our unit economics support, through channels we can access." Notice how much more is required.

**The corrective:** Whenever capability fit is asserted, immediately ask "and what is the market-fit evidence." If the answer is "I think they'd pay $X," the evidence is missing. If the answer is "comparable tools charge $X with Y churn and acquisition through Z," the evidence is present.

## Features-not-products extraction

**The failure:** Recognizing that a specific capability in the codebase is technically impressive (an AI feature, a custom algorithm, a niche workflow) and concluding that it can be sold as a standalone product. Often it can't. Many capabilities feel like products but are features that only matter when integrated with everything else around them.

**The warning sign:** "If we just packaged X as an API…" Said quickly, without checking whether a buyer would pay enough for that API to support a business.

**The corrective:** Run the buy-vs-build honest test. At realistic scale, how much would the prospective buyer pay you versus building the thing themselves with the same primitive (OpenAI API, a barcode library, a 200-line plugin)? If the cost difference is less than 10x, you don't have a product. You have a feature that is more valuable inside its current home than outside it.

## Founder-market fit blindness

**The failure:** Treating "market exists" as sufficient evidence that you should serve it. A profitable market that requires Spanish fluency, in-person visits to Peru, BD relationships with Mexican distributors, and a five-person team is not your market if you are a US-based solo founder who speaks limited Spanish. The market is real; your access to it is not.

**The warning sign:** When evaluating a segment, you list reasons the market is attractive but never list the channels you would use to acquire customers from it. The acquisition fit is the part most likely to be hand-waved.

**The corrective:** For every candidate market, require a specific acquisition story. Three sentences that describe (1) where you find prospects, (2) how you contact them, (3) at what cost. If you can't write those three sentences for a market, you don't have access to it, no matter how attractive it is.

## Kill-machine drift

**The failure:** Running every research pass in adversarial stance, regardless of phase of work. The methodology produces back-to-back kills, each one feels rigorous, but the cumulative effect is that no product ever gets the chance to defend itself because no product ever had its angle named. The output looks like good research; the conclusion is paralysis.

**The warning sign:** Three or more sequential adversarial passes that each produce "no" verdicts on candidates that were never given a named differentiated angle. Founder starts to suspect "nothing works" rather than "I keep researching at the wrong level of specificity."

**The corrective:** Pause and run an exploratory pass. See `01-research-stance.md`. Before any adversarial pass, the differentiated angle must be named in writing (`04-differentiated-angle.md`). An adversarial pass against a poorly-formulated proposal produces a low-information kill. An adversarial pass against a well-formulated angle produces a useful verdict.

## Competition-existence-as-kill

**The failure:** Treating "competitors exist in this category" as a kill signal. Every successful product had competitors at launch. Stripe shipped against PayPal and Authorize.net. Notion shipped against Evernote and Google Docs. Shopify shipped against Yahoo Stores and Magento. Linear shipped against Jira and Asana. The fact that a category has incumbents is a sign that there's revenue in the category — not a sign that the category is closed.

**The warning sign:** A research output that lists competitors and concludes "the slot is taken" or "this segment is owned" without identifying which specific competitor would beat your differentiated angle and on what specific axis.

**The corrective:** Distinguish competition from foreclosure. Competition means others are in the category. Foreclosure means a specific competitor with a specific moat would beat your specific angle. Foreclosure requires evidence; competition is just the default state of any worthwhile market. The right question is which competitor, on which axis, beats which version of your angle — not "are there competitors."

## Single-proxy extrapolation

**The failure:** Reading one failed comparable (e.g., Shopify's CashUp at 0 reviews) as dispositive proof that the entire category is unviable. One product's failure could be due to UX, marketing, positioning, timing, pricing, or app-store SEO — not necessarily category failure. Treating one proxy as dispositive overestimates signal strength.

**The warning sign:** A verdict that hinges on a single competitor's poor performance metric, without triangulating against at least two other independent proxies.

**The corrective:** Require triangulation. A verdict that one product failed should be reinforced by independent proxies (forum demand signals, review patterns, switching-cost data, regulatory shifts) before being treated as a category-level conclusion. One signal is suggestive; three convergent signals is dispositive.
