# Runtime Adapters

Claude Code and Codex use one packaged state and knowledge engine. MCP is the
normal structured surface; the packaged CLI is a local peer fallback. Given
identical canonical state and request input, both return the same semantic
result digest. Host transport metadata is excluded from that digest.

Resolve the runtime from the active plugin installation, never a bare command,
source build, or unrelated cache. Pass explicit asset and project roots and
canonicalize both before invocation.

## Manager Flow

1. Session start requests bounded `state(mode="orient")`.
2. Resume selects a campaign and optional generation or event delta.
3. Substantive work receives a work item and run through engine mutations.
4. Preview the context pack against the registered active run and retain its
   digest; only the atomic `manager_apply` `run.prepare` transition may publish
   that active-run pack. Recruiting-run packs use the separate derived
   recruiting destination.
5. External dispatch receives an existing run; the adapter cannot create or
   mutate workflow records.
6. Return freezes a report digest and queues curation; it does not review.

## Hook Parity

PowerShell and POSIX implement the same semantic decisions for SessionStart,
PreToolUse, PreCompact, PostCompact, SubagentStart, SubagentStop, and Stop.
Compare normalized JSON decisions, not incidental wording or path separators.

PreToolUse denies accidental direct Write/Edit access to canonical campaign
and truth paths. A registered run may write its `report.md` and lazy
`payload/`; ordinary project files are writable only within the explicit run
grant. Hosts without reliable hook delivery still remain safe because engine
mutations validate roles, revisions, paths, invariants, and event heads.

## Curator Dispatch

The `knowledge-curator` role receives exact returned-run and finding handles,
a bounded context pack, the packaged schemas, a curator run, and one intake
destination. Host-specific subagent syntax may differ, but authority and write
scope do not.
