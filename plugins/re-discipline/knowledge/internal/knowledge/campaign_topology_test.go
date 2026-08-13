package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func topologyTestService(t *testing.T) (*Service, *StateStore, string) {
	t.Helper()
	store, root := newStateTestStore(t)
	service := &Service{Boundary: store.Boundary}
	return service, store, root
}

func topologyOpenCampaign(
	t *testing.T,
	store *StateStore,
	slug, campaignID, workID, correlation, idempotency string,
) StateTransactionReceipt {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	campaign := stateTestCampaignRecord()
	campaign.ID, campaign.Slug = campaignID, slug
	campaign.Title = "Campaign " + slug
	campaign.CorrelationID, campaign.Digest = correlation, ""
	work := stateTestWorkItem(workID)
	work.CampaignID = campaignID
	work.Title = "Work in " + slug
	work.CorrelationID, work.Digest = correlation, ""
	receipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: slug, CampaignID: campaignID, Actor: "manager", Authority: "manager",
		Action: "campaign.open", Rationale: "Open topology fixture",
		CorrelationID: correlation, IdempotencyKey: idempotency,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{
			{Path: "active/" + slug + "/campaign.json", Record: campaign},
			{Path: "active/" + slug + "/work-items/" + workID + ".json", Record: work},
		},
	})
	if err != nil {
		t.Fatalf("open topology campaign %s: %v", slug, err)
	}
	return receipt
}

func topologyMergeSpec() CampaignMergeSpec {
	return CampaignMergeSpec{
		TargetCampaignID: "C-MERGED", TargetSlug: "merged-campaign",
		Title: "Merged campaign", Objective: "Consolidate exact campaign history",
		Owner: "manager", PermittedManagers: []string{"manager"},
		MergedAt: "2026-08-12T18:00:00Z",
		Sources: []CampaignMergeSourceSelector{
			{CampaignID: "C-FIRST", CampaignSlug: "first-campaign"},
			{CampaignID: "C-SECOND", CampaignSlug: "second-campaign"},
		},
		Chronology: []CampaignChronologyEntry{
			{ID: "first-history", StartDate: "2026-07-18", EndDate: "2026-07-18",
				Title: "First", Summary: "First campaign history", Status: "historical",
				SourceCampaignIDs: []string{"C-FIRST"}},
			{ID: "second-history", StartDate: "2026-07-19", EndDate: "2026-07-20",
				Title: "Second", Summary: "Second campaign history", Status: "historical",
				SourceCampaignIDs: []string{"C-SECOND"}, DependsOn: []string{"first-history"}},
			{ID: "migration", StartDate: "2026-08-05", EndDate: "2026-08-05",
				Title: "Migration", Summary: "Canonical migration timestamp", Status: "migration",
				SourceCampaignIDs: []string{"C-FIRST", "C-SECOND"}, DependsOn: []string{"second-history"}},
		},
	}
}

func topologyPrepareMerge(t *testing.T, service *Service, store *StateStore) (CampaignMergePlanRequest, CampaignMergePlan) {
	t.Helper()
	topologyOpenCampaign(t, store, "first-campaign", "C-FIRST", "W-0001", "corr-first", "idem-first")
	topologyOpenCampaign(t, store, "second-campaign", "C-SECOND", "W-0001", "corr-second", "idem-second")
	artifact := filepath.Join(store.Boundary.Root, "active", "first-campaign", "subagents", "original.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("historical artifact bytes\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifact, 0o751); err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	request := CampaignMergePlanRequest{Actor: "manager", ExpectedHeadRevision: head.Revision,
		ExpectedHeadDigest: head.Digest, Spec: topologyMergeSpec()}
	plan, err := service.PlanCampaignMerge(context.Background(), request)
	if err != nil {
		t.Fatalf("plan merge: %v", err)
	}
	return request, plan
}

