package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type MigrationNormalizedManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	PlanDigest    string            `json:"planDigest"`
	Campaigns     []string          `json:"campaigns"`
	Files         map[string]string `json:"files"`
	LegacySources map[string]string `json:"legacySources"`
	Digest        string            `json:"digest"`
}

type migrationActivationJournal struct {
	SchemaVersion int    `json:"schemaVersion"`
	TransactionID string `json:"transactionId"`
	PlanDigest    string `json:"planDigest"`
	Phase         string `json:"phase"`
	StagedDigest  string `json:"stagedDigest"`
	BackupPath    string `json:"backupPath"`
	Digest        string `json:"digest"`
}

func (engine *MigrationEngine) buildNormalizedStaging(plan MigrationPlan) (MigrationNormalizedManifest, error) {
	state, err := engine.Status()
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	stagingRoot := filepath.Join(engine.migrationRoot(), "staging")
	if err := resetMigrationStaging(engine.migrationRoot(), stagingRoot); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	activeRoot := filepath.Join(stagingRoot, "active")
	if err := os.MkdirAll(activeRoot, 0o700); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	campaignSet := map[string]bool{}
	for _, source := range plan.Sources {
		if source.Campaign != "" {
			campaignSet[source.Campaign] = true
		}
	}
	campaigns := make([]string, 0, len(campaignSet))
	for campaign := range campaignSet {
		if !managedSlugRE.MatchString(campaign) {
			return MigrationNormalizedManifest{}, fmt.Errorf("legacy campaign slug %q is invalid", campaign)
		}
		campaigns = append(campaigns, campaign)
	}
	sort.Strings(campaigns)
	manifest := MigrationNormalizedManifest{
		SchemaVersion: MigrationSchemaVersion, PlanDigest: plan.PlanDigest,
		Campaigns: campaigns, Files: map[string]string{}, LegacySources: map[string]string{},
	}
	for _, campaign := range campaigns {
		if err := engine.stageCampaign(plan, state, campaign, activeRoot, &manifest); err != nil {
			return MigrationNormalizedManifest{}, err
		}
	}
	files, err := digestRegularTree(activeRoot)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	manifest.Files = files
	manifest.Digest = ""
	manifest.Digest, err = CanonicalDigest(manifest)
	if err != nil {
		return MigrationNormalizedManifest{}, err
	}
	if err := AtomicWriteJSON(filepath.Join(stagingRoot, "normalized-manifest.json"), manifest, 0o600); err != nil {
		return MigrationNormalizedManifest{}, err
	}
	return manifest, nil
}

func resetMigrationStaging(migrationRoot, stagingRoot string) error {
	root, err := filepath.Abs(migrationRoot)
	if err != nil {
		return err
	}
	staging, err := filepath.Abs(stagingRoot)
	if err != nil {
		return err
	}
	if !withinRoot(root, staging) || filepath.Clean(root) == filepath.Clean(staging) {
		return errors.New("migration staging path escapes its transaction root")
	}
	if info, err := os.Lstat(staging); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("migration staging path is unsafe")
		}
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(staging, 0o700)
}

