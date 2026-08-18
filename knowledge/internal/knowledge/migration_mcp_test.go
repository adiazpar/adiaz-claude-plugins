package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// callMigrationProjectTool exercises the MCP migration tool exactly as the
// server does, with the packaged knowledge asset root resolved from source.
func callMigrationProjectTool(
	root string,
	input migrationProjectToolInput,
) (any, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("runtime.Caller could not locate the test source")
	}
	assets := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return callMigrationProjectToolWithAssets(root, assets, input)
}

func TestMigrationMCPAcceptsLegacyRootAndMatchesSharedPreview(t *testing.T) {
	root := migrationPreviewFixture(t)
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root}
	messages := runMCPMessages(t, server,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "migration-test", "version": "1"},
		}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(3, "migrate_project", map[string]any{"action": "preview", "liveCampaigns": []string{"live-campaign"}}),
		toolCallMessage(4, "migrate_project", map[string]any{"action": "status"}),
	)
	tools := asArray(t, asObject(t, rpcResponseByID(t, messages, 2)["result"])["tools"])
	if asObject(t, tools[len(tools)-1])["name"] != "migrate_project" {
		t.Fatalf("migration tool was not discoverable on a legacy root: %#v", tools)
	}
	mcpPreview := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	plan := asObject(t, mcpPreview["plan"])
	direct, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if plan["planDigest"] != direct.Plan.PlanDigest || asObject(t, mcpPreview["receipt"])["equivalenceDigest"] != direct.Receipt.EquivalenceDigest {
		t.Fatalf("MCP preview diverged from the shared CLI engine: %#v", mcpPreview)
	}
	status := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 4))
	if status["state"] != "legacy" {
		t.Fatalf("MCP legacy status diverged from CLI semantics: %#v", status)
	}
}

func TestMigrationMCPServeAcceptsLegacyClaudeProjectDirectory(t *testing.T) {
	root := migrationPreviewFixture(t)
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	t.Setenv("CODEX_PROJECT_DIR", "")
	server := &MCPServer{AssetRoot: adversarialAssetRoot(t)}
	messages := runMCPMessages(t, server,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "legacy-environment-test", "version": "1"},
		}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		toolCallMessage(3, "migrate_project", map[string]any{"action": "preview", "liveCampaigns": []string{"live-campaign"}}),
	)
	tools := asArray(t, asObject(t, rpcResponseByID(t, messages, 2)["result"])["tools"])
	found := false
	for _, tool := range tools {
		found = found || asObject(t, tool)["name"] == "migrate_project"
	}
	if !found {
		t.Fatal("migrate_project was not exposed when CLAUDE_PROJECT_DIR selected a 0.7 project")
	}
	result := assertSuccessfulToolResult(t, rpcResponseByID(t, messages, 3))
	if asObject(t, result["plan"])["detectedVersion"] != "0.7" {
		t.Fatalf("legacy environment-root preview did not execute: %#v", result)
	}
}

func TestMigrationMCPExportsAndSubmitsReviewedTruthSplit(t *testing.T) {
	root := migrationPreviewFixture(t)
	first := strings.TrimSpace(strings.Repeat("first reviewed source span remains exact ", 15))
	second := strings.TrimSpace(strings.Repeat("second reviewed source span remains exact ", 15))
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# MCP compound truth\n\n**Claim:** "+first+" "+second+"\n")
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "truth-review-test", "version": "1"},
	}}
	firstMessages := runMCPMessages(t, &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initialize,
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		toolCallMessage(2, "migrate_project", map[string]any{"action": "truth-conflicts"}),
	)
	packet := assertSuccessfulToolResult(t, rpcResponseByID(t, firstMessages, 2))
	conflicts := asArray(t, packet["conflicts"])
	if len(conflicts) != 1 {
		t.Fatalf("MCP truth conflict export: %#v", packet)
	}
	conflict := asObject(t, conflicts[0])
	submission := map[string]any{
		"schemaVersion": 1, "packetDigest": packet["digest"],
		"sourcePath": conflict["sourcePath"], "sourceDigest": conflict["sourceDigest"],
		"reviewer": "manager", "rationale": "MCP-reviewed independent assertions",
		"claims": []any{
			map[string]any{"sourceText": first, "title": "First MCP assertion", "claim": "The first MCP assertion remains exact.",
				"syntheticQuestions": []string{"What first MCP assertion remains exact?", "Which first MCP assertion is accepted?", "How is the first MCP assertion treated?"}},
			map[string]any{"sourceText": second, "title": "Second MCP assertion", "claim": "The second MCP assertion remains exact.",
				"syntheticQuestions": []string{"What second MCP assertion remains exact?", "Which second MCP assertion is accepted?", "How is the second MCP assertion treated?"}},
		},
	}
	secondMessages := runMCPMessages(t, &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initialize,
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		toolCallMessage(3, "migrate_project", map[string]any{"action": "truth-review", "truthReview": submission}),
		toolCallMessage(4, "migrate_project", map[string]any{"action": "preview", "liveCampaigns": []string{"live-campaign"}}),
	)
	review := assertSuccessfulToolResult(t, rpcResponseByID(t, secondMessages, 3))
	if review["digest"] == "" {
		t.Fatalf("MCP truth review submission: %#v", review)
	}
	preview := assertSuccessfulToolResult(t, rpcResponseByID(t, secondMessages, 4))
	if len(asArray(t, asObject(t, preview["plan"])["truthConversions"])) != 2 {
		t.Fatalf("MCP preview did not consume reviewed split: %#v", preview)
	}
}

