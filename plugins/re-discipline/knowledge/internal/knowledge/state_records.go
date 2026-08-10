package knowledge

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const CampaignSchemaVersion = 2

var (
	campaignIDRE    = regexp.MustCompile(`^C-[A-Z0-9][A-Z0-9-]{0,62}$`)
	workItemIDRE    = regexp.MustCompile(`^W-[0-9]{4,}$`)
	runIDRE         = regexp.MustCompile(`^R-[0-9]{8}-[0-9]{4,}$`)
	findingIDRE     = regexp.MustCompile(`^F-[0-9]{4,}$`)
	intakeIDRE      = regexp.MustCompile(`^I-[0-9]{4,}$`)
	reviewIDRE      = regexp.MustCompile(`^V-[0-9]{4,}$`)
	eventIDRE       = regexp.MustCompile(`^E-[0-9]{8}-[0-9]{6}-[A-Z0-9]{6,}$`)
	digestRE        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	fileSHA256RE    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	correlationIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	actionIDRE      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	cardRelationRE  = regexp.MustCompile(`^(?:(?:supports|contradicts|depends-on|supersedes|duplicates|answers|incoming-supports|incoming-contradicts|incoming-depends-on|incoming-duplicates|incoming-answers|superseded-by|stale-dependent):F-[0-9]{4,}|stale-path:F-[0-9]{4,}(?:>F-[0-9]{4,})+|spawned:W-[0-9]{4,})$`)
)

// RecordMeta is carried by every canonical 0.8 record. Digest is calculated
// with this field empty, which makes it a content digest rather than a digest
// of its own serialization.
type RecordMeta struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	Revision      int64  `json:"revision"`
	CreatedBy     string `json:"createdBy"`
	UpdatedBy     string `json:"updatedBy"`
	Digest        string `json:"digest"`
	CorrelationID string `json:"correlationId"`
}

type CampaignRecord struct {
	RecordMeta
	Title              string   `json:"title"`
	Slug               string   `json:"slug"`
	Objective          string   `json:"objective"`
	Scope              []string `json:"scope"`
	Exclusions         []string `json:"exclusions"`
	SuccessCriteria    []string `json:"successCriteria"`
	ClosureCriteria    []string `json:"closureCriteria"`
	Status             string   `json:"status"`
	CurrentFocus       []string `json:"currentFocus"`
	Milestones         []string `json:"milestones,omitempty"`
	Owner              string   `json:"owner"`
	PermittedManagers  []string `json:"permittedManagers"`
	OpenedAt           string   `json:"openedAt"`
	PausedAt           string   `json:"pausedAt,omitempty"`
	ClosingAt          string   `json:"closingAt,omitempty"`
	ClosedAt           string   `json:"closedAt,omitempty"`
	LastEventID        string   `json:"lastEventId"`
	ArchiveDestination string   `json:"archiveDestination,omitempty"`
}

type WorkRelations struct {
	ParentIDs    []string `json:"parentIds,omitempty"`
	ChildIDs     []string `json:"childIds,omitempty"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	BlockedBy    []string `json:"blockedBy,omitempty"`
	SpawnedByIDs []string `json:"spawnedByIds,omitempty"`
}

type DefermentTrigger struct {
	Type       string `json:"type"`
	At         string `json:"at,omitempty"`
	WorkItemID string `json:"workItemId,omitempty"`
	State      string `json:"state,omitempty"`
	Action     string `json:"action,omitempty"`
	AffectedID string `json:"affectedId,omitempty"`
}

type DefermentContract struct {
	Reason             string           `json:"reason"`
	RevisitWhen        DefermentTrigger `json:"revisitWhen"`
	Owner              string           `json:"owner"`
	BlocksClosure      bool             `json:"blocksClosure"`
	ClosureDisposition string           `json:"closureDisposition"`
	ClosureDestination string           `json:"closureDestination,omitempty"`
}

type WorkItemRecord struct {
	RecordMeta
	CampaignID      string             `json:"campaignId"`
	Kind            string             `json:"kind"`
	Title           string             `json:"title"`
	Problem         string             `json:"problem"`
	State           string             `json:"state"`
	Priority        string             `json:"priority"`
	Milestones      []string           `json:"milestones,omitempty"`
	Acceptance      []string           `json:"acceptanceCriteria"`
	Relations       WorkRelations      `json:"relations"`
	ActiveRunIDs    []string           `json:"activeRunIds,omitempty"`
	CompletedRunIDs []string           `json:"completedRunIds,omitempty"`
	FindingIDs      []string           `json:"findingIds,omitempty"`
	DecisionIDs     []string           `json:"decisionIds,omitempty"`
	Owner           string             `json:"owner,omitempty"`
	Assignee        string             `json:"assignee,omitempty"`
	ResumeNote      string             `json:"resumeNote,omitempty"`
	Outcome         string             `json:"outcome,omitempty"`
	Deferment       *DefermentContract `json:"deferment,omitempty"`
	SupersededBy    string             `json:"supersededBy,omitempty"`
}

type RunFile struct {
	Path         string   `json:"path"`
	MediaKind    string   `json:"mediaKind"`
	SemanticRole string   `json:"semanticRole"`
	Retention    string   `json:"retention"`
	SHA256       string   `json:"sha256"`
	Supports     []string `json:"supports,omitempty"`
	Destination  string   `json:"destination,omitempty"`
}

type FileHandle struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type RunRecord struct {
	RecordMeta
	CampaignID          string       `json:"campaignId"`
	PrimaryWorkItemID   string       `json:"primaryWorkItemId"`
	ActorID             string       `json:"actorId"`
	Role                string       `json:"role"`
	Status              string       `json:"status"`
	WriteGrants         []WriteGrant `json:"writeGrants,omitempty"`
	Brief               *FileHandle  `json:"brief,omitempty"`
	ContextPack         *FileHandle  `json:"contextPack,omitempty"`
	StartedAt           string       `json:"startedAt,omitempty"`
	ReturnedAt          string       `json:"returnedAt,omitempty"`
	ReviewedAt          string       `json:"reviewedAt,omitempty"`
	TerminalAt          string       `json:"terminalAt,omitempty"`
	Files               []RunFile    `json:"files,omitempty"`
	ChangedProjectPaths []string     `json:"changedProjectPaths,omitempty"`
	Report              *FileHandle  `json:"report,omitempty"`
	FindingIDs          []string     `json:"findingIds,omitempty"`
	SpawnedWorkItemIDs  []string     `json:"spawnedWorkItemIds,omitempty"`
	ResultSummary       string       `json:"resultSummary,omitempty"`
	InvalidatedBy       string       `json:"invalidatedBy,omitempty"`
	RetryOf             string       `json:"retryOf,omitempty"`
}

type EvidenceReference struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	ObjectKey string `json:"objectKey,omitempty"`
	SourceRun string `json:"sourceRun,omitempty"`
}

type FindingRelations struct {
	Supports    []string `json:"supports,omitempty"`
	Contradicts []string `json:"contradicts,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	Supersedes  []string `json:"supersedes,omitempty"`
	Duplicates  []string `json:"duplicates,omitempty"`
	Answers     []string `json:"answers,omitempty"`
	Spawned     []string `json:"spawned,omitempty"`
}

