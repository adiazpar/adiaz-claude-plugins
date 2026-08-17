package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// intakeRatification is the pair of records one manager ratification produces
// over a candidate-free curator intake: the immutable receipt, and the intake
// revision it advances to `reviewed`.
type intakeRatification struct {
	Committed IntakeRecord
	Reviewed  IntakeRecord
	Review    ReviewRecord
	Envelope  ReviewPacketEnvelope
}

// buildIntakeRatification echoes one persisted curator intake and seals the
// packet envelope, review receipt, and resulting reviewed revision a manager
// would submit for it. It is shared by the live path and the legacy-adoption
// path below so that the two differ only in how the records are committed, never
// in what they say.
func buildIntakeRatification(
	t *testing.T,
	fixture runPreparationFixture,
	intakeID, reviewID, correlation, at string,
	ordinal int,
) intakeRatification {
	t.Helper()
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	committed := graph.Intakes[intakeID]
	envelope := testReviewPacketEnvelope(t, CurationPacket{Intake: committed})
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: reviewID, Revision: 1,
			CreatedAt: at, UpdatedAt: at, CreatedBy: "manager", UpdatedBy: "manager",
			CorrelationID: correlation, Digest: stateTestDigest("2"),
		},
		CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: committed.ID, IntakeRevision: committed.Revision, PacketDigest: envelope.Digest,
		ReviewLoad: stateTestReviewLoad(reviewID, "C-TEST", envelope.Digest, 0, 0),
	}
	review.ReviewLoad.PacketOrdinal = ordinal
	review.ReviewLoad.StartedAt = "2026-08-02T18:06:10Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T18:06:20Z"
	review.ReviewLoad.DurationSeconds = 10
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	reviewed := committed
	reviewed.RecordMeta = lifecycleAdvanceMeta(reviewed.RecordMeta, at, "manager", correlation)
	reviewed.Digest, reviewed.Status = stateTestDigest("3"), "reviewed"
	return intakeRatification{
		Committed: committed, Reviewed: reviewed, Review: review, Envelope: envelope,
	}
}

// reviewCommittedIntake ratifies one committed curator intake exactly as a
// manager would: it echoes the persisted record, seals a packet envelope over
// it, and commits the immutable receipt together with the reviewed intake.
func reviewCommittedIntake(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	intakeID, reviewID, correlation, idempotencyKey, at string,
	ordinal int,
) StateTransactionReceipt {
	t.Helper()
	ratification := buildIntakeRatification(t, fixture, intakeID, reviewID, correlation, at, ordinal)
	applied, err := fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: correlation, IdempotencyKey: idempotencyKey,
		Rationale:             "The curator packet is accepted into the campaign record.",
		ExpectedHeadRevision:  receipt.ResultingHead.Revision,
		ExpectedHeadDigest:    receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{ratification.Committed.ID: ratification.Committed.Digest},
		Intake:                &ratification.Reviewed, Review: &ratification.Review,
		ReviewPacket: &ReviewPacketSubmission{
			Envelope: ratification.Envelope, Intake: ratification.Committed,
		},
	})
	if err != nil {
		t.Fatalf("review intake %s: %v", intakeID, err)
	}
	return applied
}

// reviewCommittedIntakeAsLegacy installs a reviewed intake that still carries
// unresolved coverage spans - the exact state this package's repairs exist for,
// and the exact state the live path may no longer produce.
//
// ValidateIntakeTransition refuses submitted -> reviewed while any span is
// unresolved, so review.submit cannot create this campaign any more, which is
// the whole point of that guard. It is reproduced here the only way it can still
// arise, and the way it actually arose in the field: canonical records that an
// engine build without the guard already committed, adopted through the engine's
// own reconcile.import path. The records are byte-for-byte what review.submit
// would have written, so nothing downstream can tell the difference.
func reviewCommittedIntakeAsLegacy(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	intakeID, reviewID, correlation, idempotencyKey, at string,
	ordinal int,
) StateTransactionReceipt {
	t.Helper()
	ratification := buildIntakeRatification(t, fixture, intakeID, reviewID, correlation, at, ordinal)
	if len(unresolvedCoverageHandles(ratification.Reviewed)) == 0 {
		t.Fatalf("intake %s carries no unresolved span; ratify it through the live path instead", intakeID)
	}
	return installLegacyRatifiedIntake(t, fixture, receipt, correlation, idempotencyKey,
		ratification.Reviewed, ratification.Review, nil)
}

