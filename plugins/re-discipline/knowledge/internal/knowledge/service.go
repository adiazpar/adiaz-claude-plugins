package knowledge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type ServiceOptions struct {
	ProjectRoot          string
	AssetRoot            string
	CacheRoot            string
	DisableDense         bool
	DisableRerank        bool
	EffectiveProfileName string
}

type Service struct {
	Boundary             Boundary
	AssetRoot            string
	Configuration        Configuration
	ProfileCatalog       RetrievalProfile
	ModelManifest        ModelManifest
	Index                IndexManager
	DisableDense         bool
	DisableRerank        bool
	EffectiveProfileName string
	Warnings             []string
	pinnedGeneration     *Generation
	telemetryMu          sync.Mutex
	telemetryBuffer      []telemetryObservation
}

// PinGeneration freezes this service on one immutable generation. A
// measurement run pins the generation it starts on so that publications by
// another session cannot move the corpus underneath it, and so that the run
// itself never reconciles the index while it is measuring. The pin is
// process-local: it constrains this service only and never blocks a
// concurrent publication.
func (service *Service) PinGeneration(generation Generation) {
	pinned := generation
	service.pinnedGeneration = &pinned
}

func (service *Service) effectiveSettings() KnowledgeSettings {
	if service.Configuration.Valid {
		return service.Configuration.Settings
	}
	return DefaultKnowledgeSettings()
}

func NewService(options ServiceOptions) (*Service, error) {
	boundary, err := NewBoundary(options.ProjectRoot)
	if err != nil {
		return nil, err
	}
	assetRoot, err := filepath.Abs(options.AssetRoot)
	if err != nil {
		return nil, err
	}
	configuration := LoadConfiguration(boundary.Root)
	if configuration.Unsafe {
		return nil, fmt.Errorf(
			"unsafe project control-plane configuration: %s",
			strings.Join(configuration.Errors, "; "),
		)
	}
	manifest, err := LoadModelManifest(assetRoot)
	if err != nil {
		return nil, fmt.Errorf("load model manifest: %w", err)
	}
	profile, warnings, err := LoadProfile(assetRoot, boundary.Root, configuration)
	if err != nil {
		return nil, fmt.Errorf("load retrieval profile: %w", err)
	}
	if err := ValidateProfileModels(profile, manifest); err != nil {
		return nil, err
	}
	unavailableIDs := make([]string, 0, len(manifest.UnavailableModels))
	for id := range manifest.UnavailableModels {
		unavailableIDs = append(unavailableIDs, id)
	}
	sort.Strings(unavailableIDs)
	for _, id := range unavailableIDs {
		warnings = append(
			warnings,
			fmt.Sprintf("model %s unavailable: %s", id, manifest.UnavailableModels[id]),
		)
	}
	settings := configuration.Settings
	if !configuration.Valid {
		settings = DefaultKnowledgeSettings()
		warnings = append(warnings, configuration.Errors...)
	}
	if configuration.Valid && configuration.Bootstrap.Memory.Mode == "native" {
		settings.Sources.SharedMemory = false
	}
	cacheRoot, err := ResolveCacheRoot(boundary, options.CacheRoot)
	if err != nil {
		return nil, err
	}
	service := &Service{
		Boundary: boundary, AssetRoot: assetRoot, Configuration: configuration,
		ProfileCatalog: profile, ModelManifest: manifest,
		DisableDense: options.DisableDense, DisableRerank: options.DisableRerank,
		EffectiveProfileName: options.EffectiveProfileName, Warnings: warnings,
	}
	service.Index = IndexManager{
		Boundary: boundary, CacheRoot: cacheRoot, Settings: settings, Manifest: manifest,
	}
	return service, nil
}

func (service *Service) ensure(ctx context.Context) (Generation, SourceInventory, SelectedProfile, bool, error) {
	if !service.Configuration.Valid {
		return Generation{}, SourceInventory{}, SelectedProfile{}, false,
			fmt.Errorf(
				"project knowledge configuration is invalid: %s",
				strings.Join(service.Configuration.Errors, "; "),
			)
	}
	if service.Configuration.Valid && !service.Configuration.Bootstrap.Knowledge.Enabled {
		return Generation{}, SourceInventory{}, SelectedProfile{}, false,
			errors.New("project knowledge server is disabled by .re-discipline/config.json")
	}
	if service.pinnedGeneration != nil {
		selected, err := service.selectProfile(service.pinnedGeneration.Runtime)
		if err != nil {
			return Generation{}, SourceInventory{}, SelectedProfile{}, false, err
		}
		return *service.pinnedGeneration, SourceInventory{}, selected, false, nil
	}
	generation, inventory, rebuilt, err := service.Index.Ensure(ctx)
	if err != nil {
		return Generation{}, inventory, SelectedProfile{}, rebuilt, err
	}
	selected, err := service.selectProfile(generation.Runtime)
	if err != nil {
		return Generation{}, inventory, SelectedProfile{}, rebuilt, err
	}
	return generation, inventory, selected, rebuilt, nil
}

