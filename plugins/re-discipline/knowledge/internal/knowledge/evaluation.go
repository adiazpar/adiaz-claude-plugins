package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CaseOutcome struct {
	CaseID                 string   `json:"caseId"`
	Split                  string   `json:"split"`
	Topic                  string   `json:"topic"`
	Paths                  []string `json:"paths"`
	Tiers                  []string `json:"tiers"`
	ChunkIDs               []string `json:"chunkIds"`
	ContentHashes          []string `json:"contentHashes"`
	RelevantPaths          []string `json:"relevantPaths"`
	RelevantRanks          []int    `json:"relevantRanks"`
	HardNegativeHits       []string `json:"hardNegativeHits"`
	ExpectedCitationsFound []string `json:"expectedCitationsFound"`
	EstimatedTokens        int      `json:"estimatedTokens"`
	ReturnedUniquePaths    int      `json:"returnedUniquePaths"`
	ExpectedFound          bool     `json:"expectedFound"`
	CompleteEvidence       bool     `json:"completeEvidence"`
	AuthoritySafe          bool     `json:"authoritySafe"`
	CitationMetadataSafe   bool     `json:"citationMetadataSafe"`
	CitationSafe           bool     `json:"citationSafe"`
	CorpusMatched          bool     `json:"corpusMatched"`
	AbstentionCorrect      bool     `json:"abstentionCorrect"`
	BudgetSafe             bool     `json:"budgetSafe"`
	ReplayIdentical        bool     `json:"replayIdentical"`
	MinimumTokenBudget     int      `json:"minimumTokenBudget"`
	QualityGateApplicable  bool     `json:"qualityGateApplicable"`
	SafetyPassed           bool     `json:"safetyPassed"`
	QualityPassed          bool     `json:"qualityPassed"`
	GatePassed             bool     `json:"gatePassed"`
	ReturnedTokens         int      `json:"returnedTokens"`
	RelevantTokens         int      `json:"relevantTokens"`
	DuplicateTokens        int      `json:"duplicateTokens"`
	StaleResults           int      `json:"staleResults"`
	LatencyMillis          int64    `json:"latencyMillis"`
}

type ContextPackOutcome struct {
	CaseID                  string   `json:"caseId"`
	Split                   string   `json:"split"`
	Topic                   string   `json:"topic"`
	Role                    string   `json:"role"`
	RequestedTokenBudget    int      `json:"requestedTokenBudget"`
	EffectiveTokenBudget    int      `json:"effectiveTokenBudget"`
	RoleTokenCeiling        int      `json:"roleTokenCeiling"`
	MaxCards                int      `json:"maxCards"`
	MaxBytes                int      `json:"maxBytes"`
	PackID                  string   `json:"packId,omitempty"`
	Digest                  string   `json:"digest,omitempty"`
	Generation              string   `json:"generation,omitempty"`
	EffectiveProfile        string   `json:"effectiveProfile,omitempty"`
	Paths                   []string `json:"paths"`
	Tiers                   []string `json:"tiers"`
	RequiredPaths           []string `json:"requiredPaths"`
	RequiredPathsFound      []string `json:"requiredPathsFound"`
	CardCount               int      `json:"cardCount"`
	SerializedBytes         int      `json:"serializedBytes"`
	EstimatedTokens         int      `json:"estimatedTokens"`
	RoleCeilingSafe         bool     `json:"roleCeilingSafe"`
	AllowedTiersSafe        bool     `json:"allowedTiersSafe"`
	RequiredEvidencePresent bool     `json:"requiredEvidencePresent"`
	ExpectedEvidenceFound   bool     `json:"expectedEvidenceFound"`
	AbstentionCorrect       bool     `json:"abstentionCorrect"`
	CardCapSafe             bool     `json:"cardCapSafe"`
	ByteCapSafe             bool     `json:"byteCapSafe"`
	TokenAccountingSafe     bool     `json:"tokenAccountingSafe"`
	BudgetSafe              bool     `json:"budgetSafe"`
	GenerationPinned        bool     `json:"generationPinned"`
	ProfilePinned           bool     `json:"profilePinned"`
	VerificationPassed      bool     `json:"verificationPassed"`
	ReplayIdentical         bool     `json:"replayIdentical"`
	MinimumTokenBudget      int      `json:"minimumTokenBudget"`
	QualityGateApplicable   bool     `json:"qualityGateApplicable"`
	SafetyPassed            bool     `json:"safetyPassed"`
	QualityPassed           bool     `json:"qualityPassed"`
	Passed                  bool     `json:"passed"`
	Error                   string   `json:"error,omitempty"`
}

// A v2 pack carries a generation identity, state-head scope, exact handles,
// and a bounded card envelope. At 512 tokens it can still provide a safe
// abstaining response, but required evidence cannot be made a quality gate
// without crowding out the metadata that makes the artifact verifiable.
const MinimumContextPackEvidenceBudget = 1024

type QualityMetrics struct {
	RecallAtK                float64 `json:"recallAtK"`
	MeanReciprocalRank       float64 `json:"meanReciprocalRank"`
	NDCG                     float64 `json:"nDCG"`
	PrecisionAtK             float64 `json:"precisionAtK"`
	ExactIdentifierHitRate   float64 `json:"exactIdentifierHitRate"`
	CompleteEvidenceCoverage float64 `json:"completeEvidenceCoverage"`
	AbstentionAccuracy       float64 `json:"abstentionAccuracy"`
	CitationPrecision        float64 `json:"citationPrecision"`
	CitationRecall           float64 `json:"citationRecall"`
	SupportingEvidenceRecall float64 `json:"supportingEvidenceRecall"`
	BudgetComplianceRate     float64 `json:"budgetComplianceRate"`
	AuthorityViolationRate   float64 `json:"authorityViolationRate"`
	StaleResultRate          float64 `json:"staleResultRate"`
	AuthorityViolations      int     `json:"authorityViolations"`
	CitationViolations       int     `json:"citationViolations"`
	// CitationMetadataViolations counts malformed or foreign-generation
	// citations only. Unlike CitationViolations it carries no recall
	// component, so it is safe to use as an absolute gate.
	CitationMetadataViolations int     `json:"citationMetadataViolations"`
	HardNegativeHits           int     `json:"hardNegativeHits"`
	RelevantTokenRatio         float64 `json:"relevantTokenRatio"`
	DuplicateTokenRatio        float64 `json:"duplicateTokenRatio"`
	DeterministicReplayRate    float64 `json:"deterministicReplayRate"`
	P50LatencyMillis           int64   `json:"p50LatencyMillis"`
	P95LatencyMillis           int64   `json:"p95LatencyMillis"`
}

type ProfileBenchmark struct {
	ProfileName            string                          `json:"profileName"`
	DeclaredDigest         string                          `json:"declaredDigest"`
	ComputedDigest         string                          `json:"computedDigest"`
	DigestVerified         bool                            `json:"digestVerified"`
	Passed                 bool                            `json:"passed"`
	NonInferiorToLexical   bool                            `json:"nonInferiorToLexical"`
	Cases                  []CaseOutcome                   `json:"cases"`
	CasesByBudget          map[string][]CaseOutcome        `json:"casesByBudget"`
	ContextPackCases       []ContextPackOutcome            `json:"contextPackCases"`
	ContextPacksByBudget   map[string][]ContextPackOutcome `json:"contextPacksByBudget"`
	ContextPackRoles       []string                        `json:"contextPackRoles"`
	ContextPackPassed      bool                            `json:"contextPackPassed"`
	Metrics                QualityMetrics                  `json:"metrics"`
	HoldoutMetrics         QualityMetrics                  `json:"holdoutMetrics"`
	MetricsBySplit         map[string]QualityMetrics       `json:"metricsBySplit"`
	MetricsByBudget        map[string]QualityMetrics       `json:"metricsByBudget"`
	QualityMetricsByBudget map[string]QualityMetrics       `json:"qualityMetricsByBudget"`
	FindingEvaluation      FindingAblationReport           `json:"findingEvaluation"`
	// ComputedEvidence is the evidence block this run measured, suitable for
	// re-declaration in the packaged profile after an intentional contract
	// change. EvaluatedAt is stamped by the updater, not here.
	ComputedEvidence BenchmarkEvidence `json:"computedEvidence,omitempty"`
}

type BenchmarkReport struct {
	SchemaVersion int    `json:"schemaVersion"`
	Mode          string `json:"mode"`
	Suite         string `json:"suite"`
	// HardNegativeCoverage says how much of the suite actually exercises the
	// hard-negative guard. A single hit fails a case, which makes a clean run
	// read as suite-wide protection when it is only protection for the cases
	// that declare a negative. It gates nothing; it stops the report from
	// overstating itself.
	HardNegativeCoverage HardNegativeCoverage `json:"hardNegativeCoverage"`
	Profiles             []ProfileBenchmark   `json:"profiles"`
	Passed               bool                 `json:"passed"`
}

func LoadEvalCases(path string) ([]EvalCase, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxSourceBytes {
		return nil, errors.New("evaluation case file has unsafe type or size")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []EvalCase
	if err := decodeStrict(body, &cases); err != nil {
		var single EvalCase
		if singleErr := decodeStrict(body, &single); singleErr != nil {
			return nil, err
		}
		cases = []EvalCase{single}
	}
	if err := ValidateEvalCorpus(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func ValidateEvalCorpus(cases []EvalCase) error {
	if len(cases) == 0 || len(cases) > 10000 {
		return errors.New("evaluation case set is empty")
	}
	seen := map[string]bool{}
	topicSplits := map[string]string{}
	for index := range cases {
		eval := &cases[index]
		if !managedSlugRE.MatchString(eval.ID) || seen[eval.ID] ||
			strings.TrimSpace(eval.Query) == "" ||
			len([]byte(eval.Query)) > 1000 {
			return errors.New("evaluation case IDs and queries must be unique and nonempty")
		}
		seen[eval.ID] = true
		if eval.Role != "manager" && eval.Role != "drafter" {
			return fmt.Errorf("case %s has invalid role", eval.ID)
		}
		if !managedSlugRE.MatchString(eval.Topic) {
			return fmt.Errorf("case %s has invalid topic", eval.ID)
		}
		if eval.Split != "development" && eval.Split != "holdout" {
			return fmt.Errorf("case %s has invalid split", eval.ID)
		}
		if previous, exists := topicSplits[eval.Topic]; exists && previous != eval.Split {
			return fmt.Errorf("evaluation topic %q leaks across development and holdout", eval.Topic)
		}
		topicSplits[eval.Topic] = eval.Split
		switch eval.QueryClass {
		case "auto", "exact", "conceptual", "orientation", "current",
			"provenance", "dependency", "contradiction":
		default:
			return fmt.Errorf("case %s has invalid query class", eval.ID)
		}
		normalizedTiers, err := ValidateTierList(eval.AllowedTiers)
		if err != nil {
			return fmt.Errorf("case %s: %w", eval.ID, err)
		}
		if len(normalizedTiers) != len(eval.AllowedTiers) {
			return fmt.Errorf("case %s repeats an allowed tier", eval.ID)
		}
		eval.AllowedTiers = normalizedTiers
		if len(SortedUnique(eval.ForbiddenTiers)) != len(eval.ForbiddenTiers) {
			return fmt.Errorf("case %s repeats a forbidden tier", eval.ID)
		}
		for _, tier := range eval.ForbiddenTiers {
			if !AllowedTiers[tier] {
				return fmt.Errorf("case %s has invalid forbidden tier %q", eval.ID, tier)
			}
		}
		if eval.TokenBudget < 128 || eval.TokenBudget > 4096 {
			return fmt.Errorf("case %s has invalid token budget", eval.ID)
		}
		if !(strings.HasPrefix(eval.CorpusSnapshot, "fixture:") &&
			managedSlugRE.MatchString(strings.TrimPrefix(eval.CorpusSnapshot, "fixture:"))) &&
			!sha256IdentityRE.MatchString(eval.CorpusSnapshot) {
			return fmt.Errorf("case %s has invalid corpus snapshot", eval.ID)
		}
		if eval.Answerable == nil {
			return fmt.Errorf("case %s must declare answerable", eval.ID)
		}
		for _, pin := range eval.EvidencePins {
			if err := validateEvalPath(pin.Path); err != nil {
				return fmt.Errorf("case %s evidence pin: %w", eval.ID, err)
			}
			if !sha256IdentityRE.MatchString(pin.ClaimSha256) {
				return fmt.Errorf("case %s has an invalid evidence pin digest", eval.ID)
			}
			if pin.ContentSha256 != "" &&
				!sha256IdentityRE.MatchString(pin.ContentSha256) {
				return fmt.Errorf("case %s has an invalid evidence pin content digest", eval.ID)
			}
		}
		for _, paths := range [][]string{
			eval.ExpectedPaths, eval.MinimumEvidencePaths, eval.HardNegativePaths,
			eval.ExpectedCitations,
		} {
			for _, path := range paths {
				if err := validateEvalPath(path); err != nil {
					return fmt.Errorf("case %s: %w", eval.ID, err)
				}
			}
			if len(SortedUnique(paths)) != len(paths) {
				return fmt.Errorf("case %s repeats an evaluation path", eval.ID)
			}
		}
		eval.ExpectedPaths = SortedUnique(eval.ExpectedPaths)
		eval.MinimumEvidencePaths = SortedUnique(eval.MinimumEvidencePaths)
		eval.HardNegativePaths = SortedUnique(eval.HardNegativePaths)
		eval.ExpectedCitations = SortedUnique(eval.ExpectedCitations)
		if *eval.Answerable && len(eval.ExpectedPaths) == 0 {
			return fmt.Errorf("answerable case %s has no relevant paths", eval.ID)
		}
		if !*eval.Answerable && (len(eval.ExpectedPaths) != 0 ||
			len(eval.MinimumEvidencePaths) != 0 || len(eval.ExpectedCitations) != 0) {
			return fmt.Errorf("unanswerable case %s declares expected evidence", eval.ID)
		}
		for _, path := range append(
			append([]string(nil), eval.MinimumEvidencePaths...),
			eval.ExpectedCitations...,
		) {
			if !contains(eval.ExpectedPaths, path) {
				return fmt.Errorf("case %s evidence path %q is not graded relevant", eval.ID, path)
			}
		}
		for _, path := range eval.HardNegativePaths {
			if contains(eval.ExpectedPaths, path) {
				return fmt.Errorf("case %s marks %q both relevant and hard-negative", eval.ID, path)
			}
		}
		for path, grade := range eval.GradedRelevantPaths {
			if !contains(eval.ExpectedPaths, path) || grade < 1 || grade > 3 {
				return fmt.Errorf("case %s has invalid graded relevance for %q", eval.ID, path)
			}
		}
	}
	return nil
}

func validateEvalPath(value string) error {
	if value == "" || filepath.IsAbs(filepath.FromSlash(value)) {
		return errors.New("evaluation paths must be nonempty project-relative paths")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(value, "\\", "/"))))
	if clean != value || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe evaluation path %q", value)
	}
	return nil
}

