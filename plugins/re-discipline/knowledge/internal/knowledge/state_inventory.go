package knowledge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const stateInventoryPath = ".re-discipline/state/inventory.json"

// StateInventory is the head-bound byte inventory for canonical state. The
// mutation hash chain proves ordering; this inventory independently proves
// that the files reachable at that head still contain the committed bytes.
// Derived views, caches, journals, and idempotency receipts are intentionally
// excluded.
type StateInventory struct {
	SchemaVersion int                   `json:"schemaVersion"`
	HeadRevision  int64                 `json:"headRevision"`
	Entries       []StateInventoryEntry `json:"entries"`
	Digest        string                `json:"digest"`
}

type StateInventoryEntry struct {
	Path          string `json:"path"`
	ContentDigest string `json:"contentDigest"`
}

func emptyStateInventory(revision int64) StateInventory {
	inventory := StateInventory{
		SchemaVersion: CampaignSchemaVersion,
		HeadRevision:  revision,
		Entries:       []StateInventoryEntry{},
	}
	_ = sealStateInventory(&inventory)
	return inventory
}

func sealStateInventory(inventory *StateInventory) error {
	if inventory == nil {
		return errors.New("state inventory is required")
	}
	inventory.Entries = append([]StateInventoryEntry(nil), inventory.Entries...)
	sort.Slice(inventory.Entries, func(i, j int) bool {
		return inventory.Entries[i].Path < inventory.Entries[j].Path
	})
	seen := map[string]bool{}
	for _, entry := range inventory.Entries {
		if validateRelativeRecordPath(entry.Path) != nil ||
			!digestRE.MatchString(entry.ContentDigest) ||
			entry.Path == stateInventoryPath || seen[entry.Path] {
			return errors.New("state inventory contains an invalid or repeated entry")
		}
		seen[entry.Path] = true
	}
	if inventory.SchemaVersion != CampaignSchemaVersion || inventory.HeadRevision < 0 {
		return errors.New("state inventory schema or head revision is invalid")
	}
	inventory.Digest = ""
	digest, err := CanonicalDigest(*inventory)
	if err != nil {
		return err
	}
	inventory.Digest = digest
	return nil
}

func verifyStateInventory(inventory StateInventory) error {
	want := inventory.Digest
	if !digestRE.MatchString(want) {
		return errors.New("state inventory digest is invalid")
	}
	if err := sealStateInventory(&inventory); err != nil {
		return err
	}
	if inventory.Digest != want {
		return errors.New("state inventory digest does not verify")
	}
	return nil
}

func inventoryEntriesMap(inventory StateInventory) map[string]string {
	entries := make(map[string]string, len(inventory.Entries))
	for _, entry := range inventory.Entries {
		entries[entry.Path] = entry.ContentDigest
	}
	return entries
}

func stateInventoryFromMap(revision int64, entries map[string]string) (StateInventory, error) {
	inventory := StateInventory{
		SchemaVersion: CampaignSchemaVersion,
		HeadRevision:  revision,
		Entries:       make([]StateInventoryEntry, 0, len(entries)),
	}
	for relative, digest := range entries {
		inventory.Entries = append(inventory.Entries, StateInventoryEntry{
			Path: relative, ContentDigest: digest,
		})
	}
	if err := sealStateInventory(&inventory); err != nil {
		return StateInventory{}, err
	}
	return inventory, nil
}

func (store *StateStore) loadCommittedInventory(head StateHead) (StateInventory, error) {
	if head.Revision == 0 {
		inventory := emptyStateInventory(0)
		if inventory.Digest != head.InventoryDigest {
			return StateInventory{}, fmt.Errorf("%w: initial inventory digest does not match the head", ErrStateDirty)
		}
		absolute := filepath.Join(store.Boundary.Root, filepath.FromSlash(stateInventoryPath))
		if _, err := os.Lstat(absolute); err == nil || !os.IsNotExist(err) {
			return StateInventory{}, fmt.Errorf("%w: initial state unexpectedly has an inventory file", ErrStateDirty)
		}
		return inventory, nil
	}
	absolute, err := store.canonicalOutputPath(stateInventoryPath)
	if err != nil {
		return StateInventory{}, err
	}
	body, err := readSingleLinkRegularFile(absolute)
	if err != nil {
		return StateInventory{}, fmt.Errorf("%w: read committed state inventory: %v", ErrStateDirty, err)
	}
	var inventory StateInventory
	if err := decodeStrictJSON(body, &inventory); err != nil {
		return StateInventory{}, fmt.Errorf("%w: decode committed state inventory: %v", ErrStateDirty, err)
	}
	if err := verifyStateInventory(inventory); err != nil ||
		inventory.HeadRevision != head.Revision || inventory.Digest != head.InventoryDigest {
		return StateInventory{}, fmt.Errorf("%w: state inventory does not match the committed head", ErrStateDirty)
	}
	canonical, err := canonicalJSON(inventory)
	if err != nil || SHA256Bytes(canonical) != SHA256Bytes(body) {
		return StateInventory{}, fmt.Errorf("%w: state inventory bytes are not canonical", ErrStateDirty)
	}
	return inventory, nil
}

