package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type truthRelocationFixture struct {
	store        *StateStore
	service      *Service
	root         string
	head         StateHead
	source       string
	destination  string
	sourceDigest string
	finding      FindingDocument
}

func newTruthRelocationFixture(t *testing.T) truthRelocationFixture {
	return newTruthRelocationFixtureAt(t, "docs/truth/legacy-topic.md")
}

func newTruthRelocationFixtureAt(t *testing.T, source string) truthRelocationFixture {
	t.Helper()
	store, root := newStateTestStore(t)
	archiveRoot := "docs/history/campaigns/source-campaign-archive"
	campaign := stateTestCampaignRecord()
	campaign.RecordMeta = stateTestMeta("C-SOURCE", 2)
	campaign.Title = "Closed source campaign"
	campaign.Slug = "source-campaign"
	campaign.Status = "closed"
	campaign.CurrentFocus = nil
	campaign.ClosingAt = stateTestTime
	campaign.ClosedAt = stateTestTime
	campaign.ArchiveDestination = archiveRoot
	campaign.Digest = ""
	_, campaignBody, err := sealCampaignRecord(campaign)
	if err != nil {
		t.Fatal(err)
	}

	reportPath := archiveRoot + "/runs/R-20260817-0001/report.md"
	reportBody := []byte("direct legacy truth evidence\ncomplete\n")
	reportDigest := "sha256:" + SHA256Bytes(reportBody)
	findingPath := archiveRoot + "/findings/F-0001.md"
	document := FindingDocument{
		Record: FindingRecord{
			SchemaVersion: CampaignSchemaVersion,
			ID:            "F-0001",
			CampaignID:    "C-SOURCE",
			Revision:      2,
			CreatedAt:     stateTestTime,
			UpdatedAt:     stateTestTime,
			CreatedBy:     "curator",
			UpdatedBy:     "manager",
			CorrelationID: "source-review",
			Kind:          "conclusion",
			Subject:       "truth.provenance",
			Claim:         "The closed campaign established one canonical truth finding.",
			Scope:         map[string]any{"component": "knowledge"},
			AppliesWhen:   []string{"the closed campaign evidence remains digest-exact"},
			KnownLimits:   []string{"no broader claim is established"},
			SourceRuns:    []string{"R-20260817-0001"},
			Evidence: []EvidenceReference{{
				Path: reportPath, SHA256: reportDigest, StartLine: 1, EndLine: 2,
				ObjectKey: "path:" + reportPath + "#L1-L2", SourceRun: "R-20260817-0001",
			}},
			EvidenceGrade: "direct",
			ReviewState:   "manager-ratified",
			Validity:      "current",
			Projection:    "truth",
			VerifiedAt:    "2026-08-17",
			Path:          findingPath,
			Body: "# Claim\n\nThe closed campaign established one canonical truth finding.\n\n" +
				"## Applies when\n\nThe cited evidence remains digest-exact.\n\n" +
				"## Does not establish\n\nNo broader claim is established.\n\n" +
				"## Evidence\n\nSee the exact archived report range.\n\n" +
				"## Reproduction\n\nVerify the archived report digest and range.\n\n" +
				"## Relations\n\nNo relations are asserted.",
		},
		SyntheticQuestions: []string{
			"Which closed campaign established the canonical truth finding?",
			"What evidence supports the canonical truth finding?",
			"When does the canonical truth finding apply?",
		},
		QuestionsReviewed: true,
	}
	findingBody, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	document, err = ParseFindingDocument(findingBody, findingPath)
	if err != nil {
		t.Fatal(err)
	}

	legacyBody := []byte(fmt.Sprintf(
		"---\nschemaVersion: 2\ntruthId: \"T-F-0001\"\nsourceFinding: \"F-0001\"\n"+
			"sourceCampaign: \"C-SOURCE\"\nsubject: %q\nclaim: %q\nstatus: current\n---\n\n%s\n",
		document.Record.Subject, document.Record.Claim, document.Record.Claim))

	for relative, body := range map[string][]byte{
		archiveRoot + "/finalization/campaign.json": campaignBody,
		reportPath:  reportBody,
		findingPath: findingBody,
		source:      legacyBody,
	} {
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, opening := openStateTestCampaign(t, store)
	return truthRelocationFixture{
		store: store, service: &Service{Boundary: store.Boundary}, root: root,
		head: opening.ResultingHead, source: source,
		destination:  "docs/truth/findings/source-campaign/F-0001.md",
		sourceDigest: "sha256:" + SHA256Bytes(legacyBody), finding: document,
	}
}

func TestTruthRelocationNormalizesFlatLegacyFindingProjection(t *testing.T) {
	fixture := newTruthRelocationFixtureAt(t, "docs/truth/findings/F-0001.md")
	receipt, err := fixture.service.ManagerApply(context.Background(), fixture.request())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.DeletedArtifacts) != 1 ||
		receipt.DeletedArtifacts[0].Path != "docs/truth/findings/F-0001.md" ||
		len(receipt.Artifacts) != 1 || receipt.Artifacts[0].Path != fixture.destination {
		t.Fatalf("flat projection was not normalized into one campaign-scoped finding: %#v", receipt)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.source))); !os.IsNotExist(err) {
		t.Fatalf("flat legacy projection still exists: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.destination)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFindingDocument(body, fixture.destination); err != nil {
		t.Fatalf("normalized flat projection is not a canonical FindingDocument: %v", err)
	}
}

