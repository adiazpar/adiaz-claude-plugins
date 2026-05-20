# Market Research Methodology

A foundation for a Claude skill. Captures a research method for evaluating the commercial viability of digital products — both products you've already built and products you're considering building — calibrated for a US-based solo founder context but adaptable to other founder profiles and to any industry.

## What this is for

Use this methodology when:

- You have built something and don't know who pays for it.
- You have an idea and want to know whether it monetizes before you build.
- You want to evaluate whether an existing product should be pivoted, extracted from, or sunset.
- You need to research a market segment whose participants don't have a strong digital presence (informal economies, micro-merchants, low-tech-literacy demographics, niche specialists) — or whose participants are so loud that you risk over-weighting what you find (developers, designers, AI enthusiasts).
- You're choosing what category to build in next, and want a structured way to compare candidates instead of jumping on the first one that sounds interesting.

Do not use this for:

- Validating something a paying customer has already asked for. Talk to them, ship it.
- Pure technical research (architecture choice, library selection). That's a different kind of work.
- "Is this a good idea?" framed as a yes/no answer. The methodology produces evidence about specific markets, not blanket judgments about ideas.

## The core idea

A complete research cycle has six phases. They are not interchangeable. Running them out of order — particularly running adversarial passes before a differentiated angle has been named — produces low-information verdicts that look rigorous but kill ideas for the wrong reasons.

1. **Map** (exploratory) — survey the landscape. Find candidates.
2. **Audit** (structured) — when there's existing code, reverse-engineer the implicit ICP from the schema and route handlers. Without this, you'll research a market that doesn't match what you've built.
3. **Differentiated angle** (exploratory + analytical) — name the specific defensible angle for the candidate. Three sentences: what it is, who it's for, why it wins against the named alternatives. This is the step most commonly skipped and the one whose absence causes the most false-negative verdicts downstream.
4. **Verify angle** (mixed) — sanity-check the angle's claims against competitor reviews, willingness-to-pay signals, and acquisition reachability.
5. **Pressure-test** (adversarial) — having a named angle to attack, run the devil's-advocate pass. This is where you find the killer.
6. **Decide** (synthesis) — commit, refine, or kill based on the pass output. If you've now had multiple convergent "no" verdicts, run meta-pattern recognition rather than another candidate.

Each phase has a corresponding agent-prompt template in `agent-prompts/`. Each phase has at least one principle in `principles/` describing the load-bearing ideas.

## How to use this foundation

The directory is organized into four parts:

- **`principles/`** — the load-bearing ideas. Read these before running any research pass. They are the why. Includes principle 08 on tool selection, which closes the gap between "tool installed" and "tool used."
- **`agent-prompts/`** — reusable prompt templates for dispatching specific research tasks. Each prompt now references the tool-selection principle so the agent reaches for specialized tools (Firecrawl, Exa, etc.) when they're available rather than defaulting to general-purpose alternatives.
- **`industry-guides/`** — industry-specific calibration. Use these as a lens over the principles. Currently most-developed for retail/SMB; stubs exist for developer tools, consumer apps, infrastructure SaaS, AI tools, and services.
- **`tooling/`** — the specific tools (MCP servers, scrapers, APIs) that materially upgrade research output in 2026. Installation makes them available; the tool-selection principle ensures they're actually used.

The principles are the part to internalize. The prompts are the part to copy and run. The industry guides are the part to read first when starting on a new vertical. The tooling is the part to install once.

## A realistic research arc

A complete evaluation typically runs 4–6 agent passes across one to two days:

1. **Codebase ICP audit** (if existing code). Output: who you've implicitly built for. (`agent-prompts/01-codebase-icp-audit.md`)
2. **ICP profitability evaluation.** Output: can this implicit ICP be monetized at SaaS economics? (`agent-prompts/02-icp-profitability.md`)
3. **Adjacent market scan** (if pass 2 returns negative). Output: what other markets the capabilities could serve. (`agent-prompts/03-adjacent-market-scan.md`)
4. **Differentiated angle identification.** Output: a specific named angle worth attacking. (`agent-prompts/04-differentiated-angle.md`)
5. **Devil's advocate pressure test.** Output: verdict on the angle. (`agent-prompts/05-devils-advocate.md`)
6. **Extraction evaluation** (if no whole-product ICP works). Output: standalone product opportunities pullable from the codebase. (`agent-prompts/06-extraction-evaluation.md`)