// writeLegacyRecord overwrites one canonical record file in place, standing in
// for the engine build that committed it before the rule under test existed.
func writeLegacyRecord(t *testing.T, root, directory, name string, body []byte) {
	t.Helper()
	// The record directory may not exist yet: a campaign that has never had a
	// review has no reviews/ directory, and the engine creates one only when it
	// writes through it.
	absolute := filepath.Join(root, "active", "test-campaign", directory)
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(absolute, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// installLegacyRatifiedIntake commits a reviewed intake, its immutable review
// receipt, and any findings the review ratified, as records that already exist
// rather than as a transition.
//
// validateAndApplyWrites treats a reconcile.import write whose record is
// byte-identical to the one on disk as an adoption and skips transition
// validation for it, which is precisely what an adoption means: the bytes are
// asserted, not re-derived. That is the only remaining route to a reviewed
// intake carrying unresolved spans, and it is a route a real campaign has taken,
// so the fixture takes it too instead of weakening the guard it exists to test.
func installLegacyRatifiedIntake(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	correlation, idempotencyKey string,
	intake IntakeRecord,
	review ReviewRecord,
	findings []FindingSubmission,
) StateTransactionReceipt {
	t.Helper()
	expected := map[string]string{}

	intakeValue, intakeBody, err := sealIntakeRecord(intake)
	if err != nil {
		t.Fatal(err)
	}
	sealedIntake := intakeValue.(IntakeRecord)
	writeLegacyRecord(t, fixture.root, "intake", sealedIntake.ID+".json", intakeBody)
	expected[sealedIntake.ID] = sealedIntake.Digest

	// injectTransactionOwnedFields stamps a review's resultingEventIds with the
	// event of the transaction that carries it, but only when the field is empty.
	// A record already on disk carries the event of the transaction that first
	// wrote it, so the adoption must supply one or the prepared record would
	// differ from the committed bytes by exactly that field and stop being an
	// exact reconciliation.
	if len(review.ResultingEventIDs) == 0 {
		head, headErr := fixture.store.LoadHead()
		if headErr != nil {
			t.Fatal(headErr)
		}
		review.ResultingEventIDs = []string{head.EventID}
	}
	reviewValue, reviewBody, err := sealReviewRecord(review)
	if err != nil {
		t.Fatal(err)
	}
	sealedReview := reviewValue.(ReviewRecord)
	writeLegacyRecord(t, fixture.root, "reviews", sealedReview.ID+".json", reviewBody)
	expected[sealedReview.ID] = sealedReview.Digest

	adopted := make([]FindingSubmission, 0, len(findings))
	for _, submission := range findings {
		path := "active/test-campaign/findings/" + submission.Record.ID + ".md"
		value, body, sealErr := sealFindingStateRecord(submission.Document(), path)
		if sealErr != nil {
			t.Fatal(sealErr)
		}
		document := value.(FindingDocument)
		writeLegacyRecord(t, fixture.root, "findings", document.Record.ID+".md", body)
		expected[document.Record.ID] = document.Record.Digest
		adopted = append(adopted, FindingSubmission{
			Record: document.Record, Body: document.Record.Body, Path: document.Record.Path,
			SyntheticQuestions: document.SyntheticQuestions,
			QuestionsReviewed:  document.QuestionsReviewed,
		})
	}

	applied, err := fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "reconcile.import", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: correlation, IdempotencyKey: idempotencyKey,
		Rationale:             "Adopt a packet an earlier engine build ratified over unresolved coverage.",
		ExpectedHeadRevision:  receipt.ResultingHead.Revision,
		ExpectedHeadDigest:    receipt.ResultingHead.Digest,
		ExpectedRecordDigests: expected,
		Intake:                &sealedIntake, Review: &sealedReview, Findings: adopted,
	})
	if err != nil {
		t.Fatalf("adopt the legacy ratified intake %s: %v", sealedIntake.ID, err)
	}
	return applied
}

