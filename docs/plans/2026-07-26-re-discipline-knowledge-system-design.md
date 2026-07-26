# Re-Discipline Unified Knowledge System Design

Status: implementation complete locally; retained pending required cross-platform and race CI gates

Date: 2026-07-26

Applies to: `plugins/re-discipline`

## Summary

Re-discipline will provide one project-owned knowledge system to Claude Code,
Codex, their native subagents, and configured external drafters.

Newly initialized projects default to:

- shared project knowledge only;
- host-native project memory disabled;
- local embeddings and reranking;
- proposal-only shared-memory writes;
- no automatic ranking-profile promotion;
- no network use for retrieval, embeddings, reranking, or telemetry.

Canonical knowledge remains in ordinary tracked project files. The knowledge
server builds disposable local indexes over those files and returns small,
citable context packs. A database or embedding is never a source of truth.

The operating rule is:

> Indexing happens automatically. Benchmarking measures. Calibration
> proposes. Promotion changes behavior.

Normal users receive tested plugin defaults and do not need to tune retrieval.
Any user may run a read-only benchmark. Project maintainers may calibrate a
project and may promote a passing candidate with explicit user approval.
Accepted project retrieval profiles are tracked and shared with that
repository. Global-default changes are controlled by plugin maintainers, are
tested in the plugin repository, and ship only in a new plugin release.

## Goals

1. Give every manager and drafter the same project-owned memory and knowledge.
2. Preserve re-discipline's epistemic tiers and DIRECT-evidence Wall.
3. Reduce repeated reading and context tokens without hiding provenance.
4. Support exact technical identifiers and conceptual questions.
5. Preserve context across sessions, compaction, managers, and subagents.
6. Scale from a small Markdown corpus to large, long-lived research projects.
7. Remain local, private, cross-host, and provider-neutral by default.
8. Make every index, ranking decision, context pack, and profile reproducible.
9. Let retrieval improve through ratified evaluation rather than intuition.
10. Degrade safely when models, indexes, hooks, or optional features fail.

## Non-Goals

- Replacing `docs/truth/` with a database.
- Treating shared memory as empirical authority.
- Merging truth, history, provisional work, backlog, and recall into one trust
  class.
- Copying or synchronizing Claude and Codex native memory directories.
- Deleting user-global native memory.
- Indexing an entire repository or arbitrary external roots by default.
- Automatically learning from agent behavior.
- Automatically accepting memory proposals or retrieval profiles.
- Requiring a hosted vector database, embedding API, reranking API, or
  telemetry service.
- Fine-tuning a custom embedding or reranking model in the initial system.
- Depending on a project's Python environment or a globally installed runtime.

## Core Invariants

### Files remain authoritative

The durable corpus is made of tracked project files:

- `.re-discipline/project-profile.md`;
- `docs/INDEX.md` and tier indexes;
- `docs/truth/**`;
- `docs/history/**`;
- `docs/backlog/**`;
- active campaign masterfiles;
- accepted `.re-discipline/memory/**` records;
- explicitly configured additional source classes.

Everything under `.re-discipline/cache/` is disposable. If deleting that
directory loses project knowledge, the implementation is incorrect.

### Trust gates precede relevance

The allowed epistemic tiers are selected before ranking. Relevance scores may
order results inside an allowed set, but may never convert:

- history into current truth;
- campaign work into verified truth;
- backlog intent into completed work;
- memory recall into empirical evidence.

A high vector score cannot cross the Wall.

### One project worktree, one shard

Each worktree has an isolated knowledge generation. A global database spanning
unrelated repositories is forbidden by default because it risks:

- branch contamination;
- project data leakage;
- relevance dilution;
- ambiguous source revisions.

Cross-project retrieval is explicit federation of isolated shards and requires
machine-local permission.

### Retrieval is reproducible

Every returned passage identifies:

- project and worktree;
- Git revision and dirty-state fingerprint;
- index generation;
- path, heading ancestry, and line span;
- source content hash;
- epistemic tier;
- parser and chunker version;
- requested retrieval profile version;
- effective retrieval profile identity;
- active retrieval lanes and model identities;
- any fallback state and its reason.

### Retrieval is deterministic for an effective profile

The core retrieval server does not recursively invoke an LLM. Query routing,
candidate retrieval, rank fusion, graph expansion, deduplication, and context
packing are deterministic for a pinned generation and effective retrieval
profile.

