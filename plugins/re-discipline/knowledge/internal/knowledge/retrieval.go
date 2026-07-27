package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
)

type SearchOptions struct {
	Query        string
	QueryClass   string
	AllowedTiers []string
	Limit        int
	TokenBudget  int
}

type candidate struct {
	Chunk        Chunk
	DocumentHash string
	LaneRanks    map[string]int
	Fusion       int64
	Rerank       int64
}

type rankedID struct {
	id    string
	score int
}

// minDistinctDocuments is how many separate documents a response tries to
// represent before spending budget on additional chunks of ones it already
// carries. Three is enough to show a claim, a corroborating source, and a
// dissenting one without crowding out depth on a single-document answer.
const minDistinctDocuments = 3

type Retriever struct {
	Boundary   Boundary
	Generation Generation
	Profile    SelectedProfile
}

func (retriever Retriever) Search(ctx context.Context, options SearchOptions) (SearchResponse, error) {
	options.Query = strings.TrimSpace(options.Query)
	if options.Query == "" || len([]byte(options.Query)) > 1000 {
		return SearchResponse{}, errors.New("query must contain 1 to 1000 UTF-8 bytes")
	}
	if options.QueryClass == "" || options.QueryClass == "auto" {
		options.QueryClass = classifyQuery(options.Query)
	}
	switch options.QueryClass {
	case "exact", "conceptual", "orientation", "current", "provenance", "dependency", "contradiction":
	default:
		return SearchResponse{}, fmt.Errorf("unsupported query class %q", options.QueryClass)
	}
	tiers, err := ValidateTierList(options.AllowedTiers)
	if err != nil {
		return SearchResponse{}, err
	}
	if options.Limit < 1 || options.Limit > 50 {
		return SearchResponse{}, errors.New("result limit must be between 1 and 50")
	}
	if options.Limit > retriever.Profile.Effective.Packing.MaxPassages {
		options.Limit = retriever.Profile.Effective.Packing.MaxPassages
	}
	if options.TokenBudget < 128 || options.TokenBudget > 8192 {
		return SearchResponse{}, errors.New("token budget must be between 128 and 8192")
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(retriever.Generation.Database))
	if err != nil {
		return SearchResponse{}, err
	}
	defer db.Close()

	candidates := make(map[string]*candidate)
	get := func(row candidate) *candidate {
		if existing, ok := candidates[row.Chunk.ID]; ok {
			return existing
		}
		row.LaneRanks = map[string]int{}
		candidates[row.Chunk.ID] = &row
		return &row
	}
	active := make(map[string]bool)
	for _, lane := range retriever.Profile.ActiveLanes {
		active[lane] = true
	}
	if active["exact"] {
		exact, err := rankExact(ctx, db, options.Query, tiers, 400)
		if err != nil {
			return SearchResponse{}, err
		}
		for rank, row := range exact {
			get(row).LaneRanks["exact"] = rank + 1
		}
	}
	if active["fts"] {
		fts, err := rankFTS(ctx, db, options.Query, tiers, 200)
		if err != nil {
			return SearchResponse{}, err
		}
		for rank, row := range fts {
			get(row).LaneRanks["fts"] = rank + 1
		}
	}
	if active["dense"] {
		if retriever.Profile.Effective.Requires.Embedding == nil {
			return SearchResponse{}, errors.New("effective profile enables dense lane without a pinned embedding model")
		}
		var embedding ModelIdentity
		for _, model := range retriever.Profile.Models {
			if model.ID == *retriever.Profile.Effective.Requires.Embedding {
				embedding = model
				break
			}
		}
		if embedding.ID == "" {
			return SearchResponse{}, errors.New("effective embedding model identity is unavailable")
		}
		dense, err := rankDense(
			ctx, db, options.Query, tiers, embedding,
		)
		if err != nil {
			return SearchResponse{}, err
		}
		for rank, row := range dense {
			get(row).LaneRanks["dense"] = rank + 1
		}
	}
	if active["graph"] {
		// Relationship expansion starts from lexical evidence. Dense-only
		// neighbors are useful results, but treating every semantic neighbor as
		// a graph seed lets well-linked unrelated documents collectively swamp
		// a strong no-overlap semantic match.
		graphCandidates := make(map[string]*candidate)
		for id, row := range candidates {
			if row.LaneRanks["exact"] > 0 || row.LaneRanks["fts"] > 0 {
				graphCandidates[id] = row
			}
		}
		seeds := sortedCandidates(
			graphCandidates,
			retriever.Profile.Effective.Weights,
			retriever.Profile.Effective.RRFK,
		)
		graph, err := rankGraph(ctx, db, seeds, tiers, 100)
		if err != nil {
			return SearchResponse{}, err
		}
		for rank, row := range graph {
			get(row).LaneRanks["graph"] = rank + 1
		}
	}
	ranked := sortedCandidates(candidates, retriever.Profile.Effective.Weights, retriever.Profile.Effective.RRFK)
	if active["rerank"] && shouldRerank(options.QueryClass, len(ranked)) {
		depth := retriever.Profile.Effective.RerankDepth
		if depth > len(ranked) {
			depth = len(ranked)
		}
		for index := 0; index < depth; index++ {
			ranked[index].Rerank = linearRerank(options.Query, ranked[index].Chunk)
		}
		sort.SliceStable(ranked[:depth], func(i, j int) bool {
			left := ranked[i].Fusion + ranked[i].Rerank*1000
			right := ranked[j].Fusion + ranked[j].Rerank*1000
			if left != right {
				return left > right
			}
			return candidateLess(ranked[i], ranked[j])
		})
	}

	// Promote the highest-ranked chunk of each distinct document ahead of
	// additional chunks of documents already represented, up to
	// minDistinctDocuments. A document whose chunks hold the top ranks would
	// otherwise fill the whole response even when the budget allows several,
	// and maxPerDocument cannot prevent it: that cap only binds once more than
	// one result fits at all.
	//
	// This is a stable reordering rather than a second packing pass, so the
	// omission counters below stay exact and the result stays deterministic.
	if len(ranked) > 1 {
		seenPath := map[string]bool{}
		lead := make([]*candidate, 0, len(ranked))
		rest := make([]*candidate, 0, len(ranked))
		for _, row := range ranked {
			if len(seenPath) < minDistinctDocuments && !seenPath[row.Chunk.Path] {
				seenPath[row.Chunk.Path] = true
				lead = append(lead, row)
				continue
			}
			rest = append(rest, row)
		}
		ranked = append(lead, rest...)
	}

	replayInput := struct {
		Generation string
		Profile    string
		Query      string
		Class      string
		Tiers      []string
		Limit      int
		Budget     int
	}{retriever.Generation.ID, retriever.Profile.EffectiveIdentity, options.Query,
		options.QueryClass, tiers, options.Limit, options.TokenBudget}
	replay, _ := CanonicalDigest(replayInput)
	replay = compactReplayHandle(replay)

	results := []SearchResult{}
	usedTokens := 0
	usedBytes := 0
	perDocument := map[string]int{}
	omitted := 0
	omittedByReason := map[string]int{
		"resultLimit": 0, "documentCap": 0, "staleSource": 0, "budget": 0,
	}
	// Mandatory response metadata is charged against the same token budget as
	// the passages, so reserve it before packing. Budgeting passages against
	// the full budget admits a passage that finalizeSearchResponse must then
	// strip, and because the loop has already skipped every smaller candidate,
	// the response comes back empty with budget left unspent.
	contentBudget := options.TokenBudget - responseTokenOverhead(SearchResponse{
		Query: options.Query, QueryClass: options.QueryClass, AllowedTiers: tiers,
		TokenBudget: options.TokenBudget, Results: []SearchResult{},
		Omitted: len(ranked), OmittedByReason: map[string]int{
			"resultLimit": 0, "documentCap": 0, "staleSource": 0, "budget": len(ranked),
		},
		Metadata: retriever.metadata(replay),
	})
	if contentBudget < 0 {
		contentBudget = 0
	}
	for _, row := range ranked {
		if len(results) >= options.Limit {
			omitted++
			omittedByReason["resultLimit"]++
			continue
		}
		if perDocument[row.Chunk.Path] >= retriever.Profile.Effective.MaxPerDocument {
			omitted++
			omittedByReason["documentCap"]++
			continue
		}
		if !retriever.verifyChunk(row.Chunk, row.DocumentHash) {
			omitted++
			omittedByReason["staleSource"]++
			continue
		}
		// The document's epistemic header is loaded only for passages that are
		// actually packed, so the cost is bounded by the result limit rather
		// than by the candidate set. A chunk carrying no context is the
		// document's opening chunk, which already contains the header.
		var chunkContext, chunkContextHash string
		_ = db.QueryRowContext(ctx,
			`SELECT context,context_hash FROM chunks WHERE id=?`,
			row.Chunk.ID).Scan(&chunkContext, &chunkContextHash)

		uri := "re-discipline://" + retriever.Generation.ID + "/chunks/" + row.Chunk.ID
		result := SearchResult{
			ChunkID: row.Chunk.ID, Score: row.Fusion, Rerank: row.Rerank,
			LaneRanks: cloneRanks(row.LaneRanks), Passage: row.Chunk.Content,
			DocumentContext: chunkContext,
			Citation: Citation{
				Path: row.Chunk.Path, Heading: row.Chunk.Heading,
				StartLine: row.Chunk.StartLine, EndLine: row.Chunk.EndLine,
				ContentHash: row.Chunk.ContentHash, SourceHash: row.DocumentHash,
				PassageHash: row.Chunk.ContentHash, Tier: row.Chunk.Tier, URI: uri,
				ContextHash: chunkContextHash,
			},
		}
		// Charge the passage what it actually costs on the wire. A citation
		// carries three content hashes, a generation URI, and JSON field names,
		// so a fixed-constant estimate understates it badly enough that the
		// loop admits passages finalizeSearchResponse must then strip.
		encoded, err := json.Marshal(result)
		if err != nil {
			return SearchResponse{}, err
		}
		tokens := EstimateTokens(string(encoded)) + 1
		bytes := len([]byte(row.Chunk.Content))
		if usedTokens+tokens > contentBudget ||
			usedBytes+bytes > retriever.Profile.Effective.Packing.MaxBytes {
			omitted++
			omittedByReason["budget"]++
			continue
		}
		results = append(results, result)
		usedTokens += tokens
		usedBytes += bytes
		perDocument[row.Chunk.Path]++
	}
	response := SearchResponse{
		Query: options.Query, QueryClass: options.QueryClass, AllowedTiers: tiers,
		TokenBudget: options.TokenBudget, Results: results, Omitted: omitted,
		OmittedByReason: omittedByReason,
		Metadata:        retriever.metadata(replay),
	}
	return finalizeSearchResponse(
		response, options.TokenBudget, retriever.Profile.Effective.Packing.MaxBytes,
	)
}

