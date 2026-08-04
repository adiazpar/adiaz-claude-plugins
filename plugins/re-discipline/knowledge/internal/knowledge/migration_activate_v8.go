package knowledge

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// These are copied from the release's project templates and compiled into the
// migrator. A project conversion cannot depend on a source checkout or infer
// a plugin installation path at runtime.
//
//go:embed migration_templates/dispatch.ps1
var migrationDispatchTemplate string

//go:embed migration_templates/external-drafter-contract.md
var migrationExternalDrafterContractTemplate string

//go:embed migration_templates/drafter-AGENTS-override.md
var migrationDrafterOverrideTemplate string

type MigrationNormalizedManifest struct {
	SchemaVersion    int                               `json:"schemaVersion"`
	PlanDigest       string                            `json:"planDigest"`
	TransactionID    string                            `json:"transactionId"`
	ImportActor      string                            `json:"importActor"`
	ImportTimestamp  string                            `json:"importTimestamp"`
	Campaigns        []string                          `json:"campaigns"`
	Files            map[string]string                 `json:"files"`
	LegacySources    map[string]string                 `json:"legacySources"`
	ManagedTargets   map[string]string                 `json:"managedTargets"`
	Materializations []MigrationMaterializationReceipt `json:"materializations"`
	Digest           string                            `json:"digest"`
}

// MigrationMaterializationReceipt binds every previewed destination to the
// exact object that will be published or retained in place. This prevents a
// plan from claiming a transformation that staging silently omitted.
type MigrationMaterializationReceipt struct {
	OperationID        string `json:"operationId"`
	SourcePath         string `json:"sourcePath"`
	SourceDigest       string `json:"sourceDigest"`
	SourceMtimeNS      int64  `json:"sourceMtimeNs"`
	Destination        string `json:"destination"`
	DestinationDigest  string `json:"destinationDigest"`
	DestinationMtimeNS int64  `json:"destinationMtimeNs"`
	Mode               string `json:"mode"`
	Digest             string `json:"digest"`
}

// migrationImportObject is the immutable content descriptor committed by
// migration.import. It includes canonical records, generated views, frozen
// source material, run payloads, coverage, and intake bytes. Event journals
// are committed by the global event chain itself.
type migrationImportObject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type migrationSourceBinding struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type MigrationTruthCompatibilityReceipt struct {
	SchemaVersion                    int               `json:"schemaVersion"`
	TransactionID                    string            `json:"transactionId"`
	PlanDigest                       string            `json:"planDigest"`
	TruthID                          string            `json:"truthId"`
	FindingID                        string            `json:"findingId"`
	SourcePath                       string            `json:"sourcePath"`
	SourceDigest                     string            `json:"sourceDigest"`
	Destination                      string            `json:"destination"`
	DestinationDigest                string            `json:"destinationDigest"`
	ProvenancePath                   string            `json:"provenancePath"`
	ProvenanceDigest                 string            `json:"provenanceDigest"`
	SourceText                       string            `json:"sourceText"`
	Claim                            string            `json:"claim"`
	ClaimDigest                      string            `json:"claimDigest"`
	LegacyConfidence                 string            `json:"legacyConfidence,omitempty"`
	LegacyStatus                     string            `json:"legacyStatus,omitempty"`
	LegacyCorrection                 string            `json:"legacyCorrection,omitempty"`
	LegacyScope                      []string          `json:"legacyScope"`
	LegacyExclusions                 []string          `json:"legacyExclusions"`
	AtomicizationReviewDigest        string            `json:"atomicizationReviewDigest"`
	SplitIndex                       int               `json:"splitIndex"`
	SplitCount                       int               `json:"splitCount"`
	ScopePreservation                string            `json:"scopePreservation"`
	ApprovedQuestionsDigest          string            `json:"approvedQuestionsDigest"`
	ManagerReviewBasis               string            `json:"managerReviewBasis"`
	DependencyMap                    map[string]string `json:"dependencyMap"`
	SearchTerms                      []string          `json:"searchTerms"`
	SemanticPreserved                bool              `json:"semanticPreserved"`
	EvidenceReachable                bool              `json:"evidenceReachable"`
	ScopeNotExpanded                 bool              `json:"scopeNotExpanded"`
	DependenciesResolve              bool              `json:"dependenciesResolve"`
	SearchCompatibility              bool              `json:"searchCompatibility"`
	SourceReportRatificationImported bool              `json:"sourceReportRatificationImported"`
	Status                           string            `json:"status"`
	Digest                           string            `json:"digest"`
}

type migrationActivationTarget struct {
	Path         string `json:"path"`
	StagedDigest string `json:"stagedDigest"`
	SourceDigest string `json:"sourceDigest,omitempty"`
	BackupPath   string `json:"backupPath"`
	Existed      bool   `json:"existed"`
	Phase        string `json:"phase"`
}

type migrationActivationJournal struct {
	SchemaVersion int                         `json:"schemaVersion"`
	TransactionID string                      `json:"transactionId"`
	PlanDigest    string                      `json:"planDigest"`
	Phase         string                      `json:"phase"`
	StagedDigest  string                      `json:"stagedDigest"`
	Targets       []migrationActivationTarget `json:"targets"`
	Digest        string                      `json:"digest"`
}

func (engine *MigrationEngine) buildNormalizedStaging(plan MigrationPlan) (MigrationNormalizedManifest, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := engine.validateMigrationNamespaces(plan, state); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	stagingRoot := filepath.Join(engine.migrationRoot(), "staging")
	if err := resetMigrationStaging(engine.migrationRoot(), stagingRoot); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	projectStagingRoot := filepath.Join(stagingRoot, "project")
	activeRoot := filepath.Join(projectStagingRoot, "active")
	if err := os.MkdirAll(activeRoot, 0o700); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := engine.stageManagedProject(plan, projectStagingRoot); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	campaigns := migrationCampaigns(plan)
	for _, campaign := range campaigns {
		if !managedSlugRE.MatchString(campaign) {
			return MigrationNormalizedManifest{}, fmt.Errorf("legacy campaign slug %q is invalid", campaign)
		}
	}
	manifest := MigrationNormalizedManifest{
		SchemaVersion: MigrationSchemaVersion, PlanDigest: plan.PlanDigest,
		TransactionID: state.TransactionID, ImportActor: state.Actor, ImportTimestamp: state.UpdatedAt,
		Campaigns: campaigns, Files: map[string]string{}, LegacySources: map[string]string{},
		ManagedTargets: map[string]string{}, Materializations: []MigrationMaterializationReceipt{},
	}
	for _, campaign := range campaigns {
		if err := engine.stageCampaign(plan, state, campaign, activeRoot, &manifest); err != nil {
			return MigrationNormalizedManifest{}, err
		}
	}
	if err := engine.stageLegacyTruthConversions(plan, state, projectStagingRoot, campaigns, &manifest); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	// Carried-forward bytes must exist before the import chain is sealed, so
	// the events bind the complete activated snapshot. They must also exist
	// before evaluation staging: the restamped corpus snapshot is derived
	// from the complete staged tree, so any preserved Markdown carried
	// forward has to be in place first.
	if err := engine.carryForwardUnplannedFiles(plan, projectStagingRoot); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := engine.stageMigratedEvaluationCorpus(plan, projectStagingRoot); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := sealMigrationImportChain(projectStagingRoot, campaigns, plan, state, manifest.LegacySources); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	// The import head is not authoritative until ratification, but its complete
	// byte inventory must already be part of the activated, recoverable shadow
	// transaction. Publishing the inventory as an independently journaled
	// target closes the gap where a hand-built genesis head could reach records
	// whose run payloads or event journals were never committed.
	stateInventory, err := migrationStateInventoryForRoot(projectStagingRoot, int64(len(campaigns)))
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	stateInventoryBody, err := canonicalJSON(stateInventory)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := AtomicWrite(filepath.Join(projectStagingRoot, filepath.FromSlash(stateInventoryPath)), stateInventoryBody, 0o600); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	manifest.Materializations, err = engine.verifyMigrationPlanMaterialization(plan, projectStagingRoot)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	files, err := digestRegularTree(projectStagingRoot)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	manifest.Files = files
	for _, target := range migrationManagedTargets(plan) {
		digest, err := digestMigrationPath(filepath.Join(projectStagingRoot, filepath.FromSlash(target)))
		if err != nil {
			return MigrationNormalizedManifest{}, fmt.Errorf("digest staged target %s: %w", target, err)
		}
		manifest.ManagedTargets[target] = digest
	}
	manifest.Digest = ""
	manifest.Digest, err = CanonicalDigest(manifest)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := AtomicWriteJSON(filepath.Join(stagingRoot, "normalized-manifest.json"), manifest, 0o600); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	return manifest, nil
}

type migrationNamespaceRegistry struct {
	owners map[string]string
}

func newMigrationNamespaceRegistry() *migrationNamespaceRegistry {
	return &migrationNamespaceRegistry{owners: map[string]string{}}
}

func (registry *migrationNamespaceRegistry) reserve(namespace, scope, id, owner string) error {
	if registry == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" {
		return errors.New("migration namespace reservation is incomplete")
	}
	key := namespace + "\x00" + scope + "\x00" + id
	if prior, exists := registry.owners[key]; exists {
		return fmt.Errorf("migration %s namespace collision for %s between %s and %s", namespace, id, prior, owner)
	}
	registry.owners[key] = owner
	return nil
}

