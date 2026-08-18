package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type TraceRequest struct {
	CampaignID  string   `json:"campaignId,omitempty"`
	StartHandle string   `json:"startHandle"`
	Relations   []string `json:"relations,omitempty"`
	Direction   string   `json:"direction,omitempty"`
	Depth       int      `json:"depth,omitempty"`
	MaxNodes    int      `json:"maxNodes,omitempty"`
	TokenBudget int      `json:"tokenBudget,omitempty"`
}

type TraceEdge struct {
	Source   string `json:"source"`
	Relation string `json:"relation"`
	Target   string `json:"target"`
}

type TraceResponse struct {
	SchemaVersion   int           `json:"schemaVersion"`
	StartHandle     string        `json:"startHandle"`
	CampaignID      string        `json:"campaignId"`
	Direction       string        `json:"direction"`
	Depth           int           `json:"depth"`
	Nodes           []ContextCard `json:"nodes"`
	Edges           []TraceEdge   `json:"edges"`
	Omissions       []string      `json:"omissions,omitempty"`
	EstimatedTokens int           `json:"estimatedTokens"`
	Digest          string        `json:"digest"`
}

type traceNode struct {
	handle string
	card   ContextCard
	edges  []TraceEdge
}

var traceRelationSet = map[string]bool{
	"supports": true, "contradicts": true, "depends-on": true,
	"supersedes": true, "duplicates": true, "answers": true, "spawned": true,
	"evidence": true, "source-run": true, "work-item": true,
	"parent": true, "child": true, "blocked-by": true,
	"run": true, "finding": true, "review": true, "intake": true,
	"focus": true, "projection": true,
}

// SupportedTraceRelations is the traversable relation set in a stable order.
// A refusal that rejects one relation name without listing the accepted ones
// costs a tool-schema round trip to recover from, and the set is nineteen
// hyphenated names that nobody guesses.
func SupportedTraceRelations() []string {
	relations := make([]string, 0, len(traceRelationSet))
	for relation := range traceRelationSet {
		relations = append(relations, relation)
	}
	sort.Strings(relations)
	return relations
}

