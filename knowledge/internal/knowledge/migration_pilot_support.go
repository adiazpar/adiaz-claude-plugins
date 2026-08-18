package knowledge

import (
	"errors"
	"fmt"
)

// BuildMigrationIntrinsicGateArtifact derives the structural or semantic
// traversal gate envelope directly from the activated disposable project.
// RecordGate and Verify independently recompute the same state before they
// accept the artifact.
func BuildMigrationIntrinsicGateArtifact(
	projectRoot string,
	assetRoot string,
	gate string,
) (MigrationGateArtifact, error) {
	if gate != "structural" && gate != "semantic-traversal" {
		return MigrationGateArtifact{}, errors.New("intrinsic gate must be structural or semantic-traversal")
	}
	engine, err := NewMigrationEngineWithAssetRoot(projectRoot, assetRoot)
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	state, err := engine.Status()
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	if state.State != "physically-reorganized" && state.State != "traversal-verified" {
		return MigrationGateArtifact{}, errors.New("intrinsic gates require a physically reorganized migration")
	}
	plan, err := engine.loadPlan()
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	if err := validateMigrationPlanStateBinding(plan, state); err != nil {
		return MigrationGateArtifact{}, err
	}
	if err := engine.verifyMigrationStructural(plan); err != nil {
		return MigrationGateArtifact{}, fmt.Errorf("structural verification: %w", err)
	}
	if err := engine.verifyMigrationTraversal(plan); err != nil {
		return MigrationGateArtifact{}, fmt.Errorf("semantic traversal verification: %w", err)
	}
	evidence := MigrationCertification{
		SchemaVersions: map[string]int{
			"migration": MigrationSchemaVersion,
			"campaign":  CampaignSchemaVersion,
			"bootstrap": BootstrapSchemaVersion,
			"knowledge": SettingsSchemaVersion,
		},
		GateReceipts:     []MigrationGateReceipt{},
		KnownLimitations: []string{},
	}
	if err := engine.populateMigrationCertificationEvidence(&evidence, plan, state); err != nil {
		return MigrationGateArtifact{}, err
	}
	schemaFingerprint, err := CanonicalDigest(evidence.SchemaVersions)
	if err != nil {
		return MigrationGateArtifact{}, err
	}
	known := map[string]string{
		"schema":            schemaFingerprint,
		"destination-state": evidence.DestinationStateHead,
		"inventory":         evidence.InventoryDigest,
		"coverage":          evidence.CoverageDigest,
	}
	artifact := MigrationGateArtifact{
		SchemaVersion: MigrationSchemaVersion,
		TransactionID: state.TransactionID,
		PlanDigest:    state.PlanDigest,
		Gate:          gate,
		Passed:        true,
		Checks:        []MigrationGateCheck{},
		Fingerprints:  map[string]string{},
	}
	for _, name := range requiredMigrationGateChecks(gate) {
		artifact.Checks = append(artifact.Checks, MigrationGateCheck{
			Name: name, Passed: true,
			Evidence: fmt.Sprintf("engine-derived %s check at %s", name, state.Digest),
		})
	}
	for _, name := range requiredMigrationGateFingerprints(gate) {
		value := known[name]
		if value == "" {
			return MigrationGateArtifact{}, fmt.Errorf("intrinsic fingerprint %s is empty", name)
		}
		artifact.Fingerprints[name] = value
	}
	artifact.ResultDigest, err = CanonicalDigest(artifact)
	return artifact, err
}
