package knowledge

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// This is a shipment gate, not a historical-evidence gate. Project-corpus
// evidence retains the dense lane and its exact executable artifact; the
// zero-benefit reranker must not remain as a dormant execution branch.
func TestProductionRetrievalSurfaceRetainsDenseAndExcludesReranker(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate production knowledge package")
	}
	packageRoot := filepath.Dir(currentFile)
	assetRoot := filepath.Clean(filepath.Join(packageRoot, "..", ".."))

	if info, err := os.Stat(filepath.Join(packageRoot, "embedding.go")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("production embedding implementation is missing or unsafe: %v", err)
	}
	if _, present := reflect.TypeOf(EffectiveProfile{}).FieldByName("Requires"); !present {
		t.Fatal("effective profile does not expose its embedding requirement")
	}
	if _, present := reflect.TypeOf(EffectiveProfile{}).FieldByName("RerankDepth"); present {
		t.Fatal("effective profile still exposes removed rerankDepth")
	}
	if _, present := reflect.TypeOf(ModelRequirements{}).FieldByName("Embedding"); !present {
		t.Fatal("model requirements do not expose the retained embedding")
	}
	if _, present := reflect.TypeOf(ModelRequirements{}).FieldByName("Reranker"); present {
		t.Fatal("model requirements still expose the removed reranker")
	}
	if _, present := reflect.TypeOf(SearchResult{}).FieldByName("Rerank"); present {
		t.Fatal("search result still exposes removed rerank diagnostics")
	}

	for _, relative := range []string{
		"retrieval.go", "context_cards.go", "index.go", "models.go", "config.go",
		"types.go", "service.go", "evaluation.go", "finding_evaluation_runtime.go",
	} {
		body, err := os.ReadFile(filepath.Join(packageRoot, relative))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, marker := range []string{
			"linearRerank(", "RerankDepth", "Requires.Reranker",
			`case "rerank"`, `laneEnabled(retriever.Profile.ActiveLanes, "rerank")`,
		} {
			if strings.Contains(text, marker) {
				t.Errorf("%s retains removed reranker execution marker %q", relative, marker)
			}
		}
	}

	profileBody, err := os.ReadFile(filepath.Join(assetRoot, "profiles", "balanced-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var profile RetrievalProfile
	if err := decodeStrict(profileBody, &profile); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfile(profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.EffectiveProfiles) != 2 ||
		!reflect.DeepEqual(profile.EffectiveProfiles[0].Lanes, []string{"exact", "fts", "graph", "dense"}) ||
		profile.EffectiveProfiles[0].Requires.Embedding == nil ||
		!reflect.DeepEqual(profile.EffectiveProfiles[1].Lanes, []string{"exact", "fts", "graph"}) ||
		profile.EffectiveProfiles[1].Requires.Embedding != nil {
		t.Fatalf("unexpected production retrieval lanes: %#v", profile.EffectiveProfiles)
	}

	for _, relative := range []string{
		"schemas/retrieval-profile.schema.json",
		"schemas/model-manifest.schema.json",
	} {
		body, err := os.ReadFile(filepath.Join(assetRoot, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(body))
		if !strings.Contains(lower, "embedding") ||
			relative == "schemas/retrieval-profile.schema.json" && !strings.Contains(lower, "dense") {
			t.Errorf("%s does not declare the retained embedding contract", relative)
		}
		if strings.Contains(lower, "reranker") || strings.Contains(lower, `"rerank"`) {
			t.Errorf("%s still exposes the removed reranker contract", relative)
		}
	}

	manifest, err := LoadModelManifest(assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Models) != 1 || len(manifest.ExecutableModels) != 1 ||
		len(manifest.UnavailableModels) != 0 {
		t.Fatalf("production model manifest does not expose one executable embedding: %#v", manifest)
	}
	model := manifest.Models[0]
	if model.ID != "builtin:glove-6b-50d-top50k-q8-v1" ||
		model.Role != "embedding" || model.Revision != "1" ||
		model.SpecSHA256 != "2cdb94f94891907db8bdba40148dc6052b7d2cddacf868bb563092255ef1319a" ||
		model.ArtifactSHA256 != "fb108eef095f00bcc06a38e10d7f9671d9e6664ab79ae8a2c1cef5b31375b2ab" {
		t.Fatalf("production embedding identity drifted: %#v", model)
	}
	if _, err := os.Stat(filepath.Join(assetRoot, "models", "specs", "linear-reranker-v1.json")); !os.IsNotExist(err) {
		t.Fatalf("removed reranker spec still ships: %v", err)
	}
}