// leaseMeasurementGeneration ensures the index and then leases the resulting
// generation for the whole run. Retention in a concurrently publishing
// session skips a leased generation, so a long measurement keeps reading the
// database it started on. The retry closes the narrow window in which another
// session can publish enough generations to retire this one between
// publication and the lease.
func (service *Service) leaseMeasurementGeneration(
	ctx context.Context,
	purpose string,
) (Generation, SelectedProfile, *GenerationLease, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		generation, _, selected, _, err := service.ensure(ctx)
		if err != nil {
			return Generation{}, SelectedProfile{}, nil, err
		}
		lease, err := acquireGenerationLease(
			service.Index.CacheRoot, generation.ID, purpose)
		if err != nil {
			return Generation{}, SelectedProfile{}, nil, err
		}
		_, statErr := os.Stat(generation.Database)
		if statErr == nil {
			return generation, selected, lease, nil
		}
		lastErr = statErr
		_ = lease.Release()
	}
	return Generation{}, SelectedProfile{}, nil, fmt.Errorf(
		"lease a stable generation for %s: %w", purpose, lastErr)
}

func (service *Service) selectProfile(runtime RuntimeIdentity) (SelectedProfile, error) {
	var selected SelectedProfile
	var err error
	if service.EffectiveProfileName == "" {
		selected, err = SelectEffectiveProfile(
			service.ProfileCatalog, service.ModelManifest, runtime,
			service.DisableDense, service.DisableRerank,
		)
	} else {
		catalog := service.ProfileCatalog
		var rowSelection *EffectiveProfile
		for index := range catalog.EffectiveProfiles {
			if catalog.EffectiveProfiles[index].Name == service.EffectiveProfileName {
				row := catalog.EffectiveProfiles[index]
				rowSelection = &row
				break
			}
		}
		if rowSelection == nil {
			return SelectedProfile{}, fmt.Errorf("unknown effective profile %q", service.EffectiveProfileName)
		}
		catalog.EffectiveProfiles = []EffectiveProfile{*rowSelection}
		selected, err = SelectEffectiveProfile(catalog, service.ModelManifest, runtime, false, false)
	}
	if err != nil {
		return SelectedProfile{}, err
	}
	return service.finalizeSelectedProfile(selected, runtime)
}

func (service *Service) finalizeSelectedProfile(
	selected SelectedProfile,
	runtime RuntimeIdentity,
) (SelectedProfile, error) {
	settings := service.Configuration.Settings
	if !service.Configuration.Valid {
		settings = DefaultKnowledgeSettings()
	}
	if selected.Effective.Packing.MaxPassages > settings.Budgets.MaxPassages {
		selected.Effective.Packing.MaxPassages = settings.Budgets.MaxPassages
	}
	if selected.Effective.Packing.MaxBytes > settings.Budgets.MaxBytes {
		selected.Effective.Packing.MaxBytes = settings.Budgets.MaxBytes
	}
	identity, err := CanonicalDigest(struct {
		Profile EffectiveProfile        `json:"profile"`
		Runtime RuntimeContractIdentity `json:"runtime"`
		Models  []ModelIdentity         `json:"models"`
	}{runtimeEffectiveProfile(selected.Effective), RuntimeContract(runtime), selected.Models})
	if err != nil {
		return SelectedProfile{}, err
	}
	selected.EffectiveIdentity = selected.Effective.Name + "@" + identity
	return selected, nil
}

func (service *Service) selectProfileRow(
	runtime RuntimeIdentity,
	row EffectiveProfile,
) (SelectedProfile, error) {
	catalog := service.ProfileCatalog
	catalog.EffectiveProfiles = []EffectiveProfile{row}
	selected, err := SelectEffectiveProfile(
		catalog, service.ModelManifest, runtime, false, false)
	if err != nil {
		return SelectedProfile{}, err
	}
	return service.finalizeSelectedProfile(selected, runtime)
}

func (service *Service) Status(ctx context.Context) (map[string]any, error) {
	if service.Configuration.Valid && !service.Configuration.Bootstrap.Knowledge.Enabled {
		return map[string]any{
			"schemaVersion": 1,
			"configuration": map[string]any{
				"valid": true, "errors": []string{}, "warnings": service.Warnings,
				"memoryMode":          service.Configuration.Bootstrap.Memory.Mode,
				"writePolicy":         service.Configuration.Bootstrap.Memory.WritePolicy,
				"nativeMemoryTouched": false,
			},
			"knowledge": map[string]any{
				"enabled": false, "indexBuilt": false,
			},
		}, nil
	}
	runtimeIdentity, err := ProbeRuntimeIdentity(service.ModelManifest)
	if err != nil {
		return nil, err
	}
	selected, err := service.selectProfile(runtimeIdentity)
	if err != nil {
		return nil, err
	}
	generation, currentErr := service.Index.LoadCurrent()
	indexFresh := false
	indexIntegrity := false
	if currentErr == nil {
		indexIntegrity = verifyDatabase(generation.Database) == nil
		indexFresh = indexIntegrity &&
			generation.ModelFingerprint == modelIndexFingerprint(service.ModelManifest) &&
			generation.SettingsFingerprint ==
				IndexingSettingsDigest(service.Index.Settings) &&
			generation.Runtime == runtimeIdentity &&
			generation.ParserVersion == ParserVersion &&
			generation.ChunkerVersion == ChunkerVersion &&
			service.Index.FastFresh(generation)
		if indexFresh {
			generation.GitRevision, generation.DirtyFingerprint = gitState(service.Boundary.Root)
		}
	}
	status := map[string]any{
		"schemaVersion": 1,
		"configuration": map[string]any{
			"valid":               service.Configuration.Valid,
			"errors":              service.Configuration.Errors,
			"warnings":            service.Warnings,
			"memoryMode":          service.Configuration.Bootstrap.Memory.Mode,
			"writePolicy":         service.Configuration.Bootstrap.Memory.WritePolicy,
			"nativeMemoryTouched": false,
		},
		"generation": PublicGeneration(generation),
		"index": map[string]any{
			"fresh": indexFresh, "integrity": indexIntegrity,
			"present": currentErr == nil, "mutationPerformed": false,
			"diagnostics": []string{},
			"fts5":        true,
		},
		"requestedProfile": selected.RequestedIdentity,
		"effectiveProfile": selected.EffectiveIdentity,
		"activeLanes":      selected.ActiveLanes,
		"models":           selected.Models,
		"modelAvailability": map[string]any{
			"executable":  sortedModelIdentityValues(service.ModelManifest.ExecutableModels),
			"unavailable": service.ModelManifest.UnavailableModels,
		},
		"fallbackReason": selected.FallbackReason,
		"runtime":        selected.Runtime,
		"benchmark":      service.benchmarkStatus(selected.Effective.Benchmark, generation, runtimeIdentity),
		"pins":           service.EvidencePinCensus(),
		"campaigns":      service.campaignStatus(),
		"telemetry":      service.telemetryStatus(),
	}
	return status, nil
}

