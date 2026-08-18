package knowledge

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBenchmarkStatusBindsPackagedFindingSuite(t *testing.T) {
	assetRoot := adversarialAssetRoot(t)
	cases, err := LoadEvalCases(filepath.Join(
		assetRoot, "evals", "conformance", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	findingSuite, err := LoadFindingEvalSuite(filepath.Join(
		assetRoot, "evals", "conformance", "finding-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	evalFingerprint, err := CanonicalDigest(struct {
		PassageCases []EvalCase       `json:"passageCases"`
		FindingSuite FindingEvalSuite `json:"findingSuite"`
	}{cases, findingSuite})
	if err != nil {
		t.Fatal(err)
	}
	runtimeIdentity := RuntimeIdentity{}
	runtimeFingerprint, err := CanonicalDigest(RuntimeContract(runtimeIdentity))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{AssetRoot: assetRoot}
	evidence := BenchmarkEvidence{
		Suite: "packaged-conformance-v1", Digest: "sha256:" + strings.Repeat("a", 64),
		Status: "passed", EvaluatedAt: time.Now().UTC().Format(time.RFC3339),
		EvalFingerprint: evalFingerprint, ModelFingerprint: mustDigest(service.ModelManifest.Models),
		RuntimeFingerprint: runtimeFingerprint,
	}

	status := service.benchmarkStatus(evidence, Generation{}, runtimeIdentity)
	if status["stale"] != false {
		t.Fatalf("current passage-plus-finding fingerprint reported stale: %+v", status)
	}

	// A passage-only fingerprint was valid before finding-card evaluation became
	// a release gate. It must now be visibly stale rather than silently treating
	// an unevaluated normalized plane as current evidence.
	passageOnly, err := CanonicalDigest(cases)
	if err != nil {
		t.Fatal(err)
	}
	evidence.EvalFingerprint = passageOnly
	status = service.benchmarkStatus(evidence, Generation{}, runtimeIdentity)
	reasons, ok := status["informationalStaleReasons"].([]string)
	if !ok || !contains(reasons, "eval-fingerprint") {
		t.Fatalf("passage-only fingerprint did not report finding-suite drift: %+v", status)
	}
}
