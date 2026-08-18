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
