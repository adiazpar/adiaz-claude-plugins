package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const closureStagingSchemaVersion = 1

// closureStagingManifest is a private, derived preparation receipt. It is
// intentionally stored below .re-discipline/cache: source discovery excludes
// that tree, the state inventory does not treat it as canonical, and deleting
// it cannot delete project truth or campaign state. Manifests and objects are
// immutable and content addressed so an interrupted state transaction cannot
// move an older job revision onto newer staged bytes.
type closureStagingManifest struct {
	SchemaVersion         int               `json:"schemaVersion"`
	CampaignID            string            `json:"campaignId"`
	CampaignSlug          string            `json:"campaignSlug"`
	ClosureJobID          string            `json:"closureJobId"`
	ArchiveDestination    string            `json:"archiveDestination"`
	SourceFindings        map[string]string `json:"sourceFindings"`
	FindingDestinations   map[string]string `json:"findingDestinations"`
	PromotedFindings      map[string]string `json:"promotedFindings"`
	TruthDigests          map[string]string `json:"truthDigests"`
	ProjectionDigests     map[string]string `json:"projectionDigests"`
	ProjectionObjects     map[string]string `json:"projectionObjects"`
	MaintainedProjections map[string]string `json:"maintainedProjections"`
	ArchiveDigest         string            `json:"archiveDigest,omitempty"`
	ArchiveManifestObject string            `json:"archiveManifestObject,omitempty"`
	ArchiveFiles          map[string]string `json:"archiveFiles,omitempty"`
	Digest                string            `json:"digest"`
}

func newClosureStagingManifest(graph CampaignGraph) closureStagingManifest {
	job := graph.ClosureJob
	return closureStagingManifest{
		SchemaVersion: closureStagingSchemaVersion,
		CampaignID:    graph.Campaign.ID, CampaignSlug: graph.Campaign.Slug,
		ClosureJobID: job.ID, ArchiveDestination: job.ArchiveDestination,
		SourceFindings: map[string]string{}, FindingDestinations: map[string]string{},
		PromotedFindings: map[string]string{},
		TruthDigests:     map[string]string{}, ProjectionDigests: map[string]string{},
		ProjectionObjects: map[string]string{}, MaintainedProjections: map[string]string{},
		ArchiveFiles: map[string]string{},
	}
}

func sealClosureStagingManifest(manifest *closureStagingManifest) error {
	if manifest == nil {
		return errors.New("closure staging manifest is required")
	}
	manifest.Digest = ""
	digest, err := CanonicalDigest(*manifest)
	if err != nil {
		return err
	}
	manifest.Digest = digest
	return validateClosureStagingManifest(*manifest)
}

func validateClosureStagingManifest(manifest closureStagingManifest) error {
	if manifest.SchemaVersion != closureStagingSchemaVersion ||
		!campaignIDRE.MatchString(manifest.CampaignID) ||
		!managedSlugRE.MatchString(manifest.CampaignSlug) ||
		!correlationIDRE.MatchString(manifest.ClosureJobID) ||
		validateArchiveDestination(manifest.ArchiveDestination) != nil ||
		!digestRE.MatchString(manifest.Digest) {
		return errors.New("closure staging identity is invalid")
	}
	for id, digest := range manifest.SourceFindings {
		if !findingIDRE.MatchString(id) || !digestRE.MatchString(digest) {
			return errors.New("closure staging source-finding inventory is invalid")
		}
	}
	for id, digest := range manifest.PromotedFindings {
		if !findingIDRE.MatchString(id) || !digestRE.MatchString(digest) ||
			!digestRE.MatchString(manifest.SourceFindings[id]) {
			return errors.New("closure staging promoted-finding inventory is invalid")
		}
	}
	for id, destination := range manifest.FindingDestinations {
		if !findingIDRE.MatchString(id) || !digestRE.MatchString(manifest.SourceFindings[id]) ||
			manifest.ProjectionDigests[destination] == "" {
			return errors.New("closure staging finding destination inventory is invalid")
		}
	}
	for id, digest := range manifest.TruthDigests {
		if !findingIDRE.MatchString(id) || !digestRE.MatchString(digest) {
			return errors.New("closure staging truth inventory is invalid")
		}
	}
	for destination, digest := range manifest.ProjectionDigests {
		if validateRelativeRecordPath(destination) != nil || !digestRE.MatchString(digest) {
			return errors.New("closure staging projection inventory is invalid")
		}
		objectDigest, staged := manifest.ProjectionObjects[destination]
		maintainedDigest, maintained := manifest.MaintainedProjections[destination]
		if staged == maintained || staged && objectDigest != digest || maintained && maintainedDigest != digest {
			return errors.New("closure staging projection has no unique byte source")
		}
	}
	for destination, digest := range manifest.ProjectionObjects {
		if manifest.ProjectionDigests[destination] != digest || !digestRE.MatchString(digest) {
			return errors.New("closure staging projection object is not in the projection inventory")
		}
	}
	for destination, digest := range manifest.MaintainedProjections {
		if manifest.ProjectionDigests[destination] != digest || !digestRE.MatchString(digest) {
			return errors.New("closure staging maintained projection is not in the projection inventory")
		}
	}
	archivePresent := manifest.ArchiveDigest != "" || manifest.ArchiveManifestObject != "" || len(manifest.ArchiveFiles) != 0
	if archivePresent {
		if !digestRE.MatchString(manifest.ArchiveDigest) ||
			!digestRE.MatchString(manifest.ArchiveManifestObject) || len(manifest.ArchiveFiles) == 0 {
			return errors.New("closure staging archive inventory is incomplete")
		}
		for relative, digest := range manifest.ArchiveFiles {
			if validateRelativeRecordPath(relative) != nil || !digestRE.MatchString(digest) {
				return errors.New("closure staging archive file inventory is invalid")
			}
		}
	}
	copy := manifest
	want := copy.Digest
	copy.Digest = ""
	digest, err := CanonicalDigest(copy)
	if err != nil || digest != want {
		return errors.New("closure staging manifest digest does not verify")
	}
	return nil
}

