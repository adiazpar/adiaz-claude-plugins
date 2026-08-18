package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOrdinaryCoreAndCLIServiceRejectLegacyProjectBeforeMutation(t *testing.T) {
	root := t.TempDir()
	control := filepath.Join(root, ".re-discipline")
	if err := os.Mkdir(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(control, "project-profile.md"),
		[]byte("# Legacy project\n\n<!-- re-discipline:shared-laws v0.7.0 -->\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	// Use a deliberately small, otherwise parseable config. The version gate
	// must be decisive rather than relying on a legacy field to fail decoding.
	if err := os.WriteFile(
		filepath.Join(control, "config.json"),
		[]byte("{\"schemaVersion\":2}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), stateTestOpenRequest(head)); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("ordinary state mutation did not reject legacy state: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(control, "state")); !os.IsNotExist(err) {
		t.Fatalf("legacy refusal created canonical state: %v", err)
	}
	if _, err := NewService(ServiceOptions{
		ProjectRoot: root,
		AssetRoot:   filepath.Join(root, "unused-assets"),
	}); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("peer CLI service construction did not reject legacy state: %v", err)
	}
}
