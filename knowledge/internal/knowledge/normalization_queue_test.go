package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type normalizationSourceFixture struct {
	run     RunRecord
	receipt StateTransactionReceipt
}

func TestNormalizationReviewCompletenessRequiresEveryCandidateExactlyOnce(t *testing.T) {
	intake := IntakeRecord{
		RecordMeta: RecordMeta{ID: "I-0001", Revision: 2}, CampaignID: "C-TEST",
		CandidateFindingIDs: []string{"F-0001", "F-0002"},
	}
	review := ReviewRecord{
		CampaignID: "C-TEST", IntakeID: intake.ID, IntakeRevision: 1,
		Decisions: []ReviewDecision{{FindingID: "F-0001"}, {FindingID: "F-0002"}},
	}
	if !normalizationReviewComplete(review, intake) {
		t.Fatal("complete manager receipt was not recognized")
	}
	review.Decisions[1].FindingID = "F-0001"
	if normalizationReviewComplete(review, intake) {
		t.Fatal("duplicate manager decisions were accepted as complete proof")
	}
	review.Decisions = []ReviewDecision{{FindingID: "F-0001"}}
	if normalizationReviewComplete(review, intake) {
		t.Fatal("manager receipt missing a candidate decision was accepted as complete proof")
	}
}

func returnNormalizationSourceRun(
	t *testing.T,
	fixture runPreparationFixture,
) normalizationSourceFixture {
	t.Helper()
	ctx := context.Background()
	preparedReceipt, err := fixture.service.ManagerApply(ctx, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	prepared := graph.Runs[runID]
	running := prepared
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	running.Status, running.StartedAt = "running", "2026-08-02T18:01:00Z"
	work := graph.WorkItems[prepared.PrimaryWorkItemID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	startedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: prepared.CorrelationID, IdempotencyKey: "idem-normalization-source-start",
		ExpectedHeadRevision: preparedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   preparedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: prepared.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := []byte("# OBSERVATION\n\nNo durable claim was established by this bounded probe.\n")
	reportPath := "active/test-campaign/runs/" + runID + "/report.md"
	writeFindingFixtureFile(t, fixture.root, reportPath, report)
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	running = graph.Runs[runID]
	returned := running
	returned.RecordMeta = lifecycleAdvanceMeta(
		returned.RecordMeta, "2026-08-02T18:02:00Z", "manager", returned.CorrelationID)
	returned.Status, returned.ReturnedAt = "returned", "2026-08-02T18:02:00Z"
	returned.Report = &FileHandle{SHA256: "sha256:" + SHA256Bytes(report)}
	returned.ResultSummary = "The bounded probe produced no durable claim."
	work = graph.WorkItems[returned.PrimaryWorkItemID]
	priorWork = work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:02:00Z", "manager", returned.CorrelationID)
	returnedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: returned.CorrelationID, IdempotencyKey: "idem-normalization-source-return",
		ExpectedHeadRevision: startedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   startedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			runID: running.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{returned}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	return normalizationSourceFixture{run: graph.Runs[runID], receipt: returnedReceipt}
}

// completeTerminallyJudgedNormalization drives one source report all the way to
// a sealed normalization resolution receipt, with every span of the report given
// the caller's chosen terminal disposition.
//
// The disposition is a parameter because `non-claim` and `out-of-scope` are the
// same kind of answer - the manager read the span and decided it contributes
// nothing to the shared record - and the resolution path must not be able to
// tell them apart. It could once, and that difference stranded campaigns at the
// normalize stage.
func completeTerminallyJudgedNormalization(
	t *testing.T,
	fixture runPreparationFixture,
	source normalizationSourceFixture,
	disposition string,
) (NormalizationSuggestion, NormalizationResolution) {
	t.Helper()
	ctx := context.Background()
	queued, err := fixture.service.NormalizationQueueApply(ctx, NormalizationQueueRequest{
		Action: "request", Actor: "manager", ReportPath: source.run.Report.Path,
		ReportDigest: source.run.Report.SHA256, Timestamp: "2026-08-02T18:02:30Z",
	})
	if err != nil || queued.Item == nil {
		t.Fatalf("queue source proof fixture: result=%#v err=%v", queued, err)
	}

	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	workID := continuousCurationWorkID(source.run.ID)
	curatorRunID := "R-20260802-0092"
	// The fixture's default drafter budget is intentionally production-sized,
	// but this proof run is prepared after the source run has materialized the
	// entire curation frontier. Give the synthetic curator enough room for every
	// mandatory state card; production configuration validation remains covered
	// independently and is not relaxed by this test-only override.
	fixture.service.Configuration.Settings.Budgets.DrafterContextTokens = 4096
	pack, err := fixture.service.ContextPackOptions(ctx, ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: "C-TEST", WorkItemID: workID, RunID: curatorRunID,
		},
		Task: "Classify every exact source report line without inventing a claim.",
		Role: "drafter", TokenBudget: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	rawBrief := "# Normalize source report\n\nClassify every exact report line and return complete coverage.\n"
	brief, err := canonicalRunBrief(rawBrief, nil)
	if err != nil {
		t.Fatal(err)
	}
	packBody, err := canonicalJSON(pack)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "active/test-campaign/runs/" + curatorRunID + "/"
	curatorRun := RunRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: curatorRunID, Revision: 1,
			CreatedAt: "2026-08-02T18:03:00Z", UpdatedAt: "2026-08-02T18:03:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "corr-normalization-curator",
		},
		CampaignID: "C-TEST", PrimaryWorkItemID: workID, ActorID: "knowledge-curator",
		Role: "curator", Status: "prepared",
		Brief: &FileHandle{Path: prefix + "brief.md", SHA256: "sha256:" + SHA256Bytes(brief)},
		ContextPack: &FileHandle{
			Path: prefix + "context-pack.json", SHA256: "sha256:" + SHA256Bytes(packBody),
		},
	}
	work := graph.WorkItems[workID]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:03:00Z", "manager", curatorRun.CorrelationID)
	work.State = "active"
	work.ActiveRunIDs = []string{curatorRunID}
	preparedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.prepare", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: curatorRun.CorrelationID, IdempotencyKey: "idem-normalization-curator-prepare",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			work.ID: priorWork,
		},
		Runs: []RunRecord{curatorRun}, WorkItems: []WorkItemRecord{work},
		RunPreparation: &RunPreparation{Brief: rawBrief, ContextPack: pack},
	})
	if err != nil {
		t.Fatal(err)
	}

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	prepared := graph.Runs[curatorRunID]
	running := prepared
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:04:00Z", "manager", running.CorrelationID)
	running.Status, running.StartedAt = "running", "2026-08-02T18:04:00Z"
	work = graph.WorkItems[workID]
	priorWork = work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:04:00Z", "manager", running.CorrelationID)
	startedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: running.CorrelationID, IdempotencyKey: "idem-normalization-curator-start",
		ExpectedHeadRevision: preparedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   preparedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			curatorRunID: prepared.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatal(err)
	}

	curatorReport := []byte("# CURATION RECEIPT\n\nAll three source lines are non-claim narrative.\n")
	curatorReportPath := prefix + "report.md"
	writeFindingFixtureFile(t, fixture.root, curatorReportPath, curatorReport)
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	running = graph.Runs[curatorRunID]
	returned := running
	returned.RecordMeta = lifecycleAdvanceMeta(
		returned.RecordMeta, "2026-08-02T18:05:00Z", "manager", returned.CorrelationID)
	returned.Status, returned.ReturnedAt = "returned", "2026-08-02T18:05:00Z"
	returned.Report = &FileHandle{SHA256: "sha256:" + SHA256Bytes(curatorReport)}
	returned.ResultSummary = "Complete source coverage found no durable claim."
	work = graph.WorkItems[workID]
	priorWork = work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:05:00Z", "manager", returned.CorrelationID)
	returnedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: returned.CorrelationID, IdempotencyKey: "idem-normalization-curator-return",
		ExpectedHeadRevision: startedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   startedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			curatorRunID: running.Digest, work.ID: priorWork,
		},
		Runs: []RunRecord{returned}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	returned = graph.Runs[curatorRunID]

	intake := IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "I-0901", Revision: 1,
			CreatedAt: "2026-08-02T18:06:00Z", UpdatedAt: "2026-08-02T18:06:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			CorrelationID: "corr-normalization-intake", Digest: stateTestDigest("1"),
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{*source.run.Report},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + source.run.Report.Path + "#L1-L3",
			SourcePath:   source.run.Report.Path, SourceSHA256: source.run.Report.SHA256,
			StartLine: 1, EndLine: 3, SourceLineCount: 3, Disposition: disposition,
			Rationale: terminalCoverageRationale(disposition),
		}},
		Triage: map[string]string{}, Status: "submitted",
	}
	curationReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: intake.CorrelationID, IdempotencyKey: "idem-normalization-intake",
		ExpectedHeadRevision: returnedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   returnedReceipt.ResultingHead.Digest,
		Intake:               intake, CuratorRun: &returned,
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	intake = graph.Intakes[intake.ID]
	packet := CurationPacket{Intake: intake}
	envelope := testReviewPacketEnvelope(t, packet)
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0901", Revision: 1,
			CreatedAt: "2026-08-02T18:07:00Z", UpdatedAt: "2026-08-02T18:07:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "corr-normalization-review",
			Digest: stateTestDigest("2"),
		},
		CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: intake.ID, IntakeRevision: intake.Revision, PacketDigest: envelope.Digest,
		ReviewLoad: stateTestReviewLoad("V-0901", "C-TEST", envelope.Digest, 0, 0),
	}
	review.ReviewLoad.StartedAt = "2026-08-02T18:06:10Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T18:06:20Z"
	review.ReviewLoad.DurationSeconds = 10
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	reviewedIntake := intake
	reviewedIntake.RecordMeta = lifecycleAdvanceMeta(
		reviewedIntake.RecordMeta, "2026-08-02T18:07:00Z", "manager", review.CorrelationID)
	reviewedIntake.Digest = stateTestDigest("3")
	reviewedIntake.Status = "reviewed"
	if len(unresolvedCoverageHandles(reviewedIntake)) > 0 {
		// ValidateIntakeTransition refuses to ratify an intake that still declares
		// unjudged spans, so an `unresolved` fixture can only be adopted as the
		// legacy record such a campaign now is. That is exactly the population the
		// disposition gate below still has to refuse, so the fixture has to be able
		// to reach it.
		installLegacyRatifiedIntake(t, fixture, curationReceipt,
			review.CorrelationID, "idem-normalization-review", reviewedIntake, review, nil)
	} else if _, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: review.CorrelationID, IdempotencyKey: "idem-normalization-review",
		ExpectedHeadRevision: curationReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   curationReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			intake.ID: intake.Digest,
		},
		Intake: &reviewedIntake, Review: &review,
		ReviewPacket: &ReviewPacketSubmission{Envelope: envelope, Intake: intake},
	}); err != nil {
		t.Fatal(err)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	canonicalIntake := graph.Intakes[intake.ID]
	canonicalReview := graph.Reviews[review.ID]
	canonicalCurator := graph.Runs[curatorRunID]
	coverageDigest, _, err := normalizationCoverageDigest(canonicalIntake, *source.run.Report)
	if err != nil {
		t.Fatal(err)
	}
	// Both terminal dispositions mean "this report yields no claim", so both seal
	// the same queue-item disposition. `reviewed-non-claim` is the wire value for
	// that outcome, not a statement that every span said `non-claim`.
	resolution := NormalizationResolution{
		SchemaVersion: CampaignSchemaVersion, Disposition: "reviewed-non-claim",
		SourceReport: *source.run.Report, CuratorRunID: canonicalCurator.ID,
		CuratorRunDigest: canonicalCurator.Digest, CuratorReport: *canonicalCurator.Report,
		IntakeID: canonicalIntake.ID, IntakeRevision: canonicalIntake.Revision,
		IntakeDigest: canonicalIntake.Digest, CoverageDigest: coverageDigest,
		ReviewID: canonicalReview.ID, ReviewRevision: canonicalReview.Revision,
		ReviewDigest: canonicalReview.Digest, ResolvedFindingIDs: []string{},
	}
	if err := sealNormalizationResolution(&resolution); err != nil {
		t.Fatal(err)
	}
	return *queued.Item, resolution
}

