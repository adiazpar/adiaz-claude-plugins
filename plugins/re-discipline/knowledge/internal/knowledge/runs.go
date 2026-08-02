package knowledge

import (
	"errors"
	"fmt"
	"reflect"
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
			if action != "closure.start" {
				return errors.New("campaign may enter closing only through closure.start")
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
		previous.ActorID != next.ActorID || previous.Role != next.Role {
		return errors.New("run campaign, primary work item, actor, and role are immutable")
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
	actual.Digest, expected.Digest = "", ""
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("automatic curation work does not match the frozen returned run")
	}
	return nil
}