func (service *Service) benchmarkStatus(
	evidence BenchmarkEvidence,
	generation Generation,
	runtimeIdentity RuntimeIdentity,
) map[string]any {
	// Staleness is two different claims wearing one name.
	//
	// ACTIONABLE means the measured behavior itself may no longer hold: the
	// models, the runtime contract, or the way the corpus is segmented have
	// changed under the measurement, or there is no valid measurement at all.
	// Re-running the benchmark is the only way to learn what retrieval now
	// does.
	//
	// INFORMATIONAL means the measurement is still a faithful description of
	// how this profile behaves, but the corpus or the evaluation set has moved
	// since it was taken. In a living project documents change every day, so
	// reporting that at the same weight as a runtime change trains sessions to
	// ignore the field entirely.
	actionable := []string{}
	informational := []string{}
	if evidence.Status != "passed" || !sha256IdentityRE.MatchString(evidence.Digest) {
		actionable = append(actionable, "benchmark-not-passed")
	}
	modelFingerprint := mustDigest(service.ModelManifest.Models)
	if evidence.ModelFingerprint == "" || evidence.ModelFingerprint != modelFingerprint {
		actionable = append(actionable, "model-fingerprint")
	}
	runtimeFingerprint, _ := CanonicalDigest(RuntimeContract(runtimeIdentity))
	if evidence.RuntimeFingerprint == "" || evidence.RuntimeFingerprint != runtimeFingerprint {
		actionable = append(actionable, "runtime-fingerprint")
	}
	// A recorded chunker or parser version that no longer matches the running
	// one means the corpus was re-segmented after the measurement, which moves
	// passage boundaries and therefore every ranking that depends on them.
	// Absent values belong to a profile measured before this was recorded;
	// guessing there would mark every existing project actionable at once.
	if evidence.ChunkerVersion != "" && evidence.ChunkerVersion != ChunkerVersion {
		actionable = append(actionable, "chunker-version")
	}
	if evidence.ParserVersion != "" && evidence.ParserVersion != ParserVersion {
		actionable = append(actionable, "parser-version")
	}
	expectedEval := ""
	coverage := HardNegativeCoverage{}
	switch evidence.Suite {
	case "packaged-conformance-v1":
		if cases, err := LoadEvalCases(filepath.Join(
			service.AssetRoot, "evals", "conformance", "cases.json",
		)); err == nil {
			expectedEval, _ = CanonicalDigest(cases)
			coverage = MeasureHardNegativeCoverage(cases)
		}
	case "project-calibration-v1":
		if cases, err := service.loadProjectEvalCases(); err == nil {
			expectedEval, _ = CanonicalDigest(cases)
			coverage = MeasureHardNegativeCoverage(cases)
		}
		if evidence.CorpusFingerprint == "" ||
			evidence.CorpusFingerprint != generation.CorpusFingerprint {
			informational = append(informational, "corpus-fingerprint")
		}
	default:
		actionable = append(actionable, "benchmark-suite")
	}
	if expectedEval == "" || evidence.EvalFingerprint != expectedEval {
		informational = append(informational, "eval-fingerprint")
	}
	ageDays := -1
	if evaluatedAt, err := time.Parse(time.RFC3339, evidence.EvaluatedAt); err == nil {
		ageDays = int(time.Since(evaluatedAt).Hours() / 24)
	} else {
		actionable = append(actionable, "evaluated-at")
	}
	actionable = SortedUnique(actionable)
	informational = SortedUnique(informational)
	severity := "none"
	switch {
	case len(actionable) > 0:
		severity = "actionable"
	case len(informational) > 0:
		severity = "informational"
	}
	return map[string]any{
		"suite": evidence.Suite, "digest": evidence.Digest,
		"status": evidence.Status, "evaluatedAt": evidence.EvaluatedAt,
		"ageDays": ageDays,
		"stale":   len(actionable) > 0 || len(informational) > 0,
		"staleReasons": SortedUnique(
			append(append([]string{}, actionable...), informational...)),
		"staleSeverity":             severity,
		"staleActionable":           len(actionable) > 0,
		"staleInformational":        len(informational) > 0,
		"actionableStaleReasons":    actionable,
		"informationalStaleReasons": informational,
		// Coverage is measured against today's evaluation set, not against the
		// set the receipt was taken on. The question it answers is "how much of
		// the guard is real right now", which a stale receipt cannot answer.
		"hardNegativeCoverage": coverage,
		"evalFingerprint":      evidence.EvalFingerprint,
		"corpusFingerprint":    evidence.CorpusFingerprint,
		"modelFingerprint":     evidence.ModelFingerprint,
		"runtimeFingerprint":   evidence.RuntimeFingerprint,
		"chunkerVersion":       evidence.ChunkerVersion,
		"parserVersion":        evidence.ParserVersion,
	}
}

