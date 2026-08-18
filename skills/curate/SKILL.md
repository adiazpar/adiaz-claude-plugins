---
name: curate
description: >-
  The re-discipline curation conventions: how investigators (inline
  manager or subagents) write reports and distill candidate findings,
  and how managers promote them. Reference this in every investigation
  brief.
---

# Curate Knowledge

Distillation happens at the point of production, by the producer — the
agent that just finished investigating has the full context, and that
moment never comes back.

## If you are investigating (inline or as a subagent)

- Write your full report to `active/<slug>/reports/R-NNN.md` — verbose
  is fine; reports are archived, never squashed.
- **Distill candidate findings incrementally as you discover them**, not
  as an end-of-run chore. One atomic claim per file in
  `active/<slug>/findings/`, format per `.re-discipline/CONVENTIONS.md`:
  frontmatter (`status: candidate`, `kind`, `grade`, `evidence` pointing
  at your report section, `tags`), title = the claim as one sentence.
- Grade honestly: `direct` only for what you observed in
  decompilation/memory; deductions are `inferred`; external docs are
  `reported`. Do not state hypotheses as facts.
- Negative results are findings too: "X does not work, because Y."
- Do NOT put findings into `docs/` — promotion is the manager's job.

## If you are the manager promoting

Skim each candidate (atomic? evidence cited? grade matches evidence?),
search `docs/` for duplicates first, resolve conflicts (higher grade
wins, or record both), set `status: promoted`, move into `docs/`, run
`.re-discipline/bin/re-search.exe index`. You review claims — you never
re-derive reports.

## Correcting promoted docs

Edit the doc, set `status: superseded`, link the replacement doc, cite
the newer evidence. Git history is the provenance trail.
