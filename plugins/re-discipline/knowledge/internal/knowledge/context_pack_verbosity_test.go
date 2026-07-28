package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// buildRequiredEvidence returns a truth document with a full epistemic header
// whose body is padded to roughly targetBytes. Required evidence is read whole,
// so the document size is the passage size.
func buildRequiredEvidence(marker string, targetBytes int) string {
	head := "# Required contract " + marker + `

**Claim:** ` + marker + ` names a contract whose required evidence must reach a
manager intact.

**Confidence:** DIRECT

## Evidence

`
	var body strings.Builder
	for body.Len()+len(head) < targetBytes {
		body.WriteString(marker + " is attested by the recorded evidence here. ")
	}
	return head + body.String() + "\n"
}

// Required evidence that fits a ratified budget must not be refused because of
// metadata a caller cannot act on.
//
// A context pack charges its passages the full serialized cost of their
// citations. A verbose citation carries the document hash, the prelude hash and
// the generation URI on top of the path, line range, passage hash and tier that
// actually pin the evidence. Measured on this fixture that is 89 tokens per
// required passage against 69 compact, so four required documents pay 80 tokens
// for metadata that is either re-derivable or unused.
//
// Below that difference nothing changes. Inside it, a pack whose required
// evidence genuinely fits the ratified budget was refused outright with
// "required source paths do not fit", returning nothing at all - which is what
// the production 35-case project benchmark hit on every profile.
//
// The sweep covers the window from both sides so the test cannot quietly stop
// exercising the defect if envelope costs shift.
func TestRequiredEvidenceFitsRatifiedBudgetUnderCompactCitations(t *testing.T) {
	const budget = 1024
	sizes := []int{180, 210, 240, 270, 300}
	flipped := 0

	for _, size := range sizes {
		root := makeAdversarialProject(t)
		required := make([]string, 0, 4)
		for index := 0; index < 4; index++ {
			marker := fmt.Sprintf("ZQREQMARK%02d", index)
			relative := fmt.Sprintf("docs/truth/required-%02d.md", index)
			writeTestFile(
				t, filepath.Join(root, filepath.FromSlash(relative)),
				buildRequiredEvidence(marker, size))
			required = append(required, relative)
		}
		service := newAdversarialService(t, root, nil)
		ctx := context.Background()

		request := ContextPackRequest{
			Task:  "required contract evidence",
			Role:  "manager",
			Tiers: []string{"truth"}, TokenBudget: budget,
			RequiredPaths: required,
		}

		verboseRequest := request
		verboseRequest.Verbosity = VerbosityVerbose
		verbosePack, verboseErr := service.ContextPackOptions(ctx, verboseRequest)

		compactRequest := request
		compactRequest.Verbosity = VerbosityCompact
		compactPack, compactErr := service.ContextPackOptions(ctx, compactRequest)

		if compactErr == nil && verboseErr == nil {
			if compactPack.EstimatedTokens > verbosePack.EstimatedTokens {
				t.Errorf(
					"document size %d: compact pack must never cost more than "+
						"verbose: compact=%d verbose=%d",
					size, compactPack.EstimatedTokens, verbosePack.EstimatedTokens)
			}
		}
		if compactErr != nil && verboseErr == nil {
			t.Fatalf(
				"document size %d: compact refused evidence that verbose accepted: %v",
				size, compactErr)
		}
		if verboseErr == nil || compactErr != nil {
			continue
		}

		// This size sits inside the window: the 0.6.8 wire shape refuses the
		// pack outright and the compact shape delivers the same evidence.
		if !strings.Contains(
			verboseErr.Error(), "required source paths do not fit") {
			t.Fatalf("document size %d: unexpected verbose failure: %v",
				size, verboseErr)
		}
		flipped++
		t.Logf(
			"document size %d bytes: verbose refused the pack, compact packed "+
				"%d passages at %d/%d tokens",
			size, len(compactPack.Passages), compactPack.EstimatedTokens, budget)

		present := map[string]bool{}
		for _, passage := range compactPack.Passages {
			present[passage.Citation.Path] = true
		}
		for _, path := range required {
			if !present[path] {
				t.Fatalf(
					"document size %d: compact pack dropped required evidence %s: %#v",
					size, path, compactPack.Omitted)
			}
		}
		if compactPack.EstimatedTokens > budget {
			t.Fatalf("document size %d: compact pack exceeded its budget: %d > %d",
				size, compactPack.EstimatedTokens, budget)
		}
		body, err := json.Marshal(compactPack)
		if err != nil {
			t.Fatal(err)
		}
		if EstimateTokens(string(body)) != compactPack.EstimatedTokens {
			t.Fatalf(
				"document size %d: compact pack token accounting is dishonest: "+
					"serialized=%d claimed=%d",
				size, EstimateTokens(string(body)), compactPack.EstimatedTokens)
		}
		if _, err := VerifyContextPackValue(compactPack); err != nil {
			t.Fatalf("document size %d: compact pack failed verification: %v",
				size, err)
		}
	}

	if flipped == 0 {
		t.Fatal(
			"no swept document size reproduced the ratified-budget refusal; " +
				"the fixture no longer covers the defect window")
	}
}