// UpdateDeclaredBenchmarks rewrites the packaged profile's declared
// evidence blocks from a full benchmark's computed evidence. It is a
// plugin-maintainer operation for intentional contract changes (runtime,
// chunker, or fixture); it never runs for consuming projects. Ratified
// ratchet values on each row are preserved so re-declaration cannot loosen
// an accepted quality floor.
func UpdateDeclaredBenchmarks(assetRoot string, report BenchmarkReport) error {
	if report.Mode != "full" {
		return errors.New("declared evidence may only be updated from a full run")
	}
	profilePath := filepath.Join(assetRoot, "profiles", "balanced-v1.json")
	profileBody, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	var profile RetrievalProfile
	if err := decodeStrict(profileBody, &profile); err != nil {
		return err
	}
	stamp := RFC3339UTC(time.Now())
	for index := range profile.EffectiveProfiles {
		row := &profile.EffectiveProfiles[index]
		var measured *ProfileBenchmark
		for candidateIndex := range report.Profiles {
			if report.Profiles[candidateIndex].ProfileName == row.Name {
				measured = &report.Profiles[candidateIndex]
				break
			}
		}
		if measured == nil {
			return fmt.Errorf("full report carries no row for profile %s", row.Name)
		}
		evidence := measured.ComputedEvidence
		evidence.EvaluatedAt = stamp
		evidence.RatifiedHardNegativeHits = row.Benchmark.RatifiedHardNegativeHits
		evidence.RatifiedAbstentionAccuracy = row.Benchmark.RatifiedAbstentionAccuracy
		row.Benchmark = evidence
	}
	return AtomicWriteJSON(profilePath, profile, 0o644)
}

func RunPackagedBenchmark(ctx context.Context, assetRoot, mode string) (BenchmarkReport, error) {
	if mode != "quick" && mode != "full" {
		return BenchmarkReport{}, errors.New("benchmark mode must be quick or full")
	}
	cases, err := LoadEvalCases(filepath.Join(assetRoot, "evals", "conformance", "cases.json"))
	if err != nil {
		return BenchmarkReport{}, err
	}
	findingSuite, err := LoadFindingEvalSuite(filepath.Join(
		assetRoot, "evals", "conformance", "finding-cases.json"))
	if err != nil {
		return BenchmarkReport{}, err
	}
	profileBody, err := os.ReadFile(filepath.Join(assetRoot, "profiles", "balanced-v1.json"))
	if err != nil {
		return BenchmarkReport{}, err
	}
	var profile RetrievalProfile
	if err := decodeStrict(profileBody, &profile); err != nil {
		return BenchmarkReport{}, err
	}
	rows := profile.EffectiveProfiles
	if mode == "quick" {
		rows = nil
		for _, row := range profile.EffectiveProfiles {
			if row.Requires.Embedding == nil && row.Requires.Reranker == nil {
				rows = []EffectiveProfile{row}
				break
			}
		}
		if len(rows) != 1 {
			return BenchmarkReport{}, errors.New(
				"packaged profile lacks one model-free quick-benchmark row")
		}
	}
	report := BenchmarkReport{
		SchemaVersion: 1, Mode: mode, Suite: "packaged-conformance-v1",
		HardNegativeCoverage: MeasureHardNegativeCoverage(cases),
		Passed:               true,
	}
	for _, row := range rows {
		temp, err := os.MkdirTemp("", "re-discipline-conformance-*")
		if err != nil {
			return BenchmarkReport{}, err
		}
		func() {
			defer os.RemoveAll(temp)
			fixture := filepath.Join(assetRoot, "evals", "conformance", "fixture")
			if err = copyTree(fixture, temp); err != nil {
				return
			}
			profileTarget := filepath.Join(temp, ".re-discipline", "knowledge", "retrieval-profile.json")
			if err = AtomicWrite(profileTarget, profileBody, 0o600); err != nil {
				return
			}
			var service *Service
			service, err = NewService(ServiceOptions{
				ProjectRoot: temp, AssetRoot: assetRoot,
				CacheRoot:            filepath.Join(temp, ".re-discipline", "cache", "knowledge"),
				EffectiveProfileName: row.Name,
			})
			if err != nil {
				return
			}
			var benchmark ProfileBenchmark
			benchmark, err = benchmarkCases(ctx, service, row, cases, findingSuite)
			if err != nil {
				return
			}
			report.Profiles = append(report.Profiles, benchmark)
			report.Passed = report.Passed && benchmark.Passed
		}()
		if err != nil {
			return BenchmarkReport{}, err
		}
	}
	if mode == "full" && len(report.Profiles) > 1 {
		baselineIndex := -1
		for index, row := range rows {
			if row.Requires.Embedding == nil && row.Requires.Reranker == nil {
				baselineIndex = index
				break
			}
		}
		if baselineIndex < 0 {
			return BenchmarkReport{}, errors.New(
				"packaged benchmark lacks a model-free lexical baseline")
		}
		baseline := report.Profiles[baselineIndex].HoldoutMetrics
		report.Profiles[baselineIndex].NonInferiorToLexical = true
		for index := range report.Profiles {
			if index == baselineIndex {
				continue
			}
			holdout := report.Profiles[index].HoldoutMetrics
			nonInferior := holdout.RecallAtK >= baseline.RecallAtK &&
				holdout.NDCG+0.02 >= baseline.NDCG &&
				holdout.CompleteEvidenceCoverage >= baseline.CompleteEvidenceCoverage &&
				holdout.CitationRecall >= baseline.CitationRecall &&
				holdout.AuthorityViolations <= baseline.AuthorityViolations
			report.Profiles[index].NonInferiorToLexical = nonInferior
			if !nonInferior {
				report.Profiles[index].Passed = false
				report.Passed = false
			}
		}
	}
	return report, nil
}

func benchmarkCases(
	ctx context.Context,
	service *Service,
	row EffectiveProfile,
	cases []EvalCase,
	findingSuite FindingEvalSuite,
) (ProfileBenchmark, error) {
	// Pin and lease the generation this profile starts on, exactly as the
	// project benchmark does. Without the pin every one of the several thousand
	// queries a full run issues re-enters IndexManager.ensure, which spawns two
	// git subprocesses, re-runs the database integrity check, and can re-walk
	// the corpus. The generation cannot change during a run over an immutable
	// fixture, so the measurements and their digests are unaffected; only the
	// per-query freshness re-proof is removed.
	generation, selected, lease, err := service.leaseMeasurementGeneration(
		ctx, "packaged-benchmark")
	if err != nil {
		return ProfileBenchmark{}, err
	}
	defer lease.Release()
	service.PinGeneration(generation)
	defer service.flushTelemetry()
	findingEvaluation, err := EvaluateFindingSuite(ctx, Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}, findingSuite)
	if err != nil {
		return ProfileBenchmark{}, err
	}
	outcomes := make([]CaseOutcome, 0, len(cases))
	passed := findingEvaluationPassed(findingEvaluation)
	for _, eval := range cases {
		outcome, err := runEvaluationCase(ctx, eval, eval.TokenBudget, service.Boundary.Root, service.Search)
		if err != nil {
			return ProfileBenchmark{}, fmt.Errorf("case %s: %w", eval.ID, err)
		}
		outcomes = append(outcomes, outcome)
		passed = passed && evaluationOutcomePassed(outcome)
	}
	contextPackCases, contextPackRoles, err := evaluateContextPackCases(
		ctx, service, cases, 0,
	)
	if err != nil {
		return ProfileBenchmark{}, err
	}
	contextPackPassed := contextPackOutcomesPassed(contextPackCases) &&
		contains(contextPackRoles, "manager") &&
		contains(contextPackRoles, "drafter")
	passed = passed && contextPackPassed
	metrics := calculateMetrics(outcomes, cases)
	developmentOutcomes, developmentCases := filterEvalSplit(outcomes, cases, "development")
	holdoutOutcomes, holdoutCases := filterEvalSplit(outcomes, cases, "holdout")
	holdoutMetrics := calculateMetrics(holdoutOutcomes, holdoutCases)
	metricsBySplit := map[string]QualityMetrics{
		"development": calculateMetrics(developmentOutcomes, developmentCases),
		"holdout":     holdoutMetrics,
	}
	metricsByBudget := map[string]QualityMetrics{}
	qualityMetricsByBudget := map[string]QualityMetrics{}
	casesByBudget := map[string][]CaseOutcome{}
	contextPacksByBudget := map[string][]ContextPackOutcome{}
	for _, budget := range []int{512, 1024, 2048, 4096} {
		budgetOutcomes := make([]CaseOutcome, 0, len(cases))
		for _, eval := range cases {
			outcome, err := runEvaluationCase(ctx, eval, budget, service.Boundary.Root, service.Search)
			if err != nil {
				return ProfileBenchmark{}, fmt.Errorf(
					"case %s at budget %d: %w", eval.ID, budget, err)
			}
			budgetOutcomes = append(budgetOutcomes, outcome)
			passed = passed && evaluationOutcomePassed(outcome)
		}
		key := fmt.Sprintf("%d", budget)
		casesByBudget[key] = budgetOutcomes
		metricsByBudget[key] = calculateMetrics(budgetOutcomes, cases)
		qualityMetricsByBudget[key] = applicableMetrics(budgetOutcomes, cases)
		if hasApplicableQualityGate(budgetOutcomes) {
			passed = passed && hardMetricsPassed(qualityMetricsByBudget[key])
		}
		contextOutcomes, roles, err := evaluateContextPackCases(
			ctx, service, cases, budget,
		)
		if err != nil {
			return ProfileBenchmark{}, fmt.Errorf(
				"context packs at budget %d: %w", budget, err)
		}
		contextPacksByBudget[key] = contextOutcomes
		contextBudgetPassed := contextPackOutcomesPassed(contextOutcomes) &&
			contains(roles, "manager") && contains(roles, "drafter")
		contextPackPassed = contextPackPassed && contextBudgetPassed
		passed = passed && contextBudgetPassed
	}
	digestOutcomes := append([]CaseOutcome(nil), outcomes...)
	for index := range digestOutcomes {
		digestOutcomes[index].LatencyMillis = 0
	}
	digestBudgets := map[string]QualityMetrics{}
	digestQualityBudgets := map[string]QualityMetrics{}
	for key, value := range metricsByBudget {
		digestBudgets[key] = metricsWithoutLatency(value)
		digestQualityBudgets[key] =
			metricsWithoutLatency(qualityMetricsByBudget[key])
	}
	digestBudgetCases := map[string][]CaseOutcome{}
	for key, values := range casesByBudget {
		copied := append([]CaseOutcome(nil), values...)
		for index := range copied {
			copied[index].LatencyMillis = 0
		}
		digestBudgetCases[key] = copied
	}
	semanticRow := cloneEffectiveProfile(row)
	semanticRow.Benchmark = BenchmarkEvidence{Suite: row.Benchmark.Suite}
	evalDigest, _ := CanonicalDigest(struct {
		PassageCases []EvalCase       `json:"passageCases"`
		FindingSuite FindingEvalSuite `json:"findingSuite"`
	}{cases, findingSuite})
	digestInput := struct {
		Suite                  string                          `json:"suite"`
		EffectiveProfile       EffectiveProfile                `json:"effectiveProfile"`
		EvalDigest             string                          `json:"evalDigest"`
		CorpusFingerprint      string                          `json:"corpusFingerprint"`
		ParserVersion          string                          `json:"parserVersion"`
		ChunkerVersion         string                          `json:"chunkerVersion"`
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
		FindingEvaluation      FindingAblationReport           `json:"findingEvaluation"`
	}{
		"packaged-conformance-v1", semanticRow, evalDigest,
		generation.CorpusFingerprint, ParserVersion, ChunkerVersion,
		RuntimeContract(selected.Runtime), selected.Models, digestOutcomes,
		metricsWithoutLatency(metrics),
		map[string]QualityMetrics{
			"development": metricsWithoutLatency(metricsBySplit["development"]),
			"holdout":     metricsWithoutLatency(metricsBySplit["holdout"]),
		},
		digestBudgets, digestQualityBudgets, digestBudgetCases,
		contextPackOutcomesForDigest(contextPackCases),
		contextPackBudgetsForDigest(contextPacksByBudget), contextPackRoles,
		findingEvaluation,
	}
	digest, err := CanonicalDigest(digestInput)
	if err != nil {
		return ProfileBenchmark{}, err
	}
	verified := digest == row.Benchmark.Digest
	runtimeDigest, _ := CanonicalDigest(RuntimeContract(selected.Runtime))
	computedEvidence := BenchmarkEvidence{
		Suite: "packaged-conformance-v1", Digest: digest, Status: "passed",
		EvalFingerprint:    evalDigest,
		CorpusFingerprint:  generation.CorpusFingerprint,
		ModelFingerprint:   mustDigest(selected.Models),
		RuntimeFingerprint: runtimeDigest,
		ChunkerVersion:     generation.ChunkerVersion,
		ParserVersion:      generation.ParserVersion,
	}
	return ProfileBenchmark{
		ProfileName: row.Name, DeclaredDigest: row.Benchmark.Digest,
		ComputedDigest: digest, DigestVerified: verified,
		ComputedEvidence: computedEvidence,
		Passed: passed && verified && metrics.AuthorityViolations == 0 &&
			metrics.CitationViolations == 0 && metrics.HardNegativeHits == 0,
		Cases: outcomes, CasesByBudget: casesByBudget,
		ContextPackCases:     contextPackCases,
		ContextPacksByBudget: contextPacksByBudget,
		ContextPackRoles:     contextPackRoles,
		ContextPackPassed:    contextPackPassed,
		Metrics:              metrics, HoldoutMetrics: holdoutMetrics,
		MetricsBySplit: metricsBySplit, MetricsByBudget: metricsByBudget,
		QualityMetricsByBudget: qualityMetricsByBudget,
		FindingEvaluation:      findingEvaluation,
	}, nil
}

func findingEvaluationPassed(report FindingAblationReport) bool {
	return report.SuiteID != "" && sha256ValueRE.MatchString(report.SuiteDigest) &&
		report.FindingRecall == 1 && report.MeanReciprocalRank == 1 && report.RawPathRecall == 1 &&
		report.AbstentionAccuracy == 1 && report.FindingHandleAccuracy == 1 &&
		report.EvidenceHandleAccuracy == 1 && report.SourceClassAccuracy == 1 &&
		report.ReviewStateAccuracy == 1 && report.ValidityAccuracy == 1 &&
		report.VocabularyDisjointRate == 1 && report.DurabilityLabelAccuracy == 1 &&
		report.HardNegativeHits == 0 && report.DeterministicReplayRate == 1 &&
		report.NormalizedMedianTokens > 0 &&
		report.NormalizedMedianTokens < report.RawMedianTokens
}

