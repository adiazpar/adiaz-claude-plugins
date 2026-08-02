package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ContextPackRequest is the single compiler contract used by orientation,
// dispatch preview, MCP, and the CLI. Target.Kind defaults to project only for
// in-process orientation and measurement callers; the public materialization
// operation requires an explicit run target.
type ContextPackRequest struct {
	Target        ContextPackTarget `json:"target"`
	Task          string            `json:"task"`
	Role          string            `json:"role"`
	Tiers         []string          `json:"tiers,omitempty"`
	TokenBudget   int               `json:"tokenBudget,omitempty"`
	RequiredPaths []string          `json:"requiredPaths,omitempty"`
}

type compiledContextScope struct {
	scope           ContextPackScope
	constraints     []ContextConstraint
	mandatoryCards  []ContextCard
	requiredHandles []string
	campaign        *CampaignGraph
}

func (service *Service) Orient(ctx context.Context, role string, tokenBudget int) (ContextPack, error) {
	if role == "" {
		role = "manager"
	}
	return service.ContextPackOptions(ctx, ContextPackRequest{
		Target: ContextPackTarget{Kind: "project"},
		Task:   "project profile current campaign orientation",
		Role:   role, TokenBudget: tokenBudget,
	})
}

func (service *Service) ContextPack(
	ctx context.Context,
	task string,
	role string,
	tiers []string,
	tokenBudget int,
) (ContextPack, error) {
	return service.ContextPackRequired(ctx, task, role, tiers, tokenBudget, nil)
}

func (service *Service) ContextPackRequired(
	ctx context.Context,
	task string,
	role string,
	tiers []string,
	tokenBudget int,
	requiredPaths []string,
) (ContextPack, error) {
	return service.ContextPackOptions(ctx, ContextPackRequest{
		Target: ContextPackTarget{Kind: "project"},
		Task:   task, Role: role, Tiers: tiers, TokenBudget: tokenBudget,
		RequiredPaths: requiredPaths,
	})
}

func (service *Service) ContextPackOptions(
	ctx context.Context,
	request ContextPackRequest,
) (pack ContextPack, err error) {
	if service == nil {
		return ContextPack{}, errors.New("service is required")
	}
	if request.Target.Kind == "" {
		request.Target.Kind = "project"
	}
	started := time.Now()
	defer func() {
		omissions := 0
		for _, count := range pack.Omitted {
			omissions += count
		}
		service.recordTelemetry(telemetryObservation{
			Operation: "context-pack", EffectiveProfile: pack.EffectiveProfile,
			Failed: err != nil, Latency: time.Since(started),
			EstimatedTokens: pack.EstimatedTokens, Results: len(pack.Cards),
			Omissions: omissions,
		})
	}()
	pack, err = service.compileContextPack(ctx, request)
	if errors.Is(err, errSourceChangedAfterIndex) && service.pinnedGeneration == nil {
		if _, _, _, reconcileErr := service.Index.Reconcile(ctx); reconcileErr != nil {
			return ContextPack{}, fmt.Errorf(
				"context-pack source reconciliation failed: %w", reconcileErr)
		}
		return service.compileContextPack(ctx, request)
	}
	return pack, err
}

