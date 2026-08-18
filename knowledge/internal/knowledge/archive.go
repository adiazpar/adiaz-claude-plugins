package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

type TruthProjection struct {
	SchemaVersion  int    `json:"schemaVersion"`
	FindingID      string `json:"findingId"`
	Destination    string `json:"destination"`
	SemanticDigest string `json:"semanticDigest"`
	ContentDigest  string `json:"contentDigest"`
	Body           []byte `json:"-"`
}

// BuildTruthProjection is the only renderer used by closure. It proves the
// epistemic gate and every evidence digest before returning bytes; publishing
// those bytes still belongs to the closure transaction.
func BuildTruthProjection(boundary Boundary, document FindingDocument, destination string) (TruthProjection, error) {
	return buildTruthProjection(boundary, document, document.Record.Evidence, destination)
}

// buildTruthProjection lets closure validate evidence at its still-live source
// path while rendering the durable archive path that will exist after the
// final atomic cutover. Ordinary callers use the same references for both.
func buildTruthProjection(
	boundary Boundary,
	document FindingDocument,
	sourceEvidence []EvidenceReference,
	destination string,
) (TruthProjection, error) {
	finding := document.Record
	if err := ValidateFinding(finding); err != nil {
		return TruthProjection{}, err
	}
	if finding.EvidenceGrade != "direct" || finding.ReviewState != "manager-ratified" ||
		finding.Validity != "current" || finding.Projection != "truth" {
		return TruthProjection{}, errors.New("truth projection requires a DIRECT, manager-ratified, current finding approved for truth")
	}
	if len(finding.Relations.Contradicts) != 0 || relationContains(finding.Relations, finding.ID) {
		return TruthProjection{}, errors.New("truth projection refuses unresolved conflict or malformed self-relation")
	}
	if err := validateTruthDestination(destination); err != nil {
		return TruthProjection{}, err
	}
	if len(sourceEvidence) != len(finding.Evidence) {
		return TruthProjection{}, errors.New("truth projection evidence source inventory is incomplete")
	}
	for index, evidence := range sourceEvidence {
		durable := finding.Evidence[index]
		if durable.SHA256 != evidence.SHA256 || durable.StartLine != evidence.StartLine ||
			durable.EndLine != evidence.EndLine ||
			durable.SourceRun != evidence.SourceRun {
			return TruthProjection{}, errors.New("truth projection durable evidence changed its source identity")
		}
		var body []byte
		if migrationGitPinnedEvidence(evidence) {
			resolved, err := resolveMigrationGitEvidence(boundary.Root, evidence)
			if err != nil {
				return TruthProjection{}, fmt.Errorf("resolve archive-pinned evidence %s: %w", EvidenceHandle(finding.ID, evidence), err)
			}
			body = resolved
		} else {
			absolute, err := boundary.Resolve(evidence.Path, true)
			if err != nil {
				return TruthProjection{}, fmt.Errorf("resolve evidence %s: %w", EvidenceHandle(finding.ID, evidence), err)
			}
			body, err = os.ReadFile(absolute)
			if err != nil {
				return TruthProjection{}, err
			}
		}
		got := SHA256Bytes(body)
		want := strings.TrimPrefix(evidence.SHA256, "sha256:")
		if got != want {
			return TruthProjection{}, fmt.Errorf("evidence %s digest changed", EvidenceHandle(finding.ID, evidence))
		}
		if evidence.StartLine > 0 {
			lines := bytes.Count(body, []byte{'\n'}) + 1
			if evidence.EndLine > lines {
				return TruthProjection{}, fmt.Errorf("evidence %s line range no longer resolves", EvidenceHandle(finding.ID, evidence))
			}
		}
	}
	document.Record.Path = destination
	body, err := RenderFindingDocument(document)
	if err != nil {
		return TruthProjection{}, err
	}
	return TruthProjection{
		SchemaVersion: CampaignSchemaVersion, FindingID: finding.ID,
		Destination: destination, SemanticDigest: finding.Digest,
		ContentDigest: "sha256:" + SHA256Bytes(body), Body: body,
	}, nil
}

func relationContains(relations FindingRelations, target string) bool {
	for _, values := range [][]string{
		relations.Supports, relations.Contradicts, relations.DependsOn,
		relations.Supersedes, relations.Duplicates,
	} {
		if containsString(values, target) {
			return true
		}
	}
	return false
}

func validateTruthDestination(value string) error {
	if err := validateRelativeRecordPath(value); err != nil {
		return err
	}
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[0] != "docs" || parts[1] != "truth" ||
		parts[2] != "findings" || !managedSlugRE.MatchString(parts[3]) ||
		!findingIDRE.MatchString(strings.TrimSuffix(parts[4], ".md")) ||
		path.Ext(parts[4]) != ".md" {
		return errors.New("truth projection destination must be docs/truth/findings/<campaign-slug>/<F-id>.md")
	}
	return nil
}

func canonicalTruthDestination(campaignSlug, findingID string) string {
	return path.Join("docs", "truth", "findings", campaignSlug, findingID+".md")
}

func validateCanonicalTruthDestination(campaignSlug, findingID, value string) error {
	expected := canonicalTruthDestination(campaignSlug, findingID)
	if value != expected {
		return fmt.Errorf("truth projection %s must use its canonical provenance path %s", findingID, expected)
	}
	return validateTruthDestination(value)
}

func yamlScalar(value string) string {
	return strconv.Quote(value)
}

