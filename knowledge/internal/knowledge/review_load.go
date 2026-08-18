package knowledge

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// ReviewLoadReceipt is an immutable measurement bound to the exact packet a
// manager reviewed and the configured ceiling in force for that review. It is
// nested in ReviewRecord, whose own immutable digest supplies a second binding.
type ReviewLoadReceipt struct {
	SchemaVersion           int    `json:"schemaVersion"`
	ReviewID                string `json:"reviewId"`
	CampaignID              string `json:"campaignId"`
	PacketDigest            string `json:"packetDigest"`
	MeasurementStatus       string `json:"measurementStatus"`
	SessionID               string `json:"sessionId"`
	PacketOrdinal           int    `json:"packetOrdinal"`
	StartedAt               string `json:"startedAt,omitempty"`
	CompletedAt             string `json:"completedAt,omitempty"`
	DurationSeconds         int64  `json:"durationSeconds"`
	RoutineRows             int    `json:"routineRows"`
	AttentionRows           int    `json:"attentionRows"`
	TargetMinutesPerPacket  int    `json:"targetMinutesPerPacket"`
	TargetPacketsPerSession int    `json:"targetPacketsPerSession"`
	CeilingDigest           string `json:"ceilingDigest"`
	OverTargetKnown         bool   `json:"overTargetKnown"`
	OverTarget              bool   `json:"overTarget"`
	GranularityDecision     string `json:"granularityDecision"`
	GranularityRationale    string `json:"granularityRationale,omitempty"`
	Digest                  string `json:"digest"`
}

type ReviewLoadAggregate struct {
	PacketCount         int      `json:"packetCount"`
	MeasuredPackets     int      `json:"measuredPackets"`
	UnmeasuredPackets   int      `json:"unmeasuredPackets"`
	SessionCount        int      `json:"sessionCount"`
	OverTargetPackets   int      `json:"overTargetPackets"`
	TotalSeconds        int64    `json:"totalSeconds"`
	MaximumSeconds      int64    `json:"maximumSeconds"`
	MaximumOrdinal      int      `json:"maximumOrdinal"`
	CoarsenDecisions    int      `json:"coarsenDecisions"`
	OverTargetReviewIDs []string `json:"overTargetReviewIds,omitempty"`
}

func ReviewLoadCeilingDigest(config ReviewLoadConfig) (string, error) {
	return CanonicalDigest(config)
}

func ReviewLoadReceiptDigest(receipt ReviewLoadReceipt) (string, error) {
	receipt.Digest = ""
	return CanonicalDigest(receipt)
}

func SealReviewLoadReceipt(receipt *ReviewLoadReceipt) error {
	if receipt == nil {
		return errors.New("review-load receipt is required")
	}
	receipt.Digest = ""
	digest, err := ReviewLoadReceiptDigest(*receipt)
	if err != nil {
		return err
	}
	receipt.Digest = digest
	return ValidateReviewLoadReceipt(*receipt)
}

