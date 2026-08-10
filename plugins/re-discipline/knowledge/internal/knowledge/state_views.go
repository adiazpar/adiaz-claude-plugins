package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type StateRequest struct {
	Mode         string `json:"mode"`
	CampaignID   string `json:"campaignId,omitempty"`
	WorkItemID   string `json:"workItemId,omitempty"`
	SinceEventID string `json:"sinceEventId,omitempty"`
	TokenBudget  int    `json:"tokenBudget,omitempty"`
	MaxCards     int    `json:"maxCards,omitempty"`
}

func (service *Service) State(ctx context.Context, request StateRequest) (StateView, error) {
	if service == nil {
		return StateView{}, errors.New("service is required")
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateView{}, err
	}
	return compileStateView(ctx, store, service.Configuration, service.Warnings, request)
}

func compileStateView(
	ctx context.Context,
	store *StateStore,
	configuration Configuration,
	warnings []string,
	request StateRequest,
) (StateView, error) {
	if err := ctx.Err(); err != nil {
		return StateView{}, err
	}
	if !validOne(request.Mode, "orient", "resume", "work", "delta", "closure") {
		return StateView{}, fmt.Errorf("unsupported state mode %q", request.Mode)
	}
	budget := request.TokenBudget
	if budget == 0 {
		budget = defaultStateBudget(request.Mode)
	}
	if budget < 128 || budget > 8192 {
		return StateView{}, errors.New("state token budget must be between 128 and 8192")
	}
	maxCards := request.MaxCards
	configuredMax := 16
	if configuration.Valid && configuration.Bootstrap.Context.MaxCards > 0 {
		configuredMax = configuration.Bootstrap.Context.MaxCards
	}
	if maxCards == 0 || maxCards > configuredMax {
		maxCards = configuredMax
	}
	if maxCards < 1 || maxCards > 50 {
		return StateView{}, errors.New("state card limit must be between 1 and 50")
	}
	head, err := store.LoadHead()
	if err != nil {
		return StateView{}, err
	}
	view := StateView{
		SchemaVersion: CampaignSchemaVersion, Mode: request.Mode,
		CampaignID: request.CampaignID, WorkItemID: request.WorkItemID,
		Generation: head.Digest, EventHead: head.EventID,
		Cards: []ContextCard{}, Omissions: []string{}, Expansions: []string{},
		Status: "ok",
	}
	asOf := store.Now().UTC()
	switch request.Mode {
	case "orient":
		err = compileOrientState(store, configuration, warnings, head, asOf, &view)
	case "resume":
		err = compileResumeState(store, request.CampaignID, asOf, &view)
	case "work":
		err = compileWorkState(store, request.CampaignID, request.WorkItemID, asOf, &view)
	case "delta":
		err = compileDeltaState(store, request.CampaignID, request.SinceEventID, &view)
	case "closure":
		err = compileClosureState(store, request.CampaignID, asOf, &view)
	}
	if err != nil {
		return StateView{}, err
	}
	if len(view.Cards) == 0 && view.Status == "ok" {
		view.Status = "insufficient"
	}
	return boundAndSealStateView(view, budget, maxCards)
}

func defaultStateBudget(mode string) int {
	switch mode {
	case "resume":
		return 1500
	case "work":
		return 2000
	case "closure":
		return 1200
	default:
		return 800
	}
}

