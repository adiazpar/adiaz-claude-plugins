# re-discipline Conventions

This project curates hard facts about the software under analysis in
`.re-discipline/`. Any agent that can read files and run a command can
use it. The recovery path for every problem here is "edit a text file"
or "delete and rebuild" — nothing requires exact hashes or protocols.

## Layout

- `docs/` — curated truth. One atomic claim or topic per file. `docs/ops/`
  holds operational recall (workflows, tool paths, dead ends) — this is
  the project's shared memory.
- `active/<campaign-slug>/` — one campaign (investigation workspace) per
  task: `CAMPAIGN.md` (goal + status), `reports/` (full investigator
  reports, append-only), `findings/` (candidate findings awaiting
  promotion).
- `archive/<campaign-slug>/` — closed campaigns.
- `golden.jsonl` — retrieval regression questions:
  `{"q": "...", "expect": "docs/..."}` per line.
- `bin/re-search.exe`, `index.db` — search tool and its disposable index
  (both gitignored; re-run init to restore the exe).

## Searching (do this before investigating anything)

    .re-discipline/bin/re-search.exe query "your question or identifier"

Add `--json` for structured output, `--limit N` for more results. Flags
go BEFORE the question text (`query --limit 8 "..."`) — flags after the
question are silently ignored. The index rebuilds itself when stale. If
results are weak, reword, or grep `docs/` directly. When a real question
misses a doc you know exists, add it to `golden.jsonl`.

## Doc format

    ---
    status: promoted | superseded    # candidate while in active/*/findings/
    kind: fact | ops
    grade: direct | inferred | reported   # facts only
    evidence: [archive/<slug>/reports/R-001.md]
    tags: [entities, animation]
    ---
    # One-sentence claim as the title

    Details, addresses, snippets.

- `grade`: `direct` = observed in decompilation/memory; `inferred` =
  deduced; `reported` = external source.
- Titles are searchable assertions, not labels.
- Negative results are first-class docs: "X does not work, because Y."
- Conflicting observations may both be recorded with the conflict noted.

## Curation flow

1. Investigate (inline or via subagents — same rules either way).
2. The investigator writes its full report to the campaign's `reports/`
   AND distills candidate findings into `findings/` incrementally, as
   discovered — one atomic claim per file, each citing its report.
3. The manager promotes: skim findings (atomic? evidence cited? grade
   matches evidence?), search `docs/` for duplicates first, resolve
   conflicts (higher grade wins, or record both), set
   `status: promoted`, move into `docs/`, run
   `.re-discipline/bin/re-search.exe index`.
   Investigators never promote their own findings.
4. Correcting a doc later: edit it, set `status: superseded`, link the
   replacement. Git history is the provenance trail.
5. Closing a campaign: final promotion sweep, write a short summary in
   CAMPAIGN.md, move the folder to `archive/`.
