package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

var normalizedRawRunIDRE = regexp.MustCompile(
	`^normalized-vs-raw-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`,
)

const knowledgePolicyPath = ".re-discipline/knowledge/policy.jsonc"

// ArchiveFallbackOptInDecision is the explicit manager authorization that
// turns one independently retained, passing normalized-vs-raw candidate into
// durable policy. The cache path is derived from CandidateRunID; callers never
// choose an arbitrary file to ratify.
type ArchiveFallbackOptInDecision struct {
	CandidateRunID         string `json:"candidateRunId"`
	CandidateReportDigest  string `json:"candidateReportDigest"`
	CandidateContentDigest string `json:"candidateContentDigest"`
	RatifiedAt             string `json:"ratifiedAt"`
	ExpectedSettingsDigest string `json:"expectedSettingsDigest"`
}

func validNormalizedRawRunID(value string) bool {
	return normalizedRawRunIDRE.MatchString(value)
}

func normalizedRawCacheRelative(runID string) string {
	return filepath.ToSlash(filepath.Join("normalized-vs-raw", runID, "report.json"))
}

func normalizedRawMeasurementRelative(runID string) string {
	return ".re-discipline/knowledge/measurements/normalized-vs-raw/" + runID + "/report.json"
}

func archiveOptInReceiptID(reportDigest string) string {
	hex := strings.TrimPrefix(reportDigest, "sha256:")
	if len(hex) < 24 {
		return "normalized-beats-raw-invalid"
	}
	return "normalized-beats-raw-" + hex[:24]
}

func archiveOptInReceiptRelative(reportDigest string) string {
	return ".re-discipline/knowledge/receipts/" + archiveOptInReceiptID(reportDigest) + ".json"
}

func validateArchiveFallbackOptInDecision(decision ArchiveFallbackOptInDecision) error {
	if !validNormalizedRawRunID(decision.CandidateRunID) {
		return errors.New("archive opt-in candidate run id is invalid")
	}
	if !digestRE.MatchString(decision.CandidateReportDigest) ||
		!digestRE.MatchString(decision.CandidateContentDigest) ||
		!digestRE.MatchString(decision.ExpectedSettingsDigest) {
		return errors.New("archive opt-in requires exact candidate report, content, and settings digests")
	}
	if err := validateUTC(decision.RatifiedAt); err != nil {
		return fmt.Errorf("archive opt-in ratifiedAt: %w", err)
	}
	return nil
}

func canonicalKnowledgeSettingsBody(settings KnowledgeSettings) ([]byte, error) {
	settings.Schema = "plugin://re-discipline/schemas/knowledge-settings.schema.json"
	if err := ValidateSettings(settings); err != nil {
		return nil, err
	}
	return canonicalJSON(settings)
}

