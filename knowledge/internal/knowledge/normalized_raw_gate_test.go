package knowledge

import (
	"fmt"
	"strings"
	"testing"
)

func normalizedRawGateFixture(
	t *testing.T,
	normalizedTokens, rawTokens int,
) (FindingEvalSuite, Generation, SelectedProfile, FindingAblationReport) {
	t.Helper()
	corpus := stateTestDigest("a")
	cases := make([]FindingEvalCase, 0, 64)
	outcomes := make([]FindingCaseOutcome, 0, 64)
	for index := 0; index < 64; index++ {
		split := "development"
		local := index
		if index >= 32 {
			split, local = "holdout", index-32
		}
		role := "manager"
		if index%2 == 1 {
			role = "drafter"
		}
		eval := FindingEvalCase{
			ID: fmt.Sprintf("paired-case-%02d", index), Role: role,
			Topic: fmt.Sprintf("%s-topic-%02d", split, local), Split: split,
			Query:                 fmt.Sprintf("bounded paired retrieval question %02d", index),
			QueryClass:            "conceptual",
			AllowedSourceClasses:  []string{"campaign", "provisional", "truth"},
			AllowedReviewStates:   []string{"curator-checked", "manager-ratified"},
			AllowedValidities:     []string{"challenged", "current", "superseded"},
			TokenBudget:           1024,
			ExpectedSourceClasses: map[string]string{},
			ExpectedReviewStates:  map[string]string{},
			ExpectedValidities:    map[string]string{},
		}
		outcome := FindingCaseOutcome{
			CaseID: eval.ID, Role: role, Topic: eval.Topic, Split: split,
			QueryClass: eval.QueryClass, Status: "ok",
			LaneRelevantHits: map[string]int{}, UniqueRelevantFirstHits: map[string]int{},
			NormalizedTokens: normalizedTokens, RawTokens: rawTokens,
			AbstentionCorrect: true, EvidenceHandlesComplete: true,
			FindingHandlesComplete: true, SourceClassesAccurate: true,
			ReviewStatesAccurate: true, ValiditiesAccurate: true,
			ClaimVocabularyDisjoint: true, DurabilityLabelsAccurate: true,
			ReplayIdentical: true,
		}
		if local >= 4 {
			findingID := fmt.Sprintf("F-%04d", index+1)
			hardNegativeID := fmt.Sprintf("F-%04d", index+1001)
			rawPath := fmt.Sprintf(
				"active/%s-campaign/runs/R-20260802-%04d/report.md", split, index+1)
			sourceClasses := []string{"truth", "campaign", "provisional"}
			reviewStates := []string{"manager-ratified", "curator-checked"}
			validities := []string{"current", "challenged", "superseded"}
			eval.ExpectedFindingIDs = []string{findingID}
			eval.ExpectedFindingHandles = []string{FindingHandle(findingID)}
			eval.ExpectedRawPaths = []string{rawPath}
			eval.ExpectedSourceClasses[findingID] = sourceClasses[index%len(sourceClasses)]
			eval.ExpectedReviewStates[findingID] = reviewStates[index%len(reviewStates)]
			eval.ExpectedValidities[findingID] = validities[index%len(validities)]
			eval.HardNegativeFindingIDs = []string{hardNegativeID}
			eval.Answerable = true
			outcome.CardIDs = []string{findingID}
			outcome.FindingIDs = []string{findingID}
			outcome.RawPaths = []string{rawPath}
			outcome.RelevantFindingRanks = []int{1}
			outcome.VocabularyDisjointApplicable = role == "manager"
		} else {
			outcome.NormalizedTokens, outcome.RawTokens = 0, 0
		}
		cases = append(cases, eval)
		outcomes = append(outcomes, outcome)
	}
	suite := FindingEvalSuite{
		SchemaVersion: FindingEvalSuiteVersion, ID: "normalized-raw-suite",
		Status: "ratified", RatifiedAt: "2026-08-02T20:00:00Z",
		RatifiedBy: "maintainer:test", CorpusSnapshot: corpus, Cases: cases,
	}
	var err error
	suite.Digest, err = FindingEvalSuiteDigest(suite)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFindingEvalSuite(suite); err != nil {
		t.Fatalf("paired gate fixture suite: %v", err)
	}
	report := FindingAblationReport{
		SchemaVersion: 1, SuiteID: suite.ID, SuiteDigest: suite.Digest,
		CorpusSnapshot: corpus, Cases: outcomes,
		LaneRelevantHits: map[string]int{}, UniqueRelevantFirstHits: map[string]int{},
		MetricsBySplit:            map[string]FindingEvaluationMetrics{},
		MetricsByRole:             map[string]FindingEvaluationMetrics{},
		ArchiveGateDiagnosticOnly: true,
	}
	overall := findingMetrics(outcomes, cases)
	report.FindingRecall, report.MeanReciprocalRank = overall.FindingRecall, overall.MeanReciprocalRank
	report.RawPathRecall, report.AbstentionAccuracy = overall.RawPathRecall, overall.AbstentionAccuracy
	report.FindingHandleAccuracy, report.EvidenceHandleAccuracy = overall.FindingHandleAccuracy, overall.EvidenceHandleAccuracy
	report.SourceClassAccuracy, report.ReviewStateAccuracy = overall.SourceClassAccuracy, overall.ReviewStateAccuracy
	report.ValidityAccuracy, report.VocabularyDisjointRate = overall.ValidityAccuracy, overall.VocabularyDisjointRate
	report.DeterministicReplayRate = overall.DeterministicReplayRate
	report.NormalizedMedianTokens, report.RawMedianTokens = overall.NormalizedMedianTokens, overall.RawMedianTokens
	report.NormalizedMedianEvidenceExpansionTokens = overall.NormalizedMedianEvidenceExpansionTokens
	report.RawMedianDocumentExpansionTokens = overall.RawMedianDocumentExpansionTokens
	report.DurabilityLabelAccuracy = 1
	for _, outcome := range outcomes {
		report.NormalizedTokens += outcome.NormalizedTokens
		report.RawTokens += outcome.RawTokens
	}
	for _, split := range []string{"development", "holdout"} {
		rows, subset := filterFindingCases(outcomes, cases, func(eval FindingEvalCase) bool {
			return eval.Split == split
		})
		report.MetricsBySplit[split] = findingMetrics(rows, subset)
	}
	for _, role := range []string{"manager", "drafter"} {
		rows, subset := filterFindingCases(outcomes, cases, func(eval FindingEvalCase) bool {
			return eval.Role == role
		})
		report.MetricsByRole[role] = findingMetrics(rows, subset)
	}
	report, err = sealFindingAblationReport(report)
	if err != nil {
		t.Fatal(err)
	}
	generation := Generation{
		ID: "generation-" + strings.Repeat("b", 20), CorpusFingerprint: corpus,
		ModelFingerprint: stateTestDigest("c"), ParserVersion: ParserVersion,
		ChunkerVersion: ChunkerVersion, CreatedAt: "2026-08-02T20:01:00Z",
	}
	selected := SelectedProfile{
		EffectiveIdentity: "project:hybrid-no-rerank-v1@" + stateTestDigest("d"),
		Effective:         EffectiveProfile{Name: "hybrid-no-rerank-v1"},
		ActiveLanes:       []string{"exact", "fts", "graph", "dense"},
	}
	return suite, generation, selected, report
}