// ValidateReviewLoadReceipt validates the receipt intrinsically. Configuration
// equality is checked separately at submission time, so archived receipts
// remain verifiable after the project changes its future ceiling.
func ValidateReviewLoadReceipt(receipt ReviewLoadReceipt) error {
	if receipt.SchemaVersion != CampaignSchemaVersion || !reviewIDRE.MatchString(receipt.ReviewID) ||
		!campaignIDRE.MatchString(receipt.CampaignID) || !digestRE.MatchString(receipt.PacketDigest) ||
		!validOne(receipt.MeasurementStatus, "measured", "legacy-unmeasured") ||
		!correlationIDRE.MatchString(receipt.SessionID) || receipt.PacketOrdinal < 1 ||
		receipt.DurationSeconds < 0 || receipt.RoutineRows < 0 || receipt.AttentionRows < 0 ||
		receipt.TargetMinutesPerPacket < 1 || receipt.TargetMinutesPerPacket > 240 ||
		receipt.TargetPacketsPerSession < 1 || receipt.TargetPacketsPerSession > 100 ||
		!digestRE.MatchString(receipt.CeilingDigest) || !digestRE.MatchString(receipt.Digest) {
		return errors.New("review-load receipt identity, counts, targets, or digest is invalid")
	}
	ceiling := ReviewLoadConfig{
		TargetMinutesPerPacket:  receipt.TargetMinutesPerPacket,
		TargetPacketsPerSession: receipt.TargetPacketsPerSession,
	}
	ceilingDigest, err := ReviewLoadCeilingDigest(ceiling)
	if err != nil || ceilingDigest != receipt.CeilingDigest {
		return errors.New("review-load ceiling digest does not match its embedded targets")
	}
	if receipt.MeasurementStatus == "legacy-unmeasured" {
		if receipt.StartedAt != "" || receipt.CompletedAt != "" || receipt.DurationSeconds != 0 ||
			receipt.OverTargetKnown || receipt.OverTarget || receipt.GranularityDecision != "unmeasured" ||
			receipt.GranularityRationale == "" {
			return errors.New("legacy-unmeasured review load must preserve an explicit unknown measurement without fabricated timing")
		}
	} else {
		started, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
		if err != nil || started.Location() != time.UTC {
			return errors.New("review-load startedAt must be a UTC RFC3339 timestamp")
		}
		completed, err := time.Parse(time.RFC3339Nano, receipt.CompletedAt)
		if err != nil || completed.Location() != time.UTC || !completed.After(started) {
			return errors.New("review-load completedAt must be a later UTC RFC3339 timestamp")
		}
		if receipt.DurationSeconds < 1 || completed.Sub(started) != time.Duration(receipt.DurationSeconds)*time.Second {
			return errors.New("review-load durationSeconds does not match its timestamps")
		}
		over := receipt.DurationSeconds > int64(receipt.TargetMinutesPerPacket*60) ||
			receipt.PacketOrdinal > receipt.TargetPacketsPerSession
		if !receipt.OverTargetKnown || receipt.OverTarget != over {
			return errors.New("review-load overTarget does not match the measured ceiling")
		}
		if over {
			if receipt.GranularityDecision != "coarsen" || receipt.GranularityRationale == "" {
				return errors.New("over-target review load requires a reasoned coarsen decision")
			}
		} else if receipt.GranularityDecision != "retain" {
			return errors.New("within-target review load requires a retain decision")
		}
	}
	digest, err := ReviewLoadReceiptDigest(receipt)
	if err != nil || digest != receipt.Digest {
		return errors.New("review-load receipt digest mismatch")
	}
	return nil
}

func ValidateReviewLoadBinding(
	receipt ReviewLoadReceipt,
	review ReviewRecord,
	packet CurationPacket,
	config ReviewLoadConfig,
) error {
	if err := ValidateReviewLoadReceipt(receipt); err != nil {
		return err
	}
	if receipt.ReviewID != review.ID || receipt.CampaignID != review.CampaignID ||
		receipt.PacketDigest != review.PacketDigest {
		return errors.New("review-load receipt does not bind the review and packet")
	}
	routine, attention := 0, 0
	for _, findingID := range packet.Intake.CandidateFindingIDs {
		switch packet.Intake.Triage[findingID] {
		case "routine":
			routine++
		case "attention":
			attention++
		}
	}
	if receipt.RoutineRows != routine || receipt.AttentionRows != attention {
		return errors.New("review-load receipt row counts do not match packet triage")
	}
	digest, err := ReviewLoadCeilingDigest(config)
	if err != nil {
		return err
	}
	if receipt.TargetMinutesPerPacket != config.TargetMinutesPerPacket ||
		receipt.TargetPacketsPerSession != config.TargetPacketsPerSession ||
		receipt.CeilingDigest != digest {
		return errors.New("review-load receipt does not bind the configured ceiling")
	}
	packetCreatedAt := packet.Intake.UpdatedAt
	if packetCreatedAt == "" {
		packetCreatedAt = packet.Intake.CreatedAt
	}
	if packetCreatedAt != "" {
		if err := ValidateReviewLoadTemporalBinding(receipt, packetCreatedAt, review); err != nil {
			return err
		}
	}
	return nil
}

