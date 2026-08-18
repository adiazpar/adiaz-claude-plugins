package knowledge

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	migrationLegacyRetrievalProfilePath = ".re-discipline/knowledge/retrieval-profile.json"
	migrationBaselineEffectiveProfile   = "hybrid-no-rerank-v1"
	migrationProfileDecisionKind        = "retain-packaged-baseline"
	migrationProfileActivationState     = "packaged-baseline-only-no-project-profile-activation"
)

// The exact release baseline is compiled into the sole legacy reader. A
// migration preview must not infer a package path from the project or depend
// on a source checkout that may not exist in an installed plugin.
//
//go:embed migration_templates/balanced-v1.json
var migrationRetrievalProfileTemplate []byte

// MigrationProfileBaseline is the complete 0.8 baseline offered to a
// maintainer resolving an unsupported 0.7 profile. ProfileDigest binds the
// exact packaged catalog (including its measured model-free fallback), while
// the effective-profile and evidence digests make the primary runtime choice
// independently reviewable.
type MigrationProfileBaseline struct {
	Profile                   RetrievalProfile  `json:"profile"`
	ProfileDigest             string            `json:"profileDigest"`
	EffectiveProfileName      string            `json:"effectiveProfileName"`
	EffectiveProfileDigest    string            `json:"effectiveProfileDigest"`
	MeasurementEvidence       BenchmarkEvidence `json:"measurementEvidence"`
	MeasurementEvidenceDigest string            `json:"measurementEvidenceDigest"`
	RuntimeVersion            string            `json:"runtimeVersion"`
	ActivationState           string            `json:"activationState"`
}

// MigrationProfileConflict captures the exact legacy profile bytes and the
// specific incompatibility that blocks migration. The legacy file remains a
// provenance source; this record does not reinterpret or activate it.
type MigrationProfileConflict struct {
	Code                string `json:"code"`
	SourcePath          string `json:"sourcePath"`
	SourceDigest        string `json:"sourceDigest"`
	CompatibilityStatus string `json:"compatibilityStatus"`
	Reason              string `json:"reason"`
	LegacyProfile       string `json:"legacyProfile"`
	RequiredDecision    string `json:"requiredDecision"`
	Digest              string `json:"digest"`
}

// MigrationProfileConflictPacket is a read-only, digest-bound manager handoff
// for the one unsupported accepted 0.7 project profile. It couples that exact
// source snapshot to the only packaged 0.8 baseline this runtime can offer.
type MigrationProfileConflictPacket struct {
	SchemaVersion     int                      `json:"schemaVersion"`
	Project           string                   `json:"project"`
	ProjectIdentity   string                   `json:"projectIdentity"`
	SourceFingerprint string                   `json:"sourceFingerprint"`
	Conflict          MigrationProfileConflict `json:"conflict"`
	Baseline          MigrationProfileBaseline `json:"baseline"`
	Digest            string                   `json:"digest"`
}

// MigrationProfileDecisionSubmission contains only manager intent and exact
// identities copied from the conflict packet. The engine supplies the trusted
// measurement body when sealing the decision, so a caller cannot substitute
// self-authored evidence under a familiar digest field.
type MigrationProfileDecisionSubmission struct {
	SchemaVersion             int    `json:"schemaVersion"`
	PacketDigest              string `json:"packetDigest"`
	SourceFingerprint         string `json:"sourceFingerprint"`
	SourcePath                string `json:"sourcePath"`
	SourceDigest              string `json:"sourceDigest"`
	BaselineProfileID         string `json:"baselineProfileId"`
	BaselineProfileDigest     string `json:"baselineProfileDigest"`
	EffectiveProfileName      string `json:"effectiveProfileName"`
	EffectiveProfileDigest    string `json:"effectiveProfileDigest"`
	MeasurementEvidenceDigest string `json:"measurementEvidenceDigest"`
	Decision                  string `json:"decision"`
	ExplicitManagerApproval   bool   `json:"explicitManagerApproval"`
	ProjectProfileActivation  bool   `json:"projectProfileActivation"`
	Authority                 string `json:"authority"`
	Reviewer                  string `json:"reviewer"`
	Rationale                 string `json:"rationale"`
	DecidedAt                 string `json:"decidedAt"`
	ReplacesDecisionDigest    string `json:"replacesDecisionDigest"`
}

