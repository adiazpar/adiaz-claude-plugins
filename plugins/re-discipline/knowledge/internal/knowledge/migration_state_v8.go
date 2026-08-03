package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type MigrationOperationReceipt struct {
	SchemaVersion int      `json:"schemaVersion"`
	OperationID   string   `json:"operationId"`
	Stage         string   `json:"stage"`
	InputDigest   string   `json:"inputDigest"`
	OutputDigest  string   `json:"outputDigest"`
	Actor         string   `json:"actor"`
	Adapter       string   `json:"adapter"`
	Timestamp     string   `json:"timestamp"`
	Validation    string   `json:"validation"`
	RecoveryPaths []string `json:"recoveryPaths,omitempty"`
	Digest        string   `json:"digest"`
}

type MigrationCoverageReceipt struct {
	SchemaVersion int                     `json:"schemaVersion"`
	SourcePath    string                  `json:"sourcePath"`
	SourceDigest  string                  `json:"sourceDigest"`
	Complete      bool                    `json:"complete"`
	Coverage      []CoverageEntry         `json:"coverage"`
	FindingIDs    []string                `json:"findingIds,omitempty"`
	Findings      []MigrationFindingInput `json:"findings,omitempty"`
	Reviewer      string                  `json:"reviewer"`
	Rationale     string                  `json:"rationale"`
	Digest        string                  `json:"digest"`
}

// MigrationFindingInput is the curator-authored semantic payload for one
// candidate-finding coverage row. The migrator supplies provenance, run
// identity, timestamps, and provisional epistemic state; it never invents or
// ratifies the claim.
type MigrationFindingInput struct {
	ID                 string                             `json:"id"`
	Kind               string                             `json:"kind"`
	Subject            string                             `json:"subject"`
	Claim              string                             `json:"claim"`
	Scope              map[string]any                     `json:"scope"`
	AppliesWhen        []string                           `json:"appliesWhen,omitempty"`
	KnownLimits        []string                           `json:"knownLimits,omitempty"`
	Tags               []string                           `json:"tags,omitempty"`
	Subsystems         []string                           `json:"subsystems,omitempty"`
	Aliases            []string                           `json:"aliases,omitempty"`
	EvidenceGrade      string                             `json:"evidenceGrade"`
	SyntheticQuestions []string                           `json:"syntheticQuestions"`
	CuratorAttestation MigrationFindingCuratorAttestation `json:"curatorAttestation"`
	Body               string                             `json:"body,omitempty"`
}

// MigrationFindingCuratorAttestation makes the semantic and source-span
// boundaries explicit.
// The engine can prove that a curator made these statements and bind them to
// the immutable coverage receipt; it cannot infer atomicity or evidence
// homogeneity from prose. Every imported candidate therefore remains an
// attention item for an independent manager even when all statements are
// attested.
type MigrationFindingCuratorAttestation struct {
	SingleIndependentlyOverturnableClaim bool   `json:"singleIndependentlyOverturnableClaim"`
	EvidenceGradeAppliesToEntireClaim    bool   `json:"evidenceGradeAppliesToEntireClaim"`
	EntireSourceSpanRepresented          bool   `json:"entireSourceSpanRepresented"`
	SemanticBoundariesVerified           bool   `json:"semanticBoundariesVerified"`
	LegacyReviewLanguageProvenanceOnly   bool   `json:"legacyReviewLanguageProvenanceOnly"`
	ManagerAttentionRequired             bool   `json:"managerAttentionRequired"`
	Rationale                            string `json:"rationale"`
}

type MigrationGateReceipt struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Gate           string `json:"gate"`
	Passed         bool   `json:"passed"`
	Artifact       string `json:"artifact"`
	ArtifactDigest string `json:"artifactDigest"`
	Reviewer       string `json:"reviewer"`
	Timestamp      string `json:"timestamp"`
	Digest         string `json:"digest"`
}

type MigrationGateCheck struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

// MigrationGateArtifact is the measured result bound by a gate receipt. A
// caller cannot turn arbitrary bytes into a passing gate merely by supplying
// their digest: the artifact must bind this transaction and plan, enumerate
// checks, and carry its own canonical result digest.
type MigrationGateArtifact struct {
	SchemaVersion int                               `json:"schemaVersion"`
	TransactionID string                            `json:"transactionId"`
	PlanDigest    string                            `json:"planDigest"`
	Gate          string                            `json:"gate"`
	Passed        bool                              `json:"passed"`
	Checks        []MigrationGateCheck              `json:"checks"`
	Fingerprints  map[string]string                 `json:"fingerprints"`
	Retrieval     *MigrationRetrievalGateEvidence   `json:"retrieval,omitempty"`
	HostParity    *MigrationHostConformanceEvidence `json:"hostParity,omitempty"`
	ResultDigest  string                            `json:"resultDigest"`
}

type MigrationPhysicalEquivalenceSummary struct {
	PlannedDestinations int    `json:"plannedDestinations"`
	ByteExact           int    `json:"byteExact"`
	Transformed         int    `json:"transformed"`
	RetainedInPlace     int    `json:"retainedInPlace"`
	ReceiptDigest       string `json:"receiptDigest"`
}

type MigrationCertification struct {
	SchemaVersion                     int                                 `json:"schemaVersion"`
	TransactionID                     string                              `json:"transactionId"`
	PlanDigest                        string                              `json:"planDigest"`
	State                             string                              `json:"state"`
	Structural                        bool                                `json:"structural"`
	SemanticTraversal                 bool                                `json:"semanticTraversal"`
	RetrievalContext                  bool                                `json:"retrievalContext"`
	HostParity                        bool                                `json:"hostParity"`
	RequiredGates                     []string                            `json:"requiredGates"`
	GateReceipts                      []MigrationGateReceipt              `json:"gateReceipts"`
	SourceStateHead                   string                              `json:"sourceStateHead"`
	DestinationStateHead              string                              `json:"destinationStateHead"`
	PluginVersion                     string                              `json:"pluginVersion"`
	SchemaVersions                    map[string]int                      `json:"schemaVersions"`
	InventoryDigest                   string                              `json:"inventoryDigest"`
	CoverageDigest                    string                              `json:"coverageDigest"`
	PhysicalEquivalence               MigrationPhysicalEquivalenceSummary `json:"physicalEquivalence"`
	BenchmarkFingerprint              string                              `json:"benchmarkFingerprint"`
	CalibrationFingerprint            string                              `json:"calibrationFingerprint"`
	BlindedAgentEvaluationFingerprint string                              `json:"blindedAgentEvaluationFingerprint"`
	MCPConformanceFingerprint         string                              `json:"mcpConformanceFingerprint"`
	CLIConformanceFingerprint         string                              `json:"cliConformanceFingerprint"`
	ClaudeHostFingerprint             string                              `json:"claudeHostFingerprint"`
	CodexHostFingerprint              string                              `json:"codexHostFingerprint"`
	KnownLimitations                  []string                            `json:"knownLimitations"`
	FinalManagerDecision              string                              `json:"finalManagerDecision"`
	Blockers                          []string                            `json:"blockers"`
	Candidate                         bool                                `json:"candidate"`
	Digest                            string                              `json:"digest"`
}

// MigrationRatificationManifest is the immutable artifact published by the
// normal state transaction that makes a converted project authoritative. It
// binds the exact manager-reviewed certification to the complete activated
// snapshot and the globally chained import head.
type MigrationRatificationManifest struct {
	SchemaVersion            int                     `json:"schemaVersion"`
	TransactionID            string                  `json:"transactionId"`
	PlanDigest               string                  `json:"planDigest"`
	Certification            MigrationCertification  `json:"certification"`
	CertificationDigest      string                  `json:"certificationDigest"`
	SnapshotDigest           string                  `json:"snapshotDigest"`
	SnapshotObjects          int                     `json:"snapshotObjects"`
	NormalizedManifestDigest string                  `json:"normalizedManifestDigest"`
	ActivationJournalDigest  string                  `json:"activationJournalDigest"`
	ManagedTargets           []migrationImportObject `json:"managedTargets"`
	ImportHead               StateHead               `json:"importHead"`
	FinalManagerDecision     string                  `json:"finalManagerDecision"`
	DecisionActor            string                  `json:"decisionActor"`
	Digest                   string                  `json:"digest"`
}

type MigrationState struct {
	SchemaVersion       int                         `json:"schemaVersion"`
	TransactionID       string                      `json:"transactionId"`
	PlanID              string                      `json:"planId"`
	PlanDigest          string                      `json:"planDigest"`
	State               string                      `json:"state"`
	CertificationScope  string                      `json:"certificationScope"`
	SourceFingerprint   string                      `json:"sourceFingerprint"`
	LiveCampaigns       []string                    `json:"liveCampaigns"`
	Actor               string                      `json:"actor"`
	Adapter             string                      `json:"adapter"`
	CreatedAt           string                      `json:"createdAt"`
	UpdatedAt           string                      `json:"updatedAt"`
	LastOperationID     string                      `json:"lastOperationId,omitempty"`
	Completed           []MigrationOperationReceipt `json:"completedOperations"`
	Blockers            []string                    `json:"blockers"`
	SafeNextAction      string                      `json:"safeNextAction"`
	CertificationDigest string                      `json:"certificationDigest,omitempty"`
	Digest              string                      `json:"digest"`
}

