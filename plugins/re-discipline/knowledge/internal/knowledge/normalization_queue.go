package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type NormalizationQueueRequest struct {
	Action         string                   `json:"action"`
	Actor          string                   `json:"actor,omitempty"`
	ItemID         string                   `json:"itemId,omitempty"`
	ExpectedDigest string                   `json:"expectedDigest,omitempty"`
	ReportPath     string                   `json:"reportPath,omitempty"`
	ReportDigest   string                   `json:"reportDigest,omitempty"`
	Timestamp      string                   `json:"timestamp,omitempty"`
	Resolution     *NormalizationResolution `json:"resolution,omitempty"`
	Limit          int                      `json:"limit,omitempty"`
}

type NormalizationQueueResult struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Action        string                   `json:"action"`
	Item          *NormalizationSuggestion `json:"item,omitempty"`
	Queue         NormalizationQueueStatus `json:"queue"`
	Digest        string                   `json:"digest"`
}

func (service *Service) NormalizationQueueApply(
	ctx context.Context,
	request NormalizationQueueRequest,
) (NormalizationQueueResult, error) {
	if service == nil || service.archiveTracker == nil {
		return NormalizationQueueResult{}, errors.New("normalization queue service is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return NormalizationQueueResult{}, err
	}
	if !validOne(request.Action, "status", "request", "claim", "ack", "resolve") {
		return NormalizationQueueResult{}, fmt.Errorf("unsupported normalization queue action %q", request.Action)
	}
	if request.Action == "status" {
		if request.Limit == 0 {
			request.Limit = 20
		}
		if request.Limit < 1 || request.Limit > 100 {
			return NormalizationQueueResult{}, errors.New("normalization queue limit must be between 1 and 100")
		}
		if err := service.archiveTracker.Refresh(); err != nil {
			return NormalizationQueueResult{}, err
		}
		return sealNormalizationQueueResult(NormalizationQueueResult{
			SchemaVersion: CampaignSchemaVersion, Action: request.Action,
			Queue: service.archiveTracker.QueueStatus(request.Limit),
		})
	}
	when, err := time.Parse(time.RFC3339Nano, request.Timestamp)
	if err != nil || when.Location() != time.UTC {
		return NormalizationQueueResult{}, errors.New("normalization queue mutation requires a UTC RFC3339 timestamp")
	}
	if request.Action == "request" {
		return service.requestNormalization(ctx, request)
	}
	if strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.ItemID) == "" ||
		!digestRE.MatchString(request.ExpectedDigest) {
		return NormalizationQueueResult{}, errors.New("normalization queue transition requires actor, itemId, and expectedDigest")
	}
	if err := service.archiveTracker.Refresh(); err != nil {
		return NormalizationQueueResult{}, err
	}
	item, present := service.archiveTracker.Get(request.ItemID)
	if !present {
		return NormalizationQueueResult{}, errors.New("normalization queue item does not exist")
	}
	campaign, err := service.loadNormalizationSourceCampaign(item)
	if err != nil {
		return NormalizationQueueResult{}, fmt.Errorf("load normalization source campaign: %w", err)
	}
	if !containsString(campaign.PermittedManagers, request.Actor) {
		return NormalizationQueueResult{}, errors.New("normalization queue mutation requires a permitted campaign manager")
	}
	var resolution *NormalizationResolution
	if request.Action == "resolve" {
		if request.Resolution == nil {
			return NormalizationQueueResult{}, errors.New(
				"normalization resolve requires a canonical resolution receipt; rationale alone is insufficient")
		}
		verified, verifyErr := service.verifyNormalizationResolution(item, *request.Resolution)
		if verifyErr != nil {
			return NormalizationQueueResult{}, verifyErr
		}
		resolution = &verified
	} else if request.Resolution != nil {
		return NormalizationQueueResult{}, errors.New("only normalization resolve may carry a resolution receipt")
	}
	item, err = service.archiveTracker.Transition(
		request.Action, request.ItemID, request.Actor, request.Timestamp,
		request.ExpectedDigest, resolution,
	)
	if err != nil {
		return NormalizationQueueResult{}, err
	}
	return sealNormalizationQueueResult(NormalizationQueueResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action, Item: &item,
		Queue: service.archiveTracker.QueueStatus(20),
	})
}

