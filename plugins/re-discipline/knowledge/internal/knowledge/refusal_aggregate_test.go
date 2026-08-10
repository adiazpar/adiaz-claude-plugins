package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests are the proof that aggregation actually aggregates. The
// implementation is easy to get subtly wrong in a way that still compiles and
// still refuses: a collector that stops at the first violation, a stage
// ordering that sorts non-deterministically, a shape validator that panics on
// the second violation because it dereferences through what the first one
// proved absent. Each of those failures looks exactly like success from the
// outside - the call refused, and the message named something true.

// violationCount reports how many separate violations a refusal carries. A
// refusal that is not an aggregate carries exactly one, which is the contract
// refusalSet.result() promises: a numbered list of one is noise, so a single
// violation is returned unwrapped and every existing caller sees no change.
func violationCount(err error) int {
	if err == nil {
		return 0
	}
	var aggregate *AggregateRefusal
	if errors.As(err, &aggregate) {
		return len(aggregate.Violations)
	}
	return 1
}

// requireOrder asserts that the listed fragments appear in the refusal in the
// order given. Order is not decoration here: the list is meant to be repaired
// top-down, and a caller that fixes the last item first may find the earlier
// ones were the reason it looked wrong.
func requireOrder(t *testing.T, message string, fragments ...string) {
	t.Helper()
	previous := -1
	for _, fragment := range fragments {
		index := strings.Index(message, fragment)
		if index < 0 {
			t.Fatalf("refusal omits %q:\n%s", fragment, message)
		}
		if index < previous {
			t.Fatalf("refusal reports %q out of repair order:\n%s", fragment, message)
		}
		previous = index
	}
}

// TestManagerApplyReportsEveryShapeViolationAtOnce is the central claim. Three
// violations that historically arrived one per round trip - a malformed brief,
// a missing compare-and-swap digest, and a stale head - now arrive together,
// ordered so that the artifact problem is fixed before the bookkeeping one and
// the canonical-state fact comes last.
func TestManagerApplyReportsEveryShapeViolationAtOnce(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.request
	request.RunPreparation = &RunPreparation{
		Brief:       strings.TrimSuffix(fixture.request.RunPreparation.Brief, "\n"),
		ContextPack: fixture.request.RunPreparation.ContextPack,
	}
	request.ExpectedRecordDigests = nil
	request.ExpectedHeadRevision = fixture.request.ExpectedHeadRevision + 7

	_, err := fixture.service.ManagerApply(context.Background(), request)
	if err == nil {
		t.Fatal("a request wrong in three independent ways was accepted")
	}
	if got := violationCount(err); got != 3 {
		t.Fatalf("expected 3 violations in one refusal, got %d:\n%s", got, err)
	}
	requireOrder(t, err.Error(),
		"run brief must be non-empty canonical UTF-8",
		"expectedRecordDigests",
		"Canonical state moved between the read")
	// The stale head keeps its identity through the aggregate. Without
	// Unwrap() []error every caller that distinguishes "somebody else went
	// first" from "your payload is wrong" would silently start treating a
	// conflict as a shape bug.
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("aggregated refusal lost its ErrStateConflict identity:\n%s", err)
	}
}

// TestManagerApplySingleViolationStaysUnwrapped pins the property that keeps
// this change invisible to every caller that was already getting one thing
// wrong at a time - which is most of them, and all of the existing tests.
func TestManagerApplySingleViolationStaysUnwrapped(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.request
	request.ExpectedRecordDigests = nil

	_, err := fixture.service.ManagerApply(context.Background(), request)
	if err == nil {
		t.Fatal("run.prepare without the work item's expected digest was accepted")
	}
	var aggregate *AggregateRefusal
	if errors.As(err, &aggregate) {
		t.Fatalf("a request wrong in exactly one way refused with a list:\n%s", err)
	}
	if !strings.Contains(err.Error(), "expectedRecordDigests") {
		t.Fatalf("single refusal lost its message: %v", err)
	}
}

