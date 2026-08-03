package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	packageSchemaVersion       = 1
	runtimeName                = "re-discipline-knowledge"
	runtimeVersion             = "0.8.0"
	runtimeBuildIDPath         = "github.com/adiaz/re-discipline-knowledge/internal/knowledge.CompiledBuildID"
	windowsLauncherBuildIDPath = "main.CompiledBuildID"
	projectLaneMeasurementPath = "evals/conformance/project-lane-ablation.json"
	windowsArtifactMode        = 0o644
)

type targetSpec struct {
	GOOS   string
	GOARCH string
}

var supportedTargets = []targetSpec{
	{GOOS: "windows", GOARCH: "amd64"},
	{GOOS: "windows", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
}

var windowsLauncherTarget = targetSpec{GOOS: "windows", GOARCH: "amd64"}

var sharedAssetRoots = []string{
	"evals/conformance",
	"models",
	"profiles",
	"schemas",
}

type packageManifest struct {
	Schema        string          `json:"$schema"`
	SchemaVersion int             `json:"schemaVersion"`
	Runtime       manifestRuntime `json:"runtime"`
	Build         manifestBuild   `json:"build"`
	Targets       []manifestFile  `json:"targets"`
	Launchers     []manifestFile  `json:"launchers"`
	SharedAssets  []manifestFile  `json:"sharedAssets"`
	Notices       manifestFile    `json:"notices"`
}

type manifestRuntime struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	BuildID string `json:"buildId"`
}

type manifestBuild struct {
	GoToolchain string   `json:"goToolchain"`
	CGOEnabled  bool     `json:"cgoEnabled"`
	Flags       []string `json:"flags"`
	Environment []string `json:"environment"`
	TargetOrder string   `json:"targetOrder"`
}

type manifestFile struct {
	Kind   string `json:"kind"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode"`
}

type packagedModelManifest struct {
	Models []struct {
		ID             string `json:"id"`
		Role           string `json:"role"`
		Revision       string `json:"revision"`
		Implementation string `json:"implementation"`
		SpecFile       string `json:"specFile"`
		SpecSHA256     string `json:"specSha256"`
		ArtifactFile   string `json:"artifactFile"`
		ArtifactSHA256 string `json:"artifactSha256"`
		License        string `json:"license"`
	} `json:"models"`
}

type packagedRetrievalProfile struct {
	EffectiveProfiles []struct {
		Name      string   `json:"name"`
		Lanes     []string `json:"lanes"`
		Benchmark struct {
			Suite              string `json:"suite"`
			Digest             string `json:"digest"`
			Status             string `json:"status"`
			EvalFingerprint    string `json:"evalFingerprint"`
			CorpusFingerprint  string `json:"corpusFingerprint"`
			ModelFingerprint   string `json:"modelFingerprint"`
			RuntimeFingerprint string `json:"runtimeFingerprint"`
		} `json:"benchmark"`
	} `json:"effectiveProfiles"`
}

type laneAblationDecision struct {
	SchemaVersion      int                                `json:"schemaVersion"`
	Suite              string                             `json:"suite"`
	EvaluatedAt        string                             `json:"evaluatedAt"`
	EvalFingerprint    string                             `json:"evalFingerprint"`
	CorpusFingerprint  string                             `json:"corpusFingerprint"`
	RuntimeFingerprint string                             `json:"runtimeFingerprint"`
	ReportDigest       string                             `json:"reportDigest"`
	EvidenceLayers     laneAblationDecisionLayers         `json:"evidenceLayers"`
	Lanes              map[string]laneAblationFinalChoice `json:"lanes"`
	Conclusion         string                             `json:"conclusion"`
}

type laneAblationDecisionLayers struct {
	PackagedConformance laneAblationPackagedDecisionLayer `json:"packagedConformance"`
	ProjectCorpus       laneAblationProjectDecisionLayer  `json:"projectCorpus"`
}

type laneAblationPackagedDecisionLayer struct {
	Status       string                                `json:"status"`
	HoldoutCases int                                   `json:"holdoutCases"`
	Lanes        map[string]laneAblationPackagedCounts `json:"lanes"`
}

type laneAblationPackagedCounts struct {
	UniqueFirst int `json:"uniqueFirst"`
	Improved    int `json:"improved"`
	Degraded    int `json:"degraded"`
}

type laneAblationProjectDecisionLayer struct {
	Status                   string                               `json:"status"`
	CasesPerArm              int                                  `json:"casesPerArm"`
	RawBenchmarkSHA256       string                               `json:"rawBenchmarkSha256"`
	ProjectMeasurementPath   string                               `json:"projectMeasurementPath"`
	ProjectMeasurementSHA256 string                               `json:"projectMeasurementSha256"`
	Lanes                    map[string]laneAblationProjectCounts `json:"lanes"`
}

type laneAblationProjectCounts struct {
	Rescues                  int `json:"rescues"`
	Losses                   int `json:"losses"`
	RankImprovements         int `json:"rankImprovements"`
	RankDegradations         int `json:"rankDegradations"`
	AddedHardGateRegressions int `json:"addedHardGateRegressions"`
}

type laneAblationFinalChoice struct {
	Decision string `json:"decision"`
	Basis    string `json:"basis"`
}

type laneAblationReport struct {
	SchemaVersion      int                         `json:"schemaVersion"`
	Suite              string                      `json:"suite"`
	EvaluatedAt        string                      `json:"evaluatedAt"`
	SourceRevision     string                      `json:"sourceRevision"`
	EvalFingerprint    string                      `json:"evalFingerprint"`
	CorpusFingerprint  string                      `json:"corpusFingerprint"`
	RuntimeFingerprint string                      `json:"runtimeFingerprint"`
	ParserVersion      string                      `json:"parserVersion"`
	ChunkerVersion     string                      `json:"chunkerVersion"`
	Profiles           []laneAblationProfile       `json:"profiles"`
	Models             []laneAblationModel         `json:"models"`
	Cases              []laneAblationCaseOutcome   `json:"cases"`
	ProjectEvidence    laneAblationProjectEvidence `json:"projectEvidence"`
}

type laneAblationProjectEvidence struct {
	Suite                          string                         `json:"suite"`
	Project                        string                         `json:"project"`
	Status                         string                         `json:"status"`
	EvaluatedAt                    string                         `json:"evaluatedAt"`
	FrozenSourceRevision           string                         `json:"frozenSourceRevision"`
	RawBenchmarkSHA256             string                         `json:"rawBenchmarkSha256"`
	RawBenchmarkRunID              string                         `json:"rawBenchmarkRunId"`
	Complete                       bool                           `json:"complete"`
	ArmCount                       int                            `json:"armCount"`
	CasesPerArm                    int                            `json:"casesPerArm"`
	CorpusFingerprint              string                         `json:"corpusFingerprint"`
	EvalFingerprint                string                         `json:"evalFingerprint"`
	ProjectionTransformDigest      string                         `json:"projectionTransformDigest"`
	ProjectMeasurementPath         string                         `json:"projectMeasurementPath"`
	ProjectMeasurementSHA256       string                         `json:"projectMeasurementSha256"`
	IndexedSourceBytesUnchanged    bool                           `json:"indexedSourceBytesUnchanged"`
	SharedHardGateFailures         int                            `json:"sharedHardGateFailures"`
	DenseAddedHardGateRegressions  int                            `json:"denseAddedHardGateRegressions"`
	RerankAddedHardGateRegressions int                            `json:"rerankAddedHardGateRegressions"`
	Dense                          laneAblationProjectLaneMetric  `json:"dense"`
	Rerank                         laneAblationProjectLaneMetric  `json:"rerank"`
	RescueCases                    []laneAblationProjectRescue    `json:"rescueCases"`
	Uncertainty                    laneAblationProjectUncertainty `json:"uncertainty"`
}

type laneAblationProjectLaneMetric struct {
	Rescues                      int     `json:"rescues"`
	Losses                       int     `json:"losses"`
	RankImprovements             int     `json:"rankImprovements"`
	RankDegradations             int     `json:"rankDegradations"`
	PathsChanged                 int     `json:"pathsChanged,omitempty"`
	OverallRecallDelta           float64 `json:"overallRecallDelta,omitempty"`
	HoldoutRecallDelta           float64 `json:"holdoutRecallDelta,omitempty"`
	OverallCompleteEvidenceDelta float64 `json:"overallCompleteEvidenceDelta,omitempty"`
	HoldoutCompleteEvidenceDelta float64 `json:"holdoutCompleteEvidenceDelta,omitempty"`
}

type laneAblationProjectRescue struct {
	CaseID                   string `json:"caseId"`
	Split                    string `json:"split"`
	Topic                    string `json:"topic"`
	TargetPath               string `json:"targetPath"`
	BaselineRelevantRank     int    `json:"baselineRelevantRank"`
	DenseRelevantRank        int    `json:"denseRelevantRank"`
	TargetDisjoint           bool   `json:"targetDisjoint"`
	SourceClass              string `json:"sourceClass"`
	AddedHardGateRegressions int    `json:"addedHardGateRegressions"`
}

type laneAblationProjectUncertainty struct {
	WilsonIntervals                  []laneAblationWilsonInterval `json:"wilsonIntervals"`
	TopicBootstrap                   laneAblationTopicBootstrap   `json:"topicBootstrap"`
	LeaveOneTopicOutZeroWhenOmitting []string                     `json:"leaveOneTopicOutZeroWhenOmitting"`
	ClusterFragile                   bool                         `json:"clusterFragile"`
	FinalCorpusRerunRequired         bool                         `json:"finalCorpusRerunRequired"`
}

type laneAblationWilsonInterval struct {
	Slice     string  `json:"slice"`
	Successes int     `json:"successes"`
	Trials    int     `json:"trials"`
	Estimate  float64 `json:"estimate"`
	Lower     float64 `json:"lower"`
	Upper     float64 `json:"upper"`
}

type laneAblationTopicBootstrap struct {
	Replicates          int     `json:"replicates"`
	PointEstimate       float64 `json:"pointEstimate"`
	Lower               float64 `json:"lower"`
	Upper               float64 `json:"upper"`
	ZeroOrLowerFraction float64 `json:"zeroOrLowerFraction"`
}

type laneAblationProfile struct {
	Role             string   `json:"role"`
	Name             string   `json:"name"`
	Lanes            []string `json:"lanes"`
	BenchmarkDigest  string   `json:"benchmarkDigest"`
	ModelFingerprint string   `json:"modelFingerprint"`
}

type laneAblationModel struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	Revision       string `json:"revision"`
	Implementation string `json:"implementation"`
	SpecSHA256     string `json:"specSha256"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
}

type laneAblationCaseOutcome struct {
	CaseID   string                   `json:"caseId"`
	Baseline laneAblationQueryOutcome `json:"baseline"`
	Dense    laneAblationQueryOutcome `json:"dense"`
	Rerank   laneAblationQueryOutcome `json:"rerank"`
}

type laneAblationQueryOutcome struct {
	RelevantHit  bool     `json:"relevantHit"`
	UniqueFirst  bool     `json:"uniqueFirst"`
	RelevantRank int      `json:"relevantRank"`
	FindingIDs   []string `json:"findingIds"`
}

type laneAblationCounts struct {
	UniqueFirst int
	Improved    int
	Degraded    int
}

type projectLaneMeasurement struct {
	Schema           string                         `json:"$schema"`
	SchemaVersion    int                            `json:"schemaVersion"`
	Kind             string                         `json:"kind"`
	MeasurementOnly  bool                           `json:"measurementOnly"`
	Project          string                         `json:"project"`
	EvaluatedAt      string                         `json:"evaluatedAt"`
	Provenance       projectLaneProvenance          `json:"provenance"`
	Corpus           projectLaneCorpus              `json:"corpus"`
	Harness          projectLaneHarness             `json:"harness"`
	Models           []projectLaneModel             `json:"models"`
	Profiles         []projectLaneProfile           `json:"profiles"`
	Cases            []projectLaneCase              `json:"cases"`
	Slices           json.RawMessage                `json:"slices"`
	Uncertainty      projectLaneUncertainty         `json:"uncertainty"`
	Sensitivity      projectLaneSensitivity         `json:"sensitivity"`
	HistoricalRerank projectLaneHistoricalRerank    `json:"historicalRerank"`
	Validation       json.RawMessage                `json:"validation"`
	Decision         projectLaneMeasurementDecision `json:"decision"`
}

type projectLaneProvenance struct {
	FrozenSourceRevision       string `json:"frozenSourceRevision"`
	ProjectGitRevision         string `json:"projectGitRevision"`
	ProjectDirtyFingerprint    string `json:"projectDirtyFingerprint"`
	FrozenRuntimeFingerprint   string `json:"frozenRuntimeFingerprint"`
	RawBenchmarkSHA256         string `json:"rawBenchmarkSha256"`
	RawBenchmarkRunID          string `json:"rawBenchmarkRunId"`
	RawBenchmarkComplete       bool   `json:"rawBenchmarkComplete"`
	RawBenchmarkArmCount       int    `json:"rawBenchmarkArmCount"`
	RawBenchmarkCasesPerArm    int    `json:"rawBenchmarkCasesPerArm"`
	RawBenchmarkDurationMillis int    `json:"rawBenchmarkDurationMillis"`
	ParserVersion              string `json:"parserVersion"`
	ChunkerVersion             string `json:"chunkerVersion"`
	ProfileCatalogSHA256       string `json:"profileCatalogSha256"`
	ModelManifestSHA256        string `json:"modelManifestSha256"`
}

type projectLaneCorpus struct {
	GenerationID                     string          `json:"generationId"`
	CorpusFingerprint                string          `json:"corpusFingerprint"`
	DocumentCount                    int             `json:"documentCount"`
	ChunkCount                       int             `json:"chunkCount"`
	IndexedSourceProof               json.RawMessage `json:"indexedSourceProof"`
	FinalEvalFileCount               int             `json:"finalEvalFileCount"`
	FinalEvalCaseCount               int             `json:"finalEvalCaseCount"`
	FinalEvalFiles                   json.RawMessage `json:"finalEvalFiles"`
	ProjectedEvalFingerprint         string          `json:"projectedEvalFingerprint"`
	ProjectionManifestSHA256         string          `json:"projectionManifestSha256"`
	ProjectionTransformDigest        string          `json:"projectionTransformDigest"`
	ProjectionOperation              string          `json:"projectionOperation"`
	ProjectionRemovedOccurrences     int             `json:"projectionRemovedOccurrences"`
	ProjectionPreservesAllOtherBytes bool            `json:"projectionPreservesAllOtherBytes"`
}

type projectLaneHarness struct {
	SourceRepositoryMutated          bool            `json:"sourceRepositoryMutated"`
	IndexedSourceBytesUnchanged      bool            `json:"indexedSourceBytesUnchanged"`
	ReceiptSHA256                    string          `json:"receiptSha256"`
	PluginRevision                   string          `json:"pluginRevision"`
	ProjectRevision                  string          `json:"projectRevision"`
	BenchmarkCommandSHA256           string          `json:"benchmarkCommandSha256"`
	ProjectionManifestArtifactSHA256 string          `json:"projectionManifestArtifactSha256"`
	IndexedSourcesManifestSHA256     string          `json:"indexedSourcesManifestSha256"`
	ControlPlaneSubstitutions        json.RawMessage `json:"controlPlaneSubstitutions"`
	RenamedPaths                     json.RawMessage `json:"renamedPaths"`
	ExcludedSourcePaths              json.RawMessage `json:"excludedSourcePaths"`
	NegativeControls                 json.RawMessage `json:"negativeControls"`
}

type projectLaneModel struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	Revision       string `json:"revision"`
	Implementation string `json:"implementation"`
	SpecSHA256     string `json:"specSha256"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
}

