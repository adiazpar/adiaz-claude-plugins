package knowledge

import (
	"context"
	"strings"
	"testing"
	"time"
)

func openContextPackTestCampaign(t *testing.T, root string) (*StateStore, StateTransactionReceipt) {
	t.Helper()
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fixed, _ := time.Parse(time.RFC3339, stateTestTime)
	store.Now = func() time.Time { return fixed }
	_, receipt := openStateTestCampaign(t, store)
	return store, receipt
}

func TestScopedContextPackBindsWorkConstraintsAndStateHead(t *testing.T) {
	root := makeAdversarialProject(t)
	store, opening := openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)
	request := ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: "C-TEST", WorkItemID: "W-0001",
			RunID: "R-20260802-0002",
		},
		Task: "Validate the canonical state graph without unrelated campaign history",
		Role: "drafter", TokenBudget: 1024,
	}
	pack, err := service.ContextPackOptions(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Scope.Kind != "active-run" || pack.Scope.CampaignID != "C-TEST" ||
		pack.Scope.WorkItemID != "W-0001" || pack.Scope.RunID != "R-20260802-0002" ||
		pack.Scope.StateHeadRevision != opening.ResultingHead.Revision ||
		pack.Scope.StateHeadDigest != opening.ResultingHead.Digest || pack.Scope.RunRevision != 0 {
		t.Fatalf("pack lost its canonical state binding: %#v", pack.Scope)
	}
	wantHandles := map[string]bool{
		"record:active/test-campaign/campaign.json":          false,
		"record:active/test-campaign/work-items/W-0001.json": false,
	}
	for _, handle := range pack.RequiredHandles {
		if _, present := wantHandles[handle]; present {
			wantHandles[handle] = true
		}
	}
	for handle, found := range wantHandles {
		if !found {
			t.Fatalf("pack omitted mandatory expansion handle %s", handle)
		}
	}
	kinds := map[string]bool{}
	for _, constraint := range pack.AcceptedConstraints {
		kinds[constraint.Kind] = true
	}
	for _, kind := range []string{"objective", "scope", "success", "closure", "problem", "acceptance"} {
		if !kinds[kind] {
			t.Fatalf("pack omitted accepted %s constraint: %#v", kind, pack.AcceptedConstraints)
		}
	}
	for _, card := range pack.Cards {
		if card.SourceClass == "history" || card.Metadata["tier"] == "history" {
			t.Fatalf("scoped drafter pack inherited unrelated campaign history: %#v", card)
		}
	}
	if _, err := VerifyContextPackValue(pack); err != nil {
		t.Fatalf("scoped pack failed independent verification: %v", err)
	}

	// Any unrelated canonical mutation advances the state binding and therefore
	// changes the pack identity. A retained preview cannot be replayed across a
	// different state head without detection.
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), stateTestCreateWorkRequest(
		head, "W-0002", "corr-context-head", "idem-context-head")); err != nil {
		t.Fatal(err)
	}
	changed, err := service.ContextPackOptions(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Scope.StateHeadDigest == pack.Scope.StateHeadDigest || changed.Digest == pack.Digest {
		t.Fatal("state-head advance did not invalidate the retained context preview")
	}
}

func TestScopedContextPackRejectsUnresolvedOrMixedTargets(t *testing.T) {
	root := makeAdversarialProject(t)
	openContextPackTestCampaign(t, root)
	service := newAdversarialService(t, root, nil)
	base := ContextPackRequest{
		Target: ContextPackTarget{
			Kind: "active-run", CampaignID: "C-TEST", WorkItemID: "W-9999",
			RunID: "R-20260802-0002",
		},
		Task: "invalid target", Role: "drafter", TokenBudget: 1024,
	}
	if _, err := service.ContextPackOptions(context.Background(), base); err == nil ||
		!strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("unresolved work target was accepted: %v", err)
	}
	base.Target.WorkItemID = "W-0001"
	base.Target.CandidateSlug = "candidate-one"
	if _, err := service.ContextPackOptions(context.Background(), base); err == nil ||
		!strings.Contains(err.Error(), "requires campaignId") {
		t.Fatalf("mixed active/recruiting target was accepted: %v", err)
	}
}