func (service *Service) Trace(ctx context.Context, request TraceRequest) (TraceResponse, error) {
	if service == nil {
		return TraceResponse{}, errors.New("service is required")
	}
	if err := ctx.Err(); err != nil {
		return TraceResponse{}, err
	}
	request.StartHandle = strings.TrimSpace(request.StartHandle)
	if request.StartHandle == "" {
		return TraceResponse{}, errors.New(
			"trace requires startHandle: an exact finding, record, evidence, run, or state " +
				"handle. query returns finding handles, and every state view returns handles " +
				"in its expansions list")
	}
	if request.Direction == "" {
		request.Direction = "both"
	}
	if !validOne(request.Direction, "outgoing", "incoming", "both") {
		return TraceResponse{}, fmt.Errorf(
			"trace direction must be outgoing, incoming, or both; it is %q", request.Direction)
	}
	if request.Depth == 0 {
		request.Depth = 2
	}
	if request.Depth < 1 || request.Depth > 4 {
		return TraceResponse{}, errors.New("trace depth must be between 1 and 4")
	}
	if request.MaxNodes == 0 {
		request.MaxNodes = 24
	}
	if request.MaxNodes < 1 || request.MaxNodes > 100 {
		return TraceResponse{}, errors.New("trace maxNodes must be between 1 and 100")
	}
	if request.TokenBudget == 0 {
		request.TokenBudget = 1500
	}
	if request.TokenBudget < 128 || request.TokenBudget > 8192 {
		return TraceResponse{}, errors.New("trace token budget must be between 128 and 8192")
	}
	allowed := map[string]bool{}
	for _, relation := range request.Relations {
		if !traceRelationSet[relation] {
			return TraceResponse{}, fmt.Errorf(
				"unsupported trace relation %q; trace follows %s",
				relation, strings.Join(SupportedTraceRelations(), ", "))
		}
		allowed[relation] = true
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	graph, err := graphForTraceHandle(store, request.CampaignID, request.StartHandle)
	if err != nil {
		return TraceResponse{}, err
	}
	resolver := newGraphTraceResolver(graph)
	start, ok := resolver.nodes[request.StartHandle]
	if !ok && strings.HasPrefix(request.StartHandle, "record:") {
		start = resolver.nodes[resolver.recordAliases[request.StartHandle]]
		ok = start.handle != ""
	}
	if !ok {
		return TraceResponse{}, fmt.Errorf(
			"trace start handle %q does not resolve to a finding, record, evidence, run, or "+
				"state node. Remedy: if the handle is campaign-local, pass campaignId to "+
				"disambiguate it; otherwise query returns exact finding handles and read "+
				"selector=record confirms a record handle before tracing from it",
			request.StartHandle)
	}
	response := TraceResponse{
		SchemaVersion: CampaignSchemaVersion, StartHandle: start.handle,
		CampaignID: graph.Campaign.ID, Direction: request.Direction, Depth: request.Depth,
		Nodes: []ContextCard{}, Edges: []TraceEdge{}, Omissions: []string{},
	}
	type frontierItem struct {
		handle string
		depth  int
	}
	frontier := []frontierItem{{handle: start.handle}}
	seen := map[string]bool{}
	seenEdges := map[string]bool{}
	omitted := 0
	for len(frontier) > 0 {
		item := frontier[0]
		frontier = frontier[1:]
		if seen[item.handle] {
			continue
		}
		if len(response.Nodes) >= request.MaxNodes {
			omitted++
			continue
		}
		node, ok := resolver.nodes[item.handle]
		if !ok {
			continue
		}
		seen[item.handle] = true
		response.Nodes = append(response.Nodes, node.card)
		if item.depth >= request.Depth {
			continue
		}
		edges := resolver.edgesFor(item.handle, request.Direction)
		for _, edge := range edges {
			if len(allowed) > 0 && !allowed[edge.Relation] {
				continue
			}
			key := edge.Source + "\x00" + edge.Relation + "\x00" + edge.Target
			if !seenEdges[key] {
				seenEdges[key] = true
				response.Edges = append(response.Edges, edge)
			}
			next := edge.Target
			if edge.Target == item.handle {
				next = edge.Source
			}
			if !seen[next] {
				frontier = append(frontier, frontierItem{handle: next, depth: item.depth + 1})
			}
		}
		sort.Slice(frontier, func(i, j int) bool {
			if frontier[i].depth != frontier[j].depth {
				return frontier[i].depth < frontier[j].depth
			}
			return frontier[i].handle < frontier[j].handle
		})
	}
	sort.Slice(response.Edges, func(i, j int) bool {
		left := response.Edges[i].Source + "\x00" + response.Edges[i].Relation + "\x00" + response.Edges[i].Target
		right := response.Edges[j].Source + "\x00" + response.Edges[j].Relation + "\x00" + response.Edges[j].Target
		return left < right
	})
	if omitted > 0 {
		response.Omissions = append(response.Omissions, strconv.Itoa(omitted)+" nodes omitted by maxNodes")
	}
	return boundAndSealTrace(response, request.TokenBudget)
}

type graphTraceResolver struct {
	nodes         map[string]traceNode
	edges         []TraceEdge
	recordAliases map[string]string
}

func newGraphTraceResolver(graph CampaignGraph) graphTraceResolver {
	resolver := graphTraceResolver{
		nodes: map[string]traceNode{}, edges: []TraceEdge{}, recordAliases: map[string]string{},
	}
	add := func(handle string, card ContextCard) {
		resolver.nodes[handle] = traceNode{handle: handle, card: card}
		if strings.HasPrefix(card.Handle, "record:") {
			resolver.recordAliases[card.Handle] = handle
		}
	}
	add("campaign:"+graph.Campaign.ID, campaignStateCard(*graph.Campaign))
	workIDs := make([]string, 0, len(graph.WorkItems))
	for id := range graph.WorkItems {
		workIDs = append(workIDs, id)
	}
	sort.Strings(workIDs)
	for _, id := range workIDs {
		item := graph.WorkItems[id]
		add("work:"+id, workStateCard(graph.Campaign.Slug, item))
		for _, target := range item.Relations.ParentIDs {
			resolver.addEdge("work:"+id, "parent", "work:"+target)
		}
		for _, target := range item.Relations.ChildIDs {
			resolver.addEdge("work:"+id, "child", "work:"+target)
		}
		for _, target := range item.Relations.DependsOn {
			resolver.addEdge("work:"+id, "depends-on", "work:"+target)
		}
		for _, target := range item.Relations.BlockedBy {
			resolver.addEdge("work:"+id, "blocked-by", "work:"+target)
		}
		for _, run := range append(append([]string{}, item.ActiveRunIDs...), item.CompletedRunIDs...) {
			resolver.addEdge("work:"+id, "run", "run:"+run)
		}
		for _, finding := range item.FindingIDs {
			resolver.addEdge("work:"+id, "finding", FindingHandle(finding))
		}
	}
	for _, focus := range graph.Campaign.CurrentFocus {
		resolver.addEdge("campaign:"+graph.Campaign.ID, "focus", "work:"+focus)
	}
	for _, id := range sortedRunIDs(graph.Runs) {
		run := graph.Runs[id]
		add("run:"+id, runStateCard(graph.Campaign.Slug, run))
		resolver.addEdge("run:"+id, "work-item", "work:"+run.PrimaryWorkItemID)
		for _, finding := range run.FindingIDs {
			resolver.addEdge("run:"+id, "finding", FindingHandle(finding))
		}
		for _, work := range run.SpawnedWorkItemIDs {
			resolver.addEdge("run:"+id, "spawned", "work:"+work)
		}
	}
	findingIDs := make([]string, 0, len(graph.Findings))
	for id := range graph.Findings {
		findingIDs = append(findingIDs, id)
	}
	sort.Strings(findingIDs)
	for _, id := range findingIDs {
		finding := graph.Findings[id]
		add(FindingHandle(id), findingStateCard(finding))
		for relation, targets := range FindingRelationSets(finding.Relations) {
			label := strings.ReplaceAll(relation, "_", "-")
			for _, target := range targets {
				handle := FindingHandle(target)
				if label == "spawned" {
					handle = "work:" + target
				}
				resolver.addEdge(FindingHandle(id), label, handle)
			}
		}
		for _, evidence := range finding.Evidence {
			handle := EvidenceHandle(id, evidence)
			add(handle, ContextCard{
				SchemaVersion: CampaignSchemaVersion, ID: handle, CardType: "provenance",
				Title: evidence.Path, SourceClass: "campaign", Handle: handle,
				ExpansionTokens: 500, Metadata: map[string]string{
					"sha256": evidence.SHA256, "sourceRun": evidence.SourceRun,
					"startLine": strconv.Itoa(evidence.StartLine), "endLine": strconv.Itoa(evidence.EndLine),
				},
			})
			resolver.addEdge(FindingHandle(id), "evidence", handle)
		}
		for _, run := range finding.SourceRuns {
			resolver.addEdge(FindingHandle(id), "source-run", "run:"+run)
		}
		if finding.Projection != "none" && finding.Projection != "campaign" {
			handle := "projection:" + id + ":" + finding.Projection
			add(handle, ContextCard{
				SchemaVersion: CampaignSchemaVersion, ID: handle, CardType: "decision",
				Title: "Projection: " + finding.Projection, SourceClass: "state",
				Handle: handle, ExpansionTokens: 100,
			})
			resolver.addEdge(FindingHandle(id), "projection", handle)
		}
	}
	for _, id := range sortedIntakeIDs(graph.Intakes) {
		intake := graph.Intakes[id]
		handle := "intake:" + id
		add(handle, ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: id, CardType: "decision",
			Title: "Intake " + id, SourceClass: "state", Handle: "record:active/" + graph.Campaign.Slug + "/intake/" + id + ".json",
			ExpansionTokens: 400, Metadata: map[string]string{"status": intake.Status},
		})
		for _, finding := range intake.CandidateFindingIDs {
			resolver.addEdge(handle, "finding", FindingHandle(finding))
		}
	}
	for _, id := range sortedReviewIDs(graph.Reviews) {
		review := graph.Reviews[id]
		handle := "review:" + id
		add(handle, reviewStateCard(graph.Campaign.Slug, review))
		resolver.addEdge(handle, "intake", "intake:"+review.IntakeID)
		for _, decision := range review.Decisions {
			resolver.addEdge(handle, "review", FindingHandle(decision.FindingID))
		}
	}
	sort.Slice(resolver.edges, func(i, j int) bool {
		left := resolver.edges[i].Source + "\x00" + resolver.edges[i].Relation + "\x00" + resolver.edges[i].Target
		right := resolver.edges[j].Source + "\x00" + resolver.edges[j].Relation + "\x00" + resolver.edges[j].Target
		return left < right
	})
	return resolver
}