func compactReplayHandle(digest string) string {
	value := strings.TrimPrefix(digest, "sha256:")
	if len(value) < 20 {
		value = SHA256String(digest)
	}
	return "replay-" + value[:20]
}

// responseTokenOverhead measures the tokens a search response consumes before
// any passage is packed: mandatory metadata, echoed query fields, and omission
// accounting. Callers subtract it from the caller-visible budget so the packing
// loop only admits passages that can survive finalizeSearchResponse.
//
// The skeleton is measured with worst-case omission counts, because the counts
// are serialized into the body and widen as they grow. Slight
// under-reservation stays safe: finalizeSearchResponse remains the backstop.
func responseTokenOverhead(skeleton SearchResponse) int {
	skeleton.Results = []SearchResult{}
	skeleton.EstimatedTokens = 0
	for iteration := 0; iteration < 6; iteration++ {
		body, err := json.Marshal(skeleton)
		if err != nil {
			return skeleton.TokenBudget
		}
		estimated := EstimateTokens(string(body))
		if estimated == skeleton.EstimatedTokens {
			break
		}
		skeleton.EstimatedTokens = estimated
	}
	return skeleton.EstimatedTokens
}

func finalizeSearchResponse(
	response SearchResponse,
	tokenBudget int,
	maxBytes int,
) (SearchResponse, error) {
	for {
		converged := false
		for iteration := 0; iteration < 6; iteration++ {
			body, err := json.Marshal(response)
			if err != nil {
				return SearchResponse{}, err
			}
			estimated := EstimateTokens(string(body))
			if estimated == response.EstimatedTokens {
				converged = true
				break
			}
			response.EstimatedTokens = estimated
		}
		if !converged {
			return SearchResponse{}, errors.New("search token accounting failed to converge")
		}
		body, err := json.Marshal(response)
		if err != nil {
			return SearchResponse{}, err
		}
		if response.EstimatedTokens <= tokenBudget && len(body) <= maxBytes {
			return response, nil
		}
		if len(response.Results) == 0 {
			return SearchResponse{}, errors.New(
				"search budget is too small for mandatory retrieval metadata")
		}
		response.Results = response.Results[:len(response.Results)-1]
		response.Omitted++
		response.OmittedByReason["budget"]++
		response.EstimatedTokens = 0
	}
}

