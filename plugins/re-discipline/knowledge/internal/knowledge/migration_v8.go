package knowledge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const MigrationSchemaVersion = 1

var migrationStates = []string{
	"legacy", "inventoried", "shadow-indexed", "normalized",
	"physically-reorganized", "traversal-verified", "migrated",
}

type MigrationSource struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Role        string `json:"role"`
	Destination string `json:"destination"`
	Disposition string `json:"disposition"`
	Campaign    string `json:"campaign,omitempty"`
}

type MigrationOperation struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination,omitempty"`
	InputDigest string   `json:"inputDigest"`
	Requires    []string `json:"requires,omitempty"`
}

type MigrationConflict struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Blocks  bool   `json:"blocks"`
}

type MigrationEstimate struct {
	SourceFiles       int `json:"sourceFiles"`
	LegacyReports     int `json:"legacyReports"`
	Campaigns         int `json:"campaigns"`
	ProposedRuns      int `json:"proposedRuns"`
	NormalizedRecords int `json:"normalizedRecords"`
}

type MigrationPlan struct {
	SchemaVersion        int                  `json:"schemaVersion"`
	PlanID               string               `json:"planId"`
	PlanDigest           string               `json:"planDigest"`
	Project              string               `json:"project"`
	ProjectIdentity      string               `json:"projectIdentity"`
	DetectedVersion      string               `json:"detectedVersion"`
	SourceFingerprint    string               `json:"sourceFingerprint"`
	LiveCampaigns        []string             `json:"liveCampaigns"`
	Sources              []MigrationSource    `json:"sources"`
	Operations           []MigrationOperation `json:"operations"`
	Conflicts            []MigrationConflict  `json:"conflicts"`
	Unresolved           []string             `json:"unresolvedClassifications"`
	ProfileChanges       []string             `json:"profileChanges"`
	BaselineRequirements []string             `json:"baselineRequirements"`
	Estimate             MigrationEstimate    `json:"estimate"`
}

type MigrationPreview struct {
	Plan                  MigrationPlan `json:"plan"`
	MigrationPlanYAML     string        `json:"migrationPlanYaml"`
	MigrationPlanMarkdown string        `json:"migrationPlanMarkdown"`
	SourceInventoryJSONL  string        `json:"sourceInventoryJsonl"`
	ConflictReport        any           `json:"conflictReport"`
	BaselineRetrievalPlan any           `json:"baselineRetrievalPlan"`
}

// PreviewMigration is deliberately read-only. It inventories only project
// paths owned by re-discipline and returns stable artifacts; writing those
// artifacts is a separate operation to an operator-selected output directory.
func PreviewMigration(projectRoot string, liveCampaigns []string) (MigrationPreview, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return MigrationPreview{}, err
	}
	liveCampaigns = SortedUnique(liveCampaigns)
	for _, slug := range liveCampaigns {
		if !managedSlugRE.MatchString(slug) {
			return MigrationPreview{}, fmt.Errorf("invalid live campaign slug %q", slug)
		}
	}
	sources, conflicts, err := migrationInventory(boundary)
	if err != nil {
		return MigrationPreview{}, err
	}
	projectName := filepath.Base(boundary.Root)
	profileDigest := "missing"
	for _, source := range sources {
		if source.Path == ".re-discipline/project-profile.md" {
			profileDigest = source.SHA256
			break
		}
	}
	detected := detectCampaignSchema(boundary, sources)
	operations, unresolved := migrationOperations(sources, liveCampaigns)
	sourceFingerprint, err := CanonicalDigest(sources)
	if err != nil {
		return MigrationPreview{}, err
	}
	plan := MigrationPlan{
		SchemaVersion: MigrationSchemaVersion,
		Project:       projectName, ProjectIdentity: profileDigest,
		DetectedVersion: detected, SourceFingerprint: sourceFingerprint,
		LiveCampaigns: liveCampaigns, Sources: sources, Operations: operations,
		Conflicts: conflicts, Unresolved: unresolved,
		ProfileChanges: []string{
			"invalidate 0.7 retrieval acceptance for finding-card representation",
			"retain the named 0.8 baseline until a finding-card suite is ratified",
			"keep raw reports as a lower-ranked default fallback until a gate receipt exists",
		},
		BaselineRequirements: []string{
			"snapshot current retrieval profile and corpus fingerprint",
			"run normalized-versus-raw paired evaluation before archive opt-in",
			"run host parity and blinded traversal before final ratification",
		},
		Estimate: estimateMigration(sources, operations),
	}
	planIDSeed, err := CanonicalDigest(struct {
		Project string
		Source  string
		Live    []string
	}{projectName, sourceFingerprint, liveCampaigns})
	if err != nil {
		return MigrationPreview{}, err
	}
	plan.PlanID = "MP-" + strings.ToUpper(strings.TrimPrefix(planIDSeed, "sha256:")[:16])
	plan.PlanDigest = ""
	plan.PlanDigest, err = CanonicalDigest(plan)
	if err != nil {
		return MigrationPreview{}, err
	}
	return renderMigrationPreview(plan)
}

