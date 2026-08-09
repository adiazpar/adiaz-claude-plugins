package knowledge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// maxScopedConstraintBytes bounds the total accepted-constraint text that one
// canonical record may contribute to every scoped context pack compiled for
// its campaign.
//
// Campaign objective/scope/exclusions/success/closure text and work-item
// problem/acceptance text are compiled into ContextPack.AcceptedConstraints.
// Constraints are mandatory: compileContextPack drops retrieved candidate
// cards to satisfy a budget, but it can never drop an accepted constraint. A
// record whose constraint text alone cannot fit therefore makes every future
// run for that campaign impossible to prepare at any permitted budget, and the
// failure surfaces much later at context-pack compilation rather than at the
// write that caused it. Bounding the write is the only place the caller still
// knows which field it just made too large.
//
// 4096 bytes is about 1024 estimated tokens (EstimateTokens charges one token
// per four bytes), roughly 650 English words per record. A campaign plus its
// work item is therefore bounded near 2048 tokens, which leaves room inside
// the 3072-token default drafter ceiling for the pack envelope and the
// mandatory campaign and work state cards. The bound is necessary, not
// sufficient: a project that lowers its configured role budgets, or a work
// item carrying many related items and accepted constraint findings, can still
// compile a pack that does not fit. mandatoryContextBudgetError reports
// exactly what did not fit when that happens. Long-form rationale belongs in a
// finding or in the work-item description, which are retrieved and therefore
// droppable.
const maxScopedConstraintBytes = 4096

// scopedConstraintField names one record field that becomes mandatory scoped
// context, together with the bytes it contributes.
type scopedConstraintField struct {
	Name  string
	Bytes int
}

func listBytes(values []string) int {
	total := 0
	for _, value := range values {
		total += len(strings.TrimSpace(value))
	}
	return total
}

// campaignScopedConstraintFields mirrors compileContextScope: every field
// listed here is turned into a mandatory ContextConstraint for every scoped
// pack bound to this campaign.
func campaignScopedConstraintFields(record CampaignRecord) []scopedConstraintField {
	return []scopedConstraintField{
		{Name: "objective", Bytes: len(strings.TrimSpace(record.Objective))},
		{Name: "scope", Bytes: listBytes(record.Scope)},
		{Name: "exclusions", Bytes: listBytes(record.Exclusions)},
		{Name: "successCriteria", Bytes: listBytes(record.SuccessCriteria)},
		{Name: "closureCriteria", Bytes: listBytes(record.ClosureCriteria)},
	}
}

// workScopedConstraintFields mirrors compileContextScope for the primary work
// item of a delegated run.
func workScopedConstraintFields(record WorkItemRecord) []scopedConstraintField {
	return []scopedConstraintField{
		{Name: "problem", Bytes: len(strings.TrimSpace(record.Problem))},
		{Name: "acceptance", Bytes: listBytes(record.Acceptance)},
	}
}

// validateScopedConstraintBudget fails a write whose mandatory scoped context
// could never fit a permitted context-pack budget. It names the record, every
// contributing field and its size, the limit, and the remedy.
func validateScopedConstraintBudget(kind, id string, fields []scopedConstraintField) error {
	total := 0
	for _, field := range fields {
		total += field.Bytes
	}
	if total <= maxScopedConstraintBytes {
		return nil
	}
	sorted := append([]scopedConstraintField(nil), fields...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Bytes > sorted[j].Bytes })
	parts := make([]string, 0, len(sorted))
	for _, field := range sorted {
		if field.Bytes == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d bytes", field.Name, field.Bytes))
	}
	return fmt.Errorf(
		"%s %s contributes %d bytes of mandatory scoped context, over the %d-byte per-record limit (%s); "+
			"every context pack bound to this campaign must carry these fields verbatim, so this record could not "+
			"prepare a run at any permitted token budget; shorten %s or move its detail into a finding, "+
			"the work-item description, or a durable truth document",
		kind, id, total, maxScopedConstraintBytes, strings.Join(parts, ", "), sorted[0].Name)
}

// ValidateScopedContextBudget checks the submitted records outright. It is the
// unconditional form used where no prior revision exists.
func ValidateScopedContextBudget(campaign *CampaignRecord, workItems []WorkItemRecord) error {
	if campaign != nil {
		if err := validateScopedConstraintBudget(
			"campaign", campaign.ID, campaignScopedConstraintFields(*campaign),
		); err != nil {
			return err
		}
	}
	for _, item := range workItems {
		if err := validateScopedConstraintBudget(
			"work item", item.ID, workScopedConstraintFields(item),
		); err != nil {
			return err
		}
	}
	return nil
}

func campaignScopedConstraintText(record CampaignRecord) string {
	return strings.Join(append([]string{record.Objective},
		append(append(append(append([]string(nil), record.Scope...), record.Exclusions...),
			record.SuccessCriteria...), record.ClosureCriteria...)...), "\x00")
}

