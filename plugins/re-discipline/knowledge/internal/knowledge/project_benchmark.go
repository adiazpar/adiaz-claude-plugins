package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectProfileBenchmark struct {
	ProfileName            string                          `json:"profileName"`
	EffectiveProfile       string                          `json:"effectiveProfile"`
	ActiveLanes            []string                        `json:"activeLanes"`
	Models                 []ModelIdentity                 `json:"models"`
	FallbackReason         *string                         `json:"fallbackReason"`
	ObservationDigest      string                          `json:"observationDigest"`
	HardGatesPassed        bool                            `json:"hardGatesPassed"`
	NonInferiorToLexical   bool                            `json:"nonInferiorToLexical"`
	Cases                  []CaseOutcome                   `json:"cases"`
	CasesByBudget          map[string][]CaseOutcome        `json:"casesByBudget"`
	ContextPackCases       []ContextPackOutcome            `json:"contextPackCases"`
	ContextPacksByBudget   map[string][]ContextPackOutcome `json:"contextPacksByBudget"`
	ContextPackRoles       []string                        `json:"contextPackRoles"`
	ContextPackPassed      bool                            `json:"contextPackPassed"`
	Metrics                QualityMetrics                  `json:"metrics"`
	MetricsBySplit         map[string]QualityMetrics       `json:"metricsBySplit"`
	MetricsByBudget        map[string]QualityMetrics       `json:"metricsByBudget"`
	QualityMetricsByBudget map[string]QualityMetrics       `json:"qualityMetricsByBudget"`
}

type UnsupportedProjectProfile struct {
	ProfileName string `json:"profileName"`
	Reason      string `json:"reason"`
}

type ProjectBenchmarkReport struct {
	SchemaVersion       int                         `json:"schemaVersion"`
	RunID               string                      `json:"runId"`
	Mode                string                      `json:"mode"`
	Suite               string                      `json:"suite"`
	RequestedProfile    string                      `json:"requestedProfile"`
	Generation          GenerationSummary           `json:"generation"`
	EvalFingerprint     string                      `json:"evalFingerprint"`
	Profiles            []ProjectProfileBenchmark   `json:"profiles"`
	UnsupportedProfiles []UnsupportedProjectProfile `json:"unsupportedProfiles"`
	Passed              bool                        `json:"passed"`
	Complete            bool                        `json:"complete"`
	DurationMillis      int64                       `json:"durationMillis"`
	ReportPath          string                      `json:"reportPath"`
}