// A compact citation must still pin its passage independently: which file,
// which lines, and a digest of exactly the bytes returned.
func TestCompactCitationsRemainIndependentlyReDerivable(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	search, err := service.Search(ctx, SearchOptions{
		Query: "A1B2C3D4", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if search.Verbosity != VerbosityCompact {
		t.Fatalf("search must default to compact, got %q", search.Verbosity)
	}
	if len(search.Results) == 0 {
		t.Fatal("fixture search returned no results")
	}
	for _, result := range search.Results {
		citation := result.Citation
		if citation.Path == "" || citation.StartLine < 1 ||
			citation.EndLine < citation.StartLine {
			t.Fatalf("compact citation lost its source range: %#v", citation)
		}
		if citation.PassageHash != SHA256String(result.Passage) {
			t.Fatalf("compact citation passage hash does not pin the passage: %#v",
				citation)
		}
		if citation.Tier == "" {
			t.Fatalf("compact citation lost its epistemic tier: %#v", citation)
		}
		if citation.SourceHash != "" || citation.URI != "" ||
			citation.ContextHash != "" {
			t.Fatalf("compact search citation kept re-derivable metadata: %#v",
				citation)
		}
		// read accepts the chunk ID directly, so dropping the URI costs a
		// caller nothing: the handle is still resolvable.
		value, err := service.Read(ctx, ReadOptions{ChunkID: result.ChunkID})
		if err != nil {
			t.Fatalf("compact result chunk handle did not resolve: %v", err)
		}
		if value["passage"].(string) != result.Passage {
			t.Fatal("resolved chunk handle returned a different passage")
		}
	}
	if search.Metadata.Generation == "" ||
		search.Metadata.CorpusFingerprint == "" ||
		search.Metadata.EffectiveProfile == "" ||
		search.Metadata.DeterministicReplay == "" {
		t.Fatalf("compact metadata dropped an actionable field: %#v", search.Metadata)
	}

	verbose, err := service.Search(ctx, SearchOptions{
		Query: "A1B2C3D4", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 1024,
		Verbosity: VerbosityVerbose,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verbose.EstimatedTokens <= search.EstimatedTokens {
		t.Fatalf(
			"compact response must cost less than verbose: compact=%d verbose=%d",
			search.EstimatedTokens, verbose.EstimatedTokens)
	}
	if verbose.Metadata.RuntimeFingerprint == "" ||
		verbose.Metadata.RequestedProfile == "" ||
		len(verbose.Metadata.ActiveLanes) == 0 {
		t.Fatalf("verbose metadata lost provenance: %#v", verbose.Metadata)
	}
	if _, err := NormalizeVerbosity("chatty"); err == nil {
		t.Fatal("unsupported verbosity must be rejected")
	}
}

// The document prelude stays charged against the caller budget, and the
// response says separately what it cost.
func TestPreludeStaysChargedAndIsAccountedSeparately(t *testing.T) {
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, "docs", "truth", "superseded.md"),
		`# Superseded reading

**Superseded-by:** docs/truth/engine.md

**Claim:** ZQPRELUDEMARK once described the frame contract.

**Confidence:** INFERRED

## Detail

ZQPRELUDEMARK appears again here so a later chunk carries the prelude rather
than the header itself.
`)
	service := newAdversarialService(t, root, nil)
	search, err := service.Search(context.Background(), SearchOptions{
		Query: "ZQPRELUDEMARK", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	carried := 0
	for _, result := range search.Results {
		if result.DocumentContext != "" {
			carried += EstimateTokens(result.DocumentContext)
			if !strings.Contains(result.DocumentContext, "superseded") {
				t.Fatalf("prelude dropped the supersession marker: %q",
					result.DocumentContext)
			}
		}
	}
	if carried == 0 {
		t.Fatal("fixture did not return a passage carrying a document prelude")
	}
	if search.ContextTokens != carried {
		t.Fatalf("contextTokens must account the packed preludes: got %d, want %d",
			search.ContextTokens, carried)
	}
	body, err := json.Marshal(search)
	if err != nil {
		t.Fatal(err)
	}
	// The prelude is charged, not exempted: the single estimatedTokens number
	// still describes everything that was serialized.
	if EstimateTokens(string(body)) != search.EstimatedTokens ||
		search.EstimatedTokens > search.TokenBudget {
		t.Fatalf(
			"prelude accounting broke the hard budget: serialized=%d claimed=%d budget=%d",
			EstimateTokens(string(body)), search.EstimatedTokens, search.TokenBudget)
	}
}