// validateMigrationNamespaces reserves every canonical identity before the
// first staging write. The generated and additive knowledge planes share the
// same registry, so a deterministic hash or legacy ID collision fails closed
// instead of overwriting an earlier record.
func (engine *MigrationEngine) validateMigrationNamespaces(plan MigrationPlan, state MigrationState) error {
	registry := newMigrationNamespaceRegistry()
	campaigns := migrationCampaigns(plan)
	live := map[string]bool{}
	for _, campaign := range plan.LiveCampaigns {
		live[campaign] = true
	}
	for _, campaign := range campaigns {
		campaignID := "C-" + strings.ToUpper(campaign)
		if err := registry.reserve("campaign", "project", campaignID, "legacy campaign "+campaign); err != nil {
			return err
		}
		masterPath, masterBody, err := engine.legacyCampaignMaster(plan, campaign)
		if err != nil {
			return err
		}
		for _, work := range legacyCampaignWorkItems(masterBody, campaignID, state) {
			if err := registry.reserve("work-item", campaignID, work.ID, masterPath+" frontier"); err != nil {
				return err
			}
		}
		workspaces := map[string]bool{"campaign-import": true}
		for _, source := range plan.Sources {
			if source.Campaign != campaign || !validOne(source.Role, "legacy-run-report", "legacy-run-file") {
				continue
			}
			parts := strings.Split(source.Path, "/")
			if len(parts) > 3 {
				workspaces[parts[3]] = true
			}
		}
		for workspace := range workspaces {
			if err := registry.reserve("run", "project", legacyRunID(campaign, workspace),
				"active/"+campaign+"/subagents/"+workspace); err != nil {
				return err
			}
		}
		if err := registry.reserve("event", "project", migrationEventID(campaign, plan.PlanDigest),
			"migration import event for "+campaign); err != nil {
			return err
		}

		generatedIntake := 0
		for _, source := range plan.Sources {
			if source.Campaign != campaign {
				continue
			}
			switch source.Role {
			case "normalized-finding":
				body, readErr := readMigrationSource(engine.ProjectRoot, source)
				if readErr != nil {
					return readErr
				}
				document, parseErr := parseMigrationCompatibleFindingDocument(body, source.Destination)
				if parseErr != nil {
					return fmt.Errorf("reserve pre-normalized finding %s: %w", source.Path, parseErr)
				}
				if err := registry.reserve("finding", "project", document.Record.ID, source.Path); err != nil {
					return err
				}
			case "intake":
				body, readErr := readMigrationSource(engine.ProjectRoot, source)
				if readErr != nil {
					return readErr
				}
				var record IntakeRecord
				if err := decodeStrictJSON(body, &record); err != nil {
					return fmt.Errorf("reserve intake %s: %w", source.Path, err)
				}
				if err := registry.reserve("intake", campaignID, record.ID, source.Path); err != nil {
					return err
				}
			case "review-receipt":
				body, readErr := readMigrationSource(engine.ProjectRoot, source)
				if readErr != nil {
					return readErr
				}
				var record ReviewRecord
				if err := decodeStrictJSON(body, &record); err != nil {
					return fmt.Errorf("reserve review %s: %w", source.Path, err)
				}
				if err := registry.reserve("review", campaignID, record.ID, source.Path); err != nil {
					return err
				}
			case "legacy-run-report":
				if !live[campaign] {
					continue
				}
				receipt, coverageErr := engine.coverageFor(source.Path)
				if coverageErr != nil {
					return coverageErr
				}
				for _, finding := range receipt.Findings {
					if err := registry.reserve("finding", "project", finding.ID, "coverage "+source.Path); err != nil {
						return err
					}
				}
				if len(receipt.Findings) > 0 || migrationCoverageHasUnresolved(receipt) {
					generatedIntake++
					id := fmt.Sprintf("I-%04d", generatedIntake)
					if err := registry.reserve("intake", campaignID, id, "coverage "+source.Path); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, truth := range plan.TruthConversions {
		if err := registry.reserve("finding", "project", truth.FindingID, "truth "+truth.SourcePath); err != nil {
			return err
		}
		if err := registry.reserve("receipt", "project", "truth:"+truth.FindingID, "truth "+truth.SourcePath); err != nil {
			return err
		}
	}
	for _, operation := range plan.Operations {
		for _, destination := range SortedUnique(append(append([]string{}, operation.Destinations...), operation.Destination)) {
			id := operation.ID + ":" + strings.TrimPrefix(SHA256String(destination), "sha256:")[:16]
			if err := registry.reserve("receipt", "project", id, "materialization "+destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func (engine *MigrationEngine) verifyMigrationPlanMaterialization(
	plan MigrationPlan,
	stagedRoot string,
) ([]MigrationMaterializationReceipt, error) {
	sources := map[string]MigrationSource{}
	for _, source := range plan.Sources {
		sources[source.Path] = source
	}
	targets := migrationManagedTargets(plan)
	isStaged := func(path string) bool {
		for _, target := range targets {
			if path == target || strings.HasPrefix(path, target+"/") {
				return true
			}
		}
		return false
	}
	receipts := []MigrationMaterializationReceipt{}
	seen := map[string]bool{}
	for _, operation := range plan.Operations {
		if len(operation.Sources) != 1 {
			return nil, fmt.Errorf("migration operation %s must bind exactly one source", operation.ID)
		}
		source, ok := sources[operation.Sources[0]]
		if !ok || operation.InputDigest != source.SHA256 {
			return nil, fmt.Errorf("migration operation %s is not bound to its inventoried source", operation.ID)
		}
		destinations := SortedUnique(append(append([]string{}, operation.Destinations...), operation.Destination))
		if len(destinations) == 0 {
			return nil, fmt.Errorf("migration operation %s has no materialized destination", operation.ID)
		}
		for _, destination := range destinations {
			key := operation.ID + "\x00" + destination
			if seen[key] {
				continue
			}
			seen[key] = true
			path := filepath.Join(engine.ProjectRoot, filepath.FromSlash(destination))
			mode := "retained-in-place"
			if isStaged(destination) {
				path = filepath.Join(stagedRoot, filepath.FromSlash(destination))
				mode = "staged-transformed"
			} else if operation.Kind != "retain" || destination != source.Path {
				return nil, fmt.Errorf("migration operation %s leaves non-retained destination %s outside the staged cutover", operation.ID, destination)
			}
			info, statErr := os.Lstat(path)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("migration operation %s did not materialize regular destination %s", operation.ID, destination)
			}
			destinationDigest, digestErr := digestMigrationPath(path)
			if digestErr != nil {
				return nil, digestErr
			}
			exact := migrationDestinationMustBeByteExact(source, destination)
			if destinationDigest == "sha256:"+source.SHA256 {
				if isStaged(destination) {
					stamp := time.Unix(0, source.MtimeNS).UTC()
					if err := os.Chtimes(path, stamp, stamp); err != nil {
						return nil, fmt.Errorf("preserve timestamp for %s: %w", destination, err)
					}
					info, statErr = os.Lstat(path)
					if statErr != nil {
						return nil, statErr
					}
					mode = "staged-byte-exact"
				}
				if exact && info.ModTime().UTC().UnixNano() != source.MtimeNS {
					return nil, fmt.Errorf("migration operation %s changed source timestamp at %s", operation.ID, destination)
				}
			} else if exact {
				return nil, fmt.Errorf("migration operation %s changed byte-exact destination %s", operation.ID, destination)
			}
			receipt := MigrationMaterializationReceipt{
				OperationID: operation.ID, SourcePath: source.Path,
				SourceDigest: "sha256:" + source.SHA256, SourceMtimeNS: source.MtimeNS,
				Destination: destination, DestinationDigest: destinationDigest,
				DestinationMtimeNS: info.ModTime().UTC().UnixNano(), Mode: mode,
			}
			receipt.Digest, digestErr = CanonicalDigest(receipt)
			if digestErr != nil {
				return nil, digestErr
			}
			receipts = append(receipts, receipt)
		}
	}
	sort.Slice(receipts, func(i, j int) bool {
		if receipts[i].OperationID == receipts[j].OperationID {
			return receipts[i].Destination < receipts[j].Destination
		}
		return receipts[i].OperationID < receipts[j].OperationID
	})
	return receipts, nil
}

func (engine *MigrationEngine) verifyPublishedMigrationMaterializations(
	plan MigrationPlan,
	receipts []MigrationMaterializationReceipt,
) error {
	sources := map[string]MigrationSource{}
	operations := map[string]MigrationOperation{}
	expected := map[string]bool{}
	for _, source := range plan.Sources {
		sources[source.Path] = source
	}
	for _, operation := range plan.Operations {
		operations[operation.ID] = operation
		for _, destination := range SortedUnique(append(append([]string{}, operation.Destinations...), operation.Destination)) {
			expected[operation.ID+"\x00"+destination] = true
		}
	}
	seen := map[string]bool{}
	for _, receipt := range receipts {
		key := receipt.OperationID + "\x00" + receipt.Destination
		operation, ok := operations[receipt.OperationID]
		if !ok || !expected[key] || seen[key] || len(operation.Sources) != 1 {
			return errors.New("migration materialization receipt set is duplicated or plan-mismatched")
		}
		seen[key] = true
		source, ok := sources[operation.Sources[0]]
		if !ok || receipt.SourcePath != source.Path || receipt.SourceDigest != "sha256:"+source.SHA256 ||
			receipt.SourceMtimeNS != source.MtimeNS ||
			!validOne(receipt.Mode, "staged-byte-exact", "staged-transformed", "retained-in-place") {
			return fmt.Errorf("migration materialization receipt for %s is source-mismatched", receipt.Destination)
		}
		expectedDigest := receipt.Digest
		receipt.Digest = ""
		digest, err := CanonicalDigest(receipt)
		if err != nil || digest != expectedDigest {
			return fmt.Errorf("migration materialization receipt for %s has an invalid digest", receipt.Destination)
		}
		actual, err := digestMigrationPath(filepath.Join(engine.ProjectRoot, filepath.FromSlash(receipt.Destination)))
		if err != nil || actual != receipt.DestinationDigest {
			return fmt.Errorf("published migration destination %s does not match its receipt", receipt.Destination)
		}
		exact := migrationDestinationMustBeByteExact(source, receipt.Destination)
		if exact && actual != "sha256:"+source.SHA256 {
			return fmt.Errorf("published byte-exact migration destination %s changed its source", receipt.Destination)
		}
		if exact {
			publishedInfo, statErr := os.Lstat(filepath.Join(engine.ProjectRoot, filepath.FromSlash(receipt.Destination)))
			if statErr != nil || publishedInfo.ModTime().UTC().UnixNano() != receipt.DestinationMtimeNS ||
				receipt.DestinationMtimeNS != source.MtimeNS {
				return fmt.Errorf("published byte-exact migration destination %s changed its source timestamp", receipt.Destination)
			}
		}
	}
	if len(seen) != len(expected) {
		return errors.New("migration materialization receipts omit a planned destination")
	}
	return nil
}

// migrationDestinationMustBeByteExact distinguishes transformed canonical
// records from frozen provenance carried by the same operation. The generic
// materialization verifier uses this plan-derived rule instead of trusting a
// producer-selected receipt mode.
func migrationDestinationMustBeByteExact(source MigrationSource, destination string) bool {
	if source.Role == "legacy-retrieval-profile" && destination == migrationProfileAuditDecisionPath(source.Path) {
		return false
	}
	if source.Disposition == "retain" || source.Disposition == "retain-as-provenance" {
		return true
	}
	if destination == source.Destination {
		return false
	}
	return strings.Contains(destination, "/payload/legacy/")
}

// sealMigrationImportChain gives the activated tree one deterministic global
// genesis history. Each event commits every byte for its campaign, while the
// final event additionally commits the complete non-event project snapshot
// and all prior import-event digests. A head that reaches the final event can
// therefore never represent only the lexicographically last campaign.
func sealMigrationImportChain(
	projectRoot string,
	campaigns []string,
	plan MigrationPlan,
	state MigrationState,
	legacySources map[string]string,
) error {
	allObjects, err := migrationImportObjects(projectRoot, "")
	if err != nil {
		return err
	}
	events, err := buildMigrationImportEvents(allObjects, campaigns, plan, state, legacySources)
	if err != nil {
		return err
	}
	for index, campaign := range campaigns {
		body, err := json.Marshal(events[index])
		if err != nil {
			return err
		}
		body = append(body, '\n')
		journal := filepath.Join(projectRoot, "active", campaign, "events", "events.jsonl")
		if err := AtomicWrite(journal, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func buildMigrationImportEvents(
	allObjects []migrationImportObject,
	campaigns []string,
	plan MigrationPlan,
	state MigrationState,
	legacySources map[string]string,
) ([]StateEvent, error) {
	globalSnapshotDigest, err := CanonicalDigest(allObjects)
	if err != nil {
		return nil, err
	}
	legacyBindings := make([]migrationSourceBinding, 0, len(legacySources))
	for source, destination := range legacySources {
		legacyBindings = append(legacyBindings, migrationSourceBinding{Source: source, Destination: destination})
	}
	sort.Slice(legacyBindings, func(i, j int) bool { return legacyBindings[i].Source < legacyBindings[j].Source })

	previousStateDigest := initialStateHead().StateDigest
	previousEventID := ""
	priorEventDigests := []string{}
	events := make([]StateEvent, 0, len(campaigns))
	for index, campaign := range campaigns {
		prefix := filepath.ToSlash(filepath.Join("active", campaign)) + "/"
		campaignObjects := make([]migrationImportObject, 0)
		for _, object := range allObjects {
			if strings.HasPrefix(object.Path, prefix) {
				campaignObjects = append(campaignObjects, object)
			}
		}
		event := StateEvent{
			SchemaVersion:       CampaignSchemaVersion,
			ID:                  migrationEventID(campaign, plan.PlanDigest),
			Timestamp:           state.UpdatedAt,
			Actor:               state.Actor,
			Authority:           "manager",
			Action:              "migration.import",
			PreviousRevision:    int64(index),
			ResultingRevision:   int64(index + 1),
			PreviousEventID:     previousEventID,
			PreviousStateDigest: previousStateDigest,
			IdempotencyKey:      "migration:" + plan.PlanID + ":" + campaign,
			CorrelationID:       state.TransactionID,
			Rationale:           "Imported from the digest-pinned 0.7 source inventory; no epistemic ratification occurred.",
		}
		for _, object := range campaignObjects {
			event.AffectedIDs = append(event.AffectedIDs, object.Path)
		}
		mutation := struct {
			Campaign             string                   `json:"campaign"`
			PlanDigest           string                   `json:"planDigest"`
			TransactionID        string                   `json:"transactionId"`
			Objects              []migrationImportObject  `json:"objects"`
			LegacySourceBindings []migrationSourceBinding `json:"legacySourceBindings,omitempty"`
			GlobalSnapshotDigest string                   `json:"globalSnapshotDigest,omitempty"`
			PriorEventDigests    []string                 `json:"priorEventDigests,omitempty"`
		}{
			Campaign: campaign, PlanDigest: plan.PlanDigest,
			TransactionID: state.TransactionID, Objects: campaignObjects,
		}
		if index == len(campaigns)-1 {
			mutation.LegacySourceBindings = legacyBindings
			mutation.GlobalSnapshotDigest = globalSnapshotDigest
			mutation.PriorEventDigests = append([]string(nil), priorEventDigests...)
		}
		event.MutationDigest, err = CanonicalDigest(mutation)
		if err != nil {
			return nil, err
		}
		event.ResultingStateDigest, err = CanonicalDigest(struct {
			Previous string `json:"previous"`
			Mutation string `json:"mutation"`
		}{event.PreviousStateDigest, event.MutationDigest})
		if err != nil {
			return nil, err
		}
		if err := setEventDigest(&event); err != nil {
			return nil, err
		}
		events = append(events, event)
		previousStateDigest = event.ResultingStateDigest
		previousEventID = event.ID
		priorEventDigests = append(priorEventDigests, event.Digest)
	}
	return events, nil
}

func migrationImportObjects(root, prefix string) ([]migrationImportObject, error) {
	tree, err := digestRegularTree(root)
	if err != nil {
		return nil, err
	}
	objects := make([]migrationImportObject, 0, len(tree))
	for relative, digest := range tree {
		if prefix != "" && !strings.HasPrefix(relative, prefix) {
			continue
		}
		if strings.HasSuffix(relative, "/events/events.jsonl") {
			continue
		}
		objects = append(objects, migrationImportObject{Path: relative, SHA256: "sha256:" + digest})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
	return objects, nil
}

func resetMigrationStaging(migrationRoot, stagingRoot string) error {
	root, err := filepath.Abs(migrationRoot)
	if err != nil {
		return err
	}
	staging, err := filepath.Abs(stagingRoot)
	if err != nil {
		return err
	}
	if !withinRoot(root, staging) || filepath.Clean(root) == filepath.Clean(staging) {
		return errors.New("migration staging path escapes its transaction root")
	}
	if info, err := os.Lstat(staging); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("migration staging path is unsafe")
		}
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(staging, 0o700)
}

var migrationExactManagedFiles = map[string]string{
	".re-discipline/config.json":            "bootstrap-config",
	".re-discipline/knowledge/policy.jsonc": "knowledge-policy",
	".re-discipline/project-profile.md":     "shared-laws",
	"AGENTS.md":                             "router",
	".claude/CLAUDE.md":                     "claude-adapter",
	".claude/settings.json":                 "claude-settings",
	".codex/AGENTS.md":                      "codex-adapter",
	".codex/config.toml":                    "codex-settings",
	".codex/external-drafter-contract.md":   "external-drafter-contract",
	"docs/INDEX.md":                         "navigation-index",
}

// migrationManagedTargets are published independently so project-owned docs
// outside truth/history/backlog and host-owned settings outside the adapter
// files cannot be overwritten by migration.
func migrationManagedTargets(plan MigrationPlan) []string {
	targets := []string{
		"active", ".re-discipline/config.json", ".re-discipline/project-profile.md",
		".re-discipline/knowledge", stateInventoryPath,
	}
	for path := range migrationExactManagedFiles {
		if path == ".re-discipline/config.json" || path == ".re-discipline/project-profile.md" ||
			path == ".re-discipline/knowledge/policy.jsonc" {
			continue
		}
		if migrationPlanHasPath(plan, path) {
			targets = append(targets, path)
		}
	}
	for _, root := range []string{
		".re-discipline/agents",
		"docs/truth", "docs/history", "docs/backlog",
	} {
		if migrationPlanHasPrefix(plan, root) {
			targets = append(targets, root)
		}
	}
	return SortedUnique(targets)
}

// migrationStateInventoryForRoot derives the exact canonical byte inventory
// that the imported genesis head will bind. It intentionally follows the
// normal StateStore ownership contract: typed records, event journals,
// durable truth/archive objects, and every report/context/payload handle
// reachable from a run record are committed; generated STATE views, caches,
// receipts, and unrelated project documentation are not.
func migrationStateInventoryForRoot(root string, revision int64) (StateInventory, error) {
	tree, err := migrationCanonicalTree(root)
	if err != nil {
		return StateInventory{}, err
	}
	entries := map[string]string{}
	for relative, digest := range tree {
		if canonicalInventoryCandidatePath(relative) {
			entries[relative] = "sha256:" + digest
		}
	}

	for relative := range tree {
		parts := strings.Split(relative, "/")
		if len(parts) != 5 || parts[0] != "active" || parts[2] != "runs" || parts[4] != "run.json" {
			continue
		}
		body, err := readSingleLinkRegularFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return StateInventory{}, fmt.Errorf("read migration inventory run %s: %w", relative, err)
		}
		var run RunRecord
		if err := decodeStrictJSON(body, &run); err != nil {
			return StateInventory{}, fmt.Errorf("decode migration inventory run %s: %w", relative, err)
		}
		if run.ID != parts[3] {
			return StateInventory{}, fmt.Errorf("migration inventory run %s has mismatched identity %s", relative, run.ID)
		}
		runRoot := path.Join("active", parts[1], "runs", parts[3])
		addHandle := func(relative, expected string) error {
			clean := path.Clean(relative)
			if clean == "." || clean == runRoot || !strings.HasPrefix(clean, runRoot+"/") ||
				validateRelativeRecordPath(clean) != nil || !digestRE.MatchString(expected) {
				return fmt.Errorf("migration run %s contains an unsafe inventory handle %q", run.ID, relative)
			}
			actual, ok := tree[clean]
			if !ok || expected != "sha256:"+actual {
				return fmt.Errorf("migration run %s inventory handle %s does not resolve to its frozen bytes", run.ID, clean)
			}
			entries[clean] = expected
			return nil
		}
		for _, handle := range []*FileHandle{run.Brief, run.ContextPack, run.Report} {
			if handle != nil {
				if err := addHandle(handle.Path, handle.SHA256); err != nil {
					return StateInventory{}, err
				}
			}
		}
		for _, file := range run.Files {
			if err := addHandle(path.Join(runRoot, file.Path), file.SHA256); err != nil {
				return StateInventory{}, err
			}
		}
	}
	return stateInventoryFromMap(revision, entries)
}

func migrationCanonicalTree(root string) (map[string]string, error) {
	tree := map[string]string{}
	for _, relativeRoot := range []string{"active", "docs/truth", "docs/history/campaigns"} {
		absolute := filepath.Join(root, filepath.FromSlash(relativeRoot))
		info, err := os.Lstat(absolute)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("migration canonical inventory root %s is unsafe", relativeRoot)
		}
		children, err := digestRegularTree(absolute)
		if err != nil {
			return nil, err
		}
		for child, digest := range children {
			tree[path.Join(relativeRoot, child)] = digest
		}
	}
	return tree, nil
}

func loadVerifiedMigrationStateInventory(root string, revision int64) (StateInventory, error) {
	body, err := readSingleLinkRegularFile(filepath.Join(root, filepath.FromSlash(stateInventoryPath)))
	if err != nil {
		return StateInventory{}, err
	}
	var inventory StateInventory
	if err := decodeStrictJSON(body, &inventory); err != nil {
		return StateInventory{}, err
	}
	if err := verifyStateInventory(inventory); err != nil || inventory.HeadRevision != revision {
		return StateInventory{}, errors.New("activated migration state inventory is invalid or revision-mismatched")
	}
	canonical, err := canonicalJSON(inventory)
	if err != nil || SHA256Bytes(canonical) != SHA256Bytes(body) {
		return StateInventory{}, errors.New("activated migration state inventory is not canonical")
	}
	expected, err := migrationStateInventoryForRoot(root, revision)
	if err != nil {
		return StateInventory{}, err
	}
	if expected.Digest != inventory.Digest {
		return StateInventory{}, errors.New("activated migration state inventory does not cover the canonical snapshot")
	}
	return inventory, nil
}

func migrationPlanHasPath(plan MigrationPlan, path string) bool {
	for _, source := range plan.Sources {
		if source.Path == path {
			return true
		}
	}
	return false
}

func migrationPlanHasPrefix(plan MigrationPlan, prefix string) bool {
	for _, source := range plan.Sources {
		if source.Path == prefix || strings.HasPrefix(source.Path, prefix+"/") {
			return true
		}
	}
	return false
}

func (engine *MigrationEngine) stageManagedProject(plan MigrationPlan, stagingRoot string) error {
	migratedConfig := DefaultBootstrapConfig()
	for _, source := range plan.Sources {
		if source.Path != ".re-discipline/config.json" {
			continue
		}
		legacy, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		body, err := migratedBootstrapConfiguration(legacy)
		if err != nil {
			return err
		}
		if err := decodeStrict(body, &migratedConfig); err != nil {
			return err
		}
		break
	}
	nativeMemoryEnabled := migratedConfig.Memory.Mode != "shared-only"
	for _, source := range plan.Sources {
		if source.Campaign != "" || strings.HasPrefix(source.Path, "active/") {
			continue
		}
		if source.Role == "truth" {
			// Legacy truth is converted below into a typed truth finding plus
			// exact frozen provenance and a compatibility receipt.
			continue
		}
		if !migrationStageSource(source.Path) {
			continue
		}
		body, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		if kind, managed := migrationExactManagedFiles[source.Path]; managed {
			switch kind {
			case "bootstrap-config":
				body, err = migratedBootstrapConfiguration(body)
			case "knowledge-policy":
				body, err = migratedKnowledgePolicy(body)
			case "shared-laws", "router", "claude-adapter", "codex-adapter":
				body, err = upgradeManagedDocument(body, kind)
			case "claude-settings":
				body, err = reconcileClaudeMemoryPolicy(body, nativeMemoryEnabled)
			case "codex-settings":
				body, err = reconcileCodexMemoryPolicy(body, nativeMemoryEnabled)
			case "external-drafter-contract":
				body, err = upgradeExternalDrafterContract(body)
			case "navigation-index":
				body, err = upgradeMigrationNavigation(body)
			}
			if err != nil {
				return fmt.Errorf("transform %s: %w", source.Path, err)
			}
		}
		if source.Path == ".re-discipline/agents/dispatch.ps1" {
			body = []byte(strings.TrimSpace(migrationDispatchTemplate) + "\n")
		}
		destination := filepath.Join(stagingRoot, filepath.FromSlash(source.Destination))
		if err := AtomicWrite(destination, body, 0o600); err != nil {
			return err
		}
	}
	if plan.ProfileDecision != nil {
		boundary, err := NewBoundary(engine.ProjectRoot)
		if err != nil {
			return err
		}
		packet, err := buildMigrationProfileConflictPacket(boundary, plan.Sources)
		if err != nil {
			return fmt.Errorf("rebuild profile conflict packet for staging: %w", err)
		}
		decision, err := loadMigrationProfileDecision(engine.ProjectRoot, packet)
		if err != nil || decision.Digest != plan.ProfileDecision.Digest {
			return errors.New("sealed profile conversion decision changed after plan approval")
		}
		decisionBody, err := readSingleLinkRegularFile(migrationProfileDecisionPath(engine.ProjectRoot, decision.SourcePath))
		if err != nil {
			return err
		}
		if err := AtomicWrite(
			filepath.Join(stagingRoot, filepath.FromSlash(migrationProfileAuditDecisionPath(decision.SourcePath))),
			decisionBody,
			0o600,
		); err != nil {
			return err
		}
	}
	// Bootstrap configuration is a required generated destination even when a
	// malformed legacy file was absent from the inventory.
	configPath := filepath.Join(stagingRoot, ".re-discipline", "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		body, marshalErr := json.MarshalIndent(DefaultBootstrapConfig(), "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		body = append(body, '\n')
		if err := AtomicWrite(configPath, body, 0o600); err != nil {
			return err
		}
	}
	settingsPath := filepath.Join(stagingRoot, ".re-discipline", "knowledge", "policy.jsonc")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		body, marshalErr := json.MarshalIndent(DefaultKnowledgeSettings(), "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		body = append(body, '\n')
		if err := AtomicWrite(settingsPath, body, 0o600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(stagingRoot, ".re-discipline", "knowledge", "evals"), 0o700); err != nil {
		return err
	}
	return nil
}

func (engine *MigrationEngine) stageLegacyTruthConversions(
	plan MigrationPlan,
	state MigrationState,
	projectRoot string,
	campaigns []string,
	manifest *MigrationNormalizedManifest,
) error {
	truthSources := []MigrationSource{}
	for _, source := range plan.Sources {
		if source.Role == "truth" {
			truthSources = append(truthSources, source)
		}
	}
	if len(truthSources) == 0 {
		return nil
	}
	if len(campaigns) == 0 {
		return errors.New("legacy truth conversion requires a canonical migration provenance campaign")
	}
	carrier := campaigns[0]
	runID := legacyRunID(carrier, "campaign-import")
	runPath := filepath.Join(projectRoot, "active", carrier, "runs", runID, "run.json")
	var run RunRecord
	runBody, err := os.ReadFile(runPath)
	if err != nil || decodeStrictJSON(runBody, &run) != nil {
		return fmt.Errorf("load truth migration provenance run: %w", err)
	}
	sourceIDs := map[string][]string{}
	truthDestinations := map[string]string{}
	destinationIDs := map[string]string{}
	for _, source := range truthSources {
		truthDestinations[source.Path] = source.Destination
	}
	plans := map[string][]MigrationTruthPlan{}
	for _, truthPlan := range plan.TruthConversions {
		if prior := destinationIDs[truthPlan.Destination]; prior != "" && prior != truthPlan.FindingID {
			return fmt.Errorf("legacy truth destination collision between %s and %s", prior, truthPlan.FindingID)
		}
		destinationIDs[truthPlan.Destination] = truthPlan.FindingID
		sourceIDs[truthPlan.SourcePath] = append(sourceIDs[truthPlan.SourcePath], truthPlan.FindingID)
		plans[truthPlan.SourcePath] = append(plans[truthPlan.SourcePath], truthPlan)
	}
	boundary, err := NewBoundary(engine.ProjectRoot)
	if err != nil {
		return err
	}
	expectedPlans, expectedConflicts, err := migrationTruthPlans(boundary, plan.Sources)
	if err != nil {
		return err
	}
	if countBlockingConflicts(expectedConflicts) != 0 || !reflect.DeepEqual(expectedPlans, plan.TruthConversions) {
		return errors.New("legacy truth conversion rows no longer match the digest-bound manager plan")
	}
	for _, source := range truthSources {
		body, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		rows := plans[source.Path]
		if len(rows) == 0 {
			return fmt.Errorf(
				"legacy truth %s lacks an exact manager-approved conversion plan (planRows=%d mappedSources=%d)",
				source.Path, len(plan.TruthConversions), len(plans))
		}
		for index, truthPlan := range rows {
			// Name the exact field that diverged. A generic "lacks a plan"
			// message sent readers hunting for a missing review when the plan
			// was present and one bound value disagreed.
			mismatch := ""
			switch {
			case truthPlan.SourceDigest != "sha256:"+source.SHA256:
				mismatch = "source digest"
			case truthPlan.SplitIndex != index+1:
				mismatch = "split index"
			case truthPlan.SplitCount != len(rows):
				mismatch = "split count"
			case truthPlan.Claim == "" || len([]rune(truthPlan.Claim)) > 500:
				mismatch = "atomic claim bound"
			case !reflect.DeepEqual(
				truthPlan.LegacyDependencies, legacyTruthDependencyPaths(body, source.Path)):
				mismatch = "legacy dependency set"
			case len(truthPlan.SyntheticQuestions) < SyntheticQuestionMinimum:
				mismatch = "reviewed question count"
			}
			if mismatch != "" {
				return fmt.Errorf(
					"legacy truth %s conversion row %d does not match its manager-approved plan: %s",
					source.Path, index+1, mismatch)
			}
		}
	}
	for _, source := range truthSources {
		body, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		provenanceRelative := filepath.ToSlash(filepath.Join("active", carrier, "runs", runID,
			"payload", "legacy", "truth", strings.TrimPrefix(source.Path, "docs/truth/")))
		if err := AtomicWrite(filepath.Join(projectRoot, filepath.FromSlash(provenanceRelative)), body, 0o600); err != nil {
			return err
		}
		manifest.LegacySources[source.Path] = provenanceRelative
		runRelative := strings.TrimPrefix(provenanceRelative,
			filepath.ToSlash(filepath.Join("active", carrier, "runs", runID))+"/")
		run.Files = append(run.Files, RunFile{
			Path: runRelative, MediaKind: "text", SemanticRole: "reference-copy",
			Retention: "retain-inline", SHA256: "sha256:" + source.SHA256,
		})

		dependencyMap, relationIDs, err := legacyTruthDependencies(body, source.Path, sourceIDs, truthDestinations, projectRoot)
		if err != nil {
			return fmt.Errorf("legacy truth %s: %w", source.Path, err)
		}
		rows := plans[source.Path]
		if rows[0].ReviewDigest != "" {
			reviewBody, readErr := readSingleLinkRegularFile(migrationTruthReviewPath(engine.ProjectRoot, source.Path))
			if readErr != nil {
				return fmt.Errorf("read truth atomicization review for %s: %w", source.Path, readErr)
			}
			if err := AtomicWrite(filepath.Join(projectRoot, filepath.FromSlash(migrationTruthAuditReviewPath(source.Path))), reviewBody, 0o600); err != nil {
				return err
			}
		}
		createdAt := state.CreatedAt
		if createdAt == "" {
			createdAt = state.UpdatedAt
		}
		for _, truthPlan := range rows {
			id := truthPlan.FindingID
			title := truthPlan.Title
			claim := truthPlan.Claim
			scope := map[string]any{"legacyTruthPath": source.Path, "authority": "accepted-pre-0.8",
				"sourceText": truthPlan.SourceText, "splitIndex": truthPlan.SplitIndex, "splitCount": truthPlan.SplitCount}
			if len(truthPlan.LegacyScope) > 0 {
				scope["legacyScope"] = append([]string(nil), truthPlan.LegacyScope...)
			}
			if truthPlan.LegacyStatus != "" {
				scope["legacyStatus"] = truthPlan.LegacyStatus
			}
			if truthPlan.LegacyCorrection != "" {
				scope["legacyCorrection"] = truthPlan.LegacyCorrection
			}
			appliesWhen := append([]string(nil), truthPlan.LegacyScope...)
			if len(appliesWhen) == 0 {
				appliesWhen = []string{"Only the exact bounded claim and provenance accepted at " + source.Path + "; migration inferred no broader scope."}
			}
			knownLimits := append([]string(nil), truthPlan.LegacyExclusions...)
			knownLimits = append(knownLimits, "Migration preserves prior truth authority and original confidence "+truthPlan.LegacyConfidence+"; it does not ratify any source report or increase confidence.")
			if truthPlan.LegacyCorrection != "" {
				knownLimits = append(knownLimits, "Legacy correction relation: "+truthPlan.LegacyCorrection)
			}
			validity, projection := migrationLegacyTruthState(truthPlan.LegacyStatus)
			document := FindingDocument{Record: FindingRecord{
				SchemaVersion: CampaignSchemaVersion, ID: id, CampaignID: "C-" + strings.ToUpper(carrier),
				Revision: 1, CreatedAt: createdAt, UpdatedAt: state.UpdatedAt,
				CreatedBy: "migration:accepted-truth", UpdatedBy: "migration:" + state.Actor,
				CorrelationID: state.TransactionID, Kind: "conclusion", Subject: title, Claim: claim,
				Scope: scope, AppliesWhen: appliesWhen, KnownLimits: knownLimits,
				Aliases: []string{title}, SourceRuns: []string{runID},
				Evidence: []EvidenceReference{{Path: provenanceRelative, SHA256: "sha256:" + source.SHA256,
					ObjectKey: "legacy-truth:" + source.Path, SourceRun: runID}},
				Relations: FindingRelations{DependsOn: relationIDs}, EvidenceGrade: "direct",
				ReviewState: "manager-ratified", Validity: validity, Projection: projection,
				Path:     truthPlan.Destination,
				PolicyID: "migration:legacy-truth-preservation-v1", VerifiedAt: truthPlan.LegacyVerifiedAt,
				Body: renderMigratedTruthFindingBody(claim, source.Path, provenanceRelative, dependencyMap),
			}, SyntheticQuestions: append([]string(nil), truthPlan.SyntheticQuestions...), QuestionsReviewed: true}
			destinationBody, err := RenderFindingDocument(document)
			if err != nil {
				return fmt.Errorf("render migrated truth %s split %d: %w", source.Path, truthPlan.SplitIndex, err)
			}
			if err := AtomicWrite(filepath.Join(projectRoot, filepath.FromSlash(truthPlan.Destination)), destinationBody, 0o600); err != nil {
				return err
			}
			receipt := MigrationTruthCompatibilityReceipt{
				SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID, PlanDigest: plan.PlanDigest,
				TruthID: "T-" + id, FindingID: id, SourcePath: source.Path, SourceDigest: "sha256:" + source.SHA256,
				Destination: truthPlan.Destination, DestinationDigest: "sha256:" + SHA256Bytes(destinationBody),
				ProvenancePath: provenanceRelative, ProvenanceDigest: "sha256:" + source.SHA256,
				SourceText: truthPlan.SourceText, Claim: claim, ClaimDigest: truthPlan.ClaimDigest,
				LegacyConfidence: truthPlan.LegacyConfidence, LegacyStatus: truthPlan.LegacyStatus,
				LegacyCorrection: truthPlan.LegacyCorrection, LegacyScope: append([]string{}, truthPlan.LegacyScope...),
				LegacyExclusions: append([]string{}, truthPlan.LegacyExclusions...), AtomicizationReviewDigest: truthPlan.ReviewDigest,
				SplitIndex: truthPlan.SplitIndex, SplitCount: truthPlan.SplitCount,
				ScopePreservation:       "exact-source-provenance-no-expansion",
				ApprovedQuestionsDigest: mustCanonicalDigest(truthPlan.SyntheticQuestions),
				ManagerReviewBasis:      "approved-migration-plan:" + plan.PlanDigest,
				DependencyMap:           dependencyMap, SearchTerms: SortedUnique([]string{title, claim}),
				SemanticPreserved: true, EvidenceReachable: true, ScopeNotExpanded: true,
				DependenciesResolve: true, SearchCompatibility: true,
				SourceReportRatificationImported: false, Status: "passed",
			}
			receipt.Digest, err = CanonicalDigest(receipt)
			if err != nil {
				return err
			}
			receiptPath := filepath.Join(projectRoot, ".re-discipline", "knowledge", "migration", "truth-receipts", id+".json")
			if err := AtomicWriteJSON(receiptPath, receipt, 0o600); err != nil {
				return err
			}
		}
		if len(rows) > 1 {
			manifestBody := renderMigratedTruthSplitManifest(source, provenanceRelative, rows)
			if err := AtomicWrite(filepath.Join(projectRoot, filepath.FromSlash(source.Destination)), manifestBody, 0o600); err != nil {
				return err
			}
		}
	}
	run.Files = normalizeRunFiles(run.Files)
	runBody, err = sealMigratedRecord(&run)
	if err != nil {
		return err
	}
	if err := AtomicWrite(runPath, runBody, 0o600); err != nil {
		return err
	}
	return rewriteMigratedTruthLinks(projectRoot, plan)
}

func mustCanonicalDigest(value any) string {
	digest, _ := CanonicalDigest(value)
	return digest
}

func normalizeRunFiles(files []RunFile) []RunFile {
	seen := map[string]RunFile{}
	for _, file := range files {
		seen[file.Path] = file
	}
	result := make([]RunFile, 0, len(seen))
	for _, file := range seen {
		result = append(result, file)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func legacyTruthAtomicClaim(body []byte) string {
	// Match the reviewed claim extractor, which normalizes line endings
	// before parsing; a CRLF checkout otherwise yields a longer claim here
	// than the conflict detector measured.
	body = []byte(normalizeDocumentLineEndings(string(body)))
	if match := preludeClaimRE.FindSubmatch(body); len(match) > 1 {
		claim := normalizePreludeField(string(match[1]))
		if claim != "" {
			return claim
		}
	}
	return ""
}

var legacyTruthDependencyRE = regexp.MustCompile(`(?:\.\./)*docs/(?:truth|history|backlog)/[A-Za-z0-9._/-]+\.md`)

func legacyTruthDependencies(
	body []byte,
	sourcePath string,
	truthIDs map[string][]string,
	truthDestinations map[string]string,
	projectRoot string,
) (map[string]string, []string, error) {
	mapping := map[string]string{}
	relations := []string{}
	for _, clean := range legacyTruthDependencyPaths(body, sourcePath) {
		if ids := truthIDs[clean]; len(ids) > 0 {
			mapping[clean] = truthDestinations[clean]
			relations = append(relations, ids...)
			continue
		}
		if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(clean))); err != nil {
			return nil, nil, fmt.Errorf("dependency %s does not resolve", clean)
		}
		mapping[clean] = clean
	}
	return mapping, SortedUnique(relations), nil
}

// legacyTruthDependencyBlock returns the document's declared relation text.
// Bare paths are relations only where the document declares them as such; a
// path-shaped string in ordinary prose -- a table cell recounting which
// document some work would close, for example -- describes a document rather
// than depending on it, and must not become a migration relation.
func legacyTruthDependencyBlock(body string) string {
	lines := strings.Split(normalizeDocumentLineEndings(body), "\n")
	block := []string{}
	collecting := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "**depends-on:**") || lower == "## depends-on" ||
			strings.HasPrefix(lower, "## depends-on ") {
			collecting = true
			block = append(block, trimmed)
			continue
		}
		if !collecting || trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**") {
			collecting = false
			continue
		}
		block = append(block, trimmed)
	}
	return strings.Join(block, "\n")
}

func legacyTruthDependencyPaths(body []byte, sourcePath string) []string {
	dependencies := []string{}
	for _, raw := range legacyTruthDependencyRE.FindAllString(legacyTruthDependencyBlock(string(body)), -1) {
		clean := filepath.ToSlash(filepath.Clean(raw))
		for strings.HasPrefix(clean, "../") {
			clean = strings.TrimPrefix(clean, "../")
		}
		dependencies = append(dependencies, clean)
	}
	for _, match := range migrationMarkdownTargetRE.FindAllStringSubmatch(string(body), -1) {
		if len(match) < 2 {
			continue
		}
		target := strings.Trim(match[1], "<>")
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		target = strings.SplitN(target, "#", 2)[0]
		// A dependency on a truth, history, or backlog document is always a
		// Markdown file. Requiring the suffix keeps decompiled call
		// expressions inside code spans -- which read exactly like a link
		// target, for example vtbl[index](ent) -- from becoming relations.
		if target == "" || !strings.HasSuffix(strings.ToLower(target), ".md") {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(target))))
		if strings.HasPrefix(clean, "docs/truth/") || strings.HasPrefix(clean, "docs/history/") || strings.HasPrefix(clean, "docs/backlog/") {
			dependencies = append(dependencies, clean)
		}
	}
	return SortedUnique(dependencies)
}

var migrationMarkdownTargetRE = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func rewriteMigratedTruthLinks(projectRoot string, plan MigrationPlan) error {
	mapping := map[string]string{}
	for _, source := range plan.Sources {
		if source.Role == "truth" {
			mapping[source.Path] = source.Destination
		}
	}
	if len(mapping) == 0 {
		return nil
	}
	roots := []string{"docs/INDEX.md", "docs/truth", "docs/history", "docs/backlog"}
	for _, relativeRoot := range roots {
		absoluteRoot := filepath.Join(projectRoot, filepath.FromSlash(relativeRoot))
		info, err := os.Lstat(absoluteRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		visit := func(absolute string) error {
			relative, err := filepath.Rel(projectRoot, absolute)
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
			text := rewriteMigratedTruthLinkTargets(string(body), relative, mapping)
			// Deliberately no blind path substitution here. Rewriting every
			// occurrence of a path string also rewrote prose and code spans
			// that merely name a document, which both misstates the record and
			// breaks the byte-for-byte preservation promised for unchanged
			// history and backlog material. Only link targets, handled above,
			// are relations.
			if text != string(body) {
				return AtomicWrite(absolute, []byte(text), 0o600)
			}
			return nil
		}
		if info.Mode().IsRegular() {
			if err := visit(absoluteRoot); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				return visit(path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func renderMigratedTruthSplitManifest(
	source MigrationSource,
	provenancePath string,
	rows []MigrationTruthPlan,
) []byte {
	var builder strings.Builder
	builder.WriteString("# Migrated atomic findings\n\n")
	// Name the preserved provenance rather than the legacy path. The legacy
	// location does not survive activation, so quoting it would leave a
	// generated navigation document pointing at a path that no longer exists.
	builder.WriteString("This accepted truth required a manager-reviewed atomic split. ")
	provenanceRelative, err := filepath.Rel(filepath.Dir(source.Destination), filepath.FromSlash(provenancePath))
	if err != nil {
		provenanceRelative = provenancePath
	}
	builder.WriteString("Its exact source is preserved at [`" + provenancePath + "`](" + filepath.ToSlash(provenanceRelative) + ").\n\n")
	for _, row := range rows {
		relative, err := filepath.Rel(filepath.Dir(source.Destination), filepath.FromSlash(row.Destination))
		if err != nil {
			relative = row.Destination
		}
		builder.WriteString(fmt.Sprintf("- [%s](%s): %s\n", row.Title, filepath.ToSlash(relative), row.Claim))
	}
	return []byte(builder.String())
}

func legacyTruthSyntheticQuestions(title, claim string) []string {
	title = strings.TrimSuffix(strings.TrimSpace(title), "?")
	claimStem := truncateRunes(strings.TrimSuffix(strings.TrimSpace(claim), "."), 140)
	return []string{
		"What accepted truth is recorded by " + title + "?",
		"Which migrated truth establishes " + claimStem + "?",
		"Where is the exact legacy provenance for " + title + "?",
	}
}

func renderMigratedTruthFindingBody(claim, source, provenance string, dependencies map[string]string) string {
	var body strings.Builder
	body.WriteString("# Claim\n")
	body.WriteString(claim + "\n\n## Applies when\n")
	body.WriteString("Within the original accepted scope preserved at `" + provenance + "`.\n\n")
	body.WriteString("## Does not establish\nMigration does not ratify source reports, expand scope, or increase confidence.\n\n")
	body.WriteString("## Evidence\nThe exact pre-0.8 truth bytes are frozen at `" + provenance + "` from `" + source + "`.\n\n")
	body.WriteString("## Reproduction\nOpen the provenance path and compare its digest with the truth compatibility receipt.\n\n")
	body.WriteString("## Relations\n")
	if len(dependencies) == 0 {
		body.WriteString("No explicit legacy dependency link was declared.")
	} else {
		keys := make([]string, 0, len(dependencies))
		for key := range dependencies {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			body.WriteString("- `" + key + "` -> `" + dependencies[key] + "`\n")
		}
	}
	return strings.TrimSpace(body.String())
}

func migrationLegacyTruthState(status string) (validity, projection string) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "superseded":
		return "superseded", "none"
	default:
		// "corrected" means this accepted record corrected an earlier record;
		// it remains current. The exact correction text is retained separately.
		return "current", "truth"
	}
}

func migrationStageSource(path string) bool {
	if _, exact := migrationExactManagedFiles[path]; exact {
		return true
	}
	for _, prefix := range []string{
		".re-discipline/knowledge/", ".re-discipline/agents/",
		"docs/truth/", "docs/history/", "docs/backlog/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func migratedBootstrapConfiguration(legacy []byte) ([]byte, error) {
	configuration := DefaultBootstrapConfig()
	var raw map[string]json.RawMessage
	if len(bytesTrimSpace(legacy)) > 0 && json.Unmarshal(legacy, &raw) == nil {
		if value, ok := raw["memory"]; ok {
			var memory MemoryConfig
			if json.Unmarshal(value, &memory) == nil && memory.Mode != "" && memory.WritePolicy != "" {
				configuration.Memory = memory
			}
		}
		if value, ok := raw["knowledge"]; ok {
			var knowledge KnowledgeConfig
			if json.Unmarshal(value, &knowledge) == nil && knowledge.Profile != "" {
				configuration.Knowledge = knowledge
			}
		}
	}
	body, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

type legacyKnowledgeSettingsV1 struct {
	Schema        string `json:"$schema,omitempty"`
	SchemaVersion int    `json:"schemaVersion"`
	Sources       struct {
		Truth           bool               `json:"truth"`
		History         bool               `json:"history"`
		Backlog         bool               `json:"backlog"`
		ActiveCampaigns bool               `json:"activeCampaigns"`
		SharedMemory    bool               `json:"sharedMemory"`
		DrafterReports  bool               `json:"drafterReports"`
		Additional      []AdditionalSource `json:"additional,omitempty"`
	} `json:"sources"`
	Models    ModelSettings `json:"models"`
	Telemetry Telemetry     `json:"telemetry"`
	Budgets   struct {
		SearchTokens         int `json:"searchTokens"`
		ManagerContextTokens int `json:"managerContextTokens"`
		DrafterContextTokens int `json:"drafterContextTokens"`
		MaxPassages          int `json:"maxPassages"`
		MaxBytes             int `json:"maxBytes"`
	} `json:"budgets"`
}

func migratedKnowledgePolicy(legacy []byte) ([]byte, error) {
	stripped, err := StripJSONComments(legacy)
	if err != nil {
		return nil, err
	}
	var version struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := decodeStrictJSONVersion(stripped, &version); err != nil {
		return nil, err
	}
	var settings KnowledgeSettings
	switch version.SchemaVersion {
	case SettingsSchemaVersion:
		var raw struct {
			Schema string `json:"$schema"`
			KnowledgeSettings
		}
		if err := decodeStrict(stripped, &raw); err != nil {
			return nil, err
		}
		settings = raw.KnowledgeSettings
		settings.Schema = raw.Schema
	case 1:
		var old legacyKnowledgeSettingsV1
		if err := decodeStrict(stripped, &old); err != nil {
			return nil, err
		}
		if err := requireLegacyKnowledgePolicyFields(stripped); err != nil {
			return nil, err
		}
		settings = DefaultKnowledgeSettings()
		settings.Schema = old.Schema
		settings.Sources = SourceSettings{
			Truth: old.Sources.Truth, HistoryFindings: old.Sources.History,
			Backlog: old.Sources.Backlog, ActiveFindings: old.Sources.ActiveCampaigns,
			SharedMemory: old.Sources.SharedMemory, ReportFallback: old.Sources.DrafterReports,
			Additional: append([]AdditionalSource(nil), old.Sources.Additional...),
		}
		settings.Models = old.Models
		settings.Telemetry = old.Telemetry
		settings.Budgets = BudgetSettings{
			SearchTokens: old.Budgets.SearchTokens, ManagerContextTokens: old.Budgets.ManagerContextTokens,
			DrafterContextTokens: old.Budgets.DrafterContextTokens, MaxPassages: old.Budgets.MaxPassages,
			MaxBytes: old.Budgets.MaxBytes,
		}
	default:
		return nil, fmt.Errorf("unsupported knowledge policy schemaVersion %d", version.SchemaVersion)
	}
	settings.SchemaVersion = SettingsSchemaVersion
	settings.Schema = "plugin://re-discipline/schemas/knowledge-settings.schema.json"
	if err := ValidateSettings(settings); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func decodeStrictJSONVersion(body []byte, target any) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	value, ok := raw["schemaVersion"]
	if !ok {
		return errors.New("knowledge policy omits schemaVersion")
	}
	var schemaVersion int
	if err := json.Unmarshal(value, &schemaVersion); err != nil {
		return err
	}
	return json.Unmarshal([]byte(fmt.Sprintf(`{"schemaVersion":%d}`, schemaVersion)), target)
}

func requireLegacyKnowledgePolicyFields(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	for _, field := range []string{"schemaVersion", "sources", "models", "telemetry", "budgets"} {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("legacy knowledge policy omits %s", field)
		}
	}
	var sources map[string]json.RawMessage
	if err := json.Unmarshal(raw["sources"], &sources); err != nil {
		return err
	}
	for _, field := range []string{"truth", "history", "backlog", "activeCampaigns", "sharedMemory", "drafterReports"} {
		if _, ok := sources[field]; !ok {
			return fmt.Errorf("legacy knowledge policy sources omit %s", field)
		}
	}
	for object, fields := range map[string][]string{
		"models": {"execution"}, "telemetry": {"mode"},
		"budgets": {"searchTokens", "managerContextTokens", "drafterContextTokens", "maxPassages", "maxBytes"},
	} {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw[object], &nested); err != nil {
			return err
		}
		for _, field := range fields {
			if _, ok := nested[field]; !ok {
				return fmt.Errorf("legacy knowledge policy %s omit %s", object, field)
			}
		}
	}
	return nil
}

func bytesTrimSpace(value []byte) []byte {
	return []byte(strings.TrimSpace(string(value)))
}

func upgradeManagedDocument(body []byte, kind string) ([]byte, error) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	marker := "re-discipline:" + kind
	re := regexp.MustCompile(`<!--\s*` + regexp.QuoteMeta(marker) + `\s+v[0-9]+\.[0-9]+\.[0-9]+\s*-->`)
	start := re.FindStringIndex(text)
	if start == nil {
		return nil, errors.New("managed block marker is missing")
	}
	endMarker := "<!-- " + marker + ":end -->"
	endOffset := strings.Index(text[start[1]:], endMarker)
	if endOffset < 0 {
		return nil, errors.New("managed block closing marker is missing")
	}
	end := start[1] + endOffset + len(endMarker)
	replacement := migrationManagedBlock(kind)
	if replacement == "" {
		return nil, fmt.Errorf("unsupported managed block %s", kind)
	}
	text = strings.TrimSuffix(text[:start[0]], "\n") + "\n" + replacement + strings.TrimPrefix(text[end:], "\n")
	return []byte(strings.TrimSpace(text) + "\n"), nil
}

func upgradeExternalDrafterContract(body []byte) ([]byte, error) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("external drafter contract is empty")
	}
	// The 0.8 contract owns dispatch topology, grants, and report lifecycle.
	// Preserve project-specific evidence/tooling laws from the legacy contract,
	// but never carry forward the subagents/CAMPAIGN.md workspace protocol.
	legacyManaged := map[string]bool{
		"context pack": true, "workspace scope": true, "report format": true,
	}
	sections := []string{}
	lines := strings.Split(text, "\n")
	for index := 0; index < len(lines); {
		if !strings.HasPrefix(strings.TrimSpace(lines[index]), "## ") {
			index++
			continue
		}
		start := index
		heading := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[index]), "## "))
		index++
		for index < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[index]), "## ") {
			index++
		}
		if legacyManaged[strings.ToLower(heading)] || strings.EqualFold(heading, "Project-specific drafter guidance") {
			continue
		}
		section := strings.TrimSpace(strings.Join(lines[start:index], "\n"))
		if section != "" {
			sections = append(sections, section)
		}
	}
	result := strings.TrimSpace(migrationExternalDrafterContractTemplate)
	if len(sections) > 0 {
		result += "\n\n## Project-specific drafter guidance\n\n" + strings.Join(sections, "\n\n")
	}
	if strings.Contains(result, "/subagents/") || strings.Contains(result, "CAMPAIGN.md") {
		return nil, errors.New("external drafter contract retained a legacy workspace instruction")
	}
	return []byte(result + "\n"), nil
}

