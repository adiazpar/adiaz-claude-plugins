package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// EvaluateFindingRetriever measures the public finding-card unit and the raw
// report fallback as two paired retrieval arms. This prevents a blended
// response from making normalized recall or token cost look better merely
// because the other arm supplied the answer.
func EvaluateFindingRetriever(
	ctx context.Context,
	retriever Retriever,
	cases []FindingEvalCase,
) (FindingAblationReport, error) {
	if len(cases) == 0 {
		return FindingAblationReport{}, errors.New("finding evaluation case set is empty")
	}
	report := FindingAblationReport{
		SchemaVersion:    1,
		LaneRelevantHits: map[string]int{}, UniqueRelevantFirstHits: map[string]int{},
		MetricsBySplit:            map[string]FindingEvaluationMetrics{},
		MetricsByRole:             map[string]FindingEvaluationMetrics{},
		ArchiveGateDiagnosticOnly: true,
	}
	for _, eval := range cases {
		if strings.TrimSpace(eval.ID) == "" || strings.TrimSpace(eval.Query) == "" {
			return FindingAblationReport{}, errors.New("finding evaluation cases require id and query")
		}
		options := eval.queryOptions()
		normalizedOptions := options
		normalizedOptions.suppressRaw = true
		normalizedOptions.suppressProvenance = true
		normalizedOptions.IncludeRaw = false
		rawOptions := options
		// The raw-only arm uses an unexported measurement switch instead of a
		// synthetic public filter, so both arms retain byte-for-byte identical
		// question, source/review/validity filters, budget, and card limit.
		rawOptions.suppressNormalized = true
		rawOptions.suppressProvenance = true
		rawOptions.IncludeRaw = true
		normalized, err := retriever.QueryFindingCards(ctx, normalizedOptions)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf("case %s normalized arm: %w", eval.ID, err)
		}
		replay, err := retriever.QueryFindingCards(ctx, normalizedOptions)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf("case %s normalized replay: %w", eval.ID, err)
		}
		raw, err := retriever.QueryFindingCards(ctx, rawOptions)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf("case %s raw arm: %w", eval.ID, err)
		}

		outcome := FindingCaseOutcome{
			CaseID: eval.ID, Role: eval.Role, Topic: eval.Topic, Split: eval.Split,
			QueryClass: eval.QueryClass, Status: normalized.Status,
			LaneRelevantHits: map[string]int{}, UniqueRelevantFirstHits: map[string]int{},
			EvidenceHandlesComplete: true, FindingHandlesComplete: true,
			SourceClassesAccurate: true, ReviewStatesAccurate: true,
			ValiditiesAccurate: true, ClaimVocabularyDisjoint: true,
			DurabilityLabelsAccurate: true,
			ReplayIdentical:          normalized.Digest == replay.Digest,
			NormalizedTokens:         normalized.EstimatedTokens,
		}
		cardsByID := map[string]ContextCard{}
		evidenceSeen, findingHandlesSeen := map[string]bool{}, map[string]bool{}
		hardNegativeSet := map[string]bool{}
		for _, id := range eval.HardNegativeFindingIDs {
			hardNegativeSet[id] = true
		}
		for rank, card := range normalized.Cards {
			outcome.CardIDs = append(outcome.CardIDs, card.ID)
			if card.CardType != "finding" {
				outcome.DurabilityLabelsAccurate = false
				continue
			}
			outcome.FindingIDs = append(outcome.FindingIDs, card.ID)
			outcome.RelevantFindingRanks = appendRelevantRank(
				outcome.RelevantFindingRanks, eval.ExpectedFindingIDs, card.ID, rank+1)
			cardsByID[card.ID] = card
			findingHandlesSeen[card.Handle] = true
			if card.EvidenceHandle != "" {
				evidenceSeen[card.EvidenceHandle] = true
			}
			if hardNegativeSet[card.ID] {
				outcome.HardNegativeHits = append(outcome.HardNegativeHits, card.ID)
			}
		}
		rawSeen := map[string]bool{}
		for _, card := range raw.Cards {
			if card.CardType != "raw-report" {
				outcome.DurabilityLabelsAccurate = false
				continue
			}
			path := card.Metadata["path"]
			outcome.RawPaths = append(outcome.RawPaths, path)
			rawSeen[path] = true
			outcome.RawDocumentExpansionTokens += card.ExpansionTokens
			if card.SourceClass != "archive" {
				outcome.DurabilityLabelsAccurate = false
			}
		}
		// Compare like with like: both arms measure the serialized bounded card
		// response the caller actually received. ExpansionTokens remains useful
		// planning metadata, but mixing a normalized response cost with a full raw
		// report expansion made the archive gate structurally favor normalization.
		outcome.RawTokens = raw.EstimatedTokens
		evidenceExpansion, err := boundedEvidenceExpansionTokens(
			ctx, retriever, normalized.Cards, options.TokenBudget)
		if err != nil {
			return FindingAblationReport{}, fmt.Errorf(
				"case %s normalized evidence expansion: %w", eval.ID, err)
		}
		outcome.NormalizedEvidenceExpansionTokens = evidenceExpansion

		_, traces := findingTraceRanks(normalized.Trace.Candidates)
		for _, expected := range eval.ExpectedFindingIDs {
			card, present := cardsByID[expected]
			if !present {
				outcome.FindingHandlesComplete = false
				outcome.SourceClassesAccurate = false
				outcome.ReviewStatesAccurate = false
				outcome.ValiditiesAccurate = false
			} else {
				outcome.FindingHandlesComplete = outcome.FindingHandlesComplete &&
					findingHandlesSeen[FindingHandle(expected)]
				if want := eval.ExpectedSourceClasses[expected]; want != "" && card.SourceClass != want {
					outcome.SourceClassesAccurate = false
				}
				if want := eval.ExpectedReviewStates[expected]; want != "" && card.ReviewState != want {
					outcome.ReviewStatesAccurate = false
				}
				if want := eval.ExpectedValidities[expected]; want != "" && card.Validity != want {
					outcome.ValiditiesAccurate = false
				}
				if eval.Role == "manager" && eval.QueryClass != "exact" {
					outcome.VocabularyDisjointApplicable = true
					if !findingClaimVocabularyDisjoint(eval.Query, card) {
						outcome.ClaimVocabularyDisjoint = false
					}
				}
			}
			trace := traces[expected]
			retrievingLanes := []string{}
			for lane, rank := range trace.LaneRanks {
				if rank <= 0 {
					continue
				}
				retrievingLanes = append(retrievingLanes, lane)
				outcome.LaneRelevantHits[lane]++
				report.LaneRelevantHits[lane]++
			}
			// "Unique first" is an ablation signal: the lane must rank the
			// relevant target first and be the only lane that retrieved it at
			// all. Merely beating another lane that also found the target does
			// not establish unique retrieval value.
			if len(retrievingLanes) == 1 && trace.LaneRanks[retrievingLanes[0]] == 1 {
				lane := retrievingLanes[0]
				outcome.UniqueRelevantFirstHits[lane]++
				report.UniqueRelevantFirstHits[lane]++
			}
		}
		for _, handle := range eval.ExpectedFindingHandles {
			if !findingHandlesSeen[handle] {
				outcome.FindingHandlesComplete = false
			}
		}
		for _, handle := range eval.ExpectedEvidenceHandles {
			if evidenceSeen[handle] {
				outcome.EvidenceHandlesFound = append(outcome.EvidenceHandlesFound, handle)
			} else {
				outcome.EvidenceHandlesComplete = false
			}
		}
		for _, path := range eval.ExpectedRawPaths {
			_ = rawSeen[path]
		}
		outcome.DurabilityLabelsAccurate = outcome.DurabilityLabelsAccurate &&
			outcome.SourceClassesAccurate && outcome.ReviewStatesAccurate &&
			outcome.ValiditiesAccurate
		if eval.Answerable {
			outcome.AbstentionCorrect = len(normalized.Cards) > 0 || len(raw.Cards) > 0
		} else {
			outcome.AbstentionCorrect = len(normalized.Cards) == 0 && len(raw.Cards) == 0
		}
		outcome.FindingIDs = SortedUnique(outcome.FindingIDs)
		outcome.RawPaths = SortedUnique(outcome.RawPaths)
		outcome.HardNegativeHits = SortedUnique(outcome.HardNegativeHits)
		outcome.EvidenceHandlesFound = SortedUnique(outcome.EvidenceHandlesFound)
		report.NormalizedTokens += outcome.NormalizedTokens
		report.RawTokens += outcome.RawTokens
		report.NormalizedEvidenceExpansionTokens += outcome.NormalizedEvidenceExpansionTokens
		report.RawDocumentExpansionTokens += outcome.RawDocumentExpansionTokens
		report.HardNegativeHits += len(outcome.HardNegativeHits)
		report.Cases = append(report.Cases, outcome)
	}

	overall := findingMetrics(report.Cases, cases)
	report.FindingRecall = overall.FindingRecall
	report.MeanReciprocalRank = overall.MeanReciprocalRank
	report.RawPathRecall = overall.RawPathRecall
	report.AbstentionAccuracy = overall.AbstentionAccuracy
	report.FindingHandleAccuracy = overall.FindingHandleAccuracy
	report.EvidenceHandleAccuracy = overall.EvidenceHandleAccuracy
	report.SourceClassAccuracy = overall.SourceClassAccuracy
	report.ReviewStateAccuracy = overall.ReviewStateAccuracy
	report.ValidityAccuracy = overall.ValidityAccuracy
	report.VocabularyDisjointRate = overall.VocabularyDisjointRate
	report.DeterministicReplayRate = overall.DeterministicReplayRate
	report.NormalizedMedianTokens = overall.NormalizedMedianTokens
	report.RawMedianTokens = overall.RawMedianTokens
	report.NormalizedMedianEvidenceExpansionTokens = overall.NormalizedMedianEvidenceExpansionTokens
	report.RawMedianDocumentExpansionTokens = overall.RawMedianDocumentExpansionTokens
	durable := 0
	for _, outcome := range report.Cases {
		if outcome.DurabilityLabelsAccurate {
			durable++
		}
	}
	report.DurabilityLabelAccuracy = float64(durable) / float64(len(report.Cases))
	for _, split := range []string{"development", "holdout"} {
		outcomes, subset := filterFindingCases(report.Cases, cases, func(eval FindingEvalCase) bool {
			return eval.Split == split
		})
		report.MetricsBySplit[split] = findingMetrics(outcomes, subset)
	}
	for _, role := range []string{"manager", "drafter"} {
		outcomes, subset := filterFindingCases(report.Cases, cases, func(eval FindingEvalCase) bool {
			return eval.Role == role
		})
		report.MetricsByRole[role] = findingMetrics(outcomes, subset)
	}
	return sealFindingAblationReport(report)
}

