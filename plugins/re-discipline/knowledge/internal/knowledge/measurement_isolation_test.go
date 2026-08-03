package knowledge

import (
	"path/filepath"
	"testing"
)

func TestMeasurementReceiptsAreNeitherSourcesNorEvaluationCases(t *testing.T) {
	root := t.TempDir()
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}

	measurementMarkdown := filepath.Join(
		root, ".re-discipline", "knowledge", "measurements", "lane-ablation.md")
	measurementJSON := filepath.Join(
		root, ".re-discipline", "knowledge", "measurements", "lane-ablation.json")
	writeTestFile(t, measurementMarkdown,
		"# Measurement only\n\nThis marker must never enter retrieval: measurement-only-canary.\n")
	// Deliberately invalid JSON: eval loading would fail if it ever crossed the
	// sibling-directory boundary and attempted to decode this receipt.
	writeTestFile(t, measurementJSON, "{not-an-eval-case\n")

	settings := DefaultKnowledgeSettings()
	settings.Sources.Additional = []AdditionalSource{{
		Path: ".re-discipline/knowledge", Pattern: "*.md", Tier: "asset",
	}}
	if err := ValidateSettings(settings); err != nil {
		t.Fatalf("broad parent source fixture is invalid: %v", err)
	}
	inventory, err := DiscoverSources(boundary, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range inventory.Documents {
		if document.Path == ".re-discipline/knowledge/measurements/lane-ablation.md" {
			t.Fatal("measurement receipt entered the discovered corpus")
		}
	}
	for _, state := range inventory.SourceStates {
		if state.Path == ".re-discipline/knowledge/measurements/lane-ablation.md" {
			t.Fatal("measurement receipt entered corpus source-state identity")
		}
	}

	direct := DefaultKnowledgeSettings()
	direct.Sources.Additional = []AdditionalSource{{
		Path: ".re-discipline/knowledge/measurements", Pattern: "*.md", Tier: "asset",
	}}
	if err := ValidateSettings(direct); err == nil {
		t.Fatal("measurement directory was accepted as an additional source class")
	}
	if !IsForbiddenSource(
		".re-discipline/knowledge/measurements/lane-ablation.json") {
		t.Fatal("measurement receipt path is not forbidden at the read boundary")
	}

	answerable := false
	writeTestJSON(t, filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "cases.json"), []EvalCase{{
		ID: "measurement-isolation", Role: "manager", Topic: "measurement-isolation",
		Split: "development", Query: "qzvxjklmnpwtrb",
		QueryClass: "exact", AllowedTiers: []string{"truth"},
		CorpusSnapshot: "fixture:measurement-isolation", TokenBudget: 512,
		Answerable: &answerable,
	}})
	service := Service{Boundary: boundary}
	cases, err := service.loadProjectEvalCases()
	if err != nil {
		t.Fatalf("measurement receipt was decoded as an eval or valid eval failed: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "measurement-isolation" {
		t.Fatalf("unexpected evaluation discovery: %#v", cases)
	}
}
