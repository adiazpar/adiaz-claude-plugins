package knowledge

import (
	"context"
	"strings"
	"testing"
)

// startRunThatNeverReturns drives the fixture run to `running` and stops. It is
// deliberately not `returnNormalizationSourceRun`: this family of tests is
// about the run that never freezes a report at all, so it must never reach
// `returned`.
func startRunThatNeverReturns(
	t *testing.T,
	fixture runPreparationFixture,
) StateTransactionReceipt {
	t.Helper()
	ctx := context.Background()
	prepared, err := fixture.service.ManagerApply(ctx, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	preparedRun := graph.Runs[runID]
	running := preparedRun
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:01:00Z", "manager", preparedRun.CorrelationID)
	running.Status, running.StartedAt = "running", "2026-08-02T18:01:00Z"
	work := graph.WorkItems[preparedRun.PrimaryWorkItemID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:01:00Z", "manager", preparedRun.CorrelationID)
	started, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: preparedRun.CorrelationID, IdempotencyKey: "idem-never-returns-start",
		ExpectedHeadRevision: prepared.ResultingHead.Revision,
		ExpectedHeadDigest:   prepared.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: preparedRun.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatal(err)
	}
	return started
}

// terminateRunWithoutReport ends a running run through the only action that
// reaches a terminal status. `run.invalidate` and `run.abort` have no actions
// of their own: both route through `run.complete`, which accepts any of
// `completed`, `blocked`, `aborted`, and `invalidated`.
func terminateRunWithoutReport(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	status, invalidatedBy, at, idempotencyKey string,
) (StateTransactionReceipt, error) {
	t.Helper()
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	running := graph.Runs[runID]
	if running.Status != "running" {
		t.Fatalf("fixture run is %s, not running", running.Status)
	}
	if running.Report != nil {
		t.Fatal("fixture run froze a report; this case is about a run that never returned")
	}
	terminal := running
	terminal.RecordMeta = lifecycleAdvanceMeta(
		terminal.RecordMeta, at, "manager", running.CorrelationID)
	terminal.Status, terminal.TerminalAt = status, at
	terminal.InvalidatedBy = invalidatedBy
	terminal.ResultSummary = "The worker never returned and froze no report."
	work := graph.WorkItems[running.PrimaryWorkItemID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(work.RecordMeta, at, "manager", running.CorrelationID)
	work.ActiveRunIDs = nil
	work.CompletedRunIDs = append(append([]string(nil), work.CompletedRunIDs...), runID)
	return fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.complete", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: running.CorrelationID, IdempotencyKey: idempotencyKey,
		Rationale:            "The dispatched run never came back, so it is taken out of flight.",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: running.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{terminal}, WorkItems: []WorkItemRecord{work},
	})
}