Most projects will not run all six. A confident founder with a clear segment may only need 4 and 5. A blocked founder with a built product and no buyers usually needs 1, 2, 4, 5. A founder who has converged on "no path forward" needs 6 to evaluate cleanup options.

## What this methodology will not do

- It will not tell you that your idea is good.
- It will not produce confident TAM numbers. The data doesn't exist for most informal markets, and the data that does exist for loud markets is biased.
- It will not replace customer interviews. It tells you which customers are worth interviewing, not what they will say.
- It will not find a market that doesn't exist. If multiple honest passes return "no," the answer is no.
- It will not protect you from over-running. If you run pass after pass without ever committing or stopping, no methodology can save that.

## What this methodology has learned

A short list of the most expensive lessons captured in `principles/`:

1. **Research stance matters.** Adversarial passes are protective discipline; running them at every step turns the methodology into a kill machine. See `01-research-stance.md`.
2. **The differentiated angle step is non-optional.** Most "no" verdicts in early use of this methodology were verdicts on feature descriptions, not on real angles. See `04-differentiated-angle.md`.
3. **Single-proxy extrapolation overstates signal strength.** Triangulate. See `02-proxy-data-research.md` and `07-anti-patterns.md` → Single-proxy extrapolation.
4. **Competition is not foreclosure.** Every successful product had competitors at launch. The question is which competitor, on which axis, would beat your specific angle. See `07-anti-patterns.md` → Competition-existence-as-kill.
5. **Founder-market fit beats market existence.** A profitable market you can't reach is not your market. See `07-anti-patterns.md` → Founder-market fit blindness.

## A note on tone

The methodology assumes the researcher wants the truth more than the comfortable answer. If you are not in that mood, save this for later. Half-running it produces narrative confirmation, which is worse than not running it.

It also assumes you can take a hard answer. Multiple no-verdicts in a row hurt; they're also frequently correct. The signal that the methodology is working is that bad ideas die fast and the good ones get sharper, not that every idea survives.

## NO is the methodology's most important output

This is the load-bearing commitment. The methodology was rebalanced once after drifting into kill-machine mode — and the rebalancing introduced a counter-risk of false-positive "survives" verdicts on ideas that don't deserve them. To guard against both failure modes:

**NO is a valid and frequent output at every phase.** It is not a failure mode. It is the methodology's primary value-add.

Specifically, NO can and should be the verdict at:

- **Codebase ICP audit** — "the code implies an ICP that the founder cannot reach"
- **ICP profitability** — "this segment does not monetize at SaaS economics for this founder profile"
- **Adjacent market scan** — "no adjacent market improves on the implicit ICP enough to justify the rework"
- **Differentiated angle identification** — "no defensible three-sentence angle can be honestly written for this candidate"
- **Pressure-test** — "the named angle does not survive scrutiny; the killer is X"
- **Extraction evaluation** — "no standalone product can be honestly pulled out of this codebase"
- **Meta-pattern recognition** — "N convergent NOs mean the framing is wrong; stop, write the postmortem, redirect"

If any research pass produces a verdict that softens "no" into "maybe" or "worth exploring further" without specific evidence justifying the softening, treat that as a warning sign. The methodology is working when bad ideas die clearly and good ones survive sharply. It is NOT working when every idea returns "promising but more research needed" — that pattern indicates either kill-machine drift (false negatives manufactured to look like rigor) or fluffy-angle drift (false positives manufactured to look like discoveries).

The founder using this methodology should be able to take a NO without it feeling like the methodology failed them. The methodology fails the founder only if it produces a YES on an idea that didn't deserve one, or a NO on an idea that did.

See `principles/01-research-stance.md` → "The legitimacy of NO" for the full elaboration.
