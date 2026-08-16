package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The rule a budgeted write must never break: whatever came back is enough to
// build the next transaction. If a budget can cost the caller a compare-and-swap
// input, it costs an extra read to recover it, which is more than the omission
// saved and turns an optimization into a regression.
//
// These tests therefore assert the floor from the caller's side - by actually
// issuing the next transaction using only what the budgeted receipt carried -
// rather than by inspecting which fields happen to be populated.

func TestBudgetedManagerReceiptStillCarriesTheNextCompareAndSwap(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.request
	// 128 is the floor of the accepted range and far below what a run.prepare
	// receipt costs, so every droppable section is dropped and the floor is
	// what remains.
	request.TokenBudget = mutationTokenBudgetMinimum

	receipt, err := fixture.service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatalf("budgeted run.prepare: %v", err)
	}
	if len(receipt.Omitted) == 0 {
		t.Fatal("a receipt well over the smallest budget reported no omission")
	}
	if len(receipt.Artifacts) != 0 {
		t.Fatalf("artifacts survived the smallest budget: %#v", receipt.Artifacts)
	}
	// Every omission must name what was dropped and how to get it, or the
	// caller cannot tell an omitted section from an empty one.
	for _, omission := range receipt.Omitted {
		if !strings.Contains(omission, "omitted under tokenBudget") ||
			!strings.Contains(omission, "tokenBudget omitted") {
			t.Errorf("omission does not name the section and the way back: %q", omission)
		}
	}

	// The floor: identity, both heads, the event, and one record result per
	// written record, each with its id, revision, and digest.
	if receipt.TransactionID == "" || receipt.CorrelationID == "" ||
		receipt.IdempotencyKey == "" || receipt.RequestDigest == "" ||
		receipt.ResultDigest == "" || receipt.CommittedAt == "" ||
		receipt.GeneratedViewDigest == "" {
		t.Fatalf("budgeted receipt dropped transaction identity: %#v", receipt)
	}
	if receipt.Event.ID == "" || receipt.Event.Digest == "" ||
		receipt.ResultingHead.EventID != receipt.Event.ID {
		t.Fatalf("budgeted receipt dropped the event anchor: %#v", receipt.Event)
	}
	if receipt.PreviousHead.Revision != request.ExpectedHeadRevision ||
		receipt.PreviousHead.Digest != request.ExpectedHeadDigest {
		t.Fatalf("budgeted receipt dropped the previous head: %#v", receipt.PreviousHead)
	}
	if receipt.ResultingHead.Revision == 0 || receipt.ResultingHead.Digest == "" {
		t.Fatalf("budgeted receipt dropped the resulting head: %#v", receipt.ResultingHead)
	}
	digests := map[string]string{}
	revisions := map[string]int64{}
	for _, record := range receipt.Records {
		if record.RecordID == "" || record.RecordDigest == "" || record.Revision == 0 {
			t.Fatalf("budgeted record result is not a usable expectation: %#v", record)
		}
		digests[record.RecordID] = record.RecordDigest
		revisions[record.RecordID] = record.Revision
	}
	runID := request.Runs[0].ID
	for _, id := range []string{runID, "W-0001"} {
		if digests[id] == "" {
			t.Fatalf("budgeted receipt omitted the expectation for %s: %#v", id, receipt.Records)
		}
	}

	// Now prove it: build the following transaction out of the budgeted receipt
	// alone and commit it. Nothing here re-reads state.
	run := request.Runs[0]
	run.Revision = revisions[runID] + 1
	run.Status, run.UpdatedBy, run.CorrelationID, run.Digest =
		"running", "manager", "corr-run-start", ""
	run.StartedAt = stateTestTime
	work := request.WorkItems[0]
	work.Revision = revisions["W-0001"] + 1
	work.UpdatedBy, work.CorrelationID, work.Digest = "manager", "corr-run-start", ""

	next := ManagerApplyRequest{
		Action: "run.start", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-run-start", IdempotencyKey: "idem-run-start",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: digests[runID], "W-0001": digests["W-0001"],
		},
		Runs: []RunRecord{run}, WorkItems: []WorkItemRecord{work},
	}
	if _, err := fixture.service.ManagerApply(context.Background(), next); err != nil {
		t.Fatalf("the following transaction could not be built from the budgeted receipt: %v", err)
	}
}

