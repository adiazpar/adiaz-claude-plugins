package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

type ClosureApplyRequest struct {
	Action                   string            `json:"action"`
	Actor                    string            `json:"actor"`
	CampaignSlug             string            `json:"campaignSlug"`
	CampaignID               string            `json:"campaignId"`
	CorrelationID            string            `json:"correlationId"`
	IdempotencyKey           string            `json:"idempotencyKey"`
	Rationale                string            `json:"rationale,omitempty"`
	Timestamp                string            `json:"timestamp,omitempty"`
	ClosureJobID             string            `json:"closureJobId,omitempty"`
	TargetStage              string            `json:"targetStage,omitempty"`
	ArchiveDestination       string            `json:"archiveDestination,omitempty"`
	ExpectedHeadRevision     int64             `json:"expectedHeadRevision,omitempty"`
	ExpectedHeadDigest       string            `json:"expectedHeadDigest,omitempty"`
	ExpectedRecordDigests    map[string]string `json:"expectedRecordDigests,omitempty"`
	ExpectedArtifactDigests  map[string]string `json:"expectedArtifactDigests,omitempty"`
	ExpectedCoverageRevision int64             `json:"expectedCoverageRevision,omitempty"`
	FileRetention            map[string]string `json:"fileRetention,omitempty"`
	ExportedWorkItemIDs      []string          `json:"exportedWorkItemIds,omitempty"`
	ProjectionDestinations   map[string]string `json:"projectionDestinations,omitempty"`
}

type ClosureApplyResult struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Action        string                   `json:"action"`
	State         *StateView               `json:"state,omitempty"`
	Transaction   *StateTransactionReceipt `json:"transaction,omitempty"`
	Plan          *ClosurePlan             `json:"plan,omitempty"`
	Job           *ClosureJob              `json:"job,omitempty"`
	Coverage      *ClosureCoverage         `json:"coverage,omitempty"`
	Receipt       *ClosureReceipt          `json:"receipt,omitempty"`
	Archive       *ArchiveManifest         `json:"archive,omitempty"`
	Digest        string                   `json:"digest"`
}

func (service *Service) ClosureApply(ctx context.Context, request ClosureApplyRequest) (ClosureApplyResult, error) {
	if service == nil {
		return ClosureApplyResult{}, errors.New("service is required")
	}
	if !validOne(request.Action, "start", "status", "advance", "reopen", "verify", "finalize") {
		return ClosureApplyResult{}, fmt.Errorf("unsupported closure action %q", request.Action)
	}
	if request.Action == "status" {
		view, err := service.State(ctx, StateRequest{Mode: "closure", CampaignID: request.CampaignID})
		if err != nil {
			return ClosureApplyResult{}, err
		}
		result := ClosureApplyResult{SchemaVersion: CampaignSchemaVersion, Action: request.Action, State: &view}
		return sealClosureApplyResult(result)
	}
	if request.Actor == "" || request.CampaignID == "" || request.CampaignSlug == "" ||
		request.CorrelationID == "" || request.IdempotencyKey == "" ||
		!digestRE.MatchString(request.ExpectedHeadDigest) {
		return ClosureApplyResult{}, errors.New("closure mutation requires actor, campaign, correlation, idempotency, and expected head")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, request.Timestamp)
	if err != nil || timestamp.Location() != time.UTC {
		return ClosureApplyResult{}, errors.New("closure mutation requires a UTC RFC3339 timestamp")
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	store.Now = func() time.Time { return timestamp }
	if err := store.Recover(ctx); err != nil {
		return ClosureApplyResult{}, err
	}
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if graph.Campaign.Slug != request.CampaignSlug || !containsString(graph.Campaign.PermittedManagers, request.Actor) {
		return ClosureApplyResult{}, errors.New("closure campaign slug or manager authority does not match")
	}
	switch request.Action {
	case "start":
		return service.startClosure(ctx, store, graph, request)
	case "advance", "verify", "finalize":
		return service.advanceClosure(ctx, store, graph, request)
	case "reopen":
		return service.reopenClosure(ctx, store, graph, request)
	default:
		return ClosureApplyResult{}, errors.New("unsupported closure transition")
	}
}

func (service *Service) startClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	if graph.ClosureJob != nil || !validOne(graph.Campaign.Status, "open", "paused") {
		return ClosureApplyResult{}, errors.New("closure can start only once on an open or paused campaign")
	}
	if !correlationIDRE.MatchString(request.ClosureJobID) {
		return ClosureApplyResult{}, errors.New("closure start requires a stable closureJobId")
	}
	plan, err := BuildClosurePlan(graph, request.ArchiveDestination)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	prior := explicitClosureCoverage(graph.Campaign.ID, request)
	coverage, err := ComputeClosureCoverage(graph, &prior)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.UpdatedAt, campaign.UpdatedBy, campaign.CorrelationID, campaign.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	campaign.Status, campaign.ClosingAt, campaign.PausedAt = "closing", request.Timestamp, ""
	job := ClosureJob{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: request.ClosureJobID,
			CreatedAt: request.Timestamp, UpdatedAt: request.Timestamp, Revision: 1,
			CreatedBy: request.Actor, UpdatedBy: request.Actor, CorrelationID: request.CorrelationID,
		},
		CampaignID: graph.Campaign.ID, Stage: "inventory", Status: "running",
		FrozenCampaignRevision: plan.CampaignRevision,
		ProjectionFindingIDs:   append([]string(nil), plan.ProjectionFindingIDs...),
		Coverage:               &coverage, ArchiveDestination: plan.ArchiveDestination,
		TruthDigests: map[string]string{}, ProjectionDigests: map[string]string{},
		Blockers: closureCoverageBlockers(coverage),
	}
	writes, err := closureWrites(request.CampaignSlug, request.CorrelationID, request.ExpectedRecordDigests, []closureWriteSpec{
		{value: campaign, expectedRevision: graph.Campaign.Revision, expectedDigest: request.ExpectedRecordDigests[graph.Campaign.ID]},
		{value: plan}, {value: job}, {value: coverage},
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	receipt, err := store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.start",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, Writes: writes,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: &plan, Job: &job, Coverage: &coverage,
	}
	return sealClosureApplyResult(result)
}