func compileOrientState(
	store *StateStore,
	configuration Configuration,
	warnings []string,
	head StateHead,
	asOf time.Time,
	view *StateView,
) error {
	health := ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: "project-health", CardType: "health",
		Title: "Project state engine", SourceClass: "state", Handle: "state:health",
		ExpansionTokens: 200,
		Metadata: map[string]string{
			"configuration": boolStatus(configuration.Valid),
			"headRevision":  strconv.FormatInt(head.Revision, 10),
			"headDigest":    head.Digest,
		},
	}
	if !configuration.Valid {
		health.RelationAlerts = []string{"stale"}
		health.Claim = strings.Join(configuration.Errors, "; ")
		view.Status = "attention"
	} else {
		health.Claim = "Canonical state configuration and project head are readable."
	}
	if len(warnings) > 0 {
		health.Metadata["warnings"] = strings.Join(SortedUnique(warnings), "; ")
		view.Status = "attention"
	}
	view.Cards = append(view.Cards, health)
	campaigns, err := store.ListCampaigns()
	if err != nil {
		return err
	}
	for _, campaign := range campaigns {
		if validOne(campaign.Status, "closed", "cancelled") {
			continue
		}
		card := campaignStateCard(campaign)
		graph, graphErr := store.LoadCampaignGraph(campaign.ID)
		if graphErr != nil {
			card.RelationAlerts = append(card.RelationAlerts, "stale")
			card.Metadata["graph"] = "invalid: " + graphErr.Error()
			view.Status = "attention"
		} else {
			evaluations, evaluationErr := loadCampaignDefermentEvaluations(store, graph, asOf)
			if evaluationErr != nil {
				return evaluationErr
			}
			blocked, pending, due, near := 0, 0, 0, 0
			for _, item := range graph.WorkItems {
				if item.State == "blocked" || (item.State == "deferred" && item.Deferment != nil && item.Deferment.BlocksClosure) {
					blocked++
				}
			}
			for _, evaluation := range evaluations {
				switch evaluation.Status {
				case DefermentStatusDue:
					due++
				case DefermentStatusNear:
					near++
				}
			}
			for _, run := range graph.Runs {
				if run.Status == "returned" {
					pending++
				}
			}
			card.Metadata["blockers"] = strconv.Itoa(blocked)
			card.Metadata["pendingReturns"] = strconv.Itoa(pending)
			card.Metadata["dueDeferments"] = strconv.Itoa(due)
			card.Metadata["nearDeferments"] = strconv.Itoa(near)
			if blocked > 0 || pending > 0 || due > 0 || near > 0 {
				view.Status = "attention"
			}
			transitions := RecommendedDefermentTransitions(graph, evaluations)
			view.Cards = append(view.Cards, card)
			for _, id := range dueOrNearDefermentIDs(evaluations) {
				itemCard := applyDefermentEvaluation(workStateCard(graph.Campaign.Slug, graph.WorkItems[id]), evaluations[id])
				itemCard.WhyMatched = []string{"typed deferment trigger is " + evaluations[id].Status}
				view.Cards = append(view.Cards, itemCard)
				for _, transition := range recommendedTransitionsForWork(transitions, id) {
					view.Cards = append(view.Cards, defermentTransitionCard(transition))
				}
			}
			view.Expansions = append(view.Expansions, "state:resume:"+campaign.ID)
			continue
		}
		view.Cards = append(view.Cards, card)
		view.Expansions = append(view.Expansions, "state:resume:"+campaign.ID)
	}
	return nil
}

