package search

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
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
	writeDoc(t, root, "docs/engine/force-idle.md", `---
status: promoted
kind: fact
grade: direct
idents: [ai_forceIdle, idAI::SetIdleState]
evidence: [archive/demo/reports/R-042.md, archive/demo/reports/R-043.md]
---
# A behavior-gating cvar exists for demons

Prose only; the cvar is declared in frontmatter, not in this text.
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
	if len(docs) != 5 {
		t.Fatalf("want 5 docs, got %d", len(docs))
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

func TestStripRelationLinks(t *testing.T) {
	body := `# The claim title

Substantive claim text mentioning DeserializeFromJson.

## Evidence
Archived at git show abc123.

## Relations
- Depends on: [Some other doc's full title with RepairAndMigrate](other-doc.md)
- Split from: docs/truth/old.md (git show abc123)
Every other finding depends on this one; unique prose stays.

## Aftermath
- Depends on: outside Relations, this bullet is content and stays.
`
	got := StripRelationLinks(body)
	if strings.Contains(got, "Some other doc's full title") || strings.Contains(got, "Split from") {
		t.Fatalf("link bullets must be stripped:\n%s", got)
	}
	for _, keep := range []string{
		"Substantive claim text mentioning DeserializeFromJson.",
		"## Relations",
		"Every other finding depends on this one; unique prose stays.",
		"outside Relations, this bullet is content and stays.",
		"Archived at git show abc123.",
	} {
		if !strings.Contains(got, keep) {
			t.Fatalf("must keep %q:\n%s", keep, got)
		}
	}
	// A body with no Relations section is returned unchanged.
	if plain := "# T\n\n- Depends on: kept, not in a Relations section\n"; StripRelationLinks(plain) != plain {
		t.Fatal("bodies without Relations must be untouched")
	}
}

// The indexed body (and idents derived from it) must exclude Relations
// link bullets, so a doc no longer competes for its dependencies' topics.
func TestBuildIndexStripsRelationLinks(t *testing.T) {
	root := buildTestCorpus(t)
	writeDoc(t, root, "docs/engine/with-relations.md", `---
status: promoted
kind: fact
grade: direct
---
# A claim about the render pipeline

Own content about renderprogs.

## Relations
- Depends on: [Entity binding goes through idAnimatedEntity::AttachJoint](joint-binding.md)
`)
	dbPath := filepath.Join(root, ".re-discipline", "index.db")
	if _, _, err := BuildIndexFile(root, dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body, idents string
	if err := db.QueryRow(`SELECT body, idents FROM docs WHERE path = 'docs/engine/with-relations.md'`).Scan(&body, &idents); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "AttachJoint") || strings.Contains(idents, "attachjoint") {
		t.Fatalf("relation link leaked into index:\nbody: %s\nidents: %s", body, idents)
	}
	if !strings.Contains(body, "Own content about renderprogs.") {
		t.Fatalf("own content missing from indexed body: %s", body)
	}
}

// Declared idents: frontmatter must be searchable both verbatim and
// through ExpandIdentifiers, without appearing in the doc body.
func TestBuildIndexDeclaredIdents(t *testing.T) {
	root := buildTestCorpus(t)
	dbPath := filepath.Join(root, ".re-discipline", "index.db")
	if _, _, err := BuildIndexFile(root, dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var idents string
	if err := db.QueryRow(`SELECT idents FROM docs WHERE path = 'docs/engine/force-idle.md'`).Scan(&idents); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ai_forceidle", "forceidle", "force", "idle", "idai::setidlestate", "setidlestate"} {
		if !slices.Contains(strings.Fields(idents), want) {
			t.Fatalf("idents column missing %q: %s", want, idents)
		}
	}
	var evidence string
	if err := db.QueryRow(`SELECT evidence FROM docmeta WHERE path = 'docs/engine/force-idle.md'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != "archive/demo/reports/R-042.md\narchive/demo/reports/R-043.md" {
		t.Fatalf("docmeta evidence: %q", evidence)
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
