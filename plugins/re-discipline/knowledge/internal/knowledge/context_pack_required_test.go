package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildLargeRequiredTruth returns a truth document shaped like a real one: an
// epistemic header carrying the claim, then several sections of recipe body,
// with a distinctive marker buried in a late section rather than in the header.
func buildLargeRequiredTruth(marker string, sections int) string {
	var body strings.Builder
	body.WriteString(`# Serialization contract

**Claim:** The durable frame commit writes its checksum before the payload.

**Confidence:** DIRECT

**Validity:**
- Verified: 2026-07-01
`)
	for index := 0; index < sections; index++ {
		fmt.Fprintf(&body, "\n## Recipe step %02d\n\n", index)
		for line := 0; line < 12; line++ {
			fmt.Fprintf(&body,
				"Step %02d line %02d records the ordinary re-derivation prose that "+
					"makes a maintained truth document far larger than a small pack.\n",
				index, line)
		}
		if index == sections-1 {
			fmt.Fprintf(&body,
				"\nThe %s handshake is only described in this final step.\n", marker)
		}
	}
	return body.String()
}

// documentTokens is what a whole-file required passage would have cost.
func documentTokens(t *testing.T, root, relative string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return EstimateTokens(string(body))
}

// A required document larger than the pack budget must still reach the caller.
//
// Required evidence was packed as the whole file, so any required document
// bigger than the budget could not be included at any citation verbosity and
// the pack was refused outright with "required source paths do not fit". On the
// 35-case project benchmark that refused 34 of 35 context-pack cases at their
// ratified budgets: exactly the documents a manager most wants pinned - a
// maintained truth document with a full re-derivation recipe - were the ones
// that could never be pinned.
//
// The fixture reproduces that shape. The document is several times the budget,
// so no whole-file projection can fit it, and the assertion below proves that
// arithmetic rather than assuming it.
func TestRequiredEvidenceLargerThanBudgetIsServedAsItsBestChunk(t *testing.T) {
	const budget = 1024
	const relative = "docs/truth/serialization-contract.md"
	const marker = "ZQDEEPMARK41"

	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
		buildLargeRequiredTruth(marker, 9))
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	whole := documentTokens(t, root, relative)
	if whole <= budget {
		t.Fatalf(
			"fixture no longer reproduces the defect: the document is %d tokens "+
				"and the budget is %d, so a whole-file required passage would fit",
			whole, budget)
	}

	pack, err := service.ContextPackOptions(ctx, ContextPackRequest{
		Task:  "how does the " + marker + " handshake commit a frame",
		Role:  "manager",
		Tiers: []string{"truth"}, TokenBudget: budget,
		RequiredPaths: []string{relative},
	})
	if err != nil {
		t.Fatalf(
			"required evidence of %d tokens was refused at a %d-token budget: %v",
			whole, budget, err)
	}

	var required *ContextPassage
	for index := range pack.Passages {
		if pack.Passages[index].Citation.Path == relative {
			required = &pack.Passages[index]
			break
		}
	}
	if required == nil {
		t.Fatalf("required path %s is absent from the pack: %#v", relative, pack.Omitted)
	}
	if EstimateTokens(required.Passage) >= whole {
		t.Fatalf(
			"required passage was not narrowed to a chunk: passage=%d document=%d",
			EstimateTokens(required.Passage), whole)
	}
	// The query names something only the last section says, so the deterministic
	// lanes must land there rather than on the opening chunk.
	if !strings.Contains(required.Passage, marker) {
		t.Fatalf(
			"required chunk selection ignored the pack task: %q",
			required.Citation.Heading)
	}

	assertPassagePinsItsSource(t, root, *required)
	assertPackAccountingIsHonest(t, pack, budget)

	// Determinism: the same request must yield the same pack, byte for byte.
	repeat, err := service.ContextPackOptions(ctx, ContextPackRequest{
		Task:  "how does the " + marker + " handshake commit a frame",
		Role:  "manager",
		Tiers: []string{"truth"}, TokenBudget: budget,
		RequiredPaths: []string{relative},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Digest != pack.Digest || stableJSON(repeat) != stableJSON(pack) {
		t.Fatal("chunk-scoped required evidence is not deterministic")
	}
}

// A query that touches nothing in the required document must still return the
// document's claim rather than an arbitrary chunk of its body.
func TestRequiredEvidenceFallsBackToTheClaimBearingOpeningChunk(t *testing.T) {
	const relative = "docs/truth/serialization-contract.md"

	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
		buildLargeRequiredTruth("ZQDEEPMARK41", 9))
	service := newAdversarialService(t, root, nil)

	pack, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Task:  "ZQABSENTTOPIC77 QVXJHWMPKD",
		Role:  "manager",
		Tiers: []string{"truth"}, TokenBudget: 1024,
		RequiredPaths: []string{relative},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, passage := range pack.Passages {
		if passage.Citation.Path != relative {
			continue
		}
		if passage.Citation.StartLine != 1 {
			t.Fatalf(
				"a query matching nothing in the document must fall back to its "+
					"opening chunk, got lines %d-%d",
				passage.Citation.StartLine, passage.Citation.EndLine)
		}
		if !strings.Contains(passage.Passage, "**Claim:**") {
			t.Fatalf("the fallback chunk does not carry the document claim: %q",
				passage.Passage)
		}
		assertPassagePinsItsSource(t, root, passage)
		return
	}
	t.Fatalf("required path %s is absent from the pack", relative)
}