func boundedEvidenceExpansionTokens(
	ctx context.Context,
	retriever Retriever,
	cards []ContextCard,
	perHandleBudget int,
) (int, error) {
	if perHandleBudget < 128 || perHandleBudget > 8192 {
		return 0, fmt.Errorf("invalid per-handle token budget %d", perHandleBudget)
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(retriever.Generation.Database))
	if err != nil {
		return 0, err
	}
	defer db.Close()
	service := Service{Boundary: retriever.Boundary}
	seen := map[string]bool{}
	total := 0
	for _, card := range cards {
		handle := card.EvidenceHandle
		campaignID := card.Metadata["campaignId"]
		identity := campaignID + "/" + handle
		if handle == "" || seen[identity] {
			continue
		}
		if campaignID == "" {
			return 0, fmt.Errorf("finding card %s has no campaignId", card.ID)
		}
		seen[identity] = true
		var findingID string
		var evidence EvidenceReference
		err := db.QueryRowContext(ctx, `SELECT f.id,e.path,e.sha256,e.start_line,e.end_line,
			e.object_key,e.source_run FROM finding_evidence e
			JOIN findings f ON f.key=e.finding_key
			WHERE e.handle=? AND f.campaign_id=?`, handle, campaignID).Scan(
			&findingID, &evidence.Path, &evidence.SHA256, &evidence.StartLine,
			&evidence.EndLine, &evidence.ObjectKey, &evidence.SourceRun,
		)
		if err != nil {
			return 0, fmt.Errorf("resolve %s: %w", handle, err)
		}
		if EvidenceHandle(findingID, evidence) != handle {
			return 0, fmt.Errorf("indexed evidence handle %s is inconsistent", handle)
		}
		expanded, err := service.readEvidenceExact(
			FindingRecord{ID: findingID}, evidence, perHandleBudget)
		if err != nil {
			return 0, fmt.Errorf("expand %s: %w", handle, err)
		}
		total += expanded.EstimatedTokens
	}
	return total, nil
}

