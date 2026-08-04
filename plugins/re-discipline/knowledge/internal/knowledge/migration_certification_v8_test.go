package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func writeMigrationEvidenceJSON(t *testing.T, root, relative string, value any) MigrationEvidenceArtifactReference {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), string(body))
	return MigrationEvidenceArtifactReference{Path: relative, Digest: "sha256:" + SHA256Bytes(body)}
}

func writeMigrationFixtureJSONIfMissing(t *testing.T, root, relative string, value any) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if _, err := os.Stat(absolute); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	_ = writeMigrationEvidenceJSON(t, root, relative, value)
}

func writeMigrationFixtureFileIfMissing(t *testing.T, root, relative, body string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if _, err := os.Stat(absolute); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	mustWriteFile(t, absolute, body)
}

func migrationCertificationCandidate(t *testing.T) RetrievalProfile {
	t.Helper()
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		t.Fatal(err)
	}
	candidate := cloneRetrievalProfile(baseline.Profile)
	candidate.ProfileID = "project:candidate-fixture000000"
	candidate.BaseProfile = baseline.Profile.ProfileID
	candidate.Approval = nil
	if err := ValidateProfile(candidate); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func migrationCertificationEvalCases(t *testing.T, root string) []EvalCase {
	t.Helper()
	writeMigrationFixtureJSONIfMissing(t, root, ".re-discipline/config.json", BootstrapConfig{
		SchemaVersion: 3, CampaignSchemaVersion: 2,
		State: StateConfig{ActiveRoot: "active", ArchiveRoot: "docs/history/campaigns",
			LockFile: ".re-discipline/state/write.lock", Recovery: "replay-and-verify",
			GeneratedViewMaxItems: 24},
		Authority: AuthorityConfig{ManagerRoles: []string{"manager", "user"},
			CuratorWrites: []string{"curator-run", "intake"}, TruthProjection: "closure-only"},
		Context: ContextConfig{ManagerCardTokens: 6144, DrafterCardTokens: 3072,
			MaxCards: 16, MaxExpansionBytes: 32768, LeaseMode: "memory-only"},
		Payload:    PayloadConfig{CreateLazily: true, MaxInlineBytes: 1048576, RequireRegistration: true},
		ReviewLoad: ReviewLoadConfig{TargetMinutesPerPacket: 12, TargetPacketsPerSession: 6},
		Closure: ClosureConfig{RequireRunCoverage: true, RequireFindingDisposition: true,
			RequireFileRetention: true, RequireArchiveVerification: true},
		Memory: MemoryConfig{Mode: "shared-only", WritePolicy: "proposal-only"},
		Knowledge: KnowledgeConfig{Enabled: true, Profile: "plugin:balanced-v1",
			SettingsFile: "knowledge/policy.jsonc", ProjectProfile: "knowledge/retrieval-profile.json"},
		Migration: MigrationConfig{Mode: "explicit-only", LegacyReaders: "migrator-only"},
	})
	writeMigrationFixtureJSONIfMissing(t, root, ".re-discipline/knowledge/policy.jsonc", KnowledgeSettings{
		Schema: "plugin://re-discipline/schemas/knowledge-settings.schema.json", SchemaVersion: 2,
		Sources: SourceSettings{Truth: true, HistoryFindings: true, Backlog: true,
			ActiveFindings: true, SharedMemory: true, ReportFallback: true, Additional: []AdditionalSource{}},
		Models: ModelSettings{Execution: "local"}, Telemetry: Telemetry{Mode: "metrics-only"},
		Budgets: BudgetSettings{SearchTokens: 3072, ManagerContextTokens: 6144,
			DrafterContextTokens: 3072, MaxPassages: 16, MaxBytes: 32768},
		Archive: ArchiveSettings{ReportFallbackUntilMeasured: true,
			NormalizationTriggerHits: 3, FallbackMode: "default-fallback"},
	})
	writeMigrationFixtureFileIfMissing(t, root, ".re-discipline/project-profile.md",
		"# Migration certification fixture\n\n<!-- re-discipline:shared-laws v0.8.0 -->\nfixture\n<!-- re-discipline:shared-laws:end -->\n")
	writeMigrationFixtureFileIfMissing(t, root, "docs/INDEX.md", "# Project map\n")
	writeMigrationFixtureFileIfMissing(t, root, "docs/backlog/development.md",
		"# Development fixture work\n\nThe development fixture retains a bounded work item.\n")
	writeMigrationFixtureFileIfMissing(t, root, "docs/backlog/next.md",
		"# Deferred fixture work\n\nThe fixture retains its bounded next step.\n")
	answerable := true
	cases := []EvalCase{
		{ID: "migration-cert-development", Role: "manager", Topic: "migration-cert-development",
			Split: "development", Query: "Which bounded development work item does the fixture retain?", QueryClass: "conceptual",
			AllowedTiers: []string{"backlog"}, CorpusSnapshot: "fixture:packaged-conformance-v1",
			ExpectedPaths:        []string{"docs/backlog/development.md"},
			MinimumEvidencePaths: []string{"docs/backlog/development.md"},
			HardNegativePaths:    []string{}, ExpectedCitations: []string{"docs/backlog/development.md"},
			ForbiddenTiers: []string{"draft"}, TokenBudget: 1024, Answerable: &answerable},
		{ID: "migration-cert-holdout", Role: "drafter", Topic: "migration-cert-holdout",
			Split: "holdout", Query: "Which bounded next step does the fixture retain?", QueryClass: "provenance",
			AllowedTiers: []string{"backlog"}, CorpusSnapshot: "fixture:packaged-conformance-v1",
			ExpectedPaths:        []string{"docs/backlog/next.md"},
			MinimumEvidencePaths: []string{"docs/backlog/next.md"},
			HardNegativePaths:    []string{}, ExpectedCitations: []string{"docs/backlog/next.md"},
			ForbiddenTiers: []string{"draft"}, TokenBudget: 1024, Answerable: &answerable},
	}
	writeMigrationFixtureJSONIfMissing(t, root, ".re-discipline/knowledge/evals/cases.json", cases)
	return cases
}