// strandRunAsLegacyCompleted installs exactly the state this fix exists to
// repair: a run completed while its only curator intake still carried an
// unresolved span.
//
// validateAppliedRunCompletion refuses that transition now, so the state is no
// longer reachable through run.complete at all - which is the point of that
// guard. It is reproduced here the only way it can still arise, and the way it
// actually arose in the field: canonical records that an engine build without
// the guard already committed, adopted through the engine's own
// reconcile.import recovery path.
func strandRunAsLegacyCompleted(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	at string,
) StateTransactionReceipt {
	t.Helper()
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	returned := graph.Runs[runID]
	if returned.Status != "returned" {
		t.Fatalf("fixture run is %s, not returned", returned.Status)
	}
	completed := returned
	completed.Status, completed.TerminalAt, completed.Digest = "completed", at, ""
	completedValue, runBody, err := sealRunRecord(completed)
	if err != nil {
		t.Fatal(err)
	}
	completed = completedValue.(RunRecord)

	work := graph.WorkItems[returned.PrimaryWorkItemID]
	work.ActiveRunIDs, work.Digest = nil, ""
	work.CompletedRunIDs = append(append([]string(nil), work.CompletedRunIDs...), runID)
	workValue, workBody, err := sealWorkItemRecord(work)
	if err != nil {
		t.Fatal(err)
	}
	work = workValue.(WorkItemRecord)

	runPath := filepath.Join(fixture.root, "active", "test-campaign", "runs", runID, "run.json")
	workPath := filepath.Join(fixture.root, "active", "test-campaign", "work-items", work.ID+".json")
	if err := os.WriteFile(runPath, runBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workPath, workBody, 0o644); err != nil {
		t.Fatal(err)
	}

	adopted, err := fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "reconcile.import", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-legacy-strand", IdempotencyKey: "idem-legacy-strand",
		Rationale:            "Adopt a run an earlier engine build completed over unresolved coverage.",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			completed.ID: completed.Digest, work.ID: work.Digest,
		},
		Runs: []RunRecord{completed}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("adopt the legacy completed run: %v", err)
	}
	return adopted
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestSupplementaryCurationRepairsAStrandedCompletedRun is the end-to-end
// regression for a campaign that could not close.
//
// A run whose intake left one span `unresolved` was reviewed and then
// completed. Closure classifies it `missing-reviewed-intake` forever:
// reviewedReportCoverage ignores any source with an unresolved span,
// `completed` has no edge back to `returned`, review cannot create an intake,
// no manager disposition seeds SourceRunCoverage, and curation refused any run
// that was not returned. The repair is a second, clean intake over the same
// frozen report - additive provenance that leaves every ratified conclusion
// exactly where it was.
func TestSupplementaryCurationRepairsAStrandedCompletedRun(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	runID := fixture.request.Runs[0].ID

	dirty := completionTestIntake("I-0901", "corr-stranded-dirty", *source.run.Report, "unresolved")
	dirtyReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: dirty.CorrelationID, IdempotencyKey: "idem-stranded-dirty",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               dirty,
	})
	if err != nil {
		t.Fatalf("submit the curator intake with an unresolved span: %v", err)
	}
	// Ratifying this packet is refused at the transition now, so the strand can
	// only be adopted from a legacy record, never created. See
	// TestRatificationRefusesUnresolvedCoverage for the refusal itself.
	reviewReceipt := reviewCommittedIntakeAsLegacy(t, fixture, dirtyReceipt,
		"I-0901", "V-0901", "corr-stranded-dirty-review", "idem-stranded-dirty-review",
		"2026-08-02T18:07:00Z", 1)

	// The two guards are complementary, not overlapping: run.complete still
	// refuses to create this situation, so the campaign under repair can only
	// be one an engine without that guard already stranded.
	if err := completeReturnedRun(t, fixture, reviewReceipt, "idem-stranded-complete"); err == nil {
		t.Fatal("run.complete accepted a run whose only intake left a span unresolved")
	} else if !strings.Contains(err.Error(), "I-0901 leaves 1 span(s) unresolved") {
		t.Fatalf("run.complete refusal is not the coverage guard: %v", err)
	}

	strandedReceipt := strandRunAsLegacyCompleted(t, fixture, reviewReceipt, "2026-08-02T18:08:00Z")

	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Runs[runID].Status != "completed" {
		t.Fatalf("legacy fixture run is %s, not completed", graph.Runs[runID].Status)
	}
	stranded, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stranded.SourceRunCoverage[runID] != "missing-reviewed-intake" ||
		!containsString(stranded.MissingDecisions, "run:"+runID+":coverage") {
		t.Fatalf("the fixture is not stranded: %s", stranded.SourceRunCoverage[runID])
	}

	// Whatever the repair does, it must not touch what the manager already
	// ratified. Compare the committed bytes, not a projection of them.
	reviewPath := filepath.Join(fixture.root, "active", "test-campaign", "reviews", "V-0901.json")
	dirtyIntakePath := filepath.Join(fixture.root, "active", "test-campaign", "intake", "I-0901.json")
	priorReview := mustReadFile(t, reviewPath)
	priorIntake := mustReadFile(t, dirtyIntakePath)

	clean := completionTestIntake("I-0902", "corr-stranded-repair", *source.run.Report, "non-claim")
	repairReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: clean.CorrelationID, IdempotencyKey: "idem-stranded-repair",
		ExpectedHeadRevision: strandedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   strandedReceipt.ResultingHead.Digest,
		Intake:               clean,
	})
	if err != nil {
		t.Fatalf("a stranded completed run refused its only remaining repair: %v", err)
	}
	repairedReceipt := reviewCommittedIntake(t, fixture, repairReceipt,
		"I-0902", "V-0902", "corr-stranded-repair-review", "idem-stranded-repair-review",
		"2026-08-02T18:09:00Z", 2)

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.SourceRunCoverage[runID] != "reviewed-intake" ||
		containsString(repaired.MissingDecisions, "run:"+runID+":coverage") {
		t.Fatalf("the supplementary intake did not clear closure coverage: %#v", repaired)
	}

	// The prior conclusion is untouched: same bytes, same binding, same
	// unresolved span. The repair added provenance; it retired nothing.
	if string(mustReadFile(t, reviewPath)) != string(priorReview) {
		t.Fatal("the ratified review record changed during the repair")
	}
	if string(mustReadFile(t, dirtyIntakePath)) != string(priorIntake) {
		t.Fatal("the reviewed intake record changed during the repair")
	}
	survivor, present := graph.Intakes["I-0901"]
	if !present || survivor.Status != "reviewed" {
		t.Fatalf("the originally reviewed intake did not survive as reviewed: %+v", survivor)
	}
	unresolved := 0
	for _, entry := range survivor.Coverage {
		if entry.Disposition == "unresolved" {
			unresolved++
		}
	}
	if unresolved != 1 {
		t.Fatalf("the original intake's unresolved span was rewritten: %d remain", unresolved)
	}
	if receipt, present := graph.Reviews["V-0901"]; !present ||
		receipt.IntakeID != "I-0901" || receipt.IntakeRevision != 1 {
		t.Fatalf("the original review no longer binds its intake revision: %+v", receipt)
	}

	// And the door closes behind the repair: the run is healthy again, so a
	// further supplementary intake has nothing to supply.
	surplus := completionTestIntake("I-0903", "corr-stranded-surplus", *source.run.Report, "non-claim")
	_, surplusErr := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: surplus.CorrelationID, IdempotencyKey: "idem-stranded-surplus",
		ExpectedHeadRevision: repairedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   repairedReceipt.ResultingHead.Digest,
		Intake:               surplus,
	})
	if surplusErr == nil {
		t.Fatal("a healthy completed run accepted a supplementary intake")
	}
	for _, want := range []string{
		runID, "is completed", "already covered by clean curator intake(s) I-0902", "overturn",
	} {
		if !strings.Contains(surplusErr.Error(), want) {
			t.Fatalf("surplus refusal does not report %q: %v", want, surplusErr)
		}
	}
}