func migrationInventory(boundary Boundary) ([]MigrationSource, []MigrationConflict, error) {
	roots := []string{".re-discipline", ".claude", ".codex", "AGENTS.md", "active", "docs"}
	sources := []MigrationSource{}
	conflicts := []MigrationConflict{}
	seen := map[string]bool{}
	for _, root := range roots {
		absolute := filepath.Join(boundary.Root, filepath.FromSlash(root))
		info, err := os.Lstat(absolute)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			conflicts = append(conflicts, MigrationConflict{
				Code: "unsafe-link", Path: root,
				Message: "migration-owned root is a symbolic link", Blocks: true,
			})
			continue
		}
		walkErr := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(boundary.Root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if migrationExcluded(relative) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if entryInfo.Mode()&os.ModeSymlink != 0 {
				conflicts = append(conflicts, MigrationConflict{
					Code: "unsafe-link", Path: relative,
					Message: "symbolic links and junctions are not migration inputs", Blocks: true,
				})
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !entryInfo.Mode().IsRegular() || seen[relative] {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			seen[relative] = true
			role, campaign := classifyMigrationSource(relative)
			destination, disposition := migrationDestination(relative, role, campaign)
			sources = append(sources, MigrationSource{
				Path: relative, Size: int64(len(body)), SHA256: SHA256Bytes(body),
				Role: role, Destination: destination, Disposition: disposition,
				Campaign: campaign,
			})
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Path == conflicts[j].Path {
			return conflicts[i].Code < conflicts[j].Code
		}
		return conflicts[i].Path < conflicts[j].Path
	})
	return sources, conflicts, nil
}

func migrationExcluded(relative string) bool {
	clean := strings.ToLower(filepath.ToSlash(relative))
	for _, prefix := range []string{
		".re-discipline/cache/", ".re-discipline/memory/proposals/",
		".re-discipline/migration/", ".git/",
	} {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	return base == "local-paths.md" || forbiddenBaseNames[base] ||
		strings.HasPrefix(base, ".env") || forbiddenExtensions[filepath.Ext(base)]
}

func classifyMigrationSource(path string) (string, string) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	campaign := ""
	if len(parts) > 1 && parts[0] == "active" {
		campaign = parts[1]
	}
	clean := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.HasSuffix(clean, "/campaign.md"):
		return "legacy-campaign-masterfile", campaign
	case strings.HasSuffix(clean, "/reviews.md"):
		return "legacy-review-ledger", campaign
	case strings.Contains(clean, "/subagents/") && strings.HasSuffix(clean, "/report.md"):
		return "legacy-run-report", campaign
	case strings.Contains(clean, "/subagents/"):
		return "legacy-run-file", campaign
	case strings.Contains(clean, "/findings/"):
		return "normalized-finding", campaign
	case strings.Contains(clean, "/intake/"):
		return "intake", campaign
	case strings.Contains(clean, "/reviews/"):
		return "review-receipt", campaign
	case strings.HasPrefix(clean, "docs/truth/"):
		return "truth", campaign
	case strings.HasPrefix(clean, "docs/history/"):
		return "history", campaign
	case strings.HasPrefix(clean, "docs/backlog/"):
		return "backlog", campaign
	case strings.HasPrefix(clean, "active/"):
		return "legacy-campaign-payload", campaign
	case strings.HasPrefix(clean, ".re-discipline/") || strings.HasPrefix(clean, ".claude/") || strings.HasPrefix(clean, ".codex/") || clean == "agents.md":
		return "control-plane", campaign
	default:
		return "navigation", campaign
	}
}

