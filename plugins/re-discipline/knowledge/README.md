# Re-Discipline Knowledge Runtime

This directory contains the local, manager-neutral state and knowledge engine
shipped with re-discipline 0.8. Claude Code, Codex, direct managers, and
delegated workers use the same executable, canonical record schemas, finding
index, retrieval profiles, and context-pack format.

The runtime is navigation infrastructure. It never makes an index, ranking
score, benchmark, or memory proposal authoritative. Authority remains
with canonical records, immutable review receipts, exact evidence, and
closure-gated projections.

## Runtime shape

- Go provides one static executable without a required system Python, Node.js,
  SQLite, model service, or network connection.
- `modernc.org/sqlite` provides SQLite and FTS5 in process.
- Identifier-aware exact, FTS5, dependency-graph, and checksum-pinned local
  dense lanes share one generation database.
- Explicit allowed-tier filters and source/path denylists run before
  relevance ranking. Drafter role selects narrower packing and budget
  defaults; the manager curates each immutable dispatch pack.
- Pending memory proposals are excluded from ordinary search and context
  packs.

Model specifications live under `models/specs/`. The release candidate carries
one checksum-pinned, deterministically quantized GloVe artifact under
`models/artifacts/`; its provenance, PDDL 1.0 license, format, and reproduction
recipe are documented beside it. The artifact is shared by all packaged
targets, is not copied into initialized projects, and is never downloaded at
runtime. An independently benchmarked lexical-graph profile remains available
if it cannot load. The packaged fixture alone was underpowered for dense
removal; a frozen pre-removal project-corpus run measured two dense-only
rescues with no added hard-gate regression. Dense remains in the candidate
inventory pending a fresh current-runtime two-arm final-corpus run. The
reranker produced no benefit in the packaged holdout or the separately frozen
historical project layer and has been removed; it is not recreated as a
synthetic current-runtime arm.

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
public surface is exactly ten role-oriented operations:

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
| `normalization_queue` | Inspect durable archive-normalization demand; create an exact path/digest/byte-bound manager request; or claim, acknowledge, and resolve one source-bound item with a verified curator-run, intake-coverage, and complete-review receipt. |
| `migrate_project` | Preview, review, apply, resume, inspect, verify, and ratify the explicit 0.7-to-0.8 conversion. It is the sole legacy-state reader. |

An explicit `projectRoot` must be a validated re-discipline 0.8 project. When
the host supplies roots, requests are restricted to those roots. One server
process can cache services for multiple validated roots without merging their
indexes or policies.

The four read operations never change canonical sources, settings, accepted
memory, or retrieval profiles. A first query may reconcile only disposable
derived index and aggregate-metrics files under the approved cache root;
deleting those files never deletes project knowledge. Managed recovery happens
before MCP handling only when the launch configuration or MCP roots grant a
project. Every state-changing operation validates its authority, expected
revisions or source digests, idempotency, and path grants in the shared engine
before publication.

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
| `state`, `query`, `read`, `trace` | Invoke the same typed read operation as MCP from strict JSON supplied with `--input <path-or->`. `query --session` accepts a stream of JSON requests in one process. |
| `context-pack-materialize`, `manager-apply`, `curation-submit`, `closure-apply`, `normalization-queue` | Invoke the same typed project operation as MCP from strict JSON. |
| `recover`, `ensure` | Restore current managed bootstrap files, or report that a legacy project requires explicit migration. |
| `migrate-project` | Preview; export and submit digest-bound non-activating retrieval-profile decisions and truth atomicization reviews; apply, resume, inspect, verify, record strict retrieval/host evidence gates, and ratify the sole 0.7-to-0.8 conversion path. |
| `status`, `preflight`, `index`, `replay` | Maintainer diagnostics for configuration, derived-index health, and deterministic passage retrieval. These are not MCP aliases. |
| `verify-pack` | Verify an independently retained pack digest and optional pack ID before dispatch. |
| `benchmark`, `pin-evals`, `normalized-vs-raw`, `calibrate`, `promote-profile` | Measure retrieval, manage eval pins, emit a generation-pinned non-authoritative 64-case normalized-versus-raw candidate, create calibration proposals, and apply an explicitly approved profile decision. |

Use `--asset-root` to identify this `knowledge/` directory and
`--project-root` for the initialized project. Project-facing token and source policy remains in
`.re-discipline/knowledge/policy.jsonc`.