func (retriever Retriever) metadata(replay string) RetrievalMetadata {
	models := make([]string, 0, len(retriever.Profile.Models))
	for _, model := range retriever.Profile.Models {
		models = append(models, model.ID+"@"+model.Revision)
	}
	modelFingerprint, _ := CanonicalDigest(retriever.Profile.Models)
	runtimeFingerprint, _ := CanonicalDigest(retriever.Generation.Runtime)
	return RetrievalMetadata{
		Project:             retriever.Generation.Project,
		Worktree:            retriever.Generation.Worktree,
		GitRevision:         retriever.Generation.GitRevision,
		DirtyFingerprint:    retriever.Generation.DirtyFingerprint,
		Generation:          retriever.Generation.ID,
		CorpusFingerprint:   retriever.Generation.CorpusFingerprint,
		ParserVersion:       retriever.Generation.ParserVersion,
		ChunkerVersion:      retriever.Generation.ChunkerVersion,
		RequestedProfile:    retriever.Profile.RequestedIdentity,
		EffectiveProfile:    retriever.Profile.EffectiveIdentity,
		ActiveLanes:         append([]string(nil), retriever.Profile.ActiveLanes...),
		Models:              models,
		ModelFingerprint:    modelFingerprint,
		RuntimeFingerprint:  runtimeFingerprint,
		FallbackReason:      retriever.Profile.FallbackReason,
		DeterministicReplay: replay,
		ServingStale:        retriever.Generation.ServingStale,
		WriterContention:    retriever.Generation.WriterContention,
	}
}

