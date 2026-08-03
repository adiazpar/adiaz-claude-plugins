package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEnsureSurfacesIncompleteMigrationBeforePhysicalCutover(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := EnsureProject(context.Background(), root, pluginRootForTests(t), 7000)
	if err != nil {
		t.Fatal(err)
	}
	system := payload["system"].(map[string]any)
	if system["migrationState"] != state.State || system["migrationTransaction"] != state.TransactionID ||
		!strings.Contains(fmt.Sprint(system["attention"]), "migration transaction is incomplete") {
		t.Fatalf("ensure hid the in-progress migration behind a legacy status: %+v", system)
	}
}

func TestMigrationMaterializationRejectsInjectedUnmaterializedDestination(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, _ := migrationFixtureToNormalized(t, root)
	plan.Operations[0].Destinations = append(plan.Operations[0].Destinations,
		"docs/backlog/injected-but-unmaterialized.md")
	staged := filepath.Join(engine.migrationRoot(), "staging", "project")
	if _, err := engine.verifyMigrationPlanMaterialization(plan, staged); err == nil ||
		!strings.Contains(err.Error(), "did not materialize") {
		t.Fatalf("unmaterialized planned destination did not fail closed: %v", err)
	}
}

func TestMigrationStartRejectsPlanMutationBehindDeclaredDigest(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	preview.Plan.Operations[0].Destinations = append(preview.Plan.Operations[0].Destinations,
		"docs/backlog/unapproved-destination.md")
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil ||
		!strings.Contains(err.Error(), "digest does not authenticate") {
		t.Fatalf("mutated plan was accepted behind its original digest: %v", err)
	}
	if _, err := os.Stat(engine.statePath()); !os.IsNotExist(err) {
		t.Fatalf("rejected unauthenticated plan created migration state: %v", err)
	}
}

func TestMigrationProjectIdentityCannotBeReplacedByRetrievalProfileDigest(t *testing.T) {
	root := migrationPreviewFixture(t)
	retrievalProfile := []byte(`{"schemaVersion":1,"weights":{"exact":8,"lexical":6}}` + "\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"), string(retrievalProfile))
	profileBody, err := os.ReadFile(filepath.Join(root, ".re-discipline", "project-profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	want := SHA256Bytes(profileBody)
	if preview.Plan.ProjectIdentity != want {
		t.Fatalf("project identity = %s, want project-profile digest %s", preview.Plan.ProjectIdentity, want)
	}
	if preview.Plan.ProjectIdentity == SHA256Bytes(retrievalProfile) {
		t.Fatal("project identity was substituted with the retrieval-profile digest")
	}
}

func TestMigrationRejectsPartialCanonicalControlPlaneShapes(t *testing.T) {
	for _, relative := range []string{
		"active/live-campaign/work-items/W-0001.json",
		"active/live-campaign/runs/R-19700101-00000001/run.json",
		"active/live-campaign/events/events.jsonl",
		"docs/history/campaigns/legacy/README.md",
		".re-discipline/state/inventory.json",
	} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			root := migrationPreviewFixture(t)
			mustWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), "{}\n")
			version, err := DetectProjectStateVersion(root)
			if err != nil || version != "0.8" {
				t.Fatalf("partial canonical path %s was reinterpreted as legacy: %q %v", relative, version, err)
			}
			if _, err := PreviewMigration(root, []string{"live-campaign"}); err == nil || !strings.Contains(err.Error(), "got 0.8") {
				t.Fatalf("migration accepted partial canonical control plane %s: %v", relative, err)
			}
		})
	}
}

