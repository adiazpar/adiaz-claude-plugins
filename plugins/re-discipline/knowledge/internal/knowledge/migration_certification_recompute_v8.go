package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

// stringSlicesEquivalent is an order-preserving element comparison that
// treats nil and empty as the same honest emptiness. A benchmark report for a
// project with zero finding-evaluation suites marshals findingSuiteDigests as
// the empty array and decodes as an empty non-nil slice, while the rederived
// certification environment appends onto a nil slice; reflect.DeepEqual calls
// those unequal, which made the environment binding unsatisfiable for exactly
// the projects with an empty finding corpus. The calibration validator
// already normalizes this same representation hazard.
func stringSlicesEquivalent(left, right []string) bool {
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

func validateMigrationBenchmarkEnvironment(
	report ProjectBenchmarkReport,
	environment MigrationCertificationEnvironment,
) error {
	gitRevision, dirtyFingerprint := gitState(environment.ProjectRoot)
	wantProject := filepath.Base(environment.ProjectRoot)
	wantWorktree := "worktree:" + SHA256String(environment.ProjectRoot)[:16]
	createdAt, createdAtErr := time.Parse(time.RFC3339Nano, report.Generation.CreatedAt)
	if !generationIdentityRE.MatchString(report.Generation.ID) ||
		createdAtErr != nil || createdAt.Location() != time.UTC ||
		report.Generation.Project != wantProject ||
		report.Generation.Worktree != wantWorktree ||
		report.Generation.GitRevision != gitRevision ||
		report.Generation.DirtyFingerprint != dirtyFingerprint ||
		report.EvalFingerprint != environment.EvalFingerprint ||
		!stringSlicesEquivalent(report.FindingSuiteDigests, environment.FindingSuiteDigests) ||
		report.Generation.CorpusFingerprint != environment.CorpusFingerprint ||
		report.Generation.ModelFingerprint != environment.ModelFingerprint ||
		report.Generation.ParserVersion != environment.ParserVersion ||
		report.Generation.ChunkerVersion != environment.ChunkerVersion ||
		report.Generation.DocumentCount != environment.DocumentCount ||
		report.Generation.ChunkCount != environment.ChunkCount ||
		report.Generation.ServingStale || report.Generation.WriterContention ||
		!reflect.DeepEqual(
			RuntimeContract(report.Generation.Runtime), environment.RuntimeContract) {
		return errors.New(
			"benchmark is not bound to the exact current corpus, evaluation, model, parser, chunker, and runtime contract")
	}
	if !reflect.DeepEqual(
		report.HardNegativeCoverage,
		MeasureHardNegativeCoverage(environment.EvalCases),
	) {
		return errors.New("benchmark hard-negative coverage is not derived from the current evaluation corpus")
	}
	return nil
}

// bindMigrationReplayGeneration keeps the independently rebuilt SQLite index
// while rebinding its public identity to the exact benchmark generation that
// was just validated against the current project. Generation IDs and creation
// timestamps are deliberately nonce-bearing, so an independent rebuild can
// only reproduce exact context-pack bytes after this binding step.
func bindMigrationReplayGeneration(
	environment MigrationCertificationEnvironment,
	summary GenerationSummary,
) error {
	if environment.replayGeneration == nil || environment.replayService == nil {
		return errors.New("isolated certification replay service was not prepared")
	}
	generation := environment.replayGeneration
	database := generation.Database
	*generation = Generation{
		ID: summary.ID, Database: database,
		CorpusFingerprint:   summary.CorpusFingerprint,
		SettingsFingerprint: generation.SettingsFingerprint,
		ModelFingerprint:    summary.ModelFingerprint,
		Project:             summary.Project, Worktree: summary.Worktree,
		GitRevision:      summary.GitRevision,
		DirtyFingerprint: summary.DirtyFingerprint,
		ParserVersion:    summary.ParserVersion,
		ChunkerVersion:   summary.ChunkerVersion,
		CreatedAt:        summary.CreatedAt,
		Runtime:          summary.Runtime,
		DocumentCount:    summary.DocumentCount,
		ChunkCount:       summary.ChunkCount,
		ServingStale:     summary.ServingStale,
		WriterContention: summary.WriterContention,
	}
	environment.replayService.PinGeneration(*generation)
	clear(environment.caseReplay)
	clear(environment.contextReplay)
	return nil
}

func validateMigrationBenchmarkProfile(
	profile ProjectProfileBenchmark,
	environment MigrationCertificationEnvironment,
	generationID string,
) error {
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return err
	}
	var packagedRow *EffectiveProfile
	for index := range baseline.Profile.EffectiveProfiles {
		if baseline.Profile.EffectiveProfiles[index].Name == profile.ProfileName {
			packagedRow = &baseline.Profile.EffectiveProfiles[index]
			break
		}
	}
	if packagedRow == nil {
		return errors.New("profile is not an embedded packaged benchmark arm")
	}
	selected, err := selectMigrationReplayProfile(environment, *packagedRow)
	if err != nil {
		return err
	}
	if profile.EffectiveProfile != selected.EffectiveIdentity ||
		profile.FallbackReason != nil {
		return errors.New("profile effective identity or fallback does not match the packaged runtime")
	}

	metrics, err := validateMigrationCaseOutcomes(
		profile.Cases, environment.EvalCases, 0, environment, selected,
	)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(profile.Metrics, metrics) {
		return errors.New("profile aggregate metrics are not derived from its exact case rows")
	}
	developmentOutcomes, developmentCases := filterEvalSplit(
		profile.Cases, environment.EvalCases, "development")
	holdoutOutcomes, holdoutCases := filterEvalSplit(
		profile.Cases, environment.EvalCases, "holdout")
	wantBySplit := map[string]QualityMetrics{
		"development": calculateMetrics(developmentOutcomes, developmentCases),
		"holdout":     calculateMetrics(holdoutOutcomes, holdoutCases),
	}
	if !reflect.DeepEqual(profile.MetricsBySplit, wantBySplit) {
		return errors.New("profile split metrics are not derived from its exact case rows")
	}

	budgetKeys := []string{"512", "1024", "2048", "4096"}
	wantByBudget := map[string]QualityMetrics{}
	wantQualityByBudget := map[string]QualityMetrics{}
	hardGates := hardMetricsPassed(metrics)
	for _, outcome := range profile.Cases {
		hardGates = hardGates && evaluationOutcomePassed(outcome)
	}
	for _, key := range budgetKeys {
		budget := 0
		if _, err := fmt.Sscanf(key, "%d", &budget); err != nil {
			return err
		}
		outcomes, present := profile.CasesByBudget[key]
		if !present {
			return fmt.Errorf("profile omits budget %s case rows", key)
		}
		budgetMetrics, err := validateMigrationCaseOutcomes(
			outcomes, environment.EvalCases, budget, environment, selected,
		)
		if err != nil {
			return fmt.Errorf("budget %s: %w", key, err)
		}
		wantByBudget[key] = budgetMetrics
		wantQualityByBudget[key] = applicableMetrics(outcomes, environment.EvalCases)
		for _, outcome := range outcomes {
			hardGates = hardGates && evaluationOutcomePassed(outcome)
		}
		if hasApplicableQualityGate(outcomes) {
			hardGates = hardGates && hardMetricsPassed(wantQualityByBudget[key])
		}
	}
	if len(profile.CasesByBudget) != len(budgetKeys) ||
		!reflect.DeepEqual(profile.MetricsByBudget, wantByBudget) ||
		!reflect.DeepEqual(profile.QualityMetricsByBudget, wantQualityByBudget) {
		return errors.New("profile budget metrics or rows are incomplete or self-authored")
	}

	contextRoles := migrationEvalRoles(environment.EvalCases)
	contextPassed, err := validateMigrationContextOutcomes(
		profile.ContextPackCases, environment.EvalCases, 0, profile.EffectiveProfile,
		generationID, *packagedRow, environment, selected,
	)
	if err != nil {
		return err
	}
	for _, key := range budgetKeys {
		budget := 0
		_, _ = fmt.Sscanf(key, "%d", &budget)
		outcomes, present := profile.ContextPacksByBudget[key]
		if !present {
			return fmt.Errorf("profile omits budget %s context rows", key)
		}
		passed, err := validateMigrationContextOutcomes(
			outcomes, environment.EvalCases, budget, profile.EffectiveProfile,
			generationID, *packagedRow, environment, selected,
		)
		if err != nil {
			return fmt.Errorf("context budget %s: %w", key, err)
		}
		contextPassed = contextPassed && passed
	}
	if len(profile.ContextPacksByBudget) != len(budgetKeys) ||
		!reflect.DeepEqual(profile.ContextPackRoles, contextRoles) ||
		profile.ContextPackPassed != contextPassed {
		return errors.New("profile context coverage or gate result is not reproducible")
	}
	hardGates = hardGates && contextPassed

	findingsPassed, err := validateMigrationFindingEvaluations(
		profile.FindingEvaluations, environment.FindingSuites, environment, selected)
	if err != nil {
		return err
	}
	hardGates = hardGates && findingsPassed
	if profile.HardGatesPassed != hardGates {
		return errors.New("profile hard-gate boolean is not derived from exact case and context rows")
	}
	return nil
}

