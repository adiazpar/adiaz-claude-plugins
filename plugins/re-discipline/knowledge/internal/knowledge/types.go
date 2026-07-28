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
	// Bumped for the per-chunk document prelude. index.go forces a full
	// rebuild when this value changes.
	ChunkerVersion = "section-block-v3"
	SchemaVersion  = 1
)

var AllowedTiers = map[string]bool{
	"profile": true, "navigation": true, "truth": true, "history": true,
	"backlog": true, "active": true, "memory": true, "asset": true,
	// campaign: drafter reports a manager has reviewed and stamped.
	// draft:    drafter reports nobody has reviewed yet.
	//
	// The split lands exactly on the review-subagent Wall, so it records a
	// decision a manager already makes rather than introducing a new one.
	// `draft` is in no default tier set: unreviewed drafter output must be
	// asked for by name, and the request is visible in a pack's allowedTiers.
	"campaign": true, "draft": true,
}

// EphemeralTiers hold content that is deleted when its campaign closes. A
// citation into one of these is a handle to something scheduled to vanish,
// which is categorically different from a citation into docs/.
var EphemeralTiers = map[string]bool{
	"active": true, "campaign": true, "draft": true,
}

// CitationDurability reports whether a tier's citations outlive their campaign.
func CitationDurability(tier string) string {
	if EphemeralTiers[tier] {
		return "ephemeral"
	}
	return "durable"
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
	Truth           bool `json:"truth"`
	History         bool `json:"history"`
	Backlog         bool `json:"backlog"`
	ActiveCampaigns bool `json:"activeCampaigns"`
	SharedMemory    bool `json:"sharedMemory"`
	// DrafterReports indexes active/*/subagents/*/report.md. Reviewed reports
	// land in the `campaign` tier; unreviewed ones in `draft`, which is in no
	// default tier set and must be asked for by name.
	DrafterReports bool               `json:"drafterReports"`
	Additional     []AdditionalSource `json:"additional,omitempty"`
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
	// ChunkerVersion and ParserVersion record how the corpus was segmented when
	// this profile was measured. The corpus fingerprint mixes both versions in
	// with the document set, so a re-chunk and an ordinary documentation edit
	// are indistinguishable there - and only one of them means the measured
	// behavior itself may have changed. Recording them separately lets status
	// tell an informational corpus edit apart from an actionable re-chunk.
	//
	// Both are optional. A profile measured before this field existed records
	// neither, and status then declines to guess rather than telling every such
	// project to re-run.
	ChunkerVersion string `json:"chunkerVersion,omitempty"`
	ParserVersion  string `json:"parserVersion,omitempty"`
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
	// Context is the document's epistemic header, recomputed per chunk. It is
	// deliberately NOT part of Content: verifyChunk re-reads the source lines
	// and requires an exact match, so synthesized text in Content would make
	// every chunk fail verification and be dropped as a stale source.
	Context     string `json:"context,omitempty"`
	ContextHash string `json:"contextHash,omitempty"`
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

// Verbosity selects how much re-derivable citation and provenance metadata a
// response carries. It never changes which passages are selected, only what is
// serialized beside them - and therefore how much of the caller's token budget
// is left for evidence.
//
// A verbose search result measured 119 tokens of citation for a 56-token
// passage. Of that, the generation URI, the document hash and the prelude hash
// are 82 tokens that a caller cannot act on: the URI is generation + chunkId,
// both already present, and read accepts the chunkId directly. Compact drops
// exactly those and keeps path, heading, line range, passage hash and tier, so
// every citation stays independently re-derivable.
const (
	VerbosityCompact = "compact"
	VerbosityVerbose = "verbose"
)

// NormalizeVerbosity defaults an unset verbosity to compact and rejects
// anything else. Compact is the default because the budget a caller states is
// the budget it means to spend on evidence, not on repeating the generation
// identity once per passage.
func NormalizeVerbosity(value string) (string, error) {
	switch value {
	case "", VerbosityCompact:
		return VerbosityCompact, nil
	case VerbosityVerbose:
		return VerbosityVerbose, nil
	default:
		return "", fmt.Errorf("unsupported verbosity %q", value)
	}
}

type Citation struct {
	Path      string `json:"path"`
	Heading   string `json:"heading"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	// ContentHash never reaches the wire; it is the in-process passage digest
	// the evaluator and the packing loop compare against.
	ContentHash string `json:"-"`
	// SourceHash and URI are omitted in compact responses. The document hash is
	// re-derivable by hashing the cited file, and the URI is the generation ID
	// and the chunk ID concatenated - both already carried by the response.
	// omitempty keeps verbose output byte-identical to earlier releases, which
	// matters because stored packs are re-marshalled and compared against their
	// own recorded digest.
	SourceHash  string `json:"sourceHash,omitempty"`
	PassageHash string `json:"passageHash"`
	Tier        string `json:"tier"`
	URI         string `json:"uri,omitempty"`
	// omitempty for the same reason as SearchResult.DocumentContext: stored
	// packs are re-marshalled and verified against their recorded digest.
	ContextHash string `json:"contextHash,omitempty"`
	// Durability says whether this handle outlives its campaign. Campaign and
	// draft citations are deliberately mortal - close-campaign removes the
	// directory - and an agent holding one needs to know that before relying
	// on it. The chronicle is their durable projection, not the handle.
	Durability string `json:"durability,omitempty"`
}

type SearchResult struct {
	ChunkID string `json:"chunkId"`
	// Score, Rerank and LaneRanks are ranking diagnostics. Nothing outside the
	// retriever reads them, and no caller can act on them, so compact responses
	// drop them. A packed result always carries a positive fusion score, so
	// omitempty never hides a real value in a verbose response.
	Score     int64          `json:"score,omitempty"`
	Rerank    int64          `json:"rerankScore,omitempty"`
	LaneRanks map[string]int `json:"laneRanks,omitempty"`
	Passage   string         `json:"passage"`
	// DocumentContext is the epistemic header of the document this passage
	// came from: what it claims, how strongly, when it was last verified, and
	// whether it has been superseded. Empty for a document's opening chunk,
	// which already contains the header.
	//
	// omitempty is required. Stored context packs are re-marshalled and
	// compared against their own recorded digest, so an always-present empty
	// field would invalidate every pack written before this field existed.
	DocumentContext string   `json:"documentContext,omitempty"`
	Citation        Citation `json:"citation"`
}

// RetrievalMetadata measured 242 tokens on a verbose search response, a
// quarter of a 1024-token budget spent before a single passage. Compact keeps
// the fields a caller or an evaluator acts on - which project, which
// generation, which corpus state, which profile actually served, why it fell
// back, and the replay handle - and omits the rest, all of which is constant
// for the generation and available from status.
type RetrievalMetadata struct {
	Project             string   `json:"project"`
	Worktree            string   `json:"worktree,omitempty"`
	GitRevision         string   `json:"gitRevision,omitempty"`
	DirtyFingerprint    string   `json:"dirtyFingerprint,omitempty"`
	Generation          string   `json:"generation"`
	CorpusFingerprint   string   `json:"corpusFingerprint"`
	ParserVersion       string   `json:"parserVersion,omitempty"`
	ChunkerVersion      string   `json:"chunkerVersion,omitempty"`
	RequestedProfile    string   `json:"requestedProfile,omitempty"`
	EffectiveProfile    string   `json:"effectiveProfile"`
	ActiveLanes         []string `json:"activeLanes,omitempty"`
	Models              []string `json:"models,omitempty"`
	ModelFingerprint    string   `json:"modelFingerprint,omitempty"`
	RuntimeFingerprint  string   `json:"runtimeFingerprint,omitempty"`
	FallbackReason      *string  `json:"fallbackReason"`
	DeterministicReplay string   `json:"deterministicReplay"`
	ServingStale        bool     `json:"servingStale,omitempty"`
	WriterContention    bool     `json:"writerContention,omitempty"`
}

// Compact returns the projection a compact response carries.
func (metadata RetrievalMetadata) Compact() RetrievalMetadata {
	return RetrievalMetadata{
		Project: metadata.Project, Generation: metadata.Generation,
		CorpusFingerprint: metadata.CorpusFingerprint,
		EffectiveProfile:  metadata.EffectiveProfile,
		FallbackReason:    metadata.FallbackReason,
		// The replay handle already digests the requested profile, the tiers,
		// the limit and the budget, so it remains the reproducibility anchor
		// after the individual fingerprints are dropped.
		DeterministicReplay: metadata.DeterministicReplay,
		ServingStale:        metadata.ServingStale,
		WriterContention:    metadata.WriterContention,
	}
}

type SearchResponse struct {
	Query        string   `json:"query"`
	QueryClass   string   `json:"queryClass"`
	AllowedTiers []string `json:"allowedTiers"`
	// Verbosity records which citation and metadata projection was serialized,
	// so a caller reading a stored response knows whether an absent sourceHash
	// means "compacted away" or "missing".
	Verbosity       string `json:"verbosity,omitempty"`
	TokenBudget     int    `json:"tokenBudget"`
	EstimatedTokens int    `json:"estimatedTokens"`
	// ContextTokens is how many of EstimatedTokens the per-passage document
	// preludes consumed. The prelude stays charged against the caller budget -
	// EstimatedTokens remains the single number that describes what was
	// serialized, and the budget stays a hard ceiling - but a caller that wants
	// to know what epistemic safety costs it can now read that separately
	// instead of inferring it.
	ContextTokens   int               `json:"contextTokens,omitempty"`
	Results         []SearchResult    `json:"results"`
	Omitted         int               `json:"omitted"`
	OmittedByReason map[string]int    `json:"omittedByReason"`
	Metadata        RetrievalMetadata `json:"metadata"`
}

type ContextPassage struct {
	ChunkID string `json:"chunkId"`
	Passage string `json:"passage"`
	// omitempty is load-bearing: MaterializeContextPackExpected re-marshals a
	// stored pack and compares it against its own recorded digest, so a field
	// that always serializes would invalidate every previously written pack.
	DocumentContext string   `json:"documentContext,omitempty"`
	Citation        Citation `json:"citation"`
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
	// Verbosity records which citation projection this pack carries, so a
	// reader knows whether an absent sourceHash was compacted away or is
	// missing. omitempty is load-bearing for the same reason as
	// ContextPassage.DocumentContext: a pack written before this field existed
	// is re-marshalled and compared against its own recorded digest, and an
	// always-present field would invalidate every one of them.
	Verbosity string           `json:"verbosity,omitempty"`
	Passages  []ContextPassage `json:"passages"`
	Omitted   map[string]int   `json:"omitted"`
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
	// EvidencePins supersede CorpusSnapshot when present. A corpus-wide
	// fingerprint invalidates every case on any edit anywhere, which on a
	// corpus that changes daily means the measurement is almost never valid.
	// A pin instead tracks the documents this case actually depends on, and
	// gates on what those documents CLAIM rather than on their exact bytes.
	EvidencePins []EvidencePin `json:"evidencePins,omitempty"`
}

// EvidencePin ties a case to one document it depends on.
type EvidencePin struct {
	Path string `json:"path"`
	// ClaimSha256 digests the document's claim, confidence grade and
	// supersession status. It is the gate: it changes only when what the
	// document asserts changes, so rewording evidence prose does not
	// invalidate a case whose ground truth still holds.
	ClaimSha256 string `json:"claimSha256"`
	// ContentSha256 digests the whole file. Recorded and reported as advisory
	// drift; it never gates.
	ContentSha256 string `json:"contentSha256,omitempty"`
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