func TestNormalizationQueuePublicLifecycleRequiresSourceCampaignManager(t *testing.T) {
	store, root := newStateTestStore(t)
	openStateTestCampaign(t, store)
	tracker, err := OpenArchiveFallbackTracker(2,
		filepath.Join(root, ".re-discipline", "knowledge", "normalization-queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("d", 64)
	source := NormalizationSource{
		CampaignID: "C-TEST", CampaignSlug: "test-campaign", RunID: "R-20260802-0042",
		ReportPath:   "active/test-campaign/runs/R-20260802-0042/report.md",
		ReportHandle: "run:R-20260802-0042",
		SourceHandle: "path:active/test-campaign/runs/R-20260802-0042/report.md",
	}
	if _, err := tracker.RecordSource(digest, "request-1", source); err != nil {
		t.Fatal(err)
	}
	served, err := tracker.RecordSource(digest, "request-2", source)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Boundary: store.Boundary, archiveTracker: tracker}
	status, err := service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "status", Limit: 10,
	})
	if err != nil || status.Queue.Queued != 1 || len(status.Queue.Items) != 1 {
		t.Fatalf("public queue status omitted actionable work: result=%#v err=%v", status, err)
	}
	queued := status.Queue.Items[0]
	if queued.ID != served.SuggestionID || queued.Digest == "" {
		t.Fatalf("public queue status lost the source-bound work identity: %#v", queued)
	}
	if _, err := service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "claim", Actor: "drafter", ItemID: queued.ID, ExpectedDigest: queued.Digest,
		Timestamp: "2099-01-01T00:00:00Z",
	}); err == nil {
		t.Fatal("non-manager claimed source-bound normalization work")
	}
	claimed, err := service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "claim", Actor: "manager", ItemID: queued.ID, ExpectedDigest: queued.Digest,
		Timestamp: "2099-01-01T00:00:00Z",
	})
	if err != nil || claimed.Item == nil || claimed.Item.Status != "claimed" {
		t.Fatalf("campaign manager could not claim normalization work: result=%#v err=%v", claimed, err)
	}
	acknowledged, err := service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "ack", Actor: "manager", ItemID: queued.ID, ExpectedDigest: claimed.Item.Digest,
		Timestamp: "2099-01-01T00:01:00Z",
	})
	if err != nil || acknowledged.Item == nil || acknowledged.Item.Status != "acknowledged" {
		t.Fatalf("campaign manager could not acknowledge normalization work: result=%#v err=%v", acknowledged, err)
	}
	if _, err := service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "resolve", Actor: "manager", ItemID: queued.ID, ExpectedDigest: acknowledged.Item.Digest,
		Timestamp: "2099-01-01T00:02:00Z",
	}); err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("free-text or missing proof resolved normalization work: %v", err)
	}
}

