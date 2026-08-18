package knowledge

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pluginRootForTests resolves the plugin root (the directory containing
// templates/ and knowledge/) from this source file's location.
func pluginRootForTests(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	// internal/knowledge/<file> -> knowledge -> plugin root
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(self))))
}

func TestRecoveryTargetsKnowledgeLayoutOnly(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# P\n\n<!-- re-discipline:shared-laws v0.8.0 -->\nlaws\n<!-- re-discipline:shared-laws:end -->\n")
	if _, err := RecoverProject(root, pluginRootForTests(t)); err != nil {
		t.Fatalf("recover: %v", err)
	}
	for _, p := range []string{
		".re-discipline/config.json",
		".re-discipline/knowledge/README.md",
		".re-discipline/knowledge/policy.jsonc",
		".re-discipline/knowledge/retrieval-profile.json",
	} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(p))); statErr != nil {
			t.Fatalf("expected %s recovered: %v", p, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, ".re-discipline", "settings")); !os.IsNotExist(statErr) {
		t.Fatal("recovery must never create settings/")
	}
}

func TestRecoveryRefusesLegacyMarker(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"<!-- re-discipline:shared-laws v0.6.0 -->\n<!-- re-discipline:shared-laws:end -->\n")
	if _, err := RecoverProject(root, pluginRootForTests(t)); err == nil {
		t.Fatal("v0.6.0 marker must be refused by v0.8 recovery")
	}
}
