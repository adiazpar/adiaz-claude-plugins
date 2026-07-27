package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	RuntimeVersion = "0.6.0"
	ParserVersion  = "markdown-structure-v1"
	ChunkerVersion = "section-block-v1"
	SchemaVersion  = 1
)

var AllowedTiers = map[string]bool{
	"profile": true, "navigation": true, "truth": true, "history": true,
	"backlog": true, "active": true, "memory": true, "asset": true,
}

type BootstrapConfig struct {
	SchemaVersion     int             `json:"schemaVersion"`
	SettingsDirectory string          `json:"settingsDirectory"`
	Memory            MemoryConfig    `json:"memory"`
	Knowledge         KnowledgeConfig `json:"knowledge"`
}

type MemoryConfig struct {
	Mode        string `json:"mode"`
	WritePolicy string `json:"writePolicy"`
}

type KnowledgeConfig struct {
	Enabled        bool   `json:"enabled"`
	Profile        string `json:"profile"`
	SettingsFile   string `json:"settingsFile"`
	ProjectProfile string `json:"projectProfile"`
}

type KnowledgeSettings struct {
	SchemaVersion int            `json:"schemaVersion"`
	Sources       SourceSettings `json:"sources"`
	Models        ModelSettings  `json:"models"`
	Telemetry     Telemetry      `json:"telemetry"`
	Budgets       BudgetSettings `json:"budgets"`
}

type SourceSettings struct {
	Truth           bool               `json:"truth"`
	History         bool               `json:"history"`
	Backlog         bool               `json:"backlog"`
	ActiveCampaigns bool               `json:"activeCampaigns"`
	SharedMemory    bool               `json:"sharedMemory"`
	Additional      []AdditionalSource `json:"additional,omitempty"`
}

type AdditionalSource struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Tier    string `json:"tier"`
}

type ModelSettings struct {
	Execution string `json:"execution"`
}

type Telemetry struct {
	Mode string `json:"mode"`
}

type BudgetSettings struct {
	SearchTokens         int `json:"searchTokens"`
	ManagerContextTokens int `json:"managerContextTokens"`
	DrafterContextTokens int `json:"drafterContextTokens"`
	MaxPassages          int `json:"maxPassages"`
	MaxBytes             int `json:"maxBytes"`
}

type Configuration struct {
	Bootstrap BootstrapConfig
	Settings  KnowledgeSettings
	Valid     bool
	Unsafe    bool
	Errors    []string
}

type RetrievalProfile struct {
	Schema            string             `json:"$schema,omitempty"`
	SchemaVersion     int                `json:"schemaVersion"`
	ProfileID         string             `json:"profileId"`
	Description       string             `json:"description"`
	BaseProfile       string             `json:"baseProfile,omitempty"`
	Approval          map[string]any     `json:"approval,omitempty"`
	EffectiveProfiles []EffectiveProfile `json:"effectiveProfiles"`
}

type EffectiveProfile struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Requires       ModelRequirements `json:"requires"`
	Lanes          []string          `json:"lanes"`
	Weights        map[string]int    `json:"weights"`
	RRFK           int               `json:"rrfK"`
	RerankDepth    int               `json:"rerankDepth"`
	MaxPerDocument int               `json:"maxPerDocument"`
	Packing        PackingPolicy     `json:"packing"`
	Benchmark      BenchmarkEvidence `json:"benchmark"`
}

type ModelRequirements struct {
	Embedding *string `json:"embedding"`
	Reranker  *string `json:"reranker"`
}

type PackingPolicy struct {
	MaxPassages int `json:"maxPassages"`
	MaxBytes    int `json:"maxBytes"`
}

