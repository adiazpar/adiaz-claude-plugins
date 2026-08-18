package knowledge

import "testing"

func TestContradictionClassificationRecognizesNaturalDisagreementLanguage(t *testing.T) {
	for _, query := range []string{
		"where do shared memory and current truth disagree",
		"show the inconsistent accounts of this behavior",
		"these two observations appear to be at odds",
		"which different account should the manager review",
	} {
		if got := classifyQuery(query); got != "contradiction" {
			t.Fatalf("natural disagreement query %q classified as %q", query, got)
		}
	}
}
