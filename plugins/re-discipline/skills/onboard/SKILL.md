---
name: onboard
description: >-
  Orient a re-discipline project at session start or when asked to get caught
  up, inspect current work, or resume a campaign. Uses bounded shared-engine
  state views instead of loading the project corpus.
---

# Onboard A Re-Discipline Project

Onboarding is read-only. Treat source records as canonical and indexes and
generated views as derived.

## Orient

1. Read `.re-discipline/project-profile.md` and the active host adapter.
2. Validate `.re-discipline/config.json` and locate the packaged knowledge
   runtime for the active plugin installation.
3. Run the shared engine health operation. Do not index, benchmark, calibrate,
   repair, or mutate during onboarding.
4. Call `state` with `mode: orient` and a small token budget. Use the CLI peer
   only when MCP is unavailable.
5. Present project identity, active campaign handles, urgent blockers, and
   health in one screen. Do not preload reports, findings, or history.

If canonical records are missing or malformed, report the exact problem and
use `init-project` recovery. If the project declares an older state version,
stop ordinary mutations and use `migrate-project`; initialization never
converts prior state implicitly.

## Resume A Named Campaign

When the user names a campaign, call `state` with `mode: resume`, its campaign
ID, and the last known generation or event handle when available. Expand only
handles needed for the next decision. To begin substantive work, call `state`
with `mode: work` for the selected work item.

Never infer authoritative state from `STATE.md`; it is a bounded generated
view that must be reproducible from records.

## Return

Report:

- the project mission in one sentence;
- active campaign handles and current focus;
- blocked or due-deferred work;
- pending returned runs or review packets;
- knowledge availability and delegation route;
- the next valid state transition.

Do not expose internal lane, model, cache, or fingerprint details unless they
are the subject of the request. Stop after orientation.

## References

- Runtime mapping: `<plugin-root>/references/runtime-adapters.md`.
- Governance: `<plugin-root>/references/knowledge-governance.md`.
- Canonical project laws: `.re-discipline/project-profile.md`.