type BenchmarkEvidence struct {
	Suite              string `json:"suite"`
	Digest             string `json:"digest"`
	Status             string `json:"status"`
	EvaluatedAt        string `json:"evaluatedAt,omitempty"`
	EvalFingerprint    string `json:"evalFingerprint,omitempty"`
	CorpusFingerprint  string `json:"corpusFingerprint,omitempty"`
	ModelFingerprint   string `json:"modelFingerprint,omitempty"`
	RuntimeFingerprint string `json:"runtimeFingerprint,omitempty"`
	// RatifiedHardNegativeHits and RatifiedAbstentionAccuracy record the
	// values this profile was accepted at. calibrationNonInferior compares a
	// candidate against the tighter of these and the incumbent's score on
	// today's corpus, so the ratchet can tighten but never loosen.
	//
	// Without them the comparison re-anchors every run: a corpus edit that
	// raises the incumbent's hard-negative count silently raises the ceiling
	// a future candidate must clear, and the drift is never recorded.
	RatifiedHardNegativeHits   int     `json:"ratifiedHardNegativeHits,omitempty"`
	RatifiedAbstentionAccuracy float64 `json:"ratifiedAbstentionAccuracy,omitempty"`
}

type ModelManifest struct {
	Schema              string          `json:"$schema,omitempty"`
	SchemaVersion       int             `json:"schemaVersion"`
	Runtime             RuntimeManifest `json:"runtime"`
	Models              []ModelSpec     `json:"models"`
	ExternalModelPolicy map[string]any  `json:"externalModelPolicy"`
	// ExecutableModels and UnavailableModels are runtime observations, not
	// trusted manifest input. Keeping them on the loaded manifest makes model
	// availability service-local and prevents one project/plugin revision from
	// changing another service's selected model identity.
	ExecutableModels  map[string]ModelIdentity `json:"-"`
	UnavailableModels map[string]string        `json:"-"`
}

type RuntimeManifest struct {
	Implementation   string `json:"implementation"`
	Version          string `json:"version"`
	SQLiteDriver     string `json:"sqliteDriver"`
	NumericalBackend string `json:"numericalBackend"`
	TieBreaker       string `json:"tieBreaker"`
}

type ModelSpec struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	Revision        string `json:"revision"`
	Implementation  string `json:"implementation"`
	SpecFile        string `json:"specFile"`
	SpecSHA256      string `json:"specSha256"`
	ArtifactFile    string `json:"artifactFile,omitempty"`
	ArtifactSHA256  string `json:"artifactSha256,omitempty"`
	License         string `json:"license"`
	Dimensions      int    `json:"dimensions,omitempty"`
	Normalization   string `json:"normalization,omitempty"`
	NetworkRequired bool   `json:"networkRequired"`
	Description     string `json:"description,omitempty"`
}

type RuntimeIdentity struct {
	Implementation   string `json:"implementation"`
	Version          string `json:"version"`
	GoVersion        string `json:"goVersion"`
	CompiledBuildID  string `json:"compiledBuildId"`
	ExecutableSHA256 string `json:"executableSha256"`
	SQLiteDriver     string `json:"sqliteDriver"`
	SQLiteVersion    string `json:"sqliteVersion"`
	SQLiteBuild      string `json:"sqliteBuild"`
	NumericalBackend string `json:"numericalBackend"`
	TieBreaker       string `json:"tieBreaker"`
}

type RuntimeContractIdentity struct {
	Implementation   string `json:"implementation"`
	Version          string `json:"version"`
	GoVersion        string `json:"goVersion"`
	CompiledBuildID  string `json:"compiledBuildId"`
	SQLiteDriver     string `json:"sqliteDriver"`
	SQLiteVersion    string `json:"sqliteVersion"`
	SQLiteBuild      string `json:"sqliteBuild"`
	NumericalBackend string `json:"numericalBackend"`
	TieBreaker       string `json:"tieBreaker"`
}

func RuntimeContract(runtime RuntimeIdentity) RuntimeContractIdentity {
	sqliteSourceContract := "sha256:" + SHA256String(strings.Join([]string{
		runtime.SQLiteDriver,
		runtime.SQLiteVersion,
		"source-contract-" + RuntimeVersion,
	}, "\n"))
	return RuntimeContractIdentity{
		Implementation: runtime.Implementation, Version: runtime.Version,
		GoVersion: runtime.GoVersion,
		// This is the portable source contract, not host-specific packaging or
		// SQLite compile-option identity. RuntimeIdentity separately reports
		// the actual linked build, executable, and SQLite build checksums.
		CompiledBuildID: "source-contract-" + RuntimeVersion,
		SQLiteDriver:    runtime.SQLiteDriver, SQLiteVersion: runtime.SQLiteVersion,
		SQLiteBuild: sqliteSourceContract, NumericalBackend: runtime.NumericalBackend,
		TieBreaker: runtime.TieBreaker,
	}
}

