package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

type campaignMergeRemap struct {
	source    campaignMergeSource
	work      map[string]string
	runs      map[string]string
	findings  map[string]string
	intakes   map[string]string
	reviews   map[string]string
	events    map[string]string
	sessions  map[string]string
	handles   map[string]string
	allEvents map[string]string
}

func buildPreparedCampaignMerge(
	head StateHead,
	request CampaignMergePlanRequest,
	sources []campaignMergeSource,
) (preparedCampaignMerge, error) {
	if len(sources) != len(request.Spec.Sources) {
		return preparedCampaignMerge{}, errors.New("campaign merge source projection is incomplete")
	}
	seed, err := CanonicalDigest(struct {
		Head    StateHead         `json:"head"`
		Spec    CampaignMergeSpec `json:"spec"`
		Digests []string          `json:"sourceTreeDigests"`
	}{head, request.Spec, campaignMergeTreeDigests(sources)})
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	planID := "campaign-merge-" + strings.TrimPrefix(seed, "sha256:")[:20]
	remaps := allocateCampaignMergeIDs(sources)
	populateCampaignMergeHandles(request.Spec, remaps)

	prepared := preparedCampaignMerge{
		Plan: CampaignMergePlan{
			SchemaVersion: CampaignMergeSchemaVersion,
			ID:            planID,
			ExpectedHead:  head,
			Spec:          request.Spec,
		},
		IDMap: CampaignMergeIDMap{
			SchemaVersion: CampaignMergeSchemaVersion,
			CampaignID:    request.Spec.TargetCampaignID,
		},
		Chronology: CampaignChronology{
			SchemaVersion: CampaignMergeSchemaVersion,
			CampaignID:    request.Spec.TargetCampaignID,
			Stages:        append([]CampaignChronologyEntry(nil), request.Spec.Chronology...),
		},
	}
	targetGraph := NewCampaignGraph()
	targetCampaign := mergedCampaignRecord(request, sources, remaps, planID)
	sealedCampaign, _, err := sealCampaignRecord(targetCampaign)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	targetCampaign = sealedCampaign.(CampaignRecord)
	targetGraph.Campaign = &targetCampaign
	prepared.Writes = append(prepared.Writes, StateWrite{
		Path: "active/" + request.Spec.TargetSlug + "/campaign.json", Record: targetCampaign,
	})

	previousRoot := ""
	for index := range remaps {
		remap := &remaps[index]
		source := remap.source
		prepared.Plan.Sources = append(prepared.Plan.Sources, CampaignMergeSourceSnapshot{
			Campaign: *source.graph.Campaign, TreeDigest: source.treeDigest,
			Counts: campaignMergeSourceCounts(source),
		})
		addRecordIDMapping(&prepared, source, "campaign", source.selector.CampaignID,
			"active/"+source.selector.CampaignSlug+"/campaign.json",
			source.graph.Campaign.Revision, source.graph.Campaign.Digest,
			source.graph.Campaign.CorrelationID, request.Spec.TargetCampaignID,
			"active/"+request.Spec.TargetSlug+"/campaign.json",
			historicalDateForRecord(request.Spec, source, "campaign", source.selector.CampaignID))

		root, mergeErr := appendMergedWorkItems(&prepared, &targetGraph, request.Spec, *remap, previousRoot)
		if mergeErr != nil {
			return preparedCampaignMerge{}, mergeErr
		}
		if root != "" {
			previousRoot = root
		}
		if err := appendMergedRuns(&prepared, &targetGraph, request.Spec, *remap); err != nil {
			return preparedCampaignMerge{}, err
		}
		if err := appendMergedFindings(&prepared, &targetGraph, request.Spec, *remap); err != nil {
			return preparedCampaignMerge{}, err
		}
		if err := appendMergedIntakes(&prepared, &targetGraph, request.Spec, *remap); err != nil {
			return preparedCampaignMerge{}, err
		}
		if err := appendMergedReviews(&prepared, &targetGraph, request.Spec, *remap); err != nil {
			return preparedCampaignMerge{}, err
		}
		for _, event := range source.events {
			addRecordIDMapping(&prepared, source, "event", event.ID,
				"active/"+source.selector.CampaignSlug+"/events/events.jsonl",
				event.ResultingRevision, event.Digest, event.CorrelationID,
				remap.events[event.ID],
				"active/"+request.Spec.TargetSlug+"/merge/historical-events.jsonl",
				historicalDateForEvent(request.Spec, source, event))
		}
	}
	if err := targetGraph.Validate(); err != nil {
		return preparedCampaignMerge{}, fmt.Errorf("merged campaign graph is invalid: %w", err)
	}

	artifacts, artifactMappings, err := buildCampaignMergeArtifacts(request.Spec, remaps)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	prepared.Artifacts = append(prepared.Artifacts, artifacts...)
	prepared.Plan.Artifacts = artifactMappings
	prepared.Plan.Counts = campaignMergeCounts(sources, artifactMappings)

	sort.Slice(prepared.IDMap.Mappings, func(i, j int) bool {
		a, b := prepared.IDMap.Mappings[i], prepared.IDMap.Mappings[j]
		if a.SourceCampaignSlug != b.SourceCampaignSlug {
			return a.SourceCampaignSlug < b.SourceCampaignSlug
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SourceID < b.SourceID
	})
	prepared.Plan.Mappings = append([]CampaignIDMapping(nil), prepared.IDMap.Mappings...)
	for _, mapping := range prepared.IDMap.Mappings {
		prepared.Chronology.Records = append(prepared.Chronology.Records, CampaignChronologyRecord{
			HistoricalDate: mapping.HistoricalDate,
			Kind:           mapping.Kind,
			SourceHandle:   mapping.SourceCampaignID + ":" + mapping.Kind + ":" + mapping.SourceID,
			TargetHandle:   "record:" + mapping.TargetPath,
		})
	}
	sort.SliceStable(prepared.Chronology.Records, func(i, j int) bool {
		a, b := prepared.Chronology.Records[i], prepared.Chronology.Records[j]
		if a.HistoricalDate != b.HistoricalDate {
			return a.HistoricalDate < b.HistoricalDate
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.TargetHandle < b.TargetHandle
	})
	prepared.IDMap.Digest = ""
	prepared.IDMap.Digest, err = CanonicalDigest(prepared.IDMap)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	prepared.Chronology.Digest = ""
	prepared.Chronology.Digest, err = CanonicalDigest(prepared.Chronology)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	prepared.Plan.IDMapDigest = prepared.IDMap.Digest
	prepared.Plan.ChronologyDigest = prepared.Chronology.Digest
	prepared.Plan.Digest = ""
	prepared.Plan.Digest, err = CanonicalDigest(prepared.Plan)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	metadata, err := campaignMergeMetadataArtifacts(request.Spec, prepared)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	prepared.Artifacts = append(prepared.Artifacts, metadata...)
	sort.Slice(prepared.Writes, func(i, j int) bool { return prepared.Writes[i].Path < prepared.Writes[j].Path })
	sort.Slice(prepared.Artifacts, func(i, j int) bool { return prepared.Artifacts[i].Path < prepared.Artifacts[j].Path })
	return prepared, nil
}

func allocateCampaignMergeIDs(sources []campaignMergeSource) []campaignMergeRemap {
	result := make([]campaignMergeRemap, 0, len(sources))
	work, run, finding, intake, review, event, session := 0, 0, 0, 0, 0, 0, 0
	for _, source := range sources {
		remap := campaignMergeRemap{
			source: source, work: map[string]string{}, runs: map[string]string{},
			findings: map[string]string{}, intakes: map[string]string{},
			reviews: map[string]string{}, events: map[string]string{}, sessions: map[string]string{},
			handles: map[string]string{},
		}
		for _, id := range mergeSortedWorkItemIDs(source.graph.WorkItems) {
			work++
			remap.work[id] = fmt.Sprintf("W-%04d", work)
		}
		for _, id := range mergeSortedRunIDs(source.graph.Runs) {
			run++
			remap.runs[id] = fmt.Sprintf("R-19700101-%08d", run)
		}
		for _, id := range mergeSortedFindingIDs(source.graph.Findings) {
			finding++
			remap.findings[id] = fmt.Sprintf("F-%04d", finding)
		}
		for _, id := range mergeSortedIntakeIDs(source.graph.Intakes) {
			intake++
			remap.intakes[id] = fmt.Sprintf("I-%04d", intake)
		}
		for _, id := range mergeSortedReviewIDs(source.graph.Reviews) {
			review++
			remap.reviews[id] = fmt.Sprintf("V-%04d", review)
		}
		for _, row := range source.events {
			event++
			remap.events[row.ID] = fmt.Sprintf("E-19700101-000000-M%011d", event)
		}
		for _, id := range mergeSortedReviewIDs(source.graph.Reviews) {
			sourceSession := source.graph.Reviews[id].ReviewLoad.SessionID
			if sourceSession != "" && remap.sessions[sourceSession] == "" {
				session++
				remap.sessions[sourceSession] = fmt.Sprintf("merge-session-%08d", session)
			}
		}
		result = append(result, remap)
	}
	allEvents := map[string]string{}
	for _, remap := range result {
		for sourceID, targetID := range remap.events {
			if existing, present := allEvents[sourceID]; present && existing != targetID {
				allEvents[sourceID] = ""
			} else {
				allEvents[sourceID] = targetID
			}
		}
	}
	for index := range result {
		result[index].allEvents = allEvents
	}
	return result
}

func populateCampaignMergeHandles(spec CampaignMergeSpec, remaps []campaignMergeRemap) {
	for index := range remaps {
		remap := &remaps[index]
		for sourceID, finding := range remap.source.graph.Findings {
			for _, sourceEvidence := range finding.Evidence {
				targetEvidence := sourceEvidence
				targetEvidence.Path = mapCampaignPath(targetEvidence.Path, spec, *remap)
				targetEvidence.ObjectKey = mapCampaignPath(targetEvidence.ObjectKey, spec, *remap)
				if targetEvidence.SourceRun != "" {
					targetEvidence.SourceRun = remap.runs[targetEvidence.SourceRun]
				}
				remap.handles[EvidenceHandle(sourceID, sourceEvidence)] =
					EvidenceHandle(remap.findings[sourceID], targetEvidence)
			}
		}
	}
}

func campaignMergeTreeDigests(sources []campaignMergeSource) []string {
	result := make([]string, len(sources))
	for index := range sources {
		result[index] = sources[index].treeDigest
	}
	return result
}

func campaignMergeCounts(sources []campaignMergeSource, artifacts []CampaignArtifactMapping) CampaignMergeCounts {
	counts := CampaignMergeCounts{Campaigns: len(sources), Artifacts: len(artifacts)}
	for _, source := range sources {
		counts.WorkItems += len(source.graph.WorkItems)
		counts.Runs += len(source.graph.Runs)
		counts.Findings += len(source.graph.Findings)
		counts.Intakes += len(source.graph.Intakes)
		counts.Reviews += len(source.graph.Reviews)
		counts.Events += len(source.events)
		for _, run := range source.graph.Runs {
			if run.Status == "returned" {
				counts.ReturnedRuns++
			}
			if run.Status == "aborted" {
				counts.AbortedRuns++
			}
		}
	}
	for _, artifact := range artifacts {
		counts.ArtifactBytes += artifact.Size
	}
	return counts
}

func campaignMergeSourceCounts(source campaignMergeSource) CampaignMergeCounts {
	counts := campaignMergeCounts([]campaignMergeSource{source}, nil)
	for _, file := range source.files {
		if mergeSourceFileIsRecord(file.relative) || file.relative == "STATE.md" {
			continue
		}
		counts.Artifacts++
		counts.ArtifactBytes += file.size
	}
	return counts
}

func addRecordIDMapping(
	prepared *preparedCampaignMerge,
	source campaignMergeSource,
	kind, sourceID, sourcePath string,
	revision int64,
	digest, correlation, targetID, targetPath, historicalDate string,
) {
	prepared.IDMap.Mappings = append(prepared.IDMap.Mappings, CampaignIDMapping{
		Kind: kind, SourceCampaignID: source.selector.CampaignID,
		SourceCampaignSlug: source.selector.CampaignSlug,
		SourceID:           sourceID, SourcePath: sourcePath, SourceRevision: revision,
		SourceDigest: digest, SourceCorrelationID: correlation,
		TargetID: targetID, TargetPath: targetPath, HistoricalDate: historicalDate,
	})
}

func mergeSortedWorkItemIDs(values map[string]WorkItemRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func mergeSortedRunIDs(values map[string]RunRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func mergeSortedFindingIDs(values map[string]FindingRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func mergeSortedIntakeIDs(values map[string]IntakeRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func mergeSortedReviewIDs(values map[string]ReviewRecord) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func mergedCampaignRecord(
	request CampaignMergePlanRequest,
	sources []campaignMergeSource,
	remaps []campaignMergeRemap,
	correlation string,
) CampaignRecord {
	mergeLists := func(selectValues func(CampaignRecord) []string) []string {
		values := []string{}
		for _, source := range sources {
			values = append(values, selectValues(*source.graph.Campaign)...)
		}
		return SortedUnique(values)
	}
	focus := []string{}
	for index := len(remaps) - 1; index >= 0 && len(focus) == 0; index-- {
		for _, id := range remaps[index].source.graph.Campaign.CurrentFocus {
			if mapped := remaps[index].work[id]; mapped != "" {
				focus = append(focus, mapped)
			}
		}
	}
	return CampaignRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion,
			ID:            request.Spec.TargetCampaignID,
			CreatedAt:     request.Spec.MergedAt,
			UpdatedAt:     request.Spec.MergedAt,
			Revision:      1,
			CreatedBy:     request.Actor,
			UpdatedBy:     request.Actor,
			CorrelationID: correlation,
		},
		Title:             request.Spec.Title,
		Slug:              request.Spec.TargetSlug,
		Objective:         request.Spec.Objective,
		Scope:             mergeLists(func(value CampaignRecord) []string { return value.Scope }),
		Exclusions:        mergeLists(func(value CampaignRecord) []string { return value.Exclusions }),
		SuccessCriteria:   mergeLists(func(value CampaignRecord) []string { return value.SuccessCriteria }),
		ClosureCriteria:   mergeLists(func(value CampaignRecord) []string { return value.ClosureCriteria }),
		Milestones:        mergeLists(func(value CampaignRecord) []string { return value.Milestones }),
		Status:            "paused",
		CurrentFocus:      SortedUnique(focus),
		Owner:             request.Spec.Owner,
		PermittedManagers: SortedUnique(request.Spec.PermittedManagers),
		OpenedAt:          request.Spec.MergedAt,
		PausedAt:          request.Spec.MergedAt,
	}
}

func mapRequiredID(value string, mapping map[string]string, label string) (string, error) {
	result, ok := mapping[value]
	if !ok {
		return "", fmt.Errorf(
			"campaign merge %s %s does not resolve inside its source campaign", label, value)
	}
	return result, nil
}

func mapIDSlice(values []string, mapping map[string]string, label string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		mapped, err := mapRequiredID(value, mapping, label)
		if err != nil {
			return nil, err
		}
		result[index] = mapped
	}
	return result, nil
}

func mapKnownID(value string, remap campaignMergeRemap) string {
	if value == "" {
		return value
	}
	for _, mapping := range []map[string]string{
		remap.work, remap.runs, remap.findings, remap.intakes, remap.reviews,
		remap.events, remap.allEvents,
	} {
		if result, ok := mapping[value]; ok && result != "" {
			return result
		}
	}
	return value
}

func mapPrefixedCampaignID(value string, spec CampaignMergeSpec, remap campaignMergeRemap) string {
	if mapped := remap.handles[value]; mapped != "" {
		return mapped
	}
	prefixes := []struct {
		prefix  string
		mapping map[string]string
	}{
		{"work:", remap.work}, {"run:", remap.runs}, {"finding:", remap.findings},
		{"intake:", remap.intakes}, {"review:", remap.reviews}, {"event:", remap.events},
	}
	for _, candidate := range prefixes {
		if !strings.HasPrefix(value, candidate.prefix) {
			continue
		}
		body := strings.TrimPrefix(value, candidate.prefix)
		end := len(body)
		for _, separator := range []string{":", "@", "#"} {
			if position := strings.Index(body, separator); position >= 0 && position < end {
				end = position
			}
		}
		if mapped := candidate.mapping[body[:end]]; mapped != "" {
			return candidate.prefix + mapped + body[end:]
		}
		return value
	}
	if strings.HasPrefix(value, "campaign:"+remap.source.selector.CampaignID) {
		suffix := strings.TrimPrefix(value, "campaign:"+remap.source.selector.CampaignID)
		if suffix == "" || strings.ContainsAny(suffix[:1], ":@#") {
			return "campaign:" + spec.TargetCampaignID + suffix
		}
	}
	return value
}

func mapKnownIDs(values []string, remap campaignMergeRemap) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = mapKnownID(value, remap)
	}
	return result
}

func rewriteMergeString(value string, spec CampaignMergeSpec, remap campaignMergeRemap) string {
	if value == remap.source.selector.CampaignID {
		return spec.TargetCampaignID
	}
	if value == "active/"+remap.source.selector.CampaignSlug {
		return "active/" + spec.TargetSlug
	}
	return mapCampaignPath(
		mapPrefixedCampaignID(mapKnownID(value, remap), spec, remap), spec, remap)
}

func rewriteMergeStrings(values []string, spec CampaignMergeSpec, remap campaignMergeRemap) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = rewriteMergeString(value, spec, remap)
	}
	return result
}

