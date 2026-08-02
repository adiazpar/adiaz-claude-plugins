package knowledge

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type CurationRow struct {
	FindingID string `json:"findingId"`
	Triage    string `json:"triage"`
}

type CurationPacket struct {
	Intake     IntakeRecord      `json:"intake"`
	Candidates []FindingDocument `json:"candidates"`
	Rows       []CurationRow     `json:"rows"`
}

// ReviewPacketRow is the compact, schema-aligned row the manager actually
// reviews. It binds display content and triage to one immutable finding
// revision so a transport cannot show one claim and submit a decision for
// another.
type ReviewPacketRow struct {
	FindingID       string   `json:"findingId"`
	FindingRevision int64    `json:"findingRevision"`
	Claim           string   `json:"claim"`
	EvidenceGrade   string   `json:"evidenceGrade"`
	Triage          string   `json:"triage"`
	Conflicted      bool     `json:"conflicted"`
	Recommendation  string   `json:"recommendation"`
	EvidenceHandles []string `json:"evidenceHandles,omitempty"`
}

// ReviewPacketEnvelope matches review-packet.schema.json. The digest is over
// the normalized envelope with Digest blank, making row order and evidence
// handle order transport-independent while retaining every reviewed field.
type ReviewPacketEnvelope struct {
	SchemaVersion    int               `json:"schemaVersion"`
	ID               string            `json:"id"`
	CampaignID       string            `json:"campaignId"`
	IntakeID         string            `json:"intakeId"`
	IntakeRevision   int64             `json:"intakeRevision"`
	Rows             []ReviewPacketRow `json:"rows"`
	CoverageComplete bool              `json:"coverageComplete"`
	CreatedAt        string            `json:"createdAt"`
	Digest           string            `json:"digest"`
}

func normalizeReviewPacketEnvelope(packet ReviewPacketEnvelope) ReviewPacketEnvelope {
	packet.Rows = append([]ReviewPacketRow(nil), packet.Rows...)
	for index := range packet.Rows {
		packet.Rows[index].EvidenceHandles = SortedUnique(packet.Rows[index].EvidenceHandles)
	}
	sort.Slice(packet.Rows, func(i, j int) bool {
		return packet.Rows[i].FindingID < packet.Rows[j].FindingID
	})
	return packet
}

func ReviewPacketDigest(packet ReviewPacketEnvelope) (string, error) {
	packet = normalizeReviewPacketEnvelope(packet)
	packet.Digest = ""
	return CanonicalDigest(packet)
}

// ValidateReviewPacketEnvelope proves that the compact manager-facing packet
// is a faithful projection of the persisted intake and candidate revisions.
func ValidateReviewPacketEnvelope(envelope ReviewPacketEnvelope, packet CurationPacket) error {
	if envelope.SchemaVersion != CampaignSchemaVersion || strings.TrimSpace(envelope.ID) == "" ||
		envelope.CampaignID != packet.Intake.CampaignID || envelope.IntakeID != packet.Intake.ID ||
		envelope.IntakeRevision != packet.Intake.Revision || !envelope.CoverageComplete ||
		validateUTC(envelope.CreatedAt) != nil || !digestRE.MatchString(envelope.Digest) {
		return errors.New("review packet envelope requires schema identity, complete coverage, timestamp, and digest")
	}
	digest, err := ReviewPacketDigest(envelope)
	if err != nil {
		return err
	}
	if digest != envelope.Digest {
		return errors.New("review packet envelope digest does not match its reviewed contents")
	}
	if len(envelope.Rows) != len(packet.Candidates) {
		return errors.New("review packet rows must match intake candidates exactly")
	}
	candidates := map[string]FindingRecord{}
	for _, candidate := range packet.Candidates {
		candidates[candidate.Record.ID] = candidate.Record
	}
	seen := map[string]bool{}
	for _, row := range envelope.Rows {
		candidate, present := candidates[row.FindingID]
		if !present || seen[row.FindingID] || row.FindingRevision != candidate.Revision ||
			row.Claim != candidate.Claim || row.EvidenceGrade != candidate.EvidenceGrade ||
			row.Triage != packet.Intake.Triage[row.FindingID] ||
			row.Conflicted != findingIsConflicted(candidate) ||
			strings.TrimSpace(row.Recommendation) == "" {
			return fmt.Errorf("review packet row %s does not bind the persisted candidate revision and triage", row.FindingID)
		}
		if len(SortedUnique(row.EvidenceHandles)) != len(row.EvidenceHandles) {
			return fmt.Errorf("review packet row %s repeats an evidence handle", row.FindingID)
		}
		for _, handle := range row.EvidenceHandles {
			if strings.TrimSpace(handle) == "" {
				return fmt.Errorf("review packet row %s has an empty evidence handle", row.FindingID)
			}
		}
		seen[row.FindingID] = true
	}
	return nil
}

