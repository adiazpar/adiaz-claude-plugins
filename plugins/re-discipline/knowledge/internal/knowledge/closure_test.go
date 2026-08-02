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
			SourceRun: "R-20260802-0001",
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
			SourceHandle: reportPath + "#L1-L2", Disposition: "candidate-finding", TargetID: "F-0001",
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
	finding := graph.Findings["F-0001"]
	finding.Evidence[0].SHA256 = "sha256:" + SHA256Bytes(reportBody)
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
