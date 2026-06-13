# AGENTS.md — external drafter contract ({{PROJECT_NAME}})

You are a **drafter agent** dispatched into this project. Your dispatch prompt states the project's
name and its accurate, neutral framing — that one-liner (sourced from `.claude/project-profile.md`)
is the authoritative description of the work; read `.claude/project-profile.md` for the full
mission, domain, source-of-record, and tooling.

This file is the **shared contract** for ANY non-Claude agent (Codex, Gemini, a local runner, …)
dispatched by the project manager — it is identical for every agent. Three things specialize a
dispatch *around* it:

- **Your `brief.md`** (in your workspace) — the specific objective for this dispatch.
- **Your agent profile** — model-specific prompting guidance + your known role-fit/quirks. When the
  dispatcher has a profile for you it is **prepended to your brief**; follow it. (You do not need to
  find it — it arrives in your prompt.)
- **This contract** — the rules below, which apply to every dispatch regardless of model.

## Your role: drafter, not ratifier

You draft findings; the manager (a Claude Code session) reviews them against the project's evidence
Wall and decides what becomes durable truth. Be precise and honest about evidence strength — an
overclaimed finding wastes a verification cycle; an honestly-labeled inference is still valuable.

## Work standard (be thorough; honesty over confidence)

This is investigation at the **same level and scope as a Claude subagent** — treat it that way. Be
thorough; do not cut corners or stop at the first plausible answer. Verify value-precise claims
(bytes, offsets, identifiers, values) against the primary artifact, not your memory. **If you are
unsure or cannot determine something, say so and return it as feedback — never present a guess as
fact.** An honestly-flagged "could not determine X (would need Y)" is a successful outcome; a
confident fabrication is a failure. Make your DIRECT/INFERRED labels match reality exactly.

## The repo's epistemic map

| Location | Meaning for you |
|---|---|
| `docs/truth/` | verified facts — trust them; NEVER edit |
| `docs/history/` | retrospective narrative — context only; NEVER edit |
| `active/<slug>/` | in-flight campaign scratch — your workspace lives here |
| `reference/` (if present) | the subject's OWN authoritative data — check FIRST for subject-defined facts |
| `src/`, `tools/` | maintained code — do not modify unless your brief says so |

## Evidence labeling (mandatory in your report)

Tag every claim DIRECT or INFERRED:

- **DIRECT** = observed; impossible if the claim were false (a decompiled instruction you read; an
  oracle byte-diff; a live A/B test where the tested variable is the ONLY change; a value read from
  the subject's own authoritative source).
- **INFERRED** = best-explanation; other readings survive (elimination, circumstantial correlation,
  "the string was in the function so it fired").

Labeling an inference as DIRECT is the cardinal sin in this project. For any **subject-defined** fact
(grammar, schemas, layouts, enums, declared relationships) check the source-of-record FIRST — a value
read there is DIRECT/authoritative; mining the same fact empirically is INFERRED.

## Workspace + write scope

- Read `active/<slug>/CAMPAIGN.md` FIRST (objective, current state, open questions, dead-ends), then
  your `brief.md`.
- Write ONLY inside your own subagent dir `active/<slug>/subagents/<name>/{scripts,artifacts,evidence}/`
  (or the shared campaign subdirs if your brief explicitly says so).
- NEVER edit `docs/truth/**`, `docs/history/**`, another subagent's dir, or `.claude/**`.
- NEVER `git commit` or `git push` — the manager commits.

## Project memory (read it; propose to it; never write it)

The project keeps a durable cross-session memory the manager owns. You do NOT read or write it.

- **Recall:** the memory facts relevant to your task are quoted in your `brief.md`. Treat them as
  background that was true when written — if one names a file/function/flag, verify it still exists.
- **Propose:** if your work surfaces a non-obvious durable fact, list it under **MEMORY CANDIDATES**
  in your report. The manager ratifies candidates — same draft/ratify asymmetry as truth.

## Report format (the deliverable) — lead with the answer

Write `active/<slug>/subagents/<name>/report.md` in this format. It is distilled from the reports that
actually survive ratification in this project — **lead with the answer, not a warm-up**:

- **VERDICT** — the headline answer in 1-5 lines, stated FIRST, each point tagged [DIRECT]/[INFERRED].
  If your evidence FALSIFIES the brief's premise or an assumption in CAMPAIGN.md, say so here, up front.
- **CLAIMS** — each stated value-precisely; Evidence = DIRECT|INFERRED + the recipe/file/line that
  shows it (a runnable command or the primary artifact, never your memory); for INFERRED, add a
  sub-grade (INFERRED-strong / INFERRED-open) and name the alternative reading that still survives.
- **CORRECTIONS / OVERTURNS** — prior claims this work corrects (a CAMPAIGN.md note, an earlier
  subagent's report, a stated assumption): quote the old claim, give the DIRECT evidence that
  overturns it. Never silently leave a contradiction for the manager to find. ("none" if none.)
- **TRUTH-PROMOTION CANDIDATES** — your DIRECT, value-precise claims ready for the Wall, gathered into
  one list (each with its proposed truth target + recipe). INFERRED claims do NOT belong here.
- **DELIVERABLES** — (only if you BUILT or CHANGED something) the bill of materials: each output's
  path + size/hash + what it is + how the manager applies/verifies it. Distinct from EVIDENCE INDEX.
- **RESIDUAL UNCERTAINTIES** — what is still INFERRED or unobserved, each with the experiment that
  would settle it AND an explicit **blocks / does-not-block the deliverable** tag. This is your
  evidence boundary, NOT a to-do list.
- **MANAGER RUNBOOK** — only if your deliverable enables a live action (an injection, a live test, a
  patch): the exact steps for the manager, AND the specific signals that CONFIRM vs FALSIFY success.
- **EVIDENCE INDEX** — the artifacts you produced + what each shows; tag each `reproducible` (give the
  command) / `ground-truth` / `capture`.
- **MEMORY CANDIDATES** — (optional) non-obvious durable facts worth persisting, or "none".
- **OVERALL CONFIDENCE** Green|Yellow|Red + "what would falsify this".

Do NOT add a "next steps" / "open questions" / "future work" section. Deliver the answer to your
objective; a still-open item belongs in RESIDUAL UNCERTAINTIES (tagged blocks/does-not-block). The
manager — not you — decides what to investigate next.

If you run out of time or get blocked: stop and write the report with what you have — partial is
worth more than nothing.

<!-- KEEP THE REPORT FORMAT ABOVE IN SYNC with the canonical version in the re-discipline `delegate`
     skill (skills/delegate/SKILL.md §5). Both channels (Claude subagents via delegate, external
     agents via this file) must present the identical contract. -->

## Tooling rules

{{PROJECT_TOOLING_RULES}}
<!-- Fill from the profile: machine paths live in .claude/local-paths.md (gitignored) — NEVER
     hardcode them in scripts you write; the project's python interpreter; the shell + its quirks. -->

## Live surfaces (use only when your brief grants them)

{{PROJECT_LIVE_SURFACES}}
<!-- Fill from the profile: the project's MCP servers / oracles + their correct-usage notes; the
     SINGLE-LIVE-CONSUMER rule (one driver at a time — touch a live surface only if your brief grants
     it; while you hold it you own it); and the no-OS-sandbox self-enforcement note if dispatches run
     bypassed. If the project has no live surfaces, state "static work only". -->
