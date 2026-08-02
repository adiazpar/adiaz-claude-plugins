package knowledge

import (
	"errors"
	"fmt"
	"strings"
)

var intakeTransitions = map[string]map[string]bool{
	"draft":      {"draft": true, "submitted": true, "superseded": true},
	"submitted":  {"submitted": true, "reviewed": true, "superseded": true},
	"reviewed":   {"reviewed": true},
	"superseded": {"superseded": true},
}

var findingReviewTransitions = map[string]map[string]bool{
	"extracted":        {"extracted": true, "curator-checked": true, "manager-ratified": true, "manager-rejected": true},
	"curator-checked":  {"curator-checked": true, "manager-ratified": true, "manager-rejected": true},
	"manager-ratified": {"manager-ratified": true, "manager-rejected": true},
	"manager-rejected": {"manager-rejected": true},
}

var findingValidityTransitions = map[string]map[string]bool{
	"provisional": {"provisional": true, "current": true, "challenged": true, "historical": true, "superseded": true, "invalid": true},
	"current":     {"current": true, "challenged": true, "historical": true, "superseded": true, "invalid": true},
	"challenged":  {"challenged": true, "provisional": true, "current": true, "historical": true, "superseded": true, "invalid": true},
	"historical":  {"historical": true, "superseded": true, "invalid": true},
	"superseded":  {"superseded": true},
	"invalid":     {"invalid": true},
}

func ValidateIntakeTransition(previous *IntakeRecord, next IntakeRecord) error {
	if err := ValidateIntake(next); err != nil {
		return err
	}
	if previous == nil {
		if next.Revision != 1 || !validOne(next.Status, "draft", "submitted") {
			return errors.New("a new intake must begin draft or submitted at revision 1")
		}
		return nil
	}
	if err := validateMetaTransition(previous.RecordMeta, next.RecordMeta); err != nil {
		return err
	}
	if previous.CampaignID != next.CampaignID {
		return errors.New("intake campaign is immutable")
	}
	if !intakeTransitions[previous.Status][next.Status] {
		return fmt.Errorf("illegal intake transition %s -> %s", previous.Status, next.Status)
	}
	return nil
}

func ValidateReviewTransition(previous *ReviewRecord, next ReviewRecord) error {
	if err := ValidateReview(next); err != nil {
		return err
	}
	if previous != nil {
		return errors.New("review receipts are immutable; create a linked review instead")
	}
	if next.Revision != 1 {
		return errors.New("a review receipt must be created at revision 1")
	}
	return nil
}

func ValidateFindingTransition(previous *FindingRecord, next FindingRecord, action, authority string) error {
	if err := ValidateFinding(next); err != nil {
		return err
	}
	if previous == nil {
		if next.Revision != 1 || !validOne(next.ReviewState, "extracted", "curator-checked") || next.Validity != "provisional" {
			return errors.New("a new finding begins provisional and unratified at revision 1")
		}
		return nil
	}
	previousMeta := RecordMeta{
		SchemaVersion: previous.SchemaVersion, ID: previous.ID, CreatedAt: previous.CreatedAt,
		UpdatedAt: previous.UpdatedAt, Revision: previous.Revision, CreatedBy: previous.CreatedBy,
		UpdatedBy: previous.UpdatedBy, Digest: previous.Digest, CorrelationID: previous.CorrelationID,
	}
	nextMeta := RecordMeta{
		SchemaVersion: next.SchemaVersion, ID: next.ID, CreatedAt: next.CreatedAt,
		UpdatedAt: next.UpdatedAt, Revision: next.Revision, CreatedBy: next.CreatedBy,
		UpdatedBy: next.UpdatedBy, Digest: next.Digest, CorrelationID: next.CorrelationID,
	}
	if err := validateMetaTransition(previousMeta, nextMeta); err != nil {
		return err
	}
	if previous.CampaignID != next.CampaignID || previous.Kind != next.Kind {
		return errors.New("finding campaign and kind are immutable")
	}
	if !findingReviewTransitions[previous.ReviewState][next.ReviewState] {
		return fmt.Errorf("illegal finding review transition %s -> %s", previous.ReviewState, next.ReviewState)
	}
	if !findingValidityTransitions[previous.Validity][next.Validity] {
		return fmt.Errorf("illegal finding validity transition %s -> %s", previous.Validity, next.Validity)
	}
	if next.ReviewState != previous.ReviewState && validOne(next.ReviewState, "manager-ratified", "manager-rejected") && authority != "manager" {
		return errors.New("only manager authority may ratify or reject a finding")
	}
	if next.Validity == "current" && !strings.HasPrefix(action, "closure.") {
		return errors.New("only a closure action may make a finding current")
	}
	return nil
}
