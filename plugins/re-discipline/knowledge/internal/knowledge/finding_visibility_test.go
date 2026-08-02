package knowledge

import "testing"

func TestDenseOnlyHardNegativeSuppressedBehindLexicalWhileRescueSurvives(t *testing.T) {
	lexical := &findingCandidate{
		record:    FindingRecord{ID: "F-0201"},
		laneRanks: map[string]int{"exact": 1, "fts": 1, "dense": 2},
	}
	denseOnlyHardNegative := &findingCandidate{
		record:    FindingRecord{ID: "F-0101"},
		laneRanks: map[string]int{"dense": 1},
	}
	ranked := []*findingCandidate{lexical, denseOnlyHardNegative}
	eligible := map[string]*findingCandidate{
		lexical.record.ID:               lexical,
		denseOnlyHardNegative.record.ID: denseOnlyHardNegative,
	}

	visible, relationEligible, suppressed :=
		suppressDenseOnlyBehindLexical(ranked, eligible)
	if suppressed != 1 || len(visible) != 1 || visible[0] != lexical {
		t.Fatalf("dense-only hard negative remained visible behind lexical evidence: visible=%v suppressed=%d",
			findingCandidateIDs(visible), suppressed)
	}
	if relationEligible[denseOnlyHardNegative.record.ID] != nil {
		t.Fatal("relation expansion could reintroduce a suppressed dense-only hard negative")
	}
	traceCandidates, _ := boundedFindingTraceCandidates(ranked, visible, 5)
	if !contains(findingCandidateIDs(traceCandidates), denseOnlyHardNegative.record.ID) {
		t.Fatal("visibility guard removed the dense-only candidate from diagnostic trace accounting")
	}

	rescueVisible, rescueEligible, rescueSuppressed := suppressDenseOnlyBehindLexical(
		[]*findingCandidate{denseOnlyHardNegative},
		map[string]*findingCandidate{
			denseOnlyHardNegative.record.ID: denseOnlyHardNegative,
		},
	)
	if rescueSuppressed != 0 || len(rescueVisible) != 1 ||
		rescueVisible[0] != denseOnlyHardNegative ||
		rescueEligible[denseOnlyHardNegative.record.ID] != denseOnlyHardNegative {
		t.Fatalf("true no-lexical dense rescue was suppressed: visible=%v suppressed=%d",
			findingCandidateIDs(rescueVisible), rescueSuppressed)
	}
}

func findingCandidateIDs(candidates []*findingCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			ids = append(ids, candidate.record.ID)
		}
	}
	return ids
}
