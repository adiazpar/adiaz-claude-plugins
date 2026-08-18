package knowledge

import (
	"context"
	"strings"
	"testing"
)

// TestRatificationRefusesUnresolvedCoverage is the fail-fast half of the
// unresolved-span defect, stated end to end.
//
// The slow half is already pinned by
// TestUnresolvedCoverageStrandsACompletedRunPermanently: one unresolved span
// makes a run's closure coverage permanently unsatisfiable. What this test adds
// is the moment the cost is paid. Before this rule, an intake could be ratified
// with unjudged spans and nothing said so until the closure gate refused, often
// weeks later and always in front of somebody who had never read the report. The
// refusal has to arrive while the packet is still open, name the exact spans, and
// name a transition that resolves them - a refusal that says only "unresolved
// coverage" moves the dead end rather than removing it.
func TestRatificationRefusesUnresolvedCoverage(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)
	runID := fixture.request.Runs[0].ID

	dirty := completionTestIntake("I-0801", "corr-ratify-dirty", *source.run.Report, "unresolved")
	dirtyReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: dirty.CorrelationID, IdempotencyKey: "idem-ratify-dirty",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               dirty,
	})
	if err != nil {
		// Curation still accepts an unjudged span. It has to: a curator that
		// cannot say "I could not decide this" would say "non-claim" instead, and
		// the record would lose the only honest thing it had.
		t.Fatalf("curation refused to record an unjudged span: %v", err)
	}

	ratification := buildIntakeRatification(t, fixture, "I-0801", "V-0801",
		"corr-ratify-dirty-review", "2026-08-02T18:07:00Z", 1)
	_, err = fixture.service.ManagerApply(ctx, ManagerApplyRequest{
		Action: "review.submit", Actor: "manager", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "corr-ratify-dirty-review", IdempotencyKey: "idem-ratify-dirty-review",
		Rationale:             "The curator packet is accepted into the campaign record.",
		ExpectedHeadRevision:  dirtyReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:    dirtyReceipt.ResultingHead.Digest,
		ExpectedRecordDigests: map[string]string{ratification.Committed.ID: ratification.Committed.Digest},
		Intake:                &ratification.Reviewed, Review: &ratification.Review,
		ReviewPacket: &ReviewPacketSubmission{
			Envelope: ratification.Envelope, Intake: ratification.Committed,
		},
	})
	if err == nil {
		t.Fatal("a packet leaving a span unjudged was ratified")
	}
	for _, want := range []string{
		"I-0801 cannot be ratified",
		"1 coverage span(s) are still unresolved",
		"path:" + source.run.Report.Path + "#L3-L3",
		"missing-reviewed-intake",
		"curation_submit",
		"candidate-finding, duplicate, non-claim, or out-of-scope",
		"manager_apply intake.coverage.retire",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the ratification refusal does not report %q: %v", want, err)
		}
	}

	// A refused ratification leaves the campaign exactly where it was: the intake
	// is still curator-owned and still resubmittable, and no review receipt was
	// minted for a decision that did not happen.
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if graph.Intakes["I-0801"].Status != "submitted" ||
		graph.Intakes["I-0801"].Digest != ratification.Committed.Digest {
		t.Fatalf("the refused ratification moved the intake: %+v", graph.Intakes["I-0801"])
	}
	if _, present := graph.Reviews["V-0801"]; present {
		t.Fatal("the refused ratification committed a review receipt")
	}

	// The remedy the refusal names has to work, and has to leave closure
	// satisfiable. This is the whole reason to refuse early rather than late: the
	// span is still a curator's to dispose.
	clean := completionTestIntake("I-0802", "corr-ratify-clean", *source.run.Report, "non-claim")
	cleanReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: clean.CorrelationID, IdempotencyKey: "idem-ratify-clean",
		ExpectedHeadRevision: dirtyReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   dirtyReceipt.ResultingHead.Digest,
		Intake:               clean,
	})
	if err != nil {
		t.Fatalf("the named remedy was refused: %v", err)
	}
	// Packet ordinal 1: the refused ratification committed no review, so this is
	// still the first packet of the manager's review session.
	reviewCommittedIntake(t, fixture, cleanReceipt, "I-0802", "V-0802",
		"corr-ratify-clean-review", "idem-ratify-clean-review", "2026-08-02T18:09:00Z", 1)

	graph, err = fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := ComputeClosureCoverage(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.SourceRunCoverage[runID] != "reviewed-intake" {
		t.Fatalf("the remedy did not clear closure coverage: %s", coverage.SourceRunCoverage[runID])
	}
}

