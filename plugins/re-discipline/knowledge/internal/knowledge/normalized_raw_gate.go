package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

const NormalizedRawGateReportVersion = 1

type NormalizedRawQuestionBinding struct {
	CaseID         string `json:"caseId"`
	Role           string `json:"role"`
	Topic          string `json:"topic"`
	Split          string `json:"split"`
	QueryClass     string `json:"queryClass"`
	QueryDigest    string `json:"queryDigest"`
	ContractDigest string `json:"contractDigest"`
	TokenBudget    int    `json:"tokenBudget"`
	CardLimit      int    `json:"cardLimit"`
}

type NormalizedRawGateChecks struct {
	Exactly64Cases             bool `json:"exactly64Cases"`
	FreshCorpusBinding         bool `json:"freshCorpusBinding"`
	IdenticalQuestionContracts bool `json:"identicalQuestionContracts"`
	OverallRecallNonInferior   bool `json:"overallRecallNonInferior"`
	SplitRecallNonInferior     bool `json:"splitRecallNonInferior"`
	RoleRecallNonInferior      bool `json:"roleRecallNonInferior"`
	CompleteKnownRecall        bool `json:"completeKnownRecall"`
	AbstentionExact            bool `json:"abstentionExact"`
	FindingHandlesExact        bool `json:"findingHandlesExact"`
	EvidenceHandlesExact       bool `json:"evidenceHandlesExact"`
	SourceClassesExact         bool `json:"sourceClassesExact"`
	ReviewStatesExact          bool `json:"reviewStatesExact"`
	ValiditiesExact            bool `json:"validitiesExact"`
	VocabularyDisjointExact    bool `json:"vocabularyDisjointExact"`
	DurabilityLabelsExact      bool `json:"durabilityLabelsExact"`
	HardNegativesZero          bool `json:"hardNegativesZero"`
	DeterministicReplayExact   bool `json:"deterministicReplayExact"`
	LowerTokenCostOverall      bool `json:"lowerTokenCostOverall"`
	LowerTokenCostBySplit      bool `json:"lowerTokenCostBySplit"`
	LowerTokenCostByRole       bool `json:"lowerTokenCostByRole"`
	AllPassed                  bool `json:"allPassed"`
}

type NormalizedRawGateDecision struct {
	Outcome              string   `json:"outcome"`
	PromotionEligible    bool     `json:"promotionEligible"`
	CurrentArchiveMode   string   `json:"currentArchiveMode"`
	RequiredNextAction   string   `json:"requiredNextAction"`
	FailedChecks         []string `json:"failedChecks"`
	AuthorizationReceipt bool     `json:"authorizationReceipt"`
}

// NormalizedRawGateReport is measurement evidence, never archive-policy
// authority. A passing report only makes a later explicit receipt decision
// eligible; it cannot switch raw provenance out of the default fallback lane.
type NormalizedRawGateReport struct {
	SchemaVersion    int                            `json:"schemaVersion"`
	Kind             string                         `json:"kind"`
	RunID            string                         `json:"runId"`
	NonAuthoritative bool                           `json:"nonAuthoritative"`
	EvaluatedAt      string                         `json:"evaluatedAt"`
	SuiteID          string                         `json:"suiteId"`
	SuiteDigest      string                         `json:"suiteDigest"`
	QuestionsDigest  string                         `json:"questionsDigest"`
	ContractDigest   string                         `json:"contractDigest"`
	CaseCount        int                            `json:"caseCount"`
	ProfileName      string                         `json:"profileName"`
	EffectiveProfile string                         `json:"effectiveProfile"`
	ActiveLanes      []string                       `json:"activeLanes"`
	Generation       ContextGenerationIdentity      `json:"generation"`
	Binding          ArchiveFallbackBinding         `json:"binding"`
	Questions        []NormalizedRawQuestionBinding `json:"questions"`
	PairedEvaluation FindingAblationReport          `json:"pairedEvaluation"`
	Checks           NormalizedRawGateChecks        `json:"checks"`
	Decision         NormalizedRawGateDecision      `json:"decision"`
	Digest           string                         `json:"digest"`
}

