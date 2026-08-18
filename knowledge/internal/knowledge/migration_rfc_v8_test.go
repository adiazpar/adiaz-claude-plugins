package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func migrationFixtureToNormalized(t *testing.T, root string) (*MigrationEngine, MigrationPlan, MigrationState) {
	t.Helper()
	migrationCertificationEvalCases(t, root)
	reviewFixtureTruthConflicts(t, root)
	commitMigrationFixture(t, root)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "shadow-indexed" {
		t.Fatalf("shadow-index migration fixture: %+v %v", state, err)
	}
	finding := 1
	for _, source := range preview.Plan.Sources {
		if source.Role != "legacy-run-report" || source.Campaign != "live-campaign" {
			continue
		}
		id := fmt.Sprintf("F-%04d", finding)
		input := migrationTestFinding(id)
		if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
			SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
			Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", id)},
			FindingIDs: []string{id}, Findings: []MigrationFindingInput{input},
			Reviewer: "manager", Rationale: "RFC 17.4 fixture coverage",
		}); err != nil {
			t.Fatal(err)
		}
		finding++
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize migration fixture: %+v %v", state, err)
	}
	return engine, preview.Plan, state
}

func migrationFixtureToPhysical(t *testing.T, root string) (*MigrationEngine, MigrationPlan, MigrationState) {
	t.Helper()
	engine, plan, state := migrationFixtureToNormalized(t, root)
	var err error
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate migration fixture: %+v %v", state, err)
	}
	return engine, plan, state
}

func migrationFixtureToTraversal(t *testing.T, root string) (*MigrationEngine, MigrationPlan, MigrationState) {
	t.Helper()
	engine, plan, state := migrationFixtureToPhysical(t, root)
	for _, gate := range []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"} {
		body := migrationGateArtifactBody(t, root, state, gate, true)
		relative := filepath.ToSlash(filepath.Join("migration-tests", gate+".json"))
		mustWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), string(body))
		if _, err := engine.RecordGate(MigrationGateReceipt{
			Gate: gate, Passed: true, Artifact: relative,
			ArtifactDigest: "sha256:" + SHA256Bytes(body), Reviewer: "manager",
		}); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "traversal-verified" {
		t.Fatalf("verify migration fixture: %+v %v", state, err)
	}
	return engine, plan, state
}

