# Re-Discipline Knowledge Runtime

This directory contains the local, manager-neutral state and knowledge engine
shipped with re-discipline 0.8.0. Claude Code, Codex, direct managers, and
delegated workers use the same executable, canonical record schemas, finding
index, retrieval profiles, and context-pack format.

The runtime is navigation infrastructure. It never makes an index, embedding,
reranker score, benchmark, or memory proposal authoritative. Authority remains
with canonical records, immutable review receipts, exact evidence, and
closure-gated projections.

## Runtime shape

- Go provides one static executable without a required system Python, Node.js,
  SQLite, model service, or network connection.
- `modernc.org/sqlite` provides SQLite and FTS5 in process.
- Exact, FTS5, dependency-graph, local learned-vector, and deterministic
  integer reranking lanes share one generation database.
- Explicit allowed-tier filters and source/path denylists run before
  relevance ranking. Drafter role selects narrower packing and budget
  defaults; the manager curates each immutable dispatch pack.
- Pending memory proposals are excluded from ordinary search and context
  packs.

Model and reranker specifications live under `models/specs/`. The release
ships one checksum-pinned, deterministically quantized GloVe artifact under
`models/artifacts/`; its provenance, PDDL 1.0 license, format, and reproduction
recipe are documented beside it. The one shared artifact is not copied into
initialized projects or any of the six platform directories. Lexical and
graph retrieval remains the independently benchmarked model-free fallback.

## Packaged launchers and platforms

`bin/re-discipline-knowledge` is the canonical extensionless command used by
both manager adapters.

On Linux and macOS it is a small POSIX launcher that selects one of:

- `bin/linux-amd64/re-discipline-knowledge`
- `bin/linux-arm64/re-discipline-knowledge`
- `bin/darwin-amd64/re-discipline-knowledge`
- `bin/darwin-arm64/re-discipline-knowledge`

On Windows, normal executable lookup resolves the same extensionless command
to `bin/re-discipline-knowledge.exe`. That small amd64 compatibility
dispatcher examines `PROCESSOR_ARCHITECTURE` and
`PROCESSOR_ARCHITEW6432`, then starts one of:

- `bin/windows-amd64/re-discipline-knowledge.exe`
- `bin/windows-arm64/re-discipline-knowledge.exe`

Windows on ARM therefore uses the standard x64 compatibility layer only for
the small dispatcher; the knowledge runtime itself runs as native arm64.
Every payload remains directly invokable for diagnostics.

The package is self-relocatable. Launchers resolve payloads relative to their
own location, never to the source checkout or a developer-specific absolute
path.

## MCP adapters

Claude Code reads the plugin-root `.mcp.json`. Its file format is a direct
server-name map, and its command and asset arguments use
`${CLAUDE_PLUGIN_ROOT}` because Claude performs that substitution.

Codex reads the inline `mcpServers` object in
`.codex-plugin/plugin.json`. It uses a plugin-root-relative command with
`cwd` set to the plugin root. Codex is not expected to expand Claude's
placeholder.

Both declarations start:

```text
knowledge/bin/re-discipline-knowledge serve --asset-root knowledge
```

with the path form required by that host. The server uses newline-delimited
JSON-RPC over stdio and supports MCP initialization, roots, ping, tool
discovery, cancellation notifications, and structured tool results. Its
public surface is exactly eight role-oriented operations:

| Tool | Effect |
|---|---|
| `state` | Return bounded `orient`, `resume`, `work`, `delta`, or `closure` views from canonical records. |
| `query` | Return normalized finding cards after source-class, review-state, validity, and budget filters. |
| `read` | Expand one exact record, finding, evidence, report, path, chunk, or URI handle. |
| `trace` | Follow evidence, dependency, contradiction, supersession, run, and projection edges. |
| `context_pack_materialize` | Preview an active-run or recruiting-run pack; after digest verification, directly publish recruiting packs only. Active-run publication belongs to `manager_apply` `run.prepare`. |
| `manager_apply` | Apply typed campaign, work-item, run, review, finding, and decision transitions. |
| `curation_submit` | Submit a curator intake batch and complete report-span coverage. |
| `closure_apply` | Start, advance, verify, reopen, or finalize a resumable closure job. |

An explicit `projectRoot` must be a validated re-discipline 0.8 project. When
the host supplies roots, requests are restricted to those roots. One server
process can cache services for multiple validated roots without merging their
indexes or policies.

The four read operations never change canonical sources, settings, accepted
memory, or retrieval profiles. A first query may reconcile only disposable
derived index and aggregate-metrics files under the approved cache root;
deleting those files never deletes project knowledge. Managed recovery happens
before MCP handling only when the launch configuration or MCP roots grant a
project. The four mutation operations validate authority, expected revisions,
idempotency, and path grants in the shared engine before publication.

## Command-line interface

From the plugin root, run commands through the canonical packaged launcher:

```powershell
knowledge/bin/re-discipline-knowledge <command> [options]
```

From this `knowledge/` directory, the same launcher is
`bin/re-discipline-knowledge`.

