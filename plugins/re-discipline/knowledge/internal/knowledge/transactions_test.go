package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStateTestStore(t *testing.T) (*StateStore, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".re-discipline"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".re-discipline", "project-profile.md"),
		[]byte("# Test project\n\n"+SharedLawsMarker+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fixed, _ := time.Parse(time.RFC3339, stateTestTime)
	store.Now = func() time.Time { return fixed }
	return store, root
}

func stateTestOpenRequest(head StateHead) StateTransactionRequest {
	campaign := stateTestCampaignRecord()
	campaign.CorrelationID, campaign.Digest = "corr-open", ""
	work := stateTestWorkItem("W-0001")
	work.CorrelationID, work.Digest = "corr-open", ""
	return StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager", Authority: "manager",
		Action: "campaign.open", Rationale: "Open the fixture campaign",
		CorrelationID: "corr-open", IdempotencyKey: "idem-open",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{
			{Path: "active/test-campaign/campaign.json", Record: campaign},
			{Path: "active/test-campaign/work-items/W-0001.json", Record: work},
		},
	}
}

func openStateTestCampaign(t *testing.T, store *StateStore) (StateTransactionRequest, StateTransactionReceipt) {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	request := stateTestOpenRequest(head)
	receipt, err := store.Apply(context.Background(), request)
	if err != nil {
		t.Fatalf("open campaign: %v", err)
	}
	return request, receipt
}

func stateTestCreateWorkRequest(head StateHead, id, correlation, key string) StateTransactionRequest {
	work := stateTestWorkItem(id)
	work.CorrelationID, work.Digest = correlation, ""
	return StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager", Authority: "manager",
		Action: "work.create", CorrelationID: correlation, IdempotencyKey: key,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{{Path: "active/test-campaign/work-items/" + id + ".json", Record: work}},
	}
}

func TestStateTransactionOpensCampaignAtomically(t *testing.T) {
	store, root := newStateTestStore(t)
	_, receipt := openStateTestCampaign(t, store)
	if receipt.PreviousHead.Revision != 0 || receipt.ResultingHead.Revision != 1 || len(receipt.Records) != 2 {
		t.Fatalf("unexpected opening receipt: %+v", receipt)
	}
	head, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, receipt.ResultingHead) {
		t.Fatalf("published head mismatch: head=%+v err=%v", head, err)
	}
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	if graph.Campaign.LastEventID != receipt.Event.ID || graph.WorkItems["W-0001"].ID == "" {
		t.Fatalf("opening graph is incomplete: %+v", graph)
	}
	view, err := os.ReadFile(filepath.Join(root, "active", "test-campaign", "STATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if "sha256:"+SHA256Bytes(view) != receipt.GeneratedViewDigest ||
		!strings.Contains(string(view), "Exercise the canonical state graph") {
		t.Fatal("generated STATE.md is missing or not bound to the receipt")
	}
	eventBody, err := os.ReadFile(filepath.Join(root, "active", "test-campaign", "events", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var event StateEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(eventBody))), &event); err != nil || verifyStateEvent(event) != nil {
		t.Fatalf("event journal is invalid: %v", err)
	}
	campaigns, err := store.ListCampaigns()
	if err != nil || len(campaigns) != 1 || campaigns[0].ID != "C-TEST" {
		t.Fatalf("deterministic campaign enumeration failed: %+v err=%v", campaigns, err)
	}
}

func TestStateTransactionIdempotentRetryAndConflict(t *testing.T) {
	store, _ := newStateTestStore(t)
	request, first := openStateTestCampaign(t, store)
	retried, err := store.Apply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(first, retried) {
		t.Fatalf("same idempotent request did not return the original receipt: %v", err)
	}
	changed := request
	changed.Rationale = "Different semantic input"
	if _, err := store.Apply(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reused idempotency key with different input returned %v", err)
	}
	head, err := store.LoadHead()
	if err != nil || head.Digest != first.ResultingHead.Digest {
		t.Fatalf("idempotency conflict changed the head: %+v err=%v", head, err)
	}
}

func TestStateTransactionConcurrentStaleWriters(t *testing.T) {
	store, _ := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	requests := []StateTransactionRequest{
		stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-two", "idem-two"),
		stateTestCreateWorkRequest(opening.ResultingHead, "W-0003", "corr-three", "idem-three"),
	}
	start := make(chan struct{})
	errs := make([]error, len(requests))
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errs[index] = store.Apply(context.Background(), requests[index])
		}(index)
	}
	close(start)
	wait.Wait()
	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrStateConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent writer result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("wanted one commit and one CAS conflict, got errors %v", errs)
	}
	graph, err := store.LoadCampaignGraph("test-campaign")
	if err != nil || len(graph.WorkItems) != 2 {
		t.Fatalf("concurrent result graph is not a single valid successor: items=%d err=%v", len(graph.WorkItems), err)
	}
}

