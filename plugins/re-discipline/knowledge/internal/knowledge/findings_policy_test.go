package knowledge

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeSettingsV2MatchesPackagedPolicyShape(t *testing.T) {
	body := []byte(`{
		"$schema":"plugin://re-discipline/schemas/knowledge-settings.schema.json",
		"schemaVersion":2,
		"sources":{
			"truth":true,
			"historyFindings":true,
			"backlog":true,
			"activeFindings":true,
			"sharedMemory":true,
			"reportFallback":true,
			"additional":[{"path":"docs/playbooks","pattern":"*.md","sourceClass":"asset"}]
		},
		"models":{"execution":"local"},
		"telemetry":{"mode":"metrics-only"},
		"budgets":{
			"searchTokens":3072,
			"managerContextTokens":6144,
			"drafterContextTokens":3072,
			"maxCards":16,
			"maxBytes":32768
		},
		"archive":{"reportFallbackUntilMeasured":true,"normalizationTriggerHits":3}
	}`)
	var settings KnowledgeSettings
	if err := decodeStrict(body, &settings); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if settings.SchemaVersion != SettingsSchemaVersion || !settings.Sources.HistoryFindings ||
		!settings.Sources.ActiveFindings || !settings.Sources.ReportFallback ||
		settings.Budgets.MaxPassages != 16 || settings.Archive.NormalizationTriggerHits != 3 {
		t.Fatalf("settings v2 fields did not reach the runtime representation: %#v", settings)
	}
	settings.Archive.FallbackMode = "opt-in"
	if err := ValidateSettings(settings); err == nil {
		t.Fatal("archive opt-in without receipt path was accepted")
	}
	settings.Archive.ReportFallbackUntilMeasured = false
	settings.Archive.NormalizedBeatsRawReceipt = "../outside.json"
	if err := ValidateSettings(settings); err == nil {
		t.Fatal("escaping archive receipt path was accepted")
	}
	if RuntimeVersion != "0.8.0" || CitationDurability("archive") != "durable" {
		t.Fatalf("runtime or archive durability is stale: version=%s archive=%s", RuntimeVersion, CitationDurability("archive"))
	}
}

func TestServiceArchivePolicyComesOnlyFromRatifiedProjectSettings(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	generation, _, selected, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	retriever := Retriever{Boundary: service.Boundary, Generation: generation, Profile: selected}
	binding, err := retriever.archiveFallbackBinding()
	if err != nil {
		t.Fatal(err)
	}
	receipt, report, suite := testArchiveEvidence(t, &generation, &selected)
	if receipt.Binding != binding {
		t.Fatalf("test archive evidence did not bind service generation: %#v != %#v", receipt.Binding, binding)
	}
	suiteRelative := ".re-discipline/knowledge/evals/findings/normalized-finding-ablation-v1.json"
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(suiteRelative)), suite)
	receiptRelative := receipt.ResultingSettings.Archive.NormalizedBeatsRawReceipt
	reportBody, err := canonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(root, filepath.FromSlash(receipt.ReportPath)), reportBody, 0o600); err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(receiptRelative)), receipt)
	settings := receipt.ResultingSettings
	writeTestJSON(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), settings)

	service = newAdversarialService(t, root, nil)
	options := FindingQueryOptions{
		Query: "immutable-unnormalized-run-report", Limit: 2, TokenBudget: 1024,
		// A caller cannot reinstate default raw fallback after the project gate.
		ArchivePolicy: ArchiveFallbackPolicy{Mode: "default-fallback"},
	}
	gated, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(gated.Cards) != 0 || gated.Trace.RawFallbackDefault {
		t.Fatalf("caller bypassed configured archive gate: %#v", gated)
	}
	options.IncludeRaw = true
	explicit, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Cards) != 1 || explicit.Cards[0].CardType != "raw-report" ||
		!strings.Contains(explicit.Cards[0].Metadata["path"], "R-20260802-0001") {
		t.Fatalf("explicit provenance expansion failed after configured gate: %#v", explicit)
	}
}

