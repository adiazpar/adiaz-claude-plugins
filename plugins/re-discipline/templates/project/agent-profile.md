---
provider: {{PROVIDER}}
model: {{MODEL_ID_OR_DEFAULT}}
role-fit: [{{ROLES}}]
promoted: {{DATE}}
---

# External Provider Profile - {{PROVIDER}}

This file is the provider-specific overlay for an external drafter. Shared
evidence, scope, and report rules live in
`.codex/external-drafter-contract.md`; do not duplicate them here.

## How To Prompt This Model

{{PROMPT_STYLE_BULLETS}}

Use current provider documentation and observed interview behavior. Record
useful structure, context ordering, autonomy boundaries, effort controls, and
known instruction-following constraints.

## Strengths And Weaknesses

{{STRENGTHS_WEAKNESSES}}

## Operational Constraints

{{QUIRKS}}

Include authentication boundaries, output limits, tool availability, and modes
that were unsafe or unreliable. Sandbox bypass is never assumed.

## Benchmark

- Fixture set: {{FIXTURE_VERSION}}
- Last assessed: {{DATE}}
- Evidence: `agents/benchmarks/`
