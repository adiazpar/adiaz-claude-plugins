# re-discipline — the snaphak-re knowledge system (v2: campaign lifecycle)

A project-local plugin that keeps a long, multi-session reverse-engineering effort coherent. Claude Code is the **steward**: it runs campaigns, delegates RE to subagents, and guards what becomes truth.

## The core idea

**The directory an artifact lives in is its epistemic status** — so trust is legible from location alone:

- `active/<slug>/` — provisional, in-flight work (do not trust as fact)
- `docs/truth/` — verified, durable facts (crossed the Wall)
- `docs/history/chronicles/` — retrospective per-campaign narratives + leads (never current state)
- `archive/` — preserved irreproducible material
- `docs/backlog/` — not-yet-opened campaign briefs

This bright line is the defense against the project's central risk: stale/provisional material being mistaken for hard truth.

## The lifecycle (skills)

A **campaign** lives in `active/<slug>/` (scratch), then becomes a chronicle when closed.

- **onboard** — orient from `docs/INDEX.md` → `truth/INDEX.md` + `history/INDEX.md` + any active campaign
- **open-campaign** — scaffold `active/<slug>/` + `CAMPAIGN.md`
- **delegate** — dispatch a subagent into the campaign workspace
- **review-subagent** — triage a subagent's draft against the Wall (subagents draft, the manager ratifies)
- **promote-truth** — the ONLY door from scratch → `docs/truth/`; DIRECT evidence only
- **overturn** — rare: a campaign DIRECTLY disconfirms a prior synthesis
- **close-campaign** — promote findings → archive irreproducible artifacts → write the chronicle → delete the scratch

## The Wall

Findings cross into `docs/truth/` only on **DIRECT** evidence (observed, not inferred). Truth is tagged **atomic** (write-once bedrock) or **synthesis** (augmentable, scoped). Citations are recipes (`tools/re/run.ps1 …`), `archive/` pointers, or the chronicle — so they survive scratch deletion.

See `.claude/CLAUDE.md` for the full contract.
