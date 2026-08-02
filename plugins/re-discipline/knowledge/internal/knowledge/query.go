package knowledge

import (
	"context"
	"errors"
	"fmt"
)

// Query is the 0.8 finding-card retrieval entry point shared by MCP and CLI.
// Passage search remains an internal/admin benchmark primitive; public query
// never silently changes its result unit back to chunks.
func (service *Service) Query(ctx context.Context, options FindingQueryOptions) (FindingQueryResponse, error) {
	if service == nil {
		return FindingQueryResponse{}, errors.New("service is required")
	}
	settings := service.effectiveSettings()
	if options.TokenBudget == 0 {
		options.TokenBudget = 1200
	}
	if options.TokenBudget > settings.Budgets.SearchTokens {
		options.TokenBudget = settings.Budgets.SearchTokens
	}
	generation, _, selected, _, err := service.ensure(ctx)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	policy, err := service.configuredArchiveFallbackPolicy()
	if err != nil {
		return FindingQueryResponse{}, err
	}
	// Archive policy is project control-plane state. Public callers may request
	// an explicit raw expansion through IncludeRaw, but cannot supply or bypass
	// the receipt that decides whether raw is the default lane.
	options.ArchivePolicy = policy
	return (Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
		ArchiveTracker: service.archiveTracker,
	}).QueryFindingCards(ctx, options)
}

func (service *Service) configuredArchiveFallbackPolicy() (ArchiveFallbackPolicy, error) {
	settings := service.effectiveSettings()
	mode := settings.Archive.FallbackMode
	if mode == "" {
		mode = "default-fallback"
	}
	policy := ArchiveFallbackPolicy{Mode: mode}
	if mode != "opt-in" {
		return policy, nil
	}
	receipt, err := LoadNormalizedBeatsRawReceipt(
		service.Boundary, settings.Archive.NormalizedBeatsRawReceipt)
	if err != nil {
		return ArchiveFallbackPolicy{}, fmt.Errorf(
			"load configured normalized-beats-raw receipt: %w", err)
	}
	suites, err := service.loadProjectFindingEvalSuites()
	if err != nil {
		return ArchiveFallbackPolicy{}, fmt.Errorf(
			"load ratified finding evaluation suites: %w", err)
	}
	var boundSuite *FindingEvalSuite
	for index := range suites {
		if suites[index].ID == receipt.SuiteID && suites[index].Digest == receipt.SuiteDigest {
			boundSuite = &suites[index]
			break
		}
	}
	if boundSuite == nil {
		return ArchiveFallbackPolicy{}, errors.New(
			"configured normalized-beats-raw receipt does not match a ratified project finding suite")
	}
	if err := validateArchiveReceiptSuiteBinding(receipt, *boundSuite); err != nil {
		return ArchiveFallbackPolicy{}, err
	}
	policy.Receipt = &receipt
	return policy, nil
}

func validateArchiveReceiptSuiteBinding(
	receipt NormalizedBeatsRawReceipt,
	suite FindingEvalSuite,
) error {
	if suite.ID != receipt.SuiteID || suite.Digest != receipt.SuiteDigest ||
		suite.CorpusSnapshot != receipt.Binding.CorpusFingerprint {
		return errors.New(
			"normalized-beats-raw receipt suite does not bind the evaluated corpus")
	}
	type splitCounts struct {
		cases, manager, drafter, abstention int
	}
	counts := map[string]*splitCounts{
		"development": {}, "holdout": {},
	}
	for _, eval := range suite.Cases {
		row := counts[eval.Split]
		row.cases++
		if eval.Role == "manager" {
			row.manager++
		} else {
			row.drafter++
		}
		if !eval.Answerable {
			row.abstention++
		}
	}
	development, holdout := counts["development"], counts["holdout"]
	if receipt.CaseCount != len(suite.Cases) ||
		receipt.DevelopmentCases != development.cases ||
		receipt.DevelopmentManager != development.manager ||
		receipt.DevelopmentDrafter != development.drafter ||
		receipt.DevelopmentAbstention != development.abstention ||
		receipt.HoldoutCases != holdout.cases ||
		receipt.HoldoutManager != holdout.manager ||
		receipt.HoldoutDrafter != holdout.drafter ||
		receipt.HoldoutAbstention != holdout.abstention {
		return errors.New(
			"normalized-beats-raw receipt coverage does not match its ratified suite")
	}
	return nil
}
