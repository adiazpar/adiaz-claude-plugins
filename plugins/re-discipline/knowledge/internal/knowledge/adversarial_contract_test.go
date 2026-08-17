package knowledge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	adversarialEnginePath      = "docs/truth/findings/fixture-truth/F-9001.md"
	adversarialPortabilityPath = "docs/truth/findings/fixture-truth/F-9002.md"
	adversarialConsumerPath    = "docs/truth/findings/fixture-truth/F-9003.md"
	adversarialHashPath        = "docs/truth/findings/fixture-truth/F-9004.md"
	adversarialLiteralHashPath = "docs/playbooks/hash#fragment.md"
)

func adversarialAssetRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate the test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "profiles", "balanced-v1.json")); err != nil {
		t.Fatalf("knowledge asset root is unavailable: %v", err)
	}
	return root
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(body)+"\n")
}

func copyTestFile(t *testing.T, source, target string) {
	t.Helper()
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeAdversarialTruthFinding(
	t *testing.T,
	root, id, relative, subject, claim string,
	relations FindingRelations,
) {
	t.Helper()
	document := testFindingDocument()
	document.Record.ID = id
	document.Record.CampaignID = "C-FIXTURE-TRUTH"
	document.Record.Path = relative
	document.Record.Subject = subject
	document.Record.Claim = claim
	document.Record.Relations = relations
	document.Record.SourceRuns = []string{"R-20260802-0001"}
	document.Record.Evidence = []EvidenceReference{{
		Path:      "active/fixture-campaign/runs/R-20260802-0001/report.md",
		SHA256:    "sha256:" + SHA256Bytes([]byte("immutable-unnormalized-run-report\n")),
		StartLine: 1, EndLine: 1, SourceRun: "R-20260802-0001",
	}}
	document.Record.ReviewState = "manager-ratified"
	document.Record.Validity = "current"
	document.Record.Projection = "truth"
	relationBody := "Relations are encoded in the canonical record."
	claimLinks := ""
	for _, findingID := range relations.DependsOn {
		claimLinks += "\n\n[Depends on " + findingID + "](./" + findingID + ".md)"
		relationBody += "\n- [Depends on " + findingID + "](./" + findingID + ".md)"
	}
	document.Record.Body = "# Claim\n" + claim + claimLinks +
		"\n\n## Applies when\nThe adversarial fixture is active." +
		"\n\n## Does not establish\nProduction behavior." +
		"\n\n## Evidence\nSee the immutable fixture report." +
		"\n\n## Reproduction\nRun the bounded fixture query." +
		"\n\n## Relations\n" + relationBody
	document.SyntheticQuestions = []string{
		"Which canonical fixture record answers this question?",
		"What does the maintained adversarial truth establish?",
		"Where is the exact fixture result recorded?",
	}
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, relative, body)
}

func makeAdversarialProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	assetRoot := adversarialAssetRoot(t)

	writeTestFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"), `---
name: "adversarial-fixture"
type: "test"
framing: "an isolated knowledge-runtime fixture"
---

# Adversarial fixture profile

The canonical fixture profile carries orientation-marker-alpha.

<!-- re-discipline:shared-laws v0.8.0 -->
The test fixture uses the supported managed-project contract.
<!-- re-discipline:shared-laws:end -->
`)
	writeTestJSON(t, filepath.Join(root, ".re-discipline", "config.json"), DefaultBootstrapConfig())
	writeTestFile(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), `{
  // Comments must be accepted without weakening strict field validation.
  "schemaVersion": 2,
  "sources": {
    "truth": true,
    "historyFindings": true,
    "backlog": true,
    "activeFindings": true,
    "sharedMemory": true,
    "reportFallback": true
  },
  "models": {
    "execution": "local"
  },
  "telemetry": {
    "mode": "metrics-only"
  },
  "budgets": {
    "searchTokens": 1024,
    "managerContextTokens": 2048,
    "drafterContextTokens": 1024,
    "maxCards": 16,
    "maxBytes": 32768
  },
  "archive": {
    "reportFallbackUntilMeasured": true,
    "normalizationTriggerHits": 3
  }
}
`)
	copyTestFile(
		t,
		filepath.Join(assetRoot, "profiles", "balanced-v1.json"),
		filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"),
	)
	writeTestFile(t, filepath.Join(root, "active", "fixture-campaign", "runs", "R-20260802-0001", "report.md"), "immutable-unnormalized-run-report\n")

	writeTestFile(t, filepath.Join(root, "docs", "INDEX.md"), `# Knowledge index

- [Engine truth](truth/findings/fixture-truth/F-9001.md)
- [Consumer truth](truth/findings/fixture-truth/F-9003.md)
`)
	writeTestFile(t, filepath.Join(root, "docs", "truth", "INDEX.md"), `# Truth index

- [Engine](findings/fixture-truth/F-9001.md)
- [Consumer](findings/fixture-truth/F-9003.md)
- [Portability](findings/fixture-truth/F-9002.md)
`)
	writeAdversarialTruthFinding(t, root, "F-9001", adversarialEnginePath,
		"fixture.engine-contract",
		"The exact engine identifier is A1B2C3D4. Frame serialization uses a checksum before durable commit. The canonical protocol name is engine-frame-v7.",
		FindingRelations{})
	writeAdversarialTruthFinding(t, root, "F-9002", adversarialPortabilityPath,
		"fixture.portability-contract",
		"Portable consumers derive stable signatures from maintained recipes.",
		FindingRelations{})
	writeAdversarialTruthFinding(t, root, "F-9003", adversarialConsumerPath,
		"fixture.consumer-contract",
		"The consumer reads engine-frame-v7 and validates stable signatures; it depends on [the portability contract](./F-9002.md).",
		FindingRelations{DependsOn: []string{"F-9002"}})
	writeAdversarialTruthFinding(t, root, "F-9004", adversarialHashPath,
		"fixture.literal-hash-path",
		"literal-hash-path-iota proves that a hash character belongs to the source identity.",
		FindingRelations{})
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(adversarialLiteralHashPath)), `# Literal hash path

literal-hash-path-iota proves that a # character belongs to the file name.
`)
	writeTestFile(t, filepath.Join(root, "docs", "history", "INDEX.md"), "# History index\n")
	writeTestFile(t, filepath.Join(root, "docs", "history", "retired.md"), `# Retired procedure

retired-procedure-zeta is historical and must never satisfy a truth-only query.
`)
	writeTestFile(t, filepath.Join(root, "docs", "backlog", "INDEX.md"), "# Backlog index\n")
	writeTestFile(t, filepath.Join(root, "docs", "backlog", "experiment.md"), `# Deferred experiment

future-experiment-kappa remains intent, not current truth.
`)
	writeTestFile(t, filepath.Join(root, "active", "fixture-campaign", "CAMPAIGN.md"), `# Fixture campaign

campaign-provisional-delta is unresolved.
`)
	writeTestFile(t, filepath.Join(root, "active", "fixture-campaign", "REVIEWS.md"), `# Review Ledger: fixture-campaign

## Unresolved Holds

review-ledger-hold-sigma still needs a decisive observation.
`)
	writeTestFile(t, filepath.Join(root, "active", "fixture-campaign", "notes.md"), "must-not-index-active-notes\n")
	writeTestFile(t, filepath.Join(root, "active", "fixture-campaign", "subagents", "run-01", "report.md"), "legacy-report-must-not-index\n")
	writeTestFile(t, filepath.Join(root, ".re-discipline", "memory", "INDEX.md"), "# Shared memory index\n")
	writeTestFile(t, filepath.Join(root, ".re-discipline", "memory", "topics", "navigation.md"), `# Navigation recall

orientation-shortcut-omega points managers to the truth index.
`)
	writeTestFile(t, filepath.Join(root, ".re-discipline", "memory", "proposals", "excluded.md"), `# Pending proposal

pending-proposal-secret-psi must not enter normal retrieval.
`)

	writeTestFile(t, filepath.Join(root, "docs", "truth", "local-paths.md"), "C:\\private\\must-not-index\n")
	writeTestFile(t, filepath.Join(root, "docs", "truth", "private-key.pem"), "must-not-index\n")
	writeTestFile(t, filepath.Join(root, ".env"), "FIXTURE_SECRET=must-not-index\n")
	return root
}

func newAdversarialService(t *testing.T, root string, mutate func(*ServiceOptions)) *Service {
	t.Helper()
	options := ServiceOptions{
		ProjectRoot: root,
		AssetRoot:   adversarialAssetRoot(t),
		CacheRoot:   filepath.Join(root, ".re-discipline", "cache", "knowledge"),
	}
	if mutate != nil {
		mutate(&options)
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func readCatalogAndManifest(t *testing.T) (RetrievalProfile, ModelManifest) {
	t.Helper()
	assetRoot := adversarialAssetRoot(t)
	body, err := os.ReadFile(filepath.Join(assetRoot, "profiles", "balanced-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var profile RetrievalProfile
	if err := decodeStrict(body, &profile); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadModelManifest(assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	return profile, manifest
}

func cloneProfile(t *testing.T, input RetrievalProfile) RetrievalProfile {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output RetrievalProfile
	if err := json.Unmarshal(body, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestAdversarialStrictConfigurationValidation(t *testing.T) {
	t.Run("comments inside strings survive JSONC stripping", func(t *testing.T) {
		input := []byte(`{"url":"https://example.invalid/a//b","literal":"/*not-comment*/"} // comment`)
		stripped, err := StripJSONComments(input)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		if err := decodeStrict(stripped, &got); err != nil {
			t.Fatal(err)
		}
		if got["url"] != "https://example.invalid/a//b" || got["literal"] != "/*not-comment*/" {
			t.Fatalf("JSONC string content changed: %#v", got)
		}
	})

	t.Run("multiple top-level values are rejected", func(t *testing.T) {
		var got map[string]any
		if err := decodeStrict([]byte(`{"one":1} {"two":2}`), &got); err == nil {
			t.Fatal("decodeStrict accepted multiple top-level JSON values")
		}
	})

	t.Run("unknown bootstrap field is rejected", func(t *testing.T) {
		root := makeAdversarialProject(t)
		writeTestFile(t, filepath.Join(root, ".re-discipline", "config.json"), `{
		  "schemaVersion": 2,
		  "knowledgeDirectory": "knowledge",
		  "memory": {"mode": "shared-only", "writePolicy": "proposal-only"},
		  "knowledge": {
		    "enabled": true,
		    "profile": "plugin:balanced-v1",
		    "settingsFile": "knowledge/policy.jsonc",
		    "projectProfile": "knowledge/retrieval-profile.json"
		  },
		  "unexpected": true
		}`)
		config := LoadConfiguration(root)
		if config.Valid || len(config.Errors) == 0 {
			t.Fatalf("unknown field did not invalidate bootstrap config: %#v", config)
		}
	})

	t.Run("unknown settings field is rejected", func(t *testing.T) {
		root := makeAdversarialProject(t)
		path := filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(body), `"mode": "metrics-only"`, `"mode": "metrics-only", "contentTrace": true`, 1)
		writeTestFile(t, path, changed)
		config := LoadConfiguration(root)
		if config.Valid || len(config.Errors) == 0 {
			t.Fatal("unknown project setting did not invalidate configuration")
		}
	})

	t.Run("unsafe budget is rejected without rewriting it", func(t *testing.T) {
		root := makeAdversarialProject(t)
		path := filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(body), `"searchTokens": 1024`, `"searchTokens": 999999`, 1)
		writeTestFile(t, path, changed)
		before, _ := os.ReadFile(path)
		config := LoadConfiguration(root)
		after, _ := os.ReadFile(path)
		if config.Valid {
			t.Fatal("unsafe budget was accepted")
		}
		if string(before) != string(after) {
			t.Fatal("runtime rewrote malformed or unsafe project settings")
		}
	})

	t.Run("managed paths cannot be redirected", func(t *testing.T) {
		config := DefaultBootstrapConfig()
		config.Knowledge.SettingsFile = "../outside.json"
		if err := ValidateBootstrap(config); err == nil {
			t.Fatal("bootstrap path redirection was accepted")
		}
	})
}

func TestAdversarialRetrievalProfileValidation(t *testing.T) {
	profile, manifest := readCatalogAndManifest(t)
	if err := ValidateProfile(profile); err != nil {
		t.Fatalf("packaged profile is invalid: %v", err)
	}
	if err := ValidateProfileModels(profile, manifest); err != nil {
		t.Fatalf("packaged profile/model contract is invalid: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*RetrievalProfile)
	}{
		{
			name: "duplicate lane",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles[0].Lanes = append(p.EffectiveProfiles[0].Lanes, "exact")
			},
		},
		{
			name: "unknown weighted lane",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles[0].Weights["unmeasured"] = 99
			},
		},
		{
			name: "truncated benchmark digest",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles[0].Benchmark.Digest = "sha256:abc"
			},
		},
		{
			name: "missing benchmark suite identity",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles[0].Benchmark.Suite = ""
			},
		},
		{
			name: "duplicate benchmark evidence",
			mutate: func(p *RetrievalProfile) {
				row := p.EffectiveProfiles[0]
				row.Name = "duplicate-row"
				p.EffectiveProfiles = append(p.EffectiveProfiles, row)
			},
		},
		{
			name: "missing lexical graph capability",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles = nil
			},
		},
		{
			name: "rerank without dense",
			mutate: func(p *RetrievalProfile) {
				p.EffectiveProfiles[0].Lanes = []string{"exact", "fts", "graph", "rerank"}
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneProfile(t, profile)
			mutation.mutate(&candidate)
			if err := ValidateProfile(candidate); err == nil {
				t.Fatalf("unsafe profile mutation %q was accepted", mutation.name)
			}
		})
	}
}

func TestAdversarialModelManifestAndSpecValidation(t *testing.T) {
	assetRoot := adversarialAssetRoot(t)
	manifest, err := LoadModelManifest(assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Models) != 1 || len(manifest.ExecutableModels) != 1 ||
		manifest.Models[0].Role != "embedding" {
		t.Fatalf("manifest does not expose the one retained embedding: %#v", manifest.Models)
	}

	makeAssets := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		if err := copyTree(filepath.Join(assetRoot, "models"), filepath.Join(root, "models")); err != nil {
			t.Fatal(err)
		}
		return root
	}
	loadRaw := func(t *testing.T, root string) map[string]any {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, "models", "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	t.Run("unknown manifest fields are rejected", func(t *testing.T) {
		root := makeAssets(t)
		raw := loadRaw(t, root)
		raw["untrackedBehavior"] = true
		writeTestJSON(t, filepath.Join(root, "models", "manifest.json"), raw)
		if _, err := LoadModelManifest(root); err == nil {
			t.Fatal("unknown manifest field was accepted")
		}
	})

	t.Run("tracked policy cannot enable network downloads", func(t *testing.T) {
		root := makeAssets(t)
		raw := loadRaw(t, root)
		raw["externalModelPolicy"].(map[string]any)["networkDownloads"] = true
		writeTestJSON(t, filepath.Join(root, "models", "manifest.json"), raw)
		if _, err := LoadModelManifest(root); err == nil {
			t.Fatal("network-enabled external model policy was accepted")
		}
	})

	t.Run("runtime numerical contract is not self-asserted", func(t *testing.T) {
		root := makeAssets(t)
		raw := loadRaw(t, root)
		raw["runtime"].(map[string]any)["numericalBackend"] = "floating-point-unpinned"
		writeTestJSON(t, filepath.Join(root, "models", "manifest.json"), raw)
		if _, err := LoadModelManifest(root); err == nil {
			t.Fatal("manifest changed the compiled numerical backend identity")
		}
	})

	t.Run("reranker model is rejected", func(t *testing.T) {
		root := makeAssets(t)
		raw := loadRaw(t, root)
		model := raw["models"].([]any)[0].(map[string]any)
		model["role"] = "reranker"
		writeTestJSON(t, filepath.Join(root, "models", "manifest.json"), raw)
		_, err := LoadModelManifest(root)
		if err == nil || !strings.Contains(err.Error(), "unsupported role") {
			t.Fatalf("reranker model was accepted or produced the wrong failure: %v", err)
		}
	})

	t.Run("embedding spec traversal is rejected", func(t *testing.T) {
		root := makeAssets(t)
		raw := loadRaw(t, root)
		model := raw["models"].([]any)[0].(map[string]any)
		model["specFile"] = "models/specs/../../outside.json"
		writeTestJSON(t, filepath.Join(root, "models", "manifest.json"), raw)
		_, err := LoadModelManifest(root)
		if err == nil || !strings.Contains(err.Error(), "unsafe spec path") {
			t.Fatalf("embedding spec traversal was accepted or produced the wrong failure: %v", err)
		}
	})
}

func TestAdversarialDenseSelectionIdentity(t *testing.T) {
	profile, manifest := readCatalogAndManifest(t)
	identity, err := ProbeRuntimeIdentity(manifest)
	if err != nil {
		t.Fatal(err)
	}

	selected, err := SelectEffectiveProfile(profile, manifest, identity)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Effective.Name != "hybrid-no-rerank-v1" || selected.FallbackReason != nil ||
		len(selected.Models) != 1 ||
		!reflect.DeepEqual(selected.ActiveLanes, []string{"exact", "fts", "graph", "dense"}) {
		t.Fatalf("unexpected dense profile selection: %#v", selected)
	}

	changedBackend := identity
	changedBackend.NumericalBackend += "-different"
	backendSelection, err := SelectEffectiveProfile(profile, manifest, changedBackend)
	if err != nil {
		t.Fatal(err)
	}
	if backendSelection.EffectiveIdentity == selected.EffectiveIdentity {
		t.Fatal("effective identity omitted the numerical backend")
	}
	changedTieBreaker := identity
	changedTieBreaker.TieBreaker += "-different"
	tieSelection, err := SelectEffectiveProfile(profile, manifest, changedTieBreaker)
	if err != nil {
		t.Fatal(err)
	}
	if tieSelection.EffectiveIdentity == selected.EffectiveIdentity {
		t.Fatal("effective identity omitted deterministic tie-breaking")
	}
}

