package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	// ExpectedClosurePlanRevision is the caller's assertion of the campaignRevision
	// carried by the plan it intends to replace. The digest compare-and-swap on
	// ExpectedRecordDigests["closure-plan"] already makes the swap safe; this field
	// exists so a restart cannot be issued by a caller that never read the plan it
	// is destroying, and so the refusal can name the number found rather than
	// returning a bare digest mismatch.
	ExpectedClosurePlanRevision int64             `json:"expectedClosurePlanRevision,omitempty"`
	FileRetention               map[string]string `json:"fileRetention,omitempty"`
	ActiveFileDispositions      map[string]string `json:"activeFileDispositions,omitempty"`
	ExportedWorkItemIDs         []string          `json:"exportedWorkItemIds,omitempty"`
	ProjectionDestinations      map[string]string `json:"projectionDestinations,omitempty"`
	TokenBudget                 int               `json:"tokenBudget,omitempty"`
}

// ClosureActions is the complete closure surface. mcp.go builds closure_apply's
// action enum from this list; an engine transition that no caller can name is
// not a capability.
var ClosureActions = []string{
	"start", "status", "advance", "reopen", "restart", "verify", "finalize",
}

func closureActionRebasesHead(action string) bool {
	return validOne(action, "start", "advance", "reopen", "restart", "verify")
}

type ClosureApplyResult struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Action        string                   `json:"action"`
	State         *StateView               `json:"state,omitempty"`
	Transaction   *StateTransactionReceipt `json:"transaction,omitempty"`
	Plan          *ClosurePlan             `json:"plan,omitempty"`
	Job           *ClosureJob              `json:"job,omitempty"`
	// Coverage is deliberately absent, and this comment is here so it stays
	// absent. It used to sit at this level as well as on Job, so every mutating
	// closure response serialized the same map twice -- including a complete
	// activeFileDispositions, which runs to dozens of entries on a real
	// campaign. Job.Coverage is the canonical copy: it is a field of the
	// closure job record itself, sealed into that record's digest and written
	// to disk, whereas this one was pure response echo. Every construction site
	// set the two from the same value, so nothing was lost by dropping it.
	// Read coverage from result.Job.Coverage.
	Receipt *ClosureReceipt  `json:"receipt,omitempty"`
	Archive *ArchiveManifest `json:"archive,omitempty"`
	Digest  string           `json:"digest"`
	// Omitted names every derived section a requested tokenBudget dropped.
	// Unlike StateTransactionReceipt.Digest, this result's Digest is a pure
	// response seal - nothing on disk carries it and nothing re-authenticates
	// it - so a budgeted result is re-sealed over exactly the body that was
	// returned and stays self-consistent. The record digests *inside* it
	// (Job.Digest, Plan.Digest, Archive.Digest) are the opposite: they are
	// on-disk identities and compare-and-swap inputs, so they are never
	// recomputed and never dropped, even when the record around them is
	// trimmed.
	Omitted []string `json:"omitted,omitempty"`
}

// ClosureApply is the whole closure surface. It budgets its own response rather
// than delegating to a wrapper because `status` is a read that already has a
// budgeted projection underneath it, while the mutating actions are receipts
// whose droppable sections are named in budgetClosureApplyResult.
func (service *Service) ClosureApply(ctx context.Context, request ClosureApplyRequest) (ClosureApplyResult, error) {
	if service == nil {
		return ClosureApplyResult{}, errors.New("service is required")
	}
	if !validOne(request.Action, ClosureActions...) {
		return ClosureApplyResult{}, fmt.Errorf(
			"unsupported closure action %q; closure_apply accepts %s",
			request.Action, strings.Join(ClosureActions, ", "))
	}
	if err := validateMutationTokenBudget("closure_apply", request.TokenBudget); err != nil {
		return ClosureApplyResult{}, err
	}
	if request.Action == "status" {
		// status compiles a bounded state view, so a tokenBudget here belongs to
		// that projection rather than to the closure envelope around it. Passing
		// it down means one budget with one meaning instead of two that could
		// disagree.
		view, err := service.State(ctx, StateRequest{
			Mode: "closure", CampaignID: request.CampaignID, TokenBudget: request.TokenBudget,
		})
		if err != nil {
			return ClosureApplyResult{}, err
		}
		result := ClosureApplyResult{SchemaVersion: CampaignSchemaVersion, Action: request.Action, State: &view}
		return sealClosureApplyResult(result)
	}
	// Name the fields that are actually missing - all of them, including the two
	// start-only ones that used to be checked several calls deeper with the
	// campaign-status and existing-job gates between them. See
	// collectClosureRequestShape in mutations_shape.go.
	shape := newRefusalSet("closure_apply " + request.Action)
	collectClosureRequestShape(shape, request)
	if err := shape.result(); err != nil {
		return ClosureApplyResult{}, err
	}
	// Parsed rather than carried out of the shape pass: the pass proved this
	// exact string is a UTC RFC3339 instant, so the parse cannot fail here, and
	// threading a time.Time out of a validator would let the two drift.
	timestamp, err := time.Parse(time.RFC3339Nano, request.Timestamp)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	store.Now = func() time.Time { return timestamp }
	if err := store.Recover(ctx); err != nil {
		return ClosureApplyResult{}, err
	}
	if closureActionRebasesHead(request.Action) {
		revision, digest, resolveErr := resolveOrdinaryPublicationHead(
			store, request.ExpectedHeadRevision, request.ExpectedHeadDigest, nil)
		if resolveErr != nil {
			return ClosureApplyResult{}, resolveErr
		}
		request.ExpectedHeadRevision = revision
		request.ExpectedHeadDigest = digest
	}
	if request.Action == "finalize" {
		if replay, found, err := replayRetiredClosureFinalization(store, request); err != nil {
			return ClosureApplyResult{}, err
		} else if found {
			return budgetClosureApplyResult(replay, request.TokenBudget)
		}
	}
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if graph.Campaign.Slug != request.CampaignSlug || !containsString(graph.Campaign.PermittedManagers, request.Actor) {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure %s refused: campaign %s is slug %q with permitted managers %s, and the request "+
				"named slug %q and actor %q. Remedy: re-issue with the canonical slug and an actor "+
				"already on that list; manager_apply campaign.update is the only transition that "+
				"changes permittedManagers",
			request.Action, graph.Campaign.ID, graph.Campaign.Slug,
			strings.Join(graph.Campaign.PermittedManagers, ", "),
			request.CampaignSlug, request.Actor)
	}
	var result ClosureApplyResult
	switch request.Action {
	case "start":
		result, err = service.startClosure(ctx, store, graph, request)
	case "advance", "verify", "finalize":
		result, err = service.advanceClosure(ctx, store, graph, request)
	case "reopen":
		result, err = service.reopenClosure(ctx, store, graph, request)
	case "restart":
		result, err = service.restartClosure(ctx, store, graph, request)
	default:
		return ClosureApplyResult{}, fmt.Errorf(
			"closure action %q has no transition; the mutating closure actions are %s",
			request.Action, strings.Join(ClosureActions[1:], ", "))
	}
	if err != nil {
		return ClosureApplyResult{}, err
	}
	return budgetClosureApplyResult(result, request.TokenBudget)
}

// budgetClosureApplyResult drops the derived sections of a closure receipt in a
// fixed least-useful-first order and re-seals what remains.
//
// The floor is everything the next closure_apply call needs:
// Transaction.Records supplies every expectedRecordDigests entry, while both
// heads remain durable audit identity and finalization proof. Job.Stage,
// Job.Status, Job.Revision, and Job.Digest say which stage may be targeted next
// and with which digest; Job.Blockers is the actionable summary of why an
// advance was refused. Plan.CampaignRevision and Plan.Digest are restart's
// expectedClosurePlanRevision and expectedRecordDigests entry. None of those
// are droppable.
//
// What goes is bulk that the caller either already holds or can re-read:
// ArchiveManifest.Coverage is a whole second copy of the coverage record;
// Files and Projections carry one row per archived file, which on a real
// campaign runs to hundreds; Job.Coverage is the same five maps Blockers
// already summarizes; Plan's five requirement lists are re-derivable from the
// campaign; Transaction.Artifacts is one row per published file.
func budgetClosureApplyResult(result ClosureApplyResult, budget int) (ClosureApplyResult, error) {
	if budget <= 0 {
		return result, nil
	}
	sections := []budgetSection{
		{
			Name: "archive.coverage",
			Note: "the archive manifest's embedded closure-coverage copy; read it from " +
				"closure_apply action=status",
			Present: func() bool { return result.Archive != nil },
			Drop:    func() { result.Archive.Coverage = ClosureCoverage{} },
		},
		{
			Name: "archive.files",
			Note: "one digest row per archived file; read manifest.json under the archive " +
				"destination, whose digest is archive.digest",
			Present: func() bool { return result.Archive != nil && len(result.Archive.Files) != 0 },
			Drop:    func() { result.Archive.Files = nil },
		},
		{
			Name: "archive.projections",
			Note: "one digest row per published durable projection; read manifest.json under " +
				"the archive destination",
			Present: func() bool { return result.Archive != nil && len(result.Archive.Projections) != 0 },
			Drop:    func() { result.Archive.Projections = nil },
		},
		{
			Name: "job.coverage",
			Note: "the derived coverage maps; job.blockers already names every unmet gate, " +
				"and closure_apply action=status returns the full coverage record",
			Present: func() bool { return result.Job != nil && result.Job.Coverage != nil },
			Drop:    func() { result.Job.Coverage = nil },
		},
		{
			Name: "transaction.artifacts",
			Note: "one row per file this transaction published; no closure input reads them",
			Present: func() bool {
				return result.Transaction != nil && len(result.Transaction.Artifacts) != 0
			},
			Drop: func() {
				result.Transaction.Artifacts = nil
				// Mark the embedded receipt itself, not only the envelope, so a
				// caller that passes it to verifyTransactionReceipt is told it
				// is a budgeted copy rather than that its digest failed.
				result.Transaction.Omitted = []string{
					"artifacts omitted under tokenBudget: one row per file this transaction " +
						"published; no closure input reads them",
				}
			},
		},
		{
			Name: "plan.requirements",
			Note: "the plan's projection, run, work-item, finding, and retention id lists; " +
				"plan.campaignRevision and plan.digest are kept because restart requires them, " +
				"and closure_apply action=status returns the lists",
			Present: func() bool { return result.Plan != nil },
			Drop: func() {
				plan := *result.Plan
				plan.ProjectionFindingIDs, plan.RequiredRunIDs = nil, nil
				plan.RequiredWorkItemIDs, plan.RequiredFindingIDs = nil, nil
				plan.RequiredRetentionPaths = nil
				result.Plan = &plan
			},
		},
	}
	// Archive, Job, and Plan are pointers into caller-visible values that other
	// code still holds, so copy before trimming: a budgeted response must never
	// mutate the job or manifest the engine just built.
	if result.Archive != nil {
		archive := *result.Archive
		result.Archive = &archive
	}
	if result.Job != nil {
		job := *result.Job
		result.Job = &job
	}
	if result.Transaction != nil {
		transaction := *result.Transaction
		result.Transaction = &transaction
	}
	omitted, err := applyResponseBudget(budget, &result, sections)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result.Omitted = omitted
	return sealClosureApplyResult(result)
}