// MigrationProfileConversionDecision is the sealed pre-transaction decision
// consumed by preview replay. ProjectProfileActivation is required to remain
// false: this decision clears a conversion blocker but never promotes or
// writes a project retrieval profile.
type MigrationProfileConversionDecision struct {
	SchemaVersion             int               `json:"schemaVersion"`
	PacketDigest              string            `json:"packetDigest"`
	ConflictDigest            string            `json:"conflictDigest"`
	SourceFingerprint         string            `json:"sourceFingerprint"`
	SourcePath                string            `json:"sourcePath"`
	SourceDigest              string            `json:"sourceDigest"`
	BaselineProfileID         string            `json:"baselineProfileId"`
	BaselineProfileDigest     string            `json:"baselineProfileDigest"`
	EffectiveProfileName      string            `json:"effectiveProfileName"`
	EffectiveProfileDigest    string            `json:"effectiveProfileDigest"`
	MeasurementEvidence       BenchmarkEvidence `json:"measurementEvidence"`
	MeasurementEvidenceDigest string            `json:"measurementEvidenceDigest"`
	Decision                  string            `json:"decision"`
	ExplicitManagerApproval   bool              `json:"explicitManagerApproval"`
	ProjectProfileActivation  bool              `json:"projectProfileActivation"`
	Authority                 string            `json:"authority"`
	Reviewer                  string            `json:"reviewer"`
	Rationale                 string            `json:"rationale"`
	DecidedAt                 string            `json:"decidedAt"`
	ReplacesDecisionDigest    string            `json:"replacesDecisionDigest"`
	Digest                    string            `json:"digest"`
}

func migrationPackagedProfileBaseline() (MigrationProfileBaseline, error) {
	var profile RetrievalProfile
	if err := decodeStrictJSON(migrationRetrievalProfileTemplate, &profile); err != nil {
		return MigrationProfileBaseline{}, fmt.Errorf("decode embedded migration retrieval baseline: %w", err)
	}
	if err := ValidateProfile(profile); err != nil {
		return MigrationProfileBaseline{}, fmt.Errorf("validate embedded migration retrieval baseline: %w", err)
	}
	if len(profile.Approval) != 0 {
		return MigrationProfileBaseline{}, errors.New("embedded migration baseline must not contain a project promotion approval")
	}
	var effective *EffectiveProfile
	for index := range profile.EffectiveProfiles {
		if profile.EffectiveProfiles[index].Name == migrationBaselineEffectiveProfile {
			effective = &profile.EffectiveProfiles[index]
			break
		}
	}
	if effective == nil || effective.Benchmark.Status != "passed" {
		return MigrationProfileBaseline{}, errors.New("embedded migration baseline lacks its passed primary effective profile")
	}
	effectiveDigest, err := CanonicalDigest(*effective)
	if err != nil {
		return MigrationProfileBaseline{}, err
	}
	measurementDigest, err := CanonicalDigest(effective.Benchmark)
	if err != nil {
		return MigrationProfileBaseline{}, err
	}
	return MigrationProfileBaseline{
		Profile: profile, ProfileDigest: "sha256:" + SHA256Bytes(migrationRetrievalProfileTemplate),
		EffectiveProfileName: effective.Name, EffectiveProfileDigest: effectiveDigest,
		MeasurementEvidence: effective.Benchmark, MeasurementEvidenceDigest: measurementDigest,
		RuntimeVersion: RuntimeVersion, ActivationState: migrationProfileActivationState,
	}, nil
}

// ExportMigrationProfileConflict is read-only. The packet stays available
// after a decision is submitted so a manager can independently replay and
// compare the exact source, baseline, and evidence identities.
func ExportMigrationProfileConflict(projectRoot string) (MigrationProfileConflictPacket, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	statePath := filepath.Join(boundary.Root, ".re-discipline", "migration", "0.8", "state.json")
	if _, stateErr := os.Lstat(statePath); stateErr == nil {
		return MigrationProfileConflictPacket{}, errors.New("profile conflict export is available only before migration application begins")
	} else if !os.IsNotExist(stateErr) {
		return MigrationProfileConflictPacket{}, stateErr
	}
	version, err := DetectProjectStateVersion(boundary.Root)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	if version != "0.7" {
		return MigrationProfileConflictPacket{}, fmt.Errorf("profile conflict export requires a 0.7 project, got %s", version)
	}
	sources, _, err := migrationInventory(boundary)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	return buildMigrationProfileConflictPacket(boundary, sources)
}

