package knowledge

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	DefermentTriggerDate          = "date"
	DefermentTriggerWorkItemState = "work-item-state"
	DefermentTriggerEvent         = "event"

	DefermentDispositionResolve       = "resolve-before-closure"
	DefermentDispositionExportBacklog = "export-backlog"

	DefermentStatusWaiting = "waiting"
	DefermentStatusNear    = "near"
	DefermentStatusDue     = "due"

	defermentNearWindow = 7 * 24 * time.Hour
)

// DefermentEvaluation is a deterministic projection of a typed deferment
// against one canonical graph, event journal, and instant. It is derived
// state: the contract, work graph, and immutable events remain authoritative.
type DefermentEvaluation struct {
	WorkItemID    string
	Status        string
	TriggerType   string
	Trigger       string
	DueAt         string
	MatchedEvent  string
	Destination   string
	BlocksClosure bool
}

type RecommendedTransition struct {
	WorkItemID  string
	Action      string
	TargetState string
	Destination string
	Reason      string
}

func ValidateDefermentContract(contract DefermentContract) error {
	if strings.TrimSpace(contract.Reason) == "" || strings.TrimSpace(contract.Owner) == "" {
		return errors.New("reason and owner are required")
	}
	if err := validateDefermentTrigger(contract.RevisitWhen); err != nil {
		return fmt.Errorf("revisitWhen: %w", err)
	}
	switch {
	case contract.BlocksClosure:
		if contract.ClosureDisposition != DefermentDispositionResolve || contract.ClosureDestination != "" {
			return errors.New("closure-blocking deferment must resolve before closure and cannot name a destination")
		}
	case contract.ClosureDisposition != DefermentDispositionExportBacklog:
		return errors.New("non-blocking deferment must use export-backlog disposition")
	default:
		if err := validateDefermentBacklogDestination(contract.ClosureDestination); err != nil {
			return err
		}
	}
	return nil
}

func validateDefermentTrigger(trigger DefermentTrigger) error {
	switch trigger.Type {
	case DefermentTriggerDate:
		if trigger.At == "" || trigger.WorkItemID != "" || trigger.State != "" ||
			trigger.Action != "" || trigger.AffectedID != "" {
			return errors.New("date trigger requires only at")
		}
		if err := validateUTC(trigger.At); err != nil {
			return err
		}
	case DefermentTriggerWorkItemState:
		if !workItemIDRE.MatchString(trigger.WorkItemID) ||
			!validOne(trigger.State, "proposed", "ready", "active", "blocked", "deferred", "done", "cancelled", "superseded") ||
			trigger.At != "" || trigger.Action != "" || trigger.AffectedID != "" {
			return errors.New("work-item-state trigger requires only a valid workItemId and state")
		}
	case DefermentTriggerEvent:
		if !actionIDRE.MatchString(trigger.Action) || trigger.At != "" ||
			trigger.WorkItemID != "" || trigger.State != "" {
			return errors.New("event trigger requires an action and optional affectedId")
		}
		if strings.TrimSpace(trigger.AffectedID) != trigger.AffectedID {
			return errors.New("event affectedId cannot contain surrounding whitespace")
		}
	default:
		return fmt.Errorf("unsupported trigger type %q", trigger.Type)
	}
	return nil
}

func validateDefermentBacklogDestination(destination string) error {
	if err := validateRelativeRecordPath(destination); err != nil ||
		!strings.HasPrefix(destination, "docs/backlog/") || path.Ext(destination) != ".md" ||
		validClosureNavigationPath(destination) {
		return errors.New("export-backlog deferment requires a Markdown destination below docs/backlog/")
	}
	return nil
}

func validateExportedDefermentIDs(graph CampaignGraph, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !workItemIDRE.MatchString(id) {
			return fmt.Errorf("exported work item id %q is invalid or duplicated", id)
		}
		seen[id] = true
		item, ok := graph.WorkItems[id]
		if !ok || item.State != "deferred" || item.Deferment == nil {
			return fmt.Errorf("exported work item %s is not a deferred campaign item", id)
		}
		contract := item.Deferment
		if contract.BlocksClosure || contract.ClosureDisposition != DefermentDispositionExportBacklog {
			return fmt.Errorf("exported work item %s does not permit closure backlog export", id)
		}
		if err := validateDefermentBacklogDestination(contract.ClosureDestination); err != nil {
			return fmt.Errorf("exported work item %s: %w", id, err)
		}
	}
	return nil
}