func TestNormalizationManagerRequestIsDigestBoundAndBelowThreshold(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture).run
	request := NormalizationQueueRequest{
		Action: "request", Actor: "manager", ReportPath: source.Report.Path,
		ReportDigest: source.Report.SHA256, Timestamp: "2026-08-02T18:03:00Z",
	}
	unauthorized := request
	unauthorized.Actor = "drafter"
	if _, err := fixture.service.NormalizationQueueApply(context.Background(), unauthorized); err == nil ||
		!strings.Contains(err.Error(), "permitted") {
		t.Fatalf("non-manager explicitly requested normalization: %v", err)
	}
	stale := request
	stale.ReportDigest = stateTestDigest("f")
	if _, err := fixture.service.NormalizationQueueApply(context.Background(), stale); err == nil {
		t.Fatal("manager request accepted a report digest that was not canonical")
	}
	aliased := request
	aliased.ReportPath = strings.Replace(
		source.Report.Path, "/runs/", "/runs/../runs/", 1)
	if _, err := fixture.service.NormalizationQueueApply(context.Background(), aliased); err == nil ||
		!strings.Contains(err.Error(), "exact canonical") {
		t.Fatalf("manager request accepted a path alias instead of the exact source path: %v", err)
	}
	result, err := fixture.service.NormalizationQueueApply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Item == nil || result.Item.ServeCount != 0 ||
		!containsString(result.Item.Triggers, normalizationTriggerManager) ||
		result.Item.ReportPath != source.Report.Path || result.Item.ReportDigest != source.Report.SHA256 {
		t.Fatalf("manager request did not queue exact below-threshold work: %#v", result.Item)
	}
	replayed, err := fixture.service.NormalizationQueueApply(context.Background(), request)
	if err != nil || replayed.Item == nil || replayed.Item.Digest != result.Item.Digest {
		t.Fatalf("exact manager request was not idempotent: result=%#v err=%v", replayed, err)
	}
}