func TestNormalizedRawGateCandidateIsNonAuthoritativeAndStrict(t *testing.T) {
	suite, generation, selected, evaluation := normalizedRawGateFixture(t, 100, 200)
	report, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Checks.AllPassed || report.Decision.Outcome != "passed" ||
		!report.Decision.PromotionEligible || report.Decision.AuthorizationReceipt ||
		report.Decision.CurrentArchiveMode != "default-fallback" ||
		report.Decision.RequiredNextAction != "explicitly-ratify-opt-in-receipt" {
		t.Fatalf("passing candidate crossed or obscured the authority boundary: %#v", report.Decision)
	}
	if len(report.Questions) != 64 || !sha256ValueRE.MatchString(report.QuestionsDigest) ||
		!sha256ValueRE.MatchString(report.ContractDigest) || !sha256ValueRE.MatchString(report.Digest) {
		t.Fatalf("candidate omitted its exact 64-case contract binding: %#v", report)
	}
}

func TestNormalizedRawGateRetainsDefaultWhenTokenGateFails(t *testing.T) {
	suite, generation, selected, evaluation := normalizedRawGateFixture(t, 250, 200)
	report, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks.AllPassed || report.Checks.LowerTokenCostOverall ||
		report.Decision.Outcome != "retain-default-fallback" ||
		report.Decision.PromotionEligible || report.Decision.AuthorizationReceipt ||
		!containsString(report.Decision.FailedChecks, "lower-token-cost-overall") {
		t.Fatalf("inferior normalized cost did not retain raw fallback: %#v", report)
	}
}