func (service *Service) compileContextPack(
	ctx context.Context,
	request ContextPackRequest,
) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if request.Role != "manager" && request.Role != "drafter" {
		return ContextPack{}, errors.New("context pack role must be manager or drafter")
	}
	request.Task = strings.TrimSpace(request.Task)
	if len([]byte(request.Task)) < 1 || len([]byte(request.Task)) > 2000 {
		return ContextPack{}, errors.New("context pack task must contain 1 to 2000 UTF-8 bytes")
	}
	tiers, err := normalizeContextPackTiers(request.Target.Kind, request.Role, request.Tiers)
	if err != nil {
		return ContextPack{}, err
	}
	settings := service.effectiveSettings()
	roleBudget := settings.Budgets.ManagerContextTokens
	if request.Role == "drafter" {
		roleBudget = settings.Budgets.DrafterContextTokens
	}
	if request.TokenBudget == 0 {
		request.TokenBudget = roleBudget
	} else if request.TokenBudget < 512 {
		return ContextPack{}, errors.New("context pack token budget must be at least 512")
	} else if request.TokenBudget > roleBudget {
		request.TokenBudget = roleBudget
	}

	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return ContextPack{}, err
	}
	state, err := service.compileContextScope(request.Target)
	if err != nil {
		return ContextPack{}, err
	}

	maxCards := service.Configuration.Bootstrap.Context.MaxCards
	if maxCards < 1 {
		maxCards = selected.Effective.Packing.MaxPassages
	}
	if maxCards < 1 {
		maxCards = 16
	}
	if len(state.mandatoryCards) > maxCards {
		return ContextPack{}, fmt.Errorf(
			"%d mandatory state cards exceed the context-pack card cap of %d",
			len(state.mandatoryCards), maxCards)
	}

	retriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
		ArchiveTracker: service.archiveTracker,
	}
	requiredCards, requiredHandles, err := compileRequiredContextCards(
		ctx, retriever, generation, request.Task, tiers, request.RequiredPaths,
	)
	if err != nil {
		return ContextPack{}, err
	}
	mandatoryCards := append([]ContextCard(nil), state.mandatoryCards...)
	mandatoryCards = appendUniqueContextCards(mandatoryCards, requiredCards...)
	if len(mandatoryCards) > maxCards {
		return ContextPack{}, fmt.Errorf(
			"mandatory state and required-path cards exceed the context-pack card cap of %d",
			maxCards)
	}

	candidates, candidateOmissions, err := service.compileContextCandidates(
		ctx, retriever, generation, selected, request, tiers, state.campaign, maxCards,
	)
	if err != nil {
		return ContextPack{}, err
	}
	cards := append([]ContextCard(nil), mandatoryCards...)
	for _, candidate := range candidates {
		if len(cards) >= maxCards {
			candidateOmissions["cardLimit"]++
			continue
		}
		cards = appendUniqueContextCards(cards, candidate)
	}

	models := make([]string, 0, len(selected.Models))
	for _, model := range selected.Models {
		models = append(models, model.ID+"@"+model.Revision)
	}
	pack := ContextPack{
		SchemaVersion: CampaignSchemaVersion,
		Task:          request.Task, Scope: state.scope,
		Generation: CompactContextGeneration(generation), Role: request.Role,
		AllowedTiers: tiers, RequestedProfile: selected.RequestedIdentity,
		EffectiveProfile: selected.EffectiveIdentity,
		ActiveLanes:      append([]string(nil), selected.ActiveLanes...),
		Models:           models, FallbackReason: selected.FallbackReason,
		TokenBudget:         request.TokenBudget,
		AcceptedConstraints: state.constraints,
		Cards:               cards,
		RequiredHandles: SortedUnique(append(
			append([]string(nil), state.requiredHandles...), requiredHandles...)),
		Omitted: map[string]int{
			"candidateCards": candidateOmissions["candidateCards"],
			"budget":         candidateOmissions["budget"],
			"cardLimit":      candidateOmissions["cardLimit"],
			"staleSource":    candidateOmissions["staleSource"],
		},
	}
	mandatoryCount := len(mandatoryCards)
	for {
		finalized, finalizeErr := finalizeContextPack(pack)
		if finalizeErr != nil {
			return ContextPack{}, finalizeErr
		}
		pack = finalized
		body, marshalErr := json.Marshal(pack)
		if marshalErr != nil {
			return ContextPack{}, marshalErr
		}
		if pack.EstimatedTokens <= pack.TokenBudget &&
			len(body) <= selected.Effective.Packing.MaxBytes {
			if semanticErr := validateContextPackSemantics(pack); semanticErr != nil {
				return ContextPack{}, semanticErr
			}
			return pack, nil
		}
		if len(pack.Cards) <= mandatoryCount {
			return ContextPack{}, errors.New(
				"token or byte budget is too small for mandatory scoped context")
		}
		pack.Cards = pack.Cards[:len(pack.Cards)-1]
		pack.Omitted["budget"]++
		pack.PackID, pack.Digest, pack.EstimatedTokens = "", "", 0
	}
}

