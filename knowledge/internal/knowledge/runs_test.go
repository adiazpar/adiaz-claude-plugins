package knowledge

import "testing"

func stateTestReturnedRun(revision int64, status string) RunRecord {
	run := RunRecord{
		RecordMeta: stateTestMeta("R-20260802-0001", revision),
		CampaignID: "C-TEST", PrimaryWorkItemID: "W-0001",
		ActorID: "manager", Role: "manager", Status: status,
		StartedAt: stateTestTime, ReturnedAt: stateTestTime,
		Report: stateTestFileHandle("report.md"), ResultSummary: "Evidence returned",
	}
	if isTerminalRun(status) {
		run.TerminalAt = stateTestTime
	}
	return run
}

func TestRunTransitionFreezesReturnedReport(t *testing.T) {
	previous := stateTestReturnedRun(2, "returned")
	next := stateTestReturnedRun(3, "completed")
	next.Report = &FileHandle{Path: previous.Report.Path, SHA256: stateTestDigest("2")}
	if err := ValidateRunTransition(&previous, next); err == nil {
		t.Fatal("completed transition changed the frozen returned report digest")
	}
	next.Report = previous.Report
	if err := ValidateRunTransition(&previous, next); err != nil {
		t.Fatalf("valid returned-to-completed transition rejected: %v", err)
	}
}

func TestRunTransitionRejectsIllegalSkip(t *testing.T) {
	previous := RunRecord{
		RecordMeta: stateTestMeta("R-20260802-0001", 1),
		CampaignID: "C-TEST", PrimaryWorkItemID: "W-0001",
		ActorID: "manager", Role: "manager", Status: "prepared",
	}
	next := stateTestReturnedRun(2, "completed")
	if err := ValidateRunTransition(&previous, next); err == nil {
		t.Fatal("prepared run skipped directly to completed")
	}
}

func TestWorkAndCampaignTerminalTransitionsAreClosed(t *testing.T) {
	campaign := stateTestCampaignRecord()
	campaign.Status = "closed"
	campaign.ClosedAt = stateTestTime
	campaign.ArchiveDestination = "docs/history/campaigns/test-campaign"
	nextCampaign := campaign
	nextCampaign.Revision++
	nextCampaign.Status = "open"
	if err := ValidateCampaignTransition(&campaign, nextCampaign); err == nil {
		t.Fatal("closed campaign reopened")
	}

	// A campaign is closed once; work is not. Done work reopens, because the
	// judgement that called it finished can turn out to be early - a closure
	// gate asking for a receipt the work never produced is exactly that case,
	// and a pack will not compile against a done item, so without the reopen
	// the run that would finish it can never be dispatched.
	item := stateTestWorkItem("W-0001")
	item.State, item.Outcome = "done", "Completed"
	reopened := item
	reopened.Revision++
	reopened.State, reopened.Outcome = "active", ""
	if err := ValidateWorkItemTransition(&item, reopened); err != nil {
		t.Fatalf("done work item could not be reopened: %v", err)
	}

	// Reopening is not a general amnesty on terminal states: done still cannot
	// be abandoned, and cancelled and superseded stay absorbing.
	cancelled := item
	cancelled.Revision++
	cancelled.State, cancelled.Outcome = "cancelled", ""
	if err := ValidateWorkItemTransition(&item, cancelled); err == nil {
		t.Fatal("done work item was cancelled")
	}
	for _, terminal := range []string{"cancelled", "superseded"} {
		absorbing := stateTestWorkItem("W-0001")
		absorbing.State = terminal
		escaped := absorbing
		escaped.Revision++
		escaped.State, escaped.Outcome = "active", ""
		if err := ValidateWorkItemTransition(&absorbing, escaped); err == nil {
			t.Fatalf("%s work item was reopened", terminal)
		}
	}
}

func TestRunReturnAutomaticallyQueuesBoundCurationWork(t *testing.T) {
	returned := stateTestReturnedRun(2, "returned")
	primary := stateTestWorkItem("W-0001")
	primary.Revision++
	request := ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignID: returned.CampaignID,
		CampaignSlug: "test-campaign", CorrelationID: "corr-test",
		Runs: []RunRecord{returned}, WorkItems: []WorkItemRecord{primary},
	}
	augmented := augmentRunReturnCuration(request)
	if len(augmented.WorkItems) != 2 {
		t.Fatalf("run.return did not create one curation work item: %d", len(augmented.WorkItems))
	}
	queue := augmented.WorkItems[1]
	if queue.ID != continuousCurationWorkID(returned.ID) || queue.State != "ready" ||
		queue.Assignee != "knowledge-curator" || !containsString(augmented.Runs[0].SpawnedWorkItemIDs, queue.ID) {
		t.Fatalf("automatic curation queue is not bound to returned run: %#v", queue)
	}
	graph := NewCampaignGraph()
	prior := stateTestReturnedRun(1, "running")
	graph.Runs[prior.ID] = prior
	writes := []preparedStateWrite{
		{Record: augmented.Runs[0], ExpectedRevision: 1},
		{Record: primary, ExpectedRevision: 1},
		{Record: queue, ExpectedRevision: 0},
	}
	transaction := StateTransactionRequest{Actor: request.Actor, CorrelationID: request.CorrelationID}
	if err := validateAppliedRunReturn(graph, transaction, writes); err != nil {
		t.Fatalf("valid atomic curation queue was rejected: %v", err)
	}

	withoutQueue := writes[:2]
	if err := validateAppliedRunReturn(graph, transaction, withoutQueue); err == nil {
		t.Fatal("returned run without automatic curation work was accepted")
	}
}

func TestCuratorReturnDoesNotRecursivelyQueueCuration(t *testing.T) {
	returned := stateTestReturnedRun(2, "returned")
	returned.Role, returned.ActorID = "curator", "knowledge-curator"
	returned.Brief, returned.ContextPack = stateTestFileHandle("brief.md"), stateTestFileHandle("context-pack.json")
	request := ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CorrelationID: "corr-test",
		Runs: []RunRecord{returned}, WorkItems: []WorkItemRecord{stateTestWorkItem("W-0001")},
	}
	augmented := augmentRunReturnCuration(request)
	if len(augmented.WorkItems) != 1 || len(augmented.Runs[0].SpawnedWorkItemIDs) != 0 {
		t.Fatal("curator return recursively queued another curator")
	}
}