| Command | Purpose |
|---|---|
| `serve` | Start the MCP stdio server. |
| `state`, `query`, `read`, `trace` | Invoke the same typed read operation as MCP from strict JSON supplied with `--input <path-or->`. |
| `context-pack-materialize`, `manager-apply`, `curation-submit`, `closure-apply` | Invoke the same typed mutation operation as MCP from strict JSON. |
| `recover`, `ensure` | Restore current managed bootstrap files, or report that a legacy project requires explicit migration. |
| `migrate-project` | Preview, apply, resume, inspect, verify, gate, and ratify the sole 0.7-to-0.8 conversion path. |
| `status`, `preflight`, `index`, `replay` | Maintainer diagnostics for configuration, derived-index health, and deterministic passage retrieval. These are not MCP aliases. |
| `verify-pack` | Verify an independently retained pack digest and optional pack ID before dispatch. |
| `benchmark`, `pin-evals`, `calibrate`, `promote-profile` | Measure retrieval, manage eval pins, create calibration proposals, and apply an explicitly approved profile decision. |

Use `--asset-root` to identify this `knowledge/` directory and
`--project-root` for the initialized project. `--disable-dense` and
`--disable-rerank` exercise the independently benchmarked fallback profiles.
Project-facing token and source policy remains in
`.re-discipline/knowledge/policy.jsonc`.

Canonical state changes flow only through typed peer operations or the
explicit migrator. Recovery, index reconciliation, calibration, and benchmark
report output have separately documented scopes. Read operations may populate
only disposable derived cache as described above.

Every public context-pack request names either an `active-run` target
(`campaignId`, `workItemId`, and `runId`) or a `recruiting-run` target
(`candidateSlug` and `recruitingRunId`). First call
`context-pack-materialize` with action `preview` and retain the returned
`digest` outside the run workspace. An active-run pack is published only by
the atomic `manager_apply` `run.prepare` transition; direct materialization is
rejected. A recruiting-run preview may be published by calling
`context-pack-materialize` with action `materialize` and the retained digest.
The runtime derives the canonical output from the target, so callers never
supply a path. Publication fails if the state head, bound run identities,
corpus, generation, profile, task, role, tiers, budget, required paths, or
compiled pack changed between operations. Before launch, run
`verify-pack --input <context-pack.json> --expected-digest <retained-digest>`.
Reading a digest from the pack being verified is not independent verification.

## Cache and context preservation

The preferred cache is
`.re-discipline/cache/knowledge/` in the current worktree. When the project is
read-only, the runtime falls back to a deterministic machine-local cache under
the operating-system cache root, keyed by the canonical worktree path. An
explicit cache path is accepted only within one of those two granted roots.

Each immutable generation records:

- corpus, parser, chunker, model, runtime, Git revision, and dirty-worktree
  fingerprints;
- the exact runtime version, Go version, compiled build ID, executable
  checksum, SQLite build, numerical backend, and tie-breaking contract;
- source path, line range, content hash, source class, graph edges, vectors,
  and search indexes.

CLI `status` is read-only. First retrieval or explicit `index` reconciles a
stale generation. One writer publishes a complete SQLite generation and then
atomically switches `current.json`; concurrent readers use the last verified
generation. Search uses bounded database candidate queries rather than
loading the corpus into manager context. A context pack returns only accepted
scoped constraints, bounded context cards, exact expansion handles,
requested/effective profile, active lanes, selected model identities, compact
generation/runtime identity, omission counts, and hard-budget metadata needed
by a manager or worker. Full model
and runtime catalogs remain available through CLI `status`. The compact runtime
fingerprint commits the generation's full runtime identity; the verbose
compiled build ID and executable checksum are not repeated in every context
pack, which keeps small token budgets independent of packaging-label length.

Cache databases, traces, calibration candidates, and benchmark reports are
disposable. Deleting the cache never deletes project knowledge.

## Failure behavior

- Missing or malformed bootstrap/knowledge configuration is visible in
  `state(mode="orient")` and CLI `status`; unsafe configuration fails closed.
  Existing malformed files are preserved byte-for-byte.
- A valid configuration with an invalid, unsafe, or unratified project
  retrieval-profile overlay uses the packaged profile and reports the
  warning. It never treats that overlay as accepted.
- `knowledge.enabled: false` leaves state and CLI status available but prevents indexing and
  retrieval.
- Missing local models select only a separately benchmarked capability row.
  The runtime never downloads a model.
- A corrupt or identity-mismatched generation is not trusted. Reconciliation
  builds and verifies a replacement; writer contention can serve only the last
  verified complete generation and reports that it may be stale.
- Source content is rechecked before a result is returned. Changed, missing,
  denied, symlink-escaped, or out-of-root sources are omitted or rejected.
- Secret-like files, pending memory, and unmanaged paths do not enter ordinary
  retrieval.
- Context-pack materialization derives a canonical destination from the scoped
  target, rejects caller-supplied paths, and refuses to overwrite a different
  immutable pack. Active-run publication is part of `run.prepare`, not a
  standalone write.
- No command reads or writes Claude or Codex native memory stores. Project
  recovery changes only the initialized project's `.claude/settings.json` and
  `.codex/config.toml` managed memory fields.