func compileResumeState(store *StateStore, campaignID string, asOf time.Time, view *StateView) error {
	if campaignID == "" {
		return errors.New("resume state requires campaignId")
	}
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		return err
	}
	view.CampaignID = graph.Campaign.ID
	view.EventHead = graph.Campaign.LastEventID
	view.Cards = append(view.Cards, campaignStateCard(*graph.Campaign))
	evaluations, err := loadCampaignDefermentEvaluations(store, graph, asOf)
	if err != nil {
		return err
	}
	transitions := RecommendedDefermentTransitions(graph, evaluations)
	priority := map[string]int{"blocked": 1, "active": 2, "ready": 3, "deferred": 5, "proposed": 6}
	items := make([]WorkItemRecord, 0, len(graph.WorkItems))
	for _, item := range graph.WorkItems {
		if _, ok := priority[item.State]; ok || containsString(graph.Campaign.CurrentFocus, item.ID) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		leftFocus := containsString(graph.Campaign.CurrentFocus, items[i].ID)
		rightFocus := containsString(graph.Campaign.CurrentFocus, items[j].ID)
		if leftFocus != rightFocus {
			return leftFocus
		}
		leftPriority, rightPriority := priority[items[i].State], priority[items[j].State]
		if evaluation, ok := evaluations[items[i].ID]; ok {
			if evaluation.Status == DefermentStatusDue {
				leftPriority = 0
			} else if evaluation.Status == DefermentStatusNear {
				leftPriority = 4
			}
		}
		if evaluation, ok := evaluations[items[j].ID]; ok {
			if evaluation.Status == DefermentStatusDue {
				rightPriority = 0
			} else if evaluation.Status == DefermentStatusNear {
				rightPriority = 4
			}
		}
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[i].ID < items[j].ID
	})
	for _, item := range items {
		card := workStateCard(graph.Campaign.Slug, item)
		if evaluation, ok := evaluations[item.ID]; ok {
			card = applyDefermentEvaluation(card, evaluation)
		}
		view.Cards = append(view.Cards, card)
		for _, transition := range recommendedTransitionsForWork(transitions, item.ID) {
			view.Cards = append(view.Cards, defermentTransitionCard(transition))
		}
		view.Expansions = append(view.Expansions, "state:work:"+item.ID)
		if item.State == "blocked" || (item.State == "deferred" && item.Deferment != nil && item.Deferment.BlocksClosure) {
			view.Status = "attention"
		}
		if evaluation, ok := evaluations[item.ID]; ok && validOne(evaluation.Status, DefermentStatusDue, DefermentStatusNear) {
			view.Status = "attention"
		}
	}
	runIDs := sortedRunIDs(graph.Runs)
	for _, id := range runIDs {
		run := graph.Runs[id]
		if run.Status != "returned" {
			continue
		}
		view.Cards = append(view.Cards, runStateCard(graph.Campaign.Slug, run))
		view.Status = "attention"
	}
	intakeIDs := sortedIntakeIDs(graph.Intakes)
	for _, id := range intakeIDs {
		intake := graph.Intakes[id]
		if !validOne(intake.Status, "draft", "submitted") {
			continue
		}
		view.Cards = append(view.Cards, ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: intake.ID, CardType: "decision",
			Title: "Curation intake awaiting review", SourceClass: "state",
			Handle:          "record:active/" + graph.Campaign.Slug + "/intake/" + intake.ID + ".json",
			ExpansionTokens: 400, Metadata: map[string]string{
				"status": intake.Status, "candidateCount": strconv.Itoa(len(intake.CandidateFindingIDs)),
				"conflictCount": strconv.Itoa(len(intake.Conflicts)),
			},
		})
		view.Status = "attention"
	}
	reviewIDs := sortedReviewIDs(graph.Reviews)
	if len(reviewIDs) > 3 {
		reviewIDs = reviewIDs[len(reviewIDs)-3:]
	}
	for _, id := range reviewIDs {
		review := graph.Reviews[id]
		view.Cards = append(view.Cards, reviewStateCard(graph.Campaign.Slug, review))
	}
	appendFindingStalenessState(graph, view)
	appendReviewLoadState(graph, view)
	return nil
}

func compileWorkState(store *StateStore, campaignID, workItemID string, asOf time.Time, view *StateView) error {
	if campaignID == "" || !workItemIDRE.MatchString(workItemID) {
		return errors.New("work state requires campaignId and a valid workItemId")
	}
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		return err
	}
	item, ok := graph.WorkItems[workItemID]
	if !ok {
		return fmt.Errorf("work item %s does not exist in campaign %s", workItemID, graph.Campaign.ID)
	}
	view.CampaignID, view.WorkItemID, view.EventHead = graph.Campaign.ID, item.ID, graph.Campaign.LastEventID
	evaluations, err := loadCampaignDefermentEvaluations(store, graph, asOf)
	if err != nil {
		return err
	}
	selectedCard := workStateCard(graph.Campaign.Slug, item)
	if evaluation, ok := evaluations[item.ID]; ok {
		selectedCard = applyDefermentEvaluation(selectedCard, evaluation)
	}
	view.Cards = append(view.Cards, selectedCard)
	for _, transition := range recommendedTransitionsForWork(
		RecommendedDefermentTransitions(graph, evaluations), item.ID) {
		view.Cards = append(view.Cards, defermentTransitionCard(transition))
	}
	relatedWork := append([]string{}, item.Relations.DependsOn...)
	relatedWork = append(relatedWork, item.Relations.BlockedBy...)
	relatedWork = append(relatedWork, item.Relations.ParentIDs...)
	relatedWork = append(relatedWork, item.Relations.ChildIDs...)
	for _, id := range SortedUnique(relatedWork) {
		if related, ok := graph.WorkItems[id]; ok {
			card := workStateCard(graph.Campaign.Slug, related)
			if evaluation, ok := evaluations[id]; ok {
				card = applyDefermentEvaluation(card, evaluation)
				if validOne(evaluation.Status, DefermentStatusDue, DefermentStatusNear) {
					view.Status = "attention"
				}
			}
			card.WhyMatched = []string{"explicit work-graph relation to " + item.ID}
			view.Cards = append(view.Cards, card)
		}
	}
	for _, id := range SortedUnique(item.FindingIDs) {
		if finding, ok := graph.Findings[id]; ok {
			card := findingStateCardInGraph(graph, finding)
			card.WhyMatched = []string{"linked from work item " + item.ID}
			view.Cards = append(view.Cards, card)
		}
	}
	runIDs := append(append([]string{}, item.ActiveRunIDs...), item.CompletedRunIDs...)
	for _, id := range SortedUnique(runIDs) {
		if run, ok := graph.Runs[id]; ok {
			view.Cards = append(view.Cards, runStateCard(graph.Campaign.Slug, run))
		}
	}
	if item.State == "blocked" || len(item.Relations.BlockedBy) > 0 {
		view.Status = "attention"
	}
	if evaluation, ok := evaluations[item.ID]; ok && validOne(evaluation.Status, DefermentStatusDue, DefermentStatusNear) {
		view.Status = "attention"
	}
	return nil
}

