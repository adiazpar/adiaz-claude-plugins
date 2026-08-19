package search

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// Hit is one ranked query result.
type Hit struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Snippet  string   `json:"snippet"`
	Status   string   `json:"status,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Grade    string   `json:"grade,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// QueryOptions carries optional constraints for QueryOpts. The zero
// value means "current default behavior": limit 5, no filtering.
type QueryOptions struct {
	Limit int    // <= 0 means 5
	Kind  string // filter to this kind (fact|ops); empty = no filter
	Grade string // filter to this grade (direct|inferred|reported); empty = no filter
}

// BM25 column weights for the three indexed columns (title, body,
// idents). Weight arguments map positionally onto ALL table columns,
// UNINDEXED ones included, and missing trailing weights default to 1.0
// — the four UNINDEXED columns trail the indexed three, so passing just
// these three is exact (pinned in TestBM25WeightBehavior). Values chosen
// by sweeping combinations against golden.jsonl; identifier-bearing
// titles and the idents column must outrank incidental body mentions.
const (
	weightTitle  = 4.0
	weightBody   = 1.0
	weightIdents = 2.0
)

// Query refreshes the index if stale (never failing the query for it),
// then returns ranked hits. Superseded docs sort last, labeled by the
// caller via FormatHits.
func Query(root, q string, limit int) ([]Hit, []string, error) {
	return QueryOpts(root, q, QueryOptions{Limit: limit})
}

// QueryOpts is Query with optional kind/grade filtering.
func QueryOpts(root, q string, opts QueryOptions) ([]Hit, []string, error) {
	warnings := EnsureFresh(root)
	match := BuildMatch(q)
	if match == "" {
		return nil, warnings, nil
	}
	dbPath := IndexPath(root)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, append(warnings, "no index and rebuild unavailable; no results"), nil
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, warnings, err
	}
	defer db.Close()
	if opts.Limit <= 0 {
		opts.Limit = 5
	}
	// bm25() returns negative scores (more negative = better), so the
	// ascending ORDER BY is correct; pinned in TestBM25WeightBehavior.
	q1 := `
		SELECT docs.path, docs.title, docs.status, docs.kind, docs.grade,
		       snippet(docs, 1, '«', '»', '…', 40),
		       COALESCE(docmeta.evidence, '')
		FROM docs LEFT JOIN docmeta ON docmeta.path = docs.path
		WHERE docs MATCH ?`
	args := []any{match}
	if opts.Kind != "" {
		q1 += ` AND docs.kind = ?`
		args = append(args, opts.Kind)
	}
	if opts.Grade != "" {
		q1 += ` AND docs.grade = ?`
		args = append(args, opts.Grade)
	}
	q1 += fmt.Sprintf(`
		ORDER BY (docs.status = 'superseded') ASC, bm25(docs, %v, %v, %v)
		LIMIT ?`, weightTitle, weightBody, weightIdents)
	args = append(args, opts.Limit)
	rows, err := db.Query(q1, args...)
	if err != nil {
		return nil, warnings, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var evidence string
		if err := rows.Scan(&h.Path, &h.Title, &h.Status, &h.Kind, &h.Grade, &h.Snippet, &evidence); err != nil {
			return nil, warnings, err
		}
		if evidence != "" {
			h.Evidence = strings.Split(evidence, "\n")
		}
		hits = append(hits, h)
	}
	return hits, warnings, rows.Err()
}

// FormatHits renders hits as a numbered plain-text list for terminals
// and MCP text content.
func FormatHits(hits []Hit) string {
	if len(hits) == 0 {
		return "No results. Try rewording, or grep .re-discipline/docs/ directly.\n"
	}
	var b strings.Builder
	for i, h := range hits {
		label := strings.Join(compact(h.Status, h.Kind, h.Grade), "/")
		prefix := ""
		if h.Status == "superseded" {
			prefix = "[SUPERSEDED] "
		}
		fmt.Fprintf(&b, "%d. %s[%s] %s\n   %s\n   %s\n", i+1, prefix, label, h.Path, h.Title, h.Snippet)
	}
	return b.String()
}
