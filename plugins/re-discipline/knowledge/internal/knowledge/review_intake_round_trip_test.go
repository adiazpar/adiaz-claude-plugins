package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// roundTripTestIntake is the smallest valid curator intake over one returned
// source report: complete non-claim coverage and no candidate findings. Every
// optional collection is left unset, which is exactly the shape whose persisted
// and sealed forms disagree.
func roundTripTestIntake(id, correlation string, report FileHandle) IntakeRecord {
	return IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: id, Revision: 1,
			CreatedAt: "2026-08-02T18:06:00Z", UpdatedAt: "2026-08-02T18:06:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			CorrelationID: correlation, Digest: stateTestDigest("1"),
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{report},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + report.Path + "#L1-L3",
			SourcePath:   report.Path, SourceSHA256: report.SHA256,
			StartLine: 1, EndLine: 3, SourceLineCount: 3, Disposition: "non-claim",
		}},
		Triage: map[string]string{}, Status: "submitted",
	}
}

// TestSealedIntakeShapeIsNotThePersistedShape pins the root cause of the
// review-packet defect at the level where it originates, independently of any
// campaign fixture.
//
// sealIntakeRecord normalizes nil string slices to empty non-nil slices, and
// canonicalJSON then writes the record with `omitempty`, so those collections
// never reach the file. A caller that decodes the persisted bytes therefore
// cannot reconstruct the in-memory value the loader holds, and no uniform guess
// repairs it: the set of fields that get normalized and the set that carry
// `omitempty` are different sets.
func TestSealedIntakeShapeIsNotThePersistedShape(t *testing.T) {
	record := roundTripTestIntake("I-0901", "corr-round-trip", FileHandle{
		Path:   "active/test-campaign/runs/R-20260802-0091/report.md",
		SHA256: stateTestDigest("8"),
	})
	sealedValue, body, err := sealIntakeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	sealed := sealedValue.(IntakeRecord)

	var persisted IntakeRecord
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(persisted, sealed) {
		t.Fatal("the persisted and sealed intake shapes agree; this test no longer pins the defect")
	}
	// The divergence must be exactly the empty-collection normalization, and
	// nothing the canonical digest can see.
	if persisted.Digest != sealed.Digest {
		t.Fatalf("persisted digest %s does not match sealed digest %s", persisted.Digest, sealed.Digest)
	}
	divergent := []string{}
	for name, pair := range map[string][2]any{
		"conflicts":          {persisted.Conflicts, sealed.Conflicts},
		"spawnedWorkItems":   {persisted.SpawnedWorkItems, sealed.SpawnedWorkItems},
		"retentionDecisions": {persisted.RetentionDecisions, sealed.RetentionDecisions},
		"uncertainties":      {persisted.Uncertainties, sealed.Uncertainties},
		"requestedDecisions": {persisted.RequestedDecisions, sealed.RequestedDecisions},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			divergent = append(divergent, name)
		}
	}
	if len(divergent) == 0 {
		t.Fatal("no empty collection diverged; the fixture no longer reproduces the reported cause")
	}

	// The fix: the two shapes are the same canonical record, and the comparison
	// used at review commit must say so.
	graph := CampaignGraph{Campaign: &CampaignRecord{Slug: "test-campaign"}}
	if err := validateSubmittedIntakeIsCanonical(graph, persisted, sealed); err != nil {
		t.Fatalf("a faithful echo of the persisted bytes was refused: %v", err)
	}
	// A genuinely different record is still refused, and the refusal names the field.
	forged := persisted
	forged.Uncertainties = []string{"Caller-added uncertainty after publication."}
	err = validateSubmittedIntakeIsCanonical(graph, forged, sealed)
	if err == nil {
		t.Fatal("a forged intake was accepted as the canonical record")
	}
	for _, want := range []string{"uncertainties", "I-0901", "active/test-campaign/intake/I-0901.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mismatch refusal does not report %q: %v", want, err)
		}
	}
}

