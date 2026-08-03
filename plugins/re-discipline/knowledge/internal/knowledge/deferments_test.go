package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func blockingDeferment(trigger DefermentTrigger) *DefermentContract {
	return &DefermentContract{
		Reason: "A prerequisite is unavailable", RevisitWhen: trigger,
		Owner: "manager", BlocksClosure: true,
		ClosureDisposition: DefermentDispositionResolve,
	}
}

func exportedDeferment(trigger DefermentTrigger, destination string) *DefermentContract {
	return &DefermentContract{
		Reason: "This work belongs in a later campaign", RevisitWhen: trigger,
		Owner: "manager", BlocksClosure: false,
		ClosureDisposition: DefermentDispositionExportBacklog,
		ClosureDestination: destination,
	}
}

func deferredStateTestItem(id string, contract *DefermentContract) WorkItemRecord {
	item := stateTestWorkItem(id)
	item.State = "deferred"
	item.Deferment = contract
	return item

}

func TestDefermentDateNearAndDueBoundaries(t *testing.T) {
	graph := stateTestGraph()
	item := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerDate, At: "2026-08-10T12:00:00Z",
	}))
	graph.WorkItems[item.ID] = item

	waiting, err := EvaluateDeferments(graph, time.Date(2026, 8, 3, 11, 59, 59, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	if waiting[item.ID].Status != DefermentStatusWaiting {
		t.Fatalf("date outside the near window was not waiting: %#v", waiting[item.ID])
	}
	near, err := EvaluateDeferments(graph, time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	if near[item.ID].Status != DefermentStatusNear {
		t.Fatalf("date at the near boundary was not near: %#v", near[item.ID])
	}
	due, err := EvaluateDeferments(graph, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	if due[item.ID].Status != DefermentStatusDue {
		t.Fatalf("date at the due boundary was not due: %#v", due[item.ID])
	}
	transitions := RecommendedDefermentTransitions(graph, due)
	if len(transitions) != 1 || transitions[0].Action != "work.update" || transitions[0].TargetState != "ready" {
		t.Fatalf("due deferment did not produce one deterministic ready transition: %#v", transitions)
	}
}

func TestDefermentWorkItemStateAndEventTriggers(t *testing.T) {
	graph := stateTestGraph()
	dependency := stateTestWorkItem("W-0002")
	dependency.State, dependency.Outcome = "done", "Dependency completed"
	dependent := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerWorkItemState, WorkItemID: dependency.ID, State: "done",
	}))
	dependent.Relations.DependsOn = []string{dependency.ID}
	eventItem := deferredStateTestItem("W-0003", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerEvent, Action: "source.available", AffectedID: "W-0003",
	}))
	graph.WorkItems[dependent.ID] = dependent
	graph.WorkItems[dependency.ID] = dependency
	graph.WorkItems[eventItem.ID] = eventItem
	if err := graph.Validate(); err != nil {
		t.Fatal(err)
	}
	events := []StateEvent{
		{ID: "E-20260803-120000-AAAAAA", Action: "source.available", AffectedIDs: []string{"W-9999"}},
		{ID: "E-20260803-120001-BBBBBB", Action: "source.available", AffectedIDs: []string{"W-0003"}},
	}
	evaluations, err := EvaluateDeferments(graph, time.Date(2026, 8, 3, 12, 0, 2, 0, time.UTC), events)
	if err != nil {
		t.Fatal(err)
	}
	if evaluations[dependent.ID].Status != DefermentStatusDue {
		t.Fatalf("satisfied work-item dependency was not due: %#v", evaluations[dependent.ID])
	}
	if got := evaluations[eventItem.ID]; got.Status != DefermentStatusDue || got.MatchedEvent != events[1].ID {
		t.Fatalf("event trigger did not bind the first exact action/affected-id match: %#v", got)
	}
}

