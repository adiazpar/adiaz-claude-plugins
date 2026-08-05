package knowledge

// The 0.7-to-0.8 conversion treats the project's git history as the archive
// of record for every document it converts or retires. Converted truth prose,
// campaign masterfiles, and review ledgers are not copied into payload trees
// or backup directories: their exact bytes remain reachable through
// `git show <sourceRevision>:<path>`, and finding provenance cites that
// revision directly. That design has one precondition, enforced at preview
// and re-enforced by plan-digest identity at apply: every managed source must
// be tracked and clean in git, so the recorded source revision provably
// contains the bytes the conversion read.

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// migrationGitEvidencePrefix marks a finding evidence reference whose bytes
// are pinned to the project git archive rather than to a file on disk. The
// object key format is "legacy-truth:git:<revision>:<project-relative path>".
const migrationGitEvidencePrefix = "legacy-truth:git:"

// migrationGitProvenancePrefix marks a truth-receipt provenance path pinned
// to the project git archive: "git:<revision>:<project-relative path>".
const migrationGitProvenancePrefix = "git:"

var migrationGitRevisionRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func runProjectGit(root string, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), detail)
	}
	return stdout.Bytes(), nil
}

// migrationGitRevision resolves the project's current commit. The revision is
// recorded in the migration plan, so approval binds the exact archive commit
// that provenance will cite.
func migrationGitRevision(root string) (string, error) {
	out, err := runProjectGit(root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	revision := strings.TrimSpace(string(out))
	if !migrationGitRevisionRE.MatchString(revision) {
		return "", fmt.Errorf("git rev-parse returned an invalid revision %q", revision)
	}
	return revision, nil
}

// migrationGitStatus lists dirty or untracked entries under the given
// project-relative paths. An empty result means every listed path is tracked
// and byte-identical (modulo git's own EOL normalization) to HEAD.
func migrationGitStatus(root string, paths []string) ([]string, error) {
	args := []string{"status", "--porcelain", "--"}
	args = append(args, paths...)
	out, err := runProjectGit(root, args...)
	if err != nil {
		return nil, err
	}
	entries := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			entries = append(entries, line)
		}
	}
	return entries, nil
}

// migrationGitBlob returns the exact bytes of one tracked file at one
// revision — the runnable provenance recipe `git show <revision>:<path>`.
func migrationGitBlob(root, revision, path string) ([]byte, error) {
	if !migrationGitRevisionRE.MatchString(revision) {
		return nil, fmt.Errorf("invalid git revision %q", revision)
	}
	clean := NormalizeProjectPath(path)
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("invalid git provenance path %q", path)
	}
	return runProjectGit(root, "cat-file", "blob", revision+":"+clean)
}

// normalizeMigrationEOL maps CRLF to LF so working-tree bytes on a
// CRLF-normalizing checkout compare equal to the LF blob git archives.
func normalizeMigrationEOL(body []byte) []byte {
	return bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
}

// migrationGitEvidenceObjectKey renders the archive-pinned evidence object
// key for a converted truth source.
func migrationGitEvidenceObjectKey(revision, path string) string {
	return migrationGitEvidencePrefix + revision + ":" + path
}

// migrationGitProvenanceRef renders the archive-pinned truth-receipt
// provenance path for a converted truth source.
func migrationGitProvenanceRef(revision, path string) string {
	return migrationGitProvenancePrefix + revision + ":" + path
}

// parseMigrationGitRef splits "git:<revision>:<path>" or
// "legacy-truth:git:<revision>:<path>" into revision and path.
func parseMigrationGitRef(ref string) (revision, path string, err error) {
	trimmed := ref
	switch {
	case strings.HasPrefix(trimmed, migrationGitEvidencePrefix):
		trimmed = strings.TrimPrefix(trimmed, migrationGitEvidencePrefix)
	case strings.HasPrefix(trimmed, migrationGitProvenancePrefix):
		trimmed = strings.TrimPrefix(trimmed, migrationGitProvenancePrefix)
	default:
		return "", "", errors.New("not an archive-pinned provenance reference")
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 || !migrationGitRevisionRE.MatchString(parts[0]) || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("malformed archive-pinned provenance reference %q", ref)
	}
	return parts[0], parts[1], nil
}

// migrationGitPinnedEvidence reports whether an evidence reference is pinned
// to the project git archive rather than to a file on disk.
func migrationGitPinnedEvidence(evidence EvidenceReference) bool {
	return strings.HasPrefix(evidence.ObjectKey, migrationGitEvidencePrefix)
}

// resolveMigrationGitEvidence resolves an archive-pinned evidence reference
// to its exact bytes and verifies the recorded digest.
func resolveMigrationGitEvidence(root string, evidence EvidenceReference) ([]byte, error) {
	revision, path, err := parseMigrationGitRef(evidence.ObjectKey)
	if err != nil {
		return nil, err
	}
	if path != evidence.Path {
		return nil, fmt.Errorf("archive-pinned evidence path %q disagrees with its object key %q", evidence.Path, evidence.ObjectKey)
	}
	body, err := migrationGitBlob(root, revision, path)
	if err != nil {
		return nil, err
	}
	if "sha256:"+SHA256Bytes(body) != evidence.SHA256 {
		return nil, fmt.Errorf("archive-pinned evidence %s digest changed", evidence.ObjectKey)
	}
	return body, nil
}

// migrationGitArchiveConflicts verifies the git-archive precondition for a
// planned conversion: the project is a git repository, and every inventoried
// source under a managed target is tracked and clean, so the recorded source
// revision provably contains the bytes the conversion reads. Each violation
// becomes a blocking conflict; none of them can be waived.
func migrationGitArchiveConflicts(root string, sources []MigrationSource, targets []string) ([]MigrationConflict, string) {
	revision, err := migrationGitRevision(root)
	if err != nil {
		return []MigrationConflict{{
			Code: "git-archive-required", Path: ".",
			Message: "the 0.8 conversion cites git as the archive of record for converted documents and requires the project to be a git repository at a resolvable commit: " + err.Error(),
			Blocks:  true,
		}}, ""
	}
	// The status query runs over the managed target roots rather than the
	// individual inventoried sources: it is one bounded git invocation, and it
	// additionally catches an untracked file under a managed root that the
	// inventory excluded but activation would still displace. git status does
	// not fail on a pathspec that matches nothing, so absent optional targets
	// are harmless.
	_ = sources
	conflicts := []MigrationConflict{}
	if len(targets) > 0 {
		entries, statusErr := migrationGitStatus(root, targets)
		if statusErr != nil {
			return []MigrationConflict{{
				Code: "git-archive-required", Path: ".",
				Message: "git status over the managed source inventory failed: " + statusErr.Error(), Blocks: true,
			}}, revision
		}
		for _, entry := range entries {
			conflicts = append(conflicts, MigrationConflict{
				Code: "git-source-unarchived", Path: strings.TrimSpace(entry),
				Message: "managed migration source is dirty or untracked; commit it so the recorded source revision archives the exact bytes the conversion reads",
				Blocks:  true,
			})
		}
	}
	return conflicts, revision
}