func compileDeltaState(store *StateStore, campaignID, sinceEventID string, view *StateView) error {
	if campaignID == "" || !eventIDRE.MatchString(sinceEventID) {
		return errors.New("delta state requires campaignId and a valid sinceEventId")
	}
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		return err
	}
	view.CampaignID, view.EventHead = graph.Campaign.ID, graph.Campaign.LastEventID
	events, found, err := readCampaignEvents(store.Boundary, graph.Campaign.Slug, sinceEventID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("event %s is outside the retained campaign journal", sinceEventID)
	}
	if len(events) == 0 {
		view.Status = "unchanged"
		return nil
	}
	for _, event := range events {
		for _, id := range event.AffectedIDs {
			card, ok := stateCardByID(graph, id)
			if !ok {
				card = ContextCard{
					SchemaVersion: CampaignSchemaVersion, ID: id, CardType: "decision",
					Title: event.Action, Claim: event.Rationale, SourceClass: "state",
					Handle: "event:" + event.ID, ExpansionTokens: 250,
				}
			}
			card.WhyMatched = []string{"changed by " + event.ID + " (" + event.Action + ")"}
			view.Cards = append(view.Cards, card)
		}
	}
	view.Cards = deduplicateStateCards(view.Cards)
	return nil
}

func compileClosureState(store *StateStore, campaignID string, asOf time.Time, view *StateView) error {
	if campaignID == "" {
		return errors.New("closure state requires campaignId")
	}
	graph, err := store.LoadCampaignGraph(campaignID)
	if err != nil {
		return err
	}
	view.CampaignID, view.EventHead = graph.Campaign.ID, graph.Campaign.LastEventID
	view.Cards = append(view.Cards, campaignStateCard(*graph.Campaign))
	evaluations, err := loadCampaignDefermentEvaluations(store, graph, asOf)
	if err != nil {
		return err
	}
	transitions := RecommendedDefermentTransitions(graph, evaluations)
	for _, id := range sortedDefermentEvaluationIDs(evaluations) {
		card := applyDefermentEvaluation(workStateCard(graph.Campaign.Slug, graph.WorkItems[id]), evaluations[id])
		card.WhyMatched = []string{"deferment has an explicit closure disposition"}
		view.Cards = append(view.Cards, card)
		for _, transition := range recommendedTransitionsForWork(transitions, id) {
			view.Cards = append(view.Cards, defermentTransitionCard(transition))
		}
		if graph.WorkItems[id].Deferment.BlocksClosure ||
			validOne(evaluations[id].Status, DefermentStatusDue, DefermentStatusNear) ||
			(graph.ClosureCoverage == nil || graph.ClosureCoverage.WorkItemCoverage[id] != "exported-backlog") {
			view.Status = "attention"
		}
	}
	if graph.ClosureJob == nil {
		view.Cards = append(view.Cards, ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: "closure-not-started", CardType: "blocker",
			Title: "Closure job has not started", SourceClass: "state",
			Handle: "closure:" + graph.Campaign.ID, ExpansionTokens: 200,
		})
		view.Status = "attention"
		return nil
	}
	job := graph.ClosureJob
	view.Cards = append(view.Cards, ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: job.ID, CardType: "decision",
		Title: "Closure " + job.Stage, Claim: "Closure job is " + job.Status + ".",
		SourceClass: "state", Handle: "record:active/" + graph.Campaign.Slug + "/closure/job.json",
		ExpansionTokens: 400, Metadata: map[string]string{
			"stage": job.Stage, "status": job.Status,
			"frozenCampaignRevision": strconv.FormatInt(job.FrozenCampaignRevision, 10),
			"projectionCount":        strconv.Itoa(len(job.ProjectionFindingIDs)),
			"attempt":                strconv.FormatInt(closureAttempt(*job), 10),
		},
	})
	if job.Status == "reopened" {
		// The refusal that stranded campaigns was reachable from closure_apply
		// action "start"; the caller that hits it reads this view next. Say the door
		// out loud rather than making it inferable from the action enum.
		view.Cards = append(view.Cards, ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: "closure-restart-available",
			CardType: "decision", Title: "Closure was reopened",
			Claim: "Re-enter closure with closure_apply action \"restart\", which re-plans against the " +
				"current campaign revision. Action \"start\" refuses while this job exists.",
			SourceClass: "state", Handle: "closure:" + graph.Campaign.ID, ExpansionTokens: 250,
		})
		view.Status = "attention"
	}
	coverage := graph.ClosureCoverage
	if coverage == nil {
		coverage = job.Coverage
	}
	if coverage != nil {
		for _, blocker := range append(append([]string{}, coverage.MissingDecisions...), coverage.UnresolvedConflicts...) {
			view.Cards = append(view.Cards, ContextCard{
				SchemaVersion: CampaignSchemaVersion, ID: StableID("closure-blocker", blocker),
				CardType: "blocker", Title: blocker, SourceClass: "state",
				Handle: "closure:" + graph.Campaign.ID, ExpansionTokens: 250,
			})
			view.Status = "attention"
		}
	}
	for _, blocker := range job.Blockers {
		view.Cards = append(view.Cards, ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: StableID("closure-blocker", blocker),
			CardType: "blocker", Title: blocker, SourceClass: "state",
			Handle: "closure:" + graph.Campaign.ID, ExpansionTokens: 250,
		})
		view.Status = "attention"
	}
	appendFindingStalenessState(graph, view)
	appendReviewLoadState(graph, view)
	return nil
}