type FindingRecord struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	CampaignID    string              `json:"campaignId"`
	Revision      int64               `json:"revision"`
	CreatedAt     string              `json:"createdAt"`
	UpdatedAt     string              `json:"updatedAt"`
	CreatedBy     string              `json:"createdBy"`
	UpdatedBy     string              `json:"updatedBy"`
	Digest        string              `json:"digest"`
	CorrelationID string              `json:"correlationId"`
	Kind          string              `json:"kind"`
	Subject       string              `json:"subject"`
	Claim         string              `json:"claim"`
	Scope         map[string]any      `json:"scope"`
	AppliesWhen   []string            `json:"appliesWhen,omitempty"`
	KnownLimits   []string            `json:"knownLimits,omitempty"`
	Tags          []string            `json:"tags,omitempty"`
	Subsystems    []string            `json:"subsystems,omitempty"`
	Aliases       []string            `json:"aliases,omitempty"`
	SourceRuns    []string            `json:"sourceRuns"`
	Evidence      []EvidenceReference `json:"evidence"`
	Relations     FindingRelations    `json:"relations"`
	EvidenceGrade string              `json:"evidenceGrade"`
	ReviewState   string              `json:"reviewState"`
	Validity      string              `json:"validity"`
	Projection    string              `json:"projection"`
	PolicyID      string              `json:"policyId,omitempty"`
	VerifiedAt    string              `json:"verifiedAt,omitempty"`
	Body          string              `json:"-"`
	Path          string              `json:"-"`
}

type CoverageEntry struct {
	SourceHandle    string `json:"sourceHandle"`
	SourcePath      string `json:"sourcePath"`
	SourceSHA256    string `json:"sourceSha256"`
	StartLine       int    `json:"startLine"`
	EndLine         int    `json:"endLine"`
	SourceLineCount int    `json:"sourceLineCount"`
	Disposition     string `json:"disposition"`
	TargetID        string `json:"targetId,omitempty"`
	Rationale       string `json:"rationale,omitempty"`
}

// CoverageRetirement is one row of a coverage amendment: the exact span whose
// judgment moved, quoting both sides of the move.
//
// The displaced values are quoted rather than implied because the amendment is
// the sole authority for the change it makes, and an authority that says only
// "this span is now non-claim" cannot be checked against anything. Quoting what
// it displaced turns the entry into a claim the engine can refuse when it does
// not match the persisted row, which is what stops an amendment from being
// written over a row it never actually read.
type CoverageRetirement struct {
	SourceHandle    string `json:"sourceHandle"`
	FromDisposition string `json:"fromDisposition"`
	ToDisposition   string `json:"toDisposition"`
	FromRationale   string `json:"fromRationale"`
	ToRationale     string `json:"toRationale"`
}

// CoverageAmendment is one immutable entry in an intake's append-only coverage
// retirement log. It names the review it preserves, the actor and correlation
// that recorded it, and every span it moved.
//
// The log lives on the intake and not in a side-car record because the
// engine's recurring failure mode is two records that describe the same fact
// and then drift apart; runIsClaimSource exists only because closure and the
// normalization queue each kept their own answer to one question. A side-car
// would leave intake.Coverage asserting `unresolved` forever while something
// else said otherwise, and every reader of intake.Coverage would have to
// remember the overlay. The reader that forgets is the next bug of that class.
type CoverageAmendment struct {
	Revision      int64                `json:"revision"`
	AmendedAt     string               `json:"amendedAt"`
	AmendedBy     string               `json:"amendedBy"`
	CorrelationID string               `json:"correlationId"`
	ReviewID      string               `json:"reviewId"`
	Rationale     string               `json:"rationale"`
	Retirements   []CoverageRetirement `json:"retirements"`
}

type IntakeRecord struct {
	RecordMeta
	CampaignID          string            `json:"campaignId"`
	SourceRuns          []FileHandle      `json:"sourceRuns"`
	CandidateFindingIDs []string          `json:"candidateFindingIds"`
	ProposedDuplicates  [][]string        `json:"proposedDuplicates,omitempty"`
	ProposedMerges      [][]string        `json:"proposedMerges,omitempty"`
	Conflicts           []string          `json:"conflicts,omitempty"`
	SpawnedWorkItems    []string          `json:"spawnedWorkItems,omitempty"`
	RetentionDecisions  []string          `json:"retentionDecisions,omitempty"`
	Coverage            []CoverageEntry   `json:"coverage"`
	Triage              map[string]string `json:"triage"`
	Uncertainties       []string          `json:"uncertainties,omitempty"`
	RequestedDecisions  []string          `json:"requestedDecisions,omitempty"`
	Status              string            `json:"status"`
	// Amendments is last, and its `omitempty` is load-bearing rather than
	// stylistic. canonicalJSON is json.MarshalIndent in struct-field order, so
	// an intake that has never been amended serializes to byte-identical output
	// and keeps the digest it was sealed with. Without `omitempty` every intake
	// ever committed would gain an `"amendments": null` line, every record
	// digest in every campaign would change, and this bookkeeping transition
	// would require a whole-corpus migration to ship.
	Amendments []CoverageAmendment `json:"amendments,omitempty"`
}

// intakeReviewedRevision is the intake revision a manager review binds, and it
// is the only place that arithmetic is written down. An intake is reviewed at
// revision R; the review transaction advances it to R+1; every coverage
// retirement after that advances it by one more without touching a single
// candidate, decision, or byte of the review receipt. Subtracting the amendment
// count is therefore not a fudge factor - it is the statement that a retirement
// is not a new revision of the reviewed content, and ValidateIntake pins the
// last amendment's revision to the record's own so the count cannot be padded.
func intakeReviewedRevision(intake IntakeRecord) int64 {
	return intake.Revision - 1 - int64(len(intake.Amendments))
}

type ReviewDecision struct {
	FindingID          string `json:"findingId"`
	FindingRevision    int64  `json:"findingRevision"`
	Action             string `json:"action"`
	EvidenceCorrection string `json:"evidenceCorrection,omitempty"`
	Projection         string `json:"projection,omitempty"`
	Rationale          string `json:"rationale"`
}

