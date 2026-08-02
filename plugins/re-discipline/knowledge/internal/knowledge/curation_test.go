package knowledge

import (
	"fmt"
	"strings"
	"testing"
)

func testCurationPacket(t *testing.T) CurationPacket {
	t.Helper()
	candidates := []FindingDocument{}
	ids := []string{"F-0042", "F-0043", "F-0044"}
	for index, id := range ids {
		document := testFindingDocument()
		document.Record.ID = id
		document.Record.Path = "active/resource-registration/findings/" + id + ".md"
		document.Record.Claim = fmt.Sprintf("Atomic resource registration claim %d is scoped to the recorded build.", index+1)
		document.Record.Body = strings.Replace(document.Record.Body,
			"Resource registration uses the named table.", document.Record.Claim, 1)
		body, err := RenderFindingDocument(document)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseFindingDocument(body, document.Record.Path)
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, parsed)
	}
	meta := RecordMeta{
		SchemaVersion: CampaignSchemaVersion, ID: "I-0042",
		CreatedAt: "2026-08-02T19:30:00Z", UpdatedAt: "2026-08-02T19:30:00Z",
		Revision: 1, CreatedBy: "curator:test", UpdatedBy: "curator:test",
		Digest: "sha256:" + strings.Repeat("b", 64), CorrelationID: "corr-curation-42",
	}
	coverage := []CoverageEntry{}
	rows := []CurationRow{}
	triage := map[string]string{}
	for index, id := range ids {
		coverage = append(coverage, CoverageEntry{
			SourceHandle: fmt.Sprintf("report:lines:%d-%d", 10+index, 10+index),
			Disposition:  "candidate-finding", TargetID: id,
		})
		rows = append(rows, CurationRow{FindingID: id, Triage: "routine"})
		triage[id] = "routine"
	}
	return CurationPacket{
		Intake: IntakeRecord{
			RecordMeta: meta, CampaignID: "C-RESOURCE-REGISTRATION",
			SourceRuns: []FileHandle{{
				Path:   "active/resource-registration/runs/R-20260802-0042/report.md",
				SHA256: "sha256:" + strings.Repeat("c", 64),
			}},
			CandidateFindingIDs: ids, Coverage: coverage, Triage: triage, Status: "submitted",
		},
		Candidates: candidates, Rows: rows,
	}
}

func TestCurationPacketEnforcesCuratorBoundaryAndCoverage(t *testing.T) {
	packet := testCurationPacket(t)
	if err := ValidateCurationPacket("curator", packet); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurationPacket("manager", packet); err == nil {
		t.Fatal("manager self-declaration was accepted as a curator submission")
	}
	packet.Candidates[0].Record.ReviewState = "manager-ratified"
	if err := ValidateCurationPacket("curator", packet); err == nil || !strings.Contains(err.Error(), "cannot submit") {
		t.Fatalf("curator ratification was not rejected: %v", err)
	}
}

func TestCurationPacketRequiresAttentionForConflicts(t *testing.T) {
	packet := testCurationPacket(t)
	packet.Candidates[0].Record.Relations.Contradicts = []string{"F-0099"}
	body, err := RenderFindingDocument(packet.Candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	packet.Candidates[0], err = ParseFindingDocument(body, packet.Candidates[0].Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurationPacket("curator", packet); err == nil || !strings.Contains(err.Error(), "attention") {
		t.Fatalf("conflicted routine row was accepted: %v", err)
	}
	packet.Rows[0].Triage = "attention"
	packet.Intake.Triage[packet.Rows[0].FindingID] = "attention"
	if err := ValidateCurationPacket("curator", packet); err != nil {
		t.Fatal(err)
	}
}

func testManagerReview(packet CurationPacket) ReviewRecord {
	decisions := []ReviewDecision{}
	for _, candidate := range packet.Candidates {
		decisions = append(decisions, ReviewDecision{
			FindingID: candidate.Record.ID, FindingRevision: candidate.Record.Revision,
			Action: "ratify", Projection: "campaign", Rationale: "reviewed exact evidence",
		})
	}
	return ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0042",
			CreatedAt: "2026-08-02T19:40:00Z", UpdatedAt: "2026-08-02T19:40:00Z",
			Revision: 1, CreatedBy: "manager:test", UpdatedBy: "manager:test",
			Digest: "sha256:" + strings.Repeat("d", 64), CorrelationID: "corr-review-42",
		},
		CampaignID: packet.Intake.CampaignID, Reviewer: "manager:test", Authority: "manager",
		IntakeID: packet.Intake.ID, IntakeRevision: packet.Intake.Revision,
		PacketDigest: stateTestDigest("9"), Decisions: decisions,
	}
}