// invalidateReturnedRun voids a returned run the only way a manager can.
// `run.invalidate` has no action of its own: it routes through `run.complete`,
// which accepts `invalidated` for a returned run unconditionally because it is
// the one exit from `returned` that the completion guard may not refuse.
func invalidateReturnedRun(
	t *testing.T,
	fixture runPreparationFixture,
	receipt StateTransactionReceipt,
	invalidatedBy, at, idempotencyKey string,
) (StateTransactionReceipt, error) {
	t.Helper()
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	returned := graph.Runs[runID]
	if returned.Status != "returned" {
		t.Fatalf("fixture run is %s, not returned", returned.Status)
	}
	invalidated := returned
	invalidated.RecordMeta = lifecycleAdvanceMeta(
		invalidated.RecordMeta, at, "manager", returned.CorrelationID)
	invalidated.Status, invalidated.TerminalAt = "invalidated", at
	invalidated.InvalidatedBy = invalidatedBy
	invalidated.Report = &FileHandle{SHA256: returned.Report.SHA256}
	work := graph.WorkItems[returned.PrimaryWorkItemID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(work.RecordMeta, at, "manager", returned.CorrelationID)
	work.ActiveRunIDs = nil
	work.CompletedRunIDs = append(append([]string(nil), work.CompletedRunIDs...), runID)
	return fixture.service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.complete", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: returned.CorrelationID, IdempotencyKey: idempotencyKey,
		Rationale:            "The returned run is withdrawn in favour of the run that supersedes it.",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: returned.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{invalidated}, WorkItems: []WorkItemRecord{work},
	})
}

