package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CanonicalRecordHandle struct {
	Path          string `json:"path"`
	RecordID      string `json:"recordId"`
	Revision      int64  `json:"revision"`
	RecordDigest  string `json:"recordDigest"`
	ContentDigest string `json:"contentDigest"`
}

// ListCampaigns deterministically enumerates only 0.8 campaign records. A
// legacy directory without campaign.json is not interpreted by this store.
func (store *StateStore) ListCampaigns() ([]CampaignRecord, error) {
	activeRoot := filepath.Join(store.Boundary.Root, "active")
	entries, err := os.ReadDir(activeRoot)
	if os.IsNotExist(err) {
		return []CampaignRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := []CampaignRecord{}
	seenIDs := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("active campaign entry %s is a symbolic link", entry.Name())
			}
			continue
		}
		if !managedSlugRE.MatchString(entry.Name()) {
			continue
		}
		relative := "active/" + entry.Name() + "/campaign.json"
		if _, err := os.Stat(filepath.Join(activeRoot, entry.Name(), "campaign.json")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		_, value, _, err := store.readCanonicalRecordValue(relative)
		if err != nil {
			return nil, err
		}
		record, ok := value.(CampaignRecord)
		if !ok || record.Slug != entry.Name() || seenIDs[record.ID] {
			return nil, fmt.Errorf("campaign %s has mismatched or duplicate identity", entry.Name())
		}
		seenIDs[record.ID] = true
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Slug != records[j].Slug {
			return records[i].Slug < records[j].Slug
		}
		return records[i].ID < records[j].ID
	})
	return records, nil
}

// LoadCampaignGraph accepts either an exact slug or campaign ID and returns a
// fresh filesystem projection. No index or cache participates.
func (store *StateStore) LoadCampaignGraph(slugOrID string) (CampaignGraph, error) {
	slug := slugOrID
	if campaignIDRE.MatchString(slugOrID) {
		campaigns, err := store.ListCampaigns()
		if err != nil {
			return CampaignGraph{}, err
		}
		slug = ""
		for _, campaign := range campaigns {
			if campaign.ID == slugOrID {
				if slug != "" {
					return CampaignGraph{}, fmt.Errorf("campaign id %s is not unique", slugOrID)
				}
				slug = campaign.Slug
			}
		}
		if slug == "" {
			return CampaignGraph{}, os.ErrNotExist
		}
	}
	if !managedSlugRE.MatchString(slug) {
		return CampaignGraph{}, fmt.Errorf("invalid campaign slug or id %q", slugOrID)
	}
	graph := NewCampaignGraph()
	campaignPath := "active/" + slug + "/campaign.json"
	_, value, _, err := store.readCanonicalRecordValue(campaignPath)
	if err != nil {
		return CampaignGraph{}, err
	}
	campaign, ok := value.(CampaignRecord)
	if !ok || campaign.Slug != slug {
		return CampaignGraph{}, errors.New("campaign record path and identity disagree")
	}
	graph.Campaign = &campaign

	if err := store.loadFlatRecordDirectory("active/"+slug+"/work-items", ".json", func(_ string, value any) error {
		record, ok := value.(WorkItemRecord)
		if !ok || graph.WorkItems[record.ID].ID != "" {
			return errors.New("work-item directory contains a wrong or duplicate record")
		}
		graph.WorkItems[record.ID] = record
		return nil
	}); err != nil {
		return CampaignGraph{}, err
	}
	if err := store.loadRunRecords(slug, &graph); err != nil {
		return CampaignGraph{}, err
	}
	if err := store.loadFlatRecordDirectory("active/"+slug+"/findings", ".md", func(_ string, value any) error {
		document, ok := value.(FindingDocument)
		if !ok || graph.Findings[document.Record.ID].ID != "" {
			return errors.New("finding directory contains a wrong or duplicate record")
		}
		graph.Findings[document.Record.ID] = document.Record
		return nil
	}); err != nil {
		return CampaignGraph{}, err
	}
	if err := store.loadFlatRecordDirectory("active/"+slug+"/intake", ".json", func(_ string, value any) error {
		record, ok := value.(IntakeRecord)
		if !ok || graph.Intakes[record.ID].ID != "" {
			return errors.New("intake directory contains a wrong or duplicate record")
		}
		graph.Intakes[record.ID] = record
		return nil
	}); err != nil {
		return CampaignGraph{}, err
	}
	if err := store.loadFlatRecordDirectory("active/"+slug+"/reviews", ".json", func(_ string, value any) error {
		record, ok := value.(ReviewRecord)
		if !ok || graph.Reviews[record.ID].ID != "" {
			return errors.New("review directory contains a wrong or duplicate record")
		}
		graph.Reviews[record.ID] = record
		return nil
	}); err != nil {
		return CampaignGraph{}, err
	}
	for filename, assign := range map[string]func(any) error{
		"plan.json": func(value any) error {
			record, ok := value.(ClosurePlan)
			if !ok {
				return errors.New("invalid closure plan")
			}
			graph.ClosurePlan = &record
			return nil
		},
		"job.json": func(value any) error {
			record, ok := value.(ClosureJob)
			if !ok {
				return errors.New("invalid closure job")
			}
			graph.ClosureJob = &record
			return nil
		},
		"coverage.json": func(value any) error {
			record, ok := value.(ClosureCoverage)
			if !ok {
				return errors.New("invalid closure coverage")
			}
			graph.ClosureCoverage = &record
			return nil
		},
		"receipt.json": func(value any) error {
			record, ok := value.(ClosureReceipt)
			if !ok {
				return errors.New("invalid closure receipt")
			}
			graph.ClosureReceipt = &record
			return nil
		},
	} {
		relative := "active/" + slug + "/closure/" + filename
		if _, err := os.Stat(filepath.Join(store.Boundary.Root, filepath.FromSlash(relative))); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return CampaignGraph{}, err
		}
		_, value, _, err := store.readCanonicalRecordValue(relative)
		if err != nil {
			return CampaignGraph{}, err
		}
		if err := assign(value); err != nil {
			return CampaignGraph{}, err
		}
	}
	if err := graph.Validate(); err != nil {
		return CampaignGraph{}, err
	}
	return graph, nil
}