func migrationCertificationCaseOutcome(
	t *testing.T,
	eval EvalCase,
	budget int,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) CaseOutcome {
	t.Helper()
	outcome, err := replayMigrationCaseOutcome(environment, selected, eval, budget)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func migrationCertificationContextOutcome(
	t *testing.T,
	eval EvalCase,
	budget int,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) ContextPackOutcome {
	t.Helper()
	outcome, err := replayMigrationContextOutcome(environment, selected, eval, budget)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func migrationCertificationBenchmark(t *testing.T, root string) (ProjectBenchmarkReport, ProjectProfileBenchmark) {
	t.Helper()
	evalCases := migrationCertificationEvalCases(t, root)
	environment, err := InspectMigrationCertificationEnvironment(root)
	if err != nil {
		t.Fatal(err)
	}
	environment.certificationAssetRoot = adversarialAssetRoot(t)
	cleanup, err := environment.prepareCertificationReplay()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		t.Fatal(err)
	}
	casesByBudget := map[string][]CaseOutcome{}
	contextsByBudget := map[string][]ContextPackOutcome{}
	requestedDigest, _ := CanonicalDigest(baseline.Profile.ProfileID)
	report := ProjectBenchmarkReport{
		SchemaVersion: 1, RunID: "benchmark-migration-certification", Mode: "full",
		Suite: "project-benchmark-v1", RequestedProfile: baseline.Profile.ProfileID + "@" + requestedDigest,
		Generation:      PublicGeneration(*environment.replayGeneration),
		EvalFingerprint: environment.EvalFingerprint, FindingSuiteDigests: environment.FindingSuiteDigests,
		UnsupportedProfiles: []UnsupportedProjectProfile{}, Passed: true, Complete: true,
		HardNegativeCoverage: MeasureHardNegativeCoverage(evalCases),
		ReportPath:           filepath.ToSlash(filepath.Join(root, "migration-tests", "evidence", "benchmark.json")),
	}
	for _, row := range baseline.Profile.EffectiveProfiles {
		selected, err := selectMigrationReplayProfile(environment, row)
		if err != nil {
			t.Fatal(err)
		}
		cases := make([]CaseOutcome, 0, len(evalCases))
		contexts := make([]ContextPackOutcome, 0, len(evalCases))
		for _, eval := range evalCases {
			cases = append(cases, migrationCertificationCaseOutcome(
				t, eval, eval.TokenBudget, environment, selected))
			contexts = append(contexts, migrationCertificationContextOutcome(
				t, eval, eval.TokenBudget, environment, selected))
		}
		metricsByBudget := map[string]QualityMetrics{}
		qualityByBudget := map[string]QualityMetrics{}
		for _, budget := range []int{512, 1024, 2048, 4096} {
			key := fmt.Sprintf("%d", budget)
			budgetCases := make([]CaseOutcome, 0, len(evalCases))
			budgetContexts := make([]ContextPackOutcome, 0, len(evalCases))
			for _, eval := range evalCases {
				budgetCases = append(budgetCases, migrationCertificationCaseOutcome(
					t, eval, budget, environment, selected))
				budgetContexts = append(budgetContexts, migrationCertificationContextOutcome(
					t, eval, budget, environment, selected))
			}
			casesByBudget[key] = budgetCases
			contextsByBudget[key] = budgetContexts
			metricsByBudget[key] = calculateMetrics(budgetCases, evalCases)
			qualityByBudget[key] = applicableMetrics(budgetCases, evalCases)
		}
		developmentOutcomes, developmentCases := filterEvalSplit(cases, evalCases, "development")
		holdoutOutcomes, holdoutCases := filterEvalSplit(cases, evalCases, "holdout")
		profile := ProjectProfileBenchmark{
			ProfileName: row.Name, EffectiveProfile: selected.EffectiveIdentity,
			ActiveLanes: append([]string(nil), row.Lanes...), Models: environment.ModelsByProfile[row.Name],
			HardGatesPassed: true, NonInferiorToLexical: true, Cases: cases,
			CasesByBudget: cloneCaseOutcomeMap(casesByBudget), ContextPackCases: contexts,
			ContextPacksByBudget: cloneContextOutcomeMap(contextsByBudget),
			ContextPackRoles:     []string{"drafter", "manager"}, ContextPackPassed: true,
			Metrics: calculateMetrics(cases, evalCases), MetricsBySplit: map[string]QualityMetrics{
				"development": calculateMetrics(developmentOutcomes, developmentCases),
				"holdout":     calculateMetrics(holdoutOutcomes, holdoutCases)},
			MetricsByBudget:        cloneQualityMetricMap(metricsByBudget),
			QualityMetricsByBudget: cloneQualityMetricMap(qualityByBudget),
			FindingEvaluations:     []FindingAblationReport{},
		}
		report.Profiles = append(report.Profiles, profile)
	}
	for index := range report.Profiles {
		report.Profiles[index].ObservationDigest, err =
			MigrationProjectBenchmarkObservationDigest(report, report.Profiles[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	primary := report.Profiles[0]
	return report, primary
}

func cloneCaseOutcomeMap(input map[string][]CaseOutcome) map[string][]CaseOutcome {
	output := map[string][]CaseOutcome{}
	for key, rows := range input {
		output[key] = append([]CaseOutcome(nil), rows...)
	}
	return output
}

func cloneContextOutcomeMap(input map[string][]ContextPackOutcome) map[string][]ContextPackOutcome {
	output := map[string][]ContextPackOutcome{}
	for key, rows := range input {
		output[key] = append([]ContextPackOutcome(nil), rows...)
	}
	return output
}

func cloneQualityMetricMap(input map[string]QualityMetrics) map[string]QualityMetrics {
	output := map[string]QualityMetrics{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func migrationStrictRetrievalEvidence(
	t *testing.T,
	root string,
	state MigrationState,
) MigrationRetrievalGateEvidence {
	t.Helper()
	report, primary := migrationCertificationBenchmark(t, root)
	benchmarkRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/benchmark.json", report)
	environment, err := InspectMigrationCertificationEnvironment(root)
	if err != nil {
		t.Fatal(err)
	}
	environment.certificationAssetRoot = adversarialAssetRoot(t)
	cleanup, err := environment.prepareCertificationReplay()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		t.Fatal(err)
	}
	var selectedRow EffectiveProfile
	for _, row := range baseline.Profile.EffectiveProfiles {
		if row.Name == migrationBaselineEffectiveProfile {
			selectedRow = row
			break
		}
	}
	if selectedRow.Name == "" {
		t.Fatal("embedded migration baseline lacks its primary row")
	}
	baselineSelected, err := selectMigrationReplayProfile(environment, selectedRow)
	if err != nil {
		t.Fatal(err)
	}
	developmentCases, holdoutCases := splitEvalCases(environment.EvalCases)
	baselineHoldoutCases := make([]CaseOutcome, 0, len(holdoutCases))
	for _, eval := range holdoutCases {
		baselineHoldoutCases = append(baselineHoldoutCases,
			migrationCertificationCaseOutcome(
				t, eval, eval.TokenBudget, environment, baselineSelected))
	}
	baselineHoldoutMetrics := calculateMetrics(baselineHoldoutCases, holdoutCases)
	baselineHoldoutMetrics = ratchetBaseline(selectedRow.Benchmark, baselineHoldoutMetrics)

	candidates := make([]CalibrationCandidate, 0, 27)
	for _, exact := range []int{6, 8, 10} {
		for _, fts := range []int{4, 6, 8} {
			for _, graph := range []int{1, 2, 3} {
				row := cloneEffectiveProfile(selectedRow)
				row.Weights = map[string]int{
					"exact": exact, "fts": fts, "graph": graph,
					"dense": selectedRow.Weights["dense"],
				}
				selected, err := selectMigrationReplayProfile(environment, row)
				if err != nil {
					t.Fatal(err)
				}
				developmentOutcomes := make([]CaseOutcome, 0, len(developmentCases))
				for _, eval := range developmentCases {
					developmentOutcomes = append(
						developmentOutcomes,
						migrationCertificationCaseOutcome(
							t, eval, eval.TokenBudget, environment, selected),
					)
				}
				metrics := calculateMetrics(developmentOutcomes, developmentCases)
				candidates = append(candidates, CalibrationCandidate{
					Identity: selected.EffectiveIdentity, Weights: cloneWeights(row.Weights),
					DevelopmentCases:   developmentOutcomes,
					DevelopmentHit:     relevantPathHits(developmentOutcomes),
					DevelopmentMetrics: metrics,
					Violations: metrics.AuthorityViolations + metrics.CitationViolations +
						metrics.HardNegativeHits,
					HardGatesPassed: hardMetricsPassed(metrics),
				})
			}
		}
	}
	frontierIndexes := paretoFrontierIndexes(candidates)
	for _, index := range frontierIndexes {
		row := cloneEffectiveProfile(selectedRow)
		row.Weights = cloneWeights(candidates[index].Weights)
		selected, selectErr := selectMigrationReplayProfile(environment, row)
		if selectErr != nil {
			t.Fatal(selectErr)
		}
		holdoutOutcomes := make([]CaseOutcome, 0, len(holdoutCases))
		for _, eval := range holdoutCases {
			holdoutOutcomes = append(holdoutOutcomes,
				migrationCertificationCaseOutcome(
					t, eval, eval.TokenBudget, environment, selected))
		}
		holdoutMetrics := calculateMetrics(holdoutOutcomes, holdoutCases)
		candidate := &candidates[index]
		candidate.HoldoutCases = holdoutOutcomes
		candidate.HoldoutHit = relevantPathHits(holdoutOutcomes)
		candidate.HoldoutMetrics = holdoutMetrics
		candidate.Pareto = true
		candidate.HardGatesPassed = candidate.HardGatesPassed && hardMetricsPassed(holdoutMetrics)
		candidate.NonInferiorToBaseline = calibrationNonInferior(
			holdoutMetrics, baselineHoldoutMetrics)
		candidate.Violations += holdoutMetrics.AuthorityViolations +
			holdoutMetrics.CitationViolations + holdoutMetrics.HardNegativeHits
		candidate.BenchmarkDigest, err = calibrationBenchmarkDigest(
			candidate.Identity, report.EvalFingerprint, environment.CorpusFingerprint,
			candidate.DevelopmentMetrics, candidate.HoldoutMetrics, baselineHoldoutMetrics,
			nil, nil, nil, environment.CalibrationFindingSuiteDigests,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	frontier := make([]CalibrationCandidate, 0, len(frontierIndexes))
	for _, index := range frontierIndexes {
		if candidates[index].HardGatesPassed && candidates[index].NonInferiorToBaseline {
			frontier = append(frontier, candidates[index])
		}
	}
	sort.Slice(frontier, func(i, j int) bool {
		return calibrationCandidateLess(frontier[i], frontier[j])
	})
	if len(frontier) == 0 {
		t.Fatal("migration certification fixture produced no calibration finalist")
	}
	recommended := frontier[0]
	candidate := cloneRetrievalProfile(baseline.Profile)
	candidate.BaseProfile = baseline.Profile.ProfileID
	candidate.Approval = nil
	identityParts := strings.SplitN(recommended.Identity, "@", 2)
	if len(identityParts) != 2 || !strings.HasPrefix(identityParts[1], "sha256:") {
		t.Fatalf("invalid fixture recommendation identity %q", recommended.Identity)
	}
	candidate.ProfileID = "project:candidate-" +
		strings.TrimPrefix(identityParts[1], "sha256:")[:16]
	runtimeDigest, err := CanonicalDigest(environment.RuntimeContract)
	if err != nil {
		t.Fatal(err)
	}
	for index := range candidate.EffectiveProfiles {
		row := &candidate.EffectiveProfiles[index]
		if row.Name != selectedRow.Name {
			continue
		}
		row.Weights = cloneWeights(recommended.Weights)
		row.Benchmark = BenchmarkEvidence{
			Suite: "project-calibration-v1", Digest: recommended.BenchmarkDigest,
			Status: "passed", EvaluatedAt: RFC3339UTC(time.Date(
				2026, time.August, 3, 0, 0, 0, 0, time.UTC)),
			EvalFingerprint:    report.EvalFingerprint,
			CorpusFingerprint:  environment.CorpusFingerprint,
			ModelFingerprint:   environment.CalibrationModelFingerprint,
			RuntimeFingerprint: runtimeDigest, ParserVersion: environment.ParserVersion,
			ChunkerVersion:             environment.ChunkerVersion,
			RatifiedHardNegativeHits:   ratifiedHits(recommended.HoldoutMetrics),
			RatifiedAbstentionAccuracy: recommended.HoldoutMetrics.AbstentionAccuracy,
		}
	}
	if err := ValidateProfile(candidate); err != nil {
		t.Fatal(err)
	}
	candidateRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/candidate-profile.json", candidate)
	calibration := CalibrationReport{
		SchemaVersion: 1, RunID: "calibration-migration-certification",
		BaseProfile: baseline.Profile.ProfileID, ActiveBefore: primary.EffectiveProfile,
		ActiveAfter: primary.EffectiveProfile, EvalDigest: report.EvalFingerprint,
		FindingSuiteDigests:    environment.CalibrationFindingSuiteDigests,
		CorpusFingerprint:      environment.CorpusFingerprint,
		ModelFingerprint:       environment.CalibrationModelFingerprint,
		RuntimeContract:        environment.RuntimeContract,
		BaselineHoldoutCases:   baselineHoldoutCases,
		BaselineHoldoutMetrics: baselineHoldoutMetrics,
		Candidates:             candidates, ParetoFrontier: frontier,
		Recommended: recommended, CandidatePath: candidateRef.Path,
		CandidateDigest: candidateRef.Digest, Activated: false,
	}
	calibrationRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/calibration.json", calibration)
	holdout := baselineHoldoutCases[0]
	holdoutEval := holdoutCases[0]
	benchmarkOutcomeDigest, _ := CanonicalDigest(holdout)
	trial := func(condition string, tokens int) MigrationBlindedAgentCaseOutcome {
		outcome := MigrationBlindedAgentCaseOutcome{
			CaseID: holdout.CaseID, Condition: condition, Agent: "claude-code-drafter",
			Answerable: holdoutEval.Answerable == nil || *holdoutEval.Answerable,
			Model:      "fixed-blinded-agent-v1", BenchmarkOutcomeDigest: benchmarkOutcomeDigest,
			Request: MigrationBlindedAgentRequest{CaseID: holdout.CaseID,
				Query: holdoutEval.Query, QueryClass: holdoutEval.QueryClass,
				Role:         holdoutEval.Role,
				AllowedTiers: append([]string(nil), holdoutEval.AllowedTiers...),
				TokenBudget:  holdoutEval.TokenBudget},
			Response: MigrationBlindedAgentResponse{
				AnswerClaims: []string{"The bounded holdout fact is supported."}, Decision: "supported",
				OpenedSourceIDs:    []string{holdoutEval.ExpectedPaths[0]},
				ContextCardHandles: []string{"re-discipline://fixture/context/holdout"},
				ExpansionHandles:   []string{"path:" + holdoutEval.ExpectedPaths[0]},
				Citations:          []string{holdoutEval.ExpectedPaths[0]}, DurabilityLabels: []string{holdoutEval.AllowedTiers[0]},
				FullCorpusRead: condition == "legacy", ContextTokens: tokens,
			},
			FactualAccuracy: 1, DecisionAccuracy: 1, UnsupportedClaims: 0,
			EvidenceTracePassed: true,
		}
		if err := SealMigrationBlindedAgentCaseOutcome(&outcome); err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	blinded := MigrationBlindedAgentEvaluation{
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest,
		BenchmarkDigest: benchmarkRef.Digest, CalibrationDigest: calibrationRef.Digest,
		Evaluator: "independent-manager-evaluator",
		Cases:     []MigrationBlindedAgentCaseOutcome{trial("legacy", 900), trial("compiled", 500)},
	}
	if err := SealMigrationBlindedAgentEvaluation(&blinded); err != nil {
		t.Fatal(err)
	}
	blindedRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/blinded.json", blinded)
	evidence := MigrationRetrievalGateEvidence{
		Benchmark: benchmarkRef, Calibration: calibrationRef,
		CandidateProfile: candidateRef, BlindedEvaluation: blindedRef,
	}
	if err := SealMigrationRetrievalGateEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func migrationStrictHostEvidence(t *testing.T, state MigrationState, hosts ...string) MigrationHostConformanceEvidence {
	t.Helper()
	if len(hosts) == 0 {
		hosts = []string{"mcp", "cli", "claude", "codex"}
	}
	discovery := mustJSONRaw(toolDefinitions())
	semantic := map[string]json.RawMessage{
		"discovery":        discovery,
		"status":           mustJSONRaw(map[string]any{"transactionId": state.TransactionID, "state": state.State}),
		"retrieval":        mustJSONRaw(map[string]any{"status": "ok", "ids": []string{"F-0001"}}),
		"expansion":        mustJSONRaw(map[string]any{"status": "ok", "handle": "finding:F-0001"}),
		"role-boundary":    mustJSONRaw(map[string]any{"code": "role-boundary-refused"}),
		"bounded-recovery": mustJSONRaw(map[string]any{"status": "ok", "transactionId": state.TransactionID}),
	}
	required := map[string][]string{
		"mcp":    {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery"},
		"cli":    {"status", "retrieval", "expansion", "role-boundary", "bounded-recovery"},
		"claude": {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery", "local-fallback"},
		"codex":  {"discovery", "status", "retrieval", "expansion", "role-boundary", "bounded-recovery", "local-fallback"},
	}
	trials := []MigrationHostTrial{}
	for _, host := range hosts {
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
			trial := MigrationHostTrial{
				Host: host, Scenario: scenario, Transport: transport,
				Request: mustJSONRaw(map[string]any{"host": host, "scenario": scenario, "transactionId": state.TransactionID}),
				Result:  semanticResult, SemanticResult: semanticResult,
			}
			switch scenario {
			case "role-boundary":
				trial.Failure = &MigrationHostFailure{Code: "role-boundary-refused", Message: "curator cannot ratify manager truth"}
			case "local-fallback":
				trial.Failure = &MigrationHostFailure{Code: "mcp-unavailable", Message: "MCP unavailable; exact CLI fallback executed"}
			}
			if err := SealMigrationHostTrial(&trial); err != nil {
				t.Fatal(err)
			}
			trials = append(trials, trial)
		}
	}
	evidence := MigrationHostConformanceEvidence{
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest, Trials: trials,
	}
	if err := SealMigrationHostConformanceEvidence(&evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func migrationStrictGateArtifact(
	t *testing.T,
	root string,
	state MigrationState,
	gate string,
) MigrationGateArtifact {
	t.Helper()
	var retrieval *MigrationRetrievalGateEvidence
	var host *MigrationHostConformanceEvidence
	switch gate {
	case "retrieval-context":
		value := migrationStrictRetrievalEvidence(t, root, state)
		retrieval = &value
	case "host-parity":
		value := migrationStrictHostEvidence(t, state)
		host = &value
	default:
		t.Fatalf("strict helper does not support %s", gate)
	}
	artifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest, gate, retrieval, host)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestMigrationGateSpecificEvidenceRejectsGenericStaleTamperedAndFakeInputs(t *testing.T) {
	root := t.TempDir()
	state := MigrationState{TransactionID: "M-1234567890ABCDEF1234", PlanDigest: stateTestDigest("a")}
	engine := &MigrationEngine{ProjectRoot: root, AssetRoot: adversarialAssetRoot(t)}

	generic := MigrationGateArtifact{Gate: "retrieval-context", Passed: true,
		Fingerprints: map[string]string{
			"benchmark": stateTestDigest("a"), "calibration": stateTestDigest("b"),
			"blinded-agent-evaluation": stateTestDigest("c"),
		}}
	if err := engine.validateGateSpecificMigrationEvidence(state, generic); err == nil {
		t.Fatal("generic names and random hashes certified retrieval")
	}

	retrieval := migrationStrictRetrievalEvidence(t, root, state)
	artifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"retrieval-context", &retrieval, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err != nil {
		t.Fatalf("real typed retrieval harness was rejected: %v", err)
	}
	benchmarkPath := filepath.Join(root, filepath.FromSlash(retrieval.Benchmark.Path))
	original, err := os.ReadFile(benchmarkPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, benchmarkPath, string(original)+" ")
	if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err == nil {
		t.Fatal("tampered benchmark remained certification evidence")
	}
	mustWriteFile(t, benchmarkPath, string(original))
	blindedPath := filepath.Join(root, filepath.FromSlash(retrieval.BlindedEvaluation.Path))
	blindedBody, err := os.ReadFile(blindedPath)
	if err != nil {
		t.Fatal(err)
	}
	var fakeBlinded MigrationBlindedAgentEvaluation
	if err := decodeStrict(blindedBody, &fakeBlinded); err != nil {
		t.Fatal(err)
	}
	fakeBlinded.Cases[0].BenchmarkOutcomeDigest = stateTestDigest("9")
	if err := SealMigrationBlindedAgentEvaluation(&fakeBlinded); err != nil {
		t.Fatal(err)
	}
	fakeRetrieval := retrieval
	fakeRetrieval.BlindedEvaluation = writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/fake-blinded.json", fakeBlinded)
	if err := SealMigrationRetrievalGateEvidence(&fakeRetrieval); err != nil {
		t.Fatal(err)
	}
	fakeArtifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"retrieval-context", &fakeRetrieval, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, fakeArtifact); err == nil {
		t.Fatal("fabricated blinded benchmark-outcome binding certified retrieval")
	}

	calibrationPath := filepath.Join(root, filepath.FromSlash(retrieval.Calibration.Path))
	calibrationBody, err := os.ReadFile(calibrationPath)
	if err != nil {
		t.Fatal(err)
	}
	var calibration CalibrationReport
	if err := decodeStrict(calibrationBody, &calibration); err != nil {
		t.Fatal(err)
	}
	calibration.EvalDigest = stateTestDigest("d")
	retrieval.Calibration = writeMigrationEvidenceJSON(t, root, retrieval.Calibration.Path, calibration)
	if err := SealMigrationRetrievalGateEvidence(&retrieval); err != nil {
		t.Fatal(err)
	}
	artifact, err = BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"retrieval-context", &retrieval, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err == nil {
		t.Fatal("stale benchmark/calibration binding certified retrieval")
	}

	host := migrationStrictHostEvidence(t, state)
	host.Trials[0].ID = "generic-host-check"
	if err := SealMigrationHostConformanceEvidence(&host); err != nil {
		t.Fatal(err)
	}
	// Seal recomputes the canonical ID, so forge it after sealing.
	host.Trials[0].ID = "generic-host-check"
	host.ResultDigest = ""
	host.ResultDigest, _ = CanonicalDigest(host)
	hostArtifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"host-parity", nil, &host)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, hostArtifact); err == nil {
		t.Fatal("fabricated host trial name certified parity")
	}
}

func migrationResealedBenchmarkArtifact(
	t *testing.T,
	root string,
	state MigrationState,
	retrieval MigrationRetrievalGateEvidence,
	mutate func(*ProjectBenchmarkReport),
) MigrationGateArtifact {
	t.Helper()
	benchmarkBody, err := os.ReadFile(filepath.Join(
		root, filepath.FromSlash(retrieval.Benchmark.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var benchmark ProjectBenchmarkReport
	if err := decodeStrict(benchmarkBody, &benchmark); err != nil {
		t.Fatal(err)
	}
	mutate(&benchmark)
	for index := range benchmark.Profiles {
		benchmark.Profiles[index].ObservationDigest, err =
			MigrationProjectBenchmarkObservationDigest(benchmark, benchmark.Profiles[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	benchmarkRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/resealed-benchmark.json", benchmark)

	blindedBody, err := os.ReadFile(filepath.Join(
		root, filepath.FromSlash(retrieval.BlindedEvaluation.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var blinded MigrationBlindedAgentEvaluation
	if err := decodeStrict(blindedBody, &blinded); err != nil {
		t.Fatal(err)
	}
	blinded.BenchmarkDigest = benchmarkRef.Digest
	if err := SealMigrationBlindedAgentEvaluation(&blinded); err != nil {
		t.Fatal(err)
	}
	blindedRef := writeMigrationEvidenceJSON(t, root,
		"migration-tests/evidence/resealed-blinded.json", blinded)

	retrieval.Benchmark = benchmarkRef
	retrieval.BlindedEvaluation = blindedRef
	if err := SealMigrationRetrievalGateEvidence(&retrieval); err != nil {
		t.Fatal(err)
	}
	artifact, err := BuildMigrationGateArtifact(
		state.TransactionID, state.PlanDigest, "retrieval-context", &retrieval, nil)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestMigrationCertificationRejectsSelfResealedCorpusAndContextSubstitutions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProjectBenchmarkReport)
	}{
		{name: "chunk-id", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].Cases[0].ChunkIDs[0] = "chunk-" + strings.Repeat("1", 20)
		}},
		{name: "content-hash", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].Cases[0].ContentHashes[0] = strings.Repeat("2", 64)
		}},
		{name: "retrieval-tier", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].Cases[0].Tiers[0] = "history"
		}},
		{name: "context-generation", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].Generation =
				"generation-" + strings.Repeat("3", 20)
		}},
		{name: "context-path-and-tier", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].Paths[0] = "docs/INDEX.md"
			report.Profiles[0].ContextPackCases[0].Tiers[0] = "navigation"
		}},
		{name: "context-tier", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].Tiers[0] = "history"
		}},
		{name: "context-path-tier-cardinality", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].Paths = append(
				report.Profiles[0].ContextPackCases[0].Paths,
				"docs/backlog/next.md",
			)
		}},
		{name: "context-pack-id", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].PackID = "context-self-authored"
		}},
		{name: "context-pack-digest", mutate: func(report *ProjectBenchmarkReport) {
			report.Profiles[0].ContextPackCases[0].Digest = "sha256:not-a-digest"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			state := MigrationState{
				TransactionID: "M-1234567890ABCDEF1234", PlanDigest: stateTestDigest("a"),
			}
			retrieval := migrationStrictRetrievalEvidence(t, root, state)
			artifact := migrationResealedBenchmarkArtifact(
				t, root, state, retrieval, test.mutate)
			engine := &MigrationEngine{ProjectRoot: root, AssetRoot: adversarialAssetRoot(t)}
			if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err == nil {
				t.Fatal("self-resealed corpus/context substitution certified retrieval")
			}
		})
	}
}