func buildMigrationProfileConflictPacket(
	boundary Boundary,
	sources []MigrationSource,
) (MigrationProfileConflictPacket, error) {
	status, path, digest, reason := migrationLegacyProfileCompatibility(boundary, sources)
	if status != "unsupported" {
		return MigrationProfileConflictPacket{}, errors.New("project has no unsupported legacy retrieval-profile blocker")
	}
	source, ok := migrationSourceWithPath(sources, path)
	if !ok || source.Role != "legacy-retrieval-profile" || source.SHA256 != digest {
		return MigrationProfileConflictPacket{}, errors.New("unsupported legacy retrieval profile is absent from the migration inventory")
	}
	body, err := readMigrationSource(boundary.Root, source)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	projectIdentity := "missing"
	if profileSource, present := migrationSourceWithPath(sources, ".re-discipline/project-profile.md"); present {
		projectIdentity = profileSource.SHA256
	}
	packet := MigrationProfileConflictPacket{
		SchemaVersion: MigrationSchemaVersion,
		Project:       filepath.Base(boundary.Root), ProjectIdentity: projectIdentity,
		Baseline: baseline,
		Conflict: MigrationProfileConflict{
			Code: "unsupported-retrieval-profile", SourcePath: path,
			SourceDigest: "sha256:" + source.SHA256, CompatibilityStatus: status,
			Reason: reason, LegacyProfile: string(body),
			RequiredDecision: "Explicitly retain the exact packaged 0.8 baseline and its measured primary effective profile for conversion while leaving project-profile activation false.",
		},
	}
	// The packet is a function of the project's source bytes alone. Staging
	// rebuilds it from the approved plan's sources, which already carry their
	// planned truth destinations, while export builds it from sources as
	// read; fingerprinting a destination-free projection keeps both callers
	// on one identity so a sealed decision cannot appear to change.
	packet.SourceFingerprint, err = CanonicalDigest(sourcesAsRead(sources))
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	packet.Conflict.Digest, err = CanonicalDigest(packet.Conflict)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	packet.Digest, err = CanonicalDigest(packet)
	if err != nil {
		return MigrationProfileConflictPacket{}, err
	}
	return packet, nil
}