func (engine *MigrationEngine) stageCampaign(
	plan MigrationPlan,
	state MigrationState,
	campaign string,
	activeRoot string,
	manifest *MigrationNormalizedManifest,
) error {
	campaignDir := filepath.Join(activeRoot, campaign)
	for _, directory := range []string{"work-items", "runs", "findings", "intake", "reviews", "events", "closure"} {
		if err := os.MkdirAll(filepath.Join(campaignDir, directory), 0o700); err != nil {
			return err
		}
	}
	campaignID := "C-" + strings.ToUpper(campaign)
	masterSource, masterBody, err := engine.legacyCampaignMaster(plan, campaign)
	if err != nil {
		return err
	}
	objective := markdownSection(masterBody, "Objective")
	if objective == "" {
		objective = "Complete manager review of the imported legacy campaign."
	}
	success := markdownListSection(masterBody, "Exit Criteria", "Success Criteria")
	if len(success) == 0 {
		success = []string{"Every imported run and finding has an explicit manager disposition."}
	}
	closure := markdownListSection(masterBody, "Closure Criteria", "Exit Criteria")
	if len(closure) == 0 {
		closure = []string{"Coverage, projection, archive, retrieval, and traversal gates pass."}
	}
	eventID := migrationEventID(campaign, plan.PlanDigest)
	record := CampaignRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: campaignID,
			CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
			CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
			CorrelationID: state.TransactionID,
		},
		Title: humanCampaignTitle(masterBody, campaign), Slug: campaign,
		Objective: objective, Scope: []string{"legacy campaign state imported by " + state.TransactionID},
		Exclusions:      []string{"migration does not ratify findings or project truth"},
		SuccessCriteria: success, ClosureCriteria: closure, Status: "paused",
		CurrentFocus: []string{"W-0001"}, Owner: state.Actor,
		PermittedManagers: []string{state.Actor}, OpenedAt: state.CreatedAt,
		PausedAt: state.UpdatedAt, LastEventID: eventID,
	}
	normalizeRecordLists(&record)
	if err := setRecordDigest(&record.RecordMeta, record); err != nil {
		return err
	}
	if err := ValidateCampaign(record); err != nil {
		return fmt.Errorf("stage campaign %s: %w", campaign, err)
	}
	if err := AtomicWriteJSON(filepath.Join(campaignDir, "campaign.json"), record, 0o600); err != nil {
		return err
	}
	work := WorkItemRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "W-0001",
			CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
			CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
			CorrelationID: state.TransactionID,
		},
		CampaignID: campaignID, Kind: "verification",
		Title:   "Reconcile imported legacy campaign frontier",
		Problem: "Legacy prose and run reports require structured manager reconciliation before work resumes.",
		State:   "blocked", Priority: "high", Acceptance: []string{
			"Every imported report has curation coverage or an explicit deferred normalization contract.",
			"Every retained file has a registered role and retention outcome.",
		},
		Relations: WorkRelations{}, Owner: state.Actor,
		ResumeNote: "Review the migration coverage and certification blockers before starting new work.",
	}
	if err := setRecordDigest(&work.RecordMeta, work); err != nil {
		return err
	}
	if err := ValidateWorkItem(work); err != nil {
		return err
	}
	if err := AtomicWriteJSON(filepath.Join(campaignDir, "work-items", "W-0001.json"), work, 0o600); err != nil {
		return err
	}
	runIDs, err := engine.stageLegacyRuns(plan, state, campaign, campaignDir, manifest)
	if err != nil {
		return err
	}
	work.ActiveRunIDs = runIDs
	if len(runIDs) > 0 {
		work.Revision++
		work.UpdatedAt = state.UpdatedAt
		work.Digest = ""
		if err := setRecordDigest(&work.RecordMeta, work); err != nil {
			return err
		}
		if err := AtomicWriteJSON(filepath.Join(campaignDir, "work-items", "W-0001.json"), work, 0o600); err != nil {
			return err
		}
	}
	event := StateEvent{
		SchemaVersion: CampaignSchemaVersion, ID: eventID,
		Timestamp: state.UpdatedAt, Actor: state.Actor, Authority: "manager",
		Action: "migration.import", AffectedIDs: append([]string{campaignID, "W-0001"}, runIDs...),
		PreviousRevision: 0, ResultingRevision: 1,
		IdempotencyKey: "migration:" + plan.PlanDigest + ":" + campaign,
		CorrelationID:  state.TransactionID,
		Rationale:      "Imported from the digest-pinned 0.7 source inventory; no epistemic ratification occurred.",
	}
	event.PreviousStateDigest = initialStateHead().StateDigest
	event.MutationDigest, err = CanonicalDigest(struct {
		CampaignDigest string   `json:"campaignDigest"`
		WorkDigest     string   `json:"workDigest"`
		RunIDs         []string `json:"runIds"`
		PlanDigest     string   `json:"planDigest"`
	}{record.Digest, work.Digest, append([]string(nil), runIDs...), plan.PlanDigest})
	if err != nil {
		return err
	}
	event.ResultingStateDigest, err = CanonicalDigest(struct {
		Previous string `json:"previous"`
		Mutation string `json:"mutation"`
	}{event.PreviousStateDigest, event.MutationDigest})
	if err != nil {
		return err
	}
	if err := setEventDigest(&event); err != nil {
		return err
	}
	eventBody, err := json.Marshal(event)
	if err != nil {
		return err
	}
	eventBody = append(eventBody, '\n')
	if err := AtomicWrite(filepath.Join(campaignDir, "events", "events.jsonl"), eventBody, 0o600); err != nil {
		return err
	}
	stateView := renderMigratedStateMarkdown(record, work, runIDs, masterSource)
	if err := AtomicWrite(filepath.Join(campaignDir, "STATE.md"), []byte(stateView), 0o600); err != nil {
		return err
	}
	return nil
}

