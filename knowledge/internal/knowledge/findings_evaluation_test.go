package knowledge

import (
	"context"
	"strings"
	"testing"
)

func TestFindingEvaluationMeasuresLanesRawAndStableHandles(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	var evidenceHandle string
	for _, finding := range fixture.inventory.Findings {
		if finding.Record.ID == "F-0042" {
			evidenceHandle = EvidenceHandle(finding.Record.ID, finding.Record.Evidence[0])
		}
	}
	if evidenceHandle == "" {
		t.Fatal("fixture evidence handle is missing")
	}
	cases := []FindingEvalCase{
		{
			ID: "normalized-card", Query: "Which table drives resource registration?",
			ExpectedFindingIDs: []string{"F-0042"}, ExpectedEvidenceHandles: []string{evidenceHandle},
			ExpectedRawPaths:       []string{"active/resource-registration/runs/R-20260802-0001/report.md"},
			HardNegativeFindingIDs: []string{"F-0044"}, Answerable: true,
		},
		{
			ID: "raw-only", Query: "RareFallbackOnlyTerm",
			ExpectedRawPaths: []string{"active/resource-registration/runs/R-20260802-0001/report.md"}, Answerable: true,
		},
		{
			ID: "abstain", Query: "no-such-evidence-7e91c3",
			Answerable: false,
		},
	}
	report, err := EvaluateFindingRetriever(context.Background(), fixture.retriever, cases)
	if err != nil {
		t.Fatal(err)
	}
	if report.FindingRecall != 1 || report.RawPathRecall != 1 || report.AbstentionAccuracy != 1 {
		t.Fatalf("finding evaluation recall/abstention failed: %#v", report)
	}
	if report.EvidenceHandleAccuracy != 1 || report.DurabilityLabelAccuracy != 1 ||
		report.DeterministicReplayRate != 1 {
		t.Fatalf("finding evaluation safety metrics failed: %#v", report)
	}
	if report.HardNegativeHits != 0 || report.LaneRelevantHits["exact"] == 0 ||
		report.LaneRelevantHits["fts"] == 0 {
		t.Fatalf("lane attribution or hard negatives failed: %#v", report)
	}
	if report.UniqueRelevantFirstHits["exact"] != 0 ||
		report.UniqueRelevantFirstHits["fts"] != 0 {
		t.Fatalf("a tied rank-one result was misreported as uniquely first: %#v",
			report.UniqueRelevantFirstHits)
	}
	if !report.ArchiveGateDiagnosticOnly {
		t.Fatal("an unratified benchmark incorrectly authorized archive opt-in")
	}
	if !sha256ValueRE.MatchString(report.Digest) || strings.TrimSpace(report.Digest) == "" {
		t.Fatalf("evaluation digest is invalid: %q", report.Digest)
	}
	rawOptions := cases[1].queryOptions()
	rawOptions.suppressNormalized = true
	rawOptions.suppressProvenance = true
	rawOptions.IncludeRaw = true
	rawResponse, err := fixture.retriever.QueryFindingCards(
		context.Background(), rawOptions)
	if err != nil {
		t.Fatal(err)
	}
	var rawOutcome *FindingCaseOutcome
	for index := range report.Cases {
		if report.Cases[index].CaseID == cases[1].ID {
			rawOutcome = &report.Cases[index]
			break
		}
	}
	if rawOutcome == nil || rawOutcome.RawTokens != rawResponse.EstimatedTokens {
		t.Fatalf("raw arm measured unlike artifacts: outcome=%#v responseTokens=%d",
			rawOutcome, rawResponse.EstimatedTokens)
	}
	rawExpansionTokens := 0
	for _, card := range rawResponse.Cards {
		rawExpansionTokens += card.ExpansionTokens
	}
	if rawOutcome.RawDocumentExpansionTokens != rawExpansionTokens {
		t.Fatalf("raw expansion diagnostic was not kept separate: outcome=%d cards=%d",
			rawOutcome.RawDocumentExpansionTokens, rawExpansionTokens)
	}
	if report.Cases[0].NormalizedEvidenceExpansionTokens <= 0 {
		t.Fatalf("normalized evidence-handle expansion was not measured: %#v", report.Cases[0])
	}
}