func TestAdversarialTrackedManifestCannotGrantUnavailableExternalModels(t *testing.T) {
	profile, manifest := readCatalogAndManifest(t)
	identity, err := ProbeRuntimeIdentity(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Models = append(manifest.Models, ModelSpec{
		ID: "local:removed-model-v1", Role: "removed", Revision: "1",
		Implementation:  "onnx-local",
		SpecSHA256:      strings.Repeat("1", 64),
		ArtifactSHA256:  strings.Repeat("2", 64),
		Dimensions:      512,
		NetworkRequired: false,
	})
	if _, err := SelectEffectiveProfile(profile, manifest, identity); err == nil {
		t.Fatal("model-bearing manifest was accepted by the production selector")
	}
}

func TestAdversarialPackagedLearnedModelProvenanceAndClaimsAreHonest(t *testing.T) {
	assetRoot := adversarialAssetRoot(t)
	profile, manifest := readCatalogAndManifest(t)
	if len(profile.EffectiveProfiles) != 2 ||
		!reflect.DeepEqual(profile.EffectiveProfiles[0].Lanes, []string{"exact", "fts", "graph", "dense"}) ||
		len(manifest.Models) != 1 || manifest.Models[0].Role != "embedding" {
		t.Fatalf("two-layer ablation decision is not the shipped capability set: profile=%#v models=%#v",
			profile.EffectiveProfiles, manifest.Models)
	}
	pluginReadme, err := os.ReadFile(filepath.Join(filepath.Dir(assetRoot), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	knowledgeReadme, err := os.ReadFile(filepath.Join(assetRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"plugin": pluginReadme, "knowledge": knowledgeReadme} {
		text := strings.ToLower(string(body))
		if !strings.Contains(text, "dense") || !strings.Contains(text, "rerank") ||
			!strings.Contains(text, "holdout") {
			t.Errorf("%s README does not disclose the measured lane decision", name)
		}
	}
	for _, relative := range []string{
		"models/specs/glove-6b-50d-top50k-q8-v1.json",
		"models/artifacts/glove-6b-50d-top50k-q8-v1.bin",
	} {
		if info, err := os.Stat(filepath.Join(assetRoot, filepath.FromSlash(relative))); err != nil || !info.Mode().IsRegular() {
			t.Errorf("retained embedding asset is missing or unsafe: %s", relative)
		}
	}
	if _, err := os.Stat(filepath.Join(assetRoot, "models", "specs", "linear-reranker-v1.json")); !os.IsNotExist(err) {
		t.Error("removed reranker spec still ships")
	}
}

func makeDirectoryLink(t *testing.T, target, link string) bool {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		resolved, resolveErr := filepath.EvalSymlinks(link)
		t.Logf("created directory symlink %s -> %s (resolved=%s err=%v)",
			link, target, resolved, resolveErr)
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	command := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Logf("directory link unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
		return false
	}
	resolved, resolveErr := filepath.EvalSymlinks(link)
	t.Logf("created directory junction %s -> %s (resolved=%s err=%v)",
		link, target, resolved, resolveErr)
	return true
}

func makeFileLink(t *testing.T, target, link string) bool {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	command := exec.Command("cmd", "/c", "mklink", link, target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Logf("file symlink unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
		return false
	}
	return true
}

func TestAdversarialSourceTiersSecretsAndBoundary(t *testing.T) {
	root := makeAdversarialProject(t)
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverSources(boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]string{}
	for _, document := range inventory.Documents {
		tiers[document.Path] = document.Tier
	}
	expected := map[string]string{
		".re-discipline/project-profile.md":          "profile",
		".re-discipline/memory/INDEX.md":             "navigation",
		".re-discipline/memory/topics/navigation.md": "memory",
		"docs/INDEX.md":                              "navigation",
		"docs/truth/INDEX.md":                        "navigation",
		adversarialEnginePath:                        "truth",
		"docs/history/retired.md":                    "history",
		"docs/backlog/experiment.md":                 "backlog",
		// Raw run reports are immutable provenance fallback, never normalized
		// campaign knowledge. The canonical 0.8 runs path is part of the source
		// contract and legacy subagents paths are rejected below.
		"active/fixture-campaign/runs/R-20260802-0001/report.md": "archive",
	}
	for path, tier := range expected {
		if tiers[path] != tier {
			t.Errorf("%s tier = %q, want %q", path, tiers[path], tier)
		}
	}
	for _, forbidden := range []string{
		".re-discipline/memory/proposals/excluded.md",
		"docs/truth/local-paths.md",
		"docs/truth/private-key.pem",
		"active/fixture-campaign/notes.md",
		"active/fixture-campaign/CAMPAIGN.md",
		"active/fixture-campaign/REVIEWS.md",
		"active/fixture-campaign/subagents/run-01/report.md",
	} {
		if _, ok := tiers[forbidden]; ok {
			t.Errorf("forbidden or provisional source was indexed: %s", forbidden)
		}
	}
	for _, path := range []string{"../outside.md", `..\outside.md`, "/absolute.md"} {
		if _, err := boundary.Resolve(path, true); err == nil {
			t.Errorf("boundary accepted traversal/absolute path %q", path)
		}
	}
	for _, path := range []string{
		".env", ".env.local", "id_rsa", "id_ed25519", "local-paths.md",
		"secret.pem", "secret.key", "debug.log",
		".re-discipline/memory/proposals/pending.md",
		"active/x/evidence/raw.md",
	} {
		if !IsForbiddenSource(path) {
			t.Errorf("sensitive path class was not excluded: %s", path)
		}
	}

	outside := t.TempDir()
	writeTestFile(t, filepath.Join(outside, "escaped.md"), "# Escaped\noutside-secret\n")
	link := filepath.Join(root, "docs", "truth", "escape")
	if !makeDirectoryLink(t, outside, link) {
		t.Log("symlink/junction creation is unavailable; traversal assertions still ran")
		return
	}
	if _, err := boundary.Resolve("docs/truth/escape/escaped.md", true); err == nil {
		t.Fatal("boundary followed a symlink or junction outside the project")
	}
	inventory, err = DiscoverSources(boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range inventory.Documents {
		if strings.Contains(document.Content, "outside-secret") {
			t.Fatal("source discovery indexed content through an escaping link")
		}
	}
}

func TestAdversarialCacheRootsRequireDeterministicLocalGrant(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "localappdata"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "xdg-cache"))
	root := makeAdversarialProject(t)
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	projectCache, err := ResolveCacheRoot(
		boundary, filepath.Join(root, ".re-discipline", "cache", "knowledge"),
	)
	if err != nil || !withinRoot(boundary.Root, projectCache) {
		t.Fatalf("project cache grant failed: path=%s err=%v", projectCache, err)
	}
	machine := MachineCacheRoot(boundary)
	machineCache, err := ResolveCacheRoot(boundary, machine)
	expectedMachine, expectedErr := evalNearestExisting(machine)
	if err != nil || expectedErr != nil ||
		filepath.Clean(machineCache) != filepath.Clean(expectedMachine) {
		t.Fatalf("deterministic machine cache grant failed: path=%s err=%v", machineCache, err)
	}
	outside := t.TempDir()
	if _, err := ResolveCacheRoot(boundary, filepath.Join(outside, "arbitrary")); err == nil {
		t.Fatal("arbitrary external cache root was accepted without a local grant")
	}
	link := filepath.Join(root, ".re-discipline", "cache", "escape")
	if makeDirectoryLink(t, outside, link) {
		if _, err := ResolveCacheRoot(boundary, filepath.Join(link, "knowledge")); err == nil {
			t.Fatal("cache root escaped through a symlink or junction")
		}
	}
}

func TestAdversarialAtomicReplacementAndGenerationRecovery(t *testing.T) {
	t.Run("atomic replacement supports repeated updates", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "nested", "current.json")
		if err := AtomicWrite(path, []byte("first\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(path, []byte("second\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "second\n" {
			t.Fatalf("atomic replacement left stale content: %q", body)
		}
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".re-discipline-tmp-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("atomic replacement leaked temporary files: %v", matches)
		}
	})

	t.Run("rebuild deletion and corrupt generation recovery", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		ctx := context.Background()
		first, firstInventory, rebuilt, err := service.Index.Ensure(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !rebuilt || first.ID == "" || first.DocumentCount != len(firstInventory.Documents) {
			t.Fatalf("first generation was not built coherently: %#v rebuilt=%v", first, rebuilt)
		}
		second, _, rebuilt, err := service.Index.Ensure(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if rebuilt || second.ID != first.ID {
			t.Fatal("unchanged corpus unexpectedly rebuilt")
		}

		engine := filepath.Join(root, "docs", "history", "retired.md")
		file, err := os.OpenFile(engine, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("\nAdditional generation-marker-beta.\n"); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		third, _, rebuilt, err := service.Index.Ensure(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !rebuilt || third.ID == second.ID {
			t.Fatal("content change did not create a new generation")
		}

		if err := os.Remove(filepath.Join(root, "docs", "backlog", "experiment.md")); err != nil {
			t.Fatal(err)
		}
		fourth, fourthInventory, rebuilt, err := service.Index.Ensure(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !rebuilt || fourth.ID == third.ID {
			t.Fatal("source deletion did not create a new generation")
		}
		for _, document := range fourthInventory.Documents {
			if document.Path == "docs/backlog/experiment.md" {
				t.Fatal("deleted source survived generation reconciliation")
			}
		}

		// Ensure the rebuild receives a different timestamp-derived destination.
		nextSecond := time.Now().Truncate(time.Second).Add(time.Second)
		time.Sleep(time.Until(nextSecond) + 10*time.Millisecond)
		corruptPath := fourth.Database
		if err := os.WriteFile(corruptPath, []byte("not a SQLite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		recovered, _, rebuilt, err := service.Index.Ensure(ctx)
		if err != nil {
			t.Fatalf("corrupt active generation was not rebuilt: %v", err)
		}
		if !rebuilt || verifyDatabase(recovered.Database) != nil {
			t.Fatal("corrupt generation recovery did not activate a verified rebuild")
		}
		if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
			t.Fatalf("corrupt generation was left in the live .sqlite namespace: %s", corruptPath)
		}
		current, err := service.Index.LoadCurrent()
		if err != nil {
			t.Fatal(err)
		}
		if current.ID != recovered.ID || current.Database != recovered.Database {
			t.Fatal("current.json did not atomically select the recovered generation")
		}
	})
}

func assertAdversarialRuntimeIdentity(t *testing.T, identity RuntimeIdentity) {
	t.Helper()
	if identity.Implementation == "" || identity.Version == "" ||
		identity.GoVersion == "" || identity.CompiledBuildID == "" ||
		identity.SQLiteDriver == "" || identity.SQLiteVersion == "" ||
		!sha256IdentityRE.MatchString(identity.SQLiteBuild) ||
		identity.NumericalBackend != "fixed-int64-v1" ||
		identity.TieBreaker == "" ||
		(identity.ExecutableSHA256 != "unavailable" &&
			!sha256IdentityRE.MatchString(identity.ExecutableSHA256)) {
		t.Fatalf("runtime identity is incomplete: %#v", identity)
	}
}

func TestAdversarialRuntimeContractExcludesHostPackagingIdentity(t *testing.T) {
	first := RuntimeIdentity{
		Implementation:   "re-discipline-knowledge-go",
		Version:          RuntimeVersion,
		GoVersion:        "go1.26.5",
		CompiledBuildID:  "windows-build",
		ExecutableSHA256: "sha256:" + strings.Repeat("a", 64),
		SQLiteDriver:     "modernc.org/sqlite@v1.54.0",
		SQLiteVersion:    "3.53.3",
		SQLiteBuild:      "sha256:" + strings.Repeat("b", 64),
		NumericalBackend: "fixed-int64-v1",
		TieBreaker:       "score-desc,path-asc,start-line-asc,chunk-id-asc",
	}
	second := first
	second.CompiledBuildID = "linux-build"
	second.ExecutableSHA256 = "sha256:" + strings.Repeat("c", 64)
	second.SQLiteBuild = "sha256:" + strings.Repeat("d", 64)

	firstContract := RuntimeContract(first)
	secondContract := RuntimeContract(second)
	if firstContract != secondContract {
		t.Fatalf("portable runtime contract varies by host packaging: %#v != %#v",
			firstContract, secondContract)
	}
	if firstContract.SQLiteBuild == first.SQLiteBuild ||
		!sha256IdentityRE.MatchString(firstContract.SQLiteBuild) {
		t.Fatalf("runtime contract retained host SQLite build identity: %#v",
			firstContract)
	}
}

func TestAdversarialContextGenerationCompactsBinaryProvenance(t *testing.T) {
	runtimeIdentity := RuntimeIdentity{
		Implementation:   "re-discipline-knowledge-go",
		Version:          RuntimeVersion,
		GoVersion:        "go1.26.5",
		CompiledBuildID:  "development",
		ExecutableSHA256: "sha256:" + strings.Repeat("a", 64),
		SQLiteDriver:     "modernc.org/sqlite@v1.54.0",
		SQLiteVersion:    "3.53.3",
		SQLiteBuild:      "sha256:" + strings.Repeat("b", 64),
		NumericalBackend: "fixed-int64-v1",
		TieBreaker:       "score-desc,path-asc,start-line-asc,chunk-id-asc",
	}
	generation := Generation{
		ID:                "generation-" + strings.Repeat("c", 20),
		CorpusFingerprint: "sha256:" + strings.Repeat("d", 64),
		ModelFingerprint:  "sha256:" + strings.Repeat("e", 64),
		Project:           "fixture",
		Worktree:          "worktree:" + strings.Repeat("f", 16),
		GitRevision:       "unversioned",
		DirtyFingerprint:  "clean",
		ParserVersion:     ParserVersion,
		ChunkerVersion:    ChunkerVersion,
		CreatedAt:         "2026-07-26T00:00:00Z",
		Runtime:           runtimeIdentity,
	}
	development := CompactContextGeneration(generation)
	developmentBody, err := json.Marshal(development)
	if err != nil {
		t.Fatal(err)
	}
	expectedFingerprint, err := CanonicalDigest(runtimeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if development.RuntimeFingerprint != expectedFingerprint ||
		bytes.Contains(developmentBody, []byte("compiledBuildId")) ||
		bytes.Contains(developmentBody, []byte("executableSha256")) ||
		bytes.Contains(developmentBody, []byte(runtimeIdentity.CompiledBuildID)) ||
		bytes.Contains(developmentBody, []byte(runtimeIdentity.ExecutableSHA256)) {
		t.Fatalf("compact context generation leaks redundant binary provenance: %s",
			developmentBody)
	}

	generation.Runtime.CompiledBuildID = "sha256:" + strings.Repeat("1", 64)
	generation.Runtime.ExecutableSHA256 = "sha256:" + strings.Repeat("2", 64)
	packaged := CompactContextGeneration(generation)
	packagedBody, err := json.Marshal(packaged)
	if err != nil {
		t.Fatal(err)
	}
	if packaged.RuntimeFingerprint == development.RuntimeFingerprint {
		t.Fatal("runtime fingerprint does not commit the packaged binary identity")
	}
	if len(packagedBody) != len(developmentBody) {
		t.Fatalf(
			"compact provenance wire size depends on verbose binary identity: development=%d packaged=%d",
			len(developmentBody), len(packagedBody))
	}
}

func assertAdversarialModelIdentities(t *testing.T, models []ModelIdentity) {
	t.Helper()
	seen := map[string]bool{}
	for _, model := range models {
		if !profileIdentityRE.MatchString(model.ID) || seen[model.ID] ||
			model.Revision == "" || !hexDigestRE.MatchString(model.SpecSHA256) ||
			(model.ArtifactSHA256 != "" &&
				!hexDigestRE.MatchString(model.ArtifactSHA256)) ||
			(model.Implementation != "builtin" &&
				model.Implementation != "bundled-local") ||
			model.NumericalBackend != "fixed-int64-v1" ||
			model.Dimensions < 0 || model.Dimensions > 4096 {
			t.Fatalf("model identity is incomplete: %#v", model)
		}
		seen[model.ID] = true
	}
}

func assertAdversarialRetrievalIdentity(
	t *testing.T,
	metadata RetrievalMetadata,
	generation Generation,
	selected SelectedProfile,
) {
	t.Helper()
	modelNames := make([]string, 0, len(selected.Models))
	for _, model := range selected.Models {
		modelNames = append(modelNames, model.ID+"@"+model.Revision)
	}
	modelFingerprint, _ := CanonicalDigest(selected.Models)
	runtimeFingerprint, _ := CanonicalDigest(generation.Runtime)
	if metadata.Project != generation.Project ||
		metadata.Worktree != generation.Worktree ||
		metadata.GitRevision != generation.GitRevision ||
		metadata.DirtyFingerprint != generation.DirtyFingerprint ||
		metadata.Generation != generation.ID ||
		metadata.CorpusFingerprint != generation.CorpusFingerprint ||
		metadata.ParserVersion != generation.ParserVersion ||
		metadata.ChunkerVersion != generation.ChunkerVersion ||
		metadata.RequestedProfile != selected.RequestedIdentity ||
		metadata.EffectiveProfile != selected.EffectiveIdentity ||
		stableJSON(metadata.ActiveLanes) != stableJSON(selected.ActiveLanes) ||
		stableJSON(metadata.Models) != stableJSON(modelNames) ||
		metadata.ModelFingerprint != modelFingerprint ||
		metadata.RuntimeFingerprint != runtimeFingerprint ||
		stableJSON(metadata.FallbackReason) != stableJSON(selected.FallbackReason) ||
		!regexp.MustCompile(`^replay-[a-f0-9]{20}$`).MatchString(metadata.DeterministicReplay) {
		t.Fatalf("retrieval metadata does not bind exact execution identity: %#v", metadata)
	}
}

func TestAdversarialExactFTSGraphCitationsBudgetsAndReplay(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	exact, err := rankExact(ctx, db, "A1B2C3D4", []string{"truth"}, nil, 400)
	if err != nil || len(exact) == 0 || exact[0].Chunk.Path != adversarialEnginePath {
		t.Fatalf("exact lane missed technical identifier: rows=%#v err=%v", exact, err)
	}
	fts, err := rankFTS(ctx, db, "frame serialization checksum", []string{"truth"}, nil, 200)
	if err != nil || len(fts) == 0 || fts[0].Chunk.Path != adversarialEnginePath {
		t.Fatalf("FTS lane missed engine truth: rows=%#v err=%v", fts, err)
	}
	if len(selected.Models) != 1 ||
		!reflect.DeepEqual(selected.ActiveLanes, []string{"exact", "fts", "graph", "dense"}) {
		t.Fatalf("selected profile omitted the evidence-retained dense lane: %#v", selected)
	}
	dense, err := rankDense(ctx, db, "frame encoding serialization", []string{"truth"}, nil, selected.Models[0])
	if err != nil || len(dense) == 0 {
		t.Fatalf("dense lane did not execute against indexed vectors: rows=%d err=%v", len(dense), err)
	}
	consumer, err := rankExact(ctx, db, "depends on the portability contract", []string{"truth"}, nil, 400)
	if err != nil || len(consumer) == 0 {
		t.Fatalf("direct path lookup missed graph seed: %v", err)
	}
	consumer[0].LaneRanks = map[string]int{"exact": 1}
	graph, err := rankGraph(ctx, db, []*candidate{&consumer[0]}, []string{"truth"}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	graphPaths := map[string]bool{}
	for _, row := range graph {
		graphPaths[row.Chunk.Path] = true
	}
	if !graphPaths[adversarialPortabilityPath] {
		t.Fatalf("dependency/link graph did not expand to portability truth: %v", graphPaths)
	}

	options := SearchOptions{
		Query: "A1B2C3D4", QueryClass: "exact", AllowedTiers: []string{"truth"},
		Limit: 12, TokenBudget: 1024,
		// The full execution identity this asserts is exactly what a compact
		// response drops as re-derivable; ask for the canonical form.
		Verbosity: VerbosityVerbose,
	}
	first, err := service.Search(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAdversarialRetrievalIdentity(t, first.Metadata, generation, selected)
	second, err := service.Search(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if stableJSON(first) != stableJSON(second) ||
		first.Metadata.DeterministicReplay != second.Metadata.DeterministicReplay {
		t.Fatal("identical query/profile/generation/budget did not replay identically")
	}
	if len(first.Results) == 0 || first.EstimatedTokens > first.TokenBudget {
		t.Fatalf("search violated evidence or budget contract: %#v", first)
	}
	for _, result := range first.Results {
		citation := result.Citation
		if citation.Tier != "truth" || citation.Path == "" || citation.StartLine < 1 ||
			citation.EndLine < citation.StartLine || len(citation.ContentHash) != 64 ||
			!strings.Contains(citation.URI, generation.ID) {
			t.Fatalf("incomplete citation: %#v", citation)
		}
		body, _, err := ReadProjectFile(service.Boundary, citation.Path)
		if err != nil {
			t.Fatal(err)
		}
		passage, err := lineRangeBody(body, citation.StartLine, citation.EndLine)
		if err != nil {
			t.Fatal(err)
		}
		if passage != result.Passage || SHA256String(passage) != citation.ContentHash {
			t.Fatalf("citation does not resolve to returned passage: %#v", citation)
		}
	}

	restricted, err := service.Search(ctx, SearchOptions{
		Query: "retired-procedure-zeta", QueryClass: "current",
		AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range restricted.Results {
		if result.Citation.Tier != "truth" || result.Citation.Path == "docs/history/retired.md" {
			t.Fatalf("forbidden history tier crossed a truth-only query: %#v", result)
		}
	}

	pack, err := service.ContextPack(
		ctx, "engine frame serialization checksum", "drafter", []string{"truth"}, 1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayPack, err := service.ContextPack(
		ctx, "engine frame serialization checksum", "drafter", []string{"truth"}, 1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stableJSON(pack) != stableJSON(replayPack) || pack.Digest == "" ||
		pack.PackID == "" || pack.EstimatedTokens > pack.TokenBudget {
		t.Fatalf("context pack is not deterministic and hard-budgeted: %#v", pack)
	}
	if pack.RequestedProfile == "" || pack.EffectiveProfile == "" ||
		len(pack.ActiveLanes) == 0 || pack.Generation.ID == "" {
		t.Fatalf("context pack omitted execution identity: %#v", pack)
	}
	packModelNames := make([]string, 0, len(selected.Models))
	for _, model := range selected.Models {
		packModelNames = append(packModelNames, model.ID+"@"+model.Revision)
	}
	if stableJSON(pack.Generation) != stableJSON(CompactContextGeneration(generation)) ||
		stableJSON(pack.Models) != stableJSON(packModelNames) {
		t.Fatalf("context pack does not bind the indexed execution identity: %#v", pack)
	}

	statusPayload, err := service.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := statusPayload["system"].(map[string]any)
	if !ok {
		t.Fatalf("status omitted the system block: %#v", statusPayload)
	}
	statusRuntime, ok := status["runtime"].(RuntimeIdentity)
	if !ok || statusRuntime != selected.Runtime {
		t.Fatalf("status omitted full runtime identity: %#v", status["runtime"])
	}
	assertAdversarialRuntimeIdentity(t, statusRuntime)
	statusModels, ok := status["models"].([]ModelIdentity)
	if !ok || stableJSON(statusModels) != stableJSON(selected.Models) {
		t.Fatalf("status omitted full selected model identities: %#v", status["models"])
	}
	assertAdversarialModelIdentities(t, statusModels)
	statusGeneration, ok := status["generation"].(GenerationSummary)
	if !ok || stableJSON(statusGeneration) != stableJSON(PublicGeneration(generation)) {
		t.Fatalf("status omitted full generation identity: %#v", status["generation"])
	}
	assertAdversarialRuntimeIdentity(t, statusGeneration.Runtime)

	hashRead, err := service.Read(ctx, ReadOptions{Path: adversarialLiteralHashPath})
	if err != nil {
		t.Fatalf("managed path containing # was not readable: %v", err)
	}
	hashCitation, ok := hashRead["citation"].(Citation)
	if !ok {
		t.Fatalf("read returned unexpected citation type: %#v", hashRead["citation"])
	}
	if !strings.Contains(hashCitation.URI, "%23") {
		t.Fatalf("resource URI did not encode literal #: %s", hashCitation.URI)
	}
	hashMetadata, ok := hashRead["metadata"].(RetrievalMetadata)
	if !ok {
		t.Fatalf("read returned unexpected metadata type: %#v", hashRead["metadata"])
	}
	assertAdversarialRetrievalIdentity(t, hashMetadata, generation, selected)
	hashReplay, err := service.Read(ctx, ReadOptions{URI: hashCitation.URI})
	if err != nil {
		t.Fatalf("encoded path URI did not round-trip: %v", err)
	}
	if hashReplay["passage"] != hashRead["passage"] {
		t.Fatal("encoded # path URI resolved different content")
	}
}

func TestAdversarialReadRequiresBoundedRangeForLargeSources(t *testing.T) {
	root := makeAdversarialProject(t)
	const relative = "docs/playbooks/large-source.md"
	content := "# Large source\n\n" + strings.Repeat(
		"bounded-read-payload-0123456789\n", 80000)
	if len(content) < 2*1024*1024 || int64(len(content)) >= maxSourceBytes {
		t.Fatalf("large read fixture has wrong size: %d", len(content))
	}
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)), content)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	if _, err := service.Read(ctx, ReadOptions{Path: relative}); err == nil {
		t.Fatal("whole-path read emitted a multi-megabyte indexed source")
	}
	bounded, err := service.Read(ctx, ReadOptions{
		Path: relative, StartLine: 3, EndLine: 5,
	})
	if err != nil {
		t.Fatalf("bounded line-range read failed: %v", err)
	}
	passage, ok := bounded["passage"].(string)
	if !ok || passage != strings.TrimSuffix(
		strings.Repeat("bounded-read-payload-0123456789\n", 3), "\n") {
		t.Fatalf("bounded read returned unexpected passage: %#v", bounded["passage"])
	}
	citation, ok := bounded["citation"].(Citation)
	if !ok || citation.Path != relative || citation.StartLine != 3 ||
		citation.EndLine != 5 || citation.PassageHash != SHA256String(passage) ||
		citation.ContentHash != citation.PassageHash ||
		!hexDigestRE.MatchString(citation.SourceHash) {
		t.Fatalf("bounded read citation is incomplete: %#v", bounded["citation"])
	}

	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		toolCallMessage(2, "read", map[string]any{
			"selector": "path", "value": relative,
		}),
		toolCallMessage(3, "read", map[string]any{
			"selector": "path", "value": relative,
			"startLine": 3, "endLine": 5, "tokenBudget": 1024,
		}),
	)
	assertToolError(t, rpcResponseByID(t, messages, 2), "range")
	mcpRead := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	exactContent, ok := mcpRead["content"].(string)
	if !ok || exactContent != passage {
		t.Fatalf("MCP bounded read disagrees with service read: %#v", mcpRead)
	}
	if mcpRead["selector"] != "path" || mcpRead["handle"] != "path:"+relative ||
		mcpRead["path"] != relative || mcpRead["startLine"] != float64(3) ||
		mcpRead["endLine"] != float64(5) ||
		mcpRead["sha256"] != "sha256:"+citation.SourceHash || mcpRead["digest"] == "" {
		t.Fatalf("MCP bounded exact read metadata is incomplete: %#v", mcpRead)
	}
}

func TestAdversarialContextPackMaterializationAndTamperDefense(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	pack, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "recruiting-run", CandidateSlug: "candidate-one",
			RecruitingRunID: "20260726T010203Z",
		},
		Task: "engine frame checksum", Role: "drafter",
		Tiers: []string{"truth"}, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := ".re-discipline/agents/recruiting/candidate-one/runs/20260726T010203Z/context-pack.json"
	if err := os.MkdirAll(filepath.Join(root, ".re-discipline", "agents", "recruiting", "candidate-one", "runs", "20260726T010203Z"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.MaterializeContextPack(valid, pack); err != nil {
		t.Fatalf("valid drafter materialization failed: %v", err)
	}
	absolute := filepath.Join(root, filepath.FromSlash(valid))
	if _, err := VerifyContextPack(absolute); err != nil {
		t.Fatalf("materialized context pack failed verification: %v", err)
	}
	if err := service.MaterializeContextPack(valid, pack); err != nil {
		t.Fatalf("identical context pack materialization was not idempotent: %v", err)
	}
	different := pack
	different.Task += " changed"
	different, err = finalizeContextPack(different)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MaterializeContextPack(valid, different); err == nil {
		t.Fatal("materializer overwrote an immutable context pack")
	}

	tampered := pack
	tampered.Task += " tampered without digest update"
	if err := service.MaterializeContextPack(valid, tampered); err == nil {
		t.Fatal("materializer wrote a context pack whose digest did not match its body")
	}

	for _, invalid := range []string{
		"active/fixture-campaign/context-pack.json",
		"active/fixture-campaign/runs/not-a-run/not-context-pack.json",
		"active/Fixture Campaign/runs/R-20260802-0004/context-pack.json",
		"active/fixture-campaign/subagents/run-04/context-pack.json",
		".re-discipline/agents/recruiting/candidate-one/context-pack.json",
		"docs/context-pack.json",
		"../context-pack.json",
	} {
		if err := service.MaterializeContextPack(invalid, pack); err == nil {
			t.Errorf("materializer accepted unmanaged target %q", invalid)
		}
	}

	outside := t.TempDir()
	link := filepath.Join(root, "active", "fixture-campaign", "runs", "R-20260802-0005")
	if makeDirectoryLink(t, outside, link) {
		if err := service.MaterializeContextPack(
			"active/fixture-campaign/runs/R-20260802-0005/context-pack.json", pack,
		); err == nil {
			t.Fatal("materializer escaped through a symlink or junction")
		}
	}

	outsidePack := filepath.Join(outside, "outside-context-pack.json")
	outsideBody, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	outsideBody = append(outsideBody, '\n')
	if err := os.WriteFile(outsidePack, outsideBody, 0o600); err != nil {
		t.Fatal(err)
	}
	fileLinkRelative := "active/fixture-campaign/runs/R-20260802-0006/context-pack.json"
	fileLink := filepath.Join(root, filepath.FromSlash(fileLinkRelative))
	if makeFileLink(t, outsidePack, fileLink) {
		if err := service.MaterializeContextPack(fileLinkRelative, pack); err == nil {
			t.Fatal("materializer accepted an outside valid context pack through a file symlink")
		}
		after, err := os.ReadFile(outsidePack)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, outsideBody) {
			t.Fatal("materializer mutated the outside file behind a target symlink")
		}
	}

	collisionParent := filepath.Join(
		root, "active", "fixture-campaign", "runs", "R-20260802-0007",
	)
	collisionTarget := filepath.Join(collisionParent, "context-pack.json")
	if err := os.MkdirAll(collisionTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.MaterializeContextPack(
		"active/fixture-campaign/runs/R-20260802-0007/context-pack.json", pack,
	); err == nil {
		t.Fatal("materializer accepted a failed exclusive publish")
	}
	entries, err := os.ReadDir(collisionParent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".re-discipline-exclusive-") {
			t.Fatalf("failed context-pack publish left temporary artifact %q", entry.Name())
		}
	}
	if info, err := os.Stat(collisionTarget); err != nil || !info.IsDir() {
		t.Fatal("failed context-pack publish changed its collision target")
	}

	body, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	raw["task"] = "post-write tamper"
	writeTestJSON(t, absolute, raw)
	if _, err := VerifyContextPack(absolute); err == nil {
		t.Fatal("context pack tampering was not detected")
	}
}

func TestAdversarialRecallWritesProposalsOnlyAndPendingIsNeverRetrieved(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()
	topicPath := filepath.Join(root, ".re-discipline", "memory", "topics", "navigation.md")
	truthPath := filepath.Join(root, filepath.FromSlash(adversarialEnginePath))
	topicBefore, _ := os.ReadFile(topicPath)
	truthBefore, _ := os.ReadFile(truthPath)

	result, err := service.RecallPropose(
		ctx,
		"Candidate workflow shortcut",
		"proposal-only-marker-theta must remain provisional.",
		[]string{adversarialEnginePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := result["path"].(string)
	if !strings.HasPrefix(path, ".re-discipline/memory/proposals/") || result["created"] != true {
		t.Fatalf("proposal escaped its queue: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	again, err := service.RecallPropose(
		ctx,
		"Candidate workflow shortcut",
		"proposal-only-marker-theta must remain provisional.",
		[]string{adversarialEnginePath},
	)
	if err != nil {
		t.Fatal(err)
	}
	if again["created"] != false || again["path"] != path {
		t.Fatalf("identical proposal was not idempotent: %#v", again)
	}
	topicAfter, _ := os.ReadFile(topicPath)
	truthAfter, _ := os.ReadFile(truthPath)
	if string(topicBefore) != string(topicAfter) || string(truthBefore) != string(truthAfter) {
		t.Fatal("proposal mutated accepted memory or canonical truth")
	}
	inventory, err := service.Index.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range inventory.Documents {
		if strings.HasPrefix(document.Path, ".re-discipline/memory/proposals/") {
			t.Fatalf("pending proposal entered normal inventory: %s", document.Path)
		}
	}
	search, err := service.Search(ctx, SearchOptions{
		Query: "proposal-only-marker-theta", QueryClass: "exact",
		AllowedTiers: []string{"memory"}, Limit: 12, TokenBudget: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range search.Results {
		if strings.Contains(row.Passage, "proposal-only-marker-theta") ||
			strings.HasPrefix(row.Citation.Path, ".re-discipline/memory/proposals/") {
			t.Fatal("pending proposal was retrievable as accepted memory")
		}
	}

	rootNative := makeAdversarialProject(t)
	config := DefaultBootstrapConfig()
	config.Memory.Mode = "native"
	writeTestJSON(t, filepath.Join(rootNative, ".re-discipline", "config.json"), config)
	nativeService := newAdversarialService(t, rootNative, nil)
	if _, err := nativeService.RecallPropose(ctx, "Rejected", "must not write", nil); err == nil {
		t.Fatal("native-only policy accepted a shared-memory proposal")
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func writeCalibrationCases(t *testing.T, root string, cases []EvalCase) {
	t.Helper()
	writeTestJSON(
		t,
		filepath.Join(root, ".re-discipline", "knowledge", "evals", "adversarial.json"),
		cases,
	)
}

func TestAdversarialEvaluationCasesAreStrictAndSplitByTopic(t *testing.T) {
	t.Run("unknown case fields are rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "case.json")
		writeTestFile(t, path, `{
		  "id":"case-one",
		  "role":"manager",
		  "topic":"topic-one",
		  "split":"development",
		  "query":"A1B2C3D4",
		  "queryClass":"exact",
		  "allowedTiers":["truth"],
		  "corpusSnapshot":"fixture:adversarial",
		  "expectedPaths":["docs/truth/findings/fixture-truth/F-9001.md"],
		  "minimumEvidencePaths":["docs/truth/findings/fixture-truth/F-9001.md"],
		  "hardNegativePaths":[],
		  "expectedCitations":["docs/truth/findings/fixture-truth/F-9001.md"],
		  "forbiddenTiers":["history"],
		  "tokenBudget":512,
		  "answerable":true,
		  "selfAssertedGold":true
		}`)
		if _, err := LoadEvalCases(path); err == nil {
			t.Fatal("evaluation loader accepted an unknown gold-label field")
		}
	})

	t.Run("invalid split is rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "case.json")
		writeTestJSON(t, path, EvalCase{
			ID: "case-one", Role: "manager", Topic: "topic-one", Split: "random",
			Query: "A1B2C3D4", QueryClass: "exact", AllowedTiers: []string{"truth"},
			CorpusSnapshot: "fixture:adversarial", ExpectedPaths: []string{adversarialEnginePath},
			MinimumEvidencePaths: []string{adversarialEnginePath},
			ExpectedCitations:    []string{adversarialEnginePath},
			ForbiddenTiers:       []string{"history"}, TokenBudget: 512,
			Answerable: boolPointer(true),
		})
		if _, err := LoadEvalCases(path); err == nil {
			t.Fatal("evaluation loader accepted an unrecognized split")
		}
	})

	t.Run("development and holdout cannot share a topic", func(t *testing.T) {
		root := makeAdversarialProject(t)
		writeCalibrationCases(t, root, []EvalCase{
			{
				ID: "dev-one", Role: "manager", Topic: "leaked-topic", Split: "development",
				Query: "A1B2C3D4", QueryClass: "exact", AllowedTiers: []string{"truth"},
				CorpusSnapshot: "fixture:adversarial", ExpectedPaths: []string{adversarialEnginePath},
				MinimumEvidencePaths: []string{adversarialEnginePath},
				ExpectedCitations:    []string{adversarialEnginePath}, TokenBudget: 512,
				Answerable: boolPointer(true),
			},
			{
				ID: "holdout-one", Role: "manager", Topic: "leaked-topic", Split: "holdout",
				Query: "engine frame", QueryClass: "conceptual", AllowedTiers: []string{"truth"},
				CorpusSnapshot: "fixture:adversarial", ExpectedPaths: []string{adversarialEnginePath},
				MinimumEvidencePaths: []string{adversarialEnginePath},
				ExpectedCitations:    []string{adversarialEnginePath}, TokenBudget: 512,
				Answerable: boolPointer(true),
			},
		})
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("development/holdout topic leakage was accepted")
		}
	})

	t.Run("packaged suite contains isolated development and holdout cases", func(t *testing.T) {
		cases, err := LoadEvalCases(filepath.Join(
			adversarialAssetRoot(t), "evals", "conformance", "cases.json",
		))
		if err != nil {
			t.Fatal(err)
		}
		splits := map[string]bool{}
		topics := map[string]string{}
		roleSplits := map[string]bool{}
		queryClasses := map[string]bool{}
		budgets := map[int]bool{}
		hardNegativeCases := 0
		hasMultiSource := false
		hasAbstention := false
		hasAuthorityPolicy := false
		hasAcceptedVersusPendingMemory := false
		for _, eval := range cases {
			splits[eval.Split] = true
			roleSplits[eval.Role+"/"+eval.Split] = true
			queryClasses[eval.QueryClass] = true
			budgets[eval.TokenBudget] = true
			if previous, ok := topics[eval.Topic]; ok && previous != eval.Split {
				t.Fatalf("topic %q leaks between %s and %s", eval.Topic, previous, eval.Split)
			}
			topics[eval.Topic] = eval.Split
			if len(eval.HardNegativePaths) > 0 {
				hardNegativeCases++
			}
			if len(eval.MinimumEvidencePaths) >= 2 {
				hasMultiSource = true
			}
			if eval.Answerable != nil && !*eval.Answerable {
				hasAbstention = true
			}
			if len(eval.ForbiddenTiers) > 0 {
				hasAuthorityPolicy = true
			}
			acceptedMemory := false
			pendingMemory := false
			for _, path := range eval.ExpectedPaths {
				if strings.HasPrefix(path, ".re-discipline/memory/topics/") {
					acceptedMemory = true
				}
			}
			for _, path := range eval.HardNegativePaths {
				if strings.HasPrefix(path, ".re-discipline/memory/proposals/") {
					pendingMemory = true
				}
			}
			if acceptedMemory && pendingMemory {
				hasAcceptedVersusPendingMemory = true
			}
		}
		if len(cases) < 12 || len(topics) < 8 {
			t.Fatalf("packaged seed corpus is too small: cases=%d topics=%d", len(cases), len(topics))
		}
		if !splits["development"] || !splits["holdout"] {
			t.Fatalf("packaged suite lacks a development/holdout split: %v", splits)
		}
		for _, combination := range []string{
			"manager/development", "manager/holdout",
			"drafter/development", "drafter/holdout",
		} {
			if !roleSplits[combination] {
				t.Errorf("packaged suite lacks %s coverage", combination)
			}
		}
		for _, queryClass := range []string{"exact", "conceptual", "dependency", "current"} {
			if !queryClasses[queryClass] {
				t.Errorf("packaged suite lacks %s query coverage", queryClass)
			}
		}
		for _, budget := range []int{512, 1024, 2048, 4096} {
			if !budgets[budget] {
				t.Errorf("packaged suite lacks a %d-token case", budget)
			}
		}
		if hardNegativeCases < 2 || !hasMultiSource || !hasAbstention ||
			!hasAuthorityPolicy || !hasAcceptedVersusPendingMemory {
			t.Fatalf(
				"packaged suite lacks adversarial adequacy: hardNegativeCases=%d multiSource=%v abstention=%v authority=%v acceptedVsPendingMemory=%v",
				hardNegativeCases, hasMultiSource, hasAbstention,
				hasAuthorityPolicy, hasAcceptedVersusPendingMemory,
			)
		}
	})
}

func TestAdversarialPackagedBenchmarkDigestAndMetrics(t *testing.T) {
	cases, err := LoadEvalCases(filepath.Join(
		adversarialAssetRoot(t), "evals", "conformance", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunPackagedBenchmark(context.Background(), adversarialAssetRoot(t), "full")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Profiles) != 2 {
		t.Fatalf("full packaged dense/fallback benchmark failed: %#v", report)
	}
	digests := map[string]bool{}
	for _, profile := range report.Profiles {
		if !profile.Passed || !profile.DigestVerified ||
			profile.DeclaredDigest != profile.ComputedDigest ||
			!strings.HasPrefix(profile.ComputedDigest, "sha256:") ||
			len(profile.ComputedDigest) != 71 {
			t.Fatalf("profile lacks verified benchmark evidence: %#v", profile)
		}
		if digests[profile.ComputedDigest] {
			t.Fatalf("capability profiles share benchmark evidence: %s", profile.ComputedDigest)
		}
		digests[profile.ComputedDigest] = true
		metrics := profile.Metrics
		for name, value := range map[string]float64{
			"recallAtK":               metrics.RecallAtK,
			"meanReciprocalRank":      metrics.MeanReciprocalRank,
			"nDCG":                    metrics.NDCG,
			"precisionAtK":            metrics.PrecisionAtK,
			"relevantTokenRatio":      metrics.RelevantTokenRatio,
			"duplicateTokenRatio":     metrics.DuplicateTokenRatio,
			"deterministicReplayRate": metrics.DeterministicReplayRate,
		} {
			if value < 0 || value > 1 {
				t.Errorf("%s metric is outside [0,1]: %f", name, value)
			}
		}
		if metrics.AuthorityViolations != 0 || metrics.DeterministicReplayRate != 1 {
			t.Fatalf("hard authority/replay gate failed: %#v", metrics)
		}
		if len(profile.MetricsByBudget) == 0 {
			t.Fatal("benchmark omitted token-budget-stratified metrics")
		}
		if !profile.ContextPackPassed ||
			!contains(profile.ContextPackRoles, "manager") ||
			!contains(profile.ContextPackRoles, "drafter") {
			t.Fatalf("benchmark omitted cross-role context-pack gates: %#v", profile)
		}
		assertAdversarialContextPackOutcomes(t, profile.ContextPackCases, cases, 0)
		for _, budget := range []int{512, 1024, 2048, 4096} {
			key := strconv.Itoa(budget)
			assertAdversarialContextPackOutcomes(
				t, profile.ContextPacksByBudget[key], cases, budget)
		}
		if len(profile.ContextPacksByBudget) != 4 ||
			len(profile.QualityMetricsByBudget) != 4 {
			t.Fatalf("full benchmark omitted context/quality budget matrices: %#v", profile)
		}
		for _, outcome := range profile.Cases {
			if !outcome.AuthoritySafe || !outcome.BudgetSafe || !outcome.ReplayIdentical {
				t.Fatalf("benchmark case failed a hard gate: %#v", outcome)
			}
		}
	}
	reportBody, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(reportBody, &raw); err != nil {
		t.Fatal(err)
	}
	rawProfiles := raw["profiles"].([]any)
	for _, value := range rawProfiles {
		rawProfile := value.(map[string]any)
		bySplit := map[string]any{}
		if declared, ok := rawProfile["metricsBySplit"].(map[string]any); ok {
			bySplit = declared
		} else {
			if development, ok := rawProfile["developmentMetrics"].(map[string]any); ok {
				bySplit["development"] = development
			}
			if holdout, ok := rawProfile["holdoutMetrics"].(map[string]any); ok {
				bySplit["holdout"] = holdout
			}
		}
		for _, split := range []string{"development", "holdout"} {
			metrics, ok := bySplit[split].(map[string]any)
			if !ok {
				t.Fatalf("%s omitted %s metrics", rawProfile["profileName"], split)
			}
			for _, field := range []string{
				"recallAtK", "meanReciprocalRank", "nDCG", "precisionAtK",
				"authorityViolations", "relevantTokenRatio",
				"duplicateTokenRatio", "deterministicReplayRate",
				"p50LatencyMillis", "p95LatencyMillis",
			} {
				if _, ok := metrics[field]; !ok {
					t.Fatalf("%s %s metrics omitted %s", rawProfile["profileName"], split, field)
				}
			}
		}
		holdout := bySplit["holdout"].(map[string]any)
		if holdout["recallAtK"].(float64) < 0 || holdout["recallAtK"].(float64) > 1 {
			t.Fatalf("%s holdout recall is outside [0,1]", rawProfile["profileName"])
		}
	}
}

func TestAdversarialCalibrationUsesDevelopmentThenFrozenHoldoutWithoutActivation(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	generation, _, _, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeCalibrationCases(t, root, []EvalCase{
		{
			ID: "development-dependency", Role: "manager", Topic: "development-dependency",
			Split: "development", Query: "consumer depends-on portability", QueryClass: "dependency",
			AllowedTiers: []string{"truth"}, CorpusSnapshot: generation.CorpusFingerprint,
			ExpectedPaths: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			MinimumEvidencePaths: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			ExpectedCitations: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			ForbiddenTiers: []string{"history"}, TokenBudget: 1024,
			Answerable: boolPointer(true),
		},
		{
			ID: "holdout-exact", Role: "manager", Topic: "holdout-engine",
			Split: "holdout", Query: "A1B2C3D4", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, CorpusSnapshot: generation.CorpusFingerprint,
			ExpectedPaths:        []string{adversarialEnginePath},
			MinimumEvidencePaths: []string{adversarialEnginePath},
			ExpectedCitations:    []string{adversarialEnginePath},
			ForbiddenTiers:       []string{"history"}, TokenBudget: 512,
			Answerable: boolPointer(true),
		},
	})
	profilePath := filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")
	before, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Calibrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || report.Activated ||
		report.ActiveBefore == "" || report.ActiveBefore != report.ActiveAfter {
		t.Fatalf("calibration activated or rewrote production profile: %#v", report)
	}
	if len(report.Candidates) != 27 {
		t.Fatalf("calibration evaluated %d candidates, want the contractual 27", len(report.Candidates))
	}
	denseWeights := map[int]bool{}
	gridRows := map[string]bool{}
	for _, candidate := range report.Candidates {
		if len(candidate.Weights) != 4 {
			t.Fatalf("calibration candidate has an unsupported weight set: %#v", candidate.Weights)
		}
		exact, fts, graph, dense := candidate.Weights["exact"], candidate.Weights["fts"],
			candidate.Weights["graph"], candidate.Weights["dense"]
		if !map[int]bool{6: true, 8: true, 10: true}[exact] ||
			!map[int]bool{4: true, 6: true, 8: true}[fts] ||
			!map[int]bool{1: true, 2: true, 3: true}[graph] || dense <= 0 {
			t.Fatalf("calibration candidate is outside the contractual grid: %#v", candidate.Weights)
		}
		denseWeights[dense] = true
		gridRows[fmt.Sprintf("%d/%d/%d", exact, fts, graph)] = true
	}
	if len(denseWeights) != 1 || len(gridRows) != 27 {
		t.Fatalf("calibration tuned lane inventory instead of the 27-row RRF grid: dense=%v rows=%d", denseWeights, len(gridRows))
	}
	if report.Recommended.DevelopmentHit <= report.Recommended.HoldoutHit {
		t.Fatalf("development and holdout were not evaluated separately: %#v", report.Recommended)
	}
	if strings.Contains(
		filepath.Clean(report.CandidatePath),
		filepath.Clean(filepath.Join(root, ".re-discipline", "knowledge")),
	) {
		t.Fatalf("candidate profile was written into active tracked settings: %s", report.CandidatePath)
	}
	body, err := os.ReadFile(filepath.FromSlash(report.CandidatePath))
	if err != nil {
		t.Fatal(err)
	}
	var candidate RetrievalProfile
	if err := decodeStrict(body, &candidate); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfile(candidate); err != nil {
		t.Fatalf("calibration did not emit a full independently testable capability matrix: %v", err)
	}
	reportBody, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(reportBody, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["paretoFrontier"]; !ok {
		t.Fatal("calibration report omitted an explicit Pareto frontier")
	}
	if strings.Contains(string(reportBody), `"score"`) {
		t.Fatal("calibration hid quality/authority/token tradeoffs in a single aggregate score")
	}
	recommended, ok := raw["recommended"].(map[string]any)
	if !ok {
		t.Fatal("calibration report omitted a measured recommendation")
	}
	for _, split := range []string{"developmentMetrics", "holdoutMetrics"} {
		metrics, ok := recommended[split].(map[string]any)
		if !ok {
			t.Fatalf("recommended candidate omitted %s", split)
		}
		for _, field := range []string{
			"recallAtK", "meanReciprocalRank", "nDCG", "precisionAtK",
			"authorityViolations", "relevantTokenRatio",
			"duplicateTokenRatio", "deterministicReplayRate",
			"p50LatencyMillis", "p95LatencyMillis",
		} {
			if _, ok := metrics[field]; !ok {
				t.Fatalf("%s omitted %s", split, field)
			}
		}
	}
	frontier, ok := raw["paretoFrontier"].([]any)
	if !ok || len(frontier) == 0 {
		t.Fatal("calibration emitted an empty Pareto frontier")
	}
}

func TestAdversarialProfilePromotionAuthenticatesCandidateAndRecomputesEvidence(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	generation, _, _, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeCalibrationCases(t, root, []EvalCase{
		{
			ID: "development-promotion", Role: "manager", Topic: "promotion-development",
			Split: "development", Query: "consumer depends-on portability",
			QueryClass: "dependency", AllowedTiers: []string{"truth"},
			CorpusSnapshot: generation.CorpusFingerprint,
			ExpectedPaths: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			MinimumEvidencePaths: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			ExpectedCitations: []string{
				adversarialConsumerPath, adversarialPortabilityPath,
			},
			ForbiddenTiers: []string{"history"}, TokenBudget: 1024,
			Answerable: boolPointer(true),
		},
		{
			ID: "holdout-promotion", Role: "drafter", Topic: "promotion-holdout",
			Split: "holdout", Query: "A1B2C3D4", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, CorpusSnapshot: generation.CorpusFingerprint,
			ExpectedPaths:        []string{adversarialEnginePath},
			MinimumEvidencePaths: []string{adversarialEnginePath},
			ExpectedCitations:    []string{adversarialEnginePath},
			ForbiddenTiers:       []string{"history"}, TokenBudget: 512,
			Answerable: boolPointer(true),
		},
	})
	report, err := service.Calibrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.FromSlash(report.CandidatePath)
	reportPath := filepath.Join(filepath.Dir(candidatePath), "report.json")
	candidateOriginal, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	reportOriginal, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")
	activeOriginal, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	resetArtifacts := func(t *testing.T) {
		t.Helper()
		if err := os.WriteFile(candidatePath, candidateOriginal, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reportPath, reportOriginal, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(activePath, activeOriginal, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expectRejected := func(t *testing.T) {
		t.Helper()
		if _, err := service.PromoteProfile(
			context.Background(), candidatePath, reportPath, true); err == nil {
			t.Error("promotion accepted unauthenticated or stale calibration evidence")
		}
		activeAfter, err := os.ReadFile(activePath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(activeAfter, activeOriginal) {
			t.Error("rejected promotion changed the active retrieval profile")
		}
	}
	readCandidate := func(t *testing.T) RetrievalProfile {
		t.Helper()
		var candidate RetrievalProfile
		if err := decodeStrict(candidateOriginal, &candidate); err != nil {
			t.Fatal(err)
		}
		return candidate
	}

	t.Run("explicit approval is authority but cannot replace evidence", func(t *testing.T) {
		resetArtifacts(t)
		if _, err := service.PromoteProfile(
			context.Background(), candidatePath, reportPath, false); err == nil {
			t.Fatal("promotion without explicit user authority was accepted")
		}
	})

	t.Run("calibrated row weight mutation invalidates the report", func(t *testing.T) {
		resetArtifacts(t)
		candidate := readCandidate(t)
		changed := false
		for index := range candidate.EffectiveProfiles {
			row := &candidate.EffectiveProfiles[index]
			if row.Benchmark.Suite == "project-calibration-v1" {
				row.Weights["exact"]++
				changed = true
			}
		}
		if !changed {
			t.Fatal("calibration candidate omits its project-calibrated row")
		}
		writeTestJSON(t, candidatePath, candidate)
		expectRejected(t)
	})

	t.Run("top-level candidate mutation invalidates the report", func(t *testing.T) {
		resetArtifacts(t)
		candidate := readCandidate(t)
		candidate.Description += " unauthenticated mutation"
		writeTestJSON(t, candidatePath, candidate)
		expectRejected(t)
	})

	t.Run("forged metrics and hard-gate assertions are recomputed", func(t *testing.T) {
		resetArtifacts(t)
		var forged CalibrationReport
		if err := decodeStrict(reportOriginal, &forged); err != nil {
			t.Fatal(err)
		}
		forged.Recommended.DevelopmentMetrics.RecallAtK = -1
		forged.Recommended.HoldoutMetrics.NDCG = -1
		forged.Recommended.Violations += 999
		forged.Recommended.HardGatesPassed = true
		forged.Recommended.Weights["exact"] += 99
		writeTestJSON(t, reportPath, forged)
		expectRejected(t)
	})

	t.Run("unaltered candidate is promoted only after fresh evidence verification", func(t *testing.T) {
		resetArtifacts(t)
		result, err := service.PromoteProfile(
			context.Background(), candidatePath, reportPath, true)
		if err != nil {
			t.Fatalf("fresh authenticated promotion was rejected: %v", err)
		}
		if !result.Activated || !sha256IdentityRE.MatchString(result.ProfileDigest) ||
			!sha256IdentityRE.MatchString(result.ReportDigest) {
			t.Fatalf("promotion result lacks authenticated identities: %#v", result)
		}
		active, err := os.ReadFile(activePath)
		if err != nil {
			t.Fatal(err)
		}
		var promoted RetrievalProfile
		if err := decodeStrict(active, &promoted); err != nil {
			t.Fatal(err)
		}
		if err := ValidateProjectProfileApproval(promoted); err != nil {
			t.Fatalf("promoted profile receipt is invalid: %v", err)
		}
		recomputeReceipt := func(t *testing.T, profile *RetrievalProfile) {
			t.Helper()
			profile.Approval["profileDigest"] = ""
			digest, err := approvedProfileDigest(*profile)
			if err != nil {
				t.Fatal(err)
			}
			profile.Approval["profileDigest"] = digest
		}
		withExtra := cloneProfile(t, promoted)
		withExtra.Approval["query"] = "receipt-must-never-carry-user-input"
		recomputeReceipt(t, &withExtra)
		if err := ValidateProjectProfileApproval(withExtra); err == nil {
			t.Fatal("approval validator accepted a content-hashed extra receipt field")
		}
		withMissing := cloneProfile(t, promoted)
		delete(withMissing.Approval, "candidateDigest")
		recomputeReceipt(t, &withMissing)
		if err := ValidateProjectProfileApproval(withMissing); err == nil {
			t.Fatal("approval validator accepted a content-hashed incomplete receipt")
		}
		withNestedExtra := cloneProfile(t, promoted)
		runtimeContract, ok := withNestedExtra.Approval["runtimeContract"].(map[string]any)
		if !ok {
			t.Fatalf("approval runtime contract has unexpected type: %#v",
				withNestedExtra.Approval["runtimeContract"])
		}
		runtimeContract["query"] = "nested-extra"
		recomputeReceipt(t, &withNestedExtra)
		if err := ValidateProjectProfileApproval(withNestedExtra); err == nil {
			t.Fatal("approval validator accepted an extra nested runtime field")
		}
	})
}

func TestAdversarialSortedCandidateTieBreakIsStable(t *testing.T) {
	makeRow := func(path, id string) *candidate {
		return &candidate{
			Chunk:     Chunk{ID: id, Path: path, StartLine: 1},
			LaneRanks: map[string]int{"exact": 1},
		}
	}
	rows := map[string]*candidate{
		"z": makeRow("docs/truth/z.md", "z"),
		"a": makeRow("docs/truth/a.md", "a"),
		"b": makeRow("docs/truth/a.md", "b"),
	}
	ranked := sortedCandidates(rows, map[string]int{"exact": 1}, 60)
	got := []string{}
	for _, row := range ranked {
		got = append(got, row.Chunk.ID)
	}
	want := []string{"a", "b", "z"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deterministic tie break = %v, want %v", got, want)
	}
}

func TestAdversarialRetrievalProfileRuntimeEnforcesSchemaBoundsAndSafeIDs(t *testing.T) {
	baseline, _ := readCatalogAndManifest(t)
	if err := ValidateProfile(baseline); err != nil {
		t.Fatalf("packaged retrieval profile is not a valid mutation baseline: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RetrievalProfile)
	}{
		{
			name: "empty top-level description",
			mutate: func(profile *RetrievalProfile) {
				profile.Description = ""
			},
		},
		{
			name: "traversing profile ID",
			mutate: func(profile *RetrievalProfile) {
				profile.ProfileID = "project:../../escape"
			},
		},
		{
			name: "unsafe effective profile name",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Name = "../escape"
			},
		},
		{
			name: "traversing base profile ID",
			mutate: func(profile *RetrievalProfile) {
				profile.BaseProfile = "../escape"
			},
		},
		{
			name: "rrfK above schema maximum",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].RRFK = 1001
			},
		},
		{
			name: "document cap above schema maximum",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].MaxPerDocument = 21
			},
		},
		{
			name: "passage cap above schema maximum",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Packing.MaxPassages = 51
			},
		},
		{
			name: "byte cap above schema maximum",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Packing.MaxBytes = 262145
			},
		},
		{
			name: "passed benchmark missing evaluated timestamp",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Benchmark.EvaluatedAt = ""
			},
		},
		{
			name: "passed benchmark missing eval fingerprint",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Benchmark.EvalFingerprint = ""
			},
		},
		{
			name: "passed benchmark missing corpus fingerprint",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Benchmark.CorpusFingerprint = ""
			},
		},
		{
			name: "passed benchmark missing model fingerprint",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Benchmark.ModelFingerprint = ""
			},
		},
		{
			name: "passed benchmark missing runtime fingerprint",
			mutate: func(profile *RetrievalProfile) {
				profile.EffectiveProfiles[0].Benchmark.RuntimeFingerprint = ""
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			profile := cloneProfile(t, baseline)
			testCase.mutate(&profile)
			if err := ValidateProfile(profile); err == nil {
				t.Fatal("runtime accepted a retrieval profile rejected by its durable schema")
			}
		})
	}
}

func TestAdversarialModelManifestCannotEscapeThroughDirectoryLink(t *testing.T) {
	assetRoot := t.TempDir()
	outsideModels := filepath.Join(t.TempDir(), "models")
	copyTestFile(t,
		filepath.Join(adversarialAssetRoot(t), "models", "manifest.json"),
		filepath.Join(outsideModels, "manifest.json"))
	if !makeDirectoryLink(t, outsideModels, filepath.Join(assetRoot, "models")) {
		t.Skip("directory links are unavailable")
	}
	if _, err := LoadModelManifest(assetRoot); err == nil {
		t.Fatal("model manifest loader followed a linked models directory outside assetRoot")
	}
}

func loadAdversarialSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(adversarialAssetRoot(t), "schemas", name))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := decodeStrict(body, &schema); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	return schema
}

func adversarialSchemaMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("schema field %q is not an object: %#v", key, parent[key])
	}
	return value
}

func adversarialSchemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	return adversarialSchemaMap(t, schema, "properties")
}

func adversarialSchemaRequired(t *testing.T, schema map[string]any) map[string]bool {
	t.Helper()
	raw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema object has no required array: %#v", schema)
	}
	result := make(map[string]bool, len(raw))
	for _, value := range raw {
		name, ok := value.(string)
		if !ok || name == "" || result[name] {
			t.Fatalf("schema required array is malformed: %#v", raw)
		}
		result[name] = true
	}
	return result
}

func assertAdversarialClosedSchemaObject(
	t *testing.T,
	label string,
	schema map[string]any,
	required []string,
	optional []string,
) map[string]any {
	t.Helper()
	if schema["type"] != "object" {
		t.Fatalf("%s must be an object schema, got %#v", label, schema["type"])
	}
	if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
		t.Fatalf("%s must set additionalProperties:false", label)
	}
	properties := adversarialSchemaProperties(t, schema)
	expected := make(map[string]bool, len(required)+len(optional))
	for _, name := range append(append([]string{}, required...), optional...) {
		expected[name] = true
		if _, ok := properties[name]; !ok {
			t.Errorf("%s omits property %q", label, name)
		}
	}
	for name := range properties {
		if !expected[name] {
			t.Errorf("%s exposes undeclared runtime field %q", label, name)
		}
	}
	gotRequired := adversarialSchemaRequired(t, schema)
	for _, name := range required {
		if !gotRequired[name] {
			t.Errorf("%s does not require runtime field %q", label, name)
		}
	}
	for _, name := range optional {
		if gotRequired[name] {
			t.Errorf("%s incorrectly requires omitempty field %q", label, name)
		}
	}
	for name := range gotRequired {
		if !expected[name] {
			t.Errorf("%s requires unknown runtime field %q", label, name)
		}
	}
	return properties
}

func assertAdversarialSchemaPattern(
	t *testing.T,
	label string,
	schema map[string]any,
	accepted []string,
	rejected []string,
) {
	t.Helper()
	pattern, ok := schema["pattern"].(string)
	if !ok || pattern == "" {
		t.Fatalf("%s must have a non-empty pattern", label)
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("%s pattern must be Go-compatible for runtime conformance tests: %v", label, err)
	}
	for _, value := range accepted {
		if !compiled.MatchString(value) {
			t.Errorf("%s pattern rejects valid value %q", label, value)
		}
	}
	for _, value := range rejected {
		if compiled.MatchString(value) {
			t.Errorf("%s pattern accepts unsafe value %q", label, value)
		}
	}
}

func TestAdversarialDurableSchemasMatchRuntimeWireContracts(t *testing.T) {
	t.Run("context pack closes every nested wire object", func(t *testing.T) {
		schema := loadAdversarialSchema(t, "context-pack.schema.json")
		root := assertAdversarialClosedSchemaObject(t, "context pack", schema, []string{
			"schemaVersion", "packId", "digest", "task", "scope", "generation",
			"role", "allowedTiers", "requestedProfile", "effectiveProfile",
			"activeLanes", "models", "fallbackReason", "tokenBudget", "estimatedTokens",
			"acceptedConstraints", "cards", "requiredHandles", "omitted",
		}, []string{"writeGrants"})
		writeGrants := adversarialSchemaMap(t, root, "writeGrants")
		if writeGrants["type"] != "array" || writeGrants["maxItems"] != float64(maxRunWriteGrants) {
			t.Fatalf("context-pack write grants are not safely bounded: %#v", writeGrants)
		}
		defs := adversarialSchemaMap(t, schema, "$defs")
		generation := adversarialSchemaMap(t, defs, "generation")
		generationFields := assertAdversarialClosedSchemaObject(
			t, "context pack generation", generation, []string{
				"id", "corpusFingerprint", "modelFingerprint", "runtimeFingerprint",
				"project", "worktree", "gitRevision", "dirtyFingerprint",
				"parserVersion", "chunkerVersion", "createdAt",
			}, nil)
		for _, hash := range []string{
			"corpusFingerprint", "modelFingerprint", "runtimeFingerprint",
		} {
			if adversarialSchemaMap(t, generationFields, hash)["$ref"] != "#/$defs/sha256" {
				t.Errorf("context pack generation %s does not use the canonical digest schema", hash)
			}
		}
		assertAdversarialSchemaPattern(t, "context pack sha256",
			adversarialSchemaMap(t, defs, "sha256"),
			[]string{"sha256:" + strings.Repeat("a", 64)},
			[]string{strings.Repeat("a", 64), "sha256:abc"})

		models := adversarialSchemaMap(t, root, "models")
		if models["type"] != "array" || models["uniqueItems"] != true || models["maxItems"] != float64(1) {
			t.Error("context pack model identities must allow exactly the retained embedding")
		}
		modelItem := adversarialSchemaMap(t, models, "items")
		assertAdversarialSchemaPattern(t, "context pack compact model identity",
			modelItem,
			[]string{
				"builtin:glove-6b-50d-top50k-q8-v1@1",
			},
			[]string{"../escape@1", "builtin:model", "builtin:model@a@b"})

		scope := adversarialSchemaMap(t, defs, "scope")
		scopeFields := assertAdversarialClosedSchemaObject(t, "context pack scope", scope, []string{
			"kind", "stateHeadRevision", "stateHeadDigest",
		}, []string{
			"campaignId", "campaignSlug", "campaignRevision", "workItemId",
			"workItemRevision", "runId", "runRevision", "candidateSlug",
			"recruitingRunId", "eventId",
		})
		if alternatives, ok := scope["oneOf"].([]any); !ok || len(alternatives) != 3 {
			t.Fatal("context pack scope must discriminate project, active-run, and recruiting-run")
		}
		if adversarialSchemaMap(t, root, "scope")["$ref"] != "#/$defs/scope" ||
			adversarialSchemaMap(t, root, "generation")["$ref"] != "#/$defs/generation" {
			t.Fatal("context pack scope and generation must use the closed definitions")
		}
		assertAdversarialSchemaPattern(t, "context pack campaign id",
			adversarialSchemaMap(t, scopeFields, "campaignId"),
			[]string{"C-TEST", "C-STATE-8"}, []string{"test", "C-lower", "../C-TEST"})
		assertAdversarialSchemaPattern(t, "context pack run id",
			adversarialSchemaMap(t, scopeFields, "runId"),
			[]string{"R-20260802-0001"}, []string{"R-1", "run-20260802-0001"})

		constraints := adversarialSchemaMap(t, root, "acceptedConstraints")
		constraint := adversarialSchemaMap(t, defs, "constraint")
		assertAdversarialClosedSchemaObject(t, "context pack accepted constraint", constraint, []string{
			"id", "kind", "text", "sourceHandle",
		}, nil)
		if adversarialSchemaMap(t, constraints, "items")["$ref"] != "#/$defs/constraint" {
			t.Fatal("accepted constraints do not use their closed schema")
		}
		cards := adversarialSchemaMap(t, root, "cards")
		if adversarialSchemaMap(t, cards, "items")["$ref"] != "context-card.schema.json" {
			t.Fatal("context packs must reuse the bounded context-card contract")
		}
		handles := adversarialSchemaMap(t, root, "requiredHandles")
		if handles["uniqueItems"] != true {
			t.Fatal("context-pack required handles must be unique")
		}

		omitted := adversarialSchemaMap(t, root, "omitted")
		omittedFields := assertAdversarialClosedSchemaObject(t, "context pack omissions", omitted, []string{
			"candidateCards", "budget", "cardLimit", "staleSource",
		}, nil)
		for name, raw := range omittedFields {
			field := raw.(map[string]any)
			if field["type"] != "integer" || field["minimum"] != float64(0) {
				t.Errorf("context pack omission %s must be a non-negative integer", name)
			}
		}
	})

	t.Run("profile approval receipt is closed and authenticated", func(t *testing.T) {
		schema := loadAdversarialSchema(t, "retrieval-profile.schema.json")
		root := adversarialSchemaProperties(t, schema)
		approval := adversarialSchemaMap(t, root, "approval")
		fields := assertAdversarialClosedSchemaObject(t, "retrieval profile approval", approval, []string{
			"decision", "explicitUserApproval", "approvedAt", "profileDigest",
			"benchmarkMatrixDigest", "corpusFingerprint", "evalFingerprint",
			"modelFingerprint", "runtimeContract", "calibrationReportDigest", "candidateDigest",
		}, nil)
		if adversarialSchemaMap(t, fields, "decision")["const"] != "promoted" {
			t.Error("approval decision must be the promoted constant")
		}
		if adversarialSchemaMap(t, fields, "explicitUserApproval")["const"] != true {
			t.Error("approval explicitUserApproval must be the true constant")
		}
		for _, name := range []string{
			"profileDigest", "benchmarkMatrixDigest", "corpusFingerprint",
			"evalFingerprint", "modelFingerprint", "calibrationReportDigest", "candidateDigest",
		} {
			assertAdversarialSchemaPattern(t, "approval "+name,
				adversarialSchemaMap(t, fields, name),
				[]string{"sha256:" + strings.Repeat("a", 64)},
				[]string{strings.Repeat("a", 64), "sha256:abc"})
		}
		runtimeContract := adversarialSchemaMap(t, fields, "runtimeContract")
		assertAdversarialClosedSchemaObject(t, "approval runtime contract", runtimeContract, []string{
			"implementation", "version", "goVersion", "compiledBuildId", "sqliteDriver",
			"sqliteVersion", "sqliteBuild", "numericalBackend", "tieBreaker",
		}, nil)

		assertAdversarialSchemaPattern(t, "retrieval profile ID",
			adversarialSchemaMap(t, root, "profileId"),
			[]string{"builtin:balanced-v1", "project:candidate-0123"},
			[]string{
				"../escape", "project:../../escape", "/absolute", "UPPER",
				"project:two:colons", `project\windows-path`,
			})
		assertAdversarialSchemaPattern(t, "retrieval base profile ID",
			adversarialSchemaMap(t, root, "baseProfile"),
			[]string{"builtin:balanced-v1"},
			[]string{"../escape", "project:../../escape", "UPPER"})
		effectiveProfiles := adversarialSchemaMap(t, root, "effectiveProfiles")
		effectiveItem := adversarialSchemaMap(t, effectiveProfiles, "items")
		effectiveFields := adversarialSchemaProperties(t, effectiveItem)
		assertAdversarialSchemaPattern(t, "effective profile name",
			adversarialSchemaMap(t, effectiveFields, "name"),
			[]string{"hybrid-no-rerank-v1", "lexical-graph-v1"},
			[]string{"../escape", "UPPER", "two/slugs", "two:slugs"})

		benchmark := adversarialSchemaMap(t, effectiveFields, "benchmark")
		benchmarkFields := assertAdversarialClosedSchemaObject(
			t, "effective profile benchmark", benchmark,
			[]string{"suite", "digest", "status"},
			[]string{
				"evaluatedAt", "evalFingerprint", "corpusFingerprint",
				"modelFingerprint", "runtimeFingerprint",
				// Segmentation identity distinguishes an actionable re-chunk
				// from an informational corpus edit; the ratified scores are
				// the calibration ratchet's floor.
				"chunkerVersion", "parserVersion",
				"ratifiedHardNegativeHits", "ratifiedAbstentionAccuracy",
			})
		for _, name := range []string{
			"evalFingerprint", "corpusFingerprint",
			"modelFingerprint", "runtimeFingerprint",
		} {
			assertAdversarialSchemaPattern(t, "benchmark "+name,
				adversarialSchemaMap(t, benchmarkFields, name),
				[]string{"sha256:" + strings.Repeat("a", 64)},
				[]string{strings.Repeat("a", 64), "sha256:abc"})
		}
		passedEvidence := []string{
			"evaluatedAt", "evalFingerprint", "corpusFingerprint",
			"modelFingerprint", "runtimeFingerprint",
		}
		globalRequired := adversarialSchemaRequired(t, benchmark)
		globallyRequired := true
		for _, name := range passedEvidence {
			globallyRequired = globallyRequired && globalRequired[name]
		}
		conditionallyRequired := false
		if rules, ok := benchmark["allOf"].([]any); ok {
			for _, rawRule := range rules {
				rule, ok := rawRule.(map[string]any)
				if !ok {
					continue
				}
				condition, ok := rule["if"].(map[string]any)
				if !ok {
					continue
				}
				conditionProperties, ok := condition["properties"].(map[string]any)
				if !ok {
					continue
				}
				status, ok := conditionProperties["status"].(map[string]any)
				if !ok || status["const"] != "passed" {
					continue
				}
				thenSchema, ok := rule["then"].(map[string]any)
				if !ok {
					continue
				}
				thenRequired := adversarialSchemaRequired(t, thenSchema)
				conditionallyRequired = true
				for _, name := range passedEvidence {
					conditionallyRequired =
						conditionallyRequired && thenRequired[name]
				}
			}
		}
		if !globallyRequired && !conditionallyRequired {
			t.Error("passed benchmark schema does not require complete freshness evidence")
		}
	})

	t.Run("model external policy is exact and closed", func(t *testing.T) {
		schema := loadAdversarialSchema(t, "model-manifest.schema.json")
		root := adversarialSchemaProperties(t, schema)
		policy := adversarialSchemaMap(t, root, "externalModelPolicy")
		fields := assertAdversarialClosedSchemaObject(t, "external model policy", policy, []string{
			"networkDownloads", "requireManifestEntry",
			"requireArtifactSha256", "requireLocalPathGrant",
		}, nil)
		expected := map[string]bool{
			"networkDownloads": false, "requireManifestEntry": true,
			"requireArtifactSha256": true, "requireLocalPathGrant": true,
		}
		for name, value := range expected {
			if adversarialSchemaMap(t, fields, name)["const"] != value {
				t.Errorf("external model policy %s must be const %v", name, value)
			}
		}
	})

	t.Run("eval schema includes all strict runtime fields", func(t *testing.T) {
		schema := loadAdversarialSchema(t, "eval-case.schema.json")
		root := assertAdversarialClosedSchemaObject(t, "evaluation case", schema, []string{
			"id", "role", "topic", "split", "query", "queryClass", "allowedTiers",
			"corpusSnapshot", "expectedPaths", "minimumEvidencePaths",
			"hardNegativePaths", "expectedCitations", "forbiddenTiers",
			"tokenBudget", "answerable",
		}, []string{"gradedRelevantPaths", "evidencePins", "vocabularyPolicy"})
		policy := adversarialSchemaMap(t, root, "vocabularyPolicy")
		values, ok := policy["enum"].([]any)
		if !ok || !reflect.DeepEqual(values, []any{"target-disjoint-v1"}) {
			t.Errorf("evaluation vocabulary policy enum is not closed: %#v", policy["enum"])
		}
		for _, name := range []string{
			"expectedPaths", "minimumEvidencePaths", "hardNegativePaths", "expectedCitations",
		} {
			array := adversarialSchemaMap(t, root, name)
			if array["uniqueItems"] != true {
				t.Errorf("evaluation %s must reject duplicate paths", name)
			}
			items := adversarialSchemaMap(t, array, "items")
			assertAdversarialSchemaPattern(t, "evaluation "+name, items,
				[]string{"docs/truth/engine.md", ".re-discipline/project-profile.md"},
				[]string{"../outside.md", "/absolute.md", `docs\truth\engine.md`})
		}
		graded := adversarialSchemaMap(t, root, "gradedRelevantPaths")
		if graded["type"] != "object" {
			t.Error("gradedRelevantPaths must be an object")
		}
		propertyNames := adversarialSchemaMap(t, graded, "propertyNames")
		assertAdversarialSchemaPattern(t, "gradedRelevantPaths key", propertyNames,
			[]string{"docs/truth/engine.md"},
			[]string{"../outside.md", "/absolute.md", `docs\truth\engine.md`})
		grade := adversarialSchemaMap(t, graded, "additionalProperties")
		if grade["type"] != "integer" || grade["minimum"] != float64(1) ||
			grade["maximum"] != float64(3) {
			t.Error("gradedRelevantPaths values must be integer relevance grades 1 through 3")
		}
	})
}

func TestAdversarialTelemetryIsBoundedAggregateOnlyAndOffMeansNoWrites(t *testing.T) {
	prepareTelemetryRoot := func(t *testing.T, service *Service) {
		t.Helper()
		if err := os.MkdirAll(service.Index.CacheRoot, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	telemetryPath := func(t *testing.T, service *Service) string {
		t.Helper()
		path, err := service.telemetryPath()
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("metrics-only persists no retrieval content or source identity", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		ctx := context.Background()
		const querySecret = "ultra-sensitive-query-never-persist"
		const taskSecret = "ultra-sensitive-task-never-persist"
		if _, err := service.Search(ctx, SearchOptions{
			Query: querySecret, QueryClass: "conceptual",
			AllowedTiers: []string{"truth"}, TokenBudget: 512,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ContextPack(
			ctx, taskSecret, "drafter", []string{"truth"}, 1024); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(telemetryPath(t, service))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			querySecret, taskSecret, "docs/truth", "engine-frame-v7",
			filepath.ToSlash(root), filepath.Clean(root), "heading", "citation",
			"passage", "sourceHash", "passageHash", "nativeMemory",
		} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("aggregate telemetry persisted forbidden retrieval data %q: %s",
					forbidden, body)
			}
		}
		var aggregate TelemetryAggregate
		if err := decodeStrict(body, &aggregate); err != nil {
			t.Fatal(err)
		}
		if aggregate.SchemaVersion != 1 || aggregate.Mode != "metrics-only" {
			t.Fatalf("unexpected aggregate identity: %#v", aggregate)
		}
		if len(aggregate.Entries) != 2 {
			t.Fatalf("telemetry should aggregate the two operations, got %#v",
				aggregate.Entries)
		}
		seen := map[string]bool{}
		for _, entry := range aggregate.Entries {
			seen[entry.Operation] = true
			if entry.Calls != 1 || entry.Errors != 0 || entry.EffectiveProfile == "" {
				t.Errorf("invalid aggregate entry: %#v", entry)
			}
		}
		if !seen["search"] || !seen["context-pack"] {
			t.Errorf("telemetry omitted operation aggregates: %#v", aggregate.Entries)
		}
		status := service.telemetryStatus()
		if status["mode"] != "metrics-only" || status["persisted"] != true {
			t.Fatalf("status did not expose the bounded aggregate state: %#v", status)
		}
	})

	t.Run("same operation and effective profile accumulates totals", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		prepareTelemetryRoot(t, service)
		service.recordTelemetry(telemetryObservation{
			Operation: "search", EffectiveProfile: "balanced-v1@sha256:first",
			Latency: 7 * time.Millisecond, EstimatedTokens: 11, Results: 2, Omissions: 3,
		})
		service.recordTelemetry(telemetryObservation{
			Operation: "search", EffectiveProfile: "balanced-v1@sha256:second",
			Failed: true, Latency: 13 * time.Millisecond,
			EstimatedTokens: 17, Results: 5, Omissions: 7,
		})
		body, err := os.ReadFile(telemetryPath(t, service))
		if err != nil {
			t.Fatal(err)
		}
		var aggregate TelemetryAggregate
		if err := decodeStrict(body, &aggregate); err != nil {
			t.Fatal(err)
		}
		if len(aggregate.Entries) != 1 {
			t.Fatalf("profile generations were not aggregated: %#v", aggregate.Entries)
		}
		got := aggregate.Entries[0]
		if got.Operation != "search" || got.EffectiveProfile != "balanced-v1" ||
			got.Calls != 2 || got.Errors != 1 || got.LatencyMillis != 20 ||
			got.EstimatedTokens != 28 || got.Results != 7 || got.Omissions != 10 {
			t.Fatalf("aggregate totals are wrong: %#v", got)
		}
	})

	t.Run("cardinality remains bounded without losing call totals", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		prepareTelemetryRoot(t, service)
		const calls = telemetryEntryLimit + 9
		for index := 0; index < calls; index++ {
			service.recordTelemetry(telemetryObservation{
				Operation:        "search",
				EffectiveProfile: fmt.Sprintf("profile-%02d", index),
				EstimatedTokens:  1,
			})
		}
		body, err := os.ReadFile(telemetryPath(t, service))
		if err != nil {
			t.Fatal(err)
		}
		var aggregate TelemetryAggregate
		if err := decodeStrict(body, &aggregate); err != nil {
			t.Fatal(err)
		}
		if len(aggregate.Entries) > telemetryEntryLimit {
			t.Errorf("telemetry cardinality = %d, limit = %d",
				len(aggregate.Entries), telemetryEntryLimit)
		}
		var totalCalls uint64
		var totalTokens uint64
		for _, entry := range aggregate.Entries {
			totalCalls += entry.Calls
			totalTokens += entry.EstimatedTokens
		}
		if totalCalls != calls || totalTokens != calls {
			t.Errorf("bounded aggregation lost observations: calls=%d tokens=%d, want %d",
				totalCalls, totalTokens, calls)
		}
	})

	t.Run("concurrent service instances do not lose aggregate updates", func(t *testing.T) {
		root := makeAdversarialProject(t)
		const serviceCount = 8
		const callsPerService = 10
		services := make([]*Service, 0, serviceCount)
		for index := 0; index < serviceCount; index++ {
			services = append(services, newAdversarialService(t, root, nil))
		}
		prepareTelemetryRoot(t, services[0])
		start := make(chan struct{})
		var wait sync.WaitGroup
		for _, service := range services {
			wait.Add(1)
			go func(service *Service) {
				defer wait.Done()
				<-start
				for call := 0; call < callsPerService; call++ {
					service.recordTelemetry(telemetryObservation{
						Operation: "search", EffectiveProfile: "balanced-v1",
						EstimatedTokens: 1, Results: 1,
					})
				}
			}(service)
		}
		close(start)
		wait.Wait()
		body, err := os.ReadFile(telemetryPath(t, services[0]))
		if err != nil {
			t.Fatal(err)
		}
		var aggregate TelemetryAggregate
		if err := decodeStrict(body, &aggregate); err != nil {
			t.Fatal(err)
		}
		if len(aggregate.Entries) != 1 {
			t.Fatalf("concurrent updates split one aggregate: %#v", aggregate.Entries)
		}
		got := aggregate.Entries[0]
		want := uint64(serviceCount * callsPerService)
		if got.Calls != want || got.EstimatedTokens != want || got.Results != want {
			t.Fatalf("concurrent aggregation lost updates: %#v, want totals %d", got, want)
		}
	})

	t.Run("off neither creates nor updates an aggregate", func(t *testing.T) {
		root := makeAdversarialProject(t)
		writeTestFile(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), `{
  "schemaVersion": 2,
  "sources": {
    "truth": true,
    "historyFindings": true,
    "backlog": true,
    "activeFindings": true,
    "sharedMemory": true,
    "reportFallback": true
  },
  "models": {"execution": "local"},
  "telemetry": {"mode": "off"},
  "budgets": {
    "searchTokens": 1024,
    "managerContextTokens": 2048,
    "drafterContextTokens": 1024,
    "maxCards": 16,
    "maxBytes": 32768
  },
  "archive": {
    "reportFallbackUntilMeasured": true,
    "normalizationTriggerHits": 3
  }
}`)
		service := newAdversarialService(t, root, nil)
		prepareTelemetryRoot(t, service)
		path := telemetryPath(t, service)
		service.recordTelemetry(telemetryObservation{
			Operation: "search", EffectiveProfile: "balanced-v1",
			EstimatedTokens: 999, Results: 99,
		})
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("telemetry off created an artifact: %v", err)
		}
		if status := service.telemetryStatus(); status["mode"] != "off" ||
			status["persisted"] != false {
			t.Fatalf("telemetry off leaked aggregate state: %#v", status)
		}

		writeTestFile(t, path, "{\"sentinel\":\"must-remain-byte-identical\"}\n")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		service.recordTelemetry(telemetryObservation{
			Operation: "context-pack", EffectiveProfile: "balanced-v1",
			Failed: true, EstimatedTokens: 111,
		})
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("telemetry off updated a pre-existing aggregate artifact")
		}
	})
}

func TestAdversarialReadAndRequiredContextStayOnOneFreshGeneration(t *testing.T) {
	t.Run("chunk handles reject whole-document drift outside the chunk", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		ctx := context.Background()
		generation, _, selected, _, err := service.ensure(ctx)
		if err != nil {
			t.Fatal(err)
		}
		search, err := service.Search(ctx, SearchOptions{
			Query: "A1B2C3D4", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 1024,
			// The generation-scoped URI is the handle under test here.
			Verbosity: VerbosityVerbose,
		})
		if err != nil {
			t.Fatal(err)
		}
		var engine *SearchResult
		for index := range search.Results {
			if search.Results[index].Citation.Path == adversarialEnginePath {
				engine = &search.Results[index]
				break
			}
		}
		if engine == nil {
			t.Fatal("fixture search did not return the engine chunk")
		}
		enginePath := filepath.Join(root, filepath.FromSlash(adversarialEnginePath))
		original, err := os.ReadFile(enginePath)
		if err != nil {
			t.Fatal(err)
		}
		changed := append(append([]byte(nil), original...),
			[]byte("\n# Unrelated appended section\nwhole-document-drift-marker\n")...)
		if err := AtomicWrite(enginePath, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := service.readPinned(ctx, generation, selected, ReadOptions{
			URI: engine.Citation.URI,
		}); !errors.Is(err, errSourceChangedAfterIndex) {
			t.Fatalf("pinned read did not reject whole-document drift: %v", err)
		}
		if _, err := service.Read(ctx, ReadOptions{URI: engine.Citation.URI}); err == nil {
			t.Fatal("public read accepted a stale generation handle after source drift")
		}
		fresh, err := service.Search(ctx, SearchOptions{
			Query: "whole-document-drift-marker", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 512,
		})
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Metadata.Generation == generation.ID {
			t.Fatal("source drift did not produce a fresh generation")
		}
	})

	t.Run("required cards cannot exceed the configured hard cap", func(t *testing.T) {
		root := makeAdversarialProject(t)
		required := make([]string, 0, 17)
		for index := 0; index < 17; index++ {
			path := fmt.Sprintf("docs/playbooks/required-%02d.md", index)
			required = append(required, path)
			writeTestFile(t, filepath.Join(root, filepath.FromSlash(path)),
				fmt.Sprintf("# Required %02d\n\nrequired-cap-marker-%02d\n", index, index))
		}
		service := newAdversarialService(t, root, nil)
		_, err := service.ContextPackRequired(
			context.Background(), "required cap", "manager",
			[]string{"playbook"}, 2048, required,
		)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "card cap") {
			t.Fatalf("required paths bypassed maxCards or failed unclearly: %v", err)
		}
	})

	t.Run("concurrent mutation never creates a mixed-generation pack", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		enginePath := filepath.Join(root, filepath.FromSlash(adversarialEnginePath))
		original, err := os.ReadFile(enginePath)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		mutatorDone := make(chan error, 1)
		go func() {
			for iteration := 0; ; iteration++ {
				select {
				case <-stop:
					mutatorDone <- nil
					return
				default:
				}
				body := original
				if iteration%2 == 0 {
					body = append(append([]byte(nil), original...),
						[]byte("\nconcurrent-generation-marker\n")...)
				}
				if err := AtomicWrite(enginePath, body, 0o600); err != nil {
					// Windows can transiently deny replacement while SQLite
					// freshness verification has the source open. That is a
					// safe rejection, so keep racing until the stop signal.
					time.Sleep(time.Millisecond)
				}
			}
		}()
		successes := 0
		for iteration := 0; iteration < 16; iteration++ {
			pack, err := service.ContextPackOptions(
				context.Background(), ContextPackRequest{
					Target: ContextPackTarget{Kind: "project"},
					Task:   "engine frame checksum", Role: "drafter",
					Tiers: []string{"truth"}, TokenBudget: 1024,
					RequiredPaths: []string{adversarialEnginePath},
				})
			if err != nil {
				continue
			}
			successes++
			generationID := pack.Generation.ID
			for _, card := range pack.Cards {
				if !strings.HasPrefix(card.Handle, "re-discipline://") {
					continue
				}
				if !strings.HasPrefix(
					card.Handle,
					"re-discipline://"+generationID+"/",
				) {
					t.Errorf("context pack mixed generation %s with citation %s",
						generationID, card.Handle)
				}
			}
		}
		close(stop)
		if err := <-mutatorDone; err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(enginePath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if successes == 0 {
			t.Log("all racing attempts rejected source drift; no mixed pack was emitted")
		}
		stable, err := service.ContextPackRequired(
			context.Background(), "engine frame checksum", "drafter",
			[]string{"truth"}, 1024, []string{adversarialEnginePath},
		)
		if err != nil {
			t.Fatalf("service did not recover after source mutation stopped: %v", err)
		}
		for _, card := range stable.Cards {
			if strings.HasPrefix(card.Handle, "re-discipline://") &&
				!strings.Contains(card.Handle, stable.Generation.ID) {
				t.Fatalf("stable pack has a mixed-generation handle: %#v", card)
			}
		}
	})
}