func validateArchiveReceiptIntrinsic(
	receipt NormalizedBeatsRawReceipt,
	receiptPath string,
) error {
	if receipt.SchemaVersion != ArchiveFallbackReceiptVersion ||
		!managedSlugRE.MatchString(receipt.ID) || receipt.Status != "ratified" ||
		strings.TrimSpace(receipt.RatifiedBy) == "" || !receipt.Passed {
		return errors.New("archive receipt is not a ratified passed v2 evaluation")
	}
	if !correlationIDRE.MatchString(receipt.DecisionCorrelationID) ||
		strings.TrimSpace(receipt.DecisionIdempotencyKey) == "" ||
		len(receipt.DecisionIdempotencyKey) > 256 {
		return errors.New("archive receipt decision identity is incomplete")
	}
	if err := validateUTC(receipt.EvaluatedAt); err != nil {
		return errors.New("archive receipt evaluatedAt is invalid")
	}
	if err := validateUTC(receipt.RatifiedAt); err != nil {
		return errors.New("archive receipt ratifiedAt is invalid")
	}
	evaluated, _ := time.Parse(time.RFC3339Nano, receipt.EvaluatedAt)
	ratified, _ := time.Parse(time.RFC3339Nano, receipt.RatifiedAt)
	if ratified.Before(evaluated) {
		return errors.New("archive receipt predates its measurement")
	}
	if !managedSlugRE.MatchString(receipt.SuiteID) ||
		!digestRE.MatchString(receipt.SuiteDigest) ||
		!digestRE.MatchString(receipt.ReportDigest) ||
		!digestRE.MatchString(receipt.ReportContentDigest) ||
		!digestRE.MatchString(receipt.PairedEvaluationDigest) ||
		!digestRE.MatchString(receipt.QuestionsDigest) ||
		!digestRE.MatchString(receipt.ContractDigest) ||
		!digestRE.MatchString(receipt.PreviousSettingsDigest) ||
		!digestRE.MatchString(receipt.ResultingSettingsDigest) {
		return errors.New("archive receipt evidence or settings binding is incomplete")
	}
	if err := ValidateNormalizedRawMeasurementPath(receipt.ReportPath, receipt.ReportRunID); err != nil {
		return err
	}
	if receipt.Generation.CorpusFingerprint != receipt.Binding.CorpusFingerprint ||
		receipt.Generation.RuntimeFingerprint != receipt.Binding.RuntimeFingerprint ||
		receipt.Generation.ID == "" || receipt.Generation.ModelFingerprint == "" {
		return errors.New("archive receipt generation does not bind its representation")
	}
	if err := ValidateArchiveReceiptPath(receiptPath); err != nil {
		return err
	}
	wantReceiptPath := ".re-discipline/knowledge/receipts/" + receipt.ID + ".json"
	if receiptPath != wantReceiptPath || receipt.ID != archiveOptInReceiptID(receipt.ReportDigest) {
		return errors.New("archive receipt path and id are not derived from the ratified report")
	}
	if receipt.PreviousSettingsDigest == receipt.ResultingSettingsDigest {
		return errors.New("archive receipt does not record a real policy transition")
	}
	if receipt.ResultingSettings.Archive.FallbackMode != "opt-in" ||
		receipt.ResultingSettings.Archive.ReportFallbackUntilMeasured ||
		receipt.ResultingSettings.Archive.NormalizedBeatsRawReceipt != receiptPath ||
		!receipt.ResultingSettings.Sources.ReportFallback {
		return errors.New("archive receipt does not bind the exact opt-in settings")
	}
	settingsBody, err := canonicalKnowledgeSettingsBody(receipt.ResultingSettings)
	if err != nil {
		return err
	}
	if "sha256:"+SHA256Bytes(settingsBody) != receipt.ResultingSettingsDigest {
		return errors.New("archive receipt resulting settings digest mismatch")
	}
	want, err := ArchiveReceiptDigest(receipt)
	if err != nil || receipt.Digest != want {
		return errors.New("archive receipt digest mismatch")
	}
	return nil
}

func validateArchiveReceiptReportIdentity(
	receipt NormalizedBeatsRawReceipt,
	report NormalizedRawGateReport,
) error {
	if report.SchemaVersion != NormalizedRawGateReportVersion ||
		report.Kind != "normalized-vs-raw-candidate" || !report.NonAuthoritative ||
		report.RunID != receipt.ReportRunID || report.Digest != receipt.ReportDigest ||
		report.SuiteID != receipt.SuiteID || report.SuiteDigest != receipt.SuiteDigest ||
		report.EvaluatedAt != receipt.EvaluatedAt || report.Binding != receipt.Binding ||
		!reflect.DeepEqual(report.Generation, receipt.Generation) ||
		report.QuestionsDigest != receipt.QuestionsDigest ||
		report.ContractDigest != receipt.ContractDigest ||
		report.PairedEvaluation.Digest != receipt.PairedEvaluationDigest {
		return errors.New("archive receipt does not bind the exact normalized-vs-raw report")
	}
	copy := report
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || want != digest {
		return errors.New("normalized-vs-raw report digest mismatch")
	}
	metrics := report.PairedEvaluation
	if receipt.CaseCount != report.CaseCount ||
		receipt.NormalizedRecall != metrics.FindingRecall ||
		receipt.RawRecall != metrics.RawPathRecall ||
		receipt.AbstentionAccuracy != metrics.AbstentionAccuracy ||
		receipt.FindingHandleAccuracy != metrics.FindingHandleAccuracy ||
		receipt.EvidenceHandleAccuracy != metrics.EvidenceHandleAccuracy ||
		receipt.SourceClassAccuracy != metrics.SourceClassAccuracy ||
		receipt.ReviewStateAccuracy != metrics.ReviewStateAccuracy ||
		receipt.ValidityAccuracy != metrics.ValidityAccuracy ||
		receipt.VocabularyDisjointRate != metrics.VocabularyDisjointRate ||
		receipt.DurabilityAccuracy != metrics.DurabilityLabelAccuracy ||
		receipt.HardNegativeHits != metrics.HardNegativeHits ||
		receipt.ReplayRate != metrics.DeterministicReplayRate ||
		receipt.NormalizedMedianTokens != metrics.NormalizedMedianTokens ||
		receipt.RawMedianTokens != metrics.RawMedianTokens {
		return errors.New("archive receipt aggregate summary differs from its case-level report")
	}
	if !report.Checks.AllPassed || !report.Decision.PromotionEligible ||
		report.Decision.Outcome != "passed" || report.Decision.AuthorizationReceipt {
		return errors.New("archive receipt report is not a passing non-authoritative candidate")
	}
	return nil
}

