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

`PreToolUse` applies one write-class decision to Claude Code `Write`/`Edit` and
Codex `apply_patch`. It denies accidental direct access to canonical campaign
and truth paths. A registered run may write its `report.md` and lazy
`payload/`; ordinary project files are writable only within its immutable
exact-path or bounded-directory grants. Adapters should expose the current run
through `runId` or `RE_DISCIPLINE_RUN_ID`; no registered grants means no
ordinary project writes. `SubagentStart` and `SubagentStop` emit no context for
ordinary host subagents. They inject assignment context or a return check only
when explicit dispatch metadata resolves one unique registered run and matches
its immutable context-pack identity. This reduces accidental scope and does
not attest the caller. Hosts without reliable hook delivery still remain safe
because engine mutations validate roles, revisions, paths, invariants, and
event heads.

## Curator Dispatch

The `knowledge-curator` role receives exact returned-run and finding handles,
a bounded context pack, the packaged schemas, a curator run, and one intake
destination. Host-specific subagent syntax may differ, but authority and write
scope do not.
