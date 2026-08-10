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
	ExpectedHeadRevision    int64                         `json:"expectedHeadRevision"`
	ExpectedHeadDigest      string                        `json:"expectedHeadDigest"`
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
}

var managerActionKinds = map[string]map[string]bool{
	"campaign.open":                     {"campaign": true, "work": true},
	"campaign.update":                   {"campaign": true, "work": true},
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

func (service *Service) ManagerApply(ctx context.Context, request ManagerApplyRequest) (StateTransactionReceipt, error) {
	if service == nil {
		return StateTransactionReceipt{}, errors.New("service is required")
	}
	allowedKinds, ok := managerActionKinds[request.Action]
	if !ok {
		return StateTransactionReceipt{}, fmt.Errorf("unsupported manager action %q", request.Action)
	}
	if strings.TrimSpace(request.Actor) == "" {
		return StateTransactionReceipt{}, errors.New("manager actor is required")
	}
	request, completionErr := completeRunPreparationHandles(request)
	if completionErr != nil {
		return StateTransactionReceipt{}, completionErr
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
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
	var graph CampaignGraph
	if request.Action == "campaign.open" {
		if request.Campaign == nil || len(request.WorkItems) == 0 || request.Campaign.Status != "open" ||
			!containsString(request.Campaign.PermittedManagers, request.Actor) {
			return StateTransactionReceipt{}, errors.New("campaign.open requires an open campaign, root work, and a permitted actor")
		}
	} else {
		var err error
		graph, err = store.LoadCampaignGraph(request.CampaignID)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		if !containsString(graph.Campaign.PermittedManagers, request.Actor) {
			return StateTransactionReceipt{}, errors.New("actor is not a permitted campaign manager")
		}
	}
	if request.Action == "run.return" {
		var err error
		request, err = augmentRunReturnCuration(request)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
	}
	if err := validateManagerActionPayload(request, allowedKinds, service.Configuration); err != nil {
		return StateTransactionReceipt{}, err
	}
	// Mandatory scoped context is never droppable from a context pack, so a
	// record that cannot fit one bricks every later run for its campaign.
	// Refusing the write that introduces it is the only point where the caller
	// still knows which field it just made too large.
	if err := validateScopedContextBudgetDelta(graph, request); err != nil {
		return StateTransactionReceipt{}, err
	}
	artifacts, err := service.prepareManagerRunArtifacts(ctx, store, graph, request)
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
		Writes: writes, Artifacts: artifacts, ReviewPacket: request.ReviewPacket,
	}
}

func validateManagerActionPayload(request ManagerApplyRequest, allowed map[string]bool, configuration Configuration) error {
	archiveAction := request.Action == "knowledge.archive-fallback.opt-in"
	if request.ArchiveFallbackDecision != nil && !archiveAction {
		return fmt.Errorf("manager action %s cannot carry an archive fallback decision", request.Action)
	}
	if archiveAction && request.ArchiveFallbackDecision == nil {
		return errors.New("archive fallback opt-in requires an explicit decision payload")
	}
	preparationAction := validOne(request.Action, "run.prepare", "closure.remediation.run.create")
	if request.RunPreparation != nil && !preparationAction {
		return fmt.Errorf("manager action %s cannot carry delegated run preparation", request.Action)
	}
	kinds := map[string]int{
		"campaign": boolCount(request.Campaign != nil), "work": len(request.WorkItems),
		"run": len(request.Runs), "finding": len(request.Findings),
		"intake": boolCount(request.Intake != nil), "review": boolCount(request.Review != nil),
	}
	total := 0
	for kind, count := range kinds {
		if count > 0 && !allowed[kind] {
			return fmt.Errorf("manager action %s cannot write %s records", request.Action, kind)
		}
		total += count
	}
	if total == 0 && !archiveAction {
		// A rationale is transaction metadata journaled with every transition;
		// it is not itself a record. Naming the accepted kinds and the concrete
		// obligation keeps a caller from concluding that the action is broken.
		return fmt.Errorf(
			"manager action %s contains no typed records; it accepts %s. %s",
			request.Action, describeAllowedRecordKinds(allowed),
			managerActionObligation(request.Action))
	}
	switch request.Action {
	case "campaign.update":
		if request.Campaign == nil {
			return errors.New("campaign.update requires a campaign record")
		}
	case "work.create", "work.update":
		if len(request.WorkItems) == 0 {
			return errors.New("work action requires at least one work item")
		}
	case "knowledge.archive-fallback.opt-in":
		if total != 0 || request.RunPreparation != nil || request.ReviewPacket != nil {
			return errors.New("archive fallback opt-in may carry only its explicit decision")
		}
	case "run.prepare", "run.start", "run.return", "run.complete", "closure.remediation.run.create":
		if len(request.Runs) != 1 || len(request.WorkItems) == 0 {
			return errors.New("run action requires exactly one run and its updated work item")
		}
		primaryIncluded := false
		for _, work := range request.WorkItems {
			primaryIncluded = primaryIncluded || work.ID == request.Runs[0].PrimaryWorkItemID
		}
		if !primaryIncluded {
			return errors.New("run action must publish its primary work item")
		}
		expectedStatus := map[string]string{
			"run.prepare": "prepared", "run.start": "running", "run.return": "returned",
			"closure.remediation.run.create": "prepared",
		}[request.Action]
		if request.Action != "run.complete" && request.Runs[0].Status != expectedStatus ||
			request.Action == "run.complete" && !validOne(request.Runs[0].Status, "completed", "blocked", "aborted", "invalidated") {
			return fmt.Errorf("run record status %s does not match action %s", request.Runs[0].Status, request.Action)
		}
		if preparationAction && request.Runs[0].Role != "manager" && request.RunPreparation == nil {
			return errors.New("delegated run preparation requires a canonical brief and context pack")
		}
	case "review.submit", "decision.record":
		if request.Review == nil || request.Intake == nil || request.ReviewPacket == nil {
			return fmt.Errorf(
				"manager action %s requires review, resulting intake, and the exact reviewed packet: %s",
				request.Action, managerActionObligation(request.Action))
		}
		packet := request.ReviewPacket.Packet()
		if err := ValidateReviewPacketEnvelope(request.ReviewPacket.Envelope, packet); err != nil {
			return err
		}
		if err := ValidateManagerReview("manager", packet, *request.Review); err != nil {
			return err
		}
		if request.Review.PacketDigest != request.ReviewPacket.Envelope.Digest {
			return errors.New("review receipt does not bind the submitted packet digest")
		}
		if !configuration.Valid {
			return errors.New("manager review requires valid review-load configuration")
		}
		if err := ValidateReviewLoadBinding(request.Review.ReviewLoad, *request.Review,
			packet, configuration.Bootstrap.ReviewLoad); err != nil {
			return err
		}
		if err := ValidateReviewLoadTemporalBinding(
			request.Review.ReviewLoad, request.ReviewPacket.Envelope.CreatedAt, *request.Review,
		); err != nil {
			return err
		}
		if request.Intake.ID != request.Review.IntakeID || request.Intake.Revision != request.Review.IntakeRevision+1 ||
			request.Intake.Status != "reviewed" {
			return errors.New("review submission must transactionally advance the bound intake revision to reviewed")
		}
		outcomes := make([]FindingDocument, 0, len(request.Findings))
		for _, finding := range request.Findings {
			outcomes = append(outcomes, finding.Document())
		}
		if err := ValidateManagerReviewOutcomes(packet, *request.Review, *request.Intake, outcomes); err != nil {
			return err
		}
	case "intake.coverage.retire":
		// The payload gate is deliberately narrow. Every substantive proof -
		// that the amendment quotes what it displaced, that it reconstructs the
		// exact submitted record, that the review it names really ratified this
		// intake - needs canonical prior state and belongs in
		// validateAppliedCoverageRetirement. What belongs here is the shape a
		// caller can get wrong before the engine has even loaded the record it
		// is amending, said once and plainly.
		if request.Intake == nil {
			return fmt.Errorf(
				"manager action %s requires the next revision of the intake being amended: %s",
				request.Action, managerActionObligation(request.Action))
		}
		if request.ReviewPacket != nil || request.RunPreparation != nil {
			return errors.New(
				"a coverage retirement is not a review and prepares no run; it carries neither a review " +
					"packet nor a run preparation")
		}
		if request.Intake.Status != "reviewed" {
			return fmt.Errorf(
				"a coverage retirement amends a reviewed intake and leaves it reviewed; the submitted "+
					"intake is %s", request.Intake.Status)
		}
		if len(request.Intake.Amendments) == 0 {
			return errors.New(
				"a coverage retirement must append one amendment naming every span it retires, the exact " +
					"rationale each displaces, and the manager review it preserves")
		}
		if err := ValidateCoverageAmendmentShape(
			request.Intake.Amendments[len(request.Intake.Amendments)-1],
		); err != nil {
			return err
		}
	case "finding.challenge":
		if len(request.Findings) == 0 {
			return errors.New("finding.challenge requires at least one finding")
		}
		for _, finding := range request.Findings {
			if finding.Record.Validity != "challenged" {
				return errors.New("finding.challenge may publish only challenged findings")
			}
		}
	}
	return nil
}

// managerActionObligations states, for each typed manager transition, the
// concrete payload the engine requires. A caller that sends only a rationale
// otherwise receives a generic refusal and cannot tell whether the action is
// misdesigned or simply misused.
var managerActionObligations = map[string]string{
	"campaign.open":   "Submit the open campaign record and its root work item.",
	"campaign.update": "Submit the next campaign revision.",
	"work.create":     "Submit at least one new work item.",
	"work.update":     "Submit at least one next work-item revision.",
	"run.prepare": "Submit the prepared run, its primary work item, and the runPreparation " +
		"brief and context pack.",
	"run.start":  "Submit the running run and its primary work item.",
	"run.return": "Submit the returned run with a frozen report handle and its primary work item.",
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

func (service *Service) prepareManagerRunArtifacts(
	ctx context.Context,
	store *StateStore,
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

	head, err := store.LoadHead()
	if err != nil {
		return nil, err
	}
	if head.Revision != request.ExpectedHeadRevision || head.Digest != request.ExpectedHeadDigest {
		return nil, fmt.Errorf("%w: run preparation expected state head is stale", ErrStateConflict)
	}
	if graph.Campaign == nil || graph.Campaign.ID != request.CampaignID ||
		graph.Campaign.Slug != request.CampaignSlug {
		return nil, errors.New("run preparation campaign binding does not resolve")
	}

	var submitted *WorkItemRecord
	for index := range request.WorkItems {
		if request.WorkItems[index].ID != run.PrimaryWorkItemID {
			continue
		}
		if submitted != nil {
			return nil, errors.New("run preparation repeats its primary work item")
		}
		submitted = &request.WorkItems[index]
	}
	if submitted == nil {
		return nil, errors.New("run preparation omits its primary work item")
	}
	currentWork, present := graph.WorkItems[run.PrimaryWorkItemID]
	if !present || submitted.Revision != currentWork.Revision+1 {
		return nil, errors.New("run preparation work item is not the next canonical revision")
	}

	scope := preparation.ContextPack.Scope
	if scope.Kind != "active-run" || scope.CampaignID != request.CampaignID ||
		scope.CampaignSlug != request.CampaignSlug || scope.CampaignRevision != graph.Campaign.Revision ||
		scope.WorkItemID != run.PrimaryWorkItemID || scope.WorkItemRevision != submitted.Revision-1 ||
		scope.RunID != run.ID || scope.RunRevision != 0 ||
		scope.StateHeadRevision != request.ExpectedHeadRevision ||
		scope.StateHeadDigest != request.ExpectedHeadDigest || scope.EventID != head.EventID {
		return nil, errors.New("run preparation context pack does not bind the exact campaign, work, run, and state head")
	}
	expectedRole := "drafter"
	if run.Role == "manager" {
		expectedRole = "manager"
	}
	if preparation.ContextPack.Role != expectedRole {
		return nil, fmt.Errorf("run role %s requires a %s context pack", run.Role, expectedRole)
	}
	if !EqualWriteGrants(run.WriteGrants, preparation.ContextPack.WriteGrants) {
		return nil, errors.New("run preparation write grants do not match the immutable context pack")
	}
	campaignHandle := "record:active/" + request.CampaignSlug + "/campaign.json"
	workHandle := "record:active/" + request.CampaignSlug + "/work-items/" + run.PrimaryWorkItemID + ".json"
	if !containsString(preparation.ContextPack.RequiredHandles, campaignHandle) ||
		!containsString(preparation.ContextPack.RequiredHandles, workHandle) {
		return nil, errors.New("run preparation context pack omits its exact campaign or work handle")
	}

	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify current run preparation generation: %w", err)
	}
	models := make([]string, 0, len(selected.Models))
	for _, model := range selected.Models {
		models = append(models, model.ID+"@"+model.Revision)
	}
	if !reflect.DeepEqual(preparation.ContextPack.Generation, CompactContextGeneration(generation)) ||
		preparation.ContextPack.RequestedProfile != selected.RequestedIdentity ||
		preparation.ContextPack.EffectiveProfile != selected.EffectiveIdentity ||
		!reflect.DeepEqual(preparation.ContextPack.ActiveLanes, selected.ActiveLanes) ||
		!reflect.DeepEqual(preparation.ContextPack.Models, models) ||
		!reflect.DeepEqual(preparation.ContextPack.FallbackReason, selected.FallbackReason) {
		return nil, errors.New("run preparation context pack generation or retrieval profile is no longer current")
	}

	return artifacts, nil
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
	runPrefix := "active/" + request.CampaignSlug + "/runs/" + run.ID + "/"
	overrideBody := []byte(strings.TrimSpace(migrationDrafterOverrideTemplate) + "\n")
	return runLaunchArtifacts{
		Override: StateArtifactWrite{
			Path:          runPrefix + "AGENTS.override.md",
			ContentDigest: "sha256:" + SHA256Bytes(overrideBody), Body: overrideBody,
		},
		Brief: StateArtifactWrite{
			Path:          runPrefix + "brief.md",
			ContentDigest: "sha256:" + SHA256Bytes(briefBody), Body: briefBody,
		},
		Pack: StateArtifactWrite{
			Path:          runPrefix + "context-pack.json",
			ContentDigest: "sha256:" + SHA256Bytes(packBody), Body: packBody,
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
	if err != nil || prepared.RequestDigest != receipt.RequestDigest {
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

func augmentRunReturnCuration(request ManagerApplyRequest) (ManagerApplyRequest, error) {
	if len(request.Runs) != 1 {
		return request, errors.New("run.return requires exactly one returned run")
	}
	run := request.Runs[0]
	if run.Status != "returned" || run.Report == nil {
		return request, errors.New("run.return requires a returned run with a frozen report handle")
	}
	// A curator return is the normalization transaction itself. Queueing a
	// curator to curate that curator would recurse forever; CurationSubmit binds
	// its report directly to the resulting intake instead.
	if run.Role == "curator" {
		return request, nil
	}
	queue := continuousCurationWork(run, request.Actor, request.CorrelationID)
	for _, work := range request.WorkItems {
		if work.ID == queue.ID {
			return request, fmt.Errorf("run.return curation queue id %s is system-owned", queue.ID)
		}
	}
	request.WorkItems = append(append([]WorkItemRecord(nil), request.WorkItems...), queue)
	run.SpawnedWorkItemIDs = SortedUnique(append(run.SpawnedWorkItemIDs, queue.ID))
	request.Runs = []RunRecord{run}
	return request, nil
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
	values := []any{}
	if request.Campaign != nil {
		values = append(values, *request.Campaign)
	}
	for _, value := range request.WorkItems {
		values = append(values, value)
	}
	for _, value := range request.Runs {
		if err := verifyRunHandles(boundary, request.CampaignSlug, value, artifactByPath); err != nil {
			return nil, "", err
		}
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
	writes := make([]StateWrite, 0, len(values))
	reviewHandle := ""
	for _, value := range values {
		id, revision, _, campaignID, correlationID, err := stateRecordIdentity(value, 0, request.CorrelationID)
		if err != nil {
			return nil, "", err
		}
		if campaignID != request.CampaignID ||
			(request.Action != "reconcile.import" && correlationID != request.CorrelationID) {
			return nil, "", fmt.Errorf("record %s does not bind the requested campaign and correlation", id)
		}
		path, err := stateRecordPath(request.CampaignSlug, value)
		if err != nil {
			return nil, "", err
		}
		expectedRevision := revision - 1
		expectedDigest := ""
		if request.Action == "reconcile.import" {
			expectedRevision = revision
		}
		if expectedRevision > 0 {
			expectedDigest = request.ExpectedRecordDigests[id]
			if !digestRE.MatchString(expectedDigest) {
				return nil, "", fmt.Errorf("record %s requires its exact expected digest", id)
			}
		}
		writes = append(writes, StateWrite{
			Path: path, ExpectedRevision: expectedRevision, ExpectedDigest: expectedDigest, Record: value,
		})
		if _, ok := value.(ReviewRecord); ok {
			reviewHandle = "record:" + path
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
	ExpectedHeadRevision  int64               `json:"expectedHeadRevision"`
	ExpectedHeadDigest    string              `json:"expectedHeadDigest"`
	ExpectedRecordDigests map[string]string   `json:"expectedRecordDigests,omitempty"`
	Intake                IntakeRecord        `json:"intake"`
	Candidates            []FindingSubmission `json:"candidates"`
	Rows                  []CurationRow       `json:"rows"`
	CuratorRun            *RunRecord          `json:"curatorRun,omitempty"`
	WorkItems             []WorkItemRecord    `json:"-"`
}

func (service *Service) CurationSubmit(ctx context.Context, request CurationSubmitRequest) (StateTransactionReceipt, error) {
	if service == nil {
		return StateTransactionReceipt{}, errors.New("service is required")
	}
	packet := CurationPacket{Intake: request.Intake, Rows: append([]CurationRow(nil), request.Rows...)}
	for _, candidate := range request.Candidates {
		document := candidate.Document()
		expectedPath, pathErr := stateRecordPath(request.CampaignSlug, document)
		if pathErr != nil {
			return StateTransactionReceipt{}, pathErr
		}
		if document.Record.Path != expectedPath {
			return StateTransactionReceipt{}, fmt.Errorf(
				"candidate %s path must be canonical %s", document.Record.ID, expectedPath)
		}
		packet.Candidates = append(packet.Candidates, document)
	}
	if err := ValidateCurationPacket("curator", packet); err != nil {
		return StateTransactionReceipt{}, err
	}
	if request.CampaignID != request.Intake.CampaignID || request.CorrelationID != request.Intake.CorrelationID {
		return StateTransactionReceipt{}, errors.New("curation intake does not bind the requested campaign and correlation")
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if graph.Campaign.Status != "open" && graph.Campaign.Status != "closing" {
		return StateTransactionReceipt{}, errors.New("curation requires an open or closing campaign")
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
				"duplicate coverage target %s is not a canonical campaign finding", coverage.TargetID)
		}
	}
	if _, err := validateCurationGraphBindings(graph, packet, true); err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := verifyCanonicalIntakeCoverage(service.Boundary, request.Intake); err != nil {
		return StateTransactionReceipt{}, err
	}
	if len(request.WorkItems) != 0 {
		return StateTransactionReceipt{}, errors.New(
			"curators may propose spawned work-item ids in intake but only manager review may create work records")
	}
	values := []any{request.Intake}
	for _, candidate := range request.Candidates {
		values = append(values, candidate.Document())
	}
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
		id, revision, _, campaignID, correlationID, err := stateRecordIdentity(value, 0, request.CorrelationID)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		if campaignID != request.CampaignID || correlationID != request.CorrelationID {
			return StateTransactionReceipt{}, fmt.Errorf("curation record %s has mismatched campaign or correlation", id)
		}
		path, err := stateRecordPath(request.CampaignSlug, value)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		expected := revision - 1
		digest := ""
		if expected > 0 {
			digest = request.ExpectedRecordDigests[id]
			if !digestRE.MatchString(digest) {
				return StateTransactionReceipt{}, fmt.Errorf("curation record %s requires its exact expected digest", id)
			}
		}
		writes = append(writes, StateWrite{Path: path, ExpectedRevision: expected, ExpectedDigest: digest, Record: value})
	}
	return store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "curator", Action: "curation.submit",
		Rationale: request.Rationale, CorrelationID: request.CorrelationID,
		IdempotencyKey: request.IdempotencyKey, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, Writes: writes,
	})
}

func validateReturnedCuratorRunBinding(
	graph CampaignGraph,
	actor string,
	submitted RunRecord,
) (RunRecord, error) {
	canonical, ok := graph.Runs[submitted.ID]
	if !ok || canonical.Role != "curator" || canonical.Status != "returned" ||
		canonical.ActorID != actor || !reflect.DeepEqual(canonical, submitted) {
		return RunRecord{}, errors.New(
			"curatorRun must be an exact binding to the caller's canonical returned curator run")
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

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
