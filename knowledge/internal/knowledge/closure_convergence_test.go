package knowledge

import (
	"sort"
	"strings"
	"testing"
)

// Closure must converge, and convergence is a property of the whole gate rather
// than of any one rule in it.
//
// Every individual closure rule was correct when closure was nevertheless
// impossible to finish: coverage demanded a reviewed intake for an uncovered
// report, clearing that demanded a curator run, and the curator run returned a
// report that coverage then demanded an intake for. Each lap added one more
// blocker than it removed. Nothing in the suite could catch it, because no test
// asked the only question that exposes it -- does doing the work closure
// demands leave strictly less work than before?
//
// TestClosureConvergesAsBlockersAreCleared asks exactly that. It drives a
// blocked campaign toward closure one supported remedy at a time and requires,
// after every lap, that the blocker set be a strict subset of the previous
// one: nothing new may appear, and at least one thing must go. A rule that
// manufactures work while satisfying itself fails the subset half even though
// it may still pass the shrink half, which is why both are asserted separately
// and reported separately.
//
// This is deliberately written against closureCoverageBlockers over a campaign
// graph rather than against the closure state machine. The recursion lived in
// coverage computation, the blocker set is what a caller is actually handed,
// and a graph-level loop can carry the exact remedy for each blocker kind
// without also asserting the stage sequencing that closure_engine_test.go
// already owns.
//
// Two blocker families are therefore out of scope here and would need a
// service-level loop to cover: `active-file:` entries, which only exist once
// applyClosureActiveFileInventory has walked the active tree, and
// `normalization:` entries, which the engine unions in from the reconcile stage
// onward. A service-level loop would also have to account for run.return
// auto-creating a continuous-curation work item, which is a real per-lap
// addition rather than a defect -- worth knowing before extending this test,
// because a naive strict-subset assertion at that level would fail on it.

// blockerSet is the set a caller sees: missing decisions plus unresolved
// conflicts, which is what closureCoverageBlockers unions and what a closure
// job carries in Blockers.
func blockerSet(t *testing.T, graph CampaignGraph) map[string]bool {
	t.Helper()
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatalf("closure coverage could not be computed: %v", err)
	}
	set := map[string]bool{}
	for _, blocker := range closureCoverageBlockers(coverage) {
		set[blocker] = true
	}
	return set
}

func sortedBlockers(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for blocker := range set {
		out = append(out, blocker)
	}
	sort.Strings(out)
	return out
}

