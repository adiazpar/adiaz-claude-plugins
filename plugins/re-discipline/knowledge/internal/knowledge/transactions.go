package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	FailAfterJournal        = "after-journal"
	FailAfterRecordPublish  = "after-record-publish"
	FailAfterEventPublish   = "after-event-publish"
	FailAfterReceiptPublish = "after-receipt-publish"
	FailBeforeHeadPublish   = "before-head-publish"
	FailAfterHeadPublish    = "after-head-publish"
)

// StateWrite is a typed canonical record replacement. ExpectedRevision zero
// means create; updates also require the exact previous record digest.
type StateWrite struct {
	Path             string `json:"path"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ExpectedDigest   string `json:"expectedDigest,omitempty"`
	Record           any    `json:"-"`
}

// StateArtifactWrite publishes one run-launch, closure projection, or archive
// artifact in the same commit as its canonical records. An empty
// ExpectedDigest means the destination must not exist; otherwise it must match
// exactly.
type StateArtifactWrite struct {
	Path           string `json:"path"`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	ContentDigest  string `json:"contentDigest"`
	Body           []byte `json:"-"`
}

type StateTransactionRequest struct {
	CampaignSlug         string               `json:"campaignSlug"`
	CampaignID           string               `json:"campaignId"`
	Actor                string               `json:"actor"`
	Authority            string               `json:"authority"`
	Action               string               `json:"action"`
	Rationale            string               `json:"rationale,omitempty"`
	ReviewHandle         string               `json:"reviewHandle,omitempty"`
	CorrelationID        string               `json:"correlationId"`
	IdempotencyKey       string               `json:"idempotencyKey"`
	ExpectedHeadRevision int64                `json:"expectedHeadRevision"`
	ExpectedHeadDigest   string               `json:"expectedHeadDigest"`
	Writes               []StateWrite         `json:"-"`
	Artifacts            []StateArtifactWrite `json:"-"`
}

type StateRecordResult struct {
	Path          string `json:"path"`
	RecordID      string `json:"recordId"`
	Revision      int64  `json:"revision"`
	RecordDigest  string `json:"recordDigest"`
	ContentDigest string `json:"contentDigest"`
}

type StateArtifactResult struct {
	Path           string `json:"path"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	ContentDigest  string `json:"contentDigest"`
}

type StateTransactionReceipt struct {
	SchemaVersion       int                   `json:"schemaVersion"`
	TransactionID       string                `json:"transactionId"`
	CorrelationID       string                `json:"correlationId"`
	IdempotencyKey      string                `json:"idempotencyKey"`
	RequestDigest       string                `json:"requestDigest"`
	PreviousHead        StateHead             `json:"previousHead"`
	ResultingHead       StateHead             `json:"resultingHead"`
	Event               StateEvent            `json:"event"`
	Records             []StateRecordResult   `json:"records"`
	Artifacts           []StateArtifactResult `json:"artifacts,omitempty"`
	GeneratedViewDigest string                `json:"generatedViewDigest"`
	CommittedAt         string                `json:"committedAt"`
	ResultDigest        string                `json:"resultDigest"`
}

type preparedStateWrite struct {
	Path             string
	ExpectedRevision int64
	ExpectedDigest   string
	Record           any
	Body             []byte
	RecordID         string
	Revision         int64
	RecordDigest     string
	ContentDigest    string
	Internal         bool
}

type preparedStateArtifact struct {
	Path           string
	ExpectedDigest string
	ContentDigest  string
	Body           []byte
}

type preparedTransaction struct {
	Request       StateTransactionRequest
	Writes        []preparedStateWrite
	Artifacts     []preparedStateArtifact
	RequestDigest string
	TransactionID string
}