func upgradeMigrationNavigation(body []byte) ([]byte, error) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("project navigation index is empty")
	}
	activeMaster := regexp.MustCompile(`((?:\.\./)?active/[a-z0-9][a-z0-9-]{1,49}/)CAMPAIGN\.md`)
	text = activeMaster.ReplaceAllString(text, `${1}STATE.md`)
	text = strings.ReplaceAll(text, "active/<slug>/CAMPAIGN.md", "active/<slug>/STATE.md")
	if activeMaster.MatchString(text) || strings.Contains(text, "active/<slug>/CAMPAIGN.md") {
		return nil, errors.New("project navigation retains an operational legacy masterfile link")
	}
	return []byte(strings.TrimSpace(text) + "\n"), nil
}

func migrationManagedBlock(kind string) string {
	switch kind {
	case "shared-laws":
		return `<!-- re-discipline:shared-laws v0.8.0 -->
## Directory Means Trust

` + "`docs/truth/`" + ` contains closure-approved current claims; ` + "`docs/history/campaigns/`" + ` contains complete provenance; ` + "`active/<slug>/`" + ` contains provisional structured campaign records. Reports are provenance, findings are atomic knowledge, and backlog is deferred intent.

## Session Start

Use ` + "`onboard`" + `. Call bounded ` + "`state(mode=\"orient\")`" + `, then resume one selected campaign. Generated views and caches are derived from canonical files.

## Evidence, Review, And Lifecycle

Evidence grade, review state, and validity are independent. Managers open campaigns, delegate registered runs, review curator packets, overturn findings, and close campaigns through the shared engine. Only closure may project approved direct findings into truth.

## Roles And Safety

Managers own intent, review, retention, and closure. Drafters write only their registered report, lazy payload, and exact grants. Curators normalize and account for coverage but cannot ratify, edit truth, or close campaigns. Ordinary operations never read legacy state; use ` + "`migrate-project`" + ` for explicit digest-approved conversion. Memory is recall, never evidence. Commit or push only when the user asks.
<!-- re-discipline:shared-laws:end -->`
	case "router":
		return `<!-- re-discipline:router v0.8.0 -->
# re-discipline project entrypoint

Direct managers read their host adapter and the canonical project profile. Registered workers under ` + "`active/<slug>/runs/<run-id>/`" + ` read the external drafter contract and exact brief. Canonical mutations go through the shared engine; managers, drafters, and curators retain separate authority.
<!-- re-discipline:router:end -->`
	case "claude-adapter":
		return `<!-- re-discipline:claude-adapter v0.8.0 -->
# Claude Code Manager Adapter

Read ` + "`../.re-discipline/project-profile.md`" + `, use ` + "`onboard`" + ` and bounded state views, and keep project-owned host notes outside this block. The project config controls memory without modifying machine-local memory.
<!-- re-discipline:claude-adapter:end -->`
	case "codex-adapter":
		return `<!-- re-discipline:codex-adapter v0.8.0 -->
# Codex Manager Adapter

Read ` + "`.re-discipline/project-profile.md`" + `, use ` + "`re-discipline:onboard`" + ` and bounded state views, and keep project-owned host notes outside this block. The project config controls memory without modifying machine-local memory.
<!-- re-discipline:codex-adapter:end -->`
	default:
		return ""
	}
}

func digestMigrationPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("managed path is a symbolic link")
	}
	if info.Mode().IsRegular() {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return "sha256:" + SHA256Bytes(body), nil
	}
	if !info.IsDir() {
		return "", errors.New("managed path is neither a regular file nor directory")
	}
	tree, err := digestRegularTree(path)
	if err != nil {
		return "", err
	}
	return CanonicalDigest(tree)
}

func (engine *MigrationEngine) stageCampaign(
	plan MigrationPlan,
	state MigrationState,
	campaign string,
	activeRoot string,
	manifest *MigrationNormalizedManifest,
) error {
	campaignDir := filepath.Join(activeRoot, campaign)
	for _, directory := range []string{"work-items", "runs", "findings", "intake", "reviews", "events", "closure"} {
		if err := os.MkdirAll(filepath.Join(campaignDir, directory), 0o700); err != nil {
			return err
		}
	}
	campaignID := "C-" + strings.ToUpper(campaign)
	masterSource, masterBody, err := engine.legacyCampaignMaster(plan, campaign)
	if err != nil {
		return err
	}
	objective := markdownSection(masterBody, "Objective")
	if objective == "" {
		objective = "Complete manager review of the imported legacy campaign."
	}
	success := markdownListSection(masterBody, "Exit Criteria", "Success Criteria")
	if len(success) == 0 {
		success = []string{"Every imported run and finding has an explicit manager disposition."}
	}
	closure := markdownListSection(masterBody, "Closure Criteria", "Exit Criteria")
	if len(closure) == 0 {
		closure = []string{"Coverage, projection, archive, retrieval, and traversal gates pass."}
	}
	eventID := migrationEventID(campaign, plan.PlanDigest)
	workItems := legacyCampaignWorkItems(masterBody, campaignID, state)
	focus := []string{}
	for _, work := range workItems {
		if !validOne(work.State, "done", "cancelled", "superseded") {
			focus = append(focus, work.ID)
		}
	}
	if len(focus) == 0 {
		focus = []string{"W-0001"}
	}
	if len(focus) > 24 {
		focus = focus[:24]
	}
	record := CampaignRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: campaignID,
			CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
			CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
			CorrelationID: state.TransactionID,
		},
		Title: humanCampaignTitle(masterBody, campaign), Slug: campaign,
		Objective: objective, Scope: []string{"legacy campaign state imported by " + state.TransactionID},
		Exclusions:      []string{"migration does not ratify findings or project truth"},
		SuccessCriteria: success, ClosureCriteria: closure, Status: "paused",
		CurrentFocus: focus, Owner: state.Actor,
		PermittedManagers: []string{state.Actor}, OpenedAt: state.CreatedAt,
		PausedAt: state.UpdatedAt, LastEventID: eventID,
	}
	normalizeRecordLists(&record)
	campaignBody, err := sealMigratedRecord(&record)
	if err != nil {
		return fmt.Errorf("stage campaign %s: %w", campaign, err)
	}
	if err := AtomicWrite(filepath.Join(campaignDir, "campaign.json"), campaignBody, 0o600); err != nil {
		return err
	}
	runs, err := engine.stageLegacyRuns(plan, state, campaign, campaignDir, manifest)
	if err != nil {
		return err
	}
	if err := engine.stagePreNormalizedCampaignRecords(plan, campaign, campaignDir); err != nil {
		return err
	}
	runIDs := make([]string, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	for index := range workItems {
		work := &workItems[index]
		if work.ID == "W-0001" {
			for _, run := range runs {
				if isTerminalRun(run.Status) {
					work.CompletedRunIDs = append(work.CompletedRunIDs, run.ID)
				} else {
					work.ActiveRunIDs = append(work.ActiveRunIDs, run.ID)
				}
			}
		}
		workBody, err := sealMigratedRecord(work)
		if err != nil {
			return fmt.Errorf("stage work item %s: %w", work.ID, err)
		}
		if err := AtomicWrite(filepath.Join(campaignDir, "work-items", work.ID+".json"), workBody, 0o600); err != nil {
			return err
		}
	}
	if err := engine.stageCoverageKnowledge(plan, state, campaign, campaignDir, runs); err != nil {
		return err
	}
	if err := linkMigratedCampaignKnowledge(campaignDir); err != nil {
		return err
	}
	if err := engine.stageLegacyReviewImport(plan, state, campaign, campaignDir); err != nil {
		return err
	}
	event := StateEvent{
		SchemaVersion: CampaignSchemaVersion, ID: eventID,
		Timestamp: state.UpdatedAt, Actor: state.Actor, Authority: "manager",
		Action: "migration.import", AffectedIDs: append(append([]string{campaignID}, workItemIDs(workItems)...), runIDs...),
		PreviousRevision: 0, ResultingRevision: 1,
		IdempotencyKey: "migration:" + plan.PlanDigest + ":" + campaign,
		CorrelationID:  state.TransactionID,
		Rationale:      "Imported from the digest-pinned 0.7 source inventory; no epistemic ratification occurred.",
	}
	event.PreviousStateDigest = initialStateHead().StateDigest
	event.MutationDigest, err = CanonicalDigest(struct {
		CampaignDigest string   `json:"campaignDigest"`
		WorkDigests    []string `json:"workDigests"`
		RunIDs         []string `json:"runIds"`
		PlanDigest     string   `json:"planDigest"`
	}{record.Digest, workItemDigests(workItems), append([]string(nil), runIDs...), plan.PlanDigest})
	if err != nil {
		return err
	}
	event.ResultingStateDigest, err = CanonicalDigest(struct {
		Previous string `json:"previous"`
		Mutation string `json:"mutation"`
	}{event.PreviousStateDigest, event.MutationDigest})
	if err != nil {
		return err
	}
	if err := setEventDigest(&event); err != nil {
		return err
	}
	eventBody, err := json.Marshal(event)
	if err != nil {
		return err
	}
	eventBody = append(eventBody, '\n')
	if err := AtomicWrite(filepath.Join(campaignDir, "events", "events.jsonl"), eventBody, 0o600); err != nil {
		return err
	}
	stateView := renderMigratedStateMarkdown(record, workItems, runIDs, masterSource)
	if err := AtomicWrite(filepath.Join(campaignDir, "STATE.md"), []byte(stateView), 0o600); err != nil {
		return err
	}
	return nil
}