func (service *Service) requestNormalization(
	ctx context.Context,
	request NormalizationQueueRequest,
) (NormalizationQueueResult, error) {
	if err := ctx.Err(); err != nil {
		return NormalizationQueueResult{}, err
	}
	if strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.ReportPath) == "" ||
		!digestRE.MatchString(request.ReportDigest) {
		return NormalizationQueueResult{}, errors.New(
			"normalization request requires actor, reportPath, and exact reportDigest")
	}
	if request.ItemID != "" || request.ExpectedDigest != "" || request.Resolution != nil {
		return NormalizationQueueResult{}, errors.New(
			"normalization request cannot carry transition identity or a resolution receipt")
	}
	reportPath := strings.TrimSpace(request.ReportPath)
	if reportPath != request.ReportPath || validateRelativeRecordPath(reportPath) != nil {
		return NormalizationQueueResult{}, errors.New(
			"normalization request reportPath must be an exact canonical project-relative path")
	}
	source, err := normalizationSourceForReport(
		service.Boundary, reportPath, request.ReportDigest)
	if err != nil {
		return NormalizationQueueResult{}, err
	}
	if source.CampaignID == "C-UNRESOLVED" {
		return NormalizationQueueResult{}, errors.New(
			"normalization request must name a canonical active or archived run report")
	}
	if err := verifyCanonicalFileHandle(service.Boundary, FileHandle{
		Path: source.ReportPath, SHA256: request.ReportDigest,
	}); err != nil {
		return NormalizationQueueResult{}, fmt.Errorf("normalization request source report: %w", err)
	}
	itemIdentity := NormalizationSuggestion{
		CampaignID: source.CampaignID, CampaignSlug: source.CampaignSlug,
		RunID: source.RunID, ReportPath: source.ReportPath,
		ReportHandle: source.ReportHandle, SourceHandle: source.SourceHandle,
		ReportDigest: request.ReportDigest,
	}
	campaign, err := service.loadNormalizationSourceCampaign(itemIdentity)
	if err != nil {
		return NormalizationQueueResult{}, fmt.Errorf("load normalization source campaign: %w", err)
	}
	if !containsString(campaign.PermittedManagers, request.Actor) {
		return NormalizationQueueResult{}, errors.New(
			"normalization request requires a permitted source-campaign manager")
	}
	item, err := service.archiveTracker.QueueSource(
		request.ReportDigest, source, normalizationTriggerManager, request.Timestamp)
	if err != nil {
		return NormalizationQueueResult{}, err
	}
	return sealNormalizationQueueResult(NormalizationQueueResult{
		SchemaVersion: CampaignSchemaVersion, Action: request.Action, Item: &item,
		Queue: service.archiveTracker.QueueStatus(20),
	})
}

// queueClosureNormalization materializes Amendment 2's closure trigger for
// every substantive source report that does not yet have reviewed intake
// coverage. Curator reports are the normalization receipts themselves and are
// deliberately excluded from recursive curation.
func (service *Service) queueClosureNormalization(
	graph CampaignGraph,
	timestamp string,
) ([]NormalizationSuggestion, error) {
	if service == nil {
		return nil, errors.New("normalization queue service is unavailable")
	}
	if validateUTC(timestamp) != nil {
		return nil, errors.New("closure normalization trigger requires a UTC RFC3339 timestamp")
	}
	if service.archiveTracker == nil {
		threshold := DefaultKnowledgeSettings().Archive.NormalizationTriggerHits
		path := filepath.Join(
			service.Boundary.Root, ".re-discipline", "knowledge", "normalization-queue.json")
		tracker, err := OpenArchiveFallbackTracker(threshold, path)
		if err != nil {
			return nil, err
		}
		service.archiveTracker = tracker
	}
	reviewed := reviewedReportCoverage(graph)
	runIDs := make([]string, 0, len(graph.Runs))
	for runID := range graph.Runs {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)
	queued := []NormalizationSuggestion{}
	for _, runID := range runIDs {
		run := graph.Runs[runID]
		if run.Report == nil || run.Status == "aborted" || !runIsClaimSource(run) ||
			reviewed[reviewedRunCoverageKey(run.ID, *run.Report)] {
			continue
		}
		if err := verifyCanonicalFileHandle(service.Boundary, *run.Report); err != nil {
			return nil, fmt.Errorf("closure normalization source %s: %w", run.ID, err)
		}
		source := NormalizationSource{
			CampaignID: graph.Campaign.ID, CampaignSlug: graph.Campaign.Slug,
			RunID: run.ID, ReportPath: run.Report.Path, ReportHandle: "run:" + run.ID,
			SourceHandle: "path:" + run.Report.Path,
		}
		item, err := service.archiveTracker.QueueSource(
			run.Report.SHA256, source, normalizationTriggerClosure, timestamp)
		if err != nil {
			return nil, err
		}
		queued = append(queued, item)
	}
	return queued, nil
}

