package search

import (
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_:]*[A-Za-z0-9]`)
var camelRe = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[a-z0-9]*)`)
var wordRe = regexp.MustCompile(`[A-Za-z0-9_:]+`)

// stopTerms are English function words that OR-match nearly every doc
// and only distort ranking: camel-splitting RepairAndMigrate must not
// emit "and", and a natural question's "how are the" carries no signal.
// Deliberately small and function-word only — negations ("no", "not")
// stay searchable because negative results are first-class docs, and
// domain words are never filtered.
var stopTerms = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "be": true, "but": true, "by": true, "can": true,
	"do": true, "does": true, "for": true, "from": true, "had": true,
	"has": true, "have": true, "how": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "of": true,
	"on": true, "or": true, "that": true, "the": true, "these": true,
	"this": true, "those": true, "to": true, "was": true, "were": true,
	"what": true, "when": true, "where": true, "which": true,
	"why": true, "will": true, "with": true, "you": true, "your": true,
}

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
// parts for every code-identifier token in text, deduplicated. Split
// segments that are bare function words (the "And" of RepairAndMigrate)
// are dropped; whole identifiers are never function words.
func ExpandIdentifiers(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.ToLower(s)
		if len(s) > 1 && !seen[s] && !stopTerms[s] {
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
// Function words are filtered from the question's terms; if that would
// leave nothing, the unfiltered terms are used so an all-function-word
// question still degrades to the old behavior instead of zero results.
func BuildMatch(q string) string {
	terms, _ := AnalyzeQuery(q)
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " OR ")
}

// AnalyzeQuery splits a question exactly as BuildMatch does, but reports
// both halves: the terms that will be searched for, and the function
// words discarded on the way. Explain surfaces the discarded half,
// because "why did my question not find it" is usually answered by
// seeing which words never reached the index.
func AnalyzeQuery(q string) (terms, dropped []string) {
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, `"`, ""), ":", " "))
		for _, w := range strings.Fields(s) {
			if len(w) <= 1 || seen[w] {
				continue
			}
			seen[w] = true
			if stopTerms[w] {
				dropped = append(dropped, w)
			} else {
				terms = append(terms, w)
			}
		}
	}
	for _, tok := range wordRe.FindAllString(q, -1) {
		add(tok)
	}
	for _, part := range ExpandIdentifiers(q) {
		add(part)
	}
	// A question made entirely of function words would otherwise search
	// for nothing; fall back to them rather than return no results.
	if len(terms) == 0 {
		terms, dropped = dropped, nil
	}
	return terms, dropped
}