func setRecordDigest(meta *RecordMeta, value any) error {
	meta.Digest = ""
	digest, err := CanonicalDigest(value)
	if err != nil {
		return err
	}
	meta.Digest = digest
	return nil
}

func setEventDigest(event *StateEvent) error {
	return sealStateEvent(event)
}

func migrationEventID(campaign, planDigest string) string {
	digest := strings.ToUpper(strings.TrimPrefix(SHA256String(campaign+"\x00"+planDigest), "sha256:"))
	return "E-19700101-000000-" + digest[:12]
}

func (engine *MigrationEngine) legacyCampaignMaster(
	plan MigrationPlan, campaign string,
) (string, []byte, error) {
	for _, source := range plan.Sources {
		if source.Campaign == campaign && source.Role == "legacy-campaign-masterfile" {
			body, err := readMigrationSource(engine.ProjectRoot, source)
			return source.Path, body, err
		}
	}
	return "", []byte("# Campaign: " + campaign + "\n"), nil
}

func readMigrationSource(root string, source MigrationSource) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(source.Path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("migration source %s is no longer a regular file", source.Path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if SHA256Bytes(body) != source.SHA256 {
		return nil, fmt.Errorf("migration source %s changed after preview", source.Path)
	}
	return body, nil
}

func (engine *MigrationEngine) stageLegacyRuns(
	plan MigrationPlan,
	state MigrationState,
	campaign string,
	campaignDir string,
	manifest *MigrationNormalizedManifest,
) ([]string, error) {
	grouped := map[string][]MigrationSource{}
	for _, source := range plan.Sources {
		if source.Campaign != campaign {
			continue
		}
		if source.Role == "legacy-run-file" || source.Role == "legacy-run-report" {
			parts := strings.Split(source.Path, "/")
			if len(parts) > 3 {
				grouped[parts[3]] = append(grouped[parts[3]], source)
			}
		}
	}
	// Every campaign receives one import run which owns the masterfile, ledger,
	// and category-folder provenance not attributable to a delegated workspace.
	grouped["campaign-import"] = append(grouped["campaign-import"], campaignPayloadSources(plan, campaign)...)
	workspaces := make([]string, 0, len(grouped))
	for workspace := range grouped {
		workspaces = append(workspaces, workspace)
	}
	sort.Strings(workspaces)
	runIDs := []string{}
	for _, workspace := range workspaces {
		sources := grouped[workspace]
		if len(sources) == 0 {
			continue
		}
		runID := legacyRunID(campaign, workspace)
		runIDs = append(runIDs, runID)
		runDir := filepath.Join(campaignDir, "runs", runID)
		if err := os.MkdirAll(filepath.Join(runDir, "payload", "legacy"), 0o700); err != nil {
			return nil, err
		}
		run := RunRecord{
			RecordMeta: RecordMeta{
				SchemaVersion: CampaignSchemaVersion, ID: runID,
				CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Revision: 1,
				CreatedBy: "migration:" + state.Actor, UpdatedBy: "migration:" + state.Actor,
				CorrelationID: state.TransactionID,
			},
			CampaignID: "C-" + strings.ToUpper(campaign), PrimaryWorkItemID: "W-0001",
			ActorID: "legacy:" + workspace, Role: "manager", Status: "aborted",
			StartedAt: state.CreatedAt, TerminalAt: state.UpdatedAt,
			ResultSummary: "Imported legacy provenance; manager review is required before this run establishes knowledge.",
		}
		for _, source := range sources {
			body, err := readMigrationSource(engine.ProjectRoot, source)
			if err != nil {
				return nil, err
			}
			relative, reserved := migratedRunRelative(source, campaign, workspace)
			destination := filepath.Join(runDir, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return nil, err
			}
			if err := AtomicWrite(destination, body, 0o600); err != nil {
				return nil, err
			}
			manifest.LegacySources[source.Path] = filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, relative))
			canonicalPath := filepath.ToSlash(filepath.Join("active", campaign, "runs", runID, relative))
			handle := &FileHandle{Path: canonicalPath, SHA256: "sha256:" + source.SHA256}
			switch reserved {
			case "brief":
				run.Brief = handle
			case "context":
				run.ContextPack = handle
			case "report":
				run.Report = handle
				run.Status = "returned"
				run.ReturnedAt = state.UpdatedAt
			default:
				run.Files = append(run.Files, RunFile{
					Path: relative, MediaKind: mediaKindFor(source.Path),
					SemanticRole: "reference-copy", Retention: "distill-then-review",
					SHA256: "sha256:" + source.SHA256,
				})
			}
		}
		if run.Status == "returned" && run.Report == nil {
			return nil, errors.New("internal migration error: returned legacy run lost its report")
		}
		if err := setRecordDigest(&run.RecordMeta, run); err != nil {
			return nil, err
		}
		if err := ValidateRun(run); err != nil {
			return nil, fmt.Errorf("stage run %s: %w", runID, err)
		}
		if err := AtomicWriteJSON(filepath.Join(runDir, "run.json"), run, 0o600); err != nil {
			return nil, err
		}
	}
	return runIDs, nil
}