// closureNormalizationBlockers keeps the closure-stage gate and the durable
// demand queue in agreement. Reviewed intake coverage is the epistemic proof;
// the resolved queue receipt is the operational proof that the exact
// closure-triggered source, curator run, coverage, and manager review were
// joined. A campaign may do the normalization work while it is closing, but
// it cannot advance beyond the normalize stage with a dangling queue item that
// would become unresolvable after the active tree is retired.
func (service *Service) closureNormalizationBlockers(
	graph CampaignGraph,
) ([]string, error) {
	if service == nil || service.archiveTracker == nil {
		return nil, errors.New("normalization queue service is unavailable")
	}
	if err := service.archiveTracker.Refresh(); err != nil {
		return nil, err
	}
	reviewed := reviewedReportCoverage(graph)
	blockers := []string{}
	runIDs := make([]string, 0, len(graph.Runs))
	for runID := range graph.Runs {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)
	for _, runID := range runIDs {
		run := graph.Runs[runID]
		if run.Report == nil || run.Status == "aborted" || run.Role == "curator" {
			continue
		}
		source := NormalizationSource{
			CampaignID: graph.Campaign.ID, CampaignSlug: graph.Campaign.Slug,
			RunID: run.ID, ReportPath: run.Report.Path, ReportHandle: "run:" + run.ID,
			SourceHandle: "path:" + run.Report.Path,
		}
		key := normalizationSourceKey(run.Report.SHA256, source)
		itemID := "normalization-" + strings.TrimPrefix(key, "sha256:")[:20]
		item, present := service.archiveTracker.Get(itemID)
		covered := reviewed[reviewedRunCoverageKey(run.ID, *run.Report)]
		if !present {
			if !covered {
				blockers = append(blockers, "normalization:"+run.ID+":missing-queue")
			}
			continue
		}
		if !containsString(item.Triggers, normalizationTriggerClosure) {
			if !covered {
				blockers = append(blockers, "normalization:"+run.ID+":missing-closure-trigger")
			}
			continue
		}
		if item.Status != "resolved" || !covered {
			blockers = append(blockers, "normalization:"+run.ID+":"+item.Status)
		}
	}
	return SortedUnique(blockers), nil
}

func (service *Service) loadNormalizationSourceCampaign(
	item NormalizationSuggestion,
) (CampaignRecord, error) {
	store := NewStateStoreWithBoundary(service.Boundary)
	graph, err := store.LoadCampaignGraph(item.CampaignID)
	if err == nil && graph.Campaign != nil && graph.Campaign.ID == item.CampaignID &&
		graph.Campaign.Slug == item.CampaignSlug {
		return *graph.Campaign, nil
	}
	parts := strings.Split(item.ReportPath, "/")
	if len(parts) != 7 || parts[0] != "docs" || parts[1] != "history" ||
		parts[2] != "campaigns" || parts[4] != "runs" || parts[6] != "report.md" {
		if err != nil {
			return CampaignRecord{}, err
		}
		return CampaignRecord{}, errors.New("normalization source campaign is unavailable")
	}
	archiveRoot := strings.Join(parts[:4], "/")
	manifestBody, manifestErr := loadRetiredClosureArtifact(store, archiveRoot+"/manifest.json")
	campaignBody, campaignErr := loadRetiredClosureArtifact(store, archiveRoot+"/finalization/campaign.json")
	if manifestErr != nil || campaignErr != nil {
		return CampaignRecord{}, errors.New("normalization source archive is incomplete")
	}
	var manifest ArchiveManifest
	var campaign CampaignRecord
	if decodeStrictJSON(manifestBody, &manifest) != nil || ValidateArchiveManifest(manifest) != nil ||
		decodeStrictJSON(campaignBody, &campaign) != nil || ValidateCampaign(campaign) != nil ||
		campaign.Status != "closed" || campaign.ID != item.CampaignID || campaign.Slug != item.CampaignSlug ||
		manifest.CampaignID != item.CampaignID ||
		manifest.Files[strings.Join(parts[4:], "/")] != item.ReportDigest {
		return CampaignRecord{}, errors.New("normalization source archive binding does not verify")
	}
	return campaign, nil
}