func findingRequiresAttention(record FindingRecord) bool {
	return findingIsConflicted(record) || record.Projection == "truth"
}

func findingIsConflicted(record FindingRecord) bool {
	return len(record.Relations.Contradicts) > 0 || record.Validity == "challenged"
}

// ValidateCurationPacket enforces the non-ratifying curator boundary in the
// engine representation, independent of host claims or hook coverage.
func ValidateCurationPacket(role string, packet CurationPacket) error {
	if role != "curator" {
		return errors.New("curation submission requires the curator role")
	}
	if err := ValidateIntake(packet.Intake); err != nil {
		return err
	}
	if packet.Intake.Status != "submitted" {
		return errors.New("curation packet must be submitted before manager review")
	}
	if len(packet.Candidates) < 3 || len(packet.Candidates) > 10 {
		return errors.New("curation packet must contain 3-10 related findings")
	}
	declared := map[string]bool{}
	for _, id := range packet.Intake.CandidateFindingIDs {
		declared[id] = true
	}
	rows := map[string]string{}
	for _, row := range packet.Rows {
		if !declared[row.FindingID] || rows[row.FindingID] != "" ||
			!validOne(row.Triage, "routine", "attention") || packet.Intake.Triage[row.FindingID] != row.Triage {
			return errors.New("every curation row requires a unique candidate and routine/attention triage")
		}
		rows[row.FindingID] = row.Triage
	}
	seen := map[string]bool{}
	for _, candidate := range packet.Candidates {
		record := candidate.Record
		if !declared[record.ID] || seen[record.ID] {
			return fmt.Errorf("candidate finding %s is absent or repeated in intake", record.ID)
		}
		seen[record.ID] = true
		if rows[record.ID] == "" {
			return fmt.Errorf("candidate finding %s has no triage row", record.ID)
		}
		if record.CampaignID != packet.Intake.CampaignID {
			return fmt.Errorf("candidate finding %s belongs to another campaign", record.ID)
		}
		if !validOne(record.ReviewState, "extracted", "curator-checked") {
			return fmt.Errorf("curator cannot submit finding %s with review state %s", record.ID, record.ReviewState)
		}
		if record.Validity == "current" || record.ReviewState == "manager-ratified" ||
			record.ReviewState == "manager-rejected" {
			return fmt.Errorf("curator cannot make an authoritative decision for %s", record.ID)
		}
		if err := ValidateFindingDocument(candidate, true); err != nil {
			return fmt.Errorf("candidate %s: %w", record.ID, err)
		}
		if findingRequiresAttention(record) && rows[record.ID] != "attention" {
			return fmt.Errorf("conflicted or truth-touching finding %s requires attention triage", record.ID)
		}
	}
	if len(seen) != len(declared) || len(packet.Rows) != len(declared) {
		return errors.New("intake candidates, finding records, and triage rows must match exactly")
	}
	for _, coverage := range packet.Intake.Coverage {
		if validOne(coverage.Disposition, "candidate-finding", "duplicate") && !declared[coverage.TargetID] {
			return fmt.Errorf("coverage target %s is not in the packet", coverage.TargetID)
		}
	}
	return nil
}

