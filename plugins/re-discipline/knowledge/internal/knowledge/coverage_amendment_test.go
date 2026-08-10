package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	retirementUnresolvedRationale = "The curator could not decide whether this trailing note states a durable claim."
	retirementNewRationale        = "The line is a bounded-probe status note about the report, not a claim about the system."
	retirementSecondRationale     = "The second trailing note repeats the probe's own status and asserts nothing."
)

// retirementGraph is the smallest campaign that can carry a coverage
// retirement: one reviewed intake over one canonical run report, with one
// ratified candidate span and two spans the curator left unjudged.
//
// It exists separately from the end-to-end fixture because the binding
// arithmetic, the append-only log, and the monotonicity property are all
// statements about records rather than about transactions, and proving them
// against hand-built records keeps them readable.
func retirementGraph(t *testing.T) CampaignGraph {
	t.Helper()
	reportPath := "active/test/runs/R-20260802-0001/report.md"
	reportDigest := "sha256:" + strings.Repeat("a", 64)
	campaignAny, _, err := sealCampaignRecord(CampaignRecord{
		RecordMeta: closureTestMeta("C-TEST"), Title: "Test campaign", Slug: "test",
		Objective: "Exercise coverage retirement", Scope: []string{"plugin"},
		SuccessCriteria: []string{"implementation complete"},
		ClosureCriteria: []string{"records accounted for"}, Status: "closing",
		Owner: "manager", PermittedManagers: []string{"manager"},
		OpenedAt: closureTestTime, ClosingAt: closureTestTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	workAny, _, err := sealWorkItemRecord(WorkItemRecord{
		RecordMeta: closureTestMeta("W-0001"), CampaignID: "C-TEST", Kind: "task",
		Title: "Implement", Problem: "Implementation is needed", State: "done",
		Priority: "high", Acceptance: []string{"done"}, Owner: "manager",
		CompletedRunIDs: []string{"R-20260802-0001"}, FindingIDs: []string{"F-0001"},
		Outcome: "Implemented and verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	runAny, _, err := sealRunRecord(RunRecord{
		RecordMeta: closureTestMeta("R-20260802-0001"), CampaignID: "C-TEST",
		PrimaryWorkItemID: "W-0001", ActorID: "manager", Role: "manager",
		Status: "completed", StartedAt: closureTestTime, ReturnedAt: closureTestTime,
		ReviewedAt: closureTestTime, TerminalAt: closureTestTime,
		Report:     &FileHandle{Path: reportPath, SHA256: reportDigest},
		FindingIDs: []string{"F-0001"}, ResultSummary: "Complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	finding := FindingRecord{
		SchemaVersion: CampaignSchemaVersion, ID: "F-0001", CampaignID: "C-TEST",
		Revision: 2, CreatedAt: closureTestTime, UpdatedAt: closureTestTime,
		CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "closure-test",
		Kind: "conclusion", Subject: "plugin", Claim: "The implementation is complete.",
		Scope: map[string]any{"component": "plugin"}, SourceRuns: []string{"R-20260802-0001"},
		Evidence: []EvidenceReference{{
			Path: reportPath, SHA256: reportDigest, StartLine: 1, EndLine: 2,
			ObjectKey: "path:" + reportPath + "#L1-L2", SourceRun: "R-20260802-0001",
		}},
		EvidenceGrade: "direct", ReviewState: "manager-ratified", Validity: "current",
		Projection: "truth",
	}
	finding.Digest, err = CanonicalDigest(finding)
	if err != nil {
		t.Fatal(err)
	}
	intakeMeta := closureTestMeta("I-0001")
	intakeMeta.Revision = 2
	intakeAny, _, err := sealIntakeRecord(IntakeRecord{
		RecordMeta: intakeMeta, CampaignID: "C-TEST",
		SourceRuns:          []FileHandle{{Path: reportPath, SHA256: reportDigest}},
		CandidateFindingIDs: []string{"F-0001"},
		Coverage: []CoverageEntry{
			{
				SourceHandle: "path:" + reportPath + "#L1-L2", SourcePath: reportPath,
				SourceSHA256: reportDigest, StartLine: 1, EndLine: 2, SourceLineCount: 4,
				Disposition: "candidate-finding", TargetID: "F-0001",
			},
			{
				SourceHandle: "path:" + reportPath + "#L3-L3", SourcePath: reportPath,
				SourceSHA256: reportDigest, StartLine: 3, EndLine: 3, SourceLineCount: 4,
				Disposition: "unresolved", Rationale: retirementUnresolvedRationale,
			},
			{
				SourceHandle: "path:" + reportPath + "#L4-L4", SourcePath: reportPath,
				SourceSHA256: reportDigest, StartLine: 4, EndLine: 4, SourceLineCount: 4,
				Disposition: "unresolved", Rationale: retirementSecondRationale,
			},
		},
		Triage: map[string]string{"F-0001": "routine"},
		Status: "reviewed",
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewAny, _, err := sealReviewRecord(ReviewRecord{
		RecordMeta: closureTestMeta("V-0001"), CampaignID: "C-TEST",
		Reviewer: "manager", Authority: "manager", IntakeID: "I-0001", IntakeRevision: 1,
		PacketDigest: stateTestDigest("9"),
		ReviewLoad:   stateTestReviewLoad("V-0001", "C-TEST", stateTestDigest("9"), 1, 0),
		Decisions: []ReviewDecision{{
			FindingID: "F-0001", FindingRevision: 1, Action: "ratify", Rationale: "Direct evidence resolves it",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign := campaignAny.(CampaignRecord)
	work := workAny.(WorkItemRecord)
	run := runAny.(RunRecord)
	intake := intakeAny.(IntakeRecord)
	review := reviewAny.(ReviewRecord)
	return CampaignGraph{
		Campaign:  &campaign,
		WorkItems: map[string]WorkItemRecord{work.ID: work},
		Runs:      map[string]RunRecord{run.ID: run},
		Findings:  map[string]FindingRecord{finding.ID: finding},
		Intakes:   map[string]IntakeRecord{intake.ID: intake},
		Reviews:   map[string]ReviewRecord{review.ID: review},
	}
}

func retirementGraphReportPath() string {
	return "active/test/runs/R-20260802-0001/report.md"
}

// retireInGraph advances the fixture intake by one revision exactly as the
// engine would, so that a test can then ask the predicates whether the review
// still binds it.
func retireInGraph(t *testing.T, graph CampaignGraph, at string, retirements ...CoverageRetirement) CampaignGraph {
	t.Helper()
	prior := graph.Intakes["I-0001"]
	amendment := CoverageAmendment{
		Revision: prior.Revision + 1, AmendedAt: at, AmendedBy: "manager",
		CorrelationID: "corr-retire", ReviewID: "V-0001",
		Rationale:   "Give the spans the curator left unjudged their terminal disposition.",
		Retirements: retirements,
	}
	next, err := applyCoverageRetirements(prior, amendment)
	if err != nil {
		t.Fatalf("apply retirement: %v", err)
	}
	next.Revision++
	next.UpdatedAt, next.UpdatedBy, next.CorrelationID = at, "manager", "corr-retire"
	next.Digest = ""
	sealed, _, err := sealIntakeRecord(next)
	if err != nil {
		t.Fatalf("seal retired intake: %v", err)
	}
	result := cloneCampaignGraph(graph)
	result.Intakes["I-0001"] = sealed.(IntakeRecord)
	return result
}

func retirementOf(handle, from, to, fromRationale, toRationale string) CoverageRetirement {
	return CoverageRetirement{
		SourceHandle: handle, FromDisposition: from, ToDisposition: to,
		FromRationale: fromRationale, ToRationale: toRationale,
	}
}

func retireThirdLine() CoverageRetirement {
	return retirementOf("path:"+retirementGraphReportPath()+"#L3-L3",
		"unresolved", "non-claim", retirementUnresolvedRationale, retirementNewRationale)
}

func retireFourthLine() CoverageRetirement {
	return retirementOf("path:"+retirementGraphReportPath()+"#L4-L4",
		"unresolved", "out-of-scope", retirementSecondRationale, retirementNewRationale)
}

// TestCoverageRetirementPreservesTheReviewBinding is the mechanism the whole
// design turns on, stated as an experiment rather than as an assertion about
// arithmetic.
//
// The intake revision does bump - it must, because validateMetaTransition
// requires +1 and the journal treats (revision, digest) as one identity. What
// carries the binding across the bump is the amendment count, and the only way
// to show that is to bump the revision without appending an amendment and watch
// all three predicates fail. Those three predicates live far apart -
// CampaignGraph.Validate, reviewBindsIntake, and normalizationReviewComplete -
// and each held its own copy of the arithmetic before this change, so a test
// that exercised only one of them would not have caught the copy that was
// missed.
func TestCoverageRetirementPreservesTheReviewBinding(t *testing.T) {
	graph := retirementGraph(t)
	assertBindingHolds := func(stage string, graph CampaignGraph, want bool) {
		t.Helper()
		intake := graph.Intakes["I-0001"]
		review := graph.Reviews["V-0001"]
		validated := graph.Validate() == nil
		bound := reviewBindsIntake(graph, intake)
		normalized := normalizationReviewComplete(review, intake)
		if validated != want || bound != want || normalized != want {
			t.Fatalf("%s: graph.Validate()=%v reviewBindsIntake=%v normalizationReviewComplete=%v, want all %v (validate: %v)",
				stage, validated, bound, normalized, want, graph.Validate())
		}
	}
	assertBindingHolds("before any retirement", graph, true)

	once := retireInGraph(t, graph, "2026-08-02T21:00:00Z", retireThirdLine())
	if once.Intakes["I-0001"].Revision != 3 || len(once.Intakes["I-0001"].Amendments) != 1 {
		t.Fatalf("retirement did not advance one revision and append one amendment: %+v",
			once.Intakes["I-0001"].RecordMeta)
	}
	assertBindingHolds("after one retirement", once, true)

	twice := retireInGraph(t, once, "2026-08-02T21:05:00Z", retireFourthLine())
	if twice.Intakes["I-0001"].Revision != 4 || len(twice.Intakes["I-0001"].Amendments) != 2 {
		t.Fatalf("second retirement did not advance one revision and append one amendment: %+v",
			twice.Intakes["I-0001"].RecordMeta)
	}
	assertBindingHolds("after two retirements", twice, true)

	// The bump alone is not what preserves the binding. Advance the revision
	// with no amendment behind it and every predicate must refuse.
	bumped := cloneCampaignGraph(twice)
	unamended := bumped.Intakes["I-0001"]
	unamended.Revision++
	unamended.UpdatedAt = "2026-08-02T21:10:00Z"
	unamended.Digest = ""
	// The record can no longer seal, because ValidateIntake pins the last
	// amendment's revision to the record's own. Bypass the seal to build the
	// state anyway, so the three binding predicates are exercised rather than
	// only the validator.
	digest, err := CanonicalDigest(unamended)
	if err != nil {
		t.Fatal(err)
	}
	unamended.Digest = digest
	bumped.Intakes["I-0001"] = unamended
	assertBindingHolds("after an unamended revision bump", bumped, false)
	if err := ValidateIntake(unamended); err == nil {
		t.Fatal("an intake whose last amendment predates its own revision was accepted")
	}
}

// TestAmendmentCountCannotBePadded closes the route the binding arithmetic
// would otherwise leave open. intakeReviewedRevision subtracts the amendment
// count, so an attacker who could add entries could drive the reviewed revision
// backwards until some unrelated review appeared to bind the record.
func TestAmendmentCountCannotBePadded(t *testing.T) {
	graph := retireInGraph(t, retirementGraph(t), "2026-08-02T21:00:00Z", retireThirdLine())
	base := graph.Intakes["I-0001"]
	if err := ValidateIntake(base); err != nil {
		t.Fatalf("the retired fixture is not valid: %v", err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(IntakeRecord) IntakeRecord
		want   string
	}{
		{
			name: "the log is duplicated without advancing the revision",
			mutate: func(record IntakeRecord) IntakeRecord {
				record.Amendments = append(append([]CoverageAmendment(nil), record.Amendments...), record.Amendments[0])
				return record
			},
			want: "must strictly increase",
		},
		{
			name: "a padding entry is appended at the record's own revision",
			mutate: func(record IntakeRecord) IntakeRecord {
				padding := record.Amendments[0]
				padding.Revision = record.Revision
				record.Amendments = append(append([]CoverageAmendment(nil), record.Amendments...), padding)
				record.Revision++
				return record
			},
			want: "must strictly increase",
		},
		{
			name: "a padding entry retires a span the record never moved",
			mutate: func(record IntakeRecord) IntakeRecord {
				padding := record.Amendments[0]
				padding.Revision = record.Revision + 1
				padding.Retirements = []CoverageRetirement{retireFourthLine()}
				record.Amendments = append(append([]CoverageAmendment(nil), record.Amendments...), padding)
				record.Revision++
				return record
			},
			want: "does not carry the disposition and rationale its amendment recorded",
		},
		{
			name: "a padding entry restates a span that was already retired",
			mutate: func(record IntakeRecord) IntakeRecord {
				padding := record.Amendments[0]
				padding.Revision = record.Revision + 1
				record.Amendments = append(append([]CoverageAmendment(nil), record.Amendments...), padding)
				record.Revision++
				return record
			},
			want: "twice",
		},
		{
			name: "the log claims more history than the record has",
			mutate: func(record IntakeRecord) IntakeRecord {
				record.Revision = 1
				record.Amendments[0].Revision = 1
				return record
			},
			want: "more history than the record has",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := base
			candidate.Amendments = append([]CoverageAmendment(nil), base.Amendments...)
			candidate = testCase.mutate(candidate)
			err := ValidateIntake(candidate)
			if err == nil {
				t.Fatalf("a padded amendment log was accepted: reviewed revision would be %d",
					intakeReviewedRevision(candidate))
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("refusal does not report %q: %v", testCase.want, err)
			}
		})
	}
}

// TestOnlyUnresolvedSpansMayBeRetired walks every ordered pair of dispositions
// the engine accepts, so the permitted-change rule is pinned as a table rather
// than as whatever the current implementation happens to compute. Widening the
// transition means editing this expectation, in public.
func TestOnlyUnresolvedSpansMayBeRetired(t *testing.T) {
	dispositions := []string{"candidate-finding", "duplicate", "non-claim", "unresolved", "out-of-scope"}
	permitted := map[string]bool{
		"unresolved->non-claim":    true,
		"unresolved->out-of-scope": true,
	}
	pairs := 0
	for _, from := range dispositions {
		for _, to := range dispositions {
			pairs++
			pair := from + "->" + to
			err := validateCoverageRetirementShape(retirementOf(
				"path:report.md#L1-L1", from, to, "prior rationale", "new rationale"))
			if permitted[pair] {
				if err != nil {
					t.Fatalf("permitted pair %s was refused: %v", pair, err)
				}
				continue
			}
			if err == nil {
				t.Fatalf("forbidden pair %s was accepted", pair)
			}
			if !strings.Contains(err.Error(), "may not move disposition") {
				t.Fatalf("pair %s was refused for the wrong reason: %v", pair, err)
			}
		}
	}
	if pairs != 25 {
		t.Fatalf("the disposition table covers %d ordered pairs, not 25", pairs)
	}
	if len(permittedCoverageRetirements) != 1 || len(permittedCoverageRetirements["unresolved"]) != 2 {
		t.Fatalf("the permitted-pair table has been widened: %#v", permittedCoverageRetirements)
	}
}

func TestRetirementRequiresANewRationale(t *testing.T) {
	for _, blank := range []string{"", "   ", "\n\t "} {
		err := validateCoverageRetirementShape(retirementOf(
			"path:report.md#L1-L1", "unresolved", "non-claim", "prior rationale", blank))
		if err == nil || !strings.Contains(err.Error(), "requires a new rationale") {
			t.Fatalf("a retirement with rationale %q was accepted: %v", blank, err)
		}
	}
	// The rationale it displaces may legitimately be empty on some records, so
	// only the new one is mandatory.
	if err := validateCoverageRetirementShape(retirementOf(
		"path:report.md#L1-L1", "unresolved", "non-claim", "", "It states no claim.")); err != nil {
		t.Fatalf("a retirement displacing an empty rationale was refused: %v", err)
	}
}

// TestRetirementIsMonotoneInReportCoverage pins the consequence of the
// permitted-change rule that closure depends on: a retirement can only ever
// move a source from uncovered to covered. If it could move one the other way,
// a bookkeeping transaction could silently un-close a campaign.
func TestRetirementIsMonotoneInReportCoverage(t *testing.T) {
	graph := retirementGraph(t)
	before := reviewedReportCoverage(graph)
	if len(before) != 0 {
		t.Fatalf("the fixture starts covered, so the test proves nothing: %#v", before)
	}
	once := retireInGraph(t, graph, "2026-08-02T21:00:00Z", retireThirdLine())
	middle := reviewedReportCoverage(once)
	for key := range before {
		if !middle[key] {
			t.Fatalf("the first retirement lost coverage key %q", key)
		}
	}
	if len(middle) != 0 {
		t.Fatalf("one unresolved span still remains, so the source must stay uncovered: %#v", middle)
	}
	twice := retireInGraph(t, once, "2026-08-02T21:05:00Z", retireFourthLine())
	after := reviewedReportCoverage(twice)
	for key := range middle {
		if !after[key] {
			t.Fatalf("the second retirement lost coverage key %q", key)
		}
	}
	key := reviewedRunCoverageKey("R-20260802-0001", *twice.Runs["R-20260802-0001"].Report)
	if !after[key] {
		t.Fatalf("retiring every unresolved span did not cover the report: %#v", after)
	}
	coverage, err := ComputeClosureCoverage(twice, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage["R-20260802-0001"] != "reviewed-intake" ||
		containsString(coverage.MissingDecisions, "run:R-20260802-0001:coverage") {
		t.Fatalf("closure still blocks on a fully disposed report: %#v", coverage)
	}
}

// TestUnamendedIntakeBytesAreUnchanged is the compatibility claim the
// `omitempty` on IntakeRecord.Amendments exists to make. The literals below
// were captured from a build of this engine taken before the field existed; if
// they still hold, no committed intake needs re-sealing and no campaign needs a
// migration to adopt this release.
func TestUnamendedIntakeBytesAreUnchanged(t *testing.T) {
	report := FileHandle{
		Path:   "active/test/runs/R-20260802-0001/report.md",
		SHA256: "sha256:" + strings.Repeat("cd", 32),
	}
	record := IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "I-0001", Revision: 2,
			CreatedAt: "2026-08-02T20:00:00Z", UpdatedAt: "2026-08-02T20:05:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "manager", CorrelationID: "corr-compat",
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{report},
		Coverage: []CoverageEntry{{
			SourceHandle: "path:" + report.Path + "#L1-L2", SourcePath: report.Path,
			SourceSHA256: report.SHA256, StartLine: 1, EndLine: 2, SourceLineCount: 2,
			Disposition: "non-claim",
		}},
		Triage: map[string]string{}, Status: "reviewed",
	}
	sealed, body, err := sealIntakeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	const preChangeDigest = "sha256:fb9722ab0dbe659e14d22d79fd1dbc7765aadb26c00c131b5b20cc497bd82435"
	const preChangeBody = `{
  "schemaVersion": 2,
  "id": "I-0001",
  "createdAt": "2026-08-02T20:00:00Z",
  "updatedAt": "2026-08-02T20:05:00Z",
  "revision": 2,
  "createdBy": "knowledge-curator",
  "updatedBy": "manager",
  "digest": "sha256:fb9722ab0dbe659e14d22d79fd1dbc7765aadb26c00c131b5b20cc497bd82435",
  "correlationId": "corr-compat",
  "campaignId": "C-TEST",
  "sourceRuns": [
    {
      "path": "active/test/runs/R-20260802-0001/report.md",
      "sha256": "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
    }
  ],
  "candidateFindingIds": [],
  "coverage": [
    {
      "sourceHandle": "path:active/test/runs/R-20260802-0001/report.md#L1-L2",
      "sourcePath": "active/test/runs/R-20260802-0001/report.md",
      "sourceSha256": "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
      "startLine": 1,
      "endLine": 2,
      "sourceLineCount": 2,
      "disposition": "non-claim"
    }
  ],
  "triage": {},
  "status": "reviewed"
}
`
	if got := sealed.(IntakeRecord).Digest; got != preChangeDigest {
		t.Fatalf("an unamended intake no longer digests to its pre-change value\n got: %s\nwant: %s", got, preChangeDigest)
	}
	if string(body) != preChangeBody {
		t.Fatalf("an unamended intake no longer serializes to its pre-change bytes\n got:\n%s\nwant:\n%s", body, preChangeBody)
	}
	if strings.Contains(string(body), "amendments") {
		t.Fatal("an unamended intake serialized an amendments key")
	}
}

// retirementUnitInputs builds the exact arguments the transaction journal hands
// to validateAppliedCoverageRetirement for a well-formed retirement, so a test
// can corrupt exactly one of them.
func retirementUnitInputs(t *testing.T) (CampaignGraph, StateTransactionRequest, IntakeRecord, CoverageAmendment) {
	t.Helper()
	graph := retirementGraph(t)
	prior := graph.Intakes["I-0001"]
	amendment := CoverageAmendment{
		Revision: prior.Revision + 1, AmendedAt: "2026-08-02T21:00:00Z", AmendedBy: "manager",
		CorrelationID: "corr-retire", ReviewID: "V-0001",
		Rationale:   "Give the span the curator left unjudged its terminal disposition.",
		Retirements: []CoverageRetirement{retireThirdLine()},
	}
	next, err := applyCoverageRetirements(prior, amendment)
	if err != nil {
		t.Fatal(err)
	}
	next.Revision++
	next.UpdatedAt, next.UpdatedBy, next.CorrelationID = amendment.AmendedAt, "manager", "corr-retire"
	next.Digest = ""
	sealed, _, err := sealIntakeRecord(next)
	if err != nil {
		t.Fatal(err)
	}
	request := StateTransactionRequest{
		CampaignSlug: "test", CampaignID: "C-TEST", Actor: "manager", Authority: "manager",
		Action: "intake.coverage.retire", CorrelationID: "corr-retire",
	}
	return graph, request, sealed.(IntakeRecord), amendment
}

func retirementWrites(records ...any) []preparedStateWrite {
	writes := make([]preparedStateWrite, 0, len(records))
	for _, record := range records {
		writes = append(writes, preparedStateWrite{Record: record})
	}
	return writes
}

// reseal is the escape hatch the adversarial cases need: a caller that mutates a
// record must still present a self-consistent one, or it would be refused for
// the wrong reason.
func resealIntake(t *testing.T, record IntakeRecord) IntakeRecord {
	t.Helper()
	record.Digest = ""
	sealed, _, err := sealIntakeRecord(record)
	if err != nil {
		t.Fatalf("reseal intake: %v", err)
	}
	return sealed.(IntakeRecord)
}

func TestAmendmentRefusesAnIntakeThatIsNotReviewed(t *testing.T) {
	graph, request, submitted, _ := retirementUnitInputs(t)
	if err := validateAppliedCoverageRetirement(graph, request, retirementWrites(submitted)); err != nil {
		t.Fatalf("the well-formed retirement was refused: %v", err)
	}
	// The prior record is not reviewed. curation_submit owns a draft or
	// submitted intake outright, so there is nothing here for a retirement to
	// preserve.
	for _, status := range []string{"draft", "submitted", "superseded"} {
		unreviewed := graph.Intakes["I-0001"]
		unreviewed.Status = status
		stale := cloneCampaignGraph(graph)
		stale.Intakes["I-0001"] = unreviewed
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted))
		if err == nil || !strings.Contains(err.Error(), "amends a reviewed intake and leaves it reviewed") {
			t.Fatalf("a %s prior intake was amended: %v", status, err)
		}
	}
	// And the submitted record must stay reviewed.
	demoted := submitted
	demoted.Status = "superseded"
	err := validateAppliedCoverageRetirement(graph, request, retirementWrites(demoted))
	if err == nil || !strings.Contains(err.Error(), "amends a reviewed intake and leaves it reviewed") {
		t.Fatalf("a retirement superseded the intake it amends: %v", err)
	}
	// An intake that is not canonical state at all cannot be amended into
	// existence.
	orphan := submitted
	orphan.ID = "I-0009"
	err = validateAppliedCoverageRetirement(graph, request, retirementWrites(orphan))
	if err == nil || !strings.Contains(err.Error(), "not canonical campaign state") {
		t.Fatalf("a retirement created an intake that did not exist: %v", err)
	}
}

func TestAmendmentRefusesAnUnboundOrForeignReview(t *testing.T) {
	graph, request, submitted, amendment := retirementUnitInputs(t)
	rebuild := func(t *testing.T, mutate func(*CoverageAmendment)) IntakeRecord {
		t.Helper()
		copied := amendment
		copied.Retirements = append([]CoverageRetirement(nil), amendment.Retirements...)
		mutate(&copied)
		record := submitted
		record.Amendments = []CoverageAmendment{copied}
		return resealIntake(t, record)
	}
	t.Run("a review that does not exist", func(t *testing.T) {
		record := rebuild(t, func(entry *CoverageAmendment) { entry.ReviewID = "V-9999" })
		err := validateAppliedCoverageRetirement(graph, request, retirementWrites(record))
		if err == nil || !strings.Contains(err.Error(), "is not the manager review that ratified intake") {
			t.Fatalf("an amendment named a review that does not exist: %v", err)
		}
	})
	// The receipts below are placed into the graph unsealed on purpose. Each one
	// is a record the engine would never have written, and the point is that the
	// retirement validator resolves and checks the receipt itself rather than
	// inheriting that judgment from whoever sealed it.
	t.Run("a review of another intake", func(t *testing.T) {
		foreign := graph.Reviews["V-0001"]
		foreign.ID, foreign.IntakeID = "V-0002", "I-0002"
		stale := cloneCampaignGraph(graph)
		stale.Reviews["V-0002"] = foreign
		record := rebuild(t, func(entry *CoverageAmendment) { entry.ReviewID = "V-0002" })
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(record))
		if err == nil || !strings.Contains(err.Error(), "is not the manager review that ratified intake") {
			t.Fatalf("an amendment borrowed another intake's review: %v", err)
		}
	})
	t.Run("a review of another campaign", func(t *testing.T) {
		foreign := graph.Reviews["V-0001"]
		foreign.CampaignID = "C-OTHER"
		stale := cloneCampaignGraph(graph)
		stale.Reviews["V-0001"] = foreign
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted))
		if err == nil || !strings.Contains(err.Error(), "is not the manager review that ratified intake") {
			t.Fatalf("an amendment borrowed another campaign's review: %v", err)
		}
	})
	t.Run("a review bound to the wrong revision", func(t *testing.T) {
		drifted := graph.Reviews["V-0001"]
		drifted.IntakeRevision = 2
		stale := cloneCampaignGraph(graph)
		stale.Reviews["V-0001"] = drifted
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted))
		if err == nil || !strings.Contains(err.Error(), "is not the manager review that ratified intake") {
			t.Fatalf("an amendment preserved a review that never bound this revision: %v", err)
		}
	})
	t.Run("a curator receipt", func(t *testing.T) {
		curator := graph.Reviews["V-0001"]
		curator.Authority = "curator"
		stale := cloneCampaignGraph(graph)
		stale.Reviews["V-0001"] = curator
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted))
		if err == nil || !strings.Contains(err.Error(), "is not the manager review that ratified intake") {
			t.Fatalf("a curator receipt was accepted as the ratification a retirement preserves: %v", err)
		}
	})
}