func TestClosureQueuesEveryStillUnnormalizedSourceReport(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	returned := returnNormalizationSourceRun(t, fixture)
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ClosureApply(context.Background(), ClosureApplyRequest{
		Action: "start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "normalization-closure-start", IdempotencyKey: "normalization-closure-start",
		Rationale: "Queue every uncovered source report before closure.",
		Timestamp: "2099-01-01T18:03:00Z", ClosureJobID: "normalization-closure",
		ArchiveDestination:   "docs/history/campaigns/2026-08-02-test-campaign",
		ExpectedHeadRevision: returned.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   returned.receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{
			graph.Campaign.ID: graph.Campaign.Digest,
		},
		ExpectedArtifactDigests: map[string]string{}, FileRetention: map[string]string{},
		ProjectionDestinations: map[string]string{},
	})
	if err != nil || result.Job == nil {
		t.Fatalf("closure start failed before normalization trigger: result=%#v err=%v", result, err)
	}
	status := fixture.service.archiveTracker.QueueStatus(20)
	if status.Queued != 1 || len(status.Items) != 1 ||
		status.Items[0].RunID != returned.run.ID ||
		!containsString(status.Items[0].Triggers, normalizationTriggerClosure) {
		t.Fatalf("closure did not queue every uncovered source report: %#v", status)
	}
	before := status.Items[0].Digest
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.queueClosureNormalization(graph, "2099-01-01T18:04:00Z"); err != nil {
		t.Fatal(err)
	}
	after := fixture.service.archiveTracker.QueueStatus(20).Items[0]
	if after.Digest != before {
		t.Fatalf("replayed closure trigger rewrote idempotent work: before=%s after=%s", before, after.Digest)
	}
	blockers, err := fixture.service.closureNormalizationBlockers(graph)
	if err != nil {
		t.Fatal(err)
	}
	wantBlocker := "normalization:" + returned.run.ID + ":queued"
	if len(blockers) != 1 || blockers[0] != wantBlocker {
		t.Fatalf("closure normalize gate did not retain unresolved queue work: got=%v want=%s", blockers, wantBlocker)
	}
}

