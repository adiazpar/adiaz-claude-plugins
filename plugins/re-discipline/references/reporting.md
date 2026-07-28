# Two-Audience Reporting Law

Every re-discipline surface has two audiences and one source of truth. The
agent reads and reasons over full technical state. The user sees plain
language. These rules govern every user-facing report in every skill.

## Dashboard Plus Exceptions

Healthy subsystems get one plain word. Detail appears only when a human
decision is needed, phrased as situation then proposed action. Never
narrate internals to prove work happened; the system block and campaign
records are the audit trail.

## Vocabulary Line

User-facing (the discipline's own language): campaign, truth, chronicle,
DIRECT and INFERRED evidence, memory proposal, drafter, delegation.

Agent-internal only - never printed to the user: corpus generation,
generation IDs, retrieval lanes, RRF or reciprocal-rank weights, requested
and effective profiles, fallback reasons, fingerprints, evidence pins,
freshness and staleness flags, chunker and parser versions, reranking,
context-pack digests. Read them, use them, cite them in campaign files and
system records; do not say them to the user.

## Silent Self-Healing

Cheap, safe, local, reversible repairs happen automatically and are not
mentioned: refreshing a stale index, restoring a missing tracked file.
They are recorded in system-facing state (the ensure payload, server
logs), never narrated. Expensive or judgment-laden actions always ask
first: benchmarks, calibration, memory acceptance, retrieval-profile
promotion, anything that changes measured behavior.

Measurement health follows the same principle lazily: benchmark staleness
and evidence-pin drift matter only when measurements are used, so the
measurement skills (benchmark-knowledge, calibrate-knowledge,
decide-retrieval-profile) check and repair them at their own gate time.
Onboarding and session start never mention them.

## Marking User-Facing Output

A skill's printable template is a fenced block whose info string is
`user-facing`. The repository lint scans exactly those blocks for the
banned vocabulary. Prose instructions about what to *do* with machinery
state are fine anywhere; printable text lives only in marked fences.

## Host Adapter Hygiene

`.claude/CLAUDE.md` and `.codex/AGENTS.md` state current configuration
only: present tense, no dates, no migration narration, no "older X is
obsolete" prose. History belongs in docs/history/. Retired guidance is
deleted, not memorialized.
