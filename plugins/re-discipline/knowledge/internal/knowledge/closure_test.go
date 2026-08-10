package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const closureTestTime = "2026-08-02T20:00:00Z"

func closureTestMeta(id string) RecordMeta {
	return RecordMeta{
		SchemaVersion: CampaignSchemaVersion, ID: id,
		CreatedAt: closureTestTime, UpdatedAt: closureTestTime, Revision: 1,
		CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "closure-test",
	}
}

func closureTestGraph(t *testing.T) CampaignGraph {
	t.Helper()
	reportPath := "active/test/runs/R-20260802-0001/report.md"
	reportDigest := "sha256:" + strings.Repeat("a", 64)
	campaignAny, _, err := sealCampaignRecord(CampaignRecord{
		RecordMeta: closureTestMeta("C-TEST"), Title: "Test campaign", Slug: "test",
		Objective: "Exercise closure", Scope: []string{"plugin"},
		SuccessCriteria: []string{"implementation complete"},
		ClosureCriteria: []string{"records accounted for"}, Status: "closing",
		Owner: "manager", PermittedManagers: []string{"manager"},
		OpenedAt: closureTestTime, ClosingAt: closureTestTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	workAny, _, err := sealWorkItemRecord(WorkItemRecord{
		RecordMeta: closureTestMeta("W-0001"), CampaignID: "C-TEST", Kind: "task",
		Title: "Implement", Problem: "Implementation is needed", State: "done",
		Priority: "high", Acceptance: []string{"done"}, Owner: "manager",
		CompletedRunIDs: []string{"R-20260802-0001"}, FindingIDs: []string{"F-0001"},
		Outcome: "Implemented and verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	runAny, _, err := sealRunRecord(RunRecord{
		RecordMeta: closureTestMeta("R-20260802-0001"), CampaignID: "C-TEST",
		PrimaryWorkItemID: "W-0001", ActorID: "manager", Role: "manager",
		Status: "completed", StartedAt: closureTestTime, ReturnedAt: closureTestTime,
		ReviewedAt: closureTestTime, TerminalAt: closureTestTime,
		Report:     &FileHandle{Path: reportPath, SHA256: reportDigest},
		FindingIDs: []string{"F-0001"}, ResultSummary: "Complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	finding := FindingRecord{
		SchemaVersion: CampaignSchemaVersion, ID: "F-0001", CampaignID: "C-TEST",
		Revision: 2, CreatedAt: closureTestTime, UpdatedAt: closureTestTime,
		CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "closure-test",
		Kind: "conclusion", Subject: "plugin", Claim: "The implementation is complete.",
		Scope: map[string]any{"component": "plugin"}, SourceRuns: []string{"R-20260802-0001"},
		Evidence: []EvidenceReference{{
			Path: reportPath, SHA256: reportDigest, StartLine: 1, EndLine: 2,
			ObjectKey: "path:" + reportPath + "#L1-L2", SourceRun: "R-20260802-0001",
		}},
		EvidenceGrade: "direct", ReviewState: "manager-ratified", Validity: "current",
		Projection: "truth",
	}
	finding.Digest, err = CanonicalDigest(finding)
	if err != nil {
		t.Fatal(err)
	}
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
	reviewAny, _, err := sealReviewRecord(ReviewRecord{
		RecordMeta: closureTestMeta("V-0001"), CampaignID: "C-TEST",
		Reviewer: "manager", Authority: "manager", IntakeID: "I-0001", IntakeRevision: 1,
		PacketDigest: stateTestDigest("9"),
		ReviewLoad:   stateTestReviewLoad("V-0001", "C-TEST", stateTestDigest("9"), 1, 0),
		Decisions: []ReviewDecision{{
			FindingID: "F-0001", FindingRevision: 1, Action: "ratify", Rationale: "Direct evidence resolves it",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign := campaignAny.(CampaignRecord)
	work := workAny.(WorkItemRecord)
	run := runAny.(RunRecord)
	intake := intakeAny.(IntakeRecord)
	review := reviewAny.(ReviewRecord)
	return CampaignGraph{
		Campaign:  &campaign,
		WorkItems: map[string]WorkItemRecord{work.ID: work},
		Runs:      map[string]RunRecord{run.ID: run},
		Findings:  map[string]FindingRecord{finding.ID: finding},
		Intakes:   map[string]IntakeRecord{intake.ID: intake},
		Reviews:   map[string]ReviewRecord{review.ID: review},
	}
}

func TestClosurePlanAndCoverageAreCompleteAndDigestBound(t *testing.T) {
	graph := closureTestGraph(t)
	plan, err := BuildClosurePlan(graph, "docs/history/campaigns/2026-08-02-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.RequiredRunIDs) != 1 || len(plan.ProjectionFindingIDs) != 1 {
		t.Fatalf("closure plan omitted records: %#v", plan)
	}
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.MissingDecisions) != 0 || len(coverage.UnresolvedConflicts) != 0 {
		t.Fatalf("complete graph was blocked: %#v", coverage)
	}
	if coverage.SourceRunCoverage["R-20260802-0001"] != "reviewed-intake" ||
		coverage.FindingCoverage["F-0001"] != "truth" {
		t.Fatalf("closure coverage dispositions are wrong: %#v", coverage)
	}
	tampered := coverage
	tampered.FindingCoverage = map[string]string{"F-0001": "history"}
	if err := ValidateClosureCoverage(tampered); err == nil {
		t.Fatal("tampered coverage retained a valid digest")
	}
}

// The fixture above hands coverage a finding that is already current, which is a
// state no campaign can actually be in before it closes: only a closure action
// may make a finding current, and the only one that does is the project stage.
// So the realistic input is the provisional one, and coverage has to pass it
// through. A gate demanding "current" here deadlocks every campaign that has a
// truth-bound finding - it asks for the state that clearing the gate produces.
func TestClosureCoverageAcceptsAProvisionalTruthCandidate(t *testing.T) {
	graph := closureTestGraph(t)
	finding := graph.Findings["F-0001"]
	finding.Validity, finding.Digest = "provisional", ""
	finding.Digest, _ = CanonicalDigest(finding)
	graph.Findings[finding.ID] = finding
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.FindingCoverage["F-0001"] != "truth" {
		t.Fatalf("provisional truth candidate was gated as %q", coverage.FindingCoverage["F-0001"])
	}
	if len(coverage.MissingDecisions) != 0 {
		t.Fatalf("provisional truth candidate blocked closure: %#v", coverage.MissingDecisions)
	}
}

// The other half of the same rule: widening the gate to provisional must not
// wave through a validity that truth promotion would later refuse, or coverage
// would report a clean campaign and the project stage would fail the archive.
func TestClosureCoverageStillGatesAnUnpromotableTruthCandidate(t *testing.T) {
	// "superseded" is left out deliberately: the graph refuses one without a
	// replacement pointing back, so it cannot reach this gate unaccompanied.
	for _, validity := range []string{"challenged", "historical"} {
		graph := closureTestGraph(t)
		finding := graph.Findings["F-0001"]
		finding.Validity, finding.Digest = validity, ""
		finding.Digest, _ = CanonicalDigest(finding)
		graph.Findings[finding.ID] = finding
		coverage, err := ComputeClosureCoverage(graph, nil)
		if err != nil {
			t.Fatal(err)
		}
		if coverage.FindingCoverage["F-0001"] != "truth-gate-failed" {
			t.Fatalf("%s truth candidate was accepted as %q",
				validity, coverage.FindingCoverage["F-0001"])
		}
	}
}

// Closure's project stage promotes a truth candidate to current and advances its
// revision, which moves a ratified finding one step further from the review that
// ratified it. The receipt does not stop existing because the record it ratified
// moved forward, so the graph has to keep validating - otherwise the engine's own
// committed output fails its next load, mid-archive.
func TestReviewReceiptSurvivesTheRevisionClosurePromotionAdds(t *testing.T) {
	graph := closureTestGraph(t)
	promoted := graph.Findings["F-0001"]
	promoted.Revision++ // what prepareClosureFindingTransitions writes
	promoted.Validity, promoted.Digest = "current", ""
	promoted.Digest, _ = CanonicalDigest(promoted)
	graph.Findings[promoted.ID] = promoted
	if err := graph.Validate(); err != nil {
		t.Fatalf("review receipt stopped binding after truth promotion: %v", err)
	}

	// The invariant that remains: a review cannot cite a revision that never
	// existed, because it could not have read one.
	ahead := graph.Reviews["V-0001"]
	ahead.Decisions = []ReviewDecision{{
		FindingID: "F-0001", FindingRevision: promoted.Revision + 1,
		Action: "ratify", Rationale: "Direct evidence resolves it",
	}}
	ahead.Digest = ""
	ahead.Digest, _ = CanonicalDigest(ahead)
	graph.Reviews[ahead.ID] = ahead
	if err := graph.Validate(); err == nil ||
		!strings.Contains(err.Error(), "which the finding never reached") {
		t.Fatalf("review bound a revision ahead of the finding: %v", err)
	}
}

func TestClosureCoverageSurfacesMissingReviewAndConflict(t *testing.T) {
	graph := closureTestGraph(t)
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
	finding.Digest, _ = CanonicalDigest(finding)
	graph.Findings[finding.ID] = finding
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(coverage.MissingDecisions, "run:R-20260802-0001:coverage") ||
		!containsString(coverage.UnresolvedConflicts, "finding:F-0001:challenged") {
		t.Fatalf("closure did not surface blockers: %#v", coverage)
	}
}

func TestReviewedRunCoverageIsBoundToTheExactReportDigest(t *testing.T) {
	graph := closureTestGraph(t)
	run := graph.Runs["R-20260802-0001"]
	staleReport := *run.Report
	staleReport.SHA256 = stateTestDigest("7")
	run.Report = &staleReport
	graph.Runs[run.ID] = run

	reviewed := reviewedReportCoverage(graph)
	if reviewed[reviewedRunCoverageKey(run.ID, staleReport)] {
		t.Fatal("review of prior report bytes covered a same-path report with a new digest")
	}
}

func TestReviewedRunCoverageRequiresAnImmutableReviewReceipt(t *testing.T) {
	graph := closureTestGraph(t)
	delete(graph.Reviews, "V-0001")
	run := graph.Runs["R-20260802-0001"]
	if reviewedReportCoverage(graph)[reviewedRunCoverageKey(run.ID, *run.Report)] {
		t.Fatal("reviewed intake status without a review receipt covered its source run")
	}
}

func TestReviewedRunCoverageRequiresAReviewDecisionForEveryCandidate(t *testing.T) {
	graph := closureTestGraph(t)
	review := graph.Reviews["V-0001"]
	review.Decisions = nil
	graph.Reviews[review.ID] = review
	run := graph.Runs["R-20260802-0001"]
	if reviewedReportCoverage(graph)[reviewedRunCoverageKey(run.ID, *run.Report)] {
		t.Fatal("partial manager review covered a source run")
	}
}

func TestClosureAdvanceCannotSkipOrProjectThroughBlockers(t *testing.T) {
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "reconcile", Status: "running", FrozenCampaignRevision: 1,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{},
	}
	baseAny := base
	baseAny.Digest = ""
	base.Digest, _ = CanonicalDigest(baseAny)
	next := base
	next.Revision = 2
	next.Stage = "decide"
	next.UpdatedAt = "2026-08-02T20:01:00Z"
	nextAny := next
	nextAny.Digest = ""
	next.Digest, _ = CanonicalDigest(nextAny)
	if err := ValidateClosureAdvance(base, next); err != nil {
		t.Fatalf("valid single closure edge failed: %v", err)
	}
	skipped := next
	skipped.Stage = "verify"
	if err := ValidateClosureAdvance(base, skipped); err == nil {
		t.Fatal("closure skipped stages")
	}
	blockedCoverage := coverage
	blockedCoverage.MissingDecisions = []string{"finding:F-0001"}
	if err := sealClosureCoverage(&blockedCoverage); err != nil {
		t.Fatal(err)
	}
	project := next
	project.Stage = "project"
	project.Coverage = &blockedCoverage
	project.StagingDigest = stateTestDigest("8")
	projectAny := project
	projectAny.Digest = ""
	project.Digest, _ = CanonicalDigest(projectAny)
	if err := ValidateClosureAdvance(next, project); err == nil {
		t.Fatal("closure projected through an unresolved decision")
	}
}

func TestTruthProjectionVerifiesEvidenceAndArchiveManifestCoverage(t *testing.T) {
	graph := closureTestGraph(t)
	root := t.TempDir()
	reportPath := graph.Runs["R-20260802-0001"].Report.Path
	absolute := filepath.Join(root, filepath.FromSlash(reportPath))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	reportBody := []byte("direct evidence\ncomplete\n")
	if err := os.WriteFile(absolute, reportBody, 0o600); err != nil {
		t.Fatal(err)
	}
	reportDigest := "sha256:" + SHA256Bytes(reportBody)
	run := graph.Runs["R-20260802-0001"]
	runReport := *run.Report
	runReport.SHA256 = reportDigest
	run.Report = &runReport
	graph.Runs[run.ID] = run
	intake := graph.Intakes["I-0001"]
	intake.SourceRuns = []FileHandle{runReport}
	intake.Coverage[0].SourceSHA256 = reportDigest
	graph.Intakes[intake.ID] = intake
	finding := graph.Findings["F-0001"]
	finding.Evidence[0].SHA256 = reportDigest
	finding.Digest, _ = CanonicalDigest(finding)
	graph.Findings[finding.ID] = finding
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := BuildTruthProjection(boundary, finding, "docs/truth/plugin-complete.md")
	if err != nil {
		t.Fatal(err)
	}
	if !digestRE.MatchString(projection.ContentDigest) || !strings.Contains(string(projection.Body), "sourceFinding: \"F-0001\"") {
		t.Fatalf("truth projection is incomplete: %#v", projection)
	}

	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	job := ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "archive", Status: "verified", FrozenCampaignRevision: 1,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{"F-0001": projection.ContentDigest},
		ArchiveDigest:      "sha256:" + strings.Repeat("b", 64),
	}
	jobCopy := job
	jobCopy.Digest = ""
	job.Digest, _ = CanonicalDigest(jobCopy)
	files := map[string]string{}
	for _, name := range requiredArchiveRecordPaths(graph) {
		files[name] = "sha256:" + strings.Repeat("c", 64)
	}
	manifest, err := BuildArchiveManifest(
		graph, job, "E-20260802-200000-ABCDEF", closureTestTime, files,
		map[string]string{projection.Destination: projection.ContentDigest},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateArchiveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	delete(files, "campaign.json")
	if _, err := BuildArchiveManifest(
		graph, job, "E-20260802-200000-ABCDEF", closureTestTime, files,
		map[string]string{projection.Destination: projection.ContentDigest},
	); err == nil {
		t.Fatal("archive manifest accepted a missing campaign record")
	}
}

func TestTruthProjectionRejectsChangedEvidence(t *testing.T) {
	graph := closureTestGraph(t)
	root := t.TempDir()
	reportPath := graph.Runs["R-20260802-0001"].Report.Path
	absolute := filepath.Join(root, filepath.FromSlash(reportPath))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTruthProjection(boundary, graph.Findings["F-0001"], "docs/truth/plugin-complete.md"); err == nil {
		t.Fatal("truth projection accepted evidence whose digest changed")
	}
}

// sealClosureRestartTestJob seals a job's digest without validating it, so a
// table case can present a deliberately wrong job to ValidateClosureRestart and
// watch that rule refuse it, rather than tripping over a stale digest first.
func sealClosureRestartTestJob(t *testing.T, job ClosureJob) ClosureJob {
	t.Helper()
	job.Digest = ""
	digest, err := CanonicalDigest(job)
	if err != nil {
		t.Fatal(err)
	}
	job.Digest = digest
	return job
}

func closureRestartTestJobs(t *testing.T) (ClosureJob, ClosureJob) {
	t.Helper()
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "decide", Status: "reopened", FrozenCampaignRevision: 4,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{}, ProjectionDigests: map[string]string{},
	}
	next := previous
	next.Revision = 2
	next.UpdatedAt = "2026-08-02T20:01:00Z"
	next.Stage, next.Status = "inventory", "running"
	next.FrozenCampaignRevision = 7
	next.Attempt = 2
	return previous, next
}

// Restart is the only rule in the engine permitted to move a closure freeze, so
// it is also the only place a mistake can silently rebind a campaign onto a
// different view of itself. Enumerate the refusals rather than trusting a single
// happy path.
func TestClosureRestartRequiresAReopenedJobAndAForwardFreeze(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, previous, next *ClosureJob)
		refused bool
	}{
		{name: "well-formed restart"},
		{name: "a running attempt cannot be restarted", refused: true,
			mutate: func(_ *testing.T, previous, _ *ClosureJob) { previous.Status = "running" }},
		{name: "a blocked attempt cannot be restarted", refused: true,
			mutate: func(_ *testing.T, previous, _ *ClosureJob) { previous.Status = "blocked" }},
		{name: "a verified attempt cannot be restarted", refused: true,
			mutate: func(_ *testing.T, previous, _ *ClosureJob) { previous.Status = "verified" }},
		{name: "a completed attempt cannot be restarted", refused: true,
			mutate: func(_ *testing.T, previous, _ *ClosureJob) {
				previous.Stage, previous.Status = "finalize", "completed"
			}},
		{name: "an unchanged freeze is not a re-plan", refused: true,
			mutate: func(_ *testing.T, previous, next *ClosureJob) {
				next.FrozenCampaignRevision = previous.FrozenCampaignRevision
			}},
		{name: "a freeze cannot rewind", refused: true,
			mutate: func(_ *testing.T, previous, next *ClosureJob) {
				next.FrozenCampaignRevision = previous.FrozenCampaignRevision - 1
			}},
		{name: "an unrecorded attempt is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.Attempt = 0 }},
		{name: "skipping an attempt is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.Attempt = 3 }},
		{name: "the archive destination is pinned", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) {
				next.ArchiveDestination = "docs/history/campaigns/2026-08-02-other"
			}},
		{name: "the closure job id is pinned", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.ID = "closure-other-job" }},
		{name: "the campaign id is pinned", refused: true,
			mutate: func(t *testing.T, _, next *ClosureJob) {
				other := cloneClosureCoverage(*next.Coverage)
				other.CampaignID = "C-OTHER"
				if err := sealClosureCoverage(&other); err != nil {
					t.Fatal(err)
				}
				next.CampaignID, next.Coverage = "C-OTHER", &other
			}},
		{name: "residual staging proof is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.StagingDigest = stateTestDigest("1") }},
		{name: "residual archive proof is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.ArchiveDigest = stateTestDigest("2") }},
		{name: "residual truth proof is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) {
				next.TruthDigests = map[string]string{"F-0001": stateTestDigest("3")}
			}},
		{name: "residual projection proof is refused", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) {
				next.ProjectionDigests = map[string]string{"docs/truth/example.md": stateTestDigest("4")}
			}},
		{name: "restart must re-enter at inventory", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.Stage = "coverage" }},
		{name: "restart must re-enter running, not blocked", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.Status = "blocked" }},
		{name: "restart requires recomputed coverage", refused: true,
			mutate: func(_ *testing.T, _, next *ClosureJob) { next.Coverage = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous, next := closureRestartTestJobs(t)
			if test.mutate != nil {
				test.mutate(t, &previous, &next)
			}
			err := ValidateClosureRestart(
				sealClosureRestartTestJob(t, previous), sealClosureRestartTestJob(t, next))
			if test.refused && err == nil {
				t.Fatal("closure restart accepted a job pair it must refuse")
			}
			if !test.refused && err != nil {
				t.Fatalf("closure restart refused a well-formed re-entry: %v", err)
			}
		})
	}
}

