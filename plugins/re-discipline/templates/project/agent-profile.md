---
provider: {{PROVIDER}}            # the key in tools/agents/config.json (e.g. codex, gemini)
model: {{MODEL_ID_OR_DEFAULT}}    # the exact model id, or "rides CLI default (<id> today)"
role-fit: [{{ROLES}}]             # from the scorecard: which positions this agent is good for —
                                  #   e.g. RE-analyst, implementation, mechanical-fan-out, synthesizer
promoted: {{DATE}}                # set by decide-agent promote; absent for a candidate draft
---

# Agent profile — {{PROVIDER}}

The per-model overlay layered over the shared `AGENTS.md` contract. The dispatcher PREPENDS the
"How to prompt this model" section to every brief sent to this agent. Keep this small and specific —
the shared rules (role, the Wall, report format, scope) live in `AGENTS.md` and are NOT repeated here.

## How to prompt this model

<!-- The model-specific prompt-style, from the hiring research (research-checklist item 12) + live
     use. Be concrete and model-true. Examples of the KIND of guidance (replace with the real,
     researched specifics for THIS model — do not copy blindly):
  - structure: does it prefer XML-tagged spec blocks, markdown sections, or plain prose?
  - instruction-following: how literal is it? are contradictory instructions especially harmful?
  - autonomy: "propose and proceed" vs "ask for confirmation"; explicit stop-conditions / tool budgets?
  - context ordering: context-first-then-instructions, or instructions-first?
  - reasoning/effort knobs it exposes, and the right default for substantive RE work. -->

- {{PROMPT_STYLE_BULLETS}}

## Strengths / weaknesses (interview battery + live use)

<!-- From the scorecard (tools/agents/benchmarks/) + observed dispatches. What it's reliably good at,
     where it overclaims or underperforms, how it compares to the team. -->

- {{STRENGTHS_WEAKNESSES}}

## Quirks / gotchas

<!-- Operational traps the manager must know — e.g. "exec auto-cancels MCP calls under any sandbox →
     bypass is the default", auth model, rate/credit limits, output truncation behavior. -->

- {{QUIRKS}}

## Benchmark

- Baselines: `tools/agents/benchmarks/<task>/` (the runs that produced this profile's role-fit).
- Last (re)assessed: {{DATE}} on fixture set {{FIXTURE_VERSION}}.