// deriveMigrationReplayService builds a per-call replay service scoped to one
// effective profile and pinned generation. The base service's mutex-guarded
// telemetry and lease state must not be shared or copied.
func deriveMigrationReplayService(
	base *Service,
	profileName string,
	generation Generation,
) *Service {
	service := &Service{
		Boundary:             base.Boundary,
		AssetRoot:            base.AssetRoot,
		Configuration:        base.Configuration,
		ProfileCatalog:       base.ProfileCatalog,
		ModelManifest:        base.ModelManifest,
		Index:                base.Index,
		EffectiveProfileName: profileName,
		Warnings:             base.Warnings,
	}
	service.PinGeneration(generation)
	return service
}

func selectMigrationReplayProfile(
	environment MigrationCertificationEnvironment,
	row EffectiveProfile,
) (SelectedProfile, error) {
	if environment.replayService == nil || environment.replayGeneration == nil {
		return SelectedProfile{}, errors.New("isolated certification replay service was not prepared")
	}
	service := deriveMigrationReplayService(
		environment.replayService, row.Name, *environment.replayGeneration)
	return service.selectProfileRow(environment.Runtime, row)
}

func validateMigrationBenchmarkNonInferiority(report ProjectBenchmarkReport) error {
	byName := map[string]ProjectProfileBenchmark{}
	for _, profile := range report.Profiles {
		byName[profile.ProfileName] = profile
	}
	lexical, ok := byName["lexical-graph-v1"]
	if !ok || !lexical.NonInferiorToLexical {
		return errors.New("benchmark lacks its exact lexical non-inferiority baseline")
	}
	baseline := lexical.MetricsBySplit["holdout"]
	passed := report.Complete && len(report.UnsupportedProfiles) == 0
	for _, profile := range report.Profiles {
		passed = passed && profile.HardGatesPassed
		want := true
		if profile.ProfileName != "lexical-graph-v1" {
			metrics := profile.MetricsBySplit["holdout"]
			want = metrics.RecallAtK >= baseline.RecallAtK &&
				metrics.NDCG+0.02 >= baseline.NDCG &&
				metrics.CompleteEvidenceCoverage >= baseline.CompleteEvidenceCoverage &&
				metrics.CitationRecall >= baseline.CitationRecall &&
				metrics.AuthorityViolations <= baseline.AuthorityViolations
		}
		if profile.NonInferiorToLexical != want {
			return fmt.Errorf("profile %s non-inferiority was not recomputed", profile.ProfileName)
		}
		passed = passed && want
	}
	if report.Passed != passed {
		return errors.New("benchmark overall pass is not derived from exact profile gates")
	}
	return nil
}

func validateMigrationCaseOutcomes(
	outcomes []CaseOutcome,
	cases []EvalCase,
	requestedBudget int,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) (QualityMetrics, error) {
	if len(outcomes) != len(cases) {
		return QualityMetrics{}, errors.New("case rows do not cover the exact evaluation corpus")
	}
	for index, eval := range cases {
		budget := requestedBudget
		if budget == 0 {
			budget = eval.TokenBudget
		}
		if err := validateMigrationCaseOutcome(
			outcomes[index], eval, budget, environment,
		); err != nil {
			return QualityMetrics{}, fmt.Errorf("case %s: %w", eval.ID, err)
		}
		replayed, err := replayMigrationCaseOutcome(
			environment, selected, eval, budget)
		if err != nil {
			return QualityMetrics{}, fmt.Errorf("case %s independent replay: %w", eval.ID, err)
		}
		reported := outcomes[index]
		reported.LatencyMillis = 0
		if !reflect.DeepEqual(reported, replayed) {
			return QualityMetrics{}, fmt.Errorf(
				"case %s does not equal two-query current-generation replay", eval.ID)
		}
	}
	return calculateMetrics(outcomes, cases), nil
}