func closureStagingRoot(boundary Boundary, campaignID, jobID string) string {
	key := SHA256String(campaignID + "\x00" + jobID)[:24]
	return filepath.Join(boundary.Root, ".re-discipline", "cache", "closure", key)
}

func ensureClosureStagingDirectory(boundary Boundary, target string) error {
	root := filepath.Clean(boundary.Root)
	target = filepath.Clean(target)
	if !withinRoot(root, target) {
		return errors.New("closure staging directory escapes the project")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("closure staging directory component %s is unsafe", current)
		}
	}
	return nil
}

func closureStageObjectPath(root, digest string) (string, error) {
	if !digestRE.MatchString(digest) {
		return "", errors.New("closure staging object digest is invalid")
	}
	return containedOutputPath(root, path.Join("objects", strings.TrimPrefix(digest, "sha256:")))
}

func writeClosureStageObject(boundary Boundary, root string, body []byte) (string, error) {
	digest := "sha256:" + SHA256Bytes(body)
	if err := ensureClosureStagingDirectory(boundary, filepath.Join(root, "objects")); err != nil {
		return "", err
	}
	objectPath, err := closureStageObjectPath(root, digest)
	if err != nil {
		return "", err
	}
	created, err := AtomicWriteExclusive(objectPath, body, 0o600)
	if err != nil {
		return "", err
	}
	if !created {
		existing, readErr := readSingleLinkRegularFile(objectPath)
		if readErr != nil || "sha256:"+SHA256Bytes(existing) != digest {
			return "", errors.New("existing closure staging object does not verify")
		}
	}
	return digest, nil
}

func readClosureStageObject(root, digest string) ([]byte, error) {
	objectPath, err := closureStageObjectPath(root, digest)
	if err != nil {
		return nil, err
	}
	body, err := readSingleLinkRegularFile(objectPath)
	if err != nil {
		return nil, err
	}
	if "sha256:"+SHA256Bytes(body) != digest {
		return nil, errors.New("closure staging object digest changed")
	}
	return body, nil
}

func writeClosureStagingManifest(
	boundary Boundary,
	manifest closureStagingManifest,
) (closureStagingManifest, error) {
	if err := sealClosureStagingManifest(&manifest); err != nil {
		return closureStagingManifest{}, err
	}
	root := closureStagingRoot(boundary, manifest.CampaignID, manifest.ClosureJobID)
	manifestRoot := filepath.Join(root, "manifests")
	if err := ensureClosureStagingDirectory(boundary, manifestRoot); err != nil {
		return closureStagingManifest{}, err
	}
	body, err := canonicalJSON(manifest)
	if err != nil {
		return closureStagingManifest{}, err
	}
	manifestPath, err := containedOutputPath(
		root, path.Join("manifests", strings.TrimPrefix(manifest.Digest, "sha256:")+".json"))
	if err != nil {
		return closureStagingManifest{}, err
	}
	created, err := AtomicWriteExclusive(manifestPath, body, 0o600)
	if err != nil {
		return closureStagingManifest{}, err
	}
	if !created {
		existing, readErr := readSingleLinkRegularFile(manifestPath)
		if readErr != nil || string(existing) != string(body) {
			return closureStagingManifest{}, errors.New("closure staging manifest digest collision")
		}
	}
	return manifest, nil
}