func TestMigratedFindingBodyCitesActivatedCanonicalReport(t *testing.T) {
	root := migrationPreviewFixture(t)
	_, _, _ = migrationFixtureToPhysical(t, root)
	findingPath := filepath.Join(root, "active", "live-campaign", "findings", "F-0001.md")
	body, err := os.ReadFile(findingPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(body, "active/live-campaign/findings/F-0001.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Record.Evidence) != 1 {
		t.Fatalf("expected one canonical evidence handle, got %+v", document.Record.Evidence)
	}
	canonicalReport := document.Record.Evidence[0].Path
	if !strings.Contains(document.Record.Body, "`"+canonicalReport+"`") {
		t.Fatalf("finding body does not cite its canonical evidence path %q: %s", canonicalReport, document.Record.Body)
	}
	if strings.Contains(document.Record.Body, "/subagents/") {
		t.Fatalf("finding body retained the legacy report location: %s", document.Record.Body)
	}
}

func TestMigrationAttestationNeverAutoRatifiesCompoundProse(t *testing.T) {
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
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "shadow-indexed" {
		t.Fatalf("shadow: %+v %v", state, err)
	}
	source := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	finding := migrationTestFinding("F-0001")
	// This intentionally adversarial prose contains independently challengeable
	// observation and inference. The runtime does not pretend an NLP heuristic
	// can disprove a curator's attestation; its fail-safe is provisional state
	// plus mandatory individual manager attention.
	finding.Claim = "The fixture measured one result, and that result proves the selected approach is optimal."
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", finding.ID)},
		FindingIDs: []string{finding.ID}, Findings: []MigrationFindingInput{finding},
		Reviewer: "curator:adversarial", Rationale: "prove semantic misattestation cannot auto-ratify",
	}); err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	stagedCampaign := filepath.Join(engine.migrationRoot(), "staging", "project", "active", "live-campaign")
	findingBody, err := os.ReadFile(filepath.Join(stagedCampaign, "findings", finding.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(findingBody, "active/live-campaign/findings/"+finding.ID+".md")
	if err != nil {
		t.Fatal(err)
	}
	if document.Record.ReviewState != "extracted" || document.Record.Validity != "provisional" ||
		document.Record.Projection != "none" {
		t.Fatalf("migration auto-ratified adversarial compound prose: %+v", document.Record)
	}
	intakeBody, err := os.ReadFile(filepath.Join(stagedCampaign, "intake", "I-0001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var intake IntakeRecord
	if err := decodeStrict(intakeBody, &intake); err != nil {
		t.Fatal(err)
	}
	if intake.Status != "submitted" || intake.Triage[finding.ID] != "attention" ||
		!strings.Contains(strings.Join(intake.RequestedDecisions, "\n"), "not semantic proof") {
		t.Fatalf("compound-risk candidate bypassed manager-attention gate: %+v", intake)
	}
	entries, err := os.ReadDir(filepath.Join(stagedCampaign, "reviews"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("migration synthesized a manager review: entries=%d err=%v", len(entries), err)
	}
}

func assertOrdinaryProjectPathsBlocked(t *testing.T, root, wantState string) {
	t.Helper()
	state, exists, err := projectMigrationState(root)
	if err != nil || !exists || state.State != wantState {
		t.Fatalf("migration guard state: %+v %v", state, err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), StateTransactionRequest{}); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("state mutation was not blocked in %s: %v", wantState, err)
	}
	if _, err := NewService(ServiceOptions{ProjectRoot: root, AssetRoot: t.TempDir()}); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("ordinary service read was not blocked in %s: %v", wantState, err)
	}
	server := &MCPServer{AssetRoot: t.TempDir(), configuredRoots: []string{root}}
	if _, err := server.service(root); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("MCP adapter did not enforce mixed-mode refusal in %s: %v", wantState, err)
	}
}

func TestIncompleteMigrationRefusesOrdinaryReadsAndV2WritesAtEveryStage(t *testing.T) {
	root := migrationPreviewFixture(t)
	migrationCertificationEvalCases(t, root)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryProjectPathsBlocked(t, root, "inventoried")
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryProjectPathsBlocked(t, root, "shadow-indexed")
	live := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: live.Path, SourceDigest: live.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, live, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")},
		Reviewer: "manager", Rationale: "mixed-mode guard fixture",
	}); err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryProjectPathsBlocked(t, root, "normalized")
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryProjectPathsBlocked(t, root, "physically-reorganized")
	for _, gate := range []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"} {
		body := migrationGateArtifactBody(t, root, state, gate, true)
		relative := filepath.ToSlash(filepath.Join("migration-tests", gate+".json"))
		mustWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), string(body))
		if _, err := engine.RecordGate(MigrationGateReceipt{Gate: gate, Passed: true, Artifact: relative,
			ArtifactDigest: "sha256:" + SHA256Bytes(body), Reviewer: "manager"}); err != nil {
			t.Fatal(err)
		}
	}
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryProjectPathsBlocked(t, root, "traversal-verified")
	status, err := EnsureProject(context.Background(), root, t.TempDir(), 100)
	if err != nil || status["system"].(map[string]any)["migrationState"] != "traversal-verified" {
		t.Fatalf("ensure did not report the incomplete transaction without repairs: %+v %v", status, err)
	}
}

func TestNormalizedMigrationRefusesAStaleLegacyWriterBeforeAnyRename(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, _, state := migrationFixtureToNormalized(t, root)
	report := filepath.Join(root, "active", "live-campaign", "subagents", "2026-07-28T06-09-49Z-codex-worker-a", "report.md")
	mustWriteFile(t, report, "# VERDICT\n\nDIRECT: a legacy writer changed this after normalization.\n")
	if _, err := engine.Resume(state.TransactionID, "manager", "cli"); err == nil || !strings.Contains(err.Error(), "approval is stale") {
		t.Fatalf("stale normalized plan was not refused: %v", err)
	}
	body, err := os.ReadFile(report)
	if err != nil || !strings.Contains(string(body), "legacy writer changed") {
		t.Fatalf("stale legacy bytes were overwritten: %v %s", err, body)
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "activation.json")); !os.IsNotExist(err) {
		t.Fatal("activation journal was created before the stale plan was refused")
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "backups")); !os.IsNotExist(err) {
		t.Fatal("backup renames began before the stale plan was refused")
	}
}

func TestActivationRechecksEachLaterTargetAndKeepsPriorTargetsRecoverable(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToNormalized(t, root)
	journalPath := filepath.Join(engine.migrationRoot(), "activation.json")
	journal, err := engine.loadOrPrepareActivation(plan, state, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	mutatedIndex := -1
	for index := len(journal.Targets) - 1; index > 0; index-- {
		if journal.Targets[index].Existed {
			mutatedIndex = index
			break
		}
	}
	if mutatedIndex < 1 {
		t.Fatal("fixture did not provide a later existing activation target")
	}
	restore := mutateMigrationTarget(t, root, journal.Targets[mutatedIndex].Path)
	if _, err := engine.advancePhysical(state, plan, "manager", "cli"); err == nil || !strings.Contains(err.Error(), "changed after journal preparation") {
		t.Fatalf("later target drift was not refused: %v", err)
	}
	var stopped migrationActivationJournal
	body, err := os.ReadFile(journalPath)
	if err != nil || decodeStrict(body, &stopped) != nil {
		t.Fatal(err)
	}
	drifted := stopped.Targets[mutatedIndex]
	if drifted.Phase != "pending" {
		t.Fatalf("drifted target advanced despite mismatch: %+v", drifted)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(drifted.BackupPath))); !os.IsNotExist(err) {
		t.Fatal("drifted target was renamed before its digest mismatch was reported")
	}
	priorRecoverable := false
	for index := 0; index < mutatedIndex; index++ {
		target := stopped.Targets[index]
		if target.Phase != "published" || !target.Existed {
			continue
		}
		digest, digestErr := digestMigrationPath(filepath.Join(root, filepath.FromSlash(target.BackupPath)))
		if digestErr == nil && digest == target.SourceDigest {
			priorRecoverable = true
			break
		}
	}
	if !priorRecoverable {
		t.Fatal("targets published before the later drift lacked digest-exact recovery backups")
	}
	restore()
	state, err = engine.advancePhysical(state, plan, "manager", "mcp")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activation did not resume after restoring the approved later target: %+v %v", state, err)
	}
}

