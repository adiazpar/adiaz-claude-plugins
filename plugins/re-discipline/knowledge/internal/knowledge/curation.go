package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	EvidenceHandles []string `json:"evidenceHandles"`
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
		expectedEvidenceHandles := make([]string, 0, len(candidate.Evidence))
		for _, evidence := range candidate.Evidence {
			expectedEvidenceHandles = append(expectedEvidenceHandles, EvidenceHandle(candidate.ID, evidence))
		}
		expectedEvidenceHandles = SortedUnique(expectedEvidenceHandles)
		if !reflect.DeepEqual(SortedUnique(row.EvidenceHandles), expectedEvidenceHandles) {
			return fmt.Errorf("review packet row %s does not bind the candidate's exact evidence handles", row.FindingID)
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
		remedy := "Remedy: set intake.status to \"submitted\" on the revision this call carries"
		if packet.Intake.Status == "reviewed" {
			remedy = "A reviewed intake is ratified and curation_submit cannot resubmit it. " +
				"Remedy: to change the disposition of unresolved spans on a reviewed intake, " +
				"use manager_apply intake.coverage.retire; to add coverage a reviewed intake " +
				"does not carry, submit a further intake as a new record"
		}
		return fmt.Errorf(
			"curation_submit publishes an intake at status \"submitted\"; this one is %q. A "+
				"curator proposes and a manager ratifies, so the curator never writes the "+
				"reviewed state. %s",
			packet.Intake.Status, remedy)
	}
	if len(packet.Candidates) > 10 {
		return fmt.Errorf(
			"curation packet carries %d candidate findings and the limit is 10; a manager "+
				"review is a single sitting over related claims, and a packet larger than that "+
				"is ratified without being read. Remedy: split the coverage across several "+
				"curation_submit calls, each with its own intake id and its own spans",
			len(packet.Candidates))
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
			return fmt.Errorf(
				"candidate finding %s has reviewState %q; a curator may submit only "+
					"\"extracted\" or \"curator-checked\". manager-ratified and "+
					"manager-rejected are set by manager_apply review.submit, which is the "+
					"authority boundary this engine exists to keep. Remedy: set reviewState to "+
					"\"curator-checked\"",
				record.ID, record.ReviewState)
		}
		if record.Validity == "current" || record.ReviewState == "manager-ratified" ||
			record.ReviewState == "manager-rejected" {
			return fmt.Errorf(
				"candidate finding %s asserts an authority a curator does not hold: validity %q "+
					"with reviewState %q. \"current\" means the project treats the claim as "+
					"true, and only closure projection sets it. Remedy: submit the candidate as "+
					"validity \"provisional\" with reviewState \"curator-checked\" and let "+
					"manager_apply review.submit decide",
				record.ID, record.Validity, record.ReviewState)
		}
		if err := ValidateFindingDocument(candidate, true); err != nil {
			return fmt.Errorf("candidate %s: %w", record.ID, err)
		}
		if findingRequiresAttention(record) && rows[record.ID] != "attention" {
			return fmt.Errorf("conflicted or truth-touching finding %s requires attention triage", record.ID)
		}
	}
	if len(seen) != len(declared) || len(packet.Rows) != len(declared) {
		return fmt.Errorf(
			"the packet's three candidate lists must agree exactly: intake.candidateFindingIds "+
				"declares %d, %d finding records were submitted, and %d triage rows were "+
				"submitted. Each declared id needs exactly one record and exactly one row, "+
				"because the manager review binds all three together. Remedy: add or remove "+
				"whichever list is short",
			len(declared), len(seen), len(packet.Rows))
	}
	for _, coverage := range packet.Intake.Coverage {
		if coverage.Disposition == "candidate-finding" && !declared[coverage.TargetID] {
			return fmt.Errorf(
				"coverage span %s:%d-%d is disposed \"candidate-finding\" naming targetId %s, "+
					"which this packet neither declares in intake.candidateFindingIds nor "+
					"submits as a candidate record. Remedy: add the finding to both, or "+
					"re-dispose the span as duplicate, non-claim, or out-of-scope",
				coverage.SourcePath, coverage.StartLine, coverage.EndLine, coverage.TargetID)
		}
	}
	if err := validateCandidateCoverageEvidence(packet); err != nil {
		return err
	}
	return nil
}

