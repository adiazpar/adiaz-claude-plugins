---
name: delegate
description: This skill should be used when invoked as /delegate <slug> <name> "<objective>" or when asked to "delegate to a subagent", "dispatch a subagent for <investigation>", "spawn a research subagent", "send a Ghidra subagent into the campaign". Dispatches a subagent INTO a campaign workspace (active/<slug>/) with the project's protocol enforced: it reads CAMPAIGN.md first, writes outputs to the subdirs, and its report to subagents/<name>/report.md. Subagents draft; the manager ratifies via review-subagent.
argument-hint: <slug> <subagent-name> "<objective>"
allowed-tools: Read, Write, Bash, Agent, AskUserQuestion
---

# Delegate — dispatch a subagent into a campaign workspace

**Subagents draft, the manager ratifies.** This is the project-wide asymmetry. A subagent does the RE legwork inside a campaign's workspace; you (manager) review its report against the Wall before anything is promoted. Never dispatch raw `Agent()` calls for RE work — they bypass this protocol and produce inconsistent, un-triageable reports.

## Procedure

### Step 1: Validate inputs

- `<slug>` is an existing campaign dir under `active/`.
- `<subagent-name>` is kebab-case (3-30 chars) and not already `active/<slug>/subagents/<name>/`.
- `<objective>` is concrete (≥50 chars); a vague objective produces a vague report.

If invalid, ask the user to correct.

### Step 2: Create the subagent's self-contained scratch

```powershell
$slug="<slug>"; $name="<name>"
"scripts","artifacts","evidence" |
  ForEach-Object { New-Item -ItemType Directory -Force -Path "active/$slug/subagents/$name/$_" | Out-Null }
```

### Step 3: Gather context pointers (manager-selected; ask only when genuinely uncertain)

You are the manager and you just read `CAMPAIGN.md` and the truth INDEX — select these yourself:

1. Truth files the subagent should read first → relevant `docs/truth/` paths (from the campaign's subsystems + the INDEX).
2. Chronicles whose dead-ends it must NOT retry → relevant `docs/history/chronicles/` paths (the chronicles hold the refuted hypotheses; CAMPAIGN.md's "Dead ends so far" covers the in-flight ones).
3. Relevant **memory** facts → scan `MEMORY.md` and quote the few entries that bear on this task verbatim into the brief (the subagent — Claude or external — has no access to the memory store; the brief IS its recall channel).
4. Time budget (default 30-90 min).

Use AskUserQuestion only when a choice is genuinely the user's to make (e.g. two plausible scopes with different costs) — not as a ritual. Three serial questions per dispatch is friction that discourages delegation.

### Step 4: Build the dispatch brief

The brief establishes the workspace (there is no literal `cwd` param — the prompt does it). Compose:

```
You are a subagent for the <PROJECT_NAME> project: <lead with the project's accurate, neutral framing — the `framing` one-liner from `.claude/project-profile.md` (CLAUDE.md §10)>. Read this fully before starting.

## 1. YOUR WORKSPACE
Your root is the campaign workspace `active/<slug>/`. **Read `active/<slug>/CAMPAIGN.md` FIRST** — it holds the objective, current state, open questions, and dead-ends-so-far. Then read:
- `docs/INDEX.md` (project map) and `docs/truth/INDEX.md` (what's known now)
- Relevant truth files: <list from Step 3>
- Dead-ends NOT to retry (from these chronicles): <list from Step 3>
- Relevant memory facts (your only recall channel — you cannot read the memory store): <verbatim entries from Step 3, or "none">

## 2. THE EPISTEMIC RULE (how to label your evidence)
Tag every piece of evidence DIRECT or INFERRED:
- DIRECT = observed; impossible if the claim were false (a decompiled instruction you can read; an oracle byte-diff; a live A/B test where the variable under test is the ONLY change; a value read from the game's own decl/schema in reference/).
- INFERRED = best-explanation; other readings survive (a thread passed through a function containing string X, so X "fired"; behavior changed when I changed Y among other things; elimination "it's not Z so it's W").
Only DIRECT findings can ever become truth. Be honest — labeling an inference as DIRECT is the cardinal sin here. For any GAME-DEFINED fact (wiring grammar, schemas, ref counts, layouts, enums), check `reference/` FIRST — a value read there is DIRECT/authoritative; a corpus-mined pattern is INFERRED.

## 2b. WORK STANDARD (be thorough; honesty over confidence)
Be thorough; do not cut corners or stop at the first plausible answer. Verify value-precise claims against the primary artifact, not memory. If you are unsure or cannot determine something, SAY SO and return it as feedback — never present a guess as fact. An honestly-flagged "could not determine X (would need Y)" is a success; a confident fabrication is a failure that wastes a verification cycle.

## 3. SCOPE — where to write
- Write outputs into the campaign subdirs: `active/<slug>/scripts/`, `decomps/`, `artifacts/`, `evidence/` (or your own `subagents/<name>/{scripts,artifacts,evidence}/` for self-contained work).
- Tag each artifact you produce as `reproducible` / `ground-truth` / `capture` (close-campaign uses this).
- You MUST NOT edit `docs/truth/**`, `docs/history/**`, or another subagent's dir. You do not promote anything — you draft.
- Run experiments via daemon / Ghidra (`tools/re/run.ps1`) / oracle as needed.

## 4. OBJECTIVE
<objective verbatim>

## 5. DELIVERABLE — your report
Write `active/<slug>/subagents/<name>/report.md`. **Lead with the answer, not a warm-up.** This format is distilled from the reports that actually survived ratification in this project — follow it:
- **VERDICT** — the headline answer in 1-5 lines, stated FIRST, each point tagged [DIRECT]/[INFERRED]. If your evidence FALSIFIES the brief's premise or an assumption in CAMPAIGN.md, say so here, up front.
- **CLAIMS** — each stated value-precisely (exact bytes/offsets/RVAs/strings/decl-values); Evidence = DIRECT|INFERRED + the recipe/file/line that shows it (a runnable command or the primary artifact, never your memory); for INFERRED, add a sub-grade (INFERRED-strong / INFERRED-open) and name the alternative reading that still survives.
- **CORRECTIONS / OVERTURNS** — prior claims this work corrects (a CAMPAIGN.md note, an earlier subagent's report, a stated assumption): quote the old claim, give the DIRECT evidence that overturns it. This is how a multi-subagent campaign stays self-consistent — never silently leave a contradiction for the manager to find. ("none" if there are none.)
- **TRUTH-PROMOTION CANDIDATES** — your DIRECT, value-precise claims that are ready for the Wall, gathered into one list (each with its proposed truth target + recipe). Pre-stages the manager's ratification. INFERRED claims do NOT belong here.
- **DELIVERABLES** — (only if you BUILT or CHANGED something: a script, a decl, an injected artifact, a patch) the bill of materials — each output's path + size/hash + what it is + how the manager applies/verifies it. Distinct from EVIDENCE INDEX (that's what proves your claims; this is what you produced as the work product). An implementation/tooling dispatch usually leads with this, right after VERDICT.
- **RESIDUAL UNCERTAINTIES** — what is still INFERRED or unobserved, each with the experiment that would settle it AND an explicit **blocks / does-not-block the deliverable** tag. This is your evidence boundary, NOT a to-do list — do not propose next campaigns or future work.
- **MANAGER RUNBOOK** — only if your deliverable enables a live action (an injection, a live test, a patch): the exact steps for the manager, AND the specific signals (console strings, byte-diffs, crash absence) that CONFIRM vs FALSIFY success. Omit if not applicable.
- **EVIDENCE INDEX** — the artifacts you produced + what each shows; tag each `reproducible` (give the command) / `ground-truth` / `capture`.
- **MEMORY CANDIDATES** — (optional) non-obvious durable facts worth persisting across sessions, or "none". The manager ratifies these into the project memory store; you do not write to it.
- **OVERALL CONFIDENCE** Green|Yellow|Red + "what would falsify this".

Do NOT add a "next steps" / "future work" / "open questions" section. Deliver the answer to your objective; a still-open item belongs in RESIDUAL UNCERTAINTIES (tagged blocks/does-not-block). The manager — not you — decides what to investigate next.

## 6. EXIT
Time budget: <budget>. If blocked, stop and report what you have — partial > nothing.

## 7. POST-RETURN
The manager will triage your report against the Wall (review-subagent). You will NOT be re-invoked; state isn't preserved. A follow-up gets a fresh subagent with your report as context.

Begin now.
```

### Step 5: Dispatch — resolve the backend first

Read `tools/agents/config.json` → `backend`:

- **`claude` (the default):** Use the Agent tool: `subagent_type: general-purpose`, `run_in_background: true`, short `description`, `prompt` = the Step 4 brief, and pick `model` **by role** (CLAUDE.md §10): substantive RE / analysis / implementation → the manager's own tier (the strongest available model — today `opus`; never pin a model alias that may be retired, since a dead alias silently degrades the dispatch — that is exactly how the old `fable` default broke); mechanical, cheaply-checkable fan-out (bulk extraction, byte-diffs, log/crash triage) → a lighter tier (`haiku`) is fine. Match the model to the job.
- **An external provider (e.g. `codex`):** valid ONLY when the human set the backend field or explicitly said "use <provider>" for this dispatch — NEVER route externally on your own judgment. Procedure:
  1. Write the Step 4 brief to `active/<slug>/subagents/<provider>-<name>/brief.md`. **External subagent dirs are named `<provider>-<name>`** (e.g. `codex-combat-targeting`) so provenance is visible at a glance; the dispatcher enforces this prefix and creates the dir, so you may pass the bare `<name>` to it.
  2. Run (PowerShell tool, `run_in_background: true` for long jobs):
     `tools/agents/dispatch.ps1 -Provider <provider> -Slug <slug> -Name <name>`
  3. The report lands at `subagents/<provider>-<name>/report.md` (the dispatcher auto-copies the agent's `last_message.md` if it failed to write the report). Triage is identical — `review-subagent` is backend-blind.

External drafters take their standing rules from the repo-root `AGENTS.md`; the brief contract above is unchanged. See `tools/agents/README.md`.

### Step 6: Record in CAMPAIGN.md

Append under "Current state" (or a "Subagents in flight" note):

```
- `<name>` (dispatched <today>) — <one-line objective>; report → subagents/<name>/report.md
```

### Step 7: Confirm

```
Subagent <name> dispatched into active/<slug>/.
Report will land at active/<slug>/subagents/<name>/report.md.
When it returns, triage it with `review-subagent` — do NOT promote anything before that.
```

## Reference

- Triage on return: `review-subagent` skill.
- The Wall + DIRECT/INFERRED: `.claude/CLAUDE.md` §5 + `promote-truth` skill.