func TestDefermentEventJournalRetainsOneMatchAndStillChecksTailIntegrity(t *testing.T) {
	root := t.TempDir()
	graph := stateTestGraph()
	item := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerEvent, Action: "source.available", AffectedID: "W-0001",
	}))
	graph.WorkItems[item.ID] = item
	var journal bytes.Buffer
	previousID := ""
	for index := 0; index < 3; index++ {
		event := StateEvent{
			SchemaVersion: CampaignSchemaVersion,
			ID:            fmt.Sprintf("E-20260803-12000%d-ABC%03d", index, index),
			Timestamp:     fmt.Sprintf("2026-08-03T12:00:0%dZ", index),
			Actor:         "manager", Authority: "manager", Action: "source.available",
			AffectedIDs: []string{"W-0001"}, PreviousRevision: int64(index),
			ResultingRevision: int64(index + 1), IdempotencyKey: fmt.Sprintf("event-%d", index),
			CorrelationID: fmt.Sprintf("event-%d", index), PreviousEventID: previousID,
			PreviousStateDigest: stateTestDigest("1"), ResultingStateDigest: stateTestDigest("2"),
			MutationDigest: stateTestDigest("3"),
		}
		if err := sealStateEvent(&event); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		journal.Write(body)
		journal.WriteByte('\n')
		previousID = event.ID
	}
	eventPath := filepath.Join(root, "active", "test-campaign", "events", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(eventPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventPath, journal.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := readMatchingDefermentEvents(boundary, "test-campaign", defermentEventTriggerKeys(graph))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "E-20260803-120000-ABC000" {
		t.Fatalf("event evaluation retained more than the first exact match: %#v", events)
	}
	corrupted := append([]byte(nil), journal.Bytes()...)
	needle := []byte(stateTestDigest("3"))
	last := bytes.LastIndex(corrupted, needle)
	if last < 0 {
		t.Fatal("test journal omitted its final mutation digest")
	}
	copy(corrupted[last:last+len(needle)], []byte(stateTestDigest("4")))
	if err := os.WriteFile(eventPath, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMatchingDefermentEvents(boundary, "test-campaign", defermentEventTriggerKeys(graph)); err == nil {
		t.Fatal("event evaluation stopped after its first match and ignored corrupt journal tail")
	}
}

func TestDefermentRejectsInvalidTriggerAndClosureCombinations(t *testing.T) {
	base := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerDate, At: "2026-08-10T12:00:00Z",
	}))
	cases := []struct {
		name     string
		contract *DefermentContract
	}{
		{"date with event field", blockingDeferment(DefermentTrigger{Type: DefermentTriggerDate, At: "2026-08-10T12:00:00Z", Action: "source.available"})},
		{"blocking export", &DefermentContract{Reason: "later", RevisitWhen: DefermentTrigger{Type: DefermentTriggerEvent, Action: "source.available"}, Owner: "manager", BlocksClosure: true, ClosureDisposition: DefermentDispositionExportBacklog, ClosureDestination: "docs/backlog/later.md"}},
		{"nonblocking without destination", &DefermentContract{Reason: "later", RevisitWhen: DefermentTrigger{Type: DefermentTriggerEvent, Action: "source.available"}, Owner: "manager", BlocksClosure: false, ClosureDisposition: DefermentDispositionExportBacklog}},
		{"navigation destination", exportedDeferment(DefermentTrigger{Type: DefermentTriggerEvent, Action: "source.available"}, "docs/backlog/INDEX.md")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			item := base
			item.Deferment = test.contract
			if err := ValidateWorkItem(item); err == nil {
				t.Fatal("invalid deferment contract was accepted")
			}
		})
	}

	graph := stateTestGraph()
	self := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerWorkItemState, WorkItemID: "W-0001", State: "done",
	}))
	graph.WorkItems[self.ID] = self
	if err := graph.Validate(); err == nil || !strings.Contains(err.Error(), "own state") {
		t.Fatalf("self-referential work-item trigger was accepted: %v", err)
	}
}

func TestDefermentGeneratedStateAndReplayAreStable(t *testing.T) {
	graph := stateTestGraph()
	item := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerDate, At: "2026-08-03T12:00:00Z",
	}))
	graph.WorkItems[item.ID] = item
	asOf := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	first, err := RenderCampaignStateAt(graph, asOf, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderCampaignStateAt(graph, asOf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical graph, event history, and instant produced unstable generated state")
	}
	text := string(first)
	if !strings.Contains(text, "## Due or near deferments") ||
		!strings.Contains(text, "`W-0001` [due]") ||
		!strings.Contains(text, "## Recommended transitions") ||
		!strings.Contains(text, "`work.update -> ready`") {
		t.Fatalf("generated state omitted due deferment or transition:\n%s", text)
	}
}

func TestClosureExportsDeferredWorkToItsContractDestination(t *testing.T) {
	root := t.TempDir()
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Boundary: boundary}
	graph := stateTestGraph()
	item := deferredStateTestItem("W-0001", exportedDeferment(DefermentTrigger{
		Type: DefermentTriggerEvent, Action: "next.campaign",
	}, "docs/backlog/deferred-W-0001.md"))
	graph.WorkItems[item.ID] = item
	graph.ClosurePlan = &ClosurePlan{ProjectionFindingIDs: []string{}}
	graph.ClosureJob = &ClosureJob{ArchiveDestination: "docs/history/campaigns/2026-08-03-test"}
	coverage := ClosureCoverage{WorkItemCoverage: map[string]string{item.ID: "exported-backlog"}}
	request := ClosureApplyRequest{ExpectedArtifactDigests: map[string]string{}, ProjectionDestinations: map[string]string{}}
	_, digests, artifacts, err := service.prepareClosureProjections(graph, graph, coverage, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Path != item.Deferment.ClosureDestination ||
		digests[artifacts[0].Path] != artifacts[0].ContentDigest ||
		!strings.Contains(string(artifacts[0].Body), "sourceWorkItem:") ||
		!strings.Contains(string(artifacts[0].Body), "W-0001") {
		t.Fatalf("closure did not materialize the declared backlog export: digests=%v artifacts=%#v", digests, artifacts)
	}
	if err := validateExportedDefermentIDs(graph, []string{item.ID}); err != nil {
		t.Fatalf("valid explicit export was rejected: %v", err)
	}
	blocking := item
	blocking.Deferment = blockingDeferment(DefermentTrigger{Type: DefermentTriggerEvent, Action: "next.campaign"})
	graph.WorkItems[item.ID] = blocking
	if err := validateExportedDefermentIDs(graph, []string{item.ID}); err == nil {
		t.Fatal("closure exported an item whose contract requires resolution")
	}
}

