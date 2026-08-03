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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MigrationSchemaVersion = 1

var migrationStates = []string{
	"legacy", "inventoried", "shadow-indexed", "normalized",
	"physically-reorganized", "traversal-verified", "migrated",
}

type MigrationSource struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	MtimeNS     int64  `json:"mtimeNs"`
	SHA256      string `json:"sha256"`
	Role        string `json:"role"`
	Destination string `json:"destination"`
	Disposition string `json:"disposition"`
	Campaign    string `json:"campaign,omitempty"`
}

type MigrationOperation struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Sources      []string `json:"sources"`
	Destination  string   `json:"destination,omitempty"`
	Destinations []string `json:"destinations,omitempty"`
	InputDigest  string   `json:"inputDigest"`
	Requires     []string `json:"requires,omitempty"`
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

type MigrationHostObservation struct {
	Host                string `json:"host"`
	AdapterPath         string `json:"adapterPath"`
	AdapterStatus       string `json:"adapterStatus"`
	ConfigurationPath   string `json:"configurationPath"`
	ConfigurationStatus string `json:"configurationStatus"`
	Availability        string `json:"availability"`
	StartupStatus       string `json:"startupStatus"`
	ToolSchemaStatus    string `json:"toolSchemaStatus"`
}

type MigrationHostInventory struct {
	RuntimeVersion       string                     `json:"runtimeVersion"`
	RuntimeSource        string                     `json:"runtimeSource"`
	RuntimeAvailability  string                     `json:"runtimeAvailability"`
	InstalledPlugin      string                     `json:"installedPlugin"`
	CLIAvailability      string                     `json:"cliAvailability"`
	CLISource            string                     `json:"cliSource"`
	MCPConfigurationPath string                     `json:"mcpConfigurationPath"`
	MCPConfiguration     string                     `json:"mcpConfiguration"`
	MCPStartupStatus     string                     `json:"mcpStartupStatus"`
	MCPToolSchemaStatus  string                     `json:"mcpToolSchemaStatus"`
	ManagerHosts         []MigrationHostObservation `json:"managerHosts"`
	ProjectPolicyPath    string                     `json:"projectPolicyPath"`
	ProjectPolicyStatus  string                     `json:"projectPolicyStatus"`
	ProjectPolicyDigest  string                     `json:"projectPolicyDigest"`
	EvidenceWallStatus   string                     `json:"evidenceWallStatus"`
}

type MigrationTruthPlan struct {
	SourcePath         string   `json:"sourcePath"`
	SourceDigest       string   `json:"sourceDigest"`
	SourceText         string   `json:"sourceText"`
	FindingID          string   `json:"findingId"`
	Destination        string   `json:"destination"`
	Title              string   `json:"title"`
	Claim              string   `json:"claim"`
	LegacyConfidence   string   `json:"legacyConfidence,omitempty"`
	LegacyVerifiedAt   string   `json:"legacyVerifiedAt,omitempty"`
	LegacyStatus       string   `json:"legacyStatus,omitempty"`
	LegacyCorrection   string   `json:"legacyCorrection,omitempty"`
	LegacyScope        []string `json:"legacyScope"`
	LegacyExclusions   []string `json:"legacyExclusions"`
	LegacyDependencies []string `json:"legacyDependencies"`
	SyntheticQuestions []string `json:"syntheticQuestions"`
	ReviewDigest       string   `json:"reviewDigest"`
	SplitIndex         int      `json:"splitIndex"`
	SplitCount         int      `json:"splitCount"`
	ClaimDigest        string   `json:"claimDigest"`
}

type MigrationPlan struct {
	SchemaVersion        int                                 `json:"schemaVersion"`
	PlanID               string                              `json:"planId"`
	PlanDigest           string                              `json:"planDigest"`
	Project              string                              `json:"project"`
	ProjectIdentity      string                              `json:"projectIdentity"`
	DetectedVersion      string                              `json:"detectedVersion"`
	SourceFingerprint    string                              `json:"sourceFingerprint"`
	LiveCampaigns        []string                            `json:"liveCampaigns"`
	Sources              []MigrationSource                   `json:"sources"`
	Operations           []MigrationOperation                `json:"operations"`
	Conflicts            []MigrationConflict                 `json:"conflicts"`
	Unresolved           []string                            `json:"unresolvedClassifications"`
	ProfileChanges       []string                            `json:"profileChanges"`
	ProfileBaseline      MigrationProfileBaseline            `json:"profileBaseline"`
	ProfileDecision      *MigrationProfileConversionDecision `json:"profileDecision,omitempty"`
	BaselineRequirements []string                            `json:"baselineRequirements"`
	HostInventory        MigrationHostInventory              `json:"hostInventory"`
	TruthConversions     []MigrationTruthPlan                `json:"truthConversions"`
	Estimate             MigrationEstimate                   `json:"estimate"`
}

// MigrationPreviewReceipt makes a read-only preview independently
// verifiable. ArtifactDigest covers every rendered preview artifact while
// EquivalenceDigest binds each inventoried source to every planned
// destination. Neither value is an approval: application still requires the
// exact PlanDigest after a fresh inventory.
type MigrationPreviewReceipt struct {
	SchemaVersion     int    `json:"schemaVersion"`
	PlanDigest        string `json:"planDigest"`
	SourceFingerprint string `json:"sourceFingerprint"`
	ArtifactDigest    string `json:"artifactDigest"`
	EquivalenceDigest string `json:"equivalenceDigest"`
	Validation        string `json:"validation"`
	Digest            string `json:"digest"`
}

