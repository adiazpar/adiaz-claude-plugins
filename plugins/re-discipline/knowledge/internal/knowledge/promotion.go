package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ProfilePromotionResult struct {
	SchemaVersion         int    `json:"schemaVersion"`
	ProfilePath           string `json:"profilePath"`
	ProfileID             string `json:"profileId"`
	ProfileDigest         string `json:"profileDigest"`
	BenchmarkMatrixDigest string `json:"benchmarkMatrixDigest"`
	CorpusFingerprint     string `json:"corpusFingerprint"`
	EvalFingerprint       string `json:"evalFingerprint"`
	ReportDigest          string `json:"reportDigest"`
	CandidateDigest       string `json:"candidateDigest"`
	Activated             bool   `json:"activated"`
}

// PromoteProfile is intentionally available only through the manager CLI.
// Calibration and MCP tools cannot call it.
func (service *Service) PromoteProfile(
	ctx context.Context,
	candidatePath string,
	reportPath string,
	explicitUserApproval bool,
) (ProfilePromotionResult, error) {
	if !explicitUserApproval {
		return ProfilePromotionResult{}, errors.New(
			"profile promotion requires the explicit --approve flag after user approval")
	}
	candidateAbsolute, err := service.calibrationArtifactPath(candidatePath)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("candidate profile: %w", err)
	}
	reportAbsolute, err := service.calibrationArtifactPath(reportPath)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("calibration report: %w", err)
	}
	candidateBody, err := os.ReadFile(candidateAbsolute)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	var candidate RetrievalProfile
	if err := decodeStrict(candidateBody, &candidate); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("parse candidate profile: %w", err)
	}
	if candidate.Approval != nil {
		return ProfilePromotionResult{}, errors.New(
			"candidate profile already contains an approval receipt")
	}
	if candidate.BaseProfile != service.ProfileCatalog.ProfileID ||
		candidate.ProfileID == "" || candidate.ProfileID == service.ProfileCatalog.ProfileID {
		return ProfilePromotionResult{}, errors.New(
			"candidate profile does not identify the active profile as its base")
	}
	if err := ValidateProfile(candidate); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("validate candidate profile: %w", err)
	}
	if err := ValidateProfileModels(candidate, service.ModelManifest); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("validate candidate models: %w", err)
	}

	reportBody, err := os.ReadFile(reportAbsolute)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	var report CalibrationReport
	if err := decodeStrict(reportBody, &report); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("parse calibration report: %w", err)
	}
	candidateDigest := "sha256:" + SHA256Bytes(candidateBody)
	if report.CandidateDigest != candidateDigest {
		return ProfilePromotionResult{}, errors.New(
			"calibration report candidate digest does not match the exact candidate artifact")
	}
	reportCandidate, err := canonicalExistingPath(filepath.FromSlash(report.CandidatePath))
	if err != nil || filepath.Clean(reportCandidate) != filepath.Clean(candidateAbsolute) {
		return ProfilePromotionResult{}, errors.New(
			"calibration report does not bind the selected candidate profile")
	}
	if report.SchemaVersion != 1 || report.BaseProfile != service.ProfileCatalog.ProfileID ||
		report.ActiveBefore != report.ActiveAfter || report.Activated ||
		!report.Recommended.HardGatesPassed ||
		!sha256IdentityRE.MatchString(report.Recommended.BenchmarkDigest) {
		return ProfilePromotionResult{}, errors.New(
			"calibration report is not an inactive, hard-gate-passed promotion input")
	}
	projectRows := 0
	var calibratedRow EffectiveProfile
	for _, row := range candidate.EffectiveProfiles {
		if row.Benchmark.Suite == "project-calibration-v1" {
			projectRows++
			calibratedRow = cloneEffectiveProfile(row)
			if row.Benchmark.Status != "passed" ||
				row.Benchmark.Digest != report.Recommended.BenchmarkDigest {
				return ProfilePromotionResult{}, errors.New(
					"candidate profile benchmark does not match the recommended finalist")
			}
		}
	}
	if projectRows != 1 {
		return ProfilePromotionResult{}, errors.New(
			"candidate profile must contain exactly one project-calibrated capability row")
	}

	cases, err := service.loadProjectEvalCases()
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	evalDigest, _ := CanonicalDigest(cases)
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	modelFingerprint := mustDigest(service.ModelManifest.Models)
	if report.EvalDigest != evalDigest ||
		report.CorpusFingerprint != generation.CorpusFingerprint ||
		report.ModelFingerprint != modelFingerprint ||
		report.RuntimeContract != RuntimeContract(selected.Runtime) {
		return ProfilePromotionResult{}, errors.New(
			"calibration evidence is stale for the current eval corpus, source corpus, models, or runtime")
	}
	replayedProfile, err := selectedForRow(
		service.ProfileCatalog.ProfileID,
		calibratedRow,
		selected.Runtime,
		selected.Models,
	)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	if replayedProfile.EffectiveIdentity != report.Recommended.Identity {
		return ProfilePromotionResult{}, errors.New(
			"calibrated row identity does not match the measured recommendation")
	}
	developmentCases, holdoutCases := splitEvalCases(cases)
	if len(developmentCases) == 0 || len(holdoutCases) == 0 {
		return ProfilePromotionResult{}, errors.New(
			"promotion replay requires topic-isolated development and holdout cases")
	}
	retriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: replayedProfile,
	}
	developmentOutcomes, developmentMetrics, err :=
		evaluateRetrieverCases(ctx, retriever, developmentCases)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf(
			"replay development evidence: %w", err)
	}
	holdoutOutcomes, holdoutMetrics, err :=
		evaluateRetrieverCases(ctx, retriever, holdoutCases)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf("replay holdout evidence: %w", err)
	}
	baselineRetriever := Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}
	_, baselineHoldoutMetrics, err := evaluateRetrieverCases(
		ctx, baselineRetriever, holdoutCases)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf(
			"replay active holdout baseline: %w", err)
	}
	violations :=
		developmentMetrics.AuthorityViolations +
			developmentMetrics.CitationViolations +
			developmentMetrics.HardNegativeHits +
			holdoutMetrics.AuthorityViolations +
			holdoutMetrics.CitationViolations +
			holdoutMetrics.HardNegativeHits
	hardGates := hardMetricsPassed(developmentMetrics) &&
		hardMetricsPassed(holdoutMetrics)
	recomputedBenchmark, err := calibrationBenchmarkDigest(
		replayedProfile.EffectiveIdentity,
		evalDigest,
		generation.CorpusFingerprint,
		developmentMetrics,
		holdoutMetrics,
		baselineHoldoutMetrics,
	)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	recomputed := CalibrationCandidate{
		Identity:           replayedProfile.EffectiveIdentity,
		Weights:            cloneWeights(calibratedRow.Weights),
		DevelopmentHit:     relevantPathHits(developmentOutcomes),
		HoldoutHit:         relevantPathHits(holdoutOutcomes),
		DevelopmentMetrics: developmentMetrics,
		HoldoutMetrics:     holdoutMetrics,
		Violations:         violations, HardGatesPassed: hardGates,
		NonInferiorToBaseline: calibrationNonInferior(
			holdoutMetrics, baselineHoldoutMetrics),
		Pareto: true, BenchmarkDigest: recomputedBenchmark,
	}
	if stableJSON(report.Recommended.Weights) != stableJSON(recomputed.Weights) ||
		report.Recommended.Identity != recomputed.Identity ||
		report.Recommended.DevelopmentHit != recomputed.DevelopmentHit ||
		report.Recommended.HoldoutHit != recomputed.HoldoutHit ||
		report.Recommended.Violations != recomputed.Violations ||
		report.Recommended.HardGatesPassed != recomputed.HardGatesPassed ||
		report.Recommended.NonInferiorToBaseline != recomputed.NonInferiorToBaseline ||
		!report.Recommended.Pareto ||
		report.Recommended.BenchmarkDigest != recomputed.BenchmarkDigest ||
		stableJSON(metricsWithoutLatency(report.Recommended.DevelopmentMetrics)) !=
			stableJSON(metricsWithoutLatency(recomputed.DevelopmentMetrics)) ||
		stableJSON(metricsWithoutLatency(report.Recommended.HoldoutMetrics)) !=
			stableJSON(metricsWithoutLatency(recomputed.HoldoutMetrics)) {
		return ProfilePromotionResult{}, errors.New(
			"calibration report recommendation does not match replayed evidence")
	}
	if !hardGates || calibratedRow.Benchmark.Digest != recomputedBenchmark {
		return ProfilePromotionResult{}, errors.New(
			"candidate benchmark receipt does not match replayed hard-gate evidence")
	}
	freshCalibration, err := service.Calibrate(ctx)
	if err != nil {
		return ProfilePromotionResult{}, fmt.Errorf(
			"recompute calibration frontier: %w", err)
	}
	freshRecommended := freshCalibration.Recommended
	if freshRecommended.Identity != recomputed.Identity ||
		stableJSON(freshRecommended.Weights) != stableJSON(recomputed.Weights) ||
		freshRecommended.DevelopmentHit != recomputed.DevelopmentHit ||
		freshRecommended.HoldoutHit != recomputed.HoldoutHit ||
		freshRecommended.Violations != recomputed.Violations ||
		!freshRecommended.HardGatesPassed ||
		!freshRecommended.NonInferiorToBaseline ||
		!freshRecommended.Pareto ||
		freshRecommended.BenchmarkDigest != recomputed.BenchmarkDigest ||
		stableJSON(metricsWithoutLatency(freshRecommended.DevelopmentMetrics)) !=
			stableJSON(metricsWithoutLatency(recomputed.DevelopmentMetrics)) ||
		stableJSON(metricsWithoutLatency(freshRecommended.HoldoutMetrics)) !=
			stableJSON(metricsWithoutLatency(recomputed.HoldoutMetrics)) {
		return ProfilePromotionResult{}, errors.New(
			"candidate is not the freshly recomputed Pareto recommendation")
	}
	matrixDigest, err := benchmarkMatrixDigest(candidate)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	reportDigest := "sha256:" + SHA256Bytes(reportBody)
	approval := map[string]any{
		"decision":                "promoted",
		"explicitUserApproval":    true,
		"approvedAt":              RFC3339UTC(time.Now()),
		"profileDigest":           "",
		"benchmarkMatrixDigest":   matrixDigest,
		"corpusFingerprint":       generation.CorpusFingerprint,
		"evalFingerprint":         evalDigest,
		"modelFingerprint":        modelFingerprint,
		"runtimeContract":         RuntimeContract(selected.Runtime),
		"calibrationReportDigest": reportDigest,
		"candidateDigest":         candidateDigest,
	}
	candidate.Approval = approval
	profileDigest, err := approvedProfileDigest(candidate)
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	candidate.Approval["profileDigest"] = profileDigest
	if err := ValidateProjectProfileApproval(candidate); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf(
			"generated approval receipt failed self-validation: %w", err)
	}
	target := filepath.Join(
		service.Boundary.Root, ".re-discipline", "knowledge", "retrieval-profile.json")
	serialized, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return ProfilePromotionResult{}, err
	}
	serialized = append(serialized, '\n')
	var roundTrip RetrievalProfile
	if err := decodeStrict(serialized, &roundTrip); err != nil {
		return ProfilePromotionResult{}, err
	}
	if err := ValidateProjectProfileApproval(roundTrip); err != nil {
		return ProfilePromotionResult{}, fmt.Errorf(
			"serialized approval receipt failed validation: %w", err)
	}
	previous, previousErr := os.ReadFile(target)
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return ProfilePromotionResult{}, previousErr
	}
	if err := AtomicWrite(target, serialized, 0o600); err != nil {
		return ProfilePromotionResult{}, err
	}
	written, readErr := os.ReadFile(target)
	var activated RetrievalProfile
	validateErr := readErr
	if validateErr == nil && !bytes.Equal(written, serialized) {
		validateErr = errors.New("activated profile bytes differ from approved bytes")
	}
	if validateErr == nil {
		validateErr = decodeStrict(written, &activated)
	}
	if validateErr == nil {
		validateErr = ValidateProjectProfileApproval(activated)
	}
	if validateErr != nil {
		if previousErr == nil {
			_ = AtomicWrite(target, previous, 0o600)
		} else {
			_ = os.Remove(target)
		}
		return ProfilePromotionResult{}, fmt.Errorf(
			"activated profile failed post-write verification: %w", validateErr)
	}
	return ProfilePromotionResult{
		SchemaVersion: 1, ProfilePath: filepath.ToSlash(target),
		ProfileID: candidate.ProfileID, ProfileDigest: profileDigest,
		BenchmarkMatrixDigest: matrixDigest,
		CorpusFingerprint:     generation.CorpusFingerprint,
		EvalFingerprint:       evalDigest, ReportDigest: reportDigest,
		CandidateDigest: candidateDigest,
		Activated:       true,
	}, nil
}

