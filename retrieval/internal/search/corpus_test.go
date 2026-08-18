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