func TestArchivedFindingsAndReportsUseTypedHistoryAndArchiveClasses(t *testing.T) {
	root := t.TempDir()
	const archiveRoot = "docs/history/campaigns/2026-08-02-fixture"
	writeFindingFixtureFile(t, root, ".re-discipline/project-profile.md", []byte("# Fixture\n"))
	writeFindingFixtureFile(t, root, "docs/INDEX.md", []byte("# Index\n"))
	writeFindingFixtureFile(t, root, "docs/history/legacy.md", []byte("# Maintained history\n"))
	writeFindingFixtureFile(t, root, archiveRoot+"/README.md", []byte("# Generated archive view\n"))

	findingPath := archiveRoot + "/findings/F-0046.md"
	document := testFindingDocument()
	document.Record.ID = "F-0046"
	document.Record.Path = findingPath
	document.Record.ReviewState = "manager-ratified"
	document.Record.Validity = "current"
	findingBody, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, findingPath, findingBody)
	reportPath := archiveRoot + "/runs/R-20260802-0046/report.md"
	reportBody := []byte("# VERDICT\nArchived report provenance.\n")
	writeFindingFixtureFile(t, root, reportPath, reportBody)
	reportPath2 := archiveRoot + "/runs/R-20260802-0047/report.md"
	reportBody2 := []byte("# VERDICT\nArchived report provenance.\n")
	writeFindingFixtureFile(t, root, reportPath2, reportBody2)

	coverage := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: document.Record.CampaignID,
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		ActiveFileDispositions: map[string]string{},
		UnresolvedConflicts:    []string{}, MissingDecisions: []string{},
	}
	if err := sealClosureCoverage(&coverage); err != nil {
		t.Fatal(err)
	}
	manifest := ArchiveManifest{
		SchemaVersion: CampaignSchemaVersion, CampaignID: document.Record.CampaignID,
		ClosedAt: closureCutoverTime, SourceDigest: digestFixture("archive-policy-source"),
		Files: map[string]string{
			"findings/F-0046.md":             "sha256:" + SHA256Bytes(findingBody),
			"runs/R-20260802-0046/report.md": "sha256:" + SHA256Bytes(reportBody),
			"runs/R-20260802-0047/report.md": "sha256:" + SHA256Bytes(reportBody2),
		},
		Projections: map[string]string{}, Coverage: coverage,
		EventHead: "E-20260802-210000-ARCHIVE",
	}
	if err := sealArchiveManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	writeCanonicalCutoverFixture(t, root, archiveRoot+"/manifest.json", manifest)
	receipt := ClosureReceipt{
		SchemaVersion: CampaignSchemaVersion, CampaignID: document.Record.CampaignID,
		ClosureJobID: "archive-policy", CampaignRevision: 1, StateHeadRevision: 1,
		EventID: "E-20260802-210001-ARCHIVE", ArchiveDestination: archiveRoot,
		ArchiveDigest: manifest.Digest, TruthDigests: map[string]string{},
		CoverageDigest: coverage.Digest, ClosedAt: closureCutoverTime,
	}
	if err := sealClosureReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	writeCanonicalCutoverFixture(t, root, archiveRoot+"/closure/receipt.json", receipt)

	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverSources(boundary, KnowledgeSettings{Sources: SourceSettings{
		HistoryFindings: true, ReportFallback: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]SourceDocument{}
	for _, source := range inventory.Documents {
		classes[source.Path] = source
	}
	if source := classes[findingPath]; source.Tier != "history" || source.SourceKind != "finding" || source.FindingID != "F-0046" {
		t.Fatalf("archived finding lost typed history class: %#v", source)
	}
	if source := classes[reportPath]; source.Tier != "archive" || source.SourceKind != "raw-report" {
		t.Fatalf("archived report lost opt-in provenance class: %#v", source)
	}
	if source := classes[reportPath2]; source.Tier != "archive" || source.SourceKind != "raw-report" {
		t.Fatalf("duplicate-content archived report was not independently discovered: %#v", source)
	}
	if _, indexed := classes[archiveRoot+"/README.md"]; indexed {
		t.Fatal("generated archive README was indexed as canonical history")
	}
	if source := classes["docs/history/legacy.md"]; source.Tier != "history" {
		t.Fatalf("maintained non-campaign history was lost: %#v", source)
	}

	database := filepath.Join(root, "archive-index.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if err := createSchema(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	generation := Generation{
		ID: "generation-archive-policy", Database: database, CorpusFingerprint: inventory.Fingerprint,
		Project: "fixture", ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		DocumentCount: len(inventory.Documents), ChunkCount: len(inventory.Chunks),
	}
	if err := populateDatabase(context.Background(), db, generation, inventory, ModelManifest{}, ""); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var sourceClass string
	if err := db.QueryRow(`SELECT source_class FROM findings WHERE id='F-0046'`).Scan(&sourceClass); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var archiveRows int
	if err := db.QueryRow(`SELECT count(*) FROM archive_reports`).Scan(&archiveRows); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if sourceClass != "history" || archiveRows != 2 {
		t.Fatalf("typed archive projection drifted: sourceClass=%q archiveRows=%d", sourceClass, archiveRows)
	}
}