func normalizationCoverageDigest(
	intake IntakeRecord,
	source FileHandle,
) (string, []CoverageEntry, error) {
	coverage := []CoverageEntry{}
	for _, entry := range intake.Coverage {
		if entry.SourcePath == source.Path && entry.SourceSHA256 == source.SHA256 {
			coverage = append(coverage, entry)
		}
	}
	sort.Slice(coverage, func(i, j int) bool {
		if coverage[i].StartLine != coverage[j].StartLine {
			return coverage[i].StartLine < coverage[j].StartLine
		}
		if coverage[i].EndLine != coverage[j].EndLine {
			return coverage[i].EndLine < coverage[j].EndLine
		}
		return coverage[i].SourceHandle < coverage[j].SourceHandle
	})
	if len(coverage) == 0 {
		return "", nil, errors.New("normalization source has no canonical intake coverage")
	}
	digest, err := CanonicalDigest(struct {
		SchemaVersion  int             `json:"schemaVersion"`
		CampaignID     string          `json:"campaignId"`
		IntakeID       string          `json:"intakeId"`
		IntakeRevision int64           `json:"intakeRevision"`
		Source         FileHandle      `json:"source"`
		Coverage       []CoverageEntry `json:"coverage"`
	}{
		SchemaVersion: CampaignSchemaVersion, CampaignID: intake.CampaignID,
		IntakeID: intake.ID, IntakeRevision: intake.Revision,
		Source: source, Coverage: coverage,
	})
	return digest, coverage, err
}

func normalizationReviewComplete(review ReviewRecord, intake IntakeRecord) bool {
	if review.CampaignID != intake.CampaignID || review.IntakeID != intake.ID ||
		review.IntakeRevision != intakeReviewedRevision(intake) ||
		len(review.Decisions) != len(intake.CandidateFindingIDs) ||
		len(review.UnresolvedConflicts) != 0 {
		return false
	}
	decisions := make(map[string]bool, len(review.Decisions))
	for _, decision := range review.Decisions {
		if decisions[decision.FindingID] {
			return false
		}
		decisions[decision.FindingID] = true
	}
	for _, findingID := range intake.CandidateFindingIDs {
		if !decisions[findingID] {
			return false
		}
	}
	return len(decisions) == len(intake.CandidateFindingIDs)
}