func validateNormalizedRawGateReportForSuite(
	report NormalizedRawGateReport,
	suite FindingEvalSuite,
) error {
	if report.SchemaVersion != NormalizedRawGateReportVersion ||
		report.Kind != "normalized-vs-raw-candidate" || !report.NonAuthoritative ||
		report.SuiteID != suite.ID || report.SuiteDigest != suite.Digest ||
		report.CaseCount != len(suite.Cases) || len(suite.Cases) != 64 ||
		report.Generation.CorpusFingerprint != suite.CorpusSnapshot ||
		report.Binding.CorpusFingerprint != suite.CorpusSnapshot {
		return errors.New("normalized-vs-raw report does not bind the exact 64-case suite and corpus")
	}
	if err := ValidateFindingEvalSuite(suite); err != nil {
		return err
	}
	if err := validatePairedFindingEvaluation(suite, report.PairedEvaluation); err != nil {
		return err
	}
	questions, questionsDigest, err := normalizedRawQuestionBindings(suite.Cases)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(report.Questions, questions) || report.QuestionsDigest != questionsDigest {
		return errors.New("normalized-vs-raw question contracts do not recompute")
	}
	contractDigest, err := CanonicalDigest(struct {
		SuiteDigest     string                 `json:"suiteDigest"`
		QuestionsDigest string                 `json:"questionsDigest"`
		Binding         ArchiveFallbackBinding `json:"binding"`
		GenerationID    string                 `json:"generationId"`
		NormalizedArm   string                 `json:"normalizedArm"`
		RawArm          string                 `json:"rawArm"`
	}{
		SuiteDigest: suite.Digest, QuestionsDigest: questionsDigest,
		Binding: report.Binding, GenerationID: report.Generation.ID,
		NormalizedArm: "finding-only-same-budget-v1",
		RawArm:        "raw-report-only-same-budget-v1",
	})
	if err != nil || report.ContractDigest != contractDigest {
		return errors.New("normalized-vs-raw arm contract does not recompute")
	}
	checks := recomputeNormalizedRawGateChecks(report, suite)
	if !reflect.DeepEqual(report.Checks, checks) {
		return errors.New("normalized-vs-raw gate checks do not recompute from case evidence")
	}
	expectedDecision := NormalizedRawGateDecision{
		Outcome: "retain-default-fallback", CurrentArchiveMode: "default-fallback",
		RequiredNextAction: "retain-default-fallback", FailedChecks: normalizedRawFailedChecks(checks),
		AuthorizationReceipt: false,
	}
	expectedDecision.PromotionEligible = checks.AllPassed
	if checks.AllPassed {
		expectedDecision.Outcome = "passed"
		expectedDecision.RequiredNextAction = "explicitly-ratify-opt-in-receipt"
	}
	if !reflect.DeepEqual(report.Decision, expectedDecision) {
		return errors.New("normalized-vs-raw decision does not recompute from gate checks")
	}
	copy := report
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || want != digest {
		return errors.New("normalized-vs-raw report digest mismatch")
	}
	return nil
}