Query requests may include a caller-supplied `contextLeaseId`. With the
default `context.leaseMode` of `memory-only`, the serving process suppresses
cards already served by that lease and returns a bounded deterministic receipt
containing current source digests plus cumulative token, source-count, and
source-set-digest accounting. `resetContextLease: true` clears only that
process-local lease after real context compaction; it never mutates campaign
state. A one-shot CLI query intentionally starts a fresh lease ledger. For
lease-aware fallback without MCP, invoke `query --session --input <path-or->`;
the input is a whitespace-delimited stream of strict query JSON objects and the
output is one compact JSON response per line. Every request in that invocation
shares one in-memory lease ledger. Each response is emitted before the runtime
waits for the next request; stdin does not need to close first. Session input is
stream-decoded under a 16 MiB cumulative bound. Leases do not survive the
session process exit or MCP server restart.

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

`run.prepare` takes the raw brief text and the previewed pack under
`runPreparation`. The submitted run record's `brief` and `contextPack` handles
are optional: their digests cover engine-canonical bytes - the brief gains an
engine-sealed write-grant block and the pack is re-serialized by the runtime -
so the engine derives both when they are omitted. Supplied handles are still
compared byte for byte, which keeps compare-and-swap available to an
independent verifier that computes the same canonical bytes.

A pack refuses to compile when its mandatory scoped context - the campaign
objective, scope, exclusions, success and closure criteria, and the work-item
problem and acceptance criteria - cannot fit the requested budget. The refusal
names each contributing constraint and card with its token cost and the
minimum budget that would work. Because that context is never droppable, the
combined size of those fields is also capped per record at write time, so a
manager transition that would introduce or grow a record too large to delegate
is refused instead of leaving every later run to fail.

`review.submit` and `decision.record` require `reviewPacket.intake` to be the
committed intake. The comparison is over the canonical persisted form of both
records, not over decoded in-memory values, because the two are not the same
shape: sealing rewrites empty collections to non-nil slices that `omitempty`
then removes from the file, so an absent collection and an empty one are the
same record. Everything else - revision, status, digest, coverage, candidates,
and every text field - must match exactly, and a mismatch names each differing
field with both values.

`run.complete` refuses `completed` and `blocked` for a returned run whose
report has no clean curator intake. `returned` is the state in which
`curation_submit` accepts an intake for a run unconditionally, and no
transition leads back to it, while closure requires a reviewed intake for every
non-aborted run with a report and disregards any source that still carries an
`unresolved` span. Completing early would therefore fix an unsatisfiable
closure gate into the campaign, so the refusal happens at the transition that
can still be repaired and names the run, the unresolved spans, and the
dispositions that clear them. `invalidated` remains available as the manager's
explicit void path, because it is the only other exit from `returned` and
refusing it too would leave a dirty run with no exit at all.

Invalidation withdraws the run, not the evidence it already produced. The
report handle stays frozen, the report stays in the closure plan's retained
payload, and a finding ratified out of it stays ratified: findings bind their
source runs by id, the campaign graph requires only that each one resolve, and
nothing cascades from run invalidation to a finding's review state, validity,
or projection. Closure therefore keeps requiring a reviewed intake for an
invalidated run's report; exempting it would let a campaign close with a
projected truth resting on evidence the coverage gate no longer accounts for.

Closure's two exemptions turn on the report, not on the status. `aborted`
carries no report at all, and neither does a run invalidated before it ever
returned - `run.complete` accepts `invalidated` from `prepared` and `running`
so a manager can take a run that will never come back out of flight, and only
`returned`, `completed`, and `blocked` require a report handle. Neither run can
be cited by anything: an intake source resolves to a run only by matching a
frozen report handle, and a candidate finding's `sourceRuns` must equal exactly
the runs its coverage spans resolve to, so a run that froze no bytes can carry
no span and back no claim. Closure records them as `aborted-with-reason` and
`invalidated-without-report` and asks for nothing further. Demanding a report
from the second was unsatisfiable: `invalidated` is absorbing, terminal run
records are immutable, and the stranded-run curation below needs a frozen
report to curate against, so the campaign could never close. A run that is
still `prepared` or `running` is a different case and still reports
`missing-report`: closure must not step over work in flight, and the remedy is
to return the run, or to end it as `aborted` with a reason.

