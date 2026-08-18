package knowledge

import (
	"errors"
	"fmt"
	"sort"
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
	if previous.Status != "reviewed" && next.Status == "reviewed" {
		if err := refuseUnresolvedCoverageAtRatification(previous.Status, next); err != nil {
			return err
		}
	}
	return nil
}

// unresolvedCoverageHandles renders, in sorted order, the canonical span handle
// of every coverage row an intake still leaves `unresolved`. Sorting matters
// because the refusal below is the only place a curator or manager ever sees the
// full list, and an order that changed between two identical refusals would make
// them look like different failures.
func unresolvedCoverageHandles(intake IntakeRecord) []string {
	handles := []string{}
	for _, entry := range intake.Coverage {
		if entry.Disposition == "unresolved" {
			handles = append(handles, canonicalCoverageHandle(entry))
		}
	}
	sort.Strings(handles)
	return handles
}

// refuseUnresolvedCoverageAtRatification is the whole of the rule that an intake
// may not become `reviewed` while it still declares spans nobody judged.
//
// Why the rule exists at all. An `unresolved` disposition is not a judgment; it
// is a recorded absence of one, and it is fatal much later. reviewedReportCoverage
// refuses to count *any* source report carrying an unresolved span, so closure
// classifies the run whose report the intake covers `missing-reviewed-intake`,
// permanently: `completed` has no edge back to `returned`, curation_submit
// accepts a fresh intake for a run that has left `returned` only under the
// stranded-run admission, and `reviewed` has no edge back to `submitted`. Nothing
// between ratification and the closure gate says any of this out loud. A real
// campaign carried seven such spans for weeks and discovered them only when
// closure refused, by which point the curator who had declined to judge them was
// long gone and the manager who had to supply the judgment had never read the
// report. Ratification is the last moment at which the person deciding is still
// the person holding the packet, so it is where the cost belongs.
//
// Why this is a rule about the transition and not about the record. It would be
// natural to put this in ValidateIntake, and it would be a data-loss bug.
// decodeCanonicalRecord runs record validation on every *load*, so a rule there
// would make every already-committed reviewed intake that carries an unresolved
// span fail to decode - and because LoadCampaignGraph fails whole, the campaign
// graph containing it would stop loading at all, across every campaign reviewed
// before this rule existed. Gating the edge into `reviewed` instead leaves those
// historical records readable and holds only new ratifications to the rule. The
// reviewed -> reviewed edge is deliberately not covered: that is the amendment
// path, and it is how a historical record with unresolved spans gets repaired
// one span at a time.
func refuseUnresolvedCoverageAtRatification(from string, next IntakeRecord) error {
	handles := unresolvedCoverageHandles(next)
	if len(handles) == 0 {
		return nil
	}
	return fmt.Errorf(
		"intake %s cannot be ratified while %d coverage span(s) are still unresolved (%s): an "+
			"unresolved disposition records that nobody judged the span, and reviewedReportCoverage "+
			"counts no source report that carries one, so ratifying this packet would classify the "+
			"covered run missing-reviewed-intake at closure with nothing warning of it until then. "+
			"Remedy: while the intake is %s, resubmit it through curation_submit with each named "+
			"span disposed as candidate-finding, duplicate, non-claim, or out-of-scope, then ratify "+
			"the resubmitted revision. manager_apply intake.coverage.retire is the sibling repair "+
			"that gives exactly these spans a terminal non-claim or out-of-scope judgment under a "+
			"review that already ratified them, and it exists for the intakes that reached "+
			"reviewed with unresolved spans before this rule did",
		next.ID, len(handles), strings.Join(handles, ", "), from)
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
