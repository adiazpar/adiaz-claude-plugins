package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClosureApplyPublishesArchiveAndFinalReceipt(t *testing.T) {
	store, root, service, request := prepareClosureArchiveFixture(t)
	result, err := service.ClosureApply(context.Background(), request)
	if err != nil {
		t.Fatalf("finalize closure: %v", err)
	}
	if result.Job == nil || result.Job.Stage != "finalize" || result.Job.Status != "completed" ||
		result.Receipt == nil || result.Receipt.Digest == "" || result.Transaction == nil {
		t.Fatalf("final closure result omitted its durable state: %+v", result)
	}
	if result.Transaction.RetiredTree != "active/test-campaign" {
		t.Fatalf("closure transaction did not bind active-tree retirement: %+v", result.Transaction)
	}
	active := filepath.Join(root, "active", "test-campaign")
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("closed canonical active tree was not retired: %v", err)
	}
	if _, err := store.LoadCampaignGraph("C-TEST"); err == nil {
		t.Fatal("retired campaign remained loadable as active state")
	}
	for _, relative := range []string{
		"docs/history/campaigns/2026-08-02-test-campaign/manifest.json",
		"docs/history/campaigns/2026-08-02-test-campaign/README.md",
		"docs/history/campaigns/2026-08-02-test-campaign/closure/receipt.json",
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/campaign.json",
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/closure-job.json",
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/request.json",
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/events/events.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("closure publication %s is missing: %v", relative, err)
		}
	}
	var archivedCampaign CampaignRecord
	decodeClosureArchiveFixture(t, root,
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/campaign.json", &archivedCampaign)
	if archivedCampaign.Status != "closed" || ValidateCampaign(archivedCampaign) != nil {
		t.Fatalf("archive finalization campaign snapshot is invalid: %+v", archivedCampaign)
	}
	var archivedJob ClosureJob
	decodeClosureArchiveFixture(t, root,
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/closure-job.json", &archivedJob)
	if archivedJob.Status != "completed" || ValidateClosureJob(archivedJob) != nil {
		t.Fatalf("archive finalization job snapshot is invalid: %+v", archivedJob)
	}
	replay, err := service.ClosureApply(context.Background(), request)
	if err != nil || replay.Transaction == nil || replay.Transaction.ResultDigest != result.Transaction.ResultDigest {
		t.Fatalf("idempotent finalization did not replay from its archive overlay: result=%+v err=%v", replay, err)
	}
	changed := request
	changed.Rationale = "different finalization input"
	if _, err := service.ClosureApply(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed retired finalization input did not conflict: %v", err)
	}
}

func TestClosureReplayRejectsTamperedRetiredEventJournal(t *testing.T) {
	_, root, service, request := prepareClosureArchiveFixture(t)
	if _, err := service.ClosureApply(context.Background(), request); err != nil {
		t.Fatalf("finalize closure: %v", err)
	}
	eventPath := filepath.Join(root, filepath.FromSlash(
		"docs/history/campaigns/2026-08-02-test-campaign/finalization/events/events.jsonl"))
	body, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("{\"tampered\":true}\n")...)
	if err := os.WriteFile(eventPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClosureApply(context.Background(), request); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("replay accepted an event journal that no longer matched the immutable receipt: %v", err)
	}
}

func TestClosureFinalizationFailsClosedOnUnknownActiveFile(t *testing.T) {
	_, root, service, request := prepareClosureArchiveFixture(t)
	unknown := filepath.Join(root, "active", "test-campaign", "unregistered-notes.md")
	if err := os.WriteFile(unknown, []byte("must receive a disposition\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClosureApply(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "active-file:unregistered-notes.md") {
		t.Fatalf("closure retired an unregistered active-tree file without a decision: %v", err)
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatalf("failed closure destroyed the unknown file: %v", err)
	}
}

func TestMaintainedProjectionCannotTargetRetiredCampaignTree(t *testing.T) {
	root := t.TempDir()
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	activeDestination := "active/test-campaign/runs/R-20260802-0001/payload/tool.py"
	writeFindingFixtureFile(t, root, activeDestination, []byte("print('active')\n"))
	maintainedDestination := "tools/retained-tool.py"
	writeFindingFixtureFile(t, root, maintainedDestination, []byte("print('retained')\n"))
	service := &Service{Boundary: boundary}
	graph := NewCampaignGraph()
	graph.Findings["F-0001"] = FindingRecord{ID: "F-0001", Projection: "maintained"}
	graph.ClosurePlan = &ClosurePlan{ProjectionFindingIDs: []string{}}
	graph.ClosureJob = &ClosureJob{ArchiveDestination: "docs/history/campaigns/2026-08-02-test-campaign"}
	request := ClosureApplyRequest{
		CampaignSlug:            "test-campaign",
		ProjectionDestinations:  map[string]string{"F-0001": activeDestination},
		ExpectedArtifactDigests: map[string]string{},
	}
	if _, _, _, err := service.prepareClosureProjections(graph, graph, ClosureCoverage{}, request); err == nil ||
		!strings.Contains(err.Error(), "retired by closure") {
		t.Fatalf("maintained projection inside the retiring tree was accepted: %v", err)
	}
	request.ProjectionDestinations["F-0001"] = maintainedDestination
	_, projections, artifacts, err := service.prepareClosureProjections(graph, graph, ClosureCoverage{}, request)
	if err != nil {
		t.Fatal(err)
	}
	if !digestRE.MatchString(projections[maintainedDestination]) || len(artifacts) != 0 {
		t.Fatalf("external maintained output was not preserved by reference: projections=%v artifacts=%v", projections, artifacts)
	}
	if body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(maintainedDestination))); err != nil ||
		string(body) != "print('retained')\n" {
		t.Fatalf("maintained output did not survive projection preparation: %q err=%v", body, err)
	}
}