func TestMigrationCertificationRejectsSelfResealedFindingRowSubstitutions(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	settings := KnowledgeSettings{Sources: SourceSettings{
		ActiveFindings: true, ReportFallback: true,
	}}
	manifest, err := migrationPackagedModelManifest()
	if err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := ProbeRuntimeIdentity(manifest)
	if err != nil {
		t.Fatal(err)
	}
	environment := MigrationCertificationEnvironment{
		ProjectRoot: fixture.boundary.Root, CorpusFingerprint: fixture.inventory.Fingerprint,
		ModelFingerprint: modelIndexFingerprint(manifest), ParserVersion: ParserVersion,
		ChunkerVersion: ChunkerVersion, Runtime: runtimeIdentity,
		RuntimeContract: RuntimeContract(runtimeIdentity), Settings: settings,
		// A nonempty suite inventory requests the exact isolated replay. The row
		// test below deliberately uses a compact case set rather than weakening
		// the production suite validator's 48-case ratification floor.
		FindingSuites:          []FindingEvalSuite{{ID: "row-replay-fixture"}},
		certificationAssetRoot: adversarialAssetRoot(t),
	}
	cleanup, err := environment.prepareCertificationReplay()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	var finding FindingDocument
	for _, candidate := range fixture.inventory.Findings {
		if candidate.Record.ID == "F-0042" {
			finding = candidate
			break
		}
	}
	if finding.Record.ID == "" || len(finding.Record.Evidence) == 0 {
		t.Fatal("finding replay fixture lacks F-0042 evidence")
	}
	caseRow := FindingEvalCase{
		ID: "migration-finding-replay", Role: "manager", Topic: "registration",
		Split: "development", Query: "Which table drives resource registration?",
		QueryClass: "conceptual", AllowedSourceClasses: []string{"campaign"},
		AllowedReviewStates: []string{"manager-ratified"},
		AllowedValidities:   []string{"current"}, TokenBudget: 2048,
		ExpectedFindingIDs:     []string{"F-0042"},
		ExpectedFindingHandles: []string{FindingHandle("F-0042")},
		ExpectedEvidenceHandles: []string{
			EvidenceHandle("F-0042", finding.Record.Evidence[0]),
		},
		ExpectedRawPaths: []string{
			"active/resource-registration/runs/R-20260802-0001/report.md",
		},
		ExpectedSourceClasses:  map[string]string{"F-0042": "campaign"},
		ExpectedReviewStates:   map[string]string{"F-0042": "manager-ratified"},
		ExpectedValidities:     map[string]string{"F-0042": "current"},
		HardNegativeFindingIDs: []string{"F-0044"}, Answerable: true,
	}
	selected := fixture.retriever.Profile
	expected, err := EvaluateFindingRetriever(context.Background(), Retriever{
		Boundary: fixture.boundary, Generation: *environment.replayGeneration,
		Profile: selected,
	}, []FindingEvalCase{caseRow})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMigrationFindingRows(
		expected.Cases, []FindingEvalCase{caseRow}, environment, selected); err != nil {
		t.Fatalf("exact independent finding replay was rejected: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*FindingCaseOutcome)
	}{
		{name: "finding-handles-complete", mutate: func(row *FindingCaseOutcome) {
			row.FindingHandlesComplete = !row.FindingHandlesComplete
		}},
		{name: "evidence-handles-complete", mutate: func(row *FindingCaseOutcome) {
			row.EvidenceHandlesComplete = !row.EvidenceHandlesComplete
		}},
		{name: "evidence-handle", mutate: func(row *FindingCaseOutcome) {
			row.EvidenceHandlesFound = []string{"evidence:F-0042:00000000000000000000"}
		}},
		{name: "source-state", mutate: func(row *FindingCaseOutcome) {
			row.SourceClassesAccurate = !row.SourceClassesAccurate
		}},
		{name: "review-state", mutate: func(row *FindingCaseOutcome) {
			row.ReviewStatesAccurate = !row.ReviewStatesAccurate
		}},
		{name: "validity", mutate: func(row *FindingCaseOutcome) {
			row.ValiditiesAccurate = !row.ValiditiesAccurate
		}},
		{name: "status", mutate: func(row *FindingCaseOutcome) {
			row.Status = "abstained"
		}},
		{name: "normalized-token-cost", mutate: func(row *FindingCaseOutcome) {
			row.NormalizedTokens++
		}},
		{name: "raw-token-cost", mutate: func(row *FindingCaseOutcome) {
			row.RawTokens++
		}},
		{name: "lane-count", mutate: func(row *FindingCaseOutcome) {
			row.LaneRelevantHits["exact"] = -1
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			body, err := json.Marshal(expected.Cases)
			if err != nil {
				t.Fatal(err)
			}
			var rows []FindingCaseOutcome
			if err := json.Unmarshal(body, &rows); err != nil {
				t.Fatal(err)
			}
			mutation.mutate(&rows[0])
			resealed := expected
			resealed.Cases = rows
			if _, err := sealFindingAblationReport(resealed); err != nil {
				t.Fatal(err)
			}
			if err := validateMigrationFindingRows(
				rows, []FindingEvalCase{caseRow}, environment, selected); err == nil {
				t.Fatal("self-resealed finding-row substitution certified retrieval")
			}
		})
	}
}

func TestMigrationHostConformanceAcceptsCompleteRealSchemaMatrix(t *testing.T) {
	root := migrationPreviewFixture(t)
	messages := runMCPMessages(t, &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "migration-host-harness", "version": "1"},
		}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
	)
	actualTools := asArray(t, asObject(t, rpcResponseByID(t, messages, 2)["result"])["tools"])
	actualDigest, _, err := migrationCanonicalRawDigest(mustJSONRaw(actualTools))
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest, _, err := migrationCanonicalRawDigest(MigrationHostDiscoverySchema())
	if err != nil || actualDigest != expectedDigest {
		t.Fatalf("real MCP tools/list diverged from host certification schema: got=%s want=%s err=%v", actualDigest, expectedDigest, err)
	}

	state := MigrationState{TransactionID: "M-1234567890ABCDEF1234", PlanDigest: stateTestDigest("a"), State: "physically-reorganized"}
	evidence := migrationStrictHostEvidence(t, state)
	artifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"host-parity", nil, &evidence)
	if err != nil {
		t.Fatal(err)
	}
	engine := &MigrationEngine{ProjectRoot: t.TempDir()}
	if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err != nil {
		t.Fatalf("complete captured host request/result/failure matrix was rejected: %v", err)
	}
	seen := []string{}
	for _, trial := range evidence.Trials {
		seen = append(seen, trial.Host+":"+trial.Scenario)
	}
	sort.Strings(seen)
	if len(seen) != 25 {
		t.Fatalf("host matrix has %d trials, want 25", len(seen))
	}
}

