package knowledge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestCurationSubmitRejectsUnknownDuplicateCoverageTarget(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	receipt, err := fixture.service.ManagerApply(ctx, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	intake := IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "I-0900", Revision: 1,
			CreatedAt: "2026-08-02T18:03:00Z", UpdatedAt: "2026-08-02T18:03:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			Digest: stateTestDigest("9"), CorrelationID: "corr-duplicate-coverage",
		},
		CampaignID: "C-TEST",
		SourceRuns: []FileHandle{{
			Path:   "active/test-campaign/runs/R-20260802-0001/report.md",
			SHA256: stateTestDigest("8"),
		}},
		CandidateFindingIDs: nil,
		Coverage: []CoverageEntry{{
			SourceHandle: "path:active/test-campaign/runs/R-20260802-0001/report.md#L1-L1",
			SourcePath:   "active/test-campaign/runs/R-20260802-0001/report.md",
			SourceSHA256: stateTestDigest("8"), StartLine: 1, EndLine: 1, SourceLineCount: 1,
			Disposition: "duplicate", TargetID: "F-9999",
		}},
		Triage: map[string]string{}, Status: "submitted",
	}
	_, err = fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-duplicate-coverage", IdempotencyKey: "idem-duplicate-coverage",
		ExpectedHeadRevision: receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   receipt.ResultingHead.Digest, Intake: intake,
	})
	if err == nil || !strings.Contains(err.Error(), "is neither a canonical finding of campaign") {
		t.Fatalf("unknown duplicate coverage target was accepted: %v", err)
	}
}

func lifecycleAdvanceMeta(meta RecordMeta, at, actor, correlation string) RecordMeta {
	meta.Revision++
	meta.UpdatedAt = at
	meta.UpdatedBy = actor
	meta.CorrelationID = correlation
	meta.Digest = ""
	return meta
}