// ReadCanonicalRecord expands one exact canonical handle after path, type,
// schema, record digest, and byte-canonicalization checks.
func (store *StateStore) ReadCanonicalRecord(relative string) ([]byte, CanonicalRecordHandle, error) {
	body, _, handle, err := store.readCanonicalRecordValue(relative)
	return body, handle, err
}

func (store *StateStore) readCanonicalRecordValue(relative string) ([]byte, any, CanonicalRecordHandle, error) {
	if err := validateRelativeRecordPath(relative); err != nil {
		return nil, nil, CanonicalRecordHandle{}, err
	}
	absolute, err := store.Boundary.Resolve(relative, true)
	if err != nil {
		return nil, nil, CanonicalRecordHandle{}, err
	}
	body, err := readSingleLinkRegularFile(absolute)
	if err != nil {
		return nil, nil, CanonicalRecordHandle{}, err
	}
	value, canonical, err := decodeCanonicalRecord(relative, body)
	if err != nil {
		return nil, nil, CanonicalRecordHandle{}, err
	}
	if !bytes.Equal(body, canonical) {
		return nil, nil, CanonicalRecordHandle{}, errors.New("canonical record bytes are not in deterministic form")
	}
	id, revision, digest := recordHandleIdentity(value)
	return body, value, CanonicalRecordHandle{
		Path: relative, RecordID: id, Revision: revision, RecordDigest: digest,
		ContentDigest: "sha256:" + SHA256Bytes(body),
	}, nil
}

