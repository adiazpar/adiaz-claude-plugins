package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeProjectEvalCase(t *testing.T, root string, cases []EvalCase) {
	t.Helper()
	path := filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "cases.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pinnedFixtureCase(t *testing.T, root string) EvalCase {
	t.Helper()
	answerable := true
	body, err := os.ReadFile(filepath.Join(root, "docs", "truth", "engine.md"))
	if err != nil {
		t.Fatal(err)
	}
	return EvalCase{
		ID: "engine-identifier", Role: "manager", Topic: "engine",
		Split: "development", Query: "A1B2C3D4", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, CorpusSnapshot: "fixture:adversarial",
		ExpectedPaths:        []string{"docs/truth/engine.md"},
		MinimumEvidencePaths: []string{"docs/truth/engine.md"},
		HardNegativePaths:    []string{"docs/history/retired.md"},
		ExpectedCitations:    []string{"docs/truth/engine.md"},
		ForbiddenTiers:       []string{"history"},
		TokenBudget:          1024, Answerable: &answerable,
		EvidencePins: []EvidencePin{{
			Path:          "docs/truth/engine.md",
			ClaimSha256:   ClaimDigest(string(body), "docs/truth/engine.md"),
			ContentSha256: "sha256:" + SHA256Bytes(body),
		}},
	}
}

// Pin rot used to surface only when a calibration errored - that is, at the
// moment somebody needed the measurement to be trustworthy. Status must be able
// to say so first, and must distinguish advisory byte drift from a claim
// nobody has re-answered the query against.
func TestStatusReportsEvidencePinHealth(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	// A project with no ratified evaluation corpus is a legitimate state, not a
	// degraded one.
	empty := service.EvidencePinCensus()
	if empty.Total != 0 || empty.Unavailable != "" {
		t.Fatalf("absent evaluation corpus reported %+v", empty)
	}

	writeProjectEvalCase(t, root, []EvalCase{pinnedFixtureCase(t, root)})
	intact := service.EvidencePinCensus()
	if intact.Total != 1 || intact.Intact != 1 ||
		intact.Drifted != 0 || intact.Broken != 0 {
		t.Fatalf("freshly pinned corpus reported %+v", intact)
	}
	if len(intact.NonIntactPaths) != 0 {
		t.Fatalf("an intact census named non-intact paths: %+v", intact.NonIntactPaths)
	}

	statusPayload, err := service.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status := statusPayload["system"].(map[string]any)
	if _, present := status["pins"]; !present {
		t.Fatal("status omitted the evidence-pin census")
	}

	// Prose beneath the claim moved; the claim did not. The case still measures
	// what it was ratified to measure, so this is drift, not breakage.
	enginePath := filepath.Join(root, "docs", "truth", "engine.md")
	body, err := os.ReadFile(enginePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		enginePath, append(body, []byte("\nAn added paragraph of evidence prose.\n")...), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	drifted := service.EvidencePinCensus()
	if drifted.Drifted != 1 || drifted.Broken != 0 || drifted.Intact != 0 {
		t.Fatalf("reworded evidence reported %+v, want one drifted pin", drifted)
	}
	if len(drifted.NonIntactPaths) != 1 ||
		drifted.NonIntactPaths[0].Path != "docs/truth/engine.md" ||
		drifted.NonIntactPaths[0].State != "drifted" ||
		drifted.NonIntactPaths[0].Pins != 1 {
		t.Fatalf("drifted census named %+v", drifted.NonIntactPaths)
	}

	// The pinned document is gone: the case's ground truth cannot hold.
	if err := os.Remove(enginePath); err != nil {
		t.Fatal(err)
	}
	broken := service.EvidencePinCensus()
	if broken.Broken != 1 || broken.Drifted != 0 || broken.Intact != 0 {
		t.Fatalf("missing evidence reported %+v, want one broken pin", broken)
	}
	if len(broken.NonIntactPaths) != 1 ||
		broken.NonIntactPaths[0].State != "broken" {
		t.Fatalf("broken census named %+v", broken.NonIntactPaths)
	}
}

// The hard-negative guard fails a case on a single hit, so a clean run reads as
// suite-wide protection. It only ever protects the cases that declare a
// negative, and nothing said how few of them did.
func TestHardNegativeCoverageIsReportedNotGated(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)

	guarded := pinnedFixtureCase(t, root)
	bare := pinnedFixtureCase(t, root)
	bare.ID = "engine-identifier-unguarded"
	bare.Topic = "engine-unguarded"
	bare.HardNegativePaths = nil
	writeProjectEvalCase(t, root, []EvalCase{guarded, bare})

	cases, err := service.loadProjectEvalCases()
	if err != nil {
		t.Fatal(err)
	}
	coverage := MeasureHardNegativeCoverage(cases)
	if coverage.TotalCases != 2 || coverage.CasesWithNegatives != 1 ||
		coverage.DeclaredPaths != 1 {
		t.Fatalf("coverage = %+v, want 1 of 2 cases with 1 declared path", coverage)
	}

	statusPayload, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := statusPayload["system"].(map[string]any)
	benchmark, ok := status["benchmark"].(map[string]any)
	if !ok {
		t.Fatal("status omitted the benchmark block")
	}
	if _, present := benchmark["hardNegativeCoverage"]; !present {
		t.Fatal("the benchmark block omitted hard-negative coverage")
	}

	empty := MeasureHardNegativeCoverage(nil)
	if empty.TotalCases != 0 || empty.CasesWithNegatives != 0 {
		t.Fatalf("empty coverage = %+v", empty)
	}
}

func TestStatusReportsOnlyCanonicalCampaignRecords(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)

	// The fixture's legacy campaign prose is deliberately invisible to the
	// 0.8 runtime; only the explicit migrator may interpret it.
	states := service.campaignStatus()
	if len(states) != 0 {
		t.Fatalf("legacy campaign leaked into canonical census: %+v", states)
	}
	value, body, err := sealCampaignRecord(CampaignRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "C-STATUS", Revision: 1,
			CreatedAt: "2026-08-02T20:00:00Z", UpdatedAt: "2026-08-02T20:00:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "status-test",
		},
		Title: "Status campaign", Slug: "status-campaign", Objective: "Exercise status",
		Scope: []string{"status"}, SuccessCriteria: []string{"visible"},
		ClosureCriteria: []string{"complete"}, Status: "open", Owner: "manager",
		PermittedManagers: []string{"manager"}, OpenedAt: "2026-08-02T20:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	campaignPath := filepath.Join(root, "active", "status-campaign", "campaign.json")
	if err := os.MkdirAll(filepath.Dir(campaignPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(campaignPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	campaign := value.(CampaignRecord)
	states = service.campaignStatus()
	if len(states) != 1 || states[0].CampaignID != campaign.ID || states[0].StateViewPresent {
		t.Fatalf("canonical campaign census is wrong: %+v", states)
	}
	if len(states[0].Attention) == 0 {
		t.Fatal("missing generated state view was not surfaced")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(campaignPath), "STATE.md"), []byte("# Generated state\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	states = service.campaignStatus()
	if !states[0].StateViewPresent {
		t.Fatal("generated state view was not detected")
	}

	statusPayload, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := statusPayload["system"].(map[string]any)
	reported, ok := status["campaigns"].([]CampaignState)
	if !ok || len(reported) != 1 || reported[0].CampaignID != campaign.ID {
		t.Fatalf("status campaigns block = %#v", status["campaigns"])
	}
}