func recomputeNormalizedRawGateChecks(
	report NormalizedRawGateReport,
	suite FindingEvalSuite,
) NormalizedRawGateChecks {
	evaluation := report.PairedEvaluation
	nonInferior := func(value FindingEvaluationMetrics) bool {
		return value.FindingRecall >= value.RawPathRecall
	}
	lowerTokens := func(value FindingEvaluationMetrics) bool {
		return value.NormalizedMedianTokens > 0 && value.RawMedianTokens > 0 &&
			value.NormalizedMedianTokens < value.RawMedianTokens
	}
	checks := NormalizedRawGateChecks{
		Exactly64Cases:             len(suite.Cases) == 64,
		FreshCorpusBinding:         suite.CorpusSnapshot == report.Generation.CorpusFingerprint,
		IdenticalQuestionContracts: len(report.Questions) == 64 && report.EffectiveProfile == report.Binding.ProfileIdentity,
		OverallRecallNonInferior:   evaluation.FindingRecall >= evaluation.RawPathRecall,
		SplitRecallNonInferior:     normalizedRawSlicesPass(evaluation.MetricsBySplit, nonInferior),
		RoleRecallNonInferior:      normalizedRawSlicesPass(evaluation.MetricsByRole, nonInferior),
		CompleteKnownRecall:        evaluation.FindingRecall == 1 && evaluation.RawPathRecall == 1,
		AbstentionExact:            evaluation.AbstentionAccuracy == 1,
		FindingHandlesExact:        evaluation.FindingHandleAccuracy == 1,
		EvidenceHandlesExact:       evaluation.EvidenceHandleAccuracy == 1,
		SourceClassesExact:         evaluation.SourceClassAccuracy == 1,
		ReviewStatesExact:          evaluation.ReviewStateAccuracy == 1,
		ValiditiesExact:            evaluation.ValidityAccuracy == 1,
		VocabularyDisjointExact:    evaluation.VocabularyDisjointRate == 1,
		DurabilityLabelsExact:      evaluation.DurabilityLabelAccuracy == 1,
		HardNegativesZero:          evaluation.HardNegativeHits == 0,
		DeterministicReplayExact:   evaluation.DeterministicReplayRate == 1,
		LowerTokenCostOverall: normalizedRawTotalCostLower(
			evaluation.Cases, suite.Cases, func(FindingEvalCase) bool { return true }) &&
			evaluation.NormalizedMedianTokens > 0 && evaluation.RawMedianTokens > 0 &&
			evaluation.NormalizedMedianTokens < evaluation.RawMedianTokens,
		LowerTokenCostBySplit: normalizedRawSliceTotalsLower(
			evaluation.Cases, suite.Cases, []string{"development", "holdout"},
			func(eval FindingEvalCase) string { return eval.Split }) &&
			normalizedRawSlicesPass(evaluation.MetricsBySplit, lowerTokens),
		LowerTokenCostByRole: normalizedRawSliceTotalsLower(
			evaluation.Cases, suite.Cases, []string{"manager", "drafter"},
			func(eval FindingEvalCase) string { return eval.Role }) &&
			normalizedRawSlicesPass(evaluation.MetricsByRole, lowerTokens),
	}
	checks.AllPassed = len(normalizedRawFailedChecks(checks)) == 0
	return checks
}

func normalizedRawFailedChecks(checks NormalizedRawGateChecks) []string {
	rows := map[string]bool{
		"exactly-64-cases":             checks.Exactly64Cases,
		"fresh-corpus-binding":         checks.FreshCorpusBinding,
		"identical-question-contracts": checks.IdenticalQuestionContracts,
		"overall-recall-non-inferior":  checks.OverallRecallNonInferior,
		"split-recall-non-inferior":    checks.SplitRecallNonInferior,
		"role-recall-non-inferior":     checks.RoleRecallNonInferior,
		"complete-known-recall":        checks.CompleteKnownRecall,
		"abstention-exact":             checks.AbstentionExact,
		"finding-handles-exact":        checks.FindingHandlesExact,
		"evidence-handles-exact":       checks.EvidenceHandlesExact,
		"source-classes-exact":         checks.SourceClassesExact,
		"review-states-exact":          checks.ReviewStatesExact,
		"validities-exact":             checks.ValiditiesExact,
		"vocabulary-disjoint-exact":    checks.VocabularyDisjointExact,
		"durability-labels-exact":      checks.DurabilityLabelsExact,
		"hard-negatives-zero":          checks.HardNegativesZero,
		"deterministic-replay-exact":   checks.DeterministicReplayExact,
		"lower-token-cost-overall":     checks.LowerTokenCostOverall,
		"lower-token-cost-by-split":    checks.LowerTokenCostBySplit,
		"lower-token-cost-by-role":     checks.LowerTokenCostByRole,
	}
	failed := []string{}
	for name, passed := range rows {
		if !passed {
			failed = append(failed, name)
		}
	}
	sort.Strings(failed)
	return failed
}