func TestAmendmentRefusesAClosedCampaign(t *testing.T) {
	graph, request, submitted, _ := retirementUnitInputs(t)
	for _, status := range []string{"closed", "cancelled", "paused"} {
		campaign := *graph.Campaign
		campaign.Status = status
		campaign.ClosedAt, campaign.PausedAt = closureTestTime, closureTestTime
		campaign.ArchiveDestination = "docs/history/campaigns/2026-08-02-test"
		stale := cloneCampaignGraph(graph)
		stale.Campaign = &campaign
		err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted))
		if err == nil || !strings.Contains(err.Error(), "live bookkeeping transaction") {
			t.Fatalf("a %s campaign accepted a coverage retirement: %v", status, err)
		}
	}
	for _, status := range []string{"open", "closing"} {
		campaign := *graph.Campaign
		campaign.Status = status
		stale := cloneCampaignGraph(graph)
		stale.Campaign = &campaign
		if err := validateAppliedCoverageRetirement(stale, request, retirementWrites(submitted)); err != nil {
			t.Fatalf("a %s campaign refused a coverage retirement: %v", status, err)
		}
	}
}

// TestAmendmentBindingsAreNotCallerControlled pins that only the two free-text
// fields are the caller's. Everything else on the entry restates something the
// transaction already fixed, and a log that could disagree with its own
// transaction would be a record of an event that never happened.
func TestAmendmentBindingsAreNotCallerControlled(t *testing.T) {
	graph, request, submitted, amendment := retirementUnitInputs(t)
	for _, testCase := range []struct {
		name   string
		mutate func(*CoverageAmendment)
		want   string
	}{
		{
			name:   "a revision the transaction is not writing",
			mutate: func(entry *CoverageAmendment) { entry.Revision = 99 },
			want:   "but the intake revision it is written at is",
		},
		{
			name:   "an actor who is not the one transacting",
			mutate: func(entry *CoverageAmendment) { entry.AmendedBy = "someone-else" },
			want:   "but the transaction actor is",
		},
		{
			name:   "a correlation the transaction does not carry",
			mutate: func(entry *CoverageAmendment) { entry.CorrelationID = "corr-other" },
			want:   "but the transaction correlation is",
		},
		{
			name:   "a timestamp the record does not carry",
			mutate: func(entry *CoverageAmendment) { entry.AmendedAt = "2020-01-01T00:00:00Z" },
			want:   "but the intake revision it is written at is timestamped",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			copied := amendment
			copied.Retirements = append([]CoverageRetirement(nil), amendment.Retirements...)
			testCase.mutate(&copied)
			record := submitted
			record.Amendments = []CoverageAmendment{copied}
			// The revision case cannot reseal, because ValidateIntake pins the
			// last amendment's revision to the record's own; that is itself the
			// refusal, so accept either gate.
			record.Digest = ""
			if sealed, _, err := sealIntakeRecord(record); err == nil {
				record = sealed.(IntakeRecord)
			}
			err := validateAppliedCoverageRetirement(graph, request, retirementWrites(record))
			if err == nil {
				t.Fatal("a caller-chosen amendment binding was accepted")
			}
			if !strings.Contains(err.Error(), testCase.want) &&
				!strings.Contains(err.Error(), "last coverage amendment is recorded at revision") {
				t.Fatalf("refusal does not report %q: %v", testCase.want, err)
			}
		})
	}
}

