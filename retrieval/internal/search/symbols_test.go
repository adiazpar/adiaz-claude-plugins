package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSymbols(t *testing.T, root string, lines ...string) {
	t.Helper()
	p := SymbolsPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testSymbolLines() []string {
	return []string{
		`{"name":"idLangDict_langEntry_t","kind":"struct","render":"idLangDict_langEntry_t  size 32\n  id      unsigned int    +0x00  4","source":"reference/idlib_schema.json"}`,
		`{"name":"TAG_LANGDICT","kind":"constant","render":"TAG_LANGDICT  int = 150","source":"reference/enumvals_schema.json"}`,
		`{"name":"MAX_","kind":"group","render":"MAX_  group, 2 constants\n  MAX_PLAYERS  int = 4","source":"reference/enumvals_schema.json"}`,
		`{"name":"AB_CD","kind":"constant","render":"AB_CD  int = 1"}`,
		`{"name":"ABXCD","kind":"constant","render":"ABXCD  int = 2"}`,
	}
}

// The symbols file is optional: no file means no symbols table rows,
// no warnings, and lookups that simply return nothing.
func TestSymbolsFileOptional(t *testing.T) {
	root := buildTestCorpus(t)
	if meta := ScanSymbols(root); meta != nil {
		t.Fatalf("missing file must scan as nil: %+v", meta)
	}
	syms, warnings := LoadSymbols(root)
	if syms != nil || warnings != nil {
		t.Fatalf("missing file must load empty: %v %v", syms, warnings)
	}
	res, _, err := LookupSymbol(root, "anything", 5)
	if err != nil {
		t.Fatalf("lookup without symbols must not error: %v", err)
	}
	if res.Total != 0 || len(res.Symbols) != 0 {
		t.Fatalf("lookup without symbols must be empty: %+v", res)
	}
	if out := FormatSymbols("anything", res); !strings.Contains(out, "No symbol matches") {
		t.Fatalf("empty format: %q", out)
	}
}

func TestLookupSymbolExactCaseInsensitive(t *testing.T) {
	root := buildTestCorpus(t)
	writeSymbols(t, root, testSymbolLines()...)
	for _, q := range []string{"idLangDict_langEntry_t", "IDLANGDICT_LANGENTRY_T", "idlangdict_langentry_t"} {
		res, _, err := LookupSymbol(root, q, 5)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Exact || res.Total != 1 || res.Symbols[0].Name != "idLangDict_langEntry_t" {
			t.Fatalf("lookup %q: %+v", q, res)
		}
		if !strings.Contains(res.Symbols[0].Render, "+0x00") {
			t.Fatalf("render lost: %q", res.Symbols[0].Render)
		}
	}
	out := FormatSymbols("TAG_LANGDICT", mustLookup(t, root, "TAG_LANGDICT"))
	if !strings.Contains(out, "[constant]") || !strings.Contains(out, "TAG_LANGDICT  int = 150") {
		t.Fatalf("exact format: %q", out)
	}
}

func mustLookup(t *testing.T, root, name string) SymbolHits {
	t.Helper()
	res, _, err := LookupSymbol(root, name, 5)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// No exact match falls back to substring matching; LIKE metacharacters
// in the queried name are literal (AB_CD's underscore must not match
// ABXCD), and the compact fallback rendering lists names.
func TestLookupSymbolSubstringFallback(t *testing.T) {
	root := buildTestCorpus(t)
	writeSymbols(t, root, testSymbolLines()...)

	res := mustLookup(t, root, "langEntry")
	if res.Exact || res.Total != 1 || res.Symbols[0].Name != "idLangDict_langEntry_t" {
		t.Fatalf("substring fallback: %+v", res)
	}
	out := FormatSymbols("langEntry", res)
	if !strings.Contains(out, "No exact match") || !strings.Contains(out, "idLangDict_langEntry_t") {
		t.Fatalf("fallback format: %q", out)
	}

	res = mustLookup(t, root, "B_C")
	if res.Total != 1 || res.Symbols[0].Name != "AB_CD" {
		t.Fatalf("underscore must be literal in fallback: %+v", res)
	}

	// Limit truncates but Total reports the full count.
	many, _, err := LookupSymbol(root, "A", 2)
	if err != nil {
		t.Fatal(err)
	}
	if many.Exact || len(many.Symbols) != 2 || many.Total <= 2 {
		t.Fatalf("limit/total: %+v", many)
	}
	if out := FormatSymbols("A", many); !strings.Contains(out, "more") {
		t.Fatalf("truncation note missing: %q", out)
	}
}

// Editing, adding, or deleting symbols.jsonl must flip the manifest
// staleness check exactly like a doc edit does.
func TestEnsureFreshTracksSymbolsFile(t *testing.T) {
	root := buildTestCorpus(t)
	EnsureFresh(root)

	// Adding the file triggers a rebuild that indexes the symbols.
	writeSymbols(t, root, testSymbolLines()...)
	res := mustLookup(t, root, "TAG_LANGDICT")
	if !res.Exact || res.Total != 1 {
		t.Fatalf("symbols must be indexed after file appears: %+v", res)
	}

	// Editing the file re-indexes its content.
	writeSymbols(t, root,
		`{"name":"TAG_LANGDICT","kind":"constant","render":"TAG_LANGDICT  int = 151","source":"reference/enumvals_schema.json"}`)
	res = mustLookup(t, root, "TAG_LANGDICT")
	if res.Total != 1 || !strings.Contains(res.Symbols[0].Render, "151") {
		t.Fatalf("edit must reindex: %+v", res)
	}

	// Deleting the file empties the table without erroring.
	if err := os.Remove(SymbolsPath(root)); err != nil {
		t.Fatal(err)
	}
	res = mustLookup(t, root, "TAG_LANGDICT")
	if res.Total != 0 {
		t.Fatalf("deletion must drop symbols: %+v", res)
	}
}

// Malformed lines degrade to a summary warning, never a failed build.
func TestLoadSymbolsMalformedWarns(t *testing.T) {
	root := buildTestCorpus(t)
	writeSymbols(t, root,
		`{"name":"GOOD","kind":"constant","render":"GOOD  int = 1"}`,
		`not json at all`,
		`{"name":"","render":"nameless"}`,
		`{"name":"ALSO_GOOD","kind":"constant","render":"ALSO_GOOD  int = 2"}`,
	)
	syms, warnings := LoadSymbols(root)
	if len(syms) != 2 || syms[0].Name != "GOOD" || syms[1].Name != "ALSO_GOOD" {
		t.Fatalf("syms: %+v", syms)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "2 malformed") {
		t.Fatalf("warnings: %v", warnings)
	}
	// The build carries the warning through and still indexes the rest.
	dbPath := filepath.Join(root, ".re-discipline", "index.db")
	_, buildWarnings, err := BuildIndexFile(root, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range buildWarnings {
		if strings.Contains(w, "malformed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("build must surface symbol warnings: %v", buildWarnings)
	}
	if n, err := CountSymbols(dbPath); err != nil || n != 2 {
		t.Fatalf("count: %d %v", n, err)
	}
}
