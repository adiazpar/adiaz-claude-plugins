package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrationTruthConflictPacket is the immutable manager handoff for legacy
// truth that cannot be converted into one bounded finding without judgment.
// The packet is derived only from the legacy source snapshot, so its digest
// remains stable while the manager submits reviews one source at a time.
type MigrationTruthConflictPacket struct {
	SchemaVersion     int                      `json:"schemaVersion"`
	Project           string                   `json:"project"`
	ProjectIdentity   string                   `json:"projectIdentity"`
	SourceFingerprint string                   `json:"sourceFingerprint"`
	Conflicts         []MigrationTruthConflict `json:"conflicts"`
	Digest            string                   `json:"digest"`
}

type MigrationTruthConflict struct {
	Code               string   `json:"code"`
	SourcePath         string   `json:"sourcePath"`
	SourceDigest       string   `json:"sourceDigest"`
	Title              string   `json:"title"`
	ExplicitClaims     []string `json:"explicitClaims"`
	SourceCoverageText string   `json:"sourceCoverageText"`
	LegacyDependencies []string `json:"legacyDependencies"`
	RequiredResolution string   `json:"requiredResolution"`
	Digest             string   `json:"digest"`
}

// MigrationTruthAtomicClaim maps an exact, ordered span of the legacy
// explicit-claim text to one independently supersedable finding. SourceText
// rows must partition the complete normalized legacy claim text with no gap,
// overlap, or reordering. Claim is the manager-reviewed atomic statement.
type MigrationTruthAtomicClaim struct {
	SourceText         string   `json:"sourceText"`
	Title              string   `json:"title"`
	Claim              string   `json:"claim"`
	SyntheticQuestions []string `json:"syntheticQuestions"`
}

type MigrationTruthReviewSubmission struct {
	SchemaVersion int                         `json:"schemaVersion"`
	PacketDigest  string                      `json:"packetDigest"`
	SourcePath    string                      `json:"sourcePath"`
	SourceDigest  string                      `json:"sourceDigest"`
	Reviewer      string                      `json:"reviewer"`
	Rationale     string                      `json:"rationale"`
	Claims        []MigrationTruthAtomicClaim `json:"claims"`
}

type MigrationTruthAtomicizationReview struct {
	SchemaVersion  int                         `json:"schemaVersion"`
	PacketDigest   string                      `json:"packetDigest"`
	ConflictDigest string                      `json:"conflictDigest"`
	SourcePath     string                      `json:"sourcePath"`
	SourceDigest   string                      `json:"sourceDigest"`
	Reviewer       string                      `json:"reviewer"`
	Rationale      string                      `json:"rationale"`
	Claims         []MigrationTruthAtomicClaim `json:"claims"`
	Digest         string                      `json:"digest"`
}

// ExportMigrationTruthConflicts is read-only. It exposes every source whose
// explicit accepted-truth claim needs manager atomicization, even if a review
// has already been submitted. That makes the packet stable and lets a manager
// review and submit a large project incrementally.
func ExportMigrationTruthConflicts(projectRoot string) (MigrationTruthConflictPacket, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return MigrationTruthConflictPacket{}, err
	}
	if _, stateErr := os.Lstat(filepath.Join(boundary.Root, ".re-discipline", "migration", "0.8", "state.json")); stateErr == nil {
		return MigrationTruthConflictPacket{}, errors.New("truth conflict export is available only before migration application begins")
	} else if !os.IsNotExist(stateErr) {
		return MigrationTruthConflictPacket{}, stateErr
	}
	version, err := DetectProjectStateVersion(boundary.Root)
	if err != nil {
		return MigrationTruthConflictPacket{}, err
	}
	if version != "0.7" {
		return MigrationTruthConflictPacket{}, fmt.Errorf("truth conflict export requires a 0.7 project, got %s", version)
	}
	sources, _, err := migrationInventory(boundary)
	if err != nil {
		return MigrationTruthConflictPacket{}, err
	}
	profileDigest := "missing"
	for _, source := range sources {
		if source.Path == ".re-discipline/project-profile.md" {
			profileDigest = source.SHA256
			break
		}
	}
	packet := MigrationTruthConflictPacket{
		SchemaVersion: MigrationSchemaVersion,
		Project:       filepath.Base(boundary.Root), ProjectIdentity: profileDigest,
		Conflicts: []MigrationTruthConflict{},
	}
	packet.SourceFingerprint, err = CanonicalDigest(sources)
	if err != nil {
		return MigrationTruthConflictPacket{}, err
	}
	for _, source := range sources {
		if source.Role != "truth" {
			continue
		}
		body, readErr := readMigrationSource(boundary.Root, source)
		if readErr != nil {
			return MigrationTruthConflictPacket{}, readErr
		}
		conflict, required := migrationTruthConflictForSource(source, body)
		if !required {
			continue
		}
		packet.Conflicts = append(packet.Conflicts, conflict)
	}
	sort.Slice(packet.Conflicts, func(i, j int) bool {
		return packet.Conflicts[i].SourcePath < packet.Conflicts[j].SourcePath
	})
	packet.Digest, err = CanonicalDigest(packet)
	if err != nil {
		return MigrationTruthConflictPacket{}, err
	}
	return packet, nil
}

