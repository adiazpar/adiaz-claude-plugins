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
	// The one definition of the published version. The packager, the package
	// audit, and .github/scripts/re-discipline-sync-version.py all derive
	// from this; a release is this line plus a manifest sync.
	RuntimeVersion = "0.8.5"
	ParserVersion  = "markdown-finding-v2-identifier-v1"
	// Bumped for the per-chunk document prelude, then again for the opening
	// chunk of an unreviewed drafter report. index.go forces a full rebuild
	// when this value changes.
	//
	// The second bump does not change any chunk's content hash - the prelude
	// is stored beside Content, never inside it - but an index built before it
	// serves the opening chunk of an unreviewed report with no UNREVIEWED
	// marker, which is the exact failure the change exists to close. A stale
	// index that is merely less useful can be tolerated; one that is less safe
	// cannot.
	ChunkerVersion = "section-block-v6-finding-freshness"
	// BootstrapSchemaVersion is the .re-discipline/config.json contract. The
	// 0.8 hard cutover adds campaign state, authority, bounded-context,
	// payload, closure, and explicit-migration policy to the former knowledge
	// bootstrap. Knowledge policy keeps its own independent schema version.
	BootstrapSchemaVersion = 3
	SettingsSchemaVersion  = 2
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
	// Finding-plane retrieval classes used by the 0.8 public query surface.
	// Older path tiers remain internal only where the explicit migrator or
	// passage-level benchmark needs to inspect legacy material.
	"provisional": true, "archive": true, "intake": true, "playbook": true,
}

// EphemeralTiers hold content that is deleted when its campaign closes. A
// citation into one of these is a handle to something scheduled to vanish,
// which is categorically different from a citation into docs/.
var EphemeralTiers = map[string]bool{
	"active": true, "campaign": true, "draft": true,
	"provisional": true, "intake": true,
}

// CitationDurability reports whether a tier's citations outlive their campaign.
func CitationDurability(tier string) string {
	if EphemeralTiers[tier] {
		return "ephemeral"
	}
	return "durable"
}

type BootstrapConfig struct {
	SchemaVersion         int              `json:"schemaVersion"`
	CampaignSchemaVersion int              `json:"campaignSchemaVersion"`
	State                 StateConfig      `json:"state"`
	Authority             AuthorityConfig  `json:"authority"`
	Context               ContextConfig    `json:"context"`
	Payload               PayloadConfig    `json:"payload"`
	ReviewLoad            ReviewLoadConfig `json:"reviewLoad"`
	Closure               ClosureConfig    `json:"closure"`
	Memory                MemoryConfig     `json:"memory"`
	Knowledge             KnowledgeConfig  `json:"knowledge"`
	Migration             MigrationConfig  `json:"migration"`
}

type StateConfig struct {
	ActiveRoot            string `json:"activeRoot"`
	ArchiveRoot           string `json:"archiveRoot"`
	LockFile              string `json:"lockFile"`
	Recovery              string `json:"recovery"`
	GeneratedViewMaxItems int    `json:"generatedViewMaxItems"`
}

type AuthorityConfig struct {
	ManagerRoles      []string `json:"managerRoles"`
	CuratorWrites     []string `json:"curatorWrites"`
	DirectStateWrites bool     `json:"directStateWrites"`
	TruthProjection   string   `json:"truthProjection"`
}

type ContextConfig struct {
	ManagerCardTokens int    `json:"managerCardTokens"`
	DrafterCardTokens int    `json:"drafterCardTokens"`
	MaxCards          int    `json:"maxCards"`
	MaxExpansionBytes int    `json:"maxExpansionBytes"`
	LeaseMode         string `json:"leaseMode"`
}

type PayloadConfig struct {
	CreateLazily        bool `json:"createLazily"`
	MaxInlineBytes      int  `json:"maxInlineBytes"`
	RequireRegistration bool `json:"requireRegistration"`
}

// ReviewLoadConfig makes manager attention an explicit, measurable budget.
// Receipts copy these values and bind their canonical digest so a later config
// edit cannot silently reinterpret an earlier pilot measurement.
type ReviewLoadConfig struct {
	TargetMinutesPerPacket  int `json:"targetMinutesPerPacket"`
	TargetPacketsPerSession int `json:"targetPacketsPerSession"`
}

type ClosureConfig struct {
	RequireRunCoverage         bool `json:"requireRunCoverage"`
	RequireFindingDisposition  bool `json:"requireFindingDisposition"`
	RequireFileRetention       bool `json:"requireFileRetention"`
	RequireArchiveVerification bool `json:"requireArchiveVerification"`
}

type MigrationConfig struct {
	Mode          string `json:"mode"`
	LegacyReaders string `json:"legacyReaders"`
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
	Schema        string          `json:"$schema,omitempty"`
	SchemaVersion int             `json:"schemaVersion"`
	Sources       SourceSettings  `json:"sources"`
	Models        ModelSettings   `json:"models"`
	Telemetry     Telemetry       `json:"telemetry"`
	Budgets       BudgetSettings  `json:"budgets"`
	Archive       ArchiveSettings `json:"archive"`
}

