package knowledge

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const IdentifierAnalyzerVersion = "identifier-analysis-v1"

type IdentifierToken struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
}

var (
	identifierLexemeRE = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_./:+@-]{0,255}`)
	explicitHexRE      = regexp.MustCompile(`(?i)^0x([0-9a-f]+)$`)
	ghidraHexRE        = regexp.MustCompile(`(?i)^FUN_([0-9a-f]+)$`)
)

// AnalyzeIdentifierText produces raw lexical forms plus deterministic split
// and address forms. Explicit 0x and Ghidra FUN_ identifiers share one hex:
// key; unmarked decimal or hex-looking words are never coerced into addresses.
func AnalyzeIdentifierText(value string) []IdentifierToken {
	seen := map[string]map[string]bool{}
	add := func(token, kind string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if len(token) < 2 || len(token) > 256 {
			return
		}
		if seen[token] == nil {
			seen[token] = map[string]bool{}
		}
		seen[token][kind] = true
	}
	for _, raw := range identifierLexemeRE.FindAllString(value, -1) {
		add(raw, "raw")
		analyzeIdentifierComponent(raw, add)
		for _, component := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == '_' || r == '-' || r == '.' || r == '/' || r == '\\' ||
				r == ':' || r == '+' || r == '@'
		}) {
			add(component, "split")
			analyzeIdentifierComponent(component, add)
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	result := []IdentifierToken{}
	for _, value := range values {
		kinds := make([]string, 0, len(seen[value]))
		for kind := range seen[value] {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			result = append(result, IdentifierToken{Value: value, Kind: kind})
		}
	}
	return result
}

func analyzeIdentifierComponent(value string, add func(string, string)) {
	if match := explicitHexRE.FindStringSubmatch(value); match != nil {
		canonical := canonicalHex(match[1])
		add(canonical, "split")
		add("hex:"+canonical, "hex")
	}
	if match := ghidraHexRE.FindStringSubmatch(value); match != nil {
		canonical := canonicalHex(match[1])
		add("fun", "split")
		add(canonical, "split")
		add("hex:"+canonical, "hex")
	}
	for _, part := range splitCamelIdentifier(value) {
		add(part, "split")
	}
}

func canonicalHex(value string) string {
	value = strings.ToLower(value)
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

func splitCamelIdentifier(value string) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	parts := []string{}
	start := 0
	flush := func(end int) {
		if end > start {
			parts = append(parts, string(runes[start:end]))
		}
		start = end
	}
	for index := 1; index < len(runes); index++ {
		previous, current := runes[index-1], runes[index]
		nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		switch {
		case unicode.IsLower(previous) && unicode.IsUpper(current):
			flush(index)
		case unicode.IsUpper(previous) && unicode.IsUpper(current) && nextLower:
			flush(index)
		case unicode.IsLetter(previous) && unicode.IsDigit(current),
			unicode.IsDigit(previous) && unicode.IsLetter(current):
			flush(index)
		}
	}
	flush(len(runes))
	return parts
}

func IdentifierTerms(value string) []string {
	seen := map[string]bool{}
	for _, token := range AnalyzeIdentifierText(value) {
		seen[token.Value] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
