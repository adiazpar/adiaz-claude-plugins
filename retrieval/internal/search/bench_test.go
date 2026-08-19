package search

import (
	"os"
	"path/filepath"
	"strings"
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

func writeGolden(t *testing.T, root string, lines ...string) string {
	t.Helper()
	p := filepath.Join(root, ".re-discipline", "golden.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Symbol cases run through LookupSymbol: exact names pass, substring
// fragments pass via the fallback, and wrong expectations miss.
func TestRunBenchSymbolCases(t *testing.T) {
	root := buildTestCorpus(t)
	writeSymbols(t, root, testSymbolLines()...)
	golden := writeGolden(t, root,
		`{"symbol": "idLangDict_langEntry_t", "expect": "idLangDict_langEntry_t"}`,
		`{"symbol": "langEntry", "expect": "idLangDict_langEntry_t"}`,
		`{"symbol": "TAG_LANGDICT", "expect": "NO_SUCH_NAME"}`,
	)
	report, err := RunBench(root, golden, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.Passed != 2 {
		t.Fatalf("report: %+v", report)
	}
	if len(report.Misses) != 1 || report.Misses[0].Symbol != "TAG_LANGDICT" {
		t.Fatalf("misses: %+v", report.Misses)
	}
}

// A line with both q and symbol, or with neither, is malformed and
// counts as a miss without aborting the run.
func TestRunBenchMalformedSymbolLines(t *testing.T) {
	root := buildTestCorpus(t)
	writeSymbols(t, root, testSymbolLines()...)
	golden := writeGolden(t, root,
		`{"q": "spawn limit", "symbol": "MAX_", "expect": "docs/engine/spawn-limit.md"}`,
		`{"expect": "docs/engine/spawn-limit.md"}`,
		`{"symbol": "MAX_", "expect": "MAX_"}`,
	)
	report, err := RunBench(root, golden, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.Passed != 1 {
		t.Fatalf("report: %+v", report)
	}
	for _, m := range report.Misses {
		if m.Expect != "(malformed line)" {
			t.Fatalf("expected malformed markers, got: %+v", report.Misses)
		}
	}
}