## Evaluation and calibration

The packaged conformance corpus and cases are under `evals/conformance/`.
Every effective capability row has independent benchmark evidence. Full mode
checks declared digests, hard gates, authority and citation safety,
deterministic search and context-pack replay, manager and drafter ceilings,
the 512/1024/2048/4096 budget matrix, development/holdout separation, and the
hybrid-versus-lexical holdout comparison. Authority, privacy, freshness,
citation, replay, and hard-budget gates apply at every tested budget. Quality
coverage becomes a release gate at each case's declared budget and above;
lower-budget degradation remains measured rather than being mislabeled as a
policy failure.

Benchmark digests bind the portable source runtime contract: runtime and Go
versions, SQLite driver and version, a source-contract checksum, numerical
backend, and tie breaker. Platform-specific executable and SQLite
compile-option checksums remain visible in runtime/generation provenance but
do not make the same evidence receipt differ across operating systems.

This small packaged seed suite establishes deterministic conformance and
fixture non-inferiority. It does not establish general retrieval quality,
large-corpus latency, or optimal project-specific weights; projects grow that
evidence through ratified eval cases, read-only benchmarks, and explicit
calibration.

Run the release gate from this directory:

```powershell
go run ./cmd/re-discipline-knowledge benchmark --asset-root . --mode full
```

Omitting `--project-root` selects the immutable packaged fixture and prints
its report. Supplying an initialized `--project-root` selects that project's
ratified eval cases and writes the report only under its disposable knowledge
cache.

Benchmarking is read-only measurement. Calibration sweeps development cases,
evaluates only finalists on frozen holdout topics, and writes a non-activating
candidate. This release searches exactly the 81 combinations in the
3-by-3-by-3-by-3 grid for exact, FTS, graph, and dense reciprocal-rank-fusion
weights. It does not tune trust filters, candidate counts, reranker depth,
packing rules, or token budgets. An explicit user decision through the
profile-decision workflow is required before an accepted project profile can
change.

## Reproducible build and package

The module's `go` directive pins the release compiler. The packager invokes
that exact toolchain with `CGO_ENABLED=0`, module-readonly mode, `-trimpath`,
`-buildvcs=false`, stripped symbols, an empty Go build ID, and a deterministic
compiled build identity derived from runtime source, module locks, every shared
runtime asset, and the distributed license notices.

From this directory:

```powershell
go run ./cmd/re-discipline-knowledge-packager --output bin
go run ./cmd/re-discipline-knowledge-packager --output bin --verify
```

The first command materializes all six targets into a staging directory and
atomically replaces `bin/` only after the whole package succeeds. It emits:

- `bin/manifest.json`, with ordered targets, runtime/build identity, sizes,
  modes, SHA-256 values, and every shared runtime asset under `profiles/`,
  `models/`, `evals/conformance/`, and `schemas/`;
- `bin/SHA256SUMS`, covering every payload, launcher, the notice file, the
  manifest, and all shared runtime assets without copying them;
- `bin/THIRD_PARTY_NOTICES.md`.

The Windows runtime payloads and architecture dispatcher are canonicalized by
a Windows-hosted build. Windows packagers rebuild all three PE files from
source; Linux and macOS packagers copy the checked-in files byte-for-byte and
verify each target, pinned Go toolchain, and runtime build identity. Windows
artifacts use mode `0644` on POSIX hosts because they are not native
executables there; this also preserves their checked-in Git modes. The
Windows CI leg therefore enforces source freshness while every host can
require one identical release package without accepting host-dependent PE
output. A non-Windows source-only checkout must first generate
`knowledge/bin` on Windows.

The checksum rows for shared runtime assets use parent-relative paths such as
`../profiles/balanced-v1.json` because `SHA256SUMS` itself lives in `bin/`.
Only files under the four declared runtime-asset roots can appear there.

`--verify` does not modify `bin/`. It strictly parses the manifest, rejects
missing, extra, duplicate, symlinked, checksum-mismatched, size-mismatched, or
mode-mismatched files and shared assets, rebuilds the package in a temporary
directory, and requires byte-identical output.

Git stores POSIX executable permission in its index rather than in
`.gitattributes`. When an authorized release commit is prepared on Windows,
stage only the POSIX launcher and four POSIX runtime payloads with
`git add --chmod=+x`; the cross-platform gate rejects a release tree that loses
those `100755` modes.

## Release gate

A release is ready only when all of the following pass:

1. Python compatibility and contract tests on Windows, Linux, and macOS.
2. `gofmt` cleanliness, module checksum verification, `go vet`, Go unit tests
   on all three systems, the race detector, and vulnerability analysis on
   Linux.
3. Full packaged conformance benchmark and digest gates.
4. Packager generation and reproducibility verification.
5. A clean Git diff after regenerating `knowledge/bin/`, including executable
   modes on POSIX runners.
6. Native packaged launcher/MCP protocol tests from relocated Claude- and
   Codex-like install directories.
7. Strict Claude and Codex plugin-manifest validation.

The repository workflow performs these checks without model calls, network
model downloads, or access to a developer's native memory stores.
