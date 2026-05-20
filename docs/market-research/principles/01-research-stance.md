# Principle: Choose your research stance deliberately

Every research pass has a stance — exploratory or adversarial. The same question, asked from those two stances, produces different answers. A methodology that runs in one stance for every pass becomes unreliable. The discipline is choosing the stance that matches the phase of the work.

This principle was added after a real failure: the methodology was applied in pure adversarial stance across seven sequential passes, producing seven "no" verdicts. Some of those "no"s were correct. At least one (the standalone product extraction) was correct *for the wrong reason* — the products were attacked before any differentiated angle was named, so the adversarial pass found nothing to defend. The kill machine ate the work before the work was done.

## The two stances

**Exploratory stance.** "What could this be? Where's the opening?" Open-ended, generative, looking for unmet needs and angles. Agents in this stance hunt for evidence the opportunity is real — competitor review gaps, recurring forum complaints, "I wish X existed" posts, failed adjacent products that suggest the segment is real but the approach was wrong. Output is a list of candidates and angles, not a verdict.

**Adversarial stance.** "What kills this? What's the killer?" Skeptical, deliberately hostile, looking for the specific failure mode. Agents in this stance hunt for evidence the recommendation fails — hidden incumbents, false load-bearing claims, structural mismatches, comparable proxies that failed, regulatory or platform-level guillotines. Output is a verdict: survive, die, or conditional.

## The right sequence

A complete evaluation cycle uses both, in this order:

1. **Map** (exploratory) — survey the landscape, find candidates
2. **Audit** (structured) — when there's existing code, ground in what's built
3. **Differentiated angle** (exploratory + analytical) — name the specific defensible angle for the candidate. This is the most-skipped step in practice and the one that prevents most false-negative adversarial verdicts. See `04-differentiated-angle.md`.
4. **Verify angle** (mixed) — does the angle survive a sanity check on competitor reviews, willingness-to-pay, and acquisition reachability?
5. **Pressure-test** (adversarial) — having a named angle to attack, run the devil's-advocate pass. See `05-pressure-testing.md`.
6. **Decide** (synthesis) — commit, refine, or kill based on the pass output.

The methodology *as originally written* jumped from step 1 or 2 directly to step 5. It worked for killing weak proposals (because weak proposals don't have a named angle, so they fall fast). It failed for proposals with potentially real angles, because the angle was never given the chance to be named before being attacked.

## When to use each stance

| Phase of work | Stance | What you're asking |
|---|---|---|
| First survey of a space | Exploratory | "What's out there? Who has unmet need?" |
| Reading a codebase you've built | Audit (neutral) | "Who has this implicitly been built for?" |
| After a candidate is identified | Exploratory + analytical | "What's the specific angle that wins here?" |
| Before commitment | Adversarial | "What kills this specifically?" |
| After N adversarial kills | Meta-reflective | "Is the framing wrong, not the candidates?" |
| Choosing a category from scratch | Exploratory | "Where do my advantages and the market's pains overlap?" |

The adversarial stance is appropriate **only when there's a specific named angle to attack**. Running it earlier produces low-information kills.

## The exploratory failure mode

Exploratory stance has its own failure mode: open-ended optimism. If every pass is exploratory, you end up with a list of "interesting candidates" and never commit. That's a different anti-pattern — analysis paralysis through endless option-generation.

The mitigation: every exploratory pass must end with a candidate (or kill the space and move on). Every adversarial pass must end with a verdict (or escalate to meta-pattern recognition). No pass is allowed to end in "let me research more."

## The adversarial failure mode

Adversarial stance, applied too early, kills proposals before they have a defensible shape. The output looks rigorous — citations, killers identified, verdicts delivered — but the proposal never got to defend itself because it never had an angle. This is the failure that triggered writing this principle. It is the most subtle anti-pattern in this methodology because the output looks like good work.

The mitigation: before running an adversarial pass, name the differentiated angle in writing. If you can't write it in three sentences (what the product is, who it's for, why it wins against the specific alternatives), you don't have an angle yet — and the adversarial pass will produce a confident kill that's actually a failure to formulate.

## Practical heuristic

If you find yourself running back-to-back adversarial passes that each kill a candidate, **stop and run an exploratory pass** on the broader space before producing pass N+1. The adversarial pattern is a strong signal that the framing is being repeated, not that the candidates are individually weak. See `06-meta-pattern-recognition.md`.

If you find yourself running back-to-back exploratory passes without commitment, **stop and force a candidate selection** — adversarial pass next, or escalate to customer discovery. Endless exploration without commitment is its own failure.

The discipline isn't "be adversarial" or "be exploratory." It's "know which one you're being right now, and know why."

## The legitimacy of NO

This methodology was rebalanced after the original version drifted into kill-machine mode (see `07-anti-patterns.md` → Kill-machine drift). The rebalancing introduced a counter-risk: that the methodology could now generate weak angles to keep candidates alive, then pressure-test those soft angles and produce false-positive "survives" verdicts on ideas that wouldn't have survived honest evaluation.

To guard against that counter-risk: **NO is a valid output at every phase, and is the methodology's most important output.**

Specifically:

- **The differentiated-angle phase can return "No angle exists."** This is the kill point that prevents fluffy angles from advancing. If the three sentences cannot be written with specific named alternatives and a specific named gap, the candidate dies here. This is not a failure of the phase; it is the phase working.
- **The pressure-test phase can return "Dies entirely."** This is the kill point for angles that were named but cannot withstand scrutiny. Hidden incumbents, structural mismatches, comparable proxies that failed — these are killers, not concerns.
- **The meta-pattern phase can return "Accept the negative result."** This is the kill point for the entire framing. After N convergent NOs, the right move is sometimes to stop, write the postmortem, and redirect — not to run pass N+1.
- **Customer discovery can return "No willingness to pay."** Desk research cannot validate willingness; only customers can. A round of interviews that returns "would never pay for this" is the kill point that overrides any desk-research optimism.

The methodology is built so that any of these four NO outputs ends the inquiry. None of them is a failure of the research; all of them are the research succeeding at its primary job — protecting the founder from misdirected commitment.

**If a research pass produces a verdict that softens "no" into "maybe" or "with caveats" or "worth exploring further," that is a warning sign**, not a result. The methodology's anti-patterns include both kill-machine drift (false negatives) AND analysis-paralysis-through-endless-option-generation (false positives via never committing to a NO either). The right number of NOs in any session is whatever the data supports — sometimes zero, sometimes seven, sometimes all candidates considered.

The founder using this methodology should be able to take a NO without it feeling like the methodology failed them. The methodology failed them only if it produced a YES on an idea that didn't deserve one, or a NO on an idea that did.