func rewriteMergeValue(value any, spec CampaignMergeSpec, remap campaignMergeRemap) any {
	switch typed := value.(type) {
	case string:
		return rewriteMergeString(typed, spec, remap)
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = rewriteMergeValue(typed[index], spec, remap)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = rewriteMergeValue(item, spec, remap)
		}
		return result
	default:
		return value
	}
}

func mapCampaignEventIDs(values []string, remap campaignMergeRemap) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		if mapped := remap.events[value]; mapped != "" {
			result[index] = mapped
			continue
		}
		if mapped := remap.allEvents[value]; mapped != "" {
			result[index] = mapped
			continue
		}
		return nil, fmt.Errorf("campaign merge review event %s is missing or ambiguous", value)
	}
	return result, nil
}

func mappedCampaignRoot(remap campaignMergeRemap) string {
	for _, id := range mergeSortedWorkItemIDs(remap.source.graph.WorkItems) {
		if len(remap.source.graph.WorkItems[id].Relations.ParentIDs) == 0 {
			return remap.work[id]
		}
	}
	return remap.work["W-0001"]
}

func appendMergedWorkItems(
	prepared *preparedCampaignMerge,
	targetGraph *CampaignGraph,
	spec CampaignMergeSpec,
	remap campaignMergeRemap,
	previousRoot string,
) (string, error) {
	source := remap.source
	root := mappedCampaignRoot(remap)
	for _, sourceID := range mergeSortedWorkItemIDs(source.graph.WorkItems) {
		original := source.graph.WorkItems[sourceID]
		record := original
		targetID := remap.work[sourceID]
		record.ID = targetID
		record.CampaignID = spec.TargetCampaignID
		record.Digest = ""
		var err error
		record.Relations.ParentIDs, err = mapIDSlice(record.Relations.ParentIDs, remap.work, "work item parent")
		if err != nil {
			return "", err
		}
		record.Relations.ChildIDs, err = mapIDSlice(record.Relations.ChildIDs, remap.work, "work item child")
		if err != nil {
			return "", err
		}
		record.Relations.DependsOn, err = mapIDSlice(record.Relations.DependsOn, remap.work, "work dependency")
		if err != nil {
			return "", err
		}
		record.Relations.BlockedBy, err = mapIDSlice(record.Relations.BlockedBy, remap.work, "work blocker")
		if err != nil {
			return "", err
		}
		record.Relations.SpawnedByIDs, err = mapIDSlice(record.Relations.SpawnedByIDs, remap.work, "spawned-by work item")
		if err != nil {
			return "", err
		}
		record.ActiveRunIDs, err = mapIDSlice(record.ActiveRunIDs, remap.runs, "active run")
		if err != nil {
			return "", err
		}
		record.CompletedRunIDs, err = mapIDSlice(record.CompletedRunIDs, remap.runs, "completed run")
		if err != nil {
			return "", err
		}
		record.FindingIDs, err = mapIDSlice(record.FindingIDs, remap.findings, "work finding")
		if err != nil {
			return "", err
		}
		record.DecisionIDs = rewriteMergeStrings(record.DecisionIDs, spec, remap)
		if record.SupersededBy != "" {
			record.SupersededBy, err = mapRequiredID(record.SupersededBy, remap.work, "superseding work item")
			if err != nil {
				return "", err
			}
		}
		if record.Deferment != nil {
			deferment := *record.Deferment
			if deferment.RevisitWhen.WorkItemID != "" {
				deferment.RevisitWhen.WorkItemID, err = mapRequiredID(
					deferment.RevisitWhen.WorkItemID, remap.work, "deferment work item")
				if err != nil {
					return "", err
				}
			}
			deferment.RevisitWhen.AffectedID = rewriteMergeString(
				deferment.RevisitWhen.AffectedID, spec, remap)
			deferment.Owner = mapKnownID(deferment.Owner, remap)
			deferment.ClosureDestination = mapCampaignPath(deferment.ClosureDestination, spec, remap)
			record.Deferment = &deferment
		}
		if targetID == root && previousRoot != "" &&
			!containsString(record.Relations.DependsOn, previousRoot) {
			record.Relations.DependsOn = append(record.Relations.DependsOn, previousRoot)
		}
		sealed, _, err := sealWorkItemRecord(record)
		if err != nil {
			return "", fmt.Errorf("merge work item %s: %w", sourceID, err)
		}
		record = sealed.(WorkItemRecord)
		targetGraph.WorkItems[targetID] = record
		targetPath := fmt.Sprintf("active/%s/work-items/%s.json", spec.TargetSlug, targetID)
		prepared.Writes = append(prepared.Writes, StateWrite{Path: targetPath, Record: record})
		addRecordIDMapping(prepared, source, "work-item", sourceID,
			fmt.Sprintf("active/%s/work-items/%s.json", source.selector.CampaignSlug, sourceID),
			original.Revision, original.Digest, original.CorrelationID,
			targetID, targetPath, historicalDateForRecord(spec, source, "work-item", sourceID))
	}
	return root, nil
}

