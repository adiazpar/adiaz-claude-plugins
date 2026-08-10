package knowledge

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Closure stages are deliberately explicit because closure may span many
// process lifetimes. A caller can only advance one edge at a time and every
// edge revalidates the frozen campaign revision and the stage-specific gate.
var ClosureStages = []string{
	"inventory", "coverage", "normalize", "reconcile", "decide",
	"project", "verify", "archive", "finalize",
}

var closureStageIndex = func() map[string]int {
	result := make(map[string]int, len(ClosureStages))
	for index, stage := range ClosureStages {
		result[stage] = index
	}
	return result
}()

type ClosurePlan struct {
	SchemaVersion          int      `json:"schemaVersion"`
	CampaignID             string   `json:"campaignId"`
	CampaignRevision       int64    `json:"campaignRevision"`
	ArchiveDestination     string   `json:"archiveDestination"`
	ProjectionFindingIDs   []string `json:"projectionFindingIds"`
	RequiredRunIDs         []string `json:"requiredRunIds"`
	RequiredWorkItemIDs    []string `json:"requiredWorkItemIds"`
	RequiredFindingIDs     []string `json:"requiredFindingIds"`
	RequiredRetentionPaths []string `json:"requiredRetentionPaths"`
	Digest                 string   `json:"digest"`
}

type ClosureReceipt struct {
	SchemaVersion      int               `json:"schemaVersion"`
	CampaignID         string            `json:"campaignId"`
	ClosureJobID       string            `json:"closureJobId"`
	CampaignRevision   int64             `json:"campaignRevision"`
	StateHeadRevision  int64             `json:"stateHeadRevision"`
	EventID            string            `json:"eventId"`
	ArchiveDestination string            `json:"archiveDestination"`
	ArchiveDigest      string            `json:"archiveDigest"`
	TruthDigests       map[string]string `json:"truthDigests"`
	CoverageDigest     string            `json:"coverageDigest"`
	ClosedAt           string            `json:"closedAt"`
	Digest             string            `json:"digest"`
}

// BuildClosurePlan freezes the complete set of records that closure must
// account for. It does not write anything and is safe to use for a preview.
func BuildClosurePlan(graph CampaignGraph, archiveDestination string) (ClosurePlan, error) {
	if err := graph.Validate(); err != nil {
		return ClosurePlan{}, err
	}
	if err := validateArchiveDestination(archiveDestination); err != nil {
		return ClosurePlan{}, err
	}
	plan := ClosurePlan{
		SchemaVersion:        CampaignSchemaVersion,
		CampaignID:           graph.Campaign.ID,
		CampaignRevision:     graph.Campaign.Revision,
		ArchiveDestination:   archiveDestination,
		ProjectionFindingIDs: []string{}, RequiredRunIDs: []string{},
		RequiredWorkItemIDs: []string{}, RequiredFindingIDs: []string{},
		RequiredRetentionPaths: []string{},
	}
	for id := range graph.WorkItems {
		plan.RequiredWorkItemIDs = append(plan.RequiredWorkItemIDs, id)
	}
	for id, run := range graph.Runs {
		plan.RequiredRunIDs = append(plan.RequiredRunIDs, id)
		for _, file := range run.Files {
			plan.RequiredRetentionPaths = append(plan.RequiredRetentionPaths,
				id+":"+file.Path)
		}
	}
	for id, finding := range graph.Findings {
		plan.RequiredFindingIDs = append(plan.RequiredFindingIDs, id)
		if finding.Projection == "truth" {
			plan.ProjectionFindingIDs = append(plan.ProjectionFindingIDs, id)
		}
	}
	plan.ProjectionFindingIDs = SortedUnique(plan.ProjectionFindingIDs)
	plan.RequiredRunIDs = SortedUnique(plan.RequiredRunIDs)
	plan.RequiredWorkItemIDs = SortedUnique(plan.RequiredWorkItemIDs)
	plan.RequiredFindingIDs = SortedUnique(plan.RequiredFindingIDs)
	plan.RequiredRetentionPaths = SortedUnique(plan.RequiredRetentionPaths)
	if err := sealClosurePlan(&plan); err != nil {
		return ClosurePlan{}, err
	}
	return plan, nil
}

