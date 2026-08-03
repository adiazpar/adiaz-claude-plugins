# Disposable migration pilot harness

The release pilot runs the 0.7-to-0.8 state machine against complete,
byte-pinned clones of an existing project. It never invokes a migration action
against either source checkout. The selected `liveCampaign` defines exhaustive
normalization and certification scope; it is not a partial activation list.
Every managed path in the clone is inventoried and the entire managed tree is
cut over atomically.

The harness is `tests/re_discipline_migration_pilot.py`. It uses only Python's
standard library, Git, Go, and the packaged runtime for the current platform.
Its source inputs must both be clean worktree roots at exact lowercase
40-character commits. Dirty, untracked, mismatched, unsafe, or package-tampered
inputs fail before a disposable migration starts.

## Safety boundary

Before cloning and after every phase, the harness proves that the source
project:

- is still at the exact requested commit and byte inventory;
- still carries the 0.7 shared-laws and Codex-adapter markers;
- has no `.re-discipline/migration/0.8` transaction; and
- has no canonical `.re-discipline/state/head.json`.

The output directory must not exist and may not overlap either source
repository. Clones, command captures, manager inputs, caches, and receipts all
live below that output directory. The harness never resets, stages, commits,
or cleans either repository.

## Phases

Run from the plugin repository, replacing commits and output with absolute or
shell-appropriate paths:

```text
python tests/re_discipline_migration_pilot.py prepare \
  --plugin-repository PLUGIN_REPOSITORY \
  --plugin-revision 40_CHARACTER_PLUGIN_COMMIT \
  --project-repository PROJECT_REPOSITORY \
  --project-revision 40_CHARACTER_PROJECT_COMMIT \
  --pilot small \
  --output DISPOSABLE_OUTPUT
```

Use `--pilot small` for `prelude-pack-recalibration`, then repeat in a separate
output with `--pilot scale` for `resource-registration`.

`prepare` verifies the complete runtime package, builds the source-pinned pilot
helper, captures malformed-input refusal, captures an unavailable MCP launch
followed by the real CLI fallback, previews without mutation, and exports exact
truth/profile conflict packets. It stops at `manager-decisions-required`.

A manager reviews the packets and writes only the requested files below
`DISPOSABLE_OUTPUT/manager-input/`:

- `profile-decision.json`, when requested, using the packaged submission
  schema and exact conflict identities;
- one truth-review JSON per requested source under `truth-reviews/`; and
- `plan-approval.json`, binding the regenerated plan digest with
  `schemaVersion`, kind `migration-plan-approval-v1`, pilot, live campaign,
  authority `manager`, reviewer, rationale, UTC `decidedAt`, and
  `explicitApproval: true`.

Run the same command with `advance`. It submits those exact decisions,
regenerates the preview, accepts only its exact manager-approved digest,
starts the transaction, builds the shadow catalog, and exports the complete
live-report coverage request. It stops at `coverage-review-required`.

Coverage receipts belong under `manager-input/coverage/`. A curator must
exhaustively partition every report and author any candidate findings. The
manager reviews each finalized receipt. The harness does not infer report
dispositions, atomic claims, evidence grades, synthetic questions, or review
authority.

The next `advance` submits coverage, injects and recovers a stale legacy
writer, reaches normalized state, injects a deterministic interruption after
the activation journal records the first backup rename, resumes through the
real CLI, and records engine-derived structural and semantic-traversal gates.
It stops at `certification-evidence-required`.

Current strict retrieval and host evidence is supplied with
`manager-input/gates/evidence-submission.json`. The submission is a
manager-reviewed object with these exact identity fields:

```json
{
  "schemaVersion": 1,
  "kind": "migration-pilot-evidence-submission-v1",
  "pilot": "small",
  "liveCampaign": "prelude-pack-recalibration",
  "transactionId": "M-...",
  "planDigest": "sha256:...",
  "authority": "manager",
  "reviewer": "...",
  "rationale": "...",
  "decidedAt": "2026-08-03T00:00:00Z",
  "copies": [
    {
      "sourcePath": "manager-input/gates/bundle/benchmark.json",
      "destinationPath": ".re-discipline/migration/0.8/evidence/benchmark.json",
      "sha256": "sha256:..."
    }
  ],
  "gateArtifacts": {
    "retrieval-context": ".re-discipline/migration/0.8/evidence/retrieval-context.json",
    "host-parity": ".re-discipline/migration/0.8/evidence/host-parity.json"
  }
}
```

Every source must be below `manager-input/gates/`; every destination must be
below the disposable project's migration evidence root. The harness copies
the declared bytes once and refuses collisions. The engine then reopens and
revalidates the gate artifacts and every nested benchmark, calibration,
blinded-agent, and host-conformance binding. The harness never synthesizes
those measurements.

After the candidate certification passes, write
`manager-input/certification-approval.json` with the same approval fields,
kind `migration-certification-approval-v1`, and the exact
`certificationDigest`. The next `advance` records both strict gates, verifies,
advances to traversal-verified, ratifies, confirms project version 0.8, and
rebuilds the derived index in the disposable cache.

At any phase, replay all source, schema, receipt, command, and referenced-byte
checks without advancing:

```text
python tests/re_discipline_migration_pilot.py verify SAME_ARGUMENTS
```

## Evidence retained

Every command has a portable argument vector plus byte-exact stdout, stderr,
optional request, exit code, classified failure, timestamps, and canonical
digest. The versioned pilot receipt binds:

- source commits, Git trees, and working-byte inventories;
- disposable copy inventories;
- runtime package manifest, checksums, build ID, and selected binary;
- preview, source, plan, transaction, operation, certification, and final
  migration digests;
- decision, coverage, evidence, gate, interruption, recovery, and ratification
  artifacts; and
- identical before/after production 0.7 proofs.

Receipts validate against
`knowledge/schemas/disposable-migration-pilot.schema.json`. Each phase archives
the prior sealed receipt before publishing the next one. A changed command
capture, request, packet, copied evidence file, source checkout, package file,
or final result fails verification.
