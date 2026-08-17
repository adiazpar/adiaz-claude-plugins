package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	FindingFormatVersion     = "finding-markdown-v2-canonical"
	SyntheticQuestionMinimum = 3
	SyntheticQuestionMaximum = 5
)

var sha256ValueRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// FindingDocument is the canonical Markdown finding plus the reviewed
// doc2query keys that are indexed with it. FindingRecord stays the public
// state-engine record; this wrapper is the retrieval representation.
type FindingDocument struct {
	Record             FindingRecord
	SyntheticQuestions []string
	QuestionsReviewed  bool
}

// ParseFindingDocument accepts only the deterministic YAML subset emitted by
// RenderFindingDocument. Rejecting aliases, tags, implicit coercions, duplicate
// keys, block scalars, and unknown keys is deliberate: two hosts must never
// digest the same finding differently.
func ParseFindingDocument(body []byte, path string) (FindingDocument, error) {
	return parseFindingDocument(body, path, false)
}

// parseMigrationCompatibleFindingDocument accepts the additive 0.7 finding
// format whose sourceRuns/sourceRun values can still name a legacy
// subagents/<workspace> path. It keeps every other canonical syntax, digest,
// and epistemic validation rule. Only the explicit migrator may call it.
func parseMigrationCompatibleFindingDocument(body []byte, path string) (FindingDocument, error) {
	return parseFindingDocument(body, path, true)
}

func parseFindingDocument(body []byte, path string, allowLegacySourceRuns bool) (FindingDocument, error) {
	normalized := string(normalizeNewlines(body))
	if !strings.HasPrefix(normalized, "---\n") {
		return FindingDocument{}, errors.New("finding must begin with YAML frontmatter")
	}
	closing := strings.Index(normalized[4:], "\n---\n")
	if closing < 0 {
		return FindingDocument{}, errors.New("finding frontmatter is not terminated")
	}
	closing += 4
	frontmatter := normalized[4:closing]
	markdown := normalized[closing+5:]
	if strings.Contains(frontmatter, "\t") {
		return FindingDocument{}, errors.New("finding frontmatter uses a forbidden YAML feature")
	}

	values := map[string]string{}
	scope := map[string]any{}
	evidence := []EvidenceReference{}
	section := ""
	seenScope := map[string]bool{}
	seenEvidence := map[string]bool{}
	var currentEvidence *EvidenceReference
	for lineNumber, line := range strings.Split(frontmatter, "\n") {
		if strings.TrimSpace(line) == "" {
			return FindingDocument{}, fmt.Errorf("frontmatter line %d is blank", lineNumber+2)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return FindingDocument{}, fmt.Errorf("frontmatter line %d contains a comment", lineNumber+2)
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch indent {
		case 0:
			currentEvidence = nil
			key, value, err := splitYAMLPair(line)
			if err != nil {
				return FindingDocument{}, fmt.Errorf("frontmatter line %d: %w", lineNumber+2, err)
			}
			if _, duplicate := values[key]; duplicate {
				return FindingDocument{}, fmt.Errorf("duplicate frontmatter key %q", key)
			}
			if key == "scope" || key == "evidence" {
				if value != "" {
					return FindingDocument{}, fmt.Errorf("%s must be a block", key)
				}
				section = key
				values[key] = ""
				continue
			}
			if !findingScalarKeys[key] && !findingListKeys[key] {
				return FindingDocument{}, fmt.Errorf("unknown frontmatter key %q", key)
			}
			section = ""
			values[key] = value
		case 2:
			switch section {
			case "scope":
				key, value, err := splitYAMLPair(strings.TrimSpace(line))
				if err != nil || value == "" {
					return FindingDocument{}, fmt.Errorf("invalid scope entry on line %d", lineNumber+2)
				}
				if seenScope[key] {
					return FindingDocument{}, fmt.Errorf("duplicate scope key %q", key)
				}
				parsed, err := parseScopeValue(value)
				if err != nil {
					return FindingDocument{}, fmt.Errorf("scope %s: %w", key, err)
				}
				scope[key], seenScope[key] = parsed, true
			case "evidence":
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "- ") {
					return FindingDocument{}, fmt.Errorf("evidence line %d must start an item", lineNumber+2)
				}
				evidence = append(evidence, EvidenceReference{})
				currentEvidence = &evidence[len(evidence)-1]
				seenEvidence = map[string]bool{}
				key, value, err := splitYAMLPair(strings.TrimPrefix(trimmed, "- "))
				if err != nil {
					return FindingDocument{}, fmt.Errorf("evidence line %d: %w", lineNumber+2, err)
				}
				if err := assignEvidenceField(currentEvidence, key, value, seenEvidence); err != nil {
					return FindingDocument{}, fmt.Errorf("evidence line %d: %w", lineNumber+2, err)
				}
			default:
				return FindingDocument{}, fmt.Errorf("unexpected indentation on line %d", lineNumber+2)
			}
		case 4:
			if section != "evidence" || currentEvidence == nil {
				return FindingDocument{}, fmt.Errorf("unexpected indentation on line %d", lineNumber+2)
			}
			key, value, err := splitYAMLPair(strings.TrimSpace(line))
			if err != nil {
				return FindingDocument{}, fmt.Errorf("evidence line %d: %w", lineNumber+2, err)
			}
			if err := assignEvidenceField(currentEvidence, key, value, seenEvidence); err != nil {
				return FindingDocument{}, fmt.Errorf("evidence line %d: %w", lineNumber+2, err)
			}
		default:
			return FindingDocument{}, fmt.Errorf("unsupported indentation on line %d", lineNumber+2)
		}
	}

	record, questions, reviewed, err := decodeFindingValues(values, scope, evidence)
	if err != nil {
		return FindingDocument{}, err
	}
	record.Body = strings.TrimSuffix(markdown, "\n")
	record.Path = NormalizeProjectPath(path)
	document := normalizeFindingDocument(FindingDocument{
		Record: record, SyntheticQuestions: questions, QuestionsReviewed: reviewed,
	})
	validationDocument := document
	if allowLegacySourceRuns {
		var err error
		validationDocument, err = migrationCompatibleFindingForValidation(document)
		if err != nil {
			return FindingDocument{}, err
		}
	}
	if err := ValidateFindingDocument(validationDocument, true); err != nil {
		return FindingDocument{}, err
	}
	expected, err := findingDocumentDigest(document)
	if err != nil {
		return FindingDocument{}, err
	}
	if document.Record.Digest != expected {
		return FindingDocument{}, fmt.Errorf("finding digest mismatch: declared %s computed %s", document.Record.Digest, expected)
	}
	canonical, err := renderFindingDocument(document, expected)
	if err != nil {
		return FindingDocument{}, err
	}
	if normalized != string(canonical) {
		return FindingDocument{}, errors.New("finding is valid but not in canonical rendered form")
	}
	return document, nil
}

