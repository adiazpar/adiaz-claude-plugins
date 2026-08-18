package knowledge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// commitMigrationFixture gives a fixture project the git archive the 0.8
// conversion requires: preview blocks until every managed source is tracked
// and clean, because `git show <sourceRevision>:<path>` is the recorded
// provenance and recovery recipe. Idempotent; call it again after mutating a
// fixture when the mutation should be part of the archived source state.
func commitMigrationFixture(t *testing.T, root string) {
	t.Helper()
	git := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@invalid",
		)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		git("init", "-q")
		git("config", "core.autocrlf", "false")
		// Benchmark generations bind the repository's dirty fingerprint, so
		// paths the migration and its tests write after a generation is
		// captured must be invisible to git status or every later
		// environment recompute would see a different repository.
		mustWriteFile(t, filepath.Join(root, ".gitignore"),
			".re-discipline/migration/\n.re-discipline/state/\nmigration-tests/\n")
	}
	git("add", "-A")
	git("commit", "-q", "--allow-empty", "-m", "fixture state")
}

func migrationPreviewFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Fixture project\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy laws\n<!-- re-discipline:shared-laws:end -->\n\n## Mission\n\nPreserve fixture truth.\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "config.json"),
		"{\"schemaVersion\":2,\"knowledgeDirectory\":\"knowledge\",\"memory\":{\"mode\":\"shared-only\",\"writePolicy\":\"proposal-only\"},\"knowledge\":{\"enabled\":true,\"profile\":\"plugin:balanced-v1\",\"settingsFile\":\"knowledge/policy.jsonc\",\"projectProfile\":\"knowledge/retrieval-profile.json\"}}\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "local-paths.md"),
		"secret machine path\n")
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"), "<!-- re-discipline:router v0.7.0 -->\nrouter\n<!-- re-discipline:router:end -->\nproject notes\n")
	mustWriteFile(t, filepath.Join(root, ".claude", "CLAUDE.md"), "<!-- re-discipline:claude-adapter v0.7.0 -->\nlegacy adapter\n<!-- re-discipline:claude-adapter:end -->\nclaude notes\n")
	mustWriteFile(t, filepath.Join(root, ".claude", "settings.local.json"), "{\"projectOwned\":true}\n")
	mustWriteFile(t, filepath.Join(root, ".codex", "AGENTS.md"), "<!-- re-discipline:codex-adapter v0.7.0 -->\nlegacy adapter\n<!-- re-discipline:codex-adapter:end -->\ncodex notes\n")
	mustWriteFile(t, filepath.Join(root, ".codex", "external-drafter-contract.md"), "# External Drafter Contract\n\n## Workspace Scope\n\nUse `active/<slug>/subagents/<workspace-id>/` and read `CAMPAIGN.md`.\n\n## Work Standard\n\nPreserve fixture-specific evidence rigor.\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "agents", "dispatch.ps1"), "# legacy subagents dispatcher\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), `{
  "$schema": "plugin://re-discipline/schemas/knowledge-settings.schema.json",
  "schemaVersion": 1,
  "sources": {
    "truth": true,
    "history": false,
    "backlog": true,
    "activeCampaigns": true,
    "sharedMemory": false,
    "drafterReports": true,
    "additional": []
  },
  "models": {"execution": "local"},
  "telemetry": {"mode": "off"},
  "budgets": {
    "searchTokens": 2048,
    "managerContextTokens": 4096,
    "drafterContextTokens": 2048,
    "maxPassages": 9,
    "maxBytes": 16384
  }
}
`)
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "evals", "cases.jsonl"), "{\"id\":\"K-1\"}\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "memory", "topics", "working-style.md"), "# Retained shared memory\n")
	mustWriteFile(t, filepath.Join(root, "active", ".gitkeep"), "placeholder\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "CAMPAIGN.md"),
		"# Campaign: live-campaign\n\n**Status:** OPEN - implementation pending\n**Opened:** 2026-07-27\n\n## Objective\n\nFind the answer.\n\n## Current State\n\n- Parser complete.\n- Runtime integration pending.\n\n## Open Questions\n\n- Which bounded path should be used?\n\n## Deferred Work\n\n- Revisit optional optimization after correctness.\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "REVIEWS.md"),
		"# Review ledger\n\n| Date | Report | PROMOTE | HOLD | DROP | BLOCK | Promoted to |\n|---|---|---:|---:|---:|---:|---|\n| 2026-07-28 | `subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md` | 0 | 1 | 0 | 0 | - |\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "subagents", "2026-07-28T06-09-49Z-codex-worker-a", "brief.md"),
		"# Brief\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "subagents", "2026-07-28T06-09-49Z-codex-worker-a", "report.md"),
		"**Review:** 2026-07-29 fixture-manager\n**Disposition:** PROMOTE=0 HOLD=1 DROP=0 BLOCK=0\n\n# VERDICT\n\nDIRECT: observed bounded migration behavior.\n")
	mustWriteFile(t, filepath.Join(root, "active", "closed-campaign", "CAMPAIGN.md"),
		"# Campaign: closed-campaign\n")
	mustWriteFile(t, filepath.Join(root, "active", "closed-campaign", "subagents", "worker-b", "report.md"),
		"# VERDICT\n\nDIRECT: historical.\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"), "# Fixture accepted truth\n\n**Claim:** The fixture preserves its accepted truth during migration.\n\n**Confidence:** Strong\n")
	mustWriteFile(t, filepath.Join(root, "docs", "history", "chronicle.md"), "# Chronicle\n")
	mustWriteFile(t, filepath.Join(root, "docs", "backlog", "next.md"), "# Next\n")
	mustWriteFile(t, filepath.Join(root, "docs", "INDEX.md"), "# Project map\n\n- [Live](../active/live-campaign/CAMPAIGN.md)\n- `active/<slug>/CAMPAIGN.md` is the legacy navigation shape.\n\nProject-owned navigation note.\n")
	mustWriteFile(t, filepath.Join(root, "docs", "product-guide.md"), "# Unrelated product documentation\n")
	commitMigrationFixture(t, root)
	return root
}

func TestMigrationPreviewIsStableReadOnlyAndDemandDriven(t *testing.T) {
	root := migrationPreviewFixture(t)
	before, err := migrationInventoryDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	second, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if first.Plan.PlanDigest != second.Plan.PlanDigest ||
		first.MigrationPlanYAML != second.MigrationPlanYAML ||
		first.SourceInventoryJSONL != second.SourceInventoryJSONL {
		t.Fatal("identical inputs must produce byte-stable migration previews")
	}
	after, err := migrationInventoryDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("preview changed canonical project state")
	}
	if len(first.Plan.Unresolved) != 1 ||
		!strings.Contains(first.Plan.Unresolved[0], "live-campaign") {
		t.Fatalf("only the designated live report should demand exhaustive coverage: %+v", first.Plan.Unresolved)
	}
	if strings.Contains(first.SourceInventoryJSONL, "local-paths.md") ||
		strings.Contains(first.SourceInventoryJSONL, "secret machine path") ||
		strings.Contains(first.SourceInventoryJSONL, ".gitkeep") ||
		strings.Contains(first.SourceInventoryJSONL, "product-guide.md") ||
		strings.Contains(first.SourceInventoryJSONL, "settings.local.json") {
		t.Fatal("machine-local paths must not enter the migration inventory")
	}
	if first.Plan.Estimate.LegacyReports != 2 || first.Plan.Estimate.Campaigns != 2 {
		t.Fatalf("unexpected estimate: %+v", first.Plan.Estimate)
	}
	if first.Receipt.Validation != "passed" || first.Receipt.PlanDigest != first.Plan.PlanDigest || first.Receipt.Digest == "" {
		t.Fatalf("preview omitted its digest-bound equivalence receipt: %+v", first.Receipt)
	}
	if first.Plan.SourceRevision == "" {
		t.Fatal("preview did not record the archived source revision")
	}
	master := migrationSourceByPath(t, first.Plan, "active/live-campaign/CAMPAIGN.md")
	for _, operation := range first.Plan.Operations {
		if operation.Sources[0] != master.Path {
			continue
		}
		for _, destination := range append(append([]string{}, operation.Destinations...), operation.Destination) {
			if strings.Contains(destination, "/payload/legacy/") {
				t.Fatalf("converted masterfile must not plan a payload copy, got %s", destination)
			}
		}
	}
	if first.Plan.HostInventory.RuntimeVersion != RuntimeVersion ||
		first.Plan.HostInventory.RuntimeAvailability != "available" ||
		first.Plan.HostInventory.CLIAvailability != "available" ||
		first.Plan.HostInventory.InstalledPlugin != "not-probed" ||
		first.Plan.HostInventory.MCPStartupStatus != "not-probed" ||
		len(first.Plan.HostInventory.ManagerHosts) != 2 {
		t.Fatalf("preview omitted the honest deterministic host inventory: %+v", first.Plan.HostInventory)
	}
	for _, host := range first.Plan.HostInventory.ManagerHosts {
		if host.StartupStatus != "not-probed" || host.ToolSchemaStatus != "not-probed" ||
			host.AdapterStatus != "observed-present" || host.Availability != "not-probed" {
			t.Fatalf("preview claimed unperformed host health or omitted its adapter: %+v", host)
		}
	}
	if !strings.Contains(first.MigrationPlanYAML, "hostInventory:") ||
		!strings.Contains(first.MigrationPlanMarkdown, "## Host inventory") {
		t.Fatal("rendered preview omitted its plan-bound host inventory")
	}
}

func TestMigrationTransformsLegacyKnowledgePolicyWithoutSilentDefaults(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range preview.Plan.Conflicts {
		if conflict.Code == "unsupported-knowledge-policy" {
			t.Fatalf("valid legacy policy was rejected: %+v", conflict)
		}
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	source := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{fullMigrationCoverage(t, root, source, "non-claim", "")},
		Reviewer: "manager", Rationale: "policy conversion fixture coverage",
	}); err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate: %+v %v", state, err)
	}
	configuration := LoadConfiguration(root)
	if !configuration.Valid || configuration.Settings.SchemaVersion != SettingsSchemaVersion ||
		!configuration.Settings.Sources.Truth || configuration.Settings.Sources.HistoryFindings ||
		!configuration.Settings.Sources.Backlog || !configuration.Settings.Sources.ActiveFindings ||
		configuration.Settings.Sources.SharedMemory || !configuration.Settings.Sources.ReportFallback ||
		configuration.Settings.Models.Execution != "local" || configuration.Settings.Telemetry.Mode != "off" ||
		configuration.Settings.Budgets.SearchTokens != 2048 || configuration.Settings.Budgets.ManagerContextTokens != 4096 ||
		configuration.Settings.Budgets.DrafterContextTokens != 2048 || configuration.Settings.Budgets.MaxPassages != 9 ||
		configuration.Settings.Budgets.MaxBytes != 16384 || !configuration.Settings.Archive.ReportFallbackUntilMeasured {
		t.Fatalf("legacy policy semantics were not preserved in the valid 0.8 policy: %+v", configuration)
	}
	if err := os.RemoveAll(filepath.Join(root, ".re-discipline", "cache")); err != nil {
		t.Fatal(err)
	}
	certification, err := engine.Verify()
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range certification.Blockers {
		if strings.Contains(blocker, "canonical bootstrap configuration is invalid") {
			t.Fatalf("post-cache-rebuild structural verification rejected the transformed policy: %s", blocker)
		}
	}
}

func TestMigrationBlocksUnsupportedLegacyKnowledgePolicyBeforeApply(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "policy.jsonc"), `{
  "schemaVersion": 1,
  "sources": {"truth": true, "history": true, "backlog": true, "activeCampaigns": true, "sharedMemory": true, "drafterReports": true, "silentRemoteSource": true},
  "models": {"execution": "local"},
  "telemetry": {"mode": "metrics-only"},
  "budgets": {"searchTokens": 2048, "managerContextTokens": 4096, "drafterContextTokens": 2048, "maxPassages": 9, "maxBytes": 16384}
}`)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, conflict := range preview.Plan.Conflicts {
		blocked = blocked || conflict.Code == "unsupported-knowledge-policy" && conflict.Blocks
	}
	if !blocked {
		t.Fatalf("unsupported legacy policy was not an explicit blocker: %+v", preview.Plan.Conflicts)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("unsupported legacy policy was allowed to begin activation")
	}
}

func TestMigrationEmbeddedProjectTemplatesMatchReleaseTemplates(t *testing.T) {
	pluginRoot := filepath.Clean(filepath.Join(adversarialAssetRoot(t), ".."))
	for _, test := range []struct {
		path     string
		embedded string
	}{
		{"dispatch.ps1", migrationDispatchTemplate},
		{"external-drafter-contract.md", migrationExternalDrafterContractTemplate},
		{"drafter-AGENTS-override.md", migrationDrafterOverrideTemplate},
	} {
		body, err := os.ReadFile(filepath.Join(pluginRoot, "templates", "project", test.path))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(body)) != strings.TrimSpace(test.embedded) {
			t.Fatalf("embedded migration template %s drifted from the release template", test.path)
		}
	}
}

func TestMigrationRuntimeRecordsStrictRoundTrip(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	certification := MigrationCertification{
		SchemaVersion: MigrationSchemaVersion, TransactionID: "M-0123456789ABCDEF0123",
		PlanDigest: preview.Plan.PlanDigest, State: "physically-reorganized",
		RequiredGates: []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"},
		GateReceipts:  []MigrationGateReceipt{}, Blockers: []string{"fixture"},
	}
	certification.Digest, err = CanonicalDigest(certification)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		fresh any
	}{
		{"plan", preview.Plan, &MigrationPlan{}},
		{"operation", preview.Plan.Operations[0], &MigrationOperation{}},
		{"receipt", certification, &MigrationCertification{}},
	} {
		body, err := json.Marshal(test.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := decodeStrict(body, test.fresh); err != nil {
			t.Fatalf("strict %s decode: %v", test.name, err)
		}
		decoded := reflect.ValueOf(test.fresh).Elem().Interface()
		if !reflect.DeepEqual(decoded, test.value) {
			t.Fatalf("%s changed during strict round trip", test.name)
		}
	}
}

func TestMigrationPreviewDigestInvalidatesOnRelevantChange(t *testing.T) {
	root := migrationPreviewFixture(t)
	first, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(root, "active", "closed-campaign", "subagents", "worker-b", "report.md")
	mustWriteFile(t, report, "# VERDICT\n\nDIRECT: changed.\n")
	second, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.PlanDigest == second.Plan.PlanDigest ||
		first.Plan.SourceFingerprint == second.Plan.SourceFingerprint {
		t.Fatal("a relevant input change must invalidate the approved plan")
	}
}

func TestMigrationPreviewIgnoresUnrelatedDocumentationChanges(t *testing.T) {
	root := migrationPreviewFixture(t)
	first, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "docs", "product-guide.md"), "# User changed unrelated documentation\n")
	mustWriteFile(t, filepath.Join(root, ".claude", "settings.local.json"), "{\"projectOwned\":\"changed\"}\n")
	second, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.PlanDigest != second.Plan.PlanDigest || first.Plan.SourceFingerprint != second.Plan.SourceFingerprint {
		t.Fatal("unrelated documentation and host-local settings must not invalidate an approved managed-state plan")
	}
}

func TestWriteMigrationPreviewRefusesCanonicalProjectPath(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMigrationPreview(root, filepath.Join(root, "preview"), preview); err == nil {
		t.Fatal("preview artifacts must not be written into canonical project state")
	}
	output := t.TempDir()
	if err := WriteMigrationPreview(root, output, preview); err != nil {
		t.Fatalf("external preview output: %v", err)
	}
	for _, name := range []string{
		"migration-plan.yaml", "migration-plan.md", "source-inventory.jsonl",
		"conflict-report.json", "baseline-retrieval-plan.json", "preview-receipt.json",
	} {
		if info, err := os.Stat(filepath.Join(output, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing preview artifact %s: %v", name, err)
		}
	}
}

func TestMigrationPreviewRejectsMixedLegacyAndCanonicalCampaignState(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "campaign.json"), "{}\n")
	before, err := digestRegularTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewMigration(root, []string{"live-campaign"}); err == nil || !strings.Contains(err.Error(), "got 0.8") {
		t.Fatalf("mixed legacy/canonical state was not rejected as a wrong-version source: %v", err)
	}
	after, err := digestRegularTree(root)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("wrong-version preview mutated the project: %v", err)
	}
}

func migrationInventoryDigest(root string) (string, error) {
	boundary, err := NewBoundary(root)
	if err != nil {
		return "", err
	}
	sources, conflicts, err := migrationInventory(boundary)
	if err != nil {
		return "", err
	}
	return CanonicalDigest(struct {
		Sources   []MigrationSource
		Conflicts []MigrationConflict
	}{sources, conflicts})
}

// reviewFixtureTruthConflicts supplies a manager review for every pending
// truth conflict. Migration requires one per converted document because a
// finding is invalid without reviewed retrieval questions, so a fixture that
// converts truth must review it exactly as a manager would.
func reviewFixtureTruthConflicts(t *testing.T, root string) {
	t.Helper()
	for attempt := 0; attempt < 64; attempt++ {
		packet, err := ExportMigrationTruthConflicts(root)
		if err != nil {
			t.Fatalf("export truth conflicts: %v", err)
		}
		pending := []MigrationTruthConflict{}
		for _, conflict := range packet.Conflicts {
			if _, loadErr := loadMigrationTruthReview(root, conflict); loadErr != nil {
				pending = append(pending, conflict)
			}
		}
		if len(pending) == 0 {
			return
		}
		conflict := pending[0]
		title := strings.TrimSpace(conflict.Title)
		if title == "" {
			title = "Reviewed fixture truth"
		}
		claim := strings.TrimSpace(conflict.SourceCoverageText)
		if len([]rune(claim)) > 500 {
			claim = string([]rune(claim)[:480])
		}
		if _, err := SubmitMigrationTruthReview(root, MigrationTruthReviewSubmission{
			SchemaVersion: 1, PacketDigest: packet.Digest, SourcePath: conflict.SourcePath,
			SourceDigest: conflict.SourceDigest, Reviewer: "manager",
			Rationale: "Confirmed the accepted claim and supplied reviewed retrieval questions.",
			Claims: []MigrationTruthAtomicClaim{{
				SourceText: conflict.SourceCoverageText, Title: title, Claim: claim,
				SyntheticQuestions: []string{
					"What does " + title + " establish?",
					"Which accepted record covers " + title + "?",
					"How is " + title + " verified?",
				},
			}},
		}, ""); err != nil {
			t.Fatalf("review fixture truth %s: %v", conflict.SourcePath, err)
		}
	}
	t.Fatal("fixture truth conflicts did not converge")
}
