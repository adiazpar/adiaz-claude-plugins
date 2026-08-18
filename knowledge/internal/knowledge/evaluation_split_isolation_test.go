package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func splitIsolationCase(id, topic, split, target string) EvalCase {
	return EvalCase{
		ID: id, Role: "manager", Topic: topic, Split: split,
		Query:      "which maintained evidence answers isolated evaluation question " + id,
		QueryClass: "conceptual", AllowedTiers: []string{"truth"},
		CorpusSnapshot: "fixture:split-isolation", ExpectedPaths: []string{target},
		MinimumEvidencePaths: []string{target}, ExpectedCitations: []string{target},
		ForbiddenTiers: []string{"history"}, TokenBudget: 512,
		Answerable: boolPointer(true),
	}
}

func TestEvaluationAllJudgmentsAndQueriesAreSplitIsolated(t *testing.T) {
	development := splitIsolationCase(
		"development-negative", "development-negative-topic", "development",
		"docs/truth/development.md")
	development.HardNegativePaths = []string{"docs/history/shared-negative.md"}
	holdout := splitIsolationCase(
		"holdout-negative", "holdout-negative-topic", "holdout",
		"docs/truth/holdout.md")
	holdout.HardNegativePaths = []string{"docs/history/shared-negative.md"}
	if err := ValidateEvalCorpus([]EvalCase{development, holdout}); err == nil ||
		!strings.Contains(err.Error(), "shared-negative.md") {
		t.Fatalf("cross-split hard negative was accepted: %v", err)
	}

	holdout.HardNegativePaths = []string{"docs/history/holdout-negative.md"}
	holdout.Query = "  WHICH maintained evidence answers isolated evaluation question DEVELOPMENT-NEGATIVE  "
	if err := ValidateEvalCorpus([]EvalCase{development, holdout}); err == nil ||
		!strings.Contains(err.Error(), "query is repeated") {
		t.Fatalf("normalized duplicate query was accepted: %v", err)
	}
}