// ValidateManagerReview binds an immutable manager receipt to one intake
// revision and requires an individual decision for every candidate.
func ValidateManagerReview(role string, packet CurationPacket, review ReviewRecord) error {
	if role != "manager" || review.Authority != "manager" {
		return errors.New("finding ratification requires manager authority")
	}
	if err := ValidateCurationPacket("curator", packet); err != nil {
		return fmt.Errorf("review packet is invalid: %w", err)
	}
	if err := ValidateReview(review); err != nil {
		return err
	}
	if review.CampaignID != packet.Intake.CampaignID || review.IntakeID != packet.Intake.ID ||
		review.IntakeRevision != packet.Intake.Revision {
		return errors.New("review receipt does not bind the submitted intake revision")
	}
	decisions := map[string]ReviewDecision{}
	for _, decision := range review.Decisions {
		decisions[decision.FindingID] = decision
	}
	for _, candidate := range packet.Candidates {
		decision, present := decisions[candidate.Record.ID]
		if !present || decision.FindingRevision != candidate.Record.Revision {
			return fmt.Errorf("review is missing the exact revision of finding %s", candidate.Record.ID)
		}
	}
	if len(decisions) != len(packet.Candidates) {
		return errors.New("review decisions must match packet candidates exactly")
	}
	return nil
}

// ValidateManagerReviewOutcomes binds every decision to the exact state change
// it authorizes. A receipt is not sufficient by itself: the same transaction
// must advance the reviewed intake and every candidate revision to an outcome
// compatible with the recorded action.
func ValidateManagerReviewOutcomes(packet CurationPacket, review ReviewRecord, resultingIntake IntakeRecord, outcomes []FindingDocument) error {
	if err := ValidateManagerReview("manager", packet, review); err != nil {
		return err
	}
	candidates := map[string]FindingRecord{}
	for _, candidate := range packet.Candidates {
		candidates[candidate.Record.ID] = candidate.Record
	}
	resulting := make([]FindingRecord, 0, len(outcomes))
	for _, outcome := range outcomes {
		resulting = append(resulting, outcome.Record)
	}
	return validateManagerReviewRecordOutcomes(packet.Intake, candidates, review, resultingIntake, resulting)
}