// ValidateReviewLoadTemporalBinding proves that a measured receipt was
// collected while the exact packet existed and before the immutable review
// record was created. This prevents a valid old stopwatch receipt from being
// rebound to a later packet with matching row counts.
func ValidateReviewLoadTemporalBinding(
	receipt ReviewLoadReceipt,
	packetCreatedAt string,
	review ReviewRecord,
) error {
	if receipt.MeasurementStatus != "measured" {
		return nil
	}
	packetTime, err := time.Parse(time.RFC3339Nano, packetCreatedAt)
	if err != nil || packetTime.Location() != time.UTC {
		return errors.New("review-load packet timestamp must be UTC RFC3339")
	}
	reviewTime, err := time.Parse(time.RFC3339Nano, review.CreatedAt)
	if err != nil || reviewTime.Location() != time.UTC || reviewTime.Before(packetTime) {
		return errors.New("review-load review timestamp must follow the packet timestamp")
	}
	started, _ := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	completed, _ := time.Parse(time.RFC3339Nano, receipt.CompletedAt)
	if started.Before(packetTime) || completed.After(reviewTime) {
		return errors.New("review-load measurement must fall within the packet-to-review interval")
	}
	return nil
}

func ValidateReviewLoadSessions(reviews map[string]ReviewRecord) error {
	ordinals := map[string]map[int]string{}
	for id, review := range reviews {
		receipt := review.ReviewLoad
		if err := ValidateReviewLoadReceipt(receipt); err != nil {
			return fmt.Errorf("review %s load receipt: %w", id, err)
		}
		if ordinals[receipt.SessionID] == nil {
			ordinals[receipt.SessionID] = map[int]string{}
		}
		if previous := ordinals[receipt.SessionID][receipt.PacketOrdinal]; previous != "" {
			return fmt.Errorf("reviews %s and %s repeat packet ordinal %d in session %s",
				previous, id, receipt.PacketOrdinal, receipt.SessionID)
		}
		ordinals[receipt.SessionID][receipt.PacketOrdinal] = id
	}
	for session, values := range ordinals {
		for ordinal := 1; ordinal <= len(values); ordinal++ {
			if values[ordinal] == "" {
				return fmt.Errorf("review-load session %s has a gap before packet ordinal %d", session, ordinal)
			}
		}
	}
	return nil
}

func AggregateReviewLoad(reviews map[string]ReviewRecord) ReviewLoadAggregate {
	result := ReviewLoadAggregate{OverTargetReviewIDs: []string{}}
	sessions := map[string]bool{}
	for _, review := range reviews {
		receipt := review.ReviewLoad
		if ValidateReviewLoadReceipt(receipt) != nil {
			continue
		}
		result.PacketCount++
		if receipt.MeasurementStatus == "measured" {
			result.MeasuredPackets++
			result.TotalSeconds += receipt.DurationSeconds
			if receipt.DurationSeconds > result.MaximumSeconds {
				result.MaximumSeconds = receipt.DurationSeconds
			}
		} else {
			result.UnmeasuredPackets++
		}
		if receipt.PacketOrdinal > result.MaximumOrdinal {
			result.MaximumOrdinal = receipt.PacketOrdinal
		}
		sessions[receipt.SessionID] = true
		if receipt.OverTarget {
			result.OverTargetPackets++
			result.OverTargetReviewIDs = append(result.OverTargetReviewIDs, review.ID)
		}
		if receipt.GranularityDecision == "coarsen" {
			result.CoarsenDecisions++
		}
	}
	result.SessionCount = len(sessions)
	sort.Strings(result.OverTargetReviewIDs)
	return result
}