func mutateMigrationTarget(t *testing.T, root, relative string) func() {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if err != nil {
		t.Fatal(err)
	}
	file := absolute
	if info.IsDir() {
		file = ""
		if err := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || file != "" {
				return walkErr
			}
			if entry.Type().IsRegular() {
				file = path
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if file == "" {
		t.Fatal("selected activation target has no regular file to mutate")
	}
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(append([]byte(nil), original...), []byte("\nlegacy-writer-drift\n")...), mode.Mode()); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.WriteFile(file, original, mode.Mode()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationPreservesMissingLaunchProvenanceReviewConflictAndOverlappingRoles(t *testing.T) {
	root := migrationPreviewFixture(t)
	workspace := filepath.Join(root, "active", "live-campaign", "subagents", "2026-07-28T06-09-49Z-codex-worker-a")
	mustWriteFile(t, filepath.Join(workspace, "evidence", "same.txt"), "evidence bytes\n")
	mustWriteFile(t, filepath.Join(workspace, "artifacts", "same.txt"), "artifact bytes\n")
	ledger := filepath.Join(root, "active", "live-campaign", "REVIEWS.md")
	mustWriteFile(t, ledger, "# Review ledger\n\n| Date | Report | PROMOTE | HOLD | DROP | BLOCK | Promoted to |\n|---|---|---:|---:|---:|---:|---|\n| 2026-07-28 | `subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md` | 1 | 0 | 0 | 0 | `docs/truth/conflict.md` |\n")
	_, _, _ = migrationFixtureToPhysical(t, root)
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := store.LoadCampaignGraph("live-campaign")
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Reviews) != 0 {
		t.Fatal("conflicting aggregate ledger/report stamps were synthesized into manager review decisions")
	}
	run := live.Runs[legacyRunID("live-campaign", "2026-07-28T06-09-49Z-codex-worker-a")]
	wantFiles := map[string]bool{"payload/legacy/evidence/same.txt": false, "payload/legacy/artifacts/same.txt": false}
	for _, file := range run.Files {
		if _, ok := wantFiles[file.Path]; ok {
			wantFiles[file.Path] = true
		}
	}
	for path, found := range wantFiles {
		if !found {
			t.Fatalf("overlapping legacy role was not independently retained: %s", path)
		}
	}
	closed, err := store.LoadCampaignGraph("closed-campaign")
	if err != nil {
		t.Fatal(err)
	}
	missing := closed.Runs[legacyRunID("closed-campaign", "worker-b")]
	if missing.Brief == nil || missing.ContextPack == nil {
		t.Fatal("missing legacy brief/context pack did not receive explicit migration-provenance handles")
	}
	for _, handle := range []*FileHandle{missing.Brief, missing.ContextPack} {
		if err := verifyMigrationFileHandle(root, *handle); err != nil {
			t.Fatal(err)
		}
	}
	importPath := filepath.Join(root, ".re-discipline", "knowledge", "migration",
		"review-imports", "live-campaign.json")
	importBody, err := os.ReadFile(importPath)
	if err != nil || !strings.Contains(string(importBody), `"status": "proposed-import"`) || !strings.Contains(string(importBody), `"promote": 1`) {
		t.Fatalf("aggregate ledger provenance was not retained as a non-decision proposal: %v %s", err, importBody)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "live-campaign", "runs",
		legacyRunID("live-campaign", "campaign-import"), "payload", "legacy", "review-import.json")); !os.IsNotExist(err) {
		t.Fatalf("ledger import must not ride a synthetic run payload: %v", err)
	}
}

func TestMigrationReportsUnsupportedLegacyRetrievalProfileWithoutActivatingIt(t *testing.T) {
	root := migrationPreviewFixture(t)
	legacy := `{"schemaVersion":0,"profileId":"project:legacy-dense","effectiveProfiles":[{"lanes":["exact","fts","graph","dense"],"requires":{"embedding":"legacy-model"}}]}` + "\n"
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json"), legacy)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	unsupported := false
	for _, conflict := range preview.Plan.Conflicts {
		unsupported = unsupported || conflict.Code == "unsupported-retrieval-profile" && conflict.Blocks
	}
	if !unsupported || !strings.Contains(strings.Join(preview.Plan.ProfileChanges, "\n"), "compatibility=unsupported") {
		t.Fatalf("unsupported accepted profile was silently treated as the 0.8 baseline: %+v %+v", preview.Plan.Conflicts, preview.Plan.ProfileChanges)
	}
	source := migrationSourceByPath(t, preview.Plan, ".re-discipline/knowledge/retrieval-profile.json")
	if source.Role != "legacy-retrieval-profile" || source.Destination != ".re-discipline/knowledge/migration/legacy-retrieval-profile.json" {
		t.Fatalf("legacy profile was not assigned a provenance-only destination: %+v", source)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("unsupported accepted profile was allowed to begin activation")
	}
	if body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")); err != nil || string(body) != legacy {
		t.Fatalf("blocked preview changed the unsupported accepted profile: %v", err)
	}
}