type ModelIdentity struct {
	ID               string `json:"id"`
	Revision         string `json:"revision"`
	SpecSHA256       string `json:"specSha256"`
	ArtifactSHA256   string `json:"artifactSha256,omitempty"`
	Implementation   string `json:"implementation"`
	NumericalBackend string `json:"numericalBackend"`
	Dimensions       int    `json:"dimensions,omitempty"`
}

type SelectedProfile struct {
	RequestedIdentity string           `json:"requestedIdentity"`
	EffectiveIdentity string           `json:"effectiveIdentity"`
	Effective         EffectiveProfile `json:"effective"`
	ActiveLanes       []string         `json:"activeLanes"`
	Models            []ModelIdentity  `json:"models"`
	FallbackReason    *string          `json:"fallbackReason"`
	Runtime           RuntimeIdentity  `json:"runtime"`
}

type SourceDocument struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Tier        string `json:"tier"`
	Title       string `json:"title"`
	Content     string `json:"-"`
	ContentHash string `json:"-"`
	Size        int64  `json:"size"`
	MtimeNS     int64  `json:"mtimeNs"`
}

type SourceState struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtimeNs"`
}

type Chunk struct {
	ID          string `json:"id"`
	DocumentID  string `json:"documentId"`
	Path        string `json:"path"`
	Tier        string `json:"tier"`
	Heading     string `json:"heading"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
	ParentID    string `json:"parentId,omitempty"`
	PreviousID  string `json:"previousId,omitempty"`
	NextID      string `json:"nextId,omitempty"`
}

type Generation struct {
	ID                string          `json:"id"`
	Database          string          `json:"database"`
	CorpusFingerprint string          `json:"corpusFingerprint"`
	ModelFingerprint  string          `json:"modelFingerprint"`
	Project           string          `json:"project"`
	Worktree          string          `json:"worktree"`
	GitRevision       string          `json:"gitRevision"`
	DirtyFingerprint  string          `json:"dirtyFingerprint"`
	ParserVersion     string          `json:"parserVersion"`
	ChunkerVersion    string          `json:"chunkerVersion"`
	CreatedAt         string          `json:"createdAt"`
	Runtime           RuntimeIdentity `json:"runtime"`
	DocumentCount     int             `json:"documentCount"`
	ChunkCount        int             `json:"chunkCount"`
	SourceStates      []SourceState   `json:"sourceStates"`
	DirectoryStates   []SourceState   `json:"directoryStates"`
	ServingStale      bool            `json:"servingStale,omitempty"`
	WriterContention  bool            `json:"writerContention,omitempty"`
}

type GenerationSummary struct {
	ID                string          `json:"id"`
	CorpusFingerprint string          `json:"corpusFingerprint"`
	ModelFingerprint  string          `json:"modelFingerprint"`
	Project           string          `json:"project"`
	Worktree          string          `json:"worktree"`
	GitRevision       string          `json:"gitRevision"`
	DirtyFingerprint  string          `json:"dirtyFingerprint"`
	ParserVersion     string          `json:"parserVersion"`
	ChunkerVersion    string          `json:"chunkerVersion"`
	CreatedAt         string          `json:"createdAt"`
	Runtime           RuntimeIdentity `json:"runtime"`
	DocumentCount     int             `json:"documentCount"`
	ChunkCount        int             `json:"chunkCount"`
	ServingStale      bool            `json:"servingStale,omitempty"`
	WriterContention  bool            `json:"writerContention,omitempty"`
}

func PublicGeneration(generation Generation) GenerationSummary {
	return GenerationSummary{
		ID: generation.ID, CorpusFingerprint: generation.CorpusFingerprint,
		ModelFingerprint: generation.ModelFingerprint, Project: generation.Project,
		Worktree: generation.Worktree, GitRevision: generation.GitRevision,
		DirtyFingerprint: generation.DirtyFingerprint,
		ParserVersion:    generation.ParserVersion, ChunkerVersion: generation.ChunkerVersion,
		CreatedAt: generation.CreatedAt, Runtime: generation.Runtime,
		DocumentCount: generation.DocumentCount, ChunkCount: generation.ChunkCount,
		ServingStale: generation.ServingStale, WriterContention: generation.WriterContention,
	}
}

type Citation struct {
	Path        string `json:"path"`
	Heading     string `json:"heading"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	ContentHash string `json:"-"`
	SourceHash  string `json:"sourceHash"`
	PassageHash string `json:"passageHash"`
	Tier        string `json:"tier"`
	URI         string `json:"uri"`
}