func TestMigrationMCPExportsAndSubmitsNonActivatingProfileDecision(t *testing.T) {
	root := migrationPreviewFixture(t)
	legacy := `{"schemaVersion":0,"profileId":"project:legacy-rerank","effectiveProfiles":[{"lanes":["exact","fts","graph","dense","rerank"],"requires":{"embedding":"legacy-model"}}]}` + "\n"
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"), legacy)
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "profile-decision-test", "version": "1"},
	}}
	firstMessages := runMCPMessages(t, &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initialize,
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		toolCallMessage(2, "migrate_project", map[string]any{"action": "profile-conflict"}),
	)
	packet := assertSuccessfulToolResult(t, rpcResponseByID(t, firstMessages, 2))
	conflict := asObject(t, packet["conflict"])
	baseline := asObject(t, packet["baseline"])
	profile := asObject(t, baseline["profile"])
	if conflict["legacyProfile"] != legacy || baseline["measurementEvidenceDigest"] == "" {
		t.Fatalf("MCP profile conflict export: %#v", packet)
	}
	submission := map[string]any{
		"schemaVersion": 1, "packetDigest": packet["digest"], "sourceFingerprint": packet["sourceFingerprint"],
		"sourcePath": conflict["sourcePath"], "sourceDigest": conflict["sourceDigest"],
		"baselineProfileId": profile["profileId"], "baselineProfileDigest": baseline["profileDigest"],
		"effectiveProfileName": baseline["effectiveProfileName"], "effectiveProfileDigest": baseline["effectiveProfileDigest"],
		"measurementEvidenceDigest": baseline["measurementEvidenceDigest"],
		"decision":                  "retain-packaged-baseline", "explicitManagerApproval": true, "projectProfileActivation": false,
		"authority": "manager", "reviewer": "manager", "rationale": "MCP explicitly retains only the packaged measured baseline for conversion.",
		"decidedAt": "2026-08-03T12:00:00Z", "replacesDecisionDigest": "",
	}
	secondMessages := runMCPMessages(t, &MCPServer{AssetRoot: adversarialAssetRoot(t), InitialRoot: root},
		initialize,
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		toolCallMessage(3, "migrate_project", map[string]any{"action": "profile-decision", "profileDecision": submission}),
		toolCallMessage(4, "migrate_project", map[string]any{"action": "preview"}),
	)
	decision := assertSuccessfulToolResult(t, rpcResponseByID(t, secondMessages, 3))
	if decision["digest"] == "" || decision["projectProfileActivation"] != false {
		t.Fatalf("MCP profile decision submission: %#v", decision)
	}
	preview := assertSuccessfulToolResult(t, rpcResponseByID(t, secondMessages, 4))
	sealed := asObject(t, asObject(t, preview["plan"])["profileDecision"])
	if sealed["digest"] != decision["digest"] {
		t.Fatalf("MCP preview did not consume sealed profile decision: %#v", preview)
	}
}