func TestClosureProjectStagesTruthPrivatelyAndVerifiesProductionRetrieval(t *testing.T) {
	store, root, service, graph, request, original, staging := prepareStagedTruthClosureFixture(t)
	destination := request.ProjectionDestinations["F-0001"]

	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(destination))); !os.IsNotExist(err) {
		t.Fatalf("project stage published a durable truth destination before finalization: %v", err)
	}
	activeFinding := filepath.Join(root, "active", "test-campaign", "findings", "F-0001.md")
	current, err := os.ReadFile(activeFinding)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Fatal("project stage replaced the canonical provisional finding before finalization")
	}
	parsed, err := ParseFindingDocument(current, "active/test-campaign/findings/F-0001.md")
	if err != nil || parsed.Record.Validity != "provisional" {
		t.Fatalf("canonical source finding no longer remains provisional: finding=%+v err=%v", parsed.Record, err)
	}
	if !digestRE.MatchString(staging.Digest) || graph.ClosureJob.StagingDigest != staging.Digest {
		t.Fatalf("closure job did not pin the immutable private staging manifest: %+v", graph.ClosureJob)
	}
	if err := service.verifyClosureProjections(context.Background(), graph, request); err != nil {
		t.Fatalf("staged truth failed production retrieval verification: %v", err)
	}

	// Even an explicitly broad source class cannot admit any Markdown placed
	// below the derived closure cache.
	poison := filepath.Join(root, ".re-discipline", "cache", "closure", "must-not-index.md")
	if err := os.WriteFile(poison, []byte("private closure staging must never be searchable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := DefaultKnowledgeSettings()
	settings.Sources.Additional = []AdditionalSource{{
		Path: ".re-discipline/cache/closure", Pattern: "*.md", Tier: "backlog",
	}}
	inventory, err := DiscoverSources(store.Boundary, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range inventory.Documents {
		if strings.HasPrefix(document.Path, ".re-discipline/cache/closure/") {
			t.Fatalf("source discovery admitted private closure staging: %+v", document)
		}
	}
}

func TestClosureNavigationManagedBlockPreservesProjectProse(t *testing.T) {
	original := []byte("# Project knowledge\n\nOwner-authored introduction.\n\n" +
		"<!-- re-discipline:campaign-archives -->\nold generated entry\n" +
		"<!-- re-discipline:campaign-archives:end -->\n\nOwner-authored footer.\n")
	generated := "## Closed campaign archives\n\n- [new](history/campaigns/new/README.md)\n"
	first, err := replaceClosureManagedBlock(original, closureArchiveNavigationBlock, generated)
	if err != nil {
		t.Fatal(err)
	}
	second, err := replaceClosureManagedBlock(first, closureArchiveNavigationBlock, generated)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("managed navigation rendering is not deterministic")
	}
	for _, preserved := range []string{"Owner-authored introduction.", "Owner-authored footer."} {
		if strings.Count(string(first), preserved) != 1 {
			t.Fatalf("managed navigation changed surrounding prose %q: %s", preserved, first)
		}
	}
	if strings.Contains(string(first), "old generated entry") || !strings.Contains(string(first), generated) {
		t.Fatalf("managed navigation did not replace only its owned block: %s", first)
	}
}

func TestClosureVerifyRejectsTamperedPrivateStageWithoutPublishing(t *testing.T) {
	_, root, service, graph, request, _, staging := prepareStagedTruthClosureFixture(t)
	destination := request.ProjectionDestinations["F-0001"]
	stageRoot := closureStagingRoot(service.Boundary, staging.CampaignID, staging.ClosureJobID)
	objectPath, err := closureStageObjectPath(stageRoot, staging.ProjectionObjects[destination])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("tampered staged truth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.verifyClosureProjections(context.Background(), graph, request); err == nil {
		t.Fatal("closure verification accepted a tampered private projection object")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(destination))); !os.IsNotExist(err) {
		t.Fatalf("failed verification exposed a public truth projection: %v", err)
	}
}

