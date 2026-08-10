package knowledge

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

var campaignTransitions = map[string]map[string]bool{
	"open":      {"open": true, "paused": true, "closing": true, "cancelled": true},
	"paused":    {"paused": true, "open": true, "closing": true, "cancelled": true},
	"closing":   {"closing": true, "open": true, "closed": true, "cancelled": true},
	"closed":    {"closed": true},
	"cancelled": {"cancelled": true},
}

var workItemTransitions = map[string]map[string]bool{
	"proposed":   {"proposed": true, "ready": true, "deferred": true, "cancelled": true, "superseded": true},
	"ready":      {"ready": true, "active": true, "blocked": true, "deferred": true, "cancelled": true, "superseded": true},
	"active":     {"active": true, "blocked": true, "deferred": true, "done": true, "cancelled": true, "superseded": true},
	"blocked":    {"blocked": true, "ready": true, "active": true, "deferred": true, "cancelled": true, "superseded": true},
	"deferred":   {"deferred": true, "ready": true, "active": true, "cancelled": true, "superseded": true},
	"done":       {"done": true, "superseded": true},
	"cancelled":  {"cancelled": true},
	"superseded": {"superseded": true},
}

var runTransitions = map[string]map[string]bool{
	"prepared":    {"prepared": true, "running": true, "aborted": true, "invalidated": true},
	"running":     {"running": true, "returned": true, "aborted": true, "invalidated": true},
	"returned":    {"returned": true, "completed": true, "blocked": true, "invalidated": true},
	"completed":   {"completed": true, "invalidated": true},
	"blocked":     {"blocked": true, "invalidated": true},
	"aborted":     {"aborted": true},
	"invalidated": {"invalidated": true},
}

func validateMetaTransition(previous, next RecordMeta) error {
	if previous.SchemaVersion != next.SchemaVersion || previous.ID != next.ID ||
		previous.CreatedAt != next.CreatedAt || previous.CreatedBy != next.CreatedBy {
		return errors.New("record identity and creation metadata are immutable")
	}
	if next.Revision != previous.Revision+1 {
		return fmt.Errorf("record revision must advance from %d to %d", previous.Revision, previous.Revision+1)
	}
	previousUpdated, _ := time.Parse(time.RFC3339Nano, previous.UpdatedAt)
	nextUpdated, _ := time.Parse(time.RFC3339Nano, next.UpdatedAt)
	if nextUpdated.Before(previousUpdated) {
		return errors.New("record updatedAt cannot move backwards")
	}
	return nil
}

func ValidateCampaignTransition(previous *CampaignRecord, next CampaignRecord, actions ...string) error {
	if err := ValidateCampaign(next); err != nil {
		return err
	}
	if previous == nil {
		if next.Revision != 1 || next.Status != "open" {
			return errors.New("a new campaign must begin open at revision 1")
		}
		return nil
	}
	if err := validateMetaTransition(previous.RecordMeta, next.RecordMeta); err != nil {
		return err
	}
	if previous.Slug != next.Slug || previous.OpenedAt != next.OpenedAt {
		return errors.New("campaign slug and openedAt are immutable")
	}
	if !campaignTransitions[previous.Status][next.Status] {
		return fmt.Errorf("illegal campaign transition %s -> %s", previous.Status, next.Status)
	}
	action := ""
	if len(actions) != 0 {
		action = actions[0]
	}
	if previous.Status != next.Status {
		switch next.Status {
		case "closing":
			// closure.restart is the second door into `closing`, and it exists for
			// exactly one situation: the campaign already took closure.reopen, did
			// the remediation the closure gate asked for, and now has to get back
			// in. Before restart, closure.start was the only entry and it refuses
			// while any closure job exists, so reopen stranded the campaign
			// permanently.
			if !validOne(action, "closure.start", "closure.restart") {
				return errors.New("campaign may enter closing only through closure.start or closure.restart")
			}
		case "closed":
			if action != "closure.finalize" {
				return errors.New("campaign may enter closed only through closure.finalize")
			}
		case "open":
			if previous.Status == "closing" && action != "closure.reopen" {
				return errors.New("a closing campaign may reopen only through closure.reopen")
			}
		}
	}
	return nil
}