func coverageEvidenceKey(path, sha256 string, startLine, endLine int, objectKey string) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", path, sha256, startLine, endLine, objectKey)
}

func coverageEntryEvidenceKey(entry CoverageEntry) string {
	return coverageEvidenceKey(
		entry.SourcePath, entry.SourceSHA256, entry.StartLine, entry.EndLine, entry.SourceHandle,
	)
}

func evidenceReferenceCoverageKey(evidence EvidenceReference) string {
	return coverageEvidenceKey(
		evidence.Path, evidence.SHA256, evidence.StartLine, evidence.EndLine, evidence.ObjectKey,
	)
}

func validateCandidateCoverageEvidence(packet CurationPacket) error {
	expected := map[string]map[string]int{}
	for _, entry := range packet.Intake.Coverage {
		if entry.Disposition != "candidate-finding" {
			continue
		}
		if expected[entry.TargetID] == nil {
			expected[entry.TargetID] = map[string]int{}
		}
		expected[entry.TargetID][coverageEntryEvidenceKey(entry)]++
	}
	for _, candidate := range packet.Candidates {
		remaining := map[string]int{}
		for key, count := range expected[candidate.Record.ID] {
			remaining[key] = count
		}
		if len(remaining) == 0 {
			return fmt.Errorf("candidate finding %s has no exact coverage evidence", candidate.Record.ID)
		}
		for _, evidence := range candidate.Record.Evidence {
			key := evidenceReferenceCoverageKey(evidence)
			if remaining[key] == 0 {
				return fmt.Errorf("candidate finding %s has evidence outside its exact coverage spans", candidate.Record.ID)
			}
			remaining[key]--
		}
		for _, count := range remaining {
			if count != 0 {
				return fmt.Errorf("candidate finding %s does not cite every exact coverage span", candidate.Record.ID)
			}
		}
	}
	return nil
}