func TestMigrationGenesisInventoryCoversCanonicalRecordsEventsAndRunArtifacts(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToTraversal(t, root)
	inventory, err := loadVerifiedMigrationStateInventory(root, int64(len(migrationCampaigns(plan))))
	if err != nil {
		t.Fatal(err)
	}
	entries := inventoryEntriesMap(inventory)
	wantKinds := map[string]bool{"campaign": false, "run": false, "event": false, "report": false, "context": false}
	for relative := range entries {
		switch {
		case strings.HasSuffix(relative, "/campaign.json"):
			wantKinds["campaign"] = true
		case strings.HasSuffix(relative, "/run.json"):
			wantKinds["run"] = true
		case strings.HasSuffix(relative, "/events/events.jsonl"):
			wantKinds["event"] = true
		case strings.HasSuffix(relative, "/report.md"):
			wantKinds["report"] = true
		case strings.HasSuffix(relative, "/context-pack.json"):
			wantKinds["context"] = true
		}
		if strings.HasSuffix(relative, "/STATE.md") || relative == stateInventoryPath ||
			strings.HasPrefix(relative, ".re-discipline/cache/") || strings.HasPrefix(relative, ".re-discipline/state/receipts/") {
			t.Fatalf("derived/cache/receipt path entered genesis inventory: %s", relative)
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Fatalf("genesis inventory omitted canonical %s bytes: %+v", kind, inventory.Entries)
		}
	}
	state, err = engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify inventory fixture: %+v %v", state, err)
	}
	var manifest MigrationRatificationManifest
	body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "migration", "0.8", "certification.json"))
	if err != nil || json.Unmarshal(body, &manifest) != nil || manifest.ImportHead.InventoryDigest != inventory.Digest {
		t.Fatalf("ratification manifest did not bind activated genesis inventory: %+v %v", manifest.ImportHead, err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := store.loadCommittedInventory(head)
	if err != nil {
		t.Fatal(err)
	}
	if dirty, err := store.inventoryDrift(committed); err != nil || len(dirty) != 0 {
		t.Fatalf("ratified migration left dirty canonical state: %v %v", dirty, err)
	}
	if inventoryEntriesMap(committed)[".re-discipline/migration/0.8/certification.json"] == "" {
		t.Fatal("post-ratification inventory omitted the canonical migration audit artifact")
	}
	validateEmittedStateInventoryAgainstPackagedSchema(t, root)
}

func validateEmittedStateInventoryAgainstPackagedSchema(t *testing.T, root string) {
	t.Helper()
	schemaBody, err := os.ReadFile(filepath.Join(adversarialAssetRoot(t), "schemas", "state-inventory.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			SchemaVersion struct {
				Const int `json:"const"`
			} `json:"schemaVersion"`
			HeadRevision struct {
				Minimum int64 `json:"minimum"`
			} `json:"headRevision"`
			Entries struct {
				UniqueItems bool `json:"uniqueItems"`
				Items       struct {
					Required             []string `json:"required"`
					AdditionalProperties bool     `json:"additionalProperties"`
					Properties           struct {
						Path struct {
							Pattern string `json:"pattern"`
						} `json:"path"`
						ContentDigest struct {
							Pattern string `json:"pattern"`
						} `json:"contentDigest"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"entries"`
			Digest struct {
				Pattern string `json:"pattern"`
			} `json:"digest"`
		} `json:"properties"`
		AdditionalProperties bool `json:"additionalProperties"`
	}
	if err := json.Unmarshal(schemaBody, &schema); err != nil {
		t.Fatal(err)
	}
	inventoryBody, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stateInventoryPath)))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(inventoryBody, &raw); err != nil {
		t.Fatal(err)
	}
	if !schema.AdditionalProperties && len(raw) != len(schema.Required) {
		t.Fatalf("emitted inventory has schema-forbidden top-level properties: %v", raw)
	}
	for _, required := range schema.Required {
		if _, ok := raw[required]; !ok {
			t.Fatalf("emitted inventory omits schema-required property %s", required)
		}
	}
	var inventory StateInventory
	if err := decodeStrictJSON(inventoryBody, &inventory); err != nil {
		t.Fatal(err)
	}
	compile := func(pattern string) *regexp.Regexp {
		// Go's RE2 syntax lacks JSON Schema's non-capturing group notation;
		// replacing it with a capturing group preserves the accepted language.
		compiled, err := regexp.Compile(strings.ReplaceAll(pattern, "(?:", "("))
		if err != nil {
			t.Fatalf("compile packaged JSON Schema pattern %q: %v", pattern, err)
		}
		return compiled
	}
	pathPattern := compile(schema.Properties.Entries.Items.Properties.Path.Pattern)
	digestPattern := compile(schema.Properties.Entries.Items.Properties.ContentDigest.Pattern)
	rootDigestPattern := compile(schema.Properties.Digest.Pattern)
	if inventory.SchemaVersion != schema.Properties.SchemaVersion.Const ||
		inventory.HeadRevision < schema.Properties.HeadRevision.Minimum ||
		!rootDigestPattern.MatchString(inventory.Digest) {
		t.Fatalf("emitted inventory violates schema scalar constraints: %+v", inventory)
	}
	seen := map[string]bool{}
	for _, entry := range inventory.Entries {
		if !pathPattern.MatchString(entry.Path) || !digestPattern.MatchString(entry.ContentDigest) ||
			schema.Properties.Entries.UniqueItems && seen[entry.Path+"\x00"+entry.ContentDigest] {
			t.Fatalf("emitted inventory entry violates packaged JSON Schema: %+v", entry)
		}
		seen[entry.Path+"\x00"+entry.ContentDigest] = true
	}
}