func TestUntypedEvidencePinUsesContentAsSemanticIdentity(t *testing.T) {
	root := t.TempDir()
	relative := "active/example/CAMPAIGN.md"
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("# Campaign\n\nObjective: preserve the original behavior.\n")
	if err := os.WriteFile(absolute, body, 0o600); err != nil {
		t.Fatal(err)
	}
	pin := EvidencePin{
		Path: relative, ClaimSha256: ClaimDigest(string(body), relative),
		ContentSha256: "sha256:" + SHA256Bytes(body),
	}
	if !evidencePinsIntact(root, []EvidencePin{pin}) {
		t.Fatal("fresh untyped evidence pin was not intact")
	}
	mutated := []byte("# Campaign\n\nObjective: replace the original behavior.\n")
	if err := os.WriteFile(absolute, mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	if evidencePinsIntact(root, []EvidencePin{pin}) {
		t.Fatal("substantive untyped-document mutation preserved pin freshness")
	}
	if state := classifyEvidencePin(root, pin); state != "broken" {
		t.Fatalf("untyped-document mutation classified %q, want broken", state)
	}
}

func findingSplitIsolationCase(id, topic, split, findingID string) FindingEvalCase {
	handle := "evidence:" + findingID + ":" + strings.Repeat("a", 20)
	return FindingEvalCase{
		ID: id, Role: "manager", Topic: topic, Split: split,
		Query: "which normalized finding answers " + id + "?", QueryClass: "conceptual",
		AllowedSourceClasses: []string{"truth"}, AllowedReviewStates: []string{"manager-ratified"},
		AllowedValidities: []string{"current"}, TokenBudget: 512, Answerable: true,
		ExpectedFindingIDs: []string{findingID}, ExpectedFindingHandles: []string{FindingHandle(findingID)},
		ExpectedEvidenceHandles: []string{handle},
		ExpectedSourceClasses:   map[string]string{findingID: "truth"},
		ExpectedReviewStates:    map[string]string{findingID: "manager-ratified"},
		ExpectedValidities:      map[string]string{findingID: "current"},
	}
}

func TestFindingEvaluationJudgmentsAndQueriesAreSplitIsolated(t *testing.T) {
	development := findingSplitIsolationCase(
		"finding-development", "finding-development-topic", "development", "F-0101")
	development.HardNegativeFindingIDs = []string{"F-0999"}
	holdout := findingSplitIsolationCase(
		"finding-holdout", "finding-holdout-topic", "holdout", "F-0201")
	holdout.HardNegativeFindingIDs = []string{"F-0999"}
	if err := ValidateFindingEvalCases([]FindingEvalCase{development, holdout}); err == nil ||
		!strings.Contains(err.Error(), "F-0999") {
		t.Fatalf("cross-split finding hard negative was accepted: %v", err)
	}

	holdout.HardNegativeFindingIDs = []string{"F-0998"}
	holdout.Query = strings.ToUpper(development.Query)
	if err := ValidateFindingEvalCases([]FindingEvalCase{development, holdout}); err == nil ||
		!strings.Contains(err.Error(), "query is repeated") {
		t.Fatalf("duplicate finding query was accepted: %v", err)
	}
}

func TestEvaluationTargetsCannotLeakAcrossDevelopmentAndHoldout(t *testing.T) {
	cases := []EvalCase{
		splitIsolationCase("development-case", "development-topic", "development", "docs/truth/shared.md"),
		splitIsolationCase("holdout-case", "holdout-topic", "holdout", "docs/truth/shared.md"),
	}
	if err := ValidateEvalCorpus(cases); err == nil {
		t.Fatal("development and holdout accepted the same graded target document")
	}

	cases[1].ExpectedPaths = []string{"docs/truth/holdout.md"}
	cases[1].MinimumEvidencePaths = []string{"docs/truth/holdout.md"}
	cases[1].ExpectedCitations = []string{"docs/truth/holdout.md"}
	if err := ValidateEvalCorpus(cases); err != nil {
		t.Fatalf("disjoint evaluation targets were rejected: %v", err)
	}
}

func TestCorpusAbsenceCaseRequiresAnalyzerAtomicExactToken(t *testing.T) {
	eval := splitIsolationCase(
		"absent-token", "absent-token-topic", "development", "docs/truth/unused.md")
	eval.Query = "rd_absent_layout_zqv_7f31c9"
	eval.QueryClass = "exact"
	eval.ExpectedPaths = nil
	eval.MinimumEvidencePaths = nil
	eval.ExpectedCitations = nil
	eval.Answerable = boolPointer(false)
	if err := ValidateEvalCorpus([]EvalCase{eval}); err == nil ||
		!strings.Contains(err.Error(), "analyzer-atomic") {
		t.Fatalf("split corpus-absence token was accepted: %v", err)
	}

	eval.Query = "qzvxjklmnpwtrb"
	if err := ValidateEvalCorpus([]EvalCase{eval}); err != nil {
		t.Fatalf("analyzer-atomic corpus-absence token was rejected: %v", err)
	}
}

func TestAnalyzerAtomicAbsenceTokensReturnZeroEligibleCards(t *testing.T) {
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, "docs", "truth", "ordinary-terms.md"), `# Ordinary terms

The maintained layout, command, RVA, archive, cvar, and format references live here.
`)
	service := newAdversarialService(t, root, nil)
	options := SearchOptions{
		Query: "rd_absent_layout_zqv_7f31c9", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, Limit: 12, TokenBudget: 2048,
	}
	contaminated, err := service.Search(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(contaminated.Results) == 0 {
		t.Fatal("split synthetic token did not expose its ordinary-term retrieval contamination")
	}

	for _, token := range []string{
		"qzvxjklmnpwtrb",
		"nvkqmxzplrtdhs",
		"hjkzqvpxmnrltc",
		"wpxqmxzvlntrhs",
		"bvzqjkmxpntrls",
		"fdzqvmxkplnrts",
	} {
		options.Query = token
		response, err := service.Search(context.Background(), options)
		if err != nil {
			t.Fatalf("search %q: %v", token, err)
		}
		if len(response.Results) != 0 {
			t.Fatalf("analyzer-atomic token %q returned eligible cards: %#v", token, response.Results)
		}
	}
}

func TestTargetVocabularyPolicyMeasuresFullExpectedDocument(t *testing.T) {
	root := t.TempDir()
	target := "docs/truth/target.md"
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(target)), `# Target

The durable broker keeps provider sessions separate.
`)
	eval := splitIsolationCase(
		"disjoint-policy", "disjoint-policy-topic", "development", target)
	eval.VocabularyPolicy = "target-disjoint-v1"
	eval.Query = "who oversees the durable relay migration"
	if err := ValidateEvalCorpus([]EvalCase{eval}); err != nil {
		t.Fatal(err)
	}
	report, err := MeasureEvalVocabularyDisjoint(root, []EvalCase{eval})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeclaredCases != 1 || report.PassedCases != 0 ||
		report.FailedCases != 1 || len(report.Failures) != 1 ||
		!contains(report.Failures[0].OverlappingTerms, "durable") {
		t.Fatalf("target overlap was not measured: %#v", report)
	}

	eval.Query = "who oversees bilateral switchboard handoffs"
	if err := ValidateEvalCorpus([]EvalCase{eval}); err != nil {
		t.Fatal(err)
	}
	report, err = MeasureEvalVocabularyDisjoint(root, []EvalCase{eval})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeclaredCases != 1 || report.PassedCases != 1 ||
		report.FailedCases != 0 || len(report.Failures) != 0 {
		t.Fatalf("disjoint semantic paraphrase failed policy: %#v", report)
	}

	eval.QueryClass = "exact"
	if err := ValidateEvalCorpus([]EvalCase{eval}); err == nil ||
		!strings.Contains(err.Error(), "answerable semantic query") {
		t.Fatalf("exact case was allowed to claim semantic disjointness: %v", err)
	}
}