func (service *Service) ReconcileIndex(ctx context.Context) (map[string]any, error) {
	if !service.Configuration.Valid {
		return nil, fmt.Errorf(
			"project knowledge configuration is invalid: %s",
			strings.Join(service.Configuration.Errors, "; "),
		)
	}
	if service.Configuration.Valid && !service.Configuration.Bootstrap.Knowledge.Enabled {
		return nil, errors.New("project knowledge server is disabled by .re-discipline/config.json")
	}
	generation, inventory, rebuilt, err := service.Index.Reconcile(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := service.selectProfile(generation.Runtime)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"generation": PublicGeneration(generation), "rebuilt": rebuilt,
		"diagnostics":      inventory.Diagnostics,
		"requestedProfile": selected.RequestedIdentity,
		"effectiveProfile": selected.EffectiveIdentity,
		"activeLanes":      selected.ActiveLanes, "models": selected.Models,
		"fallbackReason": selected.FallbackReason,
	}, nil
}

func (service *Service) Search(
	ctx context.Context,
	options SearchOptions,
) (response SearchResponse, err error) {
	started := time.Now()
	defer func() {
		service.recordTelemetry(telemetryObservation{
			Operation: "search", EffectiveProfile: response.Metadata.EffectiveProfile,
			Failed: err != nil, Latency: time.Since(started),
			EstimatedTokens: response.EstimatedTokens, Results: len(response.Results),
			Omissions: response.Omitted,
		})
	}()
	return service.search(ctx, options, true)
}

func (service *Service) search(
	ctx context.Context,
	options SearchOptions,
	enforceConfiguredBudget bool,
) (SearchResponse, error) {
	settings := service.effectiveSettings()
	if options.TokenBudget == 0 {
		options.TokenBudget = settings.Budgets.SearchTokens
	}
	if enforceConfiguredBudget && options.TokenBudget > settings.Budgets.SearchTokens {
		options.TokenBudget = settings.Budgets.SearchTokens
	}
	if options.Limit == 0 {
		options.Limit = settings.Budgets.MaxPassages
	}
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return SearchResponse{}, err
	}
	response, err := (Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}).Search(ctx, options)
	if err != nil {
		return SearchResponse{}, err
	}
	if response.OmittedByReason["staleSource"] == 0 || generation.WriterContention ||
		service.pinnedGeneration != nil {
		return response, nil
	}
	reconciled, _, _, reconcileErr := service.Index.Reconcile(ctx)
	if reconcileErr != nil {
		return SearchResponse{}, fmt.Errorf(
			"selected source changed and index reconciliation failed: %w", reconcileErr)
	}
	selected, err = service.selectProfile(reconciled.Runtime)
	if err != nil {
		return SearchResponse{}, err
	}
	return (Retriever{
		Boundary: service.Boundary, Generation: reconciled, Profile: selected,
	}).Search(ctx, options)
}

func (service *Service) Orient(ctx context.Context, role string, tokenBudget int) (ContextPack, error) {
	return service.OrientVerbosity(ctx, role, tokenBudget, "")
}

func (service *Service) OrientVerbosity(
	ctx context.Context,
	role string,
	tokenBudget int,
	verbosity string,
) (ContextPack, error) {
	if role == "" {
		role = "manager"
	}
	tiers := []string{"profile", "navigation", "truth", "active", "memory"}
	return service.ContextPackOptions(ctx, ContextPackRequest{
		Task:  "project profile current campaign orientation",
		Role:  role,
		Tiers: tiers, TokenBudget: tokenBudget, Verbosity: verbosity,
	})
}

// ContextPackRequest is the full set of context-pack inputs. The narrower
// ContextPack and ContextPackRequired entry points remain for callers that do
// not choose a projection.
type ContextPackRequest struct {
	Task          string
	Role          string
	Tiers         []string
	TokenBudget   int
	RequiredPaths []string
	// Verbosity is compact when unset. It selects the per-passage citation
	// projection only; the pack envelope is the artifact's provenance and is
	// identical in both forms.
	Verbosity string
}

func (service *Service) ContextPack(
	ctx context.Context,
	task string,
	role string,
	tiers []string,
	tokenBudget int,
) (ContextPack, error) {
	return service.ContextPackRequired(ctx, task, role, tiers, tokenBudget, nil)
}

func (service *Service) ContextPackRequired(
	ctx context.Context,
	task string,
	role string,
	tiers []string,
	tokenBudget int,
	requiredPaths []string,
) (ContextPack, error) {
	return service.ContextPackOptions(ctx, ContextPackRequest{
		Task: task, Role: role, Tiers: tiers, TokenBudget: tokenBudget,
		RequiredPaths: requiredPaths,
	})
}

