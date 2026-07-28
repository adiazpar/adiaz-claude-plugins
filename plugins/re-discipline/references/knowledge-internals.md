# Knowledge System Internals (Agent Reference)

This reference documents the machine-managed knowledge control files under
a project's `.re-discipline/` tree. It is agent documentation: users never
need it. The tree does not contain server code, packaged model files,
indexes, benchmark output, or machine-local security state, and none of it
is narrated to users (see `reporting.md`).

## Files

| File | Owner | Edit policy |
|---|---|---|
| `.re-discipline/config.json` | re-discipline bootstrap | Small strict-JSON safety policy. Change memory mode through `init-project` resync so both host settings are updated together. |
| `knowledge/policy.jsonc` | project maintainer | Human-editable source, local-execution, telemetry, and context-budget policy. Comments are allowed. |
| `knowledge/retrieval-profile.json` | retrieval-profile governance | Generated, content-hashed production ranking profile. Do not hand-edit it; use `calibrate-knowledge` and `decide-retrieval-profile`. |

The schemas live in the installed plugin under `knowledge/schemas/`. The
`plugin://re-discipline/...` schema identifiers are stable package IDs; they
do not grant network access.

## Defaults

- New projects use `memory.mode: "shared-only"`.
- Shared-memory writes are proposal-only.
- Embeddings and reranking execute locally.
- Retrieval uses `plugin:balanced-v1`.
- Query and passage content telemetry is off; only aggregate local metrics are
  enabled.
- Tracked settings cannot authorize remote providers or external source roots.

### `config.json`

| Field | Default | Meaning |
|---|---|---|
| `schemaVersion` | `2` | Strict bootstrap schema. A newer value is never downgraded automatically. |
| `knowledgeDirectory` | `"knowledge"` | Fixed directory relative to `.re-discipline/`; traversal and absolute paths are rejected. |
| `memory.mode` | `"shared-only"` | `shared-only`, `hybrid`, or `native`; resync both host settings after changing it. |
| `memory.writePolicy` | `"proposal-only"` | Agents may propose recall but cannot directly accept shared memory. |
| `knowledge.enabled` | `true` | Enables the project knowledge server and preflight. |
| `knowledge.profile` | `"plugin:balanced-v1"` | Requested packaged production profile. |
| `knowledge.settingsFile` | `"knowledge/policy.jsonc"` | Fixed AI-curated policy path; users change behavior by asking the agent. |
| `knowledge.projectProfile` | `"knowledge/retrieval-profile.json"` | Fixed generated/accepted profile path. |

### `knowledge/policy.jsonc`

| Field | Default | Meaning |
|---|---|---|
| `$schema` | `plugin://re-discipline/schemas/knowledge-settings.schema.json` | Stable packaged schema identifier. |
| `schemaVersion` | `1` | Knowledge-settings schema. |
| `sources.truth` | `true` | Index `docs/truth/**` with current-truth tier labels. |
| `sources.history` | `true` | Index `docs/history/**` as provenance, never current authority. |
| `sources.backlog` | `true` | Index `docs/backlog/**` as deferred intent. |
| `sources.activeCampaigns` | `true` | Index active `CAMPAIGN.md` masterfiles and their `REVIEWS.md` review ledgers as provisional work in the `active` tier. Both halves of one campaign masterfile follow one toggle. |
| `sources.sharedMemory` | `true` | Index accepted `memory/topics/**`; proposals remain excluded. |
| `sources.drafterReports` | `true` | Index `active/*/subagents/*/report.md`. A report a manager has stamped as reviewed enters the `campaign` tier; an unstamped one enters `draft`, which is in no default tier set and must be requested by name. |
| `sources.additional` | `[]` | Optional explicitly classified project-relative Markdown source classes. Each entry requires `path`, a Markdown filename `pattern`, and the non-claim authority `tier` allowed by the current schema. This tracked setting cannot grant external roots, bypass the denylist, or turn a source into admitted truth. |
| `models.execution` | `"local"` | Embeddings and reranking execute locally. |
| `telemetry.mode` | `"metrics-only"` | Local aggregate metrics without query/passage text; `"off"` disables them. |
| `budgets.searchTokens` | `3072` | Maximum estimated tokens for a normal search response. The response envelope is charged against this same budget, so a ceiling near the median result cost returns one passage. |
| `budgets.managerContextTokens` | `6144` | Default ceiling for manager context packs. |
| `budgets.drafterContextTokens` | `3072` | Default ceiling for drafter context packs. |
| `budgets.maxPassages` | `12` | Maximum passages before a smaller caller limit. |
| `budgets.maxBytes` | `32768` | Absolute returned-context byte ceiling. |