// TestManagerActionSurfaceCarriesTheRetirement pins the two bindings that make
// the transition reachable at all, next to the transition itself: an engine
// action no caller can name is not a capability, and one that states no
// obligation teaches its contract one terse refusal at a time.
func TestManagerActionSurfaceCarriesTheRetirement(t *testing.T) {
	if kinds, ok := managerActionKinds["intake.coverage.retire"]; !ok ||
		len(kinds) != 1 || !kinds["intake"] {
		t.Fatalf("the retirement does not accept exactly intake records: %#v", kinds)
	}
	obligation, ok := managerActionObligations["intake.coverage.retire"]
	if !ok || !strings.Contains(obligation, "No finding, review, or run may be part of it") {
		t.Fatalf("the retirement obligation does not state its exclusivity: %q", obligation)
	}
	// The permitted-pair rule is the entire content of this transition, so a
	// caller that can discover it only by tripping a refusal has been told the
	// least useful version of it. Publish it in the tool surface and pin it here,
	// because a reflected schema would silently render it as a bare string.
	var managerTool map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] == "manager_apply" {
			managerTool = tool
			break
		}
	}
	if managerTool == nil {
		t.Fatal("manager_apply tool is missing")
	}
	input := asObject(t, managerTool["inputSchema"])
	properties := asObject(t, input["properties"])
	// The intake record is published once under $defs and referenced from every
	// site that accepts one, so follow the pointer a real caller would follow.
	// Asserting through the reference rather than around it is the point: it
	// proves the enum reaches the schema callers actually resolve.
	intake := asObject(t, resolveSchemaRef(t, input, properties["intake"])["properties"])
	amendments, present := intake["amendments"]
	if !present {
		t.Fatal("the manager_apply intake schema omits the amendment log")
	}
	entry := asObject(t, asObject(t, amendments)["items"])
	retirements := asObject(t, asObject(t, entry["properties"])["retirements"])
	row := asObject(t, asObject(t, retirements["items"])["properties"])
	from := asObject(t, row["fromDisposition"])
	if from["const"] != "unresolved" {
		t.Fatalf("the schema does not pin fromDisposition to unresolved: %#v", from)
	}
	to, ok := asObject(t, row["toDisposition"])["enum"].([]string)
	if !ok || len(to) != 2 || !containsString(to, "non-claim") || !containsString(to, "out-of-scope") {
		t.Fatalf("the schema does not publish the exact legal target dispositions: %#v", to)
	}
}