func adversarialProjectBenchmarkCases(corpusFingerprint string) []EvalCase {
	return []EvalCase{
		{
			ID: "project-benchmark-development", Role: "manager",
			Topic: "project-benchmark-exact", Split: "development",
			Query: "A1B2C3D4", QueryClass: "exact", AllowedTiers: []string{"truth"},
			CorpusSnapshot: corpusFingerprint,
			ExpectedPaths: []string{
				adversarialEnginePath,
			},
			MinimumEvidencePaths: []string{adversarialEnginePath},
			ExpectedCitations:    []string{adversarialEnginePath},
			ForbiddenTiers:       []string{"history"},
			TokenBudget:          1024, Answerable: boolPointer(true),
		},
		{
			ID: "project-benchmark-holdout", Role: "drafter",
			Topic: "project-benchmark-portability", Split: "holdout",
			Query: "stable signatures maintained recipes", QueryClass: "conceptual",
			AllowedTiers:   []string{"truth"},
			CorpusSnapshot: corpusFingerprint,
			ExpectedPaths:  []string{adversarialPortabilityPath},
			MinimumEvidencePaths: []string{
				adversarialPortabilityPath,
			},
			ExpectedCitations: []string{adversarialPortabilityPath},
			ForbiddenTiers:    []string{"history"},
			TokenBudget:       1024, Answerable: boolPointer(true),
		},
	}
}