// RunProjectBenchmark measures the accepted profile against ratified project
// evals. Its only writes are index/cache maintenance and this cache report.
func (service *Service) RunProjectBenchmark(
	ctx context.Context,
	mode string,
) (ProjectBenchmarkReport, error) {
	if mode != "quick" && mode != "full" {
		return ProjectBenchmarkReport{}, errors.New(
			"project benchmark mode must be quick or full")
	}
	refreshed, err := NewService(ServiceOptions{
		ProjectRoot: service.Boundary.Root,
		AssetRoot:   service.AssetRoot,
		CacheRoot:   service.Index.CacheRoot,
	})
	if err != nil {
		return ProjectBenchmarkReport{}, fmt.Errorf(
			"revalidate project benchmark control plane: %w", err)
	}
	service = refreshed
	if !service.Configuration.Valid {
		return ProjectBenchmarkReport{}, fmt.Errorf(
			"project benchmark requires valid configuration: %s",
			strings.Join(service.Configuration.Errors, "; "),
		)
	}
	if !service.Configuration.Bootstrap.Knowledge.Enabled {
		return ProjectBenchmarkReport{}, errors.New(
			"project benchmark cannot run while the knowledge server is disabled")
	}
	for _, warning := range service.Warnings {
		if strings.Contains(warning, "project retrieval profile") {
			return ProjectBenchmarkReport{}, fmt.Errorf(
				"project benchmark requires an accepted retrieval profile: %s", warning)
		}
	}
	started := time.Now()
	cases, err := service.loadProjectEvalCases()
	if err != nil {
		return ProjectBenchmarkReport{}, err
	}
	// The run pins and leases the generation it starts on. Another session may
	// publish and prune throughout the run without failing it, and the report
	// records the generation the measurements were actually taken against.
	generation, active, lease, err := service.leaseMeasurementGeneration(ctx, "benchmark")
	if err != nil {
		return ProjectBenchmarkReport{}, err
	}
	defer lease.Release()
	service.PinGeneration(generation)
	defer service.flushTelemetry()
	evalFingerprint, _ := CanonicalDigest(cases)
	report := ProjectBenchmarkReport{
		SchemaVersion: 1, RunID: nowRunID("benchmark"), Mode: mode,
		Suite: "project-benchmark-v1", RequestedProfile: active.RequestedIdentity,
		Generation: PublicGeneration(generation), EvalFingerprint: evalFingerprint,
		Profiles: []ProjectProfileBenchmark{}, UnsupportedProfiles: []UnsupportedProjectProfile{},
		Passed: true, Complete: true,
	}
	rows := []EffectiveProfile{active.Effective}
	if mode == "full" {
		rows = append([]EffectiveProfile(nil), service.ProfileCatalog.EffectiveProfiles...)
		sort.Slice(rows, func(i, j int) bool {
			left, right := capabilityPreference(rows[i]), capabilityPreference(rows[j])
			if left != right {
				return left < right
			}
			return rows[i].Name < rows[j].Name
		})
	}
	for _, row := range rows {
		selected, selectErr := service.selectProfileRow(generation.Runtime, row)
		if selectErr != nil || selected.Effective.Name != row.Name {
			reason := "capability-unavailable"
			if selectErr != nil {
				reason = selectErr.Error()
			}
			report.UnsupportedProfiles = append(
				report.UnsupportedProfiles,
				UnsupportedProjectProfile{ProfileName: row.Name, Reason: reason},
			)
			report.Complete = false
			continue
		}
		benchmark, benchmarkErr := benchmarkProjectProfile(
			ctx, service, generation, selected, row, cases,
			evalFingerprint, mode == "full",
		)
		if benchmarkErr != nil {
			return ProjectBenchmarkReport{}, benchmarkErr
		}
		report.Profiles = append(report.Profiles, benchmark)
		report.Passed = report.Passed && benchmark.HardGatesPassed
	}
	if len(report.Profiles) == 0 {
		return ProjectBenchmarkReport{}, errors.New(
			"no executable effective profile is available for project benchmark")
	}
	if mode == "full" {
		applyProjectNonInferiority(&report)
		if !report.Complete {
			report.Passed = false
		}
	}
	report.DurationMillis = time.Since(started).Milliseconds()
	reportPath, err := containedOutputPath(
		service.Index.CacheRoot,
		filepath.Join("benchmarks", report.RunID, "report.json"),
	)
	if err != nil {
		return ProjectBenchmarkReport{}, err
	}
	report.ReportPath = filepath.ToSlash(reportPath)
	if err := AtomicWriteJSON(
		filepath.FromSlash(report.ReportPath), report, 0o600,
	); err != nil {
		return ProjectBenchmarkReport{}, err
	}
	return report, nil
}

