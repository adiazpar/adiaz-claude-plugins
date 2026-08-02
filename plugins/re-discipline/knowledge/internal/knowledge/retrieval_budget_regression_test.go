package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// buildMarkerBody returns a single-section document whose body repeats marker
// until the document reaches at least targetBytes. It is kept to one heading
// and one unbroken paragraph so the section chunker emits a single chunk.
func buildMarkerBody(marker string, targetBytes int) string {
	head := "# Budget window document " + marker + "\n\n"
	var body strings.Builder
	for body.Len()+len(head) < targetBytes {
		body.WriteString(marker)
		body.WriteString(" saturates this line so lexical lanes rank it first ")
	}
	return head + body.String() + "\n"
}

// An oversized top-ranked passage must not starve the whole response.
//
// Regression: the packing loop budgeted passages against the full caller
// budget while the mandatory response metadata was charged against that same
// budget. When a top-ranked passage was larger than the real headroom
// (budget minus metadata) but still smaller than the budget itself, the loop
// admitted it, skipped every smaller candidate, and finalizeSearchResponse
// then stripped it. The caller received zero results with most of the budget
// unspent and every candidate attributed to "budget".
//
// EstimateTokens is (bytes+3)/4, so at a 1024-token budget the defect window
// is roughly 2800-4096 passage bytes. The sizes below sweep across it so the
// test cannot silently miss the window if metadata overhead shifts.
func TestSearchOversizedTopPassageDoesNotStarveSmallerCandidates(t *testing.T) {
	root := makeAdversarialProject(t)

	sizes := []int{2400, 2800, 3200, 3600, 4000}
	markers := make([]string, len(sizes))
	for index, size := range sizes {
		marker := fmt.Sprintf("ZQ7BUDGETMARK%02d", index)
		markers[index] = marker

		// The large document outranks the compact one on term frequency and is
		// sized into (or near) the defect window.
		writeTestFile(
			t,
			filepath.Join(root, "docs", "truth",
				fmt.Sprintf("oversized-%02d.md", index)),
			buildMarkerBody(marker, size))

		// A compact companion carrying the same marker. It always fits, so a
		// correct packer must return it whenever the large one is skipped.
		writeTestFile(
			t,
			filepath.Join(root, "docs", "truth",
				fmt.Sprintf("compact-%02d.md", index)),
			"# Compact "+marker+"\n\n"+marker+" appears in a short passage.\n")
	}

	service := newAdversarialService(t, root, nil)

	for index, marker := range markers {
		search, err := service.Search(context.Background(), SearchOptions{
			Query: marker, QueryClass: "exact", AllowedTiers: []string{"truth"},
			Limit: 10, TokenBudget: 1024,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(search.Results) == 0 {
			t.Fatalf(
				"marker %s (document %d bytes): search returned no passages "+
					"although a compact candidate fits; estimatedTokens=%v "+
					"tokenBudget=%v omittedByReason=%#v",
				marker, sizes[index], search.EstimatedTokens,
				search.TokenBudget, search.OmittedByReason)
		}
	}
}

// The reserved overhead must be positive and must describe the response
// envelope only, never the packed passages.
func TestResponseTokenOverheadIsBoundedAndPositive(t *testing.T) {
	skeleton := SearchResponse{
		Query: "budget overhead probe", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, TokenBudget: 1024,
		Results: []SearchResult{},
		OmittedByReason: map[string]int{
			"resultLimit": 0, "documentCap": 0, "staleSource": 0, "budget": 0,
		},
	}
	overhead := responseTokenOverhead(skeleton)
	if overhead <= 0 {
		t.Fatalf("overhead must be positive, got %d", overhead)
	}
	withResults := skeleton
	withResults.Results = []SearchResult{{
		ChunkID: "chunk-probe", Passage: strings.Repeat("padding ", 400),
	}}
	if got := responseTokenOverhead(withResults); got != overhead {
		t.Fatalf(
			"overhead must ignore packed passages: got %d, want %d",
			got, overhead)
	}
}
