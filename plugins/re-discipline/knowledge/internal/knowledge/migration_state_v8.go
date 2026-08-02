package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	SchemaVersion int             `json:"schemaVersion"`
	SourcePath    string          `json:"sourcePath"`
	SourceDigest  string          `json:"sourceDigest"`
	Complete      bool            `json:"complete"`
	Coverage      []CoverageEntry `json:"coverage"`
	FindingIDs    []string        `json:"findingIds,omitempty"`
	Reviewer      string          `json:"reviewer"`
	Rationale     string          `json:"rationale"`
	Digest        string          `json:"digest"`
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

type MigrationCertification struct {
	SchemaVersion     int                    `json:"schemaVersion"`
	TransactionID     string                 `json:"transactionId"`
	PlanDigest        string                 `json:"planDigest"`
	State             string                 `json:"state"`
	Structural        bool                   `json:"structural"`
	SemanticTraversal bool                   `json:"semanticTraversal"`
	RetrievalContext  bool                   `json:"retrievalContext"`
	HostParity        bool                   `json:"hostParity"`
	RequiredGates     []string               `json:"requiredGates"`
	GateReceipts      []MigrationGateReceipt `json:"gateReceipts"`
	Blockers          []string               `json:"blockers"`
	Candidate         bool                   `json:"candidate"`
	Digest            string                 `json:"digest"`
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

type MigrationEngine struct {
	ProjectRoot string
	Now         func() time.Time
}

func NewMigrationEngine(projectRoot string) (*MigrationEngine, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return nil, err
	}
	return &MigrationEngine{ProjectRoot: boundary.Root, Now: time.Now}, nil
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

func (engine *MigrationEngine) Start(
	plan MigrationPlan, approvedDigest, actor, adapter string,
) (MigrationState, error) {
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
	if err := os.MkdirAll(engine.migrationRoot(), 0o700); err != nil {
		return MigrationState{}, err
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
	body, err := os.ReadFile(engine.statePath())
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
	body, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "plan.json"))
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

func (engine *MigrationEngine) Resume(transactionID, actor, adapter string) (MigrationState, error) {
	if strings.TrimSpace(actor) == "" || !validOne(adapter, "cli", "mcp") {
		return MigrationState{}, errors.New("migration actor and adapter are required")
	}
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
	if state.State != "migrated" {
		fresh, previewErr := PreviewMigration(engine.ProjectRoot, plan.LiveCampaigns)
		if previewErr != nil {
			return MigrationState{}, previewErr
		}
		if state.State == "inventoried" || state.State == "shadow-indexed" {
			if fresh.Plan.SourceFingerprint != state.SourceFingerprint {
				return MigrationState{}, errors.New("legacy migration source snapshot changed; preview approval is stale")
			}
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
	type shadowEntry struct {
		Path       string `json:"path"`
		SHA256     string `json:"sha256"`
		Campaign   string `json:"campaign"`
		Normalized bool   `json:"normalized"`
		Label      string `json:"label"`
	}
	entries := []shadowEntry{}
	for _, source := range plan.Sources {
		if source.Role == "legacy-run-report" {
			entries = append(entries, shadowEntry{
				Path: source.Path, SHA256: source.SHA256, Campaign: source.Campaign,
				Normalized: false, Label: "unnormalized provenance",
			})
		}
	}
	artifact := struct {
		SchemaVersion int           `json:"schemaVersion"`
		PlanDigest    string        `json:"planDigest"`
		Reports       []shadowEntry `json:"reports"`
	}{MigrationSchemaVersion, plan.PlanDigest, entries}
	outputDigest, err := CanonicalDigest(artifact)
	if err != nil {
		return MigrationState{}, err
	}
	if err := AtomicWriteJSON(filepath.Join(engine.migrationRoot(), "shadow-catalog.json"), artifact, 0o600); err != nil {
		return MigrationState{}, err
	}
	return engine.commitStage(&state, "shadow-indexed", plan.SourceFingerprint, outputDigest,
		actor, adapter, nil, "submit complete curator coverage for designated live reports, then resume")
}

func (engine *MigrationEngine) SubmitCoverage(receipt MigrationCoverageReceipt) (MigrationCoverageReceipt, error) {
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
	if strings.TrimSpace(receipt.Reviewer) == "" || len(receipt.Coverage) == 0 {
		return MigrationCoverageReceipt{}, errors.New("coverage requires a reviewer and at least one classified span")
	}
	for _, entry := range receipt.Coverage {
		if strings.TrimSpace(entry.SourceHandle) == "" ||
			!validOne(entry.Disposition, "candidate-finding", "duplicate", "non-claim", "unresolved", "out-of-scope") {
			return MigrationCoverageReceipt{}, errors.New("coverage contains an invalid source handle or disposition")
		}
		if receipt.Complete && entry.Disposition == "unresolved" {
			return MigrationCoverageReceipt{}, errors.New("complete coverage cannot retain unresolved spans")
		}
	}
	receipt.SchemaVersion = MigrationSchemaVersion
	receipt.FindingIDs = SortedUnique(receipt.FindingIDs)
	receipt.Digest = ""
	receipt.Digest, err = CanonicalDigest(receipt)
	if err != nil {
		return MigrationCoverageReceipt{}, err
	}
	dir := filepath.Join(engine.migrationRoot(), "coverage")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	name := strings.TrimPrefix(SHA256String(receipt.SourcePath), "sha256:") + ".json"
	path := filepath.Join(dir, name)
	if existingBody, readErr := os.ReadFile(path); readErr == nil {
		var existing MigrationCoverageReceipt
		if json.Unmarshal(existingBody, &existing) == nil && existing.Digest == receipt.Digest {
			return existing, nil
		}
		return MigrationCoverageReceipt{}, errors.New("coverage receipt is immutable and already exists with different content")
	}
	if err := AtomicWriteJSON(path, receipt, 0o600); err != nil {
		return MigrationCoverageReceipt{}, err
	}
	return receipt, nil
}

func (engine *MigrationEngine) coverageFor(path string) (MigrationCoverageReceipt, error) {
	name := strings.TrimPrefix(SHA256String(path), "sha256:") + ".json"
	body, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "coverage", name))
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
	return receipt, nil
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
	if err := os.MkdirAll(engine.migrationRoot(), 0o700); err != nil {
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
	receipt.SchemaVersion = MigrationSchemaVersion
	if receipt.Timestamp == "" {
		receipt.Timestamp = RFC3339UTC(engine.Now().UTC())
	}
	receipt.Digest = ""
	receipt.Digest, err = CanonicalDigest(receipt)
	if err != nil {
		return MigrationGateReceipt{}, err
	}
	dir := filepath.Join(engine.migrationRoot(), "gates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MigrationGateReceipt{}, err
	}
	path := filepath.Join(dir, receipt.Gate+".json")
	if body, readErr := os.ReadFile(path); readErr == nil {
		var existing MigrationGateReceipt
		if json.Unmarshal(body, &existing) == nil && existing.Digest == receipt.Digest {
			return existing, nil
		}
		return MigrationGateReceipt{}, errors.New("gate receipt is immutable and already exists")
	}
	if err := AtomicWriteJSON(path, receipt, 0o600); err != nil {
		return MigrationGateReceipt{}, err
	}
	return receipt, nil
}

func (engine *MigrationEngine) Verify() (MigrationCertification, error) {
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
		GateReceipts:  []MigrationGateReceipt{}, Blockers: []string{},
	}
	for _, gate := range certification.RequiredGates {
		path := filepath.Join(engine.migrationRoot(), "gates", gate+".json")
		body, readErr := os.ReadFile(path)
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
	state, err := engine.Status()
	if err != nil {
		return MigrationState{}, err
	}
	if state.TransactionID != transactionID || state.State != "traversal-verified" ||
		state.CertificationDigest != certificationDigest || strings.TrimSpace(actor) == "" {
		return MigrationState{}, errors.New("ratification requires the active transaction and exact verified certification digest")
	}
	certification, err := engine.Verify()
	if err != nil || !certification.Candidate || certification.Digest != certificationDigest {
		return MigrationState{}, errors.New("certification is stale or no longer passes")
	}
	head, err := engine.writeMigrationStateHead(state)
	if err != nil {
		return MigrationState{}, err
	}
	return engine.commitStage(&state, "migrated", certificationDigest, head.Digest,
		actor, adapter, nil, "migration complete; rebuild derived indexes from canonical state")
}

// writeMigrationStateHead makes the activated shadow tree reachable through
// the same canonical project-wide commit pointer used by every post-cutover
// mutation. The explicit migrator is the sole importer allowed to construct
// this genesis head from 0.7 material.
func (engine *MigrationEngine) writeMigrationStateHead(state MigrationState) (StateHead, error) {
	active := filepath.Join(engine.ProjectRoot, "active")
	entries, err := os.ReadDir(active)
	if err != nil {
		return StateHead{}, err
	}
	campaigns := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && managedSlugRE.MatchString(entry.Name()) {
			campaigns = append(campaigns, entry.Name())
		}
	}
	sort.Strings(campaigns)
	if len(campaigns) == 0 {
		return StateHead{}, errors.New("migration activation produced no canonical campaigns")
	}

	selected := campaigns[len(campaigns)-1]
	eventJournal := filepath.ToSlash(filepath.Join("active", selected, "events", "events.jsonl"))
	eventBody, err := os.ReadFile(filepath.Join(engine.ProjectRoot, filepath.FromSlash(eventJournal)))
	if err != nil {
		return StateHead{}, err
	}
	lines := bytes.Split(bytes.TrimSpace(eventBody), []byte{'\n'})
	if len(lines) == 0 || len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		return StateHead{}, errors.New("migration campaign event journal is empty")
	}
	var event StateEvent
	if err := decodeStrictJSON(lines[len(lines)-1], &event); err != nil {
		return StateHead{}, fmt.Errorf("decode migration event: %w", err)
	}
	if err := verifyStateEvent(event); err != nil {
		return StateHead{}, fmt.Errorf("verify migration event: %w", err)
	}

	treeDigest, err := CanonicalDigest(mustTreeDigest(active))
	if err != nil {
		return StateHead{}, err
	}
	// Bind the project-wide genesis event to the exact activated tree. The
	// per-campaign import event was already valid; ratification upgrades the
	// selected journal head into the global commit chain without inventing a
	// second out-of-band marker.
	event.PreviousStateDigest = initialStateHead().StateDigest
	event.MutationDigest = treeDigest
	event.ResultingStateDigest, err = CanonicalDigest(struct {
		Previous string `json:"previous"`
		Mutation string `json:"mutation"`
	}{event.PreviousStateDigest, event.MutationDigest})
	if err != nil {
		return StateHead{}, err
	}
	if err := sealStateEvent(&event); err != nil {
		return StateHead{}, err
	}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		return StateHead{}, err
	}
	encodedEvent = append(encodedEvent, '\n')
	if err := AtomicWrite(filepath.Join(engine.ProjectRoot, filepath.FromSlash(eventJournal)), encodedEvent, 0o600); err != nil {
		return StateHead{}, err
	}
	head := StateHead{
		SchemaVersion: CampaignSchemaVersion,
		Revision:      1,
		EventID:       event.ID,
		EventDigest:   event.Digest,
		StateDigest:   event.ResultingStateDigest,
		TransactionID: state.TransactionID,
		EventJournal:  eventJournal,
		UpdatedAt:     state.UpdatedAt,
	}
	if err := sealStateHead(&head); err != nil {
		return StateHead{}, err
	}
	if err := AtomicWriteJSON(filepath.Join(engine.ProjectRoot, ".re-discipline", "state", "head.json"), head, 0o600); err != nil {
		return StateHead{}, err
	}
	return head, nil
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