func (service *Service) ContextPackOptions(
	ctx context.Context,
	request ContextPackRequest,
) (pack ContextPack, err error) {
	task, role := request.Task, request.Role
	tiers, tokenBudget := request.Tiers, request.TokenBudget
	requiredPaths := request.RequiredPaths
	verbosity, verbosityErr := NormalizeVerbosity(request.Verbosity)
	if verbosityErr != nil {
		return ContextPack{}, verbosityErr
	}
	started := time.Now()
	defer func() {
		omissions := 0
		for _, count := range pack.Omitted {
			omissions += count
		}
		service.recordTelemetry(telemetryObservation{
			Operation: "context-pack", EffectiveProfile: pack.EffectiveProfile,
			Failed: err != nil, Latency: time.Since(started),
			EstimatedTokens: pack.EstimatedTokens, Results: len(pack.Passages),
			Omissions: omissions,
		})
	}()
	pack, err = service.contextPackRequired(
		ctx, task, role, tiers, tokenBudget, requiredPaths, verbosity,
	)
	if errors.Is(err, errSourceChangedAfterIndex) && service.pinnedGeneration == nil {
		if _, _, _, reconcileErr := service.Index.Reconcile(ctx); reconcileErr != nil {
			return ContextPack{}, fmt.Errorf(
				"context-pack source reconciliation failed: %w", reconcileErr)
		}
		return service.contextPackRequired(
			ctx, task, role, tiers, tokenBudget, requiredPaths, verbosity,
		)
	}
	return pack, err
}

func (service *Service) contextPackRequired(
	ctx context.Context,
	task string,
	role string,
	tiers []string,
	tokenBudget int,
	requiredPaths []string,
	verbosity string,
) (ContextPack, error) {
	if role != "manager" && role != "drafter" {
		return ContextPack{}, errors.New("context pack role must be manager or drafter")
	}
	if len([]byte(task)) < 1 || len([]byte(task)) > 2000 {
		return ContextPack{}, errors.New("context pack task must contain 1 to 2000 UTF-8 bytes")
	}
	if len(tiers) == 0 {
		// `campaign` carries reviewed drafter findings, so a manager sees what
		// its own campaign has already established. `draft` is deliberately
		// absent from both sets: unreviewed drafter output must be requested
		// by name, and that request is then visible in the pack's allowedTiers
		// for review-subagent to check.
		if role == "drafter" {
			tiers = []string{"profile", "navigation", "truth", "active", "memory"}
		} else {
			tiers = []string{
				"profile", "navigation", "truth", "history", "backlog",
				"active", "campaign", "memory",
			}
		}
	}
	settings := service.effectiveSettings()
	if tokenBudget != 0 && tokenBudget < 512 {
		return ContextPack{}, errors.New("context pack token budget must be at least 512")
	}
	roleBudget := settings.Budgets.ManagerContextTokens
	if role == "drafter" {
		roleBudget = settings.Budgets.DrafterContextTokens
	}
	if tokenBudget == 0 {
		tokenBudget = roleBudget
	} else if tokenBudget > roleBudget {
		tokenBudget = roleBudget
	}
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return ContextPack{}, err
	}
	search, err := service.search(ctx, SearchOptions{
		Query: task, QueryClass: "auto", AllowedTiers: tiers,
		Limit: selected.Effective.Packing.MaxPassages, TokenBudget: tokenBudget,
		Verbosity: verbosity, retainURI: true,
	}, false)
	if err != nil {
		return ContextPack{}, err
	}
	if search.Metadata.Generation != generation.ID {
		generation, err = service.Index.LoadCurrent()
		if err != nil {
			return ContextPack{}, err
		}
		selected, err = service.selectProfile(generation.Runtime)
		if err != nil {
			return ContextPack{}, err
		}
	}
	requiredResults := []SearchResult{}
	requiredSeen := map[string]bool{}
	uniqueRequiredPaths := SortedUnique(requiredPaths)
	if len(uniqueRequiredPaths) > selected.Effective.Packing.MaxPassages {
		return ContextPack{}, fmt.Errorf(
			"%d required paths exceed the context-pack passage cap of %d",
			len(uniqueRequiredPaths), selected.Effective.Packing.MaxPassages,
		)
	}
	cleanRequiredPaths := make([]string, 0, len(uniqueRequiredPaths))
	for _, requiredPath := range uniqueRequiredPaths {
		normalized := strings.ReplaceAll(strings.TrimSpace(requiredPath), "\\", "/")
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(normalized)))
		if normalized == "" || normalized != clean || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) ||
			IsForbiddenSource(clean) {
			return ContextPack{}, fmt.Errorf("required path %q is unsafe", requiredPath)
		}
		cleanRequiredPaths = append(cleanRequiredPaths, clean)
	}
	requiredRetriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}
	requiredChunks, requiredErr := requiredRetriever.BestDocumentChunks(
		ctx, task, "auto", tiers, cleanRequiredPaths)
	if requiredErr != nil {
		return ContextPack{}, requiredErr
	}
	for _, clean := range cleanRequiredPaths {
		row, present := requiredChunks[clean]
		if !present {
			return ContextPack{}, fmt.Errorf(
				"required path %q is not part of the active managed knowledge "+
					"corpus within the allowed epistemic tiers", clean)
		}
		if !contains(tiers, row.Chunk.Tier) {
			return ContextPack{}, fmt.Errorf(
				"required path %q is outside the allowed epistemic tiers", clean)
		}
		// A required passage is held to exactly the same source-integrity bar as
		// a retrieved one: the cited lines are re-read from the working tree and
		// must still hash to what the index recorded. A required path is never
		// silently dropped for staleness the way a candidate is, so the failure
		// is raised and the caller reconciles.
		if !requiredRetriever.verifyChunk(row.Chunk, row.DocumentHash) {
			return ContextPack{}, fmt.Errorf(
				"read required path %q: %w", clean, errSourceChangedAfterIndex)
		}
		requiredSeen[clean] = true
		requiredResults = append(requiredResults, projectSearchResult(SearchResult{
			ChunkID: row.Chunk.ID,
			Score:   1<<62 - 1, LaneRanks: map[string]int{"required": 1},
			Passage:         row.Chunk.Content,
			DocumentContext: row.Chunk.Context,
			Citation: Citation{
				Path: row.Chunk.Path, Heading: row.Chunk.Heading,
				StartLine: row.Chunk.StartLine, EndLine: row.Chunk.EndLine,
				ContentHash: row.Chunk.ContentHash, SourceHash: row.DocumentHash,
				PassageHash: row.Chunk.ContentHash, Tier: row.Chunk.Tier,
				URI:         "re-discipline://" + generation.ID + "/chunks/" + row.Chunk.ID,
				ContextHash: row.Chunk.ContextHash,
				Durability:  CitationDurability(row.Chunk.Tier),
			},
		}, verbosity, true))
	}
	if len(requiredResults) > 0 {
		filtered := make([]SearchResult, 0, len(search.Results))
		for _, result := range search.Results {
			if !requiredSeen[result.Citation.Path] {
				filtered = append(filtered, result)
			}
		}
		remaining := selected.Effective.Packing.MaxPassages - len(requiredResults)
		if len(filtered) > remaining {
			search.Omitted += len(filtered) - remaining
			search.OmittedByReason["resultLimit"] += len(filtered) - remaining
			filtered = filtered[:remaining]
		}
		search.Results = append(requiredResults, filtered...)
	}
	requiredCount := len(requiredResults)
	models := make([]string, 0, len(selected.Models))
	for _, model := range selected.Models {
		models = append(models, model.ID+"@"+model.Revision)
	}
	passages := make([]ContextPassage, 0, len(search.Results))
	for _, result := range search.Results {
		passages = append(passages, ContextPassage{
			ChunkID: result.ChunkID, Passage: result.Passage,
			DocumentContext: result.DocumentContext, Citation: result.Citation,
		})
	}
	pack := ContextPack{
		SchemaVersion: 1, Query: task,
		Generation: CompactContextGeneration(generation),
		Role:       role, AllowedTiers: search.AllowedTiers,
		RequestedProfile: selected.RequestedIdentity,
		EffectiveProfile: selected.EffectiveIdentity,
		ActiveLanes:      append([]string(nil), selected.ActiveLanes...),
		Models:           models,
		FallbackReason:   selected.FallbackReason,
		TokenBudget:      tokenBudget, EstimatedTokens: search.EstimatedTokens,
		Verbosity: verbosity,
		Passages:  passages,
		Omitted: map[string]int{
			"candidatePassages": search.Omitted,
			"budget":            search.OmittedByReason["budget"],
			"documentCap":       search.OmittedByReason["documentCap"],
			"resultLimit":       search.OmittedByReason["resultLimit"],
			"staleSource":       search.OmittedByReason["staleSource"],
		},
	}
	for {
		finalized, err := finalizeContextPack(pack)
		if err != nil {
			return ContextPack{}, err
		}
		pack = finalized
		body, marshalErr := json.Marshal(pack)
		if marshalErr != nil {
			return ContextPack{}, marshalErr
		}
		if pack.EstimatedTokens <= tokenBudget &&
			len(body) <= selected.Effective.Packing.MaxBytes {
			return pack, nil
		}
		if len(pack.Passages) == 0 {
			return ContextPack{}, errors.New("token budget is too small for mandatory context-pack metadata")
		}
		if len(pack.Passages) <= requiredCount {
			return ContextPack{}, errors.New(
				"required source paths do not fit the context-pack token or byte budget")
		}
		pack.Passages = pack.Passages[:len(pack.Passages)-1]
		pack.Omitted["candidatePassages"]++
		pack.PackID, pack.Digest, pack.EstimatedTokens = "", "", 0
	}
}