func (service *Service) advanceClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	if graph.ClosureJob == nil || graph.ClosureCoverage == nil || graph.ClosurePlan == nil || graph.Campaign.Status != "closing" {
		return ClosureApplyResult{}, errors.New("closure advance requires a complete active closure job")
	}
	target := request.TargetStage
	if request.Action == "verify" {
		target = "verify"
	}
	if request.Action == "finalize" {
		target = "finalize"
	}
	from, fromOK := closureStageIndex[graph.ClosureJob.Stage]
	to, toOK := closureStageIndex[target]
	if !fromOK || !toOK || to != from+1 {
		return ClosureApplyResult{}, errors.New("closure targetStage must be the next explicit stage")
	}
	prior := cloneClosureCoverage(*graph.ClosureCoverage)
	for key, value := range request.FileRetention {
		prior.FileRetention[key] = value
	}
	for _, id := range request.ExportedWorkItemIDs {
		prior.WorkItemCoverage[id] = "exported-backlog"
	}
	workingGraph := graph
	extraWrites := []closureWriteSpec{}
	if target == "project" {
		projectedGraph, projectionFindingWrites, transitionErr := service.prepareClosureFindingTransitions(store, graph, request)
		if transitionErr != nil {
			return ClosureApplyResult{}, transitionErr
		}
		workingGraph = projectedGraph
		extraWrites = append(extraWrites, projectionFindingWrites...)
	}
	coverage, err := ComputeClosureCoverage(workingGraph, &prior)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	next := *graph.ClosureJob
	next.Revision++
	next.UpdatedAt, next.UpdatedBy, next.CorrelationID, next.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	next.Stage, next.Status, next.Coverage = target, "running", &coverage
	next.Blockers = closureCoverageBlockers(coverage)
	if len(next.Blockers) != 0 {
		next.Status = "blocked"
	}

	action := "closure.advance"
	artifacts := []StateArtifactWrite{}
	var archive *ArchiveManifest
	var finalReceipt *ClosureReceipt
	switch target {
	case "project":
		action = "closure.project"
		truthDigests, projectionDigests, projectArtifacts, err := service.prepareClosureProjections(workingGraph, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		next.TruthDigests = truthDigests
		next.ProjectionDigests = projectionDigests
		artifacts = append(artifacts, projectArtifacts...)
	case "verify":
		action = "closure.verify"
		if err := service.verifyClosureProjections(graph, request); err != nil {
			return ClosureApplyResult{}, err
		}
		next.Status = "verified"
	case "archive":
		action = "closure.archive"
		manifest, archiveArtifacts, err := service.prepareClosureArchive(store, graph, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		next.ArchiveDigest = manifest.Digest
		archive = &manifest
		artifacts = append(artifacts, archiveArtifacts...)
		extraWrites = append(extraWrites, closureWriteSpec{
			value: manifest, path: path.Join(graph.ClosureJob.ArchiveDestination, "manifest.json"),
		})
	case "finalize":
		action = "closure.finalize"
		next.Status = "completed"
		campaign, receipt, finalizeArtifacts, err := service.prepareClosureFinalization(store, graph, next, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		finalReceipt = &receipt
		artifacts = append(artifacts, finalizeArtifacts...)
		extraWrites = append(extraWrites,
			closureWriteSpec{value: campaign, expectedRevision: graph.Campaign.Revision,
				expectedDigest: request.ExpectedRecordDigests[graph.Campaign.ID]},
			closureWriteSpec{value: receipt},
		)
	}
	if target == "finalize" {
		next.Blockers = []string{}
	}
	if err := sealClosureJobForComparison(&next); err != nil {
		return ClosureApplyResult{}, err
	}
	if err := ValidateClosureAdvance(*graph.ClosureJob, next); err != nil {
		return ClosureApplyResult{}, err
	}
	specs := []closureWriteSpec{
		{value: next, expectedRevision: graph.ClosureJob.Revision, expectedDigest: request.ExpectedRecordDigests[graph.ClosureJob.ID]},
	}
	if coverage.Digest != graph.ClosureCoverage.Digest {
		expectedRevision := request.ExpectedCoverageRevision
		if expectedRevision < 1 {
			expectedRevision = 1
		}
		specs = append(specs, closureWriteSpec{
			value: coverage, expectedRevision: expectedRevision,
			expectedDigest: request.ExpectedRecordDigests["closure-coverage"],
		})
	}
	specs = append(specs, extraWrites...)
	writes, err := closureWrites(request.CampaignSlug, request.CorrelationID, request.ExpectedRecordDigests, specs)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	receipt, err := store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: action,
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, Writes: writes, Artifacts: artifacts,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: graph.ClosurePlan, Job: &next, Coverage: &coverage,
		Receipt: finalReceipt, Archive: archive,
	}
	return sealClosureApplyResult(result)
}

func (service *Service) reopenClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	if graph.ClosureJob == nil || graph.Campaign.Status != "closing" {
		return ClosureApplyResult{}, errors.New("only a closing campaign may be reopened")
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.UpdatedAt, campaign.UpdatedBy, campaign.CorrelationID, campaign.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	campaign.Status, campaign.ClosingAt = "open", ""
	job := *graph.ClosureJob
	job.Revision++
	job.UpdatedAt, job.UpdatedBy, job.CorrelationID, job.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	job.Status = "reopened"
	if err := sealClosureJobForComparison(&job); err != nil {
		return ClosureApplyResult{}, err
	}
	if err := ValidateClosureAdvance(*graph.ClosureJob, job); err != nil {
		return ClosureApplyResult{}, err
	}
	writes, err := closureWrites(request.CampaignSlug, request.CorrelationID, request.ExpectedRecordDigests, []closureWriteSpec{
		{value: campaign, expectedRevision: graph.Campaign.Revision, expectedDigest: request.ExpectedRecordDigests[graph.Campaign.ID]},
		{value: job, expectedRevision: graph.ClosureJob.Revision, expectedDigest: request.ExpectedRecordDigests[graph.ClosureJob.ID]},
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	receipt, err := store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.reopen",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, Writes: writes,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: graph.ClosurePlan, Job: &job, Coverage: graph.ClosureCoverage,
	}
	return sealClosureApplyResult(result)
}

type closureWriteSpec struct {
	value            any
	path             string
	expectedRevision int64
	expectedDigest   string
}

func closureWrites(
	slug, correlation string,
	expectedDigests map[string]string,
	specs []closureWriteSpec,
) ([]StateWrite, error) {
	writes := make([]StateWrite, 0, len(specs))
	for _, spec := range specs {
		id, _, _, _, _, err := stateRecordIdentity(spec.value, spec.expectedRevision, correlation)
		if err != nil {
			return nil, err
		}
		recordPath := spec.path
		if recordPath == "" {
			recordPath, err = stateRecordPath(slug, spec.value)
			if err != nil {
				return nil, err
			}
		}
		if spec.expectedRevision > 0 && !digestRE.MatchString(spec.expectedDigest) {
			return nil, fmt.Errorf("closure record %s requires its exact expected digest", id)
		}
		writes = append(writes, StateWrite{
			Path: recordPath, ExpectedRevision: spec.expectedRevision,
			ExpectedDigest: spec.expectedDigest, Record: spec.value,
		})
	}
	return writes, nil
}

func explicitClosureCoverage(campaignID string, request ClosureApplyRequest) ClosureCoverage {
	prior := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: campaignID,
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		UnresolvedConflicts: []string{}, MissingDecisions: []string{},
	}
	for key, value := range request.FileRetention {
		prior.FileRetention[key] = value
	}
	for _, id := range request.ExportedWorkItemIDs {
		prior.WorkItemCoverage[id] = "exported-backlog"
	}
	return prior
}

func closureCoverageBlockers(coverage ClosureCoverage) []string {
	return SortedUnique(append(append([]string{}, coverage.MissingDecisions...), coverage.UnresolvedConflicts...))
}

func cloneClosureCoverage(source ClosureCoverage) ClosureCoverage {
	result := source
	result.SourceRunCoverage = cloneStringMap(source.SourceRunCoverage)
	result.FindingCoverage = cloneStringMap(source.FindingCoverage)
	result.WorkItemCoverage = cloneStringMap(source.WorkItemCoverage)
	result.FileRetention = cloneStringMap(source.FileRetention)
	result.UnresolvedConflicts = append([]string(nil), source.UnresolvedConflicts...)
	result.MissingDecisions = append([]string(nil), source.MissingDecisions...)
	return result
}

// prepareClosureFindingTransitions performs the sole provisional-to-current
// promotion. It reads each canonical finding document, advances exactly one
// revision under the closure action, and returns a graph projection used to
// render truth bytes. The finding revisions and truth artifacts are committed
// together by the caller.
func (service *Service) prepareClosureFindingTransitions(
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (CampaignGraph, []closureWriteSpec, error) {
	next := cloneCampaignGraph(graph)
	writes := []closureWriteSpec{}
	ids := append([]string(nil), graph.ClosurePlan.ProjectionFindingIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		finding, present := graph.Findings[id]
		if !present || finding.Projection != "truth" {
			return CampaignGraph{}, nil, fmt.Errorf("truth projection finding %s is missing or no longer targets truth", id)
		}
		if finding.Validity == "current" {
			continue
		}
		if finding.Validity != "provisional" || finding.ReviewState != "manager-ratified" ||
			finding.EvidenceGrade != "direct" || len(finding.Relations.Contradicts) != 0 {
			return CampaignGraph{}, nil, fmt.Errorf("truth projection finding %s is not closure-promotable", id)
		}
		expectedDigest := request.ExpectedRecordDigests[id]
		if expectedDigest != finding.Digest || !digestRE.MatchString(expectedDigest) {
			return CampaignGraph{}, nil, fmt.Errorf("truth projection finding %s requires its exact expected digest", id)
		}
		relative := path.Join("active", request.CampaignSlug, "findings", id+".md")
		_, value, handle, err := store.readCanonicalRecordValue(relative)
		if err != nil {
			return CampaignGraph{}, nil, err
		}
		document, ok := value.(FindingDocument)
		if !ok || handle.RecordDigest != finding.Digest || document.Record.Revision != finding.Revision {
			return CampaignGraph{}, nil, fmt.Errorf("truth projection finding %s changed during closure", id)
		}
		document.Record.Revision++
		document.Record.UpdatedAt, document.Record.UpdatedBy = request.Timestamp, request.Actor
		document.Record.CorrelationID, document.Record.Digest = request.CorrelationID, ""
		document.Record.Validity = "current"
		sealed, _, err := sealFindingStateRecord(document, relative)
		if err != nil {
			return CampaignGraph{}, nil, err
		}
		document = sealed.(FindingDocument)
		next.Findings[id] = document.Record
		writes = append(writes, closureWriteSpec{
			value: document, expectedRevision: finding.Revision, expectedDigest: expectedDigest,
		})
	}
	return next, writes, nil
}

func (service *Service) prepareClosureProjections(
	graph CampaignGraph,
	request ClosureApplyRequest,
) (map[string]string, map[string]string, []StateArtifactWrite, error) {
	truthDigests := map[string]string{}
	projectionDigests := map[string]string{}
	artifacts := []StateArtifactWrite{}
	seenDestinations := map[string]bool{}
	ids := make([]string, 0, len(graph.Findings))
	for id := range graph.Findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		finding := graph.Findings[id]
		destination := request.ProjectionDestinations[id]
		switch finding.Projection {
		case "history", "archive", "rejected":
			continue
		case "truth", "backlog", "playbook", "maintained":
		default:
			return nil, nil, nil, fmt.Errorf("finding %s has no closure-ready projection", id)
		}
		if destination == "" {
			return nil, nil, nil, fmt.Errorf("finding %s requires a projection destination", id)
		}
		if seenDestinations[destination] {
			return nil, nil, nil, fmt.Errorf("projection destination %s is assigned more than once", destination)
		}
		seenDestinations[destination] = true

		var body []byte
		var digest string
		switch finding.Projection {
		case "truth":
			projection, err := BuildTruthProjection(service.Boundary, finding, destination)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("project finding %s: %w", id, err)
			}
			body, digest = projection.Body, projection.ContentDigest
			truthDigests[id] = digest
		case "backlog", "playbook":
			prefix := "docs/" + finding.Projection + "/"
			if validateRelativeRecordPath(destination) != nil ||
				!strings.HasPrefix(destination, prefix) || path.Ext(destination) != ".md" {
				return nil, nil, nil, fmt.Errorf("%s projection %s must be Markdown below %s", finding.Projection, id, prefix)
			}
			var err error
			body, err = renderClosureFindingProjection(finding, destination)
			if err != nil {
				return nil, nil, nil, err
			}
			digest = "sha256:" + SHA256Bytes(body)
		case "maintained":
			if err := validateRelativeRecordPath(destination); err != nil {
				return nil, nil, nil, fmt.Errorf("maintained projection %s: %w", id, err)
			}
			absolute, err := service.Boundary.Resolve(destination, true)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("maintained projection %s: %w", id, err)
			}
			body, err = readSingleLinkRegularFile(absolute)
			if err != nil {
				return nil, nil, nil, err
			}
			digest = "sha256:" + SHA256Bytes(body)
		}
		projectionDigests[destination] = digest
		if finding.Projection != "maintained" {
			artifacts = append(artifacts, StateArtifactWrite{
				Path: destination, ExpectedDigest: request.ExpectedArtifactDigests[destination],
				ContentDigest: digest, Body: body,
			})
		}
	}
	for id := range request.ProjectionDestinations {
		if _, exists := graph.Findings[id]; !exists {
			return nil, nil, nil, fmt.Errorf("projection destination names unknown finding %s", id)
		}
	}
	for _, id := range graph.ClosurePlan.ProjectionFindingIDs {
		if !digestRE.MatchString(truthDigests[id]) {
			return nil, nil, nil, fmt.Errorf("truth candidate %s was not projected", id)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return truthDigests, projectionDigests, artifacts, nil
}

func renderClosureFindingProjection(finding FindingRecord, destination string) ([]byte, error) {
	scope, err := json.Marshal(finding.Scope)
	if err != nil {
		return nil, err
	}
	semanticDigest, err := CanonicalDigest(struct {
		SchemaVersion int                 `json:"schemaVersion"`
		FindingID     string              `json:"findingId"`
		CampaignID    string              `json:"campaignId"`
		Projection    string              `json:"projection"`
		Destination   string              `json:"destination"`
		Claim         string              `json:"claim"`
		Scope         map[string]any      `json:"scope"`
		Evidence      []EvidenceReference `json:"evidence"`
	}{CampaignSchemaVersion, finding.ID, finding.CampaignID, finding.Projection,
		destination, finding.Claim, finding.Scope, finding.Evidence})
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("---\n")
	fmt.Fprintf(&output, "schemaVersion: %d\n", CampaignSchemaVersion)
	fmt.Fprintf(&output, "sourceFinding: %s\n", yamlScalar(finding.ID))
	fmt.Fprintf(&output, "sourceCampaign: %s\n", yamlScalar(finding.CampaignID))
	fmt.Fprintf(&output, "projection: %s\n", yamlScalar(finding.Projection))
	fmt.Fprintf(&output, "subject: %s\n", yamlScalar(finding.Subject))
	fmt.Fprintf(&output, "scope: %s\n", string(scope))
	fmt.Fprintf(&output, "semanticDigest: %s\n", yamlScalar(semanticDigest))
	output.WriteString("---\n\n# ")
	output.WriteString(finding.Subject)
	output.WriteString("\n\n")
	output.WriteString(finding.Claim)
	output.WriteString("\n\n## Provenance\n\n")
	fmt.Fprintf(&output, "- Finding: `%s`\n", finding.ID)
	for _, evidence := range finding.Evidence {
		fmt.Fprintf(&output, "- Evidence: `%s`\n", EvidenceHandle(finding.ID, evidence))
	}
	return []byte(output.String()), nil
}

func (service *Service) verifyClosureProjections(graph CampaignGraph, request ClosureApplyRequest) error {
	job := graph.ClosureJob
	if job == nil || len(job.ProjectionDigests) == 0 && len(job.ProjectionFindingIDs) != 0 {
		return errors.New("closure has no staged projection inventory")
	}
	for destination, expected := range job.ProjectionDigests {
		absolute, err := service.Boundary.Resolve(destination, true)
		if err != nil {
			return err
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return err
		}
		if got := "sha256:" + SHA256Bytes(body); got != expected {
			return fmt.Errorf("projection %s digest changed", destination)
		}
	}
	for _, id := range job.ProjectionFindingIDs {
		finding, exists := graph.Findings[id]
		if !exists {
			return fmt.Errorf("truth projection finding %s is missing", id)
		}
		destination := ""
		for candidate, digest := range job.ProjectionDigests {
			if digest == job.TruthDigests[id] && strings.HasPrefix(candidate, "docs/truth/") {
				destination = candidate
				break
			}
		}
		if destination == "" {
			return fmt.Errorf("truth projection %s has no destination", id)
		}
		if requested := request.ProjectionDestinations[id]; requested != "" && requested != destination {
			return fmt.Errorf("truth projection %s destination changed", id)
		}
		projection, err := BuildTruthProjection(service.Boundary, finding, destination)
		if err != nil {
			return err
		}
		if projection.ContentDigest != job.TruthDigests[id] {
			return fmt.Errorf("truth projection %s no longer reproduces", id)
		}
	}
	return nil
}

func (service *Service) prepareClosureArchive(
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ArchiveManifest, []StateArtifactWrite, error) {
	if graph.ClosureJob.Status != "verified" {
		return ArchiveManifest{}, nil, errors.New("closure archive requires a verified projection stage")
	}
	if err := service.verifyClosureProjections(graph, request); err != nil {
		return ArchiveManifest{}, nil, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return ArchiveManifest{}, nil, err
	}
	if head.Revision != request.ExpectedHeadRevision || head.Digest != request.ExpectedHeadDigest {
		return ArchiveManifest{}, nil, ErrStateConflict
	}
	files := map[string]string{}
	bodies := map[string][]byte{}
	for _, relative := range requiredArchiveRecordPaths(graph) {
		source := path.Join("active", request.CampaignSlug, relative)
		body, digest, err := readArchiveSource(service.Boundary, store, source)
		if err != nil {
			return ArchiveManifest{}, nil, fmt.Errorf("archive source %s: %w", source, err)
		}
		files[relative], bodies[relative] = digest, body
	}
	if err := collectRetainedRunFiles(service.Boundary, graph, files, bodies); err != nil {
		return ArchiveManifest{}, nil, err
	}
	manifest, err := BuildArchiveManifest(
		graph, *graph.ClosureJob, head.EventID, request.Timestamp,
		files, graph.ClosureJob.ProjectionDigests,
	)
	if err != nil {
		return ArchiveManifest{}, nil, err
	}
	relatives := make([]string, 0, len(bodies))
	for relative := range bodies {
		relatives = append(relatives, relative)
	}
	sort.Strings(relatives)
	artifacts := make([]StateArtifactWrite, 0, len(relatives))
	for _, relative := range relatives {
		destination := path.Join(graph.ClosureJob.ArchiveDestination, relative)
		artifacts = append(artifacts, StateArtifactWrite{
			Path: destination, ExpectedDigest: request.ExpectedArtifactDigests[destination],
			ContentDigest: files[relative], Body: bodies[relative],
		})
	}
	return manifest, artifacts, nil
}

func readArchiveSource(
	boundary Boundary,
	store *StateStore,
	relative string,
) ([]byte, string, error) {
	if strings.HasSuffix(relative, "/events/events.jsonl") {
		absolute, err := boundary.Resolve(relative, true)
		if err != nil {
			return nil, "", err
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return nil, "", err
		}
		return body, "sha256:" + SHA256Bytes(body), nil
	}
	body, handle, err := store.ReadCanonicalRecord(relative)
	if err != nil {
		return nil, "", err
	}
	return body, handle.ContentDigest, nil
}

func collectRetainedRunFiles(
	boundary Boundary,
	graph CampaignGraph,
	files map[string]string,
	bodies map[string][]byte,
) error {
	ids := make([]string, 0, len(graph.Runs))
	for id := range graph.Runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		run := graph.Runs[id]
		include := map[string]string{}
		runPrefix := path.Join("active", graph.Campaign.Slug, "runs", id) + "/"
		for _, handle := range []*FileHandle{run.Brief, run.ContextPack, run.Report} {
			if handle != nil {
				if !strings.HasPrefix(handle.Path, runPrefix) {
					return fmt.Errorf("run %s handle %s is outside its canonical run directory", id, handle.Path)
				}
				relative := strings.TrimPrefix(handle.Path, runPrefix)
				if validateRelativeRecordPath(relative) != nil {
					return fmt.Errorf("run %s handle %s is not archiveable", id, handle.Path)
				}
				include[relative] = handle.SHA256
			}
		}
		for _, file := range run.Files {
			disposition := graph.ClosureCoverage.FileRetention[id+":"+file.Path]
			if disposition == "retained-inline" || disposition == "distilled-and-retained" {
				if existing := include[file.Path]; existing != "" && existing != file.SHA256 {
					return fmt.Errorf("run %s file %s has conflicting registered digests", id, file.Path)
				}
				include[file.Path] = file.SHA256
			}
		}
		paths := make([]string, 0, len(include))
		for relative := range include {
			paths = append(paths, relative)
		}
		sort.Strings(paths)
		for _, relative := range paths {
			source := path.Join("active", graph.Campaign.Slug, "runs", id, relative)
			absolute, err := boundary.Resolve(source, true)
			if err != nil {
				return err
			}
			body, err := readSingleLinkRegularFile(absolute)
			if err != nil {
				return err
			}
			digest := "sha256:" + SHA256Bytes(body)
			if digest != include[relative] {
				return fmt.Errorf("run %s archived file %s changed from its registered digest", id, relative)
			}
			archiveRelative := path.Join("runs", id, relative)
			files[archiveRelative] = digest
			bodies[archiveRelative] = body
		}
	}
	return nil
}

