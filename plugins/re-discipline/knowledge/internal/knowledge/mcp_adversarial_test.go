package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runMCPMessages(t *testing.T, server *MCPServer, messages ...any) []map[string]any {
	t.Helper()
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for _, message := range messages {
		if err := encoder.Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := server.Serve(context.Background(), &input, &output); err != nil {
		t.Fatalf("MCP server failed: %v", err)
	}
	decoder := json.NewDecoder(&output)
	results := []map[string]any{}
	for {
		var message map[string]any
		err := decoder.Decode(&message)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid JSON-RPC output %q: %v", output.String(), err)
		}
		results = append(results, message)
	}
	return results
}

func rpcResponseByID(t *testing.T, messages []map[string]any, id any) map[string]any {
	t.Helper()
	want := fmt.Sprint(id)
	for _, message := range messages {
		if fmt.Sprint(message["id"]) == want && message["method"] == nil {
			return message
		}
	}
	t.Fatalf("JSON-RPC response %v not found in %#v", id, messages)
	return nil
}

func rpcRequestByMethod(t *testing.T, messages []map[string]any, method string) map[string]any {
	t.Helper()
	for _, message := range messages {
		if message["method"] == method {
			return message
		}
	}
	t.Fatalf("JSON-RPC request %q not found in %#v", method, messages)
	return nil
}

func asObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func asArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func assertSuccessfulToolResult(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	if response["error"] != nil {
		t.Fatalf("tool call returned JSON-RPC error: %#v", response)
	}
	result := asObject(t, response["result"])
	if result["isError"] != false {
		t.Fatalf("tool call returned an error result: %#v", result)
	}
	structured := asObject(t, result["structuredContent"])
	content := asArray(t, result["content"])
	if len(content) != 1 {
		t.Fatalf("tool result content count = %d, want 1", len(content))
	}
	textBlock := asObject(t, content[0])
	if textBlock["type"] != "text" {
		t.Fatalf("tool result omitted text compatibility block: %#v", textBlock)
	}
	text, ok := textBlock["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		t.Fatal("tool result omitted a nonempty text compatibility summary")
	}
	return structured
}

func assertToolError(t *testing.T, response map[string]any, containsText string) {
	t.Helper()
	result := asObject(t, response["result"])
	if result["isError"] != true {
		t.Fatalf("expected tool error, got %#v", result)
	}
	content := asArray(t, result["content"])
	if len(content) == 0 {
		t.Fatal("tool error omitted content")
	}
	text := asObject(t, content[0])["text"].(string)
	if containsText != "" && !strings.Contains(text, containsText) {
		t.Fatalf("tool error %q does not contain %q", text, containsText)
	}
	if _, ok := result["structuredContent"]; ok {
		t.Fatal("tool error mislabeled data as successful structuredContent")
	}
}

func initializeMessage(id int, roots bool) map[string]any {
	capabilities := map[string]any{}
	if roots {
		capabilities["roots"] = map[string]any{"listChanged": true}
	}
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    capabilities,
			"clientInfo": map[string]any{
				"name": "adversarial-test-client", "version": "1",
			},
		},
	}
}

func toolCallMessage(id int, name string, arguments map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	}
}

