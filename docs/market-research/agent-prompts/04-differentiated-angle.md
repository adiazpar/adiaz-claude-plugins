# Agent prompt: Differentiated angle identification + defensibility check

Use this **after** a candidate has been identified (codebase audit, adjacent-market scan, or fresh exploration) and **before** any adversarial pressure test. This is the missing analytical step that produces a defensible angle to test, rather than a feature description that fails on first attack.

The output is either: (a) a named angle worth pressure-testing, or (b) a clear "no angle exists for this candidate" verdict. The second outcome is real and useful — it saves the cost of an adversarial pass that would have killed the candidate for the wrong reason.

Dispatch with a general-purpose agent (Opus recommended). Allow 25–40 minutes. Output is ~1500–2000 words.

## Template — fill in `{CANDIDATE_PRODUCT}`, `{TARGET_SEGMENT}`, `{NAMED_ALTERNATIVES}`

```
You are running a differentiated-angle identification pass on a candidate product. Your job is to either (a) name the specific differentiated angle and verify it's defensible, or (b) honestly conclude that no defensible angle exists for this candidate.

You are NOT in adversarial mode. You are in exploratory + analytical mode. The point is to FIND the angle if one exists, not to kill the candidate. (A separate adversarial pass happens after this one if an angle is found.)

Required reading before starting:
- `/Users/adiaz/market-research-methodology/principles/04-differentiated-angle.md` — what counts as a real angle
- `/Users/adiaz/market-research-methodology/principles/08-tool-selection.md` — which tool to use for each research task. Check your tool list against the preference table. If specialized tools (Firecrawl, Exa, Apify Reddit, app-store-scraper) are available, use them in preference to WebSearch/WebFetch.

**The candidate under evaluation:**
{CANDIDATE_PRODUCT — e.g. "A Square plugin that adds session-package functionality to Square Appointments — buy 10 yoga classes, auto-deduct on each visit"}

**The hypothesized target segment:**
{TARGET_SEGMENT — e.g. "Service businesses on Square Appointments — fitness studios, salons, tutors, massage therapists — currently using workarounds (manual class cards, third-party booking tools, switching to MindBody)"}

**The named alternatives the segment currently uses:**
{NAMED_ALTERNATIVES — e.g. "(1) Square Appointments without packages, with manual tracking; (2) MindBody at $129+/mo; (3) Acuity at $20+/mo; (4) Vagaro at $30+/mo; (5) Paper class cards and Google Sheets"}

**Your task:**

1. **Competitor review mining.** For each named alternative, mine reviews / forum complaints / one-star ratings for what they specifically fail at. Quote complaints, don't paraphrase. Sources to use:
   - Capterra, G2, Trustpilot reviews of named alternatives
   - Reddit subs where the segment hangs out (be specific about which subs)
   - Twitter/X mentions, Indie Hackers threads
   - YouTube tutorial comments
   - The platform's own community forum (if applicable)
   - Vertical trade publications

2. **Complaint clustering.** Across the named alternatives, identify clusters of complaints that recur and could be addressed by the candidate. The cluster needs to be:
   - Specific (not "bad UX")
   - Recurring (not one-off)
   - Not solely a pricing complaint
   - Addressable by the candidate's capabilities

3. **Angle formulation.** Write the differentiated angle in exactly three sentences:
   - Sentence 1: What it specifically is (capability + form factor)
   - Sentence 2: Who it's specifically for (named segment with a verifiable pain)
   - Sentence 3: Why it specifically wins against the named alternatives (the specific delta)

   The third sentence is load-bearing. It must name (a) the specific alternatives the buyer is currently using and (b) the specific reason this beats those alternatives on a verifiable axis.

4. **Defensibility check.** Test the angle against the six possible defensibility advantages:
   - Distribution advantage (channel competitors don't have)
   - Data advantage (data that compounds and is hard to replicate)
   - Cost advantage (structurally lower cost to serve)
   - Specialization advantage (too niche for generalists, lucrative enough for you)
   - Founder advantage (lived experience the competitors lack)
   - Timing advantage (recent regulatory / technology / platform shift)

   If at least one is true with citable evidence, the angle is defensible. If none is true, the angle is real but indefensible — a side-project, not a primary commitment.

5. **Verify the angle's claims.** Each clause in the three-sentence angle is a claim. Verify:
   - The capability claim — does the candidate actually deliver this? Read the codebase or product spec.
   - The segment claim — does the segment exist at scale? Cite size estimates.
   - The pain claim — is the pain real and felt? Cite specific complaints.
   - The alternatives-comparison claim — does the candidate genuinely beat the alternatives on the named axis? Cite review quotes.

**Output structure (under 2000 words):**

1. **The named angle** — three sentences, exactly as the principle prescribes.

2. **Competitor review mining results** — for each named alternative, the 3–5 most representative complaints, quoted with source URLs.

3. **Complaint clusters** — the patterns that emerged across alternatives, with quote counts.

4. **Angle defensibility analysis** — which of the six advantages applies, with evidence.

5. **Angle verification** — each clause checked, with the supporting evidence.

6. **Verdict on the angle** — one of:
   - **Strong angle.** Three clauses are all defensible; at least one defensibility advantage is real. Pressure-test recommended.
   - **Weak angle.** The third sentence couldn't be written specifically, OR no defensibility advantage holds. Either reformulate or abandon — pressure-testing this won't produce useful information.
   - **No angle.** Honest analysis cannot produce a defensible three-sentence statement. The candidate is a feature description, not a product. Save the cost of the adversarial pass.

7. **If strong angle:** what specifically should the next-pass devil's-advocate agent attack? List the three weakest clauses.

8. **If weak or no angle:** what's the closest viable candidate the research surfaced? Often the complaint-cluster mining reveals a different, better candidate than the one you started with — name it.

9. **Scope of this verdict — REQUIRED for any NO or Weak result.** Explicitly bracket what the verdict applies to. Pick exactly one:
   - **(a) Candidate-specific NO.** This particular candidate doesn't work for these specific reasons. A different candidate in the same sub-category might. The framing isn't ruled out.
   - **(b) Sub-category NO.** This specific sub-category (e.g., "Square Appointments service-business plugins," "AI-image-enrichment APIs for solo founders") has structural issues that would likely repeat across similar candidates. Worth checking the meta-pattern principle before another candidate in this sub-category.
   - **(c) Category-wide NO.** The broader category (e.g., "all Square plugins," "all consumer mobile apps") is foreclosed for this founder. This is a strong claim — provide separate evidence that the failure mode generalizes, not just that this candidate failed.
   - **(d) Insufficient evidence to bracket scope.** The verdict is candidate-specific until further research expands or narrows it.

   Default to (a) unless you can cite specific evidence the same killer would apply to other candidates in the same scope.

   This is not optional. Meta-pattern recognition depends on accurate scope labeling. A NO that's actually candidate-specific (a) but reported as category-wide (c) causes over-generalization downstream; a NO that's actually category-wide (c) but reported as candidate-specific (a) causes under-reaction.

**Critical constraints:**
- Cite every quote and statistic with URLs.
- Do not produce an angle by softening the criteria — if no defensible angle exists, say so.
- Do not pre-emptively attack the angle. The pressure-test pass is separate.
- If you surface a better candidate during review mining (a complaint cluster that doesn't fit the original candidate but fits a different product), say so and recommend pivoting to that candidate.

This is the analytical move that produces something worth pressure-testing. Without it, the adversarial pass kills feature descriptions and produces low-information verdicts.
```

## How to read the output

- **Strong angle verdict:** proceed to pressure-testing (`05-devils-advocate.md`). The pressure test now has a specific target.
- **Weak angle verdict:** reformulate the candidate or abandon. Do NOT proceed to pressure-testing without a strong angle.
- **No angle verdict:** this is itself a valuable outcome. The candidate dies cleanly without burning an adversarial-pass budget.

## When this prompt fails

- The agent produces an angle that sounds specific but doesn't survive the verification step. Re-run with stricter instructions to verify each clause against specific evidence.
- The agent reports "strong angle" but the third sentence is vague. Push back — require the third sentence to name the specific alternative and the specific axis on which the candidate wins.
- The agent gets stuck on a candidate that has no real angle. Allow it to recommend pivoting to a candidate surfaced during the review-mining phase.

## What this prompt is not

It is not the adversarial pass. The job is to find the angle, not kill it. The kill attempt happens in `05-devils-advocate.md` after the angle is named.

It is not customer discovery. The angle is desk-derived from competitor complaints and segment-pain proxies. Real customer interviews still need to happen before commitment — this prompt makes those interviews more efficient by giving them a specific angle to validate.
