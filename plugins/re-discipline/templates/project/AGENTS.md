<!-- re-discipline:router v0.2.0 -->
# AGENTS.md - {{PROJECT_NAME}} entrypoint

This root file is a compatibility entrypoint for agents that discover
`AGENTS.md` at the repository root.

Direct Codex manager sessions must read and follow `.codex/AGENTS.md`.

Dispatched drafting agents given a `brief.md` under
`active/<slug>/subagents/<name>/` must read and follow
`.codex/external-drafter-contract.md`.

Both roles must read `.re-discipline/project-profile.md` for the canonical
project identity and domain facts. Harness-specific operating notes live in
`.codex/project-profile.md` or `.claude/project-profile.md`.

Do not merge these roles. The manager scopes, delegates, ratifies, promotes
truth, and closes campaigns. Drafters investigate only their brief and report
evidence for manager review.
<!-- re-discipline:router:end -->