func TestClosureNormalizationGateRequiresStructuredQueueResolution(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := fixture.service.queueClosureNormalization(graph, "2099-01-01T18:03:00Z")
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue closure normalization: queued=%#v err=%v", queued, err)
	}
	item, resolution := completeTerminallyJudgedNormalization(t, fixture, source, "non-claim")
	if !containsString(item.Triggers, normalizationTriggerClosure) {
		t.Fatalf("manager request discarded the prior closure trigger: %#v", item)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	blockers, err := fixture.service.closureNormalizationBlockers(graph)
	if err != nil || len(blockers) != 1 || !strings.HasSuffix(blockers[0], ":queued") {
		t.Fatalf("reviewed intake bypassed the structured queue receipt: blockers=%v err=%v", blockers, err)
	}
	claimed, err := fixture.service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "claim", Actor: "manager", ItemID: item.ID, ExpectedDigest: item.Digest,
		Timestamp: "2099-01-01T18:04:00Z",
	})
	if err != nil || claimed.Item == nil {
		t.Fatalf("claim closure normalization: result=%#v err=%v", claimed, err)
	}
	acknowledged, err := fixture.service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "ack", Actor: "manager", ItemID: item.ID,
		ExpectedDigest: claimed.Item.Digest, Timestamp: "2099-01-01T18:05:00Z",
	})
	if err != nil || acknowledged.Item == nil {
		t.Fatalf("ack closure normalization: result=%#v err=%v", acknowledged, err)
	}
	resolved, err := fixture.service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "resolve", Actor: "manager", ItemID: item.ID,
		ExpectedDigest: acknowledged.Item.Digest, Timestamp: "2099-01-01T18:06:00Z",
		Resolution: &resolution,
	})
	if err != nil || resolved.Item == nil || resolved.Item.Status != "resolved" {
		t.Fatalf("resolve closure normalization: result=%#v err=%v", resolved, err)
	}
	blockers, err = fixture.service.closureNormalizationBlockers(graph)
	if err != nil || len(blockers) != 0 {
		t.Fatalf("sealed queue resolution did not clear closure normalize gate: blockers=%v err=%v", blockers, err)
	}
}