func TestCampaignMergePlansAppliesAndReplaysOneCanonicalGraph(t *testing.T) {
	service, store, root := topologyTestService(t)
	planRequest, plan := topologyPrepareMerge(t, service, store)
	if plan.Counts.Campaigns != 2 || plan.Counts.WorkItems != 2 ||
		plan.Counts.Artifacts != 3 || plan.IDMapDigest == "" ||
		plan.ChronologyDigest == "" || plan.Digest == "" {
		t.Fatalf("merge plan is incomplete: %+v", plan)
	}
	repeat, err := service.PlanCampaignMerge(context.Background(), planRequest)
	if err != nil || !reflect.DeepEqual(plan, repeat) {
		t.Fatalf("merge dry-run is not deterministic: %v", err)
	}
	request := ManagerApplyRequest{
		Action: "campaign.merge", Actor: "manager", CampaignSlug: "merged-campaign",
		CampaignID: "C-MERGED", CorrelationID: "corr-merge", IdempotencyKey: "idem-merge",
		Rationale:            "Consolidate the exact campaign trees",
		ExpectedHeadRevision: planRequest.ExpectedHeadRevision,
		ExpectedHeadDigest:   planRequest.ExpectedHeadDigest,
		CampaignMerge:        &CampaignMergeSubmission{Spec: planRequest.Spec, ApprovedPlanDigest: plan.Digest},
	}
	receipt, err := service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatalf("apply merge: %v", err)
	}
	if receipt.CreatedTree != "active/merged-campaign" || len(receipt.RetiredTrees) != 2 {
		t.Fatalf("merge receipt topology is incomplete: %+v", receipt)
	}
	campaigns, err := store.ListCampaigns()
	if err != nil || len(campaigns) != 1 || campaigns[0].ID != "C-MERGED" {
		t.Fatalf("merge did not leave one canonical campaign: %+v err=%v", campaigns, err)
	}
	graph, err := store.LoadCampaignGraph("C-MERGED")
	if err != nil || len(graph.WorkItems) != 2 {
		t.Fatalf("merged graph is incomplete: items=%d err=%v", len(graph.WorkItems), err)
	}
	if !containsString(graph.WorkItems["W-0002"].Relations.DependsOn, "W-0001") {
		t.Fatalf("source dependency chain was not linked: %+v", graph.WorkItems["W-0002"].Relations)
	}
	for _, slug := range []string{"first-campaign", "second-campaign"} {
		if _, err := os.Stat(filepath.Join(root, "active", slug)); !os.IsNotExist(err) {
			t.Fatalf("source campaign %s still exists: %v", slug, err)
		}
	}
	replayed, err := service.ManagerApply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(receipt, replayed) {
		t.Fatalf("exact merge retry did not replay: %v", err)
	}
	changed := request
	changed.Rationale = "Different merge rationale"
	if _, err := service.ManagerApply(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed merge retry was accepted: %v", err)
	}
	chronology, err := os.ReadFile(filepath.Join(root, "active", "merged-campaign", "merge", "CHRONOLOGY.md"))
	if err != nil || !strings.Contains(string(chronology), "Historical dates") {
		t.Fatalf("durable chronology is missing: %v", err)
	}
	historicalEvents, err := os.ReadFile(filepath.Join(
		root, "active", "merged-campaign", "merge", "historical-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for index, line := range strings.Split(strings.TrimSpace(string(historicalEvents)), "\n") {
		var event StateEvent
		if err := decodeStrictJSON([]byte(line), &event); err != nil {
			t.Fatalf("decode historical event %d: %v", index+1, err)
		}
		if !strings.HasPrefix(event.ID, "E-19700101-000000-M") ||
			!containsString(event.AffectedIDs, "C-MERGED") ||
			containsString(event.AffectedIDs, "C-FIRST") ||
			containsString(event.AffectedIDs, "C-SECOND") {
			t.Fatalf("historical event references were not remapped: %+v", event)
		}
	}
	sourceArtifact := plan.Artifacts[0]
	for _, candidate := range plan.Artifacts {
		if strings.HasSuffix(candidate.SourcePath, "/subagents/original.txt") {
			sourceArtifact = candidate
			break
		}
	}
	preserved, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sourceArtifact.TargetPath)))
	if err != nil || string(preserved) != "historical artifact bytes\n" ||
		"sha256:"+SHA256Bytes(preserved) != sourceArtifact.SHA256 {
		t.Fatalf("historical artifact bytes were not preserved: %v", err)
	}
	preservedInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(sourceArtifact.TargetPath)))
	if err != nil || uint32(preservedInfo.Mode().Perm()) != sourceArtifact.Mode {
		t.Fatalf("historical artifact mode was not preserved: mode=%v mapping=%#o err=%v",
			preservedInfo.Mode().Perm(), sourceArtifact.Mode, err)
	}
}

