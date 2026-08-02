package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type FindingQueryOptions struct {
	Query                string                `json:"query"`
	CampaignID           string                `json:"campaignId,omitempty"`
	AllowedSourceClasses []string              `json:"allowedSourceClasses,omitempty"`
	AllowedReviewStates  []string              `json:"allowedReviewStates,omitempty"`
	AllowedValidities    []string              `json:"allowedValidities,omitempty"`
	Limit                int                   `json:"limit,omitempty"`
	TokenBudget          int                   `json:"tokenBudget,omitempty"`
	IncludeRaw           bool                  `json:"includeRaw,omitempty"`
	RequestID            string                `json:"requestId,omitempty"`
	ArchivePolicy        ArchiveFallbackPolicy `json:"archivePolicy,omitempty"`
	// suppressRaw is an in-process measurement control. Public adapters cannot
	// set it; it lets the paired evaluator measure normalized cards without the
	// fallback lane contaminating recall or token cost.
	suppressRaw bool
}

type FindingCandidateTrace struct {
	FindingID    string         `json:"findingId"`
	LaneRanks    map[string]int `json:"laneRanks,omitempty"`
	FusionScore  int64          `json:"fusionScore,omitempty"`
	RerankScore  int64          `json:"rerankScore,omitempty"`
	FilteredBy   []string       `json:"filteredBy,omitempty"`
	RelationAdds []string       `json:"relationAdds,omitempty"`
}

type FindingQueryTrace struct {
	AnalyzerVersion    string                  `json:"analyzerVersion"`
	FindingFormat      string                  `json:"findingFormat"`
	Candidates         []FindingCandidateTrace `json:"candidates"`
	CandidateOmitted   int                     `json:"candidateOmitted,omitempty"`
	FilteredByReason   map[string]int          `json:"filteredByReason,omitempty"`
	RawFallbackDefault bool                    `json:"rawFallbackDefault"`
	RawFallbackServed  bool                    `json:"rawFallbackServed"`
	// Operational counters remain available to in-process callers but are
	// excluded from the deterministic query receipt and replay identity.
	ArchiveServes            []ArchiveServeEvent `json:"-"`
	NormalizationSuggestions []string            `json:"-"`
}

type FindingQueryResponse struct {
	Query           string            `json:"query"`
	Status          string            `json:"status"`
	Cards           []ContextCard     `json:"cards"`
	TokenBudget     int               `json:"tokenBudget"`
	EstimatedTokens int               `json:"estimatedTokens"`
	Omitted         int               `json:"omitted"`
	Trace           FindingQueryTrace `json:"trace"`
	Digest          string            `json:"digest"`
}

type findingCandidate struct {
	record            FindingRecord
	path              string
	sourceClass       string
	aliases           []string
	questions         []string
	terms             map[string]bool
	laneRanks         map[string]int
	fusion            int64
	rerank            int64
	why               []string
	relationAlerts    []string
	relationAdds      []string
	relationGroup     string
	strongestEvidence string
}

