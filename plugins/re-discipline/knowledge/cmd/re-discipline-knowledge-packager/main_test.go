package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSupportedTargetMatrixIsCompleteAndOrdered(t *testing.T) {
	expected := []targetSpec{
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
	}
	if len(supportedTargets) != len(expected) {
		t.Fatalf("target count = %d, want %d", len(supportedTargets), len(expected))
	}
	for index := range expected {
		if supportedTargets[index] != expected[index] {
			t.Fatalf("target %d = %#v, want %#v", index, supportedTargets[index], expected[index])
		}
	}
}

func TestCapturedProjectSourceClassPreservesMeasurementTaxonomy(t *testing.T) {
	cases := map[string]string{
		"docs/truth/findings/F-0042.md":                  "truth",
		"docs/truth/legacy-topic.md":                     "truth",
		"docs/history/campaigns/example/findings/F-1.md": "history",
	}
	for path, want := range cases {
		if got := capturedProjectSourceClass(path); got != want {
			t.Errorf("capturedProjectSourceClass(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestStrictJSONDecoderRejectsDuplicateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.json")
	if err := os.WriteFile(path, []byte(`{"value":1,"value":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var target struct {
		Value int `json:"value"`
	}
	if _, err := decodeStrictJSONFile(path, "duplicate fixture", &target); err == nil ||
		!strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("duplicate JSON member was accepted: %v", err)
	}
}

func TestRuntimeBuildIDCommitsSharedRuntimeAssets(t *testing.T) {
	pluginRoot := t.TempDir()
	moduleRoot := filepath.Join(pluginRoot, "knowledge")
	directories := []string{
		"cmd/re-discipline-knowledge",
		"internal/knowledge",
	}
	directories = append(directories, sharedAssetRoots...)
	for _, relative := range directories {
		if err := os.MkdirAll(
			filepath.Join(moduleRoot, filepath.FromSlash(relative)), 0o755,
		); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		"go.mod":                              []byte("module example.test/runtime\n\ngo 1.26.5\n"),
		"go.sum":                              {},
		"THIRD_PARTY_NOTICES.md":              []byte("# Notices\n"),
		"../LICENSE":                          []byte("MIT License\n"),
		"profiles/balanced-v1.json":           []byte("{\"revision\":1}\n"),
		"cmd/re-discipline-knowledge/main.go": []byte("package main\n"),
		"internal/knowledge/runtime.go":       []byte("package knowledge\n"),
	}
	for relative, body := range files {
		path := filepath.Join(moduleRoot, filepath.FromSlash(relative))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if repeated != before {
		t.Fatalf("unchanged build inputs were nondeterministic: %s != %s", before, repeated)
	}
	if err := os.WriteFile(
		filepath.Join(moduleRoot, "profiles", "balanced-v1.json"),
		[]byte("{\"revision\":2}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	after, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("shared retrieval profile mutation did not change runtime build identity")
	}
}

func TestValidateOutputRootAcceptsOnlyModuleBin(t *testing.T) {
	moduleRoot := t.TempDir()
	expected := filepath.Join(moduleRoot, "bin")
	output, err := validateOutputRoot(moduleRoot, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(output, expected) {
		t.Fatalf("output = %s, want %s", output, expected)
	}
	for _, invalid := range []string{".", "..", "dist", filepath.Join(moduleRoot, "nested", "bin")} {
		if _, err := validateOutputRoot(moduleRoot, invalid); err == nil {
			t.Fatalf("unsafe output %q was accepted", invalid)
		}
	}
}

func TestPackagePathRejectsEscapesAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	for _, invalid := range []string{"", ".", "..", "../outside", "/outside"} {
		if runtime.GOOS == "windows" && invalid == "/outside" {
			invalid = `C:\outside`
		}
		if _, err := packagePath(root, invalid); err == nil {
			t.Fatalf("unsafe package path %q was accepted", invalid)
		}
	}
	path, err := packagePath(root, "linux-amd64/re-discipline-knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(path, filepath.Join(root, "linux-amd64", "re-discipline-knowledge")) {
		t.Fatalf("unexpected package path %s", path)
	}
}

func TestSharedAssetPathIsRestrictedToRuntimeAssetRoots(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{
		"evals/conformance/cases.json",
		"evals/conformance/finding-cases.json",
		"models/artifacts/model.bin",
		"models/manifest.json",
		"profiles/balanced-v1.json",
		"schemas/config.schema.json",
	} {
		valid, err := sharedAssetPath(root, relative)
		if err != nil {
			t.Fatal(err)
		}
		if !samePath(valid, filepath.Join(root, filepath.FromSlash(relative))) {
			t.Fatalf("unexpected shared asset path %s", valid)
		}
	}
	for _, invalid := range []string{
		"evals/conformance",
		"models",
		"../models/artifacts/model.bin",
		"models/artifacts/../../outside",
		"scripts/build_glove_artifact.py",
	} {
		if _, err := sharedAssetPath(root, invalid); err == nil {
			t.Fatalf("unsafe shared asset path %q was accepted", invalid)
		}
	}
}

func TestPackagedRuntimeAssetsCoverRequiredDataAndPinModelExactlyOnce(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	assets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"evals/conformance/cases.json":                                         "benchmark-cases",
		"evals/conformance/finding-cases.json":                                 "benchmark-cases",
		"evals/conformance/lane-ablation-decision.json":                        "lane-ablation-decision",
		"evals/conformance/lane-ablation-report.json":                          "lane-ablation-report",
		"evals/conformance/project-lane-ablation.json":                         "project-lane-ablation-measurement",
		"evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip": "lane-ablation-evidence-archive",
		"models/manifest.json":                                                 "model-manifest",
		"profiles/balanced-v1.json":                                            "retrieval-profile",
		"schemas/runtime-package-manifest.schema.json":                         "json-schema",
	}
	for _, asset := range assets {
		expectedKind, requiredPath := required[asset.Path]
		if requiredPath {
			if asset.Kind != expectedKind {
				t.Fatalf("asset %s kind = %s, want %s", asset.Path, asset.Kind, expectedKind)
			}
			delete(required, asset.Path)
		}
		if _, err := sharedAssetPath(moduleRoot, asset.Path); err != nil {
			t.Fatalf("manifested shared asset %s is outside allowed roots: %v", asset.Path, err)
		}
	}
	if len(required) != 0 {
		t.Fatalf("missing required shared assets: %v", required)
	}
}

func TestProjectLaneEvidenceIsDerivedFromPerCaseMeasurement(t *testing.T) {
	moduleRoot := t.TempDir()
	measurementFile := filepath.Join(moduleRoot, filepath.FromSlash(projectLaneMeasurementPath))
	if err := os.MkdirAll(filepath.Dir(measurementFile), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceModuleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	const archiveRelative = "evals/conformance/evidence/2026-08-03-snaphak-pre-removal-rerank.zip"
	sourceArchive := filepath.Join(sourceModuleRoot, filepath.FromSlash(archiveRelative))
	archiveBody, err := os.ReadFile(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	archiveFile := filepath.Join(moduleRoot, filepath.FromSlash(archiveRelative))
	if err := os.MkdirAll(filepath.Dir(archiveFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archiveFile, archiveBody, 0o644); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	memberIdentity := map[string]string{}
	memberSize := map[string]int64{}
	for _, file := range archive.File {
		switch file.Name {
		case "raw-benchmark.json", "projection-manifest.json", "profile-catalog.json", "model-manifest.json":
			reader, openErr := file.Open()
			if openErr != nil {
				archive.Close()
				t.Fatal(openErr)
			}
			body, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				archive.Close()
				t.Fatalf("read archive member %s: %v / %v", file.Name, readErr, closeErr)
			}
			memberIdentity[file.Name] = sha256Identity(body)
			memberSize[file.Name] = int64(len(body))
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	const (
		identity           = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		currentRevision    = "6666666666666666666666666666666666666666"
		historicalRevision = "215964342378678eaba3249fe0ae284c3a0622a4"
	)
	metrics := func(recall, complete float64) map[string]float64 {
		return map[string]float64{
			"recallAtK": recall, "completeEvidenceCoverage": complete,
		}
	}
	profile := func(
		role, name string,
		lanes []string,
		modelIDs []string,
		recall, complete, holdoutRecall, holdoutComplete float64,
	) projectLaneProfile {
		return projectLaneProfile{
			Role: role, Name: name, ActiveLanes: lanes,
			EffectiveIdentity: name + "@" + identity,
			ObservationDigest: identity, ModelIDs: modelIDs,
			HardGatesPassed: true, NonInferiorToLexical: true,
			Metrics: metrics(recall, complete),
			MetricsBySplit: map[string]map[string]float64{
				"development": metrics(recall, complete),
				"holdout":     metrics(holdoutRecall, holdoutComplete),
			},
		}
	}
	outcome := func(caseID, path string, hit bool) projectLaneOutcome {
		result := projectLaneOutcome{
			CaseID: caseID, Split: "holdout", Topic: "history-validation",
			AuthoritySafe: true, CitationMetadataSafe: true, CitationSafe: true,
			CorpusMatched: true, AbstentionCorrect: true, BudgetSafe: true,
			ReplayIdentical: true, MinimumTokenBudget: 128,
			QualityGateApplicable: true, SafetyPassed: true,
			LatencyMillis: 1,
		}
		if hit {
			result.Paths = []string{path}
			result.Tiers = []string{"history"}
			result.ChunkIDs = []string{"chunk-" + caseID}
			result.ContentHashes = []string{identity}
			result.RelevantPaths = []string{path}
			result.RelevantRanks = []int{1}
			result.ExpectedCitationsFound = []string{path}
			result.ReturnedUniquePaths = 1
			result.ExpectedFound = true
			result.CompleteEvidence = true
			result.QualityPassed = true
			result.GatePassed = true
			result.EstimatedTokens = 32
			result.ReturnedTokens = 32
			result.RelevantTokens = 32
		}
		return result
	}

	measurement := projectLaneMeasurement{
		Schema:        "plugin://re-discipline/schemas/project-lane-ablation-report.schema.json",
		SchemaVersion: 2, Kind: "project-retrieval-lane-ablation",
		MeasurementOnly: true, Project: "fixture-project",
		EvaluatedAt: "2026-08-03T07:00:00Z",
		Provenance: projectLaneProvenance{
			FrozenSourceRevision: currentRevision, ProjectGitRevision: strings.Repeat("7", 40),
			ProjectDirtyFingerprint: identity, FrozenRuntimeFingerprint: identity,
			RawBenchmarkSHA256: identity, RawBenchmarkRunID: "benchmark-current",
			RawBenchmarkComplete: true, RawBenchmarkArmCount: 2,
			RawBenchmarkCasesPerArm: 64, RawBenchmarkDurationMillis: 1,
			ParserVersion: "parser-v1", ChunkerVersion: "chunker-v1",
			ProfileCatalogSHA256: identity, ModelManifestSHA256: identity,
		},
		Corpus: projectLaneCorpus{
			GenerationID: "generation-11111111111111111111", CorpusFingerprint: identity,
			DocumentCount: 1, ChunkCount: 1, FinalEvalFileCount: 9,
			FinalEvalCaseCount: 64, ProjectedEvalFingerprint: identity,
			ProjectionManifestSHA256: identity, ProjectionTransformDigest: identity,
			ProjectionOperation:          "delete-whole-json-member-line-v1",
			ProjectionRemovedOccurrences: 1, ProjectionPreservesAllOtherBytes: true,
		},
		Harness: projectLaneHarness{
			IndexedSourceBytesUnchanged: true,
			ReceiptSHA256:               identity, PluginRevision: currentRevision,
			ProjectRevision: strings.Repeat("7", 40), BenchmarkCommandSHA256: identity,
			ProjectionManifestArtifactSHA256: identity, IndexedSourcesManifestSHA256: identity,
		},
		Models: []projectLaneModel{{
			ID: "embedding", Role: "embedding", Revision: "1",
			Implementation: "fixture", SpecSHA256: strings.Repeat("8", 64),
			ArtifactSHA256: strings.Repeat("9", 64),
		}},
		Profiles: []projectLaneProfile{
			profile("baseline", "lexical-graph-v1", []string{"exact", "fts", "graph"}, nil, 0.9, 0.9, 0.8, 0.8),
			profile("dense", "hybrid-no-rerank-v1", []string{"exact", "fts", "graph", "dense"}, []string{"embedding"}, 1, 1, 1, 1),
		},
		Uncertainty: projectLaneUncertainty{
			WilsonIntervals: []projectLaneWilsonInterval{
				{Slice: "all-cases", Event: "dense-unique-rescue", Successes: 1, Trials: 64, Confidence: 0.95, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
				{Slice: "answerable-cases", Event: "dense-unique-rescue", Successes: 1, Trials: 64, Confidence: 0.95, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
				{Slice: "holdout-cases", Event: "dense-unique-rescue", Successes: 1, Trials: 64, Confidence: 0.95, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
				{Slice: "target-disjoint-holdout", Event: "dense-unique-rescue", Successes: 1, Trials: 64, Confidence: 0.95, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
			},
			TopicClusterBootstrap: projectLaneBootstrap{
				Method: "topic-cluster-percentile-v1", ClusterKey: "topic", ClusterCount: 2,
				Replicates: 10000, Seed: 1, Estimand: "fixture", PointEstimate: 1.0 / 64,
				Lower: 0, Median: 1.0 / 64, Upper: 0.1, ZeroOrLowerFraction: 0.5,
			},
		},
		Sensitivity: projectLaneSensitivity{
			LeaveOneTopicOut: []projectLaneLeaveOneOut{
				{OmittedTopic: "other", AnswerableCases: 64, DenseHitRateDelta: 1.0 / 64},
			},
			DensePathChangedCases: 1,
		},
		HistoricalRerank: projectLaneHistoricalRerank{
			Provenance: projectLaneHistoricalProvenance{
				RuntimeSourceRevision: historicalRevision, ProjectGitRevision: strings.Repeat("8", 40),
				ProjectDirtyFingerprint: identity, RuntimeFingerprint: identity,
				RawBenchmarkSHA256:       memberIdentity["raw-benchmark.json"],
				EvidenceArchivePath:      archiveRelative,
				EvidenceArchiveSHA256:    sha256Identity(archiveBody),
				RawBenchmarkByteCount:    memberSize["raw-benchmark.json"],
				EvidenceArchiveByteCount: int64(len(archiveBody)),
				EvidenceArchiveFormat:    "zip-deflate-fixed-v1", RawBenchmarkRunID: "benchmark-historical",
				RawBenchmarkComplete: true, RawBenchmarkArmCount: 3, RawBenchmarkCasesPerArm: 64,
				RawBenchmarkDurationMillis: 1, GenerationID: "generation-22222222222222222222",
				CorpusFingerprint: identity, EvalFingerprint: identity,
				ParserVersion: "parser-v1", ChunkerVersion: "chunker-v1",
				ProfileCatalogSHA256:      memberIdentity["profile-catalog.json"],
				ModelManifestSHA256:       memberIdentity["model-manifest.json"],
				ProjectionManifestSHA256:  memberIdentity["projection-manifest.json"],
				ProjectionTransformDigest: identity,
			},
			Models: []projectLaneModel{
				{
					ID: "embedding", Role: "embedding", Revision: "1",
					Implementation: "fixture", SpecSHA256: strings.Repeat("8", 64),
					ArtifactSHA256: strings.Repeat("9", 64),
				},
				{
					ID: "reranker", Role: "reranker", Revision: "1",
					Implementation: "fixture", SpecSHA256: strings.Repeat("a", 64),
				},
			},
			Profiles: []projectLaneProfile{
				profile("baseline", "lexical-graph-v1", []string{"exact", "fts", "graph"}, nil, 0.9, 0.9, 0.8, 0.8),
				profile("dense", "hybrid-no-rerank-v1", []string{"exact", "fts", "graph", "dense"}, []string{"embedding"}, 1, 1, 1, 1),
				profile("rerank", "hybrid-local-v1", []string{"exact", "fts", "graph", "dense", "rerank"}, []string{"embedding", "reranker"}, 1, 1, 1, 1),
			},
			Decision: projectLaneDecision{Action: "remove", Rationale: "no historical contribution"},
		},
		Decision: projectLaneMeasurementDecision{
			Dense: projectLaneDecision{
				Action: "retain", PositiveEvidence: true, EventCount: 1,
				CaseIDs: []string{"case-00"}, Rationale: "one current rescue",
			},
			Rerank:                      projectLaneDecision{Action: "remove", Rationale: "no historical contribution"},
			ProductionLanes:             []string{"exact", "fts", "graph", "dense"},
			ProductionProfileConsistent: true, ReleaseGatePassed: true,
		},
	}
	for index := 0; index < 64; index++ {
		caseID := fmt.Sprintf("case-%02d", index)
		path := fmt.Sprintf("docs/history/case-%02d.md", index)
		baseline := outcome(caseID, path, index != 0)
		dense := outcome(caseID, path, true)
		measurement.Cases = append(measurement.Cases, projectLaneCase{
			CaseID: caseID, Split: "holdout", Role: "manager", Topic: "history-validation",
			QueryClass: "conceptual", VocabularyPolicy: "target-disjoint-v1", Answerable: true,
			Baseline: baseline, Dense: dense,
			DenseComparison: compareProjectOutcomes(baseline, dense),
		})
		measurement.HistoricalRerank.Cases = append(
			measurement.HistoricalRerank.Cases,
			projectLaneHistoricalCase{
				CaseID: caseID, Split: "holdout", Topic: "history-validation", Answerable: true,
				Dense: dense, Rerank: dense,
				RerankComparison: compareProjectOutcomes(dense, dense),
			},
		)
	}
	measurement.HistoricalRerank.Decision = measurement.Decision.Rerank

	measurementBody, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(measurementFile, measurementBody, 0o644); err != nil {
		t.Fatal(err)
	}
	measurementIdentity := sha256Identity(measurementBody)
	derived, err := deriveProjectLaneMeasurement(measurement)
	if err != nil {
		t.Fatal(err)
	}
	report := laneAblationReport{
		SourceRevision: historicalRevision,
		ProjectEvidence: laneAblationProjectEvidence{
			Suite: "project-benchmark-v1", Project: measurement.Project,
			Status: "final-corpus-verified", EvaluatedAt: measurement.EvaluatedAt,
			FrozenSourceRevision: currentRevision,
			RawBenchmarkSHA256:   measurement.Provenance.RawBenchmarkSHA256,
			RawBenchmarkRunID:    measurement.Provenance.RawBenchmarkRunID,
			Complete:             true, ArmCount: 2, CasesPerArm: 64,
			CorpusFingerprint:           measurement.Corpus.CorpusFingerprint,
			EvalFingerprint:             measurement.Corpus.ProjectedEvalFingerprint,
			ProjectionTransformDigest:   measurement.Corpus.ProjectionTransformDigest,
			ProjectMeasurementPath:      projectLaneMeasurementPath,
			ProjectMeasurementSHA256:    measurementIdentity,
			IndexedSourceBytesUnchanged: true,
			Dense:                       derived.Dense, Rerank: derived.Rerank, RescueCases: derived.Rescues,
			Uncertainty: laneAblationProjectUncertainty{
				WilsonIntervals: []laneAblationWilsonInterval{
					{Slice: "all-cases", Successes: 1, Trials: 64, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
					{Slice: "answerable-cases", Successes: 1, Trials: 64, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
					{Slice: "holdout-cases", Successes: 1, Trials: 64, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
					{Slice: "target-disjoint-holdout", Successes: 1, Trials: 64, Estimate: 1.0 / 64, Lower: 0, Upper: 0.1},
				},
				TopicBootstrap: laneAblationTopicBootstrap{
					Replicates: 10000, PointEstimate: 1.0 / 64, Lower: 0, Upper: 0.1,
					ZeroOrLowerFraction: 0.5,
				},
			},
		},
	}
	receipt := laneAblationDecision{
		EvidenceLayers: laneAblationDecisionLayers{
			ProjectCorpus: laneAblationProjectDecisionLayer{
				Status: "final-corpus-verified", CasesPerArm: 64,
				RawBenchmarkSHA256:       measurement.Provenance.RawBenchmarkSHA256,
				ProjectMeasurementPath:   projectLaneMeasurementPath,
				ProjectMeasurementSHA256: measurementIdentity,
				Lanes: map[string]laneAblationProjectCounts{
					"dense": {
						Rescues: derived.Dense.Rescues, Losses: derived.Dense.Losses,
						RankImprovements: derived.Dense.RankImprovements,
						RankDegradations: derived.Dense.RankDegradations,
					},
					"rerank": {},
				},
			},
		},
		Lanes: map[string]laneAblationFinalChoice{
			"dense":  {Decision: "retain", Basis: "project-corpus-rescues"},
			"rerank": {Decision: "remove", Basis: "no-measured-benefit-across-both-layers"},
		},
	}
	if err := validateProjectLaneEvidence(moduleRoot, report, receipt); err != nil {
		t.Fatalf("generic per-case project evidence was rejected: %v", err)
	}

	fabricated := receipt
	fabricated.EvidenceLayers.ProjectCorpus.Lanes = map[string]laneAblationProjectCounts{
		"dense": {Rescues: 2}, "rerank": {},
	}
	if err := validateProjectLaneEvidence(moduleRoot, report, fabricated); err == nil ||
		!strings.Contains(err.Error(), "recomputed per-case evidence") {
		t.Fatalf("fabricated project rescue count was accepted: %v", err)
	}

	measurement.Harness.ProjectRevision = strings.Repeat("5", 40)
	unboundBody, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(measurementFile, unboundBody, 0o644); err != nil {
		t.Fatal(err)
	}
	unboundIdentity := sha256Identity(unboundBody)
	report.ProjectEvidence.ProjectMeasurementSHA256 = unboundIdentity
	receipt.EvidenceLayers.ProjectCorpus.ProjectMeasurementSHA256 = unboundIdentity
	if err := validateProjectLaneEvidence(moduleRoot, report, receipt); err == nil ||
		!strings.Contains(err.Error(), "incomplete current-runtime identity") {
		t.Fatalf("unbound staging project revision was accepted: %v", err)
	}
	measurement.Harness.ProjectRevision = measurement.Provenance.ProjectGitRevision

	measurement.Decision.Dense.Action = "remove"
	staleBody, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(measurementFile, staleBody, 0o644); err != nil {
		t.Fatal(err)
	}
	staleIdentity := sha256Identity(staleBody)
	report.ProjectEvidence.ProjectMeasurementSHA256 = staleIdentity
	receipt.EvidenceLayers.ProjectCorpus.ProjectMeasurementSHA256 = staleIdentity
	if err := validateProjectLaneEvidence(moduleRoot, report, receipt); err == nil ||
		!strings.Contains(err.Error(), "stale dense decision") {
		t.Fatalf("stale per-case dense decision was accepted: %v", err)
	}
}

func TestSharedRuntimeAssetsRejectUnclassifiedFiles(t *testing.T) {
	for _, relative := range []string{
		"evals/conformance/fixture/secret.bin",
		"models/artifacts/notes.txt",
		"models/specs/model.tmp",
		"profiles/profile.txt",
		"schemas/schema.md",
	} {
		if _, err := sharedAssetKind(relative); err == nil {
			t.Fatalf("unclassified shared runtime asset %q was accepted", relative)
		}
	}
}

func TestFirstPartyBuiltinModelsRequireMITLicense(t *testing.T) {
	if err := verifyFirstPartyModelLicense(
		"builtin:linear-reranker-v1", "builtin", "MIT",
	); err != nil {
		t.Fatal(err)
	}
	for _, license := range []string{"", "MIT License", "all rights reserved"} {
		if err := verifyFirstPartyModelLicense(
			"builtin:linear-reranker-v1", "builtin", license,
		); err == nil {
			t.Fatalf("conflicting first-party license %q was accepted", license)
		}
	}
	if err := verifyFirstPartyModelLicense(
		"builtin:glove-6b-50d-top50k-q8-v1", "bundled-local", "PDDL-1.0",
	); err != nil {
		t.Fatal(err)
	}
}

func TestSharedAssetVerificationRejectsTamperedProfile(t *testing.T) {
	moduleRoot := t.TempDir()
	profilePath := filepath.Join(moduleRoot, "profiles", "balanced-v1.json")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"revision\":1}\n")
	if err := os.WriteFile(profilePath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	assets := []manifestFile{{
		Kind:   "retrieval-profile",
		Path:   "profiles/balanced-v1.json",
		SHA256: "sha256:" + digest,
		Size:   int64(len(original)),
		Mode:   "0644",
	}}
	if err := verifySharedAssetFiles(moduleRoot, assets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("{\"revision\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySharedAssetFiles(moduleRoot, assets); err == nil {
		t.Fatal("tampered retrieval profile passed shared-asset verification")
	}
}

func TestPOSIXLauncherDispatchesEverySupportedPOSIXTarget(t *testing.T) {
	for _, fragment := range []string{
		"Linux) platform=linux",
		"Darwin) platform=darwin",
		"x86_64|amd64) architecture=amd64",
		"arm64|aarch64) architecture=arm64",
		`exec "$runtime_path" "$@"`,
	} {
		if !contains(posixLauncher, fragment) {
			t.Fatalf("POSIX launcher lacks %q", fragment)
		}
	}
}

func TestCanonicalWindowsLauncherCopyIsByteExact(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "re-discipline-knowledge.exe")
	if err := copyCanonicalWindowsLauncher(
		moduleRoot, destination, pinnedGo, buildID,
	); err != nil {
		t.Fatal(err)
	}
	sourceBody, err := os.ReadFile(filepath.Join(moduleRoot, "bin", "re-discipline-knowledge.exe"))
	if err != nil {
		t.Fatal(err)
	}
	destinationBody, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(destinationBody, sourceBody) {
		t.Fatal("canonical Windows launcher copy was not byte-exact")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != windowsArtifactMode {
			t.Fatalf(
				"canonical Windows launcher mode = %04o, want %04o",
				info.Mode().Perm(),
				windowsArtifactMode,
			)
		}
	}
}

func TestWindowsLauncherEmbedsAndValidatesRuntimeBuildIdentity(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "sha256:" + strings.Repeat("a", 64)
	outputRoot := t.TempDir()
	destination := filepath.Join(outputRoot, "re-discipline-knowledge.exe")
	if err := buildGoBinaryWithIdentityPath(
		moduleRoot,
		destination,
		windowsLauncherTarget,
		"./cmd/re-discipline-knowledge-launcher",
		pinnedGo,
		windowsLauncherBuildIDPath,
		expected,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyWindowsLauncherBuildIdentity(
		outputRoot, pinnedGo, expected,
	); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(expected)) {
		t.Fatal("Windows architecture dispatcher omitted its release build identity")
	}
}

func TestWindowsLauncherIdentityValidationRejectsStaleOrMissing(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "sha256:" + strings.Repeat("a", 64)
	for _, testCase := range []struct {
		name    string
		buildID string
	}{
		{name: "missing"},
		{name: "stale", buildID: "sha256:" + strings.Repeat("b", 64)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outputRoot := t.TempDir()
			if err := buildGoBinaryWithIdentityPath(
				moduleRoot,
				filepath.Join(outputRoot, "re-discipline-knowledge.exe"),
				windowsLauncherTarget,
				"./cmd/re-discipline-knowledge-launcher",
				pinnedGo,
				windowsLauncherBuildIDPath,
				testCase.buildID,
			); err != nil {
				t.Fatal(err)
			}
			err := verifyWindowsLauncherBuildIdentity(
				outputRoot, pinnedGo, expected,
			)
			if err == nil || !strings.Contains(err.Error(), "omits compiled build identity") {
				t.Fatalf("%s dispatcher identity returned %v", testCase.name, err)
			}
		})
	}
}

func TestBuiltPackageCarriesWindowsLauncherIdentityIntoManifestAndChecksums(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-hosted package build is the canonical PE producer")
	}
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := t.TempDir()
	manifest, err := buildPackageTree(moduleRoot, outputRoot, pinnedGo, buildID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyPackageContents(
		moduleRoot, outputRoot, pinnedGo, buildID,
	); err != nil {
		t.Fatal(err)
	}
	launcher := manifest.Launchers[1]
	digest, err := fileSHA256(filepath.Join(outputRoot, launcher.Path))
	if err != nil {
		t.Fatal(err)
	}
	if launcher.SHA256 != "sha256:"+digest {
		t.Fatalf("dispatcher manifest digest = %s, want sha256:%s", launcher.SHA256, digest)
	}
	sums, err := os.ReadFile(filepath.Join(outputRoot, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	wantRow := []byte(digest + "  re-discipline-knowledge.exe\n")
	if !bytes.Contains(sums, wantRow) {
		t.Fatalf("SHA256SUMS omitted dispatcher row %q", wantRow)
	}
}

func TestCanonicalWindowsRuntimeCopiesAreByteExact(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range supportedTargets {
		if target.GOOS != "windows" {
			continue
		}
		relative := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH,
			targetBinaryName(target.GOOS),
		))
		destination := filepath.Join(t.TempDir(), target.GOARCH, targetBinaryName(target.GOOS))
		if err := copyCanonicalWindowsBinary(
			moduleRoot, relative, destination, target, pinnedGo, buildID,
		); err != nil {
			t.Fatal(err)
		}
		sourceBody, err := os.ReadFile(filepath.Join(moduleRoot, "bin", filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		destinationBody, err := os.ReadFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(destinationBody, sourceBody) {
			t.Fatalf("canonical Windows %s runtime copy was not byte-exact", target.GOARCH)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != windowsArtifactMode {
				t.Fatalf(
					"canonical Windows %s runtime mode = %04o, want %04o",
					target.GOARCH,
					info.Mode().Perm(),
					windowsArtifactMode,
				)
			}
		}
	}
}

func TestCanonicalWindowsLauncherCopyRequiresExistingArtifact(t *testing.T) {
	err := copyCanonicalWindowsLauncher(
		t.TempDir(),
		filepath.Join(t.TempDir(), "re-discipline-knowledge.exe"),
		"go1.26.0",
		"sha256:"+strings.Repeat("a", 64),
	)
	if err == nil || !strings.Contains(err.Error(), "generate knowledge/bin on Windows first") {
		t.Fatalf("missing canonical Windows launcher error = %v", err)
	}
}

func TestBuildEnvironmentPinsReleaseInputs(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOAMD64", "v4")
	t.Setenv("GOARM64", "v9.5")
	t.Setenv("GOENV", "host-environment")
	t.Setenv("GOEXPERIMENT", "host-value")
	t.Setenv("GOFIPS140", "latest")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "host-workspace")

	environment := buildEnvironment("linux", "amd64", "go1.26.5")
	values := map[string]string{}
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[strings.ToUpper(key)] = value
		}
	}
	expected := map[string]string{
		"CGO_ENABLED":  "0",
		"GOAMD64":      "v1",
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "-mod=readonly",
		"GOOS":         "linux",
		"GOARCH":       "amd64",
		"GOTOOLCHAIN":  "go1.26.5",
		"GOWORK":       "off",
	}
	for key, value := range expected {
		if values[key] != value {
			t.Fatalf("%s = %q, want %q", key, values[key], value)
		}
	}
}

func TestNoticeCoverageRequiresExactToolchainAndLinkedModules(t *testing.T) {
	notices := []byte(`| Component | Version | License |
|---|---:|---|
| Go standard library | go1.26.5 | BSD-3-Clause |
| ` + "`example.test/dependency`" + ` | v1.2.3 | MIT |
`)
	dependencies := [][2]string{{"example.test/dependency", "v1.2.3"}}
	if err := verifyNoticeCoverage(notices, "go1.26.5", dependencies); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoticeCoverage(notices, "go1.26.4", dependencies); err == nil {
		t.Fatal("mismatched release compiler was accepted")
	}
	if err := verifyNoticeCoverage(
		notices,
		"go1.26.5",
		append(dependencies, [2]string{"example.test/omitted", "v4.5.6"}),
	); err == nil {
		t.Fatal("omitted linked dependency was accepted")
	}
	withUnlinked := append(
		append([]byte{}, notices...),
		[]byte("| `example.test/unlinked` | v7.8.9 | MIT |\n")...,
	)
	if err := verifyNoticeCoverage(withUnlinked, "go1.26.5", dependencies); err == nil {
		t.Fatal("unlinked module notice was accepted as a linked dependency row")
	}
}

func TestReleaseNoticesReproduceFirstPartyLicense(t *testing.T) {
	license := []byte("MIT License\r\n\r\nCopyright (c) 2026 Example\r\n")
	notices := []byte("# Notices\n\nMIT License\n\nCopyright (c) 2026 Example\n")
	if err := verifyFirstPartyLicenseCoverage(notices, license); err != nil {
		t.Fatal(err)
	}
	if err := verifyFirstPartyLicenseCoverage(
		[]byte("# Notices\n\nMIT License\n"), license,
	); err == nil {
		t.Fatal("truncated first-party license was accepted")
	}
	if err := verifyFirstPartyLicenseCoverage(notices, []byte(" \r\n")); err == nil {
		t.Fatal("empty first-party license was accepted")
	}
}

func TestUnexpectedPackageDirectoriesAreRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "linux-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "linux-amd64", "re-discipline-knowledge"),
		[]byte("fixture"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{
		"linux-amd64/re-discipline-knowledge": true,
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoUnexpectedPackageFiles(root, expected); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "unexpected"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoUnexpectedPackageFiles(root, expected); err == nil {
		t.Fatal("unexpected empty package directory was accepted")
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
