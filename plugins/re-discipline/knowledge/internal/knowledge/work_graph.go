package knowledge

import (
	"errors"
	"fmt"
)

// CampaignGraph is a complete in-memory projection of one campaign's
// canonical control records. It contains no derived cache state.
type CampaignGraph struct {
	Campaign        *CampaignRecord
	WorkItems       map[string]WorkItemRecord
	Runs            map[string]RunRecord
	Findings        map[string]FindingRecord
	Intakes         map[string]IntakeRecord
	Reviews         map[string]ReviewRecord
	ClosurePlan     *ClosurePlan
	ClosureJob      *ClosureJob
	ClosureCoverage *ClosureCoverage
	ClosureReceipt  *ClosureReceipt
}

func NewCampaignGraph() CampaignGraph {
	return CampaignGraph{
		WorkItems: map[string]WorkItemRecord{}, Runs: map[string]RunRecord{},
		Findings: map[string]FindingRecord{}, Intakes: map[string]IntakeRecord{},
		Reviews: map[string]ReviewRecord{},
	}
}

func (graph CampaignGraph) Validate() error {
	if graph.Campaign == nil {
		return errors.New("campaign graph requires campaign.json")
	}
	if err := ValidateCampaign(*graph.Campaign); err != nil {
		return fmt.Errorf("campaign: %w", err)
	}
	campaignID := graph.Campaign.ID
	for _, focus := range graph.Campaign.CurrentFocus {
		if _, ok := graph.WorkItems[focus]; !ok {
			return fmt.Errorf("campaign focus %s does not resolve", focus)
		}
	}
	for id, item := range graph.WorkItems {
		if id != item.ID || item.CampaignID != campaignID {
			return fmt.Errorf("work item %s has mismatched identity or campaign", id)
		}
		if err := ValidateWorkItem(item); err != nil {
			return fmt.Errorf("work item %s: %w", id, err)
		}
		if err := graph.validateWorkRelations(item); err != nil {
			return err
		}
		if item.Deferment != nil {
			if trigger := item.Deferment.RevisitWhen; trigger.Type == DefermentTriggerWorkItemState {
				if trigger.WorkItemID == item.ID {
					return fmt.Errorf("work item %s deferment cannot wait on its own state", id)
				}
				if _, ok := graph.WorkItems[trigger.WorkItemID]; !ok {
					return fmt.Errorf("work item %s deferment target %s does not resolve", id, trigger.WorkItemID)
				}
			}
			if owner := item.Deferment.Owner; workItemIDRE.MatchString(owner) {
				if owner == item.ID {
					return fmt.Errorf("work item %s deferment cannot own itself", id)
				}
				if _, ok := graph.WorkItems[owner]; !ok {
					return fmt.Errorf("work item %s deferment owner %s does not resolve", id, owner)
				}
			}
		}
	}
	if err := graph.validateDependencyCycles(); err != nil {
		return err
	}
	for id, run := range graph.Runs {
		if id != run.ID || run.CampaignID != campaignID {
			return fmt.Errorf("run %s has mismatched identity or campaign", id)
		}
		if err := ValidateRun(run); err != nil {
			return fmt.Errorf("run %s: %w", id, err)
		}
		item, ok := graph.WorkItems[run.PrimaryWorkItemID]
		if !ok {
			return fmt.Errorf("run %s primary work item %s does not resolve", id, run.PrimaryWorkItemID)
		}
		if isTerminalRun(run.Status) {
			if !containsString(item.CompletedRunIDs, id) || containsString(item.ActiveRunIDs, id) {
				return fmt.Errorf("terminal run %s must appear only in completedRunIds", id)
			}
		} else if !containsString(item.ActiveRunIDs, id) || containsString(item.CompletedRunIDs, id) {
			return fmt.Errorf("non-terminal run %s must appear only in activeRunIds", id)
		}
		if run.Status == "returned" && item.State == "done" {
			return fmt.Errorf("work item %s cannot be done with unprocessed returned run %s", item.ID, id)
		}
		for _, spawnedID := range run.SpawnedWorkItemIDs {
			spawned, present := graph.WorkItems[spawnedID]
			if !present || !containsString(spawned.Relations.SpawnedByIDs, run.PrimaryWorkItemID) {
				return fmt.Errorf("run %s spawned work item %s is missing or not linked to its primary work", id, spawnedID)
			}
		}
	}
	for id, item := range graph.WorkItems {
		for _, runID := range append(append([]string{}, item.ActiveRunIDs...), item.CompletedRunIDs...) {
			run, ok := graph.Runs[runID]
			if !ok || run.PrimaryWorkItemID != id {
				return fmt.Errorf("work item %s references unrelated or missing run %s", id, runID)
			}
		}
		if item.State == "done" && len(item.ActiveRunIDs) != 0 {
			return fmt.Errorf("done work item %s still has active runs", id)
		}
	}
	for id, intake := range graph.Intakes {
		if id != intake.ID || intake.CampaignID != campaignID {
			return fmt.Errorf("intake %s has mismatched identity or campaign", id)
		}
		if err := ValidateIntake(intake); err != nil {
			return fmt.Errorf("intake %s: %w", id, err)
		}
		for _, findingID := range intake.CandidateFindingIDs {
			if _, ok := graph.Findings[findingID]; !ok {
				return fmt.Errorf("intake %s candidate finding %s does not resolve", id, findingID)
			}
		}
		packet := CurationPacket{Intake: intake}
		for _, findingID := range intake.CandidateFindingIDs {
			packet.Candidates = append(packet.Candidates, FindingDocument{Record: graph.Findings[findingID]})
		}
		if _, err := validateCurationGraphBindings(graph, packet, false); err != nil {
			return fmt.Errorf("intake %s: %w", id, err)
		}
		if intake.Status == "reviewed" && !reviewBindsIntake(graph, intake) {
			return fmt.Errorf("reviewed intake %s has no immutable review receipt", id)
		}
	}
	for id, review := range graph.Reviews {
		if id != review.ID || review.CampaignID != campaignID {
			return fmt.Errorf("review %s has mismatched identity or campaign", id)
		}
		if err := ValidateReview(review); err != nil {
			return fmt.Errorf("review %s: %w", id, err)
		}
		intake, ok := graph.Intakes[review.IntakeID]
		if !ok || intake.Status != "reviewed" || intake.Revision != review.IntakeRevision+1 {
			return fmt.Errorf("review %s does not bind the submitted revision immediately before the reviewed intake", id)
		}
		for _, decision := range review.Decisions {
			finding, ok := graph.Findings[decision.FindingID]
			if !ok || (decision.FindingRevision != finding.Revision && decision.FindingRevision != finding.Revision-1) {
				return fmt.Errorf("review %s does not bind finding %s revision", id, decision.FindingID)
			}
		}
	}
	if err := ValidateReviewLoadSessions(graph.Reviews); err != nil {
		return err
	}
	supersededBy := map[string][]string{}
	for id, finding := range graph.Findings {
		if id != finding.ID || finding.CampaignID != campaignID {
			return fmt.Errorf("finding %s has mismatched identity or campaign", id)
		}
		if err := ValidateFinding(finding); err != nil {
			return fmt.Errorf("finding %s: %w", id, err)
		}
		for _, runID := range finding.SourceRuns {
			if _, ok := graph.Runs[runID]; !ok {
				return fmt.Errorf("finding %s source run %s does not resolve", id, runID)
			}
		}
		for relation, targets := range map[string][]string{
			"supports": finding.Relations.Supports, "contradicts": finding.Relations.Contradicts,
			"depends-on": finding.Relations.DependsOn, "supersedes": finding.Relations.Supersedes,
			"duplicates": finding.Relations.Duplicates, "answers": finding.Relations.Answers,
		} {
			for _, target := range targets {
				if _, ok := graph.Findings[target]; !ok {
					return fmt.Errorf("finding %s %s relation %s does not resolve", id, relation, target)
				}
			}
		}
		for _, workID := range finding.Relations.Spawned {
			if _, ok := graph.WorkItems[workID]; !ok {
				return fmt.Errorf("finding %s spawned work item %s does not resolve", id, workID)
			}
		}
		for _, target := range finding.Relations.Supersedes {
			supersededBy[target] = append(supersededBy[target], id)
		}
		if finding.ReviewState == "manager-ratified" && !graph.hasFindingDecision(finding, "ratify") {
			return fmt.Errorf("manager-ratified finding %s has no immutable review receipt", id)
		}
		if finding.ReviewState == "manager-rejected" && !graph.hasFindingDecision(finding, "reject") {
			return fmt.Errorf("manager-rejected finding %s has no immutable review receipt", id)
		}
	}
	for id, finding := range graph.Findings {
		replacements := supersededBy[id]
		if finding.Validity == "superseded" && len(replacements) == 0 {
			return fmt.Errorf("superseded finding %s requires at least one replacement that points back", id)
		}
		if finding.Validity == "superseded" && len(replacements) > 1 && !graph.hasFindingDecision(finding, "split") {
			return fmt.Errorf("superseded finding %s has multiple replacements without an exact split receipt", id)
		}
		if finding.Validity != "superseded" && len(replacements) != 0 {
			return fmt.Errorf("finding %s is superseded by %v but is not marked superseded", id, replacements)
		}
	}
	if graph.ClosurePlan != nil {
		if err := ValidateClosurePlan(*graph.ClosurePlan); err != nil {
			return fmt.Errorf("closure plan: %w", err)
		}
		if graph.ClosurePlan.CampaignID != campaignID {
			return errors.New("closure plan belongs to another campaign")
		}
	}
	if graph.ClosureJob != nil {
		if err := ValidateClosureJob(*graph.ClosureJob); err != nil {
			return fmt.Errorf("closure job: %w", err)
		}
		if graph.ClosureJob.CampaignID != campaignID {
			return errors.New("closure job belongs to another campaign")
		}
		if graph.ClosureJob.Status == "reopened" {
			if graph.Campaign.Status != "open" {
				return errors.New("reopened closure job requires an open campaign")
			}
		} else if graph.Campaign.Status != "closing" && graph.Campaign.Status != "closed" {
			return errors.New("active closure job requires a closing or closed campaign")
		}
	}
	if graph.ClosureCoverage != nil {
		if err := ValidateClosureCoverage(*graph.ClosureCoverage); err != nil {
			return fmt.Errorf("closure coverage: %w", err)
		}
		if graph.ClosureCoverage.CampaignID != campaignID {
			return errors.New("closure coverage belongs to another campaign")
		}
	}
	if graph.ClosureReceipt != nil {
		if err := ValidateClosureReceipt(*graph.ClosureReceipt); err != nil {
			return fmt.Errorf("closure receipt: %w", err)
		}
		if graph.ClosureReceipt.CampaignID != campaignID {
			return errors.New("closure receipt belongs to another campaign")
		}
		if graph.Campaign.Status != "closed" || graph.ClosureJob == nil || graph.ClosureJob.Status != "completed" {
			return errors.New("closure receipt requires a closed campaign and completed closure job")
		}
	}
	return nil
}