type MigrationShadowEntry struct {
	Handle     string `json:"handle"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Campaign   string `json:"campaign"`
	Normalized bool   `json:"normalized"`
	Label      string `json:"label"`
}

type MigrationShadowCatalog struct {
	SchemaVersion int                    `json:"schemaVersion"`
	PlanDigest    string                 `json:"planDigest"`
	Reports       []MigrationShadowEntry `json:"reports"`
	Digest        string                 `json:"digest"`
}

type MigrationShadowMatch struct {
	MigrationShadowEntry
	Excerpt string `json:"excerpt"`
}

type MigrationShadowQueryResult struct {
	SchemaVersion int                    `json:"schemaVersion"`
	PlanDigest    string                 `json:"planDigest"`
	Query         string                 `json:"query"`
	Campaign      string                 `json:"campaign,omitempty"`
	Matches       []MigrationShadowMatch `json:"matches"`
	Omitted       int                    `json:"omitted"`
	CatalogDigest string                 `json:"catalogDigest"`
}

type MigrationEngine struct {
	ProjectRoot           string
	AssetRoot             string
	Now                   func() time.Time
	StateFailpoint        func(StateFailpoint) error
	ActivationPrepareHook func()
	// ActivationFailpoint is a deterministic crash-recovery seam used by the
	// disposable release pilot. Production adapters never set it.
	ActivationFailpoint func(MigrationActivationFailpoint) error
}

// MigrationActivationFailpoint identifies a durable activation boundary. It
// is emitted only after the activation journal records the named target phase,
// so a returned error exercises the same forward recovery as process loss.
type MigrationActivationFailpoint struct {
	Phase       string `json:"phase"`
	TargetIndex int    `json:"targetIndex"`
	TargetPath  string `json:"targetPath"`
}

func NewMigrationEngine(projectRoot string) (*MigrationEngine, error) {
	return NewMigrationEngineWithAssetRoot(projectRoot, "")
}

func NewMigrationEngineWithAssetRoot(projectRoot, assetRoot string) (*MigrationEngine, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return nil, err
	}
	resolvedAssets := ""
	if strings.TrimSpace(assetRoot) != "" {
		resolvedAssets, err = filepath.Abs(assetRoot)
		if err != nil {
			return nil, err
		}
	}
	return &MigrationEngine{
		ProjectRoot: boundary.Root, AssetRoot: resolvedAssets, Now: time.Now,
	}, nil
}

func (engine *MigrationEngine) migrationRoot() string {
	return filepath.Join(engine.ProjectRoot, ".re-discipline", "migration", "0.8")
}

func (engine *MigrationEngine) statePath() string {
	return filepath.Join(engine.migrationRoot(), "state.json")
}

func (engine *MigrationEngine) lockPath() string {
	return filepath.Join(engine.migrationRoot(), "writer.lock")
}

func (engine *MigrationEngine) operationLockPath() string {
	return filepath.Join(engine.migrationRoot(), "operation.lock")
}

// ensureMigrationDirectory creates a transaction directory one component at
// a time and rejects every existing link/non-directory component. Migration
// artifacts carry approval authority, so following a project-local junction
// outside the project would be a safety boundary violation.
func (engine *MigrationEngine) ensureMigrationDirectory(target string) error {
	root := filepath.Clean(engine.ProjectRoot)
	target = filepath.Clean(target)
	if !withinRoot(root, target) {
		return errors.New("migration directory escapes the project")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("migration directory component %s is unsafe", current)
		}
	}
	return nil
}

func (engine *MigrationEngine) validateMigrationDirectory(target string) error {
	return validateRealDirectoryChain(engine.ProjectRoot, target)
}

func (engine *MigrationEngine) Start(
	plan MigrationPlan, approvedDigest, actor, adapter string,
) (MigrationState, error) {
	declaredPlanDigest := plan.PlanDigest
	plan.PlanDigest = ""
	computedPlanDigest, digestErr := CanonicalDigest(plan)
	plan.PlanDigest = declaredPlanDigest
	if digestErr != nil || declaredPlanDigest == "" || computedPlanDigest != declaredPlanDigest {
		return MigrationState{}, errors.New("migration plan digest does not authenticate the supplied plan")
	}
	version, err := DetectProjectStateVersion(engine.ProjectRoot)
	if err != nil {
		return MigrationState{}, err
	}
	if version != "0.7" || plan.DetectedVersion != "0.7" {
		return MigrationState{}, fmt.Errorf("0.7-to-0.8 migration apply requires a legacy 0.7 project, got %s", version)
	}
	if approvedDigest == "" || approvedDigest != plan.PlanDigest {
		return MigrationState{}, errors.New("migration apply requires the exact preview plan digest")
	}
	if strings.TrimSpace(actor) == "" || !validOne(adapter, "cli", "mcp") {
		return MigrationState{}, errors.New("migration actor and adapter are required")
	}
	if countBlockingConflicts(plan.Conflicts) > 0 {
		return MigrationState{}, errors.New("migration plan has blocking conflicts")
	}
	fresh, err := PreviewMigration(engine.ProjectRoot, plan.LiveCampaigns)
	if err != nil {
		return MigrationState{}, err
	}
	if fresh.Plan.PlanDigest != approvedDigest || fresh.Plan.SourceFingerprint != plan.SourceFingerprint {
		return MigrationState{}, errors.New("migration inputs changed after preview; create and approve a new plan")
	}
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationState{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer operationLock.Close()
	// Serialize approval with pre-transaction truth reviews and profile
	// decisions, then rederive the plan after acquiring that boundary.
	// Otherwise a decision can land between the earlier preview and state
	// creation.
	fresh, err = PreviewMigration(engine.ProjectRoot, plan.LiveCampaigns)
	if err != nil {
		return MigrationState{}, err
	}
	if fresh.Plan.PlanDigest != approvedDigest || fresh.Plan.SourceFingerprint != plan.SourceFingerprint {
		return MigrationState{}, errors.New("migration inputs changed after preview; create and approve a new plan")
	}
	lock, err := acquireWriterLock(engine.lockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer lock.Close()
	if existing, readErr := engine.Status(); readErr == nil {
		if existing.PlanDigest == approvedDigest {
			return existing, nil
		}
		return MigrationState{}, errors.New("a different 0.8 migration transaction already exists")
	} else if !os.IsNotExist(readErr) {
		return MigrationState{}, readErr
	}
	now := RFC3339UTC(engine.Now().UTC())
	transactionID := "M-" + strings.ToUpper(strings.TrimPrefix(approvedDigest, "sha256:")[:20])
	state := MigrationState{
		SchemaVersion: MigrationSchemaVersion, TransactionID: transactionID,
		PlanID: plan.PlanID, PlanDigest: approvedDigest, State: "inventoried",
		CertificationScope: certificationScope(plan.LiveCampaigns),
		SourceFingerprint:  plan.SourceFingerprint, LiveCampaigns: plan.LiveCampaigns,
		Actor: actor, Adapter: adapter, CreatedAt: now, UpdatedAt: now,
		Completed: []MigrationOperationReceipt{}, Blockers: append([]string{}, plan.Unresolved...),
		SafeNextAction: "resume to build the read-only shadow catalog",
	}
	planPath := filepath.Join(engine.migrationRoot(), "plan.json")
	if err := AtomicWriteJSON(planPath, plan, 0o600); err != nil {
		return MigrationState{}, err
	}
	receipt, err := engine.receipt("inventory", plan.SourceFingerprint, approvedDigest, actor, adapter, nil)
	if err != nil {
		return MigrationState{}, err
	}
	state.Completed = append(state.Completed, receipt)
	state.LastOperationID = receipt.OperationID
	if err := engine.writeState(&state); err != nil {
		return MigrationState{}, err
	}
	return state, nil
}

func certificationScope(live []string) string {
	if len(live) == 0 {
		return "shadow-only"
	}
	return "live-certified"
}

func (engine *MigrationEngine) Status() (MigrationState, error) {
	var state MigrationState
	if err := engine.validateMigrationDirectory(engine.migrationRoot()); err != nil {
		return state, err
	}
	body, err := readSingleLinkRegularFile(engine.statePath())
	if err != nil {
		return state, err
	}
	if err := decodeStrict(body, &state); err != nil {
		return state, err
	}
	if state.SchemaVersion != MigrationSchemaVersion || !validMigrationState(state.State) {
		return state, errors.New("migration state is invalid")
	}
	expected := state.Digest
	state.Digest = ""
	digest, err := CanonicalDigest(state)
	if err != nil || digest != expected {
		return MigrationState{}, errors.New("migration state digest mismatch")
	}
	state.Digest = expected
	return state, nil
}

func validMigrationState(value string) bool {
	for _, state := range migrationStates {
		if state == value {
			return true
		}
	}
	return false
}

func (engine *MigrationEngine) loadPlan() (MigrationPlan, error) {
	var plan MigrationPlan
	body, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "plan.json"))
	if err != nil {
		return plan, err
	}
	if err := decodeStrict(body, &plan); err != nil {
		return plan, err
	}
	expected := plan.PlanDigest
	plan.PlanDigest = ""
	digest, err := CanonicalDigest(plan)
	plan.PlanDigest = expected
	if err != nil || digest != expected {
		return MigrationPlan{}, errors.New("stored migration plan digest mismatch")
	}
	return plan, nil
}

func validateMigrationPlanStateBinding(plan MigrationPlan, state MigrationState) error {
	if plan.PlanID != state.PlanID || plan.PlanDigest != state.PlanDigest ||
		plan.SourceFingerprint != state.SourceFingerprint ||
		!reflect.DeepEqual(plan.LiveCampaigns, state.LiveCampaigns) {
		return errors.New("stored migration plan does not match the active transaction state")
	}
	return nil
}

func (engine *MigrationEngine) Resume(transactionID, actor, adapter string) (MigrationState, error) {
	if strings.TrimSpace(actor) == "" || !validOne(adapter, "cli", "mcp") {
		return MigrationState{}, errors.New("migration actor and adapter are required")
	}
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationState{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer operationLock.Close()
	state, err := engine.Status()
	if err != nil {
		return MigrationState{}, err
	}
	if transactionID != state.TransactionID {
		return MigrationState{}, errors.New("migration transaction id does not match active state")
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationState{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return MigrationState{}, err
	}
	activationStarted := false
	if state.State == "normalized" {
		_, activationErr := os.Lstat(filepath.Join(engine.migrationRoot(), "activation.json"))
		activationStarted = activationErr == nil
		if activationErr != nil && !os.IsNotExist(activationErr) {
			return MigrationState{}, activationErr
		}
	}
	if state.State == "inventoried" || state.State == "shadow-indexed" ||
		(state.State == "normalized" && !activationStarted) {
		fresh, previewErr := PreviewMigration(engine.ProjectRoot, plan.LiveCampaigns)
		if previewErr != nil {
			return MigrationState{}, previewErr
		}
		// The exact approved plan is re-derived immediately before every stage
		// that still consumes legacy bytes. In particular, normalized staging
		// is not authority to overwrite a 0.7 writer's later change during the
		// physical rename transaction.
		if fresh.Plan.SourceFingerprint != state.SourceFingerprint ||
			fresh.Plan.PlanDigest != plan.PlanDigest {
			return MigrationState{}, errors.New("legacy migration source snapshot or destination plan changed; preview approval is stale")
		}
	}
	switch state.State {
	case "inventoried":
		return engine.advanceShadow(state, plan, actor, adapter)
	case "shadow-indexed":
		return engine.advanceNormalized(state, plan, actor, adapter)
	case "normalized":
		return engine.advancePhysical(state, plan, actor, adapter)
	case "physically-reorganized":
		certification, verifyErr := engine.Verify()
		if verifyErr != nil {
			return MigrationState{}, verifyErr
		}
		if !certification.Candidate {
			state.Blockers = certification.Blockers
			state.SafeNextAction = "satisfy every certification gate, then resume"
			if err := engine.writeStateLocked(&state); err != nil {
				return MigrationState{}, err
			}
			return state, nil
		}
		return engine.advanceVerified(state, certification, actor, adapter)
	case "traversal-verified":
		state.SafeNextAction = "manager must ratify the exact certification digest"
		return state, nil
	case "migrated":
		return state, nil
	default:
		return MigrationState{}, fmt.Errorf("migration cannot resume from state %q", state.State)
	}
}

func (engine *MigrationEngine) advanceShadow(
	state MigrationState, plan MigrationPlan, actor, adapter string,
) (MigrationState, error) {
	entries := []MigrationShadowEntry{}
	for _, source := range plan.Sources {
		if source.Role == "legacy-run-report" {
			entries = append(entries, MigrationShadowEntry{
				Handle: "legacy-report:" + source.SHA256,
				Path:   source.Path, SHA256: source.SHA256, Campaign: source.Campaign,
				Normalized: false, Label: "unnormalized provenance",
			})
		}
	}
	artifact := MigrationShadowCatalog{
		SchemaVersion: MigrationSchemaVersion, PlanDigest: plan.PlanDigest, Reports: entries,
	}
	outputDigest, err := CanonicalDigest(artifact)
	if err != nil {
		return MigrationState{}, err
	}
	artifact.Digest = outputDigest
	if err := AtomicWriteJSON(filepath.Join(engine.migrationRoot(), "shadow-catalog.json"), artifact, 0o600); err != nil {
		return MigrationState{}, err
	}
	return engine.commitStage(&state, "shadow-indexed", plan.SourceFingerprint, outputDigest,
		actor, adapter, nil, "submit complete curator coverage for designated live reports, then resume")
}

// QueryShadow exposes legacy reports only while they are the digest-pinned
// migration fallback. Every returned byte is re-read from the approved source
// path and checked against the catalog before a bounded excerpt is returned.
func (engine *MigrationEngine) QueryShadow(query, campaign string, limit int) (MigrationShadowQueryResult, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationShadowQueryResult{}, err
	}
	if state.State != "shadow-indexed" && state.State != "normalized" {
		return MigrationShadowQueryResult{}, errors.New("shadow provenance is queryable only after shadow indexing and before physical activation")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		return MigrationShadowQueryResult{}, errors.New("shadow query limit cannot exceed 20")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return MigrationShadowQueryResult{}, errors.New("shadow query is required")
	}
	var catalog MigrationShadowCatalog
	body, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "shadow-catalog.json"))
	if err != nil {
		return MigrationShadowQueryResult{}, err
	}
	if err := decodeStrict(body, &catalog); err != nil {
		return MigrationShadowQueryResult{}, err
	}
	expected := catalog.Digest
	catalog.Digest = ""
	digest, err := CanonicalDigest(catalog)
	catalog.Digest = expected
	if err != nil || expected != digest || catalog.PlanDigest != state.PlanDigest {
		return MigrationShadowQueryResult{}, errors.New("shadow catalog digest or plan identity mismatch")
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationShadowQueryResult{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return MigrationShadowQueryResult{}, err
	}
	sources := map[string]MigrationSource{}
	for _, source := range plan.Sources {
		sources[source.Path] = source
	}
	lowerQuery := strings.ToLower(query)
	result := MigrationShadowQueryResult{
		SchemaVersion: MigrationSchemaVersion, PlanDigest: state.PlanDigest,
		Query: query, Campaign: campaign, Matches: []MigrationShadowMatch{}, CatalogDigest: expected,
	}
	for _, entry := range catalog.Reports {
		if campaign != "" && entry.Campaign != campaign {
			continue
		}
		source, ok := sources[entry.Path]
		if !ok || source.SHA256 != entry.SHA256 {
			return MigrationShadowQueryResult{}, fmt.Errorf("shadow source %s is not bound to the approved plan", entry.Path)
		}
		report, readErr := readMigrationSource(engine.ProjectRoot, source)
		if readErr != nil {
			return MigrationShadowQueryResult{}, readErr
		}
		reportText := string(report)
		index := strings.Index(strings.ToLower(reportText), lowerQuery)
		metadataMatch := strings.Contains(strings.ToLower(entry.Path+"\n"+entry.Campaign), lowerQuery)
		if index < 0 && !metadataMatch {
			continue
		}
		if index < 0 {
			index = 0
		}
		start := index - 240
		if start < 0 {
			start = 0
		}
		end := index + len(lowerQuery) + 560
		if end > len(reportText) {
			end = len(reportText)
		}
		if metadataMatch && index == 0 {
			start = 0
			end = len(reportText)
			if end > 800 {
				end = 800
			}
		}
		match := MigrationShadowMatch{MigrationShadowEntry: entry,
			Excerpt: strings.TrimSpace(reportText[start:end])}
		if len(result.Matches) < limit {
			result.Matches = append(result.Matches, match)
		} else {
			result.Omitted++
		}
	}
	return result, nil
}

func (engine *MigrationEngine) SubmitCoverage(receipt MigrationCoverageReceipt) (MigrationCoverageReceipt, error) {
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	defer operationLock.Close()
	state, err := engine.Status()
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	if state.State != "shadow-indexed" && state.State != "normalized" {
		return MigrationCoverageReceipt{}, errors.New("coverage is accepted only after shadow indexing and before physical reorganization")
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	var source *MigrationSource
	for index := range plan.Sources {
		if plan.Sources[index].Path == receipt.SourcePath {
			source = &plan.Sources[index]
			break
		}
	}
	if source == nil || source.Role != "legacy-run-report" || source.SHA256 != receipt.SourceDigest {
		return MigrationCoverageReceipt{}, errors.New("coverage source is absent or its digest is stale")
	}
	if !receipt.Complete {
		return MigrationCoverageReceipt{}, errors.New("only finalized complete coverage receipts can be submitted; keep drafts outside canonical migration state")
	}
	if strings.TrimSpace(receipt.Reviewer) == "" || strings.TrimSpace(receipt.Rationale) == "" || len(receipt.Coverage) == 0 {
		return MigrationCoverageReceipt{}, errors.New("coverage requires a reviewer, rationale, and at least one classified span")
	}
	reportBody, err := engine.migrationCoverageReportBody(*source)
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	lineCount, err := migrationReportLineCount(reportBody)
	if err != nil {
		return MigrationCoverageReceipt{}, fmt.Errorf("coverage source %s: %w", source.Path, err)
	}
	expectedCoveragePath := source.Destination
	expectedCoverageDigest := "sha256:" + source.SHA256
	receipt.Reviewer = strings.TrimSpace(receipt.Reviewer)
	receipt.Rationale = strings.TrimSpace(receipt.Rationale)
	seenHandles := map[string]bool{}
	for index := range receipt.Coverage {
		entry := &receipt.Coverage[index]
		entry.SourceHandle = strings.TrimSpace(entry.SourceHandle)
		entry.SourcePath = strings.TrimSpace(entry.SourcePath)
		entry.SourceSHA256 = strings.TrimSpace(entry.SourceSHA256)
		entry.Disposition = strings.TrimSpace(entry.Disposition)
		entry.TargetID = strings.TrimSpace(entry.TargetID)
		entry.Rationale = strings.TrimSpace(entry.Rationale)
		if entry.SourceHandle == "" ||
			!validOne(entry.Disposition, "candidate-finding", "duplicate", "non-claim", "unresolved", "out-of-scope") {
			return MigrationCoverageReceipt{}, errors.New("coverage contains an invalid source handle or disposition")
		}
		if entry.SourcePath != expectedCoveragePath || entry.SourceSHA256 != expectedCoverageDigest ||
			entry.StartLine < 1 || entry.EndLine < entry.StartLine || entry.SourceLineCount != lineCount ||
			entry.EndLine > lineCount || entry.SourceHandle != canonicalCoverageHandle(*entry) {
			return MigrationCoverageReceipt{}, errors.New("coverage must use the exact canonical report path, digest, line count, and inclusive span handle")
		}
		if seenHandles[entry.SourceHandle] {
			return MigrationCoverageReceipt{}, errors.New("coverage classifies the same source handle more than once")
		}
		seenHandles[entry.SourceHandle] = true
		if validOne(entry.Disposition, "candidate-finding", "unresolved", "out-of-scope") && entry.Rationale == "" {
			return MigrationCoverageReceipt{}, errors.New("candidate-finding, unresolved, and out-of-scope coverage require an exact span rationale")
		}
		if !validOne(entry.Disposition, "candidate-finding", "duplicate") && entry.TargetID != "" {
			return MigrationCoverageReceipt{}, errors.New("non-finding coverage dispositions cannot name a target finding")
		}
	}
	orderedCoverage := append([]CoverageEntry(nil), receipt.Coverage...)
	sort.Slice(orderedCoverage, func(i, j int) bool {
		if orderedCoverage[i].StartLine != orderedCoverage[j].StartLine {
			return orderedCoverage[i].StartLine < orderedCoverage[j].StartLine
		}
		return orderedCoverage[i].EndLine < orderedCoverage[j].EndLine
	})
	nextLine := 1
	for _, entry := range orderedCoverage {
		if entry.StartLine != nextLine {
			return MigrationCoverageReceipt{}, fmt.Errorf("coverage has a gap or overlap at source line %d", nextLine)
		}
		nextLine = entry.EndLine + 1
	}
	if nextLine != lineCount+1 {
		return MigrationCoverageReceipt{}, fmt.Errorf("coverage ends at source line %d, expected %d", nextLine-1, lineCount)
	}
	candidateIDs := []string{}
	for _, entry := range receipt.Coverage {
		if entry.Disposition == "candidate-finding" {
			if !findingIDRE.MatchString(entry.TargetID) {
				return MigrationCoverageReceipt{}, errors.New("candidate-finding coverage requires a target finding id")
			}
			candidateIDs = append(candidateIDs, entry.TargetID)
		}
		if entry.Disposition == "duplicate" && !findingIDRE.MatchString(entry.TargetID) {
			return MigrationCoverageReceipt{}, errors.New("duplicate coverage requires a target finding id")
		}
	}
	inputIDs := []string{}
	seenInputIDs := map[string]bool{}
	for _, finding := range receipt.Findings {
		if err := validateMigrationFindingInput(finding); err != nil {
			return MigrationCoverageReceipt{}, err
		}
		if seenInputIDs[finding.ID] {
			return MigrationCoverageReceipt{}, fmt.Errorf("migration finding payload %s is duplicated", finding.ID)
		}
		seenInputIDs[finding.ID] = true
		inputIDs = append(inputIDs, finding.ID)
	}
	candidateIDs = SortedUnique(candidateIDs)
	inputIDs = SortedUnique(inputIDs)
	if strings.Join(candidateIDs, "\x00") != strings.Join(inputIDs, "\x00") {
		return MigrationCoverageReceipt{}, errors.New("every candidate-finding coverage target requires exactly one curator-authored finding payload")
	}
	receipt.SchemaVersion = MigrationSchemaVersion
	receipt.FindingIDs = SortedUnique(append(receipt.FindingIDs, candidateIDs...))
	if strings.Join(receipt.FindingIDs, "\x00") != strings.Join(candidateIDs, "\x00") {
		return MigrationCoverageReceipt{}, errors.New("findingIds must match candidate-finding coverage targets")
	}
	sort.Slice(receipt.Findings, func(i, j int) bool { return receipt.Findings[i].ID < receipt.Findings[j].ID })
	if err := validatePersistedMigrationCoverage(receipt, *source, reportBody); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	receipt.Digest = ""
	receipt.Digest, err = CanonicalDigest(receipt)
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	dir := filepath.Join(engine.migrationRoot(), "coverage")
	if err := engine.ensureMigrationDirectory(dir); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	name := strings.TrimPrefix(SHA256String(receipt.SourcePath), "sha256:") + ".json"
	path := filepath.Join(dir, name)
	if existingBody, readErr := readSingleLinkRegularFile(path); readErr == nil {
		var existing MigrationCoverageReceipt
		if json.Unmarshal(existingBody, &existing) == nil && existing.Digest == receipt.Digest {
			return existing, nil
		}
		return MigrationCoverageReceipt{}, errors.New("coverage receipt is immutable and already exists with different content")
	} else if !os.IsNotExist(readErr) {
		return MigrationCoverageReceipt{}, readErr
	}
	if err := AtomicWriteJSON(path, receipt, 0o600); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	return receipt, nil
}

func migrationReportLineCount(body []byte) (int, error) {
	if !utf8.Valid(body) {
		return 0, errors.New("report is not valid UTF-8")
	}
	lines := strings.Split(string(normalizeNewlines(body)), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return 0, errors.New("report has no lines to cover")
	}
	return len(lines), nil
}

func validateMigrationFindingInput(input MigrationFindingInput) error {
	if !findingIDRE.MatchString(input.ID) ||
		!validOne(input.Kind, "observation", "conclusion", "method", "decision", "constraint", "dead-end", "open-question") ||
		strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.Claim) == "" ||
		strings.ContainsAny(input.Claim, "\r\n") || len([]rune(input.Claim)) > 500 || len(input.Scope) == 0 ||
		!validOne(input.EvidenceGrade, "direct", "inferred", "reported", "unknown") {
		return fmt.Errorf("migration finding %s is incomplete or invalid", input.ID)
	}
	questions := SortedUnique(input.SyntheticQuestions)
	if len(questions) < SyntheticQuestionMinimum || len(questions) > SyntheticQuestionMaximum {
		return fmt.Errorf("migration finding %s requires %d-%d reviewed synthetic questions", input.ID, SyntheticQuestionMinimum, SyntheticQuestionMaximum)
	}
	if len(questions) != len(input.SyntheticQuestions) {
		return fmt.Errorf("migration finding %s synthetic questions must be unique", input.ID)
	}
	for _, question := range questions {
		if !strings.HasSuffix(strings.TrimSpace(question), "?") || strings.ContainsAny(question, "\r\n") {
			return fmt.Errorf("migration finding %s has an invalid synthetic question", input.ID)
		}
	}
	attestation := input.CuratorAttestation
	if !attestation.SingleIndependentlyOverturnableClaim ||
		!attestation.EvidenceGradeAppliesToEntireClaim ||
		!attestation.EntireSourceSpanRepresented ||
		!attestation.SemanticBoundariesVerified ||
		!attestation.LegacyReviewLanguageProvenanceOnly ||
		!attestation.ManagerAttentionRequired ||
		strings.TrimSpace(attestation.Rationale) == "" {
		return fmt.Errorf(
			"migration finding %s requires an explicit curator atomicity, evidence-grade, whole-span, semantic-boundary, legacy-authority, and manager-attention attestation",
			input.ID,
		)
	}
	if strings.TrimSpace(input.Body) != "" {
		if err := validateFindingBodySections(input.Body); err != nil {
			return fmt.Errorf("migration finding %s body: %w", input.ID, err)
		}
	}
	return nil
}

func (engine *MigrationEngine) coverageFor(path string) (MigrationCoverageReceipt, error) {
	name := strings.TrimPrefix(SHA256String(path), "sha256:") + ".json"
	body, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "coverage", name))
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	var receipt MigrationCoverageReceipt
	if err := decodeStrict(body, &receipt); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	expected := receipt.Digest
	receipt.Digest = ""
	digest, err := CanonicalDigest(receipt)
	receipt.Digest = expected
	if err != nil || digest != expected {
		return MigrationCoverageReceipt{}, errors.New("coverage receipt digest mismatch")
	}
	if receipt.SourcePath != path {
		return MigrationCoverageReceipt{}, errors.New("coverage receipt source path does not match its storage identity")
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	var source *MigrationSource
	for index := range plan.Sources {
		if plan.Sources[index].Path == path {
			source = &plan.Sources[index]
			break
		}
	}
	if source == nil || source.Role != "legacy-run-report" || receipt.SourceDigest != source.SHA256 {
		return MigrationCoverageReceipt{}, errors.New("coverage receipt is not bound to an approved legacy report")
	}
	reportBody, err := engine.migrationCoverageReportBody(*source)
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	if err := validatePersistedMigrationCoverage(receipt, *source, reportBody); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	return receipt, nil
}

func (engine *MigrationEngine) migrationCoverageReportBody(source MigrationSource) ([]byte, error) {
	legacyPath := filepath.Join(engine.ProjectRoot, filepath.FromSlash(source.Path))
	if _, err := os.Lstat(legacyPath); err == nil {
		// Before cutover, never hide a changed or unsafe approved source behind
		// an already-present destination copy.
		return readMigrationSource(engine.ProjectRoot, source)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	destination := filepath.Join(engine.ProjectRoot, filepath.FromSlash(source.Destination))
	body, err := readSingleLinkRegularFile(destination)
	if err != nil {
		return nil, err
	}
	if SHA256Bytes(body) != source.SHA256 {
		return nil, errors.New("activated coverage report does not match its approved source digest")
	}
	return body, nil
}

func validatePersistedMigrationCoverage(receipt MigrationCoverageReceipt, source MigrationSource, reportBody []byte) error {
	if receipt.SchemaVersion != MigrationSchemaVersion || !receipt.Complete ||
		strings.TrimSpace(receipt.Reviewer) == "" || strings.TrimSpace(receipt.Rationale) == "" || len(receipt.Coverage) == 0 {
		return errors.New("coverage receipt is not a complete canonical migration receipt")
	}
	lineCount, err := migrationReportLineCount(reportBody)
	if err != nil {
		return err
	}
	seenHandles := map[string]bool{}
	candidates := []string{}
	ordered := append([]CoverageEntry(nil), receipt.Coverage...)
	for _, entry := range receipt.Coverage {
		if entry.SourcePath != source.Destination || entry.SourceSHA256 != "sha256:"+source.SHA256 ||
			entry.SourceHandle != canonicalCoverageHandle(entry) || seenHandles[entry.SourceHandle] ||
			entry.SourceHandle != strings.TrimSpace(entry.SourceHandle) ||
			entry.StartLine < 1 || entry.EndLine < entry.StartLine || entry.EndLine > lineCount ||
			entry.SourceLineCount != lineCount ||
			!validOne(entry.Disposition, "candidate-finding", "duplicate", "non-claim", "unresolved", "out-of-scope") {
			return errors.New("coverage receipt contains a non-canonical or incomplete source partition")
		}
		seenHandles[entry.SourceHandle] = true
		if validOne(entry.Disposition, "candidate-finding", "duplicate") {
			if !findingIDRE.MatchString(entry.TargetID) {
				return errors.New("coverage receipt has an invalid finding target")
			}
			if entry.Disposition == "candidate-finding" {
				candidates = append(candidates, entry.TargetID)
			}
		} else if entry.TargetID != "" {
			return errors.New("non-finding coverage receipt row names a finding")
		}
		if validOne(entry.Disposition, "candidate-finding", "unresolved", "out-of-scope") && strings.TrimSpace(entry.Rationale) == "" {
			return errors.New("candidate-finding, unresolved, or out-of-scope coverage receipt row lacks exact span rationale")
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].StartLine != ordered[j].StartLine {
			return ordered[i].StartLine < ordered[j].StartLine
		}
		return ordered[i].EndLine < ordered[j].EndLine
	})
	nextLine := 1
	for _, entry := range ordered {
		if entry.StartLine != nextLine {
			return fmt.Errorf("coverage receipt has a gap or overlap at source line %d", nextLine)
		}
		nextLine = entry.EndLine + 1
	}
	if nextLine != lineCount+1 {
		return errors.New("coverage receipt does not exhaustively partition its report")
	}
	inputIDs := []string{}
	seenInputs := map[string]bool{}
	for _, finding := range receipt.Findings {
		if err := validateMigrationFindingInput(finding); err != nil || seenInputs[finding.ID] {
			return errors.New("coverage receipt contains an invalid or repeated finding payload")
		}
		seenInputs[finding.ID] = true
		inputIDs = append(inputIDs, finding.ID)
	}
	candidates = SortedUnique(candidates)
	inputIDs = SortedUnique(inputIDs)
	if strings.Join(candidates, "\x00") != strings.Join(inputIDs, "\x00") ||
		strings.Join(candidates, "\x00") != strings.Join(SortedUnique(receipt.FindingIDs), "\x00") ||
		len(receipt.FindingIDs) != len(SortedUnique(receipt.FindingIDs)) {
		return errors.New("coverage receipt finding identities do not match its candidate spans")
	}
	return nil
}

func (engine *MigrationEngine) advanceNormalized(
	state MigrationState, plan MigrationPlan, actor, adapter string,
) (MigrationState, error) {
	live := map[string]bool{}
	for _, slug := range plan.LiveCampaigns {
		live[slug] = true
	}
	blockers := []string{}
	coverageDigests := []string{}
	for _, source := range plan.Sources {
		if source.Role != "legacy-run-report" || !live[source.Campaign] {
			continue
		}
		receipt, err := engine.coverageFor(source.Path)
		if err != nil || !receipt.Complete || receipt.SourceDigest != source.SHA256 {
			blockers = append(blockers, source.Path+": complete digest-matched curator coverage is required")
			continue
		}
		coverageDigests = append(coverageDigests, receipt.Digest)
	}
	if len(blockers) > 0 {
		sort.Strings(blockers)
		state.Blockers = blockers
		state.SafeNextAction = "submit complete coverage for each named live report"
		if err := engine.writeStateLocked(&state); err != nil {
			return MigrationState{}, err
		}
		return state, nil
	}
	manifest, err := engine.buildNormalizedStaging(plan)
	if err != nil {
		return MigrationState{}, err
	}
	outputDigest, err := CanonicalDigest(struct {
		Manifest any
		Coverage []string
	}{manifest, SortedUnique(coverageDigests)})
	if err != nil {
		return MigrationState{}, err
	}
	return engine.commitStage(&state, "normalized", plan.SourceFingerprint, outputDigest,
		actor, adapter, nil, "resume to activate the staged canonical campaign tree")
}

func (engine *MigrationEngine) commitStage(
	state *MigrationState, next, inputDigest, outputDigest, actor, adapter string,
	recovery []string, safeNext string,
) (MigrationState, error) {
	receipt, err := engine.receipt(next, inputDigest, outputDigest, actor, adapter, recovery)
	if err != nil {
		return MigrationState{}, err
	}
	state.State = next
	state.Actor = actor
	state.Adapter = adapter
	state.UpdatedAt = RFC3339UTC(engine.Now().UTC())
	state.LastOperationID = receipt.OperationID
	state.Completed = append(state.Completed, receipt)
	state.Blockers = []string{}
	state.SafeNextAction = safeNext
	if err := engine.writeStateLocked(state); err != nil {
		return MigrationState{}, err
	}
	return *state, nil
}

func (engine *MigrationEngine) receipt(
	stage, inputDigest, outputDigest, actor, adapter string, recovery []string,
) (MigrationOperationReceipt, error) {
	timestamp := RFC3339UTC(engine.Now().UTC())
	receipt := MigrationOperationReceipt{
		SchemaVersion: MigrationSchemaVersion,
		OperationID:   StableID("MOP", stage, inputDigest, outputDigest),
		Stage:         stage, InputDigest: inputDigest, OutputDigest: outputDigest,
		Actor: actor, Adapter: adapter, Timestamp: timestamp,
		Validation: "passed", RecoveryPaths: recovery,
	}
	receipt.Digest = ""
	digest, err := CanonicalDigest(receipt)
	receipt.Digest = digest
	return receipt, err
}

func (engine *MigrationEngine) writeStateLocked(state *MigrationState) error {
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return err
	}
	lock, err := acquireWriterLock(engine.lockPath())
	if err != nil {
		return err
	}
	defer lock.Close()
	return engine.writeState(state)
}

func (engine *MigrationEngine) writeState(state *MigrationState) error {
	state.Digest = ""
	digest, err := CanonicalDigest(*state)
	if err != nil {
		return err
	}
	state.Digest = digest
	return AtomicWriteJSON(engine.statePath(), *state, 0o600)
}

func (engine *MigrationEngine) RecordGate(receipt MigrationGateReceipt) (MigrationGateReceipt, error) {
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationGateReceipt{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationGateReceipt{}, err
	}
	defer operationLock.Close()
	state, err := engine.Status()
	if err != nil {
		return MigrationGateReceipt{}, err
	}
	if state.State != "physically-reorganized" && state.State != "traversal-verified" {
		return MigrationGateReceipt{}, errors.New("certification gates are recorded only after physical reorganization")
	}
	if !validOne(receipt.Gate, "structural", "semantic-traversal", "retrieval-context", "host-parity") ||
		strings.TrimSpace(receipt.Artifact) == "" || strings.TrimSpace(receipt.ArtifactDigest) == "" ||
		strings.TrimSpace(receipt.Reviewer) == "" {
		return MigrationGateReceipt{}, errors.New("gate receipt is incomplete")
	}
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(receipt.ArtifactDigest)), "sha256:")
	receipt.ArtifactDigest = "sha256:" + want
	if err := engine.validateMigrationGateArtifact(state, receipt); err != nil {
		return MigrationGateReceipt{}, err
	}
	receipt.SchemaVersion = MigrationSchemaVersion
	if receipt.Timestamp == "" {
		receipt.Timestamp = RFC3339UTC(engine.Now().UTC())
	}
	if err := validateUTC(receipt.Timestamp); err != nil {
		return MigrationGateReceipt{}, fmt.Errorf("gate receipt timestamp: %w", err)
	}
	receipt.Digest = ""
	receipt.Digest, err = CanonicalDigest(receipt)
	if err != nil {
		return MigrationGateReceipt{}, err
	}
	dir := filepath.Join(engine.migrationRoot(), "gates")
	if err := engine.ensureMigrationDirectory(dir); err != nil {
		return MigrationGateReceipt{}, err
	}
	path := filepath.Join(dir, receipt.Gate+".json")
	if body, readErr := readSingleLinkRegularFile(path); readErr == nil {
		var existing MigrationGateReceipt
		if json.Unmarshal(body, &existing) == nil && existing.Digest == receipt.Digest {
			return existing, nil
		}
		return MigrationGateReceipt{}, errors.New("gate receipt is immutable and already exists")
	} else if !os.IsNotExist(readErr) {
		return MigrationGateReceipt{}, readErr
	}
	if err := AtomicWriteJSON(path, receipt, 0o600); err != nil {
		return MigrationGateReceipt{}, err
	}
	return receipt, nil
}

func (engine *MigrationEngine) validateMigrationGateArtifact(
	state MigrationState, receipt MigrationGateReceipt,
) error {
	return engine.validateMigrationGateArtifactMode(state, receipt, false)
}

func (engine *MigrationEngine) validateMigrationGateArtifactMode(
	state MigrationState, receipt MigrationGateReceipt,
	skipLiveEvidenceReplay bool,
) error {
	artifactBody, err := readMigrationGateArtifact(engine.ProjectRoot, receipt.Artifact)
	if err != nil {
		return fmt.Errorf("read gate artifact: %w", err)
	}
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(receipt.ArtifactDigest)), "sha256:")
	if !hexDigestRE.MatchString(want) || SHA256Bytes(artifactBody) != want {
		return errors.New("gate artifact digest does not match the referenced regular file")
	}
	var artifact MigrationGateArtifact
	if err := decodeStrict(artifactBody, &artifact); err != nil {
		return fmt.Errorf("decode gate artifact: %w", err)
	}
	expectedResult := artifact.ResultDigest
	artifact.ResultDigest = ""
	resultDigest, digestErr := CanonicalDigest(artifact)
	artifact.ResultDigest = expectedResult
	if digestErr != nil || expectedResult != resultDigest || artifact.SchemaVersion != MigrationSchemaVersion ||
		artifact.TransactionID != state.TransactionID || artifact.PlanDigest != state.PlanDigest ||
		artifact.Gate != receipt.Gate || artifact.Passed != receipt.Passed || len(artifact.Checks) == 0 {
		return errors.New("gate artifact identity, result digest, or measured result is invalid")
	}
	measuredPass := true
	seenChecks := map[string]bool{}
	for _, check := range artifact.Checks {
		name := strings.TrimSpace(check.Name)
		if name == "" || strings.TrimSpace(check.Evidence) == "" || seenChecks[name] {
			return errors.New("gate artifact contains an incomplete measured check")
		}
		seenChecks[name] = true
		measuredPass = measuredPass && check.Passed
	}
	for _, name := range requiredMigrationGateChecks(receipt.Gate) {
		if !seenChecks[name] {
			return fmt.Errorf("gate artifact omits required measured check %s", name)
		}
	}
	for _, name := range requiredMigrationGateFingerprints(receipt.Gate) {
		value := strings.TrimSpace(artifact.Fingerprints[name])
		if !digestRE.MatchString(value) {
			return fmt.Errorf("gate artifact omits authenticated fingerprint %s", name)
		}
	}
	if !skipLiveEvidenceReplay {
		if err := engine.validateGateSpecificMigrationEvidence(state, artifact); err != nil {
			return err
		}
	}
	if measuredPass != artifact.Passed {
		return errors.New("gate artifact aggregate result disagrees with its measured checks")
	}
	return nil
}

func requiredMigrationGateChecks(gate string) []string {
	switch gate {
	case "structural":
		return []string{"canonical-records", "stable-ids", "relations", "payload-digests", "event-chain", "legacy-write-paths"}
	case "semantic-traversal":
		return []string{"claim-coverage", "source-provenance", "evidence-resolution", "scope-confidence", "conflicts-dependencies", "bounded-query"}
	case "retrieval-context":
		return []string{"factual-accuracy", "evidence-selection", "durability-labeling", "context-token-budget", "full-corpus-independence", "blinded-agent-evaluation"}
	case "host-parity":
		return []string{"mcp-schema", "cli-status", "claude-host", "codex-host", "semantic-parity", "role-truth-boundaries", "bounded-recovery", "local-fallback"}
	default:
		return nil
	}
}

func requiredMigrationGateFingerprints(gate string) []string {
	switch gate {
	case "structural":
		return []string{"schema", "destination-state"}
	case "semantic-traversal":
		return []string{"inventory", "coverage"}
	case "retrieval-context":
		return []string{"benchmark", "calibration", "blinded-agent-evaluation"}
	case "host-parity":
		return []string{"mcp", "cli", "claude-host", "codex-host"}
	default:
		return nil
	}
}

func readMigrationGateArtifact(projectRoot, handle string) ([]byte, error) {
	clean := NormalizeProjectPath(handle)
	if filepath.IsAbs(handle) || clean == "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || clean != filepath.ToSlash(handle) {
		return nil, errors.New("gate artifact path must be canonical and project-relative")
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(clean))
	resolved, err := canonicalExistingPath(path)
	if err != nil {
		return nil, err
	}
	if !withinRoot(projectRoot, resolved) {
		return nil, errors.New("gate artifact path escapes the project")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("gate artifact must be a regular non-link file")
	}
	return os.ReadFile(path)
}

func (engine *MigrationEngine) Verify() (MigrationCertification, error) {
	return engine.verifyCertification(false)
}

// verifyCertification rebuilds the candidate certification. With
// afterGenesisHeadPublish set, the digest-bound structure of every gate
// receipt and artifact is still fully verified, but the live gate-evidence
// recompute is skipped: once the ratification genesis head is committed,
// context packs legitimately bind the new state head, so a byte-identical
// replay of pre-genesis evidence is impossible by design. The migration plan
// requires recovery to repair an interruption after inventory publication to
// the matching committed head rather than re-deriving fresh evidence.
func (engine *MigrationEngine) verifyCertification(
	afterGenesisHeadPublish bool,
) (MigrationCertification, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationCertification{}, err
	}
	certification := MigrationCertification{
		SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID,
		// The candidate receipt stays byte-stable between the
		// physically-reorganized and traversal-verified states. Ratification
		// can therefore require the exact digest the manager reviewed.
		PlanDigest: state.PlanDigest, State: "physically-reorganized",
		RequiredGates: []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"},
		GateReceipts:  []MigrationGateReceipt{}, SchemaVersions: map[string]int{
			"migration": MigrationSchemaVersion, "campaign": CampaignSchemaVersion,
			"bootstrap": BootstrapSchemaVersion, "knowledge": SettingsSchemaVersion,
		}, KnownLimitations: []string{}, FinalManagerDecision: "pending-ratification", Blockers: []string{},
		PluginVersion: RuntimeVersion,
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationCertification{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return MigrationCertification{}, err
	}
	if structuralErr := engine.verifyMigrationStructural(plan); structuralErr != nil {
		certification.Blockers = append(certification.Blockers, "structural: "+structuralErr.Error())
	}
	if traversalErr := engine.verifyMigrationTraversal(plan); traversalErr != nil {
		certification.Blockers = append(certification.Blockers, "semantic-traversal: "+traversalErr.Error())
	}
	for _, gate := range certification.RequiredGates {
		path := filepath.Join(engine.migrationRoot(), "gates", gate+".json")
		body, readErr := readSingleLinkRegularFile(path)
		if readErr != nil {
			certification.Blockers = append(certification.Blockers, gate+": receipt missing")
			continue
		}
		var receipt MigrationGateReceipt
		if decodeStrict(body, &receipt) != nil {
			certification.Blockers = append(certification.Blockers, gate+": receipt malformed")
			continue
		}
		expected := receipt.Digest
		receipt.Digest = ""
		digest, digestErr := CanonicalDigest(receipt)
		receipt.Digest = expected
		if digestErr != nil || digest != expected || !receipt.Passed {
			certification.Blockers = append(certification.Blockers, gate+": gate did not pass or digest mismatched")
			continue
		}
		if artifactErr := engine.validateMigrationGateArtifactMode(
			state, receipt, afterGenesisHeadPublish); artifactErr != nil {
			certification.Blockers = append(certification.Blockers, gate+": "+artifactErr.Error())
			continue
		}
		certification.GateReceipts = append(certification.GateReceipts, receipt)
		switch gate {
		case "structural":
			certification.Structural = true
		case "semantic-traversal":
			certification.SemanticTraversal = true
		case "retrieval-context":
			certification.RetrievalContext = true
		case "host-parity":
			certification.HostParity = true
		}
	}
	if evidenceErr := engine.populateMigrationCertificationEvidence(&certification, plan, state); evidenceErr != nil {
		certification.Blockers = append(certification.Blockers, "certification-evidence: "+evidenceErr.Error())
	}
	certification.Candidate = len(certification.Blockers) == 0 &&
		certification.Structural && certification.SemanticTraversal &&
		certification.RetrievalContext && certification.HostParity
	certification.Digest = ""
	certification.Digest, err = CanonicalDigest(certification)
	if err != nil {
		return MigrationCertification{}, err
	}
	return certification, nil
}

func (engine *MigrationEngine) populateMigrationCertificationEvidence(
	certification *MigrationCertification,
	plan MigrationPlan,
	state MigrationState,
) error {
	certification.SourceStateHead = plan.SourceFingerprint
	certification.InventoryDigest = plan.SourceFingerprint
	coverageReceipts := []string{}
	live := map[string]bool{}
	for _, campaign := range plan.LiveCampaigns {
		live[campaign] = true
	}
	unnormalized := 0
	for _, source := range plan.Sources {
		if source.Role != "legacy-run-report" {
			continue
		}
		if !live[source.Campaign] {
			unnormalized++
			continue
		}
		receipt, err := engine.coverageFor(source.Path)
		if err != nil || !receipt.Complete || receipt.SourceDigest != source.SHA256 {
			return fmt.Errorf("coverage receipt for %s is unavailable or stale", source.Path)
		}
		coverageReceipts = append(coverageReceipts, receipt.Digest)
	}
	var err error
	certification.CoverageDigest, err = CanonicalDigest(SortedUnique(coverageReceipts))
	if err != nil {
		return err
	}
	if unnormalized > 0 {
		certification.KnownLimitations = append(certification.KnownLimitations,
			fmt.Sprintf("%d non-live legacy reports remain digest-verified unnormalized provenance", unnormalized))
	}

	manifestBody, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "staging", "normalized-manifest.json"))
	if err != nil {
		return err
	}
	var manifest MigrationNormalizedManifest
	if err := decodeStrictJSON(manifestBody, &manifest); err != nil {
		return err
	}
	expectedManifest := manifest.Digest
	manifest.Digest = ""
	actualManifest, err := CanonicalDigest(manifest)
	manifest.Digest = expectedManifest
	if err != nil || actualManifest != expectedManifest || manifest.PlanDigest != plan.PlanDigest {
		return errors.New("normalized manifest cannot support certification")
	}
	physical := MigrationPhysicalEquivalenceSummary{PlannedDestinations: len(manifest.Materializations)}
	for _, receipt := range manifest.Materializations {
		switch receipt.Mode {
		case "staged-byte-exact":
			physical.ByteExact++
		case "staged-transformed":
			physical.Transformed++
		case "retained-in-place":
			physical.RetainedInPlace++
		default:
			return fmt.Errorf("materialization %s has unsupported mode %s", receipt.Destination, receipt.Mode)
		}
	}
	physical.ReceiptDigest, err = CanonicalDigest(manifest.Materializations)
	if err != nil {
		return err
	}
	certification.PhysicalEquivalence = physical

	objects, _, _, _, legacySources, importState, err := engine.verifiedActivatedMigrationSnapshot(plan, state)
	if err != nil {
		return err
	}
	events, err := buildMigrationImportEvents(objects, migrationCampaigns(plan), plan, importState, legacySources)
	if err != nil || len(events) == 0 {
		return errors.New("destination import head cannot be reconstructed")
	}
	last := events[len(events)-1]
	inventory, err := loadVerifiedMigrationStateInventory(engine.ProjectRoot, int64(len(events)))
	if err != nil {
		return err
	}
	destinationHead := StateHead{
		SchemaVersion: CampaignSchemaVersion, Revision: int64(len(events)),
		EventID: last.ID, EventDigest: last.Digest, StateDigest: last.ResultingStateDigest,
		InventoryDigest: inventory.Digest,
		TransactionID:   state.TransactionID,
		EventJournal:    filepath.ToSlash(filepath.Join("active", migrationCampaigns(plan)[len(events)-1], "events", "events.jsonl")),
		UpdatedAt:       last.Timestamp,
	}
	if err := sealStateHead(&destinationHead); err != nil {
		return err
	}
	certification.DestinationStateHead = destinationHead.Digest

	fingerprints := map[string]string{}
	for _, receipt := range certification.GateReceipts {
		body, readErr := readMigrationGateArtifact(engine.ProjectRoot, receipt.Artifact)
		if readErr != nil {
			return readErr
		}
		var artifact MigrationGateArtifact
		if err := decodeStrict(body, &artifact); err != nil {
			return err
		}
		for name, value := range artifact.Fingerprints {
			fingerprints[receipt.Gate+":"+name] = value
		}
	}
	schemaFingerprint, err := CanonicalDigest(certification.SchemaVersions)
	if err != nil {
		return err
	}
	for label, actual := range map[string]string{
		"structural:schema":            schemaFingerprint,
		"structural:destination-state": certification.DestinationStateHead,
		"semantic-traversal:inventory": certification.InventoryDigest,
		"semantic-traversal:coverage":  certification.CoverageDigest,
	} {
		if provided := fingerprints[label]; provided != "" && provided != actual {
			return fmt.Errorf("gate fingerprint %s is stale", label)
		}
	}
	certification.BenchmarkFingerprint = fingerprints["retrieval-context:benchmark"]
	certification.CalibrationFingerprint = fingerprints["retrieval-context:calibration"]
	certification.BlindedAgentEvaluationFingerprint = fingerprints["retrieval-context:blinded-agent-evaluation"]
	certification.MCPConformanceFingerprint = fingerprints["host-parity:mcp"]
	certification.CLIConformanceFingerprint = fingerprints["host-parity:cli"]
	certification.ClaudeHostFingerprint = fingerprints["host-parity:claude-host"]
	certification.CodexHostFingerprint = fingerprints["host-parity:codex-host"]
	return nil
}

func (engine *MigrationEngine) verifyMigrationStructural(plan MigrationPlan) error {
	state, err := engine.Status()
	if err != nil {
		return err
	}
	objects, _, _, _, legacySources, importState, err := engine.verifiedActivatedMigrationSnapshot(plan, state)
	if err != nil {
		return err
	}
	campaigns := migrationCampaigns(plan)
	expectedEvents, err := buildMigrationImportEvents(objects, campaigns, plan, importState, legacySources)
	if err != nil {
		return err
	}
	for index, campaign := range campaigns {
		body, readErr := readSingleLinkRegularFile(filepath.Join(engine.ProjectRoot, "active", campaign, "events", "events.jsonl"))
		if readErr != nil {
			return readErr
		}
		lines := bytes.Split(bytes.TrimSpace(body), []byte{'\n'})
		if len(lines) != 1 {
			return fmt.Errorf("campaign %s import journal has an unexpected event count", campaign)
		}
		var event StateEvent
		if decodeErr := decodeStrictJSON(lines[0], &event); decodeErr != nil || verifyStateEvent(event) != nil ||
			event.Digest != expectedEvents[index].Digest {
			return fmt.Errorf("campaign %s import event does not bind the complete activated snapshot", campaign)
		}
	}
	configuration := LoadConfiguration(engine.ProjectRoot)
	if !configuration.Valid || configuration.Unsafe || configuration.Bootstrap.SchemaVersion != BootstrapSchemaVersion {
		return fmt.Errorf("canonical bootstrap configuration is invalid: %s", strings.Join(configuration.Errors, "; "))
	}
	profile, err := os.ReadFile(filepath.Join(engine.ProjectRoot, ".re-discipline", "project-profile.md"))
	if err != nil || !strings.Contains(string(profile), SharedLawsMarker) {
		return errors.New("canonical project profile lacks the 0.8 shared-laws marker")
	}
	if migrationPlanHasPath(plan, "docs/INDEX.md") {
		body, readErr := os.ReadFile(filepath.Join(engine.ProjectRoot, "docs", "INDEX.md"))
		if readErr != nil || strings.Contains(string(body), "CAMPAIGN.md") {
			return errors.New("project navigation still names a retired legacy campaign masterfile")
		}
		stateLink := regexp.MustCompile(`(?:\.\./)?active/([a-z0-9][a-z0-9-]{1,49})/STATE\.md`)
		for _, match := range stateLink.FindAllStringSubmatch(string(body), -1) {
			path := filepath.Join(engine.ProjectRoot, "active", match[1], "STATE.md")
			if info, statErr := os.Lstat(path); statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("project navigation target active/%s/STATE.md does not resolve", match[1])
			}
		}
	}
	store, err := NewStateStore(engine.ProjectRoot)
	if err != nil {
		return err
	}
	seenRecords := map[string]string{}
	for _, campaign := range migrationCampaigns(plan) {
		graph, err := store.LoadCampaignGraph(campaign)
		if err != nil {
			return fmt.Errorf("load campaign %s: %w", campaign, err)
		}
		if err := graph.Validate(); err != nil {
			return fmt.Errorf("validate campaign %s: %w", campaign, err)
		}
		register := func(id, kind string) error {
			if prior := seenRecords[id]; prior != "" {
				return fmt.Errorf("stable id %s is duplicated by %s and %s", id, prior, kind)
			}
			seenRecords[id] = kind
			return nil
		}
		if err := register(graph.Campaign.ID, "campaign "+campaign); err != nil {
			return err
		}
		for id := range graph.WorkItems {
			if err := register(campaign+":"+id, "work item"); err != nil {
				return err
			}
		}
		for id, run := range graph.Runs {
			if err := register(campaign+":"+id, "run"); err != nil {
				return err
			}
			for _, handle := range []*FileHandle{run.Brief, run.ContextPack, run.Report} {
				if handle == nil {
					continue
				}
				if err := verifyMigrationFileHandle(engine.ProjectRoot, *handle); err != nil {
					return fmt.Errorf("run %s: %w", id, err)
				}
			}
			for _, file := range run.Files {
				handle := FileHandle{Path: filepath.ToSlash(filepath.Join("active", campaign, "runs", id, file.Path)), SHA256: file.SHA256}
				if err := verifyMigrationFileHandle(engine.ProjectRoot, handle); err != nil {
					return fmt.Errorf("run %s payload: %w", id, err)
				}
			}
		}
		for id := range graph.Findings {
			if err := register(campaign+":"+id, "finding"); err != nil {
				return err
			}
		}
		for id := range graph.Intakes {
			if err := register(campaign+":"+id, "intake"); err != nil {
				return err
			}
		}
		for id := range graph.Reviews {
			if err := register(campaign+":"+id, "review"); err != nil {
				return err
			}
		}
		if _, _, err := readCampaignEvents(store.Boundary, campaign, ""); err != nil {
			return fmt.Errorf("campaign %s event journal: %w", campaign, err)
		}
	}
	return nil
}

func migrationCampaigns(plan MigrationPlan) []string {
	set := map[string]bool{}
	for _, source := range plan.Sources {
		if source.Campaign != "" {
			set[source.Campaign] = true
		}
	}
	if len(set) == 0 && len(plan.Sources) > 0 {
		set["migration-provenance"] = true
	}
	out := make([]string, 0, len(set))
	for campaign := range set {
		out = append(out, campaign)
	}
	sort.Strings(out)
	return out
}

func verifyMigrationFileHandle(root string, handle FileHandle) error {
	clean := NormalizeProjectPath(handle.Path)
	if clean != handle.Path || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("file handle %s escapes the project", handle.Path)
	}
	path := filepath.Join(root, filepath.FromSlash(clean))
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("file handle %s does not resolve to a regular file", handle.Path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if "sha256:"+SHA256Bytes(body) != handle.SHA256 {
		return fmt.Errorf("file handle %s digest mismatch", handle.Path)
	}
	return nil
}

func (engine *MigrationEngine) verifyMigrationTraversal(plan MigrationPlan) error {
	state, err := engine.Status()
	if err != nil {
		return err
	}
	store, err := NewStateStore(engine.ProjectRoot)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, campaign := range plan.LiveCampaigns {
		live[campaign] = true
	}
	for _, source := range plan.Sources {
		if source.Role == "legacy-run-report" && live[source.Campaign] {
			receipt, err := engine.coverageFor(source.Path)
			if err != nil || !receipt.Complete || receipt.SourceDigest != source.SHA256 {
				return fmt.Errorf("report %s lacks complete digest-bound coverage", source.Path)
			}
			graph, err := store.LoadCampaignGraph(source.Campaign)
			if err != nil {
				return err
			}
			for _, entry := range receipt.Coverage {
				switch entry.Disposition {
				case "candidate-finding", "duplicate":
					finding, ok := graph.Findings[entry.TargetID]
					if !ok {
						return fmt.Errorf("coverage target %s does not resolve", entry.TargetID)
					}
					resolved := false
					for _, evidence := range finding.Evidence {
						if evidence.ObjectKey == entry.SourceHandle && "sha256:"+source.SHA256 == evidence.SHA256 {
							if err := verifyMigrationFileHandle(engine.ProjectRoot, FileHandle{Path: evidence.Path, SHA256: evidence.SHA256}); err != nil {
								return err
							}
							resolved = true
						}
					}
					if !resolved {
						return fmt.Errorf("coverage handle %s is not exact evidence on %s", entry.SourceHandle, entry.TargetID)
					}
				case "non-claim", "out-of-scope":
				case "unresolved":
					if !migrationUnresolvedCoverageVisible(graph, entry) {
						return fmt.Errorf("coverage handle %s is not preserved as a manager-visible unresolved intake span", entry.SourceHandle)
					}
				default:
					return fmt.Errorf("coverage handle %s remains unresolved", entry.SourceHandle)
				}
			}
		}
		if source.Role == "truth" {
			if err := engine.verifyMigratedTruthCompatibility(plan, state, source); err != nil {
				return err
			}
		}
		if validOne(source.Role, "history", "backlog", "shared-memory") {
			body, err := os.ReadFile(filepath.Join(engine.ProjectRoot, filepath.FromSlash(source.Destination)))
			if err != nil {
				return fmt.Errorf("durable source %s is not readable at %s", source.Path, source.Destination)
			}
			// Durable material is preserved exactly, except that a link into a
			// converted truth document is retargeted to its canonical
			// destination. Recomputing that exact rewrite from the frozen
			// source proves nothing else changed, which a bare digest
			// comparison cannot distinguish from silent corruption.
			frozen, frozenErr := os.ReadFile(
				filepath.Join(engine.ProjectRoot, filepath.FromSlash(source.Path)))
			if frozenErr != nil {
				frozen = body
			}
			expected := rewriteMigratedTruthLinkTargets(
				string(frozen), source.Destination, migratedTruthLinkMapping(plan))
			if SHA256Bytes(body) != source.SHA256 && SHA256Bytes([]byte(expected)) != SHA256Bytes(body) {
				return fmt.Errorf(
					"durable source %s at %s differs from its source beyond the approved link rewrite",
					source.Path, source.Destination)
			}
		}
	}
	return nil
}

func migrationUnresolvedCoverageVisible(graph CampaignGraph, entry CoverageEntry) bool {
	wantUncertainty := entry.SourceHandle + ": " + strings.TrimSpace(entry.Rationale)
	for _, intake := range graph.Intakes {
		if !validOne(intake.Status, "submitted", "reviewed") {
			continue
		}
		matchedCoverage := false
		for _, candidate := range intake.Coverage {
			if reflect.DeepEqual(candidate, entry) {
				matchedCoverage = true
				break
			}
		}
		if !matchedCoverage {
			continue
		}
		matchedUncertainty := false
		for _, uncertainty := range intake.Uncertainties {
			if uncertainty == wantUncertainty {
				matchedUncertainty = true
				break
			}
		}
		if !matchedUncertainty {
			continue
		}
		for _, decision := range intake.RequestedDecisions {
			if strings.Contains(decision, "unresolved source span") {
				return true
			}
		}
	}
	return false
}

func (engine *MigrationEngine) verifyMigratedTruthCompatibility(
	plan MigrationPlan,
	state MigrationState,
	source MigrationSource,
) error {
	rows := []MigrationTruthPlan{}
	truthIDs := map[string][]string{}
	truthDestinations := map[string]string{}
	for _, plannedSource := range plan.Sources {
		if plannedSource.Role == "truth" {
			truthDestinations[plannedSource.Path] = plannedSource.Destination
		}
	}
	for _, planned := range plan.TruthConversions {
		truthIDs[planned.SourcePath] = append(truthIDs[planned.SourcePath], planned.FindingID)
		if planned.SourcePath == source.Path {
			rows = append(rows, planned)
		}
	}
	if len(rows) == 0 {
		return fmt.Errorf("converted truth %s lacks its manager-approved plan row", source.Path)
	}
	firstReceiptPath := filepath.Join(engine.ProjectRoot, ".re-discipline", "knowledge", "migration", "truth-receipts", rows[0].FindingID+".json")
	firstReceiptBody, err := readSingleLinkRegularFile(firstReceiptPath)
	if err != nil {
		return fmt.Errorf("read truth compatibility receipt %s: %w", rows[0].FindingID, err)
	}
	var firstReceipt MigrationTruthCompatibilityReceipt
	if err := decodeStrictJSON(firstReceiptBody, &firstReceipt); err != nil {
		return err
	}
	legacyBody, err := readSingleLinkRegularFile(filepath.Join(engine.ProjectRoot, filepath.FromSlash(firstReceipt.ProvenancePath)))
	if err != nil || "sha256:"+SHA256Bytes(legacyBody) != firstReceipt.ProvenanceDigest ||
		firstReceipt.ProvenanceDigest != "sha256:"+source.SHA256 {
		return fmt.Errorf("converted truth %s does not preserve its exact source provenance", source.Path)
	}
	if rows[0].ReviewDigest == "" {
		if len(rows) != 1 || rows[0].SourceText != legacyTruthAtomicClaim(legacyBody) || rows[0].Claim != rows[0].SourceText {
			return fmt.Errorf("converted truth %s changed an unreviewed legacy atomic claim", source.Path)
		}
	} else if err := verifyMigratedTruthReview(engine.ProjectRoot, source, legacyBody, rows); err != nil {
		return err
	}
	expectedDependencies, expectedRelations, err := legacyTruthDependencies(
		legacyBody, source.Path, truthIDs, truthDestinations, engine.ProjectRoot,
	)
	if err != nil {
		return fmt.Errorf("converted truth %s does not preserve its dependency graph", source.Path)
	}
	legacyDependencyPaths := legacyTruthDependencyPaths(legacyBody, source.Path)
	for _, row := range rows {
		if !reflect.DeepEqual(row.LegacyDependencies, legacyDependencyPaths) {
			return fmt.Errorf("converted truth %s does not preserve its dependency graph", source.Path)
		}
	}
	for _, destination := range expectedDependencies {
		if _, err := os.Stat(filepath.Join(engine.ProjectRoot, filepath.FromSlash(destination))); err != nil {
			return fmt.Errorf("converted truth %s dependency %s does not resolve: %w", source.Path, destination, err)
		}
	}
	settings := DefaultKnowledgeSettings()
	boundary, err := NewBoundary(engine.ProjectRoot)
	if err != nil {
		return err
	}
	inventory, err := DiscoverSources(boundary, settings)
	if err != nil {
		return err
	}
	for _, truthPlan := range rows {
		destinationBody, readErr := readSingleLinkRegularFile(filepath.Join(engine.ProjectRoot, filepath.FromSlash(truthPlan.Destination)))
		if readErr != nil {
			return fmt.Errorf("read converted truth %s split %d: %w", source.Path, truthPlan.SplitIndex, readErr)
		}
		document, parseErr := ParseFindingDocument(destinationBody, truthPlan.Destination)
		if parseErr != nil {
			return fmt.Errorf("parse converted truth %s split %d: %w", source.Path, truthPlan.SplitIndex, parseErr)
		}
		id := truthPlan.FindingID
		expectedValidity, expectedProjection := migrationLegacyTruthState(truthPlan.LegacyStatus)
		if document.Record.ID != id || document.Record.Path != truthPlan.Destination ||
			document.Record.Projection != expectedProjection || document.Record.Validity != expectedValidity ||
			document.Record.PolicyID != "migration:legacy-truth-preservation-v1" ||
			document.Record.Claim != truthPlan.Claim || !document.QuestionsReviewed ||
			!reflect.DeepEqual(document.SyntheticQuestions, truthPlan.SyntheticQuestions) ||
			!reflect.DeepEqual(document.Record.Relations.DependsOn, expectedRelations) {
			return fmt.Errorf("converted truth %s split %d lost its approved identity or dependency graph", source.Path, truthPlan.SplitIndex)
		}
		for _, value := range truthPlan.LegacyScope {
			if !containsString(document.Record.AppliesWhen, value) {
				return fmt.Errorf("converted truth %s split %d lost legacy scope %q", source.Path, truthPlan.SplitIndex, value)
			}
		}
		for _, value := range truthPlan.LegacyExclusions {
			if !containsString(document.Record.KnownLimits, value) {
				return fmt.Errorf("converted truth %s split %d lost legacy exclusion %q", source.Path, truthPlan.SplitIndex, value)
			}
		}
		if truthPlan.LegacyStatus != "" && fmt.Sprint(document.Record.Scope["legacyStatus"]) != truthPlan.LegacyStatus {
			return fmt.Errorf("converted truth %s split %d lost legacy status", source.Path, truthPlan.SplitIndex)
		}
		if truthPlan.LegacyCorrection != "" && fmt.Sprint(document.Record.Scope["legacyCorrection"]) != truthPlan.LegacyCorrection {
			return fmt.Errorf("converted truth %s split %d lost legacy correction", source.Path, truthPlan.SplitIndex)
		}
		receiptPath := filepath.Join(engine.ProjectRoot, ".re-discipline", "knowledge", "migration", "truth-receipts", id+".json")
		receiptBody, readErr := readSingleLinkRegularFile(receiptPath)
		if readErr != nil {
			return fmt.Errorf("read truth compatibility receipt %s: %w", id, readErr)
		}
		var receipt MigrationTruthCompatibilityReceipt
		if err := decodeStrictJSON(receiptBody, &receipt); err != nil {
			return err
		}
		expectedReceiptDigest := receipt.Digest
		receipt.Digest = ""
		receiptDigest, digestErr := CanonicalDigest(receipt)
		receipt.Digest = expectedReceiptDigest
		expectedSearchTerms := SortedUnique([]string{truthPlan.Title, truthPlan.Claim})
		if digestErr != nil || receiptDigest != expectedReceiptDigest || receipt.SchemaVersion != MigrationSchemaVersion ||
			receipt.TransactionID != state.TransactionID || receipt.PlanDigest != plan.PlanDigest ||
			receipt.TruthID != "T-"+id || receipt.FindingID != id || receipt.SourcePath != source.Path ||
			receipt.SourceDigest != "sha256:"+source.SHA256 || receipt.Destination != truthPlan.Destination ||
			receipt.DestinationDigest != "sha256:"+SHA256Bytes(destinationBody) || receipt.ProvenancePath != firstReceipt.ProvenancePath ||
			receipt.ProvenanceDigest != "sha256:"+source.SHA256 || receipt.SourceText != truthPlan.SourceText ||
			receipt.Claim != truthPlan.Claim || receipt.ClaimDigest != truthPlan.ClaimDigest ||
			receipt.LegacyConfidence != truthPlan.LegacyConfidence || receipt.LegacyStatus != truthPlan.LegacyStatus ||
			receipt.LegacyCorrection != truthPlan.LegacyCorrection ||
			!reflect.DeepEqual(receipt.LegacyScope, truthPlan.LegacyScope) ||
			!reflect.DeepEqual(receipt.LegacyExclusions, truthPlan.LegacyExclusions) ||
			receipt.AtomicizationReviewDigest != truthPlan.ReviewDigest ||
			receipt.SplitIndex != truthPlan.SplitIndex || receipt.SplitCount != truthPlan.SplitCount ||
			receipt.ScopePreservation != "exact-source-provenance-no-expansion" ||
			receipt.ApprovedQuestionsDigest != mustCanonicalDigest(truthPlan.SyntheticQuestions) ||
			receipt.ManagerReviewBasis != "approved-migration-plan:"+plan.PlanDigest ||
			!reflect.DeepEqual(receipt.DependencyMap, expectedDependencies) || !reflect.DeepEqual(receipt.SearchTerms, expectedSearchTerms) ||
			!receipt.SemanticPreserved || !receipt.EvidenceReachable || !receipt.ScopeNotExpanded ||
			!receipt.DependenciesResolve || !receipt.SearchCompatibility || receipt.SourceReportRatificationImported || receipt.Status != "passed" {
			return fmt.Errorf("truth compatibility receipt %s is invalid or incomplete", id)
		}
		discovered := false
		for _, candidate := range inventory.Documents {
			if candidate.Path == truthPlan.Destination && candidate.SourceKind == "finding" && candidate.FindingID == id && candidate.Tier == "truth" {
				discovered = true
				break
			}
		}
		if !discovered {
			return fmt.Errorf("converted truth %s split %d is not discoverable as a bounded truth finding", source.Path, truthPlan.SplitIndex)
		}
		if !migrationTruthSearchFinds(plan, receipt.SearchTerms, id, engine.ProjectRoot, 5) {
			return fmt.Errorf("converted truth %s split %d is discoverable but its approved title/core concepts do not retrieve it", source.Path, truthPlan.SplitIndex)
		}
	}
	if len(rows) > 1 {
		manifest, readErr := readSingleLinkRegularFile(filepath.Join(engine.ProjectRoot, filepath.FromSlash(source.Destination)))
		if readErr != nil {
			return fmt.Errorf("read migrated truth split manifest %s: %w", source.Path, readErr)
		}
		for _, row := range rows {
			if !strings.Contains(string(manifest), row.FindingID) {
				return fmt.Errorf("migrated truth split manifest %s omits %s", source.Destination, row.FindingID)
			}
		}
	}
	return verifyMigratedTruthNavigation(engine.ProjectRoot, plan)
}

func verifyMigratedTruthReview(root string, source MigrationSource, legacyBody []byte, rows []MigrationTruthPlan) error {
	conflict, required := migrationTruthConflictForSource(source, legacyBody)
	if !required {
		return fmt.Errorf("converted truth %s cites a review for a source that did not require atomicization", source.Path)
	}
	body, err := readSingleLinkRegularFile(filepath.Join(root, filepath.FromSlash(migrationTruthAuditReviewPath(source.Path))))
	if err != nil {
		return fmt.Errorf("read migrated truth review %s: %w", source.Path, err)
	}
	var review MigrationTruthAtomicizationReview
	if err := decodeStrictJSON(body, &review); err != nil {
		return err
	}
	expected := review.Digest
	review.Digest = ""
	digest, err := CanonicalDigest(review)
	review.Digest = expected
	if err != nil || digest != expected || expected != rows[0].ReviewDigest {
		return fmt.Errorf("migrated truth review %s has an invalid digest", source.Path)
	}
	if err := validateMigrationTruthReview(review, conflict); err != nil {
		return fmt.Errorf("migrated truth review %s is stale: %w", source.Path, err)
	}
	if len(review.Claims) != len(rows) {
		return fmt.Errorf("migrated truth review %s does not cover every planned split", source.Path)
	}
	for index, claim := range review.Claims {
		row := rows[index]
		if row.SourceText != claim.SourceText || row.Title != claim.Title || row.Claim != claim.Claim ||
			!reflect.DeepEqual(row.SyntheticQuestions, claim.SyntheticQuestions) || row.ReviewDigest != review.Digest ||
			row.SplitIndex != index+1 || row.SplitCount != len(rows) {
			return fmt.Errorf("migrated truth review %s does not match planned split %d", source.Path, index+1)
		}
	}
	return nil
}

func migrationTruthSearchFinds(plan MigrationPlan, terms []string, targetID, root string, limit int) bool {
	type candidate struct {
		id    string
		text  string
		score int
	}
	for _, term := range terms {
		queryTokens := migrationSearchTokens(term)
		if len(queryTokens) == 0 {
			return false
		}
		rows := make([]candidate, 0, len(plan.TruthConversions))
		for _, truth := range plan.TruthConversions {
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(truth.Destination)))
			if err != nil {
				return false
			}
			// Weight the field a term matches in. Counting containment
			// anywhere in a document body ties every record that merely
			// mentions the words -- an approved title of a few common terms
			// then loses to unrelated documents on a lexicographic path
			// tie-break, even though real retrieval boosts title and claim
			// fields. This mirrors that ordering without weakening the gate.
			title := strings.ToLower(truth.Title)
			claim := strings.ToLower(truth.Claim)
			text := strings.ToLower(string(body))
			score := 0
			for _, token := range queryTokens {
				switch {
				case strings.Contains(title, token):
					score += 4
				case strings.Contains(claim, token):
					score += 2
				case strings.Contains(text, token):
					score++
				}
			}
			rows = append(rows, candidate{id: truth.FindingID, text: truth.Destination, score: score})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].score != rows[j].score {
				return rows[i].score > rows[j].score
			}
			return rows[i].text < rows[j].text
		})
		found := false
		for index := 0; index < len(rows) && index < limit; index++ {
			if rows[index].id == targetID && rows[index].score > 0 {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var migrationSearchTokenRE = regexp.MustCompile(`[a-z0-9][a-z0-9._-]*`)

func migrationSearchTokens(value string) []string {
	result := []string{}
	for _, token := range migrationSearchTokenRE.FindAllString(strings.ToLower(value), -1) {
		if len(token) > 2 {
			result = append(result, token)
		}
	}
	return SortedUnique(result)
}

func verifyMigratedTruthNavigation(root string, plan MigrationPlan) error {
	mapping := map[string]string{}
	for _, source := range plan.Sources {
		if source.Role == "truth" {
			mapping[source.Path] = source.Destination
		}
	}
	for _, relativeRoot := range []string{"docs/INDEX.md", "docs/truth", "docs/history", "docs/backlog"} {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		info, err := os.Lstat(absoluteRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		verify := func(absolute string) error {
			relative, err := filepath.Rel(root, absolute)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if strings.HasPrefix(relative, "docs/truth/findings/") || filepath.Ext(relative) != ".md" {
				return nil
			}
			body, err := readSingleLinkRegularFile(absolute)
			if err != nil {
				return err
			}
			for _, match := range migrationMarkdownTargetRE.FindAllStringSubmatch(string(body), -1) {
				if len(match) < 2 || strings.Contains(match[1], "://") || strings.HasPrefix(match[1], "#") {
					continue
				}
				target := strings.SplitN(match[1], "#", 2)[0]
				resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(relative), filepath.FromSlash(target))))
				if destination := mapping[resolved]; destination != "" && destination != resolved {
					return fmt.Errorf("managed navigation %s retains stale legacy truth link %s", relative, match[1])
				}
			}
			for source := range mapping {
				if strings.Contains(string(body), source) {
					return fmt.Errorf("managed navigation %s retains stale legacy truth path %s", relative, source)
				}
			}
			return nil
		}
		if info.Mode().IsRegular() {
			if err := verify(absoluteRoot); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(absoluteRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				return verify(path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (engine *MigrationEngine) advanceVerified(
	state MigrationState, certification MigrationCertification, actor, adapter string,
) (MigrationState, error) {
	state.CertificationDigest = certification.Digest
	return engine.commitStage(&state, "traversal-verified", state.Completed[len(state.Completed)-1].OutputDigest,
		certification.Digest, actor, adapter, nil, "manager must ratify the exact certification digest")
}

func (engine *MigrationEngine) Ratify(
	transactionID, certificationDigest, actor, adapter string,
) (MigrationState, error) {
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationState{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer operationLock.Close()
	state, err := engine.Status()
	if err != nil {
		return MigrationState{}, err
	}
	if state.TransactionID != transactionID || state.State != "traversal-verified" ||
		state.CertificationDigest != certificationDigest || strings.TrimSpace(actor) == "" ||
		!validOne(adapter, "cli", "mcp") {
		return MigrationState{}, errors.New("ratification requires the active transaction and exact verified certification digest")
	}
	if head, completed, recoverErr := engine.recoverCompletedMigrationRatification(state, certificationDigest); recoverErr != nil {
		return MigrationState{}, recoverErr
	} else if completed {
		return engine.commitStage(&state, "migrated", certificationDigest, head.Digest,
			actor, adapter, nil, "migration complete; rebuild derived indexes from canonical state")
	}
	genesisCommitted, err := engine.migrationGenesisHeadCommitted(state)
	if err != nil {
		return MigrationState{}, err
	}
	certification, err := engine.verifyCertification(genesisCommitted)
	if err != nil {
		return MigrationState{}, fmt.Errorf("certification is stale or no longer passes: %w", err)
	}
	if !certification.Candidate || certification.Digest != certificationDigest {
		return MigrationState{}, fmt.Errorf("certification is stale or no longer passes (candidate=%t digest=%s blockers=%s)",
			certification.Candidate, certification.Digest, strings.Join(certification.Blockers, "; "))
	}
	head, err := engine.writeMigrationStateHead(state, certification, actor)
	if err != nil {
		return MigrationState{}, err
	}
	return engine.commitStage(&state, "migrated", certificationDigest, head.Digest,
		actor, adapter, nil, "migration complete; rebuild derived indexes from canonical state")
}

// migrationGenesisHeadCommitted reports whether this transaction's genesis
// import head is already the committed canonical head. That is the recovery
// window after an interrupted ratification: the head advanced, so live gate
// evidence bound to the pre-genesis head can no longer replay byte-identically
// and certification must instead be verified from its digest-bound receipts.
func (engine *MigrationEngine) migrationGenesisHeadCommitted(
	state MigrationState,
) (bool, error) {
	store, err := NewStateStore(engine.ProjectRoot)
	if err != nil {
		return false, err
	}
	root, exists, err := store.existingStateRoot()
	if err != nil || !exists || root == "" {
		return false, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return false, err
	}
	initial := initialStateHead()
	if head.Digest == initial.Digest {
		return false, nil
	}
	return head.TransactionID == state.TransactionID, nil
}

func (engine *MigrationEngine) recoverCompletedMigrationRatification(
	state MigrationState,
	certificationDigest string,
) (StateHead, bool, error) {
	store, err := NewStateStore(engine.ProjectRoot)
	if err != nil {
		return StateHead{}, false, err
	}
	if err := store.Recover(context.Background()); err != nil {
		return StateHead{}, false, err
	}
	key := "migration-ratify:" + state.TransactionID + ":" + certificationDigest
	receipt, found, err := store.loadIdempotencyReceipt(key)
	if err != nil || !found {
		return StateHead{}, false, err
	}
	head, err := store.LoadHead()
	if err != nil || head.Digest != receipt.ResultingHead.Digest {
		return StateHead{}, false, errors.New("completed migration ratification does not match the canonical head")
	}
	return head, true, nil
}

// writeMigrationStateHead makes the activated shadow tree reachable through
// the same canonical project-wide commit pointer used by every post-cutover
// mutation. The explicit migrator is the sole importer allowed to construct
// this genesis head from 0.7 material.
func (engine *MigrationEngine) writeMigrationStateHead(
	state MigrationState,
	certification MigrationCertification,
	actor string,
) (StateHead, error) {
	plan, err := engine.loadPlan()
	if err != nil {
		return StateHead{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return StateHead{}, err
	}
	campaigns := migrationCampaigns(plan)
	if len(campaigns) == 0 {
		return StateHead{}, errors.New("migration activation produced no canonical campaigns")
	}
	store, err := NewStateStore(engine.ProjectRoot)
	if err != nil {
		return StateHead{}, err
	}
	store.Failpoint = engine.StateFailpoint
	ctx := context.Background()
	if err := store.Recover(ctx); err != nil {
		return StateHead{}, err
	}
	idempotencyKey := "migration-ratify:" + state.TransactionID + ":" + certification.Digest
	if receipt, found, receiptErr := store.loadIdempotencyReceipt(idempotencyKey); receiptErr != nil {
		return StateHead{}, receiptErr
	} else if found {
		head, headErr := store.LoadHead()
		if headErr != nil || head.Digest != receipt.ResultingHead.Digest {
			return StateHead{}, errors.New("migration ratification receipt does not match the committed head")
		}
		return receipt.ResultingHead, nil
	}

	objects, targets, normalizedDigest, activationDigest, legacySources, importState, err := engine.verifiedActivatedMigrationSnapshot(plan, state)
	if err != nil {
		return StateHead{}, err
	}
	expectedEvents, err := buildMigrationImportEvents(objects, campaigns, plan, importState, legacySources)
	if err != nil {
		return StateHead{}, err
	}
	for index, campaign := range campaigns {
		journal := filepath.Join(engine.ProjectRoot, "active", campaign, "events", "events.jsonl")
		body, readErr := readSingleLinkRegularFile(journal)
		if readErr != nil {
			return StateHead{}, readErr
		}
		lines := bytes.Split(bytes.TrimSpace(body), []byte{'\n'})
		if len(lines) != 1 {
			return StateHead{}, fmt.Errorf("migration import journal %s has an unexpected event count", campaign)
		}
		var actual StateEvent
		if decodeErr := decodeStrictJSON(lines[0], &actual); decodeErr != nil || verifyStateEvent(actual) != nil ||
			actual.Digest != expectedEvents[index].Digest {
			return StateHead{}, fmt.Errorf("migration import event %s does not bind the activated canonical snapshot", campaign)
		}
	}
	last := expectedEvents[len(expectedEvents)-1]
	selected := campaigns[len(campaigns)-1]
	inventory, err := loadVerifiedMigrationStateInventory(engine.ProjectRoot, int64(len(expectedEvents)))
	if err != nil {
		return StateHead{}, err
	}
	importHead := StateHead{
		SchemaVersion: CampaignSchemaVersion, Revision: int64(len(expectedEvents)),
		EventID: last.ID, EventDigest: last.Digest, StateDigest: last.ResultingStateDigest,
		InventoryDigest: inventory.Digest,
		TransactionID:   state.TransactionID,
		EventJournal:    filepath.ToSlash(filepath.Join("active", selected, "events", "events.jsonl")),
		UpdatedAt:       last.Timestamp,
	}
	if err := sealStateHead(&importHead); err != nil {
		return StateHead{}, err
	}
	currentHead, err := store.LoadHead()
	if err != nil {
		return StateHead{}, err
	}
	initial := initialStateHead()
	if currentHead.Digest == initial.Digest {
		headPath, pathErr := store.statePath("head.json")
		if pathErr != nil {
			return StateHead{}, pathErr
		}
		if err := durableAtomicWriteJSON(headPath, importHead, 0o600); err != nil {
			return StateHead{}, err
		}
		currentHead = importHead
	} else if currentHead.Digest != importHead.Digest {
		return StateHead{}, errors.New("canonical state head changed during migration ratification")
	}

	snapshotDigest, err := CanonicalDigest(objects)
	if err != nil {
		return StateHead{}, err
	}
	manifest := MigrationRatificationManifest{
		SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID,
		PlanDigest: plan.PlanDigest, Certification: certification,
		CertificationDigest: certification.Digest, SnapshotDigest: snapshotDigest,
		SnapshotObjects: len(objects), NormalizedManifestDigest: normalizedDigest,
		ActivationJournalDigest: activationDigest, ManagedTargets: targets, ImportHead: importHead,
		FinalManagerDecision: "ratified", DecisionActor: actor,
	}
	manifest.Digest, err = CanonicalDigest(manifest)
	if err != nil {
		return StateHead{}, err
	}
	manifestBody, err := canonicalJSON(manifest)
	if err != nil {
		return StateHead{}, err
	}
	graph, err := store.LoadCampaignGraph(selected)
	if err != nil || graph.Campaign == nil {
		return StateHead{}, fmt.Errorf("load migration ratification campaign: %w", err)
	}
	artifactPath := ".re-discipline/migration/0.8/certification.json"
	receipt, err := store.applyAllowingMigration(ctx, StateTransactionRequest{
		CampaignSlug: selected, CampaignID: graph.Campaign.ID,
		Actor: actor, Authority: "manager", Action: "migration.ratify",
		Rationale:     "Ratified the exact fully passing 0.8 migration certification and activated snapshot.",
		CorrelationID: state.TransactionID, IdempotencyKey: idempotencyKey,
		ExpectedHeadRevision: currentHead.Revision, ExpectedHeadDigest: currentHead.Digest,
		Artifacts: []StateArtifactWrite{{Path: artifactPath, ContentDigest: "sha256:" + SHA256Bytes(manifestBody), Body: manifestBody}},
	})
	if err != nil {
		return StateHead{}, err
	}
	return receipt.ResultingHead, nil
}

func (engine *MigrationEngine) verifiedActivatedMigrationSnapshot(
	plan MigrationPlan,
	state MigrationState,
) ([]migrationImportObject, []migrationImportObject, string, string, map[string]string, MigrationState, error) {
	manifestPath := filepath.Join(engine.migrationRoot(), "staging", "normalized-manifest.json")
	manifestBody, err := readSingleLinkRegularFile(manifestPath)
	if err != nil {
		return nil, nil, "", "", nil, MigrationState{}, err
	}
	var manifest MigrationNormalizedManifest
	if err := decodeStrictJSON(manifestBody, &manifest); err != nil {
		return nil, nil, "", "", nil, MigrationState{}, err
	}
	expectedManifestDigest := manifest.Digest
	manifest.Digest = ""
	manifestDigest, err := CanonicalDigest(manifest)
	manifest.Digest = expectedManifestDigest
	if err != nil || manifestDigest != expectedManifestDigest || manifest.PlanDigest != plan.PlanDigest {
		return nil, nil, "", "", nil, MigrationState{}, errors.New("normalized migration manifest is stale or invalid")
	}
	if manifest.TransactionID != state.TransactionID || strings.TrimSpace(manifest.ImportActor) == "" ||
		validateUTC(manifest.ImportTimestamp) != nil {
		return nil, nil, "", "", nil, MigrationState{}, errors.New("normalized migration import identity is invalid")
	}
	if err := engine.verifyPublishedMigrationMaterializations(plan, manifest.Materializations); err != nil {
		return nil, nil, "", "", nil, MigrationState{}, err
	}
	importState := MigrationState{TransactionID: manifest.TransactionID, Actor: manifest.ImportActor, UpdatedAt: manifest.ImportTimestamp}

	activationBody, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "activation.json"))
	if err != nil {
		return nil, nil, "", "", nil, MigrationState{}, err
	}
	var activation migrationActivationJournal
	if err := decodeStrictJSON(activationBody, &activation); err != nil {
		return nil, nil, "", "", nil, MigrationState{}, err
	}
	expectedActivationDigest := activation.Digest
	activation.Digest = ""
	activationDigest, err := CanonicalDigest(activation)
	activation.Digest = expectedActivationDigest
	if err != nil || activationDigest != expectedActivationDigest || activation.Phase != "activated" ||
		activation.TransactionID != state.TransactionID || activation.PlanDigest != plan.PlanDigest ||
		activation.StagedDigest != expectedManifestDigest {
		return nil, nil, "", "", nil, MigrationState{}, errors.New("activation journal is not a fully published plan-bound transaction")
	}

	wantTargets := migrationManagedTargets(plan)
	seen := map[string]bool{}
	targets := make([]migrationImportObject, 0, len(activation.Targets))
	objects := make([]migrationImportObject, 0, len(manifest.Files))
	for _, target := range activation.Targets {
		if seen[target.Path] || target.Phase != "published" || manifest.ManagedTargets[target.Path] != target.StagedDigest {
			return nil, nil, "", "", nil, MigrationState{}, errors.New("activation target set is duplicated, incomplete, or manifest-mismatched")
		}
		seen[target.Path] = true
		absolute := filepath.Join(engine.ProjectRoot, filepath.FromSlash(target.Path))
		actual, digestErr := digestMigrationPath(absolute)
		if digestErr != nil || actual != target.StagedDigest {
			return nil, nil, "", "", nil, MigrationState{}, fmt.Errorf("activated managed target %s drifted", target.Path)
		}
		targets = append(targets, migrationImportObject{Path: target.Path, SHA256: actual})
		rows, rowErr := migrationTargetImportObjects(engine.ProjectRoot, target.Path)
		if rowErr != nil {
			return nil, nil, "", "", nil, MigrationState{}, rowErr
		}
		objects = append(objects, rows...)
	}
	for _, target := range wantTargets {
		if !seen[target] {
			return nil, nil, "", "", nil, MigrationState{}, fmt.Errorf("activation journal omitted managed target %s", target)
		}
	}
	if len(seen) != len(wantTargets) {
		return nil, nil, "", "", nil, MigrationState{}, errors.New("activation journal includes an unapproved managed target")
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
	return objects, targets, expectedManifestDigest, expectedActivationDigest,
		manifest.LegacySources, importState, nil
}

func migrationTargetImportObjects(projectRoot, relative string) ([]migrationImportObject, error) {
	// inventory.json is independently authenticated by the normalized
	// manifest, activation journal, and StateHead.InventoryDigest. Excluding
	// the inventory file itself avoids a circular import-event commitment.
	if relative == stateInventoryPath {
		return []migrationImportObject{}, nil
	}
	absolute := filepath.Join(projectRoot, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("managed migration target %s is unsafe or missing", relative)
	}
	if info.Mode().IsRegular() {
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return nil, err
		}
		return []migrationImportObject{{Path: relative, SHA256: "sha256:" + SHA256Bytes(body)}}, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("managed migration target %s is not a regular tree", relative)
	}
	tree, err := digestRegularTree(absolute)
	if err != nil {
		return nil, err
	}
	objects := make([]migrationImportObject, 0, len(tree))
	for child, digest := range tree {
		path := filepath.ToSlash(filepath.Join(relative, child))
		if strings.HasSuffix(path, "/events/events.jsonl") {
			continue
		}
		objects = append(objects, migrationImportObject{Path: path, SHA256: "sha256:" + digest})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
	return objects, nil
}

// stableJSONLines returns canonical newline-delimited JSON for deterministic
// migration artifacts.
func stableJSONLines(values []any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}
