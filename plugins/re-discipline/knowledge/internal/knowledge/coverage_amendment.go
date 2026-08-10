package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// permittedCoverageRetirements is the whole of the permitted-change rule,
// written as an explicit table of the ordered disposition pairs a retirement
// may make. It is deliberately not the negation of a forbidden set.
//
// Why only `unresolved` may move. `candidate-finding` and `duplicate` are the
// only dispositions that carry a targetId, and both are load-bearing for what a
// ratified finding rests on: moving a row into or out of either changes which
// report bytes a claim is sourced from, which is a change to evidence, not to
// bookkeeping. `non-claim` and `out-of-scope` are terminal judgments a manager
// already made; moving away from one retracts coverage a campaign may have
// closed on, and retracting a conclusion is overturn's job, not this
// transition's. `unresolved` is the only disposition that is not a judgment at
// all - it is a declared absence of one - and the only one reviewedReportCoverage
// treats as blocking. Giving it a judgment is therefore the only coverage edit
// that neither retracts nor re-sources anything.
//
// Why a table and not a negation. A negation ("anything except these pairs")
// silently admits every disposition added to ValidateIntake in future: a new
// disposition would become retirable the moment it existed, without anyone
// deciding that it should be. A table admits nothing that is not written in it,
// so widening this transition is an edit somebody has to make on purpose and a
// reviewer can see.
//
// Known asymmetry, accepted and not reconciled here. reviewedReportCoverage
// treats `out-of-scope` as covered, but verifyNormalizationResolution accepts
// only `candidate-finding`, `duplicate`, and `non-claim`. Retiring a span to
// `out-of-scope` therefore clears the closure coverage gate while still
// blocking a cross-campaign normalization resolution over the same report. That
// disagreement predates this transition; the transition only makes it easier to
// reach. Reconciling the two gates is a separate change with its own evidence,
// and folding it in here would hide it inside a bookkeeping fix.
var permittedCoverageRetirements = map[string]map[string]bool{
	"unresolved": {"non-claim": true, "out-of-scope": true},
}

func coverageRetirementIsPermitted(from, to string) bool {
	return permittedCoverageRetirements[from][to]
}

// permittedCoverageRetirementTargets renders the legal destinations for one
// starting disposition so a refusal can name them instead of leaving the caller
// to guess which pair it was that failed.
func permittedCoverageRetirementTargets(from string) string {
	targets := make([]string, 0, len(permittedCoverageRetirements[from]))
	for target := range permittedCoverageRetirements[from] {
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return "nothing"
	}
	sort.Strings(targets)
	return strings.Join(targets, " or ")
}

func validateCoverageRetirementShape(retirement CoverageRetirement) error {
	if strings.TrimSpace(retirement.SourceHandle) == "" {
		return errors.New("a coverage retirement must name the exact source handle of the span it retires")
	}
	if !coverageRetirementIsPermitted(retirement.FromDisposition, retirement.ToDisposition) {
		return fmt.Errorf(
			"coverage retirement %s may not move disposition %q to %q: a retirement gives a judgment to a "+
				"span that carried none, so only %q may move, and only to %s. Changing any other disposition "+
				"either re-sources a ratified finding or retracts a conclusion, which is overturn's business",
			retirement.SourceHandle, retirement.FromDisposition, retirement.ToDisposition,
			"unresolved", permittedCoverageRetirementTargets("unresolved"))
	}
	// The new rationale is mandatory because the retirement's whole content is
	// the judgment it supplies. An amendment that moved a span to `non-claim`
	// with no stated reason would leave the record asserting a manager judgment
	// nobody can read, which is exactly the state the unresolved span already
	// described more honestly.
	if strings.TrimSpace(retirement.ToRationale) == "" {
		return fmt.Errorf(
			"coverage retirement %s requires a new rationale stating why the span carries its new disposition",
			retirement.SourceHandle)
	}
	return nil
}