type MigrationPreview struct {
	Plan                  MigrationPlan           `json:"plan"`
	MigrationPlanYAML     string                  `json:"migrationPlanYaml"`
	MigrationPlanMarkdown string                  `json:"migrationPlanMarkdown"`
	SourceInventoryJSONL  string                  `json:"sourceInventoryJsonl"`
	ConflictReport        any                     `json:"conflictReport"`
	BaselineRetrievalPlan any                     `json:"baselineRetrievalPlan"`
	Receipt               MigrationPreviewReceipt `json:"receipt"`
}

// PreviewMigration is deliberately read-only. It inventories only project
// paths owned by re-discipline and returns stable artifacts; writing those
// artifacts is a separate operation to an operator-selected output directory.
func PreviewMigration(projectRoot string, liveCampaigns []string) (MigrationPreview, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return MigrationPreview{}, err
	}
	detected, err := DetectProjectStateVersion(boundary.Root)
	if err != nil {
		return MigrationPreview{}, err
	}
	if detected != "0.7" {
		return MigrationPreview{}, fmt.Errorf("0.7-to-0.8 migration preview requires a legacy 0.7 project, got %s", detected)
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
	projectIdentityDigest := "missing"
	profilePresent := false
	for _, source := range sources {
		if source.Path == ".re-discipline/project-profile.md" {
			projectIdentityDigest = source.SHA256
			profilePresent = true
			break
		}
	}
	if !profilePresent {
		conflicts = append(conflicts, MigrationConflict{Code: "project-identity-missing",
			Path: ".re-discipline/project-profile.md", Message: "project identity cannot be established without the canonical profile", Blocks: true})
	}
	campaigns := map[string]bool{}
	for _, source := range sources {
		if source.Campaign != "" {
			campaigns[source.Campaign] = true
		}
	}
	for _, slug := range liveCampaigns {
		if !campaigns[slug] {
			conflicts = append(conflicts, MigrationConflict{
				Code: "live-campaign-missing", Path: "active/" + slug,
				Message: "manager-designated live campaign is absent from the approved source inventory", Blocks: true,
			})
		}
	}
	profileBaseline, err := migrationPackagedProfileBaseline()
	if err != nil {
		return MigrationPreview{}, err
	}
	profileStatus, profilePath, retrievalProfileDigest, profileReason := migrationLegacyProfileCompatibility(boundary, sources)
	profileDecisionStatus := "not-required"
	var profileDecision *MigrationProfileConversionDecision
	if profileStatus == "unsupported" {
		packet, packetErr := buildMigrationProfileConflictPacket(boundary, sources)
		if packetErr != nil {
			return MigrationPreview{}, packetErr
		}
		decision, decisionErr := loadMigrationProfileDecision(boundary.Root, packet)
		if decisionErr == nil {
			profileDecision = &decision
			profileDecisionStatus = "sealed:" + decision.Digest
		} else if os.IsNotExist(decisionErr) {
			profileDecisionStatus = "required-unsubmitted"
			conflicts = append(conflicts, MigrationConflict{
				Code: "unsupported-retrieval-profile", Path: profilePath, Blocks: true,
				Message: "accepted 0.7 retrieval profile is unsupported by the 0.8 finding-card runtime: " + profileReason +
					" Export the profile conflict packet and submit an explicit digest-bound non-activating manager decision.",
			})
		} else {
			profileDecisionStatus = "invalid-or-stale"
			conflicts = append(conflicts, MigrationConflict{
				Code: "retrieval-profile-decision-invalid", Path: profilePath, Blocks: true,
				Message: "submitted retrieval-profile conversion decision is invalid or stale: " + decisionErr.Error(),
			})
		}
	}
	if policySource, ok := migrationSourceWithPath(sources, ".re-discipline/knowledge/policy.jsonc"); ok {
		body, readErr := readMigrationSource(boundary.Root, policySource)
		if readErr != nil {
			return MigrationPreview{}, readErr
		}
		if _, policyErr := migratedKnowledgePolicy(body); policyErr != nil {
			conflicts = append(conflicts, MigrationConflict{
				Code: "unsupported-knowledge-policy", Path: policySource.Path, Blocks: true,
				Message: "legacy knowledge policy cannot be converted without changing unreviewed behavior: " + policyErr.Error(),
			})
		}
	}
	// The source fingerprint answers one question: did the project's own
	// bytes change while the manager reviewed the packets? It is therefore
	// taken over the sources exactly as read, before any planned destination
	// is stamped onto them. Deriving it after destination planning would make
	// the fingerprint move whenever a manager submits a truth review, which
	// no submission is allowed to do.
	sourceFingerprint, err := CanonicalDigest(sourcesAsRead(sources))
	if err != nil {
		return MigrationPreview{}, err
	}
	truthConversions, truthConflicts, err := migrationTruthPlans(boundary, sources)
	if err != nil {
		return MigrationPreview{}, err
	}
	conflicts = append(conflicts, truthConflicts...)
	applyMigrationTruthDestinations(sources, truthConversions)
	operations, unresolved := migrationOperations(sources, truthConversions, liveCampaigns, profileDecision)
	conflicts = append(conflicts, migrationDestinationConflicts(boundary, operations)...)
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Path == conflicts[j].Path {
			return conflicts[i].Code < conflicts[j].Code
		}
		return conflicts[i].Path < conflicts[j].Path
	})
	plan := MigrationPlan{
		SchemaVersion: MigrationSchemaVersion,
		Project:       projectName, ProjectIdentity: projectIdentityDigest,
		DetectedVersion: detected, SourceFingerprint: sourceFingerprint,
		LiveCampaigns: liveCampaigns, Sources: sources, Operations: operations,
		Conflicts: conflicts, Unresolved: unresolved,
		ProfileChanges: []string{
			"legacy profile compatibility=" + profileStatus + " path=" + profilePath + " digest=" + retrievalProfileDigest + " reason=" + profileReason,
			"legacy profile conversion decision=" + profileDecisionStatus,
			"invalidate 0.7 retrieval acceptance for finding-card representation",
			"retain the named 0.8 baseline until a finding-card suite is ratified",
			"keep raw reports as a lower-ranked default fallback until a gate receipt exists",
		},
		ProfileBaseline: profileBaseline, ProfileDecision: profileDecision,
		BaselineRequirements: []string{
			"bind any unsupported legacy profile decision to the exact source, packaged baseline, effective profile, and measurement evidence digests",
			"do not treat an unsupported or stale 0.7 profile as accepted 0.8 evidence",
			"do not activate or promote a project retrieval profile during migration",
			"run normalized-versus-raw paired evaluation before archive opt-in",
			"run host parity and blinded traversal before final ratification",
		},
		HostInventory:    migrationHostInventory(boundary, sources),
		TruthConversions: truthConversions,
		Estimate:         estimateMigration(sources, operations),
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

func migrationSourceWithPath(sources []MigrationSource, target string) (MigrationSource, bool) {
	for _, source := range sources {
		if source.Path == target {
			return source, true
		}
	}
	return MigrationSource{}, false
}

func migrationHostInventory(boundary Boundary, sources []MigrationSource) MigrationHostInventory {
	byPath := map[string]MigrationSource{}
	for _, source := range sources {
		byPath[source.Path] = source
	}
	status := func(path string) string {
		if _, ok := byPath[path]; ok {
			return "observed-present"
		}
		return "observed-absent"
	}
	host := func(name, adapterPath, configurationPath string) MigrationHostObservation {
		adapterStatus := status(adapterPath)
		availability := "not-probed"
		if adapterStatus == "observed-absent" {
			availability = "unavailable"
		}
		return MigrationHostObservation{
			Host: name, AdapterPath: adapterPath, AdapterStatus: adapterStatus,
			ConfigurationPath: configurationPath, ConfigurationStatus: status(configurationPath),
			Availability: availability, StartupStatus: "not-probed", ToolSchemaStatus: "not-probed",
		}
	}
	policyPath := ".re-discipline/project-profile.md"
	policyStatus := status(policyPath)
	policyDigest := "missing"
	wallStatus := "observed-absent"
	if source, ok := byPath[policyPath]; ok {
		policyDigest = "sha256:" + source.SHA256
		if body, err := readMigrationSource(boundary.Root, source); err == nil &&
			(strings.Contains(string(body), "## The Wall") || strings.Contains(string(body), "# The Wall")) {
			wallStatus = "observed-present"
		}
	}
	return MigrationHostInventory{
		RuntimeVersion: RuntimeVersion, RuntimeSource: "invoking-shared-engine",
		RuntimeAvailability: "available", InstalledPlugin: "not-probed",
		CLIAvailability: "available", CLISource: "invoking-shared-engine",
		MCPConfigurationPath: ".mcp.json", MCPConfiguration: "not-probed",
		MCPStartupStatus: "not-probed", MCPToolSchemaStatus: "not-probed",
		ManagerHosts: []MigrationHostObservation{
			host("claude", ".claude/CLAUDE.md", ".claude/settings.json"),
			host("codex", ".codex/AGENTS.md", ".codex/config.toml"),
		},
		ProjectPolicyPath: policyPath, ProjectPolicyStatus: policyStatus,
		ProjectPolicyDigest: policyDigest, EvidenceWallStatus: wallStatus,
	}
}

func migrationLegacyProfileCompatibility(
	boundary Boundary,
	sources []MigrationSource,
) (status, path, digest, reason string) {
	path = ".re-discipline/knowledge/retrieval-profile.json"
	digest = "missing"
	for _, source := range sources {
		if source.Path != path {
			continue
		}
		digest = source.SHA256
		body, err := readMigrationSource(boundary.Root, source)
		if err != nil {
			return "unsupported", path, digest, err.Error()
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return "unsupported", path, digest, "profile is not valid JSON"
		}
		version, _ := raw["schemaVersion"].(float64)
		text := strings.ToLower(string(body))
		if int(version) != 1 || strings.Contains(text, `"dense"`) || strings.Contains(text, `"rerank"`) ||
			strings.Contains(text, `"embedding"`) && !strings.Contains(text, `"embedding": null`) {
			return "unsupported", path, digest, "schema or model lanes are outside the packaged 0.8 runtime contract"
		}
		return "stale", path, digest, "0.7 report-level acceptance cannot certify the 0.8 finding-card representation"
	}
	return "plugin-baseline", path, digest, "no project-specific accepted profile was present"
}

func migrationInventory(boundary Boundary) ([]MigrationSource, []MigrationConflict, error) {
	// Inventory only re-discipline-owned state. In particular, ordinary docs
	// and host-local files are outside the approved snapshot: an unrelated
	// edit there must neither invalidate a plan nor be swept into activation.
	roots := []string{
		".re-discipline",
		"AGENTS.md",
		".claude/CLAUDE.md", ".claude/settings.json",
		".codex/AGENTS.md", ".codex/config.toml", ".codex/external-drafter-contract.md",
		"active",
		"docs/INDEX.md",
		"docs/truth", "docs/history", "docs/backlog",
	}
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
			if validOne(strings.ToLower(relative), ".re-discipline/migration", ".re-discipline/migration/0.8") {
				entryInfo, infoErr := entry.Info()
				if infoErr != nil {
					return infoErr
				}
				if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.IsDir() {
					conflicts = append(conflicts, MigrationConflict{
						Code: "unsafe-migration-root", Path: relative,
						Message: "the migration transaction root must be a real project-local directory", Blocks: true,
					})
					return nil
				}
			}
			if migrationExcluded(relative) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				if migrationExcludedPathWouldBeReplaced(relative) {
					conflicts = append(conflicts, MigrationConflict{
						Code: "excluded-managed-file", Path: relative,
						Message: "an excluded or sensitive file exists inside a migration-owned replacement root; move it to a project-owned location or explicitly remove it before preview",
						Blocks:  true,
					})
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
			parts := strings.Split(relative, "/")
			if len(parts) == 2 && parts[0] == "active" {
				conflicts = append(conflicts, MigrationConflict{Code: "active-root-file", Path: relative,
					Message: "non-placeholder files at the active root cannot be assigned to a campaign", Blocks: true})
			}
			if campaign != "" && !managedSlugRE.MatchString(campaign) {
				conflicts = append(conflicts, MigrationConflict{Code: "invalid-campaign-slug", Path: relative,
					Message: "legacy campaign directory is not a valid canonical slug", Blocks: true})
			}
			if (strings.HasSuffix(strings.ToLower(relative), ".md") ||
				strings.HasSuffix(strings.ToLower(relative), ".json") ||
				strings.HasSuffix(strings.ToLower(relative), ".jsonl")) && !utf8.Valid(body) {
				conflicts = append(conflicts, MigrationConflict{
					Code: "undecodable-managed-text", Path: relative,
					Message: "managed text input is not valid UTF-8 and cannot be preserved semantically", Blocks: true,
				})
			}
			destination, disposition := migrationDestination(relative, role, campaign)
			sources = append(sources, MigrationSource{
				Path: relative, Size: int64(len(body)), MtimeNS: entryInfo.ModTime().UTC().UnixNano(), SHA256: SHA256Bytes(body),
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

func migrationExcludedPathWouldBeReplaced(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(path))
	for _, prefix := range []string{
		"active/", ".re-discipline/knowledge/", ".re-discipline/agents/",
		"docs/truth/", "docs/history/", "docs/backlog/",
	} {
		if strings.HasPrefix(clean, prefix) {
			base := strings.ToLower(filepath.Base(clean))
			return base != ".gitkeep" && base != ".keep" && base != ".ds_store"
		}
	}
	return false
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
	return base == "local-paths.md" || base == ".gitkeep" || base == ".keep" ||
		base == ".ds_store" || forbiddenBaseNames[base] ||
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
		base := strings.ToLower(filepath.Base(clean))
		if filepath.Ext(base) != ".md" || validOne(base, "index.md", "readme.md") ||
			strings.Contains(clean, "/evidence/") || strings.Contains(clean, "/assets/") || strings.Contains(clean, "/support/") {
			return "truth-support", campaign
		}
		return "truth", campaign
	case strings.HasPrefix(clean, "docs/history/"):
		return "history", campaign
	case strings.HasPrefix(clean, "docs/backlog/"):
		return "backlog", campaign
	case strings.HasPrefix(clean, ".re-discipline/memory/"):
		return "shared-memory", campaign
	case clean == ".re-discipline/knowledge/retrieval-profile.json":
		return "legacy-retrieval-profile", campaign
	case strings.HasPrefix(clean, ".re-discipline/knowledge/measurements/"):
		return "measurement", campaign
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
		return "active/" + campaign + "/runs/" + legacyRunID(campaign, "campaign-import") + "/payload/legacy/review-import.json", "transform"
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
	case "legacy-retrieval-profile":
		return ".re-discipline/knowledge/migration/legacy-retrieval-profile.json", "retain-as-provenance"
	case "normalized-finding", "intake", "review-receipt":
		return clean, "transform"
	case "truth", "truth-support", "history", "backlog", "shared-memory", "navigation", "measurement":
		if role == "truth" {
			return "docs/truth/findings/" + stableLegacyTruthFindingID(clean) + ".md", "transform"
		}
		if role == "measurement" {
			return clean, "retain"
		}
		if migrationStageSource(clean) {
			return clean, "transform-if-managed"
		}
		return clean, "retain"
	case "control-plane":
		if migrationStageSource(clean) {
			return clean, "transform-if-managed"
		}
		return clean, "retain"
	default:
		return clean, "review"
	}
}

func migrationTruthPlans(boundary Boundary, sources []MigrationSource) ([]MigrationTruthPlan, []MigrationConflict, error) {
	plans := []MigrationTruthPlan{}
	conflicts := []MigrationConflict{}
	for _, source := range sources {
		if source.Role != "truth" {
			continue
		}
		body, err := readMigrationSource(boundary.Root, source)
		if err != nil {
			return nil, nil, err
		}
		prelude := ExtractDocumentPrelude(string(body), source.Path)
		title := normalizePreludeField(prelude.Title)
		dependencies := legacyTruthDependencyPaths(body, source.Path)
		legacyScope, legacyExclusions := legacyTruthScopeAndExclusions(body)
		if reviewConflict, needsReview := migrationTruthConflictForSource(source, body); needsReview {
			review, reviewErr := loadMigrationTruthReview(boundary.Root, reviewConflict)
			if reviewErr != nil {
				if os.IsNotExist(reviewErr) {
					message := reviewConflict.RequiredResolution + " Export the truth conflict packet and submit a digest-bound manager review."
					conflicts = append(conflicts, MigrationConflict{Code: reviewConflict.Code, Path: source.Path, Message: message, Blocks: true})
				} else {
					conflicts = append(conflicts, MigrationConflict{Code: "truth-atomicization-review-invalid", Path: source.Path,
						Message: "submitted truth atomicization review is invalid or stale: " + reviewErr.Error(), Blocks: true})
				}
				continue
			}
			for index, reviewed := range review.Claims {
				id := stableLegacyTruthFindingID(source.Path)
				if len(review.Claims) > 1 {
					id = stableLegacyTruthSplitFindingID(source.Path, index+1, reviewed.SourceText)
				}
				row, rowErr := buildMigrationTruthPlan(source, prelude, dependencies, legacyScope, legacyExclusions, reviewed.SourceText,
					reviewed.Title, reviewed.Claim, reviewed.SyntheticQuestions, review.Digest,
					id, index+1, len(review.Claims))
				if rowErr != nil {
					return nil, nil, rowErr
				}
				plans = append(plans, row)
			}
			continue
		}
		claim := legacyTruthAtomicClaim(body)
		questions := SortedUnique(legacyTruthSyntheticQuestions(title, claim))
		row, rowErr := buildMigrationTruthPlan(source, prelude, dependencies, legacyScope, legacyExclusions, claim, title, claim, questions, "",
			stableLegacyTruthFindingID(source.Path), 1, 1)
		if rowErr != nil {
			return nil, nil, rowErr
		}
		plans = append(plans, row)
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].SourcePath == plans[j].SourcePath {
			return plans[i].SplitIndex < plans[j].SplitIndex
		}
		return plans[i].SourcePath < plans[j].SourcePath
	})
	return plans, conflicts, nil
}

func buildMigrationTruthPlan(
	source MigrationSource,
	prelude DocumentPrelude,
	dependencies []string,
	legacyScope, legacyExclusions []string,
	sourceText, title, claim string,
	questions []string,
	reviewDigest, id string,
	splitIndex, splitCount int,
) (MigrationTruthPlan, error) {
	questions = SortedUnique(questions)
	destination := "docs/truth/findings/" + id + ".md"
	claimDigest, err := CanonicalDigest(struct {
		SourceDigest string   `json:"sourceDigest"`
		SourceText   string   `json:"sourceText"`
		Title        string   `json:"title"`
		Claim        string   `json:"claim"`
		Confidence   string   `json:"confidence"`
		VerifiedAt   string   `json:"verifiedAt"`
		Status       string   `json:"status"`
		Correction   string   `json:"correction"`
		Scope        []string `json:"scope"`
		Exclusions   []string `json:"exclusions"`
		Dependencies []string `json:"dependencies"`
		Questions    []string `json:"questions"`
		ReviewDigest string   `json:"reviewDigest"`
		SplitIndex   int      `json:"splitIndex"`
		SplitCount   int      `json:"splitCount"`
	}{"sha256:" + source.SHA256, sourceText, title, claim, prelude.Confidence, prelude.Verified,
		prelude.Status, prelude.Correction, legacyScope, legacyExclusions,
		dependencies, questions, reviewDigest, splitIndex, splitCount})
	if err != nil {
		return MigrationTruthPlan{}, err
	}
	return MigrationTruthPlan{
		SourcePath: source.Path, SourceDigest: "sha256:" + source.SHA256, SourceText: sourceText,
		FindingID: id, Destination: destination, Title: title, Claim: claim,
		LegacyConfidence: prelude.Confidence, LegacyVerifiedAt: prelude.Verified,
		LegacyStatus: prelude.Status, LegacyCorrection: prelude.Correction,
		LegacyScope: legacyScope, LegacyExclusions: legacyExclusions,
		LegacyDependencies: dependencies, SyntheticQuestions: questions,
		ReviewDigest: reviewDigest, SplitIndex: splitIndex, SplitCount: splitCount, ClaimDigest: claimDigest,
	}, nil
}

var legacyTruthScopeLabelRE = regexp.MustCompile(`(?im)^\*\*(?:scope|applies when):\*\*\s*(.+)$`)
var legacyTruthExclusionLabelRE = regexp.MustCompile(`(?im)^\*\*(?:exclusions?|does not establish|known limits?):\*\*\s*(.+)$`)

func legacyTruthScopeAndExclusions(body []byte) ([]string, []string) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	collect := func(pattern *regexp.Regexp, headings ...string) []string {
		values := []string{}
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 {
				if value := normalizeLegacyTruthMetadata(match[1]); value != "" {
					values = append(values, value)
				}
			}
		}
		for _, heading := range headings {
			if value := normalizeLegacyTruthMetadata(markdownSection(body, heading)); value != "" {
				values = append(values, value)
			}
		}
		return SortedUnique(values)
	}
	return collect(legacyTruthScopeLabelRE, "Scope", "Applies when"),
		collect(legacyTruthExclusionLabelRE, "Exclusions", "Does not establish", "Known limits")
}