func (service *Service) verifyNormalizationResolution(
	item NormalizationSuggestion,
	submitted NormalizationResolution,
) (NormalizationResolution, error) {
	store := NewStateStoreWithBoundary(service.Boundary)
	graph, err := store.LoadCampaignGraph(item.CampaignID)
	if err != nil {
		return NormalizationResolution{}, errors.New(
			"normalization resolution requires the source campaign's canonical active graph")
	}
	if graph.Campaign == nil || graph.Campaign.ID != item.CampaignID ||
		graph.Campaign.Slug != item.CampaignSlug {
		return NormalizationResolution{}, errors.New("normalization source campaign identity changed")
	}
	sourceRun, ok := graph.Runs[item.RunID]
	if !ok || sourceRun.Report == nil || sourceRun.Report.Path != item.ReportPath ||
		sourceRun.Report.SHA256 != item.ReportDigest {
		return NormalizationResolution{}, errors.New(
			"normalization source no longer binds its canonical run report")
	}
	if sourceRun.Role == "curator" {
		return NormalizationResolution{}, errors.New(
			"curator run reports are normalization receipts, not recursively normalizable sources")
	}
	if err := verifyCanonicalFileHandle(service.Boundary, *sourceRun.Report); err != nil {
		return NormalizationResolution{}, fmt.Errorf("normalization source report: %w", err)
	}

	curatorRun, ok := graph.Runs[submitted.CuratorRunID]
	if !ok || curatorRun.Role != "curator" || curatorRun.Status != "returned" ||
		curatorRun.Report == nil || curatorRun.Digest != submitted.CuratorRunDigest ||
		!reflect.DeepEqual(*curatorRun.Report, submitted.CuratorReport) {
		return NormalizationResolution{}, errors.New(
			"normalization resolution does not bind an exact canonical returned curator run")
	}
	curationWorkID := continuousCurationWorkID(item.RunID)
	curationWork, ok := graph.WorkItems[curationWorkID]
	if !ok || curatorRun.PrimaryWorkItemID != curationWorkID ||
		(!containsString(curationWork.ActiveRunIDs, curatorRun.ID) &&
			!containsString(curationWork.CompletedRunIDs, curatorRun.ID)) {
		return NormalizationResolution{}, errors.New(
			"normalization curator run is not bound to the queued source's canonical curation work")
	}
	if err := verifyCanonicalFileHandle(service.Boundary, *curatorRun.Report); err != nil {
		return NormalizationResolution{}, fmt.Errorf("normalization curator report: %w", err)
	}

	// This binds the submitted receipt to the intake's exact revision and digest,
	// both of which a later coverage retirement moves. That is compare-and-swap,
	// not a standing invariant, because this function runs only at submission:
	// NormalizationQueueApply is its sole caller and calls it only for the
	// `resolve` action, on the caller-supplied request.Resolution, immediately
	// before ArchiveFallbackTracker.Transition seals a copy into the queue item.
	// Nothing re-runs it on a persisted resolution afterwards -
	// validateNormalizationSuggestion re-checks a stored resolution's own
	// self-digest and its source-report path and digest only, and
	// closureNormalizationBlockers reads the item's status and re-derives
	// coverage from the live graph rather than from the stored receipt. So a
	// retirement after the fact cannot invalidate a resolution that was valid
	// when it was recorded; it can only make the next resolution submission
	// quote the newer revision and digest.
	intake, ok := graph.Intakes[submitted.IntakeID]
	if !ok || intake.Status != "reviewed" || intake.Revision != submitted.IntakeRevision ||
		intake.Digest != submitted.IntakeDigest || !reviewBindsIntake(graph, intake) {
		return NormalizationResolution{}, errors.New(
			"normalization resolution does not bind an exact canonical reviewed intake")
	}
	if len(intake.Conflicts) != 0 || len(intake.Uncertainties) != 0 ||
		len(intake.RequestedDecisions) != 0 {
		return NormalizationResolution{}, errors.New(
			"normalization intake retains unresolved conflicts, uncertainties, or decisions")
	}
	if err := verifyCanonicalIntakeCoverage(service.Boundary, intake); err != nil {
		return NormalizationResolution{}, err
	}
	source := FileHandle{Path: item.ReportPath, SHA256: item.ReportDigest}
	coverageDigest, coverage, err := normalizationCoverageDigest(intake, source)
	if err != nil {
		return NormalizationResolution{}, err
	}

	resolvedFindings := []string{}
	allNonClaim := true
	for _, entry := range coverage {
		switch entry.Disposition {
		case "candidate-finding", "duplicate":
			allNonClaim = false
			resolvedFindings = append(resolvedFindings, entry.TargetID)
			if _, exists := graph.Findings[entry.TargetID]; !exists {
				return NormalizationResolution{}, fmt.Errorf(
					"normalization coverage target %s is not canonical", entry.TargetID)
			}
		case "non-claim":
		default:
			return NormalizationResolution{}, fmt.Errorf(
				"normalization coverage retains non-final disposition %s", entry.Disposition)
		}
	}
	resolvedFindings = SortedUnique(resolvedFindings)
	disposition := "normalized"
	if allNonClaim {
		disposition = "reviewed-non-claim"
	}

	review, ok := graph.Reviews[submitted.ReviewID]
	if !ok || review.Revision != submitted.ReviewRevision || review.Digest != submitted.ReviewDigest ||
		!normalizationReviewComplete(review, intake) {
		return NormalizationResolution{}, errors.New(
			"normalization resolution does not bind the exact complete manager review receipt")
	}
	expected := NormalizationResolution{
		SchemaVersion: CampaignSchemaVersion, Disposition: disposition,
		SourceReport: source, CuratorRunID: curatorRun.ID,
		CuratorRunDigest: curatorRun.Digest, CuratorReport: *curatorRun.Report,
		IntakeID: intake.ID, IntakeRevision: intake.Revision, IntakeDigest: intake.Digest,
		CoverageDigest: coverageDigest, ReviewID: review.ID,
		ReviewRevision: review.Revision, ReviewDigest: review.Digest,
		ResolvedFindingIDs: resolvedFindings,
	}
	if err := sealNormalizationResolution(&expected); err != nil {
		return NormalizationResolution{}, err
	}
	if !reflect.DeepEqual(expected, submitted) {
		return NormalizationResolution{}, errors.New(
			"normalization resolution receipt does not match canonical source, coverage, curator, and review proofs")
	}
	return expected, nil
}

