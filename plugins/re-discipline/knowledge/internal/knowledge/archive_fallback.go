package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ArchiveFallbackReceiptVersion = 2

type ArchiveFallbackBinding struct {
	CorpusFingerprint  string `json:"corpusFingerprint"`
	ProfileIdentity    string `json:"profileIdentity"`
	RuntimeFingerprint string `json:"runtimeFingerprint"`
	FindingFormat      string `json:"findingFormat"`
	IdentifierAnalyzer string `json:"identifierAnalyzer"`
}

type NormalizedBeatsRawReceipt struct {
	SchemaVersion           int                       `json:"schemaVersion"`
	ID                      string                    `json:"id"`
	Status                  string                    `json:"status"`
	RatifiedAt              string                    `json:"ratifiedAt"`
	RatifiedBy              string                    `json:"ratifiedBy"`
	DecisionCorrelationID   string                    `json:"decisionCorrelationId"`
	DecisionIdempotencyKey  string                    `json:"decisionIdempotencyKey"`
	EvaluatedAt             string                    `json:"evaluatedAt"`
	SuiteID                 string                    `json:"suiteId"`
	SuiteDigest             string                    `json:"suiteDigest"`
	ReportRunID             string                    `json:"reportRunId"`
	ReportPath              string                    `json:"reportPath"`
	ReportDigest            string                    `json:"reportDigest"`
	ReportContentDigest     string                    `json:"reportContentDigest"`
	PairedEvaluationDigest  string                    `json:"pairedEvaluationDigest"`
	QuestionsDigest         string                    `json:"questionsDigest"`
	ContractDigest          string                    `json:"contractDigest"`
	Generation              ContextGenerationIdentity `json:"generation"`
	Binding                 ArchiveFallbackBinding    `json:"binding"`
	PreviousSettingsDigest  string                    `json:"previousSettingsDigest"`
	ResultingSettingsDigest string                    `json:"resultingSettingsDigest"`
	ResultingSettings       KnowledgeSettings         `json:"resultingSettings"`
	CaseCount               int                       `json:"caseCount"`
	DevelopmentCases        int                       `json:"developmentCases"`
	DevelopmentManager      int                       `json:"developmentManagerCases"`
	DevelopmentDrafter      int                       `json:"developmentDrafterCases"`
	DevelopmentAbstention   int                       `json:"developmentAbstentionCases"`
	HoldoutCases            int                       `json:"holdoutCases"`
	HoldoutManager          int                       `json:"holdoutManagerCases"`
	HoldoutDrafter          int                       `json:"holdoutDrafterCases"`
	HoldoutAbstention       int                       `json:"holdoutAbstentionCases"`
	NormalizedRecall        float64                   `json:"normalizedRecall"`
	RawRecall               float64                   `json:"rawRecall"`
	AbstentionAccuracy      float64                   `json:"abstentionAccuracy"`
	FindingHandleAccuracy   float64                   `json:"findingHandleAccuracy"`
	EvidenceHandleAccuracy  float64                   `json:"evidenceHandleAccuracy"`
	SourceClassAccuracy     float64                   `json:"sourceClassAccuracy"`
	ReviewStateAccuracy     float64                   `json:"reviewStateAccuracy"`
	ValidityAccuracy        float64                   `json:"validityAccuracy"`
	VocabularyDisjointRate  float64                   `json:"vocabularyDisjointRate"`
	DurabilityAccuracy      float64                   `json:"durabilityLabelAccuracy"`
	HardNegativeHits        int                       `json:"hardNegativeHits"`
	ReplayRate              float64                   `json:"deterministicReplayRate"`
	NormalizedMedianTokens  int                       `json:"normalizedMedianTokens"`
	RawMedianTokens         int                       `json:"rawMedianTokens"`
	Passed                  bool                      `json:"passed"`
	Digest                  string                    `json:"digest"`
}

