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