func appendMergedRuns(
	prepared *preparedCampaignMerge,
	targetGraph *CampaignGraph,
	spec CampaignMergeSpec,
	remap campaignMergeRemap,
) error {
	source := remap.source
	for _, sourceID := range mergeSortedRunIDs(source.graph.Runs) {
		original := source.graph.Runs[sourceID]
		record := original
		targetID := remap.runs[sourceID]
		record.ID = targetID
		record.CampaignID = spec.TargetCampaignID
		record.Digest = ""
		var err error
		record.PrimaryWorkItemID, err = mapRequiredID(record.PrimaryWorkItemID, remap.work, "run primary work item")
		if err != nil {
			return err
		}
		record.FindingIDs, err = mapIDSlice(record.FindingIDs, remap.findings, "run finding")
		if err != nil {
			return err
		}
		record.SpawnedWorkItemIDs, err = mapIDSlice(record.SpawnedWorkItemIDs, remap.work, "run spawned work item")
		if err != nil {
			return err
		}
		if record.InvalidatedBy != "" {
			record.InvalidatedBy, err = mapRequiredID(record.InvalidatedBy, remap.runs, "invalidating run")
			if err != nil {
				return err
			}
		}
		if record.RetryOf != "" {
			record.RetryOf, err = mapRequiredID(record.RetryOf, remap.runs, "retry run")
			if err != nil {
				return err
			}
		}
		mapHandle := func(handle *FileHandle) *FileHandle {
			if handle == nil {
				return nil
			}
			copy := *handle
			copy.Path = mapCampaignPath(copy.Path, spec, remap)
			return &copy
		}
		record.Brief = mapHandle(record.Brief)
		record.ContextPack = mapHandle(record.ContextPack)
		record.Report = mapHandle(record.Report)
		for index := range record.Files {
			record.Files[index].Supports, err = mapIDSlice(
				record.Files[index].Supports, remap.findings, "run file finding")
			if err != nil {
				return err
			}
			record.Files[index].Destination = mapCampaignPath(
				record.Files[index].Destination, spec, remap)
		}
		for index := range record.ChangedProjectPaths {
			record.ChangedProjectPaths[index] = mapCampaignPath(
				record.ChangedProjectPaths[index], spec, remap)
		}
		for index := range record.WriteGrants {
			record.WriteGrants[index].Path = mapCampaignPath(record.WriteGrants[index].Path, spec, remap)
		}
		sealed, _, err := sealRunRecord(record)
		if err != nil {
			return fmt.Errorf("merge run %s: %w", sourceID, err)
		}
		record = sealed.(RunRecord)
		targetGraph.Runs[targetID] = record
		targetPath := fmt.Sprintf("active/%s/runs/%s/run.json", spec.TargetSlug, targetID)
		prepared.Writes = append(prepared.Writes, StateWrite{Path: targetPath, Record: record})
		addRecordIDMapping(prepared, source, "run", sourceID,
			fmt.Sprintf("active/%s/runs/%s/run.json", source.selector.CampaignSlug, sourceID),
			original.Revision, original.Digest, original.CorrelationID,
			targetID, targetPath, historicalDateForRecord(spec, source, "run", sourceID))
	}
	return nil
}

