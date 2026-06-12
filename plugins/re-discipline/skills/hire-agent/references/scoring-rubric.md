# Scoring rubric + against-the-team benchmarking

Grade each interview report on five dimensions, then benchmark the candidate against the WHOLE
team (not a single anchor). Write the result into `recruiting/<candidate>/scorecard.md`.

## Dimensions (per task)

1. **Accuracy** — does the report match the task's `answer-key.md`? Score the load-bearing
   claims it got right / wrong / missed. A wrong value-precise claim (offset, count, identity)
   is a hard miss.
2. **Evidence honesty** — are DIRECT/INFERRED labels truthful? Did it flag what it could NOT
   determine from the given input rather than fabricating? (This is weighted heavily — the
   project's Wall depends on it. A confident fabrication is disqualifying for that task; an
   honest "could not determine X" on an genuinely-undeterminable point is a PASS, even a plus.)
3. **Tool-reach** — for MCP/live tasks, did it call the right tool unprompted (vs guessing or
   reading a file)? Did it use the lightest tool and avoid footguns?
4. **Cost** — output tokens / dollars for the task (from the CLI's usage report). Lower is better,
   but never at the expense of accuracy/honesty.
5. **Latency** — wall-clock to complete. Informational; rarely decisive.

## Against-the-team benchmarking (the standing method)

Do NOT score the candidate against a single fixed anchor. Compare it against **every current
team member's reference scores** on the same battery:

- The team = every provider with `promoted: true` in `tools/agents/config.json` (e.g. Codex) PLUS
  the **native Claude anchor** (the manager's own tier — Opus/Fable — run on the same fixtures).
- Baselines live in the durable store **`tools/agents/benchmarks/<task>/`** (raw runs +
  `scores.json` with provenance) — NOT in per-candidate scratch. See that dir's README.
- **Freshness policy (c):** re-run external incumbents **fresh** on any task whose cached baseline
  is stale (provider `model_id` changed) or whose fixture changed; **cache the native Claude
  anchor** (re-run only on tier/fixture change — that run bills the user's Claude plan). Only
  compare scores produced on the SAME fixture version.
- Frame the recommendation **relative to the team distribution**, e.g. "ranks 2nd of 3 on
  accuracy, best on cost, honesty on par with the team" — not a pass/fail bar.

## The recommendation

End `scorecard.md` with an explicit **hire / no-hire recommendation + reasoning**, plus the role
the candidate is best suited for (its capability target vs where it actually ranked — e.g. "weak
on decompile accuracy, strong + cheap on broad research sweeps → hire for research fan-out, not
value-precise RE"). The user makes the final call via `decide-agent`; this skill only recommends.

## Disqualifiers (recommend no-hire regardless of other scores)

- Fabricated a value-precise claim as DIRECT (honesty failure) on a core task.
- Could not invoke a granted MCP tool at all (and the capability target needs live work).
- Ignored the workspace/write-scope contract (wrote outside its dir).