// TestRatifiedFindingsOutliveTheirSourceRunsInvalidation answers the question
// that decides how closure must treat an invalidated run: can a ratified
// finding still cite one?
//
// It can. A finding binds its source runs by id (FindingRecord.SourceRuns) and
// the campaign graph requires only that each one resolve - never that it be in
// any particular state. Nothing cascades from run invalidation to findings:
// ValidateRunTransition does not look at findings, findingClosureDisposition
// and findingIsEpistemicallyLive do not look at run status, and no transition
// retracts a ratified review. So a manager can ratify a finding out of a run's
// report and then invalidate that run, and the finding stays live, stays
// ratified, and stays eligible for truth projection.
//
// That is why closure keeps demanding a reviewed intake for an invalidated
// run's frozen report. Exempting it the way `aborted` is exempt would let a
// campaign close while a projected truth rests on report bytes the coverage
// gate no longer accounts for. `aborted` is exempt for the opposite reason: it
// is unreachable from `returned` and carries no report at all, so there is
// nothing for an intake to cover and no finding can ever cite it.
func TestRatifiedFindingsOutliveTheirSourceRunsInvalidation(t *testing.T) {
	graph := closureTestGraph(t)
	runID := "R-20260802-0001"
	run := graph.Runs[runID]
	if run.Report == nil {
		t.Fatal("fixture run has no frozen report")
	}
	run.Status, run.InvalidatedBy, run.Digest = "invalidated", "R-20260802-0002", ""
	sealed, _, err := sealRunRecord(run)
	if err != nil {
		t.Fatal(err)
	}
	graph.Runs[runID] = sealed.(RunRecord)

	if err := graph.Validate(); err != nil {
		t.Fatalf("a ratified finding may not cite an invalidated run after all: %v", err)
	}
	finding := graph.Findings["F-0001"]
	if !containsString(finding.SourceRuns, runID) {
		t.Fatalf("fixture finding does not cite the invalidated run: %v", finding.SourceRuns)
	}
	if finding.ReviewState != "manager-ratified" || !findingIsEpistemicallyLive(finding) ||
		findingClosureDisposition(finding) != "truth" {
		t.Fatalf("invalidating the run changed the standing of the finding it sourced: %s/%s/%s",
			finding.ReviewState, finding.Validity, findingClosureDisposition(finding))
	}

	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.FindingCoverage["F-0001"] != "truth" {
		t.Fatalf("closure stopped projecting a finding sourced from an invalidated run: %s",
			coverage.FindingCoverage["F-0001"])
	}
	if coverage.SourceRunCoverage[runID] != "reviewed-intake" {
		t.Fatalf("closure stopped accounting for the invalidated run's frozen report: %s",
			coverage.SourceRunCoverage[runID])
	}
}