func TestMigrationGlobalGenesisAndRatificationRecoverAfterHeadPublication(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToTraversal(t, root)
	fired := false
	engine.StateFailpoint = func(point StateFailpoint) error {
		if !fired && point.Name == FailAfterHeadPublish {
			fired = true
			return errors.New("fixture crash after migration head publication")
		}
		return nil
	}
	if _, err := engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli"); err == nil {
		t.Fatal("ratification failpoint did not interrupt the final normal transaction")
	}
	interrupted, err := engine.Status()
	if err != nil || interrupted.State != "traversal-verified" {
		t.Fatalf("interrupted ratification changed the durable migration stage: %+v %v", interrupted, err)
	}
	engine.StateFailpoint = nil
	state, err = engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "mcp")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratification did not recover idempotently: %+v %v", state, err)
	}
	var manifest MigrationRatificationManifest
	body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "migration", "0.8", "certification.json"))
	if err != nil || json.Unmarshal(body, &manifest) != nil {
		t.Fatal(err)
	}
	campaigns := migrationCampaigns(plan)
	if manifest.ImportHead.Revision != int64(len(campaigns)) || manifest.SnapshotObjects == 0 || len(manifest.ManagedTargets) < 2 {
		t.Fatalf("genesis did not bind every activated campaign and managed target: %+v", manifest)
	}
	var prior StateEvent
	for index, campaign := range campaigns {
		eventsBody, err := os.ReadFile(filepath.Join(root, "active", campaign, "events", "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(eventsBody)), "\n")
		var imported StateEvent
		if err := json.Unmarshal([]byte(lines[0]), &imported); err != nil {
			t.Fatal(err)
		}
		if index > 0 && (imported.PreviousEventID != prior.ID || imported.PreviousStateDigest != prior.ResultingStateDigest) {
			t.Fatal("campaign import events do not form one project-wide genesis chain")
		}
		prior = imported
	}
	if prior.ID != manifest.ImportHead.EventID || prior.Digest != manifest.ImportHead.EventDigest {
		t.Fatal("ratification manifest head does not reach the complete import chain")
	}
}

func TestMigratedGlobalHeadAcceptsOrdinaryTransactionOnNonHeadCampaign(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, plan, state := migrationFixtureToTraversal(t, root)
	state, err := engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify migration fixture: %+v %v", state, err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := migrationCampaigns(plan)
	if len(campaigns) < 2 {
		t.Fatal("fixture requires a non-head campaign")
	}
	target := campaigns[0]
	graph, err := store.LoadCampaignGraph(target)
	if err != nil || graph.Campaign == nil {
		t.Fatal(err)
	}
	workID := ""
	for id := range graph.WorkItems {
		if workID == "" || id < workID {
			workID = id
		}
	}
	if workID == "" {
		t.Fatal("migrated campaign contains no work item")
	}
	work := graph.WorkItems[workID]
	previousWork := work
	work.Revision++
	work.UpdatedAt, work.UpdatedBy, work.CorrelationID, work.Digest = state.UpdatedAt, "manager", "corr-post-migration", ""
	work.ResumeNote = "ordinary post-migration transaction proved the global head remains writable"
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: target, CampaignID: graph.Campaign.ID,
		Actor: "manager", Authority: "manager", Action: "work.update",
		CorrelationID: "corr-post-migration", IdempotencyKey: "post-migration-non-head-campaign",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{{
			Path:             "active/" + target + "/work-items/" + workID + ".json",
			ExpectedRevision: previousWork.Revision, ExpectedDigest: previousWork.Digest, Record: work,
		}},
	})
	if err != nil {
		t.Fatalf("ordinary transaction after migration was rejected: %v", err)
	}
	if receipt.PreviousHead.Digest != head.Digest || receipt.ResultingHead.Revision != head.Revision+1 ||
		receipt.Event.PreviousEventID != head.EventID || receipt.Event.PreviousStateDigest != head.StateDigest {
		t.Fatalf("ordinary transaction did not extend the project-global migration head: %+v", receipt)
	}
	loaded, err := store.LoadCampaignGraph(target)
	if err != nil || loaded.WorkItems[workID].ResumeNote != work.ResumeNote {
		t.Fatalf("post-migration record was not durably published: %v %+v", err, loaded.WorkItems[workID])
	}
}

