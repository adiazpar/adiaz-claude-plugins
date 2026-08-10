# Design note: making re-discipline cheap to use correctly

**Status:** proposal, unratified. Written 2026-08-10 from one long agent session
that closed a real campaign (`C-SNAPMAP-CRASH-TRIAGE`) against 0.8.2 and shipped
0.8.4 in response.

**Audience:** whoever works on the engine next.

**Relationship to the RFCs:** `docs/rfcs/0001-campaign-state-engine.md` defines
what the engine guarantees. This note is about what it costs to obtain those
guarantees, and argues that the cost is currently high enough to defeat them.

---

## 0. Why this exists

Closing one campaign took an entire session. The campaign was substantively
finished: 10 of 10 work items done, 19 of 19 findings manager-ratified, every
run reported. Nothing about the *knowledge* was in doubt. The session was spent
almost entirely on the machinery.

Worse, three separate times the only way forward was hand-editing canonical JSON
and re-sealing the state inventory with a throwaway Python script. Every one of
those edits did more damage to the integrity story than the gate that forced it
was preventing.

That is the observation this note is built on. It is not a complaint about
strictness. The strictness is right. The problem is what surrounds it.

---

## 1. Thesis

> The engine models integrity as a property of each individual write. Integrity
> is actually a property of the history. So the engine is maximally strict about
> every step and almost silent about how to recover from a bad one.

Two consequences follow, and both were observed:

1. **A system that cannot be repaired through supported operations will be
   repaired through unsupported ones.** The strictness manufactures the exact
   class of event it exists to prevent.

2. **Cost is a correctness property.** When the supported path is expensive
   enough, callers take the unsupported one. An operation nobody can afford to
   perform correctly is not a safety feature.

A second, narrower thesis for Part II:

> Most of what makes the engine expensive is incidental complexity, not
> essential complexity. The epistemics are essential and worth every token. The
> bookkeeping is not, and it dominates.

---

## 2. Evidence from one session

Measured or directly observed, not estimated:

| Observation | Value |
|---|---|
| Tool discovery payload | **58,355 bytes** (~15k tokens) against the repo's own 64 KiB ceiling |
| `manager_apply` schema alone | **24,419 bytes** — 41.8% of all discovery |
| Top three tools (`manager_apply`, `curation_submit`, `migrate_project`) | **71.3%** of discovery |