func (service *Service) startClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	if graph.ClosureJob != nil {
		// Name the remedy. Before restart existed this refusal was terminal for any
		// campaign that had reopened: the reopened job stays on disk, start refuses
		// while it is there, and no canonical record has a delete path. Callers
		// hand-edited state to escape.
		if graph.ClosureJob.Status == "reopened" {
			return ClosureApplyResult{}, errors.New(
				"this campaign has a reopened closure job; re-enter closure with action \"restart\", " +
					"which re-plans against the current campaign revision")
		}
		return ClosureApplyResult{}, fmt.Errorf(
			"closure job %s already exists on this campaign at stage %q, status %q. Remedy: "+
				"closure_apply action=status to see where it stands, then advance (or verify, "+
				"or finalize) to the next stage, or reopen to return the campaign to open",
			graph.ClosureJob.ID, graph.ClosureJob.Stage, graph.ClosureJob.Status)
	}
	if !validOne(graph.Campaign.Status, "open", "paused") {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure can start only on an open or paused campaign, and %s is %q. %s",
			graph.Campaign.ID, graph.Campaign.Status,
			closureCampaignStatusRemedy(graph.Campaign.Status))
	}
	if !correlationIDRE.MatchString(request.ClosureJobID) {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure start requires closureJobId matching %s; it is %q. It is caller-chosen and "+
				"permanent - the job keeps this id across reopen and restart, and every closure "+
				"transition after this one names it",
			correlationIDRE.String(), request.ClosureJobID)
	}
	if err := validateExportedDefermentIDs(graph, request.ExportedWorkItemIDs); err != nil {
		return ClosureApplyResult{}, err
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
	if err := service.applyClosureActiveFileInventory(
		graph, request.CampaignSlug, prior.ActiveFileDispositions, &coverage,
	); err != nil {
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
	if _, err := service.queueClosureNormalization(graph, request.Timestamp); err != nil {
		return ClosureApplyResult{}, fmt.Errorf("queue closure normalization: %w", err)
	}
	receipt, err := store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.start",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest,
		RebaseHead:         closureActionRebasesHead(request.Action), Writes: writes,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: &plan, Job: &job,
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
		if graph.ClosureJob != nil && graph.ClosureJob.Status == "reopened" {
			return ClosureApplyResult{}, fmt.Errorf(
				"closure %s cannot run against reopened job %s; a reopen returned the campaign "+
					"to %q and discarded the attempt's staged work. Remedy: closure_apply "+
					"action=restart, which re-plans against the current campaign revision and "+
					"requires expectedClosurePlanRevision plus the prior campaign, plan, job, "+
					"and coverage digests",
				request.Action, graph.ClosureJob.ID, graph.Campaign.Status)
		}
		if graph.ClosureJob == nil {
			return ClosureApplyResult{}, fmt.Errorf(
				"closure %s requires an active closure job, and campaign %s has none. Remedy: "+
					"closure_apply action=start with a caller-chosen closureJobId and an "+
					"archiveDestination",
				request.Action, graph.Campaign.ID)
		}
		return ClosureApplyResult{}, fmt.Errorf(
			"closure %s requires a complete active closure job on a closing campaign: campaign "+
				"%s is %q, and the job, plan, and coverage records must all be present. "+
				"closure_apply action=status reports which of them the campaign carries",
			request.Action, graph.Campaign.ID, graph.Campaign.Status)
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
		// Closure stages are strictly sequential and there is exactly one legal
		// next value, so naming it turns this from a guessing game into a copy.
		return ClosureApplyResult{}, fmt.Errorf(
			"closure is at stage %q and targetStage %q is not the next one; %s",
			graph.ClosureJob.Stage, target, closureNextStageRemedy(graph.ClosureJob.Stage))
	}
	if err := validateExportedDefermentIDs(graph, request.ExportedWorkItemIDs); err != nil {
		return ClosureApplyResult{}, err
	}
	if to > closureStageIndex["project"] && len(request.ExportedWorkItemIDs) != 0 {
		return ClosureApplyResult{}, fmt.Errorf(
			"exportedWorkItemIds may not be declared while advancing to %q: the project stage "+
				"renders every exported backlog item into a durable file, and verify re-derives "+
				"those files, so an export declared afterwards would be sealed into the archive "+
				"without ever having been projected. This closure already passed project. "+
				"Remedy: drop exportedWorkItemIds from this request; to add one, closure_apply "+
				"action=reopen then action=restart and declare it at or before project",
			target)
	}
	prior := cloneClosureCoverage(*graph.ClosureCoverage)
	for key, value := range request.FileRetention {
		prior.FileRetention[key] = value
	}
	for key, value := range request.ActiveFileDispositions {
		prior.ActiveFileDispositions[key] = value
	}
	for _, id := range request.ExportedWorkItemIDs {
		prior.WorkItemCoverage[id] = "exported-backlog"
	}
	workingGraph := graph
	projected := false
	extraWrites := []closureWriteSpec{}
	promotionWrites := []closureWriteSpec{}
	if target == "project" {
		projectedGraph, projectionFindingWrites, transitionErr := service.prepareClosureFindingTransitions(store, graph, request)
		if transitionErr != nil {
			return ClosureApplyResult{}, transitionErr
		}
		workingGraph = projectedGraph
		promotionWrites = projectionFindingWrites
		projected = true
	} else if from >= closureStageIndex["project"] {
		projectedGraph, _, stagingErr := service.stagedClosureGraph(graph)
		if stagingErr != nil {
			return ClosureApplyResult{}, stagingErr
		}
		workingGraph = projectedGraph
		projected = true
	}
	// The canonical graph is the one the coherence invariants are about, so it is
	// validated here; the projection built from it is classified without being
	// re-validated. See computeClosureCoverageProjected.
	var coverage ClosureCoverage
	var err error
	if projected {
		if err = graph.Validate(); err != nil {
			return ClosureApplyResult{}, err
		}
		coverage, err = computeClosureCoverageProjected(workingGraph, &prior)
	} else {
		coverage, err = ComputeClosureCoverage(workingGraph, &prior)
	}
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if err := service.applyClosureActiveFileInventory(
		graph, request.CampaignSlug, prior.ActiveFileDispositions, &coverage,
	); err != nil {
		return ClosureApplyResult{}, err
	}
	if _, err := service.queueClosureNormalization(graph, request.Timestamp); err != nil {
		return ClosureApplyResult{}, fmt.Errorf("queue closure normalization: %w", err)
	}
	normalizationBlockers := []string{}
	if closureStageIndex[target] >= closureStageIndex["reconcile"] {
		normalizationBlockers, err = service.closureNormalizationBlockers(graph)
		if err != nil {
			return ClosureApplyResult{}, fmt.Errorf("verify closure normalization: %w", err)
		}
	}
	if target == "finalize" && coverage.Digest != graph.ClosureCoverage.Digest {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure finalization recomputed coverage %s against the verified %s: something "+
				"changed after verify, so the archive would freeze coverage nobody verified. "+
				"Outstanding blockers: %s. Remedy: closure_apply action=reopen then "+
				"action=restart to re-plan and re-verify against current state; a finalize "+
				"cannot absorb a coverage change",
			coverage.Digest, graph.ClosureCoverage.Digest,
			blockerListOrNone(closureCoverageBlockers(coverage)),
		)
	}
	if target == "finalize" && len(normalizationBlockers) != 0 {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure finalization cannot retire unresolved normalization work: %s. Each entry is "+
				"normalization:<runId>:<state>. Remedy: for every one, work the queue item to "+
				"resolved - normalization_queue action=status to find it, then claim, ack, and "+
				"resolve with a receipt binding the curator run, reviewed intake, and manager "+
				"review that covered that run's report - then finalize again",
			strings.Join(normalizationBlockers, ", "),
		)
	}
	next := *graph.ClosureJob
	next.Revision++
	next.UpdatedAt, next.UpdatedBy, next.CorrelationID, next.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	next.Stage, next.Status, next.Coverage = target, "running", &coverage
	next.Blockers = SortedUnique(append(
		closureCoverageBlockers(coverage), normalizationBlockers...))
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
		truthDigests, projectionDigests, projectArtifacts, err := service.prepareClosureProjections(workingGraph, graph, coverage, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		staging, err := service.stageClosureProjections(
			graph, workingGraph, truthDigests, projectionDigests,
			projectArtifacts, promotionWrites, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		next.TruthDigests = truthDigests
		next.ProjectionDigests = projectionDigests
		next.StagingDigest = staging.Digest
	case "verify":
		action = "closure.verify"
		if err := service.verifyClosureProjections(ctx, graph, request); err != nil {
			return ClosureApplyResult{}, err
		}
		next.Status = "verified"
	case "archive":
		action = "closure.archive"
		manifest, staging, err := service.prepareClosureArchive(ctx, store, graph, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		next.ArchiveDigest = manifest.Digest
		next.StagingDigest = staging.Digest
		archive = &manifest
	case "finalize":
		action = "closure.finalize"
		next.Status = "completed"
		next.StagingDigest = ""
		campaign, receipt, manifest, finalizeArtifacts, err := service.prepareClosureFinalization(ctx, store, graph, next, request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		finalReceipt = &receipt
		archive = &manifest
		artifacts = append(artifacts, finalizeArtifacts...)
		extraWrites = append(extraWrites,
			closureWriteSpec{value: manifest, path: path.Join(graph.ClosureJob.ArchiveDestination, "manifest.json")},
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
	transaction := StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: action,
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest,
		RebaseHead:         closureActionRebasesHead(request.Action),
		Writes:             writes, Artifacts: artifacts,
	}
	if target == "finalize" {
		snapshots, err := closureFinalizationSnapshotArtifacts(request, writes, graph.ClosureJob.ArchiveDestination)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		transaction.Artifacts = append(transaction.Artifacts, snapshots...)
		replayBody, err := canonicalJSON(request)
		if err != nil {
			return ClosureApplyResult{}, err
		}
		replayPath := path.Join(graph.ClosureJob.ArchiveDestination, "finalization", "request.json")
		transaction.Artifacts = append(transaction.Artifacts, StateArtifactWrite{
			Path: replayPath, ExpectedDigest: request.ExpectedArtifactDigests[replayPath],
			ContentDigest: "sha256:" + SHA256Bytes(replayBody), Body: replayBody,
		})
		transaction.RetireActiveTree = path.Join("active", request.CampaignSlug)
		transaction.RetiredEventJournal = path.Join(
			graph.ClosureJob.ArchiveDestination, "finalization", "events", "events.jsonl")
	}
	receipt, err := store.Apply(ctx, transaction)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if target == "finalize" {
		// The immutable transaction is already committed. Staging is derived and
		// intentionally best-effort to clean; a crash here leaves only an inert,
		// unindexed cache that the replay path can discard later.
		_ = discardClosureStaging(service.Boundary, graph.Campaign.ID, graph.ClosureJob.ID)
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: graph.ClosurePlan, Job: &next,
		Receipt: finalReceipt, Archive: archive,
	}
	return sealClosureApplyResult(result)
}

func closureFinalizationSnapshotArtifacts(
	request ClosureApplyRequest,
	writes []StateWrite,
	archiveDestination string,
) ([]StateArtifactWrite, error) {
	predictedEvent := eventIDForRequest(StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.finalize",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest,
	})
	result := []StateArtifactWrite{}
	for _, write := range writes {
		name := ""
		switch record := write.Record.(type) {
		case CampaignRecord:
			name = "campaign.json"
			record.LastEventID = predictedEvent
			write.Record = record
		case ClosureJob:
			name = "closure-job.json"
		default:
			continue
		}
		prepared, err := prepareStateWrite(
			request.CampaignSlug, request.CampaignID, request.CorrelationID,
			predictedEvent, request.ExpectedHeadRevision+1, write)
		if err != nil {
			return nil, err
		}
		destination := path.Join(archiveDestination, "finalization", name)
		result = append(result, StateArtifactWrite{
			Path: destination, ExpectedDigest: request.ExpectedArtifactDigests[destination],
			ContentDigest: prepared.ContentDigest, Body: prepared.Body,
		})
	}
	if len(result) != 2 {
		return nil, errors.New("closure finalization must snapshot its closed campaign and completed job")
	}
	return result, nil
}

func replayRetiredClosureFinalization(
	store *StateStore,
	request ClosureApplyRequest,
) (ClosureApplyResult, bool, error) {
	receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		return ClosureApplyResult{}, false, err
	}
	retiredTree := path.Join("active", request.CampaignSlug)
	if receipt.RetiredTree != retiredTree || receipt.Event.Action != "closure.finalize" ||
		receipt.Event.Actor != request.Actor || receipt.Event.Authority != "manager" ||
		receipt.CorrelationID != request.CorrelationID || receipt.Event.CorrelationID != request.CorrelationID ||
		receipt.Event.Timestamp != request.Timestamp || receipt.Event.Rationale != request.Rationale ||
		receipt.PreviousHead.Revision != request.ExpectedHeadRevision ||
		receipt.PreviousHead.Digest != request.ExpectedHeadDigest {
		return ClosureApplyResult{}, false, ErrIdempotencyConflict
	}
	activePath, err := store.canonicalOutputPath(retiredTree)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	if _, err := os.Lstat(activePath); err == nil || !os.IsNotExist(err) {
		return ClosureApplyResult{}, false, fmt.Errorf("%w: committed closure retirement left an active tree", ErrStateDirty)
	}

	archiveDestination := ""
	for _, artifact := range receipt.Artifacts {
		if strings.HasSuffix(artifact.Path, "/closure/receipt.json") {
			if archiveDestination != "" {
				return ClosureApplyResult{}, false, ErrStateDirty
			}
			archiveDestination = strings.TrimSuffix(artifact.Path, "/closure/receipt.json")
		}
	}
	if archiveDestination == "" || validateArchiveDestination(archiveDestination) != nil ||
		validateRetiredEventJournal(path.Join(archiveDestination, "finalization", "events", "events.jsonl")) != nil {
		return ClosureApplyResult{}, false, ErrStateDirty
	}
	var closureReceipt ClosureReceipt
	_, err = loadRetiredClosureRecord(
		store, path.Join(archiveDestination, "closure", "receipt.json"), &closureReceipt)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	var campaign CampaignRecord
	_, err = loadRetiredClosureRecord(
		store, path.Join(archiveDestination, "finalization", "campaign.json"), &campaign)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	var job ClosureJob
	_, err = loadRetiredClosureRecord(
		store, path.Join(archiveDestination, "finalization", "closure-job.json"), &job)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	if campaign.ID != request.CampaignID || campaign.Slug != request.CampaignSlug ||
		campaign.Status != "closed" || job.CampaignID != request.CampaignID ||
		job.Status != "completed" || closureReceipt.CampaignID != request.CampaignID ||
		closureReceipt.ArchiveDestination != archiveDestination {
		return ClosureApplyResult{}, false, ErrStateDirty
	}
	var manifest ArchiveManifest
	_, err = loadRetiredClosureRecord(
		store, path.Join(archiveDestination, "manifest.json"), &manifest)
	if err != nil || manifest.Digest != closureReceipt.ArchiveDigest ||
		manifest.CampaignID != closureReceipt.CampaignID ||
		manifest.Coverage.Digest != closureReceipt.CoverageDigest {
		return ClosureApplyResult{}, false, fmt.Errorf("%w: retired archive manifest does not bind the closure receipt", ErrStateDirty)
	}
	if err := authenticateRetiredClosureArtifacts(store, receipt, archiveDestination, manifest); err != nil {
		return ClosureApplyResult{}, false, err
	}
	if err := authenticateRetiredArchiveManifest(store.Boundary, archiveDestination, manifest); err != nil {
		return ClosureApplyResult{}, false, err
	}
	if err := authenticateRetiredClosureEventJournal(store, receipt, archiveDestination, manifest); err != nil {
		return ClosureApplyResult{}, false, err
	}
	replayPath := path.Join(archiveDestination, "finalization", "request.json")
	replayBody, err := loadRetiredClosureArtifact(store, replayPath)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	wantReplayBody, err := canonicalJSON(request)
	if err != nil {
		return ClosureApplyResult{}, false, err
	}
	if string(replayBody) != string(wantReplayBody) {
		return ClosureApplyResult{}, false, ErrIdempotencyConflict
	}
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Job: &job, Receipt: &closureReceipt,
		Archive: &manifest,
	}
	_ = discardClosureStaging(store.Boundary, job.CampaignID, job.ID)
	sealed, err := sealClosureApplyResult(result)
	return sealed, true, err
}