func TestMigrationRatificationRecoversAfterInventoryPublication(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, _, state := migrationFixtureToTraversal(t, root)
	fired := false
	engine.StateFailpoint = func(point StateFailpoint) error {
		if !fired && point.Name == FailAfterInventoryPublish {
			fired = true
			return errors.New("fixture crash after migration inventory publication")
		}
		return nil
	}
	if _, err := engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli"); err == nil {
		t.Fatal("inventory publication failpoint did not interrupt ratification")
	}
	engine.StateFailpoint = nil
	state, err := engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "mcp")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratification did not recover the inventory/head pair: %+v %v", state, err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.loadCommittedInventory(head); err != nil {
		t.Fatalf("recovered ratification head lost its inventory: %v", err)
	}
}

func TestMigrationRejectsRecomputedIncompleteGenesisInventory(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToPhysical(t, root)
	inventory, err := loadVerifiedMigrationStateInventory(root, int64(len(migrationCampaigns(plan))))
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	filtered := inventory.Entries[:0]
	for _, entry := range inventory.Entries {
		if !removed && strings.HasSuffix(entry.Path, "/report.md") {
			removed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !removed {
		t.Fatal("fixture inventory has no report handle to remove")
	}
	inventory.Entries = filtered
	if err := sealStateInventory(&inventory); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(filepath.Join(root, filepath.FromSlash(stateInventoryPath)), inventory, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVerifiedMigrationStateInventory(root, int64(len(migrationCampaigns(plan)))); err == nil {
		t.Fatal("recomputed inventory that omitted a canonical report was accepted")
	}
	certification, err := engine.Verify()
	if err != nil || certification.Candidate ||
		!strings.Contains(strings.Join(certification.Blockers, "\n"), "activated managed target") {
		t.Fatalf("certification did not fail closed on incomplete activated inventory: %+v %v state=%+v", certification, err, state)
	}
}

func TestMigrationRejectsUnsafeTransactionRootBeforeWriting(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "migration"), "not a directory\n")
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, conflict := range preview.Plan.Conflicts {
		found = found || conflict.Code == "unsafe-migration-root" && conflict.Blocks
	}
	if !found {
		t.Fatalf("unsafe transaction root was not a preview blocker: %+v", preview.Plan.Conflicts)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("migration attempted to write through an unsafe transaction root")
	}
}

func TestMigrationRejectsPOSIXSymlinkTransactionParentBeforePlanOrStagingWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink coverage")
	}
	root := migrationPreviewFixture(t)
	outside := t.TempDir()
	link := filepath.Join(root, ".re-discipline", "migration")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, conflict := range preview.Plan.Conflicts {
		blocked = blocked || conflict.Code == "unsafe-migration-root" && conflict.Blocks
	}
	if !blocked {
		t.Fatalf("real symlink transaction parent was not a preview blocker: %+v", preview.Plan.Conflicts)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("migration wrote a plan or staging output through a symlink parent")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("migration wrote through the rejected symlink: %v %+v", err, entries)
	}
}

