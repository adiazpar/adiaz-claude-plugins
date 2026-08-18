package knowledge

import (
	"strings"
	"testing"
)

func measuredReviewLoad(t *testing.T, minutes int, ordinal int, routine, attention int) ReviewLoadReceipt {
	t.Helper()
	config := ReviewLoadConfig{TargetMinutesPerPacket: 12, TargetPacketsPerSession: 6}
	ceiling, err := ReviewLoadCeilingDigest(config)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ReviewLoadReceipt{
		SchemaVersion: CampaignSchemaVersion, ReviewID: "V-0042",
		CampaignID: "C-RESOURCE-REGISTRATION", PacketDigest: stateTestDigest("9"),
		MeasurementStatus: "measured", OverTargetKnown: true,
		SessionID: "pilot-session-1", PacketOrdinal: ordinal,
		StartedAt:       "2026-08-02T19:00:00Z",
		CompletedAt:     "2026-08-02T19:10:00Z",
		DurationSeconds: int64(minutes * 60), RoutineRows: routine, AttentionRows: attention,
		TargetMinutesPerPacket:  config.TargetMinutesPerPacket,
		TargetPacketsPerSession: config.TargetPacketsPerSession, CeilingDigest: ceiling,
		GranularityDecision: "retain",
	}
	// Keep the timestamp/duration pair exact for both test cases.
	if minutes == 13 {
		receipt.CompletedAt = "2026-08-02T19:13:00Z"
	}
	receipt.OverTarget = minutes > config.TargetMinutesPerPacket || ordinal > config.TargetPacketsPerSession
	if receipt.OverTarget {
		receipt.GranularityDecision = "coarsen"
		receipt.GranularityRationale = "Combine tightly coupled observations before the next packet."
	}
	if err := SealReviewLoadReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestReviewLoadReceiptPreservesLegacyUnknownWithoutFabricatedTiming(t *testing.T) {
	config := DefaultBootstrapConfig().ReviewLoad
	ceiling, err := ReviewLoadCeilingDigest(config)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ReviewLoadReceipt{
		SchemaVersion: CampaignSchemaVersion, ReviewID: "V-0042", CampaignID: "C-RESOURCE-REGISTRATION",
		PacketDigest: stateTestDigest("9"), MeasurementStatus: "legacy-unmeasured",
		SessionID: "migration-legacy-review", PacketOrdinal: 1, RoutineRows: 0, AttentionRows: 1,
		TargetMinutesPerPacket: config.TargetMinutesPerPacket, TargetPacketsPerSession: config.TargetPacketsPerSession,
		CeilingDigest: ceiling, GranularityDecision: "unmeasured",
		GranularityRationale: "Legacy evidence contains no contemporaneous review-load measurement.",
	}
	if err := SealReviewLoadReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.DurationSeconds != 0 || receipt.OverTargetKnown || receipt.StartedAt != "" || receipt.CompletedAt != "" {
		t.Fatalf("legacy unknown receipt invented a measurement: %+v", receipt)
	}
	if err := ValidateManagerReview("manager", CurationPacket{}, ReviewRecord{
		Authority: "manager", ReviewLoad: receipt,
	}); err == nil || !strings.Contains(err.Error(), "contemporaneously measured") {
		t.Fatalf("legacy-unmeasured receipt was accepted for a new manager review: %v", err)
	}
	tampered := receipt
	tampered.DurationSeconds = 1
	tampered.Digest, _ = ReviewLoadReceiptDigest(tampered)
	if err := ValidateReviewLoadReceipt(tampered); err == nil || !strings.Contains(err.Error(), "without fabricated timing") {
		t.Fatalf("fabricated legacy timing was accepted: %v", err)
	}
}

func TestReviewLoadReceiptBelowAndOverConfiguredCeiling(t *testing.T) {
	below := measuredReviewLoad(t, 10, 1, 3, 0)
	if below.OverTarget || below.GranularityDecision != "retain" {
		t.Fatalf("within-target receipt was misclassified: %+v", below)
	}
	over := measuredReviewLoad(t, 13, 1, 2, 1)
	if !over.OverTarget || over.GranularityDecision != "coarsen" {
		t.Fatalf("over-target receipt omitted the granularity decision: %+v", over)
	}
	over.GranularityDecision = "retain"
	over.GranularityRationale = ""
	over.Digest, _ = ReviewLoadReceiptDigest(over)
	if err := ValidateReviewLoadReceipt(over); err == nil || !strings.Contains(err.Error(), "coarsen") {
		t.Fatalf("over-target receipt without coarsening was accepted: %v", err)
	}
}

func TestReviewLoadReceiptRejectsTamperingAndWrongConfiguration(t *testing.T) {
	receipt := measuredReviewLoad(t, 10, 1, 3, 0)
	tampered := receipt
	tampered.DurationSeconds++
	if err := ValidateReviewLoadReceipt(tampered); err == nil {
		t.Fatal("tampered review-load measurement was accepted")
	}
	review := ReviewRecord{
		RecordMeta: stateTestMeta("V-0042", 1), CampaignID: receipt.CampaignID,
		PacketDigest: receipt.PacketDigest, ReviewLoad: receipt,
	}
	packet := CurationPacket{Intake: IntakeRecord{
		CandidateFindingIDs: []string{"F-0042", "F-0043", "F-0044"},
		Triage:              map[string]string{"F-0042": "routine", "F-0043": "routine", "F-0044": "routine"},
	}}
	wrong := ReviewLoadConfig{TargetMinutesPerPacket: 15, TargetPacketsPerSession: 6}
	if err := ValidateReviewLoadBinding(receipt, review, packet, wrong); err == nil ||
		!strings.Contains(err.Error(), "configured ceiling") {
		t.Fatalf("receipt was rebound to a different configured ceiling: %v", err)
	}
}

func TestReviewLoadBindingRejectsMeasurementOutsidePacketReviewInterval(t *testing.T) {
	receipt := measuredReviewLoad(t, 10, 1, 3, 0)
	review := ReviewRecord{
		RecordMeta: RecordMeta{ID: "V-0042", CreatedAt: "2026-08-02T19:15:00Z"},
		CampaignID: receipt.CampaignID, PacketDigest: receipt.PacketDigest,
	}
	packet := CurationPacket{Intake: IntakeRecord{
		RecordMeta:          RecordMeta{CreatedAt: "2026-08-02T19:05:00Z", UpdatedAt: "2026-08-02T19:05:00Z"},
		CandidateFindingIDs: []string{"F-0042", "F-0043", "F-0044"},
		Triage: map[string]string{
			"F-0042": "routine", "F-0043": "routine", "F-0044": "routine",
		},
	}}
	config := ReviewLoadConfig{TargetMinutesPerPacket: 12, TargetPacketsPerSession: 6}
	if err := ValidateReviewLoadBinding(receipt, review, packet, config); err == nil ||
		!strings.Contains(err.Error(), "packet-to-review interval") {
		t.Fatalf("historical measurement was rebound to a later packet: %v", err)
	}
}

func TestReviewLoadSessionsRejectDuplicateOrdinalAndAggregateBoundedly(t *testing.T) {
	first := measuredReviewLoad(t, 10, 1, 3, 0)
	second := measuredReviewLoad(t, 13, 1, 2, 1)
	second.ReviewID = "V-0043"
	second.Digest, _ = ReviewLoadReceiptDigest(second)
	reviews := map[string]ReviewRecord{
		"V-0042": {RecordMeta: stateTestMeta("V-0042", 1), ReviewLoad: first},
		"V-0043": {RecordMeta: stateTestMeta("V-0043", 1), ReviewLoad: second},
	}
	if err := ValidateReviewLoadSessions(reviews); err == nil || !strings.Contains(err.Error(), "repeat packet ordinal") {
		t.Fatalf("duplicate session ordinal was accepted: %v", err)
	}
	second.PacketOrdinal = 2
	second.Digest, _ = ReviewLoadReceiptDigest(second)
	reviews["V-0043"] = ReviewRecord{RecordMeta: stateTestMeta("V-0043", 1), ReviewLoad: second}
	if err := ValidateReviewLoadSessions(reviews); err != nil {
		t.Fatal(err)
	}
	aggregate := AggregateReviewLoad(reviews)
	if aggregate.PacketCount != 2 || aggregate.SessionCount != 1 || aggregate.OverTargetPackets != 1 ||
		aggregate.CoarsenDecisions != 1 || len(aggregate.OverTargetReviewIDs) != 1 {
		t.Fatalf("review-load aggregate lost bounded pilot outputs: %+v", aggregate)
	}
}
