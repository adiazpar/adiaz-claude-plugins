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
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Status  string `json:"status,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Grade   string `json:"grade,omitempty"`
}

// Query refreshes the index if stale (never failing the query for it),
// then returns ranked hits. Superseded docs sort last, labeled by the
// caller via FormatHits.
func Query(root, q string, limit int) ([]Hit, []string, error) {
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
	if limit <= 0 {
		limit = 5
	}
	rows, err := db.Query(`
		SELECT path, title, status, kind, grade,
		       snippet(docs, 1, '', '', '…', 16)
		FROM docs WHERE docs MATCH ?
		ORDER BY (status = 'superseded') ASC, rank
		LIMIT ?`, match, limit)
	if err != nil {
		return nil, warnings, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Path, &h.Title, &h.Status, &h.Kind, &h.Grade, &h.Snippet); err != nil {
			return nil, warnings, err
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
