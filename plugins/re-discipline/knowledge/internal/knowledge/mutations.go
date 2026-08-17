package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type FindingSubmission struct {
	Record             FindingRecord `json:"record"`
	Body               string        `json:"body"`
	Path               string        `json:"path"`
	SyntheticQuestions []string      `json:"syntheticQuestions"`
	QuestionsReviewed  bool          `json:"questionsReviewed"`
}

func (submission FindingSubmission) MarshalJSON() ([]byte, error) {
	type wireFindingSubmission FindingSubmission
	wire := wireFindingSubmission(submission)
	if wire.Body == "" {
		wire.Body = submission.Record.Body
	}
	if wire.Path == "" {
		wire.Path = submission.Record.Path
	}
	return json.Marshal(wire)
}

func (submission FindingSubmission) Document() FindingDocument {
	record := submission.Record
	if submission.Body != "" {
		record.Body = submission.Body
	}
	if submission.Path != "" {
		record.Path = submission.Path
	}
	return FindingDocument{
		Record: record, SyntheticQuestions: append([]string(nil), submission.SyntheticQuestions...),
		QuestionsReviewed: submission.QuestionsReviewed,
	}
}

type ReviewPacketSubmission struct {
	Envelope   ReviewPacketEnvelope `json:"envelope"`
	Intake     IntakeRecord         `json:"intake"`
	Candidates []FindingSubmission  `json:"candidates"`
}

// RunPreparation carries the two immutable launch inputs that are published
// in the same state transaction as a delegated run. The context pack remains
// a typed value until this boundary so the manager service can independently
// verify its scope, generation, and retrieval profile before any bytes exist
// in the run workspace.
type RunPreparation struct {
	Brief       string      `json:"brief"`
	ContextPack ContextPack `json:"contextPack"`
}

func (submission ReviewPacketSubmission) Packet() CurationPacket {
	packet := CurationPacket{Intake: submission.Intake}
	for _, row := range submission.Envelope.Rows {
		packet.Rows = append(packet.Rows, CurationRow{FindingID: row.FindingID, Triage: row.Triage})
	}
	for _, candidate := range submission.Candidates {
		packet.Candidates = append(packet.Candidates, candidate.Document())
	}
	return packet
}

type ManagerApplyRequest struct {
	Action                  string                        `json:"action"`
	Actor                   string                        `json:"actor"`
	CampaignSlug            string                        `json:"campaignSlug"`
	CampaignID              string                        `json:"campaignId"`
	CorrelationID           string                        `json:"correlationId"`
	IdempotencyKey          string                        `json:"idempotencyKey"`
	Rationale               string                        `json:"rationale,omitempty"`
	ExpectedHeadRevision    int64                         `json:"expectedHeadRevision,omitempty"`
	ExpectedHeadDigest      string                        `json:"expectedHeadDigest,omitempty"`
	ExpectedRecordDigests   map[string]string             `json:"expectedRecordDigests,omitempty"`
	Campaign                *CampaignRecord               `json:"campaign,omitempty"`
	WorkItems               []WorkItemRecord              `json:"workItems,omitempty"`
	Runs                    []RunRecord                   `json:"runs,omitempty"`
	Findings                []FindingSubmission           `json:"findings,omitempty"`
	Intake                  *IntakeRecord                 `json:"intake,omitempty"`
	Review                  *ReviewRecord                 `json:"review,omitempty"`
	ReviewPacket            *ReviewPacketSubmission       `json:"reviewPacket,omitempty"`
	RunPreparation          *RunPreparation               `json:"runPreparation,omitempty"`
	ArchiveFallbackDecision *ArchiveFallbackOptInDecision `json:"archiveFallbackDecision,omitempty"`
	CampaignMerge           *CampaignMergeSubmission      `json:"campaignMerge,omitempty"`
	CampaignDiscard         *CampaignDiscardSubmission    `json:"campaignDiscard,omitempty"`
	TokenBudget             int                           `json:"tokenBudget,omitempty"`
}

var managerActionKinds = map[string]map[string]bool{
	"campaign.open":                     {"campaign": true, "work": true},
	"campaign.update":                   {"campaign": true, "work": true},
	"campaign.merge":                    {},
	"campaign.discard":                  {},
	"work.create":                       {"work": true, "campaign": true},
	"work.update":                       {"work": true, "campaign": true},
	"run.prepare":                       {"run": true, "work": true},
	"run.start":                         {"run": true, "work": true},
	"run.return":                        {"run": true, "work": true},
	"run.complete":                      {"run": true, "work": true, "finding": true},
	"closure.remediation.run.create":    {"run": true, "work": true},
	"review.submit":                     {"review": true, "intake": true, "finding": true, "work": true},
	"intake.coverage.retire":            {"intake": true},
	"finding.challenge":                 {"finding": true, "work": true},
	"finding.update":                    {"finding": true, "work": true},
	"decision.record":                   {"review": true, "intake": true, "finding": true, "work": true},
	"reconcile.import":                  {"campaign": true, "work": true, "run": true, "finding": true, "intake": true, "review": true},
	"knowledge.archive-fallback.opt-in": {},
}

