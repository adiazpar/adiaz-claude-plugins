package knowledge

import "testing"

// Closure must converge. Clearing an uncovered report requires a curator run,
// that run returns a report of its own, and if that report also demanded a
// reviewed intake the campaign would gain one uncovered report per lap forever.
// The normalization queue already knew curator reports need no intake; closure
// coverage did not, so it demanded intakes the queue would never offer items
// for and the only escape was hand-editing canonical state.
func TestCuratorReportDoesNotBlockClosure(t *testing.T) {
	graph := closureTestGraph(t)

	curatorMeta := closureTestMeta("R-20260802-0002")
	curatorReport := FileHandle{
		Path:   "active/test/runs/R-20260802-0002/report.md",
		SHA256: stateTestDigest("c"),
	}
	curatorAny, _, err := sealRunRecord(RunRecord{
		RecordMeta: curatorMeta, CampaignID: "C-TEST",
		PrimaryWorkItemID: "W-0001", ActorID: "knowledge-curator", Role: "curator",
		Status: "completed", StartedAt: closureTestTime, ReturnedAt: closureTestTime,
		ReviewedAt: closureTestTime, TerminalAt: closureTestTime,
		Brief: &FileHandle{
			Path:   "active/test/runs/R-20260802-0002/brief.md",
			SHA256: stateTestDigest("d"),
		},
		ContextPack: &FileHandle{
			Path:   "active/test/runs/R-20260802-0002/context-pack.json",
			SHA256: stateTestDigest("e"),
		},
		Report:        &curatorReport,
		ResultSummary: "Proposed a terminal disposition for every uncovered span.",
	})
	if err != nil {
		t.Fatal(err)
	}
	curator := curatorAny.(RunRecord)
	graph.Runs[curator.ID] = curator

	owner := graph.WorkItems["W-0001"]
	owner.Revision++
	owner.CompletedRunIDs = append(owner.CompletedRunIDs, curator.ID)
	ownerAny, _, err := sealWorkItemRecord(owner)
	if err != nil {
		t.Fatal(err)
	}
	graph.WorkItems["W-0001"] = ownerAny.(WorkItemRecord)

	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := coverage.SourceRunCoverage[curator.ID]; got != "not-a-claim-source" {
		t.Fatalf("curator run coverage = %q, want not-a-claim-source", got)
	}
	for _, missing := range coverage.MissingDecisions {
		if missing == "run:"+curator.ID+":coverage" {
			t.Fatal("a curator report was demanded as a claim source, so closure cannot converge")
		}
	}

	// The exemption must be narrow. Every other role still has to be curated,
	// including the manager-role run the shared fixture builds, which carries a
	// ratified truth finding of its own.
	if got := coverage.SourceRunCoverage["R-20260802-0001"]; got != "reviewed-intake" {
		t.Fatalf("investigator-class run coverage = %q, want reviewed-intake", got)
	}
}

// The rule has to hold in exactly one place. It previously lived only in the
// normalization queue, and closure coverage disagreed with it.
func TestClaimSourceRuleIsSharedByCoverageAndQueue(t *testing.T) {
	for _, role := range []string{"manager", "investigator", "reviewer"} {
		if !runIsClaimSource(RunRecord{Role: role}) {
			t.Errorf("role %q must remain a claim source", role)
		}
	}
	if runIsClaimSource(RunRecord{Role: "curator"}) {
		t.Error("a curator report records triage decisions, not claims")
	}
}