func TestHugeUnstampedShadowInventoryStillCutsOverTheWholeManagedProject(t *testing.T) {
	root := migrationPreviewFixture(t)
	master := filepath.Join(root, "active", "closed-campaign", "CAMPAIGN.md")
	mustWriteFile(t, master, "# Campaign: closed-campaign\n\n## Historical inventory\n\n"+strings.Repeat("- unstamped historical observation remains provenance.\n", 4096))
	for index := 0; index < 64; index++ {
		mustWriteFile(t, filepath.Join(root, "active", "closed-campaign", "subagents", fmt.Sprintf("unstamped-%03d", index), "report.md"),
			fmt.Sprintf("# VERDICT\n\nDIRECT: unstamped historical observation %d.\n", index))
	}
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.Estimate.LegacyReports != 66 || len(preview.Plan.Unresolved) != 1 {
		t.Fatalf("large unstamped corpus was misclassified: estimate=%+v unresolved=%+v", preview.Plan.Estimate, preview.Plan.Unresolved)
	}
	_, _, _ = migrationFixtureToPhysical(t, root)
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := store.LoadCampaignGraph("closed-campaign")
	if err != nil || len(closed.Runs) < 65 {
		t.Fatalf("project-wide activation omitted non-frontier historical runs: runs=%d err=%v", len(closed.Runs), err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "history", "chronicle.md")); err != nil {
		t.Fatalf("frontier-only activation discarded durable history: %v", err)
	}
}