The requested project profile and the effective execution profile are distinct
identities. An effective profile is an immutable, content-hashed description
of the active lexical, graph, dense, reranking, and packing lanes, including
model checksums, weights, thresholds, and fallback state. Every supported
capability set has a separately versioned effective profile. Model
unavailability may select only a predefined effective fallback profile; it may
not dynamically improvise a new lane combination or reuse the requested
profile identity.

Every retrieval result and compiled context pack reports both profile
identities, the active lanes, model identities, and the fallback reason. Given
the same index generation, effective profile, query, and budget, replay must
produce the same ranked passages and packed context.

### Context is budgeted

Every query and context pack has hard passage, byte, and estimated-token
limits. Larger context windows do not justify unbounded retrieval.

## Logical Architecture

```text
Canonical project files
        |
        v
Versioned source adapters
        |
        v
Normalized content-addressed documents and chunks
        |
        +--> exact / path / symbol index
        +--> SQLite FTS5 / BM25 index
        +--> dense vector index
        `--> explicit relationship graph
                         |
                         v
                 query-aware fusion
                         |
                         v
                 conditional reranking
                         |
                         v
             token-budgeted context compiler
                         |
                         v
                   knowledge MCP
                         |
              +----------+----------+
              |                     |
              v                     v
           manager                drafter
              |                     |
              `------ reports ------'
                         |
                         v
              ratified evaluation cases
```

The retrieval pipeline and its data contracts are stable. Embedding models,
rerankers, vector backends, ranking parameters, and budgets are replaceable
behind versioned interfaces.

## Physical Layout

### Plugin package

```text
plugins/re-discipline/
    .mcp.json
    knowledge/
        models/
            manifest.json
        profiles/
            balanced-v1.json
        evals/
            conformance/
        schemas/
            config.schema.json
            knowledge-settings.schema.json
            retrieval-profile.schema.json
            eval-case.schema.json
            context-pack.schema.json
        runtime/
        server/
    skills/
        benchmark-knowledge/
        calibrate-knowledge/
        decide-retrieval-profile/
        review-memory/
```

The exact runtime packaging is an implementation decision, but it must expose
one self-contained local launch command to both Claude Code and Codex. The
plugin may not assume a project virtual environment, global Python, or global
Node installation.

### Initialized project

```text
.re-discipline/
    config.json
    project-profile.md
    settings/
        README.md
        knowledge.jsonc
        retrieval-profile.json
    memory/
        INDEX.md
        proposals/
        topics/
    knowledge/
        evals/
    cache/
        knowledge/
            current.json
            generations/
            vectors/
        calibration/
```

`.re-discipline/config.json`, `.re-discipline/settings/`, accepted memory,
pending memory proposals, and ratified evaluation cases are tracked. All cache
and calibration run output is ignored.

The settings directory is the documented project-facing control plane:

- `README.md` explains every file, ownership boundary, default, and recovery
  rule;
- `knowledge.jsonc` is the commented, human-editable policy for source
  classes, default context budgets, local execution, and local-only telemetry;
- `retrieval-profile.json` is generated and accepted through calibration
  governance rather than hand-edited.

Server code, model artifacts, indexes, benchmark runs, shared memory, and eval
cases do not belong in `settings/` because they have different authority and
lifecycle rules.

### Machine-local state

The platform user-data directory contains:

```text
re-discipline/
    models/
    hardware-settings.json
    trust-grants.json
```

This state is untracked. It owns:

- model artifacts and checksums;
- CPU/GPU selection;
- concurrency and memory limits;
- permitted external roots;
- optional remote-provider grants.

Machine-local state never contains production ranking weights. Hardware choice
may affect latency, but not ranking within one effective profile. Managers
with the same index generation, effective profile, query, and budget must
retrieve the same knowledge. If their available model capabilities differ,
they may use different predefined effective profiles, and that difference must
be explicit in every result rather than hidden as a hardware variation.

## Project Configuration

`.re-discipline/config.json` is a small strict-JSON bootstrap and recovery
manifest. Hooks and the bundled bootstrap CLI can parse it without first
loading the full knowledge runtime. It points to the documented settings
directory and carries only safety-critical initialization policy:

```json
{
  "schemaVersion": 1,
  "settingsDirectory": "settings",
  "memory": {
    "mode": "shared-only",
    "writePolicy": "proposal-only"
  },
  "knowledge": {
    "enabled": true,
    "profile": "plugin:balanced-v1",
    "settingsFile": "settings/knowledge.jsonc",
    "projectProfile": "settings/retrieval-profile.json"
  }
}
```

`settings/knowledge.jsonc` is the human-editable surface. It supports comments
and a `$schema` reference. It contains understandable policy rather than raw
internal machinery:

