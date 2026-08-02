# Interview task T3 - production-shaped investigation

This test evaluates a drafter on a question with a known answer held by the
manager. Report honestly and let the manager make every epistemic decision.

## Isolation

Use only the evidence and live tools granted in this run. Do not read
`docs/truth/**`, `docs/history/campaigns/**`, or campaign finding records;
those contain or hint at the answer and invalidate the comparison.

## Granted live surface

You may use the read-only Ghidra surface and the binary artifacts named in the
run brief. This test owns the single logical Ghidra consumer.

## Question

DOOM 2016 resource archives use a small `.verify` sidecar. A modified archive
mounted with a stale sidecar black-screens before render.

Derive from engine code:

1. the sidecar structure, block size, and hash or MAC algorithm;
2. the mount-time enforcement path and mismatch behavior;
3. the key derivation inputs and whether offline regeneration requires a
   machine or Steam secret.

Starting points are the resource-container open path, sidecar reader, and the
KDF it calls. Treat them as leads, not answers.

## Deliverable

Write the required run report. For each point give confidence, evidence grade,
and exact decompile or address citation. If the evidence cannot decide a point,
say so and name the missing observation. Do not guess.