func authenticateRetiredClosureArtifacts(
	store *StateStore,
	receipt StateTransactionReceipt,
	archiveDestination string,
	manifest ArchiveManifest,
) error {
	required := map[string]bool{
		path.Join(archiveDestination, "closure", "receipt.json"):          true,
		path.Join(archiveDestination, "README.md"):                        true,
		path.Join(archiveDestination, "finalization", "campaign.json"):    true,
		path.Join(archiveDestination, "finalization", "closure-job.json"): true,
		path.Join(archiveDestination, "finalization", "request.json"):     true,
	}
	expected := map[string]string{}
	for relative, digest := range manifest.Files {
		destination := path.Join(archiveDestination, relative)
		expected[destination] = digest
		required[destination] = true
	}
	for destination, digest := range manifest.Projections {
		expected[destination] = digest
		required[destination] = true
	}
	for _, navigation := range []string{
		"docs/INDEX.md", "docs/history/INDEX.md", "docs/truth/INDEX.md",
		"docs/backlog/INDEX.md", "docs/playbooks/INDEX.md",
	} {
		expected[navigation] = ""
	}
	seen := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		_, special := required[artifact.Path]
		want, allowed := expected[artifact.Path]
		if seen[artifact.Path] || (!special && !allowed) || !digestRE.MatchString(artifact.ContentDigest) ||
			(want != "" && want != artifact.ContentDigest) {
			return fmt.Errorf("%w: finalization receipt has an unexpected artifact inventory", ErrStateDirty)
		}
		seen[artifact.Path] = true
		body, err := loadRetiredClosureArtifact(store, artifact.Path)
		if err != nil || "sha256:"+SHA256Bytes(body) != artifact.ContentDigest {
			return fmt.Errorf("%w: archived artifact %s does not match its transaction receipt", ErrStateDirty, artifact.Path)
		}
	}
	for artifact := range required {
		if !seen[artifact] {
			return fmt.Errorf("%w: finalization receipt omits artifact %s", ErrStateDirty, artifact)
		}
	}
	return nil
}

