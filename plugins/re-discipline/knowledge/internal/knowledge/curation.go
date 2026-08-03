package knowledge

import (
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
		return errors.New("curation packet must be submitted before manager review")
	}
	if len(packet.Candidates) > 10 {
		return errors.New("curation packet may contain at most 10 related findings")
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
		if coverage.Disposition == "candidate-finding" && !declared[coverage.TargetID] {
			return fmt.Errorf("coverage target %s is not in the packet", coverage.TargetID)
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
// requireReturned is true at curator submission and false when validating
// durable state after the source run has advanced to a terminal status.
func validateCurationGraphBindings(
	graph CampaignGraph,
	packet CurationPacket,
	requireReturned bool,
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
		if requireReturned && run.Status != "returned" {
			return nil, fmt.Errorf("intake source run %s must be returned before curation", run.ID)
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
	if !reflect.DeepEqual(submission.Intake, priorIntake) {
		return errors.New("review packet intake is not the exact canonical submitted intake")
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