func TestMigrationRejectsWindowsJunctionTransactionParentBeforePlanOrStagingWrite(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction coverage")
	}
	root := migrationPreviewFixture(t)
	outside := t.TempDir()
	junction := filepath.Join(root, ".re-discipline", "migration")
	command := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, outside)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create real junction: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(junction); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove test junction: %v", err)
		}
	})
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, conflict := range preview.Plan.Conflicts {
		blocked = blocked || conflict.Code == "unsafe-migration-root" && conflict.Blocks
	}
	if !blocked {
		t.Fatalf("real junction transaction parent was not a preview blocker: %+v", preview.Plan.Conflicts)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("migration wrote a plan or staging output through a junction parent")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("migration wrote through the rejected junction: %v %+v", err, entries)
	}
}

func TestMigrationActivationClosesPostDigestLegacyWriterWindow(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, _, state := migrationFixtureToNormalized(t, root)
	report := filepath.Join(root, "active", "live-campaign", "subagents",
		"2026-07-28T06-09-49Z-codex-worker-a", "report.md")
	engine.ActivationPrepareHook = func() {
		_ = os.WriteFile(report, []byte("# VERDICT\n\nDIRECT: raced after activation target digests.\n"), 0o600)
	}
	if _, err := engine.Resume(state.TransactionID, "manager", "cli"); err == nil ||
		(!strings.Contains(err.Error(), "inputs changed after preview") && !strings.Contains(err.Error(), "source changed")) {
		t.Fatalf("post-digest legacy writer was not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "activation.json")); !os.IsNotExist(err) {
		t.Fatal("activation journal was published after TOCTOU rejection")
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "backups")); !os.IsNotExist(err) {
		t.Fatal("backup rename began after TOCTOU rejection")
	}
}

func TestMigrationPreservesMeasurementArtifactsByteExactWithRecovery(t *testing.T) {
	root := migrationPreviewFixture(t)
	relative := filepath.ToSlash(filepath.Join(".re-discipline", "knowledge", "measurements", "lane-ablation.json"))
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	body := []byte("{\"kind\":\"measurement-only\",\"result\":\"passed\"}\n")
	mustWriteFile(t, absolute, string(body))
	wantTime := time.Date(2026, 7, 31, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(absolute, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	source := migrationSourceByPath(t, preview.Plan, relative)
	if source.Role != "measurement" || source.Disposition != "retain" || source.MtimeNS != wantTime.UnixNano() {
		t.Fatalf("measurement was not inventoried as byte-exact timestamped state: %+v", source)
	}
	engine, _, state := migrationFixtureToNormalized(t, root)
	staged := filepath.Join(engine.migrationRoot(), "staging", "project", filepath.FromSlash(relative))
	if got, err := os.ReadFile(staged); err != nil || string(got) != string(body) {
		t.Fatalf("staged measurement changed: %v %q", err, got)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate measurement fixture: %+v %v", state, err)
	}
	info, err := os.Stat(absolute)
	if got, readErr := os.ReadFile(absolute); err != nil || readErr != nil || string(got) != string(body) || info.ModTime().UnixNano() != wantTime.UnixNano() {
		t.Fatalf("published measurement lost bytes or timestamp: stat=%v read=%v", err, readErr)
	}
	backup := strings.TrimPrefix(SHA256String(".re-discipline/knowledge"), "sha256:")[:16] + "-knowledge"
	backupPath := filepath.Join(engine.migrationRoot(), "backups", backup, "measurements", "lane-ablation.json")
	if got, err := os.ReadFile(backupPath); err != nil || string(got) != string(body) {
		t.Fatalf("measurement recovery copy is missing: %v %q", err, got)
	}
}

func TestMigrationBlocksExcludedSensitiveFileInsideReplacementRoot(t *testing.T) {
	for _, relative := range []string{"active/live-campaign/.env.secret", ".re-discipline/agents/.env.secret"} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			root := migrationPreviewFixture(t)
			target := filepath.Join(root, filepath.FromSlash(relative))
			mustWriteFile(t, target, "DO_NOT_DROP=1\n")
			preview, err := PreviewMigration(root, []string{"live-campaign"})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, conflict := range preview.Plan.Conflicts {
				if conflict.Code == "excluded-managed-file" && conflict.Path == relative && conflict.Blocks {
					found = true
				}
			}
			if !found {
				t.Fatalf("sensitive file inside replaced tree was not a blocker: %+v", preview.Plan.Conflicts)
			}
			engine := fixedMigrationEngine(t, root)
			if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
				t.Fatal("apply accepted a plan that would drop an excluded managed file")
			}
		})
	}
}

