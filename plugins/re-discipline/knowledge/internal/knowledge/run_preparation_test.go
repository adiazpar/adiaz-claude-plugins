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

type runPreparationFixture struct {
	root     string
	service  *Service
	store    *StateStore
	request  ManagerApplyRequest
	brief    []byte
	packBody []byte
}

func newRunPreparationFixture(t *testing.T) runPreparationFixture {
	t.Helper()
	root := makeAdversarialProject(t)
	store, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)
	runID := "R-20260802-0091"
	pack, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: "C-TEST", WorkItemID: "W-0001", RunID: runID,
		},
		Task: "Inspect the canonical campaign state and report exact evidence",
		Role: "drafter", TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	brief := []byte("# Delegated objective\n\nInspect the canonical state and return exact evidence.\n")
	packBody, err := canonicalJSON(pack)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "active/test-campaign/runs/" + runID + "/"
	run := RunRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: runID,
			CreatedAt: stateTestTime, UpdatedAt: stateTestTime, Revision: 1,
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "corr-run-prepare",
		},
		CampaignID: "C-TEST", PrimaryWorkItemID: "W-0001",
		ActorID: "drafter-1", Role: "investigator", Status: "prepared",
		Brief: &FileHandle{
			Path: prefix + "brief.md", SHA256: "sha256:" + SHA256Bytes(brief),
		},
		ContextPack: &FileHandle{
			Path: prefix + "context-pack.json", SHA256: "sha256:" + SHA256Bytes(packBody),
		},
	}
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	work := graph.WorkItems["W-0001"]
	work.Revision++
	work.UpdatedBy, work.CorrelationID, work.Digest = "manager", "corr-run-prepare", ""
	work.State = "active"
	work.ActiveRunIDs = []string{runID}
	request := ManagerApplyRequest{
		Action: "run.prepare", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-run-prepare", IdempotencyKey: "idem-run-prepare",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{"W-0001": graph.WorkItems["W-0001"].Digest},
		WorkItems:             []WorkItemRecord{work}, Runs: []RunRecord{run},
		RunPreparation: &RunPreparation{Brief: string(brief), ContextPack: pack},
	}
	return runPreparationFixture{
		root: root, service: service, store: store, request: request, brief: brief, packBody: packBody,
	}
}

