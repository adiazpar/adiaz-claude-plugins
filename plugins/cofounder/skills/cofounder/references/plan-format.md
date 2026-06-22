# The actionable plan + persistence

## The plan format (Stage 5)

The plan must be specific enough to act on Monday morning. Vague plans ("build an MVP, find customers") are where founders stall — at any experience level. Produce these sections:

### 1. Smallest sellable thing
The concrete v0 that someone would actually pay for — 2–3 sentences. Not the full vision; the smallest slice that delivers real value and can ship in ~8 weeks.

### 2. First ten customers
Name who the first ten are and *exactly* where to find them — the specific subreddit, Discord, directory, event, or list. If you can't name where the first ten come from, the direction has a reachability problem; go back to it.

### 3. The falsifiable first move
The single cheapest experiment that could prove this wrong, runnable this week. State:
- **The test** — e.g. a fake-door landing page + ~$40 of traffic, 5 cold DMs to named prospects, a pre-sale offer, or a concierge/manual delivery to one customer.
- **Cost and time.**
- **The kill result** — the specific outcome that should make the founder walk (e.g. "0 of 20 cold DMs reply with interest").

This is non-negotiable: conviction *and* a way to be proven wrong fast.

### 4. Milestones
- **2-week:** what's true if this is working (usually: the experiment ran and produced signal).
- **8-week:** the next real proof point — first paying customer, N signups, or a working v0 in real use.

### 5. Skills-gap roadmap
Given the founder's honest profile, what they need to learn or get help with to execute — and *how* (a specific course/doc, a no-code tool, a contractor, a co-founder). Sized to their actual time.

### 6. The walk-away line
The explicit condition under which they stop and reassess — a pre-committed kill criterion, written down now while it's unemotional.

## Persistence schemas

The store lives at `<root>/.claude/cofounder/`, where `<root>` is the project directory Claude Code is operating in (`${CLAUDE_PROJECT_DIR:-$(pwd)}` — never `git rev-parse --show-toplevel`, which walks up to a parent repo or home). Address files by their absolute path with `Read`/`Write` — `Write` creates parent directories as needed, and don't rely on a shell variable persisting between stages.

### `profile.json` (latest-wins — overwrite each session)
```json
{
  "skills": "8 years backend; no design; no sales experience yet",
  "time_per_week": "10 hours",
  "runway": "employed, no income pressure, exploring",
  "risk_tolerance": "moderate",
  "optimizing_for": "$3k/mo side income within 12 months",
  "interests": ["climbing", "small-business ops", "developer tooling"],
  "updated_at": "2026-06-21"
}
```

### `sessions.jsonl` (one line appended per session)
```json
{"date": "2026-06-21", "direction": "session-package add-on for climbing gyms", "brief_path": "briefs/2026-06-21-climbing-gym-packages.md", "next_move": "5 cold DMs to gym owners in r/climbing", "status": "experiment-pending", "signal": "pending", "execution_path": null}
```
`signal` is `pending` on a fresh session and becomes `green` / `yellow` / `red` after a `resume` execution review (see `execution.md`). `execution_path` stays `null` until a green signal unlocks Stage 7, then holds the `execution/<slug>/` directory.

### `execution/<direction-slug>/` (Stage 7 — written only on a green-signal resume)
The earned handoff artifacts: `operating-plan.md` (the minute-detail plan), `build-prompt.md` (the Claude Code build prompt for v1), and one `<role>.md` per active role. On the opt-in install, the same role prompts are also written as agent definitions to `<root>/.claude/agents/cofounder-<role>.md`. Full spec in `execution.md`.

### `briefs/<YYYY-MM-DD>-<slug>.md`
The full written brief: the chosen direction, the three-sentence wedge, the strategy (distribution, first-dollar path, moat), and the complete Stage 5 plan. This is the durable artifact `/cofounder resume` reads to run an execution review.