// A budgeted copy is not a receipt, and must not be able to masquerade as one.
// Its resultDigest still names the complete committed receipt, so silently
// letting it fail digest verification would read as corruption.
func TestBudgetedReceiptRefusesToVerifyAsAReceipt(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	complete, err := fixture.service.ManagerApply(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyTransactionReceipt(complete); err != nil {
		t.Fatalf("an unbudgeted receipt must verify: %v", err)
	}
	budgeted, err := budgetTransactionReceipt(complete, mutationTokenBudgetMinimum)
	if err != nil {
		t.Fatal(err)
	}
	if budgeted.ResultDigest != complete.ResultDigest {
		t.Fatal("a budgeted copy re-sealed its own digest; that mints a second identity " +
			"for one committed transaction")
	}
	err = verifyTransactionReceipt(budgeted)
	if err == nil {
		t.Fatal("a budgeted copy verified as a complete receipt")
	}
	if !strings.Contains(err.Error(), "tokenBudget") ||
		!strings.Contains(err.Error(), "complete committed receipt") {
		t.Fatalf("verification refusal does not explain the budgeted copy: %v", err)
	}
	if err := sealTransactionReceipt(&budgeted); err == nil {
		t.Fatal("a budgeted copy was sealable")
	}
}

// The persisted receipt and the journal must be untouched by a caller's budget:
// budgeting runs at the service return boundary, after the commit.
func TestBudgetDoesNotReachThePersistedReceipt(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.request
	request.TokenBudget = mutationTokenBudgetMinimum
	budgeted, err := fixture.service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStateStoreWithBoundary(fixture.service.Boundary)
	stored, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		t.Fatalf("committed receipt is not retrievable: %v", err)
	}
	if len(stored.Omitted) != 0 {
		t.Fatalf("a caller's tokenBudget reached the persisted receipt: %#v", stored.Omitted)
	}
	if len(stored.Artifacts) != 3 {
		t.Fatalf("the persisted receipt lost artifacts: %#v", stored.Artifacts)
	}
	if err := verifyTransactionReceipt(stored); err != nil {
		t.Fatalf("the persisted receipt no longer verifies: %v", err)
	}
	if stored.ResultDigest != budgeted.ResultDigest {
		t.Fatal("the budgeted response and the persisted receipt disagree on the transaction digest")
	}
}

func TestMutationTokenBudgetIsAnAdvisoryNonNegativeHint(t *testing.T) {
	for _, budget := range []int{0, 1, 64, 8193, 100000} {
		if err := validateMutationTokenBudget("manager_apply", budget); err != nil {
			t.Fatalf("advisory tokenBudget %d was refused: %v", budget, err)
		}
	}
	if err := validateMutationTokenBudget("manager_apply", -1); err == nil ||
		!strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("negative tokenBudget returned %v", err)
	}
}