// TestManagerApplyAddsAtMostOneStateViolation guards the half of the split that
// is easy to lose. Once the head is stale, every later state check is being
// evaluated against a premise that has already been shown false, so exactly one
// state fact may join the list and no more. A cascade would read as five
// independent problems when there is one.
func TestManagerApplyAddsAtMostOneStateViolation(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.request
	// Stale head, wrong actor, and a work item revision that cannot follow the
	// canonical one: three state-side problems, of which the caller may be told
	// exactly one.
	request.ExpectedHeadRevision = fixture.request.ExpectedHeadRevision + 7
	request.Actor = "not-a-permitted-manager"
	request.WorkItems[0].Revision += 5
	request.ExpectedRecordDigests = nil

	_, err := fixture.service.ManagerApply(context.Background(), request)
	if err == nil {
		t.Fatal("a request contradicting canonical state three ways was accepted")
	}
	message := err.Error()
	stateFacts := 0
	for _, fragment := range []string{
		"Canonical state moved between the read",
		"is not a permitted manager of campaign",
		"the canonical revision is",
	} {
		if strings.Contains(message, fragment) {
			stateFacts++
		}
	}
	if stateFacts != 1 {
		t.Fatalf("expected exactly one canonical-state fact, found %d:\n%s", stateFacts, message)
	}
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("the one state fact carried was not the stale head:\n%s", message)
	}
}

// TestManagerApplyRefusesMaximallyMalformedRequestWithoutPanic is the test the
// design note asked for by name.
//
// Every shape validator in mutations_shape.go runs against this request, and
// this request satisfies none of their premises: no actor, no ids, no head, no
// digests, a run array with a zero run in it, a nil-free but entirely empty
// review packet, an intake with no amendments, and a run preparation whose
// context pack is the zero value. A validator that assumes an earlier check
// passed - indexes request.Runs[0] after the cardinality gate failed,
// dereferences request.Review after the presence gate failed, reads
// Amendments[len-1] on an empty slice - crashes here rather than refusing, and
// a crash in a mutation path is strictly worse than any refusal.
func TestManagerApplyRefusesMaximallyMalformedRequestWithoutPanic(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	for _, action := range SupportedManagerActions() {
		request := ManagerApplyRequest{
			Action:                  action,
			ExpectedHeadRevision:    -1,
			ExpectedHeadDigest:      "not-a-digest",
			ExpectedRecordDigests:   map[string]string{"": ""},
			Campaign:                &CampaignRecord{},
			WorkItems:               []WorkItemRecord{{}, {}},
			Runs:                    []RunRecord{{}, {}},
			Findings:                []FindingSubmission{{}},
			Intake:                  &IntakeRecord{},
			Review:                  &ReviewRecord{},
			ReviewPacket:            &ReviewPacketSubmission{},
			RunPreparation:          &RunPreparation{},
			ArchiveFallbackDecision: &ArchiveFallbackOptInDecision{},
		}
		_, err := service.ManagerApply(context.Background(), request)
		if err == nil {
			t.Fatalf("%s accepted a maximally malformed request", action)
		}
		if strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("%s refused with an empty message", action)
		}
		if violationCount(err) < 2 {
			t.Fatalf("%s reported one violation for a request wrong in every way:\n%s", action, err)
		}
	}
}

// TestManagerApplyMalformedRequestRepairOrder pins the ordering contract on the
// worst case: identity first, then which records the action wants, then what is
// wrong inside them, then the bytes, then the per-record bookkeeping. A caller
// reading this list under pressure repairs it from the top.
func TestManagerApplyMalformedRequestRepairOrder(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	_, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action:         "run.prepare",
		Runs:           []RunRecord{{}},
		RunPreparation: &RunPreparation{},
	})
	if err == nil {
		t.Fatal("run.prepare with an empty envelope and an empty run was accepted")
	}
	requireOrder(t, err.Error(),
		"manager_apply requires actor",
		"is a mutation and requires",
		"requires exactly one run and at least one work item",
		"run brief must be non-empty canonical UTF-8",
	)
}