// StateTransactionWriteDescriptor is the body-independent write commitment
// used by the canonical transaction digest. Record bytes are committed by
// both recordDigest and contentDigest without being duplicated in journals.
type StateTransactionWriteDescriptor struct {
	Path             string `json:"path"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ExpectedDigest   string `json:"expectedDigest,omitempty"`
	RecordID         string `json:"recordId"`
	Revision         int64  `json:"revision"`
	RecordDigest     string `json:"recordDigest"`
	ContentDigest    string `json:"contentDigest"`
}

type StateTransactionArtifactDescriptor struct {
	Path           string `json:"path"`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	ContentDigest  string `json:"contentDigest"`
}

// StateTransactionDescriptor is the versioned, portable transaction shape
// committed by RequestDigest and described by transaction.schema.json.
type StateTransactionDescriptor struct {
	SchemaVersion        int                                  `json:"schemaVersion"`
	CampaignSlug         string                               `json:"campaignSlug"`
	CampaignID           string                               `json:"campaignId"`
	Actor                string                               `json:"actor"`
	Authority            string                               `json:"authority"`
	Action               string                               `json:"action"`
	Rationale            string                               `json:"rationale,omitempty"`
	ReviewHandle         string                               `json:"reviewHandle,omitempty"`
	CorrelationID        string                               `json:"correlationId"`
	IdempotencyKey       string                               `json:"idempotencyKey"`
	ExpectedHeadRevision int64                                `json:"expectedHeadRevision"`
	ExpectedHeadDigest   string                               `json:"expectedHeadDigest"`
	Writes               []StateTransactionWriteDescriptor    `json:"writes"`
	Artifacts            []StateTransactionArtifactDescriptor `json:"artifacts,omitempty"`
}

func (store *StateStore) Apply(ctx context.Context, request StateTransactionRequest) (StateTransactionReceipt, error) {
	if store == nil {
		return StateTransactionReceipt{}, errors.New("state store is required")
	}
	if err := ctx.Err(); err != nil {
		return StateTransactionReceipt{}, err
	}
	prepared, err := prepareTransactionRequest(request)
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	return store.applyPreparedTransaction(ctx, prepared)
}

func (store *StateStore) applyPreparedTransaction(ctx context.Context, prepared preparedTransaction) (StateTransactionReceipt, error) {
	return store.runPreparedTransaction(ctx, prepared)
}

func prepareTransactionRequest(request StateTransactionRequest) (preparedTransaction, error) {
	if !managedSlugRE.MatchString(request.CampaignSlug) || !campaignIDRE.MatchString(request.CampaignID) ||
		strings.TrimSpace(request.Actor) == "" || !validOne(request.Authority, "manager", "investigator", "reviewer", "curator", "system") ||
		!actionIDRE.MatchString(request.Action) || !correlationIDRE.MatchString(request.CorrelationID) ||
		strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 128 ||
		request.ExpectedHeadRevision < 0 || !digestRE.MatchString(request.ExpectedHeadDigest) ||
		(len(request.Writes) == 0 && len(request.Artifacts) == 0) {
		return preparedTransaction{}, errors.New("transaction identity, authority, action, expected head, and writes are required")
	}
	prepared := make([]preparedStateWrite, 0, len(request.Writes))
	seenPaths := map[string]bool{}
	seenIDs := map[string]bool{}
	predictedEventID := eventIDForRequest(request)
	for _, write := range request.Writes {
		item, err := prepareStateWrite(
			request.CampaignSlug, request.CampaignID, request.CorrelationID,
			predictedEventID, request.ExpectedHeadRevision+1, write)
		if err != nil {
			return preparedTransaction{}, err
		}
		if seenPaths[item.Path] || seenIDs[item.RecordID] {
			return preparedTransaction{}, fmt.Errorf("transaction repeats path or record %s", item.RecordID)
		}
		seenPaths[item.Path], seenIDs[item.RecordID] = true, true
		prepared = append(prepared, item)
	}
	sort.Slice(prepared, func(i, j int) bool { return prepared[i].Path < prepared[j].Path })
	artifacts := make([]preparedStateArtifact, 0, len(request.Artifacts))
	for _, artifact := range request.Artifacts {
		item, err := prepareStateArtifact(request.Action, request.Authority, request.CampaignSlug, artifact)
		if err != nil {
			return preparedTransaction{}, err
		}
		if seenPaths[item.Path] {
			return preparedTransaction{}, fmt.Errorf("transaction repeats path %s", item.Path)
		}
		seenPaths[item.Path] = true
		artifacts = append(artifacts, item)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	if err := validateRunPreparationArtifactBindings(request, artifacts); err != nil {
		return preparedTransaction{}, err
	}
	descriptors := make([]StateTransactionWriteDescriptor, 0, len(prepared))
	for _, write := range prepared {
		descriptors = append(descriptors, StateTransactionWriteDescriptor{
			Path: write.Path, ExpectedRevision: write.ExpectedRevision,
			ExpectedDigest: write.ExpectedDigest, RecordID: write.RecordID,
			Revision: write.Revision, RecordDigest: write.RecordDigest,
			ContentDigest: write.ContentDigest,
		})
	}
	artifactDescriptors := make([]StateTransactionArtifactDescriptor, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactDescriptors = append(artifactDescriptors, StateTransactionArtifactDescriptor{
			Path: artifact.Path, ExpectedDigest: artifact.ExpectedDigest, ContentDigest: artifact.ContentDigest,
		})
	}
	semantic := StateTransactionDescriptor{
		SchemaVersion: CampaignSchemaVersion,
		CampaignSlug:  request.CampaignSlug, CampaignID: request.CampaignID,
		Actor: request.Actor, Authority: request.Authority, Action: request.Action,
		Rationale: request.Rationale, ReviewHandle: request.ReviewHandle,
		CorrelationID: request.CorrelationID, IdempotencyKey: request.IdempotencyKey,
		ExpectedHeadRevision: request.ExpectedHeadRevision, ExpectedHeadDigest: request.ExpectedHeadDigest,
		Writes: descriptors, Artifacts: artifactDescriptors,
	}
	requestDigest, err := CanonicalDigest(semantic)
	if err != nil {
		return preparedTransaction{}, err
	}
	transactionID := "T-" + SHA256String(request.IdempotencyKey + "\x00" + requestDigest)[:24]
	return preparedTransaction{
		Request: request, Writes: prepared, Artifacts: artifacts,
		RequestDigest: requestDigest, TransactionID: transactionID,
	}, nil
}

func prepareStateArtifact(action, authority, slug string, artifact StateArtifactWrite) (preparedStateArtifact, error) {
	if err := validateRelativeRecordPath(artifact.Path); err != nil {
		return preparedStateArtifact{}, err
	}
	if artifact.ExpectedDigest != "" && !digestRE.MatchString(artifact.ExpectedDigest) {
		return preparedStateArtifact{}, errors.New("artifact expected digest is invalid")
	}
	if len(artifact.Body) == 0 || int64(len(artifact.Body)) > maxSourceBytes {
		return preparedStateArtifact{}, fmt.Errorf("artifact body must contain 1..%d bytes", maxSourceBytes)
	}
	digest := "sha256:" + SHA256Bytes(artifact.Body)
	if artifact.ContentDigest != digest {
		return preparedStateArtifact{}, errors.New("artifact content digest does not match its body")
	}
	switch {
	case authority == "manager" && validOne(action, "run.prepare", "closure.remediation.run.create"):
		if artifact.ExpectedDigest != "" {
			return preparedStateArtifact{}, errors.New("run preparation artifacts are create-only")
		}
		if err := validateRunPreparationArtifactPath(slug, artifact.Path); err != nil {
			return preparedStateArtifact{}, err
		}
	case strings.HasPrefix(action, "closure.") && validOne(authority, "manager", "system"):
		if err := validateClosureArtifactPath(slug, artifact.Path); err != nil {
			return preparedStateArtifact{}, err
		}
	default:
		return preparedStateArtifact{}, errors.New("only manager run preparation or manager/system closure actions may publish artifacts")
	}
	return preparedStateArtifact{
		Path: artifact.Path, ExpectedDigest: artifact.ExpectedDigest,
		ContentDigest: artifact.ContentDigest, Body: append([]byte(nil), artifact.Body...),
	}, nil
}

func validateRunPreparationArtifactPath(slug, value string) error {
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[0] != "active" || parts[1] != slug ||
		parts[2] != "runs" || !runIDRE.MatchString(parts[3]) ||
		!validOne(parts[4], "brief.md", "context-pack.json") {
		return errors.New("run preparation artifacts must be active/<slug>/runs/<R-id>/{brief.md,context-pack.json}")
	}
	return nil
}

func validateRunPreparationArtifactBindings(
	request StateTransactionRequest,
	artifacts []preparedStateArtifact,
) error {
	if !validOne(request.Action, "run.prepare", "closure.remediation.run.create") {
		return nil
	}
	if request.Authority != "manager" {
		return errors.New("run preparation requires manager authority")
	}
	var run *RunRecord
	for _, write := range request.Writes {
		var candidate *RunRecord
		switch value := write.Record.(type) {
		case RunRecord:
			copy := value
			candidate = &copy
		case *RunRecord:
			candidate = value
		}
		if candidate == nil {
			continue
		}
		if run != nil {
			return errors.New("run preparation transaction must contain exactly one run")
		}
		run = candidate
	}
	if run == nil {
		return errors.New("run preparation transaction must contain exactly one run")
	}
	requiresArtifacts := run.Role != "manager" || len(artifacts) != 0
	if !requiresArtifacts {
		return nil
	}
	if len(artifacts) != 2 || run.Brief == nil || run.ContextPack == nil {
		return errors.New("delegated run must atomically create its brief and context pack")
	}
	prefix := "active/" + request.CampaignSlug + "/runs/" + run.ID + "/"
	expected := map[string]string{
		prefix + "brief.md":          run.Brief.SHA256,
		prefix + "context-pack.json": run.ContextPack.SHA256,
	}
	if run.Brief.Path != prefix+"brief.md" || run.ContextPack.Path != prefix+"context-pack.json" {
		return errors.New("delegated run launch handles do not use its canonical workspace paths")
	}
	for _, artifact := range artifacts {
		want, present := expected[artifact.Path]
		if !present || artifact.ExpectedDigest != "" || artifact.ContentDigest != want {
			return errors.New("delegated run launch artifact does not match its immutable run handle")
		}
		delete(expected, artifact.Path)
	}
	if len(expected) != 0 {
		return errors.New("delegated run launch is only partially prepared")
	}
	return nil
}

func validateClosureArtifactPath(slug, value string) error {
	activeStaging := "active/" + slug + "/closure/staging/"
	switch {
	case strings.HasPrefix(value, "docs/truth/"):
		return validateTruthDestination(value)
	case strings.HasPrefix(value, "docs/backlog/"), strings.HasPrefix(value, "docs/playbooks/"):
		if path.Ext(value) != ".md" {
			return errors.New("backlog and playbook projections must be Markdown files")
		}
	case strings.HasPrefix(value, "docs/history/"):
		if validateArchiveManifestWritePath(value) == nil {
			return errors.New("archive manifests must use the typed ArchiveManifest record")
		}
	case strings.HasPrefix(value, activeStaging):
	default:
		return errors.New("closure artifact path is outside the projection and archive allowlist")
	}
	if strings.HasSuffix(value, "/") {
		return errors.New("closure artifact path must name a file")
	}
	return nil
}

func prepareStateWrite(slug, campaignID, correlationID, eventID string, resultingHeadRevision int64, write StateWrite) (preparedStateWrite, error) {
	if write.ExpectedRevision < 0 || (write.ExpectedRevision == 0 && write.ExpectedDigest != "") ||
		(write.ExpectedRevision > 0 && !digestRE.MatchString(write.ExpectedDigest)) {
		return preparedStateWrite{}, errors.New("record expected revision and digest are inconsistent")
	}
	if err := validateRelativeRecordPath(write.Path); err != nil {
		return preparedStateWrite{}, err
	}
	write.Record = injectTransactionOwnedFields(write.Record, eventID, resultingHeadRevision)
	record, body, err := sealStateRecord(write.Record, write.Path)
	if err != nil {
		return preparedStateWrite{}, err
	}
	id, revision, digest, recordCampaign, recordCorrelation, err := stateRecordIdentity(
		record, write.ExpectedRevision, correlationID)
	if err != nil {
		return preparedStateWrite{}, err
	}
	if recordCampaign != campaignID || recordCorrelation != correlationID || revision != write.ExpectedRevision+1 {
		return preparedStateWrite{}, fmt.Errorf("record %s campaign, correlation, or resulting revision is inconsistent", id)
	}
	if _, archive := record.(ArchiveManifest); archive {
		if err := validateArchiveManifestWritePath(write.Path); err != nil {
			return preparedStateWrite{}, err
		}
	} else {
		expectedPath, err := stateRecordPath(slug, record)
		if err != nil {
			return preparedStateWrite{}, err
		}
		if write.Path != expectedPath {
			return preparedStateWrite{}, fmt.Errorf("record %s must be written to %s", id, expectedPath)
		}
	}
	return preparedStateWrite{
		Path: write.Path, ExpectedRevision: write.ExpectedRevision,
		ExpectedDigest: write.ExpectedDigest, Record: record, Body: body,
		RecordID: id, Revision: revision, RecordDigest: digest,
		ContentDigest: "sha256:" + SHA256Bytes(body),
	}, nil
}

func eventIDForRequest(request StateTransactionRequest) string {
	suffix := strings.ToUpper(SHA256String(strings.Join([]string{
		request.CorrelationID, request.IdempotencyKey,
		fmt.Sprintf("%d", request.ExpectedHeadRevision), request.ExpectedHeadDigest,
	}, "\x00"))[:12])
	return "E-19700101-000000-" + suffix
}

func injectTransactionOwnedFields(value any, eventID string, resultingHeadRevision int64) any {
	switch record := value.(type) {
	case ClosureReceipt:
		if record.EventID == "" {
			record.EventID = eventID
		}
		if record.StateHeadRevision == 0 {
			record.StateHeadRevision = resultingHeadRevision
		}
		return record
	case *ClosureReceipt:
		if record == nil {
			return record
		}
		copy := *record
		if copy.EventID == "" {
			copy.EventID = eventID
		}
		if copy.StateHeadRevision == 0 {
			copy.StateHeadRevision = resultingHeadRevision
		}
		return copy
	case ArchiveManifest:
		if record.EventHead == "" {
			record.EventHead = eventID
		}
		return record
	case *ArchiveManifest:
		if record == nil {
			return record
		}
		copy := *record
		if copy.EventHead == "" {
			copy.EventHead = eventID
		}
		return copy
	case ReviewRecord:
		if len(record.ResultingEventIDs) == 0 {
			record.ResultingEventIDs = []string{eventID}
		}
		return record
	case *ReviewRecord:
		if record == nil {
			return record
		}
		copy := *record
		if len(copy.ResultingEventIDs) == 0 {
			copy.ResultingEventIDs = []string{eventID}
		}
		return copy
	default:
		return value
	}
}

func sealStateRecord(value any, recordPath string) (any, []byte, error) {
	switch source := value.(type) {
	case CampaignRecord:
		return sealCampaignRecord(source)
	case *CampaignRecord:
		if source == nil {
			return nil, nil, errors.New("nil campaign record")
		}
		return sealCampaignRecord(*source)
	case WorkItemRecord:
		return sealWorkItemRecord(source)
	case *WorkItemRecord:
		if source == nil {
			return nil, nil, errors.New("nil work-item record")
		}
		return sealWorkItemRecord(*source)
	case RunRecord:
		return sealRunRecord(source)
	case *RunRecord:
		if source == nil {
			return nil, nil, errors.New("nil run record")
		}
		return sealRunRecord(*source)
	case FindingDocument:
		return sealFindingStateRecord(source, recordPath)
	case *FindingDocument:
		if source == nil {
			return nil, nil, errors.New("nil finding document")
		}
		return sealFindingStateRecord(*source, recordPath)
	case IntakeRecord:
		return sealIntakeRecord(source)
	case *IntakeRecord:
		if source == nil {
			return nil, nil, errors.New("nil intake record")
		}
		return sealIntakeRecord(*source)
	case ReviewRecord:
		return sealReviewRecord(source)
	case *ReviewRecord:
		if source == nil {
			return nil, nil, errors.New("nil review record")
		}
		return sealReviewRecord(*source)
	case ClosurePlan:
		return sealClosurePlanRecord(source)
	case *ClosurePlan:
		if source == nil {
			return nil, nil, errors.New("nil closure plan")
		}
		return sealClosurePlanRecord(*source)
	case ClosureJob:
		return sealClosureJobRecord(source)
	case *ClosureJob:
		if source == nil {
			return nil, nil, errors.New("nil closure job")
		}
		return sealClosureJobRecord(*source)
	case ClosureCoverage:
		return sealClosureCoverageRecord(source)
	case *ClosureCoverage:
		if source == nil {
			return nil, nil, errors.New("nil closure coverage")
		}
		return sealClosureCoverageRecord(*source)
	case ClosureReceipt:
		return sealClosureReceiptRecord(source)
	case *ClosureReceipt:
		if source == nil {
			return nil, nil, errors.New("nil closure receipt")
		}
		return sealClosureReceiptRecord(*source)
	case ArchiveManifest:
		return sealArchiveManifestRecord(source)
	case *ArchiveManifest:
		if source == nil {
			return nil, nil, errors.New("nil archive manifest")
		}
		return sealArchiveManifestRecord(*source)
	default:
		return nil, nil, fmt.Errorf("unsupported canonical state record %T", value)
	}
}

func sealCampaignRecord(record CampaignRecord) (any, []byte, error) {
	normalizeRecordLists(&record)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateCampaign(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealWorkItemRecord(record WorkItemRecord) (any, []byte, error) {
	normalizeWorkItemRecord(&record)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateWorkItem(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealRunRecord(record RunRecord) (any, []byte, error) {
	normalizeRunRecord(&record)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateRun(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealFindingStateRecord(document FindingDocument, recordPath string) (any, []byte, error) {
	document.Record.Path = recordPath
	body, err := RenderFindingDocument(document)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := ParseFindingDocument(body, recordPath)
	if err != nil {
		return nil, nil, err
	}
	return parsed, body, nil
}

func sealIntakeRecord(record IntakeRecord) (any, []byte, error) {
	normalizeIntakeRecord(&record)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateIntake(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealReviewRecord(record ReviewRecord) (any, []byte, error) {
	normalizeReviewRecord(&record)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateReview(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealClosurePlanRecord(record ClosurePlan) (any, []byte, error) {
	if err := sealClosurePlan(&record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealClosureJobRecord(record ClosureJob) (any, []byte, error) {
	record.ProjectionFindingIDs = SortedUnique(record.ProjectionFindingIDs)
	record.Blockers = SortedUnique(record.Blockers)
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateClosureJob(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealClosureCoverageRecord(record ClosureCoverage) (any, []byte, error) {
	record.UnresolvedConflicts = SortedUnique(record.UnresolvedConflicts)
	record.MissingDecisions = SortedUnique(record.MissingDecisions)
	if err := sealClosureCoverage(&record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealClosureReceiptRecord(record ClosureReceipt) (any, []byte, error) {
	if err := sealClosureReceipt(&record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func sealArchiveManifestRecord(record ArchiveManifest) (any, []byte, error) {
	record.Digest = ""
	digest, err := CanonicalDigest(record)
	if err != nil {
		return nil, nil, err
	}
	record.Digest = digest
	if err := ValidateArchiveManifest(record); err != nil {
		return nil, nil, err
	}
	body, err := canonicalJSON(record)
	return record, body, err
}

func stateRecordIdentity(record any, expectedRevision int64, fallbackCorrelation string) (id string, revision int64, digest, campaignID, correlationID string, err error) {
	switch value := record.(type) {
	case CampaignRecord:
		return value.ID, value.Revision, value.Digest, value.ID, value.CorrelationID, nil
	case WorkItemRecord:
		return value.ID, value.Revision, value.Digest, value.CampaignID, value.CorrelationID, nil
	case RunRecord:
		return value.ID, value.Revision, value.Digest, value.CampaignID, value.CorrelationID, nil
	case FindingDocument:
		finding := value.Record
		return finding.ID, finding.Revision, finding.Digest, finding.CampaignID, finding.CorrelationID, nil
	case IntakeRecord:
		return value.ID, value.Revision, value.Digest, value.CampaignID, value.CorrelationID, nil
	case ReviewRecord:
		return value.ID, value.Revision, value.Digest, value.CampaignID, value.CorrelationID, nil
	case ClosurePlan:
		return "closure-plan", expectedRevision + 1, value.Digest, value.CampaignID, fallbackCorrelation, nil
	case ClosureJob:
		return value.ID, value.Revision, value.Digest, value.CampaignID, value.CorrelationID, nil
	case ClosureCoverage:
		return "closure-coverage", expectedRevision + 1, value.Digest, value.CampaignID, fallbackCorrelation, nil
	case ClosureReceipt:
		return "closure-receipt", expectedRevision + 1, value.Digest, value.CampaignID, fallbackCorrelation, nil
	case ArchiveManifest:
		return "archive-manifest", expectedRevision + 1, value.Digest, value.CampaignID, fallbackCorrelation, nil
	default:
		return "", 0, "", "", "", fmt.Errorf("unsupported state record %T", record)
	}
}

func stateRecordPath(slug string, record any) (string, error) {
	switch value := record.(type) {
	case CampaignRecord:
		return canonicalStatePath(slug, "", "", "campaign.json")
	case WorkItemRecord:
		return canonicalStatePath(slug, "work-items", "", value.ID+".json")
	case RunRecord:
		return canonicalStatePath(slug, "runs", value.ID, "run.json")
	case FindingDocument:
		return canonicalStatePath(slug, "findings", "", value.Record.ID+".md")
	case IntakeRecord:
		return canonicalStatePath(slug, "intake", "", value.ID+".json")
	case ReviewRecord:
		return canonicalStatePath(slug, "reviews", "", value.ID+".json")
	case ClosurePlan:
		return canonicalStatePath(slug, "closure", "", "plan.json")
	case ClosureJob:
		return canonicalStatePath(slug, "closure", "", "job.json")
	case ClosureCoverage:
		return canonicalStatePath(slug, "closure", "", "coverage.json")
	case ClosureReceipt:
		return canonicalStatePath(slug, "closure", "", "receipt.json")
	case ArchiveManifest:
		return "", errors.New("archive manifest path is destination-specific")
	default:
		return "", fmt.Errorf("unsupported state record %T", record)
	}
}

func validateArchiveManifestWritePath(value string) error {
	if err := validateRelativeRecordPath(value); err != nil {
		return err
	}
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[0] != "docs" || parts[1] != "history" ||
		parts[2] != "campaigns" || strings.TrimSpace(parts[3]) == "" || parts[4] != "manifest.json" {
		return errors.New("archive manifest must be written below docs/history/campaigns/<archive>/manifest.json")
	}
	return nil
}

func normalizeWorkItemRecord(record *WorkItemRecord) {
	record.Milestones = SortedUnique(record.Milestones)
	record.Acceptance = SortedUnique(record.Acceptance)
	record.Relations.ParentIDs = SortedUnique(record.Relations.ParentIDs)
	record.Relations.ChildIDs = SortedUnique(record.Relations.ChildIDs)
	record.Relations.DependsOn = SortedUnique(record.Relations.DependsOn)
	record.Relations.BlockedBy = SortedUnique(record.Relations.BlockedBy)
	record.Relations.SpawnedByIDs = SortedUnique(record.Relations.SpawnedByIDs)
	record.ActiveRunIDs = SortedUnique(record.ActiveRunIDs)
	record.CompletedRunIDs = SortedUnique(record.CompletedRunIDs)
	record.FindingIDs = SortedUnique(record.FindingIDs)
	record.DecisionIDs = SortedUnique(record.DecisionIDs)
}

func normalizeRunRecord(record *RunRecord) {
	sort.Slice(record.Files, func(i, j int) bool { return record.Files[i].Path < record.Files[j].Path })
	for i := range record.Files {
		record.Files[i].Supports = SortedUnique(record.Files[i].Supports)
	}
	record.ChangedProjectPaths = SortedUnique(record.ChangedProjectPaths)
	record.FindingIDs = SortedUnique(record.FindingIDs)
	record.SpawnedWorkItemIDs = SortedUnique(record.SpawnedWorkItemIDs)
}

func normalizeIntakeRecord(record *IntakeRecord) {
	sort.Slice(record.SourceRuns, func(i, j int) bool { return record.SourceRuns[i].Path < record.SourceRuns[j].Path })
	record.CandidateFindingIDs = SortedUnique(record.CandidateFindingIDs)
	sort.Slice(record.Coverage, func(i, j int) bool { return record.Coverage[i].SourceHandle < record.Coverage[j].SourceHandle })
	record.Conflicts = SortedUnique(record.Conflicts)
	record.SpawnedWorkItems = SortedUnique(record.SpawnedWorkItems)
	record.RetentionDecisions = SortedUnique(record.RetentionDecisions)
	record.Uncertainties = SortedUnique(record.Uncertainties)
	record.RequestedDecisions = SortedUnique(record.RequestedDecisions)
}

func normalizeReviewRecord(record *ReviewRecord) {
	sort.Slice(record.Decisions, func(i, j int) bool { return record.Decisions[i].FindingID < record.Decisions[j].FindingID })
	record.UnresolvedConflicts = SortedUnique(record.UnresolvedConflicts)
	record.ResultingEventIDs = SortedUnique(record.ResultingEventIDs)
	record.ResultingRecordIDs = SortedUnique(record.ResultingRecordIDs)
}
