<!-- re-discipline:router v0.7.0 -->
# AGENTS.md - {{PROJECT_NAME}} entrypoint

This root file is a compatibility entrypoint for agents that discover
`AGENTS.md` at the repository root.

Direct Codex manager sessions must read and follow `.codex/AGENTS.md`.

Dispatched drafting agents given a `brief.md` under either
`active/<slug>/subagents/<workspace-id>/` or
`.re-discipline/agents/recruiting/<candidate>/runs/<workspace-id>/` must read
and follow `.codex/external-drafter-contract.md`.

Both roles must read `.re-discipline/project-profile.md` for the canonical
shared laws, project identity, and domain facts. Project-owned host notes live
outside the managed block in the applicable manager adapter.

All roles use the same project knowledge policy in
`.re-discipline/config.json`. Drafters receive an immutable context pack
materialized inside their workspace and may not accept memory proposals or
change retrieval profiles.

Do not merge these roles. The manager scopes, delegates, ratifies, promotes
truth, and closes campaigns. Drafters investigate only their brief and report
evidence for manager review.
<!-- re-discipline:router:end -->
