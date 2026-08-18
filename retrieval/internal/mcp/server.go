// Package mcp implements a minimal MCP stdio server for re-search.
package mcp

import (
	"fmt"
	"io"
)

// QueryFunc answers one question with formatted text.
type QueryFunc func(query string, limit int) (string, error)

// Serve speaks MCP over stdio (implemented in a later task).
func Serve(in io.Reader, out io.Writer, version string, query QueryFunc) error {
	return fmt.Errorf("mcp: not yet implemented")
}