func testReviewPacketEnvelope(t *testing.T, packet CurationPacket) ReviewPacketEnvelope {
	t.Helper()
	envelope := ReviewPacketEnvelope{
		SchemaVersion: CampaignSchemaVersion, ID: "review-packet-I-0042-r1",
		CampaignID: packet.Intake.CampaignID, IntakeID: packet.Intake.ID,
		IntakeRevision: packet.Intake.Revision, CoverageComplete: true,
		CreatedAt: "2026-08-02T19:35:00Z",
	}
	for _, candidate := range packet.Candidates {
		envelope.Rows = append(envelope.Rows, ReviewPacketRow{
			FindingID: candidate.Record.ID, FindingRevision: candidate.Record.Revision,
			Claim: candidate.Record.Claim, EvidenceGrade: candidate.Record.EvidenceGrade,
			Triage:     packet.Intake.Triage[candidate.Record.ID],
			Conflicted: findingIsConflicted(candidate.Record), Recommendation: "Review candidate",
		})
	}
	var err error
	envelope.Digest, err = ReviewPacketDigest(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func testReviewOutcomes(packet CurationPacket, review ReviewRecord) (IntakeRecord, []FindingDocument) {
	intake := packet.Intake
	intake.Revision++
	intake.UpdatedAt, intake.UpdatedBy, intake.Status = "2026-08-02T19:40:00Z", "manager:test", "reviewed"
	outcomes := make([]FindingDocument, 0, len(packet.Candidates))
	for index, candidate := range packet.Candidates {
		outcome := candidate
		outcome.Record.Revision++
		outcome.Record.UpdatedAt, outcome.Record.UpdatedBy = intake.UpdatedAt, intake.UpdatedBy
		decision := review.Decisions[index]
		switch decision.Action {
		case "ratify":
			outcome.Record.ReviewState = "manager-ratified"
			if decision.Projection != "" {
				outcome.Record.Projection = decision.Projection
			}
		case "reject":
			outcome.Record.ReviewState, outcome.Record.Validity, outcome.Record.Projection = "manager-rejected", "invalid", "rejected"
		}
		outcomes = append(outcomes, outcome)
	}
	return intake, outcomes
}

func TestManagerReviewBindsEveryFindingRevisionAndIsImmutable(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(packet)
	if err := ValidateManagerReview("manager", packet, review); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManagerReview("curator", packet, review); err == nil {
		t.Fatal("curator was allowed to ratify")
	}
	missing := review
	missing.Decisions = append([]ReviewDecision(nil), review.Decisions[:2]...)
	if err := ValidateManagerReview("manager", packet, missing); err == nil {
		t.Fatal("partial review receipt was accepted")
	}
	invalidPacket := packet
	invalidPacket.Candidates = append([]FindingDocument(nil), packet.Candidates...)
	invalidPacket.Candidates[0].Record.Relations.Contradicts = []string{"F-0099"}
	body, err := RenderFindingDocument(invalidPacket.Candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	invalidPacket.Candidates[0], err = ParseFindingDocument(body, invalidPacket.Candidates[0].Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManagerReview("manager", invalidPacket, review); err == nil || !strings.Contains(err.Error(), "attention") {
		t.Fatalf("manager review bypassed invalid curator triage: %v", err)
	}
	digest, err := ReviewReceiptDigest(review)
	if err != nil {
		t.Fatal(err)
	}
	review.Digest = digest
	identical := review
	identical.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	if err := VerifyImmutableReviewReceipt(review, identical); err != nil {
		t.Fatal(err)
	}
	invalidDigest := identical
	invalidDigest.Digest = "sha256:" + strings.Repeat("e", 64)
	if err := VerifyImmutableReviewReceipt(invalidDigest, invalidDigest); err == nil || !strings.Contains(err.Error(), "invalid content digest") {
		t.Fatalf("self-consistent but unsealed receipt was accepted: %v", err)
	}
	identical.Decisions[0].Rationale = "rewritten after publication"
	identical.Digest, err = ReviewReceiptDigest(identical)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableReviewReceipt(review, identical); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("mutated receipt was accepted: %v", err)
	}
}

func TestReviewPacketEnvelopeBindsDisplayedRowsAndPersistedTriage(t *testing.T) {
	packet := testCurationPacket(t)
	envelope := testReviewPacketEnvelope(t, packet)
	if err := ValidateReviewPacketEnvelope(envelope, packet); err != nil {
		t.Fatal(err)
	}
	tampered := envelope
	tampered.Rows = append([]ReviewPacketRow(nil), envelope.Rows...)
	tampered.Rows[0].Claim = "A different claim was displayed to the manager."
	tampered.Digest, _ = ReviewPacketDigest(tampered)
	if err := ValidateReviewPacketEnvelope(tampered, packet); err == nil || !strings.Contains(err.Error(), "persisted candidate") {
		t.Fatalf("packet display substitution was accepted: %v", err)
	}
	tampered = envelope
	tampered.Rows = append([]ReviewPacketRow(nil), envelope.Rows...)
	tampered.Rows[0].Triage = "attention"
	tampered.Digest, _ = ReviewPacketDigest(tampered)
	if err := ValidateReviewPacketEnvelope(tampered, packet); err == nil || !strings.Contains(err.Error(), "triage") {
		t.Fatalf("packet triage substitution was accepted: %v", err)
	}
}

func TestManagerReviewDecisionsRequireExactFindingOutcomes(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(packet)
	envelope := testReviewPacketEnvelope(t, packet)
	review.PacketDigest = envelope.Digest
	intake, outcomes := testReviewOutcomes(packet, review)
	if err := ValidateManagerReviewOutcomes(packet, review, intake, outcomes); err != nil {
		t.Fatal(err)
	}

	reject := review
	reject.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	reject.Decisions[0].Action, reject.Decisions[0].Projection = "reject", "rejected"
	if err := ValidateManagerReviewOutcomes(packet, reject, intake, outcomes); err == nil || !strings.Contains(err.Error(), "do not match reject") {
		t.Fatalf("reject receipt published a ratified finding: %v", err)
	}
	_, rejectedOutcomes := testReviewOutcomes(packet, reject)
	if err := ValidateManagerReviewOutcomes(packet, reject, intake, rejectedOutcomes); err != nil {
		t.Fatalf("valid reject outcome was rejected: %v", err)
	}

	omitted := append([]FindingDocument(nil), outcomes[1:]...)
	if err := ValidateManagerReviewOutcomes(packet, review, intake, omitted); err == nil || !strings.Contains(err.Error(), "no resulting finding revision") {
		t.Fatalf("decision without a finding outcome was accepted: %v", err)
	}

	wrongRevision := append([]FindingDocument(nil), outcomes...)
	wrongRevision[0].Record.Revision++
	if err := ValidateManagerReviewOutcomes(packet, review, intake, wrongRevision); err == nil || !strings.Contains(err.Error(), "advance reviewed revision") {
		t.Fatalf("decision was rebound to the wrong finding revision: %v", err)
	}

	attention := review
	attention.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	attention.Decisions[0].Action = "challenge"
	if err := ValidateManagerReviewOutcomes(packet, attention, intake, outcomes); err == nil || !strings.Contains(err.Error(), "individual attention") {
		t.Fatalf("attention decision used a routine intake row: %v", err)
	}
}

func TestAppliedManagerReviewBindsDecisionsToCanonicalFindingRevision(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(packet)
	intake, outcomes := testReviewOutcomes(packet, review)
	graph := NewCampaignGraph()
	graph.Intakes[packet.Intake.ID] = packet.Intake
	for _, candidate := range packet.Candidates {
		graph.Findings[candidate.Record.ID] = candidate.Record
	}
	canonical := graph.Findings[packet.Candidates[0].Record.ID]
	canonical.Revision++
	graph.Findings[canonical.ID] = canonical
	writes := []preparedStateWrite{{Record: review}, {Record: intake}}
	for _, outcome := range outcomes {
		writes = append(writes, preparedStateWrite{Record: outcome})
	}
	if err := validateAppliedManagerReview(graph, writes); err == nil || !strings.Contains(err.Error(), "current finding revision") {
		t.Fatalf("review packet was allowed to substitute a stale canonical finding: %v", err)
	}
}