func (service *Service) prepareClosureFinalization(
	store *StateStore,
	graph CampaignGraph,
	next ClosureJob,
	request ClosureApplyRequest,
) (CampaignRecord, ClosureReceipt, []StateArtifactWrite, error) {
	manifestPath := path.Join(graph.ClosureJob.ArchiveDestination, "manifest.json")
	_, value, handle, err := store.readCanonicalRecordValue(manifestPath)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, nil, err
	}
	manifest, ok := value.(ArchiveManifest)
	if !ok || handle.RecordDigest != graph.ClosureJob.ArchiveDigest || manifest.CampaignID != request.CampaignID {
		return CampaignRecord{}, ClosureReceipt{}, nil, errors.New("closure archive manifest does not match the active job")
	}
	if err := verifyArchivePublication(service.Boundary, graph.ClosureJob.ArchiveDestination, manifest); err != nil {
		return CampaignRecord{}, ClosureReceipt{}, nil, err
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.UpdatedAt, campaign.UpdatedBy, campaign.CorrelationID, campaign.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	campaign.Status, campaign.ClosedAt, campaign.ClosingAt = "closed", request.Timestamp, ""
	campaign.ArchiveDestination = graph.ClosureJob.ArchiveDestination
	predictedEvent := eventIDForRequest(StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.finalize",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest,
	})
	receipt := ClosureReceipt{
		SchemaVersion: CampaignSchemaVersion, CampaignID: request.CampaignID,
		ClosureJobID: next.ID, CampaignRevision: campaign.Revision,
		StateHeadRevision: request.ExpectedHeadRevision + 1, EventID: predictedEvent,
		ArchiveDestination: graph.ClosureJob.ArchiveDestination,
		ArchiveDigest:      graph.ClosureJob.ArchiveDigest,
		TruthDigests:       cloneStringMap(graph.ClosureJob.TruthDigests),
		CoverageDigest:     graph.ClosureCoverage.Digest, ClosedAt: request.Timestamp,
	}
	if err := sealClosureReceipt(&receipt); err != nil {
		return CampaignRecord{}, ClosureReceipt{}, nil, err
	}
	receiptBody, err := canonicalJSON(receipt)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, nil, err
	}
	readme := renderArchiveREADME(campaign, next, receipt)
	paths := []struct {
		path string
		body []byte
	}{
		{path.Join(graph.ClosureJob.ArchiveDestination, "closure", "receipt.json"), receiptBody},
		{path.Join(graph.ClosureJob.ArchiveDestination, "README.md"), readme},
	}
	artifacts := make([]StateArtifactWrite, 0, len(paths))
	for _, item := range paths {
		artifacts = append(artifacts, StateArtifactWrite{
			Path: item.path, ExpectedDigest: request.ExpectedArtifactDigests[item.path],
			ContentDigest: "sha256:" + SHA256Bytes(item.body), Body: item.body,
		})
	}
	return campaign, receipt, artifacts, nil
}