`aborted` is the more accurate record of a run that never came back, and it is
reachable from both `prepared` and `running`: it carries the reason in
`resultSummary`, where `invalidated` carries the id of the run that supersedes
it. Both are exempt, so neither strands a campaign; the engine names the better
one when a curator tries to curate such a run, which is where the question is
actually asked.

`curation_submit` also accepts a source run that is already `completed` or
`invalidated`, but only while that run is stranded: only when no non-superseded
intake covers its report with every span disposed. For `completed` this is the
exact complement of the `run.complete` guard above - no run can satisfy both -
and it exists for campaigns stranded before that guard did. `invalidated` has
no such guard to complement, since that is the one exit `run.complete` must
keep accepting, so this is the standing repair for a run voided over an
unresolved span. A run whose report already carries a clean intake is refused
in either state, whether that intake is reviewed (nothing to repair; use
`overturn` if a conclusion is wrong) or still pending review (review it rather
than adding a second repair). Every other run state is refused, and each
refusal names the run, its state, and the remedy.

The supplementary intake is purely additive. Reviewed report coverage is a
union across reviewed intakes with unresolved spans computed per intake, so a
second clean intake moves the run to `reviewed-intake` without altering,
superseding, or invalidating the review already ratified over the dirty one.
That second intake carries its own manager review, so the added coverage is
ratified rather than asserted, it does not restore a run it covers, and
retiring a prior conclusion is still `overturn`'s job.

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
- A missing or invalid local embedding selects only the independently
  benchmarked lexical-graph capability row. The runtime never downloads a
  model.
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
Both the local-dense and lexical-graph effective profiles have checked-in
benchmark evidence. Full mode checks their declared digests, hard gates,
authority and citation safety, deterministic search and context-pack replay,
manager and drafter ceilings, the 512/1024/2048/4096 budget matrix, and
development/holdout separation. A separate checked-in ablation decision spans
two evidence layers. The packaged fixture layer was underpowered and found no
positive contribution. The project protocol uses a fresh current-runtime
64-case baseline-versus-dense run for the dense decision and an immutable
pre-removal three-arm archive for the rerank decision. Those runtime layers
remain separate; only aggregate sensitivity compares them. Each report is
bound to its source revision, eval corpus, indexed corpus, runtime, parser,
chunker, controlled profiles, and exact model revisions and checksums. The
packager independently recomputes current dense and historical rerank
contribution totals from the full per-case measurement, verifies the archived
raw benchmark bytes and manifests by digest, and requires both lane decisions,
effective profiles, and the model manifest to agree.
Authority, privacy, freshness, citation, replay, and hard-budget gates apply at
every tested budget. Quality coverage becomes a release gate at each case's
declared budget and above; lower-budget degradation remains measured rather
than being mislabeled as a policy failure.

Finding-card evaluation compares the estimated tokens of the bounded,
serialized response returned by each arm. Full raw-document expansion cost and
bounded normalized evidence-handle expansion cost are reported separately as
diagnostics and never mixed into that release-gate comparison.

Benchmark digests bind the portable source runtime contract: runtime and Go
versions, SQLite driver and version, a source-contract checksum, numerical
backend, and tie breaker. Platform-specific executable and SQLite
compile-option checksums remain visible in runtime/generation provenance but
do not make the same evidence receipt differ across operating systems.

This small packaged seed suite has 48 finding cases, including 24 holdout
cases. It establishes deterministic conformance and fixture non-inferiority.
It does not represent the 64-case `snaphak-re` project corpus and does not
establish general retrieval quality, large-corpus latency, or optimal
project-specific weights; projects grow that evidence through ratified eval
cases, read-only benchmarks, and explicit calibration.

Run the release gate from this directory:

```powershell
go run ./cmd/re-discipline-knowledge benchmark --asset-root . --mode full
```

Omitting `--project-root` selects the immutable packaged fixture and prints
its report. Supplying an initialized `--project-root` selects that project's
ratified eval cases and writes the report only under its disposable knowledge
cache.

The checked-in packaged lane-ablation report binds its source revision, eval
and fixture corpora, controlled profiles, runtime, and every model input used
for that measurement. The separate project measurement binds a fresh
current-runtime two-arm run and the fixed-metadata historical rerank archive;
reproduction requires those exact inputs. Measure an initialized project
against an explicit disposable cache and project path:

```powershell
go run ./cmd/re-discipline-knowledge benchmark `
  --asset-root . `
  --project-root C:\path\to\initialized-project `
  --cache-root C:\path\to\disposable-knowledge-cache `
  --mode full
```

The report records the source revision, controlled profile digests, and exact
corpus/runtime identities required to audit the run. Model-bearing runs also
record model, specification, and artifact digests. Do not run an historical
runtime against a project's live cache.

For the release measurement, do not point that command at the source project.
From a clean plugin revision, stage the current two-arm run in a directory
outside both repositories. The harness rejects a dirty or mismatched revision,
clones the project without hardlinks, preserves `active/`, `docs/`, and
`.re-discipline/` outside the disposable project, applies only the three
current-runtime control files, projects the 64 final eval cases byte-for-byte,
and refuses any pre-existing or newly created 0.8 migration roots:

```powershell
$pluginRevision = git -C C:\path\to\adiaz-claude-plugins rev-parse HEAD
$projectRevision = git -C C:\path\to\snaphak-re rev-parse HEAD

python tests/re_discipline_project_lane_ablation_stage.py `
  --plugin-repository C:\path\to\adiaz-claude-plugins `
  --plugin-revision $pluginRevision `
  --project-repository C:\path\to\snaphak-re `
  --project-revision $projectRevision `
  --output-root C:\path\outside-both-repos\snaphak-re-retrieval-stage

python tests/re_discipline_project_lane_ablation_stage.py `
  --plugin-repository C:\path\to\adiaz-claude-plugins `
  --plugin-revision $pluginRevision `
  --project-repository C:\path\to\snaphak-re `
  --project-revision $projectRevision `
  --output-root C:\path\outside-both-repos\snaphak-re-retrieval-stage `
  --verify
```

Verification is read-only. It rechecks both clean revisions, every artifact
size and digest, the final-to-projected eval transform, the cache report and
leased generation, the SQLite document inventory, and every indexed source
against the byte-exact preserved checkout. The generated
`artifacts/harness-receipt.json` is schema validated and binds the semantic Go
command, tool versions, runtime identity, profile/model catalogs, corpus and
eval fingerprints, and the complete 2 x 64 budget/context-pack matrix. It
rejects byte drift among the packaged, project-template, and
migration-template profile catalogs. It does not create
`.re-discipline/state`, `.re-discipline/transactions`, or
`docs/history/campaigns`, and it does not rebuild packaged binaries.

The rerank evidence archive is a byte-preserving record of the pre-removal
runtime, not a substitute benchmark arm. From the repository root, rebuild and
then verify it with the exact historical inputs materialized from revision
`215964342378678eaba3249fe0ae284c3a0622a4`:

```powershell
python tests/re_discipline_project_lane_ablation_archive.py `
  --raw-benchmark <pre-removal-raw-benchmark.json> `
  --projection-manifest <pre-removal-projection.json> `
  --projected-eval-root <pre-removal-projected-evals> `
  --profile-catalog <pre-removal-profile-catalog.json> `
  --model-manifest <pre-removal-model-manifest.json> `
  --output plugins/re-discipline/knowledge/evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip

python tests/re_discipline_project_lane_ablation_archive.py `
  --raw-benchmark <pre-removal-raw-benchmark.json> `
  --projection-manifest <pre-removal-projection.json> `
  --projected-eval-root <pre-removal-projected-evals> `
  --profile-catalog <pre-removal-profile-catalog.json> `
  --model-manifest <pre-removal-model-manifest.json> `
  --output plugins/re-discipline/knowledge/evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip `
  --verify
```

The canonical archive is 456,281 bytes with SHA-256
`c20ed97f01a324397eedad89f2c628a28cad94d562595f6f342a8efdae5c9ac6`.
After the verified staging run, derive and independently verify the full
project receipt directly from its bound artifacts; both revision arguments
must name the source trees that actually produced their respective benchmark
layers:

```powershell
python tests/re_discipline_project_lane_ablation_build.py `
  --raw-benchmark C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\current-two-arm-raw.json `
  --projection-manifest C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\current-projection.json `
  --final-eval-root C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\final-evals `
  --projected-eval-root C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\projected-evals `
  --profile-catalog C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\current-profile-catalog.json `
  --model-manifest C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\current-model-manifest.json `
  --harness-receipt C:\path\outside-both-repos\snaphak-re-retrieval-stage\artifacts\harness-receipt.json `
  --historical-evidence-archive plugins/re-discipline/knowledge/evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip `
  --historical-evidence-path evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip `
  --production-profile plugins/re-discipline/knowledge/profiles/balanced-v1.json `
  --schema plugins/re-discipline/knowledge/schemas/project-lane-ablation-report.schema.json `
  --validator tests/re_discipline_project_lane_ablation.py `
  --runtime-source-revision $pluginRevision `
  --historical-runtime-source-revision 215964342378678eaba3249fe0ae284c3a0622a4 `
  --output plugins/re-discipline/knowledge/evals/conformance/project-lane-ablation.json

python tests/re_discipline_project_lane_ablation_build.py <the-same-arguments> --verify
```

Promote that validated receipt into the packaged aggregate report and
digest-bound decision, then verify both byte-for-byte:

```powershell
python tests/re_discipline_project_lane_ablation_promote.py `
  --measurement plugins/re-discipline/knowledge/evals/conformance/project-lane-ablation.json `
  --project-schema plugins/re-discipline/knowledge/schemas/project-lane-ablation-report.schema.json `
  --aggregate-schema plugins/re-discipline/knowledge/schemas/lane-ablation-report.schema.json `
  --report plugins/re-discipline/knowledge/evals/conformance/lane-ablation-report.json `
  --finding-cases plugins/re-discipline/knowledge/evals/conformance/finding-cases.json `
  --decision-output plugins/re-discipline/knowledge/evals/conformance/lane-ablation-decision.json

python tests/re_discipline_project_lane_ablation_promote.py <the-same-arguments> --verify
```

Promotion preserves the packaged conformance layer, recomputes its holdout
counts, and derives every project count, rescue row, uncertainty projection,
lane action, measurement digest, and report digest. It refuses an inconclusive
measurement or a packaged report from a runtime other than the historical
archive revision.

`normalized-vs-raw` is a separate Amendment 3 diagnostic. It requires one
ratified 64-case finding suite, leases one fresh generation, and evaluates the
normalized and raw arms with identical questions, filters, card limits, and
token budgets. It writes the full paired report only below the disposable
cache. Passing every recall, abstention, handle, provenance, durability,
hard-negative, replay, and lower-token-cost gate makes a later explicit opt-in
decision eligible; it never changes the default raw-fallback policy or emits
an authorization receipt itself.

The eligible candidate is applied only through the manager operation
`knowledge.archive-fallback.opt-in`. Its strict request names the candidate
run, semantic and content digests, an explicit UTC `ratifiedAt`, and the
expected current settings digest. The runtime resolves the candidate from its
derived run path, reloads the bound ratified suite, recomputes every case,
split, role, contract, and aggregate gate, and refuses stale generation,
profile, lane, or policy bindings. Success journals three artifacts as one
compare-and-swap transition: the exact durable report under
`.re-discipline/knowledge/measurements/normalized-vs-raw/`, the versioned
receipt under `.re-discipline/knowledge/receipts/`, and the amended policy.
Identical replay is reconstructed from those durable artifacts even after the
candidate cache is pruned; altered replay or any tampered artifact is rejected.

Benchmarking is read-only measurement. Calibration sweeps development cases,
evaluates only finalists on frozen holdout topics, and writes a non-activating
candidate. This release searches exactly the 27 combinations in the 3-by-3-by-3
grid for exact, FTS, and graph reciprocal-rank-fusion weights. It does not tune
trust filters, candidate counts,
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
  `models/`, `evals/conformance/`, and `schemas/`; the model root contains the
  checksum-pinned embedding manifest, specification, artifact, and provenance
  documentation selected by the final lane receipt;
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
3. Full packaged conformance benchmark and digest gates, including agreement
   among the final project lane measurement, explicit lane decisions,
   effective profiles, and model inventory.
4. Packager generation and reproducibility verification.
5. A clean Git diff after regenerating `knowledge/bin/`, including executable
   modes on POSIX runners.
6. Native packaged launcher/MCP protocol tests from relocated Claude- and
   Codex-like install directories.
7. Strict Claude and Codex plugin-manifest validation.

The repository workflow performs these checks without model calls, network
model downloads, or access to a developer's native memory stores.
