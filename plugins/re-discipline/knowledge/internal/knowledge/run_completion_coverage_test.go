package knowledge

import (
	"context"
	"strings"
	"testing"
)

// TestUnresolvedCoverageStrandsACompletedRunPermanently pins the consequence
// the guard exists to prevent, so the guard cannot be dismissed as cosmetic: a
// single unresolved span demotes an otherwise fully reviewed run to
// missing-reviewed-intake, and `completed` has no transition that could ever
// let a curator repair it.
func TestUnresolvedCoverageStrandsACompletedRunPermanently(t *testing.T) {
	graph := closureTestGraph(t)
	run := graph.Runs["R-20260802-0001"]
	if run.Status != "completed" {
		t.Fatalf("fixture run is %s, not completed", run.Status)
	}
	baseline, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.SourceRunCoverage[run.ID] != "reviewed-intake" {
		t.Fatalf("fixture does not start from covered closure: %s", baseline.SourceRunCoverage[run.ID])
	}

	intake := graph.Intakes["I-0001"]
	spans := append([]CoverageEntry(nil), intake.Coverage...)
	for index := range spans {
		spans[index].SourceLineCount = 3
	}
	spans = append(spans, CoverageEntry{
		SourceHandle: "path:" + run.Report.Path + "#L3-L3", SourcePath: run.Report.Path,
		SourceSHA256: run.Report.SHA256, StartLine: 3, EndLine: 3, SourceLineCount: 3,
		Disposition: "unresolved",
		Rationale:   "The curator could not decide whether this line states a durable claim.",
	})
	intake.Coverage, intake.Digest = spans, ""
	sealed, _, err := sealIntakeRecord(intake)
	if err != nil {
		t.Fatal(err)
	}
	graph.Intakes[intake.ID] = sealed.(IntakeRecord)

	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[run.ID] != "missing-reviewed-intake" ||
		!containsString(coverage.MissingDecisions, "run:"+run.ID+":coverage") {
		t.Fatalf("one unresolved span did not block closure coverage: %#v", coverage)
	}
	// And there is no route back to the only state in which curation_submit
	// would accept a repaired intake.
	for _, terminal := range []string{"completed", "blocked"} {
		if runTransitions[terminal]["returned"] {
			t.Fatalf("%s can now return; the run.complete guard must be revisited", terminal)
		}
	}
}

// completionTestIntake covers one three-line returned report with two spans so
// the second span's disposition can be varied between clean and unresolved.
func completionTestIntake(id, correlation string, report FileHandle, tailDisposition string) IntakeRecord {
	tail := CoverageEntry{
		SourceHandle: "path:" + report.Path + "#L3-L3",
		SourcePath:   report.Path, SourceSHA256: report.SHA256,
		StartLine: 3, EndLine: 3, SourceLineCount: 3, Disposition: tailDisposition,
	}
	if tailDisposition == "unresolved" {
		tail.Rationale = "The curator could not decide whether this line states a durable claim."
	}
	return IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: id, Revision: 1,
			CreatedAt: "2026-08-02T18:06:00Z", UpdatedAt: "2026-08-02T18:06:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			CorrelationID: correlation, Digest: stateTestDigest("1"),
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{report},
		Coverage: []CoverageEntry{
			{
				SourceHandle: "path:" + report.Path + "#L1-L2",
				SourcePath:   report.Path, SourceSHA256: report.SHA256,
				StartLine: 1, EndLine: 2, SourceLineCount: 3, Disposition: "non-claim",
			},
			tail,
		},
		Triage: map[string]string{}, Status: "submitted",
	}
}