// applyResponseBudget drops whole sections in a fixed order and stops as soon
// as the response fits. Both halves matter: a partial section is a falsified
// receipt, and a data-dependent order would make a caller's handling of the
// response depend on how large the campaign happens to be.
func TestResponseBudgetDropsWholeSectionsInAFixedOrder(t *testing.T) {
	type payload struct {
		Identity string   `json:"identity"`
		First    []string `json:"first,omitempty"`
		Second   []string `json:"second,omitempty"`
	}
	filler := make([]string, 40)
	for index := range filler {
		filler[index] = strings.Repeat("x", 40)
	}
	value := payload{Identity: "keep-me", First: filler, Second: filler}
	sections := []budgetSection{
		{
			Name: "first", Note: "re-issue without tokenBudget",
			Present: func() bool { return len(value.First) != 0 },
			Drop:    func() { value.First = nil },
		},
		{
			Name: "second", Note: "re-issue without tokenBudget",
			Present: func() bool { return len(value.Second) != 0 },
			Drop:    func() { value.Second = nil },
		},
	}
	// A budget that one section's removal already satisfies must not take the
	// other: "omit derived detail" is not "shrink as far as possible".
	total, err := estimateResponseTokens(&value)
	if err != nil {
		t.Fatal(err)
	}
	omitted, err := applyResponseBudget(total*2/3, &value, sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(omitted) != 1 || !strings.HasPrefix(omitted[0], "first omitted under tokenBudget") {
		t.Fatalf("budget did not drop exactly the first section: %#v", omitted)
	}
	if len(value.Second) != len(filler) {
		t.Fatalf("budget truncated or dropped a section it did not need: %d rows", len(value.Second))
	}
	if value.Identity != "keep-me" {
		t.Fatal("budget touched identity")
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"first"`) {
		t.Fatal("a dropped section was rendered partially rather than omitted")
	}
}

// A queue transition's receipt must keep the itemId and digest the next
// transition compares against, whatever the budget.
func TestBudgetedNormalizationReceiptKeepsItsTransitionInputs(t *testing.T) {
	item := NormalizationSuggestion{
		ID: "normalization-0123456789abcdef0123", Status: "claimed",
		Digest: "sha256:" + strings.Repeat("b", 64),
		Resolution: &NormalizationResolution{
			SchemaVersion: CampaignSchemaVersion,
			ResolvedFindingIDs: []string{
				"F-0001", "F-0002", "F-0003", "F-0004", "F-0005", "F-0006",
			},
			CoverageDigest: "sha256:" + strings.Repeat("c", 64),
		},
	}
	queue := NormalizationQueueStatus{Queued: 3, Claimed: 1}
	for index := 0; index < 12; index++ {
		queue.Items = append(queue.Items, NormalizationSuggestion{
			ID: "normalization-filler", ReportPath: strings.Repeat("p", 80),
		})
	}
	result, err := budgetNormalizationQueueResult(NormalizationQueueResult{
		SchemaVersion: CampaignSchemaVersion, Action: "claim",
		Item: &item, Queue: queue,
	}, mutationTokenBudgetMinimum)
	if err != nil {
		t.Fatal(err)
	}
	if result.Item == nil || result.Item.ID != item.ID ||
		result.Item.Digest != item.Digest || result.Item.Status != "claimed" {
		t.Fatalf("budgeted queue receipt lost the next transition's inputs: %#v", result.Item)
	}
	if len(result.Omitted) == 0 {
		t.Fatal("a queue receipt well over the smallest budget reported no omission")
	}
	if result.Item.Resolution != nil {
		t.Fatal("the resolution echo survived the smallest budget")
	}
	if len(result.Queue.Items) != 0 || result.Queue.Omitted != len(queue.Items) {
		t.Fatalf("the backlog listing was not dropped whole and counted: %#v", result.Queue)
	}
	// Unlike a transaction receipt, this result's digest is a pure response
	// seal, so it must reseal over exactly what it returned.
	resealed, err := sealNormalizationQueueResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if resealed.Digest != result.Digest {
		t.Fatal("a budgeted queue result's digest does not seal its own body")
	}
	// The counts are the actionable summary and are never dropped.
	if resealed.Queue.Queued != 3 || resealed.Queue.Claimed != 1 {
		t.Fatalf("budget dropped the queue counts: %#v", resealed.Queue)
	}
}

// closure_apply's droppable sections are the bulky derived maps; the plan's
// identity and the job's stage, status, blockers, and digest are the inputs to
// the next closure transition and survive any budget.
func TestBudgetedClosureResultKeepsTheNextClosureTransitionsInputs(t *testing.T) {
	coverage := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: "C-TEST",
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		ActiveFileDispositions: map[string]string{},
	}
	for index := 0; index < 40; index++ {
		key := strings.Repeat("k", 30) + string(rune('a'+index%26)) + string(rune('0'+index/26))
		coverage.ActiveFileDispositions[key] = "retain"
		coverage.FileRetention[key] = "retained-inline"
	}
	job := ClosureJob{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "closure-job-1", Revision: 4,
			Digest: "sha256:" + strings.Repeat("d", 64),
		},
		CampaignID: "C-TEST", Stage: "coverage", Status: "blocked",
		Blockers: []string{"active-file:notes.md"}, Coverage: &coverage,
	}
	plan := ClosurePlan{
		SchemaVersion: CampaignSchemaVersion, CampaignID: "C-TEST", CampaignRevision: 7,
		ArchiveDestination: "docs/history/campaigns/test-campaign",
		Digest:             "sha256:" + strings.Repeat("e", 64),
	}
	for index := 0; index < 40; index++ {
		plan.RequiredRunIDs = append(plan.RequiredRunIDs, strings.Repeat("r", 40))
	}
	transaction := StateTransactionReceipt{
		SchemaVersion: CampaignSchemaVersion, TransactionID: "tx-1",
		PreviousHead:  StateHead{Revision: 10, Digest: "sha256:" + strings.Repeat("f", 64)},
		ResultingHead: StateHead{Revision: 11, Digest: "sha256:" + strings.Repeat("9", 64)},
		Records: []StateRecordResult{{
			Path: "active/test-campaign/closure/job.json", RecordID: "closure-job-1",
			Revision: 4, RecordDigest: job.Digest,
		}},
		ResultDigest: "sha256:" + strings.Repeat("8", 64),
	}
	for index := 0; index < 30; index++ {
		transaction.Artifacts = append(transaction.Artifacts, StateArtifactResult{
			Path: strings.Repeat("a", 50), ContentDigest: "sha256:" + strings.Repeat("7", 64),
		})
	}

	result, err := budgetClosureApplyResult(ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: "advance",
		Transaction: &transaction, Plan: &plan, Job: &job,
	}, mutationTokenBudgetMinimum)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Omitted) == 0 {
		t.Fatal("a closure result well over the smallest budget reported no omission")
	}
	if result.Job == nil || result.Job.Coverage != nil {
		t.Fatalf("job.coverage survived the smallest budget: %#v", result.Job)
	}
	if result.Job.ID != "closure-job-1" || result.Job.Revision != 4 ||
		result.Job.Digest != job.Digest || result.Job.Stage != "coverage" ||
		result.Job.Status != "blocked" || len(result.Job.Blockers) != 1 {
		t.Fatalf("budget dropped a closure job field the next transition needs: %#v", result.Job)
	}
	if result.Plan == nil || result.Plan.CampaignRevision != 7 || result.Plan.Digest != plan.Digest {
		t.Fatalf("budget dropped restart's expectedClosurePlanRevision inputs: %#v", result.Plan)
	}
	if result.Transaction == nil ||
		result.Transaction.ResultingHead != transaction.ResultingHead ||
		len(result.Transaction.Records) != 1 ||
		result.Transaction.Records[0].RecordDigest != job.Digest {
		t.Fatalf("budget dropped the closure transaction's compare-and-swap: %#v", result.Transaction)
	}
	// The engine's own job and plan values must not have been mutated by
	// producing a budgeted view of them.
	if job.Coverage == nil || len(plan.RequiredRunIDs) != 40 || len(transaction.Artifacts) != 30 {
		t.Fatal("budgeting mutated the values the engine built rather than a copy")
	}
	// A pure response seal must cover exactly the body that was returned.
	resealed, err := sealClosureApplyResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if resealed.Digest != result.Digest {
		t.Fatal("a budgeted closure result's digest does not seal its own body")
	}
}