func appendRelevantRank(ranks []int, expected []string, id string, rank int) []int {
	if contains(expected, id) {
		return append(ranks, rank)
	}
	return ranks
}

func findingTraceRanks(
	candidates []FindingCandidateTrace,
) (map[string]int, map[string]FindingCandidateTrace) {
	fusionOrder := append([]FindingCandidateTrace(nil), candidates...)
	sort.Slice(fusionOrder, func(i, j int) bool {
		if fusionOrder[i].FusionScore != fusionOrder[j].FusionScore {
			return fusionOrder[i].FusionScore > fusionOrder[j].FusionScore
		}
		return fusionOrder[i].FindingID < fusionOrder[j].FindingID
	})
	fusionRanks, traces := map[string]int{}, map[string]FindingCandidateTrace{}
	for index, candidate := range fusionOrder {
		fusionRanks[candidate.FindingID] = index + 1
		traces[candidate.FindingID] = candidate
	}
	return fusionRanks, traces
}

var findingEvalStopWords = map[string]bool{
	"about": true, "after": true, "before": true, "does": true, "from": true,
	"have": true, "into": true, "that": true, "their": true, "this": true,
	"what": true, "when": true, "where": true, "which": true, "with": true,
}

func findingClaimVocabularyDisjoint(query string, card ContextCard) bool {
	target := map[string]bool{}
	for _, term := range IdentifierTerms(card.Subject + "\n" + card.Claim) {
		if len(term) >= 4 && !findingEvalStopWords[term] {
			target[term] = true
		}
	}
	for _, term := range IdentifierTerms(query) {
		if len(term) >= 4 && !findingEvalStopWords[term] && target[term] {
			return false
		}
	}
	return true
}