// TestClosureExemptsARunInvalidatedBeforeItReturned is the end-to-end
// regression for the last strand in this family.
//
// A run that never returned has no report, and `run.complete` accepts
// `invalidated` from `running` because a manager must be able to void a run
// that will never come back. Closure then recorded `missing-report` with the
// decision `run:<id>:report`, and nothing could ever clear it: `invalidated`
// is absorbing, terminal records are immutable, and the stranded-run curation
// admission cannot reach it because there is no report to curate against.
//
// So closure now exempts it. The exemption is safe precisely because the run
// froze no bytes: no coverage span can exist over a report that was never
// written, so no curator intake can bind a candidate finding to the run
// (TestCurationCannotBindAnyFindingToAReportlessRun), and there is nothing for
// a projected truth to rest on.
func TestClosureExemptsARunInvalidatedBeforeItReturned(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	runID := fixture.request.Runs[0].ID
	started := startRunThatNeverReturns(t, fixture)

	stranded, err := terminateRunWithoutReport(t, fixture, started,
		"invalidated", "R-20260802-0092", "2026-08-02T18:08:00Z", "idem-never-returned-void")
	if err != nil {
		t.Fatalf("invalidate a run that never returned: %v", err)
	}

	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	run := graph.Runs[runID]
	if run.Status != "invalidated" || run.Report != nil {
		t.Fatalf("fixture run is %s with report %v, not invalidated without one", run.Status, run.Report)
	}

	// The state is absorbing before it is exempt: there is no transition out,
	// which is what made `missing-report` permanent.
	for status := range runTransitions {
		if status == "invalidated" {
			continue
		}
		if runTransitions["invalidated"][status] {
			t.Fatalf("invalidated is no longer absorbing; it reaches %s", status)
		}
	}
	_, reopenErr := fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.complete", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: run.CorrelationID, IdempotencyKey: "idem-never-returned-reabort",
		Rationale:            "Try to correct the void into the abort closure would have exempted.",
		ExpectedHeadRevision: stranded.ResultingHead.Revision,
		ExpectedHeadDigest:   stranded.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: run.Digest, "W-0001": graph.WorkItems["W-0001"].Digest,
		},
		Runs: []RunRecord{func() RunRecord {
			corrected := run
			corrected.RecordMeta = lifecycleAdvanceMeta(
				corrected.RecordMeta, "2026-08-02T18:09:00Z", "manager", run.CorrelationID)
			corrected.Status, corrected.InvalidatedBy = "aborted", ""
			return corrected
		}()}, WorkItems: []WorkItemRecord{graph.WorkItems["W-0001"]},
	})
	if reopenErr == nil {
		t.Fatal("an invalidated run accepted a correction to aborted")
	}
	if !strings.Contains(reopenErr.Error(), "illegal run transition invalidated -> aborted") {
		t.Fatalf("the refusal does not name the absorbing transition: %v", reopenErr)
	}

	// Curation is not a route out either: the stranded-run admission needs a
	// frozen report to curate against, and this run has none.
	if validateCurationSourceRunAdmissible(graph, run) == nil {
		t.Fatal("curation admitted a run with no report to cover")
	}

	// The gate itself: closure must not demand a report this run can never
	// produce, and must not carry an uncleanable decision.
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[runID] != "invalidated-without-report" {
		t.Fatalf("closure still strands a run invalidated before it returned: %s",
			coverage.SourceRunCoverage[runID])
	}
	if containsString(coverage.MissingDecisions, "run:"+runID+":report") ||
		containsString(coverage.MissingDecisions, "run:"+runID+":coverage") {
		t.Fatalf("closure kept an unclearable decision for the run: %v", coverage.MissingDecisions)
	}
	if containsString(closureCoverageBlockers(coverage), "run:"+runID+":report") {
		t.Fatalf("the closure blocker survived the exemption: %v", closureCoverageBlockers(coverage))
	}

	// Unlike the previous hole, the normalization queue never gated on this
	// predicate: both halves skip a run with no report before they look at its
	// status, so exempting closure does not move the strand to the reconcile
	// stage the way it would have for a run that kept its frozen report.
	queued, err := fixture.service.queueClosureNormalization(graph, "2099-01-01T18:03:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 0 {
		t.Fatalf("closure normalization demanded a report that was never frozen: %#v", queued)
	}
	blockers, err := fixture.service.closureNormalizationBlockers(graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range blockers {
		if strings.Contains(blocker, runID) {
			t.Fatalf("the normalize gate blocked on a reportless run: %v", blockers)
		}
	}
}