// The attempt counter is what tells a reader which plan a job is gated on. If an
// ordinary stage edge could move it, a restart would stop being distinguishable
// from a resumption both in the record and in the archived event journal.
func TestClosureAdvancePinsTheFreezeAndTheAttempt(t *testing.T) {
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := sealClosureRestartTestJob(t, ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "reconcile", Status: "running", FrozenCampaignRevision: 4,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{},
	})
	next := previous
	next.Revision, next.UpdatedAt, next.Stage = 2, "2026-08-02T20:01:00Z", "decide"
	if err := ValidateClosureAdvance(previous, sealClosureRestartTestJob(t, next)); err != nil {
		t.Fatalf("ordinary closure edge failed: %v", err)
	}
	moved := next
	moved.Attempt = 2
	if err := ValidateClosureAdvance(previous, sealClosureRestartTestJob(t, moved)); err == nil {
		t.Fatal("an ordinary closure edge moved the attempt counter")
	}
	rebound := next
	rebound.FrozenCampaignRevision = 5
	if err := ValidateClosureAdvance(previous, sealClosureRestartTestJob(t, rebound)); err == nil {
		t.Fatal("an ordinary closure edge moved the campaign freeze")
	}
}

// Every closure job committed before restart existed carries no attempt field.
// readCanonicalRecordValue re-encodes what it reads and rejects it when the
// bytes differ, so if the new field ever encoded as an explicit zero those
// records would stop verifying and every in-flight closure would be bricked.
func TestClosureAttemptTreatsAnAbsentCounterAsTheFirstAttempt(t *testing.T) {
	if closureAttempt(ClosureJob{}) != 1 || closureAttempt(ClosureJob{Attempt: 0}) != 1 {
		t.Fatal("an absent attempt counter was not read as the first attempt")
	}
	if closureAttempt(ClosureJob{Attempt: 4}) != 4 {
		t.Fatal("an explicit attempt counter was not read back")
	}
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	job := sealClosureRestartTestJob(t, ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "inventory", Status: "running", FrozenCampaignRevision: 1,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{}, ProjectionDigests: map[string]string{},
	})
	body, err := canonicalJSON(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "\"attempt\"") {
		t.Fatalf("a first-attempt closure job encoded an attempt field: %s", body)
	}
	var decoded ClosureJob
	if err := decodeStrictJSON(body, &decoded); err != nil {
		t.Fatal(err)
	}
	reencoded, err := canonicalJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(body) {
		t.Fatalf("a pre-restart closure job did not re-encode to its committed bytes:\n%s\n%s", body, reencoded)
	}
	if resealed := sealClosureRestartTestJob(t, decoded); resealed.Digest != job.Digest {
		t.Fatalf("a pre-restart closure job changed digest: %s want %s", resealed.Digest, job.Digest)
	}
}