func finalizeContextPack(pack ContextPack) (ContextPack, error) {
	for iteration := 0; iteration < 4; iteration++ {
		pack.PackID, pack.Digest = "", ""
		digest, err := CanonicalDigest(pack)
		if err != nil {
			return ContextPack{}, err
		}
		pack.Digest = digest
		pack.PackID = "context-" + strings.TrimPrefix(digest, "sha256:")[:20]
		body, err := json.Marshal(pack)
		if err != nil {
			return ContextPack{}, err
		}
		estimated := EstimateTokens(string(body))
		if estimated == pack.EstimatedTokens {
			// estimatedTokens participates in the digest, so finalize once more.
			digestInput := pack
			digestInput.PackID, digestInput.Digest = "", ""
			finalDigest, err := CanonicalDigest(digestInput)
			if err != nil {
				return ContextPack{}, err
			}
			pack.Digest = finalDigest
			pack.PackID = "context-" + strings.TrimPrefix(finalDigest, "sha256:")[:20]
			return pack, nil
		}
		pack.EstimatedTokens = estimated
	}
	return ContextPack{}, errors.New("context-pack token accounting failed to converge")
}

type ReadOptions struct {
	Path      string
	ChunkID   string
	URI       string
	StartLine int
	EndLine   int
}

var errSourceChangedAfterIndex = errors.New("source changed after indexing")

