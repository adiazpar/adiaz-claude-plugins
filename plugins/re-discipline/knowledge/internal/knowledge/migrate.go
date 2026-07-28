package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrationResult reports what MigrateLegacyLayout did. Moves use
// "old -> new" notation; Warnings are non-fatal follow-ups a manager may
// want to see in system-facing state.
type MigrationResult struct {
	Migrated bool     `json:"migrated"`
	Moves    []string `json:"moves"`
	Warnings []string `json:"warnings"`
}

const legacySharedLawsMarker = "<!-- re-discipline:shared-laws v0.6.0 -->"

// legacyBootstrap mirrors the v1 config.json shape so a legacy file can be
// decoded leniently before it is rewritten to the v2 contract.
type legacyBootstrap struct {
	SchemaVersion     int             `json:"schemaVersion"`
	SettingsDirectory string          `json:"settingsDirectory"`
	Memory            MemoryConfig    `json:"memory"`
	Knowledge         KnowledgeConfig `json:"knowledge"`
}

// MigrateLegacyLayout moves a v0.6.0 project (settings/ control plane) to
// the v0.7.0 layout (knowledge/ control plane) in place. It is idempotent:
// projects without the legacy marker and layout return Migrated:false.
// Only ensure calls this; recovery and status refuse legacy projects.
func MigrateLegacyLayout(projectRoot string) (MigrationResult, error) {
	result := MigrationResult{Moves: []string{}, Warnings: []string{}}
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return result, err
	}
	profilePath := filepath.Join(boundary.Root, ".re-discipline", "project-profile.md")
	profileBody, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		return result, nil
	}
	settingsDir := filepath.Join(boundary.Root, ".re-discipline", "settings")
	settingsInfo, settingsErr := os.Lstat(settingsDir)
	hasLegacyMarker := strings.Contains(string(profileBody), legacySharedLawsMarker)
	hasLegacyDir := settingsErr == nil && settingsInfo.IsDir()
	if !hasLegacyMarker || !hasLegacyDir {
		return result, nil
	}

	knowledgeDir := filepath.Join(boundary.Root, ".re-discipline", "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		return result, err
	}

	moves := []struct{ from, to string }{
		{"settings/knowledge.jsonc", "knowledge/policy.jsonc"},
		{"settings/retrieval-profile.json", "knowledge/retrieval-profile.json"},
	}
	for _, move := range moves {
		from := filepath.Join(boundary.Root, ".re-discipline", filepath.FromSlash(move.from))
		to := filepath.Join(boundary.Root, ".re-discipline", filepath.FromSlash(move.to))
		info, statErr := os.Lstat(from)
		if statErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("legacy file %s absent; recovery will create %s", move.from, move.to))
			continue
		}
		if !info.Mode().IsRegular() {
			return result, fmt.Errorf("legacy file %s is not a regular file", move.from)
		}
		if _, existsErr := os.Lstat(to); existsErr == nil {
			return result, fmt.Errorf("migration target %s already exists", move.to)
		}
		if renameErr := os.Rename(from, to); renameErr != nil {
			body, copyReadErr := os.ReadFile(from)
			if copyReadErr != nil {
				return result, copyReadErr
			}
			if writeErr := AtomicWrite(to, body, 0o644); writeErr != nil {
				return result, writeErr
			}
			if removeErr := os.Remove(from); removeErr != nil {
				return result, removeErr
			}
		}
		result.Moves = append(result.Moves, move.from+" -> "+move.to)
	}

	// settings/README.md is superseded by the packaged agent reference; its
	// content stays reachable through git history.
	legacyReadme := filepath.Join(settingsDir, "README.md")
	if _, statErr := os.Lstat(legacyReadme); statErr == nil {
		if removeErr := os.Remove(legacyReadme); removeErr != nil {
			return result, removeErr
		}
		result.Moves = append(result.Moves, "settings/README.md -> removed (superseded)")
	}
	if entries, dirErr := os.ReadDir(settingsDir); dirErr == nil {
		if len(entries) == 0 {
			if removeErr := os.Remove(settingsDir); removeErr != nil {
				return result, removeErr
			}
		} else {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			result.Warnings = append(result.Warnings,
				"settings/ retained: unexpected entries "+strings.Join(names, ", "))
		}
	}

	// Rewrite config.json to the v2 contract, carrying the project's own
	// memory and knowledge choices.
	configPath := filepath.Join(boundary.Root, ".re-discipline", "config.json")
	migrated := DefaultBootstrapConfig()
	if legacyRaw, configErr := os.ReadFile(configPath); configErr == nil {
		var legacy legacyBootstrap
		if jsonErr := json.Unmarshal(legacyRaw, &legacy); jsonErr == nil {
			if legacy.Memory.Mode != "" {
				migrated.Memory.Mode = legacy.Memory.Mode
			}
			if legacy.Memory.WritePolicy != "" {
				migrated.Memory.WritePolicy = legacy.Memory.WritePolicy
			}
			migrated.Knowledge.Enabled = legacy.Knowledge.Enabled
			if legacy.Knowledge.Profile != "" {
				migrated.Knowledge.Profile = legacy.Knowledge.Profile
			}
		} else {
			result.Warnings = append(result.Warnings,
				"legacy config.json unparsable; rewrote from defaults")
		}
	}
	if writeErr := AtomicWriteJSON(configPath, migrated, 0o644); writeErr != nil {
		return result, writeErr
	}

	// Bump the shared-laws marker; the old binary's handshake now refuses
	// this project, which is the point.
	bumped := strings.Replace(string(profileBody), legacySharedLawsMarker, SharedLawsMarker, 1)
	if writeErr := AtomicWrite(profilePath, []byte(bumped), 0o644); writeErr != nil {
		return result, writeErr
	}

	adapterMarkers := []struct{ path, old, new string }{
		{".claude/CLAUDE.md", "re-discipline:claude-adapter v0.6.0", "re-discipline:claude-adapter v0.7.0"},
		{".codex/AGENTS.md", "re-discipline:codex-adapter v0.6.0", "re-discipline:codex-adapter v0.7.0"},
		{"AGENTS.md", "re-discipline:router v0.6.0", "re-discipline:router v0.7.0"},
	}
	for _, adapter := range adapterMarkers {
		path := filepath.Join(boundary.Root, filepath.FromSlash(adapter.path))
		body, adapterErr := os.ReadFile(path)
		if adapterErr != nil {
			continue
		}
		if !strings.Contains(string(body), adapter.old) {
			result.Warnings = append(result.Warnings,
				adapter.path+" carries no v0.6.0 adapter marker; left unchanged")
			continue
		}
		next := strings.Replace(string(body), adapter.old, adapter.new, 1)
		if writeErr := AtomicWrite(path, []byte(next), 0o644); writeErr != nil {
			return result, writeErr
		}
		result.Moves = append(result.Moves, adapter.path+" marker -> v0.7.0")
	}

	// The project-local external dispatcher pins a marker regex; widen it so
	// migrated projects keep dispatching.
	dispatchPath := filepath.Join(boundary.Root, ".re-discipline", "agents", "dispatch.ps1")
	if body, dispatchErr := os.ReadFile(dispatchPath); dispatchErr == nil {
		const oldRegex = `v0\.6\.\d+`
		const newRegex = `v0\.[67]\.\d+`
		if strings.Contains(string(body), oldRegex) {
			next := strings.Replace(string(body), oldRegex, newRegex, 1)
			if writeErr := AtomicWrite(dispatchPath, []byte(next), 0o644); writeErr != nil {
				return result, writeErr
			}
			result.Moves = append(result.Moves, ".re-discipline/agents/dispatch.ps1 marker regex widened")
		} else if !strings.Contains(string(body), newRegex) {
			result.Warnings = append(result.Warnings, "dispatch.ps1 marker regex not migrated")
		}
	}

	result.Migrated = true
	return result, nil
}