// retirementFixture is the campaign this transition exists for, built through
// the live engine rather than by hand: one returned run whose curator intake was
// reviewed, whose two candidate findings a manager ratified, and which still
// leaves two trailing spans `unresolved`.
//
// Everything a retirement must not disturb is present - ratified findings, an
// immutable review receipt, and an intake a curator may no longer touch because
// `reviewed` has no edge back to `submitted`.
type retirementFixture struct {
	fixture    runPreparationFixture
	runID      string
	reportPath string
	intakeID   string
	reviewID   string
	findingIDs []string
	firstSpan  string
	secondSpan string
	head       StateTransactionReceipt
}

func (f retirementFixture) graph(t *testing.T) CampaignGraph {
	t.Helper()
	graph, err := f.fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func (f retirementFixture) recordPath(parts ...string) string {
	return filepath.Join(append([]string{f.fixture.root, "active", "test-campaign"}, parts...)...)
}

func newRetirementFixture(t *testing.T) retirementFixture {
	t.Helper()
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	runID := fixture.request.Runs[0].ID
	reportPath := "active/test-campaign/runs/" + runID + "/report.md"
	report := []byte("# VERIFIED OBSERVATION\n\n" +
		"The frame registry uses locator cobalt-seventeen in the measured build.\n\n" +
		"# LIMITS\nThis result applies only to the measured build.\n\n" +
		"An unclassified trailing note.\nA second unclassified trailing note.\n")
	reportDigest := "sha256:" + SHA256Bytes(report)

	preparedReceipt, err := fixture.service.ManagerApply(ctx, fixture.request)
	if err != nil {
		t.Fatalf("prepare the run: %v", err)
	}
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	prepared := graph.Runs[runID]
	running := prepared
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	running.Status, running.StartedAt = "running", "2026-08-02T18:01:00Z"
	work := graph.WorkItems["W-0001"]
	priorWork := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:01:00Z", "manager", prepared.CorrelationID)
	startedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.start", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: prepared.CorrelationID, IdempotencyKey: "idem-retire-start",
		ExpectedHeadRevision:  preparedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    preparedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{runID: prepared.Digest, "W-0001": priorWork},
		Runs:                  []RunRecord{running}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("start the run: %v", err)
	}

	writeFindingFixtureFile(t, fixture.root, reportPath, report)
	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	running = graph.Runs[runID]
	returned := running
	returned.RecordMeta = lifecycleAdvanceMeta(
		returned.RecordMeta, "2026-08-02T18:02:00Z", "manager", running.CorrelationID)
	returned.Status, returned.ReturnedAt = "returned", "2026-08-02T18:02:00Z"
	returned.Report = &FileHandle{Path: reportPath, SHA256: reportDigest}
	returned.ResultSummary = "Verified the frame registry locator."
	work = graph.WorkItems["W-0001"]
	priorWork = work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:02:00Z", "manager", running.CorrelationID)
	returnedReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "run.return", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: running.CorrelationID, IdempotencyKey: "idem-retire-return",
		ExpectedHeadRevision:  startedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    startedReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{runID: running.Digest, "W-0001": priorWork},
		Runs:                  []RunRecord{returned}, WorkItems: []WorkItemRecord{work},
	})
	if err != nil {
		t.Fatalf("return the run: %v", err)
	}

	correlation := "corr-retire-curation"
	ids := []string{"F-0101", "F-0102"}
	bounds := [][2]int{{1, 3}, {4, 6}}
	claims := []string{
		"The frame registry uses locator cobalt-seventeen in the measured build.",
		"The locator observation is scoped to the measured build identifier.",
	}
	candidates := make([]FindingDocument, 0, len(ids))
	submissions := make([]FindingSubmission, 0, len(ids))
	rows := make([]CurationRow, 0, len(ids))
	coverage := make([]CoverageEntry, 0, len(ids)+2)
	triage := map[string]string{}
	for index, id := range ids {
		document := testFindingDocument()
		document.Record.ID = id
		document.Record.CampaignID = "C-TEST"
		document.Record.CorrelationID = correlation
		document.Record.CreatedAt = "2026-08-02T18:03:00Z"
		document.Record.UpdatedAt = "2026-08-02T18:03:00Z"
		document.Record.CreatedBy = "knowledge-curator"
		document.Record.UpdatedBy = "knowledge-curator"
		document.Record.Path = "active/test-campaign/findings/" + id + ".md"
		document.Record.Subject = fmt.Sprintf("frame.registry.fact-%d", index+1)
		document.Record.Claim = claims[index]
		document.Record.SourceRuns = []string{runID}
		document.Record.Relations = FindingRelations{}
		handle := fmt.Sprintf("path:%s#L%d-L%d", reportPath, bounds[index][0], bounds[index][1])
		document.Record.Evidence = []EvidenceReference{{
			Path: reportPath, SHA256: reportDigest,
			StartLine: bounds[index][0], EndLine: bounds[index][1],
			ObjectKey: handle, SourceRun: runID,
		}}
		document.Record.Body = strings.Replace(document.Record.Body,
			"Resource registration uses the named table.", document.Record.Claim, 1)
		document.SyntheticQuestions = []string{
			fmt.Sprintf("What does frame registry fact %d establish?", index+1),
			fmt.Sprintf("Which measured locator fact is numbered %d?", index+1),
			fmt.Sprintf("How can frame registry fact %d be reproduced?", index+1),
		}
		body, renderErr := RenderFindingDocument(document)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		document, renderErr = ParseFindingDocument(body, document.Record.Path)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		candidates = append(candidates, document)
		submissions = append(submissions, FindingSubmission{
			Record: document.Record, Body: document.Record.Body, Path: document.Record.Path,
			SyntheticQuestions: document.SyntheticQuestions,
			QuestionsReviewed:  document.QuestionsReviewed,
		})
		rows = append(rows, CurationRow{FindingID: id, Triage: "routine"})
		coverage = append(coverage, CoverageEntry{
			SourceHandle: handle, SourcePath: reportPath, SourceSHA256: reportDigest,
			StartLine: bounds[index][0], EndLine: bounds[index][1], SourceLineCount: 9,
			Disposition: "candidate-finding", TargetID: id,
		})
		triage[id] = "routine"
	}
	firstSpan := fmt.Sprintf("path:%s#L7-L8", reportPath)
	secondSpan := fmt.Sprintf("path:%s#L9-L9", reportPath)
	coverage = append(coverage,
		CoverageEntry{
			SourceHandle: firstSpan, SourcePath: reportPath, SourceSHA256: reportDigest,
			StartLine: 7, EndLine: 8, SourceLineCount: 9,
			Disposition: "unresolved", Rationale: retirementUnresolvedRationale,
		},
		CoverageEntry{
			SourceHandle: secondSpan, SourcePath: reportPath, SourceSHA256: reportDigest,
			StartLine: 9, EndLine: 9, SourceLineCount: 9,
			Disposition: "unresolved", Rationale: retirementSecondRationale,
		})
	intake := IntakeRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "I-0101", Revision: 1,
			CreatedAt: "2026-08-02T18:03:00Z", UpdatedAt: "2026-08-02T18:03:00Z",
			CreatedBy: "knowledge-curator", UpdatedBy: "knowledge-curator",
			Digest: stateTestDigest("1"), CorrelationID: correlation,
		},
		CampaignID: "C-TEST", SourceRuns: []FileHandle{*returned.Report},
		CandidateFindingIDs: ids, Coverage: coverage, Triage: triage, Status: "submitted",
	}
	curationReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: correlation, IdempotencyKey: "idem-retire-curation",
		ExpectedHeadRevision: returnedReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   returnedReceipt.ResultingHead.Digest,
		Intake:               intake, Candidates: submissions, Rows: rows,
	})
	if err != nil {
		t.Fatalf("submit the curator intake: %v", err)
	}

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	committed := graph.Intakes[intake.ID]
	packet := CurationPacket{Intake: committed, Candidates: candidates, Rows: rows}
	envelope := testReviewPacketEnvelope(t, packet)
	reviewCorrelation := "corr-retire-review"
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0101", Revision: 1,
			CreatedAt: "2026-08-02T18:04:00Z", UpdatedAt: "2026-08-02T18:04:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", Digest: stateTestDigest("2"),
			CorrelationID: reviewCorrelation,
		},
		CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
		IntakeID: committed.ID, IntakeRevision: committed.Revision, PacketDigest: envelope.Digest,
		ReviewLoad: stateTestReviewLoad("V-0101", "C-TEST", envelope.Digest, len(ids), 0),
	}
	review.ReviewLoad.StartedAt = "2026-08-02T18:03:10Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T18:04:00Z"
	review.ReviewLoad.DurationSeconds = 50
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	expectedDigests := map[string]string{committed.ID: committed.Digest}
	outcomes := make([]FindingSubmission, 0, len(candidates))
	for _, candidate := range candidates {
		canonical := graph.Findings[candidate.Record.ID]
		expectedDigests[candidate.Record.ID] = canonical.Digest
		review.Decisions = append(review.Decisions, ReviewDecision{
			FindingID: candidate.Record.ID, FindingRevision: canonical.Revision,
			Action: "ratify", Projection: "campaign", Rationale: "Exact returned-run evidence verified.",
		})
		outcome := candidate
		outcome.Record.Revision++
		outcome.Record.UpdatedAt = "2026-08-02T18:04:00Z"
		outcome.Record.UpdatedBy = "manager"
		outcome.Record.CorrelationID = reviewCorrelation
		outcome.Record.Digest = ""
		outcome.Record.ReviewState = "manager-ratified"
		outcome.Record.Validity = "provisional"
		outcome.Record.Projection = "campaign"
		body, renderErr := RenderFindingDocument(outcome)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		outcome, renderErr = ParseFindingDocument(body, outcome.Record.Path)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		outcomes = append(outcomes, FindingSubmission{
			Record: outcome.Record, Body: outcome.Record.Body, Path: outcome.Record.Path,
			SyntheticQuestions: outcome.SyntheticQuestions,
			QuestionsReviewed:  outcome.QuestionsReviewed,
		})
	}
	reviewedIntake := committed
	reviewedIntake.RecordMeta = lifecycleAdvanceMeta(
		reviewedIntake.RecordMeta, "2026-08-02T18:04:00Z", "manager", reviewCorrelation)
	reviewedIntake.Digest, reviewedIntake.Status = stateTestDigest("3"), "reviewed"
	reviewReceipt, err := fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: reviewCorrelation, IdempotencyKey: "idem-retire-review",
		ExpectedHeadRevision:  curationReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    curationReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: expectedDigests,
		Intake:                &reviewedIntake, Review: &review, Findings: outcomes,
		ReviewPacket: &ReviewPacketSubmission{
			Envelope: envelope, Intake: committed, Candidates: submissions,
		},
	})
	if err != nil {
		t.Fatalf("ratify the curator packet: %v", err)
	}
	return retirementFixture{
		fixture: fixture, runID: runID, reportPath: reportPath,
		intakeID: intake.ID, reviewID: review.ID, findingIDs: ids,
		firstSpan: firstSpan, secondSpan: secondSpan, head: reviewReceipt,
	}
}

