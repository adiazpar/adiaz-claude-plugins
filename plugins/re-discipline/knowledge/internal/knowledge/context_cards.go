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
	Query                  string                `json:"query"`
	QueryClass             string                `json:"queryClass,omitempty"`
	CampaignID             string                `json:"campaignId,omitempty"`
	AllowedSourceClasses   []string              `json:"allowedSourceClasses,omitempty"`
	AllowedProvenanceTiers []string              `json:"allowedProvenanceTiers,omitempty"`
	AllowedReviewStates    []string              `json:"allowedReviewStates,omitempty"`
	AllowedValidities      []string              `json:"allowedValidities,omitempty"`
	Limit                  int                   `json:"limit,omitempty"`
	TokenBudget            int                   `json:"tokenBudget,omitempty"`
	IncludeRaw             bool                  `json:"includeRaw,omitempty"`
	RequestID              string                `json:"requestId,omitempty"`
	ContextLeaseID         string                `json:"contextLeaseId,omitempty"`
	ResetContextLease      bool                  `json:"resetContextLease,omitempty"`
	ArchivePolicy          ArchiveFallbackPolicy `json:"archivePolicy,omitempty"`
	// suppressRaw is an in-process measurement control. Public adapters cannot
	// set it; it lets the paired evaluator measure normalized cards without the
	// fallback lane contaminating recall or token cost.
	suppressRaw        bool
	suppressProvenance bool
	// suppressNormalized is the reciprocal in-process measurement control. It
	// leaves every public question, filter, budget, and limit unchanged while
	// preventing finding cards from entering the raw-only arm.
	suppressNormalized bool
}

type FindingCandidateTrace struct {
	FindingID    string         `json:"findingId"`
	LaneRanks    map[string]int `json:"laneRanks,omitempty"`
	FusionScore  int64          `json:"fusionScore,omitempty"`
	FilteredBy   []string       `json:"filteredBy,omitempty"`
	RelationAdds []string       `json:"relationAdds,omitempty"`
}

type FindingQueryTrace struct {
	AnalyzerVersion          string                  `json:"analyzerVersion"`
	FindingFormat            string                  `json:"findingFormat"`
	Candidates               []FindingCandidateTrace `json:"candidates"`
	CandidateOmitted         int                     `json:"candidateOmitted,omitempty"`
	FilteredByReason         map[string]int          `json:"filteredByReason,omitempty"`
	ProvenanceFallbackServed bool                    `json:"provenanceFallbackServed"`
	RawFallbackDefault       bool                    `json:"rawFallbackDefault"`
	RawFallbackServed        bool                    `json:"rawFallbackServed"`
	// Operational counters remain available to in-process callers but are
	// excluded from the deterministic query receipt and replay identity.
	ArchiveServes            []ArchiveServeEvent `json:"-"`
	NormalizationSuggestions []string            `json:"-"`
}