func assertAdversarialContextPackOutcomes(
	t *testing.T,
	outcomes []ContextPackOutcome,
	cases []EvalCase,
	requestedBudget int,
) {
	t.Helper()
	if len(outcomes) != len(cases) {
		t.Fatalf("context-pack outcomes=%d, want %d", len(outcomes), len(cases))
	}
	casesByID := make(map[string]EvalCase, len(cases))
	for _, eval := range cases {
		casesByID[eval.ID] = eval
	}
	for _, outcome := range outcomes {
		eval, ok := casesByID[outcome.CaseID]
		if !ok {
			t.Fatalf("context-pack outcome references unknown case %q", outcome.CaseID)
		}
		expectedRequested := requestedBudget
		if expectedRequested == 0 {
			expectedRequested = eval.TokenBudget
		}
		expectedCeiling := 2048
		if eval.Role == "drafter" {
			expectedCeiling = 1024
		}
		expectedEffective := expectedRequested
		if expectedEffective > expectedCeiling {
			expectedEffective = expectedCeiling
		}
		expectedMinimum := eval.TokenBudget
		if expectedMinimum < MinimumContextPackEvidenceBudget {
			expectedMinimum = MinimumContextPackEvidenceBudget
		}
		qualityApplicable := expectedRequested >= expectedMinimum
		if outcome.RequestedTokenBudget != expectedRequested ||
			outcome.EffectiveTokenBudget != expectedEffective ||
			outcome.RoleTokenCeiling != expectedCeiling ||
			outcome.MinimumTokenBudget != expectedMinimum ||
			outcome.QualityGateApplicable != qualityApplicable {
			t.Fatalf("context-pack outcome has wrong budget gate: %#v", outcome)
		}
		if outcome.Role != eval.Role || outcome.Split != eval.Split ||
			outcome.Topic != eval.Topic ||
			!sha256IdentityRE.MatchString(outcome.Digest) ||
			!strings.HasPrefix(outcome.PackID, "context-") ||
			outcome.Generation == "" || outcome.EffectiveProfile == "" ||
			outcome.Error != "" {
			t.Fatalf("context-pack outcome omitted pinned identity: %#v", outcome)
		}
		if !outcome.RoleCeilingSafe || !outcome.AllowedTiersSafe ||
			!outcome.CardCapSafe || !outcome.ByteCapSafe ||
			!outcome.TokenAccountingSafe || !outcome.BudgetSafe ||
			!outcome.GenerationPinned || !outcome.ProfilePinned ||
			!outcome.VerificationPassed || !outcome.ReplayIdentical ||
			!outcome.SafetyPassed || !outcome.QualityPassed || !outcome.Passed {
			t.Fatalf("context-pack outcome failed a required gate: %#v", outcome)
		}
		if outcome.CardCount > outcome.MaxCards ||
			outcome.SerializedBytes > outcome.MaxBytes ||
			outcome.EstimatedTokens > outcome.EffectiveTokenBudget {
			t.Fatalf("context-pack outcome exceeded packing ceilings: %#v", outcome)
		}
		if qualityApplicable &&
			(!outcome.RequiredEvidencePresent ||
				!outcome.ExpectedEvidenceFound ||
				!outcome.AbstentionCorrect) {
			t.Fatalf("declared-budget context pack missed required quality: %#v", outcome)
		}
	}
}