func normalizeContextPackTiers(kind, role string, requested []string) ([]string, error) {
	if len(requested) == 0 {
		if kind == "active-run" && role == "drafter" {
			requested = []string{"profile", "navigation", "truth", "campaign", "memory"}
		} else if role == "drafter" {
			requested = []string{"profile", "navigation", "truth", "memory"}
		} else {
			requested = []string{
				"profile", "navigation", "truth", "history", "backlog",
				"active", "campaign", "memory",
			}
		}
	}
	return ValidateTierList(requested)
}

func (service *Service) compileContextScope(target ContextPackTarget) (compiledContextScope, error) {
	store := NewStateStoreWithBoundary(service.Boundary)
	head, err := store.LoadHead()
	if err != nil {
		return compiledContextScope{}, err
	}
	base := compiledContextScope{scope: ContextPackScope{
		Kind: target.Kind, StateHeadRevision: head.Revision,
		StateHeadDigest: head.Digest, EventID: head.EventID,
	}}
	switch target.Kind {
	case "project":
		if target.CampaignID != "" || target.WorkItemID != "" || target.RunID != "" ||
			target.CandidateSlug != "" || target.RecruitingRunID != "" {
			return compiledContextScope{}, errors.New("project context target cannot carry run bindings")
		}
		return base, nil
	case "recruiting-run":
		if !managedSlugRE.MatchString(target.CandidateSlug) ||
			!workspaceIDRE.MatchString(target.RecruitingRunID) ||
			target.CampaignID != "" || target.WorkItemID != "" || target.RunID != "" {
			return compiledContextScope{}, errors.New(
				"recruiting context target requires only candidateSlug and recruitingRunId")
		}
		base.scope.CandidateSlug = target.CandidateSlug
		base.scope.RecruitingRunID = target.RecruitingRunID
		return base, nil
	case "active-run":
		if !campaignIDRE.MatchString(target.CampaignID) ||
			!workItemIDRE.MatchString(target.WorkItemID) ||
			!runIDRE.MatchString(target.RunID) || target.CandidateSlug != "" ||
			target.RecruitingRunID != "" {
			return compiledContextScope{}, errors.New(
				"active context target requires campaignId, workItemId, and runId")
		}
	default:
		return compiledContextScope{}, fmt.Errorf("unsupported context target kind %q", target.Kind)
	}

	graph, err := store.LoadCampaignGraph(target.CampaignID)
	if err != nil {
		return compiledContextScope{}, fmt.Errorf("load context campaign: %w", err)
	}
	item, ok := graph.WorkItems[target.WorkItemID]
	if !ok {
		return compiledContextScope{}, fmt.Errorf("work item %s does not resolve", target.WorkItemID)
	}
	if validOne(item.State, "done", "superseded") {
		return compiledContextScope{}, fmt.Errorf("work item %s is not dispatchable", item.ID)
	}
	base.scope.CampaignID = graph.Campaign.ID
	base.scope.CampaignSlug = graph.Campaign.Slug
	base.scope.CampaignRevision = graph.Campaign.Revision
	base.scope.WorkItemID = item.ID
	base.scope.WorkItemRevision = item.Revision
	base.scope.RunID = target.RunID
	if run, exists := graph.Runs[target.RunID]; exists {
		if run.PrimaryWorkItemID != item.ID {
			return compiledContextScope{}, errors.New("context run belongs to another work item")
		}
		base.scope.RunRevision = run.Revision
	}
	base.campaign = &graph

	campaignHandle := "record:active/" + graph.Campaign.Slug + "/campaign.json"
	workHandle := "record:active/" + graph.Campaign.Slug + "/work-items/" + item.ID + ".json"
	base.mandatoryCards = append(base.mandatoryCards,
		campaignStateCard(*graph.Campaign), workStateCard(graph.Campaign.Slug, item))
	base.requiredHandles = append(base.requiredHandles, campaignHandle, workHandle)
	base.constraints = append(base.constraints,
		contextConstraint("objective", graph.Campaign.Objective, campaignHandle),
		contextConstraint("problem", item.Problem, workHandle))
	base.constraints = appendConstraintList(base.constraints, "scope", graph.Campaign.Scope, campaignHandle)
	base.constraints = appendConstraintList(base.constraints, "exclusion", graph.Campaign.Exclusions, campaignHandle)
	base.constraints = appendConstraintList(base.constraints, "success", graph.Campaign.SuccessCriteria, campaignHandle)
	base.constraints = appendConstraintList(base.constraints, "closure", graph.Campaign.ClosureCriteria, campaignHandle)
	base.constraints = appendConstraintList(base.constraints, "acceptance", item.Acceptance, workHandle)

	relatedIDs := append([]string{}, item.Relations.ParentIDs...)
	relatedIDs = append(relatedIDs, item.Relations.DependsOn...)
	relatedIDs = append(relatedIDs, item.Relations.BlockedBy...)
	relatedIDs = append(relatedIDs, item.Relations.SpawnedByIDs...)
	for _, id := range SortedUnique(relatedIDs) {
		related, exists := graph.WorkItems[id]
		if !exists {
			return compiledContextScope{}, fmt.Errorf("related work item %s does not resolve", id)
		}
		base.mandatoryCards = appendUniqueContextCards(
			base.mandatoryCards, workStateCard(graph.Campaign.Slug, related))
		base.requiredHandles = append(base.requiredHandles,
			"record:active/"+graph.Campaign.Slug+"/work-items/"+id+".json")
	}

	for _, findingID := range SortedUnique(item.FindingIDs) {
		finding, exists := graph.Findings[findingID]
		if !exists {
			return compiledContextScope{}, fmt.Errorf("linked finding %s does not resolve", findingID)
		}
		base.requiredHandles = append(base.requiredHandles, FindingHandle(finding.ID))
		if acceptedConstraintFinding(finding, item.ID) {
			base.constraints = append(base.constraints,
				contextConstraint("finding", finding.Claim, FindingHandle(finding.ID)))
			base.mandatoryCards = appendUniqueContextCards(
				base.mandatoryCards, findingStateCard(finding))
		}
	}
	base.requiredHandles = SortedUnique(base.requiredHandles)
	return base, nil
}