type FindingQueryResponse struct {
	Query                    string               `json:"query"`
	QueryClass               string               `json:"queryClass"`
	Status                   string               `json:"status"`
	Cards                    []ContextCard        `json:"cards"`
	TierDisagreements        []TierDisagreement   `json:"tierDisagreements,omitempty"`
	TierDisagreementsOmitted int                  `json:"tierDisagreementsOmitted,omitempty"`
	TierAuthorityRule        string               `json:"tierAuthorityRule,omitempty"`
	TokenBudget              int                  `json:"tokenBudget"`
	EstimatedTokens          int                  `json:"estimatedTokens"`
	Omitted                  int                  `json:"omitted"`
	Trace                    FindingQueryTrace    `json:"trace"`
	ContextLease             *ContextLeaseReceipt `json:"contextLease,omitempty"`
	Digest                   string               `json:"digest"`
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
	options.QueryClass = strings.TrimSpace(options.QueryClass)
	if options.QueryClass == "" || options.QueryClass == "auto" {
		options.QueryClass = classifyQuery(options.Query)
	}
	if !validOne(options.QueryClass, "exact", "conceptual", "orientation", "current",
		"provenance", "dependency", "contradiction") {
		return FindingQueryResponse{}, fmt.Errorf("unsupported finding query class %q", options.QueryClass)
	}
	if _, err := managedProvenanceTiers(options.AllowedProvenanceTiers); err != nil {
		return FindingQueryResponse{}, err
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
	if options.suppressNormalized {
		candidates = map[string]*findingCandidate{}
		filtered = nil
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
			FusionScore:  candidate.fusion,
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
	provenanceCandidateCount := 0
	provenanceCount := 0
	if !options.suppressProvenance && len(cards) < options.Limit {
		provenanceCards, total, err := queryManagedProvenanceCards(
			ctx, db, options, options.Limit-len(cards))
		if err != nil {
			return FindingQueryResponse{}, err
		}
		provenanceCandidateCount = total
		for _, provenance := range provenanceCards {
			omitted := normalizedCandidateCount - normalizedCount +
				provenanceCandidateCount - (provenanceCount + 1)
			if !cardsFitFindingBudget(options, cards, []ContextCard{provenance}, trace, omitted) {
				continue
			}
			cards = append(cards, provenance)
			provenanceCount++
			trace.ProvenanceFallbackServed = true
		}
	}
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
				source, err := normalizationSourceForReport(
					retriever.Boundary, raw.card.Metadata["path"], raw.digest)
				if err != nil {
					return FindingQueryResponse{}, err
				}
				event, err := retriever.ArchiveTracker.RecordSource(raw.digest, requestID, source)
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
	relevantPaths := map[string]bool{}
	for _, card := range cards {
		if candidatePath := card.Metadata["path"]; candidatePath != "" {
			relevantPaths[candidatePath] = true
		}
	}
	if options.QueryClass == "contradiction" {
		relevantPaths = nil
	}
	disagreements, err := loadTierDisagreements(ctx, db, relevantPaths, options.Limit)
	if err != nil {
		return FindingQueryResponse{}, err
	}
	response := FindingQueryResponse{
		Query: options.Query, QueryClass: options.QueryClass,
		Status: findingResponseStatus(cards, disagreements.Signals),
		Cards:  cards, TokenBudget: options.TokenBudget,
		TierDisagreements:        disagreements.Signals,
		TierDisagreementsOmitted: disagreements.Omitted,
		TierAuthorityRule:        disagreements.AuthorityRule,
		Omitted: normalizedCandidateCount - normalizedCount +
			provenanceCandidateCount - provenanceCount,
		Trace: trace,
	}
	if response.Omitted < 0 {
		response.Omitted = 0
	}
	return finalizeBoundedFindingResponse(response)
}

func findingResponseStatus(cards []ContextCard, disagreements []TierDisagreement) string {
	if len(disagreements) != 0 {
		return "conflicted"
	}
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

// Tier-disagreement metadata is useful only if the complete response remains
// inside the caller's hard budget. Contradiction queries prioritize at least
// one typed disagreement over ordinary cards; other query classes preserve
// their ranked cards and trim only the bounded disagreement tail.
func finalizeBoundedFindingResponse(response FindingQueryResponse) (FindingQueryResponse, error) {
	for {
		response.Status = findingResponseStatus(response.Cards, response.TierDisagreements)
		if response.TierDisagreementsOmitted > 0 {
			response.Status = "conflicted"
		}
		finalized, err := finalizeFindingResponse(response)
		if err == nil {
			return finalized, nil
		}
		if response.QueryClass == "contradiction" && len(response.TierDisagreements) != 0 &&
			len(response.Cards) != 0 {
			response.Cards = response.Cards[:len(response.Cards)-1]
			response.Omitted++
			continue
		}
		if len(response.TierDisagreements) != 0 {
			response.TierDisagreements = response.TierDisagreements[:len(response.TierDisagreements)-1]
			response.TierDisagreementsOmitted++
			if len(response.TierDisagreements) == 0 && response.TierDisagreementsOmitted == 0 {
				response.TierAuthorityRule = ""
			}
			continue
		}
		return FindingQueryResponse{}, err
	}
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
		f.record_digest,f.body,f.source_class,f.verified_at,d.path
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
			&candidate.record.Body, &candidate.sourceClass, &candidate.record.VerifiedAt, &candidate.path,
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

func (retriever Retriever) rankFindingDense(
	ctx context.Context,
	db *sql.DB,
	query string,
	candidates map[string]*findingCandidate,
) error {
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
// deletion. When lexical evidence exists, a normalized finding supported only
// by dense similarity cannot trail that lexical evidence as an authoritative
// card. In a true lexical vacuum dense-only candidates remain eligible as
// semantic rescue results.
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

func expandFindingRelations(ctx context.Context, db *sql.DB, ranked []*findingCandidate, eligible map[string]*findingCandidate, limit int) ([]*findingCandidate, error) {
	critical := map[string][]string{}
	stalenessGraph := map[string]FindingRecord{}
	rows, err := db.QueryContext(ctx, `SELECT r.source_id,r.target_id,r.kind,
		COALESCE(source.verified_at,''),COALESCE(target.verified_at,'')
		FROM finding_relations r
		LEFT JOIN findings source ON source.id=r.source_id
		LEFT JOIN findings target ON target.id=r.target_id
		ORDER BY r.kind,r.source_id,r.target_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var source, target, kind, sourceVerifiedAt, targetVerifiedAt string
		if err := rows.Scan(&source, &target, &kind, &sourceVerifiedAt, &targetVerifiedAt); err != nil {
			rows.Close()
			return nil, err
		}
		sourceCandidate, sourceEligible := eligible[source]
		targetCandidate, targetEligible := eligible[target]
		sourceFinding := stalenessGraph[source]
		sourceFinding.ID, sourceFinding.VerifiedAt = source, sourceVerifiedAt
		targetFinding := stalenessGraph[target]
		targetFinding.ID, targetFinding.VerifiedAt = target, targetVerifiedAt
		if kind == "depends-on" {
			sourceFinding.Relations.DependsOn = append(sourceFinding.Relations.DependsOn, target)
		}
		stalenessGraph[source], stalenessGraph[target] = sourceFinding, targetFinding
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
	stalenessPaths := FindingStalenessPaths(stalenessGraph)
	for dependentID := range stalenessPaths {
		if candidate := eligible[dependentID]; candidate != nil {
			candidate.relationAlerts = append(candidate.relationAlerts,
				findingStalenessRelationAlerts(stalenessPaths, dependentID)...)
		}
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
		Query: options.Query, QueryClass: options.QueryClass,
		Cards:       append(append([]ContextCard(nil), existing...), additions...),
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

type managedProvenanceCard struct {
	card  ContextCard
	score int
}

const managedProvenanceCandidateCeiling = 64

type managedProvenanceChunk struct {
	id            string
	sourcePath    string
	title         string
	digest        string
	tier          string
	documentBytes int64
	heading       string
	startLine     int
	endLine       int
	byteRange     bool
	startByte     int
	endByte       int
	content       string
}

type managedProvenanceCandidateRank struct {
	id        string
	exactHits int
	exactRank int
	ftsRank   int
}

// queryManagedProvenanceCards is the explicitly labeled escape hatch for
// ordinary managed Markdown that has no normalized FindingRecord. It returns
// exact path/range handles and never assigns review state, validity, or finding
// authority to the matched prose.
func queryManagedProvenanceCards(
	ctx context.Context,
	db *sql.DB,
	options FindingQueryOptions,
	limit int,
) ([]ContextCard, int, error) {
	if limit < 1 {
		return nil, 0, nil
	}
	allowed, err := managedProvenanceTiers(options.AllowedProvenanceTiers)
	if err != nil {
		return nil, 0, err
	}
	if len(allowed) == 0 {
		return nil, 0, nil
	}
	tiers := make([]string, 0, len(allowed))
	for tier := range allowed {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	candidates, err := loadManagedProvenanceCandidates(
		ctx, db, options.Query, tiers, managedProvenanceCandidateCeiling)
	if err != nil {
		return nil, 0, err
	}
	queryTerms := IdentifierTerms(options.Query)
	queryFolded := strings.ToLower(options.Query)
	best := map[string]managedProvenanceCard{}
	for _, candidate := range candidates {
		candidateText := candidate.sourcePath + "\n" + candidate.title + "\n" +
			candidate.heading + "\n" + candidate.content
		terms := map[string]bool{}
		for _, term := range IdentifierTerms(candidateText) {
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
		if strings.Contains(strings.ToLower(candidateText), queryFolded) {
			score += len(queryTerms) + 1
		}
		evidenceHandle := formatExactPathHandle(
			candidate.sourcePath, candidate.byteRange, candidate.startLine,
			candidate.endLine, candidate.startByte, candidate.endByte)
		card := ContextCard{
			SchemaVersion:  CampaignSchemaVersion,
			ID:             StableID("provenance", candidate.digest, candidate.sourcePath),
			CardType:       "provenance",
			Claim:          firstSentence(strings.TrimSpace(candidate.content), 320),
			Title:          candidate.title,
			EvidenceGrade:  "unknown",
			SourceClass:    candidate.tier,
			Handle:         formatExactPathHandle(candidate.sourcePath, false, 0, 0, 0, 0),
			EvidenceHandle: evidenceHandle,
			WhyMatched: []string{
				"provenance-fallback:identifier-lexical",
				"authority:provenance-only",
			},
			ExpansionTokens: int((candidate.documentBytes + 3) / 4),
			Metadata: map[string]string{
				"path": candidate.sourcePath, "digest": "sha256:" + candidate.digest,
				"tier": candidate.tier, "authority": "provenance-only", "sourceKind": "managed-document",
				"heading": candidate.heading, "startLine": fmt.Sprint(candidate.startLine), "endLine": fmt.Sprint(candidate.endLine),
				"byteRange": fmt.Sprint(candidate.byteRange), "startByte": fmt.Sprint(candidate.startByte), "endByte": fmt.Sprint(candidate.endByte),
			},
		}
		current, present := best[candidate.sourcePath]
		if !present || score > current.score ||
			score == current.score && card.EvidenceHandle < current.card.EvidenceHandle {
			best[candidate.sourcePath] = managedProvenanceCard{card: card, score: score}
		}
	}
	result := make([]managedProvenanceCard, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].score != result[j].score {
			return result[i].score > result[j].score
		}
		return result[i].card.Handle < result[j].card.Handle
	})
	total := len(result)
	if len(result) > limit {
		result = result[:limit]
	}
	cards := make([]ContextCard, 0, len(result))
	for _, candidate := range result {
		cards = append(cards, candidate.card)
	}
	return cards, total, nil
}

// loadManagedProvenanceCandidates admits candidates only through the exact
// term index and FTS5. Each lane receives a share of one hard ceiling, so the
// public fallback never materializes or scores an O(corpus) row set in Go.
func loadManagedProvenanceCandidates(
	ctx context.Context,
	db *sql.DB,
	query string,
	tiers []string,
	ceiling int,
) ([]managedProvenanceChunk, error) {
	if len(tiers) == 0 || ceiling < 1 || ceiling > managedProvenanceCandidateCeiling {
		return nil, errors.New("managed provenance candidate request is invalid")
	}
	exactTerms := SortedUnique(IdentifierTerms(query))
	if len(exactTerms) > 24 {
		exactTerms = exactTerms[:24]
	}
	ftsTokens := SortedUnique(modelTokens(query))
	if len(ftsTokens) > 24 {
		ftsTokens = ftsTokens[:24]
	}
	exactLimit, ftsLimit := 0, 0
	switch {
	case len(exactTerms) > 0 && len(ftsTokens) > 0:
		exactLimit = ceiling / 2
		ftsLimit = ceiling - exactLimit
	case len(exactTerms) > 0:
		exactLimit = ceiling
	case len(ftsTokens) > 0:
		ftsLimit = ceiling
	default:
		return nil, nil
	}

	ranked := map[string]*managedProvenanceCandidateRank{}
	if exactLimit > 0 {
		rows, err := queryManagedProvenanceExactCandidates(
			ctx, db, exactTerms, tiers, exactLimit)
		if err != nil {
			return nil, err
		}
		for index, row := range rows {
			row.exactRank = index + 1
			copy := row
			ranked[row.id] = &copy
		}
	}
	if ftsLimit > 0 {
		ids, err := queryManagedProvenanceFTSCandidates(
			ctx, db, ftsTokens, tiers, ftsLimit)
		if err != nil {
			return nil, err
		}
		for index, id := range ids {
			candidate := ranked[id]
			if candidate == nil {
				candidate = &managedProvenanceCandidateRank{id: id}
				ranked[id] = candidate
			}
			candidate.ftsRank = index + 1
		}
	}
	ordered := make([]managedProvenanceCandidateRank, 0, len(ranked))
	for _, candidate := range ranked {
		ordered = append(ordered, *candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := managedProvenanceAdmissionScore(ordered[i]),
			managedProvenanceAdmissionScore(ordered[j])
		if left != right {
			return left > right
		}
		return ordered[i].id < ordered[j].id
	})
	if len(ordered) > ceiling {
		ordered = ordered[:ceiling]
	}
	ids := make([]string, len(ordered))
	for index := range ordered {
		ids[index] = ordered[index].id
	}
	return loadManagedProvenanceChunks(ctx, db, ids, tiers)
}

func managedProvenanceAdmissionScore(candidate managedProvenanceCandidateRank) int {
	score := candidate.exactHits * 10000
	if candidate.exactRank > 0 {
		score += 100000 / (10 + candidate.exactRank)
	}
	if candidate.ftsRank > 0 {
		score += 100000 / (10 + candidate.ftsRank)
	}
	return score
}

func queryManagedProvenanceExactCandidates(
	ctx context.Context,
	db *sql.DB,
	terms []string,
	tiers []string,
	limit int,
) ([]managedProvenanceCandidateRank, error) {
	termPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(terms)), ",")
	tierPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := make([]any, 0, len(terms)+len(tiers)+1)
	for _, term := range terms {
		args = append(args, term)
	}
	for _, tier := range tiers {
		args = append(args, tier)
	}
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `SELECT t.chunk_id,count(*) AS hits
		FROM terms t JOIN chunks c ON c.id=t.chunk_id
		JOIN documents d ON d.id=c.document_id
		WHERE t.term IN (`+termPlaceholders+`) AND d.source_kind=''
		AND c.tier IN (`+tierPlaceholders+`)
		GROUP BY t.chunk_id
		ORDER BY hits DESC,c.path ASC,c.start_line ASC,c.id ASC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []managedProvenanceCandidateRank{}
	for rows.Next() {
		var candidate managedProvenanceCandidateRank
		if err := rows.Scan(&candidate.id, &candidate.exactHits); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func queryManagedProvenanceFTSCandidates(
	ctx context.Context,
	db *sql.DB,
	tokens []string,
	tiers []string,
	limit int,
) ([]string, error) {
	parts := make([]string, len(tokens))
	for index, token := range tokens {
		parts[index] = `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
	}
	tierPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := []any{strings.Join(parts, " OR ")}
	for _, tier := range tiers {
		args = append(args, tier)
	}
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `SELECT c.id,
		bm25(chunks_fts,0.0,3.0,5.0,1.0) AS rank
		FROM chunks_fts JOIN chunks c ON c.id=chunks_fts.chunk_id
		JOIN documents d ON d.id=c.document_id
		WHERE chunks_fts MATCH ? AND d.source_kind=''
		AND c.tier IN (`+tierPlaceholders+`)
		ORDER BY rank ASC,c.path ASC,c.start_line ASC,c.id ASC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		var ignoredRank float64
		if err := rows.Scan(&id, &ignoredRank); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func loadManagedProvenanceChunks(
	ctx context.Context,
	db *sql.DB,
	ids []string,
	tiers []string,
) ([]managedProvenanceChunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	idPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	tierPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := make([]any, 0, len(ids)+len(tiers))
	for _, id := range ids {
		args = append(args, id)
	}
	for _, tier := range tiers {
		args = append(args, tier)
	}
	rows, err := db.QueryContext(ctx, `SELECT c.id,d.path,d.title,d.content_hash,d.tier,d.size,
		c.heading,c.start_line,c.end_line,c.byte_range,c.start_byte,c.end_byte,c.content
		FROM chunks c JOIN documents d ON d.id=c.document_id
		WHERE c.id IN (`+idPlaceholders+`) AND d.source_kind=''
		AND c.tier IN (`+tierPlaceholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]managedProvenanceChunk{}
	for rows.Next() {
		var candidate managedProvenanceChunk
		if err := rows.Scan(
			&candidate.id, &candidate.sourcePath, &candidate.title, &candidate.digest,
			&candidate.tier, &candidate.documentBytes, &candidate.heading,
			&candidate.startLine, &candidate.endLine, &candidate.byteRange,
			&candidate.startByte, &candidate.endByte, &candidate.content,
		); err != nil {
			return nil, err
		}
		byID[candidate.id] = candidate
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]managedProvenanceChunk, 0, len(ids))
	for _, id := range ids {
		if candidate, ok := byID[id]; ok {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func managedProvenanceTiers(explicit []string) (map[string]bool, error) {
	if len(explicit) != 0 {
		allowed := map[string]bool{}
		for _, tier := range explicit {
			if !validOne(tier, "truth", "campaign", "provisional", "history", "backlog",
				"profile", "memory", "intake", "state", "navigation", "playbook", "asset") {
				return nil, fmt.Errorf("unsupported managed provenance tier %q", tier)
			}
			if allowed[tier] {
				return nil, fmt.Errorf("managed provenance tier %q is repeated", tier)
			}
			allowed[tier] = true
		}
		return allowed, nil
	}
	return map[string]bool{
		"truth": true, "profile": true, "navigation": true, "history": true,
		"backlog": true, "memory": true, "playbook": true, "asset": true,
	}, nil
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
		evidenceHandle := formatExactPathHandle(
			path, byteRange, startLine, endLine, startByte, endByte)
		card := ContextCard{
			SchemaVersion: CampaignSchemaVersion, ID: StableID("raw", digest, path), CardType: "raw-report",
			Claim: claim, Title: title, SourceClass: "archive",
			Handle:         formatExactPathHandle(path, false, 0, 0, 0, 0),
			EvidenceHandle: evidenceHandle,
			// The source path handle expands the generation-bound report, not merely the
			// matched preview chunk. Expose that expected expansion cost for caller
			// planning; paired evaluation measures the bounded response bytes for
			// both arms and never mixes this value into only the raw side.
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
