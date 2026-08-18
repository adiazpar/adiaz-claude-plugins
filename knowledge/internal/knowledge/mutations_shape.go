package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Shape validation for manager_apply.
//
// Everything in this file is answerable from the ManagerApplyRequest alone. No
// function here loads the campaign graph, reads the committed head, or touches
// the filesystem, which is what makes it safe to run every check against every
// request without ordering them: a check that needs nothing cannot be poisoned
// by a check that failed. See refusal_aggregate.go for why the split exists and
// what stays on the state side of it.
//
// The rule this file must never break is that aggregation changes when and how
// many violations are reported, never whether one is caught. Every refusal
// moved here still refuses; several of them now refuse alongside four others
// instead of one per round trip.

// validateManagerRequestShape collects every violation the request alone
// reveals and, on the way, performs the request-only transformations the
// engine owes the caller: deriving omitted launch handles, deriving the report
// path from the run identity, and queueing the curation work a return implies.
//
// Those transformations are done here rather than before validation because
// they read fields whose shape is checked here, and doing them first
// meant a malformed brief refused from inside a helper whose job is derivation,
// as a single fact, before the payload had been looked at once. The derivation
// is gated on its own inputs being clean and skipped otherwise, so a request
// with a broken brief gets the brief violation and not a second, derived one
// about the handle that could not be built from it.
//
// The returned request is the transformed one. It is only meaningful when the
// error is nil; on a refusal the caller should use it for nothing but naming
// the action in the message.
func validateManagerRequestShape(
	request ManagerApplyRequest,
	allowed map[string]bool,
	configuration Configuration,
) (ManagerApplyRequest, error) {
	set := newRefusalSet("manager_apply " + request.Action)
	collectManagerEnvelopeShape(set, request)
	if request.Action == "campaign.open" {
		// Four separate preconditions used to collapse into one sentence, so a
		// caller that got three of them right learned only that something was
		// wrong. Say which one failed and what value would satisfy it.
		collectCampaignOpenPayload(set, request)
	}
	collectManagerActionPayload(set, request, allowed, configuration)
	launchDerivable := collectManagerLaunchShape(set, request)

	if launchDerivable {
		completed, err := completeRunPreparationHandles(request)
		if err != nil {
			// Unreachable when collectManagerLaunchShape passed, because it
			// proved the same bytes canonical. Recorded rather than dropped:
			// a silent divergence between the two would hand the caller a
			// half-derived request.
			set.add(shapeStageArtifact, err)
			launchDerivable = false
		} else {
			request = completed
		}
	}
	completed, err := completeRunReportHandles(request)
	if err != nil {
		set.add(shapeStageArtifact, err)
	} else {
		request = completed
	}
	// The curation queue a run.return implies is engine-owned, so it is
	// appended before per-record validation and validated with everything else.
	request = augmentRunReturnCuration(request)
	collectManagerRecordShape(set, request, launchDerivable)
	return request, set.result()
}

// mutationEnvelope is the transaction identity every mutating tool requires,
// whatever it writes. The three mutation surfaces - manager_apply,
// curation_submit, and closure_apply - carry the same six fields under the same
// names, and all three used to discover them at different depths: closure_apply
// named the missing ones in a list, manager_apply and curation_submit let them
// fall through to prepareTransactionRequest's "transaction identity, authority,
// action, expected head, and writes are required", a sentence that names the
// union of six requirements and says which one failed only by omission.
type mutationEnvelope struct {
	Actor                string
	CampaignID           string
	CampaignSlug         string
	CorrelationID        string
	IdempotencyKey       string
	ExpectedHeadDigest   string
	ExpectedHeadRevision int64
}