func TestCampaignMergePreservesReturnedAndAbortedRunStates(t *testing.T) {
	spec := topologyMergeSpec()
	firstCampaign := stateTestCampaignRecord()
	firstCampaign.ID, firstCampaign.Slug = "C-FIRST", "first-campaign"
	firstWork := stateTestWorkItem("W-0001")
	firstWork.CampaignID = firstCampaign.ID
	returned := stateTestReturnedRun(3, "returned")
	returned.CampaignID = firstCampaign.ID
	returned.ID = "R-20260802-0001"
	returned.Digest = ""
	returnedAny, _, err := sealRunRecord(returned)
	if err != nil {
		t.Fatal(err)
	}
	returned = returnedAny.(RunRecord)
	aborted := stateTestReturnedRun(4, "aborted")
	aborted.CampaignID = firstCampaign.ID
	aborted.ID = "R-20260802-0002"
	aborted.Digest = ""
	abortedAny, _, err := sealRunRecord(aborted)
	if err != nil {
		t.Fatal(err)
	}
	aborted = abortedAny.(RunRecord)
	firstWork.ActiveRunIDs = []string{returned.ID}
	firstWork.CompletedRunIDs = []string{aborted.ID}

	secondCampaign := stateTestCampaignRecord()
	secondCampaign.ID, secondCampaign.Slug = "C-SECOND", "second-campaign"
	secondWork := stateTestWorkItem("W-0001")
	secondWork.CampaignID = secondCampaign.ID
	sources := []campaignMergeSource{
		{
			selector: CampaignMergeSourceSelector{CampaignID: firstCampaign.ID, CampaignSlug: firstCampaign.Slug},
			graph: CampaignGraph{Campaign: &firstCampaign,
				WorkItems: map[string]WorkItemRecord{firstWork.ID: firstWork},
				Runs:      map[string]RunRecord{returned.ID: returned, aborted.ID: aborted},
				Findings:  map[string]FindingRecord{}, Intakes: map[string]IntakeRecord{}, Reviews: map[string]ReviewRecord{}},
			treeDigest: stateTestDigest("1"),
		},
		{
			selector: CampaignMergeSourceSelector{CampaignID: secondCampaign.ID, CampaignSlug: secondCampaign.Slug},
			graph: CampaignGraph{Campaign: &secondCampaign,
				WorkItems: map[string]WorkItemRecord{secondWork.ID: secondWork}, Runs: map[string]RunRecord{},
				Findings: map[string]FindingRecord{}, Intakes: map[string]IntakeRecord{}, Reviews: map[string]ReviewRecord{}},
			treeDigest: stateTestDigest("2"),
		},
	}
	head := StateHead{SchemaVersion: CampaignSchemaVersion, Revision: 7, Digest: stateTestDigest("7")}
	prepared, err := buildPreparedCampaignMerge(head, CampaignMergePlanRequest{
		Actor: "manager", ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest, Spec: spec,
	}, sources)
	if err != nil {
		t.Fatalf("build merge with terminal run states: %v", err)
	}
	statuses := map[string]RunRecord{}
	for _, write := range prepared.Writes {
		if run, ok := write.Record.(RunRecord); ok {
			statuses[run.Status] = run
		}
	}
	if prepared.Plan.Counts.ReturnedRuns != 1 || prepared.Plan.Counts.AbortedRuns != 1 ||
		statuses["returned"].Status != "returned" || statuses["returned"].ReviewedAt != "" ||
		statuses["aborted"].Status != "aborted" {
		t.Fatalf("merge changed returned or aborted truth: counts=%+v runs=%+v", prepared.Plan.Counts, statuses)
	}
}

