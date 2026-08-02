---
name: decide-agent
description: >-
  Apply a user's explicit decision to promote, reject, or remove an evaluated
  external agent. Updates normalized provider configuration and durable
  provider records without changing manager adapters.
---

# Apply An External-Agent Decision

The user, not the manager, chooses `promote`, `reject`, or `fire`. If the
current request does not state that choice explicitly, present the scorecard or
target and obtain confirmation before changing anything.

Root and host instruction files are manager routing adapters. Never replace
them with a provider-specific drafter prompt.

## Promote

Require the candidate's `candidate.md`, `config.json`, `profile.md`,
`scorecard.md`, and `teardown.md`. Require an affirmative recommendation and a
successful sandboxed candidate dry run.

1. Extract the verified provider entry from candidate `config.json`.
2. Add it to `.re-discipline/agents/config.json`. Leave `backend` unchanged
   unless the user also selected this provider as the project default.
3. Create `.re-discipline/agents/providers/<provider>/` containing exactly:
   `profile.md`, `scorecard.md`, and `teardown.md`.
4. Make only user-approved CLI or tool registrations permanent.
5. Parse live config and run a sandboxed live-provider dry run.
6. Delete the candidate directory, including raw `runs/`, only after the live
   checks and teardown coverage succeed.

Provider presence in live config is the complete promotion state. Do not add a
second status field or duplicate provider list.

## Reject

Require an existing candidate workspace. Apply its `teardown.md` in reverse,
verifying each exact target before deletion or config editing. Delete the
candidate directory only after temporary external registrations are gone.
Leave the CLI installed unless the user explicitly asks to uninstall it.
Create no retained rejection artifact.

## Fire

Require a configured provider and its three-file durable record.

1. If live `backend` selects the provider, change it to `native`.
2. Apply `teardown.md` in reverse, verifying every exact external target.
3. Remove only that provider entry from live config and parse the result.
4. Delete only `.re-discipline/agents/providers/<provider>/`.
5. Search current configuration for unintended provider residue.

Create no retained firing artifact or event-specific narrative. Never delete
the shared root router, manager adapters, external drafter contract, agent
core, or another provider's configuration. Leave the CLI installed unless the
user explicitly asks otherwise.

## Verify

Parse every edited JSON file and search for unintended current references.
After promotion, run a sandboxed dispatcher dry run and confirm the provider
directory contains exactly the three durable Markdown files. After reject or
fire, confirm exact teardown targets are gone and report residue instead of
hiding partial cleanup.

Do not commit unless the user explicitly asks.

## Reference

- Integration checklist: `references/integration-points.md`.
- Evaluation: `hire-agent`.