func replayMigrationCaseOutcome(
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
	eval EvalCase,
	budget int,
) (CaseOutcome, error) {
	if environment.replayGeneration == nil {
		return CaseOutcome{}, errors.New("isolated passage replay generation was not prepared")
	}
	key := selected.EffectiveIdentity + "\x00" + fmt.Sprintf("%d", budget) + "\x00" + eval.ID
	if cached, ok := environment.caseReplay[key]; ok {
		return cached, nil
	}
	retriever := Retriever{
		Boundary:   environment.replayService.Boundary,
		Generation: *environment.replayGeneration,
		Profile:    selected,
	}
	outcome, err := runEvaluationCase(
		context.Background(), eval, budget, environment.ProjectRoot, retriever.Search)
	if err != nil {
		return CaseOutcome{}, err
	}
	outcome.LatencyMillis = 0
	environment.caseReplay[key] = outcome
	return outcome, nil
}

func validateMigrationCaseOutcome(
	outcome CaseOutcome,
	eval EvalCase,
	budget int,
	environment MigrationCertificationEnvironment,
) error {
	if outcome.CaseID != eval.ID || outcome.Split != eval.Split ||
		outcome.Topic != eval.Topic || len(outcome.Paths) != len(outcome.Tiers) ||
		len(outcome.Paths) != len(outcome.ChunkIDs) ||
		len(outcome.Paths) != len(outcome.ContentHashes) ||
		outcome.EstimatedTokens < 0 || outcome.ReturnedTokens != outcome.EstimatedTokens ||
		outcome.RelevantTokens < 0 || outcome.RelevantTokens > outcome.ReturnedTokens ||
		outcome.DuplicateTokens < 0 || outcome.DuplicateTokens > outcome.ReturnedTokens ||
		outcome.StaleResults != 0 || outcome.LatencyMillis < 0 {
		return errors.New("case identity, cardinality, token, or staleness fields are invalid")
	}
	seenPaths := map[string]bool{}
	seenChunks := map[string]bool{}
	relevantSeen := map[string]bool{}
	// Mirror runEvaluationCase exactly: it appends onto nil slices, so a case
	// with no relevant results carries null relevance fields. Initializing
	// these as empty non-nil slices made reflect.DeepEqual reject every
	// honest quality-missing case as self-authored - the same nil-versus-
	// empty representation hazard as the finding-suite digests.
	var relevantPaths []string
	var relevantRanks []int
	hardNegatives := map[string]bool{}
	citations := map[string]bool{}
	authoritySafe := true
	citationMetadataSafe := true
	for index, path := range outcome.Paths {
		if strings.TrimSpace(path) == "" || strings.TrimSpace(outcome.Tiers[index]) == "" ||
			strings.TrimSpace(outcome.ChunkIDs[index]) == "" ||
			!hexDigestRE.MatchString(outcome.ContentHashes[index]) {
			citationMetadataSafe = false
		}
		chunk, current := environment.chunksByID[outcome.ChunkIDs[index]]
		if !current || seenChunks[outcome.ChunkIDs[index]] ||
			chunk.Path != path || chunk.Tier != outcome.Tiers[index] ||
			chunk.ContentHash != outcome.ContentHashes[index] {
			return errors.New(
				"returned path, tier, chunk id, and content hash tuple is not an exact unique current corpus chunk")
		}
		seenChunks[outcome.ChunkIDs[index]] = true
		seenPaths[path] = true
		if contains(eval.ExpectedPaths, path) && !relevantSeen[path] {
			relevantSeen[path] = true
			relevantPaths = append(relevantPaths, path)
			relevantRanks = append(relevantRanks, index+1)
		}
		if contains(eval.HardNegativePaths, path) {
			hardNegatives[path] = true
		}
		if contains(eval.ExpectedCitations, path) {
			citations[path] = true
		}
		if contains(eval.ForbiddenTiers, outcome.Tiers[index]) {
			authoritySafe = false
		}
	}
	completeEvidence := true
	for _, required := range eval.MinimumEvidencePaths {
		if !seenPaths[required] {
			completeEvidence = false
		}
	}
	expectedFound := false
	abstentionCorrect := false
	if *eval.Answerable {
		expectedFound = len(relevantPaths) > 0 && completeEvidence
		abstentionCorrect = len(outcome.Paths) > 0
	} else {
		expectedFound = len(outcome.Paths) == 0
		completeEvidence = len(outcome.Paths) == 0
		abstentionCorrect = len(outcome.Paths) == 0
	}
	citationSafe := citationMetadataSafe
	for _, expected := range eval.ExpectedCitations {
		if !citations[expected] {
			citationSafe = false
		}
	}
	corpusMatched := false
	if len(eval.EvidencePins) > 0 {
		corpusMatched = evidencePinsIntact(environment.ProjectRoot, eval.EvidencePins)
	} else {
		corpusMatched = eval.CorpusSnapshot == "fixture:packaged-conformance-v1" ||
			eval.CorpusSnapshot == environment.CorpusFingerprint
	}
	budgetSafe := outcome.EstimatedTokens <= budget
	qualityApplicable := budget >= eval.TokenBudget
	safetyPassed := authoritySafe && citationMetadataSafe && corpusMatched &&
		budgetSafe && outcome.ReplayIdentical && outcome.StaleResults == 0
	qualityPassed := !qualityApplicable ||
		(expectedFound && completeEvidence && citationSafe && abstentionCorrect &&
			len(hardNegatives) == 0)
	if outcome.ReturnedUniquePaths != len(seenPaths) ||
		!reflect.DeepEqual(outcome.RelevantPaths, relevantPaths) ||
		!reflect.DeepEqual(outcome.RelevantRanks, relevantRanks) ||
		!reflect.DeepEqual(outcome.HardNegativeHits, mapKeysSorted(hardNegatives)) ||
		!reflect.DeepEqual(outcome.ExpectedCitationsFound, mapKeysSorted(citations)) ||
		outcome.ExpectedFound != expectedFound || outcome.CompleteEvidence != completeEvidence ||
		outcome.AuthoritySafe != authoritySafe ||
		outcome.CitationMetadataSafe != citationMetadataSafe ||
		outcome.CitationSafe != citationSafe || outcome.CorpusMatched != corpusMatched ||
		outcome.AbstentionCorrect != abstentionCorrect || outcome.BudgetSafe != budgetSafe ||
		outcome.MinimumTokenBudget != eval.TokenBudget ||
		outcome.QualityGateApplicable != qualityApplicable ||
		outcome.SafetyPassed != safetyPassed || outcome.QualityPassed != qualityPassed ||
		outcome.GatePassed != (safetyPassed && qualityPassed) {
		return errors.New("case derived relevance, safety, quality, or gate fields are self-authored")
	}
	return nil
}

