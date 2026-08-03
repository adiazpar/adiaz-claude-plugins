package knowledge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestExternalProjectEvalCorpus validates a maintainer-supplied project corpus
// without making that machine-local corpus part of the hermetic release
// fixture. It is skipped unless RE_DISCIPLINE_EVAL_CORPUS names the project
// root. This is an authoring check only; it does not index or benchmark the
// project and cannot change retrieval policy.
func TestExternalProjectEvalCorpus(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("RE_DISCIPLINE_EVAL_CORPUS"))
	if root == "" {
		t.Skip("RE_DISCIPLINE_EVAL_CORPUS not set")
	}
	evalRoot := filepath.Join(root, ".re-discipline", "knowledge", "evals")
	entries, err := os.ReadDir(evalRoot)
	if err != nil {
		t.Fatal(err)
	}
	all := []EvalCase{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		cases, err := LoadEvalCases(filepath.Join(evalRoot, entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		all = append(all, cases...)
	}
	if err := ValidateEvalCorpus(all); err != nil {
		t.Fatalf("combined project evaluation corpus: %v", err)
	}
	if len(all) != 64 {
		t.Fatalf("project evaluation corpus has %d cases, want 64", len(all))
	}

	snapshots := map[string]bool{}
	development, holdout, manager, drafter := 0, 0, 0, 0
	answerable, abstention, pinCount := 0, 0, 0
	pinPaths := map[string]bool{}
	for _, eval := range all {
		snapshots[eval.CorpusSnapshot] = true
		switch eval.Split {
		case "development":
			development++
		case "holdout":
			holdout++
		}
		switch eval.Role {
		case "manager":
			manager++
		case "drafter":
			drafter++
		}
		if *eval.Answerable {
			answerable++
		} else {
			abstention++
		}
		if !evidencePinsIntact(root, eval.EvidencePins) {
			t.Fatalf("case %s has a stale or unreadable evidence pin", eval.ID)
		}
		pinCount += len(eval.EvidencePins)
		for _, pin := range eval.EvidencePins {
			pinPaths[pin.Path] = true
		}
	}
	if len(snapshots) != 1 {
		values := make([]string, 0, len(snapshots))
		for value := range snapshots {
			values = append(values, value)
		}
		sort.Strings(values)
		t.Fatalf("project evaluation corpus mixes snapshots: %v", values)
	}
	if development != 32 || holdout != 32 || manager != 52 || drafter != 12 ||
		answerable != 58 || abstention != 6 || pinCount != 92 || len(pinPaths) != 65 {
		t.Fatalf(
			"project evaluation strata drifted: dev=%d holdout=%d manager=%d drafter=%d answerable=%d abstention=%d pins=%d paths=%d",
			development, holdout, manager, drafter, answerable, abstention,
			pinCount, len(pinPaths),
		)
	}
	vocabulary, err := MeasureEvalVocabularyDisjoint(root, all)
	if err != nil {
		t.Fatal(err)
	}
	if vocabulary.DeclaredCases != 26 || vocabulary.PassedCases != 26 ||
		vocabulary.FailedCases != 0 {
		t.Fatalf("project vocabulary-disjoint stratum drifted: %#v", vocabulary)
	}
}