func reviewBindsIntake(graph CampaignGraph, intake IntakeRecord) bool {
	for _, review := range graph.Reviews {
		if review.CampaignID != intake.CampaignID || review.IntakeID != intake.ID ||
			review.IntakeRevision != intake.Revision-1 ||
			len(review.Decisions) != len(intake.CandidateFindingIDs) {
			continue
		}
		decisions := map[string]bool{}
		for _, decision := range review.Decisions {
			decisions[decision.FindingID] = true
		}
		complete := true
		for _, findingID := range intake.CandidateFindingIDs {
			complete = complete && decisions[findingID]
		}
		if complete && len(decisions) == len(intake.CandidateFindingIDs) {
			return true
		}
	}
	return false
}

func (graph CampaignGraph) validateWorkRelations(item WorkItemRecord) error {
	for _, parentID := range item.Relations.ParentIDs {
		parent, ok := graph.WorkItems[parentID]
		if !ok || !containsString(parent.Relations.ChildIDs, item.ID) {
			return fmt.Errorf("work relation %s parent %s is missing or not reciprocal", item.ID, parentID)
		}
	}
	for _, childID := range item.Relations.ChildIDs {
		child, ok := graph.WorkItems[childID]
		if !ok || !containsString(child.Relations.ParentIDs, item.ID) {
			return fmt.Errorf("work relation %s child %s is missing or not reciprocal", item.ID, childID)
		}
	}
	for _, related := range append(append(append([]string{}, item.Relations.DependsOn...), item.Relations.BlockedBy...), item.Relations.SpawnedByIDs...) {
		if _, ok := graph.WorkItems[related]; !ok {
			return fmt.Errorf("work item %s relation %s does not resolve", item.ID, related)
		}
	}
	if item.SupersededBy != "" {
		if _, ok := graph.WorkItems[item.SupersededBy]; !ok {
			return fmt.Errorf("work item %s replacement %s does not resolve", item.ID, item.SupersededBy)
		}
	}
	return nil
}

func (graph CampaignGraph) validateDependencyCycles() error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("work dependency cycle reaches %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range graph.WorkItems[id].Relations.DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range graph.WorkItems {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (graph CampaignGraph) hasFindingDecision(finding FindingRecord, action string) bool {
	for _, review := range graph.Reviews {
		if review.Authority != "manager" {
			continue
		}
		for _, decision := range review.Decisions {
			if decision.FindingID == finding.ID && decision.Action == action &&
				(decision.FindingRevision == finding.Revision || decision.FindingRevision == finding.Revision-1) {
				return true
			}
		}
	}
	return false
}
