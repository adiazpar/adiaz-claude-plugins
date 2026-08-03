package knowledge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const unsupportedLegacyProfileFixture = `{"schemaVersion":0,"profileId":"project:legacy-dense-rerank","effectiveProfiles":[{"name":"legacy","lanes":["exact","fts","graph","dense","rerank"],"requires":{"embedding":"legacy-model"}}]}` + "\n"

func migrationProfileDecisionFixture(t *testing.T) (string, MigrationProfileConflictPacket) {
	t.Helper()
	root := migrationPreviewFixture(t)
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(migrationLegacyRetrievalProfilePath)), unsupportedLegacyProfileFixture)
	packet, err := ExportMigrationProfileConflict(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, packet
}

func validMigrationProfileDecisionSubmission(packet MigrationProfileConflictPacket) MigrationProfileDecisionSubmission {
	return MigrationProfileDecisionSubmission{
		SchemaVersion: MigrationSchemaVersion, PacketDigest: packet.Digest,
		SourceFingerprint: packet.SourceFingerprint,
		SourcePath:        packet.Conflict.SourcePath, SourceDigest: packet.Conflict.SourceDigest,
		BaselineProfileID: packet.Baseline.Profile.ProfileID, BaselineProfileDigest: packet.Baseline.ProfileDigest,
		EffectiveProfileName: packet.Baseline.EffectiveProfileName, EffectiveProfileDigest: packet.Baseline.EffectiveProfileDigest,
		MeasurementEvidenceDigest: packet.Baseline.MeasurementEvidenceDigest,
		Decision:                  migrationProfileDecisionKind,
		ExplicitManagerApproval:   true, ProjectProfileActivation: false,
		Authority: "manager", Reviewer: "fixture-manager", Rationale: "Retain the measured packaged baseline without activating the incompatible legacy project profile.",
		DecidedAt: "2026-08-03T12:00:00Z", ReplacesDecisionDigest: "",
	}
}

func TestMigrationProfileConflictPacketCapturesExactLegacyAndPackagedEvidence(t *testing.T) {
	root, first := migrationProfileDecisionFixture(t)
	second, err := ExportMigrationProfileConflict(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || first.Digest != second.Digest || first.SourceFingerprint != second.SourceFingerprint {
		t.Fatalf("profile conflict export is not stable: first=%+v second=%+v", first, second)
	}
	if first.Conflict.LegacyProfile != unsupportedLegacyProfileFixture ||
		first.Conflict.SourceDigest != "sha256:"+SHA256Bytes([]byte(unsupportedLegacyProfileFixture)) ||
		first.Conflict.CompatibilityStatus != "unsupported" || first.Conflict.Digest == "" {
		t.Fatalf("packet did not capture the exact incompatible legacy profile: %+v", first.Conflict)
	}
	if first.Baseline.Profile.ProfileID != "plugin:balanced-v1" ||
		first.Baseline.EffectiveProfileName != migrationBaselineEffectiveProfile ||
		first.Baseline.MeasurementEvidence.Status != "passed" ||
		first.Baseline.MeasurementEvidence.Digest == "" || first.Baseline.MeasurementEvidenceDigest == "" ||
		first.Baseline.ActivationState != migrationProfileActivationState {
		t.Fatalf("packet omitted the measured packaged baseline: %+v", first.Baseline)
	}
	production, err := os.ReadFile(filepath.Join(adversarialAssetRoot(t), "profiles", "balanced-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(production, migrationRetrievalProfileTemplate) || first.Baseline.ProfileDigest != "sha256:"+SHA256Bytes(production) {
		t.Fatal("embedded migration baseline drifted from the release retrieval profile")
	}
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "migration")); !os.IsNotExist(err) {
		t.Fatalf("read-only profile conflict export mutated the project: %v", err)
	}
}

