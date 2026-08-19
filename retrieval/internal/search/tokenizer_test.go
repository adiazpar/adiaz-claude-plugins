package search

import (
	"slices"
	"strings"
	"testing"
)

func TestExpandIdentifiers(t *testing.T) {
	got := ExpandIdentifiers("call idAnimatedEntity::AttachJoint and spawn_limit here")
	for _, want := range []string{"idanimatedentity", "attachjoint", "animated", "entity", "attach", "joint", "spawn", "limit", "spawn_limit"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if slices.Contains(got, "call") {
		t.Fatalf("plain words must not be expanded: %v", got)
	}
}

func TestExpandIdentifiersDropsFunctionWordSegments(t *testing.T) {
	got := ExpandIdentifiers("call idSnapMap::RepairAndMigrate and signInWhenOnline")
	for _, want := range []string{"repairandmigrate", "repair", "migrate", "signinwhenonline", "sign", "online"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	for _, junk := range []string{"and", "in", "when"} {
		if slices.Contains(got, junk) {
			t.Fatalf("function-word segment %q must be dropped: %v", junk, got)
		}
	}
}

func TestBuildMatchDropsFunctionWords(t *testing.T) {
	m := BuildMatch("how does the RepairAndMigrate validator work")
	for _, junk := range []string{`"how"`, `"does"`, `"the"`, `"and"`} {
		if strings.Contains(m, junk) {
			t.Fatalf("function word %s must be dropped: %s", junk, m)
		}
	}
	for _, want := range []string{`"repairandmigrate"`, `"validator"`, `"work"`, `"repair"`, `"migrate"`} {
		if !strings.Contains(m, want) {
			t.Fatalf("missing %s in %s", want, m)
		}
	}
	// Negations are NOT stopwords: negative results are first-class docs.
	if m := BuildMatch("inherit is not required"); !strings.Contains(m, `"not"`) {
		t.Fatalf("negation must survive: %s", m)
	}
	// An all-function-word question falls back to the unfiltered terms
	// instead of returning nothing.
	if m := BuildMatch("what is this for"); m != `"what" OR "is" OR "this" OR "for"` {
		t.Fatalf("all-stopword fallback: %s", m)
	}
}

func TestBuildMatch(t *testing.T) {
	m := BuildMatch(`how do I bind entities to AttachJoint? "quotes" too`)
	if !strings.Contains(m, `"attachjoint"`) || !strings.Contains(m, `"entities"`) {
		t.Fatalf("match: %s", m)
	}
	if !strings.Contains(m, " OR ") {
		t.Fatalf("terms must be OR-joined: %s", m)
	}
	if strings.Contains(m, `""quotes""`) {
		t.Fatalf("embedded quotes must be stripped, got %s", m)
	}
	if BuildMatch("???") != "" {
		t.Fatal("unusable query must return empty match")
	}
}