// BuildArchiveManifest seals the archive inventory after the caller has
// staged every file and calculated byte digests. Keys are archive-relative;
// no live project path is accepted as a manifest key.
func BuildArchiveManifest(
	graph CampaignGraph,
	job ClosureJob,
	eventHead, closedAt string,
	files, projections map[string]string,
) (ArchiveManifest, error) {
	if err := graph.Validate(); err != nil {
		return ArchiveManifest{}, err
	}
	if err := ValidateClosureJob(job); err != nil {
		return ArchiveManifest{}, err
	}
	if job.CampaignID != graph.Campaign.ID || job.Coverage == nil ||
		len(job.Coverage.MissingDecisions) != 0 || len(job.Coverage.UnresolvedConflicts) != 0 {
		return ArchiveManifest{}, errors.New("archive requires complete, conflict-free closure coverage")
	}
	if !eventIDRE.MatchString(eventHead) {
		return ArchiveManifest{}, errors.New("archive event head is invalid")
	}
	if err := validateUTC(closedAt); err != nil {
		return ArchiveManifest{}, err
	}
	if err := validateArchiveFileDigests(files); err != nil {
		return ArchiveManifest{}, err
	}
	if err := validateProjectionDigests(projections, job); err != nil {
		return ArchiveManifest{}, err
	}
	required := requiredArchiveRecordPaths(graph)
	for _, recordPath := range required {
		if !digestRE.MatchString(files[recordPath]) {
			return ArchiveManifest{}, fmt.Errorf("archive is missing canonical record %s", recordPath)
		}
	}
	sourceInput := struct {
		CampaignDigest string            `json:"campaignDigest"`
		Files          map[string]string `json:"files"`
		CoverageDigest string            `json:"coverageDigest"`
		EventHead      string            `json:"eventHead"`
	}{
		CampaignDigest: graph.Campaign.Digest, Files: cloneStringMap(files),
		CoverageDigest: job.Coverage.Digest, EventHead: eventHead,
	}
	sourceDigest, err := CanonicalDigest(sourceInput)
	if err != nil {
		return ArchiveManifest{}, err
	}
	manifest := ArchiveManifest{
		SchemaVersion: CampaignSchemaVersion, CampaignID: graph.Campaign.ID,
		ClosedAt: closedAt, SourceDigest: sourceDigest,
		Files: cloneStringMap(files), Projections: cloneStringMap(projections),
		Coverage: *job.Coverage, EventHead: eventHead,
	}
	if err := sealArchiveManifest(&manifest); err != nil {
		return ArchiveManifest{}, err
	}
	return manifest, nil
}

func requiredArchiveRecordPaths(graph CampaignGraph) []string {
	paths := []string{
		"campaign.json", "events/events.jsonl", "closure/plan.json",
		"closure/job.json", "closure/coverage.json",
	}
	for id := range graph.WorkItems {
		paths = append(paths, "work-items/"+id+".json")
	}
	for id := range graph.Runs {
		paths = append(paths, "runs/"+id+"/run.json")
	}
	for id := range graph.Findings {
		paths = append(paths, "findings/"+id+".md")
	}
	for id := range graph.Intakes {
		paths = append(paths, "intake/"+id+".json")
	}
	for id := range graph.Reviews {
		paths = append(paths, "reviews/"+id+".json")
	}
	sort.Strings(paths)
	return paths
}

func validateArchiveFileDigests(files map[string]string) error {
	if len(files) == 0 {
		return errors.New("archive file inventory is required")
	}
	for relative, digest := range files {
		if err := validateRelativeRecordPath(relative); err != nil || strings.HasPrefix(relative, "docs/") ||
			!digestRE.MatchString(digest) {
			return fmt.Errorf("archive file inventory entry %q is invalid", relative)
		}
	}
	return nil
}

func validateProjectionDigests(projections map[string]string, job ClosureJob) error {
	if projections == nil && len(job.ProjectionFindingIDs) != 0 {
		return errors.New("archive projection inventory is required")
	}
	for destination, digest := range projections {
		if err := validateRelativeRecordPath(destination); err != nil || !digestRE.MatchString(digest) {
			return errors.New("archive projection inventory is invalid")
		}
	}
	for _, findingID := range job.ProjectionFindingIDs {
		found := false
		for destination, digest := range projections {
			if strings.HasPrefix(destination, "docs/truth/") && digest == job.TruthDigests[findingID] {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("truth projection %s is missing from archive projection inventory", findingID)
		}
	}
	return nil
}

func sealArchiveManifest(manifest *ArchiveManifest) error {
	manifest.Digest = ""
	digest, err := CanonicalDigest(*manifest)
	if err != nil {
		return err
	}
	manifest.Digest = digest
	return ValidateArchiveManifest(*manifest)
}

// ValidateArchiveManifest verifies the immutable archive inventory. It lives
// with the archive planner so both closure and the transactional state writer
// enforce exactly the same byte-level contract.
func ValidateArchiveManifest(manifest ArchiveManifest) error {
	if manifest.SchemaVersion != CampaignSchemaVersion || !campaignIDRE.MatchString(manifest.CampaignID) ||
		!digestRE.MatchString(manifest.SourceDigest) || !eventIDRE.MatchString(manifest.EventHead) ||
		!digestRE.MatchString(manifest.Digest) {
		return errors.New("archive manifest identity or digest is invalid")
	}
	if err := validateUTC(manifest.ClosedAt); err != nil {
		return err
	}
	if err := ValidateClosureCoverage(manifest.Coverage); err != nil {
		return err
	}
	if manifest.Coverage.CampaignID != manifest.CampaignID {
		return errors.New("archive coverage belongs to another campaign")
	}
	if err := validateArchiveFileDigests(manifest.Files); err != nil {
		return err
	}
	for destination, digest := range manifest.Projections {
		if err := validateRelativeRecordPath(destination); err != nil || !digestRE.MatchString(digest) {
			return errors.New("archive manifest projection entry is invalid")
		}
	}
	copy := manifest
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || digest != want {
		return errors.New("archive manifest digest does not verify")
	}
	return nil
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
