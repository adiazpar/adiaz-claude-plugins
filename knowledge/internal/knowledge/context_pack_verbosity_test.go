package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

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
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "replacement.md"),
		"# Current reading\n\n**Claim:** The maintained frame contract replaces ZQPRELUDEMARK.\n")
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "superseded.md"),
		`# Superseded reading

**Superseded-by:** docs/playbooks/replacement.md

**Claim:** ZQPRELUDEMARK once described the frame contract.

**Confidence:** INFERRED

## Detail

ZQPRELUDEMARK appears again here so a later chunk carries the prelude rather
than the header itself.
`)
	service := newAdversarialService(t, root, nil)
	search, err := service.Search(context.Background(), SearchOptions{
		Query: "ZQPRELUDEMARK", QueryClass: "exact",
		AllowedTiers: []string{"playbook"}, Limit: 12, TokenBudget: 1024,
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