func loadClosureStagingManifest(boundary Boundary, job ClosureJob) (closureStagingManifest, error) {
	if !digestRE.MatchString(job.StagingDigest) {
		return closureStagingManifest{}, errors.New("closure job has no authenticated private staging manifest")
	}
	root := closureStagingRoot(boundary, job.CampaignID, job.ID)
	if err := validateRealDirectoryChain(boundary.Root, filepath.Join(root, "manifests")); err != nil {
		return closureStagingManifest{}, err
	}
	manifestPath, err := containedOutputPath(
		root, path.Join("manifests", strings.TrimPrefix(job.StagingDigest, "sha256:")+".json"))
	if err != nil {
		return closureStagingManifest{}, err
	}
	body, err := readSingleLinkRegularFile(manifestPath)
	if err != nil {
		return closureStagingManifest{}, err
	}
	var manifest closureStagingManifest
	if err := decodeStrictJSON(body, &manifest); err != nil {
		return closureStagingManifest{}, err
	}
	if err := validateClosureStagingManifest(manifest); err != nil || manifest.Digest != job.StagingDigest ||
		manifest.CampaignID != job.CampaignID || manifest.ClosureJobID != job.ID ||
		manifest.ArchiveDestination != job.ArchiveDestination {
		return closureStagingManifest{}, errors.New("closure staging manifest does not match its canonical job")
	}
	canonical, err := canonicalJSON(manifest)
	if err != nil || string(canonical) != string(body) {
		return closureStagingManifest{}, errors.New("closure staging manifest encoding is not canonical")
	}
	objects := map[string]bool{}
	for _, digest := range manifest.PromotedFindings {
		objects[digest] = true
	}
	for _, digest := range manifest.ProjectionObjects {
		objects[digest] = true
	}
	for _, digest := range manifest.ArchiveFiles {
		objects[digest] = true
	}
	if manifest.ArchiveManifestObject != "" {
		objects[manifest.ArchiveManifestObject] = true
	}
	for digest := range objects {
		if _, err := readClosureStageObject(root, digest); err != nil {
			return closureStagingManifest{}, err
		}
	}
	return manifest, nil
}

func (service *Service) stageClosureProjections(
	sourceGraph CampaignGraph,
	projectedGraph CampaignGraph,
	truthDigests map[string]string,
	projectionDigests map[string]string,
	artifacts []StateArtifactWrite,
	promotions []closureWriteSpec,
	request ClosureApplyRequest,
) (closureStagingManifest, error) {
	manifest := newClosureStagingManifest(sourceGraph)
	root := closureStagingRoot(service.Boundary, manifest.CampaignID, manifest.ClosureJobID)
	if err := ensureClosureStagingDirectory(service.Boundary, root); err != nil {
		return closureStagingManifest{}, err
	}
	ids := make([]string, 0, len(sourceGraph.Findings))
	for id := range sourceGraph.Findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		manifest.SourceFindings[id] = sourceGraph.Findings[id].Digest
	}
	for _, promotion := range promotions {
		document, ok := promotion.value.(FindingDocument)
		if !ok {
			return closureStagingManifest{}, errors.New("closure staging promotion is not a finding document")
		}
		if projectedGraph.Findings[document.Record.ID].Digest != document.Record.Digest {
			return closureStagingManifest{}, errors.New("closure staging promotion disagrees with projected graph")
		}
		body, err := RenderFindingDocument(document)
		if err != nil {
			return closureStagingManifest{}, err
		}
		digest, err := writeClosureStageObject(service.Boundary, root, body)
		if err != nil {
			return closureStagingManifest{}, err
		}
		manifest.PromotedFindings[document.Record.ID] = digest
	}
	manifest.TruthDigests = cloneStringMap(truthDigests)
	manifest.ProjectionDigests = cloneStringMap(projectionDigests)
	for id, finding := range projectedGraph.Findings {
		destination := closureFindingDestination(request, finding)
		if projectionDigests[destination] != "" {
			manifest.FindingDestinations[id] = destination
		}
	}
	for _, artifact := range artifacts {
		if projectionDigests[artifact.Path] != artifact.ContentDigest ||
			"sha256:"+SHA256Bytes(artifact.Body) != artifact.ContentDigest {
			return closureStagingManifest{}, errors.New("closure projection artifact does not match its digest inventory")
		}
		digest, err := writeClosureStageObject(service.Boundary, root, artifact.Body)
		if err != nil {
			return closureStagingManifest{}, err
		}
		manifest.ProjectionObjects[artifact.Path] = digest
	}
	for destination, digest := range projectionDigests {
		if _, staged := manifest.ProjectionObjects[destination]; !staged {
			manifest.MaintainedProjections[destination] = digest
		}
	}
	return writeClosureStagingManifest(service.Boundary, manifest)
}