```jsonc
{
  "$schema": "plugin://re-discipline/schemas/knowledge-settings.schema.json",

  // Index only re-discipline-owned knowledge classes by default.
  "sources": {
    "truth": true,
    "history": true,
    "backlog": true,
    "activeCampaigns": true,
    "sharedMemory": true
  },

  // Embeddings and reranking run on this machine unless a user grants an
  // optional provider in machine-local trust settings.
  "models": {
    "execution": "local"
  },

  // Content traces are off. Local aggregate measurements are sufficient for
  // normal health and token reporting.
  "telemetry": {
    "mode": "metrics-only"
  }
}
```

`settings/retrieval-profile.json` is strict generated JSON. It records its
base profile, parameters, model revisions, benchmark digest, approval, schema,
and the finite set of supported effective profiles. Each effective profile has
its own content-hashed identity, active lanes, model requirements, weights,
thresholds, packing policy, fallback reason, and independent benchmark
evidence. Its `description` fields and `settings/README.md` explain the values;
comments are not used because the file is machine-generated and content-hashed.

The implementation schema may add fields, but it must preserve these
semantics:

- `shared-only` is the initialization default;
- shared memory is authoritative only for recall, not empirical truth;
- accepted memory requires manager ratification;
- local model execution is the default;
- network access is not granted by tracked configuration;
- the requested retrieval profile is explicit and versioned;
- every supported full or fallback execution shape has a distinct effective
  profile identity and benchmark record;
- project-facing settings are discoverable under one documented directory;
- production ranking parameters are changed only by profile promotion.

Security-sensitive grants live only in machine-local state. A repository may
not grant itself external-root access or remote data transmission.

## Native Memory Policy

Initialization materializes supported project-local host settings that disable
native project memory for Claude Code and Codex. It does not delete or modify
global memory directories.

The host adapters remain thin:

- they load the canonical project profile;
- they point the host at the same local knowledge server;
- they apply the selected project memory mode;
- they retain only host-specific operational notes.

Supported modes remain:

- `shared-only`: native project memory disabled; re-discipline is the project
  recall system;
- `hybrid`: native memory may exist as a private cache, but re-discipline is
  authoritative;
- `native`: host-native behavior, explicitly selected.

New projects start in `shared-only`.

## Configuration Recovery

The managed shared-law marker and schema version in
`.re-discipline/project-profile.md` state whether `config.json` and the
required `settings/` files are expected.

At SessionStart and before knowledge-server startup:

1. Locate the project root using the managed profile marker.
2. Validate any present bootstrap and required settings files.
3. Restore each missing tracked file from `HEAD`.
4. If a required file has never been tracked but the managed schema requires
   it, create the current safe default atomically.
5. Validate the complete recovered settings set and all referenced paths.
6. Re-materialize or verify host memory settings.
7. Report every recovered file.

Safeguards:

- never overwrite a malformed existing file silently;
- never downgrade a newer schema;
- never reconstruct machine-local grants;
- use create-if-absent and atomic replacement semantics;
- distinguish legacy projects from accidental deletion;
- keep expensive indexing and embedding work out of the startup hook.

Managed configuration is recovered even when its deletion is staged. Removing
re-discipline configuration requires a separate explicit de-initialization or
migration operation that first removes the managed expectation marker. This
keeps "regenerate if ever deleted" unambiguous and prevents a transient Git
state from re-enabling native memory.

If both the neutral config and host settings are absent, a launcher or MCP
preflight repairs them before the next fully compliant session. A SessionStart
repair cannot retroactively remove context that a host loaded before the hook.

## Source Discovery And Ingestion

### Default source classes

The initial allowlist includes:

- canonical project profile;
- documentation indexes;
- truth, history, and backlog Markdown;
- active `CAMPAIGN.md` files;
- accepted shared-memory records.

Additional source roots require explicit project classification and, for
external roots, machine-local permission.

The default denylist includes:

- `.git/**`;
- `.re-discipline/local-paths.md`;
- credential and environment files;
- private keys;
- raw binary and evidence directories;
- generated logs;
- subagent scratch except explicitly selected reports;
- the knowledge cache itself.

### Parsing and chunking

Markdown is parsed by structure, not fixed character windows.

The parser preserves as atomic units where practical:

- headings and their ancestry;
- claim, confidence, validity, re-verification, dependency, and evidence
  fields;
- code fences;
- tables;
- short truth records;
- links and source references.

Each searchable child chunk records:

- its parent section;
- adjacent chunks;
- exact source line span;
- deterministic context prefix;
- stable content-derived ID.