func findingMetrics(
	outcomes []FindingCaseOutcome,
	cases []FindingEvalCase,
) FindingEvaluationMetrics {
	metrics := FindingEvaluationMetrics{CaseCount: len(cases)}
	if len(cases) == 0 {
		return metrics
	}
	byID := map[string]FindingCaseOutcome{}
	for _, outcome := range outcomes {
		byID[outcome.CaseID] = outcome
	}
	expectedFindings, foundFindings, findingCases := 0, 0, 0
	expectedRaw, foundRaw := 0, 0
	abstention, findingHandles, evidenceHandles := 0, 0, 0
	sourceClasses, reviewStates, validities := 0, 0, 0
	vocabularyApplicable, vocabularyDisjoint, replay := 0, 0, 0
	var reciprocal float64
	normalizedCosts, rawCosts := []int{}, []int{}
	normalizedExpansionCosts, rawExpansionCosts := []int{}, []int{}
	for _, eval := range cases {
		outcome := byID[eval.ID]
		foundIDs, foundPaths := map[string]bool{}, map[string]bool{}
		for _, id := range outcome.FindingIDs {
			foundIDs[id] = true
		}
		for _, path := range outcome.RawPaths {
			foundPaths[path] = true
		}
		if len(eval.ExpectedFindingIDs) > 0 {
			findingCases++
			best := 0
			for _, rank := range outcome.RelevantFindingRanks {
				if best == 0 || rank < best {
					best = rank
				}
			}
			if best > 0 {
				reciprocal += 1 / float64(best)
			}
			if outcome.FindingHandlesComplete {
				findingHandles++
			}
			if outcome.SourceClassesAccurate {
				sourceClasses++
			}
			if outcome.ReviewStatesAccurate {
				reviewStates++
			}
			if outcome.ValiditiesAccurate {
				validities++
			}
		}
		for _, id := range eval.ExpectedFindingIDs {
			expectedFindings++
			if foundIDs[id] {
				foundFindings++
			}
		}
		for _, path := range eval.ExpectedRawPaths {
			expectedRaw++
			if foundPaths[path] {
				foundRaw++
			}
		}
		if len(eval.ExpectedEvidenceHandles) > 0 {
			if outcome.EvidenceHandlesComplete {
				evidenceHandles++
			}
		}
		if outcome.AbstentionCorrect {
			abstention++
		}
		if outcome.VocabularyDisjointApplicable {
			vocabularyApplicable++
			if outcome.ClaimVocabularyDisjoint {
				vocabularyDisjoint++
			}
		}
		if outcome.ReplayIdentical {
			replay++
		}
		metrics.HardNegativeHits += len(outcome.HardNegativeHits)
		if len(eval.ExpectedFindingIDs) > 0 && len(eval.ExpectedRawPaths) > 0 {
			normalizedCosts = append(normalizedCosts, outcome.NormalizedTokens)
			rawCosts = append(rawCosts, outcome.RawTokens)
			normalizedExpansionCosts = append(
				normalizedExpansionCosts, outcome.NormalizedEvidenceExpansionTokens)
			rawExpansionCosts = append(
				rawExpansionCosts, outcome.RawDocumentExpansionTokens)
		}
	}
	metrics.FindingRecall = safeRatio(foundFindings, expectedFindings, 1)
	metrics.RawPathRecall = safeRatio(foundRaw, expectedRaw, 1)
	metrics.AbstentionAccuracy = safeRatio(abstention, len(cases), 1)
	metrics.FindingHandleAccuracy = safeRatio(findingHandles, findingCases, 1)
	metrics.EvidenceHandleAccuracy = safeRatio(evidenceHandles, countFindingEvidenceCases(cases), 1)
	metrics.SourceClassAccuracy = safeRatio(sourceClasses, findingCases, 1)
	metrics.ReviewStateAccuracy = safeRatio(reviewStates, findingCases, 1)
	metrics.ValidityAccuracy = safeRatio(validities, findingCases, 1)
	metrics.VocabularyDisjointRate = safeRatio(vocabularyDisjoint, vocabularyApplicable, 1)
	metrics.DeterministicReplayRate = safeRatio(replay, len(cases), 1)
	if findingCases > 0 {
		metrics.MeanReciprocalRank = reciprocal / float64(findingCases)
	}
	metrics.NormalizedMedianTokens = medianInt(normalizedCosts)
	metrics.RawMedianTokens = medianInt(rawCosts)
	metrics.NormalizedMedianEvidenceExpansionTokens = medianInt(normalizedExpansionCosts)
	metrics.RawMedianDocumentExpansionTokens = medianInt(rawExpansionCosts)
	return metrics
}