func prepareAdversarialProjectBenchmark(
	t *testing.T,
	root string,
) *Service {
	t.Helper()
	service := newAdversarialService(t, root, nil)
	generation, _, _, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "project-benchmark.json"),
		adversarialProjectBenchmarkCases(generation.CorpusFingerprint))
	return service
}

func TestAdversarialProjectConfigurationCannotEscapeThroughLinks(t *testing.T) {
	tests := []struct {
		name      string
		relative  string
		parentDir bool
	}{
		{name: "config file link", relative: ".re-discipline/config.json"},
		{name: "config parent link", relative: ".re-discipline", parentDir: true},
		{name: "knowledge settings file link", relative: ".re-discipline/knowledge/policy.jsonc"},
		{name: "knowledge settings parent link", relative: ".re-discipline/knowledge", parentDir: true},
		{name: "retrieval profile file link", relative: ".re-discipline/knowledge/retrieval-profile.json"},
		{name: "retrieval profile parent link", relative: ".re-discipline/knowledge", parentDir: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			root := makeAdversarialProject(t)
			service := prepareAdversarialProjectBenchmark(t, root)
			target := filepath.Join(root, filepath.FromSlash(testCase.relative))
			outside := filepath.Join(t.TempDir(), "escaped")
			if testCase.parentDir {
				if err := copyTree(target, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(target); err != nil {
					t.Fatal(err)
				}
				if !makeDirectoryLink(t, outside, target) {
					t.Skip("directory links are unavailable")
				}
			} else {
				copyTestFile(t, target, outside)
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if !makeFileLink(t, outside, target) {
					t.Skip("file links are unavailable")
				}
			}

			if _, err := NewService(ServiceOptions{
				ProjectRoot: root,
				AssetRoot:   adversarialAssetRoot(t),
				CacheRoot: filepath.Join(
					root, ".re-discipline", "cache", "knowledge"),
			}); err == nil {
				t.Fatal("NewService accepted project configuration through an escaping link")
			}
			if _, err := service.RunProjectBenchmark(
				context.Background(), "quick"); err == nil {
				t.Fatal("project benchmark trusted configuration swapped to an escaping link")
			}
			escapedReports, err := filepath.Glob(filepath.Join(
				outside, "cache", "knowledge", "benchmarks", "*", "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			if len(escapedReports) != 0 {
				t.Fatalf("project benchmark wrote outside the project through linked configuration: %v",
					escapedReports)
			}
		})
	}
}

func TestAdversarialProjectBenchmarkUsesRatifiedProjectEvalsAndCacheOnly(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	generation, _, _, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := adversarialProjectBenchmarkCases(generation.CorpusFingerprint)
	evalPath := filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "project-benchmark.json")
	writeTestJSON(t, evalPath, cases)
	canonicalPaths := []string{
		".re-discipline/project-profile.md",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".re-discipline/knowledge/retrieval-profile.json",
		".re-discipline/knowledge/evals/project-benchmark.json",
		adversarialEnginePath,
		adversarialPortabilityPath,
	}
	before := map[string][]byte{}
	for _, relative := range canonicalPaths {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		before[relative] = body
	}

	quick, err := service.RunProjectBenchmark(context.Background(), "quick")
	if err != nil {
		t.Fatal(err)
	}
	if quick.SchemaVersion != 1 || quick.Mode != "quick" ||
		quick.Suite != "project-benchmark-v1" || !quick.Complete || !quick.Passed ||
		len(quick.Profiles) != 1 || len(quick.UnsupportedProfiles) != 0 {
		t.Fatalf("quick project benchmark is incomplete: %#v", quick)
	}
	if quick.Generation.ID != generation.ID ||
		!sha256IdentityRE.MatchString(quick.EvalFingerprint) ||
		!strings.HasPrefix(quick.RequestedProfile, service.ProfileCatalog.ProfileID+"@") {
		t.Fatalf("quick report omitted pinned project identities: %#v", quick)
	}
	quickProfile := quick.Profiles[0]
	if len(quickProfile.Cases) != len(cases) ||
		len(quickProfile.MetricsByBudget) != 0 ||
		len(quickProfile.CasesByBudget) != 0 ||
		len(quickProfile.ContextPacksByBudget) != 0 ||
		!quickProfile.ContextPackPassed ||
		!contains(quickProfile.ContextPackRoles, "manager") ||
		!contains(quickProfile.ContextPackRoles, "drafter") ||
		!sha256IdentityRE.MatchString(quickProfile.ObservationDigest) {
		t.Fatalf("quick mode did not evaluate only accepted project cases: %#v", quickProfile)
	}
	assertAdversarialContextPackOutcomes(
		t, quickProfile.ContextPackCases, cases, 0)
	caseIDs := map[string]bool{}
	for _, outcome := range quickProfile.Cases {
		caseIDs[outcome.CaseID] = true
	}
	for _, eval := range cases {
		if !caseIDs[eval.ID] {
			t.Errorf("quick project report omitted case %s", eval.ID)
		}
	}
	for _, split := range []string{"development", "holdout"} {
		if _, ok := quickProfile.MetricsBySplit[split]; !ok {
			t.Errorf("quick project report omitted %s metrics", split)
		}
	}
	reportAbsolute := filepath.FromSlash(quick.ReportPath)
	if !withinRoot(service.Index.CacheRoot, reportAbsolute) {
		t.Fatalf("project report escaped cache root: %s", reportAbsolute)
	}
	reportBody, err := os.ReadFile(reportAbsolute)
	if err != nil {
		t.Fatal(err)
	}
	var serialized ProjectBenchmarkReport
	if err := decodeStrict(reportBody, &serialized); err != nil {
		t.Fatal(err)
	}
	if stableJSON(serialized) != stableJSON(quick) {
		t.Fatal("cached project benchmark report differs from returned report")
	}
	for relative, expected := range before {
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Errorf("project benchmark mutated canonical file %s", relative)
		}
	}

	replay, err := service.RunProjectBenchmark(context.Background(), "quick")
	if err != nil {
		t.Fatal(err)
	}
	if replay.EvalFingerprint != quick.EvalFingerprint ||
		replay.Generation.ID != quick.Generation.ID ||
		replay.Profiles[0].ObservationDigest != quickProfile.ObservationDigest {
		t.Fatal("identical project benchmark did not produce stable evidence identity")
	}

	full, err := service.RunProjectBenchmark(context.Background(), "full")
	if err != nil {
		t.Fatal(err)
	}
	if !full.Complete || !full.Passed ||
		len(full.Profiles)+len(full.UnsupportedProfiles) !=
			len(service.ProfileCatalog.EffectiveProfiles) {
		failures := []string{}
		for _, profile := range full.Profiles {
			reasons := []string{}
			if !hardMetricsPassed(profile.Metrics) {
				reasons = append(reasons, "base-metrics")
			}
			for _, outcome := range profile.Cases {
				if !evaluationOutcomePassed(outcome) {
					reasons = append(reasons, "case:"+outcome.CaseID)
				}
			}
			for budget, metrics := range profile.QualityMetricsByBudget {
				if !hardMetricsPassed(metrics) {
					reasons = append(reasons, "budget-metrics:"+budget)
				}
			}
			for budget, outcomes := range profile.CasesByBudget {
				for _, outcome := range outcomes {
					if !evaluationOutcomePassed(outcome) {
						reasons = append(
							reasons, "budget-case:"+budget+":"+outcome.CaseID)
					}
				}
			}
			if !profile.ContextPackPassed {
				reasons = append(reasons, "context-pack")
			}
			if !profile.NonInferiorToLexical {
				reasons = append(reasons, "non-inferior")
			}
			if !profile.HardGatesPassed || len(reasons) > 0 {
				failures = append(failures,
					fmt.Sprintf("%s(hard=%t reasons=%s)",
						profile.ProfileName, profile.HardGatesPassed,
						strings.Join(reasons, ",")))
			}
		}
		t.Fatalf(
			"full project capability matrix is incomplete: passed=%t complete=%t profiles=%d unsupported=%#v failures=%v",
			full.Passed, full.Complete, len(full.Profiles),
			full.UnsupportedProfiles, failures)
	}
	for _, profile := range full.Profiles {
		if !profile.HardGatesPassed || !profile.NonInferiorToLexical ||
			!profile.ContextPackPassed ||
			!contains(profile.ContextPackRoles, "manager") ||
			!contains(profile.ContextPackRoles, "drafter") {
			t.Errorf("supported project profile failed a full gate: %#v", profile)
		}
		assertAdversarialContextPackOutcomes(
			t, profile.ContextPackCases, cases, 0)
		for _, budget := range []string{"512", "1024", "2048", "4096"} {
			if _, ok := profile.MetricsByBudget[budget]; !ok {
				t.Errorf("profile %s omitted budget metrics %s", profile.ProfileName, budget)
			}
			if _, ok := profile.QualityMetricsByBudget[budget]; !ok {
				t.Errorf("profile %s omitted applicable-quality metrics %s",
					profile.ProfileName, budget)
			}
			outcomes, ok := profile.CasesByBudget[budget]
			if !ok || len(outcomes) != len(cases) {
				t.Errorf("profile %s omitted per-case budget evidence %s",
					profile.ProfileName, budget)
			}
			contextOutcomes, ok := profile.ContextPacksByBudget[budget]
			if !ok {
				t.Errorf("profile %s omitted context-pack budget evidence %s",
					profile.ProfileName, budget)
				continue
			}
			budgetValue, err := strconv.Atoi(budget)
			if err != nil {
				t.Fatal(err)
			}
			assertAdversarialContextPackOutcomes(
				t, contextOutcomes, cases, budgetValue)
		}
	}

	cases[0].Query = "exact identifier A1B2C3D4"
	writeTestJSON(t, evalPath, cases)
	fresh, err := service.RunProjectBenchmark(context.Background(), "quick")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.EvalFingerprint == quick.EvalFingerprint ||
		fresh.Profiles[0].ObservationDigest == quickProfile.ObservationDigest {
		t.Fatal("project benchmark evidence did not bind the current eval corpus")
	}
	if _, err := service.RunProjectBenchmark(context.Background(), "invalid"); err == nil {
		t.Fatal("project benchmark accepted an unsupported mode")
	}
}