func migrationEvalRoles(cases []EvalCase) []string {
	roles := map[string]bool{}
	for _, eval := range cases {
		roles[eval.Role] = true
	}
	return mapKeysSorted(roles)
}

func validateMigrationContextOutcomes(
	outcomes []ContextPackOutcome,
	cases []EvalCase,
	requestedBudget int,
	effectiveProfile string,
	generationID string,
	row EffectiveProfile,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) (bool, error) {
	if len(outcomes) != len(cases) {
		return false, errors.New("context rows do not cover the exact evaluation corpus")
	}
	passed := len(outcomes) > 0
	for index, eval := range cases {
		outcome := outcomes[index]
		if len(outcome.Paths) != len(outcome.Tiers) {
			return false, fmt.Errorf(
				"context case %s path/tier cardinality is invalid", eval.ID)
		}
		budget := requestedBudget
		if budget == 0 {
			budget = eval.TokenBudget
		}
		roleCeiling := environment.Settings.Budgets.ManagerContextTokens
		if eval.Role == "drafter" {
			roleCeiling = environment.Settings.Budgets.DrafterContextTokens
		}
		effectiveBudget := budget
		if effectiveBudget > roleCeiling {
			effectiveBudget = roleCeiling
		}
		maxCards := environment.Bootstrap.Context.MaxCards
		if maxCards < 1 {
			maxCards = selected.Effective.Packing.MaxPassages
		}
		required := SortedUnique(append(
			append([]string(nil), eval.MinimumEvidencePaths...),
			eval.ExpectedCitations...,
		))
		foundSet := map[string]bool{}
		for pathIndex, path := range outcome.Paths {
			// A pack honestly serves multiple chunks of one document - a
			// ranked finding card plus its provenance passages all carry the
			// same source path - so repetition is legitimate. Each entry must
			// still name an exact current corpus document at its exact tier.
			document, current := environment.documentsByPath[path]
			if !current || document.Tier != outcome.Tiers[pathIndex] {
				return false, fmt.Errorf(
					"context case %s path/tier is not an exact current corpus document", eval.ID)
			}
			foundSet[path] = true
		}
		found := []string{}
		for _, path := range required {
			if foundSet[path] {
				found = append(found, path)
			}
		}
		minimumBudget := eval.TokenBudget
		if minimumBudget < MinimumContextPackEvidenceBudget {
			minimumBudget = MinimumContextPackEvidenceBudget
		}
		qualityApplicable := budget >= minimumBudget
		requiredPresent := len(found) == len(required)
		expectedFound := false
		abstention := false
		if *eval.Answerable {
			for _, path := range outcome.Paths {
				if contains(eval.ExpectedPaths, path) {
					expectedFound = true
					break
				}
			}
			abstention = outcome.CardCount > 0
		} else {
			expectedFound = outcome.CardCount == 0
			abstention = outcome.CardCount == 0
		}
		allowedSafe := true
		for _, tier := range outcome.Tiers {
			if !contains(eval.AllowedTiers, tier) && tier != "state" {
				allowedSafe = false
			}
		}
		roleSafe := outcome.EffectiveTokenBudget == effectiveBudget &&
			outcome.EffectiveTokenBudget <= roleCeiling
		cardSafe := outcome.CardCount <= maxCards
		byteSafe := outcome.SerializedBytes <= selected.Effective.Packing.MaxBytes
		budgetSafe := outcome.EstimatedTokens <= outcome.EffectiveTokenBudget &&
			outcome.EffectiveTokenBudget <= effectiveBudget
		profilePinned := outcome.EffectiveProfile == effectiveProfile
		generationPinned := outcome.Generation == generationID
		safety := roleSafe && allowedSafe && cardSafe && byteSafe &&
			outcome.TokenAccountingSafe && budgetSafe && generationPinned &&
			profilePinned && outcome.VerificationPassed && outcome.ReplayIdentical
		quality := !qualityApplicable ||
			(requiredPresent && expectedFound && abstention)
		mismatches := []string{}
		check := func(name string, ok bool) {
			if !ok {
				mismatches = append(mismatches, name)
			}
		}
		check("caseId", outcome.CaseID == eval.ID)
		check("split", outcome.Split == eval.Split)
		check("topic", outcome.Topic == eval.Topic)
		check("role", outcome.Role == eval.Role)
		check("requestedTokenBudget", outcome.RequestedTokenBudget == budget)
		check("roleTokenCeiling", outcome.RoleTokenCeiling == roleCeiling)
		check("maxCards", outcome.MaxCards == maxCards)
		// The outcome was produced through the finalized selected profile,
		// whose packing is clamped to the project budgets; the raw packaged
		// row would falsely fail projects with tighter converted budgets.
		check("maxBytes", outcome.MaxBytes == selected.Effective.Packing.MaxBytes)
		check("cardCount", outcome.CardCount == len(outcome.Paths) &&
			outcome.CardCount == len(outcome.Tiers))
		check("packId", validMigrationContextPackIdentity(outcome.PackID))
		check("digest", digestRE.MatchString(outcome.Digest))
		check("serializedBytes", outcome.SerializedBytes >= 0)
		check("estimatedTokens", outcome.EstimatedTokens >= 0)
		check("error", strings.TrimSpace(outcome.Error) == "")
		check("requiredPaths", reflect.DeepEqual(outcome.RequiredPaths, required))
		check("requiredPathsFound", reflect.DeepEqual(outcome.RequiredPathsFound, found))
		check("minimumTokenBudget", outcome.MinimumTokenBudget == minimumBudget)
		check("qualityGateApplicable", outcome.QualityGateApplicable == qualityApplicable)
		check("requiredEvidencePresent", outcome.RequiredEvidencePresent == requiredPresent)
		check("expectedEvidenceFound", outcome.ExpectedEvidenceFound == expectedFound)
		check("abstentionCorrect", outcome.AbstentionCorrect == abstention)
		check("allowedTiersSafe", outcome.AllowedTiersSafe == allowedSafe)
		check("roleCeilingSafe", outcome.RoleCeilingSafe == roleSafe)
		check("cardCapSafe", outcome.CardCapSafe == cardSafe)
		check("byteCapSafe", outcome.ByteCapSafe == byteSafe)
		check("budgetSafe", outcome.BudgetSafe == budgetSafe)
		check("profilePinned", outcome.ProfilePinned == profilePinned)
		check("generationPinned", outcome.GenerationPinned == generationPinned)
		check("safetyPassed", outcome.SafetyPassed == safety)
		check("qualityPassed", outcome.QualityPassed == quality)
		check("passed", outcome.Passed == (safety && quality))
		if len(mismatches) > 0 {
			return false, fmt.Errorf(
				"context case %s fields are not reproducible: %s",
				eval.ID, strings.Join(mismatches, ", "))
		}
		var retained ContextPack
		if len(outcome.Pack) == 0 || decodeStrict(outcome.Pack, &retained) != nil {
			return false, fmt.Errorf("context case %s omits its exact retained pack", eval.ID)
		}
		retainedBody, err := json.Marshal(retained)
		if err != nil || len(retainedBody) != outcome.SerializedBytes ||
			EstimateTokens(string(retainedBody)) != outcome.EstimatedTokens ||
			retained.PackID != outcome.PackID || retained.Digest != outcome.Digest ||
			retained.Generation.ID != outcome.Generation ||
			retained.EffectiveProfile != outcome.EffectiveProfile {
			return false, fmt.Errorf(
				"context case %s retained pack bytes do not derive its row", eval.ID)
		}
		if _, err := VerifyContextPackValueExpected(
			retained, outcome.Digest, outcome.PackID); err != nil {
			return false, fmt.Errorf("context case %s retained pack: %w", eval.ID, err)
		}
		replayed, err := replayMigrationContextOutcome(
			environment, selected, eval, budget)
		if err != nil {
			return false, fmt.Errorf("context case %s independent replay: %w", eval.ID, err)
		}
		var replayedPack ContextPack
		if decodeStrict(replayed.Pack, &replayedPack) != nil ||
			!reflect.DeepEqual(retained, replayedPack) {
			return false, fmt.Errorf(
				"context case %s retained pack differs from current recompilation", eval.ID)
		}
		reportedComparable, replayedComparable := outcome, replayed
		reportedComparable.Pack, replayedComparable.Pack = nil, nil
		if !reflect.DeepEqual(reportedComparable, replayedComparable) {
			return false, fmt.Errorf(
				"context case %s row differs from current recompilation", eval.ID)
		}
		passed = passed && outcome.Passed
	}
	return passed && contains(migrationEvalRoles(cases), "manager") &&
		contains(migrationEvalRoles(cases), "drafter"), nil
}

