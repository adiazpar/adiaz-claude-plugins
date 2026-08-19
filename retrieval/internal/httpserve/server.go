// Package httpserve exposes re-search queries over a small JSON HTTP
// endpoint — the retrieval half any future hosted consumer wraps.
package httpserve

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

// QueryFunc answers one question with ranked hits.
type QueryFunc func(query string, opts search.QueryOptions) ([]search.Hit, error)

// Handler serves GET /query?q=...&limit=N&kind=...&grade=... as JSON.
func Handler(query QueryFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q parameter"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		hits, err := query(q, search.QueryOptions{
			Limit: limit,
			Kind:  r.URL.Query().Get("kind"),
			Grade: r.URL.Query().Get("grade"),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if hits == nil {
			hits = []search.Hit{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})
	return mux
}

// ListenAndServe blocks serving the query API on addr.
func ListenAndServe(addr string, query QueryFunc) error {
	return http.ListenAndServe(addr, Handler(query))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