### Generated `knowledge/retrieval-profile.json`

Do not edit these fields manually:

| Field | Meaning |
|---|---|
| `$schema`, `schemaVersion` | Packaged schema identity and version. |
| `profileId`, `description` | Requested base profile and human-readable purpose. |
| `baseProfile` | Present on a generated project candidate or accepted overlay; identifies the packaged/project profile it was measured from. |
| `approval` | Present only after explicit promotion. The closed receipt records the decision and time, exact candidate/report digests, benchmark-matrix digest, corpus/eval/model/runtime identities, and the reproducible accepted-profile digest. |
| `effectiveProfiles[]` | Finite, independently benchmarked execution shapes; undeclared combinations fail. |
| `effectiveProfiles[].name`, `.description` | Stable effective-profile name and supported behavior. |
| `effectiveProfiles[].requires.embedding`, `effectiveProfiles[].requires.reranker` | Exact required model IDs or `null`. |
| `effectiveProfiles[].lanes` | Active exact, FTS, graph, dense, and/or rerank lanes. |
| `effectiveProfiles[].weights` | Reciprocal-rank-fusion lane weights for exact, FTS, graph, and dense retrieval. |
| `effectiveProfiles[].rrfK` | Reciprocal-rank-fusion constant. |
| `effectiveProfiles[].rerankDepth` | Bounded reranking candidate depth; zero disables reranking. |
| `effectiveProfiles[].maxPerDocument` | Diversification cap per source document. |
| `effectiveProfiles[].packing.maxPassages`, `.maxBytes` | Hard context-packing ceilings. |
| `effectiveProfiles[].benchmark.suite` | Frozen suite that evaluated this exact capability shape. |
| `effectiveProfiles[].benchmark.digest`, `.status` | Content-hashed evidence identity and passing state. |
| `effectiveProfiles[].benchmark.evaluatedAt` | UTC time of the recorded evaluation receipt. |
| `effectiveProfiles[].benchmark.evalFingerprint`, `.corpusFingerprint` | Exact evaluation corpus and, for project calibration, indexed project corpus measured by the receipt. |
| `effectiveProfiles[].benchmark.modelFingerprint`, `.runtimeFingerprint` | Exact model catalog and reproducible numerical runtime contract measured by the receipt. |
| generated profile/effective content hashes | Runtime-computed immutable identities returned by status, search, and context packs. |

`shared-only` disables Claude Code auto memory in `.claude/settings.json` and
Codex memories in `.codex/config.toml`. It does not delete, read, copy, or
migrate either host's native memory directory. `hybrid` and `native` are
explicit opt-in modes; invoke `init-project` resync after selecting one so the
host files and bootstrap policy cannot drift.

## Authority And Storage Boundaries

- Canonical truth remains in `docs/truth/`.
- History, backlog, and active campaigns retain their own epistemic tiers.
- Accepted operational recall lives in `.re-discipline/memory/topics/`.
- Pending proposals live in `.re-discipline/memory/proposals/` and are excluded from
  ordinary retrieval until a manager reviews them and the user ratifies them.
- Project evaluation cases live in `.re-discipline/knowledge/evals/`.
- Everything under `.re-discipline/cache/` is disposable and ignored.
- The checksum-pinned model artifact lives in the installed plugin, not this
  project. This release exposes no remote-model, external-root, or hardware
  grant; any future security-sensitive grant must be explicit machine-local
  state.

## Recovery And De-Initialization

The `re-discipline:shared-laws v0.7.0` marker in
`.re-discipline/project-profile.md` declares that this managed configuration is expected.
At session start, the cheap bootstrap hook restores a missing tracked
bootstrap, settings, or host-memory policy file from `HEAD`. If a required
file has never been tracked, it creates the current safe default atomically.
It reports every recovered path, then runs the packaged read-only knowledge
`status` under the hook timeout. Status never indexes sources or repairs files.

An existing malformed file is never silently replaced. The project enters a
read-only degraded state until a manager repairs or explicitly resyncs it.
Only status remains available while bootstrap or knowledge settings are
invalid; indexing, retrieval, context packing, calibration, and promotion fail
closed. Recovery never reconstructs machine-local grants and never indexes
sources, builds models, benchmarks, or calibrates.

To de-initialize, use an explicit manager-reviewed migration. Remove or replace
the managed shared-law expectation marker first, then remove the managed
configuration and host-policy fields. Deleting configuration while the marker
remains is treated as accidental deletion, even when that deletion is staged.