func TestMigrationProfileDecisionResolvesOnlyExactBlockerAndNeverActivatesProfile(t *testing.T) {
	root, packet := migrationProfileDecisionFixture(t)
	before, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before.Plan.ProfileDecision != nil || !migrationHasBlockingConflict(before.Plan, "unsupported-retrieval-profile") {
		t.Fatalf("unsupported profile did not begin as an unresolved blocker: %+v", before.Plan)
	}
	submission := validMigrationProfileDecisionSubmission(packet)
	decision, err := SubmitMigrationProfileDecision(root, submission, "")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := SubmitMigrationProfileDecision(root, submission, "")
	if err != nil || replayed.Digest != decision.Digest {
		t.Fatalf("identical profile decision replay was not idempotent: %+v %v", replayed, err)
	}
	if decision.ProjectProfileActivation || !decision.ExplicitManagerApproval || decision.Digest == "" {
		t.Fatalf("sealed decision imported activation authority: %+v", decision)
	}
	after, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after.Plan.SourceFingerprint != before.Plan.SourceFingerprint || after.Plan.ProfileDecision == nil ||
		after.Plan.ProfileDecision.Digest != decision.Digest || migrationHasProfileConflict(after.Plan) {
		t.Fatalf("preview replay did not resolve exactly the profile blocker: %+v", after.Plan)
	}
	wantConflicts := make([]MigrationConflict, 0, len(before.Plan.Conflicts)-1)
	for _, conflict := range before.Plan.Conflicts {
		if conflict.Code != "unsupported-retrieval-profile" {
			wantConflicts = append(wantConflicts, conflict)
		}
	}
	if !reflect.DeepEqual(wantConflicts, after.Plan.Conflicts) {
		t.Fatalf("profile decision changed unrelated conflicts:\nwant=%+v\ngot=%+v", wantConflicts, after.Plan.Conflicts)
	}
	auditDestination := migrationProfileAuditDecisionPath(packet.Conflict.SourcePath)
	auditPlanned := false
	for _, operation := range after.Plan.Operations {
		if len(operation.Sources) == 1 && operation.Sources[0] == packet.Conflict.SourcePath &&
			containsString(operation.Destinations, auditDestination) &&
			containsString(operation.Requires, "sealed-profile-decision:"+decision.Digest) {
			auditPlanned = true
		}
	}
	if !auditPlanned {
		t.Fatal("approved plan did not disclose the sealed profile-decision audit materialization")
	}
	legacyPath := filepath.Join(root, filepath.FromSlash(migrationLegacyRetrievalProfilePath))
	if body, err := os.ReadFile(legacyPath); err != nil || string(body) != unsupportedLegacyProfileFixture {
		t.Fatalf("decision submission changed legacy profile bytes: %v %s", err, body)
	}

	engine := fixedMigrationEngine(t, root)
	state, err := engine.Start(after.Plan, after.Plan.PlanDigest, "fixture-manager", "cli")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitMigrationProfileDecision(root, submission, decision.Digest); err == nil {
		t.Fatal("profile decision changed after migration application began")
	}
	for _, wantState := range []string{"shadow-indexed", "normalized", "physically-reorganized"} {
		state, err = engine.Resume(state.TransactionID, "fixture-manager", "cli")
		if err != nil {
			t.Fatalf("resume to %s: %v", wantState, err)
		}
		if state.State != wantState {
			t.Fatalf("resume state: got %s want %s", state.State, wantState)
		}
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy profile remained in the active project-profile slot: %v", err)
	}
	provenancePath := filepath.Join(root, ".re-discipline", "knowledge", "migration", "legacy-retrieval-profile.json")
	if body, err := os.ReadFile(provenancePath); err != nil || string(body) != unsupportedLegacyProfileFixture {
		t.Fatalf("legacy profile provenance was not byte-exact: %v %s", err, body)
	}
	auditPath := filepath.Join(root, filepath.FromSlash(migrationProfileAuditDecisionPath(packet.Conflict.SourcePath)))
	var audit MigrationProfileConversionDecision
	if body, err := os.ReadFile(auditPath); err != nil || json.Unmarshal(body, &audit) != nil || audit.Digest != decision.Digest {
		t.Fatalf("activated tree omitted the sealed non-activating decision: %v %+v", err, audit)
	}
	if _, err := os.Stat(filepath.Join(root, ".re-discipline", "knowledge", "retrieval-profile.json")); !os.IsNotExist(err) {
		t.Fatalf("migration silently promoted a project retrieval profile: %v", err)
	}
}

