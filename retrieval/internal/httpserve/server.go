// Package httpserve exposes re-search queries over HTTP.
package httpserve

import (
	"fmt"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

// QueryFunc answers one question with ranked hits.
type QueryFunc func(query string, limit int) ([]search.Hit, error)

// ListenAndServe serves the query API (implemented in a later task).
func ListenAndServe(addr string, query QueryFunc) error {
	return fmt.Errorf("http: not yet implemented")
}