// inventoryDrift returns every tracked path whose bytes changed or vanished,
// plus every untracked path whose location declares it to be canonical state.
func (store *StateStore) inventoryDrift(inventory StateInventory) ([]string, error) {
	committed := inventoryEntriesMap(inventory)
	dirty := []string{}
	for relative, expected := range committed {
		absolute, err := store.canonicalOutputPath(relative)
		if err != nil {
			return nil, err
		}
		actual, exists, err := currentFileDigest(absolute)
		if err != nil {
			return nil, err
		}
		if !exists || actual != expected {
			dirty = append(dirty, relative)
		}
	}
	candidates, err := store.canonicalInventoryCandidates(inventory.HeadRevision > 0)
	if err != nil {
		return nil, err
	}
	for relative := range candidates {
		if _, tracked := committed[relative]; !tracked {
			dirty = append(dirty, relative)
		}
	}
	return SortedUnique(dirty), nil
}

func (store *StateStore) canonicalInventoryCandidates(includeDurableDocs bool) (map[string]string, error) {
	roots := []string{"active"}
	if includeDurableDocs {
		roots = append(roots, "docs/truth", "docs/history/campaigns", ".re-discipline/migration/0.8")
	}
	return store.canonicalInventoryCandidatesForRoots(roots)
}

func (store *StateStore) canonicalInventoryCandidatesForRoots(roots []string) (map[string]string, error) {
	result := map[string]string{}
	for _, root := range roots {
		absolute := filepath.Join(store.Boundary.Root, filepath.FromSlash(root))
		info, err := os.Lstat(absolute)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("%w: canonical inventory root %s is unsafe", ErrStateDirty, root)
		}
		err = filepath.WalkDir(absolute, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == absolute {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: canonical inventory contains a symbolic link", ErrStateDirty)
			}
			if entry.IsDir() {
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%w: canonical inventory contains a non-regular file", ErrStateDirty)
			}
			relative, err := filepath.Rel(store.Boundary.Root, current)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if !canonicalInventoryCandidatePath(relative) {
				return nil
			}
			body, err := readSingleLinkRegularFile(current)
			if err != nil {
				return err
			}
			result[relative] = "sha256:" + SHA256Bytes(body)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func canonicalInventoryCandidatePath(relative string) bool {
	if relative == ".re-discipline/migration/0.8/certification.json" {
		return true
	}
	if strings.HasPrefix(relative, "docs/truth/") ||
		strings.HasPrefix(relative, "docs/history/campaigns/") {
		return true
	}
	parts := strings.Split(relative, "/")
	if len(parts) < 3 || parts[0] != "active" || !managedSlugRE.MatchString(parts[1]) {
		return false
	}
	if len(parts) == 3 {
		return parts[2] == "campaign.json"
	}
	switch parts[2] {
	case "work-items":
		return len(parts) == 4 && workItemIDRE.MatchString(strings.TrimSuffix(parts[3], ".json")) && strings.HasSuffix(parts[3], ".json")
	case "runs":
		return len(parts) == 5 && runIDRE.MatchString(parts[3]) && parts[4] == "run.json"
	case "findings":
		return len(parts) == 4 && findingIDRE.MatchString(strings.TrimSuffix(parts[3], ".md")) && strings.HasSuffix(parts[3], ".md")
	case "intake":
		return len(parts) == 4 && intakeIDRE.MatchString(strings.TrimSuffix(parts[3], ".json")) && strings.HasSuffix(parts[3], ".json")
	case "reviews":
		return len(parts) == 4 && reviewIDRE.MatchString(strings.TrimSuffix(parts[3], ".json")) && strings.HasSuffix(parts[3], ".json")
	case "events":
		return len(parts) == 4 && parts[3] == "events.jsonl"
	case "closure":
		return len(parts) == 4 && strings.HasSuffix(parts[3], ".json")
	}
	return false
}

func (store *StateStore) resultingStateInventory(
	previous StateInventory,
	revision int64,
	writes []preparedStateWrite,
	artifacts []preparedStateArtifact,
	eventPath string,
	eventBody []byte,
	retiredTree string,
) (StateInventory, error) {
	entries := inventoryEntriesMap(previous)
	if previous.HeadRevision == 0 {
		// Drop-in and greenfield initialization may begin with durable truth or
		// campaign-history documents but no campaign transaction yet. Bind those
		// existing canonical bytes in the first real head; otherwise the first
		// later truth projection would make the older documents appear as
		// unexplained additions.
		baseline, err := store.canonicalInventoryCandidatesForRoots(
			[]string{"docs/truth", "docs/history/campaigns"},
		)
		if err != nil {
			return StateInventory{}, err
		}
		for relative, digest := range baseline {
			entries[relative] = digest
		}
	}
	for _, write := range writes {
		entries[write.Path] = write.ContentDigest
		if run, ok := write.Record.(RunRecord); ok {
			if err := store.addRunInventoryEntries(entries, run, write.Path, artifacts); err != nil {
				return StateInventory{}, err
			}
		}
	}
	for _, artifact := range artifacts {
		entries[artifact.Path] = artifact.ContentDigest
	}
	entries[eventPath] = "sha256:" + SHA256Bytes(eventBody)
	if retiredTree != "" {
		prefix := strings.TrimSuffix(retiredTree, "/") + "/"
		for relative := range entries {
			if relative == retiredTree || strings.HasPrefix(relative, prefix) {
				delete(entries, relative)
			}
		}
	}
	return stateInventoryFromMap(revision, entries)
}

func (store *StateStore) addRunInventoryEntries(
	entries map[string]string,
	run RunRecord,
	runRecordPath string,
	artifacts []preparedStateArtifact,
) error {
	prepared := map[string]string{}
	for _, artifact := range artifacts {
		prepared[artifact.Path] = artifact.ContentDigest
	}
	add := func(handle FileHandle) error {
		if digest, ok := prepared[handle.Path]; ok {
			if digest != handle.SHA256 {
				return errors.New("run inventory handle disagrees with its transaction artifact")
			}
			entries[handle.Path] = digest
			return nil
		}
		absolute, err := store.canonicalOutputPath(handle.Path)
		if err != nil {
			return err
		}
		digest, exists, err := currentFileDigest(absolute)
		if err != nil || !exists || digest != handle.SHA256 {
			return fmt.Errorf("run inventory handle %s does not resolve to its frozen bytes", handle.Path)
		}
		entries[handle.Path] = digest
		return nil
	}
	parts := strings.Split(runRecordPath, "/")
	if len(parts) != 5 || parts[0] != "active" || parts[2] != "runs" ||
		parts[3] != run.ID || parts[4] != "run.json" {
		return errors.New("run inventory record path is invalid")
	}
	slug := parts[1]
	for _, handle := range []*FileHandle{run.Brief, run.ContextPack, run.Report} {
		if handle != nil {
			if err := add(*handle); err != nil {
				return err
			}
		}
	}
	for _, file := range run.Files {
		relative := path.Join("active", slug, "runs", run.ID, file.Path)
		if err := add(FileHandle{Path: relative, SHA256: file.SHA256}); err != nil {
			return err
		}
	}
	return nil
}

func requireReconciledInventoryDrift(dirty []string, writes []preparedStateWrite, artifacts []preparedStateArtifact, eventPath string) error {
	explicit := map[string]bool{}
	touched := map[string]bool{eventPath: true}
	for _, write := range writes {
		touched[write.Path] = true
		if !write.Internal {
			explicit[write.Path] = true
		}
	}
	for _, artifact := range artifacts {
		touched[artifact.Path] = true
	}
	reconciled := []string{}
	for _, relative := range dirty {
		if explicit[relative] {
			reconciled = append(reconciled, relative)
			continue
		}
		if touched[relative] {
			return fmt.Errorf(
				"%w: reconciliation would implicitly adopt dirty canonical path %s; submit that record explicitly or restore its committed bytes",
				ErrStateDirty, relative,
			)
		}
	}
	if len(reconciled) == 0 {
		return fmt.Errorf("%w: reconciliation must explicitly write at least one dirty canonical path", ErrStateDirty)
	}
	return nil
}

func (service *Service) canonicalStateIntegrityStatus() map[string]any {
	status := map[string]any{
		"clean": false, "headRevision": int64(0), "dirtyPaths": []string{},
	}
	if service == nil {
		status["error"] = "service is unavailable"
		return status
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	head, err := store.LoadHead()
	if err != nil {
		status["error"] = err.Error()
		return status
	}
	status["headRevision"] = head.Revision
	inventory, err := store.loadCommittedInventory(head)
	if err != nil {
		status["error"] = err.Error()
		return status
	}
	dirty, err := store.inventoryDrift(inventory)
	if err != nil {
		status["error"] = err.Error()
		return status
	}
	status["dirtyPaths"] = dirty
	status["clean"] = len(dirty) == 0
	status["inventoryDigest"] = inventory.Digest
	return status
}
