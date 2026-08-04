package knowledge

import (
	"sort"
	"strings"
	"time"
)

type FindingStalenessPath struct {
	DependentID    string   `json:"dependentId"`
	RootDependency string   `json:"rootDependency"`
	Path           []string `json:"path"`
}

func findingVerificationTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), true
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, false
	}
	return parsed, true
}

func findingDependentIsStale(dependentVerifiedAt, dependencyVerifiedAt string) bool {
	dependent, dependentOK := findingVerificationTime(dependentVerifiedAt)
	dependency, dependencyOK := findingVerificationTime(dependencyVerifiedAt)
	return dependentOK && dependencyOK && dependent.Before(dependency)
}

// FindingStalenessPaths seeds direct newer-dependency staleness, then
// propagates it to a fixed point. Each propagated result retains the complete
// dependency path to the directly newer root so the reason remains auditable.
func FindingStalenessPaths(findings map[string]FindingRecord) map[string][]FindingStalenessPath {
	result := map[string][]FindingStalenessPath{}
	for id, finding := range findings {
		for _, dependencyID := range finding.Relations.DependsOn {
			dependency, present := findings[dependencyID]
			if present && findingDependentIsStale(finding.VerifiedAt, dependency.VerifiedAt) {
				appendUniqueStalenessPath(result, id, FindingStalenessPath{
					DependentID: id, RootDependency: dependencyID, Path: []string{id, dependencyID},
				})
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for id, finding := range findings {
			for _, dependencyID := range finding.Relations.DependsOn {
				for _, downstream := range result[dependencyID] {
					if containsString(downstream.Path, id) {
						continue
					}
					candidate := FindingStalenessPath{
						DependentID: id, RootDependency: downstream.RootDependency,
						Path: append([]string{id}, downstream.Path...),
					}
					if appendUniqueStalenessPath(result, id, candidate) {
						changed = true
					}
				}
			}
		}
	}
	for id := range result {
		sort.Slice(result[id], func(i, j int) bool {
			return strings.Join(result[id][i].Path, "\x00") < strings.Join(result[id][j].Path, "\x00")
		})
	}
	return result
}

func appendUniqueStalenessPath(result map[string][]FindingStalenessPath, id string, candidate FindingStalenessPath) bool {
	want := strings.Join(candidate.Path, "\x00")
	for index, existing := range result[id] {
		if existing.RootDependency == candidate.RootDependency {
			existingPath := strings.Join(existing.Path, "\x00")
			if len(existing.Path) < len(candidate.Path) ||
				len(existing.Path) == len(candidate.Path) && existingPath <= want {
				return false
			}
			result[id][index] = candidate
			return true
		}
	}
	result[id] = append(result[id], candidate)
	return true
}

// StaleFindingDependents is the compact compatibility view used by cards and
// coverage. Its values are the immediate dependency hop; full paths are
// available from FindingStalenessPaths.
func StaleFindingDependents(findings map[string]FindingRecord) map[string][]string {
	result := map[string][]string{}
	for id, reasons := range FindingStalenessPaths(findings) {
		for _, reason := range reasons {
			if len(reason.Path) > 1 {
				result[id] = append(result[id], reason.Path[1])
			}
		}
		result[id] = SortedUnique(result[id])
	}
	return result
}

// findingStalenessRelationAlerts reports card-sized staleness: one
// stale-dependent alert per direct dependency hop. A card is a bounded
// retrieval surface, and the complete transitive path inventory grows with
// the corpus's dependency graph, not with the card - on a densely linked
// corpus it dwarfed the claim itself and priced finding cards out of every
// realistic token budget. The full auditable paths remain available from
// FindingStalenessPaths and the dedicated stale-dependents state view.
func findingStalenessRelationAlerts(paths map[string][]FindingStalenessPath, dependentID string) []string {
	alerts := []string{}
	for _, reason := range paths[dependentID] {
		if len(reason.Path) < 2 {
			continue
		}
		alerts = append(alerts, "stale-dependent:"+reason.Path[1])
	}
	return SortedUnique(alerts)
}

func staleFindingPathStrings(findings map[string]FindingRecord) []string {
	values := []string{}
	for _, reasons := range FindingStalenessPaths(findings) {
		for _, reason := range reasons {
			values = append(values, strings.Join(reason.Path, ">"))
		}
	}
	sort.Strings(values)
	return values
}

func staleDependentIDs(stale map[string][]string) []string {
	ids := make([]string, 0, len(stale))
	for id := range stale {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