type projectLaneProfile struct {
	Role                 string                        `json:"role"`
	Name                 string                        `json:"name"`
	ActiveLanes          []string                      `json:"activeLanes"`
	EffectiveIdentity    string                        `json:"effectiveIdentity"`
	ObservationDigest    string                        `json:"observationDigest"`
	ModelIDs             []string                      `json:"modelIds"`
	HardGatesPassed      bool                          `json:"hardGatesPassed"`
	NonInferiorToLexical bool                          `json:"nonInferiorToLexical"`
	Metrics              map[string]float64            `json:"metrics"`
	MetricsBySplit       map[string]map[string]float64 `json:"metricsBySplit"`
}

type projectLaneOutcome struct {
	CaseID                 string   `json:"caseId"`
	Split                  string   `json:"split"`
	Topic                  string   `json:"topic"`
	Paths                  []string `json:"paths"`
	Tiers                  []string `json:"tiers"`
	ChunkIDs               []string `json:"chunkIds"`
	ContentHashes          []string `json:"contentHashes"`
	RelevantPaths          []string `json:"relevantPaths"`
	RelevantRanks          []int    `json:"relevantRanks"`
	HardNegativeHits       []string `json:"hardNegativeHits"`
	ExpectedCitationsFound []string `json:"expectedCitationsFound"`
	EstimatedTokens        int      `json:"estimatedTokens"`
	ReturnedUniquePaths    int      `json:"returnedUniquePaths"`
	ExpectedFound          bool     `json:"expectedFound"`
	CompleteEvidence       bool     `json:"completeEvidence"`
	AuthoritySafe          bool     `json:"authoritySafe"`
	CitationMetadataSafe   bool     `json:"citationMetadataSafe"`
	CitationSafe           bool     `json:"citationSafe"`
	CorpusMatched          bool     `json:"corpusMatched"`
	AbstentionCorrect      bool     `json:"abstentionCorrect"`
	BudgetSafe             bool     `json:"budgetSafe"`
	ReplayIdentical        bool     `json:"replayIdentical"`
	MinimumTokenBudget     int      `json:"minimumTokenBudget"`
	QualityGateApplicable  bool     `json:"qualityGateApplicable"`
	SafetyPassed           bool     `json:"safetyPassed"`
	QualityPassed          bool     `json:"qualityPassed"`
	GatePassed             bool     `json:"gatePassed"`
	ReturnedTokens         int      `json:"returnedTokens"`
	RelevantTokens         int      `json:"relevantTokens"`
	DuplicateTokens        int      `json:"duplicateTokens"`
	StaleResults           int      `json:"staleResults"`
	LatencyMillis          int      `json:"latencyMillis"`
}

type projectLaneComparison struct {
	HitEffect    string `json:"hitEffect"`
	RankDelta    int    `json:"rankDelta"`
	PathsChanged bool   `json:"pathsChanged"`
	UniqueRescue bool   `json:"uniqueRescue"`
	RankImproved bool   `json:"rankImproved"`
	RankDegraded bool   `json:"rankDegraded"`
}

type projectLaneCase struct {
	CaseID           string                `json:"caseId"`
	Split            string                `json:"split"`
	Role             string                `json:"role"`
	Topic            string                `json:"topic"`
	QueryClass       string                `json:"queryClass"`
	VocabularyPolicy string                `json:"vocabularyPolicy"`
	Answerable       bool                  `json:"answerable"`
	Baseline         projectLaneOutcome    `json:"baseline"`
	Dense            projectLaneOutcome    `json:"dense"`
	DenseComparison  projectLaneComparison `json:"denseComparison"`
}

type projectLaneWilsonInterval struct {
	Slice      string  `json:"slice"`
	Event      string  `json:"event"`
	Successes  int     `json:"successes"`
	Trials     int     `json:"trials"`
	Confidence float64 `json:"confidence"`
	Estimate   float64 `json:"estimate"`
	Lower      float64 `json:"lower"`
	Upper      float64 `json:"upper"`
}

type projectLaneBootstrap struct {
	Method              string  `json:"method"`
	ClusterKey          string  `json:"clusterKey"`
	ClusterCount        int     `json:"clusterCount"`
	Replicates          int     `json:"replicates"`
	Seed                int     `json:"seed"`
	Estimand            string  `json:"estimand"`
	PointEstimate       float64 `json:"pointEstimate"`
	Lower               float64 `json:"lower"`
	Median              float64 `json:"median"`
	Upper               float64 `json:"upper"`
	ZeroOrLowerFraction float64 `json:"zeroOrLowerFraction"`
}

type projectLaneUncertainty struct {
	WilsonIntervals       []projectLaneWilsonInterval `json:"wilsonIntervals"`
	TopicClusterBootstrap projectLaneBootstrap        `json:"topicClusterBootstrap"`
}

type projectLaneLeaveOneOut struct {
	OmittedTopic      string  `json:"omittedTopic"`
	AnswerableCases   int     `json:"answerableCases"`
	DenseHitRateDelta float64 `json:"denseHitRateDelta"`
}

type projectLaneSensitivity struct {
	BudgetSlices          json.RawMessage          `json:"budgetSlices"`
	BudgetCaseComparisons json.RawMessage          `json:"budgetCaseComparisons"`
	LeaveOneTopicOut      []projectLaneLeaveOneOut `json:"leaveOneTopicOut"`
	DensePathChangedCases int                      `json:"densePathChangedCases"`
	PreliminaryComparison json.RawMessage          `json:"preliminaryComparison"`
}

type projectLaneHistoricalProvenance struct {
	RuntimeSourceRevision      string `json:"runtimeSourceRevision"`
	ProjectGitRevision         string `json:"projectGitRevision"`
	ProjectDirtyFingerprint    string `json:"projectDirtyFingerprint"`
	RuntimeFingerprint         string `json:"runtimeFingerprint"`
	RawBenchmarkSHA256         string `json:"rawBenchmarkSha256"`
	EvidenceArchivePath        string `json:"evidenceArchivePath"`
	EvidenceArchiveSHA256      string `json:"evidenceArchiveSha256"`
	RawBenchmarkByteCount      int64  `json:"rawBenchmarkByteCount"`
	EvidenceArchiveByteCount   int64  `json:"evidenceArchiveByteCount"`
	EvidenceArchiveFormat      string `json:"evidenceArchiveFormat"`
	RawBenchmarkRunID          string `json:"rawBenchmarkRunId"`
	RawBenchmarkComplete       bool   `json:"rawBenchmarkComplete"`
	RawBenchmarkArmCount       int    `json:"rawBenchmarkArmCount"`
	RawBenchmarkCasesPerArm    int    `json:"rawBenchmarkCasesPerArm"`
	RawBenchmarkDurationMillis int    `json:"rawBenchmarkDurationMillis"`
	GenerationID               string `json:"generationId"`
	CorpusFingerprint          string `json:"corpusFingerprint"`
	EvalFingerprint            string `json:"evalFingerprint"`
	ParserVersion              string `json:"parserVersion"`
	ChunkerVersion             string `json:"chunkerVersion"`
	ProfileCatalogSHA256       string `json:"profileCatalogSha256"`
	ModelManifestSHA256        string `json:"modelManifestSha256"`
	ProjectionManifestSHA256   string `json:"projectionManifestSha256"`
	ProjectionTransformDigest  string `json:"projectionTransformDigest"`
}

type projectLaneHistoricalCase struct {
	CaseID           string                `json:"caseId"`
	Split            string                `json:"split"`
	Topic            string                `json:"topic"`
	Answerable       bool                  `json:"answerable"`
	Dense            projectLaneOutcome    `json:"dense"`
	Rerank           projectLaneOutcome    `json:"rerank"`
	RerankComparison projectLaneComparison `json:"rerankComparison"`
}

type projectLaneHistoricalRerank struct {
	Provenance                   projectLaneHistoricalProvenance `json:"provenance"`
	Models                       []projectLaneModel              `json:"models"`
	Profiles                     []projectLaneProfile            `json:"profiles"`
	Cases                        []projectLaneHistoricalCase     `json:"cases"`
	BudgetSlices                 json.RawMessage                 `json:"budgetSlices"`
	BudgetCaseComparisons        json.RawMessage                 `json:"budgetCaseComparisons"`
	SharedSafetyFailures         int                             `json:"sharedSafetyFailures"`
	RerankAddedSafetyRegressions int                             `json:"rerankAddedSafetyRegressions"`
	Decision                     projectLaneDecision             `json:"decision"`
}

type projectLaneDecision struct {
	Action           string   `json:"action"`
	PositiveEvidence bool     `json:"positiveEvidence"`
	EventCount       int      `json:"eventCount"`
	CaseIDs          []string `json:"caseIds"`
	Rationale        string   `json:"rationale"`
}

type projectLaneMeasurementDecision struct {
	Dense                       projectLaneDecision `json:"dense"`
	Rerank                      projectLaneDecision `json:"rerank"`
	ProductionLanes             []string            `json:"productionLanes"`
	SharedSafetyFailures        int                 `json:"sharedSafetyFailures"`
	DenseAddedSafetyRegressions int                 `json:"denseAddedSafetyRegressions"`
	ProductionProfileConsistent bool                `json:"productionProfileConsistent"`
	ReleaseGatePassed           bool                `json:"releaseGatePassed"`
	Rationale                   string              `json:"rationale"`
}

type projectLaneDerived struct {
	Dense             laneAblationProjectLaneMetric
	Rerank            laneAblationProjectLaneMetric
	Rescues           []laneAblationProjectRescue
	ZeroOmittedTopics []string
	ClusterFragile    bool
}

func main() {
	output := flag.String("output", "bin", "runtime package output; only knowledge/bin is supported")
	verify := flag.Bool("verify", false, "verify checksums and a clean reproducible rebuild without changing output")
	flag.Parse()

	if err := run(*output, *verify); err != nil {
		fmt.Fprintln(os.Stderr, "re-discipline-knowledge-packager:", err)
		os.Exit(1)
	}
}

func run(output string, verify bool) error {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	outputRoot, err := validateOutputRoot(moduleRoot, output)
	if err != nil {
		return err
	}
	pinnedGo, err := readPinnedGoVersion(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return err
	}
	buildID, err := computeRuntimeBuildID(moduleRoot)
	if err != nil {
		return err
	}

	if verify {
		return verifyPackage(moduleRoot, outputRoot, pinnedGo, buildID)
	}
	return buildAndInstall(moduleRoot, outputRoot, pinnedGo, buildID)
}

func findModuleRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return "", err
	}
	for {
		goMod := filepath.Join(current, "go.mod")
		if body, readErr := os.ReadFile(goMod); readErr == nil {
			if bytes.Contains(body, []byte("module github.com/adiaz/re-discipline-knowledge")) {
				return filepath.Clean(current), nil
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return "", readErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("could not find the re-discipline knowledge go.mod")
		}
		current = parent
	}
}