type ReviewRecord struct {
	RecordMeta
	CampaignID          string            `json:"campaignId"`
	Reviewer            string            `json:"reviewer"`
	Authority           string            `json:"authority"`
	IntakeID            string            `json:"intakeId"`
	IntakeRevision      int64             `json:"intakeRevision"`
	PacketDigest        string            `json:"packetDigest"`
	ReviewLoad          ReviewLoadReceipt `json:"reviewLoad"`
	Decisions           []ReviewDecision  `json:"decisions"`
	UnresolvedConflicts []string          `json:"unresolvedConflicts,omitempty"`
	PriorReviewID       string            `json:"priorReviewId,omitempty"`
	ResultingEventIDs   []string          `json:"resultingEventIds,omitempty"`
	ResultingRecordIDs  []string          `json:"resultingRecordIds,omitempty"`
}

type StateEvent struct {
	SchemaVersion        int      `json:"schemaVersion"`
	ID                   string   `json:"id"`
	Timestamp            string   `json:"timestamp"`
	Actor                string   `json:"actor"`
	Authority            string   `json:"authority"`
	Action               string   `json:"action"`
	AffectedIDs          []string `json:"affectedIds"`
	PreviousRevision     int64    `json:"previousRevision"`
	ResultingRevision    int64    `json:"resultingRevision"`
	IdempotencyKey       string   `json:"idempotencyKey"`
	CorrelationID        string   `json:"correlationId"`
	Rationale            string   `json:"rationale,omitempty"`
	ReviewHandle         string   `json:"reviewHandle,omitempty"`
	PreviousEventID      string   `json:"previousEventId,omitempty"`
	PreviousStateDigest  string   `json:"previousStateDigest"`
	ResultingStateDigest string   `json:"resultingStateDigest"`
	MutationDigest       string   `json:"mutationDigest"`
	Digest               string   `json:"digest"`
}

type ClosureCoverage struct {
	SchemaVersion          int               `json:"schemaVersion"`
	CampaignID             string            `json:"campaignId"`
	SourceRunCoverage      map[string]string `json:"sourceRunCoverage"`
	FindingCoverage        map[string]string `json:"findingCoverage"`
	WorkItemCoverage       map[string]string `json:"workItemCoverage"`
	FileRetention          map[string]string `json:"fileRetention"`
	ActiveFileDispositions map[string]string `json:"activeFileDispositions"`
	UnresolvedConflicts    []string          `json:"unresolvedConflicts"`
	MissingDecisions       []string          `json:"missingDecisions"`
	Digest                 string            `json:"digest"`
}

type ClosureJob struct {
	RecordMeta
	CampaignID             string            `json:"campaignId"`
	Stage                  string            `json:"stage"`
	Status                 string            `json:"status"`
	FrozenCampaignRevision int64             `json:"frozenCampaignRevision"`
	ProjectionFindingIDs   []string          `json:"projectionFindingIds"`
	Coverage               *ClosureCoverage  `json:"coverage,omitempty"`
	ArchiveDestination     string            `json:"archiveDestination,omitempty"`
	TruthDigests           map[string]string `json:"truthDigests,omitempty"`
	ProjectionDigests      map[string]string `json:"projectionDigests,omitempty"`
	StagingDigest          string            `json:"stagingDigest,omitempty"`
	ArchiveDigest          string            `json:"archiveDigest,omitempty"`
	Blockers               []string          `json:"blockers,omitempty"`
	// Attempt counts the closure attempts this job has made; a restart after a
	// reopen begins the next one. `omitempty` here is load-bearing rather than
	// cosmetic. readCanonicalRecordValue re-encodes every record it reads and
	// rejects it when the bytes differ from what was committed, and every
	// closure job written before restart existed carries no such field. With
	// `omitempty` a zero value re-encodes to byte-identical content and an
	// identical CanonicalDigest, so campaigns already in flight keep verifying.
	// Read it through closureAttempt, never directly: absent means the first
	// attempt, never attempt zero.
	//
	// The compatibility runs one way only, and the asymmetry is deliberate. A
	// record that already exists keeps loading, but a record this engine writes
	// after a restart does not load on an engine that predates the field:
	// decodeStrictJSON calls DisallowUnknownFields, so `attempt` is fatal to an
	// older decoder and takes the whole campaign graph down with it. A campaign
	// that has restarted closure is therefore pinned to the engine version that
	// restarted it, and rolling that binary back means restoring the job record
	// too. The alternative - a decoder that tolerates fields it does not know -
	// would forfeit the canonical-bytes guarantee every other integrity rule
	// here rests on, so the pinning is the cheaper of the two. See
	// TestTheAttemptFieldIsForwardCompatibleAndNotBackwardReadable.
	Attempt int64 `json:"attempt,omitempty"`
}

type ArchiveManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	CampaignID    string            `json:"campaignId"`
	ClosedAt      string            `json:"closedAt"`
	SourceDigest  string            `json:"sourceDigest"`
	Files         map[string]string `json:"files"`
	Projections   map[string]string `json:"projections"`
	Coverage      ClosureCoverage   `json:"coverage"`
	EventHead     string            `json:"eventHead"`
	Digest        string            `json:"digest"`
}