func replayMigrationContextOutcome(
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
	eval EvalCase,
	budget int,
) (ContextPackOutcome, error) {
	if environment.replayGeneration == nil || environment.replayService == nil {
		return ContextPackOutcome{}, errors.New("isolated context replay service was not prepared")
	}
	key := selected.EffectiveIdentity + "\x00" + fmt.Sprintf("%d", budget) + "\x00" + eval.ID
	if cached, ok := environment.contextReplay[key]; ok {
		return cached, nil
	}
	service := deriveMigrationReplayService(
		environment.replayService, selected.Effective.Name, *environment.replayGeneration)
	outcome := runContextPackCase(
		context.Background(), service, selected, environment.Settings, eval, budget)
	if strings.TrimSpace(outcome.Error) != "" {
		return ContextPackOutcome{}, errors.New(outcome.Error)
	}
	environment.contextReplay[key] = outcome
	return outcome, nil
}

func validMigrationContextPackIdentity(value string) bool {
	if !strings.HasPrefix(value, "context-") ||
		len(value) != len("context-")+20 {
		return false
	}
	return hexDigestRE.MatchString(
		strings.Repeat("0", 44) + strings.TrimPrefix(value, "context-"))
}

func validateMigrationFindingEvaluations(
	reports []FindingAblationReport,
	suites []FindingEvalSuite,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) (bool, error) {
	if len(reports) != len(suites) {
		return false, errors.New("finding evaluations do not cover the exact finding suites")
	}
	passed := true
	for index, suite := range suites {
		report := reports[index]
		expectedReport, err := replayMigrationFindingSuite(environment, selected, suite)
		if err != nil {
			return false, fmt.Errorf("finding suite %s independent replay: %w", suite.ID, err)
		}
		if !reflect.DeepEqual(report, expectedReport) {
			return false, fmt.Errorf(
				"finding suite %s does not equal the independent current-corpus replay", suite.ID)
		}
		if report.SchemaVersion != 1 || report.SuiteID != suite.ID ||
			report.SuiteDigest != suite.Digest || report.CorpusSnapshot != suite.CorpusSnapshot ||
			!report.ArchiveGateDiagnosticOnly {
			return false, fmt.Errorf("finding suite %s identity is invalid", suite.ID)
		}
		if err := validateMigrationFindingRows(
			report.Cases, suite.Cases, environment, selected); err != nil {
			return false, fmt.Errorf("finding suite %s: %w", suite.ID, err)
		}
		if err := validateMigrationFindingReportAggregates(report, suite.Cases); err != nil {
			return false, fmt.Errorf("finding suite %s: %w", suite.ID, err)
		}
		expected := report.Digest
		report.Digest = ""
		digest, err := CanonicalDigest(report)
		if err != nil || expected != digest {
			return false, fmt.Errorf("finding suite %s report digest is invalid", suite.ID)
		}
		passed = passed && findingEvaluationPassed(reports[index])
	}
	return passed, nil
}

func replayMigrationFindingSuite(
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
	suite FindingEvalSuite,
) (FindingAblationReport, error) {
	if environment.replayGeneration == nil {
		return FindingAblationReport{}, errors.New(
			"isolated finding replay generation was not prepared")
	}
	boundary, err := NewBoundary(environment.ProjectRoot)
	if err != nil {
		return FindingAblationReport{}, err
	}
	return EvaluateFindingSuite(context.Background(), Retriever{
		Boundary: boundary, Generation: *environment.replayGeneration,
		Profile: selected,
	}, suite)
}

