package knowledge

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

const closureCutoverTime = "2026-08-02T21:00:00Z"

func TestClosedCampaignIndexRebuildCutsOverToSingleArchiveFinding(t *testing.T) {
	root := t.TempDir()
	writeFindingFixtureFile(t, root, ".re-discipline/project-profile.md", []byte("---\nname: \"fixture\"\n---\n# Fixture\n"))
	writeFindingFixtureFile(t, root, "docs/INDEX.md", []byte("# Index\n"))

	const (
		slug        = "resource-registration"
		campaignID  = "C-TEST"
		findingID   = "F-0042"
		archiveRoot = "docs/history/campaigns/2026-08-02-resource-registration"
		activePath  = "active/resource-registration/findings/F-0042.md"
		archivePath = archiveRoot + "/findings/F-0042.md"
	)
	findingBody := renderedFixtureFinding(t, findingID, activePath,
		"manager-ratified", "current",
		"Resource registration uses table Alpha under the recorded build scope.",
		[]string{"Which table drives resource registration?", "Where is table Alpha selected?", "How does this build register resources?"},
		FindingRelations{})
	writeFindingFixtureFile(t, root, activePath, findingBody)
	writeFindingFixtureFile(t, root, archivePath, findingBody)

	campaign := closureCutoverCampaign(t, slug, campaignID, "closing", archiveRoot)
	writeCanonicalCutoverFixture(t, root, "active/"+slug+"/campaign.json", campaign)

	coverage := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: campaignID,
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{findingID: "history"},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		ActiveFileDispositions: map[string]string{},
		UnresolvedConflicts:    []string{}, MissingDecisions: []string{},
	}
	if err := sealClosureCoverage(&coverage); err != nil {
		t.Fatal(err)
	}
	manifest := ArchiveManifest{
		SchemaVersion: CampaignSchemaVersion, CampaignID: campaignID, ClosedAt: closureCutoverTime,
		SourceDigest: digestFixture("source"),
		Files:        map[string]string{"findings/F-0042.md": "sha256:" + SHA256Bytes(findingBody)},
		Projections:  map[string]string{}, Coverage: coverage,
		EventHead: "E-20260802-210000-CUTOVER",
	}
	if err := sealArchiveManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	writeCanonicalCutoverFixture(t, root, archiveRoot+"/manifest.json", manifest)

	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := KnowledgeSettings{Sources: SourceSettings{ActiveFindings: true, HistoryFindings: true}}
	assertSingleCutoverFinding(t, boundary, settings, activePath)

	// Publishing the closed campaign before the final archive artifacts is a
	// recoverable transaction seam. Active remains authoritative until the
	// receipt and README are both durable.
	campaign = closureCutoverCampaign(t, slug, campaignID, "closed", archiveRoot)
	writeCanonicalCutoverFixture(t, root, "active/"+slug+"/campaign.json", campaign)
	assertSingleCutoverFinding(t, boundary, settings, activePath)

	receipt := ClosureReceipt{
		SchemaVersion: CampaignSchemaVersion, CampaignID: campaignID, ClosureJobID: "closure-cutover",
		CampaignRevision: 2, StateHeadRevision: 9, EventID: "E-20260802-210001-CUTOVER",
		ArchiveDestination: archiveRoot, ArchiveDigest: manifest.Digest,
		TruthDigests:   map[string]string{},
		CoverageDigest: coverage.Digest, ClosedAt: closureCutoverTime,
	}
	if err := sealClosureReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	writeCanonicalCutoverFixture(t, root, archiveRoot+"/closure/receipt.json", receipt)
	writeFindingFixtureFile(t, root, archiveRoot+"/README.md", []byte("# Archived campaign\n"))

	inventory := assertSingleCutoverFinding(t, boundary, settings, archivePath)
	extraPath := archiveRoot + "/findings/F-0099.md"
	writeFindingFixtureFile(t, root, extraPath, renderedFixtureFinding(t,
		"F-0099", extraPath, "manager-ratified", "current",
		"This unmanifested archive finding must never become historical authority.",
		[]string{
			"Should this extra archive finding be indexed?",
			"Can an unmanifested archive object become authority?",
			"Which archive inventory controls historical indexing?",
		}, FindingRelations{}))
	assertSingleCutoverFinding(t, boundary, settings, activePath)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(extraPath))); err != nil {
		t.Fatal(err)
	}
	inventory = assertSingleCutoverFinding(t, boundary, settings, archivePath)
	writeFindingFixtureFile(t, root, archivePath, append(append([]byte(nil), findingBody...), []byte("\narchive corruption\n")...))
	assertSingleCutoverFinding(t, boundary, settings, activePath)
	writeFindingFixtureFile(t, root, archivePath, findingBody)
	inventory = assertSingleCutoverFinding(t, boundary, settings, archivePath)
	database := filepath.Join(root, "rebuilt.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if err := createSchema(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	generation := Generation{
		ID: "generation-closure-cutover", Database: database,
		CorpusFingerprint: inventory.Fingerprint, Project: "fixture",
		ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		CreatedAt: closureCutoverTime, DocumentCount: len(inventory.Documents), ChunkCount: len(inventory.Chunks),
	}
	if err := populateDatabase(context.Background(), db, generation, inventory, ModelManifest{}, ""); err != nil {
		db.Close()
		t.Fatalf("post-closure rebuild failed: %v", err)
	}
	var findings int
	if err := db.QueryRow("SELECT count(*) FROM findings WHERE id=?", findingID).Scan(&findings); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if findings != 1 {
		t.Fatalf("post-closure rebuild retained %d copies of %s", findings, findingID)
	}
}

func closureCutoverCampaign(t *testing.T, slug, campaignID, status, archiveRoot string) CampaignRecord {
	t.Helper()
	record := CampaignRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: campaignID,
			CreatedAt: closureCutoverTime, UpdatedAt: closureCutoverTime, Revision: 2,
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "closure-cutover",
		},
		Title: "Resource registration", Slug: slug, Objective: "Preserve one durable finding identity.",
		Scope: []string{"plugin"}, SuccessCriteria: []string{"closure completes"},
		ClosureCriteria: []string{"archive is queryable"}, Status: status,
		Owner: "manager", PermittedManagers: []string{"manager"}, OpenedAt: closureCutoverTime,
		ClosingAt: closureCutoverTime, LastEventID: "E-20260802-210000-CUTOVER",
	}
	if status == "closed" {
		record.ClosedAt = closureCutoverTime
		record.ArchiveDestination = archiveRoot
	}
	value, _, err := sealCampaignRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return value.(CampaignRecord)
}

func writeCanonicalCutoverFixture(t *testing.T, root, relative string, value any) {
	t.Helper()
	body, err := canonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFindingFixtureFile(t, root, relative, body)
}

func assertSingleCutoverFinding(
	t *testing.T, boundary Boundary, settings KnowledgeSettings, expectedPath string,
) SourceInventory {
	t.Helper()
	inventory, err := DiscoverSources(boundary, settings)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, document := range inventory.Documents {
		if document.FindingID == "F-0042" {
			paths = append(paths, document.Path)
		}
	}
	if len(paths) != 1 || paths[0] != expectedPath {
		t.Fatalf("closure cutover selected finding paths %v, want [%s]", paths, expectedPath)
	}
	return inventory
}

func digestFixture(seed string) string {
	return "sha256:" + SHA256String(seed)
}
