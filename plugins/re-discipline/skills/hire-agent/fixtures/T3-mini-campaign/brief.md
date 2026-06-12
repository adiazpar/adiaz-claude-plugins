# Interview task T3 — mini-campaign (the production loop, on a proven result)

This is the deciding test: it runs the real drafter→ratify loop on a question that has a KNOWN,
proven answer (withheld from you). You are a drafter subagent; your report will be triaged by a
manager against the evidence Wall. Do the RE legwork and report honestly.

## Isolation (critical — do not break)

Base your work ONLY on the evidence below + the live tools you are granted. You are **scoped away
from the answer**: do NOT read `docs/truth/**`, `docs/history/chronicles/**`, or any campaign
`CAMPAIGN.md` — those contain or hint at the solution and reading them invalidates the test. If
you find yourself about to look up the answer, stop; derive it.

## Granted live surface

You are granted the **ghidra** MCP (read-only static analysis) and may read the listed binary
artifacts. Single live consumer: you own Ghidra for this task.

## The question (seed: the archive-mount verify gate)

Background: DOOM 2016 ships resource archives (`*.resources` / `*.patch`) each with a small
`.verify` sidecar. When a modified archive is mounted with a stale/wrong sidecar, the game
**black-screens before render** (no error UI). 

Derive, from the engine code:
1. **What the `.verify` sidecar contains and how it is structured** (header? per-block hashes?
   block size? hash/MAC algorithm?).
2. **How the engine enforces it at mount** — which function reads it, what happens on mismatch,
   and why the symptom is a pre-render black screen rather than a recoverable error.
3. **The key derivation** — how the MAC key is built (from what inputs), and crucially **whether
   it depends on any machine/Steam secret** (i.e. is offline regeneration of a valid `.verify`
   achievable, or is it gated by a secret you cannot reproduce?).

Starting points (verify everything yourself; these are leads, not answers): the resource-container
open path around `idResourceContainer::Open`; the sidecar reader; the KDF the reader calls.

## Deliverable

Write your report in the AGENTS.md report format. For each of the three points, give your finding
with Confidence + DIRECT/INFERRED + the exact RVA/decompile that shows it. **If you cannot
determine a point from the available evidence, say so and name what you'd need — do NOT guess.**
Your draft will be judged on whether it lets the manager ratify the correct, proven answer.