func (service *Service) calibrationArtifactPath(value string) (string, error) {
	if value == "" {
		return "", errors.New("artifact path is required")
	}
	absolute, err := filepath.Abs(filepath.FromSlash(value))
	if err != nil {
		return "", err
	}
	resolved, err := canonicalExistingPath(absolute)
	if err != nil {
		return "", err
	}
	cache, err := canonicalExistingPath(service.Index.CacheRoot)
	if err != nil {
		return "", err
	}
	calibrationRoot := filepath.Clean(filepath.Join(cache, "..", "calibration"))
	if !withinRoot(calibrationRoot, resolved) {
		return "", errors.New("artifact is outside the managed calibration directory")
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("artifact is not a regular file")
	}
	return resolved, nil
}

func benchmarkMatrixDigest(profile RetrievalProfile) (string, error) {
	matrix := make([]map[string]string, 0, len(profile.EffectiveProfiles))
	for _, row := range profile.EffectiveProfiles {
		if row.Benchmark.Status != "passed" ||
			!sha256IdentityRE.MatchString(row.Benchmark.Digest) {
			return "", fmt.Errorf(
				"effective profile %q lacks passed benchmark evidence", row.Name)
		}
		matrix = append(matrix, map[string]string{
			"name": row.Name, "digest": row.Benchmark.Digest,
			"status": row.Benchmark.Status, "suite": row.Benchmark.Suite,
		})
	}
	sort.Slice(matrix, func(i, j int) bool { return matrix[i]["name"] < matrix[j]["name"] })
	return CanonicalDigest(matrix)
}
