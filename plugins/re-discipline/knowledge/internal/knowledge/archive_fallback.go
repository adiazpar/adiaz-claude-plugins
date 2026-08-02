package knowledge

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const ArchiveFallbackReceiptVersion = 1

type ArchiveFallbackBinding struct {
	CorpusFingerprint  string `json:"corpusFingerprint"`
	ProfileIdentity    string `json:"profileIdentity"`
	RuntimeFingerprint string `json:"runtimeFingerprint"`
	FindingFormat      string `json:"findingFormat"`
	IdentifierAnalyzer string `json:"identifierAnalyzer"`
}

type NormalizedBeatsRawReceipt struct {
	SchemaVersion          int                    `json:"schemaVersion"`
	ID                     string                 `json:"id"`
	Status                 string                 `json:"status"`
	RatifiedAt             string                 `json:"ratifiedAt"`
	RatifiedBy             string                 `json:"ratifiedBy"`
	EvaluatedAt            string                 `json:"evaluatedAt"`
	SuiteID                string                 `json:"suiteId"`
	SuiteDigest            string                 `json:"suiteDigest"`
	Binding                ArchiveFallbackBinding `json:"binding"`
	CaseCount              int                    `json:"caseCount"`
	DevelopmentCases       int                    `json:"developmentCases"`
	DevelopmentManager     int                    `json:"developmentManagerCases"`
	DevelopmentDrafter     int                    `json:"developmentDrafterCases"`
	DevelopmentAbstention  int                    `json:"developmentAbstentionCases"`
	HoldoutCases           int                    `json:"holdoutCases"`
	HoldoutManager         int                    `json:"holdoutManagerCases"`
	HoldoutDrafter         int                    `json:"holdoutDrafterCases"`
	HoldoutAbstention      int                    `json:"holdoutAbstentionCases"`
	NormalizedRecall       float64                `json:"normalizedRecall"`
	RawRecall              float64                `json:"rawRecall"`
	AbstentionAccuracy     float64                `json:"abstentionAccuracy"`
	FindingHandleAccuracy  float64                `json:"findingHandleAccuracy"`
	EvidenceHandleAccuracy float64                `json:"evidenceHandleAccuracy"`
	SourceClassAccuracy    float64                `json:"sourceClassAccuracy"`
	ReviewStateAccuracy    float64                `json:"reviewStateAccuracy"`
	ValidityAccuracy       float64                `json:"validityAccuracy"`
	VocabularyDisjointRate float64                `json:"vocabularyDisjointRate"`
	DurabilityAccuracy     float64                `json:"durabilityLabelAccuracy"`
	HardNegativeHits       int                    `json:"hardNegativeHits"`
	ReplayRate             float64                `json:"deterministicReplayRate"`
	NormalizedMedianTokens int                    `json:"normalizedMedianTokens"`
	RawMedianTokens        int                    `json:"rawMedianTokens"`
	Passed                 bool                   `json:"passed"`
	Digest                 string                 `json:"digest"`
}

type ArchiveFallbackPolicy struct {
	// default-fallback keeps raw reports beneath normalized findings. opt-in is
	// accepted only with a valid receipt and serves raw reports on explicit
	// request instead.
	Mode    string
	Receipt *NormalizedBeatsRawReceipt
}

func ArchiveReceiptDigest(receipt NormalizedBeatsRawReceipt) (string, error) {
	receipt.Digest = ""
	return CanonicalDigest(receipt)
}

func ValidateArchiveReceiptPath(relative string) error {
	if err := validateEvalPath(relative); err != nil {
		return err
	}
	if !strings.HasPrefix(relative, ".re-discipline/knowledge/receipts/") ||
		!strings.HasSuffix(strings.ToLower(relative), ".json") {
		return errors.New("archive receipt must be a JSON file below .re-discipline/knowledge/receipts")
	}
	return nil
}

func LoadNormalizedBeatsRawReceipt(
	boundary Boundary,
	relative string,
) (NormalizedBeatsRawReceipt, error) {
	if err := ValidateArchiveReceiptPath(relative); err != nil {
		return NormalizedBeatsRawReceipt{}, err
	}
	body, err := readProjectControlFile(boundary, relative)
	if err != nil {
		return NormalizedBeatsRawReceipt{}, err
	}
	var receipt NormalizedBeatsRawReceipt
	if err := decodeStrict(body, &receipt); err != nil {
		return NormalizedBeatsRawReceipt{}, err
	}
	// Validate the receipt's intrinsic measurement and signature contract here.
	// The query path validates it again against the active generation binding.
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{
		Mode: "opt-in", Receipt: &receipt,
	}, receipt.Binding); err != nil {
		return NormalizedBeatsRawReceipt{}, err
	}
	return receipt, nil
}