func TestMigrationProfileDecisionRejectsStaleWrongAndImplicitSubmissions(t *testing.T) {
	root, packet := migrationProfileDecisionFixture(t)
	valid := validMigrationProfileDecisionSubmission(packet)
	tests := []struct {
		name   string
		mutate func(*MigrationProfileDecisionSubmission)
	}{
		{"stale-packet", func(row *MigrationProfileDecisionSubmission) {
			row.PacketDigest = "sha256:" + SHA256String("stale-packet")
		}},
		{"stale-source-fingerprint", func(row *MigrationProfileDecisionSubmission) {
			row.SourceFingerprint = "sha256:" + SHA256String("stale-source")
		}},
		{"wrong-source", func(row *MigrationProfileDecisionSubmission) {
			row.SourceDigest = "sha256:" + SHA256String("wrong-source")
		}},
		{"wrong-baseline-profile", func(row *MigrationProfileDecisionSubmission) { row.BaselineProfileID = "project:forged" }},
		{"wrong-baseline-digest", func(row *MigrationProfileDecisionSubmission) {
			row.BaselineProfileDigest = "sha256:" + SHA256String("wrong-profile")
		}},
		{"wrong-effective-profile", func(row *MigrationProfileDecisionSubmission) { row.EffectiveProfileName = "lexical-graph-v1" }},
		{"wrong-effective-digest", func(row *MigrationProfileDecisionSubmission) {
			row.EffectiveProfileDigest = "sha256:" + SHA256String("wrong-effective")
		}},
		{"wrong-evidence", func(row *MigrationProfileDecisionSubmission) {
			row.MeasurementEvidenceDigest = "sha256:" + SHA256String("wrong-evidence")
		}},
		{"implicit", func(row *MigrationProfileDecisionSubmission) { row.ExplicitManagerApproval = false }},
		{"unauthorized", func(row *MigrationProfileDecisionSubmission) { row.Authority = "drafter" }},
		{"activation-authority", func(row *MigrationProfileDecisionSubmission) { row.ProjectProfileActivation = true }},
		{"wrong-decision", func(row *MigrationProfileDecisionSubmission) { row.Decision = "promote" }},
		{"anonymous", func(row *MigrationProfileDecisionSubmission) { row.Reviewer = "" }},
		{"no-rationale", func(row *MigrationProfileDecisionSubmission) { row.Rationale = "" }},
		{"missing-time", func(row *MigrationProfileDecisionSubmission) { row.DecidedAt = "" }},
		{"invalid-time", func(row *MigrationProfileDecisionSubmission) { row.DecidedAt = "not-a-time" }},
		{"non-utc-time", func(row *MigrationProfileDecisionSubmission) { row.DecidedAt = "2026-08-03T13:00:00+01:00" }},
		{"malformed-lineage", func(row *MigrationProfileDecisionSubmission) { row.ReplacesDecisionDigest = "prior" }},
		{"fabricated-lineage", func(row *MigrationProfileDecisionSubmission) {
			row.ReplacesDecisionDigest = "sha256:" + SHA256String("missing-prior")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			submission := valid
			test.mutate(&submission)
			if _, err := SubmitMigrationProfileDecision(root, submission, ""); err == nil {
				t.Fatal("invalid profile decision was accepted")
			}
		})
	}
	if _, err := os.Stat(migrationProfileDecisionPath(root, packet.Conflict.SourcePath)); !os.IsNotExist(err) {
		t.Fatalf("rejected submission wrote a sealed decision: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(migrationLegacyRetrievalProfilePath))); err != nil || string(body) != unsupportedLegacyProfileFixture {
		t.Fatalf("rejected submissions changed the legacy profile: %v %s", err, body)
	}
}

func TestMigrationProfileDecisionFailsClosedOnSourceOrDecisionDriftAndResume(t *testing.T) {
	t.Run("source-drift", func(t *testing.T) {
		root, packet := migrationProfileDecisionFixture(t)
		firstDecision, err := SubmitMigrationProfileDecision(root, validMigrationProfileDecisionSubmission(packet), "")
		if err != nil {
			t.Fatal(err)
		}
		approved, err := PreviewMigration(root, nil)
		if err != nil || approved.Plan.ProfileDecision == nil {
			t.Fatalf("approved preview: %+v %v", approved.Plan, err)
		}
		mustWriteFile(t, filepath.Join(root, filepath.FromSlash(migrationLegacyRetrievalProfilePath)),
			strings.Replace(unsupportedLegacyProfileFixture, "legacy-model", "changed-model", 1))
		stale, err := PreviewMigration(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !migrationHasBlockingConflict(stale.Plan, "retrieval-profile-decision-invalid") || stale.Plan.ProfileDecision != nil {
			t.Fatalf("source drift did not stale the exact decision: %+v", stale.Plan)
		}
		engine := fixedMigrationEngine(t, root)
		if _, err := engine.Start(approved.Plan, approved.Plan.PlanDigest, "fixture-manager", "cli"); err == nil {
			t.Fatal("stale approved plan began migration after legacy source drift")
		}
		if _, err := os.Stat(engine.statePath()); !os.IsNotExist(err) {
			t.Fatalf("stale apply wrote transaction state: %v", err)
		}
		newPacket, err := ExportMigrationProfileConflict(root)
		if err != nil || newPacket.Digest == packet.Digest {
			t.Fatalf("source drift did not produce a new conflict packet: %+v %v", newPacket, err)
		}
		replacement := validMigrationProfileDecisionSubmission(newPacket)
		replacement.DecidedAt = "2026-08-03T12:01:00Z"
		replacement.ReplacesDecisionDigest = firstDecision.Digest
		secondDecision, err := SubmitMigrationProfileDecision(root, replacement, firstDecision.Digest)
		if err != nil || secondDecision.ReplacesDecisionDigest != firstDecision.Digest {
			t.Fatalf("exact stale-source decision replacement failed: %+v %v", secondDecision, err)
		}
		refreshed, err := PreviewMigration(root, nil)
		if err != nil || refreshed.Plan.ProfileDecision == nil ||
			refreshed.Plan.ProfileDecision.Digest != secondDecision.Digest || migrationHasProfileConflict(refreshed.Plan) {
			t.Fatalf("replacement decision did not resolve the refreshed exact blocker: %+v %v", refreshed.Plan, err)
		}
	})

	t.Run("sealed-decision-drift-before-resume", func(t *testing.T) {
		root, packet := migrationProfileDecisionFixture(t)
		decision, err := SubmitMigrationProfileDecision(root, validMigrationProfileDecisionSubmission(packet), "")
		if err != nil {
			t.Fatal(err)
		}
		approved, err := PreviewMigration(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		engine := fixedMigrationEngine(t, root)
		state, err := engine.Start(approved.Plan, approved.Plan.PlanDigest, "fixture-manager", "cli")
		if err != nil {
			t.Fatal(err)
		}
		decision.MeasurementEvidence.Digest = "sha256:" + SHA256String("forged-measurement")
		decision.Digest = ""
		decision.Digest, err = CanonicalDigest(decision)
		if err != nil {
			t.Fatal(err)
		}
		if err := AtomicWriteJSON(migrationProfileDecisionPath(root, packet.Conflict.SourcePath), decision, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Resume(state.TransactionID, "fixture-manager", "cli"); err == nil {
			t.Fatal("resume consumed tampered measurement evidence")
		}
		unchanged, err := engine.Status()
		if err != nil || unchanged.State != "inventoried" {
			t.Fatalf("failed resume mutated migration state: %+v %v", unchanged, err)
		}
	})

	t.Run("fabricated-replacement-lineage", func(t *testing.T) {
		root, packet := migrationProfileDecisionFixture(t)
		decision, err := SubmitMigrationProfileDecision(root, validMigrationProfileDecisionSubmission(packet), "")
		if err != nil {
			t.Fatal(err)
		}
		decision.ReplacesDecisionDigest = "sha256:" + SHA256String("fabricated-history")
		decision.Digest = ""
		decision.Digest, err = CanonicalDigest(decision)
		if err != nil {
			t.Fatal(err)
		}
		if err := AtomicWriteJSON(migrationProfileDecisionPath(root, packet.Conflict.SourcePath), decision, 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := PreviewMigration(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !migrationHasBlockingConflict(preview.Plan, "retrieval-profile-decision-invalid") || preview.Plan.ProfileDecision != nil {
			t.Fatalf("fabricated lineage was not rejected on preview replay: %+v", preview.Plan)
		}
	})
}

func TestMigrationProfileDecisionReplacementRequiresExactPriorDigest(t *testing.T) {
	root, packet := migrationProfileDecisionFixture(t)
	firstSubmission := validMigrationProfileDecisionSubmission(packet)
	first, err := SubmitMigrationProfileDecision(root, firstSubmission, "")
	if err != nil {
		t.Fatal(err)
	}
	secondSubmission := firstSubmission
	secondSubmission.Rationale = "A corrected manager rationale still retains only the exact packaged baseline and grants no activation authority."
	secondSubmission.DecidedAt = "2026-08-03T12:01:00Z"
	secondSubmission.ReplacesDecisionDigest = first.Digest
	if _, err := SubmitMigrationProfileDecision(root, secondSubmission, ""); err == nil {
		t.Fatal("profile decision replacement omitted its exact prior digest")
	}
	if _, err := SubmitMigrationProfileDecision(root, secondSubmission, "sha256:"+SHA256String("wrong-prior")); err == nil {
		t.Fatal("profile decision replacement accepted the wrong prior digest")
	}
	mismatchedLineage := secondSubmission
	mismatchedLineage.ReplacesDecisionDigest = "sha256:" + SHA256String("wrong-lineage")
	if _, err := SubmitMigrationProfileDecision(root, mismatchedLineage, first.Digest); err == nil {
		t.Fatal("profile decision replacement accepted mismatched declared lineage")
	}
	nonIncreasing := secondSubmission
	nonIncreasing.DecidedAt = first.DecidedAt
	if _, err := SubmitMigrationProfileDecision(root, nonIncreasing, first.Digest); err == nil {
		t.Fatal("profile decision replacement accepted a non-increasing decision timestamp")
	}
	second, err := SubmitMigrationProfileDecision(root, secondSubmission, first.Digest)
	if err != nil || second.Digest == first.Digest || second.ReplacesDecisionDigest != first.Digest {
		t.Fatalf("exact profile decision replacement failed: %+v %v", second, err)
	}
	replayed, err := SubmitMigrationProfileDecision(root, secondSubmission, first.Digest)
	if err != nil || replayed.Digest != second.Digest {
		t.Fatalf("replacement decision replay was not idempotent: %+v %v", replayed, err)
	}
	historyPath := migrationProfileDecisionHistoryPath(root, packet.Conflict.SourcePath, first.Digest)
	var archived MigrationProfileConversionDecision
	if body, err := os.ReadFile(historyPath); err != nil || json.Unmarshal(body, &archived) != nil || archived.Digest != first.Digest {
		t.Fatalf("prior profile decision was not archived exactly: %v %+v", err, archived)
	}
	preview, err := PreviewMigration(root, nil)
	if err != nil || preview.Plan.ProfileDecision == nil || preview.Plan.ProfileDecision.Digest != second.Digest || migrationHasProfileConflict(preview.Plan) {
		t.Fatalf("preview did not replay the exact replacement decision: %+v %v", preview.Plan, err)
	}
}

func migrationHasBlockingConflict(plan MigrationPlan, code string) bool {
	for _, conflict := range plan.Conflicts {
		if conflict.Code == code && conflict.Blocks {
			return true
		}
	}
	return false
}

func migrationHasProfileConflict(plan MigrationPlan) bool {
	return migrationHasBlockingConflict(plan, "unsupported-retrieval-profile") ||
		migrationHasBlockingConflict(plan, "retrieval-profile-decision-invalid")
}
