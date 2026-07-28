package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func adversarialPluginRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(adversarialAssetRoot(t))
}

func runRecoveryGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func initializeRecoveryRepository(t *testing.T, root string, paths ...string) {
	t.Helper()
	runRecoveryGit(t, root, "init")
	runRecoveryGit(t, root, "config", "user.name", "Recovery Test")
	runRecoveryGit(t, root, "config", "user.email", "recovery@example.invalid")
	runRecoveryGit(t, root, "config", "core.autocrlf", "false")
	runRecoveryGit(t, root, append([]string{"add", "--"}, paths...)...)
	runRecoveryGit(t, root, "commit", "-m", "recovery fixture")
}

func snapshotRecoveryTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snapshot := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertRecoveryTreeUnchanged(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	after := snapshotRecoveryTree(t, root)
	if len(after) != len(before) {
		t.Fatalf("native-memory tree changed shape: before=%v after=%v", before, after)
	}
	for path, expected := range before {
		actual, ok := after[path]
		if !ok || !bytes.Equal(actual, expected) {
			t.Fatalf("native-memory path %q was touched", path)
		}
	}
}

func redirectNativeMemoryRoots(t *testing.T, parent string) (string, map[string][]byte) {
	t.Helper()
	nativeRoot := filepath.Join(parent, "native-manager-state")
	claudeRoot := filepath.Join(nativeRoot, "claude")
	codexRoot := filepath.Join(nativeRoot, "codex")
	writeTestFile(t, filepath.Join(claudeRoot, "projects", "memory", "MEMORY.md"),
		"CLAUDE_NATIVE_SENTINEL")
	writeTestFile(t, filepath.Join(codexRoot, "memories", "MEMORY.md"),
		"CODEX_NATIVE_SENTINEL")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeRoot)
	t.Setenv("CODEX_HOME", codexRoot)
	t.Setenv("HOME", filepath.Join(nativeRoot, "home"))
	t.Setenv("USERPROFILE", filepath.Join(nativeRoot, "user-profile"))
	t.Setenv("APPDATA", filepath.Join(nativeRoot, "app-data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(nativeRoot, "local-app-data"))
	return nativeRoot, snapshotRecoveryTree(t, nativeRoot)
}

func assertMCPStatusReportsInvalid(t *testing.T, root, expectedText string) {
	t.Helper()
	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t)},
		initializeMessage(1, false),
		toolCallMessage(2, "status", map[string]any{"projectRoot": root}),
	)
	status := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	configuration := asObject(t, status["configuration"])
	if configuration["valid"] != false {
		t.Fatalf("safely diagnosable malformed configuration was reported valid: %#v", status)
	}
	errorsValue := asArray(t, configuration["errors"])
	if len(errorsValue) == 0 {
		t.Fatalf("invalid configuration omitted diagnostics: %#v", configuration)
	}
	if expectedText != "" && !strings.Contains(
		strings.Join(func() []string {
			out := make([]string, 0, len(errorsValue))
			for _, value := range errorsValue {
				out = append(out, fmt.Sprint(value))
			}
			return out
		}(), "\n"),
		expectedText,
	) {
		t.Fatalf("invalid configuration diagnostics omit %q: %#v",
			expectedText, configuration)
	}
}

