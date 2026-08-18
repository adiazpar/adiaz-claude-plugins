package search

import (
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_:]*[A-Za-z0-9]`)
var camelRe = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[a-z0-9]*)`)
var wordRe = regexp.MustCompile(`[A-Za-z0-9_:]+`)

// isIdentifier reports whether tok looks like a code identifier rather
// than a plain word: contains ::, _, or an internal case transition.
func isIdentifier(tok string) bool {
	if strings.Contains(tok, "::") || strings.Contains(tok, "_") {
		return true
	}
	hasLower, hasUpper := false, false
	for _, r := range tok[1:] { // ignore leading capital of ordinary words
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}

// ExpandIdentifiers returns lowercased whole identifiers and their split
// parts for every code-identifier token in text, deduplicated.
func ExpandIdentifiers(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.ToLower(s)
		if len(s) > 1 && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, tok := range identRe.FindAllString(text, -1) {
		if !isIdentifier(tok) {
			continue
		}
		add(strings.ReplaceAll(strings.ReplaceAll(tok, "::", ""), "_", ""))
		add(tok) // whole, verbatim-lowercased (keeps spawn_limit findable)
		for _, seg := range strings.FieldsFunc(tok, func(r rune) bool { return r == ':' || r == '_' }) {
			add(seg)
			for _, part := range camelRe.FindAllString(seg, -1) {
				add(part)
			}
		}
	}
	return out
}

// BuildMatch converts a free-text question into a safe FTS5 MATCH
// expression: quoted terms OR-joined. Empty string means "nothing usable".
func BuildMatch(q string) string {
	seen := map[string]bool{}
	var terms []string
	add := func(s string) {
		s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, `"`, ""), ":", " "))
		for _, w := range strings.Fields(s) {
			if len(w) > 1 && !seen[w] {
				seen[w] = true
				terms = append(terms, `"`+w+`"`)
			}
		}
	}
	for _, tok := range wordRe.FindAllString(q, -1) {
		add(tok)
	}
	for _, part := range ExpandIdentifiers(q) {
		add(part)
	}
	return strings.Join(terms, " OR ")
}