func (fixture runPreparationFixture) transactionRequest(t *testing.T) StateTransactionRequest {
	t.Helper()
	artifacts, err := runPreparationArtifacts(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	writes, reviewHandle, err := buildManagerWrites(fixture.service.Boundary, fixture.request, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	return managerStateTransactionRequest(fixture.request, writes, artifacts, reviewHandle)
}

func TestManagerRunPreparationPublishesLaunchAtomicallyAndIdempotently(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	receipt, err := fixture.service.ManagerApply(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("prepare delegated run: %v", err)
	}
	if len(receipt.Artifacts) != 2 {
		t.Fatalf("run preparation published %d launch artifacts", len(receipt.Artifacts))
	}
	briefPath := filepath.Join(fixture.root, "active", "test-campaign", "runs", fixture.request.Runs[0].ID, "brief.md")
	packPath := filepath.Join(fixture.root, "active", "test-campaign", "runs", fixture.request.Runs[0].ID, "context-pack.json")
	brief, briefErr := os.ReadFile(briefPath)
	packBody, packErr := os.ReadFile(packPath)
	if briefErr != nil || packErr != nil || !reflect.DeepEqual(brief, fixture.brief) ||
		!reflect.DeepEqual(packBody, fixture.packBody) {
		t.Fatalf("launch artifacts differ: briefErr=%v packErr=%v", briefErr, packErr)
	}
	if _, err := VerifyContextPack(
		packPath, fixture.request.RunPreparation.ContextPack.Digest,
		fixture.request.RunPreparation.ContextPack.PackID,
	); err != nil {
		t.Fatalf("published context pack does not verify: %v", err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	runID := fixture.request.Runs[0].ID
	if graph.Runs[runID].Status != "prepared" ||
		!containsString(graph.WorkItems["W-0001"].ActiveRunIDs, runID) {
		t.Fatal("run record and primary work item were not committed with launch artifacts")
	}

	replayed, err := fixture.service.ManagerApply(context.Background(), fixture.request)
	if err != nil || !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("exact run preparation retry did not replay its receipt: %v", err)
	}
	tampered := fixture.request
	tamperedBrief := fixture.request.RunPreparation.Brief + "Changed after commit.\n"
	tamperedPreparation := *fixture.request.RunPreparation
	tamperedPreparation.Brief = tamperedBrief
	tampered.RunPreparation = &tamperedPreparation
	tampered.Runs = append([]RunRecord(nil), fixture.request.Runs...)
	tamperedRun := tampered.Runs[0]
	tamperedRun.Brief = &FileHandle{
		Path: tamperedRun.Brief.Path, SHA256: "sha256:" + SHA256String(tamperedBrief),
	}
	tampered.Runs[0] = tamperedRun
	if _, err := fixture.service.ManagerApply(context.Background(), tampered); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same idempotency key accepted changed launch bytes: %v", err)
	}
}

func TestManagerRunPreparationRejectsTamperAndHalfPreparedLaunches(t *testing.T) {
	t.Run("delegated payload is mandatory", func(t *testing.T) {
		fixture := newRunPreparationFixture(t)
		request := fixture.request
		request.RunPreparation = nil
		if _, err := fixture.service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "requires a canonical brief") {
			t.Fatalf("delegated run without launch payload returned %v", err)
		}
	})

	t.Run("brief is canonical LF", func(t *testing.T) {
		fixture := newRunPreparationFixture(t)
		request := fixture.request
		preparation := *request.RunPreparation
		preparation.Brief = strings.ReplaceAll(preparation.Brief, "\n", "\r\n")
		request.RunPreparation = &preparation
		if _, err := fixture.service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "canonical UTF-8") {
			t.Fatalf("CRLF brief returned %v", err)
		}
	})

	t.Run("pack content digest is independently verified", func(t *testing.T) {
		fixture := newRunPreparationFixture(t)
		request := fixture.request
		preparation := *request.RunPreparation
		preparation.ContextPack.Task += " tampered"
		request.RunPreparation = &preparation
		if _, err := fixture.service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "digest or pack ID mismatch") {
			t.Fatalf("self-inconsistent pack returned %v", err)
		}
	})

	t.Run("pack must use the current generation", func(t *testing.T) {
		fixture := newRunPreparationFixture(t)
		request := fixture.request
		preparation := *request.RunPreparation
		preparation.ContextPack.Generation.DirtyFingerprint = strings.Repeat(
			"x", len(preparation.ContextPack.Generation.DirtyFingerprint))
		var err error
		preparation.ContextPack, err = finalizeContextPack(preparation.ContextPack)
		if err != nil {
			t.Fatal(err)
		}
		body, err := canonicalJSON(preparation.ContextPack)
		if err != nil {
			t.Fatal(err)
		}
		request.RunPreparation = &preparation
		request.Runs = append([]RunRecord(nil), request.Runs...)
		run := request.Runs[0]
		run.ContextPack = &FileHandle{Path: run.ContextPack.Path, SHA256: "sha256:" + SHA256Bytes(body)}
		request.Runs[0] = run
		if _, err := fixture.service.ManagerApply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "no longer current") {
			t.Fatalf("stale generation returned %v", err)
		}
	})

	t.Run("transaction rejects a half launch", func(t *testing.T) {
		fixture := newRunPreparationFixture(t)
		request := fixture.transactionRequest(t)
		request.Artifacts = request.Artifacts[:1]
		if _, err := fixture.store.Apply(context.Background(), request); err == nil ||
			!strings.Contains(err.Error(), "atomically create") {
			t.Fatalf("half-prepared delegated run returned %v", err)
		}
	})
}

func TestDelegatedRunPreparationRecoversArtifactPublicationCrash(t *testing.T) {
	fixture := newRunPreparationFixture(t)
	request := fixture.transactionRequest(t)
	runID := fixture.request.Runs[0].ID
	briefPath := filepath.Join(fixture.root, "active", "test-campaign", "runs", runID, "brief.md")
	packPath := filepath.Join(fixture.root, "active", "test-campaign", "runs", runID, "context-pack.json")
	fixture.store.Failpoint = func(hit StateFailpoint) error {
		if hit.Name == FailAfterRecordPublish && strings.HasSuffix(hit.Path, "/brief.md") {
			return errors.New("simulated loss after first launch artifact")
		}
		return nil
	}
	if _, err := fixture.store.Apply(context.Background(), request); err == nil {
		t.Fatal("artifact publication failpoint did not interrupt the transaction")
	}

	recovered := reopenStateTestStore(t, fixture.root)
	if err := recovered.Recover(context.Background()); err != nil {
		t.Fatalf("recover delegated launch: %v", err)
	}
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Fatalf("rollback retained half-published brief: %v", err)
	}
	if _, err := os.Stat(packPath); !os.IsNotExist(err) {
		t.Fatalf("rollback retained half-published context pack: %v", err)
	}
	graph, err := recovered.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, present := graph.Runs[runID]; present ||
		containsString(graph.WorkItems["W-0001"].ActiveRunIDs, runID) {
		t.Fatal("rollback retained a run record without launch artifacts")
	}

	receipt, err := recovered.Apply(context.Background(), request)
	if err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if len(receipt.Artifacts) != 2 {
		t.Fatalf("recovered retry published %d artifacts", len(receipt.Artifacts))
	}
	if _, err := os.Stat(briefPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packPath); err != nil {
		t.Fatal(err)
	}
}