// validateCurationGraphBindings resolves every intake source to one canonical
// run report and binds candidate evidence to the exact run that returned it.
// atSubmission is true at curator submission, where the source run's state
// gates whether an intake may be written at all, and false when validating
// durable state after the source run has advanced to a terminal status.
func validateCurationGraphBindings(
	graph CampaignGraph,
	packet CurationPacket,
	atSubmission bool,
) (map[string]string, error) {
	if graph.Campaign == nil {
		return nil, errors.New("curation source binding requires a campaign")
	}
	if err := validateCandidateCoverageEvidence(packet); err != nil {
		return nil, err
	}
	bindings := map[string]string{}
	for _, source := range packet.Intake.SourceRuns {
		key := coverageSourceKey(source.Path, source.SHA256)
		matches := []RunRecord{}
		for _, run := range graph.Runs {
			if run.Report != nil && run.Report.Path == source.Path && run.Report.SHA256 == source.SHA256 {
				matches = append(matches, run)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("intake source %s must resolve to exactly one canonical run report", source.Path)
		}
		run := matches[0]
		expectedPath := fmt.Sprintf("active/%s/runs/%s/report.md", graph.Campaign.Slug, run.ID)
		if source.Path != expectedPath {
			return nil, fmt.Errorf("intake source %s is not canonical report path %s", source.Path, expectedPath)
		}
		if atSubmission {
			if err := validateCurationSourceRunAdmissible(graph, run); err != nil {
				return nil, err
			}
		}
		bindings[key] = run.ID
	}

	declaredCandidates := map[string]FindingRecord{}
	for _, candidate := range packet.Candidates {
		declaredCandidates[candidate.Record.ID] = candidate.Record
	}
	for _, entry := range packet.Intake.Coverage {
		if entry.Disposition == "duplicate" {
			if _, ok := graph.Findings[entry.TargetID]; !ok {
				return nil, fmt.Errorf("duplicate coverage target %s is not a canonical campaign finding", entry.TargetID)
			}
		}
	}
	for _, findingID := range packet.Intake.CandidateFindingIDs {
		candidate, ok := declaredCandidates[findingID]
		if !ok {
			return nil, fmt.Errorf("intake candidate %s has no submitted finding", findingID)
		}
		expectedRuns := map[string]bool{}
		for _, entry := range packet.Intake.Coverage {
			if entry.Disposition != "candidate-finding" || entry.TargetID != findingID {
				continue
			}
			runID := bindings[coverageSourceKey(entry.SourcePath, entry.SourceSHA256)]
			if runID == "" {
				return nil, fmt.Errorf("candidate %s coverage does not resolve to a source run", findingID)
			}
			expectedRuns[runID] = true
		}
		for _, evidence := range candidate.Evidence {
			matchedRun := ""
			for _, entry := range packet.Intake.Coverage {
				if entry.Disposition == "candidate-finding" && entry.TargetID == findingID &&
					evidenceReferenceCoverageKey(evidence) == coverageEntryEvidenceKey(entry) {
					matchedRun = bindings[coverageSourceKey(entry.SourcePath, entry.SourceSHA256)]
					break
				}
			}
			if matchedRun == "" || evidence.SourceRun != matchedRun {
				return nil, fmt.Errorf("candidate %s evidence is not bound to its canonical source run", findingID)
			}
		}
		if len(candidate.SourceRuns) != len(expectedRuns) {
			return nil, fmt.Errorf("candidate %s sourceRuns do not match its exact coverage sources", findingID)
		}
		for _, runID := range candidate.SourceRuns {
			if !expectedRuns[runID] {
				return nil, fmt.Errorf("candidate %s cites unrelated source run %s", findingID, runID)
			}
		}
	}
	return bindings, nil
}

// validateCurationSourceRunAdmissible decides whether a curator may write a new
// intake over one source run's frozen report.
//
// `returned` is the ordinary case and keeps its historical behaviour exactly:
// the run has just come back, its report is frozen, and nothing has been
// concluded about it yet.
//
// A run that has left `returned` for `completed` or `invalidated` is admitted
// only while it is genuinely stranded. Closure classifies every non-aborted run
// with a report by whether a reviewed curator intake covers that report with no
// `unresolved` span (reviewedReportCoverage), and neither state has a
// transition back to `returned`. A run that left `returned` over an intake that
// still carried an unresolved span therefore fixes "missing-reviewed-intake"
// into the campaign with no way to clear it: curation refused the run, review
// could not create an intake, and no manager override seeds SourceRunCoverage.
// This is the only remaining route to a clean intake for those runs.
//
// For `completed` the admission is the exact complement of the completion
// guard: both ask inspectRunReportIntakeCoverage the same question, and a
// completed run is admitted here precisely when it could not have passed
// validateAppliedRunCompletion. That guard now prevents the strand, but it
// cannot repair a campaign stranded before it existed.
//
// `invalidated` needs the same admission for the opposite reason: it is the one
// exit from `returned` that the completion guard must not refuse, because it is
// the only other exit and refusing it would leave a dirty run with no exit at
// all. So the engine still creates this strand today, and curation is where it
// is repaired.
//
// Closure does not simply exempt an invalidated run instead, and must not: an
// invalidated run's report stays frozen (ValidateRunTransition), stays in the
// closure plan's retained payload, and can still be cited by a ratified
// finding - FindingRecord.SourceRuns is bound at curation and CampaignGraph
// requires only that each source run resolve, while nothing cascades from run
// invalidation to a finding's review state, validity, or projection. Exempting
// the report would let a campaign close with a projected truth resting on
// evidence the coverage gate no longer accounts for.
//
// The two exemptions closure does grant turn on the report, not the status.
// `aborted` is unreachable from `returned` and carries no report at all, and a
// run invalidated before it ever returned carries none either. Neither can be
// cited: validateCurationGraphBindings resolves an intake source to a run only
// by matching a frozen report handle, and a candidate's sourceRuns must equal
// exactly the runs its coverage spans resolve to, so no span and no finding can
// ever bind to a run that froze no bytes. Both are therefore refused here, with
// a remedy that says so rather than pointing at a report that cannot exist.
//
// A run whose report already carries a clean, non-superseded intake is still
// refused in either state, so this cannot be used to pile supplementary intakes
// onto healthy runs, nor to add a second repair while one is already pending
// review. Every other run state stays refused.
//
// A supplementary intake is purely additive. reviewedReportCoverage unions the
// clean sources of every reviewed intake with no supersession or uniqueness
// filter and computes unresolved spans per intake, so the new intake flips this
// run to `reviewed-intake` without altering, superseding, or invalidating the
// ratified review already recorded over the dirty one. It does not restore the
// run either: an invalidated run stays invalidated, and its candidates face the
// same individual manager review as any other. Retiring a prior conclusion
// remains the business of overturn.
func validateCurationSourceRunAdmissible(graph CampaignGraph, run RunRecord) error {
	if run.Status == "returned" {
		return nil
	}
	if !validOne(run.Status, "completed", "invalidated") || run.Report == nil {
		return fmt.Errorf(
			"intake source run %s is %s and cannot be curated: curation accepts a returned run, "+
				"and a completed or invalidated run only while its frozen report still has no clean "+
				"curator intake for closure to count. Remedy: %s",
			run.ID, run.Status, curationSourceRunRemedy(run))
	}
	coverage := inspectRunReportIntakeCoverage(graph, *run.Report)
	if len(coverage.Clean) == 0 {
		// Genuinely stranded: no longer returned, and closure cannot be
		// satisfied for this report by any intake that exists.
		return nil
	}
	remedy := fmt.Sprintf(
		"nothing needs repair here; if a prior conclusion over %s is wrong, overturn it rather than "+
			"curating the run again", run.Report.Path)
	pending := []string{}
	for _, intakeID := range coverage.Clean {
		if coverage.Statuses[intakeID] != "reviewed" {
			pending = append(pending, fmt.Sprintf("%s (%s)", intakeID, coverage.Statuses[intakeID]))
		}
	}
	if len(pending) == len(coverage.Clean) {
		remedy = "review the clean intake that already covers it (" + strings.Join(pending, ", ") +
			") instead of submitting another"
	}
	dirty := ""
	if len(coverage.Dirty) > 0 {
		dirty = fmt.Sprintf(" (%s, but that does not strand the run while a clean intake exists)",
			strings.Join(coverage.Dirty, "; "))
		// A reviewed dirty intake is not repairable here and never was: an
		// intake a manager has ratified has no edge back to `submitted`, so
		// curation can only ever add a new one beside it. Saying so, and naming
		// the transition that does edit it in place, keeps a caller from
		// reading this refusal as "submit the supplementary intake differently".
		if reviewed := reviewedDirtyCoverageIntakes(coverage); reviewed != "" {
			dirty += fmt.Sprintf(
				". %s are already reviewed, so curation cannot revise them at all; their unresolved "+
					"spans are re-dispositioned in place through manager_apply intake.coverage.retire",
				reviewed)
		}
	}
	return fmt.Errorf(
		"intake source run %s is %s and its report %s is already covered by clean curator "+
			"intake(s) %s%s, so it is not stranded: a run that has left returned may be curated only "+
			"to supply the clean coverage closure requires and cannot otherwise obtain. Remedy: %s",
		run.ID, run.Status, run.Report.Path, strings.Join(coverage.Clean, ", "), dirty, remedy)
}

func curationSourceRunRemedy(run RunRecord) string {
	if run.Report == nil {
		// No intake can dispose spans of a report that was never frozen, so
		// the remedy is never curation. What it is instead depends on whether
		// the run can still produce one.
		switch run.Status {
		case "prepared", "running":
			return "this run has no frozen report, so no intake can cover it; return the run first, " +
				"so its report is frozen, then curate it - or, if it will never return, end it " +
				"through manager_apply run.complete as aborted with a reason in resultSummary, " +
				"which closure exempts"
		case "invalidated":
			return "this run has no frozen report, so no intake can cover it; closure exempts a run " +
				"voided before it ever returned, exactly as it exempts an aborted one, so there is " +
				"nothing to clear here. aborted is the more accurate record of a run that never " +
				"came back: it carries the reason rather than a superseding run id"
		default:
			return "this run has no frozen report, so no intake can cover it, and closure does not " +
				"ask for one"
		}
	}
	switch run.Status {
	case "prepared", "running":
		return "return the run first, so its report is frozen, then curate it"
	case "blocked":
		return "run.complete already required a clean curator intake before this run could be " +
			"blocked, so there is nothing here for a further intake to supply; if the work must " +
			"continue, reopen the work item and delegate a fresh run"
	case "aborted":
		return "an aborted run is exempt from closure coverage and needs no intake"
	default:
		return "curate the run while it is returned"
	}
}

// ValidateManagerReview binds an immutable manager receipt to one intake
// revision and requires an individual decision for every candidate.
func ValidateManagerReview(role string, packet CurationPacket, review ReviewRecord) error {
	if role != "manager" || review.Authority != "manager" {
		return errors.New("finding ratification requires manager authority")
	}
	if review.ReviewLoad.MeasurementStatus != "measured" {
		return errors.New("new manager reviews require contemporaneously measured review load")
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
	if err := ValidateReviewLoadBinding(review.ReviewLoad, review, packet, ReviewLoadConfig{
		TargetMinutesPerPacket:  review.ReviewLoad.TargetMinutesPerPacket,
		TargetPacketsPerSession: review.ReviewLoad.TargetPacketsPerSession,
	}); err != nil {
		return err
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
	candidates := map[string]FindingDocument{}
	for _, candidate := range packet.Candidates {
		candidates[candidate.Record.ID] = candidate
	}
	return validateManagerReviewDocumentOutcomes(packet.Intake, candidates, review, resultingIntake, outcomes)
}

func validateManagerReviewDocumentOutcomes(
	intake IntakeRecord,
	candidates map[string]FindingDocument,
	review ReviewRecord,
	resultingIntake IntakeRecord,
	outcomes []FindingDocument,
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
		if !ok || decision.FindingRevision != candidate.Record.Revision {
			return fmt.Errorf("review decision for %s does not bind the current finding revision", decision.FindingID)
		}
		if reviewDecisionRequiresAttention(candidate.Record, decision) && intake.Triage[decision.FindingID] != "attention" {
			return fmt.Errorf("review decision %s for %s requires individual attention", decision.Action, decision.FindingID)
		}
		decisions[decision.FindingID] = decision
	}
	if len(decisions) != len(candidates) {
		return errors.New("review decisions must match persisted intake candidates exactly")
	}
	resulting := map[string]FindingDocument{}
	for _, outcome := range outcomes {
		if _, duplicate := resulting[outcome.Record.ID]; duplicate {
			return fmt.Errorf("review transaction repeats finding outcome %s", outcome.Record.ID)
		}
		resulting[outcome.Record.ID] = outcome
	}
	for findingID, candidate := range candidates {
		decision := decisions[findingID]
		outcome, present := resulting[findingID]
		if !present {
			return fmt.Errorf("review decision %s for %s has no resulting finding revision", decision.Action, findingID)
		}
		if outcome.Record.Revision != decision.FindingRevision+1 {
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
		for _, sourceID := range resulting[findingID].Record.Relations.Supersedes {
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

func reviewedFindingContentDigest(document FindingDocument) (string, error) {
	document = normalizeFindingDocument(document)
	record := document.Record
	body, path := record.Body, record.Path
	record.Revision = 0
	record.UpdatedAt = ""
	record.UpdatedBy = ""
	record.Digest = ""
	record.CorrelationID = ""
	record.EvidenceGrade = ""
	record.ReviewState = ""
	record.Validity = ""
	record.Projection = ""
	record.Body = ""
	record.Path = ""
	document.Record = record
	return CanonicalDigest(struct {
		Document FindingDocument `json:"document"`
		Body     string          `json:"body"`
		Path     string          `json:"path"`
	}{Document: document, Body: body, Path: path})
}

func validateReviewedFindingContent(candidate, outcome FindingDocument) error {
	before, err := reviewedFindingContentDigest(candidate)
	if err != nil {
		return err
	}
	after, err := reviewedFindingContentDigest(outcome)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New(
			"review outcome may not substitute claim, evidence, source, relations, body, or synthetic-query content")
	}
	return nil
}

func validateReviewDecisionOutcome(
	candidate FindingDocument,
	decision ReviewDecision,
	outcome FindingDocument,
	allOutcomes map[string]FindingDocument,
	candidates map[string]FindingDocument,
) error {
	candidateRecord, outcomeRecord := candidate.Record, outcome.Record
	if outcomeRecord.ID != candidateRecord.ID || outcomeRecord.CampaignID != candidateRecord.CampaignID || outcomeRecord.Kind != candidateRecord.Kind {
		return errors.New("finding identity, campaign, and kind are immutable")
	}
	if err := validateReviewedFindingContent(candidate, outcome); err != nil {
		return err
	}
	expectedGrade := candidateRecord.EvidenceGrade
	if decision.EvidenceCorrection != "" {
		expectedGrade = decision.EvidenceCorrection
	}
	if decision.Action == "correct-grade" && decision.EvidenceCorrection == "" {
		return errors.New("correct-grade requires an explicit evidence correction")
	}
	if outcomeRecord.EvidenceGrade != expectedGrade {
		return fmt.Errorf("evidence grade %s does not match decision outcome %s", outcomeRecord.EvidenceGrade, expectedGrade)
	}
	expectedProjection := candidateRecord.Projection
	if decision.Projection != "" {
		expectedProjection = decision.Projection
	}
	expectedReviewState := candidateRecord.ReviewState
	expectedValidity := candidateRecord.Validity
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
	if outcomeRecord.ReviewState != expectedReviewState || outcomeRecord.Validity != expectedValidity || outcomeRecord.Projection != expectedProjection {
		return fmt.Errorf("states %s/%s/%s do not match %s decision outcome %s/%s/%s",
			outcomeRecord.ReviewState, outcomeRecord.Validity, outcomeRecord.Projection, decision.Action,
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
		if containsString(replacement.Record.Relations.Supersedes, candidateRecord.ID) {
			replacements++
		}
	}
	if replacements < replacementMinimum || replacementMaximum > 0 && replacements > replacementMaximum {
		return fmt.Errorf("%s requires %d..%d replacement findings with supersedes back-pointers; found %d",
			decision.Action, replacementMinimum, replacementMaximum, replacements)
	}
	return nil
}

func validateAppliedManagerReview(
	store *StateStore,
	previous CampaignGraph,
	writes []preparedStateWrite,
	submission *ReviewPacketSubmission,
) error {
	var review *ReviewRecord
	var intake *IntakeRecord
	outcomes := []FindingDocument{}
	workItems := []WorkItemRecord{}
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
			outcomes = append(outcomes, record)
		case WorkItemRecord:
			workItems = append(workItems, record)
		}
	}
	if review == nil || intake == nil {
		return errors.New("review transaction requires one receipt and one resulting intake")
	}
	if store == nil || submission == nil {
		return errors.New("review transaction requires the exact manager-facing packet at commit")
	}
	priorIntake, present := previous.Intakes[review.IntakeID]
	if !present {
		return fmt.Errorf("review intake %s is not canonical campaign state", review.IntakeID)
	}
	if err := validateSubmittedIntakeIsCanonical(previous, submission.Intake, priorIntake); err != nil {
		return err
	}
	candidates := map[string]FindingDocument{}
	for _, findingID := range priorIntake.CandidateFindingIDs {
		candidateRecord, present := previous.Findings[findingID]
		if !present {
			return fmt.Errorf("review candidate %s is not canonical campaign state", findingID)
		}
		path := candidateRecord.Path
		if path == "" {
			path = fmt.Sprintf("active/%s/findings/%s.md", previous.Campaign.Slug, findingID)
		}
		value, _, exists, err := store.existingRecord(path)
		if err != nil {
			return err
		}
		candidate, ok := value.(FindingDocument)
		if !exists || !ok || candidate.Record.Digest != candidateRecord.Digest ||
			candidate.Record.Revision != candidateRecord.Revision {
			return fmt.Errorf("review candidate %s could not be reconstructed from canonical state", findingID)
		}
		candidates[findingID] = candidate
	}
	provided := map[string]FindingDocument{}
	for _, candidate := range submission.Candidates {
		document := candidate.Document()
		if _, duplicate := provided[document.Record.ID]; duplicate {
			return fmt.Errorf("review packet repeats candidate %s", document.Record.ID)
		}
		provided[document.Record.ID] = document
	}
	if len(provided) != len(candidates) {
		return errors.New("review packet candidates do not match the canonical intake")
	}
	for findingID, canonical := range candidates {
		candidate, ok := provided[findingID]
		if !ok || candidate.Record.Revision != canonical.Record.Revision ||
			candidate.Record.Digest != canonical.Record.Digest {
			return fmt.Errorf("review packet candidate %s is not its exact canonical revision", findingID)
		}
		candidateContentDigest, candidateErr := findingDocumentDigest(candidate)
		canonicalContentDigest, canonicalErr := findingDocumentDigest(canonical)
		if candidateErr != nil || canonicalErr != nil ||
			candidateContentDigest != candidate.Record.Digest ||
			canonicalContentDigest != canonical.Record.Digest ||
			candidateContentDigest != canonicalContentDigest ||
			NormalizeProjectPath(candidate.Record.Path) != canonical.Record.Path {
			return fmt.Errorf("review packet candidate %s is not its exact canonical revision", findingID)
		}
	}
	packet := CurationPacket{Intake: priorIntake}
	for _, findingID := range priorIntake.CandidateFindingIDs {
		packet.Candidates = append(packet.Candidates, candidates[findingID])
	}
	for _, row := range submission.Envelope.Rows {
		packet.Rows = append(packet.Rows, CurationRow{FindingID: row.FindingID, Triage: row.Triage})
	}
	if err := ValidateReviewPacketEnvelope(submission.Envelope, packet); err != nil {
		return fmt.Errorf("canonical review packet: %w", err)
	}
	if review.PacketDigest != submission.Envelope.Digest {
		return errors.New("review receipt does not bind the canonical packet digest")
	}
	if err := ValidateManagerReview("manager", packet, *review); err != nil {
		return err
	}
	if err := validateReviewWorkItemCreations(previous, priorIntake, *review, workItems); err != nil {
		return err
	}
	return validateManagerReviewDocumentOutcomes(priorIntake, candidates, *review, *intake, outcomes)
}

// validateSubmittedIntakeIsCanonical proves that the intake echoed in a review
// packet is the committed intake, and names every field on which it is not.
//
// The comparison is over the canonical persisted form of both records rather
// than over the in-memory Go values. Those two shapes are not the same shape,
// and only one of them is reachable by a caller. sealIntakeRecord runs
// normalizeIntakeRecord, which rewrites nil string slices to empty non-nil
// slices via SortedUnique; canonicalJSON then writes the record with
// `omitempty`, so every one of those empty collections is absent from the file
// on disk. A caller that reads the persisted record and echoes it back
// therefore holds nil where the loader holds []string{} - a difference
// reflect.DeepEqual rejects, that CanonicalDigest cannot see (both marshal to
// nothing), and that no caller can repair by guessing, because the fields that
// get normalized and the fields that carry `omitempty` are different sets.
//
// Comparing persisted forms is the same technique validateAppliedRunReturn
// already uses for the automatic curation work item: normalize both sides, then
// require exact equality. The declared digest, revision, status and every
// content field are still compared verbatim, so a genuinely different intake is
// still refused; what is no longer refused is a faithful echo of the committed
// bytes.
func validateSubmittedIntakeIsCanonical(previous CampaignGraph, submitted, canonical IntakeRecord) error {
	submittedBody, err := canonicalIntakeComparisonForm(submitted)
	if err != nil {
		return fmt.Errorf("review packet intake could not be canonicalized: %w", err)
	}
	canonicalBody, err := canonicalIntakeComparisonForm(canonical)
	if err != nil {
		return fmt.Errorf("canonical intake %s could not be canonicalized: %w", canonical.ID, err)
	}
	if bytes.Equal(submittedBody, canonicalBody) {
		return nil
	}
	slug := ""
	if previous.Campaign != nil {
		slug = previous.Campaign.Slug
	}
	path := fmt.Sprintf("active/%s/intake/%s.json", slug, canonical.ID)
	differences := canonicalFieldDifferences(submittedBody, canonicalBody)
	detail := "the submitted and canonical records differ"
	if len(differences) > 0 {
		detail = "differing fields: " + strings.Join(differences, "; ")
	}
	return fmt.Errorf(
		"review packet intake %s is not the exact canonical submitted intake (%s); "+
			"the reviewed packet must echo the committed record at %s, so re-read that record "+
			"and resubmit it unchanged (an empty collection may be present or absent, but every "+
			"other value must match the committed revision exactly)",
		canonical.ID, detail, path)
}

// canonicalIntakeComparisonForm renders one intake in the exact bytes the
// engine would persist for it. Collections that the engine writes unconditionally
// are coerced from nil to empty so that a record read back from disk and a
// record sealed in memory produce identical bytes; the `omitempty` collections
// need no coercion because both shapes already marshal to nothing.
func canonicalIntakeComparisonForm(record IntakeRecord) ([]byte, error) {
	normalizeIntakeRecord(&record)
	if record.SourceRuns == nil {
		record.SourceRuns = []FileHandle{}
	}
	if record.Coverage == nil {
		record.Coverage = []CoverageEntry{}
	}
	if record.Triage == nil {
		record.Triage = map[string]string{}
	}
	return canonicalJSON(record)
}

// canonicalFieldDifferences names the top-level canonical fields on which two
// rendered records disagree, with a bounded rendering of each side. A refusal
// that only asserts inequality leaves a caller bisecting a record by hand.
func canonicalFieldDifferences(submitted, canonical []byte) []string {
	left, right := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if json.Unmarshal(submitted, &left) != nil || json.Unmarshal(canonical, &right) != nil {
		return nil
	}
	names := map[string]bool{}
	for name := range left {
		names[name] = true
	}
	for name := range right {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	differences := make([]string, 0, len(sorted))
	for _, name := range sorted {
		leftValue, leftPresent := left[name]
		rightValue, rightPresent := right[name]
		if leftPresent == rightPresent && bytes.Equal(compactJSONValue(leftValue), compactJSONValue(rightValue)) {
			continue
		}
		differences = append(differences, fmt.Sprintf("%s (submitted %s, canonical %s)",
			name, renderCanonicalFieldValue(leftValue, leftPresent),
			renderCanonicalFieldValue(rightValue, rightPresent)))
	}
	return differences
}

func compactJSONValue(value json.RawMessage) []byte {
	compacted := &bytes.Buffer{}
	if err := json.Compact(compacted, value); err != nil {
		return value
	}
	return compacted.Bytes()
}

func renderCanonicalFieldValue(value json.RawMessage, present bool) string {
	if !present {
		return "absent"
	}
	// Truncate on a rune boundary so a long field cannot leave a broken
	// multi-byte sequence in the refusal text.
	runes := []rune(string(compactJSONValue(value)))
	if len(runes) > 160 {
		return string(runes[:157]) + "..."
	}
	return string(runes)
}

func validateReviewWorkItemCreations(
	previous CampaignGraph,
	intake IntakeRecord,
	review ReviewRecord,
	workItems []WorkItemRecord,
) error {
	proposedWork := map[string]bool{}
	for _, workID := range intake.SpawnedWorkItems {
		proposedWork[workID] = true
	}
	for _, work := range workItems {
		if !proposedWork[work.ID] {
			return fmt.Errorf("review work item %s was not proposed by intake %s", work.ID, intake.ID)
		}
		if _, exists := previous.WorkItems[work.ID]; exists {
			return fmt.Errorf("review may create proposed work item %s only once", work.ID)
		}
		if !containsString(work.DecisionIDs, review.ID) {
			return fmt.Errorf("review work item %s must link back to review %s", work.ID, review.ID)
		}
	}
	return nil
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