func ValidateWorkItemTransition(previous *WorkItemRecord, next WorkItemRecord) error {
	if err := ValidateWorkItem(next); err != nil {
		return err
	}
	if previous == nil {
		if next.Revision != 1 || !validOne(next.State, "proposed", "ready") {
			return errors.New("a new work item must begin proposed or ready at revision 1")
		}
		return nil
	}
	if err := validateMetaTransition(previous.RecordMeta, next.RecordMeta); err != nil {
		return err
	}
	if previous.CampaignID != next.CampaignID || previous.Kind != next.Kind {
		return errors.New("work-item campaign and kind are immutable")
	}
	if !workItemTransitions[previous.State][next.State] {
		return fmt.Errorf("illegal work-item transition %s -> %s", previous.State, next.State)
	}
	return nil
}

func ValidateRunTransition(previous *RunRecord, next RunRecord) error {
	if err := ValidateRun(next); err != nil {
		return err
	}
	if previous == nil {
		if next.Revision != 1 || next.Status != "prepared" {
			return errors.New("a new run must begin prepared at revision 1")
		}
		return nil
	}
	if err := validateMetaTransition(previous.RecordMeta, next.RecordMeta); err != nil {
		return err
	}
	if previous.CampaignID != next.CampaignID || previous.PrimaryWorkItemID != next.PrimaryWorkItemID ||
		previous.ActorID != next.ActorID || previous.Role != next.Role ||
		!EqualWriteGrants(previous.WriteGrants, next.WriteGrants) {
		return errors.New("run campaign, primary work item, actor, role, and write grants are immutable")
	}
	if !runTransitions[previous.Status][next.Status] {
		return fmt.Errorf("illegal run transition %s -> %s", previous.Status, next.Status)
	}
	if validOne(previous.Status, "returned", "completed", "blocked") && !reflect.DeepEqual(previous.Report, next.Report) {
		return errors.New("a returned report digest is frozen")
	}
	if isTerminalRun(previous.Status) && previous.Status == next.Status && !reflect.DeepEqual(*previous, next) {
		return errors.New("terminal run records are immutable except for explicit invalidation")
	}
	return nil
}

func isTerminalRun(status string) bool {
	return validOne(status, "completed", "blocked", "aborted", "invalidated")
}

func validateAppliedRunReturn(previous CampaignGraph, request StateTransactionRequest, writes []preparedStateWrite) error {
	var returned *RunRecord
	workWrites := map[string]preparedStateWrite{}
	for _, write := range writes {
		switch record := write.Record.(type) {
		case RunRecord:
			if returned != nil {
				return errors.New("run.return must publish exactly one returned run")
			}
			copy := record
			returned = &copy
		case WorkItemRecord:
			workWrites[record.ID] = write
		}
	}
	if returned == nil || returned.Status != "returned" || returned.Report == nil {
		return errors.New("run.return must publish one returned run with a report")
	}
	prior, present := previous.Runs[returned.ID]
	if !present || prior.Status != "running" {
		return errors.New("run.return may only freeze a currently running run")
	}
	if _, present := workWrites[returned.PrimaryWorkItemID]; !present {
		return errors.New("run.return must transactionally publish its primary work item")
	}
	if returned.Role == "curator" {
		return nil
	}
	queueID := continuousCurationWorkID(returned.ID)
	if !containsString(returned.SpawnedWorkItemIDs, queueID) {
		return errors.New("substantive run.return must reference its automatic curation work")
	}
	write, present := workWrites[queueID]
	if !present || write.ExpectedRevision != 0 {
		return errors.New("substantive run.return must create its automatic curation work in the same transaction")
	}
	actual, ok := write.Record.(WorkItemRecord)
	if !ok {
		return errors.New("automatic curation queue is not a work-item record")
	}
	expected := continuousCurationWork(*returned, request.Actor, request.CorrelationID)
	// Prepared writes are normalized before this invariant runs. Normalize the
	// engine-generated expectation and the supplied value so nil and canonical
	// empty slices cannot make either the pre-seal or service-level check fail.
	normalizeWorkItemRecord(&actual)
	normalizeWorkItemRecord(&expected)
	actual.Digest, expected.Digest = "", ""
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("automatic curation work does not match the frozen returned run")
	}
	return nil
}

