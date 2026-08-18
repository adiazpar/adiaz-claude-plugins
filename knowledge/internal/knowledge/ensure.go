package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureProject is the session-start operation: detect the campaign schema,
// recover missing managed files only for an initialized 0.8 project, run
// bounded cheap repairs, and return
// the two-block status with an `ensure` summary. Repairs are silent
// self-healing per the reporting law: they are recorded here in
// system-facing state and never narrated to the user.
//
// The boundary is deliberate and tested: ensure runs only recovery,
// migration, and an index reconcile under a time budget. Benchmarks,
// calibration, evidence pinning, profile promotion, and declared-evidence
// updates are expensive or judgment-laden and always ask first.
func EnsureProject(
	ctx context.Context, projectRoot, pluginRoot string, budgetMs int,
) (map[string]any, error) {
	if budgetMs <= 0 {
		budgetMs = 7000
	}
	repairs := []string{}
	budgetExhausted := false

	projectVersion, versionErr := DetectProjectStateVersion(projectRoot)
	if versionErr != nil {
		return nil, versionErr
	}
	if migration, exists, migrationErr := projectMigrationState(projectRoot); migrationErr != nil {
		return nil, migrationErr
	} else if exists && migration.State != "migrated" {
		system := map[string]any{
			"status": "attention", "projectStateVersion": projectVersion,
			"attention":            "migration transaction is incomplete; use migrate-project status or resume; ordinary recovery, indexing, reads, and writes are disabled",
			"migrationState":       migration.State,
			"migrationTransaction": migration.TransactionID,
		}
		payload := map[string]any{"user": BuildUserStatus(system), "system": system}
		payload["ensure"] = map[string]any{
			"migrated": false, "repairs": []string{}, "budgetExhausted": false,
			"projectStateVersion": projectVersion, "attention": system["attention"],
		}
		return payload, nil
	} else if exists && projectVersion != "0.8" {
		return nil, errors.New("ratified migration state exists without a valid 0.8 project state")
	}
	// Migration is never a session-start side effect. A 0.7 (or older)
	// project remains completely unchanged until the manager previews a stable
	// plan and applies its exact digest through migrate-project.
	if projectVersion != "0.8" {
		system := map[string]any{
			"status":              "attention",
			"attention":           "legacy campaign state detected; run migrate-project --preview; no canonical project mutation occurred",
			"projectStateVersion": projectVersion,
		}
		payload := map[string]any{"user": BuildUserStatus(system), "system": system}
		payload["ensure"] = map[string]any{
			"migrated": false, "repairs": []string{}, "budgetExhausted": false,
			"projectStateVersion": projectVersion,
			"attention":           system["attention"],
		}
		return payload, nil
	}
	if recovery, recoverErr := RecoverProject(projectRoot, pluginRoot); recoverErr != nil {
		repairs = append(repairs, "recovery unavailable: "+recoverErr.Error())
	} else {
		for _, action := range recovery.Restored {
			repairs = append(repairs, "restored "+action.Path+" ("+action.Source+")")
		}
	}

	service, serviceErr := NewService(ServiceOptions{
		ProjectRoot: projectRoot, AssetRoot: filepath.Join(pluginRoot, "knowledge"),
	})
	if serviceErr != nil {
		return nil, serviceErr
	}

	repairCtx, cancel := context.WithTimeout(ctx, time.Duration(budgetMs)*time.Millisecond)
	defer cancel()
	if _, reconcileErr := service.ReconcileIndex(repairCtx); reconcileErr != nil {
		if repairCtx.Err() != nil {
			budgetExhausted = true
			repairs = append(repairs,
				"index refresh hit the time budget; it completes on the next search")
		} else {
			repairs = append(repairs, "index refresh unavailable: "+reconcileErr.Error())
		}
	}

	payload, statusErr := service.Status(ctx)
	if statusErr != nil {
		return nil, statusErr
	}
	payload["ensure"] = map[string]any{
		"migrated":            false,
		"repairs":             repairs,
		"budgetExhausted":     budgetExhausted,
		"projectStateVersion": projectVersion,
	}
	return payload, nil
}

// DetectProjectStateVersion is a read-only campaign-schema check. Bootstrap
// configuration and campaign-state schemas are independent. A valid state
// head or campaign record identifies 0.8 directly; the exact shared-laws
// handshake also identifies a damaged but recoverable 0.8 control plane.
func DetectProjectStateVersion(projectRoot string) (string, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return "", err
	}
	marker := filepath.Join(boundary.Root, ".re-discipline", "state", "head.json")
	if info, statErr := os.Lstat(marker); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("0.8 state head is not a regular file")
		}
		store := NewStateStoreWithBoundary(boundary)
		if _, loadErr := store.LoadHead(); loadErr != nil {
			return "", fmt.Errorf("invalid 0.8 state head: %w", loadErr)
		}
		return "0.8", nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	// Any surviving 0.8-only control-plane shape makes the project 0.8 (or a
	// damaged/mixed 0.8 project), even when its head is missing. The explicit
	// 0.7 migrator must never reinterpret a partial 0.8 tree as legacy prose.
	stateRoot := filepath.Join(boundary.Root, ".re-discipline", "state")
	if info, statErr := os.Lstat(stateRoot); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("0.8 state root is a symbolic link")
		}
		return "0.8", nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	archiveRoot := filepath.Join(boundary.Root, "docs", "history", "campaigns")
	if info, statErr := os.Lstat(archiveRoot); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("0.8 campaign archive root is a symbolic link")
		}
		return "0.8", nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	active := filepath.Join(boundary.Root, "active")
	entries, readErr := os.ReadDir(active)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", readErr
	}
	for _, entry := range entries {
		if !entry.IsDir() || !managedSlugRE.MatchString(entry.Name()) {
			continue
		}
		campaignRoot := filepath.Join(active, entry.Name())
		for _, name := range []string{"campaign.json", "STATE.md", "work-items", "runs", "events", "closure"} {
			path := filepath.Join(campaignRoot, name)
			if info, statErr := os.Lstat(path); statErr == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return "", fmt.Errorf("0.8 campaign path %s is a symbolic link", filepath.ToSlash(filepath.Join("active", entry.Name(), name)))
				}
				return "0.8", nil
			} else if !os.IsNotExist(statErr) {
				return "", statErr
			}
		}
	}
	profile := filepath.Join(boundary.Root, ".re-discipline", "project-profile.md")
	if info, statErr := os.Lstat(profile); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("project profile is not a regular file")
		}
		body, readErr := os.ReadFile(profile)
		if readErr != nil {
			return "", readErr
		}
		if strings.Contains(string(body), SharedLawsMarker) {
			return "0.8", nil
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	return "0.7", nil
}