type SourceSettings struct {
	Truth           bool `json:"truth"`
	HistoryFindings bool `json:"historyFindings"`
	Backlog         bool `json:"backlog"`
	ActiveFindings  bool `json:"activeFindings"`
	SharedMemory    bool `json:"sharedMemory"`
	// ReportFallback keeps immutable run reports in the lower-ranked raw
	// provenance lane until the normalized-beats-raw release gate is ratified.
	ReportFallback bool               `json:"reportFallback"`
	Additional     []AdditionalSource `json:"additional,omitempty"`
}

type AdditionalSource struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Tier    string `json:"sourceClass"`
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
	MaxPassages          int `json:"maxCards"`
	MaxBytes             int `json:"maxBytes"`
}

type ArchiveSettings struct {
	ReportFallbackUntilMeasured bool   `json:"reportFallbackUntilMeasured"`
	NormalizationTriggerHits    int    `json:"normalizationTriggerHits"`
	FallbackMode                string `json:"fallbackMode,omitempty"`
	NormalizedBeatsRawReceipt   string `json:"normalizedBeatsRawReceipt,omitempty"`
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
	MaxPerDocument int               `json:"maxPerDocument"`
	Packing        PackingPolicy     `json:"packing"`
	Benchmark      BenchmarkEvidence `json:"benchmark"`
}

// ModelRequirements binds an executable profile row to the exact local
// embedding declared by the model manifest. Reranking is intentionally absent:
// the controlled project benchmark measured no rerank benefit in 64 cases.
type ModelRequirements struct {
	Embedding *string `json:"embedding"`
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
	//
	// The hard-negative count is a pointer because zero is its best possible
	// value and also the value an absent field decodes to. Held as a plain
	// int, a profile ratified at zero hard-negative hits recorded nothing and
	// therefore clamped nothing, so the ceiling was free to drift upward in
	// exactly the case where it must not move at all. nil means no ratified
	// value was recorded - every profile promoted before this field existed -
	// and leaves the ratchet disengaged, unchanged.
	//
	// Abstention accuracy needs no such distinction: the clamp takes the
	// larger of the ratified and the live value, and zero is the loosest
	// possible floor, so an absent field and a genuine zero behave alike.
	RatifiedHardNegativeHits   *int    `json:"ratifiedHardNegativeHits,omitempty"`
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
	ID    string `json:"id"`
	Path  string `json:"path"`
	Tier  string `json:"tier"`
	Title string `json:"title"`
	// SourceKind distinguishes normalized findings, ordinary managed Markdown,
	// and raw provenance while keeping the derived index rebuildable.
	SourceKind    string `json:"sourceKind,omitempty"`
	FindingID     string `json:"findingId,omitempty"`
	CampaignID    string `json:"campaignId,omitempty"`
	FindingClaim  string `json:"findingClaim,omitempty"`
	EvidenceGrade string `json:"evidenceGrade,omitempty"`
	ReviewState   string `json:"reviewState,omitempty"`
	Validity      string `json:"validity,omitempty"`
	Content       string `json:"-"`
	ContentHash   string `json:"-"`
	Size          int64  `json:"size"`
	MtimeNS       int64  `json:"mtimeNs"`
}

type SourceState struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtimeNs"`
}

