package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrationPreviewFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "project-profile.md"),
		"# Fixture project\n\n<!-- re-discipline:shared-laws v0.7.0 -->\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "config.json"),
		"{\"schemaVersion\":2}\n")
	mustWriteFile(t, filepath.Join(root, ".re-discipline", "local-paths.md"),
		"secret machine path\n")
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"), "router\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "CAMPAIGN.md"),
		"# Campaign: live-campaign\n\n## Objective\n\nFind the answer.\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "REVIEWS.md"),
		"# Review ledger\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "subagents", "worker-a", "brief.md"),
		"# Brief\n")
	mustWriteFile(t, filepath.Join(root, "active", "live-campaign", "subagents", "worker-a", "report.md"),
		"# VERDICT\n\nDIRECT: observed.\n")
	mustWriteFile(t, filepath.Join(root, "active", "closed-campaign", "CAMPAIGN.md"),
		"# Campaign: closed-campaign\n")
	mustWriteFile(t, filepath.Join(root, "active", "closed-campaign", "subagents", "worker-b", "report.md"),
		"# VERDICT\n\nDIRECT: historical.\n")
	mustWriteFile(t, filepath.Join(root, "docs", "truth", "claim.md"), "# Claim\n")
	return root
}

func TestMigrationPreviewIsStableReadOnlyAndDemandDriven(t *testing.T) {
	root := migrationPreviewFixture(t)
	before, err := migrationInventoryDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	second, err := PreviewMigration(root, []string{"live-campaign"})
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if first.Plan.PlanDigest != second.Plan.PlanDigest ||
		first.MigrationPlanYAML != second.MigrationPlanYAML ||
		first.SourceInventoryJSONL != second.SourceInventoryJSONL {
		t.Fatal("identical inputs must produce byte-stable migration previews")
	}
	after, err := migrationInventoryDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("preview changed canonical project state")
	}
	if len(first.Plan.Unresolved) != 1 ||
		!strings.Contains(first.Plan.Unresolved[0], "live-campaign") {
		t.Fatalf("only the designated live report should demand exhaustive coverage: %+v", first.Plan.Unresolved)
	}
	if strings.Contains(first.SourceInventoryJSONL, "local-paths.md") ||
		strings.Contains(first.SourceInventoryJSONL, "secret machine path") {
		t.Fatal("machine-local paths must not enter the migration inventory")
	}
	if first.Plan.Estimate.LegacyReports != 2 || first.Plan.Estimate.Campaigns != 2 {
		t.Fatalf("unexpected estimate: %+v", first.Plan.Estimate)
	}
}

func TestMigrationPreviewDigestInvalidatesOnRelevantChange(t *testing.T) {
	root := migrationPreviewFixture(t)
	first, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(root, "active", "closed-campaign", "subagents", "worker-b", "report.md")
	mustWriteFile(t, report, "# VERDICT\n\nDIRECT: changed.\n")
	second, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.PlanDigest == second.Plan.PlanDigest ||
		first.Plan.SourceFingerprint == second.Plan.SourceFingerprint {
		t.Fatal("a relevant input change must invalidate the approved plan")
	}
}

func TestWriteMigrationPreviewRefusesCanonicalProjectPath(t *testing.T) {
	root := migrationPreviewFixture(t)
	preview, err := PreviewMigration(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMigrationPreview(root, filepath.Join(root, "preview"), preview); err == nil {
		t.Fatal("preview artifacts must not be written into canonical project state")
	}
	output := t.TempDir()
	if err := WriteMigrationPreview(root, output, preview); err != nil {
		t.Fatalf("external preview output: %v", err)
	}
	for _, name := range []string{
		"migration-plan.yaml", "migration-plan.md", "source-inventory.jsonl",
		"conflict-report.json", "baseline-retrieval-plan.json",
	} {
		if info, err := os.Stat(filepath.Join(output, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing preview artifact %s: %v", name, err)
		}
	}
}

func migrationInventoryDigest(root string) (string, error) {
	boundary, err := NewBoundary(root)
	if err != nil {
		return "", err
	}
	sources, conflicts, err := migrationInventory(boundary)
	if err != nil {
		return "", err
	}
	return CanonicalDigest(struct {
		Sources   []MigrationSource
		Conflicts []MigrationConflict
	}{sources, conflicts})
}