// QueryFindingCards is the 0.8 public finding-first retrieval surface.
// Passage search remains an internal benchmark and explicit provenance
// primitive; public adapters return these bounded cards.
func (retriever Retriever) QueryFindingCards(ctx context.Context, options FindingQueryOptions) (FindingQueryResponse, error) {
	options.Query = strings.TrimSpace(options.Query)
	if options.Query == "" || len([]byte(options.Query)) > 1000 {
		return FindingQueryResponse{}, errors.New("query must contain 1 to 1000 UTF-8 bytes")
	}
	if options.Limit == 0 {
		options.Limit = 5
	}
	if options.Limit < 1 || options.Limit > 5 {
		return FindingQueryResponse{}, errors.New("finding card limit must be between 1 and 5")
	}
	if options.TokenBudget == 0 {
		options.TokenBudget = 1200
	}
	if options.TokenBudget < 128 || options.TokenBudget > 8192 {
		return FindingQueryResponse{}, errors.New("token budget must be between 128 and 8192")
	}
	binding, err := retriever.archiveFallbackBinding()
	if err != nil {
		return FindingQueryResponse{}, err
	}
	if err := ValidateArchiveFallbackPolicy(options.ArchivePolicy, binding); err != nil && options.ArchivePolicy.Mode == "opt-in" {
		return FindingQueryResponse{}, err
	}
	rawDefault := options.ArchivePolicy.RawIsDefault(binding)
	trace := FindingQueryTrace{
		AnalyzerVersion: IdentifierAnalyzerVersion, FindingFormat: FindingFormatVersion,
		RawFallbackDefault: rawDefault, FilteredByReason: map[string]int{},
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(retriever.Generation.Database))
	if err != nil {
		return FindingQueryResponse{}, err
	}
	defer db.Close()

	candidates, filtered, err := loadFindingCandidates(ctx, db, options)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	queryTerms := IdentifierTerms(options.Query)
	rankFindingExact(options.Query, queryTerms, candidates)
	if err := rankFindingFTS(ctx, db, options.Query, candidates); err != nil {
		return FindingQueryResponse{}, err
	}
	pruneWeakFindingFTS(queryTerms, candidates)
	if laneEnabled(retriever.Profile.ActiveLanes, "dense") && len(candidates) > 0 {
		if err := retriever.rankFindingDense(ctx, db, options.Query, candidates); err != nil {
			return FindingQueryResponse{}, err
		}
	}
	fuseFindingCandidates(candidates, retriever.Profile.Effective.Weights, retriever.Profile.Effective.RRFK)
	ranked := sortedFindingCandidates(candidates)
	if laneEnabled(retriever.Profile.ActiveLanes, "rerank") {
		rerankFindingCandidates(options.Query, ranked, retriever.Profile.Effective.RerankDepth)
	}
	visibilityRanked, relationEligible, denseOnlySuppressed :=
		suppressDenseOnlyBehindLexical(ranked, candidates)
	visibleRanked, err := expandFindingRelations(
		ctx, db, visibilityRanked, relationEligible, options.Limit)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	if denseOnlySuppressed > 0 {
		trace.FilteredByReason["dense-only-behind-lexical"] += denseOnlySuppressed
	}

	for _, candidate := range filtered {
		for _, reason := range candidate.why {
			trace.FilteredByReason[reason]++
		}
	}
	traceCandidates, normalizedCandidateCount := boundedFindingTraceCandidates(ranked, visibleRanked, options.Limit)
	trace.CandidateOmitted = normalizedCandidateCount - len(traceCandidates)
	for _, candidate := range traceCandidates {
		trace.Candidates = append(trace.Candidates, FindingCandidateTrace{
			FindingID: candidate.record.ID, LaneRanks: cloneRanks(candidate.laneRanks),
			FusionScore: candidate.fusion, RerankScore: candidate.rerank,
			RelationAdds: append([]string(nil), candidate.relationAdds...),
		})
	}

	cards := []ContextCard{}
	for index := 0; index < len(visibleRanked); {
		if len(cards) >= options.Limit {
			break
		}
		end := index + 1
		if group := visibleRanked[index].relationGroup; group != "" {
			for end < len(visibleRanked) && visibleRanked[end].relationGroup == group {
				end++
			}
		}
		additions := make([]ContextCard, 0, end-index)
		for _, candidate := range visibleRanked[index:end] {
			additions = append(additions, findingContextCard(candidate))
		}
		index = end
		if len(cards)+len(additions) > options.Limit ||
			!cardsFitFindingBudget(options, cards, additions, trace, normalizedCandidateCount-(len(cards)+len(additions))) {
			continue
		}
		cards = append(cards, additions...)
	}
	normalizedCount := len(cards)
	serveRaw := !options.suppressRaw && (rawDefault || options.IncludeRaw)
	if serveRaw && len(cards) < options.Limit {
		rawCards, err := queryRawReportCards(ctx, db, options.Query, options.Limit-len(cards))
		if err != nil {
			return FindingQueryResponse{}, err
		}
		for _, raw := range rawCards {
			if !cardsFitFindingBudget(options, cards, []ContextCard{raw.card}, trace, normalizedCandidateCount-normalizedCount) {
				continue
			}
			cards = append(cards, raw.card)
			trace.RawFallbackServed = true
			if retriever.ArchiveTracker != nil {
				requestID := options.RequestID
				if requestID == "" {
					requestID = StableID("query", retriever.Generation.ID, options.Query)
				}
				event, err := retriever.ArchiveTracker.Record(raw.digest, requestID)
				if err != nil {
					return FindingQueryResponse{}, err
				}
				trace.ArchiveServes = append(trace.ArchiveServes, event)
				if event.NormalizationSuggested {
					trace.NormalizationSuggestions = append(trace.NormalizationSuggestions, event.ReportDigest)
				}
			}
		}
	}
	trace.NormalizationSuggestions = SortedUnique(trace.NormalizationSuggestions)
	response := FindingQueryResponse{
		Query: options.Query, Status: findingResponseStatus(cards),
		Cards: cards, TokenBudget: options.TokenBudget,
		Omitted: normalizedCandidateCount - normalizedCount, Trace: trace,
	}
	if response.Omitted < 0 {
		response.Omitted = 0
	}
	return finalizeFindingResponse(response)
}

