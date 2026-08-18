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

func TestCommittedInventoryBlocksOutOfBandCanonicalEditUntilExplicitReconciliation(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}

	manual := graph.WorkItems["W-0001"]
	manual.Title = "Emergency out-of-band title"
	manual.Digest = ""
	sealedValue, body, err := sealWorkItemRecord(manual)
	if err != nil {
		t.Fatal(err)
	}
	manual = sealedValue.(WorkItemRecord)
	target := filepath.Join(root, "active", "test-campaign", "work-items", "W-0001.json")
	if err := os.WriteFile(target, body, 0o644); err != nil {
		t.Fatal(err)
	}

	ordinary := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-after-manual", "idem-after-manual")
	if _, err := store.Apply(context.Background(), ordinary); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("ordinary mutation accepted an out-of-band canonical edit: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, opening.ResultingHead) {
		t.Fatalf("dirty refusal changed the committed head: %+v %v", head, err)
	}

	service := &Service{Boundary: store.Boundary}
	reconcile := ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", Rationale: "Import the exact emergency record with an audit event",
		CorrelationID: "corr-reconcile", IdempotencyKey: "idem-reconcile",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{manual.ID: manual.Digest},
		WorkItems:             []WorkItemRecord{manual},
	}
	receipt, err := service.ManagerApply(context.Background(), reconcile)
	if err != nil {
		t.Fatalf("explicit reconciliation failed: %v", err)
	}
	if receipt.Event.Action != "reconcile.import" || receipt.ResultingHead.InventoryDigest == opening.ResultingHead.InventoryDigest {
		t.Fatalf("reconciliation did not emit a new inventory-bound head: %+v", receipt)
	}

	work := stateTestWorkItem("W-0002")
	work.CorrelationID, work.Digest = "corr-after-reconcile", ""
	clean := ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "work.create", CorrelationID: "corr-after-reconcile", IdempotencyKey: "idem-after-reconcile",
		ExpectedHeadRevision: receipt.ResultingHead.Revision, ExpectedHeadDigest: receipt.ResultingHead.Digest,
		WorkItems: []WorkItemRecord{work},
	}
	if _, err := service.ManagerApply(context.Background(), clean); err != nil {
		t.Fatalf("ordinary mutation remained blocked after reconciliation: %v", err)
	}
}

func TestCommittedInventoryDetectsUntrackedCanonicalRecord(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	extra := stateTestWorkItem("W-0002")
	extra.CorrelationID = "corr-manual-extra"
	extra.Digest = ""
	sealedValue, body, err := sealWorkItemRecord(extra)
	if err != nil {
		t.Fatal(err)
	}
	extra = sealedValue.(WorkItemRecord)
	target := filepath.Join(root, "active", "test-campaign", "work-items", "W-0002.json")
	if err := os.WriteFile(target, body, 0o644); err != nil {
		t.Fatal(err)
	}
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0003", "corr-untracked", "idem-untracked")
	if _, err := store.Apply(context.Background(), request); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("untracked canonical record did not dirty the project: %v", err)
	}
	service := &Service{Boundary: store.Boundary}
	receipt, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-import-extra", IdempotencyKey: "idem-import-extra",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{extra.ID: extra.Digest}, WorkItems: []WorkItemRecord{extra},
	})
	if err != nil {
		t.Fatalf("explicit import of an untracked canonical record failed: %v", err)
	}
	next := stateTestWorkItem("W-0003")
	next.CorrelationID, next.Digest = "corr-after-extra", ""
	if _, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "work.create", CorrelationID: "corr-after-extra", IdempotencyKey: "idem-after-extra",
		ExpectedHeadRevision: receipt.ResultingHead.Revision, ExpectedHeadDigest: receipt.ResultingHead.Digest,
		WorkItems: []WorkItemRecord{next},
	}); err != nil {
		t.Fatalf("ordinary mutation remained blocked after importing extra record: %v", err)
	}
}

