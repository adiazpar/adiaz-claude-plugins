package knowledge

import (
	"context"
	"database/sql"
	"sort"
)

// TierDisagreement is emitted only for an explicit cross-tier contradiction
// edge. The authority fields are intentionally part of the signal: accepted
// memory remains operational recall and cannot overrule closure-approved truth.
type TierDisagreement struct {
	Signal                string `json:"signal"`
	TruthPath             string `json:"truthPath"`
	MemoryPath            string `json:"memoryPath"`
	AuthorityTier         string `json:"authorityTier"`
	AdvisoryTier          string `json:"advisoryTier"`
	Relation              string `json:"relation"`
	RequiresManagerReview bool   `json:"requiresManagerReview"`
}

type TierDisagreementStatus struct {
	Count         int                `json:"count"`
	Signals       []TierDisagreement `json:"signals"`
	Omitted       int                `json:"omitted"`
	AuthorityRule string             `json:"authorityRule"`
}

func loadTierDisagreements(
	ctx context.Context,
	db *sql.DB,
	relevantPaths map[string]bool,
	limit int,
) (TierDisagreementStatus, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `SELECT source_document.path,source_document.tier,
		target_document.path,target_document.tier
		FROM edges relation
		JOIN chunks source_chunk ON source_chunk.id=relation.source_id
		JOIN documents source_document ON source_document.id=source_chunk.document_id
		JOIN chunks target_chunk ON target_chunk.id=relation.target_id
		JOIN documents target_document ON target_document.id=target_chunk.document_id
		WHERE relation.kind='contradicts'
		AND ((source_document.tier='truth' AND target_document.tier='memory')
		  OR (source_document.tier='memory' AND target_document.tier='truth'))
		ORDER BY source_document.path,target_document.path`)
	if err != nil {
		return TierDisagreementStatus{}, err
	}
	defer rows.Close()
	all := []TierDisagreement{}
	seen := map[string]bool{}
	for rows.Next() {
		var sourcePath, sourceTier, targetPath, targetTier string
		if err := rows.Scan(&sourcePath, &sourceTier, &targetPath, &targetTier); err != nil {
			return TierDisagreementStatus{}, err
		}
		truthPath, memoryPath := sourcePath, targetPath
		if sourceTier == "memory" {
			truthPath, memoryPath = targetPath, sourcePath
		}
		if relevantPaths != nil && !relevantPaths[truthPath] && !relevantPaths[memoryPath] {
			continue
		}
		key := truthPath + "\x00" + memoryPath
		if seen[key] {
			continue
		}
		seen[key] = true
		all = append(all, TierDisagreement{
			Signal: "truth-vs-memory", TruthPath: truthPath, MemoryPath: memoryPath,
			AuthorityTier: "truth", AdvisoryTier: "memory", Relation: "contradicts",
			RequiresManagerReview: true,
		})
	}
	if err := rows.Err(); err != nil {
		return TierDisagreementStatus{}, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].TruthPath != all[j].TruthPath {
			return all[i].TruthPath < all[j].TruthPath
		}
		return all[i].MemoryPath < all[j].MemoryPath
	})
	status := TierDisagreementStatus{
		Count: len(all), Signals: all, AuthorityRule: "truth-remains-authoritative-until-closure",
	}
	if len(status.Signals) > limit {
		status.Signals = status.Signals[:limit]
		status.Omitted = status.Count - limit
	}
	return status, nil
}

func filterTierDisagreements(results []SearchResult, signals []TierDisagreement) []TierDisagreement {
	paths := map[string]bool{}
	for _, result := range results {
		paths[result.Citation.Path] = true
	}
	filtered := make([]TierDisagreement, 0, len(signals))
	for _, signal := range signals {
		if paths[signal.TruthPath] || paths[signal.MemoryPath] {
			filtered = append(filtered, signal)
		}
	}
	return filtered
}