// TestRunPrepareHistoricalSevenRoundTripsCollapse reconstructs the request that
// cost a recorded session seven sequential refusals and roughly fifty thousand
// tokens, and measures what it costs now.
//
// Two of the seven - the closure job id and the archive destination - belong to
// closure_apply, not manager_apply, and are named here only so the count is not
// quietly redefined. Of the five that are manager_apply's, four are
// simultaneously expressible in one request and all four arrive together. The
// fifth, a missing runs array, is mutually exclusive with two of the others by
// construction: with no run there is no brief to be malformed and no
// primaryWorkItemId for a work item to be missing against. That case is
// measured separately below.
func TestRunPrepareHistoricalSevenRoundTripsCollapse(t *testing.T) {
	fixture := newRunPreparationFixture(t)

	// Historical refusals 3, 4, 6, and 7, all true of the same request:
	//   6. the submitted work item is not the run's primaryWorkItemId
	//   7. the brief has no trailing LF
	//   3. the submitted work item is an update with no expectedRecordDigests
	//      entry
	//   4. the expected head revision is not the current one
	stray := fixture.request.WorkItems[0]
	stray.ID, stray.Revision = "W-0002", 2
	request := fixture.request
	request.WorkItems = []WorkItemRecord{stray}
	request.RunPreparation = &RunPreparation{
		Brief:       strings.TrimSuffix(fixture.request.RunPreparation.Brief, "\n"),
		ContextPack: fixture.request.RunPreparation.ContextPack,
	}
	request.ExpectedRecordDigests = nil
	request.ExpectedHeadRevision = fixture.request.ExpectedHeadRevision + 4

	_, err := fixture.service.ManagerApply(context.Background(), request)
	if err == nil {
		t.Fatal("the historical run.prepare payload was accepted")
	}
	t.Logf("run.prepare aggregated refusal (%d violations):\n%s", violationCount(err), err)
	for _, fragment := range []string{
		"must publish the next revision of work item W-0001",
		"run brief must be non-empty canonical UTF-8",
		"expectedRecordDigests",
		"Canonical state moved between the read",
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the aggregated refusal omits %q:\n%s", fragment, err)
		}
	}
	if got := violationCount(err); got != 4 {
		t.Fatalf("expected 4 of the historical refusals in one response, got %d:\n%s", got, err)
	}

	// Historical refusal 5, which is mutually exclusive with 6 and 7: with no
	// run there is no brief to be malformed and no primaryWorkItemId for a work
	// item to be missing against. It still refuses with a list rather than a
	// fact, because the missing compare-and-swap digest and the stale head are
	// independent of the run.
	withoutRun := fixture.request
	withoutRun.Runs = nil
	withoutRun.RunPreparation = nil
	withoutRun.WorkItems = []WorkItemRecord{stray}
	withoutRun.ExpectedRecordDigests = nil
	withoutRun.ExpectedHeadRevision = fixture.request.ExpectedHeadRevision + 4
	_, err = fixture.service.ManagerApply(context.Background(), withoutRun)
	if err == nil {
		t.Fatal("run.prepare with no run was accepted")
	}
	t.Logf("run.prepare with no runs array (%d violations):\n%s", violationCount(err), err)
	if violationCount(err) < 3 {
		t.Fatalf("a missing runs array still refuses one fact at a time:\n%s", err)
	}
}

// TestCurationSubmitReportsEveryShapeViolationAtOnce throws an empty packet at
// the curator surface. Nothing in it is present: no actor, no ids, no head, an
// intake that binds no campaign, and a candidate whose path cannot be
// canonical. Every one of those used to be a separate round trip carrying the
// whole packet back.
func TestCurationSubmitReportsEveryShapeViolationAtOnce(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	_, err := service.CurationSubmit(context.Background(), CurationSubmitRequest{
		ExpectedHeadRevision: -1,
		ExpectedHeadDigest:   "not-a-digest",
		Intake:               IntakeRecord{},
		Candidates:           []FindingSubmission{{}},
		Rows:                 []CurationRow{{}},
		WorkItems:            []WorkItemRecord{{}},
	})
	if err == nil {
		t.Fatal("an empty curator packet was accepted")
	}
	if got := violationCount(err); got < 3 {
		t.Fatalf("empty curator packet reported %d violations:\n%s", got, err)
	}
	requireOrder(t, err.Error(),
		"curation_submit is a mutation and requires",
		"only manager review may create work records",
	)
}

// TestCurationSubmitSingleViolationStaysUnwrapped keeps the invisibility
// property on the curator surface too: the existing suite asserts several of
// these messages verbatim, and a packet wrong in one way must still refuse with
// exactly that sentence and nothing around it.
func TestCurationSubmitSingleViolationStaysUnwrapped(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	_, err := service.CurationSubmit(context.Background(), CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-curation", IdempotencyKey: "idem-curation",
		ExpectedHeadDigest: stateTestDigest("1"),
		Intake:             IntakeRecord{},
	})
	if err == nil {
		t.Fatal("a curator packet with no intake identity was accepted")
	}
	var aggregate *AggregateRefusal
	if errors.As(err, &aggregate) && len(aggregate.Violations) < 2 {
		t.Fatalf("a one-item list was returned as an aggregate:\n%s", err)
	}
}