func writeAdditiveKnowledgeFixture(t *testing.T, root, findingID string) {
	t.Helper()
	campaign := "live-campaign"
	campaignID := "C-LIVE-CAMPAIGN"
	legacyRef := "subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md"
	legacyPath := "active/" + campaign + "/" + legacyRef
	report, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(legacyPath)))
	if err != nil {
		t.Fatal(err)
	}
	lineCount, err := migrationReportLineCount(report)
	if err != nil {
		t.Fatal(err)
	}
	coverage := CoverageEntry{
		SourcePath: legacyPath, SourceSHA256: "sha256:" + SHA256Bytes(report),
		StartLine: 1, EndLine: lineCount, SourceLineCount: lineCount,
		Disposition: "candidate-finding", TargetID: findingID,
	}
	coverage.SourceHandle = canonicalCoverageHandle(coverage)
	findingPath := "active/" + campaign + "/findings/" + findingID + ".md"
	document := FindingDocument{Record: FindingRecord{
		SchemaVersion: CampaignSchemaVersion, ID: findingID, CampaignID: campaignID, Revision: 1,
		CreatedAt: stateTestTime, UpdatedAt: stateTestTime, CreatedBy: "curator", UpdatedBy: "manager",
		CorrelationID: "additive-07", Kind: "conclusion", Subject: "Additive knowledge",
		Claim: "The additive 0.7 knowledge record retains its manager decision during cutover.",
		Scope: map[string]any{"campaign": campaign}, AppliesWhen: []string{"During the fixture migration."},
		KnownLimits: []string{"The source run still uses the 0.7 compatibility path."}, SourceRuns: []string{legacyRef},
		Evidence: []EvidenceReference{{Path: legacyPath, SHA256: "sha256:" + SHA256Bytes(report),
			StartLine: 1, EndLine: lineCount, ObjectKey: coverage.SourceHandle, SourceRun: legacyRef}}, Relations: FindingRelations{},
		EvidenceGrade: "direct", ReviewState: "manager-ratified", Validity: "current", Projection: "campaign",
		Body: renderMigrationFindingBody(MigrationFindingInput{Claim: "The additive 0.7 knowledge record retains its manager decision during cutover."}, legacyPath),
		Path: findingPath,
	}, SyntheticQuestions: []string{
		"What happens to additive knowledge during cutover?",
		"Which record retains the manager decision?",
		"Where is the additive finding provenance?",
	}, QuestionsReviewed: true}
	document = normalizeFindingDocument(document)
	digest, err := findingDocumentDigest(document)
	if err != nil {
		t.Fatal(err)
	}
	findingBody, err := renderFindingDocument(document, digest)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(findingPath)), string(findingBody))

	intake := IntakeRecord{RecordMeta: stateTestMeta("I-9001", 2), CampaignID: campaignID,
		SourceRuns:          []FileHandle{{Path: legacyPath, SHA256: "sha256:" + SHA256Bytes(report)}},
		CandidateFindingIDs: []string{findingID}, Coverage: []CoverageEntry{coverage},
		Triage: map[string]string{findingID: "attention"}, Status: "reviewed"}
	intake.Digest = ""
	intakeBody, err := sealMigratedRecord(&intake)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "active", campaign, "intake", intake.ID+".json"), string(intakeBody))

	packetDigest := stateTestDigest("a")
	review := ReviewRecord{RecordMeta: stateTestMeta("V-9001", 1), CampaignID: campaignID,
		Reviewer: "manager", Authority: "manager", IntakeID: intake.ID, IntakeRevision: intake.Revision - 1,
		PacketDigest: packetDigest, ReviewLoad: stateTestReviewLoad("V-9001", campaignID, packetDigest, 0, 1),
		Decisions: []ReviewDecision{{FindingID: findingID, FindingRevision: 1, Action: "ratify", Projection: "campaign", Rationale: "Measured additive decision."}}}
	review.Digest = ""
	reviewBody, err := sealMigratedRecord(&review)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "active", campaign, "reviews", review.ID+".json"), string(reviewBody))
}