// SubmitMigrationProfileDecision writes only a sealed decision under the
// excluded pre-transaction migration root. It is idempotent for an identical
// replay; replacing a different decision requires its exact prior digest.
func SubmitMigrationProfileDecision(
	projectRoot string,
	submission MigrationProfileDecisionSubmission,
	expectedPriorDigest string,
) (MigrationProfileConversionDecision, error) {
	engine, err := NewMigrationEngine(projectRoot)
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	defer operationLock.Close()
	if _, err := os.Lstat(engine.statePath()); err == nil {
		return MigrationProfileConversionDecision{}, errors.New("profile conversion decisions cannot change after migration application begins")
	} else if !os.IsNotExist(err) {
		return MigrationProfileConversionDecision{}, err
	}
	packet, err := ExportMigrationProfileConflict(engine.ProjectRoot)
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	submission.SourcePath = NormalizeProjectPath(submission.SourcePath)
	submission.Decision = strings.TrimSpace(submission.Decision)
	submission.Authority = strings.TrimSpace(submission.Authority)
	submission.Reviewer = strings.TrimSpace(submission.Reviewer)
	submission.Rationale = strings.TrimSpace(submission.Rationale)
	submission.DecidedAt = strings.TrimSpace(submission.DecidedAt)
	submission.ReplacesDecisionDigest = strings.TrimSpace(submission.ReplacesDecisionDigest)
	if err := validateMigrationProfileDecisionSubmission(submission, packet); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	decision := MigrationProfileConversionDecision{
		SchemaVersion: MigrationSchemaVersion, PacketDigest: packet.Digest,
		ConflictDigest: packet.Conflict.Digest, SourceFingerprint: packet.SourceFingerprint,
		SourcePath: packet.Conflict.SourcePath, SourceDigest: packet.Conflict.SourceDigest,
		BaselineProfileID: packet.Baseline.Profile.ProfileID, BaselineProfileDigest: packet.Baseline.ProfileDigest,
		EffectiveProfileName: packet.Baseline.EffectiveProfileName, EffectiveProfileDigest: packet.Baseline.EffectiveProfileDigest,
		MeasurementEvidence: packet.Baseline.MeasurementEvidence, MeasurementEvidenceDigest: packet.Baseline.MeasurementEvidenceDigest,
		Decision: submission.Decision, ExplicitManagerApproval: submission.ExplicitManagerApproval,
		ProjectProfileActivation: submission.ProjectProfileActivation,
		Authority:                submission.Authority, Reviewer: submission.Reviewer, Rationale: submission.Rationale,
		DecidedAt: submission.DecidedAt, ReplacesDecisionDigest: submission.ReplacesDecisionDigest,
	}
	if err := validateMigrationProfileDecision(decision, packet); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	decision.Digest, err = CanonicalDigest(decision)
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	path := migrationProfileDecisionPath(engine.ProjectRoot, decision.SourcePath)
	if err := engine.ensureMigrationDirectory(filepath.Dir(path)); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	if body, readErr := readSingleLinkRegularFile(path); readErr == nil {
		prior, decodeErr := decodeSealedMigrationProfileDecision(body)
		if decodeErr != nil {
			return MigrationProfileConversionDecision{}, fmt.Errorf("existing profile conversion decision is invalid: %w", decodeErr)
		}
		if lineageErr := validateMigrationProfileDecisionLineage(engine.ProjectRoot, prior); lineageErr != nil {
			return MigrationProfileConversionDecision{}, fmt.Errorf("existing profile conversion decision lineage is invalid: %w", lineageErr)
		}
		if prior.Digest == decision.Digest {
			if expectedPriorDigest != "" && expectedPriorDigest != decision.ReplacesDecisionDigest {
				return MigrationProfileConversionDecision{}, errors.New("idempotent profile decision replay has mismatched replacement lineage")
			}
			return prior, nil
		}
		if expectedPriorDigest == "" || expectedPriorDigest != prior.Digest ||
			decision.ReplacesDecisionDigest != prior.Digest {
			return MigrationProfileConversionDecision{}, errors.New("replacing a profile conversion decision requires its exact prior digest")
		}
		if !migrationProfileDecisionTimeAfter(decision.DecidedAt, prior.DecidedAt) {
			return MigrationProfileConversionDecision{}, errors.New("replacement profile decision decidedAt must be later than the prior decision")
		}
		historyPath := migrationProfileDecisionHistoryPath(engine.ProjectRoot, decision.SourcePath, prior.Digest)
		if err := engine.ensureMigrationDirectory(filepath.Dir(historyPath)); err != nil {
			return MigrationProfileConversionDecision{}, err
		}
		if existingHistory, historyErr := readSingleLinkRegularFile(historyPath); historyErr == nil {
			if string(existingHistory) != string(body) {
				return MigrationProfileConversionDecision{}, errors.New("archived prior profile decision does not match the decision being replaced")
			}
		} else if os.IsNotExist(historyErr) {
			if err := AtomicWrite(historyPath, body, 0o600); err != nil {
				return MigrationProfileConversionDecision{}, err
			}
		} else {
			return MigrationProfileConversionDecision{}, historyErr
		}
	} else if !os.IsNotExist(readErr) {
		return MigrationProfileConversionDecision{}, readErr
	} else if expectedPriorDigest != "" || decision.ReplacesDecisionDigest != "" {
		return MigrationProfileConversionDecision{}, errors.New("profile decision declares replacement lineage but no prior decision exists")
	}
	if err := AtomicWriteJSON(path, decision, 0o600); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	return decision, nil
}