func managedFileURI(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	slash := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func TestAdversarialMCPToolSchemasAndAuthoritySurface(t *testing.T) {
	definitions := toolDefinitions()
	if len(definitions) != 7 {
		t.Fatalf("tool count = %d, want 7", len(definitions))
	}
	expected := map[string]bool{
		"status": true, "orient": true, "search": true,
		"read": true, "context_pack": true,
		"context_pack_materialize": true, "recall_propose": true,
	}
	for _, definition := range definitions {
		name, _ := definition["name"].(string)
		if !expected[name] {
			t.Fatalf("unexpected authority-bearing tool exposed: %q", name)
		}
		delete(expected, name)
		schema := asObject(t, definition["inputSchema"])
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatalf("%s input schema is not closed: %#v", name, schema)
		}
		annotations := asObject(t, definition["annotations"])
		if name == "recall_propose" || name == "context_pack_materialize" {
			if annotations["readOnlyHint"] != false ||
				annotations["destructiveHint"] != false ||
				annotations["idempotentHint"] != true ||
				annotations["openWorldHint"] != false {
				t.Fatalf("%s write annotations are unsafe: %#v", name, annotations)
			}
		} else if annotations["readOnlyHint"] != true ||
			annotations["destructiveHint"] != false ||
			annotations["idempotentHint"] != true ||
			annotations["openWorldHint"] != false {
			t.Fatalf("%s read-only annotations are incomplete: %#v", name, annotations)
		}
		if name == "orient" {
			properties := asObject(t, schema["properties"])
			budget := asObject(t, properties["tokenBudget"])
			if budget["minimum"] != float64(512) && budget["minimum"] != 512 {
				t.Fatalf("orient schema permits a budget rejected by execution: %#v", budget)
			}
		}
		if name == "read" {
			if _, ok := schema["oneOf"]; !ok {
				t.Fatal("read schema does not declare exactly one of path, chunkId, or uri")
			}
		}
		if name == "context_pack" || name == "context_pack_materialize" {
			properties := asObject(t, schema["properties"])
			requiredPaths := asObject(t, properties["requiredPaths"])
			if requiredPaths["type"] != "array" ||
				requiredPaths["uniqueItems"] != true ||
				requiredPaths["maxItems"] != 20 &&
					requiredPaths["maxItems"] != float64(20) {
				t.Fatalf("%s requiredPaths schema is not safely bounded: %#v",
					name, requiredPaths)
			}
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing MCP tools: %v", expected)
	}
	for _, forbidden := range []string{
		"accept_memory", "promote_truth", "activate_profile", "close_campaign",
		"sql", "command", "write_truth",
	} {
		for _, definition := range definitions {
			if definition["name"] == forbidden {
				t.Fatalf("general MCP surface exposes manager-only operation %q", forbidden)
			}
		}
	}
}

func TestAdversarialMCPProjectBudgetsAreEnforcedNotMerelyAdvertised(t *testing.T) {
	root := makeAdversarialProject(t)
	settingsPath := filepath.Join(
		root, ".re-discipline", "settings", "knowledge.jsonc")
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(
		string(body), `"searchTokens": 1024`, `"searchTokens": 512`, 1)
	changed = strings.Replace(
		changed, `"maxPassages": 12`, `"maxPassages": 2`, 1)
	if changed == string(body) {
		t.Fatal("test did not change project budget settings")
	}
	if err := os.WriteFile(settingsPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		map[string]any{
			"jsonrpc": "2.0", "method": "notifications/initialized",
		},
		toolCallMessage(2, "search", map[string]any{
			"query": "A1B2C3D4", "queryClass": "exact",
			"allowedTiers": []string{"truth"}, "limit": 12, "tokenBudget": 1024,
		}),
		toolCallMessage(3, "orient", map[string]any{
			"role": "drafter",
		}),
		toolCallMessage(4, "context_pack", map[string]any{
			"task": "engine serialization", "role": "drafter",
			"allowedTiers": []string{"truth"}, "tokenBudget": 2048,
		}),
	)
	search := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	if search["tokenBudget"] != float64(512) && search["tokenBudget"] != 512 {
		t.Fatalf(
			"explicit MCP search bypassed configured searchTokens ceiling: %#v",
			search["tokenBudget"])
	}
	if len(asArray(t, search["results"])) > 2 {
		t.Fatalf("explicit MCP search bypassed configured maxPassages: %#v", search["results"])
	}
	orientation := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	if orientation["role"] != "drafter" ||
		orientation["tokenBudget"] != float64(1024) &&
			orientation["tokenBudget"] != 1024 {
		t.Fatalf(
			"drafter orientation did not use the configured drafter budget: %#v",
			orientation)
	}
	pack := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 4))
	if pack["role"] != "drafter" ||
		pack["tokenBudget"] != float64(1024) &&
			pack["tokenBudget"] != 1024 {
		t.Fatalf(
			"explicit drafter context pack bypassed configured drafter ceiling: %#v",
			pack)
	}
}