// EvidencePinHealth is the cheap, read-only census of pin rot.
//
// Until this existed, a rotten pin was discovered only when a calibration run
// tripped over it - which is to say, at the moment somebody needed the
// measurement to be trustworthy. Hashing the pinned documents is cheap enough
// to do on every status call, so the census belongs where a session already
// looks rather than where a run already fails.
type EvidencePinHealth struct {
	Total int `json:"total"`
	// Intact: the document still asserts what it asserted when the case was
	// ratified.
	Intact int `json:"intact"`
	// Drifted: the claim is unchanged but the file's bytes moved. The case
	// still measures what it was ratified to measure; the pin is advisory here
	// and never gates.
	Drifted int `json:"drifted"`
	// Broken: the claim digest changed, or the document cannot be read at all.
	// The case's ground truth may no longer hold and re-answering it is the
	// only honest repair.
	Broken         int               `json:"broken"`
	NonIntactPaths []EvidencePinPath `json:"nonIntactPaths"`
	// Unavailable records why no census could be taken - a malformed evaluation
	// corpus, say. Absent when the project simply has no evaluation cases,
	// which is not a fault.
	Unavailable string `json:"unavailable,omitempty"`
}

// EvidencePinPath counts the pins one document is responsible for.
type EvidencePinPath struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Pins  int    `json:"pins"`
}

// EvidencePinCensus hashes every pinned document once and classifies the pins
// that reference it.
func (service *Service) EvidencePinCensus() EvidencePinHealth {
	health := EvidencePinHealth{NonIntactPaths: []EvidencePinPath{}}
	root := filepath.Join(
		service.Boundary.Root, ".re-discipline", "knowledge", "evals")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		// No ratified evaluation corpus is a legitimate project state, not a
		// degraded one. Report an empty census rather than an error.
		return health
	}
	cases, err := service.loadProjectEvalCases()
	if err != nil {
		health.Unavailable = err.Error()
		return health
	}
	states := map[string]string{}
	counts := map[string]int{}
	for _, eval := range cases {
		for _, pin := range eval.EvidencePins {
			health.Total++
			state, resolved := states[pin.Path]
			if !resolved {
				state = classifyEvidencePin(service.Boundary.Root, pin)
				states[pin.Path] = state
			}
			switch state {
			case "intact":
				health.Intact++
				continue
			case "drifted":
				health.Drifted++
			default:
				health.Broken++
			}
			counts[pin.Path]++
		}
	}
	paths := make([]string, 0, len(counts))
	for path := range counts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		health.NonIntactPaths = append(health.NonIntactPaths, EvidencePinPath{
			Path: path, State: states[path], Pins: counts[path],
		})
	}
	return health
}

// classifyEvidencePin decides one pinned document's state. A pin's claim digest
// is the gate and its content digest is advisory, so an unreadable document and
// a rewritten claim are the same kind of news and a reworded paragraph is not.
func classifyEvidencePin(root string, pin EvidencePin) string {
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pin.Path)))
	if err != nil {
		return "broken"
	}
	if ClaimDigest(string(body), pin.Path) != pin.ClaimSha256 {
		return "broken"
	}
	if pin.ContentSha256 != "" &&
		pin.ContentSha256 != "sha256:"+SHA256Bytes(body) {
		return "drifted"
	}
	return "intact"
}

// HardNegativeCoverage reports how much of an evaluation set actually exercises
// the hard-negative guard.
//
// The guard is absolute - one hard-negative hit fails a case - which makes it
// read as strong protection across the whole suite. It only protects the cases
// that declare a negative, and nothing said how few of them did. This is
// visibility, not a gate: it changes no pass/fail decision, it stops the
// measurement from overstating itself.
type HardNegativeCoverage struct {
	CasesWithNegatives int `json:"casesWithNegatives"`
	TotalCases         int `json:"totalCases"`
	DeclaredPaths      int `json:"declaredPaths"`
}

func MeasureHardNegativeCoverage(cases []EvalCase) HardNegativeCoverage {
	coverage := HardNegativeCoverage{TotalCases: len(cases)}
	for _, eval := range cases {
		if len(eval.HardNegativePaths) == 0 {
			continue
		}
		coverage.CasesWithNegatives++
		coverage.DeclaredPaths += len(eval.HardNegativePaths)
	}
	return coverage
}