// ValidateCoverageAmendmentShape is the state-free half of the retirement rule:
// everything about one amendment entry that can be decided without the intake
// it is attached to, the campaign it belongs to, or the review it preserves.
func ValidateCoverageAmendmentShape(amendment CoverageAmendment) error {
	if amendment.Revision < 2 {
		return fmt.Errorf(
			"coverage amendment revision %d is impossible: an intake is created at revision 1 and reviewed "+
				"at a later one, so no amendment can predate revision 2", amendment.Revision)
	}
	if validateUTC(amendment.AmendedAt) != nil {
		return errors.New("coverage amendment requires a UTC RFC3339 amendedAt")
	}
	if strings.TrimSpace(amendment.AmendedBy) == "" {
		return errors.New("coverage amendment requires the manager identity that recorded it")
	}
	if !correlationIDRE.MatchString(amendment.CorrelationID) {
		return errors.New("coverage amendment requires the canonical correlation ID of its transaction")
	}
	if !reviewIDRE.MatchString(amendment.ReviewID) {
		return errors.New("coverage amendment must name the manager review it preserves")
	}
	if strings.TrimSpace(amendment.Rationale) == "" {
		return errors.New("coverage amendment requires a rationale for the retirement as a whole")
	}
	if len(amendment.Retirements) == 0 {
		return errors.New("coverage amendment must retire at least one span; an empty amendment records nothing")
	}
	seen := map[string]bool{}
	for _, retirement := range amendment.Retirements {
		if seen[retirement.SourceHandle] {
			return fmt.Errorf("coverage amendment names span %s twice", retirement.SourceHandle)
		}
		seen[retirement.SourceHandle] = true
		if err := validateCoverageRetirementShape(retirement); err != nil {
			return err
		}
	}
	return nil
}

// applyCoverageRetirements is the reconstruction: it derives the intake a
// retirement must produce from the intake that already exists plus the
// amendment alone, and nothing else.
//
// Reconstruction rather than diffing is what makes the amendment the sole
// authority for the change. A diff would ask "does the submitted record differ
// only in ways the amendment allows", and every route into the record that the
// diff did not think to look at - a coverage row split into two, a row deleted
// while its neighbour widened to close the gap, a candidate quietly added -
// would be a gap in it. Reconstruction asks the opposite question: the engine
// builds the only record the amendment can justify, and the caller's bytes
// either equal it or are refused. There is nothing left for the caller to
// smuggle, because nothing the caller sends is used except the amendment.
func applyCoverageRetirements(prior IntakeRecord, amendment CoverageAmendment) (IntakeRecord, error) {
	if err := ValidateCoverageAmendmentShape(amendment); err != nil {
		return IntakeRecord{}, err
	}
	next := prior
	next.Coverage = append([]CoverageEntry(nil), prior.Coverage...)
	positions := make(map[string]int, len(next.Coverage))
	for position, entry := range next.Coverage {
		positions[entry.SourceHandle] = position
	}
	for _, retirement := range amendment.Retirements {
		position, present := positions[retirement.SourceHandle]
		if !present {
			return IntakeRecord{}, fmt.Errorf(
				"coverage retirement names span %s, which intake %s does not cover. The handle is "+
					"path:<sourcePath>#L<start>-L<end> of an existing row, read from the committed record",
				retirement.SourceHandle, prior.ID)
		}
		row := next.Coverage[position]
		// The amendment must faithfully quote what it displaced. Comparing
		// against the persisted row is what makes an amendment a
		// compare-and-swap over one span rather than a blind overwrite: a
		// caller working from a stale read cannot retire a span whose
		// disposition or rationale has moved under it.
		if row.Disposition != retirement.FromDisposition || row.Rationale != retirement.FromRationale {
			return IntakeRecord{}, fmt.Errorf(
				"coverage retirement for %s quotes disposition %q and rationale %q, but intake %s records "+
					"%q and %q; an amendment must quote the exact values it displaces",
				retirement.SourceHandle, retirement.FromDisposition, retirement.FromRationale,
				prior.ID, row.Disposition, row.Rationale)
		}
		// An `unresolved` row can never carry a target, so this is redundant
		// against ValidateIntake today. It is stated anyway because it is the
		// property that matters - a retirement must not be able to change what
		// a finding rests on - and a redundant check that names the real rule
		// survives a future edit to the validator that a silent dependency
		// would not.
		if row.TargetID != "" {
			return IntakeRecord{}, fmt.Errorf(
				"coverage span %s targets finding %s and is therefore evidence, not unjudged coverage; "+
					"a retirement may not touch it", retirement.SourceHandle, row.TargetID)
		}
		row.Disposition, row.Rationale = retirement.ToDisposition, retirement.ToRationale
		next.Coverage[position] = row
	}
	next.Amendments = append(append([]CoverageAmendment(nil), prior.Amendments...), amendment)
	return next, nil
}