func campaignPayloadSources(plan MigrationPlan, campaign string) []MigrationSource {
	out := []MigrationSource{}
	for _, source := range plan.Sources {
		if source.Campaign != campaign {
			continue
		}
		if source.Role == "legacy-campaign-masterfile" || source.Role == "legacy-review-ledger" ||
			source.Role == "legacy-campaign-payload" {
			out = append(out, source)
		}
	}
	return out
}

func migratedRunRelative(source MigrationSource, campaign, workspace string) (string, string) {
	parts := strings.Split(source.Path, "/")
	if workspace != "campaign-import" && len(parts) > 4 {
		relative := strings.Join(parts[4:], "/")
		switch relative {
		case "brief.md":
			return relative, "brief"
		case "context-pack.json":
			return relative, "context"
		case "report.md":
			return relative, "report"
		default:
			return "payload/legacy/" + relative, ""
		}
	}
	relative := strings.TrimPrefix(source.Path, "active/"+campaign+"/")
	return "payload/legacy/" + relative, ""
}

func mediaKindFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".ps1", ".sh", ".js", ".ts", ".c", ".cpp", ".h":
		return "source-code"
	case ".json", ".jsonl", ".yaml", ".yml", ".csv", ".tsv", ".toml":
		return "structured-data"
	case ".md", ".txt", ".log":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".zip", ".tar", ".gz", ".7z":
		return "archive"
	default:
		return "binary"
	}
}

func markdownSection(body []byte, heading string) string {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	start := -1
	for index, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "## "+heading) {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	out := []string{}
	for _, line := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func markdownListSection(body []byte, headings ...string) []string {
	for _, heading := range headings {
		section := markdownSection(body, heading)
		if section == "" {
			continue
		}
		items := []string{}
		for _, line := range strings.Split(section, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				items = append(items, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			}
		}
		if len(items) > 0 {
			return SortedUnique(items)
		}
	}
	return nil
}

func humanCampaignTitle(body []byte, slug string) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "# Campaign:") {
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# Campaign:"))
			if value != "" {
				return value
			}
		}
	}
	return strings.ReplaceAll(slug, "-", " ")
}

func renderMigratedStateMarkdown(campaign CampaignRecord, work WorkItemRecord, runIDs []string, source string) string {
	var builder strings.Builder
	builder.WriteString("# State: " + campaign.Title + "\n\n")
	builder.WriteString("> Generated from canonical records. Do not edit this file directly.\n\n")
	builder.WriteString("## Objective\n\n" + campaign.Objective + "\n\n")
	builder.WriteString("## Current focus\n\n- `" + work.ID + "` — " + work.Title + " (`" + work.State + "`)\n\n")
	builder.WriteString("## Pending returned runs\n\n")
	if len(runIDs) == 0 {
		builder.WriteString("None.\n\n")
	} else {
		for _, runID := range runIDs {
			builder.WriteString("- `" + runID + "`\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Migration provenance\n\n")
	if source == "" {
		builder.WriteString("No legacy masterfile was present; the campaign requires manager reconciliation.\n")
	} else {
		builder.WriteString("Legacy source `" + source + "` is retained in the campaign import run. No finding or truth was ratified by migration.\n")
	}
	return builder.String()
}

func digestRegularTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("staged migration tree contains a non-regular file")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = SHA256Bytes(body)
		return nil
	})
	return files, err
}

