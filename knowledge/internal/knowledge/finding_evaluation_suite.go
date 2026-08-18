package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const FindingEvalSuiteVersion = 1

// FindingEvalSuite is a ratified, loadable finding-card judgment set. The
// split, role, epistemic-state, exact-handle, and paired raw-report judgments
// are part of the signed suite identity rather than test-only options.
type FindingEvalSuite struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	RatifiedAt     string            `json:"ratifiedAt"`
	RatifiedBy     string            `json:"ratifiedBy"`
	CorpusSnapshot string            `json:"corpusSnapshot"`
	Cases          []FindingEvalCase `json:"cases"`
	Digest         string            `json:"digest"`
}

func FindingEvalSuiteDigest(suite FindingEvalSuite) (string, error) {
	suite.Digest = ""
	return CanonicalDigest(suite)
}

func LoadFindingEvalSuite(path string) (FindingEvalSuite, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FindingEvalSuite{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxSourceBytes {
		return FindingEvalSuite{}, errors.New("finding evaluation suite has unsafe type or size")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return FindingEvalSuite{}, err
	}
	var suite FindingEvalSuite
	if err := decodeStrict(body, &suite); err != nil {
		return FindingEvalSuite{}, err
	}
	if err := ValidateFindingEvalSuite(suite); err != nil {
		return FindingEvalSuite{}, err
	}
	digest, err := FindingEvalSuiteDigest(suite)
	if err != nil {
		return FindingEvalSuite{}, err
	}
	if suite.Digest != digest {
		return FindingEvalSuite{}, errors.New("finding evaluation suite digest mismatch")
	}
	return suite, nil
}

func (service *Service) loadProjectFindingEvalSuites() ([]FindingEvalSuite, error) {
	root := filepath.Join(
		service.Boundary.Root, ".re-discipline", "knowledge", "evals", "findings")
	canonicalRoot, err := canonicalExistingPath(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !withinRoot(service.Boundary.Root, canonicalRoot) {
		return nil, errors.New("project finding-evaluation root escapes the project boundary")
	}
	paths := []string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("finding evaluation corpus contains a symbolic link: %s", path)
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return nil
		}
		resolved, err := canonicalExistingPath(path)
		if err != nil {
			return err
		}
		if !withinRoot(canonicalRoot, resolved) {
			return errors.New("finding evaluation suite escapes its evaluation root")
		}
		paths = append(paths, resolved)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	suites := make([]FindingEvalSuite, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		suite, err := LoadFindingEvalSuite(path)
		if err != nil {
			return nil, err
		}
		if seen[suite.ID] {
			return nil, fmt.Errorf("finding evaluation suite id %s is repeated", suite.ID)
		}
		seen[suite.ID] = true
		suites = append(suites, suite)
	}
	return suites, nil
}

func ValidateFindingEvalSuite(suite FindingEvalSuite) error {
	if suite.SchemaVersion != FindingEvalSuiteVersion ||
		!managedSlugRE.MatchString(suite.ID) || suite.Status != "ratified" ||
		strings.TrimSpace(suite.RatifiedBy) == "" {
		return errors.New("finding evaluation suite identity or ratification is invalid")
	}
	ratifiedAt, err := time.Parse(time.RFC3339Nano, suite.RatifiedAt)
	if err != nil || ratifiedAt.Location() != time.UTC {
		return errors.New("finding evaluation suite ratifiedAt must be UTC RFC3339")
	}
	if !strings.HasPrefix(suite.CorpusSnapshot, "fixture:") &&
		!sha256ValueRE.MatchString(suite.CorpusSnapshot) {
		return errors.New("finding evaluation suite corpusSnapshot is invalid")
	}
	if !sha256ValueRE.MatchString(suite.Digest) {
		return errors.New("finding evaluation suite digest is invalid")
	}
	if err := ValidateFindingEvalCases(suite.Cases); err != nil {
		return err
	}

	type splitCounts struct {
		cases, manager, drafter, abstention int
	}
	counts := map[string]*splitCounts{
		"development": {}, "holdout": {},
	}
	targetSplit := map[string]string{}
	answerable, withHardNegative := 0, 0
	sourceClasses, reviewStates, validities := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, eval := range suite.Cases {
		row := counts[eval.Split]
		row.cases++
		if eval.Role == "manager" {
			row.manager++
		} else {
			row.drafter++
		}
		if !eval.Answerable {
			row.abstention++
		} else {
			answerable++
			if len(eval.HardNegativeFindingIDs) > 0 {
				withHardNegative++
			}
			if len(eval.ExpectedRawPaths) == 0 {
				return fmt.Errorf("case %s lacks its paired raw-report judgment", eval.ID)
			}
		}
		for _, id := range eval.ExpectedFindingIDs {
			if prior := targetSplit[id]; prior != "" && prior != eval.Split {
				return fmt.Errorf("finding %s leaks across development and holdout", id)
			}
			targetSplit[id] = eval.Split
			sourceClasses[eval.ExpectedSourceClasses[id]] = true
			reviewStates[eval.ExpectedReviewStates[id]] = true
			validities[eval.ExpectedValidities[id]] = true
		}
	}
	for split, row := range counts {
		if row.cases < 24 || row.manager < 6 || row.drafter < 6 || row.abstention < 4 {
			return fmt.Errorf(
				"finding evaluation %s split is too small or taxonomically narrow: cases=%d manager=%d drafter=%d abstention=%d",
				split, row.cases, row.manager, row.drafter, row.abstention,
			)
		}
	}
	if answerable == 0 || float64(withHardNegative)/float64(answerable) < 0.75 {
		return errors.New("finding evaluation hard-negative coverage is below 75 percent")
	}
	for _, required := range []string{"truth", "campaign", "provisional"} {
		if !sourceClasses[required] {
			return fmt.Errorf("finding evaluation suite does not exercise %s source cards", required)
		}
	}
	for _, required := range []string{"manager-ratified", "curator-checked"} {
		if !reviewStates[required] {
			return fmt.Errorf("finding evaluation suite does not exercise %s review state", required)
		}
	}
	for _, required := range []string{"current", "challenged", "superseded"} {
		if !validities[required] {
			return fmt.Errorf("finding evaluation suite does not exercise %s validity", required)
		}
	}
	return nil
}

func ValidateFindingEvalCases(cases []FindingEvalCase) error {
	if len(cases) == 0 || len(cases) > 10000 {
		return errors.New("finding evaluation case set is empty or too large")
	}
	seenIDs, seenQueries := map[string]bool{}, map[string]string{}
	topicSplits := map[string]string{}
	type splitOwner struct {
		split  string
		caseID string
		label  string
	}
	judgmentSplits := map[string]splitOwner{}
	for _, eval := range cases {
		queryIdentity := evaluationQueryIdentity(eval.Query)
		if !managedSlugRE.MatchString(eval.ID) || seenIDs[eval.ID] ||
			strings.TrimSpace(eval.Query) == "" || len([]byte(eval.Query)) > 1000 {
			return errors.New("finding evaluation IDs and queries must be unique and nonempty")
		}
		if prior := seenQueries[queryIdentity]; prior != "" {
			return fmt.Errorf(
				"finding evaluation query is repeated by cases %s and %s", prior, eval.ID)
		}
		seenIDs[eval.ID] = true
		seenQueries[queryIdentity] = eval.ID
		if !validOne(eval.Role, "manager", "drafter") ||
			!managedSlugRE.MatchString(eval.Topic) ||
			!validOne(eval.Split, "development", "holdout") {
			return fmt.Errorf("case %s has invalid role, topic, or split", eval.ID)
		}
		if prior := topicSplits[eval.Topic]; prior != "" && prior != eval.Split {
			return fmt.Errorf("finding evaluation topic %q leaks across splits", eval.Topic)
		}
		topicSplits[eval.Topic] = eval.Split
		if !validOne(eval.QueryClass, "auto", "exact", "conceptual", "orientation", "current", "provenance", "dependency", "contradiction") {
			return fmt.Errorf("case %s has invalid query class", eval.ID)
		}
		if eval.TokenBudget < 128 || eval.TokenBudget > 4096 {
			return fmt.Errorf("case %s has invalid token budget", eval.ID)
		}
		if err := validateFindingEvalFilter(eval.ID, "source class", eval.AllowedSourceClasses,
			"truth", "campaign", "provisional", "history", "archive"); err != nil {
			return err
		}
		if err := validateFindingEvalFilter(eval.ID, "review state", eval.AllowedReviewStates,
			"extracted", "curator-checked", "manager-ratified", "manager-rejected"); err != nil {
			return err
		}
		if err := validateFindingEvalFilter(eval.ID, "validity", eval.AllowedValidities,
			"provisional", "current", "challenged", "historical", "superseded", "invalid"); err != nil {
			return err
		}
		if len(eval.ExpectedFindingIDs) != len(SortedUnique(eval.ExpectedFindingIDs)) ||
			len(eval.HardNegativeFindingIDs) != len(SortedUnique(eval.HardNegativeFindingIDs)) ||
			len(eval.ExpectedFindingHandles) != len(SortedUnique(eval.ExpectedFindingHandles)) ||
			len(eval.ExpectedEvidenceHandles) != len(SortedUnique(eval.ExpectedEvidenceHandles)) ||
			len(eval.ExpectedRawPaths) != len(SortedUnique(eval.ExpectedRawPaths)) {
			return fmt.Errorf("case %s repeats a judgment", eval.ID)
		}
		expected := map[string]bool{}
		for _, id := range eval.ExpectedFindingIDs {
			if !findingIDRE.MatchString(id) {
				return fmt.Errorf("case %s has invalid expected finding %q", eval.ID, id)
			}
			expected[id] = true
			if !contains(eval.ExpectedFindingHandles, FindingHandle(id)) {
				return fmt.Errorf("case %s lacks exact handle for %s", eval.ID, id)
			}
			if !validOne(eval.ExpectedSourceClasses[id], "truth", "campaign", "provisional", "history") ||
				!validOne(eval.ExpectedReviewStates[id], "extracted", "curator-checked", "manager-ratified", "manager-rejected") ||
				!validOne(eval.ExpectedValidities[id], "provisional", "current", "challenged", "historical", "superseded", "invalid") {
				return fmt.Errorf("case %s lacks exact state judgments for %s", eval.ID, id)
			}
		}
		claimJudgment := func(key, label string) error {
			if previous, exists := judgmentSplits[key]; exists && previous.split != eval.Split {
				return fmt.Errorf(
					"finding evaluation judgment %q leaks across %s case %s (%s) and %s case %s (%s)",
					strings.TrimPrefix(strings.TrimPrefix(key, "finding:"), "evidence:"),
					previous.split, previous.caseID, previous.label,
					eval.Split, eval.ID, label)
			}
			if _, exists := judgmentSplits[key]; !exists {
				judgmentSplits[key] = splitOwner{
					split: eval.Split, caseID: eval.ID, label: label,
				}
			}
			return nil
		}
		for _, id := range eval.ExpectedFindingIDs {
			if err := claimJudgment("finding:"+id, "expected"); err != nil {
				return err
			}
		}
		for _, id := range eval.HardNegativeFindingIDs {
			if err := claimJudgment("finding:"+id, "hard-negative"); err != nil {
				return err
			}
		}
		for _, handle := range eval.ExpectedEvidenceHandles {
			if err := claimJudgment("evidence:"+handle, "evidence"); err != nil {
				return err
			}
		}
		for _, path := range eval.ExpectedRawPaths {
			if err := claimJudgment("raw:"+path, "raw-path"); err != nil {
				return err
			}
		}
		if len(eval.ExpectedFindingHandles) != len(eval.ExpectedFindingIDs) ||
			len(eval.ExpectedSourceClasses) != len(eval.ExpectedFindingIDs) ||
			len(eval.ExpectedReviewStates) != len(eval.ExpectedFindingIDs) ||
			len(eval.ExpectedValidities) != len(eval.ExpectedFindingIDs) {
			return fmt.Errorf("case %s has extraneous or incomplete finding judgments", eval.ID)
		}
		for _, id := range eval.HardNegativeFindingIDs {
			if !findingIDRE.MatchString(id) || expected[id] {
				return fmt.Errorf("case %s has invalid or relevant hard negative %q", eval.ID, id)
			}
		}
		for _, handle := range eval.ExpectedEvidenceHandles {
			parts := strings.Split(handle, ":")
			if len(parts) != 3 || parts[0] != "evidence" ||
				!findingIDRE.MatchString(parts[1]) || len(parts[2]) != 20 {
				return fmt.Errorf("case %s has invalid evidence handle", eval.ID)
			}
		}
		for _, path := range eval.ExpectedRawPaths {
			if err := validateEvalPath(path); err != nil {
				return fmt.Errorf("case %s has invalid raw path: %w", eval.ID, err)
			}
		}
		if eval.Answerable != (len(eval.ExpectedFindingIDs)+len(eval.ExpectedRawPaths) > 0) {
			return fmt.Errorf("case %s answerability contradicts its judgments", eval.ID)
		}
	}
	return nil
}

func validateFindingEvalFilter(caseID, label string, values []string, allowed ...string) error {
	if len(values) == 0 || len(values) != len(SortedUnique(values)) {
		return fmt.Errorf("case %s has an empty or repeated %s filter", caseID, label)
	}
	for _, value := range values {
		if !validOne(value, allowed...) {
			return fmt.Errorf("case %s has invalid %s %q", caseID, label, value)
		}
	}
	return nil
}

func (eval FindingEvalCase) queryOptions() FindingQueryOptions {
	options := eval.Options
	options.Query = eval.Query
	options.QueryClass = eval.QueryClass
	if len(eval.AllowedSourceClasses) > 0 {
		options.AllowedSourceClasses = append([]string(nil), eval.AllowedSourceClasses...)
	}
	if len(eval.AllowedReviewStates) > 0 {
		options.AllowedReviewStates = append([]string(nil), eval.AllowedReviewStates...)
	}
	if len(eval.AllowedValidities) > 0 {
		options.AllowedValidities = append([]string(nil), eval.AllowedValidities...)
	}
	if eval.TokenBudget > 0 {
		options.TokenBudget = eval.TokenBudget
	}
	if options.TokenBudget == 0 {
		options.TokenBudget = 4096
	}
	if options.Limit == 0 {
		options.Limit = 5
	}
	return options
}

func EvaluateFindingSuite(
	ctx context.Context,
	retriever Retriever,
	suite FindingEvalSuite,
) (FindingAblationReport, error) {
	if err := ValidateFindingEvalSuite(suite); err != nil {
		return FindingAblationReport{}, err
	}
	report, err := EvaluateFindingRetriever(ctx, retriever, suite.Cases)
	if err != nil {
		return FindingAblationReport{}, err
	}
	report.SuiteID = suite.ID
	report.SuiteDigest = suite.Digest
	report.CorpusSnapshot = suite.CorpusSnapshot
	return sealFindingAblationReport(report)
}

func sealFindingAblationReport(report FindingAblationReport) (FindingAblationReport, error) {
	report.Digest = ""
	digest, err := CanonicalDigest(report)
	if err != nil {
		return FindingAblationReport{}, err
	}
	report.Digest = digest
	return report, nil
}
