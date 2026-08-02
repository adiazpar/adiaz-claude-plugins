package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrStateConflict       = errors.New("canonical state revision conflict")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with different input")
	ErrStateDirty          = errors.New("canonical state does not match its committed head")
)

// StateHead is the single project-wide commit pointer. Readers may use only
// records reachable through this head; recovery repairs a prepared journal to
// either this head or the fully published successor before another mutation.
type StateHead struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      int64  `json:"revision"`
	EventID       string `json:"eventId,omitempty"`
	EventDigest   string `json:"eventDigest,omitempty"`
	StateDigest   string `json:"stateDigest"`
	TransactionID string `json:"transactionId,omitempty"`
	EventJournal  string `json:"eventJournal,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	Digest        string `json:"digest"`
}

// StateStore owns only canonical filesystem transactions. Derived indexes
// are intentionally absent: deleting every cache cannot change this head.
type StateStore struct {
	Boundary  Boundary
	Now       func() time.Time
	Failpoint func(StateFailpoint) error
}

// StateFailpoint is invoked only at crash-recovery seams. Tests may return an
// error to emulate abrupt process loss after the named durable step.
type StateFailpoint struct {
	Name          string
	TransactionID string
	Path          string
	Index         int
}

func NewStateStore(projectRoot string) (*StateStore, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return nil, err
	}
	return NewStateStoreWithBoundary(boundary), nil
}

func NewStateStoreWithBoundary(boundary Boundary) *StateStore {
	return &StateStore{Boundary: boundary, Now: func() time.Time { return time.Now().UTC() }}
}

func initialStateHead() StateHead {
	head := StateHead{
		SchemaVersion: CampaignSchemaVersion,
		StateDigest:   "sha256:" + SHA256String("re-discipline-state-v2-empty"),
	}
	_ = sealStateHead(&head)
	return head
}

func sealStateHead(head *StateHead) error {
	if head == nil {
		return errors.New("state head is required")
	}
	head.Digest = ""
	digest, err := CanonicalDigest(*head)
	if err != nil {
		return err
	}
	head.Digest = digest
	return validateStateHead(*head)
}

func validateStateHead(head StateHead) error {
	if head.SchemaVersion != CampaignSchemaVersion || head.Revision < 0 ||
		!digestRE.MatchString(head.StateDigest) || !digestRE.MatchString(head.Digest) {
		return errors.New("state head schema, revision, or digest is invalid")
	}
	if head.Revision == 0 {
		if head.EventID != "" || head.EventDigest != "" || head.TransactionID != "" ||
			head.EventJournal != "" || head.UpdatedAt != "" {
			return errors.New("initial state head cannot name an event or transaction")
		}
		return nil
	}
	if !eventIDRE.MatchString(head.EventID) || !digestRE.MatchString(head.EventDigest) ||
		!correlationIDRE.MatchString(head.TransactionID) || head.EventJournal == "" {
		return errors.New("committed state head is missing event identity")
	}
	if err := validateRelativeRecordPath(head.EventJournal); err != nil {
		return fmt.Errorf("event journal: %w", err)
	}
	if err := validateUTC(head.UpdatedAt); err != nil {
		return fmt.Errorf("state head updatedAt: %w", err)
	}
	return nil
}

func verifyStateHead(head StateHead) error {
	want := head.Digest
	if err := sealStateHead(&head); err != nil {
		return err
	}
	if want != head.Digest {
		return errors.New("state head digest does not verify")
	}
	return nil
}

// LoadHead is read-only. It does not create the state root or perform
// recovery; callers that intend to mutate must call Recover or Apply.
func (store *StateStore) LoadHead() (StateHead, error) {
	root, exists, err := store.existingStateRoot()
	if err != nil {
		return StateHead{}, err
	}
	if !exists {
		return initialStateHead(), nil
	}
	path, err := containedOutputPath(root, "head.json")
	if err != nil {
		return StateHead{}, err
	}
	body, err := readSingleLinkRegularFile(path)
	if os.IsNotExist(err) {
		return initialStateHead(), nil
	}
	if err != nil {
		return StateHead{}, err
	}
	var head StateHead
	if err := decodeStrictJSON(body, &head); err != nil {
		return StateHead{}, fmt.Errorf("decode state head: %w", err)
	}
	if err := verifyStateHead(head); err != nil {
		return StateHead{}, err
	}
	return head, nil
}

func (store *StateStore) existingStateRoot() (string, bool, error) {
	if store == nil || store.Boundary.Root == "" {
		return "", false, errors.New("state store boundary is required")
	}
	stateRoot := filepath.Join(store.Boundary.Root, ".re-discipline", "state")
	info, err := os.Lstat(stateRoot)
	if os.IsNotExist(err) {
		return stateRoot, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, errors.New("canonical state root is not a real directory")
	}
	resolved, err := canonicalExistingPath(stateRoot)
	if err != nil || !withinRoot(store.Boundary.Root, resolved) {
		return "", false, errors.New("canonical state root escapes the project")
	}
	return resolved, true, nil
}

func (store *StateStore) ensureStateRoot() (string, error) {
	if root, exists, err := store.existingStateRoot(); err != nil || exists {
		return root, err
	}
	controlRoot := filepath.Join(store.Boundary.Root, ".re-discipline")
	info, err := os.Lstat(controlRoot)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("project .re-discipline directory is missing or unsafe")
	}
	target, err := containedOutputPath(store.Boundary.Root, ".re-discipline/state")
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(target, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	root, exists, err := store.existingStateRoot()
	if err != nil || !exists {
		return "", fmt.Errorf("create canonical state root: %w", err)
	}
	return root, nil
}

func (store *StateStore) statePath(relative string) (string, error) {
	root, err := store.ensureStateRoot()
	if err != nil {
		return "", err
	}
	return containedOutputPath(root, relative)
}

func (store *StateStore) canonicalOutputPath(relative string) (string, error) {
	if err := validateRelativeRecordPath(relative); err != nil {
		return "", err
	}
	return containedOutputPath(store.Boundary.Root, relative)
}

func decodeStrictJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func readSingleLinkRegularFile(filePath string) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("canonical state input is not a real regular file")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	multiple, err := writerFileHasMultipleLinks(file)
	if err != nil {
		return nil, err
	}
	if multiple {
		return nil, errors.New("canonical state input has an unsafe link count")
	}
	return io.ReadAll(file)
}

func canonicalStatePath(slug, category, id, filename string) (string, error) {
	if !managedSlugRE.MatchString(slug) {
		return "", fmt.Errorf("invalid campaign slug %q", slug)
	}
	parts := []string{"active", slug}
	if category != "" {
		parts = append(parts, category)
	}
	if id != "" {
		parts = append(parts, id)
	}
	if filename != "" {
		parts = append(parts, filename)
	}
	return strings.Join(parts, "/"), nil
}