func (service *Service) prepareArchiveFallbackOptInArtifacts(
	ctx context.Context,
	store *StateStore,
	request ManagerApplyRequest,
) ([]StateArtifactWrite, error) {
	if request.Action != "knowledge.archive-fallback.opt-in" {
		return nil, nil
	}
	if request.ArchiveFallbackDecision == nil {
		return nil, errors.New("archive fallback opt-in requires an explicit decision payload")
	}
	decision := *request.ArchiveFallbackDecision
	if err := validateArchiveFallbackOptInDecision(decision); err != nil {
		return nil, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return nil, err
	}
	if head.Revision != request.ExpectedHeadRevision || head.Digest != request.ExpectedHeadDigest {
		return nil, fmt.Errorf("%w: archive opt-in expected state head is stale", ErrStateConflict)
	}
	// Reload every project- and package-owned input before reading the retained
	// candidate. A Service may have been alive while policy, suite, or profile
	// files changed; its construction-time catalog is not ratification evidence.
	refreshed, err := NewService(ServiceOptions{
		ProjectRoot: service.Boundary.Root,
		AssetRoot:   service.AssetRoot,
		CacheRoot:   service.Index.CacheRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("reload archive opt-in control plane: %w", err)
	}
	service = refreshed
	candidatePath, err := containedOutputPath(
		service.Index.CacheRoot, normalizedRawCacheRelative(decision.CandidateRunID))
	if err != nil {
		return nil, err
	}
	candidateBody, err := readSingleLinkRegularFile(candidatePath)
	if err != nil {
		return nil, fmt.Errorf("read normalized-vs-raw candidate: %w", err)
	}
	if "sha256:"+SHA256Bytes(candidateBody) != decision.CandidateContentDigest {
		return nil, errors.New("normalized-vs-raw candidate content digest changed before ratification")
	}
	var report NormalizedRawGateReport
	if err := decodeStrict(candidateBody, &report); err != nil {
		return nil, fmt.Errorf("decode normalized-vs-raw candidate: %w", err)
	}
	if report.RunID != decision.CandidateRunID || report.Digest != decision.CandidateReportDigest {
		return nil, errors.New("normalized-vs-raw candidate identity differs from the retained decision")
	}
	canonicalCandidate, err := canonicalJSON(report)
	if err != nil || !bytes.Equal(candidateBody, canonicalCandidate) {
		return nil, errors.New("normalized-vs-raw candidate is not canonical engine output")
	}
	suites, err := service.loadProjectFindingEvalSuites()
	if err != nil {
		return nil, err
	}
	var suite *FindingEvalSuite
	for index := range suites {
		if suites[index].ID == report.SuiteID && suites[index].Digest == report.SuiteDigest {
			candidate := suites[index]
			suite = &candidate
			break
		}
	}
	if suite == nil {
		return nil, errors.New("normalized-vs-raw candidate no longer binds a ratified project suite")
	}
	if err := validateNormalizedRawGateReportForSuite(report, *suite); err != nil {
		return nil, err
	}
	generation, selected, lease, err := service.leaseMeasurementGeneration(
		ctx, "archive-opt-in-replay")
	if err != nil {
		return nil, fmt.Errorf("revalidate archive opt-in generation: %w", err)
	}
	defer lease.Release()
	if generation.ServingStale || !reflect.DeepEqual(report.Generation, CompactContextGeneration(generation)) ||
		report.ProfileName != selected.Effective.Name ||
		report.EffectiveProfile != selected.EffectiveIdentity ||
		!reflect.DeepEqual(report.ActiveLanes, selected.ActiveLanes) {
		return nil, errors.New("normalized-vs-raw candidate is stale for the active generation or profile")
	}
	binding, err := (Retriever{Generation: generation, Profile: selected}).archiveFallbackBinding()
	if err != nil {
		return nil, err
	}
	if report.Binding != binding {
		return nil, errors.New("normalized-vs-raw candidate does not bind the active representation")
	}
	// The retained cache report is an input proposal, never evidence by
	// itself. Re-run both finding-only and raw-report-only arms for every case
	// against the leased current generation. Then compile a fresh expected
	// report from those engine-derived rows and require byte-semantic equality.
	// This rejects an attacker who alters a row, recomputes every aggregate,
	// and reseals every nested and outer digest.
	replayedEvaluation, err := EvaluateFindingSuite(ctx, Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}, *suite)
	if err != nil {
		return nil, fmt.Errorf("independently replay normalized-vs-raw candidate: %w", err)
	}
	expectedReport, err := buildNormalizedRawGateReportForRun(
		report.RunID, report.EvaluatedAt, *suite, generation, selected, replayedEvaluation)
	if err != nil {
		return nil, fmt.Errorf("compile independently replayed normalized-vs-raw report: %w", err)
	}
	if !reflect.DeepEqual(report, expectedReport) {
		return nil, errors.New(
			"normalized-vs-raw candidate differs from the independent current-generation replay")
	}
	// Close the measurement window by reloading the control plane and ensuring
	// its corpus, suite, and effective profile still identify the exact replay
	// inputs. A concurrent source or profile rewrite cannot be ratified merely
	// because the leased database remains readable.
	if err := service.revalidateArchiveOptInReplayInputs(ctx, generation, selected, *suite); err != nil {
		return nil, err
	}
	configuration := LoadConfiguration(service.Boundary.Root)
	if !configuration.Valid {
		return nil, fmt.Errorf("reload archive policy before ratification: %s", strings.Join(configuration.Errors, "; "))
	}
	policyPath, err := service.Boundary.Resolve(knowledgePolicyPath, true)
	if err != nil {
		return nil, err
	}
	policyBody, err := readSingleLinkRegularFile(policyPath)
	if err != nil {
		return nil, err
	}
	if "sha256:"+SHA256Bytes(policyBody) != decision.ExpectedSettingsDigest {
		return nil, fmt.Errorf("%w: archive policy changed before ratification", ErrStateConflict)
	}
	mode := configuration.Settings.Archive.FallbackMode
	if mode == "" {
		mode = "default-fallback"
	}
	if mode != "default-fallback" || !configuration.Settings.Archive.ReportFallbackUntilMeasured {
		return nil, errors.New("archive policy is not at the default-fallback decision boundary")
	}
	receiptPath := archiveOptInReceiptRelative(report.Digest)
	resultingSettings := configuration.Settings
	resultingSettings.Schema = "plugin://re-discipline/schemas/knowledge-settings.schema.json"
	resultingSettings.Archive.FallbackMode = "opt-in"
	resultingSettings.Archive.ReportFallbackUntilMeasured = false
	resultingSettings.Archive.NormalizedBeatsRawReceipt = receiptPath
	settingsBody, err := canonicalKnowledgeSettingsBody(resultingSettings)
	if err != nil {
		return nil, err
	}
	measurementPath := normalizedRawMeasurementRelative(report.RunID)
	receipt, err := buildNormalizedBeatsRawReceipt(
		request, decision, report, *suite, measurementPath, receiptPath,
		resultingSettings, "sha256:"+SHA256Bytes(settingsBody))
	if err != nil {
		return nil, err
	}
	receiptBody, err := canonicalJSON(receipt)
	if err != nil {
		return nil, err
	}
	if err := validateArchiveReceiptIntrinsic(receipt, receiptPath); err != nil {
		return nil, err
	}
	if err := validateArchiveReceiptReportIdentity(receipt, report); err != nil {
		return nil, err
	}
	if err := validateArchiveReceiptSuiteBinding(receipt, *suite); err != nil {
		return nil, err
	}
	return []StateArtifactWrite{
		{
			Path: measurementPath, ContentDigest: decision.CandidateContentDigest,
			Body: append([]byte(nil), candidateBody...),
		},
		{
			Path: receiptPath, ContentDigest: "sha256:" + SHA256Bytes(receiptBody),
			Body: receiptBody,
		},
		{
			Path: knowledgePolicyPath, ExpectedDigest: decision.ExpectedSettingsDigest,
			ContentDigest: "sha256:" + SHA256Bytes(settingsBody), Body: settingsBody,
		},
	}, nil
}

