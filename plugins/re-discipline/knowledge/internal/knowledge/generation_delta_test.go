package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func orientDelta(t *testing.T, server *MCPServer, root, since string) map[string]any {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{
		"projectRoot": root, "sinceGeneration": since,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := server.callTool(context.Background(), "orient", arguments)
	if err != nil {
		t.Fatal(err)
	}
	// Read it the way a client does: through the serialized tool result, so the
	// assertions bind to the wire shape rather than to in-process Go types.
	object, err := toObject(value)
	if err != nil {
		t.Fatalf("orient with sinceGeneration returned an unserializable result: %v", err)
	}
	delta, ok := object["delta"].(map[string]any)
	if !ok {
		t.Fatalf("orient omitted the delta: %#v", object)
	}
	if _, present := object["passages"]; !present {
		t.Fatal("orient dropped the pack when asked for a delta")
	}
	return delta
}

// A session resuming days later asks what moved. The delta exists only during a
// publication, so if nobody records it the honest answer is "unavailable" - and
// an honest "unavailable" is worth far more than a confident "nothing changed".
func TestOrientServesRecordedGenerationDeltas(t *testing.T) {
	root := makeAdversarialProject(t)
	server := &MCPServer{
		AssetRoot: adversarialAssetRoot(t), InitialRoot: root, initialized: true,
	}
	server.services = map[string]*Service{}
	server.serviceStamps = map[string]string{}
	server.preflightedRoots = map[string]bool{}
	managed, err := server.preflightRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	server.configuredRoots = []string{managed}
	ctx := context.Background()

	service, err := server.service(managed)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _, err := service.Index.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Asking about the generation you are already on is answerable and empty.
	same := orientDelta(t, server, managed, first.ID)
	if same["available"] != true {
		t.Fatalf("a delta against the current generation was unavailable: %#v", same)
	}
	if len(same["added"].([]any)) != 0 || len(same["changed"].([]any)) != 0 {
		t.Fatalf("the current generation differed from itself: %#v", same)
	}

	// A generation nobody published: the chain cannot reach it, and the server
	// must say so instead of returning an empty delta that reads as "no change".
	missing := orientDelta(t, server, managed,
		"generation-0123456789abcdef0123")
	if missing["available"] != false {
		t.Fatalf("an unreachable generation returned a delta: %#v", missing)
	}
	if missing["reason"] == "" || missing["reason"] == nil {
		t.Fatal("an unavailable delta gave no reason")
	}

	addedPath := filepath.Join(managed, "docs", "truth", "codec.md")
	if err := os.WriteFile(addedPath, []byte(
		"# Codec contract\n\nThe codec identifier is E5F6A7B8.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(
		filepath.Join(managed, "docs", "truth", "portability.md")); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(managed, "docs", "history", "retired.md")
	existing, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, append(existing,
		[]byte("\nA later retrospective note.\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, rebuilt, err := service.Index.Ensure(ctx); err != nil {
		t.Fatal(err)
	} else if !rebuilt {
		t.Fatal("the corpus edits did not publish a new generation")
	}

	delta := orientDelta(t, server, managed, first.ID)
	if delta["available"] != true {
		t.Fatalf("a delta across one publication was unavailable: %#v", delta)
	}
	assertDeltaPath(t, delta, "added", "docs/truth/codec.md", "truth")
	assertDeltaPath(t, delta, "removed", "docs/truth/portability.md", "truth")
	assertDeltaPath(t, delta, "changed", "docs/history/retired.md", "history")
}

func assertDeltaPath(t *testing.T, delta map[string]any, list, path, tier string) {
	t.Helper()
	entries, ok := delta[list].([]any)
	if !ok {
		t.Fatalf("delta %s is not a list: %#v", list, delta[list])
	}
	for _, raw := range entries {
		entry, isObject := raw.(map[string]any)
		if !isObject {
			continue
		}
		if entry["path"] == path {
			// A path without its tier would make an added truth document and an
			// added campaign scratch file look alike.
			if entry["tier"] != tier {
				t.Fatalf("%s %s tier = %v, want %q", list, path, entry["tier"], tier)
			}
			return
		}
	}
	t.Fatalf("delta %s did not contain %s: %#v", list, path, entries)
}

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
