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
	"sort"
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

// resolveSchemaRef follows a "#/$defs/<name>" pointer into the enclosing tool
// schema's own $defs block, the way a schema consumer does. A node that is not
// a reference is returned unchanged, so callers can traverse a schema without
// caring whether a given record happens to be inlined or hoisted.
func resolveSchemaRef(t *testing.T, root map[string]any, value any) map[string]any {
	t.Helper()
	node := asObject(t, value)
	reference, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	const prefix = "#/$defs/"
	if !strings.HasPrefix(reference, prefix) {
		t.Fatalf("schema reference %q is not a same-document $defs pointer", reference)
	}
	definitions := asObject(t, root["$defs"])
	resolved, present := definitions[strings.TrimPrefix(reference, prefix)]
	if !present {
		t.Fatalf("schema reference %q does not resolve; $defs has %v", reference, definitions)
	}
	return asObject(t, resolved)
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

// toolSchemaBudgets caps each tool's share of the discovery payload.
//
// The 64 KiB total in TestAdversarialMCPToolSchemasAndAuthoritySurface is a
// portability ceiling, not a cost control: one tool can quietly grow to 40% of
// discovery and the total still passes. manager_apply did exactly that -- it
// inlines the record schemas of fourteen actions, and every caller pays for all
// of them before doing any work, whether it is opening a campaign or retiring a
// coverage span. A per-tool ceiling makes that growth a deliberate edit: adding
// a field to a record now either fits the budget or forces whoever added it to
// raise a number and say why in the commit.
//
// Each ceiling sits just above where its tool actually lands, so the headroom
// is for a field or two, not for another record type. Raising one is a normal
// thing to do -- silently drifting past it is not.
//
// Raised for the four mutating tools when `tokenBudget` was added to them. The
// number bought a documented affordance rather than a field: the description
// has to say what a budget may drop and promise that nothing is truncated,
// because a caller cannot decide whether it can afford a short receipt without
// knowing which sections are at risk, and discovering that by tripping a
// refusal is the exact cost this parameter exists to remove. curation_submit
// and closure_apply needed the raise; manager_apply and normalization_queue
// absorbed it inside their existing headroom.
var toolSchemaBudgets = map[string]int{
	"state":                    1350,
	"read":                     1600,
	"trace":                    1700,
	"query":                    2950,
	"normalization_queue":      3550,
	"closure_apply":            3750,
	"context_pack_materialize": 3750,
	"migrate_project":          8800,
	"curation_submit":          9750,
	"manager_apply":            23600,
}

func TestMCPToolDiscoveryStaysWithinPerToolBudgets(t *testing.T) {
	definitions := toolDefinitions()
	measured := map[string]int{}
	total := 0
	for _, definition := range definitions {
		name, _ := definition["name"].(string)
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		measured[name] = len(encoded)
		total += len(encoded)
	}
	// Log every tool every run, largest first, so the number a maintainer needs
	// in order to judge a schema change is visible without instrumenting
	// anything.
	names := make([]string, 0, len(measured))
	for name := range measured {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return measured[names[i]] > measured[names[j]] })
	for _, name := range names {
		budget, present := toolSchemaBudgets[name]
		if !present {
			t.Errorf("tool %s has no schema budget; add one to toolSchemaBudgets", name)
			continue
		}
		t.Logf("%-24s %6d bytes  (budget %6d, %3d%% of discovery)",
			name, measured[name], budget, measured[name]*100/total)
		if measured[name] > budget {
			t.Errorf(
				"%s schema is %d bytes, over its %d-byte budget. Every caller pays this "+
					"before doing any work. Shrink it -- hoist repeated record schemas into "+
					"$defs -- or raise the budget deliberately and say why.",
				name, measured[name], budget)
		}
	}
	for name := range toolSchemaBudgets {
		if _, present := measured[name]; !present {
			t.Errorf("toolSchemaBudgets caps %s, which is no longer a tool", name)
		}
	}
	t.Logf("discovery payload total: %d bytes", total)
}