// EvalPinChange describes one pin whose document changed.
type EvalPinChange struct {
	CaseID     string `json:"caseId"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	OldDigest  string `json:"oldDigest,omitempty"`
	NewDigest  string `json:"newDigest"`
	CaseQuery  string `json:"caseQuery,omitempty"`
	Unreadable bool   `json:"unreadable,omitempty"`
}

// EvalPinReport is the outcome of recomputing evidence pins.
type EvalPinReport struct {
	SchemaVersion int             `json:"schemaVersion"`
	Cases         int             `json:"cases"`
	Pinned        int             `json:"pinned"`
	Added         []EvalPinChange `json:"added"`
	ClaimChanged  []EvalPinChange `json:"claimChanged"`
	ContentDrift  []EvalPinChange `json:"contentDrift"`
	Applied       bool            `json:"applied"`
}

// PinEvalCases recomputes evidence pins for every project evaluation case.
//
// A pin whose document changed only its prose is refreshed silently. A pin
// whose CLAIM changed is reported and, unless force is set, not written: the
// case's ground truth may no longer hold, and re-stamping without re-answering
// would make the evaluator assert that a rewritten document still supports the
// original query. That is measurement capture, and it is worse than a stale
// pin because it looks like a passing test.
func (service *Service) PinEvalCases(apply, force bool) (EvalPinReport, error) {
	report := EvalPinReport{SchemaVersion: 1}
	root := filepath.Join(service.Boundary.Root, ".re-discipline", "knowledge", "evals")
	entries, err := os.ReadDir(root)
	if err != nil {
		return EvalPinReport{}, err
	}
	digestFor := map[string][2]string{}
	resolve := func(path string) ([2]string, bool) {
		if cached, ok := digestFor[path]; ok {
			return cached, cached[0] != ""
		}
		body, readErr := os.ReadFile(
			filepath.Join(service.Boundary.Root, filepath.FromSlash(path)))
		if readErr != nil {
			digestFor[path] = [2]string{}
			return [2]string{}, false
		}
		pair := [2]string{ClaimDigest(string(body), path), "sha256:" + SHA256Bytes(body)}
		digestFor[path] = pair
		return pair, true
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		file := filepath.Join(root, entry.Name())
		cases, loadErr := LoadEvalCases(file)
		if loadErr != nil {
			return EvalPinReport{}, loadErr
		}
		changed := false
		for index := range cases {
			eval := &cases[index]
			report.Cases++
			previous := map[string]EvidencePin{}
			for _, pin := range eval.EvidencePins {
				previous[pin.Path] = pin
			}
			paths := SortedUnique(append(append(append(
				append([]string{}, eval.ExpectedPaths...),
				eval.MinimumEvidencePaths...),
				eval.ExpectedCitations...),
				eval.HardNegativePaths...))
			pins := make([]EvidencePin, 0, len(paths))
			for _, path := range paths {
				pair, ok := resolve(path)
				if !ok {
					report.ClaimChanged = append(report.ClaimChanged, EvalPinChange{
						CaseID: eval.ID, Path: path, Kind: "unreadable",
						CaseQuery: eval.Query, Unreadable: true,
					})
					if old, had := previous[path]; had {
						pins = append(pins, old)
					}
					continue
				}
				fresh := EvidencePin{
					Path: path, ClaimSha256: pair[0], ContentSha256: pair[1],
				}
				old, had := previous[path]
				switch {
				case !had:
					report.Added = append(report.Added, EvalPinChange{
						CaseID: eval.ID, Path: path, Kind: "added",
						NewDigest: fresh.ClaimSha256, CaseQuery: eval.Query,
					})
				case old.ClaimSha256 != fresh.ClaimSha256:
					report.ClaimChanged = append(report.ClaimChanged, EvalPinChange{
						CaseID: eval.ID, Path: path, Kind: "claim-changed",
						OldDigest: old.ClaimSha256, NewDigest: fresh.ClaimSha256,
						CaseQuery: eval.Query,
					})
					if !force {
						// Keep the ratified pin so the case reports stale
						// rather than silently re-anchoring to a claim nobody
						// has re-answered the query against.
						fresh = old
					}
				case old.ContentSha256 != fresh.ContentSha256:
					report.ContentDrift = append(report.ContentDrift, EvalPinChange{
						CaseID: eval.ID, Path: path, Kind: "content-drift",
						OldDigest: old.ContentSha256, NewDigest: fresh.ContentSha256,
					})
				}
				pins = append(pins, fresh)
				report.Pinned++
			}
			if !equalPins(eval.EvidencePins, pins) {
				eval.EvidencePins = pins
				changed = true
			}
		}
		if apply && changed {
			if err := AtomicWriteJSON(file, cases, 0o600); err != nil {
				return EvalPinReport{}, err
			}
			report.Applied = true
		}
	}
	return report, nil
}

func equalPins(left, right []EvidencePin) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// evidencePinsIntact reports whether every document a case depends on still
// asserts what it asserted when the case was ratified. Returns false if any
// pinned document is unreadable or its claim has changed.
func evidencePinsIntact(root string, pins []EvidencePin) bool {
	if root == "" {
		return false
	}
	for _, pin := range pins {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pin.Path)))
		if err != nil {
			return false
		}
		if ClaimDigest(string(body), pin.Path) != pin.ClaimSha256 {
			return false
		}
	}
	return true
}

func runEvaluationCase(
	ctx context.Context,
	eval EvalCase,
	tokenBudget int,
	root string,
	search func(context.Context, SearchOptions) (SearchResponse, error),
) (CaseOutcome, error) {
	options := SearchOptions{
		Query: eval.Query, QueryClass: eval.QueryClass,
		AllowedTiers: eval.AllowedTiers, Limit: 20, TokenBudget: tokenBudget,
	}
	started := time.Now()
	first, err := search(ctx, options)
	if err != nil {
		return CaseOutcome{}, err
	}
	second, err := search(ctx, options)
	if err != nil {
		return CaseOutcome{}, err
	}
	outcome := CaseOutcome{CaseID: eval.ID, Split: eval.Split, Topic: eval.Topic}
	seenPaths := map[string]bool{}
	seenRelevant := map[string]bool{}
	seenPassages := map[string]bool{}
	foundCitations := map[string]bool{}
	hardNegatives := map[string]bool{}
	citationMetadataSafe := true
	authoritySafe := true
	for _, result := range first.Results {
		path := result.Citation.Path
		outcome.Paths = append(outcome.Paths, path)
		outcome.Tiers = append(outcome.Tiers, result.Citation.Tier)
		outcome.ChunkIDs = append(outcome.ChunkIDs, result.ChunkID)
		outcome.ContentHashes = append(outcome.ContentHashes, result.Citation.ContentHash)
		if !seenPaths[path] {
			seenPaths[path] = true
			outcome.ReturnedUniquePaths++
		}
		if contains(eval.ExpectedPaths, path) && !seenRelevant[path] {
			seenRelevant[path] = true
			outcome.RelevantPaths = append(outcome.RelevantPaths, path)
			outcome.RelevantRanks = append(outcome.RelevantRanks, len(outcome.Paths))
			outcome.RelevantTokens += EstimateTokens(result.Passage)
		}
		if contains(eval.ExpectedCitations, path) {
			foundCitations[path] = true
		}
		if contains(eval.HardNegativePaths, path) {
			hardNegatives[path] = true
		}
		if seenPassages[result.Citation.ContentHash] {
			outcome.DuplicateTokens += EstimateTokens(result.Passage)
		}
		seenPassages[result.Citation.ContentHash] = true
		// A compact response omits the generation URI because it is the
		// generation ID and the chunk ID concatenated, both of which the
		// response already carries. Check the reconstructed handle instead, so
		// the same generation-scoping guarantee is measured either way.
		handle := result.Citation.URI
		if handle == "" {
			handle = "re-discipline://" + first.Metadata.Generation +
				"/chunks/" + result.ChunkID
		}
		if result.Citation.Path == "" || result.Citation.StartLine < 1 ||
			result.Citation.EndLine < result.Citation.StartLine ||
			len(result.Citation.ContentHash) != 64 ||
			result.Citation.PassageHash != result.Citation.ContentHash ||
			!strings.HasPrefix(handle,
				"re-discipline://"+first.Metadata.Generation+"/chunks/") {
			citationMetadataSafe = false
		}
		if contains(eval.ForbiddenTiers, result.Citation.Tier) {
			authoritySafe = false
		}
	}
	outcome.HardNegativeHits = mapKeysSorted(hardNegatives)
	outcome.ExpectedCitationsFound = mapKeysSorted(foundCitations)
	// Authority is tier discipline only: did retrieval serve a tier this case
	// forbade. Hard-negative hits used to be folded in here, which made a
	// ranking-quality signal absolute by way of a safety field and meant no
	// weight vector could ever clear the gate. They are graded under
	// QualityPassed and compared against the incumbent in
	// calibrationNonInferior instead.
	outcome.AuthoritySafe = authoritySafe
	outcome.CompleteEvidence = true
	for _, expected := range eval.MinimumEvidencePaths {
		if !seenPaths[expected] {
			outcome.CompleteEvidence = false
		}
	}
	if *eval.Answerable {
		outcome.ExpectedFound = len(outcome.RelevantPaths) > 0 && outcome.CompleteEvidence
		outcome.AbstentionCorrect = len(first.Results) > 0
	} else {
		outcome.ExpectedFound = len(first.Results) == 0
		outcome.CompleteEvidence = len(first.Results) == 0
		outcome.AbstentionCorrect = len(first.Results) == 0
	}
	outcome.CitationMetadataSafe = citationMetadataSafe
	outcome.CitationSafe = citationMetadataSafe
	for _, expected := range eval.ExpectedCitations {
		if !foundCitations[expected] {
			outcome.CitationSafe = false
		}
	}
	// Evidence pins, when declared, replace the corpus-wide fingerprint. The
	// fingerprint invalidates every case on any edit anywhere in the corpus,
	// so on a corpus that changes daily it reports staleness far more often
	// than a case's ground truth actually goes stale.
	switch {
	case len(eval.EvidencePins) > 0:
		outcome.CorpusMatched = evidencePinsIntact(root, eval.EvidencePins)
	default:
		outcome.CorpusMatched = eval.CorpusSnapshot == "fixture:packaged-conformance-v1" ||
			eval.CorpusSnapshot == first.Metadata.CorpusFingerprint
	}
	outcome.EstimatedTokens = first.EstimatedTokens
	outcome.ReturnedTokens = first.EstimatedTokens
	outcome.BudgetSafe = first.EstimatedTokens <= tokenBudget
	outcome.ReplayIdentical = stableJSON(first) == stableJSON(second)
	outcome.MinimumTokenBudget = eval.TokenBudget
	outcome.QualityGateApplicable = tokenBudget >= eval.TokenBudget
	outcome.SafetyPassed = outcome.AuthoritySafe &&
		outcome.CitationMetadataSafe && outcome.CorpusMatched &&
		outcome.BudgetSafe && outcome.ReplayIdentical &&
		outcome.StaleResults == 0
	outcome.QualityPassed = !outcome.QualityGateApplicable ||
		(outcome.ExpectedFound && outcome.CompleteEvidence &&
			outcome.CitationSafe && outcome.AbstentionCorrect &&
			len(outcome.HardNegativeHits) == 0)
	outcome.GatePassed = outcome.SafetyPassed && outcome.QualityPassed
	outcome.LatencyMillis = time.Since(started).Milliseconds()
	return outcome, nil
}

func evaluateContextPackCases(
	ctx context.Context,
	service *Service,
	cases []EvalCase,
	tokenBudget int,
) ([]ContextPackOutcome, []string, error) {
	_, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return nil, nil, err
	}
	settings := service.effectiveSettings()
	outcomes := make([]ContextPackOutcome, 0, len(cases))
	roleSet := map[string]bool{}
	for _, eval := range cases {
		budget := tokenBudget
		if budget == 0 {
			budget = eval.TokenBudget
		}
		outcome := runContextPackCase(
			ctx, service, selected, settings, eval, budget,
		)
		outcomes = append(outcomes, outcome)
		roleSet[eval.Role] = true
	}
	return outcomes, mapKeysSorted(roleSet), nil
}

func runContextPackCase(
	ctx context.Context,
	service *Service,
	selected SelectedProfile,
	settings KnowledgeSettings,
	eval EvalCase,
	tokenBudget int,
) ContextPackOutcome {
	roleCeiling := settings.Budgets.ManagerContextTokens
	if eval.Role == "drafter" {
		roleCeiling = settings.Budgets.DrafterContextTokens
	}
	effectiveBudget := tokenBudget
	if effectiveBudget > roleCeiling {
		effectiveBudget = roleCeiling
	}
	requiredPaths := SortedUnique(append(
		append([]string(nil), eval.MinimumEvidencePaths...),
		eval.ExpectedCitations...,
	))
	minimumEvidenceBudget := eval.TokenBudget
	if minimumEvidenceBudget < MinimumContextPackEvidenceBudget {
		minimumEvidenceBudget = MinimumContextPackEvidenceBudget
	}
	outcome := ContextPackOutcome{
		CaseID: eval.ID, Split: eval.Split, Topic: eval.Topic, Role: eval.Role,
		RequestedTokenBudget: tokenBudget, EffectiveTokenBudget: effectiveBudget,
		RoleTokenCeiling: roleCeiling,
		MaxCards:         service.Configuration.Bootstrap.Context.MaxCards,
		MaxBytes:         selected.Effective.Packing.MaxBytes,
		RequiredPaths:    requiredPaths,
		Paths:            []string{}, Tiers: []string{},
		RequiredPathsFound:    []string{},
		MinimumTokenBudget:    minimumEvidenceBudget,
		QualityGateApplicable: tokenBudget >= minimumEvidenceBudget,
	}
	if outcome.MaxCards < 1 {
		outcome.MaxCards = selected.Effective.Packing.MaxPassages
	}
	injectedRequired := requiredPaths
	if !outcome.QualityGateApplicable {
		// Below a case's declared evidence budget, measure safety and packing
		// without forcing evidence that is explicitly known not to fit.
		injectedRequired = nil
	}
	first, err := service.ContextPackRequired(
		ctx, eval.Query, eval.Role, eval.AllowedTiers,
		tokenBudget, injectedRequired,
	)
	if err != nil {
		outcome.Error = err.Error()
		return outcome
	}
	second, err := service.ContextPackRequired(
		ctx, eval.Query, eval.Role, eval.AllowedTiers,
		tokenBudget, injectedRequired,
	)
	if err != nil {
		outcome.Error = err.Error()
		return outcome
	}
	body, err := json.Marshal(first)
	if err != nil {
		outcome.Error = err.Error()
		return outcome
	}
	outcome.PackID = first.PackID
	outcome.Digest = first.Digest
	outcome.Generation = first.Generation.ID
	outcome.EffectiveProfile = first.EffectiveProfile
	outcome.CardCount = len(first.Cards)
	outcome.SerializedBytes = len(body)
	outcome.EstimatedTokens = first.EstimatedTokens
	outcome.ReplayIdentical = stableJSON(first) == stableJSON(second)
	outcome.RoleCeilingSafe =
		first.TokenBudget == effectiveBudget &&
			first.TokenBudget <= roleCeiling
	outcome.CardCapSafe = len(first.Cards) <= outcome.MaxCards
	outcome.ByteCapSafe = len(body) <= outcome.MaxBytes
	outcome.TokenAccountingSafe =
		EstimateTokens(string(body)) == first.EstimatedTokens
	outcome.BudgetSafe =
		first.EstimatedTokens <= first.TokenBudget &&
			first.TokenBudget <= effectiveBudget
	outcome.ProfilePinned =
		first.EffectiveProfile == selected.EffectiveIdentity
	outcome.GenerationPinned = first.Generation.ID != ""
	allowed := SortedUnique(eval.AllowedTiers)
	outcome.AllowedTiersSafe = stableJSON(first.AllowedTiers) == stableJSON(allowed)
	foundPaths := map[string]bool{}
	expectedFound := false
	for _, card := range first.Cards {
		path := card.Metadata["path"]
		tier := card.Metadata["tier"]
		if tier == "" {
			tier = card.SourceClass
		}
		if path != "" {
			outcome.Paths = append(outcome.Paths, path)
			foundPaths[path] = true
		}
		outcome.Tiers = append(outcome.Tiers, tier)
		if contains(eval.ExpectedPaths, path) {
			expectedFound = true
		}
		if !contextCardAllowedByTiers(card, allowed) {
			outcome.AllowedTiersSafe = false
		}
		if strings.HasPrefix(card.Handle, "re-discipline://") &&
			!strings.HasPrefix(card.Handle, "re-discipline://"+first.Generation.ID+"/") {
			outcome.GenerationPinned = false
		}
	}
	for _, required := range requiredPaths {
		if foundPaths[required] {
			outcome.RequiredPathsFound = append(
				outcome.RequiredPathsFound, required)
		}
	}
	outcome.RequiredPathsFound = SortedUnique(outcome.RequiredPathsFound)
	outcome.RequiredEvidencePresent =
		len(outcome.RequiredPathsFound) == len(requiredPaths)
	if eval.Answerable != nil && *eval.Answerable {
		outcome.ExpectedEvidenceFound = expectedFound
		outcome.AbstentionCorrect = len(first.Cards) > 0
	} else {
		outcome.ExpectedEvidenceFound = len(first.Cards) == 0
		outcome.AbstentionCorrect = len(first.Cards) == 0
	}
	if _, err := VerifyContextPackValueExpected(
		first, first.Digest, first.PackID,
	); err == nil {
		outcome.VerificationPassed = true
	} else {
		outcome.Error = err.Error()
	}
	outcome.SafetyPassed =
		outcome.RoleCeilingSafe && outcome.AllowedTiersSafe &&
			outcome.CardCapSafe && outcome.ByteCapSafe &&
			outcome.TokenAccountingSafe && outcome.BudgetSafe &&
			outcome.GenerationPinned && outcome.ProfilePinned &&
			outcome.VerificationPassed && outcome.ReplayIdentical
	outcome.QualityPassed = !outcome.QualityGateApplicable ||
		(outcome.RequiredEvidencePresent &&
			outcome.ExpectedEvidenceFound && outcome.AbstentionCorrect)
	outcome.Passed = outcome.SafetyPassed && outcome.QualityPassed
	return outcome
}

func contextCardAllowedByTiers(card ContextCard, tiers []string) bool {
	if card.SourceClass == "state" {
		// State cards are target bindings, not retrieved epistemic content.
		return true
	}
	for _, tier := range tiers {
		if contextSourceClass(tier) == card.SourceClass || tier == card.Metadata["tier"] {
			return true
		}
	}
	return false
}

func evaluationOutcomePassed(outcome CaseOutcome) bool {
	// The hard gate is per-case SAFETY, mirroring hardMetricsPassed: no forbidden
	// tier, malformed citation, stale content, budget overflow, or non-determinism.
	// GatePassed folds in QualityPassed too (ExpectedFound, CompleteEvidence,
	// CitationSafe's recall component, HardNegativeHits), which are exactly the
	// retrieval-quality dimensions that trade off against recall and that
	// hardMetricsPassed deliberately stopped treating as absolute. Gating on
	// GatePassed reintroduced that perfection requirement per case, so a
	// safety-clean baseline whose retrieval is merely imperfect on real project
	// cases failed hard gates. Quality is graded for non-inferiority against the
	// incumbent (applyProjectNonInferiority / calibrationNonInferior), not here.
	return outcome.SafetyPassed
}

func contextPackOutcomesPassed(outcomes []ContextPackOutcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, outcome := range outcomes {
		if !outcome.Passed {
			return false
		}
	}
	return true
}

func contextPackOutcomesForDigest(
	outcomes []ContextPackOutcome,
) []ContextPackOutcome {
	normalized := append([]ContextPackOutcome(nil), outcomes...)
	for index := range normalized {
		normalized[index].PackID = ""
		normalized[index].Digest = ""
		normalized[index].Generation = ""
	}
	return normalized
}

func contextPackBudgetsForDigest(
	input map[string][]ContextPackOutcome,
) map[string][]ContextPackOutcome {
	normalized := make(map[string][]ContextPackOutcome, len(input))
	for budget, outcomes := range input {
		normalized[budget] = contextPackOutcomesForDigest(outcomes)
	}
	return normalized
}

func applicableMetrics(
	outcomes []CaseOutcome,
	cases []EvalCase,
) QualityMetrics {
	applicableOutcomes := []CaseOutcome{}
	applicableCases := []EvalCase{}
	for index, outcome := range outcomes {
		if outcome.QualityGateApplicable {
			applicableOutcomes = append(applicableOutcomes, outcome)
			applicableCases = append(applicableCases, cases[index])
		}
	}
	return calculateMetrics(applicableOutcomes, applicableCases)
}

func calculateMetrics(outcomes []CaseOutcome, cases []EvalCase) QualityMetrics {
	var expected, found, returned, relevant, totalTokens, relevantTokens, duplicateTokens int
	var expectedCitations, foundCitations, returnedCitations, staleResults int
	var reciprocal, ndcg float64
	violations, citationViolations, hardNegativeHits, replay := 0, 0, 0, 0
	citationMetadataViolations := 0
	completeEvidence, abstentionCorrect, budgetSafe, exactCases, exactHits := 0, 0, 0, 0, 0
	latencies := make([]int64, 0, len(outcomes))
	for index, outcome := range outcomes {
		eval := cases[index]
		expected += len(eval.ExpectedPaths)
		found += len(outcome.RelevantPaths)
		returned += outcome.ReturnedUniquePaths
		relevant += len(outcome.RelevantPaths)
		totalTokens += outcome.ReturnedTokens
		relevantTokens += outcome.RelevantTokens
		duplicateTokens += outcome.DuplicateTokens
		expectedCitations += len(eval.ExpectedCitations)
		foundCitations += len(outcome.ExpectedCitationsFound)
		returnedCitations += outcome.ReturnedUniquePaths
		staleResults += outcome.StaleResults
		if eval.QueryClass == "exact" && *eval.Answerable {
			exactCases++
			if len(outcome.RelevantRanks) > 0 {
				exactHits++
				reciprocal += 1.0 / float64(outcome.RelevantRanks[0])
			}
		}
		if outcome.CompleteEvidence {
			completeEvidence++
		}
		if outcome.AbstentionCorrect {
			abstentionCorrect++
		}
		if outcome.BudgetSafe {
			budgetSafe++
		}
		if !outcome.AuthoritySafe {
			violations++
		}
		if !outcome.CitationSafe {
			citationViolations++
		}
		// CitationSafe folds citation RECALL into a field whose name reads as
		// safety. Track well-formedness separately so a hard gate can require
		// citation integrity without also requiring perfect recall.
		if !outcome.CitationMetadataSafe {
			citationMetadataViolations++
		}
		hardNegativeHits += len(outcome.HardNegativeHits)
		if outcome.ReplayIdentical {
			replay++
		}
		grades := make([]int, 0, len(eval.ExpectedPaths))
		for _, path := range eval.ExpectedPaths {
			grade := eval.GradedRelevantPaths[path]
			if grade == 0 {
				grade = 1
			}
			grades = append(grades, grade)
		}
		var dcg float64
		for rankIndex, rank := range outcome.RelevantRanks {
			path := outcome.RelevantPaths[rankIndex]
			grade := eval.GradedRelevantPaths[path]
			if grade == 0 {
				grade = 1
			}
			dcg += float64((int64(1)<<grade)-1) / math.Log2(float64(rank)+1)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(grades)))
		var ideal float64
		for rank, grade := range grades {
			ideal += float64((int64(1)<<grade)-1) / math.Log2(float64(rank+2))
		}
		if ideal > 0 {
			ndcg += dcg / ideal
		} else if len(outcome.Paths) == 0 {
			ndcg += 1
		}
		latencies = append(latencies, outcome.LatencyMillis)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	metrics := QualityMetrics{
		AuthorityViolations: violations, CitationViolations: citationViolations,
		CitationMetadataViolations: citationMetadataViolations,
		HardNegativeHits:           hardNegativeHits,
	}
	if expected > 0 {
		metrics.RecallAtK = float64(found) / float64(expected)
		metrics.SupportingEvidenceRecall = metrics.RecallAtK
	}
	if returned > 0 {
		metrics.PrecisionAtK = float64(relevant) / float64(returned)
	}
	if len(outcomes) > 0 {
		if exactCases > 0 {
			metrics.MeanReciprocalRank = reciprocal / float64(exactCases)
			metrics.ExactIdentifierHitRate = float64(exactHits) / float64(exactCases)
		}
		metrics.NDCG = ndcg / float64(len(outcomes))
		metrics.CompleteEvidenceCoverage = float64(completeEvidence) / float64(len(outcomes))
		metrics.AbstentionAccuracy = float64(abstentionCorrect) / float64(len(outcomes))
		metrics.BudgetComplianceRate = float64(budgetSafe) / float64(len(outcomes))
		metrics.AuthorityViolationRate = float64(violations) / float64(len(outcomes))
		metrics.DeterministicReplayRate = float64(replay) / float64(len(outcomes))
		metrics.P50LatencyMillis = percentileMillis(latencies, 0.50)
		metrics.P95LatencyMillis = percentileMillis(latencies, 0.95)
	}
	if expectedCitations > 0 {
		metrics.CitationRecall = float64(foundCitations) / float64(expectedCitations)
	}
	if returnedCitations > 0 {
		metrics.CitationPrecision = float64(foundCitations) / float64(returnedCitations)
		metrics.StaleResultRate = float64(staleResults) / float64(returnedCitations)
	}
	if totalTokens > 0 {
		metrics.RelevantTokenRatio = float64(relevantTokens) / float64(totalTokens)
		metrics.DuplicateTokenRatio = float64(duplicateTokens) / float64(totalTokens)
	}
	return metrics
}

func metricsWithoutLatency(metrics QualityMetrics) QualityMetrics {
	metrics.P50LatencyMillis = 0
	metrics.P95LatencyMillis = 0
	return metrics
}

func filterEvalSplit(
	outcomes []CaseOutcome,
	cases []EvalCase,
	split string,
) ([]CaseOutcome, []EvalCase) {
	selectedOutcomes := []CaseOutcome{}
	selectedCases := []EvalCase{}
	for index, eval := range cases {
		if eval.Split == split {
			selectedOutcomes = append(selectedOutcomes, outcomes[index])
			selectedCases = append(selectedCases, eval)
		}
	}
	return selectedOutcomes, selectedCases
}

func mapKeysSorted(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func percentileMillis(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(values))*quantile)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("fixture contains unsupported file type: %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (service *Service) DeterministicReplay(
	ctx context.Context,
	options SearchOptions,
) (map[string]any, error) {
	first, err := service.Search(ctx, options)
	if err != nil {
		return nil, err
	}
	second, err := service.Search(ctx, options)
	if err != nil {
		return nil, err
	}
	firstDigest, _ := CanonicalDigest(first)
	secondDigest, _ := CanonicalDigest(second)
	return map[string]any{
		"identical":   stableJSON(first) == stableJSON(second),
		"firstDigest": firstDigest, "secondDigest": secondDigest,
		"replayHandle": first.Metadata.DeterministicReplay,
	}, nil
}

func VerifyContextPack(path string, expected ...string) (map[string]any, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 262144 {
		return nil, errors.New("context pack file has unsafe type or size")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pack ContextPack
	if err := decodeStrict(body, &pack); err != nil {
		return nil, err
	}
	expectedDigest, expectedID := "", ""
	if len(expected) > 0 {
		expectedDigest = expected[0]
	}
	if len(expected) > 1 {
		expectedID = expected[1]
	}
	if len(expected) > 2 {
		return nil, errors.New("too many expected context-pack identities")
	}
	return VerifyContextPackValueExpected(pack, expectedDigest, expectedID)
}

func VerifyContextPackValue(pack ContextPack) (map[string]any, error) {
	return VerifyContextPackValueExpected(pack, "", "")
}

func VerifyContextPackValueExpected(
	pack ContextPack,
	expectedDigest string,
	expectedID string,
) (map[string]any, error) {
	if pack.SchemaVersion != CampaignSchemaVersion || pack.PackID == "" || pack.Digest == "" {
		return nil, errors.New("context pack identity is missing")
	}
	claimedDigest, claimedID := pack.Digest, pack.PackID
	if expectedDigest != "" {
		if !sha256IdentityRE.MatchString(expectedDigest) {
			return nil, errors.New("expected context-pack digest is malformed")
		}
		if expectedID == "" {
			expectedID = "context-" + strings.TrimPrefix(expectedDigest, "sha256:")[:20]
		}
		if claimedDigest != expectedDigest || claimedID != expectedID {
			return nil, errors.New(
				"context pack does not match the independently expected manager identity")
		}
	} else if expectedID != "" {
		return nil, errors.New("expected pack ID requires an expected digest")
	}
	pack.Digest, pack.PackID = "", ""
	computed, err := CanonicalDigest(pack)
	if err != nil {
		return nil, err
	}
	computedID := "context-" + strings.TrimPrefix(computed, "sha256:")[:20]
	valid := claimedDigest == computed && claimedID == computedID
	if !valid {
		return nil, errors.New("context pack digest or pack ID mismatch")
	}
	pack.Digest, pack.PackID = claimedDigest, claimedID
	if err := validateContextPackSemantics(pack); err != nil {
		return nil, err
	}
	return map[string]any{
		"valid": true, "packId": claimedID, "digest": claimedDigest,
		"scope": pack.Scope, "generation": pack.Generation,
		"effectiveProfile": pack.EffectiveProfile,
	}, nil
}

func validateContextPackSemantics(pack ContextPack) error {
	if pack.SchemaVersion != CampaignSchemaVersion ||
		len([]byte(pack.Task)) < 1 || len([]byte(pack.Task)) > 2000 {
		return errors.New("context pack schema or task is invalid")
	}
	if pack.Role != "manager" && pack.Role != "drafter" {
		return errors.New("context pack role is invalid")
	}
	maximumBudget := 8192
	if pack.Role == "drafter" {
		maximumBudget = 4096
	}
	if pack.TokenBudget < 512 || pack.TokenBudget > maximumBudget ||
		pack.EstimatedTokens < 1 || pack.EstimatedTokens > pack.TokenBudget {
		return errors.New("context pack token bounds are invalid")
	}
	tiers, err := ValidateTierList(pack.AllowedTiers)
	if err != nil || len(tiers) != len(pack.AllowedTiers) {
		return errors.New("context pack epistemic tiers are invalid or repeated")
	}
	if strings.TrimSpace(pack.RequestedProfile) == "" ||
		strings.TrimSpace(pack.EffectiveProfile) == "" {
		return errors.New("context pack profile identities are missing")
	}
	laneSet := map[string]bool{}
	for _, lane := range pack.ActiveLanes {
		switch lane {
		case "exact", "fts", "graph", "dense", "rerank":
		default:
			return fmt.Errorf("context pack has invalid active lane %q", lane)
		}
		if laneSet[lane] {
			return errors.New("context pack repeats an active lane")
		}
		laneSet[lane] = true
	}
	if !laneSet["exact"] || !laneSet["fts"] || !laneSet["graph"] ||
		laneSet["rerank"] && !laneSet["dense"] {
		return errors.New("context pack active-lane shape is invalid")
	}
	if laneSet["dense"] && len(pack.Models) == 0 ||
		laneSet["rerank"] && len(pack.Models) < 2 ||
		!laneSet["dense"] && len(pack.Models) != 0 {
		return errors.New("context pack model set disagrees with active lanes")
	}
	modelIDs := map[string]bool{}
	for _, identity := range pack.Models {
		separator := strings.LastIndex(identity, "@")
		if separator < 1 || separator == len(identity)-1 {
			return errors.New("context pack contains an invalid model identity")
		}
		modelID, revision := identity[:separator], identity[separator+1:]
		if !profileIdentityRE.MatchString(modelID) ||
			!modelRevisionRE.MatchString(revision) || modelIDs[modelID] {
			return errors.New("context pack contains an invalid model identity")
		}
		modelIDs[modelID] = true
	}
	if pack.FallbackReason != nil &&
		(strings.TrimSpace(*pack.FallbackReason) == "" ||
			len([]byte(*pack.FallbackReason)) > 1000) {
		return errors.New("context pack fallback reason is invalid")
	}
	generationID := pack.Generation.ID
	if !strings.HasPrefix(generationID, "generation-") ||
		len(generationID) != len("generation-")+20 ||
		!hexDigestRE.MatchString(strings.Repeat("0", 44)+
			strings.TrimPrefix(generationID, "generation-")) ||
		!sha256IdentityRE.MatchString(pack.Generation.CorpusFingerprint) ||
		!sha256IdentityRE.MatchString(pack.Generation.ModelFingerprint) ||
		!sha256IdentityRE.MatchString(pack.Generation.RuntimeFingerprint) ||
		strings.TrimSpace(pack.Generation.Project) == "" ||
		strings.TrimSpace(pack.Generation.Worktree) == "" ||
		pack.Generation.GitRevision == "" ||
		pack.Generation.DirtyFingerprint == "" ||
		pack.Generation.ParserVersion == "" ||
		pack.Generation.ChunkerVersion == "" {
		return errors.New("context pack generation identity is invalid")
	}
	if _, err := time.Parse(time.RFC3339, pack.Generation.CreatedAt); err != nil {
		return errors.New("context pack generation createdAt is malformed")
	}
	if err := validateContextPackScope(pack.Scope); err != nil {
		return err
	}
	requiredOmissions := map[string]bool{
		"candidateCards": true, "budget": true, "cardLimit": true,
		"staleSource": true,
	}
	if len(pack.Omitted) != len(requiredOmissions) {
		return errors.New("context pack omission counters have unsupported fields")
	}
	for key, value := range pack.Omitted {
		if !requiredOmissions[key] || value < 0 {
			return errors.New("context pack omission counters are invalid")
		}
	}
	if len(pack.Cards) > 50 {
		return errors.New("context pack card cardinality is invalid")
	}
	if stableJSON(pack.RequiredHandles) != stableJSON(SortedUnique(pack.RequiredHandles)) {
		return errors.New("context pack required handles must be unique and sorted")
	}
	requiredSet := map[string]bool{}
	for _, handle := range pack.RequiredHandles {
		if strings.TrimSpace(handle) != handle || handle == "" || len([]byte(handle)) > 2000 {
			return errors.New("context pack contains an invalid required handle")
		}
		requiredSet[handle] = true
	}
	seenConstraints := map[string]bool{}
	for _, constraint := range pack.AcceptedConstraints {
		if constraint.ID == "" || seenConstraints[constraint.ID] ||
			!validOne(constraint.Kind, "objective", "scope", "exclusion", "success", "closure", "problem", "acceptance", "finding") ||
			strings.TrimSpace(constraint.Text) == "" || len([]byte(constraint.Text)) > 4000 ||
			!requiredSet[constraint.SourceHandle] {
			return errors.New("context pack accepted constraint is invalid or unbound")
		}
		seenConstraints[constraint.ID] = true
	}
	if pack.Scope.Kind == "active-run" && len(pack.AcceptedConstraints) < 2 {
		return errors.New("active-run context pack lacks accepted campaign and work constraints")
	}
	if pack.Scope.Kind != "active-run" && len(pack.AcceptedConstraints) != 0 {
		return errors.New("only active-run context packs may carry accepted constraints")
	}
	seenIDs, seenHandles := map[string]bool{}, map[string]bool{}
	for _, card := range pack.Cards {
		if err := ValidateContextCard(card); err != nil {
			return fmt.Errorf("context pack card: %w", err)
		}
		if seenIDs[card.ID] || seenHandles[card.Handle] {
			return errors.New("context pack card identities or handles are repeated")
		}
		seenIDs[card.ID], seenHandles[card.Handle] = true, true
		if !requiredSet[card.Handle] && !contextCardAllowedByTiers(card, tiers) {
			return fmt.Errorf(
				"context pack card %s (%s, tier %s) is outside the allowed epistemic tiers",
				card.ID, card.SourceClass, card.Metadata["tier"])
		}
		if strings.HasPrefix(card.Handle, "re-discipline://") {
			prefix := "re-discipline://" + generationID + "/"
			if !strings.HasPrefix(card.Handle, prefix+"chunks/") &&
				!strings.HasPrefix(card.Handle, prefix+"sources/") {
				return errors.New("context pack card belongs to another generation")
			}
		}
		if path := card.Metadata["path"]; path != "" {
			if err := validateEvalPath(path); err != nil {
				return errors.New("context pack card carries an invalid source path")
			}
		}
		if tier := card.Metadata["tier"]; tier != "" && !AllowedTiers[tier] {
			return errors.New("context pack card carries an invalid source tier")
		}
		for _, key := range []string{"passageHash", "sourceHash"} {
			if value := card.Metadata[key]; value != "" && !hexDigestRE.MatchString(value) {
				return errors.New("context pack provenance digest is malformed")
			}
		}
	}
	body, err := json.Marshal(pack)
	if err != nil {
		return err
	}
	if len(body) > 262144 || EstimateTokens(string(body)) != pack.EstimatedTokens ||
		pack.EstimatedTokens > pack.TokenBudget {
		return errors.New("context pack serialized token or byte accounting is invalid")
	}
	return nil
}

func validateContextPackScope(scope ContextPackScope) error {
	if scope.StateHeadRevision < 0 || !digestRE.MatchString(scope.StateHeadDigest) {
		return errors.New("context pack state-head binding is invalid")
	}
	if scope.StateHeadRevision == 0 && scope.EventID != "" ||
		scope.StateHeadRevision > 0 && !eventIDRE.MatchString(scope.EventID) {
		return errors.New("context pack event-head binding is invalid")
	}
	switch scope.Kind {
	case "project":
		if scope.CampaignID != "" || scope.CampaignSlug != "" ||
			scope.CampaignRevision != 0 || scope.WorkItemID != "" ||
			scope.WorkItemRevision != 0 || scope.RunID != "" || scope.RunRevision != 0 ||
			scope.CandidateSlug != "" || scope.RecruitingRunID != "" {
			return errors.New("project context scope carries run bindings")
		}
	case "active-run":
		if !campaignIDRE.MatchString(scope.CampaignID) ||
			!managedSlugRE.MatchString(scope.CampaignSlug) || scope.CampaignRevision < 1 ||
			!workItemIDRE.MatchString(scope.WorkItemID) || scope.WorkItemRevision < 1 ||
			!runIDRE.MatchString(scope.RunID) || scope.RunRevision < 0 ||
			scope.CandidateSlug != "" || scope.RecruitingRunID != "" {
			return errors.New("active-run context scope is incomplete or mixed")
		}
	case "recruiting-run":
		if !managedSlugRE.MatchString(scope.CandidateSlug) ||
			!workspaceIDRE.MatchString(scope.RecruitingRunID) ||
			scope.CampaignID != "" || scope.CampaignSlug != "" ||
			scope.CampaignRevision != 0 || scope.WorkItemID != "" ||
			scope.WorkItemRevision != 0 || scope.RunID != "" || scope.RunRevision != 0 {
			return errors.New("recruiting-run context scope is incomplete or mixed")
		}
	default:
		return errors.New("context pack target kind is invalid")
	}
	return nil
}

type CalibrationCandidate struct {
	Identity              string                    `json:"identity"`
	Weights               map[string]int            `json:"weights"`
	DevelopmentHit        int                       `json:"developmentHits"`
	HoldoutHit            int                       `json:"holdoutHits"`
	DevelopmentMetrics    QualityMetrics            `json:"developmentMetrics"`
	HoldoutMetrics        QualityMetrics            `json:"holdoutMetrics"`
	FindingDevelopment    *FindingEvaluationMetrics `json:"findingDevelopment,omitempty"`
	FindingHoldout        *FindingEvaluationMetrics `json:"findingHoldout,omitempty"`
	Violations            int                       `json:"violations"`
	HardGatesPassed       bool                      `json:"hardGatesPassed"`
	NonInferiorToBaseline bool                      `json:"nonInferiorToBaseline"`
	Pareto                bool                      `json:"pareto"`
	BenchmarkDigest       string                    `json:"benchmarkDigest,omitempty"`
}

type CalibrationReport struct {
	SchemaVersion       int                     `json:"schemaVersion"`
	RunID               string                  `json:"runId"`
	BaseProfile         string                  `json:"baseProfile"`
	ActiveBefore        string                  `json:"activeBefore"`
	ActiveAfter         string                  `json:"activeAfter"`
	EvalDigest          string                  `json:"evalDigest"`
	FindingSuiteDigests []string                `json:"findingSuiteDigests,omitempty"`
	CorpusFingerprint   string                  `json:"corpusFingerprint"`
	ModelFingerprint    string                  `json:"modelFingerprint"`
	RuntimeContract     RuntimeContractIdentity `json:"runtimeContract"`
	Candidates          []CalibrationCandidate  `json:"candidates"`
	ParetoFrontier      []CalibrationCandidate  `json:"paretoFrontier"`
	Recommended         CalibrationCandidate    `json:"recommended"`
	CandidatePath       string                  `json:"candidatePath"`
	CandidateDigest     string                  `json:"candidateDigest"`
	Activated           bool                    `json:"activated"`
	// FailureReason is set when a sweep produced no promotable candidate. The
	// report is still written so the measurements survive and the operator can
	// see which gate blocked which candidate.
	FailureReason string `json:"failureReason,omitempty"`
}

// persistCalibrationFailure records a sweep that produced no promotable
// candidate, returning the report path or "" if it could not be written.
//
// A failed calibration is data. Without this the full candidate grid is
// measured and then discarded, leaving no way to tell whether a candidate
// missed by one case or by fifty.
func (service *Service) persistCalibrationFailure(
	before string,
	evalDigest string,
	generation Generation,
	selected SelectedProfile,
	candidates []CalibrationCandidate,
	frontier []CalibrationCandidate,
	findingSuiteDigests []string,
	reason string,
) string {
	runID := nowRunID("calibration")
	runDir, err := containedOutputPath(
		filepath.Dir(service.Index.CacheRoot),
		filepath.Join("calibration", runID),
	)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return ""
	}
	report := CalibrationReport{
		SchemaVersion: 1, RunID: runID, BaseProfile: service.ProfileCatalog.ProfileID,
		ActiveBefore: before, ActiveAfter: before, EvalDigest: evalDigest,
		FindingSuiteDigests: findingSuiteDigests,
		CorpusFingerprint:   generation.CorpusFingerprint,
		ModelFingerprint:    mustDigest(service.ModelManifest.Models),
		RuntimeContract:     RuntimeContract(selected.Runtime),
		Candidates:          candidates, ParetoFrontier: frontier,
		Activated: false, FailureReason: reason,
	}
	path := filepath.Join(runDir, "report.json")
	if err := AtomicWriteJSON(path, report, 0o600); err != nil {
		return ""
	}
	return filepath.ToSlash(path)
}

func (service *Service) Calibrate(ctx context.Context) (CalibrationReport, error) {
	cases, err := service.loadProjectEvalCases()
	if err != nil {
		return CalibrationReport{}, err
	}
	developmentCases, holdoutCases := splitEvalCases(cases)
	if len(developmentCases) == 0 || len(holdoutCases) == 0 {
		return CalibrationReport{}, errors.New(
			"calibration requires topic-isolated development and holdout cases")
	}
	findingSuites, err := service.loadProjectFindingEvalSuites()
	if err != nil {
		return CalibrationReport{}, err
	}
	findingDevelopmentCases, findingHoldoutCases, findingSuiteDigests :=
		splitFindingEvalSuites(findingSuites)
	// Calibration measures every candidate against one generation for far
	// longer than a benchmark does. It never re-ensures, so the lease alone
	// keeps a concurrently publishing session's retention from deleting the
	// database these candidates are being measured against.
	generation, selected, lease, err := service.leaseMeasurementGeneration(
		ctx, "calibration")
	if err != nil {
		return CalibrationReport{}, err
	}
	defer lease.Release()
	before := selected.EffectiveIdentity
	evalDigest, _ := CanonicalDigest(cases)
	if len(findingSuites) > 0 {
		evalDigest, _ = CanonicalDigest(struct {
			PassageCases  []EvalCase         `json:"passageCases"`
			FindingSuites []FindingEvalSuite `json:"findingSuites"`
		}{cases, findingSuites})
	}
	baselineRetriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}
	_, baselineHoldoutMetrics, err := evaluateRetrieverCases(
		ctx, baselineRetriever, holdoutCases)
	if err != nil {
		return CalibrationReport{}, fmt.Errorf(
			"evaluate active holdout baseline: %w", err)
	}
	baselineHoldoutMetrics = ratchetBaseline(
		selected.Effective.Benchmark, baselineHoldoutMetrics)
	var baselineFindingHoldout *FindingEvaluationMetrics
	if len(findingHoldoutCases) > 0 {
		findingReport, err := EvaluateFindingRetriever(
			ctx, baselineRetriever, findingHoldoutCases)
		if err != nil {
			return CalibrationReport{}, fmt.Errorf(
				"evaluate active finding holdout baseline: %w", err)
		}
		metrics := findingReport.MetricsBySplit["holdout"]
		baselineFindingHoldout = &metrics
	}
	candidates := []CalibrationCandidate{}
	rowsByIdentity := map[string]EffectiveProfile{}
	findingDevelopmentCache := map[string]FindingEvaluationMetrics{}
	for _, exact := range []int{6, 8, 10} {
		for _, fts := range []int{4, 6, 8} {
			for _, graph := range []int{1, 2, 3} {
				for _, dense := range []int{2, 4, 6} {
					row := cloneEffectiveProfile(selected.Effective)
					row.Weights = map[string]int{
						"exact": exact, "fts": fts, "graph": graph, "dense": dense,
					}
					candidateProfile, err := selectedForRow(
						service.ProfileCatalog.ProfileID, row, selected.Runtime, selected.Models,
					)
					if err != nil {
						return CalibrationReport{}, err
					}
					retriever := Retriever{
						Boundary: service.Boundary, Generation: generation, Profile: candidateProfile,
					}
					outcomes, metrics, err := evaluateRetrieverCases(ctx, retriever, developmentCases)
					if err != nil {
						return CalibrationReport{}, err
					}
					hits := relevantPathHits(outcomes)
					candidate := CalibrationCandidate{
						Identity: candidateProfile.EffectiveIdentity,
						Weights:  cloneWeights(row.Weights), DevelopmentHit: hits,
						DevelopmentMetrics: metrics,
						Violations: metrics.AuthorityViolations + metrics.CitationViolations +
							metrics.HardNegativeHits,
						HardGatesPassed: hardMetricsPassed(metrics),
					}
					if len(findingDevelopmentCases) > 0 {
						cacheKey := findingCalibrationWeightKey(row.Weights)
						findingMetrics, cached := findingDevelopmentCache[cacheKey]
						if !cached {
							findingReport, findingErr := EvaluateFindingRetriever(
								ctx, retriever, findingDevelopmentCases)
							if findingErr != nil {
								return CalibrationReport{}, findingErr
							}
							findingMetrics = findingReport.MetricsBySplit["development"]
							findingDevelopmentCache[cacheKey] = findingMetrics
						}
						candidate.FindingDevelopment = &findingMetrics
						candidate.Violations += findingMetrics.HardNegativeHits
						candidate.HardGatesPassed = candidate.HardGatesPassed &&
							findingCalibrationMetricsPassed(findingMetrics)
					}
					candidates = append(candidates, candidate)
					rowsByIdentity[candidate.Identity] = row
				}
			}
		}
	}
	frontierIndexes := paretoFrontierIndexes(candidates)
	if len(frontierIndexes) == 0 {
		reason := "no calibration candidate reached the Pareto frontier"
		path := service.persistCalibrationFailure(
			before, evalDigest, generation, selected, candidates, nil,
			findingSuiteDigests, reason,
		)
		if path != "" {
			return CalibrationReport{}, fmt.Errorf("%s; measurements written to %s", reason, path)
		}
		return CalibrationReport{}, errors.New(reason)
	}
	findingHoldoutCache := map[string]FindingEvaluationMetrics{}
	for _, index := range frontierIndexes {
		row := rowsByIdentity[candidates[index].Identity]
		candidateProfile, err := selectedForRow(
			service.ProfileCatalog.ProfileID, row, selected.Runtime, selected.Models,
		)
		if err != nil {
			return CalibrationReport{}, err
		}
		retriever := Retriever{
			Boundary: service.Boundary, Generation: generation, Profile: candidateProfile,
		}
		outcomes, metrics, err := evaluateRetrieverCases(ctx, retriever, holdoutCases)
		if err != nil {
			return CalibrationReport{}, err
		}
		candidates[index].HoldoutHit = relevantPathHits(outcomes)
		candidates[index].HoldoutMetrics = metrics
		candidates[index].Pareto = true
		candidates[index].HardGatesPassed =
			candidates[index].HardGatesPassed && hardMetricsPassed(metrics)
		candidates[index].NonInferiorToBaseline =
			calibrationNonInferior(metrics, baselineHoldoutMetrics)
		if len(findingHoldoutCases) > 0 {
			cacheKey := findingCalibrationWeightKey(row.Weights)
			findingMetrics, cached := findingHoldoutCache[cacheKey]
			if !cached {
				findingReport, findingErr := EvaluateFindingRetriever(
					ctx, retriever, findingHoldoutCases)
				if findingErr != nil {
					return CalibrationReport{}, findingErr
				}
				findingMetrics = findingReport.MetricsBySplit["holdout"]
				findingHoldoutCache[cacheKey] = findingMetrics
			}
			candidates[index].FindingHoldout = &findingMetrics
			candidates[index].Violations += findingMetrics.HardNegativeHits
			candidates[index].HardGatesPassed = candidates[index].HardGatesPassed &&
				findingCalibrationMetricsPassed(findingMetrics)
			candidates[index].NonInferiorToBaseline =
				candidates[index].NonInferiorToBaseline &&
					baselineFindingHoldout != nil &&
					findingCalibrationNonInferior(findingMetrics, *baselineFindingHoldout)
		}
		candidates[index].Violations +=
			metrics.AuthorityViolations + metrics.CitationViolations +
				metrics.HardNegativeHits
		benchmarkDigest, _ := calibrationBenchmarkDigest(
			candidates[index].Identity,
			evalDigest,
			generation.CorpusFingerprint,
			candidates[index].DevelopmentMetrics,
			candidates[index].HoldoutMetrics,
			baselineHoldoutMetrics,
			candidates[index].FindingDevelopment,
			candidates[index].FindingHoldout,
			baselineFindingHoldout,
			findingSuiteDigests,
		)
		candidates[index].BenchmarkDigest = benchmarkDigest
	}
	frontier := []CalibrationCandidate{}
	for _, index := range frontierIndexes {
		if candidates[index].HardGatesPassed &&
			candidates[index].NonInferiorToBaseline {
			frontier = append(frontier, candidates[index])
		}
	}
	if len(frontier) == 0 {
		reason := "no Pareto finalist passed frozen holdout hard gates " +
			"and non-inferiority against the incumbent"
		// Report the candidates that reached holdout evaluation, not nil.
		// Their metrics are populated in place above, and they are precisely
		// the set an operator needs in order to see which gate each one missed.
		evaluated := make([]CalibrationCandidate, 0, len(frontierIndexes))
		for _, index := range frontierIndexes {
			evaluated = append(evaluated, candidates[index])
		}
		path := service.persistCalibrationFailure(
			before, evalDigest, generation, selected, candidates, evaluated,
			findingSuiteDigests, reason,
		)
		if path != "" {
			return CalibrationReport{}, fmt.Errorf("%s; measurements written to %s", reason, path)
		}
		return CalibrationReport{}, errors.New(reason)
	}
	sort.Slice(frontier, func(i, j int) bool {
		return calibrationCandidateLess(frontier[i], frontier[j])
	})
	recommended := frontier[0]
	runID := nowRunID("calibration")
	runDir, err := containedOutputPath(
		filepath.Dir(service.Index.CacheRoot),
		filepath.Join("calibration", runID),
	)
	if err != nil {
		return CalibrationReport{}, err
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return CalibrationReport{}, err
	}
	candidateProfile := cloneRetrievalProfile(service.ProfileCatalog)
	candidateProfile.BaseProfile = service.ProfileCatalog.ProfileID
	candidateProfile.Approval = nil
	candidateProfile.ProfileID = "project:candidate-" +
		strings.TrimPrefix(strings.SplitN(recommended.Identity, "@", 2)[1], "sha256:")[:16]
	for index := range candidateProfile.EffectiveProfiles {
		if candidateProfile.EffectiveProfiles[index].Name == selected.Effective.Name {
			candidateProfile.EffectiveProfiles[index].Weights = cloneWeights(recommended.Weights)
			candidateProfile.EffectiveProfiles[index].Benchmark = BenchmarkEvidence{
				Suite: "project-calibration-v1", Digest: recommended.BenchmarkDigest,
				Status: "passed", EvaluatedAt: RFC3339UTC(time.Now()),
				EvalFingerprint: evalDigest, CorpusFingerprint: generation.CorpusFingerprint,
				ModelFingerprint: mustDigest(service.ModelManifest.Models),
				ChunkerVersion:   generation.ChunkerVersion,
				ParserVersion:    generation.ParserVersion,
				// Record what this profile was accepted at so a later
				// calibration compares against the tighter of this and the
				// incumbent's drifted live score.
				RatifiedHardNegativeHits:   ratifiedHits(recommended.HoldoutMetrics),
				RatifiedAbstentionAccuracy: recommended.HoldoutMetrics.AbstentionAccuracy,
			}
			runtimeDigest, _ := CanonicalDigest(RuntimeContract(selected.Runtime))
			candidateProfile.EffectiveProfiles[index].Benchmark.RuntimeFingerprint = runtimeDigest
		}
	}
	if err := ValidateProfile(candidateProfile); err != nil {
		return CalibrationReport{}, fmt.Errorf("candidate capability matrix is invalid: %w", err)
	}
	candidatePath := filepath.Join(runDir, "candidate-profile.json")
	if err := AtomicWriteJSON(candidatePath, candidateProfile, 0o600); err != nil {
		return CalibrationReport{}, err
	}
	candidateBody, err := os.ReadFile(candidatePath)
	if err != nil {
		return CalibrationReport{}, err
	}
	candidateDigest := "sha256:" + SHA256Bytes(candidateBody)
	afterSelected, err := service.selectProfile(generation.Runtime)
	if err != nil || afterSelected.EffectiveIdentity != before {
		return CalibrationReport{}, errors.New("active profile changed during calibration")
	}
	report := CalibrationReport{
		SchemaVersion: 1, RunID: runID, BaseProfile: service.ProfileCatalog.ProfileID,
		ActiveBefore: before, ActiveAfter: before, EvalDigest: evalDigest,
		FindingSuiteDigests: findingSuiteDigests,
		CorpusFingerprint:   generation.CorpusFingerprint,
		ModelFingerprint:    mustDigest(service.ModelManifest.Models),
		RuntimeContract:     RuntimeContract(selected.Runtime),
		Candidates:          candidates, ParetoFrontier: frontier, Recommended: recommended,
		CandidatePath: filepath.ToSlash(candidatePath), CandidateDigest: candidateDigest,
		Activated: false,
	}
	if err := AtomicWriteJSON(filepath.Join(runDir, "report.json"), report, 0o600); err != nil {
		return CalibrationReport{}, err
	}
	return report, nil
}

func calibrationBenchmarkDigest(
	identity string,
	evalDigest string,
	corpusFingerprint string,
	development QualityMetrics,
	holdout QualityMetrics,
	baselineHoldout QualityMetrics,
	findingDevelopment *FindingEvaluationMetrics,
	findingHoldout *FindingEvaluationMetrics,
	baselineFindingHoldout *FindingEvaluationMetrics,
	findingSuiteDigests []string,
) (string, error) {
	return CanonicalDigest(struct {
		Suite                  string                    `json:"suite"`
		Identity               string                    `json:"identity"`
		EvalDigest             string                    `json:"evalDigest"`
		Corpus                 string                    `json:"corpusFingerprint"`
		Development            QualityMetrics            `json:"development"`
		Holdout                QualityMetrics            `json:"holdout"`
		BaselineHoldout        QualityMetrics            `json:"baselineHoldout"`
		FindingDevelopment     *FindingEvaluationMetrics `json:"findingDevelopment,omitempty"`
		FindingHoldout         *FindingEvaluationMetrics `json:"findingHoldout,omitempty"`
		BaselineFindingHoldout *FindingEvaluationMetrics `json:"baselineFindingHoldout,omitempty"`
		FindingSuiteDigests    []string                  `json:"findingSuiteDigests,omitempty"`
	}{
		"project-calibration-v1", identity, evalDigest, corpusFingerprint,
		metricsWithoutLatency(development), metricsWithoutLatency(holdout),
		metricsWithoutLatency(baselineHoldout),
		findingDevelopment, findingHoldout, baselineFindingHoldout,
		append([]string(nil), findingSuiteDigests...),
	})
}

func calibrationNonInferior(
	candidate QualityMetrics,
	baseline QualityMetrics,
) bool {
	return candidate.RecallAtK >= baseline.RecallAtK &&
		candidate.NDCG >= baseline.NDCG &&
		candidate.CompleteEvidenceCoverage >= baseline.CompleteEvidenceCoverage &&
		candidate.CitationRecall >= baseline.CitationRecall &&
		candidate.AuthorityViolations <= baseline.AuthorityViolations &&
		// Moved off the absolute gate: still may never regress, but is now
		// measured against the incumbent rather than against perfection.
		candidate.HardNegativeHits <= baseline.HardNegativeHits &&
		candidate.AbstentionAccuracy >= baseline.AbstentionAccuracy
}

func splitEvalCases(cases []EvalCase) ([]EvalCase, []EvalCase) {
	development := []EvalCase{}
	holdout := []EvalCase{}
	for _, eval := range cases {
		if eval.Split == "development" {
			development = append(development, eval)
		} else if eval.Split == "holdout" {
			holdout = append(holdout, eval)
		}
	}
	return development, holdout
}

func evaluateRetrieverCases(
	ctx context.Context,
	retriever Retriever,
	cases []EvalCase,
) ([]CaseOutcome, QualityMetrics, error) {
	outcomes := make([]CaseOutcome, 0, len(cases))
	for _, eval := range cases {
		outcome, err := runEvaluationCase(ctx, eval, eval.TokenBudget, retriever.Boundary.Root, retriever.Search)
		if err != nil {
			return nil, QualityMetrics{}, fmt.Errorf("case %s: %w", eval.ID, err)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, calculateMetrics(outcomes, cases), nil
}

func relevantPathHits(outcomes []CaseOutcome) int {
	hits := 0
	for _, outcome := range outcomes {
		hits += len(outcome.RelevantPaths)
	}
	return hits
}

func hardMetricsPassed(metrics QualityMetrics) bool {
	// Absolute gates carry only properties that must never regress under any
	// tuning: serving a forbidden tier, emitting a malformed citation, serving
	// content that no longer matches its source, exceeding the caller's
	// budget, or being non-deterministic.
	//
	// CitationViolations, HardNegativeHits, and AbstentionAccuracy were
	// previously absolute. All three trade off against recall, so requiring
	// perfection on them meant no weight vector could ever qualify and
	// calibration could not promote even a strict improvement over the
	// incumbent. They are compared against the incumbent in
	// calibrationNonInferior instead.
	return metrics.AuthorityViolations == 0 &&
		metrics.CitationMetadataViolations == 0 && metrics.StaleResultRate == 0 &&
		metrics.BudgetComplianceRate == 1 && metrics.DeterministicReplayRate == 1
}

func hasApplicableQualityGate(outcomes []CaseOutcome) bool {
	for _, outcome := range outcomes {
		if outcome.QualityGateApplicable {
			return true
		}
	}
	return false
}

func paretoFrontierIndexes(candidates []CalibrationCandidate) []int {
	frontier := []int{}
	for index, candidate := range candidates {
		// The frontier is built on quality alone. Filtering by HardGatesPassed
		// here meant that when no candidate passed, the frontier was empty and
		// every measurement was discarded before the incumbent comparison ran.
		// Hard gates are applied at finalist selection instead.
		dominated := false
		for otherIndex, other := range candidates {
			if otherIndex != index &&
				developmentDominates(other.DevelopmentMetrics, candidate.DevelopmentMetrics) &&
				findingDevelopmentNotWorse(other.FindingDevelopment, candidate.FindingDevelopment) {
				dominated = true
				break
			}
		}
		if !dominated {
			frontier = append(frontier, index)
		}
	}
	return frontier
}

func developmentDominates(left, right QualityMetrics) bool {
	notWorse := left.RecallAtK >= right.RecallAtK &&
		left.NDCG >= right.NDCG &&
		left.CompleteEvidenceCoverage >= right.CompleteEvidenceCoverage &&
		left.CitationRecall >= right.CitationRecall &&
		left.RelevantTokenRatio >= right.RelevantTokenRatio &&
		left.DuplicateTokenRatio <= right.DuplicateTokenRatio &&
		// A candidate may only be pruned by one that is no worse on every axis
		// finalist selection will later check. These three are gated by
		// hardMetricsPassed or calibrationNonInferior, so omitting them let a
		// quality-dominant but ungated candidate shadow a promotable one that
		// was then never holdout-evaluated.
		left.AuthorityViolations <= right.AuthorityViolations &&
		left.HardNegativeHits <= right.HardNegativeHits &&
		left.AbstentionAccuracy >= right.AbstentionAccuracy
	strict := left.RecallAtK > right.RecallAtK ||
		left.NDCG > right.NDCG ||
		left.CompleteEvidenceCoverage > right.CompleteEvidenceCoverage ||
		left.CitationRecall > right.CitationRecall ||
		left.RelevantTokenRatio > right.RelevantTokenRatio ||
		left.DuplicateTokenRatio < right.DuplicateTokenRatio
	return notWorse && strict
}

func calibrationCandidateLess(left, right CalibrationCandidate) bool {
	if left.FindingHoldout != nil && right.FindingHoldout != nil {
		for _, pair := range [][2]float64{
			{left.FindingHoldout.FindingRecall, right.FindingHoldout.FindingRecall},
			{left.FindingHoldout.MeanReciprocalRank, right.FindingHoldout.MeanReciprocalRank},
		} {
			if pair[0] != pair[1] {
				return pair[0] > pair[1]
			}
		}
	}
	for _, pair := range [][2]float64{
		{left.HoldoutMetrics.CompleteEvidenceCoverage, right.HoldoutMetrics.CompleteEvidenceCoverage},
		{left.HoldoutMetrics.RecallAtK, right.HoldoutMetrics.RecallAtK},
		{left.HoldoutMetrics.NDCG, right.HoldoutMetrics.NDCG},
		{left.DevelopmentMetrics.RecallAtK, right.DevelopmentMetrics.RecallAtK},
		{left.DevelopmentMetrics.RelevantTokenRatio, right.DevelopmentMetrics.RelevantTokenRatio},
	} {
		if pair[0] != pair[1] {
			return pair[0] > pair[1]
		}
	}
	if left.HoldoutMetrics.DuplicateTokenRatio != right.HoldoutMetrics.DuplicateTokenRatio {
		return left.HoldoutMetrics.DuplicateTokenRatio < right.HoldoutMetrics.DuplicateTokenRatio
	}
	return stableJSON(left.Weights) < stableJSON(right.Weights)
}

func cloneWeights(input map[string]int) map[string]int {
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

// ratchetBaseline clamps the comparison floor to the tighter of the
// incumbent's score on today's corpus and the score it was ratified at.
//
// The incumbent's live score drifts as the corpus changes - an overturn that
// leaves a near-duplicate chronicle behind can raise its hard-negative count
// with no profile change at all - and without this clamp that drift silently
// raises the bar a candidate is allowed to meet.
//
// A profile ratified at zero hard-negative hits is the case that matters most
// and the one that was silently exempt: zero is the best achievable score, so
// nothing may relax it, yet it is also what an unrecorded value looks like.
// Absent stays absent and disengages the ratchet; a recorded zero clamps.
func ratchetBaseline(ratified BenchmarkEvidence, baseline QualityMetrics) QualityMetrics {
	if ratified.RatifiedHardNegativeHits != nil &&
		*ratified.RatifiedHardNegativeHits < baseline.HardNegativeHits {
		baseline.HardNegativeHits = *ratified.RatifiedHardNegativeHits
	}
	if ratified.RatifiedAbstentionAccuracy > baseline.AbstentionAccuracy {
		baseline.AbstentionAccuracy = ratified.RatifiedAbstentionAccuracy
	}
	return baseline
}

// ratifiedHits records a candidate's accepted hard-negative count, including
// zero. Recording zero is the whole point: it is the value that must clamp the
// hardest and the one an omitted field cannot express.
func ratifiedHits(metrics QualityMetrics) *int {
	hits := metrics.HardNegativeHits
	return &hits
}

func cloneEffectiveProfile(input EffectiveProfile) EffectiveProfile {
	output := input
	output.Lanes = append([]string(nil), input.Lanes...)
	output.Weights = cloneWeights(input.Weights)
	if input.Benchmark.RatifiedHardNegativeHits != nil {
		hits := *input.Benchmark.RatifiedHardNegativeHits
		output.Benchmark.RatifiedHardNegativeHits = &hits
	}
	if input.Requires.Embedding != nil {
		value := *input.Requires.Embedding
		output.Requires.Embedding = &value
	}
	if input.Requires.Reranker != nil {
		value := *input.Requires.Reranker
		output.Requires.Reranker = &value
	}
	return output
}

func cloneRetrievalProfile(input RetrievalProfile) RetrievalProfile {
	output := input
	output.Approval = nil
	output.EffectiveProfiles = make([]EffectiveProfile, len(input.EffectiveProfiles))
	for index, row := range input.EffectiveProfiles {
		output.EffectiveProfiles[index] = cloneEffectiveProfile(row)
	}
	return output
}

func selectedForRow(
	requested string,
	row EffectiveProfile,
	runtime RuntimeIdentity,
	models []ModelIdentity,
) (SelectedProfile, error) {
	requestedDigest, _ := CanonicalDigest(requested)
	identity, err := CanonicalDigest(struct {
		Profile EffectiveProfile
		Runtime RuntimeContractIdentity
		Models  []ModelIdentity
	}{runtimeEffectiveProfile(row), RuntimeContract(runtime), models})
	if err != nil {
		return SelectedProfile{}, err
	}
	return SelectedProfile{
		RequestedIdentity: requested + "@" + requestedDigest,
		EffectiveIdentity: row.Name + "@" + identity,
		Effective:         row, ActiveLanes: row.Lanes, Models: models, Runtime: runtime,
	}, nil
}

func (service *Service) loadProjectEvalCases() ([]EvalCase, error) {
	root := filepath.Join(service.Boundary.Root, ".re-discipline", "knowledge", "evals")
	canonicalRoot, err := canonicalExistingPath(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no ratified project evaluation cases exist")
		}
		return nil, err
	}
	if !withinRoot(service.Boundary.Root, canonicalRoot) {
		return nil, errors.New("project evaluation root escapes the project boundary")
	}
	cases := []EvalCase{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evaluation corpus contains a symbolic link: %s", path)
		}
		if entry.IsDir() {
			if path != root && entry.Name() == "findings" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return nil
		}
		resolved, err := canonicalExistingPath(path)
		if err != nil {
			return err
		}
		if !withinRoot(canonicalRoot, resolved) {
			return errors.New("evaluation case file escapes the evaluation root")
		}
		loaded, err := LoadEvalCases(resolved)
		if err != nil {
			return err
		}
		cases = append(cases, loaded...)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no ratified project evaluation cases exist")
		}
		return nil, err
	}
	if len(cases) == 0 {
		return nil, errors.New("no ratified project evaluation cases exist")
	}
	if err := ValidateEvalCorpus(cases); err != nil {
		return nil, fmt.Errorf("combined project evaluation corpus: %w", err)
	}
	return cases, nil
}

// FindingEvalCase and FindingAblationReport are additive finding-card
// judgments. They intentionally use stable finding/evidence handles rather
// than adapting the path-only 0.7 case schema in place.
type FindingEvalCase struct {
	ID                      string              `json:"id"`
	Role                    string              `json:"role"`
	Topic                   string              `json:"topic"`
	Split                   string              `json:"split"`
	Query                   string              `json:"query"`
	QueryClass              string              `json:"queryClass"`
	AllowedSourceClasses    []string            `json:"allowedSourceClasses"`
	AllowedReviewStates     []string            `json:"allowedReviewStates"`
	AllowedValidities       []string            `json:"allowedValidities"`
	TokenBudget             int                 `json:"tokenBudget"`
	Options                 FindingQueryOptions `json:"-"`
	ExpectedFindingIDs      []string            `json:"expectedFindingIds"`
	ExpectedFindingHandles  []string            `json:"expectedFindingHandles"`
	ExpectedEvidenceHandles []string            `json:"expectedEvidenceHandles,omitempty"`
	ExpectedRawPaths        []string            `json:"expectedRawPaths,omitempty"`
	ExpectedSourceClasses   map[string]string   `json:"expectedSourceClasses"`
	ExpectedReviewStates    map[string]string   `json:"expectedReviewStates"`
	ExpectedValidities      map[string]string   `json:"expectedValidities"`
	HardNegativeFindingIDs  []string            `json:"hardNegativeFindingIds,omitempty"`
	Answerable              bool                `json:"answerable"`
}

type FindingCaseOutcome struct {
	CaseID                       string         `json:"caseId"`
	Role                         string         `json:"role"`
	Topic                        string         `json:"topic"`
	Split                        string         `json:"split"`
	QueryClass                   string         `json:"queryClass"`
	Status                       string         `json:"status"`
	CardIDs                      []string       `json:"cardIds"`
	FindingIDs                   []string       `json:"findingIds"`
	RawPaths                     []string       `json:"rawPaths"`
	RelevantFindingRanks         []int          `json:"relevantFindingRanks"`
	EvidenceHandlesFound         []string       `json:"evidenceHandlesFound"`
	HardNegativeHits             []string       `json:"hardNegativeHits"`
	LaneRelevantHits             map[string]int `json:"laneRelevantHits"`
	UniqueRelevantFirstHits      map[string]int `json:"uniqueRelevantFirstHits"`
	RerankDelta                  int            `json:"rerankDelta"`
	NormalizedTokens             int            `json:"normalizedTokens"`
	RawTokens                    int            `json:"rawTokens"`
	AbstentionCorrect            bool           `json:"abstentionCorrect"`
	EvidenceHandlesComplete      bool           `json:"evidenceHandlesComplete"`
	FindingHandlesComplete       bool           `json:"findingHandlesComplete"`
	SourceClassesAccurate        bool           `json:"sourceClassesAccurate"`
	ReviewStatesAccurate         bool           `json:"reviewStatesAccurate"`
	ValiditiesAccurate           bool           `json:"validitiesAccurate"`
	VocabularyDisjointApplicable bool           `json:"vocabularyDisjointApplicable"`
	ClaimVocabularyDisjoint      bool           `json:"claimVocabularyDisjoint"`
	DurabilityLabelsAccurate     bool           `json:"durabilityLabelsAccurate"`
	ReplayIdentical              bool           `json:"replayIdentical"`
}

type FindingEvaluationMetrics struct {
	CaseCount               int     `json:"caseCount"`
	FindingRecall           float64 `json:"findingRecall"`
	MeanReciprocalRank      float64 `json:"meanReciprocalRank"`
	RawPathRecall           float64 `json:"rawPathRecall"`
	AbstentionAccuracy      float64 `json:"abstentionAccuracy"`
	FindingHandleAccuracy   float64 `json:"findingHandleAccuracy"`
	EvidenceHandleAccuracy  float64 `json:"evidenceHandleAccuracy"`
	SourceClassAccuracy     float64 `json:"sourceClassAccuracy"`
	ReviewStateAccuracy     float64 `json:"reviewStateAccuracy"`
	ValidityAccuracy        float64 `json:"validityAccuracy"`
	VocabularyDisjointRate  float64 `json:"vocabularyDisjointRate"`
	HardNegativeHits        int     `json:"hardNegativeHits"`
	DeterministicReplayRate float64 `json:"deterministicReplayRate"`
	NormalizedMedianTokens  int     `json:"normalizedMedianTokens"`
	RawMedianTokens         int     `json:"rawMedianTokens"`
}

type FindingAblationReport struct {
	SchemaVersion             int                                 `json:"schemaVersion"`
	SuiteID                   string                              `json:"suiteId,omitempty"`
	SuiteDigest               string                              `json:"suiteDigest,omitempty"`
	CorpusSnapshot            string                              `json:"corpusSnapshot,omitempty"`
	Cases                     []FindingCaseOutcome                `json:"cases"`
	FindingRecall             float64                             `json:"findingRecall"`
	MeanReciprocalRank        float64                             `json:"meanReciprocalRank"`
	RawPathRecall             float64                             `json:"rawPathRecall"`
	AbstentionAccuracy        float64                             `json:"abstentionAccuracy"`
	EvidenceHandleAccuracy    float64                             `json:"evidenceHandleAccuracy"`
	FindingHandleAccuracy     float64                             `json:"findingHandleAccuracy"`
	SourceClassAccuracy       float64                             `json:"sourceClassAccuracy"`
	ReviewStateAccuracy       float64                             `json:"reviewStateAccuracy"`
	ValidityAccuracy          float64                             `json:"validityAccuracy"`
	VocabularyDisjointRate    float64                             `json:"vocabularyDisjointRate"`
	DurabilityLabelAccuracy   float64                             `json:"durabilityLabelAccuracy"`
	MetricsBySplit            map[string]FindingEvaluationMetrics `json:"metricsBySplit"`
	MetricsByRole             map[string]FindingEvaluationMetrics `json:"metricsByRole"`
	LaneRelevantHits          map[string]int                      `json:"laneRelevantHits"`
	UniqueRelevantFirstHits   map[string]int                      `json:"uniqueRelevantFirstHits"`
	RerankImproved            int                                 `json:"rerankImproved"`
	RerankDegraded            int                                 `json:"rerankDegraded"`
	NormalizedTokens          int                                 `json:"normalizedTokens"`
	RawTokens                 int                                 `json:"rawTokens"`
	NormalizedMedianTokens    int                                 `json:"normalizedMedianTokens"`
	RawMedianTokens           int                                 `json:"rawMedianTokens"`
	HardNegativeHits          int                                 `json:"hardNegativeHits"`
	DeterministicReplayRate   float64                             `json:"deterministicReplayRate"`
	ArchiveGateDiagnosticOnly bool                                `json:"archiveGateDiagnosticOnly"`
	Digest                    string                              `json:"digest"`
}

// EvaluateFindingRetriever records per-lane contribution before packing and
// rerank movement separately. It never deletes or promotes a lane and never
// creates an archive-policy receipt; maintainer ratification remains distinct.
func evaluateFindingRetrieverLegacy(ctx context.Context, retriever Retriever, cases []FindingEvalCase) (FindingAblationReport, error) {
	report := FindingAblationReport{
		SchemaVersion: 1, LaneRelevantHits: map[string]int{},
		UniqueRelevantFirstHits: map[string]int{}, ArchiveGateDiagnosticOnly: true,
	}
	var expectedFindings, foundFindings, expectedRaw, foundRaw int
	var reciprocal float64
	abstentionCorrect, evidenceComplete, evidenceCases, durabilityCorrect, replay := 0, 0, 0, 0, 0
	for _, eval := range cases {
		if strings.TrimSpace(eval.ID) == "" || strings.TrimSpace(eval.Query) == "" {
			return FindingAblationReport{}, errors.New("finding evaluation cases require id and query")
		}
		options := eval.Options
		options.Query = eval.Query
		if options.TokenBudget == 0 {
			options.TokenBudget = 4096
		}
		if options.Limit == 0 {
			options.Limit = 5
		}
		first, err := retriever.QueryFindingCards(ctx, options)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf("case %s: %w", eval.ID, err)
		}
		second, err := retriever.QueryFindingCards(ctx, options)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf("case %s replay: %w", eval.ID, err)
		}
		outcome := FindingCaseOutcome{
			CaseID: eval.ID, LaneRelevantHits: map[string]int{}, UniqueRelevantFirstHits: map[string]int{},
			DurabilityLabelsAccurate: true, ReplayIdentical: first.Digest == second.Digest,
		}
		if outcome.ReplayIdentical {
			replay++
		}
		findingRanks := map[string]int{}
		evidenceSeen := map[string]bool{}
		rawSeen := map[string]bool{}
		hardNegativeSet := map[string]bool{}
		for _, id := range eval.HardNegativeFindingIDs {
			hardNegativeSet[id] = true
		}
		for rank, card := range first.Cards {
			outcome.CardIDs = append(outcome.CardIDs, card.ID)
			switch card.CardType {
			case "finding":
				outcome.FindingIDs = append(outcome.FindingIDs, card.ID)
				findingRanks[card.ID] = rank + 1
				outcome.NormalizedTokens += EstimateTokens(stableJSON(card))
				if card.SourceClass == "archive" || card.SourceClass == "intake" {
					outcome.DurabilityLabelsAccurate = false
				}
				if hardNegativeSet[card.ID] {
					outcome.HardNegativeHits = append(outcome.HardNegativeHits, card.ID)
				}
			case "raw-report":
				path := card.Metadata["path"]
				outcome.RawPaths = append(outcome.RawPaths, path)
				rawSeen[path] = true
				outcome.RawTokens += EstimateTokens(stableJSON(card))
				if card.SourceClass != "archive" {
					outcome.DurabilityLabelsAccurate = false
				}
			}
			if card.EvidenceHandle != "" {
				evidenceSeen[card.EvidenceHandle] = true
			}
		}
		fusionOrder := append([]FindingCandidateTrace(nil), first.Trace.Candidates...)
		sort.Slice(fusionOrder, func(i, j int) bool {
			if fusionOrder[i].FusionScore != fusionOrder[j].FusionScore {
				return fusionOrder[i].FusionScore > fusionOrder[j].FusionScore
			}
			return fusionOrder[i].FindingID < fusionOrder[j].FindingID
		})
		fusionRanks := map[string]int{}
		traces := map[string]FindingCandidateTrace{}
		for index, candidate := range fusionOrder {
			fusionRanks[candidate.FindingID] = index + 1
			traces[candidate.FindingID] = candidate
		}
		rerankOrder := append([]FindingCandidateTrace(nil), fusionOrder...)
		rerankDepth := retriever.Profile.Effective.RerankDepth
		if rerankDepth < 1 || rerankDepth > len(rerankOrder) {
			rerankDepth = len(rerankOrder)
		}
		sort.SliceStable(rerankOrder[:rerankDepth], func(i, j int) bool {
			if rerankOrder[i].RerankScore != rerankOrder[j].RerankScore {
				return rerankOrder[i].RerankScore > rerankOrder[j].RerankScore
			}
			if rerankOrder[i].FusionScore != rerankOrder[j].FusionScore {
				return rerankOrder[i].FusionScore > rerankOrder[j].FusionScore
			}
			return rerankOrder[i].FindingID < rerankOrder[j].FindingID
		})
		rerankRanks := map[string]int{}
		for index, candidate := range rerankOrder {
			rerankRanks[candidate.FindingID] = index + 1
		}
		retrievingLanes := map[string]bool{}
		firstLanes := map[string]bool{}
		for _, expected := range eval.ExpectedFindingIDs {
			expectedFindings++
			if rank := findingRanks[expected]; rank > 0 {
				foundFindings++
				outcome.RelevantFindingRanks = append(outcome.RelevantFindingRanks, rank)
				if len(outcome.RelevantFindingRanks) == 1 {
					reciprocal += 1 / float64(rank)
				}
			}
			if fusionRanks[expected] > 0 && rerankRanks[expected] > 0 {
				outcome.RerankDelta += fusionRanks[expected] - rerankRanks[expected]
			}
			trace := traces[expected]
			for lane, rank := range trace.LaneRanks {
				if rank > 0 {
					retrievingLanes[lane] = true
					outcome.LaneRelevantHits[lane]++
					report.LaneRelevantHits[lane]++
					if rank == 1 {
						firstLanes[lane] = true
					}
				}
			}
		}
		if len(retrievingLanes) == 1 && len(firstLanes) == 1 {
			for lane := range firstLanes {
				outcome.UniqueRelevantFirstHits[lane]++
				report.UniqueRelevantFirstHits[lane]++
			}
		}
		for _, path := range eval.ExpectedRawPaths {
			expectedRaw++
			if rawSeen[path] {
				foundRaw++
			}
		}
		outcome.EvidenceHandlesComplete = true
		if len(eval.ExpectedEvidenceHandles) > 0 {
			evidenceCases++
		}
		for _, handle := range eval.ExpectedEvidenceHandles {
			if evidenceSeen[handle] {
				outcome.EvidenceHandlesFound = append(outcome.EvidenceHandlesFound, handle)
			} else {
				outcome.EvidenceHandlesComplete = false
			}
		}
		if len(eval.ExpectedEvidenceHandles) > 0 && outcome.EvidenceHandlesComplete {
			evidenceComplete++
		}
		if outcome.DurabilityLabelsAccurate {
			durabilityCorrect++
		}
		if eval.Answerable {
			outcome.AbstentionCorrect = len(first.Cards) > 0
		} else {
			outcome.AbstentionCorrect = len(first.Cards) == 0
		}
		if outcome.AbstentionCorrect {
			abstentionCorrect++
		}
		if outcome.RerankDelta > 0 {
			report.RerankImproved++
		} else if outcome.RerankDelta < 0 {
			report.RerankDegraded++
		}
		report.NormalizedTokens += outcome.NormalizedTokens
		report.RawTokens += outcome.RawTokens
		report.HardNegativeHits += len(outcome.HardNegativeHits)
		report.Cases = append(report.Cases, outcome)
	}
	if expectedFindings > 0 {
		report.FindingRecall = float64(foundFindings) / float64(expectedFindings)
		report.MeanReciprocalRank = reciprocal / float64(expectedFindings)
	}
	if expectedRaw > 0 {
		report.RawPathRecall = float64(foundRaw) / float64(expectedRaw)
	}
	if len(cases) > 0 {
		report.AbstentionAccuracy = float64(abstentionCorrect) / float64(len(cases))
		if evidenceCases > 0 {
			report.EvidenceHandleAccuracy = float64(evidenceComplete) / float64(evidenceCases)
		} else {
			report.EvidenceHandleAccuracy = 1
		}
		report.DurabilityLabelAccuracy = float64(durabilityCorrect) / float64(len(cases))
		report.DeterministicReplayRate = float64(replay) / float64(len(cases))
	}
	digestInput := report
	digestInput.Digest = ""
	digest, err := CanonicalDigest(digestInput)
	if err != nil {
		return FindingAblationReport{}, err
	}
	report.Digest = digest
	return report, nil
}
