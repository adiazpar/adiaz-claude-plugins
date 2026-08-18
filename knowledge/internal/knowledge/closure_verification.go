package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type closureEvidenceProbe struct {
	Path       string
	Tier       string
	Searchable bool
}

// verifyStagedClosureRetrieval builds an isolated index over the projected
// post-cutover corpus. It exercises the production Retriever with bounded
// limits and token budgets; no staged byte is copied into a normal project
// source path or the project's serving index.
func (service *Service) verifyStagedClosureRetrieval(
	ctx context.Context,
	sourceGraph CampaignGraph,
	projectedGraph CampaignGraph,
	manifest closureStagingManifest,
) error {
	if len(manifest.ProjectionObjects) == 0 {
		return nil
	}
	temporary, err := os.MkdirTemp("", "re-discipline-closure-verification-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	verificationBoundary, err := NewBoundary(temporary)
	if err != nil {
		return err
	}

	base, err := DiscoverSources(service.Boundary, service.effectiveSettings())
	if err != nil {
		return err
	}
	documents := map[string]SourceDocument{}
	evidenceBodies := map[string][]byte{}
	activePrefix := path.Join("active", sourceGraph.Campaign.Slug) + "/"
	for _, document := range base.Documents {
		if strings.HasPrefix(document.Path, activePrefix) {
			continue
		}
		documents[document.Path] = document
	}
	root := closureStagingRoot(service.Boundary, manifest.CampaignID, manifest.ClosureJobID)
	for destination, digest := range manifest.ProjectionObjects {
		body, err := readClosureStageObject(root, digest)
		if err != nil {
			return err
		}
		tier, err := closureProjectionTier(destination)
		if err != nil {
			return err
		}
		documents[destination] = closureVerificationDocument(destination, tier, "", body)
	}
	findingIDs := make([]string, 0, len(sourceGraph.Findings))
	for id := range sourceGraph.Findings {
		findingIDs = append(findingIDs, id)
	}
	sort.Strings(findingIDs)
	for _, id := range findingIDs {
		body := []byte(nil)
		if digest := manifest.PromotedFindings[id]; digest != "" {
			body, err = readClosureStageObject(root, digest)
		} else {
			source := path.Join("active", sourceGraph.Campaign.Slug, "findings", id+".md")
			absolute, resolveErr := service.Boundary.Resolve(source, true)
			if resolveErr != nil {
				return resolveErr
			}
			body, err = readSingleLinkRegularFile(absolute)
			if err == nil {
				var sourceDocument FindingDocument
				sourceDocument, err = ParseFindingDocument(body, source)
				if err == nil && sourceDocument.Record.Digest != sourceGraph.Findings[id].Digest {
					err = fmt.Errorf("closure source finding %s changed during verification", id)
				}
			}
		}
		if err != nil {
			return err
		}
		destination := path.Join(manifest.ArchiveDestination, "findings", id+".md")
		archived, err := ParseFindingDocument(body, destination)
		if err != nil || archived.Record.Digest != projectedGraph.Findings[id].Digest {
			return fmt.Errorf("closure archived finding %s does not match its projected state", id)
		}
		documents[destination] = closureVerificationDocument(destination, "history", "finding", body)
	}

	probes := []closureEvidenceProbe{}
	for _, id := range findingIDs {
		prior, exists := sourceGraph.Findings[id]
		if !exists {
			return fmt.Errorf("staged finding %s lost its source finding", id)
		}
		projected, exists := projectedGraph.Findings[id]
		if !exists {
			return fmt.Errorf("staged finding %s lost its projected finding", id)
		}
		sourceEvidenceOrder, err := closureEvidenceSourcesInDurableOrder(
			prior.Evidence, projected.Evidence, sourceGraph.Campaign.Slug,
			manifest.ArchiveDestination)
		if err != nil {
			return err
		}
		for index, sourceEvidence := range sourceEvidenceOrder {
			durableEvidence := projected.Evidence[index]
			sourcePath, err := service.Boundary.Resolve(sourceEvidence.Path, true)
			if err != nil {
				return fmt.Errorf("closure evidence %s is unreachable: %w", sourceEvidence.Path, err)
			}
			sourceBody, err := readSingleLinkRegularFile(sourcePath)
			if err != nil {
				return err
			}
			if "sha256:"+SHA256Bytes(sourceBody) != sourceEvidence.SHA256 || durableEvidence.SHA256 != sourceEvidence.SHA256 {
				return fmt.Errorf("closure evidence %s changed during staged verification", sourceEvidence.Path)
			}
			if existing, duplicate := evidenceBodies[durableEvidence.Path]; duplicate &&
				"sha256:"+SHA256Bytes(existing) != sourceEvidence.SHA256 {
				return fmt.Errorf("closure evidence path %s has conflicting bytes", durableEvidence.Path)
			}
			evidenceBodies[durableEvidence.Path] = sourceBody
			searchable := utf8.Valid(sourceBody) && SensitiveContentReason(string(sourceBody)) == ""
			tier := "archive"
			if existing, ok := documents[durableEvidence.Path]; ok {
				tier = existing.Tier
			} else if searchable {
				documents[durableEvidence.Path] = closureVerificationDocument(
					durableEvidence.Path, tier, "", sourceBody)
			}
			probes = append(probes, closureEvidenceProbe{
				Path: durableEvidence.Path, Tier: tier, Searchable: searchable,
			})
		}
	}

	paths := make([]string, 0, len(documents))
	for relative := range documents {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	ordered := make([]SourceDocument, 0, len(paths))
	for _, relative := range paths {
		document := documents[relative]
		if err := writeClosureVerificationFile(verificationBoundary, relative, []byte(document.Content)); err != nil {
			return err
		}
		ordered = append(ordered, document)
	}
	for relative, body := range evidenceBodies {
		if _, indexed := documents[relative]; indexed {
			continue
		}
		if err := writeClosureVerificationFile(verificationBoundary, relative, body); err != nil {
			return err
		}
	}
	inventory, err := closureInventoryFromDocuments(ordered)
	if err != nil {
		return err
	}
	database := filepath.Join(temporary, "closure-verification.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return err
	}
	if err := createSchema(ctx, db); err != nil {
		db.Close()
		return err
	}
	generation := Generation{
		ID:       "closure-verification-" + strings.TrimPrefix(manifest.Digest, "sha256:")[:20],
		Database: database, CorpusFingerprint: inventory.Fingerprint,
		Project: "closure-verification", Worktree: "derived",
		ParserVersion: ParserVersion, ChunkerVersion: ChunkerVersion,
		CreatedAt:     projectedGraph.ClosureJob.UpdatedAt,
		DocumentCount: len(inventory.Documents), ChunkCount: len(inventory.Chunks),
	}
	if err := populateDatabase(ctx, db, generation, inventory, ModelManifest{}, ""); err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := verifyDatabase(database); err != nil {
		return err
	}
	retriever := Retriever{
		Boundary: verificationBoundary, Generation: generation,
		Profile: SelectedProfile{
			EffectiveIdentity: "closure-verification-v1",
			ActiveLanes:       []string{"exact", "fts", "graph"},
			Effective: EffectiveProfile{
				Weights: map[string]int{"exact": 8, "fts": 6, "graph": 2}, RRFK: 60,
				MaxPerDocument: 3, Packing: PackingPolicy{MaxPassages: 12, MaxBytes: 32768},
			},
		},
	}
	for id, destination := range manifest.FindingDestinations {
		if manifest.ProjectionObjects[destination] == "" {
			continue
		}
		finding := projectedGraph.Findings[id]
		tier, err := closureProjectionTier(destination)
		if err != nil {
			return err
		}
		response, err := retriever.Search(ctx, SearchOptions{
			Query: finding.Subject + " " + finding.Claim, QueryClass: "conceptual",
			AllowedTiers: []string{tier}, Limit: 8, TokenBudget: 2048,
		})
		if err != nil {
			return fmt.Errorf("verify staged retrieval for %s: %w", destination, err)
		}
		if !searchResponseContainsPath(response, destination) {
			return fmt.Errorf("staged projection %s is not reachable within 8 passages and 2048 tokens", destination)
		}
	}
	for _, probe := range probes {
		absolute, err := verificationBoundary.Resolve(probe.Path, true)
		if err != nil {
			return fmt.Errorf("staged evidence path %s is not exactly reachable", probe.Path)
		}
		body, err := readSingleLinkRegularFile(absolute)
		if err != nil || "sha256:"+SHA256Bytes(body) != "sha256:"+SHA256Bytes(evidenceBodies[probe.Path]) {
			return fmt.Errorf("staged evidence path %s is not exactly reachable", probe.Path)
		}
		if !probe.Searchable {
			continue
		}
		response, err := retriever.Search(ctx, SearchOptions{
			Query: probe.Path, QueryClass: "exact", AllowedTiers: []string{probe.Tier},
			Limit: 4, TokenBudget: 1024,
		})
		if err != nil || !searchResponseContainsPath(response, probe.Path) {
			return fmt.Errorf("staged evidence %s is not reachable through bounded retrieval", probe.Path)
		}
	}
	return nil
}

func closureProjectionTier(destination string) (string, error) {
	switch {
	case strings.HasPrefix(destination, "docs/truth/"):
		return "truth", nil
	case strings.HasPrefix(destination, "docs/backlog/"):
		return "backlog", nil
	case strings.HasPrefix(destination, "docs/playbooks/"):
		return "playbook", nil
	default:
		return "", fmt.Errorf("closure projection %s is outside a retrievable durable tier", destination)
	}
}

func closureVerificationDocument(relative, tier, sourceKind string, body []byte) SourceDocument {
	hash := SHA256Bytes(body)
	return SourceDocument{
		ID: StableID("doc", relative, hash), Path: relative, Tier: tier,
		Title: titleFromMarkdown(string(body), relative), SourceKind: sourceKind,
		Content: string(body), ContentHash: hash, Size: int64(len(body)),
	}
}

func writeClosureVerificationFile(boundary Boundary, relative string, body []byte) error {
	if validateRelativeRecordPath(relative) != nil {
		return fmt.Errorf("closure verification path %s is invalid", relative)
	}
	target, err := containedOutputPath(boundary.Root, relative)
	if err != nil {
		return err
	}
	return durableAtomicWrite(target, body, 0o600)
}

func closureInventoryFromDocuments(documents []SourceDocument) (SourceInventory, error) {
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	findings := []FindingDocument{}
	chunks := []Chunk{}
	for index := range documents {
		document := &documents[index]
		body := []byte(document.Content)
		document.ContentHash = SHA256Bytes(body)
		document.ID = StableID("doc", document.Path, document.ContentHash)
		document.Size = int64(len(body))
		document.Title = titleFromMarkdown(document.Content, document.Path)
		if document.SourceKind == "finding" {
			finding, err := ParseFindingDocument(body, document.Path)
			if err != nil {
				return SourceInventory{}, err
			}
			findings = append(findings, finding)
			document.FindingID = finding.Record.ID
			document.CampaignID = finding.Record.CampaignID
			document.FindingClaim = finding.Record.Claim
			document.EvidenceGrade = finding.Record.EvidenceGrade
			document.ReviewState = finding.Record.ReviewState
			document.Validity = finding.Record.Validity
		}
		chunks = append(chunks, ChunkMarkdown(*document)...)
	}
	edges := BuildGraphEdges(documents, chunks)
	fingerprintInput := make([]struct {
		Path, Tier, Hash, Kind string
	}, 0, len(documents))
	for _, document := range documents {
		fingerprintInput = append(fingerprintInput, struct {
			Path, Tier, Hash, Kind string
		}{document.Path, document.Tier, document.ContentHash, document.SourceKind})
	}
	fingerprint, err := CanonicalDigest(struct {
		Parser, Chunker string
		Documents       any
	}{ParserVersion, ChunkerVersion, fingerprintInput})
	if err != nil {
		return SourceInventory{}, err
	}
	return SourceInventory{
		Documents: documents, Findings: findings, Chunks: chunks, Edges: edges,
		Fingerprint: fingerprint,
	}, nil
}

func searchResponseContainsPath(response SearchResponse, target string) bool {
	for _, result := range response.Results {
		if result.Citation.Path == target {
			return true
		}
	}
	return false
}