// TestMCPToolSchemaReferencesResolve guards the de-duplication that keeps
// manager_apply inside its budget. A $ref that does not resolve is worse than
// the duplication it replaced: the caller cannot discover the record shape at
// all, and nothing else in the suite would notice.
func TestMCPToolSchemaReferencesResolve(t *testing.T) {
	for _, definition := range toolDefinitions() {
		name, _ := definition["name"].(string)
		encoded, err := json.Marshal(definition["inputSchema"])
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err := json.Unmarshal(encoded, &root); err != nil {
			t.Fatal(err)
		}
		definitions, _ := root["$defs"].(map[string]any)
		used := map[string]bool{}
		var walk func(node any)
		walk = func(node any) {
			switch typed := node.(type) {
			case map[string]any:
				if reference, ok := typed["$ref"].(string); ok {
					const prefix = "#/$defs/"
					if !strings.HasPrefix(reference, prefix) {
						t.Errorf("%s uses non-portable schema reference %q", name, reference)
						return
					}
					key := strings.TrimPrefix(reference, prefix)
					if _, present := definitions[key]; !present {
						t.Errorf("%s references %q but $defs defines %v", name, reference, definitions)
						return
					}
					used[key] = true
					return
				}
				for _, item := range typed {
					walk(item)
				}
			case []any:
				for _, item := range typed {
					walk(item)
				}
			}
		}
		walk(root)
		for key := range definitions {
			if !used[key] {
				t.Errorf("%s defines $defs/%s but never references it", name, key)
			}
		}
	}
}