func TestAdversarialMCPInitializeToolsAndStructuredResults(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	root := makeAdversarialProject(t)
	server := &MCPServer{
		AssetRoot:   adversarialAssetRoot(t),
		InitialRoot: filepath.Join(root, "docs", "truth"),
	}
	messages := runMCPMessages(
		t,
		server,
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(3, "status", map[string]any{}),
		toolCallMessage(4, "search", map[string]any{
			"query": "A1B2C3D4", "queryClass": "exact",
			"allowedTiers": []string{"truth"}, "limit": 5, "tokenBudget": 512,
		}),
		toolCallMessage(5, "search", map[string]any{
			"query": "A1B2C3D4", "unexpected": true,
		}),
	)

	initialize := asObject(t, rpcResponseByID(t, messages, 1)["result"])
	if initialize["protocolVersion"] != "2025-06-18" {
		t.Fatalf("server did not negotiate requested supported protocol: %#v", initialize)
	}
	serverInfo := asObject(t, initialize["serverInfo"])
	if serverInfo["name"] != "re-discipline-knowledge" || serverInfo["version"] != RuntimeVersion {
		t.Fatalf("server identity is incomplete: %#v", serverInfo)
	}
	toolsResult := asObject(t, rpcResponseByID(t, messages, 2)["result"])
	if len(asArray(t, toolsResult["tools"])) != 7 {
		t.Fatalf("tools/list result is incomplete: %#v", toolsResult)
	}
	status := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	configuration := asObject(t, status["configuration"])
	if configuration["nativeMemoryTouched"] != false {
		t.Fatal("knowledge status reported native-memory mutation")
	}
	search := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 4))
	results := asArray(t, search["results"])
	if len(results) == 0 {
		t.Fatal("MCP search missed exact fixture identifier")
	}
	citation := asObject(t, asObject(t, results[0])["citation"])
	if citation["path"] != "docs/truth/engine.md" || citation["tier"] != "truth" {
		t.Fatalf("MCP result citation is wrong: %#v", citation)
	}
	assertToolError(t, rpcResponseByID(t, messages, 5), "unknown field")
}

func TestAdversarialMCPRequiresInitialization(t *testing.T) {
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
	messages := runMCPMessages(
		t,
		server,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(2, "status", map[string]any{}),
	)
	for _, id := range []int{1, 2} {
		response := rpcResponseByID(t, messages, id)
		errorObject := asObject(t, response["error"])
		if errorObject["code"] != float64(-32002) {
			t.Fatalf("pre-initialize request %d did not fail with -32002: %#v", id, response)
		}
	}
}

func TestAdversarialMCPExplicitRootWithoutHostRoots(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	root := makeAdversarialProject(t)
	other := makeAdversarialProject(t)
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
	messages := runMCPMessages(
		t,
		server,
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		toolCallMessage(2, "status", map[string]any{"projectRoot": root}),
		toolCallMessage(3, "status", map[string]any{}),
		toolCallMessage(4, "status", map[string]any{"projectRoot": other}),
	)
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	assertToolError(t, rpcResponseByID(t, messages, 4), "session shard")
}

func TestAdversarialMCPExplicitStatusDoesNotRecoverAnUngrantedRoot(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	root := makeAdversarialProject(t)
	configPath := filepath.Join(root, ".re-discipline", "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	before := snapshotRecoveryTree(t, root)

	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t)},
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		toolCallMessage(2, "status", map[string]any{"projectRoot": root}),
	)
	status := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	configuration := asObject(t, status["configuration"])
	if configuration["valid"] != false {
		t.Fatalf("explicit diagnostic status concealed missing configuration: %#v", status)
	}
	errorsValue, ok := configuration["errors"].([]any)
	if !ok || len(errorsValue) == 0 {
		t.Fatalf("explicit diagnostic status omitted structured errors: %#v", configuration)
	}
	after := snapshotRecoveryTree(t, root)
	if stableJSON(before) != stableJSON(after) {
		t.Fatal("explicit status recovered or otherwise mutated an ungranted project root")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("explicit status recreated missing bootstrap configuration")
	}
}

