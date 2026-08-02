package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type findingIndexFixture struct {
	boundary   Boundary
	generation Generation
	retriever  Retriever
	inventory  SourceInventory
}

func writeFindingFixtureFile(t *testing.T, root, relative string, body []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func renderedFixtureFinding(t *testing.T, id, path, review, validity, claim string, questions []string, relations FindingRelations) []byte {
	t.Helper()
	document := testFindingDocument()
	document.Record.ID = id
	document.Record.Path = path
	document.Record.ReviewState = review
	document.Record.Validity = validity
	document.Record.Claim = claim
	document.Record.Relations = relations
	document.SyntheticQuestions = questions
	document.Record.Body = strings.Replace(document.Record.Body,
		"Resource registration uses the named table.", claim, 1)
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func buildFindingIndexFixture(t *testing.T) findingIndexFixture {
	t.Helper()
	root := t.TempDir()
	writeFindingFixtureFile(t, root, ".re-discipline/project-profile.md", []byte("---\nname: \"fixture\"\n---\n# Fixture\n"))
	writeFindingFixtureFile(t, root, "docs/INDEX.md", []byte("# Index\n"))
	f42Path := "active/resource-registration/findings/F-0042.md"
	f43Path := "active/resource-registration/findings/F-0043.md"
	f44Path := "active/resource-registration/findings/F-0044.md"
	writeFindingFixtureFile(t, root, f42Path, renderedFixtureFinding(t,
		"F-0042", f42Path, "manager-ratified", "current",
		"Resource registration uses table Alpha under the recorded build scope.",
		[]string{"Which table drives resource registration?", "Where is table Alpha selected?", "How does this build register resources?"},
		FindingRelations{},
	))
	writeFindingFixtureFile(t, root, f43Path, renderedFixtureFinding(t,
		"F-0043", f43Path, "manager-ratified", "current",
		"A competing observation says table Beta drives resource registration.",
		[]string{"Does table Beta drive registration?", "What conflicts with table Alpha?", "Which registration observation is contested?"},
		FindingRelations{Contradicts: []string{"F-0042"}},
	))
	writeFindingFixtureFile(t, root, f44Path, renderedFixtureFinding(t,
		"F-0044", f44Path, "curator-checked", "provisional",
		"Provisional identifier FUN_141A08E80 is associated with the registration probe.",
		[]string{"Which provisional function is associated with the probe?", "What is FUN_141A08E80 used for?", "Which address appears in the provisional result?"},
		FindingRelations{},
	))
	invalidPath := "active/resource-registration/findings/F-0099.md"
	invalid := renderedFixtureFinding(t,
		"F-0099", invalidPath, "curator-checked", "provisional",
		"This record is deliberately tampered after rendering.",
		[]string{"Why is this record invalid?", "Was this finding tampered?", "Should this finding be indexed?"},
		FindingRelations{},
	)
	invalid = []byte(strings.Replace(string(invalid), "deliberately tampered", "silently tampered", 1))
	writeFindingFixtureFile(t, root, invalidPath, invalid)
	writeFindingFixtureFile(t, root, "active/resource-registration/runs/R-20260802-0001/report.md", []byte(
		"# VERDICT\nResource registration uses table Alpha. RareFallbackOnlyTerm appears only in this raw report.\n\n"+
			"# CLAIMS\nRaw provenance for the registration probe.\n"))

	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := KnowledgeSettings{Sources: SourceSettings{ActiveFindings: true, ReportFallback: true}}
	inventory, err := DiscoverSources(boundary, settings)
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "finding-index.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if err := createSchema(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	generation := Generation{
		ID: "generation-finding-test", Database: database, CorpusFingerprint: inventory.Fingerprint,
		Project: "fixture", ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		CreatedAt: "2026-08-02T20:00:00Z", DocumentCount: len(inventory.Documents),
		ChunkCount: len(inventory.Chunks),
	}
	if err := populateDatabase(context.Background(), db, generation, inventory, ModelManifest{}, ""); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyDatabase(database); err != nil {
		t.Fatal(err)
	}
	tracker, err := NewArchiveFallbackTracker(2)
	if err != nil {
		t.Fatal(err)
	}
	retriever := Retriever{
		Boundary: boundary, Generation: generation, ArchiveTracker: tracker,
		Profile: SelectedProfile{
			EffectiveIdentity: "fixture:finding-card-v1", ActiveLanes: []string{"exact", "fts", "rerank"},
			Effective: EffectiveProfile{
				Weights: map[string]int{"exact": 8, "fts": 6}, RRFK: 60, RerankDepth: 10,
			},
		},
	}
	return findingIndexFixture{boundary: boundary, generation: generation, retriever: retriever, inventory: inventory}
}

func TestFindingDiscoveryRejectsTamperAndBuildsTypedProjection(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	if len(fixture.inventory.Findings) != 3 {
		t.Fatalf("expected three valid findings, got %d", len(fixture.inventory.Findings))
	}
	foundDiagnostic := false
	foundRaw := false
	for _, diagnostic := range fixture.inventory.Diagnostics {
		if strings.Contains(diagnostic, "F-0099.md: invalid finding") && strings.Contains(diagnostic, "digest mismatch") {
			foundDiagnostic = true
		}
	}
	for _, document := range fixture.inventory.Documents {
		if document.SourceKind == "raw-report" {
			foundRaw = true
		}
	}
	if !foundDiagnostic || !foundRaw {
		t.Fatalf("discovery lost invalid-record diagnostic or raw provenance label: diagnostics=%v raw=%t", fixture.inventory.Diagnostics, foundRaw)
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(fixture.generation.Database))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var findings, questions, evidence, archives int
	for query, target := range map[string]*int{
		"SELECT count(*) FROM findings":                                        findingsPointer(&findings),
		"SELECT count(*) FROM finding_queries WHERE kind='synthetic-question'": findingsPointer(&questions),
		"SELECT count(*) FROM finding_evidence":                                findingsPointer(&evidence),
		"SELECT count(*) FROM archive_reports":                                 findingsPointer(&archives),
	} {
		if err := db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if findings != 3 || questions != 9 || evidence != 3 || archives != 1 {
		t.Fatalf("typed projection counts are wrong: findings=%d questions=%d evidence=%d archives=%d", findings, questions, evidence, archives)
	}
}

func findingsPointer(value *int) *int { return value }

func TestFindingCardQueryFiltersThenRanksAndExpandsRelations(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	response, err := fixture.retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "Which table drives resource registration?", Limit: 3, TokenBudget: 4096,
		RequestID: "query-normalized-first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Cards) < 2 || response.Cards[0].ID != "F-0042" {
		t.Fatalf("normalized finding did not rank first: %#v", response.Cards)
	}
	if response.Cards[1].ID != "F-0043" || !contains(response.Cards[0].RelationAlerts, "incoming-contradicts:F-0043") {
		t.Fatalf("typed contradiction was not expanded and labeled: %#v", response.Cards)
	}
	if len(response.Cards) == 3 && response.Cards[2].CardType != "raw-report" {
		t.Fatalf("raw fallback did not stay below normalized cards: %#v", response.Cards)
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := EstimateTokens(string(body)); got != response.EstimatedTokens || got > response.TokenBudget {
		t.Fatalf("response token receipt does not describe returned bytes: got=%d receipt=%d budget=%d", got, response.EstimatedTokens, response.TokenBudget)
	}
	digestInput := response
	digestInput.Digest = ""
	expectedDigest, err := CanonicalDigest(digestInput)
	if err != nil {
		t.Fatal(err)
	}
	if response.Digest != expectedDigest {
		t.Fatalf("response digest does not bind the converged token receipt: got=%s want=%s", response.Digest, expectedDigest)
	}
	for _, card := range response.Cards {
		if card.ID == "F-0044" {
			t.Fatal("provisional finding bypassed default hard filters")
		}
	}
	provisional, err := fixture.retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "FUN_0x141a08e80", Limit: 2, TokenBudget: 4096,
		AllowedSourceClasses: []string{"provisional"}, AllowedReviewStates: []string{"curator-checked"},
		AllowedValidities: []string{"provisional"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provisional.Cards) == 0 || provisional.Cards[0].ID != "F-0044" || provisional.Cards[0].SourceClass != "provisional" {
		t.Fatalf("explicit provisional query failed: %#v", provisional.Cards)
	}
}

func TestFindingRelationExpansionReservesPairBeforeRankPrefixFills(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE finding_relations(source_id TEXT,target_id TEXT,kind TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO finding_relations(source_id,target_id,kind) VALUES('F-0042','F-0045','contradicts')`); err != nil {
		t.Fatal(err)
	}
	ranked := []*findingCandidate{
		{record: FindingRecord{ID: "F-0041"}},
		{record: FindingRecord{ID: "F-0042"}},
		{record: FindingRecord{ID: "F-0043"}},
		{record: FindingRecord{ID: "F-0045"}},
	}
	eligible := map[string]*findingCandidate{}
	for _, candidate := range ranked {
		eligible[candidate.record.ID] = candidate
	}
	visible, err := expandFindingRelations(context.Background(), db, ranked, eligible, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 3 || visible[0].record.ID != "F-0041" || visible[1].record.ID != "F-0042" || visible[2].record.ID != "F-0045" {
		t.Fatalf("relation-aware prefix failed to reserve both endpoints: %#v", visible)
	}
	if !contains(visible[1].relationAlerts, "contradicts:F-0045") || !contains(visible[2].relationAdds, "relation-from:F-0042") {
		t.Fatalf("relation expansion provenance was not retained: source=%v target=%v", visible[1].relationAlerts, visible[2].relationAdds)
	}
	if visible[1].relationGroup == "" || visible[1].relationGroup != visible[2].relationGroup {
		t.Fatalf("relation pair was not marked for atomic budget packing: source=%q target=%q", visible[1].relationGroup, visible[2].relationGroup)
	}
}

func TestFindingTraceBoundsCandidatesAndRetainsRelationExpansion(t *testing.T) {
	ranked := make([]*findingCandidate, 20)
	for index := range ranked {
		ranked[index] = &findingCandidate{record: FindingRecord{ID: "F-" + strings.Repeat("0", 3) + string(rune('A'+index))}}
	}
	related := &findingCandidate{record: FindingRecord{ID: "F-9999"}}
	visible := []*findingCandidate{ranked[0], related}
	trace, total := boundedFindingTraceCandidates(ranked, visible, 2)
	if total != 21 || len(trace) != 6 || trace[0] != ranked[0] || trace[1] != related {
		t.Fatalf("bounded trace lost returned relation or accounting: total=%d trace=%#v", total, trace)
	}
}

func TestFindingCardRawFallbackGateAndServeAccounting(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	options := FindingQueryOptions{
		Query: "RareFallbackOnlyTerm", Limit: 2, TokenBudget: 4096, RequestID: "raw-1",
	}
	first, err := fixture.retriever.QueryFindingCards(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Cards) != 1 || first.Cards[0].CardType != "raw-report" || !first.Trace.RawFallbackDefault {
		t.Fatalf("default raw fallback failed: %#v", first)
	}
	options.RequestID = "raw-2"
	second, err := fixture.retriever.QueryFindingCards(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Trace.NormalizationSuggestions) != 1 {
		t.Fatalf("repeat raw serve did not suggest normalization: %#v", second.Trace)
	}
	binding, err := fixture.retriever.archiveFallbackBinding()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testArchiveReceipt(t)
	receipt.Binding = binding
	receipt.Digest, err = ArchiveReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	options.ArchivePolicy = ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &receipt}
	options.IncludeRaw = false
	options.RequestID = "raw-gated"
	gated, err := fixture.retriever.QueryFindingCards(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(gated.Cards) != 0 || gated.Trace.RawFallbackDefault {
		t.Fatalf("gate-approved archive remained default: %#v", gated)
	}
	options.IncludeRaw = true
	explicit, err := fixture.retriever.QueryFindingCards(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Cards) != 1 || explicit.Cards[0].CardType != "raw-report" {
		t.Fatalf("explicit archive request failed after gate: %#v", explicit)
	}
}
