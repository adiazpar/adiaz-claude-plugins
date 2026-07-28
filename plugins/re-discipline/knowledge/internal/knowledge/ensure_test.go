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

func TestEnsureMigratesRepairsAndReportsWithinBudget(t *testing.T) {
	root := legacyFixtureProject(t)
	payload, err := EnsureProject(context.Background(), root, pluginRootForTests(t), 7000)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ensureBlock, ok := payload["ensure"].(map[string]any)
	if !ok {
		t.Fatalf("ensure block missing: %#v", payload)
	}
	if ensureBlock["migrated"] != true {
		t.Fatal("legacy project must be migrated")
	}
	if _, ok := payload["user"]; !ok {
		t.Fatal("ensure must return the user block")
	}
	if _, ok := payload["system"]; !ok {
		t.Fatal("ensure must return the system block")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".re-discipline", "settings")); !os.IsNotExist(statErr) {
		t.Fatal("settings/ must be gone after ensure")
	}

	payload2, err := EnsureProject(context.Background(), root, pluginRootForTests(t), 7000)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if payload2["ensure"].(map[string]any)["migrated"] != false {
		t.Fatal("ensure must be idempotent")
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
