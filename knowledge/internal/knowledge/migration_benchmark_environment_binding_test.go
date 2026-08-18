package knowledge

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A project with zero finding-evaluation suites produces a benchmark report
// whose findingSuiteDigests marshals as the empty JSON array, while the
// independently rederived certification environment carries a nil slice for
// the same empty corpus. The environment binding must treat those as the
// same honest emptiness - the calibration side already normalizes this
// exact representation hazard - or no such project can ever record its
// retrieval-context gate.
func TestBenchmarkEnvironmentBindingAcceptsEmptyFindingSuites(t *testing.T) {
	root := t.TempDir()
	environment := MigrationCertificationEnvironment{
		ProjectRoot:       root,
		CorpusFingerprint: "sha256:" + strings.Repeat("11", 32),
		EvalFingerprint:   "sha256:" + strings.Repeat("22", 32),
		// nil: the environment builder appends onto a nil slice.
		FindingSuiteDigests: nil,
		ModelFingerprint:    "sha256:" + strings.Repeat("33", 32),
		ParserVersion:       ParserVersion,
		ChunkerVersion:      ChunkerVersion,
		DocumentCount:       3, ChunkCount: 9,
		RuntimeContract: RuntimeContract(RuntimeIdentity{}),
	}
	report := ProjectBenchmarkReport{
		Generation: GenerationSummary{
			ID:                "generation-0123456789abcdef0123",
			CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
			Project:           filepath.Base(root),
			Worktree:          "worktree:" + SHA256String(root)[:16],
			GitRevision:       "unversioned",
			DirtyFingerprint:  "unversioned",
			CorpusFingerprint: environment.CorpusFingerprint,
			ModelFingerprint:  environment.ModelFingerprint,
			ParserVersion:     environment.ParserVersion,
			ChunkerVersion:    environment.ChunkerVersion,
			DocumentCount:     environment.DocumentCount,
			ChunkCount:        environment.ChunkCount,
		},
		EvalFingerprint: environment.EvalFingerprint,
		// Empty but non-nil: exactly what decoding the report's marshaled
		// findingSuiteDigests: [] produces.
		FindingSuiteDigests:  []string{},
		HardNegativeCoverage: MeasureHardNegativeCoverage(environment.EvalCases),
	}
	if err := validateMigrationBenchmarkEnvironment(report, environment); err != nil {
		t.Fatalf("empty-but-non-nil finding suite digests must bind to a nil environment set: %v", err)
	}

	report.FindingSuiteDigests = []string{"sha256:" + strings.Repeat("44", 32)}
	if err := validateMigrationBenchmarkEnvironment(report, environment); err == nil {
		t.Fatal("a genuinely differing finding suite set must still fail the binding")
	}
}