func sourceFindingDocuments(source campaignMergeSource) (map[string]FindingDocument, error) {
	result := map[string]FindingDocument{}
	for _, file := range source.files {
		parts := strings.Split(file.relative, "/")
		if len(parts) != 2 || parts[0] != "findings" || path.Ext(parts[1]) != ".md" {
			continue
		}
		documentPath := "active/" + source.selector.CampaignSlug + "/" + file.relative
		document, err := ParseFindingDocument(file.body, documentPath)
		if err != nil {
			return nil, err
		}
		result[document.Record.ID] = document
	}
	return result, nil
}

func appendMergedFindings(
	prepared *preparedCampaignMerge,
	targetGraph *CampaignGraph,
	spec CampaignMergeSpec,
	remap campaignMergeRemap,
) error {
	source := remap.source
	documents, err := sourceFindingDocuments(source)
	if err != nil {
		return err
	}
	for _, sourceID := range mergeSortedFindingIDs(source.graph.Findings) {
		document, present := documents[sourceID]
		if !present {
			return fmt.Errorf("campaign merge source finding %s has no canonical document", sourceID)
		}
		original := source.graph.Findings[sourceID]
		record := document.Record
		targetID := remap.findings[sourceID]
		record.ID = targetID
		record.CampaignID = spec.TargetCampaignID
		record.Digest = ""
		record.SourceRuns, err = mapIDSlice(record.SourceRuns, remap.runs, "finding source run")
		if err != nil {
			return err
		}
		record.Scope = rewriteMergeValue(record.Scope, spec, remap).(map[string]any)
		for index := range record.Evidence {
			record.Evidence[index].Path = mapCampaignPath(record.Evidence[index].Path, spec, remap)
			record.Evidence[index].ObjectKey = rewriteMergeString(
				record.Evidence[index].ObjectKey, spec, remap)
			if record.Evidence[index].SourceRun != "" {
				record.Evidence[index].SourceRun, err = mapRequiredID(
					record.Evidence[index].SourceRun, remap.runs, "finding evidence run")
				if err != nil {
					return err
				}
			}
		}
		record.Relations.Supports, err = mapIDSlice(record.Relations.Supports, remap.findings, "finding support")
		if err != nil {
			return err
		}
		record.Relations.Contradicts, err = mapIDSlice(record.Relations.Contradicts, remap.findings, "finding contradiction")
		if err != nil {
			return err
		}
		record.Relations.DependsOn, err = mapIDSlice(record.Relations.DependsOn, remap.findings, "finding dependency")
		if err != nil {
			return err
		}
		record.Relations.Supersedes, err = mapIDSlice(record.Relations.Supersedes, remap.findings, "finding supersession")
		if err != nil {
			return err
		}
		record.Relations.Duplicates, err = mapIDSlice(record.Relations.Duplicates, remap.findings, "finding duplicate")
		if err != nil {
			return err
		}
		record.Relations.Answers, err = mapIDSlice(record.Relations.Answers, remap.findings, "finding answer")
		if err != nil {
			return err
		}
		record.Relations.Spawned, err = mapIDSlice(record.Relations.Spawned, remap.work, "finding spawned work")
		if err != nil {
			return err
		}
		targetPath := fmt.Sprintf("active/%s/findings/%s.md", spec.TargetSlug, targetID)
		document.Record = record
		document.Record.Path = targetPath
		sealed, _, err := sealFindingStateRecord(document, targetPath)
		if err != nil {
			return fmt.Errorf("merge finding %s: %w", sourceID, err)
		}
		document = sealed.(FindingDocument)
		targetGraph.Findings[targetID] = document.Record
		prepared.Writes = append(prepared.Writes, StateWrite{Path: targetPath, Record: document})
		addRecordIDMapping(prepared, source, "finding", sourceID,
			fmt.Sprintf("active/%s/findings/%s.md", source.selector.CampaignSlug, sourceID),
			original.Revision, original.Digest, original.CorrelationID,
			targetID, targetPath, historicalDateForRecord(spec, source, "finding", sourceID))
		if len(original.Evidence) != len(document.Record.Evidence) {
			return fmt.Errorf("campaign merge finding %s evidence projection changed shape", sourceID)
		}
		for evidenceIndex, sourceEvidence := range original.Evidence {
			sourceHandle := EvidenceHandle(sourceID, sourceEvidence)
			targetEvidence := document.Record.Evidence[evidenceIndex]
			sourceDigest, digestErr := CanonicalDigest(struct {
				FindingID string            `json:"findingId"`
				Evidence  EvidenceReference `json:"evidence"`
			}{sourceID, sourceEvidence})
			if digestErr != nil {
				return digestErr
			}
			addRecordIDMapping(prepared, source, "evidence", sourceHandle,
				sourceEvidence.Path, 0, sourceDigest, original.CorrelationID,
				EvidenceHandle(targetID, targetEvidence), targetEvidence.Path,
				historicalDateForRecord(spec, source, "finding", sourceID))
		}
	}
	return nil
}