type ArchiveFallbackPolicy struct {
	// default-fallback keeps raw reports beneath normalized findings. opt-in is
	// accepted only with a valid receipt and serves raw reports on explicit
	// request instead.
	Mode    string
	Receipt *NormalizedBeatsRawReceipt
	Report  *NormalizedRawGateReport
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

func ValidateNormalizedRawMeasurementPath(relative, runID string) error {
	if err := validateEvalPath(relative); err != nil {
		return err
	}
	if !validNormalizedRawRunID(runID) {
		return errors.New("normalized-vs-raw report run id is invalid")
	}
	want := ".re-discipline/knowledge/measurements/normalized-vs-raw/" + runID + "/report.json"
	if relative != want {
		return errors.New("normalized-vs-raw report path is not derived from its run id")
	}
	return nil
}

func LoadNormalizedBeatsRawReceipt(
	boundary Boundary,
	relative string,
) (NormalizedBeatsRawReceipt, NormalizedRawGateReport, error) {
	if err := ValidateArchiveReceiptPath(relative); err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	body, err := readProjectControlFile(boundary, relative)
	if err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	var receipt NormalizedBeatsRawReceipt
	if err := decodeStrict(body, &receipt); err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	if err := validateArchiveReceiptIntrinsic(receipt, relative); err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	reportBody, err := readProjectControlFile(boundary, receipt.ReportPath)
	if err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{},
			fmt.Errorf("read receipt-bound normalized-vs-raw report: %w", err)
	}
	if digest := "sha256:" + SHA256Bytes(reportBody); digest != receipt.ReportContentDigest {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{},
			errors.New("receipt-bound normalized-vs-raw report content digest mismatch")
	}
	var report NormalizedRawGateReport
	if err := decodeStrict(reportBody, &report); err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	if err := validateArchiveReceiptReportIdentity(receipt, report); err != nil {
		return NormalizedBeatsRawReceipt{}, NormalizedRawGateReport{}, err
	}
	return receipt, report, nil
}

