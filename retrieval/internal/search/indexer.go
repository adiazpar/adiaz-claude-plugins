package search

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// IndexPath returns the canonical index location for a project root.
func IndexPath(root string) string {
	return filepath.Join(root, ".re-discipline", "index.db")
}

// indexFormatVersion is stored in the indexmeta table and checked by
// schemaCurrent. Bump it whenever indexing CONTENT changes (stripping,
// tokenization, column composition), not just table shape — an index
// built by an older binary must be rebuilt even when the corpus is
// unchanged, or queries silently serve stale term data.
const indexFormatVersion = "2"

// relationLinkRe matches "- <Label>: ..." bullets inside a Relations
// section ("- Depends on:", "- Split from:", and future labels alike).
var relationLinkRe = regexp.MustCompile(`^\s*-\s+[A-Za-z][A-Za-z ]*:\s`)

// StripRelationLinks removes cross-reference link bullets from a doc's
// "## Relations" section for INDEXING only — the file is untouched.
// Those bullets quote other docs' titles verbatim (corpus survey: 1,516
// "- Depends on:" + 130 "- Split from:" lines across 243 docs), which
// makes every doc compete for every related doc's topic terms. Free
// prose inside the section is real content and is kept.
func StripRelationLinks(body string) string {
	if !strings.Contains(body, "## Relations") {
		return body
	}
	lines := strings.Split(body, "\n")
	out := lines[:0]
	inRelations := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			inRelations = trimmed == "## Relations"
		}
		if inRelations && relationLinkRe.MatchString(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// BuildIndexFile writes a complete FTS5 index + manifest to dbPath.
// Corpus problems degrade to warnings; only I/O or SQL failures error.
func BuildIndexFile(root, dbPath string) ([]Doc, []string, error) {
	metas, err := ScanDocs(root)
	if err != nil {
		return nil, nil, err
	}
	docs, warnings := LoadDocs(root, metas)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, warnings, err
	}
	defer db.Close()

	stmts := []string{
		`CREATE VIRTUAL TABLE docs USING fts5(title, body, idents, path UNINDEXED, status UNINDEXED, kind UNINDEXED, grade UNINDEXED)`,
		`CREATE TABLE manifest(path TEXT PRIMARY KEY, mtime INTEGER, size INTEGER)`,
		// Per-doc metadata that must stay out of the searchable text
		// (evidence holds file paths — indexing them would add noise).
		`CREATE TABLE docmeta(path TEXT PRIMARY KEY, evidence TEXT)`,
		`CREATE TABLE indexmeta(key TEXT PRIMARY KEY, value TEXT)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, warnings, err
		}
	}
	if _, err := db.Exec(`INSERT INTO indexmeta(key, value) VALUES('format', ?)`, indexFormatVersion); err != nil {
		return nil, warnings, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, warnings, err
	}
	for _, d := range docs {
		indexBody := StripRelationLinks(d.Body)
		parts := ExpandIdentifiers(d.Title + " " + indexBody)
		parts = append(parts, d.Tags...)
		// Declared idents go in both verbatim-lowercased and expanded,
		// so a declared ai_forceIdle matches queries for ai_forceIdle,
		// forceIdle, force, and idle.
		for _, id := range d.Idents {
			parts = append(parts, strings.ToLower(id))
		}
		if len(d.Idents) > 0 {
			parts = append(parts, ExpandIdentifiers(strings.Join(d.Idents, " "))...)
		}
		idents := strings.Join(parts, " ")
		if _, err := tx.Exec(`INSERT INTO docs(title, body, idents, path, status, kind, grade) VALUES(?,?,?,?,?,?,?)`,
			d.Title, indexBody, idents, d.Path, d.Status, d.Kind, d.Grade); err != nil {
			tx.Rollback()
			return nil, warnings, err
		}
		if len(d.Evidence) > 0 {
			if _, err := tx.Exec(`INSERT INTO docmeta(path, evidence) VALUES(?,?)`,
				d.Path, strings.Join(d.Evidence, "\n")); err != nil {
				tx.Rollback()
				return nil, warnings, err
			}
		}
	}
	for _, m := range metas {
		if _, err := tx.Exec(`INSERT INTO manifest(path, mtime, size) VALUES(?,?,?)`, m.Path, m.MTime, m.Size); err != nil {
			tx.Rollback()
			return nil, warnings, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, warnings, err
	}
	return docs, warnings, nil
}

// ReadManifest loads the stored file manifest from an index database.
func ReadManifest(dbPath string) (map[string]FileMeta, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT path, mtime, size FROM manifest`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FileMeta{}
	for rows.Next() {
		var m FileMeta
		if err := rows.Scan(&m.Path, &m.MTime, &m.Size); err != nil {
			return nil, err
		}
		out[m.Path] = m
	}
	return out, rows.Err()
}
