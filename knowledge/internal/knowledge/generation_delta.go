package knowledge

import (
	"context"
	"database/sql"
	"os"
	"sort"
)

// generationDeltaHistory is how many document-level deltas the cache keeps.
//
// The delta a caller wants is almost always "since my last session", which is
// one or a few publications back. Keeping a short chain answers that without
// turning a disposable cache into a change log nobody prunes; a caller that
// falls off the end is told the delta is unavailable rather than being handed a
// guess.
const generationDeltaHistory = 8

// generationDeltaPathCap bounds one delta's per-list size. A first build lists
// the entire corpus as added, and a cache file must not grow with the project.
const generationDeltaPathCap = 200

// DocumentRef names one document in a delta, with the tier that decides how a
// caller may use it. A path without its tier would make an added truth document
// and an added campaign scratch file look alike.
type DocumentRef struct {
	Path string `json:"path"`
	Tier string `json:"tier"`
}

// GenerationDelta is what changed between two published generations.
type GenerationDelta struct {
	Generation         string        `json:"generation"`
	PreviousGeneration string        `json:"previousGeneration,omitempty"`
	CreatedAt          string        `json:"createdAt"`
	Added              []DocumentRef `json:"added"`
	Changed            []DocumentRef `json:"changed"`
	Removed            []DocumentRef `json:"removed"`
	// Truncated says a list hit the per-delta cap, so absence of a path is not
	// evidence the path did not change.
	Truncated bool `json:"truncated,omitempty"`
}

type generationDeltaFile struct {
	SchemaVersion int               `json:"schemaVersion"`
	Deltas        []GenerationDelta `json:"deltas"`
}

type documentRecord struct {
	Tier        string
	ContentHash string
}

func readGenerationDocuments(path string) (map[string]documentRecord, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT path,tier,content_hash FROM documents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := map[string]documentRecord{}
	for rows.Next() {
		var documentPath, tier, hash string
		if err := rows.Scan(&documentPath, &tier, &hash); err != nil {
			return nil, err
		}
		documents[documentPath] = documentRecord{Tier: tier, ContentHash: hash}
	}
	return documents, rows.Err()
}

func buildGenerationDelta(
	previousID string,
	previous map[string]documentRecord,
	generation Generation,
	inventory SourceInventory,
) GenerationDelta {
	delta := GenerationDelta{
		Generation: generation.ID, PreviousGeneration: previousID,
		CreatedAt: generation.CreatedAt,
		Added:     []DocumentRef{}, Changed: []DocumentRef{}, Removed: []DocumentRef{},
	}
	seen := map[string]bool{}
	for _, document := range inventory.Documents {
		seen[document.Path] = true
		record, existed := previous[document.Path]
		ref := DocumentRef{Path: document.Path, Tier: document.Tier}
		switch {
		case !existed:
			delta.Added = append(delta.Added, ref)
		case record.ContentHash != document.ContentHash || record.Tier != document.Tier:
			delta.Changed = append(delta.Changed, ref)
		}
	}
	removed := make([]string, 0, len(previous))
	for path := range previous {
		if !seen[path] {
			removed = append(removed, path)
		}
	}
	sort.Strings(removed)
	for _, path := range removed {
		delta.Removed = append(delta.Removed, DocumentRef{
			Path: path, Tier: previous[path].Tier,
		})
	}
	delta.Added, delta.Truncated = capDocumentRefs(delta.Added, delta.Truncated)
	delta.Changed, delta.Truncated = capDocumentRefs(delta.Changed, delta.Truncated)
	delta.Removed, delta.Truncated = capDocumentRefs(delta.Removed, delta.Truncated)
	return delta
}

func capDocumentRefs(refs []DocumentRef, truncated bool) ([]DocumentRef, bool) {
	if len(refs) <= generationDeltaPathCap {
		return refs, truncated
	}
	return refs[:generationDeltaPathCap], true
}

func generationDeltaFilePath(cacheRoot string) (string, error) {
	return containedOutputPath(cacheRoot, "generation-deltas.json")
}