func authenticateRetiredArchiveManifest(
	boundary Boundary,
	archiveDestination string,
	manifest ArchiveManifest,
) error {
	for relative, expected := range manifest.Files {
		absolute, err := boundary.Resolve(path.Join(archiveDestination, relative), true)
		if err != nil {
			return fmt.Errorf("%w: archived manifest file %s is unavailable", ErrStateDirty, relative)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != expected {
			return fmt.Errorf("%w: archived manifest file %s changed", ErrStateDirty, relative)
		}
	}
	for projection, expected := range manifest.Projections {
		absolute, err := boundary.Resolve(projection, true)
		if err != nil {
			return fmt.Errorf("%w: archived projection %s is unavailable", ErrStateDirty, projection)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != expected {
			return fmt.Errorf("%w: archived projection %s changed", ErrStateDirty, projection)
		}
	}
	allowed := map[string]bool{
		"manifest.json": true, "closure/receipt.json": true, "README.md": true,
		"finalization/campaign.json": true, "finalization/closure-job.json": true,
		"finalization/request.json": true, "finalization/events/events.jsonl": true,
	}
	for relative := range manifest.Files {
		allowed[relative] = true
	}
	if err := verifyArchiveDirectoryInventory(boundary, archiveDestination, allowed, false); err != nil {
		return fmt.Errorf("%w: %v", ErrStateDirty, err)
	}
	return nil
}

func authenticateRetiredClosureEventJournal(
	store *StateStore,
	receipt StateTransactionReceipt,
	archiveDestination string,
	manifest ArchiveManifest,
) error {
	finalRelative := path.Join(archiveDestination, "finalization", "events", "events.jsonl")
	if receipt.ResultingHead.EventJournal != finalRelative ||
		receipt.ResultingHead.EventID != receipt.Event.ID ||
		receipt.ResultingHead.EventDigest != receipt.Event.Digest {
		return fmt.Errorf("%w: transaction receipt does not bind the retired event journal", ErrStateDirty)
	}
	baseRelative := "events/events.jsonl"
	expectedBaseDigest, present := manifest.Files[baseRelative]
	if !present {
		return fmt.Errorf("%w: archive manifest omits the pre-finalization event journal", ErrStateDirty)
	}
	base, err := loadRetiredClosureArtifact(store, path.Join(archiveDestination, baseRelative))
	if err != nil || "sha256:"+SHA256Bytes(base) != expectedBaseDigest {
		return fmt.Errorf("%w: pre-finalization event journal does not verify", ErrStateDirty)
	}
	for index, line := range strings.Split(strings.TrimSpace(string(base)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event StateEvent
		if decodeStrictJSON([]byte(line), &event) != nil || verifyStateEvent(event) != nil {
			return fmt.Errorf("%w: archived event journal line %d is invalid", ErrStateDirty, index+1)
		}
	}
	chain, err := authenticatedReceiptsSinceManifestHead(
		store, receipt.PreviousHead, manifest.EventHead,
	)
	if err != nil {
		return err
	}
	if err := authenticateArchivePublicationReceipt(receipt, archiveDestination, manifest); err != nil {
		return err
	}
	expected := append([]byte(nil), base...)
	if len(expected) != 0 && expected[len(expected)-1] != '\n' {
		expected = append(expected, '\n')
	}
	for _, prior := range chain {
		if !transactionReceiptBindsCampaign(prior, receipt.RetiredTree) {
			continue
		}
		line, marshalErr := json.Marshal(prior.Event)
		if marshalErr != nil {
			return marshalErr
		}
		expected = append(expected, line...)
		expected = append(expected, '\n')
	}
	eventLine, err := json.Marshal(receipt.Event)
	if err != nil {
		return err
	}
	expected = append(expected, eventLine...)
	expected = append(expected, '\n')
	actual, err := loadRetiredClosureArtifact(store, finalRelative)
	if err != nil || string(actual) != string(expected) {
		return fmt.Errorf("%w: retired event journal bytes do not match the immutable transaction event", ErrStateDirty)
	}
	return nil
}

func authenticatedReceiptsSinceManifestHead(
	store *StateStore,
	cursor StateHead,
	eventHead string,
) ([]StateTransactionReceipt, error) {
	backward := []StateTransactionReceipt{}
	seen := map[string]bool{}
	for cursor.Revision > 0 && cursor.EventID != eventHead {
		if seen[cursor.TransactionID] || !correlationIDRE.MatchString(cursor.TransactionID) {
			return nil, fmt.Errorf("%w: archived receipt chain is cyclic or incomplete", ErrStateDirty)
		}
		seen[cursor.TransactionID] = true
		prior, found, err := loadTransactionReceiptByID(store, cursor.TransactionID)
		if err != nil {
			return nil, err
		}
		if !found || prior.ResultingHead != cursor {
			return nil, fmt.Errorf("%w: archived event is not bound to its immutable transaction receipt", ErrStateDirty)
		}
		backward = append(backward, prior)
		cursor = prior.PreviousHead
	}
	if cursor.EventID != eventHead {
		return nil, fmt.Errorf("%w: archive manifest event head is unavailable", ErrStateDirty)
	}
	for left, right := 0, len(backward)-1; left < right; left, right = left+1, right-1 {
		backward[left], backward[right] = backward[right], backward[left]
	}
	return backward, nil
}

func loadTransactionReceiptByID(
	store *StateStore,
	transactionID string,
) (StateTransactionReceipt, bool, error) {
	receiptRoot, err := store.statePath("receipts")
	if err != nil {
		return StateTransactionReceipt{}, false, err
	}
	entries, err := os.ReadDir(receiptRoot)
	if err != nil {
		return StateTransactionReceipt{}, false, err
	}
	var matched StateTransactionReceipt
	found := false
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			return StateTransactionReceipt{}, false, fmt.Errorf("%w: receipt store contains an unexpected entry", ErrStateDirty)
		}
		body, readErr := readSingleLinkRegularFile(filepath.Join(receiptRoot, entry.Name()))
		if readErr != nil {
			return StateTransactionReceipt{}, false, readErr
		}
		var candidate StateTransactionReceipt
		if decodeStrictJSON(body, &candidate) != nil ||
			authenticateTransactionReceiptEnvelope(candidate) != nil ||
			entry.Name() != SHA256String(candidate.IdempotencyKey)+".json" {
			return StateTransactionReceipt{}, false, fmt.Errorf("%w: immutable transaction receipt does not verify", ErrStateDirty)
		}
		canonical, canonicalErr := canonicalJSON(candidate)
		if canonicalErr != nil || string(canonical) != string(body) {
			return StateTransactionReceipt{}, false, fmt.Errorf("%w: immutable transaction receipt is not canonical", ErrStateDirty)
		}
		if candidate.TransactionID != transactionID {
			continue
		}
		if found {
			return StateTransactionReceipt{}, false, fmt.Errorf("%w: duplicate immutable transaction receipt", ErrStateDirty)
		}
		matched, found = candidate, true
	}
	return matched, found, nil
}

func authenticateTransactionReceiptEnvelope(receipt StateTransactionReceipt) error {
	if verifyTransactionReceipt(receipt) != nil ||
		verifyStateHead(receipt.PreviousHead) != nil ||
		verifyStateHead(receipt.ResultingHead) != nil ||
		verifyStateEvent(receipt.Event) != nil ||
		receipt.ResultingHead.TransactionID != receipt.TransactionID ||
		receipt.ResultingHead.EventID != receipt.Event.ID ||
		receipt.ResultingHead.EventDigest != receipt.Event.Digest ||
		receipt.Event.PreviousRevision != receipt.PreviousHead.Revision ||
		receipt.Event.ResultingRevision != receipt.ResultingHead.Revision ||
		receipt.Event.PreviousEventID != receipt.PreviousHead.EventID ||
		receipt.Event.PreviousStateDigest != receipt.PreviousHead.StateDigest ||
		receipt.Event.ResultingStateDigest != receipt.ResultingHead.StateDigest {
		return errors.New("transaction receipt envelope does not verify")
	}
	return nil
}

func archivePublicationReceiptMatches(
	receipt StateTransactionReceipt,
	archiveDestination string,
	manifest ArchiveManifest,
) bool {
	manifestPath := path.Join(archiveDestination, "manifest.json")
	manifestBody, err := canonicalJSON(manifest)
	if err != nil {
		return false
	}
	foundManifest := false
	for _, record := range receipt.Records {
		if record.Path == manifestPath && record.RecordID == "archive-manifest" &&
			record.RecordDigest == manifest.Digest &&
			record.ContentDigest == "sha256:"+SHA256Bytes(manifestBody) {
			foundManifest = true
		}
	}
	if !foundManifest {
		return false
	}
	expected := map[string]string{}
	for relative, digest := range manifest.Files {
		expected[path.Join(archiveDestination, relative)] = digest
	}
	for destination, digest := range manifest.Projections {
		expected[destination] = digest
	}
	for _, artifact := range receipt.Artifacts {
		if want, required := expected[artifact.Path]; required {
			if want != artifact.ContentDigest {
				return false
			}
			delete(expected, artifact.Path)
		}
	}
	return len(expected) == 0
}

func authenticateArchivePublicationReceipt(
	receipt StateTransactionReceipt,
	archiveDestination string,
	manifest ArchiveManifest,
) error {
	if !archivePublicationReceiptMatches(receipt, archiveDestination, manifest) {
		return fmt.Errorf("%w: archive manifest and files are not bound to the archive transaction receipt", ErrStateDirty)
	}
	return nil
}

func transactionReceiptBindsCampaign(receipt StateTransactionReceipt, retiredTree string) bool {
	prefix := retiredTree + "/"
	if retiredTree == "" || !strings.HasPrefix(retiredTree, "active/") {
		return false
	}
	found := false
	for _, record := range receipt.Records {
		if strings.HasPrefix(record.Path, "active/") {
			if !strings.HasPrefix(record.Path, prefix) {
				return false
			}
			found = true
		}
	}
	return found
}

func loadRetiredClosureRecord(store *StateStore, relative string, target any) ([]byte, error) {
	body, err := loadRetiredClosureArtifact(store, relative)
	if err != nil {
		return nil, err
	}
	if err := decodeStrictJSON(body, target); err != nil {
		return nil, err
	}
	_, canonical, err := sealStateRecord(targetValue(target), relative)
	if err != nil || string(canonical) != string(body) {
		return nil, errors.New("retired closure record digest or canonical encoding does not verify")
	}
	return body, nil
}

func loadRetiredClosureArtifact(store *StateStore, relative string) ([]byte, error) {
	absolute, err := store.canonicalOutputPath(relative)
	if err != nil {
		return nil, err
	}
	return readSingleLinkRegularFile(absolute)
}

func (service *Service) reopenClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	if graph.ClosureJob == nil || graph.Campaign.Status != "closing" {
		if graph.ClosureJob == nil {
			return ClosureApplyResult{}, fmt.Errorf(
				"campaign %s has no closure job to reopen. Remedy: nothing is needed - the "+
					"campaign is already %q; closure_apply action=start begins closure",
				graph.Campaign.ID, graph.Campaign.Status)
		}
		return ClosureApplyResult{}, fmt.Errorf(
			"only a closing campaign may be reopened, and %s is %q with closure job %s at status "+
				"%q. %s",
			graph.Campaign.ID, graph.Campaign.Status, graph.ClosureJob.ID,
			graph.ClosureJob.Status, closureReopenRemedy(graph.Campaign.Status, *graph.ClosureJob))
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
	job.StagingDigest, job.ArchiveDigest = "", ""
	job.TruthDigests, job.ProjectionDigests = map[string]string{}, map[string]string{}
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
		ExpectedHeadDigest: request.ExpectedHeadDigest,
		RebaseHead:         closureActionRebasesHead(request.Action), Writes: writes,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	_ = discardClosureStaging(service.Boundary, graph.Campaign.ID, graph.ClosureJob.ID)
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: graph.ClosurePlan, Job: &job,
	}
	return sealClosureApplyResult(result)
}