func linkMigratedCampaignKnowledge(campaignDir string) error {
	findingDir := filepath.Join(campaignDir, "findings")
	entries, err := os.ReadDir(findingDir)
	if err != nil {
		return err
	}
	runFindings := map[string][]string{}
	allFindingIDs := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(findingDir, entry.Name()))
		if readErr != nil {
			return readErr
		}
		document, parseErr := ParseFindingDocument(body, filepath.ToSlash(filepath.Join("active", filepath.Base(campaignDir), "findings", entry.Name())))
		if parseErr != nil {
			return parseErr
		}
		allFindingIDs = append(allFindingIDs, document.Record.ID)
		for _, runID := range document.Record.SourceRuns {
			runFindings[runID] = append(runFindings[runID], document.Record.ID)
		}
	}
	for runID, findingIDs := range runFindings {
		path := filepath.Join(campaignDir, "runs", runID, "run.json")
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("link finding to source run %s: %w", runID, readErr)
		}
		var run RunRecord
		if err := decodeStrictJSON(body, &run); err != nil {
			return err
		}
		run.FindingIDs = SortedUnique(append(run.FindingIDs, findingIDs...))
		body, err = sealMigratedRecord(&run)
		if err != nil {
			return err
		}
		if err := AtomicWrite(path, body, 0o600); err != nil {
			return err
		}
	}
	if len(allFindingIDs) == 0 {
		return nil
	}
	rootPath := filepath.Join(campaignDir, "work-items", "W-0001.json")
	body, err := os.ReadFile(rootPath)
	if err != nil {
		return err
	}
	var root WorkItemRecord
	if err := decodeStrictJSON(body, &root); err != nil {
		return err
	}
	root.FindingIDs = SortedUnique(append(root.FindingIDs, allFindingIDs...))
	body, err = sealMigratedRecord(&root)
	if err != nil {
		return err
	}
	return AtomicWrite(rootPath, body, 0o600)
}

func legacyCampaignWorkItems(body []byte, campaignID string, state MigrationState) []WorkItemRecord {
	meta := func(id string) RecordMeta {
		return RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: id,
			CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
			CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
			CorrelationID: state.TransactionID,
		}
	}
	items := []WorkItemRecord{{
		RecordMeta: meta("W-0001"), CampaignID: campaignID, Kind: "verification",
		Title:   "Reconcile imported legacy campaign frontier",
		Problem: "Legacy prose, review language, and run reports require structured manager reconciliation before work resumes.",
		State:   "blocked", Priority: "high", Acceptance: []string{
			"Every imported report has complete coverage or an explicit non-claim disposition.",
			"Every retained object and reconstructed frontier item passes structural and traversal verification.",
		}, Relations: WorkRelations{}, Owner: state.Actor,
		ResumeNote: "Review the imported frontier and certification evidence before starting new work.",
	}}
	type candidate struct{ title, problem, kind, state string }
	candidates := []candidate{}
	for _, heading := range []string{
		"Open Questions", "Questions", "Next Steps", "Follow-up Work", "Follow-ups",
		"Current State", "Deferred Work", "Planned Phases", "Goals", "Branches",
		"Problems", "Leads", "Abandoned Approaches", "Decisions", "Contradictions",
	} {
		section := markdownSection(body, heading)
		for _, text := range legacyFrontierStatements(section) {
			kind := "task"
			workState := legacyFrontierState(text)
			if strings.Contains(strings.ToLower(heading), "question") || strings.HasSuffix(text, "?") {
				kind = "question"
			}
			if kind == "question" && workState == "proposed" {
				workState = "ready"
			}
			if heading == "Deferred Work" {
				workState = "deferred"
			}
			title := text
			if len([]rune(title)) > 120 {
				title = string([]rune(title)[:120])
			}
			candidates = append(candidates, candidate{title: title, problem: text, kind: kind, state: workState})
		}
	}
	// Explicit phase headings often carry frontier state without a list.
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "## ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		lower := strings.ToLower(title)
		if !strings.HasPrefix(lower, "phase ") && !strings.HasPrefix(lower, "phase-") {
			continue
		}
		candidates = append(candidates, candidate{title: title, problem: "Legacy campaign phase: " + title, kind: "task", state: "proposed"})
	}
	seen := map[string]bool{}
	for _, item := range candidates {
		key := strings.ToLower(strings.TrimSpace(item.problem))
		if seen[key] {
			continue
		}
		seen[key] = true
		id := fmt.Sprintf("W-%04d", len(items)+1)
		record := WorkItemRecord{
			RecordMeta: meta(id), CampaignID: campaignID, Kind: item.kind,
			Title: item.title, Problem: item.problem, State: item.state,
			Priority: "normal", Acceptance: []string{"Manager explicitly resolves this imported legacy frontier item."},
			Relations: WorkRelations{ParentIDs: []string{"W-0001"}}, Owner: state.Actor,
			ResumeNote: "Imported from legacy campaign prose; confirm scope and disposition before execution.",
		}
		if item.state == "done" {
			record.Outcome = "Legacy prose records this item as completed or resolved; manager verification remains provenance-only."
		}
		if item.state == "deferred" {
			record.Deferment = &DefermentContract{
				Reason: item.problem,
				RevisitWhen: DefermentTrigger{
					Type: DefermentTriggerEvent, Action: "work.update", AffectedID: id,
				},
				Owner: state.Actor, BlocksClosure: true,
				ClosureDisposition: DefermentDispositionResolve,
			}
		}
		items[0].Relations.ChildIDs = append(items[0].Relations.ChildIDs, id)
		items = append(items, record)
	}
	return items
}

var legacyFrontierExplicitDoneRE = regexp.MustCompile(`(?i)^(?:\[[xX]\]\s*|(?:done|complete(?:d)?|resolved|applied)\s*[:\-]\s*)`)
var legacyFrontierExplicitBlockedRE = regexp.MustCompile(`(?i)^(?:\[!\]\s*|(?:blocked|pending|awaiting)\s*[:\-]\s*)`)
var legacyFrontierExplicitDeferredRE = regexp.MustCompile(`(?i)^(?:deferred|later)\s*[:\-]\s*`)
var legacyFrontierNumberedRE = regexp.MustCompile(`^[0-9]+[.)]\s+`)
var legacyFrontierTableStatusRE = regexp.MustCompile(`(?i)^(?:done|complete(?:d)?|resolved|applied|blocked|pending|awaiting|deferred|later)$`)

func legacyFrontierState(text string) string {
	trimmed := strings.TrimSpace(text)
	switch {
	case legacyFrontierExplicitDoneRE.MatchString(trimmed):
		return "done"
	case legacyFrontierExplicitBlockedRE.MatchString(trimmed):
		return "blocked"
	case legacyFrontierExplicitDeferredRE.MatchString(trimmed):
		return "deferred"
	default:
		// Ambiguous prose is never promoted to completed state by keyword
		// coincidence. It remains a proposed manager-review item.
		return "proposed"
	}
}

func legacyFrontierStatements(section string) []string {
	statements := []string{}
	paragraph := []string{}
	flush := func() {
		text := strings.TrimSpace(strings.Join(paragraph, " "))
		if text != "" {
			statements = append(statements, text)
		}
		paragraph = nil
	}
	for _, raw := range strings.Split(strings.ReplaceAll(section, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") {
			flush()
			continue
		}
		if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") {
			flush()
			cells := strings.Split(strings.Trim(line, "|"), "|")
			values := []string{}
			separator := true
			for _, cell := range cells {
				value := strings.TrimSpace(cell)
				if value != "" && strings.Trim(value, " :-") != "" {
					separator = false
					values = append(values, value)
				}
			}
			if !separator && len(values) > 0 && !validOne(strings.ToLower(values[0]),
				"status", "item", "branch", "goal", "question", "problem", "phase") {
				text := strings.Join(values, " | ")
				if len(values) > 1 && legacyFrontierTableStatusRE.MatchString(values[len(values)-1]) {
					text = values[len(values)-1] + ": " + strings.Join(values[:len(values)-1], " | ")
				}
				statements = append(statements, text)
			}
			continue
		}
		trimmed := line
		isList := false
		for _, prefix := range []string{"- ", "* ", "+ "} {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				isList = true
				break
			}
		}
		if !isList {
			if match := legacyFrontierNumberedRE.FindString(trimmed); match != "" {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, match))
				isList = true
			}
		}
		if isList {
			flush()
			if trimmed != "" {
				statements = append(statements, trimmed)
			}
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flush()
	return statements
}

func workItemIDs(items []WorkItemRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func workItemDigests(items []WorkItemRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Digest)
	}
	return out
}

func sealMigratedRecord[T any](record *T) ([]byte, error) {
	sealed, body, err := sealStateRecord(*record, "")
	if err != nil {
		return nil, err
	}
	normalized, ok := sealed.(T)
	if !ok {
		return nil, fmt.Errorf("migration state record type changed from %T to %T", *record, sealed)
	}
	*record = normalized
	return body, nil
}

func setEventDigest(event *StateEvent) error {
	return sealStateEvent(event)
}

func migrationEventID(campaign, planDigest string) string {
	digest := strings.ToUpper(strings.TrimPrefix(SHA256String(campaign+"\x00"+planDigest), "sha256:"))
	return "E-19700101-000000-" + digest[:12]
}

func (engine *MigrationEngine) legacyCampaignMaster(
	plan MigrationPlan, campaign string,
) (string, []byte, error) {
	for _, source := range plan.Sources {
		if source.Campaign == campaign && source.Role == "legacy-campaign-masterfile" {
			body, err := readMigrationSource(engine.ProjectRoot, source)
			return source.Path, body, err
		}
	}
	return "", []byte("# Campaign: " + campaign + "\n"), nil
}

func readMigrationSource(root string, source MigrationSource) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(source.Path))
	if err := validateRealDirectoryChain(root, filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("migration source %s has an unsafe parent: %w", source.Path, err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("migration source %s is no longer a regular file", source.Path)
	}
	if info.Size() != source.Size || info.ModTime().UTC().UnixNano() != source.MtimeNS {
		return nil, fmt.Errorf("migration source %s metadata changed after preview (size %d -> %d, mtime %d -> %d)",
			source.Path, source.Size, info.Size(), source.MtimeNS, info.ModTime().UTC().UnixNano())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if SHA256Bytes(body) != source.SHA256 {
		return nil, fmt.Errorf("migration source %s changed after preview", source.Path)
	}
	return body, nil
}

func validateRealDirectoryChain(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if !withinRoot(root, target) {
		return errors.New("directory escapes the project")
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
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("directory component %s is unsafe", current)
		}
	}
	return nil
}

func (engine *MigrationEngine) stageLegacyRuns(
	plan MigrationPlan,
	state MigrationState,
	campaign string,
	campaignDir string,
	manifest *MigrationNormalizedManifest,
) ([]RunRecord, error) {
	grouped := map[string][]MigrationSource{}
	for _, source := range plan.Sources {
		if source.Campaign != campaign {
			continue
		}
		if source.Role == "legacy-run-file" || source.Role == "legacy-run-report" {
			parts := strings.Split(source.Path, "/")
			if len(parts) > 3 {
				grouped[parts[3]] = append(grouped[parts[3]], source)
			}
		}
	}
	// Every campaign receives one import run which owns the masterfile, ledger,
	// and category-folder provenance not attributable to a delegated workspace.
	grouped["campaign-import"] = append(grouped["campaign-import"], campaignPayloadSources(plan, campaign)...)
	workspaces := make([]string, 0, len(grouped))
	for workspace := range grouped {
		workspaces = append(workspaces, workspace)
	}
	sort.Strings(workspaces)
	runs := []RunRecord{}
	for _, workspace := range workspaces {
		sources := grouped[workspace]
		if len(sources) == 0 && workspace != "campaign-import" {
			continue
		}
		runID := legacyRunID(campaign, workspace)
		runDir := filepath.Join(campaignDir, "runs", runID)
		if err := os.MkdirAll(filepath.Join(runDir, "payload", "legacy"), 0o700); err != nil {
			return nil, err
		}
		startedAt := legacyWorkspaceTimestamp(workspace, state.CreatedAt)
		role := legacyWorkspaceRole(workspace)
		run := RunRecord{
			RecordMeta: RecordMeta{
				SchemaVersion: CampaignSchemaVersion, ID: runID,
				CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
				CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
				CorrelationID: state.TransactionID,
			},
			CampaignID: "C-" + strings.ToUpper(campaign), PrimaryWorkItemID: "W-0001",
			ActorID: "legacy:" + workspace, Role: role, Status: "aborted",
			StartedAt: startedAt, TerminalAt: state.UpdatedAt,
			ResultSummary: "Imported legacy provenance; manager review is required before this run establishes knowledge.",
		}
		for _, source := range sources {
			body, err := readMigrationSource(engine.ProjectRoot, source)
			if err != nil {
				return nil, err
			}
			relative, reserved := migratedRunRelative(source, campaign, workspace)
			destination := filepath.Join(runDir, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return nil, err
			}
			if err := AtomicWrite(destination, body, 0o600); err != nil {
				return nil, err
			}
			manifest.LegacySources[source.Path] = filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, relative))
			canonicalPath := filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, relative))
			handle := &FileHandle{Path: canonicalPath, SHA256: "sha256:" + source.SHA256}
			switch reserved {
			case "brief":
				run.Brief = handle
			case "context":
				run.ContextPack = handle
			case "report":
				run.Report = handle
				run.Status = "returned"
				run.ReturnedAt = legacyReportTimestamp(body, startedAt, state.UpdatedAt)
				if reviewedAt := legacyReviewTimestamp(body); reviewedAt != "" {
					run.ReviewedAt = reviewedAt
				}
			default:
				run.Files = append(run.Files, RunFile{
					Path: relative, MediaKind: mediaKindFor(source.Path),
					SemanticRole: "reference-copy", Retention: "distill-then-review",
					SHA256: "sha256:" + source.SHA256,
				})
			}
		}
		if run.Role != "manager" {
			if run.Brief == nil {
				body := []byte("# Imported legacy run brief\n\nThe original workspace did not retain a brief. Scope is bounded to the preserved report and payload inventory.\n")
				path := filepath.Join(runDir, "brief.md")
				if err := AtomicWrite(path, body, 0o600); err != nil {
					return nil, err
				}
				handlePath := filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, "brief.md"))
				run.Brief = &FileHandle{Path: handlePath, SHA256: "sha256:" + SHA256Bytes(body)}
			}
			if run.ContextPack == nil {
				pack := map[string]any{
					"schemaVersion": 1, "kind": "migration-provenance", "transactionId": state.TransactionID,
					"campaignId": run.CampaignID, "runId": run.ID, "sourceWorkspace": workspace,
				}
				body, err := json.MarshalIndent(pack, "", "  ")
				if err != nil {
					return nil, err
				}
				body = append(body, '\n')
				path := filepath.Join(runDir, "context-pack.json")
				if err := AtomicWrite(path, body, 0o600); err != nil {
					return nil, err
				}
				handlePath := filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, "context-pack.json"))
				run.ContextPack = &FileHandle{Path: handlePath, SHA256: "sha256:" + SHA256Bytes(body)}
			}
		}
		if run.Status == "returned" && run.Report == nil {
			return nil, errors.New("internal migration error: returned legacy run lost its report")
		}
		runBody, err := sealMigratedRecord(&run)
		if err != nil {
			return nil, fmt.Errorf("stage run %s: %w", runID, err)
		}
		if err := AtomicWrite(filepath.Join(runDir, "run.json"), runBody, 0o600); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, nil
}

var legacyWorkspaceTimeRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z)`)

func legacyWorkspaceTimestamp(workspace, fallback string) string {
	match := legacyWorkspaceTimeRE.FindStringSubmatch(workspace)
	if len(match) == 2 {
		if parsed, err := time.Parse("2006-01-02T15-04-05Z", match[1]); err == nil {
			return RFC3339UTC(parsed.UTC())
		}
	}
	return fallback
}

func legacyWorkspaceRole(workspace string) string {
	lower := strings.ToLower(workspace)
	switch {
	case workspace == "campaign-import":
		return "manager"
	case strings.Contains(lower, "curator"):
		return "curator"
	case strings.Contains(lower, "review"), strings.Contains(lower, "audit"):
		return "reviewer"
	default:
		return "investigator"
	}
}

var legacyReviewDateRE = regexp.MustCompile(`(?im)^\*\*Review:\*\*\s*(\d{4}-\d{2}-\d{2})(?:\s+([^\r\n]+))?`)

func legacyReviewTimestamp(body []byte) string {
	match := legacyReviewDateRE.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", string(match[1]))
	if err != nil {
		return ""
	}
	return RFC3339UTC(parsed.UTC())
}

func legacyReportTimestamp(body []byte, startedAt, fallback string) string {
	if reviewed := legacyReviewTimestamp(body); reviewed != "" {
		start, _ := time.Parse(time.RFC3339Nano, startedAt)
		value, _ := time.Parse(time.RFC3339Nano, reviewed)
		if !value.Before(start) {
			return reviewed
		}
	}
	return fallback
}

func campaignPayloadSources(plan MigrationPlan, campaign string) []MigrationSource {
	out := []MigrationSource{}
	for _, source := range plan.Sources {
		if source.Campaign != campaign {
			continue
		}
		if source.Role == "legacy-campaign-masterfile" || source.Role == "legacy-review-ledger" ||
			source.Role == "legacy-campaign-payload" || source.Role == "normalized-finding" ||
			source.Role == "intake" || source.Role == "review-receipt" {
			out = append(out, source)
		}
	}
	return out
}

func (engine *MigrationEngine) stagePreNormalizedCampaignRecords(
	plan MigrationPlan,
	campaign string,
	campaignDir string,
) error {
	campaignID := "C-" + strings.ToUpper(campaign)
	for _, source := range plan.Sources {
		if source.Campaign != campaign || !validOne(source.Role, "normalized-finding", "intake", "review-receipt") {
			continue
		}
		body, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(source.Destination, "active/"+campaign+"/")
		if relative == source.Destination || NormalizeProjectPath(relative) != relative {
			return fmt.Errorf("pre-normalized record %s has an invalid campaign destination", source.Path)
		}
		switch source.Role {
		case "normalized-finding":
			document, parseErr := parseMigrationCompatibleFindingDocument(body, source.Destination)
			if parseErr != nil {
				return fmt.Errorf("pre-normalized finding %s is not a valid plan-bound additive record: %w", source.Path, parseErr)
			}
			if document.Record.CampaignID != campaignID || filepath.Base(source.Destination) != document.Record.ID+".md" {
				return fmt.Errorf("pre-normalized finding %s has a campaign or filename identity mismatch", source.Path)
			}
			for index, runReference := range document.Record.SourceRuns {
				mapped, mapErr := migrationCanonicalRunReference(plan, campaign, runReference)
				if mapErr != nil {
					return fmt.Errorf("pre-normalized finding %s sourceRuns: %w", source.Path, mapErr)
				}
				document.Record.SourceRuns[index] = mapped
			}
			for index := range document.Record.Evidence {
				evidence := &document.Record.Evidence[index]
				if evidence.SourceRun != "" {
					mapped, mapErr := migrationCanonicalRunReference(plan, campaign, evidence.SourceRun)
					if mapErr != nil {
						return fmt.Errorf("pre-normalized finding %s evidence sourceRun: %w", source.Path, mapErr)
					}
					evidence.SourceRun = mapped
				}
				mappedPath, changed, mapErr := migrationCanonicalLegacyPath(plan, campaign, evidence.Path)
				if mapErr != nil {
					return fmt.Errorf("pre-normalized finding %s evidence path: %w", source.Path, mapErr)
				}
				if changed {
					evidence.Path = mappedPath
					if evidence.StartLine > 0 && evidence.EndLine >= evidence.StartLine {
						evidence.ObjectKey = fmt.Sprintf("path:%s#L%d-L%d", evidence.Path, evidence.StartLine, evidence.EndLine)
					}
				}
			}
			document.Record.Path = source.Destination
			body, parseErr = RenderFindingDocument(document)
			if parseErr != nil {
				return fmt.Errorf("transform pre-normalized finding %s: %w", source.Path, parseErr)
			}
		case "intake":
			var record IntakeRecord
			if decodeErr := decodeStrictJSON(body, &record); decodeErr != nil {
				return fmt.Errorf("pre-normalized intake %s is not valid JSON: %w", source.Path, decodeErr)
			}
			if record.CampaignID != campaignID || filepath.Base(source.Destination) != record.ID+".json" {
				return fmt.Errorf("pre-normalized intake %s is not a valid plan-bound 0.8-compatible record", source.Path)
			}
			for index := range record.SourceRuns {
				mapped, changed, mapErr := migrationCanonicalLegacyPath(plan, campaign, record.SourceRuns[index].Path)
				if mapErr != nil {
					return fmt.Errorf("pre-normalized intake %s source run: %w", source.Path, mapErr)
				}
				if changed {
					record.SourceRuns[index].Path = mapped
				}
			}
			for index := range record.Coverage {
				entry := &record.Coverage[index]
				mapped, changed, mapErr := migrationCanonicalLegacyPath(plan, campaign, entry.SourcePath)
				if mapErr != nil {
					return fmt.Errorf("pre-normalized intake %s coverage: %w", source.Path, mapErr)
				}
				if changed {
					entry.SourcePath = mapped
					entry.SourceHandle = canonicalCoverageHandle(*entry)
				}
			}
			body, err = sealMigratedRecord(&record)
			if err != nil {
				return fmt.Errorf("transform pre-normalized intake %s: %w", source.Path, err)
			}
		case "review-receipt":
			var record ReviewRecord
			if decodeErr := decodeStrictJSON(body, &record); decodeErr != nil || ValidateReview(record) != nil ||
				record.CampaignID != campaignID || filepath.Base(source.Destination) != record.ID+".json" {
				return fmt.Errorf("pre-normalized review %s is not a valid plan-bound 0.8-compatible record", source.Path)
			}
		}
		destination := filepath.Join(campaignDir, filepath.FromSlash(relative))
		if _, statErr := os.Lstat(destination); statErr == nil {
			return fmt.Errorf("pre-normalized record destination collision at %s", source.Destination)
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := AtomicWrite(destination, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func migrationCanonicalRunReference(plan MigrationPlan, campaign, reference string) (string, error) {
	if runIDRE.MatchString(reference) {
		for _, source := range plan.Sources {
			if source.Campaign == campaign && strings.Contains(source.Destination, "/runs/"+reference+"/") {
				return reference, nil
			}
		}
		return "", fmt.Errorf("canonical run ID %s is not reconstructed by the approved plan", reference)
	}
	clean := NormalizeProjectPath(reference)
	if strings.HasPrefix(clean, "subagents/") {
		clean = "active/" + campaign + "/" + clean
	}
	prefix := "active/" + campaign + "/subagents/"
	if !strings.HasPrefix(clean, prefix) {
		return "", fmt.Errorf("legacy run reference %q is outside campaign %s", reference, campaign)
	}
	remainder := strings.TrimPrefix(clean, prefix)
	workspace := strings.Split(remainder, "/")[0]
	if workspace == "" {
		return "", fmt.Errorf("legacy run reference %q omits its workspace", reference)
	}
	workspacePrefix := prefix + workspace + "/"
	for _, source := range plan.Sources {
		if source.Campaign == campaign && strings.HasPrefix(source.Path, workspacePrefix) &&
			validOne(source.Role, "legacy-run-report", "legacy-run-file") {
			return legacyRunID(campaign, workspace), nil
		}
	}
	return "", fmt.Errorf("legacy run reference %q has no inventoried workspace", reference)
}

func migrationCanonicalLegacyPath(plan MigrationPlan, campaign, value string) (string, bool, error) {
	clean := NormalizeProjectPath(value)
	if strings.HasPrefix(clean, "subagents/") {
		clean = "active/" + campaign + "/" + clean
	}
	prefix := "active/" + campaign + "/subagents/"
	if !strings.HasPrefix(clean, prefix) {
		return value, false, nil
	}
	for _, source := range plan.Sources {
		if source.Path == clean && validOne(source.Role, "legacy-run-report", "legacy-run-file") {
			return source.Destination, true, nil
		}
	}
	return "", false, fmt.Errorf("legacy path %q is not an inventoried file", value)
}

func (engine *MigrationEngine) stageCoverageKnowledge(
	plan MigrationPlan, state MigrationState, campaign, campaignDir string, runs []RunRecord,
) error {
	live := false
	for _, slug := range plan.LiveCampaigns {
		if slug == campaign {
			live = true
		}
	}
	if !live {
		return nil
	}
	runByID := map[string]*RunRecord{}
	for index := range runs {
		runByID[runs[index].ID] = &runs[index]
	}
	findingSeen := map[string]bool{}
	intakeIndex := 0
	allFindingIDs := []string{}
	for _, source := range plan.Sources {
		if source.Campaign != campaign || source.Role != "legacy-run-report" {
			continue
		}
		receipt, err := engine.coverageFor(source.Path)
		if err != nil {
			return fmt.Errorf("load coverage for %s: %w", source.Path, err)
		}
		workspace := legacyWorkspaceForSource(source.Path)
		runID := legacyRunID(campaign, workspace)
		run := runByID[runID]
		if run == nil || run.Report == nil {
			return fmt.Errorf("coverage source %s has no reconstructed run", source.Path)
		}
		entryByFinding := map[string][]CoverageEntry{}
		for _, entry := range receipt.Coverage {
			if validOne(entry.Disposition, "candidate-finding", "duplicate") {
				entryByFinding[entry.TargetID] = append(entryByFinding[entry.TargetID], entry)
			}
		}
		for _, input := range receipt.Findings {
			if findingSeen[input.ID] {
				return fmt.Errorf("finding %s is supplied by multiple migration receipts", input.ID)
			}
			findingSeen[input.ID] = true
			evidence := []EvidenceReference{}
			for _, entry := range entryByFinding[input.ID] {
				evidence = append(evidence, EvidenceReference{
					Path: entry.SourcePath, SHA256: entry.SourceSHA256,
					StartLine: entry.StartLine, EndLine: entry.EndLine,
					ObjectKey: entry.SourceHandle, SourceRun: runID,
				})
			}
			if len(evidence) == 0 {
				return fmt.Errorf("finding %s has no exact coverage handle", input.ID)
			}
			body := input.Body
			if strings.TrimSpace(body) == "" {
				body = renderMigrationFindingBody(input, run.Report.Path)
			}
			path := filepath.ToSlash(filepath.Join("active", campaign, "findings", input.ID+".md"))
			document := FindingDocument{
				Record: FindingRecord{
					SchemaVersion: CampaignSchemaVersion, ID: input.ID,
					CampaignID: "C-" + strings.ToUpper(campaign), Revision: 1,
					CreatedAt: run.StartedAt, UpdatedAt: state.UpdatedAt,
					CreatedBy: "migration-curator:" + receipt.Reviewer, UpdatedBy: "migration-curator:" + receipt.Reviewer,
					CorrelationID: state.TransactionID, Kind: input.Kind, Subject: input.Subject,
					Claim: input.Claim, Scope: input.Scope, AppliesWhen: input.AppliesWhen,
					KnownLimits: input.KnownLimits, Tags: input.Tags, Subsystems: input.Subsystems,
					Aliases: input.Aliases, SourceRuns: []string{runID}, Evidence: evidence,
					Relations: FindingRelations{}, EvidenceGrade: input.EvidenceGrade,
					ReviewState: "extracted", Validity: "provisional", Projection: "none",
					Body: body, Path: path,
				},
				SyntheticQuestions: input.SyntheticQuestions, QuestionsReviewed: true,
			}
			findingBody, err := RenderFindingDocument(document)
			if err != nil {
				return fmt.Errorf("render migration finding %s: %w", input.ID, err)
			}
			if _, err := ParseFindingDocument(findingBody, path); err != nil {
				return fmt.Errorf("validate migration finding %s: %w", input.ID, err)
			}
			if err := AtomicWrite(filepath.Join(campaignDir, "findings", input.ID+".md"), findingBody, 0o600); err != nil {
				return err
			}
			run.FindingIDs = append(run.FindingIDs, input.ID)
			allFindingIDs = append(allFindingIDs, input.ID)
		}
		unresolved := migrationCoverageUncertainties(receipt)
		if len(receipt.Findings) == 0 && len(unresolved) == 0 {
			continue
		}
		intakeIndex++
		intakeID := fmt.Sprintf("I-%04d", intakeIndex)
		candidateIDs := append([]string(nil), receipt.FindingIDs...)
		triage := map[string]string{}
		for _, id := range candidateIDs {
			triage[id] = "attention"
		}
		requestedDecisions := []string{}
		requestedDecisions = append(requestedDecisions,
			"Legacy verdicts, confirmations, manager re-derivations, concurrences, and other review-attestation language in the source report are provenance only; none satisfies 0.8 curator attestation or manager ratification.")
		if len(candidateIDs) > 0 {
			requestedDecisions = append(requestedDecisions,
				"Manager must explicitly adjudicate imported provisional findings.",
				"Curator atomicity and evidence-grade attestations are review inputs, not semantic proof; every imported candidate requires individual manager attention.")
		}
		if len(unresolved) > 0 {
			requestedDecisions = append(requestedDecisions,
				"Manager must resolve or explicitly retain every reasoned unresolved source span; migration did not coerce those spans into findings.")
		}
		intake := IntakeRecord{
			RecordMeta: RecordMeta{SchemaVersion: CampaignSchemaVersion, ID: intakeID,
				CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
				CreatedBy: "migration-curator:" + receipt.Reviewer, UpdatedBy: "migration-curator:" + receipt.Reviewer,
				CorrelationID: state.TransactionID},
			CampaignID: "C-" + strings.ToUpper(campaign), SourceRuns: []FileHandle{*run.Report},
			CandidateFindingIDs: candidateIDs, Coverage: receipt.Coverage, Triage: triage,
			Uncertainties: unresolved, RequestedDecisions: requestedDecisions, Status: "submitted",
		}
		if reviewer := legacyReviewActorForSource(engine.ProjectRoot, source, ""); reviewer != "" {
			intake.RequestedDecisions = append(intake.RequestedDecisions,
				"Legacy review metadata names "+reviewer+"; it is provenance only and requires a fresh measured 0.8 manager decision.")
		}
		intakeBody, err := sealMigratedRecord(&intake)
		if err != nil {
			return fmt.Errorf("validate migration intake: %w", err)
		}
		if err := AtomicWrite(filepath.Join(campaignDir, "intake", intake.ID+".json"), intakeBody, 0o600); err != nil {
			return err
		}
	}
	for _, run := range runs {
		run = *runByID[run.ID]
		run.FindingIDs = SortedUnique(run.FindingIDs)
		runBody, err := sealMigratedRecord(&run)
		if err != nil {
			return err
		}
		if err := AtomicWrite(filepath.Join(campaignDir, "runs", run.ID, "run.json"), runBody, 0o600); err != nil {
			return err
		}
	}
	if len(allFindingIDs) > 0 {
		var root WorkItemRecord
		body, err := os.ReadFile(filepath.Join(campaignDir, "work-items", "W-0001.json"))
		if err != nil {
			return err
		}
		if err := decodeStrict(body, &root); err != nil {
			return err
		}
		root.FindingIDs = SortedUnique(append(root.FindingIDs, allFindingIDs...))
		root.Revision++
		root.UpdatedAt = state.UpdatedAt
		rootBody, err := sealMigratedRecord(&root)
		if err != nil {
			return err
		}
		if err := AtomicWrite(filepath.Join(campaignDir, "work-items", "W-0001.json"), rootBody, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func migrationCoverageHasUnresolved(receipt MigrationCoverageReceipt) bool {
	for _, entry := range receipt.Coverage {
		if entry.Disposition == "unresolved" {
			return true
		}
	}
	return false
}

func migrationCoverageUncertainties(receipt MigrationCoverageReceipt) []string {
	uncertainties := []string{}
	for _, entry := range receipt.Coverage {
		if entry.Disposition == "unresolved" {
			uncertainties = append(uncertainties, entry.SourceHandle+": "+strings.TrimSpace(entry.Rationale))
		}
	}
	return SortedUnique(uncertainties)
}

func legacyWorkspaceForSource(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) > 3 {
		return parts[3]
	}
	return "legacy"
}

func renderMigrationFindingBody(input MigrationFindingInput, source string) string {
	return "# Claim\n\n" + input.Claim + "\n\n## Applies when\n\nWithin the imported campaign scope recorded in this finding.\n\n## Does not establish\n\nMigration does not ratify this provisional claim or expand its scope.\n\n## Evidence\n\nDigest-pinned legacy report `" + source + "`; exact object handles are recorded in frontmatter.\n\n## Reproduction\n\nOpen the exact evidence handle and compare it to the approved source digest.\n\n## Relations\n\nNo relations were inferred by migration.\n"
}

func legacyReviewActorForSource(root string, source MigrationSource, fallback string) string {
	body, err := readMigrationSource(root, source)
	if err != nil {
		return ""
	}
	match := legacyReviewDateRE.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	if len(match) > 2 {
		actor := strings.TrimSpace(string(match[2]))
		if actor != "" {
			return actor
		}
	}
	return fallback
}

type migrationReviewImportRow struct {
	Date        string `json:"date"`
	Report      string `json:"report"`
	Promote     int    `json:"promote"`
	Hold        int    `json:"hold"`
	Drop        int    `json:"drop"`
	Block       int    `json:"block"`
	Destination string `json:"destination,omitempty"`
	Status      string `json:"status"`
}

func (engine *MigrationEngine) stageLegacyReviewImport(
	plan MigrationPlan, state MigrationState, campaign, campaignDir string,
) error {
	rows := []migrationReviewImportRow{}
	sources := []MigrationSource{}
	for _, source := range plan.Sources {
		if source.Campaign == campaign && source.Role == "legacy-review-ledger" {
			sources = append(sources, source)
		}
	}
	for _, source := range sources {
		body, err := readMigrationSource(engine.ProjectRoot, source)
		if err != nil {
			return err
		}
		rows = append(rows, parseLegacyReviewRows(body)...)
	}
	if len(sources) == 0 {
		return nil
	}
	artifact := struct {
		SchemaVersion int                        `json:"schemaVersion"`
		TransactionID string                     `json:"transactionId"`
		Campaign      string                     `json:"campaign"`
		Rows          []migrationReviewImportRow `json:"rows"`
		Status        string                     `json:"status"`
		Digest        string                     `json:"digest"`
	}{MigrationSchemaVersion, state.TransactionID, campaign, rows, "proposed-import", ""}
	if len(rows) == 0 {
		artifact.Status = "needs-manager-review"
	}
	artifact.Digest, _ = CanonicalDigest(artifact)
	body, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	runID := legacyRunID(campaign, "campaign-import")
	relative := "payload/legacy/review-import.json"
	if err := AtomicWrite(filepath.Join(campaignDir, "runs", runID, filepath.FromSlash(relative)), body, 0o600); err != nil {
		return err
	}
	runPath := filepath.Join(campaignDir, "runs", runID, "run.json")
	runBody, err := os.ReadFile(runPath)
	if err != nil {
		return err
	}
	var run RunRecord
	if err := decodeStrictJSON(runBody, &run); err != nil {
		return err
	}
	run.Files = append(run.Files, RunFile{Path: relative, MediaKind: "structured-data", SemanticRole: "reference-copy", Retention: "distill-then-review", SHA256: "sha256:" + SHA256Bytes(body)})
	run.Files = normalizeRunFiles(run.Files)
	runBody, err = sealMigratedRecord(&run)
	if err != nil {
		return err
	}
	if err := AtomicWrite(runPath, runBody, 0o600); err != nil {
		return err
	}
	return nil
}

func parseLegacyReviewRows(body []byte) []migrationReviewImportRow {
	rows := []migrationReviewImportRow{}
	for _, raw := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") || strings.Contains(strings.ToLower(line), "| date |") || strings.Contains(line, "---") {
			continue
		}
		parts := strings.Split(strings.Trim(line, "|"), "|")
		if len(parts) < 6 {
			continue
		}
		for index := range parts {
			parts[index] = strings.TrimSpace(parts[index])
		}
		promote, e1 := strconv.Atoi(parts[2])
		hold, e2 := strconv.Atoi(parts[3])
		drop, e3 := strconv.Atoi(parts[4])
		block, e4 := strconv.Atoi(parts[5])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		row := migrationReviewImportRow{Date: parts[0], Report: strings.Trim(parts[1], "`"), Promote: promote, Hold: hold, Drop: drop, Block: block, Status: "proposed-import"}
		if len(parts) > 6 {
			row.Destination = parts[6]
		}
		rows = append(rows, row)
	}
	return rows
}