func ValidateArchiveFallbackPolicy(policy ArchiveFallbackPolicy, binding ArchiveFallbackBinding) error {
	switch policy.Mode {
	case "", "default-fallback":
		return nil
	case "opt-in":
		if policy.Receipt == nil {
			return errors.New("archive opt-in requires a normalized-beats-raw receipt")
		}
	default:
		return fmt.Errorf("unsupported archive fallback mode %q", policy.Mode)
	}
	receipt := *policy.Receipt
	if receipt.SchemaVersion != ArchiveFallbackReceiptVersion ||
		!managedSlugRE.MatchString(receipt.ID) || receipt.Status != "ratified" ||
		strings.TrimSpace(receipt.RatifiedBy) == "" || !receipt.Passed {
		return errors.New("archive receipt is not a ratified passed evaluation")
	}
	evaluatedAt, err := time.Parse(time.RFC3339Nano, receipt.EvaluatedAt)
	if err != nil || evaluatedAt.Location() != time.UTC {
		return errors.New("archive receipt evaluatedAt is invalid")
	}
	ratifiedAt, err := time.Parse(time.RFC3339Nano, receipt.RatifiedAt)
	if err != nil || ratifiedAt.Location() != time.UTC || ratifiedAt.Before(evaluatedAt) {
		return errors.New("archive receipt ratifiedAt is invalid")
	}
	if !managedSlugRE.MatchString(receipt.SuiteID) ||
		!sha256ValueRE.MatchString(receipt.SuiteDigest) {
		return errors.New("archive receipt is not bound to a ratified evaluation suite")
	}
	if receipt.DevelopmentCases < 24 || receipt.HoldoutCases < 24 ||
		receipt.CaseCount != receipt.DevelopmentCases+receipt.HoldoutCases ||
		receipt.DevelopmentManager < 6 || receipt.DevelopmentDrafter < 6 ||
		receipt.HoldoutManager < 6 || receipt.HoldoutDrafter < 6 ||
		receipt.DevelopmentManager+receipt.DevelopmentDrafter != receipt.DevelopmentCases ||
		receipt.HoldoutManager+receipt.HoldoutDrafter != receipt.HoldoutCases ||
		receipt.DevelopmentAbstention < 4 || receipt.HoldoutAbstention < 4 {
		return errors.New("archive receipt evaluation coverage is insufficient")
	}
	if !sha256ValueRE.MatchString(receipt.Binding.CorpusFingerprint) ||
		!sha256ValueRE.MatchString(receipt.Binding.RuntimeFingerprint) ||
		strings.TrimSpace(receipt.Binding.ProfileIdentity) == "" ||
		strings.TrimSpace(receipt.Binding.FindingFormat) == "" ||
		strings.TrimSpace(receipt.Binding.IdentifierAnalyzer) == "" {
		return errors.New("archive receipt binding is incomplete")
	}
	if receipt.Binding != binding {
		return errors.New("archive receipt does not bind the active representation")
	}
	if receipt.NormalizedRecall < 0 || receipt.NormalizedRecall > 1 ||
		receipt.RawRecall < 0 || receipt.RawRecall > 1 ||
		receipt.NormalizedMedianTokens < 1 || receipt.RawMedianTokens < 1 ||
		receipt.NormalizedRecall < receipt.RawRecall ||
		receipt.NormalizedMedianTokens >= receipt.RawMedianTokens {
		return errors.New("archive receipt does not establish non-inferior recall and lower token cost")
	}
	if receipt.AbstentionAccuracy != 1 || receipt.FindingHandleAccuracy != 1 ||
		receipt.EvidenceHandleAccuracy != 1 || receipt.SourceClassAccuracy != 1 ||
		receipt.ReviewStateAccuracy != 1 || receipt.ValidityAccuracy != 1 ||
		receipt.VocabularyDisjointRate != 1 || receipt.DurabilityAccuracy != 1 ||
		receipt.HardNegativeHits != 0 || receipt.ReplayRate != 1 {
		return errors.New("archive receipt does not establish exact handles, states, safety, and replay")
	}
	digest, err := ArchiveReceiptDigest(receipt)
	if err != nil {
		return err
	}
	if receipt.Digest != digest {
		return errors.New("archive receipt digest mismatch")
	}
	return nil
}

