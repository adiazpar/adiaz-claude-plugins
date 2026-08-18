package knowledge

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// The migration verifier cannot trust a caller-selected asset root. The
// model/runtime contract used for certification is therefore compiled beside
// the already-embedded migration retrieval baseline.
//
//go:embed migration_templates/model-manifest.json
var migrationModelManifestTemplate []byte

// MigrationCertificationEnvironment is the exact read-only project and
// packaged-runtime identity against which migration evidence is replayed.
// Evidence producers may inspect it, but the gate verifier always rederives it
// independently at recording and verification time.
type MigrationCertificationEnvironment struct {
	ProjectRoot                    string
	CorpusFingerprint              string
	EvalFingerprint                string
	FindingSuiteDigests            []string
	CalibrationFindingSuiteDigests []string
	ModelFingerprint               string
	CalibrationModelFingerprint    string
	ParserVersion                  string
	ChunkerVersion                 string
	DocumentCount                  int
	ChunkCount                     int
	Runtime                        RuntimeIdentity
	RuntimeContract                RuntimeContractIdentity
	Bootstrap                      BootstrapConfig
	Settings                       KnowledgeSettings
	EvalCases                      []EvalCase
	FindingSuites                  []FindingEvalSuite
	ModelsByProfile                map[string][]ModelIdentity
	documentsByPath                map[string]SourceDocument
	chunksByID                     map[string]Chunk
	findingsByID                   map[string]FindingDocument
	replayGeneration               *Generation
	replayService                  *Service
	caseReplay                     map[string]CaseOutcome
	contextReplay                  map[string]ContextPackOutcome
	certificationAssetRoot         string
}

// InspectMigrationCertificationEnvironment rederives the exact canonical
// corpus, evaluation, model, parser, chunker, and runtime identities required
// by migration certification. It never reads an index generation and never
// writes project state.
func InspectMigrationCertificationEnvironment(
	projectRoot string,
) (MigrationCertificationEnvironment, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	configuration := LoadConfiguration(boundary.Root)
	if configuration.Unsafe || !configuration.Valid {
		return MigrationCertificationEnvironment{}, fmt.Errorf(
			"migration certification requires valid current configuration: %s",
			stringsOr(configuration.Errors, "configuration is invalid"),
		)
	}
	inventory, err := DiscoverSources(boundary, configuration.Settings)
	if err != nil {
		return MigrationCertificationEnvironment{}, fmt.Errorf(
			"discover current certification corpus: %w", err)
	}
	documentsByPath := make(map[string]SourceDocument, len(inventory.Documents))
	for _, document := range inventory.Documents {
		if _, duplicate := documentsByPath[document.Path]; duplicate {
			return MigrationCertificationEnvironment{}, fmt.Errorf(
				"current certification corpus repeats document path %s", document.Path)
		}
		documentsByPath[document.Path] = document
	}
	chunksByID := make(map[string]Chunk, len(inventory.Chunks))
	for _, chunk := range inventory.Chunks {
		if _, duplicate := chunksByID[chunk.ID]; duplicate {
			return MigrationCertificationEnvironment{}, fmt.Errorf(
				"current certification corpus repeats chunk id %s", chunk.ID)
		}
		chunksByID[chunk.ID] = chunk
	}
	findingsByID := make(map[string]FindingDocument, len(inventory.Findings))
	for _, finding := range inventory.Findings {
		if _, duplicate := findingsByID[finding.Record.ID]; duplicate {
			return MigrationCertificationEnvironment{}, fmt.Errorf(
				"current certification corpus repeats finding id %s", finding.Record.ID)
		}
		findingsByID[finding.Record.ID] = finding
	}

	service := &Service{Boundary: boundary}
	cases, err := service.loadProjectEvalCases()
	if err != nil {
		return MigrationCertificationEnvironment{}, fmt.Errorf(
			"load current project evaluation corpus: %w", err)
	}
	findingSuites, err := service.loadProjectFindingEvalSuites()
	if err != nil {
		return MigrationCertificationEnvironment{}, fmt.Errorf(
			"load current project finding evaluation corpus: %w", err)
	}
	evalFingerprint, err := migrationProjectEvalFingerprint(cases, findingSuites)
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	findingSuiteDigests := make([]string, 0, len(findingSuites))
	for _, suite := range findingSuites {
		findingSuiteDigests = append(findingSuiteDigests, suite.Digest)
	}
	calibrationFindingSuiteDigests := SortedUnique(findingSuiteDigests)
	if len(calibrationFindingSuiteDigests) == 0 {
		// CalibrationReport omits an empty findingSuiteDigests field. Preserve
		// the same nil representation here so an honest JSON round trip cannot
		// make an empty finding corpus look stale.
		calibrationFindingSuiteDigests = nil
	}

	manifest, err := migrationPackagedModelManifest()
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	runtimeIdentity, err := ProbeRuntimeIdentity(manifest)
	if err != nil {
		return MigrationCertificationEnvironment{}, fmt.Errorf(
			"probe current certification runtime: %w", err)
	}
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	modelsByProfile, err := migrationModelsByProfile(baseline.Profile, manifest)
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	runtimeContract := RuntimeContract(runtimeIdentity)
	runtimeDigest, err := CanonicalDigest(runtimeContract)
	if err != nil {
		return MigrationCertificationEnvironment{}, err
	}
	for _, row := range baseline.Profile.EffectiveProfiles {
		models := modelsByProfile[row.Name]
		modelDigest, digestErr := CanonicalDigest(models)
		if digestErr != nil || row.Benchmark.ModelFingerprint != modelDigest ||
			row.Benchmark.RuntimeFingerprint != runtimeDigest ||
			row.Benchmark.ParserVersion != ParserVersion ||
			row.Benchmark.ChunkerVersion != ChunkerVersion {
			return MigrationCertificationEnvironment{}, fmt.Errorf(
				"embedded packaged profile evidence for %s is stale", row.Name)
		}
	}

	return MigrationCertificationEnvironment{
		ProjectRoot:       boundary.Root,
		CorpusFingerprint: inventory.Fingerprint, EvalFingerprint: evalFingerprint,
		FindingSuiteDigests:            append([]string(nil), findingSuiteDigests...),
		CalibrationFindingSuiteDigests: calibrationFindingSuiteDigests,
		ModelFingerprint:               modelIndexFingerprint(manifest),
		CalibrationModelFingerprint:    mustDigest(manifest.Models),
		ParserVersion:                  ParserVersion,
		ChunkerVersion:                 ChunkerVersion, DocumentCount: len(inventory.Documents),
		ChunkCount: len(inventory.Chunks), Runtime: runtimeIdentity,
		RuntimeContract: runtimeContract, Bootstrap: configuration.Bootstrap,
		Settings: configuration.Settings, EvalCases: append([]EvalCase(nil), cases...),
		FindingSuites:   append([]FindingEvalSuite(nil), findingSuites...),
		ModelsByProfile: modelsByProfile,
		documentsByPath: documentsByPath, chunksByID: chunksByID,
		findingsByID: findingsByID,
	}, nil
}

