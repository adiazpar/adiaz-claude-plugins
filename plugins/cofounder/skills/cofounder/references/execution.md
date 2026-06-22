# Execution: the earned phase (Stage 7)

This phase is the payoff, and it is **earned, not given.** It runs only when a `/cofounder resume` execution review shows the falsifiable first move came back **green** — the market said yes to something. Until then this file does not apply: planning a startup to the minute before a single test has passed is the exact over-build trap the rest of the session exists to prevent. The discipline that makes the falsifiable first move non-negotiable is the same discipline that gates this.

## Gate: classify the signal first

A `resume` opens with the execution review ("how did the experiment go?"). Classify the result honestly, with the founder, against the kill criterion they pre-committed to in the original brief:

- **Green** — the experiment produced real positive signal: replies with buying intent, signups, a pre-sale, a first dollar. Demand is no longer a guess. → run this phase.
- **Yellow** — ambiguous: some interest, nothing conclusive. → don't scale. Iterate the experiment — sharpen the offer, the audience, or the channel — and re-test. Name out loud the specific result that would turn it green.
- **Red** — it hit the walk-away line. → don't soften it. Return to Stage 2 and re-orient, reusing the loaded profile. A red result honored cheaply is the tool working, not failing.

Only green unlocks what follows.

## The operating plan (minute-detail)

Now the detail is earned. Build it *with* the founder, calibrated to their per-domain level (see `facilitation.md`). Cover:

- **Distribution** — the specific channels, the posting/outreach cadence, and a concrete first-month content or outbound calendar. Named surfaces and a schedule, never "social media."
- **Marketing & messaging** — the positioning line, the three load-bearing proof points, and the objection-handlers for each named alternative.
- **Pricing & first-dollar ops** — the price, the packaging, how money is actually collected, and what happens operationally the moment someone pays.
- **Metrics** — the two or three numbers that tell you it's working, and exactly where each is read.
- **30/60/90 operating milestones** — what is true at each mark if this is on track, and the check that catches it drifting off.

## The team: catalog, then instantiate to the stage

A startup has many functions. A just-validated solo founder does not need all of them staffed on day one. So **select**, don't dump — a full org chart of subagents for a one-person experiment is the over-build trap relocated to the team layer.

The catalog of functions to draw from:

> product/build · product management · design/UX · growth & acquisition · content/SEO · sales/outbound/BD · customer success/support · pricing & unit economics · finance · operations/fulfillment · data/analytics · legal/compliance · community · partnerships · hiring · fundraising/investor relations

Run a **role-selection step** out loud:

1. From the strategy (wedge, distribution, first-dollar path, moat), the founder's profile, and the validated stage, pick the **active** roles — the ones with real work in the next 90 days. Name them and say *why each is in or out*.
2. Weight by the strategy: a content-led play leans content/SEO; a B2B play leans outbound/BD; a regulated space pulls legal/compliance in early.
3. List the **dormant** roles with the explicit trigger that wakes each — *"add Support past ~20 paying customers," "add Fundraising only on a committed venture-scale swing."* Nothing is lost; it's deferred until it has real work.

## Handoff artifacts

For each **active** role, write a self-contained prompt — detailed enough to hand to a Claude Code subagent *or* a human professional. Calibrate each to the founder: tighter where they're strong, more scaffolded where they have a gap.

Write the portable artifacts to:

```
<root>/.claude/cofounder/execution/<direction-slug>/
├── operating-plan.md      # the minute-detail plan above
├── build-prompt.md        # the Claude Code build prompt for v1
├── <role>.md              # one per active role (e.g. growth.md, outbound.md, content.md)
└── …
```

Each role prompt states: the role's single objective this quarter, the context it needs (pulled from the brief, not re-derived), the concrete first deliverables, the constraints (budget, the founder's time, the brand voice), and how its success is measured. `build-prompt.md` is specific enough that Claude Code could start building v1 from it cold — the stack, the v0 scope carried from the original plan, and the first files to create.

## Opt-in: install the team as live agents

The portable prompts above are **always** written. Installing them as runnable Claude Code agents is a separate, explicit yes/no — use `AskUserQuestion`. Default the founder's **gap roles** to "recommended to install": those are the roles a standing agent earns its keep on, where the founder can't easily do the work themselves. The roles they're already strong in, they can just run.

On **yes**, write each chosen role to `<root>/.claude/agents/cofounder-<role>.md` as a valid agent definition — YAML frontmatter with `name`, `description`, and `tools`, and the role prompt as the body. Namespace every filename `cofounder-` so nothing of the founder's existing agents is clobbered. On **no**, the portable prompts stand on their own — say where they are and stop.

## Persist the transition

Append the resume's outcome to `sessions.jsonl` with `signal` set to `green`/`yellow`/`red`, and on green record the `execution_path`. Update the brief with the operating plan. Schemas in `plan-format.md`.