// TestSupplementaryCurationRepairsAStrandedInvalidatedRun is the end-to-end
// regression for the strand that survived the completed-run repair.
//
// `run.complete` refuses `completed` and `blocked` over a dirty intake but must
// leave `invalidated` open - it is the only other exit from `returned`, and
// refusing it too would leave a dirty run with no exit at all. Closure,
// however, exempts only `aborted`, so invalidating that run fixes
// `missing-reviewed-intake` into the campaign exactly as completing it once
// did, and the completed-run admission did not reach it.
//
// Unlike the completed strand, this one needs no legacy fixture: it is
// reachable through the live engine today, which is what makes it worth
// closing.
func TestSupplementaryCurationRepairsAStrandedInvalidatedRun(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	runID := fixture.request.Runs[0].ID

	dirty := completionTestIntake("I-0921", "corr-invalidated-dirty", *source.run.Report, "unresolved")
	dirtyReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: dirty.CorrelationID, IdempotencyKey: "idem-invalidated-dirty",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               dirty,
	})
	if err != nil {
		t.Fatalf("submit the curator intake with an unresolved span: %v", err)
	}
	// As above: the ratification that produced this state is refused now, so the
	// dirty reviewed intake is adopted as the legacy record it can only be.
	reviewReceipt := reviewCommittedIntakeAsLegacy(t, fixture, dirtyReceipt,
		"I-0921", "V-0921", "corr-invalidated-dirty-review", "idem-invalidated-dirty-review",
		"2026-08-02T18:07:00Z", 1)

	// The transition the completion guard deliberately does not refuse.
	strandedReceipt, err := invalidateReturnedRun(t, fixture, reviewReceipt,
		"R-20260802-0092", "2026-08-02T18:08:00Z", "idem-invalidated-void")
	if err != nil {
		t.Fatalf("invalidate a returned run over a dirty intake: %v", err)
	}

	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Runs[runID].Status != "invalidated" {
		t.Fatalf("fixture run is %s, not invalidated", graph.Runs[runID].Status)
	}
	stranded, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stranded.SourceRunCoverage[runID] != "missing-reviewed-intake" ||
		!containsString(stranded.MissingDecisions, "run:"+runID+":coverage") {
		t.Fatalf("the fixture is not stranded: %s", stranded.SourceRunCoverage[runID])
	}

	// The coverage gate is not the only one keyed on this run's report: the
	// closure normalization gate exempts `aborted` and curator runs and nothing
	// else, so an invalidated run's report is demanded there too. Exempting
	// invalidated runs from ComputeClosureCoverage alone would have moved the
	// strand from the project stage to the reconcile stage, not removed it.
	queued, err := fixture.service.queueClosureNormalization(graph, "2099-01-01T18:03:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].RunID != runID {
		t.Fatalf("closure normalization did not demand the invalidated run's report: %#v", queued)
	}
	blockers, err := fixture.service.closureNormalizationBlockers(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(blockers, "normalization:"+runID+":queued") {
		t.Fatalf("closure normalize gate did not block on the invalidated run: %v", blockers)
	}

	// Whatever the repair does, it must not touch what the manager already
	// ratified. Compare the committed bytes, not a projection of them.
	reviewPath := filepath.Join(fixture.root, "active", "test-campaign", "reviews", "V-0921.json")
	dirtyIntakePath := filepath.Join(fixture.root, "active", "test-campaign", "intake", "I-0921.json")
	priorReview := mustReadFile(t, reviewPath)
	priorIntake := mustReadFile(t, dirtyIntakePath)

	clean := completionTestIntake("I-0922", "corr-invalidated-repair", *source.run.Report, "non-claim")
	repairReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: clean.CorrelationID, IdempotencyKey: "idem-invalidated-repair",
		ExpectedHeadRevision: strandedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   strandedReceipt.ResultingHead.Digest,
		Intake:               clean,
	})
	if err != nil {
		t.Fatalf("a stranded invalidated run refused its only remaining repair: %v", err)
	}
	repairedReceipt := reviewCommittedIntake(t, fixture, repairReceipt,
		"I-0922", "V-0922", "corr-invalidated-repair-review", "idem-invalidated-repair-review",
		"2026-08-02T18:09:00Z", 2)

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.SourceRunCoverage[runID] != "reviewed-intake" ||
		containsString(repaired.MissingDecisions, "run:"+runID+":coverage") {
		t.Fatalf("the supplementary intake did not clear closure coverage: %#v", repaired)
	}
	// The normalization gate's epistemic half is satisfied by the same intake:
	// the run is covered, so closure stops demanding a new queue item for it.
	// Its outstanding item is ordinary work with a route to resolution, not a
	// strand.
	requeued, err := fixture.service.queueClosureNormalization(graph, "2099-01-01T18:05:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(requeued) != 0 {
		t.Fatalf("closure still demands normalization for a covered run: %#v", requeued)
	}

	// The prior conclusion is untouched: same bytes, same binding, same
	// unresolved span. The repair added provenance; it retired nothing.
	if string(mustReadFile(t, reviewPath)) != string(priorReview) {
		t.Fatal("the ratified review record changed during the repair")
	}
	if string(mustReadFile(t, dirtyIntakePath)) != string(priorIntake) {
		t.Fatal("the reviewed intake record changed during the repair")
	}
	survivor, present := graph.Intakes["I-0921"]
	if !present || survivor.Status != "reviewed" {
		t.Fatalf("the originally reviewed intake did not survive as reviewed: %+v", survivor)
	}
	if graph.Runs[runID].Status != "invalidated" || graph.Runs[runID].InvalidatedBy != "R-20260802-0092" {
		t.Fatalf("the repair changed the run's own standing: %+v", graph.Runs[runID])
	}

	// And the door closes behind the repair: the run is covered again, so a
	// further supplementary intake has nothing to supply.
	surplus := completionTestIntake("I-0923", "corr-invalidated-surplus", *source.run.Report, "non-claim")
	_, surplusErr := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: surplus.CorrelationID, IdempotencyKey: "idem-invalidated-surplus",
		ExpectedHeadRevision: repairedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   repairedReceipt.ResultingHead.Digest,
		Intake:               surplus,
	})
	if surplusErr == nil {
		t.Fatal("a covered invalidated run accepted a supplementary intake")
	}
	for _, want := range []string{
		runID, "is invalidated", "already covered by clean curator intake(s) I-0922", "overturn",
	} {
		if !strings.Contains(surplusErr.Error(), want) {
			t.Fatalf("surplus refusal does not report %q: %v", want, surplusErr)
		}
	}
}

// admissibilityTestGraph builds the smallest graph that binds one run report to
// a chosen set of curator intakes.
func admissibilityTestGraph(status string, intakes ...IntakeRecord) (CampaignGraph, RunRecord) {
	report := FileHandle{
		Path:   "active/test-campaign/runs/R-20260802-0091/report.md",
		SHA256: stateTestDigest("8"),
	}
	run := RunRecord{
		RecordMeta: RecordMeta{ID: "R-20260802-0091"}, CampaignID: "C-TEST",
		Status: status, Report: &report,
	}
	graph := NewCampaignGraph()
	graph.Runs[run.ID] = run
	for _, intake := range intakes {
		graph.Intakes[intake.ID] = intake
	}
	return graph, run
}