// coverageRetirementRequest assembles the transaction from committed state. The
// next intake revision is built by hand rather than through
// applyCoverageRetirements so that the engine's reconstruction is checked
// against an independently produced record rather than against itself.
func coverageRetirementRequest(
	t *testing.T,
	f retirementFixture,
	receipt StateTransactionReceipt,
	at, correlation, idempotency string,
	retirements ...CoverageRetirement,
) ManagerApplyRequest {
	t.Helper()
	committed := f.graph(t).Intakes[f.intakeID]
	next := committed
	next.Coverage = append([]CoverageEntry(nil), committed.Coverage...)
	for _, retirement := range retirements {
		moved := false
		for index := range next.Coverage {
			if next.Coverage[index].SourceHandle != retirement.SourceHandle {
				continue
			}
			next.Coverage[index].Disposition = retirement.ToDisposition
			next.Coverage[index].Rationale = retirement.ToRationale
			moved = true
		}
		if !moved {
			t.Fatalf("fixture intake does not carry span %s", retirement.SourceHandle)
		}
	}
	next.RecordMeta = lifecycleAdvanceMeta(committed.RecordMeta, at, "manager", correlation)
	next.Amendments = append(append([]CoverageAmendment(nil), committed.Amendments...), CoverageAmendment{
		Revision: next.Revision, AmendedAt: at, AmendedBy: "manager",
		CorrelationID: correlation, ReviewID: f.reviewID,
		Rationale:   "The trailing notes describe the probe's own status and assert nothing durable.",
		Retirements: retirements,
	})
	return ManagerApplyRequest{
		Action: "intake.coverage.retire", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: correlation, IdempotencyKey: idempotency,
		Rationale:             "Retire the spans the curator left unjudged.",
		ExpectedHeadRevision:  receipt.ResultingHead.Revision,
		ExpectedHeadDigest:    receipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{committed.ID: committed.Digest},
		Intake:                &next,
	}
}

