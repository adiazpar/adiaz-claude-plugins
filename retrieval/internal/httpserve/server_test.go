package httpserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

func TestHandlerQuery(t *testing.T) {
	var gotOpts search.QueryOptions
	h := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) {
		gotOpts = opts
		return []search.Hit{{Path: "docs/engine/a.md", Title: "A: " + q}}, nil
	})
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
	h := Handler(func(q string, opts search.QueryOptions) ([]search.Hit, error) { return nil, nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/query", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}