func TestMigrationHostParityCoversConfiguredHostsAndCodexIsOptional(t *testing.T) {
	state := MigrationState{TransactionID: "M-1234567890ABCDEF1234", PlanDigest: stateTestDigest("a"), State: "physically-reorganized"}
	engine := &MigrationEngine{ProjectRoot: t.TempDir()}

	// A project without the Codex host proves the complete matrix over the
	// mandatory surfaces: MCP, CLI, and Claude Code.
	evidence := migrationStrictHostEvidence(t, state, "mcp", "cli", "claude")
	if len(evidence.Trials) != 18 {
		t.Fatalf("mandatory host matrix has %d trials, want 18", len(evidence.Trials))
	}
	artifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"host-parity", nil, &evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err != nil {
		t.Fatalf("complete mandatory-host matrix was rejected: %v", err)
	}
	if _, ok := artifact.Fingerprints["codex-host"]; ok {
		t.Fatalf("an absent optional host must not carry a fingerprint: %+v", artifact.Fingerprints)
	}
	for _, name := range []string{"mcp", "cli", "claude-host"} {
		if !digestRE.MatchString(artifact.Fingerprints[name]) {
			t.Fatalf("mandatory host fingerprint %s missing: %+v", name, artifact.Fingerprints)
		}
	}

	// A mandatory host can never be dropped.
	for _, missing := range []string{"mcp", "cli", "claude"} {
		kept := []string{}
		for _, host := range []string{"mcp", "cli", "claude"} {
			if host != missing {
				kept = append(kept, host)
			}
		}
		partial := migrationStrictHostEvidence(t, state, kept...)
		if _, _, err := validateMigrationHostConformanceEvidence(state, state.PlanDigest, partial); err == nil {
			t.Fatalf("host evidence without mandatory host %s was accepted", missing)
		}
	}

	// When Codex trials are present, the complete Codex scenario matrix is
	// required: partial optional-host coverage is rejected.
	full := migrationStrictHostEvidence(t, state, "mcp", "cli", "claude", "codex")
	trimmed := []MigrationHostTrial{}
	for _, trial := range full.Trials {
		if trial.Host == "codex" && trial.Scenario == "local-fallback" {
			continue
		}
		trimmed = append(trimmed, trial)
	}
	partialCodex := MigrationHostConformanceEvidence{
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest, Trials: trimmed,
	}
	if err := SealMigrationHostConformanceEvidence(&partialCodex); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateMigrationHostConformanceEvidence(state, state.PlanDigest, partialCodex); err == nil ||
		!strings.Contains(err.Error(), "codex/local-fallback") {
		t.Fatalf("partial optional-host coverage was accepted: %v", err)
	}

	// A fully captured Codex host still validates and carries its fingerprint.
	fullArtifact, err := BuildMigrationGateArtifact(state.TransactionID, state.PlanDigest,
		"host-parity", nil, &full)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.validateGateSpecificMigrationEvidence(state, fullArtifact); err != nil {
		t.Fatalf("complete four-host matrix was rejected: %v", err)
	}
	if !digestRE.MatchString(fullArtifact.Fingerprints["codex-host"]) {
		t.Fatalf("captured codex host lost its fingerprint: %+v", fullArtifact.Fingerprints)
	}
}

