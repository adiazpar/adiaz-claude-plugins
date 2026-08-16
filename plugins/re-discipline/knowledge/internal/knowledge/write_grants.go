package knowledge

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

const maxRunWriteGrants = 32

// WriteGrant is a project-relative, engine-registered write boundary for one
// run. Run-local report.md and payload/** writes are implicit and therefore
// never represented here.
type WriteGrant struct {
	Mode string `json:"mode"`
	Path string `json:"path"`
}

type activeRunGrantSet struct {
	RecordPath string
	Run        RunRecord
}

var writeGrantPathRE = regexp.MustCompile(`^[A-Za-z0-9._@+#() -]+(?:/[A-Za-z0-9._@+#() -]+)*$`)

// NormalizeWriteGrants validates and deterministically orders a grant set.
// Overlaps are rejected rather than silently coalesced so the manager's exact
// authority remains visible in the run, context pack, and brief digests.
func NormalizeWriteGrants(grants []WriteGrant) ([]WriteGrant, error) {
	if len(grants) > maxRunWriteGrants {
		return nil, fmt.Errorf("run write grants exceed the %d-entry limit", maxRunWriteGrants)
	}
	if grants == nil {
		return nil, nil
	}
	normalized := append([]WriteGrant(nil), grants...)
	for index := range normalized {
		normalized[index].Mode = strings.TrimSpace(normalized[index].Mode)
		normalized[index].Path = strings.TrimSpace(normalized[index].Path)
		grant := normalized[index]
		if grant.Mode != "exact" && grant.Mode != "directory" {
			return nil, fmt.Errorf("run write grant %q has unsupported mode %q", grant.Path, grant.Mode)
		}
		if len(grant.Path) > 500 || !writeGrantPathRE.MatchString(grant.Path) {
			return nil, fmt.Errorf("run write grant %q is not a safely representable project path", grant.Path)
		}
		if err := validateRelativeRecordPath(grant.Path); err != nil {
			return nil, fmt.Errorf("run write grant: %w", err)
		}
		if managedWriteGrantPath(grant.Path) {
			return nil, fmt.Errorf("run write grant %q overlaps engine-managed state", grant.Path)
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Path == normalized[j].Path {
			return normalized[i].Mode < normalized[j].Mode
		}
		return normalized[i].Path < normalized[j].Path
	})
	for index, grant := range normalized {
		for priorIndex := 0; priorIndex < index; priorIndex++ {
			prior := normalized[priorIndex]
			if grantsOverlap(prior, grant) {
				return nil, fmt.Errorf(
					"run write grants %s:%s and %s:%s overlap ambiguously",
					prior.Mode, prior.Path, grant.Mode, grant.Path,
				)
			}
		}
	}
	return normalized, nil
}

func ValidateCanonicalWriteGrants(grants []WriteGrant) error {
	normalized, err := NormalizeWriteGrants(grants)
	if err != nil {
		return err
	}
	if len(normalized) != len(grants) {
		return errors.New("run write grants are not canonical")
	}
	for index := range grants {
		if grants[index] != normalized[index] {
			return errors.New("run write grants must be normalized and sorted")
		}
	}
	return nil
}

func EqualWriteGrants(left, right []WriteGrant) bool {
	leftNormalized, leftErr := NormalizeWriteGrants(left)
	rightNormalized, rightErr := NormalizeWriteGrants(right)
	if leftErr != nil || rightErr != nil || len(leftNormalized) != len(rightNormalized) {
		return false
	}
	for index := range leftNormalized {
		if leftNormalized[index] != rightNormalized[index] {
			return false
		}
	}
	return true
}

func grantsOverlap(left, right WriteGrant) bool {
	if left.Path == right.Path {
		return true
	}
	if left.Mode == "directory" && strings.HasPrefix(right.Path, left.Path+"/") {
		return true
	}
	return right.Mode == "directory" && strings.HasPrefix(left.Path, right.Path+"/")
}

// validatePreparedRunWriteGrantIsolation runs while the canonical writer lock
// is held. It prevents two prepared/running runs, including runs owned by
// different campaigns and manager processes, from receiving overlapping
// project write authority. The global state head remains the serialization
// point; this check makes the conflict actionable instead of allowing two
// independently valid run records to authorize writes to the same bytes.
func (store *StateStore) validatePreparedRunWriteGrantIsolation(
	inventory StateInventory,
	prepared preparedTransaction,
) error {
	if !validOne(prepared.Request.Action, "run.prepare", "closure.remediation.run.create") {
		return nil
	}
	proposed := make([]activeRunGrantSet, 0, 1)
	for _, write := range prepared.Writes {
		run, ok := write.Record.(RunRecord)
		if !ok || !validOne(run.Status, "prepared", "running") || len(run.WriteGrants) == 0 {
			continue
		}
		proposed = append(proposed, activeRunGrantSet{RecordPath: write.Path, Run: run})
	}
	for index, left := range proposed {
		for priorIndex := 0; priorIndex < index; priorIndex++ {
			if err := activeRunGrantConflict(proposed[priorIndex], left); err != nil {
				return err
			}
		}
	}
	if len(proposed) == 0 {
		return nil
	}
	for _, entry := range inventory.Entries {
		if !canonicalRunRecordPath(entry.Path) {
			continue
		}
		value, _, exists, err := store.existingRecord(entry.Path)
		if err != nil {
			return err
		}
		run, ok := value.(RunRecord)
		if !exists || !ok {
			return fmt.Errorf("%w: inventory run record %s could not be loaded", ErrStateDirty, entry.Path)
		}
		if !validOne(run.Status, "prepared", "running") || len(run.WriteGrants) == 0 {
			continue
		}
		existing := activeRunGrantSet{RecordPath: entry.Path, Run: run}
		for _, candidate := range proposed {
			if candidate.RecordPath == existing.RecordPath {
				continue
			}
			if err := activeRunGrantConflict(existing, candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalRunRecordPath(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 5 && parts[0] == "active" && managedSlugRE.MatchString(parts[1]) &&
		parts[2] == "runs" && runIDRE.MatchString(parts[3]) && parts[4] == "run.json"
}

func activeRunGrantConflict(left, right activeRunGrantSet) error {
	for _, leftGrant := range left.Run.WriteGrants {
		for _, rightGrant := range right.Run.WriteGrants {
			if !grantsOverlap(leftGrant, rightGrant) {
				continue
			}
			return fmt.Errorf(
				"%w: proposed run %s grant %s:%s overlaps active run %s (%s) grant %s:%s. "+
					"Remedy: return, abort, or invalidate the active run before reusing its path, "+
					"or re-scope one run to non-overlapping project paths",
				ErrStateConflict,
				right.Run.ID, rightGrant.Mode, rightGrant.Path,
				left.Run.ID, left.Run.Status, leftGrant.Mode, leftGrant.Path,
			)
		}
	}
	return nil
}

func managedWriteGrantPath(value string) bool {
	clean := path.Clean(value)
	for _, root := range []string{
		"active", ".re-discipline", "docs/truth", "docs/history/campaigns",
	} {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}