// validateAppliedRunCompletion refuses to move a run out of `returned` while
// its frozen report has no clean curator intake.
//
// `returned` is the state in which curation_submit accepts an intake over a
// run's report unconditionally (validateCurationSourceRunAdmissible), and
// runTransitions has no edge back: completed and blocked reach only themselves
// and invalidated. ComputeClosureCoverage, meanwhile, classifies every
// non-aborted run with a report by reviewedReportCoverage, which ignores any
// source whose intake still disposes a span as `unresolved`. Completing a run
// whose intake is dirty or absent therefore fixes "missing-reviewed-intake"
// into the campaign forever: the closure gate can never be satisfied and the
// campaign can never close.
//
// This transition is the last point at which the situation is still repairable,
// so it is where the engine must refuse, naming the run, the spans that are
// unresolved, and the curation that would clear them.
func validateAppliedRunCompletion(previous CampaignGraph, writes []preparedStateWrite) error {
	for _, write := range writes {
		run, isRun := write.Record.(RunRecord)
		if !isRun {
			continue
		}
		prior, present := previous.Runs[run.ID]
		if !present || prior.Status != "returned" || run.Status == prior.Status {
			continue
		}
		// `invalidated` is the manager's explicit void path and the only other
		// exit from `returned`; refusing it too would leave a dirty run with no
		// exit at all. It does not escape the coverage obligation - closure's
		// exemptions are for runs that froze no report, and a run leaving
		// `returned` always has one, which a ratified finding may still cite -
		// so a run invalidated over a dirty intake is repaired through
		// validateCurationSourceRunAdmissible rather than prevented here.
		// `aborted` is unreachable from `returned` and is exempt from
		// reviewed-intake coverage anyway.
		if !validOne(run.Status, "completed", "blocked") || run.Report == nil {
			continue
		}
		if err := validateRunReportIsCurated(previous, run); err != nil {
			return err
		}
	}
	return nil
}

// runReportIntakeCoverage is the answer to one question asked by two guards
// that must never disagree: which non-superseded curator intakes cover this
// run's frozen report, and which of them still leave one of its spans
// `unresolved`.
//
// run.complete refuses to leave `returned` for `completed` or `blocked` unless
// Clean is non-empty (validateRunReportIsCurated). curation_submit admits a
// supplementary intake over a run that has already left `returned` for
// `completed` or `invalidated` only while Clean is empty
// (validateCurationSourceRunAdmissible). For `completed` the two predicates are
// exact complements, so they share this one computation rather than each
// restating it: no run can ever satisfy both, and none can fall between them.
// `invalidated` has no completion guard to complement - it is the one exit
// run.complete may not refuse - so the same emptiness test is the whole
// admission there.
type runReportIntakeCoverage struct {
	// Clean holds the sorted IDs of intakes that cover the report and dispose
	// every one of its spans.
	Clean []string
	// Statuses maps every covering intake ID to its record status, so a refusal
	// can say whether a clean intake is already reviewed or still pending.
	Statuses map[string]string
	// Dirty holds one rendered phrase per covering intake that still carries
	// unresolved spans, naming the exact spans.
	Dirty []string
}