func findingResponseStatus(cards []ContextCard) string {
	if len(cards) == 0 {
		return "abstained"
	}
	normalized := false
	for _, card := range cards {
		if card.CardType == "finding" {
			normalized = true
		}
		for _, alert := range card.RelationAlerts {
			if strings.HasPrefix(alert, "contradicts:") ||
				strings.HasPrefix(alert, "incoming-contradicts:") ||
				strings.HasPrefix(alert, "superseded-by:") {
				return "conflicted"
			}
		}
	}
	if !normalized {
		return "insufficient-evidence"
	}
	return "answered"
}

func (retriever Retriever) archiveFallbackBinding() (ArchiveFallbackBinding, error) {
	runtimeFingerprint, err := CanonicalDigest(retriever.Generation.Runtime)
	if err != nil {
		return ArchiveFallbackBinding{}, err
	}
	return ArchiveFallbackBinding{
		CorpusFingerprint: retriever.Generation.CorpusFingerprint,
		ProfileIdentity:   retriever.Profile.EffectiveIdentity, RuntimeFingerprint: runtimeFingerprint,
		FindingFormat: FindingFormatVersion, IdentifierAnalyzer: IdentifierAnalyzerVersion,
	}, nil
}

func defaultFindingFilter(values []string, defaults ...string) map[string]bool {
	if len(values) == 0 {
		values = defaults
	}
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func loadFindingCandidates(ctx context.Context, db *sql.DB, options FindingQueryOptions) (map[string]*findingCandidate, []*findingCandidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT f.id,f.campaign_id,f.kind,f.subject,f.claim,
		f.scope_json,f.evidence_grade,f.review_state,f.validity,f.projection,
		f.record_digest,f.body,f.source_class,d.path
		FROM findings f JOIN documents d ON d.id=f.document_id ORDER BY f.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	sourceClasses := defaultFindingFilter(options.AllowedSourceClasses, "truth", "campaign", "history")
	reviewStates := defaultFindingFilter(options.AllowedReviewStates, "manager-ratified")
	validities := defaultFindingFilter(options.AllowedValidities, "provisional", "current", "challenged", "historical")
	result := map[string]*findingCandidate{}
	filtered := []*findingCandidate{}
	for rows.Next() {
		candidate := &findingCandidate{laneRanks: map[string]int{}, terms: map[string]bool{}}
		var scopeJSON string
		if err := rows.Scan(
			&candidate.record.ID, &candidate.record.CampaignID, &candidate.record.Kind,
			&candidate.record.Subject, &candidate.record.Claim, &scopeJSON,
			&candidate.record.EvidenceGrade, &candidate.record.ReviewState,
			&candidate.record.Validity, &candidate.record.Projection, &candidate.record.Digest,
			&candidate.record.Body, &candidate.sourceClass, &candidate.path,
		); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal([]byte(scopeJSON), &candidate.record.Scope); err != nil {
			return nil, nil, fmt.Errorf("finding %s scope: %w", candidate.record.ID, err)
		}
		if !sourceClasses[candidate.sourceClass] {
			candidate.why = append(candidate.why, "source-class")
		}
		if !reviewStates[candidate.record.ReviewState] {
			candidate.why = append(candidate.why, "review-state")
		}
		if !validities[candidate.record.Validity] {
			candidate.why = append(candidate.why, "validity")
		}
		if options.CampaignID != "" && candidate.record.CampaignID != options.CampaignID {
			candidate.why = append(candidate.why, "campaign")
		}
		if len(candidate.why) > 0 {
			filtered = append(filtered, candidate)
			continue
		}
		queryRows, err := db.QueryContext(ctx, `SELECT kind,value FROM finding_queries WHERE finding_id=? ORDER BY kind,value`, candidate.record.ID)
		if err != nil {
			return nil, nil, err
		}
		for queryRows.Next() {
			var kind, value string
			if err := queryRows.Scan(&kind, &value); err != nil {
				queryRows.Close()
				return nil, nil, err
			}
			if kind == "alias" {
				candidate.aliases = append(candidate.aliases, value)
			} else if kind == "synthetic-question" {
				candidate.questions = append(candidate.questions, value)
			}
		}
		if err := queryRows.Close(); err != nil {
			return nil, nil, err
		}
		termRows, err := db.QueryContext(ctx, `SELECT DISTINCT term FROM finding_terms WHERE finding_id=? ORDER BY term`, candidate.record.ID)
		if err != nil {
			return nil, nil, err
		}
		for termRows.Next() {
			var term string
			if err := termRows.Scan(&term); err != nil {
				termRows.Close()
				return nil, nil, err
			}
			candidate.terms[term] = true
		}
		if err := termRows.Close(); err != nil {
			return nil, nil, err
		}
		_ = db.QueryRowContext(ctx, `SELECT handle FROM finding_evidence WHERE finding_id=? ORDER BY handle LIMIT 1`, candidate.record.ID).Scan(&candidate.strongestEvidence)
		result[candidate.record.ID] = candidate
	}
	return result, filtered, rows.Err()
}

func rankFindingExact(query string, queryTerms []string, candidates map[string]*findingCandidate) {
	type scored struct {
		id    string
		score int
	}
	rows := []scored{}
	queryLower := strings.ToLower(query)
	for id, candidate := range candidates {
		score := 0
		if strings.EqualFold(query, id) {
			score += 100000
		}
		fields := append([]string{candidate.record.Subject, candidate.record.Claim}, candidate.aliases...)
		fields = append(fields, candidate.questions...)
		for _, field := range fields {
			fieldLower := strings.ToLower(field)
			if fieldLower == queryLower {
				score += 20000
			} else if strings.Contains(fieldLower, queryLower) {
				score += 4000
			}
		}
		matched := 0
		for _, term := range queryTerms {
			if candidate.terms[term] {
				matched++
			}
		}
		score += matched * 500
		if score > 0 {
			candidate.why = append(candidate.why, fmt.Sprintf("exact:%d-terms", matched))
			rows = append(rows, scored{id, score})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].id < rows[j].id
	})
	for index, row := range rows {
		candidates[row.id].laneRanks["exact"] = index + 1
	}
}