type NormalizedRawGateRunResult struct {
	Report              NormalizedRawGateReport `json:"report"`
	ReportPath          string                  `json:"reportPath"`
	ReportDigest        string                  `json:"reportDigest"`
	ReportContentDigest string                  `json:"reportContentDigest"`
}

func normalizedRawQuestionBindings(
	cases []FindingEvalCase,
) ([]NormalizedRawQuestionBinding, string, error) {
	bindings := make([]NormalizedRawQuestionBinding, 0, len(cases))
	for _, eval := range cases {
		options := eval.queryOptions()
		queryDigest := "sha256:" + SHA256String(eval.Query)
		contractDigest, err := CanonicalDigest(struct {
			CaseID                 string   `json:"caseId"`
			Query                  string   `json:"query"`
			QueryClass             string   `json:"queryClass"`
			CampaignID             string   `json:"campaignId"`
			AllowedSourceClasses   []string `json:"allowedSourceClasses"`
			AllowedProvenanceTiers []string `json:"allowedProvenanceTiers"`
			AllowedReviewStates    []string `json:"allowedReviewStates"`
			AllowedValidities      []string `json:"allowedValidities"`
			TokenBudget            int      `json:"tokenBudget"`
			CardLimit              int      `json:"cardLimit"`
		}{
			CaseID: eval.ID, Query: eval.Query, QueryClass: options.QueryClass,
			CampaignID:             options.CampaignID,
			AllowedSourceClasses:   append([]string(nil), options.AllowedSourceClasses...),
			AllowedProvenanceTiers: append([]string(nil), options.AllowedProvenanceTiers...),
			AllowedReviewStates:    append([]string(nil), options.AllowedReviewStates...),
			AllowedValidities:      append([]string(nil), options.AllowedValidities...),
			TokenBudget:            options.TokenBudget, CardLimit: options.Limit,
		})
		if err != nil {
			return nil, "", err
		}
		bindings = append(bindings, NormalizedRawQuestionBinding{
			CaseID: eval.ID, Role: eval.Role, Topic: eval.Topic, Split: eval.Split,
			QueryClass: eval.QueryClass, QueryDigest: queryDigest,
			ContractDigest: contractDigest, TokenBudget: options.TokenBudget,
			CardLimit: options.Limit,
		})
	}
	digest, err := CanonicalDigest(bindings)
	return bindings, digest, err
}

