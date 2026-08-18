package knowledge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func ensureSourcePath(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Join(filepath.Dir(self), "ensure.go")
}

// legacyFixtureProject models only the read-only 0.7 detection boundary. The
// 0.8 runtime intentionally contains no legacy-layout writer.
func legacyFixtureProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# P\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlaws\n<!-- re-discipline:shared-laws:end -->\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "settings", "knowledge.jsonc"),
		"// legacy policy\n{\"schemaVersion\":1}\n")
	return root
}

func TestEnsureDetectsLegacyWithoutMutation(t *testing.T) {
	root := legacyFixtureProject(t)
	settingsBefore, err := os.ReadFile(filepath.Join(root, ".re-discipline", "settings", "knowledge.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := EnsureProject(context.Background(), root, pluginRootForTests(t), 7000)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ensureBlock, ok := payload["ensure"].(map[string]any)
	if !ok {
		t.Fatalf("ensure block missing: %#v", payload)
	}
	if ensureBlock["migrated"] != false || ensureBlock["projectStateVersion"] != "0.7" {
		t.Fatalf("legacy ensure must report without migrating: %#v", ensureBlock)
	}
	if _, ok := payload["user"]; !ok {
		t.Fatal("ensure must return the user block")
	}
	if _, ok := payload["system"]; !ok {
		t.Fatal("ensure must return the system block")
	}
	settingsAfter, readErr := os.ReadFile(filepath.Join(root, ".re-discipline", "settings", "knowledge.jsonc"))
	if readErr != nil || !bytes.Equal(settingsBefore, settingsAfter) {
		t.Fatal("legacy ensure changed the project")
	}

	payload2, err := EnsureProject(context.Background(), root, pluginRootForTests(t), 7000)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if payload2["ensure"].(map[string]any)["projectStateVersion"] != "0.7" {
		t.Fatal("legacy ensure must remain a read-only attention result")
	}
}

func TestDetectProjectStateVersionSeparatesBootstrapAndCampaignSchemas(t *testing.T) {
	root := legacyFixtureProject(t)
	version, err := DetectProjectStateVersion(root)
	if err != nil || version != "0.7" {
		t.Fatalf("legacy detection: %q %v", version, err)
	}
	if err := AtomicWriteJSON(filepath.Join(root, ".re-discipline", "state", "head.json"),
		initialStateHead(), 0o600); err != nil {
		t.Fatal(err)
	}
	version, err = DetectProjectStateVersion(root)
	if err != nil || version != "0.8" {
		t.Fatalf("0.8 detection: %q %v", version, err)
	}
	if err := os.Remove(filepath.Join(root, ".re-discipline", "state", "head.json")); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(root, ".re-discipline", "project-profile.md"),
		[]byte("# Recoverable project\n\n"+SharedLawsMarker+"\nmanaged\n<!-- re-discipline:shared-laws:end -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	version, err = DetectProjectStateVersion(root)
	if err != nil || version != "0.8" {
		t.Fatalf("damaged 0.8 marker detection: %q %v", version, err)
	}
}

func TestEnsureNeverRunsExpensiveOperations(t *testing.T) {
	src, err := os.ReadFile(ensureSourcePath(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{
		"RunProjectBenchmark", "RunPackagedBenchmark", "Calibrate",
		"PromoteProfile", "PinEvals", "UpdateDeclaredBenchmarks",
	} {
		if bytes.Contains(src, []byte(banned)) {
			t.Fatalf("ensure must never reference %s", banned)
		}
	}
}