func TestCampaignMergeRewritesFindingIntakeReviewAndRunArtifacts(t *testing.T) {
	spec := topologyMergeSpec()
	graph := stateTestRatifiedFindingGraph()
	campaign := *graph.Campaign
	campaign.ID, campaign.Slug, campaign.Digest = "C-FIRST", "first-campaign", ""
	sealedCampaign, _, err := sealCampaignRecord(campaign)
	if err != nil {
		t.Fatal(err)
	}
	campaign = sealedCampaign.(CampaignRecord)
	graph.Campaign = &campaign

	briefBody := []byte("historical brief\n")
	packBody := append([]byte(`{"historical":true}`), '\n')
	reportBody := []byte("direct evidence\nsecond line\n")
	payloadBody := []byte("preserved payload bytes\n")
	run := graph.Runs["R-20260802-0001"]
	run.CampaignID, run.Digest = campaign.ID, ""
	run.Brief = &FileHandle{Path: "active/first-campaign/runs/" + run.ID + "/brief.md",
		SHA256: "sha256:" + SHA256Bytes(briefBody)}
	run.ContextPack = &FileHandle{Path: "active/first-campaign/runs/" + run.ID + "/context-pack.json",
		SHA256: "sha256:" + SHA256Bytes(packBody)}
	run.Report = &FileHandle{Path: "active/first-campaign/runs/" + run.ID + "/report.md",
		SHA256: "sha256:" + SHA256Bytes(reportBody)}
	run.Files = []RunFile{{
		Path: "payload/output.bin", MediaKind: "binary", SemanticRole: "raw-observation",
		Retention: "retain-inline", SHA256: "sha256:" + SHA256Bytes(payloadBody),
		Supports: []string{"F-0001"},
	}}
	sealedRun, _, err := sealRunRecord(run)
	if err != nil {
		t.Fatal(err)
	}
	run = sealedRun.(RunRecord)
	graph.Runs[run.ID] = run

	work := graph.WorkItems["W-0001"]
	work.CampaignID, work.Digest = campaign.ID, ""
	sealedWork, _, err := sealWorkItemRecord(work)
	if err != nil {
		t.Fatal(err)
	}
	work = sealedWork.(WorkItemRecord)
	graph.WorkItems[work.ID] = work

	finding := graph.Findings["F-0001"]
	finding.CampaignID, finding.Digest = campaign.ID, ""
	finding.Scope = map[string]any{
		"run":  run.ID,
		"path": run.Report.Path,
	}
	finding.Evidence[0].Path = run.Report.Path
	finding.Evidence[0].SHA256 = run.Report.SHA256
	finding.Evidence[0].SourceRun = run.ID
	finding.Evidence[0].ObjectKey = "path:" + run.Report.Path + "#L1-L2"
	finding.Path = "active/first-campaign/findings/" + finding.ID + ".md"
	finding.Body = "# Claim\nThe state graph preserves review provenance.\n\n" +
		"## Applies when\nThe campaign graph is evaluated.\n\n" +
		"## Does not establish\nUnrelated campaign behavior.\n\n" +
		"## Evidence\nSee the exact report range.\n\n" +
		"## Reproduction\nRead the cited report.\n\n" +
		"## Relations\nNo additional finding relations.\n"
	findingBody, err := RenderFindingDocument(FindingDocument{
		Record: finding,
		SyntheticQuestions: []string{
			"How is review provenance retained?",
			"Which report proves review provenance?",
			"Where is the review provenance recorded?",
		},
		QuestionsReviewed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	findingDocument, err := ParseFindingDocument(findingBody, finding.Path)
	if err != nil {
		t.Fatal(err)
	}
	finding = findingDocument.Record
	graph.Findings[finding.ID] = finding
	sourceEvidenceHandle := EvidenceHandle(finding.ID, finding.Evidence[0])

	intake := IntakeRecord{
		RecordMeta: stateTestMeta("I-0001", 2), CampaignID: campaign.ID,
		SourceRuns: []FileHandle{*run.Report}, CandidateFindingIDs: []string{finding.ID},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + run.Report.Path + "#L1-L2",
			SourcePath:   run.Report.Path, SourceSHA256: run.Report.SHA256,
			StartLine: 1, EndLine: 2, SourceLineCount: 2,
			Disposition: "candidate-finding", TargetID: finding.ID,
		}},
		Triage: map[string]string{finding.ID: "routine"}, Status: "reviewed",
		RetentionDecisions: []string{sourceEvidenceHandle},
		RequestedDecisions: []string{"run:" + run.ID + ":coverage"},
	}
	intake.CorrelationID, intake.Digest = "corr-source-intake", ""
	sealedIntake, _, err := sealIntakeRecord(intake)
	if err != nil {
		t.Fatal(err)
	}
	intake = sealedIntake.(IntakeRecord)
	graph.Intakes[intake.ID] = intake

	packetDigest := stateTestDigest("9")
	reviewLoad := stateTestReviewLoad("V-0001", campaign.ID, packetDigest, 1, 0)
	reviewLoad.SessionID = "source-review-session"
	if err := SealReviewLoadReceipt(&reviewLoad); err != nil {
		t.Fatal(err)
	}
	review := ReviewRecord{
		RecordMeta: stateTestMeta("V-0001", 1), CampaignID: campaign.ID,
		Reviewer: "manager", Authority: "manager", IntakeID: intake.ID,
		IntakeRevision: 1, PacketDigest: packetDigest, ReviewLoad: reviewLoad,
		Decisions: []ReviewDecision{{
			FindingID: finding.ID, FindingRevision: 1, Action: "ratify",
			Rationale: "Direct evidence verified",
		}},
		UnresolvedConflicts: []string{"finding:" + finding.ID + ":challenged"},
		ResultingRecordIDs: []string{
			finding.ID, finding.Path,
			"record:active/first-campaign/campaign.json",
			"path:active/first-campaign/events/events.jsonl",
		},
	}
	review.CorrelationID, review.Digest = "corr-source-review", ""
	sealedReview, _, err := sealReviewRecord(review)
	if err != nil {
		t.Fatal(err)
	}
	review = sealedReview.(ReviewRecord)
	graph.Reviews[review.ID] = review
	if err := graph.Validate(); err != nil {
		t.Fatalf("full source graph is invalid: %v", err)
	}

	files := []campaignMergeSourceFile{
		{relative: "runs/" + run.ID + "/brief.md", body: briefBody,
			digest: "sha256:" + SHA256Bytes(briefBody), size: int64(len(briefBody)), mode: 0o640},
		{relative: "runs/" + run.ID + "/context-pack.json", body: packBody,
			digest: "sha256:" + SHA256Bytes(packBody), size: int64(len(packBody)), mode: 0o600},
		{relative: "runs/" + run.ID + "/report.md", body: reportBody,
			digest: "sha256:" + SHA256Bytes(reportBody), size: int64(len(reportBody)), mode: 0o644},
		{relative: "runs/" + run.ID + "/payload/output.bin", body: payloadBody,
			digest: "sha256:" + SHA256Bytes(payloadBody), size: int64(len(payloadBody)), mode: 0o640},
		{relative: "findings/" + finding.ID + ".md", body: findingBody,
			digest: "sha256:" + SHA256Bytes(findingBody), size: int64(len(findingBody)), mode: 0o644},
	}
	second := stateTestGraph()
	secondCampaign := *second.Campaign
	secondCampaign.ID, secondCampaign.Slug, secondCampaign.Digest = "C-SECOND", "second-campaign", ""
	sealedSecondCampaign, _, err := sealCampaignRecord(secondCampaign)
	if err != nil {
		t.Fatal(err)
	}
	secondCampaign = sealedSecondCampaign.(CampaignRecord)
	second.Campaign = &secondCampaign
	secondWork := second.WorkItems["W-0001"]
	secondWork.CampaignID, secondWork.Digest = secondCampaign.ID, ""
	sealedSecondWork, _, err := sealWorkItemRecord(secondWork)
	if err != nil {
		t.Fatal(err)
	}
	second.WorkItems[secondWork.ID] = sealedSecondWork.(WorkItemRecord)
	if err := second.Validate(); err != nil {
		t.Fatal(err)
	}

	sources := []campaignMergeSource{
		{selector: spec.Sources[0], graph: graph, treeDigest: stateTestDigest("a"), files: files},
		{selector: spec.Sources[1], graph: second, treeDigest: stateTestDigest("b")},
	}
	head := StateHead{SchemaVersion: CampaignSchemaVersion, Revision: 12, Digest: stateTestDigest("c")}
	prepared, err := buildPreparedCampaignMerge(head, CampaignMergePlanRequest{
		Actor: "manager", ExpectedHeadRevision: head.Revision,
		ExpectedHeadDigest: head.Digest, Spec: spec,
	}, sources)
	if err != nil {
		t.Fatalf("build full graph merge: %v", err)
	}
	if prepared.Plan.Counts.Findings != 1 || prepared.Plan.Counts.Intakes != 1 ||
		prepared.Plan.Counts.Reviews != 1 || len(prepared.Plan.Artifacts) != 4 {
		t.Fatalf("full graph counts were not preserved: %+v", prepared.Plan.Counts)
	}

	var mergedRun RunRecord
	var mergedFinding FindingDocument
	var mergedIntake IntakeRecord
	var mergedReview ReviewRecord
	for _, write := range prepared.Writes {
		switch record := write.Record.(type) {
		case RunRecord:
			mergedRun = record
		case FindingDocument:
			mergedFinding = record
		case IntakeRecord:
			mergedIntake = record
		case ReviewRecord:
			mergedReview = record
		}
	}
	if mergedRun.CampaignID != spec.TargetCampaignID ||
		!strings.HasPrefix(mergedRun.Report.Path, "active/"+spec.TargetSlug+"/runs/R-19700101-") ||
		mergedFinding.Record.SourceRuns[0] != mergedRun.ID ||
		mergedFinding.Record.Evidence[0].Path != mergedRun.Report.Path ||
		mergedFinding.Record.Scope["run"] != mergedRun.ID ||
		mergedFinding.Record.Scope["path"] != mergedRun.Report.Path ||
		mergedRun.Files[0].Path != "payload/output.bin" ||
		mergedRun.Files[0].SHA256 != "sha256:"+SHA256Bytes(payloadBody) ||
		mergedRun.Files[0].Supports[0] != mergedFinding.Record.ID {
		t.Fatalf("run or finding references were not rewritten: run=%+v finding=%+v",
			mergedRun, mergedFinding.Record)
	}
	if mergedIntake.CampaignID != spec.TargetCampaignID ||
		mergedIntake.SourceRuns[0].Path != mergedRun.Report.Path ||
		mergedIntake.CandidateFindingIDs[0] != mergedFinding.Record.ID ||
		mergedIntake.Coverage[0].TargetID != mergedFinding.Record.ID ||
		mergedIntake.RetentionDecisions[0] !=
			EvidenceHandle(mergedFinding.Record.ID, mergedFinding.Record.Evidence[0]) ||
		mergedIntake.RequestedDecisions[0] != "run:"+mergedRun.ID+":coverage" {
		t.Fatalf("intake references were not rewritten: %+v", mergedIntake)
	}
	if mergedReview.CampaignID != spec.TargetCampaignID ||
		mergedReview.IntakeID != mergedIntake.ID ||
		mergedReview.Decisions[0].FindingID != mergedFinding.Record.ID ||
		mergedReview.ReviewLoad.CampaignID != spec.TargetCampaignID ||
		mergedReview.ReviewLoad.ReviewID != mergedReview.ID ||
		mergedReview.ReviewLoad.SessionID == reviewLoad.SessionID ||
		mergedReview.UnresolvedConflicts[0] !=
			"finding:"+mergedFinding.Record.ID+":challenged" ||
		!containsString(mergedReview.ResultingRecordIDs, mergedFinding.Record.ID) ||
		!containsString(mergedReview.ResultingRecordIDs,
			"active/"+spec.TargetSlug+"/findings/"+mergedFinding.Record.ID+".md") ||
		!containsString(mergedReview.ResultingRecordIDs,
			"record:active/"+spec.TargetSlug+"/campaign.json") ||
		!containsString(mergedReview.ResultingRecordIDs,
			"path:active/"+spec.TargetSlug+"/merge/source-events/first-campaign.jsonl") {
		t.Fatalf("review references were not rewritten: %+v", mergedReview)
	}
	for _, artifact := range prepared.Plan.Artifacts {
		if !strings.Contains(artifact.TargetPath, "/runs/"+mergedRun.ID+"/") {
			t.Fatalf("run artifact was not placed under the remapped run: %+v", artifact)
		}
	}
	foundEvidenceMapping, foundLoadMapping, foundSessionMapping := false, false, false
	for _, mapping := range prepared.IDMap.Mappings {
		if mapping.Kind == "evidence" && mapping.SourceID == sourceEvidenceHandle &&
			mapping.SourceDigest != "" &&
			mapping.TargetID == EvidenceHandle(
				mergedFinding.Record.ID, mergedFinding.Record.Evidence[0]) {
			foundEvidenceMapping = true
		}
		if mapping.Kind == "review-load" && mapping.SourceID == reviewLoad.ReviewID &&
			mapping.SourceDigest == reviewLoad.Digest && mapping.TargetID == mergedReview.ID {
			foundLoadMapping = true
		}
		if mapping.Kind == "review-session" && mapping.SourceID == reviewLoad.SessionID &&
			mapping.SourceCampaignID == campaign.ID &&
			mapping.TargetID == mergedReview.ReviewLoad.SessionID {
			foundSessionMapping = true
		}
	}
	if !foundEvidenceMapping || !foundLoadMapping || !foundSessionMapping {
		t.Fatalf("source-qualified nested provenance mappings are missing: evidence=%v load=%v session=%v",
			foundEvidenceMapping, foundLoadMapping, foundSessionMapping)
	}
}