func loadEligibleChunks(ctx context.Context, db *sql.DB, tiers []string) ([]candidate, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := make([]any, len(tiers))
	for index, tier := range tiers {
		args[index] = tier
	}
	query := `SELECT c.id,c.document_id,c.path,c.tier,c.heading,c.start_line,c.end_line,
		c.content,c.content_hash,COALESCE(c.parent_id,''),COALESCE(c.previous_id,''),
		COALESCE(c.next_id,''),d.content_hash
		FROM chunks c JOIN documents d ON d.id=c.document_id
		WHERE c.tier IN (` + placeholders + `)
		ORDER BY c.path,c.start_line,c.id`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []candidate{}
	for rows.Next() {
		var row candidate
		if err := rows.Scan(
			&row.Chunk.ID, &row.Chunk.DocumentID, &row.Chunk.Path, &row.Chunk.Tier,
			&row.Chunk.Heading, &row.Chunk.StartLine, &row.Chunk.EndLine,
			&row.Chunk.Content, &row.Chunk.ContentHash, &row.Chunk.ParentID,
			&row.Chunk.PreviousID, &row.Chunk.NextID, &row.DocumentHash,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func rankExact(
	ctx context.Context,
	db *sql.DB,
	query string,
	tiers []string,
	candidateLimit int,
) ([]candidate, error) {
	queryTokens := SortedUnique(modelTokens(query))
	terms := append([]string(nil), queryTokens...)
	if len(terms) > 24 {
		terms = terms[:24]
	}
	trigrams := []string{}
	for _, term := range terms {
		trigrams = append(trigrams, termTrigrams(term)...)
	}
	trigrams = SortedUnique(trigrams)
	if len(trigrams) > 48 {
		trigrams = trigrams[:48]
	}
	tierPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	idScores := map[string]int{}
	queryIDs := func(table, column string, values []string, weight int) error {
		if len(values) == 0 {
			return nil
		}
		valuePlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
		args := []any{}
		for _, value := range values {
			args = append(args, value)
		}
		for _, tier := range tiers {
			args = append(args, tier)
		}
		args = append(args, candidateLimit)
		statement := `SELECT x.chunk_id,count(*) AS hits FROM ` + table + ` x
			JOIN chunks c ON c.id=x.chunk_id
			WHERE x.` + column + ` IN (` + valuePlaceholders + `)
			AND c.tier IN (` + tierPlaceholders + `)
			GROUP BY x.chunk_id ORDER BY hits DESC,x.chunk_id ASC LIMIT ?`
		rows, err := db.QueryContext(ctx, statement, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var hits int
			if err := rows.Scan(&id, &hits); err != nil {
				return err
			}
			idScores[id] += hits * weight
		}
		return rows.Err()
	}
	if err := queryIDs("terms", "term", terms, 10); err != nil {
		return nil, err
	}
	if err := queryIDs("trigrams", "trigram", trigrams, 1); err != nil {
		return nil, err
	}
	// Direct source identifiers remain bounded by exact path/title indexes.
	directArgs := []any{strings.ToLower(query), strings.ToLower(query)}
	for _, tier := range tiers {
		directArgs = append(directArgs, tier)
	}
	directRows, err := db.QueryContext(ctx, `SELECT c.id FROM chunks c
		JOIN documents d ON d.id=c.document_id
		WHERE (lower(c.path)=? OR lower(d.title)=?) AND c.tier IN (`+
		tierPlaceholders+`) ORDER BY c.path,c.start_line LIMIT 50`, directArgs...)
	if err != nil {
		return nil, err
	}
	for directRows.Next() {
		var id string
		if err := directRows.Scan(&id); err != nil {
			directRows.Close()
			return nil, err
		}
		idScores[id] += 100000
	}
	directRows.Close()
	ids := make([]rankedID, 0, len(idScores))
	for id, score := range idScores {
		ids = append(ids, rankedID{id, score})
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].score != ids[j].score {
			return ids[i].score > ids[j].score
		}
		return ids[i].id < ids[j].id
	})
	if len(ids) > candidateLimit {
		ids = ids[:candidateLimit]
	}
	candidates, err := loadChunksByID(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	queryLower := strings.ToLower(query)
	type scored struct {
		row   candidate
		score int64
	}
	scoredRows := []scored{}
	for _, row := range candidates {
		content := strings.ToLower(row.Chunk.Content)
		heading := strings.ToLower(row.Chunk.Heading)
		path := strings.ToLower(row.Chunk.Path)
		var score int64
		score += int64(strings.Count(content, queryLower)) * 10000
		score += int64(strings.Count(heading, queryLower)) * 18000
		score += int64(strings.Count(path, queryLower)) * 22000
		for _, token := range queryTokens {
			score += int64(strings.Count(content, token)) * 100
			score += int64(strings.Count(heading, token)) * 300
			score += int64(strings.Count(path, token)) * 500
		}
		if score > 0 {
			scoredRows = append(scoredRows, scored{row, score})
		}
	}
	sort.Slice(scoredRows, func(i, j int) bool {
		if scoredRows[i].score != scoredRows[j].score {
			return scoredRows[i].score > scoredRows[j].score
		}
		return candidateLess(&scoredRows[i].row, &scoredRows[j].row)
	})
	if len(scoredRows) > 200 {
		scoredRows = scoredRows[:200]
	}
	result := make([]candidate, len(scoredRows))
	for index := range scoredRows {
		result[index] = scoredRows[index].row
	}
	return result, nil
}

func loadChunksByID(ctx context.Context, db *sql.DB, ids []rankedID) ([]candidate, error) {
	result := []candidate{}
	for _, item := range ids {
		var row candidate
		err := db.QueryRowContext(ctx, `SELECT c.id,c.document_id,c.path,c.tier,c.heading,
			c.start_line,c.end_line,c.content,c.content_hash,COALESCE(c.parent_id,''),
			COALESCE(c.previous_id,''),COALESCE(c.next_id,''),d.content_hash
			FROM chunks c JOIN documents d ON d.id=c.document_id WHERE c.id=?`,
			item.id).Scan(
			&row.Chunk.ID, &row.Chunk.DocumentID, &row.Chunk.Path, &row.Chunk.Tier,
			&row.Chunk.Heading, &row.Chunk.StartLine, &row.Chunk.EndLine,
			&row.Chunk.Content, &row.Chunk.ContentHash, &row.Chunk.ParentID,
			&row.Chunk.PreviousID, &row.Chunk.NextID, &row.DocumentHash,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, nil
}

func rankFTS(ctx context.Context, db *sql.DB, query string, tiers []string, limit int) ([]candidate, error) {
	tokens := SortedUnique(modelTokens(query))
	if len(tokens) == 0 {
		return nil, nil
	}
	if len(tokens) > 24 {
		tokens = tokens[:24]
	}
	parts := make([]string, len(tokens))
	for index, token := range tokens {
		parts[index] = `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
	}
	match := strings.Join(parts, " OR ")
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := []any{match}
	for _, tier := range tiers {
		args = append(args, tier)
	}
	args = append(args, limit)
	statement := `SELECT c.id,c.document_id,c.path,c.tier,c.heading,c.start_line,c.end_line,
		c.content,c.content_hash,COALESCE(c.parent_id,''),COALESCE(c.previous_id,''),
		COALESCE(c.next_id,''),d.content_hash,bm25(chunks_fts,0.0,3.0,5.0,1.0) AS rank
		FROM chunks_fts
		JOIN chunks c ON c.id=chunks_fts.chunk_id
		JOIN documents d ON d.id=c.document_id
		WHERE chunks_fts MATCH ? AND c.tier IN (` + placeholders + `)
		ORDER BY rank ASC,c.path ASC,c.start_line ASC,c.id ASC LIMIT ?`
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []candidate{}
	for rows.Next() {
		var row candidate
		var ignoredRank float64
		if err := rows.Scan(
			&row.Chunk.ID, &row.Chunk.DocumentID, &row.Chunk.Path, &row.Chunk.Tier,
			&row.Chunk.Heading, &row.Chunk.StartLine, &row.Chunk.EndLine,
			&row.Chunk.Content, &row.Chunk.ContentHash, &row.Chunk.ParentID,
			&row.Chunk.PreviousID, &row.Chunk.NextID, &row.DocumentHash, &ignoredRank,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func rankDense(
	ctx context.Context,
	db *sql.DB,
	query string,
	tiers []string,
	modelValue any,
) ([]candidate, error) {
	model, err := resolveEmbeddingModel(modelValue)
	if err != nil {
		return nil, err
	}
	queryVector, err := embeddingVector(model, query)
	if err != nil {
		return nil, err
	}
	modelID := model.ID
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	countArgs := []any{modelID}
	for _, tier := range tiers {
		countArgs = append(countArgs, tier)
	}
	var eligibleCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM vectors v
		JOIN chunks c ON c.id=v.chunk_id
		WHERE v.model_id=? AND c.tier IN (`+placeholders+`)`, countArgs...).Scan(&eligibleCount); err != nil {
		return nil, err
	}
	var rows []denseCandidate
	if eligibleCount <= denseExactScanMaximum {
		rows, err = loadDenseCandidates(
			ctx, db, tiers, modelID, nil, denseCandidateMaximum)
	} else {
		rows, err = loadDenseCandidateFloor(
			ctx, db, tiers, modelID,
			vectorSignatureSlice(queryVector), denseExpandedMinimum, denseCandidateMaximum,
		)
	}
	if err != nil {
		return nil, err
	}
	type scored struct {
		row   candidate
		score int64
	}
	scoredRows := []scored{}
	for _, row := range rows {
		score, err := vectorDot(queryVector, row.vector)
		if err != nil {
			return nil, err
		}
		minimumScore := int64(1)
		if model.Implementation == "bundled-local" {
			// Q8 token vectors and sentence vectors are L2 normalized. A
			// positive dot product alone is not evidence; require cosine >=
			// 0.30 to keep unrelated dense neighbors from defeating abstention.
			minimumScore = sentenceVectorScale * sentenceVectorScale * 30 / 100
		}
		if score >= minimumScore {
			scoredRows = append(scoredRows, scored{row.candidate, score})
		}
	}
	sort.Slice(scoredRows, func(i, j int) bool {
		if scoredRows[i].score != scoredRows[j].score {
			return scoredRows[i].score > scoredRows[j].score
		}
		return candidateLess(&scoredRows[i].row, &scoredRows[j].row)
	})
	if len(scoredRows) > 200 {
		scoredRows = scoredRows[:200]
	}
	result := make([]candidate, len(scoredRows))
	for index := range scoredRows {
		result[index] = scoredRows[index].row
	}
	return result, nil
}

func loadDenseCandidateFloor(
	ctx context.Context,
	db *sql.DB,
	tiers []string,
	modelID string,
	querySignature uint16,
	minimum int,
	maximum int,
) ([]denseCandidate, error) {
	signatures := make([]uint16, 1<<16)
	for value := range signatures {
		signatures[value] = uint16(value)
	}
	sort.Slice(signatures, func(i, j int) bool {
		leftDistance := bits.OnesCount16(signatures[i] ^ querySignature)
		rightDistance := bits.OnesCount16(signatures[j] ^ querySignature)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		return signatures[i] < signatures[j]
	})
	const signatureBatch = 700
	seen := map[string]bool{}
	result := make([]denseCandidate, 0, minimum)
	for start := 0; start < len(signatures) && len(result) < minimum && len(result) < maximum; start += signatureBatch {
		end := start + signatureBatch
		if end > len(signatures) {
			end = len(signatures)
		}
		rows, err := loadDenseCandidates(
			ctx, db, tiers, modelID, signatures[start:end], maximum-len(result),
		)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !seen[row.Chunk.ID] {
				seen[row.Chunk.ID] = true
				result = append(result, row)
			}
		}
	}
	return result, nil
}

type denseCandidate struct {
	candidate
	vector []int32
}

func loadDenseCandidates(
	ctx context.Context,
	db *sql.DB,
	tiers []string,
	modelID string,
	signatures []uint16,
	limit int,
) ([]denseCandidate, error) {
	tierPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
	args := []any{modelID}
	statement := `SELECT c.id,c.document_id,c.path,c.tier,c.heading,c.start_line,c.end_line,
		c.content,c.content_hash,COALESCE(c.parent_id,''),COALESCE(c.previous_id,''),
		COALESCE(c.next_id,''),d.content_hash,v.vector
		FROM vectors v JOIN chunks c ON c.id=v.chunk_id
		JOIN documents d ON d.id=c.document_id
		WHERE v.model_id=? AND c.tier IN (` + tierPlaceholders + `)`
	for _, tier := range tiers {
		args = append(args, tier)
	}
	if len(signatures) > 0 {
		signaturePlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(signatures)), ",")
		statement += ` AND v.signature IN (` + signaturePlaceholders + `)`
		for _, signature := range signatures {
			args = append(args, int64(signature))
		}
	}
	statement += ` ORDER BY c.path,c.start_line,c.id LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []denseCandidate{}
	for rows.Next() {
		var row denseCandidate
		var vectorBody []byte
		if err := rows.Scan(
			&row.Chunk.ID, &row.Chunk.DocumentID, &row.Chunk.Path, &row.Chunk.Tier,
			&row.Chunk.Heading, &row.Chunk.StartLine, &row.Chunk.EndLine,
			&row.Chunk.Content, &row.Chunk.ContentHash, &row.Chunk.ParentID,
			&row.Chunk.PreviousID, &row.Chunk.NextID, &row.DocumentHash, &vectorBody,
		); err != nil {
			return nil, err
		}
		if len(vectorBody)%4 != 0 {
			return nil, errors.New("persisted embedding vector is not int32-aligned")
		}
		vector, err := decodeVector(vectorBody, len(vectorBody)/4)
		if err != nil {
			return nil, err
		}
		row.vector = vector
		result = append(result, row)
	}
	return result, rows.Err()
}

func rankGraph(
	ctx context.Context,
	db *sql.DB,
	seeds []*candidate,
	tiers []string,
	limit int,
) ([]candidate, error) {
	if len(seeds) > 30 {
		seeds = seeds[:30]
	}
	tierSet := map[string]bool{}
	for _, tier := range tiers {
		tierSet[tier] = true
	}
	seen := map[string]bool{}
	result := []candidate{}
	for _, seed := range seeds {
		rows, err := db.QueryContext(ctx, `SELECT DISTINCT c.id,c.document_id,c.path,c.tier,
			c.heading,c.start_line,c.end_line,c.content,c.content_hash,
			COALESCE(c.parent_id,''),COALESCE(c.previous_id,''),COALESCE(c.next_id,''),
			d.content_hash
			FROM edges e
			JOIN chunks c ON c.id=CASE WHEN e.source_id=? THEN e.target_id ELSE e.source_id END
			JOIN documents d ON d.id=c.document_id
			WHERE e.source_id=? OR e.target_id=?
			ORDER BY CASE e.kind WHEN 'depends-on' THEN 0 WHEN 'link' THEN 1
				WHEN 'section' THEN 2 ELSE 3 END,c.path,c.start_line,c.id`,
			seed.Chunk.ID, seed.Chunk.ID, seed.Chunk.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row candidate
			if err := rows.Scan(
				&row.Chunk.ID, &row.Chunk.DocumentID, &row.Chunk.Path, &row.Chunk.Tier,
				&row.Chunk.Heading, &row.Chunk.StartLine, &row.Chunk.EndLine,
				&row.Chunk.Content, &row.Chunk.ContentHash, &row.Chunk.ParentID,
				&row.Chunk.PreviousID, &row.Chunk.NextID, &row.DocumentHash,
			); err != nil {
				rows.Close()
				return nil, err
			}
			if tierSet[row.Chunk.Tier] && !seen[row.Chunk.ID] {
				seen[row.Chunk.ID] = true
				result = append(result, row)
				if len(result) >= limit {
					rows.Close()
					return result, nil
				}
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func sortedCandidates(rows map[string]*candidate, weights map[string]int, k int) []*candidate {
	result := make([]*candidate, 0, len(rows))
	for _, row := range rows {
		var score int64
		for lane, rank := range row.LaneRanks {
			score += int64(weights[lane]) * 1_000_000_000 / int64(k+rank)
		}
		row.Fusion = score
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Fusion != result[j].Fusion {
			return result[i].Fusion > result[j].Fusion
		}
		return candidateLess(result[i], result[j])
	})
	return result
}

func candidateLess(left, right *candidate) bool {
	if left.Chunk.Path != right.Chunk.Path {
		return left.Chunk.Path < right.Chunk.Path
	}
	if left.Chunk.StartLine != right.Chunk.StartLine {
		return left.Chunk.StartLine < right.Chunk.StartLine
	}
	return left.Chunk.ID < right.Chunk.ID
}

func (retriever Retriever) verifyChunk(chunk Chunk, documentHash string) bool {
	body, hash, err := ReadProjectFile(retriever.Boundary, chunk.Path)
	if err != nil || hash != documentHash {
		return false
	}
	lines := strings.Split(string(normalizeNewlines(body)), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if chunk.StartLine < 1 || chunk.EndLine > len(lines) || chunk.StartLine > chunk.EndLine {
		return false
	}
	content := strings.Join(lines[chunk.StartLine-1:chunk.EndLine], "\n")
	return SHA256String(content) == chunk.ContentHash && content == chunk.Content
}

func cloneRanks(input map[string]int) map[string]int {
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func classifyQuery(query string) string {
	lower := strings.ToLower(query)
	if strings.ContainsAny(query, `/\`) || strings.Contains(lower, ".md") ||
		strings.Contains(lower, "0x") || strings.Contains(lower, "sha256") {
		return "exact"
	}
	if strings.Contains(lower, "depend") || strings.Contains(lower, "invalidate") {
		return "dependency"
	}
	if strings.Contains(lower, "history") || strings.Contains(lower, "previous") ||
		strings.Contains(lower, "provenance") {
		return "provenance"
	}
	if strings.Contains(lower, "contradict") || strings.Contains(lower, "conflict") {
		return "contradiction"
	}
	if strings.Contains(lower, "orient") || strings.Contains(lower, "overview") {
		return "orientation"
	}
	return "conceptual"
}

func shouldRerank(class string, count int) bool {
	if count < 2 {
		return false
	}
	return class == "conceptual" || class == "orientation" ||
		class == "dependency" || class == "contradiction"
}

func lineRangeBody(body []byte, start, end int) (string, error) {
	lines := strings.Split(string(normalizeNewlines(body)), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = len(lines)
	}
	if start < 1 || end < start || end > len(lines) {
		return "", fmt.Errorf("invalid line range %s-%s", strconv.Itoa(start), strconv.Itoa(end))
	}
	return strings.Join(lines[start-1:end], "\n"), nil
}