func (engine *MigrationEngine) advancePhysical(
	state MigrationState, plan MigrationPlan, actor, adapter string,
) (MigrationState, error) {
	journalPath := filepath.Join(engine.migrationRoot(), "activation.json")
	journal, err := engine.loadOrPrepareActivation(plan, state, journalPath)
	if err != nil {
		return MigrationState{}, err
	}
	lock, err := acquireWriterLock(engine.lockPath())
	if err != nil {
		return MigrationState{}, err
	}
	defer lock.Close()
	active := filepath.Join(engine.ProjectRoot, "active")
	staged := filepath.Join(engine.migrationRoot(), "staging", "active")
	backup := filepath.Join(engine.migrationRoot(), "legacy-active")
	if journal.Phase == "prepared" {
		if _, err := os.Lstat(backup); err == nil {
			return MigrationState{}, errors.New("migration backup path already exists before activation")
		} else if !os.IsNotExist(err) {
			return MigrationState{}, err
		}
		if _, err := os.Lstat(active); err == nil {
			if err := os.Rename(active, backup); err != nil {
				return MigrationState{}, err
			}
		}
		journal.Phase = "legacy-moved"
		if err := writeActivationJournal(journalPath, &journal); err != nil {
			return MigrationState{}, err
		}
	}
	if journal.Phase == "legacy-moved" {
		if _, err := os.Lstat(staged); err != nil {
			// A crash may have completed the rename before the journal update.
			if _, activeErr := os.Lstat(active); activeErr != nil {
				return MigrationState{}, errors.New("activation lost both staged and canonical active trees")
			}
		} else if err := os.Rename(staged, active); err != nil {
			return MigrationState{}, err
		}
		journal.Phase = "activated"
		if err := writeActivationJournal(journalPath, &journal); err != nil {
			return MigrationState{}, err
		}
	}
	if journal.Phase != "activated" {
		return MigrationState{}, fmt.Errorf("unsupported activation phase %q", journal.Phase)
	}
	activeDigest, err := CanonicalDigest(mustTreeDigest(active))
	if err != nil || activeDigest != journal.StagedDigest {
		return MigrationState{}, errors.New("activated campaign tree digest does not match staged manifest")
	}
	receipt, err := engine.receipt("physically-reorganized", journal.StagedDigest, activeDigest,
		actor, adapter, []string{filepath.ToSlash(strings.TrimPrefix(backup, engine.ProjectRoot+string(filepath.Separator)))})
	if err != nil {
		return MigrationState{}, err
	}
	state.State = "physically-reorganized"
	state.Actor = actor
	state.Adapter = adapter
	state.UpdatedAt = RFC3339UTC(engine.Now().UTC())
	state.LastOperationID = receipt.OperationID
	state.Completed = append(state.Completed, receipt)
	state.Blockers = []string{}
	state.SafeNextAction = "run read-only verification and record structural, traversal, retrieval, and host-parity gate receipts"
	if err := engine.writeState(&state); err != nil {
		return MigrationState{}, err
	}
	return state, nil
}

func (engine *MigrationEngine) loadOrPrepareActivation(
	plan MigrationPlan, state MigrationState, path string,
) (migrationActivationJournal, error) {
	var journal migrationActivationJournal
	if body, err := os.ReadFile(path); err == nil {
		if err := decodeStrict(body, &journal); err != nil {
			return journal, err
		}
		expected := journal.Digest
		journal.Digest = ""
		digest, err := CanonicalDigest(journal)
		journal.Digest = expected
		if err != nil || digest != expected || journal.TransactionID != state.TransactionID || journal.PlanDigest != plan.PlanDigest {
			return migrationActivationJournal{}, errors.New("activation journal identity or digest mismatch")
		}
		return journal, nil
	}
	active := filepath.Join(engine.migrationRoot(), "staging", "active")
	tree := mustTreeDigest(active)
	digest, err := CanonicalDigest(tree)
	if err != nil {
		return journal, err
	}
	journal = migrationActivationJournal{
		SchemaVersion: MigrationSchemaVersion, TransactionID: state.TransactionID,
		PlanDigest: plan.PlanDigest, Phase: "prepared", StagedDigest: digest,
		BackupPath: ".re-discipline/migration/0.8/legacy-active",
	}
	if err := writeActivationJournal(path, &journal); err != nil {
		return migrationActivationJournal{}, err
	}
	return journal, nil
}

func writeActivationJournal(path string, journal *migrationActivationJournal) error {
	journal.Digest = ""
	digest, err := CanonicalDigest(*journal)
	if err != nil {
		return err
	}
	journal.Digest = digest
	return AtomicWriteJSON(path, *journal, 0o600)
}

func mustTreeDigest(root string) map[string]string {
	files, err := digestRegularTree(root)
	if err != nil {
		return map[string]string{"!error": err.Error()}
	}
	return files
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".migration-copy-*")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(tempPath, destination)
}