func TestLegacyTruthBecomesQueryableTypedTruthWithCompatibilityReceipt(t *testing.T) {
	root := migrationPreviewFixture(t)
	legacyPath := filepath.Join(root, "docs", "truth", "claim.md")
	legacyBody := "# Prefab cache contract\n\n**Claim:** Prefab cache entries remain stable across a bounded reload.\n\n**Confidence:** Strong\n\n**Validity:**\n- Verified: 2026-07-20\n\n## Evidence\n\nThe accepted project observation was recorded before 0.8.\n"
	mustWriteFile(t, legacyPath, legacyBody)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "INDEX.md"), "# Truth navigation\n\n- [Prefab cache contract](claim.md)\n")
	engine, plan, state := migrationFixtureToTraversal(t, root)
	state, err := engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify truth fixture: %+v %v", state, err)
	}
	source := migrationSourceByPath(t, plan, "docs/truth/claim.md")
	if source.Destination == source.Path || !strings.HasPrefix(source.Destination, "docs/truth/findings/F-") {
		t.Fatalf("legacy truth was not assigned a canonical typed destination: %+v", source)
	}
	destinationBody, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source.Destination)))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(destinationBody, source.Destination)
	if err != nil || document.Record.Claim != "Prefab cache entries remain stable across a bounded reload." ||
		document.Record.Projection != "truth" || document.Record.Validity != "current" {
		t.Fatalf("converted truth is not canonical or semantically preserved: %+v %v", document.Record, err)
	}
	receiptPath := filepath.Join(root, ".re-discipline", "knowledge", "migration", "truth-receipts", document.Record.ID+".json")
	var receipt MigrationTruthCompatibilityReceipt
	receiptBody, err := os.ReadFile(receiptPath)
	if err != nil || json.Unmarshal(receiptBody, &receipt) != nil {
		t.Fatal(err)
	}
	if !receipt.SemanticPreserved || !receipt.EvidenceReachable || !receipt.ScopeNotExpanded ||
		!receipt.DependenciesResolve || !receipt.SearchCompatibility || receipt.SourceReportRatificationImported {
		t.Fatalf("truth receipt did not prove the no-expansion/no-fabrication contract: %+v", receipt)
	}
	if receipt.ManagerReviewBasis != "approved-migration-plan:"+plan.PlanDigest || receipt.ApprovedQuestionsDigest == "" ||
		receipt.LegacyConfidence != "Strong" {
		t.Fatalf("generated retrieval questions/confidence were not bound to exact manager plan approval: %+v", receipt)
	}
	indexBody, err := os.ReadFile(filepath.Join(root, "docs", "truth", "INDEX.md"))
	if err != nil || strings.Contains(string(indexBody), "(claim.md)") || !strings.Contains(string(indexBody), "findings/") {
		t.Fatalf("truth support navigation was removed or retained a stale link: %v %s", err, indexBody)
	}
	provenanceRevision, provenancePath, err := parseMigrationGitRef(receipt.ProvenancePath)
	if err != nil || provenancePath != "docs/truth/claim.md" || provenanceRevision != plan.SourceRevision {
		t.Fatalf("truth receipt does not cite the approved archived provenance: %q %v", receipt.ProvenancePath, err)
	}
	provenance, err := migrationGitBlob(root, provenanceRevision, provenancePath)
	if err != nil || string(provenance) != legacyBody {
		t.Fatalf("legacy truth provenance was not byte-preserved in the archive: %v", err)
	}
	service, err := NewService(ServiceOptions{ProjectRoot: root, AssetRoot: adversarialAssetRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Query(context.Background(), FindingQueryOptions{
		Query: "What remains stable across a bounded prefab reload?", Limit: 4, TokenBudget: 1200,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, card := range result.Cards {
		if card.ID == document.Record.ID && card.SourceClass == "truth" {
			found = true
		}
	}
	if !found {
		t.Fatalf("public finding-card query could not retrieve migrated truth: %+v", result.Cards)
	}
}

func TestLegacyTruthRelativeDependenciesBecomeTypedRelationsAndRewrittenLinks(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Primary contract\n\n**Claim:** The primary contract depends on the peer contract.\n\n**Confidence:** Strong\n\n## Depends-on\n\n- [Peer contract](peer.md)\n- [Historical context](../history/chronicle.md)\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "peer.md"),
		"# Peer contract\n\n**Claim:** The peer contract remains independently addressable.\n\n**Confidence:** Strong\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "INDEX.md"),
		"# Truth navigation\n\n- [Primary](claim.md)\n- [Peer](peer.md)\n")
	_, plan, _ := migrationFixtureToTraversal(t, root)
	primary := migrationSourceByPath(t, plan, "docs/truth/claim.md")
	peer := migrationSourceByPath(t, plan, "docs/truth/peer.md")
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(primary.Destination)))
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseFindingDocument(body, primary.Destination)
	if err != nil {
		t.Fatal(err)
	}
	peerID := stableLegacyTruthFindingID(peer.Path)
	if !reflect.DeepEqual(document.Record.Relations.DependsOn, []string{peerID}) {
		t.Fatalf("relative truth dependency was not converted to a typed relation: %+v", document.Record.Relations)
	}
	var receipt MigrationTruthCompatibilityReceipt
	receiptBody, err := os.ReadFile(filepath.Join(root, ".re-discipline", "knowledge", "migration", "truth-receipts", document.Record.ID+".json"))
	if err != nil || json.Unmarshal(receiptBody, &receipt) != nil {
		t.Fatal(err)
	}
	wantDependencies := map[string]string{
		"docs/history/chronicle.md": "docs/history/chronicle.md",
		"docs/truth/peer.md":        peer.Destination,
	}
	if !reflect.DeepEqual(receipt.DependencyMap, wantDependencies) {
		t.Fatalf("truth compatibility receipt lost relative dependencies: got=%+v want=%+v", receipt.DependencyMap, wantDependencies)
	}
	indexBody, err := os.ReadFile(filepath.Join(root, "docs", "truth", "INDEX.md"))
	if err != nil || strings.Contains(string(indexBody), "(claim.md)") || strings.Contains(string(indexBody), "(peer.md)") {
		t.Fatalf("truth navigation retained a stale relative path: %v %s", err, indexBody)
	}
}

func TestTruthCompatibilityVerificationRecomputesReceiptAssertions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*MigrationTruthCompatibilityReceipt)
	}{
		{name: "search terms", mutate: func(receipt *MigrationTruthCompatibilityReceipt) {
			receipt.SearchTerms = []string{"unrelated migration assertion"}
		}},
		{name: "dependency graph", mutate: func(receipt *MigrationTruthCompatibilityReceipt) {
			receipt.DependencyMap = map[string]string{"docs/truth/phantom.md": "docs/truth/findings/F-9999.md"}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := migrationPreviewFixture(t)
			engine, plan, state := migrationFixtureToPhysical(t, root)
			source := migrationSourceByPath(t, plan, "docs/truth/claim.md")
			id := stableLegacyTruthFindingID(source.Path)
			path := filepath.Join(root, ".re-discipline", "knowledge", "migration", "truth-receipts", id+".json")
			var receipt MigrationTruthCompatibilityReceipt
			body, err := os.ReadFile(path)
			if err != nil || decodeStrictJSON(body, &receipt) != nil {
				t.Fatal(err)
			}
			testCase.mutate(&receipt)
			receipt.Digest = ""
			receipt.Digest, err = CanonicalDigest(receipt)
			if err != nil {
				t.Fatal(err)
			}
			if err := AtomicWriteJSON(path, receipt, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := engine.verifyMigratedTruthCompatibility(plan, state, source); err == nil {
				t.Fatal("self-consistent but semantically false truth receipt was accepted")
			}
		})
	}
}