func admissibilityTestIntake(id, status, disposition string) IntakeRecord {
	report := FileHandle{
		Path:   "active/test-campaign/runs/R-20260802-0091/report.md",
		SHA256: stateTestDigest("8"),
	}
	return IntakeRecord{
		RecordMeta: RecordMeta{ID: id}, CampaignID: "C-TEST", Status: status,
		SourceRuns: []FileHandle{report},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + report.Path + "#L1-L3", SourcePath: report.Path,
			SourceSHA256: report.SHA256, StartLine: 1, EndLine: 3, SourceLineCount: 3,
			Disposition: disposition,
		}},
	}
}

// TestCurationSourceRunAdmissionIsExactlyTheStrandedCase pins the predicate
// itself, including every state that must stay refused. The relaxation is worth
// only as much as its boundary: a run that has left `returned` is admitted
// precisely when closure has no clean intake to count and none can be produced
// any other way.
func TestCurationSourceRunAdmissionIsExactlyTheStrandedCase(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		status   string
		noReport bool
		intakes  []IntakeRecord
		admit    bool
		wants    []string
	}{
		{
			name: "returned is unconditional", status: "returned", admit: true,
			intakes: []IntakeRecord{admissibilityTestIntake("I-0001", "reviewed", "non-claim")},
		},
		{
			name: "completed with no intake at all", status: "completed", admit: true,
		},
		{
			name: "completed with only unresolved coverage", status: "completed", admit: true,
			intakes: []IntakeRecord{admissibilityTestIntake("I-0001", "reviewed", "unresolved")},
		},
		{
			name: "completed whose only clean intake is superseded", status: "completed", admit: true,
			intakes: []IntakeRecord{
				admissibilityTestIntake("I-0001", "superseded", "non-claim"),
				admissibilityTestIntake("I-0002", "reviewed", "unresolved"),
			},
		},
		{
			name: "completed and already cleanly reviewed", status: "completed", admit: false,
			intakes: []IntakeRecord{admissibilityTestIntake("I-0001", "reviewed", "non-claim")},
			wants: []string{
				"R-20260802-0091 is completed", "already covered by clean curator intake(s) I-0001",
				"not stranded", "overturn it rather than curating the run again",
			},
		},
		{
			name: "completed with a clean repair already pending review", status: "completed", admit: false,
			intakes: []IntakeRecord{
				admissibilityTestIntake("I-0001", "reviewed", "unresolved"),
				admissibilityTestIntake("I-0002", "submitted", "non-claim"),
			},
			wants: []string{
				"already covered by clean curator intake(s) I-0002",
				"I-0001 leaves 1 span(s) unresolved",
				"review the clean intake that already covers it (I-0002 (submitted))",
			},
		},
		{
			name: "invalidated with no intake at all", status: "invalidated", admit: true,
		},
		{
			name: "invalidated with only unresolved coverage", status: "invalidated", admit: true,
			intakes: []IntakeRecord{admissibilityTestIntake("I-0001", "reviewed", "unresolved")},
		},
		{
			name: "invalidated whose only clean intake is superseded", status: "invalidated", admit: true,
			intakes: []IntakeRecord{
				admissibilityTestIntake("I-0001", "superseded", "non-claim"),
				admissibilityTestIntake("I-0002", "reviewed", "unresolved"),
			},
		},
		{
			name: "invalidated and already cleanly reviewed", status: "invalidated", admit: false,
			intakes: []IntakeRecord{admissibilityTestIntake("I-0001", "reviewed", "non-claim")},
			wants: []string{
				"R-20260802-0091 is invalidated", "already covered by clean curator intake(s) I-0001",
				"not stranded", "overturn it rather than curating the run again",
			},
		},
		{
			// Invalidated before the run ever returned: no intake can dispose
			// spans of a report that was never frozen, and closure exempts the
			// run rather than demanding one, so the refusal must not send a
			// curator after coverage that cannot exist.
			name: "invalidated with no frozen report", status: "invalidated", noReport: true, admit: false,
			wants: []string{
				"R-20260802-0091 is invalidated", "has no frozen report",
				"closure exempts a run voided before it ever returned",
				"aborted is the more accurate record",
			},
		},
		{
			name: "prepared", status: "prepared", admit: false,
			wants: []string{"R-20260802-0091 is prepared", "return the run first"},
		},
		{
			name: "prepared with no frozen report", status: "prepared", noReport: true, admit: false,
			wants: []string{
				"R-20260802-0091 is prepared", "has no frozen report", "return the run first",
				"end it through manager_apply run.complete as aborted",
			},
		},
		{
			name: "running", status: "running", admit: false,
			wants: []string{"R-20260802-0091 is running", "return the run first"},
		},
		{
			name: "running with no frozen report", status: "running", noReport: true, admit: false,
			wants: []string{
				"R-20260802-0091 is running", "has no frozen report", "return the run first",
				"end it through manager_apply run.complete as aborted",
			},
		},
		{
			name: "blocked", status: "blocked", admit: false,
			wants: []string{
				"R-20260802-0091 is blocked",
				"run.complete already required a clean curator intake",
			},
		},
		{
			name: "aborted", status: "aborted", admit: false,
			wants: []string{"R-20260802-0091 is aborted", "exempt from closure coverage"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			graph, run := admissibilityTestGraph(testCase.status, testCase.intakes...)
			if testCase.noReport {
				run.Report = nil
				graph.Runs[run.ID] = run
			}
			err := validateCurationSourceRunAdmissible(graph, run)
			if testCase.admit {
				if err != nil {
					t.Fatalf("a curatable source run was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("an uncuratable source run was admitted")
			}
			for _, want := range testCase.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal does not report %q: %v", want, err)
				}
			}
		})
	}
}