type SearchResult struct {
	ChunkID   string         `json:"chunkId"`
	Score     int64          `json:"score"`
	Rerank    int64          `json:"rerankScore,omitempty"`
	LaneRanks map[string]int `json:"laneRanks"`
	Passage   string         `json:"passage"`
	Citation  Citation       `json:"citation"`
}

type RetrievalMetadata struct {
	Project             string   `json:"project"`
	Worktree            string   `json:"worktree"`
	GitRevision         string   `json:"gitRevision"`
	DirtyFingerprint    string   `json:"dirtyFingerprint"`
	Generation          string   `json:"generation"`
	CorpusFingerprint   string   `json:"corpusFingerprint"`
	ParserVersion       string   `json:"parserVersion"`
	ChunkerVersion      string   `json:"chunkerVersion"`
	RequestedProfile    string   `json:"requestedProfile"`
	EffectiveProfile    string   `json:"effectiveProfile"`
	ActiveLanes         []string `json:"activeLanes"`
	Models              []string `json:"models"`
	ModelFingerprint    string   `json:"modelFingerprint"`
	RuntimeFingerprint  string   `json:"runtimeFingerprint"`
	FallbackReason      *string  `json:"fallbackReason"`
	DeterministicReplay string   `json:"deterministicReplay"`
	ServingStale        bool     `json:"servingStale,omitempty"`
	WriterContention    bool     `json:"writerContention,omitempty"`
}

type SearchResponse struct {
	Query           string            `json:"query"`
	QueryClass      string            `json:"queryClass"`
	AllowedTiers    []string          `json:"allowedTiers"`
	TokenBudget     int               `json:"tokenBudget"`
	EstimatedTokens int               `json:"estimatedTokens"`
	Results         []SearchResult    `json:"results"`
	Omitted         int               `json:"omitted"`
	OmittedByReason map[string]int    `json:"omittedByReason"`
	Metadata        RetrievalMetadata `json:"metadata"`
}

type ContextPassage struct {
	ChunkID  string   `json:"chunkId"`
	Passage  string   `json:"passage"`
	Citation Citation `json:"citation"`
}

type ContextPack struct {
	SchemaVersion    int                       `json:"schemaVersion"`
	PackID           string                    `json:"packId"`
	Digest           string                    `json:"digest"`
	Query            string                    `json:"query"`
	Generation       ContextGenerationIdentity `json:"generation"`
	Role             string                    `json:"role"`
	AllowedTiers     []string                  `json:"allowedTiers"`
	RequestedProfile string                    `json:"requestedProfile"`
	EffectiveProfile string                    `json:"effectiveProfile"`
	ActiveLanes      []string                  `json:"activeLanes"`
	Models           []string                  `json:"models"`
	FallbackReason   *string                   `json:"fallbackReason"`
	TokenBudget      int                       `json:"tokenBudget"`
	EstimatedTokens  int                       `json:"estimatedTokens"`
	Passages         []ContextPassage          `json:"passages"`
	Omitted          map[string]int            `json:"omitted"`
}