func sealClosurePlan(plan *ClosurePlan) error {
	if plan == nil {
		return errors.New("closure plan is required")
	}
	plan.Digest = ""
	digest, err := CanonicalDigest(*plan)
	if err != nil {
		return err
	}
	plan.Digest = digest
	return ValidateClosurePlan(*plan)
}

func ValidateClosurePlan(plan ClosurePlan) error {
	if plan.SchemaVersion != CampaignSchemaVersion || !campaignIDRE.MatchString(plan.CampaignID) ||
		plan.CampaignRevision < 1 || !digestRE.MatchString(plan.Digest) {
		return errors.New("closure plan identity, revision, or digest is invalid")
	}
	if err := validateArchiveDestination(plan.ArchiveDestination); err != nil {
		return err
	}
	if err := validateIDList("closure projection findings", plan.ProjectionFindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("closure required runs", plan.RequiredRunIDs, runIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("closure required work items", plan.RequiredWorkItemIDs, workItemIDRE, ""); err != nil {
		return err
	}
	if err := validateIDList("closure required findings", plan.RequiredFindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("closure retention paths", plan.RequiredRetentionPaths); err != nil {
		return err
	}
	for _, retention := range plan.RequiredRetentionPaths {
		runID, filePath, present := strings.Cut(retention, ":")
		if !present || !runIDRE.MatchString(runID) || validateRelativeRecordPath(filePath) != nil ||
			!strings.HasPrefix(filePath, "payload/") {
			return fmt.Errorf("closure retention path %q is invalid", retention)
		}
	}
	copy := plan
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || digest != want {
		return errors.New("closure plan digest does not verify")
	}
	return nil
}

// ComputeClosureCoverage deterministically classifies every canonical record.
// Prior coverage is accepted only for the two manager-owned dispositions that
// cannot be inferred from record state: exported work and explicit file
// retention. All other values are recomputed so stale coverage cannot pass.
func ComputeClosureCoverage(graph CampaignGraph, prior *ClosureCoverage) (ClosureCoverage, error) {
	if err := graph.Validate(); err != nil {
		return ClosureCoverage{}, err
	}
	coverage := ClosureCoverage{
		SchemaVersion: CampaignSchemaVersion, CampaignID: graph.Campaign.ID,
		SourceRunCoverage: map[string]string{}, FindingCoverage: map[string]string{},
		WorkItemCoverage: map[string]string{}, FileRetention: map[string]string{},
		ActiveFileDispositions: map[string]string{},
		UnresolvedConflicts:    []string{}, MissingDecisions: []string{},
	}
	if prior != nil {
		coverage.ActiveFileDispositions = cloneStringMap(prior.ActiveFileDispositions)
	}
	reviewedReports := reviewedReportCoverage(graph)
	for id, run := range graph.Runs {
		switch {
		case run.Status == "aborted":
			coverage.SourceRunCoverage[id] = "aborted-with-reason"
		case run.Status == "invalidated" && run.Report == nil:
			// A run voided before it ever returned. `run.complete` accepts
			// `invalidated` from `prepared` and `running` because a manager
			// must be able to take a run that will never come back out of
			// flight, and ValidateRun requires a report only from `returned`,
			// `completed`, and `blocked` - so this is the one terminal state
			// that can carry no report at all besides `aborted`.
			//
			// Demanding the report here made the campaign unclosable:
			// `invalidated` is absorbing, terminal run records are immutable
			// (ValidateRunTransition), and the stranded-run curation admission
			// cannot help because an intake must bind a source to one
			// canonical run report by path and digest.
			//
			// The exemption costs nothing the gate was protecting. It exists so
			// that no reviewed claim rests on report bytes a curator never
			// normalized, and this run froze no bytes: with no report there is
			// no coverage span, so validateCurationGraphBindings can never
			// resolve a source to it and can never bind a candidate finding's
			// sourceRuns to it. That is exactly why `aborted` is exempt too,
			// and exactly what an invalidated run that kept its frozen report
			// does not qualify for. The run's payload files are unaffected:
			// FileRetention below still classifies every one of them.
			coverage.SourceRunCoverage[id] = "invalidated-without-report"
		case run.Report == nil:
			// Whatever is left is a run still in flight - `prepared` or
			// `running`. Closure must not step over it, and the remedy is
			// available: return the run, or end it with `run.complete` as
			// `aborted` (with a reason) or `invalidated` (naming the run that
			// supersedes it).
			coverage.SourceRunCoverage[id] = "missing-report"
			coverage.MissingDecisions = append(coverage.MissingDecisions, "run:"+id+":report")
		case !runIsClaimSource(run):
			// A curator report records how the record itself was triaged, not
			// what is true of the system under study, so it is not a claim
			// source. See runIsClaimSource for the single definition.
			//
			// Triaging curator reports as claim sources made closure recursive and
			// therefore non-convergent. Closure queues normalization for an
			// uncovered report; clearing that queue item requires a curator run;
			// that curator run returns its own report, which is uncovered, which
			// queues again. Every lap adds a report, so the campaign can never
			// close, and the only escape was to hand-edit canonical state -
			// which costs far more integrity than the gate was ever buying.
			//
			// The exemption costs nothing the gate protects. The gate exists so
			// that no ratified claim rests on report bytes a curator never
			// normalized, and a curator report is where normalization is
			// recorded, not where a claim enters. Binding stays intact if a
			// manager does choose to curate one: an intake that names such a
			// report still has to cover it span by span, and
			// validateCurationGraphBindings still resolves every candidate's
			// evidence through intake.sourceRuns. The run's payload files are
			// unaffected - FileRetention below still classifies every one.
			coverage.SourceRunCoverage[id] = "not-a-claim-source"
		case reviewedReports[reviewedRunCoverageKey(id, *run.Report)]:
			coverage.SourceRunCoverage[id] = "reviewed-intake"
		default:
			coverage.SourceRunCoverage[id] = "missing-reviewed-intake"
			coverage.MissingDecisions = append(coverage.MissingDecisions, "run:"+id+":coverage")
		}
		for _, file := range run.Files {
			key := id + ":" + file.Path
			value := inferRetentionDisposition(file)
			if prior != nil && validExplicitRetention(prior.FileRetention[key]) {
				value = prior.FileRetention[key]
			}
			coverage.FileRetention[key] = value
			if !retentionComplete(value) {
				coverage.MissingDecisions = append(coverage.MissingDecisions, "file:"+key)
			}
		}
	}

	for id, item := range graph.WorkItems {
		value := "nonterminal"
		switch item.State {
		case "done", "cancelled", "superseded":
			value = item.State
		case "deferred":
			if item.Deferment != nil && !item.Deferment.BlocksClosure &&
				item.Deferment.ClosureDisposition == DefermentDispositionExportBacklog &&
				validateDefermentBacklogDestination(item.Deferment.ClosureDestination) == nil && prior != nil &&
				prior.WorkItemCoverage[id] == "exported-backlog" {
				value = "exported-backlog"
			}
		}
		coverage.WorkItemCoverage[id] = value
		if !workCoverageComplete(value) {
			coverage.MissingDecisions = append(coverage.MissingDecisions, "work:"+id)
		}
	}

	for id, finding := range graph.Findings {
		value := findingClosureDisposition(finding)
		coverage.FindingCoverage[id] = value
		if !findingCoverageComplete(value) {
			coverage.MissingDecisions = append(coverage.MissingDecisions, "finding:"+id)
		}
		if finding.Validity == "challenged" {
			coverage.UnresolvedConflicts = append(coverage.UnresolvedConflicts, "finding:"+id+":challenged")
		}
		for _, other := range finding.Relations.Contradicts {
			pair := []string{id, other}
			sort.Strings(pair)
			if conflictingFindingsRemainLive(graph, pair[0], pair[1]) {
				coverage.UnresolvedConflicts = append(coverage.UnresolvedConflicts,
					"findings:"+pair[0]+":"+pair[1])
			}
		}
	}
	for dependentID, dependencyIDs := range closureStaleFindingDependents(graph.Findings) {
		for _, dependencyID := range dependencyIDs {
			coverage.MissingDecisions = append(coverage.MissingDecisions,
				"finding:"+dependentID+":stale-dependency:"+dependencyID)
		}
	}
	for _, intake := range graph.Intakes {
		for _, conflict := range intake.Conflicts {
			coverage.UnresolvedConflicts = append(coverage.UnresolvedConflicts,
				"intake:"+intake.ID+":"+conflict)
		}
	}
	for _, review := range graph.Reviews {
		for _, conflict := range review.UnresolvedConflicts {
			coverage.UnresolvedConflicts = append(coverage.UnresolvedConflicts,
				"review:"+review.ID+":"+conflict)
		}
	}
	coverage.UnresolvedConflicts = SortedUnique(coverage.UnresolvedConflicts)
	coverage.MissingDecisions = SortedUnique(coverage.MissingDecisions)
	if err := sealClosureCoverage(&coverage); err != nil {
		return ClosureCoverage{}, err
	}
	return coverage, nil
}

func closureStaleFindingDependents(findings map[string]FindingRecord) map[string][]string {
	result := map[string][]string{}
	for dependentID, dependencyIDs := range StaleFindingDependents(findings) {
		if findingIsEpistemicallyLive(findings[dependentID]) {
			result[dependentID] = append([]string(nil), dependencyIDs...)
		}
	}
	return result
}

func reviewedReportCoverage(graph CampaignGraph) map[string]bool {
	result := map[string]bool{}
	for _, intake := range graph.Intakes {
		if intake.Status != "reviewed" || !reviewBindsIntake(graph, intake) {
			continue
		}
		packet := CurationPacket{Intake: intake}
		for _, findingID := range intake.CandidateFindingIDs {
			finding, ok := graph.Findings[findingID]
			if !ok {
				continue
			}
			packet.Candidates = append(packet.Candidates, FindingDocument{Record: finding})
		}
		bindings, err := validateCurationGraphBindings(graph, packet, false)
		if err != nil {
			continue
		}
		unresolvedSources := map[string]bool{}
		for _, entry := range intake.Coverage {
			if entry.Disposition == "unresolved" {
				unresolvedSources[coverageSourceKey(entry.SourcePath, entry.SourceSHA256)] = true
			}
		}
		for _, source := range intake.SourceRuns {
			sourceKey := coverageSourceKey(source.Path, source.SHA256)
			if unresolvedSources[sourceKey] {
				continue
			}
			runID := bindings[sourceKey]
			if runID != "" {
				result[reviewedRunCoverageKey(runID, source)] = true
			}
		}
	}
	return result
}

func reviewedRunCoverageKey(runID string, report FileHandle) string {
	return runID + "\x00" + report.Path + "\x00" + report.SHA256
}

func inferRetentionDisposition(file RunFile) string {
	switch file.Retention {
	case "retain-inline":
		return "retained-inline"
	case "retain-by-reference":
		return "retained-by-reference"
	case "candidate-maintained":
		if file.Destination != "" {
			return "maintained:" + file.Destination
		}
	}
	return "decision-required"
}

func validExplicitRetention(value string) bool {
	return value == "discarded-approved" || value == "distilled-and-retained" ||
		value == "external-verified" || strings.HasPrefix(value, "maintained:")
}

func retentionComplete(value string) bool {
	return value == "retained-inline" || value == "retained-by-reference" ||
		value == "discarded-approved" || value == "distilled-and-retained" ||
		value == "external-verified" || strings.HasPrefix(value, "maintained:")
}

func workCoverageComplete(value string) bool {
	return value == "done" || value == "cancelled" || value == "superseded" || value == "exported-backlog"
}

func findingClosureDisposition(finding FindingRecord) string {
	if finding.ReviewState == "manager-rejected" {
		return "rejected"
	}
	if finding.ReviewState != "manager-ratified" {
		return "manager-decision-required"
	}
	if finding.Projection == "rejected" || finding.Validity == "invalid" {
		return "rejected"
	}
	if finding.Projection == "none" || finding.Projection == "campaign" {
		return "destination-required"
	}
	if finding.Projection == "truth" && (finding.EvidenceGrade != "direct" || finding.Validity != "current" ||
		len(finding.Relations.Contradicts) != 0) {
		return "truth-gate-failed"
	}
	return finding.Projection
}

func findingCoverageComplete(value string) bool {
	switch value {
	case "truth", "history", "backlog", "playbook", "maintained", "archive", "rejected":
		return true
	default:
		return false
	}
}

func conflictingFindingsRemainLive(graph CampaignGraph, leftID, rightID string) bool {
	left, leftOK := graph.Findings[leftID]
	right, rightOK := graph.Findings[rightID]
	if !leftOK || !rightOK {
		return true
	}
	return findingIsEpistemicallyLive(left) && findingIsEpistemicallyLive(right)
}

func findingIsEpistemicallyLive(record FindingRecord) bool {
	return record.ReviewState != "manager-rejected" &&
		!validOne(record.Validity, "invalid", "superseded", "historical")
}

// runIsClaimSource reports whether a run's report can introduce a claim about
// the system under study. A curator run cannot: it records decisions about the
// campaign record itself - which spans were triaged, and why - so its report is
// a proof artifact rather than a claim source. Every other role can, including
// `manager`, which is why this is not a test for `investigator`.
//
// This is the single definition, and it must stay single. The rule previously
// existed in only one of the two places that need it: the normalization queue
// skipped curator runs, while closure coverage skipped nothing. Closure
// therefore demanded a reviewed intake for exactly the reports the queue would
// never offer a normalization item for, and said only `missing-reviewed-intake`
// while doing it.
func runIsClaimSource(run RunRecord) bool {
	return run.Role != "curator"
}

func sealClosureCoverage(coverage *ClosureCoverage) error {
	if coverage == nil {
		return errors.New("closure coverage is required")
	}
	coverage.Digest = ""
	digest, err := CanonicalDigest(*coverage)
	if err != nil {
		return err
	}
	coverage.Digest = digest
	return ValidateClosureCoverage(*coverage)
}

func ValidateClosureCoverage(coverage ClosureCoverage) error {
	if coverage.SchemaVersion != CampaignSchemaVersion || !campaignIDRE.MatchString(coverage.CampaignID) ||
		!digestRE.MatchString(coverage.Digest) {
		return errors.New("closure coverage identity or digest is invalid")
	}
	if coverage.SourceRunCoverage == nil || coverage.FindingCoverage == nil ||
		coverage.WorkItemCoverage == nil || coverage.FileRetention == nil ||
		coverage.ActiveFileDispositions == nil {
		return errors.New("all closure coverage maps are required")
	}
	for key, value := range coverage.SourceRunCoverage {
		if !runIDRE.MatchString(key) || !validOne(value,
			"reviewed-intake", "aborted-with-reason", "invalidated-without-report",
			"not-a-claim-source", "missing-report", "missing-reviewed-intake") {
			return fmt.Errorf("closure run coverage %q has an invalid disposition", key)
		}
	}
	for key, value := range coverage.FindingCoverage {
		if !findingIDRE.MatchString(key) || !validOne(value,
			"truth", "history", "backlog", "playbook", "maintained", "archive", "rejected",
			"manager-decision-required", "destination-required", "truth-gate-failed") {
			return fmt.Errorf("closure finding coverage %q has an invalid disposition", key)
		}
	}
	for key, value := range coverage.WorkItemCoverage {
		if !workItemIDRE.MatchString(key) || !validOne(value,
			"done", "cancelled", "superseded", "exported-backlog", "nonterminal") {
			return fmt.Errorf("closure work coverage %q has an invalid disposition", key)
		}
	}
	for key, value := range coverage.FileRetention {
		runID, filePath, present := strings.Cut(key, ":")
		if !present || !runIDRE.MatchString(runID) || validateRelativeRecordPath(filePath) != nil ||
			!strings.HasPrefix(filePath, "payload/") ||
			!(validOne(value, "retained-inline", "retained-by-reference", "discarded-approved",
				"distilled-and-retained", "external-verified", "decision-required") ||
				strings.HasPrefix(value, "maintained:")) {
			return fmt.Errorf("closure file retention %q has an invalid disposition", key)
		}
		if strings.HasPrefix(value, "maintained:") &&
			validateRelativeRecordPath(strings.TrimPrefix(value, "maintained:")) != nil {
			return fmt.Errorf("closure file retention %q has an invalid maintained destination", key)
		}
	}
	for key, value := range coverage.ActiveFileDispositions {
		if validateRelativeRecordPath(key) != nil ||
			!validOne(value, "retain", "destroy-approved", "ephemeral", "decision-required") {
			return fmt.Errorf("closure active file %q has an invalid disposition", key)
		}
	}
	if err := requireUniqueNonEmpty("closure unresolved conflicts", coverage.UnresolvedConflicts); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty("closure missing decisions", coverage.MissingDecisions); err != nil {
		return err
	}
	copy := coverage
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || digest != want {
		return errors.New("closure coverage digest does not verify")
	}
	return nil
}

func ValidateClosureJob(job ClosureJob) error {
	if err := validateRecordMeta(job.RecordMeta, correlationIDRE); err != nil {
		return fmt.Errorf("closure job: %w", err)
	}
	if !campaignIDRE.MatchString(job.CampaignID) || job.FrozenCampaignRevision < 1 {
		return errors.New("closure job campaign and frozen revision are required")
	}
	if _, ok := closureStageIndex[job.Stage]; !ok {
		return fmt.Errorf("unsupported closure stage %q", job.Stage)
	}
	if !validOne(job.Status, "running", "blocked", "verified", "completed", "reopened") {
		return fmt.Errorf("unsupported closure status %q", job.Status)
	}
	if err := validateIDList("closure projection findings", job.ProjectionFindingIDs, findingIDRE, ""); err != nil {
		return err
	}
	if job.Coverage != nil {
		if err := ValidateClosureCoverage(*job.Coverage); err != nil {
			return err
		}
		if job.Coverage.CampaignID != job.CampaignID {
			return errors.New("closure job coverage belongs to another campaign")
		}
	}
	if job.ArchiveDestination != "" {
		if err := validateArchiveDestination(job.ArchiveDestination); err != nil {
			return err
		}
	}
	for findingID, digest := range job.TruthDigests {
		if !findingIDRE.MatchString(findingID) || !digestRE.MatchString(digest) {
			return errors.New("closure truth digest map is invalid")
		}
	}
	for destination, digest := range job.ProjectionDigests {
		if validateRelativeRecordPath(destination) != nil || !digestRE.MatchString(digest) {
			return errors.New("closure projection digest map is invalid")
		}
	}
	if job.StagingDigest != "" && !digestRE.MatchString(job.StagingDigest) {
		return errors.New("closure staging digest is invalid")
	}
	if job.ArchiveDigest != "" && !digestRE.MatchString(job.ArchiveDigest) {
		return errors.New("closure archive digest is invalid")
	}
	if err := requireUniqueNonEmpty("closure blockers", job.Blockers); err != nil {
		return err
	}
	return nil
}

// ValidateClosureAdvance enforces a single resumable stage edge and the gates
// that can be proven from canonical records at that edge.
func ValidateClosureAdvance(previous, next ClosureJob) error {
	if err := ValidateClosureJob(previous); err != nil {
		return err
	}
	if err := ValidateClosureJob(next); err != nil {
		return err
	}
	if previous.CampaignID != next.CampaignID || previous.ID != next.ID ||
		previous.FrozenCampaignRevision != next.FrozenCampaignRevision {
		return errors.New("closure identity and frozen campaign revision are immutable")
	}
	from, to := closureStageIndex[previous.Stage], closureStageIndex[next.Stage]
	if next.Status == "reopened" {
		if next.Stage != previous.Stage {
			return errors.New("reopen cannot skip or rewind closure stages")
		}
		return nil
	}
	if to != from+1 {
		return errors.New("closure may advance exactly one stage at a time")
	}
	if previous.Status == "blocked" && len(next.Blockers) != 0 {
		return errors.New("closure blockers must be resolved before advancing")
	}
	if to >= closureStageIndex["normalize"] && next.Coverage == nil {
		return errors.New("closure coverage is required after the coverage stage")
	}
	if to >= closureStageIndex["project"] {
		if next.Coverage == nil || len(next.Coverage.MissingDecisions) != 0 ||
			len(next.Coverage.UnresolvedConflicts) != 0 {
			return errors.New("closure cannot project with missing decisions or unresolved conflicts")
		}
		if next.Stage != "finalize" && !digestRE.MatchString(next.StagingDigest) {
			return errors.New("closure projected state requires an authenticated private staging manifest")
		}
	}
	if to >= closureStageIndex["verify"] {
		if err := validateArchiveDestination(next.ArchiveDestination); err != nil {
			return err
		}
		for _, id := range next.ProjectionFindingIDs {
			if !digestRE.MatchString(next.TruthDigests[id]) {
				return fmt.Errorf("truth projection %s has no verified digest", id)
			}
		}
		if len(next.ProjectionDigests) < len(next.ProjectionFindingIDs) {
			return errors.New("closure verify requires every projection destination and digest")
		}
	}
	if to >= closureStageIndex["archive"] && !digestRE.MatchString(next.ArchiveDigest) {
		return errors.New("closure archive stage requires a verified archive digest")
	}
	if next.Stage == "finalize" && next.Status != "completed" {
		return errors.New("final closure stage must be completed")
	}
	return nil
}

func validateArchiveDestination(value string) error {
	if err := validateRelativeRecordPath(value); err != nil {
		return fmt.Errorf("archive destination: %w", err)
	}
	clean := path.Clean(value)
	parts := strings.Split(clean, "/")
	if len(parts) != 4 || parts[0] != "docs" || parts[1] != "history" ||
		parts[2] != "campaigns" || !managedSlugRE.MatchString(parts[3]) {
		return errors.New("archive destination must be docs/history/campaigns/<archive>")
	}
	return nil
}

func sealClosureReceipt(receipt *ClosureReceipt) error {
	if receipt == nil {
		return errors.New("closure receipt is required")
	}
	receipt.Digest = ""
	digest, err := CanonicalDigest(*receipt)
	if err != nil {
		return err
	}
	receipt.Digest = digest
	return ValidateClosureReceipt(*receipt)
}

func ValidateClosureReceipt(receipt ClosureReceipt) error {
	if receipt.SchemaVersion != CampaignSchemaVersion || !campaignIDRE.MatchString(receipt.CampaignID) ||
		strings.TrimSpace(receipt.ClosureJobID) == "" || receipt.CampaignRevision < 1 ||
		receipt.StateHeadRevision < 1 || !eventIDRE.MatchString(receipt.EventID) ||
		!digestRE.MatchString(receipt.ArchiveDigest) || !digestRE.MatchString(receipt.CoverageDigest) ||
		!digestRE.MatchString(receipt.Digest) {
		return errors.New("closure receipt identity, revision, event, or digest is invalid")
	}
	if err := validateArchiveDestination(receipt.ArchiveDestination); err != nil {
		return err
	}
	if err := validateUTC(receipt.ClosedAt); err != nil {
		return fmt.Errorf("closedAt: %w", err)
	}
	for id, digest := range receipt.TruthDigests {
		if !findingIDRE.MatchString(id) || !digestRE.MatchString(digest) {
			return errors.New("closure receipt truth digest map is invalid")
		}
	}
	copy := receipt
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || digest != want {
		return errors.New("closure receipt digest does not verify")
	}
	return nil
}