func appendMergedIntakes(
	prepared *preparedCampaignMerge,
	targetGraph *CampaignGraph,
	spec CampaignMergeSpec,
	remap campaignMergeRemap,
) error {
	source := remap.source
	for _, sourceID := range mergeSortedIntakeIDs(source.graph.Intakes) {
		original := source.graph.Intakes[sourceID]
		record := original
		targetID := remap.intakes[sourceID]
		record.ID = targetID
		record.CampaignID = spec.TargetCampaignID
		record.Digest = ""
		for index := range record.SourceRuns {
			record.SourceRuns[index].Path = mapCampaignPath(record.SourceRuns[index].Path, spec, remap)
		}
		var err error
		record.CandidateFindingIDs, err = mapIDSlice(record.CandidateFindingIDs, remap.findings, "intake candidate")
		if err != nil {
			return err
		}
		mapGroups := func(groups [][]string) ([][]string, error) {
			result := make([][]string, len(groups))
			for index := range groups {
				mapped, mapErr := mapIDSlice(groups[index], remap.findings, "intake finding group")
				if mapErr != nil {
					return nil, mapErr
				}
				result[index] = mapped
			}
			return result, nil
		}
		record.ProposedDuplicates, err = mapGroups(record.ProposedDuplicates)
		if err != nil {
			return err
		}
		record.ProposedMerges, err = mapGroups(record.ProposedMerges)
		if err != nil {
			return err
		}
		record.SpawnedWorkItems, err = mapIDSlice(record.SpawnedWorkItems, remap.work, "intake spawned work")
		if err != nil {
			return err
		}
		record.Conflicts = rewriteMergeStrings(record.Conflicts, spec, remap)
		record.RetentionDecisions = rewriteMergeStrings(record.RetentionDecisions, spec, remap)
		record.RequestedDecisions = rewriteMergeStrings(record.RequestedDecisions, spec, remap)
		coverageHandles := map[string]string{}
		for index := range record.Coverage {
			oldHandle := record.Coverage[index].SourceHandle
			record.Coverage[index].SourcePath = mapCampaignPath(record.Coverage[index].SourcePath, spec, remap)
			if record.Coverage[index].TargetID != "" {
				record.Coverage[index].TargetID, err = mapRequiredID(
					record.Coverage[index].TargetID, remap.findings, "coverage target")
				if err != nil {
					return err
				}
			}
			record.Coverage[index].SourceHandle = canonicalCoverageHandle(record.Coverage[index])
			coverageHandles[oldHandle] = record.Coverage[index].SourceHandle
		}
		triage := map[string]string{}
		for id, value := range record.Triage {
			mapped, mapErr := mapRequiredID(id, remap.findings, "intake triage finding")
			if mapErr != nil {
				return mapErr
			}
			triage[mapped] = value
		}
		record.Triage = triage
		for index := range record.Amendments {
			if record.Amendments[index].ReviewID != "" {
				record.Amendments[index].ReviewID, err = mapRequiredID(
					record.Amendments[index].ReviewID, remap.reviews, "coverage amendment review")
				if err != nil {
					return err
				}
			}
			for retirementIndex := range record.Amendments[index].Retirements {
				old := record.Amendments[index].Retirements[retirementIndex].SourceHandle
				mapped, ok := coverageHandles[old]
				if !ok {
					return fmt.Errorf("coverage amendment handle %s does not resolve", old)
				}
				record.Amendments[index].Retirements[retirementIndex].SourceHandle = mapped
			}
		}
		sealed, _, err := sealIntakeRecord(record)
		if err != nil {
			return fmt.Errorf("merge intake %s: %w", sourceID, err)
		}
		record = sealed.(IntakeRecord)
		targetGraph.Intakes[targetID] = record
		targetPath := fmt.Sprintf("active/%s/intake/%s.json", spec.TargetSlug, targetID)
		prepared.Writes = append(prepared.Writes, StateWrite{Path: targetPath, Record: record})
		addRecordIDMapping(prepared, source, "intake", sourceID,
			fmt.Sprintf("active/%s/intake/%s.json", source.selector.CampaignSlug, sourceID),
			original.Revision, original.Digest, original.CorrelationID,
			targetID, targetPath, historicalDateForRecord(spec, source, "intake", sourceID))
	}
	return nil
}

