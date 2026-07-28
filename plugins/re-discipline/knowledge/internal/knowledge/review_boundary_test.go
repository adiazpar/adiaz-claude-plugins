package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// unreviewedReport opens on its VERDICT, so the claim a reader must not take
// as evidence is in the document's opening chunk.
const unreviewedReport = `# VERDICT: frame commit ordering

DIRECT: ZQDRAFTMARK88 commits the frame checksum before the payload.

## 2. CLAIMS

The ZQDRAFTMARK88 ordering was observed once under a debugger.

## 3. RESIDUAL UNCERTAINTIES

Only one build was examined.

**Overall confidence:** medium
`

const reviewStamp = "\n**Review:** promote\n\n**Disposition:** promote\n"

// enableDrafterReports turns on report indexing for a fixture project. The
// baseline fixture leaves it off, and the review boundary only exists once
// reports are indexed at all.
func enableDrafterReports(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), `{
  "schemaVersion": 1,
  "sources": {
    "truth": true,
    "history": true,
    "backlog": true,
    "activeCampaigns": true,
    "sharedMemory": true,
    "drafterReports": true
  },
  "models": {
    "execution": "local"
  },
  "telemetry": {
    "mode": "metrics-only"
  },
  "budgets": {
    "searchTokens": 1024,
    "managerContextTokens": 2048,
    "drafterContextTokens": 2048,
    "maxPassages": 12,
    "maxBytes": 32768
  }
}
`)
}

func writeDrafterReport(t *testing.T, root string, stamped bool) string {
	t.Helper()
	enableDrafterReports(t, root)
	relative := "active/fixture-campaign/subagents/run-02/report.md"
	body := unreviewedReport
	if stamped {
		body += reviewStamp
	}
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)), body)
	return relative
}