func TestClosureReopenDiscardsOnlyDerivedPrivateStage(t *testing.T) {
	store, root, service, _ := prepareClosureArchiveFixture(t)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	stageRoot := closureStagingRoot(service.Boundary, graph.Campaign.ID, graph.ClosureJob.ID)
	if _, err := os.Stat(stageRoot); err != nil {
		t.Fatalf("archive stage did not retain its resumable private stage: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	request := ClosureApplyRequest{
		Action: "reopen", Actor: "manager", CampaignSlug: graph.Campaign.Slug,
		CampaignID: graph.Campaign.ID, CorrelationID: "closure-reopen",
		IdempotencyKey: "closure-reopen", Rationale: "resume campaign work",
		Timestamp: "2026-08-02T18:11:00Z", ExpectedHeadRevision: head.Revision,
		ExpectedHeadDigest: head.Digest, ExpectedRecordDigests: map[string]string{
			graph.Campaign.ID:   graph.Campaign.Digest,
			graph.ClosureJob.ID: graph.ClosureJob.Digest,
		},
	}
	result, err := service.ClosureApply(context.Background(), request)
	if err != nil {
		t.Fatalf("reopen staged closure: %v", err)
	}
	if result.Job == nil || result.Job.Status != "reopened" || result.Job.StagingDigest != "" ||
		result.Job.ArchiveDigest != "" || len(result.Job.ProjectionDigests) != 0 {
		t.Fatalf("reopened closure retained derived projection state: %+v", result.Job)
	}
	// Reopen ends an attempt; it does not begin one. Only closure.restart may move
	// the counter, and it is what distinguishes a re-plan from a resumption in the
	// archived event journal.
	if closureAttempt(*result.Job) != closureAttempt(*graph.ClosureJob) {
		t.Fatalf("reopen moved the closure attempt counter: %+v", result.Job)
	}
	if _, err := os.Stat(stageRoot); !os.IsNotExist(err) {
		t.Fatalf("reopen did not discard derived private staging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(
		"docs/history/campaigns/2026-08-02-test-campaign/manifest.json"))); !os.IsNotExist(err) {
		t.Fatalf("pre-finalization archive became public during reopen: %v", err)
	}
}

func TestClosureFinalizationTreeRetirementRecoversAtomically(t *testing.T) {
	for _, point := range []string{
		FailAfterJournal,
		FailAfterRecordPublish,
		FailAfterEventPublish,
		FailAfterInventoryPublish,
		FailAfterReceiptPublish,
		FailAfterTreeRetire,
		FailBeforeHeadPublish,
		FailAfterHeadPublish,
	} {
		t.Run(point, func(t *testing.T) {
			store, root, service, request := prepareClosureArchiveFixture(t)
			projectIndex := []byte("# Project documentation\n\nKeep this exact owner prose.\n")
			historyIndex := []byte("# History\n\nKeep this history introduction.\n")
			writeFindingFixtureFile(t, root, "docs/INDEX.md", projectIndex)
			writeFindingFixtureFile(t, root, "docs/history/INDEX.md", historyIndex)
			request.ExpectedArtifactDigests["docs/INDEX.md"] = "sha256:" + SHA256Bytes(projectIndex)
			request.ExpectedArtifactDigests["docs/history/INDEX.md"] = "sha256:" + SHA256Bytes(historyIndex)
			failed := false
			store.Failpoint = func(observed StateFailpoint) error {
				if !failed && observed.Name == point {
					failed = true
					return errors.New("injected closure retirement interruption")
				}
				return nil
			}
			graph, err := store.LoadCampaignGraph(request.CampaignID)
			if err != nil {
				t.Fatal(err)
			}
			stamp, err := time.Parse(time.RFC3339, request.Timestamp)
			if err != nil {
				t.Fatal(err)
			}
			store.Now = func() time.Time { return stamp }
			if _, err := service.advanceClosure(context.Background(), store, graph, request); err == nil {
				t.Fatal("interrupted closure finalization unexpectedly succeeded")
			}
			if !failed {
				t.Fatalf("closure did not reach failpoint %s", point)
			}
			active := filepath.Join(root, "active", "test-campaign")

			store.Failpoint = nil
			if err := store.Recover(context.Background()); err != nil {
				t.Fatalf("recover finalization interrupted at %s: %v", point, err)
			}
			finalization := filepath.Join(root, filepath.FromSlash(
				"docs/history/campaigns/2026-08-02-test-campaign/finalization/campaign.json"))
			if point != FailAfterHeadPublish {
				graph, err := store.LoadCampaignGraph("C-TEST")
				if err != nil {
					t.Fatalf("old-head recovery did not restore the active graph: %v", err)
				}
				if graph.ClosureJob == nil || graph.ClosureJob.Stage != "archive" || graph.Campaign.Status != "closing" {
					t.Fatalf("old-head recovery restored the wrong state: %+v", graph)
				}
				if _, err := os.Stat(finalization); !os.IsNotExist(err) {
					t.Fatalf("old-head rollback left a finalization snapshot: %v", err)
				}
				for relative, want := range map[string][]byte{
					"docs/INDEX.md":         projectIndex,
					"docs/history/INDEX.md": historyIndex,
				} {
					body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
					if err != nil || string(body) != string(want) {
						t.Fatalf("old-head rollback did not restore pre-existing %s bytes: %q err=%v", relative, body, err)
					}
				}
				archiveManifest := filepath.Join(root, filepath.FromSlash(
					"docs/history/campaigns/2026-08-02-test-campaign/manifest.json"))
				if _, err := os.Stat(archiveManifest); !os.IsNotExist(err) {
					t.Fatalf("old-head rollback exposed a partial archive: %v", err)
				}
				retry, err := service.ClosureApply(context.Background(), request)
				if err != nil || retry.Job == nil || retry.Job.Status != "completed" {
					t.Fatalf("rolled-back finalization was not resumable: result=%+v err=%v", retry, err)
				}
			} else {
				if _, err := os.Stat(active); !os.IsNotExist(err) {
					t.Fatalf("new-head recovery restored a retired tree: %v", err)
				}
				if _, err := os.Stat(finalization); err != nil {
					t.Fatalf("new-head recovery lost the finalization snapshot: %v", err)
				}
				receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
				if err != nil || !found || receipt.RetiredTree != "active/test-campaign" {
					t.Fatalf("new-head recovery lost its retirement receipt: found=%v receipt=%+v err=%v", found, receipt, err)
				}
				replay, err := service.ClosureApply(context.Background(), request)
				if err != nil || replay.Transaction == nil || replay.Transaction.ResultDigest != receipt.ResultDigest {
					t.Fatalf("recovered finalization was not resumably replayable: result=%+v err=%v", replay, err)
				}
			}
		})
	}
}