func TestAdversarialRecoveryPrecedesMCPValidationAndNeverTouchesNativeMemory(t *testing.T) {
	parent := t.TempDir()
	nativeRoot, nativeBefore := redirectNativeMemoryRoots(t, parent)
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	fixture := makeAdversarialProject(t)
	err := filepath.WalkDir(fixture, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatalf("initial recovery: %v", err)
	}
	trackedConfig := []byte("{\n  \"schemaVersion\": 2,\n  \"knowledgeDirectory\": \"knowledge\",\n  \"memory\": {\"mode\": \"shared-only\", \"writePolicy\": \"proposal-only\"},\n  \"knowledge\": {\"enabled\": true, \"profile\": \"plugin:balanced-v1\", \"settingsFile\": \"knowledge/policy.jsonc\", \"projectProfile\": \"knowledge/retrieval-profile.json\"}\n}\n")
	configPath := filepath.Join(root, ".re-discipline", "config.json")
	if err := os.WriteFile(configPath, trackedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, ".claude", "settings.json")
	trackedClaude := []byte("{\n  \"permissions\": {\"allow\": [\"Read\"]},\n  \"autoMemoryEnabled\": false\n}\n")
	if err := os.WriteFile(claudePath, trackedClaude, 0o600); err != nil {
		t.Fatal(err)
	}
	knowledgePath := filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc")
	trackedKnowledge, err := os.ReadFile(knowledgePath)
	if err != nil {
		t.Fatal(err)
	}
	tracked := []string{
		".re-discipline/project-profile.md",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".claude/settings.json",
	}
	initializeRecoveryRepository(t, root, tracked...)

	codexPath := filepath.Join(root, ".codex", "config.toml")
	profilePath := filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")
	for _, path := range []string{configPath, knowledgePath, claudePath, codexPath, profilePath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	runRecoveryGit(t, root, "add", "-u", "--",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".claude/settings.json",
	)
	stagedBefore := runRecoveryGit(t, root, "diff", "--cached", "--name-status", "--",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".claude/settings.json",
	)

	messages := runMCPMessages(
		t,
		&MCPServer{
			AssetRoot:   adversarialAssetRoot(t),
			InitialRoot: root,
		},
		initializeMessage(1, false),
		toolCallMessage(2, "status", map[string]any{}),
	)
	status := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	configuration := asObject(t, status["configuration"])
	if configuration["memoryMode"] != "shared-only" ||
		configuration["nativeMemoryTouched"] != false {
		t.Fatalf("recovered status violates shared-only policy: %#v", status)
	}

	for path, expected := range map[string][]byte{
		configPath:    trackedConfig,
		knowledgePath: trackedKnowledge,
		claudePath:    trackedClaude,
	} {
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("tracked deletion %q was not restored byte-for-byte from HEAD", path)
		}
	}
	for target, template := range map[string]string{
		codexPath:   "codex-config.toml",
		profilePath: "retrieval-profile.json",
	} {
		actual, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(filepath.Join(
			adversarialPluginRoot(t), "templates", "project", template,
		))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("untracked deletion %q did not use the packaged safe default", target)
		}
	}
	stagedAfter := runRecoveryGit(t, root, "diff", "--cached", "--name-status", "--",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".claude/settings.json",
	)
	if stagedAfter != stagedBefore {
		t.Fatalf("recovery modified the staged deletion intent:\nbefore:\n%s\nafter:\n%s",
			stagedBefore, stagedAfter)
	}
	assertRecoveryTreeUnchanged(t, nativeRoot, nativeBefore)
}

func TestAdversarialRecoveryPreservesMalformedOrAmbiguousFilesAndFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		path string
		body []byte
	}{
		{
			name: "bootstrap trailing JSON",
			path: ".re-discipline/config.json",
			body: []byte("{\"schemaVersion\":1}\n{}\n"),
		},
		{
			name: "Claude trailing JSON",
			path: ".claude/settings.json",
			body: []byte("{\"autoMemoryEnabled\":false}\n{}\n"),
		},
		{
			name: "Claude duplicate managed key",
			path: ".claude/settings.json",
			body: []byte("{\"autoMemoryEnabled\":false,\"autoMemoryEnabled\":true}\n"),
		},
		{
			name: "Claude nested managed key ambiguity",
			path: ".claude/settings.json",
			body: []byte("{\"autoMemoryEnabled\":false,\"nested\":{\"autoMemoryEnabled\":true}}\n"),
		},
		{
			name: "Claude escaped managed key ambiguity",
			path: ".claude/settings.json",
			body: []byte("{\"auto\\u004demoryEnabled\":false}\n"),
		},
		{
			name: "Codex duplicate managed keys",
			path: ".codex/config.toml",
			body: []byte("[features]\nmemories = false\nmemories = true\n\n[memories]\ngenerate_memories = false\ngenerate_memories = true\nuse_memories = false\n"),
		},
		{
			name: "Codex inline table managed key ambiguity",
			path: ".codex/config.toml",
			body: []byte("features = { memories = true }\n"),
		},
		{
			name: "Codex scalar and table prefix conflict",
			path: ".codex/config.toml",
			body: []byte("features = false\n\n[features]\nmemories = true\n"),
		},
		{
			name: "Codex memories scalar and table conflict",
			path: ".codex/config.toml",
			body: []byte("memories = false\n\n[memories]\ngenerate_memories = true\nuse_memories = true\n"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parent := t.TempDir()
			nativeRoot, nativeBefore := redirectNativeMemoryRoots(t, parent)
			root := makeAdversarialProject(t)
			if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, filepath.FromSlash(testCase.path))
			if err := os.WriteFile(target, testCase.body, 0o600); err != nil {
				t.Fatal(err)
			}
			unrelatedMissing := filepath.Join(root, ".re-discipline", "knowledge", "README.md")
			if err := os.Remove(unrelatedMissing); err != nil {
				t.Fatal(err)
			}

			if _, err := RecoverProject(root, adversarialPluginRoot(t)); err == nil {
				t.Fatal("recovery accepted malformed or ambiguous managed configuration")
			}
			preserved, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(preserved, testCase.body) {
				t.Fatal("recovery rewrote malformed or ambiguous bytes")
			}
			if _, err := os.Stat(unrelatedMissing); !os.IsNotExist(err) {
				t.Fatal("recovery made partial changes before validating surviving files")
			}
			assertMCPStatusReportsInvalid(t, root, "")
			assertRecoveryTreeUnchanged(t, nativeRoot, nativeBefore)
		})
	}
}