func TestNormalizationResolutionRequiresCanonicalCuratorCoverageAndReviewProof(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	queued, resolution := completeTerminallyJudgedNormalization(t, fixture, source, "non-claim")
	claimed, err := fixture.service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "claim", Actor: "manager", ItemID: queued.ID, ExpectedDigest: queued.Digest,
		Timestamp: "2099-01-01T00:00:00Z",
	})
	if err != nil || claimed.Item == nil {
		t.Fatalf("claim canonical normalization proof fixture: result=%#v err=%v", claimed, err)
	}
	acknowledged, err := fixture.service.NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
		Action: "ack", Actor: "manager", ItemID: queued.ID,
		ExpectedDigest: claimed.Item.Digest, Timestamp: "2099-01-01T00:01:00Z",
	})
	if err != nil || acknowledged.Item == nil {
		t.Fatalf("ack canonical normalization proof fixture: result=%#v err=%v", acknowledged, err)
	}
	base := NormalizationQueueRequest{
		Action: "resolve", Actor: "manager", ItemID: queued.ID,
		ExpectedDigest: acknowledged.Item.Digest, Timestamp: "2099-01-01T00:02:00Z",
	}
	if _, err := fixture.service.NormalizationQueueApply(context.Background(), base); err == nil ||
		!strings.Contains(err.Error(), "receipt") {
		t.Fatalf("missing proof receipt resolved normalization: %v", err)
	}
	for name, mutate := range map[string]func(*NormalizationResolution){
		"substituted curator run": func(value *NormalizationResolution) {
			value.CuratorRunDigest = stateTestDigest("f")
		},
		"substituted intake coverage": func(value *NormalizationResolution) {
			value.CoverageDigest = stateTestDigest("e")
		},
		"substituted manager review": func(value *NormalizationResolution) {
			value.ReviewDigest = stateTestDigest("d")
		},
	} {
		t.Run(name, func(t *testing.T) {
			forged := resolution
			mutate(&forged)
			if err := sealNormalizationResolution(&forged); err != nil {
				t.Fatal(err)
			}
			request := base
			request.Resolution = &forged
			if _, err := fixture.service.NormalizationQueueApply(context.Background(), request); err == nil {
				t.Fatal("forged proof receipt resolved normalization")
			}
		})
	}
	base.Resolution = &resolution
	resolved, err := fixture.service.NormalizationQueueApply(context.Background(), base)
	if err != nil || resolved.Item == nil || resolved.Item.Status != "resolved" ||
		resolved.Item.Resolution == nil ||
		resolved.Item.Resolution.Disposition != "reviewed-non-claim" ||
		resolved.Item.Resolution.Digest != resolution.Digest || resolved.Queue.Resolved != 1 {
		t.Fatalf("canonical reviewed non-claim proof did not resolve: result=%#v err=%v", resolved, err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	closureQueued, err := fixture.service.queueClosureNormalization(
		graph, "2099-01-01T00:03:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(closureQueued) != 0 {
		t.Fatalf("closure requeued a fully reviewed source report: %#v", closureQueued)
	}
	retained, present := fixture.service.archiveTracker.Get(queued.ID)
	if !present || containsString(retained.Triggers, normalizationTriggerClosure) ||
		retained.Status != "resolved" {
		t.Fatalf("closure altered resolved normalization work: %#v", retained)
	}
}

// terminalCoverageRationale supplies the span rationale ValidateIntake demands
// for the dispositions that carry one, so a fixture can vary the disposition
// without also having to vary its own bookkeeping.
func terminalCoverageRationale(disposition string) string {
	switch disposition {
	case "out-of-scope":
		return "The span states a claim about a subject this campaign does not own."
	case "unresolved":
		return "The curator could not decide whether this span states a durable claim."
	default:
		return ""
	}
}

// TestNormalizationResolvesOutOfScopeExactlyAsNonClaim closes a disagreement
// between two gates that both claim to mean "terminal".
//
// reviewedReportCoverage has always counted an `out-of-scope` span as covered,
// so a campaign could clear the closure coverage gate with one; run.complete
// names `out-of-scope` as one of the four dispositions that satisfy it; and
// intake.coverage.retire offers it as one of the two judgments an unresolved
// span may be given. verifyNormalizationResolution accepted only
// `candidate-finding`, `duplicate`, and `non-claim`, so the same span that
// cleared closure coverage refused the cross-campaign normalization resolution
// over the same report - blocking the campaign at the normalize stage with a
// refusal that named a disposition the rest of the engine had told the manager
// to use.
//
// The test drives both dispositions through the identical path and requires the
// same outcome, because that identity - not the acceptance of one extra string -
// is the property that must not drift again.
func TestNormalizationResolvesOutOfScopeExactlyAsNonClaim(t *testing.T) {
	for _, disposition := range []string{"non-claim", "out-of-scope"} {
		t.Run(disposition, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRunPreparationFixture(t)
			source := returnNormalizationSourceRun(t, fixture)
			queued, resolution := completeTerminallyJudgedNormalization(
				t, fixture, source, disposition)

			// The receipt the engine independently derives must be the one the
			// caller submitted, including its disposition: a report whose every
			// span is out-of-scope yields no finding, exactly as one whose every
			// span is non-claim does.
			if resolution.Disposition != "reviewed-non-claim" ||
				len(resolution.ResolvedFindingIDs) != 0 {
				t.Fatalf("a fully %s report did not seal as a claimless resolution: %#v",
					disposition, resolution)
			}

			claimed, err := fixture.service.NormalizationQueueApply(ctx, NormalizationQueueRequest{
				Action: "claim", Actor: "manager", ItemID: queued.ID,
				ExpectedDigest: queued.Digest, Timestamp: "2099-01-01T00:00:00Z",
			})
			if err != nil || claimed.Item == nil {
				t.Fatalf("claim normalization: result=%#v err=%v", claimed, err)
			}
			acknowledged, err := fixture.service.NormalizationQueueApply(ctx, NormalizationQueueRequest{
				Action: "ack", Actor: "manager", ItemID: queued.ID,
				ExpectedDigest: claimed.Item.Digest, Timestamp: "2099-01-01T00:01:00Z",
			})
			if err != nil || acknowledged.Item == nil {
				t.Fatalf("ack normalization: result=%#v err=%v", acknowledged, err)
			}
			resolved, err := fixture.service.NormalizationQueueApply(ctx, NormalizationQueueRequest{
				Action: "resolve", Actor: "manager", ItemID: queued.ID,
				ExpectedDigest: acknowledged.Item.Digest, Timestamp: "2099-01-01T00:02:00Z",
				Resolution: &resolution,
			})
			if err != nil || resolved.Item == nil || resolved.Item.Status != "resolved" ||
				resolved.Item.Resolution == nil ||
				resolved.Item.Resolution.Disposition != "reviewed-non-claim" {
				t.Fatalf("a %s span did not resolve its normalization: result=%#v err=%v",
					disposition, resolved, err)
			}

			// The two gates now agree about the same records: coverage counts the
			// report, and the queue no longer blocks the normalize stage.
			graph, err := fixture.store.LoadCampaignGraph("C-TEST")
			if err != nil {
				t.Fatal(err)
			}
			coverage, err := ComputeClosureCoverage(graph, nil)
			if err != nil {
				t.Fatal(err)
			}
			if coverage.SourceRunCoverage[source.run.ID] != "reviewed-intake" {
				t.Fatalf("a %s span stopped counting as closure coverage: %s",
					disposition, coverage.SourceRunCoverage[source.run.ID])
			}
			blockers, err := fixture.service.closureNormalizationBlockers(graph)
			if err != nil || len(blockers) != 0 {
				t.Fatalf("a %s span left the closure normalize gate blocked: blockers=%v err=%v",
					disposition, blockers, err)
			}
		})
	}
}