func migratedRunRelative(source MigrationSource, campaign, workspace string) (string, string) {
	parts := strings.Split(source.Path, "/")
	if workspace != "campaign-import" && len(parts) > 4 {
		relative := strings.Join(parts[4:], "/")
		switch relative {
		case "brief.md":
			return relative, "brief"
		case "context-pack.json":
			return relative, "context"
		case "report.md":
			return relative, "report"
		default:
			return "payload/legacy/" + relative, ""
		}
	}
	relative := strings.TrimPrefix(source.Path, "active/"+campaign+"/")
	return "payload/legacy/" + relative, ""
}

func mediaKindFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".ps1", ".sh", ".js", ".ts", ".c", ".cpp", ".h":
		return "source-code"
	case ".json", ".jsonl", ".yaml", ".yml", ".csv", ".tsv", ".toml":
		return "structured-data"
	case ".md", ".txt", ".log":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".zip", ".tar", ".gz", ".7z":
		return "archive"
	default:
		return "binary"
	}
}

func markdownSection(body []byte, heading string) string {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	start := -1
	for index, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "## "+heading) {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	out := []string{}
	for _, line := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func markdownListSection(body []byte, headings ...string) []string {
	for _, heading := range headings {
		section := markdownSection(body, heading)
		if section == "" {
			continue
		}
		items := []string{}
		for _, line := range strings.Split(section, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				items = append(items, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			}
		}
		if len(items) > 0 {
			return SortedUnique(items)
		}
	}
	return nil
}

func humanCampaignTitle(body []byte, slug string) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "# Campaign:") {
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# Campaign:"))
			if value != "" {
				return value
			}
		}
	}
	return strings.ReplaceAll(slug, "-", " ")
}

func renderMigratedStateMarkdown(campaign CampaignRecord, workItems []WorkItemRecord, runIDs []string, source string) string {
	var builder strings.Builder
	builder.WriteString("# State: " + campaign.Title + "\n\n")
	builder.WriteString("> Generated from canonical records. Do not edit this file directly.\n\n")
	builder.WriteString("## Objective\n\n" + campaign.Objective + "\n\n")
	builder.WriteString("## Current focus\n\n")
	workByID := map[string]WorkItemRecord{}
	for _, work := range workItems {
		workByID[work.ID] = work
	}
	for _, id := range campaign.CurrentFocus {
		work := workByID[id]
		builder.WriteString("- `" + work.ID + "` - " + work.Title + " (`" + work.State + "`)\n")
	}
	builder.WriteString("\n")
	builder.WriteString("## Pending returned runs\n\n")
	if len(runIDs) == 0 {
		builder.WriteString("None.\n\n")
	} else {
		for _, runID := range runIDs {
			builder.WriteString("- `" + runID + "`\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Migration provenance\n\n")
	if source == "" {
		builder.WriteString("No legacy masterfile was present; the campaign requires manager reconciliation.\n")
	} else {
		builder.WriteString("Legacy source `" + source + "` is retained in the campaign import run. No finding or truth was ratified by migration.\n")
	}
	return builder.String()
}

func digestRegularTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("staged migration tree contains a non-regular file")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = SHA256Bytes(body)
		return nil
	})
	return files, err
}