func TestAdversarialRecoveryReconcilesTrackedSharedOnlyHostPolicy(t *testing.T) {
	root := makeAdversarialProject(t)
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, ".claude", "settings.json")
	claudeTracked := []byte("{\n  \"permissions\": {\"allow\": [\"Read\"]},\n  \"autoMemoryEnabled\": true\n}\n")
	if err := os.WriteFile(claudePath, claudeTracked, 0o600); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(root, ".codex", "config.toml")
	codexTracked := []byte("# preserve this comment\nmodel = \"gpt-test\"\n\n[features]\nweb_search = true\nmemories = true\n\n[memories]\nmax_unused_days = 30\ngenerate_memories = true\nuse_memories = true\n")
	if err := os.WriteFile(codexPath, codexTracked, 0o600); err != nil {
		t.Fatal(err)
	}
	initializeRecoveryRepository(t, root,
		".re-discipline/project-profile.md",
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".re-discipline/knowledge/retrieval-profile.json",
		".claude/settings.json",
		".codex/config.toml",
	)
	if err := os.Remove(claudePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(codexPath); err != nil {
		t.Fatal(err)
	}
	runRecoveryGit(t, root, "add", "-u", "--", ".claude/settings.json", ".codex/config.toml")
	stagedBefore := runRecoveryGit(t, root, "diff", "--cached", "--name-status", "--",
		".claude/settings.json", ".codex/config.toml")

	result, err := RecoverProject(root, adversarialPluginRoot(t))
	if err != nil {
		t.Fatalf("tracked host policy recovery failed: %v", err)
	}
	if result.NativeMemoryTouched {
		t.Fatal("host policy recovery reported touching native memory")
	}
	claudeBody, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := json.Unmarshal(claudeBody, &claude); err != nil {
		t.Fatal(err)
	}
	if claude["autoMemoryEnabled"] != false {
		t.Fatalf("tracked Claude policy re-enabled native memory: %s", claudeBody)
	}
	if _, ok := claude["permissions"]; !ok {
		t.Fatal("Claude host policy reconciliation discarded unrelated settings")
	}
	codexBody, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	codex := string(codexBody)
	for _, retained := range []string{
		"# preserve this comment",
		"model = \"gpt-test\"",
		"web_search = true",
		"max_unused_days = 30",
	} {
		if !strings.Contains(codex, retained) {
			t.Fatalf("Codex host policy reconciliation discarded %q:\n%s", retained, codex)
		}
	}
	for _, required := range []string{
		"memories = false",
		"generate_memories = false",
		"use_memories = false",
	} {
		if !strings.Contains(codex, required) {
			t.Fatalf("Codex host policy lacks %q:\n%s", required, codex)
		}
	}
	for _, forbidden := range []string{
		"memories = true",
		"generate_memories = true",
		"use_memories = true",
	} {
		if strings.Contains(codex, forbidden) {
			t.Fatalf("Codex host policy retained %q:\n%s", forbidden, codex)
		}
	}
	firstClaude := append([]byte(nil), claudeBody...)
	firstCodex := append([]byte(nil), codexBody...)
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	secondClaude, _ := os.ReadFile(claudePath)
	secondCodex, _ := os.ReadFile(codexPath)
	if !bytes.Equal(firstClaude, secondClaude) || !bytes.Equal(firstCodex, secondCodex) {
		t.Fatal("host policy reconciliation is not idempotent")
	}
	stagedAfter := runRecoveryGit(t, root, "diff", "--cached", "--name-status", "--",
		".claude/settings.json", ".codex/config.toml")
	if stagedAfter != stagedBefore {
		t.Fatal("host policy recovery changed the staged deletions")
	}

	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		toolCallMessage(2, "status", map[string]any{}),
	)
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
}

func TestAdversarialRecoveryRejectsUnsupportedMarkerWithoutMutation(t *testing.T) {
	parent := t.TempDir()
	nativeRoot, nativeBefore := redirectNativeMemoryRoots(t, parent)
	root := makeAdversarialProject(t)
	markerPath := filepath.Join(root, ".re-discipline", "project-profile.md")
	original, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	newer := bytes.ReplaceAll(original, []byte("v0.7.0"), []byte("v0.8.0"))
	if err := os.WriteFile(markerPath, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".re-discipline", "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err == nil {
		t.Fatal("recovery attempted to downgrade an unsupported managed marker")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("unsupported-marker recovery created managed files")
	}
	assertRecoveryTreeUnchanged(t, nativeRoot, nativeBefore)
}

