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
