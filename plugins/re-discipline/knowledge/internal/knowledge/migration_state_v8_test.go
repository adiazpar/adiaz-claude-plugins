package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedMigrationEngine(t *testing.T, root string) *MigrationEngine {
	t.Helper()
	engine, err := NewMigrationEngine(root)
	if err != nil {
		t.Fatal(err)
	}
	engine.Now = func() time.Time {
		return time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	}
	return engine
}

func TestMigrationStateMachineRequiresApprovalCoverageGatesAndRatification(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, "sha256:"+strings.Repeat("0", 64), "manager", "cli"); err == nil {
		t.Fatal("an unapproved digest must not start migration")
	}
	if _, err := os.Stat(engine.statePath()); !os.IsNotExist(err) {
		t.Fatal("failed approval must not create canonical migration state")
	}
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil || state.State != "inventoried" {
		t.Fatalf("start: %+v %v", state, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "shadow-indexed" {
		t.Fatalf("shadow: %+v %v", state, err)
	}
	shadow, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "shadow-catalog.json"))
	if err != nil || bytesCount(shadow, []byte("unnormalized provenance")) != 2 {
		t.Fatalf("every report must remain shadow provenance: %v %s", err, shadow)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "shadow-indexed" || len(state.Blockers) != 1 {
		t.Fatalf("live coverage gate: %+v %v", state, err)
	}
	liveReport := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/worker-a/report.md")
	coverage, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: liveReport.Path, SourceDigest: liveReport.SHA256, Complete: true,
		Coverage: []CoverageEntry{{
			SourceHandle: "report.md#VERDICT", Disposition: "candidate-finding", TargetID: "F-0001",
		}},
		FindingIDs: []string{"F-0001"}, Reviewer: "manager",
		Rationale: "fixture coverage",
	})
	if err != nil || coverage.Digest == "" {
		t.Fatalf("coverage: %+v %v", coverage, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate: %+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "live-campaign", "campaign.json")); err != nil {
		t.Fatalf("canonical campaign missing after activation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "live-campaign", "CAMPAIGN.md")); !os.IsNotExist(err) {
		t.Fatal("legacy masterfile remained an operational input")
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "legacy-active", "live-campaign", "CAMPAIGN.md")); err != nil {
		t.Fatalf("recoverable legacy backup missing: %v", err)
	}
	certification, err := engine.Verify()
	if err != nil || certification.Candidate || len(certification.Blockers) != 4 {
		t.Fatalf("certification must await all four gates: %+v %v", certification, err)
	}
	for _, gate := range []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"} {
		_, err := engine.RecordGate(MigrationGateReceipt{
			Gate: gate, Passed: true, Artifact: "migration-tests/" + gate + ".json",
			ArtifactDigest: SHA256String(gate), Reviewer: "manager",
		})
		if err != nil {
			t.Fatalf("record %s gate: %v", gate, err)
		}
	}
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "traversal-verified" || state.CertificationDigest == "" {
		t.Fatalf("verified: %+v %v", state, err)
	}
	state, err = engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify: %+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "state", "head.json")); err != nil {
		t.Fatalf("0.8 state head missing: %v", err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if head, err := store.LoadHead(); err != nil || head.TransactionID != state.TransactionID {
		t.Fatalf("migrated state head is not a valid engine head: %+v %v", head, err)
	}
}

func TestMigrationCoverageIsImmutableAndDigestBound(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	source := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/worker-a/report.md")
	base := MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{{SourceHandle: "report#1", Disposition: "non-claim"}},
		Reviewer: "manager", Rationale: "fixture",
	}
	first, err := engine.SubmitCoverage(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.SubmitCoverage(base)
	if err != nil || first.Digest != second.Digest {
		t.Fatal("identical coverage retry must return the same receipt")
	}
	base.Rationale = "different"
	if _, err := engine.SubmitCoverage(base); err == nil {
		t.Fatal("an immutable coverage receipt cannot be replaced")
	}
	base.SourceDigest = SHA256String("stale")
	if _, err := engine.SubmitCoverage(base); err == nil {
		t.Fatal("stale source coverage must fail")
	}
}

func migrationSourceByPath(t *testing.T, plan MigrationPlan, path string) MigrationSource {
	t.Helper()
	for _, source := range plan.Sources {
		if source.Path == path {
			return source
		}
	}
	t.Fatalf("migration source %s absent", path)
	return MigrationSource{}
}

func bytesCount(body, fragment []byte) int {
	count := 0
	for {
		index := strings.Index(string(body), string(fragment))
		if index < 0 {
			return count
		}
		count++
		body = body[index+len(fragment):]
	}
}