func (tracker *ArchiveFallbackTracker) Get(id string) (NormalizationSuggestion, bool) {
	if tracker == nil {
		return NormalizationSuggestion{}, false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	item, present := tracker.suggestions[id]
	return item, present
}

func (tracker *ArchiveFallbackTracker) Transition(
	action, itemID, actor, timestamp, expectedDigest string,
	resolution *NormalizationResolution,
) (NormalizationSuggestion, error) {
	if tracker == nil {
		return NormalizationSuggestion{}, errors.New("archive fallback tracker is nil")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	lock, err := tracker.acquirePersistentLockLocked()
	if err != nil {
		return NormalizationSuggestion{}, err
	}
	if lock != nil {
		defer lock.Close()
		if err := tracker.reloadLocked(); err != nil {
			return NormalizationSuggestion{}, err
		}
	}
	current, present := tracker.suggestions[itemID]
	if !present {
		return NormalizationSuggestion{}, errors.New("normalization queue item does not exist")
	}
	if current.Digest != expectedDigest {
		return NormalizationSuggestion{}, ErrStateConflict
	}
	if strings.TrimSpace(actor) == "" || validateUTC(timestamp) != nil {
		return NormalizationSuggestion{}, errors.New("normalization transition requires actor and UTC timestamp")
	}
	next := current
	next.Revision++
	switch action {
	case "claim":
		if current.Status != "queued" {
			return NormalizationSuggestion{}, fmt.Errorf("normalization item in %s state cannot be claimed", current.Status)
		}
		next.Status, next.ClaimedBy, next.ClaimedAt = "claimed", actor, timestamp
	case "ack":
		if current.Status != "claimed" {
			return NormalizationSuggestion{}, fmt.Errorf("normalization item in %s state cannot be acknowledged", current.Status)
		}
		next.Status, next.AcknowledgedBy, next.AcknowledgedAt = "acknowledged", actor, timestamp
	case "resolve":
		if current.Status != "acknowledged" || resolution == nil ||
			validateNormalizationResolution(*resolution) != nil ||
			resolution.SourceReport.Path != current.ReportPath ||
			resolution.SourceReport.SHA256 != current.ReportDigest {
			return NormalizationSuggestion{}, errors.New(
				"only an acknowledged normalization item may be resolved with a sealed source-bound receipt")
		}
		next.Status, next.ResolvedBy, next.ResolvedAt = "resolved", actor, timestamp
		copy := *resolution
		next.Resolution = &copy
	default:
		return NormalizationSuggestion{}, fmt.Errorf("unsupported normalization transition %q", action)
	}
	if err := sealNormalizationSuggestion(&next); err != nil {
		return NormalizationSuggestion{}, err
	}
	tracker.suggestions[itemID] = next
	if err := tracker.persistLocked(); err != nil {
		tracker.suggestions[itemID] = current
		return NormalizationSuggestion{}, err
	}
	return next, nil
}

func sealNormalizationQueueResult(result NormalizationQueueResult) (NormalizationQueueResult, error) {
	result.Digest = ""
	digest, err := CanonicalDigest(result)
	if err != nil {
		return NormalizationQueueResult{}, err
	}
	result.Digest = digest
	return result, nil
}
