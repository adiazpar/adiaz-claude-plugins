package knowledge

import "fmt"

func splitFindingEvalSuites(
	suites []FindingEvalSuite,
) ([]FindingEvalCase, []FindingEvalCase, []string) {
	development, holdout := []FindingEvalCase{}, []FindingEvalCase{}
	digests := make([]string, 0, len(suites))
	for _, suite := range suites {
		digests = append(digests, suite.Digest)
		for _, eval := range suite.Cases {
			switch eval.Split {
			case "development":
				development = append(development, eval)
			case "holdout":
				holdout = append(holdout, eval)
			}
		}
	}
	return development, holdout, SortedUnique(digests)
}

func findingCalibrationWeightKey(weights map[string]int) string {
	// Finding retrieval has no graph lane. Caching across graph-only variants
	// reduces the 81-row calibration sweep to the 27 distinct finding rankers
	// without changing a measured response.
	return fmt.Sprintf("exact=%d/fts=%d/dense=%d",
		weights["exact"], weights["fts"], weights["dense"])
}

func findingCalibrationMetricsPassed(metrics FindingEvaluationMetrics) bool {
	return metrics.CaseCount > 0 && metrics.FindingRecall == 1 &&
		metrics.MeanReciprocalRank == 1 && metrics.RawPathRecall == 1 &&
		metrics.AbstentionAccuracy == 1 && metrics.FindingHandleAccuracy == 1 &&
		metrics.EvidenceHandleAccuracy == 1 && metrics.SourceClassAccuracy == 1 &&
		metrics.ReviewStateAccuracy == 1 && metrics.ValidityAccuracy == 1 &&
		metrics.VocabularyDisjointRate == 1 && metrics.HardNegativeHits == 0 &&
		metrics.DeterministicReplayRate == 1 && metrics.NormalizedMedianTokens > 0 &&
		metrics.NormalizedMedianTokens < metrics.RawMedianTokens
}

func findingCalibrationNonInferior(
	candidate FindingEvaluationMetrics,
	baseline FindingEvaluationMetrics,
) bool {
	return candidate.FindingRecall >= baseline.FindingRecall &&
		candidate.MeanReciprocalRank >= baseline.MeanReciprocalRank &&
		candidate.RawPathRecall >= baseline.RawPathRecall &&
		candidate.AbstentionAccuracy >= baseline.AbstentionAccuracy &&
		candidate.FindingHandleAccuracy >= baseline.FindingHandleAccuracy &&
		candidate.EvidenceHandleAccuracy >= baseline.EvidenceHandleAccuracy &&
		candidate.SourceClassAccuracy >= baseline.SourceClassAccuracy &&
		candidate.ReviewStateAccuracy >= baseline.ReviewStateAccuracy &&
		candidate.ValidityAccuracy >= baseline.ValidityAccuracy &&
		candidate.VocabularyDisjointRate >= baseline.VocabularyDisjointRate &&
		candidate.HardNegativeHits <= baseline.HardNegativeHits &&
		candidate.DeterministicReplayRate >= baseline.DeterministicReplayRate &&
		candidate.NormalizedMedianTokens <= baseline.NormalizedMedianTokens
}

func findingDevelopmentNotWorse(
	left *FindingEvaluationMetrics,
	right *FindingEvaluationMetrics,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return findingCalibrationNonInferior(*left, *right)
}