func (service *Service) revalidateArchiveOptInReplayInputs(
	ctx context.Context,
	generation Generation,
	selected SelectedProfile,
	suite FindingEvalSuite,
) error {
	current, err := NewService(ServiceOptions{
		ProjectRoot: service.Boundary.Root,
		AssetRoot:   service.AssetRoot,
		CacheRoot:   service.Index.CacheRoot,
	})
	if err != nil {
		return fmt.Errorf("reload archive opt-in replay inputs: %w", err)
	}
	currentGeneration, _, currentSelected, _, err := current.ensure(ctx)
	if err != nil {
		return fmt.Errorf("recheck archive opt-in generation: %w", err)
	}
	if !reflect.DeepEqual(CompactContextGeneration(currentGeneration), CompactContextGeneration(generation)) ||
		currentSelected.Effective.Name != selected.Effective.Name ||
		currentSelected.EffectiveIdentity != selected.EffectiveIdentity ||
		!reflect.DeepEqual(currentSelected.ActiveLanes, selected.ActiveLanes) {
		return errors.New(
			"normalized-vs-raw replay became stale before archive opt-in ratification")
	}
	suites, err := current.loadProjectFindingEvalSuites()
	if err != nil {
		return fmt.Errorf("recheck archive opt-in suite: %w", err)
	}
	for _, candidate := range suites {
		if candidate.ID == suite.ID && candidate.Digest == suite.Digest {
			if !reflect.DeepEqual(candidate, suite) {
				return errors.New("normalized-vs-raw suite bytes changed without a new digest")
			}
			return nil
		}
	}
	return errors.New("normalized-vs-raw suite changed during archive opt-in replay")
}