// SubmitMigrationTruthReview writes only a sealed pre-transaction manager
// decision under the excluded migration root. It never changes the legacy
// truth source. A different existing review requires its exact digest, which
// makes correction explicit and prevents last-writer-wins review loss.
func SubmitMigrationTruthReview(
	projectRoot string,
	submission MigrationTruthReviewSubmission,
	expectedPriorDigest string,
) (MigrationTruthAtomicizationReview, error) {
	engine, err := NewMigrationEngine(projectRoot)
	if err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	if err := engine.ensureMigrationDirectory(engine.migrationRoot()); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	operationLock, err := acquireWriterLock(engine.operationLockPath())
	if err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	defer operationLock.Close()
	if _, err := os.Lstat(engine.statePath()); err == nil {
		return MigrationTruthAtomicizationReview{}, errors.New("truth atomicization reviews cannot change after migration application begins")
	} else if !os.IsNotExist(err) {
		return MigrationTruthAtomicizationReview{}, err
	}
	packet, err := ExportMigrationTruthConflicts(engine.ProjectRoot)
	if err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	if submission.SchemaVersion != MigrationSchemaVersion || submission.PacketDigest == "" || submission.PacketDigest != packet.Digest {
		return MigrationTruthAtomicizationReview{}, errors.New("truth review is not bound to the current conflict packet digest")
	}
	var conflict *MigrationTruthConflict
	for index := range packet.Conflicts {
		if packet.Conflicts[index].SourcePath == NormalizeProjectPath(submission.SourcePath) {
			conflict = &packet.Conflicts[index]
			break
		}
	}
	if conflict == nil {
		return MigrationTruthAtomicizationReview{}, errors.New("truth review source is not an unresolved atomicization conflict")
	}
	submission.SourcePath = NormalizeProjectPath(submission.SourcePath)
	submission.Reviewer = strings.TrimSpace(submission.Reviewer)
	submission.Rationale = strings.TrimSpace(submission.Rationale)
	for index := range submission.Claims {
		row := &submission.Claims[index]
		row.SourceText = normalizeTruthCoverageText(row.SourceText)
		row.Title = normalizePreludeField(row.Title)
		row.Claim = normalizePreludeField(row.Claim)
		for questionIndex := range row.SyntheticQuestions {
			row.SyntheticQuestions[questionIndex] = strings.TrimSpace(row.SyntheticQuestions[questionIndex])
		}
		row.SyntheticQuestions = SortedUnique(row.SyntheticQuestions)
	}
	review := MigrationTruthAtomicizationReview{
		SchemaVersion: MigrationSchemaVersion, PacketDigest: packet.Digest,
		ConflictDigest: conflict.Digest, SourcePath: submission.SourcePath,
		SourceDigest: submission.SourceDigest, Reviewer: submission.Reviewer,
		Rationale: submission.Rationale, Claims: submission.Claims,
	}
	if err := validateMigrationTruthReview(review, *conflict); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	review.Digest, err = CanonicalDigest(review)
	if err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	path := migrationTruthReviewPath(engine.ProjectRoot, review.SourcePath)
	if err := engine.ensureMigrationDirectory(filepath.Dir(path)); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	if body, readErr := readSingleLinkRegularFile(path); readErr == nil {
		var prior MigrationTruthAtomicizationReview
		if decodeErr := decodeStrictJSON(body, &prior); decodeErr != nil {
			return MigrationTruthAtomicizationReview{}, fmt.Errorf("existing truth review is invalid: %w", decodeErr)
		}
		if prior.Digest == review.Digest {
			return prior, nil
		}
		if expectedPriorDigest == "" || expectedPriorDigest != prior.Digest {
			return MigrationTruthAtomicizationReview{}, errors.New("replacing a truth review requires its exact prior digest")
		}
		historyPath := migrationTruthReviewHistoryPath(engine.ProjectRoot, review.SourcePath, prior.Digest)
		if err := engine.ensureMigrationDirectory(filepath.Dir(historyPath)); err != nil {
			return MigrationTruthAtomicizationReview{}, err
		}
		if existingHistory, historyErr := readSingleLinkRegularFile(historyPath); historyErr == nil {
			if string(existingHistory) != string(body) {
				return MigrationTruthAtomicizationReview{}, errors.New("archived prior truth review does not match the review being replaced")
			}
		} else if os.IsNotExist(historyErr) {
			if err := AtomicWrite(historyPath, body, 0o600); err != nil {
				return MigrationTruthAtomicizationReview{}, err
			}
		} else {
			return MigrationTruthAtomicizationReview{}, historyErr
		}
	} else if !os.IsNotExist(readErr) {
		return MigrationTruthAtomicizationReview{}, readErr
	} else if expectedPriorDigest != "" {
		return MigrationTruthAtomicizationReview{}, errors.New("expected prior truth review does not exist")
	}
	if err := AtomicWriteJSON(path, review, 0o600); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	return review, nil
}