func TestMigrationMCPProfileDecisionSchemaRequiresExplicitAuthorityAndLineage(t *testing.T) {
	var migrationTool map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] == "migrate_project" {
			migrationTool = tool
			break
		}
	}
	if migrationTool == nil {
		t.Fatal("migrate_project tool is missing")
	}
	input := asObject(t, migrationTool["inputSchema"])
	properties := asObject(t, input["properties"])
	profileDecision := asObject(t, properties["profileDecision"])
	decisionProperties := asObject(t, profileDecision["properties"])
	for field, want := range map[string]any{
		"decision": migrationProfileDecisionKind, "explicitManagerApproval": true,
		"projectProfileActivation": false, "authority": "manager",
	} {
		if got := asObject(t, decisionProperties[field])["const"]; got != want {
			t.Fatalf("profile decision schema %s const: got %#v want %#v", field, got, want)
		}
	}
	if asObject(t, decisionProperties["decidedAt"])["pattern"] != "Z$" ||
		asObject(t, decisionProperties["replacesDecisionDigest"])["pattern"] == "" {
		t.Fatalf("profile decision schema omitted explicit time or replacement lineage: %#v", profileDecision)
	}
	required, ok := profileDecision["required"].([]string)
	if !ok {
		t.Fatalf("profile decision required list has unexpected type: %#v", profileDecision["required"])
	}
	for _, field := range []string{"authority", "decidedAt", "replacesDecisionDigest", "projectProfileActivation"} {
		if !containsString(required, field) {
			t.Fatalf("profile decision schema does not require %s: %#v", field, required)
		}
	}
}

func TestMigrationMCPExposesCompleteStateMachine(t *testing.T) {
	root := migrationPreviewFixture(t)
	migrationCertificationEvalCases(t, root)
	previewValue, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "preview", LiveCampaigns: []string{"live-campaign"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := previewValue.(MigrationPreview)
	applyValue, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "apply", LiveCampaigns: []string{"live-campaign"}, ApprovedPlanDigest: preview.Plan.PlanDigest, Actor: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	state := applyValue.(MigrationState)
	if state.Adapter != "mcp" || state.State != "inventoried" {
		t.Fatalf("apply did not use MCP adapter: %+v", state)
	}
	resume := func() {
		value, resumeErr := callMigrationProjectTool(root, migrationProjectToolInput{Action: "resume", TransactionID: state.TransactionID, Actor: "manager"})
		if resumeErr != nil {
			t.Fatal(resumeErr)
		}
		state = value.(MigrationState)
	}
	resume()
	if state.State != "shadow-indexed" {
		t.Fatalf("shadow resume: %+v", state)
	}
	query, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "shadow-query", Query: "bounded migration behavior", Campaign: "live-campaign", Limit: 3})
	if err != nil || len(query.(MigrationShadowQueryResult).Matches) != 1 {
		t.Fatalf("shadow query: %+v %v", query, err)
	}
	source := migrationSourceByPath(t, preview.Plan, "active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	coverage := MigrationCoverageReceipt{SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")}, Reviewer: "manager", Rationale: "MCP parity"}
	if _, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "coverage", Coverage: &coverage}); err != nil {
		t.Fatal(err)
	}
	resume()
	if state.State != "normalized" {
		t.Fatalf("normalized resume: %+v", state)
	}
	resume()
	if state.State != "physically-reorganized" {
		t.Fatalf("physical resume: %+v", state)
	}
	for _, gate := range []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"} {
		body := migrationGateArtifactBody(t, root, state, gate, true)
		path := filepath.Join(root, "migration-tests", gate+".json")
		mustWriteFile(t, path, string(body))
		if _, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "gate", Gate: gate, GatePassed: true,
			Artifact: "migration-tests/" + gate + ".json", ArtifactDigest: "sha256:" + SHA256Bytes(body), Reviewer: "manager"}); err != nil {
			t.Fatal(err)
		}
	}
	verifyValue, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "verify"})
	if err != nil || !verifyValue.(MigrationCertification).Candidate {
		t.Fatalf("verify: %+v %v", verifyValue, err)
	}
	resume()
	if state.State != "traversal-verified" {
		t.Fatalf("verified resume: %+v", state)
	}
	statusValue, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "status"})
	if err != nil || statusValue.(MigrationState).CertificationDigest == "" {
		t.Fatalf("status: %+v %v", statusValue, err)
	}
	ratifyValue, err := callMigrationProjectTool(root, migrationProjectToolInput{Action: "ratify", TransactionID: state.TransactionID,
		CertificationDigest: state.CertificationDigest, Actor: "manager"})
	if err != nil || ratifyValue.(MigrationState).State != "migrated" {
		t.Fatalf("ratify: %+v %v", ratifyValue, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "state", "head.json")); err != nil {
		t.Fatalf("ratification head: %v", err)
	}
}
