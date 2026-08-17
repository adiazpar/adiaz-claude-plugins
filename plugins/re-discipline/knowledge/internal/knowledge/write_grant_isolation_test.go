package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grantIsolationPreparation(
	t *testing.T,
	service *Service,
	store *StateStore,
	slug, campaignID, workID, runID, correlation, idempotency string,
	grants []WriteGrant,
) ManagerApplyRequest {
	t.Helper()
	pack, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: campaignID, WorkItemID: workID, RunID: runID,
		},
		Task: "Complete only the isolated project paths assigned to this run",
		Role: "drafter", TokenBudget: 2048, WriteGrants: grants,
	})
	if err != nil {
		t.Fatalf("compile context pack for %s: %v", runID, err)
	}
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	work := graph.WorkItems[workID]
	priorWorkDigest := work.Digest
	work.RecordMeta = grantIsolationAdvanceMeta(work.RecordMeta, correlation)
	work.State = "active"
	work.ActiveRunIDs = SortedUnique(append(work.ActiveRunIDs, runID))
	run := RunRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: runID,
			CreatedAt: stateTestTime, UpdatedAt: stateTestTime, Revision: 1,
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: correlation,
		},
		CampaignID: campaignID, PrimaryWorkItemID: workID,
		ActorID: "drafter-" + runID, Role: "investigator", Status: "prepared",
		WriteGrants: grants,
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	return ManagerApplyRequest{
		Action: "run.prepare", Actor: "manager", CampaignSlug: slug, CampaignID: campaignID,
		CorrelationID: correlation, IdempotencyKey: idempotency,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{workID: priorWorkDigest},
		WorkItems:             []WorkItemRecord{work}, Runs: []RunRecord{run},
		RunPreparation: &RunPreparation{
			Brief:       "# Delegated objective\n\nComplete only the engine-sealed write grants.\n",
			ContextPack: pack,
		},
	}
}

func grantIsolationAdvanceMeta(meta RecordMeta, correlation string) RecordMeta {
	meta.Revision++
	meta.UpdatedAt = stateTestTime
	meta.UpdatedBy = "manager"
	meta.CorrelationID = correlation
	meta.Digest = ""
	return meta
}

func grantIsolationReturnRun(
	t *testing.T,
	service *Service,
	store *StateStore,
	slug, campaignID, workID, runID string,
) {
	t.Helper()
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	prepared := graph.Runs[runID]
	running := prepared
	running.RecordMeta = grantIsolationAdvanceMeta(running.RecordMeta, prepared.CorrelationID)
	running.Status = "running"
	running.StartedAt = stateTestTime
	work := graph.WorkItems[workID]
	priorWorkDigest := work.Digest
	work.RecordMeta = grantIsolationAdvanceMeta(work.RecordMeta, prepared.CorrelationID)
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: slug, CampaignID: campaignID,
		CorrelationID: prepared.CorrelationID, IdempotencyKey: "idem-grant-start-" + runID,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{runID: prepared.Digest, workID: priorWorkDigest},
		Runs:                  []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("start %s: %v", runID, err)
	}

	reportPath := "active/" + slug + "/runs/" + runID + "/report.md"
	report := []byte("# Result\n\nThe assigned write-boundary fixture completed.\n")
	if err := os.WriteFile(filepath.Join(store.Boundary.Root, filepath.FromSlash(reportPath)), report, 0o644); err != nil {
		t.Fatal(err)
	}
	graph, err = store.LoadCampaignGraph(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	running = graph.Runs[runID]
	returned := running
	returned.RecordMeta = grantIsolationAdvanceMeta(returned.RecordMeta, running.CorrelationID)
	returned.Status = "returned"
	returned.ReturnedAt = stateTestTime
	returned.Report = &FileHandle{SHA256: "sha256:" + SHA256Bytes(report)}
	returned.ResultSummary = "Completed the write-boundary fixture."
	work = graph.WorkItems[workID]
	priorWorkDigest = work.Digest
	work.RecordMeta = grantIsolationAdvanceMeta(work.RecordMeta, running.CorrelationID)
	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: slug, CampaignID: campaignID,
		CorrelationID: running.CorrelationID, IdempotencyKey: "idem-grant-return-" + runID,
		ExpectedHeadRevision:  started.ResultingHead.Revision,
		ExpectedHeadDigest:    started.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{runID: running.Digest, workID: priorWorkDigest},
		Runs:                  []RunRecord{returned}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("return %s: %v", runID, err)
	}
}

func TestPreparedRunWriteGrantsAreIsolatedAcrossCampaignsAndReleasedOnReturn(t *testing.T) {
	root := makeAdversarialProject(t)
	store, _ := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)
	topologyOpenCampaign(t, store, "grant-beta", "C-GRANT-BETA", "W-0001", "corr-grant-beta", "idem-grant-beta")

	alpha := grantIsolationPreparation(
		t, service, store,
		"test-campaign", "C-TEST", "W-0001", "R-20260815-0101",
		"corr-grant-alpha-run", "idem-grant-alpha-run",
		[]WriteGrant{{Mode: "directory", Path: "src/shared"}},
	)
	if _, err := service.ManagerApply(context.Background(), alpha); err != nil {
		t.Fatalf("prepare alpha: %v", err)
	}

	betaConflict := grantIsolationPreparation(
		t, service, store,
		"grant-beta", "C-GRANT-BETA", "W-0001", "R-20260815-0102",
		"corr-grant-beta-conflict", "idem-grant-beta-conflict",
		[]WriteGrant{{Mode: "exact", Path: "src/shared/file.go"}},
	)
	_, err := service.ManagerApply(context.Background(), betaConflict)
	if !errors.Is(err, ErrStateConflict) ||
		!strings.Contains(err.Error(), "R-20260815-0101") ||
		!strings.Contains(err.Error(), "R-20260815-0102") ||
		!strings.Contains(err.Error(), "overlaps active run") {
		t.Fatalf("cross-campaign overlap was not rejected precisely: %v", err)
	}

	grantIsolationReturnRun(
		t, service, store, "test-campaign", "C-TEST", "W-0001", "R-20260815-0101",
	)
	betaAfterReturn := grantIsolationPreparation(
		t, service, store,
		"grant-beta", "C-GRANT-BETA", "W-0001", "R-20260815-0102",
		"corr-grant-beta-after-return", "idem-grant-beta-after-return",
		[]WriteGrant{{Mode: "exact", Path: "src/shared/file.go"}},
	)
	if _, err := service.ManagerApply(context.Background(), betaAfterReturn); err != nil {
		t.Fatalf("returned run did not release its grant: %v", err)
	}

	betaSameCampaignConflict := grantIsolationPreparation(
		t, service, store,
		"grant-beta", "C-GRANT-BETA", "W-0001", "R-20260815-0103",
		"corr-grant-beta-same", "idem-grant-beta-same",
		[]WriteGrant{{Mode: "directory", Path: "src/shared"}},
	)
	if _, err := service.ManagerApply(context.Background(), betaSameCampaignConflict); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("same-campaign overlap was accepted: %v", err)
	}

	betaDisjoint := grantIsolationPreparation(
		t, service, store,
		"grant-beta", "C-GRANT-BETA", "W-0001", "R-20260815-0104",
		"corr-grant-beta-disjoint", "idem-grant-beta-disjoint",
		[]WriteGrant{{Mode: "directory", Path: "generated/isolated"}},
	)
	if _, err := service.ManagerApply(context.Background(), betaDisjoint); err != nil {
		t.Fatalf("disjoint grants were rejected: %v", err)
	}
}