// TestAdversarialProjectBenchmarkHardGatesGateSafetyNotQuality pins the hard
// gate to per-case SAFETY. A model-free baseline whose retrieval is safe,
// deterministic, and budget-compliant, but that legitimately falls short of
// perfect recall/citation coverage on a case, must still pass hard gates:
// retrieval-quality shortfalls are graded for non-inferiority against the
// incumbent, not treated as absolute gate violations. Before the fix the hard
// gate read outcome.GatePassed (safety AND per-case quality perfection), so a
// single imperfect-but-safe case sank a spotless baseline.
func TestAdversarialProjectBenchmarkHardGatesGateSafetyNotQuality(t *testing.T) {
	root := makeAdversarialProject(t)
	// Keep the free-retrieval miss deterministic even when the production dense
	// lane is available: more than the result limit's worth of high-signal truth
	// documents outrank the deliberately unrelated required document. Context-pack
	// evaluation still injects the required path explicitly.
	for index := 0; index < 32; index++ {
		id := fmt.Sprintf("F-%04d", 9100+index)
		path := "docs/truth/findings/fixture-truth/" + id + ".md"
		writeAdversarialTruthFinding(t, root, id, path,
			fmt.Sprintf("fixture.quality-distractor-%02d", index),
			fmt.Sprintf("A1B2C3D4 checksum evidence engine frame serialization distractor %02d.", index),
			FindingRelations{})
	}
	service := newAdversarialService(t, root, nil)
	generation, _, _, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := adversarialProjectBenchmarkCases(generation.CorpusFingerprint)
	// The query surfaces truth-tier, well-formed, in-budget, deterministic
	// results while the case declares the unlinked literal-hash document as its
	// evidence. Free retrieval is therefore safety-clean but quality-imperfect:
	// expectedFound/completeEvidence/citationSafe are all false, yet no safety,
	// determinism, or budget property is violated. The context pack force-injects
	// the required path, so the context-pack conjunct still passes.
	cases = append(cases, EvalCase{
		ID: "project-benchmark-quality-shortfall", Role: "manager",
		Topic: "project-benchmark-quality-shortfall", Split: "development",
		Query: "A1B2C3D4 checksum evidence", QueryClass: "conceptual", AllowedTiers: []string{"truth"},
		CorpusSnapshot:       generation.CorpusFingerprint,
		ExpectedPaths:        []string{adversarialHashPath},
		MinimumEvidencePaths: []string{adversarialHashPath},
		ExpectedCitations:    []string{adversarialHashPath},
		ForbiddenTiers:       []string{"history"},
		TokenBudget:          1024, Answerable: boolPointer(true),
	})
	writeTestJSON(t, filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "project-benchmark.json"), cases)

	report, err := service.RunProjectBenchmark(context.Background(), "quick")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Profiles) != 1 {
		t.Fatalf("quick benchmark produced %d profiles, want 1", len(report.Profiles))
	}
	profile := report.Profiles[0]

	// Precondition: the baseline is safety-clean on every case and the crafted
	// case genuinely fails quality. If this stops holding the fixture no longer
	// reproduces the bug and the assertion below would be vacuous.
	sawQualityShortfall := false
	for _, outcome := range profile.Cases {
		if !outcome.SafetyPassed {
			t.Fatalf("baseline case %q failed a safety gate: %#v",
				outcome.CaseID, outcome)
		}
		if outcome.CaseID == "project-benchmark-quality-shortfall" &&
			!outcome.QualityPassed {
			sawQualityShortfall = true
		}
	}
	if !sawQualityShortfall {
		t.Fatal("fixture no longer produces a safe-but-quality-imperfect case")
	}

	if !profile.ContextPackPassed {
		t.Fatalf("context-pack conjunct failed, confounding the hard-gate check: %#v",
			profile.ContextPackCases)
	}
	if !profile.HardGatesPassed {
		t.Fatal("hard gates failed on a safety-clean baseline with only a " +
			"retrieval-quality shortfall; quality must be graded for " +
			"non-inferiority, not gated absolutely")
	}
	if !report.Passed {
		t.Fatal("quick project benchmark did not pass despite a safety-clean baseline")
	}
}