func validateMigrationFindingRows(
	outcomes []FindingCaseOutcome,
	cases []FindingEvalCase,
	environment MigrationCertificationEnvironment,
	selected SelectedProfile,
) error {
	if environment.replayGeneration == nil {
		return errors.New("isolated finding replay generation was not prepared")
	}
	boundary, err := NewBoundary(environment.ProjectRoot)
	if err != nil {
		return err
	}
	expected, err := EvaluateFindingRetriever(context.Background(), Retriever{
		Boundary: boundary, Generation: *environment.replayGeneration,
		Profile: selected,
	}, cases)
	if err != nil {
		return fmt.Errorf("independent current-corpus finding replay: %w", err)
	}
	if !reflect.DeepEqual(outcomes, expected.Cases) {
		return errors.New(
			"finding rows, handles, metadata, lane evidence, status, or token costs diverge from independent replay")
	}
	if len(outcomes) != len(cases) {
		return errors.New("finding rows do not cover the exact case set")
	}
	for index, eval := range cases {
		outcome := outcomes[index]
		if outcome.CaseID != eval.ID || outcome.Role != eval.Role ||
			outcome.Topic != eval.Topic || outcome.Split != eval.Split ||
			outcome.QueryClass != eval.QueryClass ||
			len(outcome.FindingIDs) != len(SortedUnique(outcome.FindingIDs)) ||
			len(outcome.RawPaths) != len(SortedUnique(outcome.RawPaths)) ||
			len(outcome.HardNegativeHits) != len(SortedUnique(outcome.HardNegativeHits)) ||
			len(outcome.EvidenceHandlesFound) != len(SortedUnique(outcome.EvidenceHandlesFound)) ||
			outcome.NormalizedTokens < 0 || outcome.RawTokens < 0 ||
			outcome.NormalizedEvidenceExpansionTokens < 0 ||
			outcome.RawDocumentExpansionTokens < 0 {
			return fmt.Errorf("finding case %s identity or raw rows are invalid", eval.ID)
		}
		wantHard := []string{}
		for _, id := range outcome.FindingIDs {
			if contains(eval.HardNegativeFindingIDs, id) {
				wantHard = append(wantHard, id)
			}
		}
		if !reflect.DeepEqual(outcome.HardNegativeHits, SortedUnique(wantHard)) {
			return fmt.Errorf("finding case %s hard-negative rows are fabricated", eval.ID)
		}
		wantRanks := []int{}
		for rank, id := range outcome.CardIDs {
			if contains(eval.ExpectedFindingIDs, id) {
				wantRanks = append(wantRanks, rank+1)
			}
		}
		if !reflect.DeepEqual(outcome.RelevantFindingRanks, wantRanks) {
			return fmt.Errorf("finding case %s relevant ranks are fabricated", eval.ID)
		}
	}
	return nil
}

func validateMigrationFindingReportAggregates(
	report FindingAblationReport,
	cases []FindingEvalCase,
) error {
	overall := findingMetrics(report.Cases, cases)
	if report.FindingRecall != overall.FindingRecall ||
		report.MeanReciprocalRank != overall.MeanReciprocalRank ||
		report.RawPathRecall != overall.RawPathRecall ||
		report.AbstentionAccuracy != overall.AbstentionAccuracy ||
		report.FindingHandleAccuracy != overall.FindingHandleAccuracy ||
		report.EvidenceHandleAccuracy != overall.EvidenceHandleAccuracy ||
		report.SourceClassAccuracy != overall.SourceClassAccuracy ||
		report.ReviewStateAccuracy != overall.ReviewStateAccuracy ||
		report.ValidityAccuracy != overall.ValidityAccuracy ||
		report.VocabularyDisjointRate != overall.VocabularyDisjointRate ||
		report.DeterministicReplayRate != overall.DeterministicReplayRate ||
		report.NormalizedMedianTokens != overall.NormalizedMedianTokens ||
		report.RawMedianTokens != overall.RawMedianTokens ||
		report.NormalizedMedianEvidenceExpansionTokens != overall.NormalizedMedianEvidenceExpansionTokens ||
		report.RawMedianDocumentExpansionTokens != overall.RawMedianDocumentExpansionTokens {
		return errors.New("finding aggregate metrics are not derived from exact case rows")
	}
	wantSplit := map[string]FindingEvaluationMetrics{}
	for _, split := range []string{"development", "holdout"} {
		outcomes, subset := filterFindingCases(report.Cases, cases, func(eval FindingEvalCase) bool {
			return eval.Split == split
		})
		wantSplit[split] = findingMetrics(outcomes, subset)
	}
	wantRole := map[string]FindingEvaluationMetrics{}
	for _, role := range []string{"manager", "drafter"} {
		outcomes, subset := filterFindingCases(report.Cases, cases, func(eval FindingEvalCase) bool {
			return eval.Role == role
		})
		wantRole[role] = findingMetrics(outcomes, subset)
	}
	laneHits, uniqueHits := map[string]int{}, map[string]int{}
	normalized, raw, normalizedExpansion, rawExpansion, hard, durable := 0, 0, 0, 0, 0, 0
	for _, outcome := range report.Cases {
		normalized += outcome.NormalizedTokens
		raw += outcome.RawTokens
		normalizedExpansion += outcome.NormalizedEvidenceExpansionTokens
		rawExpansion += outcome.RawDocumentExpansionTokens
		hard += len(outcome.HardNegativeHits)
		if outcome.DurabilityLabelsAccurate {
			durable++
		}
		for lane, count := range outcome.LaneRelevantHits {
			laneHits[lane] += count
		}
		for lane, count := range outcome.UniqueRelevantFirstHits {
			uniqueHits[lane] += count
		}
	}
	durability := 0.0
	if len(report.Cases) > 0 {
		durability = float64(durable) / float64(len(report.Cases))
	}
	if !reflect.DeepEqual(report.MetricsBySplit, wantSplit) ||
		!reflect.DeepEqual(report.MetricsByRole, wantRole) ||
		!reflect.DeepEqual(report.LaneRelevantHits, laneHits) ||
		!reflect.DeepEqual(report.UniqueRelevantFirstHits, uniqueHits) ||
		report.NormalizedTokens != normalized || report.RawTokens != raw ||
		report.NormalizedEvidenceExpansionTokens != normalizedExpansion ||
		report.RawDocumentExpansionTokens != rawExpansion ||
		report.HardNegativeHits != hard || report.DurabilityLabelAccuracy != durability {
		return errors.New("finding totals, slices, or durability metrics are not derived from case rows")
	}
	return nil
}