func (engine *MigrationEngine) advancePhysical(
	state MigrationState, plan MigrationPlan, actor, adapter string,
) (MigrationState, error) {
	journalPath := filepath.Join(engine.migrationRoot(), "activation.json")
	lock, err := acquireWriterLock(engine.lockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer lock.Close()
	// Preparation and publication share the same migration writer lock. The
	// expected source digest is nevertheless rechecked at each rename because
	// a legacy process may not know about the 0.8 lock.
	journal, err := engine.loadOrPrepareActivation(plan, state, journalPath)
	if err != nil {
		return MigrationState{}, err
	}
	journal.Phase = "activating"
	if err := writeActivationJournal(journalPath, &journal); err != nil {
		return MigrationState{}, err
	}
	for index := range journal.Targets {
		target := &journal.Targets[index]
		canonical := filepath.Join(engine.ProjectRoot, filepath.FromSlash(target.Path))
		staged := filepath.Join(engine.migrationRoot(), "staging", "project", filepath.FromSlash(target.Path))
		backup := filepath.Join(engine.ProjectRoot, filepath.FromSlash(target.BackupPath))
		if target.Phase == "pending" {
			if _, err := os.Lstat(backup); err == nil {
				if _, targetErr := os.Lstat(canonical); os.IsNotExist(targetErr) {
					digest, digestErr := digestMigrationPath(backup)
					if digestErr != nil || (target.SourceDigest != "" && digest != target.SourceDigest) {
						return MigrationState{}, fmt.Errorf("backup for %s does not match the prepared source", target.Path)
					}
					target.Phase = "backed-up"
				} else {
					return MigrationState{}, fmt.Errorf("backup for %s already exists while canonical target is still present", target.Path)
				}
			} else if !os.IsNotExist(err) {
				return MigrationState{}, err
			} else {
				if err := verifyActivationSource(canonical, *target); err != nil {
					return MigrationState{}, err
				}
				if _, err := os.Lstat(canonical); err == nil {
					if err := engine.ensureMigrationDirectory(filepath.Dir(backup)); err != nil {
						return MigrationState{}, err
					}
					if err := os.Rename(canonical, backup); err != nil {
						return MigrationState{}, err
					}
					backupDigest, digestErr := digestMigrationPath(backup)
					if digestErr != nil || backupDigest != target.SourceDigest {
						// A non-cooperating legacy writer raced the pre-rename
						// digest. Restore its bytes and leave this target pending.
						if restoreErr := os.Rename(backup, canonical); restoreErr != nil {
							return MigrationState{}, fmt.Errorf("source %s changed during backup and restoration failed: %v", target.Path, restoreErr)
						}
						return MigrationState{}, fmt.Errorf("source %s changed during activation backup", target.Path)
					}
				} else if !os.IsNotExist(err) {
					return MigrationState{}, err
				}
				target.Phase = "backed-up"
			}
			if err := writeActivationJournal(journalPath, &journal); err != nil {
				return MigrationState{}, err
			}
			if engine.ActivationFailpoint != nil {
				if err := engine.ActivationFailpoint(MigrationActivationFailpoint{
					Phase: "backed-up", TargetIndex: index, TargetPath: target.Path,
				}); err != nil {
					return MigrationState{}, err
				}
			}
		}
		if target.Phase == "backed-up" {
			if _, err := os.Lstat(staged); err == nil {
				if _, targetErr := os.Lstat(canonical); targetErr == nil {
					return MigrationState{}, fmt.Errorf("canonical target %s reappeared during activation", target.Path)
				} else if !os.IsNotExist(targetErr) {
					return MigrationState{}, targetErr
				}
				if err := os.MkdirAll(filepath.Dir(canonical), 0o700); err != nil {
					return MigrationState{}, err
				}
				if err := os.Rename(staged, canonical); err != nil {
					return MigrationState{}, err
				}
			} else if !os.IsNotExist(err) {
				return MigrationState{}, err
			}
			publishedDigest, digestErr := digestMigrationPath(canonical)
			if digestErr != nil || publishedDigest != target.StagedDigest {
				return MigrationState{}, fmt.Errorf("published target %s does not match staged digest", target.Path)
			}
			target.Phase = "published"
			if err := writeActivationJournal(journalPath, &journal); err != nil {
				return MigrationState{}, err
			}
			if engine.ActivationFailpoint != nil {
				if err := engine.ActivationFailpoint(MigrationActivationFailpoint{
					Phase: "published", TargetIndex: index, TargetPath: target.Path,
				}); err != nil {
					return MigrationState{}, err
				}
			}
		}
		if target.Phase != "published" {
			return MigrationState{}, fmt.Errorf("unsupported activation target phase %q", target.Phase)
		}
		publishedDigest, err := digestMigrationPath(canonical)
		if err != nil || publishedDigest != target.StagedDigest {
			return MigrationState{}, fmt.Errorf("activated target %s drifted", target.Path)
		}
	}
	journal.Phase = "activated"
	if err := writeActivationJournal(journalPath, &journal); err != nil {
		return MigrationState{}, err
	}
	if journal.Phase != "activated" {
		return MigrationState{}, fmt.Errorf("unsupported activation phase %q", journal.Phase)
	}
	published := map[string]string{}
	recovery := []string{}
	for _, target := range journal.Targets {
		published[target.Path] = target.StagedDigest
		if target.Existed {
			recovery = append(recovery, target.BackupPath)
		}
	}
	activeDigest, err := CanonicalDigest(published)
	if err != nil {
		return MigrationState{}, err
	}
	receipt, err := engine.receipt("physically-reorganized", journal.StagedDigest, activeDigest,
		actor, adapter, recovery)
	if err != nil {
		return MigrationState{}, err
	}
	state.State = "physically-reorganized"
	state.Actor = actor
	state.Adapter = adapter
	state.UpdatedAt = RFC3339UTC(engine.Now().UTC())
	state.LastOperationID = receipt.OperationID
	state.Completed = append(state.Completed, receipt)
	state.Blockers = []string{}
	state.SafeNextAction = "run read-only verification and record structural, traversal, retrieval, and host-parity gate receipts"
	if err := engine.writeState(&state); err != nil {
		return MigrationState{}, err
	}
	return state, nil
}

func verifyActivationSource(canonical string, target migrationActivationTarget) error {
	info, err := os.Lstat(canonical)
	if target.Existed {
		if err != nil {
			return fmt.Errorf("activation source %s disappeared after approval", target.Path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("activation source %s became a symbolic link", target.Path)
		}
		digest, digestErr := digestMigrationPath(canonical)
		if digestErr != nil || digest != target.SourceDigest {
			return fmt.Errorf("activation source %s changed after journal preparation", target.Path)
		}
		return nil
	}
	if err == nil {
		return fmt.Errorf("activation source %s appeared after approval", target.Path)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (engine *MigrationEngine) loadOrPrepareActivation(
	plan MigrationPlan, state MigrationState, path string,
) (migrationActivationJournal, error) {
	var journal migrationActivationJournal
	if body, readErr := readSingleLinkRegularFile(path); readErr == nil {
		if err := decodeStrict(body, &journal); err != nil {
			return journal, err
		}
		expected := journal.Digest
		journal.Digest = ""
		digest, err := CanonicalDigest(journal)
		journal.Digest = expected
		if err != nil || digest != expected || journal.TransactionID != state.TransactionID || journal.PlanDigest != plan.PlanDigest {
			return migrationActivationJournal{}, errors.New("activation journal identity or digest mismatch")
		}
		return journal, nil
	} else if !os.IsNotExist(readErr) {
		return migrationActivationJournal{}, readErr
	}
	var manifest MigrationNormalizedManifest
	body, err := readSingleLinkRegularFile(filepath.Join(engine.migrationRoot(), "staging", "normalized-manifest.json"))
	if err != nil {
		return journal, err
	}
	if err := decodeStrict(body, &manifest); err != nil {
		return journal, err
	}
	expectedManifest := manifest.Digest
	manifest.Digest = ""
	digest, err := CanonicalDigest(manifest)
	manifest.Digest = expectedManifest
	if err != nil || digest != expectedManifest || manifest.PlanDigest != plan.PlanDigest {
		return journal, errors.New("normalized staging manifest digest or plan identity mismatch")
	}
	targets := []migrationActivationTarget{}
	for _, relative := range migrationManagedTargets(plan) {
		stagedDigest := manifest.ManagedTargets[relative]
		if stagedDigest == "" {
			return journal, fmt.Errorf("normalized manifest omitted managed target %s", relative)
		}
		stagedPath := filepath.Join(engine.migrationRoot(), "staging", "project", filepath.FromSlash(relative))
		actual, err := digestMigrationPath(stagedPath)
		if err != nil || actual != stagedDigest {
			return journal, fmt.Errorf("staged managed target %s does not match manifest", relative)
		}
		canonical := filepath.Join(engine.ProjectRoot, filepath.FromSlash(relative))
		existed := false
		sourceDigest := ""
		if _, err := os.Lstat(canonical); err == nil {
			existed = true
			sourceDigest, err = digestMigrationPath(canonical)
			if err != nil {
				return journal, err
			}
		} else if !os.IsNotExist(err) {
			return journal, err
		}
		backupName := strings.TrimPrefix(SHA256String(relative), "sha256:")[:16] + "-" + filepath.Base(relative)
		backupRelative := filepath.ToSlash(filepath.Join(".re-discipline", "migration", "0.8", "backups", backupName))
		targets = append(targets, migrationActivationTarget{Path: relative, StagedDigest: stagedDigest, SourceDigest: sourceDigest, BackupPath: backupRelative, Existed: existed, Phase: "pending"})
	}
	// Bind every target digest to the same approved source snapshot. A legacy
	// writer does not honor the 0.8 transaction lock, so it can race the first
	// preview. Capturing target digests first and then re-deriving the complete
	// approved plan closes that window: a write before this preview invalidates
	// approval, while a write after it is caught by the backup digest check.
	if engine.ActivationPrepareHook != nil {
		engine.ActivationPrepareHook()
	}
	fresh, err := PreviewMigration(engine.ProjectRoot, plan.LiveCampaigns)
	if err != nil {
		return journal, err
	}
	if fresh.Plan.PlanDigest != plan.PlanDigest || fresh.Plan.SourceFingerprint != plan.SourceFingerprint {
		return journal, errors.New("legacy migration source changed while activation journal was being prepared")
	}
	journal = migrationActivationJournal{
		SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID,
		PlanDigest: plan.PlanDigest, Phase: "prepared", StagedDigest: expectedManifest, Targets: targets,
	}
	if err := writeActivationJournal(path, &journal); err != nil {
		return migrationActivationJournal{}, err
	}
	return journal, nil
}

func writeActivationJournal(path string, journal *migrationActivationJournal) error {
	journal.Digest = ""
	digest, err := CanonicalDigest(*journal)
	if err != nil {
		return err
	}
	journal.Digest = digest
	return AtomicWriteJSON(path, *journal, 0o600)
}

func mustTreeDigest(root string) map[string]string {
	files, err := digestRegularTree(root)
	if err != nil {
		return map[string]string{"!error": err.Error()}
	}
	return files
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".migration-copy-*")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(tempPath, destination)
}

// carryForwardUnplannedFiles copies ordinary project files that live inside a
// replaced managed root but were never inventoried, so directory replacement
// cannot silently delete them. Activation swaps whole roots, so a placeholder
// such as active/.gitkeep would otherwise disappear even though no plan
// operation ever claimed it. Anything the plan already accounts for, and
// anything the staged tree already produced, is left untouched.
func (engine *MigrationEngine) carryForwardUnplannedFiles(
	plan MigrationPlan,
	projectStagingRoot string,
) error {
	planned := map[string]bool{}
	for _, source := range plan.Sources {
		planned[NormalizeProjectPath(source.Path)] = true
	}
	for _, root := range migrationManagedTargets(plan) {
		if root == stateInventoryPath {
			continue
		}
		canonical := filepath.Join(engine.ProjectRoot, filepath.FromSlash(root))
		info, err := os.Lstat(canonical)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		walkErr := filepath.WalkDir(canonical, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relative, relErr := filepath.Rel(engine.ProjectRoot, current)
			if relErr != nil {
				return relErr
			}
			slashed := filepath.ToSlash(relative)
			if planned[slashed] {
				return nil
			}
			destination := filepath.Join(projectStagingRoot, filepath.FromSlash(slashed))
			if _, statErr := os.Lstat(destination); statErr == nil {
				return nil
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
			body, readErr := readSingleLinkRegularFile(current)
			if readErr != nil {
				return readErr
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return err
			}
			return AtomicWrite(destination, body, 0o600)
		})
		if walkErr != nil {
			return walkErr
		}
	}
	return nil
}

// rewriteMigratedTruthLinkTargets retargets Markdown links that point at a
// converted truth document. It is a pure function of the document bytes, its
// project-relative path, and the conversion mapping, so verification can
// recompute the exact expected result from frozen provenance instead of
// demanding byte equality from a document whose links were legitimately
// rewritten.
func rewriteMigratedTruthLinkTargets(
	text string,
	relative string,
	mapping map[string]string,
) string {
	return migrationMarkdownTargetRE.ReplaceAllStringFunc(text, func(match string) string {
		parts := migrationMarkdownTargetRE.FindStringSubmatch(match)
		if len(parts) < 2 || strings.Contains(parts[1], "://") || strings.HasPrefix(parts[1], "#") {
			return match
		}
		target, fragment := parts[1], ""
		if index := strings.Index(target, "#"); index >= 0 {
			target, fragment = target[:index], target[index:]
		}
		resolved := filepath.ToSlash(filepath.Clean(
			filepath.Join(filepath.Dir(relative), filepath.FromSlash(target))))
		destination := mapping[resolved]
		if destination == "" {
			return match
		}
		rewritten, relErr := filepath.Rel(filepath.Dir(relative), filepath.FromSlash(destination))
		if relErr != nil {
			return match
		}
		return strings.Replace(match, parts[1], filepath.ToSlash(rewritten)+fragment, 1)
	})
}

// migratedTruthLinkMapping is the converted-truth destination map used by both
// the rewriter and its verification.
func migratedTruthLinkMapping(plan MigrationPlan) map[string]string {
	mapping := map[string]string{}
	for _, source := range plan.Sources {
		if source.Role == "truth" {
			mapping[source.Path] = source.Destination
		}
	}
	return mapping
}

// stageMigratedEvaluationCorpus retargets benchmark judgments onto the
// canonical destinations conversion produced. The migration plan requires
// converting judgments from path-only targets, and without it the converted
// project's own benchmark cannot run: every judgment still names a legacy
// path that activation replaced, so the retrieval gate could never be
// measured for the project it certifies.
//
// Only exact whole-path judgment values are retargeted. Prose, queries, and
// topics are never touched, and a path the plan does not convert is left
// alone so preserved history and backlog targets keep pointing at themselves.
// migrationEvalJudgmentContext resolves where each judged legacy path's
// retrievable content actually went. A campaign masterfile's canonical
// successor is structured JSON that chunk retrieval and context packs can
// never serve, and a converted truth document's full prose survives only as
// archive-tier provenance while its claim lives in one or more findings.
// Judgments must follow the content, not merely the canonical record.
type migrationEvalJudgmentContext struct {
	mapping        map[string]string
	masterfile     map[string]string
	truthManifest  map[string]string
	truthRows      map[string][]MigrationTruthPlan
	preservedTruth map[string]string
	stagingRoot    string
	stagedText     map[string]string
	// activatedCorpusFingerprint restamps every non-fixture corpusSnapshot
	// onto the corpus identity activation is about to publish.
	activatedCorpusFingerprint string
}

func newMigrationEvalJudgmentContext(
	plan MigrationPlan,
	projectStagingRoot string,
) migrationEvalJudgmentContext {
	context := migrationEvalJudgmentContext{
		mapping:        map[string]string{},
		masterfile:     map[string]string{},
		truthManifest:  map[string]string{},
		truthRows:      map[string][]MigrationTruthPlan{},
		preservedTruth: map[string]string{},
		stagingRoot:    projectStagingRoot,
		stagedText:     map[string]string{},
	}
	for _, row := range plan.TruthConversions {
		context.truthRows[row.SourcePath] = append(context.truthRows[row.SourcePath], row)
	}
	carriers := []string{}
	for _, campaign := range migrationCampaigns(plan) {
		carriers = append(carriers, campaign)
	}
	sort.Strings(carriers)
	carrier := "migration-provenance"
	if len(carriers) > 0 {
		carrier = carriers[0]
	}
	for _, source := range plan.Sources {
		if source.Role == "legacy-campaign-masterfile" && source.Campaign != "" {
			// The preserved masterfile bytes under the campaign's import run
			// are the only chunk-retrievable representation the campaign has
			// after conversion.
			context.masterfile[source.Path] = "active/" + source.Campaign + "/runs/" +
				legacyRunID(source.Campaign, "campaign-import") + "/payload/legacy/CAMPAIGN.md"
			continue
		}
		if source.Role == "truth" {
			if len(context.truthRows[source.Path]) > 1 {
				context.truthManifest[source.Path] = source.Destination
			}
			context.preservedTruth[source.Path] = "active/" + carrier + "/runs/" +
				legacyRunID(carrier, "campaign-import") + "/payload/legacy/truth/" +
				strings.TrimPrefix(source.Path, "docs/truth/")
			continue
		}
		destination := strings.TrimSpace(source.Destination)
		if destination == "" || destination == source.Path {
			continue
		}
		context.mapping[source.Path] = destination
	}
	return context
}

func (context *migrationEvalJudgmentContext) empty() bool {
	return len(context.mapping) == 0 && len(context.masterfile) == 0 &&
		len(context.truthRows) == 0
}

func (context *migrationEvalJudgmentContext) staged(path string) string {
	if cached, present := context.stagedText[path]; present {
		return cached
	}
	body, err := os.ReadFile(filepath.Join(context.stagingRoot, filepath.FromSlash(path)))
	text := ""
	if err == nil {
		text = strings.ToLower(string(body))
	}
	context.stagedText[path] = text
	return text
}

func (context *migrationEvalJudgmentContext) stagedContainsAny(path string, tokens []string) bool {
	text := context.staged(path)
	if text == "" {
		return false
	}
	for _, token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

// bestTruthRow mirrors the migrated-truth bounded-query ordering: title
// matches outrank claim matches outrank body mentions. It selects the split
// finding a query's vocabulary actually reaches; a zero score means no split
// carries the vocabulary at all.
func (context *migrationEvalJudgmentContext) bestTruthRow(
	rows []MigrationTruthPlan,
	tokens []string,
) (MigrationTruthPlan, int) {
	best, bestScore := MigrationTruthPlan{}, 0
	for _, row := range rows {
		title := strings.ToLower(row.Title)
		claim := strings.ToLower(row.Claim)
		body := context.staged(row.Destination)
		score := 0
		for _, token := range tokens {
			switch {
			case strings.Contains(title, token):
				score += 4
			case strings.Contains(claim, token):
				score += 2
			case strings.Contains(body, token):
				score++
			}
		}
		if score > bestScore || score == bestScore && score > 0 && row.Destination < best.Destination {
			best, bestScore = row, score
		}
	}
	return best, bestScore
}

// preservedTruthScore counts how much of a query's vocabulary survives only
// in the preserved pre-conversion prose. Unweighted single counts keep the
// finding-first bias: a claim or title match on a finding outweighs the same
// token appearing in preserved body prose.
func (context *migrationEvalJudgmentContext) preservedTruthScore(
	sourcePath string,
	tokens []string,
) int {
	preserved := context.preservedTruth[sourcePath]
	if preserved == "" {
		return 0
	}
	body := context.staged(preserved)
	if body == "" {
		return 0
	}
	score := 0
	for _, token := range tokens {
		if strings.Contains(body, token) {
			score++
		}
	}
	return score
}

// expandExpected converts one expected-path judgment value. Any-of judgment
// lists receive every destination the source's content reached, so a case
// stays satisfied by whichever converted representation retrieval ranks.
func (context *migrationEvalJudgmentContext) expandExpected(
	value string,
	tokens []string,
) (paths []string, archive bool) {
	normalized := NormalizeProjectPath(value)
	if preserved, ok := context.masterfile[normalized]; ok {
		return []string{preserved}, true
	}
	if rows, ok := context.truthRows[normalized]; ok {
		if manifest, split := context.truthManifest[normalized]; split {
			paths = append(paths, manifest)
		}
		_, rowScore := context.bestTruthRow(rows, tokens)
		for _, row := range rows {
			paths = append(paths, row.Destination)
		}
		if context.preservedTruthScore(normalized, tokens) > rowScore {
			// More of the query's vocabulary survives in the preserved prose
			// the conversion demoted to provenance than in any finding; the
			// judgment follows the content there.
			paths = append(paths, context.preservedTruth[normalized])
			archive = true
		}
		return paths, archive
	}
	if destination, ok := context.mapping[normalized]; ok {
		return []string{destination}, false
	}
	return []string{value}, false
}

// selectEvidence converts one must-be-retrieved judgment value to the single
// destination whose text the case's query can actually reach.
func (context *migrationEvalJudgmentContext) selectEvidence(
	value string,
	tokens []string,
) (path string, archive bool) {
	normalized := NormalizeProjectPath(value)
	if preserved, ok := context.masterfile[normalized]; ok {
		return preserved, true
	}
	if rows, ok := context.truthRows[normalized]; ok {
		row, rowScore := context.bestTruthRow(rows, tokens)
		if rowScore > 0 && context.preservedTruthScore(normalized, tokens) <= rowScore {
			return row.Destination, false
		}
		return context.preservedTruth[normalized], true
	}
	if destination, ok := context.mapping[normalized]; ok {
		return destination, false
	}
	return value, false
}

// expandHardNegative converts one hard-negative judgment value. A split
// source's pollution signal applies to every one of its rows.
func (context *migrationEvalJudgmentContext) expandHardNegative(value string) []string {
	normalized := NormalizeProjectPath(value)
	if preserved, ok := context.masterfile[normalized]; ok {
		return []string{preserved}
	}
	if rows, ok := context.truthRows[normalized]; ok {
		paths := []string{}
		if manifest, split := context.truthManifest[normalized]; split {
			paths = append(paths, manifest)
		}
		for _, row := range rows {
			paths = append(paths, row.Destination)
		}
		return paths
	}
	if destination, ok := context.mapping[normalized]; ok {
		return []string{destination}
	}
	return []string{value}
}

func appendUniquePaths(existing []string, values ...string) ([]string, bool) {
	changed := false
	seen := map[string]bool{}
	result := make([]string, 0, len(existing)+len(values))
	for _, value := range existing {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
			changed = true
		}
	}
	return result, changed
}

// migrationActivatedCorpusFingerprint computes the corpus identity the
// activated project will report, before activation publishes it. An unpinned
// evaluation case gates on the corpus-wide fingerprint, and conversion is the
// manager-reviewed transformation that changes the corpus, so the staged
// evaluation files must carry the post-activation value; a post-activation
// rewrite would break the materialization receipts that bind the staged
// bytes. The activated corpus is exactly the staged tree plus every live
// document outside the managed activation targets, discovered under the
// staged (migrated) knowledge settings.
func migrationActivatedCorpusFingerprint(
	plan MigrationPlan,
	projectRoot string,
	projectStagingRoot string,
) (string, error) {
	configuration := LoadConfiguration(projectStagingRoot)
	if !configuration.Valid || configuration.Unsafe {
		return "", fmt.Errorf(
			"staged knowledge configuration is invalid: %s",
			stringsOr(configuration.Errors, "configuration is invalid"))
	}
	stagedBoundary, err := NewBoundary(projectStagingRoot)
	if err != nil {
		return "", err
	}
	staged, err := DiscoverSources(stagedBoundary, configuration.Settings)
	if err != nil {
		return "", fmt.Errorf("discover staged corpus: %w", err)
	}
	liveBoundary, err := NewBoundary(projectRoot)
	if err != nil {
		return "", err
	}
	live, err := DiscoverSources(liveBoundary, configuration.Settings)
	if err != nil {
		return "", fmt.Errorf("discover surviving live corpus: %w", err)
	}
	targets := migrationManagedTargets(plan)
	underTarget := func(path string) bool {
		for _, target := range targets {
			if path == target || strings.HasPrefix(path, target+"/") {
				return true
			}
		}
		return false
	}
	merged := map[string]SourceDocument{}
	for _, document := range staged.Documents {
		merged[document.Path] = document
	}
	for _, document := range live.Documents {
		if _, present := merged[document.Path]; !present && !underTarget(document.Path) {
			merged[document.Path] = document
		}
	}
	paths := make([]string, 0, len(merged))
	for path := range merged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	documents := make([]SourceDocument, 0, len(paths))
	for _, path := range paths {
		documents = append(documents, merged[path])
	}
	return CorpusFingerprintForDocuments(documents)
}

// convertMigratedEvalCases rewrites one evaluation file's judgments onto the
// destinations conversion produced. It never touches queries, topics, or
// prose; it converts where a judgment says the answer lives, and extends a
// case's allowed tiers with the archive provenance tier exactly when the
// judged content now lives only there.
func convertMigratedEvalCases(
	context *migrationEvalJudgmentContext,
	cases []EvalCase,
) {
	for index := range cases {
		eval := &cases[index]
		tokens := migrationSearchTokens(eval.Query)
		retargeted := false
		archiveNeeded := false
		note := func(before, after string) string {
			if before != after {
				retargeted = true
			}
			return after
		}

		expanded := []string{}
		for _, value := range eval.ExpectedPaths {
			paths, archive := context.expandExpected(value, tokens)
			archiveNeeded = archiveNeeded || archive
			if len(paths) != 1 || paths[0] != value {
				retargeted = true
			}
			expanded, _ = appendUniquePaths(expanded, paths...)
		}
		eval.ExpectedPaths = expanded

		for position := range eval.MinimumEvidencePaths {
			path, archive := context.selectEvidence(eval.MinimumEvidencePaths[position], tokens)
			archiveNeeded = archiveNeeded || archive
			eval.MinimumEvidencePaths[position] = note(eval.MinimumEvidencePaths[position], path)
		}
		negatives := []string{}
		for _, value := range eval.HardNegativePaths {
			paths := context.expandHardNegative(value)
			if len(paths) != 1 || paths[0] != value {
				retargeted = true
			}
			negatives, _ = appendUniquePaths(negatives, paths...)
		}
		eval.HardNegativePaths = negatives
		for position := range eval.ExpectedCitations {
			path, archive := context.selectEvidence(eval.ExpectedCitations[position], tokens)
			archiveNeeded = archiveNeeded || archive
			eval.ExpectedCitations[position] = note(eval.ExpectedCitations[position], path)
		}
		if len(eval.GradedRelevantPaths) > 0 {
			graded := make(map[string]int, len(eval.GradedRelevantPaths))
			for value, grade := range eval.GradedRelevantPaths {
				paths, _ := context.expandExpected(value, tokens)
				if len(paths) != 1 || paths[0] != value {
					retargeted = true
				}
				for _, path := range paths {
					if existing, present := graded[path]; !present || grade > existing {
						graded[path] = grade
					}
				}
			}
			eval.GradedRelevantPaths = graded
		}
		for position := range eval.EvidencePins {
			pin := &eval.EvidencePins[position]
			path, archive := context.selectEvidence(pin.Path, tokens)
			archiveNeeded = archiveNeeded || archive
			pin.Path = note(pin.Path, path)
			// A pin gates on what its document claims, so that ordinary
			// drift invalidates a case. Conversion is not drift: it is the
			// manager-reviewed transformation this migration exists to
			// perform, and its result is the document the case must now
			// measure. Re-pin against the staged destination, or every
			// converted case reports a corpus mismatch forever.
			staged := filepath.Join(context.stagingRoot, filepath.FromSlash(pin.Path))
			body, pinErr := readSingleLinkRegularFile(staged)
			if pinErr != nil {
				continue
			}
			pin.ClaimSha256 = ClaimDigest(string(body), pin.Path)
			if pin.ContentSha256 != "" {
				pin.ContentSha256 = "sha256:" + SHA256Bytes(body)
			}
		}
		if archiveNeeded && !contains(eval.ForbiddenTiers, "archive") {
			tiers, changed := appendUniquePaths(eval.AllowedTiers, "archive")
			eval.AllowedTiers = tiers
			retargeted = retargeted || changed
		}
		// A target-disjoint attestation is a manager's review of one
		// query against one target's exact wording. Conversion replaces
		// that wording, so carrying the attestation onto the new text
		// would assert a review nobody performed. Drop it and let the
		// case be re-attested deliberately.
		if retargeted {
			eval.VocabularyPolicy = ""
		}
		// An unpinned case gates on the corpus-wide fingerprint, and
		// conversion is exactly the reviewed transformation that changes the
		// corpus. Without a restamp every such case reports a corpus
		// mismatch forever, the same defect shape re-pinning fixes for
		// pinned cases.
		if context.activatedCorpusFingerprint != "" &&
			eval.CorpusSnapshot != "" && !strings.HasPrefix(eval.CorpusSnapshot, "fixture:") {
			eval.CorpusSnapshot = context.activatedCorpusFingerprint
		}
	}
}

func (engine *MigrationEngine) stageMigratedEvaluationCorpus(
	plan MigrationPlan,
	projectStagingRoot string,
) error {
	context := newMigrationEvalJudgmentContext(plan, projectStagingRoot)
	if context.empty() {
		return nil
	}
	fingerprint, err := migrationActivatedCorpusFingerprint(
		plan, engine.ProjectRoot, projectStagingRoot)
	if err != nil {
		return fmt.Errorf("activated corpus fingerprint: %w", err)
	}
	context.activatedCorpusFingerprint = fingerprint
	relativeRoot := ".re-discipline/knowledge/evals"
	sourceRoot := filepath.Join(engine.ProjectRoot, filepath.FromSlash(relativeRoot))
	info, err := os.Lstat(sourceRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(sourceRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(current), ".json") {
			return nil
		}
		body, readErr := readSingleLinkRegularFile(current)
		if readErr != nil {
			return readErr
		}
		var cases []EvalCase
		if decodeErr := decodeStrictJSON(body, &cases); decodeErr != nil {
			return fmt.Errorf("decode evaluation corpus %s: %w", current, decodeErr)
		}
		convertMigratedEvalCases(&context, cases)
		relative, relErr := filepath.Rel(engine.ProjectRoot, current)
		if relErr != nil {
			return relErr
		}
		destination := filepath.Join(projectStagingRoot, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		converted, marshalErr := canonicalJSON(cases)
		if marshalErr != nil {
			return marshalErr
		}
		return AtomicWrite(destination, converted, 0o600)
	})
}