// TestReviewSubmitAcceptsThePersistedIntakeBytes is the end-to-end regression
// for the reported failure: a manager that read the committed intake record and
// echoed it back into review.submit was refused, with an error that named no
// field, and had no way to construct an accepted packet.
func TestReviewSubmitAcceptsThePersistedIntakeBytes(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)

	intake := roundTripTestIntake("I-0901", "corr-round-trip-intake", *source.run.Report)
	curationReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: intake.CorrelationID, IdempotencyKey: "idem-round-trip-intake",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               intake,
	})
	if err != nil {
		t.Fatalf("submit curator intake: %v", err)
	}

	// Read the committed record exactly as an independent client would: the
	// canonical bytes on disk, decoded with the published schema.
	persistedPath := filepath.Join(
		fixture.root, "active", "test-campaign", "intake", intake.ID+".json")
	body, err := os.ReadFile(persistedPath)
	if err != nil {
		t.Fatal(err)
	}
	var echoed IntakeRecord
	if err := json.Unmarshal(body, &echoed); err != nil {
		t.Fatal(err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	committed := graph.Intakes[intake.ID]
	if reflect.DeepEqual(echoed, committed) {
		t.Fatal("the persisted bytes now decode to the loaded record; this test no longer pins the defect")
	}
	// Prove the accepting half against real committed state before the refusal
	// cases, which necessarily run first because a successful review advances
	// the head this request expects.
	if err := validateSubmittedIntakeIsCanonical(graph, echoed, committed); err != nil {
		t.Fatalf("the committed intake bytes are not accepted as the committed intake: %v", err)
	}

	packet := CurationPacket{Intake: echoed}
	envelope := testReviewPacketEnvelope(t, packet)
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0901", Revision: 1,
			CreatedAt: "2026-08-02T18:07:00Z", UpdatedAt: "2026-08-02T18:07:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "corr-round-trip-review",
			Digest: stateTestDigest("2"),
		},
		CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: echoed.ID, IntakeRevision: echoed.Revision, PacketDigest: envelope.Digest,
		ReviewLoad: stateTestReviewLoad("V-0901", "C-TEST", envelope.Digest, 0, 0),
	}
	review.ReviewLoad.StartedAt = "2026-08-02T18:06:10Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T18:06:20Z"
	review.ReviewLoad.DurationSeconds = 10
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	reviewedIntake := echoed
	reviewedIntake.RecordMeta = lifecycleAdvanceMeta(
		reviewedIntake.RecordMeta, "2026-08-02T18:07:00Z", "manager", review.CorrelationID)
	reviewedIntake.Digest = stateTestDigest("3")
	reviewedIntake.Status = "reviewed"

	request := ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: review.CorrelationID, IdempotencyKey: "idem-round-trip-review",
		ExpectedHeadRevision:  curationReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    curationReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{echoed.ID: committed.Digest},
		Intake:                &reviewedIntake, Review: &review,
		ReviewPacket: &ReviewPacketSubmission{Envelope: envelope, Intake: echoed},
	}

	// A genuinely different intake is still refused, and the refusal names the
	// field that differs instead of only asserting inequality.
	forged := request
	forged.IdempotencyKey = "idem-round-trip-forged"
	forgedPacket := *request.ReviewPacket
	forgedPacket.Intake.Uncertainties = []string{"Caller-added uncertainty after packet publication."}
	forgedResulting := *request.Intake
	forgedResulting.Uncertainties = append([]string(nil), forgedPacket.Intake.Uncertainties...)
	forged.Intake = &forgedResulting
	forged.ReviewPacket = &forgedPacket
	_, forgedErr := fixture.service.ManagerApply(ctx, forged)
	if forgedErr == nil {
		t.Fatal("a caller-forged intake was accepted at review commit")
	}
	for _, want := range []string{
		"not the exact canonical submitted intake",
		"uncertainties (submitted",
		"active/test-campaign/intake/I-0901.json",
	} {
		if !strings.Contains(forgedErr.Error(), want) {
			t.Fatalf("mismatch refusal does not report %q: %v", want, forgedErr)
		}
	}

	if _, err := fixture.service.ManagerApply(ctx, request); err != nil {
		t.Fatalf("a manager that echoed the persisted intake bytes could not submit its review: %v", err)
	}
}
