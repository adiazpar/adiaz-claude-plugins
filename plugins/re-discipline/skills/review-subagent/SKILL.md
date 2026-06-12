---
name: review-subagent
description: This skill should be used when a subagent dispatched via delegate completes and returns a report, or when asked to "triage the subagent's report", "review the subagent's findings", "process the subagent's results". Triages the report against the DIRECT-evidence Wall (DIRECT vs INFERRED) BEFORE any promotion. Green/Yellow/Red. Subagents draft; the manager ratifies — NEVER blindly accept a subagent claim.
argument-hint: <slug>/<subagent-name> (or report path)
allowed-tools: Read, Glob, Grep, Bash, AskUserQuestion
---

# Review-subagent — ratify (or reject) a subagent's draft against the Wall

A subagent's report is a **draft**. Triage is your epistemic work as manager: nothing crosses into `docs/truth/` until you have classified its evidence against the Wall yourself. Blindly trusting a subagent is the #1 way false claims enter truth.

**The subagent's prose is a CLAIM about its evidence — not the evidence.** The actual DIRECT evidence is the preserved primary artifact it left behind: the decompile logs in `decomps/`, the scripts in `ghidra/`, the captures/ground-truth in `artifacts/`. A summary can mis-transcribe what the decompile or the bytes actually show. So for any **value-precise** claim (exact bytes, keys, offsets, RVAs, strings) you intend to PROMOTE — or that appears to CONTRADICT another DIRECT source — **open the primary artifact and read the value yourself; never adjudicate (accept OR reject) from the prose alone.** This cuts both ways: blindly trusting the summary promotes false claims; blindly distrusting it on a surface "contradiction" rejects correct findings.

**Weigh by provenance — and a user/tool-generated artifact is NOT infallible ground-truth.** When a subagent's RE conflicts with a user-provided artifact (a saved rawmap, an editor export), do NOT assume the artifact wins. RE of the engine's own code attests what the engine **does**; a saved/exported artifact attests only **what a human + tool produced** (which can be lossy or wrong). Each source can be PARTLY right — reconcile by what each reliably attests and combine, with the engine oracle as the empirical tiebreaker. (2026-05-26: a sound-arg encoding took three tries because a user's GUI-saved rawmap — a lossy *flat* value — was trusted over the subagent's decompile, which correctly showed the *nested* value; the right answer combined the artifact's key with the RE's nesting. See `.claude/CLAUDE.md` §5 + memory `triage-against-primary-evidence`.)

## Procedure

### Step 1: Read the report

Locate `active/<slug>/subagents/<name>/report.md` and read it in full, **OVERALL CONFIDENCE and SUMMARY first**:

- **Green** — trust the claims; high probability correct.
- **Yellow** — trust the EVIDENCE but interpret the claims cautiously; likely needs an extra verification step before promotion.
- **Red** — treat as a discarded hypothesis. Do not promote. Its value is the dead-end it records (which becomes a chronicle entry at close).

The overall color sets your prior; the per-claim Wall check below sets the verdict.

### Step 2: Re-classify every claim's evidence yourself (the Wall)

Do not accept the subagent's DIRECT/INFERRED labels at face value — re-derive them. For each claim, ask: **"Could this evidence exist even if the claim were false?"**

| Class | What it is |
|---|---|
| **DIRECT** | Observed, not inferred. Impossible if the claim were false: a decompiled instruction; an oracle byte-diff; a live A/B test where the variable under test is the ONLY change; a value read from the game's own decl/schema in `reference/`. |
| **INFERRED** | Best-explanation; other readings survive: "a thread passed through a function containing string X, so X fired"; "behavior changed when I changed Y" (among other changes); elimination ("not Z, so W"); a corpus-mined pattern presented as the complete grammar. |

Common downgrades to catch: a circumstantial call-chain attribution dressed as DIRECT; an elimination argument treated as proof of its alternative; a corpus statistic presented as a game-defined fact (check `reference/` — the authoritative source makes it DIRECT, the corpus alone is INFERRED).

### Step 3: Contradiction scan

For each claim that might be promoted, Grep `docs/truth/` and the relevant `docs/history/chronicles/` for the subsystem, RVAs/function names, and key nouns. Flag:
- **Contradiction** — an existing truth asserts the opposite (resolve before anything promotes), or a chronicle's dead-end already buried this exact idea (the subagent re-derived a known-dead hypothesis).
- **Overlap** — an existing truth already says this (it's an augment via promote-truth, not a new file).
- **Dependency** — an existing truth would be affected if this is true.

### Step 4: Assign a per-claim verdict

| Verdict | Condition | Disposition |
|---|---|---|
| **PROMOTE** | DIRECT evidence, no contradiction | hand to `promote-truth` (Step 5) |
| **HOLD** | INFERRED, or Yellow needing one more DIRECT step | stays in the campaign; record what DIRECT evidence would confirm it; possibly `delegate` a follow-up |
| **DROP** | Red / disproven / re-derived dead-end | record as a dead-end in `CAMPAIGN.md` (becomes a chronicle entry at close); do not promote |
| **BLOCK** | contradiction with an existing truth / ground-truth | stop; **reconcile at the primary-evidence layer** — open BOTH sides' raw artifacts (the subagent's `decomps/` log / `artifacts/` AND the conflicting truth or ground-truth file) and read the disputed value directly. A contradiction is *often a summary-transcription error, not a real conflict* — resolving it from prose can wrongly REJECT a correct finding (or promote a wrong one). Only after the raw values genuinely disagree do you pick a winner. |

INFERRED never crosses the Wall. That is the whole point — it stays provisional in `active/`.

### Step 5: Present to the user, then act

Summarize via AskUserQuestion: each claim, your re-derived evidence class, your verdict. Options: Approve all PROMOTEs / Selective / Inspect-first.

For approved PROMOTEs → invoke the `promote-truth` skill per claim (it re-applies the Wall as the final gate). Do **not** edit truth files directly here.

### Step 5.5: Ratify MEMORY CANDIDATES (if the report has any)

The report may list **MEMORY CANDIDATES** (durable cross-session facts the drafter proposes). They are drafts like any claim — you ratify, the drafter does not write memory. For each: keep only if it's non-obvious AND not already in the repo/truth/`MEMORY.md` (dedupe against the index first). For a keeper, write the memory file + add its one-line `MEMORY.md` pointer per the project memory conventions. A candidate resting on an INFERRED claim waits until that claim is DIRECT — don't persist a guess as durable knowledge.

### Step 6: Fold the rest into the campaign

- Update `active/<slug>/CAMPAIGN.md`: record the subagent (name, date), which claims were PROMOTEd, which are on HOLD (with the DIRECT evidence that would resolve them), which are DROPped (into "Dead ends so far"), and any new open questions.
- For NEW QUESTIONS worth their own pass: add to Open questions or `delegate` a follow-up subagent.
- **Subagent artifacts STAY** in `active/<slug>/subagents/<name>/` — they're provenance until close-campaign dispositions them.

### Step 7: Commit (only if something was promoted)

```powershell
git add docs/truth docs/truth/INDEX.md active/<slug> ; git commit -m "campaign: <slug> -- review <name>, promote N findings"
```

If nothing was promoted (all HOLD/DROP), commit just the CAMPAIGN.md update, or none.

## Reference

- The promotion door: `promote-truth` skill.
- The Wall + manager voice: `.claude/CLAUDE.md` §5-6.
- Dispatch a follow-up: `delegate` skill.