// TestCurationAdmissionAndCompletionGuardAreExactComplements proves the two
// guards cannot drift apart or leave a run between them. Both ask
// inspectRunReportIntakeCoverage the same question, so for every intake shape a
// run can carry, exactly one of "run.complete accepts it" and "curation admits
// a supplementary intake for it" holds.
//
// It also pins that `invalidated` is admitted on exactly the same predicate as
// `completed`. The two states reach the strand differently - one is now
// prevented at the transition and repaired only for legacy campaigns, the other
// is a transition the engine must keep accepting - but "stranded" means the
// same thing for both, and a stranded-only admission that answered differently
// for them would be a second predicate to keep in step.
func TestCurationAdmissionAndCompletionGuardAreExactComplements(t *testing.T) {
	shapes := [][]IntakeRecord{
		{},
		{admissibilityTestIntake("I-0001", "reviewed", "unresolved")},
		{admissibilityTestIntake("I-0001", "submitted", "unresolved")},
		{admissibilityTestIntake("I-0001", "reviewed", "non-claim")},
		{admissibilityTestIntake("I-0001", "submitted", "non-claim")},
		{admissibilityTestIntake("I-0001", "superseded", "non-claim")},
		{
			admissibilityTestIntake("I-0001", "reviewed", "unresolved"),
			admissibilityTestIntake("I-0002", "submitted", "non-claim"),
		},
		{
			admissibilityTestIntake("I-0001", "superseded", "non-claim"),
			admissibilityTestIntake("I-0002", "reviewed", "unresolved"),
		},
	}
	for _, intakes := range shapes {
		returnedGraph, returnedRun := admissibilityTestGraph("returned", intakes...)
		completable := validateRunReportIsCurated(returnedGraph, returnedRun) == nil

		completedGraph, completedRun := admissibilityTestGraph("completed", intakes...)
		curatable := validateCurationSourceRunAdmissible(completedGraph, completedRun) == nil

		invalidatedGraph, invalidatedRun := admissibilityTestGraph("invalidated", intakes...)
		invalidatedCuratable := validateCurationSourceRunAdmissible(
			invalidatedGraph, invalidatedRun) == nil

		ids := []string{}
		for _, intake := range intakes {
			ids = append(ids, intake.ID+"/"+intake.Status)
		}
		if completable == curatable {
			t.Fatalf("guards overlap or leave a gap for intakes %v: completable=%v curatable=%v",
				ids, completable, curatable)
		}
		if invalidatedCuratable != curatable {
			t.Fatalf("the stranded admission answers differently for intakes %v: "+
				"completed=%v invalidated=%v", ids, curatable, invalidatedCuratable)
		}
	}
}

// A curator report records how the record was triaged, not what is true of the
// system under study, so closure classifies it `not-a-claim-source` and never
// asks for coverage over it. run.complete has to make the same exemption. When
// it did not, a curator run could be returned and then never completed: the
// only exit demanded an intake over the receipt, and curating that receipt
// produces another one. Closure documents avoiding exactly that recursion.
func TestRunCompletionExemptsCuratorReportsLikeClosureDoes(t *testing.T) {
	for _, test := range []struct {
		role        string
		completable bool
	}{
		{role: "curator", completable: true},
		{role: "investigator", completable: false},
	} {
		graph, run := admissibilityTestGraph("returned")
		run.Role = test.role
		graph.Runs[run.ID] = run

		completed := run
		completed.Status = "completed"
		err := validateAppliedRunCompletion(graph, []preparedStateWrite{{Record: completed}})
		if (err == nil) != test.completable {
			t.Fatalf("%s run with no covering intake: completable=%v err=%v",
				test.role, err == nil, err)
		}
	}
}