func (f retirementFixture) retireBothSpans() []CoverageRetirement {
	return []CoverageRetirement{
		retirementOf(f.firstSpan, "unresolved", "non-claim",
			retirementUnresolvedRationale, retirementNewRationale),
		retirementOf(f.secondSpan, "unresolved", "out-of-scope",
			retirementSecondRationale, retirementNewRationale),
	}
}

// TestRetiringAnUnresolvedSpanClearsClosureWithoutTouchingFindings is the
// motivating case end to end.
//
// Seven spans needed retiring in the campaign that produced this design. The
// only supported route was a supplementary curator intake, and because a
// curator may not resubmit an already-ratified finding, that route dragged
// nineteen ratified findings back through a re-ratification nobody re-read.
// This asserts the opposite: closure flips, and every other canonical record in
// the campaign is byte-identical afterwards.
func TestRetiringAnUnresolvedSpanClearsClosureWithoutTouchingFindings(t *testing.T) {
	fixture := newRetirementFixture(t)
	graph := fixture.graph(t)
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[fixture.runID] != "missing-reviewed-intake" ||
		!containsString(coverage.MissingDecisions, "run:"+fixture.runID+":coverage") {
		t.Fatalf("the fixture does not start blocked on unresolved coverage: %s",
			coverage.SourceRunCoverage[fixture.runID])
	}

	reviewPath := fixture.recordPath("reviews", fixture.reviewID+".json")
	priorReview := mustReadFile(t, reviewPath)
	priorFindings := map[string][]byte{}
	priorFindingRecords := map[string]FindingRecord{}
	for _, id := range fixture.findingIDs {
		priorFindings[id] = mustReadFile(t, fixture.recordPath("findings", id+".md"))
		priorFindingRecords[id] = graph.Findings[id]
	}
	priorRun := mustReadFile(t, fixture.recordPath("runs", fixture.runID, "run.json"))

	if _, err := fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, fixture.head,
			"2026-08-02T18:05:00Z", "corr-retire-spans", "idem-retire-spans",
			fixture.retireBothSpans()...)); err != nil {
		t.Fatalf("retire the unresolved spans: %v", err)
	}

	graph = fixture.graph(t)
	if err := graph.Validate(); err != nil {
		t.Fatalf("the campaign graph no longer validates after a retirement: %v", err)
	}
	amended := graph.Intakes[fixture.intakeID]
	if amended.Revision != 3 || len(amended.Amendments) != 1 ||
		len(amended.Amendments[0].Retirements) != 2 {
		t.Fatalf("the retirement did not advance one revision with one two-span amendment: %+v", amended)
	}
	if !reviewBindsIntake(graph, amended) {
		t.Fatal("the review no longer binds the intake it ratified")
	}
	if intakeReviewedRevision(amended) != graph.Reviews[fixture.reviewID].IntakeRevision {
		t.Fatalf("the reviewed revision moved: %d vs review %d",
			intakeReviewedRevision(amended), graph.Reviews[fixture.reviewID].IntakeRevision)
	}
	for _, entry := range amended.Coverage {
		if entry.Disposition == "unresolved" {
			t.Fatalf("span %s is still unresolved", entry.SourceHandle)
		}
	}

	coverage, err = ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[fixture.runID] != "reviewed-intake" ||
		containsString(coverage.MissingDecisions, "run:"+fixture.runID+":coverage") {
		t.Fatalf("the retirement did not clear closure coverage: %#v", coverage)
	}

	// Nothing the manager already ratified moved. Compare the committed bytes,
	// not a projection of them.
	if string(mustReadFile(t, reviewPath)) != string(priorReview) {
		t.Fatal("the immutable review receipt changed during a coverage retirement")
	}
	if string(mustReadFile(t, fixture.recordPath("runs", fixture.runID, "run.json"))) != string(priorRun) {
		t.Fatal("the source run record changed during a coverage retirement")
	}
	for _, id := range fixture.findingIDs {
		if string(mustReadFile(t, fixture.recordPath("findings", id+".md"))) != string(priorFindings[id]) {
			t.Fatalf("finding %s was rewritten by a coverage retirement", id)
		}
		before, after := priorFindingRecords[id], graph.Findings[id]
		if before.Revision != after.Revision || before.Digest != after.Digest ||
			before.ReviewState != after.ReviewState || before.Validity != after.Validity {
			t.Fatalf("finding %s standing changed: %+v -> %+v", id, before, after)
		}
	}
	if len(graph.Intakes) != 1 {
		t.Fatalf("the retirement created a supplementary intake: %d intakes", len(graph.Intakes))
	}
}

// TestRetirementUnblocksRunCompleteWithoutASupplementaryIntake pins the
// operational consequence: the transition that the coverage gate refused now
// succeeds, and it succeeds without any second curator packet or second review.
func TestRetirementUnblocksRunCompleteWithoutASupplementaryIntake(t *testing.T) {
	fixture := newRetirementFixture(t)
	blocked := completeReturnedRun(t, fixture.fixture, fixture.head, "idem-retire-complete-blocked")
	if blocked == nil {
		t.Fatal("run.complete accepted a run whose only intake left spans unresolved")
	}
	for _, want := range []string{
		fixture.intakeID + " leaves 2 span(s) unresolved",
		"manager_apply intake.coverage.retire",
	} {
		if !strings.Contains(blocked.Error(), want) {
			t.Fatalf("the refusal does not report %q: %v", want, blocked)
		}
	}

	retired, err := fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, fixture.head,
			"2026-08-02T18:05:00Z", "corr-retire-spans", "idem-retire-spans",
			fixture.retireBothSpans()...))
	if err != nil {
		t.Fatalf("retire the unresolved spans: %v", err)
	}
	if err := completeReturnedRun(t, fixture.fixture, retired, "idem-retire-complete"); err != nil {
		t.Fatalf("the documented remedy did not clear the refusal: %v", err)
	}
	graph := fixture.graph(t)
	if graph.Runs[fixture.runID].Status != "completed" {
		t.Fatalf("the run did not reach completed: %s", graph.Runs[fixture.runID].Status)
	}
	if len(graph.Intakes) != 1 || len(graph.Reviews) != 1 {
		t.Fatalf("clearing the gate cost a further intake or review: %d intakes, %d reviews",
			len(graph.Intakes), len(graph.Reviews))
	}
}

