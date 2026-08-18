// Package search implements the re-search corpus scanner, indexer, and
// query engine over a .re-discipline/ markdown knowledge base.
package search

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindRoot walks up from startDir to the nearest directory containing a
// .re-discipline directory and returns that directory as the project root.
func FindRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if fi, statErr := os.Stat(filepath.Join(dir, ".re-discipline")); statErr == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .re-discipline directory found walking up from %s", startDir)
		}
		dir = parent
	}
}