func validatePairedFindingEvaluation(
	suite FindingEvalSuite,
	report FindingAblationReport,
) error {
	if report.SchemaVersion != 1 || report.SuiteID != suite.ID || report.SuiteDigest != suite.Digest ||
		report.CorpusSnapshot != suite.CorpusSnapshot || !report.ArchiveGateDiagnosticOnly ||
		len(report.Cases) != len(suite.Cases) {
		return errors.New("paired finding evaluation does not bind the exact diagnostic suite")
	}
	sealed, err := sealFindingAblationReport(report)
	if err != nil || sealed.Digest != report.Digest {
		return errors.New("paired finding evaluation digest does not verify")
	}
	byID := make(map[string]FindingCaseOutcome, len(report.Cases))
	normalizedTokens, rawTokens := 0, 0
	normalizedExpansionTokens, rawExpansionTokens := 0, 0
	durable, hardNegativeHits := 0, 0
	laneRelevantHits := map[string]int{}
	uniqueRelevantFirstHits := map[string]int{}
	for _, outcome := range report.Cases {
		if _, exists := byID[outcome.CaseID]; exists {
			return fmt.Errorf("paired finding evaluation repeats case %s", outcome.CaseID)
		}
		byID[outcome.CaseID] = outcome
		normalizedTokens += outcome.NormalizedTokens
		rawTokens += outcome.RawTokens
		normalizedExpansionTokens += outcome.NormalizedEvidenceExpansionTokens
		rawExpansionTokens += outcome.RawDocumentExpansionTokens
		hardNegativeHits += len(outcome.HardNegativeHits)
		if outcome.DurabilityLabelsAccurate {
			durable++
		}
		for lane, hits := range outcome.LaneRelevantHits {
			laneRelevantHits[lane] += hits
		}
		for lane, hits := range outcome.UniqueRelevantFirstHits {
			uniqueRelevantFirstHits[lane] += hits
		}
	}
	for _, eval := range suite.Cases {
		outcome, exists := byID[eval.ID]
		if !exists || outcome.Role != eval.Role || outcome.Topic != eval.Topic ||
			outcome.Split != eval.Split || outcome.QueryClass != eval.QueryClass {
			return fmt.Errorf("paired finding evaluation case %s changed identity", eval.ID)
		}
	}
	overall := findingMetrics(report.Cases, suite.Cases)
	if report.FindingRecall != overall.FindingRecall ||
		report.MeanReciprocalRank != overall.MeanReciprocalRank ||
		report.RawPathRecall != overall.RawPathRecall ||
		report.AbstentionAccuracy != overall.AbstentionAccuracy ||
		report.FindingHandleAccuracy != overall.FindingHandleAccuracy ||
		report.EvidenceHandleAccuracy != overall.EvidenceHandleAccuracy ||
		report.SourceClassAccuracy != overall.SourceClassAccuracy ||
		report.ReviewStateAccuracy != overall.ReviewStateAccuracy ||
		report.ValidityAccuracy != overall.ValidityAccuracy ||
		report.VocabularyDisjointRate != overall.VocabularyDisjointRate ||
		report.HardNegativeHits != overall.HardNegativeHits ||
		report.DeterministicReplayRate != overall.DeterministicReplayRate ||
		report.NormalizedMedianTokens != overall.NormalizedMedianTokens ||
		report.RawMedianTokens != overall.RawMedianTokens ||
		report.NormalizedMedianEvidenceExpansionTokens != overall.NormalizedMedianEvidenceExpansionTokens ||
		report.RawMedianDocumentExpansionTokens != overall.RawMedianDocumentExpansionTokens ||
		report.NormalizedTokens != normalizedTokens || report.RawTokens != rawTokens ||
		report.NormalizedEvidenceExpansionTokens != normalizedExpansionTokens ||
		report.RawDocumentExpansionTokens != rawExpansionTokens ||
		report.HardNegativeHits != hardNegativeHits ||
		report.DurabilityLabelAccuracy != safeRatio(durable, len(report.Cases), 1) ||
		!reflect.DeepEqual(report.LaneRelevantHits, laneRelevantHits) ||
		!reflect.DeepEqual(report.UniqueRelevantFirstHits, uniqueRelevantFirstHits) {
		return errors.New("paired finding evaluation aggregate metrics do not recompute")
	}
	for _, split := range []string{"development", "holdout"} {
		outcomes, cases := filterFindingCases(report.Cases, suite.Cases, func(eval FindingEvalCase) bool {
			return eval.Split == split
		})
		if !reflect.DeepEqual(report.MetricsBySplit[split], findingMetrics(outcomes, cases)) {
			return fmt.Errorf("paired finding evaluation %s metrics do not recompute", split)
		}
	}
	for _, role := range []string{"manager", "drafter"} {
		outcomes, cases := filterFindingCases(report.Cases, suite.Cases, func(eval FindingEvalCase) bool {
			return eval.Role == role
		})
		if !reflect.DeepEqual(report.MetricsByRole[role], findingMetrics(outcomes, cases)) {
			return fmt.Errorf("paired finding evaluation %s metrics do not recompute", role)
		}
	}
	return nil
}

func normalizedRawSlicesPass(
	metrics map[string]FindingEvaluationMetrics,
	check func(FindingEvaluationMetrics) bool,
) bool {
	for _, value := range metrics {
		if value.CaseCount < 1 || !check(value) {
			return false
		}
	}
	return len(metrics) == 2
}

func normalizedRawTotalCostLower(
	outcomes []FindingCaseOutcome,
	cases []FindingEvalCase,
	keep func(FindingEvalCase) bool,
) bool {
	byID := make(map[string]FindingCaseOutcome, len(outcomes))
	for _, outcome := range outcomes {
		byID[outcome.CaseID] = outcome
	}
	normalized, raw, measured := 0, 0, 0
	for _, eval := range cases {
		if !keep(eval) || len(eval.ExpectedFindingIDs) == 0 || len(eval.ExpectedRawPaths) == 0 {
			continue
		}
		outcome, present := byID[eval.ID]
		if !present || outcome.NormalizedTokens <= 0 || outcome.RawTokens <= 0 {
			return false
		}
		normalized += outcome.NormalizedTokens
		raw += outcome.RawTokens
		measured++
	}
	return measured > 0 && normalized < raw
}