Generated summaries or contextual descriptions may assist retrieval but remain
derived navigation data. Context packs return unmodified source passages for
evidence.

### Incremental generations

The local cache is generation based:

```text
.re-discipline/cache/knowledge/
    current.json
    generations/<generation-id>.sqlite
    vectors/<generation-id>-<model-id>.index
```

A generation records the corpus fingerprint, source metadata, parser and
chunker versions, FTS index, relationship graph, model IDs, and retrieval
profile.

Incremental ingestion is content-hash based. Full rebuilds create a new
generation beside the active one, validate it, then atomically switch
`current.json`. Readers continue using the last complete generation.

File watchers are optional latency optimizations. Correctness relies on:

- SessionStart inventory reconciliation;
- explicit reconciliation after branch or worktree changes;
- content hashes;
- source verification immediately before return.

## Retrieval Pipeline

### Query routing

The deterministic router selects a retrieval profile by query class:

| Query class | Primary path |
|---|---|
| known source URI, path, or heading | direct read |
| symbol, hash, offset, path, or error text | exact, trigram, and BM25 |
| conceptual question | lexical plus dense |
| orientation | curated profile, indexes, and active masterfiles |
| current factual claim | permitted current tier first |
| provenance question | explicitly selected history and campaign tiers |
| dependency or invalidation question | retrieval plus graph expansion |
| contradiction review | explicit cross-tier retrieval with labels |

Query rewriting may add terms but never discard the original exact terms.

### Candidate generation

Candidate lanes run independently:

1. exact entity, path, title, and alias lookup;
2. substring or trigram identifier lookup;
3. SQLite FTS5/BM25;
4. dense semantic search;
5. graph neighbors and declared dependencies.

### Rank fusion

The default hybrid method is weighted reciprocal-rank fusion. It combines
independent rankings rather than adding incompatible BM25 and cosine scores.

Weights are profile data and vary by query class. Trust, access, and epistemic
tier are hard filters, never relevance weights.

### Conditional reranking

The local reranker operates on a bounded fused candidate set for broad or
ambiguous queries. It is skipped for unambiguous direct reads and exact
authoritative matches.

If the reranker is unavailable, the server returns fused results. If dense
search is unavailable, it returns lexical and graph results. Optional stages
improve quality but are not availability dependencies.

### Diversification and abstention

Before packing, the server:

- removes duplicate and heavily overlapping chunks;
- caps repeated passages from one document;
- expands a child to its parent or neighbor only when needed;
- follows explicit dependency edges for multi-source questions;
- reports conflicts and stale dependencies;
- abstains when authoritative evidence is insufficient.

## Context Compiler

Retrieval results are candidates, not the final context.

The compiler maximizes supporting-evidence coverage under a caller-supplied
budget. It considers:

- epistemic eligibility;
- relevance;
- evidence completeness;
- novelty per token;
- required source diversity;
- duplicate-token limits;
- mandatory project laws and task brief;
- citation overhead.

Evaluation covers standard budgets such as 512, 1024, 2048, and 4096 tokens.
Managers may receive broader orientation packs. Drafters receive narrow task
capsules plus handles for just-in-time follow-up reads.

Every context pack is immutable and contains:

- pack ID and retrieval query;
- project, worktree, and generation;
- caller role and allowed tiers;
- requested and effective retrieval profile identities;
- active lanes, model identities, and any fallback reason;
- token budget;
- exact source paths, headings, lines, hashes, and passages;
- omitted-result summary and follow-up handles;
- creation time.

## MCP Surface

The local MCP server uses stdio and exposes a small dedicated tool set:

### `status`

Returns configuration validity, active generation, requested and effective
retrieval profiles, active lanes, fallback state, model availability, index
freshness, and benchmark age.

### `orient`

Returns a bounded project or campaign orientation using curated navigation
sources rather than unconstrained search.

### `search`

Searches allowed knowledge tiers with explicit query class, filters, result
limit, and token budget. Returns structured results with citations.

### `read`

Reads a known source, section, chunk, or resource URI after canonical path and
scope validation.

### `context_pack`

Builds a reproducible bounded capsule for a manager or drafter task.

### `recall_propose`

Returns or records a bounded provisional memory proposal. It cannot accept the
proposal into canonical shared memory. A recorded proposal is written only to
`.re-discipline/memory/proposals/`, never directly to accepted topics.

All read tools carry read-only annotations. Proposal creation is isolated from
memory acceptance. The server exposes no tool that promotes truth, closes a
campaign, accepts canonical memory, or activates a retrieval profile.

MCP resources may expose stable source URIs as an optional host optimization.
Core correctness depends on tools and materialized context packs because host
resource support differs.