func loadGenerationDeltas(cacheRoot string) []GenerationDelta {
	path, err := generationDeltaFilePath(cacheRoot)
	if err != nil {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var stored generationDeltaFile
	if err := decodeStrict(body, &stored); err != nil || stored.SchemaVersion != 1 {
		return nil
	}
	return stored.Deltas
}

// recordGenerationDelta appends one delta to the bounded history. It is
// best-effort: a delta that cannot be written costs a caller an explicit
// "unavailable", which is a far better outcome than failing the publication
// that produced it.
func recordGenerationDelta(cacheRoot string, delta GenerationDelta) {
	if delta.Generation == "" {
		return
	}
	deltas := loadGenerationDeltas(cacheRoot)
	for _, existing := range deltas {
		if existing.Generation == delta.Generation {
			return
		}
	}
	deltas = append(deltas, delta)
	if len(deltas) > generationDeltaHistory {
		deltas = deltas[len(deltas)-generationDeltaHistory:]
	}
	path, err := generationDeltaFilePath(cacheRoot)
	if err != nil {
		return
	}
	_ = AtomicWriteJSON(path, generationDeltaFile{
		SchemaVersion: 1, Deltas: deltas,
	}, 0o600)
}

// ComposeGenerationDelta walks the recorded chain forward from a caller's
// last-seen generation and folds it into one net delta.
//
// It reports unavailable rather than guessing when the chain does not reach
// back that far. A caller that asked what changed and is told nothing changed
// has been misinformed; a caller told the history is too short knows to
// re-orient from scratch.
func ComposeGenerationDelta(
	history []GenerationDelta,
	since string,
	current string,
) (GenerationDelta, bool) {
	composed := GenerationDelta{
		Generation: current, PreviousGeneration: since,
		Added: []DocumentRef{}, Changed: []DocumentRef{}, Removed: []DocumentRef{},
	}
	if since == current {
		return composed, true
	}
	byPrevious := map[string]GenerationDelta{}
	for _, delta := range history {
		byPrevious[delta.PreviousGeneration] = delta
	}
	states := map[string]string{}
	tiers := map[string]string{}
	cursor := since
	for step := 0; step <= len(history); step++ {
		delta, ok := byPrevious[cursor]
		if !ok {
			return GenerationDelta{}, false
		}
		if delta.Truncated {
			return GenerationDelta{}, false
		}
		applyDeltaState(states, tiers, delta)
		cursor = delta.Generation
		if cursor == current {
			composed.CreatedAt = delta.CreatedAt
			for _, path := range sortedMapStringKeys(states) {
				ref := DocumentRef{Path: path, Tier: tiers[path]}
				switch states[path] {
				case "added":
					composed.Added = append(composed.Added, ref)
				case "changed":
					composed.Changed = append(composed.Changed, ref)
				case "removed":
					composed.Removed = append(composed.Removed, ref)
				}
			}
			return composed, true
		}
	}
	return GenerationDelta{}, false
}

// applyDeltaState folds one hop into the running net state. A document added
// and then removed within the span nets out to nothing, and a caller who never
// saw it should not be told about it.
func applyDeltaState(
	states map[string]string,
	tiers map[string]string,
	delta GenerationDelta,
) {
	for _, ref := range delta.Added {
		tiers[ref.Path] = ref.Tier
		if states[ref.Path] == "removed" {
			states[ref.Path] = "changed"
			continue
		}
		states[ref.Path] = "added"
	}
	for _, ref := range delta.Changed {
		tiers[ref.Path] = ref.Tier
		if states[ref.Path] == "added" {
			continue
		}
		states[ref.Path] = "changed"
	}
	for _, ref := range delta.Removed {
		tiers[ref.Path] = ref.Tier
		if states[ref.Path] == "added" {
			delete(states, ref.Path)
			delete(tiers, ref.Path)
			continue
		}
		states[ref.Path] = "removed"
	}
}

func sortedMapStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// GenerationDeltaSince answers what changed for a caller holding an older
// generation id. The answer is always explicit about whether it is an answer.
func (service *Service) GenerationDeltaSince(
	ctx context.Context,
	since string,
) map[string]any {
	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		return map[string]any{
			"available": false,
			"reason":    "the active generation could not be resolved",
		}
	}
	if !generationIdentityRE.MatchString(since) {
		return map[string]any{
			"available": false,
			"reason":    "sinceGeneration is not a generation identity",
		}
	}
	composed, ok := ComposeGenerationDelta(
		loadGenerationDeltas(service.Index.CacheRoot), since, generation.ID)
	if !ok {
		return map[string]any{
			"available":       false,
			"sinceGeneration": since,
			"generation":      generation.ID,
			"reason": "no recorded delta chain reaches this generation from " +
				since + "; re-orient without a delta",
		}
	}
	return map[string]any{
		"available":       true,
		"sinceGeneration": since,
		"generation":      generation.ID,
		"added":           composed.Added,
		"changed":         composed.Changed,
		"removed":         composed.Removed,
	}
}