func migrationDestination(path, role, campaign string) (string, string) {
	clean := filepath.ToSlash(path)
	switch role {
	case "legacy-campaign-masterfile":
		return "active/" + campaign + "/campaign.json", "transform"
	case "legacy-review-ledger":
		return "active/" + campaign + "/reviews/imported-ledger.json", "transform"
	case "legacy-run-report", "legacy-run-file":
		parts := strings.Split(clean, "/")
		workspace := "legacy"
		if len(parts) > 3 {
			workspace = parts[3]
		}
		run := legacyRunID(campaign, workspace)
		name := filepath.ToSlash(filepath.Join(parts[4:]...))
		if name == "" {
			name = filepath.Base(clean)
		}
		if name == "brief.md" || name == "context-pack.json" || name == "report.md" {
			return "active/" + campaign + "/runs/" + run + "/" + name, "transform"
		}
		return "active/" + campaign + "/runs/" + run + "/payload/legacy/" + name, "retain-as-provenance"
	case "legacy-campaign-payload":
		return "active/" + campaign + "/runs/" + legacyRunID(campaign, "campaign-import") + "/payload/legacy/" + strings.TrimPrefix(clean, "active/"+campaign+"/"), "retain-as-provenance"
	case "normalized-finding", "intake", "review-receipt", "truth", "history", "backlog", "navigation":
		return clean, "retain"
	case "control-plane":
		return clean, "transform-if-managed"
	default:
		return clean, "review"
	}
}

func legacyRunID(campaign, workspace string) string {
	digest := SHA256String(campaign + "\x00" + workspace)
	value, _ := strconv.ParseUint(strings.TrimPrefix(digest, "sha256:")[:8], 16, 32)
	return fmt.Sprintf("R-19700101-%08d", value%100000000)
}

func migrationOperations(sources []MigrationSource, live []string) ([]MigrationOperation, []string) {
	operations := make([]MigrationOperation, 0, len(sources))
	unresolved := []string{}
	liveSet := map[string]bool{}
	for _, slug := range live {
		liveSet[slug] = true
	}
	for _, source := range sources {
		kind := "retain"
		switch source.Disposition {
		case "transform", "transform-if-managed":
			kind = "transform"
		case "retain-as-provenance":
			kind = "copy-provenance"
		case "review":
			kind = "classify"
		}
		requires := []string{}
		if source.Role == "legacy-run-report" {
			requires = append(requires, "shadow-index")
			if liveSet[source.Campaign] {
				requires = append(requires, "exhaustive-live-normalization")
				unresolved = append(unresolved, source.Path+": live report requires curator coverage")
			}
		}
		operations = append(operations, MigrationOperation{
			ID:   StableID("MOP", source.Path, source.SHA256, source.Destination),
			Kind: kind, Sources: []string{source.Path}, Destination: source.Destination,
			InputDigest: source.SHA256, Requires: requires,
		})
	}
	sort.Strings(unresolved)
	return operations, unresolved
}

func detectCampaignSchema(boundary Boundary, sources []MigrationSource) string {
	for _, source := range sources {
		if strings.HasSuffix(source.Path, "/campaign.json") || source.Path == ".re-discipline/state-head.json" {
			return "0.8"
		}
	}
	return "0.7"
}

func estimateMigration(sources []MigrationSource, operations []MigrationOperation) MigrationEstimate {
	estimate := MigrationEstimate{SourceFiles: len(sources)}
	campaigns := map[string]bool{}
	for _, source := range sources {
		if source.Campaign != "" {
			campaigns[source.Campaign] = true
		}
		if source.Role == "legacy-run-report" {
			estimate.LegacyReports++
		}
		if source.Role == "normalized-finding" {
			estimate.NormalizedRecords++
		}
	}
	runs := map[string]bool{}
	for _, operation := range operations {
		parts := strings.Split(operation.Destination, "/")
		if len(parts) > 3 && parts[0] == "active" && parts[2] == "runs" {
			runs[strings.Join(parts[:4], "/")] = true
		}
	}
	estimate.Campaigns = len(campaigns)
	estimate.ProposedRuns = len(runs)
	return estimate
}

func renderMigrationPreview(plan MigrationPlan) (MigrationPreview, error) {
	var inventory bytes.Buffer
	encoder := json.NewEncoder(&inventory)
	encoder.SetEscapeHTML(false)
	for _, source := range plan.Sources {
		if err := encoder.Encode(source); err != nil {
			return MigrationPreview{}, err
		}
	}
	conflicts := map[string]any{
		"schemaVersion":             MigrationSchemaVersion,
		"planDigest":                plan.PlanDigest,
		"blocking":                  countBlockingConflicts(plan.Conflicts),
		"conflicts":                 plan.Conflicts,
		"unresolvedClassifications": plan.Unresolved,
	}
	baseline := map[string]any{
		"schemaVersion":          MigrationSchemaVersion,
		"planDigest":             plan.PlanDigest,
		"required":               plan.BaselineRequirements,
		"rawArchivePolicy":       "default-fallback",
		"acceptedFindingProfile": nil,
	}
	return MigrationPreview{
		Plan: plan, MigrationPlanYAML: migrationPlanYAML(plan),
		MigrationPlanMarkdown: migrationPlanMarkdown(plan),
		SourceInventoryJSONL:  inventory.String(), ConflictReport: conflicts,
		BaselineRetrievalPlan: baseline,
	}, nil
}

