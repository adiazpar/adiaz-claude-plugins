package knowledge

import (
	"strings"
	"testing"
)

const stateTestTime = "2026-08-02T18:00:00Z"

func stateTestDigest(fill string) string {
	return "sha256:" + strings.Repeat(fill, 64)
}

func stateTestMeta(id string, revision int64) RecordMeta {
	return RecordMeta{
		SchemaVersion: CampaignSchemaVersion,
		ID:            id,
		CreatedAt:     stateTestTime,
		UpdatedAt:     stateTestTime,
		Revision:      revision,
		CreatedBy:     "manager",
		UpdatedBy:     "manager",
		Digest:        stateTestDigest("0"),
		CorrelationID: "corr-test",
	}
}

func stateTestFileHandle(name string) *FileHandle {
	return &FileHandle{Path: "active/test-campaign/runs/R-20260802-0001/" + name, SHA256: stateTestDigest("1")}
}

func TestStateRecordsDelegatedRunRequiresBriefAndContextPack(t *testing.T) {
	run := RunRecord{
		RecordMeta: stateTestMeta("R-20260802-0001", 1),
		CampaignID: "C-TEST", PrimaryWorkItemID: "W-0001",
		ActorID: "drafter", Role: "investigator", Status: "prepared",
	}
	if err := ValidateRun(run); err == nil {
		t.Fatal("delegated run without immutable launch handles was accepted")
	}
	run.Brief = stateTestFileHandle("brief.md")
	run.ContextPack = stateTestFileHandle("context-pack.json")
	if err := ValidateRun(run); err != nil {
		t.Fatalf("valid delegated run rejected: %v", err)
	}
}

func TestStateRecordsDeferredWorkRequiresCompleteContract(t *testing.T) {
	item := WorkItemRecord{
		RecordMeta: stateTestMeta("W-0001", 1), CampaignID: "C-TEST",
		Kind: "task", Title: "Deferred work", Problem: "Wait for evidence",
		State: "deferred", Priority: "normal", Acceptance: []string{"Evidence arrives"}, Owner: "manager",
		Deferment: &DefermentContract{Reason: "Input unavailable", RevisitWhen: "Input is published"},
	}
	if err := ValidateWorkItem(item); err == nil {
		t.Fatal("deferment without an accountable owner was accepted")
	}
	item.Deferment.Owner = "manager"
	if err := ValidateWorkItem(item); err != nil {
		t.Fatalf("complete deferment contract rejected: %v", err)
	}
}

func TestStateRecordsReviewerCannotRatify(t *testing.T) {
	review := ReviewRecord{
		RecordMeta: stateTestMeta("V-0001", 1), CampaignID: "C-TEST",
		Reviewer: "independent-reviewer", Authority: "reviewer",
		IntakeID: "I-0001", IntakeRevision: 1, PacketDigest: stateTestDigest("9"),
		Decisions: []ReviewDecision{{
			FindingID: "F-0001", FindingRevision: 1,
			Action: "ratify", Rationale: "Looks correct",
		}},
	}
	if err := ValidateReview(review); err == nil {
		t.Fatal("reviewer authority was allowed to ratify a finding")
	}
	review.Decisions[0].Action = "challenge"
	if err := ValidateReview(review); err != nil {
		t.Fatalf("reviewer challenge was rejected: %v", err)
	}
}