type Chunk struct {
	ID         string `json:"id"`
	DocumentID string `json:"documentId"`
	Path       string `json:"path"`
	Tier       string `json:"tier"`
	Heading    string `json:"heading"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	// ByteRange is set only when a single source line had to be split. The
	// offsets are an absolute half-open range over normalized UTF-8 source
	// bytes, preserving an exact citation while enforcing the chunk ceiling.
	ByteRange   bool   `json:"byteRange,omitempty"`
	StartByte   int    `json:"startByte,omitempty"`
	EndByte     int    `json:"endByte,omitempty"`
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
	ID                string `json:"id"`
	Database          string `json:"database"`
	CorpusFingerprint string `json:"corpusFingerprint"`
	// SettingsFingerprint digests the settings that decide WHICH files are
	// indexed. The corpus fingerprint answers "did the indexed documents
	// change", which is a different question: the fast freshness path never
	// computes it, and answers "is anything stale" from recorded file mtimes
	// alone. Toggling a source class moves no file, so a project that enabled
	// drafter reports kept serving an index built without them until somebody
	// happened to edit a document.
	//
	// Only the fields that change what gets indexed belong here. Budgets and
	// telemetry change what a served response carries, not what the index
	// contains, and folding them in would rebuild the whole corpus to answer a
	// question the index has no part in.
	SettingsFingerprint string          `json:"settingsFingerprint,omitempty"`
	ModelFingerprint    string          `json:"modelFingerprint"`
	Project             string          `json:"project"`
	Worktree            string          `json:"worktree"`
	GitRevision         string          `json:"gitRevision"`
	DirtyFingerprint    string          `json:"dirtyFingerprint"`
	ParserVersion       string          `json:"parserVersion"`
	ChunkerVersion      string          `json:"chunkerVersion"`
	CreatedAt           string          `json:"createdAt"`
	Runtime             RuntimeIdentity `json:"runtime"`
	DocumentCount       int             `json:"documentCount"`
	ChunkCount          int             `json:"chunkCount"`
	SourceStates        []SourceState   `json:"sourceStates"`
	DirectoryStates     []SourceState   `json:"directoryStates"`
	ServingStale        bool            `json:"servingStale,omitempty"`
	WriterContention    bool            `json:"writerContention,omitempty"`
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
	ByteRange bool   `json:"byteRange,omitempty"`
	StartByte int    `json:"startByte,omitempty"`
	EndByte   int    `json:"endByte,omitempty"`
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
	// Score and LaneRanks are ranking diagnostics. Nothing outside the
	// retriever reads them, and no caller can act on them, so compact responses
	// drop them. A packed result always carries a positive fusion score, so
	// omitempty never hides a real value in a verbose response.
	Score     int64          `json:"score,omitempty"`
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
	ContextTokens     int                `json:"contextTokens,omitempty"`
	Results           []SearchResult     `json:"results"`
	TierDisagreements []TierDisagreement `json:"tierDisagreements,omitempty"`
	Omitted           int                `json:"omitted"`
	OmittedByReason   map[string]int     `json:"omittedByReason"`
	Metadata          RetrievalMetadata  `json:"metadata"`
}

// ContextPackTarget identifies the authority boundary a pack is compiled
// for. Project packs are an internal orientation/measurement primitive and
// can never be materialized. Public run packs bind either one canonical
// active campaign run or one isolated recruiting run.
type ContextPackTarget struct {
	Kind            string `json:"kind"`
	CampaignID      string `json:"campaignId,omitempty"`
	WorkItemID      string `json:"workItemId,omitempty"`
	RunID           string `json:"runId,omitempty"`
	CandidateSlug   string `json:"candidateSlug,omitempty"`
	RecruitingRunID string `json:"recruitingRunId,omitempty"`
}

// ContextPackScope freezes every mutable state identity used to compile a
// delegated pack. A caller cannot reuse a preview after the campaign head,
// work item, or existing run changed without producing a different digest.
type ContextPackScope struct {
	Kind              string `json:"kind"`
	CampaignID        string `json:"campaignId,omitempty"`
	CampaignSlug      string `json:"campaignSlug,omitempty"`
	CampaignRevision  int64  `json:"campaignRevision,omitempty"`
	WorkItemID        string `json:"workItemId,omitempty"`
	WorkItemRevision  int64  `json:"workItemRevision,omitempty"`
	RunID             string `json:"runId,omitempty"`
	RunRevision       int64  `json:"runRevision,omitempty"`
	CandidateSlug     string `json:"candidateSlug,omitempty"`
	RecruitingRunID   string `json:"recruitingRunId,omitempty"`
	StateHeadRevision int64  `json:"stateHeadRevision"`
	StateHeadDigest   string `json:"stateHeadDigest"`
	EventID           string `json:"eventId,omitempty"`
}

// ContextConstraint is accepted input, not retrieved prose. Each constraint
// remains independently attributable to an exact canonical record handle.
type ContextConstraint struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Text         string `json:"text"`
	SourceHandle string `json:"sourceHandle"`
}

// ContextPack is the immutable 0.8 dispatch artifact. It deliberately carries
// bounded cards and exact expansion handles instead of arbitrary passages.
// This keeps state/epistemic labels visible and prevents an investigator from
// silently inheriting the campaign's full corpus.
type ContextPack struct {
	SchemaVersion       int                       `json:"schemaVersion"`
	PackID              string                    `json:"packId"`
	Digest              string                    `json:"digest"`
	Task                string                    `json:"task"`
	Scope               ContextPackScope          `json:"scope"`
	Generation          ContextGenerationIdentity `json:"generation"`
	Role                string                    `json:"role"`
	WriteGrants         []WriteGrant              `json:"writeGrants,omitempty"`
	AllowedTiers        []string                  `json:"allowedTiers"`
	RequestedProfile    string                    `json:"requestedProfile"`
	EffectiveProfile    string                    `json:"effectiveProfile"`
	ActiveLanes         []string                  `json:"activeLanes"`
	Models              []string                  `json:"models"`
	FallbackReason      *string                   `json:"fallbackReason"`
	TokenBudget         int                       `json:"tokenBudget"`
	EstimatedTokens     int                       `json:"estimatedTokens"`
	AcceptedConstraints []ContextConstraint       `json:"acceptedConstraints"`
	Cards               []ContextCard             `json:"cards"`
	RequiredHandles     []string                  `json:"requiredHandles"`
	Omitted             map[string]int            `json:"omitted"`
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
	VocabularyPolicy     string         `json:"vocabularyPolicy,omitempty"`
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