func workScopedConstraintText(record WorkItemRecord) string {
	return strings.Join(append([]string{record.Problem}, record.Acceptance...), "\x00")
}

// validateScopedContextBudgetDelta is the write-time guard for manager
// transitions. It refuses a record that introduces or grows mandatory scoped
// context past the limit, and deliberately ignores a record whose constraint
// text is byte-identical to the committed revision. A campaign that was
// already oversized before this guard existed must remain completable: a
// run.return or work.update that merely carries the same text forward is not
// the write that caused the problem, and stranding it would trade a late
// failure for an unrecoverable one.
func validateScopedContextBudgetDelta(graph CampaignGraph, request ManagerApplyRequest) error {
	if request.Action == "reconcile.import" {
		// reconcile.import is the recovery path for state that already exists on
		// disk. Refusing it would leave an oversized legacy record unfixable.
		return nil
	}
	if request.Campaign != nil {
		previous := ""
		if graph.Campaign != nil {
			previous = campaignScopedConstraintText(*graph.Campaign)
		}
		if campaignScopedConstraintText(*request.Campaign) != previous {
			if err := validateScopedConstraintBudget(
				"campaign", request.Campaign.ID,
				campaignScopedConstraintFields(*request.Campaign),
			); err != nil {
				return err
			}
		}
	}
	for _, item := range request.WorkItems {
		previous := ""
		if existing, present := graph.WorkItems[item.ID]; present {
			previous = workScopedConstraintText(existing)
		}
		if workScopedConstraintText(item) == previous {
			continue
		}
		if err := validateScopedConstraintBudget(
			"work item", item.ID, workScopedConstraintFields(item),
		); err != nil {
			return err
		}
	}
	return nil
}

// mandatoryContextBudgetError explains which mandatory scoped context exceeded
// a context-pack budget. The compiler reaches this point only after dropping
// every droppable candidate card, so the reported floor is exactly what the
// pack cannot shed: the envelope, the accepted constraints, and the mandatory
// state cards. Reporting only "budget is too small" forced callers to bisect
// budgets and then read the engine source to discover which record had grown.
func mandatoryContextBudgetError(
	pack ContextPack,
	serializedBytes int,
	maxBytes int,
	roleBudget int,
) error {
	type contribution struct {
		label  string
		tokens int
	}
	contributions := make([]contribution, 0, len(pack.AcceptedConstraints)+len(pack.Cards))
	constraintTokens, cardTokens := 0, 0
	for _, constraint := range pack.AcceptedConstraints {
		tokens := estimatedValueTokens(constraint)
		constraintTokens += tokens
		contributions = append(contributions, contribution{
			label: fmt.Sprintf("constraint %s from %s (%d tokens)",
				constraint.Kind, constraint.SourceHandle, tokens),
			tokens: tokens,
		})
	}
	for _, card := range pack.Cards {
		tokens := estimatedValueTokens(card)
		cardTokens += tokens
		contributions = append(contributions, contribution{
			label:  fmt.Sprintf("card %s %s (%d tokens)", card.CardType, card.Handle, tokens),
			tokens: tokens,
		})
	}
	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].tokens > contributions[j].tokens
	})
	largest := make([]string, 0, 3)
	for index := 0; index < len(contributions) && index < 3; index++ {
		largest = append(largest, contributions[index].label)
	}

	overflow := make([]string, 0, 2)
	if pack.EstimatedTokens > pack.TokenBudget {
		overflow = append(overflow, fmt.Sprintf(
			"needs %d tokens against a %d-token budget", pack.EstimatedTokens, pack.TokenBudget))
	}
	if serializedBytes > maxBytes {
		overflow = append(overflow, fmt.Sprintf(
			"needs %d bytes against a %d-byte packing cap", serializedBytes, maxBytes))
	}

	remedy := fmt.Sprintf("raise tokenBudget to at least %d", pack.EstimatedTokens)
	if pack.EstimatedTokens > roleBudget {
		remedy = fmt.Sprintf(
			"no budget can fit this pack: the %d-token floor already exceeds the %d-token %s role ceiling, "+
				"so the named records must be shortened",
			pack.EstimatedTokens, roleBudget, pack.Role)
	} else if serializedBytes > maxBytes {
		remedy = "shorten the named records: the packing byte cap is policy, not a request parameter"
	}

	return fmt.Errorf(
		"token or byte budget is too small for mandatory scoped context: "+
			"the mandatory floor (%d accepted constraints costing %d tokens, %d state cards costing %d tokens, "+
			"no droppable candidates remain) %s; largest contributors: %s; %s",
		len(pack.AcceptedConstraints), constraintTokens, len(pack.Cards), cardTokens,
		strings.Join(overflow, " and "), strings.Join(largest, "; "), remedy)
}

func estimatedValueTokens(value any) int {
	body, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return EstimateTokens(string(body))
}
