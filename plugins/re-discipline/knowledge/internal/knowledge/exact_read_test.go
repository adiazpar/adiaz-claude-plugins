package knowledge

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactIndexedReadFlattensPathChunkAndURI(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()
	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const relative = "docs/truth/engine.md"
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		t.Fatal(err)
	}
	var chunkID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM chunks WHERE path=?
		ORDER BY start_line,id LIMIT 1`, relative).Scan(&chunkID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	sourceURI := "re-discipline://" + generation.ID + "/sources/" +
		url.PathEscape(relative)

	tests := []struct {
		name     string
		selector string
		value    string
		options  ReadOptions
	}{
		{name: "path", selector: "path", value: relative, options: ReadOptions{Path: relative}},
		{name: "chunk", selector: "chunk", value: chunkID, options: ReadOptions{ChunkID: chunkID}},
		{name: "uri", selector: "uri", value: sourceURI, options: ReadOptions{URI: sourceURI}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			pinned, err := service.Read(ctx, testCase.options)
			if err != nil {
				t.Fatal(err)
			}
			passage, passageOK := pinned["passage"].(string)
			citation, citationOK := pinned["citation"].(Citation)
			if !passageOK || !citationOK {
				t.Fatalf("pinned read has unexpected shape: %#v", pinned)
			}
			exact, err := service.ReadExact(ctx, ExactReadRequest{
				Selector: testCase.selector, Value: testCase.value, TokenBudget: 8192,
			})
			if err != nil {
				t.Fatal(err)
			}
			if exact.Selector != testCase.selector ||
				exact.Handle != testCase.selector+":"+testCase.value ||
				exact.Path != citation.Path || exact.StartLine != citation.StartLine ||
				exact.EndLine != citation.EndLine || exact.Content != passage ||
				exact.SHA256 != "sha256:"+citation.SourceHash || exact.Truncated ||
				!sha256ValueRE.MatchString(exact.Digest) {
				t.Fatalf("exact %s read did not flatten its pinned passage: %#v",
					testCase.selector, exact)
			}
		})
	}
}

func TestReturnedProvenanceAndRawHandlesRoundTripThroughReadExact(t *testing.T) {
	root := makeAdversarialProject(t)
	provenancePath := "docs/backlog/round-trip.md"
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(provenancePath)),
		"# Round trip\n\nprovenance-round-trip-lambda remains non-authoritative backlog context.\n")
	rawPath := "active/fixture-campaign/runs/R-20260802-0001/report.md"
	longLine := strings.Repeat(string(rune(0x03b1)), 900) +
		" raw-byte-round-trip-omicron " + strings.Repeat(string(rune(0x03b2)), 900)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(rawPath)),
		"# VERDICT\n"+longLine+"\n")
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	queries := []struct {
		query                  string
		cardType               string
		path                   string
		allowedProvenanceTiers []string
	}{
		{"provenance-round-trip-lambda", "provenance", provenancePath, []string{"backlog"}},
		{"raw-byte-round-trip-omicron", "raw-report", rawPath, []string{"asset"}},
	}
	for _, test := range queries {
		t.Run(test.cardType, func(t *testing.T) {
			response, err := service.Query(ctx, FindingQueryOptions{
				Query: test.query, Limit: 1, TokenBudget: 1024,
				AllowedProvenanceTiers: test.allowedProvenanceTiers,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Cards) != 1 || response.Cards[0].CardType != test.cardType {
				t.Fatalf("query did not return the expected card: %+v", response)
			}
			card := response.Cards[0]
			if card.Handle != "path:"+test.path || !strings.HasPrefix(card.EvidenceHandle, "path:"+test.path+"#") {
				t.Fatalf("card omitted reusable exact handles: %+v", card)
			}
			if test.cardType == "raw-report" && !strings.Contains(card.EvidenceHandle, "#B") {
				t.Fatalf("long single-line raw evidence did not use a byte-range handle: %+v", card)
			}
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(test.path)))
			if err != nil {
				t.Fatal(err)
			}
			normalized := normalizeNewlines(body)
			for _, handle := range []string{card.Handle, card.EvidenceHandle} {
				exact, err := service.ReadExact(ctx, ExactReadRequest{
					Selector: "path", Value: handle, TokenBudget: 8192,
				})
				if err != nil {
					t.Fatalf("read returned handle %s: %v", handle, err)
				}
				parsed, err := parseExactPathHandle(handle)
				if err != nil {
					t.Fatal(err)
				}
				expected := string(normalized)
				if parsed.byteRange {
					expected = string(normalized[parsed.startByte:parsed.endByte])
				} else if parsed.startLine > 0 {
					expected, err = lineRangeBody(normalized, parsed.startLine, parsed.endLine)
					if err != nil {
						t.Fatal(err)
					}
				}
				if exact.Handle != handle || exact.Path != test.path || exact.Content != expected ||
					exact.SHA256 != "sha256:"+SHA256Bytes(body) || exact.Truncated ||
					exact.ByteRange != parsed.byteRange || exact.StartByte != parsed.startByte ||
					exact.EndByte != parsed.endByte {
					t.Fatalf("handle did not reproduce its generation-bound bytes: handle=%s response=%+v", handle, exact)
				}
			}
		})
	}

	_, err := service.ReadExact(ctx, ExactReadRequest{
		Selector: "path", Value: "path:" + rawPath + "#B11-B14", TokenBudget: 8192,
	})
	if err == nil || !strings.Contains(err.Error(), "UTF-8 byte range") {
		t.Fatalf("mid-rune byte range was not rejected: %v", err)
	}
}