// ManagerApply validates and applies one typed manager transition, then trims
// the receipt to the caller's requested tokenBudget. The trim happens here, at
// the outermost boundary and after the commit, so that every internal path -
// journaling, the persisted receipt, the idempotency replay comparison - still
// sees the complete receipt. See mutation_budget.go for what is and is not
// droppable.
func (service *Service) ManagerApply(ctx context.Context, request ManagerApplyRequest) (StateTransactionReceipt, error) {
	if err := validateMutationTokenBudget("manager_apply", request.TokenBudget); err != nil {
		return StateTransactionReceipt{}, err
	}
	receipt, err := service.managerApply(ctx, request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	return budgetTransactionReceipt(receipt, request.TokenBudget)
}

func (service *Service) managerApply(ctx context.Context, request ManagerApplyRequest) (StateTransactionReceipt, error) {
	if service == nil {
		return StateTransactionReceipt{}, errors.New("service is required")
	}
	allowedKinds, ok := managerActionKinds[request.Action]
	if !ok {
		// The one refusal that can never be aggregated with anything, because
		// every payload rule below is selected by the action. An unknown action
		// has no rules to be judged against, so there is nothing else to say.
		return StateTransactionReceipt{}, fmt.Errorf(
			"unsupported manager action %q; manager_apply accepts %s",
			request.Action, strings.Join(SupportedManagerActions(), ", "))
	}
	if request.Action == "campaign.merge" {
		return service.managerCampaignMerge(ctx, request)
	}
	if request.Action == "campaign.discard" {
		return service.managerCampaignDiscard(ctx, request)
	}
	// One pass, every violation the request alone reveals, before any write.
	// The returned request carries the engine-owned run handles and the curation
	// work item a returned run implies, which is why it replaces the caller's
	// copy from here on.
	// See mutations_shape.go.
	request, shapeErr := validateManagerRequestShape(request, allowedKinds, service.Configuration)
	if shapeErr != nil {
		return StateTransactionReceipt{}, withFirstStateViolation(
			"manager_apply "+request.Action, shapeErr,
			service.firstManagerStateViolation(ctx, request))
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	request, err := resolveManagerPublicationHead(store, request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if receipt, replayed, err := replayManagerRunPreparation(store, service.Boundary, request); err != nil {
		return StateTransactionReceipt{}, err
	} else if replayed {
		return receipt, nil
	}
	if receipt, replayed, err := replayArchiveFallbackOptIn(store, service.Boundary, request); err != nil {
		return StateTransactionReceipt{}, err
	} else if replayed {
		return receipt, nil
	}
	// From here down every check needs canonical state, so every one of them
	// still fails fast. Once the campaign does not resolve or the actor has no
	// authority over it, the answers to the rules below are not evidence about
	// the request - they are evidence about a premise that has already been
	// shown false.
	var graph CampaignGraph
	if request.Action != "campaign.open" {
		var err error
		graph, err = store.LoadCampaignGraph(request.CampaignID)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		if !containsString(graph.Campaign.PermittedManagers, request.Actor) {
			return StateTransactionReceipt{}, fmt.Errorf(
				"actor %q is not a permitted manager of campaign %s; its permittedManagers are %s. "+
					"Remedy: re-issue as one of those actors, or add this one with manager_apply "+
					"campaign.update carrying the next campaign revision",
				request.Actor, graph.Campaign.ID,
				strings.Join(graph.Campaign.PermittedManagers, ", "))
		}
	}
	// Mandatory scoped context is never droppable from a context pack, so a
	// record that cannot fit one bricks every later run for its campaign.
	// Refusing the write that introduces it is the only point where the caller
	// still knows which field it just made too large.
	if err := validateScopedContextBudgetDelta(graph, request); err != nil {
		return StateTransactionReceipt{}, err
	}
	artifacts, err := service.prepareManagerRunArtifacts(graph, request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	archiveArtifacts, err := service.prepareArchiveFallbackOptInArtifacts(ctx, store, request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	artifacts = append(artifacts, archiveArtifacts...)
	writes, reviewHandle, err := buildManagerWrites(service.Boundary, request, artifacts)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	return store.Apply(ctx, managerStateTransactionRequest(request, writes, artifacts, reviewHandle))
}

func managerStateTransactionRequest(
	request ManagerApplyRequest,
	writes []StateWrite,
	artifacts []StateArtifactWrite,
	reviewHandle string,
) StateTransactionRequest {
	return StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: request.Action,
		Rationale: request.Rationale, ReviewHandle: reviewHandle,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadRevision: request.ExpectedHeadRevision, ExpectedHeadDigest: request.ExpectedHeadDigest,
		RebaseHead: managerActionRebasesHead(request.Action),
		Writes:     writes, Artifacts: artifacts, ReviewPacket: request.ReviewPacket,
	}
}

// managerActionRebasesHead is an allow-list so newly added transitions remain
// exact-head by default until their scope has been reviewed. These actions are
// record-scoped; merge, discard, reconcile, archive-profile changes, migration,
// and closure finalization keep their stricter project-wide proofs.
func managerActionRebasesHead(action string) bool {
	return validOne(action,
		"campaign.open", "campaign.update", "work.create", "work.update",
		"run.prepare", "run.start", "run.return", "run.complete",
		"closure.remediation.run.create", "review.submit", "intake.coverage.retire",
		"finding.challenge", "finding.update", "decision.record")
}

func resolveManagerPublicationHead(
	store *StateStore,
	request ManagerApplyRequest,
) (ManagerApplyRequest, error) {
	if !managerActionRebasesHead(request.Action) {
		return request, nil
	}
	var snapshot *ContextPackScope
	if request.RunPreparation != nil {
		snapshot = &request.RunPreparation.ContextPack.Scope
	}
	revision, digest, err := resolveOrdinaryPublicationHead(
		store, request.ExpectedHeadRevision, request.ExpectedHeadDigest, snapshot)
	if err != nil {
		return ManagerApplyRequest{}, err
	}
	request.ExpectedHeadRevision = revision
	request.ExpectedHeadDigest = digest
	return request, nil
}

func resolveOrdinaryPublicationHead(
	store *StateStore,
	revision int64,
	digest string,
	snapshot *ContextPackScope,
) (int64, string, error) {
	if digestRE.MatchString(digest) && revision >= 0 {
		return revision, digest, nil
	}
	if digest != "" || revision != 0 {
		return 0, "", errors.New(
			"optional expectedHeadRevision and expectedHeadDigest must be omitted together or supplied as one valid pair")
	}
	if snapshot != nil {
		if snapshot.StateHeadRevision < 0 || !digestRE.MatchString(snapshot.StateHeadDigest) {
			return 0, "", errors.New("run preparation context pack has invalid state-head provenance")
		}
		return snapshot.StateHeadRevision, snapshot.StateHeadDigest, nil
	}
	head, err := store.LoadHead()
	if err != nil {
		return 0, "", err
	}
	return head.Revision, head.Digest, nil
}

// SupportedManagerActions is the published manager surface in a stable order.
// A refusal that names the action the caller asked for and not the ones it
// could have asked for costs a schema round trip to recover from, which is the
// whole cost this file is trying to remove.
func SupportedManagerActions() []string {
	actions := make([]string, 0, len(managerActionKinds))
	for action := range managerActionKinds {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}

// validateCampaignOpenPayload reports every campaign.open precondition the
// request fails, not the first. It is retained as a named entry point because
// campaign.open's payload rules are the one set that is worth exercising
// directly in a test; the aggregation lives in collectCampaignOpenPayload.
func validateCampaignOpenPayload(request ManagerApplyRequest) error {
	set := newRefusalSet("manager_apply campaign.open")
	collectCampaignOpenPayload(set, request)
	return set.result()
}

// managerActionsAcceptingKind names every action that may write one record
// kind, so a "cannot write X records" refusal can point at the transitions that
// can rather than leaving the caller to read the schema enum.
func managerActionsAcceptingKind(kind string) string {
	actions := []string{}
	for action, kinds := range managerActionKinds {
		if kinds[kind] {
			actions = append(actions, action)
		}
	}
	if len(actions) == 0 {
		return "no manager action"
	}
	sort.Strings(actions)
	return strings.Join(actions, ", ")
}

// validateManagerActionPayload reports every per-action payload rule the
// request fails. It is the aggregating entry point kept for direct testing; the
// rules themselves live in collectManagerActionPayload in mutations_shape.go,
// where the reason each one may or may not be evaluated out of order is
// recorded next to it.
func validateManagerActionPayload(request ManagerApplyRequest, allowed map[string]bool, configuration Configuration) error {
	set := newRefusalSet("manager_apply " + request.Action)
	collectManagerActionPayload(set, request, allowed, configuration)
	return set.result()
}

// managerActionObligations states, for each typed manager transition, the
// concrete payload the engine requires. A caller that sends only a rationale
// otherwise receives a generic refusal and cannot tell whether the action is
// misdesigned or simply misused.
var managerActionObligations = map[string]string{
	"campaign.open":    "Submit the open campaign record and its root work item.",
	"campaign.update":  "Submit the next campaign revision.",
	"campaign.merge":   "Submit the exact merge specification and the independently retained dry-run plan digest.",
	"campaign.discard": "Submit the exact campaign digest, destructive confirmation, and a non-empty reason.",
	"work.create":      "Submit at least one new work item.",
	"work.update":      "Submit at least one next work-item revision.",
	"run.prepare": "Submit the prepared run, its primary work item, and the runPreparation " +
		"brief and context pack.",
	"run.start": "Submit the running run and its primary work item.",
	"run.return": "Submit the returned run with the report SHA-256 and its primary work item; " +
		"the engine derives the report path.",
	"run.complete": "Submit the terminal run, its primary work item, and any findings the " +
		"review ratified.",
	"closure.remediation.run.create": "Submit the prepared remediation run, its primary work " +
		"item, and the runPreparation brief and context pack.",
	"review.submit": "A manager review is a transaction over a curated packet: submit the review " +
		"record, the resulting reviewed intake revision, and the exact reviewed packet.",
	"finding.challenge": "Submit at least one finding whose validity is challenged.",
	"finding.update":    "Submit at least one next finding revision.",
	"decision.record": "decision.record is the same review transaction as review.submit: a manager " +
		"decision is recorded against reviewed candidates, so submit the review record, the resulting " +
		"reviewed intake revision, and the exact reviewed packet. A standalone rationale is not a record; " +
		"every transition already journals its rationale in the state event.",
	"reconcile.import": "Submit the exact canonical records being reconciled.",
	"intake.coverage.retire": "A retirement is a bookkeeping transaction over one reviewed intake: submit " +
		"the next intake revision with the unresolved spans re-dispositioned and one appended amendment " +
		"naming each retired span, the exact rationale it displaces, and the review it preserves. No " +
		"finding, review, or run may be part of it.",
}

func managerActionObligation(action string) string {
	if obligation, ok := managerActionObligations[action]; ok {
		return obligation
	}
	return "Submit at least one typed record."
}

func describeAllowedRecordKinds(allowed map[string]bool) string {
	kinds := make([]string, 0, len(allowed))
	for kind := range allowed {
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		return "no typed records"
	}
	sort.Strings(kinds)
	if len(kinds) == 1 {
		return kinds[0] + " records"
	}
	return strings.Join(kinds[:len(kinds)-1], ", ") + ", and " + kinds[len(kinds)-1] + " records"
}

func (*Service) prepareManagerRunArtifacts(
	graph CampaignGraph,
	request ManagerApplyRequest,
) ([]StateArtifactWrite, error) {
	if !validOne(request.Action, "run.prepare", "closure.remediation.run.create") ||
		request.RunPreparation == nil {
		return nil, nil
	}
	if len(request.Runs) != 1 {
		return nil, errors.New("run preparation requires exactly one run")
	}
	run := request.Runs[0]
	preparation := *request.RunPreparation
	artifacts, err := runPreparationArtifacts(request)
	if err != nil {
		return nil, err
	}

	if graph.Campaign == nil || graph.Campaign.ID != request.CampaignID ||
		graph.Campaign.Slug != request.CampaignSlug {
		return nil, fmt.Errorf(
			"run preparation names campaign %s/%s, which does not resolve to a canonical open "+
				"campaign. Remedy: take campaignId and campaignSlug from state mode=orient",
			request.CampaignID, request.CampaignSlug)
	}

	var submitted *WorkItemRecord
	for index := range request.WorkItems {
		if request.WorkItems[index].ID != run.PrimaryWorkItemID {
			continue
		}
		if submitted != nil {
			return nil, fmt.Errorf(
				"run preparation submits work item %s more than once; a transaction writes one "+
					"revision per record. Remedy: send exactly one next revision of %s",
				run.PrimaryWorkItemID, run.PrimaryWorkItemID)
		}
		submitted = &request.WorkItems[index]
	}
	if submitted == nil {
		return nil, fmt.Errorf(
			"run preparation omits work item %s, the run's primaryWorkItemId. Remedy: read it "+
				"with state mode=work workItemId=%s and submit its next revision alongside the run",
			run.PrimaryWorkItemID, run.PrimaryWorkItemID)
	}
	currentWork, present := graph.WorkItems[run.PrimaryWorkItemID]
	if !present {
		return nil, fmt.Errorf(
			"run preparation names primaryWorkItemId %s, which is not a work item of campaign %s. "+
				"Remedy: state mode=resume campaignId=%s lists the campaign's work items; create "+
				"one first with manager_apply work.create if none fits",
			run.PrimaryWorkItemID, request.CampaignID, request.CampaignID)
	}
	if submitted.Revision != currentWork.Revision+1 {
		return nil, fmt.Errorf(
			"run preparation submits work item %s at revision %d; the canonical revision is %d, "+
				"so the next one is %d. Remedy: re-read it with state mode=work workItemId=%s and "+
				"submit revision %d with expectedRecordDigests[%q] set to %s",
			run.PrimaryWorkItemID, submitted.Revision, currentWork.Revision,
			currentWork.Revision+1, run.PrimaryWorkItemID, currentWork.Revision+1,
			run.PrimaryWorkItemID, currentWork.Digest)
	}

	// Name the exact scope fields that disagree. A pack binds ten values, and a
	// refusal that says only "does not bind" makes a caller diff two nested
	// objects by hand -- or, as happened, recompile the pack repeatedly hoping
	// to hit the combination the engine wanted.
	if mismatches := describeContextPackScopeMismatch(
		preparation.ContextPack.Scope, request, graph, run, *submitted,
	); len(mismatches) != 0 {
		return nil, fmt.Errorf(
			"run preparation context pack scope does not bind current state: %s. The pack is "+
				"immutable and its scope is what makes it auditable, so it is never adjusted "+
				"in place. Remedy: recompile with context_pack_materialize action=preview "+
				"against target kind=active-run campaignId=%s workItemId=%s runId=%s and submit "+
				"the previewed pack unchanged",
			strings.Join(mismatches, "; "),
			request.CampaignID, run.PrimaryWorkItemID, run.ID)
	}
	// Generation and retrieval-profile fields are provenance inside the sealed
	// pack, not leases on whatever index happens to be current at dispatch time.
	// Requiring them to equal the latest generation made unrelated indexing
	// activity invalidate an otherwise exact immutable launch snapshot.
	return artifacts, nil
}

// describeContextPackScopeMismatch lists, in a stable order, every scope field
// on a submitted active-run pack that disagrees with the state the transaction
// is being applied against. Stable order matters for the same reason it does in
// unresolvedCoverageHandles: two identical refusals must read identically, or
// they look like two different failures.
func describeContextPackScopeMismatch(
	scope ContextPackScope,
	request ManagerApplyRequest,
	graph CampaignGraph,
	run RunRecord,
	submitted WorkItemRecord,
) []string {
	mismatches := []string{}
	add := func(field, got, want string) {
		if got != want {
			mismatches = append(mismatches, fmt.Sprintf("scope.%s is %s, expected %s", field, got, want))
		}
	}
	add("kind", scope.Kind, "active-run")
	add("campaignId", scope.CampaignID, request.CampaignID)
	add("campaignSlug", scope.CampaignSlug, request.CampaignSlug)
	add("campaignRevision",
		strconv.FormatInt(scope.CampaignRevision, 10),
		strconv.FormatInt(graph.Campaign.Revision, 10))
	add("workItemId", scope.WorkItemID, run.PrimaryWorkItemID)
	add("workItemRevision",
		strconv.FormatInt(scope.WorkItemRevision, 10),
		strconv.FormatInt(submitted.Revision-1, 10))
	add("runId", scope.RunID, run.ID)
	add("runRevision", strconv.FormatInt(scope.RunRevision, 10), "0")
	add("stateHeadRevision",
		strconv.FormatInt(scope.StateHeadRevision, 10),
		strconv.FormatInt(request.ExpectedHeadRevision, 10))
	add("stateHeadDigest", scope.StateHeadDigest, request.ExpectedHeadDigest)
	return mismatches
}

// runLaunchArtifacts are the three immutable files a delegated run publishes.
// Their digests are the engine's own canonicalization: canonicalRunBrief
// appends the engine-sealed write-grant block to the submitted brief, and
// canonicalJSON serializes the pack with this package's exact indentation,
// field ordering, and HTML escaping.
type runLaunchArtifacts struct {
	Override StateArtifactWrite
	Brief    StateArtifactWrite
	Pack     StateArtifactWrite
}

func (artifacts runLaunchArtifacts) writes() []StateArtifactWrite {
	return []StateArtifactWrite{artifacts.Override, artifacts.Brief, artifacts.Pack}
}

func buildRunLaunchArtifacts(request ManagerApplyRequest) (runLaunchArtifacts, error) {
	if request.RunPreparation == nil || len(request.Runs) != 1 {
		return runLaunchArtifacts{}, errors.New(
			"run preparation requires exactly one run and launch payload")
	}
	run := request.Runs[0]
	preparation := *request.RunPreparation
	briefBody, err := canonicalRunBrief(preparation.Brief, run.WriteGrants)
	if err != nil {
		return runLaunchArtifacts{}, err
	}
	if _, err := VerifyContextPackValueExpected(
		preparation.ContextPack, preparation.ContextPack.Digest, preparation.ContextPack.PackID,
	); err != nil {
		return runLaunchArtifacts{}, fmt.Errorf("run preparation context pack: %w", err)
	}
	packBody, err := canonicalJSON(preparation.ContextPack)
	if err != nil {
		return runLaunchArtifacts{}, fmt.Errorf("serialize run preparation context pack: %w", err)
	}
	workspace, err := newCanonicalRunWorkspace(request.CampaignSlug, run.ID)
	if err != nil {
		return runLaunchArtifacts{}, err
	}
	briefHandle, err := workspace.handle(runBriefHandle, "sha256:"+SHA256Bytes(briefBody))
	if err != nil {
		return runLaunchArtifacts{}, err
	}
	packHandle, err := workspace.handle(runContextPackHandle, "sha256:"+SHA256Bytes(packBody))
	if err != nil {
		return runLaunchArtifacts{}, err
	}
	overrideBody := []byte(strings.TrimSpace(migrationDrafterOverrideTemplate) + "\n")
	return runLaunchArtifacts{
		Override: StateArtifactWrite{
			Path:          workspace.root + "/AGENTS.override.md",
			ContentDigest: "sha256:" + SHA256Bytes(overrideBody), Body: overrideBody,
		},
		Brief: StateArtifactWrite{
			Path: briefHandle.Path, ContentDigest: briefHandle.SHA256, Body: briefBody,
		},
		Pack: StateArtifactWrite{
			Path: packHandle.Path, ContentDigest: packHandle.SHA256, Body: packBody,
		},
	}, nil
}

// completeRunPreparationHandles derives the launch handles a delegated run
// omitted.
//
// The brief and context-pack digests are taken over engine-canonical bytes
// that no caller can reproduce without reimplementing this package, so
// demanding them as input made the documented delegation workflow impossible
// to execute by hand. Omitting them now means "the engine's canonicalization
// is authoritative", which is the only answer a caller could ever have given.
// Supplying them stays legal and stays strict: runPreparationArtifacts still
// compares byte for byte, so an independent verifier that computes the same
// digests keeps its compare-and-swap.
func completeRunPreparationHandles(request ManagerApplyRequest) (ManagerApplyRequest, error) {
	if !validOne(request.Action, "run.prepare", "closure.remediation.run.create") ||
		request.RunPreparation == nil || len(request.Runs) != 1 {
		return request, nil
	}
	run := request.Runs[0]
	if run.Brief != nil && run.ContextPack != nil {
		return request, nil
	}
	launch, err := buildRunLaunchArtifacts(request)
	if err != nil {
		return request, err
	}
	if run.Brief == nil {
		run.Brief = &FileHandle{Path: launch.Brief.Path, SHA256: launch.Brief.ContentDigest}
	}
	if run.ContextPack == nil {
		run.ContextPack = &FileHandle{Path: launch.Pack.Path, SHA256: launch.Pack.ContentDigest}
	}
	request.Runs = append([]RunRecord(nil), request.Runs...)
	request.Runs[0] = run
	return request, nil
}

// completeRunReportHandles replaces the digest-only report input on every
// ordinary run transition with the one canonical handle the run can own. A
// caller cannot choose or redirect the report path; reconcile.import remains
// separate because its purpose is to submit an exact canonical record.
func completeRunReportHandles(request ManagerApplyRequest) (ManagerApplyRequest, error) {
	if !validOne(request.Action,
		"run.prepare", "run.start", "run.return", "run.complete",
		"closure.remediation.run.create") || len(request.Runs) != 1 {
		return request, nil
	}
	run := request.Runs[0]
	if run.Report == nil {
		return request, nil
	}
	workspace, err := newCanonicalRunWorkspace(request.CampaignSlug, run.ID)
	if err != nil {
		return request, err
	}
	report, err := workspace.handle(runReportHandle, run.Report.SHA256)
	if err != nil {
		return request, err
	}
	run.Report = &report
	request.Runs = append([]RunRecord(nil), request.Runs...)
	request.Runs[0] = run
	return request, nil
}

func runPreparationArtifacts(request ManagerApplyRequest) ([]StateArtifactWrite, error) {
	launch, err := buildRunLaunchArtifacts(request)
	if err != nil {
		return nil, err
	}
	run := request.Runs[0]
	if err := compareRunLaunchHandle("brief", run.Brief, launch.Brief); err != nil {
		return nil, err
	}
	if err := compareRunLaunchHandle("context pack", run.ContextPack, launch.Pack); err != nil {
		return nil, err
	}
	return launch.writes(), nil
}

func compareRunLaunchHandle(label string, submitted *FileHandle, generated StateArtifactWrite) error {
	remedy := "omit the run record's brief and contextPack handles to let the engine derive them, " +
		"or compute them from the engine-canonical bytes it publishes"
	if submitted == nil {
		return fmt.Errorf(
			"run launch %s handle is missing; the engine generates %s with digest %s; %s",
			label, generated.Path, generated.ContentDigest, remedy)
	}
	if submitted.Path != generated.Path || submitted.SHA256 != generated.ContentDigest {
		return fmt.Errorf(
			"run launch %s handle %s/%s does not match the generated artifact %s/%s; %s",
			label, submitted.Path, submitted.SHA256,
			generated.Path, generated.ContentDigest, remedy)
	}
	return nil
}

func replayManagerRunPreparation(
	store *StateStore,
	boundary Boundary,
	request ManagerApplyRequest,
) (StateTransactionReceipt, bool, error) {
	if !validOne(request.Action, "run.prepare", "closure.remediation.run.create") ||
		request.RunPreparation == nil {
		return StateTransactionReceipt{}, false, nil
	}
	receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		return StateTransactionReceipt{}, false, err
	}
	artifacts, err := runPreparationArtifacts(request)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	writes, reviewHandle, err := buildManagerWrites(boundary, request, artifacts)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	prepared, err := prepareTransactionRequest(
		managerStateTransactionRequest(request, writes, artifacts, reviewHandle))
	if err != nil || !receiptAcceptsPreparedRequest(receipt, prepared) {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	return receipt, true, nil
}

const sealedWriteGrantMarker = "<!-- re-discipline:sealed-write-grants v1 -->"

func canonicalRunBrief(value string, grants []WriteGrant) ([]byte, error) {
	if !utf8.ValidString(value) || strings.HasPrefix(value, "\ufeff") ||
		strings.ContainsRune(value, '\x00') || strings.ContainsRune(value, '\r') ||
		!strings.HasSuffix(value, "\n") || strings.TrimSpace(value) == "" {
		return nil, errors.New("run brief must be non-empty canonical UTF-8 with LF line endings and a final LF")
	}
	if strings.Contains(value, sealedWriteGrantMarker) {
		return nil, errors.New("run brief may not supply the engine-sealed write-grant block")
	}
	if err := ValidateCanonicalWriteGrants(grants); err != nil {
		return nil, err
	}
	briefGrants := grants
	if briefGrants == nil {
		briefGrants = []WriteGrant{}
	}
	grantBody, err := json.Marshal(briefGrants)
	if err != nil {
		return nil, err
	}
	value += "\n" + sealedWriteGrantMarker + "\n## Engine-Sealed Write Grants\n\n" +
		"Run-local `report.md` and `payload/**` are implicit. The only ordinary project writes are:\n\n" +
		"```json\n" + string(grantBody) + "\n```\n"
	if int64(len(value)) > maxSourceBytes {
		return nil, fmt.Errorf("run brief exceeds the %d-byte source limit", maxSourceBytes)
	}
	return []byte(value), nil
}

// augmentRunReturnCuration appends the curation work a returned run implies.
//
// It used to refuse three ways: no single run, no frozen report, and a caller
// that submitted the system-owned queue id itself. All three are ordinary shape
// rules about the submitted request and they now live with the rest of them in
// collectManagerRunPayload, which reports them alongside everything else wrong
// with the same request instead of one per round trip. What is left is a pure
// transformation, so it cannot fail and does not return an error: on a request
// whose shape has not been established it declines to transform and leaves the
// refusal to the validator that owns it.
func augmentRunReturnCuration(request ManagerApplyRequest) ManagerApplyRequest {
	if request.Action != "run.return" || len(request.Runs) != 1 {
		return request
	}
	run := request.Runs[0]
	if run.Status != "returned" || run.Report == nil {
		return request
	}
	// A curator return is the normalization transaction itself. Queueing a
	// curator to curate that curator would recurse forever; CurationSubmit binds
	// its report directly to the resulting intake instead.
	if run.Role == "curator" {
		return request
	}
	queue := continuousCurationWork(run, request.Actor, request.CorrelationID)
	for _, work := range request.WorkItems {
		if work.ID == queue.ID {
			return request
		}
	}
	request.WorkItems = append(append([]WorkItemRecord(nil), request.WorkItems...), queue)
	run.SpawnedWorkItemIDs = SortedUnique(append(run.SpawnedWorkItemIDs, queue.ID))
	request.Runs = []RunRecord{run}
	return request
}

func continuousCurationWorkID(runID string) string {
	digest := SHA256String("continuous-curation\x00" + runID)
	value, _ := strconv.ParseUint(digest[:15], 16, 64)
	return fmt.Sprintf("W-%012d", value%1_000_000_000_000)
}

func continuousCurationWork(run RunRecord, actor, correlationID string) WorkItemRecord {
	reportPath := ""
	if run.Report != nil {
		reportPath = run.Report.Path
	}
	return WorkItemRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: continuousCurationWorkID(run.ID),
			CreatedAt: run.ReturnedAt, UpdatedAt: run.ReturnedAt, Revision: 1,
			CreatedBy: actor, UpdatedBy: actor, CorrelationID: correlationID,
		},
		CampaignID: run.CampaignID, Kind: "task", State: "ready", Priority: "normal",
		Title:   "Curate returned run " + run.ID,
		Problem: "Normalize the immutable returned report at " + reportPath + " into complete intake coverage and candidate findings.",
		Acceptance: []string{
			"Every substantive report span has an intake coverage disposition",
			"The submitted intake preserves exact source handles and routine/attention triage",
		},
		Relations:  WorkRelations{SpawnedByIDs: []string{run.PrimaryWorkItemID}},
		Owner:      "knowledge-curator",
		Assignee:   "knowledge-curator",
		ResumeNote: "Dispatch a curator against immutable source run " + run.ID + ".",
	}
}

// managerRecordValues lists, in a fixed order, every typed record a manager
// transition would write.
//
// It exists as its own function because two passes walk the same list for
// different reasons: the shape validator, to report everything wrong with all
// of them at once, and buildManagerWrites, to derive the writes that are
// actually committed. Two hand-maintained copies of this order would eventually
// disagree, and the disagreement would show up as a violation reported for a
// record that the write path never looked at.
func managerRecordValues(request ManagerApplyRequest) []any {
	values := []any{}
	if request.Campaign != nil {
		values = append(values, *request.Campaign)
	}
	for _, value := range request.WorkItems {
		values = append(values, value)
	}
	for _, value := range request.Runs {
		values = append(values, value)
	}
	for _, value := range request.Findings {
		values = append(values, value.Document())
	}
	if request.Intake != nil {
		values = append(values, *request.Intake)
	}
	if request.Review != nil {
		values = append(values, *request.Review)
	}
	return values
}

// managerRecordWrite derives the single StateWrite one submitted record
// produces, including the compare-and-swap expectation that guards it.
//
// Every input is the caller's: the record itself, the request's campaign and
// correlation ids, and the expectedRecordDigests map. Nothing here reads
// canonical state, which is what lets the shape validator run it over every
// record before the engine has loaded anything, and report all of the failures
// rather than the first.
func managerRecordWrite(request ManagerApplyRequest, value any) (StateWrite, error) {
	id, revision, _, campaignID, correlationID, err := stateRecordIdentity(value, 0, request.CorrelationID)
	if err != nil {
		return StateWrite{}, err
	}
	if campaignID != request.CampaignID ||
		(request.Action != "reconcile.import" && correlationID != request.CorrelationID) {
		return StateWrite{}, fmt.Errorf(
			"record %s does not bind the requested campaign and correlation", id)
	}
	path, err := stateRecordPath(request.CampaignSlug, value)
	if err != nil {
		return StateWrite{}, err
	}
	expectedRevision := revision - 1
	expectedDigest := ""
	if request.Action == "reconcile.import" {
		expectedRevision = revision
	}
	if expectedRevision > 0 {
		expectedDigest = request.ExpectedRecordDigests[id]
		if !digestRE.MatchString(expectedDigest) {
			return StateWrite{}, missingExpectedDigestError("manager_apply", id, expectedRevision, path)
		}
	}
	return StateWrite{
		Path: path, ExpectedRevision: expectedRevision, ExpectedDigest: expectedDigest, Record: value,
	}, nil
}

func buildManagerWrites(
	boundary Boundary,
	request ManagerApplyRequest,
	artifacts []StateArtifactWrite,
) ([]StateWrite, string, error) {
	artifactByPath := make(map[string]StateArtifactWrite, len(artifacts))
	for _, artifact := range artifacts {
		if _, exists := artifactByPath[artifact.Path]; exists {
			return nil, "", fmt.Errorf("run preparation repeats artifact path %s", artifact.Path)
		}
		artifactByPath[artifact.Path] = artifact
	}
	// Handle verification reads bytes off disk to prove a submitted digest, so
	// it is state, not shape, and it stays here failing fast.
	for _, value := range request.Runs {
		if err := verifyRunHandles(boundary, request.CampaignSlug, value, artifactByPath); err != nil {
			return nil, "", err
		}
	}
	values := managerRecordValues(request)
	writes := make([]StateWrite, 0, len(values))
	reviewHandle := ""
	for _, value := range values {
		write, err := managerRecordWrite(request, value)
		if err != nil {
			return nil, "", err
		}
		writes = append(writes, write)
		if _, ok := value.(ReviewRecord); ok {
			reviewHandle = "record:" + write.Path
		}
	}
	return writes, reviewHandle, nil
}

type CurationSubmitRequest struct {
	Actor                 string              `json:"actor"`
	CampaignSlug          string              `json:"campaignSlug"`
	CampaignID            string              `json:"campaignId"`
	CorrelationID         string              `json:"correlationId"`
	IdempotencyKey        string              `json:"idempotencyKey"`
	Rationale             string              `json:"rationale,omitempty"`
	ExpectedHeadRevision  int64               `json:"expectedHeadRevision,omitempty"`
	ExpectedHeadDigest    string              `json:"expectedHeadDigest,omitempty"`
	ExpectedRecordDigests map[string]string   `json:"expectedRecordDigests,omitempty"`
	Intake                IntakeRecord        `json:"intake"`
	Candidates            []FindingSubmission `json:"candidates"`
	Rows                  []CurationRow       `json:"rows"`
	CuratorRun            *RunRecord          `json:"curatorRun,omitempty"`
	TokenBudget           int                 `json:"tokenBudget,omitempty"`
	WorkItems             []WorkItemRecord    `json:"-"`
}

// CurationSubmit publishes one curator packet and, like ManagerApply, trims the
// returned receipt only at this boundary and only after the commit.
func (service *Service) CurationSubmit(ctx context.Context, request CurationSubmitRequest) (StateTransactionReceipt, error) {
	if err := validateMutationTokenBudget("curation_submit", request.TokenBudget); err != nil {
		return StateTransactionReceipt{}, err
	}
	receipt, err := service.curationSubmit(ctx, request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	return budgetTransactionReceipt(receipt, request.TokenBudget)
}

func (service *Service) curationSubmit(ctx context.Context, request CurationSubmitRequest) (StateTransactionReceipt, error) {
	if service == nil {
		return StateTransactionReceipt{}, errors.New("service is required")
	}
	// One pass, every violation the packet alone reveals, before any write.
	// See validateCurationRequestShape in mutations_shape.go; the packet it
	// returns is the same one the rules below were judged against, so the
	// engine never re-derives it from records it has already refused.
	packet, shapeErr := validateCurationRequestShape(request)
	if shapeErr != nil {
		return StateTransactionReceipt{}, shapeErr
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	revision, digest, err := resolveOrdinaryPublicationHead(
		store, request.ExpectedHeadRevision, request.ExpectedHeadDigest, nil)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	request.ExpectedHeadRevision = revision
	request.ExpectedHeadDigest = digest
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if graph.Campaign.Status != "open" && graph.Campaign.Status != "closing" {
		remedy := "no transition reopens a closed campaign; open a new campaign against the " +
			"archived one instead"
		if graph.Campaign.Status == "paused" {
			remedy = "Remedy: resume it with manager_apply campaign.update setting status \"open\", " +
				"then resubmit this packet"
		}
		return StateTransactionReceipt{}, fmt.Errorf(
			"curation_submit requires campaign %s to be open or closing, and it is %q. %s",
			graph.Campaign.ID, graph.Campaign.Status, remedy)
	}
	declared := map[string]bool{}
	for _, findingID := range request.Intake.CandidateFindingIDs {
		declared[findingID] = true
	}
	for _, coverage := range request.Intake.Coverage {
		if coverage.Disposition != "duplicate" || declared[coverage.TargetID] {
			continue
		}
		if _, exists := graph.Findings[coverage.TargetID]; !exists {
			return StateTransactionReceipt{}, fmt.Errorf(
				"coverage span disposed \"duplicate\" names targetId %s, which is neither a "+
					"canonical finding of campaign %s nor one of this packet's own candidates. "+
					"Remedy: point targetId at an existing finding (query returns their ids), "+
					"submit the span as a candidate-finding with its own record, or dispose it "+
					"as non-claim or out-of-scope",
				coverage.TargetID, request.CampaignID)
		}
	}
	if _, err := validateCurationGraphBindings(graph, packet, true); err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := verifyCanonicalIntakeCoverage(service.Boundary, request.Intake); err != nil {
		return StateTransactionReceipt{}, err
	}
	values := curationRecordValues(request)
	if request.CuratorRun != nil {
		canonical, bindingErr := validateReturnedCuratorRunBinding(graph, request.Actor, *request.CuratorRun)
		if bindingErr != nil {
			return StateTransactionReceipt{}, bindingErr
		}
		if err := verifyRunHandles(service.Boundary, request.CampaignSlug, canonical, nil); err != nil {
			return StateTransactionReceipt{}, err
		}
	}
	writes := []StateWrite{}
	for _, value := range values {
		write, err := curationRecordWrite(request, value)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		writes = append(writes, write)
	}
	return store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "curator", Action: "curation.submit",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, RebaseHead: true, Writes: writes,
	})
}

func validateReturnedCuratorRunBinding(
	graph CampaignGraph,
	actor string,
	submitted RunRecord,
) (RunRecord, error) {
	canonical, ok := graph.Runs[submitted.ID]
	if !ok {
		return RunRecord{}, fmt.Errorf(
			"curatorRun names run %s, which is not a run of this campaign. curatorRun is optional "+
				"proof, never a write. Remedy: omit it, or name the caller's own returned "+
				"curator run - state mode=resume lists the campaign's runs",
			submitted.ID)
	}
	// Say which of the four bindings failed. This value is proof, not a write:
	// a caller that cannot tell "wrong role" from "one field differs" has no way
	// to decide between fixing the copy and dropping the field entirely.
	problems := []string{}
	if canonical.Role != "curator" {
		problems = append(problems, fmt.Sprintf("its role is %q, not \"curator\"", canonical.Role))
	}
	if canonical.Status != "returned" {
		problems = append(problems, fmt.Sprintf("its status is %q, not \"returned\"", canonical.Status))
	}
	if canonical.ActorID != actor {
		problems = append(problems, fmt.Sprintf(
			"its actorId is %q and this packet is submitted by %q", canonical.ActorID, actor))
	}
	if !reflect.DeepEqual(canonical, submitted) {
		problems = append(problems, "the submitted copy is not byte-identical to the canonical record")
	}
	if len(problems) != 0 {
		return RunRecord{}, fmt.Errorf(
			"curatorRun %s is not an exact binding to the caller's canonical returned curator "+
				"run: %s. Remedy: omit curatorRun, or read run %s with read selector=record and "+
				"submit that record unchanged",
			submitted.ID, strings.Join(problems, "; "), submitted.ID)
	}
	return canonical, nil
}

func verifyCanonicalIntakeCoverage(boundary Boundary, intake IntakeRecord) error {
	for _, source := range intake.SourceRuns {
		if err := verifyCanonicalFileHandle(boundary, source); err != nil {
			return fmt.Errorf("intake source %s: %w", source.Path, err)
		}
		absolute, err := boundary.Resolve(source.Path, true)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(absolute)
		if err != nil {
			return err
		}
		if !utf8.Valid(body) {
			return fmt.Errorf("intake source %s is not valid UTF-8 report text", source.Path)
		}
		lines := strings.Split(string(normalizeNewlines(body)), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) == 0 {
			return fmt.Errorf("intake source %s has no report lines to cover", source.Path)
		}
		for _, entry := range intake.Coverage {
			if coverageSourceKey(entry.SourcePath, entry.SourceSHA256) == coverageSourceKey(source.Path, source.SHA256) &&
				entry.SourceLineCount != len(lines) {
				return fmt.Errorf("coverage for %s declares %d lines; canonical report has %d",
					source.Path, entry.SourceLineCount, len(lines))
			}
		}
	}
	return nil
}

func verifyRunHandles(
	boundary Boundary,
	slug string,
	run RunRecord,
	transactionArtifacts map[string]StateArtifactWrite,
) error {
	if err := validateRunHandleLocations(slug, run); err != nil {
		return err
	}
	for label, handle := range map[string]*FileHandle{
		"brief": run.Brief, "context pack": run.ContextPack, "report": run.Report,
	} {
		if handle == nil {
			continue
		}
		if err := validateFileHandle(*handle); err != nil {
			return fmt.Errorf("run %s %s: %w", run.ID, label, err)
		}
		if artifact, generated := transactionArtifacts[handle.Path]; generated {
			digest := "sha256:" + SHA256Bytes(artifact.Body)
			if handle.SHA256 != artifact.ContentDigest || artifact.ContentDigest != digest {
				return fmt.Errorf("run %s %s does not match its transaction artifact", run.ID, label)
			}
			continue
		}
		if err := verifyCanonicalFileHandle(boundary, *handle); err != nil {
			return fmt.Errorf("run %s %s: %w", run.ID, label, err)
		}
	}
	for _, file := range run.Files {
		relative := "active/" + slug + "/runs/" + run.ID + "/" + file.Path
		if err := verifyCanonicalFileHandle(boundary, FileHandle{Path: relative, SHA256: file.SHA256}); err != nil {
			return fmt.Errorf("run %s payload %s: %w", run.ID, file.Path, err)
		}
	}
	return nil
}

func verifyCanonicalFileHandle(boundary Boundary, handle FileHandle) error {
	if err := validateFileHandle(handle); err != nil {
		return err
	}
	absolute, err := boundary.Resolve(handle.Path, true)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(absolute)
	if err != nil {
		return err
	}
	if SHA256Bytes(body) != strings.TrimPrefix(handle.SHA256, "sha256:") {
		return errors.New("file digest does not match")
	}
	return nil
}

// missingExpectedDigestError is the one refusal every caller meets while
// learning this engine: an update carries the next revision of a record but no
// expectedRecordDigests entry for it, because nothing said the map was keyed by
// record id rather than by path, or that creates are exempt and updates are not.
//
// It is the cheapest possible refusal to make useful - the key, the reason, and
// the read that returns the value are all known here - and the most expensive
// to leave terse, because it fires once per record on a multi-record
// transaction and a caller that fixes one meets it again on the next.
func missingExpectedDigestError(tool, recordID string, expectedRevision int64, path string) error {
	return fmt.Errorf(
		"%s writes %s as revision %d, an update, so it requires expectedRecordDigests[%q]: the "+
			"exact digest of revision %d, the record being replaced. Creates (revision 1) are "+
			"exempt; updates are not, because the digest is the compare-and-swap that stops two "+
			"managers from silently overwriting each other. Remedy: read %s with state or read "+
			"and copy its digest into expectedRecordDigests under the key %q",
		tool, recordID, expectedRevision+1, recordID, expectedRevision, path, recordID)
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