// missing names, in a fixed order, the envelope fields this mutation did not
// supply. includeActor is false for manager_apply, whose actor has a sentence
// of its own because the value must appear on a list the caller may not have
// read yet. requireHead is true only for project-global transitions; ordinary
// record-scoped work lets the engine choose and serialize the publication head.
func (envelope mutationEnvelope) missing(includeActor, requireHead bool) []string {
	fields := []struct {
		name    string
		value   string
		include bool
	}{
		{"actor", envelope.Actor, includeActor},
		{"campaignId", envelope.CampaignID, true},
		{"campaignSlug", envelope.CampaignSlug, true},
		{"correlationId", envelope.CorrelationID, true},
		{"idempotencyKey", envelope.IdempotencyKey, true},
	}
	missing := []string{}
	for _, field := range fields {
		if field.include && strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	headSupplied := envelope.ExpectedHeadDigest != "" || envelope.ExpectedHeadRevision != 0
	if requireHead || headSupplied {
		if !digestRE.MatchString(envelope.ExpectedHeadDigest) {
			missing = append(missing, "expectedHeadDigest (sha256:<64 hex>)")
		}
		if envelope.ExpectedHeadRevision < 0 {
			missing = append(missing, "expectedHeadRevision (a non-negative head revision)")
		}
	}
	return missing
}

// collectMutationEnvelopeShape records one violation naming every envelope
// field the request is missing. One violation rather than six: these are all
// the same edit, read off the same state mode=orient response, and six numbered
// items saying "you also did not send this id" is a wall, not a list.
func collectMutationEnvelopeShape(
	set *refusalSet,
	subject string,
	envelope mutationEnvelope,
	includeActor, requireHead bool,
) {
	missing := envelope.missing(includeActor, requireHead)
	if len(missing) == 0 {
		return
	}
	headGuidance := ""
	if requireHead {
		headGuidance = " state mode=orient returns the exact current head for this project-global transition;"
	} else if envelope.ExpectedHeadDigest != "" || envelope.ExpectedHeadRevision != 0 {
		headGuidance = " expectedHeadRevision and expectedHeadDigest are optional for this record-scoped transition, but when either is supplied they must form one valid pair;"
	}
	set.addf(shapeStageEnvelope,
		"%s is a mutation and requires %s.%s state mode=orient returns the campaign id and slug; "+
			"correlationId and idempotencyKey are "+
			"caller-chosen and must be stable across retries of this same transition",
		subject, strings.Join(missing, ", "), headGuidance)
}

func collectManagerEnvelopeShape(set *refusalSet, request ManagerApplyRequest) {
	if strings.TrimSpace(request.Actor) == "" {
		set.add(shapeStageEnvelope, errors.New(
			"manager_apply requires actor: the identity journaled with the transition. It must "+
				"already appear in the campaign's permittedManagers, which state mode=orient "+
				"returns and manager_apply campaign.update is the only transition that changes"))
	}
	collectMutationEnvelopeShape(set, "manager_apply "+request.Action, mutationEnvelope{
		Actor: request.Actor, CampaignID: request.CampaignID, CampaignSlug: request.CampaignSlug,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadDigest:   request.ExpectedHeadDigest,
		ExpectedHeadRevision: request.ExpectedHeadRevision,
	}, false, !managerActionRebasesHead(request.Action))
}

// collectCampaignOpenPayload splits the one precondition campaign.open used to
// state as a single sentence into the four it actually is. campaign.open is the
// first call a caller ever makes against this engine, so it is the worst place
// to answer "no" without saying which half of the payload was wrong - and the
// worst place to answer with one quarter of it at a time.
func collectCampaignOpenPayload(set *refusalSet, request ManagerApplyRequest) {
	if request.Campaign == nil {
		set.add(shapeStageComposition, errors.New(
			"campaign.open requires campaign: the new campaign record at revision 1. "+
				managerActionObligation("campaign.open")))
	}
	if len(request.WorkItems) == 0 {
		set.add(shapeStageComposition, errors.New(
			"campaign.open requires at least one work item: the campaign's root work. A campaign "+
				"with no work item has nothing a run can be prepared against"))
	}
	if request.Campaign == nil {
		// The remaining two rules are statements about the submitted campaign
		// record. With no record they have no subject, and answering them from
		// a zero value would report the absent record's default status as if
		// the caller had chosen it.
		return
	}
	if request.Campaign.Status != "open" {
		set.addf(shapeStagePayload,
			"campaign.open requires campaign.status \"open\"; the submitted campaign is %q. A "+
				"campaign reaches closing only through closure_apply start and closed only "+
				"through closure_apply finalize",
			request.Campaign.Status)
	}
	if !containsString(request.Campaign.PermittedManagers, request.Actor) {
		set.addf(shapeStagePayload,
			"campaign.open actor %q must appear in the submitted campaign.permittedManagers %s; "+
				"a manager cannot open a campaign it is not permitted to manage. Remedy: add %q "+
				"to permittedManagers in the same submitted campaign record",
			request.Actor, strings.Join(request.Campaign.PermittedManagers, ", "), request.Actor)
	}
}

// collectManagerLaunchShape checks everything about a submitted run launch
// payload that can be decided without canonical state, and reports whether the
// engine may go on to derive the launch handles from it.
//
// What is deliberately *not* here is the context pack's scope and generation
// binding. Those compare the pack against the campaign graph and the committed
// head, so they are state, and they stay in prepareManagerRunArtifacts failing
// fast. That is not only taxonomy: the remedy for any scope or generation
// mismatch is a single recompile of the pack, so splitting the ten-field
// comparison across a shape half and a state half would hand the caller two
// partial lists whose fix is the same one command. One list, once.
func collectManagerLaunchShape(set *refusalSet, request ManagerApplyRequest) bool {
	if !validOne(request.Action, "run.prepare", "closure.remediation.run.create") ||
		request.RunPreparation == nil || len(request.Runs) != 1 {
		// Nothing to derive. The composition rules already refused a
		// preparation action carrying the wrong number of runs, and an action
		// that launches nothing has no launch payload to be wrong about.
		return false
	}
	run := request.Runs[0]
	preparation := *request.RunPreparation
	derivable := true

	// The brief and the pack are two independent artifacts and a caller can get
	// both wrong at once, so they are checked separately rather than through
	// buildRunLaunchArtifacts, which stops at the first of the two.
	if _, err := canonicalRunBrief(preparation.Brief, run.WriteGrants); err != nil {
		set.add(shapeStageArtifact, err)
		derivable = false
	}
	if _, err := VerifyContextPackValueExpected(
		preparation.ContextPack, preparation.ContextPack.Digest, preparation.ContextPack.PackID,
	); err != nil {
		set.addf(shapeStageArtifact, "run preparation context pack: %w", err)
		derivable = false
	}

	expectedRole := "drafter"
	if run.Role == "manager" {
		expectedRole = "manager"
	}
	if preparation.ContextPack.Role != expectedRole {
		set.addf(shapeStagePayload, "run role %s requires a %s context pack", run.Role, expectedRole)
	}
	if !EqualWriteGrants(run.WriteGrants, preparation.ContextPack.WriteGrants) {
		set.add(shapeStagePayload, errors.New(
			"the run record's writeGrants and the context pack's writeGrants differ; the pack "+
				"seals the grants into the brief the drafter reads, so the two can never "+
				"disagree. Remedy: recompile the pack with context_pack_materialize "+
				"action=preview passing exactly the writeGrants the run record carries"))
	}

	// Both handles are derived from the request's own slug and the run's own
	// primaryWorkItemId, so whether the pack carries them is a fact about the
	// submitted pack and nothing else.
	campaignHandle := "record:active/" + request.CampaignSlug + "/campaign.json"
	workHandle := "record:active/" + request.CampaignSlug + "/work-items/" + run.PrimaryWorkItemID + ".json"
	missingHandles := []string{}
	if !containsString(preparation.ContextPack.RequiredHandles, campaignHandle) {
		missingHandles = append(missingHandles, campaignHandle)
	}
	if !containsString(preparation.ContextPack.RequiredHandles, workHandle) {
		missingHandles = append(missingHandles, workHandle)
	}
	if len(missingHandles) != 0 {
		set.addf(shapeStagePayload,
			"run preparation context pack omits required handle(s) %s; a run's pack must always "+
				"carry its own campaign and work item so the drafter cannot be launched without "+
				"the record defining its task. Remedy: recompile with "+
				"context_pack_materialize action=preview against target kind=active-run "+
				"campaignId=%s workItemId=%s runId=%s, which adds both handles",
			strings.Join(missingHandles, " and "),
			request.CampaignID, run.PrimaryWorkItemID, run.ID)
	}

	// One revision per record per transaction. Two copies of the primary work
	// item is a fact about the submitted array, not about what is on disk.
	duplicates := 0
	for index := range request.WorkItems {
		if request.WorkItems[index].ID == run.PrimaryWorkItemID {
			duplicates++
		}
	}
	if duplicates > 1 {
		set.addf(shapeStagePayload,
			"run preparation submits work item %s more than once; a transaction writes one "+
				"revision per record. Remedy: send exactly one next revision of %s",
			run.PrimaryWorkItemID, run.PrimaryWorkItemID)
	}
	return derivable
}

// collectManagerRecordShape validates every record the transaction would write:
// its identity, the canonical path it belongs at, the compare-and-swap digest
// an update requires, and the record's own field rules.
//
// It runs the same derivation the write path runs, over the same values in the
// same order, so a violation reported here is exactly the one the caller would
// have met later - one record at a time, one round trip each. On a review
// submission that carries an intake, a review, and six candidate findings, that
// is the difference between one refusal and eight.
//
// runRecordsDerivable is false when the launch payload could not produce the
// brief and context-pack handles the engine owns. Run records are skipped in
// that case: their handles are legitimately absent until derivation fills them
// in, and validating them first would refuse a request for a field the caller
// was invited to omit.
func collectManagerRecordShape(set *refusalSet, request ManagerApplyRequest, runRecordsDerivable bool) {
	preparation := validOne(request.Action, "run.prepare", "closure.remediation.run.create") &&
		request.RunPreparation != nil
	eventID := eventIDForRequest(managerStateTransactionRequest(request, nil, nil, ""))
	for _, value := range managerRecordValues(request) {
		if _, isRun := value.(RunRecord); isRun && preparation && !runRecordsDerivable {
			continue
		}
		write, err := managerRecordWrite(request, value)
		if err != nil {
			set.add(shapeStageBinding, err)
			continue
		}
		// prepareStateWrite is the transaction's own record gate: canonical
		// path, sealed field rules, and the revision arithmetic between the
		// submitted record and the expectation accompanying it. Every input it
		// reads here comes from the request - the predicted event id and the
		// resulting head revision are both computed from caller-supplied
		// fields - so running it early reports what the commit would have
		// reported, without writing anything.
		if _, err := prepareStateWrite(
			request.CampaignSlug, request.CampaignID, request.CorrelationID,
			eventID, request.ExpectedHeadRevision+1, write, request.Action,
		); err != nil {
			set.add(shapeStageBinding, err)
		}
	}
}

// collectManagerActionPayload is the aggregating form of the per-action payload
// rules. Every branch that used to return now records and continues, except
// where continuing would mean judging a record that is not there; those are
// marked at the point they give up and say what they were the premise for.
func collectManagerActionPayload(
	set *refusalSet,
	request ManagerApplyRequest,
	allowed map[string]bool,
	configuration Configuration,
) {
	archiveAction := request.Action == "knowledge.archive-fallback.opt-in"
	relocationAction := request.Action == "truth.relocate"
	if len(request.TruthRelocations) != 0 && !relocationAction {
		set.addf(shapeStageComposition,
			"manager action %s cannot carry truthRelocations; only truth.relocate accepts them. "+
				"Remedy: drop the field, or re-issue as truth.relocate",
			request.Action)
	}
	if relocationAction && len(request.TruthRelocations) == 0 {
		set.add(shapeStageComposition, errors.New(
			"truth.relocate requires truthRelocations: "+managerActionObligation("truth.relocate")))
	}
	if len(request.TruthRelocations) > maxTruthRelocations {
		set.addf(shapeStageComposition,
			"truth.relocate accepts at most %d truthRelocations per atomic transition",
			maxTruthRelocations)
	}
	if request.ArchiveFallbackDecision != nil && !archiveAction {
		set.addf(shapeStageComposition,
			"manager action %s cannot carry archiveFallbackDecision; only "+
				"knowledge.archive-fallback.opt-in accepts it. Remedy: drop the field, or "+
				"re-issue as knowledge.archive-fallback.opt-in carrying only the decision",
			request.Action)
	}
	if archiveAction && request.ArchiveFallbackDecision == nil {
		// Every field here is named, and none of them is discoverable through
		// the MCP surface: the candidate is a normalized-vs-raw benchmark
		// report under .re-discipline/knowledge/measurements/, and no tool
		// lists those. Naming the file is the most this refusal can honestly
		// do; see the note in the schema for what would close the gap.
		set.add(shapeStageComposition, errors.New(
			"knowledge.archive-fallback.opt-in requires archiveFallbackDecision with "+
				"candidateRunId, candidateReportDigest, candidateContentDigest, ratifiedAt, "+
				"and expectedSettingsDigest. The candidate is a passing normalized-vs-raw "+
				"measurement at .re-discipline/knowledge/measurements/normalized-vs-raw/"+
				"<candidateRunId>/report.json, and expectedSettingsDigest is the exact current "+
				"byte digest of .re-discipline/knowledge/policy.jsonc"))
	}
	preparationAction := validOne(request.Action, "run.prepare", "closure.remediation.run.create")
	if request.RunPreparation != nil && !preparationAction {
		set.addf(shapeStageComposition,
			"manager action %s cannot carry runPreparation; only run.prepare and "+
				"closure.remediation.run.create launch a run. Remedy: drop the field, or "+
				"re-issue as run.prepare",
			request.Action)
	}
	kinds := map[string]int{
		"campaign": boolCount(request.Campaign != nil), "work": len(request.WorkItems),
		"run": len(request.Runs), "finding": len(request.Findings),
		"intake": boolCount(request.Intake != nil), "review": boolCount(request.Review != nil),
	}
	total := 0
	// Iterate a fixed order rather than the map's: a request that carries two
	// disallowed kinds must refuse with the same messages in the same order
	// every time, or two identical retries look like two different failures.
	for _, kind := range []string{"campaign", "work", "run", "finding", "intake", "review"} {
		count := kinds[kind]
		if count > 0 && !allowed[kind] {
			set.addf(shapeStageComposition,
				"manager action %s cannot write %s records; it accepts %s. %s records are "+
					"written by %s",
				request.Action, kind, describeAllowedRecordKinds(allowed), kind,
				managerActionsAcceptingKind(kind))
		}
		total += count
	}
	if total == 0 && !archiveAction && !relocationAction {
		// A rationale is transaction metadata journaled with every transition;
		// it is not itself a record. Naming the accepted kinds and the concrete
		// obligation keeps a caller from concluding that the action is broken.
		set.addf(shapeStageComposition,
			"manager action %s contains no typed records; it accepts %s. %s",
			request.Action, describeAllowedRecordKinds(allowed),
			managerActionObligation(request.Action))
	}
	switch request.Action {
	case "campaign.update":
		if request.Campaign == nil {
			set.add(shapeStageComposition, errors.New(
				"campaign.update requires campaign: "+managerActionObligation("campaign.update")))
		}
	case "work.create", "work.update":
		if len(request.WorkItems) == 0 {
			set.addf(shapeStageComposition,
				"%s requires workItems: %s", request.Action, managerActionObligation(request.Action))
		}
	case "knowledge.archive-fallback.opt-in":
		if total != 0 || request.RunPreparation != nil || request.ReviewPacket != nil {
			set.add(shapeStageComposition, errors.New(
				"knowledge.archive-fallback.opt-in may carry only archiveFallbackDecision; it "+
					"ratifies a retrieval policy and writes no campaign record. Remedy: drop "+
					"every typed record, runPreparation, and reviewPacket from this request and "+
					"submit any record changes as their own transitions"))
		}
	case "truth.relocate":
		if total != 0 || request.RunPreparation != nil || request.ReviewPacket != nil ||
			request.ArchiveFallbackDecision != nil || request.CampaignMerge != nil ||
			request.CampaignDiscard != nil {
			set.add(shapeStageComposition, errors.New(
				"truth.relocate may carry only truthRelocations; it normalizes existing truth "+
					"artifacts and writes no campaign record. Remedy: drop every typed record and "+
					"unrelated special payload from this request"))
		}
		seenSources := map[string]bool{}
		for _, relocation := range request.TruthRelocations {
			if err := validateRelocatableTruthSource(relocation.Source); err != nil {
				set.addf(shapeStagePayload, "truth relocation source %q: %v", relocation.Source, err)
			}
			if !digestRE.MatchString(relocation.ExpectedDigest) {
				set.addf(shapeStagePayload,
					"truth relocation %s requires expectedDigest as lowercase sha256:<64 hex>",
					relocation.Source)
			}
			if seenSources[relocation.Source] {
				set.addf(shapeStagePayload, "truth relocation repeats source %s", relocation.Source)
			}
			seenSources[relocation.Source] = true
		}
	case "run.prepare", "run.start", "run.return", "run.complete", "closure.remediation.run.create":
		collectManagerRunPayload(set, request, preparationAction)
	case "review.submit", "decision.record":
		collectManagerReviewPayload(set, request, configuration)
	case "intake.coverage.retire":
		collectCoverageRetirementPayload(set, request)
	case "finding.challenge":
		if len(request.Findings) == 0 {
			set.add(shapeStageComposition, errors.New(
				"finding.challenge requires findings: "+managerActionObligation("finding.challenge")))
		}
		// Every submitted finding is judged, not just the first. A challenge
		// batch that got the validity wrong got it wrong on all of them, and
		// naming one at a time turned a single edit into one round trip per
		// finding.
		for _, finding := range request.Findings {
			if finding.Record.Validity != "challenged" {
				set.addf(shapeStagePayload,
					"finding.challenge may publish only findings whose validity is \"challenged\"; "+
						"finding %s is %q. Remedy: set validity to \"challenged\" on this "+
						"revision, or use manager_apply finding.update for a revision that is "+
						"not a challenge",
					finding.Record.ID, finding.Record.Validity)
			}
		}
	}
}

// collectManagerRunPayload holds the run-transition rules. Everything after the
// cardinality gate reads request.Runs[0], so the gate is the one place in this
// file that stops: with no run, or with two, there is no single run whose
// status, role, or primary work item could be judged, and answering from a zero
// value would report a status the caller never sent.
func collectManagerRunPayload(set *refusalSet, request ManagerApplyRequest, preparationAction bool) {
	if len(request.Runs) != 1 || len(request.WorkItems) == 0 {
		set.addf(shapeStageComposition,
			"%s requires exactly one run and at least one work item; it carries %d run(s) "+
				"and %d work item(s). %s",
			request.Action, len(request.Runs), len(request.WorkItems),
			managerActionObligation(request.Action))
	}
	if len(request.Runs) != 1 {
		return
	}
	run := request.Runs[0]
	if run.Report != nil && run.Report.Path != "" {
		set.addf(shapeStagePayload,
			"%s must not supply report.path for run %s; the report path is engine-owned. "+
				"Submit only report.sha256 and the engine derives active/<slug>/runs/<run-id>/report.md",
			request.Action, run.ID)
	}
	if len(request.WorkItems) != 0 {
		// Only asked when work items were sent at all. With an empty array the
		// requirement above already says the whole answer, and repeating it as
		// "the one you did not send is missing" is noise.
		primaryIncluded := false
		for _, work := range request.WorkItems {
			primaryIncluded = primaryIncluded || work.ID == run.PrimaryWorkItemID
		}
		if !primaryIncluded {
			set.addf(shapeStagePayload,
				"%s must publish the next revision of work item %s, the run's primaryWorkItemId; "+
					"a run transition and the work item it advances are one transaction so the "+
					"work item can never lag the run. state mode=work workItemId=%s returns the "+
					"current revision and digest",
				request.Action, run.PrimaryWorkItemID, run.PrimaryWorkItemID)
		}
	}
	expectedStatus := map[string]string{
		"run.prepare": "prepared", "run.start": "running", "run.return": "returned",
		"closure.remediation.run.create": "prepared",
	}[request.Action]
	if request.Action != "run.complete" && run.Status != expectedStatus {
		set.addf(shapeStagePayload,
			"%s must submit the run at status %q; it is %q. The run lifecycle is "+
				"prepared (run.prepare) -> running (run.start) -> returned (run.return) -> "+
				"completed, blocked, aborted, or invalidated (run.complete). Remedy: set the "+
				"run's status to %q, or issue the transition that owns %q",
			request.Action, expectedStatus, run.Status, expectedStatus, run.Status)
	}
	if request.Action == "run.complete" &&
		!validOne(run.Status, "completed", "blocked", "aborted", "invalidated") {
		set.addf(shapeStagePayload,
			"run.complete must submit the run at a terminal status - completed, blocked, "+
				"aborted, or invalidated - and it is %q. Remedy: set one of the four, or "+
				"issue the transition that owns %q",
			run.Status, run.Status)
	}
	if preparationAction && run.Role != "manager" && request.RunPreparation == nil {
		set.addf(shapeStageComposition,
			"%s of a %s run requires runPreparation: the brief text and the immutable "+
				"context pack. Compile the pack with context_pack_materialize action=preview "+
				"against target kind=active-run for this campaign, work item, and run, and "+
				"submit the previewed pack unchanged; the run record's brief and contextPack "+
				"handles may be omitted and the engine derives them",
			request.Action, run.Role)
	}
	if request.Action != "run.return" {
		return
	}
	// These two used to live in augmentRunReturnCuration, where they refused
	// from inside a helper whose job is to append the curation work item. They
	// are ordinary shape rules about the submitted run and belong with the rest
	// of them; the helper is now free to be a pure transformation.
	if run.Report == nil {
		set.add(shapeStagePayload, errors.New(
			"run.return requires a returned run with the report SHA-256: the immutable "+
				"report the drafter left behind. Remedy: set report.sha256 and omit "+
				"report.path; the engine derives the canonical path"))
	}
	if run.Role != "curator" {
		queueID := continuousCurationWorkID(run.ID)
		for _, work := range request.WorkItems {
			if work.ID == queueID {
				set.addf(shapeStagePayload,
					"run.return curation queue id %s is system-owned; the engine appends that "+
						"work item itself so a returned run can never exist without the curation "+
						"it implies. Remedy: drop the work item with id %s from this request",
					queueID, queueID)
			}
		}
	}
}

// collectManagerReviewPayload holds the review-transaction rules. The three
// records it relates are checked for presence first and then treated as
// present, because every rule after that point is a cross-check between them:
// with the packet absent there is nothing for the review's packetDigest to
// name, and reporting "digest mismatch" against a zero value would be inventing
// a disagreement the caller never expressed.
func collectManagerReviewPayload(set *refusalSet, request ManagerApplyRequest, configuration Configuration) {
	if request.Review == nil || request.Intake == nil || request.ReviewPacket == nil {
		set.addf(shapeStageComposition,
			"manager action %s requires review, resulting intake, and the exact reviewed packet: %s",
			request.Action, managerActionObligation(request.Action))
		return
	}
	packet := request.ReviewPacket.Packet()
	set.add(shapeStagePayload, ValidateReviewPacketEnvelope(request.ReviewPacket.Envelope, packet))
	set.add(shapeStagePayload, ValidateManagerReview("manager", packet, *request.Review))
	if request.Review.PacketDigest != request.ReviewPacket.Envelope.Digest {
		set.addf(shapeStagePayload,
			"review.packetDigest is %s but the submitted reviewPacket.envelope.digest is %s; "+
				"the receipt must name the exact packet that was read. Remedy: set "+
				"review.packetDigest to %s",
			request.Review.PacketDigest, request.ReviewPacket.Envelope.Digest,
			request.ReviewPacket.Envelope.Digest)
	}
	if !configuration.Valid {
		// This is the one rule in the review branch that is not about the
		// request: it is about the project's own policy file. It is collected
		// with the rest because it costs the caller a round trip either way,
		// and it gates the load binding below, which cannot be answered without
		// a declared bootstrap.
		set.add(shapeStagePayload, errors.New(
			"manager review requires valid review-load configuration; the project's "+
				".re-discipline/knowledge/policy.jsonc did not parse into a usable "+
				"reviewLoad bootstrap, so the engine cannot check that this review was "+
				"measured under a declared load. Remedy: repair or restore policy.jsonc "+
				"(re-discipline init-project reinstalls the canonical file) and re-submit; "+
				"no state was written"))
	} else {
		set.add(shapeStagePayload, ValidateReviewLoadBinding(
			request.Review.ReviewLoad, *request.Review, packet, configuration.Bootstrap.ReviewLoad))
	}
	set.add(shapeStagePayload, ValidateReviewLoadTemporalBinding(
		request.Review.ReviewLoad, request.ReviewPacket.Envelope.CreatedAt, *request.Review))
	if request.Intake.ID != request.Review.IntakeID ||
		request.Intake.Revision != request.Review.IntakeRevision+1 ||
		request.Intake.Status != "reviewed" {
		set.addf(shapeStagePayload,
			"review submission must advance the reviewed intake in the same transaction: the "+
				"submitted intake must be id %s at revision %d with status \"reviewed\", and "+
				"it is id %s at revision %d with status %q. The review receipt and the intake "+
				"revision it produces are one write because a receipt that outlives its "+
				"intake revision cannot be told from one that was never applied",
			request.Review.IntakeID, request.Review.IntakeRevision+1,
			request.Intake.ID, request.Intake.Revision, request.Intake.Status)
	}
	outcomes := make([]FindingDocument, 0, len(request.Findings))
	for _, finding := range request.Findings {
		outcomes = append(outcomes, finding.Document())
	}
	set.add(shapeStagePayload, ValidateManagerReviewOutcomes(
		packet, *request.Review, *request.Intake, outcomes))
}

// collectCoverageRetirementPayload keeps the deliberately narrow payload gate.
// Every substantive proof - that the amendment quotes what it displaced, that
// it reconstructs the exact submitted record, that the review it names really
// ratified this intake - needs canonical prior state and belongs in
// validateAppliedCoverageRetirement. What belongs here is the shape a caller
// can get wrong before the engine has even loaded the record it is amending.
func collectCoverageRetirementPayload(set *refusalSet, request ManagerApplyRequest) {
	if request.ReviewPacket != nil || request.RunPreparation != nil {
		set.add(shapeStageComposition, errors.New(
			"a coverage retirement is not a review and prepares no run; it carries neither a review "+
				"packet nor a run preparation"))
	}
	if request.Intake == nil {
		set.addf(shapeStageComposition,
			"manager action %s requires the next revision of the intake being amended: %s",
			request.Action, managerActionObligation(request.Action))
		return
	}
	if request.Intake.Status != "reviewed" {
		set.addf(shapeStagePayload,
			"a coverage retirement amends a reviewed intake and leaves it reviewed; the submitted "+
				"intake is %s", request.Intake.Status)
	}
	if len(request.Intake.Amendments) == 0 {
		set.add(shapeStageComposition, errors.New(
			"a coverage retirement must append one amendment naming every span it retires, the exact "+
				"rationale each displaces, and the manager review it preserves"))
		return
	}
	set.add(shapeStagePayload, ValidateCoverageAmendmentShape(
		request.Intake.Amendments[len(request.Intake.Amendments)-1]))
}

// firstManagerStateViolation adds at most one canonical-state fact to an
// aggregate that is already refusing on shape.
//
// Project-global actions still use an exact head, while ordinary actions let
// the engine serialize publication. For the strict actions, reporting a stale
// head alongside a payload mistake avoids a second round trip.
//
// It is strictly bounded to one violation, and it runs only when the shape pass
// already refused, so the successful path is untouched and pays nothing for it.
// Everything it cannot establish is silently skipped rather than guessed:
// a store that will not recover or a campaign that will not load is not
// evidence about the caller's request, and turning an infrastructure error into
// a violation in this list would be reporting noise as fact.
func (service *Service) firstManagerStateViolation(ctx context.Context, request ManagerApplyRequest) error {
	if service == nil {
		return nil
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return nil
	}
	if !managerActionRebasesHead(request.Action) &&
		digestRE.MatchString(request.ExpectedHeadDigest) &&
		request.ExpectedHeadRevision >= 0 {
		head, err := store.LoadHead()
		if err != nil {
			return nil
		}
		if head.Revision != request.ExpectedHeadRevision || head.Digest != request.ExpectedHeadDigest {
			return staleHeadConflict(
				request.ExpectedHeadRevision, request.ExpectedHeadDigest, head, request.CampaignID)
		}
	}
	if request.Action == "campaign.open" {
		// There is no campaign to load yet, and authority for the first
		// transition is a property of the submitted record, which the shape
		// pass already judged.
		return nil
	}
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil || graph.Campaign == nil {
		return nil
	}
	if !containsString(graph.Campaign.PermittedManagers, request.Actor) {
		return fmt.Errorf(
			"actor %q is not a permitted manager of campaign %s; its permittedManagers are %s. "+
				"Remedy: re-issue as one of those actors, or add this one with manager_apply "+
				"campaign.update carrying the next campaign revision",
			request.Actor, graph.Campaign.ID,
			strings.Join(graph.Campaign.PermittedManagers, ", "))
	}
	return nil
}

// withFirstStateViolation folds one state fact into an existing shape refusal.
// A single shape violation plus a state violation becomes a two-item list; the
// aggregate keeps its ordering contract because the state fact always sorts
// last.
func withFirstStateViolation(subject string, shape, state error) error {
	if state == nil {
		return shape
	}
	set := newRefusalSet(subject)
	var aggregate *AggregateRefusal
	if errors.As(shape, &aggregate) {
		// Flatten rather than nest. The violations are already in repair order
		// and re-adding them at one stage preserves it, whereas an aggregate
		// containing an aggregate would render as a numbered list with a
		// multi-line item in it, which is precisely the wall of prose this is
		// supposed to replace.
		for _, violation := range aggregate.Violations {
			set.add(shapeStageEnvelope, violation)
		}
	} else {
		set.add(shapeStageEnvelope, shape)
	}
	set.add(stateStageFirst, state)
	return set.result()
}

// Shape validation for curation_submit.
//
// A curator packet is the widest multi-record payload in the engine: one intake
// plus every candidate finding it proposes. The per-record rules below fired
// one record at a time, so a packet of six candidates that all carried the same
// mistake cost six round trips, each one re-sending all six candidates. They
// are all facts about the submitted packet, so they aggregate.
func validateCurationRequestShape(request CurationSubmitRequest) (CurationPacket, error) {
	set := newRefusalSet("curation_submit")
	collectMutationEnvelopeShape(set, "curation_submit", mutationEnvelope{
		Actor: request.Actor, CampaignID: request.CampaignID, CampaignSlug: request.CampaignSlug,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadDigest:   request.ExpectedHeadDigest,
		ExpectedHeadRevision: request.ExpectedHeadRevision,
	}, true, false)
	if len(request.WorkItems) != 0 {
		set.add(shapeStageComposition, errors.New(
			"curators may propose spawned work-item ids in intake but only manager review may create work records"))
	}
	packet := CurationPacket{Intake: request.Intake, Rows: append([]CurationRow(nil), request.Rows...)}
	for _, candidate := range request.Candidates {
		document := candidate.Document()
		expectedPath, pathErr := stateRecordPath(request.CampaignSlug, document)
		switch {
		case pathErr != nil:
			set.add(shapeStagePayload, pathErr)
		case document.Record.Path != expectedPath:
			set.addf(shapeStagePayload,
				"candidate %s path must be canonical %s", document.Record.ID, expectedPath)
		}
		// Appended whichever way the path check went. A packet missing one of
		// its own candidates would make ValidateCurationPacket report a row
		// with no candidate behind it - a second, invented problem on top of
		// the real one.
		packet.Candidates = append(packet.Candidates, document)
	}
	set.add(shapeStagePayload, ValidateCurationPacket("curator", packet))
	if request.CampaignID != request.Intake.CampaignID || request.CorrelationID != request.Intake.CorrelationID {
		set.add(shapeStageBinding, errors.New(
			"curation intake does not bind the requested campaign and correlation"))
	}
	for _, value := range curationRecordValues(request) {
		if _, err := curationRecordWrite(request, value); err != nil {
			set.add(shapeStageBinding, err)
		}
	}
	return packet, set.result()
}

// curationRecordValues lists the records a curator packet writes, in the order
// the transaction writes them. Like managerRecordValues it is shared between the
// shape pass and the write path so the two cannot drift.
func curationRecordValues(request CurationSubmitRequest) []any {
	values := []any{request.Intake}
	for _, candidate := range request.Candidates {
		values = append(values, candidate.Document())
	}
	return values
}

// curationRecordWrite derives the single StateWrite one curated record produces.
// It differs from managerRecordWrite only in that curation has no
// reconcile.import exemption and names itself in the digest refusal, which is
// the refusal a caller meets most often here: an intake being amended is always
// an update, so it always needs its prior digest.
func curationRecordWrite(request CurationSubmitRequest, value any) (StateWrite, error) {
	id, revision, _, campaignID, correlationID, err := stateRecordIdentity(value, 0, request.CorrelationID)
	if err != nil {
		return StateWrite{}, err
	}
	if campaignID != request.CampaignID || correlationID != request.CorrelationID {
		return StateWrite{}, fmt.Errorf(
			"curation record %s has mismatched campaign or correlation", id)
	}
	path, err := stateRecordPath(request.CampaignSlug, value)
	if err != nil {
		return StateWrite{}, err
	}
	expected := revision - 1
	digest := ""
	if expected > 0 {
		digest = request.ExpectedRecordDigests[id]
		if !digestRE.MatchString(digest) {
			return StateWrite{}, missingExpectedDigestError("curation_submit", id, expected, path)
		}
	}
	return StateWrite{Path: path, ExpectedRevision: expected, ExpectedDigest: digest, Record: value}, nil
}

// Shape validation for closure_apply.
//
// closure_apply already answered its envelope as a list, which is where the
// pattern in this package came from. What it did not do is put the two
// start-only fields on the same list: closureJobId was checked inside
// startClosure and archiveDestination two calls further in, inside
// BuildClosurePlan, with the campaign-status and existing-job gates between
// them. A caller opening closure for the first time therefore learned "needs a
// job id", fixed it, and learned "needs an archive destination" - the first two
// refusals of the recorded seven-round-trip session, and both of them
// answerable from the request alone.
func collectClosureRequestShape(set *refusalSet, request ClosureApplyRequest) {
	collectMutationEnvelopeShape(set, "closure "+request.Action, mutationEnvelope{
		Actor: request.Actor, CampaignID: request.CampaignID, CampaignSlug: request.CampaignSlug,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadDigest:   request.ExpectedHeadDigest,
		ExpectedHeadRevision: request.ExpectedHeadRevision,
	}, true, !closureActionRebasesHead(request.Action))
	if timestamp, err := time.Parse(time.RFC3339Nano, request.Timestamp); err != nil ||
		timestamp.Location() != time.UTC {
		set.addf(shapeStageEnvelope,
			"closure %s requires timestamp as a UTC RFC3339 instant, for example "+
				"2026-01-31T14:05:00Z; it is %q. The engine never invents a closure time because "+
				"the receipt it seals is the campaign's durable record of when it closed",
			request.Action, request.Timestamp)
	}
	if request.Action != "start" {
		// Every other closure action's inputs are judged against the job on
		// disk: which stage may follow this one, which plan revision is being
		// replaced, which spans are already dispositioned. None of that is
		// answerable from the request, so it stays failing fast where the job
		// is loaded.
		return
	}
	if !correlationIDRE.MatchString(request.ClosureJobID) {
		set.addf(shapeStagePayload,
			"closure start requires closureJobId matching %s; it is %q. It is caller-chosen and "+
				"permanent - the job keeps this id across reopen and restart, and every closure "+
				"transition after this one names it",
			correlationIDRE.String(), request.ClosureJobID)
	}
	if err := validateArchiveDestination(request.ArchiveDestination); err != nil {
		set.addf(shapeStagePayload,
			"closure start requires archiveDestination, the durable tree this campaign's records "+
				"are frozen into: %w. Remedy: pass docs/history/campaigns/<archive-slug>, which is "+
				"where every finalized campaign in this project lives",
			err)
	}
}
