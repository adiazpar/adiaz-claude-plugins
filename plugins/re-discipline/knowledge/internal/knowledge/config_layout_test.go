package knowledge

import "testing"

func TestBootstrapValidationPinsKnowledgeLayout(t *testing.T) {
	valid := DefaultBootstrapConfig()
	if err := ValidateBootstrap(valid); err != nil {
		t.Fatalf("default bootstrap must validate, got %v", err)
	}
	if valid.SchemaVersion != 2 {
		t.Fatalf("bootstrap schemaVersion must be 2, got %d", valid.SchemaVersion)
	}
	if valid.KnowledgeDirectory != "knowledge" {
		t.Fatalf("knowledgeDirectory must be knowledge, got %q", valid.KnowledgeDirectory)
	}
	if valid.Knowledge.SettingsFile != "knowledge/policy.jsonc" {
		t.Fatalf("settingsFile must be knowledge/policy.jsonc, got %q", valid.Knowledge.SettingsFile)
	}
	if valid.Knowledge.ProjectProfile != "knowledge/retrieval-profile.json" {
		t.Fatalf("projectProfile must be knowledge/retrieval-profile.json, got %q", valid.Knowledge.ProjectProfile)
	}
	legacy := DefaultBootstrapConfig()
	legacy.SchemaVersion = 1
	if err := ValidateBootstrap(legacy); err == nil {
		t.Fatal("schemaVersion 1 must be rejected by v2 validation")
	}
	wrongPath := DefaultBootstrapConfig()
	wrongPath.Knowledge.SettingsFile = "settings/knowledge.jsonc"
	if err := ValidateBootstrap(wrongPath); err == nil {
		t.Fatal("legacy settings path must be rejected")
	}
}