func (resolver *graphTraceResolver) addEdge(source, relation, target string) {
	resolver.edges = append(resolver.edges, TraceEdge{Source: source, Relation: relation, Target: target})
}

func (resolver graphTraceResolver) edgesFor(handle, direction string) []TraceEdge {
	result := []TraceEdge{}
	for _, edge := range resolver.edges {
		if (direction == "outgoing" || direction == "both") && edge.Source == handle {
			result = append(result, edge)
		}
		if (direction == "incoming" || direction == "both") && edge.Target == handle {
			result = append(result, edge)
		}
	}
	return result
}

func graphForTraceHandle(store *StateStore, campaignID, handle string) (CampaignGraph, error) {
	if campaignID != "" {
		return store.LoadCampaignGraph(campaignID)
	}
	parts := strings.Split(handle, ":")
	if len(parts) >= 2 {
		switch parts[0] {
		case "finding", "evidence":
			graph, _, err := locateFinding(store, "", parts[1])
			return graph, err
		case "run":
			graph, _, err := locateRun(store, "", parts[1])
			return graph, err
		case "campaign":
			return store.LoadCampaignGraph(parts[1])
		}
	}
	graphs, err := candidateGraphs(store, "")
	if err != nil {
		return CampaignGraph{}, err
	}
	var found *CampaignGraph
	for index := range graphs {
		resolver := newGraphTraceResolver(graphs[index])
		_, direct := resolver.nodes[handle]
		_, alias := resolver.recordAliases[handle]
		if direct || alias {
			if found != nil {
				return CampaignGraph{}, errors.New(
					"trace handle resolves in more than one campaign. Remedy: pass campaignId " +
						"to name the one you mean; state mode=orient lists the open campaigns")
			}
			copy := graphs[index]
			found = &copy
		}
	}
	if found == nil {
		return CampaignGraph{}, errors.New("trace handle does not resolve")
	}
	return *found, nil
}

