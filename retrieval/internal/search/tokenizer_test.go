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