func (policy ArchiveFallbackPolicy) RawIsDefault(binding ArchiveFallbackBinding) bool {
	return ValidateArchiveFallbackPolicy(policy, binding) != nil || policy.Mode != "opt-in"
}

type ArchiveServeEvent struct {
	ReportDigest           string `json:"reportDigest"`
	ServeCount             int    `json:"serveCount"`
	NormalizationSuggested bool   `json:"normalizationSuggested"`
	RepeatedRequestIgnored bool   `json:"repeatedRequestIgnored,omitempty"`
}

const archiveFallbackTrackerStateVersion = 1

// NormalizationSuggestion is durable operational work, never epistemic truth.
// The queue records demand for curator review without changing retrieval rank
// or promoting a report automatically.
type NormalizationSuggestion struct {
	ReportDigest     string `json:"reportDigest"`
	ServeCount       int    `json:"serveCount"`
	Status           string `json:"status"`
	FirstSuggestedAt string `json:"firstSuggestedAt"`
	LastObservedAt   string `json:"lastObservedAt"`
}

type archiveFallbackTrackerState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Threshold     int                       `json:"threshold"`
	Counts        map[string]int            `json:"counts"`
	RequestKeys   []string                  `json:"requestKeys"`
	Suggestions   []NormalizationSuggestion `json:"suggestions"`
	Digest        string                    `json:"digest"`
}

// ArchiveFallbackTracker is operational cache state, not epistemic state.
// Counts never influence ranking; they only queue a demand-driven
// normalization suggestion at the threshold.
type ArchiveFallbackTracker struct {
	mu          sync.Mutex
	threshold   int
	path        string
	counts      map[string]int
	requests    map[string]bool
	suggestions map[string]NormalizationSuggestion
}

func NewArchiveFallbackTracker(threshold int) (*ArchiveFallbackTracker, error) {
	if threshold < 1 {
		return nil, errors.New("archive serve threshold must be positive")
	}
	return &ArchiveFallbackTracker{
		threshold: threshold, counts: map[string]int{}, requests: map[string]bool{},
		suggestions: map[string]NormalizationSuggestion{},
	}, nil
}

// OpenArchiveFallbackTracker loads the durable serve ledger and normalization
// queue. A missing file is an empty ledger; malformed or tampered state fails
// closed instead of silently discarding demand signals.
func OpenArchiveFallbackTracker(threshold int, path string) (*ArchiveFallbackTracker, error) {
	tracker, err := NewArchiveFallbackTracker(threshold)
	if err != nil {
		return nil, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("archive fallback state path is required")
	}
	tracker.path = path
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tracker, nil
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		info.Size() < 1 || info.Size() > maxSourceBytes {
		return nil, errors.New("archive fallback state has unsafe type or size")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state archiveFallbackTrackerState
	if err := decodeStrict(body, &state); err != nil {
		return nil, fmt.Errorf("decode archive fallback state: %w", err)
	}
	if err := validateArchiveFallbackTrackerState(state); err != nil {
		return nil, err
	}
	want := state.Digest
	state.Digest = ""
	digest, err := CanonicalDigest(state)
	if err != nil || digest != want {
		return nil, errors.New("archive fallback state digest mismatch")
	}
	tracker.counts = cloneIntMap(state.Counts)
	for _, key := range state.RequestKeys {
		tracker.requests[key] = true
	}
	for _, suggestion := range state.Suggestions {
		tracker.suggestions[suggestion.ReportDigest] = suggestion
	}
	return tracker, nil
}

func validateArchiveFallbackTrackerState(state archiveFallbackTrackerState) error {
	if state.SchemaVersion != archiveFallbackTrackerStateVersion || state.Threshold < 1 ||
		state.Counts == nil || !sha256ValueRE.MatchString(state.Digest) {
		return errors.New("archive fallback state identity is invalid")
	}
	seenRequests, seenSuggestions := map[string]bool{}, map[string]bool{}
	for digest, count := range state.Counts {
		if _, err := normalizeArchiveDigest(digest); err != nil || count < 1 {
			return errors.New("archive fallback state count is invalid")
		}
	}
	for _, key := range state.RequestKeys {
		if !sha256ValueRE.MatchString(key) || seenRequests[key] {
			return errors.New("archive fallback state request key is invalid or repeated")
		}
		seenRequests[key] = true
	}
	for _, suggestion := range state.Suggestions {
		if _, err := normalizeArchiveDigest(suggestion.ReportDigest); err != nil ||
			suggestion.Status != "queued" || suggestion.ServeCount < 1 ||
			seenSuggestions[suggestion.ReportDigest] ||
			validateUTC(suggestion.FirstSuggestedAt) != nil ||
			validateUTC(suggestion.LastObservedAt) != nil {
			return errors.New("archive normalization suggestion is invalid or repeated")
		}
		if count := state.Counts[suggestion.ReportDigest]; count < suggestion.ServeCount {
			return errors.New("archive normalization suggestion exceeds its serve count")
		}
		seenSuggestions[suggestion.ReportDigest] = true
	}
	return nil
}

func normalizeArchiveDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 64 {
		value = "sha256:" + value
	}
	if !sha256ValueRE.MatchString(value) {
		return "", errors.New("archive report digest must be sha256")
	}
	return value, nil
}

