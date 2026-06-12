# Campaign: <slug>

**Status:** OPEN — <one line: what's being investigated right now>
**Opened:** <YYYY-MM-DD>
**Owner:** Claude Code (manager) + delegated subagents

> This is **provisional, in-flight** work (`active/`). Nothing here is truth until it crosses the DIRECT-evidence Wall into `docs/truth/`. A fresh agent or subagent reads this file first to orient.

## Objective

<What question does this campaign answer? What does "solved" look like (the closure bar)?>

## Current state

<Where we are now. Rewrite at the end of every session / on PreCompact (the `checkpoint-campaign` skill) so the next agent picks up cold. **Hard cap: ~30 lines, newest first.** Must answer: what just happened, what is proven (pointers), the next move, what's blocked on what. Older material moves to `## Historical log` at the bottom or into `evidence/` — "Current state" is for resuming, history is for the chronicle.>

## Dir map

- `scripts/` — one-off python (reproducible → deleted at close)
- `ghidra/` — one-off Ghidra scripts (reproducible → deleted; reusable → promote to `tools/re/`)
- `decomps/` — decompile / trace logs (reproducible → deleted; the RECIPE goes in the truth)
- `artifacts/` — data: test rawmaps, generated maps, captured crash/freeze/watch JSON, ground-truth
- `evidence/` — `.md` reasoning notes (→ folded into the chronicle at close)
- `subagents/<name>/` — per-subagent scratch (`scripts/ artifacts/ evidence/ report.md`)

## Open questions

- <the unknowns gating closure>

## Dead ends so far (don't retry within this campaign)

- <hypothesis> — <why it was ruled out> <DIRECT|INFERRED>

## Leads

- <threads worth pulling; pointers to truth/chronicles/backlog that seeded this>

## Disposition manifest

As artifacts are produced, tag each so **close-campaign** is mechanical:

| artifact | tag | destination at close |
|---|---|---|
| <e.g. engine_ref.json> | ground-truth | `archive/ground-truth-maps/` |
| <e.g. freeze_snapshot.json> | capture | `archive/evidence/` (if truth-cited) else delete |
| <e.g. decomp_x.log> | reproducible | delete; recipe → truth |
| <e.g. event_tracer.py> | keeper-tool | promote to `tools/` (generalized) |

## Historical log

<Optional. Superseded "Current state" sections move here verbatim at each checkpoint (newest first). Feeds the chronicle at close; never read for current state.>