## Shared Memory

`.re-discipline/memory/` contains shared operational recall:

- navigation knowledge;
- workflow preferences;
- recurring failure patterns;
- useful commands;
- prior decisions and their durable locations;
- cross-session continuity hints.

It does not hold unverified empirical claims as authority.

Memory proposals may originate from:

- managers;
- drafter report `MEMORY CANDIDATES`;
- `recall_propose`;
- campaign closure or checkpoint review.

`.re-discipline/memory/proposals/` is a tracked provisional queue so proposals
survive compaction and manager changes. It is excluded from normal orientation,
search, and context packs. It is visible only to an explicit proposal-review
query or the manager-only `review-memory` skill.

A manager reviews each proposal for:

- duplication;
- scope;
- sensitivity;
- authority leakage;
- durable source links;
- expiration or re-verification conditions.

`review-memory` presents an accept or reject recommendation and requires user
ratification. Acceptance distills the approved content into
`.re-discipline/memory/topics/`, updates the memory index, and removes the
pending proposal. Rejection removes the pending proposal and records its
disposition in the applicable campaign or memory decision log.

Only accepted topic records participate in ordinary shared-memory retrieval.
No agent may silently teach future managers.

## Subagents And External Drafters

`delegate` replaces manual broad context gathering with `context_pack`.

For every dispatch:

1. The manager creates the chronological drafter workspace.
2. The manager requests a task-specific context pack.
3. The immutable pack is materialized in that workspace.
4. `brief.md` names the pack, its budget, and the allowed evidence boundary.
5. A native spawn message includes the exact brief and pack paths.
6. An external provider receives the same materialized files.
7. The drafter may perform further read-only knowledge queries when supported.
8. The report cites sources and proposes, but does not accept, memory.
9. The manager invokes `review-subagent`.

`SubagentStart` may inject a short generic knowledge reminder and project
identity, but cannot be the sole task-capsule selector because host payloads do
not reliably contain the brief path or task prompt.

The knowledge server does not rely on self-declared role instructions for
write security. Canonical truth and memory promotion remain outside the
drafter-accessible MCP surface.

## Context Preservation

Durable state remains in:

- canonical profile;
- accepted shared memory;
- active `CAMPAIGN.md`;
- reviewed reports;
- tracked truth, history, and backlog.

Session lifecycle:

- SessionStart validates configuration and rehydrates a compact orientation;
- PreCompact invokes campaign checkpoint policy;
- PostCompact rehydrates the active campaign and knowledge handles;
- delegation isolates deep investigation in drafter contexts;
- raw conversation transcripts are not promoted into shared memory.

Compaction preserves decisions, unresolved work, evidence boundaries, and
source handles while dropping spent tool output.

## Local Models

Embeddings and reranking are local by default.

The plugin ships a model manifest containing:

- stable model ID and revision;
- expected files and checksums;
- license metadata;
- embedding dimensions and normalization;
- compatible index/profile versions;
- supported hardware execution backends.

Model artifacts are cached in machine-local user state. They are not committed
to initialized projects.

Changing an embedding model invalidates the associated vector generation.
Changing a reranker invalidates benchmark evidence for affected profiles.

Remote providers are optional, disabled by default, and require an explicit
machine-local grant. A tracked repository may not enable them by itself.

## Evaluation And Calibration

### Three operations

#### Health check

Automatic and cheap. It verifies:

- config and schema validity;
- managed-source allowlists;
- source and index hashes;
- tier filters;
- model availability and checksums;
- index integrity;
- citation resolution;
- token-budget enforcement.

It does not benchmark retrieval quality, tune parameters, or change behavior.

#### Benchmark

`benchmark-knowledge` is read-only and may be run by any user.

Modes:

- `quick`: integrity, exact retrieval, tier policy, citation, and budget
  conformance;
- `full`: lexical, dense, hybrid, reranking, context-pack, and project evals
  for every supported effective profile, including fallback profiles;
- `end-to-end`: selected manager and drafter trials across supported hosts.

The production capability matrix is finite and declared by the accepted
profile. At minimum it contains:

- full hybrid retrieval with dense and reranking lanes;
- hybrid retrieval without the reranking lane;
- the model-free lexical and graph baseline.

Each row has a distinct effective profile identity and independent quality,
policy, budget, and deterministic-replay results. A capability combination
that has not passed its own gates is unsupported and must fail clearly or
select a separately approved baseline; it may not inherit evidence from
another profile.

#### Calibration