func ValidateArchiveFallbackPolicy(policy ArchiveFallbackPolicy, binding ArchiveFallbackBinding) error {
	switch policy.Mode {
	case "", "default-fallback":
		return nil
	case "opt-in":
		if policy.Receipt == nil || policy.Report == nil {
			return errors.New("archive opt-in requires a normalized-beats-raw receipt and its exact report")
		}
	default:
		return fmt.Errorf("unsupported archive fallback mode %q", policy.Mode)
	}
	receipt := *policy.Receipt
	report := *policy.Report
	if err := validateArchiveReceiptIntrinsic(
		receipt, receipt.ResultingSettings.Archive.NormalizedBeatsRawReceipt); err != nil {
		return err
	}
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
	if err := validateArchiveReceiptReportIdentity(receipt, report); err != nil {
		return err
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
	SuggestionID           string `json:"suggestionId,omitempty"`
	ServeCount             int    `json:"serveCount"`
	NormalizationSuggested bool   `json:"normalizationSuggested"`
	RepeatedRequestIgnored bool   `json:"repeatedRequestIgnored,omitempty"`
}

const archiveFallbackTrackerStateVersion = 3

const (
	normalizationTriggerRetrieval = "retrieval-threshold"
	normalizationTriggerManager   = "manager-request"
	normalizationTriggerClosure   = "closure"
)

type NormalizationSource struct {
	CampaignID   string `json:"campaignId"`
	CampaignSlug string `json:"campaignSlug"`
	RunID        string `json:"runId"`
	ReportPath   string `json:"reportPath"`
	ReportHandle string `json:"reportHandle"`
	SourceHandle string `json:"sourceHandle"`
}

// NormalizationResolution is a sealed operational receipt. It does not make
// any epistemic decision: it proves that the exact queued report was covered
// by a canonical curator run, a gap-free reviewed intake, and the immutable
// manager review that disposed every candidate (or explicitly found no claim).
type NormalizationResolution struct {
	SchemaVersion      int        `json:"schemaVersion"`
	Disposition        string     `json:"disposition"`
	SourceReport       FileHandle `json:"sourceReport"`
	CuratorRunID       string     `json:"curatorRunId"`
	CuratorRunDigest   string     `json:"curatorRunDigest"`
	CuratorReport      FileHandle `json:"curatorReport"`
	IntakeID           string     `json:"intakeId"`
	IntakeRevision     int64      `json:"intakeRevision"`
	IntakeDigest       string     `json:"intakeDigest"`
	CoverageDigest     string     `json:"coverageDigest"`
	ReviewID           string     `json:"reviewId"`
	ReviewRevision     int64      `json:"reviewRevision"`
	ReviewDigest       string     `json:"reviewDigest"`
	ResolvedFindingIDs []string   `json:"resolvedFindingIds"`
	Digest             string     `json:"digest"`
}

// NormalizationSuggestion is durable operational work, never epistemic truth.
// The queue records demand for curator review without changing retrieval rank
// or promoting a report automatically.
type NormalizationSuggestion struct {
	ID               string                   `json:"id"`
	Revision         int64                    `json:"revision"`
	SourceKey        string                   `json:"sourceKey"`
	ReportDigest     string                   `json:"reportDigest"`
	CampaignID       string                   `json:"campaignId"`
	CampaignSlug     string                   `json:"campaignSlug"`
	RunID            string                   `json:"runId"`
	ReportPath       string                   `json:"reportPath"`
	ReportHandle     string                   `json:"reportHandle"`
	SourceHandle     string                   `json:"sourceHandle"`
	ServeCount       int                      `json:"serveCount"`
	Triggers         []string                 `json:"triggers"`
	Status           string                   `json:"status"`
	FirstSuggestedAt string                   `json:"firstSuggestedAt"`
	LastObservedAt   string                   `json:"lastObservedAt"`
	ClaimedBy        string                   `json:"claimedBy,omitempty"`
	ClaimedAt        string                   `json:"claimedAt,omitempty"`
	AcknowledgedBy   string                   `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt   string                   `json:"acknowledgedAt,omitempty"`
	ResolvedBy       string                   `json:"resolvedBy,omitempty"`
	ResolvedAt       string                   `json:"resolvedAt,omitempty"`
	Resolution       *NormalizationResolution `json:"resolution,omitempty"`
	Digest           string                   `json:"digest"`
}

type NormalizationQueueStatus struct {
	Queued       int                       `json:"queued"`
	Claimed      int                       `json:"claimed"`
	Acknowledged int                       `json:"acknowledged"`
	Resolved     int                       `json:"resolved"`
	Items        []NormalizationSuggestion `json:"items"`
	Omitted      int                       `json:"omitted"`
}

type archiveFallbackTrackerState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Threshold     int                       `json:"threshold"`
	Counts        map[string]int            `json:"counts"`
	RequestKeys   []string                  `json:"requestKeys"`
	Suggestions   []NormalizationSuggestion `json:"suggestions"`
	Digest        string                    `json:"digest"`
}

// ArchiveFallbackTracker is canonical operational state, not epistemic truth.
// It deliberately lives outside the deletable retrieval cache. Counts never
// influence ranking; they only queue demand-driven curator work.
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
	body, err := readSingleLinkRegularFile(path)
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
	if state.Threshold != threshold {
		return nil, errors.New("archive fallback state threshold differs from project configuration")
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
		tracker.suggestions[suggestion.ID] = suggestion
	}
	return tracker, nil
}

func validateArchiveFallbackTrackerState(state archiveFallbackTrackerState) error {
	if state.SchemaVersion != archiveFallbackTrackerStateVersion || state.Threshold < 1 ||
		state.Counts == nil || !sha256ValueRE.MatchString(state.Digest) {
		return errors.New("archive fallback state identity is invalid")
	}
	seenRequests, seenSuggestions := map[string]bool{}, map[string]bool{}
	for sourceKey, count := range state.Counts {
		if !sha256ValueRE.MatchString(sourceKey) || count < 1 {
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
		if err := validateNormalizationSuggestion(suggestion); err != nil ||
			seenSuggestions[suggestion.ID] {
			return errors.New("archive normalization suggestion is invalid or repeated")
		}
		if count := state.Counts[suggestion.SourceKey]; count < suggestion.ServeCount {
			return errors.New("archive normalization suggestion exceeds its serve count")
		}
		seenSuggestions[suggestion.ID] = true
	}
	return nil
}

func validateNormalizationSource(source NormalizationSource) error {
	if !campaignIDRE.MatchString(source.CampaignID) ||
		!managedSlugRE.MatchString(source.CampaignSlug) ||
		!runIDRE.MatchString(source.RunID) || validateRelativeRecordPath(source.ReportPath) != nil ||
		!strings.HasPrefix(source.ReportHandle, "run:") ||
		strings.TrimPrefix(source.ReportHandle, "run:") != source.RunID ||
		source.SourceHandle != "path:"+source.ReportPath {
		return errors.New("normalization source requires canonical campaign, run, report, and source handles")
	}
	return nil
}

func normalizationSourceKey(digest string, source NormalizationSource) string {
	return "sha256:" + SHA256String(digest+"\x00"+source.CampaignID+"\x00"+source.RunID+"\x00"+source.ReportPath)
}

func normalizationSuggestionDigest(suggestion NormalizationSuggestion) (string, error) {
	suggestion.Digest = ""
	return CanonicalDigest(suggestion)
}

func normalizationResolutionDigest(resolution NormalizationResolution) (string, error) {
	resolution.Digest = ""
	resolution.ResolvedFindingIDs = SortedUnique(resolution.ResolvedFindingIDs)
	return CanonicalDigest(resolution)
}

func sealNormalizationResolution(resolution *NormalizationResolution) error {
	if resolution == nil {
		return errors.New("normalization resolution receipt is required")
	}
	resolution.ResolvedFindingIDs = SortedUnique(resolution.ResolvedFindingIDs)
	resolution.Digest = ""
	digest, err := normalizationResolutionDigest(*resolution)
	if err != nil {
		return err
	}
	resolution.Digest = digest
	return validateNormalizationResolution(*resolution)
}

func validateNormalizationResolution(resolution NormalizationResolution) error {
	if resolution.SchemaVersion != CampaignSchemaVersion ||
		!validOne(resolution.Disposition, "normalized", "reviewed-non-claim") ||
		validateFileHandle(resolution.SourceReport) != nil ||
		!runIDRE.MatchString(resolution.CuratorRunID) ||
		!digestRE.MatchString(resolution.CuratorRunDigest) ||
		validateFileHandle(resolution.CuratorReport) != nil ||
		!intakeIDRE.MatchString(resolution.IntakeID) || resolution.IntakeRevision < 2 ||
		!digestRE.MatchString(resolution.IntakeDigest) ||
		!digestRE.MatchString(resolution.CoverageDigest) ||
		!reviewIDRE.MatchString(resolution.ReviewID) || resolution.ReviewRevision < 1 ||
		!digestRE.MatchString(resolution.ReviewDigest) ||
		!digestRE.MatchString(resolution.Digest) {
		return errors.New("normalization resolution identity or proof binding is invalid")
	}
	if resolution.Disposition == "reviewed-non-claim" && len(resolution.ResolvedFindingIDs) != 0 {
		return errors.New("reviewed non-claim resolution cannot name findings")
	}
	if resolution.Disposition == "normalized" && len(resolution.ResolvedFindingIDs) == 0 {
		return errors.New("normalized resolution requires at least one reviewed or duplicate finding")
	}
	if err := validateIDList("normalization resolved findings", resolution.ResolvedFindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if len(SortedUnique(resolution.ResolvedFindingIDs)) != len(resolution.ResolvedFindingIDs) {
		return errors.New("normalization resolved findings must be sorted and unique")
	}
	want, err := normalizationResolutionDigest(resolution)
	if err != nil || want != resolution.Digest {
		return errors.New("normalization resolution digest mismatch")
	}
	return nil
}

func sealNormalizationSuggestion(suggestion *NormalizationSuggestion) error {
	if suggestion == nil {
		return errors.New("normalization suggestion is required")
	}
	suggestion.Digest = ""
	digest, err := normalizationSuggestionDigest(*suggestion)
	if err != nil {
		return err
	}
	suggestion.Digest = digest
	return validateNormalizationSuggestion(*suggestion)
}

func validateNormalizationSuggestion(suggestion NormalizationSuggestion) error {
	source := NormalizationSource{
		CampaignID: suggestion.CampaignID, CampaignSlug: suggestion.CampaignSlug,
		RunID: suggestion.RunID, ReportPath: suggestion.ReportPath,
		ReportHandle: suggestion.ReportHandle, SourceHandle: suggestion.SourceHandle,
	}
	if !strings.HasPrefix(suggestion.ID, "normalization-") ||
		len(suggestion.ID) != len("normalization-")+20 || suggestion.Revision < 1 ||
		!sha256ValueRE.MatchString(suggestion.SourceKey) ||
		!sha256ValueRE.MatchString(suggestion.ReportDigest) ||
		validateNormalizationSource(source) != nil || suggestion.ServeCount < 0 ||
		!validOne(suggestion.Status, "queued", "claimed", "acknowledged", "resolved") ||
		validateUTC(suggestion.FirstSuggestedAt) != nil || validateUTC(suggestion.LastObservedAt) != nil ||
		!digestRE.MatchString(suggestion.Digest) {
		return errors.New("normalization suggestion identity, source, status, or digest is invalid")
	}
	if len(suggestion.Triggers) == 0 || len(SortedUnique(suggestion.Triggers)) != len(suggestion.Triggers) {
		return errors.New("normalization suggestion requires sorted unique triggers")
	}
	for _, trigger := range suggestion.Triggers {
		if !validOne(trigger, normalizationTriggerRetrieval, normalizationTriggerManager, normalizationTriggerClosure) {
			return fmt.Errorf("unsupported normalization trigger %q", trigger)
		}
	}
	if containsString(suggestion.Triggers, normalizationTriggerRetrieval) && suggestion.ServeCount < 1 {
		return errors.New("retrieval-triggered normalization requires a positive serve count")
	}
	if suggestion.SourceKey != normalizationSourceKey(suggestion.ReportDigest, source) ||
		suggestion.ID != "normalization-"+strings.TrimPrefix(suggestion.SourceKey, "sha256:")[:20] {
		return errors.New("normalization suggestion source identity does not verify")
	}
	first, _ := time.Parse(time.RFC3339Nano, suggestion.FirstSuggestedAt)
	last, _ := time.Parse(time.RFC3339Nano, suggestion.LastObservedAt)
	if last.Before(first) {
		return errors.New("normalization suggestion observation timestamps are reversed")
	}
	switch suggestion.Status {
	case "queued":
		if suggestion.ClaimedBy != "" || suggestion.ClaimedAt != "" || suggestion.AcknowledgedBy != "" ||
			suggestion.AcknowledgedAt != "" || suggestion.ResolvedBy != "" || suggestion.ResolvedAt != "" ||
			suggestion.Resolution != nil {
			return errors.New("queued normalization suggestion contains later lifecycle fields")
		}
	case "claimed":
		if suggestion.ClaimedBy == "" || validateUTC(suggestion.ClaimedAt) != nil ||
			suggestion.AcknowledgedBy != "" || suggestion.AcknowledgedAt != "" ||
			suggestion.ResolvedBy != "" || suggestion.ResolvedAt != "" || suggestion.Resolution != nil {
			return errors.New("claimed normalization suggestion lifecycle is invalid")
		}
		claimed, _ := time.Parse(time.RFC3339Nano, suggestion.ClaimedAt)
		if claimed.Before(first) {
			return errors.New("normalization suggestion was claimed before it was queued")
		}
	case "acknowledged":
		if suggestion.ClaimedBy == "" || validateUTC(suggestion.ClaimedAt) != nil ||
			suggestion.AcknowledgedBy == "" || validateUTC(suggestion.AcknowledgedAt) != nil ||
			suggestion.ResolvedBy != "" || suggestion.ResolvedAt != "" || suggestion.Resolution != nil {
			return errors.New("acknowledged normalization suggestion lifecycle is invalid")
		}
		claimed, _ := time.Parse(time.RFC3339Nano, suggestion.ClaimedAt)
		acknowledged, _ := time.Parse(time.RFC3339Nano, suggestion.AcknowledgedAt)
		if claimed.Before(first) || acknowledged.Before(claimed) {
			return errors.New("normalization acknowledgment timestamps are out of order")
		}
	case "resolved":
		if suggestion.ClaimedBy == "" || validateUTC(suggestion.ClaimedAt) != nil ||
			suggestion.AcknowledgedBy == "" || validateUTC(suggestion.AcknowledgedAt) != nil ||
			suggestion.ResolvedBy == "" || validateUTC(suggestion.ResolvedAt) != nil ||
			suggestion.Resolution == nil || validateNormalizationResolution(*suggestion.Resolution) != nil ||
			suggestion.Resolution.SourceReport.Path != suggestion.ReportPath ||
			suggestion.Resolution.SourceReport.SHA256 != suggestion.ReportDigest {
			return errors.New("resolved normalization suggestion lifecycle is invalid")
		}
		claimed, _ := time.Parse(time.RFC3339Nano, suggestion.ClaimedAt)
		acknowledged, _ := time.Parse(time.RFC3339Nano, suggestion.AcknowledgedAt)
		resolved, _ := time.Parse(time.RFC3339Nano, suggestion.ResolvedAt)
		if claimed.Before(first) || acknowledged.Before(claimed) || resolved.Before(acknowledged) {
			return errors.New("normalization resolution timestamps are out of order")
		}
	}
	want, err := normalizationSuggestionDigest(suggestion)
	if err != nil || want != suggestion.Digest {
		return errors.New("normalization suggestion digest mismatch")
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
	digest, err := normalizeArchiveDigest(reportDigest)
	if err != nil {
		return ArchiveServeEvent{}, err
	}
	return tracker.RecordSource(digest, requestID, fallbackNormalizationSource(digest))
}

func fallbackNormalizationSource(digest string) NormalizationSource {
	hex := strings.TrimPrefix(digest, "sha256:")
	reportPath := ".re-discipline/knowledge/unresolved/" + hex + "/report.md"
	return NormalizationSource{
		CampaignID: "C-UNRESOLVED", CampaignSlug: "unresolved",
		RunID: "R-00000000-0000", ReportPath: reportPath,
		ReportHandle: "run:R-00000000-0000", SourceHandle: "path:" + reportPath,
	}
}

func normalizationSourceForReport(
	boundary Boundary,
	reportPath, reportDigest string,
) (NormalizationSource, error) {
	digest, err := normalizeArchiveDigest(reportDigest)
	if err != nil {
		return NormalizationSource{}, err
	}
	parts := strings.Split(reportPath, "/")
	if len(parts) == 5 && parts[0] == "active" && parts[2] == "runs" &&
		managedSlugRE.MatchString(parts[1]) && runIDRE.MatchString(parts[3]) && parts[4] == "report.md" {
		store := NewStateStoreWithBoundary(boundary)
		campaignBody, campaignHandle, campaignErr := store.ReadCanonicalRecord("active/" + parts[1] + "/campaign.json")
		runPath := reportPath[:strings.LastIndex(reportPath, "/")+1] + "run.json"
		runBody, runHandle, runErr := store.ReadCanonicalRecord(runPath)
		if campaignErr == nil && runErr == nil {
			var campaign CampaignRecord
			var run RunRecord
			if decodeStrictJSON(campaignBody, &campaign) == nil && decodeStrictJSON(runBody, &run) == nil &&
				campaignHandle.RecordDigest == campaign.Digest && runHandle.RecordDigest == run.Digest &&
				ValidateCampaign(campaign) == nil && ValidateRun(run) == nil && campaign.Slug == parts[1] &&
				run.ID == parts[3] && run.CampaignID == campaign.ID && run.Report != nil &&
				run.Report.Path == reportPath && run.Report.SHA256 == digest {
				return NormalizationSource{
					CampaignID: campaign.ID, CampaignSlug: campaign.Slug, RunID: parts[3],
					ReportPath: reportPath, ReportHandle: "run:" + parts[3],
					SourceHandle: "path:" + reportPath,
				}, nil
			}
		}
	}
	if len(parts) == 7 && parts[0] == "docs" && parts[1] == "history" &&
		parts[2] == "campaigns" && parts[4] == "runs" &&
		managedSlugRE.MatchString(parts[3]) && runIDRE.MatchString(parts[5]) && parts[6] == "report.md" {
		archiveRoot := strings.Join(parts[:4], "/")
		manifestPath := archiveRoot + "/manifest.json"
		campaignPath := archiveRoot + "/finalization/campaign.json"
		manifestAbsolute, manifestResolveErr := boundary.Resolve(manifestPath, true)
		campaignAbsolute, campaignResolveErr := boundary.Resolve(campaignPath, true)
		if manifestResolveErr == nil && campaignResolveErr == nil {
			manifestBody, manifestReadErr := readSingleLinkRegularFile(manifestAbsolute)
			campaignBody, campaignReadErr := readSingleLinkRegularFile(campaignAbsolute)
			var manifest ArchiveManifest
			var campaign CampaignRecord
			relativeReport := strings.Join(parts[4:], "/")
			if manifestReadErr == nil && campaignReadErr == nil &&
				decodeStrictJSON(manifestBody, &manifest) == nil && decodeStrictJSON(campaignBody, &campaign) == nil &&
				ValidateArchiveManifest(manifest) == nil && ValidateCampaign(campaign) == nil &&
				campaign.Status == "closed" && campaign.ID == manifest.CampaignID &&
				manifest.Files[relativeReport] == digest {
				return NormalizationSource{
					CampaignID: manifest.CampaignID, CampaignSlug: campaign.Slug, RunID: parts[5],
					ReportPath: reportPath, ReportHandle: "run:" + parts[5],
					SourceHandle: "path:" + reportPath,
				}, nil
			}
		}
	}
	// Tests and pre-cutover shadow fixtures may not yet carry the canonical
	// campaign record. They still receive explicit unresolved handles; a real
	// initialized 0.8 report always resolves through one of the branches above.
	return fallbackNormalizationSource(digest), nil
}

// QueueSource records an explicit non-retrieval demand signal. The same source
// and trigger are idempotent, while independent triggers are retained so a
// manager can see why the work exists. A resolved item is never reopened.
func (tracker *ArchiveFallbackTracker) QueueSource(
	reportDigest string,
	source NormalizationSource,
	trigger, timestamp string,
) (NormalizationSuggestion, error) {
	if tracker == nil {
		return NormalizationSuggestion{}, errors.New("archive fallback tracker is nil")
	}
	digest, err := normalizeArchiveDigest(reportDigest)
	if err != nil {
		return NormalizationSuggestion{}, err
	}
	if err := validateNormalizationSource(source); err != nil {
		return NormalizationSuggestion{}, err
	}
	if !validOne(trigger, normalizationTriggerManager, normalizationTriggerClosure) {
		return NormalizationSuggestion{}, fmt.Errorf("unsupported explicit normalization trigger %q", trigger)
	}
	if validateUTC(timestamp) != nil {
		return NormalizationSuggestion{}, errors.New("normalization trigger requires a UTC RFC3339 timestamp")
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	persistentLock, err := tracker.acquirePersistentLockLocked()
	if err != nil {
		return NormalizationSuggestion{}, err
	}
	if persistentLock != nil {
		defer persistentLock.Close()
		if err := tracker.reloadLocked(); err != nil {
			return NormalizationSuggestion{}, err
		}
	}
	sourceKey := normalizationSourceKey(digest, source)
	suggestionID := "normalization-" + strings.TrimPrefix(sourceKey, "sha256:")[:20]
	previous, present := tracker.suggestions[suggestionID]
	if present && (previous.Status == "resolved" || containsString(previous.Triggers, trigger)) {
		return previous, nil
	}
	next := previous
	if !present {
		next = NormalizationSuggestion{
			ID: suggestionID, Revision: 1, SourceKey: sourceKey,
			ReportDigest: digest, CampaignID: source.CampaignID,
			CampaignSlug: source.CampaignSlug, RunID: source.RunID,
			ReportPath: source.ReportPath, ReportHandle: source.ReportHandle,
			SourceHandle: source.SourceHandle, ServeCount: tracker.counts[sourceKey],
			Triggers: []string{trigger}, Status: "queued",
			FirstSuggestedAt: timestamp, LastObservedAt: timestamp,
		}
	} else {
		next.Revision++
		next.Triggers = SortedUnique(append(next.Triggers, trigger))
		next.LastObservedAt = laterNormalizationTimestamp(next.LastObservedAt, timestamp)
	}
	if err := sealNormalizationSuggestion(&next); err != nil {
		return NormalizationSuggestion{}, err
	}
	tracker.suggestions[suggestionID] = next
	if err := tracker.persistLocked(); err != nil {
		if present {
			tracker.suggestions[suggestionID] = previous
		} else {
			delete(tracker.suggestions, suggestionID)
		}
		return NormalizationSuggestion{}, err
	}
	return next, nil
}

func laterNormalizationTimestamp(left, right string) string {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil || rightTime.After(leftTime) {
		return right
	}
	return left
}

func (tracker *ArchiveFallbackTracker) RecordSource(
	reportDigest, requestID string,
	source NormalizationSource,
) (ArchiveServeEvent, error) {
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
	if err := validateNormalizationSource(source); err != nil {
		return ArchiveServeEvent{}, err
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	persistentLock, err := tracker.acquirePersistentLockLocked()
	if err != nil {
		return ArchiveServeEvent{}, err
	}
	if persistentLock != nil {
		defer persistentLock.Close()
		if err := tracker.reloadLocked(); err != nil {
			return ArchiveServeEvent{}, err
		}
	}
	sourceKey := normalizationSourceKey(digest, source)
	suggestionID := "normalization-" + strings.TrimPrefix(sourceKey, "sha256:")[:20]
	requestKey := "sha256:" + SHA256String(requestID+"\x00"+sourceKey)
	if tracker.requests[requestKey] {
		count := tracker.counts[sourceKey]
		suggestion, suggested := tracker.suggestions[suggestionID]
		return ArchiveServeEvent{
			ReportDigest: digest, SuggestionID: suggestionID, ServeCount: count,
			NormalizationSuggested: suggested && suggestion.Status != "resolved",
			RepeatedRequestIgnored: true,
		}, nil
	}
	tracker.requests[requestKey] = true
	tracker.counts[sourceKey]++
	count := tracker.counts[sourceKey]
	previousSuggestion, hadSuggestion := tracker.suggestions[suggestionID]
	now := RFC3339UTC(time.Now())
	if count >= tracker.threshold {
		suggestion := previousSuggestion
		if !hadSuggestion {
			suggestion = NormalizationSuggestion{
				ID: suggestionID, Revision: 1, SourceKey: sourceKey,
				ReportDigest: digest, CampaignID: source.CampaignID,
				CampaignSlug: source.CampaignSlug, RunID: source.RunID,
				ReportPath: source.ReportPath, ReportHandle: source.ReportHandle,
				SourceHandle: source.SourceHandle,
				Triggers:     []string{normalizationTriggerRetrieval},
				Status:       "queued", FirstSuggestedAt: now,
			}
		} else {
			suggestion.Revision++
			suggestion.Triggers = SortedUnique(append(
				suggestion.Triggers, normalizationTriggerRetrieval))
		}
		suggestion.ServeCount = count
		suggestion.LastObservedAt = laterNormalizationTimestamp(suggestion.LastObservedAt, now)
		if err := sealNormalizationSuggestion(&suggestion); err != nil {
			return ArchiveServeEvent{}, err
		}
		tracker.suggestions[suggestionID] = suggestion
	}
	if err := tracker.persistLocked(); err != nil {
		delete(tracker.requests, requestKey)
		if count == 1 {
			delete(tracker.counts, sourceKey)
		} else {
			tracker.counts[sourceKey] = count - 1
		}
		if hadSuggestion {
			tracker.suggestions[suggestionID] = previousSuggestion
		} else {
			delete(tracker.suggestions, suggestionID)
		}
		return ArchiveServeEvent{}, err
	}
	suggestion, suggested := tracker.suggestions[suggestionID]
	return ArchiveServeEvent{
		ReportDigest: digest, SuggestionID: suggestionID, ServeCount: count,
		NormalizationSuggested: suggested && suggestion.Status != "resolved",
	}, nil
}

func (tracker *ArchiveFallbackTracker) acquirePersistentLockLocked() (*writerLock, error) {
	if tracker.path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(tracker.path), 0o700); err != nil {
		return nil, err
	}
	return acquireWriterLock(tracker.path + ".lock")
}

func (tracker *ArchiveFallbackTracker) reloadLocked() error {
	if tracker.path == "" {
		return nil
	}
	info, err := os.Lstat(tracker.path)
	if os.IsNotExist(err) {
		tracker.counts = map[string]int{}
		tracker.requests = map[string]bool{}
		tracker.suggestions = map[string]NormalizationSuggestion{}
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		info.Size() < 1 || info.Size() > maxSourceBytes {
		return errors.New("archive fallback state has unsafe type or size")
	}
	body, err := readSingleLinkRegularFile(tracker.path)
	if err != nil {
		return err
	}
	var state archiveFallbackTrackerState
	if err := decodeStrict(body, &state); err != nil {
		return fmt.Errorf("decode archive fallback state: %w", err)
	}
	if err := validateArchiveFallbackTrackerState(state); err != nil {
		return err
	}
	want := state.Digest
	state.Digest = ""
	digest, err := CanonicalDigest(state)
	if err != nil || digest != want || state.Threshold != tracker.threshold {
		return errors.New("archive fallback state digest or threshold mismatch")
	}
	tracker.counts = cloneIntMap(state.Counts)
	tracker.requests = map[string]bool{}
	for _, key := range state.RequestKeys {
		tracker.requests[key] = true
	}
	tracker.suggestions = map[string]NormalizationSuggestion{}
	for _, suggestion := range state.Suggestions {
		tracker.suggestions[suggestion.ID] = suggestion
	}
	return nil
}

func (tracker *ArchiveFallbackTracker) Refresh() error {
	if tracker == nil {
		return errors.New("archive fallback tracker is nil")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	lock, err := tracker.acquirePersistentLockLocked()
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	return tracker.reloadLocked()
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
		return state.Suggestions[i].ID < state.Suggestions[j].ID
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
	result := map[string]int{}
	for _, suggestion := range tracker.suggestions {
		result[suggestion.ReportDigest] += suggestion.ServeCount
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
		return result[i].ID < result[j].ID
	})
	return result
}

func (tracker *ArchiveFallbackTracker) QueueStatus(limit int) NormalizationQueueStatus {
	all := tracker.Suggestions()
	items := make([]NormalizationSuggestion, 0, len(all))
	status := NormalizationQueueStatus{}
	for _, item := range all {
		switch item.Status {
		case "queued":
			status.Queued++
		case "claimed":
			status.Claimed++
		case "acknowledged":
			status.Acknowledged++
		case "resolved":
			status.Resolved++
			continue
		}
		items = append(items, item)
	}
	status.Items = items
	if limit < 0 {
		limit = 0
	}
	if len(status.Items) > limit {
		status.Items = status.Items[:limit]
		status.Omitted = len(items) - limit
	}
	return status
}