func validateManagerReviewRecordOutcomes(
	intake IntakeRecord,
	candidates map[string]FindingRecord,
	review ReviewRecord,
	resultingIntake IntakeRecord,
	outcomes []FindingRecord,
) error {
	if intake.Status != "submitted" || review.IntakeID != intake.ID || review.IntakeRevision != intake.Revision {
		return errors.New("review must bind the current submitted intake revision")
	}
	if err := ValidateIntakeTransition(&intake, resultingIntake); err != nil {
		return fmt.Errorf("resulting reviewed intake: %w", err)
	}
	if resultingIntake.Status != "reviewed" {
		return errors.New("review transaction must advance the intake to reviewed")
	}
	beforeContent := intake
	afterContent := resultingIntake
	beforeContent.RecordMeta, afterContent.RecordMeta = RecordMeta{}, RecordMeta{}
	beforeContent.Status, afterContent.Status = "", ""
	beforeDigest, err := CanonicalDigest(beforeContent)
	if err != nil {
		return err
	}
	afterDigest, err := CanonicalDigest(afterContent)
	if err != nil {
		return err
	}
	if beforeDigest != afterDigest {
		return errors.New("review transaction may only advance intake metadata and status")
	}
	if len(candidates) != len(intake.CandidateFindingIDs) {
		return errors.New("review candidates do not match the persisted intake")
	}
	for _, findingID := range intake.CandidateFindingIDs {
		if _, ok := candidates[findingID]; !ok {
			return fmt.Errorf("review candidate %s is missing from the persisted intake", findingID)
		}
	}
	decisions := map[string]ReviewDecision{}
	for _, decision := range review.Decisions {
		candidate, ok := candidates[decision.FindingID]
		if !ok || decision.FindingRevision != candidate.Revision {
			return fmt.Errorf("review decision for %s does not bind the current finding revision", decision.FindingID)
		}
		if reviewDecisionRequiresAttention(candidate, decision) && intake.Triage[decision.FindingID] != "attention" {
			return fmt.Errorf("review decision %s for %s requires individual attention", decision.Action, decision.FindingID)
		}
		decisions[decision.FindingID] = decision
	}
	if len(decisions) != len(candidates) {
		return errors.New("review decisions must match persisted intake candidates exactly")
	}
	resulting := map[string]FindingRecord{}
	for _, outcome := range outcomes {
		if _, duplicate := resulting[outcome.ID]; duplicate {
			return fmt.Errorf("review transaction repeats finding outcome %s", outcome.ID)
		}
		resulting[outcome.ID] = outcome
	}
	for findingID, candidate := range candidates {
		decision := decisions[findingID]
		outcome, present := resulting[findingID]
		if !present {
			return fmt.Errorf("review decision %s for %s has no resulting finding revision", decision.Action, findingID)
		}
		if outcome.Revision != decision.FindingRevision+1 {
			return fmt.Errorf("review outcome %s must advance reviewed revision %d exactly once", findingID, decision.FindingRevision)
		}
		if err := validateReviewDecisionOutcome(candidate, decision, outcome, resulting, candidates); err != nil {
			return fmt.Errorf("review outcome %s: %w", findingID, err)
		}
	}
	for findingID := range resulting {
		if _, candidate := candidates[findingID]; candidate {
			continue
		}
		claimed := false
		for _, sourceID := range resulting[findingID].Relations.Supersedes {
			decision, ok := decisions[sourceID]
			if ok && validOne(decision.Action, "merge", "split", "supersede") {
				claimed = true
			}
		}
		if !claimed {
			return fmt.Errorf("review transaction includes unrelated finding outcome %s", findingID)
		}
	}
	return nil
}

func reviewDecisionRequiresAttention(candidate FindingRecord, decision ReviewDecision) bool {
	return findingRequiresAttention(candidate) || decision.Projection == "truth" ||
		validOne(decision.Action, "challenge", "merge", "split", "supersede")
}

func validateReviewDecisionOutcome(
	candidate FindingRecord,
	decision ReviewDecision,
	outcome FindingRecord,
	allOutcomes map[string]FindingRecord,
	candidates map[string]FindingRecord,
) error {
	if outcome.ID != candidate.ID || outcome.CampaignID != candidate.CampaignID || outcome.Kind != candidate.Kind {
		return errors.New("finding identity, campaign, and kind are immutable")
	}
	expectedGrade := candidate.EvidenceGrade
	if decision.EvidenceCorrection != "" {
		expectedGrade = decision.EvidenceCorrection
	}
	if decision.Action == "correct-grade" && decision.EvidenceCorrection == "" {
		return errors.New("correct-grade requires an explicit evidence correction")
	}
	if outcome.EvidenceGrade != expectedGrade {
		return fmt.Errorf("evidence grade %s does not match decision outcome %s", outcome.EvidenceGrade, expectedGrade)
	}
	expectedProjection := candidate.Projection
	if decision.Projection != "" {
		expectedProjection = decision.Projection
	}
	expectedReviewState := candidate.ReviewState
	expectedValidity := candidate.Validity
	replacementMinimum, replacementMaximum := 0, 0
	switch decision.Action {
	case "ratify":
		expectedReviewState = "manager-ratified"
	case "reject":
		if decision.Projection != "" && decision.Projection != "rejected" {
			return errors.New("reject cannot authorize a non-rejected projection")
		}
		expectedReviewState, expectedValidity, expectedProjection = "manager-rejected", "invalid", "rejected"
	case "challenge":
		expectedValidity = "challenged"
	case "hold", "correct-grade":
	case "merge":
		expectedValidity, replacementMinimum, replacementMaximum = "superseded", 1, 1
	case "split":
		expectedValidity, replacementMinimum = "superseded", 2
	case "supersede":
		expectedValidity, replacementMinimum, replacementMaximum = "superseded", 1, 1
	default:
		return fmt.Errorf("unsupported decision action %s", decision.Action)
	}
	if outcome.ReviewState != expectedReviewState || outcome.Validity != expectedValidity || outcome.Projection != expectedProjection {
		return fmt.Errorf("states %s/%s/%s do not match %s decision outcome %s/%s/%s",
			outcome.ReviewState, outcome.Validity, outcome.Projection, decision.Action,
			expectedReviewState, expectedValidity, expectedProjection)
	}
	if replacementMinimum == 0 {
		return nil
	}
	replacements := 0
	for id, replacement := range allOutcomes {
		if _, isCandidate := candidates[id]; isCandidate {
			continue
		}
		if containsString(replacement.Relations.Supersedes, candidate.ID) {
			replacements++
		}
	}
	if replacements < replacementMinimum || replacementMaximum > 0 && replacements > replacementMaximum {
		return fmt.Errorf("%s requires %d..%d replacement findings with supersedes back-pointers; found %d",
			decision.Action, replacementMinimum, replacementMaximum, replacements)
	}
	return nil
}