func TestAdversarialRecoveryRejectsExistingManagedFileLinkEscape(t *testing.T) {
	root := makeAdversarialProject(t)
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".claude", "settings.json")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-claude-settings.json")
	outsideBody := []byte("{\"autoMemoryEnabled\":false}\n")
	if err := os.WriteFile(outside, outsideBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if !makeFileLink(t, outside, target) {
		t.Skip("file links are unavailable")
	}
	before := snapshotRecoveryTree(t, root)
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err == nil {
		t.Fatal("recovery followed an existing managed-file link outside the project")
	}
	afterOutside, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterOutside, outsideBody) {
		t.Fatal("recovery mutated the outside target of a managed-file link")
	}
	assertRecoveryTreeUnchanged(t, root, before)
}

func TestAdversarialRecoveryRestoresMCPOnlyRequiredTopology(t *testing.T) {
	parent := t.TempDir()
	nativeRoot, nativeBefore := redirectNativeMemoryRoots(t, parent)
	root := makeAdversarialProject(t)
	for _, relative := range []string{
		".re-discipline/memory/proposals",
		".re-discipline/memory/topics",
		".re-discipline/knowledge/evals",
		".re-discipline/cache",
	} {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		".re-discipline/memory/proposals",
		".re-discipline/memory/topics",
		".re-discipline/knowledge/evals",
		".re-discipline/cache/knowledge",
		".re-discipline/cache/knowledge/generations",
		".re-discipline/cache/knowledge/vectors",
		".re-discipline/cache/calibration",
	} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !info.IsDir() {
			t.Errorf("MCP recovery omitted required directory %s: %v", relative, err)
		}
	}
	service := newAdversarialService(t, root, nil)
	proposal, err := service.RecallPropose(
		context.Background(), "Recovered topology", "proposal-body", nil)
	if err != nil {
		t.Fatalf("recall_propose failed after MCP-only recovery: %v", err)
	}
	path, _ := proposal["path"].(string)
	if !strings.HasPrefix(path, ".re-discipline/memory/proposals/") {
		t.Fatalf("recall proposal escaped recovered topology: %#v", proposal)
	}
	assertRecoveryTreeUnchanged(t, nativeRoot, nativeBefore)
}

func makeRecoveryDirectoryUnwritable(t *testing.T, directory string) (func(), bool) {
	t.Helper()
	restore := func() {}
	if runtime.GOOS == "windows" {
		user := os.Getenv("USERNAME")
		if user == "" {
			return restore, false
		}
		command := exec.Command(
			"icacls", directory, "/inheritance:r", "/deny", user+":(W,D,DC)")
		if output, err := command.CombinedOutput(); err != nil {
			t.Logf("cannot install temporary write-deny ACL: %v (%s)",
				err, strings.TrimSpace(string(output)))
			return restore, false
		}
		restore = func() {
			_, _ = exec.Command("icacls", directory, "/remove:d", user).CombinedOutput()
			_, _ = exec.Command("icacls", directory, "/inheritance:e").CombinedOutput()
		}
	} else {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		mode := info.Mode().Perm()
		if err := os.Chmod(directory, 0o500); err != nil {
			return restore, false
		}
		restore = func() { _ = os.Chmod(directory, mode) }
	}
	probe := filepath.Join(directory, ".recovery-write-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err == nil {
		_ = os.Remove(probe)
		restore()
		return func() {}, false
	}
	return restore, true
}

func TestAdversarialRecoveryRollsBackOnLateWriteFailure(t *testing.T) {
	root := makeAdversarialProject(t)
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, ".claude", "settings.json")
	codexPath := filepath.Join(root, ".codex", "config.toml")
	writeTestFile(t, claudePath,
		"{\"permissions\":{\"allow\":[\"Read\"]},\"autoMemoryEnabled\":true}\n")
	writeTestFile(t, codexPath,
		"[features]\nmemories = true\n\n[memories]\ngenerate_memories = true\nuse_memories = true\n")
	before := snapshotRecoveryTree(t, root)
	restore, induced := makeRecoveryDirectoryUnwritable(t, filepath.Dir(codexPath))
	if !induced {
		t.Skip("filesystem cannot induce a deterministic late write failure")
	}
	defer restore()
	if _, err := RecoverProject(root, adversarialPluginRoot(t)); err == nil {
		t.Fatal("recovery unexpectedly completed through an unwritable late target")
	}
	restore()
	assertRecoveryTreeUnchanged(t, root, before)
}
