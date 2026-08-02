package knowledge

import (
	"context"
	"database/sql"
	"net/url"
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