func TestReturnedRunCurationRatificationIsRetrievableAcrossCampaigns(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	preparedReceipt, err := fixture.service.ManagerApply(ctx, fixture.request)
	if err != nil {
		t.Fatalf("prepare returned-run fixture: %v", err)
	}
	runID := fixture.request.Runs[0].ID

	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	prepared := graph.Runs[runID]
	running := prepared
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	running.Status = "running"
	running.StartedAt = "2026-08-02T18:01:00Z"
	work := graph.WorkItems["W-0001"]
	priorWorkDigest := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	startedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: prepared.CorrelationID, IdempotencyKey: "idem-e2e-run-start",
		ExpectedHeadRevision:  preparedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    preparedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{runID: prepared.Digest, "W-0001": priorWorkDigest},
		Runs:                  []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("start returned-run fixture: %v", err)
	}

	reportPath := "active/test-campaign/runs/" + runID + "/report.md"
	report := []byte("# VERIFIED OBSERVATION\n\nThe frame registry uses locator cobalt-seventeen in the measured build.\n\n# LIMITS\n\nThis result applies only to the measured build.\n")
	writeFindingFixtureFile(t, fixture.root, reportPath, report)
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	running = graph.Runs[runID]
	returned := running
	returned.RecordMeta = lifecycleAdvanceMeta(
		returned.RecordMeta, "2026-08-02T18:02:00Z", "manager", running.CorrelationID)
	returned.Status = "returned"
	returned.ReturnedAt = "2026-08-02T18:02:00Z"
	returned.Report = &FileHandle{Path: reportPath, SHA256: "sha256:" + SHA256Bytes(report)}
	returned.ResultSummary = "Verified the frame registry locator."
	work = graph.WorkItems["W-0001"]
	priorWorkDigest = work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:02:00Z", "manager", running.CorrelationID)
	returnedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: running.CorrelationID, IdempotencyKey: "idem-e2e-run-return",
		ExpectedHeadRevision:  startedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    startedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{runID: running.Digest, "W-0001": priorWorkDigest},
		Runs:                  []RunRecord{returned}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("return run and queue curation: %v", err)
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.WorkItems[continuousCurationWorkID(runID)].Assignee != "knowledge-curator" {
		t.Fatal("returned run did not create its engine-owned curation queue")
	}

	correlation := "corr-e2e-curation"
	ids := []string{"F-0101", "F-0102"}
	candidates := make([]FindingDocument, 0, len(ids))
	rows := make([]CurationRow, 0, len(ids))
	coverage := make([]CoverageEntry, 0, len(ids))
	triage := map[string]string{}
	for index, id := range ids {
		document := testFindingDocument()
		document.Record.ID = id
		document.Record.CampaignID = "C-TEST"
		document.Record.CorrelationID = correlation
		document.Record.CreatedAt = "2026-08-02T18:03:00Z"
		document.Record.UpdatedAt = "2026-08-02T18:03:00Z"
		document.Record.CreatedBy = "knowledge-curator"
		document.Record.UpdatedBy = "knowledge-curator"
		document.Record.Path = "active/test-campaign/findings/" + id + ".md"
		document.Record.Subject = fmt.Sprintf("frame.registry.fact-%d", index+1)
		document.Record.Claim = []string{
			"The frame registry uses locator cobalt-seventeen in the measured build.",
			"The locator observation is scoped to the measured build identifier.",
		}[index]
		document.Record.SourceRuns = []string{runID}
		document.Record.Relations = FindingRelations{}
		startLine, endLine := 1, 3
		if index == 1 {
			startLine, endLine = 4, 7
		}
		handle := fmt.Sprintf("path:%s#L%d-L%d", reportPath, startLine, endLine)
		document.Record.Evidence = []EvidenceReference{{
			Path: reportPath, SHA256: "sha256:" + SHA256Bytes(report),
			StartLine: startLine, EndLine: endLine, ObjectKey: handle, SourceRun: runID,
		}}
		document.Record.Body = strings.Replace(document.Record.Body,
			"Resource registration uses the named table.", document.Record.Claim, 1)
		document.SyntheticQuestions = []string{
			fmt.Sprintf("What does frame registry fact %d establish?", index+1),
			fmt.Sprintf("Which measured locator fact is numbered %d?", index+1),
			fmt.Sprintf("How can frame registry fact %d be reproduced?", index+1),
		}
		body, renderErr := RenderFindingDocument(document)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		document, renderErr = ParseFindingDocument(body, document.Record.Path)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		candidates = append(candidates, document)
		rows = append(rows, CurationRow{FindingID: id, Triage: "routine"})
		coverage = append(coverage, CoverageEntry{
			SourceHandle: handle, SourcePath: reportPath, SourceSHA256: "sha256:" + SHA256Bytes(report),
			StartLine: startLine, EndLine: endLine, SourceLineCount: 7,
			Disposition: "candidate-finding", TargetID: id,
		})
		triage[id] = "routine"
	}
	intake := IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "I-0101", Revision: 1,
			CreatedAt: "2026-08-02T18:03:00Z", UpdatedAt: "2026-08-02T18:03:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			Digest: stateTestDigest("1"), CorrelationID: correlation,
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{*returned.Report},
		CandidateFindingIDs: ids, Coverage: coverage, Triage: triage, Status: "submitted",
	}
	submissions := make([]FindingSubmission, 0, len(candidates))
	for _, candidate := range candidates {
		submissions = append(submissions, FindingSubmission{
			Record: candidate.Record, Body: candidate.Record.Body, Path: candidate.Record.Path,
			SyntheticQuestions: candidate.SyntheticQuestions,
			QuestionsReviewed:  candidate.QuestionsReviewed,
		})
	}
	baseCurationRequest := CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID:        correlation,
		ExpectedHeadRevision: returnedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   returnedReceipt.ResultingHead.Digest,
		Intake:               intake, Candidates: submissions, Rows: rows,
	}
	wrongCandidatePath := baseCurationRequest
	wrongCandidatePath.IdempotencyKey = "idem-e2e-curation-wrong-candidate-path"
	wrongCandidatePath.Candidates = append([]FindingSubmission(nil), submissions...)
	wrongCandidatePath.Candidates[0].Path = "active/other-campaign/findings/F-0101.md"
	if _, submitErr := fixture.service.CurationSubmit(ctx, wrongCandidatePath); submitErr == nil ||
		!strings.Contains(submitErr.Error(), "path must be canonical") {
		t.Fatalf("noncanonical candidate path was accepted: %v", submitErr)
	}

	workWrite := baseCurationRequest
	workWrite.IdempotencyKey = "idem-e2e-curation-work-write"
	workWrite.WorkItems = []WorkItemRecord{stateTestWorkItem("W-9999")}
	if _, submitErr := fixture.service.CurationSubmit(ctx, workWrite); submitErr == nil ||
		!strings.Contains(submitErr.Error(), "only manager review may create work records") {
		t.Fatalf("curator-created work record was accepted: %v", submitErr)
	}

	mutatedRun := baseCurationRequest
	mutatedRun.IdempotencyKey = "idem-e2e-curation-mutated-run"
	mutatedRun.CuratorRun = &returned
	if _, submitErr := fixture.service.CurationSubmit(ctx, mutatedRun); submitErr == nil ||
		!strings.Contains(submitErr.Error(), "exact binding") {
		t.Fatalf("non-curator source run was accepted as curatorRun: %v", submitErr)
	}

	staleDigest := stateTestDigest("7")
	stale := baseCurationRequest
	stale.IdempotencyKey = "idem-e2e-curation-stale-source"
	stale.Intake.SourceRuns = append([]FileHandle(nil), intake.SourceRuns...)
	stale.Intake.SourceRuns[0].SHA256 = staleDigest
	stale.Intake.Coverage = append([]CoverageEntry(nil), intake.Coverage...)
	for index := range stale.Intake.Coverage {
		stale.Intake.Coverage[index].SourceSHA256 = staleDigest
	}
	stale.Candidates = append([]FindingSubmission(nil), submissions...)
	for index := range stale.Candidates {
		stale.Candidates[index].Record.Evidence = append(
			[]EvidenceReference(nil), submissions[index].Record.Evidence...)
		stale.Candidates[index].Record.Evidence[0].SHA256 = staleDigest
	}
	if _, submitErr := fixture.service.CurationSubmit(ctx, stale); submitErr == nil ||
		!strings.Contains(submitErr.Error(), "exactly one canonical run report") {
		t.Fatalf("stale report digest was accepted for curation: %v", submitErr)
	}

	wrongLineCount := baseCurationRequest
	wrongLineCount.IdempotencyKey = "idem-e2e-curation-wrong-line-count"
	wrongLineCount.Intake.Coverage = append([]CoverageEntry(nil), intake.Coverage...)
	for index := range wrongLineCount.Intake.Coverage {
		wrongLineCount.Intake.Coverage[index].SourceLineCount = 8
	}
	wrongLineCount.Intake.Coverage = append(wrongLineCount.Intake.Coverage, CoverageEntry{
		SourceHandle: "path:" + reportPath + "#L8-L8", SourcePath: reportPath,
		SourceSHA256: returned.Report.SHA256, StartLine: 8, EndLine: 8, SourceLineCount: 8,
		Disposition: "non-claim",
	})
	if _, submitErr := fixture.service.CurationSubmit(ctx, wrongLineCount); submitErr == nil ||
		!strings.Contains(submitErr.Error(), "canonical report has 7") {
		t.Fatalf("forged report line count was accepted for curation: %v", submitErr)
	}

	baseCurationRequest.IdempotencyKey = "idem-e2e-curation"
	curationReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: baseCurationRequest.Actor, CampaignSlug: baseCurationRequest.CampaignSlug,
		CampaignID: baseCurationRequest.CampaignID, CorrelationID: baseCurationRequest.CorrelationID,
		IdempotencyKey: baseCurationRequest.IdempotencyKey,
		Intake:         baseCurationRequest.Intake,
		Candidates:     baseCurationRequest.Candidates,
		Rows:           baseCurationRequest.Rows,
	})
	if err != nil {
		t.Fatalf("submit curator findings: %v", err)
	}

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	intake = graph.Intakes[intake.ID]
	packet := CurationPacket{Intake: intake, Candidates: candidates, Rows: rows}
	envelope := testReviewPacketEnvelope(t, packet)
	reviewCorrelation := "corr-e2e-review"
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0101", Revision: 1,
			CreatedAt: "2026-08-02T18:04:00Z", UpdatedAt: "2026-08-02T18:04:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", Digest: stateTestDigest("2"),
			CorrelationID: reviewCorrelation,
		},
		CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: intake.ID, IntakeRevision: intake.Revision, PacketDigest: envelope.Digest,
		ReviewLoad: stateTestReviewLoad("V-0101", "C-TEST", envelope.Digest, len(ids), 0),
	}
	review.ReviewLoad.StartedAt = "2026-08-02T18:03:10Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T18:04:00Z"
	review.ReviewLoad.DurationSeconds = 50
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		review.Decisions = append(review.Decisions, ReviewDecision{
			FindingID: candidate.Record.ID, FindingRevision: candidate.Record.Revision,
			Action: "ratify", Projection: "campaign", Rationale: "Exact returned-run evidence verified.",
		})
	}
	reviewedIntake := intake
	reviewedIntake.RecordMeta = lifecycleAdvanceMeta(
		reviewedIntake.RecordMeta, "2026-08-02T18:04:00Z", "manager", reviewCorrelation)
	reviewedIntake.Digest = stateTestDigest("3")
	reviewedIntake.Status = "reviewed"
	outcomes := make([]FindingSubmission, 0, len(candidates))
	for _, candidate := range candidates {
		outcome := candidate
		outcome.Record.Revision++
		outcome.Record.UpdatedAt = "2026-08-02T18:04:00Z"
		outcome.Record.UpdatedBy = "manager"
		outcome.Record.CorrelationID = reviewCorrelation
		outcome.Record.Digest = ""
		outcome.Record.ReviewState = "manager-ratified"
		outcome.Record.Validity = "provisional"
		outcome.Record.Projection = "campaign"
		body, renderErr := RenderFindingDocument(outcome)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		outcome, renderErr = ParseFindingDocument(body, outcome.Record.Path)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		outcomes = append(outcomes, FindingSubmission{
			Record: outcome.Record, Body: outcome.Record.Body, Path: outcome.Record.Path,
			SyntheticQuestions: outcome.SyntheticQuestions,
			QuestionsReviewed:  outcome.QuestionsReviewed,
		})
	}
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{intake.ID: graph.Intakes[intake.ID].Digest}
	for _, candidate := range candidates {
		expected[candidate.Record.ID] = graph.Findings[candidate.Record.ID].Digest
	}
	reviewRequest := ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: reviewCorrelation, IdempotencyKey: "idem-e2e-review",
		ExpectedHeadRevision:  curationReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    curationReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: expected, Intake: &reviewedIntake, Review: &review,
		Findings: outcomes,
		ReviewPacket: &ReviewPacketSubmission{
			Envelope: envelope, Intake: intake, Candidates: submissions,
		},
	}
	forgedIntake := reviewRequest
	forgedIntake.IdempotencyKey = "idem-e2e-review-forged-intake"
	forgedIntakePacket := *reviewRequest.ReviewPacket
	forgedIntakePacket.Intake.Uncertainties = []string{"Caller-added uncertainty after packet publication."}
	forgedResultingIntake := *reviewRequest.Intake
	forgedResultingIntake.Uncertainties = append(
		[]string(nil), forgedIntakePacket.Intake.Uncertainties...)
	forgedIntake.Intake = &forgedResultingIntake
	forgedIntake.ReviewPacket = &forgedIntakePacket
	if _, reviewErr := fixture.service.ManagerApply(ctx, forgedIntake); reviewErr == nil ||
		!strings.Contains(reviewErr.Error(), "exact canonical submitted intake") {
		t.Fatalf("caller-forged intake was accepted at review commit: %v", reviewErr)
	}

	forgedCandidate := reviewRequest
	forgedCandidate.IdempotencyKey = "idem-e2e-review-forged-candidate"
	forgedCandidatePacket := *reviewRequest.ReviewPacket
	forgedCandidatePacket.Candidates = append([]FindingSubmission(nil), reviewRequest.ReviewPacket.Candidates...)
	forgedCandidatePacket.Candidates[0].SyntheticQuestions = append(
		[]string(nil), forgedCandidatePacket.Candidates[0].SyntheticQuestions...)
	forgedCandidatePacket.Candidates[0].SyntheticQuestions[0] = "What caller-substituted question bypasses canonical review?"
	forgedCandidate.Findings = append([]FindingSubmission(nil), reviewRequest.Findings...)
	forgedCandidate.Findings[0].SyntheticQuestions = append(
		[]string(nil), reviewRequest.Findings[0].SyntheticQuestions...)
	forgedCandidate.Findings[0].SyntheticQuestions[0] =
		forgedCandidatePacket.Candidates[0].SyntheticQuestions[0]
	forgedCandidate.ReviewPacket = &forgedCandidatePacket
	if _, reviewErr := fixture.service.ManagerApply(ctx, forgedCandidate); reviewErr == nil ||
		!strings.Contains(reviewErr.Error(), "not its exact canonical revision") {
		t.Fatalf("caller-forged candidate content was accepted at review commit: %v", reviewErr)
	}

	_, err = fixture.service.ManagerApply(ctx, reviewRequest)
	if err != nil {
		t.Fatalf("ratify curator findings: %v", err)
	}

	consumerCampaign := stateTestCampaignRecord()
	consumerCampaign.ID = "C-CONSUMER"
	consumerCampaign.Slug = "consumer-campaign"
	consumerCampaign.Title = "Consumer campaign"
	consumerCampaign.CurrentFocus = []string{"W-9001"}
	consumerCampaign.CorrelationID = "corr-e2e-consumer"
	consumerCampaign.Digest = ""
	consumerWork := stateTestWorkItem("W-9001")
	consumerWork.CampaignID = consumerCampaign.ID
	consumerWork.CorrelationID = consumerCampaign.CorrelationID
	consumerWork.Digest = ""
	_, err = fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "campaign.open", Actor: "manager", CampaignSlug: consumerCampaign.Slug,
		CampaignID: consumerCampaign.ID, CorrelationID: consumerCampaign.CorrelationID,
		IdempotencyKey: "idem-e2e-consumer-open",
		Campaign:       &consumerCampaign, WorkItems: []WorkItemRecord{consumerWork},
	})
	if err != nil {
		t.Fatalf("open consuming campaign: %v", err)
	}

	response, err := fixture.service.Query(ctx, FindingQueryOptions{
		Query: candidates[0].SyntheticQuestions[0], Limit: 5, TokenBudget: 2048,
		ContextLeaseID: "consumer-W-9001",
	})
	if err != nil {
		t.Fatalf("cross-campaign finding query: %v", err)
	}
	found := false
	for _, card := range response.Cards {
		if card.ID == candidates[0].Record.ID && card.SourceClass == "campaign" &&
			card.Metadata["campaignId"] == "C-TEST" {
			found = true
		}
	}
	if !found || response.ContextLease == nil {
		t.Fatalf("ratified returned-run finding was not retrievable from the consuming campaign: %#v", response)
	}
}
