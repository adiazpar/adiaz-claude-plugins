package search

import (
	"database/sql"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// IndexPath returns the canonical index location for a project root.
func IndexPath(root string) string {
	return filepath.Join(root, ".re-discipline", "index.db")
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
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, warnings, err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, warnings, err
	}
	for _, d := range docs {
		idents := strings.Join(ExpandIdentifiers(d.Title+" "+d.Body), " ")
		if len(d.Tags) > 0 {
			idents += " " + strings.Join(d.Tags, " ")
		}
		if _, err := tx.Exec(`INSERT INTO docs(title, body, idents, path, status, kind, grade) VALUES(?,?,?,?,?,?,?)`,
			d.Title, d.Body, idents, d.Path, d.Status, d.Kind, d.Grade); err != nil {
			tx.Rollback()
			return nil, warnings, err
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