func countBlockingConflicts(conflicts []MigrationConflict) int {
	count := 0
	for _, conflict := range conflicts {
		if conflict.Blocks {
			count++
		}
	}
	return count
}

func migrationPlanYAML(plan MigrationPlan) string {
	var builder strings.Builder
	builder.WriteString("schemaVersion: 1\n")
	builder.WriteString("planId: " + plan.PlanID + "\n")
	builder.WriteString("planDigest: " + plan.PlanDigest + "\n")
	builder.WriteString("project: " + jsonString(plan.Project) + "\n")
	builder.WriteString("projectIdentity: " + plan.ProjectIdentity + "\n")
	builder.WriteString("detectedVersion: " + jsonString(plan.DetectedVersion) + "\n")
	builder.WriteString("sourceFingerprint: " + plan.SourceFingerprint + "\n")
	builder.WriteString("liveCampaigns:\n")
	for _, slug := range plan.LiveCampaigns {
		builder.WriteString("  - " + jsonString(slug) + "\n")
	}
	builder.WriteString("operations:\n")
	for _, operation := range plan.Operations {
		builder.WriteString("  - id: " + operation.ID + "\n")
		builder.WriteString("    kind: " + operation.Kind + "\n")
		builder.WriteString("    source: " + jsonString(operation.Sources[0]) + "\n")
		builder.WriteString("    destination: " + jsonString(operation.Destination) + "\n")
		builder.WriteString("    inputDigest: " + operation.InputDigest + "\n")
	}
	return builder.String()
}

func migrationPlanMarkdown(plan MigrationPlan) string {
	var builder strings.Builder
	builder.WriteString("# re-discipline 0.7 to 0.8 migration preview\n\n")
	builder.WriteString("This preview is read-only. Applying it requires the exact plan digest.\n\n")
	builder.WriteString("- Project: `" + plan.Project + "`\n")
	builder.WriteString("- Detected version: `" + plan.DetectedVersion + "`\n")
	builder.WriteString("- Plan digest: `" + plan.PlanDigest + "`\n")
	builder.WriteString(fmt.Sprintf("- Sources: %d\n- Operations: %d\n- Blocking conflicts: %d\n- Unresolved classifications: %d\n\n",
		len(plan.Sources), len(plan.Operations), countBlockingConflicts(plan.Conflicts), len(plan.Unresolved)))
	builder.WriteString("## Live campaigns\n\n")
	if len(plan.LiveCampaigns) == 0 {
		builder.WriteString("None designated. Closed legacy reports remain shadow provenance.\n\n")
	} else {
		for _, slug := range plan.LiveCampaigns {
			builder.WriteString("- `" + slug + "`\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Unresolved classifications\n\n")
	if len(plan.Unresolved) == 0 {
		builder.WriteString("None.\n")
	} else {
		for _, item := range plan.Unresolved {
			builder.WriteString("- " + item + "\n")
		}
	}
	return builder.String()
}

func jsonString(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}

// WriteMigrationPreview writes only to an explicit output directory. The
// caller must keep that directory outside canonical project state; this method
// refuses paths inside the managed project root.
func WriteMigrationPreview(projectRoot, outputDir string, preview MigrationPreview) error {
	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if withinRoot(filepath.Clean(projectAbs), filepath.Clean(outputAbs)) {
		return errors.New("migration preview output must be outside canonical project state")
	}
	if err := os.MkdirAll(outputAbs, 0o700); err != nil {
		return err
	}
	conflictBody, err := json.MarshalIndent(preview.ConflictReport, "", "  ")
	if err != nil {
		return err
	}
	baselineBody, err := json.MarshalIndent(preview.BaselineRetrievalPlan, "", "  ")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"migration-plan.yaml":          []byte(preview.MigrationPlanYAML),
		"migration-plan.md":            []byte(preview.MigrationPlanMarkdown),
		"source-inventory.jsonl":       []byte(preview.SourceInventoryJSONL),
		"conflict-report.json":         append(conflictBody, '\n'),
		"baseline-retrieval-plan.json": append(baselineBody, '\n'),
	}
	for name, body := range files {
		if err := AtomicWrite(filepath.Join(outputAbs, name), body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// ReadMigrationPlanYAML reads the deliberately small, deterministic preview
// format. Canonical application uses the JSON-equivalent plan retained by the
// operator; this reader exists to validate the headline identity fields.
func ReadMigrationPlanYAML(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		values[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if values["planDigest"] == "" || values["planId"] == "" {
		return nil, errors.New("migration plan is missing identity fields")
	}
	return values, nil
}
