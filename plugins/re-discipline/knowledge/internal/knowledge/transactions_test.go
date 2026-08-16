package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
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

func crossProcessCreateWorkRequest(
	head StateHead,
	slug, campaignID, id, correlation, key string,
) StateTransactionRequest {
	work := stateTestWorkItem(id)
	work.CampaignID = campaignID
	work.CorrelationID, work.Digest = correlation, ""
	return StateTransactionRequest{
		CampaignSlug: slug, CampaignID: campaignID, Actor: "manager", Authority: "manager",
		Action: "work.create", CorrelationID: correlation, IdempotencyKey: key,
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		RebaseHead: true,
		Writes:     []StateWrite{{Path: "active/" + slug + "/work-items/" + id + ".json", Record: work}},
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

func TestTransactionReceiptAcceptsLegacyHeadBoundIdentityLazily(t *testing.T) {
	head := StateHead{Revision: 7, Digest: stateTestDigest("7")}
	request := stateTestCreateWorkRequest(
		head, "W-0002", "corr-legacy-replay", "idem-legacy-replay")
	request.RebaseHead = true
	current, err := prepareTransactionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := prepareTransactionRequestIdentity(request, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.RequestDigest == legacy.RequestDigest {
		t.Fatal("record-scoped identity still depends on the observed global head")
	}
	if !receiptAcceptsPreparedRequest(
		StateTransactionReceipt{RequestDigest: legacy.RequestDigest}, current) {
		t.Fatal("legacy head-bound receipt no longer replays")
	}
}

func TestStateTransactionInternalRebaseStillRejectsSameRecordCollision(t *testing.T) {
	store, _ := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	requests := []StateTransactionRequest{
		stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-rebase-two-a", "idem-rebase-two-a"),
		stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-rebase-two-b", "idem-rebase-two-b"),
	}
	for index := range requests {
		requests[index].RebaseHead = true
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
			t.Fatalf("unexpected same-record result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("same-record collision must produce one commit and one true conflict: %v", errs)
	}
	graph, err := store.LoadCampaignGraph("C-TEST")
	if err != nil || len(graph.WorkItems) != 2 || graph.Validate() != nil {
		t.Fatalf("same-record collision corrupted the campaign: items=%d err=%v", len(graph.WorkItems), err)
	}
}

func TestStateTransactionCrossProcessHelper(t *testing.T) {
	if os.Getenv("RE_DISCIPLINE_CROSS_PROCESS_HELPER") != "1" {
		return
	}
	revision, err := strconv.ParseInt(os.Getenv("RE_DISCIPLINE_EXPECTED_REVISION"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStateStore(os.Getenv("RE_DISCIPLINE_PROJECT_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, _ := time.Parse(time.RFC3339, stateTestTime)
	store.Now = func() time.Time { return fixed }
	request := crossProcessCreateWorkRequest(
		StateHead{Revision: revision, Digest: os.Getenv("RE_DISCIPLINE_EXPECTED_DIGEST")},
		os.Getenv("RE_DISCIPLINE_CAMPAIGN_SLUG"),
		os.Getenv("RE_DISCIPLINE_CAMPAIGN_ID"),
		os.Getenv("RE_DISCIPLINE_WORK_ID"),
		os.Getenv("RE_DISCIPLINE_CORRELATION_ID"),
		os.Getenv("RE_DISCIPLINE_IDEMPOTENCY_KEY"),
	)
	if err := os.WriteFile(os.Getenv("RE_DISCIPLINE_READY_FILE"), []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("RE_DISCIPLINE_START_FILE")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for cross-process start barrier")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, applyErr := store.Apply(context.Background(), request)
	result := "success"
	if errors.Is(applyErr, ErrStateConflict) {
		result = "conflict"
	} else if applyErr != nil {
		result = "error: " + applyErr.Error()
	}
	if err := os.WriteFile(os.Getenv("RE_DISCIPLINE_RESULT_FILE"), []byte(result+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(result, "error:") {
		t.Fatal(result)
	}
}

func TestStateTransactionConcurrentManagersSerializeAcrossProcesses(t *testing.T) {
	store, root := newStateTestStore(t)
	_, _ = openStateTestCampaign(t, store)
	topologyOpenCampaign(t, store, "other-campaign", "C-OTHER", "W-0001", "corr-other-open", "idem-other-open")
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	type processCase struct {
		slug, campaignID, workID, correlation, idempotency string
		ready, result                                      string
		command                                            *exec.Cmd
		output                                             bytes.Buffer
	}
	cases := []processCase{
		{
			slug: "test-campaign", campaignID: "C-TEST", workID: "W-0002",
			correlation: "corr-process-first", idempotency: "idem-process-first",
		},
		{
			slug: "other-campaign", campaignID: "C-OTHER", workID: "W-0002",
			correlation: "corr-process-second", idempotency: "idem-process-second",
		},
	}
	barrierDirectory := t.TempDir()
	startFile := filepath.Join(barrierDirectory, "start")
	for index := range cases {
		item := &cases[index]
		item.ready = filepath.Join(barrierDirectory, "ready-"+strconv.Itoa(index))
		item.result = filepath.Join(barrierDirectory, "result-"+strconv.Itoa(index))
		item.command = exec.Command(os.Args[0], "-test.run=^TestStateTransactionCrossProcessHelper$", "-test.count=1")
		item.command.Env = append(os.Environ(),
			"RE_DISCIPLINE_CROSS_PROCESS_HELPER=1",
			"RE_DISCIPLINE_PROJECT_ROOT="+root,
			"RE_DISCIPLINE_EXPECTED_REVISION="+strconv.FormatInt(head.Revision, 10),
			"RE_DISCIPLINE_EXPECTED_DIGEST="+head.Digest,
			"RE_DISCIPLINE_CAMPAIGN_SLUG="+item.slug,
			"RE_DISCIPLINE_CAMPAIGN_ID="+item.campaignID,
			"RE_DISCIPLINE_WORK_ID="+item.workID,
			"RE_DISCIPLINE_CORRELATION_ID="+item.correlation,
			"RE_DISCIPLINE_IDEMPOTENCY_KEY="+item.idempotency,
			"RE_DISCIPLINE_READY_FILE="+item.ready,
			"RE_DISCIPLINE_START_FILE="+startFile,
			"RE_DISCIPLINE_RESULT_FILE="+item.result,
		)
		item.command.Stdout = &item.output
		item.command.Stderr = &item.output
		if err := item.command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		ready := true
		for index := range cases {
			if _, err := os.Stat(cases[index].ready); err != nil {
				ready = false
				break
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cross-process writers did not reach the start barrier")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(startFile, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := make([]string, len(cases))
	for index := range cases {
		if err := cases[index].command.Wait(); err != nil {
			t.Fatalf("writer %d failed: %v\n%s", index, err, cases[index].output.String())
		}
		body, err := os.ReadFile(cases[index].result)
		if err != nil {
			t.Fatal(err)
		}
		results[index] = strings.TrimSpace(string(body))
	}
	if results[0] != "success" || results[1] != "success" {
		t.Fatalf("disjoint cross-process commits must both finish without caller retry, got %v", results)
	}
	for _, item := range cases {
		graph, err := store.LoadCampaignGraph(item.campaignID)
		if err != nil || len(graph.WorkItems) != 2 || graph.Validate() != nil {
			t.Fatalf("campaign %s is not a valid two-work-item successor: items=%d err=%v", item.campaignID, len(graph.WorkItems), err)
		}
	}
	head, err = store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := store.loadCommittedInventory(head)
	if err != nil {
		t.Fatal(err)
	}
	if drift, err := store.inventoryDrift(inventory); err != nil || len(drift) != 0 {
		t.Fatalf("cross-process commits left canonical byte drift: drift=%v err=%v", drift, err)
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

// The journal, not the engine, is the last line here. ValidateClosureJob never
// compares against a previous record, so before this rule any same-stage closure
// job write could quietly re-point FrozenCampaignRevision under an ordinary
// action and gate the campaign on records that no longer described it. Only
// closure.restart may move that freeze, and only through ValidateClosureRestart.
func TestOnlyClosureRestartMayRebindAClosureFreeze(t *testing.T) {
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	prior := sealClosureRestartTestJob(t, ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "coverage", Status: "running", FrozenCampaignRevision: 4,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{}, ProjectionDigests: map[string]string{},
	})
	sameStage := prior
	sameStage.Revision, sameStage.UpdatedAt = 2, "2026-08-02T20:01:00Z"
	if err := validateRecordTransition(
		prior, sealClosureRestartTestJob(t, sameStage), "closure.advance", "manager"); err != nil {
		t.Fatalf("an ordinary same-stage closure write was refused: %v", err)
	}
	rebound := sameStage
	rebound.FrozenCampaignRevision = 9
	if err := validateRecordTransition(
		prior, sealClosureRestartTestJob(t, rebound), "closure.advance", "manager"); err == nil {
		t.Fatal("a same-stage closure write rebound the campaign freeze under closure.advance")
	}
	counted := sameStage
	counted.Attempt = 2
	if err := validateRecordTransition(
		prior, sealClosureRestartTestJob(t, counted), "closure.advance", "manager"); err == nil {
		t.Fatal("a same-stage closure write moved the attempt counter under closure.advance")
	}
	// The same rebind under closure.restart is refused for a different reason -
	// the prior job is not reopened - which is exactly the point: restart routes
	// to ValidateClosureRestart and nothing else may reach that door at all.
	restart := prior
	restart.Revision, restart.UpdatedAt = 2, "2026-08-02T20:01:00Z"
	restart.Stage, restart.Status = "inventory", "running"
	restart.FrozenCampaignRevision, restart.Attempt = 9, 2
	if err := validateRecordTransition(
		prior, sealClosureRestartTestJob(t, restart), "closure.restart", "manager"); err == nil {
		t.Fatal("closure.restart re-entered a closure attempt that was never reopened")
	}
	reopened := prior
	reopened.Status = "reopened"
	reopened = sealClosureRestartTestJob(t, reopened)
	if err := validateRecordTransition(
		reopened, sealClosureRestartTestJob(t, restart), "closure.restart", "manager"); err != nil {
		t.Fatalf("closure.restart refused a well-formed re-entry from a reopened job: %v", err)
	}
}

// A closure plan is the frozen obligation of one attempt, so no action inside an
// attempt may shrink it. Restart is not inside an attempt: it ends one and
// freezes another, and it is the only action allowed to replace the plan.
func TestClosurePlanIsImmutableOutsideRestart(t *testing.T) {
	graph := closureTestGraph(t)
	destination := "docs/history/campaigns/2026-08-02-test"
	prior, err := BuildClosurePlan(graph, destination)
	if err != nil {
		t.Fatal(err)
	}
	forward := prior
	forward.CampaignRevision = prior.CampaignRevision + 3
	if err := sealClosurePlan(&forward); err != nil {
		t.Fatal(err)
	}
	if err := validateRecordTransition(nil, prior, "closure.start", "manager"); err != nil {
		t.Fatalf("a first closure plan was refused: %v", err)
	}
	for _, action := range []string{"closure.start", "closure.advance", "closure.project", "closure.finalize"} {
		if err := validateRecordTransition(prior, forward, action, "manager"); err == nil {
			t.Fatalf("action %s replaced an existing closure plan", action)
		}
	}
	if err := validateRecordTransition(prior, forward, "closure.restart", "manager"); err != nil {
		t.Fatalf("closure.restart could not re-plan: %v", err)
	}
	same := prior
	if err := validateRecordTransition(prior, same, "closure.restart", "manager"); err == nil {
		t.Fatal("closure.restart re-planned against the revision it already froze")
	}
	repointed := forward
	repointed.ArchiveDestination = "docs/history/campaigns/2026-08-02-other"
	if err := sealClosurePlan(&repointed); err != nil {
		t.Fatal(err)
	}
	if err := validateRecordTransition(prior, repointed, "closure.restart", "manager"); err == nil {
		t.Fatal("closure.restart repointed the campaign archive destination")
	}
}