func contextConstraint(kind, text, handle string) ContextConstraint {
	text = strings.TrimSpace(text)
	return ContextConstraint{
		ID: StableID("constraint", kind, handle, text), Kind: kind,
		Text: text, SourceHandle: handle,
	}
}

func appendConstraintList(
	constraints []ContextConstraint,
	kind string,
	values []string,
	handle string,
) []ContextConstraint {
	for _, value := range values {
		constraints = append(constraints, contextConstraint(kind, value, handle))
	}
	return constraints
}

func acceptedConstraintFinding(finding FindingRecord, workItemID string) bool {
	if finding.Kind != "constraint" || finding.ReviewState != "manager-ratified" ||
		validOne(finding.Validity, "invalid", "superseded", "challenged") {
		return false
	}
	if value, ok := finding.Scope["workItemId"].(string); ok && value == workItemID {
		return true
	}
	if value, ok := finding.Scope["campaignWide"].(bool); ok && value {
		return true
	}
	return true // The caller already established an explicit WorkItem.FindingIDs link.
}

func compileRequiredContextCards(
	ctx context.Context,
	retriever Retriever,
	generation Generation,
	task string,
	tiers []string,
	requiredPaths []string,
) ([]ContextCard, []string, error) {
	cleanPaths := make([]string, 0, len(requiredPaths))
	for _, requiredPath := range SortedUnique(requiredPaths) {
		normalized := strings.ReplaceAll(strings.TrimSpace(requiredPath), "\\", "/")
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(normalized)))
		if normalized == "" || normalized != clean || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) ||
			IsForbiddenSource(clean) {
			return nil, nil, fmt.Errorf("required path %q is unsafe", requiredPath)
		}
		cleanPaths = append(cleanPaths, clean)
	}
	if len(cleanPaths) == 0 {
		return []ContextCard{}, []string{}, nil
	}
	rows, err := retriever.BestDocumentChunks(ctx, task, "auto", tiers, cleanPaths)
	if err != nil {
		return nil, nil, err
	}
	cards, handles := []ContextCard{}, []string{}
	for _, clean := range cleanPaths {
		row, present := rows[clean]
		if !present || !contains(tiers, row.Chunk.Tier) {
			return nil, nil, fmt.Errorf(
				"required path %q is not in an allowed managed source class", clean)
		}
		if !retriever.verifyChunk(row.Chunk, row.DocumentHash) {
			return nil, nil, fmt.Errorf("read required path %q: %w", clean, errSourceChangedAfterIndex)
		}
		uri := "re-discipline://" + generation.ID + "/chunks/" + row.Chunk.ID
		card := contextProvenanceCard(SearchResult{
			ChunkID: row.Chunk.ID, Passage: row.Chunk.Content,
			DocumentContext: row.Chunk.Context, LaneRanks: map[string]int{"required": 1},
			Citation: Citation{
				Path: row.Chunk.Path, Heading: row.Chunk.Heading,
				StartLine: row.Chunk.StartLine, EndLine: row.Chunk.EndLine,
				SourceHash: row.DocumentHash, PassageHash: row.Chunk.ContentHash,
				Tier: row.Chunk.Tier, URI: uri,
			},
		}, true)
		cards = appendUniqueContextCards(cards, card)
		handles = append(handles, uri)
	}
	return cards, SortedUnique(handles), nil
}

