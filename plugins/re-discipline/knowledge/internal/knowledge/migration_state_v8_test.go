package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedMigrationEngine(t *testing.T, root string) *MigrationEngine {
	t.Helper()
	engine, err := NewMigrationEngine(root)
	if err != nil {
		t.Fatal(err)
	}
	engine.AssetRoot = adversarialAssetRoot(t)
	engine.Now = func() time.Time {
		return time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	}
	return engine
}

func fullMigrationCoverage(t *testing.T, root string, source MigrationSource, disposition, targetID string) CoverageEntry {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source.Path)))
	if err != nil {
		t.Fatal(err)
	}
	lineCount, err := migrationReportLineCount(body)
	if err != nil {
		t.Fatal(err)
	}
	entry := CoverageEntry{
		SourcePath: source.Destination, SourceSHA256: "sha256:" + source.SHA256,
		StartLine: 1, EndLine: lineCount, SourceLineCount: lineCount,
		Disposition: disposition, TargetID: targetID,
	}
	if disposition == "candidate-finding" {
		entry.Rationale = "The fixture treats the complete source span as one bounded candidate for contract testing."
	}
	entry.SourceHandle = canonicalCoverageHandle(entry)
	return entry
}

func TestMigrationStateMachineRequiresApprovalCoverageGatesAndRatification(t *testing.T) {
	root := migrationPreviewFixture(t)
	// Retrieval certification must measure the corpus that was present when the
	// migration was approved. Seed the fixture's eval corpus before preview so
	// the post-activation benchmark cannot mutate a receipt-bound managed target.
	migrationCertificationEvalCases(t, root)
	preview, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatal(err)
	}
	engine := fixedMigrationEngine(t, root)
	if _, err := engine.Start(preview.Plan, "sha256:"+strings.Repeat("0", 64), "manager", "cli"); err == nil {
		t.Fatal("an unapproved digest must not start migration")
	}
	if _, err := os.Stat(engine.statePath()); !os.IsNotExist(err) {
		t.Fatal("failed approval must not create canonical migration state")
	}
	state, err := engine.Start(preview.Plan, preview.Plan.PlanDigest, "manager", "cli")
	if err != nil || state.State != "inventoried" {
		t.Fatalf("start: %+v %v", state, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "shadow-indexed" {
		t.Fatalf("shadow: %+v %v", state, err)
	}
	shadow, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "shadow-catalog.json"))
	if err != nil || bytesCount(shadow, []byte("unnormalized provenance")) != 2 {
		t.Fatalf("every report must remain shadow provenance: %v %s", err, shadow)
	}
	shadowResult, err := engine.QueryShadow("bounded migration behavior", "live-campaign", 5)
	if err != nil || len(shadowResult.Matches) != 1 || shadowResult.Matches[0].Label != "unnormalized provenance" || shadowResult.CatalogDigest == "" {
		t.Fatalf("digest-bound shadow provenance was not queryable: %+v %v", shadowResult, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "shadow-indexed" || len(state.Blockers) != 1 {
		t.Fatalf("live coverage gate: %+v %v", state, err)
	}
	liveReport := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	coverage, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: liveReport.Path, SourceDigest: liveReport.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, liveReport, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")}, Reviewer: "manager",
		Rationale: "fixture coverage",
	})
	if err != nil || coverage.Digest == "" {
		t.Fatalf("coverage: %+v %v", coverage, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate: %+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "live-campaign", "campaign.json")); err != nil {
		t.Fatalf("canonical campaign missing after activation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "active", "live-campaign", "CAMPAIGN.md")); !os.IsNotExist(err) {
		t.Fatal("legacy masterfile remained an operational input")
	}
	activeBackup := strings.TrimPrefix(SHA256String("active"), "sha256:")[:16] + "-active"
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "backups", activeBackup, "live-campaign", "CAMPAIGN.md")); err != nil {
		t.Fatalf("recoverable legacy backup missing: %v", err)
	}
	var activation migrationActivationJournal
	activationBody, err := os.ReadFile(filepath.Join(engine.migrationRoot(), "activation.json"))
	if err != nil || decodeStrict(activationBody, &activation) != nil {
		t.Fatalf("read activation journal: %v", err)
	}
	for _, target := range activation.Targets {
		if target.Phase != "published" {
			t.Fatalf("managed target %s did not publish: %+v", target.Path, target)
		}
		if target.Existed {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target.BackupPath))); err != nil {
				t.Fatalf("managed target %s lacks backup: %v", target.Path, err)
			}
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "config.json")); err != nil || !strings.Contains(string(body), `"schemaVersion": 3`) {
		t.Fatalf("bootstrap configuration was not cut over: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "project-profile.md")); err != nil || !strings.Contains(string(body), "shared-laws v0.8.0") {
		t.Fatalf("project profile was not cut over: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".codex", "external-drafter-contract.md")); err != nil ||
		strings.Contains(string(body), "/subagents/") || strings.Contains(string(body), "CAMPAIGN.md") ||
		!strings.Contains(string(body), "active/<campaign>/runs/<run-id>/") ||
		!strings.Contains(string(body), "Preserve fixture-specific evidence rigor") {
		t.Fatalf("external drafter contract did not cut over while preserving project guidance: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "agents", "dispatch.ps1")); err != nil ||
		!strings.Contains(string(body), "existing validated re-discipline run") ||
		!strings.Contains(string(body), "campaign.json") || strings.Contains(string(body), "subagents") {
		t.Fatalf("external provider dispatcher did not cut over to registered 0.8 runs: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, "docs", "product-guide.md")); err != nil || !strings.Contains(string(body), "Unrelated product") {
		t.Fatalf("unrelated docs were not preserved: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, "docs", "INDEX.md")); err != nil ||
		strings.Contains(string(body), "CAMPAIGN.md") || !strings.Contains(string(body), "../active/live-campaign/STATE.md") ||
		!strings.Contains(string(body), "Project-owned navigation note") {
		t.Fatalf("front-door navigation retained removed masterfile links or lost project notes: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json")); err != nil || !strings.Contains(string(body), "projectOwned") {
		t.Fatalf("host-local settings were not preserved: %v %s", err, body)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".re-discipline", "memory", "topics", "working-style.md")); err != nil ||
		string(body) != "# Retained shared memory\n" {
		t.Fatalf("accepted shared memory was not preserved byte-for-byte: %v %s", err, body)
	}
	graph, err := NewStateStoreWithBoundaryMust(root).LoadCampaignGraph("live-campaign")
	if err != nil || len(graph.WorkItems) < 4 || graph.Findings["F-0001"].ID == "" || len(graph.Reviews) != 0 || len(graph.Intakes) != 1 {
		t.Fatalf("migration did not reconstruct frontier and provisional knowledge without inventing a review: work=%d findings=%d intakes=%d reviews=%d err=%v", len(graph.WorkItems), len(graph.Findings), len(graph.Intakes), len(graph.Reviews), err)
	}
	for _, intake := range graph.Intakes {
		if intake.Status != "submitted" || !strings.Contains(strings.Join(intake.RequestedDecisions, "\n"), "fresh measured 0.8 manager decision") {
			t.Fatalf("legacy review metadata must remain provenance pending real 0.8 review: %+v", intake)
		}
	}
	certification, err := engine.Verify()
	if err != nil || certification.Candidate || len(certification.Blockers) != 4 {
		t.Fatalf("certification must await all four gates: %+v %v", certification, err)
	}
	tamperedPath := filepath.Join(root, "migration-tests", "tampered.json")
	mustWriteFile(t, tamperedPath, "{\"passed\":false}\n")
	if _, err := engine.RecordGate(MigrationGateReceipt{Gate: "structural", Passed: true,
		Artifact: "migration-tests/tampered.json", ArtifactDigest: "sha256:" + SHA256String("different"), Reviewer: "manager"}); err == nil {
		t.Fatal("gate accepted a self-attested digest that did not match its artifact bytes")
	}
	gateBodies := map[string][]byte{}
	for _, gate := range []string{"structural", "semantic-traversal", "retrieval-context", "host-parity"} {
		artifactPath := filepath.Join(root, "migration-tests", gate+".json")
		artifactBody := migrationGateArtifactBody(t, root, state, gate, true)
		gateBodies[gate] = artifactBody
		mustWriteFile(t, artifactPath, string(artifactBody))
		_, err := engine.RecordGate(MigrationGateReceipt{
			Gate: gate, Passed: true, Artifact: "migration-tests/" + gate + ".json",
			ArtifactDigest: "sha256:" + SHA256Bytes(artifactBody), Reviewer: "manager",
		})
		if err != nil {
			t.Fatalf("record %s gate: %v", gate, err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "migration-tests", "host-parity.json"), "{\"tampered\":true}\n")
	if stale, err := engine.Verify(); err != nil || stale.Candidate ||
		!strings.Contains(strings.Join(stale.Blockers, "\n"), "host-parity: gate artifact digest") {
		t.Fatalf("certification trusted a gate artifact changed after receipt: %+v %v", stale, err)
	}
	mustWriteFile(t, filepath.Join(root, "migration-tests", "host-parity.json"), string(gateBodies["host-parity"]))
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "traversal-verified" || state.CertificationDigest == "" {
		t.Fatalf("verified: %+v %v", state, err)
	}
	state, err = engine.Ratify(state.TransactionID, state.CertificationDigest, "manager", "cli")
	if err != nil || state.State != "migrated" {
		t.Fatalf("ratify: %+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "state", "head.json")); err != nil {
		t.Fatalf("0.8 state head missing: %v", err)
	}
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if head, err := store.LoadHead(); err != nil || head.Revision < 2 || head.EventID == "" || head.TransactionID == "" {
		t.Fatalf("migrated state head is not a valid engine head: %+v %v", head, err)
	}
	var ratification MigrationRatificationManifest
	ratificationBody, err := os.ReadFile(filepath.Join(root, ".re-discipline", "migration", "0.8", "certification.json"))
	if err != nil || json.Unmarshal(ratificationBody, &ratification) != nil {
		t.Fatalf("read migration ratification artifact: %v", err)
	}
	if ratification.TransactionID != state.TransactionID || ratification.CertificationDigest != state.CertificationDigest ||
		ratification.ImportHead.Revision < 1 || ratification.SnapshotObjects == 0 {
		t.Fatalf("ratification artifact did not bind the migration transaction and activated snapshot: %+v", ratification)
	}
}

func migrationGateArtifactBody(t *testing.T, root string, state MigrationState, gate string, passed bool) []byte {
	t.Helper()
	if passed && (gate == "retrieval-context" || gate == "host-parity") {
		artifact := migrationStrictGateArtifact(t, root, state, gate)
		body, err := json.MarshalIndent(artifact, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return append(body, '\n')
	}
	engine := fixedMigrationEngine(t, root)
	plan, err := engine.loadPlan()
	if err != nil {
		t.Fatal(err)
	}
	evidence := MigrationCertification{SchemaVersions: map[string]int{
		"migration": MigrationSchemaVersion, "campaign": CampaignSchemaVersion,
		"bootstrap": BootstrapSchemaVersion, "knowledge": SettingsSchemaVersion,
	}, GateReceipts: []MigrationGateReceipt{}, KnownLimitations: []string{}}
	if err := engine.populateMigrationCertificationEvidence(&evidence, plan, state); err != nil {
		t.Fatal(err)
	}
	schemaFingerprint, err := CanonicalDigest(evidence.SchemaVersions)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]string{
		"schema": schemaFingerprint, "destination-state": evidence.DestinationStateHead,
		"inventory": evidence.InventoryDigest, "coverage": evidence.CoverageDigest,
	}
	checks := []MigrationGateCheck{}
	for _, name := range requiredMigrationGateChecks(gate) {
		checks = append(checks, MigrationGateCheck{Name: name, Passed: passed, Evidence: "disposable migration fixture measurement for " + name})
	}
	fingerprints := map[string]string{}
	for _, name := range requiredMigrationGateFingerprints(gate) {
		value := known[name]
		if value == "" {
			value = "sha256:" + SHA256String("fixture:"+gate+":"+name)
		}
		fingerprints[name] = value
	}
	artifact := MigrationGateArtifact{SchemaVersion: MigrationSchemaVersion,
		TransactionID: state.TransactionID, PlanDigest: state.PlanDigest, Gate: gate, Passed: passed,
		Checks: checks, Fingerprints: fingerprints}
	digest, err := CanonicalDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifact.ResultDigest = digest
	body, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(body, '\n')
}

func TestMigrationCoverageIsImmutableAndDigestBound(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	source := migrationSourceByPath(t, preview.Plan,
		"active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	base := MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{fullMigrationCoverage(t, root, source, "non-claim", "")},
		Reviewer: "manager", Rationale: "fixture",
	}
	gap := base
	gap.Coverage = append([]CoverageEntry(nil), base.Coverage...)
	gap.Coverage[0].StartLine = 2
	gap.Coverage[0].SourceHandle = canonicalCoverageHandle(gap.Coverage[0])
	if _, err := engine.SubmitCoverage(gap); err == nil || !strings.Contains(err.Error(), "gap or overlap") {
		t.Fatalf("non-exhaustive report coverage was accepted: %v", err)
	}
	wrongSource := base
	wrongSource.Coverage = append([]CoverageEntry(nil), base.Coverage...)
	wrongSource.Coverage[0].SourcePath = source.Path
	wrongSource.Coverage[0].SourceHandle = canonicalCoverageHandle(wrongSource.Coverage[0])
	if _, err := engine.SubmitCoverage(wrongSource); err == nil || !strings.Contains(err.Error(), "canonical report path") {
		t.Fatalf("legacy rather than canonical report coverage was accepted: %v", err)
	}
	first, err := engine.SubmitCoverage(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.SubmitCoverage(base)
	if err != nil || first.Digest != second.Digest {
		t.Fatal("identical coverage retry must return the same receipt")
	}
	base.Rationale = "different"
	if _, err := engine.SubmitCoverage(base); err == nil {
		t.Fatal("an immutable coverage receipt cannot be replaced")
	}
	base.SourceDigest = SHA256String("stale")
	if _, err := engine.SubmitCoverage(base); err == nil {
		t.Fatal("stale source coverage must fail")
	}
}

func TestMigrationPhysicalActivationResumesAfterBackupRename(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	source := migrationSourceByPath(t, preview.Plan, "active/live-campaign/subagents/2026-07-28T06-09-49Z-codex-worker-a/report.md")
	_, err = engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")},
		Reviewer: "manager", Rationale: "resumability fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize: %+v %v", state, err)
	}
	journalPath := filepath.Join(engine.migrationRoot(), "activation.json")
	journal, err := engine.loadOrPrepareActivation(preview.Plan, state, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var interrupted migrationActivationTarget
	for _, target := range journal.Targets {
		if target.Existed {
			interrupted = target
			break
		}
	}
	if interrupted.Path == "" {
		t.Fatal("fixture has no existing managed target to interrupt")
	}
	canonical := filepath.Join(root, filepath.FromSlash(interrupted.Path))
	backup := filepath.Join(root, filepath.FromSlash(interrupted.BackupPath))
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(canonical, backup); err != nil {
		t.Fatal(err)
	}
	// The simulated crash happened before the pending target phase could be
	// journaled. Resume must rederive the backup digest and continue forward.
	state, err = engine.Resume(state.TransactionID, "manager", "mcp")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("resume after backup rename: %+v %v", state, err)
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("interrupted target was not published: %v", err)
	}
}

func migrationTestFinding(id string) MigrationFindingInput {
	return MigrationFindingInput{
		ID: id, Kind: "observation", Subject: "migration behavior",
		Claim: "The disposable legacy report records bounded migration behavior.",
		Scope: map[string]any{"campaign": "live-campaign"}, EvidenceGrade: "direct",
		SyntheticQuestions: []string{
			"What behavior did the disposable legacy report record?",
			"Which report supports the bounded migration behavior?",
			"Is the imported migration behavior manager-ratified?",
		},
		CuratorAttestation: MigrationFindingCuratorAttestation{
			SingleIndependentlyOverturnableClaim: true,
			EvidenceGradeAppliesToEntireClaim:    true,
			EntireSourceSpanRepresented:          true,
			SemanticBoundariesVerified:           true,
			LegacyReviewLanguageProvenanceOnly:   true,
			ManagerAttentionRequired:             true,
			Rationale:                            "The bounded fixture claim has one evidence grade and still requires independent manager review.",
		},
	}
}

func TestMigrationRejectsMissingOrNegativeCuratorAttestationBeforeSealingCoverage(t *testing.T) {
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
	receipt := MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage:   []CoverageEntry{fullMigrationCoverage(t, root, source, "candidate-finding", "F-0001")},
		FindingIDs: []string{"F-0001"}, Findings: []MigrationFindingInput{migrationTestFinding("F-0001")},
		Reviewer: "curator:test", Rationale: "atomicity attestation adversarial fixture",
	}
	coverageName := strings.TrimPrefix(SHA256String(source.Path), "sha256:") + ".json"
	coveragePath := filepath.Join(engine.migrationRoot(), "coverage", coverageName)
	withoutSpanRationale := receipt
	withoutSpanRationale.Coverage = append([]CoverageEntry(nil), receipt.Coverage...)
	withoutSpanRationale.Coverage[0].Rationale = ""
	if _, err := engine.SubmitCoverage(withoutSpanRationale); err == nil || !strings.Contains(err.Error(), "exact span rationale") {
		t.Fatalf("candidate without an exact whole-span rationale was accepted: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*MigrationFindingCuratorAttestation)
	}{
		{name: "missing", mutate: func(value *MigrationFindingCuratorAttestation) { *value = MigrationFindingCuratorAttestation{} }},
		{name: "compound", mutate: func(value *MigrationFindingCuratorAttestation) { value.SingleIndependentlyOverturnableClaim = false }},
		{name: "mixed evidence grade", mutate: func(value *MigrationFindingCuratorAttestation) { value.EvidenceGradeAppliesToEntireClaim = false }},
		{name: "unrepresented source text", mutate: func(value *MigrationFindingCuratorAttestation) { value.EntireSourceSpanRepresented = false }},
		{name: "unsafe semantic boundary", mutate: func(value *MigrationFindingCuratorAttestation) { value.SemanticBoundariesVerified = false }},
		{name: "legacy authority reuse", mutate: func(value *MigrationFindingCuratorAttestation) { value.LegacyReviewLanguageProvenanceOnly = false }},
		{name: "manager attention bypass", mutate: func(value *MigrationFindingCuratorAttestation) { value.ManagerAttentionRequired = false }},
		{name: "unreasoned", mutate: func(value *MigrationFindingCuratorAttestation) { value.Rationale = "" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := receipt
			candidate.Findings = append([]MigrationFindingInput(nil), receipt.Findings...)
			test.mutate(&candidate.Findings[0].CuratorAttestation)
			if _, err := engine.SubmitCoverage(candidate); err == nil || !strings.Contains(err.Error(), "curator atomicity") {
				t.Fatalf("invalid curator attestation was accepted: %v", err)
			}
			if _, err := os.Stat(coveragePath); !os.IsNotExist(err) {
				t.Fatalf("invalid immutable coverage receipt was written: %v", err)
			}
		})
	}
}

func TestMigrationPreservesReasonedUnresolvedCoverageForManagerAttention(t *testing.T) {
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
	entry := fullMigrationCoverage(t, root, source, "unresolved", "")
	if _, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{entry}, Reviewer: "curator:test",
		Rationale: "unreasoned unresolved coverage adversarial fixture",
	}); err == nil || !strings.Contains(err.Error(), "exact span rationale") {
		t.Fatalf("unreasoned unresolved coverage was accepted: %v", err)
	}
	entry.Rationale = "The exact span mixes observation and inference and cannot be split honestly at line boundaries."
	receipt, err := engine.SubmitCoverage(MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{entry}, Reviewer: "curator:test",
		Rationale: "preserve rather than coerce ambiguous source content",
	})
	if err != nil || receipt.Digest == "" {
		t.Fatalf("reasoned unresolved coverage was rejected: %+v %v", receipt, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("normalize unresolved coverage: %+v %v", state, err)
	}
	intakePath := filepath.Join(engine.migrationRoot(), "staging", "project", "active", "live-campaign", "intake", "I-0001.json")
	intakeBody, err := os.ReadFile(intakePath)
	if err != nil {
		t.Fatal(err)
	}
	var intake IntakeRecord
	if err := decodeStrict(intakeBody, &intake); err != nil {
		t.Fatal(err)
	}
	wantUncertainty := entry.SourceHandle + ": " + entry.Rationale
	requested := strings.Join(intake.RequestedDecisions, "\n")
	if len(intake.CandidateFindingIDs) != 0 || len(intake.Uncertainties) != 1 ||
		intake.Uncertainties[0] != wantUncertainty || intake.Status != "submitted" ||
		!strings.Contains(requested, "unresolved source span") ||
		!strings.Contains(requested, "Legacy verdicts, confirmations, manager re-derivations") {
		t.Fatalf("unresolved coverage was not preserved for manager attention: %+v", intake)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "physically-reorganized" {
		t.Fatalf("activate unresolved coverage: %+v %v", state, err)
	}
	if err := engine.verifyMigrationTraversal(preview.Plan); err != nil {
		t.Fatalf("reasoned manager-visible unresolved coverage failed traversal: %v", err)
	}
}

func TestMigrationRejectsInvalidFindingBodyBeforeSealingCoverage(t *testing.T) {
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
	entry := fullMigrationCoverage(t, root, source, "candidate-finding", "F-0001")
	finding := migrationTestFinding("F-0001")
	finding.Body = "A noncanonical body that would fail only after the receipt became immutable."
	receipt := MigrationCoverageReceipt{
		SourcePath: source.Path, SourceDigest: source.SHA256, Complete: true,
		Coverage: []CoverageEntry{entry}, FindingIDs: []string{"F-0001"},
		Findings: []MigrationFindingInput{finding}, Reviewer: "manager",
		Rationale: "adversarial body validation",
	}
	if _, err := engine.SubmitCoverage(receipt); err == nil || !strings.Contains(err.Error(), "missing stable section") {
		t.Fatalf("noncanonical finding body was not rejected before persistence: %v", err)
	}
	coverageName := strings.TrimPrefix(SHA256String(source.Path), "sha256:") + ".json"
	if _, err := os.Stat(filepath.Join(engine.migrationRoot(), "coverage", coverageName)); !os.IsNotExist(err) {
		t.Fatalf("invalid immutable coverage receipt was written: %v", err)
	}

	receipt.Findings[0].Body = renderMigrationFindingBody(receipt.Findings[0], source.Path)
	accepted, err := engine.SubmitCoverage(receipt)
	if err != nil || accepted.Digest == "" {
		t.Fatalf("corrected canonical finding body was not accepted: %+v %v", accepted, err)
	}
	state, err = engine.Resume(state.TransactionID, "manager", "cli")
	if err != nil || state.State != "normalized" {
		t.Fatalf("corrected submission did not permit normalization: %+v %v", state, err)
	}
}

func NewStateStoreWithBoundaryMust(root string) *StateStore {
	store, err := NewStateStore(root)
	if err != nil {
		panic(err)
	}
	return store
}

func migrationSourceByPath(t *testing.T, plan MigrationPlan, path string) MigrationSource {
	t.Helper()
	for _, source := range plan.Sources {
		if source.Path == path {
			return source
		}
	}
	t.Fatalf("migration source %s absent", path)
	return MigrationSource{}
}

func bytesCount(body, fragment []byte) int {
	count := 0
	for {
		index := strings.Index(string(body), string(fragment))
		if index < 0 {
			return count
		}
		count++
		body = body[index+len(fragment):]
	}
}
