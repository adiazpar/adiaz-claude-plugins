# re-discipline 1.0 Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the 0.x re-discipline engine with the 1.0 product: markdown-canonical curation conventions, a small ephemeral Go retrieval CLI (`re-search`), 4 skills, one hook, and single-job Windows CI.

**Architecture:** Curated markdown under `.re-discipline/docs/` is the source of truth; `re-search` builds a disposable SQLite FTS5 index from it and answers queries via CLI, MCP stdio, and HTTP. All 0.x machinery (Go engine, schemas, write-guard hooks, 8 of 12 skills) is deleted. The plugin repo ships the source, a committed windows-amd64 exe, templates, skills, and one SessionStart hook.

**Tech Stack:** Go (pinned toolchain, pure-Go `modernc.org/sqlite` with FTS5, `golang.org/x/sys` for Windows locking), PowerShell (hook + build script), GitHub Actions (one Windows job), Claude Code / Codex plugin manifests.

**Spec:** `docs/superpowers/specs/2026-08-17-re-discipline-simplification-design.md`

## Global Constraints

- Windows-only: no mac/linux binaries, hooks, or CI jobs.
- SQLite driver MUST be `modernc.org/sqlite` (pure Go, FTS5). No cgo anywhere.
- Canonical build command, shared verbatim by release and CI (via `scripts/build.ps1`): `go build -trimpath -buildvcs=false -ldflags "-X main.version=X.Y.Z"`.
- Go toolchain pinned via the `toolchain` directive in `retrieval/go.mod`; CI uses `go-version-file`.
- A query must NEVER fail or block because a reindex could not complete.
- Indexing is lenient: malformed frontmatter → indexed as plain text + warning; indexing never fails on doc content.
- Any `index.db` open/read error → silent delete-and-rebuild.
- Staleness = manifest diff of `(path, mtime, size)` over `.re-discipline/docs/**/*.md`, excluding `INDEX.md`.
- Reindex locking: OS-held exclusive file handle (kernel releases on process death). Never a bare marker file. Index swap: retry with backoff; on persistent failure serve the existing index.
- Doc paths in all outputs and `golden.jsonl` are relative to `.re-discipline/`, forward slashes (e.g. `docs/engine/joint-binding.md`).
- Superseded hits are labeled and downranked, never hidden.
- Version 1.0.0 in `.claude-plugin/plugin.json` (canonical), matched by `.codex-plugin/plugin.json` and marketplace entries.
- Commit messages end with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` (omitted from per-task snippets below for brevity — always append it).

---

### Task 1: Land the staged single-plugin restructure

The repo has an in-flight, user-authored restructure staged (moves `plugins/re-discipline/*` to repo root, removes the multi-plugin layout). Land it as-is so demolition and building start from a clean tree.

**Files:**
- Modify: none by hand — commit what is already staged/modified.

- [ ] **Step 1: Inspect what will be committed**

Run: `git status` and `git diff --stat` (unstaged) — confirm the change set is the plugin-to-root rename plus its follow-on edits (README, .gitattributes, workflows, marketplace manifests). Confirm nothing looks like a secret or unrelated work. If anything unrelated appears, stop and report instead of committing.

- [ ] **Step 2: Stage everything and commit**

```bash
git add -A
git commit -m "Restructure repo to single-plugin root layout"
```

- [ ] **Step 3: Verify clean tree**

Run: `git status` → expect "nothing to commit, working tree clean".

### Task 2: Demolish the 0.x machinery

**Files:**
- Delete: `knowledge/` (entire tree: engine, schemas, models, evals, bin), `references/`, `hooks/` (all 0.x files), `templates/` (0.x campaign/finding/run JSON templates — recreated fresh in Task 14), `tests/` (0.x Python suite), `.github/scripts/`, `.github/workflows/re-discipline.yml`, `.mcp.json` (recreated in Task 13), the 12 0.x skill directories under `skills/`, and `plugins/` if any remnant exists.
- Keep: `LICENSE`, `README.md` (rewritten Task 17), `CHANGELOG.md`, `.claude-plugin/`, `.codex-plugin/`, `.agents/`, `.gitattributes` (simplified Task 13), `.claude/`, `docs/superpowers/`.

- [ ] **Step 1: Delete**

```bash
git rm -r -q knowledge references hooks templates tests .github/scripts .github/workflows/re-discipline.yml .mcp.json skills
git rm -r -q plugins 2>/dev/null || true
```

- [ ] **Step 2: Verify what remains**

Run: `git status --short | head -20` and `ls` → remaining tracked tree is manifests, LICENSE, README, CHANGELOG, `.claude/`, `.agents/`, `.gitattributes`, `docs/`. Nothing under `knowledge/`, `hooks/`, `references/`, `skills/`, `templates/`, or `tests/` remains.

- [ ] **Step 3: Commit**

```bash
git commit -m "Delete 0.x engine, schemas, hooks, references, and skills"
```

### Task 3: Go module scaffold + project-root discovery

**Files:**
- Create: `retrieval/go.mod`, `retrieval/internal/search/root.go`
- Test: `retrieval/internal/search/root_test.go`

**Interfaces:**
- Produces: `search.FindRoot(startDir string) (string, error)` — walks up from `startDir` to the nearest directory containing a `.re-discipline/` directory; returns that directory (the project root). Error if none found. Package path: `github.com/adiazpar/re-discipline/retrieval/internal/search`.

- [ ] **Step 1: Initialize the module with pinned toolchain**

```bash
cd retrieval
go mod init github.com/adiazpar/re-discipline/retrieval
go version   # note the exact local version, e.g. go1.24.5
```

Edit `retrieval/go.mod` so it contains a `toolchain` directive matching the local version exactly, e.g.:

```
module github.com/adiazpar/re-discipline/retrieval

go 1.24

toolchain go1.24.5
```

- [ ] **Step 2: Write the failing test**

`retrieval/internal/search/root_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootWalksUp(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".re-discipline", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "src", "game", "anim")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(root)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindRootNotFound(t *testing.T) {
	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Fatal("expected error when no .re-discipline exists")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/` → FAIL (FindRoot undefined).

- [ ] **Step 4: Implement**

`retrieval/internal/search/root.go`:

```go
// Package search implements the re-search corpus scanner, indexer, and
// query engine over a .re-discipline/ markdown knowledge base.
package search

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindRoot walks up from startDir to the nearest directory containing a
// .re-discipline directory and returns that directory as the project root.
func FindRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if fi, statErr := os.Stat(filepath.Join(dir, ".re-discipline")); statErr == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .re-discipline directory found walking up from %s", startDir)
		}
		dir = parent
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add retrieval
git commit -m "feat: retrieval module scaffold with project-root discovery"
```

### Task 4: Lenient frontmatter parsing

**Files:**
- Create: `retrieval/internal/search/frontmatter.go`
- Test: `retrieval/internal/search/frontmatter_test.go`

**Interfaces:**
- Produces:
  - `type Doc struct { Path, Title, Body, Status, Kind, Grade string; Tags []string; Warning string }`
  - `search.ParseDoc(relPath string, raw string) Doc` — parses optional `---` frontmatter. Lenient: unclosed or unparseable frontmatter ⇒ whole file becomes `Body`, `Warning` set, never an error. `Title` = first `# ` heading, else the filename stem. Handles both LF and CRLF input.

- [ ] **Step 1: Write the failing test**

`retrieval/internal/search/frontmatter_test.go`:

```go
package search

import "testing"

const goodDoc = `---
status: promoted
kind: fact
grade: direct
tags: [entities, animation]
evidence: [archive/demo/reports/R-007.md]
---
# Entity binding goes through idAnimatedEntity::AttachJoint

Body text here.
`

func TestParseDocFrontmatter(t *testing.T) {
	d := ParseDoc("docs/engine/joint-binding.md", goodDoc)
	if d.Status != "promoted" || d.Kind != "fact" || d.Grade != "direct" {
		t.Fatalf("fields: %+v", d)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "entities" {
		t.Fatalf("tags: %v", d.Tags)
	}
	if d.Title != "Entity binding goes through idAnimatedEntity::AttachJoint" {
		t.Fatalf("title: %q", d.Title)
	}
	if d.Warning != "" {
		t.Fatalf("unexpected warning: %q", d.Warning)
	}
}

func TestParseDocMalformedIsLenient(t *testing.T) {
	d := ParseDoc("docs/broken.md", "---\nstatus: promoted\nno closing fence\n# A heading\nbody")
	if d.Warning == "" {
		t.Fatal("want warning for unclosed frontmatter")
	}
	if d.Body == "" || d.Status != "" {
		t.Fatalf("malformed doc must fall back to plain text: %+v", d)
	}
}

func TestParseDocCRLFAndNoFrontmatter(t *testing.T) {
	d := ParseDoc("docs/x.md", "# Title line\r\nbody\r\n")
	if d.Title != "Title line" || d.Warning != "" {
		t.Fatalf("%+v", d)
	}
	d2 := ParseDoc("docs/spawn-limit.md", "just prose, no heading")
	if d2.Title != "spawn-limit" {
		t.Fatalf("filename-stem title fallback, got %q", d2.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run TestParseDoc` → FAIL.

- [ ] **Step 3: Implement**

`retrieval/internal/search/frontmatter.go`:

```go
package search

import (
	"path"
	"strings"
)

// Doc is one parsed markdown document from .re-discipline/docs/.
type Doc struct {
	Path    string // relative to .re-discipline/, forward slashes
	Title   string
	Body    string
	Status  string // promoted | superseded | candidate | ""
	Kind    string // fact | ops | ""
	Grade   string // direct | inferred | reported | ""
	Tags    []string
	Warning string // non-empty when frontmatter was malformed
}

// ParseDoc parses raw markdown with optional frontmatter. It is lenient:
// malformed frontmatter degrades to plain-text indexing with a Warning,
// never an error.
func ParseDoc(relPath, raw string) Doc {
	d := Doc{Path: relPath}
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	body := text
	if strings.HasPrefix(text, "---\n") {
		rest := text[len("---\n"):]
		if end := strings.Index(rest, "\n---"); end >= 0 {
			fm := rest[:end]
			after := rest[end+len("\n---"):]
			if nl := strings.Index(after, "\n"); nl >= 0 {
				body = after[nl+1:]
			} else {
				body = ""
			}
			parseFrontmatter(fm, &d)
		} else {
			d.Warning = "unclosed frontmatter; indexed as plain text"
		}
	}
	d.Body = body
	d.Title = titleOf(body, relPath)
	return d
}

func parseFrontmatter(fm string, d *Doc) {
	for _, line := range strings.Split(fm, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "status":
			d.Status = val
		case "kind":
			d.Kind = val
		case "grade":
			d.Grade = val
		case "tags":
			d.Tags = parseList(val)
		}
	}
}

func parseList(val string) []string {
	val = strings.Trim(val, "[]")
	var out []string
	for _, item := range strings.Split(val, ",") {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func titleOf(body, relPath string) string {
	for _, line := range strings.Split(body, "\n") {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	base := path.Base(relPath)
	return strings.TrimSuffix(base, path.Ext(base))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add retrieval/internal/search
git commit -m "feat: lenient frontmatter parsing for corpus docs"
```

### Task 5: Identifier expansion + FTS match building

**Files:**
- Create: `retrieval/internal/search/tokenizer.go`
- Test: `retrieval/internal/search/tokenizer_test.go`

**Interfaces:**
- Produces:
  - `search.ExpandIdentifiers(text string) []string` — finds code-identifier tokens (containing `::`, `_`, or camelCase transitions) and returns lowercased whole identifiers plus their split parts, deduplicated. Used at index time to fill an `idents` FTS column, so no custom SQLite tokenizer is needed.
  - `search.BuildMatch(q string) string` — turns a free-text question into a safe FTS5 MATCH expression: every word and every expanded identifier part, lowercased, double-quoted, joined with ` OR `. Returns `""` for an empty/unusable query.

- [ ] **Step 1: Write the failing test**

`retrieval/internal/search/tokenizer_test.go`:

```go
package search

import (
	"slices"
	"strings"
	"testing"
)

func TestExpandIdentifiers(t *testing.T) {
	got := ExpandIdentifiers("call idAnimatedEntity::AttachJoint and spawn_limit here")
	for _, want := range []string{"idanimatedentity", "attachjoint", "animated", "entity", "attach", "joint", "spawn", "limit", "spawn_limit"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if slices.Contains(got, "call") {
		t.Fatalf("plain words must not be expanded: %v", got)
	}
}

func TestBuildMatch(t *testing.T) {
	m := BuildMatch(`how do I bind entities to AttachJoint? "quotes" too`)
	if !strings.Contains(m, `"attachjoint"`) || !strings.Contains(m, `"entities"`) {
		t.Fatalf("match: %s", m)
	}
	if !strings.Contains(m, " OR ") {
		t.Fatalf("terms must be OR-joined: %s", m)
	}
	if strings.Contains(m, `""quotes""`) {
		t.Fatalf("embedded quotes must be stripped, got %s", m)
	}
	if BuildMatch("???") != "" {
		t.Fatal("unusable query must return empty match")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run 'TestExpand|TestBuildMatch'` → FAIL.

- [ ] **Step 3: Implement**

`retrieval/internal/search/tokenizer.go`:

```go
package search

import (
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_:]*[A-Za-z0-9]`)
var camelRe = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[a-z0-9]*)`)
var wordRe = regexp.MustCompile(`[A-Za-z0-9_:]+`)

// isIdentifier reports whether tok looks like a code identifier rather
// than a plain word: contains ::, _, or an internal case transition.
func isIdentifier(tok string) bool {
	if strings.Contains(tok, "::") || strings.Contains(tok, "_") {
		return true
	}
	hasLower, hasUpper := false, false
	for _, r := range tok[1:] { // ignore leading capital of ordinary words
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}

// ExpandIdentifiers returns lowercased whole identifiers and their split
// parts for every code-identifier token in text, deduplicated.
func ExpandIdentifiers(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.ToLower(s)
		if len(s) > 1 && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, tok := range identRe.FindAllString(text, -1) {
		if !isIdentifier(tok) {
			continue
		}
		add(strings.ReplaceAll(strings.ReplaceAll(tok, "::", ""), "_", ""))
		add(tok) // whole, verbatim-lowercased (keeps spawn_limit findable)
		for _, seg := range strings.FieldsFunc(tok, func(r rune) bool { return r == ':' || r == '_' }) {
			add(seg)
			for _, part := range camelRe.FindAllString(seg, -1) {
				add(part)
			}
		}
	}
	return out
}

// BuildMatch converts a free-text question into a safe FTS5 MATCH
// expression: quoted terms OR-joined. Empty string means "nothing usable".
func BuildMatch(q string) string {
	seen := map[string]bool{}
	var terms []string
	add := func(s string) {
		s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, `"`, ""), ":", " "))
		for _, w := range strings.Fields(s) {
			if len(w) > 1 && !seen[w] {
				seen[w] = true
				terms = append(terms, `"`+w+`"`)
			}
		}
	}
	for _, tok := range wordRe.FindAllString(q, -1) {
		add(tok)
	}
	for _, part := range ExpandIdentifiers(q) {
		add(part)
	}
	return strings.Join(terms, " OR ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add retrieval/internal/search
git commit -m "feat: identifier-aware term expansion and FTS match building"
```

### Task 6: Corpus scan + staleness manifest model

**Files:**
- Create: `retrieval/internal/search/corpus.go`
- Test: `retrieval/internal/search/corpus_test.go`

**Interfaces:**
- Produces:
  - `type FileMeta struct { Path string; MTime int64; Size int64 }` (`Path` relative to `.re-discipline/`, forward slashes)
  - `search.ScanDocs(root string) ([]FileMeta, error)` — all `*.md` under `<root>/.re-discipline/docs/`, recursive, excluding `docs/INDEX.md`, sorted by `Path`. Missing `docs/` dir ⇒ empty slice, nil error.
  - `search.LoadDocs(root string, metas []FileMeta) (docs []Doc, warnings []string)` — reads and `ParseDoc`s each file; unreadable file ⇒ skipped with a warning (lenient); warnings carry the doc path.
  - `search.ManifestDiffers(stored map[string]FileMeta, current []FileMeta) bool` — set + (mtime,size) comparison; detects adds, edits, deletions, and moves.

- [ ] **Step 1: Write the failing test**

`retrieval/internal/search/corpus_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, ".re-discipline", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanDocsExcludesIndexMD(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/INDEX.md", "# Index")
	writeDoc(t, root, "docs/engine/a.md", "# A")
	writeDoc(t, root, "docs/ops/b.md", "# B")
	writeDoc(t, root, "docs/notes.txt", "not markdown")
	metas, err := ScanDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 || metas[0].Path != "docs/engine/a.md" || metas[1].Path != "docs/ops/b.md" {
		t.Fatalf("metas: %+v", metas)
	}
}

func TestScanDocsMissingDirIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".re-discipline"), 0o755); err != nil {
		t.Fatal(err)
	}
	metas, err := ScanDocs(root)
	if err != nil || len(metas) != 0 {
		t.Fatalf("want empty, got %v, %v", metas, err)
	}
}

func TestManifestDiffersDetectsDeletion(t *testing.T) {
	stored := map[string]FileMeta{
		"docs/a.md": {Path: "docs/a.md", MTime: 1, Size: 10},
		"docs/b.md": {Path: "docs/b.md", MTime: 1, Size: 10},
	}
	current := []FileMeta{{Path: "docs/a.md", MTime: 1, Size: 10}}
	if !ManifestDiffers(stored, current) {
		t.Fatal("deletion must mark index stale")
	}
	same := []FileMeta{{Path: "docs/a.md", MTime: 1, Size: 10}, {Path: "docs/b.md", MTime: 1, Size: 10}}
	if ManifestDiffers(stored, same) {
		t.Fatal("identical manifest must not be stale")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run 'TestScan|TestManifest'` → FAIL.

- [ ] **Step 3: Implement**

`retrieval/internal/search/corpus.go`:

```go
package search

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileMeta identifies one corpus file for staleness comparison.
type FileMeta struct {
	Path  string // relative to .re-discipline/, forward slashes
	MTime int64  // unix nanoseconds
	Size  int64
}

// ScanDocs lists every *.md under <root>/.re-discipline/docs/ except
// docs/INDEX.md, sorted by path. A missing docs dir yields an empty list.
func ScanDocs(root string) ([]FileMeta, error) {
	base := filepath.Join(root, ".re-discipline")
	docsDir := filepath.Join(base, "docs")
	var metas []FileMeta
	err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return filepath.SkipAll
			}
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(base, p)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "docs/INDEX.md" {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil // vanished mid-scan: skip
		}
		metas = append(metas, FileMeta{Path: relSlash, MTime: info.ModTime().UnixNano(), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Path < metas[j].Path })
	return metas, nil
}

// LoadDocs reads and parses each listed file. Unreadable files are
// skipped with a warning — corpus content never fails a build.
func LoadDocs(root string, metas []FileMeta) ([]Doc, []string) {
	var docs []Doc
	var warnings []string
	for _, m := range metas {
		raw, err := os.ReadFile(filepath.Join(root, ".re-discipline", filepath.FromSlash(m.Path)))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v (skipped)", m.Path, err))
			continue
		}
		d := ParseDoc(m.Path, string(raw))
		if d.Warning != "" {
			warnings = append(warnings, m.Path+": "+d.Warning)
		}
		docs = append(docs, d)
	}
	return docs, warnings
}

// ManifestDiffers reports whether the stored index manifest disagrees
// with the current scan — including deletions and moves.
func ManifestDiffers(stored map[string]FileMeta, current []FileMeta) bool {
	if len(stored) != len(current) {
		return true
	}
	for _, m := range current {
		s, ok := stored[m.Path]
		if !ok || s.MTime != m.MTime || s.Size != m.Size {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add retrieval/internal/search
git commit -m "feat: corpus scanning and staleness manifest comparison"
```

### Task 7: Index build (FTS5) + INDEX.md generation

**Files:**
- Create: `retrieval/internal/search/indexer.go`, `retrieval/internal/search/indexmd.go`
- Test: `retrieval/internal/search/indexer_test.go`
- Modify: `retrieval/go.mod` (adds `modernc.org/sqlite`)

**Interfaces:**
- Consumes: `ScanDocs`, `LoadDocs`, `ExpandIdentifiers`, `Doc`, `FileMeta`.
- Produces:
  - `search.IndexPath(root string) string` → `<root>/.re-discipline/index.db`.
  - `search.BuildIndexFile(root, dbPath string) (docs []Doc, warnings []string, err error)` — creates a complete index database at `dbPath` (caller chooses temp vs final): FTS5 table `docs(title, body, idents, path UNINDEXED, status UNINDEXED, kind UNINDEXED, grade UNINDEXED)` + plain table `manifest(path TEXT PRIMARY KEY, mtime INTEGER, size INTEGER)`. `idents` holds `ExpandIdentifiers(title+body)` joined by spaces; tags are appended to `idents` so tag words are searchable.
  - `search.WriteIndexMD(root string, docs []Doc) error` — regenerates `.re-discipline/docs/INDEX.md`, deterministic: sections per directory (sorted), entries `- [Title](relative-link) — status, kind/grade, tags`.
  - `search.ReadManifest(dbPath string) (map[string]FileMeta, error)`.

- [ ] **Step 1: Add the SQLite dependency**

```bash
cd retrieval
go get modernc.org/sqlite@latest
```

(Do NOT run `go mod tidy` yet — no file imports the driver until Step 4, so tidy would drop the requirement and the first `go test` would fail with a "to add it: go get" error. Run `go mod tidy` after Step 4 instead.)

- [ ] **Step 2: Write the failing test**

`retrieval/internal/search/indexer_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildTestCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeDoc(t, root, "docs/engine/joint-binding.md", `---
status: promoted
kind: fact
grade: direct
tags: [entities]
---
# Entity binding goes through idAnimatedEntity::AttachJoint

Bind entities to demon joints with AttachJoint.
`)
	writeDoc(t, root, "docs/engine/spawn-limit.md", `---
status: superseded
kind: fact
grade: inferred
---
# Snapmap spawn limit is 12 concurrent AI

Old superseded claim.
`)
	writeDoc(t, root, "docs/ops/ghidra.md", `---
kind: ops
---
# Ghidra project lives at G:\\snaphak\\doom.gpr

Open with Ghidra 11.
`)
	writeDoc(t, root, "docs/broken.md", "---\nstatus: promoted\nunclosed")
	return root
}

func TestBuildIndexFileAndManifest(t *testing.T) {
	root := buildTestCorpus(t)
	dbPath := filepath.Join(root, ".re-discipline", "index.db")
	docs, warnings, err := BuildIndexFile(root, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 4 {
		t.Fatalf("want 4 docs, got %d", len(docs))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "docs/broken.md") {
		t.Fatalf("want one lenient warning for broken doc, got %v", warnings)
	}
	stored, err := ReadManifest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := ScanDocs(root)
	if ManifestDiffers(stored, current) {
		t.Fatal("fresh index must match current scan")
	}
}

func TestWriteIndexMD(t *testing.T) {
	root := buildTestCorpus(t)
	metas, _ := ScanDocs(root)
	docs, _ := LoadDocs(root, metas)
	if err := WriteIndexMD(root, docs); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".re-discipline", "docs", "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "engine/joint-binding.md") || !strings.Contains(s, "superseded") {
		t.Fatalf("INDEX.md content:\n%s", s)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run 'TestBuildIndex|TestWriteIndexMD'` → FAIL.

- [ ] **Step 4: Implement**

`retrieval/internal/search/indexer.go`:

```go
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
```

`retrieval/internal/search/indexmd.go`:

```go
package search

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// WriteIndexMD regenerates docs/INDEX.md deterministically from parsed
// docs. INDEX.md is a derived artifact: clobbers cost nothing because
// this function is the recovery.
func WriteIndexMD(root string, docs []Doc) error {
	byDir := map[string][]Doc{}
	for _, d := range docs {
		dir := path.Dir(strings.TrimPrefix(d.Path, "docs/"))
		if dir == "." {
			dir = "(root)"
		}
		byDir[dir] = append(byDir[dir], d)
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var b strings.Builder
	b.WriteString("# Knowledge Index\n\nGenerated by `re-search index`. Do not edit by hand.\n")
	for _, dir := range dirs {
		b.WriteString("\n## " + dir + "\n\n")
		entries := byDir[dir]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		for _, d := range entries {
			link := strings.TrimPrefix(d.Path, "docs/")
			meta := strings.TrimSuffix(strings.Join(compact(d.Status, d.Kind, d.Grade), ", "), ", ")
			line := fmt.Sprintf("- [%s](%s)", d.Title, link)
			if meta != "" {
				line += " — " + meta
			}
			if len(d.Tags) > 0 {
				line += " [" + strings.Join(d.Tags, ", ") + "]"
			}
			b.WriteString(line + "\n")
		}
	}
	return os.WriteFile(filepath.Join(root, ".re-discipline", "docs", "INDEX.md"), []byte(b.String()), 0o644)
}

func compact(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add retrieval
git commit -m "feat: FTS5 index build with manifest and INDEX.md generation"
```

### Task 8: Windows lock, swap-with-retry, EnsureFresh

**Files:**
- Create: `retrieval/internal/search/lock_windows.go`, `retrieval/internal/search/fresh.go`
- Test: `retrieval/internal/search/fresh_test.go`
- Modify: `retrieval/go.mod` (adds `golang.org/x/sys`)

**Interfaces:**
- Consumes: `BuildIndexFile`, `ReadManifest`, `ScanDocs`, `ManifestDiffers`, `IndexPath`.
- Produces:
  - `search.TryLock(root string) (release func(), ok bool)` — OS-held exclusive handle on `.re-discipline/index.lock` via `windows.CreateFile` with share mode 0. The kernel releases it on process death; a killed rebuilder can never orphan the lock. `ok=false` when another process holds it.
  - `search.SwapIndex(tmpPath, dstPath string) error` — up to 10 `os.Rename` attempts with 100 ms backoff (Windows replace-over-open-file fails with sharing violations); on final failure removes the temp file and returns the error.
  - `search.EnsureFresh(root string) (warnings []string)` — the binding auto-reindex rule. Never returns an error: a query never fails or blocks because a reindex could not complete. Behavior: unreadable/corrupt `index.db` ⇒ delete it; stale or missing ⇒ if `TryLock` succeeds, `BuildIndexFile` into `index.tmp-<pid>.db` then `SwapIndex`; if the lock is busy or the swap fails, the existing index serves and problems are reported as warnings only.

- [ ] **Step 1: Add x/sys dependency**

```bash
cd retrieval
go get golang.org/x/sys@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test**

`retrieval/internal/search/fresh_test.go`:

```go
package search

import (
	"os"
	"testing"
)

func TestTryLockExcludesSecondHolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/.re-discipline", 0o755); err != nil {
		t.Fatal(err)
	}
	release, ok := TryLock(root)
	if !ok {
		t.Fatal("first lock must succeed")
	}
	if _, ok2 := TryLock(root); ok2 {
		t.Fatal("second lock must fail while first is held")
	}
	release()
	release2, ok3 := TryLock(root)
	if !ok3 {
		t.Fatal("lock must be reacquirable after release")
	}
	release2()
}

func TestEnsureFreshBuildsAndHealsCorruption(t *testing.T) {
	root := buildTestCorpus(t)

	warnings := EnsureFresh(root)
	if _, err := os.Stat(IndexPath(root)); err != nil {
		t.Fatalf("index must exist after EnsureFresh: %v (warnings %v)", err, warnings)
	}

	// Corrupt the index; EnsureFresh must silently delete and rebuild.
	if err := os.WriteFile(IndexPath(root), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	EnsureFresh(root)
	if _, err := ReadManifest(IndexPath(root)); err != nil {
		t.Fatalf("index must be rebuilt after corruption: %v", err)
	}

	// Fresh index + unchanged corpus: EnsureFresh must be a no-op.
	before, _ := os.Stat(IndexPath(root))
	EnsureFresh(root)
	after, _ := os.Stat(IndexPath(root))
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged corpus must not trigger rebuild")
	}
}

func TestEnsureFreshLockBusyServesExisting(t *testing.T) {
	root := buildTestCorpus(t)
	EnsureFresh(root)
	// Make corpus stale, then hold the lock as "another process".
	writeDoc(t, root, "docs/engine/new.md", "# New fact\nbody")
	release, ok := TryLock(root)
	if !ok {
		t.Fatal("setup lock failed")
	}
	defer release()
	warnings := EnsureFresh(root) // must return promptly, not block or panic
	_ = warnings
	if _, err := ReadManifest(IndexPath(root)); err != nil {
		t.Fatalf("existing index must remain usable: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run 'TestTryLock|TestEnsureFresh'` → FAIL.

- [ ] **Step 4: Implement**

`retrieval/internal/search/lock_windows.go`:

```go
//go:build windows

package search

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// TryLock acquires the project's rebuild lock as an OS-held exclusive
// file handle (share mode 0). The kernel releases it if the process
// dies, so a killed rebuilder can never orphan the lock.
func TryLock(root string) (func(), bool) {
	lockPath := filepath.Join(root, ".re-discipline", "index.lock")
	p, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, false
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_WRITE,
		0, // no sharing: exclusive
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return nil, false
	}
	return func() { windows.CloseHandle(h) }, true
}
```

`retrieval/internal/search/fresh.go`:

```go
package search

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SwapIndex renames tmpPath over dstPath. Windows refuses to replace a
// file another process has open, so retry with backoff; on persistent
// failure remove the temp file and report — callers treat this as
// non-fatal (the existing index keeps serving).
func SwapIndex(tmpPath, dstPath string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(tmpPath, dstPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	os.Remove(tmpPath)
	return fmt.Errorf("could not swap index into place: %w", err)
}

// EnsureFresh implements the binding auto-reindex rule: a query never
// fails or blocks because a reindex could not complete. It returns
// warnings only.
func EnsureFresh(root string) []string {
	var warnings []string
	dbPath := IndexPath(root)

	current, err := ScanDocs(root)
	if err != nil {
		return append(warnings, fmt.Sprintf("corpus scan failed: %v", err))
	}

	stale := false
	if stored, err := ReadManifest(dbPath); err != nil {
		// Missing or corrupt index: it is disposable — delete and rebuild.
		if rmErr := os.Remove(dbPath); rmErr != nil && !os.IsNotExist(rmErr) {
			warnings = append(warnings, fmt.Sprintf("index unreadable and could not be removed (%v); retrying next query", rmErr))
		}
		stale = true
	} else if ManifestDiffers(stored, current) {
		stale = true
	}
	if !stale {
		return warnings
	}

	release, ok := TryLock(root)
	if !ok {
		return append(warnings, "another process is rebuilding the index; serving existing index")
	}
	defer release()

	// Holding the lock, sweep temp debris from rebuilders that were
	// killed mid-build (only the owner ever removed its own temp).
	if leftovers, _ := filepath.Glob(filepath.Join(root, ".re-discipline", "index.tmp-*.db")); leftovers != nil {
		for _, l := range leftovers {
			os.Remove(l)
		}
	}
	os.Remove(filepath.Join(root, ".re-discipline", "index.db.build"))

	tmp := filepath.Join(root, ".re-discipline", fmt.Sprintf("index.tmp-%d.db", os.Getpid()))
	_, buildWarnings, err := BuildIndexFile(root, tmp)
	warnings = append(warnings, buildWarnings...)
	if err != nil {
		os.Remove(tmp)
		return append(warnings, fmt.Sprintf("index rebuild failed: %v; serving existing index if present", err))
	}
	if err := SwapIndex(tmp, dbPath); err != nil {
		return append(warnings, err.Error()+"; serving existing index")
	}
	return warnings
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add retrieval
git commit -m "feat: kernel-released rebuild lock, swap-with-retry, EnsureFresh"
```

### Task 9: Query + CLI entrypoint

**Files:**
- Create: `retrieval/internal/search/query.go`, `retrieval/cmd/re-search/main.go`
- Test: `retrieval/internal/search/query_test.go`

**Interfaces:**
- Consumes: `EnsureFresh`, `IndexPath`, `BuildMatch`, `BuildIndexFile`, `SwapIndex`, `TryLock`, `WriteIndexMD`, `FindRoot`.
- Produces:
  - `type Hit struct { Path, Title, Snippet, Status, Kind, Grade string }`
  - `search.Query(root, q string, limit int) ([]Hit, []string, error)` — runs `EnsureFresh`, then FTS MATCH ordered by `(status='superseded') ASC, rank` (superseded downranked, never hidden), `limit` capped rows. Empty match or no index ⇒ empty hits, nil error.
  - `search.FormatHits(hits []Hit) string` — numbered plain-text list; superseded hits prefixed `[SUPERSEDED]`; each hit shows `status/kind/grade`, path, title, snippet.
  - CLI (`main.go`): subcommands `index`, `query <question>`, `bench` (Task 10), `serve` (Tasks 11–12). Global flags: `--root <dir>` (default: walk-up from cwd), `--version`. `query` flags: `--json`, `--limit N` (default 5). Warnings print to stderr; results to stdout. `var version = "dev"` stamped by ldflags. `index` = lock + `BuildIndexFile` to temp + `SwapIndex` + `WriteIndexMD`; `query` does NOT rewrite `INDEX.md` (auto-reindex only rebuilds `index.db`, so a read never mutates tracked files).

- [ ] **Step 1: Write the failing test**

`retrieval/internal/search/query_test.go`:

```go
package search

import (
	"strings"
	"testing"
)

func TestQueryRanksAndLabels(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "how do I bind entities to demon joints AttachJoint", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Path != "docs/engine/joint-binding.md" {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].Status != "promoted" || hits[0].Grade != "direct" {
		t.Fatalf("hit metadata: %+v", hits[0])
	}
}

func TestQuerySupersededDownranked(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "snapmap spawn limit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("superseded docs must still be retrievable")
	}
	last := hits[len(hits)-1]
	if last.Status != "superseded" {
		t.Fatalf("superseded must sort last: %+v", hits)
	}
	out := FormatHits(hits)
	if !strings.Contains(out, "[SUPERSEDED]") {
		t.Fatalf("superseded label missing:\n%s", out)
	}
}

func TestQueryEmptyAndUnusable(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "???", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("unusable query: %v %v", hits, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run TestQuery` → FAIL.

- [ ] **Step 3: Implement query.go**

`retrieval/internal/search/query.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./internal/search/` → PASS.

- [ ] **Step 5: Implement the CLI**

`retrieval/cmd/re-search/main.go`:

```go
// Command re-search indexes and queries a .re-discipline/ markdown
// knowledge base. It is ephemeral: it starts, answers, and exits.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/adiazpar/re-discipline/retrieval/internal/mcp"
	"github.com/adiazpar/re-discipline/retrieval/internal/httpserve"
	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

var version = "dev" // stamped via -ldflags "-X main.version=X.Y.Z"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "re-search:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") {
		fmt.Println(version)
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: re-search [--version] <index|query|bench|serve> [flags]")
	}
	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	rootFlag := fs.String("root", "", "project root (default: walk up from cwd to .re-discipline)")
	jsonOut := fs.Bool("json", false, "JSON output")
	limit := fs.Int("limit", 5, "max results")
	mcpMode := fs.Bool("mcp", false, "serve MCP over stdio")
	httpAddr := fs.String("http", "", "serve HTTP on address, e.g. 127.0.0.1:7345")
	fs.Parse(rest)

	resolveRoot := func() (string, error) {
		if *rootFlag != "" {
			return *rootFlag, nil
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return search.FindRoot(cwd)
	}

	switch cmd {
	case "index":
		root, err := resolveRoot()
		if err != nil {
			return err
		}
		return runIndex(root)
	case "query":
		q := ""
		if fs.NArg() > 0 {
			q = fs.Arg(0)
		}
		if q == "" {
			return fmt.Errorf("usage: re-search query \"<question>\"")
		}
		root, err := resolveRoot()
		if err != nil {
			return err
		}
		hits, warnings, err := search.Query(root, q, *limit)
		printWarnings(warnings)
		if err != nil {
			return err
		}
		if *jsonOut {
			return json.NewEncoder(os.Stdout).Encode(hits)
		}
		fmt.Print(search.FormatHits(hits))
		return nil
	case "bench":
		root, err := resolveRoot()
		if err != nil {
			return err
		}
		return runBench(root, *limit, *jsonOut)
	case "serve":
		// Root is resolved lazily, per query: the host spawns this
		// server in every project where the plugin is enabled,
		// initialized or not, and a process that dies at spawn reads
		// as a broken plugin. An uninitialized project gets a helpful
		// text answer instead.
		queryText := func(q string, n int) (string, error) {
			root, err := resolveRoot()
			if err != nil {
				return "no .re-discipline directory found in this project — run the init-project skill to set one up", nil
			}
			hits, _, qErr := search.Query(root, q, n)
			if qErr != nil {
				return "", qErr
			}
			return search.FormatHits(hits), nil
		}
		switch {
		case *mcpMode:
			return mcp.Serve(os.Stdin, os.Stdout, version, queryText)
		case *httpAddr != "":
			return httpserve.ListenAndServe(*httpAddr, func(q string, n int) ([]search.Hit, error) {
				root, err := resolveRoot()
				if err != nil {
					return nil, err
				}
				hits, _, qErr := search.Query(root, q, n)
				return hits, qErr
			})
		default:
			return fmt.Errorf("serve requires --mcp or --http <addr>")
		}
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runIndex(root string) error {
	release, ok := search.TryLock(root)
	if !ok {
		return fmt.Errorf("another process is rebuilding the index; try again shortly")
	}
	defer release()
	tmp := search.IndexPath(root) + ".build"
	os.Remove(tmp)
	docs, warnings, err := search.BuildIndexFile(root, tmp)
	printWarnings(warnings)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := search.SwapIndex(tmp, search.IndexPath(root)); err != nil {
		return err
	}
	if err := search.WriteIndexMD(root, docs); err != nil {
		return err
	}
	fmt.Printf("indexed %d docs\n", len(docs))
	return nil
}

func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
}
```

Note: this references `runBench` (Task 10) and packages `mcp`/`httpserve` (Tasks 11–12). To keep this task compiling on its own, create the three stubs now — each is fully replaced by its own task:

`retrieval/cmd/re-search/bench.go` (replaced in Task 10):

```go
package main

import "fmt"

func runBench(root string, limit int, jsonOut bool) error {
	return fmt.Errorf("bench: not yet implemented")
}
```

`retrieval/internal/mcp/server.go` (replaced in Task 11):

```go
// Package mcp implements a minimal MCP stdio server for re-search.
package mcp

import (
	"fmt"
	"io"
)

// QueryFunc answers one question with formatted text.
type QueryFunc func(query string, limit int) (string, error)

// Serve speaks MCP over stdio (implemented in a later task).
func Serve(in io.Reader, out io.Writer, version string, query QueryFunc) error {
	return fmt.Errorf("mcp: not yet implemented")
}
```

`retrieval/internal/httpserve/server.go` (replaced in Task 12):

```go
// Package httpserve exposes re-search queries over HTTP.
package httpserve

import (
	"fmt"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

// QueryFunc answers one question with ranked hits.
type QueryFunc func(query string, limit int) ([]search.Hit, error)

// ListenAndServe serves the query API (implemented in a later task).
func ListenAndServe(addr string, query QueryFunc) error {
	return fmt.Errorf("http: not yet implemented")
}
```

- [ ] **Step 6: Verify the whole module builds and tests pass**

Run: `cd retrieval && go build ./... && go test ./...` → PASS.

- [ ] **Step 7: Smoke the CLI end-to-end**

```bash
cd retrieval && go run ./cmd/re-search --version
mkdir -p /tmp/rs-smoke/.re-discipline/docs/engine
printf -- '---\nstatus: promoted\nkind: fact\ngrade: direct\n---\n# AttachJoint binds entities to joints\n\ndetail\n' > /tmp/rs-smoke/.re-discipline/docs/engine/aj.md
go run ./cmd/re-search query --root /tmp/rs-smoke "how to bind entities AttachJoint"
```

Expected: version prints `dev`; query prints one hit for `docs/engine/aj.md`.

- [ ] **Step 8: Commit**

```bash
git add retrieval
git commit -m "feat: query engine and re-search CLI entrypoint"
```

### Task 10: bench + fixture corpus

**Files:**
- Create: `retrieval/internal/search/bench.go`, `retrieval/testdata/fixture/.re-discipline/docs/...` (fixture corpus), `retrieval/testdata/fixture/.re-discipline/golden.jsonl`
- Modify: `retrieval/cmd/re-search/bench.go` (replace stub)
- Test: `retrieval/internal/search/bench_test.go`

**Interfaces:**
- Consumes: `Query`.
- Produces:
  - `type BenchCase struct { Q string; Expect string }` — JSON tags `q` and `expect` (see implementation code below)
  - `type BenchReport struct { Total, Passed int; Misses []BenchCase }`
  - `search.RunBench(root, goldenPath string, limit int) (BenchReport, error)` — for each JSONL case, pass = `Expect` appears in the top `limit` (default 5) hit paths. Blank lines skipped; malformed lines are misses with `Q` set to the raw line (lenient, never fatal).
  - CLI `bench`: default golden path `<root>/.re-discipline/golden.jsonl`, prints `bench: P/T passed` + missed questions, **exit code 1 if any miss** (CI-friendly).

- [ ] **Step 1: Create the fixture corpus**

Create these files under `retrieval/testdata/fixture/.re-discipline/`:

`docs/engine/joint-binding.md`:

```markdown
---
status: promoted
kind: fact
grade: direct
tags: [entities, animation]
evidence: [archive/demon-transforms/reports/R-007.md]
---
# Entity binding to demon joints goes through idAnimatedEntity::AttachJoint

Call idAnimatedEntity::AttachJoint with the joint handle from
GetJointHandle. Works in snapmap-spawned entities as well.
```

`docs/engine/spawn-limit-old.md`:

```markdown
---
status: superseded
kind: fact
grade: inferred
tags: [snapmap]
---
# Snapmap concurrent AI spawn limit is 12

Superseded by spawn-limit.md — the limit is budget-based, not a count.
```

`docs/engine/spawn-limit.md`:

```markdown
---
status: promoted
kind: fact
grade: direct
tags: [snapmap]
---
# Snapmap AI spawning is limited by a points budget, not a fixed count

Each archetype has a cost; the budget is 24 points per map region.
```

`docs/ops/ghidra-setup.md`:

```markdown
---
status: promoted
kind: ops
tags: [ghidra]
---
# The shared Ghidra project lives at reference/ghidra/doom.gpr

Open with Ghidra 11.2+. Function IDs are synced via the committed fidb.
```

`docs/broken/malformed.md` (exercises lenient indexing):

```markdown
---
status: promoted
this frontmatter never closes
# Weapon viewmodel FOV lives in cvar vm_fov
plain text body
```

`golden.jsonl`:

```jsonl
{"q": "how do I bind entities to demon joints", "expect": "docs/engine/joint-binding.md"}
{"q": "snapmap spawn limit", "expect": "docs/engine/spawn-limit.md"}
{"q": "where is the ghidra project", "expect": "docs/ops/ghidra-setup.md"}
```

- [ ] **Step 2: Write the failing test**

`retrieval/internal/search/bench_test.go`:

```go
package search

import (
	"path/filepath"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestRunBenchFixtureAllPass(t *testing.T) {
	root := fixtureRoot(t)
	report, err := RunBench(root, filepath.Join(root, ".re-discipline", "golden.jsonl"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.Passed != 3 {
		t.Fatalf("report: %+v", report)
	}
}
```

Note: the fixture's `index.db`, `index.lock`, and tmp files get created by tests; add `retrieval/testdata/fixture/.re-discipline/.gitignore` containing:

```
index.db
index.lock
index.tmp-*.db
index.db.build
docs/INDEX.md
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/search/ -run TestRunBench` → FAIL (RunBench undefined).

- [ ] **Step 4: Implement**

`retrieval/internal/search/bench.go`:

```go
package search

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// BenchCase is one golden regression question.
type BenchCase struct {
	Q      string `json:"q"`
	Expect string `json:"expect"`
}

// BenchReport summarizes a bench run.
type BenchReport struct {
	Total  int
	Passed int
	Misses []BenchCase
}

// RunBench answers every golden question and checks the expected doc
// appears in the top `limit` hits. Malformed lines count as misses;
// they never abort the run.
func RunBench(root, goldenPath string, limit int) (BenchReport, error) {
	f, err := os.Open(goldenPath)
	if err != nil {
		return BenchReport{}, err
	}
	defer f.Close()

	var report BenchReport
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c BenchCase
		if err := json.Unmarshal([]byte(line), &c); err != nil || c.Q == "" || c.Expect == "" {
			report.Total++
			report.Misses = append(report.Misses, BenchCase{Q: line, Expect: "(malformed line)"})
			continue
		}
		report.Total++
		hits, _, err := Query(root, c.Q, limit)
		if err != nil {
			report.Misses = append(report.Misses, c)
			continue
		}
		found := false
		for _, h := range hits {
			if h.Path == c.Expect {
				found = true
				break
			}
		}
		if found {
			report.Passed++
		} else {
			report.Misses = append(report.Misses, c)
		}
	}
	return report, scanner.Err()
}
```

Replace `retrieval/cmd/re-search/bench.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

func runBench(root string, limit int, jsonOut bool) error {
	golden := filepath.Join(root, ".re-discipline", "golden.jsonl")
	report, err := search.RunBench(root, golden, limit)
	if err != nil {
		return err
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(report)
	} else {
		fmt.Printf("bench: %d/%d passed\n", report.Passed, report.Total)
		for _, m := range report.Misses {
			fmt.Printf("MISS: %q expected %s\n", m.Q, m.Expect)
		}
	}
	if len(report.Misses) > 0 {
		os.Exit(1)
	}
	return nil
}
```

- [ ] **Step 5: Run tests + CLI bench**

Run: `cd retrieval && go test ./...` → PASS.
Run: `cd retrieval && go run ./cmd/re-search bench --root testdata/fixture` → `bench: 3/3 passed`, exit 0.

- [ ] **Step 6: Commit**

```bash
git add retrieval
git commit -m "feat: golden-question bench command with fixture corpus"
```

### Task 11: MCP stdio serve

**Files:**
- Modify: `retrieval/internal/mcp/server.go` (replace stub)
- Test: `retrieval/internal/mcp/server_test.go`

**Interfaces:**
- Consumes: `mcp.QueryFunc` (defined in the Task 9 stub, unchanged): `func(query string, limit int) (string, error)`.
- Produces: `mcp.Serve(in io.Reader, out io.Writer, version string, query QueryFunc) error` — newline-delimited JSON-RPC 2.0. Handles `initialize` (echoes client `protocolVersion`, falls back `"2024-11-05"`; advertises `tools`), `tools/list` (one tool: `query`, inputSchema `{query: string (required), limit: integer}`), `tools/call` (returns `content: [{type:"text", text:...}]`; query errors return `isError: true` content, not protocol errors). Notifications (no `id`) are ignored. Unknown methods with an `id` get error `-32601`. Stateless; EOF ends the loop cleanly.

- [ ] **Step 1: Write the failing test**

`retrieval/internal/mcp/server_test.go`:

```go
package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func rpc(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	err := Serve(in, &out, "1.0.0-test", func(q string, n int) (string, error) {
		return "RESULT for " + q, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestServeLifecycle(t *testing.T) {
	resps := rpc(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"query","arguments":{"query":"demon joints"}}}`,
	)
	if len(resps) != 3 {
		t.Fatalf("want 3 responses (notification ignored), got %d", len(resps))
	}
	init := resps[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" {
		t.Fatalf("init: %v", init)
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "query" {
		t.Fatalf("tools: %v", tools)
	}
	content := resps[2]["result"].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "RESULT for demon joints") {
		t.Fatalf("call result: %q", text)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	resps := rpc(t, `{"jsonrpc":"2.0","id":9,"method":"nope"}`)
	errObj := resps[0]["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Fatalf("want -32601, got %v", errObj)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/mcp/` → FAIL ("mcp: not yet implemented").

- [ ] **Step 3: Implement**

Replace `retrieval/internal/mcp/server.go`:

```go
// Package mcp implements a minimal MCP stdio server for re-search:
// newline-delimited JSON-RPC 2.0 exposing one `query` tool. Stateless;
// all knowledge state lives on disk, so any number of instances are
// safe and a killed instance loses nothing.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"
)

// QueryFunc answers one question with formatted text.
type QueryFunc func(query string, limit int) (string, error)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads newline-delimited JSON-RPC requests from in until EOF.
func Serve(in io.Reader, out io.Writer, version string, query QueryFunc) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue // unparseable line: nothing safe to answer
		}
		if req.ID == nil {
			continue // notification
		}
		resp := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2024-11-05"
			}
			resp.Result = map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "re-search", "version": version},
			}
		case "tools/list":
			resp.Result = map[string]any{"tools": []map[string]any{{
				"name":        "query",
				"description": "Search the project's curated reverse-engineering knowledge base (.re-discipline/docs/). Returns ranked findings with evidence paths. Search here before investigating anything.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string", "description": "natural-language question or identifier"},
						"limit": map[string]any{"type": "integer", "description": "max results (default 5)"},
					},
					"required": []string{"query"},
				},
			}}}
		case "tools/call":
			var p struct {
				Name      string `json:"name"`
				Arguments struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			if p.Name != "query" {
				resp.Error = &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
				break
			}
			text, err := query(p.Arguments.Query, p.Arguments.Limit)
			if err != nil {
				resp.Result = map[string]any{
					"content": []map[string]any{{"type": "text", "text": "query error: " + err.Error()}},
					"isError": true,
				}
				break
			}
			resp.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
			}
		case "ping":
			resp.Result = map[string]any{}
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add retrieval/internal/mcp
git commit -m "feat: minimal stateless MCP stdio server with query tool"
```

### Task 12: HTTP serve

**Files:**
- Modify: `retrieval/internal/httpserve/server.go` (replace stub)
- Test: `retrieval/internal/httpserve/server_test.go`

**Interfaces:**
- Consumes: `httpserve.QueryFunc` (from the Task 9 stub, unchanged): `func(query string, limit int) ([]search.Hit, error)`.
- Produces: `httpserve.Handler(query QueryFunc) http.Handler` and `httpserve.ListenAndServe(addr string, query QueryFunc) error`. Routes: `GET /query?q=<question>&limit=N` → JSON `{"hits": [...]}`; missing `q` → 400; query error → 500 with `{"error": "..."}`.

- [ ] **Step 1: Write the failing test**

`retrieval/internal/httpserve/server_test.go`:

```go
package httpserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

func TestHandlerQuery(t *testing.T) {
	h := Handler(func(q string, n int) ([]search.Hit, error) {
		return []search.Hit{{Path: "docs/engine/a.md", Title: "A: " + q}}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/query?q=demon+joints", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Hits []search.Hit `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hits) != 1 || body.Hits[0].Path != "docs/engine/a.md" {
		t.Fatalf("body: %+v", body)
	}
}

func TestHandlerMissingQ(t *testing.T) {
	h := Handler(func(q string, n int) ([]search.Hit, error) { return nil, nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/query", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd retrieval && go test ./internal/httpserve/` → FAIL.

- [ ] **Step 3: Implement**

Replace `retrieval/internal/httpserve/server.go`:

```go
// Package httpserve exposes re-search queries over a small JSON HTTP
// endpoint — the retrieval half any future hosted consumer wraps.
package httpserve

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

// QueryFunc answers one question with ranked hits.
type QueryFunc func(query string, limit int) ([]search.Hit, error)

// Handler serves GET /query?q=...&limit=N as JSON.
func Handler(query QueryFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q parameter"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		hits, err := query(q, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if hits == nil {
			hits = []search.Hit{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})
	return mux
}

// ListenAndServe blocks serving the query API on addr.
func ListenAndServe(addr string, query QueryFunc) error {
	return http.ListenAndServe(addr, Handler(query))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd retrieval && go test ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add retrieval/internal/httpserve
git commit -m "feat: HTTP query endpoint"
```

### Task 13: Canonical build script, committed exe, .mcp.json, .gitattributes

**Files:**
- Create: `scripts/build.ps1`, `bin/re-search.exe` (built artifact), `.mcp.json`
- Modify: `.gitattributes`

**Interfaces:**
- Consumes: the version field of `.claude-plugin/plugin.json` (set to 1.0.0 in Task 17 — at this task it is still the current value; the script reads whatever is there).
- Produces: `scripts/build.ps1 [-Output <path>]` — the ONE canonical build, shared verbatim by release and CI: reads the version from `.claude-plugin/plugin.json`, verifies `.codex-plugin/plugin.json` matches, then runs `go build -trimpath -buildvcs=false -ldflags "-X main.version=<v>" -o <path> ./cmd/re-search`. Default output `bin/re-search.exe`.

- [ ] **Step 1: Write the build script**

`scripts/build.ps1`:

```powershell
# Canonical re-search build. This exact command is shared by release and
# CI; any drift breaks the stale-binary check by design.
param(
    [string]$Output = "bin/re-search.exe"
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

$claude = (Get-Content (Join-Path $repo '.claude-plugin/plugin.json') -Raw | ConvertFrom-Json).version
$codex  = (Get-Content (Join-Path $repo '.codex-plugin/plugin.json')  -Raw | ConvertFrom-Json).version
if ($claude -ne $codex) { throw "version mismatch: .claude-plugin=$claude .codex-plugin=$codex" }

# Marketplace entries must match too (spec §11) — this check is what
# replaced the 0.x version sync/guard scripts.
foreach ($mp in @('.claude-plugin/marketplace.json', '.agents/plugins/marketplace.json')) {
    $mpPath = Join-Path $repo $mp
    if (-not (Test-Path $mpPath)) { continue }
    $entry = (Get-Content $mpPath -Raw | ConvertFrom-Json).plugins |
        Where-Object { $_.name -eq 're-discipline' }
    if ($entry -and $entry.version -and $entry.version -ne $claude) {
        throw "version mismatch: $mp=$($entry.version) plugin.json=$claude"
    }
}

$out = Join-Path $repo $Output
New-Item -ItemType Directory -Force (Split-Path -Parent $out) | Out-Null
Push-Location (Join-Path $repo 'retrieval')
try {
    go build -trimpath -buildvcs=false -ldflags "-X main.version=$claude" -o $out ./cmd/re-search
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally { Pop-Location }
Write-Output "built $out (version $claude)"
```

- [ ] **Step 2: Build and verify**

Run: `pwsh scripts/build.ps1` then `bin/re-search.exe --version` → prints the current manifest version.

- [ ] **Step 3: Write `.mcp.json`**

The host spawns the server with cwd = the project directory, so walk-up root discovery works without flags; per spec §5, `serve` resolves the root lazily per query (Task 9), so this server never dies at spawn in an uninitialized project:

```json
{
  "mcpServers": {
    "re-search": {
      "command": "${CLAUDE_PLUGIN_ROOT}/bin/re-search.exe",
      "args": ["serve", "--mcp"]
    }
  }
}
```

- [ ] **Step 4: Simplify `.gitattributes`**

Replace the file's contents with:

```
* text=auto
*.exe binary
*.db binary
```

- [ ] **Step 5: Commit (exe included deliberately)**

```bash
git add -f bin/re-search.exe scripts/build.ps1 .mcp.json .gitattributes
git commit -m "feat: canonical build script, committed exe, MCP registration"
```

### Task 14: Templates — CONVENTIONS.md, marker block, project gitignore

**Files:**
- Create: `templates/CONVENTIONS.md`, `templates/marker-block.md`, `templates/project-gitignore`

**Interfaces:**
- Produces: the files `init-project` (Task 15) copies/renders into target projects. The marker block contains the literal policy line `Shared memory: {{SHARED_MEMORY}}` which init replaces with `enabled` or `disabled`.

- [ ] **Step 1: Write `templates/CONVENTIONS.md`**

```markdown
# re-discipline Conventions

This project curates hard facts about the software under analysis in
`.re-discipline/`. Any agent that can read files and run a command can
use it. The recovery path for every problem here is "edit a text file"
or "delete and rebuild" — nothing requires exact hashes or protocols.

## Layout

- `docs/` — curated truth. One atomic claim or topic per file. `docs/ops/`
  holds operational recall (workflows, tool paths, dead ends) — this is
  the project's shared memory.
- `active/<campaign-slug>/` — one campaign (investigation workspace) per
  task: `CAMPAIGN.md` (goal + status), `reports/` (full investigator
  reports, append-only), `findings/` (candidate findings awaiting
  promotion).
- `archive/<campaign-slug>/` — closed campaigns.
- `golden.jsonl` — retrieval regression questions:
  `{"q": "...", "expect": "docs/..."}` per line.
- `bin/re-search.exe`, `index.db` — search tool and its disposable index
  (both gitignored; re-run init to restore the exe).

## Searching (do this before investigating anything)

    .re-discipline/bin/re-search.exe query "your question or identifier"

Add `--json` for structured output, `--limit N` for more results. Flags
go BEFORE the question text (`query --limit 8 "..."`) — flags after the
question are silently ignored. The index rebuilds itself when stale. If
results are weak, reword, or grep `docs/` directly. When a real question
misses a doc you know exists, add it to `golden.jsonl`.

## Doc format

    ---
    status: promoted | superseded    # candidate while in active/*/findings/
    kind: fact | ops
    grade: direct | inferred | reported   # facts only
    evidence: [archive/<slug>/reports/R-001.md]
    tags: [entities, animation]
    ---
    # One-sentence claim as the title

    Details, addresses, snippets.