func verifyArchivePublication(boundary Boundary, destination string, manifest ArchiveManifest) error {
	for relative, expected := range manifest.Files {
		absolute, err := boundary.Resolve(path.Join(destination, relative), true)
		if err != nil {
			return err
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return err
		}
		if got := "sha256:" + SHA256Bytes(body); got != expected {
			return fmt.Errorf("archive file %s digest changed", relative)
		}
	}
	for projection, expected := range manifest.Projections {
		absolute, err := boundary.Resolve(projection, true)
		if err != nil {
			return err
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return err
		}
		if got := "sha256:" + SHA256Bytes(body); got != expected {
			return fmt.Errorf("projection %s digest changed", projection)
		}
	}
	return nil
}

func renderArchiveREADME(campaign CampaignRecord, job ClosureJob, receipt ClosureReceipt) []byte {
	var output strings.Builder
	output.WriteString("# Campaign Archive: ")
	output.WriteString(campaign.Title)
	output.WriteString("\n\n")
	fmt.Fprintf(&output, "Campaign: `%s`\n\n", campaign.ID)
	fmt.Fprintf(&output, "Closed: `%s`\n\n", receipt.ClosedAt)
	fmt.Fprintf(&output, "Closure receipt: `%s`\n\n", receipt.Digest)
	output.WriteString("## Purpose And Outcome\n\n")
	output.WriteString(campaign.Objective)
	output.WriteString("\n\n## Durable Destinations\n\n")
	destinations := make([]string, 0, len(job.ProjectionDigests))
	for destination := range job.ProjectionDigests {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)
	if len(destinations) == 0 {
		output.WriteString("- No external projections.\n")
	} else {
		for _, destination := range destinations {
			fmt.Fprintf(&output, "- `%s` (`%s`)\n", destination, job.ProjectionDigests[destination])
		}
	}
	output.WriteString("\n## Query And Reproduction\n\n")
	output.WriteString("Use `manifest.json` and exact record handles to expand findings, reviews, runs, evidence, and closure coverage. This README is a generated navigation view.\n")
	return []byte(output.String())
}

func sealClosureJobForComparison(job *ClosureJob) error {
	job.Digest = ""
	digest, err := CanonicalDigest(*job)
	if err != nil {
		return err
	}
	job.Digest = digest
	return ValidateClosureJob(*job)
}

func sealClosureApplyResult(result ClosureApplyResult) (ClosureApplyResult, error) {
	result.Digest = ""
	digest, err := CanonicalDigest(result)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result.Digest = digest
	return result, nil
}