func (service *Service) compileContextCandidates(
	ctx context.Context,
	retriever Retriever,
	generation Generation,
	selected SelectedProfile,
	request ContextPackRequest,
	tiers []string,
	graph *CampaignGraph,
	maxCards int,
) ([]ContextCard, map[string]int, error) {
	omissions := map[string]int{
		"candidateCards": 0, "budget": 0, "cardLimit": 0, "staleSource": 0,
	}
	cards := []ContextCard{}
	classes := contextFindingSourceClasses(tiers)
	if len(classes) > 0 {
		queryBudget := request.TokenBudget
		if queryBudget > 1200 {
			queryBudget = 1200
		}
		if queryBudget < 512 {
			queryBudget = 512
		}
		options := FindingQueryOptions{
			Query: request.Task, AllowedSourceClasses: classes,
			AllowedReviewStates: []string{"manager-ratified"},
			AllowedValidities:   []string{"provisional", "current", "challenged", "historical"},
			Limit:               5, TokenBudget: queryBudget,
			IncludeRaw:  contains(tiers, "archive"),
			suppressRaw: !contains(tiers, "archive"),
		}
		if graph != nil {
			options.CampaignID = graph.Campaign.ID
		}
		if contains(tiers, "provisional") || contains(tiers, "draft") || contains(tiers, "intake") {
			options.AllowedReviewStates = []string{
				"extracted", "curator-checked", "manager-ratified",
			}
		}
		response, err := retriever.QueryFindingCards(ctx, options)
		if err != nil {
			return nil, nil, err
		}
		for _, card := range response.Cards {
			cards = appendUniqueContextCards(cards, card)
		}
		omissions["candidateCards"] += response.Omitted
	}

	searchTiers := contextProvenanceTiers(tiers, graph != nil)
	if len(searchTiers) == 0 || len(cards) >= maxCards {
		return cards, omissions, nil
	}
	searchBudget := request.TokenBudget
	if searchBudget > service.effectiveSettings().Budgets.SearchTokens {
		searchBudget = service.effectiveSettings().Budgets.SearchTokens
	}
	if searchBudget < 512 {
		searchBudget = 512
	}
	search, err := (Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}).Search(ctx, SearchOptions{
		Query: request.Task, QueryClass: "auto", AllowedTiers: searchTiers,
		Limit: maxCards, TokenBudget: searchBudget,
		Verbosity: VerbosityCompact, retainURI: true,
	})
	if err != nil {
		return nil, nil, err
	}
	for _, result := range search.Results {
		cards = appendUniqueContextCards(cards, contextProvenanceCard(result, false))
	}
	omissions["candidateCards"] += search.Omitted
	omissions["budget"] += search.OmittedByReason["budget"]
	omissions["cardLimit"] += search.OmittedByReason["resultLimit"]
	omissions["staleSource"] += search.OmittedByReason["staleSource"]
	return cards, omissions, nil
}