func rankFindingFTS(ctx context.Context, db *sql.DB, query string, candidates map[string]*findingCandidate) error {
	tokens := IdentifierTerms(query)
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) > 24 {
		tokens = tokens[:24]
	}
	parts := make([]string, len(tokens))
	for index, token := range tokens {
		parts[index] = `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
	}
	rows, err := db.QueryContext(ctx, `SELECT finding_id,bm25(finding_fts,0.0,5.0,4.0,2.0,3.0,1.0,0.5)
		FROM finding_fts WHERE finding_fts MATCH ? ORDER BY 2 ASC,finding_id ASC`, strings.Join(parts, " OR "))
	if err != nil {
		return err
	}
	defer rows.Close()
	rank := 0
	for rows.Next() {
		var id string
		var ignored float64
		if err := rows.Scan(&id, &ignored); err != nil {
			return err
		}
		candidate := candidates[id]
		if candidate == nil {
			continue
		}
		rank++
		candidate.laneRanks["fts"] = rank
		candidate.why = append(candidate.why, "fts")
	}
	return rows.Err()
}

// An OR-only FTS query can match a generic body heading such as "Evidence"
// even when no compact card field supports the query. A body-only FTS hit is
// useful for explicit provenance search, not sufficient to return a finding
// card. Require at least one analyzed card key unless another lane rescued it.
func pruneWeakFindingFTS(queryTerms []string, candidates map[string]*findingCandidate) {
	for _, candidate := range candidates {
		if candidate.laneRanks["fts"] == 0 || candidate.laneRanks["exact"] > 0 ||
			candidate.laneRanks["dense"] > 0 {
			continue
		}
		matched := false
		for _, term := range queryTerms {
			if candidate.terms[term] {
				matched = true
				break
			}
		}
		if !matched {
			delete(candidate.laneRanks, "fts")
			filteredWhy := candidate.why[:0]
			for _, why := range candidate.why {
				if why != "fts" {
					filteredWhy = append(filteredWhy, why)
				}
			}
			candidate.why = filteredWhy
		}
	}
}

func (retriever Retriever) rankFindingDense(ctx context.Context, db *sql.DB, query string, candidates map[string]*findingCandidate) error {
	if retriever.Profile.Effective.Requires.Embedding == nil {
		return errors.New("effective profile enables finding dense lane without a pinned embedding model")
	}
	var embedding ModelIdentity
	for _, model := range retriever.Profile.Models {
		if model.ID == *retriever.Profile.Effective.Requires.Embedding {
			embedding = model
			break
		}
	}
	if embedding.ID == "" {
		return errors.New("effective finding embedding model identity is unavailable")
	}
	paths := []string{}
	tiers := []string{}
	byPath := map[string]*findingCandidate{}
	for _, candidate := range candidates {
		paths = append(paths, candidate.path)
		tiers = append(tiers, candidate.sourceClass)
		byPath[candidate.path] = candidate
	}
	tiers = SortedUnique(tiers)
	rows, err := rankDense(ctx, db, query, tiers, paths, embedding)
	if err != nil {
		return err
	}
	rank := 0
	seen := map[string]bool{}
	for _, row := range rows {
		candidate := byPath[row.Chunk.Path]
		if candidate == nil || seen[candidate.record.ID] {
			continue
		}
		seen[candidate.record.ID] = true
		rank++
		candidate.laneRanks["dense"] = rank
		candidate.why = append(candidate.why, "dense")
	}
	return nil
}

func laneEnabled(lanes []string, target string) bool {
	if len(lanes) == 0 {
		return target == "exact" || target == "fts"
	}
	for _, lane := range lanes {
		if lane == target {
			return true
		}
	}
	return false
}

func fuseFindingCandidates(candidates map[string]*findingCandidate, weights map[string]int, k int) {
	if k < 1 {
		k = 60
	}
	defaults := map[string]int{"exact": 8, "fts": 6, "dense": 4}
	for _, candidate := range candidates {
		candidate.fusion = 0
		for lane, rank := range candidate.laneRanks {
			weight := weights[lane]
			if weight == 0 {
				weight = defaults[lane]
			}
			candidate.fusion += int64(weight) * 1_000_000_000 / int64(k+rank)
		}
	}
}

func sortedFindingCandidates(candidates map[string]*findingCandidate) []*findingCandidate {
	result := []*findingCandidate{}
	for _, candidate := range candidates {
		if len(candidate.laneRanks) > 0 {
			result = append(result, candidate)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].fusion != result[j].fusion {
			return result[i].fusion > result[j].fusion
		}
		return result[i].record.ID < result[j].record.ID
	})
	return result
}

// suppressDenseOnlyBehindLexical is a visibility guard, not a ranking-lane
// deletion. Once exact or FTS has found any admissible finding, a candidate
// supported only by dense similarity cannot trail that lexical evidence as an
// additional answer. The original ranked slice remains intact for bounded
// trace and ablation accounting. In a true lexical vacuum dense-only
// candidates remain eligible as semantic rescue results.
func suppressDenseOnlyBehindLexical(
	ranked []*findingCandidate,
	eligible map[string]*findingCandidate,
) ([]*findingCandidate, map[string]*findingCandidate, int) {
	hasLexical := false
	for _, candidate := range eligible {
		if candidate != nil &&
			(candidate.laneRanks["exact"] > 0 || candidate.laneRanks["fts"] > 0) {
			hasLexical = true
			break
		}
	}
	if !hasLexical {
		return ranked, eligible, 0
	}
	denseOnly := func(candidate *findingCandidate) bool {
		return candidate != nil && candidate.laneRanks["dense"] > 0 &&
			candidate.laneRanks["exact"] == 0 && candidate.laneRanks["fts"] == 0
	}
	visible := make([]*findingCandidate, 0, len(ranked))
	for _, candidate := range ranked {
		if !denseOnly(candidate) {
			visible = append(visible, candidate)
		}
	}
	relationEligible := make(map[string]*findingCandidate, len(eligible))
	suppressed := 0
	for id, candidate := range eligible {
		if denseOnly(candidate) {
			suppressed++
			continue
		}
		relationEligible[id] = candidate
	}
	return visible, relationEligible, suppressed
}

func rerankFindingCandidates(query string, candidates []*findingCandidate, depth int) {
	if depth < 1 || depth > len(candidates) {
		depth = len(candidates)
	}
	for _, candidate := range candidates[:depth] {
		candidate.rerank = linearRerank(query, Chunk{
			Path: candidate.path, Heading: candidate.record.Subject,
			Content: candidate.record.Claim + "\n" + strings.Join(candidate.aliases, "\n") + "\n" + strings.Join(candidate.questions, "\n"),
		})
	}
	sort.SliceStable(candidates[:depth], func(i, j int) bool {
		if candidates[i].rerank != candidates[j].rerank {
			return candidates[i].rerank > candidates[j].rerank
		}
		if candidates[i].fusion != candidates[j].fusion {
			return candidates[i].fusion > candidates[j].fusion
		}
		return candidates[i].record.ID < candidates[j].record.ID
	})
}

func expandFindingRelations(ctx context.Context, db *sql.DB, ranked []*findingCandidate, eligible map[string]*findingCandidate, limit int) ([]*findingCandidate, error) {
	critical := map[string][]string{}
	rows, err := db.QueryContext(ctx, `SELECT source_id,target_id,kind FROM finding_relations
		ORDER BY kind,source_id,target_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var source, target, kind string
		if err := rows.Scan(&source, &target, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		sourceCandidate, sourceEligible := eligible[source]
		targetCandidate, targetEligible := eligible[target]
		if sourceEligible {
			sourceCandidate.relationAlerts = append(sourceCandidate.relationAlerts, kind+":"+target)
		}
		if targetEligible {
			alert := "incoming-" + kind + ":" + source
			if kind == "supersedes" {
				alert = "superseded-by:" + source
			}
			targetCandidate.relationAlerts = append(targetCandidate.relationAlerts, alert)
		}
		if sourceEligible && targetEligible && (kind == "contradicts" || kind == "supersedes") {
			critical[source] = append(critical[source], target)
			critical[target] = append(critical[target], source)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, candidate := range eligible {
		candidate.relationAlerts = SortedUnique(candidate.relationAlerts)
		critical[candidate.record.ID] = SortedUnique(critical[candidate.record.ID])
	}

	// Build the visible prefix as relation-aware groups. A candidate with a
	// contradiction or supersession target is admitted with that target when
	// the pair fits, so filling the ordinary rank prefix cannot consume the
	// slot needed to disclose the material relation.
	selected := map[string]bool{}
	prefix := make([]*findingCandidate, 0, limit)
	for _, candidate := range ranked {
		if len(prefix) >= limit {
			break
		}
		if selected[candidate.record.ID] {
			continue
		}
		group := []*findingCandidate{candidate}
		for _, relatedID := range critical[candidate.record.ID] {
			if !selected[relatedID] {
				group = append(group, eligible[relatedID])
			}
		}
		if len(group) > limit-len(prefix) {
			// With a one-card request the relation alert remains visible even
			// though returning both endpoints is structurally impossible.
			if limit == 1 && len(prefix) == 0 {
				group = group[:1]
			} else {
				continue
			}
		}
		if len(group) > 1 {
			groupIDs := make([]string, 0, len(group))
			for _, member := range group {
				groupIDs = append(groupIDs, member.record.ID)
			}
			groupKey := "relation:" + strings.Join(SortedUnique(groupIDs), ",")
			for _, member := range group {
				member.relationGroup = groupKey
			}
		}
		for _, member := range group {
			if member == nil || selected[member.record.ID] {
				continue
			}
			if member.record.ID != candidate.record.ID {
				member.relationAdds = append(member.relationAdds, "relation-from:"+candidate.record.ID)
				member.relationAdds = SortedUnique(member.relationAdds)
			}
			prefix = append(prefix, member)
			selected[member.record.ID] = true
		}
	}

	return prefix, nil
}

func findingContextCard(candidate *findingCandidate) ContextCard {
	why := append([]string(nil), candidate.why...)
	why = SortedUnique(why)
	alerts := append(relationAlerts(candidate.record), candidate.relationAlerts...)
	alerts = SortedUnique(alerts)
	return ContextCard{
		SchemaVersion: CampaignSchemaVersion, ID: candidate.record.ID, CardType: "finding",
		Claim: candidate.record.Claim, Subject: candidate.record.Subject, Scope: candidate.record.Scope,
		EvidenceGrade: candidate.record.EvidenceGrade, ReviewState: candidate.record.ReviewState,
		Validity: candidate.record.Validity, SourceClass: candidate.sourceClass,
		RelationAlerts: alerts, Handle: FindingHandle(candidate.record.ID),
		EvidenceHandle: candidate.strongestEvidence, WhyMatched: why,
		ExpansionTokens: EstimateTokens(candidate.record.Body),
		Metadata: map[string]string{
			"campaignId": candidate.record.CampaignID, "path": candidate.path,
			"recordDigest": candidate.record.Digest, "projection": candidate.record.Projection,
		},
	}
}

func cardsFitFindingBudget(options FindingQueryOptions, existing, additions []ContextCard, trace FindingQueryTrace, omitted int) bool {
	if omitted < 0 {
		omitted = 0
	}
	response := FindingQueryResponse{
		Query: options.Query, Cards: append(append([]ContextCard(nil), existing...), additions...),
		TokenBudget: options.TokenBudget, Omitted: omitted, Trace: trace,
	}
	_, err := finalizeFindingResponse(response)
	return err == nil
}

func boundedFindingTraceCandidates(ranked, visible []*findingCandidate, limit int) ([]*findingCandidate, int) {
	all := make([]*findingCandidate, 0, len(ranked)+len(visible))
	seen := map[string]bool{}
	appendUnique := func(candidates []*findingCandidate) {
		for _, candidate := range candidates {
			if candidate == nil || seen[candidate.record.ID] {
				continue
			}
			seen[candidate.record.ID] = true
			all = append(all, candidate)
		}
	}
	// Returned and relation-expanded candidates are always traceable. Fill the
	// remaining bounded trace with the highest-ranked non-returned candidates.
	appendUnique(visible)
	appendUnique(ranked)
	total := len(all)
	traceLimit := limit * 3
	if traceLimit < 5 {
		traceLimit = 5
	}
	if traceLimit > 15 {
		traceLimit = 15
	}
	if len(all) > traceLimit {
		all = all[:traceLimit]
	}
	return all, total
}

func finalizeFindingResponse(response FindingQueryResponse) (FindingQueryResponse, error) {
	for _, card := range response.Cards {
		if err := ValidateContextCard(card); err != nil {
			return FindingQueryResponse{}, err
		}
	}
	// Estimate with a fixed-width digest placeholder. The final digest has the
	// same serialized size, so EstimatedTokens describes the bytes actually
	// returned while the digest still commits to that estimate.
	response.Digest = "sha256:" + strings.Repeat("0", 64)
	for iteration := 0; iteration < 8; iteration++ {
		body, err := json.Marshal(response)
		if err != nil {
			return FindingQueryResponse{}, err
		}
		estimated := EstimateTokens(string(body))
		if estimated == response.EstimatedTokens {
			break
		}
		response.EstimatedTokens = estimated
	}
	if response.EstimatedTokens > response.TokenBudget {
		return FindingQueryResponse{}, errors.New("finding query budget is too small for mandatory trace metadata")
	}
	digestInput := response
	digestInput.Digest = ""
	digest, err := CanonicalDigest(digestInput)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	response.Digest = digest
	body, err := json.Marshal(response)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	if EstimateTokens(string(body)) != response.EstimatedTokens {
		return FindingQueryResponse{}, errors.New("finding query token estimate did not converge")
	}
	return response, nil
}

type rawReportCard struct {
	card   ContextCard
	digest string
	score  int
}

func queryRawReportCards(ctx context.Context, db *sql.DB, query string, limit int) ([]rawReportCard, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.path,d.title,d.content_hash,d.tier,d.size,
		c.heading,c.start_line,c.end_line,c.byte_range,c.start_byte,c.end_byte,c.content
		FROM documents d JOIN chunks c ON c.document_id=d.id
		WHERE d.source_kind='raw-report' ORDER BY d.path,c.start_line,c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	queryTerms := IdentifierTerms(query)
	best := map[string]rawReportCard{}
	for rows.Next() {
		var path, title, digest, tier, heading, content string
		var startLine, endLine, startByte, endByte int
		var documentBytes int64
		var byteRange bool
		if err := rows.Scan(&path, &title, &digest, &tier, &documentBytes, &heading, &startLine, &endLine,
			&byteRange, &startByte, &endByte, &content); err != nil {
			return nil, err
		}
		terms := map[string]bool{}
		for _, term := range IdentifierTerms(path + "\n" + heading + "\n" + content) {
			terms[term] = true
		}
		score := 0
		for _, term := range queryTerms {
			if terms[term] {
				score++
			}
		}
		if score == 0 {
			continue
		}
		claim := firstSentence(strings.TrimSpace(content), 320)
		evidenceHandle := fmt.Sprintf("path:%s#L%d-L%d", path, startLine, endLine)
		if byteRange {
			evidenceHandle = fmt.Sprintf("path:%s#B%d-B%d", path, startByte, endByte)
		}
		card := ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: StableID("raw", digest, path), CardType: "raw-report",
			Claim: claim, Title: title, SourceClass: "archive", Handle: "archive:" + digest,
			EvidenceHandle: evidenceHandle,
			// The archive handle expands the immutable report, not merely the
			// matched preview chunk. Charge that full expansion cost so callers and
			// normalized-vs-raw evaluation compare the artifacts they can actually
			// open rather than a selectively cheap excerpt.
			WhyMatched: []string{"raw-fallback:identifier-lexical"}, ExpansionTokens: int((documentBytes + 3) / 4),
			Metadata: map[string]string{
				"path": path, "digest": digest, "legacyTier": tier,
				"heading": heading, "startLine": fmt.Sprint(startLine), "endLine": fmt.Sprint(endLine),
				"byteRange": fmt.Sprint(byteRange), "startByte": fmt.Sprint(startByte), "endByte": fmt.Sprint(endByte),
			},
		}
		current, present := best[path]
		if !present || score > current.score || score == current.score && card.EvidenceHandle < current.card.EvidenceHandle {
			best[path] = rawReportCard{card: card, digest: digest, score: score}
		}
	}
	result := make([]rawReportCard, 0, len(best))
	for _, card := range best {
		result = append(result, card)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].score != result[j].score {
			return result[i].score > result[j].score
		}
		return result[i].card.Handle < result[j].card.Handle
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, rows.Err()
}
