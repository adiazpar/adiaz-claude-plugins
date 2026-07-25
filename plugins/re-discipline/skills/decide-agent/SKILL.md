---
name: decide-agent
description: >-
  Apply a user's explicit decision to promote, reject, or remove an evaluated
  external agent. Updates the external-provider roster, provider profile, and
  teardown record without changing the Claude Code or Codex manager adapters.
---

# Apply An External-Agent Decision

The user, not the manager, chooses `promote`, `reject`, or `fire`. If the
current request does not state that choice explicitly, present the scorecard or
target and obtain confirmation before changing anything.

The root `AGENTS.md`, `.codex/AGENTS.md`, and `.claude/CLAUDE.md` are manager
routing adapters. Never replace them with a provider-specific drafter prompt.

## Promote

Require `recruiting/<candidate>/scorecard.md`, `config-draft.json`, and
`profile-draft.md`.

1. Add the verified provider entry to `agents/config.json` with
   `promoted: true`; leave `backend` unchanged unless the user also selected
   this provider as the default.
2. Copy the profile to `agents/profiles/<provider>.md` and reference it from
   the provider entry.
3. Make only the user-approved CLI/MCP registrations permanent.
4. Update the roster section in `agents/README.md` without duplicating the
   roster in either harness profile.
5. Write `agents/roster/<provider>/teardown-manifest.md` with exact inverse
   operations and a dated hiring record.
6. Remove the recruiting directory only after the live config parses, a
   sandboxed dry run succeeds, and the teardown manifest covers every external
   change.

## Reject

Require an existing recruiting workspace. Replay its rollback manifest in
reverse, verifying each target before deletion or config editing. Remove the
candidate workspace only after temporary external registrations are gone.
Leave the CLI installed unless the user explicitly asks to uninstall it.

## Fire

Require a promoted provider and its teardown manifest. Replay the manifest in
reverse:

1. remove only that provider from `agents/config.json`;
2. reset `backend` to `native` first if it selected that provider;
3. remove only that provider's profile and approved external registrations;
4. remove only that provider's roster documentation;
5. retain a dated departure record, then remove its roster directory.

Never delete the shared root router, manager adapters, external drafter
contract, or another provider's configuration. Leave the CLI installed unless
the user explicitly asks otherwise.

## Verify

Parse all edited JSON, search for unintended residual references, and run a
sandboxed dispatcher dry run after promotion. For reject or fire, confirm the
rollback targets match the manifest and report any residue rather than hiding
partial cleanup.

Do not commit unless the user explicitly asks.

## Reference

- Integration checklist: `references/integration-points.md`.
- Evaluation: `hire-agent`.