// blockedClosureConvergenceGraph takes the shared closure fixture, which is
// complete and therefore unblocked, and reintroduces one blocker of each kind
// a campaign realistically ends up holding: a run still in flight, a work item
// that never reached a terminal state, and a returned report whose curation was
// never reviewed (which also leaves its finding challenged and uncovered).
func blockedClosureConvergenceGraph(t *testing.T) CampaignGraph {
	t.Helper()
	graph := closureTestGraph(t)

	// 1. A returned report with no reviewed intake. This is the blocker whose
	//    remedy creates another run, and therefore the one the recursion hid in.
	intake := graph.Intakes["I-0001"]
	intake.Revision, intake.Status, intake.Digest = 1, "submitted", ""
	intakeAny, _, err := sealIntakeRecord(intake)
	if err != nil {
		t.Fatal(err)
	}
	graph.Intakes[intake.ID] = intakeAny.(IntakeRecord)
	delete(graph.Reviews, "V-0001")

	finding := graph.Findings["F-0001"]
	finding.ReviewState, finding.Validity, finding.Digest = "curator-checked", "challenged", ""
	finding.Digest, err = CanonicalDigest(finding)
	if err != nil {
		t.Fatal(err)
	}
	graph.Findings[finding.ID] = finding

	// 2. A work item that is not terminal.
	openWork, _, err := sealWorkItemRecord(WorkItemRecord{
		RecordMeta: closureTestMeta("W-0002"), CampaignID: "C-TEST", Kind: "task",
		Title: "Follow up", Problem: "A follow-up is outstanding", State: "active",
		Priority: "medium", Acceptance: []string{"resolved"}, Owner: "manager",
		ActiveRunIDs: []string{"R-20260802-0003"},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.WorkItems["W-0002"] = openWork.(WorkItemRecord)

	// 3. A run still in flight, which closure must not step over.
	inFlight, _, err := sealRunRecord(RunRecord{
		RecordMeta: closureTestMeta("R-20260802-0003"), CampaignID: "C-TEST",
		PrimaryWorkItemID: "W-0002", ActorID: "drafter", Role: "investigator",
		Status: "running", StartedAt: closureTestTime,
		Brief: &FileHandle{
			Path:   "active/test/runs/R-20260802-0003/brief.md",
			SHA256: stateTestDigest("f"),
		},
		ContextPack: &FileHandle{
			Path:   "active/test/runs/R-20260802-0003/context-pack.json",
			SHA256: stateTestDigest("1"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.Runs["R-20260802-0003"] = inFlight.(RunRecord)

	return graph
}

func TestClosureConvergesAsBlockersAreCleared(t *testing.T) {
	graph := blockedClosureConvergenceGraph(t)

	initial := blockerSet(t, graph)
	// Pin what the fixture actually blocks on, so a change that stops producing
	// one of these kinds is a visible edit here rather than a silently weaker
	// test that still passes because it now converges from nothing.
	for _, want := range []string{
		"run:R-20260802-0003:report",
		"run:R-20260802-0001:coverage",
		"work:W-0002",
		"finding:F-0001",
		"finding:F-0001:challenged",
	} {
		if !initial[want] {
			t.Fatalf("fixture does not block on %s; it blocks on %v", want, sortedBlockers(initial))
		}
	}

	laps := []struct {
		name   string
		clears []string
		remedy func(t *testing.T, graph *CampaignGraph)
	}{
		{
			name:   "end a run that will never return",
			clears: []string{"run:R-20260802-0003:report"},
			remedy: closureConvergenceAbortInFlightRun,
		},
		{
			name:   "take the outstanding work item to a terminal state",
			clears: []string{"work:W-0002"},
			remedy: closureConvergenceFinishWorkItem,
		},
		{
			// The lap that used to be impossible. Curating an uncovered report
			// means dispatching a curator, and that curator run returns a
			// report of its own. If a curator report is ever treated as a claim
			// source again, this lap adds run:R-20260802-0002:coverage while
			// removing the three below, and the subset assertion fails.
			name: "curate and review the uncovered report",
			clears: []string{
				"run:R-20260802-0001:coverage", "finding:F-0001", "finding:F-0001:challenged",
			},
			remedy: closureConvergenceCurateAndReview,
		},
	}

	previous := initial
	for _, lap := range laps {
		lap.remedy(t, &graph)
		current := blockerSet(t, graph)

		appeared := []string{}
		for blocker := range current {
			if !previous[blocker] {
				appeared = append(appeared, blocker)
			}
		}
		sort.Strings(appeared)
		if len(appeared) > 0 {
			t.Fatalf(
				"closure is non-convergent: %q cleared %v but introduced %v. "+
					"Satisfying a closure gate must never create work that trips a closure gate.",
				lap.name, lap.clears, appeared)
		}
		if len(current) >= len(previous) {
			t.Fatalf("%q left the blocker set at %d (was %d): %v",
				lap.name, len(current), len(previous), sortedBlockers(current))
		}
		for _, cleared := range lap.clears {
			if current[cleared] {
				t.Fatalf("%q did not clear %s; still blocked on %v",
					lap.name, cleared, sortedBlockers(current))
			}
		}
		previous = current
	}

	if len(previous) != 0 {
		t.Fatalf("the campaign never reached an unblocked state: %v", sortedBlockers(previous))
	}
}

// closureConvergenceAbortInFlightRun ends a run that will never come back. This
// is run.complete with an aborted status: ValidateRun requires no report from
// `aborted`, which is the whole reason the state exists.
func closureConvergenceAbortInFlightRun(t *testing.T, graph *CampaignGraph) {
	t.Helper()
	run := graph.Runs["R-20260802-0003"]
	run.Revision++
	run.Status, run.TerminalAt, run.Digest = "aborted", closureTestTime, ""
	run.ResultSummary = "Abandoned; the follow-up was answered without it."
	sealed, _, err := sealRunRecord(run)
	if err != nil {
		t.Fatal(err)
	}
	graph.Runs[run.ID] = sealed.(RunRecord)

	// The run is terminal now, so its owning work item may no longer carry it
	// as active. run.complete carries this work-item update in the same
	// transaction for exactly this reason.
	owner := graph.WorkItems["W-0002"]
	owner.Revision++
	owner.ActiveRunIDs = nil
	owner.CompletedRunIDs = append(owner.CompletedRunIDs, run.ID)
	owner.Digest = ""
	ownerAny, _, err := sealWorkItemRecord(owner)
	if err != nil {
		t.Fatal(err)
	}
	graph.WorkItems[owner.ID] = ownerAny.(WorkItemRecord)
}

// closureConvergenceFinishWorkItem is work.update to a terminal state.
func closureConvergenceFinishWorkItem(t *testing.T, graph *CampaignGraph) {
	t.Helper()
	item := graph.WorkItems["W-0002"]
	item.Revision++
	item.State, item.Outcome, item.Digest = "done", "Answered by the completed run.", ""
	sealed, _, err := sealWorkItemRecord(item)
	if err != nil {
		t.Fatal(err)
	}
	graph.WorkItems[item.ID] = sealed.(WorkItemRecord)
}

// closureConvergenceCurateAndReview performs the full remedy for an uncovered
// report: a curator run is dispatched and returns its own report, that curator
// submits an intake covering the source span, and a manager review ratifies the
// candidate finding.
//
// Every record this lap adds is deliberate. The curator run in particular is
// not incidental -- it is the artifact whose existence made closure recursive,
// so a convergence test that skipped it would pass under the original defect.
func closureConvergenceCurateAndReview(t *testing.T, graph *CampaignGraph) {
	t.Helper()
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
		ResultSummary: "Normalized the uncovered report span by span.",
	})
	if err != nil {
		t.Fatal(err)
	}
	curator := curatorAny.(RunRecord)
	graph.Runs[curator.ID] = curator

	owner := graph.WorkItems["W-0001"]
	owner.Revision++
	owner.CompletedRunIDs = append(owner.CompletedRunIDs, curator.ID)
	owner.Digest = ""
	ownerAny, _, err := sealWorkItemRecord(owner)
	if err != nil {
		t.Fatal(err)
	}
	graph.WorkItems[owner.ID] = ownerAny.(WorkItemRecord)

	reportPath := "active/test/runs/R-20260802-0001/report.md"
	reportDigest := "sha256:" + strings.Repeat("a", 64)
	intakeMeta := closureTestMeta("I-0001")
	intakeMeta.Revision = 2
	intakeAny, _, err := sealIntakeRecord(IntakeRecord{
		RecordMeta: intakeMeta, CampaignID: "C-TEST",
		SourceRuns:          []FileHandle{{Path: reportPath, SHA256: reportDigest}},
		CandidateFindingIDs: []string{"F-0001"},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + reportPath + "#L1-L2", SourcePath: reportPath,
			SourceSHA256: reportDigest, StartLine: 1, EndLine: 2, SourceLineCount: 2,
			Disposition: "candidate-finding", TargetID: "F-0001",
		}},
		Triage: map[string]string{"F-0001": "routine"},
		Status: "reviewed",
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.Intakes["I-0001"] = intakeAny.(IntakeRecord)

	reviewAny, _, err := sealReviewRecord(ReviewRecord{
		RecordMeta: closureTestMeta("V-0001"), CampaignID: "C-TEST",
		Reviewer: "manager", Authority: "manager", IntakeID: "I-0001", IntakeRevision: 1,
		PacketDigest: stateTestDigest("9"),
		ReviewLoad:   stateTestReviewLoad("V-0001", "C-TEST", stateTestDigest("9"), 1, 0),
		Decisions: []ReviewDecision{{
			FindingID: "F-0001", FindingRevision: 1, Action: "ratify",
			Rationale: "Direct evidence resolves it",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.Reviews["V-0001"] = reviewAny.(ReviewRecord)

	finding := graph.Findings["F-0001"]
	finding.ReviewState, finding.Validity, finding.Projection = "manager-ratified", "current", "truth"
	finding.Digest = ""
	finding.Digest, err = CanonicalDigest(finding)
	if err != nil {
		t.Fatal(err)
	}
	graph.Findings[finding.ID] = finding
}