// preAttemptClosureJob is the ClosureJob shape exactly as it stood before 0.8.4:
// every field of the current record except `attempt`. It stands in for the
// decoder inside an older binary, which is the only thing the compatibility
// claim about that field is actually about.
type preAttemptClosureJob struct {
	RecordMeta
	CampaignID             string            `json:"campaignId"`
	Stage                  string            `json:"stage"`
	Status                 string            `json:"status"`
	FrozenCampaignRevision int64             `json:"frozenCampaignRevision"`
	ProjectionFindingIDs   []string          `json:"projectionFindingIds"`
	Coverage               *ClosureCoverage  `json:"coverage,omitempty"`
	ArchiveDestination     string            `json:"archiveDestination,omitempty"`
	TruthDigests           map[string]string `json:"truthDigests,omitempty"`
	ProjectionDigests      map[string]string `json:"projectionDigests,omitempty"`
	StagingDigest          string            `json:"stagingDigest,omitempty"`
	ArchiveDigest          string            `json:"archiveDigest,omitempty"`
	Blockers               []string          `json:"blockers,omitempty"`
}

// TestTheAttemptFieldIsForwardCompatibleAndNotBackwardReadable states both
// halves of the `attempt` field's compatibility story, because only one of them
// is good news and the record should say so.
//
// Forward, it is safe, and that is the half the engine depends on:
// readCanonicalRecordValue re-encodes every record it reads and refuses it when
// the bytes differ from what was committed, so a closure job written before
// restart existed - which carries no `attempt` at all - must re-encode without
// one. `omitempty` is what makes that true, and this test would fail the moment
// somebody removed it while "tidying up" the struct tags.
//
// Backward, it is not safe, and nothing can make it so. decodeStrictJSON calls
// DisallowUnknownFields, so a binary built before 0.8.4 cannot read a closure
// job that has been through a restart: `attempt` is an unknown field to it and
// the record fails to decode, which means the whole campaign graph fails to
// load. Records already on disk are unaffected - they carry no such field - so
// this is a one-way door rather than a break: once a campaign restarts closure,
// it is pinned to 0.8.4 or later, and rolling that binary back means restoring
// the job record too. The pinning is deliberate, and it is written down here
// because the only alternative - a parallel decoder that tolerates unknown
// fields - would give up the canonical-bytes guarantee that every other
// integrity rule in this package rests on.
func TestTheAttemptFieldIsForwardCompatibleAndNotBackwardReadable(t *testing.T) {
	graph := closureTestGraph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := sealClosureRestartTestJob(t, ClosureJob{
		RecordMeta: closureTestMeta("closure-test-job"), CampaignID: "C-TEST",
		Stage: "inventory", Status: "running", FrozenCampaignRevision: 1,
		ProjectionFindingIDs: []string{"F-0001"}, Coverage: &coverage,
		ArchiveDestination: "docs/history/campaigns/2026-08-02-test",
		TruthDigests:       map[string]string{}, ProjectionDigests: map[string]string{},
	})
	firstBody, err := canonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(firstBody), "\"attempt\"") {
		t.Fatalf("a first-attempt closure job encoded an attempt field: %s", firstBody)
	}

	// Forward compatibility, stated as the property the loader actually enforces:
	// read the committed bytes and re-encode them, and the bytes must be equal.
	var decoded ClosureJob
	if err := decodeStrictJSON(firstBody, &decoded); err != nil {
		t.Fatalf("a pre-restart closure job no longer decodes: %v", err)
	}
	reencoded, err := canonicalJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(firstBody) {
		t.Fatalf("a pre-restart closure job did not re-encode to its committed bytes:\n%s\n%s",
			firstBody, reencoded)
	}
	if closureAttempt(decoded) != 1 {
		t.Fatalf("an absent attempt counter was not read as the first attempt: %d",
			closureAttempt(decoded))
	}

	// The old decoder still reads those same bytes. This also proves the mirror
	// type above is faithful: if it were missing any other field, this decode
	// would fail and the assertion after it would be meaningless.
	var legacy preAttemptClosureJob
	if err := decodeStrictJSON(firstBody, &legacy); err != nil {
		t.Fatalf("a pre-0.8.4 decoder cannot read a record it wrote itself: %v", err)
	}

	// Backward incompatibility, pinned rather than hoped about. A restarted job
	// carries the field, and the old decoder rejects it.
	second := first
	second.Attempt = 2
	second = sealClosureRestartTestJob(t, second)
	secondBody, err := canonicalJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(secondBody), "\"attempt\": 2") {
		t.Fatalf("a restarted closure job did not encode its attempt counter: %s", secondBody)
	}
	var downgraded preAttemptClosureJob
	err = decodeStrictJSON(secondBody, &downgraded)
	if err == nil {
		t.Fatal("a pre-0.8.4 decoder read a restarted closure job; the downgrade note is now wrong")
	}
	if !strings.Contains(err.Error(), "attempt") {
		t.Fatalf("the downgrade failure is not about the attempt field: %v", err)
	}
}