func appendMergedReviews(
	prepared *preparedCampaignMerge,
	targetGraph *CampaignGraph,
	spec CampaignMergeSpec,
	remap campaignMergeRemap,
) error {
	source := remap.source
	seenSessions := map[string]bool{}
	for _, sourceID := range mergeSortedReviewIDs(source.graph.Reviews) {
		original := source.graph.Reviews[sourceID]
		record := original
		targetID := remap.reviews[sourceID]
		record.ID = targetID
		record.CampaignID = spec.TargetCampaignID
		record.Digest = ""
		var err error
		record.IntakeID, err = mapRequiredID(record.IntakeID, remap.intakes, "review intake")
		if err != nil {
			return err
		}
		for index := range record.Decisions {
			record.Decisions[index].FindingID, err = mapRequiredID(
				record.Decisions[index].FindingID, remap.findings, "review finding")
			if err != nil {
				return err
			}
		}
		if record.PriorReviewID != "" {
			record.PriorReviewID, err = mapRequiredID(record.PriorReviewID, remap.reviews, "prior review")
			if err != nil {
				return err
			}
		}
		record.ResultingEventIDs, err = mapCampaignEventIDs(record.ResultingEventIDs, remap)
		if err != nil {
			return err
		}
		record.ResultingRecordIDs = rewriteMergeStrings(record.ResultingRecordIDs, spec, remap)
		record.UnresolvedConflicts = rewriteMergeStrings(record.UnresolvedConflicts, spec, remap)
		originalSession := record.ReviewLoad.SessionID
		record.ReviewLoad.ReviewID = targetID
		record.ReviewLoad.CampaignID = spec.TargetCampaignID
		record.ReviewLoad.SessionID = remap.sessions[originalSession]
		if err := SealReviewLoadReceipt(&record.ReviewLoad); err != nil {
			return fmt.Errorf("merge review load %s: %w", sourceID, err)
		}
		sealed, _, err := sealReviewRecord(record)
		if err != nil {
			return fmt.Errorf("merge review %s: %w", sourceID, err)
		}
		record = sealed.(ReviewRecord)
		targetGraph.Reviews[targetID] = record
		targetPath := fmt.Sprintf("active/%s/reviews/%s.json", spec.TargetSlug, targetID)
		prepared.Writes = append(prepared.Writes, StateWrite{Path: targetPath, Record: record})
		addRecordIDMapping(prepared, source, "review", sourceID,
			fmt.Sprintf("active/%s/reviews/%s.json", source.selector.CampaignSlug, sourceID),
			original.Revision, original.Digest, original.CorrelationID,
			targetID, targetPath, historicalDateForRecord(spec, source, "review", sourceID))
		addRecordIDMapping(prepared, source, "review-load", original.ReviewLoad.ReviewID,
			fmt.Sprintf("active/%s/reviews/%s.json", source.selector.CampaignSlug, sourceID),
			0, original.ReviewLoad.Digest, original.CorrelationID,
			record.ReviewLoad.ReviewID, targetPath,
			historicalDateForRecord(spec, source, "review", sourceID))
		if !seenSessions[originalSession] {
			seenSessions[originalSession] = true
			sessionDigest, digestErr := CanonicalDigest(originalSession)
			if digestErr != nil {
				return digestErr
			}
			addRecordIDMapping(prepared, source, "review-session", originalSession,
				fmt.Sprintf("active/%s/reviews/%s.json", source.selector.CampaignSlug, sourceID),
				0, sessionDigest, "", remap.sessions[originalSession], targetPath,
				historicalDateForRecord(spec, source, "review", sourceID))
		}
	}
	return nil
}