func appendFindingStalenessState(graph CampaignGraph, view *StateView) {
	stale := StaleFindingDependents(graph.Findings)
	ids := staleDependentIDs(stale)
	if len(ids) == 0 {
		return
	}
	dependencies := 0
	for _, targets := range stale {
		dependencies += len(targets)
	}
	paths := staleFindingPathStrings(graph.Findings)
	visibleIDs, visiblePaths := ids, paths
	if len(visibleIDs) > 20 {
		visibleIDs = visibleIDs[:20]
	}
	if len(visiblePaths) > 20 {
		visiblePaths = visiblePaths[:20]
	}
	view.Cards = append(view.Cards, ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: "stale-finding-dependents", CardType: "blocker",
		Title:       "Finding dependents require re-verification",
		Claim:       "A dependency was verified after its dependent; the dependent must be re-verified before closure.",
		SourceClass: "state", Handle: "state:stale-dependents:" + graph.Campaign.ID,
		RelationAlerts: []string{"stale"}, ExpansionTokens: 260,
		Metadata: map[string]string{
			"dependentCount": strconv.Itoa(len(ids)), "dependencyCount": strconv.Itoa(dependencies),
			"findingIds": strings.Join(visibleIDs, ","), "paths": strings.Join(visiblePaths, ";"),
			"omittedFindingIds": strconv.Itoa(len(ids) - len(visibleIDs)),
			"omittedPaths":      strconv.Itoa(len(paths) - len(visiblePaths)),
		},
	})
	view.Status = "attention"
}