// completeReturnedRun builds the run.complete transition for an already
// returned run and applies it.
func completeReturnedRun(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	idempotencyKey string,
) error {
	t.Helper()
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	returned := graph.Runs[runID]
	completed := returned
	completed.RecordMeta = lifecycleAdvanceMeta(
		completed.RecordMeta, "2026-08-02T18:08:00Z", "manager", returned.CorrelationID)
	completed.Status, completed.TerminalAt = "completed", "2026-08-02T18:08:00Z"
	completed.Report = &FileHandle{SHA256: returned.Report.SHA256}
	work := graph.WorkItems[returned.PrimaryWorkItemID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:08:00Z", "manager", returned.CorrelationID)
	work.ActiveRunIDs = nil
	work.CompletedRunIDs = append(append([]string(nil), work.CompletedRunIDs...), runID)
	_, err = fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.complete", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: returned.CorrelationID, IdempotencyKey: idempotencyKey,
		Rationale:            "The returned run is accepted into the campaign record.",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: returned.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{completed}, WorkItems: []WorkItemRecord{work},
	})
	return err
}

// TestRunCompleteRefusesToStrandUnresolvedCoverage pins the fail-fast guard for
// the second reported defect.
//
// `returned` is the only state in which curation_submit accepts an intake over
// a run's report, and no transition leads back to it. Closure, meanwhile,
// requires a reviewed intake for every non-aborted run with a report, and
// reviewedReportCoverage skips any source that still carries an `unresolved`
// span. Completing such a run therefore made its closure coverage permanently
// unsatisfiable, with no warning at the transition that caused it.
func TestRunCompleteRefusesToStrandUnresolvedCoverage(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	runID := fixture.request.Runs[0].ID

	// Completing before any curation exists strands the run just as surely.
	uncuratedErr := completeReturnedRun(t, fixture, source.receipt, "idem-complete-uncurated")
	if uncuratedErr == nil {
		t.Fatal("a returned run with no curator intake was completed")
	}
	for _, want := range []string{
		runID,
		"no curator intake covers its report",
		source.run.Report.Path,
		"curator intake",
	} {
		if !strings.Contains(uncuratedErr.Error(), want) {
			t.Fatalf("uncurated refusal does not report %q: %v", want, uncuratedErr)
		}
	}

	dirty := completionTestIntake(
		"I-0901", "corr-completion-dirty", *source.run.Report, "unresolved")
	dirtyReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: dirty.CorrelationID, IdempotencyKey: "idem-completion-dirty-intake",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               dirty,
	})
	if err != nil {
		t.Fatalf("submit curator intake with an unresolved span: %v", err)
	}

	dirtyErr := completeReturnedRun(t, fixture, dirtyReceipt, "idem-complete-dirty")
	if dirtyErr == nil {
		t.Fatal("a returned run whose only intake left a span unresolved was completed")
	}
	for _, want := range []string{
		runID,
		"I-0901 leaves 1 span(s) unresolved",
		"path:" + source.run.Report.Path + "#L3-L3",
		"no transition returns the run",
		"disposes every span as candidate-finding, duplicate, non-claim, or out-of-scope",
	} {
		if !strings.Contains(dirtyErr.Error(), want) {
			t.Fatalf("unresolved-coverage refusal does not report %q: %v", want, dirtyErr)
		}
	}

	// The remedy the refusal names must actually clear it, while the run is
	// still returned.
	clean := completionTestIntake(
		"I-0902", "corr-completion-clean", *source.run.Report, "non-claim")
	cleanReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: clean.CorrelationID, IdempotencyKey: "idem-completion-clean-intake",
		ExpectedHeadRevision: dirtyReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   dirtyReceipt.ResultingHead.Digest,
		Intake:               clean,
	})
	if err != nil {
		t.Fatalf("submit a clean curator intake for the same returned run: %v", err)
	}
	if err := completeReturnedRun(t, fixture, cleanReceipt, "idem-complete-clean"); err != nil {
		t.Fatalf("the documented remedy did not clear the refusal: %v", err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Runs[runID].Status != "completed" {
		t.Fatalf("run did not reach completed after clean curation: %s", graph.Runs[runID].Status)
	}
}
