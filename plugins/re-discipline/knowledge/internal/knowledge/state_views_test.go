package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCanonicalClosureGraph(t *testing.T, root string) CampaignGraph {
	t.Helper()
	graph := closureTestGraph(t)
	writeJSON := func(relative string, value any) {
		t.Helper()
		body, err := canonicalJSON(value)
		if err != nil {
			t.Fatal(err)
		}
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON("active/test/campaign.json", *graph.Campaign)
	writeJSON("active/test/work-items/W-0001.json", graph.WorkItems["W-0001"])
	writeJSON("active/test/runs/R-20260802-0001/run.json", graph.Runs["R-20260802-0001"])
	writeJSON("active/test/intake/I-0001.json", graph.Intakes["I-0001"])
	writeJSON("active/test/reviews/V-0001.json", graph.Reviews["V-0001"])
	finding := graph.Findings["F-0001"]
	finding.Evidence[0].SHA256 = "sha256:" + strings.TrimPrefix(finding.Evidence[0].SHA256, "sha256:")
	finding.Body = "# Claim\n\nThe implementation is complete.\n\n## Applies when\n\nPlugin scope.\n\n## Does not establish\n\nHost conformance.\n\n## Evidence\n\nThe reviewed run report.\n\n## Reproduction\n\nInspect the report.\n\n## Relations\n\nNone."
	finding.Path = "active/test/findings/F-0001.md"
	findingDocument := FindingDocument{
		Record: finding,
		SyntheticQuestions: []string{
			"Is the plugin implementation complete?",
			"What is the implementation status?",
			"Was the plugin implemented?",
		},
		QuestionsReviewed: true,
	}
	body, err := RenderFindingDocument(findingDocument)
	if err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(root, "active", "test", "findings", "F-0001.md")
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, body, 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFindingDocument(body, "active/test/findings/F-0001.md")
	if err != nil {
		t.Fatal(err)
	}
	graph.Findings["F-0001"] = parsed.Record
	if err := os.WriteFile(filepath.Join(root, "active", "test", "STATE.md"), []byte("# Generated state\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestBoundedStateViewsUseCanonicalGraph(t *testing.T) {
	root := makeAdversarialProject(t)
	writeCanonicalClosureGraph(t, root)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	orient, err := service.State(ctx, StateRequest{Mode: "orient", TokenBudget: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if orient.TokenCost > 1200 || !digestRE.MatchString(orient.Digest) {
		t.Fatalf("orientation is not bounded or sealed: %#v", orient)
	}
	foundCampaign := false
	for _, card := range orient.Cards {
		if card.ID == "C-TEST" {
			foundCampaign = true
		}
		if card.ID == "fixture-campaign" {
			t.Fatal("legacy campaign leaked into state orientation")
		}
	}
	if !foundCampaign {
		t.Fatalf("orientation omitted canonical campaign: %#v", orient.Cards)
	}

	resume, err := service.State(ctx, StateRequest{Mode: "resume", CampaignID: "C-TEST", TokenBudget: 1800})
	if err != nil {
		t.Fatal(err)
	}
	if resume.CampaignID != "C-TEST" || len(resume.Cards) == 0 || resume.TokenCost > 1800 {
		t.Fatalf("resume view is incomplete: %#v", resume)
	}
	work, err := service.State(ctx, StateRequest{
		Mode: "work", CampaignID: "C-TEST", WorkItemID: "W-0001", TokenBudget: 2200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if work.WorkItemID != "W-0001" || len(work.Cards) < 2 {
		t.Fatalf("work view omitted linked context: %#v", work)
	}
	closure, err := service.State(ctx, StateRequest{Mode: "closure", CampaignID: "C-TEST"})
	if err != nil {
		t.Fatal(err)
	}
	if closure.Status != "attention" {
		t.Fatalf("missing closure job was not surfaced: %#v", closure)
	}
}

func TestExactFindingReadAndGraphTraceShareHandles(t *testing.T) {
	root := makeAdversarialProject(t)
	writeCanonicalClosureGraph(t, root)
	service := newAdversarialService(t, root, nil)

	read, err := service.ReadExact(context.Background(), ExactReadRequest{
		Selector: "finding", Value: "finding:F-0001", CampaignID: "C-TEST", TokenBudget: 1600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.RecordID != "F-0001" || read.Handle != "finding:F-0001" ||
		read.EstimatedTokens > 1600 || !digestRE.MatchString(read.Digest) {
		t.Fatalf("exact finding read is incomplete: %#v", read)
	}
	trace, err := service.Trace(context.Background(), TraceRequest{
		CampaignID: "C-TEST", StartHandle: "finding:F-0001",
		Depth: 2, MaxNodes: 12, TokenBudget: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Nodes) < 2 || len(trace.Edges) < 2 || trace.EstimatedTokens > 1800 ||
		!digestRE.MatchString(trace.Digest) {
		t.Fatalf("finding trace omitted provenance or exceeded budget: %#v", trace)
	}
}

// A caller stranded by start's refusal reads the closure view next. The door out
// has to be stated there, not left inferable from the action enum, and the
// attempt counter has to be visible or a reader cannot tell a resumed attempt
// from a re-planned one.
func TestClosureStateViewOffersRestartToAReopenedCampaign(t *testing.T) {
	store, _, service, _ := prepareClosureArchiveFixture(t)
	closing, err := service.State(context.Background(), StateRequest{Mode: "closure", CampaignID: "C-TEST"})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range closing.Cards {
		if card.ID == "closure-restart-available" {
			t.Fatal("a live closure attempt was offered a restart")
		}
		if card.ID == "closure-test" && card.Metadata["attempt"] != "1" {
			t.Fatalf("a first closure attempt was not reported as attempt 1: %#v", card.Metadata)
		}
	}

	reopenClosureFixture(t, store, service, "2026-08-02T18:11:00Z", "closure-reopen")
	reopened, err := service.State(context.Background(), StateRequest{Mode: "closure", CampaignID: "C-TEST"})
	if err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, card := range reopened.Cards {
		if card.ID != "closure-restart-available" {
			continue
		}
		offered = true
		if !strings.Contains(card.Claim, "restart") {
			t.Fatalf("the reopened closure card does not name the restart action: %#v", card)
		}
	}
	if !offered {
		t.Fatalf("a reopened closure job was not offered the restart door: %#v", reopened.Cards)
	}
	if reopened.Status != "attention" {
		t.Fatalf("a reopened closure job did not require attention: %#v", reopened)
	}
}