func TestAdversarialMCPToolSchemasAndAuthoritySurface(t *testing.T) {
	definitions := toolDefinitions()
	encodedDefinitions, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(encodedDefinitions) > 64*1024 {
		t.Fatalf("tool discovery payload is %d bytes, exceeding the 64 KiB portability ceiling", len(encodedDefinitions))
	}
	t.Logf("tool discovery payload: %d bytes", len(encodedDefinitions))
	expected := []string{
		"state", "query", "read", "trace", "context_pack_materialize",
		"manager_apply", "curation_submit", "closure_apply", "normalization_queue", "migrate_project",
	}
	if len(definitions) != len(expected) {
		t.Fatalf("tool count = %d, want %d", len(definitions), len(expected))
	}
	for index, definition := range definitions {
		name, _ := definition["name"].(string)
		if name != expected[index] {
			t.Fatalf("tool %d = %q, want %q", index, name, expected[index])
		}
		if definition["title"] == "" || definition["description"] == "" {
			t.Fatalf("%s omitted title or description: %#v", name, definition)
		}
		schema := asObject(t, definition["inputSchema"])
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatalf("%s input schema is not closed: %#v", name, schema)
		}
		for _, opaque := range []string{"oneOf", "anyOf", "allOf"} {
			if _, exists := schema[opaque]; exists {
				t.Fatalf("%s uses non-portable top-level %s: %#v", name, opaque, schema)
			}
		}
		annotations := asObject(t, definition["annotations"])
		if name == "context_pack_materialize" || name == "manager_apply" ||
			name == "curation_submit" || name == "closure_apply" || name == "normalization_queue" ||
			name == "migrate_project" {
			if annotations["readOnlyHint"] != false ||
				annotations["idempotentHint"] != true ||
				annotations["openWorldHint"] != false {
				t.Fatalf("%s write annotations are unsafe: %#v", name, annotations)
			}
			wantDestructive := name == "closure_apply" || name == "migrate_project"
			if annotations["destructiveHint"] != wantDestructive {
				t.Fatalf("%s destructive hint = %#v, want %t", name, annotations["destructiveHint"], wantDestructive)
			}
		} else if annotations["readOnlyHint"] != true ||
			annotations["destructiveHint"] != false ||
			annotations["idempotentHint"] != true ||
			annotations["openWorldHint"] != false {
			t.Fatalf("%s read-only annotations are incomplete: %#v", name, annotations)
		}
		properties := asObject(t, schema["properties"])
		if name == "state" {
			budget := asObject(t, properties["tokenBudget"])
			if budget["minimum"] != float64(128) && budget["minimum"] != 128 {
				t.Fatalf("state schema permits a budget rejected by execution: %#v", budget)
			}
		}
		if name == "query" {
			leaseID := asObject(t, properties["contextLeaseId"])
			if leaseID["type"] != "string" ||
				leaseID["minLength"] != 1 && leaseID["minLength"] != float64(1) ||
				leaseID["maxLength"] != 128 && leaseID["maxLength"] != float64(128) ||
				leaseID["pattern"] != contextLeaseIDRE.String() {
				t.Fatalf("query contextLeaseId schema is not safely bounded: %#v", leaseID)
			}
			resetLease := asObject(t, properties["resetContextLease"])
			if resetLease["type"] != "boolean" {
				t.Fatalf("query resetContextLease schema is not boolean: %#v", resetLease)
			}
			findingClasses := fmt.Sprint(asObject(t, asObject(t, properties["allowedSourceClasses"])["items"])["enum"])
			provenanceTiers := fmt.Sprint(asObject(t, asObject(t, properties["allowedProvenanceTiers"])["items"])["enum"])
			if strings.Contains(findingClasses, "backlog") ||
				!strings.Contains(provenanceTiers, "backlog") || !strings.Contains(provenanceTiers, "navigation") {
				t.Fatalf("query finding classes and provenance tiers are not independent: finding=%s provenance=%s",
					findingClasses, provenanceTiers)
			}
		}
		if name == "read" {
			required := map[string]bool{}
			for _, value := range schema["required"].([]string) {
				required[value] = true
			}
			if !required["selector"] || !required["value"] {
				t.Fatalf("read schema does not require selector and value: %#v", schema["required"])
			}
		}
		if name == "context_pack_materialize" {
			required := map[string]bool{}
			for _, value := range schema["required"].([]string) {
				required[value] = true
			}
			for _, field := range []string{"action", "target", "task", "role"} {
				if !required[field] {
					t.Fatalf("context-pack schema does not require %s", field)
				}
			}
			action := asObject(t, properties["action"])
			if fmt.Sprint(action["enum"]) != "[preview materialize]" {
				t.Fatalf("context-pack action discriminator is incomplete: %#v", action)
			}
			target := asObject(t, properties["target"])
			targetProperties := asObject(t, target["properties"])
			kind := asObject(t, targetProperties["kind"])
			if target["additionalProperties"] != false ||
				fmt.Sprint(kind["enum"]) != "[active-run recruiting-run]" {
				t.Fatalf("context-pack target schema is not closed and scoped: %#v", target)
			}
			for _, retired := range []string{"path", "verbosity"} {
				if _, present := properties[retired]; present {
					t.Fatalf("context-pack schema exposes retired caller field %q", retired)
				}
			}
			requiredPaths := asObject(t, properties["requiredPaths"])
			if requiredPaths["type"] != "array" ||
				requiredPaths["uniqueItems"] != true ||
				requiredPaths["maxItems"] != 20 &&
					requiredPaths["maxItems"] != float64(20) {
				t.Fatalf("%s requiredPaths schema is not safely bounded: %#v",
					name, requiredPaths)
			}
		}
		if name == "migrate_project" {
			coverage := asObject(t, properties["coverage"])
			findings := asObject(t, asObject(t, coverage["properties"])["findings"])
			finding := asObject(t, findings["items"])
			findingProperties := asObject(t, finding["properties"])
			attestation := asObject(t, findingProperties["curatorAttestation"])
			if attestation["additionalProperties"] != false {
				t.Fatalf("migration curator attestation schema is open: %#v", attestation)
			}
			required := map[string]bool{}
			for _, value := range attestation["required"].([]string) {
				required[value] = true
			}
			for _, field := range []string{
				"singleIndependentlyOverturnableClaim", "evidenceGradeAppliesToEntireClaim",
				"entireSourceSpanRepresented", "semanticBoundariesVerified",
				"legacyReviewLanguageProvenanceOnly",
				"managerAttentionRequired", "rationale",
			} {
				if !required[field] {
					t.Fatalf("migration curator attestation schema does not require %s: %#v", field, attestation)
				}
			}
		}
		if name == "curation_submit" {
			if _, present := properties["workItems"]; present {
				t.Fatalf("curation schema exposes manager-owned work-item writes")
			}
			candidates := asObject(t, properties["candidates"])
			candidate := asObject(t, candidates["items"])
			candidateProperties := asObject(t, candidate["properties"])
			candidateRequired := map[string]bool{}
			for _, field := range candidate["required"].([]string) {
				candidateRequired[field] = true
			}
			for _, field := range []string{"record", "body", "path", "syntheticQuestions", "questionsReviewed"} {
				if _, present := candidateProperties[field]; !present || !candidateRequired[field] {
					t.Fatalf("curation candidate schema cannot transport canonical %s", field)
				}
			}
			curatorRun := asObject(t, properties["curatorRun"])
			curatorRunDescription, _ := curatorRun["description"].(string)
			if !strings.Contains(curatorRunDescription, "exact copy") ||
				!strings.Contains(curatorRunDescription, "never mutated") {
				t.Fatalf("curation curatorRun schema does not advertise its proof-only boundary: %q", curatorRunDescription)
			}
			description, _ := definition["description"].(string)
			if !strings.Contains(description, "proposals only") ||
				!strings.Contains(description, "manager review") {
				t.Fatalf("curation description does not explain proposal-only work-item authority: %q", description)
			}
		}
		if name == "normalization_queue" {
			action := asObject(t, properties["action"])
			if fmt.Sprint(action["enum"]) != "[status request claim ack resolve]" {
				t.Fatalf("normalization action schema omits a demand or lifecycle trigger: %#v", action)
			}
			if _, present := properties["rationale"]; present {
				t.Fatal("normalization MCP exposes free-text rationale as resolution authority")
			}
			resolution := asObject(t, properties["resolution"])
			if resolution["type"] != "object" || resolution["additionalProperties"] != false {
				t.Fatalf("normalization resolution proof is not a closed object: %#v", resolution)
			}
			resolutionProperties := asObject(t, resolution["properties"])
			for _, field := range []string{
				"sourceReport", "curatorRunId", "curatorRunDigest", "curatorReport",
				"intakeId", "intakeRevision", "intakeDigest", "coverageDigest",
				"reviewId", "reviewRevision", "reviewDigest", "resolvedFindingIds", "digest",
			} {
				if _, present := resolutionProperties[field]; !present {
					t.Fatalf("normalization resolution schema omits canonical proof field %s", field)
				}
			}
		}
	}
	for _, forbidden := range []string{
		"status", "orient", "search", "context_pack", "recall_propose",
		"accept_memory", "promote_truth", "activate_profile", "close_campaign",
	} {
		for _, definition := range definitions {
			if definition["name"] == forbidden {
				t.Fatalf("general MCP surface exposes manager-only operation %q", forbidden)
			}
		}
	}
}