func TestNormalizedMigrationRefusesStaleLegacyTruthBeforeActivation(t *testing.T) {
	root := migrationPreviewFixture(t)
	engine, _, state := migrationFixtureToNormalized(t, root)
	truth := filepath.Join(root, "docs", "truth", "claim.md")
	original, err := os.ReadFile(truth)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, truth, string(original)+"\npost-approval truth drift\n")
	if _, err := engine.Resume(state.TransactionID, "manager", "cli"); err == nil || !strings.Contains(err.Error(), "approval is stale") {
		t.Fatalf("stale legacy truth was not refused before activation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "activation.json")); !os.IsNotExist(err) {
		t.Fatal("activation journal was created before stale truth was refused")
	}
}

func TestMultiClaimLegacyTruthBlocksUnreviewedAutomaticSplit(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Compound truth\n\n**Claim:** First independent claim.\n\n**Claim:** Second independent claim.\n")
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, conflict := range preview.Plan.Conflicts {
		blocked = blocked || conflict.Code == "truth-multi-claim" && conflict.Blocks
	}
	if !blocked {
		t.Fatalf("unreviewed multi-claim truth was not blocked: %+v", preview.Plan.Conflicts)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli"); err == nil {
		t.Fatal("migration started despite unresolved multi-claim truth coverage")
	}
}

func TestImplicitOrEmptyLegacyTruthClaimBlocksFallbackInference(t *testing.T) {
	for _, body := range []string{
		"# Empty truth\n\nOperational notes only.\n",
		"# Multi-section notes\n\n## Behavior A\nOne fact.\n\n## Behavior B\nAnother fact.\n",
	} {
		root := migrationPreviewFixture(t)
		mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"), body)
		preview, err := PreviewMigration(root, []string{"live-campaign"})
		if err != nil {
			t.Fatal(err)
		}
		blocked := false
		for _, conflict := range preview.Plan.Conflicts {
			blocked = blocked || conflict.Code == "truth-claim-boundary-unproven" && conflict.Blocks
		}
		if !blocked || len(preview.Plan.TruthConversions) != 0 {
			t.Fatalf("implicit truth claim was inferred instead of blocked: conflicts=%+v conversions=%+v", preview.Plan.Conflicts, preview.Plan.TruthConversions)
		}
	}
}

func TestManagerCanResolveImplicitLegacyTruthWithoutInferredClaim(t *testing.T) {
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Implicit cache rule\n\nCache keys remain stable during a bounded reload.\n")
	commitMigrationFixture(t, root)
	packet, err := ExportMigrationTruthConflicts(root)
	if err != nil || len(packet.Conflicts) != 1 || packet.Conflicts[0].SourceCoverageText == "" {
		t.Fatalf("implicit truth conflict packet: %+v %v", packet, err)
	}
	conflict := packet.Conflicts[0]
	review, err := SubmitMigrationTruthReview(root, MigrationTruthReviewSubmission{
		SchemaVersion: 1, PacketDigest: packet.Digest, SourcePath: conflict.SourcePath,
		SourceDigest: conflict.SourceDigest, Reviewer: "manager", Rationale: "Make the accepted implicit rule explicit.",
		Claims: []MigrationTruthAtomicClaim{{
			SourceText: conflict.SourceCoverageText, Title: "Implicit cache rule",
			Claim:              "Cache keys remain stable during a bounded reload.",
			SyntheticQuestions: []string{"What remains stable during reload?", "Which implicit cache rule is accepted?", "How do cache keys behave during reload?"},
		}},
	}, "")
	if err != nil || review.Digest == "" {
		t.Fatalf("review implicit truth: %+v %v", review, err)
	}
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil || countBlockingConflicts(preview.Plan.Conflicts) != 0 || len(preview.Plan.TruthConversions) != 1 ||
		preview.Plan.TruthConversions[0].Claim != "Cache keys remain stable during a bounded reload." {
		t.Fatalf("implicit truth review was not consumed without inference: %+v %v", preview.Plan, err)
	}
}

func TestOversizedLegacyTruthClaimBlocksSilentTruncation(t *testing.T) {
	root := migrationPreviewFixture(t)
	claim := strings.Repeat("bounded semantic clause ", 30)
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Oversized truth\n\n**Claim:** "+claim+"\n")
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, conflict := range preview.Plan.Conflicts {
		blocked = blocked || conflict.Code == "truth-claim-not-atomic" && conflict.Blocks
	}
	if !blocked || len(preview.Plan.TruthConversions) != 0 {
		t.Fatalf("oversized claim was truncated instead of requiring reviewed atomicization: conflicts=%+v conversions=%+v", preview.Plan.Conflicts, preview.Plan.TruthConversions)
	}
}