// refuseRetirement submits one corrupted retirement through the live engine and
// requires both that it is refused and that the campaign is exactly where it
// was. A refusal that still moved the record would be worse than an acceptance,
// because nothing would report it.
func refuseRetirement(
	t *testing.T,
	f retirementFixture,
	idempotency string,
	mutate func(*ManagerApplyRequest),
	wants ...string,
) {
	t.Helper()
	before := f.graph(t).Intakes[f.intakeID]
	request := coverageRetirementRequest(t, f, f.head,
		"2026-08-02T18:05:00Z", "corr-retire-spans", idempotency, f.retireBothSpans()...)
	mutate(&request)
	_, err := f.fixture.service.ManagerApply(context.Background(), request)
	if err == nil {
		t.Fatal("a corrupted coverage retirement was accepted")
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal does not report %q: %v", want, err)
		}
	}
	after := f.graph(t).Intakes[f.intakeID]
	if after.Revision != before.Revision || after.Digest != before.Digest {
		t.Fatalf("a refused retirement still moved the intake: %d/%s -> %d/%s",
			before.Revision, before.Digest, after.Revision, after.Digest)
	}
}

// coverageRowIndex locates one span in a submitted record so a test can corrupt
// exactly that row.
func coverageRowIndex(t *testing.T, record *IntakeRecord, handle string) int {
	t.Helper()
	for index := range record.Coverage {
		if record.Coverage[index].SourceHandle == handle {
			return index
		}
	}
	t.Fatalf("submitted intake does not carry span %s", handle)
	return -1
}

// TestAmendmentCannotRetargetACandidateFindingRow closes the most direct route:
// naming a row that a ratified finding rests on. The refusal is the
// permitted-pair table, and it fires before the engine has loaded anything, so
// there is no state in which this could ever be evaluated differently.
func TestAmendmentCannotRetargetACandidateFindingRow(t *testing.T) {
	fixture := newRetirementFixture(t)
	candidateSpan := "path:" + fixture.reportPath + "#L1-L3"
	for _, target := range []string{"non-claim", "out-of-scope", "duplicate", "unresolved"} {
		refuseRetirement(t, fixture, "idem-retire-retarget-"+target,
			func(request *ManagerApplyRequest) {
				amendments := append([]CoverageAmendment(nil), request.Intake.Amendments...)
				last := amendments[len(amendments)-1]
				last.Retirements = []CoverageRetirement{retirementOf(
					candidateSpan, "candidate-finding", target, "", "It is not really evidence.")}
				amendments[len(amendments)-1] = last
				request.Intake.Amendments = amendments
			},
			"may not move disposition", "candidate-finding")
	}
}

// TestAmendmentCannotChangeWhichFindingASpanSupports is the same attack made
// quietly: a legitimate retirement carried alongside a re-pointed targetId.
// Only reconstruction catches this, because the amendment itself is impeccable.
func TestAmendmentCannotChangeWhichFindingASpanSupports(t *testing.T) {
	fixture := newRetirementFixture(t)
	// The two candidate targets are swapped, so every candidate still has a
	// candidate-finding span and the record still validates on its own terms.
	// What has changed is which report bytes each ratified finding rests on,
	// and only reconstruction sees it.
	refuseRetirement(t, fixture, "idem-retire-repoint",
		func(request *ManagerApplyRequest) {
			first := coverageRowIndex(t, request.Intake, "path:"+fixture.reportPath+"#L1-L3")
			second := coverageRowIndex(t, request.Intake, "path:"+fixture.reportPath+"#L4-L6")
			request.Intake.Coverage[first].TargetID = "F-0102"
			request.Intake.Coverage[second].TargetID = "F-0101"
		},
		"is not the record its coverage amendment produces", "coverage")
}

// TestAmendmentCannotAddOrRemoveACoverageRow covers the shapes that keep the
// tiling valid while changing what the record says: splitting a row, and
// deleting one while widening its neighbour to close the gap. Each would pass a
// diff that only compared the rows it recognized.
//
// Two independent gates close them, and the sub-tests assert which one fires
// for which shape rather than blurring the two. Re-cutting a row the amendment
// names destroys the handle the amendment quotes, so the append-only log check
// catches it at load; re-cutting a row the amendment does not name leaves the
// log consistent and is caught by reconstruction.
func TestAmendmentCannotAddOrRemoveACoverageRow(t *testing.T) {
	fixture := newRetirementFixture(t)
	t.Run("a retired span is split into two rows", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-split",
			func(request *ManagerApplyRequest) {
				index := coverageRowIndex(t, request.Intake, fixture.firstSpan)
				row := request.Intake.Coverage[index]
				first, second := row, row
				first.EndLine, first.SourceHandle = 7, "path:"+fixture.reportPath+"#L7-L7"
				second.StartLine, second.SourceHandle = 8, "path:"+fixture.reportPath+"#L8-L8"
				rows := append([]CoverageEntry(nil), request.Intake.Coverage[:index]...)
				rows = append(rows, first, second)
				rows = append(rows, request.Intake.Coverage[index+1:]...)
				request.Intake.Coverage = rows
			},
			"retires span "+fixture.firstSpan, "which it does not cover")
	})
	t.Run("a retired row is deleted and its neighbour widened", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-absorb",
			func(request *ManagerApplyRequest) {
				keep := coverageRowIndex(t, request.Intake, fixture.firstSpan)
				drop := coverageRowIndex(t, request.Intake, fixture.secondSpan)
				request.Intake.Coverage[keep].EndLine = 9
				request.Intake.Coverage[keep].SourceHandle = "path:" + fixture.reportPath + "#L7-L9"
				rows := append([]CoverageEntry(nil), request.Intake.Coverage[:drop]...)
				rows = append(rows, request.Intake.Coverage[drop+1:]...)
				request.Intake.Coverage = rows
			},
			"retires span "+fixture.firstSpan, "which it does not cover")
	})
	t.Run("an unnamed evidence row is split behind an honest retirement", func(t *testing.T) {
		// Both halves still target F-0101, so the record tiles, every handle is
		// canonical, every candidate keeps a candidate-finding span, and the
		// amendment describes exactly what it did. Only rebuilding the record
		// from the prior revision reveals that a ratified finding's evidence
		// spans were re-cut.
		refuseRetirement(t, fixture, "idem-retire-split-unnamed",
			func(request *ManagerApplyRequest) {
				index := coverageRowIndex(t, request.Intake, "path:"+fixture.reportPath+"#L1-L3")
				row := request.Intake.Coverage[index]
				first, second := row, row
				first.EndLine, first.SourceHandle = 1, "path:"+fixture.reportPath+"#L1-L1"
				second.StartLine, second.SourceHandle = 2, "path:"+fixture.reportPath+"#L2-L3"
				rows := append([]CoverageEntry(nil), request.Intake.Coverage[:index]...)
				rows = append(rows, first, second)
				rows = append(rows, request.Intake.Coverage[index+1:]...)
				request.Intake.Coverage = rows
			},
			"is not the record its coverage amendment produces", "coverage")
	})
}

// TestAmendmentCannotMoveASpanOrItsSource pins that a retirement cannot change
// which bytes the record is about. Re-pointing the source digest would leave a
// record that still tiles and still validates, while silently claiming coverage
// over a report nobody read.
func TestAmendmentCannotMoveASpanOrItsSource(t *testing.T) {
	fixture := newRetirementFixture(t)
	forged := "sha256:" + strings.Repeat("b", 64)
	refuseRetirement(t, fixture, "idem-retire-move-source",
		func(request *ManagerApplyRequest) {
			request.Intake.SourceRuns = []FileHandle{{Path: fixture.reportPath, SHA256: forged}}
			rows := append([]CoverageEntry(nil), request.Intake.Coverage...)
			for index := range rows {
				rows[index].SourceSHA256 = forged
			}
			request.Intake.Coverage = rows
		},
		"is not the record its coverage amendment produces", "sourceRuns", "coverage")
}

// TestAmendmentCannotEditAnUnnamedRow is the whole point of naming rows at all.
// A row the amendment does not mention is a row the manager did not decide
// anything about.
func TestAmendmentCannotEditAnUnnamedRow(t *testing.T) {
	fixture := newRetirementFixture(t)
	refuseRetirement(t, fixture, "idem-retire-unnamed",
		func(request *ManagerApplyRequest) {
			index := coverageRowIndex(t, request.Intake, "path:"+fixture.reportPath+"#L1-L3")
			request.Intake.Coverage[index].Rationale = "Reconsidered without saying so."
		},
		"is not the record its coverage amendment produces", "coverage")
}

