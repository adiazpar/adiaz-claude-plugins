package knowledge

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkMarkdownSplitsOverlongUTF8LineWithExactByteCitations(t *testing.T) {
	root := t.TempDir()
	longLine := strings.Repeat("é", 1500)
	body := "# Long line\n" + longLine + "\n## Tail\nshort\n"
	if err := os.WriteFile(root+string(os.PathSeparator)+"long.md", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	read, hash, err := ReadProjectFile(boundary, "long.md")
	if err != nil {
		t.Fatal(err)
	}
	document := SourceDocument{
		ID: StableID("doc", "long.md", hash), Path: "long.md", Tier: "truth",
		Title: "Long line", Content: string(read), ContentHash: hash, Size: int64(len(read)),
	}
	chunks := ChunkMarkdown(document)
	retriever := Retriever{Boundary: boundary}
	var reconstructed strings.Builder
	partial := 0
	normalized := normalizeNewlines(read)
	for _, chunk := range chunks {
		if len([]byte(chunk.Content)) > maxChunkBytes {
			t.Fatalf("chunk exceeds byte ceiling: %d", len([]byte(chunk.Content)))
		}
		if !utf8.ValidString(chunk.Content) {
			t.Fatalf("chunk split invalid UTF-8: %#v", chunk)
		}
		if !retriever.verifyChunk(chunk, hash) {
			t.Fatalf("chunk citation did not verify: %#v", chunk)
		}
		if chunk.ByteRange {
			partial++
			if chunk.StartLine != 2 || chunk.EndLine != 2 {
				t.Fatalf("split line citation drifted: %#v", chunk)
			}
			if got := string(normalized[chunk.StartByte:chunk.EndByte]); got != chunk.Content {
				t.Fatalf("byte citation does not reproduce content")
			}
			reconstructed.WriteString(chunk.Content)
		}
	}
	if partial < 2 {
		t.Fatalf("expected a split line, got %d partial chunks", partial)
	}
	if reconstructed.String() != longLine {
		t.Fatalf("split chunks did not reconstruct source line: %d versus %d bytes", reconstructed.Len(), len([]byte(longLine)))
	}
}

func TestRawFallbackUsesByteHandleForSplitLine(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE documents(
		id TEXT,path TEXT,title TEXT,content_hash TEXT,tier TEXT,source_kind TEXT,size INTEGER);
		CREATE TABLE chunks(
		id TEXT,document_id TEXT,heading TEXT,start_line INTEGER,end_line INTEGER,
		byte_range INTEGER,start_byte INTEGER,end_byte INTEGER,content TEXT);`); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if _, err := db.Exec(`INSERT INTO documents VALUES('d','active/x/runs/R-20260802-0001/report.md','Report',?,'draft','raw-report',4400)`, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chunks VALUES('c','d','VERDICT',2,2,1,2200,4400,'ByteNeedle evidence')`); err != nil {
		t.Fatal(err)
	}
	cards, err := queryRawReportCards(context.Background(), db, "ByteNeedle", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].card.EvidenceHandle != "path:active/x/runs/R-20260802-0001/report.md#B2200-B4400" {
		t.Fatalf("raw split-line fallback lost exact byte handle: %#v", cards)
	}
}