func (service *Service) Read(ctx context.Context, options ReadOptions) (map[string]any, error) {
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return nil, err
	}
	value, err := service.readPinned(ctx, generation, selected, options)
	if !errors.Is(err, errSourceChangedAfterIndex) || generation.WriterContention ||
		service.pinnedGeneration != nil {
		return value, err
	}
	reconciled, _, _, reconcileErr := service.Index.Reconcile(ctx)
	if reconcileErr != nil {
		return nil, fmt.Errorf("read source reconciliation failed: %w", reconcileErr)
	}
	selected, err = service.selectProfile(reconciled.Runtime)
	if err != nil {
		return nil, err
	}
	return service.readPinned(ctx, reconciled, selected, options)
}

func (service *Service) readPinned(
	ctx context.Context,
	generation Generation,
	selected SelectedProfile,
	options ReadOptions,
) (map[string]any, error) {
	identifiers := 0
	for _, value := range []string{options.Path, options.ChunkID, options.URI} {
		if value != "" {
			identifiers++
		}
	}
	if identifiers != 1 {
		return nil, errors.New("exactly one of path, chunkId, or uri is required")
	}
	chunkID := options.ChunkID
	if options.URI != "" {
		chunkPrefix := "re-discipline://" + generation.ID + "/chunks/"
		sourcePrefix := "re-discipline://" + generation.ID + "/sources/"
		if strings.HasPrefix(options.URI, chunkPrefix) {
			decoded, err := url.PathUnescape(strings.TrimPrefix(options.URI, chunkPrefix))
			if err != nil {
				return nil, errors.New("invalid encoded chunk URI")
			}
			chunkID = decoded
		} else if strings.HasPrefix(options.URI, sourcePrefix) {
			decoded, err := url.PathUnescape(strings.TrimPrefix(options.URI, sourcePrefix))
			if err != nil {
				return nil, errors.New("invalid encoded source URI")
			}
			options.Path = decoded
		} else {
			return nil, errors.New("resource URI does not belong to the active generation")
		}
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var path, tier, heading, expectedHash, expectedDocumentHash string
	var startLine, endLine int
	if chunkID != "" {
		err = db.QueryRowContext(ctx, `SELECT c.path,c.tier,c.heading,c.start_line,
			c.end_line,c.content_hash,d.content_hash
			FROM chunks c JOIN documents d ON d.id=c.document_id WHERE c.id=?`,
			chunkID).Scan(
			&path, &tier, &heading, &startLine, &endLine,
			&expectedHash, &expectedDocumentHash)
		if err != nil {
			return nil, errors.New("unknown chunk or generation handle")
		}
	} else {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(options.Path, "\\", "/"))))
		err = db.QueryRowContext(ctx, `SELECT tier,title,content_hash FROM documents WHERE path=?`,
			path).Scan(&tier, &heading, &expectedHash)
		if err != nil {
			return nil, errors.New("path is not part of the active managed knowledge corpus")
		}
		expectedDocumentHash = expectedHash
		startLine, endLine = options.StartLine, options.EndLine
	}
	body, fileHash, err := ReadProjectFile(service.Boundary, path)
	if err != nil {
		return nil, err
	}
	if fileHash != expectedDocumentHash {
		return nil, fmt.Errorf("%w; retry after reconciliation", errSourceChangedAfterIndex)
	}
	content, err := lineRangeBody(body, startLine, endLine)
	if err != nil {
		return nil, err
	}
	maxReadBytes := selected.Effective.Packing.MaxBytes
	if maxReadBytes < 1 {
		maxReadBytes = service.effectiveSettings().Budgets.MaxBytes
	}
	if len([]byte(content)) > maxReadBytes || EstimateTokens(content) > 8192 {
		return nil, fmt.Errorf(
			"read passage exceeds the %d-byte or 8192-token return ceiling; "+
				"request a smaller explicit startLine/endLine range",
			maxReadBytes,
		)
	}
	if chunkID != "" && SHA256String(content) != expectedHash {
		return nil, fmt.Errorf("%w; chunk content no longer matches", errSourceChangedAfterIndex)
	}
	citation := Citation{
		Path: path, Heading: heading, StartLine: startLine, EndLine: endLine,
		ContentHash: SHA256String(content), SourceHash: fileHash,
		PassageHash: SHA256String(content), Tier: tier,
		URI: "re-discipline://" + generation.ID + "/chunks/" + url.PathEscape(chunkID),
	}
	if chunkID == "" {
		if startLine == 0 {
			citation.StartLine = 1
		}
		if endLine == 0 {
			lines := strings.Split(string(normalizeNewlines(body)), "\n")
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			citation.EndLine = len(lines)
		}
		citation.URI = "re-discipline://" + generation.ID + "/sources/" + url.PathEscape(path)
	}
	replay, _ := CanonicalDigest(struct {
		Generation string
		Path       string
		Start      int
		End        int
	}{generation.ID, path, citation.StartLine, citation.EndLine})
	replay = compactReplayHandle(replay)
	return map[string]any{
		"passage":  content,
		"citation": citation,
		"metadata": (Retriever{
			Boundary: service.Boundary, Generation: generation, Profile: selected,
		}).metadata(replay),
	}, nil
}

