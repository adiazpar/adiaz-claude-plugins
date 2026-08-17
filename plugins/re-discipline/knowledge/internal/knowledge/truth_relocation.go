package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type legacyTruthIdentity struct {
	CampaignID string
	FindingID  string
	Subject    string
	Claim      string
}

const maxTruthRelocations = 512

func (service *Service) prepareTruthRelocationArtifacts(
	request ManagerApplyRequest,
) ([]StateArtifactWrite, error) {
	if request.Action != "truth.relocate" {
		return nil, nil
	}
	if service == nil {
		return nil, errors.New("service is required")
	}
	artifacts := make([]StateArtifactWrite, 0, len(request.TruthRelocations))
	seenDestinations := map[string]string{}
	for _, relocation := range request.TruthRelocations {
		absolute, err := service.Boundary.Resolve(relocation.Source, true)
		if err != nil {
			return nil, fmt.Errorf("resolve truth relocation source %s: %w", relocation.Source, err)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("read truth relocation source %s: %w", relocation.Source, err)
		}
		if got := "sha256:" + SHA256Bytes(body); got != relocation.ExpectedDigest {
			return nil, fmt.Errorf(
				"truth relocation source %s has digest %s; expected %s",
				relocation.Source, got, relocation.ExpectedDigest)
		}
		identity, err := parseLegacyTruthIdentity(body)
		if err != nil {
			return nil, fmt.Errorf("parse truth relocation source %s: %w", relocation.Source, err)
		}
		archiveRoot, campaign, err := findArchivedCampaign(
			service.Boundary, identity.CampaignID)
		if err != nil {
			return nil, fmt.Errorf("resolve source campaign for %s: %w", relocation.Source, err)
		}
		findingPath := path.Join(archiveRoot, "findings", identity.FindingID+".md")
		findingAbsolute, err := service.Boundary.Resolve(findingPath, true)
		if err != nil {
			return nil, fmt.Errorf("resolve archived finding %s: %w", identity.FindingID, err)
		}
		findingBody, err := readSingleLinkRegularFile(findingAbsolute)
		if err != nil {
			return nil, fmt.Errorf("read archived finding %s: %w", identity.FindingID, err)
		}
		document, err := ParseFindingDocument(findingBody, findingPath)
		if err != nil {
			return nil, fmt.Errorf("parse archived finding %s: %w", identity.FindingID, err)
		}
		finding := document.Record
		if finding.ID != identity.FindingID || finding.CampaignID != identity.CampaignID ||
			finding.Subject != identity.Subject || finding.Claim != identity.Claim {
			return nil, fmt.Errorf(
				"legacy truth %s does not match archived finding %s",
				relocation.Source, identity.FindingID)
		}
		destination := canonicalTruthDestination(campaign.Slug, finding.ID)
		if prior := seenDestinations[destination]; prior != "" {
			return nil, fmt.Errorf(
				"truth relocations %s and %s resolve to the same destination %s",
				prior, relocation.Source, destination)
		}
		seenDestinations[destination] = relocation.Source
		projection, err := BuildTruthProjection(service.Boundary, document, destination)
		if err != nil {
			return nil, fmt.Errorf("rebuild truth finding %s: %w", finding.ID, err)
		}
		artifacts = append(artifacts, StateArtifactWrite{
			Path: destination, ContentDigest: projection.ContentDigest, Body: projection.Body,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func truthRelocationDeletes(request ManagerApplyRequest) []StateArtifactDelete {
	if request.Action != "truth.relocate" {
		return nil
	}
	deletions := make([]StateArtifactDelete, 0, len(request.TruthRelocations))
	for _, relocation := range request.TruthRelocations {
		deletions = append(deletions, StateArtifactDelete{
			Path: relocation.Source, ExpectedDigest: relocation.ExpectedDigest,
		})
	}
	sort.Slice(deletions, func(i, j int) bool { return deletions[i].Path < deletions[j].Path })
	return deletions
}

func replayTruthRelocation(
	store *StateStore,
	boundary Boundary,
	request ManagerApplyRequest,
) (StateTransactionReceipt, bool, error) {
	if request.Action != "truth.relocate" {
		return StateTransactionReceipt{}, false, nil
	}
	receipt, found, err := store.loadIdempotencyReceipt(request.IdempotencyKey)
	if err != nil || !found {
		return StateTransactionReceipt{}, false, err
	}
	if len(receipt.Artifacts) != len(request.TruthRelocations) ||
		len(receipt.DeletedArtifacts) != len(request.TruthRelocations) {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	artifacts := make([]StateArtifactWrite, 0, len(receipt.Artifacts))
	for _, result := range receipt.Artifacts {
		if err := validateTruthDestination(result.Path); err != nil {
			return StateTransactionReceipt{}, false, ErrIdempotencyConflict
		}
		absolute, err := boundary.Resolve(result.Path, true)
		if err != nil {
			return StateTransactionReceipt{}, false, ErrIdempotencyConflict
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != result.ContentDigest {
			return StateTransactionReceipt{}, false, ErrIdempotencyConflict
		}
		artifacts = append(artifacts, StateArtifactWrite{
			Path: result.Path, ContentDigest: result.ContentDigest, Body: body,
		})
	}
	writes, reviewHandle, err := buildManagerWrites(boundary, request, artifacts)
	if err != nil {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	prepared, err := prepareTransactionRequest(
		managerStateTransactionRequest(request, writes, artifacts, reviewHandle))
	if err != nil || !receiptAcceptsPreparedRequest(receipt, prepared) {
		return StateTransactionReceipt{}, false, ErrIdempotencyConflict
	}
	return receipt, true, nil
}

func parseLegacyTruthIdentity(body []byte) (legacyTruthIdentity, error) {
	normalized := string(normalizeNewlines(body))
	if !strings.HasPrefix(normalized, "---\n") {
		return legacyTruthIdentity{}, errors.New("legacy truth projection has no frontmatter")
	}
	closing := strings.Index(normalized[4:], "\n---\n")
	if closing < 0 {
		return legacyTruthIdentity{}, errors.New("legacy truth projection frontmatter is not terminated")
	}
	frontmatter := normalized[4 : closing+4]
	wanted := map[string]*string{}
	identity := legacyTruthIdentity{}
	wanted["sourceCampaign"] = &identity.CampaignID
	wanted["sourceFinding"] = &identity.FindingID
	wanted["subject"] = &identity.Subject
	wanted["claim"] = &identity.Claim
	seen := map[string]bool{}
	for lineNumber, line := range strings.Split(frontmatter, "\n") {
		if line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		key, value, err := splitYAMLPair(line)
		if err != nil {
			return legacyTruthIdentity{}, fmt.Errorf("frontmatter line %d: %w", lineNumber+2, err)
		}
		target, required := wanted[key]
		if !required {
			continue
		}
		if seen[key] {
			return legacyTruthIdentity{}, fmt.Errorf("duplicate frontmatter key %q", key)
		}
		parsed, err := parseYAMLString(value)
		if err != nil {
			return legacyTruthIdentity{}, fmt.Errorf("frontmatter %s: %w", key, err)
		}
		*target = parsed
		seen[key] = true
	}
	for key, target := range wanted {
		if !seen[key] || strings.TrimSpace(*target) == "" {
			return legacyTruthIdentity{}, fmt.Errorf("required frontmatter key %q is missing", key)
		}
	}
	if !campaignIDRE.MatchString(identity.CampaignID) || !findingIDRE.MatchString(identity.FindingID) {
		return legacyTruthIdentity{}, errors.New("legacy truth provenance identity is invalid")
	}
	return identity, nil
}

func findArchivedCampaign(
	boundary Boundary,
	campaignID string,
) (string, CampaignRecord, error) {
	root := filepath.Join(boundary.Root, "docs", "history", "campaigns")
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", CampaignRecord{}, err
	}
	type match struct {
		root     string
		campaign CampaignRecord
	}
	matches := []match{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return "", CampaignRecord{}, fmt.Errorf(
				"campaign archive entry %s is a symbolic link", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		relative := path.Join(
			"docs", "history", "campaigns", entry.Name(), "finalization", "campaign.json")
		candidate := filepath.Join(boundary.Root, filepath.FromSlash(relative))
		info, statErr := os.Lstat(candidate)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return "", CampaignRecord{}, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", CampaignRecord{}, fmt.Errorf(
				"archived campaign record %s is not a real regular file", relative)
		}
		absolute, resolveErr := boundary.Resolve(relative, true)
		if resolveErr != nil {
			return "", CampaignRecord{}, resolveErr
		}
		body, readErr := readSingleLinkRegularFile(absolute)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return "", CampaignRecord{}, readErr
		}
		var campaign CampaignRecord
		if decodeErr := decodeStrictJSON(body, &campaign); decodeErr != nil {
			return "", CampaignRecord{}, fmt.Errorf("decode %s: %w", relative, decodeErr)
		}
		sealed, canonical, sealErr := sealCampaignRecord(campaign)
		if sealErr != nil || !bytes.Equal(body, canonical) {
			return "", CampaignRecord{}, fmt.Errorf("archived campaign %s is not canonical", relative)
		}
		campaign = sealed.(CampaignRecord)
		if campaign.ID == campaignID {
			matches = append(matches, match{
				root: path.Join("docs", "history", "campaigns", entry.Name()), campaign: campaign,
			})
		}
	}
	if len(matches) != 1 {
		return "", CampaignRecord{}, fmt.Errorf(
			"expected exactly one closed archive for campaign %s and found %d", campaignID, len(matches))
	}
	return matches[0].root, matches[0].campaign, nil
}