func benchmarkProjectProfile(
	ctx context.Context,
	service *Service,
	generation Generation,
	selected SelectedProfile,
	row EffectiveProfile,
	cases []EvalCase,
	evalFingerprint string,
	includeBudgetMatrix bool,
) (ProjectProfileBenchmark, error) {
	retriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}
	outcomes, metrics, err := evaluateRetrieverCases(ctx, retriever, cases)
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	hardGates := hardMetricsPassed(metrics)
	for _, outcome := range outcomes {
		hardGates = hardGates && evaluationOutcomePassed(outcome)
	}
	profileService, err := NewService(ServiceOptions{
		ProjectRoot:          service.Boundary.Root,
		AssetRoot:            service.AssetRoot,
		CacheRoot:            service.Index.CacheRoot,
		EffectiveProfileName: row.Name,
	})
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	// Every per-profile service measures the run-pinned generation, so the
	// check below compares against what this run measures rather than against
	// whichever generation another session has since published.
	profileService.PinGeneration(generation)
	profileGeneration, _, profileSelected, _, err := profileService.ensure(ctx)
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	if profileGeneration.ID != generation.ID ||
		profileSelected.EffectiveIdentity != selected.EffectiveIdentity {
		return ProjectProfileBenchmark{}, errors.New(
			"context-pack benchmark profile or generation drifted")
	}
	contextPackCases, contextPackRoles, err := evaluateContextPackCases(
		ctx, profileService, cases, 0,
	)
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	contextPackPassed := contextPackOutcomesPassed(contextPackCases) &&
		contains(contextPackRoles, "manager") &&
		contains(contextPackRoles, "drafter")
	hardGates = hardGates && contextPackPassed
	developmentOutcomes, developmentCases :=
		filterEvalSplit(outcomes, cases, "development")
	holdoutOutcomes, holdoutCases := filterEvalSplit(outcomes, cases, "holdout")
	bySplit := map[string]QualityMetrics{
		"development": calculateMetrics(developmentOutcomes, developmentCases),
		"holdout":     calculateMetrics(holdoutOutcomes, holdoutCases),
	}
	byBudget := map[string]QualityMetrics{}
	qualityByBudget := map[string]QualityMetrics{}
	casesByBudget := map[string][]CaseOutcome{}
	contextPacksByBudget := map[string][]ContextPackOutcome{}
	if includeBudgetMatrix {
		for _, budget := range []int{512, 1024, 2048, 4096} {
			budgetOutcomes := make([]CaseOutcome, 0, len(cases))
			for _, eval := range cases {
				outcome, runErr := runEvaluationCase(
					ctx, eval, budget, retriever.Boundary.Root, retriever.Search)
				if runErr != nil {
					return ProjectProfileBenchmark{}, fmt.Errorf(
						"case %s at budget %d: %w", eval.ID, budget, runErr)
				}
				budgetOutcomes = append(budgetOutcomes, outcome)
				hardGates = hardGates && evaluationOutcomePassed(outcome)
			}
			key := fmt.Sprintf("%d", budget)
			casesByBudget[key] = budgetOutcomes
			byBudget[key] = calculateMetrics(budgetOutcomes, cases)
			qualityByBudget[key] = applicableMetrics(budgetOutcomes, cases)
			if hasApplicableQualityGate(budgetOutcomes) {
				hardGates = hardGates && hardMetricsPassed(qualityByBudget[key])
			}
			contextOutcomes, roles, contextErr := evaluateContextPackCases(
				ctx, profileService, cases, budget,
			)
			if contextErr != nil {
				return ProjectProfileBenchmark{}, fmt.Errorf(
					"context packs at budget %d: %w", budget, contextErr)
			}
			contextPacksByBudget[key] = contextOutcomes
			contextBudgetPassed := contextPackOutcomesPassed(contextOutcomes) &&
				contains(roles, "manager") && contains(roles, "drafter")
			contextPackPassed = contextPackPassed && contextBudgetPassed
			hardGates = hardGates && contextBudgetPassed
		}
	}
	digestCases := append([]CaseOutcome(nil), outcomes...)
	for index := range digestCases {
		digestCases[index].LatencyMillis = 0
	}
	digestBudgets := map[string]QualityMetrics{}
	digestQualityBudgets := map[string]QualityMetrics{}
	for budget, budgetMetrics := range byBudget {
		digestBudgets[budget] = metricsWithoutLatency(budgetMetrics)
		digestQualityBudgets[budget] =
			metricsWithoutLatency(qualityByBudget[budget])
	}
	digestBudgetCases := map[string][]CaseOutcome{}
	for budget, budgetCases := range casesByBudget {
		copied := append([]CaseOutcome(nil), budgetCases...)
		for index := range copied {
			copied[index].LatencyMillis = 0
		}
		digestBudgetCases[budget] = copied
	}
	digest, err := CanonicalDigest(struct {
		Suite                  string                          `json:"suite"`
		EffectiveProfile       EffectiveProfile                `json:"effectiveProfile"`
		EffectiveIdentity      string                          `json:"effectiveIdentity"`
		EvalFingerprint        string                          `json:"evalFingerprint"`
		CorpusFingerprint      string                          `json:"corpusFingerprint"`
		Runtime                RuntimeContractIdentity         `json:"runtime"`
		Models                 []ModelIdentity                 `json:"models"`
		Cases                  []CaseOutcome                   `json:"cases"`
		Metrics                QualityMetrics                  `json:"metrics"`
		MetricsBySplit         map[string]QualityMetrics       `json:"metricsBySplit"`
		MetricsByBudget        map[string]QualityMetrics       `json:"metricsByBudget"`
		QualityMetricsByBudget map[string]QualityMetrics       `json:"qualityMetricsByBudget"`
		CasesByBudget          map[string][]CaseOutcome        `json:"casesByBudget"`
		ContextPackCases       []ContextPackOutcome            `json:"contextPackCases"`
		ContextPacksByBudget   map[string][]ContextPackOutcome `json:"contextPacksByBudget"`
		ContextPackRoles       []string                        `json:"contextPackRoles"`
	}{
		"project-benchmark-v1", runtimeEffectiveProfile(row),
		selected.EffectiveIdentity, evalFingerprint, generation.CorpusFingerprint,
		RuntimeContract(selected.Runtime), selected.Models, digestCases,
		metricsWithoutLatency(metrics),
		map[string]QualityMetrics{
			"development": metricsWithoutLatency(bySplit["development"]),
			"holdout":     metricsWithoutLatency(bySplit["holdout"]),
		},
		digestBudgets, digestQualityBudgets, digestBudgetCases,
		contextPackOutcomesForDigest(contextPackCases),
		contextPackBudgetsForDigest(contextPacksByBudget), contextPackRoles,
	})
	if err != nil {
		return ProjectProfileBenchmark{}, err
	}
	return ProjectProfileBenchmark{
		ProfileName: row.Name, EffectiveProfile: selected.EffectiveIdentity,
		ActiveLanes:    append([]string(nil), selected.ActiveLanes...),
		Models:         append([]ModelIdentity(nil), selected.Models...),
		FallbackReason: selected.FallbackReason, ObservationDigest: digest,
		HardGatesPassed: hardGates, NonInferiorToLexical: true,
		Cases: outcomes, CasesByBudget: casesByBudget,
		ContextPackCases:     contextPackCases,
		ContextPacksByBudget: contextPacksByBudget,
		ContextPackRoles:     contextPackRoles,
		ContextPackPassed:    contextPackPassed,
		Metrics:              metrics, MetricsBySplit: bySplit,
		MetricsByBudget:        byBudget,
		QualityMetricsByBudget: qualityByBudget,
	}, nil
}

func applyProjectNonInferiority(report *ProjectBenchmarkReport) {
	baselineIndex := -1
	for index := range report.Profiles {
		if report.Profiles[index].ProfileName == "lexical-graph-v1" {
			baselineIndex = index
			break
		}
	}
	if baselineIndex < 0 {
		report.Complete = false
		return
	}
	baseline := report.Profiles[baselineIndex].MetricsBySplit["holdout"]
	for index := range report.Profiles {
		if index == baselineIndex {
			continue
		}
		metrics := report.Profiles[index].MetricsBySplit["holdout"]
		nonInferior := metrics.RecallAtK >= baseline.RecallAtK &&
			metrics.NDCG+0.02 >= baseline.NDCG &&
			metrics.CompleteEvidenceCoverage >= baseline.CompleteEvidenceCoverage &&
			metrics.CitationRecall >= baseline.CitationRecall &&
			metrics.AuthorityViolations <= baseline.AuthorityViolations
		report.Profiles[index].NonInferiorToLexical = nonInferior
		if !nonInferior {
			report.Profiles[index].HardGatesPassed = false
			report.Passed = false
		}
	}
}
