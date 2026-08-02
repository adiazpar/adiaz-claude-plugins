package knowledge

import (
	"bytes"
	"strings"
	"testing"
)

func testFindingDocument() FindingDocument {
	return FindingDocument{
		Record: FindingRecord{
			SchemaVersion: CampaignSchemaVersion,
			ID:            "F-0042",
			CampaignID:    "C-RESOURCE-REGISTRATION",
			Revision:      1,
			CreatedAt:     "2026-08-02T19:30:00Z",
			UpdatedAt:     "2026-08-02T19:30:00Z",
			CreatedBy:     "curator:test",
			UpdatedBy:     "curator:test",
			CorrelationID: "corr-test-42",
			Kind:          "conclusion",
			Subject:       "engine.resource-registration",
			Claim:         "Resource registration uses the named table under the recorded build scope.",
			Scope: map[string]any{
				"builds":  []any{"subject-build-id"},
				"product": "doom-2016",
			},
			AppliesWhen: []string{"the recorded build is active"},
			KnownLimits: []string{"does not establish other builds"},
			Tags:        []string{"registration", "resources"},
			Subsystems:  []string{"engine"},
			Aliases:     []string{"resource table"},
			SourceRuns:  []string{"R-20260802-0042"},
			Evidence: []EvidenceReference{{
				Path: "active/resource-registration/runs/R-20260802-0042/report.md", SHA256: "sha256:" + strings.Repeat("a", 64),
				StartLine: 10, EndLine: 14, SourceRun: "R-20260802-0042",
			}},
			Relations:     FindingRelations{DependsOn: []string{"F-0041"}},
			EvidenceGrade: "direct",
			ReviewState:   "curator-checked",
			Validity:      "provisional",
			Projection:    "campaign",
			VerifiedAt:    "2026-08-02",
			Path:          "active/resource-registration/findings/F-0042.md",
			Body: "# Claim\nResource registration uses the named table.\n\n" +
				"## Applies when\nThe recorded build is active.\n\n" +
				"## Does not establish\nOther builds.\n\n" +
				"## Evidence\nSee the exact report range.\n\n" +
				"## Reproduction\nRe-run the cited probe.\n\n" +
				"## Relations\nDepends on F-0041.",
		},
		SyntheticQuestions: []string{
			"Which table drives resource registration?",
			"How is resource registration selected in this build?",
			"Where is the resource registration table used?",
		},
		QuestionsReviewed: true,
	}
}

func TestFindingDocumentCanonicalRoundTrip(t *testing.T) {
	document := testFindingDocument()
	first, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFindingDocument(first, document.Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderFindingDocument(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical finding changed after parse/render round trip")
	}
	if !sha256ValueRE.MatchString(parsed.Record.Digest) {
		t.Fatalf("unexpected record digest %q", parsed.Record.Digest)
	}
	if parsed.Record.ID != document.Record.ID || parsed.Record.Claim != document.Record.Claim {
		t.Fatalf("identity changed: %#v", parsed.Record)
	}
	if got := EvidenceHandle(parsed.Record.ID, parsed.Record.Evidence[0]); !strings.HasPrefix(got, "evidence:F-0042:") {
		t.Fatalf("unexpected evidence handle %q", got)
	}
}

func TestFindingDocumentRejectsTamperAndUnsafeYAML(t *testing.T) {
	body, err := RenderFindingDocument(testFindingDocument())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(body, []byte("named table under"), []byte("other table under"), 1)
	if _, err := ParseFindingDocument(tampered, testFindingDocument().Record.Path); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered finding was not rejected by digest: %v", err)
	}
	duplicate := bytes.Replace(body, []byte("kind: \"conclusion\"\n"), []byte("kind: \"conclusion\"\nkind: \"method\"\n"), 1)
	if _, err := ParseFindingDocument(duplicate, testFindingDocument().Record.Path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate key was not rejected: %v", err)
	}
	alias := bytes.Replace(body, []byte("subject: \"engine.resource-registration\""), []byte("subject: &subject \"engine.resource-registration\""), 1)
	if _, err := ParseFindingDocument(alias, testFindingDocument().Record.Path); err == nil {
		t.Fatalf("YAML alias feature was not rejected: %v", err)
	}
}

func TestFindingDocumentQuotedPunctuationRoundTripsWithoutEnablingYAMLFeatures(t *testing.T) {
	document := testFindingDocument()
	document.Record.Claim = "A & B use pointer *table when the guard !disabled remains false."
	document.SyntheticQuestions[0] = "Why do A & B use *table?"
	body, err := RenderFindingDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFindingDocument(body, document.Record.Path); err != nil {
		t.Fatalf("canonical quoted punctuation did not round trip: %v", err)
	}
}

func TestFindingDocumentRequiresReviewedDoc2QueryAndExactEvidence(t *testing.T) {
	document := testFindingDocument()
	document.QuestionsReviewed = false
	if _, err := RenderFindingDocument(document); err == nil || !strings.Contains(err.Error(), "reviewed synthetic questions") {
		t.Fatalf("unreviewed questions were accepted: %v", err)
	}
	document = testFindingDocument()
	document.SyntheticQuestions = document.SyntheticQuestions[:2]
	if _, err := RenderFindingDocument(document); err == nil || !strings.Contains(err.Error(), "3-5") {
		t.Fatalf("too few questions were accepted: %v", err)
	}
	document = testFindingDocument()
	document.Record.Evidence[0].Path = "../outside.md"
	if _, err := RenderFindingDocument(document); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping evidence path was accepted: %v", err)
	}
	document = testFindingDocument()
	document.Record.Evidence[0].Path = "active/resource-registration/../run/report.md"
	if _, err := RenderFindingDocument(document); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("noncanonical evidence path was accepted: %v", err)
	}
	document = testFindingDocument()
	document.Record.Evidence[0].EndLine = 0
	if _, err := RenderFindingDocument(document); err == nil || !strings.Contains(err.Error(), "range") {
		t.Fatalf("partial evidence range was accepted: %v", err)
	}
}

func TestFindingDocumentDistinguishesSpawnedWorkItemsFromFindingRelations(t *testing.T) {
	document := testFindingDocument()
	document.Record.Relations.Spawned = []string{"W-0042"}
	document.Record.Projection = "playbook"
	if _, err := RenderFindingDocument(document); err != nil {
		t.Fatalf("valid spawned work item or supported projection was rejected: %v", err)
	}

	document.Record.Relations.Spawned = []string{"F-0043"}
	if _, err := RenderFindingDocument(document); err == nil {
		t.Fatalf("finding id was accepted as a spawned work item: %v", err)
	}
}

func TestFindingSourceClassDoesNotCollapseReviewAndValidityAxes(t *testing.T) {
	record := testFindingDocument().Record
	record.ReviewState = "manager-rejected"
	record.Validity = "invalid"
	if got := FindingSourceClass(record); got != "campaign" {
		t.Fatalf("manager-reviewed invalid finding was mislabeled as %q", got)
	}
	if got := FindingSourceClassAtPath(record, "docs/history/campaigns/x/findings/F-0042.md"); got != "history" {
		t.Fatalf("archived rejected finding lost historical provenance class: %q", got)
	}
	record.ReviewState = "curator-checked"
	if got := FindingSourceClass(record); got != "provisional" {
		t.Fatalf("curator finding was mislabeled as %q", got)
	}
}
