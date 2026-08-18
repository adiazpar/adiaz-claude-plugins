package search

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SwapIndex renames tmpPath over dstPath. Windows refuses to replace a
// file another process has open, so retry with backoff; on persistent
// failure remove the temp file and report — callers treat this as
// non-fatal (the existing index keeps serving).
func SwapIndex(tmpPath, dstPath string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(tmpPath, dstPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	os.Remove(tmpPath)
	return fmt.Errorf("could not swap index into place: %w", err)
}

// EnsureFresh implements the binding auto-reindex rule: a query never
// fails or blocks because a reindex could not complete. It returns
// warnings only.
func EnsureFresh(root string) []string {
	var warnings []string
	dbPath := IndexPath(root)

	current, err := ScanDocs(root)
	if err != nil {
		return append(warnings, fmt.Sprintf("corpus scan failed: %v", err))
	}

	stale := false
	if stored, err := ReadManifest(dbPath); err != nil {
		// Missing or corrupt index: it is disposable — delete and rebuild.
		if rmErr := os.Remove(dbPath); rmErr != nil && !os.IsNotExist(rmErr) {
			warnings = append(warnings, fmt.Sprintf("index unreadable and could not be removed (%v); retrying next query", rmErr))
		}
		stale = true
	} else if ManifestDiffers(stored, current) {
		stale = true
	}
	if !stale {
		return warnings
	}

	release, ok := TryLock(root)
	if !ok {
		return append(warnings, "another process is rebuilding the index; serving existing index")
	}
	defer release()

	// Holding the lock, sweep temp debris from rebuilders that were
	// killed mid-build (only the owner ever removed its own temp).
	if leftovers, _ := filepath.Glob(filepath.Join(root, ".re-discipline", "index.tmp-*.db")); leftovers != nil {
		for _, l := range leftovers {
			os.Remove(l)
		}
	}
	os.Remove(filepath.Join(root, ".re-discipline", "index.db.build"))

	tmp := filepath.Join(root, ".re-discipline", fmt.Sprintf("index.tmp-%d.db", os.Getpid()))
	_, buildWarnings, err := BuildIndexFile(root, tmp)
	warnings = append(warnings, buildWarnings...)
	if err != nil {
		os.Remove(tmp)
		return append(warnings, fmt.Sprintf("index rebuild failed: %v; serving existing index if present", err))
	}
	if err := SwapIndex(tmp, dbPath); err != nil {
		return append(warnings, err.Error()+"; serving existing index")
	}
	return warnings
}