`calibrate-knowledge` searches candidate retrieval parameters against ratified
development cases, then evaluates finalists against a frozen holdout. It
produces a candidate profile and report under disposable calibration state.
It never changes the active profile. Running it requires a direct manager or
project-maintainer session because it consumes local compute and creates
candidate state.

`decide-retrieval-profile` promotes, rejects, retains, or rolls back a
candidate. Promotion requires hard gates and explicit user approval.

Permissions are:

| Operation | Who may invoke it |
|---|---|
| health check | automatic bootstrap, hook, or server preflight |
| read-only benchmark | any user |
| project calibration | direct manager or project maintainer |
| project profile decision | direct manager with explicit user approval |
| global calibration and promotion | plugin maintainer in the plugin repository |

### Exact execution points

| Event | Action |
|---|---|
| every SessionStart | health/freshness check only |
| project initialization | index build plus smoke conformance |
| ordinary source change | incremental reindex only |
| incompatible cache | rebuild plus health check |
| installed plugin parser, chunker, model, or schema change | rebuild plus local health/smoke check; every packaged effective profile was benchmarked in plugin CI |
| proposed project profile or project model change | explicit full benchmark of every proposed effective profile before promotion |
| plugin pull request touching retrieval | full packaged capability-matrix benchmark in CI |
| plugin release candidate | global holdout plus deterministic replay for every supported effective profile and selected end-to-end trials |
| user benchmark request | requested read-only mode |
| user calibration request | development sweep, holdout, candidate report |
| profile activation | explicit decision after passing gates |

No expensive benchmark or calibration runs invisibly during ordinary sessions.
If a local compatibility change invalidates prior benchmark evidence, the
active profile falls back safely or is marked stale; `status` recommends the
explicit benchmark required for a new promotion.

### Evaluation corpus

Each case records:

- query;
- manager or drafter role;
- query class;
- allowed tiers and paths;
- corpus snapshot;
- graded relevant passages;
- minimum complete evidence set;
- hard-negative distractors;
- expected authority behavior;
- answerable or unanswerable status;
- expected citations;
- token budget.

Cases come from:

- real manager questions;
- delegation briefs;
- accepted and rejected reports;
- campaign checkpoints;
- truth promotions and overturns;
- repeated search reformulations;
- exact-identifier tests;
- ratified synthetic paraphrases and adversarial distractors.

Data is split by project, campaign, or topic rather than randomly by individual
query. This keeps near-duplicate material out of the holdout.

### Metrics

Ingestion:

- expected files and chunks indexed;
- deletion propagation;
- metadata and tier correctness;
- stale index rate;
- citation line and hash correctness.

Retrieval:

- evidence-set Recall@k;
- MRR for exact targets;
- nDCG for graded results;
- Precision@k;
- exact-identifier hit rate;
- complete multi-source evidence coverage;
- authority-policy violation rate;
- stale-result rate;
- abstention accuracy.

Context packs:

- supporting-evidence recall;
- relevant tokens divided by returned tokens;
- duplicate-token ratio;
- citation precision and recall;
- coverage by token budget.

End-to-end:

- grounded task correctness;
- unsupported-claim rate;
- manager disposition;
- input tokens and tool calls;
- p50 and p95 latency;
- local compute cost.

Hard authority, privacy, citation, and freshness gates are evaluated before
token or latency optimization. Profile selection uses the Pareto frontier
rather than hiding tradeoffs in one score.

### Role of subagents

Subagents do not establish gold labels and are not launched for every parameter
combination.

They may:

- generate candidate paraphrases and hard negatives;
- act as realistic blinded clients for finalists;
- identify ambiguous tools or missing context;
- compare Claude and Codex usage;
- investigate retrieval failures.

The deterministic harness performs the parameter sweep. A manager or user
ratifies expected evidence and ambiguous outcomes. Major global calibration
may use a provisional campaign with `delegate` and `review-subagent`.

### Continuous improvement

Local metrics may identify:

- repeated reformulation;
- sparse/dense disagreement;
- low confidence;
- excessive context;
- frequently opened omitted sources;
- manager rejection.

These signals create proposed evaluation cases. They never alter weights
automatically.

Candidate profiles run in offline replay or shadow mode before promotion.
Every accepted profile records its base profile, corpus and eval fingerprints,
model revisions, benchmark digest, and approval.

## Profile Ownership

### Plugin baseline

The plugin repository owns:

- default routing and ranking profile;
- pinned model specifications;
- generic conformance fixtures;
- cross-project held-out suite.

A global improvement is calibrated and reviewed in the plugin repository,
passes CI and release gates, and ships in a new plugin version.

### Project overlay

