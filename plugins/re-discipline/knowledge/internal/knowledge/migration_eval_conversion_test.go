package knowledge

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The conversion regression this file guards: judgments were retargeted onto
// canonical successors that chunk retrieval and context packs can never
// serve. A campaign masterfile's successor is structured JSON, and a split
// truth document's successor manifest is a link stub whose prose vocabulary
// lives only in the demoted archive provenance. Judgments must follow the
// content.
func testEvalConversionPlan(t *testing.T, stagingRoot string) MigrationPlan {
	t.Helper()
	writeStaged := func(relative, body string) {
		t.Helper()
		target := filepath.Join(stagingRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan := MigrationPlan{
		Sources: []MigrationSource{
			{
				Path: "active/alpha-campaign/CAMPAIGN.md", Role: "legacy-campaign-masterfile",
				Campaign: "alpha-campaign", Destination: "active/alpha-campaign/campaign.json",
			},
			{
				Path: "docs/truth/daemon/oracle.md", Role: "truth",
				Destination: "docs/truth/findings/F-000000000000000001.md",
			},
			{
				Path: "docs/truth/tooling/wwise.md", Role: "truth",
				Destination: "docs/truth/splits/F-000000000000000002.md",
			},
		},
		TruthConversions: []MigrationTruthPlan{
			{
				SourcePath: "docs/truth/daemon/oracle.md", FindingID: "F-000000000000000001",
				Destination: "docs/truth/findings/F-000000000000000001.md",
				Title:       "Validation oracle", Claim: "The oracle validates a submitted map.",
			},
			{
				SourcePath: "docs/truth/tooling/wwise.md", FindingID: "F-000000000000000002",
				Destination: "docs/truth/findings/F-000000000000000002.md",
				Title:       "Wwise audio catalog", Claim: "DOOM 2016 audio is Audiokinetic Wwise.",
			},
			{
				SourcePath: "docs/truth/tooling/wwise.md", FindingID: "F-000000000000000003",
				Destination: "docs/truth/findings/F-000000000000000003.md",
				Title:       "Wwise exporter tools", Claim: "Two tools exploit the Wwise metadata join.",
			},
		},
	}
	writeStaged("docs/truth/findings/F-000000000000000001.md",
		"# Validation oracle\n\nThe oracle validates a submitted map.\n")
	writeStaged("docs/truth/findings/F-000000000000000002.md",
		"# Wwise audio catalog\n\nDOOM 2016 audio is Audiokinetic Wwise.\n")
	writeStaged("docs/truth/findings/F-000000000000000003.md",
		"# Wwise exporter tools\n\nTwo tools exploit the Wwise metadata join.\n")
	writeStaged(
		"active/alpha-campaign/runs/"+legacyRunID("alpha-campaign", "campaign-import")+
			"/payload/legacy/CAMPAIGN.md",
		"# Campaign: alpha\n\nClaim: This campaign owns the offline switchboard.\n")
	writeStaged(
		"active/alpha-campaign/runs/"+legacyRunID("alpha-campaign", "campaign-import")+
			"/payload/legacy/truth/daemon/oracle.md",
		"# Validation oracle\n\nClaim: The oracle validates a submitted map.\n\nVerdicts carry dtor_ok flags.\n")
	return plan
}

func TestEvalConversionRetargetsMasterfileJudgmentsToPreservedProse(t *testing.T) {
	staging := t.TempDir()
	plan := testEvalConversionPlan(t, staging)
	context := newMigrationEvalJudgmentContext(plan, staging)
	answerable := true
	cases := []EvalCase{{
		ID: "campaign-owner", Query: "which campaign owns the offline switchboard",
		AllowedTiers: []string{"active"}, Answerable: &answerable,
		VocabularyPolicy:     "target-disjoint",
		ExpectedPaths:        []string{"active/alpha-campaign/CAMPAIGN.md"},
		MinimumEvidencePaths: []string{"active/alpha-campaign/CAMPAIGN.md"},
	}}
	convertMigratedEvalCases(&context, cases)
	preserved := "active/alpha-campaign/runs/" +
		legacyRunID("alpha-campaign", "campaign-import") + "/payload/legacy/CAMPAIGN.md"
	if !reflect.DeepEqual(cases[0].ExpectedPaths, []string{preserved}) ||
		!reflect.DeepEqual(cases[0].MinimumEvidencePaths, []string{preserved}) {
		t.Fatalf("masterfile judgment did not follow the preserved prose: %#v", cases[0])
	}
	if !contains(cases[0].AllowedTiers, "archive") {
		t.Fatalf("masterfile case did not gain the provenance tier: %#v", cases[0].AllowedTiers)
	}
	if strings.Contains(strings.Join(cases[0].ExpectedPaths, " "), "campaign.json") {
		t.Fatal("judgment still names the unservable structured record")
	}
	if cases[0].VocabularyPolicy != "" {
		t.Fatal("retargeted case kept a disjointness attestation nobody re-reviewed")
	}
}

func TestEvalConversionFollowsVocabularyIntoPreservedTruth(t *testing.T) {
	staging := t.TempDir()
	plan := testEvalConversionPlan(t, staging)
	context := newMigrationEvalJudgmentContext(plan, staging)
	answerable := true
	cases := []EvalCase{{
		ID: "identifier", Query: "dtor_ok",
		AllowedTiers: []string{"truth"}, Answerable: &answerable,
		ExpectedPaths:        []string{"docs/truth/daemon/oracle.md"},
		MinimumEvidencePaths: []string{"docs/truth/daemon/oracle.md"},
	}}
	convertMigratedEvalCases(&context, cases)
	preserved := "active/alpha-campaign/runs/" +
		legacyRunID("alpha-campaign", "campaign-import") + "/payload/legacy/truth/daemon/oracle.md"
	if !contains(cases[0].ExpectedPaths, "docs/truth/findings/F-000000000000000001.md") ||
		!contains(cases[0].ExpectedPaths, preserved) {
		t.Fatalf("identifier judgment lost either the finding or the preserved prose: %#v", cases[0].ExpectedPaths)
	}
	if !reflect.DeepEqual(cases[0].MinimumEvidencePaths, []string{preserved}) {
		t.Fatalf("minimum evidence does not follow the only text carrying the identifier: %#v",
			cases[0].MinimumEvidencePaths)
	}
	if !contains(cases[0].AllowedTiers, "archive") {
		t.Fatalf("vocabulary-forced case did not gain the provenance tier: %#v", cases[0].AllowedTiers)
	}
}

func TestEvalConversionExpandsSplitTruthToItsFindings(t *testing.T) {
	staging := t.TempDir()
	plan := testEvalConversionPlan(t, staging)
	context := newMigrationEvalJudgmentContext(plan, staging)
	answerable := true
	cases := []EvalCase{{
		ID: "split", Query: "Wwise audio integration",
		AllowedTiers: []string{"truth"}, Answerable: &answerable,
		ExpectedPaths:        []string{"docs/truth/tooling/wwise.md"},
		MinimumEvidencePaths: []string{"docs/truth/tooling/wwise.md"},
		HardNegativePaths:    []string{"docs/truth/daemon/oracle.md"},
	}}
	convertMigratedEvalCases(&context, cases)
	wantExpected := []string{
		"docs/truth/splits/F-000000000000000002.md",
		"docs/truth/findings/F-000000000000000002.md",
		"docs/truth/findings/F-000000000000000003.md",
	}
	if !reflect.DeepEqual(cases[0].ExpectedPaths, wantExpected) {
		t.Fatalf("split judgment is not satisfied by its own findings: %#v", cases[0].ExpectedPaths)
	}
	if len(cases[0].MinimumEvidencePaths) != 1 ||
		!strings.HasPrefix(cases[0].MinimumEvidencePaths[0], "docs/truth/findings/") {
		t.Fatalf("split minimum evidence did not select a vocabulary-bearing finding: %#v",
			cases[0].MinimumEvidencePaths)
	}
	if contains(cases[0].AllowedTiers, "archive") {
		t.Fatalf("vocabulary-reached split case must not widen its tiers: %#v", cases[0].AllowedTiers)
	}
	if !reflect.DeepEqual(cases[0].HardNegativePaths,
		[]string{"docs/truth/findings/F-000000000000000001.md"}) {
		t.Fatalf("hard negative did not follow the single-claim conversion: %#v", cases[0].HardNegativePaths)
	}
}

func TestEvalConversionLeavesUnconvertedJudgmentsAlone(t *testing.T) {
	staging := t.TempDir()
	plan := testEvalConversionPlan(t, staging)
	context := newMigrationEvalJudgmentContext(plan, staging)
	answerable := true
	cases := []EvalCase{{
		ID: "history", Query: "phone control session",
		AllowedTiers: []string{"history"}, Answerable: &answerable,
		VocabularyPolicy:     "target-disjoint",
		ExpectedPaths:        []string{"docs/history/chronicles/2026-07-29-remote-play-control.md"},
		MinimumEvidencePaths: []string{"docs/history/chronicles/2026-07-29-remote-play-control.md"},
	}}
	convertMigratedEvalCases(&context, cases)
	if !reflect.DeepEqual(cases[0].ExpectedPaths,
		[]string{"docs/history/chronicles/2026-07-29-remote-play-control.md"}) ||
		cases[0].VocabularyPolicy != "target-disjoint" ||
		!reflect.DeepEqual(cases[0].AllowedTiers, []string{"history"}) {
		t.Fatalf("unconverted case drifted: %#v", cases[0])
	}
}

func TestEvalConversionRestampsCorpusSnapshotsOntoActivatedCorpus(t *testing.T) {
	staging := t.TempDir()
	plan := testEvalConversionPlan(t, staging)
	context := newMigrationEvalJudgmentContext(plan, staging)
	context.activatedCorpusFingerprint = "sha256:" + strings.Repeat("ab", 32)
	notAnswerable := false
	cases := []EvalCase{
		{
			ID: "absence", Query: "qzvxjklmnpwtrb", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, Answerable: &notAnswerable,
			CorpusSnapshot: "sha256:" + strings.Repeat("cd", 32),
		},
		{
			ID: "fixture", Query: "conformance", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, Answerable: &notAnswerable,
			CorpusSnapshot: "fixture:packaged-conformance-v1",
		},
	}
	convertMigratedEvalCases(&context, cases)
	if cases[0].CorpusSnapshot != context.activatedCorpusFingerprint {
		t.Fatalf("unpinned corpus snapshot was not restamped: %q", cases[0].CorpusSnapshot)
	}
	if cases[1].CorpusSnapshot != "fixture:packaged-conformance-v1" {
		t.Fatalf("fixture snapshot must never be restamped: %q", cases[1].CorpusSnapshot)
	}
}