type ContextCard struct {
	SchemaVersion   int               `json:"schemaVersion"`
	ID              string            `json:"id"`
	CardType        string            `json:"cardType"`
	Claim           string            `json:"claim,omitempty"`
	Title           string            `json:"title,omitempty"`
	Subject         string            `json:"subject,omitempty"`
	Scope           map[string]any    `json:"scope,omitempty"`
	EvidenceGrade   string            `json:"evidenceGrade,omitempty"`
	ReviewState     string            `json:"reviewState,omitempty"`
	Validity        string            `json:"validity,omitempty"`
	SourceClass     string            `json:"sourceClass"`
	RelationAlerts  []string          `json:"relationAlerts,omitempty"`
	Handle          string            `json:"handle"`
	EvidenceHandle  string            `json:"evidenceHandle,omitempty"`
	WhyMatched      []string          `json:"whyMatched,omitempty"`
	ExpansionTokens int               `json:"expansionTokens"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type StateView struct {
	SchemaVersion int           `json:"schemaVersion"`
	Mode          string        `json:"mode"`
	CampaignID    string        `json:"campaignId,omitempty"`
	WorkItemID    string        `json:"workItemId,omitempty"`
	Generation    string        `json:"generation,omitempty"`
	EventHead     string        `json:"eventHead,omitempty"`
	Cards         []ContextCard `json:"cards"`
	TokenCost     int           `json:"tokenCost"`
	Omissions     []string      `json:"omissions,omitempty"`
	Expansions    []string      `json:"expansions,omitempty"`
	Status        string        `json:"status"`
	Digest        string        `json:"digest"`
}

func validOne(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateRecordMeta(meta RecordMeta, pattern *regexp.Regexp) error {
	if meta.SchemaVersion != CampaignSchemaVersion {
		return fmt.Errorf("record %q uses unsupported schema version %d", meta.ID, meta.SchemaVersion)
	}
	if !pattern.MatchString(meta.ID) {
		return fmt.Errorf("record id %q is invalid", meta.ID)
	}
	if meta.Revision < 1 || strings.TrimSpace(meta.CreatedBy) == "" || strings.TrimSpace(meta.UpdatedBy) == "" {
		return errors.New("record revision and actor identities are required")
	}
	if !digestRE.MatchString(meta.Digest) {
		return fmt.Errorf("record %q has an invalid content digest", meta.ID)
	}
	if !correlationIDRE.MatchString(meta.CorrelationID) {
		return fmt.Errorf("record %q has an invalid correlation id", meta.ID)
	}
	if err := validateUTC(meta.CreatedAt); err != nil {
		return fmt.Errorf("createdAt: %w", err)
	}
	if err := validateUTC(meta.UpdatedAt); err != nil {
		return fmt.Errorf("updatedAt: %w", err)
	}
	created, _ := time.Parse(time.RFC3339Nano, meta.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, meta.UpdatedAt)
	if updated.Before(created) {
		return errors.New("updatedAt cannot precede createdAt")
	}
	return nil
}

func validateUTC(value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC {
		return fmt.Errorf("%q is not a UTC RFC3339 timestamp", value)
	}
	return nil
}

func ValidateCampaign(record CampaignRecord) error {
	if err := validateRecordMeta(record.RecordMeta, campaignIDRE); err != nil {
		return err
	}
	if !managedSlugRE.MatchString(record.Slug) || strings.TrimSpace(record.Title) == "" ||
		strings.TrimSpace(record.Objective) == "" || len(record.SuccessCriteria) == 0 ||
		len(record.ClosureCriteria) == 0 || strings.TrimSpace(record.Owner) == "" ||
		len(record.PermittedManagers) == 0 {
		return errors.New("campaign slug, title, objective, owner, success criteria, and closure criteria are required")
	}
	if !validOne(record.Status, "open", "paused", "closing", "closed", "cancelled") {
		return fmt.Errorf("unsupported campaign status %q", record.Status)
	}
	if err := requireUniqueNonEmpty("campaign scope", record.Scope); err != nil {
		return err
	}
	for name, values := range map[string][]string{
		"campaign exclusions": record.Exclusions, "campaign success criteria": record.SuccessCriteria,
		"campaign closure criteria": record.ClosureCriteria, "campaign milestones": record.Milestones,
		"campaign managers": record.PermittedManagers,
	} {
		if err := requireUniqueNonEmpty(name, values); err != nil {
			return err
		}
	}
	if err := validateIDList("campaign focus", record.CurrentFocus, workItemIDRE, ""); err != nil {
		return err
	}
	if !containsString(record.PermittedManagers, record.Owner) {
		return errors.New("campaign owner must be a permitted manager")
	}
	if err := validateUTC(record.OpenedAt); err != nil {
		return fmt.Errorf("openedAt: %w", err)
	}
	for name, value := range map[string]string{
		"pausedAt": record.PausedAt, "closingAt": record.ClosingAt, "closedAt": record.ClosedAt,
	} {
		if value != "" {
			if err := validateUTC(value); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if record.LastEventID != "" && !eventIDRE.MatchString(record.LastEventID) {
		return fmt.Errorf("invalid last event id %q", record.LastEventID)
	}
	if record.Status == "paused" && record.PausedAt == "" {
		return errors.New("paused campaign requires pausedAt")
	}
	if record.Status == "closing" && record.ClosingAt == "" {
		return errors.New("closing campaign requires closingAt")
	}
	if record.Status == "closed" && (record.ClosedAt == "" || strings.TrimSpace(record.ArchiveDestination) == "") {
		return errors.New("closed campaign requires closedAt and archiveDestination")
	}
	if record.ArchiveDestination != "" {
		if err := validateRelativeRecordPath(record.ArchiveDestination); err != nil {
			return fmt.Errorf("archiveDestination: %w", err)
		}
	}
	return nil
}

func ValidateWorkItem(record WorkItemRecord) error {
	if err := validateRecordMeta(record.RecordMeta, workItemIDRE); err != nil {
		return err
	}
	if !campaignIDRE.MatchString(record.CampaignID) || strings.TrimSpace(record.Title) == "" ||
		strings.TrimSpace(record.Problem) == "" || len(record.Acceptance) == 0 {
		return errors.New("work item campaign, title, problem, and acceptance criteria are required")
	}
	if !validOne(record.Kind, "task", "question", "decision", "verification", "blocker") {
		return fmt.Errorf("unsupported work-item kind %q", record.Kind)
	}
	if !validOne(record.State, "proposed", "ready", "active", "blocked", "deferred", "done", "cancelled", "superseded") {
		return fmt.Errorf("unsupported work-item state %q", record.State)
	}
	if strings.TrimSpace(record.Priority) == "" || (strings.TrimSpace(record.Owner) == "" && strings.TrimSpace(record.Assignee) == "") {
		return errors.New("work item priority and an owner or assignee are required")
	}
	if err := requireUniqueNonEmpty("work acceptance criteria", record.Acceptance); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("work milestones", record.Milestones); err != nil {
		return err
	}
	for name, ids := range map[string][]string{
		"parent ids": record.Relations.ParentIDs, "child ids": record.Relations.ChildIDs,
		"dependencies": record.Relations.DependsOn, "blockers": record.Relations.BlockedBy,
		"spawned-by ids": record.Relations.SpawnedByIDs,
	} {
		if err := validateIDList(name, ids, workItemIDRE, record.ID); err != nil {
			return err
		}
	}
	if err := validateIDList("active run ids", record.ActiveRunIDs, runIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("completed run ids", record.CompletedRunIDs, runIDRE, ""); err != nil {
		return err
	}
	if intersects(record.ActiveRunIDs, record.CompletedRunIDs) {
		return errors.New("a run cannot be both active and completed on one work item")
	}
	if err := validateIDList("finding ids", record.FindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("decision ids", record.DecisionIDs); err != nil {
		return err
	}
	if record.State == "deferred" {
		if record.Deferment == nil {
			return errors.New("deferred work requires a deferment contract")
		}
		if err := ValidateDefermentContract(*record.Deferment); err != nil {
			return fmt.Errorf("deferment: %w", err)
		}
	} else if record.Deferment != nil {
		return errors.New("only deferred work may carry a deferment contract")
	}
	if record.State == "done" && strings.TrimSpace(record.Outcome) == "" {
		return errors.New("done work requires a terminal outcome")
	}
	if record.State == "superseded" {
		if !workItemIDRE.MatchString(record.SupersededBy) || record.SupersededBy == record.ID {
			return errors.New("superseded work requires a different replacement work item")
		}
	} else if record.SupersededBy != "" {
		return errors.New("only superseded work may name supersededBy")
	}
	return nil
}

func ValidateRun(record RunRecord) error {
	if err := validateRecordMeta(record.RecordMeta, runIDRE); err != nil {
		return err
	}
	if !campaignIDRE.MatchString(record.CampaignID) || !workItemIDRE.MatchString(record.PrimaryWorkItemID) ||
		strings.TrimSpace(record.ActorID) == "" {
		return errors.New("run campaign, primary work item, and actor are required")
	}
	if !validOne(record.Role, "manager", "investigator", "reviewer", "curator") ||
		!validOne(record.Status, "prepared", "running", "returned", "completed", "blocked", "aborted", "invalidated") {
		return errors.New("run role or status is unsupported")
	}
	if err := ValidateCanonicalWriteGrants(record.WriteGrants); err != nil {
		return err
	}
	if record.Role != "manager" && (record.Brief == nil || record.ContextPack == nil) {
		return errors.New("delegated runs require brief and verified context-pack handles")
	}
	if validOne(record.Status, "returned", "completed", "blocked") && record.Report == nil {
		return errors.New("returned and terminal non-aborted runs require a report handle")
	}
	for name, handle := range map[string]*FileHandle{
		"brief": record.Brief, "context pack": record.ContextPack, "report": record.Report,
	} {
		if handle != nil {
			if err := validateFileHandle(*handle); err != nil {
				return fmt.Errorf("%s handle: %w", name, err)
			}
		}
	}
	if record.Brief != nil && path.Base(record.Brief.Path) != "brief.md" {
		return errors.New("brief handle must name brief.md")
	}
	if record.ContextPack != nil && path.Base(record.ContextPack.Path) != "context-pack.json" {
		return errors.New("context-pack handle must name context-pack.json")
	}
	if record.Report != nil && path.Base(record.Report.Path) != "report.md" {
		return errors.New("report handle must name report.md")
	}
	if validOne(record.Status, "running", "returned", "completed", "blocked") && record.StartedAt == "" {
		return errors.New("a started run requires startedAt")
	}
	if validOne(record.Status, "returned", "completed", "blocked") && record.ReturnedAt == "" {
		return errors.New("a returned run requires returnedAt")
	}
	if validOne(record.Status, "completed", "blocked", "aborted", "invalidated") && record.TerminalAt == "" {
		return errors.New("a terminal run requires terminalAt")
	}
	for name, value := range map[string]string{
		"startedAt": record.StartedAt, "returnedAt": record.ReturnedAt,
		"reviewedAt": record.ReviewedAt, "terminalAt": record.TerminalAt,
	} {
		if value != "" {
			if err := validateUTC(value); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if record.Status == "aborted" && strings.TrimSpace(record.ResultSummary) == "" {
		return errors.New("aborted run requires a reason in resultSummary")
	}
	if record.Status == "invalidated" && !runIDRE.MatchString(record.InvalidatedBy) {
		return errors.New("invalidated run requires invalidatedBy run id")
	}
	if record.RetryOf != "" && (!runIDRE.MatchString(record.RetryOf) || record.RetryOf == record.ID) {
		return errors.New("retryOf must name a different run")
	}
	seenFiles := map[string]bool{}
	for _, file := range record.Files {
		if !validOne(file.MediaKind, "source-code", "structured-data", "text", "image", "binary", "archive", "external-reference") ||
			!validOne(file.SemanticRole, "input", "raw-observation", "reproducer", "intermediate", "candidate-deliverable", "reference-copy") ||
			!validOne(file.Retention, "retain-inline", "candidate-maintained", "retain-by-reference", "distill-then-review", "discard-candidate") {
			return fmt.Errorf("run file %q has invalid classification", file.Path)
		}
		if err := validateRelativeRecordPath(file.Path); err != nil || !strings.HasPrefix(file.Path, "payload/") {
			return fmt.Errorf("run file %q must be a canonical payload path", file.Path)
		}
		if seenFiles[file.Path] || !fileSHA256RE.MatchString(file.SHA256) {
			return fmt.Errorf("run file %q has duplicate path or invalid digest", file.Path)
		}
		seenFiles[file.Path] = true
		if err := validateIDList("run file supports", file.Supports, findingIDRE, ""); err != nil {
			return err
		}
		if file.Destination != "" {
			if err := validateRelativeRecordPath(file.Destination); err != nil {
				return fmt.Errorf("run file destination: %w", err)
			}
		}
	}
	if err := validateIDList("run findings", record.FindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("spawned work items", record.SpawnedWorkItemIDs, workItemIDRE, ""); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("changed project paths", record.ChangedProjectPaths); err != nil {
		return err
	}
	for _, changed := range record.ChangedProjectPaths {
		if err := validateRelativeRecordPath(changed); err != nil {
			return fmt.Errorf("changed project path: %w", err)
		}
	}
	return nil
}

func ValidateFinding(record FindingRecord) error {
	if err := validateRecordMeta(RecordMeta{
		SchemaVersion: record.SchemaVersion, ID: record.ID, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, Revision: record.Revision, CreatedBy: record.CreatedBy,
		UpdatedBy: record.UpdatedBy, Digest: record.Digest, CorrelationID: record.CorrelationID,
	}, findingIDRE); err != nil {
		return err
	}
	if !campaignIDRE.MatchString(record.CampaignID) {
		return errors.New("finding campaign id is invalid")
	}
	if !validOne(record.Kind, "observation", "conclusion", "method", "decision", "constraint", "dead-end", "open-question") ||
		strings.TrimSpace(record.Subject) == "" || strings.TrimSpace(record.Claim) == "" || len(record.Scope) == 0 {
		return errors.New("finding kind, subject, scope, and atomic claim are required")
	}
	if !validOne(record.EvidenceGrade, "direct", "inferred", "reported", "unknown") ||
		!validOne(record.ReviewState, "extracted", "curator-checked", "manager-ratified", "manager-rejected") ||
		!validOne(record.Validity, "provisional", "current", "challenged", "historical", "superseded", "invalid") {
		return errors.New("finding epistemic states are invalid")
	}
	if len(record.Evidence) == 0 {
		return errors.New("finding requires exact evidence")
	}
	if len(record.SourceRuns) == 0 {
		// A finding may omit source runs only when every evidence reference
		// is pinned to the project's git archive - the migrated-truth shape,
		// whose provenance is a revision-addressed blob rather than a run
		// payload. Every run-derived finding still requires its run.
		for _, evidence := range record.Evidence {
			if !migrationGitPinnedEvidence(evidence) {
				return errors.New("finding requires a source run for evidence that is not archive-pinned")
			}
		}
	}
	if err := validateIDList("finding source runs", record.SourceRuns, runIDRE, ""); err != nil {
		return err
	}
	for _, evidence := range record.Evidence {
		validRange := evidence.StartLine > 0 && evidence.EndLine >= evidence.StartLine
		if strings.TrimSpace(evidence.Path) == "" || !fileSHA256RE.MatchString(evidence.SHA256) ||
			(!validRange && strings.TrimSpace(evidence.ObjectKey) == "") {
			return errors.New("each evidence reference requires path, digest, and a range or object key")
		}
		if evidence.SourceRun != "" && !containsString(record.SourceRuns, evidence.SourceRun) {
			return errors.New("evidence sourceRun must appear in finding sourceRuns")
		}
	}
	for name, ids := range map[string][]string{
		"supports": record.Relations.Supports, "contradicts": record.Relations.Contradicts,
		"depends-on": record.Relations.DependsOn, "supersedes": record.Relations.Supersedes,
		"duplicates": record.Relations.Duplicates, "answers": record.Relations.Answers,
		"spawned": record.Relations.Spawned,
	} {
		pattern := findingIDRE
		if name == "spawned" {
			pattern = workItemIDRE
		}
		if err := validateIDList("finding "+name, ids, pattern, record.ID); err != nil {
			return err
		}
	}
	for _, values := range [][]string{record.AppliesWhen, record.KnownLimits, record.Tags, record.Subsystems, record.Aliases} {
		if err := requireUniqueNonEmpty("finding metadata list", values); err != nil {
			return err
		}
	}
	if !validOne(record.Projection, "none", "campaign", "truth", "history", "backlog", "playbook", "maintained", "archive", "rejected") {
		return fmt.Errorf("unsupported finding projection %q", record.Projection)
	}
	if record.Validity == "current" && (record.ReviewState != "manager-ratified" || record.EvidenceGrade != "direct") {
		return errors.New("current findings require manager ratification and direct evidence")
	}
	if record.Validity == "superseded" && len(record.Relations.Supersedes) != 0 {
		return errors.New("a superseded finding cannot claim that it supersedes another finding")
	}
	if record.VerifiedAt != "" {
		if _, err := time.Parse("2006-01-02", record.VerifiedAt); err != nil {
			if utcErr := validateUTC(record.VerifiedAt); utcErr != nil {
				return errors.New("verifiedAt must be an ISO date or UTC timestamp")
			}
		}
	}
	return nil
}

func ValidateIntake(record IntakeRecord) error {
	if err := validateRecordMeta(record.RecordMeta, intakeIDRE); err != nil {
		return err
	}
	if !campaignIDRE.MatchString(record.CampaignID) || len(record.SourceRuns) == 0 ||
		len(record.Coverage) == 0 || record.Triage == nil {
		return errors.New("intake requires campaign, source runs, coverage, and triage")
	}
	if !validOne(record.Status, "draft", "submitted", "reviewed", "superseded") {
		return fmt.Errorf("unsupported intake status %q", record.Status)
	}
	seenSources := map[string]bool{}
	for _, source := range record.SourceRuns {
		if err := validateFileHandle(source); err != nil {
			return fmt.Errorf("intake source: %w", err)
		}
		if seenSources[source.Path] {
			return fmt.Errorf("duplicate intake source %q", source.Path)
		}
		seenSources[source.Path] = true
	}
	if len(record.CandidateFindingIDs) > 10 {
		return errors.New("intake may contain at most 10 candidate findings")
	}
	if err := validateIDList("intake candidate findings", record.CandidateFindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("intake spawned work proposals", record.SpawnedWorkItems, workItemIDRE, ""); err != nil {
		return err
	}
	candidates := map[string]bool{}
	for _, findingID := range record.CandidateFindingIDs {
		candidates[findingID] = true
	}
	if len(record.Triage) != len(record.CandidateFindingIDs) {
		return errors.New("intake triage must classify every candidate exactly once")
	}
	for _, findingID := range record.CandidateFindingIDs {
		if !validOne(record.Triage[findingID], "routine", "attention") {
			return fmt.Errorf("intake candidate %s requires routine or attention triage", findingID)
		}
	}
	sourceHandles := map[string]FileHandle{}
	for _, source := range record.SourceRuns {
		sourceHandles[coverageSourceKey(source.Path, source.SHA256)] = source
	}
	seenCoverage := map[string]bool{}
	coverageBySource := map[string][]CoverageEntry{}
	candidateCoverage := map[string]bool{}
	for _, coverage := range record.Coverage {
		if strings.TrimSpace(coverage.SourceHandle) == "" || seenCoverage[coverage.SourceHandle] ||
			!validOne(coverage.Disposition, "candidate-finding", "duplicate", "non-claim", "unresolved", "out-of-scope") {
			return errors.New("coverage requires unique handles and a supported disposition")
		}
		if err := validateRelativeRecordPath(coverage.SourcePath); err != nil ||
			!fileSHA256RE.MatchString(coverage.SourceSHA256) || coverage.StartLine < 1 ||
			coverage.EndLine < coverage.StartLine || coverage.SourceLineCount < coverage.EndLine {
			return errors.New("coverage requires a canonical source path, digest, and valid inclusive line span")
		}
		key := coverageSourceKey(coverage.SourcePath, coverage.SourceSHA256)
		if _, ok := sourceHandles[key]; !ok {
			return fmt.Errorf("coverage source %s is not an exact intake source", coverage.SourcePath)
		}
		if coverage.SourceHandle != canonicalCoverageHandle(coverage) {
			return fmt.Errorf("coverage handle %q is not canonical for its declared source span", coverage.SourceHandle)
		}
		seenCoverage[coverage.SourceHandle] = true
		coverageBySource[key] = append(coverageBySource[key], coverage)
		if validOne(coverage.Disposition, "candidate-finding", "duplicate") && !findingIDRE.MatchString(coverage.TargetID) {
			return errors.New("finding coverage dispositions require a target finding")
		}
		if coverage.Disposition == "candidate-finding" && !candidates[coverage.TargetID] {
			return errors.New("candidate-finding coverage must target a finding in the intake")
		}
		if coverage.Disposition == "candidate-finding" {
			candidateCoverage[coverage.TargetID] = true
		}
		if !validOne(coverage.Disposition, "candidate-finding", "duplicate") && coverage.TargetID != "" {
			return errors.New("only finding coverage dispositions may name a target finding")
		}
		if validOne(coverage.Disposition, "unresolved", "out-of-scope") && strings.TrimSpace(coverage.Rationale) == "" {
			return errors.New("unresolved or out-of-scope coverage requires rationale")
		}
	}
	for findingID := range candidates {
		if !candidateCoverage[findingID] {
			return fmt.Errorf("intake candidate %s has no candidate-finding coverage span", findingID)
		}
	}
	for key, source := range sourceHandles {
		spans := append([]CoverageEntry(nil), coverageBySource[key]...)
		if len(spans) == 0 {
			return fmt.Errorf("intake source %s has no coverage spans", source.Path)
		}
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].StartLine != spans[j].StartLine {
				return spans[i].StartLine < spans[j].StartLine
			}
			return spans[i].EndLine < spans[j].EndLine
		})
		lineCount := spans[0].SourceLineCount
		nextLine := 1
		for _, span := range spans {
			if span.SourceLineCount != lineCount {
				return fmt.Errorf("coverage for %s disagrees on source line count", source.Path)
			}
			if span.StartLine != nextLine {
				return fmt.Errorf("coverage for %s has a gap or overlap at line %d", source.Path, nextLine)
			}
			nextLine = span.EndLine + 1
		}
		if nextLine != lineCount+1 {
			return fmt.Errorf("coverage for %s ends at line %d, expected %d", source.Path, nextLine-1, lineCount)
		}
	}
	return validateIntakeAmendmentLog(record)
}

// validateIntakeAmendmentLog is the shape half of the retirement rule, and it
// runs on every load of every intake, where no prior revision is available to
// compare against. It cannot prove that an amendment was applied faithfully -
// validateAppliedCoverageRetirement does that at the transaction, by
// reconstruction - but it can prove that the log the record carries is
// internally consistent with the coverage the record now declares.
//
// The clause that pins the last amendment's revision to the record's own is
// what turns intakeReviewedRevision from a hope into a theorem: without it, a
// forged record could pad the log with entries and drive the reviewed revision
// arbitrarily far backwards, which would make every review in the campaign
// appear to bind an intake it never saw.
//
// Every entry - not only the last - must still describe the row the record
// carries now. That follows from the permitted-change rule: only `unresolved`
// may move, and it may move only to a terminal judgment, so a span can leave
// `unresolved` exactly once and no later entry can move it again. Enforcing it
// makes the whole log verifiable from the record alone. If the permitted pairs
// in permittedCoverageRetirements are ever widened so that a span can move
// twice, this check must be revisited - which is the point of writing it down
// here rather than trusting the table to stay narrow silently.
func validateIntakeAmendmentLog(record IntakeRecord) error {
	if len(record.Amendments) == 0 {
		return nil
	}
	if record.Status != "reviewed" {
		return fmt.Errorf(
			"intake %s carries coverage retirements but is %s; only a reviewed intake may be amended",
			record.ID, record.Status)
	}
	if record.Revision <= int64(len(record.Amendments)) {
		return fmt.Errorf(
			"intake %s records %d coverage amendments at revision %d, which is more history than the record has",
			record.ID, len(record.Amendments), record.Revision)
	}
	rows := make(map[string]CoverageEntry, len(record.Coverage))
	for _, entry := range record.Coverage {
		rows[entry.SourceHandle] = entry
	}
	retired := map[string]string{}
	previousRevision := int64(0)
	for index, amendment := range record.Amendments {
		if err := ValidateCoverageAmendmentShape(amendment); err != nil {
			return fmt.Errorf("intake %s amendment %d: %w", record.ID, index+1, err)
		}
		if amendment.Revision <= previousRevision {
			return fmt.Errorf(
				"intake %s amendment revisions must strictly increase; %d does not follow %d",
				record.ID, amendment.Revision, previousRevision)
		}
		previousRevision = amendment.Revision
		if index == len(record.Amendments)-1 && amendment.Revision != record.Revision {
			return fmt.Errorf(
				"intake %s last coverage amendment is recorded at revision %d, not the record's own revision %d",
				record.ID, amendment.Revision, record.Revision)
		}
		for _, retirement := range amendment.Retirements {
			if earlier, repeated := retired[retirement.SourceHandle]; repeated {
				return fmt.Errorf(
					"intake %s retires span %s twice; it was already retired to %s",
					record.ID, retirement.SourceHandle, earlier)
			}
			retired[retirement.SourceHandle] = retirement.ToDisposition
			row, present := rows[retirement.SourceHandle]
			if !present {
				return fmt.Errorf(
					"intake %s retires span %s, which it does not cover",
					record.ID, retirement.SourceHandle)
			}
			if row.Disposition != retirement.ToDisposition || row.Rationale != retirement.ToRationale {
				return fmt.Errorf(
					"intake %s coverage span %s does not carry the disposition and rationale its amendment recorded",
					record.ID, retirement.SourceHandle)
			}
		}
	}
	return nil
}

func coverageSourceKey(sourcePath, sourceSHA256 string) string {
	return sourcePath + "\x00" + sourceSHA256
}

func canonicalCoverageHandle(entry CoverageEntry) string {
	return fmt.Sprintf("path:%s#L%d-L%d", entry.SourcePath, entry.StartLine, entry.EndLine)
}

func ValidateReview(record ReviewRecord) error {
	if err := validateRecordMeta(record.RecordMeta, reviewIDRE); err != nil {
		return err
	}
	if !campaignIDRE.MatchString(record.CampaignID) || strings.TrimSpace(record.Reviewer) == "" ||
		!validOne(record.Authority, "manager", "reviewer") || !intakeIDRE.MatchString(record.IntakeID) ||
		record.IntakeRevision < 1 || !digestRE.MatchString(record.PacketDigest) {
		return errors.New("review campaign, reviewer, authority, intake revision, and packet digest are required")
	}
	if err := ValidateReviewLoadReceipt(record.ReviewLoad); err != nil {
		return err
	}
	if record.ReviewLoad.ReviewID != record.ID || record.ReviewLoad.CampaignID != record.CampaignID ||
		record.ReviewLoad.PacketDigest != record.PacketDigest {
		return errors.New("review-load receipt does not bind its immutable review")
	}
	if len(record.Decisions) > 10 {
		return errors.New("review may contain at most 10 candidate decisions")
	}
	seen := map[string]bool{}
	for _, decision := range record.Decisions {
		if !findingIDRE.MatchString(decision.FindingID) || decision.FindingRevision < 1 || seen[decision.FindingID] ||
			!validOne(decision.Action, "ratify", "reject", "challenge", "merge", "split", "hold", "correct-grade", "supersede") ||
			strings.TrimSpace(decision.Rationale) == "" {
			return errors.New("review decisions require a unique finding revision, supported action, and rationale")
		}
		seen[decision.FindingID] = true
		if decision.EvidenceCorrection != "" &&
			!validOne(decision.EvidenceCorrection, "direct", "inferred", "reported", "unknown") {
			return errors.New("review evidence correction is invalid")
		}
		if decision.Projection != "" &&
			!validOne(decision.Projection, "none", "campaign", "truth", "history", "backlog", "playbook", "maintained", "archive", "rejected") {
			return errors.New("review projection is invalid")
		}
		if record.Authority != "manager" && validOne(decision.Action, "ratify", "reject", "merge", "split", "supersede") {
			return errors.New("reviewer authority cannot make a manager decision")
		}
	}
	if record.PriorReviewID != "" && (!reviewIDRE.MatchString(record.PriorReviewID) || record.PriorReviewID == record.ID) {
		return errors.New("priorReviewId must name a different review")
	}
	if err := validateIDList("resulting event ids", record.ResultingEventIDs, eventIDRE, ""); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("resulting record ids", record.ResultingRecordIDs); err != nil {
		return err
	}
	return nil
}

func ValidateEvent(event StateEvent) error {
	if event.SchemaVersion != CampaignSchemaVersion || !eventIDRE.MatchString(event.ID) ||
		strings.TrimSpace(event.Actor) == "" || strings.TrimSpace(event.Authority) == "" ||
		!actionIDRE.MatchString(event.Action) || !correlationIDRE.MatchString(event.CorrelationID) ||
		strings.TrimSpace(event.IdempotencyKey) == "" || len(event.IdempotencyKey) > 128 {
		return errors.New("event identity, actor, authority, action, idempotency key, or correlation is invalid")
	}
	if err := validateUTC(event.Timestamp); err != nil {
		return fmt.Errorf("event timestamp: %w", err)
	}
	if event.PreviousRevision < 0 || event.ResultingRevision != event.PreviousRevision+1 {
		return errors.New("event revisions must advance exactly once")
	}
	if event.PreviousRevision > 0 && !eventIDRE.MatchString(event.PreviousEventID) {
		return errors.New("non-initial events require a previous event id")
	}
	if err := requireUniqueNonEmpty("event affected ids", event.AffectedIDs); err != nil {
		return err
	}
	for name, digest := range map[string]string{
		"event": event.Digest, "previous state": event.PreviousStateDigest,
		"resulting state": event.ResultingStateDigest, "mutation": event.MutationDigest,
	} {
		if !digestRE.MatchString(digest) {
			return fmt.Errorf("%s digest is invalid", name)
		}
	}
	return nil
}

func ValidateContextCard(card ContextCard) error {
	if card.SchemaVersion != CampaignSchemaVersion || strings.TrimSpace(card.ID) == "" ||
		!validOne(card.CardType, "campaign", "work-item", "run", "finding", "decision", "blocker", "provenance", "raw-report", "health") ||
		!validOne(card.SourceClass, "truth", "campaign", "provisional", "history", "archive", "backlog", "profile", "memory", "intake", "state", "navigation", "playbook", "asset") ||
		strings.TrimSpace(card.Handle) == "" || card.ExpansionTokens < 0 {
		return errors.New("context card identity, class, handle, or expansion estimate is invalid")
	}
	if card.EvidenceGrade != "" && !validOne(card.EvidenceGrade, "direct", "inferred", "reported", "unknown") {
		return errors.New("context card evidence grade is invalid")
	}
	if card.ReviewState != "" && !validOne(card.ReviewState, "extracted", "curator-checked", "manager-ratified", "manager-rejected") {
		return errors.New("context card review state is invalid")
	}
	if card.Validity != "" && !validOne(card.Validity, "provisional", "current", "challenged", "historical", "superseded", "invalid") {
		return errors.New("context card validity is invalid")
	}
	for _, alert := range card.RelationAlerts {
		if !validOne(alert, "conflict", "challenged", "supersession", "stale") && !cardRelationRE.MatchString(alert) {
			return fmt.Errorf("context card relation alert %q is invalid", alert)
		}
	}
	if err := requireUniqueNonEmpty("context card relation alerts", card.RelationAlerts); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("context card match reasons", card.WhyMatched); err != nil {
		return err
	}
	for key := range card.Metadata {
		if strings.TrimSpace(key) == "" {
			return errors.New("context card metadata contains an empty key")
		}
	}
	return nil
}

func requireUniqueNonEmpty(name string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return fmt.Errorf("%s contains an empty or duplicate value", name)
		}
		seen[value] = true
	}
	return nil
}

func validateIDList(name string, values []string, pattern *regexp.Regexp, self string) error {
	if err := requireUniqueNonEmpty(name, values); err != nil {
		return err
	}
	for _, id := range values {
		if !pattern.MatchString(id) || (self != "" && id == self) {
			return fmt.Errorf("%s contains invalid id %q", name, id)
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	seen := map[string]bool{}
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if seen[value] {
			return true
		}
	}
	return false
}

func validateRelativeRecordPath(value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') || strings.Contains(value, "\\") ||
		strings.Contains(value, ":") || path.IsAbs(value) || path.Clean(value) != value ||
		value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("%q is not a canonical project-relative path", value)
	}
	return nil
}

func validateFileHandle(handle FileHandle) error {
	if err := validateRelativeRecordPath(handle.Path); err != nil {
		return err
	}
	if !fileSHA256RE.MatchString(handle.SHA256) {
		return errors.New("file handle digest is invalid")
	}
	return nil
}

func normalizeRecordLists(record *CampaignRecord) {
	record.Scope = SortedUnique(record.Scope)
	record.Exclusions = SortedUnique(record.Exclusions)
	record.SuccessCriteria = SortedUnique(record.SuccessCriteria)
	record.ClosureCriteria = SortedUnique(record.ClosureCriteria)
	record.CurrentFocus = SortedUnique(record.CurrentFocus)
	record.Milestones = SortedUnique(record.Milestones)
	record.PermittedManagers = SortedUnique(record.PermittedManagers)
}

func relationAlerts(record FindingRecord) []string {
	alerts := []string{}
	if record.Validity == "challenged" {
		alerts = append(alerts, "challenged")
	}
	if record.Validity == "superseded" || len(record.Relations.Supersedes) > 0 {
		alerts = append(alerts, "supersession")
	}
	if len(record.Relations.Contradicts) > 0 {
		alerts = append(alerts, "conflict")
	}
	sort.Strings(alerts)
	return alerts
}