func validateOutputRoot(moduleRoot, output string) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "", errors.New("--output cannot be empty")
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(moduleRoot, output)
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(moduleRoot, "bin")
	if !samePath(output, expected) {
		return "", fmt.Errorf("--output must resolve to %s", expected)
	}
	if samePath(output, moduleRoot) || filepath.Dir(output) == output {
		return "", errors.New("refusing unsafe output path")
	}
	return filepath.Clean(output), nil
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func readPinnedGoVersion(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "go" {
			if fields[1] == "" {
				break
			}
			return "go" + fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("go.mod does not declare a Go version")
}

func computeRuntimeBuildID(moduleRoot string) (string, error) {
	paths := []string{
		filepath.Join(moduleRoot, "go.mod"),
		filepath.Join(moduleRoot, "go.sum"),
		filepath.Join(moduleRoot, "THIRD_PARTY_NOTICES.md"),
		filepath.Join(moduleRoot, "..", "LICENSE"),
	}
	for _, directory := range []string{
		filepath.Join(moduleRoot, "cmd", "re-discipline-knowledge"),
		filepath.Join(moduleRoot, "internal", "knowledge"),
	} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("build input cannot be a symlink: %s", path)
			}
			lowerName := strings.ToLower(entry.Name())
			if !entry.IsDir() &&
				strings.HasSuffix(lowerName, ".go") &&
				!strings.HasSuffix(lowerName, "_test.go") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	for _, relativeRoot := range sharedAssetRoots {
		directory := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("build input cannot be a symlink: %s", path)
			}
			if !entry.IsDir() {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("build input is not a regular file: %s", path)
				}
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		left, _ := filepath.Rel(moduleRoot, paths[i])
		right, _ := filepath.Rel(moduleRoot, paths[j])
		return filepath.ToSlash(left) < filepath.ToSlash(right)
	})

	hash := sha256.New()
	for _, path := range paths {
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("build input is not a regular file: %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), len(body))
		if _, err := hash.Write(body); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func buildAndInstall(moduleRoot, outputRoot, pinnedGo, buildID string) error {
	parent := filepath.Dir(outputRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".re-discipline-bin-staging-")
	if err != nil {
		return err
	}
	stagingExists := true
	defer func() {
		if stagingExists {
			_ = os.RemoveAll(staging)
		}
	}()

	if _, err := buildPackageTree(moduleRoot, staging, pinnedGo, buildID); err != nil {
		return err
	}
	if _, _, err := verifyPackageContents(
		moduleRoot, staging, pinnedGo, buildID,
	); err != nil {
		return fmt.Errorf("verify staged package: %w", err)
	}
	if err := installDirectoryAtomically(staging, outputRoot); err != nil {
		return err
	}
	stagingExists = false
	return nil
}

func installDirectoryAtomically(staging, outputRoot string) error {
	parent := filepath.Dir(outputRoot)
	backup, err := uniqueAbsentPath(parent, ".re-discipline-bin-backup-")
	if err != nil {
		return err
	}
	hadOutput := false
	if info, statErr := os.Lstat(outputRoot); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("output must be a real directory: %s", outputRoot)
		}
		if err := os.Rename(outputRoot, backup); err != nil {
			return fmt.Errorf("move prior package aside: %w", err)
		}
		hadOutput = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	if err := os.Rename(staging, outputRoot); err != nil {
		if hadOutput {
			_ = os.Rename(backup, outputRoot)
		}
		return fmt.Errorf("install package: %w", err)
	}
	if hadOutput {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove replaced package backup %s: %w", backup, err)
		}
	}
	return nil
}

func uniqueAbsentPath(parent, prefix string) (string, error) {
	directory, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", err
	}
	if err := os.Remove(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func buildPackageTree(moduleRoot, outputRoot, pinnedGo, buildID string) (packageManifest, error) {
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return packageManifest{}, err
	}
	if err := verifyGoToolchain(moduleRoot, pinnedGo); err != nil {
		return packageManifest{}, err
	}

	manifest := packageManifest{
		Schema:        "../schemas/runtime-package-manifest.schema.json",
		SchemaVersion: packageSchemaVersion,
		Runtime: manifestRuntime{
			Name: runtimeName, Version: runtimeVersion, BuildID: buildID,
		},
		Build: manifestBuild{
			GoToolchain: pinnedGo,
			CGOEnabled:  false,
			Flags: []string{
				"-trimpath",
				"-buildvcs=false",
				"-ldflags=-s -w -buildid= -X " + runtimeBuildIDPath + "=" + buildID,
			},
			Environment: []string{
				"CGO_ENABLED=0",
				"GOAMD64=v1",
				"GOARM64=v8.0",
				"GOENV=off",
				"GOEXPERIMENT=",
				"GOFIPS140=off",
				"GOFLAGS=-mod=readonly",
				"GOWORK=off",
			},
			TargetOrder: "windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64",
		},
	}

	goBinaries := make([]string, 0, len(supportedTargets)+1)
	for _, target := range supportedTargets {
		relative := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH,
			targetBinaryName(target.GOOS),
		))
		destination := filepath.Join(outputRoot, filepath.FromSlash(relative))
		if err := materializeRuntimeBinary(
			moduleRoot,
			relative,
			destination,
			target,
			pinnedGo,
			buildID,
		); err != nil {
			return packageManifest{}, err
		}
		mode := "0755"
		if target.GOOS == "windows" {
			mode = "0644"
		}
		artifact, err := describeFile(outputRoot, relative, "runtime", target.GOOS, target.GOARCH, mode)
		if err != nil {
			return packageManifest{}, err
		}
		manifest.Targets = append(manifest.Targets, artifact)
		goBinaries = append(goBinaries, destination)
	}

	noticesSource := filepath.Join(moduleRoot, "THIRD_PARTY_NOTICES.md")

	posixRelative := "re-discipline-knowledge"
	posixPath := filepath.Join(outputRoot, posixRelative)
	if err := os.WriteFile(posixPath, []byte(posixLauncher), 0o755); err != nil {
		return packageManifest{}, err
	}
	if err := os.Chmod(posixPath, 0o755); err != nil {
		return packageManifest{}, err
	}
	posixArtifact, err := describeFile(outputRoot, posixRelative, "posix-dispatch", "", "", "0755")
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Launchers = append(manifest.Launchers, posixArtifact)

	windowsRelative := "re-discipline-knowledge.exe"
	windowsPath := filepath.Join(outputRoot, windowsRelative)
	if err := materializeWindowsLauncher(
		moduleRoot, windowsPath, pinnedGo, buildID,
	); err != nil {
		return packageManifest{}, err
	}
	windowsArtifact, err := describeFile(
		outputRoot, windowsRelative, "windows-architecture-dispatch", "windows", "amd64", "0644",
	)
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Launchers = append(manifest.Launchers, windowsArtifact)
	goBinaries = append(goBinaries, windowsPath)
	if err := verifyThirdPartyNotices(noticesSource, goBinaries, pinnedGo); err != nil {
		return packageManifest{}, err
	}

	sharedAssets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		return packageManifest{}, err
	}
	manifest.SharedAssets = sharedAssets

	noticesRelative := "THIRD_PARTY_NOTICES.md"
	noticesDestination := filepath.Join(outputRoot, noticesRelative)
	if err := copyRegularFile(noticesSource, noticesDestination, 0o644); err != nil {
		return packageManifest{}, err
	}
	noticesArtifact, err := describeFile(outputRoot, noticesRelative, "third-party-notices", "", "", "0644")
	if err != nil {
		return packageManifest{}, err
	}
	manifest.Notices = noticesArtifact

	manifestBody, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return packageManifest{}, err
	}
	manifestBody = append(manifestBody, '\n')
	if err := os.WriteFile(filepath.Join(outputRoot, "manifest.json"), manifestBody, 0o644); err != nil {
		return packageManifest{}, err
	}
	if err := writeSHA256Sums(moduleRoot, outputRoot, manifest); err != nil {
		return packageManifest{}, err
	}
	return manifest, nil
}

func verifyGoToolchain(moduleRoot, pinnedGo string) error {
	command := exec.Command("go", "env", "GOVERSION")
	command.Dir = moduleRoot
	command.Env = buildEnvironment("", "", pinnedGo)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("select %s: %w\n%s", pinnedGo, err, output)
	}
	actual := strings.TrimSpace(string(output))
	if actual != pinnedGo {
		return fmt.Errorf("selected Go toolchain %q, expected %q", actual, pinnedGo)
	}
	return nil
}

func buildGoBinary(
	moduleRoot string,
	destination string,
	target targetSpec,
	mainPackage string,
	pinnedGo string,
	buildID string,
) error {
	return buildGoBinaryWithIdentityPath(
		moduleRoot, destination, target, mainPackage, pinnedGo,
		runtimeBuildIDPath, buildID,
	)
}

func buildGoBinaryWithIdentityPath(
	moduleRoot string,
	destination string,
	target targetSpec,
	mainPackage string,
	pinnedGo string,
	buildIDPath string,
	buildID string,
) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	ldflags := "-s -w -buildid="
	if buildID != "" {
		if buildIDPath == "" {
			return errors.New("compiled build identity path is required")
		}
		ldflags += " -X " + buildIDPath + "=" + buildID
	}
	command := exec.Command(
		"go",
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags="+ldflags,
		"-o",
		destination,
		mainPackage,
	)
	command.Dir = moduleRoot
	command.Env = buildEnvironment(target.GOOS, target.GOARCH, pinnedGo)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s-%s %s: %w\n%s", target.GOOS, target.GOARCH, mainPackage, err, output)
	}
	if err := os.Chmod(destination, 0o755); err != nil {
		return err
	}
	if err := verifyBuiltBinary(destination, target, pinnedGo, buildID); err != nil {
		return err
	}
	return nil
}

func materializeRuntimeBinary(
	moduleRoot string,
	relative string,
	destination string,
	target targetSpec,
	pinnedGo string,
	buildID string,
) error {
	if runtime.GOOS != "windows" && target.GOOS == "windows" {
		return copyCanonicalWindowsBinary(
			moduleRoot, relative, destination, target, pinnedGo, buildID,
		)
	}
	if err := buildGoBinary(
		moduleRoot,
		destination,
		target,
		"./cmd/re-discipline-knowledge",
		pinnedGo,
		buildID,
	); err != nil {
		return err
	}
	if target.GOOS == "windows" {
		return os.Chmod(destination, windowsArtifactMode)
	}
	return nil
}

func materializeWindowsLauncher(
	moduleRoot, destination, pinnedGo, buildID string,
) error {
	if runtime.GOOS == "windows" {
		if err := buildGoBinaryWithIdentityPath(
			moduleRoot,
			destination,
			windowsLauncherTarget,
			"./cmd/re-discipline-knowledge-launcher",
			pinnedGo,
			windowsLauncherBuildIDPath,
			buildID,
		); err != nil {
			return err
		}
		return os.Chmod(destination, windowsArtifactMode)
	}
	return copyCanonicalWindowsLauncher(
		moduleRoot, destination, pinnedGo, buildID,
	)
}

func copyCanonicalWindowsLauncher(
	moduleRoot, destination, pinnedGo, expectedBuildID string,
) error {
	return copyCanonicalWindowsBinary(
		moduleRoot,
		"re-discipline-knowledge.exe",
		destination,
		windowsLauncherTarget,
		pinnedGo,
		expectedBuildID,
	)
}

func copyCanonicalWindowsBinary(
	moduleRoot string,
	relative string,
	destination string,
	target targetSpec,
	pinnedGo string,
	expectedBuildID string,
) error {
	source, err := packagePath(filepath.Join(moduleRoot, "bin"), relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := copyRegularFile(source, destination, windowsArtifactMode); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"canonical Windows artifact %s is missing; generate knowledge/bin on Windows first",
				relative,
			)
		}
		return fmt.Errorf("copy canonical Windows artifact %s: %w", relative, err)
	}
	if err := verifyBuiltBinary(destination, target, pinnedGo, expectedBuildID); err != nil {
		return fmt.Errorf("verify canonical Windows artifact %s: %w", relative, err)
	}
	return nil
}

func verifyBuiltBinary(path string, target targetSpec, pinnedGo, expectedBuildID string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Go build information from %s: %w", path, err)
	}
	if info.GoVersion != pinnedGo {
		return fmt.Errorf(
			"binary %s used Go toolchain %q, expected %q",
			path, info.GoVersion, pinnedGo,
		)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	expected := map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        target.GOOS,
		"GOARCH":      target.GOARCH,
	}
	switch target.GOARCH {
	case "amd64":
		expected["GOAMD64"] = "v1"
	case "arm64":
		expected["GOARM64"] = "v8.0"
	}
	for key, value := range expected {
		if settings[key] != value {
			return fmt.Errorf(
				"binary %s build setting %s=%q, expected %q",
				path, key, settings[key], value,
			)
		}
	}
	if expectedBuildID != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(body, []byte(expectedBuildID)) {
			return fmt.Errorf("binary %s omits compiled build identity %s", path, expectedBuildID)
		}
	}
	return nil
}

func verifyThirdPartyNotices(noticesPath string, binaryPaths []string, pinnedGo string) error {
	notices, err := os.ReadFile(noticesPath)
	if err != nil {
		return err
	}
	firstPartyLicense, err := os.ReadFile(filepath.Join(filepath.Dir(noticesPath), "..", "LICENSE"))
	if err != nil {
		return fmt.Errorf("read first-party license: %w", err)
	}
	if err := verifyFirstPartyLicenseCoverage(notices, firstPartyLicense); err != nil {
		return err
	}
	linked := map[string]string{}
	for _, binaryPath := range binaryPaths {
		info, err := buildinfo.ReadFile(binaryPath)
		if err != nil {
			return fmt.Errorf("read dependency information from %s: %w", binaryPath, err)
		}
		for _, dependency := range info.Deps {
			if dependency.Replace != nil {
				return fmt.Errorf(
					"release binary %s contains unsupported module replacement %s => %s",
					binaryPath, dependency.Path, dependency.Replace.Path,
				)
			}
			if previous, found := linked[dependency.Path]; found && previous != dependency.Version {
				return fmt.Errorf(
					"release binaries link conflicting versions of %s: %s and %s",
					dependency.Path, previous, dependency.Version,
				)
			}
			linked[dependency.Path] = dependency.Version
		}
	}
	paths := make([]string, 0, len(linked))
	for path := range linked {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	dependencies := make([][2]string, 0, len(paths))
	for _, path := range paths {
		dependencies = append(dependencies, [2]string{path, linked[path]})
	}
	if err := verifyNoticeCoverage(notices, pinnedGo, dependencies); err != nil {
		return err
	}
	return nil
}

func verifyFirstPartyLicenseCoverage(notices, firstPartyLicense []byte) error {
	normalize := func(body []byte) []byte {
		body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
		return bytes.TrimSpace(body)
	}
	license := normalize(firstPartyLicense)
	if len(license) == 0 {
		return errors.New("first-party LICENSE is empty")
	}
	if !bytes.Contains(normalize(notices), license) {
		return errors.New("release notices do not reproduce the first-party LICENSE")
	}
	return nil
}

func verifyNoticeCoverage(notices []byte, pinnedGo string, dependencies [][2]string) error {
	rows := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(notices))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) < 5 {
			continue
		}
		component := strings.Trim(strings.TrimSpace(columns[1]), "`")
		version := strings.Trim(strings.TrimSpace(columns[2]), "`")
		if component != "" && version != "" {
			if _, duplicate := rows[component]; duplicate {
				return fmt.Errorf("third-party notices repeat component %q", component)
			}
			rows[component] = version
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if rows["Go standard library"] != pinnedGo {
		return fmt.Errorf(
			"third-party notices declare Go standard library %q, expected %q",
			rows["Go standard library"], pinnedGo,
		)
	}
	linked := map[string]string{}
	for _, dependency := range dependencies {
		path, version := dependency[0], dependency[1]
		if path == "" || version == "" {
			return fmt.Errorf("linked dependency has incomplete identity %q@%q", path, version)
		}
		linked[path] = version
		if rows[path] != version {
			return fmt.Errorf(
				"third-party notices omit linked module %s@%s",
				path, version,
			)
		}
	}
	for component := range rows {
		if strings.Contains(component, "/") {
			if _, found := linked[component]; !found {
				return fmt.Errorf(
					"third-party notices list module %s that is not linked",
					component,
				)
			}
		}
	}
	return nil
}