func (fixture truthRelocationFixture) request() ManagerApplyRequest {
	return ManagerApplyRequest{
		Action: "truth.relocate", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "relocate-truth", IdempotencyKey: "relocate-truth-once",
		Rationale:            "Normalize legacy native truth into the single finding provenance system.",
		ExpectedHeadRevision: fixture.head.Revision, ExpectedHeadDigest: fixture.head.Digest,
		TruthRelocations: []TruthRelocation{{
			Source: fixture.source, ExpectedDigest: fixture.sourceDigest,
		}},
	}
}

func TestTruthRelocationPublishesOneTypedFindingAndReplays(t *testing.T) {
	fixture := newTruthRelocationFixture(t)
	request := fixture.request()
	receipt, err := fixture.service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Artifacts) != 1 || receipt.Artifacts[0].Path != fixture.destination ||
		len(receipt.DeletedArtifacts) != 1 || receipt.DeletedArtifacts[0].Path != fixture.source {
		t.Fatalf("truth relocation receipt is incomplete: %#v", receipt)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.source))); !os.IsNotExist(err) {
		t.Fatalf("legacy truth source still exists: %v", err)
	}
	destinationBody, err := os.ReadFile(filepath.Join(
		fixture.root, filepath.FromSlash(fixture.destination)))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFindingDocument(destinationBody, fixture.destination)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Record.Digest != fixture.finding.Record.Digest ||
		parsed.Record.CampaignID != "C-SOURCE" || parsed.Record.ID != "F-0001" ||
		!reflect.DeepEqual(parsed.SyntheticQuestions, fixture.finding.SyntheticQuestions) {
		t.Fatalf("relocated truth is not the archived finding: %#v", parsed)
	}
	inventorySources, err := DiscoverSources(fixture.store.Boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	foundTyped := false
	for _, source := range inventorySources.Documents {
		if source.Path == fixture.destination {
			foundTyped = source.SourceKind == "finding" && source.Tier == "truth" &&
				source.FindingID == "F-0001"
		}
	}
	if !foundTyped {
		t.Fatalf("campaign-scoped truth was not indexed as one typed finding: %#v",
			inventorySources.Diagnostics)
	}

	replayed, err := fixture.service.ManagerApply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("truth relocation did not replay exactly: receipt=%#v err=%v", replayed, err)
	}
	tampered := request
	tampered.TruthRelocations = []TruthRelocation{{
		Source: fixture.source, ExpectedDigest: "sha256:" + strings.Repeat("f", 64),
	}}
	if _, err := fixture.service.ManagerApply(context.Background(), tampered); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed truth relocation reused its idempotency key: %v", err)
	}

	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := fixture.store.loadCommittedInventory(head)
	if err != nil {
		t.Fatal(err)
	}
	entries := inventoryEntriesMap(inventory)
	if entries[fixture.source] != "" || entries[fixture.destination] != receipt.Artifacts[0].ContentDigest {
		t.Fatalf("truth relocation inventory is wrong: source=%q destination=%q",
			entries[fixture.source], entries[fixture.destination])
	}
}

func TestTruthRelocationCrashBeforeHeadRollsBackCreateAndDelete(t *testing.T) {
	fixture := newTruthRelocationFixture(t)
	request := fixture.request()
	artifacts, err := fixture.service.prepareTruthRelocationArtifacts(request)
	if err != nil {
		t.Fatal(err)
	}
	transaction := managerStateTransactionRequest(request, nil, artifacts, "")
	fixture.store.Failpoint = func(point StateFailpoint) error {
		if point.Name == FailBeforeHeadPublish {
			return errors.New("injected truth relocation interruption")
		}
		return nil
	}
	if _, err := fixture.store.Apply(context.Background(), transaction); err == nil {
		t.Fatal("truth relocation failpoint did not interrupt publication")
	}
	fixture.store.Failpoint = nil
	recovered := NewStateStoreWithBoundary(fixture.store.Boundary)
	fixed, _ := time.Parse(time.RFC3339, stateTestTime)
	recovered.Now = func() time.Time { return fixed }
	if err := recovered.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.source))); err != nil {
		t.Fatalf("rollback did not restore legacy truth source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.destination))); !os.IsNotExist(err) {
		t.Fatalf("rollback retained relocated destination: %v", err)
	}
	if _, err := fixture.service.ManagerApply(context.Background(), request); err != nil {
		t.Fatalf("truth relocation did not commit after exact rollback: %v", err)
	}
}
