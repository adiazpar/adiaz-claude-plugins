package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const CampaignMergeSchemaVersion = 1

const maxCampaignMergeArtifactBytes int64 = 64 * 1024 * 1024

const campaignDiscardEventJournal = ".re-discipline/state/campaign-discards/events.jsonl"

var historicalDateRE = regexp.MustCompile(`\b(?:20[0-9]{2})-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12][0-9]|3[01])\b`)

type CampaignMergeSourceSelector struct {
	CampaignID   string `json:"campaignId"`
	CampaignSlug string `json:"campaignSlug"`
}

type CampaignChronologyEntry struct {
	ID                string   `json:"id"`
	StartDate         string   `json:"startDate"`
	EndDate           string   `json:"endDate"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	Status            string   `json:"status"`
	SourceCampaignIDs []string `json:"sourceCampaignIds,omitempty"`
	DependsOn         []string `json:"dependsOn,omitempty"`
}

type CampaignMergeSpec struct {
	TargetCampaignID  string                        `json:"targetCampaignId"`
	TargetSlug        string                        `json:"targetSlug"`
	Title             string                        `json:"title"`
	Objective         string                        `json:"objective"`
	Owner             string                        `json:"owner"`
	PermittedManagers []string                      `json:"permittedManagers"`
	MergedAt          string                        `json:"mergedAt"`
	Sources           []CampaignMergeSourceSelector `json:"sources"`
	Chronology        []CampaignChronologyEntry     `json:"chronology"`
}

type CampaignMergePlanRequest struct {
	Actor                string            `json:"actor"`
	ExpectedHeadRevision int64             `json:"expectedHeadRevision"`
	ExpectedHeadDigest   string            `json:"expectedHeadDigest"`
	Spec                 CampaignMergeSpec `json:"spec"`
}

type CampaignMergeCounts struct {
	Campaigns     int   `json:"campaigns"`
	WorkItems     int   `json:"workItems"`
	Runs          int   `json:"runs"`
	ReturnedRuns  int   `json:"returnedRuns"`
	AbortedRuns   int   `json:"abortedRuns"`
	Findings      int   `json:"findings"`
	Intakes       int   `json:"intakes"`
	Reviews       int   `json:"reviews"`
	Events        int   `json:"events"`
	Artifacts     int   `json:"artifacts"`
	ArtifactBytes int64 `json:"artifactBytes"`
}

type CampaignMergeSourceSnapshot struct {
	Campaign   CampaignRecord      `json:"campaign"`
	TreeDigest string              `json:"treeDigest"`
	Counts     CampaignMergeCounts `json:"counts"`
}

type CampaignIDMapping struct {
	Kind                string `json:"kind"`
	SourceCampaignID    string `json:"sourceCampaignId"`
	SourceCampaignSlug  string `json:"sourceCampaignSlug"`
	SourceID            string `json:"sourceId"`
	SourcePath          string `json:"sourcePath"`
	SourceRevision      int64  `json:"sourceRevision,omitempty"`
	SourceDigest        string `json:"sourceDigest"`
	SourceCorrelationID string `json:"sourceCorrelationId,omitempty"`
	TargetID            string `json:"targetId"`
	TargetPath          string `json:"targetPath"`
	HistoricalDate      string `json:"historicalDate"`
}

type CampaignArtifactMapping struct {
	SourceCampaignID   string `json:"sourceCampaignId"`
	SourceCampaignSlug string `json:"sourceCampaignSlug"`
	SourcePath         string `json:"sourcePath"`
	TargetPath         string `json:"targetPath"`
	SHA256             string `json:"sha256"`
	Size               int64  `json:"size"`
	Mode               uint32 `json:"mode"`
}

type CampaignChronologyRecord struct {
	HistoricalDate string `json:"historicalDate"`
	Kind           string `json:"kind"`
	SourceHandle   string `json:"sourceHandle"`
	TargetHandle   string `json:"targetHandle"`
}

type CampaignChronology struct {
	SchemaVersion int                        `json:"schemaVersion"`
	CampaignID    string                     `json:"campaignId"`
	Stages        []CampaignChronologyEntry  `json:"stages"`
	Records       []CampaignChronologyRecord `json:"records"`
	Digest        string                     `json:"digest"`
}

type CampaignMergeIDMap struct {
	SchemaVersion int                 `json:"schemaVersion"`
	CampaignID    string              `json:"campaignId"`
	Mappings      []CampaignIDMapping `json:"mappings"`
	Digest        string              `json:"digest"`
}

type CampaignMergePlan struct {
	SchemaVersion    int                           `json:"schemaVersion"`
	ID               string                        `json:"id"`
	ExpectedHead     StateHead                     `json:"expectedHead"`
	Spec             CampaignMergeSpec             `json:"spec"`
	Sources          []CampaignMergeSourceSnapshot `json:"sources"`
	Mappings         []CampaignIDMapping           `json:"mappings"`
	Artifacts        []CampaignArtifactMapping     `json:"artifacts"`
	Counts           CampaignMergeCounts           `json:"counts"`
	ChronologyDigest string                        `json:"chronologyDigest"`
	IDMapDigest      string                        `json:"idMapDigest"`
	Digest           string                        `json:"digest"`
}

type CampaignMergeSubmission struct {
	Spec               CampaignMergeSpec `json:"spec"`
	ApprovedPlanDigest string            `json:"approvedPlanDigest"`
}

type CampaignDiscardSubmission struct {
	Confirmation           string `json:"confirmation"`
	Reason                 string `json:"reason"`
	ExpectedCampaignDigest string `json:"expectedCampaignDigest"`
	ExpectedTreeDigest     string `json:"expectedTreeDigest,omitempty"`
}

type preparedCampaignMerge struct {
	Plan       CampaignMergePlan
	IDMap      CampaignMergeIDMap
	Chronology CampaignChronology
	Writes     []StateWrite
	Artifacts  []StateArtifactWrite
}

func transactionRetiredTrees(request StateTransactionRequest) []string {
	if len(request.RetireActiveTrees) != 0 {
		return append([]string(nil), request.RetireActiveTrees...)
	}
	if request.RetireActiveTree != "" {
		return []string{request.RetireActiveTree}
	}
	return nil
}

func validateStateTopologyRequest(request StateTransactionRequest) error {
	retired := transactionRetiredTrees(request)
	if request.RetireActiveTree != "" && len(request.RetireActiveTrees) != 0 {
		return errors.New("transaction cannot mix legacy single-tree and multi-tree retirement fields")
	}
	validateTree := func(value string) error {
		parts := strings.Split(value, "/")
		if len(parts) != 2 || parts[0] != "active" || !managedSlugRE.MatchString(parts[1]) {
			return errors.New("active tree must be the exact path active/<campaign-slug>")
		}
		return nil
	}
	seen := map[string]bool{}
	for _, tree := range retired {
		if seen[tree] || validateTree(tree) != nil {
			return errors.New("retired active trees must be unique exact active/<campaign-slug> paths")
		}
		seen[tree] = true
	}
	switch request.Action {
	case "closure.finalize":
		if request.CreateActiveTree != "" || len(request.RetireActiveTrees) != 0 ||
			request.EventJournal != "" || len(request.RetireTreeDigests) != 0 ||
			!validOne(request.Authority, "manager", "system") ||
			request.RetireActiveTree != "active/"+request.CampaignSlug ||
			validateRetiredEventJournal(request.RetiredEventJournal) != nil {
			return errors.New("closure.finalize may retire only its exact active tree into a durable archive journal")
		}
	case "campaign.merge":
		if request.Authority != "manager" || request.CreateActiveTree != "active/"+request.CampaignSlug ||
			validateTree(request.CreateActiveTree) != nil || len(retired) < 2 ||
			request.RetireActiveTree != "" || request.RetiredEventJournal != "" ||
			request.EventJournal != "" || len(request.Writes) == 0 || !digestRE.MatchString(request.ReviewHandle) {
			return errors.New("campaign.merge requires a new exact target tree, at least two exact source trees, typed writes, and an approved plan digest")
		}
		if seen[request.CreateActiveTree] || len(request.RetireTreeDigests) != len(retired) {
			return errors.New("campaign.merge target must differ from every source and every source tree needs one exact digest")
		}
		for _, tree := range retired {
			if !digestRE.MatchString(request.RetireTreeDigests[tree]) {
				return errors.New("campaign.merge source tree digest is missing or malformed")
			}
		}
	case "campaign.discard":
		if request.Authority != "manager" || request.CreateActiveTree != "" || len(retired) != 1 ||
			request.RetireActiveTree != "" || retired[0] != "active/"+request.CampaignSlug ||
			request.RetiredEventJournal != "" || request.EventJournal != campaignDiscardEventJournal ||
			len(request.Writes) != 0 || len(request.Artifacts) != 0 || strings.TrimSpace(request.Rationale) == "" ||
			!digestRE.MatchString(request.ReviewHandle) || len(request.RetireTreeDigests) != 1 ||
			!digestRE.MatchString(request.RetireTreeDigests[retired[0]]) {
			return errors.New("campaign.discard requires one exact source tree and digest, an exact campaign digest, a reason, and the project discard journal")
		}
	default:
		if request.CreateActiveTree != "" || len(retired) != 0 || len(request.RetireTreeDigests) != 0 ||
			request.EventJournal != "" || request.RetiredEventJournal != "" {
			return errors.New("active-tree topology fields are reserved for closure.finalize, campaign.merge, and campaign.discard")
		}
	}
	return nil
}

type campaignMergeSource struct {
	selector   CampaignMergeSourceSelector
	graph      CampaignGraph
	events     []StateEvent
	treeDigest string
	files      []campaignMergeSourceFile
}

type campaignMergeSourceFile struct {
	relative string
	body     []byte
	digest   string
	size     int64
	mode     uint32
}

func (service *Service) PlanCampaignMerge(ctx context.Context, request CampaignMergePlanRequest) (CampaignMergePlan, error) {
	if service == nil {
		return CampaignMergePlan{}, errors.New("service is required")
	}
	prepared, err := service.prepareCampaignMerge(ctx, request)
	if err != nil {
		return CampaignMergePlan{}, err
	}
	return prepared.Plan, nil
}

func validateCampaignMergePlanRequest(request CampaignMergePlanRequest) error {
	if strings.TrimSpace(request.Actor) == "" || request.ExpectedHeadRevision < 0 ||
		!digestRE.MatchString(request.ExpectedHeadDigest) {
		return errors.New("campaign merge planning requires actor and exact expected head revision and digest")
	}
	spec := request.Spec
	if !campaignIDRE.MatchString(spec.TargetCampaignID) || !managedSlugRE.MatchString(spec.TargetSlug) ||
		strings.TrimSpace(spec.Title) == "" || strings.TrimSpace(spec.Objective) == "" ||
		strings.TrimSpace(spec.Owner) == "" || len(spec.Sources) < 2 || len(spec.PermittedManagers) == 0 ||
		!containsString(spec.PermittedManagers, request.Actor) ||
		!containsString(spec.PermittedManagers, spec.Owner) {
		return errors.New("campaign merge target identity, objective, owner, sources, and permitted managers are required")
	}
	mergedAt, err := time.Parse(time.RFC3339Nano, spec.MergedAt)
	if err != nil || mergedAt.Location() != time.UTC {
		return errors.New("campaign merge mergedAt must be an explicit UTC RFC3339 timestamp")
	}
	seenCampaigns := map[string]bool{}
	seenSlugs := map[string]bool{}
	for _, source := range spec.Sources {
		if !campaignIDRE.MatchString(source.CampaignID) || !managedSlugRE.MatchString(source.CampaignSlug) ||
			seenCampaigns[source.CampaignID] || seenSlugs[source.CampaignSlug] ||
			source.CampaignID == spec.TargetCampaignID || source.CampaignSlug == spec.TargetSlug {
			return errors.New("campaign merge sources must be unique exact non-target campaign identifiers and slugs")
		}
		seenCampaigns[source.CampaignID], seenSlugs[source.CampaignSlug] = true, true
	}
	if len(spec.Chronology) == 0 {
		return errors.New("campaign merge requires an explicit historical chronology")
	}
	seenStages := map[string]bool{}
	for _, stage := range spec.Chronology {
		start, startErr := time.Parse("2006-01-02", stage.StartDate)
		end, endErr := time.Parse("2006-01-02", stage.EndDate)
		if !correlationIDRE.MatchString(stage.ID) || seenStages[stage.ID] || startErr != nil || endErr != nil ||
			end.Before(start) || strings.TrimSpace(stage.Title) == "" || strings.TrimSpace(stage.Summary) == "" ||
			!validOne(stage.Status, "historical", "migration", "pending") {
			return errors.New("campaign merge chronology entries need unique IDs, valid date ranges, titles, summaries, and historical, migration, or pending status")
		}
		seenStages[stage.ID] = true
		if err := requireUniqueNonEmpty(
			"campaign merge chronology source campaigns", stage.SourceCampaignIDs); err != nil {
			return err
		}
		for _, sourceID := range stage.SourceCampaignIDs {
			if !seenCampaigns[sourceID] {
				return fmt.Errorf("campaign merge chronology stage %s names unknown source %s", stage.ID, sourceID)
			}
		}
	}
	for _, stage := range spec.Chronology {
		for _, dependency := range stage.DependsOn {
			if !seenStages[dependency] || dependency == stage.ID {
				return fmt.Errorf("campaign merge chronology stage %s has an invalid dependency %s", stage.ID, dependency)
			}
		}
	}
	dependencies := map[string][]string{}
	for _, stage := range spec.Chronology {
		dependencies[stage.ID] = append([]string(nil), stage.DependsOn...)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("campaign merge chronology dependency cycle reaches %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range dependencies[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range dependencies {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) prepareCampaignMerge(ctx context.Context, request CampaignMergePlanRequest) (preparedCampaignMerge, error) {
	if err := validateCampaignMergePlanRequest(request); err != nil {
		return preparedCampaignMerge{}, err
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	if err := store.Recover(ctx); err != nil {
		return preparedCampaignMerge{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	if head.Revision != request.ExpectedHeadRevision || head.Digest != request.ExpectedHeadDigest {
		return preparedCampaignMerge{}, staleHeadConflict(
			request.ExpectedHeadRevision, request.ExpectedHeadDigest, head, request.Spec.TargetCampaignID)
	}
	inventory, err := store.loadCommittedInventory(head)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	dirty, err := store.inventoryDrift(inventory)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	if len(dirty) != 0 {
		return preparedCampaignMerge{}, fmt.Errorf("%w: %s", ErrStateDirty, describeStateDrift(dirty))
	}
	activeCampaigns, err := store.ListCampaigns()
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	for _, active := range activeCampaigns {
		if active.ID == request.Spec.TargetCampaignID || active.Slug == request.Spec.TargetSlug {
			return preparedCampaignMerge{}, fmt.Errorf(
				"%w: campaign merge target identity %s/%s is already in use",
				ErrStateConflict, request.Spec.TargetCampaignID, request.Spec.TargetSlug)
		}
	}
	if closed, closedErr := campaignWasClosed(store, request.Spec.TargetCampaignID); closedErr != nil {
		return preparedCampaignMerge{}, closedErr
	} else if closed {
		return preparedCampaignMerge{}, fmt.Errorf(
			"%w: campaign merge target id %s belongs to immutable closed history",
			ErrStateConflict, request.Spec.TargetCampaignID)
	}
	if discarded, discardedErr := campaignWasDiscarded(
		store, request.Spec.TargetCampaignID, "active/"+request.Spec.TargetSlug,
	); discardedErr != nil {
		return preparedCampaignMerge{}, discardedErr
	} else if discarded {
		return preparedCampaignMerge{}, fmt.Errorf(
			"%w: campaign merge target identity %s/%s was intentionally discarded",
			ErrStateConflict, request.Spec.TargetCampaignID, request.Spec.TargetSlug)
	}
	targetTree := "active/" + request.Spec.TargetSlug
	targetPath, err := store.canonicalOutputPath(targetTree)
	if err != nil {
		return preparedCampaignMerge{}, err
	}
	if _, err := os.Lstat(targetPath); err == nil {
		return preparedCampaignMerge{}, fmt.Errorf("%w: campaign merge target %s already exists", ErrStateConflict, targetTree)
	} else if !os.IsNotExist(err) {
		return preparedCampaignMerge{}, err
	}
	sources := make([]campaignMergeSource, 0, len(request.Spec.Sources))
	for _, selector := range request.Spec.Sources {
		if err := ctx.Err(); err != nil {
			return preparedCampaignMerge{}, err
		}
		graph, err := store.LoadCampaignGraph(selector.CampaignID)
		if err != nil {
			return preparedCampaignMerge{}, err
		}
		if graph.Campaign.ID != selector.CampaignID || graph.Campaign.Slug != selector.CampaignSlug {
			return preparedCampaignMerge{}, fmt.Errorf("campaign merge source %s does not match exact slug %s", selector.CampaignID, selector.CampaignSlug)
		}
		if !validOne(graph.Campaign.Status, "open", "paused") {
			return preparedCampaignMerge{}, fmt.Errorf("campaign merge source %s is %s; only open or paused campaigns may merge", selector.CampaignID, graph.Campaign.Status)
		}
		if !containsString(graph.Campaign.PermittedManagers, request.Actor) {
			return preparedCampaignMerge{}, fmt.Errorf("actor %q is not a permitted manager of merge source %s", request.Actor, selector.CampaignID)
		}
		if graph.ClosurePlan != nil || graph.ClosureJob != nil || graph.ClosureCoverage != nil || graph.ClosureReceipt != nil {
			return preparedCampaignMerge{}, fmt.Errorf("campaign merge source %s has live closure records; reopen or finish that closure before merging", selector.CampaignID)
		}
		sourceRoot := filepath.Join(store.Boundary.Root, "active", selector.CampaignSlug)
		treeDigest, err := digestDirectoryTree(sourceRoot)
		if err != nil {
			return preparedCampaignMerge{}, err
		}
		files, events, err := loadCampaignMergeSourceFiles(sourceRoot, selector.CampaignSlug)
		if err != nil {
			return preparedCampaignMerge{}, err
		}
		sources = append(sources, campaignMergeSource{
			selector: selector, graph: graph, events: events, treeDigest: treeDigest, files: files,
		})
	}
	return buildPreparedCampaignMerge(head, request, sources)
}

func loadCampaignMergeSourceFiles(root, slug string) ([]campaignMergeSourceFile, []StateEvent, error) {
	files := []campaignMergeSourceFile{}
	events := []StateEvent{}
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("campaign merge source %s contains a symbolic link", slug)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("campaign merge source %s contains a non-regular file", slug)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if info.Size() > maxCampaignMergeArtifactBytes {
			return fmt.Errorf(
				"campaign merge source %s file %s exceeds the %d-byte merge limit",
				slug, relative, maxCampaignMergeArtifactBytes)
		}
		body, err := readSingleLinkRegularFile(current)
		if err != nil {
			return err
		}
		files = append(files, campaignMergeSourceFile{
			relative: relative, body: body, digest: "sha256:" + SHA256Bytes(body),
			size: int64(len(body)), mode: uint32(info.Mode().Perm()),
		})
		if relative == "events/events.jsonl" {
			for index, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				var event StateEvent
				if err := decodeStrictJSON([]byte(line), &event); err != nil || verifyStateEvent(event) != nil {
					return fmt.Errorf("campaign merge source %s event line %d is invalid", slug, index+1)
				}
				events = append(events, event)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relative < files[j].relative })
	return files, events, nil
}
