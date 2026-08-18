package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adiaz/re-discipline-knowledge/internal/knowledge"
)

func TestMigrationCLIExposesSharedEngineAndRejectsFabricatedCertification(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	writeMigrationCLIFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Fixture\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy\n<!-- re-discipline:shared-laws:end -->\n")
	writeMigrationCLIFile(t, filepath.Join(root, "active", "live", "CAMPAIGN.md"),
		"# Campaign: live\n\n## Objective\n\nExercise CLI migration parity.\n")
	reportPath := filepath.Join(root, "active", "live", "subagents", "worker", "report.md")
	writeMigrationCLIFile(t, reportPath, "# VERDICT\n\nDIRECT: CLI and MCP use the shared migration engine.\n")
	writeMigrationCLIFile(t, filepath.Join(root, "docs", "backlog", "development.md"),
		"# CLI development fixture\n\nThe CLI fixture retains its development item.\n")
	writeMigrationCLIFile(t, filepath.Join(root, "docs", "backlog", "holdout.md"),
		"# CLI holdout fixture\n\nThe CLI fixture retains its holdout item.\n")
	answerable := true
	evalCases := []knowledge.EvalCase{
		{ID: "cli-migration-development", Role: "manager", Topic: "cli-migration-development",
			Split: "development", Query: "Which CLI development item is retained?", QueryClass: "conceptual",
			AllowedTiers: []string{"backlog"}, CorpusSnapshot: "fixture:packaged-conformance-v1",
			ExpectedPaths: []string{"docs/backlog/development.md"}, MinimumEvidencePaths: []string{"docs/backlog/development.md"},
			HardNegativePaths: []string{}, ExpectedCitations: []string{"docs/backlog/development.md"},
			ForbiddenTiers: []string{"draft"}, TokenBudget: 1024, Answerable: &answerable},
		{ID: "cli-migration-holdout", Role: "drafter", Topic: "cli-migration-holdout",
			Split: "holdout", Query: "Which CLI holdout item is retained?", QueryClass: "provenance",
			AllowedTiers: []string{"backlog"}, CorpusSnapshot: "fixture:packaged-conformance-v1",
			ExpectedPaths: []string{"docs/backlog/holdout.md"}, MinimumEvidencePaths: []string{"docs/backlog/holdout.md"},
			HardNegativePaths: []string{}, ExpectedCitations: []string{"docs/backlog/holdout.md"},
			ForbiddenTiers: []string{"draft"}, TokenBudget: 1024, Answerable: &answerable},
	}
	evalBody, err := json.MarshalIndent(evalCases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeMigrationCLIFile(t, filepath.Join(root, ".re-discipline", "knowledge", "evals", "cases.json"),
		string(append(evalBody, '\n')))
	commitMigrationCLIFixture(t, root)

	var preview knowledge.MigrationPreview
	runMigrationCLIJSON(t, &preview, "--project", root, "--preview", "--live-campaigns", "live")
	direct, err := knowledge.PreviewMigration(root, []string{"live"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.PlanDigest != direct.Plan.PlanDigest || preview.Receipt.Digest != direct.Receipt.Digest {
		t.Fatal("CLI preview diverged from the shared engine result")
	}

	var state knowledge.MigrationState
	runMigrationCLIJSON(t, &state, "--project", root, "--apply", preview.Plan.PlanDigest,
		"--live-campaigns", "live", "--actor", "manager")
	if state.State != "inventoried" {
		t.Fatalf("apply: %+v", state)
	}
	runMigrationCLIJSON(t, &state, "--project", root, "--resume", state.TransactionID)
	if state.State != "shadow-indexed" {
		t.Fatalf("shadow: %+v", state)
	}
	var shadow knowledge.MigrationShadowQueryResult
	runMigrationCLIJSON(t, &shadow, "--project", root, "--shadow-query", "shared migration engine",
		"--shadow-campaign", "live", "--shadow-limit", "3")
	if len(shadow.Matches) != 1 || shadow.CatalogDigest == "" {
		t.Fatalf("shadow query: %+v", shadow)
	}

	var source knowledge.MigrationSource
	for _, candidate := range preview.Plan.Sources {
		if candidate.Path == "active/live/subagents/worker/report.md" {
			source = candidate
		}
	}
	if source.Path == "" {
		t.Fatal("preview omitted report source")
	}
	coverage := knowledge.MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []knowledge.CoverageEntry{{
			SourceHandle: "path:" + source.Destination + "#L1-L3",
			SourcePath:   source.Destination, SourceSHA256: "sha256:" + source.SHA256,
			StartLine: 1, EndLine: 3, SourceLineCount: 3,
			Disposition: "candidate-finding", TargetID: "F-0001",
			Rationale: "The complete three-line fixture is represented by the single adapter-parity candidate.",
		}},
		FindingIDs: []string{"F-0001"}, Reviewer: "manager", Rationale: "CLI parity fixture",
		Findings: []knowledge.MigrationFindingInput{{
			ID: "F-0001", Kind: "observation", Subject: "migration adapter parity",
			Claim: "The CLI invokes the shared migration engine.", Scope: map[string]any{"component": "migration"},
			KnownLimits: []string{"Disposable fixture only."}, EvidenceGrade: "direct",
			CuratorAttestation: knowledge.MigrationFindingCuratorAttestation{
				SingleIndependentlyOverturnableClaim: true,
				EvidenceGradeAppliesToEntireClaim:    true,
				EntireSourceSpanRepresented:          true,
				SemanticBoundariesVerified:           true,
				LegacyReviewLanguageProvenanceOnly:   true,
				ManagerAttentionRequired:             true,
				Rationale:                            "The fixture isolates one direct adapter-parity claim for independent manager attention.",
			},
			SyntheticQuestions: []string{
				"Does the CLI invoke the shared migration engine?",
				"Which adapter performed the fixture migration?",
				"How is CLI migration parity checked?",
			},
		}},
	}
	coverageBody, err := json.MarshalIndent(coverage, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	coverageFile := filepath.Join(t.TempDir(), "coverage.json")
	writeMigrationCLIFile(t, coverageFile, string(append(coverageBody, '\n')))
	var accepted knowledge.MigrationCoverageReceipt
	runMigrationCLIJSON(t, &accepted, "--project", root, "--coverage", coverageFile)
	if accepted.Digest == "" {
		t.Fatal("CLI coverage did not return its canonical receipt")
	}

	runMigrationCLIJSON(t, &state, "--project", root, "--resume", state.TransactionID)
	if state.State != "normalized" {
		t.Fatalf("normalize: %+v", state)
	}
	runMigrationCLIJSON(t, &state, "--project", root, "--resume", state.TransactionID)
	if state.State != "physically-reorganized" {
		t.Fatalf("activate: %+v", state)
	}

	var baseline knowledge.MigrationCertification
	runMigrationCLIJSON(t, &baseline, "--project", root, "--verify")
	schemaFingerprint, err := knowledge.CanonicalDigest(baseline.SchemaVersions)
	if err != nil {
		t.Fatal(err)
	}
	checksByGate := map[string][]string{
		"structural":         {"canonical-records", "stable-ids", "relations", "payload-digests", "event-chain", "legacy-write-paths"},
		"semantic-traversal": {"claim-coverage", "source-provenance", "evidence-resolution", "scope-confidence", "conflicts-dependencies", "bounded-query"},
		"retrieval-context":  {"factual-accuracy", "evidence-selection", "durability-labeling", "context-token-budget", "full-corpus-independence", "blinded-agent-evaluation"},
		"host-parity":        {"mcp-schema", "cli-status", "claude-host", "codex-host", "semantic-parity", "role-truth-boundaries", "bounded-recovery", "local-fallback"},
	}
	fingerprintsByGate := map[string]map[string]string{
		"structural":         {"schema": schemaFingerprint, "destination-state": baseline.DestinationStateHead},
		"semantic-traversal": {"inventory": baseline.InventoryDigest, "coverage": baseline.CoverageDigest},
	}
	for _, gate := range []string{"structural", "semantic-traversal", "host-parity"} {
		var artifact knowledge.MigrationGateArtifact
		if gate == "host-parity" {
			artifact = strictMigrationCLIGateArtifact(t, root, state, gate)
		} else {
			checks := make([]knowledge.MigrationGateCheck, 0, len(checksByGate[gate]))
			for _, name := range checksByGate[gate] {
				checks = append(checks, knowledge.MigrationGateCheck{Name: name, Passed: true, Evidence: "disposable CLI measurement"})
			}
			artifact = knowledge.MigrationGateArtifact{
				SchemaVersion: knowledge.MigrationSchemaVersion, TransactionID: state.TransactionID,
				PlanDigest: state.PlanDigest, Gate: gate, Passed: true,
				Checks: checks, Fingerprints: fingerprintsByGate[gate],
			}
			artifact.ResultDigest, err = knowledge.CanonicalDigest(artifact)
			if err != nil {
				t.Fatal(err)
			}
		}
		artifactBody, err := json.MarshalIndent(artifact, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		artifactBody = append(artifactBody, '\n')
		relative := filepath.ToSlash(filepath.Join("migration-tests", gate+".json"))
		writeMigrationCLIFile(t, filepath.Join(root, filepath.FromSlash(relative)), string(artifactBody))
		var receipt knowledge.MigrationGateReceipt
		runMigrationCLIJSON(t, &receipt, "--project", root, "--gate", gate, "--gate-passed",
			"--artifact", relative, "--artifact-digest", "sha256:"+knowledge.SHA256Bytes(artifactBody), "--reviewer", "manager")
		if receipt.Digest == "" {
			t.Fatalf("gate %s returned no receipt", gate)
		}
	}

	// A CLI transport test must not invent semantic measurements. Submit the
	// historical fabricated fixture once and prove the shared engine rejects it
	// after independently rederiving the post-activation corpus and eval set.
	fabricated := strictMigrationCLIGateArtifact(t, root, state, "retrieval-context")
	fabricatedBody, err := json.MarshalIndent(fabricated, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fabricatedBody = append(fabricatedBody, '\n')
	fabricatedRelative := "migration-tests/retrieval-context-fabricated.json"
	writeMigrationCLIFile(t, filepath.Join(root, filepath.FromSlash(fabricatedRelative)), string(fabricatedBody))
	err = runMigrationCLIError("--project", root, "--gate", "retrieval-context", "--gate-passed",
		"--artifact", fabricatedRelative,
		"--artifact-digest", "sha256:"+knowledge.SHA256Bytes(fabricatedBody), "--reviewer", "manager")
	if err == nil || !strings.Contains(err.Error(), "retrieval-context evidence") {
		t.Fatalf("CLI accepted fabricated retrieval certification or returned the wrong boundary error: %v", err)
	}

	var certification knowledge.MigrationCertification
	runMigrationCLIJSON(t, &certification, "--project", root, "--verify")
	if certification.Candidate || !strings.Contains(strings.Join(certification.Blockers, "\n"), "retrieval-context") {
		t.Fatalf("verify did not preserve the strict retrieval blocker: %+v", certification)
	}
}

func TestMigrationCLIExportsAndSubmitsReviewedTruthSplit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	writeMigrationCLIFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Fixture\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy\n<!-- re-discipline:shared-laws:end -->\n")
	writeMigrationCLIFile(t, filepath.Join(root, "active", "live", "CAMPAIGN.md"), "# Campaign: live\n")
	first := strings.TrimSpace(strings.Repeat("first source assertion remains exact ", 16))
	second := strings.TrimSpace(strings.Repeat("second source assertion remains exact ", 16))
	writeMigrationCLIFile(t, filepath.Join(root, "docs", "truth", "compound.md"),
		"# Compound truth\n\n**Claim:** "+first+" "+second+"\n")

	var packet knowledge.MigrationTruthConflictPacket
	runMigrationCLIJSON(t, &packet, "--project", root, "--truth-conflicts")
	if len(packet.Conflicts) != 1 || packet.Digest == "" {
		t.Fatalf("CLI conflict packet: %+v", packet)
	}
	submission := knowledge.MigrationTruthReviewSubmission{
		SchemaVersion: knowledge.MigrationSchemaVersion, PacketDigest: packet.Digest,
		SourcePath: packet.Conflicts[0].SourcePath, SourceDigest: packet.Conflicts[0].SourceDigest,
		Reviewer: "manager", Rationale: "CLI-reviewed independent assertions",
		Claims: []knowledge.MigrationTruthAtomicClaim{
			{SourceText: first, Title: "First assertion", Claim: "The first assertion remains exact.",
				SyntheticQuestions: []string{"What remains exact first?", "Which first assertion is accepted?", "How is the first assertion treated?"}},
			{SourceText: second, Title: "Second assertion", Claim: "The second assertion remains exact.",
				SyntheticQuestions: []string{"What remains exact second?", "Which second assertion is accepted?", "How is the second assertion treated?"}},
		},
	}
	body, err := json.MarshalIndent(submission, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := filepath.Join(t.TempDir(), "truth-review.json")
	writeMigrationCLIFile(t, reviewPath, string(append(body, '\n')))
	var review knowledge.MigrationTruthAtomicizationReview
	runMigrationCLIJSON(t, &review, "--project", root, "--truth-review", reviewPath)
	if review.Digest == "" || len(review.Claims) != 2 {
		t.Fatalf("CLI truth review submission: %+v", review)
	}
	var preview knowledge.MigrationPreview
	runMigrationCLIJSON(t, &preview, "--project", root, "--preview", "--live-campaigns", "live")
	if len(preview.Plan.TruthConversions) != 2 {
		t.Fatalf("CLI preview did not consume reviewed split: %+v", preview.Plan)
	}
}

func TestMigrationCLIExportsAndSubmitsNonActivatingProfileDecision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	writeMigrationCLIFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Fixture\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy\n<!-- re-discipline:shared-laws:end -->\n")
	legacy := `{"schemaVersion":0,"profileId":"project:legacy-rerank","effectiveProfiles":[{"lanes":["exact","fts","graph","dense","rerank"],"requires":{"embedding":"legacy-model"}}]}` + "\n"
	writeMigrationCLIFile(t, filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"), legacy)

	var packet knowledge.MigrationProfileConflictPacket
	runMigrationCLIJSON(t, &packet, "--project", root, "--profile-conflict")
	if packet.Digest == "" || packet.Conflict.LegacyProfile != legacy || packet.Baseline.MeasurementEvidenceDigest == "" {
		t.Fatalf("CLI profile conflict packet: %+v", packet)
	}
	submission := knowledge.MigrationProfileDecisionSubmission{
		SchemaVersion: knowledge.MigrationSchemaVersion, PacketDigest: packet.Digest,
		SourceFingerprint: packet.SourceFingerprint, SourcePath: packet.Conflict.SourcePath, SourceDigest: packet.Conflict.SourceDigest,
		BaselineProfileID: packet.Baseline.Profile.ProfileID, BaselineProfileDigest: packet.Baseline.ProfileDigest,
		EffectiveProfileName: packet.Baseline.EffectiveProfileName, EffectiveProfileDigest: packet.Baseline.EffectiveProfileDigest,
		MeasurementEvidenceDigest: packet.Baseline.MeasurementEvidenceDigest,
		Decision:                  "retain-packaged-baseline", ExplicitManagerApproval: true, ProjectProfileActivation: false,
		Authority: "manager", Reviewer: "manager", Rationale: "CLI explicitly retains only the packaged measured baseline for conversion.",
		DecidedAt: "2026-08-03T12:00:00Z", ReplacesDecisionDigest: "",
	}
	body, err := json.MarshalIndent(submission, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	decisionPath := filepath.Join(t.TempDir(), "profile-decision.json")
	writeMigrationCLIFile(t, decisionPath, string(append(body, '\n')))
	var decision knowledge.MigrationProfileConversionDecision
	runMigrationCLIJSON(t, &decision, "--project", root, "--profile-decision", decisionPath)
	if decision.Digest == "" || decision.ProjectProfileActivation {
		t.Fatalf("CLI profile decision submission: %+v", decision)
	}
	var preview knowledge.MigrationPreview
	runMigrationCLIJSON(t, &preview, "--project", root, "--preview")
	if preview.Plan.ProfileDecision == nil || preview.Plan.ProfileDecision.Digest != decision.Digest {
		t.Fatalf("CLI preview did not consume the sealed profile decision: %+v", preview.Plan)
	}
}

func migrationCLIEvidenceRef(t *testing.T, root, relative string, value any) knowledge.MigrationEvidenceArtifactReference {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	writeMigrationCLIFile(t, filepath.Join(root, filepath.FromSlash(relative)), string(body))
	return knowledge.MigrationEvidenceArtifactReference{Path: relative, Digest: "sha256:" + knowledge.SHA256Bytes(body)}
}

func strictMigrationCLIRetrievalEvidence(
	t *testing.T,
	root string,
	state knowledge.MigrationState,
) knowledge.MigrationRetrievalGateEvidence {
	t.Helper()
	development := knowledge.CaseOutcome{
		CaseID: "cli-migration-development", Split: "development", Topic: "cli-migration-development",
		Paths: []string{"docs/truth/cli-development.md"}, Tiers: []string{"truth"},
		RelevantPaths: []string{"docs/truth/cli-development.md"}, RelevantRanks: []int{1},
		ExpectedCitationsFound: []string{"docs/truth/cli-development.md"}, EstimatedTokens: 320,
		ReturnedUniquePaths: 1, ExpectedFound: true, CompleteEvidence: true,
		AuthoritySafe: true, CitationMetadataSafe: true, CitationSafe: true,
		CorpusMatched: true, AbstentionCorrect: true, BudgetSafe: true, ReplayIdentical: true,
		MinimumTokenBudget: 1024, QualityGateApplicable: true, SafetyPassed: true,
		QualityPassed: true, GatePassed: true, ReturnedTokens: 320, RelevantTokens: 300,
	}
	holdout := development
	holdout.CaseID, holdout.Split, holdout.Topic = "cli-migration-holdout", "holdout", "cli-migration-holdout"
	holdout.Paths = []string{"docs/truth/cli-holdout.md"}
	holdout.RelevantPaths = []string{"docs/truth/cli-holdout.md"}
	holdout.ExpectedCitationsFound = []string{"docs/truth/cli-holdout.md"}
	cases := []knowledge.CaseOutcome{development, holdout}
	contextCases := []knowledge.ContextPackOutcome{
		{CaseID: development.CaseID, Split: development.Split, Topic: development.Topic, Role: "manager", Passed: true},
		{CaseID: holdout.CaseID, Split: holdout.Split, Topic: holdout.Topic, Role: "drafter", Passed: true},
	}
	casesByBudget := map[string][]knowledge.CaseOutcome{}
	contextsByBudget := map[string][]knowledge.ContextPackOutcome{}
	metricsByBudget := map[string]knowledge.QualityMetrics{}
	for _, budget := range []string{"512", "1024", "2048", "4096"} {
		casesByBudget[budget] = append([]knowledge.CaseOutcome(nil), cases...)
		contextsByBudget[budget] = append([]knowledge.ContextPackOutcome(nil), contextCases...)
		metricsByBudget[budget] = knowledge.QualityMetrics{BudgetComplianceRate: 1, DeterministicReplayRate: 1}
	}
	baselineBody, err := os.ReadFile(filepath.Join("..", "..", "profiles", "balanced-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var candidate knowledge.RetrievalProfile
	if err := json.Unmarshal(baselineBody, &candidate); err != nil {
		t.Fatal(err)
	}
	lanes := map[string][]string{}
	for _, row := range candidate.EffectiveProfiles {
		lanes[row.Name] = append([]string(nil), row.Lanes...)
	}
	effectiveDigest, _ := knowledge.CanonicalDigest("cli-effective-profile")
	primary := knowledge.ProjectProfileBenchmark{
		ProfileName: "hybrid-no-rerank-v1", EffectiveProfile: "hybrid-no-rerank-v1@" + effectiveDigest,
		ActiveLanes:     lanes["hybrid-no-rerank-v1"],
		HardGatesPassed: true, NonInferiorToLexical: true, Cases: cases,
		CasesByBudget: casesByBudget, ContextPackCases: contextCases,
		ContextPacksByBudget: contextsByBudget, ContextPackRoles: []string{"manager", "drafter"},
		ContextPackPassed: true, MetricsBySplit: map[string]knowledge.QualityMetrics{
			"development": {BudgetComplianceRate: 1, DeterministicReplayRate: 1},
			"holdout":     {BudgetComplianceRate: 1, DeterministicReplayRate: 1},
		}, MetricsByBudget: metricsByBudget, QualityMetricsByBudget: metricsByBudget,
	}
	evalDigest, _ := knowledge.CanonicalDigest(cases)
	corpusDigest, _ := knowledge.CanonicalDigest("cli migration corpus")
	modelDigest, _ := knowledge.CanonicalDigest("cli migration models")
	requestedDigest, _ := knowledge.CanonicalDigest(candidate.ProfileID)
	lexical := primary
	lexical.ProfileName = "lexical-graph-v1"
	lexical.EffectiveProfile = lexical.ProfileName + "@" + modelDigest
	lexical.ActiveLanes = lanes[lexical.ProfileName]
	benchmark := knowledge.ProjectBenchmarkReport{
		SchemaVersion: 1, RunID: "benchmark-cli-migration", Mode: "full", Suite: "project-benchmark-v1",
		RequestedProfile: candidate.ProfileID + "@" + requestedDigest,
		Generation: knowledge.GenerationSummary{ID: "generation-cli-migration",
			CorpusFingerprint: corpusDigest, ModelFingerprint: modelDigest},
		EvalFingerprint: evalDigest, Profiles: []knowledge.ProjectProfileBenchmark{primary, lexical},
		UnsupportedProfiles: []knowledge.UnsupportedProjectProfile{}, Passed: true, Complete: true,
		ReportPath: filepath.ToSlash(filepath.Join(root, "migration-tests", "evidence", "cli-benchmark.json")),
	}
	for index := range benchmark.Profiles {
		benchmark.Profiles[index].ObservationDigest, err =
			knowledge.MigrationProjectBenchmarkObservationDigest(benchmark, benchmark.Profiles[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	primary = benchmark.Profiles[0]
	benchmarkRef := migrationCLIEvidenceRef(t, root, "migration-tests/evidence/cli-benchmark.json", benchmark)

	candidate.BaseProfile = candidate.ProfileID
	candidate.ProfileID = "project:candidate-cli00000000"
	candidate.Approval = nil
	if err := knowledge.ValidateProfile(candidate); err != nil {
		t.Fatal(err)
	}
	candidateRef := migrationCLIEvidenceRef(t, root, "migration-tests/evidence/cli-candidate.json", candidate)
	recommendedDigest, _ := knowledge.CanonicalDigest(map[string]any{"suite": "project-calibration-v1", "identity": primary.EffectiveProfile})
	recommended := knowledge.CalibrationCandidate{
		Identity: primary.EffectiveProfile, Weights: map[string]int{"exact": 8, "fts": 6, "graph": 2, "dense": 4},
		HardGatesPassed: true, NonInferiorToBaseline: true, Pareto: true, BenchmarkDigest: recommendedDigest,
	}
	calibration := knowledge.CalibrationReport{
		SchemaVersion: 1, RunID: "calibration-cli-migration", BaseProfile: "plugin:balanced-v1",
		ActiveBefore: primary.EffectiveProfile, ActiveAfter: primary.EffectiveProfile,
		EvalDigest: benchmark.EvalFingerprint, CorpusFingerprint: benchmark.Generation.CorpusFingerprint,
		ModelFingerprint: benchmark.Generation.ModelFingerprint,
		RuntimeContract:  knowledge.RuntimeContract(benchmark.Generation.Runtime),
		Candidates:       []knowledge.CalibrationCandidate{recommended}, ParetoFrontier: []knowledge.CalibrationCandidate{recommended},
		Recommended: recommended, CandidatePath: candidateRef.Path, CandidateDigest: candidateRef.Digest,
	}
	calibrationRef := migrationCLIEvidenceRef(t, root, "migration-tests/evidence/cli-calibration.json", calibration)
	holdoutDigest, _ := knowledge.CanonicalDigest(holdout)
	trial := func(condition string, tokens int) knowledge.MigrationBlindedAgentCaseOutcome {
		outcome := knowledge.MigrationBlindedAgentCaseOutcome{
			CaseID: holdout.CaseID, Condition: condition, Agent: "claude-code-cli-harness",
			Model: "fixed-blinded-agent-v1", BenchmarkOutcomeDigest: holdoutDigest,
			Request: knowledge.MigrationBlindedAgentRequest{CaseID: holdout.CaseID,
				Query: "Which CLI holdout fact is supported?", QueryClass: "provenance",
				Role: "drafter", AllowedTiers: []string{"truth"}, TokenBudget: 1024},
			Response: knowledge.MigrationBlindedAgentResponse{
				AnswerClaims: []string{"The CLI holdout fact is supported."}, Decision: "supported",
				OpenedSourceIDs:    []string{"docs/truth/cli-holdout.md"},
				ContextCardHandles: []string{"re-discipline://fixture/context/cli-holdout"},
				ExpansionHandles:   []string{"path:docs/truth/cli-holdout.md"},
				Citations:          []string{"docs/truth/cli-holdout.md"}, DurabilityLabels: []string{"truth"},
				FullCorpusRead: condition == "legacy", ContextTokens: tokens,
			}, FactualAccuracy: 1, DecisionAccuracy: 1, EvidenceTracePassed: true,
		}
		if err := knowledge.SealMigrationBlindedAgentCaseOutcome(&outcome); err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	blinded := knowledge.MigrationBlindedAgentEvaluation{
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest,
		BenchmarkDigest: benchmarkRef.Digest, CalibrationDigest: calibrationRef.Digest,
		Evaluator: "independent-cli-manager", Cases: []knowledge.MigrationBlindedAgentCaseOutcome{
			trial("legacy", 900), trial("compiled", 500),
		},
	}
	if err := knowledge.SealMigrationBlindedAgentEvaluation(&blinded); err != nil {
		t.Fatal(err)
	}
	blindedRef := migrationCLIEvidenceRef(t, root, "migration-tests/evidence/cli-blinded.json", blinded)
	evidence := knowledge.MigrationRetrievalGateEvidence{
		Benchmark: benchmarkRef, Calibration: calibrationRef,
		CandidateProfile: candidateRef, BlindedEvaluation: blindedRef,
	}
	if err := knowledge.SealMigrationRetrievalGateEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func strictMigrationCLIHostEvidence(
	t *testing.T,
	state knowledge.MigrationState,
) knowledge.MigrationHostConformanceEvidence {
	t.Helper()
	semantic := map[string]json.RawMessage{
		"discovery":        knowledge.MigrationHostDiscoverySchema(),
		"status":           mustCLIRaw(t, map[string]any{"transactionId": state.TransactionID, "state": state.State}),
		"retrieval":        mustCLIRaw(t, map[string]any{"status": "ok", "ids": []string{"F-0001"}}),
		"expansion":        mustCLIRaw(t, map[string]any{"status": "ok", "handle": "finding:F-0001"}),
		"role-boundary":    mustCLIRaw(t, map[string]any{"code": "role-boundary-refused"}),
		"bounded-recovery": mustCLIRaw(t, map[string]any{"status": "ok", "transactionId": state.TransactionID}),
	}
	required := map[string][]string{
		"mcp":    {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery"},
		"cli":    {"status", "retrieval", "expansion", "role-boundary", "bounded-recovery"},
		"claude": {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery", "local-fallback"},
		"codex":  {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery", "local-fallback"},
	}
	trials := []knowledge.MigrationHostTrial{}
	for _, host := range []string{"mcp", "cli", "claude", "codex"} {
		for _, scenario := range required[host] {
			semanticResult := semantic[scenario]
			if scenario == "local-fallback" {
				semanticResult = semantic["status"]
			}
			transport := map[string]string{
				"mcp": "mcp-stdio-jsonrpc", "cli": "cli-process-json",
				"claude": "claude-code-plugin", "codex": "codex-plugin",
			}[host]
			if scenario == "local-fallback" {
				transport = host + "-cli-fallback"
			}
			trial := knowledge.MigrationHostTrial{
				Host: host, Scenario: scenario, Transport: transport,
				Request: mustCLIRaw(t, map[string]any{"host": host, "scenario": scenario}),
				Result:  semanticResult, SemanticResult: semanticResult,
			}
			if scenario == "role-boundary" {
				trial.Failure = &knowledge.MigrationHostFailure{Code: "role-boundary-refused", Message: "manager boundary enforced"}
			} else if scenario == "local-fallback" {
				trial.Failure = &knowledge.MigrationHostFailure{Code: "mcp-unavailable", Message: "CLI fallback executed"}
			}
			if err := knowledge.SealMigrationHostTrial(&trial); err != nil {
				t.Fatal(err)
			}
			trials = append(trials, trial)
		}
	}
	evidence := knowledge.MigrationHostConformanceEvidence{
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest, Trials: trials,
	}
	if err := knowledge.SealMigrationHostConformanceEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func strictMigrationCLIGateArtifact(
	t *testing.T,
	root string,
	state knowledge.MigrationState,
	gate string,
) knowledge.MigrationGateArtifact {
	t.Helper()
	var retrieval *knowledge.MigrationRetrievalGateEvidence
	var host *knowledge.MigrationHostConformanceEvidence
	if gate == "retrieval-context" {
		value := strictMigrationCLIRetrievalEvidence(t, root, state)
		retrieval = &value
	} else {
		value := strictMigrationCLIHostEvidence(t, state)
		host = &value
	}
	artifact, err := knowledge.BuildMigrationGateArtifact(
		state.TransactionID, state.PlanDigest, gate, retrieval, host)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func mustCLIRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func runMigrationCLIJSON(t *testing.T, target any, args ...string) {
	t.Helper()
	args = withMigrationCLIAssetRoot(t, args)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	type readResult struct {
		body []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	go func() {
		body, err := io.ReadAll(reader)
		readDone <- readResult{body: body, err: err}
	}()
	os.Stdout = writer
	runErr := runMigrationCommand(context.Background(), args)
	closeErr := writer.Close()
	os.Stdout = original
	result := <-readDone
	_ = reader.Close()
	if runErr != nil {
		t.Fatalf("migrate-project %v: %v\n%s", args, runErr, result.body)
	}
	if closeErr != nil || result.err != nil {
		t.Fatalf("capture migrate-project output: close=%v read=%v", closeErr, result.err)
	}
	if err := json.Unmarshal(result.body, target); err != nil {
		t.Fatalf("decode migrate-project output for %v: %v\n%s", args, err, result.body)
	}
}

func runMigrationCLIError(args ...string) error {
	assetRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return err
	}
	return runMigrationCommand(context.Background(), append(args, "--asset-root", assetRoot))
}

func withMigrationCLIAssetRoot(t *testing.T, args []string) []string {
	t.Helper()
	for index, arg := range args {
		if arg == "--asset-root" || strings.HasPrefix(arg, "--asset-root=") {
			if arg == "--asset-root" && index+1 >= len(args) {
				t.Fatal("--asset-root requires a value")
			}
			return args
		}
	}
	assetRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return append(args, "--asset-root", assetRoot)
}

func writeMigrationCLIFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// commitMigrationCLIFixture gives a fixture project the git archive the 0.8
// conversion requires: preview blocks until every managed source is tracked
// and clean, because `git show <sourceRevision>:<path>` is the recorded
// provenance and recovery recipe.
func commitMigrationCLIFixture(t *testing.T, root string) {
	t.Helper()
	git := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@invalid",
		)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		git("init", "-q")
		git("config", "core.autocrlf", "false")
		writeMigrationCLIFile(t, filepath.Join(root, ".gitignore"),
			".re-discipline/migration/\n.re-discipline/state/\nmigration-tests/\n")
	}
	git("add", "-A")
	git("commit", "-q", "--allow-empty", "-m", "fixture state")
}