// Narrowing a required path to one chunk is not a licence to drop it. When even
// the best chunk cannot fit, the caller must still be told, not handed a pack
// that silently lacks the evidence it named.
func TestRequiredEvidenceThatCannotFitStillFailsExplicitly(t *testing.T) {
	root := makeAdversarialProject(t)
	required := make([]string, 0, 6)
	for index := 0; index < 6; index++ {
		relative := fmt.Sprintf("docs/truth/bulk-%02d.md", index)
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
			buildLargeRequiredTruth(fmt.Sprintf("ZQBULKMARK%02d", index), 4))
		required = append(required, relative)
	}
	service := newAdversarialService(t, root, nil)

	_, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Task:  "every bulk contract at once",
		Role:  "manager",
		Tiers: []string{"truth"}, TokenBudget: 512,
		RequiredPaths: required,
	})
	if err == nil {
		t.Fatal("required evidence that cannot fit must fail explicitly")
	}
	if !strings.Contains(err.Error(), "required source paths do not fit") {
		t.Fatalf("unexpected failure for unfittable required evidence: %v", err)
	}
}

// A required path outside the corpus or outside the allowed tiers must still be
// refused by name rather than quietly omitted.
func TestRequiredEvidenceOutsideTheCorpusOrTiersIsRefused(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	_, err := service.ContextPackOptions(ctx, ContextPackRequest{
		Task: "missing evidence", Role: "manager",
		Tiers: []string{"truth"}, TokenBudget: 1024,
		RequiredPaths: []string{"docs/truth/does-not-exist.md"},
	})
	if err == nil || !strings.Contains(err.Error(), "required path") {
		t.Fatalf("a required path outside the corpus must be refused: %v", err)
	}

	_, err = service.ContextPackOptions(ctx, ContextPackRequest{
		Task: "history evidence", Role: "manager",
		Tiers: []string{"truth"}, TokenBudget: 1024,
		RequiredPaths: []string{"docs/history/retired.md"},
	})
	if err == nil || !strings.Contains(err.Error(), "required path") {
		t.Fatalf("a required path outside the allowed tiers must be refused: %v", err)
	}
}

// assertPassagePinsItsSource re-derives the passage from the working tree the
// way verifyChunk and an independent auditor do: the cited line range of the
// cited file must be exactly the bytes served, and the passage hash must digest
// exactly those bytes.
func assertPassagePinsItsSource(t *testing.T, root string, passage ContextPassage) {
	t.Helper()
	citation := passage.Citation
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(citation.Path)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if citation.StartLine < 1 || citation.EndLine > len(lines) ||
		citation.StartLine > citation.EndLine {
		t.Fatalf("citation line range %d-%d is not inside %s (%d lines)",
			citation.StartLine, citation.EndLine, citation.Path, len(lines))
	}
	source := strings.Join(lines[citation.StartLine-1:citation.EndLine], "\n")
	if source != passage.Passage {
		t.Fatalf("citation %s:%d-%d does not reproduce the served passage",
			citation.Path, citation.StartLine, citation.EndLine)
	}
	if citation.PassageHash != SHA256String(passage.Passage) {
		t.Fatalf("citation passage hash does not pin the passage: %#v", citation)
	}
	if passage.ChunkID == "" {
		t.Fatalf("required passage lost its chunk handle: %#v", passage)
	}
}

// assertPackAccountingIsHonest checks the pack against its own claims: the
// serialized artifact costs what it says, stays inside the budget, and passes
// the same verification an independent reader performs.
func assertPackAccountingIsHonest(t *testing.T, pack ContextPack, budget int) {
	t.Helper()
	body, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	if EstimateTokens(string(body)) != pack.EstimatedTokens {
		t.Fatalf("pack token accounting is dishonest: serialized=%d claimed=%d",
			EstimateTokens(string(body)), pack.EstimatedTokens)
	}
	if pack.EstimatedTokens > budget {
		t.Fatalf("pack exceeded its budget: %d > %d", pack.EstimatedTokens, budget)
	}
	if _, err := VerifyContextPackValue(pack); err != nil {
		t.Fatalf("pack failed verification: %v", err)
	}
}
