package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClosureApplyPublishesArchiveAndFinalReceipt(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("test-campaign")
	if err != nil {
		t.Fatal(err)
	}
	work := graph.WorkItems["W-0001"]
	work.Revision++
	work.UpdatedAt, work.UpdatedBy, work.CorrelationID, work.Digest =
		"2026-08-02T18:01:00Z", "manager", "corr-cancel", ""
	work.State = "cancelled"
	cancelled, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		Actor: "manager", Authority: "manager", Action: "work.cancel",
		CorrelationID: "corr-cancel", IdempotencyKey: "idem-cancel",
		ExpectedHeadRevision: opening.ResultingHead.Revision,
		ExpectedHeadDigest:   opening.ResultingHead.Digest,
		Writes: []StateWrite{{
			Path:             "active/test-campaign/work-items/W-0001.json",
			ExpectedRevision: graph.WorkItems["W-0001"].Revision,
			ExpectedDigest:   graph.WorkItems["W-0001"].Digest,
			Record:           work,
		}},
	})
	if err != nil {
		t.Fatalf("cancel fixture work: %v", err)
	}
	graph, err = store.LoadCampaignGraph("test-campaign")
	if err != nil {
		t.Fatal(err)
	}

	service := &Service{Boundary: store.Boundary}
	request := ClosureApplyRequest{
		Action: "start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-start", IdempotencyKey: "closure-start",
		Rationale: "close the fully covered fixture", Timestamp: "2026-08-02T18:02:00Z",
		ClosureJobID: "closure-test", ArchiveDestination: "docs/history/campaigns/2026-08-02-test-campaign",
		ExpectedHeadRevision:    cancelled.ResultingHead.Revision,
		ExpectedHeadDigest:      cancelled.ResultingHead.Digest,
		ExpectedRecordDigests:   map[string]string{graph.Campaign.ID: graph.Campaign.Digest},
		ExpectedArtifactDigests: map[string]string{},
		FileRetention:           map[string]string{}, ProjectionDestinations: map[string]string{},
	}
	result, err := service.ClosureApply(context.Background(), request)
	if err != nil {
		t.Fatalf("start closure: %v", err)
	}
	if result.Job == nil || result.Job.Stage != "inventory" || len(result.Job.Blockers) != 0 {
		t.Fatalf("unexpected starting closure state: %+v", result.Job)
	}

	stages := []string{"coverage", "normalize", "reconcile", "decide", "project", "verify", "archive", "finalize"}
	for index, stage := range stages {
		graph, err = store.LoadCampaignGraph("test-campaign")
		if err != nil {
			t.Fatal(err)
		}
		head, err := store.LoadHead()
		if err != nil {
			t.Fatal(err)
		}
		action := "advance"
		if stage == "verify" || stage == "finalize" {
			action = stage
		}
		stamp := time.Date(2026, 8, 2, 18, 3+index, 0, 0, time.UTC).Format(time.RFC3339)
		request = ClosureApplyRequest{
			Action: action, Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
			CorrelationID: "closure-" + stage, IdempotencyKey: "closure-" + stage,
			Rationale: "advance fixture closure", Timestamp: stamp, TargetStage: stage,
			ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
			ExpectedRecordDigests: map[string]string{
				graph.ClosureJob.ID: graph.ClosureJob.Digest,
				"closure-coverage":  graph.ClosureCoverage.Digest,
				graph.Campaign.ID:   graph.Campaign.Digest,
			},
			ExpectedArtifactDigests: map[string]string{},
			FileRetention:           map[string]string{}, ProjectionDestinations: map[string]string{},
		}
		result, err = service.ClosureApply(context.Background(), request)
		if err != nil {
			t.Fatalf("advance closure to %s: %v", stage, err)
		}
		if result.Job == nil || result.Job.Stage != stage {
			t.Fatalf("closure did not reach %s: %+v", stage, result.Job)
		}
	}
	if result.Receipt == nil || result.Receipt.Digest == "" || result.Transaction == nil {
		t.Fatalf("final closure result omitted its receipt: %+v", result)
	}
	graph, err = store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Campaign.Status != "closed" || graph.ClosureJob.Status != "completed" || graph.ClosureReceipt == nil {
		t.Fatalf("canonical campaign did not finalize: %+v", graph)
	}
	for _, relative := range []string{
		"docs/history/campaigns/2026-08-02-test-campaign/manifest.json",
		"docs/history/campaigns/2026-08-02-test-campaign/README.md",
		"docs/history/campaigns/2026-08-02-test-campaign/closure/receipt.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("closure publication %s is missing: %v", relative, err)
		}
	}
}

func TestClosureProjectAtomicallyAdvancesTruthFindingToCurrent(t *testing.T) {
	store, root := newStateTestStore(t)
	document := testFindingDocument()
	document.Record.ID, document.Record.CampaignID = "F-0001", "C-TEST"
	document.Record.Path = "active/test-campaign/findings/F-0001.md"
	document.Record.ReviewState, document.Record.Validity, document.Record.Projection =
		"manager-ratified", "provisional", "truth"
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	document, err = ParseFindingDocument(body, document.Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, document.Record.Path, body)
	graph := NewCampaignGraph()
	graph.Findings[document.Record.ID] = document.Record
	graph.ClosurePlan = &ClosurePlan{ProjectionFindingIDs: []string{document.Record.ID}}
	service := &Service{Boundary: store.Boundary}
	request := ClosureApplyRequest{
		Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-project", Timestamp: "2026-08-02T20:00:00Z",
		ExpectedRecordDigests: map[string]string{document.Record.ID: document.Record.Digest},
	}
	next, writes, err := service.prepareClosureFindingTransitions(store, graph, request)
	if err != nil {
		t.Fatal(err)
	}
	promoted := next.Findings[document.Record.ID]
	if promoted.Validity != "current" || promoted.Revision != document.Record.Revision+1 || len(writes) != 1 {
		t.Fatalf("closure did not prepare one current finding revision: finding=%+v writes=%d", promoted, len(writes))
	}
	if err := ValidateFindingTransition(&document.Record, promoted, "closure.project", "manager"); err != nil {
		t.Fatalf("prepared truth transition violates the canonical state machine: %v", err)
	}
}