// TestHistoricalReviewedIntakeWithUnresolvedCoverageStillLoads is the
// adversarial test for where the rule was put, and it is the reason it was not
// put in ValidateIntake.
//
// decodeCanonicalRecord runs record validation on every load, and
// LoadCampaignGraph fails whole. A rule in ValidateIntake would therefore have
// converted "this campaign has some unjudged spans" into "this campaign no
// longer exists", for every campaign ratified before the rule shipped. There is
// no migration out of that: the records are correct, canonical, digest-verified
// bytes, and the engine would simply refuse to read them.
//
// So the fixture installs exactly such a record and then asserts the boring
// thing: it loads, it validates, it is still `reviewed`, and it still carries
// its unjudged span. It also asserts that the reviewed -> reviewed edge remains
// open for it, because that edge is the repair path - intake.coverage.retire -
// and a rule that closed it would strand the same records a different way.
func TestHistoricalReviewedIntakeWithUnresolvedCoverageStillLoads(t *testing.T) {
	ctx := context.Background()
	fixture := newRunPreparationFixture(t)
	source := returnNormalizationSourceRun(t, fixture)

	dirty := completionTestIntake("I-0811", "corr-legacy-load", *source.run.Report, "unresolved")
	dirtyReceipt, err := fixture.service.CurationSubmit(ctx, CurationSubmitRequest{
		Actor: "knowledge-curator", CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: dirty.CorrelationID, IdempotencyKey: "idem-legacy-load",
		ExpectedHeadRevision: source.receipt.ResultingHead.Revision,
		ExpectedHeadDigest:   source.receipt.ResultingHead.Digest,
		Intake:               dirty,
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewCommittedIntakeAsLegacy(t, fixture, dirtyReceipt, "I-0811", "V-0811",
		"corr-legacy-load-review", "idem-legacy-load-review", "2026-08-02T18:07:00Z", 1)

	// The load itself is the assertion. If the rule were a record rule this call
	// would fail, and so would every other read of this campaign.
	graph, err := fixture.store.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatalf("a reviewed intake with an unresolved span stopped loading: %v", err)
	}
	survivor, present := graph.Intakes["I-0811"]
	if !present || survivor.Status != "reviewed" {
		t.Fatalf("the historical reviewed intake did not survive: %+v", survivor)
	}
	handles := unresolvedCoverageHandles(survivor)
	if len(handles) != 1 {
		t.Fatalf("the historical record's unjudged span was rewritten on load: %v", handles)
	}
	if err := ValidateIntake(survivor); err != nil {
		t.Fatalf("the record validator rejected a committed historical intake: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("the campaign graph rejected a committed historical intake: %v", err)
	}
	if _, _, err := fixture.store.ReadCanonicalRecord(
		"active/test-campaign/intake/I-0811.json"); err != nil {
		t.Fatalf("the committed bytes no longer re-encode to themselves: %v", err)
	}

	// And the repair edge is still open. A retirement advances a reviewed intake
	// to a further reviewed revision, so if the new rule covered that edge too,
	// the only supported way to give these spans a judgment would be closed.
	amended := survivor
	amended.Revision++
	amended.UpdatedAt, amended.UpdatedBy = "2026-08-02T18:10:00Z", "manager"
	if err := ValidateIntakeTransition(&survivor, amended); err != nil {
		t.Fatalf("the amendment edge is closed for the records it exists to repair: %v", err)
	}
}

// ratificationEdgeIntake builds one minimally valid intake for the transition
// table below. Only the fields the rule reads vary.
func ratificationEdgeIntake(status, disposition string, revision int64) IntakeRecord {
	report := FileHandle{
		Path:   "active/test-campaign/runs/R-20260802-0091/report.md",
		SHA256: stateTestDigest("8"),
	}
	intake := completionTestIntake("I-0001", "corr-ratify-edges", report, disposition)
	intake.Status, intake.Revision = status, revision
	// ValidateIntake demands a rationale for the dispositions that assert
	// something about why a span was decided the way it was.
	if rationale := terminalCoverageRationale(disposition); rationale != "" {
		intake.Coverage[len(intake.Coverage)-1].Rationale = rationale
	}
	return intake
}

// TestOnlyTheEdgeIntoReviewedRefusesUnresolvedCoverage pins the rule's shape
// rather than one instance of it.
//
// Three properties have to hold together and none of them implies the others: a
// curator must still be able to *record* an unjudged span (or the honest answer
// disappears from the record), an intake must not be able to *become* reviewed
// while one remains, and an already-reviewed intake must still be able to
// advance (or its repair path closes). Enumerating the edges is the only way to
// state that as one rule instead of three coincidences.
func TestOnlyTheEdgeIntoReviewedRefusesUnresolvedCoverage(t *testing.T) {
	for _, test := range []struct {
		name        string
		from        string
		to          string
		disposition string
		refused     bool
	}{
		{name: "a curator may draft an unjudged span", from: "draft", to: "draft",
			disposition: "unresolved"},
		{name: "a curator may submit an unjudged span", from: "draft", to: "submitted",
			disposition: "unresolved"},
		{name: "a curator may resubmit an unjudged span", from: "submitted", to: "submitted",
			disposition: "unresolved"},
		{name: "a packet with an unjudged span may be superseded", from: "submitted", to: "superseded",
			disposition: "unresolved"},
		{name: "a packet with an unjudged span may not be ratified", from: "submitted", to: "reviewed",
			disposition: "unresolved", refused: true},
		{name: "a fully judged packet ratifies", from: "submitted", to: "reviewed",
			disposition: "non-claim"},
		{name: "an out-of-scope judgment ratifies", from: "submitted", to: "reviewed",
			disposition: "out-of-scope"},
		{name: "an already-reviewed intake may still advance", from: "reviewed", to: "reviewed",
			disposition: "unresolved"},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := ratificationEdgeIntake(test.from, test.disposition, 1)
			next := ratificationEdgeIntake(test.to, test.disposition, 2)
			next.UpdatedAt, next.UpdatedBy = "2026-08-02T18:07:00Z", "manager"
			err := ValidateIntakeTransition(&previous, next)
			if test.refused {
				if err == nil {
					t.Fatal("an intake carrying an unjudged span was ratified")
				}
				if !strings.Contains(err.Error(), "still unresolved") {
					t.Fatalf("the edge was refused for the wrong reason: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("a legal intake edge was refused: %v", err)
			}
		})
	}
}
