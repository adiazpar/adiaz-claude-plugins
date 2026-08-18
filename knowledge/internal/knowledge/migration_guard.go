package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrMigrationIncomplete = errors.New("re-discipline migration is incomplete")

// projectMigrationState is the fail-closed mixed-mode boundary shared by
// every ordinary adapter. The explicit migrator is the only component that
// may operate while this durable transaction exists in a non-terminal state.
func projectMigrationState(projectRoot string) (MigrationState, bool, error) {
	engine, err := NewMigrationEngine(projectRoot)
	if err != nil {
		return MigrationState{}, false, err
	}
	if err := engine.validateMigrationDirectory(engine.migrationRoot()); err != nil {
		if os.IsNotExist(err) {
			return MigrationState{}, false, nil
		}
		return MigrationState{}, true, fmt.Errorf("validate migration state root: %w", err)
	}
	path := filepath.Join(engine.migrationRoot(), "state.json")
	body, err := readSingleLinkRegularFile(path)
	if os.IsNotExist(err) {
		return MigrationState{}, false, nil
	}
	if err != nil {
		return MigrationState{}, true, fmt.Errorf("read migration state: %w", err)
	}
	var state MigrationState
	if err := decodeStrictJSON(body, &state); err != nil {
		return MigrationState{}, true, fmt.Errorf("decode migration state: %w", err)
	}
	expected := state.Digest
	state.Digest = ""
	digest, err := CanonicalDigest(state)
	state.Digest = expected
	if err != nil || expected != digest || state.SchemaVersion != MigrationSchemaVersion ||
		!validMigrationState(state.State) {
		return MigrationState{}, true, errors.New("migration state is invalid or digest-mismatched")
	}
	return state, true, nil
}

func requireCompletedMigration(projectRoot string) error {
	state, exists, err := projectMigrationState(projectRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMigrationIncomplete, err)
	}
	if exists && state.State != "migrated" {
		return fmt.Errorf("%w: transaction %s is %s; use migrate-project status/resume/verify/ratify",
			ErrMigrationIncomplete, state.TransactionID, state.State)
	}
	version, err := DetectProjectStateVersion(projectRoot)
	if err != nil {
		return fmt.Errorf("validate operational project version: %w", err)
	}
	if version != "0.8" {
		return fmt.Errorf(
			"%w: legacy project state %s is read-only outside migrate-project preview/apply/resume",
			ErrMigrationIncomplete, version,
		)
	}
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return err
	}
	profile, err := readSingleLinkRegularFile(filepath.Join(
		boundary.Root, ".re-discipline", "project-profile.md",
	))
	if err != nil || !strings.Contains(string(profile), SharedLawsMarker) {
		return errors.New(
			"operational 0.8 project requires the runtime-supported re-discipline shared-laws marker; use init-project recovery",
		)
	}
	return nil
}
