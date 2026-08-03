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
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
)

type journalOperation struct {
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	Stage          string `json:"stage"`
	Backup         string `json:"backup,omitempty"`
	Existed        bool   `json:"existed"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	ResultDigest   string `json:"resultDigest"`
	PreviousMode   uint32 `json:"previousMode,omitempty"`
	ResultMode     uint32 `json:"resultMode"`
}

type transactionJournal struct {
	SchemaVersion int                     `json:"schemaVersion"`
	ID            string                  `json:"id"`
	Phase         string                  `json:"phase"`
	RequestDigest string                  `json:"requestDigest"`
	PreviousHead  StateHead               `json:"previousHead"`
	ResultingHead StateHead               `json:"resultingHead"`
	Operations    []journalOperation      `json:"operations"`
	Receipt       StateTransactionReceipt `json:"receipt"`
	CreatedAt     string                  `json:"createdAt"`
	UpdatedAt     string                  `json:"updatedAt"`
	Digest        string                  `json:"digest"`
}

type publicationArtifact struct {
	Kind   string
	Target string
	Body   []byte
	Mode   fs.FileMode
}

func (store *StateStore) runPreparedTransaction(ctx context.Context, prepared preparedTransaction) (StateTransactionReceipt, error) {
	stateRoot, err := store.ensureStateRoot()
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	lockPath, err := containedOutputPath(stateRoot, "writer.lock")
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	lock, err := acquireStateWriter(ctx, lockPath)
	if err != nil {
		return StateTransactionReceipt{}, fmt.Errorf("acquire state writer: %w", err)
	}
	defer lock.Close()
	if err := ctx.Err(); err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := store.recoverTransactionsLocked(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	inventory, err := store.loadCommittedInventory(head)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	dirtyPaths, err := store.inventoryDrift(inventory)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if len(dirtyPaths) != 0 && prepared.Request.Action != "reconcile.import" {
		return StateTransactionReceipt{}, fmt.Errorf("%w: %s", ErrStateDirty, strings.Join(dirtyPaths, ", "))
	}
	if receipt, found, err := store.loadIdempotencyReceipt(prepared.Request.IdempotencyKey); err != nil {
		return StateTransactionReceipt{}, err
	} else if found {
		if receipt.RequestDigest != prepared.RequestDigest {
			return StateTransactionReceipt{}, ErrIdempotencyConflict
		}
		return receipt, nil
	}
	if head.Revision != prepared.Request.ExpectedHeadRevision || head.Digest != prepared.Request.ExpectedHeadDigest {
		return StateTransactionReceipt{}, fmt.Errorf("%w: expected head %d/%s, found %d/%s",
			ErrStateConflict, prepared.Request.ExpectedHeadRevision, prepared.Request.ExpectedHeadDigest,
			head.Revision, head.Digest)
	}
	if err := store.validateArtifactExpectations(prepared.Artifacts); err != nil {
		return StateTransactionReceipt{}, err
	}
	graph, err := store.loadGraphForTransaction(prepared.Request.CampaignSlug)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	previousCampaignEventID := ""
	if graph.Campaign != nil {
		previousCampaignEventID = graph.Campaign.LastEventID
	}
	now := store.Now().UTC()
	eventID := eventIDForRequest(prepared.Request)
	writes, err := store.augmentCampaignWrite(prepared, graph, eventID, RFC3339UTC(now))
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	nextGraph, err := store.validateAndApplyWrites(prepared.Request, graph, writes)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := nextGraph.Validate(); err != nil {
		return StateTransactionReceipt{}, fmt.Errorf("resulting campaign graph: %w", err)
	}
	mutationDigest, resultingStateDigest, err := transactionStateDigests(
		head, writes, prepared.Artifacts, prepared.Request.RetireActiveTree)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	affected := make([]string, 0, len(writes))
	for _, write := range writes {
		affected = append(affected, write.RecordID)
	}
	for _, artifact := range prepared.Artifacts {
		affected = append(affected, artifact.Path)
	}
	if prepared.Request.RetireActiveTree != "" {
		affected = append(affected, prepared.Request.RetireActiveTree)
	}
	affected = SortedUnique(affected)
	event := StateEvent{
		SchemaVersion: CampaignSchemaVersion, ID: eventID, Timestamp: RFC3339UTC(now),
		Actor: prepared.Request.Actor, Authority: prepared.Request.Authority,
		Action: prepared.Request.Action, AffectedIDs: affected,
		PreviousRevision: head.Revision, ResultingRevision: head.Revision + 1,
		IdempotencyKey: prepared.Request.IdempotencyKey,
		CorrelationID:  prepared.Request.CorrelationID, Rationale: prepared.Request.Rationale,
		ReviewHandle: prepared.Request.ReviewHandle, PreviousEventID: head.EventID,
		PreviousStateDigest: head.StateDigest, ResultingStateDigest: resultingStateDigest,
		MutationDigest: mutationDigest,
	}
	if err := sealStateEvent(&event); err != nil {
		return StateTransactionReceipt{}, err
	}
	eventPath := "active/" + prepared.Request.CampaignSlug + "/events/events.jsonl"
	resultingEventJournal := eventPath
	if prepared.Request.RetiredEventJournal != "" {
		resultingEventJournal = prepared.Request.RetiredEventJournal
	}
	eventBody, err := store.appendEventBody(eventPath, previousCampaignEventID, event)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	events, err := campaignEventsFromBody(eventBody, nextGraph)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	stateViewBody, err := RenderCampaignStateAt(nextGraph, now, events)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	stateViewDigest := "sha256:" + SHA256Bytes(stateViewBody)
	if prepared.Request.RetiredEventJournal != "" {
		target, err := store.canonicalOutputPath(prepared.Request.RetiredEventJournal)
		if err != nil {
			return StateTransactionReceipt{}, err
		}
		if _, exists, err := currentFileDigest(target); err != nil {
			return StateTransactionReceipt{}, err
		} else if exists {
			return StateTransactionReceipt{}, fmt.Errorf("%w: retired event journal already exists", ErrStateConflict)
		}
	}
	if prepared.Request.Action == "reconcile.import" {
		if err := requireReconciledInventoryDrift(dirtyPaths, writes, prepared.Artifacts, resultingEventJournal); err != nil {
			return StateTransactionReceipt{}, err
		}
	}
	resultingInventory, err := store.resultingStateInventory(
		inventory, head.Revision+1, writes, prepared.Artifacts, resultingEventJournal,
		eventBody, prepared.Request.RetireActiveTree,
	)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	inventoryBody, err := canonicalJSON(resultingInventory)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	resultingHead := StateHead{
		SchemaVersion: CampaignSchemaVersion, Revision: head.Revision + 1,
		EventID: event.ID, EventDigest: event.Digest, StateDigest: resultingStateDigest,
		InventoryDigest: resultingInventory.Digest,
		TransactionID:   prepared.TransactionID, EventJournal: resultingEventJournal,
		UpdatedAt: RFC3339UTC(now),
	}
	if err := sealStateHead(&resultingHead); err != nil {
		return StateTransactionReceipt{}, err
	}
	recordResults := make([]StateRecordResult, 0, len(writes))
	var artifactResults []StateArtifactResult
	if len(prepared.Artifacts) != 0 {
		artifactResults = make([]StateArtifactResult, 0, len(prepared.Artifacts))
	}
	artifacts := make([]publicationArtifact, 0, len(writes)+len(prepared.Artifacts)+3)
	for _, write := range writes {
		recordResults = append(recordResults, StateRecordResult{
			Path: write.Path, RecordID: write.RecordID, Revision: write.Revision,
			RecordDigest: write.RecordDigest, ContentDigest: write.ContentDigest,
		})
		artifacts = append(artifacts, publicationArtifact{
			Kind: "record", Target: write.Path, Body: write.Body, Mode: 0o644,
		})
	}
	for _, artifact := range prepared.Artifacts {
		artifactResults = append(artifactResults, StateArtifactResult{
			Path: artifact.Path, PreviousDigest: artifact.ExpectedDigest, ContentDigest: artifact.ContentDigest,
		})
		artifacts = append(artifacts, publicationArtifact{
			Kind: "artifact", Target: artifact.Path, Body: artifact.Body, Mode: 0o644,
		})
	}
	stateViewPath := "active/" + prepared.Request.CampaignSlug + "/STATE.md"
	artifacts = append(artifacts, publicationArtifact{
		Kind: "derived", Target: stateViewPath, Body: stateViewBody, Mode: 0o644,
	})
	artifacts = append(artifacts, publicationArtifact{Kind: "event", Target: resultingEventJournal, Body: eventBody, Mode: 0o644})
	artifacts = append(artifacts, publicationArtifact{Kind: "inventory", Target: stateInventoryPath, Body: inventoryBody, Mode: 0o600})
	receipt := StateTransactionReceipt{
		SchemaVersion: CampaignSchemaVersion, TransactionID: prepared.TransactionID,
		CorrelationID: prepared.Request.CorrelationID, IdempotencyKey: prepared.Request.IdempotencyKey,
		RequestDigest: prepared.RequestDigest, PreviousHead: head, ResultingHead: resultingHead,
		Event: event, Records: recordResults, Artifacts: artifactResults,
		RetiredTree: prepared.Request.RetireActiveTree, GeneratedViewDigest: stateViewDigest,
		CommittedAt: RFC3339UTC(now),
	}
	if err := sealTransactionReceipt(&receipt); err != nil {
		return StateTransactionReceipt{}, err
	}
	receiptBody, err := canonicalJSON(receipt)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	receiptTarget := ".re-discipline/state/receipts/" + SHA256String(prepared.Request.IdempotencyKey) + ".json"
	artifacts = append(artifacts, publicationArtifact{Kind: "receipt", Target: receiptTarget, Body: receiptBody, Mode: 0o600})
	if prepared.Request.RetireActiveTree != "" {
		artifacts = append(artifacts, publicationArtifact{
			Kind: "retire-tree", Target: prepared.Request.RetireActiveTree, Mode: 0o700,
		})
	}
	journal, journalPath, err := store.prepareTransactionJournal(prepared, head, resultingHead, receipt, artifacts)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := store.hitFailpoint(FailAfterJournal, prepared.TransactionID, journalPath, -1); err != nil {
		return StateTransactionReceipt{}, err
	}
	for index, operation := range journal.Operations {
		if err := ctx.Err(); err != nil {
			return StateTransactionReceipt{}, err
		}
		if err := store.publishJournalOperation(operation); err != nil {
			return StateTransactionReceipt{}, err
		}
		point := FailAfterRecordPublish
		if operation.Kind == "event" {
			point = FailAfterEventPublish
		}
		if operation.Kind == "inventory" {
			point = FailAfterInventoryPublish
		}
		if operation.Kind == "receipt" {
			point = FailAfterReceiptPublish
		}
		if operation.Kind == "retire-tree" {
			point = FailAfterTreeRetire
		}
		if err := store.hitFailpoint(point, prepared.TransactionID, operation.Target, index); err != nil {
			return StateTransactionReceipt{}, err
		}
	}
	if err := store.hitFailpoint(FailBeforeHeadPublish, prepared.TransactionID, ".re-discipline/state/head.json", -1); err != nil {
		return StateTransactionReceipt{}, err
	}
	headPath, err := store.statePath("head.json")
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := durableAtomicWriteJSON(headPath, resultingHead, 0o600); err != nil {
		return StateTransactionReceipt{}, err
	}
	if err := store.hitFailpoint(FailAfterHeadPublish, prepared.TransactionID, ".re-discipline/state/head.json", -1); err != nil {
		return StateTransactionReceipt{}, err
	}
	journal.Phase, journal.UpdatedAt = "committed", RFC3339UTC(store.Now())
	if err := store.writeTransactionJournal(journalPath, &journal); err != nil {
		return StateTransactionReceipt{}, err
	}
	store.cleanupJournalStaging(journal)
	return receipt, nil
}

func (store *StateStore) hitFailpoint(name, transactionID, path string, index int) error {
	if store.Failpoint == nil {
		return nil
	}
	if err := store.Failpoint(StateFailpoint{Name: name, TransactionID: transactionID, Path: path, Index: index}); err != nil {
		return fmt.Errorf("state failpoint %s: %w", name, err)
	}
	return nil
}

func (store *StateStore) loadGraphForTransaction(slug string) (CampaignGraph, error) {
	campaignPath := filepath.Join(store.Boundary.Root, "active", slug, "campaign.json")
	if _, err := os.Stat(campaignPath); os.IsNotExist(err) {
		return NewCampaignGraph(), nil
	} else if err != nil {
		return CampaignGraph{}, err
	}
	return store.LoadCampaignGraph(slug)
}

func (store *StateStore) augmentCampaignWrite(prepared preparedTransaction, graph CampaignGraph, eventID, timestamp string) ([]preparedStateWrite, error) {
	writes := append([]preparedStateWrite(nil), prepared.Writes...)
	for index := range writes {
		campaign, ok := writes[index].Record.(CampaignRecord)
		if !ok {
			continue
		}
		if prepared.Request.Action == "reconcile.import" {
			// A campaign carries transaction-owned event-head fields, so an
			// explicitly imported out-of-band campaign cannot remain byte-exact.
			// Advance it from the exact submitted revision while preserving the
			// manager-selected content; all other imported record types remain at
			// the submitted revision.
			campaign.Revision++
		}
		campaign.LastEventID, campaign.UpdatedAt = eventID, timestamp
		campaign.UpdatedBy, campaign.CorrelationID = prepared.Request.Actor, prepared.Request.CorrelationID
		sealed, err := prepareStateWrite(prepared.Request.CampaignSlug, prepared.Request.CampaignID,
			prepared.Request.CorrelationID, eventID, prepared.Request.ExpectedHeadRevision+1,
			StateWrite{Path: writes[index].Path, ExpectedRevision: writes[index].ExpectedRevision,
				ExpectedDigest: writes[index].ExpectedDigest, Record: campaign})
		if err != nil {
			return nil, err
		}
		writes[index] = sealed
		sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
		return writes, nil
	}
	if graph.Campaign == nil {
		return nil, errors.New("opening transaction must include campaign.json")
	}
	campaign := *graph.Campaign
	campaign.Revision++
	campaign.UpdatedAt, campaign.UpdatedBy = timestamp, prepared.Request.Actor
	campaign.CorrelationID, campaign.LastEventID = prepared.Request.CorrelationID, eventID
	path := "active/" + prepared.Request.CampaignSlug + "/campaign.json"
	sealed, err := prepareStateWrite(prepared.Request.CampaignSlug, prepared.Request.CampaignID,
		prepared.Request.CorrelationID, eventID, prepared.Request.ExpectedHeadRevision+1,
		StateWrite{Path: path, ExpectedRevision: graph.Campaign.Revision,
			ExpectedDigest: graph.Campaign.Digest, Record: campaign})
	if err != nil {
		return nil, err
	}
	sealed.Internal = true
	writes = append(writes, sealed)
	sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
	return writes, nil
}

func (store *StateStore) validateAndApplyWrites(request StateTransactionRequest, graph CampaignGraph, writes []preparedStateWrite) (CampaignGraph, error) {
	next := cloneCampaignGraph(graph)
	for _, write := range writes {
		previous, handle, exists, err := store.existingRecord(write.Path)
		if err != nil {
			return CampaignGraph{}, err
		}
		if write.ExpectedRevision == 0 {
			if exists {
				return CampaignGraph{}, fmt.Errorf("%w: record %s already exists", ErrStateConflict, write.Path)
			}
		} else {
			if !exists {
				return CampaignGraph{}, fmt.Errorf("%w: record %s is missing", ErrStateConflict, write.Path)
			}
			if handle.RecordDigest != write.ExpectedDigest || (handle.Revision > 0 && handle.Revision != write.ExpectedRevision) {
				return CampaignGraph{}, fmt.Errorf("%w: record %s expected revision/digest does not match", ErrStateConflict, write.Path)
			}
		}
		if !write.Internal {
			if err := authorizeStateWrite(request, previous, write.Record); err != nil {
				return CampaignGraph{}, err
			}
		}
		reconciledExactRecord := request.Action == "reconcile.import" && previous != nil &&
			reflect.DeepEqual(previous, write.Record)
		if !reconciledExactRecord {
			if err := validateRecordTransition(previous, write.Record, request.Action, request.Authority); err != nil {
				return CampaignGraph{}, fmt.Errorf("record %s: %w", write.RecordID, err)
			}
		}
		applyRecordToGraph(&next, write.Record)
	}
	if graph.Campaign != nil && graph.Campaign.Status == "closing" {
		for _, write := range writes {
			if _, isRun := write.Record.(RunRecord); isRun {
				if _, _, exists, _ := store.existingRecord(write.Path); !exists && request.Action != "closure.remediation.run.create" {
					return CampaignGraph{}, errors.New("ordinary runs cannot be created while campaign is closing")
				}
			}
		}
	}
	if request.Action == "review.submit" || request.Action == "decision.record" {
		if err := validateAppliedManagerReview(store, graph, writes, request.ReviewPacket); err != nil {
			return CampaignGraph{}, err
		}
	}
	if request.Action == "run.return" {
		if err := validateAppliedRunReturn(graph, request, writes); err != nil {
			return CampaignGraph{}, err
		}
	}
	return next, nil
}

func (store *StateStore) validateArtifactExpectations(artifacts []preparedStateArtifact) error {
	for _, artifact := range artifacts {
		target, err := store.canonicalOutputPath(artifact.Path)
		if err != nil {
			return err
		}
		digest, exists, err := currentFileDigest(target)
		if err != nil {
			return err
		}
		if artifact.ExpectedDigest == "" {
			if exists {
				return fmt.Errorf("%w: artifact %s already exists", ErrStateConflict, artifact.Path)
			}
			continue
		}
		if !exists || digest != artifact.ExpectedDigest {
			return fmt.Errorf("%w: artifact %s expected digest does not match", ErrStateConflict, artifact.Path)
		}
	}
	return nil
}

func cloneCampaignGraph(graph CampaignGraph) CampaignGraph {
	next := NewCampaignGraph()
	if graph.Campaign != nil {
		campaign := *graph.Campaign
		next.Campaign = &campaign
	}
	for id, value := range graph.WorkItems {
		next.WorkItems[id] = value
	}
	for id, value := range graph.Runs {
		next.Runs[id] = value
	}
	for id, value := range graph.Findings {
		next.Findings[id] = value
	}
	for id, value := range graph.Intakes {
		next.Intakes[id] = value
	}
	for id, value := range graph.Reviews {
		next.Reviews[id] = value
	}
	if graph.ClosurePlan != nil {
		value := *graph.ClosurePlan
		next.ClosurePlan = &value
	}
	if graph.ClosureJob != nil {
		value := *graph.ClosureJob
		next.ClosureJob = &value
	}
	if graph.ClosureCoverage != nil {
		value := *graph.ClosureCoverage
		next.ClosureCoverage = &value
	}
	if graph.ClosureReceipt != nil {
		value := *graph.ClosureReceipt
		next.ClosureReceipt = &value
	}
	return next
}

func (store *StateStore) existingRecord(path string) (any, CanonicalRecordHandle, bool, error) {
	if _, err := os.Stat(filepath.Join(store.Boundary.Root, filepath.FromSlash(path))); os.IsNotExist(err) {
		return nil, CanonicalRecordHandle{}, false, nil
	} else if err != nil {
		return nil, CanonicalRecordHandle{}, false, err
	}
	_, value, handle, err := store.readCanonicalRecordValue(path)
	return value, handle, err == nil, err
}

func authorizeStateWrite(request StateTransactionRequest, previous, next any) error {
	if request.Authority == "manager" || request.Authority == "system" {
		return nil
	}
	switch value := next.(type) {
	case RunRecord:
		if value.ActorID != request.Actor {
			return errors.New("actor may mutate only its own run")
		}
		if previous == nil {
			return errors.New("only a manager may create a run")
		}
		before := previous.(RunRecord)
		if !((before.Status == "prepared" && value.Status == "running") ||
			(before.Status == "running" && validOne(value.Status, "returned", "aborted"))) {
			return errors.New("worker authority may only start or return its own run")
		}
		return nil
	case FindingDocument:
		if request.Authority != "curator" || validOne(value.Record.ReviewState, "manager-ratified", "manager-rejected") || value.Record.Validity == "current" {
			return errors.New("non-manager finding mutation exceeds curator authority")
		}
		return nil
	case IntakeRecord:
		if request.Authority != "curator" {
			return errors.New("only curator or manager may write intake")
		}
		return nil
	default:
		return errors.New("declared authority cannot mutate this canonical record type")
	}
}

func validateRecordTransition(previous, next any, action, authority string) error {
	switch value := next.(type) {
	case CampaignRecord:
		var prior *CampaignRecord
		if previous != nil {
			item := previous.(CampaignRecord)
			prior = &item
		}
		return ValidateCampaignTransition(prior, value, action)
	case WorkItemRecord:
		var prior *WorkItemRecord
		if previous != nil {
			item := previous.(WorkItemRecord)
			prior = &item
		}
		return ValidateWorkItemTransition(prior, value)
	case RunRecord:
		var prior *RunRecord
		if previous != nil {
			item := previous.(RunRecord)
			prior = &item
		}
		return ValidateRunTransition(prior, value)
	case FindingDocument:
		var prior *FindingRecord
		if previous != nil {
			item := previous.(FindingDocument).Record
			prior = &item
		}
		return ValidateFindingTransition(prior, value.Record, action, authority)
	case IntakeRecord:
		var prior *IntakeRecord
		if previous != nil {
			item := previous.(IntakeRecord)
			prior = &item
		}
		return ValidateIntakeTransition(prior, value)
	case ReviewRecord:
		var prior *ReviewRecord
		if previous != nil {
			item := previous.(ReviewRecord)
			prior = &item
		}
		return ValidateReviewTransition(prior, value)
	case ClosurePlan:
		if previous != nil {
			return errors.New("closure plans are immutable")
		}
		return ValidateClosurePlan(value)
	case ClosureJob:
		if previous == nil {
			if value.Revision != 1 || value.Stage != "inventory" {
				return errors.New("closure job must begin at inventory revision 1")
			}
			return ValidateClosureJob(value)
		}
		prior := previous.(ClosureJob)
		if err := validateMetaTransition(prior.RecordMeta, value.RecordMeta); err != nil {
			return err
		}
		if prior.Stage == value.Stage {
			return ValidateClosureJob(value)
		}
		return ValidateClosureAdvance(prior, value)
	case ClosureCoverage:
		return ValidateClosureCoverage(value)
	case ClosureReceipt:
		if previous != nil {
			return errors.New("closure receipt is immutable")
		}
		return ValidateClosureReceipt(value)
	case ArchiveManifest:
		if previous != nil {
			return errors.New("archive manifest is immutable")
		}
		return ValidateArchiveManifest(value)
	default:
		return fmt.Errorf("unsupported record transition %T", next)
	}
}

func applyRecordToGraph(graph *CampaignGraph, value any) {
	switch record := value.(type) {
	case CampaignRecord:
		graph.Campaign = &record
	case WorkItemRecord:
		graph.WorkItems[record.ID] = record
	case RunRecord:
		graph.Runs[record.ID] = record
	case FindingDocument:
		graph.Findings[record.Record.ID] = record.Record
	case IntakeRecord:
		graph.Intakes[record.ID] = record
	case ReviewRecord:
		graph.Reviews[record.ID] = record
	case ClosurePlan:
		graph.ClosurePlan = &record
	case ClosureJob:
		graph.ClosureJob = &record
	case ClosureCoverage:
		graph.ClosureCoverage = &record
	case ClosureReceipt:
		graph.ClosureReceipt = &record
	}
}

func transactionStateDigests(head StateHead, writes []preparedStateWrite, artifacts []preparedStateArtifact, retiredTree string) (string, string, error) {
	descriptors := make([]any, 0, len(writes)+len(artifacts)+1)
	for _, write := range writes {
		descriptors = append(descriptors, struct {
			Kind   string            `json:"kind"`
			Record StateRecordResult `json:"record"`
		}{"record", StateRecordResult{Path: write.Path, RecordID: write.RecordID,
			Revision: write.Revision, RecordDigest: write.RecordDigest, ContentDigest: write.ContentDigest}})
	}
	for _, artifact := range artifacts {
		descriptors = append(descriptors, struct {
			Kind     string              `json:"kind"`
			Artifact StateArtifactResult `json:"artifact"`
		}{"artifact", StateArtifactResult{
			Path: artifact.Path, PreviousDigest: artifact.ExpectedDigest, ContentDigest: artifact.ContentDigest,
		}})
	}
	if retiredTree != "" {
		descriptors = append(descriptors, struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}{"retire-tree", retiredTree})
	}
	mutationDigest, err := CanonicalDigest(descriptors)
	if err != nil {
		return "", "", err
	}
	stateDigest, err := CanonicalDigest(struct {
		Previous string `json:"previous"`
		Mutation string `json:"mutation"`
	}{head.StateDigest, mutationDigest})
	return mutationDigest, stateDigest, err
}

func sealStateEvent(event *StateEvent) error {
	event.AffectedIDs = SortedUnique(event.AffectedIDs)
	event.Digest = ""
	digest, err := CanonicalDigest(*event)
	if err != nil {
		return err
	}
	event.Digest = digest
	return ValidateEvent(*event)
}

func sealTransactionReceipt(receipt *StateTransactionReceipt) error {
	receipt.ResultDigest = ""
	digest, err := CanonicalDigest(*receipt)
	if err != nil {
		return err
	}
	receipt.ResultDigest = digest
	if receipt.SchemaVersion != CampaignSchemaVersion || !correlationIDRE.MatchString(receipt.TransactionID) ||
		!correlationIDRE.MatchString(receipt.CorrelationID) || !digestRE.MatchString(receipt.RequestDigest) ||
		!digestRE.MatchString(receipt.ResultDigest) || !digestRE.MatchString(receipt.GeneratedViewDigest) ||
		receipt.ResultingHead.Revision != receipt.PreviousHead.Revision+1 {
		return errors.New("transaction receipt is invalid")
	}
	seenArtifacts := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		if seenArtifacts[artifact.Path] || validateRelativeRecordPath(artifact.Path) != nil ||
			!digestRE.MatchString(artifact.ContentDigest) ||
			(artifact.PreviousDigest != "" && !digestRE.MatchString(artifact.PreviousDigest)) {
			return errors.New("transaction receipt artifact is invalid")
		}
		seenArtifacts[artifact.Path] = true
	}
	if receipt.RetiredTree != "" {
		parts := strings.Split(receipt.RetiredTree, "/")
		if len(parts) != 2 || parts[0] != "active" || !managedSlugRE.MatchString(parts[1]) ||
			receipt.Event.Action != "closure.finalize" {
			return errors.New("transaction receipt active-tree retirement is invalid")
		}
	}
	return nil
}

func verifyTransactionReceipt(receipt StateTransactionReceipt) error {
	want := receipt.ResultDigest
	if err := sealTransactionReceipt(&receipt); err != nil {
		return err
	}
	if receipt.ResultDigest != want {
		return errors.New("transaction receipt digest does not verify")
	}
	return nil
}

func (store *StateStore) appendEventBody(relative, previousCampaignEventID string, event StateEvent) ([]byte, error) {
	absolute := filepath.Join(store.Boundary.Root, filepath.FromSlash(relative))
	body, err := readSingleLinkRegularFile(absolute)
	if os.IsNotExist(err) {
		body = nil
	} else if err != nil {
		return nil, err
	}
	lastID := ""
	for index, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var existing StateEvent
		if err := decodeStrictJSON([]byte(line), &existing); err != nil || verifyStateEvent(existing) != nil {
			return nil, fmt.Errorf("event journal line %d is invalid", index+1)
		}
		lastID = existing.ID
	}
	if lastID != previousCampaignEventID {
		return nil, ErrStateDirty
	}
	line, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if len(body) != 0 && body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	body = append(body, line...)
	body = append(body, '\n')
	return body, nil
}

func campaignEventsFromBody(body []byte, graph CampaignGraph) ([]StateEvent, error) {
	result := []StateEvent{}
	seen := map[string]bool{}
	pending := defermentEventTriggerKeys(graph)
	var previousRevision int64 = -1
	for index, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event StateEvent
		if err := decodeStrictJSON([]byte(line), &event); err != nil || verifyStateEvent(event) != nil {
			return nil, fmt.Errorf("event journal line %d is invalid", index+1)
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
	return result, nil
}

func verifyStateEvent(event StateEvent) error {
	want := event.Digest
	if err := sealStateEvent(&event); err != nil {
		return err
	}
	if event.Digest != want {
		return errors.New("event digest does not verify")
	}
	return nil
}

func (store *StateStore) prepareTransactionJournal(
	prepared preparedTransaction,
	previousHead, resultingHead StateHead,
	receipt StateTransactionReceipt,
	artifacts []publicationArtifact,
) (transactionJournal, string, error) {
	operations := make([]journalOperation, 0, len(artifacts))
	for index, artifact := range artifacts {
		operation, err := store.prepareJournalOperation(
			prepared.TransactionID, index, artifact, artifacts[:index])
		if err != nil {
			store.removeTransactionStaging(prepared.TransactionID)
			return transactionJournal{}, "", err
		}
		operations = append(operations, operation)
	}
	now := RFC3339UTC(store.Now())
	journal := transactionJournal{
		SchemaVersion: CampaignSchemaVersion, ID: prepared.TransactionID,
		Phase: "prepared", RequestDigest: prepared.RequestDigest,
		PreviousHead: previousHead, ResultingHead: resultingHead,
		Operations: operations, Receipt: receipt, CreatedAt: now, UpdatedAt: now,
	}
	journalPath, err := store.statePath("journal/" + prepared.TransactionID + ".json")
	if err != nil {
		store.removeTransactionStaging(prepared.TransactionID)
		return transactionJournal{}, "", err
	}
	if err := store.writeTransactionJournal(journalPath, &journal); err != nil {
		store.removeTransactionStaging(prepared.TransactionID)
		return transactionJournal{}, "", err
	}
	return journal, journalPath, nil
}

func (store *StateStore) prepareJournalOperation(transactionID string, index int, artifact publicationArtifact, prior []publicationArtifact) (journalOperation, error) {
	target, err := store.canonicalOutputPath(artifact.Target)
	if err != nil {
		return journalOperation{}, err
	}
	if artifact.Kind == "retire-tree" {
		info, err := os.Lstat(target)
		if err != nil {
			return journalOperation{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return journalOperation{}, fmt.Errorf("retirement target %s is not a real directory", artifact.Target)
		}
		previousDigest, err := digestDirectoryTree(target)
		if err != nil {
			return journalOperation{}, err
		}
		resultDigest, err := digestDirectoryTreeWithArtifacts(target, artifact.Target, prior)
		if err != nil {
			return journalOperation{}, err
		}
		stage := fmt.Sprintf("staging/%s/retired-tree", transactionID)
		stagePath, err := store.statePath(stage)
		if err != nil {
			return journalOperation{}, err
		}
		if _, err := os.Lstat(stagePath); err == nil || !os.IsNotExist(err) {
			return journalOperation{}, errors.New("transaction retirement staging already exists")
		}
		if err := os.MkdirAll(filepath.Dir(stagePath), 0o700); err != nil {
			return journalOperation{}, err
		}
		return journalOperation{
			Kind: "retire-tree", Target: artifact.Target, Stage: stage,
			Existed: true, PreviousDigest: previousDigest, ResultDigest: resultDigest,
			PreviousMode: uint32(info.Mode().Perm()), ResultMode: uint32(info.Mode().Perm()),
		}, nil
	}
	operation := journalOperation{
		Kind: artifact.Kind, Target: artifact.Target,
		Stage:        fmt.Sprintf("staging/%s/new/%04d", transactionID, index),
		ResultDigest: "sha256:" + SHA256Bytes(artifact.Body),
		ResultMode:   uint32(artifact.Mode.Perm()),
	}
	info, err := os.Lstat(target)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return journalOperation{}, fmt.Errorf("transaction target %s is not a real regular file", artifact.Target)
		}
		file, openErr := os.Open(target)
		if openErr != nil {
			return journalOperation{}, openErr
		}
		multiple, linkErr := writerFileHasMultipleLinks(file)
		_ = file.Close()
		if linkErr != nil || multiple {
			return journalOperation{}, fmt.Errorf("transaction target %s has an unsafe link count", artifact.Target)
		}
		previous, readErr := readSingleLinkRegularFile(target)
		if readErr != nil {
			return journalOperation{}, readErr
		}
		operation.Existed = true
		operation.PreviousDigest = "sha256:" + SHA256Bytes(previous)
		operation.PreviousMode = uint32(info.Mode().Perm())
		operation.Backup = fmt.Sprintf("staging/%s/backup/%04d", transactionID, index)
		backupPath, pathErr := store.statePath(operation.Backup)
		if pathErr != nil {
			return journalOperation{}, pathErr
		}
		if writeErr := durableAtomicWrite(backupPath, previous, 0o600); writeErr != nil {
			return journalOperation{}, writeErr
		}
	case os.IsNotExist(err):
	case err != nil:
		return journalOperation{}, err
	}
	stagePath, err := store.statePath(operation.Stage)
	if err != nil {
		return journalOperation{}, err
	}
	if err := durableAtomicWrite(stagePath, artifact.Body, 0o600); err != nil {
		return journalOperation{}, err
	}
	return operation, nil
}

func sealTransactionJournal(journal *transactionJournal) error {
	journal.Digest = ""
	digest, err := CanonicalDigest(*journal)
	if err != nil {
		return err
	}
	journal.Digest = digest
	if journal.SchemaVersion != CampaignSchemaVersion || !correlationIDRE.MatchString(journal.ID) ||
		!validOne(journal.Phase, "prepared", "committed", "rolled-back") ||
		!digestRE.MatchString(journal.RequestDigest) || !digestRE.MatchString(journal.Digest) ||
		len(journal.Operations) == 0 {
		return errors.New("transaction journal identity, phase, or operations are invalid")
	}
	if err := verifyStateHead(journal.PreviousHead); err != nil {
		return err
	}
	if err := verifyStateHead(journal.ResultingHead); err != nil {
		return err
	}
	if err := verifyTransactionReceipt(journal.Receipt); err != nil {
		return err
	}
	for _, operation := range journal.Operations {
		if !validOne(operation.Kind, "record", "artifact", "derived", "event", "inventory", "receipt", "retire-tree") ||
			!digestRE.MatchString(operation.ResultDigest) || operation.ResultMode == 0 {
			return errors.New("transaction journal operation is invalid")
		}
		if err := validateRelativeRecordPath(operation.Target); err != nil {
			return err
		}
		if err := validateRelativeRecordPath(operation.Stage); err != nil {
			return err
		}
		if operation.Kind == "retire-tree" {
			if !operation.Existed || operation.Backup != "" ||
				!digestRE.MatchString(operation.PreviousDigest) || operation.PreviousMode == 0 {
				return errors.New("transaction retirement metadata is invalid")
			}
			continue
		}
		if operation.Existed {
			if !digestRE.MatchString(operation.PreviousDigest) || operation.Backup == "" || operation.PreviousMode == 0 {
				return errors.New("transaction journal backup metadata is invalid")
			}
			if err := validateRelativeRecordPath(operation.Backup); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyTransactionJournal(journal transactionJournal) error {
	want := journal.Digest
	if err := sealTransactionJournal(&journal); err != nil {
		return err
	}
	if journal.Digest != want {
		return errors.New("transaction journal digest does not verify")
	}
	return nil
}

func (store *StateStore) writeTransactionJournal(path string, journal *transactionJournal) error {
	if err := sealTransactionJournal(journal); err != nil {
		return err
	}
	return durableAtomicWriteJSON(path, journal, 0o600)
}

func (store *StateStore) publishJournalOperation(operation journalOperation) error {
	if operation.Kind == "retire-tree" {
		return store.publishTreeRetirement(operation)
	}
	stagePath, err := store.statePath(operation.Stage)
	if err != nil {
		return err
	}
	body, err := readSingleLinkRegularFile(stagePath)
	if err != nil {
		return err
	}
	if "sha256:"+SHA256Bytes(body) != operation.ResultDigest {
		return errors.New("staged transaction content digest does not verify")
	}
	target, err := store.canonicalOutputPath(operation.Target)
	if err != nil {
		return err
	}
	if err := durableAtomicWrite(target, body, fs.FileMode(operation.ResultMode)); err != nil {
		return err
	}
	return verifyFileDigest(target, operation.ResultDigest)
}

func (store *StateStore) publishTreeRetirement(operation journalOperation) error {
	target, err := store.canonicalOutputPath(operation.Target)
	if err != nil {
		return err
	}
	stage, err := store.statePath(operation.Stage)
	if err != nil {
		return err
	}
	targetInfo, targetErr := os.Lstat(target)
	stageInfo, stageErr := os.Lstat(stage)
	switch {
	case targetErr == nil && stageErr == nil:
		return fmt.Errorf("%w: retirement target and staging both exist", ErrStateDirty)
	case targetErr == nil:
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
			return errors.New("retirement target is not a real directory")
		}
		digest, err := digestDirectoryTree(target)
		if err != nil || digest != operation.ResultDigest {
			return fmt.Errorf("%w: retirement target digest changed (got %s, want %s)", ErrStateDirty, digest, operation.ResultDigest)
		}
		if err := os.Rename(target, stage); err != nil {
			return err
		}
		if err := syncTransactionDirectory(filepath.Dir(target)); err != nil {
			return err
		}
		return syncTransactionDirectory(filepath.Dir(stage))
	case os.IsNotExist(targetErr) && stageErr == nil:
		if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
			return errors.New("retired tree staging is not a real directory")
		}
		digest, err := digestDirectoryTree(stage)
		if err != nil || digest != operation.ResultDigest {
			return fmt.Errorf("%w: retired tree staging digest changed", ErrStateDirty)
		}
		return nil
	case !os.IsNotExist(targetErr):
		return targetErr
	case !os.IsNotExist(stageErr):
		return stageErr
	default:
		return fmt.Errorf("%w: retirement target and staging are both missing", ErrStateDirty)
	}
}

type treeDigestEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Digest string `json:"digest,omitempty"`
}

func digestDirectoryTree(root string) (string, error) {
	entries, err := directoryTreeEntries(root)
	if err != nil {
		return "", err
	}
	return digestTreeEntries(entries)
}

func directoryTreeEntries(root string) (map[string]treeDigestEntry, error) {
	entries := map[string]treeDigestEntry{}
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("retirement tree contains a symbolic link")
		}
		item := treeDigestEntry{Path: filepath.ToSlash(relative), Mode: uint32(info.Mode().Perm())}
		switch {
		case entry.IsDir():
			item.Kind = "directory"
		case info.Mode().IsRegular():
			file, err := os.Open(current)
			if err != nil {
				return err
			}
			multiple, linkErr := writerFileHasMultipleLinks(file)
			_ = file.Close()
			if linkErr != nil || multiple {
				return errors.New("retirement tree contains an unsafe hard-linked file")
			}
			body, err := readSingleLinkRegularFile(current)
			if err != nil {
				return err
			}
			item.Kind, item.Digest = "file", "sha256:"+SHA256Bytes(body)
		default:
			return errors.New("retirement tree contains an unsupported filesystem entry")
		}
		entries[item.Path] = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func digestTreeEntries(entries map[string]treeDigestEntry) (string, error) {
	ordered := make([]treeDigestEntry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	return CanonicalDigest(ordered)
}

func digestDirectoryTreeWithArtifacts(root, target string, artifacts []publicationArtifact) (string, error) {
	entries, err := directoryTreeEntries(root)
	if err != nil {
		return "", err
	}
	prefix := target + "/"
	for _, artifact := range artifacts {
		if artifact.Kind == "retire-tree" || !strings.HasPrefix(artifact.Target, prefix) {
			continue
		}
		relative := strings.TrimPrefix(artifact.Target, prefix)
		parent := path.Dir(relative)
		for parent != "." && parent != "" {
			if _, present := entries[parent]; !present {
				entries[parent] = treeDigestEntry{Path: parent, Kind: "directory", Mode: effectivePublishedDirectoryMode(0o700)}
			}
			parent = path.Dir(parent)
		}
		entries[relative] = treeDigestEntry{
			Path: relative, Kind: "file", Mode: effectivePublishedFileMode(artifact.Mode),
			Digest: "sha256:" + SHA256Bytes(artifact.Body),
		}
	}
	return digestTreeEntries(entries)
}

func effectivePublishedFileMode(mode fs.FileMode) uint32 {
	if runtime.GOOS == "windows" {
		// Windows exposes non-read-only files through Go's synthetic 0666 mode.
		return uint32(0o666)
	}
	return uint32(mode.Perm())
}

func effectivePublishedDirectoryMode(mode fs.FileMode) uint32 {
	if runtime.GOOS == "windows" {
		return uint32(0o777)
	}
	return uint32(mode.Perm())
}

func verifyFileDigest(path, expected string) error {
	body, err := readSingleLinkRegularFile(path)
	if err != nil {
		return err
	}
	if "sha256:"+SHA256Bytes(body) != expected {
		return errors.New("published file digest does not verify")
	}
	return nil
}

func currentFileDigest(path string) (string, bool, error) {
	body, err := readSingleLinkRegularFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return "sha256:" + SHA256Bytes(body), true, nil
}

func (store *StateStore) loadIdempotencyReceipt(key string) (StateTransactionReceipt, bool, error) {
	path, err := store.statePath("receipts/" + SHA256String(key) + ".json")
	if err != nil {
		return StateTransactionReceipt{}, false, err
	}
	body, err := readSingleLinkRegularFile(path)
	if os.IsNotExist(err) {
		return StateTransactionReceipt{}, false, nil
	}
	if err != nil {
		return StateTransactionReceipt{}, false, err
	}
	var receipt StateTransactionReceipt
	if err := decodeStrictJSON(body, &receipt); err != nil {
		return StateTransactionReceipt{}, false, err
	}
	if receipt.IdempotencyKey != key {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	if err := verifyTransactionReceipt(receipt); err != nil {
		return StateTransactionReceipt{}, false, err
	}
	canonical, err := canonicalJSON(receipt)
	if err != nil || !strings.EqualFold("sha256:"+SHA256Bytes(body), "sha256:"+SHA256Bytes(canonical)) {
		return StateTransactionReceipt{}, false, errors.New("idempotency receipt is not canonical")
	}
	return receipt, true, nil
}

// Recover resolves every prepared journal before another mutation. It never
// guesses: an old head rolls back, a published new head rolls forward, and any
// third head is a dirty-state error.
func (store *StateStore) Recover(ctx context.Context) error {
	if store == nil {
		return errors.New("state store is required")
	}
	root, exists, err := store.existingStateRoot()
	if err != nil || !exists {
		return err
	}
	lockPath, err := containedOutputPath(root, "writer.lock")
	if err != nil {
		return err
	}
	lock, err := acquireStateWriter(ctx, lockPath)
	if err != nil {
		return fmt.Errorf("acquire state writer: %w", err)
	}
	defer lock.Close()
	return store.recoverTransactionsLocked(ctx)
}

func (store *StateStore) recoverTransactionsLocked(ctx context.Context) error {
	journalRoot, err := store.statePath("journal")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(journalRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return errors.New("transaction journal directory contains an unexpected entry")
		}
		path := filepath.Join(journalRoot, entry.Name())
		body, err := readSingleLinkRegularFile(path)
		if err != nil {
			return err
		}
		var journal transactionJournal
		if err := decodeStrictJSON(body, &journal); err != nil {
			return err
		}
		if err := verifyTransactionJournal(journal); err != nil {
			return err
		}
		if entry.Name() != journal.ID+".json" {
			return errors.New("transaction journal filename does not match its identity")
		}
		if journal.Phase != "prepared" {
			store.cleanupJournalStaging(journal)
			continue
		}
		if err := store.recoverTransactionJournal(path, &journal); err != nil {
			return err
		}
	}
	return nil
}

func (store *StateStore) recoverTransactionJournal(path string, journal *transactionJournal) error {
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	switch head.Digest {
	case journal.ResultingHead.Digest:
		retiredPrefix := ""
		for _, operation := range journal.Operations {
			if operation.Kind == "retire-tree" {
				if err := store.publishTreeRetirement(operation); err != nil {
					return err
				}
				retiredPrefix = operation.Target + "/"
				break
			}
		}
		for _, operation := range journal.Operations {
			if operation.Kind == "retire-tree" {
				continue
			}
			// The verified staged-tree digest commits every publication under
			// the retired prefix, whose canonical paths are intentionally gone.
			if retiredPrefix != "" && strings.HasPrefix(operation.Target, retiredPrefix) {
				continue
			}
			target, err := store.canonicalOutputPath(operation.Target)
			if err != nil {
				return err
			}
			digest, exists, err := currentFileDigest(target)
			if err != nil {
				return err
			}
			if exists && digest == operation.ResultDigest {
				continue
			}
			if operation.Existed {
				if !exists || digest != operation.PreviousDigest {
					return fmt.Errorf("%w: transaction target %s changed during roll-forward", ErrStateDirty, operation.Target)
				}
			} else if exists {
				return fmt.Errorf("%w: transaction target %s changed during roll-forward", ErrStateDirty, operation.Target)
			}
			if err := store.publishJournalOperation(operation); err != nil {
				return err
			}
		}
		journal.Phase = "committed"
	case journal.PreviousHead.Digest:
		var retiredOperation *journalOperation
		for index := len(journal.Operations) - 1; index >= 0; index-- {
			operation := journal.Operations[index]
			if operation.Kind == "retire-tree" {
				if err := store.rollbackTreeRetirement(operation); err != nil {
					return err
				}
				copy := operation
				retiredOperation = &copy
				continue
			}
			target, err := store.canonicalOutputPath(operation.Target)
			if err != nil {
				return err
			}
			digest, exists, err := currentFileDigest(target)
			if err != nil {
				return err
			}
			if operation.Existed {
				if !exists {
					return fmt.Errorf("%w: transaction target %s disappeared during rollback", ErrStateDirty, operation.Target)
				}
				if digest == operation.PreviousDigest {
					continue
				}
				if digest != operation.ResultDigest {
					return fmt.Errorf("%w: transaction target %s changed during rollback", ErrStateDirty, operation.Target)
				}
				backupPath, err := store.statePath(operation.Backup)
				if err != nil {
					return err
				}
				body, err := readSingleLinkRegularFile(backupPath)
				if err != nil {
					return err
				}
				if "sha256:"+SHA256Bytes(body) != operation.PreviousDigest {
					return errors.New("transaction backup digest does not verify")
				}
				if err := durableAtomicWrite(target, body, fs.FileMode(operation.PreviousMode)); err != nil {
					return err
				}
				if err := verifyFileDigest(target, operation.PreviousDigest); err != nil {
					return err
				}
			} else if !exists {
				if err := store.removeEmptyRolledBackRunWorkspace(operation.Target); err != nil {
					return err
				}
				continue
			} else if digest != operation.ResultDigest {
				return fmt.Errorf("%w: transaction target %s changed during rollback", ErrStateDirty, operation.Target)
			} else {
				if err := os.Remove(target); err != nil {
					return err
				}
				if err := syncTransactionDirectory(filepath.Dir(target)); err != nil {
					return err
				}
				if err := store.removeEmptyRolledBackRunWorkspace(operation.Target); err != nil {
					return err
				}
			}
		}
		if retiredOperation != nil {
			if err := store.verifyRolledBackTree(*retiredOperation); err != nil {
				return err
			}
		}
		journal.Phase = "rolled-back"
	default:
		return fmt.Errorf("%w: prepared transaction %s has neither its old nor new head", ErrStateDirty, journal.ID)
	}
	journal.UpdatedAt = RFC3339UTC(store.Now())
	if err := store.writeTransactionJournal(path, journal); err != nil {
		return err
	}
	store.cleanupJournalStaging(*journal)
	return nil
}

func (store *StateStore) verifyRolledBackTree(operation journalOperation) error {
	target, err := store.canonicalOutputPath(operation.Target)
	if err != nil {
		return err
	}
	stage, err := store.statePath(operation.Stage)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: rolled-back retirement target is missing or unsafe", ErrStateDirty)
	}
	if _, err := os.Lstat(stage); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("%w: retirement staging remained after rollback", ErrStateDirty)
	}
	digest, err := digestDirectoryTree(target)
	if err != nil || digest != operation.PreviousDigest {
		return fmt.Errorf("%w: rolled-back retirement target digest changed", ErrStateDirty)
	}
	return nil
}

func (store *StateStore) rollbackTreeRetirement(operation journalOperation) error {
	target, err := store.canonicalOutputPath(operation.Target)
	if err != nil {
		return err
	}
	stage, err := store.statePath(operation.Stage)
	if err != nil {
		return err
	}
	targetInfo, targetErr := os.Lstat(target)
	stageInfo, stageErr := os.Lstat(stage)
	switch {
	case targetErr == nil && os.IsNotExist(stageErr):
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
			return fmt.Errorf("%w: restored retirement target is unsafe", ErrStateDirty)
		}
		// Earlier journal operations are rolled back after this one. The tree
		// may therefore still contain their published bytes at this point.
		return nil
	case os.IsNotExist(targetErr) && stageErr == nil:
		if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
			return fmt.Errorf("%w: retirement staging is unsafe", ErrStateDirty)
		}
		digest, err := digestDirectoryTree(stage)
		if err != nil || digest != operation.ResultDigest {
			return fmt.Errorf("%w: retirement staging changed", ErrStateDirty)
		}
		if err := os.Rename(stage, target); err != nil {
			return err
		}
		if err := syncTransactionDirectory(filepath.Dir(stage)); err != nil {
			return err
		}
		return syncTransactionDirectory(filepath.Dir(target))
	case targetErr == nil && stageErr == nil:
		return fmt.Errorf("%w: retirement target and staging both exist during rollback", ErrStateDirty)
	case !os.IsNotExist(targetErr):
		return targetErr
	case !os.IsNotExist(stageErr):
		return stageErr
	default:
		return fmt.Errorf("%w: retirement target and staging both disappeared", ErrStateDirty)
	}
}

// removeEmptyRolledBackRunWorkspace is deliberately narrower than general
// parent cleanup. A create-only run transaction can be interrupted after the
// writer made active/<slug>/runs/<R-id>/ but before its new head committed.
// Removing the rolled-back files without this directory would leave a phantom
// run that canonical graph loading rejects. No shared parent and no non-empty
// run workspace is ever removed.
func (store *StateStore) removeEmptyRolledBackRunWorkspace(target string) error {
	parts := strings.Split(target, "/")
	if len(parts) < 5 || parts[0] != "active" || !managedSlugRE.MatchString(parts[1]) ||
		parts[2] != "runs" || !runIDRE.MatchString(parts[3]) {
		return nil
	}
	workspace, err := store.canonicalOutputPath(strings.Join(parts[:4], "/"))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(workspace)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return nil
	}
	if err := os.Remove(workspace); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncTransactionDirectory(filepath.Dir(workspace))
}

func (store *StateStore) cleanupJournalStaging(journal transactionJournal) {
	store.removeTransactionStaging(journal.ID)
}

func (store *StateStore) removeTransactionStaging(transactionID string) {
	root, err := store.statePath("staging/" + transactionID)
	if err == nil {
		_ = os.RemoveAll(root)
	}
}

func acquireStateWriter(ctx context.Context, path string) (*writerLock, error) {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		lock, err := acquireWriterLock(path)
		if err == nil || !errors.Is(err, errWriterLocked) {
			return lock, err
		}
		retry := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			retry.Stop()
			return nil, ctx.Err()
		case <-deadline.C:
			retry.Stop()
			return nil, errWriterLocked
		case <-retry.C:
		}
	}
}

func durableAtomicWrite(path string, body []byte, mode fs.FileMode) error {
	if err := AtomicWrite(path, body, mode); err != nil {
		return err
	}
	return syncTransactionDirectory(filepath.Dir(path))
}

func durableAtomicWriteJSON(path string, value any, mode fs.FileMode) error {
	body, err := canonicalJSON(value)
	if err != nil {
		return err
	}
	return durableAtomicWrite(path, body, mode)
}