An initialized project may own:

- `.re-discipline/settings/retrieval-profile.json`;
- `.re-discipline/knowledge/evals/**`.

The overlay is accepted only through profile promotion, tracked with the
project, and shared by every manager and teammate. Project data does not flow
upstream automatically.

### Machine-local state

Hardware, model-cache, resource, and trust settings remain local. They may
affect performance but may not change production ranking behavior.

## Security And Privacy

- Use local stdio, not an unauthenticated listening port.
- Resolve canonical paths and reject symlink or junction escapes.
- Treat MCP roots as hints, not a security boundary.
- Index explicit source classes, not arbitrary repository content.
- Keep secrets, private paths, environment files, keys, raw evidence, and
  caches excluded by default.
- Treat embeddings as sensitive derivatives of source text.
- Log metrics and hashes by default, not query or passage text.
- Make query-content traces explicit opt-in local state.
- Expose no arbitrary SQL, regex, command, or unrestricted path parameter.
- Require machine-local grants for external roots and remote providers.
- Keep manager-only promotion outside the general MCP surface.

## Concurrency And Failure Recovery

Managers and subagents may run separate stdio server processes against one
completed generation. SQLite provides concurrent readers; one serialized
writer owns generation changes.

Failure behavior:

- missing or corrupt index: quarantine and rebuild;
- unavailable embedding model: select the approved lexical-and-graph effective
  profile and report the fallback;
- mismatched vector model: ignore and regenerate vectors while selecting an
  approved effective profile that does not consume them;
- unavailable reranker: select the approved hybrid-without-reranking effective
  profile and report the fallback;
- writer contention: serve the last complete generation;
- branch change during query: retry or report the pinned snapshot;
- stale selected source: reparse before return;
- read-only worktree: place cache in machine-local storage;
- invalid config: safe read-only degraded mode;
- unavailable hooks: MCP and initializer preflight repeat required checks.

No failure path modifies canonical truth or silently changes a retrieval
profile. Every fallback changes the effective profile identity, preserves the
requested profile identity, and records the active lanes and reason in the
result and context pack. If no independently benchmarked effective profile
matches the available capabilities, retrieval fails clearly instead of
inventing unmeasured ranking behavior.

## Plugin Integration

The plugin root will declare the same local stdio server to Claude Code and
Codex. The Codex manifest points at the shared MCP declaration. The server
receives or discovers the active project root and validates it against the
managed marker.

Hook responsibilities:

- `SessionStart`: config recovery, health status, compact orientation;
- `PreCompact`: checkpoint reminder;
- `PostCompact`: compact rehydration;
- `SubagentStart`: generic drafter and knowledge reminder.

Hooks do not build vectors, run full benchmarks, or inject a growing corpus.

Existing lifecycle integration:

- `init-project`: config, adapters, knowledge topology, smoke verification;
- `onboard`: knowledge status and compact orientation;
- `delegate`: immutable context pack in `brief.md`;
- `review-subagent`: claim review plus memory/eval candidates;
- `checkpoint-campaign`: durable resume state;
- `close-campaign`: distillation and proposed durable recall.

## Authoritative Implementation Source

The feature must be implemented in the actual marketplace source repository:

```text
C:\Users\alexd\adiaz-claude-plugins\
    plugins\re-discipline\
    tests\
    .agents\plugins\marketplace.json
    .claude-plugin\marketplace.json
```

The authoritative plugin implementation is
`plugins/re-discipline/**`, including its manifests, MCP declaration, runtime,
server, schemas, skills, hooks, templates, references, and README.
Repository-level compatibility and integration coverage belongs in `tests/**`.
Marketplace metadata and the Claude/Codex plugin manifests must remain version
aligned.

Implementation must not:

- edit an installed copy under a Claude or Codex plugin cache as the source of
  the feature;
- patch only `snaphak-re` or another initialized project;
- leave source templates, recovery paths, or migration logic behind a
  hand-patched example project;
- declare success from an installed-cache test without verifying the source
  package and a fresh installation.

Initialized projects receive the feature through source-owned templates,
`init-project`, migration/resync behavior, and packaged hooks. Installed copies
are validation targets or generated outputs, not implementation inputs.

## Design Plan Lifecycle

This file is a temporary implementation-control artifact. It must remain
available, current, and reviewable throughout implementation, intermediate
integration, and testing. Passing a slice, a smoke test, or an incomplete
capability set is not permission to remove it.