func TestDerivedStateViewAndCacheRemainOutsideCommittedInventory(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	view := filepath.Join(root, "active", "test-campaign", "STATE.md")
	if err := os.WriteFile(view, []byte("derived and disposable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, ".re-discipline", "cache", "knowledge", "scratch")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("derived\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-derived", "idem-derived")
	if _, err := store.Apply(context.Background(), request); err != nil {
		t.Fatalf("derived view or cache changed canonical inventory: %v", err)
	}
}

func TestFirstStateHeadBaselinesExistingTruthAndHistoryBytes(t *testing.T) {
	store, root := newStateTestStore(t)
	truthPath := filepath.Join(root, "docs", "truth", "existing.md")
	historyPath := filepath.Join(root, "docs", "history", "campaigns", "prior", "summary.md")
	for _, target := range []string{truthPath, historyPath} {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	truthBody := []byte("# Existing truth\n")
	historyBody := []byte("# Existing campaign history\n")
	if err := os.WriteFile(truthPath, truthBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, historyBody, 0o644); err != nil {
		t.Fatal(err)
	}

	_, opening := openStateTestCampaign(t, store)
	inventory, err := store.loadCommittedInventory(opening.ResultingHead)
	if err != nil {
		t.Fatal(err)
	}
	entries := inventoryEntriesMap(inventory)
	if entries["docs/truth/existing.md"] != "sha256:"+SHA256Bytes(truthBody) ||
		entries["docs/history/campaigns/prior/summary.md"] != "sha256:"+SHA256Bytes(historyBody) {
		t.Fatalf("first state head omitted existing durable knowledge bytes: %+v", inventory.Entries)
	}

	newTruth := filepath.Join(root, "docs", "truth", "unmanaged-addition.md")
	if err := os.WriteFile(newTruth, []byte("# Out-of-band truth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-new-truth", "idem-new-truth")
	if _, err := store.Apply(context.Background(), request); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("unmanaged truth addition did not dirty the committed inventory: %v", err)
	}
}

func TestCommittedInventoryDetectsFirstOutOfBandTruthAddition(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	truthPath := filepath.Join(root, "docs", "truth", "first-unmanaged-truth.md")
	if err := os.MkdirAll(filepath.Dir(truthPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(truthPath, []byte("# Out-of-band truth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-first-truth", "idem-first-truth")
	if _, err := store.Apply(context.Background(), request); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("first unmanaged truth addition did not dirty the committed inventory: %v", err)
	}
}

func TestReconciliationDrainsDirtyPathsWithoutUnblockingEarly(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	first := graph.WorkItems["W-0001"]
	first.Title, first.Digest = "Emergency edit one", ""
	sealedFirst, firstBody, err := sealWorkItemRecord(first)
	if err != nil {
		t.Fatal(err)
	}
	first = sealedFirst.(WorkItemRecord)
	firstPath := filepath.Join(root, "active", "test-campaign", "work-items", "W-0001.json")
	if err := os.WriteFile(firstPath, firstBody, 0o644); err != nil {
		t.Fatal(err)
	}
	second := stateTestWorkItem("W-0002")
	second.CorrelationID, second.Digest = "corr-manual-second", ""
	sealedSecond, secondBody, err := sealWorkItemRecord(second)
	if err != nil {
		t.Fatal(err)
	}
	second = sealedSecond.(WorkItemRecord)
	secondPath := filepath.Join(root, "active", "test-campaign", "work-items", "W-0002.json")
	if err := os.WriteFile(secondPath, secondBody, 0o644); err != nil {
		t.Fatal(err)
	}

	service := &Service{Boundary: store.Boundary}
	firstReceipt, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-reconcile-first", IdempotencyKey: "idem-reconcile-first",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{first.ID: first.Digest}, WorkItems: []WorkItemRecord{first},
	})
	if err != nil {
		t.Fatalf("first partial reconciliation failed: %v", err)
	}
	probe := stateTestCreateWorkRequest(firstReceipt.ResultingHead, "W-0003", "corr-still-dirty", "idem-still-dirty")
	if _, err := store.Apply(context.Background(), probe); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("partial reconciliation unblocked ordinary mutation: %v", err)
	}
	secondReceipt, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-reconcile-second", IdempotencyKey: "idem-reconcile-second",
		ExpectedHeadRevision: firstReceipt.ResultingHead.Revision, ExpectedHeadDigest: firstReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{second.ID: second.Digest}, WorkItems: []WorkItemRecord{second},
	})
	if err != nil {
		t.Fatalf("second partial reconciliation failed: %v", err)
	}
	status := service.canonicalStateIntegrityStatus()
	if clean, _ := status["clean"].(bool); !clean {
		t.Fatalf("all dirty paths were reconciled but integrity stayed dirty: %+v", status)
	}
	if secondReceipt.ResultingHead.InventoryDigest == firstReceipt.ResultingHead.InventoryDigest {
		t.Fatal("second reconciliation did not advance the bound inventory")
	}
}