// restartClosure re-enters closure after a reopen. It is deliberately modelled on
// startClosure: nothing about the previous attempt is resumed, the stage returns
// to `inventory`, and the plan and coverage are recomputed from the campaign as it
// stands now. It keeps exactly two things from the previous attempt - the closure
// job's identity, because a campaign has one closure record at one canonical path,
// and the archive destination, because every durable evidence handle projected at
// the `project` stage and the whole retired-finalization replay path are written
// against it.
func (service *Service) restartClosure(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ClosureApplyResult, error) {
	previousJob, previousPlan := graph.ClosureJob, graph.ClosurePlan
	if previousJob == nil || previousPlan == nil || graph.ClosureCoverage == nil {
		absent := []string{}
		for _, record := range []struct {
			name    string
			present bool
		}{
			{"closure/job.json", previousJob != nil},
			{"closure/plan.json", previousPlan != nil},
			{"closure/coverage.json", graph.ClosureCoverage != nil},
		} {
			if !record.present {
				absent = append(absent, record.name)
			}
		}
		return ClosureApplyResult{}, fmt.Errorf(
			"closure restart replaces the reopened job, plan, and coverage in one transaction, "+
				"and campaign %s carries no %s. Remedy: if closure never started here, "+
				"closure_apply action=start; otherwise closure_apply action=status reports what "+
				"the campaign actually holds",
			graph.Campaign.ID, strings.Join(absent, " or "))
	}
	// A closure receipt is the proof that a campaign was finalized, and finalizing
	// retires the whole active tree, so in practice restart never meets one: the
	// loader cannot even build such a graph. CampaignGraph.Validate requires a
	// receipt to come with a `closed` campaign and a `completed` closure job,
	// while restart requires a `reopened` job on an `open` or `paused` campaign,
	// and LoadCampaignGraph validates before it returns - so every graph that
	// reaches ClosureApply already satisfies this clause.
	//
	// It is kept, stated separately, and tested directly anyway. This function
	// takes a CampaignGraph as an argument rather than loading one, so the
	// invariant that makes the clause redundant lives in a different file from the
	// code that depends on it; a future caller that assembles a graph in memory,
	// or a future relaxation of the receipt rule in Validate, would silently hand
	// restart a finalized campaign to re-plan. Restarting on a receipt would
	// re-open closure over records the archive already froze and the campaign no
	// longer owns, which is unrecoverable rather than merely wrong. The refusal is
	// its own clause so that a caller who somehow reaches it is told which of the
	// three preconditions failed instead of being sent to look at the job status.
	if graph.ClosureReceipt != nil {
		return ClosureApplyResult{}, fmt.Errorf(
			"campaign %s already carries closure receipt for job %s: closure was finalized and its "+
				"records were archived to %s, so there is no active attempt to re-enter. Restart "+
				"re-plans a reopened attempt; a finalized campaign is reopened only by opening a new "+
				"campaign against the archived one",
			graph.Campaign.ID, graph.ClosureReceipt.ClosureJobID,
			graph.ClosureReceipt.ArchiveDestination)
	}
	if previousJob.Status != "reopened" || !validOne(graph.Campaign.Status, "open", "paused") {
		remedy := "Remedy: closure_apply action=reopen first, which withdraws the current " +
			"attempt and returns the campaign to open; restart then re-enters closure"
		if previousJob.Status == "reopened" {
			remedy = fmt.Sprintf(
				"Remedy: the job is already reopened, so it is the campaign that is wrong - "+
					"manager_apply campaign.update setting status \"open\" or \"paused\", not %q",
				graph.Campaign.Status)
		}
		return ClosureApplyResult{}, fmt.Errorf(
			"closure restart requires a reopened closure job on an open or paused campaign; job "+
				"%s is %q and campaign %s is %q. %s",
			previousJob.ID, previousJob.Status, graph.Campaign.ID, graph.Campaign.Status, remedy)
	}
	if request.ClosureJobID != previousJob.ID {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure restart must name the exact closure job it re-enters; it named %q and this "+
				"campaign's job is %s. A campaign has one closure record at one canonical path, "+
				"and a restart keeps its identity. Remedy: set closureJobId to %s",
			request.ClosureJobID, previousJob.ID, previousJob.ID)
	}
	// Name the file and the number found. A terse refusal here would simply move
	// the dead end rather than remove it: a caller who cannot tell which
	// revision the canonical plan froze has no way to construct a request that
	// would be accepted.
	if request.ExpectedClosurePlanRevision != previousPlan.CampaignRevision {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure restart expected plan campaign revision %d but active/%s/closure/plan.json froze %d",
			request.ExpectedClosurePlanRevision, request.CampaignSlug, previousPlan.CampaignRevision)
	}
	if request.ArchiveDestination != "" &&
		request.ArchiveDestination != previousJob.ArchiveDestination {
		return ClosureApplyResult{}, fmt.Errorf(
			"closure restart cannot repoint job %s from archive destination %s to %s: every "+
				"durable evidence handle the project stage writes, and the whole "+
				"retired-finalization replay path, are addressed against the original. Remedy: "+
				"omit archiveDestination, or set it to %s",
			previousJob.ID, previousJob.ArchiveDestination, request.ArchiveDestination,
			previousJob.ArchiveDestination)
	}
	if err := validateExportedDefermentIDs(graph, request.ExportedWorkItemIDs); err != nil {
		return ClosureApplyResult{}, err
	}
	plan, err := BuildClosurePlan(graph, previousJob.ArchiveDestination)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	prior, err := service.inheritedClosureCoverage(
		graph.Campaign.ID, request.CampaignSlug, request, *graph.ClosureCoverage)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	coverage, err := ComputeClosureCoverage(graph, &prior)
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if err := service.applyClosureActiveFileInventory(
		graph, request.CampaignSlug, prior.ActiveFileDispositions, &coverage,
	); err != nil {
		return ClosureApplyResult{}, err
	}

	campaign := *graph.Campaign
	campaign.Revision++
	campaign.UpdatedAt, campaign.UpdatedBy, campaign.CorrelationID, campaign.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	campaign.Status, campaign.ClosingAt, campaign.PausedAt = "closing", request.Timestamp, ""

	job := *previousJob
	job.Revision++
	job.UpdatedAt, job.UpdatedBy, job.CorrelationID, job.Digest =
		request.Timestamp, request.Actor, request.CorrelationID, ""
	job.Stage, job.Status = "inventory", "running"
	job.Attempt = closureAttempt(*previousJob) + 1
	job.FrozenCampaignRevision = plan.CampaignRevision
	job.ProjectionFindingIDs = append([]string(nil), plan.ProjectionFindingIDs...)
	job.Coverage = &coverage
	job.TruthDigests, job.ProjectionDigests = map[string]string{}, map[string]string{}
	job.StagingDigest, job.ArchiveDigest = "", ""
	job.Blockers = closureCoverageBlockers(coverage)
	if err := sealClosureJobForComparison(&job); err != nil {
		return ClosureApplyResult{}, err
	}
	if err := ValidateClosureRestart(*previousJob, job); err != nil {
		return ClosureApplyResult{}, err
	}

	coverageRevision := request.ExpectedCoverageRevision
	if coverageRevision < 1 {
		coverageRevision = 1
	}
	writes, err := closureWrites(request.CampaignSlug, request.CorrelationID, request.ExpectedRecordDigests,
		[]closureWriteSpec{
			{value: campaign, expectedRevision: graph.Campaign.Revision,
				expectedDigest: request.ExpectedRecordDigests[graph.Campaign.ID]},
			// A create spec is expectedRevision 0, and the journal errors with
			// "record already exists" for one. Every record a restart touches is a
			// true update, so every one of the four carries its exact prior
			// revision and digest and closureWrites refuses a missing digest.
			{value: plan, expectedRevision: previousPlan.CampaignRevision,
				expectedDigest: request.ExpectedRecordDigests["closure-plan"]},
			{value: job, expectedRevision: previousJob.Revision,
				expectedDigest: request.ExpectedRecordDigests[previousJob.ID]},
			{value: coverage, expectedRevision: coverageRevision,
				expectedDigest: request.ExpectedRecordDigests["closure-coverage"]},
		})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	if _, err := service.queueClosureNormalization(graph, request.Timestamp); err != nil {
		return ClosureApplyResult{}, fmt.Errorf("queue closure normalization: %w", err)
	}
	receipt, err := store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: "closure.restart",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest,
		RebaseHead:         closureActionRebasesHead(request.Action), Writes: writes,
	})
	if err != nil {
		return ClosureApplyResult{}, err
	}
	// Reopen already discarded the previous attempt's private stage, and a
	// restarted job keeps its ID, so it keeps the same staging key. Sweep again:
	// if the process died between reopen's commit and its best-effort cleanup, the
	// old content-addressed objects are still sitting under the key the new
	// attempt will write to. Only an inert, unindexed cache is at risk here.
	_ = discardClosureStaging(service.Boundary, graph.Campaign.ID, job.ID)
	result := ClosureApplyResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action,
		Transaction: &receipt, Plan: &plan, Job: &job,
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
			return nil, fmt.Errorf(
				"closure_apply updates %s at %s and requires expectedRecordDigests[%q]: the "+
					"exact digest of the record it replaces. Remedy: closure_apply "+
					"action=status returns the current campaign, closure-plan, closure-job, and "+
					"closure-coverage records; copy this one's digest in under the key %q. Note "+
					"that the plan and coverage are keyed by the literal names \"closure-plan\" "+
					"and \"closure-coverage\", not by a record id",
				id, recordPath, id, id)
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
		ActiveFileDispositions: map[string]string{},
		UnresolvedConflicts:    []string{}, MissingDecisions: []string{},
	}
	for key, value := range request.FileRetention {
		prior.FileRetention[key] = value
	}
	for key, value := range request.ActiveFileDispositions {
		prior.ActiveFileDispositions[key] = value
	}
	for _, id := range request.ExportedWorkItemIDs {
		prior.WorkItemCoverage[id] = "exported-backlog"
	}
	return prior
}

// inheritedClosureCoverage carries into a restarted attempt the two manager-owned
// dispositions that cannot be inferred from record state - explicit file retention
// and exported backlog - and nothing else. ComputeClosureCoverage re-derives every
// other class from scratch and re-checks both of these against live records: a
// retention decision is keyed to a run file whose run is immutable once terminal,
// and an exported-backlog work item is honored only if it still carries a valid
// export contract. So inheritance can never smuggle a stale approval past a gate.
//
// Demanding re-declaration would have been the stricter choice and the wrong one.
// A long campaign accumulates hundreds of retention rows; a restart that required
// all of them back in a single request would be a second way to strand exactly the
// campaigns this transition exists to rescue.
//
// Inherited active-file dispositions are pruned to files that still exist; declared
// ones are not. That asymmetry is deliberate. applyClosureActiveFileInventory fails
// closed on a disposition naming a missing file, which is right for a caller's
// assertion and wrong for an inheritance - one deleted scratch file would otherwise
// make every future restart refuse, with no supported way to withdraw the row.
// Inheritance here is also narrow in practice: knownActiveFileDispositions infers a
// disposition for every canonical record and run file and takes precedence, so
// carried rows only ever cover files the engine cannot type on its own.
func (service *Service) inheritedClosureCoverage(
	campaignID, slug string,
	request ClosureApplyRequest,
	previous ClosureCoverage,
) (ClosureCoverage, error) {
	if service == nil {
		return ClosureCoverage{}, errors.New("closure coverage inheritance requires a service")
	}
	files, err := listActiveTreeFiles(service.Boundary, slug)
	if err != nil {
		return ClosureCoverage{}, err
	}
	present := make(map[string]bool, len(files))
	for _, relative := range files {
		present[relative] = true
	}
	prior := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: campaignID,
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		ActiveFileDispositions: map[string]string{},
		UnresolvedConflicts:    []string{}, MissingDecisions: []string{},
	}
	for key, value := range previous.FileRetention {
		if validExplicitRetention(value) {
			prior.FileRetention[key] = value
		}
	}
	for id, value := range previous.WorkItemCoverage {
		if value == "exported-backlog" {
			prior.WorkItemCoverage[id] = value
		}
	}
	for relative, value := range previous.ActiveFileDispositions {
		if present[relative] && validOne(value, "retain", "destroy-approved", "ephemeral") {
			prior.ActiveFileDispositions[relative] = value
		}
	}
	// The request overlays the inheritance last so a manager can revise a carried
	// decision in the same transaction that re-enters closure, rather than having
	// to reopen a second time to change one of them.
	for key, value := range request.FileRetention {
		prior.FileRetention[key] = value
	}
	for key, value := range request.ActiveFileDispositions {
		prior.ActiveFileDispositions[key] = value
	}
	for _, id := range request.ExportedWorkItemIDs {
		prior.WorkItemCoverage[id] = "exported-backlog"
	}
	return prior, nil
}

// blockerListOrNone keeps a refusal from ending in an empty clause. A finalize
// can be refused for a coverage digest change with no blockers at all - the
// coverage moved but is still satisfiable - and "blockers: " followed by
// nothing reads as a truncated message rather than as an answer.
func blockerListOrNone(blockers []string) string {
	if len(blockers) == 0 {
		return "none (the coverage moved but is not itself blocked)"
	}
	return strings.Join(blockers, ", ")
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
	result.ActiveFileDispositions = cloneStringMap(source.ActiveFileDispositions)
	result.UnresolvedConflicts = append([]string(nil), source.UnresolvedConflicts...)
	result.MissingDecisions = append([]string(nil), source.MissingDecisions...)
	return result
}

func (service *Service) applyClosureActiveFileInventory(
	graph CampaignGraph,
	slug string,
	prior map[string]string,
	coverage *ClosureCoverage,
) error {
	if service == nil || coverage == nil {
		return errors.New("closure active-file inventory requires a service and coverage")
	}
	files, err := listActiveTreeFiles(service.Boundary, slug)
	if err != nil {
		return err
	}
	known := knownActiveFileDispositions(graph, *coverage)
	actual := map[string]bool{}
	dispositions := map[string]string{}
	missing := make([]string, 0, len(coverage.MissingDecisions))
	for _, blocker := range coverage.MissingDecisions {
		if !strings.HasPrefix(blocker, "active-file:") {
			missing = append(missing, blocker)
		}
	}
	for _, relative := range files {
		actual[relative] = true
		if inferred := known[relative]; inferred != "" {
			dispositions[relative] = inferred
			continue
		}
		disposition := prior[relative]
		if !validOne(disposition, "retain", "destroy-approved", "ephemeral") {
			disposition = "decision-required"
			missing = append(missing, "active-file:"+relative)
		}
		dispositions[relative] = disposition
	}
	for relative := range prior {
		if validateRelativeRecordPath(relative) != nil {
			return fmt.Errorf("closure active-file disposition path %q is invalid", relative)
		}
		if !actual[relative] {
			return fmt.Errorf(
				"activeFileDispositions names %s, which does not exist under active/%s. A "+
					"disposition is an assertion about a file the archive will decide the fate "+
					"of, so one naming nothing is silently ignored rather than obeyed. Remedy: "+
					"drop the entry; closure_apply action=status lists the files that still "+
					"need a disposition as active-file:<path> blockers",
				relative, slug)
		}
	}
	coverage.ActiveFileDispositions = dispositions
	coverage.MissingDecisions = SortedUnique(missing)
	return sealClosureCoverage(coverage)
}

