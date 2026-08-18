package knowledge

import (
	"context"
	"strings"
	"testing"
)

// oversizedObjective is deliberately larger than maxScopedConstraintBytes and
// larger than every permitted context-pack budget.
func oversizedObjective() string {
	return strings.TrimSpace(strings.Repeat(
		"The campaign objective grew into a full design document that every scoped "+
			"context pack must then carry verbatim as mandatory accepted context. ", 60))
}

// TestOversizedObjectiveIsRefusedAtWriteTime pins the root-cause fix: a
// campaign objective too large to fit any permitted pack budget is refused
// when it is written, not silently accepted and then discovered later when
// every run preparation fails.
func TestOversizedObjectiveIsRefusedAtWriteTime(t *testing.T) {
	root := makeAdversarialProject(t)
	store, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.Objective = oversizedObjective()
	campaign.UpdatedBy, campaign.CorrelationID, campaign.Digest = "manager", "corr-fat-objective", ""

	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "campaign.update", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-fat-objective", IdempotencyKey: "idem-fat-objective",
		ExpectedHeadRevision:  opening.ResultingHead.Revision,
		ExpectedHeadDigest:    opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{"C-TEST": graph.Campaign.Digest},
		Campaign:              &campaign,
	})
	if err == nil {
		t.Fatal("an objective too large for any pack budget was accepted")
	}
	for _, want := range []string{"objective", "mandatory scoped context", "per-record limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("write-time refusal does not name %q: %v", want, err)
		}
	}

	// The campaign must be unchanged, so a manager can still shorten and retry.
	after, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if after.Campaign.Objective != graph.Campaign.Objective {
		t.Fatal("refused campaign update still mutated canonical state")
	}
}

// TestOversizedWorkItemProblemIsRefusedAtWriteTime covers the second mandatory
// scoped-context record.
func TestOversizedWorkItemProblemIsRefusedAtWriteTime(t *testing.T) {
	root := makeAdversarialProject(t)
	store, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	work := graph.WorkItems["W-0001"]
	work.Revision++
	work.Problem = oversizedObjective()
	work.UpdatedBy, work.CorrelationID, work.Digest = "manager", "corr-fat-problem", ""

	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "work.update", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-fat-problem", IdempotencyKey: "idem-fat-problem",
		ExpectedHeadRevision:  opening.ResultingHead.Revision,
		ExpectedHeadDigest:    opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{"W-0001": graph.WorkItems["W-0001"].Digest},
		WorkItems:             []WorkItemRecord{work},
	})
	if err == nil || !strings.Contains(err.Error(), "problem") ||
		!strings.Contains(err.Error(), "work item W-0001") {
		t.Fatalf("oversized work-item problem was not refused by field name: %v", err)
	}
}

// TestScopedConstraintBudgetGuardAllowsOrdinaryRecords proves the guard is a
// stop on unbounded growth, not a new obstacle for normal campaigns.
func TestScopedConstraintBudgetGuardAllowsOrdinaryRecords(t *testing.T) {
	campaign := stateTestCampaignRecord()
	campaign.Objective = strings.Repeat("A precise but ordinary campaign objective. ", 20)
	campaign.Scope = []string{strings.Repeat("bounded scope statement ", 20)}
	campaign.SuccessCriteria = []string{strings.Repeat("observable success criterion ", 20)}
	campaign.ClosureCriteria = []string{strings.Repeat("observable closure criterion ", 20)}
	work := stateTestWorkItem("W-0001")
	work.Problem = strings.Repeat("A well-scoped problem statement. ", 30)
	if err := ValidateScopedContextBudget(&campaign, []WorkItemRecord{work}); err != nil {
		t.Fatalf("ordinary campaign and work item were refused: %v", err)
	}
}

// TestScopedContextBudgetGuardIgnoresUnchangedLegacyText keeps the new guard
// from turning a late failure into an unrecoverable one. A campaign that was
// already oversized before the guard existed must still be able to complete
// its in-flight runs and to shrink itself; only a write that introduces or
// grows the oversized text is refused.
func TestScopedContextBudgetGuardIgnoresUnchangedLegacyText(t *testing.T) {
	graph := NewCampaignGraph()
	campaign := stateTestCampaignRecord()
	graph.Campaign = &campaign
	legacy := stateTestWorkItem("W-0001")
	legacy.Problem = oversizedObjective()
	graph.WorkItems["W-0001"] = legacy

	carried := legacy
	carried.Revision++
	carried.State = "active"
	if err := validateScopedContextBudgetDelta(graph, ManagerApplyRequest{
		Action: "run.prepare", WorkItems: []WorkItemRecord{carried},
	}); err != nil {
		t.Fatalf("carrying committed constraint text forward was refused: %v", err)
	}

	shrunk := carried
	shrunk.Problem = "A bounded problem statement."
	if err := validateScopedContextBudgetDelta(graph, ManagerApplyRequest{
		Action: "work.update", WorkItems: []WorkItemRecord{shrunk},
	}); err != nil {
		t.Fatalf("shrinking an oversized record was refused: %v", err)
	}

	grown := carried
	grown.Problem += " One more paragraph of mandatory scoped context."
	if err := validateScopedContextBudgetDelta(graph, ManagerApplyRequest{
		Action: "work.update", WorkItems: []WorkItemRecord{grown},
	}); err == nil {
		t.Fatal("growing an already-oversized record was accepted")
	}

	if err := validateScopedContextBudgetDelta(graph, ManagerApplyRequest{
		Action: "reconcile.import", WorkItems: []WorkItemRecord{grown},
	}); err != nil {
		t.Fatalf("recovery import was blocked by the budget guard: %v", err)
	}
}

// TestMandatoryContextBudgetErrorNamesWhatDidNotFit pins the diagnostic fix:
// a pack that cannot fit its mandatory floor must report which constraints and
// cards are responsible, their sizes, and the minimum budget that would work.
func TestMandatoryContextBudgetErrorNamesWhatDidNotFit(t *testing.T) {
	root := makeAdversarialProject(t)
	store, _ := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	// Reach past the write-time guard the way an already-committed legacy
	// record does, so the compiler still has to explain itself.
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	campaign := *graph.Campaign
	campaign.Objective = oversizedObjective()
	writeCanonicalCampaignForTest(t, store, campaign)

	_, err = service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: "C-TEST", WorkItemID: "W-0001",
			RunID: "R-20260802-0002",
		},
		Task: "Inspect the canonical campaign state", Role: "drafter", TokenBudget: 2048,
	})
	if err == nil {
		t.Fatal("a pack whose mandatory floor cannot fit was compiled anyway")
	}
	message := err.Error()
	for _, want := range []string{
		"mandatory scoped context",
		"constraint objective from record:active/test-campaign/campaign.json",
		"tokens",
		"largest contributors",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("budget refusal does not report %q: %s", want, message)
		}
	}
	if !strings.Contains(message, "no budget can fit this pack") &&
		!strings.Contains(message, "raise tokenBudget to at least") {
		t.Fatalf("budget refusal states no actionable minimum: %s", message)
	}
}

// writeCanonicalCampaignForTest republishes a campaign record directly through
// the sealing path so a test can simulate state committed before a newer
// write-time guard existed.
func writeCanonicalCampaignForTest(t *testing.T, store *StateStore, campaign CampaignRecord) {
	t.Helper()
	sealed, body, err := sealStateRecord(campaign, "active/test-campaign/campaign.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sealed.(CampaignRecord); !ok {
		t.Fatal("sealed value is not a campaign record")
	}
	absolute, err := store.canonicalOutputPath("active/test-campaign/campaign.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(absolute, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
