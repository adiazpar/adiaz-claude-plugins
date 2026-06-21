---
name: market-research
description: Use when evaluating commercial viability of a digital product — either brownfield (built but don't know who pays) or greenfield (starting from nothing and looking for a problem worth building for via demand discovery). Six-phase methodology plus a Phase-0 demand-discovery entry point. Disciplined kill criteria, named-angle requirement, adversarial pressure-testing, NO as a valid output at every phase. Persists candidate state as JSONL under the project's .claude/market-research/ directory. Runs entirely on Claude Code's built-in tools — no external services or credentials.
---

# Market Research Methodology

A research method for evaluating the commercial viability of digital products — both products already built and products being considered. Calibrated for a US-based solo founder context but adaptable to other founder profiles and to any industry.

This skill is the central methodology reference. The plugin's slash commands (`/research-icp-audit`, `/research-profitability`, `/research-adjacent-scan`, `/research-angle`, `/research-pressure-test`, `/research-extraction`) and their corresponding subagents all consult this skill at runtime.

## 1. When to use / when not to use

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

## 2. The six phases

A complete research cycle has six phases. They are not interchangeable. Running them out of order — particularly running adversarial passes before a differentiated angle has been named — produces low-information verdicts that look rigorous but kill ideas for the wrong reasons.

1. **Map** (exploratory) — survey the landscape. Find candidates.
2. **Audit** (structured) — when there's existing code, reverse-engineer the implicit ICP from the schema and route handlers. Without this, you'll research a market that doesn't match what you've built.
3. **Differentiated angle** (exploratory + analytical) — name the specific defensible angle for the candidate. Three sentences: what it is, who it's for, why it wins against the named alternatives. This is the step most commonly skipped and the one whose absence causes the most false-negative verdicts downstream.
4. **Verify angle** (mixed) — sanity-check the angle's claims against competitor reviews, willingness-to-pay signals, and acquisition reachability.
5. **Pressure-test** (adversarial) — having a named angle to attack, run the devil's-advocate pass. This is where you find the killer.
6. **Decide** (synthesis) — commit, refine, or kill based on the pass output. If you've now had multiple convergent "no" verdicts, run meta-pattern recognition rather than another candidate.

> Note: Phase 1 (Map) is superseded by Phase 0 (Demand Discovery) for the greenfield case. See the Phase-to-command map below for the routing.

Phase 2 has three sub-passes invoked sequentially as needed: 2 (codebase audit), 2b (profitability evaluation), 2c (adjacent market scan). The phase-to-command map below shows when each fires.

Phase order is load-bearing. Adversarial passes before an angle is named produce kills of poorly-formed proposals — the output looks rigorous, but the proposal never got to defend itself because it never had an angle. This is the most subtle anti-pattern in this methodology because the output looks like good work.

### The three-sentence angle rule

A **differentiated angle** is a three-sentence claim that survives independent scrutiny:

1. **What it specifically is** (capability + form factor)
2. **Who it's specifically for** (named segment with a verifiable pain)
3. **Why it specifically wins against the named alternatives** (the defensible delta)

Without all three sentences written out, there is nothing to pressure-test. The adversarial pass will produce a confident kill that's actually a kill of a poorly-formed proposal.

The third sentence — the "why it wins" — is the load-bearing part. It must name (a) the specific alternatives the buyer is currently using and (b) the specific reason this beats those alternatives. "Better UX" is not a reason. "Faster" is not a reason unless you can quantify against a named alternative. "AI-powered" is not a reason. A reason is something you can verify in customer reviews or product comparison: the specific gap one alternative leaves open and you fill.

## 3. The legitimacy of NO

NO is the methodology's most important output. It is not a failure mode. It is the methodology's primary value-add.

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

## 4. Phase-to-command map

| Phase | Slash command | Subagent | When to invoke |
|---|---|---|---|
| 0. Demand Discovery (from-nothing entry point) | `/research-demand-discovery` | `demand-discovery-agent` | Greenfield: no codebase, no committed candidate, surfacing problems from external evidence |
| 1. Map | (superseded by Phase 0 for greenfield — describe in conversation for brownfield landscape surveys) | — | Surveying candidates from scratch (Phase 0 is the structured replacement) |
| 2. Audit | `/research-icp-audit` | `icp-audit-agent` | Existing codebase, founder unsure who pays |
| 2b. ICP profitability | `/research-profitability` | `profitability-agent` | After audit, evaluating if implicit ICP monetizes |
| 2c. Adjacent scan | `/research-adjacent-scan` | `adjacent-scan-agent` | If audit returns "ICP can't be monetized at SaaS economics" |
| 3. Differentiated angle | `/research-angle` | `angle-agent` | Naming the three-sentence defensible angle |
| 4. Verify | (no v1 command — inline WebSearch/WebFetch) | — | Sanity-checking the named angle |
| 5. Pressure-test | `/research-pressure-test` | `pressure-test-agent` | Adversarial pass on a named angle |
| 6. Extraction (alt path) | `/research-extraction` | `extraction-agent` | When no whole-product ICP works, evaluating standalone product pulls |

Phase 0 is the from-nothing entry point. Greenfield workflow: Phase 0 → 3 → 5 → Decide. Brownfield workflow (existing codebase): unchanged, Phase 2 → 2b → 3 → 5, with Phase 0 as optional cross-check.

## 5. Research tools

This methodology runs entirely on Claude Code's built-in tools — no external services, credentials, or credits. Two web tools cover the desk research, plus the code-reading tools for the codebase phases:

- **WebSearch** — discovery and time-sensitive queries (use the current year): complaint / willingness-to-pay phrasings, firmographic and segment-size data, trend triangulation, missed-competitor and disconfirming-evidence searches.
- **WebFetch** — a known URL: competitor reviews (G2, Capterra, Trustpilot, app stores), incumbent pricing pages, sold-business listings, trade publications, and free Reddit via `reddit.com/r/<sub>/top.json?limit=N`.
- **Read / Grep / Glob / Bash** — reading the codebase in the Phase 2 audit and the Phase 6 extraction passes (the source code is the only input there).

Each agent's `## Tools` section names the tools its phase leans on. Two habits carry across every phase:

1. **Cite a source URL for every claim.** The URL is the evidence trail that makes downstream meta-analysis (was the research thorough?) possible.
2. **Report what you couldn't get.** If a source was unreachable or the evidence for a claim is thin, say so in the output — that signals to the reviewer where output quality was bounded.

### Workflow-state persistence

State is persisted as project-local JSONL files, not in any external service. The plugin writes to three files under `<project_root>/.claude/market-research/` (path resolved via `git rev-parse --show-toplevel`, falling back to `pwd`):

| File | Purpose | Write pattern |
|---|---|---|
| `candidates.jsonl` | One line per candidate (product / category / segment being evaluated). Updated as candidates move through phases. | Append a new line whenever status changes; latest line per `id` wins on read. |
| `phase-outputs.jsonl` | One line per (candidate, phase) result. Verdict + evidence summary + path to the full agent report. | Append-only. |
| `pain-signals.jsonl` | One line per mined pain quote. Severity 1-3, linked to a candidate by `candidate_id`. | Append-only. |

Resolve paths in any command or agent with:

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
MR_DIR="$PROJECT_ROOT/.claude/market-research"
mkdir -p "$MR_DIR"
```

Query active candidates (latest-line-wins, excluding kills):

```bash
jq -s 'group_by(.id) | map(max_by(.updated_at)) | map(select(.verdict != "kill"))' "$MR_DIR/candidates.jsonl"
```

This persistence layer requires no external service, no API keys, and no setup — the directory is created on first write. See the plugin's README for the full per-file schema and example records.

## 6. What to skip

- **General "AI research assistant" SaaS.** You already have Claude Code with WebSearch and WebFetch. Most consumer "AI research" tools wrap the same primitives at higher cost.
- **AI-native "validate your startup idea" tools.** ~20 launched 2024-2026 (ValidatorAI, IdeaProof, WorthBuild, ValidateMySaaS, ProductGapHunt, etc.). All GPT wrappers with no proprietary data — they produce TAM / SAM / SOM-style reports from a one-paragraph prompt. Claude Code's built-in web research (WebSearch + WebFetch) gathers actual primary data and is strictly stronger. Treat the entire category as zombie-from-birth. If a tool's pitch is "we'll validate your idea with AI," its evidence has either been validated by someone else (and is now publicly available, so why pay) or made up (in which case the tool is worse than no tool because it manufactures false confidence). See the stance discipline rule below — false-positive validation is a worse failure mode than no validation.
- **LinkedIn data scrapers.** ToS violations + LinkedIn enforcement = high risk. Use sparingly and for direct outreach only, not for systematic data collection.
- **Twitter/X API.** Pricing changes in 2023+ made it impractical for indie research. Use WebSearch / direct WebFetch for X content.
- **Survey tools (Typeform, Google Forms).** Useful for primary research, not for market research desk work. Add them when you're past desk research and into customer discovery.
- **Vertical scrapers for verticals you're not researching.** EverBee (Etsy), Helium 10 / Keepa (Amazon), Marmalead (Etsy), PPSPY (Shopify) — all legitimate tools for their vertical, all noise otherwise. Reach for one only if a candidate actually lives in that vertical; otherwise the built-in web tools cover the desk research.

## 7. Failure modes summary

The top three anti-patterns to watch for during any research pass:

**Kill-machine drift.** Running every research pass in adversarial stance, regardless of phase of work. The methodology produces back-to-back kills, each one feels rigorous, but the cumulative effect is that no product ever gets the chance to defend itself because no product ever had its angle named. The output looks like good research; the conclusion is paralysis. Warning sign: three or more sequential adversarial passes that each produce "no" verdicts on candidates that were never given a named differentiated angle. The corrective: pause and run an exploratory pass. Before any adversarial pass, the differentiated angle must be named in writing. An adversarial pass against a poorly-formulated proposal produces a low-information kill; an adversarial pass against a well-formulated angle produces a useful verdict.

**Fluffy-angle drift.** The opposite failure mode introduced when rebalancing away from kill-machine drift — generating weak angles to keep candidates alive, then pressure-testing those soft angles and producing false-positive "survives" verdicts on ideas that wouldn't have survived honest evaluation. Warning sign: research passes that soften "no" into "maybe" or "worth exploring further" without specific evidence justifying the softening; angles whose third sentence ("why it wins against the named alternatives") cannot be written with a specific named alternative and a specific named gap. The corrective: the differentiated-angle phase can and should return "No angle exists." That is the kill point that prevents fluffy angles from advancing.

**Single-proxy extrapolation.** Reading one failed comparable (e.g., a competitor with 0 reviews) as dispositive proof that the entire category is unviable. One product's failure could be due to UX, marketing, positioning, timing, pricing, or app-store SEO — not necessarily category failure. Treating one proxy as dispositive overestimates signal strength. Warning sign: a verdict that hinges on a single competitor's poor performance metric, without triangulating against at least two other independent proxies. The corrective: require triangulation. A verdict that one product failed should be reinforced by independent proxies (forum demand signals, review patterns, switching-cost data, regulatory shifts) before being treated as a category-level conclusion. One signal is suggestive; three convergent signals is dispositive.

Two additional anti-patterns are worth naming because subagents reference them:

**Competition-as-foreclosure.** Treating "competitors exist in this category" as a kill signal. Every successful product had competitors at launch — Stripe shipped against PayPal, Notion against Evernote, Linear against Jira — so incumbents are a sign that revenue is in the category, not that the slot is taken. The corrective: distinguish competition from foreclosure. Foreclosure means a specific competitor with a specific moat would beat your specific angle on a specific axis; demand that evidence rather than treating presence of competitors as dispositive.

**Founder-market-fit blindness.** Treating "market exists" as sufficient evidence that you should serve it — but a profitable market that requires Spanish fluency, in-person visits to Peru, and BD relationships with regional distributors is not your market if you are a US-based solo founder without those assets. The corrective: for every candidate market, require a three-sentence acquisition story — where you find prospects, how you contact them, and at what cost. If you can't write those three sentences, you don't have access to the market, however attractive it looks.

## 8. Stance discipline rule (no adversarial pass without a named angle)

Every research pass has a stance — exploratory or adversarial. The same question, asked from those two stances, produces different answers. A methodology that runs in one stance for every pass becomes unreliable. The discipline is choosing the stance that matches the phase of the work.

The adversarial stance is appropriate **only when there's a specific named angle to attack**. Running it earlier produces low-information kills.

> *Before running an adversarial pass (e.g., `/research-pressure-test`), confirm a differentiated angle has been written in three sentences. If no angle exists, run `/research-angle` first. Adversarial passes against unnamed angles produce false-negative kills.*