func validateMigrationTruthReview(review MigrationTruthAtomicizationReview, conflict MigrationTruthConflict) error {
	if review.SchemaVersion != MigrationSchemaVersion || review.PacketDigest == "" ||
		review.ConflictDigest != conflict.Digest || review.SourcePath != conflict.SourcePath ||
		review.SourceDigest != conflict.SourceDigest {
		return errors.New("truth review source identity or conflict digest is stale")
	}
	if review.Reviewer == "" || review.Rationale == "" {
		return errors.New("truth review requires a manager identity and rationale")
	}
	if len(review.Claims) == 0 || len(review.Claims) > 1000 {
		return errors.New("truth review requires between 1 and 1000 atomic claim rows")
	}
	covered := make([]string, 0, len(review.Claims))
	for index, row := range review.Claims {
		if row.SourceText == "" || row.Title == "" || row.Claim == "" {
			return fmt.Errorf("truth review claim %d requires sourceText, title, and claim", index+1)
		}
		if strings.ContainsAny(row.Title, "\r\n") || strings.ContainsAny(row.Claim, "\r\n") || len([]rune(row.Claim)) > 500 {
			return fmt.Errorf("truth review claim %d is not a bounded single-line atomic finding", index+1)
		}
		if len(row.SyntheticQuestions) < SyntheticQuestionMinimum || len(row.SyntheticQuestions) > SyntheticQuestionMaximum ||
			len(SortedUnique(row.SyntheticQuestions)) != len(row.SyntheticQuestions) {
			return fmt.Errorf("truth review claim %d requires %d-%d unique reviewed questions", index+1, SyntheticQuestionMinimum, SyntheticQuestionMaximum)
		}
		for _, question := range row.SyntheticQuestions {
			if strings.ContainsAny(question, "\r\n") || !strings.HasSuffix(strings.TrimSpace(question), "?") {
				return fmt.Errorf("truth review claim %d contains an invalid synthetic question", index+1)
			}
		}
		covered = append(covered, normalizeTruthCoverageText(row.SourceText))
	}
	want := normalizeTruthCoverageText(conflict.SourceCoverageText)
	got := normalizeTruthCoverageText(strings.Join(covered, " "))
	if want == "" || got != want {
		return errors.New("truth review sourceText rows must cover every explicit legacy claim exactly once and in order")
	}
	return nil
}

func loadMigrationTruthReview(projectRoot string, conflict MigrationTruthConflict) (MigrationTruthAtomicizationReview, error) {
	var review MigrationTruthAtomicizationReview
	body, err := readSingleLinkRegularFile(migrationTruthReviewPath(projectRoot, conflict.SourcePath))
	if err != nil {
		return review, err
	}
	if err := decodeStrictJSON(body, &review); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	expected := review.Digest
	review.Digest = ""
	digest, err := CanonicalDigest(review)
	review.Digest = expected
	if err != nil || expected == "" || digest != expected {
		return MigrationTruthAtomicizationReview{}, errors.New("truth atomicization review digest mismatch")
	}
	if err := validateMigrationTruthReview(review, conflict); err != nil {
		return MigrationTruthAtomicizationReview{}, err
	}
	return review, nil
}

func migrationTruthReviewPath(projectRoot, sourcePath string) string {
	key := strings.TrimPrefix(SHA256String("migration-truth-review\x00"+NormalizeProjectPath(sourcePath)), "sha256:")
	return filepath.Join(projectRoot, ".re-discipline", "migration", "0.8", "truth-reviews", key+".json")
}