func appendReviewLoadState(graph CampaignGraph, view *StateView) {
	aggregate := AggregateReviewLoad(graph.Reviews)
	if aggregate.PacketCount == 0 {
		return
	}
	average := int64(0)
	if aggregate.MeasuredPackets > 0 {
		average = aggregate.TotalSeconds / int64(aggregate.MeasuredPackets)
	}
	card := ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: "review-load", CardType: "health",
		Title: "Manager review load", SourceClass: "state",
		Handle: "state:review-load:" + graph.Campaign.ID, ExpansionTokens: 220,
		Metadata: map[string]string{
			"packetCount":          strconv.Itoa(aggregate.PacketCount),
			"measuredPackets":      strconv.Itoa(aggregate.MeasuredPackets),
			"unmeasuredPackets":    strconv.Itoa(aggregate.UnmeasuredPackets),
			"sessionCount":         strconv.Itoa(aggregate.SessionCount),
			"averageSeconds":       strconv.FormatInt(average, 10),
			"maximumSeconds":       strconv.FormatInt(aggregate.MaximumSeconds, 10),
			"maximumPacketOrdinal": strconv.Itoa(aggregate.MaximumOrdinal),
			"overTargetPackets":    strconv.Itoa(aggregate.OverTargetPackets),
			"coarsenDecisions":     strconv.Itoa(aggregate.CoarsenDecisions),
		},
	}
	if aggregate.OverTargetPackets > 0 {
		card.Claim = "Measured manager review load exceeded its configured ceiling; each affected receipt records a required coarsen decision."
		visible := aggregate.OverTargetReviewIDs
		if len(visible) > 20 {
			visible = visible[:20]
		}
		card.Metadata["overTargetReviewIds"] = strings.Join(visible, ",")
		card.Metadata["omittedOverTargetReviewIds"] = strconv.Itoa(len(aggregate.OverTargetReviewIDs) - len(visible))
		view.Status = "attention"
	} else if aggregate.UnmeasuredPackets > 0 {
		card.Claim = "Legacy review evidence includes no contemporaneous load measurement; no timing was inferred."
	} else {
		card.Claim = "Measured manager review load remains within its configured ceiling."
	}
	view.Cards = append(view.Cards, card)
}

func campaignStateCard(campaign CampaignRecord) ContextCard {
	return ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: campaign.ID, CardType: "campaign",
		Title: campaign.Title, Claim: campaign.Objective, SourceClass: "state",
		Handle: "record:active/" + campaign.Slug + "/campaign.json", ExpansionTokens: 500,
		Metadata: map[string]string{
			"slug": campaign.Slug, "status": campaign.Status,
			"revision":    strconv.FormatInt(campaign.Revision, 10),
			"lastEventId": campaign.LastEventID,
			"focusCount":  strconv.Itoa(len(campaign.CurrentFocus)),
		},
	}
}

func workStateCard(slug string, item WorkItemRecord) ContextCard {
	card := ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: item.ID, CardType: "work-item",
		Title: item.Title, Claim: item.Problem, SourceClass: "state",
		Handle:          "record:active/" + slug + "/work-items/" + item.ID + ".json",
		ExpansionTokens: 500, Metadata: map[string]string{
			"state": item.State, "priority": item.Priority,
			"owner": item.Owner, "assignee": item.Assignee,
			"activeRuns": strconv.Itoa(len(item.ActiveRunIDs)),
		},
	}
	if item.ResumeNote != "" {
		card.Metadata["resumeNote"] = item.ResumeNote
	}
	if item.State == "blocked" || len(item.Relations.BlockedBy) > 0 {
		card.CardType = "blocker"
		card.RelationAlerts = []string{"conflict"}
	}
	if item.State == "deferred" && item.Deferment != nil {
		card.Metadata["revisitWhen"] = summarizeDefermentTrigger(item.Deferment.RevisitWhen)
		card.Metadata["blocksClosure"] = strconv.FormatBool(item.Deferment.BlocksClosure)
		card.Metadata["closureDisposition"] = item.Deferment.ClosureDisposition
		if item.Deferment.ClosureDestination != "" {
			card.Metadata["closureDestination"] = item.Deferment.ClosureDestination
		}
	}
	return card
}

func runStateCard(slug string, run RunRecord) ContextCard {
	return ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: run.ID, CardType: "provenance",
		Title: "Run " + run.ID, Claim: run.ResultSummary, SourceClass: "state",
		Handle:          "record:active/" + slug + "/runs/" + run.ID + "/run.json",
		ExpansionTokens: 400, Metadata: map[string]string{
			"status": run.Status, "role": run.Role, "actor": run.ActorID,
			"workItemId": run.PrimaryWorkItemID,
		},
	}
}

func reviewStateCard(slug string, review ReviewRecord) ContextCard {
	card := ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: review.ID, CardType: "decision",
		Title: "Review " + review.ID, Claim: summarizeReviewDecisions(review.Decisions),
		SourceClass: "state", Handle: "record:active/" + slug + "/reviews/" + review.ID + ".json",
		ExpansionTokens: 350, Metadata: map[string]string{
			"authority": review.Authority, "reviewer": review.Reviewer,
			"intakeId": review.IntakeID, "decisionCount": strconv.Itoa(len(review.Decisions)),
		},
	}
	if ValidateReviewLoadReceipt(review.ReviewLoad) == nil {
		card.Metadata["measurementStatus"] = review.ReviewLoad.MeasurementStatus
		card.Metadata["packetOrdinal"] = strconv.Itoa(review.ReviewLoad.PacketOrdinal)
		card.Metadata["granularityDecision"] = review.ReviewLoad.GranularityDecision
		if review.ReviewLoad.OverTargetKnown {
			card.Metadata["reviewSeconds"] = strconv.FormatInt(review.ReviewLoad.DurationSeconds, 10)
			card.Metadata["overTarget"] = strconv.FormatBool(review.ReviewLoad.OverTarget)
		}
	}
	return card
}