func TestReconciliationRefusesImplicitAdoptionOfDirtyCampaign(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	manualCampaign := *graph.Campaign
	manualCampaign.Title, manualCampaign.Digest = "Emergency campaign title", ""
	sealedCampaign, campaignBody, err := sealCampaignRecord(manualCampaign)
	if err != nil {
		t.Fatal(err)
	}
	manualCampaign = sealedCampaign.(CampaignRecord)
	campaignPath := filepath.Join(root, "active", "test-campaign", "campaign.json")
	if err := os.WriteFile(campaignPath, campaignBody, 0o644); err != nil {
		t.Fatal(err)
	}
	manualWork := graph.WorkItems["W-0001"]
	manualWork.Title, manualWork.Digest = "Emergency work title", ""
	sealedWork, workBody, err := sealWorkItemRecord(manualWork)
	if err != nil {
		t.Fatal(err)
	}
	manualWork = sealedWork.(WorkItemRecord)
	workPath := filepath.Join(root, "active", "test-campaign", "work-items", "W-0001.json")
	if err := os.WriteFile(workPath, workBody, 0o644); err != nil {
		t.Fatal(err)
	}

	service := &Service{Boundary: store.Boundary}
	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-work-only", IdempotencyKey: "idem-work-only",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{manualWork.ID: manualWork.Digest},
		WorkItems:             []WorkItemRecord{manualWork},
	})
	if !errors.Is(err, ErrStateDirty) || !strings.Contains(err.Error(), "campaign.json") {
		t.Fatalf("work-only reconciliation implicitly adopted a dirty campaign: %v", err)
	}
	head, headErr := store.LoadHead()
	if headErr != nil || !reflect.DeepEqual(head, opening.ResultingHead) {
		t.Fatalf("implicit-adoption refusal changed the head: %+v %v", head, headErr)
	}

	receipt, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-both-explicit", IdempotencyKey: "idem-both-explicit",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		ExpectedRecordDigests: map[string]string{
			manualCampaign.ID: manualCampaign.Digest,
			manualWork.ID:     manualWork.Digest,
		},
		Campaign:  &manualCampaign,
		WorkItems: []WorkItemRecord{manualWork},
	})
	if err != nil {
		t.Fatalf("explicit campaign and work reconciliation failed: %v", err)
	}
	result, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if result.Campaign.Title != manualCampaign.Title || result.Campaign.Revision != manualCampaign.Revision+1 ||
		result.Campaign.LastEventID != receipt.Event.ID {
		t.Fatalf("explicit campaign import did not preserve content and advance transaction metadata: %+v", result.Campaign)
	}
}

func TestReconciliationRefusesImplicitAdoptionOfDirtyEventJournal(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	manual := graph.WorkItems["W-0001"]
	manual.Title, manual.Digest = "Emergency work title", ""
	sealed, body, err := sealWorkItemRecord(manual)
	if err != nil {
		t.Fatal(err)
	}
	manual = sealed.(WorkItemRecord)
	workPath := filepath.Join(root, "active", "test-campaign", "work-items", "W-0001.json")
	if err := os.WriteFile(workPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "active", "test-campaign", "events", "events.jsonl")
	events, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	events = append(events, '\n') // Valid but byte-dirty JSONL framing.
	if err := os.WriteFile(eventPath, events, 0o644); err != nil {
		t.Fatal(err)
	}

	service := &Service{Boundary: store.Boundary}
	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-dirty-event", IdempotencyKey: "idem-dirty-event",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{manual.ID: manual.Digest}, WorkItems: []WorkItemRecord{manual},
	})
	if !errors.Is(err, ErrStateDirty) || !strings.Contains(err.Error(), "events.jsonl") {
		t.Fatalf("reconciliation implicitly adopted a dirty event journal: %v", err)
	}
	head, headErr := store.LoadHead()
	if headErr != nil || !reflect.DeepEqual(head, opening.ResultingHead) {
		t.Fatalf("dirty-event refusal changed the head: %+v %v", head, headErr)
	}
}

func TestReconciliationRequiresAnExplicitDirtyRecord(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	manual := graph.WorkItems["W-0001"]
	manual.Title, manual.Digest = "Unsubmitted emergency work title", ""
	_, body, err := sealWorkItemRecord(manual)
	if err != nil {
		t.Fatal(err)
	}
	workPath := filepath.Join(root, "active", "test-campaign", "work-items", "W-0001.json")
	if err := os.WriteFile(workPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cleanCampaign := *graph.Campaign
	service := &Service{Boundary: store.Boundary}
	_, err = service.ManagerApply(context.Background(), ManagerApplyRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Action: "reconcile.import", CorrelationID: "corr-clean-only", IdempotencyKey: "idem-clean-only",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{cleanCampaign.ID: cleanCampaign.Digest}, Campaign: &cleanCampaign,
	})
	if !errors.Is(err, ErrStateDirty) || !strings.Contains(err.Error(), "at least one dirty canonical path") {
		t.Fatalf("reconciliation without an explicit dirty record was accepted: %v", err)
	}
}