func TestAdversarialMCPRejectsUnsupportedManagedProjectVersion(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	root := makeAdversarialProject(t)
	profilePath := filepath.Join(root, ".re-discipline", "project-profile.md")
	body, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.ReplaceAll(
		body,
		[]byte("<!-- re-discipline:shared-laws v0.6.0 -->"),
		[]byte("<!-- re-discipline:shared-laws v0.5.0 -->"),
	)
	if err := os.WriteFile(profilePath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
	messages := runMCPMessages(
		t,
		server,
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		toolCallMessage(2, "status", map[string]any{"projectRoot": root}),
	)
	assertToolError(t, rpcResponseByID(t, messages, 2), "shared-laws v0.6.0")
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "cache", "knowledge")); !os.IsNotExist(err) {
		t.Fatal("unsupported project version was mutated before rejection")
	}
}

func TestAdversarialMCPRootsSingleMultipleAndCrossRoot(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	first := makeAdversarialProject(t)
	second := makeAdversarialProject(t)
	third := makeAdversarialProject(t)

	t.Run("single root permits omission", func(t *testing.T) {
		server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
		messages := runMCPMessages(
			t,
			server,
			initializeMessage(1, true),
			map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
			map[string]any{
				"jsonrpc": "2.0", "id": "rd-roots-1",
				"result": map[string]any{
					"roots": []map[string]any{{"uri": managedFileURI(t, filepath.Join(first, "docs"))}},
				},
			},
			toolCallMessage(2, "status", map[string]any{}),
		)
		request := rpcRequestByMethod(t, messages, "roots/list")
		if request["id"] != "rd-roots-1" {
			t.Fatalf("unexpected roots/list correlation ID: %#v", request)
		}
		assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	})

	t.Run("multiple roots require an explicit granted root", func(t *testing.T) {
		server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
		messages := runMCPMessages(
			t,
			server,
			initializeMessage(1, true),
			map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
			map[string]any{
				"jsonrpc": "2.0", "id": "rd-roots-1",
				"result": map[string]any{
					"roots": []map[string]any{
						{"uri": managedFileURI(t, first)},
						{"uri": managedFileURI(t, second)},
					},
				},
			},
			toolCallMessage(2, "status", map[string]any{}),
			toolCallMessage(3, "status", map[string]any{"projectRoot": second}),
			toolCallMessage(4, "status", map[string]any{"projectRoot": third}),
		)
		assertToolError(t, rpcResponseByID(t, messages, 2), "multiple managed MCP roots")
		assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
		assertToolError(t, rpcResponseByID(t, messages, 4), "not granted")
	})

	t.Run("non-file and unmanaged roots are ignored", func(t *testing.T) {
		unmanaged := t.TempDir()
		server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
		messages := runMCPMessages(
			t,
			server,
			initializeMessage(1, true),
			map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
			map[string]any{
				"jsonrpc": "2.0", "id": "rd-roots-1",
				"result": map[string]any{
					"roots": []map[string]any{
						{"uri": "https://example.invalid/not-local"},
						{"uri": managedFileURI(t, unmanaged)},
					},
				},
			},
			toolCallMessage(2, "status", map[string]any{}),
		)
		assertToolError(t, rpcResponseByID(t, messages, 2), "no managed MCP root")
	})
}