func TestCampaignMergeRejectsTargetIdentityReuse(t *testing.T) {
	service, store, _ := topologyTestService(t)
	topologyOpenCampaign(t, store, "first-campaign", "C-FIRST", "W-0001",
		"corr-first", "idem-first")
	topologyOpenCampaign(t, store, "second-campaign", "C-SECOND", "W-0001",
		"corr-second", "idem-second")
	topologyOpenCampaign(t, store, "existing-campaign", "C-EXISTING", "W-0001",
		"corr-existing", "idem-existing")
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	spec := topologyMergeSpec()
	spec.TargetCampaignID = "C-EXISTING"
	_, err = service.PlanCampaignMerge(context.Background(), CampaignMergePlanRequest{
		Actor: "manager", ExpectedHeadRevision: head.Revision,
		ExpectedHeadDigest: head.Digest, Spec: spec,
	})
	if !errors.Is(err, ErrStateConflict) || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("merge reused an active campaign identity: %v", err)
	}
}

func TestCampaignMergeCrashAfterFirstTreeRetireRollsBackEveryTree(t *testing.T) {
	service, store, root := topologyTestService(t)
	planRequest, plan := topologyPrepareMerge(t, service, store)
	failing := NewStateStoreWithBoundary(service.Boundary)
	failing.Now = store.Now
	failing.Failpoint = func(point StateFailpoint) error {
		if point.Name == FailAfterTreeRetire && point.Path == "active/first-campaign" {
			return errors.New("simulated crash")
		}
		return nil
	}
	request := StateTransactionRequest{
		CampaignSlug: "merged-campaign", CampaignID: "C-MERGED", Actor: "manager", Authority: "manager",
		Action: "campaign.merge", Rationale: "Crash merge", ReviewHandle: plan.Digest,
		CorrelationID: "corr-crash-merge", IdempotencyKey: "idem-crash-merge",
		ExpectedHeadRevision: planRequest.ExpectedHeadRevision, ExpectedHeadDigest: planRequest.ExpectedHeadDigest,
		CreateActiveTree:  "active/merged-campaign",
		RetireActiveTrees: []string{"active/first-campaign", "active/second-campaign"},
		RetireTreeDigests: map[string]string{
			"active/first-campaign":  plan.Sources[0].TreeDigest,
			"active/second-campaign": plan.Sources[1].TreeDigest,
		},
	}
	prepared, err := service.prepareCampaignMerge(context.Background(), planRequest)
	if err != nil {
		t.Fatal(err)
	}
	request.Writes, request.Artifacts = prepared.Writes, prepared.Artifacts
	if _, err := failing.Apply(context.Background(), request); err == nil {
		t.Fatal("merge failpoint did not interrupt publication")
	}
	if err := store.Recover(context.Background()); err != nil {
		t.Fatalf("recover rolled-back merge: %v", err)
	}
	for _, slug := range []string{"first-campaign", "second-campaign"} {
		if _, err := os.Stat(filepath.Join(root, "active", slug, "campaign.json")); err != nil {
			t.Fatalf("source campaign %s was not restored: %v", slug, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "active", "merged-campaign")); !os.IsNotExist(err) {
		t.Fatalf("rolled-back target tree remains: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil || head.Revision != planRequest.ExpectedHeadRevision || head.Digest != planRequest.ExpectedHeadDigest {
		t.Fatalf("rollback changed the project head: %+v err=%v", head, err)
	}
}

func TestCampaignDiscardIsExactDestructiveAndIdempotent(t *testing.T) {
	service, store, root := topologyTestService(t)
	opening := topologyOpenCampaign(t, store, "discard-campaign", "C-DISCARD", "W-0001", "corr-open-discard", "idem-open-discard")
	graph, err := store.LoadCampaignGraph("C-DISCARD")
	if err != nil {
		t.Fatal(err)
	}
	treeDigest, err := digestDirectoryTree(filepath.Join(root, "active", "discard-campaign"))
	if err != nil {
		t.Fatal(err)
	}
	reason := "Fixture campaign is intentionally disposable"
	request := ManagerApplyRequest{
		Action: "campaign.discard", Actor: "manager", CampaignSlug: "discard-campaign",
		CampaignID: "C-DISCARD", CorrelationID: "corr-discard", IdempotencyKey: "idem-discard",
		Rationale: reason, ExpectedHeadRevision: opening.ResultingHead.Revision,
		ExpectedHeadDigest: opening.ResultingHead.Digest,
		CampaignDiscard: &CampaignDiscardSubmission{
			Confirmation: "DISCARD C-DISCARD FROM discard-campaign", Reason: reason,
			ExpectedCampaignDigest: graph.Campaign.Digest,
		},
	}
	receipt, err := service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatalf("discard campaign: %v", err)
	}
	if len(receipt.RetiredTrees) != 1 || receipt.Event.Action != "campaign.discard" {
		t.Fatalf("discard receipt is incomplete: %+v", receipt)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "discard-campaign")); !os.IsNotExist(err) {
		t.Fatalf("discarded campaign still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "history", "campaigns")); err == nil {
		t.Fatal("discard retained a disguised campaign archive")
	}
	replayed, err := service.ManagerApply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(receipt, replayed) {
		t.Fatalf("exact discard retry did not replay: %v", err)
	}
	missing := request
	missing.IdempotencyKey = "idem-discard-again"
	missing.ExpectedHeadRevision = receipt.ResultingHead.Revision
	missing.ExpectedHeadDigest = receipt.ResultingHead.Digest
	if _, err := service.ManagerApply(context.Background(), missing); err == nil ||
		!strings.Contains(err.Error(), "already intentionally discarded") {
		t.Fatalf("already-discarded campaign behavior is unclear: %v", err)
	}
	malformed := request
	malformed.IdempotencyKey = "idem-discard-malformed"
	malformed.CampaignDiscard = &CampaignDiscardSubmission{
		Confirmation: "yes", Reason: reason,
		ExpectedCampaignDigest: graph.Campaign.Digest, ExpectedTreeDigest: treeDigest,
	}
	if _, err := service.ManagerApply(context.Background(), malformed); err == nil {
		t.Fatal("weak destructive confirmation was accepted")
	}
}

func topologyDiscardRequest(
	head StateHead,
	campaignID, slug, campaignDigest, treeDigest, key string,
) ManagerApplyRequest {
	reason := "Destructive topology edge-case test"
	return ManagerApplyRequest{
		Action: "campaign.discard", Actor: "manager", CampaignSlug: slug,
		CampaignID: campaignID, CorrelationID: "corr-" + key, IdempotencyKey: key,
		Rationale: reason, ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		CampaignDiscard: &CampaignDiscardSubmission{
			Confirmation: "DISCARD " + campaignID + " FROM " + slug, Reason: reason,
			ExpectedCampaignDigest: campaignDigest, ExpectedTreeDigest: treeDigest,
		},
	}
}

func TestCampaignDiscardRefusesClosedMissingMalformedAndConcurrentTargets(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		service, store, _ := topologyTestService(t)
		head, _ := store.LoadHead()
		request := topologyDiscardRequest(
			head, "C-MISSING", "missing-campaign", stateTestDigest("1"), stateTestDigest("2"), "idem-missing")
		if _, err := service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("missing discard target did not fail explicitly: %v", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		service, store, root := topologyTestService(t)
		directory := filepath.Join(root, "active", "malformed-campaign")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "campaign.json"), []byte("not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		head, _ := store.LoadHead()
		request := topologyDiscardRequest(
			head, "C-MALFORMED", "malformed-campaign", stateTestDigest("1"), stateTestDigest("2"), "idem-malformed")
		if _, err := service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "malformed campaign tree") {
			t.Fatalf("malformed discard target did not fail explicitly: %v", err)
		}
	})

	t.Run("closed", func(t *testing.T) {
		service, store, root := topologyTestService(t)
		coverage := ClosureCoverage{
			SchemaVersion: CampaignSchemaVersion, CampaignID: "C-CLOSED",
			SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
			WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
			ActiveFileDispositions: map[string]string{}, UnresolvedConflicts: []string{},
			MissingDecisions: []string{},
		}
		if err := sealClosureCoverage(&coverage); err != nil {
			t.Fatal(err)
		}
		manifest := ArchiveManifest{
			SchemaVersion: CampaignSchemaVersion, CampaignID: "C-CLOSED",
			ClosedAt: stateTestTime, SourceDigest: stateTestDigest("3"),
			Files:       map[string]string{"campaign.json": stateTestDigest("4")},
			Projections: map[string]string{}, Coverage: coverage,
			EventHead: "E-20260802-180000-CLOSED",
		}
		if err := sealArchiveManifest(&manifest); err != nil {
			t.Fatal(err)
		}
		body, err := canonicalJSON(manifest)
		if err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(root, "docs", "history", "campaigns", "closed-fixture", "manifest.json")
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, body, 0o600); err != nil {
			t.Fatal(err)
		}
		head, _ := store.LoadHead()
		request := topologyDiscardRequest(
			head, "C-CLOSED", "closed-campaign", stateTestDigest("1"), stateTestDigest("2"), "idem-closed")
		if _, err := service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "refuses closed campaign") {
			t.Fatalf("closed discard target did not fail explicitly: %v", err)
		}
	})

	t.Run("concurrently-modified", func(t *testing.T) {
		service, store, root := topologyTestService(t)
		opening := topologyOpenCampaign(t, store, "concurrent-campaign", "C-CONCURRENT", "W-0001",
			"corr-concurrent-open", "idem-concurrent-open")
		graph, err := store.LoadCampaignGraph("C-CONCURRENT")
		if err != nil {
			t.Fatal(err)
		}
		treeDigest, err := digestDirectoryTree(filepath.Join(root, "active", "concurrent-campaign"))
		if err != nil {
			t.Fatal(err)
		}
		stale := topologyDiscardRequest(opening.ResultingHead, "C-CONCURRENT", "concurrent-campaign",
			graph.Campaign.Digest, treeDigest, "idem-concurrent-discard")
		work := stateTestWorkItem("W-0002")
		work.CampaignID, work.CorrelationID, work.Digest = "C-CONCURRENT", "corr-concurrent-work", ""
		if _, err := store.Apply(context.Background(), StateTransactionRequest{
			CampaignSlug: "concurrent-campaign", CampaignID: "C-CONCURRENT", Actor: "manager", Authority: "manager",
			Action: "work.create", CorrelationID: "corr-concurrent-work", IdempotencyKey: "idem-concurrent-work",
			ExpectedHeadRevision: opening.ResultingHead.Revision,
			ExpectedHeadDigest:   opening.ResultingHead.Digest,
			Writes:               []StateWrite{{Path: "active/concurrent-campaign/work-items/W-0002.json", Record: work}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ManagerApply(context.Background(), stale); !errors.Is(err, ErrStateConflict) {
			t.Fatalf("concurrently modified discard target returned %v", err)
		}
	})
}
