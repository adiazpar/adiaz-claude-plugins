package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A measurement run - a project benchmark or a calibration - reads one
// immutable generation for many minutes while another session in the same
// project may publish new generations. Publication is already serialized by
// the index writer lock, but generation retention deletes superseded
// databases, and retrieval opens the generation SQLite once per query. A
// deleted database therefore fails an in-flight run with SQLITE_CANTOPEN
// rather than corrupting anything.
//
// A generation lease is a small record under <cacheRoot>/leases/ that the
// measurement run holds open under the same operating-system lock primitive
// the index writer uses. Retention skips a leased generation and reclaims it
// after a later publication. The lease never blocks a publication, so a
// concurrent session keeps working at full speed.
//
// Crash cleanup falls out of the lock: the operating system drops the lock
// when the holder exits, so a later sweep acquires the lock, recognizes the
// absent holder, and removes the orphan record. A hard time-to-live bounds
// the pin even when a live holder hangs.

const generationLeaseTTL = 12 * time.Hour

const maxGenerationLeaseBytes = 4096

var generationIdentityRE = regexp.MustCompile(`^generation-[a-f0-9]{20}$`)

var generationLeasePurposeRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

type generationLeaseRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	GenerationID  string `json:"generationId"`
	Purpose       string `json:"purpose"`
	ProcessID     int    `json:"processId"`
	AcquiredAt    string `json:"acquiredAt"`
	ExpiresAt     string `json:"expiresAt"`
}

// GenerationLease pins one immutable generation for the life of a measurement
// run. Release is idempotent.
type GenerationLease struct {
	GenerationID string
	path         string
	file         *os.File
}

func generationLeaseDirectory(cacheRoot string, create bool) (string, error) {
	if create {
		if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
			return "", err
		}
	}
	directory, err := containedOutputPath(cacheRoot, "leases")
	if err != nil {
		return "", err
	}
	if create {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return "", err
		}
	}
	return directory, nil
}

func acquireGenerationLease(
	cacheRoot string,
	generationID string,
	purpose string,
) (*GenerationLease, error) {
	if !generationIdentityRE.MatchString(generationID) {
		return nil, errors.New(
			"generation lease requires a canonical generation identity")
	}
	if !generationLeasePurposeRE.MatchString(purpose) {
		return nil, errors.New("generation lease requires a simple purpose name")
	}
	directory, err := generationLeaseDirectory(cacheRoot, true)
	if err != nil {
		return nil, fmt.Errorf("resolve generation lease directory: %w", err)
	}
	now := time.Now()
	record := generationLeaseRecord{
		SchemaVersion: 1, GenerationID: generationID, Purpose: purpose,
		ProcessID:  os.Getpid(),
		AcquiredAt: RFC3339UTC(now),
		ExpiresAt:  RFC3339UTC(now.Add(generationLeaseTTL)),
	}
	body, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	for attempt := 0; attempt < 64; attempt++ {
		nonce := SHA256String(fmt.Sprintf(
			"%s|%d|%d|%d", purpose, record.ProcessID, now.UnixNano(), attempt))[:12]
		path, err := containedOutputPath(
			directory,
			fmt.Sprintf("%s.%d-%s.lease", generationID, record.ProcessID, nonce),
		)
		if err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		multiple, linkErr := writerFileHasMultipleLinks(file)
		if linkErr != nil || multiple {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, errors.New("generation lease file has an unsafe link count")
		}
		if err := tryLockLeaseFile(file); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, fmt.Errorf("lock generation lease: %w", err)
		}
		if _, err := file.Write(body); err != nil {
			_ = unlockLeaseFile(file)
			_ = file.Close()
			_ = os.Remove(path)
			return nil, err
		}
		if err := file.Sync(); err != nil {
			_ = unlockLeaseFile(file)
			_ = file.Close()
			_ = os.Remove(path)
			return nil, err
		}
		return &GenerationLease{
			GenerationID: generationID, path: path, file: file,
		}, nil
	}
	return nil, errors.New("generation lease could not claim an unused record name")
}

// Release drops the lease so retention may reclaim the generation again.
func (lease *GenerationLease) Release() error {
	if lease == nil || lease.file == nil {
		return nil
	}
	unlockErr := unlockLeaseFile(lease.file)
	closeErr := lease.file.Close()
	lease.file = nil
	// A concurrent sweep may already have removed an expired record, so a
	// missing file is a successful release rather than a failure.
	removeErr := os.Remove(lease.path)
	if removeErr != nil && os.IsNotExist(removeErr) {
		removeErr = nil
	}
	if unlockErr != nil {
		return unlockErr
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

// leasedGenerationIDs reports the generations an active measurement run is
// still reading and sweeps records whose holder is gone or whose lease
// expired. The index writer calls it while it holds the writer lock, which is
// exactly where retention decides what it may delete.
func leasedGenerationIDs(cacheRoot string) map[string]bool {
	pinned := map[string]bool{}
	directory, err := generationLeaseDirectory(cacheRoot, false)
	if err != nil {
		return pinned
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return pinned
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasSuffix(entry.Name(), ".lease") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if info, infoErr := entry.Info(); infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		// A record whose lock nobody holds belongs to a run that exited or
		// crashed. An unreadable record is only removed once its holder is
		// gone, so a transient read failure never revokes a live pin.
		exited := generationLeaseHolderExited(path)
		record, readErr := readGenerationLease(path)
		if readErr != nil {
			if exited {
				_ = os.Remove(path)
			}
			continue
		}
		expires, parseErr := time.Parse(time.RFC3339, record.ExpiresAt)
		if exited || parseErr != nil || !now.Before(expires) {
			// Removal fails while a hung holder keeps an expired record open
			// on Windows. The expired lease stops pinning either way.
			_ = os.Remove(path)
			continue
		}
		pinned[record.GenerationID] = true
	}
	return pinned
}

func readGenerationLease(path string) (generationLeaseRecord, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return generationLeaseRecord{}, err
	}
	if len(body) < 1 || len(body) > maxGenerationLeaseBytes {
		return generationLeaseRecord{}, errors.New(
			"generation lease record has an unsafe size")
	}
	var record generationLeaseRecord
	if err := decodeStrict(body, &record); err != nil {
		return generationLeaseRecord{}, err
	}
	if record.SchemaVersion != 1 ||
		!generationIdentityRE.MatchString(record.GenerationID) ||
		!generationLeasePurposeRE.MatchString(record.Purpose) {
		return generationLeaseRecord{}, errors.New(
			"generation lease record is not a supported lease")
	}
	return record, nil
}

// generationLeaseHolderExited reports whether nobody holds the record's lock.
// Both supported lock primitives conflict between open file descriptions, so
// this answer is correct for a holder in this process and for a holder in
// another session's process.
func generationLeaseHolderExited(path string) bool {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		// The holder's identity is unknown, so keep the pin rather than
		// deleting a generation a live run may still be reading.
		return false
	}
	defer file.Close()
	if err := tryLockLeaseFile(file); err != nil {
		return false
	}
	_ = unlockLeaseFile(file)
	return true
}
