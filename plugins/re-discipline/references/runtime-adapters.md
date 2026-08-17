# Runtime Adapters

Claude Code and Codex use one packaged state and knowledge engine. MCP is the
normal structured surface; the packaged CLI is a local peer fallback. Given
identical canonical state and request input, both return the same semantic
result digest. Host transport metadata is excluded from that digest.

Resolve the runtime from the active plugin installation, never a bare command,
source build, or unrelated cache. Pass explicit asset and project roots and
canonicalize both before invocation.

## Manager Flow

1. Session start requests bounded `state(mode="orient")` once for that host
   session. Ordinary prompts, tool rounds, and compaction do not re-onboard;
   only a new/resumed host session or explicit runtime/state invalidation does.
   Duplicate SessionStart delivery for the same host `session_id` is suppressed
   by a disposable atomic marker.
2. Resume selects a campaign and optional generation or event delta.
3. Substantive work receives a work item and run through engine mutations.
4. Preview the context pack against the registered active run and retain its
   digest; only the atomic `manager_apply` `run.prepare` transition may publish
   that active-run pack. Recruiting-run packs use the separate derived
   recruiting destination.
5. External dispatch receives an existing run; the adapter cannot create or
   mutate workflow records.
6. Return accepts the report SHA-256, derives the exact canonical run-local
   `active/<slug>/runs/<run-id>/report.md` path, verifies those bytes, and
   queues curation; it does not review. A manager-supplied report path is
   refused before publication.

## Hook Parity

PowerShell and POSIX implement the same semantic decisions for SessionStart,
PreToolUse, PostToolUse, PreCompact, PostCompact, SubagentStart, SubagentStop,
and Stop.
Compare normalized JSON decisions, not incidental wording or path separators.

`PreToolUse` applies one write-class decision to Claude Code `Write`/`Edit` and
Codex `apply_patch`. It denies accidental direct access to canonical campaign
and truth paths. Relative and absolute paths are normalized against the
verified root before containment is evaluated. A registered run may write its
`report.md` and lazy `payload/`; ordinary project files are writable only
within its immutable exact-path or bounded-directory grants.

Native registered launches begin with
`re-discipline-run: <run-id> <context-pack-digest>` as the exact first message
line. `PreToolUse` verifies it and atomically reserves one pending launch slot
under the disposable cache for that manager `session_id`; ordinary launches
reserve the same slot with an ordinary kind. `SubagentStart` claims the slot
into a binding keyed by the host `agent_id`. Simultaneous launch calls within
one manager session wait inside this short handoff rather than failing and
asking the model to retry. Any number of already-bound agents may then execute
concurrently, and every write resolves the nested session/agent identity back
to either one registered run or an ordinary non-canonical write boundary.
Separate manager sessions use separate directories. `PostToolUse` clears a
reservation when a launch never produces `SubagentStart`. An agent that
bypassed launch registration fails closed for project writes, and
process-global run environment variables are ignored for agent-aware events so
siblings cannot inherit one another's grant identity.

Legacy explicit `runId`/`runPath` envelopes remain a compatibility path for a
single worker when the host really supplies those fields. `SubagentStart` and
`SubagentStop` emit no context for ordinary host subagents. Registered context
is injected only when the resolved run and immutable context-pack identity
match. This reduces accidental scope and does not attest the caller. Hosts
without reliable hook delivery still remain safe because engine mutations
validate roles, revisions, paths, invariants, and event heads.

## Concurrent Managers

Independent manager processes may operate on different campaigns in one
project. Canonical publication is protected by the project-wide OS writer lock
(`LockFileEx` on Windows and `flock` on POSIX), a crash-recoverable journal,
and a global state head. That head is an internal serialization and audit
pointer for ordinary manager and curator mutations. If two disjoint
transactions were compiled from the same head, the second waits only for the
publication lock and the engine rebases it once inside the same call. No agent
refresh, context-pack rebuild, or retry is required.

Ordinary `manager_apply` and `curation_submit` calls omit
`expectedHeadRevision` and `expectedHeadDigest`; the engine records the actual
previous and resulting heads in the receipt. Supplying an observed pair remains
compatible and records provenance, but it is not a freshness gate. The pair is
still mandatory for the project-global operations named below.

Record revisions and digests, artifact expectations, graph invariants, and
write grants remain exact. Two operations that really touch the same record or
path still produce one commit and one actionable conflict rather than an
overwrite. Destructive campaign topology, reconciliation, migration, and
closure finalization keep exact-head gates because their proof is
project-global. An active-run context pack is a sealed snapshot with generation
and state provenance, not a lease on the latest head or retrieval index;
unrelated advancement does not invalidate it.

At `run.prepare`, the engine additionally rejects any exact/directory grant
that overlaps a grant held by another `prepared` or `running` run anywhere in
the project. A terminal or returned run releases its project grants. This
keeps concurrent subagents from receiving authority over the same project
bytes even when their campaigns and manager sessions differ.

Mutation token budgets are advisory response hints. The engine may omit only
named optional sections; it always returns the transaction identity floor and
never refuses a valid mutation because a hint is below or above a preferred
display size.

## Curator Dispatch

The `knowledge-curator` role receives exact returned-run and finding handles,
a bounded context pack, the packaged schemas, a curator run, and one intake
destination. Host-specific subagent syntax may differ, but authority and write
scope do not.