func migrationTruthReviewHistoryPath(projectRoot, sourcePath, reviewDigest string) string {
	key := strings.TrimSuffix(filepath.Base(migrationTruthReviewPath("", sourcePath)), ".json")
	digest := strings.TrimPrefix(reviewDigest, "sha256:")
	return filepath.Join(projectRoot, ".re-discipline", "migration", "0.8", "truth-reviews", "history", key, digest+".json")
}

func migrationTruthAuditReviewPath(sourcePath string) string {
	return filepath.ToSlash(filepath.Join(".re-discipline", "knowledge", "migration", "truth-reviews", filepath.Base(migrationTruthReviewPath("", sourcePath))))
}

func normalizeTruthCoverageText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\r", "")), " ")
}

func legacyTruthManualReviewReason(body []byte, sourcePath string) (string, string) {
	claims := legacyTruthExplicitClaims(body)
	title := normalizePreludeField(ExtractDocumentPrelude(string(body), sourcePath).Title)
	switch {
	case len(claims) == 0 || title == "":
		return "truth-claim-boundary-unproven", "Provide an exhaustive ordered mapping from explicit accepted-truth text to one or more bounded atomic findings."
	case len(claims) > 1:
		return "truth-multi-claim", "Map every explicit claim, in order, to independently supersedable atomic findings without dropping any source text."
	case len([]rune(claims[0])) > 500:
		return "truth-claim-not-atomic", "Split or rewrite the complete explicit claim into manager-reviewed atomic findings of at most 500 characters."
	default:
		return "", ""
	}
}

func migrationTruthConflictForSource(source MigrationSource, body []byte) (MigrationTruthConflict, bool) {
	code, required := legacyTruthManualReviewReason(body, source.Path)
	if code == "" {
		return MigrationTruthConflict{}, false
	}
	conflict := MigrationTruthConflict{
		Code: code, SourcePath: source.Path, SourceDigest: "sha256:" + source.SHA256,
		Title:              normalizePreludeField(ExtractDocumentPrelude(string(body), source.Path).Title),
		ExplicitClaims:     legacyTruthExplicitClaims(body),
		SourceCoverageText: migrationTruthSourceCoverageText(body),
		LegacyDependencies: legacyTruthDependencyPaths(body, source.Path),
		RequiredResolution: required,
	}
	conflict.Digest, _ = CanonicalDigest(conflict)
	return conflict, true
}

func migrationTruthSourceCoverageText(body []byte) string {
	claims := legacyTruthExplicitClaims(body)
	if len(claims) > 0 {
		return normalizeTruthCoverageText(strings.Join(claims, " "))
	}
	// With no explicit claim marker, the migrator cannot infer which prose is
	// authoritative. Binding the review to the complete normalized document is
	// the conservative alternative: the manager may rewrite or split it, but no
	// source text can disappear outside the reviewed coverage decision.
	return normalizeTruthCoverageText(string(body))
}

func legacyTruthExplicitClaims(body []byte) []string {
	text := strings.ReplaceAll(strings.ReplaceAll(string(body), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	claims := []string{}
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		lower := strings.ToLower(trimmed)
		var parts []string
		switch {
		case strings.HasPrefix(lower, "**claim:**"):
			parts = append(parts, strings.TrimSpace(trimmed[len("**Claim:**"):]))
			for index+1 < len(lines) {
				next := strings.TrimSpace(lines[index+1])
				if next == "" || strings.HasPrefix(strings.ToLower(next), "**") || strings.HasPrefix(next, "#") {
					break
				}
				index++
				parts = append(parts, next)
			}
		case lower == "## claim" || strings.HasPrefix(lower, "## claim "):
			remainder := strings.TrimSpace(trimmed[len("## Claim"):])
			remainder = strings.TrimSpace(strings.TrimLeft(remainder, ":-"))
			if remainder != "" {
				parts = append(parts, remainder)
			}
			for index+1 < len(lines) {
				next := strings.TrimSpace(lines[index+1])
				if strings.HasPrefix(next, "#") || strings.HasPrefix(strings.ToLower(next), "**claim:**") {
					break
				}
				index++
				if next != "" {
					parts = append(parts, next)
				}
			}
		default:
			continue
		}
		claim := normalizePreludeField(strings.Join(parts, " "))
		if claim != "" {
			claims = append(claims, claim)
		}
	}
	return claims
}