func (tracker *ArchiveFallbackTracker) Record(reportDigest, requestID string) (ArchiveServeEvent, error) {
	if tracker == nil {
		return ArchiveServeEvent{}, errors.New("archive fallback tracker is nil")
	}
	digest, err := normalizeArchiveDigest(reportDigest)
	if err != nil {
		return ArchiveServeEvent{}, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ArchiveServeEvent{}, errors.New("archive serve request id is required")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	requestKey := "sha256:" + SHA256String(requestID+"\x00"+digest)
	if tracker.requests[requestKey] {
		count := tracker.counts[digest]
		return ArchiveServeEvent{
			ReportDigest: digest, ServeCount: count,
			NormalizationSuggested: count >= tracker.threshold, RepeatedRequestIgnored: true,
		}, nil
	}
	tracker.requests[requestKey] = true
	tracker.counts[digest]++
	count := tracker.counts[digest]
	previousSuggestion, hadSuggestion := tracker.suggestions[digest]
	now := RFC3339UTC(time.Now())
	if count >= tracker.threshold {
		suggestion := previousSuggestion
		if !hadSuggestion {
			suggestion = NormalizationSuggestion{
				ReportDigest: digest, Status: "queued", FirstSuggestedAt: now,
			}
		}
		suggestion.ServeCount = count
		suggestion.LastObservedAt = now
		tracker.suggestions[digest] = suggestion
	}
	if err := tracker.persistLocked(); err != nil {
		delete(tracker.requests, requestKey)
		if count == 1 {
			delete(tracker.counts, digest)
		} else {
			tracker.counts[digest] = count - 1
		}
		if hadSuggestion {
			tracker.suggestions[digest] = previousSuggestion
		} else {
			delete(tracker.suggestions, digest)
		}
		return ArchiveServeEvent{}, err
	}
	return ArchiveServeEvent{
		ReportDigest: digest, ServeCount: count, NormalizationSuggested: count >= tracker.threshold,
	}, nil
}

func (tracker *ArchiveFallbackTracker) persistLocked() error {
	if tracker.path == "" {
		return nil
	}
	state := archiveFallbackTrackerState{
		SchemaVersion: archiveFallbackTrackerStateVersion, Threshold: tracker.threshold,
		Counts: cloneIntMap(tracker.counts), RequestKeys: make([]string, 0, len(tracker.requests)),
		Suggestions: make([]NormalizationSuggestion, 0, len(tracker.suggestions)),
	}
	for key := range tracker.requests {
		state.RequestKeys = append(state.RequestKeys, key)
	}
	for _, suggestion := range tracker.suggestions {
		state.Suggestions = append(state.Suggestions, suggestion)
	}
	sort.Strings(state.RequestKeys)
	sort.Slice(state.Suggestions, func(i, j int) bool {
		return state.Suggestions[i].ReportDigest < state.Suggestions[j].ReportDigest
	})
	digest, err := CanonicalDigest(state)
	if err != nil {
		return err
	}
	state.Digest = digest
	return AtomicWriteJSON(tracker.path, state, 0o600)
}

func cloneIntMap(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (tracker *ArchiveFallbackTracker) Snapshot() map[string]int {
	if tracker == nil {
		return nil
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	result := make(map[string]int, len(tracker.counts))
	for digest, count := range tracker.counts {
		result[digest] = count
	}
	return result
}

func (tracker *ArchiveFallbackTracker) Suggestions() []NormalizationSuggestion {
	if tracker == nil {
		return nil
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	result := make([]NormalizationSuggestion, 0, len(tracker.suggestions))
	for _, suggestion := range tracker.suggestions {
		result = append(result, suggestion)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ReportDigest < result[j].ReportDigest
	})
	return result
}
