package knowledge

import (
	"strings"
	"testing"
)

func TestCanonicalRunWorkspaceOwnsEveryHandlePath(t *testing.T) {
	workspace, err := newCanonicalRunWorkspace("test-campaign", "R-20260802-0099")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		kind runHandleKind
		want string
	}{
		{runBriefHandle, "active/test-campaign/runs/R-20260802-0099/brief.md"},
		{runContextPackHandle, "active/test-campaign/runs/R-20260802-0099/context-pack.json"},
		{runReportHandle, "active/test-campaign/runs/R-20260802-0099/report.md"},
	} {
		handle, err := workspace.handle(test.kind, stateTestDigest("8"))
		if err != nil {
			t.Fatal(err)
		}
		if handle.Path != test.want || handle.SHA256 != stateTestDigest("8") {
			t.Fatalf("canonical handle = %+v, want path %s", handle, test.want)
		}
	}
	if _, err := newCanonicalRunWorkspace("test-campaign", "not-a-run"); err == nil {
		t.Fatal("canonical run workspace accepted an invalid run id")
	}
	if _, err := workspace.path(runHandleKind(255)); err == nil {
		t.Fatal("canonical run workspace accepted an unknown handle kind")
	}
}

func TestManagerRunReportInputIsDigestOnlyAndEngineDerived(t *testing.T) {
	returned := stateTestReturnedRun(2, "returned")
	returned.Report = &FileHandle{SHA256: stateTestDigest("8")}
	request := ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: "test-campaign",
		CampaignID: returned.CampaignID, CorrelationID: "corr-test",
		Runs:      []RunRecord{returned},
		WorkItems: []WorkItemRecord{stateTestWorkItem(returned.PrimaryWorkItemID)},
	}
	derived, err := completeRunReportHandles(request)
	if err != nil {
		t.Fatal(err)
	}
	want := "active/test-campaign/runs/" + returned.ID + "/report.md"
	if derived.Runs[0].Report == nil || derived.Runs[0].Report.Path != want ||
		derived.Runs[0].Report.SHA256 != returned.Report.SHA256 {
		t.Fatalf("derived report handle = %+v, want %s with unchanged digest",
			derived.Runs[0].Report, want)
	}

	request.Runs[0].Report.Path = want
	err = validateManagerActionPayload(
		request, managerActionKinds["run.return"], Configuration{})
	if err == nil || !strings.Contains(err.Error(), "must not supply report.path") {
		t.Fatalf("manager-supplied report path was not refused: %v", err)
	}
}

func TestStateWriteRefusesNonCanonicalRunHandleBeforeSealing(t *testing.T) {
	run := stateTestReturnedRun(1, "returned")
	run.Report.Path = "active/other-campaign/runs/" + run.ID + "/report.md"
	_, err := prepareStateWrite(
		"test-campaign", run.CampaignID, run.CorrelationID, "E-TEST", 1,
		StateWrite{
			Path:   "active/test-campaign/runs/" + run.ID + "/run.json",
			Record: run,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "outside its canonical run workspace") {
		t.Fatalf("transaction boundary accepted a noncanonical report handle: %v", err)
	}
}

func TestManagerApplySchemaExposesDigestOnlyReportInput(t *testing.T) {
	var managerTool map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] == "manager_apply" {
			managerTool = tool
			break
		}
	}
	if managerTool == nil {
		t.Fatal("manager_apply tool is missing")
	}
	input := asObject(t, managerTool["inputSchema"])
	properties := asObject(t, input["properties"])
	runs := asObject(t, properties["runs"])
	run := asObject(t, runs["items"])
	runProperties := asObject(t, run["properties"])
	report := asObject(t, runProperties["report"])
	reportProperties := asObject(t, report["properties"])
	if _, present := reportProperties["path"]; present {
		t.Fatal("manager_apply report input still exposes caller-owned path")
	}
	if _, present := reportProperties["sha256"]; !present || len(reportProperties) != 1 {
		t.Fatalf("manager_apply report input is not digest-only: %#v", reportProperties)
	}
	required, ok := report["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "sha256" {
		t.Fatalf("manager_apply report input does not require only sha256: %#v", report["required"])
	}
}