func buildEnvironment(goos, goarch, pinnedGo string) []string {
	replacements := map[string]string{
		"CGO_ENABLED":  "0",
		"GOAMD64":      "v1",
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "-mod=readonly",
		"GOTOOLCHAIN":  pinnedGo,
		"GOWORK":       "off",
	}
	if goos != "" {
		replacements["GOOS"] = goos
	}
	if goarch != "" {
		replacements["GOARCH"] = goarch
	}
	environment := make([]string, 0, len(os.Environ())+len(replacements))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replaced := replacements[strings.ToUpper(key)]; replaced {
				continue
			}
		}
		environment = append(environment, entry)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+replacements[key])
	}
	return environment
}

func targetBinaryName(goos string) string {
	if goos == "windows" {
		return runtimeName + ".exe"
	}
	return runtimeName
}

func copyRegularFile(source, destination string, mode fs.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, mode)
}

func describeFile(outputRoot, relative, kind, goos, goarch, mode string) (manifestFile, error) {
	path, err := packagePath(outputRoot, relative)
	if err != nil {
		return manifestFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return manifestFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return manifestFile{}, fmt.Errorf("package file is not regular: %s", relative)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		return manifestFile{}, err
	}
	return manifestFile{
		Kind: kind, GOOS: goos, GOARCH: goarch, Path: filepath.ToSlash(relative),
		SHA256: "sha256:" + digest, Size: info.Size(), Mode: mode,
	}, nil
}

