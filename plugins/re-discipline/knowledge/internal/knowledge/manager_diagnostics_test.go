package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManagerDecisionRecordWithoutRecordsNamesItsContract pins the diagnostic
// for a manager decision submitted with a rationale and nothing else.
//
// decision.record is deliberately the same review transaction as
// review.submit: the engine has no standalone decision record, and every
// transition already journals its rationale in the state event. The refusal
// must say that, and must name the record kinds the action accepts, instead of
// the bare "manager mutation contains no typed records" that made the action
// look broken.
func TestManagerDecisionRecordWithoutRecordsNamesItsContract(t *testing.T) {
	root := makeAdversarialProject(t)
	_, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	_, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "decision.record", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-bare-decision", IdempotencyKey: "idem-bare-decision",
		Rationale:            "The manager decided to keep the current approach.",
		ExpectedHeadRevision: opening.ResultingHead.Revision,
		ExpectedHeadDigest:   opening.ResultingHead.Digest,
	})
	if err == nil {
		t.Fatal("a decision with no typed record was accepted")
	}
	message := err.Error()
	for _, want := range []string{
		"decision.record",
		"finding, intake, review, and work records",
		"same review transaction as review.submit",
		"standalone rationale is not a record",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal does not explain %q: %s", want, message)
		}
	}
}

// TestManagerRecordlessActionsNameTheirAcceptedKinds keeps every typed action
// honest about what it needs, not just the one the manager tripped over.
func TestManagerRecordlessActionsNameTheirAcceptedKinds(t *testing.T) {
	root := makeAdversarialProject(t)
	_, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	for action, want := range map[string]string{
		"work.create":       "at least one new work item",
		"finding.challenge": "validity is challenged",
		"run.start":         "running run and its primary work item",
	} {
		_, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
			Action: action, Actor: "manager",
			CampaignSlug: "test-campaign", CampaignID: "C-TEST",
			CorrelationID: "corr-empty", IdempotencyKey: "idem-empty-" + action,
			ExpectedHeadRevision: opening.ResultingHead.Revision,
			ExpectedHeadDigest:   opening.ResultingHead.Digest,
		})
		if err == nil {
			t.Fatalf("%s accepted an empty payload", action)
		}
		if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), action) {
			t.Fatalf("%s refusal is not actionable: %v", action, err)
		}
	}
}

// TestUnregisteredRunDirectoryReportsPathAndRemedy pins the fix for a bare
// platform error. Writing report files under a run ID that was never published
// froze every mutation and every state view with the operating system's
// "cannot find the file specified" text, which named neither the path nor the
// cause.
func TestUnregisteredRunDirectoryReportsPathAndRemedy(t *testing.T) {
	root := makeAdversarialProject(t)
	store, _ := openContextPackTestCampaign(t, root)

	strayReport := filepath.Join(
		root, "active", "test-campaign", "runs", "R-20260802-0777", "report.md")
	writeTestFile(t, strayReport, "# Report written without a registered run\n")

	_, err := store.LoadCampaignGraph("C-TEST")
	if err == nil {
		t.Fatal("an unregistered run directory was accepted as canonical state")
	}
	message := err.Error()
	for _, want := range []string{
		"unregistered run directory",
		"active/test-campaign/runs/R-20260802-0777",
		"run.json",
		"manager_apply run.prepare",
		"remove that directory",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal does not report %q: %s", want, message)
		}
	}
	if strings.Contains(message, "cannot find the file") {
		t.Fatalf("refusal still leaks a bare platform error: %s", message)
	}

	// Removing the stray directory must fully clear the refusal.
	if err := os.RemoveAll(filepath.Dir(strayReport)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadCampaignGraph("C-TEST"); err != nil {
		t.Fatalf("documented remedy did not clear the refusal: %v", err)
	}
}

// TestMissingCanonicalRecordNamesItsPath covers the general case behind the
// bare platform error: any unreadable canonical record must name itself.
func TestMissingCanonicalRecordNamesItsPath(t *testing.T) {
	root := makeAdversarialProject(t)
	store, _ := openContextPackTestCampaign(t, root)

	_, _, err := store.ReadCanonicalRecord("active/test-campaign/work-items/W-9999.json")
	if err == nil {
		t.Fatal("a missing canonical record read succeeded")
	}
	if !strings.Contains(err.Error(), "active/test-campaign/work-items/W-9999.json") {
		t.Fatalf("missing record error does not name its path: %v", err)
	}
}

// TestDirtyCanonicalStateClassifiesEachPath pins the second half of the same
// defect: the mutation refusal named the path but never said whether it was an
// out-of-band edit or an unexpected addition, nor what would clear it.
func TestDirtyCanonicalStateClassifiesEachPath(t *testing.T) {
	root := makeAdversarialProject(t)
	_, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)

	// A hand-edited tracked document, exactly the observed docs/truth/INDEX.md case.
	writeTestFile(t, filepath.Join(root, "docs", "truth", "INDEX.md"),
		"# Truth index\n\n- Hand written entry\n")
	// An unexpected new file in the same canonical location.
	writeTestFile(t, filepath.Join(root, "docs", "truth", "stray-note.md"), "# Stray note\n")

	work := stateTestWorkItem("W-0002")
	work.CorrelationID, work.Digest = "corr-dirty", ""
	_, err := service.ManagerApply(context.Background(), ManagerApplyRequest{
		Action: "work.create", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-dirty", IdempotencyKey: "idem-dirty",
		ExpectedHeadRevision: opening.ResultingHead.Revision,
		ExpectedHeadDigest:   opening.ResultingHead.Digest,
		WorkItems:            []WorkItemRecord{work},
	})
	if err == nil {
		t.Fatal("a mutation over dirty canonical state was accepted")
	}
	message := err.Error()
	for _, want := range []string{
		"canonical state does not match its committed head",
		"docs/truth/INDEX.md (tracked canonical record was edited outside the workflow",
		"restore its committed bytes",
		"docs/truth/stray-note.md (unexpected file in a canonical state location",
		"remove it, or publish it through the transition that owns it",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("dirty-state refusal does not report %q: %s", want, message)
		}
	}
}

// TestMissingTrackedRecordIsClassifiedSeparately keeps the third drift class
// distinguishable from an out-of-band edit.
func TestMissingTrackedRecordIsClassifiedSeparately(t *testing.T) {
	root := makeAdversarialProject(t)
	store, _ := openContextPackTestCampaign(t, root)

	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := store.loadCommittedInventory(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(
		root, "active", "test-campaign", "work-items", "W-0001.json")); err != nil {
		t.Fatal(err)
	}
	drift, err := store.inventoryDrift(inventory)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range drift {
		if entry.Path == "active/test-campaign/work-items/W-0001.json" {
			found = true
			if entry.Reason != stateDriftMissing {
				t.Fatalf("removed record classified as %q", entry.Reason)
			}
			if !strings.Contains(entry.describe(), "restore its committed bytes") {
				t.Fatalf("missing-record description has no remedy: %s", entry.describe())
			}
		}
	}
	if !found {
		t.Fatalf("removed tracked record was not reported as drift: %+v", drift)
	}
}