func normalizedRawSliceTotalsLower(
	outcomes []FindingCaseOutcome,
	cases []FindingEvalCase,
	values []string,
	key func(FindingEvalCase) string,
) bool {
	for _, value := range values {
		if !normalizedRawTotalCostLower(outcomes, cases, func(eval FindingEvalCase) bool {
			return key(eval) == value
		}) {
			return false
		}
	}
	return true
}

func buildNormalizedRawGateReport(
	evaluatedAt string,
	suite FindingEvalSuite,
	generation Generation,
	selected SelectedProfile,
	evaluation FindingAblationReport,
) (NormalizedRawGateReport, error) {
	return buildNormalizedRawGateReportForRun(
		nowRunID("normalized-vs-raw"), evaluatedAt, suite, generation, selected, evaluation,
	)
}

// buildNormalizedRawGateReportForRun is the deterministic report compiler
// used by the opt-in authorization boundary. The ordinary measurement path
// chooses a fresh run ID; ratification supplies the retained candidate's ID
// and independently recompiles every other field from the current suite,
// generation, profile, and replayed paired outcomes.
func buildNormalizedRawGateReportForRun(
	runID string,
	evaluatedAt string,
	suite FindingEvalSuite,
	generation Generation,
	selected SelectedProfile,
	evaluation FindingAblationReport,
) (NormalizedRawGateReport, error) {
	if !validNormalizedRawRunID(runID) {
		return NormalizedRawGateReport{}, errors.New(
			"normalized-vs-raw run id is invalid")
	}
	when, err := time.Parse(time.RFC3339Nano, evaluatedAt)
	if err != nil || when.Location() != time.UTC {
		return NormalizedRawGateReport{}, errors.New("normalized-vs-raw evaluatedAt must be UTC RFC3339")
	}
	if err := ValidateFindingEvalSuite(suite); err != nil {
		return NormalizedRawGateReport{}, err
	}
	if len(suite.Cases) != 64 {
		return NormalizedRawGateReport{}, fmt.Errorf(
			"normalized-vs-raw gate requires exactly 64 ratified cases; got %d", len(suite.Cases))
	}
	if suite.CorpusSnapshot != generation.CorpusFingerprint {
		return NormalizedRawGateReport{}, errors.New(
			"normalized-vs-raw suite does not bind the pinned generation corpus")
	}
	if err := validatePairedFindingEvaluation(suite, evaluation); err != nil {
		return NormalizedRawGateReport{}, err
	}
	retriever := Retriever{Generation: generation, Profile: selected}
	binding, err := retriever.archiveFallbackBinding()
	if err != nil {
		return NormalizedRawGateReport{}, err
	}
	questions, questionsDigest, err := normalizedRawQuestionBindings(suite.Cases)
	if err != nil {
		return NormalizedRawGateReport{}, err
	}
	contractDigest, err := CanonicalDigest(struct {
		SuiteDigest     string                 `json:"suiteDigest"`
		QuestionsDigest string                 `json:"questionsDigest"`
		Binding         ArchiveFallbackBinding `json:"binding"`
		GenerationID    string                 `json:"generationId"`
		NormalizedArm   string                 `json:"normalizedArm"`
		RawArm          string                 `json:"rawArm"`
	}{
		SuiteDigest: suite.Digest, QuestionsDigest: questionsDigest, Binding: binding,
		GenerationID: generation.ID, NormalizedArm: "finding-only-same-budget-v1",
		RawArm: "raw-report-only-same-budget-v1",
	})
	if err != nil {
		return NormalizedRawGateReport{}, err
	}
	nonInferior := func(value FindingEvaluationMetrics) bool {
		return value.FindingRecall >= value.RawPathRecall
	}
	lowerTokens := func(value FindingEvaluationMetrics) bool {
		return value.NormalizedMedianTokens > 0 && value.RawMedianTokens > 0 &&
			value.NormalizedMedianTokens < value.RawMedianTokens
	}
	overallTotalLower := normalizedRawTotalCostLower(
		evaluation.Cases, suite.Cases, func(FindingEvalCase) bool { return true })
	splitTotalsLower := normalizedRawSliceTotalsLower(
		evaluation.Cases, suite.Cases, []string{"development", "holdout"},
		func(eval FindingEvalCase) string { return eval.Split })
	roleTotalsLower := normalizedRawSliceTotalsLower(
		evaluation.Cases, suite.Cases, []string{"manager", "drafter"},
		func(eval FindingEvalCase) string { return eval.Role })
	checks := NormalizedRawGateChecks{
		Exactly64Cases: true,
		FreshCorpusBinding: !generation.ServingStale &&
			suite.CorpusSnapshot == generation.CorpusFingerprint,
		IdenticalQuestionContracts: len(questions) == 64 &&
			sha256ValueRE.MatchString(questionsDigest) && selected.EffectiveIdentity == binding.ProfileIdentity,
		OverallRecallNonInferior: evaluation.FindingRecall >= evaluation.RawPathRecall,
		SplitRecallNonInferior:   normalizedRawSlicesPass(evaluation.MetricsBySplit, nonInferior),
		RoleRecallNonInferior:    normalizedRawSlicesPass(evaluation.MetricsByRole, nonInferior),
		CompleteKnownRecall:      evaluation.FindingRecall == 1 && evaluation.RawPathRecall == 1,
		AbstentionExact:          evaluation.AbstentionAccuracy == 1,
		FindingHandlesExact:      evaluation.FindingHandleAccuracy == 1,
		EvidenceHandlesExact:     evaluation.EvidenceHandleAccuracy == 1,
		SourceClassesExact:       evaluation.SourceClassAccuracy == 1,
		ReviewStatesExact:        evaluation.ReviewStateAccuracy == 1,
		ValiditiesExact:          evaluation.ValidityAccuracy == 1,
		VocabularyDisjointExact:  evaluation.VocabularyDisjointRate == 1,
		DurabilityLabelsExact:    evaluation.DurabilityLabelAccuracy == 1,
		HardNegativesZero:        evaluation.HardNegativeHits == 0,
		DeterministicReplayExact: evaluation.DeterministicReplayRate == 1,
		LowerTokenCostOverall: overallTotalLower && evaluation.NormalizedMedianTokens > 0 &&
			evaluation.RawMedianTokens > 0 && evaluation.NormalizedMedianTokens < evaluation.RawMedianTokens,
		LowerTokenCostBySplit: splitTotalsLower &&
			normalizedRawSlicesPass(evaluation.MetricsBySplit, lowerTokens),
		LowerTokenCostByRole: roleTotalsLower &&
			normalizedRawSlicesPass(evaluation.MetricsByRole, lowerTokens),
	}
	checkRows := map[string]bool{
		"exactly-64-cases":             checks.Exactly64Cases,
		"fresh-corpus-binding":         checks.FreshCorpusBinding,
		"identical-question-contracts": checks.IdenticalQuestionContracts,
		"overall-recall-non-inferior":  checks.OverallRecallNonInferior,
		"split-recall-non-inferior":    checks.SplitRecallNonInferior,
		"role-recall-non-inferior":     checks.RoleRecallNonInferior,
		"complete-known-recall":        checks.CompleteKnownRecall,
		"abstention-exact":             checks.AbstentionExact,
		"finding-handles-exact":        checks.FindingHandlesExact,
		"evidence-handles-exact":       checks.EvidenceHandlesExact,
		"source-classes-exact":         checks.SourceClassesExact,
		"review-states-exact":          checks.ReviewStatesExact,
		"validities-exact":             checks.ValiditiesExact,
		"vocabulary-disjoint-exact":    checks.VocabularyDisjointExact,
		"durability-labels-exact":      checks.DurabilityLabelsExact,
		"hard-negatives-zero":          checks.HardNegativesZero,
		"deterministic-replay-exact":   checks.DeterministicReplayExact,
		"lower-token-cost-overall":     checks.LowerTokenCostOverall,
		"lower-token-cost-by-split":    checks.LowerTokenCostBySplit,
		"lower-token-cost-by-role":     checks.LowerTokenCostByRole,
	}
	failed := []string{}
	for name, passed := range checkRows {
		if !passed {
			failed = append(failed, name)
		}
	}
	sort.Strings(failed)
	checks.AllPassed = len(failed) == 0
	decision := NormalizedRawGateDecision{
		Outcome: "retain-default-fallback", PromotionEligible: checks.AllPassed,
		CurrentArchiveMode: "default-fallback", RequiredNextAction: "retain-default-fallback",
		FailedChecks: failed, AuthorizationReceipt: false,
	}
	if checks.AllPassed {
		decision.Outcome = "passed"
		decision.RequiredNextAction = "explicitly-ratify-opt-in-receipt"
	}
	report := NormalizedRawGateReport{
		SchemaVersion: NormalizedRawGateReportVersion,
		Kind:          "normalized-vs-raw-candidate", RunID: runID,
		NonAuthoritative: true, EvaluatedAt: evaluatedAt,
		SuiteID: suite.ID, SuiteDigest: suite.Digest,
		QuestionsDigest: questionsDigest, ContractDigest: contractDigest,
		CaseCount: len(suite.Cases), ProfileName: selected.Effective.Name,
		EffectiveProfile: selected.EffectiveIdentity,
		ActiveLanes:      append([]string(nil), selected.ActiveLanes...),
		Generation:       CompactContextGeneration(generation), Binding: binding,
		Questions: questions, PairedEvaluation: evaluation,
		Checks: checks, Decision: decision,
	}
	report.Digest, err = CanonicalDigest(report)
	return report, err
}

