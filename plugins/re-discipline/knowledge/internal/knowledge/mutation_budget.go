package knowledge

import (
	"encoding/json"
	"fmt"
)

// Budgeting a write is a different problem from budgeting a read, and the
// difference is the whole reason this file exists rather than reusing the
// retrieval budget machinery.
//
// A read is a projection: the caller asked a question, and dropping the tail of
// the answer costs it recall, not correctness. Every budgeted read here says so
// - `query`, `trace`, and `read` all drop cards or nodes from the end and
// record the count in an omissions list, and a caller that wanted more raises
// its budget and asks again.
//
// A write response is a receipt. It is the caller's only account of a
// transaction that has already committed, and it is the input to the next
// compare-and-swap. Truncating it is therefore not a smaller answer, it is a
// wrong one: a caller that receives four of six record results has no way to
// tell that from a transaction that wrote four records, and the next mutation
// it builds carries a stale or missing expectedRecordDigests entry and is
// refused for a reason that has nothing to do with what it did wrong. Worse, a
// receipt is the thing a caller retains as proof; a silently short one is a
// falsified record.
//
// So the rule this file implements is narrower than "fit in N tokens":
//
//   - Never truncate. A section is either whole or absent.
//   - Never omit identity. Record ids, revisions, digests, both state heads,
//     the transaction id, and the event id are the compare-and-swap inputs for
//     the following transaction; omitting any of them forces an extra read and
//     costs more than the omission saves. They are the floor and are not
//     droppable at any budget.
//   - Name every omission in the response, with the operation that returns the
//     omitted section. A caller must never have to distinguish "absent" from
//     "empty" by guessing.
//   - Leave `digest` fields alone even when the record they belong to has been
//     trimmed. A record's digest is its identity on disk, not a checksum of
//     this response body; it stays because the next transaction needs it. The
//     omission note is what tells a caller that recomputing it from what it
//     received will not reproduce it.
//
// When no budget is requested the response is complete, exactly as before.
// That is deliberate: a caller that does not opt in cannot be surprised by a
// receipt that is missing something it used to rely on.
const (
	mutationTokenBudgetMinimum = 128
	mutationTokenBudgetMaximum = 8192
)

// validateMutationTokenBudget accepts zero as "return everything" and otherwise
// holds writes to the same 128..8192 window the read tools use, so a caller does
// not have to remember two ranges.
func validateMutationTokenBudget(tool string, budget int) error {
	if budget == 0 || (budget >= mutationTokenBudgetMinimum && budget <= mutationTokenBudgetMaximum) {
		return nil
	}
	return fmt.Errorf(
		"%s tokenBudget must be between %d and %d; omit tokenBudget entirely for a complete response",
		tool, mutationTokenBudgetMinimum, mutationTokenBudgetMaximum)
}

// mutationTokenBudgetSchema publishes the write budget as an affordance rather
// than leaving a caller to discover it by tripping a refusal - the same mistake
// the optional run-launch handles made, where `omitempty` rendered as an absent
// `required` entry and nothing more. droppable names, in the schema itself,
// exactly which sections a budget may remove, so a caller can decide whether it
// can afford to lose them before it sets the field.
func mutationTokenBudgetSchema(droppable string) map[string]any {
	return integerSchemaDescription(
		mutationTokenBudgetMinimum, mutationTokenBudgetMaximum,
		"Optional response budget; omit for the complete response. Nothing is truncated and "+
			"ids, revisions, digests, heads, and the event id always return. Droppable: "+
			droppable+". Each drop is named in omitted.")
}

// estimateResponseTokens measures the response the way the transport will
// serialize it. It deliberately measures the whole value rather than summing
// per-section estimates: the section list below is small, and a measurement
// that can disagree with what is actually sent is worse than one extra
// Marshal.
func estimateResponseTokens(value any) (int, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return EstimateTokens(string(body)), nil
}

// budgetSection is one derived region of a mutation response that may be
// dropped whole.
//
// Note is not decoration. It is the entire value of the omission: a caller that
// is told "job.coverage omitted" and nothing else is in exactly the position
// this change exists to remove, having to discover by experiment which call
// returns it. Every Note names the operation that returns the section.
type budgetSection struct {
	Name    string
	Note    string
	Present func() bool
	Drop    func()
}

// applyResponseBudget drops sections in the fixed order given until the
// response fits, and returns one rendered note per section it dropped.
//
// The order is fixed rather than largest-first on purpose. Largest-first makes
// the same request return different shapes on two campaigns of different sizes,
// which makes a caller's handling of the response data-dependent; a fixed order
// means "if X was dropped, everything before X was dropped too", which a caller
// can reason about without measuring anything. Order the list least-useful-first
// for building the next call.
//
// If the floor alone exceeds the budget the response is still returned whole
// below the floor - refusing would strand a committed transaction whose receipt
// the caller has no other way to obtain, and truncating is forbidden above. The
// caller sees every section it lost and an honest token estimate.
func applyResponseBudget(budget int, response any, sections []budgetSection) ([]string, error) {
	if budget <= 0 {
		return nil, nil
	}
	estimated, err := estimateResponseTokens(response)
	if err != nil {
		return nil, err
	}
	omitted := []string{}
	for _, section := range sections {
		if estimated <= budget {
			break
		}
		if section.Present != nil && !section.Present() {
			continue
		}
		section.Drop()
		omitted = append(omitted, section.Name+" omitted under tokenBudget: "+section.Note)
		if estimated, err = estimateResponseTokens(response); err != nil {
			return nil, err
		}
	}
	if len(omitted) == 0 {
		return nil, nil
	}
	return omitted, nil
}

// budgetTransactionReceipt trims the one derived section a manager or curator
// transaction receipt carries.
//
// Everything else in the receipt is floor. Records is the compare-and-swap
// input for the next transaction, one entry per written record with its id,
// revision, and digest, and it is the single most expensive thing to have to
// re-derive: without it the caller must re-read every record it just wrote.
// PreviousHead and ResultingHead are expectedHeadRevision/expectedHeadDigest for
// the next call. Event carries the event id that anchors the journal chain, and
// it is not trimmed field-by-field because Event.digest is computed over the
// whole event - a partial event would carry a digest that does not verify,
// which is precisely the falsified receipt this file refuses to produce.
//
// Artifacts is the exception: it is one row per published file, it is derived
// from paths the caller either supplied or can recompute, and nothing in the
// next transaction reads it. On a closure finalization it is also the largest
// thing in the receipt by a wide margin.
func budgetTransactionReceipt(
	receipt StateTransactionReceipt,
	budget int,
) (StateTransactionReceipt, error) {
	if budget <= 0 {
		return receipt, nil
	}
	omitted, err := applyResponseBudget(budget, &receipt, []budgetSection{
		{
			Name: "artifacts",
			Note: "one row per published file; the paths are already known to the caller " +
				"and no compare-and-swap input reads them. Re-issue this transition with " +
				"tokenBudget omitted to see them",
			Present: func() bool { return len(receipt.Artifacts) != 0 },
			Drop:    func() { receipt.Artifacts = nil },
		},
	})
	if err != nil {
		return StateTransactionReceipt{}, err
	}
	receipt.Omitted = omitted
	return receipt, nil
}