Those figures are a point measurement taken before the two closure fixes in this
release, which grew discovery to 62,715 before it was cut back to 58,571 by
`$defs`/`$ref` de-duplication of the record types. Treat the ratios, not the
absolute bytes, as the durable finding: one tool owning ~40% of discovery, and
the top three owning ~70%, is the shape that matters. The per-tool budgets now
asserted in the suite are what keep it honest.
| Attempts to commit one `run.prepare` | **7**, each a full re-send |
| Context pack carried inline per attempt | `estimatedTokens: 3052` (engine's own figure) |
| State head revisions consumed | 43 → 61 |
| Out-of-band state repairs required | **3** |

Two structural facts, verifiable by inspection rather than measurement:

- **`closure_apply` responses embed the full coverage map twice** — once as
  top-level `coverage`, once nested inside `job.coverage` — with the complete
  `activeFileDispositions` map (60+ entries) in both copies.
- **Bounded state views take a `tokenBudget` (128–8192). Mutation responses take
  nothing.** Reads are budgeted; writes are unbounded.

Two defects found and fixed in 0.8.4, both the same shape — *a rule written in
one place and not its sibling*:

- `closure.remediation.run.create` was wired through authorization, record kinds,
  submission obligation, and the journal's closing-campaign exemption, and was
  absent from the one enum a caller can name. Closure could demand work the
  exposed surface forbade.
- `normalization_queue.go` skipped curator runs; `ComputeClosureCoverage` did
  not. Closure demanded reviewed intakes for exactly the reports the queue would
  never offer a normalization item for, and each remediation lap added one more
  such report. **Closure was non-convergent by construction.**

### 2.1 What it felt like to use, ranked

The sections below are organised by cause. This is the same material ordered by
how much it actually hurt in the chair, which is not the same ordering and is
worth recording separately.

1. **The contract is inverted.** Being asked to precompute the hash of a file the
   engine itself writes — including bytes it appends after I hand the input over
   — was the single most hostile moment. It is not a safety property. The values
   were derived from the engine's own source, so the check proved only that the
   caller can read Go. Any caller who cannot is simply locked out.

2. **Errors are single-fact oracles.** Each refusal surfaced exactly one missing
   requirement, so the number of round trips equalled the number of unmet
   requirements, and each round trip carried a very large payload. This is
   cheap to fix and would change the experience more than anything else here.

3. **No small edit exists.** Seven words of bookkeeping demanded a work item, a
   run, a brief, a context pack, a dispatched curator, and would have dragged 19
   ratified findings back through review. That is what drove the decision to
   edit state by hand.

4. **The same rule, written twice, in different words.** Both defects fixed in
   0.8.4 were this. Nothing structural prevented the drift and no test detected
   it.

5. **Gates diagnose but do not instruct.** Knowing *what* was wrong still left
   the question of what would fix it, answerable only by reading the engine.

---

## 3. Part I — correctness: the missing half

### 3.1 There is no repair vocabulary

Every out-of-band edit in the session maps to a missing engine operation:

| What was hand-edited | Missing operation |
|---|---|
| Deleted `closure/{job,plan,coverage}.json`, re-sealed inventory | retire or restart a stranded closure job |
| Rewrote 7 intake coverage dispositions, re-sealed inventory | amend coverage bookkeeping |
| (avoided only by luck) | supersede a record without a full packet |

**Proposed litmus test, and it should be treated as a hard rule:**

> If a maintainer ever needs a script to re-seal `inventory.json`, that is a
> missing engine operation and a P0 defect — not a workaround, not a one-off,
> not "recorded manager repair".

Repair operations must be typed, evented, authority-checked, and
compare-and-swapped like everything else. The point is not to weaken the model.
It is that an unlogged hand-edit is strictly worse than a logged repair, and
today the engine only offers the former.

### 3.2 Domain rules are written inline, so they drift

Both 0.8.4 defects were duplicated rules that fell out of sync. The fix in both
cases was to name the rule once (`runIsClaimSource`) and have every site consult
it, plus a test that fails when two surfaces disagree.

**Rule:** any predicate that answers a domain question — *can this report carry
a claim? may this action be reached? is this run terminal?* — gets exactly one
named definition and a drift test. Not an inline conditional at each call site.

### 3.3 Gates can be non-convergent, and nothing checks

Closure queued work that closure forbade, and the work produced artifacts that
re-queued the same gate. Nothing in the suite could have caught this, because
every individual rule was correct.

**Proposed test class:** drive a campaign to closure in a loop and assert the
blocker set **strictly shrinks**. That single property kills the whole family,
including the ones not yet written.

**Proposed design question, to be asked of every new gate:** *can satisfying
this gate create new work that trips it again?*

### 3.4 Gates diagnose; they do not instruct

`missing-reviewed-intake` names a condition. It never names the operation that
would clear it. Working that out took reading `closure.go`, `curation.go`, and
`work_graph.go`.

`state(mode="closure")` rendered a *frozen* coverage snapshot with nothing
indicating it predated the current campaign revision. A previous session misread
it as live and wrote an incorrect "permanently unsatisfiable in engine 0.8.2"
rationale into the event journal, where it is now permanent.

**Rule:** every refusal carries the exact operation and the minimal arguments
that would satisfy it. Every projection states whether it is live or frozen, and
as of what revision.

### 3.5 `unresolved` is legal to write and fatal to close

A curator may disposition a span `unresolved`. Nothing warns that doing so makes
the campaign unclosable until closure, possibly weeks later. Being legal at write
time and fatal at close time is the worst of both.

**Pick one:** refuse to mark an intake `reviewed` while any span is `unresolved`,
or stop letting `unresolved` block closure. Not both.

---

## 4. Part II — token economics

### 4.1 Where the tokens actually go

Ranked by observed cost:

**1. Discovery, paid before any work happens.** 58 KB of tool schemas, ~15k
tokens, 42% of it one tool. `manager_apply` inlines the full schema of every
record type it might accept — findings, intake, review, `reviewPacket`, runs,
work items, `runPreparation` — as one flat union. Every caller pays for all 14
actions to use one.

**2. Full-record submission.** A mutation must carry the complete next record,
not a delta. Flipping a work item to `done` means resending its title, problem,
every acceptance criterion, relations, and owner. Each field is paid for twice:
once to read it, once to send it back.

**3. Client-side proof obligations — largely already fixed, and the reason it was
still felt is itself the lesson.** `run.prepare` demanded the SHA-256 of
artifacts *the engine generates*, including a block the engine appends after the
caller hands over the brief. Producing those hashes meant reading the Go structs,
matching field order and `MarshalIndent`, and validating the serializer against a
previously written pack — a check that proves only that the caller can read Go.

The engine stopped requiring it in `8e9d417`: `completeRunPreparationHandles`
derives omitted handles and keeps verifying supplied ones. The session that
produced this note paid the full cost anyway, because **the running MCP server
was the 0.8.2 plugin cache while the source tree was 0.8.3**. Seven round trips
were spent satisfying a contract that no longer existed.

That is worth more than the item it replaces. A stale cached runtime is
indistinguishable, from inside a session, from a live defect — and it is the
exact failure the version guard was written for after it "stranded 75 commits of
changes under a single published version". The residual real defect was
narrower: nothing *advertised* that the handles were optional. `omitempty`
renders as an absent `required` entry and nothing more, so a caller discovered
the affordance only by tripping a refusal. Fixed by publishing it in the schema.

**4. Round-trip amplification.** Refusals reveal one missing requirement each.
Seven attempts × a payload dominated by a ~3k-token context pack. Cost 2 and
cost 3 multiply by the number of attempts.

**5. Unbounded mutation responses.** Reads are budgeted; writes echo everything,
twice in the closure case.

### 4.2 Principles

- **P1 — Never echo what the caller just sent.** Return the new head, the changed
  record ids with revisions and digests, and the event id. Nothing else.
- **P2 — Never serialize the same object twice in one response.**
- **P3 — Deltas, not whole records.** The engine already holds the previous
  record; accept a patch plus the expected digest and let it seal the result.
- **P4 — The engine computes what the engine knows.** Artifact handles, packs,
  derived digests. Never make the caller predict server output.
- **P5 — One round trip per intent.** Validate the whole request and return
  every violation at once, ordered.
- **P6 — Schema economy.** A caller should load only the schema for what it is
  doing.
- **P7 — Budget writes like reads.** `tokenBudget` on mutation responses too.
- **P8 — Refusals carry their remedy.** This is a token optimization as much as a
  usability one: it converts N exploratory round trips into 1.

### 4.3 Concrete changes, highest value first

1. **Split `manager_apply` per action.** Still open, and still the largest single
   win. The `$ref` half of this is now **done and exhausted**: `IntakeRecord` and
   `FindingSubmission` are hoisted into `$defs`, which cut 4,144 bytes and, more
   usefully, caught a live drift — the two inlined intake copies had diverged, and
   only one described the amendment log. Naming a record once fixes duplication
   and drift together.

   What remains needs the tool set itself to change, which the adversarial suite
   asserts by name and count, so it is a deliberate API change rather than a
   refactor. A further ~1.8 KB is available by hoisting primitives (the shape
   `{"type":"array","items":{"type":"string"}}` occurs 54 times in
   `manager_apply`), and was declined: it trades legibility of published surface
   for 3%.
2. **Return receipts, not state.** Drop `coverage` from the top level of
   `closure_apply` results, or drop `job.coverage`. One of the two is pure
   duplication. Then trim `activeFileDispositions` out of the default response
   behind an explicit request.
3. ~~**Let `run.prepare` derive its own handles.**~~ **Done before this note was
   written** (`8e9d417`), and published in the schema afterwards so callers can
   see the affordance instead of discovering it through a refusal. Retained here
   because the *reason* it still hurt — a 0.8.2 cached runtime against a 0.8.3
   tree — is a live hazard that no amount of engine work fixes. Before treating a
   refusal as a defect, confirm the runtime version matches the tree.
4. **Aggregate validation.** One pass, all violations, before any write.
5. **Delta mutations for the common transitions** — `work.update` state changes,
   run status transitions. These are the highest-frequency, highest-waste calls.
6. **A `next` affordance on every refusal:** operation name plus the minimal
   argument set.

### 4.4 What this would have cost

Rough, and labelled as such: the `run.prepare` sequence alone consumed on the
order of 50k tokens across seven attempts. Under P4 (engine derives handles) and
P5 (aggregate validation) the same operation is one call carrying a brief and a
target — plausibly under 5k. The discovery saving is ~6k tokens per session
before any work begins.

---

## 5. "The old one was simpler"

That instinct is worth taking seriously, and I think it resolves cleanly.

**Essential complexity — keep all of it.** The epistemic model is the reason to
use this tool at all, and I have not seen another that does it: claims typed by
evidence grade and validity, review state separated from validity, truth gated
behind closure, findings bound to exact evidence spans, retrieval that serves
normalized findings rather than raw documents. Every one of those earns its cost.
The digest-chained event history is essential too.

**Incidental complexity — most of what hurts.** Full-record submission.
Client-side digest computation. Response echo. A single monolithic tool schema.
One-fact-at-a-time refusals. Rules duplicated across call sites. None of these
protect anything. They are implementation choices that leaked into the contract.

So the answer to *"does it need an overhaul or a simplification?"* is **neither**.
Simplification would cut the part worth having. What is missing is the other
half of the design: repair, ergonomics, and cost discipline. The engine is a
very good ratchet with no pawl release.

---

## 6. What must not change

Stated explicitly so this note cannot be read as license to loosen things:

- Compare-and-swap on head revision and record digests.
- The event journal and its digest chain.
- Truth projection only through closure.
- Curators propose; managers ratify; drafters never do either.
- Refusal over silent corruption. **Every refusal in the session was correct.**
  Nothing lied, nothing silently mutated, nothing was lost.

The code comments explaining *why* each rule exists are a genuine asset and the
only reason this analysis was possible. Keep writing them.

### 6.1 Why it is like this, and why that matters

This engine was built by someone who had been burned by silent corruption. The
version guard says so in its own docstring:

> the divergence is silent -- the runtime keeps serving stale packaged behavior
> with no error anywhere. That is not hypothetical; it stranded 75 commits of
> changes under a single published version.

Every gate here is a scar, and every one of them is earned. That is why the
instinct to "simplify" is wrong: the gates are not decoration, they are the
memory of specific failures.

But scar tissue accumulates. The engine now defends against corruption so
determinedly that it forces corruption — three unlogged hand-edits in one
session, each one a larger breach than the gate that caused it. **Adding the
repair half does not weaken the integrity model. It is the only thing that lets
it survive contact with real use.**

One more honest note, since it bears on the diagnosis. Part of the session's
cost was the agent's, not the engine's: the ceremonial path was pursued well past
the point where a smaller repair should have been considered, and the first
draft of the `runIsClaimSource` rule was wrong — too broad — caught only because
the repository's own closure fixture contradicted it by carrying a
`manager`-role run with a ratified truth finding.

That cuts toward this note's argument rather than against it. A system this
strict punishes caller drift severely and gives very little help recovering from
it. That is an argument for better ergonomics and a repair vocabulary, not for
less rigor. It is also an argument for more fixtures like that one: it was the
only thing standing between a plausible-sounding rule and a wrong one shipping.

---

## 6.2 Cutting a release — the order is not optional

Discovered twice by walking into it, so it is written down here rather than left
in a session transcript.

```
1. bump RuntimeVersion in knowledge/internal/knowledge/types.go
2. .github/scripts/re-discipline-sync-version.py
3. packager --output bin
4. bin/re-discipline-knowledge benchmark --asset-root . --mode full --update-declared
5. packager --output bin          # again
6. packager --output bin --verify
7. go test ./...                  # only now will it pass
```

Two circular-looking constraints force it, and each announces itself as an
unrelated failure:

- **Step 4 needs step 3.** `--update-declared` runs the *packaged* binary, and a
  binary built at the old version refuses the freshly synced manifest with
  `model manifest runtime version mismatch`. So the package has to be rebuilt at
  the new version before the evidence can be re-measured.
- **Step 5 needs step 4.** The declared profile evidence embeds a runtime
  fingerprint derived from `RuntimeVersion`, and the shipped binaries embed the
  evidence. Package before re-measuring and you ship binaries whose embedded
  evidence is already stale; the Go suite then fails with `embedded packaged
  profile evidence for hybrid-no-rerank-v1 is stale`.

Neither failure names the ordering as the cause, which is what makes this worth
a section. Both are correct refusals doing their job — the cost is only that a
maintainer has to already know the sequence to read them.

**This belongs in `A1` of the repair vocabulary work**: each of those two
refusals should name the step that precedes it.

---

## 7. Sequencing

1. **0.8.5** — repair vocabulary: restart a reopened closure job; amend coverage
   without disturbing findings or the review binding. (Designs in progress.)
2. **0.8.6** — discovery cost: split or `$ref` the `manager_apply` schema;
   de-duplicate closure responses.
3. **0.8.7** — contract inversion: engine-derived run handles; aggregate
   validation; refusals that carry their remedy.
4. **Ongoing** — a convergence test for closure; drift tests for every named
   domain rule; a per-tool schema budget asserted in CI, the way the 64 KiB
   total already is.

---

## 8. Open questions

- Should coverage bookkeeping live on the intake at all? It is the only mutable
  thing on an otherwise immutable record, and every problem in this note touches
  it. A separate coverage record with its own revision line would make amendment
  natural instead of exceptional.
- Is `run.prepare`'s context pack worth its cost at all, given the caller usually
  holds the same information? Measure what a drafter actually reads from it.
- Should closure freeze runs at all, now that remediation is reachable? The
  freeze exists to stop the record moving under a closing campaign, but the
  campaign revision is already compare-and-swapped on every write.
- What is the right budget for a mutation response? Reads settled on 128–8192.