func decodeCanonicalRecord(relative string, body []byte) (any, []byte, error) {
	parts := strings.Split(relative, "/")
	var target any
	switch {
	case len(parts) == 3 && parts[0] == "active" && parts[2] == "campaign.json":
		target = &CampaignRecord{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "work-items" && strings.HasSuffix(parts[3], ".json"):
		target = &WorkItemRecord{}
	case len(parts) == 5 && parts[0] == "active" && parts[2] == "runs" && parts[4] == "run.json":
		target = &RunRecord{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "findings" && strings.HasSuffix(parts[3], ".md"):
		document, err := ParseFindingDocument(body, relative)
		if err != nil {
			return nil, nil, err
		}
		canonical, err := RenderFindingDocument(document)
		return document, canonical, err
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "intake" && strings.HasSuffix(parts[3], ".json"):
		target = &IntakeRecord{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "reviews" && strings.HasSuffix(parts[3], ".json"):
		target = &ReviewRecord{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "closure" && parts[3] == "plan.json":
		target = &ClosurePlan{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "closure" && parts[3] == "job.json":
		target = &ClosureJob{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "closure" && parts[3] == "coverage.json":
		target = &ClosureCoverage{}
	case len(parts) == 4 && parts[0] == "active" && parts[2] == "closure" && parts[3] == "receipt.json":
		target = &ClosureReceipt{}
	case len(parts) == 5 && parts[0] == "docs" && parts[1] == "history" && parts[2] == "campaigns" && parts[4] == "manifest.json":
		target = &ArchiveManifest{}
	default:
		return nil, nil, fmt.Errorf("path %s is not a canonical record handle", relative)
	}
	if len(parts) >= 2 && parts[0] == "active" && !managedSlugRE.MatchString(parts[1]) {
		return nil, nil, errors.New("canonical record has an invalid campaign slug")
	}
	if err := decodeStrictJSON(body, target); err != nil {
		return nil, nil, err
	}
	sealed, canonical, err := sealStateRecord(target, relative)
	if err != nil {
		return nil, nil, err
	}
	_, _, declaredDigest := recordHandleIdentity(targetValue(target))
	_, _, sealedDigest := recordHandleIdentity(sealed)
	if declaredDigest != sealedDigest {
		return nil, nil, errors.New("canonical record digest does not verify")
	}
	return sealed, canonical, nil
}

func targetValue(target any) any {
	switch value := target.(type) {
	case *CampaignRecord:
		return *value
	case *WorkItemRecord:
		return *value
	case *RunRecord:
		return *value
	case *IntakeRecord:
		return *value
	case *ReviewRecord:
		return *value
	case *ClosurePlan:
		return *value
	case *ClosureJob:
		return *value
	case *ClosureCoverage:
		return *value
	case *ClosureReceipt:
		return *value
	case *ArchiveManifest:
		return *value
	default:
		return target
	}
}

func recordHandleIdentity(value any) (string, int64, string) {
	switch record := value.(type) {
	case CampaignRecord:
		return record.ID, record.Revision, record.Digest
	case WorkItemRecord:
		return record.ID, record.Revision, record.Digest
	case RunRecord:
		return record.ID, record.Revision, record.Digest
	case FindingDocument:
		return record.Record.ID, record.Record.Revision, record.Record.Digest
	case IntakeRecord:
		return record.ID, record.Revision, record.Digest
	case ReviewRecord:
		return record.ID, record.Revision, record.Digest
	case ClosurePlan:
		return "closure-plan", record.CampaignRevision, record.Digest
	case ClosureJob:
		return record.ID, record.Revision, record.Digest
	case ClosureCoverage:
		return "closure-coverage", 0, record.Digest
	case ClosureReceipt:
		return "closure-receipt", record.StateHeadRevision, record.Digest
	case ArchiveManifest:
		return "archive-manifest", 0, record.Digest
	default:
		return "", 0, ""
	}
}

func (store *StateStore) loadFlatRecordDirectory(relative, extension string, consume func(string, any) error) error {
	absolute := filepath.Join(store.Boundary.Root, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("canonical record directory %s is unsafe", relative)
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), extension) {
			return fmt.Errorf("canonical record directory %s contains unexpected entry %s", relative, entry.Name())
		}
		path := relative + "/" + entry.Name()
		_, value, _, err := store.readCanonicalRecordValue(path)
		if err != nil {
			return err
		}
		if err := consume(path, value); err != nil {
			return err
		}
	}
	return nil
}

func (store *StateStore) loadRunRecords(slug string, graph *CampaignGraph) error {
	relative := "active/" + slug + "/runs"
	absolute := filepath.Join(store.Boundary.Root, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("run record directory is unsafe")
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !runIDRE.MatchString(entry.Name()) {
			return fmt.Errorf("runs contains unexpected entry %s", entry.Name())
		}
		path := relative + "/" + entry.Name() + "/run.json"
		_, value, _, err := store.readCanonicalRecordValue(path)
		if err != nil {
			return err
		}
		record, ok := value.(RunRecord)
		if !ok || record.ID != entry.Name() || graph.Runs[record.ID].ID != "" {
			return errors.New("run record path and identity disagree")
		}
		graph.Runs[record.ID] = record
	}
	return nil
}
