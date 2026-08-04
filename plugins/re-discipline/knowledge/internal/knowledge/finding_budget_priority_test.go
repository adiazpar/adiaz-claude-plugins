package knowledge

import (
	"context"
	"fmt"
	"testing"
)

// A ranked finding card must not be displaced by its own diagnostic trace.
//
// Regression: card admission budgeted every finding card against the caller
// budget alongside the complete candidate trace. When a budget could carry
// the envelope plus the card but not also every trace row, the card was
// omitted while the trace rows were kept, so a tight per-call budget
// returned telemetry about the findings it refused to serve — and the
// cheaper provenance fallback could then shadow the omitted truth finding.
// Observed in the migration blinded evaluation: per-call budgets of 150-700
// tokens served zero finding cards or a history provenance snippet in every
// decision-gap case while the relevant finding sat in trace.candidates.
//
// The contract under test: trace candidate rows are re-derivable through
// deterministic replay, so when the budget cannot carry both, trace rows are
// shed before a ranked finding card is omitted.
func TestFindingCardOutranksTraceCandidatesUnderTightBudget(t *testing.T) {
	root := makeAdversarialProject(t)
	marker := "zq7contentfirstmark"

	writeBudgetFinding(t, root, "F-7001",
		"The "+marker+" resolver lives in the maintained encoder and returns cobalt seventeen.",
		marker+" resolver ownership")
	// Decoy findings share the marker weakly so the candidate trace carries
	// multiple rows the budget must account for.
	for index := 0; index < 8; index++ {
		id := fmt.Sprintf("F-71%02d", index)
		writeBudgetFinding(t, root, id,
			fmt.Sprintf("Unrelated subsystem note %02d mentioning %s once among other terms.", index, marker),
			fmt.Sprintf("decoy subsystem %02d", index))
	}

	service := newAdversarialService(t, root, nil)

	full, err := service.Query(context.Background(), FindingQueryOptions{
		Query: marker, QueryClass: "exact", Limit: 1, TokenBudget: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Cards) == 0 || full.Cards[0].CardType != "finding" {
		t.Fatalf("baseline query must serve a finding card: %#v", full.Cards)
	}
	if len(full.Trace.Candidates) < 2 {
		t.Fatalf("fixture must populate multiple trace candidates, got %d",
			len(full.Trace.Candidates))
	}

	// One token below the full response forces a choice between the card and
	// the complete trace. Content must win.
	tight := full.EstimatedTokens - 1
	if tight < 128 {
		t.Fatalf("fixture response is too small to exercise the budget window: %d",
			full.EstimatedTokens)
	}
	response, err := service.Query(context.Background(), FindingQueryOptions{
		Query: marker, QueryClass: "exact", Limit: 1, TokenBudget: tight,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Cards) == 0 {
		t.Fatalf(
			"budget %d omitted the ranked finding card while keeping %d trace rows; "+
				"trace must be shed before content",
			tight, len(response.Trace.Candidates))
	}
	if response.Cards[0].CardType != "finding" || response.Cards[0].ID != full.Cards[0].ID {
		t.Fatalf(
			"budget %d displaced the ranked finding %s with %s %s",
			tight, full.Cards[0].ID, response.Cards[0].CardType, response.Cards[0].ID)
	}
	if response.EstimatedTokens > tight {
		t.Fatalf("response exceeds its budget: %d > %d", response.EstimatedTokens, tight)
	}
	if len(response.Trace.Candidates) >= len(full.Trace.Candidates) {
		t.Fatalf(
			"trimmed response must carry fewer trace rows: %d >= %d",
			len(response.Trace.Candidates), len(full.Trace.Candidates))
	}

	// Below the full card's own weight, the card sheds its re-derivable
	// weight (the split text duplicating the claim) before the response
	// omits the finding entirely.
	if _, ok := full.Cards[0].Scope["sourceText"]; !ok {
		t.Fatalf("fixture card must carry a duplicated sourceText: %#v", full.Cards[0].Scope)
	}
	sawCompact := false
	for budget := full.EstimatedTokens - 1; budget >= 128; budget -= 8 {
		swept, err := service.Query(context.Background(), FindingQueryOptions{
			Query: marker, QueryClass: "exact", Limit: 1, TokenBudget: budget,
		})
		if err != nil {
			t.Fatal(err)
		}
		if swept.EstimatedTokens > budget {
			t.Fatalf("budget %d: response exceeds its budget: %d", budget, swept.EstimatedTokens)
		}
		if len(swept.Cards) == 0 {
			break
		}
		if swept.Cards[0].CardType != "finding" {
			t.Fatalf("budget %d displaced the finding with %s", budget, swept.Cards[0].CardType)
		}
		if _, ok := swept.Cards[0].Scope["sourceText"]; !ok {
			sawCompact = true
		}
	}
	if !sawCompact {
		t.Fatal("no budget in the sweep served a compacted finding card")
	}
}

func writeBudgetFinding(t *testing.T, root, id, claim, subject string) {
	t.Helper()
	document := testFindingDocument()
	document.Record.ID = id
	document.Record.CampaignID = "C-FIXTURE-CAMPAIGN"
	document.Record.Path = "docs/truth/findings/" + id + ".md"
	document.Record.Subject = subject
	document.Record.Claim = claim
	// Mirror the migration-produced card shape: the split text duplicates the
	// claim byte for byte inside the scope.
	document.Record.Scope = map[string]any{
		"authority":  "accepted-pre-0.8",
		"sourceText": claim,
		"splitCount": 1,
		"splitIndex": 1,
	}
	document.Record.ReviewState = "manager-ratified"
	document.Record.Validity = "current"
	document.Record.Projection = "truth"
	document.Record.Body = "# Claim\n" + claim +
		"\n\n## Applies when\nThe packaged fixture is measured." +
		"\n\n## Does not establish\nProduction behavior." +
		"\n\n## Evidence\nSee the exact run range." +
		"\n\n## Reproduction\nIssue the reviewed locator query." +
		"\n\n## Relations\nNo relations."
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, document.Record.Path, body)
}