Before deletion, every durable contract needed to operate and maintain the
feature must exist in the authoritative plugin source as code, schemas,
source-owned templates, skill and hook contracts, README material, settings
documentation, and executable tests. The complete framework means all eight
delivery slices below and every acceptance criterion, including fresh-install,
fresh-initialization, migration, dual-host, subagent, fallback-profile,
benchmark, security, and failure-recovery validation.

Only after the complete end-to-end suite passes against the source package may
the final implementation change delete this plan. If any release criterion is
unimplemented, unverified, skipped, or failing, the plan remains. Its eventual
absence is intentional final cleanup, not initialization recovery and not
evidence that a partial implementation is complete.

## Delivery Strategy

Implementation may land in internally gated slices, but the first public
release of this feature should contain the complete production shape:

1. schema, bootstrap, `shared-only`, and recovery;
2. source adapters, metadata catalog, exact search, FTS5, and graph;
3. local embedding and reranking model runtime;
4. hybrid retrieval, context compiler, and MCP tools;
5. shared-memory proposal and ratification workflow;
6. subagent context-pack integration;
7. benchmark, calibration, profile-decision skills, and seed suites;
8. Claude and Codex packaging, migration, documentation, and tests.

Dense and reranking components may remain shadow-gated until the packaged
benchmark promotes their initial profile. This is an evidence gate inside the
final architecture, not a throwaway implementation.

## Acceptance Criteria

The feature is ready to release when:

- new projects initialize to `shared-only`;
- deleted tracked bootstrap or settings files are safely recovered;
- malformed config is not silently replaced;
- `.re-discipline/settings/README.md` documents every project-facing setting,
  generated file, ownership boundary, and default;
- human-editable policy uses commented `knowledge.jsonc`, while accepted
  retrieval profiles are generated and content-hashed;
- Claude and Codex use the same canonical profile, config, server, and project
  retrieval profile;
- native and external drafters receive equivalent immutable context packs;
- truth, history, active, backlog, and memory remain distinguishable;
- no forbidden tier can satisfy a restricted query;
- all passages carry valid path, line, hash, tier, and generation metadata;
- exact technical identifiers pass the conformance suite;
- hybrid retrieval is non-inferior to lexical fallback on the frozen holdout;
- every supported full and fallback capability set has an independently
  benchmarked, content-hashed effective profile;
- deterministic replay returns identical ranking and packed context for the
  same generation, effective profile, query, and budget under every supported
  capability set;
- every result and context pack reports the requested profile, effective
  profile, active lanes, model identities, and fallback reason;
- context packs obey hard budgets and citation requirements;
- local models are checksum-pinned and work offline;
- missing optional models degrade to useful lexical retrieval;
- no project virtual environment or global runtime is required;
- candidate memory and retrieval profiles cannot self-promote;
- pending memory proposals are excluded from normal retrieval and can be
  accepted or rejected only through `review-memory`;
- all implementation lives in the authoritative
  `adiaz-claude-plugins/plugins/re-discipline/` source with synchronized
  templates, tests, manifests, and marketplace metadata;
- a fresh plugin installation and fresh project initialization reproduce the
  verified behavior without relying on an edited installed cache;
- global, project, and machine-local configuration scopes remain separate;
- existing re-discipline lifecycle and compatibility tests remain green;
- new dual-host integration, migration, recovery, security, benchmark, and
  failure-mode tests pass;
- this plan remains present until all preceding criteria and the complete
  end-to-end suite pass, and is then deleted as the final cleanup step after
  its durable contracts have been transferred into source documentation,
  schemas, and tests.

## Implementation Decisions To Resolve By Measurement

The architecture deliberately does not freeze:

- the packaged runtime and build system;
- the initial local embedding model;
- the initial local reranking model;
- the embedded vector-index implementation;
- chunk-size ranges;
- query-class fusion weights;
- reranking depth and thresholds;
- default manager and drafter token budgets.

The implementation plan must define candidate choices and select them through
portability tests and the approved evaluation process. These choices may
evolve without changing canonical storage, MCP contracts, or governance.

## References

- SQLite FTS5: <https://www.sqlite.org/fts5.html>
- SQLite WAL: <https://www.sqlite.org/wal.html>
- Model Context Protocol: <https://modelcontextprotocol.io/>
- OpenAI Retrieval: <https://developers.openai.com/api/docs/guides/retrieval>
- OpenAI evaluation best practices:
  <https://developers.openai.com/api/docs/guides/evaluation-best-practices>
- Anthropic Contextual Retrieval:
  <https://www.anthropic.com/engineering/contextual-retrieval>
- Anthropic context engineering:
  <https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents>
- BEIR benchmark: <https://arxiv.org/abs/2104.08663>
