package search

import (
	"path/filepath"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestRunBenchFixtureAllPass(t *testing.T) {
	root := fixtureRoot(t)
	report, err := RunBench(root, filepath.Join(root, ".re-discipline", "golden.jsonl"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.Passed != 3 {
		t.Fatalf("report: %+v", report)
	}
}