func (service *Service) RecallPropose(
	ctx context.Context,
	title string,
	content string,
	sourceLinks []string,
) (map[string]any, error) {
	if !service.Configuration.Valid {
		return nil, errors.New("memory proposals are disabled while configuration is invalid")
	}
	if service.Configuration.Bootstrap.Memory.WritePolicy != "proposal-only" ||
		service.Configuration.Bootstrap.Memory.Mode == "native" {
		return nil, errors.New("shared memory proposals are disabled by project policy")
	}
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" || len([]byte(title)) > 120 || content == "" || len([]byte(content)) > 8000 {
		return nil, errors.New("proposal title or content is outside safe bounds")
	}
	if strings.IndexFunc(title, unicode.IsControl) >= 0 || strings.ContainsRune(content, '\x00') {
		return nil, errors.New("proposal title or content contains forbidden control characters")
	}
	if len(sourceLinks) > 20 {
		return nil, errors.New("at most 20 source links are allowed")
	}
	normalizedLinks := make([]string, 0, len(sourceLinks))
	for _, rawLink := range sourceLinks {
		normalized := strings.ReplaceAll(strings.TrimSpace(rawLink), "\\", "/")
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(normalized)))
		if normalized == "" || clean != normalized || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) ||
			strings.IndexFunc(clean, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("source link %q is not a normalized project-relative path", rawLink)
		}
		normalizedLinks = append(normalizedLinks, clean)
	}
	links := SortedUnique(normalizedLinks)
	if reason := SensitiveContentReason(title + "\n" + content + "\n" +
		strings.Join(links, "\n")); reason != "" {
		return nil, errors.New(reason + "; memory proposal rejected")
	}
	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	for _, link := range links {
		if IsForbiddenSource(link) {
			return nil, fmt.Errorf("source link %q belongs to an excluded class", link)
		}
		if _, err := service.Boundary.Resolve(link, true); err != nil {
			return nil, fmt.Errorf("invalid source link %q: %w", link, err)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE path=?`,
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(link)))).Scan(&count); err != nil || count != 1 {
			return nil, fmt.Errorf("source link %q is not in the active managed knowledge corpus", link)
		}
	}
	input := struct {
		Title   string
		Content string
		Links   []string
	}{title, content, links}
	digest, _ := CanonicalDigest(input)
	fileName := SafeSlug(title) + "-" + strings.TrimPrefix(digest, "sha256:")[:12] + ".md"
	relative := filepath.ToSlash(filepath.Join(".re-discipline", "memory", "proposals", fileName))
	absolute, err := service.Boundary.Resolve(relative, false)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, err
	}
	var builder strings.Builder
	builder.WriteString("# Memory proposal: " + title + "\n\n")
	builder.WriteString("Status: pending\n\n")
	builder.WriteString("Proposal digest: `" + digest + "`\n\n")
	if len(links) > 0 {
		builder.WriteString("## Durable source links\n\n")
		for _, link := range links {
			builder.WriteString("- `" + strings.ReplaceAll(link, "`", "") + "`\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Candidate recall\n\n" + content + "\n")
	proposalBody := []byte(builder.String())
	created, err := AtomicWriteExclusive(absolute, proposalBody, 0o600)
	if err != nil {
		return nil, err
	}
	if !created {
		existing, readErr := os.ReadFile(absolute)
		if readErr != nil || !bytes.Equal(existing, proposalBody) {
			return nil, errors.New("existing memory proposal differs from deterministic content")
		}
	}
	return map[string]any{
		"path": relative, "digest": digest, "created": created,
		"governance": "pending proposal; excluded from normal retrieval; no acceptance occurred",
	}, nil
}

func (service *Service) MaterializeContextPack(path string, pack ContextPack) error {
	return service.MaterializeContextPackExpected(
		path, pack, pack.Digest, pack.PackID)
}

func (service *Service) MaterializeContextPackExpected(
	path string,
	pack ContextPack,
	expectedDigest string,
	expectedPackID string,
) error {
	if !service.Configuration.Valid {
		return fmt.Errorf(
			"context-pack materialization is disabled while project "+
				"configuration is invalid: %s",
			strings.Join(service.Configuration.Errors, "; "),
		)
	}
	if expectedDigest == "" {
		return errors.New(
			"context-pack materialization requires an independently expected digest")
	}
	if _, err := VerifyContextPackValueExpected(
		pack, expectedDigest, expectedPackID,
	); err != nil {
		return fmt.Errorf("refuse invalid context pack: %w", err)
	}
	absolute, err := service.Boundary.Resolve(path, false)
	if err != nil {
		return err
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	parts := strings.Split(clean, "/")
	valid := false
	if len(parts) == 5 && parts[0] == "active" && parts[2] == "subagents" &&
		managedSlugRE.MatchString(parts[1]) && workspaceIDRE.MatchString(parts[3]) &&
		parts[4] == "context-pack.json" {
		valid = true
	}
	if len(parts) == 7 && parts[0] == ".re-discipline" && parts[1] == "agents" &&
		parts[2] == "recruiting" && managedSlugRE.MatchString(parts[3]) &&
		parts[4] == "runs" && workspaceIDRE.MatchString(parts[5]) &&
		parts[6] == "context-pack.json" {
		valid = true
	}
	if !valid {
		return errors.New("context packs may only be materialized into managed drafter workspaces")
	}
	body, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	created, err := AtomicWriteExclusive(absolute, body, 0o600)
	if err != nil {
		return err
	}
	if !created {
		verified, verifyErr := VerifyContextPack(
			absolute, expectedDigest, expectedPackID)
		if verifyErr == nil && verified["digest"] == pack.Digest {
			return nil
		}
		return errors.New("existing context-pack.json differs or fails verification")
	}
	return nil
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func stableJSON(value any) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nowRunID(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, time.Now().UTC().Format("20060102T150405.000000000Z"))
}