// TestAbortIsReachableForARunThatNeverReturned pins the alternative the engine
// now names in its refusal. `aborted` is unreachable from `returned` - which is
// why it can be exempt from reviewed-intake coverage - but it is reachable from
// exactly the two states a run that never returned can be in, so telling a
// manager to abort such a run is actionable advice rather than a dead end.
func TestAbortIsReachableForARunThatNeverReturned(t *testing.T) {
	for _, from := range []string{"prepared", "running"} {
		if !runTransitions[from]["aborted"] {
			t.Fatalf("aborted is unreachable from %s, so the engine cannot steer a caller there", from)
		}
	}
	if runTransitions["returned"]["aborted"] {
		t.Fatal("aborted became reachable from returned; its coverage exemption is no longer sound")
	}

	fixture := newRunPreparationFixture(t)
	runID := fixture.request.Runs[0].ID
	started := startRunThatNeverReturns(t, fixture)
	if _, err := terminateRunWithoutReport(t, fixture, started,
		"aborted", "", "2026-08-02T18:08:00Z", "idem-never-returned-abort"); err != nil {
		t.Fatalf("abort a run that never returned: %v", err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Runs[runID].Status != "aborted" {
		t.Fatalf("fixture run is %s, not aborted", graph.Runs[runID].Status)
	}
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[runID] != "aborted-with-reason" ||
		containsString(coverage.MissingDecisions, "run:"+runID+":report") {
		t.Fatalf("aborting a run that never returned did not clear closure: %s / %v",
			coverage.SourceRunCoverage[runID], coverage.MissingDecisions)
	}
}

// TestClosureStillDemandsAReportFromAnInFlightRun keeps the exemption to the
// terminal case. `missing-report` remains the correct answer for a run that
// has not finished: it is the gate that stops a campaign closing over work
// still in flight, and the remedy - return it, or abort it - is available.
func TestClosureStillDemandsAReportFromAnInFlightRun(t *testing.T) {
	for _, status := range []string{"prepared", "running"} {
		fixture := newRunPreparationFixture(t)
		runID := fixture.request.Runs[0].ID
		startRunThatNeverReturns(t, fixture)
		graph, err := fixture.store.LoadCampaignGraph("C-TEST")
		if err != nil {
			t.Fatal(err)
		}
		run := graph.Runs[runID]
		if status == "prepared" {
			run.Status, run.StartedAt = "prepared", ""
			graph.Runs[runID] = run
		}
		coverage, err := ComputeClosureCoverage(graph, nil)
		if err != nil {
			t.Fatal(err)
		}
		if coverage.SourceRunCoverage[runID] != "missing-report" ||
			!containsString(coverage.MissingDecisions, "run:"+runID+":report") {
			t.Fatalf("closure stopped accounting for a %s run: %s / %v",
				status, coverage.SourceRunCoverage[runID], coverage.MissingDecisions)
		}
	}
}

// TestCurationCannotBindAnyFindingToAReportlessRun is the proof the exemption
// rests on.
//
// A candidate finding's sourceRuns must equal exactly the set of runs its
// `candidate-finding` coverage entries resolve to, and every coverage entry
// resolves through an intake source that must match one canonical run report
// by path and digest. A run with no report matches nothing, so no coverage
// span, and therefore no candidate finding, can ever be bound to it - which is
// what makes exempting it from the coverage gate different from exempting an
// invalidated run that kept its frozen report.
func TestCurationCannotBindAnyFindingToAReportlessRun(t *testing.T) {
	graph, run := admissibilityTestGraph("invalidated")
	report := *run.Report
	run.Report = nil
	graph.Runs[run.ID] = run
	graph.Campaign = &CampaignRecord{
		RecordMeta: RecordMeta{ID: "C-TEST"}, Slug: "test-campaign",
	}

	// The intake names the run's own canonical report path, which is the most
	// favourable case a curator could construct.
	intake := admissibilityTestIntake("I-0001", "submitted", "candidate-finding")
	if intake.SourceRuns[0].Path != report.Path {
		t.Fatalf("fixture intake does not name the run's canonical report: %s", intake.SourceRuns[0].Path)
	}
	_, err := validateCurationGraphBindings(graph, CurationPacket{Intake: intake}, true)
	if err == nil {
		t.Fatal("curation bound an intake source to a run that never froze a report")
	}
	if !strings.Contains(err.Error(), "must resolve to exactly one canonical run report") {
		t.Fatalf("the refusal does not name the unresolvable source: %v", err)
	}

	// Control: the same intake resolves the moment the run has the report, so
	// the refusal above is caused by the absent report and nothing else.
	run.Report = &report
	graph.Runs[run.ID] = run
	if _, err := validateCurationGraphBindings(graph, CurationPacket{Intake: intake}, true); err != nil {
		t.Fatalf("the control case did not bind: %v", err)
	}
}