// The opening chunk of an unreviewed drafter report must carry the UNREVIEWED
// marker.
//
// Every other chunk received it, because the prelude exists to give later
// chunks the header they lack. Chunk 0 was skipped on the reasoning that it
// already contains the header verbatim - true of a truth document, false of an
// unreviewed report, whose status is synthesized from the ABSENCE of a review
// stamp and therefore appears nowhere in the body. The chunk that was served
// bare is the one holding the VERDICT.
func TestUnreviewedReportOpeningChunkCarriesTheMarker(t *testing.T) {
	root := makeAdversarialProject(t)
	relative := writeDrafterReport(t, root, false)
	service := newAdversarialService(t, root, nil)

	search, err := service.Search(context.Background(), SearchOptions{
		Query: "ZQDRAFTMARK88", QueryClass: "exact",
		AllowedTiers: []string{"draft"}, Limit: 12, TokenBudget: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	opening := openingResult(t, search.Results, relative)
	if !strings.Contains(opening.Passage, "VERDICT") {
		t.Fatalf("fixture no longer puts the verdict in the opening chunk: %q",
			opening.Passage)
	}
	if !strings.Contains(opening.DocumentContext, "UNREVIEWED") {
		t.Fatalf(
			"the opening chunk of an unreviewed report was served without the "+
				"UNREVIEWED marker: context=%q",
			opening.DocumentContext)
	}
	if opening.Citation.ContextHash != "" &&
		opening.Citation.ContextHash != SHA256String(opening.DocumentContext) {
		t.Fatalf("prelude hash does not pin the served prelude: %#v", opening.Citation)
	}
	// The marker is synthesized, so it must not have been smuggled into the
	// passage itself: verifyChunk re-reads the source lines and requires an
	// exact match.
	if strings.Contains(opening.Passage, "UNREVIEWED") {
		t.Fatal("the synthesized marker leaked into the verifiable passage")
	}
}

// Once a manager stamps the report, its opening chunk behaves like any other
// reviewed content: the header is genuinely in the body, so the prelude adds
// nothing there.
func TestStampedReportOpeningChunkBehavesLikeReviewedContent(t *testing.T) {
	root := makeAdversarialProject(t)
	relative := writeDrafterReport(t, root, true)
	service := newAdversarialService(t, root, nil)

	search, err := service.Search(context.Background(), SearchOptions{
		Query: "ZQDRAFTMARK88", QueryClass: "exact",
		AllowedTiers: []string{"campaign"}, Limit: 12, TokenBudget: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	opening := openingResult(t, search.Results, relative)
	if opening.Citation.Tier != "campaign" {
		t.Fatalf("a stamped report must be promoted to campaign, got %q",
			opening.Citation.Tier)
	}
	if opening.DocumentContext != "" {
		t.Fatalf(
			"a reviewed report's opening chunk must not repeat its own header: %q",
			opening.DocumentContext)
	}
}

// The review stamp is the whole tier boundary, so its behavior is asserted
// directly rather than inferred from retrieval.
func TestReviewStampPromotesTheReportTier(t *testing.T) {
	root := makeAdversarialProject(t)
	relative := writeDrafterReport(t, root, false)
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	tierOf := func() string {
		inventory, err := DiscoverSources(boundary, DefaultKnowledgeSettings())
		if err != nil {
			t.Fatal(err)
		}
		for _, document := range inventory.Documents {
			if document.Path == relative {
				return document.Tier
			}
		}
		return ""
	}
	if tier := tierOf(); tier != "draft" {
		t.Fatalf("an unstamped report must stay in draft, got %q", tier)
	}
	writeDrafterReport(t, root, true)
	if tier := tierOf(); tier != "campaign" {
		t.Fatalf("a stamped report must be promoted to campaign, got %q", tier)
	}

	// The stamp is line-anchored: a mention in prose must not promote.
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
		unreviewedReport+"\nThe manager will add a **Review:** stamp later.\n")
	if tier := tierOf(); tier != "draft" {
		t.Fatalf("an in-prose review mention promoted an unreviewed report: %q", tier)
	}
}

// The safety-critical property, proved end to end: unstamped drafter output
// cannot reach a context pack that did not ask for `draft` by name.
func TestUnreviewedDrafterOutputCannotReachADefaultTierPack(t *testing.T) {
	root := makeAdversarialProject(t)
	relative := writeDrafterReport(t, root, false)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	for _, role := range []string{"manager", "drafter"} {
		pack, err := service.ContextPack(ctx, "ZQDRAFTMARK88 frame commit ordering",
			role, nil, 2048)
		if err != nil {
			t.Fatal(err)
		}
		if contains(pack.AllowedTiers, "draft") {
			t.Fatalf("the default %s tier set contains draft: %v",
				role, pack.AllowedTiers)
		}
		for _, passage := range pack.Passages {
			if passage.Citation.Path == relative || passage.Citation.Tier == "draft" {
				t.Fatalf("unreviewed drafter output reached a default %s pack: %#v",
					role, passage.Citation)
			}
		}
	}

	orient, err := service.Orient(ctx, "manager", 2048)
	if err != nil {
		t.Fatal(err)
	}
	if contains(orient.AllowedTiers, "draft") {
		t.Fatalf("orient exposes the draft tier: %v", orient.AllowedTiers)
	}

	// Asked for by name, it is retrievable - and the request is visible in the
	// pack's allowedTiers, which is what review-subagent checks.
	named, err := service.ContextPack(ctx, "ZQDRAFTMARK88 frame commit ordering",
		"manager", []string{"draft"}, 2048)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, passage := range named.Passages {
		if passage.Citation.Path == relative {
			found = true
			if !strings.Contains(passage.DocumentContext, "UNREVIEWED") {
				t.Fatalf(
					"a draft passage reached a pack without the UNREVIEWED marker: %#v",
					passage)
			}
		}
	}
	if !found || !contains(named.AllowedTiers, "draft") {
		t.Fatalf("an explicit draft request did not return the report: %v",
			named.AllowedTiers)
	}
}

// ExtractReportPrelude is what renders the marker, so its contract is asserted
// directly: an unreviewed report says so, a reviewed one carries the manager's
// disposition, and neither is truncated away.
func TestExtractReportPreludeMarksReviewState(t *testing.T) {
	unreviewed := ExtractReportPrelude(unreviewedReport, "report.md", false)
	if unreviewed.Status != "UNREVIEWED - drafter claim, not evidence" {
		t.Fatalf("unreviewed status = %q", unreviewed.Status)
	}
	if !strings.Contains(unreviewed.Claim, "ZQDRAFTMARK88") {
		t.Fatalf("the verdict is not the report's claim: %q", unreviewed.Claim)
	}
	// The epistemic label opens the verdict and must not consume the claim.
	if strings.HasPrefix(unreviewed.Claim, "DIRECT") {
		t.Fatalf("the verdict label displaced the claim: %q", unreviewed.Claim)
	}
	if unreviewed.Confidence != "medium" {
		t.Fatalf("overall confidence = %q", unreviewed.Confidence)
	}
	if !strings.Contains(unreviewed.Render(), "UNREVIEWED") {
		t.Fatalf("rendered prelude lost the marker: %q", unreviewed.Render())
	}

	reviewed := ExtractReportPrelude(
		unreviewedReport+reviewStamp, "report.md", true)
	if reviewed.Status != "reviewed" || reviewed.Correction != "promote" {
		t.Fatalf("reviewed prelude = %#v", reviewed)
	}
	if strings.Contains(reviewed.Render(), "UNREVIEWED") {
		t.Fatalf("a reviewed report still claims to be unreviewed: %q",
			reviewed.Render())
	}

	// A long report must not truncate the marker away.
	long := "# Report\n\n## 1. VERDICT\n\n" + strings.Repeat("long verdict text ", 60) +
		"\n\n**Overall confidence:** " + strings.Repeat("high ", 20) + "\n"
	rendered := ExtractReportPrelude(long, "report.md", false).Render()
	if !strings.Contains(rendered, "UNREVIEWED") || len(rendered) > preludeMaxBytes {
		t.Fatalf("truncation dropped the marker or broke the cap: %d bytes %q",
			len(rendered), rendered)
	}
}

// openingResult returns the result holding the document's first chunk.
func openingResult(t *testing.T, results []SearchResult, path string) SearchResult {
	t.Helper()
	best := SearchResult{}
	found := false
	for _, result := range results {
		if result.Citation.Path != path {
			continue
		}
		if !found || result.Citation.StartLine < best.Citation.StartLine {
			best = result
			found = true
		}
	}
	if !found {
		t.Fatalf("no result for %s in %d results", path, len(results))
	}
	if best.Citation.StartLine != 1 {
		t.Fatalf("the opening chunk of %s was not returned: lines %d-%d",
			path, best.Citation.StartLine, best.Citation.EndLine)
	}
	return best
}