func normalizeLegacyTruthMetadata(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func applyMigrationTruthDestinations(sources []MigrationSource, plans []MigrationTruthPlan) {
	bySource := map[string][]MigrationTruthPlan{}
	for _, plan := range plans {
		bySource[plan.SourcePath] = append(bySource[plan.SourcePath], plan)
	}
	for index := range sources {
		if sources[index].Role != "truth" {
			continue
		}
		rows := bySource[sources[index].Path]
		switch len(rows) {
		case 0:
		case 1:
			sources[index].Destination = rows[0].Destination
		default:
			sources[index].Destination = legacyTruthSplitManifestPath(sources[index].Path)
		}
	}
}

func legacyRunID(campaign, workspace string) string {
	digest := SHA256String(campaign + "\x00" + workspace)
	value, _ := strconv.ParseUint(strings.TrimPrefix(digest, "sha256:")[:8], 16, 32)
	return fmt.Sprintf("R-19700101-%08d", value%100000000)
}

func stableLegacyTruthFindingID(sourcePath string) string {
	value, _ := strconv.ParseUint(strings.TrimPrefix(SHA256String("legacy-truth\x00"+sourcePath), "sha256:")[:15], 16, 64)
	return fmt.Sprintf("F-%018d", value)
}

func stableLegacyTruthSplitFindingID(sourcePath string, splitIndex int, sourceText string) string {
	return stableLegacyTruthFindingID(fmt.Sprintf("%s\x00split:%d\x00%s", sourcePath, splitIndex, SHA256String(sourceText)))
}

func legacyTruthSplitManifestPath(sourcePath string) string {
	return "docs/truth/splits/" + stableLegacyTruthFindingID(sourcePath) + ".md"
}

func legacyTruthExplicitClaimCount(body []byte) int {
	return len(legacyTruthExplicitClaims(body))
}

func migrationOperations(
	sources []MigrationSource,
	truthPlans []MigrationTruthPlan,
	live []string,
	profileDecision *MigrationProfileConversionDecision,
) ([]MigrationOperation, []string) {
	operations := make([]MigrationOperation, 0, len(sources))
	unresolved := []string{}
	liveSet := map[string]bool{}
	campaignSet := map[string]bool{}
	for _, slug := range live {
		liveSet[slug] = true
	}
	for _, source := range sources {
		if source.Campaign != "" {
			campaignSet[source.Campaign] = true
		}
	}
	carriers := make([]string, 0, len(campaignSet))
	for campaign := range campaignSet {
		carriers = append(carriers, campaign)
	}
	sort.Strings(carriers)
	if len(carriers) == 0 && len(truthPlans) > 0 {
		carriers = []string{"migration-provenance"}
	}
	truthBySource := map[string][]MigrationTruthPlan{}
	for _, truthPlan := range truthPlans {
		truthBySource[truthPlan.SourcePath] = append(truthBySource[truthPlan.SourcePath], truthPlan)
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
		destinations := migrationPlannedDestinations(source)
		if source.Role == "legacy-retrieval-profile" && profileDecision != nil {
			destinations = append(destinations, migrationProfileAuditDecisionPath(source.Path))
			requires = append(requires, "sealed-profile-decision:"+profileDecision.Digest)
		}
		if source.Role == "truth" && len(carriers) > 0 {
			carrier := carriers[0]
			destinations = append(destinations,
				"active/"+carrier+"/runs/"+legacyRunID(carrier, "campaign-import")+"/payload/legacy/truth/"+strings.TrimPrefix(source.Path, "docs/truth/"))
			for _, truthPlan := range truthBySource[source.Path] {
				destinations = append(destinations, truthPlan.Destination,
					".re-discipline/knowledge/migration/truth-receipts/"+truthPlan.FindingID+".json")
			}
			if rows := truthBySource[source.Path]; len(rows) > 0 && rows[0].ReviewDigest != "" {
				destinations = append(destinations, migrationTruthAuditReviewPath(source.Path))
			}
			destinations = SortedUnique(destinations)
		}
		if len(requires) == 0 {
			requires = nil
		}
		operations = append(operations, MigrationOperation{
			ID:   StableID("MOP", source.Path, source.SHA256, source.Destination),
			Kind: kind, Sources: []string{source.Path}, Destination: source.Destination,
			Destinations: destinations, InputDigest: source.SHA256, Requires: requires,
		})
	}
	sort.Strings(unresolved)
	return operations, unresolved
}

func migrationPlannedDestinations(source MigrationSource) []string {
	destinations := []string{source.Destination}
	if source.Campaign == "" {
		return SortedUnique(destinations)
	}
	switch source.Role {
	case "legacy-campaign-masterfile", "legacy-review-ledger":
		run := legacyRunID(source.Campaign, "campaign-import")
		relative := strings.TrimPrefix(source.Path, "active/"+source.Campaign+"/")
		destinations = append(destinations,
			"active/"+source.Campaign+"/runs/"+run+"/payload/legacy/"+relative)
	case "normalized-finding", "intake", "review-receipt":
		run := legacyRunID(source.Campaign, "campaign-import")
		relative := strings.TrimPrefix(source.Path, "active/"+source.Campaign+"/")
		destinations = append(destinations,
			"active/"+source.Campaign+"/runs/"+run+"/payload/legacy/"+relative)
	}
	return SortedUnique(destinations)
}

func migrationDestinationConflicts(
	boundary Boundary, operations []MigrationOperation,
) []MigrationConflict {
	conflicts := []MigrationConflict{}
	owners := map[string]string{}
	for _, operation := range operations {
		for _, destination := range operation.Destinations {
			clean := NormalizeProjectPath(destination)
			if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(destination) {
				conflicts = append(conflicts, MigrationConflict{
					Code: "destination-escape", Path: destination,
					Message: "planned destination escapes the managed project", Blocks: true,
				})
				continue
			}
			if prior, exists := owners[clean]; exists && prior != operation.ID {
				conflicts = append(conflicts, MigrationConflict{
					Code: "destination-collision", Path: clean,
					Message: "multiple migration operations would publish the same destination", Blocks: true,
				})
			} else {
				owners[clean] = operation.ID
			}
			absolute := filepath.Join(boundary.Root, filepath.FromSlash(clean))
			if info, err := os.Lstat(absolute); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					conflicts = append(conflicts, MigrationConflict{
						Code: "unsafe-destination", Path: clean,
						Message: "planned destination is a symbolic link or junction", Blocks: true,
					})
				} else if !containsString(operation.Sources, clean) {
					conflicts = append(conflicts, MigrationConflict{
						Code: "destination-exists", Path: clean,
						Message: "planned destination already exists and belongs to a different approved source", Blocks: true,
					})
				}
			} else if !os.IsNotExist(err) {
				conflicts = append(conflicts, MigrationConflict{
					Code: "destination-unreadable", Path: clean,
					Message: "planned destination could not be inspected", Blocks: true,
				})
			}
		}
	}
	return conflicts
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
	if len(campaigns) == 0 && len(sources) > 0 {
		campaigns["migration-provenance"] = true
	}
	runs := map[string]bool{}
	for campaign := range campaigns {
		runs["active/"+campaign+"/runs/"+legacyRunID(campaign, "campaign-import")] = true
	}
	for _, operation := range operations {
		for _, destination := range SortedUnique(append(append([]string{}, operation.Destinations...), operation.Destination)) {
			parts := strings.Split(destination, "/")
			if len(parts) > 3 && parts[0] == "active" && parts[2] == "runs" {
				runs[strings.Join(parts[:4], "/")] = true
			}
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
		"packagedBaseline":       plan.ProfileBaseline,
		"conversionDecision":     plan.ProfileDecision,
	}
	preview := MigrationPreview{
		Plan: plan, MigrationPlanYAML: migrationPlanYAML(plan),
		MigrationPlanMarkdown: migrationPlanMarkdown(plan),
		SourceInventoryJSONL:  inventory.String(), ConflictReport: conflicts,
		BaselineRetrievalPlan: baseline,
	}
	artifactDigest, err := CanonicalDigest(struct {
		YAML      string `json:"yaml"`
		Markdown  string `json:"markdown"`
		Inventory string `json:"inventory"`
		Conflicts any    `json:"conflicts"`
		Baseline  any    `json:"baseline"`
	}{preview.MigrationPlanYAML, preview.MigrationPlanMarkdown,
		preview.SourceInventoryJSONL, conflicts, baseline})
	if err != nil {
		return MigrationPreview{}, err
	}
	equivalenceDigest, err := CanonicalDigest(plan.Operations)
	if err != nil {
		return MigrationPreview{}, err
	}
	preview.Receipt = MigrationPreviewReceipt{
		SchemaVersion: MigrationSchemaVersion, PlanDigest: plan.PlanDigest,
		SourceFingerprint: plan.SourceFingerprint, ArtifactDigest: artifactDigest,
		EquivalenceDigest: equivalenceDigest, Validation: "passed",
	}
	preview.Receipt.Digest, err = CanonicalDigest(preview.Receipt)
	if err != nil {
		return MigrationPreview{}, err
	}
	return preview, nil
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
	builder.WriteString("profileBaseline:\n")
	builder.WriteString("  profileId: " + jsonString(plan.ProfileBaseline.Profile.ProfileID) + "\n")
	builder.WriteString("  profileDigest: " + plan.ProfileBaseline.ProfileDigest + "\n")
	builder.WriteString("  effectiveProfileName: " + jsonString(plan.ProfileBaseline.EffectiveProfileName) + "\n")
	builder.WriteString("  effectiveProfileDigest: " + plan.ProfileBaseline.EffectiveProfileDigest + "\n")
	builder.WriteString("  measurementEvidenceDigest: " + plan.ProfileBaseline.MeasurementEvidenceDigest + "\n")
	builder.WriteString("  activationState: " + jsonString(plan.ProfileBaseline.ActivationState) + "\n")
	if plan.ProfileDecision == nil {
		builder.WriteString("profileDecision: null\n")
	} else {
		builder.WriteString("profileDecision:\n")
		builder.WriteString("  digest: " + plan.ProfileDecision.Digest + "\n")
		builder.WriteString("  packetDigest: " + plan.ProfileDecision.PacketDigest + "\n")
		builder.WriteString("  decision: " + jsonString(plan.ProfileDecision.Decision) + "\n")
		builder.WriteString("  authority: " + jsonString(plan.ProfileDecision.Authority) + "\n")
		builder.WriteString("  decidedAt: " + jsonString(plan.ProfileDecision.DecidedAt) + "\n")
		builder.WriteString("  replacesDecisionDigest: " + jsonString(plan.ProfileDecision.ReplacesDecisionDigest) + "\n")
		builder.WriteString("  projectProfileActivation: false\n")
	}
	builder.WriteString("liveCampaigns:\n")
	for _, slug := range plan.LiveCampaigns {
		builder.WriteString("  - " + jsonString(slug) + "\n")
	}
	builder.WriteString("hostInventory:\n")
	builder.WriteString("  runtimeVersion: " + jsonString(plan.HostInventory.RuntimeVersion) + "\n")
	builder.WriteString("  runtimeSource: " + jsonString(plan.HostInventory.RuntimeSource) + "\n")
	builder.WriteString("  cliAvailability: " + jsonString(plan.HostInventory.CLIAvailability) + "\n")
	builder.WriteString("  mcpStartupStatus: " + jsonString(plan.HostInventory.MCPStartupStatus) + "\n")
	builder.WriteString("  managerHosts:\n")
	for _, host := range plan.HostInventory.ManagerHosts {
		builder.WriteString("    - host: " + jsonString(host.Host) + "\n")
		builder.WriteString("      adapterStatus: " + jsonString(host.AdapterStatus) + "\n")
		builder.WriteString("      availability: " + jsonString(host.Availability) + "\n")
		builder.WriteString("      startupStatus: " + jsonString(host.StartupStatus) + "\n")
		builder.WriteString("      toolSchemaStatus: " + jsonString(host.ToolSchemaStatus) + "\n")
	}
	builder.WriteString("operations:\n")
	for _, operation := range plan.Operations {
		builder.WriteString("  - id: " + operation.ID + "\n")
		builder.WriteString("    kind: " + operation.Kind + "\n")
		builder.WriteString("    source: " + jsonString(operation.Sources[0]) + "\n")
		builder.WriteString("    destination: " + jsonString(operation.Destination) + "\n")
		builder.WriteString("    destinations:\n")
		for _, destination := range SortedUnique(append(append([]string{}, operation.Destinations...), operation.Destination)) {
			builder.WriteString("      - " + jsonString(destination) + "\n")
		}
		builder.WriteString("    inputDigest: " + operation.InputDigest + "\n")
	}
	builder.WriteString("truthConversions:\n")
	for _, truth := range plan.TruthConversions {
		builder.WriteString("  - sourcePath: " + jsonString(truth.SourcePath) + "\n")
		builder.WriteString("    sourceDigest: " + truth.SourceDigest + "\n")
		builder.WriteString("    sourceText: " + jsonString(truth.SourceText) + "\n")
		builder.WriteString("    findingId: " + truth.FindingID + "\n")
		builder.WriteString("    destination: " + jsonString(truth.Destination) + "\n")
		builder.WriteString("    title: " + jsonString(truth.Title) + "\n")
		builder.WriteString("    claim: " + jsonString(truth.Claim) + "\n")
		builder.WriteString("    legacyStatus: " + jsonString(truth.LegacyStatus) + "\n")
		builder.WriteString("    legacyCorrection: " + jsonString(truth.LegacyCorrection) + "\n")
		builder.WriteString("    legacyScope:\n")
		for _, value := range truth.LegacyScope {
			builder.WriteString("      - " + jsonString(value) + "\n")
		}
		builder.WriteString("    legacyExclusions:\n")
		for _, value := range truth.LegacyExclusions {
			builder.WriteString("      - " + jsonString(value) + "\n")
		}
		builder.WriteString("    reviewDigest: " + jsonString(truth.ReviewDigest) + "\n")
		builder.WriteString(fmt.Sprintf("    splitIndex: %d\n    splitCount: %d\n", truth.SplitIndex, truth.SplitCount))
		builder.WriteString("    claimDigest: " + truth.ClaimDigest + "\n")
		builder.WriteString("    syntheticQuestions:\n")
		for _, question := range truth.SyntheticQuestions {
			builder.WriteString("      - " + jsonString(question) + "\n")
		}
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
	builder.WriteString("## Retrieval profile conversion\n\n")
	builder.WriteString("- Packaged baseline: `" + plan.ProfileBaseline.Profile.ProfileID + "` / `" + plan.ProfileBaseline.ProfileDigest + "`\n")
	builder.WriteString("- Primary effective profile: `" + plan.ProfileBaseline.EffectiveProfileName + "` / `" + plan.ProfileBaseline.EffectiveProfileDigest + "`\n")
	builder.WriteString("- Measurement evidence: `" + plan.ProfileBaseline.MeasurementEvidenceDigest + "`\n")
	if plan.ProfileDecision == nil {
		decisionRequired := false
		for _, conflict := range plan.Conflicts {
			decisionRequired = decisionRequired ||
				conflict.Code == "unsupported-retrieval-profile" ||
				conflict.Code == "retrieval-profile-decision-invalid"
		}
		if decisionRequired {
			builder.WriteString("- Conversion decision: required and unresolved; the legacy profile remains blocking.\n\n")
		} else {
			builder.WriteString("- Conversion decision: not required for this source snapshot.\n\n")
		}
	} else {
		builder.WriteString("- Conversion decision: `" + plan.ProfileDecision.Digest + "` at `" + plan.ProfileDecision.DecidedAt + "`; project-profile activation remains `false`.\n")
		builder.WriteString("- Replaces decision: `" + plan.ProfileDecision.ReplacesDecisionDigest + "`.\n\n")
	}
	builder.WriteString("## Live campaigns\n\n")
	if len(plan.LiveCampaigns) == 0 {
		builder.WriteString("None designated. Closed legacy reports remain shadow provenance.\n\n")
	} else {
		for _, slug := range plan.LiveCampaigns {
			builder.WriteString("- `" + slug + "`\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Host inventory\n\n")
	builder.WriteString("- Invoking runtime: `" + plan.HostInventory.RuntimeVersion + "` from `" + plan.HostInventory.RuntimeSource + "`\n")
	builder.WriteString("- Installed plugin observation: `" + plan.HostInventory.InstalledPlugin + "`\n")
	builder.WriteString("- MCP startup/schema discovery: `" + plan.HostInventory.MCPStartupStatus + "` / `" + plan.HostInventory.MCPToolSchemaStatus + "`\n")
	for _, host := range plan.HostInventory.ManagerHosts {
		builder.WriteString("- `" + host.Host + "`: adapter `" + host.AdapterStatus + "`; availability `" + host.Availability + "`; startup `" + host.StartupStatus + "`; schema `" + host.ToolSchemaStatus + "`\n")
	}
	builder.WriteString("\n")
	builder.WriteString("## Unresolved classifications\n\n")
	if len(plan.Unresolved) == 0 {
		builder.WriteString("None.\n")
	} else {
		for _, item := range plan.Unresolved {
			builder.WriteString("- " + item + "\n")
		}
	}
	builder.WriteString("\n\n## Truth conversions requiring plan approval\n\n")
	if len(plan.TruthConversions) == 0 {
		builder.WriteString("None.\n")
	} else {
		for _, truth := range plan.TruthConversions {
			builder.WriteString("- `" + truth.SourcePath + "` -> `" + truth.Destination + "` (`" + truth.FindingID + "`)\n")
			if truth.ReviewDigest != "" {
				builder.WriteString(fmt.Sprintf("  - Reviewed split: %d/%d; review `%s`\n", truth.SplitIndex, truth.SplitCount, truth.ReviewDigest))
			}
			builder.WriteString("  - Claim: " + truth.Claim + "\n")
			builder.WriteString("  - Approved retrieval questions: " + strings.Join(truth.SyntheticQuestions, " | ") + "\n")
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
	receiptBody, err := json.MarshalIndent(preview.Receipt, "", "  ")
	if err != nil {
		return err
	}
	files["preview-receipt.json"] = append(receiptBody, '\n')
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