func TestAdversarialMCPServiceCacheIsCanonicalAndRootIsolated(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	firstRoot := makeAdversarialProject(t)
	secondRoot := makeAdversarialProject(t)
	writeTestFile(
		t,
		filepath.Join(firstRoot, "docs", "truth", "engine.md"),
		"# First project\n\nroot-isolation-first-alpha\n",
	)
	writeTestFile(
		t,
		filepath.Join(secondRoot, "docs", "truth", "engine.md"),
		"# Second project\n\nroot-isolation-second-beta\n",
	)
	firstCanonical, err := validateManagedRoot(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	secondCanonical, err := validateManagedRoot(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	server := &MCPServer{
		AssetRoot:       adversarialAssetRoot(t),
		configuredRoots: []string{firstCanonical, secondCanonical},
		services:        map[string]*Service{},
	}

	first, err := server.service(filepath.Join(firstRoot, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	firstAgain, err := server.service(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.service(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	if first != firstAgain {
		t.Fatal("canonical aliases for one root did not reuse the cached service")
	}
	if first == second {
		t.Fatal("distinct project roots shared one cached service")
	}
	if len(server.services) != 2 {
		t.Fatalf("service cache contains %d entries, want one per canonical root", len(server.services))
	}
	if server.services[firstCanonical] != first || server.services[secondCanonical] != second {
		t.Fatalf("service cache keys are not canonical project roots: %#v", server.services)
	}

	firstRead, err := first.Read(context.Background(), ReadOptions{Path: "docs/truth/engine.md"})
	if err != nil {
		t.Fatal(err)
	}
	secondRead, err := second.Read(context.Background(), ReadOptions{Path: "docs/truth/engine.md"})
	if err != nil {
		t.Fatal(err)
	}
	passageBytes := func(value any) ([]byte, bool) {
		switch typed := value.(type) {
		case string:
			return []byte(typed), true
		case []byte:
			return typed, true
		default:
			return nil, false
		}
	}
	firstPassage, ok := passageBytes(firstRead["passage"])
	if !ok {
		t.Fatalf("first read passage has unexpected type: %T", firstRead["passage"])
	}
	secondPassage, ok := passageBytes(secondRead["passage"])
	if !ok {
		t.Fatalf("second read passage has unexpected type: %T", secondRead["passage"])
	}
	if !bytes.Contains(firstPassage, []byte("root-isolation-first-alpha")) ||
		bytes.Contains(firstPassage, []byte("root-isolation-second-beta")) ||
		!bytes.Contains(secondPassage, []byte("root-isolation-second-beta")) ||
		bytes.Contains(secondPassage, []byte("root-isolation-first-alpha")) {
		t.Fatalf("project knowledge bled across cached roots:\nfirst=%s\nsecond=%s",
			firstPassage, secondPassage)
	}
}

func TestAdversarialMCPLargeResultsUseCompactCompatibilityText(t *testing.T) {
	root := makeAdversarialProject(t)
	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		toolCallMessage(2, "orient", map[string]any{
			"role": "manager", "tokenBudget": 2048,
		}),
	)
	response := rpcResponseByID(t, messages, 2)
	structured := assertSuccessfulToolResult(t, response)
	result := asObject(t, response["result"])
	content := asArray(t, result["content"])
	text := asObject(t, content[0])["text"].(string)
	structuredJSON, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	if len(structuredJSON) < 1024 {
		t.Fatalf("fixture did not produce a meaningfully large structured payload: %d bytes",
			len(structuredJSON))
	}
	if len(text) >= len(structuredJSON) || len(text) > 1024 {
		t.Fatalf("text compatibility block duplicates the large structured payload: text=%d structured=%d",
			len(text), len(structuredJSON))
	}
}

func TestAdversarialMCPRequiredContextAndAtomicMaterialization(t *testing.T) {
	root := makeAdversarialProject(t)
	relative := "active/fixture-campaign/subagents/mcp-pack-run/context-pack.json"
	if err := os.MkdirAll(
		filepath.Join(root, "active", "fixture-campaign", "subagents", "mcp-pack-run"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	arguments := map[string]any{
		"task":          "Explain the engine frame checksum.",
		"role":          "drafter",
		"allowedTiers":  []string{"truth"},
		"tokenBudget":   1024,
		"requiredPaths": []string{"docs/truth/engine.md"},
	}
	missing := map[string]any{}
	for key, value := range arguments {
		missing[key] = value
	}
	missing["requiredPaths"] = []string{"docs/truth/missing.md"}

	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		toolCallMessage(2, "context_pack", arguments),
		toolCallMessage(5, "context_pack", missing),
	)
	pack := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	expectedDigest, digestOK := pack["digest"].(string)
	expectedPackID, idOK := pack["packId"].(string)
	if !digestOK || !idOK {
		t.Fatalf("context pack omitted manager-verifiable identity: %#v", pack)
	}
	passages := asArray(t, pack["passages"])
	foundRequired := false
	for _, value := range passages {
		passage := asObject(t, value)
		citation := asObject(t, passage["citation"])
		if citation["path"] == "docs/truth/engine.md" {
			foundRequired = true
		}
	}
	if !foundRequired {
		t.Fatal("required context source was omitted from the pack")
	}
	assertToolError(t, rpcResponseByID(t, messages, 5), "required")

	materialize := map[string]any{}
	for key, value := range arguments {
		materialize[key] = value
	}
	materialize["path"] = relative
	materialize["expectedDigest"] = expectedDigest
	materialize["expectedPackId"] = expectedPackID
	invalidTarget := map[string]any{}
	for key, value := range materialize {
		invalidTarget[key] = value
	}
	invalidTarget["path"] = "docs/context-pack.json"
	missingExpected := map[string]any{}
	for key, value := range materialize {
		if key != "expectedDigest" && key != "expectedPackId" {
			missingExpected[key] = value
		}
	}
	wrongExpected := map[string]any{}
	for key, value := range materialize {
		wrongExpected[key] = value
	}
	wrongExpected["expectedDigest"] = "sha256:" + strings.Repeat("0", 64)
	wrongExpected["expectedPackId"] = "context-" + strings.Repeat("0", 20)

	materializeMessages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(10, false),
		toolCallMessage(3, "context_pack_materialize", materialize),
		toolCallMessage(4, "context_pack_materialize", materialize),
		toolCallMessage(6, "context_pack_materialize", invalidTarget),
		toolCallMessage(7, "context_pack_materialize", missingExpected),
		toolCallMessage(8, "context_pack_materialize", wrongExpected),
	)
	first := assertSuccessfulToolResult(t, rpcResponseByID(t, materializeMessages, 3))
	second := assertSuccessfulToolResult(t, rpcResponseByID(t, materializeMessages, 4))
	if first["path"] != relative || first["materialized"] != true ||
		first["digest"] != second["digest"] || first["packId"] != second["packId"] {
		t.Fatalf("materialization was not exact and idempotent: first=%#v second=%#v",
			first, second)
	}
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if _, err := VerifyContextPack(absolute); err != nil {
		t.Fatalf("MCP materialized an invalid context pack: %v", err)
	}
	assertToolError(t, rpcResponseByID(t, materializeMessages, 6), "managed drafter")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 7), "expected digest")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 8), "expected")
	if _, err := os.Stat(filepath.Join(root, "docs", "context-pack.json")); !os.IsNotExist(err) {
		t.Fatal("invalid materialization left a partial artifact")
	}
}

func TestAdversarialMalformedConfigurationHasReadOnlyStatusAndFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		body     string
	}{
		{
			name: "bootstrap config",
			relative: filepath.ToSlash(filepath.Join(
				".re-discipline", "config.json")),
			body: `{"schemaVersion":`,
		},
		{
			name: "knowledge settings",
			relative: filepath.ToSlash(filepath.Join(
				".re-discipline", "settings", "knowledge.jsonc")),
			body: `{"schemaVersion":1,"sources":`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			root := makeAdversarialProject(t)
			valid := newAdversarialService(t, root, nil)
			pack, err := valid.ContextPack(
				context.Background(), "engine frame checksum", "drafter",
				[]string{"truth"}, 1024,
			)
			if err != nil {
				t.Fatal(err)
			}
			relativePack := "active/fixture-campaign/subagents/invalid-config/context-pack.json"
			if err := os.MkdirAll(filepath.Join(
				root, "active", "fixture-campaign", "subagents", "invalid-config"),
				0o700); err != nil {
				t.Fatal(err)
			}
			controlPath := filepath.Join(root, filepath.FromSlash(testCase.relative))
			writeTestFile(t, controlPath, testCase.body)
			before := snapshotRecoveryTree(t, root)

			service, err := NewService(ServiceOptions{
				ProjectRoot: root, AssetRoot: adversarialAssetRoot(t),
				CacheRoot: filepath.Join(
					root, ".re-discipline", "cache", "knowledge"),
			})
			if err != nil {
				t.Fatalf("malformed ordinary configuration blocked diagnostic service: %v", err)
			}
			status, err := service.Status(context.Background())
			if err != nil {
				t.Fatalf("read-only status failed for malformed configuration: %v", err)
			}
			configuration, ok := status["configuration"].(map[string]any)
			if !ok || configuration["valid"] != false {
				t.Fatalf("status did not report invalid configuration: %#v", status)
			}
			errorsValue, ok := configuration["errors"].([]string)
			if !ok || len(errorsValue) == 0 {
				t.Fatalf("status omitted invalid-configuration diagnostics: %#v", configuration)
			}

			if _, err := service.Search(context.Background(), SearchOptions{
				Query: "A1B2C3D4", QueryClass: "exact",
				AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 1024,
			}); err == nil {
				t.Fatal("search did not fail closed for malformed configuration")
			}
			if _, err := service.Read(
				context.Background(), ReadOptions{Path: "docs/truth/engine.md"}); err == nil {
				t.Fatal("read did not fail closed for malformed configuration")
			}
			if _, err := service.ReconcileIndex(context.Background()); err == nil {
				t.Fatal("index reconciliation did not fail closed for malformed configuration")
			}
			if _, err := service.ContextPack(
				context.Background(), "engine frame checksum", "drafter",
				[]string{"truth"}, 1024); err == nil {
				t.Fatal("context-pack generation did not fail closed for malformed configuration")
			}
			if err := service.MaterializeContextPackExpected(
				relativePack, pack, pack.Digest, pack.PackID); err == nil {
				t.Fatal("materialization did not fail closed for malformed configuration")
			}

			messages := runMCPMessages(
				t,
				&MCPServer{AssetRoot: adversarialAssetRoot(t)},
				initializeMessage(1, false),
				toolCallMessage(2, "status", map[string]any{"projectRoot": root}),
				toolCallMessage(3, "search", map[string]any{
					"query": "A1B2C3D4", "queryClass": "exact",
					"allowedTiers": []string{"truth"}, "limit": 5,
					"tokenBudget": 1024, "projectRoot": root,
				}),
			)
			mcpStatus := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
			mcpConfiguration := asObject(t, mcpStatus["configuration"])
			if mcpConfiguration["valid"] != false {
				t.Fatalf("MCP status did not preserve invalid diagnostics: %#v", mcpStatus)
			}
			assertToolError(t, rpcResponseByID(t, messages, 3), "configuration")

			after := snapshotRecoveryTree(t, root)
			if stableJSON(before) != stableJSON(after) {
				t.Fatal("diagnostic status or failed-closed operations mutated project state")
			}
			body, err := os.ReadFile(controlPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != testCase.body {
				t.Fatal("status or recovery rewrote malformed user configuration")
			}
			if _, err := os.Stat(filepath.Join(
				root, filepath.FromSlash(relativePack))); !os.IsNotExist(err) {
				t.Fatal("failed-closed materialization left a context-pack artifact")
			}
		})
	}
}