func boundAndSealTrace(response TraceResponse, budget int) (TraceResponse, error) {
	for _, card := range response.Nodes {
		if err := ValidateContextCard(card); err != nil {
			return TraceResponse{}, err
		}
	}
	omitted := 0
	for {
		response.Digest = ""
		response.EstimatedTokens = 0
		body, err := json.Marshal(response)
		if err != nil {
			return TraceResponse{}, err
		}
		response.EstimatedTokens = EstimateTokens(string(body))
		if response.EstimatedTokens <= budget {
			break
		}
		if len(response.Nodes) <= 1 {
			return TraceResponse{}, fmt.Errorf(
				"trace tokenBudget %d is below the %d tokens the start node and envelope "+
					"already cost. Remedy: re-issue with tokenBudget at least %d, or narrow the "+
					"trace with depth=1 and a shorter relations list",
				budget, response.EstimatedTokens, response.EstimatedTokens)
		}
		removed := response.Nodes[len(response.Nodes)-1].Handle
		response.Nodes = response.Nodes[:len(response.Nodes)-1]
		keptEdges := response.Edges[:0]
		for _, edge := range response.Edges {
			if edge.Source != removed && edge.Target != removed {
				keptEdges = append(keptEdges, edge)
			}
		}
		response.Edges = keptEdges
		omitted++
	}
	if omitted > 0 {
		response.Omissions = append(response.Omissions, strconv.Itoa(omitted)+" nodes omitted by token budget")
		response.Omissions = SortedUnique(response.Omissions)
	}
	for iteration := 0; iteration < 4; iteration++ {
		response.Digest = ""
		digest, err := CanonicalDigest(response)
		if err != nil {
			return TraceResponse{}, err
		}
		response.Digest = digest
		body, _ := json.Marshal(response)
		cost := EstimateTokens(string(body))
		if cost == response.EstimatedTokens {
			return response, nil
		}
		response.EstimatedTokens = cost
	}
	if response.EstimatedTokens > budget {
		return TraceResponse{}, errors.New("trace response exceeded token budget after sealing")
	}
	return response, nil
}