func TestMigrationTransformsAdditiveKnowledgeAndKeepsReferencesIntact(t *testing.T) {
	root := migrationPreviewFixture(t)
	writeAdditiveKnowledgeFixture(t, root, "F-9001")
	engine, _, state := migrationFixtureToNormalized(t, root)
	stagedCampaign := filepath.Join(engine.migrationRoot(), "staging", "project", "active", "live-campaign")
	findingBody, err := os.ReadFile(filepath.Join(stagedCampaign, "findings", "F-9001.md"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(findingBody, "active/live-campaign/findings/F-9001.md")
	if err != nil {
		t.Fatal(err)
	}
	wantRun := legacyRunID("live-campaign", "2026-07-28T06-09-49Z-codex-worker-a")
	if len(document.Record.SourceRuns) != 1 || document.Record.SourceRuns[0] != wantRun ||
		document.Record.Evidence[0].SourceRun != wantRun ||
		!strings.Contains(document.Record.Evidence[0].Path, "/runs/"+wantRun+"/report.md") {
		t.Fatalf("additive finding did not receive canonical run references: %+v", document.Record)
	}
	var intake IntakeRecord
	intakeBody, _ := os.ReadFile(filepath.Join(stagedCampaign, "intake", "I-9001.json"))
	if err := decodeStrictJSON(intakeBody, &intake); err != nil || intake.ID != "I-9001" ||
		!strings.Contains(intake.SourceRuns[0].Path, "/runs/"+wantRun+"/report.md") {
		t.Fatalf("additive intake identity/reference changed incorrectly: %+v %v", intake, err)
	}
	var review ReviewRecord
	reviewBody, _ := os.ReadFile(filepath.Join(stagedCampaign, "reviews", "V-9001.json"))
	if err := decodeStrictJSON(reviewBody, &review); err != nil || review.ID != "V-9001" ||
		review.IntakeID != "I-9001" || review.Decisions[0].FindingID != "F-9001" {
		t.Fatalf("additive review referential identity changed: %+v %v", review, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate additive knowledge fixture: %+v %v", state, err)
	}
	if err := engine.verifyMigrationStructural(mustLoadMigrationPlan(t, engine)); err != nil {
		t.Fatalf("additive knowledge graph did not certify structurally: %v", err)
	}
}

func TestLegacyReviewImportDoesNotClobberPreviouslyLinkedKnowledge(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToNormalized(t, root)
	campaignDir := filepath.Join(engine.migrationRoot(), "staging", "project", "active", "live-campaign")
	runID := legacyRunID("live-campaign", "campaign-import")
	runPath := filepath.Join(campaignDir, "runs", runID, "run.json")
	body, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatal(err)
	}
	var run RunRecord
	if err := decodeStrictJSON(body, &run); err != nil {
		t.Fatal(err)
	}
	run.FindingIDs = SortedUnique(append(run.FindingIDs, "F-9999"))
	body, err = sealMigratedRecord(&run)
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(runPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := engine.stageLegacyReviewImport(plan, state, "live-campaign", campaignDir); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(runPath)
	if err != nil || decodeStrictJSON(body, &run) != nil || !containsString(run.FindingIDs, "F-9999") {
		t.Fatalf("review import clobbered linked finding IDs: %+v %v", run.FindingIDs, err)
	}
}

func mustLoadMigrationPlan(t *testing.T, engine *MigrationEngine) MigrationPlan {
	t.Helper()
	plan, err := engine.loadPlan()
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestMigrationNamespaceRegistryRejectsEveryGeneratedNamespaceCollision(t *testing.T) {
	for _, namespace := range []string{"campaign", "work-item", "run", "finding", "intake", "review", "event", "receipt"} {
		registry := newMigrationNamespaceRegistry()
		if err := registry.reserve(namespace, "scope", "ID-1", "first"); err != nil {
			t.Fatal(err)
		}
		if err := registry.reserve(namespace, "scope", "ID-1", "second"); err == nil || !strings.Contains(err.Error(), namespace+" namespace collision") {
			t.Fatalf("%s namespace collision was not deterministic: %v", namespace, err)
		}
	}
	root := migrationPreviewFixture(t)
	writeAdditiveKnowledgeFixture(t, root, "F-0001")
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
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
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")},
		Reviewer: "manager", Rationale: "collision fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Resume(state.TransactionID, "manager", "cli"); err == nil || !strings.Contains(err.Error(), "finding namespace collision") {
		t.Fatalf("generated/additive finding collision did not fail before staging: %v", err)
	}
}

func TestLegacyFrontierExtractionIsUnboundedAndDoesNotInferCompletion(t *testing.T) {
	var body strings.Builder
	body.WriteString("# Campaign\n\n## Branches\n\n")
	for index := 0; index < 120; index++ {
		fmt.Fprintf(&body, "- Branch %03d remains complete enough to investigate later.\n", index)
	}
	body.WriteString("\n| Branch | Status |\n|---|---|\n| Explicit branch | completed |\n")
	state := MigrationState{Actor: "manager", TransactionID: "M-TEST", CreatedAt: stateTestTime, UpdatedAt: stateTestTime}
	items := legacyCampaignWorkItems([]byte(body.String()), "C-FRONTIER", state)
	if len(items) != 122 {
		t.Fatalf("frontier extraction silently capped or dropped rows: got %d want 122", len(items))
	}
	for _, item := range items[1:121] {
		if item.State == "done" {
			t.Fatalf("ambiguous prose was silently marked done: %+v", item)
		}
	}
	if items[len(items)-1].State != "done" {
		t.Fatalf("explicit completed table row was not preserved: %+v", items[len(items)-1])
	}
}

func TestMigratedTruthPreservesSupersessionScopeAndExclusions(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"), "# Retired fixture truth\n\n**Claim:** The retired fixture claim remains provenance only.\n\n**Confidence:** Moderate\n\n**Scope:** Windows fixture builds only.\n\n**Exclusions:** Linux behavior is not established.\n\n**Superseded-by:** docs/truth/replacement.md\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "replacement.md"), "# Replacement fixture truth\n\n**Claim:** The replacement fixture claim is current.\n\n**Confidence:** Strong\n")
	engine, plan, _ := migrationFixtureToPhysical(t, root)
	var row MigrationTruthPlan
	for _, candidate := range plan.TruthConversions {
		if candidate.SourcePath == "docs/truth/claim.md" {
			row = candidate
		}
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(row.Destination)))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(body, row.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if document.Record.Validity != "superseded" || document.Record.Projection != "none" ||
		!containsString(document.Record.AppliesWhen, "Windows fixture builds only.") ||
		!containsString(document.Record.KnownLimits, "Linux behavior is not established.") {
		t.Fatalf("truth metadata was expanded or lost: %+v", document.Record)
	}
	receiptPath := filepath.Join(root, ".re-discipline", "knowledge", "migration", "truth-receipts", row.FindingID+".json")
	var receipt MigrationTruthCompatibilityReceipt
	receiptBody, _ := os.ReadFile(receiptPath)
	if err := json.Unmarshal(receiptBody, &receipt); err != nil || receipt.LegacyStatus != "superseded" ||
		receipt.LegacyCorrection != "docs/truth/replacement.md" || len(receipt.LegacyScope) != 1 || len(receipt.LegacyExclusions) != 1 {
		t.Fatalf("truth receipt did not authenticate legacy metadata: %+v %v", receipt, err)
	}
	if err := engine.verifyMigratedTruthCompatibility(plan, mustMigrationStatus(t, engine), migrationSourceByPath(t, plan, "docs/truth/claim.md")); err != nil {
		t.Fatalf("truth compatibility verifier rejected preserved metadata: %v", err)
	}
}

func mustMigrationStatus(t *testing.T, engine *MigrationEngine) MigrationState {
	t.Helper()
	state, err := engine.Status()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestMigrationCreatesSyntheticProvenanceCampaignWithoutLegacyActiveTree(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Truth-only project\n\n<!-- re-discipline:shared-laws v0.7.0 -->\nlegacy laws\n<!-- re-discipline:shared-laws:end -->\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Truth-only claim\n\n**Claim:** A truth-only project still receives canonical migration provenance.\n\n**Confidence:** Strong\n")
	preview, err := PreviewMigration(root, nil)
	if err != nil || preview.Plan.Estimate.Campaigns != 1 || preview.Plan.Estimate.ProposedRuns != 1 {
		t.Fatalf("truth-only preview: %+v %v", preview.Plan.Estimate, err)
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shadow-indexed", "normalized", "physically-reorganized"} {
		state, err = engine.Resume(state.TransactionID, "manager", "cli")
		if err != nil || state.State != want {
			t.Fatalf("truth-only migration wanted %s: %+v %v", want, state, err)
		}
	}
	for _, path := range []string{
		"active/migration-provenance/campaign.json",
		"active/migration-provenance/runs/" + legacyRunID("migration-provenance", "campaign-import") + "/run.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("synthetic provenance object %s missing: %v", path, err)
		}
	}
}

func TestGateArtifactCannotSelfAttestWithOneGenericCheck(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, _, state := migrationFixtureToPhysical(t, root)
	artifact := MigrationGateArtifact{SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID,
		PlanDigest: state.PlanDigest, Gate: "retrieval-context", Passed: true,
		Checks:       []MigrationGateCheck{{Name: "looks-good", Passed: true, Evidence: "self assertion"}},
		Fingerprints: map[string]string{"benchmark": stateTestDigest("a"), "calibration": stateTestDigest("b"), "blinded-agent-evaluation": stateTestDigest("c")}}
	artifact.ResultDigest, _ = CanonicalDigest(artifact)
	body, _ := json.MarshalIndent(artifact, "", "  ")
	body = append(body, '\n')
	path := filepath.Join(root, "migration-tests", "self-attested.json")
	mustWriteFile(t, path, string(body))
	if _, err := engine.RecordGate(MigrationGateReceipt{Gate: artifact.Gate, Passed: true,
		Artifact: "migration-tests/self-attested.json", ArtifactDigest: "sha256:" + SHA256Bytes(body), Reviewer: "manager"}); err == nil ||
		!strings.Contains(err.Error(), "required measured check") {
		t.Fatalf("generic self-attested gate was accepted: %v", err)
	}
}