// RunNormalizedRawGate evaluates one ratified 64-case suite on one leased,
// pinned generation and persists only to the derived cache. It never writes
// project state, project measurements, retrieval policy, or an authorization
// receipt.
func (service *Service) RunNormalizedRawGate(
	ctx context.Context,
) (NormalizedRawGateRunResult, error) {
	if service == nil {
		return NormalizedRawGateRunResult{}, errors.New("service is required")
	}
	refreshed, err := NewService(ServiceOptions{
		ProjectRoot: service.Boundary.Root, AssetRoot: service.AssetRoot,
		CacheRoot: service.Index.CacheRoot,
	})
	if err != nil {
		return NormalizedRawGateRunResult{}, fmt.Errorf(
			"revalidate normalized-vs-raw control plane: %w", err)
	}
	service = refreshed
	suites, err := service.loadProjectFindingEvalSuites()
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	if len(suites) != 1 || len(suites[0].Cases) != 64 {
		return NormalizedRawGateRunResult{}, errors.New(
			"normalized-vs-raw gate requires exactly one ratified 64-case finding suite")
	}
	generation, selected, lease, err := service.leaseMeasurementGeneration(
		ctx, "normalized-vs-raw")
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	defer lease.Release()
	service.PinGeneration(generation)
	if suites[0].CorpusSnapshot != generation.CorpusFingerprint {
		return NormalizedRawGateRunResult{}, errors.New(
			"normalized-vs-raw suite corpus snapshot is stale for the pinned generation")
	}
	evaluation, err := EvaluateFindingSuite(ctx, Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}, suites[0])
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	report, err := buildNormalizedRawGateReport(
		RFC3339UTC(time.Now()), suites[0], generation, selected, evaluation)
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	reportPath, err := containedOutputPath(
		service.Index.CacheRoot,
		filepath.Join("normalized-vs-raw", report.RunID, "report.json"),
	)
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	if err := AtomicWriteJSON(reportPath, report, 0o600); err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	reportBody, err := canonicalJSON(report)
	if err != nil {
		return NormalizedRawGateRunResult{}, err
	}
	return NormalizedRawGateRunResult{
		Report: report, ReportPath: filepath.ToSlash(reportPath),
		ReportDigest:        report.Digest,
		ReportContentDigest: "sha256:" + SHA256Bytes(reportBody),
	}, nil
}
