# Principle: When N passes converge, the framing is wrong

A research methodology that is willing to keep researching will eventually produce a recommendation, regardless of whether one exists. This is not a feature; it is the methodology's most dangerous failure mode. You will run pass after pass on different segments, each returning "this specific segment is not viable," and you will believe that the next segment is the one that works. It usually isn't.

The signal you need to notice: **when N independent research passes converge on the same negative answer for what appear to be different reasons, the reasons are not actually different. The framing is wrong.**

## What this looked like in practice

The research process that produced this methodology ran five passes:

1. The codebase implied a LatAm bodega ICP. Profitability research said: bodegas don't monetize as paid SaaS. Killed.
2. Adjacent US markets (food trucks, makers, market organizers). Pressure-tested. Three of five claims fell. Killed.
3. Convention vendor / artist alley. Pressure-tested. Conventory exists; schema mismatch; WTP squeezed by Square Free. Killed.
4. Standalone product extraction from the codebase. No viable extraction; best candidate is a side income, not a company.
5. Specific deep-dive on the AI capability as a B2B API. Features-not-products market; no successful precedent. Killed.

Five independent investigations, five honest convergent answers. The instinct after the third pass was to run a fourth — surely there must be a segment that works. The instinct was wrong. The signal at pass three was already that the framing was wrong: the question wasn't "which segment can this codebase serve" — it was "is this codebase the wrong shape for a paid-SaaS business from this founder's profile."

## The cost of missing the meta-pattern

Each pass costs hours. Five passes is a day, maybe two. That's cheap. But each pass also produces a recommendation that feels like progress and consumes the founder's attention. The opportunity cost of running pass six instead of accepting the meta-pattern is much larger than the literal time spent — it's the time spent emotionally committed to "the next pass will be the one." That commitment delays the harder decision (reframe or stop) by months.

## When to suspect the meta-pattern

Three signals, in order of severity:

1. **The negative reasons across passes start to overlap.** Pass two said "free incumbents own this." Pass three said "specialist incumbents own this." Pass four said "platform bundling eats this." These look different but they're the same root cause — the category is structurally hostile to your profile of founder. If you can compress the negatives into one sentence, you have the meta-pattern.

2. **The pressure tests keep finding the same killers.** If three different recommendations all died because of "incumbent X already does this," the issue isn't that you keep picking segments with strong incumbents — it's that every segment in this category has strong incumbents because the category itself attracts them.

3. **You start narrating "this pass is different because…"** The defense lawyer in your head is the meta-pattern surfacing. Notice it.

## What to do when the meta-pattern is real

Pause. Do not run another pass. Three honest options:

1. **Reframe the question.** Instead of "what market does this serve," ask "what's the smallest most defensible thing I could pull out of this codebase" (the extraction question). Or ask "what would this codebase look like in a category that isn't structurally hostile." Or ask "is this codebase the right vehicle for me at all."

2. **Accept the negative result.** Some research processes end in "no." That is a real outcome, not a failure to research enough. A founder who can take a hard negative answer cleanly is a founder who will not waste two more years on a doomed product.

3. **Step back from the methodology.** If the question itself has been wrong, the next research isn't more rigorous market research — it's reflection on what you actually want to build, what you have natural advantages for, what category you'd be in even if no specific idea existed yet. That conversation has different inputs than this methodology generates.

## When the meta-pattern is false

Two cases where N convergent negatives are actually different and the methodology should continue:

1. **Each pass was about a fundamentally different product.** If pass one was "B2B SaaS for X," pass two was "consumer mobile app for Y," and pass three was "open-source CLI tool for Z," then three negatives is three different markets returning negatives, not one meta-pattern. The methodology can continue.

2. **The negatives are about execution constraints that change.** If three passes said "no, because you don't have funding," then the meta-pattern isn't "the category is hostile" — it's "you need different resources." Acquiring those resources is the next move, not reframing the question.

In both cases, you should be able to articulate why the negatives are not the same root cause. If you can't, they probably are.

## Reading scope correctly

Agent prompts (per `agent-prompts/04-differentiated-angle.md` and `agent-prompts/05-devils-advocate.md`) require every NO or kill verdict to bracket its scope: candidate-specific, sub-category, category-wide, or insufficient-evidence. Meta-pattern recognition depends on reading these brackets correctly:

- **N candidate-specific (a) NOs do not constitute a meta-pattern.** Each one is informative about that candidate, not the category. Continue evaluating other candidates in the sub-category.
- **N sub-category (b) NOs DO constitute a meta-pattern** if they share root cause. Two NOs at the (b) level with the same structural killer is a stronger signal than three NOs at the (a) level.
- **One category-wide (c) NO with strong evidence** can be sufficient to escalate immediately. Do not require multiple convergent (c) NOs — that's wasteful.
- **(d) NOs do not contribute to meta-pattern signal until rescoped.** They are noise, not evidence.

A common failure mode: treating a candidate-specific (a) NO as if it were a sub-category (b) NO, then over-reacting by abandoning the whole sub-category prematurely. The reverse failure: treating a sub-category (b) NO as if it were a candidate-specific (a) NO, then under-reacting by continuing to test candidates in a foreclosed sub-category.

The discipline: when invoking meta-pattern recognition, count the *bracketed scopes* of prior NOs, not just the raw count. If you can't tell what scope a prior NO was at, re-read its scope statement (which the methodology requires) before drawing conclusions.
