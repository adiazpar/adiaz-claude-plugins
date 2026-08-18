package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisposablePilotInterruptsAfterDurableBackupAndResumes(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "shadow-indexed" {
		t.Fatalf("shadow: %+v %v", state, err)
	}
	report := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	entry := fullMigrationCoverage(t, root, report, "unresolved", "")
	entry.Rationale = "The disposable recovery fixture preserves this complete span for explicit manager attention."
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: report.Path, SourceDigest: report.SHA256, Complete: true,
		Coverage: []CoverageEntry{entry}, Findings: []MigrationFindingInput{},
		FindingIDs: []string{}, Reviewer: "manager",
		Rationale: "Exercise migration activation recovery without inventing a finding.",
	}); err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	injected := errors.New("disposable interruption")
	engine.ActivationFailpoint = func(point MigrationActivationFailpoint) error {
		if point.Phase == "backed-up" && point.TargetIndex == 0 {
			return injected
		}
		return nil
	}
	if _, err := engine.Resume(state.TransactionID, "manager", "cli"); !errors.Is(err, injected) {
		t.Fatalf("activation did not reach injected seam: %v", err)
	}
	var journal migrationActivationJournal
	body, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "activation.json"))
	if err != nil || decodeStrict(body, &journal) != nil {
		t.Fatalf("read activation journal: %v", err)
	}
	if journal.Phase != "activating" || len(journal.Targets) == 0 || journal.Targets[0].Phase != "backed-up" {
		t.Fatalf("interruption was not durable after backup: %+v", journal)
	}
	engine.ActivationFailpoint = nil
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("forward recovery: %+v %v", state, err)
	}
	for _, gate := range []string{"structural", "semantic-traversal"} {
		artifact, err := BuildMigrationIntrinsicGateArtifact(root, "", gate)
		if err != nil {
			t.Fatalf("derive %s: %v", gate, err)
		}
		if !artifact.Passed || artifact.Gate != gate || artifact.TransactionID != state.TransactionID ||
			artifact.PlanDigest != state.PlanDigest || artifact.ResultDigest == "" {
			t.Fatalf("invalid %s artifact: %+v", gate, artifact)
		}
		for _, check := range artifact.Checks {
			if !strings.Contains(check.Evidence, state.Digest) {
				t.Fatalf("%s check is not state-bound: %+v", gate, check)
			}
		}
	}
}
