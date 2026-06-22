# cofounder

> A single-session founding meeting between two co-founders — you and Claude.

`/cofounder` runs one continuous working session that takes you from *"I want to build something profitable but I don't know where to start"* to a chosen direction and an actionable plan with a falsifiable first move. You bring an honest account of who you are; Claude brings live research, the lenses that decide whether a founder wins, and the willingness to disagree with you once and then commit.

It calibrates to **your** experience, per domain — it reads where you're strong and where you're not (you might ship code fluently and have never run an outbound campaign), talks to you as a peer on the former and teaches plainly on the latter, and never assumes you're a beginner across the board. It works for a first-time founder and a seasoned operator alike.

It is **not an idea oracle and not a judge.** It's a thinking partner whose job is to *educate you enough to choose*, help you build a real strategy, and point you at the cheapest experiment that could prove you wrong.

## What it does

A `/cofounder` session moves through six stages in one sitting:

1. **Open** — establish who you honestly are: skills (per domain — build, sell, distribute, operate), time, runway, risk tolerance, what you're optimizing for, and what pulls you. For calibration, not for filtering. Claude states its read of your level out loud and lets you correct it.
2. **Orient** — Claude teaches the four lenses that decide whether a founder wins, then surveys candidate directions with *live web evidence*, not vibes.
3. **Converge** — an honest shortlist (each option with its case *and* its strongest objection), Claude's recommendation and reasoning at the bottom, your choice, one honest challenge, then **disagree-and-commit**.
4. **Strategy** — the wedge, the distribution you can actually reach, the first-dollar path, and the moat over time.
5. **Plan** — a Monday-morning-specific plan: smallest sellable thing, first ten customers, the cheapest falsifiable experiment, 2-week and 8-week milestones, a skills-gap roadmap, and an explicit walk-away line.
6. **Close** — the one move you make this week, and the full brief saved.

## Beyond the first session: the earned execution phase

The first session deliberately *stops* at the cheapest test — planning a startup to the minute before the market has said yes is the single most common way founders waste months, and the falsifiable first move exists to prevent it.

So the detailed execution layer is **earned, not given.** When you `/cofounder resume` and the experiment came back **green** (real signal — replies with intent, signups, a pre-sale, a first dollar), the session unlocks a final phase that builds:

- a **minute-detail operating plan** — distribution channels and cadence, messaging, pricing and first-dollar ops, the metrics to watch, 30/60/90 milestones;
- **handoff prompt files** — one per role the strategy actually needs *at this stage* (not a generic org chart), each self-contained enough to hand to a Claude Code subagent **or** a human professional, plus a `build-prompt.md` Claude Code can build v1 from cold;
- on an explicit yes, those role prompts **installed as runnable agents** under `.claude/agents/cofounder-<role>.md`.

A yellow result means iterate the experiment; a red result means re-orient. Only green earns the build-out.

## Usage

```
/cofounder                      # start a fresh founding session
/cofounder "AI dev tools"       # start biased toward a domain you're curious about
/cofounder resume               # pick up from your saved brief (execution review)
```

The *meeting* is one sitting, but the artifacts persist (below), so the natural next session opens as "here's how the experiment went."

## Persistence (project-local JSONL)

State lives under `<your-project>/.claude/cofounder/`, where `<your-project>` is the directory you launched Claude Code in (`${CLAUDE_PROJECT_DIR:-$(pwd)}`) — pinned to where you are, never resolved upward to a parent repo:

```
<your-project>/.claude/cofounder/
├── profile.json        # your honest self-portrait (latest wins)
├── sessions.jsonl      # one line per session: chosen direction, brief path, next move, signal
├── briefs/             # the full written brief from each session
└── execution/          # earned handoff artifacts (only after a green-signal resume)
```

On the opt-in agent install, runnable agents are also written to `<your-project>/.claude/agents/cofounder-<role>.md`.

No external service, no API keys, no setup. The directory is created on first write, and state travels with the repo — different projects keep different founding ledgers.

## What it will not do

- It will not hand you a guaranteed profitable idea. No tool can.
- It will not flatter you. A real co-founder disagrees when it sees a wall — *once* — then commits to your call.
- It will not replace customers. It gets you to the cheapest test; the market gives the verdict.
- It will not pretend research is conviction. The session ends with a **falsifiable first move**, not just a plan.

## Tools

Runs entirely on Claude Code's built-in tools — no external services, credentials, or credits:

- **`WebSearch` / `WebFetch`** — the live evidence the education and strategy stand on.
- **`AskUserQuestion`** — structured intake and the converge-step choice.
- **File I/O** — the project-local brief + profile.

## Install

```
/plugin marketplace add adiazpar/adiaz-claude-plugins
/plugin install cofounder@adiaz-claude-plugins
```

From a local clone, swap the first line for `/plugin marketplace add /path/to/adiaz-claude-plugins`.

## License

MIT.

## Author

Alex Diaz (<alexdiaz0923@gmail.com>)