func TestAdversarialReplayAcrossEveryEffectiveFallback(t *testing.T) {
	root := makeAdversarialProject(t)
	ctx := context.Background()
	type capability struct {
		name          string
		disableDense  bool
		disableRerank bool
		effective     string
		fallback      bool
	}
	capabilities := []capability{
		{name: "full", effective: "hybrid-local-v1"},
		{name: "no-rerank", disableRerank: true, effective: "hybrid-no-rerank-v1", fallback: true},
		{name: "model-free", disableDense: true, disableRerank: true, effective: "lexical-graph-v1", fallback: true},
	}
	effectiveIdentities := map[string]bool{}
	var requestedIdentity string
	for _, capability := range capabilities {
		t.Run(capability.name, func(t *testing.T) {
			service := newAdversarialService(t, root, func(options *ServiceOptions) {
				options.DisableDense = capability.disableDense
				options.DisableRerank = capability.disableRerank
			})
			options := SearchOptions{
				Query: "engine frame serialization checksum", QueryClass: "conceptual",
				AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 1024,
				// Requested-profile identity is verbose provenance; a compact
				// response reports only what actually served.
				Verbosity: VerbosityVerbose,
			}
			first, err := service.Search(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			second, err := service.Search(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			if stableJSON(first) != stableJSON(second) ||
				first.Metadata.DeterministicReplay != second.Metadata.DeterministicReplay {
				t.Fatal("search replay differs")
			}
			if !strings.HasPrefix(first.Metadata.EffectiveProfile, capability.effective+"@") {
				t.Fatalf("effective profile = %s, want %s", first.Metadata.EffectiveProfile, capability.effective)
			}
			if (first.Metadata.FallbackReason != nil) != capability.fallback {
				t.Fatalf("fallback reason mismatch: %#v", first.Metadata)
			}
			if requestedIdentity == "" {
				requestedIdentity = first.Metadata.RequestedProfile
			} else if first.Metadata.RequestedProfile != requestedIdentity {
				t.Fatal("capability fallback changed requested profile identity")
			}
			effectiveIdentities[first.Metadata.EffectiveProfile] = true

			pack, err := service.ContextPack(
				ctx, "engine frame serialization checksum", "drafter", []string{"truth"}, 1024,
			)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := service.ContextPack(
				ctx, "engine frame serialization checksum", "drafter", []string{"truth"}, 1024,
			)
			if err != nil {
				t.Fatal(err)
			}
			if stableJSON(pack) != stableJSON(replay) ||
				pack.RequestedProfile != first.Metadata.RequestedProfile ||
				pack.EffectiveProfile != first.Metadata.EffectiveProfile {
				t.Fatal("context-pack replay or profile metadata differs")
			}
			body, err := json.Marshal(pack)
			if err != nil {
				t.Fatal(err)
			}
			if pack.EstimatedTokens != EstimateTokens(string(body)) ||
				pack.EstimatedTokens > pack.TokenBudget {
				t.Fatalf("context-pack token accounting is not exact: reported=%d actual=%d budget=%d",
					pack.EstimatedTokens, EstimateTokens(string(body)), pack.TokenBudget)
			}
		})
	}
	if len(effectiveIdentities) != 3 {
		t.Fatalf("fallback capability identities collapsed: %v", effectiveIdentities)
	}
}

func TestAdversarialUnapprovedProjectProfileCannotSelfPromote(t *testing.T) {
	root := makeAdversarialProject(t)
	profilePath := filepath.Join(root, ".re-discipline", "settings", "retrieval-profile.json")
	body, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile RetrievalProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
	profile.ProfileID = "project:self-promoted"
	profile.BaseProfile = "plugin:balanced-v1"
	profile.EffectiveProfiles[0].Weights["exact"]++
	profile.Approval = nil
	writeTestJSON(t, profilePath, profile)

	service := newAdversarialService(t, root, nil)
	if service.ProfileCatalog.ProfileID != "plugin:balanced-v1" {
		t.Fatalf("unapproved profile became active: %s", service.ProfileCatalog.ProfileID)
	}
	foundWarning := false
	for _, warning := range service.Warnings {
		if strings.Contains(warning, "unratified") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("unapproved project profile fallback was silent: %v", service.Warnings)
	}
}

func TestAdversarialPackagedWindowsBinaryMCPProtocol(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows packaged-binary protocol test")
	}
	assetRoot := adversarialAssetRoot(t)
	binary := filepath.Join(assetRoot, "bin", "re-discipline-knowledge.exe")
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		t.Skip("packaged Windows binary has not been produced")
	} else if err != nil {
		t.Fatal(err)
	}
	root := makeAdversarialProject(t)
	inputMessages := []any{
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(3, "status", map[string]any{"projectRoot": root}),
	}
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for _, message := range inputMessages {
		if err := encoder.Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	command := exec.Command(
		binary, "serve", "--asset-root", assetRoot,
	)
	command.Dir = filepath.Dir(assetRoot)
	command.Stdin = &input
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	command.Env = append(os.Environ(),
		"CODEX_HOME="+filepath.Join(state, "codex"),
		"CLAUDE_CONFIG_DIR="+filepath.Join(state, "claude"),
		"LOCALAPPDATA="+filepath.Join(state, "localappdata"),
		"XDG_CACHE_HOME="+filepath.Join(state, "xdg-cache"),
		"CLAUDE_PROJECT_DIR=",
	)
	if err := command.Run(); err != nil {
		t.Fatalf("packaged binary protocol failed: %v\nstderr:\n%s", err, stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	messages := []map[string]any{}
	for {
		var message map[string]any
		err := decoder.Decode(&message)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("packaged binary emitted invalid protocol: %v\nstdout:\n%s\nstderr:\n%s",
				err, stdout.String(), stderr.String())
		}
		messages = append(messages, message)
	}
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	tools := asObject(t, rpcResponseByID(t, messages, 2)["result"])
	if len(asArray(t, tools["tools"])) != 7 {
		t.Fatal("packaged binary exposed an incomplete MCP surface")
	}
}
