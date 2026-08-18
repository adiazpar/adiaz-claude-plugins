package knowledge

import "testing"

func TestIdentifierAnalysisPreservesRawSplitsAndCanonicalHex(t *testing.T) {
	tokens := AnalyzeIdentifierText("idSnapEditorLocal+0x00023618 FUN_141A08E80 get_connections-for/source")
	values := map[string]map[string]bool{}
	for _, token := range tokens {
		if values[token.Value] == nil {
			values[token.Value] = map[string]bool{}
		}
		values[token.Value][token.Kind] = true
	}
	for _, expected := range []struct {
		value string
		kind  string
	}{
		{"idsnapeditorlocal+0x00023618", "raw"},
		{"idsnapeditorlocal", "split"},
		{"snap", "split"},
		{"editor", "split"},
		{"local", "split"},
		{"0x00023618", "split"},
		{"hex:23618", "hex"},
		{"fun_141a08e80", "raw"},
		{"hex:141a08e80", "hex"},
		{"connections", "split"},
		{"source", "split"},
	} {
		if !values[expected.value][expected.kind] {
			t.Errorf("missing %s token %q in %#v", expected.kind, expected.value, tokens)
		}
	}
}

func TestIdentifierAnalysisDoesNotCoerceBareNumbersOrHexWords(t *testing.T) {
	for _, token := range AnalyzeIdentifierText("141a08e80 123456 decimal deadbeef") {
		if token.Kind == "hex" {
			t.Fatalf("unmarked token was coerced to hex: %#v", token)
		}
	}
}

func TestIdentifierTermsAreDeterministicAndUnique(t *testing.T) {
	first := IdentifierTerms("DeclSourceRebuild decl_source-rebuild")
	second := IdentifierTerms("DeclSourceRebuild decl_source-rebuild")
	if len(first) != len(second) {
		t.Fatalf("term count drifted: %v versus %v", first, second)
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("terms are not deterministic: %v versus %v", first, second)
		}
		if index > 0 && first[index] <= first[index-1] {
			t.Fatalf("terms are not sorted unique: %v", first)
		}
	}
}