// validateAppliedCoverageRetirement is the whole of intake.coverage.retire at
// the transaction boundary.
//
// The transition exists because there was no small edit. Changing one coverage
// row's disposition and rationale - touching no finding, adding and removing no
// candidate - otherwise required a full curator packet through curation_submit,
// and because a curator may not resubmit an already-ratified finding, that
// dragged every candidate in the intake back through a re-ratification nobody
// re-read. The alternative in practice was hand-editing canonical JSON, which
// costs more integrity than the gate was buying.
func validateAppliedCoverageRetirement(
	previous CampaignGraph,
	request StateTransactionRequest,
	writes []preparedStateWrite,
) error {
	var submitted *IntakeRecord
	for _, write := range writes {
		if write.Internal {
			// The campaign record the journal advances for every transaction.
			continue
		}
		record, isIntake := write.Record.(IntakeRecord)
		if !isIntake {
			return fmt.Errorf(
				"a coverage retirement writes one intake and nothing else; this transaction also writes %T. "+
					"A retirement is bookkeeping over an already-ratified review, so a finding, review, or run "+
					"inside it would be a substantive change wearing a bookkeeping name", write.Record)
		}
		if submitted != nil {
			return errors.New("a coverage retirement must advance exactly one intake")
		}
		copied := record
		submitted = &copied
	}
	if submitted == nil {
		return errors.New("a coverage retirement requires the next revision of the intake it amends")
	}
	if previous.Campaign == nil {
		return errors.New("a coverage retirement requires canonical campaign state")
	}
	if !validOne(previous.Campaign.Status, "open", "closing") {
		return fmt.Errorf(
			"campaign %s is %s; a coverage retirement is a live bookkeeping transaction and cannot amend "+
				"an archived campaign's records", previous.Campaign.ID, previous.Campaign.Status)
	}
	prior, present := previous.Intakes[submitted.ID]
	if !present {
		return fmt.Errorf("intake %s is not canonical campaign state", submitted.ID)
	}
	if prior.Status != "reviewed" || submitted.Status != "reviewed" {
		return fmt.Errorf(
			"a coverage retirement amends a reviewed intake and leaves it reviewed; intake %s is %s and was "+
				"submitted as %s. An intake that is not yet reviewed needs no amendment - resubmit it through "+
				"curation_submit instead", submitted.ID, prior.Status, submitted.Status)
	}
	// Append-only history. The prefix relation is checked entry by entry rather
	// than by length alone, because a caller that rewrote an earlier entry while
	// appending a new one would otherwise be able to restate the history of a
	// span it already retired.
	if len(submitted.Amendments) != len(prior.Amendments)+1 {
		return fmt.Errorf(
			"a coverage retirement appends exactly one amendment; intake %s has %d and the submission carries %d",
			submitted.ID, len(prior.Amendments), len(submitted.Amendments))
	}
	for index, existing := range prior.Amendments {
		if !reflect.DeepEqual(existing, submitted.Amendments[index]) {
			return fmt.Errorf(
				"a coverage retirement may not rewrite amendment %d of intake %s; the log is append-only",
				index+1, submitted.ID)
		}
	}
	amendment := submitted.Amendments[len(submitted.Amendments)-1]
	// Every binding on the amendment except its two free-text fields is
	// determined by the transaction that carries it. A caller-chosen revision,
	// actor, correlation, or timestamp would let the log describe a transaction
	// that did not happen.
	if amendment.Revision != submitted.Revision {
		return fmt.Errorf(
			"coverage amendment records revision %d but the intake revision it is written at is %d",
			amendment.Revision, submitted.Revision)
	}
	if amendment.AmendedBy != request.Actor {
		return fmt.Errorf(
			"coverage amendment names %q but the transaction actor is %q",
			amendment.AmendedBy, request.Actor)
	}
	if amendment.CorrelationID != request.CorrelationID {
		return fmt.Errorf(
			"coverage amendment names correlation %q but the transaction correlation is %q",
			amendment.CorrelationID, request.CorrelationID)
	}
	if amendment.AmendedAt != submitted.UpdatedAt {
		return fmt.Errorf(
			"coverage amendment is timestamped %q but the intake revision it is written at is timestamped %q",
			amendment.AmendedAt, submitted.UpdatedAt)
	}
	// The amendment names the review it preserves, and the engine resolves it
	// rather than taking the caller's word. This is the third leg of the binding
	// argument: the revision does bump, the amendment count cancels the bump in
	// intakeReviewedRevision, and the named receipt is proved to be a manager
	// review of this exact intake at exactly the revision that arithmetic says
	// it binds - before and after the transaction.
	review, resolved := previous.Reviews[amendment.ReviewID]
	if !resolved || review.CampaignID != prior.CampaignID || review.IntakeID != prior.ID ||
		review.Authority != "manager" || review.IntakeRevision != intakeReviewedRevision(prior) {
		return fmt.Errorf(
			"coverage amendment names review %s, which is not the manager review that ratified intake %s at "+
				"revision %d. A retirement preserves an existing ratification; it cannot invent one",
			amendment.ReviewID, prior.ID, intakeReviewedRevision(prior))
	}
	expected, err := applyCoverageRetirements(prior, amendment)
	if err != nil {
		return err
	}
	// The submitted record's own metadata is transaction-owned and is validated
	// on its own terms by validateMetaTransition and the amendment bindings
	// above, so it is copied across before the comparison. Everything else must
	// be exactly what the amendment produces.
	expected.RecordMeta = submitted.RecordMeta
	expectedBody, err := canonicalIntakeComparisonForm(expected)
	if err != nil {
		return fmt.Errorf("intake %s could not be reconstructed from its amendment: %w", prior.ID, err)
	}
	submittedBody, err := canonicalIntakeComparisonForm(*submitted)
	if err != nil {
		return fmt.Errorf("submitted intake %s could not be canonicalized: %w", submitted.ID, err)
	}
	if !bytes.Equal(expectedBody, submittedBody) {
		detail := "the submitted and reconstructed records differ"
		if differences := canonicalFieldDifferences(submittedBody, expectedBody); len(differences) > 0 {
			detail = "differing fields: " + strings.Join(differences, "; ")
		}
		return fmt.Errorf(
			"intake %s is not the record its coverage amendment produces (%s). A retirement changes only the "+
				"disposition and rationale of the spans its amendment names; resubmit the committed record with "+
				"exactly those changes applied and nothing else",
			submitted.ID, detail)
	}
	// The post-condition the whole design turns on: after the transaction the
	// review still binds the intake, so reviewBindsIntake, CampaignGraph.Validate,
	// and normalizationReviewComplete all continue to hold.
	if intakeReviewedRevision(*submitted) != review.IntakeRevision {
		return fmt.Errorf(
			"after the retirement intake %s would report reviewed revision %d, but review %s binds %d",
			submitted.ID, intakeReviewedRevision(*submitted), review.ID, review.IntakeRevision)
	}
	return nil
}
