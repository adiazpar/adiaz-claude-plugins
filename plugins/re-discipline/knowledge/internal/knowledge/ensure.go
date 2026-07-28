package knowledge

import (
	"context"
	"path/filepath"
	"time"
)

// EnsureProject is the session-start operation: recover missing managed
// files, migrate a legacy layout, run bounded cheap repairs, and return
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

	migration, migrateErr := MigrateLegacyLayout(projectRoot)
	if migrateErr != nil {
		repairs = append(repairs, "migration failed: "+migrateErr.Error())
	}
	repairs = append(repairs, migration.Moves...)
	repairs = append(repairs, migration.Warnings...)

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
		"migrated":        migration.Migrated,
		"repairs":         repairs,
		"budgetExhausted": budgetExhausted,
	}
	return payload, nil
}