// TestAmendmentCannotMisquoteWhatItDisplaced pins the compare-and-swap half of
// the rule. Quoting the prior values is what turns the amendment from an
// overwrite into a claim about the record it read, and a caller working from a
// stale copy must be refused rather than allowed to clobber.
func TestAmendmentCannotMisquoteWhatItDisplaced(t *testing.T) {
	fixture := newRetirementFixture(t)
	refuseRetirement(t, fixture, "idem-retire-misquote",
		func(request *ManagerApplyRequest) {
			amendments := append([]CoverageAmendment(nil), request.Intake.Amendments...)
			last := amendments[len(amendments)-1]
			retirements := append([]CoverageRetirement(nil), last.Retirements...)
			retirements[0].FromRationale = "A rationale the committed record never carried."
			last.Retirements = retirements
			amendments[len(amendments)-1] = last
			request.Intake.Amendments = amendments
		},
		"an amendment must quote the exact values it displaces")

	// A span can leave `unresolved` exactly once, so a replayed retirement has
	// nothing left to displace. The log check refuses it at load, before the
	// quoting rule is even reached, and names the judgment already recorded.
	retired, err := fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, fixture.head,
			"2026-08-02T18:05:00Z", "corr-retire-spans", "idem-retire-spans",
			fixture.retireBothSpans()...))
	if err != nil {
		t.Fatalf("retire the unresolved spans: %v", err)
	}
	_, err = fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, retired,
			"2026-08-02T18:06:00Z", "corr-retire-replay", "idem-retire-replay",
			fixture.retireBothSpans()...))
	if err == nil || !strings.Contains(err.Error(), "twice; it was already retired to non-claim") {
		t.Fatalf("a replayed retirement overwrote a judgment already recorded: %v", err)
	}
	// And a fresh amendment that quotes `unresolved` for a span that has since
	// been judged is refused by the quoting rule itself.
	graph := fixture.graph(t)
	prior := graph.Intakes[fixture.intakeID]
	stale := CoverageAmendment{
		Revision: prior.Revision + 1, AmendedAt: "2026-08-02T18:06:00Z", AmendedBy: "manager",
		CorrelationID: "corr-retire-replay", ReviewID: fixture.reviewID,
		Rationale:   "A retirement built from a copy of the record read before the first one landed.",
		Retirements: []CoverageRetirement{fixture.retireBothSpans()[0]},
	}
	if _, err := applyCoverageRetirements(prior, stale); err == nil ||
		!strings.Contains(err.Error(), "an amendment must quote the exact values it displaces") {
		t.Fatalf("a stale amendment overwrote a judgment already recorded: %v", err)
	}
}

// TestAmendmentCannotRewriteItsOwnHistory pins the append-only property against
// the subtle version of the attack: appending a legitimate new entry while
// editing an old one in the same transaction.
func TestAmendmentCannotRewriteItsOwnHistory(t *testing.T) {
	fixture := newRetirementFixture(t)
	first := retirementOf(fixture.firstSpan, "unresolved", "non-claim",
		retirementUnresolvedRationale, retirementNewRationale)
	second := retirementOf(fixture.secondSpan, "unresolved", "out-of-scope",
		retirementSecondRationale, retirementNewRationale)
	retired, err := fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, fixture.head,
			"2026-08-02T18:05:00Z", "corr-retire-first", "idem-retire-first", first))
	if err != nil {
		t.Fatalf("retire the first span: %v", err)
	}
	committed := fixture.graph(t).Intakes[fixture.intakeID]
	if len(committed.Amendments) != 1 {
		t.Fatalf("the first retirement did not append one amendment: %+v", committed.Amendments)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*CoverageAmendment)
	}{
		{"its rationale", func(entry *CoverageAmendment) { entry.Rationale = "A different reason, recorded later." }},
		{"the review it preserved", func(entry *CoverageAmendment) { entry.ReviewID = "V-0999" }},
		{"the actor who recorded it", func(entry *CoverageAmendment) { entry.AmendedBy = "someone-else" }},
		{"the span it retired", func(entry *CoverageAmendment) {
			entry.Retirements = []CoverageRetirement{second}
		}},
	} {
		t.Run("rewriting "+testCase.name, func(t *testing.T) {
			request := coverageRetirementRequest(t, fixture, retired,
				"2026-08-02T18:06:00Z", "corr-retire-second",
				"idem-retire-rewrite-"+strings.ReplaceAll(testCase.name, " ", "-"), second)
			amendments := append([]CoverageAmendment(nil), request.Intake.Amendments...)
			rewritten := amendments[0]
			rewritten.Retirements = append([]CoverageRetirement(nil), amendments[0].Retirements...)
			testCase.mutate(&rewritten)
			amendments[0] = rewritten
			request.Intake.Amendments = amendments
			_, err := fixture.fixture.service.ManagerApply(context.Background(), request)
			if err == nil {
				t.Fatal("a retirement rewrote an earlier amendment")
			}
			if !strings.Contains(err.Error(), "the log is append-only") &&
				!strings.Contains(err.Error(), "does not carry the disposition and rationale") &&
				!strings.Contains(err.Error(), "twice") {
				t.Fatalf("refusal is not the append-only guard: %v", err)
			}
		})
	}
	// The honest second retirement still works.
	if _, err := fixture.fixture.service.ManagerApply(context.Background(),
		coverageRetirementRequest(t, fixture, retired,
			"2026-08-02T18:06:00Z", "corr-retire-second", "idem-retire-second", second)); err != nil {
		t.Fatalf("an honest second retirement was refused: %v", err)
	}
	final := fixture.graph(t).Intakes[fixture.intakeID]
	if final.Revision != 4 || len(final.Amendments) != 2 {
		t.Fatalf("two retirements did not produce revision 4 with two amendments: %+v", final.RecordMeta)
	}
	if !reflect.DeepEqual(final.Amendments[0], committed.Amendments[0]) {
		t.Fatal("the first amendment changed when the second was appended")
	}
}

// TestAmendmentCannotEditCandidatesTriageStatusOrProposals walks the fields a
// retirement has no business touching. Each is a separate route because each
// would be reached by a different mistake.
func TestAmendmentCannotEditCandidatesTriageStatusOrProposals(t *testing.T) {
	fixture := newRetirementFixture(t)
	t.Run("triage", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-triage",
			func(request *ManagerApplyRequest) {
				triage := map[string]string{}
				for id, value := range request.Intake.Triage {
					triage[id] = value
				}
				triage["F-0101"] = "attention"
				request.Intake.Triage = triage
			},
			"is not the record its coverage amendment produces", "triage")
	})
	t.Run("status", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-status",
			func(request *ManagerApplyRequest) { request.Intake.Status = "superseded" },
			"amends a reviewed intake and leaves it reviewed")
	})
	t.Run("proposed duplicates", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-proposals",
			func(request *ManagerApplyRequest) {
				request.Intake.ProposedDuplicates = [][]string{{"F-0101", "F-0102"}}
			},
			"is not the record its coverage amendment produces", "proposedDuplicates")
	})
	t.Run("uncertainties", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-uncertainties",
			func(request *ManagerApplyRequest) {
				request.Intake.Uncertainties = []string{"A doubt introduced after ratification."}
			},
			"is not the record its coverage amendment produces", "uncertainties")
	})
	t.Run("candidates", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-candidates",
			func(request *ManagerApplyRequest) {
				request.Intake.CandidateFindingIDs = append(
					append([]string(nil), request.Intake.CandidateFindingIDs...), "F-0103")
				triage := map[string]string{"F-0103": "routine"}
				for id, value := range request.Intake.Triage {
					triage[id] = value
				}
				request.Intake.Triage = triage
			},
			"has no candidate-finding coverage span")
	})
}

// TestAmendmentCannotCarryFindingsReviewsOrRuns pins the exclusivity that makes
// the transition safe to read as bookkeeping. If a retirement could carry a
// finding revision, then "no finding moved" would be a claim about the caller's
// restraint rather than about the engine.
func TestAmendmentCannotCarryFindingsReviewsOrRuns(t *testing.T) {
	fixture := newRetirementFixture(t)
	graph := fixture.graph(t)
	t.Run("a finding", func(t *testing.T) {
		finding := graph.Findings["F-0101"]
		refuseRetirement(t, fixture, "idem-retire-carry-finding",
			func(request *ManagerApplyRequest) {
				request.Findings = []FindingSubmission{{
					Record: finding, Body: finding.Body, Path: finding.Path,
				}}
			},
			"cannot write finding records")
	})
	t.Run("a review", func(t *testing.T) {
		review := graph.Reviews[fixture.reviewID]
		refuseRetirement(t, fixture, "idem-retire-carry-review",
			func(request *ManagerApplyRequest) { request.Review = &review },
			"cannot write review records")
	})
	t.Run("a run", func(t *testing.T) {
		run := graph.Runs[fixture.runID]
		refuseRetirement(t, fixture, "idem-retire-carry-run",
			func(request *ManagerApplyRequest) { request.Runs = []RunRecord{run} },
			"cannot write run records")
	})
	t.Run("a work item", func(t *testing.T) {
		work := graph.WorkItems["W-0001"]
		refuseRetirement(t, fixture, "idem-retire-carry-work",
			func(request *ManagerApplyRequest) { request.WorkItems = []WorkItemRecord{work} },
			"cannot write work records")
	})
	t.Run("a review packet", func(t *testing.T) {
		refuseRetirement(t, fixture, "idem-retire-carry-packet",
			func(request *ManagerApplyRequest) {
				request.ReviewPacket = &ReviewPacketSubmission{Intake: graph.Intakes[fixture.intakeID]}
			},
			"carries neither a review packet nor a run preparation")
	})
}
