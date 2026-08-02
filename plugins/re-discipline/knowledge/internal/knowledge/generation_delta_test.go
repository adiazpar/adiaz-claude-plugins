package knowledge

import "testing"

// The recorded chain is short by design. Composing across it must fold hops
// into one net answer rather than replaying each publication at the caller.
func TestComposeGenerationDeltaFoldsAndRefusesBeyondHistory(t *testing.T) {
	history := []GenerationDelta{
		{
			Generation: "generation-aaaaaaaaaaaaaaaaaaaa", PreviousGeneration: "",
			Added: []DocumentRef{{Path: "docs/truth/a.md", Tier: "truth"}},
		},
		{
			Generation:         "generation-bbbbbbbbbbbbbbbbbbbb",
			PreviousGeneration: "generation-aaaaaaaaaaaaaaaaaaaa",
			Added:              []DocumentRef{{Path: "docs/truth/b.md", Tier: "truth"}},
			Changed:            []DocumentRef{{Path: "docs/truth/a.md", Tier: "truth"}},
		},
		{
			Generation:         "generation-cccccccccccccccccccc",
			PreviousGeneration: "generation-bbbbbbbbbbbbbbbbbbbb",
			Removed:            []DocumentRef{{Path: "docs/truth/b.md", Tier: "truth"}},
			Changed:            []DocumentRef{{Path: "docs/truth/c.md", Tier: "truth"}},
		},
	}

	composed, ok := ComposeGenerationDelta(
		history, "generation-aaaaaaaaaaaaaaaaaaaa",
		"generation-cccccccccccccccccccc")
	if !ok {
		t.Fatal("a chain covering the requested span reported unavailable")
	}
	// b.md was added and then removed inside the span. A caller who never saw
	// it must not be told about it in either direction.
	for _, ref := range append(append(
		append([]DocumentRef{}, composed.Added...), composed.Changed...),
		composed.Removed...) {
		if ref.Path == "docs/truth/b.md" {
			t.Fatalf("a document added and removed within the span survived: %+v", ref)
		}
	}
	if len(composed.Changed) != 2 {
		t.Fatalf("composed changed = %+v, want a.md and c.md", composed.Changed)
	}
	if len(composed.Added) != 0 || len(composed.Removed) != 0 {
		t.Fatalf("composed delta = %+v", composed)
	}

	if _, ok := ComposeGenerationDelta(
		history, "generation-dddddddddddddddddddd",
		"generation-cccccccccccccccccccc"); ok {
		t.Fatal("a generation outside the chain was answered instead of refused")
	}

	// A truncated hop cannot be composed honestly: absence of a path in it is
	// not evidence the path did not change.
	truncated := append([]GenerationDelta(nil), history...)
	truncated[1].Truncated = true
	if _, ok := ComposeGenerationDelta(
		truncated, "generation-aaaaaaaaaaaaaaaaaaaa",
		"generation-cccccccccccccccccccc"); ok {
		t.Fatal("a truncated hop was composed into a complete-looking answer")
	}
}