func migrationCompatibleFindingForValidation(document FindingDocument) (FindingDocument, error) {
	record := document.Record
	record.SourceRuns = append([]string(nil), document.Record.SourceRuns...)
	record.Evidence = append([]EvidenceReference(nil), document.Record.Evidence...)
	legacyRuns := map[string]string{}
	for index, sourceRun := range record.SourceRuns {
		if runIDRE.MatchString(sourceRun) {
			continue
		}
		if !validLegacySourceRunReference(sourceRun) {
			return FindingDocument{}, fmt.Errorf("finding source run %q is neither a canonical run ID nor a legacy subagent reference", sourceRun)
		}
		mapped := fmt.Sprintf("R-19700101-%08d", 90000000+index)
		legacyRuns[sourceRun] = mapped
		record.SourceRuns[index] = mapped
	}
	for index := range record.Evidence {
		sourceRun := record.Evidence[index].SourceRun
		if sourceRun == "" || runIDRE.MatchString(sourceRun) {
			continue
		}
		if !validLegacySourceRunReference(sourceRun) {
			return FindingDocument{}, fmt.Errorf("finding evidence source run %q is neither a canonical run ID nor a legacy subagent reference", sourceRun)
		}
		mapped := legacyRuns[sourceRun]
		if mapped == "" {
			return FindingDocument{}, fmt.Errorf("finding evidence source run %q is absent from sourceRuns", sourceRun)
		}
		record.Evidence[index].SourceRun = mapped
	}
	document.Record = record
	return document, nil
}

