package knowledge

import "testing"

// The context-pack suite bar mirrors evaluationOutcomePassed: per-case safety
// and the required-evidence contract are absolute, while the retrieval-quality
// dimensions that trade off against ranking (expected evidence found,
// abstention) are graded by the aggregate instruments instead of hard-failing
// the whole benchmark on a single ranking miss.
func TestContextPackSuiteBarGatesSafetyAndRequiredContractOnly(t *testing.T) {
	safeQualityMiss := ContextPackOutcome{
		CaseID: "ranking-miss", SafetyPassed: true,
		QualityGateApplicable: true, RequiredEvidencePresent: true,
		ExpectedEvidenceFound: false, AbstentionCorrect: true,
		QualityPassed: false, Passed: false,
	}
	if !contextPackOutcomesPassed([]ContextPackOutcome{safeQualityMiss}) {
		t.Fatal("a safety-clean ranking miss must not fail the suite bar; quality is graded, not absolute")
	}

	requiredMissing := safeQualityMiss
	requiredMissing.CaseID = "required-missing"
	requiredMissing.RequiredEvidencePresent = false
	if contextPackOutcomesPassed([]ContextPackOutcome{requiredMissing}) {
		t.Fatal("a pinned required path missing at an applicable evidence budget must fail the suite bar")
	}

	belowEvidenceBudget := requiredMissing
	belowEvidenceBudget.CaseID = "below-evidence-budget"
	belowEvidenceBudget.QualityGateApplicable = false
	if !contextPackOutcomesPassed([]ContextPackOutcome{belowEvidenceBudget}) {
		t.Fatal("below a case's declared evidence budget the required contract is deliberately not enforced")
	}

	unsafe := safeQualityMiss
	unsafe.CaseID = "unsafe"
	unsafe.SafetyPassed = false
	if contextPackOutcomesPassed([]ContextPackOutcome{unsafe}) {
		t.Fatal("a safety violation must fail the suite bar")
	}

	errored := safeQualityMiss
	errored.CaseID = "errored"
	errored.Error = "context pack build failed"
	if contextPackOutcomesPassed([]ContextPackOutcome{errored}) {
		t.Fatal("a pack that failed to build must fail the suite bar")
	}

	if contextPackOutcomesPassed(nil) {
		t.Fatal("an empty suite proves nothing and must not pass")
	}
}