func TestColdResumeSurfacesEventDueDefermentInEveryBoundedView(t *testing.T) {
	root := t.TempDir()
	event := StateEvent{
		SchemaVersion: CampaignSchemaVersion, ID: "E-20260803-120000-ABCDEF",
		Timestamp: "2026-08-03T12:00:00Z", Actor: "manager", Authority: "manager",
		Action: "source.available", AffectedIDs: []string{"W-0001"},
		PreviousRevision: 0, ResultingRevision: 1, IdempotencyKey: "deferment-event",
		CorrelationID: "deferment-event", PreviousStateDigest: stateTestDigest("1"),
		ResultingStateDigest: stateTestDigest("2"), MutationDigest: stateTestDigest("3"),
	}
	if err := sealStateEvent(&event); err != nil {
		t.Fatal(err)
	}
	campaign := stateTestCampaignRecord()
	campaign.Slug, campaign.LastEventID = "deferment-test", event.ID
	campaign.UpdatedAt = event.Timestamp
	campaignAny, _, err := sealCampaignRecord(campaign)
	if err != nil {
		t.Fatal(err)
	}
	campaign = campaignAny.(CampaignRecord)
	item := deferredStateTestItem("W-0001", blockingDeferment(DefermentTrigger{
		Type: DefermentTriggerEvent, Action: event.Action, AffectedID: "W-0001",
	}))
	item.UpdatedAt = event.Timestamp
	itemAny, _, err := sealWorkItemRecord(item)
	if err != nil {
		t.Fatal(err)
	}
	item = itemAny.(WorkItemRecord)
	writeCanonical := func(relative string, value any) {
		t.Helper()
		body, marshalErr := canonicalJSON(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		if mkdirErr := os.MkdirAll(filepath.Dir(absolute), 0o700); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		if writeErr := os.WriteFile(absolute, body, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	writeCanonical("active/deferment-test/campaign.json", campaign)
	writeCanonical("active/deferment-test/work-items/W-0001.json", item)
	eventBody, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "active", "deferment-test", "events", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(eventPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventPath, append(eventBody, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) }
	configuration := Configuration{Valid: true}
	requests := []StateRequest{
		{Mode: "orient", TokenBudget: 4000, MaxCards: 30},
		{Mode: "resume", CampaignID: "C-TEST", TokenBudget: 4000, MaxCards: 30},
		{Mode: "work", CampaignID: "C-TEST", WorkItemID: "W-0001", TokenBudget: 4000, MaxCards: 30},
		{Mode: "closure", CampaignID: "C-TEST", TokenBudget: 4000, MaxCards: 30},
	}
	var firstResumeDigest string
	for _, request := range requests {
		view, compileErr := compileStateView(context.Background(), store, configuration, nil, request)
		if compileErr != nil {
			t.Fatalf("%s cold-resume view failed: %v", request.Mode, compileErr)
		}
		foundDue, foundTransition := false, false
		for _, card := range view.Cards {
			if card.ID == "W-0001" && card.Metadata["defermentStatus"] == DefermentStatusDue &&
				card.Metadata["defermentMatchedEventId"] == event.ID {
				foundDue = true
			}
			if card.Metadata["workItemId"] == "W-0001" && card.Metadata["action"] == "work.update" {
				foundTransition = true
			}
		}
		if !foundDue || !foundTransition {
			t.Fatalf("%s omitted due event deferment or recommendation: %#v", request.Mode, view.Cards)
		}
		if request.Mode == "resume" {
			firstResumeDigest = view.Digest
		}
	}
	replayed, err := compileStateView(context.Background(), store, configuration, nil, requests[1])
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Digest != firstResumeDigest {
		t.Fatalf("cold resume was not stable: %s != %s", replayed.Digest, firstResumeDigest)
	}
}

func TestLegacyDeferredImportUsesTypedReopenEvent(t *testing.T) {
	items := legacyCampaignWorkItems([]byte("# Campaign\n\n## Deferred Work\n\n- verify later\n"), "C-TEST", MigrationState{Actor: "manager"})
	found := false
	for _, item := range items {
		if item.State != "deferred" {
			continue
		}
		found = true
		if item.Deferment == nil || item.Deferment.RevisitWhen.Type != DefermentTriggerEvent ||
			item.Deferment.RevisitWhen.Action != "work.update" ||
			item.Deferment.RevisitWhen.AffectedID != item.ID ||
			item.Deferment.ClosureDisposition != DefermentDispositionResolve {
			t.Fatalf("legacy deferment was not converted to a typed exact-item event: %#v", item.Deferment)
		}
	}
	if !found {
		t.Fatal("legacy deferred frontier statement was not imported")
	}
}