func discoverSharedAssets(moduleRoot string) ([]manifestFile, error) {
	assets := []manifestFile{}
	binaryAssets := map[string]string{}
	for _, relativeRoot := range sharedAssetRoots {
		assetRoot := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(assetRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("shared runtime assets cannot contain symlinks: %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("shared runtime asset is not a regular file: %s", path)
			}
			relative, err := filepath.Rel(moduleRoot, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			kind, err := sharedAssetKind(relative)
			if err != nil {
				return err
			}
			digest, err := fileSHA256(path)
			if err != nil {
				return err
			}
			assets = append(assets, manifestFile{
				Kind: kind, Path: relative, SHA256: "sha256:" + digest,
				Size: info.Size(), Mode: "0644",
			})
			if kind == "shared-model-artifact" {
				binaryAssets[relative] = digest
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	if err := verifySharedModelPins(moduleRoot, binaryAssets); err != nil {
		return nil, err
	}
	if err := verifyLaneShipmentGate(moduleRoot); err != nil {
		return nil, err
	}
	return assets, nil
}

func sharedAssetKind(relative string) (string, error) {
	switch {
	case relative == "evals/conformance/cases.json" ||
		relative == "evals/conformance/finding-cases.json":
		return "benchmark-cases", nil
	case relative == "evals/conformance/lane-ablation-decision.json":
		return "lane-ablation-decision", nil
	case relative == "evals/conformance/lane-ablation-report.json":
		return "lane-ablation-report", nil
	case relative == projectLaneMeasurementPath:
		return "project-lane-ablation-measurement", nil
	case strings.HasPrefix(relative, "evals/conformance/evidence/") &&
		strings.HasSuffix(strings.ToLower(relative), ".zip"):
		return "lane-ablation-evidence-archive", nil
	case strings.HasPrefix(relative, "evals/conformance/fixture/") &&
		(strings.HasSuffix(strings.ToLower(relative), ".md") ||
			strings.HasSuffix(strings.ToLower(relative), ".json") ||
			strings.HasSuffix(strings.ToLower(relative), ".jsonc")):
		return "benchmark-fixture", nil
	case relative == "models/manifest.json":
		return "model-manifest", nil
	case strings.HasPrefix(relative, "models/specs/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "model-specification", nil
	case strings.HasPrefix(relative, "models/artifacts/") &&
		strings.HasSuffix(strings.ToLower(relative), ".bin"):
		return "shared-model-artifact", nil
	case relative == "models/artifacts/README.md":
		return "model-artifact-documentation", nil
	case strings.HasPrefix(relative, "profiles/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "retrieval-profile", nil
	case strings.HasPrefix(relative, "schemas/") &&
		strings.HasSuffix(strings.ToLower(relative), ".json"):
		return "json-schema", nil
	default:
		return "", fmt.Errorf("unclassified shared runtime asset %q", relative)
	}
}

// verifyLaneShipmentGate makes the holdout decision executable release
// policy. A future profile cannot silently reintroduce a dense or rerank lane,
// or its associated model, unless the checked-in holdout receipt records a
// positive unique-first or net ranking contribution and explicitly retains
// that lane.
func verifyLaneShipmentGate(moduleRoot string) error {
	profileBody, err := os.ReadFile(filepath.Join(moduleRoot, "profiles", "balanced-v1.json"))
	if err != nil {
		return err
	}
	var profile packagedRetrievalProfile
	if err := json.Unmarshal(profileBody, &profile); err != nil {
		return fmt.Errorf("decode retrieval profile for lane shipment gate: %w", err)
	}
	if len(profile.EffectiveProfiles) == 0 {
		return errors.New("retrieval profile has no evidence-gated effective profile")
	}
	manifestBody, err := os.ReadFile(filepath.Join(moduleRoot, "models", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest packagedModelManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return fmt.Errorf("decode model manifest for lane shipment gate: %w", err)
	}
	reportPath := filepath.Join(
		moduleRoot, "evals", "conformance", "lane-ablation-report.json")
	var report laneAblationReport
	reportBody, err := decodeStrictJSONFile(reportPath, "lane ablation report", &report)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(
		moduleRoot, "evals", "conformance", "lane-ablation-decision.json")
	var receipt laneAblationDecision
	if _, err := decodeStrictJSONFile(receiptPath, "lane shipment evidence", &receipt); err != nil {
		return err
	}
	if err := validateLaneAblationIdentity(report, receipt, reportBody); err != nil {
		return err
	}

	findingCasesBody, err := os.ReadFile(filepath.Join(
		moduleRoot, "evals", "conformance", "finding-cases.json"))
	if err != nil {
		return fmt.Errorf("read lane shipment holdout corpus: %w", err)
	}
	var findingSuite struct {
		Cases []struct {
			ID    string `json:"id"`
			Split string `json:"split"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(findingCasesBody, &findingSuite); err != nil {
		return fmt.Errorf("decode lane shipment holdout corpus: %w", err)
	}
	holdoutIDs := map[string]bool{}
	for _, eval := range findingSuite.Cases {
		if eval.Split != "holdout" {
			continue
		}
		if strings.TrimSpace(eval.ID) == "" || holdoutIDs[eval.ID] {
			return errors.New("lane shipment holdout corpus has a missing or duplicate case id")
		}
		holdoutIDs[eval.ID] = true
	}
	packagedLayer := receipt.EvidenceLayers.PackagedConformance
	if len(holdoutIDs) < 10 || packagedLayer.HoldoutCases != len(holdoutIDs) {
		return fmt.Errorf(
			"lane shipment evidence holdout count %d does not match corpus count %d",
			packagedLayer.HoldoutCases, len(holdoutIDs))
	}
	if err := validateLaneAblationProfiles(report.Profiles); err != nil {
		return err
	}
	if err := validateLaneAblationModels(report.Models); err != nil {
		return err
	}
	computed, err := validateAndAggregateLaneCases(report.Cases, holdoutIDs)
	if err != nil {
		return err
	}
	for _, lane := range []string{"dense", "rerank"} {
		evidence, present := packagedLayer.Lanes[lane]
		counts := computed[lane]
		if !present || evidence.UniqueFirst != counts.UniqueFirst ||
			evidence.Improved != counts.Improved || evidence.Degraded != counts.Degraded {
			return fmt.Errorf(
				"lane shipment evidence for %s does not match recomputed report aggregates", lane)
		}
	}
	if err := validateProjectLaneEvidence(moduleRoot, report, receipt); err != nil {
		return err
	}

	requested := map[string]bool{}
	for _, row := range profile.EffectiveProfiles {
		for _, lane := range row.Lanes {
			if lane == "dense" || lane == "rerank" {
				requested[lane] = true
			}
		}
		if row.Benchmark.Suite != receipt.Suite || row.Benchmark.Status != "passed" {
			return fmt.Errorf(
				"lane shipment evidence uses a different or unpassed suite for profile %q", row.Name)
		}
		role := ""
		if containsString(row.Lanes, "rerank") {
			role = "rerank"
		} else if containsString(row.Lanes, "dense") {
			role = "dense"
		}
		if role != "" {
			evidenceProfile, ok := laneProfileByRole(report.Profiles, role)
			if !ok || row.Benchmark.EvalFingerprint != receipt.EvalFingerprint ||
				row.Benchmark.ModelFingerprint != evidenceProfile.ModelFingerprint ||
				row.Benchmark.RuntimeFingerprint != report.RuntimeFingerprint ||
				!validSHA256Identity(row.Benchmark.Digest) ||
				!validSHA256Identity(row.Benchmark.CorpusFingerprint) {
				return fmt.Errorf(
					"requested %s lane profile %q is not bound to the measured model/runtime and current packaged receipt", role, row.Name)
			}
		}
	}
	modelCounts := map[string]int{}
	for _, model := range manifest.Models {
		switch model.Role {
		case "embedding":
			modelCounts[model.Role]++
			if !requested["dense"] {
				return fmt.Errorf("embedding model %q ships without an evidence-gated dense lane", model.ID)
			}
			if evidenceModel, ok := laneModelByRole(report.Models, model.Role); !ok ||
				!packagedModelMatchesAblation(model, evidenceModel) {
				return fmt.Errorf("embedding model %q is not the model measured by the lane report", model.ID)
			}
		case "reranker":
			return fmt.Errorf("reranker model %q ships despite a two-layer removal decision", model.ID)
		default:
			return fmt.Errorf("model %q has unsupported shipment role %q", model.ID, model.Role)
		}
	}
	if requested["dense"] && modelCounts["embedding"] != 1 {
		return errors.New("dense lane must ship exactly one report-bound embedding model")
	}
	if requested["rerank"] {
		return errors.New("rerank lane cannot ship after zero measured benefit in both evidence layers")
	}
	if !requested["dense"] {
		return errors.New("production profile omits the project-evidence-retained dense lane")
	}
	denseChoice, densePresent := receipt.Lanes["dense"]
	rerankChoice, rerankPresent := receipt.Lanes["rerank"]
	if !densePresent || denseChoice.Decision != "retain" ||
		denseChoice.Basis != "project-corpus-rescues" {
		return errors.New("dense lane lacks a project-corpus rescue decision and cannot ship")
	}
	if !rerankPresent || rerankChoice.Decision != "remove" ||
		rerankChoice.Basis != "no-measured-benefit-across-both-layers" {
		return errors.New("rerank removal decision is incomplete")
	}
	return nil
}

func decodeStrictJSONFile(path, label string, target any) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if err := rejectDuplicateJSONKeys(body); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode %s: trailing JSON value", label)
	}
	return body, nil
}

func rejectDuplicateJSONKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var consumeValue func() error
	consumeValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := consumeValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim('}') {
				return errors.New("object is not closed")
			}
		case '[':
			for decoder.More() {
				if err := consumeValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim(']') {
				return errors.New("array is not closed")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
		return nil
	}
	if err := consumeValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func validSHA256Identity(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validBareSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateLaneAblationIdentity(
	report laneAblationReport,
	receipt laneAblationDecision,
	reportBody []byte,
) error {
	if report.SchemaVersion != 2 || report.Suite != "packaged-conformance-v1" ||
		!validSHA256Identity(report.EvalFingerprint) ||
		!validSHA256Identity(report.CorpusFingerprint) ||
		!validSHA256Identity(report.RuntimeFingerprint) ||
		strings.TrimSpace(report.ParserVersion) == "" || strings.TrimSpace(report.ChunkerVersion) == "" ||
		len(report.SourceRevision) != 40 || report.SourceRevision != strings.ToLower(report.SourceRevision) {
		return errors.New("lane ablation report has an incomplete immutable runtime identity")
	}
	if _, err := hex.DecodeString(report.SourceRevision); err != nil {
		return errors.New("lane ablation report has an invalid source revision")
	}
	if _, err := time.Parse(time.RFC3339, report.EvaluatedAt); err != nil {
		return errors.New("lane ablation report has an invalid evaluation timestamp")
	}
	reportHash := sha256.Sum256(reportBody)
	reportDigest := "sha256:" + hex.EncodeToString(reportHash[:])
	if receipt.SchemaVersion != 3 || receipt.Suite != report.Suite ||
		receipt.EvaluatedAt != report.EvaluatedAt ||
		receipt.EvalFingerprint != report.EvalFingerprint ||
		receipt.CorpusFingerprint != report.CorpusFingerprint ||
		receipt.RuntimeFingerprint != report.RuntimeFingerprint ||
		receipt.ReportDigest != reportDigest ||
		strings.TrimSpace(receipt.Conclusion) == "" || len(receipt.Lanes) != 2 {
		return errors.New("lane shipment evidence is not bound to the immutable ablation report")
	}
	for _, lane := range []string{"dense", "rerank"} {
		choice, present := receipt.Lanes[lane]
		if !present || (choice.Decision != "remove" && choice.Decision != "retain") ||
			strings.TrimSpace(choice.Basis) == "" {
			return fmt.Errorf("lane shipment evidence for %s is incomplete", lane)
		}
	}
	packaged := receipt.EvidenceLayers.PackagedConformance
	if packaged.Status != "underpowered-no-positive-contribution" ||
		packaged.HoldoutCases < 10 || len(packaged.Lanes) != 2 {
		return errors.New("packaged conformance evidence layer is incomplete")
	}
	project := receipt.EvidenceLayers.ProjectCorpus
	if project.Status != "final-corpus-verified" ||
		project.CasesPerArm != 64 || !validSHA256Identity(project.RawBenchmarkSHA256) ||
		project.ProjectMeasurementPath != projectLaneMeasurementPath ||
		!validSHA256Identity(project.ProjectMeasurementSHA256) ||
		len(project.Lanes) != 2 {
		return errors.New("project-corpus evidence layer is incomplete")
	}
	return nil
}

func validateProjectLaneEvidence(
	moduleRoot string,
	report laneAblationReport,
	receipt laneAblationDecision,
) error {
	measurementFile, err := sharedAssetPath(moduleRoot, projectLaneMeasurementPath)
	if err != nil {
		return err
	}
	var measurement projectLaneMeasurement
	measurementBody, err := decodeStrictJSONFile(
		measurementFile, "project lane-ablation measurement", &measurement)
	if err != nil {
		return err
	}
	measurementSum := sha256.Sum256(measurementBody)
	measurementIdentity := "sha256:" + hex.EncodeToString(measurementSum[:])
	if err := validateProjectLaneMeasurementIdentity(measurement, report); err != nil {
		return err
	}
	if err := validateProjectLaneArchive(moduleRoot, measurement.HistoricalRerank.Provenance); err != nil {
		return err
	}
	derived, err := deriveProjectLaneMeasurement(measurement)
	if err != nil {
		return err
	}

	project := report.ProjectEvidence
	if project.Suite != "project-benchmark-v1" || project.Project != measurement.Project ||
		project.Status != "final-corpus-verified" ||
		project.EvaluatedAt != measurement.EvaluatedAt ||
		project.FrozenSourceRevision != measurement.Provenance.FrozenSourceRevision ||
		project.RawBenchmarkSHA256 != measurement.Provenance.RawBenchmarkSHA256 ||
		project.RawBenchmarkRunID != measurement.Provenance.RawBenchmarkRunID ||
		project.Complete != measurement.Provenance.RawBenchmarkComplete ||
		project.ArmCount != 2 || project.CasesPerArm != 64 ||
		project.CorpusFingerprint != measurement.Corpus.CorpusFingerprint ||
		project.EvalFingerprint != measurement.Corpus.ProjectedEvalFingerprint ||
		project.ProjectionTransformDigest != measurement.Corpus.ProjectionTransformDigest ||
		project.ProjectMeasurementPath != projectLaneMeasurementPath ||
		project.ProjectMeasurementSHA256 != measurementIdentity ||
		project.IndexedSourceBytesUnchanged != measurement.Harness.IndexedSourceBytesUnchanged ||
		project.SharedHardGateFailures != measurement.Decision.SharedSafetyFailures ||
		project.DenseAddedHardGateRegressions != measurement.Decision.DenseAddedSafetyRegressions ||
		project.RerankAddedHardGateRegressions != measurement.HistoricalRerank.RerankAddedSafetyRegressions {
		return errors.New("project lane evidence does not match the checked-in per-case measurement")
	}
	if err := compareProjectLaneMetrics("dense", project.Dense, derived.Dense); err != nil {
		return err
	}
	if err := compareProjectLaneMetrics("rerank", project.Rerank, derived.Rerank); err != nil {
		return err
	}
	if err := compareProjectRescues(project.RescueCases, derived.Rescues); err != nil {
		return err
	}
	if err := compareProjectUncertainty(
		project.Uncertainty, measurement.Uncertainty, derived,
	); err != nil {
		return err
	}

	projectReceipt := receipt.EvidenceLayers.ProjectCorpus
	if projectReceipt.Status != project.Status ||
		projectReceipt.CasesPerArm != project.CasesPerArm ||
		projectReceipt.RawBenchmarkSHA256 != project.RawBenchmarkSHA256 ||
		projectReceipt.ProjectMeasurementPath != project.ProjectMeasurementPath ||
		projectReceipt.ProjectMeasurementSHA256 != project.ProjectMeasurementSHA256 ||
		len(projectReceipt.Lanes) != 2 {
		return errors.New("project decision layer is not bound to the checked-in measurement")
	}
	want := map[string]laneAblationProjectCounts{
		"dense": {
			Rescues: derived.Dense.Rescues, Losses: derived.Dense.Losses,
			RankImprovements:         derived.Dense.RankImprovements,
			RankDegradations:         derived.Dense.RankDegradations,
			AddedHardGateRegressions: measurement.Decision.DenseAddedSafetyRegressions,
		},
		"rerank": {
			Rescues: derived.Rerank.Rescues, Losses: derived.Rerank.Losses,
			RankImprovements:         derived.Rerank.RankImprovements,
			RankDegradations:         derived.Rerank.RankDegradations,
			AddedHardGateRegressions: measurement.HistoricalRerank.RerankAddedSafetyRegressions,
		},
	}
	for lane, expected := range want {
		if actual, ok := projectReceipt.Lanes[lane]; !ok || actual != expected {
			return fmt.Errorf(
				"project decision layer for %s does not match recomputed per-case evidence", lane)
		}
	}
	if choice := receipt.Lanes["dense"]; choice.Decision != measurement.Decision.Dense.Action {
		return errors.New("dense shipment decision differs from the current-runtime project measurement")
	}
	if choice := receipt.Lanes["rerank"]; choice.Decision != measurement.Decision.Rerank.Action {
		return errors.New("rerank shipment decision differs from the frozen historical project measurement")
	}
	return nil
}

func validateProjectLaneMeasurementIdentity(
	measurement projectLaneMeasurement,
	report laneAblationReport,
) error {
	current := measurement.Provenance
	historical := measurement.HistoricalRerank.Provenance
	if measurement.SchemaVersion != 2 ||
		measurement.Schema != "plugin://re-discipline/schemas/project-lane-ablation-report.schema.json" ||
		measurement.Kind != "project-retrieval-lane-ablation" || !measurement.MeasurementOnly ||
		strings.TrimSpace(measurement.Project) == "" ||
		current.RawBenchmarkArmCount != 2 || current.RawBenchmarkCasesPerArm != 64 ||
		!current.RawBenchmarkComplete || !validSHA256Identity(current.RawBenchmarkSHA256) ||
		!validSHA256Identity(current.FrozenRuntimeFingerprint) ||
		!validSHA256Identity(current.ProjectDirtyFingerprint) ||
		!validSHA256Identity(current.ProfileCatalogSHA256) ||
		!validSHA256Identity(current.ModelManifestSHA256) ||
		!validFullRevision(current.FrozenSourceRevision) ||
		!validFullRevision(current.ProjectGitRevision) ||
		measurement.Corpus.FinalEvalCaseCount != 64 ||
		!validSHA256Identity(measurement.Corpus.CorpusFingerprint) ||
		!validSHA256Identity(measurement.Corpus.ProjectedEvalFingerprint) ||
		!validSHA256Identity(measurement.Corpus.ProjectionTransformDigest) ||
		measurement.Harness.SourceRepositoryMutated ||
		!measurement.Harness.IndexedSourceBytesUnchanged ||
		!validSHA256Identity(measurement.Harness.ReceiptSHA256) ||
		!validSHA256Identity(measurement.Harness.BenchmarkCommandSHA256) ||
		!validSHA256Identity(measurement.Harness.ProjectionManifestArtifactSHA256) ||
		!validSHA256Identity(measurement.Harness.IndexedSourcesManifestSHA256) ||
		!validFullRevision(measurement.Harness.PluginRevision) ||
		!validFullRevision(measurement.Harness.ProjectRevision) ||
		measurement.Harness.PluginRevision != current.FrozenSourceRevision ||
		measurement.Harness.ProjectRevision != current.ProjectGitRevision ||
		measurement.Harness.ProjectionManifestArtifactSHA256 != measurement.Corpus.ProjectionManifestSHA256 {
		return errors.New("project lane measurement has an incomplete current-runtime identity")
	}
	if _, err := time.Parse(time.RFC3339, measurement.EvaluatedAt); err != nil {
		return errors.New("project lane measurement has an invalid evaluation timestamp")
	}
	if historical.RawBenchmarkArmCount != 3 || historical.RawBenchmarkCasesPerArm != 64 ||
		!historical.RawBenchmarkComplete || historical.EvidenceArchiveFormat != "zip-deflate-fixed-v1" ||
		!validSHA256Identity(historical.RawBenchmarkSHA256) ||
		!validSHA256Identity(historical.EvidenceArchiveSHA256) ||
		!validSHA256Identity(historical.RuntimeFingerprint) ||
		!validSHA256Identity(historical.ProjectDirtyFingerprint) ||
		!validSHA256Identity(historical.CorpusFingerprint) ||
		!validSHA256Identity(historical.EvalFingerprint) ||
		!validSHA256Identity(historical.ProfileCatalogSHA256) ||
		!validSHA256Identity(historical.ModelManifestSHA256) ||
		!validSHA256Identity(historical.ProjectionManifestSHA256) ||
		!validSHA256Identity(historical.ProjectionTransformDigest) ||
		!validFullRevision(historical.RuntimeSourceRevision) ||
		!validFullRevision(historical.ProjectGitRevision) ||
		historical.RuntimeSourceRevision != report.SourceRevision ||
		historical.RawBenchmarkByteCount < 1 || historical.EvidenceArchiveByteCount < 1 {
		return errors.New("project lane measurement has an incomplete historical rerank identity")
	}
	if err := validateProjectMeasurementModels(measurement); err != nil {
		return err
	}
	if err := validateProjectMeasurementProfiles(
		measurement.Profiles, false,
	); err != nil {
		return err
	}
	if err := validateProjectMeasurementProfiles(
		measurement.HistoricalRerank.Profiles, true,
	); err != nil {
		return err
	}
	if !measurement.Decision.ReleaseGatePassed ||
		!measurement.Decision.ProductionProfileConsistent ||
		measurement.Decision.DenseAddedSafetyRegressions != 0 ||
		measurement.HistoricalRerank.RerankAddedSafetyRegressions != 0 ||
		!sameProjectDecision(
			measurement.Decision.Rerank,
			measurement.HistoricalRerank.Decision,
		) {
		return errors.New("project lane measurement does not carry a closed two-layer release decision")
	}
	return nil
}

func validateProjectMeasurementModels(measurement projectLaneMeasurement) error {
	current := measurement.Models
	historical := measurement.HistoricalRerank.Models
	if len(current) != 1 || current[0].Role != "embedding" ||
		len(historical) != 2 || historical[0].Role != "embedding" ||
		historical[1].Role != "reranker" {
		return errors.New("project lane measurement mixes current and historical model inventories")
	}
	validate := func(model projectLaneModel, requireArtifact bool) bool {
		return strings.TrimSpace(model.ID) != "" &&
			strings.TrimSpace(model.Revision) != "" &&
			strings.TrimSpace(model.Implementation) != "" &&
			validBareSHA256(model.SpecSHA256) &&
			(!requireArtifact || validBareSHA256(model.ArtifactSHA256)) &&
			(model.ArtifactSHA256 == "" || validBareSHA256(model.ArtifactSHA256))
	}
	if !validate(current[0], true) || !validate(historical[0], true) ||
		!validate(historical[1], false) || current[0] != historical[0] {
		return errors.New("project lane measurement model identities are incomplete or cross-runtime inconsistent")
	}
	currentProfiles := measurement.Profiles
	historicalProfiles := measurement.HistoricalRerank.Profiles
	if len(currentProfiles) != 2 || len(historicalProfiles) != 3 ||
		len(currentProfiles[0].ModelIDs) != 0 ||
		!equalStrings(currentProfiles[1].ModelIDs, []string{current[0].ID}) ||
		len(historicalProfiles[0].ModelIDs) != 0 ||
		!equalStrings(historicalProfiles[1].ModelIDs, []string{historical[0].ID}) ||
		!equalStrings(
			historicalProfiles[2].ModelIDs,
			[]string{historical[0].ID, historical[1].ID},
		) {
		return errors.New("project lane measurement profiles do not bind the controlled model inventory")
	}
	return nil
}

func validateProjectMeasurementProfiles(
	profiles []projectLaneProfile,
	includeRerank bool,
) error {
	expected := []struct {
		role  string
		name  string
		lanes []string
	}{
		{role: "baseline", name: "lexical-graph-v1", lanes: []string{"exact", "fts", "graph"}},
		{role: "dense", name: "hybrid-no-rerank-v1", lanes: []string{"exact", "fts", "graph", "dense"}},
	}
	if includeRerank {
		expected = append(expected, struct {
			role  string
			name  string
			lanes []string
		}{role: "rerank", name: "hybrid-local-v1", lanes: []string{"exact", "fts", "graph", "dense", "rerank"}})
	}
	if len(profiles) != len(expected) {
		return errors.New("project lane measurement has the wrong controlled profile count")
	}
	for index, want := range expected {
		profile := profiles[index]
		if profile.Role != want.role || profile.Name != want.name ||
			!equalStrings(profile.ActiveLanes, want.lanes) ||
			!validSHA256Identity(profile.ObservationDigest) ||
			strings.TrimSpace(profile.EffectiveIdentity) == "" {
			return fmt.Errorf("project lane measurement profile %q is not controlled", profile.Role)
		}
	}
	return nil
}

func validateProjectLaneArchive(
	moduleRoot string,
	provenance projectLaneHistoricalProvenance,
) error {
	if !strings.HasPrefix(provenance.EvidenceArchivePath, "evals/conformance/evidence/") ||
		!strings.HasSuffix(strings.ToLower(provenance.EvidenceArchivePath), ".zip") {
		return errors.New("historical rerank evidence archive path is not canonical")
	}
	archivePath, err := sharedAssetPath(moduleRoot, provenance.EvidenceArchivePath)
	if err != nil {
		return err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("stat historical rerank evidence archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != provenance.EvidenceArchiveByteCount {
		return errors.New("historical rerank evidence archive size or file type differs from provenance")
	}
	digest, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	if "sha256:"+digest != provenance.EvidenceArchiveSHA256 {
		return errors.New("historical rerank evidence archive digest differs from provenance")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open historical rerank evidence archive: %w", err)
	}
	defer archive.Close()
	if archive.Comment != "" {
		return errors.New("historical rerank evidence archive comment must be empty")
	}
	required := map[string]string{
		"raw-benchmark.json":       provenance.RawBenchmarkSHA256,
		"projection-manifest.json": provenance.ProjectionManifestSHA256,
		"profile-catalog.json":     provenance.ProfileCatalogSHA256,
		"model-manifest.json":      provenance.ModelManifestSHA256,
	}
	if len(archive.File) != 13 {
		return fmt.Errorf("historical rerank evidence archive has %d entries, want 13", len(archive.File))
	}
	previous := ""
	evalCount := 0
	seen := map[string]bool{}
	for _, file := range archive.File {
		name := file.Name
		parts := strings.Split(name, "/")
		canonicalParts := len(parts) > 0
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				canonicalParts = false
				break
			}
		}
		if !canonicalParts || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") ||
			strings.Contains("/"+name+"/", "/../") || seen[name] ||
			(previous != "" && name <= previous) || file.Method != zip.Deflate ||
			file.Flags != 0 || file.CreatorVersion != (3<<8)|20 ||
			file.ReaderVersion != 20 || file.ExternalAttrs != uint32(0o100644<<16) ||
			len(file.Extra) != 0 || file.Comment != "" ||
			!file.Mode().IsRegular() || file.Mode().Perm() != 0o644 ||
			file.Modified.Year() != 1980 || file.Modified.Month() != time.January ||
			file.Modified.Day() != 1 || file.Modified.Hour() != 0 ||
			file.Modified.Minute() != 0 || file.Modified.Second() != 0 {
			return fmt.Errorf("historical rerank evidence archive entry %q is non-canonical", name)
		}
		seen[name] = true
		previous = name
		if strings.HasPrefix(name, "evals/") {
			evalCount++
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, 32<<20))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(body)) != int64(file.UncompressedSize64) {
			return fmt.Errorf("historical rerank evidence archive member %q is truncated", name)
		}
		wantDigest, selected := required[name]
		if !selected {
			continue
		}
		if sha256Identity(body) != wantDigest {
			return fmt.Errorf("historical rerank evidence archive member %q differs from provenance", name)
		}
		if name == "raw-benchmark.json" && int64(len(body)) != provenance.RawBenchmarkByteCount {
			return errors.New("historical raw benchmark byte count differs from provenance")
		}
		delete(required, name)
	}
	if evalCount != 9 || len(required) != 0 {
		return errors.New("historical rerank evidence archive inventory is incomplete")
	}
	return nil
}

func deriveProjectLaneMeasurement(
	measurement projectLaneMeasurement,
) (projectLaneDerived, error) {
	var derived projectLaneDerived
	if len(measurement.Cases) != 64 {
		return derived, errors.New("project lane measurement must contain 64 current cases")
	}
	previous := ""
	denseIDs := []string{}
	for _, eval := range measurement.Cases {
		if strings.TrimSpace(eval.CaseID) == "" ||
			(previous != "" && eval.CaseID <= previous) {
			return derived, fmt.Errorf("project lane measurement has an unsorted or duplicate current case %q", eval.CaseID)
		}
		previous = eval.CaseID
		if err := validateProjectOutcome(eval, "baseline", eval.Baseline); err != nil {
			return derived, err
		}
		if err := validateProjectOutcome(eval, "dense", eval.Dense); err != nil {
			return derived, err
		}
		comparison := compareProjectOutcomes(eval.Baseline, eval.Dense)
		if comparison != eval.DenseComparison {
			return derived, fmt.Errorf("project lane measurement current case %s has a stale dense comparison", eval.CaseID)
		}
		updateProjectLaneMetric(&derived.Dense, comparison)
		if comparison.UniqueRescue {
			denseIDs = append(denseIDs, eval.CaseID)
			path, rank, ok := bestProjectRelevantPath(eval.Dense)
			if !ok {
				return derived, fmt.Errorf("project lane measurement rescue %s has no relevant evidence", eval.CaseID)
			}
			derived.Rescues = append(derived.Rescues, laneAblationProjectRescue{
				CaseID: eval.CaseID, Split: eval.Split, Topic: eval.Topic,
				TargetPath: path, BaselineRelevantRank: projectOutcomeRank(eval.Baseline),
				DenseRelevantRank: rank,
				TargetDisjoint:    eval.VocabularyPolicy == "target-disjoint-v1",
				SourceClass:       projectSourceClass(path), AddedHardGateRegressions: 0,
			})
		}
	}

	historical := measurement.HistoricalRerank
	if len(historical.Cases) != 64 {
		return derived, errors.New("project lane measurement must contain 64 historical rerank cases")
	}
	previous = ""
	rerankIDs := []string{}
	for _, eval := range historical.Cases {
		if strings.TrimSpace(eval.CaseID) == "" ||
			(previous != "" && eval.CaseID <= previous) {
			return derived, fmt.Errorf("project lane measurement has an unsorted or duplicate historical case %q", eval.CaseID)
		}
		previous = eval.CaseID
		metadata := projectLaneCase{
			CaseID: eval.CaseID, Split: eval.Split, Topic: eval.Topic,
			Answerable: eval.Answerable,
		}
		if err := validateProjectOutcome(metadata, "dense", eval.Dense); err != nil {
			return derived, err
		}
		if err := validateProjectOutcome(metadata, "rerank", eval.Rerank); err != nil {
			return derived, err
		}
		comparison := compareProjectOutcomes(eval.Dense, eval.Rerank)
		if comparison != eval.RerankComparison {
			return derived, fmt.Errorf("project lane measurement historical case %s has a stale rerank comparison", eval.CaseID)
		}
		updateProjectLaneMetric(&derived.Rerank, comparison)
		if comparison.PathsChanged {
			derived.Rerank.PathsChanged++
		}
		if comparison.UniqueRescue || comparison.RankImproved {
			rerankIDs = append(rerankIDs, eval.CaseID)
		}
	}

	if err := validateProjectDecision(
		measurement.Decision.Dense, denseIDs, derived.Dense.Losses,
		measurement.Decision.DenseAddedSafetyRegressions, "dense",
	); err != nil {
		return derived, err
	}
	if err := validateProjectDecision(
		historical.Decision, rerankIDs, derived.Rerank.Losses,
		historical.RerankAddedSafetyRegressions, "rerank",
	); err != nil {
		return derived, err
	}

	currentProfiles := projectProfilesByRole(measurement.Profiles)
	if err := populateProjectMetricDeltas(&derived.Dense, currentProfiles, "baseline", "dense"); err != nil {
		return derived, err
	}
	for _, row := range measurement.Sensitivity.LeaveOneTopicOut {
		if row.DenseHitRateDelta <= 0 {
			derived.ZeroOmittedTopics = append(derived.ZeroOmittedTopics, row.OmittedTopic)
		}
	}
	derived.ClusterFragile = len(derived.ZeroOmittedTopics) > 0
	return derived, nil
}

func validateProjectOutcome(
	metadata projectLaneCase,
	label string,
	outcome projectLaneOutcome,
) error {
	if outcome.CaseID != metadata.CaseID || outcome.Split != metadata.Split ||
		outcome.Topic != metadata.Topic ||
		len(outcome.Paths) != len(outcome.Tiers) ||
		len(outcome.Paths) != len(outcome.ChunkIDs) ||
		len(outcome.Paths) != len(outcome.ContentHashes) ||
		len(outcome.RelevantPaths) != len(outcome.RelevantRanks) ||
		outcome.ReturnedUniquePaths != uniqueStringCount(outcome.Paths) ||
		outcome.EstimatedTokens < 0 || outcome.ReturnedTokens < 0 ||
		outcome.RelevantTokens < 0 || outcome.DuplicateTokens < 0 ||
		outcome.StaleResults < 0 || outcome.LatencyMillis < 0 ||
		outcome.MinimumTokenBudget < 128 {
		return fmt.Errorf("project lane measurement case %s has an invalid %s outcome", metadata.CaseID, label)
	}
	pathSet := map[string]bool{}
	for _, path := range outcome.Paths {
		pathSet[path] = true
	}
	for index, path := range outcome.RelevantPaths {
		if !pathSet[path] || outcome.RelevantRanks[index] < 1 ||
			outcome.RelevantRanks[index] > len(outcome.Paths) {
			return fmt.Errorf("project lane measurement case %s has invalid %s relevant evidence", metadata.CaseID, label)
		}
	}
	expectedFound := len(outcome.RelevantRanks) > 0
	if !metadata.Answerable {
		expectedFound = outcome.AbstentionCorrect
	}
	safety := outcome.AuthoritySafe && outcome.CitationMetadataSafe &&
		outcome.CorpusMatched && outcome.BudgetSafe && outcome.ReplayIdentical &&
		outcome.StaleResults == 0
	quality := !outcome.QualityGateApplicable ||
		(outcome.ExpectedFound && outcome.CompleteEvidence && outcome.CitationSafe &&
			outcome.AbstentionCorrect && len(outcome.HardNegativeHits) == 0)
	if outcome.ExpectedFound != expectedFound || outcome.SafetyPassed != safety ||
		outcome.QualityPassed != quality || outcome.GatePassed != (safety && quality) {
		return fmt.Errorf("project lane measurement case %s has inconsistent %s gates", metadata.CaseID, label)
	}
	return nil
}

func compareProjectOutcomes(before, after projectLaneOutcome) projectLaneComparison {
	beforeRank := projectOutcomeRank(before)
	afterRank := projectOutcomeRank(after)
	effect := "unchanged"
	if !before.ExpectedFound && after.ExpectedFound {
		effect = "rescue"
	} else if before.ExpectedFound && !after.ExpectedFound {
		effect = "loss"
	}
	both := before.ExpectedFound && after.ExpectedFound
	return projectLaneComparison{
		HitEffect: effect,
		RankDelta: func() int {
			if both {
				return beforeRank - afterRank
			}
			return 0
		}(),
		PathsChanged: !equalStrings(before.Paths, after.Paths),
		UniqueRescue: effect == "rescue",
		RankImproved: both && afterRank < beforeRank,
		RankDegraded: both && afterRank > beforeRank,
	}
}

func updateProjectLaneMetric(
	metric *laneAblationProjectLaneMetric,
	comparison projectLaneComparison,
) {
	if comparison.UniqueRescue {
		metric.Rescues++
	}
	if comparison.HitEffect == "loss" {
		metric.Losses++
	}
	if comparison.RankImproved {
		metric.RankImprovements++
	}
	if comparison.RankDegraded {
		metric.RankDegradations++
	}
}

func validateProjectDecision(
	decision projectLaneDecision,
	caseIDs []string,
	losses int,
	regressions int,
	lane string,
) error {
	expectedAction := "remove"
	if len(caseIDs) > 0 && (losses > 0 || regressions > 0) {
		expectedAction = "inconclusive"
	} else if len(caseIDs) > 0 {
		expectedAction = "retain"
	}
	if !equalStrings(decision.CaseIDs, caseIDs) ||
		decision.EventCount != len(caseIDs) ||
		decision.PositiveEvidence != (len(caseIDs) > 0) ||
		decision.Action != expectedAction || strings.TrimSpace(decision.Rationale) == "" {
		return fmt.Errorf("project lane measurement has a stale %s decision", lane)
	}
	return nil
}

func populateProjectMetricDeltas(
	metric *laneAblationProjectLaneMetric,
	profiles map[string]projectLaneProfile,
	baselineRole string,
	candidateRole string,
) error {
	baseline, baselineOK := profiles[baselineRole]
	candidate, candidateOK := profiles[candidateRole]
	if !baselineOK || !candidateOK {
		return errors.New("project lane measurement is missing metric profiles")
	}
	lookup := func(values map[string]float64, field string) (float64, bool) {
		value, ok := values[field]
		return value, ok && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	baselineRecall, ok1 := lookup(baseline.Metrics, "recallAtK")
	candidateRecall, ok2 := lookup(candidate.Metrics, "recallAtK")
	baselineComplete, ok3 := lookup(baseline.Metrics, "completeEvidenceCoverage")
	candidateComplete, ok4 := lookup(candidate.Metrics, "completeEvidenceCoverage")
	baselineHoldoutRecall, ok5 := lookup(baseline.MetricsBySplit["holdout"], "recallAtK")
	candidateHoldoutRecall, ok6 := lookup(candidate.MetricsBySplit["holdout"], "recallAtK")
	baselineHoldoutComplete, ok7 := lookup(baseline.MetricsBySplit["holdout"], "completeEvidenceCoverage")
	candidateHoldoutComplete, ok8 := lookup(candidate.MetricsBySplit["holdout"], "completeEvidenceCoverage")
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 && ok8) {
		return errors.New("project lane measurement profiles omit required quality metrics")
	}
	metric.OverallRecallDelta = candidateRecall - baselineRecall
	metric.HoldoutRecallDelta = candidateHoldoutRecall - baselineHoldoutRecall
	metric.OverallCompleteEvidenceDelta = candidateComplete - baselineComplete
	metric.HoldoutCompleteEvidenceDelta = candidateHoldoutComplete - baselineHoldoutComplete
	return nil
}

func compareProjectLaneMetrics(
	lane string,
	actual laneAblationProjectLaneMetric,
	expected laneAblationProjectLaneMetric,
) error {
	if actual.Rescues != expected.Rescues || actual.Losses != expected.Losses ||
		actual.RankImprovements != expected.RankImprovements ||
		actual.RankDegradations != expected.RankDegradations ||
		actual.PathsChanged != expected.PathsChanged ||
		!closeFloat(actual.OverallRecallDelta, expected.OverallRecallDelta) ||
		!closeFloat(actual.HoldoutRecallDelta, expected.HoldoutRecallDelta) ||
		!closeFloat(actual.OverallCompleteEvidenceDelta, expected.OverallCompleteEvidenceDelta) ||
		!closeFloat(actual.HoldoutCompleteEvidenceDelta, expected.HoldoutCompleteEvidenceDelta) {
		return fmt.Errorf("project %s aggregate does not match recomputed per-case measurement", lane)
	}
	return nil
}

func compareProjectRescues(
	actual []laneAblationProjectRescue,
	expected []laneAblationProjectRescue,
) error {
	if len(actual) != len(expected) {
		return errors.New("project dense rescue inventory does not match per-case measurement")
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("project dense rescue %q does not match per-case measurement", actual[index].CaseID)
		}
	}
	return nil
}

func compareProjectUncertainty(
	actual laneAblationProjectUncertainty,
	expected projectLaneUncertainty,
	derived projectLaneDerived,
) error {
	if len(actual.WilsonIntervals) != len(expected.WilsonIntervals) {
		return errors.New("project uncertainty does not mirror the current measurement")
	}
	for index, want := range expected.WilsonIntervals {
		got := actual.WilsonIntervals[index]
		if got.Slice != want.Slice || got.Successes != want.Successes ||
			got.Trials != want.Trials || !closeFloat(got.Estimate, want.Estimate) ||
			!closeFloat(got.Lower, want.Lower) || !closeFloat(got.Upper, want.Upper) {
			return errors.New("project Wilson intervals do not mirror the current measurement")
		}
	}
	bootstrap := expected.TopicClusterBootstrap
	if actual.TopicBootstrap.Replicates != bootstrap.Replicates ||
		!closeFloat(actual.TopicBootstrap.PointEstimate, bootstrap.PointEstimate) ||
		!closeFloat(actual.TopicBootstrap.Lower, bootstrap.Lower) ||
		!closeFloat(actual.TopicBootstrap.Upper, bootstrap.Upper) ||
		!closeFloat(actual.TopicBootstrap.ZeroOrLowerFraction, bootstrap.ZeroOrLowerFraction) ||
		!equalStrings(actual.LeaveOneTopicOutZeroWhenOmitting, derived.ZeroOmittedTopics) ||
		actual.ClusterFragile != derived.ClusterFragile || actual.FinalCorpusRerunRequired {
		return errors.New("project cluster uncertainty does not mirror the current measurement")
	}
	return nil
}

func projectProfilesByRole(profiles []projectLaneProfile) map[string]projectLaneProfile {
	result := map[string]projectLaneProfile{}
	for _, profile := range profiles {
		result[profile.Role] = profile
	}
	return result
}

func projectOutcomeRank(outcome projectLaneOutcome) int {
	best := 0
	for _, rank := range outcome.RelevantRanks {
		if best == 0 || rank < best {
			best = rank
		}
	}
	return best
}

func bestProjectRelevantPath(outcome projectLaneOutcome) (string, int, bool) {
	bestIndex := -1
	for index, rank := range outcome.RelevantRanks {
		if bestIndex < 0 || rank < outcome.RelevantRanks[bestIndex] {
			bestIndex = index
		}
	}
	if bestIndex < 0 || bestIndex >= len(outcome.RelevantPaths) {
		return "", 0, false
	}
	return outcome.RelevantPaths[bestIndex], outcome.RelevantRanks[bestIndex], true
}

func projectSourceClass(path string) string {
	for _, candidate := range []struct {
		prefix string
		class  string
	}{
		{prefix: "docs/history/", class: "history"},
		{prefix: "docs/truth/", class: "truth"},
		{prefix: "docs/backlog/", class: "backlog"},
		{prefix: "active/", class: "active"},
	} {
		if strings.HasPrefix(path, candidate.prefix) {
			return candidate.class
		}
	}
	return "project"
}

func uniqueStringCount(values []string) int {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return len(seen)
}

func sameProjectDecision(left, right projectLaneDecision) bool {
	return left.Action == right.Action &&
		left.PositiveEvidence == right.PositiveEvidence &&
		left.EventCount == right.EventCount &&
		equalStrings(left.CaseIDs, right.CaseIDs) &&
		left.Rationale == right.Rationale
}

func validFullRevision(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sha256Identity(body []byte) string {
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func closeFloat(left, right float64) bool {
	return !math.IsNaN(left) && !math.IsNaN(right) &&
		!math.IsInf(left, 0) && !math.IsInf(right, 0) &&
		math.Abs(left-right) <= 1e-12
}

func validateLaneAblationProfiles(profiles []laneAblationProfile) error {
	if len(profiles) != 3 {
		return errors.New("lane ablation report must contain exactly three controlled profiles")
	}
	expected := map[string][]string{
		"baseline": {"exact", "fts", "graph"},
		"dense":    {"exact", "fts", "graph", "dense"},
		"rerank":   {"exact", "fts", "graph", "dense", "rerank"},
	}
	seen := map[string]bool{}
	fingerprints := map[string]bool{}
	for _, profile := range profiles {
		lanes, ok := expected[profile.Role]
		if !ok || seen[profile.Role] || strings.TrimSpace(profile.Name) == "" ||
			!equalStringSet(profile.Lanes, lanes) ||
			!validSHA256Identity(profile.BenchmarkDigest) ||
			!validSHA256Identity(profile.ModelFingerprint) {
			return fmt.Errorf("lane ablation profile %q is incomplete", profile.Role)
		}
		seen[profile.Role] = true
		fingerprints[profile.ModelFingerprint] = true
	}
	if len(fingerprints) != 3 {
		return errors.New("lane ablation profiles do not identify three distinct model configurations")
	}
	return nil
}

func validateLaneAblationModels(models []laneAblationModel) error {
	if len(models) != 2 {
		return errors.New("lane ablation report must identify the measured embedding and reranker models")
	}
	seen := map[string]bool{}
	for _, model := range models {
		if (model.Role != "embedding" && model.Role != "reranker") || seen[model.Role] ||
			strings.TrimSpace(model.ID) == "" || strings.TrimSpace(model.Revision) == "" ||
			strings.TrimSpace(model.Implementation) == "" || !validBareSHA256(model.SpecSHA256) {
			return fmt.Errorf("lane ablation model %q is incomplete", model.ID)
		}
		if model.Role == "embedding" && !validBareSHA256(model.ArtifactSHA256) {
			return fmt.Errorf("lane ablation embedding model %q lacks its artifact identity", model.ID)
		}
		if model.Role == "reranker" && model.ArtifactSHA256 != "" &&
			!validBareSHA256(model.ArtifactSHA256) {
			return fmt.Errorf("lane ablation reranker model %q has an invalid artifact identity", model.ID)
		}
		seen[model.Role] = true
	}
	return nil
}

func validateAndAggregateLaneCases(
	cases []laneAblationCaseOutcome,
	holdoutIDs map[string]bool,
) (map[string]laneAblationCounts, error) {
	if len(cases) != len(holdoutIDs) {
		return nil, fmt.Errorf(
			"lane ablation report case count %d does not match holdout count %d",
			len(cases), len(holdoutIDs))
	}
	seen := map[string]bool{}
	previous := ""
	computed := map[string]laneAblationCounts{"dense": {}, "rerank": {}}
	for _, eval := range cases {
		if !holdoutIDs[eval.CaseID] || seen[eval.CaseID] ||
			(previous != "" && eval.CaseID <= previous) {
			return nil, fmt.Errorf("lane ablation report has an unknown, duplicate, or unsorted case %q", eval.CaseID)
		}
		seen[eval.CaseID] = true
		previous = eval.CaseID
		for label, outcome := range map[string]laneAblationQueryOutcome{
			"baseline": eval.Baseline, "dense": eval.Dense, "rerank": eval.Rerank,
		} {
			if err := validateLaneAblationOutcome(eval.CaseID, label, outcome); err != nil {
				return nil, err
			}
		}
		if eval.Baseline.UniqueFirst || eval.Rerank.UniqueFirst {
			return nil, fmt.Errorf("lane ablation case %s assigns unique-first to a non-dense arm", eval.CaseID)
		}
		dense := computed["dense"]
		if eval.Dense.UniqueFirst {
			dense.UniqueFirst++
		}
		updateLaneMovement(&dense, eval.Baseline, eval.Dense)
		computed["dense"] = dense
		rerank := computed["rerank"]
		updateLaneMovement(&rerank, eval.Dense, eval.Rerank)
		computed["rerank"] = rerank
	}
	return computed, nil
}

func validateLaneAblationOutcome(
	caseID, label string,
	outcome laneAblationQueryOutcome,
) error {
	if outcome.RelevantRank < 0 || outcome.RelevantHit != (outcome.RelevantRank > 0) ||
		(outcome.UniqueFirst && (!outcome.RelevantHit || outcome.RelevantRank != 1)) ||
		(outcome.RelevantRank > len(outcome.FindingIDs)) || !strictlySortedUnique(outcome.FindingIDs) {
		return fmt.Errorf("lane ablation case %s has an invalid %s outcome", caseID, label)
	}
	for _, id := range outcome.FindingIDs {
		if len(id) < 3 || !strings.HasPrefix(id, "F-") {
			return fmt.Errorf("lane ablation case %s has an invalid %s finding id", caseID, label)
		}
	}
	return nil
}

func updateLaneMovement(
	counts *laneAblationCounts,
	baseline, candidate laneAblationQueryOutcome,
) {
	if candidate.RelevantHit && (!baseline.RelevantHit || candidate.RelevantRank < baseline.RelevantRank) {
		counts.Improved++
	}
	if baseline.RelevantHit && (!candidate.RelevantHit || candidate.RelevantRank > baseline.RelevantRank) {
		counts.Degraded++
	}
}

func strictlySortedUnique(values []string) bool {
	for index, value := range values {
		if strings.TrimSpace(value) == "" || (index > 0 && value <= values[index-1]) {
			return false
		}
	}
	return true
}

func equalStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	want := map[string]bool{}
	for _, value := range right {
		want[value] = true
	}
	for _, value := range left {
		if !want[value] {
			return false
		}
		delete(want, value)
	}
	return len(want) == 0
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func laneProfileByRole(
	profiles []laneAblationProfile,
	role string,
) (laneAblationProfile, bool) {
	for _, profile := range profiles {
		if profile.Role == role {
			return profile, true
		}
	}
	return laneAblationProfile{}, false
}

func laneModelByRole(models []laneAblationModel, role string) (laneAblationModel, bool) {
	for _, model := range models {
		if model.Role == role {
			return model, true
		}
	}
	return laneAblationModel{}, false
}

func packagedModelMatchesAblation(
	model struct {
		ID             string `json:"id"`
		Role           string `json:"role"`
		Revision       string `json:"revision"`
		Implementation string `json:"implementation"`
		SpecFile       string `json:"specFile"`
		SpecSHA256     string `json:"specSha256"`
		ArtifactFile   string `json:"artifactFile"`
		ArtifactSHA256 string `json:"artifactSha256"`
		License        string `json:"license"`
	},
	evidence laneAblationModel,
) bool {
	return model.ID == evidence.ID && model.Role == evidence.Role &&
		model.Revision == evidence.Revision && model.Implementation == evidence.Implementation &&
		model.SpecSHA256 == evidence.SpecSHA256 && model.ArtifactSHA256 == evidence.ArtifactSHA256
}

func verifySharedModelPins(moduleRoot string, binaryAssets map[string]string) error {
	body, err := os.ReadFile(filepath.Join(moduleRoot, "models", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest packagedModelManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("decode model manifest for packaging: %w", err)
	}
	pinned := map[string]bool{}
	for _, model := range manifest.Models {
		if err := verifyFirstPartyModelLicense(
			model.ID, model.Implementation, model.License,
		); err != nil {
			return err
		}
		if model.ArtifactFile == "" {
			if model.ArtifactSHA256 != "" {
				return fmt.Errorf("model %q has an artifact checksum without an artifact file", model.ID)
			}
			continue
		}
		if model.Implementation != "bundled-local" {
			return fmt.Errorf("model %q has a packaged artifact but is not bundled-local", model.ID)
		}
		if strings.TrimSpace(model.License) == "" {
			return fmt.Errorf("model %q has a packaged artifact without a license", model.ID)
		}
		if _, err := sharedAssetPath(moduleRoot, model.ArtifactFile); err != nil {
			return fmt.Errorf("model %q: %w", model.ID, err)
		}
		actual, found := binaryAssets[model.ArtifactFile]
		if !found {
			return fmt.Errorf("model %q references an unshipped artifact %q", model.ID, model.ArtifactFile)
		}
		if len(model.ArtifactSHA256) != 64 ||
			!strings.EqualFold(model.ArtifactSHA256, actual) {
			return fmt.Errorf("model %q artifact checksum mismatch", model.ID)
		}
		if pinned[model.ArtifactFile] {
			return fmt.Errorf("multiple model entries reference artifact %q", model.ArtifactFile)
		}
		pinned[model.ArtifactFile] = true
	}
	for path := range binaryAssets {
		if !pinned[path] {
			return fmt.Errorf("shared model artifact %q lacks a manifest entry", path)
		}
	}
	return nil
}

func verifyFirstPartyModelLicense(id, implementation, license string) error {
	if implementation == "builtin" && license != "MIT" {
		return fmt.Errorf("first-party builtin model %q must declare the MIT license", id)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeSHA256Sums(moduleRoot, outputRoot string, manifest packageManifest) error {
	paths := manifestPaths(manifest)
	paths = append(paths, "manifest.json")
	type checksumEntry struct {
		Display string
		Path    string
	}
	entries := make([]checksumEntry, 0, len(paths)+len(manifest.SharedAssets))
	for _, relative := range paths {
		path, err := packagePath(outputRoot, relative)
		if err != nil {
			return err
		}
		entries = append(entries, checksumEntry{Display: filepath.ToSlash(relative), Path: path})
	}
	for _, asset := range manifest.SharedAssets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return err
		}
		entries = append(entries, checksumEntry{
			Display: "../" + filepath.ToSlash(asset.Path),
			Path:    path,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Display < entries[j].Display })
	var body strings.Builder
	for _, entry := range entries {
		digest, err := fileSHA256(entry.Path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&body, "%s  %s\n", digest, entry.Display)
	}
	return os.WriteFile(filepath.Join(outputRoot, "SHA256SUMS"), []byte(body.String()), 0o644)
}

func manifestPaths(manifest packageManifest) []string {
	paths := make([]string, 0, len(manifest.Targets)+len(manifest.Launchers)+1)
	for _, artifact := range manifest.Targets {
		paths = append(paths, artifact.Path)
	}
	for _, artifact := range manifest.Launchers {
		paths = append(paths, artifact.Path)
	}
	paths = append(paths, manifest.Notices.Path)
	return paths
}

func packagePath(outputRoot, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid package path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package path escapes output: %q", relative)
	}
	path := filepath.Join(outputRoot, clean)
	relativeCheck, err := filepath.Rel(outputRoot, path)
	if err != nil || relativeCheck == ".." || strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package path escapes output: %q", relative)
	}
	return path, nil
}

func sharedAssetPath(moduleRoot, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid shared asset path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	path := filepath.Join(moduleRoot, clean)
	for _, relativeRoot := range sharedAssetRoots {
		allowedRoot := filepath.Join(moduleRoot, filepath.FromSlash(relativeRoot))
		relativeCheck, err := filepath.Rel(allowedRoot, path)
		if err == nil &&
			relativeCheck != "." &&
			relativeCheck != ".." &&
			!strings.HasPrefix(relativeCheck, ".."+string(filepath.Separator)) {
			return path, nil
		}
	}
	return "", fmt.Errorf("shared asset path is outside packaged runtime asset roots: %q", relative)
}

func verifyPackage(moduleRoot, outputRoot, pinnedGo, buildID string) error {
	_, manifestBody, err := verifyPackageContents(
		moduleRoot, outputRoot, pinnedGo, buildID,
	)
	if err != nil {
		return err
	}

	rebuildRoot, err := os.MkdirTemp(moduleRoot, ".re-discipline-reproducibility-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rebuildRoot)
	if _, err := buildPackageTree(moduleRoot, rebuildRoot, pinnedGo, buildID); err != nil {
		return err
	}
	rebuiltManifest, err := os.ReadFile(filepath.Join(rebuildRoot, "manifest.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(manifestBody, rebuiltManifest) {
		return errors.New("reproducible rebuild changed manifest.json")
	}
	if err := comparePackageTrees(outputRoot, rebuildRoot); err != nil {
		return err
	}
	return nil
}

func verifyPackageContents(
	moduleRoot, outputRoot, pinnedGo, buildID string,
) (packageManifest, []byte, error) {
	manifest, manifestBody, err := readAndValidateManifest(moduleRoot, outputRoot, pinnedGo, buildID)
	if err != nil {
		return packageManifest{}, nil, err
	}
	if err := verifyManifestFiles(moduleRoot, outputRoot, manifest); err != nil {
		return packageManifest{}, nil, err
	}
	if err := verifyWindowsLauncherBuildIdentity(
		outputRoot, pinnedGo, buildID,
	); err != nil {
		return packageManifest{}, nil, err
	}
	expectedSums, err := expectedSHA256Sums(moduleRoot, outputRoot, manifest)
	if err != nil {
		return packageManifest{}, nil, err
	}
	actualSums, err := os.ReadFile(filepath.Join(outputRoot, "SHA256SUMS"))
	if err != nil {
		return packageManifest{}, nil, err
	}
	if !bytes.Equal(actualSums, expectedSums) {
		return packageManifest{}, nil, errors.New("SHA256SUMS does not exactly match package contents")
	}
	return manifest, manifestBody, nil
}

func verifyWindowsLauncherBuildIdentity(
	outputRoot, pinnedGo, expectedBuildID string,
) error {
	if expectedBuildID == "" {
		return errors.New("windows architecture dispatcher build identity is required")
	}
	path, err := packagePath(outputRoot, "re-discipline-knowledge.exe")
	if err != nil {
		return err
	}
	if err := verifyBuiltBinary(
		path, windowsLauncherTarget, pinnedGo, expectedBuildID,
	); err != nil {
		return fmt.Errorf("verify windows architecture dispatcher: %w", err)
	}
	return nil
}

func readAndValidateManifest(
	moduleRoot, outputRoot, pinnedGo, buildID string,
) (packageManifest, []byte, error) {
	path := filepath.Join(outputRoot, "manifest.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return packageManifest{}, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return packageManifest{}, nil, fmt.Errorf("decode manifest: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return packageManifest{}, nil, errors.New("manifest contains trailing JSON")
	}
	if manifest.Schema != "../schemas/runtime-package-manifest.schema.json" ||
		manifest.SchemaVersion != packageSchemaVersion ||
		manifest.Runtime.Name != runtimeName ||
		manifest.Runtime.Version != runtimeVersion ||
		manifest.Runtime.BuildID != buildID {
		return packageManifest{}, nil, errors.New("manifest runtime identity mismatch")
	}
	if manifest.Build.GoToolchain != pinnedGo ||
		manifest.Build.CGOEnabled ||
		manifest.Build.TargetOrder !=
			"windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64" {
		return packageManifest{}, nil, errors.New("manifest build identity mismatch")
	}
	expectedFlags := []string{
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=-s -w -buildid= -X " + runtimeBuildIDPath + "=" + buildID,
	}
	if !equalStrings(manifest.Build.Flags, expectedFlags) {
		return packageManifest{}, nil, errors.New("manifest build flags mismatch")
	}
	expectedEnvironment := []string{
		"CGO_ENABLED=0",
		"GOAMD64=v1",
		"GOARM64=v8.0",
		"GOENV=off",
		"GOEXPERIMENT=",
		"GOFIPS140=off",
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
	}
	if !equalStrings(manifest.Build.Environment, expectedEnvironment) {
		return packageManifest{}, nil, errors.New("manifest build environment mismatch")
	}
	if len(manifest.Targets) != len(supportedTargets) || len(manifest.Launchers) != 2 {
		return packageManifest{}, nil, errors.New("manifest platform matrix is incomplete")
	}
	for index, target := range supportedTargets {
		artifact := manifest.Targets[index]
		expectedPath := filepath.ToSlash(filepath.Join(
			target.GOOS+"-"+target.GOARCH, targetBinaryName(target.GOOS),
		))
		expectedMode := "0755"
		if target.GOOS == "windows" {
			expectedMode = "0644"
		}
		if artifact.Kind != "runtime" ||
			artifact.GOOS != target.GOOS ||
			artifact.GOARCH != target.GOARCH ||
			artifact.Path != expectedPath ||
			artifact.Mode != expectedMode {
			return packageManifest{}, nil, fmt.Errorf("manifest target %d is invalid", index)
		}
	}
	if manifest.Launchers[0].Kind != "posix-dispatch" ||
		manifest.Launchers[0].Path != "re-discipline-knowledge" ||
		manifest.Launchers[0].Mode != "0755" ||
		manifest.Launchers[1].Kind != "windows-architecture-dispatch" ||
		manifest.Launchers[1].Path != "re-discipline-knowledge.exe" ||
		manifest.Launchers[1].GOOS != "windows" ||
		manifest.Launchers[1].GOARCH != "amd64" ||
		manifest.Launchers[1].Mode != "0644" {
		return packageManifest{}, nil, errors.New("manifest launcher contract mismatch")
	}
	if manifest.Notices.Kind != "third-party-notices" ||
		manifest.Notices.Path != "THIRD_PARTY_NOTICES.md" ||
		manifest.Notices.Mode != "0644" {
		return packageManifest{}, nil, errors.New("manifest notices contract mismatch")
	}
	expectedAssets, err := discoverSharedAssets(moduleRoot)
	if err != nil {
		return packageManifest{}, nil, err
	}
	if len(manifest.SharedAssets) != len(expectedAssets) {
		return packageManifest{}, nil, errors.New("manifest shared runtime asset set is incomplete")
	}
	for index := range expectedAssets {
		if manifest.SharedAssets[index] != expectedAssets[index] {
			return packageManifest{}, nil, fmt.Errorf("manifest shared asset %d mismatch", index)
		}
	}
	canonical, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return packageManifest{}, nil, err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(body, canonical) {
		return packageManifest{}, nil, errors.New("manifest is not canonical JSON")
	}
	return manifest, body, nil
}

func verifyManifestFiles(moduleRoot, outputRoot string, manifest packageManifest) error {
	files := append([]manifestFile{}, manifest.Targets...)
	files = append(files, manifest.Launchers...)
	files = append(files, manifest.Notices)
	seen := map[string]bool{}
	for _, artifact := range files {
		if seen[artifact.Path] {
			return fmt.Errorf("duplicate manifest path %q", artifact.Path)
		}
		seen[artifact.Path] = true
		path, err := packagePath(outputRoot, artifact.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("manifest path is not a regular file: %s", artifact.Path)
		}
		if info.Size() != artifact.Size {
			return fmt.Errorf("size mismatch for %s", artifact.Path)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if artifact.SHA256 != "sha256:"+digest {
			return fmt.Errorf("checksum mismatch for %s", artifact.Path)
		}
		if runtime.GOOS != "windows" {
			expectedMode, err := strconv.ParseUint(artifact.Mode, 8, 32)
			if err != nil {
				return err
			}
			if info.Mode().Perm() != fs.FileMode(expectedMode) {
				return fmt.Errorf(
					"mode mismatch for %s: got %04o want %s",
					artifact.Path, info.Mode().Perm(), artifact.Mode,
				)
			}
		}
	}
	if err := verifySharedAssetFiles(moduleRoot, manifest.SharedAssets); err != nil {
		return err
	}
	return verifyNoUnexpectedPackageFiles(outputRoot, seen)
}

func verifySharedAssetFiles(moduleRoot string, assets []manifestFile) error {
	for _, asset := range assets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("shared asset is not a regular file: %s", asset.Path)
		}
		if info.Size() != asset.Size {
			return fmt.Errorf("size mismatch for shared asset %s", asset.Path)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if asset.SHA256 != "sha256:"+digest {
			return fmt.Errorf("checksum mismatch for shared asset %s", asset.Path)
		}
		if asset.Mode != "0644" {
			return fmt.Errorf("invalid shared asset mode for %s", asset.Path)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
			return fmt.Errorf(
				"mode mismatch for shared asset %s: got %04o want 0644",
				asset.Path, info.Mode().Perm(),
			)
		}
	}
	return nil
}

func verifyNoUnexpectedPackageFiles(outputRoot string, expected map[string]bool) error {
	expected["manifest.json"] = true
	expected["SHA256SUMS"] = true
	expectedDirectories := map[string]bool{".": true}
	for relative := range expected {
		directory := filepath.Dir(filepath.FromSlash(relative))
		for directory != "." {
			expectedDirectories[filepath.ToSlash(directory)] = true
			directory = filepath.Dir(directory)
		}
	}
	return filepath.WalkDir(outputRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package cannot contain symlinks: %s", path)
		}
		relative, err := filepath.Rel(outputRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if !expectedDirectories[relative] {
				return fmt.Errorf("unexpected package directory %s", relative)
			}
			return nil
		}
		if !expected[relative] {
			return fmt.Errorf("unexpected package file %s", relative)
		}
		return nil
	})
}

func expectedSHA256Sums(moduleRoot, outputRoot string, manifest packageManifest) ([]byte, error) {
	paths := append(manifestPaths(manifest), "manifest.json")
	type checksumEntry struct {
		Display string
		Path    string
	}
	entries := make([]checksumEntry, 0, len(paths)+len(manifest.SharedAssets))
	for _, relative := range paths {
		path, err := packagePath(outputRoot, relative)
		if err != nil {
			return nil, err
		}
		entries = append(entries, checksumEntry{Display: filepath.ToSlash(relative), Path: path})
	}
	for _, asset := range manifest.SharedAssets {
		path, err := sharedAssetPath(moduleRoot, asset.Path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, checksumEntry{
			Display: "../" + filepath.ToSlash(asset.Path),
			Path:    path,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Display < entries[j].Display })
	var body strings.Builder
	for _, entry := range entries {
		digest, err := fileSHA256(entry.Path)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&body, "%s  %s\n", digest, entry.Display)
	}
	return []byte(body.String()), nil
}

func comparePackageTrees(leftRoot, rightRoot string) error {
	leftFiles, err := packageFileDigests(leftRoot)
	if err != nil {
		return err
	}
	rightFiles, err := packageFileDigests(rightRoot)
	if err != nil {
		return err
	}
	if len(leftFiles) != len(rightFiles) {
		return errors.New("reproducible rebuild changed package file count")
	}
	for path, leftDigest := range leftFiles {
		rightDigest, found := rightFiles[path]
		if !found {
			return fmt.Errorf("reproducible rebuild omitted %s", path)
		}
		if leftDigest != rightDigest {
			return fmt.Errorf("reproducible rebuild changed %s", path)
		}
	}
	return nil
}

func packageFileDigests(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package cannot contain symlinks: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = digest
		return nil
	})
	return result, err
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

const posixLauncher = `#!/bin/sh
set -eu

launcher_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
kernel=$(uname -s)
machine=$(uname -m)

case "$kernel" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *)
    echo "re-discipline-knowledge: unsupported operating system: $kernel" >&2
    exit 1
    ;;
esac

case "$machine" in
  x86_64|amd64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *)
    echo "re-discipline-knowledge: unsupported architecture: $machine" >&2
    exit 1
    ;;
esac

runtime_path="$launcher_dir/$platform-$architecture/re-discipline-knowledge"
if [ ! -f "$runtime_path" ] || [ ! -x "$runtime_path" ]; then
  echo "re-discipline-knowledge: packaged runtime is missing or not executable: $runtime_path" >&2
  exit 1
fi

exec "$runtime_path" "$@"
`
