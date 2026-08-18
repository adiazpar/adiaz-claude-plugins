package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func validateCampaignTopologyEnvelope(request ManagerApplyRequest) error {
	set := newRefusalSet("manager_apply " + request.Action)
	collectManagerEnvelopeShape(set, request)
	if strings.TrimSpace(request.CampaignID) != "" && !campaignIDRE.MatchString(request.CampaignID) {
		set.add(shapeStageEnvelope, errors.New("campaign topology campaignId is malformed"))
	}
	if strings.TrimSpace(request.CampaignSlug) != "" && !managedSlugRE.MatchString(request.CampaignSlug) {
		set.add(shapeStageEnvelope, errors.New("campaign topology campaignSlug is malformed"))
	}
	if strings.TrimSpace(request.CorrelationID) != "" && !correlationIDRE.MatchString(request.CorrelationID) {
		set.add(shapeStageEnvelope, errors.New("campaign topology correlationId is malformed"))
	}
	if len(request.IdempotencyKey) > 128 {
		set.add(shapeStageEnvelope, errors.New("campaign topology idempotencyKey exceeds 128 characters"))
	}
	if request.Campaign != nil || len(request.WorkItems) != 0 || len(request.Runs) != 0 ||
		len(request.Findings) != 0 || request.Intake != nil || request.Review != nil ||
		request.ReviewPacket != nil || request.RunPreparation != nil ||
		request.ArchiveFallbackDecision != nil || len(request.ExpectedRecordDigests) != 0 ||
		request.TokenBudget != 0 {
		set.add(shapeStageComposition, errors.New(
			"campaign topology mutations reject ordinary record, review, run, and archive payloads"))
	}
	return set.result()
}

func (service *Service) managerCampaignMerge(
	ctx context.Context,
	request ManagerApplyRequest,
) (StateTransactionReceipt, error) {
	if err := validateCampaignTopologyEnvelope(request); err != nil {
		return StateTransactionReceipt{}, err
	}
	if request.Action != "campaign.merge" || request.CampaignMerge == nil ||
		request.CampaignDiscard != nil || strings.TrimSpace(request.Rationale) == "" {
		return StateTransactionReceipt{}, errors.New(
			"campaign.merge requires only campaignMerge and a non-empty transaction rationale")
	}
	submission := *request.CampaignMerge
	if request.CampaignID != submission.Spec.TargetCampaignID ||
		request.CampaignSlug != submission.Spec.TargetSlug ||
		!digestRE.MatchString(submission.ApprovedPlanDigest) {
		return StateTransactionReceipt{}, errors.New(
			"campaign.merge envelope must name the exact target and bind an approved plan digest")
	}
	planRequest := CampaignMergePlanRequest{
		Actor: request.Actor, ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest: request.ExpectedHeadDigest, Spec: submission.Spec,
	}
	if err := validateCampaignMergePlanRequest(planRequest); err != nil {
		return StateTransactionReceipt{}, err
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	if receipt, replayed, err := replayCampaignMerge(store, request, submission); err != nil {
		return StateTransactionReceipt{}, err
	} else if replayed {
		return receipt, nil
	}
	prepared, err := service.prepareCampaignMerge(ctx, planRequest)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if prepared.Plan.Digest != submission.ApprovedPlanDigest {
		return StateTransactionReceipt{}, fmt.Errorf(
			"%w: approved campaign merge plan digest is %s; the exact current dry-run is %s",
			ErrStateConflict, submission.ApprovedPlanDigest, prepared.Plan.Digest)
	}
	retired := make([]string, 0, len(prepared.Plan.Sources))
	treeDigests := map[string]string{}
	for index, snapshot := range prepared.Plan.Sources {
		tree := "active/" + submission.Spec.Sources[index].CampaignSlug
		retired = append(retired, tree)
		treeDigests[tree] = snapshot.TreeDigest
	}
	return store.Apply(ctx, StateTransactionRequest{
		CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: "manager", Action: request.Action,
		Rationale: request.Rationale, ReviewHandle: prepared.Plan.Digest,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadRevision: request.ExpectedHeadRevision,
		ExpectedHeadDigest:   request.ExpectedHeadDigest,
		CreateActiveTree:     "active/" + request.CampaignSlug,
		RetireActiveTrees:    retired, RetireTreeDigests: treeDigests,
		Writes: prepared.Writes, Artifacts: prepared.Artifacts,
	})
}

func replayCampaignMerge(
	store *StateStore,
	request ManagerApplyRequest,
	submission CampaignMergeSubmission,
) (StateTransactionReceipt, bool, error) {
	receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		return StateTransactionReceipt{}, false, err
	}
	if receipt.Event.Action != "campaign.merge" || receipt.Event.Actor != request.Actor ||
		receipt.Event.CorrelationID != request.CorrelationID ||
		receipt.Event.Rationale != request.Rationale ||
		receipt.Event.ReviewHandle != submission.ApprovedPlanDigest ||
		receipt.PreviousHead.Revision != request.ExpectedHeadRevision ||
		receipt.PreviousHead.Digest != request.ExpectedHeadDigest ||
		receipt.CreatedTree != "active/"+request.CampaignSlug {
		return StateTransactionReceipt{}, false, fmt.Errorf(
			"%w: campaign.merge retry envelope differs from the committed request",
			ErrIdempotencyConflict)
	}
	wantRetired := make([]string, len(submission.Spec.Sources))
	for index, source := range submission.Spec.Sources {
		wantRetired[index] = "active/" + source.CampaignSlug
	}
	if !reflect.DeepEqual(receipt.RetiredTrees, wantRetired) {
		return StateTransactionReceipt{}, false, fmt.Errorf(
			"%w: campaign.merge retry sources differ from the committed request",
			ErrIdempotencyConflict)
	}
	plan, err := loadCommittedCampaignMergePlan(store, request.CampaignSlug)
	if err != nil {
		return StateTransactionReceipt{}, false, err
	}
	if plan.Digest != submission.ApprovedPlanDigest ||
		plan.Spec.TargetCampaignID != request.CampaignID ||
		!reflect.DeepEqual(plan.Spec, submission.Spec) ||
		plan.ExpectedHead.Revision != request.ExpectedHeadRevision ||
		plan.ExpectedHead.Digest != request.ExpectedHeadDigest {
		return StateTransactionReceipt{}, false, fmt.Errorf(
			"%w: campaign.merge retry does not match its immutable committed plan",
			ErrIdempotencyConflict)
	}
	return receipt, true, nil
}