func mapCampaignPath(value string, spec CampaignMergeSpec, remap campaignMergeRemap) string {
	if value == "" {
		return value
	}
	for _, handlePrefix := range []string{"path:", "record:"} {
		if !strings.HasPrefix(value, handlePrefix) {
			continue
		}
		payload := strings.TrimPrefix(value, handlePrefix)
		suffix := ""
		if marker := strings.Index(payload, "#"); marker >= 0 {
			suffix, payload = payload[marker:], payload[:marker]
		}
		mapped := mapCampaignPath(payload, spec, remap)
		if mapped != payload {
			return handlePrefix + mapped + suffix
		}
		return value
	}
	prefix := "active/" + remap.source.selector.CampaignSlug + "/"
	if !strings.HasPrefix(value, prefix) {
		return value
	}
	relative := strings.TrimPrefix(value, prefix)
	parts := strings.Split(relative, "/")
	switch {
	case relative == "campaign.json":
		return "active/" + spec.TargetSlug + "/campaign.json"
	case relative == "STATE.md":
		return "active/" + spec.TargetSlug + "/STATE.md"
	case relative == "events/events.jsonl":
		return "active/" + spec.TargetSlug + "/merge/source-events/" +
			remap.source.selector.CampaignSlug + ".jsonl"
	case len(parts) >= 3 && parts[0] == "runs":
		if mapped := remap.runs[parts[1]]; mapped != "" {
			return "active/" + spec.TargetSlug + "/runs/" + mapped + "/" + strings.Join(parts[2:], "/")
		}
	case len(parts) == 2 && parts[0] == "work-items":
		id := strings.TrimSuffix(parts[1], ".json")
		if mapped := remap.work[id]; mapped != "" {
			return "active/" + spec.TargetSlug + "/work-items/" + mapped + ".json"
		}
	case len(parts) == 2 && parts[0] == "findings":
		id := strings.TrimSuffix(parts[1], ".md")
		if mapped := remap.findings[id]; mapped != "" {
			return "active/" + spec.TargetSlug + "/findings/" + mapped + ".md"
		}
	case len(parts) == 2 && parts[0] == "intake":
		id := strings.TrimSuffix(parts[1], ".json")
		if mapped := remap.intakes[id]; mapped != "" {
			return "active/" + spec.TargetSlug + "/intake/" + mapped + ".json"
		}
	case len(parts) == 2 && parts[0] == "reviews":
		id := strings.TrimSuffix(parts[1], ".json")
		if mapped := remap.reviews[id]; mapped != "" {
			return "active/" + spec.TargetSlug + "/reviews/" + mapped + ".json"
		}
	}
	return "active/" + spec.TargetSlug + "/merge/artifacts/" +
		remap.source.selector.CampaignSlug + "/" + relative
}

func mergeSourceFileIsRecord(relative string) bool {
	parts := strings.Split(relative, "/")
	switch {
	case relative == "campaign.json":
		return true
	case len(parts) == 2 && parts[0] == "work-items" && path.Ext(parts[1]) == ".json":
		return true
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "run.json":
		return true
	case len(parts) == 2 && parts[0] == "findings" && path.Ext(parts[1]) == ".md":
		return true
	case len(parts) == 2 && parts[0] == "intake" && path.Ext(parts[1]) == ".json":
		return true
	case len(parts) == 2 && parts[0] == "reviews" && path.Ext(parts[1]) == ".json":
		return true
	default:
		return false
	}
}