func (service *Service) stagedClosureGraph(
	graph CampaignGraph,
) (CampaignGraph, closureStagingManifest, error) {
	if graph.ClosureJob == nil || graph.Campaign == nil {
		return CampaignGraph{}, closureStagingManifest{}, errors.New("closure staging requires an active job and campaign")
	}
	manifest, err := loadClosureStagingManifest(service.Boundary, *graph.ClosureJob)
	if err != nil {
		return CampaignGraph{}, closureStagingManifest{}, err
	}
	if manifest.CampaignSlug != graph.Campaign.Slug ||
		!equalStringMap(manifest.TruthDigests, graph.ClosureJob.TruthDigests) ||
		!equalStringMap(manifest.ProjectionDigests, graph.ClosureJob.ProjectionDigests) ||
		len(manifest.SourceFindings) != len(graph.Findings) {
		return CampaignGraph{}, closureStagingManifest{}, errors.New("closure staging inventory does not match canonical closure state")
	}
	projected := cloneCampaignGraph(graph)
	root := closureStagingRoot(service.Boundary, manifest.CampaignID, manifest.ClosureJobID)
	for id, sourceDigest := range manifest.SourceFindings {
		prior, exists := graph.Findings[id]
		if !exists || prior.Digest != sourceDigest {
			return CampaignGraph{}, closureStagingManifest{}, fmt.Errorf("closure source finding %s changed after projection", id)
		}
		objectDigest, promoted := manifest.PromotedFindings[id]
		if !promoted {
			continue
		}
		body, err := readClosureStageObject(root, objectDigest)
		if err != nil {
			return CampaignGraph{}, closureStagingManifest{}, err
		}
		documentPath := path.Join("active", graph.Campaign.Slug, "findings", id+".md")
		document, err := ParseFindingDocument(body, documentPath)
		if err != nil {
			return CampaignGraph{}, closureStagingManifest{}, fmt.Errorf("staged finding %s: %w", id, err)
		}
		if err := ValidateFindingTransition(&prior, document.Record, "closure.finalize", "manager"); err != nil {
			return CampaignGraph{}, closureStagingManifest{}, fmt.Errorf("staged finding %s transition: %w", id, err)
		}
		projected.Findings[id] = document.Record
	}
	for _, id := range graph.ClosurePlan.ProjectionFindingIDs {
		finding, exists := projected.Findings[id]
		if !exists || finding.Validity != "current" {
			return CampaignGraph{}, closureStagingManifest{}, fmt.Errorf("truth projection finding %s has no current staged state", id)
		}
		if graph.Findings[id].Validity != "current" {
			if _, promoted := manifest.PromotedFindings[id]; !promoted {
				return CampaignGraph{}, closureStagingManifest{}, fmt.Errorf("truth projection finding %s lost its required staged promotion", id)
			}
		}
	}
	return projected, manifest, nil
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func discardClosureStaging(boundary Boundary, campaignID, jobID string) error {
	target := closureStagingRoot(boundary, campaignID, jobID)
	base := filepath.Join(boundary.Root, ".re-discipline", "cache", "closure")
	if !withinRoot(base, target) || filepath.Clean(target) == filepath.Clean(base) {
		return errors.New("closure staging cleanup target is unsafe")
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("closure staging cleanup target is not a real directory")
	}
	if err := validateRealDirectoryChain(boundary.Root, target); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return syncTransactionDirectory(base)
}
