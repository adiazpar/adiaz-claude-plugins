package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTruthDiscoveryHasOneAuthoritativeRecordSystem(t *testing.T) {
	root := makeAdversarialProject(t)
	writeFindingFixtureFile(t, root, "docs/truth/splits/F-0088.md", []byte(
		"# Migrated atomic findings\n\n- [Compatibility view](../findings/F-0088.md)\n"))
	writeFindingFixtureFile(t, root, "docs/truth/legacy-claim.md", []byte(
		"# Legacy claim\n\nThis compact projection must not become truth authority.\n"))

	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverSources(boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	foundIndex := false
	for _, source := range inventory.Documents {
		switch source.Path {
		case "docs/truth/INDEX.md":
			foundIndex = source.Tier == "navigation" && source.SourceKind == ""
		case "docs/truth/splits/F-0088.md", "docs/truth/legacy-claim.md":
			t.Fatalf("non-canonical truth document entered retrieval: %#v", source)
		}
	}
	if !foundIndex {
		t.Fatal("truth index was not retained as navigation")
	}
}

func TestTruthFindingIsTypedQueriedByDefaultAndExactlyReadable(t *testing.T) {
	root := makeAdversarialProject(t)
	reportPath := "active/fixture-campaign/runs/R-20260802-0088/report.md"
	report := []byte("# VERIFIED OBSERVATION\n\nThe immutable source establishes the truth finding fixture.\n")
	writeFindingFixtureFile(t, root, reportPath, report)
	evidence := EvidenceReference{
		Path: reportPath, SHA256: "sha256:" + SHA256Bytes(report),
		StartLine: 1, EndLine: 3, SourceRun: "R-20260802-0088",
	}
	findingPath := "docs/truth/findings/F-0088.md"
	document := testFindingDocument()
	document.Record.ID = "F-0088"
	document.Record.CampaignID = "C-FIXTURE-CAMPAIGN"
	document.Record.Path = findingPath
	document.Record.Subject = "truth.typed-finding"
	document.Record.Claim = "The maintained locator resolves to cobalt seventeen."
	document.Record.SourceRuns = []string{"R-20260802-0088"}
	document.Record.Evidence = []EvidenceReference{evidence}
	document.Record.ReviewState = "manager-ratified"
	document.Record.Validity = "current"
	document.Record.Projection = "truth"
	document.Record.Body = "# Claim\n" + document.Record.Claim +
		"\n\n## Applies when\nThe packaged fixture is measured." +
		"\n\n## Does not establish\nProduction behavior." +
		"\n\n## Evidence\nSee the exact run range." +
		"\n\n## Reproduction\nIssue the reviewed locator query." +
		"\n\n## Relations\nNo relations."
	document.SyntheticQuestions = []string{
		"Which maintained locator is authoritative?",
		"Where does cobalt seventeen resolve?",
		"What truth finding governs the locator?",
	}
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, findingPath, body)

	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverSources(boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	foundTypedTruth := false
	for _, source := range inventory.Documents {
		if source.Path == findingPath {
			foundTypedTruth = source.SourceKind == "finding" && source.Tier == "truth" &&
				source.FindingID == "F-0088"
		}
	}
	if !foundTypedTruth {
		t.Fatalf("truth finding was not discovered as a typed truth source: %#v", inventory.Diagnostics)
	}

	service := newAdversarialService(t, root, nil)
	response, err := service.Query(context.Background(), FindingQueryOptions{
		Query: "Which maintained locator is authoritative?", Limit: 2, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Cards) == 0 || response.Cards[0].ID != "F-0088" ||
		response.Cards[0].SourceClass != "truth" {
		t.Fatalf("default finding query did not return typed truth: %#v", response.Cards)
	}
	findingRead, err := service.ReadExact(context.Background(), ExactReadRequest{
		Selector: "finding", Value: FindingHandle("F-0088"), TokenBudget: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if findingRead.Path != findingPath || findingRead.RecordID != "F-0088" ||
		findingRead.Handle != FindingHandle("F-0088") {
		t.Fatalf("truth finding exact read lost identity: %#v", findingRead)
	}
	evidenceHandle := EvidenceHandle("F-0088", evidence)
	evidenceRead, err := service.ReadExact(context.Background(), ExactReadRequest{
		Selector: "evidence", Value: evidenceHandle, TokenBudget: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidenceRead.Handle != evidenceHandle || evidenceRead.Path != reportPath ||
		evidenceRead.Content == "" || filepath.ToSlash(evidenceRead.Path) != reportPath {
		t.Fatalf("truth evidence exact read lost its immutable target: %#v", evidenceRead)
	}
}
