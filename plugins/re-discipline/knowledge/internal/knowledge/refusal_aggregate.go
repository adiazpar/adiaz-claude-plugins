package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// Aggregate refusals: why a mutation refuses with a list rather than a fact.
//
// Every refusal in this engine is correct, and that was never the problem. The
// problem is that each one answers exactly one question. A caller that gets a
// large mutation wrong in five independent ways learns one of the five per
// attempt, and pays the full payload - on run.prepare, a context pack running
// to thousands of tokens - to learn the next one. A recorded session spent
// seven round trips and roughly fifty thousand tokens on a single run.prepare,
// almost all of it re-sending fields that had been correct since the first
// attempt. DESIGN.md P5 states the rule this file implements: validate the
// whole request and return every violation at once, ordered.
//
// The reason this is not simply "collect the errors into a slice" is that the
// checks are not independent of each other. Large parts of this engine's
// validation are written in the ordinary Go style where a later check assumes
// an earlier one passed: it dereferences a pointer a prior check proved
// non-nil, indexes request.Runs[0] after proving there is exactly one run,
// reads graph.WorkItems[id] after proving the id resolves. Running all of them
// unconditionally against a malformed request does not produce five honest
// facts; it produces one fact and a panic, or five facts of which four are
// noise derived from a premise that does not hold. Reporting noise as if it
// were fact is worse than reporting one true thing, because the caller cannot
// tell which of the five to believe.
//
// So the checks are split by what they need, and only one half aggregates:
//
//   - Shape violations are answerable from the request alone. Required fields,
//     formats, enum membership, digest patterns, canonical path shape, LF and
//     trailing-newline rules, and the internal consistency of a record the
//     caller submitted. These can be evaluated in any order against anything,
//     including a zero-valued request, so they never short-circuit. Every one
//     is collected.
//
//   - State violations need the loaded campaign graph: record compare-and-swap,
//     revision arithmetic, transition legality, cross-record binding, and
//     authority. Project-global actions additionally bind the committed head.
//     These keep failing fast once their canonical premise cannot be resolved.
//
// An aggregate therefore carries all shape violations plus at most one state
// violation - the first one that could be established without depending on a
// broken shape. Ordinary record-scoped work never adds an unrelated global-head
// fact; strict project-global actions may still report their exact-head gate.
//
// Aggregation never changes what is caught. Every check that refused before
// still refuses; only the number of them delivered per response changed. Where
// a check could not be made safe to run out of order it was left failing fast,
// and the reason is written at its call site.

// Shape violations are grouped into stages and reported in stage order so that
// a caller can fix them top-down. The order is the order in which a problem
// makes the ones after it unanswerable: there is no point telling someone their
// run's status is wrong before telling them they sent no run at all. Within a
// stage, violations keep the order they were found in, which for loops over
// submitted records is the caller's own submission order.
const (
	// shapeStageEnvelope covers the transaction identity every mutation needs
	// regardless of what it writes: actor, ids, correlation, idempotency, and
	// the compare-and-swap inputs.
	shapeStageEnvelope = 10
	// shapeStageComposition covers which typed records the named action
	// requires and forbids. Nothing below this stage can be trusted until the
	// request is carrying the right kinds of things at the right cardinality.
	shapeStageComposition = 20
	// shapeStagePayload covers per-record and cross-field rules inside the
	// request: statuses, roles, revision relationships between two submitted
	// records, enum membership.
	shapeStagePayload = 30
	// shapeStageArtifact covers the canonical bytes a request carries - a run
	// brief's encoding, a submitted context pack's self-consistency - which the
	// engine seals verbatim and therefore cannot repair.
	shapeStageArtifact = 40
	// shapeStageBinding covers record identity, canonical path, and the
	// expectedRecordDigests entries each submitted revision requires. It is
	// last because it is per-record bookkeeping: correct records in the wrong
	// transaction are a smaller edit than the wrong records.
	shapeStageBinding = 50
	// stateStageFirst is reserved for the single state violation an aggregate
	// may carry. It always sorts last so the list reads as "these are the
	// things your request says, and this is what canonical state says".
	stateStageFirst = 90
)