func findingStateCard(finding FindingRecord) ContextCard {
	return ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: finding.ID, CardType: "finding",
		Claim: finding.Claim, Subject: finding.Subject, Scope: finding.Scope,
		EvidenceGrade: finding.EvidenceGrade, ReviewState: finding.ReviewState,
		Validity: finding.Validity, SourceClass: FindingSourceClass(finding),
		RelationAlerts: relationAlerts(finding), Handle: FindingHandle(finding.ID),
		EvidenceHandle: strongestFindingEvidence(finding), ExpansionTokens: EstimateTokens(finding.Body) + 200,
	}
}

func findingStateCardInGraph(graph CampaignGraph, finding FindingRecord) ContextCard {
	card := findingStateCard(finding)
	paths := FindingStalenessPaths(graph.Findings)
	card.RelationAlerts = append(card.RelationAlerts, findingStalenessRelationAlerts(paths, finding.ID)...)
	card.RelationAlerts = SortedUnique(card.RelationAlerts)
	return card
}

func strongestFindingEvidence(finding FindingRecord) string {
	if len(finding.Evidence) == 0 {
		return ""
	}
	return EvidenceHandle(finding.ID, finding.Evidence[0])
}

func summarizeReviewDecisions(decisions []ReviewDecision) string {
	parts := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		parts = append(parts, decision.Action+" "+decision.FindingID)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func loadCampaignDefermentEvaluations(
	store *StateStore,
	graph CampaignGraph,
	asOf time.Time,
) (map[string]DefermentEvaluation, error) {
	events := []StateEvent{}
	pending := defermentEventTriggerKeys(graph)
	if len(pending) != 0 {
		var err error
		events, err = readMatchingDefermentEvents(store.Boundary, graph.Campaign.Slug, pending)
		if err != nil {
			return nil, err
		}
	}
	return EvaluateDeferments(graph, asOf, events)
}

func readMatchingDefermentEvents(
	boundary Boundary,
	slug string,
	pending map[string]bool,
) ([]StateEvent, error) {
	relative := "active/" + slug + "/events/events.jsonl"
	absolute, err := boundary.Resolve(relative, true)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := []StateEvent{}
	seen := map[string]bool{}
	var previousRevision int64 = -1
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event StateEvent
		if err := decodeStrictJSON(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		if err := verifyJournalEvent(event); err != nil {
			return nil, err
		}
		if seen[event.ID] || (previousRevision >= 0 && event.PreviousRevision <= previousRevision) {
			return nil, errors.New("campaign event journal order is broken")
		}
		seen[event.ID] = true
		previousRevision = event.PreviousRevision
		if consumeMatchingDefermentEvent(event, pending) {
			result = append(result, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func sortedDefermentEvaluationIDs(evaluations map[string]DefermentEvaluation) []string {
	ids := make([]string, 0, len(evaluations))
	for id := range evaluations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func dueOrNearDefermentIDs(evaluations map[string]DefermentEvaluation) []string {
	ids := []string{}
	for id, evaluation := range evaluations {
		if validOne(evaluation.Status, DefermentStatusDue, DefermentStatusNear) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := evaluations[ids[i]], evaluations[ids[j]]
		if left.Status != right.Status {
			return left.Status == DefermentStatusDue
		}
		return ids[i] < ids[j]
	})
	return ids
}

func recommendedTransitionsForWork(
	transitions []RecommendedTransition,
	workItemID string,
) []RecommendedTransition {
	result := []RecommendedTransition{}
	for _, transition := range transitions {
		if transition.WorkItemID == workItemID {
			result = append(result, transition)
		}
	}
	return result
}

func stateCardByID(graph CampaignGraph, id string) (ContextCard, bool) {
	if id == graph.Campaign.ID {
		return campaignStateCard(*graph.Campaign), true
	}
	if item, ok := graph.WorkItems[id]; ok {
		return workStateCard(graph.Campaign.Slug, item), true
	}
	if run, ok := graph.Runs[id]; ok {
		return runStateCard(graph.Campaign.Slug, run), true
	}
	if finding, ok := graph.Findings[id]; ok {
		return findingStateCardInGraph(graph, finding), true
	}
	if review, ok := graph.Reviews[id]; ok {
		return reviewStateCard(graph.Campaign.Slug, review), true
	}
	return ContextCard{}, false
}

func readCampaignEvents(boundary Boundary, slug, since string) ([]StateEvent, bool, error) {
	relative := "active/" + slug + "/events/events.jsonl"
	absolute, err := boundary.Resolve(relative, true)
	if err != nil {
		return nil, false, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	result := []StateEvent{}
	found := since == ""
	seen := map[string]bool{}
	var previousRevision int64 = -1
	for scanner.Scan() {
		var event StateEvent
		if err := decodeStrictJSON(scanner.Bytes(), &event); err != nil {
			return nil, false, err
		}
		if err := verifyJournalEvent(event); err != nil {
			return nil, false, err
		}
		// PreviousEventID is the project-wide chain, whose predecessor may be
		// in another campaign journal. A campaign-local journal therefore
		// verifies uniqueness and strictly increasing global revisions rather
		// than incorrectly requiring local adjacency.
		if seen[event.ID] || (previousRevision >= 0 && event.PreviousRevision <= previousRevision) {
			return nil, false, errors.New("campaign event journal order is broken")
		}
		seen[event.ID] = true
		previousRevision = event.PreviousRevision
		if found {
			result = append(result, event)
		}
		if event.ID == since {
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	return result, found, nil
}

func verifyJournalEvent(event StateEvent) error {
	if err := ValidateEvent(event); err != nil {
		return err
	}
	want := event.Digest
	event.Digest = ""
	digest, err := CanonicalDigest(event)
	if err != nil || digest != want {
		return errors.New("event digest does not verify")
	}
	return nil
}

func boundAndSealStateView(view StateView, budget, maxCards int) (StateView, error) {
	for _, card := range view.Cards {
		if err := ValidateContextCard(card); err != nil {
			return StateView{}, err
		}
	}
	view.Cards = deduplicateStateCards(view.Cards)
	view.Expansions = SortedUnique(view.Expansions)
	view.Omissions = SortedUnique(view.Omissions)
	omitted := 0
	if len(view.Cards) > maxCards {
		omitted = len(view.Cards) - maxCards
		view.Cards = view.Cards[:maxCards]
	}
	for {
		view.Digest = ""
		view.TokenCost = 0
		body, err := json.Marshal(view)
		if err != nil {
			return StateView{}, err
		}
		cost := EstimateTokens(string(body))
		view.TokenCost = cost
		if cost <= budget {
			break
		}
		if len(view.Cards) == 0 {
			return StateView{}, errors.New("state token budget is too small for mandatory metadata")
		}
		view.Cards = view.Cards[:len(view.Cards)-1]
		omitted++
	}
	if omitted > 0 {
		view.Omissions = append(view.Omissions, fmt.Sprintf("%d lower-priority cards omitted by limit or token budget", omitted))
		view.Omissions = SortedUnique(view.Omissions)
	}
	for iteration := 0; iteration < 4; iteration++ {
		view.Digest = ""
		digest, err := CanonicalDigest(view)
		if err != nil {
			return StateView{}, err
		}
		view.Digest = digest
		body, err := json.Marshal(view)
		if err != nil {
			return StateView{}, err
		}
		cost := EstimateTokens(string(body))
		if cost == view.TokenCost {
			return view, nil
		}
		view.TokenCost = cost
	}
	if view.TokenCost > budget {
		return StateView{}, errors.New("state response exceeded its token budget after sealing")
	}
	return view, nil
}

func deduplicateStateCards(cards []ContextCard) []ContextCard {
	result := make([]ContextCard, 0, len(cards))
	seen := map[string]bool{}
	for _, card := range cards {
		key := card.CardType + "\x00" + card.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, card)
	}
	return result
}

func sortedRunIDs(values map[string]RunRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func sortedIntakeIDs(values map[string]IntakeRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func sortedReviewIDs(values map[string]ReviewRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func boolStatus(value bool) string {
	if value {
		return "valid"
	}
	return "invalid"
}