func TestStateTransactionClosureArtifactSharesCommit(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	graph, err := store.LoadCampaignGraph("test-campaign")
	if err != nil {
		t.Fatal(err)
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.Status, campaign.ClosingAt = "closing", stateTestTime
	campaign.CorrelationID, campaign.Digest = "corr-close", ""
	job := ClosureJob{
		RecordMeta: stateTestMeta("closure-job", 1), CampaignID: "C-TEST",
		Stage: "inventory", Status: "running", FrozenCampaignRevision: campaign.Revision,
	}
	job.CorrelationID, job.Digest = "corr-close", ""
	body := []byte("archive staging payload\n")
	request := StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager", Authority: "manager",
		Action: "closure.start", CorrelationID: "corr-close", IdempotencyKey: "idem-close",
		ExpectedHeadRevision: opening.ResultingHead.Revision, ExpectedHeadDigest: opening.ResultingHead.Digest,
		Writes: []StateWrite{
			{Path: "active/test-campaign/campaign.json", ExpectedRevision: graph.Campaign.Revision, ExpectedDigest: graph.Campaign.Digest, Record: campaign},
			{Path: "active/test-campaign/closure/job.json", Record: job},
		},
		Artifacts: []StateArtifactWrite{{
			Path:          "active/test-campaign/closure/staging/archive.bin",
			ContentDigest: "sha256:" + SHA256Bytes(body), Body: body,
		}},
	}
	receipt, err := store.Apply(context.Background(), request)
	if err != nil {
		t.Fatalf("closure artifact transaction: %v", err)
	}
	if len(receipt.Artifacts) != 1 || receipt.Artifacts[0].ContentDigest != request.Artifacts[0].ContentDigest {
		t.Fatalf("artifact receipt is incomplete: %+v", receipt.Artifacts)
	}
	published, err := os.ReadFile(filepath.Join(root, "active", "test-campaign", "closure", "staging", "archive.bin"))
	if err != nil || !reflect.DeepEqual(published, body) {
		t.Fatalf("artifact was not published with the state commit: %q err=%v", published, err)
	}
	retried, err := store.Apply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(retried, receipt) {
		t.Fatalf("artifact transaction retry did not replay its receipt: %v", err)
	}
	replacement := []byte("replacement staging payload\n")
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	update := StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager", Authority: "manager",
		Action: "closure.stage", CorrelationID: "corr-close-update", IdempotencyKey: "idem-close-update",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Artifacts: []StateArtifactWrite{{
			Path:           "active/test-campaign/closure/staging/archive.bin",
			ExpectedDigest: request.Artifacts[0].ContentDigest,
			ContentDigest:  "sha256:" + SHA256Bytes(replacement), Body: replacement,
		}},
	}
	if _, err := store.Apply(context.Background(), update); err != nil {
		t.Fatalf("artifact exact-digest replacement failed: %v", err)
	}
	published, err = os.ReadFile(filepath.Join(root, "active", "test-campaign", "closure", "staging", "archive.bin"))
	if err != nil || !reflect.DeepEqual(published, replacement) {
		t.Fatalf("artifact replacement was not published: %q err=%v", published, err)
	}
	staleHead, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	stale := update
	stale.IdempotencyKey = "idem-close-stale"
	stale.ExpectedHeadRevision, stale.ExpectedHeadDigest = staleHead.Revision, staleHead.Digest
	if _, err := store.Apply(context.Background(), stale); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("stale artifact digest returned %v", err)
	}

	disallowed := request
	disallowed.IdempotencyKey = "idem-close-bad"
	disallowed.Artifacts = []StateArtifactWrite{{
		Path: "src/unrelated.txt", ContentDigest: "sha256:" + SHA256Bytes(body), Body: body,
	}}
	if _, err := store.Apply(context.Background(), disallowed); err == nil {
		t.Fatal("closure artifact escaped its destination allowlist")
	}
}

func TestStateStoreIgnoresDerivedCacheDeletion(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	cache := filepath.Join(root, ".re-discipline", "cache", "derived")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "index.bin"), []byte("derived"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".re-discipline", "cache")); err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.Digest != opening.ResultingHead.Digest {
		t.Fatalf("cache deletion changed canonical head: %+v err=%v", head, err)
	}
	if _, err := store.LoadCampaignGraph("test-campaign"); err != nil {
		t.Fatalf("cache deletion affected canonical graph: %v", err)
	}
}