// prepareCertificationReplay builds one isolated, disposable generation from
// the exact source inventory certification just inspected. Passage, context,
// and finding evidence are all replayed against this generation; no reported
// replay boolean or token total is accepted without executing the bound
// runtime again. The cache lives outside the project and is removed before
// gate verification returns.
func (environment *MigrationCertificationEnvironment) prepareCertificationReplay() (
	func(),
	error,
) {
	cacheRoot, err := os.MkdirTemp("", "re-discipline-migration-certification-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(cacheRoot) }
	embeddedManifest, err := migrationPackagedModelManifest()
	if err != nil {
		cleanup()
		return nil, err
	}
	if strings.TrimSpace(environment.certificationAssetRoot) == "" {
		cleanup()
		return nil, errors.New(
			"finding certification requires the packaged knowledge asset root")
	}
	manifest, err := LoadModelManifest(environment.certificationAssetRoot)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("load exact packaged finding replay model: %w", err)
	}
	if !reflect.DeepEqual(manifest.Runtime, embeddedManifest.Runtime) ||
		!reflect.DeepEqual(manifest.Models, embeddedManifest.Models) ||
		!reflect.DeepEqual(
			manifest.ExternalModelPolicy, embeddedManifest.ExternalModelPolicy) ||
		modelIndexFingerprint(manifest) != environment.ModelFingerprint {
		cleanup()
		return nil, errors.New(
			"finding certification asset model does not equal the embedded packaged identity")
	}
	boundary, err := NewBoundary(environment.ProjectRoot)
	if err != nil {
		cleanup()
		return nil, err
	}
	manager := IndexManager{
		Boundary: boundary, CacheRoot: filepath.Join(cacheRoot, "knowledge"),
		Settings: environment.Settings, Manifest: manifest,
	}
	generation, inventory, _, err := manager.Ensure(context.Background())
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("build isolated finding certification replay: %w", err)
	}
	if generation.CorpusFingerprint != environment.CorpusFingerprint ||
		inventory.Fingerprint != environment.CorpusFingerprint ||
		generation.ModelFingerprint != environment.ModelFingerprint ||
		generation.ParserVersion != environment.ParserVersion ||
		generation.ChunkerVersion != environment.ChunkerVersion ||
		!reflect.DeepEqual(RuntimeContract(generation.Runtime), environment.RuntimeContract) ||
		generation.ServingStale || generation.WriterContention {
		cleanup()
		return nil, errors.New(
			"isolated finding certification replay diverged from the inspected environment")
	}
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		cleanup()
		return nil, err
	}
	service := &Service{
		Boundary: boundary, AssetRoot: environment.certificationAssetRoot,
		Configuration: Configuration{
			Valid: true, Bootstrap: environment.Bootstrap, Settings: environment.Settings,
		},
		ProfileCatalog: baseline.Profile, ModelManifest: manifest, Index: manager,
		contextLeases: map[string]*contextLeaseState{},
	}
	service.PinGeneration(generation)
	environment.replayGeneration = &generation
	environment.replayService = service
	environment.caseReplay = map[string]CaseOutcome{}
	environment.contextReplay = map[string]ContextPackOutcome{}
	return cleanup, nil
}

func stringsOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, "; ")
}

func migrationProjectEvalFingerprint(
	cases []EvalCase,
	findingSuites []FindingEvalSuite,
) (string, error) {
	if len(findingSuites) == 0 {
		return CanonicalDigest(cases)
	}
	return CanonicalDigest(struct {
		PassageCases  []EvalCase         `json:"passageCases"`
		FindingSuites []FindingEvalSuite `json:"findingSuites"`
	}{cases, findingSuites})
}

func migrationPackagedModelManifest() (ModelManifest, error) {
	var manifest ModelManifest
	if err := decodeStrict(migrationModelManifestTemplate, &manifest); err != nil {
		return ModelManifest{}, fmt.Errorf("decode embedded migration model manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Runtime.Implementation != "re-discipline-knowledge-go" ||
		manifest.Runtime.Version != RuntimeVersion ||
		manifest.Runtime.SQLiteDriver != "modernc.org/sqlite@v1.54.0" ||
		manifest.Runtime.NumericalBackend != "fixed-int64-v1" ||
		manifest.Runtime.TieBreaker != "score-desc,path-asc,start-line-asc,chunk-id-asc" ||
		len(manifest.Models) == 0 {
		return ModelManifest{}, errors.New("embedded migration model manifest identity is invalid")
	}
	manifest.ExecutableModels = map[string]ModelIdentity{}
	manifest.UnavailableModels = map[string]string{}
	seen := map[string]bool{}
	for _, model := range manifest.Models {
		if seen[model.ID] || !profileIdentityRE.MatchString(model.ID) ||
			!hexDigestRE.MatchString(model.SpecSHA256) ||
			model.NetworkRequired || model.Dimensions < 1 || model.Dimensions > 4096 ||
			(model.Implementation != "builtin" && model.Implementation != "bundled-local") {
			return ModelManifest{}, fmt.Errorf(
				"embedded migration model %q is not an executable packaged model", model.ID)
		}
		if model.Implementation == "bundled-local" &&
			!hexDigestRE.MatchString(model.ArtifactSHA256) {
			return ModelManifest{}, fmt.Errorf(
				"embedded migration model %q lacks a pinned artifact", model.ID)
		}
		manifest.ExecutableModels[model.ID] = modelIdentity(
			model, manifest.Runtime.NumericalBackend)
		seen[model.ID] = true
	}
	return manifest, nil
}

func migrationModelsByProfile(
	profile RetrievalProfile,
	manifest ModelManifest,
) (map[string][]ModelIdentity, error) {
	models := map[string]ModelIdentity{}
	for id, identity := range manifest.ExecutableModels {
		models[id] = identity
	}
	result := map[string][]ModelIdentity{}
	for _, row := range profile.EffectiveProfiles {
		selected := []ModelIdentity{}
		if row.Requires.Embedding != nil {
			identity, ok := models[*row.Requires.Embedding]
			if !ok {
				return nil, fmt.Errorf(
					"embedded profile %s requires unavailable model %s",
					row.Name, *row.Requires.Embedding)
			}
			selected = append(selected, identity)
		}
		sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
		result[row.Name] = selected
	}
	if len(result) != len(profile.EffectiveProfiles) || reflect.DeepEqual(result, map[string][]ModelIdentity{}) {
		return nil, errors.New("embedded profile has no effective model mapping")
	}
	return result, nil
}