- `grade`: `direct` = observed in decompilation/memory; `inferred` =
  deduced; `reported` = external source.
- Titles are searchable assertions, not labels.
- Negative results are first-class docs: "X does not work, because Y."
- Conflicting observations may both be recorded with the conflict noted.

## Curation flow

1. Investigate (inline or via subagents — same rules either way).
2. The investigator writes its full report to the campaign's `reports/`
   AND distills candidate findings into `findings/` incrementally, as
   discovered — one atomic claim per file, each citing its report.
3. The manager promotes: skim findings (atomic? evidence cited? grade
   matches evidence?), search `docs/` for duplicates first, resolve
   conflicts (higher grade wins, or record both), set
   `status: promoted`, move into `docs/`, run
   `.re-discipline/bin/re-search.exe index`.
   Investigators never promote their own findings.
4. Correcting a doc later: edit it, set `status: superseded`, link the
   replacement. Git history is the provenance trail.
5. Closing a campaign: final promotion sweep, write a short summary in
   CAMPAIGN.md, move the folder to `archive/`.
```

- [ ] **Step 2: Write `templates/marker-block.md`**

```markdown
<!-- re-discipline:start -->
## re-discipline

- Curated knowledge base: `.re-discipline/docs/`. Search it BEFORE
  investigating: `.re-discipline/bin/re-search.exe query "<question>"`