func listActiveTreeFiles(boundary Boundary, slug string) ([]string, error) {
	if !managedSlugRE.MatchString(slug) {
		return nil, errors.New("closure active-file inventory requires a valid campaign slug")
	}
	root := filepath.Join(boundary.Root, "active", slug)
	resolved, err := canonicalExistingPath(root)
	if err != nil || !withinRoot(boundary.Root, resolved) {
		return nil, errors.New("closure active tree does not resolve inside the project")
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("closure active tree is not a real directory")
	}
	files := []string{}
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("closure active tree contains symbolic link %s", current)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("closure active tree contains unsupported entry %s", current)
		}
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		multiple, linkErr := writerFileHasMultipleLinks(file)
		closeErr := file.Close()
		if linkErr != nil || closeErr != nil || multiple {
			return fmt.Errorf("closure active tree contains unsafe hard-linked file %s", current)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if validateRelativeRecordPath(relative) != nil {
			return fmt.Errorf("closure active tree contains unsafe path %s", relative)
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func knownActiveFileDispositions(graph CampaignGraph, coverage ClosureCoverage) map[string]string {
	known := map[string]string{
		"campaign.json": "retain", "events/events.jsonl": "retain", "STATE.md": "ephemeral",
	}
	for id := range graph.WorkItems {
		known[path.Join("work-items", id+".json")] = "retain"
	}
	for id := range graph.Findings {
		known[path.Join("findings", id+".md")] = "retain"
	}
	for id := range graph.Intakes {
		known[path.Join("intake", id+".json")] = "retain"
	}
	for id := range graph.Reviews {
		known[path.Join("reviews", id+".json")] = "retain"
	}
	if graph.ClosurePlan != nil {
		known["closure/plan.json"] = "retain"
	}
	if graph.ClosureJob != nil {
		known["closure/job.json"] = "retain"
	}
	if graph.ClosureCoverage != nil {
		known["closure/coverage.json"] = "retain"
	}
	if graph.ClosureReceipt != nil {
		known["closure/receipt.json"] = "retain"
	}
	activePrefix := path.Join("active", graph.Campaign.Slug) + "/"
	for id, run := range graph.Runs {
		known[path.Join("runs", id, "run.json")] = "retain"
		known[path.Join("runs", id, "AGENTS.override.md")] = "ephemeral"
		for _, handle := range []*FileHandle{run.Brief, run.ContextPack, run.Report} {
			if handle != nil && strings.HasPrefix(handle.Path, activePrefix) {
				known[strings.TrimPrefix(handle.Path, activePrefix)] = "retain"
			}
		}
		for _, file := range run.Files {
			relative := path.Join("runs", id, file.Path)
			switch coverage.FileRetention[id+":"+file.Path] {
			case "retained-inline", "distilled-and-retained":
				known[relative] = "retain"
			case "retained-by-reference", "discarded-approved", "external-verified":
				known[relative] = "destroy-approved"
			case "decision-required", "":
				known[relative] = "decision-required"
			default:
				if strings.HasPrefix(coverage.FileRetention[id+":"+file.Path], "maintained:") {
					known[relative] = "destroy-approved"
				}
			}
		}
	}
	return known
}

// prepareClosureFindingTransitions builds closure-owned finding revisions
// without publishing them. Truth candidates become current, and every finding
// whose evidence is inside the retiring campaign receives durable archive
// handles. The revised documents and rendered projections remain private until
// finalization publishes the archive and durable outputs in one transaction.
func (service *Service) prepareClosureFindingTransitions(
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (CampaignGraph, []closureWriteSpec, error) {
	if graph.ClosurePlan == nil || graph.ClosureJob == nil {
		return CampaignGraph{}, nil, errors.New("closure finding projection requires a canonical plan and job")
	}
	next := cloneCampaignGraph(graph)
	writes := []closureWriteSpec{}
	truthCandidates := map[string]bool{}
	for _, id := range graph.ClosurePlan.ProjectionFindingIDs {
		finding, present := graph.Findings[id]
		if !present || finding.Projection != "truth" {
			return CampaignGraph{}, nil, fmt.Errorf("truth projection finding %s is missing or no longer targets truth", id)
		}
		truthCandidates[id] = true
	}
	ids := make([]string, 0, len(graph.Findings))
	for id := range graph.Findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		finding := graph.Findings[id]
		truthCandidate := truthCandidates[id]
		if truthCandidate && (!validOne(finding.Validity, "provisional", "current") ||
			finding.ReviewState != "manager-ratified" || finding.EvidenceGrade != "direct" ||
			len(finding.Relations.Contradicts) != 0) {
			// Truth promotion is the one projection that changes what the
			// project believes, so it carries four independent preconditions.
			// Naming the failing one is the difference between one repair and
			// four guesses, and two of them have different remedies entirely -
			// a contradiction needs overturn, a soft grade needs new evidence.
			unmet := []string{}
			if !validOne(finding.Validity, "provisional", "current") {
				unmet = append(unmet, fmt.Sprintf(
					"validity is %q, and truth promotion accepts only provisional or current",
					finding.Validity))
			}
			if finding.ReviewState != "manager-ratified" {
				unmet = append(unmet, fmt.Sprintf(
					"reviewState is %q, not \"manager-ratified\"; ratify it through "+
						"manager_apply review.submit before closure projects it",
					finding.ReviewState))
			}
			if finding.EvidenceGrade != "direct" {
				unmet = append(unmet, fmt.Sprintf(
					"evidenceGrade is %q, and truth requires \"direct\"; a claim the project "+
						"will treat as true must cite evidence it can re-read",
					finding.EvidenceGrade))
			}
			if len(finding.Relations.Contradicts) != 0 {
				unmet = append(unmet, fmt.Sprintf(
					"it contradicts %s, and an unresolved contradiction cannot become truth; "+
						"settle it through the overturn workflow first",
					strings.Join(finding.Relations.Contradicts, ", ")))
			}
			return CampaignGraph{}, nil, fmt.Errorf(
				"finding %s targets truth but is not closure-promotable: %s. Remedy: repair the "+
					"finding with manager_apply finding.update, or retarget it with "+
					"projection \"history\", \"backlog\", \"playbook\", or \"archive\", then "+
					"reopen and restart closure so the plan is rebuilt without it",
				id, strings.Join(unmet, "; "))
		}
		changed := truthCandidate && finding.Validity != "current"
		projectedEvidence := append([]EvidenceReference(nil), finding.Evidence...)
		for index := range projectedEvidence {
			durable := durableClosureEvidenceReference(
				projectedEvidence[index], request.CampaignSlug,
				graph.ClosureJob.ArchiveDestination)
			if durable != projectedEvidence[index] {
				changed = true
			}
			projectedEvidence[index] = durable
		}
		if !changed {
			continue
		}
		expectedDigest := request.ExpectedRecordDigests[id]
		if expectedDigest != finding.Digest || !digestRE.MatchString(expectedDigest) {
			return CampaignGraph{}, nil, fmt.Errorf("closure finding %s requires its exact expected digest", id)
		}
		relative := path.Join("active", request.CampaignSlug, "findings", id+".md")
		_, value, handle, err := store.readCanonicalRecordValue(relative)
		if err != nil {
			return CampaignGraph{}, nil, err
		}
		document, ok := value.(FindingDocument)
		if !ok || handle.RecordDigest != finding.Digest || document.Record.Revision != finding.Revision {
			return CampaignGraph{}, nil, fmt.Errorf("closure finding %s changed during projection", id)
		}
		document.Record.Evidence = projectedEvidence
		document.Record.Revision++
		document.Record.UpdatedAt, document.Record.UpdatedBy = request.Timestamp, request.Actor
		document.Record.CorrelationID, document.Record.Digest = request.CorrelationID, ""
		if truthCandidate {
			document.Record.Validity = "current"
		}
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

func durableClosureEvidenceReference(
	evidence EvidenceReference,
	campaignSlug, archiveDestination string,
) EvidenceReference {
	activePrefix := path.Join("active", campaignSlug) + "/"
	if !strings.HasPrefix(evidence.Path, activePrefix) {
		return evidence
	}
	priorPath := evidence.Path
	evidence.Path = path.Join(archiveDestination, strings.TrimPrefix(evidence.Path, activePrefix))
	pathObjectPrefix := "path:" + priorPath
	if strings.HasPrefix(evidence.ObjectKey, pathObjectPrefix) {
		evidence.ObjectKey = "path:" + evidence.Path + strings.TrimPrefix(evidence.ObjectKey, pathObjectPrefix)
	}
	return evidence
}

func closureEvidenceSourcesInDurableOrder(
	source, durable []EvidenceReference,
	campaignSlug, archiveDestination string,
) ([]EvidenceReference, error) {
	if len(source) != len(durable) {
		return nil, errors.New("closure evidence cardinality changed")
	}
	used := make([]bool, len(source))
	ordered := make([]EvidenceReference, 0, len(durable))
	for _, target := range durable {
		matched := -1
		for index, candidate := range source {
			if used[index] {
				continue
			}
			projected := durableClosureEvidenceReference(candidate, campaignSlug, archiveDestination)
			if projected == target {
				matched = index
				break
			}
		}
		if matched < 0 {
			return nil, errors.New("closure durable evidence no longer maps to its exact source reference")
		}
		used[matched] = true
		ordered = append(ordered, source[matched])
	}
	return ordered, nil
}

func (service *Service) prepareClosureProjections(
	graph CampaignGraph,
	sourceGraph CampaignGraph,
	coverage ClosureCoverage,
	request ClosureApplyRequest,
) (map[string]string, map[string]string, []StateArtifactWrite, error) {
	if graph.ClosurePlan == nil || graph.ClosureJob == nil {
		return nil, nil, nil, errors.New("closure projection requires a canonical plan and job")
	}
	truthDigests := map[string]string{}
	projectionDigests := map[string]string{}
	artifacts := []StateArtifactWrite{}
	seenDestinations := map[string]bool{}
	workIDs := make([]string, 0, len(graph.WorkItems))
	for id := range graph.WorkItems {
		workIDs = append(workIDs, id)
	}
	sort.Strings(workIDs)
	for _, id := range workIDs {
		if coverage.WorkItemCoverage[id] != "exported-backlog" {
			continue
		}
		item := graph.WorkItems[id]
		if item.State != "deferred" || item.Deferment == nil || item.Deferment.BlocksClosure ||
			item.Deferment.ClosureDisposition != DefermentDispositionExportBacklog {
			return nil, nil, nil, fmt.Errorf("work item %s has invalid exported-backlog coverage", id)
		}
		destination := item.Deferment.ClosureDestination
		if err := validateDefermentBacklogDestination(destination); err != nil {
			return nil, nil, nil, fmt.Errorf("work item %s: %w", id, err)
		}
		if seenDestinations[destination] {
			return nil, nil, nil, fmt.Errorf("projection destination %s is assigned more than once", destination)
		}
		body, err := renderClosureWorkItemProjection(item, destination)
		if err != nil {
			return nil, nil, nil, err
		}
		digest := "sha256:" + SHA256Bytes(body)
		seenDestinations[destination] = true
		projectionDigests[destination] = digest
		artifacts = append(artifacts, StateArtifactWrite{
			Path: destination, ExpectedDigest: request.ExpectedArtifactDigests[destination],
			ContentDigest: digest, Body: body,
		})
	}
	ids := make([]string, 0, len(graph.Findings))
	for id := range graph.Findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		finding := graph.Findings[id]
		destination := closureFindingDestination(request, finding)
		switch finding.Projection {
		case "history", "archive", "rejected":
			continue
		case "truth", "backlog", "playbook", "maintained":
		default:
			return nil, nil, nil, fmt.Errorf(
				"finding %s has projection %q, which closure cannot project. Remedy: set it with "+
					"manager_apply finding.update to one that publishes a file - truth, "+
					"backlog, playbook, or maintained - or to one closure retires without "+
					"publishing: history, archive, or rejected",
				id, finding.Projection)
		}
		if destination == "" {
			return nil, nil, nil, fmt.Errorf(
				"finding %s has projection %q and needs a destination, which the engine will not "+
					"invent because the path is what readers cite forever. Remedy: pass "+
					"projectionDestinations[%q] on this closure_apply call - %s",
				id, finding.Projection, id, closureProjectionDestinationHint(
					finding.Projection, request.CampaignSlug, id))
		}
		if validClosureNavigationPath(destination) {
			return nil, nil, nil, fmt.Errorf(
				"finding %s cannot project to %s: closure generates that navigation index from "+
					"everything it publishes, so a finding written there would be overwritten by "+
					"the same transaction. Remedy: choose a leaf path such as %s",
				id, destination, closureProjectionDestinationHint(
					finding.Projection, request.CampaignSlug, id))
		}
		if seenDestinations[destination] {
			return nil, nil, nil, fmt.Errorf(
				"projection destination %s is assigned more than once in this closure; one file "+
					"cannot hold two projections and the second would silently replace the "+
					"first. Remedy: give finding %s its own path in projectionDestinations",
				destination, id)
		}
		seenDestinations[destination] = true

		var body []byte
		var digest string
		switch finding.Projection {
		case "truth":
			if err := validateCanonicalTruthDestination(request.CampaignSlug, id, destination); err != nil {
				return nil, nil, nil, err
			}
			sourceFinding, exists := sourceGraph.Findings[id]
			if !exists {
				return nil, nil, nil, fmt.Errorf("truth projection source finding %s is missing", id)
			}
			sourceEvidence, err := closureEvidenceSourcesInDurableOrder(
				sourceFinding.Evidence, finding.Evidence, request.CampaignSlug,
				graph.ClosureJob.ArchiveDestination)
			if err != nil {
				return nil, nil, nil, err
			}
			document, err := closureTruthFindingDocument(
				service.Boundary, request.CampaignSlug, sourceFinding, finding)
			if err != nil {
				return nil, nil, nil, err
			}
			projection, err := buildTruthProjection(
				service.Boundary, document, sourceEvidence, destination)
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
			activeTree := path.Join("active", request.CampaignSlug)
			cleanDestination := path.Clean(destination)
			if cleanDestination == activeTree || strings.HasPrefix(cleanDestination, activeTree+"/") {
				return nil, nil, nil, fmt.Errorf(
					"maintained projection %s cannot target the campaign tree retired by closure", id)
			}
			archiveTree := graph.ClosureJob.ArchiveDestination
			if cleanDestination == archiveTree || strings.HasPrefix(cleanDestination, archiveTree+"/") {
				return nil, nil, nil, fmt.Errorf(
					"maintained projection %s cannot target the closure archive", id)
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
			return nil, nil, nil, fmt.Errorf(
				"projectionDestinations names %s, which is not a finding of campaign %s. Remedy: "+
					"remove the key, or correct it - closure_apply action=status lists every "+
					"finding this closure will project",
				id, graph.Campaign.ID)
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

func closureFindingDestination(request ClosureApplyRequest, finding FindingRecord) string {
	if destination := request.ProjectionDestinations[finding.ID]; destination != "" {
		return destination
	}
	if finding.Projection == "truth" {
		return canonicalTruthDestination(request.CampaignSlug, finding.ID)
	}
	return ""
}

func closureTruthFindingDocument(
	boundary Boundary,
	campaignSlug string,
	source FindingRecord,
	projected FindingRecord,
) (FindingDocument, error) {
	sourcePath := source.Path
	if sourcePath == "" {
		sourcePath = path.Join("active", campaignSlug, "findings", source.ID+".md")
	}
	absolute, err := boundary.Resolve(sourcePath, true)
	if err != nil {
		return FindingDocument{}, fmt.Errorf("resolve truth source finding %s: %w", source.ID, err)
	}
	body, err := readSingleLinkRegularFile(absolute)
	if err != nil {
		return FindingDocument{}, fmt.Errorf("read truth source finding %s: %w", source.ID, err)
	}
	document, err := ParseFindingDocument(body, sourcePath)
	if err != nil {
		return FindingDocument{}, fmt.Errorf("parse truth source finding %s: %w", source.ID, err)
	}
	if document.Record.Digest != source.Digest || document.Record.CampaignID != projected.CampaignID ||
		document.Record.ID != projected.ID {
		return FindingDocument{}, fmt.Errorf("truth source finding %s no longer matches the closure graph", source.ID)
	}
	document.Record = projected
	return document, nil
}

// closureProjectionDestinationHint gives the shape of a legal destination for
// one projection kind. The rules differ per kind and two of them are checked
// far apart from where the destination is first demanded, so a caller told only
// "requires a projection destination" would have to trip a second refusal to
// learn the constraint.
func closureProjectionDestinationHint(projection, campaignSlug, findingID string) string {
	switch projection {
	case "backlog":
		return "a Markdown file below docs/backlog/, for example docs/backlog/<slug>.md"
	case "playbook":
		return "a Markdown file below docs/playbooks/, for example docs/playbooks/<slug>.md"
	case "maintained":
		return "the existing project-relative file this finding documents, outside both the " +
			"campaign's active tree and the closure archive"
	default:
		return "the canonical finding provenance path " +
			canonicalTruthDestination(campaignSlug, findingID)
	}
}

func validClosureNavigationPath(value string) bool {
	switch value {
	case "docs/INDEX.md", "docs/history/INDEX.md", "docs/truth/INDEX.md",
		"docs/backlog/INDEX.md", "docs/playbooks/INDEX.md":
		return true
	default:
		return false
	}
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

func renderClosureWorkItemProjection(item WorkItemRecord, destination string) ([]byte, error) {
	if item.Deferment == nil || item.Deferment.ClosureDisposition != DefermentDispositionExportBacklog ||
		item.Deferment.ClosureDestination != destination {
		return nil, errors.New("closure work-item projection requires its exact export-backlog contract")
	}
	semanticDigest, err := CanonicalDigest(struct {
		SchemaVersion int               `json:"schemaVersion"`
		WorkItemID    string            `json:"workItemId"`
		CampaignID    string            `json:"campaignId"`
		Destination   string            `json:"destination"`
		Title         string            `json:"title"`
		Problem       string            `json:"problem"`
		Acceptance    []string          `json:"acceptanceCriteria"`
		Deferment     DefermentContract `json:"deferment"`
	}{CampaignSchemaVersion, item.ID, item.CampaignID, destination, item.Title,
		item.Problem, item.Acceptance, *item.Deferment})
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("---\n")
	fmt.Fprintf(&output, "schemaVersion: %d\n", CampaignSchemaVersion)
	fmt.Fprintf(&output, "sourceWorkItem: %s\n", yamlScalar(item.ID))
	fmt.Fprintf(&output, "sourceCampaign: %s\n", yamlScalar(item.CampaignID))
	fmt.Fprintf(&output, "closureDisposition: %s\n", yamlScalar(item.Deferment.ClosureDisposition))
	fmt.Fprintf(&output, "semanticDigest: %s\n", yamlScalar(semanticDigest))
	output.WriteString("---\n\n# ")
	output.WriteString(item.Title)
	output.WriteString("\n\n")
	output.WriteString(item.Problem)
	output.WriteString("\n\n## Why deferred\n\n")
	output.WriteString(item.Deferment.Reason)
	output.WriteString("\n\n## Revisit trigger\n\n- ")
	output.WriteString(summarizeDefermentTrigger(item.Deferment.RevisitWhen))
	output.WriteString("\n\n## Owner\n\n- ")
	output.WriteString(item.Deferment.Owner)
	output.WriteString("\n\n## Acceptance criteria\n\n")
	for _, criterion := range item.Acceptance {
		output.WriteString("- ")
		output.WriteString(criterion)
		output.WriteByte('\n')
	}
	return []byte(output.String()), nil
}

func (service *Service) verifyClosureProjections(
	ctx context.Context,
	graph CampaignGraph,
	request ClosureApplyRequest,
) error {
	job := graph.ClosureJob
	if job == nil || len(job.ProjectionDigests) == 0 && len(job.ProjectionFindingIDs) != 0 {
		return errors.New("closure has no staged projection inventory")
	}
	projectedGraph, staging, err := service.stagedClosureGraph(graph)
	if err != nil {
		return err
	}
	root := closureStagingRoot(service.Boundary, staging.CampaignID, staging.ClosureJobID)
	for destination, expected := range job.ProjectionDigests {
		if staged := staging.ProjectionObjects[destination]; staged != "" {
			body, err := readClosureStageObject(root, staged)
			if err != nil {
				return err
			}
			if got := "sha256:" + SHA256Bytes(body); got != expected {
				return fmt.Errorf("staged projection %s digest changed", destination)
			}
			continue
		}
		absolute, err := service.Boundary.Resolve(destination, true)
		if err != nil {
			return fmt.Errorf("maintained projection %s: %w", destination, err)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != expected {
			return fmt.Errorf("maintained projection %s digest changed", destination)
		}
	}
	for id, destination := range staging.FindingDestinations {
		if requested := request.ProjectionDestinations[id]; requested != "" && requested != destination {
			return fmt.Errorf("projection %s destination changed", id)
		}
		finding, exists := projectedGraph.Findings[id]
		sourceFinding, sourceExists := graph.Findings[id]
		if !exists || !sourceExists {
			return fmt.Errorf("projection finding %s is missing", id)
		}
		expected := staging.ProjectionDigests[destination]
		switch finding.Projection {
		case "truth":
			sourceEvidence, err := closureEvidenceSourcesInDurableOrder(
				sourceFinding.Evidence, finding.Evidence, graph.Campaign.Slug,
				graph.ClosureJob.ArchiveDestination)
			if err != nil {
				return err
			}
			document, err := closureTruthFindingDocument(
				service.Boundary, graph.Campaign.Slug, sourceFinding, finding)
			if err != nil {
				return err
			}
			projection, err := buildTruthProjection(
				service.Boundary, document, sourceEvidence, destination)
			if err != nil {
				return err
			}
			if projection.ContentDigest != expected || projection.ContentDigest != job.TruthDigests[id] {
				return fmt.Errorf("truth projection %s no longer reproduces", id)
			}
		case "backlog", "playbook":
			body, err := renderClosureFindingProjection(finding, destination)
			if err != nil {
				return err
			}
			if "sha256:"+SHA256Bytes(body) != expected {
				return fmt.Errorf("%s projection %s no longer reproduces", finding.Projection, id)
			}
		case "maintained":
			// Its exact live digest was checked above.
		default:
			return fmt.Errorf("finding %s has an unsupported staged projection %s", id, finding.Projection)
		}
	}
	if graph.ClosureCoverage != nil {
		workIDs := make([]string, 0, len(graph.ClosureCoverage.WorkItemCoverage))
		for id, disposition := range graph.ClosureCoverage.WorkItemCoverage {
			if disposition == "exported-backlog" {
				workIDs = append(workIDs, id)
			}
		}
		sort.Strings(workIDs)
		for _, id := range workIDs {
			item, ok := graph.WorkItems[id]
			if !ok || item.Deferment == nil {
				return fmt.Errorf("exported work item %s is missing during projection verification", id)
			}
			destination := item.Deferment.ClosureDestination
			body, err := renderClosureWorkItemProjection(item, destination)
			if err != nil {
				return err
			}
			if expected := staging.ProjectionDigests[destination]; expected == "" ||
				"sha256:"+SHA256Bytes(body) != expected || job.ProjectionDigests[destination] != expected {
				return fmt.Errorf("work-item backlog projection %s no longer reproduces", id)
			}
		}
	}
	return service.verifyStagedClosureRetrieval(ctx, graph, projectedGraph, staging)
}

func (service *Service) prepareClosureArchive(
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	request ClosureApplyRequest,
) (ArchiveManifest, closureStagingManifest, error) {
	if graph.ClosureJob.Status != "verified" {
		return ArchiveManifest{}, closureStagingManifest{}, errors.New("closure archive requires a verified projection stage")
	}
	if err := service.verifyClosureProjections(ctx, graph, request); err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	projectedGraph, staging, err := service.stagedClosureGraph(graph)
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	files := map[string]string{}
	bodies := map[string][]byte{}
	stagingRoot := closureStagingRoot(service.Boundary, staging.CampaignID, staging.ClosureJobID)
	for _, relative := range requiredArchiveRecordPaths(projectedGraph) {
		if strings.HasPrefix(relative, "findings/") && path.Ext(relative) == ".md" {
			id := strings.TrimSuffix(path.Base(relative), ".md")
			if objectDigest := staging.PromotedFindings[id]; objectDigest != "" {
				body, readErr := readClosureStageObject(stagingRoot, objectDigest)
				if readErr != nil {
					return ArchiveManifest{}, closureStagingManifest{}, readErr
				}
				files[relative], bodies[relative] = "sha256:"+SHA256Bytes(body), body
				continue
			}
		}
		source := path.Join("active", request.CampaignSlug, relative)
		body, digest, err := readArchiveSource(service.Boundary, store, source)
		if err != nil {
			return ArchiveManifest{}, closureStagingManifest{}, fmt.Errorf("archive source %s: %w", source, err)
		}
		files[relative], bodies[relative] = digest, body
	}
	if err := collectRetainedRunFiles(service.Boundary, projectedGraph, files, bodies); err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	if err := collectRetainedActiveFiles(service.Boundary, projectedGraph, files, bodies); err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	if err := service.verifyClosureArchiveEvidence(graph, projectedGraph, files); err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	manifest, err := BuildArchiveManifest(
		projectedGraph, *graph.ClosureJob, head.EventID, request.Timestamp,
		files, graph.ClosureJob.ProjectionDigests,
	)
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	allowedArchiveFiles := map[string]bool{"manifest.json": true}
	for relative := range manifest.Files {
		allowedArchiveFiles[relative] = true
	}
	if err := verifyArchiveDirectoryInventory(
		service.Boundary, graph.ClosureJob.ArchiveDestination, allowedArchiveFiles, true,
	); err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	relatives := make([]string, 0, len(bodies))
	for relative := range bodies {
		relatives = append(relatives, relative)
	}
	sort.Strings(relatives)
	for _, relative := range relatives {
		digest, err := writeClosureStageObject(service.Boundary, stagingRoot, bodies[relative])
		if err != nil || digest != files[relative] {
			return ArchiveManifest{}, closureStagingManifest{}, errors.New("closure archive staging digest mismatch")
		}
	}
	manifestBody, err := canonicalJSON(manifest)
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	manifestObject, err := writeClosureStageObject(service.Boundary, stagingRoot, manifestBody)
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	staging.ArchiveDigest = manifest.Digest
	staging.ArchiveManifestObject = manifestObject
	staging.ArchiveFiles = cloneStringMap(files)
	staging, err = writeClosureStagingManifest(service.Boundary, staging)
	if err != nil {
		return ArchiveManifest{}, closureStagingManifest{}, err
	}
	return manifest, staging, nil
}

func (service *Service) verifyClosureArchiveEvidence(
	sourceGraph CampaignGraph,
	projectedGraph CampaignGraph,
	files map[string]string,
) error {
	archivePrefix := projectedGraph.ClosureJob.ArchiveDestination + "/"
	for id, projected := range projectedGraph.Findings {
		source, exists := sourceGraph.Findings[id]
		if !exists {
			return fmt.Errorf("archive evidence inventory for finding %s is incomplete", id)
		}
		sourceEvidence, err := closureEvidenceSourcesInDurableOrder(
			source.Evidence, projected.Evidence, projectedGraph.Campaign.Slug,
			projectedGraph.ClosureJob.ArchiveDestination)
		if err != nil {
			return fmt.Errorf("archive evidence inventory for finding %s: %w", id, err)
		}
		for index, durable := range projected.Evidence {
			prior := sourceEvidence[index]
			if durable.SHA256 != prior.SHA256 {
				return fmt.Errorf("archive evidence %s changed identity", durable.Path)
			}
			if strings.HasPrefix(durable.Path, archivePrefix) {
				relative := strings.TrimPrefix(durable.Path, archivePrefix)
				if files[relative] != durable.SHA256 {
					return fmt.Errorf("archive evidence %s is not retained at its durable path", durable.Path)
				}
				continue
			}
			absolute, err := service.Boundary.Resolve(durable.Path, true)
			if err != nil {
				return fmt.Errorf("external closure evidence %s is unreachable: %w", durable.Path, err)
			}
			body, err := readSingleLinkRegularFile(absolute)
			if err != nil || "sha256:"+SHA256Bytes(body) != durable.SHA256 {
				return fmt.Errorf("external closure evidence %s changed", durable.Path)
			}
		}
	}
	return nil
}

func collectRetainedActiveFiles(
	boundary Boundary,
	graph CampaignGraph,
	files map[string]string,
	bodies map[string][]byte,
) error {
	if graph.ClosureCoverage == nil {
		return errors.New("closure active-file retention requires canonical coverage")
	}
	paths := make([]string, 0, len(graph.ClosureCoverage.ActiveFileDispositions))
	for relative, disposition := range graph.ClosureCoverage.ActiveFileDispositions {
		if disposition == "retain" {
			paths = append(paths, relative)
		}
	}
	sort.Strings(paths)
	for _, relative := range paths {
		if _, alreadyCollected := bodies[relative]; alreadyCollected {
			continue
		}
		absolute, err := boundary.Resolve(path.Join("active", graph.Campaign.Slug, relative), true)
		if err != nil {
			return fmt.Errorf("retained active file %s: %w", relative, err)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return fmt.Errorf("retained active file %s: %w", relative, err)
		}
		digest := "sha256:" + SHA256Bytes(body)
		if existing := files[relative]; existing != "" && existing != digest {
			return fmt.Errorf("retained active file %s conflicts with canonical archive content", relative)
		}
		files[relative], bodies[relative] = digest, body
	}
	return nil
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
	ctx context.Context,
	store *StateStore,
	graph CampaignGraph,
	next ClosureJob,
	request ClosureApplyRequest,
) (CampaignRecord, ClosureReceipt, ArchiveManifest, []StateArtifactWrite, error) {
	if err := service.verifyClosureProjections(ctx, graph, request); err != nil {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	_, staging, err := service.stagedClosureGraph(graph)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	if staging.ArchiveDigest != graph.ClosureJob.ArchiveDigest ||
		!digestRE.MatchString(staging.ArchiveManifestObject) {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil,
			errors.New("closure archive staging does not match the active job")
	}
	stagingRoot := closureStagingRoot(service.Boundary, staging.CampaignID, staging.ClosureJobID)
	manifestBody, err := readClosureStageObject(stagingRoot, staging.ArchiveManifestObject)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	var manifest ArchiveManifest
	if decodeStrictJSON(manifestBody, &manifest) != nil || ValidateArchiveManifest(manifest) != nil ||
		manifest.Digest != graph.ClosureJob.ArchiveDigest || manifest.CampaignID != request.CampaignID ||
		!equalStringMap(manifest.Files, staging.ArchiveFiles) ||
		!equalStringMap(manifest.Projections, staging.ProjectionDigests) {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil,
			errors.New("closure staged archive manifest does not verify")
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
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	receiptBody, err := canonicalJSON(receipt)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	readme := renderArchiveREADME(campaign, next, receipt)
	artifacts := []StateArtifactWrite{}
	appendArtifact := func(destination, digest string, body []byte) error {
		if "sha256:"+SHA256Bytes(body) != digest {
			return fmt.Errorf("closure final artifact %s does not match its staged digest", destination)
		}
		artifacts = append(artifacts, StateArtifactWrite{
			Path: destination, ExpectedDigest: request.ExpectedArtifactDigests[destination],
			ContentDigest: digest, Body: body,
		})
		return nil
	}
	archiveRelatives := make([]string, 0, len(staging.ArchiveFiles))
	for relative := range staging.ArchiveFiles {
		archiveRelatives = append(archiveRelatives, relative)
	}
	sort.Strings(archiveRelatives)
	for _, relative := range archiveRelatives {
		body, err := readClosureStageObject(stagingRoot, staging.ArchiveFiles[relative])
		if err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
		if err := appendArtifact(
			path.Join(graph.ClosureJob.ArchiveDestination, relative),
			staging.ArchiveFiles[relative], body); err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
	}
	projectionPaths := make([]string, 0, len(staging.ProjectionObjects))
	for destination := range staging.ProjectionObjects {
		projectionPaths = append(projectionPaths, destination)
	}
	sort.Strings(projectionPaths)
	for _, destination := range projectionPaths {
		body, err := readClosureStageObject(stagingRoot, staging.ProjectionObjects[destination])
		if err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
		if err := appendArtifact(destination, staging.ProjectionObjects[destination], body); err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
	}
	maintainedPaths := make([]string, 0, len(staging.MaintainedProjections))
	for destination := range staging.MaintainedProjections {
		maintainedPaths = append(maintainedPaths, destination)
	}
	sort.Strings(maintainedPaths)
	for _, destination := range maintainedPaths {
		digest := staging.MaintainedProjections[destination]
		absolute, err := service.Boundary.Resolve(destination, true)
		if err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != digest {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil,
				fmt.Errorf("maintained projection %s changed before finalization", destination)
		}
		artifacts = append(artifacts, StateArtifactWrite{
			Path: destination, ExpectedDigest: digest, ContentDigest: digest, Body: body,
		})
	}
	for _, item := range []struct {
		path string
		body []byte
	}{
		{path.Join(graph.ClosureJob.ArchiveDestination, "closure", "receipt.json"), receiptBody},
		{path.Join(graph.ClosureJob.ArchiveDestination, "README.md"), readme},
	} {
		if err := appendArtifact(item.path, "sha256:"+SHA256Bytes(item.body), item.body); err != nil {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
		}
	}
	navigation, err := service.prepareClosureNavigationArtifacts(campaign, next, manifest, request)
	if err != nil {
		return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil, err
	}
	artifacts = append(artifacts, navigation...)
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if seen[artifact.Path] {
			return CampaignRecord{}, ClosureReceipt{}, ArchiveManifest{}, nil,
				fmt.Errorf("closure final transaction repeats artifact %s", artifact.Path)
		}
		seen[artifact.Path] = true
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return campaign, receipt, manifest, artifacts, nil
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
	allowed := map[string]bool{"manifest.json": true}
	for relative := range manifest.Files {
		allowed[relative] = true
	}
	return verifyArchiveDirectoryInventory(boundary, destination, allowed, false)
}

func verifyArchiveDirectoryInventory(
	boundary Boundary,
	destination string,
	allowed map[string]bool,
	allowMissing bool,
) error {
	if err := validateArchiveDestination(destination); err != nil {
		return err
	}
	root := filepath.Join(boundary.Root, filepath.FromSlash(destination))
	info, err := os.Lstat(root)
	if os.IsNotExist(err) && allowMissing {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("closure archive destination is not a real directory")
	}
	resolved, err := canonicalExistingPath(root)
	if err != nil || !withinRoot(boundary.Root, resolved) {
		return errors.New("closure archive destination escapes the project boundary")
	}
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("closure archive contains symbolic link %s", current)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("closure archive contains unsupported entry %s", current)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !allowed[relative] {
			return fmt.Errorf("closure archive contains unmanifested file %s", relative)
		}
		return nil
	})
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
