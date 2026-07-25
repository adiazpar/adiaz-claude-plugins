# External Provider Integration Points

Promotion records an exact inverse for every external point it touches. Fire
applies those instructions in reverse.

1. `.re-discipline/agents/config.json`: add or remove one provider. The user
   controls `backend`; reset it to `native` before removing the selected
   provider.
2. `.re-discipline/agents/providers/<provider>/profile.md`: provider-specific
   prompting and operational guidance.
3. `.re-discipline/agents/providers/<provider>/scorecard.md`: evaluation
   evidence and the user-approved promotion basis.
4. `.re-discipline/agents/providers/<provider>/teardown.md`: exact additions,
   config keys, external paths, and inverse operations.
5. Provider CLI configuration: only approved tool registrations and their
   exact config locations. Never store credentials in the repository.

Do not edit root `AGENTS.md`, `.codex/AGENTS.md`, or `.claude/CLAUDE.md` for
provider membership. Live config is the only provider list and backend switch.

After promotion, parse config, verify the three-file provider record, and run a
sandboxed dispatcher dry run. After reject or fire, search the repository and
recorded external config locations for the provider name. Explain any
unrelated historical prose that intentionally remains.