func validLegacySourceRunReference(value string) bool {
	clean := NormalizeProjectPath(value)
	if filepath.IsAbs(value) || clean == "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || clean != filepath.ToSlash(value) {
		return false
	}
	parts := strings.Split(clean, "/")
	for index, part := range parts {
		if part == "subagents" && index+1 < len(parts) && strings.TrimSpace(parts[index+1]) != "" {
			return true
		}
	}
	return false
}

var findingScalarKeys = map[string]bool{
	"schemaVersion": true, "id": true, "campaignId": true, "revision": true,
	"createdAt": true, "updatedAt": true, "createdBy": true, "updatedBy": true,
	"digest": true, "correlationId": true, "kind": true, "subject": true,
	"claim": true, "evidenceGrade": true, "reviewState": true, "validity": true,
	"projection": true, "policyId": true, "verifiedAt": true,
	"questionsReviewed": true,
}

var findingListKeys = map[string]bool{
	"appliesWhen": true, "knownLimits": true, "tags": true, "subsystems": true,
	"aliases": true, "sourceRuns": true, "supports": true, "contradicts": true,
	"dependsOn": true, "supersedes": true, "duplicates": true, "answers": true,
	"spawned": true, "syntheticQuestions": true,
}

func splitYAMLPair(line string) (string, string, error) {
	colon := strings.IndexByte(line, ':')
	if colon < 1 {
		return "", "", errors.New("expected key: value")
	}
	key := strings.TrimSpace(line[:colon])
	value := strings.TrimSpace(line[colon+1:])
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`).MatchString(key) {
		return "", "", fmt.Errorf("invalid key %q", key)
	}
	return key, value, nil
}

func parseYAMLString(value string) (string, error) {
	if value == "" {
		return "", errors.New("empty scalar")
	}
	if strings.HasPrefix(value, "\"") {
		var result string
		if err := json.Unmarshal([]byte(value), &result); err != nil {
			return "", errors.New("invalid quoted scalar")
		}
		return result, nil
	}
	if strings.ContainsAny(value, "[]{}#,:\"'") || strings.TrimSpace(value) != value {
		return "", errors.New("ambiguous scalar must be JSON quoted")
	}
	return value, nil
}

func parseInlineStrings(value string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, errors.New("list must be a JSON-compatible YAML string array")
	}
	for _, item := range result {
		if strings.TrimSpace(item) == "" || strings.ContainsAny(item, "\r\n") {
			return nil, errors.New("list values must be non-empty single-line strings")
		}
	}
	return result, nil
}

func parseScopeValue(value string) (any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil, errors.New("scope values must use explicit JSON-compatible YAML scalars")
	}
	switch typed := parsed.(type) {
	case string, bool, float64:
		return typed, nil
	case []any:
		for _, value := range typed {
			if _, ok := value.(string); !ok {
				return nil, errors.New("scope arrays may contain strings only")
			}
		}
		return typed, nil
	default:
		return nil, errors.New("scope values may be strings, numbers, booleans, or string arrays")
	}
}

func assignEvidenceField(target *EvidenceReference, key, value string, seen map[string]bool) error {
	if seen[key] {
		return fmt.Errorf("duplicate evidence key %q", key)
	}
	seen[key] = true
	switch key {
	case "path":
		parsed, err := parseYAMLString(value)
		target.Path = parsed
		return err
	case "sha256":
		parsed, err := parseYAMLString(value)
		target.SHA256 = parsed
		return err
	case "startLine", "endLine":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return fmt.Errorf("%s must be a positive integer", key)
		}
		if key == "startLine" {
			target.StartLine = parsed
		} else {
			target.EndLine = parsed
		}
		return nil
	case "objectKey":
		parsed, err := parseYAMLString(value)
		target.ObjectKey = parsed
		return err
	case "sourceRun":
		parsed, err := parseYAMLString(value)
		target.SourceRun = parsed
		return err
	default:
		return fmt.Errorf("unknown evidence key %q", key)
	}
}

func decodeFindingValues(values map[string]string, scope map[string]any, evidence []EvidenceReference) (FindingRecord, []string, bool, error) {
	required := []string{"schemaVersion", "id", "campaignId", "revision", "createdAt", "updatedAt", "createdBy", "updatedBy", "digest", "correlationId", "kind", "subject", "claim", "scope", "sourceRuns", "evidence", "evidenceGrade", "reviewState", "validity", "projection", "syntheticQuestions", "questionsReviewed"}
	for _, key := range required {
		if _, present := values[key]; !present {
			return FindingRecord{}, nil, false, fmt.Errorf("required frontmatter key %q is missing", key)
		}
	}
	integer := func(key string) (int64, error) {
		value, err := strconv.ParseInt(values[key], 10, 64)
		if err != nil || value < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", key)
		}
		return value, nil
	}
	scalar := func(key string) (string, error) { return parseYAMLString(values[key]) }
	list := func(key string) ([]string, error) {
		value, present := values[key]
		if !present {
			return nil, nil
		}
		return parseInlineStrings(value)
	}
	schema, err := integer("schemaVersion")
	if err != nil {
		return FindingRecord{}, nil, false, err
	}
	revision, err := integer("revision")
	if err != nil {
		return FindingRecord{}, nil, false, err
	}
	get := func(key string) string {
		value, _ := scalar(key)
		return value
	}
	record := FindingRecord{
		SchemaVersion: int(schema), ID: get("id"), CampaignID: get("campaignId"),
		Revision: revision, CreatedAt: get("createdAt"), UpdatedAt: get("updatedAt"),
		CreatedBy: get("createdBy"), UpdatedBy: get("updatedBy"), Digest: get("digest"),
		CorrelationID: get("correlationId"), Kind: get("kind"), Subject: get("subject"),
		Claim: get("claim"), Scope: scope, Evidence: evidence,
		EvidenceGrade: get("evidenceGrade"), ReviewState: get("reviewState"),
		Validity: get("validity"), Projection: get("projection"),
	}
	for _, key := range []string{"id", "campaignId", "createdAt", "updatedAt", "createdBy", "updatedBy", "digest", "correlationId", "kind", "subject", "claim", "evidenceGrade", "reviewState", "validity", "projection"} {
		if _, err := scalar(key); err != nil {
			return FindingRecord{}, nil, false, fmt.Errorf("%s: %w", key, err)
		}
	}
	if value, present := values["policyId"]; present {
		record.PolicyID, err = parseYAMLString(value)
		if err != nil {
			return FindingRecord{}, nil, false, fmt.Errorf("policyId: %w", err)
		}
	}
	if value, present := values["verifiedAt"]; present {
		record.VerifiedAt, err = parseYAMLString(value)
		if err != nil {
			return FindingRecord{}, nil, false, fmt.Errorf("verifiedAt: %w", err)
		}
	}
	if record.AppliesWhen, err = list("appliesWhen"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.KnownLimits, err = list("knownLimits"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Tags, err = list("tags"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Subsystems, err = list("subsystems"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Aliases, err = list("aliases"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.SourceRuns, err = list("sourceRuns"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Supports, err = list("supports"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Contradicts, err = list("contradicts"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.DependsOn, err = list("dependsOn"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Supersedes, err = list("supersedes"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Duplicates, err = list("duplicates"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Answers, err = list("answers"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	if record.Relations.Spawned, err = list("spawned"); err != nil {
		return FindingRecord{}, nil, false, err
	}
	questions, err := list("syntheticQuestions")
	if err != nil {
		return FindingRecord{}, nil, false, err
	}
	reviewed, err := strconv.ParseBool(values["questionsReviewed"])
	if err != nil {
		return FindingRecord{}, nil, false, errors.New("questionsReviewed must be true or false")
	}
	return record, questions, reviewed, nil
}

// RenderFindingDocument canonicalizes lists and evidence, computes the record
// digest with the digest field empty, and emits deterministic UTF-8 Markdown.
func RenderFindingDocument(document FindingDocument) ([]byte, error) {
	document = normalizeFindingDocument(document)
	if err := ValidateFindingDocument(document, false); err != nil {
		return nil, err
	}
	digest, err := findingDocumentDigest(document)
	if err != nil {
		return nil, err
	}
	return renderFindingDocument(document, digest)
}

func renderFindingDocument(document FindingDocument, digest string) ([]byte, error) {
	record := document.Record
	record.Digest = digest
	var builder strings.Builder
	writeScalar := func(key string, value any) {
		encoded, _ := json.Marshal(value)
		builder.WriteString(key + ": " + string(encoded) + "\n")
	}
	writeList := func(key string, values []string) {
		encoded, _ := json.Marshal(values)
		builder.WriteString(key + ": " + string(encoded) + "\n")
	}
	builder.WriteString("---\n")
	builder.WriteString(fmt.Sprintf("schemaVersion: %d\n", record.SchemaVersion))
	writeScalar("id", record.ID)
	writeScalar("campaignId", record.CampaignID)
	builder.WriteString(fmt.Sprintf("revision: %d\n", record.Revision))
	writeScalar("createdAt", record.CreatedAt)
	writeScalar("updatedAt", record.UpdatedAt)
	writeScalar("createdBy", record.CreatedBy)
	writeScalar("updatedBy", record.UpdatedBy)
	writeScalar("digest", record.Digest)
	writeScalar("correlationId", record.CorrelationID)
	writeScalar("kind", record.Kind)
	writeScalar("subject", record.Subject)
	writeScalar("claim", record.Claim)
	builder.WriteString("scope:\n")
	scopeKeys := make([]string, 0, len(record.Scope))
	for key := range record.Scope {
		scopeKeys = append(scopeKeys, key)
	}
	sort.Strings(scopeKeys)
	for _, key := range scopeKeys {
		encoded, err := json.Marshal(record.Scope[key])
		if err != nil {
			return nil, fmt.Errorf("scope %s: %w", key, err)
		}
		builder.WriteString("  " + key + ": " + string(encoded) + "\n")
	}
	writeList("appliesWhen", record.AppliesWhen)
	writeList("knownLimits", record.KnownLimits)
	writeList("tags", record.Tags)
	writeList("subsystems", record.Subsystems)
	writeList("aliases", record.Aliases)
	writeList("sourceRuns", record.SourceRuns)
	builder.WriteString("evidence:\n")
	for _, item := range record.Evidence {
		encoded, _ := json.Marshal(item.Path)
		builder.WriteString("  - path: " + string(encoded) + "\n")
		encoded, _ = json.Marshal(item.SHA256)
		builder.WriteString("    sha256: " + string(encoded) + "\n")
		if item.StartLine > 0 {
			builder.WriteString(fmt.Sprintf("    startLine: %d\n", item.StartLine))
			builder.WriteString(fmt.Sprintf("    endLine: %d\n", item.EndLine))
		}
		if item.ObjectKey != "" {
			encoded, _ = json.Marshal(item.ObjectKey)
			builder.WriteString("    objectKey: " + string(encoded) + "\n")
		}
		if item.SourceRun != "" {
			encoded, _ = json.Marshal(item.SourceRun)
			builder.WriteString("    sourceRun: " + string(encoded) + "\n")
		}
	}
	writeList("supports", record.Relations.Supports)
	writeList("contradicts", record.Relations.Contradicts)
	writeList("dependsOn", record.Relations.DependsOn)
	writeList("supersedes", record.Relations.Supersedes)
	writeList("duplicates", record.Relations.Duplicates)
	writeList("answers", record.Relations.Answers)
	writeList("spawned", record.Relations.Spawned)
	writeScalar("evidenceGrade", record.EvidenceGrade)
	writeScalar("reviewState", record.ReviewState)
	writeScalar("validity", record.Validity)
	writeScalar("projection", record.Projection)
	if record.PolicyID != "" {
		writeScalar("policyId", record.PolicyID)
	}
	if record.VerifiedAt != "" {
		writeScalar("verifiedAt", record.VerifiedAt)
	}
	writeList("syntheticQuestions", document.SyntheticQuestions)
	builder.WriteString(fmt.Sprintf("questionsReviewed: %t\n", document.QuestionsReviewed))
	builder.WriteString("---\n")
	builder.WriteString(strings.TrimSuffix(record.Body, "\n"))
	builder.WriteByte('\n')
	return []byte(builder.String()), nil
}

func normalizeFindingDocument(document FindingDocument) FindingDocument {
	record := document.Record
	record.Path = NormalizeProjectPath(record.Path)
	record.Body = strings.TrimSuffix(string(normalizeNewlines([]byte(record.Body))), "\n")
	record.AppliesWhen = SortedUnique(record.AppliesWhen)
	record.KnownLimits = SortedUnique(record.KnownLimits)
	record.Tags = SortedUnique(record.Tags)
	record.Subsystems = SortedUnique(record.Subsystems)
	record.Aliases = SortedUnique(record.Aliases)
	record.SourceRuns = SortedUnique(record.SourceRuns)
	record.Relations.Supports = SortedUnique(record.Relations.Supports)
	record.Relations.Contradicts = SortedUnique(record.Relations.Contradicts)
	record.Relations.DependsOn = SortedUnique(record.Relations.DependsOn)
	record.Relations.Supersedes = SortedUnique(record.Relations.Supersedes)
	record.Relations.Duplicates = SortedUnique(record.Relations.Duplicates)
	record.Relations.Answers = SortedUnique(record.Relations.Answers)
	record.Relations.Spawned = SortedUnique(record.Relations.Spawned)
	sort.Slice(record.Evidence, func(i, j int) bool {
		return EvidenceHandle(record.ID, record.Evidence[i]) < EvidenceHandle(record.ID, record.Evidence[j])
	})
	document.Record = record
	document.SyntheticQuestions = SortedUnique(document.SyntheticQuestions)
	return document
}

func findingDocumentDigest(document FindingDocument) (string, error) {
	document = normalizeFindingDocument(document)
	record := document.Record
	record.Digest = ""
	record.Path = ""
	record.Body = ""
	payload := struct {
		Format             string        `json:"format"`
		Record             FindingRecord `json:"record"`
		Body               string        `json:"body"`
		SyntheticQuestions []string      `json:"syntheticQuestions"`
		QuestionsReviewed  bool          `json:"questionsReviewed"`
	}{FindingFormatVersion, record, document.Record.Body, document.SyntheticQuestions, document.QuestionsReviewed}
	return CanonicalDigest(payload)
}

func ValidateFindingDocument(document FindingDocument, requireDigest bool) error {
	record := document.Record
	if !requireDigest && record.Digest == "" {
		record.Digest = "sha256:" + strings.Repeat("0", 64)
	}
	if err := ValidateFinding(record); err != nil {
		return err
	}
	for label, value := range map[string]string{
		"createdAt": record.CreatedAt, "updatedAt": record.UpdatedAt,
	} {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || parsed.Location() != time.UTC {
			return fmt.Errorf("%s must be a UTC RFC3339 timestamp", label)
		}
	}
	if record.CreatedBy == "" || record.UpdatedBy == "" || record.CorrelationID == "" {
		return errors.New("finding actors and correlationId are required")
	}
	if requireDigest && !sha256ValueRE.MatchString(record.Digest) {
		return errors.New("finding digest must be a lowercase sha256 value")
	}
	if strings.ContainsAny(record.Claim, "\r\n") || len([]rune(record.Claim)) > 500 {
		return errors.New("finding must contain one bounded single-line atomic claim")
	}
	if len(record.Scope) == 0 {
		return errors.New("finding scope is required")
	}
	if !validOne(record.Projection, "none", "campaign", "truth", "history", "backlog", "playbook", "maintained", "archive", "rejected") {
		return fmt.Errorf("unsupported finding projection %q", record.Projection)
	}
	if record.VerifiedAt != "" {
		if _, dateErr := time.Parse("2006-01-02", record.VerifiedAt); dateErr != nil {
			parsed, timestampErr := time.Parse(time.RFC3339Nano, record.VerifiedAt)
			if timestampErr != nil || parsed.Location() != time.UTC {
				return errors.New("verifiedAt must use YYYY-MM-DD or a UTC RFC3339 timestamp")
			}
		}
	}
	for _, evidence := range record.Evidence {
		if !sha256ValueRE.MatchString(evidence.SHA256) {
			return fmt.Errorf("evidence %q has an invalid digest", evidence.Path)
		}
		clean := NormalizeProjectPath(evidence.Path)
		if filepath.IsAbs(evidence.Path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("evidence path %q escapes the project", evidence.Path)
		}
		if clean != evidence.Path {
			return fmt.Errorf("evidence path %q is not canonical", evidence.Path)
		}
		if (evidence.StartLine == 0) != (evidence.EndLine == 0) ||
			(evidence.StartLine > 0 && evidence.EndLine < evidence.StartLine) {
			return fmt.Errorf("evidence %q has an invalid line range", evidence.Path)
		}
		if evidence.SourceRun != "" && !runIDRE.MatchString(evidence.SourceRun) {
			return fmt.Errorf("evidence %q has an invalid source run", evidence.Path)
		}
	}
	for _, relation := range findingRelationIDs(record.Relations) {
		if !findingIDRE.MatchString(relation) || relation == record.ID {
			return fmt.Errorf("finding relation target %q is invalid", relation)
		}
	}
	for _, workItemID := range record.Relations.Spawned {
		if !workItemIDRE.MatchString(workItemID) {
			return fmt.Errorf("spawned work item target %q is invalid", workItemID)
		}
	}
	if !document.QuestionsReviewed || len(document.SyntheticQuestions) < SyntheticQuestionMinimum ||
		len(document.SyntheticQuestions) > SyntheticQuestionMaximum {
		return fmt.Errorf("finding requires %d-%d reviewed synthetic questions", SyntheticQuestionMinimum, SyntheticQuestionMaximum)
	}
	for _, question := range document.SyntheticQuestions {
		if !strings.HasSuffix(strings.TrimSpace(question), "?") || strings.ContainsAny(question, "\r\n") {
			return errors.New("synthetic questions must be single-line questions")
		}
	}
	if err := validateFindingBodySections(record.Body); err != nil {
		return err
	}
	if base := strings.TrimSuffix(filepath.Base(record.Path), filepath.Ext(record.Path)); record.Path != "" && base != record.ID {
		return fmt.Errorf("finding id %s does not match path %s", record.ID, record.Path)
	}
	return nil
}

func validateFindingBodySections(body string) error {
	requiredHeadings := []string{"# Claim", "## Applies when", "## Does not establish", "## Evidence", "## Reproduction", "## Relations"}
	bodyLines := strings.Split(body, "\n")
	position := -1
	for _, heading := range requiredHeadings {
		next := -1
		for index := position + 1; index < len(bodyLines); index++ {
			if bodyLines[index] == heading {
				next = index
				break
			}
		}
		if next < 0 {
			return fmt.Errorf("finding body is missing stable section %q", heading)
		}
		position = next
	}
	return nil
}

func findingRelationIDs(relations FindingRelations) []string {
	values := []string{}
	values = append(values, relations.Supports...)
	values = append(values, relations.Contradicts...)
	values = append(values, relations.DependsOn...)
	values = append(values, relations.Supersedes...)
	values = append(values, relations.Duplicates...)
	values = append(values, relations.Answers...)
	return values
}

// EvidenceHandle is stable across index rebuilds and changes only when the
// canonical evidence target changes.
func EvidenceHandle(findingID string, evidence EvidenceReference) string {
	digest, _ := CanonicalDigest(struct {
		FindingID string
		Evidence  EvidenceReference
	}{findingID, evidence})
	return "evidence:" + findingID + ":" + strings.TrimPrefix(digest, "sha256:")[:20]
}

func FindingHandle(id string) string {
	return "finding:" + id
}

// findingStorageKey is the derived index identity for a campaign-local
// finding. Canonical records and public handles intentionally retain their
// compact F-* IDs; callers disambiguate those handles with campaignId. The
// SQLite projection must not collapse equal local IDs from unrelated
// campaigns, so every internal relation uses this compound key instead.
func findingStorageKey(campaignID, findingID string) string {
	return campaignID + "/" + findingID
}

func findingIDFromStorageKey(key string) string {
	if separator := strings.LastIndexByte(key, '/'); separator >= 0 && separator+1 < len(key) {
		return key[separator+1:]
	}
	return key
}

func FindingSourceClass(record FindingRecord) string {
	switch record.ReviewState {
	case "manager-ratified", "manager-rejected":
		return "campaign"
	default:
		return "provisional"
	}
}

func FindingSourceClassAtPath(record FindingRecord, sourcePath string) string {
	class := FindingSourceClass(record)
	clean := NormalizeProjectPath(sourcePath)
	if strings.HasPrefix(clean, "docs/truth/findings/") {
		return "truth"
	}
	if class == "campaign" && strings.HasPrefix(clean, "docs/history/campaigns/") {
		return "history"
	}
	return class
}

func FindingRelationSets(relations FindingRelations) map[string][]string {
	return map[string][]string{
		"supports":    relations.Supports,
		"contradicts": relations.Contradicts,
		"depends-on":  relations.DependsOn,
		"supersedes":  relations.Supersedes,
		"duplicates":  relations.Duplicates,
		"answers":     relations.Answers,
		"spawned":     relations.Spawned,
	}
}