func EvaluateDeferments(
	graph CampaignGraph,
	asOf time.Time,
	events []StateEvent,
) (map[string]DefermentEvaluation, error) {
	if asOf.Location() != time.UTC {
		return nil, errors.New("deferment evaluation instant must be UTC")
	}
	result := map[string]DefermentEvaluation{}
	ids := make([]string, 0, len(graph.WorkItems))
	for id := range graph.WorkItems {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := graph.WorkItems[id]
		if item.State != "deferred" || item.Deferment == nil {
			continue
		}
		if err := ValidateDefermentContract(*item.Deferment); err != nil {
			return nil, fmt.Errorf("work item %s: %w", id, err)
		}
		contract := item.Deferment
		evaluation := DefermentEvaluation{
			WorkItemID: id, Status: DefermentStatusWaiting,
			TriggerType:   contract.RevisitWhen.Type,
			Trigger:       summarizeDefermentTrigger(contract.RevisitWhen),
			Destination:   contract.ClosureDestination,
			BlocksClosure: contract.BlocksClosure,
		}
		switch trigger := contract.RevisitWhen; trigger.Type {
		case DefermentTriggerDate:
			due, _ := time.Parse(time.RFC3339Nano, trigger.At)
			evaluation.DueAt = trigger.At
			if !asOf.Before(due) {
				evaluation.Status = DefermentStatusDue
			} else if due.Sub(asOf) <= defermentNearWindow {
				evaluation.Status = DefermentStatusNear
			}
		case DefermentTriggerWorkItemState:
			if target, ok := graph.WorkItems[trigger.WorkItemID]; !ok {
				return nil, fmt.Errorf("work item %s deferment target %s does not resolve", id, trigger.WorkItemID)
			} else if target.State == trigger.State {
				evaluation.Status = DefermentStatusDue
			}
		case DefermentTriggerEvent:
			for _, event := range events {
				if event.Action != trigger.Action ||
					(trigger.AffectedID != "" && !containsString(event.AffectedIDs, trigger.AffectedID)) {
					continue
				}
				evaluation.Status, evaluation.MatchedEvent = DefermentStatusDue, event.ID
				break
			}
		}
		result[id] = evaluation
	}
	return result, nil
}

func summarizeDefermentTrigger(trigger DefermentTrigger) string {
	switch trigger.Type {
	case DefermentTriggerDate:
		return "at " + trigger.At
	case DefermentTriggerWorkItemState:
		return trigger.WorkItemID + " reaches " + trigger.State
	case DefermentTriggerEvent:
		if trigger.AffectedID != "" {
			return "event " + trigger.Action + " affects " + trigger.AffectedID
		}
		return "event " + trigger.Action
	default:
		return "invalid trigger"
	}
}

func defermentEventTriggerKeys(graph CampaignGraph) map[string]bool {
	result := map[string]bool{}
	for _, item := range graph.WorkItems {
		if item.State != "deferred" || item.Deferment == nil ||
			item.Deferment.RevisitWhen.Type != DefermentTriggerEvent {
			continue
		}
		trigger := item.Deferment.RevisitWhen
		result[trigger.Action+"\x00"+trigger.AffectedID] = true
	}
	return result
}

// consumeMatchingDefermentEvent removes every trigger key satisfied by event
// and reports whether this is the first retained event for at least one key.
// Callers can validate an unbounded journal while retaining at most one event
// per distinct deferment trigger.
func consumeMatchingDefermentEvent(event StateEvent, pending map[string]bool) bool {
	matched := false
	generic := event.Action + "\x00"
	if pending[generic] {
		delete(pending, generic)
		matched = true
	}
	for _, affectedID := range event.AffectedIDs {
		key := event.Action + "\x00" + affectedID
		if pending[key] {
			delete(pending, key)
			matched = true
		}
	}
	return matched
}

func RecommendedDefermentTransitions(
	graph CampaignGraph,
	evaluations map[string]DefermentEvaluation,
) []RecommendedTransition {
	result := []RecommendedTransition{}
	ids := make([]string, 0, len(evaluations))
	for id := range evaluations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		evaluation := evaluations[id]
		item := graph.WorkItems[id]
		if evaluation.Status == DefermentStatusDue {
			result = append(result, RecommendedTransition{
				WorkItemID: id, Action: "work.update", TargetState: "ready",
				Reason: "the typed revisit trigger is due",
			})
			continue
		}
		if graph.Campaign != nil && graph.Campaign.Status == "closing" && item.Deferment != nil &&
			!item.Deferment.BlocksClosure && item.Deferment.ClosureDisposition == DefermentDispositionExportBacklog &&
			(graph.ClosureCoverage == nil || graph.ClosureCoverage.WorkItemCoverage[id] != "exported-backlog") {
			result = append(result, RecommendedTransition{
				WorkItemID: id, Action: "closure.export-backlog",
				Destination: item.Deferment.ClosureDestination,
				Reason:      "the non-blocking deferment must be exported before closure",
			})
		}
	}
	return result
}

func defermentTransitionCard(transition RecommendedTransition) ContextCard {
	id := StableID("transition", transition.WorkItemID, transition.Action,
		transition.TargetState, transition.Destination)
	metadata := map[string]string{
		"action": transition.Action, "workItemId": transition.WorkItemID,
	}
	if transition.TargetState != "" {
		metadata["targetState"] = transition.TargetState
	}
	if transition.Destination != "" {
		metadata["destination"] = transition.Destination
	}
	return ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: id, CardType: "decision",
		Title: "Recommended transition for " + transition.WorkItemID,
		Claim: transition.Reason, SourceClass: "state",
		Handle: "state:transition:" + id, ExpansionTokens: 180,
		Metadata: metadata,
	}
}

func applyDefermentEvaluation(card ContextCard, evaluation DefermentEvaluation) ContextCard {
	if card.Metadata == nil {
		card.Metadata = map[string]string{}
	}
	card.Metadata["defermentStatus"] = evaluation.Status
	card.Metadata["defermentTrigger"] = evaluation.Trigger
	if evaluation.DueAt != "" {
		card.Metadata["defermentDueAt"] = evaluation.DueAt
	}
	if evaluation.MatchedEvent != "" {
		card.Metadata["defermentMatchedEventId"] = evaluation.MatchedEvent
	}
	return card
}