func loadCommittedCampaignMergePlan(store *StateStore, slug string) (CampaignMergePlan, error) {
	relative := "active/" + slug + "/merge/plan.json"
	absolute, err := store.canonicalOutputPath(relative)
	if err != nil {
		return CampaignMergePlan{}, err
	}
	body, err := readSingleLinkRegularFile(absolute)
	if err != nil {
		return CampaignMergePlan{}, fmt.Errorf("read committed campaign merge plan: %w", err)
	}
	var plan CampaignMergePlan
	if err := decodeStrictJSON(body, &plan); err != nil {
		return CampaignMergePlan{}, err
	}
	want := plan.Digest
	plan.Digest = ""
	digest, err := CanonicalDigest(plan)
	if err != nil || digest != want || !digestRE.MatchString(want) {
		return CampaignMergePlan{}, errors.New("committed campaign merge plan digest does not verify")
	}
	plan.Digest = want
	canonical, err := canonicalJSON(plan)
	if err != nil || !bytes.Equal(body, canonical) {
		return CampaignMergePlan{}, errors.New("committed campaign merge plan is not canonical")
	}
	return plan, nil
}

func (service *Service) managerCampaignDiscard(
	ctx context.Context,
	request ManagerApplyRequest,
) (StateTransactionReceipt, error) {
	if err := validateCampaignTopologyEnvelope(request); err != nil {
		return StateTransactionReceipt{}, err
	}
	if request.Action != "campaign.discard" || request.CampaignDiscard == nil ||
		request.CampaignMerge != nil {
		return StateTransactionReceipt{}, errors.New(
			"campaign.discard requires only the destructive campaignDiscard payload")
	}
	submission := *request.CampaignDiscard
	wantConfirmation := "DISCARD " + request.CampaignID + " FROM " + request.CampaignSlug
	if submission.Confirmation != wantConfirmation ||
		strings.TrimSpace(submission.Reason) == "" || request.Rationale != submission.Reason ||
		!digestRE.MatchString(submission.ExpectedCampaignDigest) ||
		(submission.ExpectedTreeDigest != "" && !digestRE.MatchString(submission.ExpectedTreeDigest)) {
		return StateTransactionReceipt{}, fmt.Errorf(
			"campaign.discard is destructive and requires confirmation %q, identical non-empty reason and rationale, and an exact campaign digest; expectedTreeDigest, when supplied, must also be exact",
			wantConfirmation)
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return StateTransactionReceipt{}, err
	}
	tree := "active/" + request.CampaignSlug
	transaction := func(treeDigest string) StateTransactionRequest {
		return StateTransactionRequest{
			CampaignSlug: request.CampaignSlug, CampaignID: request.CampaignID,
			Actor: request.Actor, Authority: "manager", Action: request.Action,
			Rationale: submission.Reason, ReviewHandle: submission.ExpectedCampaignDigest,
			CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
			ExpectedHeadRevision: request.ExpectedHeadRevision,
			ExpectedHeadDigest:   request.ExpectedHeadDigest,
			RetireActiveTrees:    []string{tree},
			RetireTreeDigests:    map[string]string{tree: treeDigest},
			EventJournal:         campaignDiscardEventJournal,
		}
	}
	if receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey); err != nil {
		return StateTransactionReceipt{}, err
	} else if found {
		committedTreeDigest := receipt.RetiredTreeDigests[tree]
		if !digestRE.MatchString(committedTreeDigest) ||
			(submission.ExpectedTreeDigest != "" && submission.ExpectedTreeDigest != committedTreeDigest) {
			return StateTransactionReceipt{}, fmt.Errorf(
				"%w: campaign.discard retry tree digest differs from the committed request",
				ErrIdempotencyConflict)
		}
		return store.Apply(ctx, transaction(committedTreeDigest))
	}
	graph, err := store.LoadCampaignGraph(request.CampaignSlug)
	if err != nil {
		if discarded, checkErr := campaignWasDiscarded(store, request.CampaignID, tree); checkErr != nil {
			return StateTransactionReceipt{}, checkErr
		} else if discarded {
			return StateTransactionReceipt{}, fmt.Errorf(
				"campaign %s was already intentionally discarded; only an exact retry with its original idempotency key can replay the receipt",
				request.CampaignID)
		}
		if closed, checkErr := campaignWasClosed(store, request.CampaignID); checkErr != nil {
			return StateTransactionReceipt{}, checkErr
		} else if closed {
			return StateTransactionReceipt{}, fmt.Errorf(
				"campaign.discard refuses closed campaign %s; its history is immutable",
				request.CampaignID)
		}
		if info, statErr := os.Lstat(filepath.Join(
			store.Boundary.Root, "active", request.CampaignSlug)); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return StateTransactionReceipt{}, errors.New(
					"campaign.discard refuses an unsafe non-directory campaign target")
			}
			return StateTransactionReceipt{}, fmt.Errorf(
				"campaign.discard refuses malformed campaign tree %s: %w",
				request.CampaignSlug, err)
		} else if !os.IsNotExist(statErr) {
			return StateTransactionReceipt{}, statErr
		}
		return StateTransactionReceipt{}, fmt.Errorf(
			"campaign.discard target %s/%s does not exist", request.CampaignID, request.CampaignSlug)
	}
	if graph.Campaign.ID != request.CampaignID || graph.Campaign.Slug != request.CampaignSlug {
		return StateTransactionReceipt{}, errors.New(
			"campaign.discard target id and slug do not resolve to the same exact campaign")
	}
	if !validOne(graph.Campaign.Status, "open", "paused") {
		return StateTransactionReceipt{}, fmt.Errorf(
			"campaign.discard accepts only open or paused campaigns; %s is %s",
			request.CampaignID, graph.Campaign.Status)
	}
	if !containsString(graph.Campaign.PermittedManagers, request.Actor) {
		return StateTransactionReceipt{}, fmt.Errorf(
			"actor %q is not a permitted manager of discard target %s",
			request.Actor, request.CampaignID)
	}
	if graph.Campaign.Digest != submission.ExpectedCampaignDigest {
		return StateTransactionReceipt{}, fmt.Errorf(
			"%w: campaign.discard expected campaign digest %s and found %s",
			ErrStateConflict, submission.ExpectedCampaignDigest, graph.Campaign.Digest)
	}
	root := filepath.Join(store.Boundary.Root, "active", request.CampaignSlug)
	treeDigest, err := digestDirectoryTree(root)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	if treeDigest != submission.ExpectedTreeDigest {
		if submission.ExpectedTreeDigest != "" {
			return StateTransactionReceipt{}, fmt.Errorf(
				"%w: campaign.discard expected tree digest %s and found %s",
				ErrStateConflict, submission.ExpectedTreeDigest, treeDigest)
		}
	}
	return store.Apply(ctx, transaction(treeDigest))
}

