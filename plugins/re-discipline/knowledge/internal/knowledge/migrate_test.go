package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyFixtureProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# P\n\n<!-- re-discipline:shared-laws v0.6.0 -->\nlaws\n<!-- re-discipline:shared-laws:end -->\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "config.json"),
		`{"schemaVersion":1,"settingsDirectory":"settings","memory":{"mode":"shared-only","writePolicy":"proposal-only"},"knowledge":{"enabled":true,"profile":"plugin:balanced-v1","settingsFile":"settings/knowledge.jsonc","projectProfile":"settings/retrieval-profile.json"}}`)
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "settings", "knowledge.jsonc"),
		"// policy\n{\"schemaVersion\":1}\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "settings", "retrieval-profile.json"),
		"{\"profileId\":\"plugin:balanced-v1\"}\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "settings", "README.md"),
		"old control plane doc\n")
	return root
}

func TestMigrateLegacyLayout(t *testing.T) {
	root := legacyFixtureProject(t)

	result, err := MigrateLegacyLayout(root)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !result.Migrated {
		t.Fatal("expected migration to run")
	}
	policy, _ := os.ReadFile(filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"))
	if string(policy) != "// policy\n{\"schemaVersion\":1}\n" {
		t.Fatalf("policy content changed: %q", policy)
	}
	profileBody, _ := os.ReadFile(filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"))
	if string(profileBody) != "{\"profileId\":\"plugin:balanced-v1\"}\n" {
		t.Fatalf("retrieval profile content changed: %q", profileBody)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".re-discipline", "settings")); !os.IsNotExist(statErr) {
		t.Fatal("settings/ must be removed after migration")
	}
	var cfg BootstrapConfig
	raw, _ := os.ReadFile(filepath.Join(root, ".re-discipline", "config.json"))
	if jsonErr := json.Unmarshal(raw, &cfg); jsonErr != nil {
		t.Fatalf("migrated config unparsable: %v", jsonErr)
	}
	if cfg.SchemaVersion != 2 || cfg.Memory.Mode != "shared-only" ||
		cfg.Knowledge.SettingsFile != "knowledge/policy.jsonc" {
		t.Fatalf("migrated config wrong: %+v", cfg)
	}
	if err := ValidateBootstrap(cfg); err != nil {
		t.Fatalf("migrated config must validate: %v", err)
	}
	profile, _ := os.ReadFile(filepath.Join(root, ".re-discipline", "project-profile.md"))
	if !strings.Contains(string(profile), "shared-laws v0.7.0") ||
		strings.Contains(string(profile), "v0.6.0") {
		t.Fatal("marker must be v0.7.0 after migration")
	}
	second, err := MigrateLegacyLayout(root)
	if err != nil || second.Migrated {
		t.Fatalf("second run must be a no-op, got %+v err %v", second, err)
	}
}

func TestMigratePreservesMemoryModeAndAdapterMarkers(t *testing.T) {
	root := legacyFixtureProject(t)
	// Hybrid memory mode must survive; adapter markers must bump.
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "config.json"),
		`{"schemaVersion":1,"settingsDirectory":"settings","memory":{"mode":"hybrid","writePolicy":"proposal-only"},"knowledge":{"enabled":true,"profile":"plugin:balanced-v1","settingsFile":"settings/knowledge.jsonc","projectProfile":"settings/retrieval-profile.json"}}`)
	mustWriteFile(t, filepath.Join(root, ".claude", "CLAUDE.md"),
		"<!-- re-discipline:claude-adapter v0.6.0 -->\nadapter\n<!-- re-discipline:claude-adapter:end -->\nproject notes\n")
	mustWriteFile(t, filepath.Join(root, ".codex", "AGENTS.md"),
		"<!-- re-discipline:codex-adapter v0.6.0 -->\nadapter\n")
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"),
		"<!-- re-discipline:router v0.6.0 -->\nrouter\n")

	result, err := MigrateLegacyLayout(root)
	if err != nil || !result.Migrated {
		t.Fatalf("migrate: %+v %v", result, err)
	}
	var cfg BootstrapConfig
	raw, _ := os.ReadFile(filepath.Join(root, ".re-discipline", "config.json"))
	if jsonErr := json.Unmarshal(raw, &cfg); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if cfg.Memory.Mode != "hybrid" {
		t.Fatalf("memory mode must carry over, got %q", cfg.Memory.Mode)
	}
	claude, _ := os.ReadFile(filepath.Join(root, ".claude", "CLAUDE.md"))
	if !strings.Contains(string(claude), "claude-adapter v0.7.0") ||
		!strings.Contains(string(claude), "project notes") {
		t.Fatalf("claude adapter marker not bumped or notes lost: %s", claude)
	}
	codex, _ := os.ReadFile(filepath.Join(root, ".codex", "AGENTS.md"))
	if !strings.Contains(string(codex), "codex-adapter v0.7.0") {
		t.Fatalf("codex adapter marker not bumped: %s", codex)
	}
	router, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(string(router), "router v0.7.0") {
		t.Fatalf("router marker not bumped: %s", router)
	}
}

func TestMigrateIgnoresNonLegacyProjects(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"<!-- re-discipline:shared-laws v0.7.0 -->\n<!-- re-discipline:shared-laws:end -->\n")
	result, err := MigrateLegacyLayout(root)
	if err != nil {
		t.Fatalf("non-legacy project must not error: %v", err)
	}
	if result.Migrated {
		t.Fatal("non-legacy project must not migrate")
	}
}