func countFindingEvidenceCases(cases []FindingEvalCase) int {
	count := 0
	for _, eval := range cases {
		if len(eval.ExpectedEvidenceHandles) > 0 {
			count++
		}
	}
	return count
}

func safeRatio(numerator, denominator int, empty float64) float64 {
	if denominator == 0 {
		return empty
	}
	return float64(numerator) / float64(denominator)
}

func medianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	copy := append([]int(nil), values...)
	sort.Ints(copy)
	middle := len(copy) / 2
	if len(copy)%2 == 1 {
		return copy[middle]
	}
	return (copy[middle-1] + copy[middle]) / 2
}

func filterFindingCases(
	outcomes []FindingCaseOutcome,
	cases []FindingEvalCase,
	keep func(FindingEvalCase) bool,
) ([]FindingCaseOutcome, []FindingEvalCase) {
	caseIDs := map[string]bool{}
	filteredCases := []FindingEvalCase{}
	for _, eval := range cases {
		if keep(eval) {
			filteredCases = append(filteredCases, eval)
			caseIDs[eval.ID] = true
		}
	}
	filteredOutcomes := []FindingCaseOutcome{}
	for _, outcome := range outcomes {
		if caseIDs[outcome.CaseID] {
			filteredOutcomes = append(filteredOutcomes, outcome)
		}
	}
	return filteredOutcomes, filteredCases
}
