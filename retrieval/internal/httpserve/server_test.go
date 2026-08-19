package httpserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

func noSymbols(name string, limit int) (search.SymbolHits, error) {
	return search.SymbolHits{}, nil
}

func TestHandlerQuery(t *testing.T) {
	var gotOpts search.QueryOptions
	h := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) {
		gotOpts = opts
		return []search.Hit{{Path: "docs/engine/a.md", Title: "A: " + q}}, nil
	}, noSymbols)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/query?q=demon+joints&kind=fact&grade=inferred&limit=7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Hits []search.Hit `json:"hits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hits) != 1 || body.Hits[0].Path != "docs/engine/a.md" {
		t.Fatalf("body: %+v", body)
	}
	if gotOpts.Kind != "fact" || gotOpts.Grade != "inferred" || gotOpts.Limit != 7 {
		t.Fatalf("opts passthrough: %+v", gotOpts)
	}
}

func TestHandlerMissingQ(t *testing.T) {
	h := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) { return nil, nil }, noSymbols)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/query", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandlerSymbol(t *testing.T) {
	var gotName string
	var gotLimit int
	h := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) { return nil, nil },
		func(name string, limit int) (search.SymbolHits, error) {
			gotName, gotLimit = name, limit
			return search.SymbolHits{Exact: true, Total: 1, Symbols: []search.Symbol{
				{Name: "TAG_LANGDICT", Kind: "constant", Render: "TAG_LANGDICT  int = 150"},
			}}, nil
		})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/symbol?name=TAG_LANGDICT&limit=3", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body search.SymbolHits
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Exact || body.Total != 1 || len(body.Symbols) != 1 || body.Symbols[0].Name != "TAG_LANGDICT" {
		t.Fatalf("body: %+v", body)
	}
	if gotName != "TAG_LANGDICT" || gotLimit != 3 {
		t.Fatalf("passthrough: %q %d", gotName, gotLimit)
	}

	// Missing name is a client error, and an empty result still returns
	// a JSON array rather than null.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/symbol", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name status %d", rec.Code)
	}
	empty := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) { return nil, nil }, noSymbols)
	rec = httptest.NewRecorder()
	empty.ServeHTTP(rec, httptest.NewRequest("GET", "/symbol?name=zzz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"symbols":[]`) {
		t.Fatalf("empty result: %d %s", rec.Code, rec.Body.String())
	}
}