func validateAppliedManagerReview(previous CampaignGraph, writes []preparedStateWrite) error {
	var review *ReviewRecord
	var intake *IntakeRecord
	outcomes := []FindingRecord{}
	for _, write := range writes {
		switch record := write.Record.(type) {
		case ReviewRecord:
			if review != nil {
				return errors.New("review transaction must publish exactly one immutable review receipt")
			}
			copy := record
			review = &copy
		case IntakeRecord:
			if intake != nil {
				return errors.New("review transaction must advance exactly one intake")
			}
			copy := record
			intake = &copy
		case FindingDocument:
			outcomes = append(outcomes, record.Record)
		}
	}
	if review == nil || intake == nil {
		return errors.New("review transaction requires one receipt and one resulting intake")
	}
	priorIntake, present := previous.Intakes[review.IntakeID]
	if !present {
		return fmt.Errorf("review intake %s is not canonical campaign state", review.IntakeID)
	}
	candidates := map[string]FindingRecord{}
	for _, findingID := range priorIntake.CandidateFindingIDs {
		candidate, present := previous.Findings[findingID]
		if !present {
			return fmt.Errorf("review candidate %s is not canonical campaign state", findingID)
		}
		candidates[findingID] = candidate
	}
	return validateManagerReviewRecordOutcomes(priorIntake, candidates, *review, *intake, outcomes)
}

func ReviewReceiptDigest(review ReviewRecord) (string, error) {
	review.Digest = ""
	review.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	sort.Slice(review.Decisions, func(i, j int) bool {
		return review.Decisions[i].FindingID < review.Decisions[j].FindingID
	})
	review.UnresolvedConflicts = SortedUnique(review.UnresolvedConflicts)
	review.ResultingEventIDs = SortedUnique(review.ResultingEventIDs)
	review.ResultingRecordIDs = SortedUnique(review.ResultingRecordIDs)
	return CanonicalDigest(review)
}

func VerifyImmutableReviewReceipt(existing, candidate ReviewRecord) error {
	if existing.ID != candidate.ID {
		return errors.New("immutable receipt comparison requires the same review id")
	}
	left, err := ReviewReceiptDigest(existing)
	if err != nil {
		return err
	}
	right, err := ReviewReceiptDigest(candidate)
	if err != nil {
		return err
	}
	if existing.Digest != left || candidate.Digest != right {
		return errors.New("review receipt has an invalid content digest")
	}
	if left != right {
		return errors.New("review receipts are immutable; create a linked later review")
	}
	return nil
}