func prepareClosureArchiveFixture(t *testing.T) (*StateStore, string, *Service, ClosureApplyRequest) {
	t.Helper()
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

	stages := []string{"coverage", "normalize", "reconcile", "decide", "project", "verify", "archive"}
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
		if stage == "verify" {
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
	graph, err = store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	request = ClosureApplyRequest{
		Action: "finalize", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-finalize", IdempotencyKey: "closure-finalize",
		Rationale: "advance fixture closure", Timestamp: "2026-08-02T18:10:00Z", TargetStage: "finalize",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{
			graph.ClosureJob.ID: graph.ClosureJob.Digest,
			"closure-coverage":  graph.ClosureCoverage.Digest,
			graph.Campaign.ID:   graph.Campaign.Digest,
		},
		ExpectedArtifactDigests: map[string]string{},
		FileRetention:           map[string]string{}, ProjectionDestinations: map[string]string{},
	}
	return store, root, service, request
}

func decodeClosureArchiveFixture(t *testing.T, root, relative string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeStrictJSON(body, target); err != nil {
		t.Fatal(err)
	}
}

func prepareStagedTruthClosureFixture(
	t *testing.T,
) (*StateStore, string, *Service, CampaignGraph, ClosureApplyRequest, []byte, closureStagingManifest) {
	t.Helper()
	store, root := newStateTestStore(t)
	service := &Service{Boundary: store.Boundary}
	reportPath := "active/test-campaign/runs/R-20260802-0001/report.md"
	reportBody := []byte("private staging retrieval proof\ncomplete direct evidence\n")
	writeFindingFixtureFile(t, root, reportPath, reportBody)

	document := testFindingDocument()
	document.Record.ID, document.Record.CampaignID = "F-0001", "C-TEST"
	document.Record.Path = "active/test-campaign/findings/F-0001.md"
	document.Record.SourceRuns = []string{"R-20260802-0001"}
	document.Record.Evidence = []EvidenceReference{{
		Path: reportPath, SHA256: "sha256:" + SHA256Bytes(reportBody),
		StartLine: 1, EndLine: 2, ObjectKey: "path:" + reportPath + "#L1-L2",
		SourceRun: "R-20260802-0001",
	}}
	document.Record.Relations = FindingRelations{}
	document.Record.ReviewState, document.Record.Validity, document.Record.Projection =
		"manager-ratified", "provisional", "truth"
	document.Record.Subject = "closure private staging"
	document.Record.Claim = "Closure verifies durable truth through the production retriever before publication."
	original, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	document, err = ParseFindingDocument(original, document.Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, document.Record.Path, original)

	campaign := CampaignRecord{RecordMeta: closureTestMeta("C-TEST"), Slug: "test-campaign"}
	job := ClosureJob{
		RecordMeta: closureTestMeta("closure-test"), CampaignID: "C-TEST",
		Stage: "decide", Status: "running", FrozenCampaignRevision: 1,
		ProjectionFindingIDs: []string{"F-0001"},
		ArchiveDestination:   "docs/history/campaigns/2026-08-02-test-campaign",
		TruthDigests:         map[string]string{}, ProjectionDigests: map[string]string{},
	}
	graph := NewCampaignGraph()
	graph.Campaign = &campaign
	graph.Findings[document.Record.ID] = document.Record
	graph.ClosurePlan = &ClosurePlan{ProjectionFindingIDs: []string{document.Record.ID}}
	graph.ClosureJob = &job
	request := ClosureApplyRequest{
		Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-project", Timestamp: "2026-08-02T20:01:00Z",
		ExpectedRecordDigests:   map[string]string{document.Record.ID: document.Record.Digest},
		ExpectedArtifactDigests: map[string]string{},
		ProjectionDestinations:  map[string]string{"F-0001": "docs/truth/closure-private-staging.md"},
	}
	projected, promotions, err := service.prepareClosureFindingTransitions(store, graph, request)
	if err != nil {
		t.Fatal(err)
	}
	truthDigests, projectionDigests, artifacts, err := service.prepareClosureProjections(
		projected, graph, ClosureCoverage{}, request)
	if err != nil {
		t.Fatal(err)
	}
	staging, err := service.stageClosureProjections(
		graph, projected, truthDigests, projectionDigests, artifacts, promotions, request)
	if err != nil {
		t.Fatal(err)
	}
	job.Stage = "project"
	job.TruthDigests = truthDigests
	job.ProjectionDigests = projectionDigests
	job.StagingDigest = staging.Digest
	graph.ClosureJob = &job
	return store, root, service, graph, request, original, staging
}

func TestClosureProjectPreparesTruthFindingPromotionWithoutPublishingIt(t *testing.T) {
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
	graph.ClosureJob = &ClosureJob{ArchiveDestination: "docs/history/campaigns/2026-08-02-test-campaign"}
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

// reopenClosureFixture takes the archive-stage fixture all the way to a reopened
// closure job on an open campaign - the exact state that used to be terminal.
func reopenClosureFixture(
	t *testing.T, store *StateStore, service *Service, timestamp, key string,
) CampaignGraph {
	t.Helper()
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClosureApply(context.Background(), ClosureApplyRequest{
		Action: "reopen", Actor: "manager", CampaignSlug: graph.Campaign.Slug,
		CampaignID: graph.Campaign.ID, CorrelationID: key, IdempotencyKey: key,
		Rationale: "remediate before re-entering closure", Timestamp: timestamp,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{
			graph.Campaign.ID:   graph.Campaign.Digest,
			graph.ClosureJob.ID: graph.ClosureJob.Digest,
		},
	}); err != nil {
		t.Fatalf("reopen closure: %v", err)
	}
	reopened, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Campaign.Status != "open" || reopened.ClosureJob.Status != "reopened" {
		t.Fatalf("fixture did not reach the stranded state: %+v", reopened.ClosureJob)
	}
	return reopened
}

func closureRestartRequest(
	t *testing.T, store *StateStore, graph CampaignGraph, timestamp, key string,
) ClosureApplyRequest {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	return ClosureApplyRequest{
		Action: "restart", Actor: "manager", CampaignSlug: graph.Campaign.Slug,
		CampaignID: graph.Campaign.ID, CorrelationID: key, IdempotencyKey: key,
		Rationale: "re-enter closure after remediation", Timestamp: timestamp,
		ClosureJobID:         graph.ClosureJob.ID,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedClosurePlanRevision: graph.ClosurePlan.CampaignRevision,
		ExpectedRecordDigests: map[string]string{
			graph.Campaign.ID:   graph.Campaign.Digest,
			graph.ClosureJob.ID: graph.ClosureJob.Digest,
			"closure-plan":      graph.ClosurePlan.Digest,
			"closure-coverage":  graph.ClosureCoverage.Digest,
		},
		ExpectedArtifactDigests: map[string]string{},
		FileRetention:           map[string]string{}, ProjectionDestinations: map[string]string{},
	}
}

// The headline. A campaign that took the documented remedy for a closure refusal
// used to have no supported way back in: the reopened job stays on disk, start
// refuses while it is there, and no canonical record has a delete path. Prove the
// second door exists, that it re-plans against the campaign as it stands after
// remediation, and that what comes out of it is an ordinary usable attempt.
func TestClosureReopenIsNoLongerAOneWayDoor(t *testing.T) {
	store, _, service, _ := prepareClosureArchiveFixture(t)
	graph := reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")
	priorFreeze := graph.ClosureJob.FrozenCampaignRevision
	priorPlanDigest := graph.ClosurePlan.Digest

	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remediatedAt, err := time.Parse(time.RFC3339, "2026-08-02T18:12:00Z")
	if err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return remediatedAt }
	if _, err := store.Apply(context.Background(),
		stateTestCreateWorkRequest(head, "W-0002", "corr-remediate", "idem-remediate")); err != nil {
		t.Fatalf("remediate the reopened campaign: %v", err)
	}
	graph, err = store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ClosureApply(context.Background(),
		closureRestartRequest(t, store, graph, "2026-08-02T18:13:00Z", "closure-restart"))
	if err != nil {
		t.Fatalf("restart closure: %v", err)
	}
	if result.Job == nil || result.Job.Stage != "inventory" || result.Job.Status != "running" ||
		result.Job.Attempt != 2 {
		t.Fatalf("restart did not re-enter as a second running attempt: %+v", result.Job)
	}
	if result.Job.FrozenCampaignRevision <= priorFreeze {
		t.Fatalf("restart did not move the campaign freeze forward: %d from %d",
			result.Job.FrozenCampaignRevision, priorFreeze)
	}
	if result.Job.StagingDigest != "" || result.Job.ArchiveDigest != "" ||
		len(result.Job.TruthDigests) != 0 || len(result.Job.ProjectionDigests) != 0 {
		t.Fatalf("restart carried derived proof from the abandoned attempt: %+v", result.Job)
	}

	restarted, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Campaign.Status != "closing" || restarted.ClosureReceipt != nil {
		t.Fatalf("restart did not return the campaign to closing: %+v", restarted.Campaign)
	}
	if restarted.ClosurePlan.Digest == priorPlanDigest ||
		restarted.ClosurePlan.CampaignRevision != restarted.ClosureJob.FrozenCampaignRevision {
		t.Fatalf("restart did not replace the closure plan: %+v", restarted.ClosurePlan)
	}
	if !containsString(restarted.ClosurePlan.RequiredWorkItemIDs, "W-0002") {
		t.Fatalf("re-planned closure did not account for remediation work: %+v", restarted.ClosurePlan)
	}

	head, err = store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := service.ClosureApply(context.Background(), ClosureApplyRequest{
		Action: "advance", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-restart-coverage", IdempotencyKey: "closure-restart-coverage",
		Rationale: "advance the restarted attempt", Timestamp: "2026-08-02T18:14:00Z",
		TargetStage:          "coverage",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{
			restarted.ClosureJob.ID: restarted.ClosureJob.Digest,
			"closure-coverage":      restarted.ClosureCoverage.Digest,
			restarted.Campaign.ID:   restarted.Campaign.Digest,
		},
		ExpectedArtifactDigests: map[string]string{},
		FileRetention:           map[string]string{}, ProjectionDestinations: map[string]string{},
	})
	if err != nil {
		t.Fatalf("restarted closure was not advanceable: %v", err)
	}
	if advanced.Job == nil || advanced.Job.Stage != "coverage" || advanced.Job.Attempt != 2 {
		t.Fatalf("advancing a restarted attempt lost its stage or attempt: %+v", advanced.Job)
	}
}

// Every refusal here has to leave the campaign exactly where it was. A partially
// applied restart would be worse than the dead end it replaces: the job would be
// running against a plan that never landed.
func TestClosureRestartRefusesWithoutExactPlanAndJobCompareAndSwap(t *testing.T) {
	tests := []struct {
		name   string
		damage func(request *ClosureApplyRequest)
	}{
		{name: "stale expected plan revision", damage: func(request *ClosureApplyRequest) {
			request.ExpectedClosurePlanRevision--
		}},
		{name: "wrong plan digest", damage: func(request *ClosureApplyRequest) {
			request.ExpectedRecordDigests["closure-plan"] = stateTestDigest("9")
		}},
		{name: "wrong job digest", damage: func(request *ClosureApplyRequest) {
			request.ExpectedRecordDigests[request.ClosureJobID] = stateTestDigest("9")
		}},
		{name: "wrong coverage digest", damage: func(request *ClosureApplyRequest) {
			request.ExpectedRecordDigests["closure-coverage"] = stateTestDigest("9")
		}},
		{name: "wrong closure job id", damage: func(request *ClosureApplyRequest) {
			request.ClosureJobID = "closure-some-other-job"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _, service, _ := prepareClosureArchiveFixture(t)
			graph := reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")
			request := closureRestartRequest(t, store, graph, "2026-08-02T18:13:00Z", "closure-restart")
			test.damage(&request)
			if _, err := service.ClosureApply(context.Background(), request); err == nil {
				t.Fatal("closure restart applied without an exact compare-and-swap")
			}
			after, err := store.LoadCampaignGraph("C-TEST")
			if err != nil {
				t.Fatal(err)
			}
			if after.Campaign.Status != "open" || after.ClosureJob.Status != "reopened" ||
				after.ClosureJob.Revision != graph.ClosureJob.Revision ||
				after.ClosurePlan.Digest != graph.ClosurePlan.Digest {
				t.Fatalf("a refused restart moved canonical state: campaign=%s job=%+v",
					after.Campaign.Status, after.ClosureJob)
			}
		})
	}
}

// Restart ends one attempt and begins another; it may never discard a live one.
// `reopened` is the only status it accepts, and reopen is the only door into it.
func TestClosureRestartRefusesOnALiveOrFinalizedJob(t *testing.T) {
	store, _, service, finalize := prepareClosureArchiveFixture(t)
	live, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClosureApply(context.Background(),
		closureRestartRequest(t, store, live, "2026-08-02T18:11:00Z", "closure-restart")); err == nil {
		t.Fatal("closure restart discarded a live archive-stage attempt")
	}
	unchanged, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ClosureJob.Digest != live.ClosureJob.Digest ||
		unchanged.Campaign.Status != "closing" {
		t.Fatalf("a refused restart disturbed a live closure attempt: %+v", unchanged.ClosureJob)
	}

	if _, err := service.ClosureApply(context.Background(), finalize); err != nil {
		t.Fatalf("finalize closure: %v", err)
	}
	// `closed` is absorbing in campaignTransitions and finalize retires the whole
	// active tree, so a finalized closure is unreachable by construction rather
	// than by a status check restart would have to remember to perform.
	if _, err := service.ClosureApply(context.Background(), ClosureApplyRequest{
		Action: "restart", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-restart", IdempotencyKey: "closure-restart-final",
		Rationale: "attempt to resurrect a finalized closure", Timestamp: "2026-08-02T18:20:00Z",
		ClosureJobID: "closure-test", ExpectedHeadRevision: 1,
		ExpectedHeadDigest: stateTestDigest("9"),
	}); err == nil {
		t.Fatal("closure restart resurrected a finalized campaign")
	}
}

// Risk 2, and the single most important detail in this change.
// applyClosureActiveFileInventory hard-errors on a disposition naming a missing
// file. Inheriting such a row blindly would make every future restart of that
// campaign refuse, with no supported way to withdraw it - a worse trap than the
// one restart exists to remove. Declared rows still fail closed; only inherited
// ones are pruned.
func TestClosureRestartCarriesManagerDispositionsAndPrunesVanishedActiveFiles(t *testing.T) {
	store, root, service, _ := prepareClosureArchiveFixture(t)
	graph := reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")

	survivor := filepath.Join(root, "active", "test-campaign", "surviving-notes.md")
	vanishing := filepath.Join(root, "active", "test-campaign", "vanishing-notes.md")
	for _, absolute := range []string{survivor, vanishing} {
		if err := os.WriteFile(absolute, []byte("untyped manager material\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	declared := closureRestartRequest(t, store, graph, "2026-08-02T18:13:00Z", "closure-restart")
	declared.ActiveFileDispositions = map[string]string{
		"surviving-notes.md": "retain", "vanishing-notes.md": "ephemeral",
	}
	first, err := service.ClosureApply(context.Background(), declared)
	if err != nil {
		t.Fatalf("restart with declared dispositions: %v", err)
	}
	if first.Job.Coverage.ActiveFileDispositions["surviving-notes.md"] != "retain" ||
		first.Job.Coverage.ActiveFileDispositions["vanishing-notes.md"] != "ephemeral" ||
		len(first.Job.Coverage.MissingDecisions) != 0 {
		t.Fatalf("declared dispositions did not type the unknown files: %+v", first.Job.Coverage)
	}

	graph = reopenClosureFixture(t, store, service, "2026-08-02T18:15:00Z", "closure-reopen-again")
	if err := os.Remove(vanishing); err != nil {
		t.Fatal(err)
	}
	second, err := service.ClosureApply(context.Background(),
		closureRestartRequest(t, store, graph, "2026-08-02T18:17:00Z", "closure-restart-again"))
	if err != nil {
		t.Fatalf("restart refused because an inherited disposition named a deleted file: %v", err)
	}
	if second.Job.Coverage.ActiveFileDispositions["surviving-notes.md"] != "retain" {
		t.Fatalf("restart did not inherit the manager disposition for a file that still exists: %+v",
			second.Job.Coverage.ActiveFileDispositions)
	}
	if _, present := second.Job.Coverage.ActiveFileDispositions["vanishing-notes.md"]; present {
		t.Fatalf("restart inherited a disposition for a file that no longer exists: %+v",
			second.Job.Coverage.ActiveFileDispositions)
	}
	if len(second.Job.Coverage.MissingDecisions) != 0 {
		t.Fatalf("inheritance left the restarted attempt blocked: %+v", second.Job.Coverage.MissingDecisions)
	}
	if second.Job.Attempt != 3 {
		t.Fatalf("the second restart did not record a third attempt: %+v", second.Job)
	}

	// A declared row still fails closed. Inheritance is forgiving because the
	// manager cannot withdraw a carried row; a request is the caller's own
	// assertion in this transaction and must be exact.
	graph = reopenClosureFixture(t, store, service, "2026-08-02T18:19:00Z", "closure-reopen-third")
	bogus := closureRestartRequest(t, store, graph, "2026-08-02T18:21:00Z", "closure-restart-third")
	bogus.ActiveFileDispositions = map[string]string{"vanishing-notes.md": "ephemeral"}
	if _, err := service.ClosureApply(context.Background(), bogus); err == nil ||
		!strings.Contains(err.Error(), "names a missing file") {
		t.Fatalf("a declared disposition for a missing file was accepted: %v", err)
	}
}

// A restarted job keeps its ID and therefore its staging key. Reopen sweeps the
// private stage, but only best-effort after its commit, so a crash in that window
// leaves the previous attempt's content-addressed objects sitting exactly where
// the new attempt will write. Sweep again.
func TestClosureRestartDiscardsStalePrivateStaging(t *testing.T) {
	store, _, service, _ := prepareClosureArchiveFixture(t)
	graph := reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")
	stageRoot := closureStagingRoot(service.Boundary, graph.Campaign.ID, graph.ClosureJob.ID)
	if err := os.MkdirAll(filepath.Join(stageRoot, "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(stageRoot, "objects", "stale"), []byte("abandoned attempt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClosureApply(context.Background(),
		closureRestartRequest(t, store, graph, "2026-08-02T18:13:00Z", "closure-restart")); err != nil {
		t.Fatalf("restart closure: %v", err)
	}
	if _, err := os.Stat(stageRoot); !os.IsNotExist(err) {
		t.Fatalf("restart reused a stale private stage: %v", err)
	}
}

// This defect class is "the engine knew and would not say". The refusal a
// stranded caller actually hits is start's, so start is where the remedy has to
// be named.
func TestClosureStartNamesRestartAsTheRemedy(t *testing.T) {
	store, _, service, _ := prepareClosureArchiveFixture(t)
	live, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	start := ClosureApplyRequest{
		Action: "start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "closure-restart-hint", IdempotencyKey: "closure-restart-hint",
		Rationale: "start closure again", Timestamp: "2026-08-02T18:11:00Z",
		ClosureJobID:         "closure-test",
		ArchiveDestination:   "docs/history/campaigns/2026-08-02-test-campaign",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{live.Campaign.ID: live.Campaign.Digest},
	}
	_, err = service.ClosureApply(context.Background(), start)
	if err == nil || strings.Contains(err.Error(), "restart") {
		t.Fatalf("a live closure attempt was described as restartable: %v", err)
	}

	reopenClosureFixture(t, store, service, "2026-08-02T18:12:00Z", "closure-reopen")
	reloaded, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	head, err = store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	start.ExpectedHeadRevision, start.ExpectedHeadDigest = head.Revision, head.Digest
	start.ExpectedRecordDigests = map[string]string{reloaded.Campaign.ID: reloaded.Campaign.Digest}
	_, err = service.ClosureApply(context.Background(), start)
	if err == nil || !strings.Contains(err.Error(), "\"restart\"") {
		t.Fatalf("start refused a reopened campaign without naming the remedy: %v", err)
	}
}

// TestClosureRestartRefusesAGraphCarryingAClosureReceipt tests the one guard in
// restartClosure that no end-to-end path can reach, by handing the function the
// graph the loader will not build.
//
// The guard's own comment claims it is unreachable through ClosureApply, and
// this test proves both halves of that claim rather than asserting it.
// CampaignGraph.Validate must reject a receipt sitting beside a reopened job on
// an open campaign - that is what makes the guard redundant today - and
// restartClosure must refuse the same graph anyway, because it takes a
// CampaignGraph as an argument rather than loading one, so the invariant it
// leans on lives in a different file and is not its own to keep. The differential
// matters as much as the refusal: the identical graph without the receipt
// restarts, so the receipt is demonstrably the reason and not one of the other
// two preconditions in the same neighbourhood.
func TestClosureRestartRefusesAGraphCarryingAClosureReceipt(t *testing.T) {
	store, _, service, _ := prepareClosureArchiveFixture(t)
	graph := reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")
	request := closureRestartRequest(t, store, graph, "2026-08-02T18:13:00Z", "closure-restart")

	receipt := ClosureReceipt{
		SchemaVersion: CampaignSchemaVersion, CampaignID: graph.Campaign.ID,
		ClosureJobID: graph.ClosureJob.ID, CampaignRevision: graph.Campaign.Revision,
		StateHeadRevision: 9, EventID: "E-20260802-210001-RESTART",
		ArchiveDestination: graph.ClosureJob.ArchiveDestination,
		ArchiveDigest:      stateTestDigest("7"), TruthDigests: map[string]string{},
		CoverageDigest: graph.ClosureCoverage.Digest, ClosedAt: "2026-08-02T18:12:00Z",
	}
	if err := sealClosureReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	finalized := cloneCampaignGraph(graph)
	finalized.ClosureReceipt = &receipt

	// Half one: the loader can never produce this graph, which is why no
	// end-to-end path reaches the guard. If this assertion ever fails, the guard
	// has stopped being defence in depth and become load-bearing.
	if err := finalized.Validate(); err == nil {
		t.Fatal("a closure receipt validated beside a reopened job on an open campaign")
	}

	// Half two: restart refuses it regardless, and says which precondition failed.
	// The call is direct because no public entry point can deliver this graph -
	// ClosureApply loads its own, and the loader has just been shown to refuse it.
	_, err := service.restartClosure(context.Background(), store, finalized, request)
	if err == nil {
		t.Fatal("closure restart re-planned a campaign whose closure was already finalized")
	}
	for _, want := range []string{
		graph.Campaign.ID, "already carries closure receipt",
		graph.ClosureJob.ID, graph.ClosureJob.ArchiveDestination,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the finalized-closure refusal does not report %q: %v", want, err)
		}
	}

	// The differential, through the ordinary entry point: nothing about the
	// campaign changed between the two calls, so the receipt is the whole of the
	// difference between a refusal and a restart. Without this half the refusal
	// above would be equally consistent with restart being broken.
	result, err := service.ClosureApply(context.Background(), request)
	if err != nil {
		t.Fatalf("the same campaign without a receipt did not restart: %v", err)
	}
	if result.Job == nil || result.Job.Stage != "inventory" || result.Job.Status != "running" {
		t.Fatalf("restart did not re-enter as a running attempt: %+v", result.Job)
	}
}