func TestReviewedLongTruthSplitRegeneratesPlanAndMigratesEndToEnd(t *testing.T) {
	root := migrationPreviewFixture(t)
	firstSource := strings.TrimSpace(strings.Repeat("alpha invariant detail remains source-bound ", 12))
	secondSource := strings.TrimSpace(strings.Repeat("beta boundary detail remains independently reviewable ", 12))
	legacyClaim := firstSource + " " + secondSource
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"),
		"# Compound cache contract\n\n**Claim:** "+legacyClaim+"\n\n**Confidence:** Strong\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "INDEX.md"),
		"# Truth navigation\n\n- [Compound cache contract](claim.md)\n")
	commitMigrationFixture(t, root)

	blocked, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if countBlockingConflicts(blocked.Plan.Conflicts) == 0 || len(blocked.Plan.TruthConversions) != 0 {
		t.Fatalf("long truth did not require reviewed atomicization: %+v", blocked.Plan.Conflicts)
	}
	packet, err := ExportMigrationTruthConflicts(root)
	if err != nil || len(packet.Conflicts) != 1 || packet.Conflicts[0].SourceDigest == "" {
		t.Fatalf("export truth conflict packet: %+v %v", packet, err)
	}
	review, err := SubmitMigrationTruthReview(root, MigrationTruthReviewSubmission{
		SchemaVersion: MigrationSchemaVersion, PacketDigest: packet.Digest,
		SourcePath: "docs/truth/claim.md", SourceDigest: packet.Conflicts[0].SourceDigest,
		Reviewer: "manager", Rationale: "The source contains two independently supersedable assertions.",
		Claims: []MigrationTruthAtomicClaim{
			{SourceText: firstSource, Title: "Alpha cache invariant", Claim: "The alpha cache invariant remains stable.",
				SyntheticQuestions: []string{"What remains stable for alpha cache entries?", "Which alpha invariant is accepted?", "How does the alpha cache behave?"}},
			{SourceText: secondSource, Title: "Beta cache boundary", Claim: "The beta cache boundary remains independently reviewable.",
				SyntheticQuestions: []string{"What beta boundary is accepted?", "Which cache boundary is independently reviewable?", "How is the beta cache boundary treated?"}},
		},
	}, "")
	if err != nil || review.Digest == "" {
		t.Fatalf("submit reviewed split: %+v %v", review, err)
	}
	resolved, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Plan.PlanDigest == blocked.Plan.PlanDigest || countBlockingConflicts(resolved.Plan.Conflicts) != 0 ||
		len(resolved.Plan.TruthConversions) != 2 {
		t.Fatalf("review did not regenerate a clean split-bound plan: %+v", resolved.Plan)
	}
	for index, row := range resolved.Plan.TruthConversions {
		if row.ReviewDigest != review.Digest || row.SplitIndex != index+1 || row.SplitCount != 2 || row.SourceText == "" {
			t.Fatalf("split row is not bound to reviewed coverage: %+v", row)
		}
	}
	source := migrationSourceByPath(t, resolved.Plan, "docs/truth/claim.md")
	if !strings.HasPrefix(source.Destination, "docs/truth/splits/") {
		t.Fatalf("legacy split link has no exhaustive navigation destination: %+v", source)
	}

	engine, plan, state := migrationFixtureToTraversal(t, root)
	state, err = engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify reviewed truth split: %+v %v", state, err)
	}
	rows := []MigrationTruthPlan{}
	for _, row := range plan.TruthConversions {
		if row.SourcePath == source.Path {
			rows = append(rows, row)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("ratified plan lost reviewed split rows: %+v", rows)
	}
	manifest, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source.Destination)))
	if err != nil || !strings.Contains(string(manifest), rows[0].FindingID) || !strings.Contains(string(manifest), rows[1].FindingID) {
		t.Fatalf("split navigation does not reach every atomic finding: %v %s", err, manifest)
	}
	index, err := os.ReadFile(filepath.Join(root, "docs", "truth", "INDEX.md"))
	if err != nil || strings.Contains(string(index), "(claim.md)") || !strings.Contains(string(index), "splits/") {
		t.Fatalf("legacy truth link was not rewritten to the split manifest: %v %s", err, index)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(migrationTruthAuditReviewPath(source.Path)))); err != nil {
		t.Fatalf("sealed atomicization review was not retained in canonical audit state: %v", err)
	}
	service, err := NewService(ServiceOptions{ProjectRoot: root, AssetRoot: adversarialAssetRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		result, queryErr := service.Query(context.Background(), FindingQueryOptions{Query: row.Claim, Limit: 5, TokenBudget: 1200})
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		found := false
		for _, card := range result.Cards {
			found = found || card.ID == row.FindingID && card.SourceClass == "truth"
		}
		if !found {
			t.Fatalf("reviewed split %s was not retrievable after ratification: %+v", row.FindingID, result.Cards)
		}
	}
}