// TestClosureApplyCollapsesTheStartPreconditions is the other half of the
// recorded seven. "needs a job id" and "needs an archive destination" were
// refusals one and two of that session, separated in the code by the
// existing-job and campaign-status gates, so they could not be learned
// together. They are both facts about the request.
func TestClosureApplyCollapsesTheStartPreconditions(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	_, err := service.ClosureApply(context.Background(), ClosureApplyRequest{
		Action: "start", Actor: "manager", CampaignID: "C-TEST", CampaignSlug: "test-campaign",
		CorrelationID: "corr-closure", IdempotencyKey: "idem-closure",
		ExpectedHeadDigest: stateTestDigest("1"),
		Timestamp:          "2026-08-02T18:00:00Z",
	})
	if err == nil {
		t.Fatal("closure start with neither a job id nor an archive destination was accepted")
	}
	t.Logf("closure start aggregated refusal (%d violations):\n%s", violationCount(err), err)
	if got := violationCount(err); got != 2 {
		t.Fatalf("expected the two start preconditions in one refusal, got %d:\n%s", got, err)
	}
	requireOrder(t, err.Error(),
		"closure start requires closureJobId",
		"closure start requires archiveDestination",
	)
}

// TestClosureApplyRefusesMaximallyMalformedRequestWithoutPanic runs the same
// no-panic sweep over every mutating closure action. status is excluded because
// it is a read that compiles a bounded state view rather than a mutation.
func TestClosureApplyRefusesMaximallyMalformedRequestWithoutPanic(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	for _, action := range ClosureActions {
		if action == "status" {
			continue
		}
		_, err := service.ClosureApply(context.Background(), ClosureApplyRequest{
			Action:                  action,
			ExpectedHeadRevision:    -1,
			ExpectedHeadDigest:      "not-a-digest",
			Timestamp:               "yesterday",
			ExpectedRecordDigests:   map[string]string{"": ""},
			ExpectedArtifactDigests: map[string]string{"": ""},
			FileRetention:           map[string]string{"": ""},
			ActiveFileDispositions:  map[string]string{"": ""},
			ExportedWorkItemIDs:     []string{""},
			ProjectionDestinations:  map[string]string{"": ""},
		})
		if err == nil {
			t.Fatalf("closure %s accepted a maximally malformed request", action)
		}
		if violationCount(err) < 2 {
			t.Fatalf("closure %s reported one violation for a request wrong in every way:\n%s",
				action, err)
		}
	}
}

// TestRunReturnShapeRulesSurvivedTheMoveOutOfAugmentation pins the two rules
// that moved out of augmentRunReturnCuration when that helper became a pure
// transformation. Neither had a test of its own, which is exactly why they are
// the two most likely to be lost in a refactor that turns a validating helper
// into a non-validating one: nothing would have failed.
func TestRunReturnShapeRulesSurvivedTheMoveOutOfAugmentation(t *testing.T) {
	returned := stateTestReturnedRun(2, "returned")
	base := ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignID: returned.CampaignID,
		CampaignSlug: "test-campaign", CorrelationID: "corr-test",
		Runs:      []RunRecord{returned},
		WorkItems: []WorkItemRecord{stateTestWorkItem(returned.PrimaryWorkItemID)},
	}

	withoutReport := base
	withoutReport.Runs = []RunRecord{returned}
	withoutReport.Runs[0].Report = nil
	err := validateManagerActionPayload(
		withoutReport, managerActionKinds["run.return"], Configuration{})
	if err == nil || !strings.Contains(err.Error(), "frozen report handle") {
		t.Fatalf("run.return without a frozen report handle was accepted: %v", err)
	}

	queued := base
	queued.Runs = []RunRecord{returned}
	queued.WorkItems = append(append([]WorkItemRecord(nil), base.WorkItems...),
		stateTestWorkItem(continuousCurationWorkID(returned.ID)))
	err = validateManagerActionPayload(queued, managerActionKinds["run.return"], Configuration{})
	if err == nil || !strings.Contains(err.Error(), "system-owned") {
		t.Fatalf("a caller-submitted system-owned curation queue id was accepted: %v", err)
	}
}