func validateMigrationProfileDecisionSubmission(
	submission MigrationProfileDecisionSubmission,
	packet MigrationProfileConflictPacket,
) error {
	if submission.SchemaVersion != MigrationSchemaVersion || submission.PacketDigest == "" ||
		submission.PacketDigest != packet.Digest || submission.SourceFingerprint != packet.SourceFingerprint ||
		submission.SourcePath != packet.Conflict.SourcePath || submission.SourceDigest != packet.Conflict.SourceDigest {
		return errors.New("profile decision is not bound to the current conflict packet and legacy source")
	}
	if submission.BaselineProfileID != packet.Baseline.Profile.ProfileID ||
		submission.BaselineProfileDigest != packet.Baseline.ProfileDigest ||
		submission.EffectiveProfileName != packet.Baseline.EffectiveProfileName ||
		submission.EffectiveProfileDigest != packet.Baseline.EffectiveProfileDigest ||
		submission.MeasurementEvidenceDigest != packet.Baseline.MeasurementEvidenceDigest {
		return errors.New("profile decision does not bind the offered 0.8 baseline, effective profile, and measurement evidence")
	}
	if submission.Decision != migrationProfileDecisionKind || !submission.ExplicitManagerApproval || submission.Authority != "manager" {
		return errors.New("profile decision requires explicit manager approval to retain the packaged baseline")
	}
	if submission.ProjectProfileActivation {
		return errors.New("migration profile decision cannot activate or promote a project retrieval profile")
	}
	if submission.Reviewer == "" || submission.Rationale == "" {
		return errors.New("profile decision requires a manager identity and rationale")
	}
	if err := validateUTC(submission.DecidedAt); err != nil {
		return errors.New("profile decision requires an explicit UTC RFC3339 decidedAt")
	}
	if submission.ReplacesDecisionDigest != "" && !sha256IdentityRE.MatchString(submission.ReplacesDecisionDigest) {
		return errors.New("profile decision replacement lineage digest is malformed")
	}
	return nil
}

func validateMigrationProfileDecision(
	decision MigrationProfileConversionDecision,
	packet MigrationProfileConflictPacket,
) error {
	if decision.SchemaVersion != MigrationSchemaVersion || decision.PacketDigest != packet.Digest ||
		decision.ConflictDigest != packet.Conflict.Digest || decision.SourceFingerprint != packet.SourceFingerprint ||
		decision.SourcePath != packet.Conflict.SourcePath || decision.SourceDigest != packet.Conflict.SourceDigest {
		return errors.New("profile conversion decision source identity or conflict digest is stale")
	}
	if decision.BaselineProfileID != packet.Baseline.Profile.ProfileID ||
		decision.BaselineProfileDigest != packet.Baseline.ProfileDigest ||
		decision.EffectiveProfileName != packet.Baseline.EffectiveProfileName ||
		decision.EffectiveProfileDigest != packet.Baseline.EffectiveProfileDigest ||
		decision.MeasurementEvidenceDigest != packet.Baseline.MeasurementEvidenceDigest {
		return errors.New("profile conversion decision baseline identity or evidence digest is stale")
	}
	return validateMigrationProfileDecisionEnvelope(decision)
}

func loadMigrationProfileDecision(
	projectRoot string,
	packet MigrationProfileConflictPacket,
) (MigrationProfileConversionDecision, error) {
	body, err := readSingleLinkRegularFile(migrationProfileDecisionPath(projectRoot, packet.Conflict.SourcePath))
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	decision, err := decodeSealedMigrationProfileDecision(body)
	if err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	if err := validateMigrationProfileDecision(decision, packet); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	if err := validateMigrationProfileDecisionLineage(projectRoot, decision); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	return decision, nil
}

func validateMigrationProfileDecisionEnvelope(decision MigrationProfileConversionDecision) error {
	for label, digest := range map[string]string{
		"packet": decision.PacketDigest, "conflict": decision.ConflictDigest,
		"source fingerprint": decision.SourceFingerprint, "source": decision.SourceDigest,
		"baseline profile": decision.BaselineProfileDigest, "effective profile": decision.EffectiveProfileDigest,
		"measurement evidence": decision.MeasurementEvidenceDigest,
	} {
		if !sha256IdentityRE.MatchString(digest) {
			return fmt.Errorf("profile conversion decision %s digest is malformed", label)
		}
	}
	if decision.SourcePath != migrationLegacyRetrievalProfilePath ||
		!profileIdentityRE.MatchString(decision.BaselineProfileID) ||
		!managedSlugRE.MatchString(decision.EffectiveProfileName) {
		return errors.New("profile conversion decision profile identity is malformed")
	}
	measurementDigest, err := CanonicalDigest(decision.MeasurementEvidence)
	if err != nil || measurementDigest != decision.MeasurementEvidenceDigest ||
		decision.MeasurementEvidence.Status != "passed" {
		return errors.New("profile conversion decision measurement evidence does not match its passed digest")
	}
	if decision.Decision != migrationProfileDecisionKind || !decision.ExplicitManagerApproval ||
		decision.ProjectProfileActivation || decision.Authority != "manager" || decision.Reviewer == "" || decision.Rationale == "" {
		return errors.New("profile conversion decision lacks explicit non-activating manager authority")
	}
	if err := validateUTC(decision.DecidedAt); err != nil {
		return errors.New("profile conversion decision decidedAt is not UTC RFC3339")
	}
	if decision.ReplacesDecisionDigest != "" && !sha256IdentityRE.MatchString(decision.ReplacesDecisionDigest) {
		return errors.New("profile conversion decision replacement lineage digest is malformed")
	}
	return nil
}

