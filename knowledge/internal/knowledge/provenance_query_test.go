package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicQueryReturnsBoundedNonAuthoritativeManagedProvenance(t *testing.T) {
	root := t.TempDir()
	writeFindingFixtureFile(t, root, ".re-discipline/project-profile.md", []byte("# Fixture profile\n"))
	writeFindingFixtureFile(t, root, "docs/INDEX.md", []byte("# Navigation\n\nNavigationZeta locates the durable implementation map.\n"))
	writeFindingFixtureFile(t, root, "docs/history/retired.md", []byte("# Retired note\n\nHistoryTheta records a superseded investigation.\n"))
	writeFindingFixtureFile(t, root, "docs/backlog/next.md", []byte("# Backlog item\n\nBacklogOmega remains proposed work, not current truth.\n"))
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverSources(boundary, KnowledgeSettings{Sources: SourceSettings{
		HistoryFindings: true, Backlog: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "managed-provenance.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if err := createSchema(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	generation := Generation{
		ID: "generation-managed-provenance", Database: database,
		CorpusFingerprint: inventory.Fingerprint, Project: "fixture",
		ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		CreatedAt: "2026-08-02T20:00:00Z", DocumentCount: len(inventory.Documents),
		ChunkCount: len(inventory.Chunks),
	}
	if err := populateDatabase(context.Background(), db, generation, inventory, ModelManifest{}, ""); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	retriever := Retriever{
		Boundary: boundary, Generation: generation,
		Profile: SelectedProfile{EffectiveIdentity: "fixture:provenance-v1"},
	}
	for _, test := range []struct {
		query, sourceClass, sourcePath string
	}{
		{"NavigationZeta", "navigation", "docs/INDEX.md"},
		{"HistoryTheta", "history", "docs/history/retired.md"},
		{"BacklogOmega", "backlog", "docs/backlog/next.md"},
	} {
		t.Run(test.sourceClass, func(t *testing.T) {
			response, err := retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
				Query: test.query, Limit: 1, TokenBudget: 2048,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != "insufficient-evidence" || len(response.Cards) != 1 ||
				!response.Trace.ProvenanceFallbackServed || response.EstimatedTokens > response.TokenBudget {
				t.Fatalf("ordinary managed source was not returned as a bounded fallback: %+v", response)
			}
			card := response.Cards[0]
			if card.CardType != "provenance" || card.SourceClass != test.sourceClass ||
				card.Handle != "path:"+test.sourcePath ||
				!strings.HasPrefix(card.EvidenceHandle, "path:"+test.sourcePath+"#") ||
				card.Metadata["authority"] != "provenance-only" ||
				card.Metadata["sourceKind"] != "managed-document" ||
				card.ReviewState != "" || card.Validity != "" {
				t.Fatalf("managed prose received finding authority or lost exact handles: %+v", card)
			}
		})
	}
	filtered, err := retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "BacklogOmega", AllowedSourceClasses: []string{"history"},
		AllowedProvenanceTiers: []string{"navigation"},
		Limit:                  1, TokenBudget: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Status != "abstained" || len(filtered.Cards) != 0 ||
		filtered.Trace.ProvenanceFallbackServed {
		t.Fatalf("finding filters leaked into the independent provenance-tier boundary: %+v", filtered)
	}
	requested, err := retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "BacklogOmega", AllowedSourceClasses: []string{"history"},
		AllowedProvenanceTiers: []string{"backlog"}, Limit: 1, TokenBudget: 2048,
	})
	if err != nil || len(requested.Cards) != 1 || requested.Cards[0].SourceClass != "backlog" {
		t.Fatalf("explicit provenance tier was not independent of finding source filters: response=%+v err=%v", requested, err)
	}
	if _, err := retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "BacklogOmega", AllowedProvenanceTiers: []string{"archive"},
		Limit: 1, TokenBudget: 2048,
	}); err == nil {
		t.Fatal("unsupported raw archive tier was accepted as ordinary managed provenance")
	}
}

func TestManagedProvenanceCandidatesAreIndexedHardBoundedAndTierIsolated(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "provenance-scale.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := createSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	insertDocument, err := tx.PrepareContext(ctx, `INSERT INTO documents(
		id,path,tier,title,content_hash,size,source_kind) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertDocument.Close()
	insertChunk, err := tx.PrepareContext(ctx, `INSERT INTO chunks(
		id,document_id,path,tier,heading,start_line,end_line,content,content_hash)
		VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertChunk.Close()
	insertFTS, err := tx.PrepareContext(ctx,
		`INSERT INTO chunks_fts(chunk_id,path,heading,context,content) VALUES(?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertFTS.Close()
	insertTerm, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO terms(term,chunk_id) VALUES(?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertTerm.Close()

	// The allowed backlog population alone is much larger than the public
	// admission ceiling. Equally relevant history and raw rows exercise both
	// tier and source-kind isolation before candidate materialization.
	total := managedProvenanceCandidateCeiling * 8
	for index := 0; index < total; index++ {
		tier, sourceKind, prefix := "backlog", "", "docs/backlog"
		switch {
		case index >= total*3/4:
			tier, sourceKind, prefix = "backlog", "raw-report", "active/scale/runs"
		case index >= total/2:
			tier, prefix = "history", "docs/history"
		}
		id := fmt.Sprintf("scale-%04d", index)
		path := fmt.Sprintf("%s/%s.md", prefix, id)
		heading := fmt.Sprintf("Scale needle %04d", index)
		content := "ScaleNeedle appears in this bounded-candidate fixture."
		digest := SHA256String(path + "\n" + content)
		if _, err := insertDocument.ExecContext(
			ctx, "document-"+id, path, tier, heading, digest, len(content), sourceKind,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := insertChunk.ExecContext(
			ctx, "chunk-"+id, "document-"+id, path, tier, heading, 1, 1, content, digest,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := insertFTS.ExecContext(
			ctx, "chunk-"+id, path, heading, "", content,
		); err != nil {
			t.Fatal(err)
		}
		for _, term := range IdentifierTerms(path + "\n" + heading + "\n" + content) {
			if _, err := insertTerm.ExecContext(ctx, term, "chunk-"+id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	first, err := loadManagedProvenanceCandidates(
		ctx, db, "ScaleNeedle", []string{"backlog"}, managedProvenanceCandidateCeiling)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || len(first) > managedProvenanceCandidateCeiling {
		t.Fatalf("indexed candidate admission ignored its hard ceiling: %d", len(first))
	}
	for _, candidate := range first {
		if candidate.tier != "backlog" || !strings.HasPrefix(candidate.sourcePath, "docs/backlog/") {
			t.Fatalf("candidate admission crossed tier or ordinary-source boundary: %+v", candidate)
		}
	}
	second, err := loadManagedProvenanceCandidates(
		ctx, db, "ScaleNeedle", []string{"backlog"}, managedProvenanceCandidateCeiling)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("bounded candidate ordering was not replay-stable: %d != %d", len(first), len(second))
	}
	for index := range first {
		if first[index].id != second[index].id {
			t.Fatalf("bounded candidate ordering changed at %d: %s != %s",
				index, first[index].id, second[index].id)
		}
	}
	if _, err := loadManagedProvenanceCandidates(
		ctx, db, "ScaleNeedle", []string{"backlog"}, managedProvenanceCandidateCeiling+1,
	); err == nil {
		t.Fatal("caller enlarged the managed provenance hard candidate ceiling")
	}
}