func buildNormalizedBeatsRawReceipt(
	request ManagerApplyRequest,
	decision ArchiveFallbackOptInDecision,
	report NormalizedRawGateReport,
	suite FindingEvalSuite,
	measurementPath string,
	receiptPath string,
	resultingSettings KnowledgeSettings,
	resultingSettingsDigest string,
) (NormalizedBeatsRawReceipt, error) {
	type counts struct{ cases, manager, drafter, abstention int }
	bySplit := map[string]*counts{"development": {}, "holdout": {}}
	for _, eval := range suite.Cases {
		row, ok := bySplit[eval.Split]
		if !ok {
			return NormalizedBeatsRawReceipt{}, fmt.Errorf("unsupported finding evaluation split %q", eval.Split)
		}
		row.cases++
		if eval.Role == "manager" {
			row.manager++
		} else if eval.Role == "drafter" {
			row.drafter++
		} else {
			return NormalizedBeatsRawReceipt{}, fmt.Errorf("unsupported finding evaluation role %q", eval.Role)
		}
		if len(eval.ExpectedFindingIDs) == 0 && len(eval.ExpectedRawPaths) == 0 {
			row.abstention++
		}
	}
	metrics := report.PairedEvaluation
	receipt := NormalizedBeatsRawReceipt{
		SchemaVersion: ArchiveFallbackReceiptVersion,
		ID:            archiveOptInReceiptID(report.Digest), Status: "ratified",
		RatifiedAt: decision.RatifiedAt, RatifiedBy: request.Actor,
		DecisionCorrelationID:  request.CorrelationID,
		DecisionIdempotencyKey: request.IdempotencyKey,
		EvaluatedAt:            report.EvaluatedAt, SuiteID: report.SuiteID,
		SuiteDigest: report.SuiteDigest, ReportRunID: report.RunID,
		ReportPath: measurementPath, ReportDigest: report.Digest,
		ReportContentDigest:    decision.CandidateContentDigest,
		PairedEvaluationDigest: report.PairedEvaluation.Digest,
		QuestionsDigest:        report.QuestionsDigest, ContractDigest: report.ContractDigest,
		Generation: report.Generation, Binding: report.Binding,
		PreviousSettingsDigest:  decision.ExpectedSettingsDigest,
		ResultingSettingsDigest: resultingSettingsDigest,
		ResultingSettings:       resultingSettings,
		CaseCount:               len(suite.Cases),
		DevelopmentCases:        bySplit["development"].cases,
		DevelopmentManager:      bySplit["development"].manager,
		DevelopmentDrafter:      bySplit["development"].drafter,
		DevelopmentAbstention:   bySplit["development"].abstention,
		HoldoutCases:            bySplit["holdout"].cases,
		HoldoutManager:          bySplit["holdout"].manager,
		HoldoutDrafter:          bySplit["holdout"].drafter,
		HoldoutAbstention:       bySplit["holdout"].abstention,
		NormalizedRecall:        metrics.FindingRecall, RawRecall: metrics.RawPathRecall,
		AbstentionAccuracy:     metrics.AbstentionAccuracy,
		FindingHandleAccuracy:  metrics.FindingHandleAccuracy,
		EvidenceHandleAccuracy: metrics.EvidenceHandleAccuracy,
		SourceClassAccuracy:    metrics.SourceClassAccuracy,
		ReviewStateAccuracy:    metrics.ReviewStateAccuracy,
		ValidityAccuracy:       metrics.ValidityAccuracy,
		VocabularyDisjointRate: metrics.VocabularyDisjointRate,
		DurabilityAccuracy:     metrics.DurabilityLabelAccuracy,
		HardNegativeHits:       metrics.HardNegativeHits,
		ReplayRate:             metrics.DeterministicReplayRate,
		NormalizedMedianTokens: metrics.NormalizedMedianTokens,
		RawMedianTokens:        metrics.RawMedianTokens,
		Passed:                 report.Checks.AllPassed,
	}
	if receipt.ResultingSettings.Archive.NormalizedBeatsRawReceipt != receiptPath {
		return NormalizedBeatsRawReceipt{}, errors.New("resulting settings do not select the derived archive receipt")
	}
	digest, err := ArchiveReceiptDigest(receipt)
	if err != nil {
		return NormalizedBeatsRawReceipt{}, err
	}
	receipt.Digest = digest
	return receipt, nil
}