func decodeSealedMigrationProfileDecision(body []byte) (MigrationProfileConversionDecision, error) {
	var decision MigrationProfileConversionDecision
	if err := decodeStrictJSON(body, &decision); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	expected := decision.Digest
	decision.Digest = ""
	digest, err := CanonicalDigest(decision)
	decision.Digest = expected
	if err != nil || expected == "" || digest != expected {
		return MigrationProfileConversionDecision{}, errors.New("profile conversion decision digest mismatch")
	}
	if err := validateMigrationProfileDecisionEnvelope(decision); err != nil {
		return MigrationProfileConversionDecision{}, err
	}
	return decision, nil
}

func validateMigrationProfileDecisionLineage(projectRoot string, decision MigrationProfileConversionDecision) error {
	seen := map[string]bool{decision.Digest: true}
	newer := decision
	for newer.ReplacesDecisionDigest != "" {
		if seen[newer.ReplacesDecisionDigest] {
			return errors.New("profile conversion decision replacement lineage contains a cycle")
		}
		body, err := readSingleLinkRegularFile(migrationProfileDecisionHistoryPath(
			projectRoot, newer.SourcePath, newer.ReplacesDecisionDigest,
		))
		if err != nil {
			return fmt.Errorf("read prior profile conversion decision %s: %w", newer.ReplacesDecisionDigest, err)
		}
		prior, err := decodeSealedMigrationProfileDecision(body)
		if err != nil || prior.Digest != newer.ReplacesDecisionDigest || prior.SourcePath != newer.SourcePath {
			return errors.New("profile conversion decision replacement lineage is invalid or mismatched")
		}
		if !migrationProfileDecisionTimeAfter(newer.DecidedAt, prior.DecidedAt) {
			return errors.New("profile conversion decision replacement lineage timestamps are not strictly increasing")
		}
		seen[prior.Digest] = true
		newer = prior
	}
	return nil
}

func migrationProfileDecisionTimeAfter(newer, older string) bool {
	newerTime, newerErr := time.Parse(time.RFC3339Nano, newer)
	olderTime, olderErr := time.Parse(time.RFC3339Nano, older)
	return newerErr == nil && olderErr == nil && newerTime.After(olderTime)
}

func migrationProfileDecisionPath(projectRoot, sourcePath string) string {
	key := strings.TrimPrefix(SHA256String("migration-profile-decision\x00"+NormalizeProjectPath(sourcePath)), "sha256:")
	return filepath.Join(projectRoot, ".re-discipline", "migration", "0.8", "profile-decisions", key+".json")
}

func migrationProfileDecisionHistoryPath(projectRoot, sourcePath, decisionDigest string) string {
	key := strings.TrimSuffix(filepath.Base(migrationProfileDecisionPath("", sourcePath)), ".json")
	digest := strings.TrimPrefix(decisionDigest, "sha256:")
	return filepath.Join(projectRoot, ".re-discipline", "migration", "0.8", "profile-decisions", "history", key, digest+".json")
}

func migrationProfileAuditDecisionPath(sourcePath string) string {
	return filepath.ToSlash(filepath.Join(
		".re-discipline", "knowledge", "migration", "profile-decisions",
		filepath.Base(migrationProfileDecisionPath("", sourcePath)),
	))
}

// sourcesAsRead returns the inventory with planned destinations cleared, so a
// fingerprint over it describes only what was discovered in the project.
func sourcesAsRead(sources []MigrationSource) []MigrationSource {
	projection := make([]MigrationSource, len(sources))
	copy(projection, sources)
	for index := range projection {
		projection[index].Destination = ""
	}
	return projection
}