func validateMigrationCalibrationRecomputation(
	report CalibrationReport,
	primary ProjectProfileBenchmark,
	candidateProfile RetrievalProfile,
	environment MigrationCertificationEnvironment,
) error {
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return err
	}
	var selectedRow EffectiveProfile
	for _, row := range baseline.Profile.EffectiveProfiles {
		if row.Name == migrationBaselineEffectiveProfile {
			selectedRow = row
			break
		}
	}
	if selectedRow.Name == "" {
		return errors.New("embedded baseline lacks the migration effective profile")
	}
	selected, err := selectMigrationReplayProfile(environment, selectedRow)
	if err != nil {
		return err
	}
	if report.ActiveBefore != selected.EffectiveIdentity ||
		primary.EffectiveProfile != selected.EffectiveIdentity {
		return errors.New("calibration did not start from the exact current packaged baseline")
	}
	developmentCases, holdoutCases := splitEvalCases(environment.EvalCases)
	findingDevelopment, findingHoldout, _ := splitFindingEvalSuites(environment.FindingSuites)
	baselineMetrics, err := validateMigrationCaseOutcomes(
		report.BaselineHoldoutCases, holdoutCases, 0, environment, selected)
	if err != nil {
		return fmt.Errorf("calibration baseline holdout: %w", err)
	}
	baselineMetrics = ratchetBaseline(selectedRow.Benchmark, baselineMetrics)
	if !reflect.DeepEqual(report.BaselineHoldoutMetrics, baselineMetrics) {
		return errors.New("calibration baseline holdout metrics are not derived from exact rows")
	}
	var baselineFinding *FindingEvaluationMetrics
	if len(findingHoldout) > 0 {
		if err := validateMigrationFindingRows(
			report.BaselineFindingHoldoutCases, findingHoldout,
			environment, selected); err != nil {
			return fmt.Errorf("calibration baseline finding holdout: %w", err)
		}
		metrics := findingMetrics(report.BaselineFindingHoldoutCases, findingHoldout)
		baselineFinding = &metrics
		if report.BaselineFindingHoldout == nil ||
			!reflect.DeepEqual(*report.BaselineFindingHoldout, metrics) {
			return errors.New("calibration baseline finding metrics are not derived from exact rows")
		}
	} else if report.BaselineFindingHoldout != nil ||
		len(report.BaselineFindingHoldoutCases) != 0 {
		return errors.New("calibration invents a baseline finding holdout")
	}

	denseWeight, ok := selectedRow.Weights["dense"]
	if !ok {
		return errors.New("selected packaged profile lacks its fixed dense inventory weight")
	}
	expectedWeights := []map[string]int{}
	for _, exact := range []int{6, 8, 10} {
		for _, fts := range []int{4, 6, 8} {
			for _, graph := range []int{1, 2, 3} {
				expectedWeights = append(expectedWeights, map[string]int{
					"exact": exact, "fts": fts, "graph": graph, "dense": denseWeight,
				})
			}
		}
	}
	if len(report.Candidates) != 27 || len(report.Candidates) != len(expectedWeights) {
		return errors.New("calibration does not contain the exact 27-row fixed-dense grid")
	}
	recomputed := append([]CalibrationCandidate(nil), report.Candidates...)
	for index := range recomputed {
		candidate := &recomputed[index]
		if !reflect.DeepEqual(candidate.Weights, expectedWeights[index]) {
			return fmt.Errorf("calibration candidate %d is outside or reorders the exact grid", index)
		}
		row := cloneEffectiveProfile(selectedRow)
		row.Weights = cloneWeights(candidate.Weights)
		candidateSelected, err := selectMigrationReplayProfile(environment, row)
		if err != nil || candidate.Identity != candidateSelected.EffectiveIdentity {
			return fmt.Errorf("calibration candidate %d identity is fabricated", index)
		}
		developmentMetrics, err := validateMigrationCaseOutcomes(
			candidate.DevelopmentCases, developmentCases, 0, environment, candidateSelected)
		if err != nil {
			return fmt.Errorf("calibration candidate %d development: %w", index, err)
		}
		if !reflect.DeepEqual(candidate.DevelopmentMetrics, developmentMetrics) ||
			candidate.DevelopmentHit != relevantPathHits(candidate.DevelopmentCases) {
			return fmt.Errorf("calibration candidate %d development metrics are fabricated", index)
		}
		violations := developmentMetrics.AuthorityViolations +
			developmentMetrics.CitationViolations + developmentMetrics.HardNegativeHits
		hardPassed := hardMetricsPassed(developmentMetrics)
		if len(findingDevelopment) > 0 {
			if err := validateMigrationFindingRows(
				candidate.FindingDevelopmentCases, findingDevelopment,
				environment, candidateSelected); err != nil {
				return fmt.Errorf("calibration candidate %d finding development: %w", index, err)
			}
			metrics := findingMetrics(candidate.FindingDevelopmentCases, findingDevelopment)
			if candidate.FindingDevelopment == nil ||
				!reflect.DeepEqual(*candidate.FindingDevelopment, metrics) {
				return fmt.Errorf("calibration candidate %d finding development metrics are fabricated", index)
			}
			violations += metrics.HardNegativeHits
			hardPassed = hardPassed && findingCalibrationMetricsPassed(metrics)
		} else if candidate.FindingDevelopment != nil ||
			len(candidate.FindingDevelopmentCases) != 0 {
			return fmt.Errorf("calibration candidate %d invents finding development evidence", index)
		}
		candidate.DevelopmentMetrics = developmentMetrics
		candidate.DevelopmentHit = relevantPathHits(candidate.DevelopmentCases)
		candidate.Violations = violations
		candidate.HardGatesPassed = hardPassed
		candidate.Pareto = false
		candidate.NonInferiorToBaseline = false
		candidate.BenchmarkDigest = ""
	}
	frontierIndexes := paretoFrontierIndexes(recomputed)
	if len(frontierIndexes) == 0 {
		return errors.New("calibration exact development rows produce no Pareto frontier")
	}
	frontierSet := map[int]bool{}
	for _, index := range frontierIndexes {
		frontierSet[index] = true
	}
	for index := range recomputed {
		candidate := &recomputed[index]
		original := report.Candidates[index]
		if !frontierSet[index] {
			if len(original.HoldoutCases) != 0 || len(original.FindingHoldoutCases) != 0 ||
				original.FindingHoldout != nil || original.Pareto ||
				original.NonInferiorToBaseline || original.BenchmarkDigest != "" ||
				!reflect.DeepEqual(original.HoldoutMetrics, QualityMetrics{}) {
				return fmt.Errorf("non-finalist calibration candidate %d carries invented holdout evidence", index)
			}
			if original.Violations != candidate.Violations ||
				original.HardGatesPassed != candidate.HardGatesPassed {
				return fmt.Errorf("calibration candidate %d development gates are fabricated", index)
			}
			continue
		}
		row := cloneEffectiveProfile(selectedRow)
		row.Weights = cloneWeights(original.Weights)
		candidateSelected, selectErr := selectMigrationReplayProfile(environment, row)
		if selectErr != nil {
			return selectErr
		}
		holdoutMetrics, err := validateMigrationCaseOutcomes(
			original.HoldoutCases, holdoutCases, 0, environment, candidateSelected)
		if err != nil {
			return fmt.Errorf("calibration candidate %d holdout: %w", index, err)
		}
		candidate.HoldoutCases = original.HoldoutCases
		candidate.HoldoutHit = relevantPathHits(original.HoldoutCases)
		candidate.HoldoutMetrics = holdoutMetrics
		candidate.Pareto = true
		candidate.HardGatesPassed = candidate.HardGatesPassed && hardMetricsPassed(holdoutMetrics)
		candidate.NonInferiorToBaseline = calibrationNonInferior(
			holdoutMetrics, baselineMetrics)
		if len(findingHoldout) > 0 {
			if err := validateMigrationFindingRows(
				original.FindingHoldoutCases, findingHoldout,
				environment, candidateSelected); err != nil {
				return fmt.Errorf("calibration candidate %d finding holdout: %w", index, err)
			}
			metrics := findingMetrics(original.FindingHoldoutCases, findingHoldout)
			candidate.FindingHoldoutCases = original.FindingHoldoutCases
			candidate.FindingHoldout = &metrics
			candidate.Violations += metrics.HardNegativeHits
			candidate.HardGatesPassed = candidate.HardGatesPassed &&
				findingCalibrationMetricsPassed(metrics)
			candidate.NonInferiorToBaseline = candidate.NonInferiorToBaseline &&
				baselineFinding != nil && findingCalibrationNonInferior(metrics, *baselineFinding)
		}
		candidate.Violations += holdoutMetrics.AuthorityViolations +
			holdoutMetrics.CitationViolations + holdoutMetrics.HardNegativeHits
		candidate.BenchmarkDigest, err = calibrationBenchmarkDigest(
			candidate.Identity, report.EvalDigest, report.CorpusFingerprint,
			candidate.DevelopmentMetrics, candidate.HoldoutMetrics, baselineMetrics,
			candidate.FindingDevelopment, candidate.FindingHoldout, baselineFinding,
			report.FindingSuiteDigests,
		)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(original, *candidate) {
			return fmt.Errorf("calibration finalist %d metrics, gates, or digest are fabricated", index)
		}
	}
	frontier := []CalibrationCandidate{}
	for _, index := range frontierIndexes {
		if recomputed[index].HardGatesPassed && recomputed[index].NonInferiorToBaseline {
			frontier = append(frontier, recomputed[index])
		}
	}
	if len(frontier) == 0 {
		return errors.New("calibration has no recomputed passing non-inferior finalist")
	}
	sort.Slice(frontier, func(i, j int) bool {
		return calibrationCandidateLess(frontier[i], frontier[j])
	})
	if !reflect.DeepEqual(report.ParetoFrontier, frontier) ||
		!reflect.DeepEqual(report.Recommended, frontier[0]) {
		return errors.New("calibration Pareto frontier or recommendation ordering is fabricated")
	}
	return validateMigrationCandidateProfile(
		candidateProfile, report, selectedRow, frontier[0], baseline, environment)
}