func campaignWasDiscarded(store *StateStore, campaignID, tree string) (bool, error) {
	absolute, err := store.canonicalOutputPath(campaignDiscardEventJournal)
	if err != nil {
		return false, err
	}
	body, err := readSingleLinkRegularFile(absolute)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for index, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event StateEvent
		if err := decodeStrictJSON([]byte(line), &event); err != nil || verifyStateEvent(event) != nil {
			return false, fmt.Errorf("campaign discard event line %d is invalid", index+1)
		}
		if event.Action == "campaign.discard" &&
			(containsString(event.AffectedIDs, campaignID) || containsString(event.AffectedIDs, tree)) {
			return true, nil
		}
	}
	return false, nil
}

func campaignWasClosed(store *StateStore, campaignID string) (bool, error) {
	root := filepath.Join(store.Boundary.Root, "docs", "history", "campaigns")
	found := false
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "manifest.json" {
			return nil
		}
		relative, err := filepath.Rel(store.Boundary.Root, current)
		if err != nil {
			return err
		}
		_, value, _, err := store.readCanonicalRecordValue(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		manifest, ok := value.(ArchiveManifest)
		if ok && manifest.CampaignID == campaignID {
			found = true
		}
		return nil
	})
	if os.IsNotExist(err) {
		return false, nil
	}
	return found, err
}
