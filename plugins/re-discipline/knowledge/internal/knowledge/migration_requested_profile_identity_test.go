package knowledge

import "testing"

// The migration validator's expectation for the benchmark's requested-profile
// identity must be derived exactly the way the runtime derives the identity
// it stamps into every report: profile ID at the canonical digest of the
// semantic profile. If the two derivations diverge, no honest benchmark can
// ever satisfy the retrieval-context gate.
func TestMigrationRequestedProfileExpectationMatchesRuntimeIdentity(t *testing.T) {
	baseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		t.Fatalf("load packaged baseline: %v", err)
	}
	manifest, err := migrationPackagedModelManifest()
	if err != nil {
		t.Fatalf("load packaged model manifest: %v", err)
	}
	runtime, err := ProbeRuntimeIdentity(manifest)
	if err != nil {
		t.Fatalf("probe runtime: %v", err)
	}
	selected, err := SelectEffectiveProfile(baseline.Profile, manifest, runtime)
	if err != nil {
		t.Fatalf("select packaged profile: %v", err)
	}
	requestedDigest, err := CanonicalDigest(semanticProfile(baseline.Profile))
	if err != nil {
		t.Fatalf("digest semantic baseline: %v", err)
	}
	expected := baseline.Profile.ProfileID + "@" + requestedDigest
	if selected.RequestedIdentity != expected {
		t.Fatalf("validator expectation %q does not match the runtime identity %q",
			expected, selected.RequestedIdentity)
	}
}