func contextFindingSourceClasses(tiers []string) []string {
	classes := []string{}
	for _, tier := range tiers {
		switch tier {
		case "truth", "history", "backlog", "memory", "archive", "intake", "provisional", "playbook":
			classes = append(classes, tier)
		case "active", "campaign":
			classes = append(classes, "campaign")
		case "draft":
			classes = append(classes, "provisional")
		}
	}
	return SortedUnique(classes)
}

func contextProvenanceTiers(tiers []string, scoped bool) []string {
	result := []string{}
	for _, tier := range tiers {
		if scoped && validOne(tier, "active", "campaign", "draft", "provisional", "intake", "archive", "history") {
			continue
		}
		if validOne(tier, "profile", "navigation", "truth", "history", "backlog", "memory", "asset", "playbook") {
			result = append(result, tier)
		}
	}
	return SortedUnique(result)
}

func contextProvenanceCard(result SearchResult, required bool) ContextCard {
	why := []string{}
	lanes := make([]string, 0, len(result.LaneRanks))
	for lane := range result.LaneRanks {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	for _, lane := range lanes {
		why = append(why, "lane:"+lane+":"+strconv.Itoa(result.LaneRanks[lane]))
	}
	if required {
		why = []string{"required-path"}
	}
	claim := firstSentence(strings.TrimSpace(result.Passage), 320)
	card := ContextCard{
		SchemaVersion: CampaignSchemaVersion,
		ID:            StableID("provenance", result.ChunkID, result.Citation.URI),
		CardType:      "provenance", Claim: claim, Title: result.Citation.Heading,
		SourceClass: contextSourceClass(result.Citation.Tier),
		Handle:      result.Citation.URI,
		EvidenceHandle: fmt.Sprintf(
			"path:%s#L%d-L%d", result.Citation.Path,
			result.Citation.StartLine, result.Citation.EndLine),
		WhyMatched:      why,
		ExpansionTokens: EstimateTokens(result.Passage + result.DocumentContext),
		Metadata: map[string]string{
			"path": result.Citation.Path, "tier": result.Citation.Tier,
			"passageHash": result.Citation.PassageHash,
			"startLine":   strconv.Itoa(result.Citation.StartLine),
			"endLine":     strconv.Itoa(result.Citation.EndLine),
		},
	}
	if result.Citation.SourceHash != "" {
		card.Metadata["sourceHash"] = result.Citation.SourceHash
	}
	return card
}

func contextSourceClass(tier string) string {
	switch tier {
	case "active", "campaign":
		return "campaign"
	case "draft":
		return "provisional"
	default:
		return tier
	}
}

func appendUniqueContextCards(cards []ContextCard, additions ...ContextCard) []ContextCard {
	seen := map[string]bool{}
	for _, card := range cards {
		seen[card.Handle] = true
	}
	for _, card := range additions {
		if card.Handle == "" || seen[card.Handle] {
			continue
		}
		seen[card.Handle] = true
		cards = append(cards, card)
	}
	return cards
}

func finalizeContextPack(pack ContextPack) (ContextPack, error) {
	for iteration := 0; iteration < 8; iteration++ {
		pack.PackID, pack.Digest = "", ""
		digest, err := CanonicalDigest(pack)
		if err != nil {
			return ContextPack{}, err
		}
		pack.Digest = digest
		pack.PackID = "context-" + strings.TrimPrefix(digest, "sha256:")[:20]
		body, err := json.Marshal(pack)
		if err != nil {
			return ContextPack{}, err
		}
		estimated := EstimateTokens(string(body))
		if estimated == pack.EstimatedTokens {
			return pack, nil
		}
		pack.EstimatedTokens = estimated
	}
	return ContextPack{}, errors.New("context-pack token accounting failed to converge")
}