func validateMigrationCandidateProfile(
	candidate RetrievalProfile,
	report CalibrationReport,
	selectedRow EffectiveProfile,
	recommended CalibrationCandidate,
	baseline MigrationProfileBaseline,
	environment MigrationCertificationEnvironment,
) error {
	identityParts := strings.SplitN(recommended.Identity, "@", 2)
	if len(identityParts) != 2 || !strings.HasPrefix(identityParts[1], "sha256:") {
		return errors.New("recommended calibration identity is invalid")
	}
	wantID := "project:candidate-" +
		strings.TrimPrefix(identityParts[1], "sha256:")[:16]
	if candidate.SchemaVersion != baseline.Profile.SchemaVersion ||
		candidate.ProfileID != wantID || candidate.BaseProfile != baseline.Profile.ProfileID ||
		candidate.Approval != nil || len(candidate.EffectiveProfiles) != len(baseline.Profile.EffectiveProfiles) {
		return errors.New("calibration candidate profile lineage or identity is invalid")
	}
	runtimeDigest, err := CanonicalDigest(environment.RuntimeContract)
	if err != nil {
		return err
	}
	for index, row := range candidate.EffectiveProfiles {
		packaged := baseline.Profile.EffectiveProfiles[index]
		if row.Name != packaged.Name {
			return errors.New("calibration candidate reorders packaged effective profiles")
		}
		if row.Name != selectedRow.Name {
			if !reflect.DeepEqual(row, packaged) {
				return fmt.Errorf("calibration candidate changes unselected profile %s", row.Name)
			}
			continue
		}
		if !reflect.DeepEqual(row.Weights, recommended.Weights) ||
			!reflect.DeepEqual(row.Lanes, packaged.Lanes) ||
			!reflect.DeepEqual(row.Requires, packaged.Requires) ||
			row.RRFK != packaged.RRFK || row.MaxPerDocument != packaged.MaxPerDocument ||
			!reflect.DeepEqual(row.Packing, packaged.Packing) ||
			row.Benchmark.Suite != "project-calibration-v1" ||
			row.Benchmark.Digest != recommended.BenchmarkDigest ||
			row.Benchmark.Status != "passed" ||
			row.Benchmark.EvalFingerprint != report.EvalDigest ||
			row.Benchmark.CorpusFingerprint != report.CorpusFingerprint ||
			row.Benchmark.ModelFingerprint != report.ModelFingerprint ||
			row.Benchmark.RuntimeFingerprint != runtimeDigest ||
			row.Benchmark.ParserVersion != environment.ParserVersion ||
			row.Benchmark.ChunkerVersion != environment.ChunkerVersion ||
			row.Benchmark.RatifiedHardNegativeHits == nil ||
			*row.Benchmark.RatifiedHardNegativeHits != recommended.HoldoutMetrics.HardNegativeHits ||
			row.Benchmark.RatifiedAbstentionAccuracy != recommended.HoldoutMetrics.AbstentionAccuracy {
			return errors.New("calibration candidate selected profile evidence is not reproducible")
		}
		when, parseErr := time.Parse(time.RFC3339, row.Benchmark.EvaluatedAt)
		if parseErr != nil || when.Location() != time.UTC {
			return errors.New("calibration candidate evaluatedAt is not explicit UTC RFC3339")
		}
	}
	return nil
}