type ContextGenerationIdentity struct {
	ID                 string `json:"id"`
	CorpusFingerprint  string `json:"corpusFingerprint"`
	ModelFingerprint   string `json:"modelFingerprint"`
	RuntimeFingerprint string `json:"runtimeFingerprint"`
	Project            string `json:"project"`
	Worktree           string `json:"worktree"`
	GitRevision        string `json:"gitRevision"`
	DirtyFingerprint   string `json:"dirtyFingerprint"`
	ParserVersion      string `json:"parserVersion"`
	ChunkerVersion     string `json:"chunkerVersion"`
	CreatedAt          string `json:"createdAt"`
}

func CompactContextGeneration(generation Generation) ContextGenerationIdentity {
	runtimeFingerprint, _ := CanonicalDigest(generation.Runtime)
	return ContextGenerationIdentity{
		ID: generation.ID, CorpusFingerprint: generation.CorpusFingerprint,
		ModelFingerprint:   generation.ModelFingerprint,
		RuntimeFingerprint: runtimeFingerprint,
		Project:            generation.Project, Worktree: generation.Worktree,
		GitRevision:      generation.GitRevision,
		DirtyFingerprint: generation.DirtyFingerprint,
		ParserVersion:    generation.ParserVersion,
		ChunkerVersion:   generation.ChunkerVersion,
		CreatedAt:        generation.CreatedAt,
	}
}

type EvalCase struct {
	ID                   string         `json:"id"`
	Role                 string         `json:"role"`
	Topic                string         `json:"topic"`
	Split                string         `json:"split"`
	Query                string         `json:"query"`
	QueryClass           string         `json:"queryClass"`
	AllowedTiers         []string       `json:"allowedTiers"`
	CorpusSnapshot       string         `json:"corpusSnapshot"`
	ExpectedPaths        []string       `json:"expectedPaths"`
	GradedRelevantPaths  map[string]int `json:"gradedRelevantPaths,omitempty"`
	MinimumEvidencePaths []string       `json:"minimumEvidencePaths"`
	HardNegativePaths    []string       `json:"hardNegativePaths"`
	ExpectedCitations    []string       `json:"expectedCitations"`
	ForbiddenTiers       []string       `json:"forbiddenTiers"`
	TokenBudget          int            `json:"tokenBudget"`
	Answerable           *bool          `json:"answerable"`
}

func SHA256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func SHA256String(value string) string {
	return SHA256Bytes([]byte(value))
}

func CanonicalDigest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return "sha256:" + SHA256Bytes(body), nil
}

func StableID(prefix string, values ...string) string {
	return prefix + "-" + SHA256String(strings.Join(values, "\x00"))[:20]
}

func NormalizeProjectPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func SortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedModelIdentityValues(values map[string]ModelIdentity) []ModelIdentity {
	result := make([]ModelIdentity, 0, len(values))
	for _, identity := range values {
		result = append(result, identity)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		if result[i].SpecSHA256 != result[j].SpecSHA256 {
			return result[i].SpecSHA256 < result[j].SpecSHA256
		}
		return result[i].ArtifactSHA256 < result[j].ArtifactSHA256
	})
	return result
}

func modelIndexFingerprint(manifest ModelManifest) string {
	digest, _ := CanonicalDigest(struct {
		Models      []ModelSpec       `json:"models"`
		Executable  []ModelIdentity   `json:"executable"`
		Unavailable map[string]string `json:"unavailable"`
	}{
		Models: manifest.Models, Executable: sortedModelIdentityValues(manifest.ExecutableModels),
		Unavailable: manifest.UnavailableModels,
	})
	return digest
}

func EstimateTokens(value string) int {
	if value == "" {
		return 0
	}
	return (len([]byte(value)) + 3) / 4
}

func RFC3339UTC(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func ValidateTierList(tiers []string) ([]string, error) {
	if len(tiers) == 0 {
		return nil, fmt.Errorf("at least one allowed tier is required")
	}
	out := SortedUnique(tiers)
	for _, tier := range out {
		if !AllowedTiers[tier] {
			return nil, fmt.Errorf("unsupported epistemic tier %q", tier)
		}
	}
	return out, nil
}