func inspectRunReportIntakeCoverage(graph CampaignGraph, report FileHandle) runReportIntakeCoverage {
	sourceKey := coverageSourceKey(report.Path, report.SHA256)
	result := runReportIntakeCoverage{Statuses: map[string]string{}}
	covering := []string{}
	unresolvedByIntake := map[string][]string{}
	for _, intake := range graph.Intakes {
		if intake.Status == "superseded" {
			continue
		}
		carries := false
		for _, source := range intake.SourceRuns {
			if coverageSourceKey(source.Path, source.SHA256) == sourceKey {
				carries = true
				break
			}
		}
		if !carries {
			continue
		}
		covering = append(covering, intake.ID)
		result.Statuses[intake.ID] = intake.Status
		for _, entry := range intake.Coverage {
			if entry.Disposition != "unresolved" ||
				coverageSourceKey(entry.SourcePath, entry.SourceSHA256) != sourceKey {
				continue
			}
			unresolvedByIntake[intake.ID] = append(unresolvedByIntake[intake.ID],
				canonicalCoverageHandle(entry))
		}
	}
	sort.Strings(covering)
	for _, intakeID := range covering {
		spans := unresolvedByIntake[intakeID]
		if len(spans) == 0 {
			result.Clean = append(result.Clean, intakeID)
			continue
		}
		sort.Strings(spans)
		result.Dirty = append(result.Dirty, fmt.Sprintf("%s leaves %d span(s) unresolved (%s)",
			intakeID, len(spans), strings.Join(spans, ", ")))
	}
	return result
}

func validateRunReportIsCurated(previous CampaignGraph, run RunRecord) error {
	coverage := inspectRunReportIntakeCoverage(previous, *run.Report)
	if len(coverage.Clean) > 0 {
		return nil
	}
	cause := fmt.Sprintf("no curator intake covers its report %s", run.Report.Path)
	remedy := fmt.Sprintf(
		"while the run is still returned, submit a curator intake over %s that disposes every span as "+
			"candidate-finding, duplicate, non-claim, or out-of-scope, then complete the run",
		run.Report.Path)
	if len(coverage.Dirty) > 0 {
		cause = "its only curator intake(s) still carry unresolved coverage: " +
			strings.Join(coverage.Dirty, "; ")
		// A dirty intake that is already `reviewed` has a cheaper remedy than a
		// second curator packet. curation_submit cannot resubmit an intake a
		// manager has ratified, so repairing it there means a whole new intake
		// and a second review over candidates nobody re-read; the retirement
		// changes the disposition and rationale of the named spans and nothing
		// else, and the existing review keeps binding the record.
		if reviewedDirtyCoverageIntakes(coverage) != "" {
			remedy = fmt.Sprintf(
				"the covering intake(s) %s are already reviewed, so the cheapest repair is "+
					"manager_apply intake.coverage.retire, which re-dispositions exactly those unresolved "+
					"spans as non-claim or out-of-scope under the review that already ratified them and "+
					"touches no finding; otherwise, while the run is still returned, submit a further "+
					"curator intake over %s that disposes every span",
				reviewedDirtyCoverageIntakes(coverage), run.Report.Path)
		}
	}
	return fmt.Errorf(
		"run %s cannot leave returned as %s: %s. Closure classifies every non-aborted run by "+
			"whether a reviewed curator intake covers its exact report, and an unresolved span does "+
			"not count as coverage; no transition returns the run, and once it has left returned the "+
			"only remaining route to that coverage is the stranded-run repair curation_submit "+
			"reserves for runs that can no longer obtain it any other way. Remedy: %s",
		run.ID, run.Status, cause, remedy)
}

// reviewedDirtyCoverageIntakes names the covering intakes that are both dirty
// and already reviewed - the exact population for which a coverage retirement
// is available and a supplementary curator intake is the expensive path.
func reviewedDirtyCoverageIntakes(coverage runReportIntakeCoverage) string {
	reviewed := []string{}
	for intakeID, status := range coverage.Statuses {
		if status != "reviewed" || containsString(coverage.Clean, intakeID) {
			continue
		}
		reviewed = append(reviewed, intakeID)
	}
	sort.Strings(reviewed)
	return strings.Join(reviewed, ", ")
}