// AggregateRefusal is a refusal carrying more than one violation. It exists so
// that a caller can fix a request in one edit instead of one edit per round
// trip.
//
// It implements Unwrap() []error rather than flattening its violations into a
// string, so errors.Is and errors.As keep working through it. That matters
// concretely: a stale-head violation folded into an aggregate must still
// satisfy errors.Is(err, ErrStateConflict), or every caller and test that
// distinguishes "somebody else went first" from "your payload is wrong" would
// silently start treating a conflict as a shape bug.
type AggregateRefusal struct {
	// Subject names the operation that refused, for example
	// "manager_apply run.prepare". A caller reading a list of eight items under
	// pressure needs to know, on the first line, which of its calls produced
	// them.
	Subject string
	// Violations are ordered for top-down repair and are never empty; a
	// refusalSet with one violation returns that violation unwrapped instead of
	// building an aggregate, because a numbered list of one is noise.
	Violations []error
}

func (refusal *AggregateRefusal) Error() string {
	if refusal == nil || len(refusal.Violations) == 0 {
		return "refused"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"%s refused with %d violations; fix all of them and re-issue once. "+
			"They were evaluated independently, so this is the complete list:",
		refusal.Subject, len(refusal.Violations))
	for index, violation := range refusal.Violations {
		// One violation per line, numbered. The individual messages are already
		// long because each names its own remedy; wrapping or re-flowing them
		// here would make the list harder to scan, not easier.
		fmt.Fprintf(&builder, "\n  %d. %s", index+1, violation.Error())
	}
	return builder.String()
}

func (refusal *AggregateRefusal) Unwrap() []error {
	if refusal == nil {
		return nil
	}
	return refusal.Violations
}

// stagedRefusal is one violation plus enough ordering information to sort a set
// deterministically. The insertion index is kept because sort.Slice is not
// stable and two identical requests must refuse with identical text - a
// non-deterministic ordering would make one retry look like a different failure
// from the last, which is exactly the confusion this file exists to remove.
type stagedRefusal struct {
	stage int
	order int
	err   error
}

// refusalSet accumulates violations for one request. The zero value is usable.
type refusalSet struct {
	subject    string
	violations []stagedRefusal
}

func newRefusalSet(subject string) *refusalSet {
	return &refusalSet{subject: subject}
}

// add records a violation. A nil error is ignored so that call sites can pass
// the result of an existing validator directly without an if.
func (set *refusalSet) add(stage int, err error) {
	if set == nil || err == nil {
		return
	}
	set.violations = append(set.violations, stagedRefusal{
		stage: stage, order: len(set.violations), err: err,
	})
}

func (set *refusalSet) addf(stage int, format string, args ...any) {
	set.add(stage, fmt.Errorf(format, args...))
}

// empty reports whether anything has been collected. Call sites use it to skip
// work whose only purpose is to produce a better refusal.
func (set *refusalSet) empty() bool {
	return set == nil || len(set.violations) == 0
}

// result returns nil, the single violation unwrapped, or an AggregateRefusal.
//
// Returning the lone violation unwrapped is deliberate and load-bearing. Most
// requests are wrong in exactly one way, and for those the aggregate machinery
// must be invisible: the caller sees the same message it always saw, with the
// same error identity, and no numbered list wrapped around a single item.
func (set *refusalSet) result() error {
	if set.empty() {
		return nil
	}
	sorted := append([]stagedRefusal(nil), set.violations...)
	sort.Slice(sorted, func(left, right int) bool {
		if sorted[left].stage != sorted[right].stage {
			return sorted[left].stage < sorted[right].stage
		}
		return sorted[left].order < sorted[right].order
	})
	if len(sorted) == 1 {
		return sorted[0].err
	}
	violations := make([]error, 0, len(sorted))
	for _, violation := range sorted {
		violations = append(violations, violation.err)
	}
	return &AggregateRefusal{Subject: set.subject, Violations: violations}
}

// staleHeadConflict is the engine's single stale-head message.
//
// It lives here rather than inline because strict project-global operations may
// reach the same gate in the transaction journal or their aggregate state
// probe. Ordinary record-scoped transactions do not call it.
func staleHeadConflict(expectedRevision int64, expectedDigest string, head StateHead, campaignID string) error {
	return fmt.Errorf(
		"%w: this project-global action expected head %d/%s, found %d/%s. Canonical state "+
			"moved between its proof and this call, so nothing was written. Remedy: "+
			"state mode=delta campaignId=%s sinceEventId=<the eventHead you last read> "+
			"shows exactly what changed; rebuild the operation's project-wide proof and re-issue",
		ErrStateConflict, expectedRevision, expectedDigest,
		head.Revision, head.Digest, campaignID)
}
