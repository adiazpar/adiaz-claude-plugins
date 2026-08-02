package knowledge

import (
	"bytes"
	"encoding/json"
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
func BuildTruthProjection(boundary Boundary, finding FindingRecord, destination string) (TruthProjection, error) {
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
	for _, evidence := range finding.Evidence {
		absolute, err := boundary.Resolve(evidence.Path, true)
		if err != nil {
			return TruthProjection{}, fmt.Errorf("resolve evidence %s: %w", EvidenceHandle(finding.ID, evidence), err)
		}
		body, err := os.ReadFile(absolute)
		if err != nil {
			return TruthProjection{}, err
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
	semantic := struct {
		SchemaVersion int                 `json:"schemaVersion"`
		FindingID     string              `json:"findingId"`
		CampaignID    string              `json:"campaignId"`
		Subject       string              `json:"subject"`
		Claim         string              `json:"claim"`
		Scope         map[string]any      `json:"scope"`
		AppliesWhen   []string            `json:"appliesWhen,omitempty"`
		KnownLimits   []string            `json:"knownLimits,omitempty"`
		Evidence      []EvidenceReference `json:"evidence"`
		VerifiedAt    string              `json:"verifiedAt,omitempty"`
	}{
		SchemaVersion: CampaignSchemaVersion, FindingID: finding.ID,
		CampaignID: finding.CampaignID, Subject: finding.Subject, Claim: finding.Claim,
		Scope: finding.Scope, AppliesWhen: SortedUnique(finding.AppliesWhen),
		KnownLimits: SortedUnique(finding.KnownLimits), Evidence: finding.Evidence,
		VerifiedAt: finding.VerifiedAt,
	}
	semanticDigest, err := CanonicalDigest(semantic)
	if err != nil {
		return TruthProjection{}, err
	}
	body, err := renderTruthProjection(semantic, semanticDigest)
	if err != nil {
		return TruthProjection{}, err
	}
	return TruthProjection{
		SchemaVersion: CampaignSchemaVersion, FindingID: finding.ID,
		Destination: destination, SemanticDigest: semanticDigest,
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
	if !strings.HasPrefix(value, "docs/truth/") || path.Ext(value) != ".md" {
		return errors.New("truth projection destination must be a Markdown file below docs/truth")
	}
	return nil
}

func renderTruthProjection(value struct {
	SchemaVersion int                 `json:"schemaVersion"`
	FindingID     string              `json:"findingId"`
	CampaignID    string              `json:"campaignId"`
	Subject       string              `json:"subject"`
	Claim         string              `json:"claim"`
	Scope         map[string]any      `json:"scope"`
	AppliesWhen   []string            `json:"appliesWhen,omitempty"`
	KnownLimits   []string            `json:"knownLimits,omitempty"`
	Evidence      []EvidenceReference `json:"evidence"`
	VerifiedAt    string              `json:"verifiedAt,omitempty"`
}, semanticDigest string) ([]byte, error) {
	scope, err := json.Marshal(value.Scope)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("---\n")
	fmt.Fprintf(&output, "schemaVersion: %d\n", value.SchemaVersion)
	fmt.Fprintf(&output, "truthId: %s\n", yamlScalar("T-"+value.FindingID))
	fmt.Fprintf(&output, "sourceFinding: %s\n", yamlScalar(value.FindingID))
	fmt.Fprintf(&output, "sourceCampaign: %s\n", yamlScalar(value.CampaignID))
	fmt.Fprintf(&output, "subject: %s\n", yamlScalar(value.Subject))
	fmt.Fprintf(&output, "claim: %s\n", yamlScalar(value.Claim))
	fmt.Fprintf(&output, "scope: %s\n", string(scope))
	writeYAMLStrings(&output, "appliesWhen", value.AppliesWhen)
	writeYAMLStrings(&output, "knownLimits", value.KnownLimits)
	output.WriteString("evidence:\n")
	for _, evidence := range value.Evidence {
		fmt.Fprintf(&output, "  - path: %s\n", yamlScalar(evidence.Path))
		fmt.Fprintf(&output, "    sha256: %s\n", yamlScalar(evidence.SHA256))
		if evidence.StartLine > 0 {
			fmt.Fprintf(&output, "    startLine: %d\n    endLine: %d\n", evidence.StartLine, evidence.EndLine)
		}
		if evidence.ObjectKey != "" {
			fmt.Fprintf(&output, "    objectKey: %s\n", yamlScalar(evidence.ObjectKey))
		}
		if evidence.SourceRun != "" {
			fmt.Fprintf(&output, "    sourceRun: %s\n", yamlScalar(evidence.SourceRun))
		}
	}
	if value.VerifiedAt != "" {
		fmt.Fprintf(&output, "verifiedAt: %s\n", yamlScalar(value.VerifiedAt))
	}
	fmt.Fprintf(&output, "semanticDigest: %s\n", yamlScalar(semanticDigest))
	output.WriteString("status: current\n---\n\n")
	output.WriteString(value.Claim)
	output.WriteString("\n")
	return []byte(output.String()), nil
}

func yamlScalar(value string) string {
	return strconv.Quote(value)
}

func writeYAMLStrings(output *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(output, "%s:\n", key)
	for _, value := range values {
		fmt.Fprintf(output, "  - %s\n", yamlScalar(value))
	}
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
