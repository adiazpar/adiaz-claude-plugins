package knowledge

import (
	"context"
	"testing"
)

// A source-class toggle moves no file, so every mtime-derived freshness signal
// keeps reporting a stale index as fresh. Enabling a class must rebuild once,
// and exactly once; changing a serving budget must not rebuild at all, because
// budgets decide what a response carries and have no part in the index.
func TestIndexRebuildsForSourceSettingsAndNotForBudgets(t *testing.T) {
	root := makeAdversarialProject(t)
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadModelManifest(adversarialAssetRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := t.TempDir()
	ctx := context.Background()

	settings := DefaultKnowledgeSettings()
	settings.Sources.ReportFallback = false
	manager := IndexManager{
		Boundary: boundary, CacheRoot: cacheRoot,
		Settings: settings, Manifest: manifest,
	}
	baseline, _, rebuilt, err := manager.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rebuilt {
		t.Fatal("the first Ensure must build a generation")
	}
	if _, _, rebuilt, err = manager.Ensure(ctx); err != nil {
		t.Fatal(err)
	} else if rebuilt {
		t.Fatal("an unchanged project rebuilt the index")
	}

	enabled := settings
	enabled.Sources.ReportFallback = true
	manager.Settings = enabled
	toggled, _, rebuilt, err := manager.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rebuilt {
		t.Fatal("enabling a source class did not rebuild the index")
	}
	if toggled.SettingsFingerprint == baseline.SettingsFingerprint {
		t.Fatal("the settings fingerprint did not record the source-class change")
	}
	if toggled.DocumentCount <= baseline.DocumentCount {
		t.Fatalf("document count %d did not grow past %d after enabling a class",
			toggled.DocumentCount, baseline.DocumentCount)
	}
	// Exactly one rebuild: the change is absorbed, not re-detected forever.
	if _, _, rebuilt, err = manager.Ensure(ctx); err != nil {
		t.Fatal(err)
	} else if rebuilt {
		t.Fatal("the source-class change rebuilt the index more than once")
	}

	budgeted := enabled
	budgeted.Budgets.SearchTokens = enabled.Budgets.SearchTokens / 2
	budgeted.Budgets.MaxPassages = enabled.Budgets.MaxPassages - 1
	budgeted.Telemetry = Telemetry{Mode: "off"}
	manager.Settings = budgeted
	after, _, rebuilt, err := manager.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt {
		t.Fatal("a serving-budget change rebuilt the index")
	}
	if after.ID != toggled.ID {
		t.Fatalf("generation moved from %s to %s without an indexing change",
			toggled.ID, after.ID)
	}
	if IndexingSettingsDigest(budgeted) != IndexingSettingsDigest(enabled) {
		t.Fatal("budget and telemetry policy leaked into the indexing digest")
	}
}

// An additional declared source class changes what is indexed, so it belongs in
// the digest; its declaration order does not.
func TestIndexingSettingsDigestCoversAdditionalClassesOrderIndependently(t *testing.T) {
	base := DefaultKnowledgeSettings()
	first := base
	first.Sources.Additional = []AdditionalSource{
		{Path: "docs/reference", Pattern: "*.md", Tier: "asset"},
		{Path: "docs/notes", Pattern: "*.md", Tier: "asset"},
	}
	reordered := base
	reordered.Sources.Additional = []AdditionalSource{
		{Path: "docs/notes", Pattern: "*.md", Tier: "asset"},
		{Path: "docs/reference", Pattern: "*.md", Tier: "asset"},
	}
	if IndexingSettingsDigest(first) != IndexingSettingsDigest(reordered) {
		t.Fatal("declaration order of additional source classes changed the digest")
	}
	if IndexingSettingsDigest(first) == IndexingSettingsDigest(base) {
		t.Fatal("an additional source class did not change the digest")
	}
}