// TestNormalizationStillRefusesAnUnjudgedSpanAndNamesTheJudgments pins the
// boundary of the change above. Widening the accepted set is only worth what
// stays outside it: `unresolved` is the one disposition that is not a judgment
// at all, and resolving a normalization over one would record that a manager
// decided something nobody decided.
//
// It runs against a legacy reviewed intake because that is now the only kind
// that can carry an unresolved span - which is itself the point: the two rules
// meet here, and the older gate must keep holding for the records the newer one
// arrived too late for.
func TestNormalizationStillRefusesAnUnjudgedSpanAndNamesTheJudgments(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	queued, resolution := completeTerminallyJudgedNormalization(t, fixture, source, "unresolved")

	claimed, err := fixture.service.NormalizationQueueApply(context.Background(),
		NormalizationQueueRequest{
			Action: "claim", Actor: "manager", ItemID: queued.ID,
			ExpectedDigest: queued.Digest, Timestamp: "2099-01-01T00:00:00Z",
		})
	if err != nil || claimed.Item == nil {
		t.Fatalf("claim normalization: result=%#v err=%v", claimed, err)
	}
	acknowledged, err := fixture.service.NormalizationQueueApply(context.Background(),
		NormalizationQueueRequest{
			Action: "ack", Actor: "manager", ItemID: queued.ID,
			ExpectedDigest: claimed.Item.Digest, Timestamp: "2099-01-01T00:01:00Z",
		})
	if err != nil || acknowledged.Item == nil {
		t.Fatalf("ack normalization: result=%#v err=%v", acknowledged, err)
	}
	_, err = fixture.service.NormalizationQueueApply(context.Background(),
		NormalizationQueueRequest{
			Action: "resolve", Actor: "manager", ItemID: queued.ID,
			ExpectedDigest: acknowledged.Item.Digest, Timestamp: "2099-01-01T00:02:00Z",
			Resolution: &resolution,
		})
	if err == nil {
		t.Fatal("a normalization resolved over a span nobody judged")
	}
	for _, want := range []string{
		"non-final disposition unresolved",
		"candidate-finding, duplicate, non-claim, or out-of-scope",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the unjudged-span refusal does not report %q: %v", want, err)
		}
	}
}