// TestProjectNonInferiorityKeepsHardGatesSeparate pins the second axis: a
// profile that clears every absolute hard gate but underperforms the lexical
// baseline must keep hardGatesPassed=true and only lose nonInferiorToLexical.
// The overall run still fails (report.Passed=false) so an inferior promoted
// profile is rejected, but the per-profile hard-gate verdict must not be
// corrupted, or a reader and calibration cannot tell "hard gates fail" from
// "hard gates pass but loses to the baseline".
func TestProjectNonInferiorityKeepsHardGatesSeparate(t *testing.T) {
	report := &ProjectBenchmarkReport{
		Passed: true, Complete: true,
		Profiles: []ProjectProfileBenchmark{
			{
				ProfileName:     "lexical-graph-v1",
				HardGatesPassed: true, NonInferiorToLexical: true,
				MetricsBySplit: map[string]QualityMetrics{
					"holdout": {
						RecallAtK: 0.9, NDCG: 0.9,
						CompleteEvidenceCoverage: 0.9, CitationRecall: 0.9,
					},
				},
			},
			{
				// Passes every absolute hard gate but is strictly worse than the
				// baseline on every non-inferiority axis.
				ProfileName:     "hybrid-local-v1",
				HardGatesPassed: true, NonInferiorToLexical: true,
				MetricsBySplit: map[string]QualityMetrics{
					"holdout": {
						RecallAtK: 0.5, NDCG: 0.5,
						CompleteEvidenceCoverage: 0.5, CitationRecall: 0.5,
					},
				},
			},
		},
	}
	applyProjectNonInferiority(report)

	hybrid := report.Profiles[1]
	if hybrid.NonInferiorToLexical {
		t.Fatal("expected the underperforming profile to lose non-inferiority")
	}
	if !hybrid.HardGatesPassed {
		t.Fatal("non-inferiority failure corrupted hardGatesPassed; the absolute " +
			"hard-gate axis and the non-inferiority axis must stay separate")
	}
	if report.Passed {
		t.Fatal("overall benchmark must fail when a promoted profile is inferior")
	}
	if base := report.Profiles[0]; !base.HardGatesPassed || !base.NonInferiorToLexical {
		t.Fatalf("baseline profile must remain passing: %#v", base)
	}
}

