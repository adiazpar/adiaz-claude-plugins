package knowledge

import (
	"strings"
	"testing"
)

func stateTestCampaignRecord() CampaignRecord {
	return CampaignRecord{
		RecordMeta: stateTestMeta("C-TEST", 1), Title: "Test campaign", Slug: "test-campaign",
		Objective: "Exercise the canonical state graph", Scope: []string{"state engine"},
		SuccessCriteria: []string{"Graph validates"}, ClosureCriteria: []string{"All work is decided"},
		Status: "open", CurrentFocus: []string{"W-0001"}, Owner: "manager",
		PermittedManagers: []string{"manager"}, OpenedAt: stateTestTime,
	}
}

func stateTestWorkItem(id string) WorkItemRecord {
	return WorkItemRecord{
		RecordMeta: stateTestMeta(id, 1), CampaignID: "C-TEST", Kind: "task",
		Title: "Work " + id, Problem: "Validate " + id, State: "ready", Priority: "normal",
		Acceptance: []string{"Invariant holds"}, Owner: "manager",
	}
}

func stateTestGraph() CampaignGraph {
	graph := NewCampaignGraph()
	campaign := stateTestCampaignRecord()
	graph.Campaign = &campaign
	graph.WorkItems["W-0001"] = stateTestWorkItem("W-0001")
	return graph
}

func TestWorkGraphReturnedRunBlocksDoneWork(t *testing.T) {
	graph := stateTestGraph()
	run := stateTestReturnedRun(1, "returned")
	graph.Runs[run.ID] = run
	item := graph.WorkItems["W-0001"]
	item.State, item.Outcome = "done", "Prematurely marked done"
	item.ActiveRunIDs = []string{run.ID}
	graph.WorkItems[item.ID] = item
	if err := graph.Validate(); err == nil {
		t.Fatal("work item with an unprocessed returned run was accepted as done")
	}
}

func TestWorkGraphRejectsDependencyCycle(t *testing.T) {
	graph := stateTestGraph()
	first := graph.WorkItems["W-0001"]
	second := stateTestWorkItem("W-0002")
	first.Relations.DependsOn = []string{second.ID}
	second.Relations.DependsOn = []string{first.ID}
	graph.WorkItems[first.ID], graph.WorkItems[second.ID] = first, second
	if err := graph.Validate(); err == nil {
		t.Fatal("cyclic work dependencies were accepted")
	}
}

func stateTestRatifiedFindingGraph() CampaignGraph {
	graph := stateTestGraph()
	run := stateTestReturnedRun(1, "completed")
	graph.Runs[run.ID] = run
	item := graph.WorkItems["W-0001"]
	item.CompletedRunIDs = []string{run.ID}
	graph.WorkItems[item.ID] = item
	finding := FindingRecord{
		SchemaVersion: CampaignSchemaVersion, ID: "F-0001", CampaignID: "C-TEST", Revision: 2,
		CreatedAt: stateTestTime, UpdatedAt: stateTestTime, CreatedBy: "curator", UpdatedBy: "manager",
		Digest: stateTestDigest("3"), CorrelationID: "corr-test", Kind: "conclusion",
		Subject: "state.engine", Claim: "The state graph preserves review provenance.",
		Scope: map[string]any{"component": "state"}, SourceRuns: []string{run.ID},
		Evidence: []EvidenceReference{{
			Path: run.Report.Path, SHA256: run.Report.SHA256, StartLine: 1, EndLine: 2,
			ObjectKey: "path:" + run.Report.Path + "#L1-L2", SourceRun: run.ID,
		}},
		EvidenceGrade: "direct", ReviewState: "manager-ratified", Validity: "provisional", Projection: "campaign",
	}
	graph.Findings[finding.ID] = finding
	return graph
}

func TestWorkGraphRatificationRequiresImmutableReceipt(t *testing.T) {
	graph := stateTestRatifiedFindingGraph()
	if err := graph.Validate(); err == nil {
		t.Fatal("manager-ratified finding without a review receipt was accepted")
	}

	intake := IntakeRecord{
		RecordMeta: stateTestMeta("I-0001", 2), CampaignID: "C-TEST",
		SourceRuns:          []FileHandle{*graph.Runs["R-20260802-0001"].Report},
		CandidateFindingIDs: []string{"F-0001"},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + graph.Runs["R-20260802-0001"].Report.Path + "#L1-L2",
			SourcePath:   graph.Runs["R-20260802-0001"].Report.Path,
			SourceSHA256: graph.Runs["R-20260802-0001"].Report.SHA256,
			StartLine:    1, EndLine: 2, SourceLineCount: 2,
			Disposition: "candidate-finding", TargetID: "F-0001",
		}},
		Triage: map[string]string{"F-0001": "routine"},
		Status: "reviewed",
	}
	graph.Intakes[intake.ID] = intake
	review := ReviewRecord{
		RecordMeta: stateTestMeta("V-0001", 1), CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: intake.ID, IntakeRevision: intake.Revision - 1, PacketDigest: stateTestDigest("9"),
		ReviewLoad: stateTestReviewLoad("V-0001", "C-TEST", stateTestDigest("9"), 1, 0),
		Decisions:  []ReviewDecision{{FindingID: "F-0001", FindingRevision: 1, Action: "ratify", Rationale: "Direct evidence verified"}},
	}
	graph.Reviews[review.ID] = review
	if err := graph.Validate(); err != nil {
		t.Fatalf("graph with immutable ratification receipt rejected: %v", err)
	}
	delete(graph.Reviews, review.ID)
	finding := graph.Findings["F-0001"]
	finding.ReviewState, finding.Validity, finding.Projection = "curator-checked", "provisional", "campaign"
	graph.Findings[finding.ID] = finding
	if err := graph.Validate(); err == nil || !strings.Contains(err.Error(), "reviewed intake") {
		t.Fatalf("reviewed intake without immutable receipt was accepted: %v", err)
	}
}

func TestWorkGraphReopenedClosureRequiresOpenCampaign(t *testing.T) {
	graph := stateTestGraph()
	job := ClosureJob{
		RecordMeta: stateTestMeta("closure-job", 1), CampaignID: "C-TEST",
		Stage: "inventory", Status: "reopened", FrozenCampaignRevision: 1,
	}
	graph.ClosureJob = &job
	if err := graph.Validate(); err != nil {
		t.Fatalf("reopened closure on open campaign rejected: %v", err)
	}
	campaign := *graph.Campaign
	campaign.Status, campaign.ClosingAt = "closing", stateTestTime
	graph.Campaign = &campaign
	if err := graph.Validate(); err == nil {
		t.Fatal("reopened closure job was accepted on a closing campaign")
	}
}

func TestWorkGraphSupersessionRequiresReplacementBackPointer(t *testing.T) {
	graph := stateTestRatifiedFindingGraph()
	old := graph.Findings["F-0001"]
	old.ReviewState, old.Validity = "extracted", "superseded"
	replacement := old
	replacement.ID, replacement.Claim = "F-0002", "The replacement preserves review provenance."
	replacement.Validity = "provisional"
	replacement.Relations.Supersedes = []string{old.ID}
	graph.Findings = map[string]FindingRecord{old.ID: old, replacement.ID: replacement}
	if err := graph.Validate(); err != nil {
		t.Fatalf("paired supersession rejected: %v", err)
	}
	replacement.Relations.Supersedes = nil
	graph.Findings[replacement.ID] = replacement
	if err := graph.Validate(); err == nil {
		t.Fatal("superseded finding without a replacement back-pointer was accepted")
	}
}