func TestNormalizedRawGateRejectsMedianWinWithWorseTotalCost(t *testing.T) {
	suite, generation, selected, evaluation := normalizedRawGateFixture(t, 100, 200)
	// One outlier leaves the overall, split, and role medians lower while
	// making normalized total cost worse. A release gate must not conceal that
	// regression behind the median.
	evaluation.Cases[4].NormalizedTokens = 10000
	evaluation.NormalizedTokens, evaluation.RawTokens = 0, 0
	for _, outcome := range evaluation.Cases {
		evaluation.NormalizedTokens += outcome.NormalizedTokens
		evaluation.RawTokens += outcome.RawTokens
	}
	overall := findingMetrics(evaluation.Cases, suite.Cases)
	evaluation.NormalizedMedianTokens = overall.NormalizedMedianTokens
	evaluation.RawMedianTokens = overall.RawMedianTokens
	for _, split := range []string{"development", "holdout"} {
		rows, subset := filterFindingCases(evaluation.Cases, suite.Cases, func(eval FindingEvalCase) bool {
			return eval.Split == split
		})
		evaluation.MetricsBySplit[split] = findingMetrics(rows, subset)
	}
	for _, role := range []string{"manager", "drafter"} {
		rows, subset := filterFindingCases(evaluation.Cases, suite.Cases, func(eval FindingEvalCase) bool {
			return eval.Role == role
		})
		evaluation.MetricsByRole[role] = findingMetrics(rows, subset)
	}
	var err error
	evaluation, err = sealFindingAblationReport(evaluation)
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks.AllPassed || report.Checks.LowerTokenCostOverall ||
		report.Decision.Outcome != "retain-default-fallback" ||
		!containsString(report.Decision.FailedChecks, "lower-token-cost-overall") {
		t.Fatalf("worse total cost passed behind a lower median: %#v", report.Checks)
	}
}

func TestNormalizedRawGateRejectsStaleOrTamperedEvidence(t *testing.T) {
	suite, generation, selected, evaluation := normalizedRawGateFixture(t, 100, 200)
	tampered := evaluation
	tampered.RawMedianTokens++
	if _, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, tampered); err == nil {
		t.Fatal("tampered paired evaluation was accepted")
	}
	generation.CorpusFingerprint = stateTestDigest("e")
	if _, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation); err == nil {
		t.Fatal("stale suite corpus snapshot was accepted")
	}
}

func TestNormalizedRawGateRecomputesSealedEvaluationTotals(t *testing.T) {
	suite, generation, selected, evaluation := normalizedRawGateFixture(t, 100, 200)
	evaluation.RawTokens++
	var err error
	evaluation, err = sealFindingAblationReport(evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation); err == nil {
		t.Fatal("self-consistently resealed but false evaluation totals were accepted")
	}
}

func TestFindingEvalQueryContractCarriesItsDeclaredQueryClass(t *testing.T) {
	eval := FindingEvalCase{Query: "typed query", QueryClass: "contradiction"}
	if got := eval.queryOptions().QueryClass; got != "contradiction" {
		t.Fatalf("finding evaluation silently ran query class %q", got)
	}
}