func buildCampaignMergeArtifacts(
	spec CampaignMergeSpec,
	remaps []campaignMergeRemap,
) ([]StateArtifactWrite, []CampaignArtifactMapping, error) {
	artifacts := []StateArtifactWrite{}
	mappings := []CampaignArtifactMapping{}
	targets := map[string]string{}
	historicalLines := [][]byte{}
	for _, remap := range remaps {
		for _, file := range remap.source.files {
			if file.relative == "STATE.md" || mergeSourceFileIsRecord(file.relative) {
				continue
			}
			if file.relative == "closure" || strings.HasPrefix(file.relative, "closure/") {
				return nil, nil, fmt.Errorf(
					"campaign merge source %s contains live closure files",
					remap.source.selector.CampaignID)
			}
			target := ""
			switch {
			case file.relative == "events/events.jsonl":
				target = "active/" + spec.TargetSlug + "/merge/source-events/" +
					remap.source.selector.CampaignSlug + ".jsonl"
			case strings.HasPrefix(file.relative, "runs/"):
				parts := strings.Split(file.relative, "/")
				if len(parts) >= 3 && remap.runs[parts[1]] != "" {
					target = "active/" + spec.TargetSlug + "/runs/" + remap.runs[parts[1]] +
						"/" + strings.Join(parts[2:], "/")
				}
			}
			if target == "" {
				target = "active/" + spec.TargetSlug + "/merge/artifacts/" +
					remap.source.selector.CampaignSlug + "/" + file.relative
			}
			if prior, exists := targets[target]; exists {
				return nil, nil, fmt.Errorf(
					"campaign merge artifact collision at %s from %s and %s",
					target, prior, file.relative)
			}
			targets[target] = remap.source.selector.CampaignSlug + "/" + file.relative
			artifacts = append(artifacts, StateArtifactWrite{
				Path: target, ContentDigest: file.digest, Mode: file.mode,
				Body: append([]byte(nil), file.body...),
			})
			mappings = append(mappings, CampaignArtifactMapping{
				SourceCampaignID:   remap.source.selector.CampaignID,
				SourceCampaignSlug: remap.source.selector.CampaignSlug,
				SourcePath:         "active/" + remap.source.selector.CampaignSlug + "/" + file.relative,
				TargetPath:         target, SHA256: file.digest, Size: file.size, Mode: file.mode,
			})
		}
		for _, sourceEvent := range remap.source.events {
			event := sourceEvent
			event.ID = remap.events[sourceEvent.ID]
			event.AffectedIDs = rewriteMergeStrings(event.AffectedIDs, spec, remap)
			event.ReviewHandle = rewriteMergeString(event.ReviewHandle, spec, remap)
			if event.PreviousEventID != "" {
				if mapped := remap.events[event.PreviousEventID]; mapped != "" {
					event.PreviousEventID = mapped
				} else if mapped := remap.allEvents[event.PreviousEventID]; mapped != "" {
					event.PreviousEventID = mapped
				}
			}
			if err := sealStateEvent(&event); err != nil {
				return nil, nil, fmt.Errorf("remap historical event %s: %w", sourceEvent.ID, err)
			}
			line, err := json.Marshal(event)
			if err != nil {
				return nil, nil, err
			}
			historicalLines = append(historicalLines, line)
		}
	}
	if len(historicalLines) != 0 {
		body := bytes.Join(historicalLines, []byte("\n"))
		body = append(body, '\n')
		artifacts = append(artifacts, StateArtifactWrite{
			Path:          "active/" + spec.TargetSlug + "/merge/historical-events.jsonl",
			ContentDigest: "sha256:" + SHA256Bytes(body), Body: body,
		})
	}
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].SourceCampaignSlug != mappings[j].SourceCampaignSlug {
			return mappings[i].SourceCampaignSlug < mappings[j].SourceCampaignSlug
		}
		return mappings[i].SourcePath < mappings[j].SourcePath
	})
	return artifacts, mappings, nil
}

func campaignMergeMetadataArtifacts(
	spec CampaignMergeSpec,
	prepared preparedCampaignMerge,
) ([]StateArtifactWrite, error) {
	values := []struct {
		name  string
		value any
	}{
		{"plan.json", prepared.Plan},
		{"id-map.json", prepared.IDMap},
		{"chronology.json", prepared.Chronology},
	}
	result := make([]StateArtifactWrite, 0, len(values)+1)
	for _, value := range values {
		body, err := canonicalJSON(value.value)
		if err != nil {
			return nil, err
		}
		result = append(result, StateArtifactWrite{
			Path:          "active/" + spec.TargetSlug + "/merge/" + value.name,
			ContentDigest: "sha256:" + SHA256Bytes(body), Body: body,
		})
	}
	markdown := renderCampaignChronologyMarkdown(prepared.Chronology, prepared.Plan.Counts)
	result = append(result, StateArtifactWrite{
		Path:          "active/" + spec.TargetSlug + "/merge/CHRONOLOGY.md",
		ContentDigest: "sha256:" + SHA256Bytes(markdown), Body: markdown,
	})
	return result, nil
}

func renderCampaignChronologyMarkdown(
	chronology CampaignChronology,
	counts CampaignMergeCounts,
) []byte {
	var builder strings.Builder
	builder.WriteString("# Campaign chronology\n\n")
	builder.WriteString(
		"Historical dates below are independent from canonical migration and merge timestamps.\n\n")
	for _, stage := range chronology.Stages {
		builder.WriteString("## " + stage.StartDate)
		if stage.EndDate != stage.StartDate {
			builder.WriteString(" through " + stage.EndDate)
		}
		builder.WriteString(": " + stage.Title + "\n\n")
		builder.WriteString(stage.Summary + "\n\n")
		builder.WriteString("Status: " + stage.Status + "\n\n")
	}
	builder.WriteString(fmt.Sprintf(
		"Merged records: %d work items, %d runs, %d findings, %d intakes, %d reviews, and %d historical events.\n",
		counts.WorkItems, counts.Runs, counts.Findings, counts.Intakes, counts.Reviews, counts.Events))
	return []byte(builder.String())
}

func historicalDateForEvent(
	spec CampaignMergeSpec,
	source campaignMergeSource,
	event StateEvent,
) string {
	return historicalDateFromBodies(
		spec, source.selector.CampaignID, []byte(event.Timestamp+" "+event.Rationale))
}

func historicalDateForRecord(
	spec CampaignMergeSpec,
	source campaignMergeSource,
	kind, id string,
) string {
	var bodies [][]byte
	for _, file := range source.files {
		match := false
		switch kind {
		case "campaign":
			match = file.relative == "campaign.json"
		case "work-item":
			match = file.relative == "work-items/"+id+".json"
		case "run":
			match = file.relative == "runs/"+id+"/run.json" ||
				strings.HasPrefix(file.relative, "runs/"+id+"/")
		case "finding":
			match = file.relative == "findings/"+id+".md"
		case "intake":
			match = file.relative == "intake/"+id+".json"
		case "review":
			match = file.relative == "reviews/"+id+".json"
		}
		if match {
			bodies = append(bodies, file.body)
		}
	}
	return historicalDateFromBodies(
		spec, source.selector.CampaignID, bytes.Join(bodies, []byte("\n")))
}

func historicalDateFromBodies(spec CampaignMergeSpec, campaignID string, body []byte) string {
	eligible := []CampaignChronologyEntry{}
	for _, stage := range spec.Chronology {
		if stage.Status == "historical" && containsString(stage.SourceCampaignIDs, campaignID) {
			eligible = append(eligible, stage)
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].StartDate < eligible[j].StartDate })
	dates := historicalDateRE.FindAllString(string(body), -1)
	sort.Strings(dates)
	for _, date := range dates {
		for _, stage := range eligible {
			if date >= stage.StartDate && date <= stage.EndDate {
				return date
			}
		}
	}
	if len(eligible) != 0 {
		return eligible[0].StartDate
	}
	for _, stage := range spec.Chronology {
		if stage.Status == "historical" {
			return stage.StartDate
		}
	}
	return strings.Split(spec.MergedAt, "T")[0]
}