func archiveFallbackArtifactsFromReceipt(
	boundary Boundary,
	request ManagerApplyRequest,
	receipt NormalizedBeatsRawReceipt,
) ([]StateArtifactWrite, error) {
	if request.ArchiveFallbackDecision == nil {
		return nil, errors.New("archive fallback replay omits its decision")
	}
	decision := *request.ArchiveFallbackDecision
	if err := validateArchiveFallbackOptInDecision(decision); err != nil {
		return nil, err
	}
	if receipt.ReportRunID != decision.CandidateRunID ||
		receipt.ReportDigest != decision.CandidateReportDigest ||
		receipt.ReportContentDigest != decision.CandidateContentDigest ||
		receipt.PreviousSettingsDigest != decision.ExpectedSettingsDigest ||
		receipt.RatifiedAt != decision.RatifiedAt || receipt.RatifiedBy != request.Actor ||
		receipt.DecisionCorrelationID != request.CorrelationID ||
		receipt.DecisionIdempotencyKey != request.IdempotencyKey {
		return nil, ErrIdempotencyConflict
	}
	receiptPath := archiveOptInReceiptRelative(receipt.ReportDigest)
	if err := validateArchiveReceiptIntrinsic(receipt, receiptPath); err != nil {
		return nil, err
	}
	reportBody, err := readProjectControlFile(boundary, receipt.ReportPath)
	if err != nil || "sha256:"+SHA256Bytes(reportBody) != receipt.ReportContentDigest {
		return nil, errors.New("archive fallback replay report no longer verifies")
	}
	receiptBody, err := readProjectControlFile(boundary, receiptPath)
	if err != nil {
		return nil, err
	}
	var stored NormalizedBeatsRawReceipt
	if err := decodeStrict(receiptBody, &stored); err != nil || !reflect.DeepEqual(stored, receipt) {
		return nil, errors.New("archive fallback replay receipt changed")
	}
	settingsBody, err := canonicalKnowledgeSettingsBody(receipt.ResultingSettings)
	if err != nil || "sha256:"+SHA256Bytes(settingsBody) != receipt.ResultingSettingsDigest {
		return nil, errors.New("archive fallback replay settings no longer verify")
	}
	return []StateArtifactWrite{
		{Path: receipt.ReportPath, ContentDigest: receipt.ReportContentDigest, Body: reportBody},
		{Path: receiptPath, ContentDigest: "sha256:" + SHA256Bytes(receiptBody), Body: receiptBody},
		{
			Path: knowledgePolicyPath, ExpectedDigest: decision.ExpectedSettingsDigest,
			ContentDigest: receipt.ResultingSettingsDigest, Body: settingsBody,
		},
	}, nil
}

func replayArchiveFallbackOptIn(
	store *StateStore,
	boundary Boundary,
	request ManagerApplyRequest,
) (StateTransactionReceipt, bool, error) {
	if request.Action != "knowledge.archive-fallback.opt-in" || request.ArchiveFallbackDecision == nil {
		return StateTransactionReceipt{}, false, nil
	}
	transactionReceipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		return StateTransactionReceipt{}, false, err
	}
	if err := validateArchiveFallbackOptInDecision(*request.ArchiveFallbackDecision); err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	receiptPath := archiveOptInReceiptRelative(request.ArchiveFallbackDecision.CandidateReportDigest)
	body, err := readProjectControlFile(boundary, receiptPath)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	var receipt NormalizedBeatsRawReceipt
	if err := decodeStrict(body, &receipt); err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	artifacts, err := archiveFallbackArtifactsFromReceipt(boundary, request, receipt)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	writes, reviewHandle, err := buildManagerWrites(boundary, request, artifacts)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	prepared, err := prepareTransactionRequest(
		managerStateTransactionRequest(request, writes, artifacts, reviewHandle))
	if err != nil || prepared.RequestDigest != transactionReceipt.RequestDigest {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	return transactionReceipt, true, nil
}