func TestAdversarialRehashedContextPackStillRequiresSemanticValidity(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	valid, err := service.ContextPackRequired(
		context.Background(), "engine frame checksum", "drafter",
		[]string{"truth", "playbook"}, 2048, []string{adversarialLiteralHashPath},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyContextPackValue(valid); err != nil {
		t.Fatalf("valid context pack failed verification: %v", err)
	}
	provenanceIndex := -1
	for index := range valid.Cards {
		if valid.Cards[index].Metadata["path"] == adversarialLiteralHashPath {
			provenanceIndex = index
			break
		}
	}
	if provenanceIndex < 0 {
		t.Fatal("valid fixture pack omitted its required generation-bound provenance card")
	}
	requiredHandles := map[string]bool{}
	for _, handle := range valid.RequiredHandles {
		requiredHandles[handle] = true
	}
	nonRequiredIndex := -1
	for index := range valid.Cards {
		if !requiredHandles[valid.Cards[index].Handle] {
			nonRequiredIndex = index
			break
		}
	}
	if nonRequiredIndex < 0 {
		t.Fatal("valid fixture pack omitted a non-required card for allowlist validation")
	}
	rehash := func(t *testing.T, pack ContextPack) ContextPack {
		t.Helper()
		pack.PackID, pack.Digest = "", ""
		finalized, err := finalizeContextPack(pack)
		if err != nil {
			t.Fatal(err)
		}
		return finalized
	}
	tests := []struct {
		name   string
		mutate func(*ContextPack)
	}{
		{
			name: "tier outside allowlist",
			mutate: func(pack *ContextPack) {
				pack.Cards[nonRequiredIndex].SourceClass = "history"
				pack.Cards[nonRequiredIndex].Metadata["tier"] = "history"
			},
		},
		{
			name: "traversing provenance path",
			mutate: func(pack *ContextPack) {
				pack.Cards[provenanceIndex].Metadata["path"] = "../outside.md"
			},
		},
		{
			name: "malformed provenance hash",
			mutate: func(pack *ContextPack) {
				pack.Cards[provenanceIndex].Metadata["passageHash"] = "sha256:bad"
			},
		},
		{
			name: "generation and URI mismatch",
			mutate: func(pack *ContextPack) {
				pack.Generation.ID = "generation-" + strings.Repeat("0", 20)
			},
		},
		{
			name: "missing parser identity",
			mutate: func(pack *ContextPack) {
				pack.Generation.ParserVersion = ""
			},
		},
		{
			name: "missing project identity",
			mutate: func(pack *ContextPack) {
				pack.Generation.Project = ""
			},
		},
		{
			name: "malformed runtime fingerprint",
			mutate: func(pack *ContextPack) {
				pack.Generation.RuntimeFingerprint = "sha256:abc"
			},
		},
		{
			name: "removed model identity",
			mutate: func(pack *ContextPack) {
				pack.Models = []string{"../escape@1"}
			},
		},
		{
			name: "claimed token use below content",
			mutate: func(pack *ContextPack) {
				pack.TokenBudget = 1
				pack.EstimatedTokens = 0
			},
		},
		{
			name: "invalid generation follow-up handle",
			mutate: func(pack *ContextPack) {
				pack.Cards[provenanceIndex].Handle = "re-discipline://forged"
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			forged := valid
			forged.Models = append([]string(nil), valid.Models...)
			forged.Cards = append([]ContextCard(nil), valid.Cards...)
			for index := range forged.Cards {
				if forged.Cards[index].Metadata != nil {
					forged.Cards[index].Metadata = cloneStringMap(forged.Cards[index].Metadata)
				}
			}
			testCase.mutate(&forged)
			forged = rehash(t, forged)
			if _, err := VerifyContextPackValue(forged); err == nil {
				t.Fatal("verifier accepted a semantically forged but rehashed context pack")
			}
		})
	}
	t.Run("independent manager digest rejects valid re-finalized substitution", func(t *testing.T) {
		substitute := valid
		substitute.Task = "different but semantically valid task"
		substitute = rehash(t, substitute)
		if _, err := VerifyContextPackValue(substitute); err != nil {
			t.Fatalf("substitute should remain a semantically valid standalone pack: %v", err)
		}
		if _, err := VerifyContextPackValueExpected(
			substitute, valid.Digest, valid.PackID); err == nil {
			t.Fatal("manager-bound verification accepted a valid re-finalized substitute")
		}
		if _, err := VerifyContextPackValueExpected(
			substitute, substitute.Digest, substitute.PackID); err != nil {
			t.Fatalf("matching independent identity rejected valid pack: %v", err)
		}
		if _, err := VerifyContextPackValueExpected(
			substitute, strings.Repeat("a", 64), ""); err == nil {
			t.Fatal("verifier accepted an expected digest without sha256 identity prefix")
		}

		identitySubstitute := valid
		identitySubstitute.Generation.RuntimeFingerprint =
			"sha256:" + strings.Repeat("0", 64)
		identitySubstitute = rehash(t, identitySubstitute)
		if _, err := VerifyContextPackValue(identitySubstitute); err != nil {
			t.Fatalf("compact digest identity should be standalone-valid: %v", err)
		}
		if _, err := VerifyContextPackValueExpected(
			identitySubstitute, valid.Digest, valid.PackID); err == nil {
			t.Fatal("manager-bound verification accepted substituted runtime identity")
		}
	})

	t.Run("file verifier enforces expected identity and size before parsing", func(t *testing.T) {
		validPath := filepath.Join(t.TempDir(), "context-pack.json")
		writeTestJSON(t, validPath, valid)
		if _, err := VerifyContextPack(validPath, valid.Digest, valid.PackID); err != nil {
			t.Fatalf("file verifier rejected matching expected identity: %v", err)
		}
		if _, err := VerifyContextPack(
			validPath, "sha256:"+strings.Repeat("0", 64)); err == nil {
			t.Fatal("file verifier accepted a mismatched independent digest")
		}
		oversize := filepath.Join(t.TempDir(), "oversize-context-pack.json")
		writeTestFile(t, oversize, strings.Repeat(" ", 262145))
		if _, err := VerifyContextPack(oversize); err == nil ||
			!strings.Contains(strings.ToLower(err.Error()), "size") {
			t.Fatalf("file verifier did not fail early on oversize input: %v", err)
		}
	})
}

func TestAdversarialProjectEvalLoaderEnforcesBoundarySizeAndAggregateCorpus(t *testing.T) {
	makeCase := func(id, topic, split string) EvalCase {
		return EvalCase{
			ID: id, Role: "manager", Topic: topic, Split: split,
			Query: "A1B2C3D4", QueryClass: "exact",
			AllowedTiers:   []string{"truth"},
			CorpusSnapshot: "fixture:aggregate-evals",
			ExpectedPaths:  []string{adversarialEnginePath},
			MinimumEvidencePaths: []string{
				adversarialEnginePath,
			},
			ExpectedCitations: []string{adversarialEnginePath},
			ForbiddenTiers:    []string{"history"},
			TokenBudget:       512, Answerable: boolPointer(true),
		}
	}
	t.Run("file link cannot import evaluation data from outside project", func(t *testing.T) {
		root := makeAdversarialProject(t)
		evalRoot := filepath.Join(root, ".re-discipline", "knowledge", "evals")
		outside := filepath.Join(t.TempDir(), "outside.json")
		writeTestJSON(t, outside, []EvalCase{
			makeCase("outside-case", "outside-topic", "development"),
		})
		link := filepath.Join(evalRoot, "outside.json")
		if !makeFileLink(t, outside, link) {
			outsideDirectory := filepath.Join(t.TempDir(), "outside-evals")
			copyTestFile(t, outside, filepath.Join(outsideDirectory, "outside.json"))
			if !makeDirectoryLink(
				t, outsideDirectory, filepath.Join(evalRoot, "linked-directory")) {
				t.Skip("file and directory links are unavailable")
			}
		}
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("project eval loader followed an outside file link")
		}
	})

	t.Run("oversize evaluation file is rejected", func(t *testing.T) {
		root := makeAdversarialProject(t)
		path := filepath.Join(
			root, ".re-discipline", "knowledge", "evals", "oversize.json")
		writeTestFile(t, path, strings.Repeat(" ", int(maxSourceBytes)+1))
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("project eval loader accepted an oversize file")
		}
	})

	t.Run("duplicate IDs across files are rejected", func(t *testing.T) {
		root := makeAdversarialProject(t)
		evalRoot := filepath.Join(root, ".re-discipline", "knowledge", "evals")
		writeTestJSON(t, filepath.Join(evalRoot, "one.json"), []EvalCase{
			makeCase("duplicate-case", "topic-one", "development"),
		})
		second := makeCase("duplicate-case", "topic-two", "holdout")
		second.Query = "stable signatures"
		writeTestJSON(t, filepath.Join(evalRoot, "two.json"), []EvalCase{second})
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("aggregate project eval loader accepted duplicate cross-file IDs")
		}
	})

	t.Run("topic split leakage across files is rejected", func(t *testing.T) {
		root := makeAdversarialProject(t)
		evalRoot := filepath.Join(root, ".re-discipline", "knowledge", "evals")
		writeTestJSON(t, filepath.Join(evalRoot, "development.json"), []EvalCase{
			makeCase("development-case", "shared-topic", "development"),
		})
		holdout := makeCase("holdout-case", "shared-topic", "holdout")
		holdout.Query = "stable signatures"
		writeTestJSON(t, filepath.Join(evalRoot, "holdout.json"), []EvalCase{holdout})
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("aggregate project eval loader allowed topic leakage across files")
		}
	})

	t.Run("aggregate case limit applies across files", func(t *testing.T) {
		root := makeAdversarialProject(t)
		evalRoot := filepath.Join(root, ".re-discipline", "knowledge", "evals")
		first := make([]EvalCase, 5001)
		second := make([]EvalCase, 5000)
		for index := 0; index < 10001; index++ {
			eval := makeCase(
				fmt.Sprintf("bulk-%05d", index), "bulk-topic", "development")
			if index < len(first) {
				first[index] = eval
			} else {
				second[index-len(first)] = eval
			}
		}
		writeTestJSON(t, filepath.Join(evalRoot, "first.json"), first)
		writeTestJSON(t, filepath.Join(evalRoot, "second.json"), second)
		service := newAdversarialService(t, root, nil)
		if _, err := service.loadProjectEvalCases(); err == nil {
			t.Fatal("aggregate project eval loader accepted more than 10,000 cases")
		}
	})
}

func TestAdversarialDerivedCacheWritersRejectLinkEscapes(t *testing.T) {
	prepare := func(t *testing.T) (string, *Service) {
		t.Helper()
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		generation, _, _, _, err := service.ensure(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		writeCalibrationCases(
			t, root, adversarialProjectBenchmarkCases(generation.CorpusFingerprint))
		return root, service
	}
	assertOutsideEmpty := func(t *testing.T, outside string) {
		t.Helper()
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			t.Fatalf("derived cache writer escaped through link: %v", names)
		}
	}

	t.Run("telemetry", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		cacheRoot := service.Index.CacheRoot
		if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if !makeDirectoryLink(
			t, outside, filepath.Join(cacheRoot, "telemetry")) {
			t.Skip("directory links are unavailable")
		}
		service.recordTelemetry(telemetryObservation{
			Operation: "search", EffectiveProfile: "balanced-v1",
			EstimatedTokens: 10, Results: 1,
		})
		assertOutsideEmpty(t, outside)
	})

	t.Run("project benchmark report", func(t *testing.T) {
		_, service := prepare(t)
		outside := t.TempDir()
		if !makeDirectoryLink(
			t, outside, filepath.Join(service.Index.CacheRoot, "benchmarks")) {
			t.Skip("directory links are unavailable")
		}
		if _, err := service.RunProjectBenchmark(
			context.Background(), "quick"); err == nil {
			t.Fatal("project benchmark wrote through an escaping cache link")
		}
		assertOutsideEmpty(t, outside)
	})

	t.Run("calibration artifacts", func(t *testing.T) {
		_, service := prepare(t)
		outside := t.TempDir()
		calibrationRoot := filepath.Join(service.Index.CacheRoot, "..", "calibration")
		if !makeDirectoryLink(t, outside, calibrationRoot) {
			t.Skip("directory links are unavailable")
		}
		if _, err := service.Calibrate(context.Background()); err == nil {
			t.Fatal("calibration wrote through an escaping cache link")
		}
		assertOutsideEmpty(t, outside)
	})
}

func TestAdversarialWriterLockCannotAliasOutsideSentinel(t *testing.T) {
	tests := []struct {
		name string
		link func(*testing.T, string, string) bool
	}{
		{
			name: "symbolic link",
			link: func(t *testing.T, target, link string) bool {
				return makeFileLink(t, target, link)
			},
		},
		{
			name: "hard link",
			link: func(t *testing.T, target, link string) bool {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, link); err != nil {
					t.Logf("hard links are unavailable: %v", err)
					return false
				}
				return true
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			root := makeAdversarialProject(t)
			service := newAdversarialService(t, root, nil)
			cacheRoot := service.Index.CacheRoot
			if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "sentinel.txt")
			sentinel := []byte("OUTSIDE_WRITER_LOCK_SENTINEL\n")
			if err := os.WriteFile(outside, sentinel, 0o600); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(cacheRoot, "writer.lock")
			if !testCase.link(t, outside, lockPath) {
				t.Skip(testCase.name + " unavailable")
			}

			_, _, _, reconcileErr := service.Index.Reconcile(context.Background())
			after, err := os.ReadFile(outside)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, sentinel) {
				t.Fatal("writer lock followed or truncated an outside sentinel")
			}
			if reconcileErr == nil {
				lockInfo, lockErr := os.Stat(lockPath)
				outsideInfo, outsideErr := os.Stat(outside)
				if lockErr != nil || outsideErr != nil ||
					os.SameFile(lockInfo, outsideInfo) {
					t.Fatal("successful reconciliation retained an aliased writer lock")
				}
			}
		})
	}
}

func TestAdversarialCurrentGenerationPointerAndEmbeddedMetadataAreAuthenticated(t *testing.T) {
	t.Run("current pointer symlink is rejected without touching target", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		if _, _, _, _, err := service.ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
		currentPath := filepath.Join(service.Index.CacheRoot, "current.json")
		currentBody, err := os.ReadFile(currentPath)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside-current.json")
		if err := os.WriteFile(outside, currentBody, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(currentPath); err != nil {
			t.Fatal(err)
		}
		if !makeFileLink(t, outside, currentPath) {
			t.Skip("file links are unavailable")
		}
		if _, err := service.Index.LoadCurrent(); err == nil {
			t.Fatal("current pointer loader followed an escaping symlink")
		}
		after, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, currentBody) {
			t.Fatal("current pointer loader modified linked outside target")
		}
	})

	t.Run("oversized current pointer is rejected before decoding", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		if _, _, _, _, err := service.ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
		currentPath := filepath.Join(service.Index.CacheRoot, "current.json")
		if err := os.WriteFile(
			currentPath, []byte(strings.Repeat(" ", int(maxSourceBytes)+1)),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Index.LoadCurrent(); err == nil ||
			(!strings.Contains(strings.ToLower(err.Error()), "size") &&
				!strings.Contains(strings.ToLower(err.Error()), "large") &&
				!strings.Contains(strings.ToLower(err.Error()), "limit")) {
			t.Fatalf("oversized current pointer was not rejected by an explicit size gate: %v", err)
		}
	})

	for _, mutation := range []struct {
		name   string
		mutate func(*Generation)
	}{
		{
			name: "pointer generation identity",
			mutate: func(generation *Generation) {
				generation.ID = "generation-" + strings.Repeat("0", 20)
			},
		},
		{
			name: "pointer source state",
			mutate: func(generation *Generation) {
				if len(generation.SourceStates) == 0 {
					panic("generation has no source states")
				}
				generation.SourceStates = append(
					[]SourceState(nil), generation.SourceStates...)
				generation.SourceStates[0].Size++
			},
		},
	} {
		t.Run(mutation.name+" mismatch is rejected and recovered", func(t *testing.T) {
			root := makeAdversarialProject(t)
			service := newAdversarialService(t, root, nil)
			original, _, _, _, err := service.ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			forged := original
			mutation.mutate(&forged)
			writeTestJSON(t,
				filepath.Join(service.Index.CacheRoot, "current.json"), forged)
			if _, err := service.Index.LoadCurrent(); err == nil {
				t.Fatal("generation pointer disagreed with SQLite metadata but was accepted")
			}
			recovered, _, _, _, err := service.ensure(context.Background())
			if err != nil {
				t.Fatalf("generation pointer mismatch did not recover safely: %v", err)
			}
			if recovered.ID == forged.ID && forged.ID != original.ID {
				t.Fatal("forged generation identity was served after recovery")
			}
			current, err := service.Index.LoadCurrent()
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := readGenerationMetadata(current.Database)
			if err != nil {
				t.Fatal(err)
			}
			if stableJSON(current) != stableJSON(embedded) {
				t.Fatal("recovered current pointer still disagrees with embedded metadata")
			}
		})
	}

	t.Run("embedded metadata mismatch cannot pass quick-check freshness", func(t *testing.T) {
		root := makeAdversarialProject(t)
		service := newAdversarialService(t, root, nil)
		original, _, _, _, err := service.ensure(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := readGenerationMetadata(original.Database)
		if err != nil {
			t.Fatal(err)
		}
		embedded.CorpusFingerprint = "sha256:" + strings.Repeat("0", 64)
		body, err := json.Marshal(embedded)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", original.Database)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(
			`UPDATE metadata SET value=? WHERE key='generation'`, string(body),
		); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyDatabase(original.Database); err != nil {
			t.Fatalf("fixture is not an otherwise valid SQLite database: %v", err)
		}
		if _, err := service.Index.LoadCurrent(); err == nil {
			t.Fatal("current pointer accepted mismatched embedded generation metadata")
		}
		recovered, _, rebuilt, err := service.Index.Ensure(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !rebuilt || recovered.CorpusFingerprint == embedded.CorpusFingerprint {
			t.Fatalf("forged embedded metadata was served as fresh: %#v", recovered)
		}
		current, err := service.Index.LoadCurrent()
		if err != nil {
			t.Fatal(err)
		}
		recoveredEmbedded, err := readGenerationMetadata(current.Database)
		if err != nil {
			t.Fatal(err)
		}
		if stableJSON(current) != stableJSON(recoveredEmbedded) {
			t.Fatal("rebuilt pointer does not match embedded generation metadata")
		}
	})
}

func TestAdversarialPackagedBenchmarkDoesNotDependOnProfileRowOrder(t *testing.T) {
	source := adversarialAssetRoot(t)
	assetRoot := t.TempDir()
	if err := copyTree(filepath.Join(source, "models"), filepath.Join(assetRoot, "models")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(source, "evals"), filepath.Join(assetRoot, "evals")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(source, "profiles", "balanced-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var profile RetrievalProfile
	if err := decodeStrict(body, &profile); err != nil {
		t.Fatal(err)
	}
	sort.Slice(profile.EffectiveProfiles, func(i, j int) bool {
		return profile.EffectiveProfiles[i].Name > profile.EffectiveProfiles[j].Name
	})
	writeTestJSON(t, filepath.Join(assetRoot, "profiles", "balanced-v1.json"), profile)

	quick, err := RunPackagedBenchmark(context.Background(), assetRoot, "quick")
	if err != nil {
		t.Fatal(err)
	}
	if len(quick.Profiles) != 1 ||
		quick.Profiles[0].ProfileName != "lexical-graph-v1" {
		t.Fatalf("quick benchmark selected a row by position: %#v", quick.Profiles)
	}
	full, err := RunPackagedBenchmark(context.Background(), assetRoot, "full")
	if err != nil {
		t.Fatal(err)
	}
	if !full.Passed || len(full.Profiles) != len(profile.EffectiveProfiles) {
		t.Fatalf("reordered full capability matrix failed: %#v", full)
	}
	baselineCount := 0
	for _, row := range full.Profiles {
		if row.ProfileName == "lexical-graph-v1" {
			baselineCount++
			if !row.NonInferiorToLexical {
				t.Error("lexical baseline was not identified by capability")
			}
		}
	}
	if baselineCount != 1 {
		t.Fatalf("full benchmark identified %d lexical baselines", baselineCount)
	}
}