- Active campaigns (one per task): `.re-discipline/active/`
- Conventions (doc format, curation flow): `.re-discipline/CONVENTIONS.md`
- Shared memory: {{SHARED_MEMORY}} — durable operational recall goes in
  `.re-discipline/docs/ops/`, not host-native memory.
<!-- re-discipline:end -->
```

- [ ] **Step 3: Write `templates/project-gitignore`**

(Content for `.re-discipline/.gitignore` in target projects:)

```
index.db
index.lock
index.tmp-*.db
index.db.build
bin/
```

- [ ] **Step 4: Commit**

```bash
git add templates
git commit -m "feat: project templates for conventions, marker block, gitignore"
```

### Task 15: Skills — init-project, open-campaign, close-campaign, curate

**Files:**
- Create: `skills/init-project/SKILL.md`, `skills/open-campaign/SKILL.md`, `skills/close-campaign/SKILL.md`, `skills/curate/SKILL.md`

**Interfaces:**
- Consumes: `templates/*` (Task 14), `bin/re-search.exe` (Task 13).
- Produces: the plugin's four user-facing skills.

- [ ] **Step 1: Write `skills/init-project/SKILL.md`**

````markdown
---
name: init-project
description: >-
  Initialize or resync a re-discipline project. Idempotent and
  non-destructive: creates what is missing, never overwrites existing
  content, updates only marker-bounded blocks, and reports what it did.
---

# Initialize A re-discipline Project

Run from anywhere inside the target project. New and existing projects
are the same procedure — they just start with different things missing.

## 0. Old-format check (hard stop)

If `.re-discipline/config.json` or campaign JSON records from
re-discipline 0.x exist: create and convert NOTHING. Tell the user:
"old-format re-discipline state detected — run the one-time migration
session first (an agent reads the old JSON and writes 1.0 markdown docs;
you review the diff before the old tree is deleted)." Stop.

## 1. Create the tree (only what's missing)

```
.re-discipline/
  CONVENTIONS.md      copy of <plugin-root>/templates/CONVENTIONS.md
  docs/  docs/ops/  active/  archive/
  golden.jsonl        empty file
  bin/re-search.exe   copy of <plugin-root>/bin/re-search.exe
  .gitignore          copy of <plugin-root>/templates/project-gitignore
```

Re-running init refreshes `CONVENTIONS.md`, `.gitignore`, and the exe
from the plugin (these three are plugin-owned copies); everything else
is user data — never touch it.

## 2. Shared memory decision

Read the policy line from the marker block in root `AGENTS.md` (the
canonical policy file). If no recorded decision exists, ask the user
once: "Use shared memory? Replaces Claude/Codex native memory with
`.re-discipline/docs/ops/` so all agents share the same recall.
[Recommended: yes]". Then converge:

- **enabled**: set `autoMemoryEnabled: false` in the project's
  `.claude/settings.json` (create the file if absent, preserving any
  existing keys). For Codex, apply the equivalent project-scoped memory
  switch if one exists; if none exists, the marker-block instruction is
  the mechanism.
- **disabled**: do NOT touch host memory settings at all — init only
  ever writes them when enabling.

## 3. Marker blocks

Render `<plugin-root>/templates/marker-block.md` with
`{{SHARED_MEMORY}}` → `enabled`/`disabled`, then apply to root
`AGENTS.md` and `.claude/CLAUDE.md`:

- markers absent → append the block (create the file if missing);
- exactly one well-formed `<!-- re-discipline:start -->` …
  `<!-- re-discipline:end -->` pair → replace its contents;
- anything else (unpaired, duplicated, nested) → leave that file
  untouched and report it for manual repair.

`AGENTS.md` is canonical: sync the `CLAUDE.md` block from it whenever
they disagree. Never modify anything outside the markers.

## 4. Report

List what was created, refreshed, skipped, and any files needing manual
repair. Do not commit unless the user asks.
````

- [ ] **Step 2: Write `skills/open-campaign/SKILL.md`**

````markdown
---
name: open-campaign
description: >-
  Open a re-discipline campaign: an investigation workspace for one
  task. Creates the campaign folder and records the goal in-session.
---

# Open A Campaign

1. **Search first.** Run
   `.re-discipline/bin/re-search.exe query "<the goal>"` — existing
   docs may already answer part or all of it. Cite what you find.
2. Pick a short kebab-case slug (e.g. `demon-transforms`).
3. Create:

```
.re-discipline/active/<slug>/
  CAMPAIGN.md
  reports/
  findings/
```

4. Write `CAMPAIGN.md`:

```markdown
# <Campaign title>

**Goal:** <one paragraph: what we want to achieve or learn>
**Opened:** <date>
**Status:** open

## Working notes
<running log: theories, leads, decisions — updated as the campaign runs>
```

Goals live here and in the session — there is no separate goal document
type. One campaign per concurrent session/task keeps sessions from
colliding. Follow `.re-discipline/CONVENTIONS.md` (or the curate skill)
for how reports and findings are written.
````

- [ ] **Step 3: Write `skills/close-campaign/SKILL.md`**

````markdown
---
name: close-campaign
description: >-
  Close a re-discipline campaign: promote remaining findings, write the
  summary, archive the folder. A file move, not a proof obligation.
---

# Close A Campaign

1. **Final promotion sweep** over `active/<slug>/findings/`. For each
   candidate, skim with the checklist: atomic claim? evidence cited?
   grade matches evidence? Then:
   - search `docs/` for duplicates (`re-search query`) and merge instead
     of duplicating;
   - conflicts: higher evidence grade wins, or record both with the
     conflict noted;
   - accept → set `status: promoted`, move into the right `docs/`
     subfolder;
   - reject → leave it in the campaign folder (it archives with the
     campaign; nothing is destroyed).
2. Run `.re-discipline/bin/re-search.exe index` (rebuilds the index and
   regenerates `docs/INDEX.md`).
3. Optionally run `.re-discipline/bin/re-search.exe bench` and add any
   questions this campaign answered to `golden.jsonl`.
4. In `CAMPAIGN.md`: set `Status: closed`, add a short summary — what
   was achieved, key promoted findings, dead ends worth remembering.
5. Move `active/<slug>/` to `archive/<slug>/`.
6. Show the user the git diff of promotions for review. Do not commit
   unless asked.
````

- [ ] **Step 4: Write `skills/curate/SKILL.md`**

````markdown
---
name: curate
description: >-
  The re-discipline curation conventions: how investigators (inline
  manager or subagents) write reports and distill candidate findings,
  and how managers promote them. Reference this in every investigation
  brief.
---

# Curate Knowledge

Distillation happens at the point of production, by the producer — the
agent that just finished investigating has the full context, and that
moment never comes back.

## If you are investigating (inline or as a subagent)

- Write your full report to `active/<slug>/reports/R-NNN.md` — verbose
  is fine; reports are archived, never squashed.
- **Distill candidate findings incrementally as you discover them**, not
  as an end-of-run chore. One atomic claim per file in
  `active/<slug>/findings/`, format per `.re-discipline/CONVENTIONS.md`:
  frontmatter (`status: candidate`, `kind`, `grade`, `evidence` pointing
  at your report section, `tags`), title = the claim as one sentence.
- Grade honestly: `direct` only for what you observed in
  decompilation/memory; deductions are `inferred`; external docs are
  `reported`. Do not state hypotheses as facts.
- Negative results are findings too: "X does not work, because Y."
- Do NOT put findings into `docs/` — promotion is the manager's job.

## If you are the manager promoting

Skim each candidate (atomic? evidence cited? grade matches evidence?),
search `docs/` for duplicates first, resolve conflicts (higher grade
wins, or record both), set `status: promoted`, move into `docs/`, run
`.re-discipline/bin/re-search.exe index`. You review claims — you never
re-derive reports.

## Correcting promoted docs

Edit the doc, set `status: superseded`, link the replacement doc, cite
the newer evidence. Git history is the provenance trail.
````

- [ ] **Step 5: Verify and commit**

Check each SKILL.md has valid frontmatter (name + description). Then:

```bash
git add skills
git commit -m "feat: 1.0 skills — init-project, open-campaign, close-campaign, curate"
```

### Task 16: SessionStart hook

**Files:**
- Create: `hooks/hooks.json`, `hooks/session-start.ps1`

**Interfaces:**
- Produces: the plugin's only hook — read-only, fails silent, prints one status line into session context.

- [ ] **Step 1: Write `hooks/hooks.json`**

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "powershell.exe -NoProfile -ExecutionPolicy Bypass -File \"${CLAUDE_PLUGIN_ROOT}/hooks/session-start.ps1\""
          }
        ]
      }
    ]
  }
}
```

- [ ] **Step 2: Write `hooks/session-start.ps1`**

```powershell
# Read-only session-start status line. Any failure exits silently; the
# session must start identically with or without this hook.
try {
    $rd = Join-Path (Get-Location) '.re-discipline'
    if (-not (Test-Path $rd -PathType Container)) { exit 0 }

    $campaigns = @(Get-ChildItem (Join-Path $rd 'active') -Directory -ErrorAction SilentlyContinue)
    $docs = @(Get-ChildItem (Join-Path $rd 'docs') -Recurse -Filter *.md -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -ne 'INDEX.md' })

    $campaignPart = "$($campaigns.Count) active campaign(s)"
    if ($campaigns.Count -gt 0) {
        $names = $campaigns | ForEach-Object {
            $age = [int]((Get-Date) - $_.LastWriteTime).TotalDays
            "$($_.Name) (updated ${age}d ago)"
        }
        $campaignPart = "$($campaigns.Count) active campaign(s) — $($names -join ', ')"
    }
    Write-Output "re-discipline: $campaignPart. $($docs.Count) docs curated."
} catch { }
exit 0
```

- [ ] **Step 3: Test the hook manually**

```bash
mkdir -p /tmp/hooktest/.re-discipline/active/demo /tmp/hooktest/.re-discipline/docs
cd /tmp/hooktest && powershell.exe -NoProfile -ExecutionPolicy Bypass -File <repo>/hooks/session-start.ps1
```

Expected: one line `re-discipline: 1 active campaign(s) — demo (updated 0d ago). 0 docs curated.` Also run it from a directory with no `.re-discipline/` → no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add hooks
git commit -m "feat: single read-only SessionStart status hook"
```

### Task 17: Manifests to 1.0.0, CHANGELOG, README

**Files:**
- Modify: `.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `.agents/plugins/marketplace.json`, `CHANGELOG.md`, `README.md`
- Modify: `bin/re-search.exe` (rebuilt with the 1.0.0 stamp)

- [ ] **Step 1: Set versions and purge 0.x manifest content**

In `.claude-plugin/plugin.json` and `.codex-plugin/plugin.json`: set `"version": "1.0.0"` and update the `description` field to: `"Markdown-canonical reverse-engineering knowledge: campaign curation conventions plus the re-search retrieval CLI/MCP tool."`. In both marketplace.json files, update the re-discipline entry's version/description to match (leave unrelated fields untouched).

The Codex manifest additionally carries 0.x content that must be rewritten, not preserved:

- Replace its `mcpServers` entry (which points at the deleted `knowledge/bin/re-discipline-knowledge`) with the 1.0 server, using the same repo-relative path style the 0.x entry used:

```json
"mcpServers": {
  "re-search": {
    "command": "bin/re-search.exe",
    "args": ["serve", "--mcp"],
    "cwd": "."
  }
}
```

After editing, verify the Codex registration once: start a Codex session in a test project and confirm the `query` tool answers (or returns the "run init-project" message).

- Rewrite its `interface` block (`shortDescription`, `longDescription`, `defaultPrompt`) from the 0.x engine language (runs, ratification, guarded discard, atomic merge) to 1.0 semantics. Use: shortDescription `"Curated RE knowledge: search it, campaign it, promote findings."`; longDescription `"Markdown-canonical reverse-engineering knowledge base in .re-discipline/: search curated docs with the re-search query tool before investigating, work in campaign folders (reports + candidate findings), and promote reviewed findings into docs/."`; defaultPrompt `"Search the project knowledge base with the re-search query tool before investigating anything. Follow .re-discipline/CONVENTIONS.md for reports, findings, and promotion."`. If any of these fields don't exist in the current manifest, skip them; if other 0.x-specific fields reference deleted paths, remove those fields and note them in the commit message.

- [ ] **Step 2: Rebuild the exe with the new version**

Run: `pwsh scripts/build.ps1` then `bin/re-search.exe --version` → `1.0.0`.

- [ ] **Step 3: CHANGELOG entry**

Prepend to `CHANGELOG.md`:

```markdown
## 1.0.0

Ground-up simplification. Markdown in `.re-discipline/docs/` is now the
canonical knowledge store; the retrieval index is derived and
disposable. Replaces the 0.x transactional engine (digests, head
revisions, ratification, write-guard hooks, retrieval profiles,
benchmarking apparatus) with:

- `re-search`: small ephemeral Go CLI — `index`, `query`
  (auto-reindexing, identifier-aware FTS5), `bench` (golden regression
  questions), `serve --mcp` (per-session stdio, any MCP host),
  `serve --http`.
- Producer-distilled curation: investigators write reports + atomic
  candidate findings; the manager's promotion skim is the single gate.
- 4 skills (init-project, open-campaign, close-campaign, curate), one
  read-only SessionStart hook, shared memory as `docs/ops/`.
- Windows-only; single 1–2 minute CI job.

Migration from 0.x is a one-time agent session (old JSON → markdown);
init detects old state and stops rather than converting.
```

- [ ] **Step 4: Rewrite `README.md`**

```markdown
# re-discipline 1.0

An evidence-disciplined reverse-engineering knowledge system for Claude
Code, Codex, and any MCP-capable agent. Two halves:

- **Curation** — campaign workspaces in `.re-discipline/active/` where
  investigators write full reports and distill atomic candidate
  findings; a manager promotion skim is the single quality gate into
  `.re-discipline/docs/`, the markdown-canonical knowledge base.
- **Retrieval** — `re-search`, a small ephemeral CLI: SQLite FTS5 with
  identifier-aware matching, auto-reindexing, golden-question
  regression bench, per-session MCP stdio server, and an HTTP endpoint.

Design rule: at every layer the recovery path is "edit a text file" or
"delete and rebuild" — never satisfying a machine's proof requirements.
Docs are truth; the index is disposable; git is history.

## Install

```text
/plugin marketplace add adiazpar/re-discipline
/plugin install re-discipline@re-discipline
```

Then run `/re-discipline:init-project` inside your target project. It is
idempotent: it creates `.re-discipline/` (docs, campaigns, conventions,
a project-local copy of `re-search.exe`), adds marker-bounded
orientation blocks to `AGENTS.md`/`.claude/CLAUDE.md`, and asks once
whether to use shared memory (`docs/ops/` replacing host-native memory).

## Daily use

- `.re-discipline/bin/re-search.exe query "how do I …"` — search before
  investigating; agents get the same via the `query` MCP tool.
- `/re-discipline:open-campaign` — start an investigation workspace.
- Investigate; producers distill findings as they go (see the curate
  skill / `.re-discipline/CONVENTIONS.md`).
- `/re-discipline:close-campaign` — promote, summarize, archive.
- Wrong doc later? Edit it, mark `superseded`, link the replacement.

Windows-only. Requires nothing running in the background: the tool
starts, answers, exits.
```

- [ ] **Step 5: Verify version consistency**

Run: `pwsh scripts/build.ps1` (rebuild is a no-op-diff check: it throws on manifest mismatch) and `bin/re-search.exe --version` → `1.0.0`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Cut re-discipline 1.0.0"
```

### Task 18: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `scripts/build.ps1` (canonical build), `retrieval/` tests, fixture corpus, committed `bin/re-search.exe`.

- [ ] **Step 1: Write the workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:

jobs:
  windows:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: retrieval/go.mod

      - name: Vet and test
        working-directory: retrieval
        run: |
          go vet ./...
          go test ./...

      - name: Canonical build (also checks manifest version consistency)
        run: pwsh scripts/build.ps1 -Output bin/ci-re-search.exe

      - name: Stale binary check
        shell: pwsh
        run: |
          $committed = (Get-FileHash bin/re-search.exe).Hash
          $rebuilt   = (Get-FileHash bin/ci-re-search.exe).Hash
          if ($committed -ne $rebuilt) {
            throw "bin/re-search.exe is stale: source changed without 'pwsh scripts/build.ps1'. committed=$committed rebuilt=$rebuilt"
          }

      - name: Bench fixture corpus
        run: bin/ci-re-search.exe bench --root retrieval/testdata/fixture
```

- [ ] **Step 2: Verify each step locally**

```bash
cd retrieval && go vet ./... && go test ./... && cd ..
pwsh scripts/build.ps1 -Output bin/ci-re-search.exe
# hash-compare bin/re-search.exe vs bin/ci-re-search.exe (must match)
bin/ci-re-search.exe bench --root retrieval/testdata/fixture
rm bin/ci-re-search.exe
```

Expected: tests pass, hashes match, bench 3/3, exit 0. If the hashes differ locally, the committed exe wasn't built with the canonical script — rebuild via `pwsh scripts/build.ps1` and re-commit before pushing.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "feat: single Windows CI job — vet, test, stale-binary check, bench"
```

- [ ] **Step 4: Full local gate**

Run the complete verification once: `cd retrieval && go vet ./... && go test ./...` then `pwsh scripts/build.ps1 -Output bin/ci-re-search.exe`, hash-compare, `bin/ci-re-search.exe bench --root retrieval/testdata/fixture`, delete the ci exe. All green = the plan's product is complete: engine deleted, `re-search` built and benched, skills/templates/hook/manifests/CI in place.

---

## Post-plan notes (not tasks)

- **Migration of snaphak-re** is deliberately NOT a task here (spec §12): it is a one-time interactive session in that project — an agent reads the old campaign/finding JSON, writes 1.0 markdown docs, the user reviews the diff, and only then is the old tree deleted. `init-project` hard-stops on old-format state and points there.
- **Pushing to the remote** is the user's call; everything above is local commits.