func TestBlindedEvaluationPassRestsOnPairedNonInferiority(t *testing.T) {
	trial := func(condition string, factual float64) MigrationBlindedAgentCaseOutcome {
		outcome := MigrationBlindedAgentCaseOutcome{
			CaseID: "case-1", Condition: condition, Agent: "claude-code", Model: "m",
			Answerable: true,
			Request: MigrationBlindedAgentRequest{
				CaseID: "case-1", Query: "q", QueryClass: "exact", Role: "manager",
				AllowedTiers: []string{"truth"}, TokenBudget: 1024,
			},
			Response: MigrationBlindedAgentResponse{
				AnswerClaims: []string{"claim"}, Decision: "answer",
				Citations: []string{"docs/truth/a.md"}, DurabilityLabels: []string{"truth"},
				ContextTokens: 100,
			},
			FactualAccuracy: factual, DecisionAccuracy: factual, EvidenceTracePassed: factual >= 0.5,
		}
		if factual < 0.5 {
			// A missed baseline: the agent answered but its evidence did not
			// support the case.
			outcome.EvidenceTracePassed = false
		}
		return outcome
	}
	evaluation := MigrationBlindedAgentEvaluation{
		TransactionID: "M-1234567890ABCDEF1234", PlanDigest: stateTestDigest("a"),
		BenchmarkDigest: stateTestDigest("b"), CalibrationDigest: stateTestDigest("c"),
		Evaluator: "deterministic-judgment:test",
		Cases: []MigrationBlindedAgentCaseOutcome{
			trial("legacy", 0), trial("compiled", 1),
		},
	}
	if err := SealMigrationBlindedAgentEvaluation(&evaluation); err != nil {
		t.Fatal(err)
	}
	if evaluation.Cases[0].Passed || !evaluation.Cases[1].Passed {
		t.Fatalf("per-trial results must record the measured outcome: %+v", evaluation.Cases)
	}
	if !evaluation.Passed {
		t.Fatal("a measured legacy baseline miss must not fail the evaluation the compiled arm passed")
	}
	failing := MigrationBlindedAgentEvaluation{
		TransactionID: evaluation.TransactionID, PlanDigest: evaluation.PlanDigest,
		BenchmarkDigest: evaluation.BenchmarkDigest, CalibrationDigest: evaluation.CalibrationDigest,
		Evaluator: evaluation.Evaluator,
		Cases: []MigrationBlindedAgentCaseOutcome{
			trial("legacy", 1), trial("compiled", 0),
		},
	}
	if err := SealMigrationBlindedAgentEvaluation(&failing); err != nil {
		t.Fatal(err)
	}
	if failing.Passed {
		t.Fatal("a compiled trial that regresses its measured baseline must fail the evaluation")
	}
	sharedMiss := MigrationBlindedAgentEvaluation{
		TransactionID: evaluation.TransactionID, PlanDigest: evaluation.PlanDigest,
		BenchmarkDigest: evaluation.BenchmarkDigest, CalibrationDigest: evaluation.CalibrationDigest,
		Evaluator: evaluation.Evaluator,
		Cases: []MigrationBlindedAgentCaseOutcome{
			trial("legacy", 0), trial("compiled", 0),
		},
	}
	if err := SealMigrationBlindedAgentEvaluation(&sharedMiss); err != nil {
		t.Fatal(err)
	}
	if !sharedMiss.Passed {
		t.Fatal("a case both stochastic arms miss is a respondent measurement, not a migration regression")
	}
	legacyOnly := MigrationBlindedAgentEvaluation{
		TransactionID: evaluation.TransactionID, PlanDigest: evaluation.PlanDigest,
		BenchmarkDigest: evaluation.BenchmarkDigest, CalibrationDigest: evaluation.CalibrationDigest,
		Evaluator: evaluation.Evaluator,
		Cases: []MigrationBlindedAgentCaseOutcome{
			trial("legacy", 1),
		},
	}
	if err := SealMigrationBlindedAgentEvaluation(&legacyOnly); err != nil {
		t.Fatal(err)
	}
	if legacyOnly.Passed {
		t.Fatal("an evaluation without a compiled trial cannot pass")
	}
}