func TestAdversarialMCPCurationRejectsManagerOwnedWorkItemField(t *testing.T) {
	var input curationSubmitToolInput
	err := decodeToolInput(json.RawMessage(`{"workItems":[]}`), &input)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("curation MCP accepted manager-owned workItems input: %v", err)
	}
}

func TestAdversarialMCPNormalizationRejectsProseAsAuthority(t *testing.T) {
	for name, body := range map[string]string{
		"unknown rationale": `{"action":"resolve","rationale":"looks normalized"}`,
		"string resolution": `{"action":"resolve","resolution":"looks normalized"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var input normalizationQueueToolInput
			err := decodeToolInput(json.RawMessage(body), &input)
			if err == nil {
				t.Fatal("normalization MCP accepted prose in place of canonical proof")
			}
		})
	}
}

func TestAdversarialMCPProjectBudgetsAreEnforcedNotMerelyAdvertised(t *testing.T) {
	root := makeAdversarialProject(t)
	writeCanonicalClosureGraph(t, root)
	settingsPath := filepath.Join(
		root, ".re-discipline", "knowledge", "policy.jsonc")
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
		&MCPServer{
			AssetRoot: adversarialAssetRoot(t), InitialRoot: root,
		},
		initializeMessage(1, false),
		map[string]any{
			"jsonrpc": "2.0", "method": "notifications/initialized",
		},
		toolCallMessage(2, "query", map[string]any{
			"query": "implementation complete", "campaignId": "C-TEST",
			"allowedSourceClasses": []string{"campaign"},
			"allowedReviewStates":  []string{"manager-ratified"},
			"allowedValidities":    []string{"current"},
			"limit":                3, "tokenBudget": 1024,
		}),
		toolCallMessage(4, "context_pack_materialize", map[string]any{
			"action": "preview",
			"target": map[string]any{
				"kind": "recruiting-run", "candidateSlug": "candidate-one",
				"recruitingRunId": "20260802T190000Z",
			},
			"task": "engine serialization", "role": "drafter",
			"allowedTiers": []string{"truth"}, "tokenBudget": 2048,
		}),
	)
	query := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	if query["tokenBudget"] != float64(512) && query["tokenBudget"] != 512 {
		t.Fatalf(
			"explicit MCP query bypassed configured searchTokens ceiling: %#v",
			query["tokenBudget"])
	}
	if len(asArray(t, query["cards"])) > 3 {
		t.Fatalf("explicit MCP query bypassed its card limit: %#v", query["cards"])
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

func TestMCPV8RepresentativeRequestsAndStructuredResults(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	root := makeAdversarialProject(t)
	writeCanonicalClosureGraph(t, root)
	messages := runMCPMessages(
		t,
		&MCPServer{
			AssetRoot: adversarialAssetRoot(t), InitialRoot: root,
		},
		initializeMessage(1, false),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(3, "state", map[string]any{
			"mode": "resume", "campaignId": "C-TEST", "tokenBudget": 1800,
		}),
		toolCallMessage(4, "query", map[string]any{
			"query": "implementation complete", "campaignId": "C-TEST",
			"allowedSourceClasses": []string{"campaign"},
			"allowedReviewStates":  []string{"manager-ratified"},
			"allowedValidities":    []string{"current"},
			"limit":                3, "tokenBudget": 1200, "requestId": "mcp-v8-query",
		}),
		toolCallMessage(5, "read", map[string]any{
			"selector": "finding", "value": "finding:F-0001",
			"campaignId": "C-TEST", "tokenBudget": 1600,
		}),
		toolCallMessage(6, "trace", map[string]any{
			"campaignId": "C-TEST", "startHandle": "finding:F-0001",
			"depth": 2, "maxNodes": 12, "tokenBudget": 1800,
		}),
		toolCallMessage(7, "context_pack_materialize", map[string]any{
			"action": "preview", "task": "Review the implementation status",
			"target": map[string]any{
				"kind": "recruiting-run", "candidateSlug": "candidate-one",
				"recruitingRunId": "20260802T190000Z",
			},
			"role": "manager", "tokenBudget": 1024,
		}),
		toolCallMessage(8, "closure_apply", map[string]any{
			"action": "status", "campaignId": "C-TEST",
		}),
		toolCallMessage(9, "manager_apply", map[string]any{
			"action": "work.update",
		}),
		toolCallMessage(10, "curation_submit", map[string]any{}),
		toolCallMessage(11, "status", map[string]any{}),
		toolCallMessage(12, "state", map[string]any{
			"mode": "orient", "unexpected": true,
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
	tools := asArray(t, toolsResult["tools"])
	if len(tools) != 10 {
		t.Fatalf("tools/list returned %d tools, want 10", len(tools))
	}
	state := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	if state["mode"] != "resume" || state["campaignId"] != "C-TEST" || state["digest"] == "" {
		t.Fatalf("state adapter result is incomplete: %#v", state)
	}
	query := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 4))
	if len(asArray(t, query["cards"])) == 0 || query["digest"] == "" {
		t.Fatalf("query adapter omitted normalized cards or digest: %#v", query)
	}
	read := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 5))
	if read["recordId"] != "F-0001" || read["handle"] != "finding:F-0001" || read["digest"] == "" {
		t.Fatalf("read adapter result is incomplete: %#v", read)
	}
	trace := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 6))
	if len(asArray(t, trace["nodes"])) < 2 || trace["digest"] == "" {
		t.Fatalf("trace adapter omitted graph context or digest: %#v", trace)
	}
	pack := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 7))
	scope := asObject(t, pack["scope"])
	if pack["packId"] == "" || pack["digest"] == "" ||
		scope["kind"] != "recruiting-run" ||
		scope["candidateSlug"] != "candidate-one" ||
		len(asArray(t, pack["cards"])) == 0 {
		t.Fatalf("context-pack preview omitted immutable identity: %#v", pack)
	}
	closure := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 8))
	if closure["action"] != "status" || closure["digest"] == "" || closure["state"] == nil {
		t.Fatalf("closure status adapter result is incomplete: %#v", closure)
	}
	assertToolError(t, rpcResponseByID(t, messages, 9), "manager_apply requires actor")
	assertToolError(t, rpcResponseByID(t, messages, 10), "unsupported schema version")
	assertToolError(t, rpcResponseByID(t, messages, 11), `unknown tool "status"`)
	assertToolError(t, rpcResponseByID(t, messages, 12), "unknown field")
}

func TestAdversarialMCPRequiresInitialization(t *testing.T) {
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
	messages := runMCPMessages(
		t,
		server,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(2, "state", map[string]any{"mode": "orient"}),
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
		toolCallMessage(2, "state", map[string]any{"mode": "orient", "projectRoot": root}),
		toolCallMessage(3, "state", map[string]any{"mode": "orient"}),
		toolCallMessage(4, "state", map[string]any{"mode": "orient", "projectRoot": other}),
	)
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	assertToolError(t, rpcResponseByID(t, messages, 4), "session shard")
}

func TestAdversarialMCPExplicitStateDoesNotRecoverAnUngrantedRoot(t *testing.T) {
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
		toolCallMessage(2, "state", map[string]any{"mode": "orient", "projectRoot": root}),
	)
	state := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	if state["status"] != "attention" {
		t.Fatalf("diagnostic state concealed missing configuration: %#v", state)
	}
	cards := asArray(t, state["cards"])
	if len(cards) == 0 || !strings.Contains(fmt.Sprint(asObject(t, cards[0])["claim"]), ".re-discipline/config.json") {
		t.Fatalf("diagnostic state omitted configuration failure: %#v", state)
	}
	after := snapshotRecoveryTree(t, root)
	if stableJSON(before) != stableJSON(after) {
		t.Fatal("explicit state read recovered or otherwise mutated an ungranted project root")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("explicit state read recreated missing bootstrap configuration")
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
		[]byte("<!-- re-discipline:shared-laws v0.8.0 -->"),
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
		toolCallMessage(2, "state", map[string]any{"mode": "orient", "projectRoot": root}),
	)
	assertToolError(t, rpcResponseByID(t, messages, 2), "runtime-supported re-discipline shared-laws marker")
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
			toolCallMessage(2, "state", map[string]any{"mode": "orient"}),
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
			toolCallMessage(2, "state", map[string]any{"mode": "orient"}),
			toolCallMessage(3, "state", map[string]any{"mode": "orient", "projectRoot": second}),
			toolCallMessage(4, "state", map[string]any{"mode": "orient", "projectRoot": third}),
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
			toolCallMessage(2, "state", map[string]any{"mode": "orient"}),
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
	writeCanonicalClosureGraph(t, root)
	messages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(1, false),
		toolCallMessage(2, "state", map[string]any{
			"mode": "work", "campaignId": "C-TEST", "workItemId": "W-0001",
			"tokenBudget": 2200,
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
	arguments := map[string]any{
		"action": "preview",
		"target": map[string]any{
			"kind": "recruiting-run", "candidateSlug": "candidate-one",
			"recruitingRunId": "20260802T190000Z",
		},
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
		toolCallMessage(2, "context_pack_materialize", arguments),
		toolCallMessage(5, "context_pack_materialize", missing),
	)
	pack := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
	expectedDigest, digestOK := pack["digest"].(string)
	expectedPackID, idOK := pack["packId"].(string)
	if !digestOK || !idOK {
		t.Fatalf("context pack omitted manager-verifiable identity: %#v", pack)
	}
	if _, legacy := pack["passages"]; legacy {
		t.Fatal("scoped context pack leaked the retired whole-passage payload")
	}
	cards := asArray(t, pack["cards"])
	foundRequired := false
	for _, value := range cards {
		card := asObject(t, value)
		metadata := asObject(t, card["metadata"])
		if metadata["path"] == "docs/truth/engine.md" && card["handle"] != "" {
			foundRequired = true
		}
	}
	if !foundRequired {
		t.Fatal("required context source was omitted from the pack")
	}
	if len(asArray(t, pack["requiredHandles"])) == 0 {
		t.Fatal("required context source omitted its exact expansion handle")
	}
	assertToolError(t, rpcResponseByID(t, messages, 5), "required")

	materialize := map[string]any{}
	for key, value := range arguments {
		materialize[key] = value
	}
	materialize["action"] = "materialize"
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
	previewWithWriteFields := map[string]any{}
	for key, value := range materialize {
		previewWithWriteFields[key] = value
	}
	previewWithWriteFields["action"] = "preview"
	activeMaterialize := map[string]any{}
	for key, value := range materialize {
		activeMaterialize[key] = value
	}
	activeMaterialize["target"] = map[string]any{
		"kind": "active-run", "campaignId": "C-TEST",
		"workItemId": "W-0001", "runId": "R-20260802-0001",
	}
	if err := os.MkdirAll(filepath.Join(
		root, ".re-discipline", "agents", "recruiting", "candidate-one",
		"runs", "20260802T190000Z",
	), 0o700); err != nil {
		t.Fatal(err)
	}

	materializeMessages := runMCPMessages(
		t,
		&MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initializeMessage(10, false),
		toolCallMessage(3, "context_pack_materialize", materialize),
		toolCallMessage(4, "context_pack_materialize", materialize),
		toolCallMessage(6, "context_pack_materialize", invalidTarget),
		toolCallMessage(7, "context_pack_materialize", missingExpected),
		toolCallMessage(8, "context_pack_materialize", wrongExpected),
		toolCallMessage(9, "context_pack_materialize", previewWithWriteFields),
		toolCallMessage(11, "context_pack_materialize", activeMaterialize),
	)
	materializedPath := ".re-discipline/agents/recruiting/candidate-one/runs/" +
		"20260802T190000Z/context-pack.json"
	for _, id := range []int{3, 4} {
		result := assertSuccessfulToolResult(t, rpcResponseByID(t, materializeMessages, id))
		if result["path"] != materializedPath || result["materialized"] != true ||
			result["digest"] != expectedDigest || result["packId"] != expectedPackID {
			t.Fatalf("materialization %d returned the wrong immutable identity: %#v", id, result)
		}
	}
	materialized, err := VerifyContextPack(filepath.Join(
		root, filepath.FromSlash(materializedPath),
	))
	if err != nil {
		t.Fatalf("materialized context pack failed verification: %v", err)
	}
	if materialized["digest"] != expectedDigest || materialized["packId"] != expectedPackID {
		t.Fatalf("materialized context pack identity changed: %#v", materialized)
	}
	assertToolError(t, rpcResponseByID(t, materializeMessages, 6), "unknown field")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 7), "requires expectedDigest")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 8), "expected")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 9),
		"does not accept expectedDigest or expectedPackId")
	assertToolError(t, rpcResponseByID(t, materializeMessages, 11), "manager_apply run.prepare")
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
				".re-discipline", "knowledge", "policy.jsonc")),
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
			relativePack := "active/fixture-campaign/runs/R-20260802-0099/context-pack.json"
			if err := os.MkdirAll(filepath.Join(
				root, "active", "fixture-campaign", "runs", "R-20260802-0099"),
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
			statusPayload, err := service.Status(context.Background())
			if err != nil {
				t.Fatalf("read-only status failed for malformed configuration: %v", err)
			}
			status, systemOK := statusPayload["system"].(map[string]any)
			if !systemOK {
				t.Fatalf("status omitted the system block: %#v", statusPayload)
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
				toolCallMessage(2, "state", map[string]any{
					"mode": "orient", "projectRoot": root,
				}),
				toolCallMessage(3, "query", map[string]any{
					"query": "A1B2C3D4", "limit": 5,
					"tokenBudget": 1024, "projectRoot": root,
				}),
			)
			mcpState := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 2))
			if mcpState["status"] != "attention" || len(asArray(t, mcpState["cards"])) == 0 {
				t.Fatalf("MCP state did not preserve invalid diagnostics: %#v", mcpState)
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
		name         string
		disableDense bool
		effective    string
		fallback     bool
	}
	capabilities := []capability{
		{name: "dense", effective: "hybrid-no-rerank-v1"},
		{name: "model-free", disableDense: true, effective: "lexical-graph-v1", fallback: true},
	}
	effectiveIdentities := map[string]bool{}
	var requestedIdentity string
	for _, capability := range capabilities {
		t.Run(capability.name, func(t *testing.T) {
			service := newAdversarialService(t, root, nil)
			if capability.disableDense {
				for _, model := range service.ModelManifest.Models {
					delete(service.ModelManifest.ExecutableModels, model.ID)
					service.ModelManifest.UnavailableModels[model.ID] = "test-unavailable"
				}
			}
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
	if len(effectiveIdentities) != 2 {
		t.Fatalf("shipped dense-to-lexical identities were unstable: %v", effectiveIdentities)
	}
}

func TestAdversarialUnapprovedProjectProfileCannotSelfPromote(t *testing.T) {
	root := makeAdversarialProject(t)
	profilePath := filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")
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
		toolCallMessage(3, "state", map[string]any{
			"mode": "orient", "projectRoot": root,
		}),
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
	initialize := asObject(t, rpcResponseByID(t, messages, 1)["result"])
	serverInfo := asObject(t, initialize["serverInfo"])
	if serverInfo["version"] != RuntimeVersion {
		t.Fatalf("packaged binary version = %#v, want %s", serverInfo["version"], RuntimeVersion)
	}
	stateResult := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	if stateResult["mode"] != "orient" || stateResult["digest"] == "" {
		t.Fatalf("packaged binary returned an incomplete state view: %#v", stateResult)
	}
	tools := asObject(t, rpcResponseByID(t, messages, 2)["result"])
	toolRows := asArray(t, tools["tools"])
	expected := []string{
		"state", "query", "read", "trace", "context_pack_materialize",
		"manager_apply", "curation_submit", "closure_apply", "normalization_queue", "migrate_project",
	}
	if len(toolRows) != len(expected) {
		t.Fatalf("packaged binary exposed %d tools, want %d", len(toolRows), len(expected))
	}
	for index, value := range toolRows {
		definition := asObject(t, value)
		if definition["name"] != expected[index] {
			t.Fatalf("packaged tool %d = %#v, want %q", index, definition["name"], expected[index])
		}
	}
}
